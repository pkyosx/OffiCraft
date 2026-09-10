package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRenderSSETopics(t *testing.T) {
	t.Run("enabled topics are sorted and disabled entries are omitted from the JSON document", func(t *testing.T) {
		blob, count, err := renderSSETopics(map[string]bool{"zeta": true, "alpha": true, "disabled": false})
		if err != nil {
			t.Fatalf("renderSSETopics: %v", err)
		}
		var got sseTopicsDocument
		if err := json.Unmarshal(blob, &got); err != nil {
			t.Fatalf("rendered JSON: %v", err)
		}
		want := sseTopicsDocument{
			GeneratedBy: sseTopicsGeneratedBy,
			Source:      sseTopicsSource,
			Topics:      []string{"alpha", "zeta"},
		}
		if count != len(want.Topics) || !reflect.DeepEqual(got, want) {
			t.Fatalf("renderSSETopics = count %d, document %#v; want count %d, document %#v", count, got, len(want.Topics), want)
		}
		if !strings.HasSuffix(string(blob), "\n") {
			t.Fatalf("rendered topic document has no trailing newline: %q", blob)
		}
	})

	t.Run("an empty enabled set is refused instead of producing vacuous coverage data", func(t *testing.T) {
		blob, count, err := renderSSETopics(map[string]bool{"disabled": false})
		if blob != nil || count != 0 || err == nil {
			t.Fatalf("renderSSETopics(empty) = (%q, %d, %v), want (nil, 0, error)", blob, count, err)
		}
		if !strings.Contains(err.Error(), "closed topic set is EMPTY") {
			t.Fatalf("empty-set error = %q, want the closed-set explanation", err)
		}
	})
}

func TestCmdSSETopics(t *testing.T) {
	t.Run("missing output path returns usage without creating a file", func(t *testing.T) {
		var out strings.Builder
		if got := cmdSSETopics(nil, &out); got != 2 {
			t.Fatalf("cmdSSETopics(nil) = %d, want 2", got)
		}
		if !strings.Contains(out.String(), "usage: ocserverd sse-topics <out.json>") {
			t.Fatalf("usage output = %q", out.String())
		}
	})

	t.Run("an explicit path receives the same bytes the renderer derives from the live topic map", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "topics.json")
		var out strings.Builder
		if got := cmdSSETopics([]string{path}, &out); got != 0 {
			t.Fatalf("cmdSSETopics(%q) = %d, output %q", path, got, out.String())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		want, count, err := renderSSETopics(sseTopics)
		if err != nil {
			t.Fatalf("renderSSETopics(live map): %v", err)
		}
		if !reflect.DeepEqual(data, want) {
			t.Fatalf("written topic document differs from renderer output:\n got %s\nwant %s", data, want)
		}
		if !strings.Contains(out.String(), "[gen-sse-topics] wrote "+path) || !strings.Contains(out.String(), "topic(s)") {
			t.Fatalf("success output = %q, want path and count %d", out.String(), count)
		}
	})
}
