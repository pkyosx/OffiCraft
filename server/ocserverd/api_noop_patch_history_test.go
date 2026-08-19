package main

// api_noop_patch_history_test.go — a patch that changes NOTHING must not
// consume a document-history version.
//
// The shape of the defect these pin. All three anchor-patch seams that existed
// when this file was written (patch_lessons, patch_insight,
// patch_task_learnings) called
// SaveWithDocumentHistory unconditionally, including when ApplyDocEdits
// reported applied == 0 — every anchor matched, every splice was the identity,
// the resulting text byte-identical to what was already stored. The write is
// harmless. The RETENTION is not: document history keeps only the three most
// recent versions per document (dal.go), so three such patches evict every
// version the owner could have restored and replace them with three copies of
// the text that is already live.
//
// The SECOND way in, which the applied == 0 gate does not close. That gate asks
// ApplyDocEdits how many edits changed something, and ApplyDocEdits counts an
// edit as applied when it changed the INTERMEDIATE result it was handed — not
// when it moved the final document away from where the document started. A
// batch of two uniquely-anchored edits that undo one another (anchor → middle,
// then middle → anchor) therefore reports applied == 2 while leaving the stored
// text byte-identical, sails through `applied > 0`, and lands exactly the write
// and exactly the retention the gate above exists to prevent. Same harm, same
// seams, different branch — and the cheapest batch to reach it by
// accident is an agent that edits a line and then reverts it inside one call.
//
// patch_task_sop (T-1667) is the fourth seam and it shipped with neither gate,
// so it burned an SOP version on a batch that changed nothing exactly as the
// first three did. It is covered here for the same reason they are: a manual's
// SOP and its learnings are two INDEPENDENT retention series on one type_key
// (T-1f39), so a leak on the SOP face is invisible to every learnings case
// above.
//
// The tests below come in those two flavours and the flavour is in the name:
// NoOpPatch* send a single old == new probe (applied == 0), CancellingBatch*
// send the two-edit batch that cancels (applied != 0). Neither may cost a
// version.
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

