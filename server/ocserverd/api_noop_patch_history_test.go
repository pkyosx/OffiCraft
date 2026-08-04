package main

// api_noop_patch_history_test.go — a patch that changes NOTHING must not
// consume a document-history version.
//
// The shape of the defect these pin. All three anchor-patch seams
// (patch_lessons, patch_insight, patch_task_learnings) called
// SaveWithDocumentHistory unconditionally, including when ApplyDocEdits
// reported applied == 0 — every anchor matched, every splice was the identity,
// the resulting text byte-identical to what was already stored. The write is
// harmless. The RETENTION is not: document history keeps only the three most
// recent versions per document (dal.go), so three such patches evict every
// version the owner could have restored and replace them with three copies of
// the text that is already live.
//
// Why it stayed invisible. Nothing about the symptom looks like a failure. The
// doc still reads correctly, the receipt still answers 200 with applied_edits:
// 0, and what was consumed is a RECOVERY path — noticed only later, by someone
// reaching for "restore the previous version" in the cockpit and finding three
// identical entries where their history used to be. No log line, no error, no
// other test goes red.
//
// Why it is reached by accident rather than by abuse. `old == new` is the
// cheapest way an agent can ask "does this string occur exactly once" or "what
// is this doc's sha256/size right now" without pulling the whole document into
// its context — the receipt's verification anchors are DESIGNED to answer that
// without a re-read. That probe is legitimate and stays legitimate: these tests
// therefore assert the receipt is unchanged (applied_edits: 0 plus the anchors
// over the doc as it stands) as well as that no version was consumed.
//
// Every test here uses a REAL edit first as its control. Without it, "history
// did not grow" could just mean retention never happens on this seam at all,
// and the no-op assertion would pass vacuously.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf8"
)

// noOpProbe is the batch an agent sends to count occurrences or read back the
// doc's anchors: a well-formed edit whose new is identical to its old, so the
// anchor must match exactly once and the splice changes nothing.
func noOpProbe(anchor string) map[string]any {
	return map[string]any{"edits": []any{map[string]any{"old": anchor, "new": anchor}}}
}

// assertNoOpReceipt pins the receipt shape a no-op patch answers with: the same
// fields as a landing patch, applied_edits 0, and anchors describing the doc as
// it stands. Callers depend on these — skipping the write must not change what
// they read back.
func assertNoOpReceipt(t *testing.T, data map[string]any, doc string) {
	t.Helper()
	if got, ok := data["applied_edits"].(float64); !ok || int(got) != 0 {
		t.Fatalf("a no-op patch must report applied_edits 0, got %v", data["applied_edits"])
	}
	sum := sha256.Sum256([]byte(doc))
	if got, _ := data["sha256"].(string); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 anchor must describe the doc as it stands: got %v", data["sha256"])
	}
	if got, _ := data["size_chars"].(float64); int(got) != utf8.RuneCountInString(doc) {
		t.Fatalf("size_chars anchor mismatch: got %v want %d", data["size_chars"], utf8.RuneCountInString(doc))
	}
	if _, present := data["cap_chars"]; !present {
		t.Fatalf("a no-op receipt must still carry cap_chars: %v", data)
	}
}

func TestNoOpPatchLessonsConsumesNoHistoryVersion(t *testing.T) {
	f := newHistoryFixture(t)
	const role, taskType = seedRoleAssistant, "general"
	const original = "LESSON: widen the anchor until it is unique.\n"
	const edited = "LESSON: widen the anchor until it is provably unique.\n"
	if err := f.api.dal.PutLessons(Lessons{RoleKey: role, TaskType: taskType, Text: original}); err != nil {
		t.Fatalf("seed lessons: %v", err)
	}
	patch := func(body any) (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost(rec,
			f.req(http.MethodPost, "/api/lessons/"+role+"/"+taskType+"/patch", body), role, taskType)
		var data map[string]any
		if rec.Body.Len() > 0 {
			_ = json.Unmarshal(rec.Body.Bytes(), &data)
		}
		return rec.Code, data
	}

	// Control: a real edit retains exactly one restorable version — the text it
	// replaced. If this does not hold, the assertions below prove nothing.
	status, data := patch(map[string]any{
		"edits": []any{map[string]any{"old": "unique.", "new": "provably unique."}},
	})
	if status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	history := f.list("lessons", role+"::"+taskType)
	if len(history) != 1 || history[0].Content["text"] != original {
		t.Fatalf("control: a real patch must retain the text it replaced, got %+v", history)
	}

	// Three no-op probes — one more than the doc has slots, so any per-call
	// retention would evict `original` outright.
	for i := 0; i < 3; i++ {
		status, data := patch(noOpProbe("provably unique"))
		if status != http.StatusOK {
			t.Fatalf("no-op probe %d must answer 200, got %d: %v", i, status, data)
		}
		assertNoOpReceipt(t, data, edited)
	}

	history = f.list("lessons", role+"::"+taskType)
	if len(history) != 1 {
		t.Fatalf("no-op patches consumed %d history versions, want 0 — the owner's undo path "+
			"was spent on writes that changed nothing", len(history)-1)
	}
	if history[0].Content["text"] != original {
		t.Fatalf("the retained version is no longer the text a restore would bring back: %+v", history[0].Content)
	}
	current, err := f.api.foldLessonsDTO(role, taskType)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != edited {
		t.Fatalf("no-op patches must leave the doc alone, got %q", current.Text)
	}
}

