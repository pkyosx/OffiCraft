package main

// mcp.go — the tools/call loopback (spec/mcp.md §3): re-enter the app's OWN mux
// in-process with the caller's Authorization, so gate, RBAC, param binding and
// handler run exactly as for a direct REST call — that equivalence is the
// contract. Tool NAMES derive from the route table; tools/list DESCRIPTORS stay
// served from the frozen spec/mcp-catalog.json (api_infra.go).

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

func emptyPathParam(value any) bool {
	if value == nil {
		return true
	}
	s, isString := value.(string)
	return isString && strings.TrimSpace(s) == ""
}

func unsafePathParam(value string) bool {
	return strings.Contains(value, "/") || value == "." || value == ".."
}

// toolName applies the frozen tool_name rule verbatim.
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

// pyArgString renders a value the way the Python transport's str() did (True /
// False / None; json.Number keeps "3" vs "3.0").
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

// splitToolArguments splits the flat `arguments` object per spec/mcp.md §3.1.
func splitToolArguments(spec RouteSpec, arguments map[string]any) (reqPath string, rawQuery string, body []byte, err error) {
	remaining := make(map[string]any, len(arguments))
	for k, v := range arguments {
		remaining[k] = v
	}

	reqPath = spec.Path
	for _, match := range pathParamRe.FindAllStringSubmatch(spec.Path, -1) {
		name := match[1]
		value, ok := remaining[name]
		if !ok || emptyPathParam(value) {
			return "", "", nil, errors.New("field required: " + name)
		}
		sub := pyArgString(value)
		if unsafePathParam(sub) {
			return "", "", nil, errors.New("invalid path: " + name)
		}
		delete(remaining, name)
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
		return reqPath, query.Encode(), nil, nil
	}

	raw, err := json.Marshal(remaining)
	if err != nil {
		raw = []byte("{}")
	}
	return reqPath, "", raw, nil
}

// loopbackRecorder is deliberately not an http.Flusher: no tool route streams.
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

func (s *apiServer) loopbackCall(r *http.Request, method, reqPath, rawQuery string, body []byte) (int, []byte, error) {
	if s.loopback == nil {
		return 0, nil, errors.New("loopback handler not wired")
	}
	// Pre-clean so the mux gets the canonical path instead of issuing a 301; keep
	// it even though splitToolArguments already rejects route-reinterpreting params.
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

// callToolResult (spec/mcp.md §3.3): a route 4xx is a successful JSON-RPC result
// with isError, never a JSON-RPC error; numbers stay json.Number so the
// structuredContent re-marshal is literal-exact.
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