// cancellingBatch is the batch that reaches the same write through the other
// branch: two well-formed edits, each anchored uniquely at the moment it runs,
// whose composition is the identity. The first rewrites anchor → middle, the
// second rewrites that middle straight back, so every individual edit changed
// the text it was handed (applied == 2) and the document ends where it began.
//
// middle must not occur in the doc already, or the second edit's anchor is
// ambiguous and the batch is refused for a reason that has nothing to do with
// what is being pinned here.
func cancellingBatch(anchor, middle string) map[string]any {
	return map[string]any{"edits": []any{
		map[string]any{"old": anchor, "new": middle},
		map[string]any{"old": middle, "new": anchor},
	}}
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

func TestNoOpPatchTaskSopConsumesNoHistoryVersion(t *testing.T) {
	f := newHistoryFixture(t)
	const original = "## SOP\n1. 先讀 spec\n"
	const edited = "## SOP\n1. 先讀 spec 再改 handler\n"
	key := seedManualWithSop(t, f.api, original)

	status, data := patchSop(t, f.api, key, map[string]any{
		"edits": []any{edit("先讀 spec", "先讀 spec 再改 handler")},
	})
	if status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	if history := f.list(docKindTaskManualSop, key); len(history) != 1 ||
		history[0].Content["sop_md"] != original {
		t.Fatalf("control: a real patch must retain the sop_md it replaced, got %+v", history)
	}
	before, err := f.api.dal.GetTaskManual(key)
	if err != nil || before == nil {
		t.Fatalf("read manual: %+v %v", before, err)
	}

	for i := 0; i < 3; i++ {
		status, data := patchSop(t, f.api, key, noOpProbe("先讀 spec 再改 handler"))
		if status != http.StatusOK {
			t.Fatalf("no-op probe %d must answer 200, got %d: %v", i, status, data)
		}
		assertNoOpReceipt(t, data, edited)
	}

	history := f.list(docKindTaskManualSop, key)
	if len(history) != 1 {
		t.Errorf("no-op patches consumed %d sop history versions, want 0", len(history)-1)
	}
	if len(history) == 0 || history[0].Content["sop_md"] != original {
		t.Errorf("the retained version is no longer the text a restore would bring back: %+v", history)
	}
	if got := storedSop(t, f.api, key); got != edited {
		t.Errorf("no-op patches must leave the sop alone, got %q", got)
	}
	// The learnings series shares this manual's type_key and must not have been
	// touched at all — a stream named by mistake would show up here.
	if history := f.list(docKindTaskManualLearnings, key); len(history) != 0 {
		t.Errorf("the SOP face retained %d learnings versions, want 0: %+v", len(history), history)
	}
	after, err := f.api.dal.GetTaskManual(key)
	if err != nil || after == nil {
		t.Fatalf("re-read manual: %+v %v", after, err)
	}
	if after.UpdatedTS != before.UpdatedTS {
		t.Errorf("a no-op patch moved updated_ts from %v to %v", before.UpdatedTS, after.UpdatedTS)
	}
}

// ── the cancelling-batch branch: applied != 0, document unchanged ────────────
//
// None of the four below assert on applied_edits. What a cancelling batch
// ought to REPORT is a live question (2, because two splices ran, or 0, because
// nothing moved) and it is not the question these pin — pinning it here would
// nail down a receipt shape before anyone chose one. What they pin is the part
// that is not a matter of taste: a document nobody changed does not cost a
// version.

func TestCancellingBatchPatchLessonsConsumesNoHistoryVersion(t *testing.T) {
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
	// replaced. It is also what stops everything below passing vacuously: if the
	// handler were never reached, or list_document_history stopped returning
	// rows, this is the line that goes red first, and "history did not grow"
	// never gets the chance to be true for the wrong reason.
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

	// Three cancelling batches — one more than the doc has slots, so any
	// per-call retention evicts `original` outright.
	for i := 0; i < 3; i++ {
		status, data := patch(cancellingBatch("provably unique", "demonstrably unique"))
		if status != http.StatusOK {
			t.Fatalf("cancelling batch %d must answer 200, got %d: %v", i, status, data)
		}
	}

	// The premise, asserted before the retention claim: these batches really did
	// leave the document where they found it. A doc that CHANGED has every right
	// to consume a version, so without this the assertion below pins nothing.
	current, err := f.api.foldLessonsDTO(role, taskType)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != edited {
		t.Fatalf("cancelling batches must leave the doc alone, got %q want %q", current.Text, edited)
	}

	// Errorf, not Fatalf: the version COUNT and WHICH version survived are two
	// independent claims, and a reader who only ever sees the first one go red
	// cannot tell whether the second still holds.
	history = f.list("lessons", role+"::"+taskType)
	if len(history) != 1 {
		t.Errorf("cancelling batches consumed %d history versions, want 0 — 'some edit changed "+
			"the text it was handed' is not the same question as 'the document changed'", len(history)-1)
	}
	if len(history) == 0 || history[0].Content["text"] != original {
		t.Errorf("the retained version is no longer the text a restore would bring back: %+v", history)
	}
}

func TestCancellingBatchPatchInsightConsumesNoHistoryVersion(t *testing.T) {
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
		status, data := patch(cancellingBatch("fast wrong merge", "fast mistaken merge"))
		if status != http.StatusOK {
			t.Fatalf("cancelling batch %d must answer 200, got %d: %v", i, status, data)
		}
	}

	current, err := f.api.foldInsightDTO(role)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != edited {
		t.Fatalf("cancelling batches must leave the doc alone, got %q want %q", current.Text, edited)
	}

	history := f.list("insight", role)
	if len(history) != 1 {
		t.Errorf("cancelling batches consumed %d insight history versions, want 0", len(history)-1)
	}
	if len(history) == 0 || history[0].Content["text"] != original {
		t.Errorf("the retained version is no longer the text a restore would bring back: %+v", history)
	}
}

