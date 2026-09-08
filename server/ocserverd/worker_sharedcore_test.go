// Skeleton generated from server/ocserverd/worker_sharedcore.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestWorkerSharedHead(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)

	base, err := api.workerSharedHead()
	if err != nil {
		t.Fatalf("workerSharedHead(default): %v", err)
	}
	if !strings.HasPrefix(base, "# Global Context（AI 工作室 · 成員 boot context）") {
		t.Fatalf("worker shared head does not begin with the shared system-interaction block: %q", base[:min(len(base), 80)])
	}
	if strings.Contains(base, userAdditionsTitle) {
		t.Fatalf("empty owner additions produced an empty block: %q", base)
	}
	if strings.TrimSpace(base) != base {
		t.Fatalf("worker shared head has surrounding whitespace")
	}

	if err := d.PutUserContext(UserContext{Text: "studio rule"}); err != nil {
		t.Fatalf("PutUserContext: %v", err)
	}
	withAdditions, err := api.workerSharedHead()
	if err != nil {
		t.Fatalf("workerSharedHead(overlay): %v", err)
	}
	wantSuffix := userAdditionsTitle + "\n\nstudio rule"
	if !strings.HasPrefix(withAdditions, base+"\n\n") || !strings.HasSuffix(withAdditions, wantSuffix) {
		t.Fatalf("owner additions were not appended as the second shared block: %q", withAdditions)
	}
}

func TestWorkerBootSequence(t *testing.T) {
	api, h, _, owner := newAPITestServer(t)
	status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"Claude steps"}`)
	if status != http.StatusOK {
		t.Fatalf("replace claude boot sequence: %d (%v)", status, data)
	}
	status, data = apiJSON(t, h, "POST", "/api/boot-sequence/codex", owner, `{"body":"Codex steps"}`)
	if status != http.StatusOK {
		t.Fatalf("replace codex boot sequence: %d (%v)", status, data)
	}

	for _, tc := range []struct {
		runtime string
		want    string
	}{
		{runtime: RuntimeClaude, want: "Claude steps"},
		{runtime: RuntimeCodex, want: "Codex steps"},
		{runtime: "opus", want: "Claude steps"},
		{runtime: "", want: "Claude steps"},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			got, err := api.workerBootSequence(tc.runtime)
			if err != nil {
				t.Fatalf("workerBootSequence(%q): %v", tc.runtime, err)
			}
			if got != tc.want {
				t.Fatalf("workerBootSequence(%q) = %q, want %q", tc.runtime, got, tc.want)
			}
		})
	}
}
