package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type capturedPost struct {
	path string
	auth string
	body string
}

// contextServer accepts every POST and records it verbatim.
func contextServer(t *testing.T) (*httptest.Server, *[]capturedPost) {
	t.Helper()
	var posts []capturedPost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		posts = append(posts, capturedPost{
			path: r.URL.Path,
			auth: r.Header.Get("Authorization"),
			body: string(raw),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &posts
}

func testEnv(extra map[string]string) func(string) string {
	return func(key string) string { return extra[key] }
}

func findPost(posts []capturedPost, path string) *capturedPost {
	for i := range posts {
		if posts[i].path == path {
			return &posts[i]
		}
	}
	return nil
}

func postPaths(posts []capturedPost) []string {
	paths := make([]string, 0, len(posts))
	for _, one := range posts {
		paths = append(paths, one.path)
	}
	return paths
}

func writeClaudeJSON(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", ".claude.json"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

// frozenIngestProperties loads one request schema's declared properties from
// spec/openapi.json as name → declared JSON type ("" when the property declares
// none), and refuses a schema that is not closed.
func frozenIngestProperties(t *testing.T, schemaName string) map[string]string {
	t.Helper()
	specPath := filepath.Join("..", "..", "spec", "openapi.json")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read frozen spec %s: %v", specPath, err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
				AdditionalProperties *bool `json:"additionalProperties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse frozen spec: %v", err)
	}
	schema, ok := spec.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("%s not in the frozen spec", schemaName)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatalf("%s is not a closed schema — comparing against it proves nothing", schemaName)
	}
	declared := map[string]string{}
	for name, prop := range schema.Properties {
		declared[name] = prop.Type
	}
	return declared
}

// jsonKind names a raw JSON value's type in OpenAPI's vocabulary.
func jsonKind(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "invalid"
	}
	switch trimmed[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// schemaViolations reports every key in body the frozen schema would refuse:
// undeclared names, plus declared names carrying the wrong JSON type. A property
// that declares no type is deliberately permissive and is skipped; a null value
// is how a producer says "not measured".
func schemaViolations(body string, declared map[string]string) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &obj) != nil {
		return []string{"<body is not a JSON object>"}
	}
	var bad []string
	for key, raw := range obj {
		want, isDeclared := declared[key]
		if !isDeclared {
			bad = append(bad, key+" (undeclared)")
			continue
		}
		if want == "" {
			continue
		}
		if got := jsonKind(raw); got != "null" && got != want &&
			!(want == "integer" && got == "number") {
			bad = append(bad, key+" (declared "+want+", sent "+got+")")
		}
	}
	sort.Strings(bad)
	return bad
}
