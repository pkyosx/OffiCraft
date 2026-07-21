package main

// api_taskmanuals_patch_test.go — T-9ffd: anchor-addressed patch of a type's
// learnings (MCP patch_task_learnings), the patch_lessons twin for task
// manuals.
//
// Why it exists: the ONLY write face for learnings was whole-doc replace
// (write_task_learnings / update_task_manual.learnings). As a manual's
// learnings grows (30k chars observed) re-typing the whole doc to add a few
// lines stops fitting in one model output AND every re-type silently risks
// transcription loss — the tool answers 200 either way. patch makes the write
// cost scale with the CHANGE, not the doc.
//
// ApplyLessonsEdits is the SHARED engine (generic over the doc text), so the
// anchor/append/atomicity/shrink semantics are byte-identical to patch_lessons;
// these tests re-pin them on the LEARNINGS document specifically. EVERY test
// asserts on a READ-BACK of the stored learnings, never the status code alone —
// a wipe/mis-splice that returned 2xx would otherwise slip through.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// patchLearnings drives the handler directly (like writeLearnings) and returns
// the status plus the decoded JSON body.
func patchLearnings(t *testing.T, api *apiServer, typeKey string, body any) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost(rec, taskReq(t, "POST",
		"/api/task-manuals/"+typeKey+"/learnings/patch", body, "m-exec", "agent"), typeKey)
	var data map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
	}
	return rec.Code, data
}

// edit is a tiny helper for the {old,new} shape taskReq marshals to JSON.
func edit(old, newv string) map[string]any { return map[string]any{"old": old, "new": newv} }

// TestPatchTaskLearningsUniqueAnchorReplace: a unique-anchor replace lands
// splice-precise and the receipt's size/sha256/applied_edits describe the
// RESULTING learnings so the caller can confirm without re-reading the doc.
func TestPatchTaskLearningsUniqueAnchorReplace(t *testing.T) {
	api := newTasksTestServer(t)
	key := seedManualWithLearnings(t, api,
		"line one\nline two: keep the old habit\nline three\n")

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("line two: keep the old habit", "line two: adopt the new habit")},
	})
	if status != http.StatusOK {
		t.Fatalf("unique-anchor patch must land, got %d: %v", status, data)
	}

	want := "line one\nline two: adopt the new habit\nline three\n"
	if got := storedLearnings(t, api, key); got != want {
		t.Fatalf("patched doc mismatch:\n got: %q\nwant: %q", got, want)
	}
	sum := sha256.Sum256([]byte(want))
	if got, _ := data["sha256"].(string); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 anchor mismatch: %v", data["sha256"])
	}
	if got, _ := data["size"].(float64); int(got) != len(want) {
		t.Fatalf("size anchor mismatch: got %v want %d", data["size"], len(want))
	}
	if got, _ := data["applied_edits"].(float64); int(got) != 1 {
		t.Fatalf("applied_edits mismatch: %v", data["applied_edits"])
	}
	if got, _ := data["type_key"].(string); got != key {
		t.Fatalf("receipt type_key mismatch: got %v want %s", data["type_key"], key)
	}
}

// TestPatchTaskLearningsAppendWithEmptyOld: an empty old appends, joined with a
// single newline when the doc does not already end in one.
func TestPatchTaskLearningsAppendWithEmptyOld(t *testing.T) {
	api := newTasksTestServer(t)
	key := seedManualWithLearnings(t, api, "existing learning") // no trailing \n

	status, _ := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("", "appended learning")},
	})
	if status != http.StatusOK {
		t.Fatalf("append must land, got %d", status)
	}
	if got := storedLearnings(t, api, key); got != "existing learning\nappended learning" {
		t.Fatalf("append must join with one newline, got %q", got)
	}
}

// TestPatchTaskLearningsAnchorNotFoundIs400: a non-empty old that matches
// nothing rejects the whole batch and writes nothing.
func TestPatchTaskLearningsAnchorNotFoundIs400(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "line one\nline two\n"
	key := seedManualWithLearnings(t, api, seeded)

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("a line that is simply not there", "x")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("a missing anchor must be a flat 400, got %d: %v", status, data)
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchTaskLearningsMultiEditAtomicity: a batch whose later edit misses
// must 400 with ZERO writes — no earlier edit's splice may survive. And a
// batch of sequential hits sees each edit's result.
func TestPatchTaskLearningsMultiEditAtomicity(t *testing.T) {
	api := newTasksTestServer(t)
	const base = "alpha\nbeta\ngamma\n"
	key := seedManualWithLearnings(t, api, base)

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("alpha", "ALPHA"), edit("never-there", "x")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("batch with a missing anchor must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "edits[1]") {
		t.Fatalf("error must name the failing edit index, got: %q", msg)
	}
	if got := storedLearnings(t, api, key); got != base {
		t.Fatalf("partial write leaked — atomicity broken:\n got: %q\nwant: %q", got, base)
	}

	status, _ = patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("beta", "beta prime"), edit("beta prime", "beta prime indeed")},
	})
	if status != http.StatusOK {
		t.Fatalf("sequential edits must land, got %d", status)
	}
	if got := storedLearnings(t, api, key); !strings.Contains(got, "beta prime indeed") {
		t.Fatalf("sequential edit result missing: %q", got)
	}
}

