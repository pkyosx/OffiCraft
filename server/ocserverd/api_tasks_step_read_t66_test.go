package main

// T-66 — the step-note split: get_task became a SUMMARY that says so, and
// get_task_step became the one read that serves a note's text.
//
// Owner card rc-4c8065fb30a5, verbatim:「整個拿掉，做在組裝票那一層（九個介面
// 一起瘦），座艙改成點開才抓」.
//
// WHAT THESE PIN, and why each one needs its own case:
//
//   * The note text is GONE FROM THE WIRE, not merely blank. Asserting on the
//     decoded struct could not tell those apart — the field would simply be
//     absent from a Go type that no longer declares it — so the removal is
//     asserted against the RAW JSON of a task whose step really does have a
//     note. A `note` key reappearing on the step (however it got there) fails
//     here.
//   * The response SAYS what it is. The AC is verbatim「成功的回應不得看起來像
//     完整的 task」, so a 200 that looks complete is the defect; detail_level /
//     notes_included are asserted on the get_task exit AND on one of the other
//     eight exits that share newTaskDTO, because doing it in the builder is
//     what makes the other eight true.
//   * The size is the SUBSTITUTE, so it must be exact and it must survive
//     multi-byte text. Rune-counting is the whole contract: a byte count would
//     over-report every Chinese handover note in this system.
//   * A step is only reachable through ITS OWN task. Without that check the
//     task_id in the path is decoration and any caller who can name one task
//     reads every step in the database.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// t66Note is deliberately multi-byte and deliberately longer in bytes than in
// runes: len() and utf8.RuneCountInString() disagree on it, so a size computed
// the wrong way cannot pass by coincidence.
const t66Note = "跑完 conformance；下一步接前端 i18n，備註本身要夠長才量得出差異"

// getTaskStepRaw calls the single-step read and returns the recorder, so each
// case asserts its own status and can look at the raw bytes.
func getTaskStepRaw(t *testing.T, api *apiServer, taskID, stepID, caller string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskStepApiTasksTaskIdStepsStepIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID+"/steps/"+stepID, nil, caller, "agent"),
		taskID, stepID)
	return rec
}

// t66Fixture builds one task with two steps and writes t66Note onto the first.
func t66Fixture(t *testing.T, api *apiServer, executor string) (taskDTO, string, string) {
	t.Helper()
	task := createAdHocTask(t, api, executor)
	view := submitPlan(t, api, task.ID, executor, []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
	})
	if rec := writeStepNote(t, api, task.ID, view.Steps[0].ID, executor, t66Note); rec.Code != http.StatusOK {
		t.Fatalf("seed note: %d %s", rec.Code, rec.Body.String())
	}
	return view, view.Steps[0].ID, view.Steps[1].ID
}

// TestGetTaskDeclaresItselfASummaryAndCarriesNoNoteText is the AC:「成功的回應
// 不得看起來像完整的 task」. It reads the RAW body, because the point is what is
// on the WIRE — a Go struct that no longer declares Note would decode a
// note-bearing payload just as happily.
func TestGetTaskDeclaresItselfASummaryAndCarriesNoNoteText(t *testing.T) {
	api := newTasksTestServer(t)
	view, notedStep, _ := t66Fixture(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+view.ID, nil, "m-exec", "agent"), view.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// 1. The text is not on the wire at all. The note itself is distinctive,
	//    so this catches it wherever it might be smuggled — including inside a
	//    step object under a different key.
	if strings.Contains(body, t66Note) {
		t.Fatalf("get_task carried the step note TEXT; the whole point of T-66 is that it does not: %s", body)
	}
	// 2. And not as a declared-but-empty field either, which is the failure
	//    mode the owner's「整個拿掉」rules out: an always-"" note reads to every
	//    existing client as "this step has no note".
	var raw struct {
		DetailLevel   *string          `json:"detail_level"`
		NotesIncluded *bool            `json:"notes_included"`
		Steps         []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Steps) != 2 {
		t.Fatalf("expected 2 step rows, got %d", len(raw.Steps))
	}
	for i, st := range raw.Steps {
		if _, present := st["note"]; present {
			t.Fatalf("step %d still declares a `note` key on the wire — removed from the SCHEMA "+
				"on purpose (owner rc-4c8065fb30a5), because a declared always-empty field is "+
				"read by every existing client as 'no note': %v", i, st)
		}
	}
	// 3. The response says what it is, rather than leaving the caller to infer
	//    it from which fields happen to be present.
	if raw.DetailLevel == nil || *raw.DetailLevel != "summary" {
		t.Fatalf("get_task must declare detail_level=summary, got %v", raw.DetailLevel)
	}
	if raw.NotesIncluded == nil || *raw.NotesIncluded != false {
		t.Fatalf("get_task must declare notes_included=false, got %v", raw.NotesIncluded)
	}
	// 4. The substitute is EXACT and rune-counted. This is the number a caller
	//    decides on, so an off-by-a-multibyte-character here is the whole
	//    field being useless.
	var sized *map[string]any
	for i := range raw.Steps {
		if raw.Steps[i]["id"] == notedStep {
			sized = &raw.Steps[i]
		}
	}
	if sized == nil {
		t.Fatalf("the noted step is missing from the step list")
	}
	wantRunes := float64(utf8.RuneCountInString(t66Note))
	if (*sized)["note_size_chars"] != wantRunes {
		t.Fatalf("note_size_chars = %v, want %v runes (NOT %d bytes — every handover note "+
			"in this system is Chinese, so a byte count over-reports all of them)",
			(*sized)["note_size_chars"], wantRunes, len(t66Note))
	}
}

