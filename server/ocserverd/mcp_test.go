package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestEmptyPathParam(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "nil", value: nil, want: true},
		{name: "empty string", value: "", want: true},
		{name: "whitespace only", value: " \t\n", want: true},
		{name: "nonempty string", value: " member ", want: false},
		{name: "false is a value", value: false, want: false},
		{name: "zero is a value", value: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := emptyPathParam(tt.value); got != tt.want {
				t.Fatalf("emptyPathParam(%#v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestToolName(t *testing.T) {
	tests := []struct {
		name string
		spec RouteSpec
		want string
	}{
		{
			name: "explicit name wins",
			spec: RouteSpec{Method: http.MethodGet, Path: "/api/members", MCPTool: "list_people"},
			want: "list_people",
		},
		{
			name: "method and api path are derived",
			spec: RouteSpec{Method: http.MethodGet, Path: "/api/members"},
			want: "get_members",
		},
		{
			name: "nested path separators become underscores",
			spec: RouteSpec{Method: http.MethodPost, Path: "/api/tasks/notes"},
			want: "post_tasks_notes",
		},
		{
			name: "trailing slash does not add an empty component",
			spec: RouteSpec{Method: http.MethodPatch, Path: "/api/settings/"},
			want: "patch_settings",
		},
		{
			name: "a path without an api tail uses only the method",
			spec: RouteSpec{Method: http.MethodDelete, Path: "/api/"},
			want: "delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.toolName(); got != tt.want {
				t.Fatalf("toolName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMcpToolIndex(t *testing.T) {
	rows := []RouteSpec{
		{Method: http.MethodGet, Path: "/api/health", MCPExclude: true},
		{Method: http.MethodGet, Path: "/api/members", Summary: "list members"},
		{Method: http.MethodPost, Path: "/api/members", MCPTool: "hire_member", Summary: "hire member"},
	}

	got := mcpToolIndex(rows)
	if len(got) != 2 {
		t.Fatalf("mcpToolIndex returned %d tools, want 2", len(got))
	}

	for _, tt := range []struct {
		name    string
		method  string
		path    string
		summary string
	}{
		{name: "get_members", method: http.MethodGet, path: "/api/members", summary: "list members"},
		{name: "hire_member", method: http.MethodPost, path: "/api/members", summary: "hire member"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec, ok := got[tt.name]
			if !ok {
				t.Fatalf("tool %q is missing from %#v", tt.name, got)
			}
			if spec.Method != tt.method || spec.Path != tt.path || spec.Summary != tt.summary {
				t.Fatalf("tool %q = %#v, want method=%q path=%q summary=%q", tt.name, spec, tt.method, tt.path, tt.summary)
			}
		})
	}

	if _, ok := got["get_health"]; ok {
		t.Fatal("an MCP-excluded route appeared in the tool index")
	}
}

func TestPyArgString(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "none", value: nil, want: "None"},
		{name: "string is not quoted", value: " hello ", want: " hello "},
		{name: "true", value: true, want: "True"},
		{name: "false", value: false, want: "False"},
		{name: "integer number literal", value: json.Number("3"), want: "3"},
		{name: "decimal number literal", value: json.Number("3.0"), want: "3.0"},
		{name: "default integer", value: 3, want: "3"},
		{name: "default array", value: []any{1, "x"}, want: `[1,"x"]`},
		{name: "marshal failure is empty", value: func() {}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pyArgString(tt.value); got != tt.want {
				t.Fatalf("pyArgString(%#v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestSplitToolArguments(t *testing.T) {
	t.Run("a GET removes path keys and encodes the remaining values", func(t *testing.T) {
		spec := RouteSpec{Method: http.MethodGet, Path: "/api/items/{item_id}"}
		args := map[string]any{
			"item_id": "item-7",
			"enabled": true,
			"status":  "open",
			"tag":     []any{"alpha", "beta value"},
			"unset":   nil,
		}

		path, query, body, err := splitToolArguments(spec, args)
		if err != nil {
			t.Fatalf("splitToolArguments: %v", err)
		}
		if path != "/api/items/item-7" {
			t.Fatalf("path = %q, want %q", path, "/api/items/item-7")
		}
		if query != "enabled=True&status=open&tag=alpha&tag=beta+value" {
			t.Fatalf("query = %q, want %q", query, "enabled=True&status=open&tag=alpha&tag=beta+value")
		}
		if body != nil {
			t.Fatalf("GET body = %q, want nil", body)
		}
	})

	t.Run("a GET with only unset optionals has no query", func(t *testing.T) {
		spec := RouteSpec{Method: http.MethodGet, Path: "/api/items"}
		path, query, body, err := splitToolArguments(spec, map[string]any{"filter": nil})
		if err != nil {
			t.Fatalf("splitToolArguments: %v", err)
		}
		if path != "/api/items" || query != "" || body != nil {
			t.Fatalf("got path=%q query=%q body=%q, want /api/items, empty query, nil body", path, query, body)
		}
	})

	t.Run("a write route serializes all remaining arguments", func(t *testing.T) {
		spec := RouteSpec{Method: http.MethodPost, Path: "/api/items/{item_id}"}
		path, query, body, err := splitToolArguments(spec, map[string]any{
			"item_id": "item-7",
			"title":   "Ship it",
			"count":   json.Number("3.0"),
		})
		if err != nil {
			t.Fatalf("splitToolArguments: %v", err)
		}
		if path != "/api/items/item-7" || query != "" {
			t.Fatalf("got path=%q query=%q, want /api/items/item-7 and empty query", path, query)
		}
		if got, want := string(body), `{"count":3.0,"title":"Ship it"}`; got != want {
			t.Fatalf("body = %q, want %q", got, want)
		}
	})

	t.Run("a write route always gets an empty object when it has no body fields", func(t *testing.T) {
		spec := RouteSpec{Method: http.MethodPatch, Path: "/api/items/{item_id}"}
		path, query, body, err := splitToolArguments(spec, map[string]any{"item_id": "item-7"})
		if err != nil {
			t.Fatalf("splitToolArguments: %v", err)
		}
		if path != "/api/items/item-7" || query != "" || string(body) != "{}" {
			t.Fatalf("got path=%q query=%q body=%q, want /api/items/item-7, empty query, {}", path, query, body)
		}
	})

	t.Run("path values use the Python string spelling", func(t *testing.T) {
		spec := RouteSpec{Method: http.MethodGet, Path: "/api/items/{item_id}"}
		path, _, _, err := splitToolArguments(spec, map[string]any{"item_id": json.Number("3.0")})
		if err != nil {
			t.Fatalf("splitToolArguments: %v", err)
		}
		if path != "/api/items/3.0" {
			t.Fatalf("path = %q, want %q", path, "/api/items/3.0")
		}
	})

	for _, tt := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing", args: map[string]any{}, want: "field required: item_id"},
		{name: "null", args: map[string]any{"item_id": nil}, want: "field required: item_id"},
		{name: "empty", args: map[string]any{"item_id": ""}, want: "field required: item_id"},
		{name: "whitespace", args: map[string]any{"item_id": " \t"}, want: "field required: item_id"},
		{name: "slash", args: map[string]any{"item_id": "items/7"}, want: "invalid path: item_id"},
		{name: "dot", args: map[string]any{"item_id": "."}, want: "invalid path: item_id"},
		{name: "dot dot", args: map[string]any{"item_id": ".."}, want: "invalid path: item_id"},
	} {
		t.Run("invalid path: "+tt.name, func(t *testing.T) {
			spec := RouteSpec{Method: http.MethodGet, Path: "/api/items/{item_id}"}
			path, query, body, err := splitToolArguments(spec, tt.args)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if path != "" || query != "" || body != nil {
				t.Fatalf("failed split returned path=%q query=%q body=%q", path, query, body)
			}
		})
	}
}

func TestWriteHeader(t *testing.T) {
	rec := newLoopbackRecorder()
	if rec.status != http.StatusOK || rec.wroteHeader {
		t.Fatalf("new recorder = status %d, wroteHeader=%v; want 200 and false", rec.status, rec.wroteHeader)
	}

	rec.Header().Set("X-Test", "present")
	rec.WriteHeader(http.StatusCreated)
	rec.WriteHeader(http.StatusTeapot)

	if rec.status != http.StatusCreated {
		t.Fatalf("status after duplicate WriteHeader = %d, want %d", rec.status, http.StatusCreated)
	}
	if !rec.wroteHeader {
		t.Fatal("WriteHeader did not mark the header as written")
	}
	if got := rec.Header().Get("X-Test"); got != "present" {
		t.Fatalf("Header value = %q, want %q", got, "present")
	}
}

func TestWrite(t *testing.T) {
	rec := newLoopbackRecorder()

	n, err := rec.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("first Write = (%d, %v), want (5, nil)", n, err)
	}
	if rec.status != http.StatusOK || !rec.wroteHeader {
		t.Fatalf("implicit header = status %d, wroteHeader=%v; want 200 and true", rec.status, rec.wroteHeader)
	}

	n, err = rec.Write([]byte(" world"))
	if err != nil || n != 6 {
		t.Fatalf("second Write = (%d, %v), want (6, nil)", n, err)
	}
	if got := rec.body.String(); got != "hello world" {
		t.Fatalf("body = %q, want %q", got, "hello world")
	}
}

func TestLoopbackCall(t *testing.T) {
	t.Run("forwards the request contract and returns the sub-response", func(t *testing.T) {
		type contextKey struct{}
		key := contextKey{}
		ctx := context.WithValue(context.Background(), key, "same context")
		original := httptest.NewRequest(http.MethodPost, "/api/mcp", nil).WithContext(ctx)
		original.Header.Set("Authorization", "Bearer exact-value")
		requestBody := []byte(`{"name":"Kip"}`)

		api := &apiServer{loopback: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPatch {
				t.Errorf("method = %q, want %q", r.Method, http.MethodPatch)
			}
			if r.URL.Path != "/api/members/kip" {
				t.Errorf("path = %q, want %q", r.URL.Path, "/api/members/kip")
			}
			if r.URL.RawQuery != "fields=light" {
				t.Errorf("query = %q, want %q", r.URL.RawQuery, "fields=light")
			}
			if got := r.Header.Get("Authorization"); got != "Bearer exact-value" {
				t.Errorf("authorization = %q, want %q", got, "Bearer exact-value")
			}
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Errorf("accept = %q, want %q", got, "application/json")
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("content type = %q, want %q", got, "application/json")
			}
			if r.ContentLength != int64(len(requestBody)) {
				t.Errorf("content length = %d, want %d", r.ContentLength, len(requestBody))
			}
			if got := r.Context().Value(key); got != "same context" {
				t.Errorf("context value = %#v, want %q", got, "same context")
			}
			gotBody, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			if !reflect.DeepEqual(gotBody, requestBody) {
				t.Errorf("body = %q, want %q", gotBody, requestBody)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("created"))
		})}

		status, body, err := api.loopbackCall(original, http.MethodPatch, "/api/items/../members/kip", "fields=light", requestBody)
		if err != nil {
			t.Fatalf("loopbackCall: %v", err)
		}
		if status != http.StatusCreated || string(body) != "created" {
			t.Fatalf("response = (%d, %q), want (201, %q)", status, body, "created")
		}
	})

	t.Run("a nil body uses NoBody and does not set write headers", func(t *testing.T) {
		api := &apiServer{loopback: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != http.NoBody {
				t.Errorf("body = %#v, want http.NoBody", r.Body)
			}
			if r.Header.Get("Content-Type") != "" {
				t.Errorf("content type = %q, want empty", r.Header.Get("Content-Type"))
			}
			_, _ = w.Write([]byte("ok"))
		})}

		original := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
		status, body, err := api.loopbackCall(original, http.MethodGet, "/api/health", "", nil)
		if err != nil {
			t.Fatalf("loopbackCall: %v", err)
		}
		if status != http.StatusOK || string(body) != "ok" {
			t.Fatalf("response = (%d, %q), want (200, %q)", status, body, "ok")
		}
	})

	t.Run("without a wired handler it returns an internal error", func(t *testing.T) {
		api := &apiServer{}
		_, _, err := api.loopbackCall(httptest.NewRequest(http.MethodPost, "/api/mcp", nil), http.MethodGet, "/api/health", "", nil)
		if err == nil || err.Error() != "loopback handler not wired" {
			t.Fatalf("error = %v, want loopback handler not wired", err)
		}
	})
}