func TestNoOpPatchInsightConsumesNoHistoryVersion(t *testing.T) {
	f := newHistoryFixture(t)
	const role = seedRoleAssistant
	const original = "INSIGHT: prefer a slow correct split to a fast wrong one.\n"
	const edited = "INSIGHT: prefer a slow correct split to a fast wrong merge.\n"
	if err := f.api.dal.PutInsight(Insight{RoleKey: role, Text: original}); err != nil {
		t.Fatalf("seed insight: %v", err)
	}
	patch := func(body any) (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandlePatchInsightApiInsightRoleKeyPatchPost(rec,
			f.req(http.MethodPost, "/api/insight/"+role+"/patch", body), role)
		var data map[string]any
		if rec.Body.Len() > 0 {
			_ = json.Unmarshal(rec.Body.Bytes(), &data)
		}
		return rec.Code, data
	}

	status, data := patch(map[string]any{
		"edits": []any{map[string]any{"old": "wrong one.", "new": "wrong merge."}},
	})
	if status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	if history := f.list("insight", role); len(history) != 1 || history[0].Content["text"] != original {
		t.Fatalf("control: a real patch must retain the text it replaced, got %+v", history)
	}

	for i := 0; i < 3; i++ {
		status, data := patch(noOpProbe("fast wrong merge"))
		if status != http.StatusOK {
			t.Fatalf("no-op probe %d must answer 200, got %d: %v", i, status, data)
		}
		assertNoOpReceipt(t, data, edited)
	}

	history := f.list("insight", role)
	if len(history) != 1 {
		t.Fatalf("no-op patches consumed %d insight history versions, want 0", len(history)-1)
	}
	if history[0].Content["text"] != original {
		t.Fatalf("the retained version is no longer the text a restore would bring back: %+v", history[0].Content)
	}
	current, err := f.api.foldInsightDTO(role)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != edited {
		t.Fatalf("no-op patches must leave the doc alone, got %q", current.Text)
	}
}

func TestNoOpPatchTaskLearningsConsumesNoHistoryVersion(t *testing.T) {
	f := newHistoryFixture(t)
	const original = "LEARNING: the fixture seeds one manual per test.\n"
	const edited = "LEARNING: the fixture seeds exactly one manual per test.\n"
	key := seedManualWithLearnings(t, f.api, original)

	status, data := patchLearnings(t, f.api, key, map[string]any{
		"edits": []any{edit("seeds one manual", "seeds exactly one manual")},
	})
	if status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	if history := f.list(docKindTaskManualLearnings, key); len(history) != 1 ||
		history[0].Content["learnings"] != original {
		t.Fatalf("control: a real patch must retain the learnings it replaced, got %+v", history)
	}
	before, err := f.api.dal.GetTaskManual(key)
	if err != nil || before == nil {
		t.Fatalf("read manual: %+v %v", before, err)
	}

	for i := 0; i < 3; i++ {
		status, data := patchLearnings(t, f.api, key, noOpProbe("seeds exactly one manual"))
		if status != http.StatusOK {
			t.Fatalf("no-op probe %d must answer 200, got %d: %v", i, status, data)
		}
		assertNoOpReceipt(t, data, edited)
	}

	history := f.list(docKindTaskManualLearnings, key)
	if len(history) != 1 {
		t.Fatalf("no-op patches consumed %d learnings history versions, want 0", len(history)-1)
	}
	if history[0].Content["learnings"] != original {
		t.Fatalf("the retained version is no longer the text a restore would bring back: %+v", history[0].Content)
	}
	if got := storedLearnings(t, f.api, key); got != edited {
		t.Fatalf("no-op patches must leave the learnings alone, got %q", got)
	}
	// updated_ts is the manual's "when did this last change" signal — a patch
	// that changed nothing must not move it either.
	after, err := f.api.dal.GetTaskManual(key)
	if err != nil || after == nil {
		t.Fatalf("re-read manual: %+v %v", after, err)
	}
	if after.UpdatedTS != before.UpdatedTS {
		t.Fatalf("a no-op patch moved updated_ts from %v to %v", before.UpdatedTS, after.UpdatedTS)
	}
}