// TestTaskWriteFacesCarryNoStepsAtAll: T-66's ruling was to slim「在組裝票那一
// 層」so that nine responses got thinner at once — a per-handler fix would have
// left the other eight lying. This test stood on one of those eight,
// set_task_deps, and asserted that its task payload declared itself a summary
// with the step notes left out.
//
// 🔴 T-91 TOOK THAT FACE OFF THE SHARED BUILDER ENTIRELY, so this test's
// premise moves rather than disappears. The eight task-driving writes now
// answer taskWriteReceiptDTO, which carries no step ROWS at all — only
// progress_done / progress_total, two integers standing in for fifteen fields
// per step. That makes the absent note structural instead of declared: there is
// no place on this shape for a note to hide, so `notes_included` and
// `detail_level` have nothing left to be honest about and are gone with the
// rows. get_task, the READ, still declares itself exactly as before — pinned in
// the test above, which is now the only place that self-description exists.
func TestTaskWriteFacesCarryNoStepsAtAll(t *testing.T) {
	api := newTasksTestServer(t)
	view, _, _ := t66Fixture(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandleSetTaskDepsApiTasksTaskIdDepsPost(rec,
		taskReq(t, "POST", "/api/tasks/"+view.ID+"/deps",
			map[string]any{"blocked_by": []string{}}, "m-exec", "agent"), view.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("set_task_deps: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, t66Note) {
		t.Fatalf("set_task_deps still carries the step note text: %s", body)
	}
	var raw struct {
		Steps         []map[string]any `json:"steps"`
		DetailLevel   *string          `json:"detail_level"`
		NotesIncluded *bool            `json:"notes_included"`
		ProgressDone  *int             `json:"progress_done"`
		ProgressTotal *int             `json:"progress_total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw.Steps != nil || raw.DetailLevel != nil || raw.NotesIncluded != nil {
		t.Fatalf("a write receipt must carry no step rows, and nothing to declare "+
			"about them: %s", body)
	}
	// Anti-vacuity: the pair that REPLACED the rows has to actually be there and
	// actually describe this plan — otherwise "no steps" would also be satisfied
	// by a handler that answered nothing at all.
	if raw.ProgressTotal == nil || *raw.ProgressTotal != len(view.Steps) {
		t.Fatalf("progress_total must report the plan the fixture built (%d steps), got %v",
			len(view.Steps), raw.ProgressTotal)
	}
	if raw.ProgressDone == nil {
		t.Fatalf("progress_done must ride beside progress_total: %s", body)
	}
}

// TestGetTaskStepServesTheWholeNoteAndSaysSo is the other half: the text has to
// be reachable, in full, through the read that replaces it.
func TestGetTaskStepServesTheWholeNoteAndSaysSo(t *testing.T) {
	api := newTasksTestServer(t)
	view, notedStep, blankStep := t66Fixture(t, api, "m-exec")

	rec := getTaskStepRaw(t, api, view.ID, notedStep, "m-exec")
	if rec.Code != http.StatusOK {
		t.Fatalf("get_task_step: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[taskStepDetailDTO](t, rec)
	if got.Note != t66Note {
		t.Fatalf("note = %q, want %q", got.Note, t66Note)
	}
	if got.DetailLevel != "full" {
		t.Fatalf("detail_level = %q, want full — it is what lets a caller tell this "+
			"response apart from get_task's step row", got.DetailLevel)
	}
	if got.NoteSizeChars != utf8.RuneCountInString(t66Note) || got.NoteCapChars <= 0 {
		t.Fatalf("size/cap pair = %d/%d, want %d and a positive ceiling",
			got.NoteSizeChars, got.NoteCapChars, utf8.RuneCountInString(t66Note))
	}
	if got.ID != notedStep || got.TaskID != view.ID {
		t.Fatalf("answered step %q of task %q, asked for %q of %q",
			got.ID, got.TaskID, notedStep, view.ID)
	}
	// 0 means "genuinely no note", not "withheld" — the sibling proves the
	// number is measured rather than defaulted.
	blank := decodeBody[taskStepDetailDTO](t, getTaskStepRaw(t, api, view.ID, blankStep, "m-exec"))
	if blank.Note != "" || blank.NoteSizeChars != 0 {
		t.Fatalf("a step with no note must read back as \"\"/0, got %q/%d",
			blank.Note, blank.NoteSizeChars)
	}
}

// TestGetTaskStepAnswersOnlyTheStep: it must not quietly become a second
// get_task. The task's own fields are what the split exists to stop paying for.
func TestGetTaskStepAnswersOnlyTheStep(t *testing.T) {
	api := newTasksTestServer(t)
	view, notedStep, _ := t66Fixture(t, api, "m-exec")

	rec := getTaskStepRaw(t, api, view.ID, notedStep, "m-exec")
	if rec.Code != http.StatusOK {
		t.Fatalf("get_task_step: %d %s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Task-shaped keys, each of which would mean the ticket came along for the
	// ride. `steps` would mean the siblings did.
	for _, forbidden := range []string{
		"steps", "deps", "blocking", "artifacts", "title", "description",
		"progress_done", "progress_total", "priority", "executor_id", "task_no",
	} {
		if _, present := raw[forbidden]; present {
			t.Fatalf("the single-step read carries %q — it answers ONE STEP and nothing "+
				"else; dragging the ticket along reinstates the cost this split removes: %v",
				forbidden, raw)
		}
	}
	// And it does carry the step's own fields, so the check above is not
	// passing because the payload is empty.
	for _, required := range []string{
		"id", "task_id", "order_idx", "name", "dod", "status", "note",
		"note_size_chars", "note_cap_chars", "waiting_reason", "is_gate",
		"parallel_group", "reply_card_id", "reply_card_status", "detail_level",
	} {
		if _, present := raw[required]; !present {
			t.Fatalf("the single-step read is missing %q: %v", required, raw)
		}
	}
}

// TestGetTaskStepForeignStepIs404 is the ownership guard. Without it the
// task_id in the path is decoration: GetTaskStep(step_id) alone would serve any
// step in the database to anyone who can name any task.
//
// 404, not 403, and not 200: from THIS task's point of view that step is
// absent. The body is checked too — a refusal that echoed the other task's note
// would leak exactly what the refusal exists to withhold.
func TestGetTaskStepForeignStepIs404(t *testing.T) {
	api := newTasksTestServer(t)
	mine, _, _ := t66Fixture(t, api, "m-exec")
	theirs, theirNoted, _ := t66Fixture(t, api, "m-other")

	rec := getTaskStepRaw(t, api, mine.ID, theirNoted, "m-exec")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a step belonging to another task must 404, got %d %s — otherwise the "+
			"task_id in the path means nothing and one task id reads every step in the "+
			"database", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), t66Note) {
		t.Fatalf("the 404 body leaked the other task's note text: %s", rec.Body.String())
	}
	// Positive control: the SAME step id is readable through ITS OWN task, so
	// the 404 above is about ownership and not about a step that does not exist.
	if rec := getTaskStepRaw(t, api, theirs.ID, theirNoted, "m-exec"); rec.Code != http.StatusOK {
		t.Fatalf("the same step must read 200 through its own task, got %d %s",
			rec.Code, rec.Body.String())
	}
}

// TestGetTaskStepUnknownIdsAre404 covers the two plain misses, so the ownership
// case above is not the only thing keeping this handler from 500-ing.
func TestGetTaskStepUnknownIdsAre404(t *testing.T) {
	api := newTasksTestServer(t)
	view, notedStep, _ := t66Fixture(t, api, "m-exec")

	if rec := getTaskStepRaw(t, api, "t-does-not-exist", notedStep, "m-exec"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task: %d %s", rec.Code, rec.Body.String())
	}
	if rec := getTaskStepRaw(t, api, view.ID, "step-does-not-exist", "m-exec"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown step: %d %s", rec.Code, rec.Body.String())
	}
}

// TestGetTaskStepIsAReadNotAWrite: the floor is GET /api/tasks/{task_id}'s, so
// an agent that does NOT execute this task still reads it — exactly as it
// already could through get_task. This is asserted rather than assumed because
// the three neighbouring routes on this same path segment are all executor-
// gated writes, and copying their gate onto a read would make the note
// unreachable through the one tool meant to serve it.
func TestGetTaskStepIsAReadNotAWrite(t *testing.T) {
	api := newTasksTestServer(t)
	view, notedStep, _ := t66Fixture(t, api, "m-exec")

	rec := getTaskStepRaw(t, api, view.ID, notedStep, "m-bystander")
	if rec.Code != http.StatusOK {
		t.Fatalf("a non-executor agent must read a step exactly as it reads the task, got %d %s",
			rec.Code, rec.Body.String())
	}
	if got := decodeBody[taskStepDetailDTO](t, rec).Note; got != t66Note {
		t.Fatalf("bystander read note = %q, want %q", got, t66Note)
	}
}
