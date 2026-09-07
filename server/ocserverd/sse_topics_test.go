// The two cases here are INPUT→OUTPUT behaviour, which the drift gate cannot
// see. The other two properties this render has — that it is deterministic, and
// that it describes hub.go's sseTopics exactly — are guaranteed by
// drift-sse-topics (regenerate the artifact and byte-diff the committed one), so
// they are NOT restated here: a second guard over a property something else
// already proves is the duplication this whole change exists to remove.

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderSSETopics(t *testing.T) {
	t.Run("renders the closed vocabulary sorted, with the generated-artifact header", func(t *testing.T) {
		blob, n, err := renderSSETopics(map[string]bool{"member": true, "chat": true, "task": true})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if n != 3 {
			t.Fatalf("count = %d, want 3", n)
		}
		var doc sseTopicsDocument
		if err := json.Unmarshal(blob, &doc); err != nil {
			t.Fatalf("output is not JSON: %v", err)
		}
		if doc.GeneratedBy != sseTopicsGeneratedBy || doc.Source != sseTopicsSource {
			t.Fatalf("header = %q/%q, want %q/%q", doc.GeneratedBy, doc.Source, sseTopicsGeneratedBy, sseTopicsSource)
		}
		if got := strings.Join(doc.Topics, ","); got != "chat,member,task" {
			t.Fatalf("topics = %q, want sorted chat,member,task", got)
		}
		if !bytes.HasSuffix(blob, []byte("\n")) {
			t.Fatal("output must end in a newline (the drift gate diffs it as a text file)")
		}
	})

	// An empty vocabulary is the vacuity failure: every consumer confronts its
	// own coverage with this list, and an empty one makes each of those
	// confrontations pass while proving nothing.
	t.Run("refuses an empty vocabulary rather than writing one", func(t *testing.T) {
		for name, in := range map[string]map[string]bool{
			"no entries":        {},
			"all entries false": {"member": false},
			"nil map":           nil,
		} {
			if _, _, err := renderSSETopics(in); err == nil {
				t.Fatalf("%s: rendered an empty topic set instead of refusing", name)
			}
		}
	})
}
