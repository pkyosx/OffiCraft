package main

// mcp.go — the tools/call loopback (spec/mcp.md §3): resolve the tool name
// back to its route-table row, split the flat `arguments` object into
// path / query / body, re-enter the app's OWN mux in-process forwarding the
// caller's Authorization header (same gate, same RBAC choke, same param
// binding, same handler — the loopback mechanism is an implementation
// detail; that equivalence is the contract), and wrap the sub-response as a
// CallToolResult (isError ≡ status>=400; structuredContent iff the body is a
// JSON object).
//
// The tool NAME surface is derived from the route table (RouteSpec.toolName
// mirrors the frozen tool_name vocabulary), never hand-maintained; the
// tools/list DESCRIPTORS stay served from the frozen spec/mcp-catalog.json
// (api_infra.go) — see the note there.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"
)

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// toolName is the MCP tool name of a route row: the explicit override, else
// derived from method+path — the frozen tool_name rule verbatim.
func (s RouteSpec) toolName() string {
	if s.MCPTool != "" {
		return s.MCPTool
	}
	tail := strings.TrimPrefix(s.Path, "/api/")
	tail = strings.Trim(tail, "/")
	tail = strings.ReplaceAll(tail, "/", "_")
	if tail != "" {
		return strings.ToLower(s.Method) + "_" + tail
	}
	return strings.ToLower(s.Method)
}

// mcpToolIndex maps tool name → route row over the non-mcp_exclude rows —
// the same filter the frozen catalog and catalog_hash key off, so the
// callable set is exactly the tools/list surface.
func mcpToolIndex(specs []RouteSpec) map[string]RouteSpec {
	index := make(map[string]RouteSpec)
	for _, spec := range specs {
		if spec.MCPExclude {
			continue
		}
		index[spec.toolName()] = spec
	}
	return index
}

// pyArgString renders one argument value the way the Python transport's
// str() does when substituting path params / urlencoding query params:
// JSON literals for numbers (json.Number preserves "3" vs "3.0"),
// True/False/None spellings for bool/null.
func pyArgString(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case string:
		return t
	case bool:
		if t {
			return "True"
		}
		return "False"
	case json.Number:
		return t.String()
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

// splitToolArguments splits the flat `arguments` object per spec/mcp.md §3.1:
// path keys pop into the path template (a missing key substitutes the empty
// string — no error at this layer), a GET route's remaining non-null keys
// become the query string (list values expand doseq-style), any other
// method's remaining keys become the JSON body (an empty object when nothing
// remains — a body is ALWAYS sent for a write route).
func splitToolArguments(spec RouteSpec, arguments map[string]any) (reqPath string, rawQuery string, body []byte) {
	remaining := make(map[string]any, len(arguments))
	for k, v := range arguments {
		remaining[k] = v
	}

	reqPath = spec.Path
	for _, match := range pathParamRe.FindAllStringSubmatch(spec.Path, -1) {
		name := match[1]
		sub := ""
		if value, ok := remaining[name]; ok {
			sub = pyArgString(value)
			delete(remaining, name)
		}
		reqPath = strings.ReplaceAll(reqPath, "{"+name+"}", sub)
	}

	if spec.Method == http.MethodGet {
		query := url.Values{}
		for k, v := range remaining {
			if v == nil {
				continue // unset optionals are dropped, never sent as "None"
			}
			if items, isList := v.([]any); isList {
				for _, item := range items {
					query.Add(k, pyArgString(item))
				}
				continue
			}
			query.Add(k, pyArgString(v))
		}
		return reqPath, query.Encode(), nil
	}

	raw, err := json.Marshal(remaining)
	if err != nil {
		raw = []byte("{}")
	}
	return reqPath, "", raw
}

// loopbackRecorder captures the sub-response (status + body) of an in-process
// re-entry. Deliberately NOT an http.Flusher: no tool route streams (the SSE
// route is mcp_exclude).
type loopbackRecorder struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func newLoopbackRecorder() *loopbackRecorder {
	return &loopbackRecorder{header: make(http.Header), status: http.StatusOK}
}

func (rec *loopbackRecorder) Header() http.Header { return rec.header }

func (rec *loopbackRecorder) WriteHeader(status int) {
	if rec.wroteHeader {
		return
	}
	rec.wroteHeader = true
	rec.status = status
}

func (rec *loopbackRecorder) Write(b []byte) (int, error) {
	if !rec.wroteHeader {
		rec.WriteHeader(http.StatusOK)
	}
	return rec.body.Write(b)
}

// loopbackCall re-enters the app's own mux in-process (spec/mcp.md §3.2),
// forwarding the caller's Authorization header verbatim so the auth gate,
// the RBAC choke, the wrapper param binding, and the handler guards run
// exactly as for a direct REST call.
func (s *apiServer) loopbackCall(r *http.Request, method, reqPath, rawQuery string, body []byte) (int, []byte, error) {
	if s.loopback == nil {
		return 0, nil, errors.New("loopback handler not wired")
	}
	// Pre-clean the path so the mux serves the natural 404/405 instead of a
	// 301 canonicalisation redirect (an empty path-param substitution leaves
	// "//" in the path; spec §3.1 wants the route to 404/405 naturally).
	cleaned := pathpkg.Clean(reqPath)
	req := (&http.Request{
		Method:     method,
		URL:        &url.URL{Path: cleaned, RawQuery: rawQuery},
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       "loopback",
		RemoteAddr: "loopback:0",
	}).WithContext(r.Context())
	req.Header.Set("Accept", "application/json")
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Length", strconv.Itoa(len(body)))
		req.ContentLength = int64(len(body))
		req.Body = io.NopCloser(bytes.NewReader(body))
	} else {
		req.Body = http.NoBody
	}

	rec := newLoopbackRecorder()
	s.loopback.ServeHTTP(rec, req)
	return rec.status, rec.body.Bytes(), nil
}

// callToolResult wraps a loopback sub-response as a CallToolResult
// (spec/mcp.md §3.3): a single text content item carrying the raw body,
// isError ≡ status>=400 (a route 4xx is a successful JSON-RPC result, never
// a JSON-RPC error), structuredContent present iff the body parses as a JSON
// object (numbers kept as json.Number so the re-marshal is literal-exact).
func callToolResult(status int, raw []byte) map[string]any {
	result := map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(raw)}},
		"isError": status >= 400,
	}
	if len(raw) > 0 && json.Valid(raw) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var structured any
		if dec.Decode(&structured) == nil {
			if obj, isObj := structured.(map[string]any); isObj {
				result["structuredContent"] = obj
			}
		}
	}
	return result
}
