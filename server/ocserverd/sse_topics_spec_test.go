package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// specTopicRow matches one row of the spec/sse.md §3.1 topic table:
// "| `<topic>` | <trigger> | <op> |".
//
// ⚠️ DUPLICATED PARSER (knowingly, this round): conformance/test_sse.py's
// _closed_topic_set() parses the SAME markdown table to confront its trigger
// table with the closed set. Two parsers of one table is a smell, and the
// proper fix is a MACHINE-READABLE spec asset (a spec/sse-topics.json emitted
// or frozen alongside spec/openapi.json, consumed by both sides and by the
// frontend's SSE_RESYNC_TOPICS) — that adds a frozen wire asset, which is the
// owner's call under the wire freeze (root CLAUDE.md「驗證、CI 與出貨／wire spec-first」), not a tidy-up to do
// in passing. Until then: keep the two parsers in step, and do NOT "solve" the
// duplication by deleting one of the two guards — they bind different edges
// (this one code↔spec, the Python one spec↔conformance coverage).
var specTopicRow = regexp.MustCompile("(?m)^\\|\\s*`([a-z_]+)`\\s*\\|")

// ⚠️ ALREADY-INVESTIGATED, ALREADY-GUARDED — do not re-run this hunt. Go's test
// cache does NOT track ../../spec/sse.md, so editing only the spec and re-running
// `go test` locally can hand back a CACHED ok while this equality would in fact
// fail (measured in review round 2: a `budget` row added to §3.1 kept printing
// `ok ocserverd (cached)`; the same mutant with -count=1 fails as intended).
// That is not a hole in CI: bin/tests/go-test-nocache-guard.sh parses every
// `go test` invocation in CI and requires -count=1, precisely so no CI green is
// a cache replay. The only rule this leaves for humans: pass -count=1 when you
// run this by hand after touching the spec.
//
// specSSETopics reads the CLOSED topic set from the product's own wire
// contract, spec/sse.md §3.1 — the table hub.go's sseTopics cites as its
// source. Same precedent as the openapi/mcp-catalog readers in this package
// (os.ReadFile("../../spec/…")).
func specSSETopics(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../spec/sse.md")
	if err != nil {
		t.Fatalf("read spec/sse.md: %v", err)
	}
	_, after, found := strings.Cut(string(raw), "### 3.1")
	if !found {
		t.Fatal("spec/sse.md: §3.1 heading not found — the closed-topic table " +
			"moved or was renamed; this guard reads it by heading")
	}
	section, _, found := strings.Cut(after, "### 3.2")
	if !found {
		t.Fatal("spec/sse.md: §3.2 heading not found — cannot bound the §3.1 " +
			"topic table")
	}
	out := map[string]bool{}
	for _, m := range specTopicRow.FindAllStringSubmatch(section, -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatal("spec/sse.md §3.1: parsed ZERO topics — the table's shape " +
			"changed. Fix the parser; an empty set would make this guard " +
			"assert nothing.")
	}
	return out
}

// TestSSETopicsMatchSpec binds the publish seam's closed set to the wire
// contract that declares it.
//
// WHY this exists: sseTopics is the enforcement point (Publish drops anything
// outside it) and spec/sse.md §3.1 is the contract, but NOTHING checked that
// the two agree — a topic added to one and not the other drifted silently in
// either direction. conformance/test_sse.py now confronts its trigger table
// with the spec table, which makes spec/sse.md the authority for that guard;
// without THIS test that authority is unbacked (add a topic to hub.go only,
// and every guard stays green).
//
// Deliberately an EQUALITY, not a subset: an extra topic in the code is a
// phantom wire topic no client is contracted to understand, and a topic in the
// spec that the code drops is a delta the wire promises and never sends.
//
// ⚠️ SCOPE — the four DIRECTED bands are NOT missing from this equality, they
// are deliberately outside it. `context-high` (spec §6), `token-expiry`
// (§6.1), `warden-command` (§7) and `task-close` (§8) go out through
// hub.PushDirected and never touch Publish, so they are neither in sseTopics
// nor in §3.1's table; §3.1 says so itself ("a separate envelope family, not
// entity-delta topics"). Do NOT
// "repair" this test by adding them to either side — that would make the
// equality fail against a spec table they were never meant to be in, and it
// would put non-entity topics through the Publish gate. They are pinned by
// their own tests (conformance/test_sse.py's test_context_high_* and
// test_warden_command_band_start_frame). The same note sits on the Python edge
// of this pair (conformance/test_sse.py's _closed_topic_set docstring).
func TestSSETopicsMatchSpec(t *testing.T) {
	spec := specSSETopics(t)

	var missing, extra []string
	for topic := range spec {
		if !sseTopics[topic] {
			missing = append(missing, topic)
		}
	}
	for topic, on := range sseTopics {
		if on && !spec[topic] {
			extra = append(extra, topic)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("hub.go sseTopics MUST equal the closed topic set declared by "+
			"spec/sse.md §3.1.\n"+
			"  declared by the spec but DROPPED at the publish seam (the wire "+
			"promises a delta that can never be sent): %v\n"+
			"  emitted by the code but NOT in the contract (a phantom wire "+
			"topic; add it to spec §3.1 first — spec-first): %v",
			missing, extra)
	}
}