func TestCallToolResult(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		raw            string
		wantError      bool
		wantStructured bool
	}{
		{name: "empty successful body", status: http.StatusOK, raw: "", wantError: false, wantStructured: false},
		{name: "object at success boundary", status: http.StatusOK, raw: `{"count":3.0}`, wantError: false, wantStructured: true},
		{name: "object at error boundary", status: http.StatusBadRequest, raw: `{"error":"bad"}`, wantError: true, wantStructured: true},
		{name: "array stays text only", status: http.StatusOK, raw: `[1,2]`, wantError: false, wantStructured: false},
		{name: "scalar stays text only", status: http.StatusOK, raw: `3.0`, wantError: false, wantStructured: false},
		{name: "null stays text only", status: http.StatusOK, raw: `null`, wantError: false, wantStructured: false},
		{name: "invalid JSON stays text only", status: http.StatusInternalServerError, raw: `{"error":`, wantError: true, wantStructured: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callToolResult(tt.status, []byte(tt.raw))
			if got["isError"] != tt.wantError {
				t.Fatalf("isError = %#v, want %v", got["isError"], tt.wantError)
			}
			content, ok := got["content"].([]any)
			if !ok || len(content) != 1 {
				t.Fatalf("content = %#v, want one item", got["content"])
			}
			item, ok := content[0].(map[string]any)
			if !ok || item["type"] != "text" || item["text"] != tt.raw {
				t.Fatalf("content item = %#v, want text %q", content[0], tt.raw)
			}
			_, hasStructured := got["structuredContent"]
			if hasStructured != tt.wantStructured {
				t.Fatalf("structuredContent present=%v, want %v", hasStructured, tt.wantStructured)
			}
		})
	}

	structured := callToolResult(http.StatusOK, []byte(`{"count":3.0,"nested":{"id":7}}`))["structuredContent"]
	want := map[string]any{
		"count":  json.Number("3.0"),
		"nested": map[string]any{"id": json.Number("7")},
	}
	if !reflect.DeepEqual(structured, want) {
		t.Fatalf("structuredContent = %#v, want %#v", structured, want)
	}
}