// TestPatchTaskLearningsAmbiguousAnchorRejected: an old matching >1 locations
// is ambiguous and rejects the whole batch.
func TestPatchTaskLearningsAmbiguousAnchorRejected(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "repeat\nrepeat\n"
	key := seedManualWithLearnings(t, api, seeded)

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("repeat", "changed")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("ambiguous anchor must 400, got %d: %v", status, data)
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchTaskLearningsEmptyEditsRejected: an empty edits array is a malformed
// request, not a request to change nothing.
func TestPatchTaskLearningsEmptyEditsRejected(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "line one\nline two\n"
	key := seedManualWithLearnings(t, api, seeded)

	status, data := patchLearnings(t, api, key, map[string]any{"edits": []any{}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("empty edits must be refused (422), got %d: %v", status, data)
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchTaskLearningsRejectsEmptyEdit: an edit carrying NEITHER old NOR new
// is malformed. Folding nil→"" would route it into the empty-old APPEND branch
// (a perfect no-op behind a 200) — the exact patch_lessons pre-fix bug.
func TestPatchTaskLearningsRejectsEmptyEdit(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "line one\nline two\n"
	key := seedManualWithLearnings(t, api, seeded)

	status, data := patchLearnings(t, api, key, map[string]any{"edits": []any{map[string]any{}}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("an edit with neither old nor new must be refused (422), got %d: %v", status, data)
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchTaskLearningsRejectsUnknownEditKeys pins the NESTED
// DisallowUnknownFields: the incident shape ({old_text,new_text}) must not be
// dropped key-by-key into {nil,nil} and appended as nothing behind a 200.
func TestPatchTaskLearningsRejectsUnknownEditKeys(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "line one\nline two\n"
	key := seedManualWithLearnings(t, api, seeded)

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{map[string]any{"old_text": "line two", "new_text": "line two changed"}},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown edit keys must be refused (422), got %d: %v", status, data)
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchTaskLearningsRejectsUnknownKeyAlongsideValidEdit isolates the nested
// unknown-field guard from the neither-old-nor-new check: a VALID old/new rides
// along with a stray key, so ONLY DisallowUnknownFields descending into the
// array element can reject it.
func TestPatchTaskLearningsRejectsUnknownKeyAlongsideValidEdit(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "line one\nline two\n"
	key := seedManualWithLearnings(t, api, seeded)

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{map[string]any{"old": "line two", "new": "line two changed", "old_text": "stray"}},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("a stray key on an otherwise valid edit must be refused (422), got %d: %v", status, data)
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchTaskLearningsWipeNeedsAllowShrink: a patch that empties the doc is
// refused without allow_shrink; the flag lets it through on purpose.
func TestPatchTaskLearningsWipeNeedsAllowShrink(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "learnings worth protecting from an accidental blanking patch\n"
	key := seedManualWithLearnings(t, api, seeded)

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit(seeded, "")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("unguarded wipe must be refused (400), got %d: %v", status, data)
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("learnings must survive a refused wipe:\n got: %q\nwant: %q", got, seeded)
	}

	status, _ = patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit(seeded, "")}, "allow_shrink": true,
	})
	if status != http.StatusOK {
		t.Fatalf("allow_shrink wipe must land, got %d", status)
	}
	if got := storedLearnings(t, api, key); got != "" {
		t.Fatalf("allow_shrink wipe must empty the doc, got %q", got)
	}
}

// TestPatchTaskLearningsAppliedEditsCountsWhatLanded: applied_edits reports the
// edits that ACTUALLY changed the doc, so "0 applied" is expressible and a
// no-op cannot masquerade as a success (the patch_lessons pre-fix bug reported
// len(edits), structurally incapable of being 0).
func TestPatchTaskLearningsAppliedEditsCountsWhatLanded(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "line one\nline two\n"
	key := seedManualWithLearnings(t, api, seeded)

	// A real replace plus an append of the empty string (a perfect no-op).
	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("line two", "line two changed"), edit("", "")},
	})
	if status != http.StatusOK {
		t.Fatalf("patch must land, got %d: %v", status, data)
	}
	if applied, _ := data["applied_edits"].(float64); applied != 1 {
		t.Fatalf("applied_edits must count only the edit that changed the doc, got %v", data["applied_edits"])
	}
	want := "line one\nline two changed\n"
	if got := storedLearnings(t, api, key); got != want {
		t.Fatalf("patched doc mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestPatchTaskLearningsUnknownTypeIs404: a patch against a type that does not
// exist is a 404, not a silent create.
func TestPatchTaskLearningsUnknownTypeIs404(t *testing.T) {
	api := newTasksTestServer(t)
	status, data := patchLearnings(t, api, "tm-does-not-exist", map[string]any{
		"edits": []any{edit("anything", "x")},
	})
	if status != http.StatusNotFound {
		t.Fatalf("patch of an unknown type must 404, got %d: %v", status, data)
	}
}