func TestCancellingBatchPatchTaskLearningsConsumesNoHistoryVersion(t *testing.T) {
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
	// Read updated_ts AFTER the control edit — the control is supposed to move
	// it, so this is the value a cancelling batch must leave alone.
	before, err := f.api.dal.GetTaskManual(key)
	if err != nil || before == nil {
		t.Fatalf("read manual: %+v %v", before, err)
	}

	for i := 0; i < 3; i++ {
		status, data := patchLearnings(t, f.api, key,
			cancellingBatch("exactly one manual", "precisely one manual"))
		if status != http.StatusOK {
			t.Fatalf("cancelling batch %d must answer 200, got %d: %v", i, status, data)
		}
	}

	if got := storedLearnings(t, f.api, key); got != edited {
		t.Fatalf("cancelling batches must leave the learnings alone, got %q want %q", got, edited)
	}

	// Errorf, not Fatalf: retention and updated_ts are independent claims about
	// this seam, and a Fatalf on the first hides whether the second ever held.
	history := f.list(docKindTaskManualLearnings, key)
	if len(history) != 1 {
		t.Errorf("cancelling batches consumed %d learnings history versions, want 0", len(history)-1)
	}
	if len(history) == 0 || history[0].Content["learnings"] != original {
		t.Errorf("the retained version is no longer the text a restore would bring back: %+v", history)
	}
	// updated_ts is the manual's "when did this last change" signal — a batch
	// that changed nothing must not move it either.
	after, err := f.api.dal.GetTaskManual(key)
	if err != nil || after == nil {
		t.Fatalf("re-read manual: %+v %v", after, err)
	}
	if after.UpdatedTS != before.UpdatedTS {
		t.Errorf("a cancelling batch moved updated_ts from %v to %v", before.UpdatedTS, after.UpdatedTS)
	}
}

func TestCancellingBatchPatchTaskSopConsumesNoHistoryVersion(t *testing.T) {
	f := newHistoryFixture(t)
	const original = "## SOP\n1. 先讀 spec\n"
	const edited = "## SOP\n1. 先讀 spec 再改 handler\n"
	key := seedManualWithSop(t, f.api, original)

	status, data := patchSop(t, f.api, key, map[string]any{
		"edits": []any{edit("先讀 spec", "先讀 spec 再改 handler")},
	})
	if status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	if history := f.list(docKindTaskManualSop, key); len(history) != 1 ||
		history[0].Content["sop_md"] != original {
		t.Fatalf("control: a real patch must retain the sop_md it replaced, got %+v", history)
	}
	// Read updated_ts AFTER the control edit — the control is supposed to move it,
	// so this is the value a cancelling batch must leave alone.
	before, err := f.api.dal.GetTaskManual(key)
	if err != nil || before == nil {
		t.Fatalf("read manual: %+v %v", before, err)
	}

	for i := 0; i < 3; i++ {
		status, data := patchSop(t, f.api, key,
			cancellingBatch("再改 handler", "再改 routes"))
		if status != http.StatusOK {
			t.Fatalf("cancelling batch %d must answer 200, got %d: %v", i, status, data)
		}
	}

	if got := storedSop(t, f.api, key); got != edited {
		t.Fatalf("cancelling batches must leave the sop alone, got %q want %q", got, edited)
	}

	// Errorf, not Fatalf: retention and updated_ts are independent claims about
	// this seam, and a Fatalf on the first hides whether the second ever held.
	history := f.list(docKindTaskManualSop, key)
	if len(history) != 1 {
		t.Errorf("cancelling batches consumed %d sop history versions, want 0 — 'some edit changed "+
			"the text it was handed' is not the same question as 'the document changed'", len(history)-1)
	}
	if len(history) == 0 || history[0].Content["sop_md"] != original {
		t.Errorf("the retained version is no longer the text a restore would bring back: %+v", history)
	}
	after, err := f.api.dal.GetTaskManual(key)
	if err != nil || after == nil {
		t.Fatalf("re-read manual: %+v %v", after, err)
	}
	if after.UpdatedTS != before.UpdatedTS {
		t.Errorf("a cancelling batch moved updated_ts from %v to %v", before.UpdatedTS, after.UpdatedTS)
	}
}
