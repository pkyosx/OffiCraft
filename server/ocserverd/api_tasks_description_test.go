package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// ── T-e271 任務描述可編輯 guards ─────────────────────────────────────────────
//
// Every case below reads the value back out through a real read path, never
// merely asserting a status code: the ticket exists because a documented
// capability ("agents can edit the task description separately", said in
// update_step_note's own tool description) turned out to name nothing at all,
// and a test that only checked for a 200 would have been just as happy against
// that nothing.

func writeTaskDescription(t *testing.T, api *apiServer, taskID, caller, scope string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/description", body, caller, scope),
		taskID)
	return rec
}

// assertErrorEnvelope pins WHY a request was refused, not merely that it was.
//
// The DoD for this ticket asks for the reason explicitly, and the reason is the
// part that rots: a 403 is emitted by the authz gate, but a 403 could equally
// arrive from a future guard added above it, and a status-only assertion would
// keep passing while the test silently stopped covering the rule it names. The
// code comes from the unified envelope (server.go writeError) and the message
// is matched as a substring so wording may change around the claim.
func assertErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, code, contains string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("refusal is not the unified error envelope (%d %s): %v",
			rec.Code, rec.Body.String(), err)
	}
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q (body %s)", body.Error.Code, code,
			rec.Body.String())
	}
	if !strings.Contains(body.Error.Message, contains) {
		t.Fatalf("error message = %q, want it to contain %q",
			body.Error.Message, contains)
	}
}

// readTask re-reads the whole task through get_task — used to ASSERT a fixture
// really is what a test claims it is.
func readTask(t *testing.T, api *apiServer, taskID string) taskDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "owner", "owner"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskDTO](t, rec)
}

// readTaskDescription re-reads the description through get_task — the path the
// cockpit and every agent actually use, not the DAL.
func readTaskDescription(t *testing.T, api *apiServer, taskID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskDTO](t, rec).Description
}

func listTaskDescriptionHistory(t *testing.T, api *apiServer, taskID, caller, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		taskReq(t, "GET", "/api/document-history/task_description/"+taskID, nil, caller, scope),
		docKindTaskDescription, taskID)
	return rec
}

// terminateTask closes a task through the owner's terminate route, so the
// terminal-state cases below face a genuinely closed task rather than a
// hand-poked status column.
func terminateTask(t *testing.T, api *apiServer, taskID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/terminate", nil, "owner", "owner"),
		taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}
}

// TestTaskDescriptionRoundTripsThroughTheTaskView is the core assertion: the
// corrected wording is what the next reader sees. Dropping the description
// assignment from the handler, or the column from SetTaskDescriptionOn, reddens
// this — both are ways for the write to answer 200 and change nothing.
func TestTaskDescriptionRoundTripsThroughTheTaskView(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	const corrected = "更正:這張票要的是「描述可編輯」,不是步驟備註"

	rec := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": corrected})
	if rec.Code != http.StatusOK {
		t.Fatalf("write description: %d %s", rec.Code, rec.Body.String())
	}
	// T-91: the write receipt reports the description as a SIZE and a HASH, not
	// as the text — the caller sent that text one line ago. The hash is the
	// stronger form of the same claim this line always made ("what landed is
	// what I sent"), and it is on this face specifically because this write
	// TRIMS while create_task does not, so a caller cannot assume the two agree.
	receipt := decodeBody[taskWriteReceiptDTO](t, rec)
	if receipt.DescriptionSha256 != receiptSha256(corrected) {
		t.Fatalf("response description sha256 = %q, want the hash of %q",
			receipt.DescriptionSha256, corrected)
	}
	if receipt.DescriptionSizeChars != utf8.RuneCountInString(corrected) {
		t.Fatalf("response description_size_chars = %d, want %d RUNES",
			receipt.DescriptionSizeChars, utf8.RuneCountInString(corrected))
	}
	if got := readTaskDescription(t, api, task.ID); got != corrected {
		t.Fatalf("description read back = %q, want %q", got, corrected)
	}
}

// Ruling 1: the EXECUTOR may edit; the CREATOR earns no standing from having
// created the task. Owner explicitly excluded the creator, so the negative case
// has to be a REAL creator — not merely some member who is not the executor.
//
// ⚠️ This test previously did NOT do that. It created the task under an OWNER
// token and then had an unrelated member try to edit, which makes the caller a
// bystander whose 403 says nothing about creators at all: it would have passed
// just as happily against a handler that admitted creators. The name promised
// more than the fixture delivered. The fixture below builds the real thing and
// then ASSERTS it, so it cannot quietly decay back into a bystander test.
//
// How a creator stops being the executor, using only real routes: m-creator
// creates an ad-hoc task for ITSELF (a plain 正職 may not name another member),
// then reassigns it away. CreatorID stays m-creator; ExecutorID becomes m-exec.
func TestTaskDescriptionCreatorIsNotTheEditor(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-creator", "Creator", KindStaff)
	putActiveMember(t, api, "m-exec", "Executor", KindStaff)

	task := createAdHocTask(t, api, "m-creator")
	// The OWNER performs the handover: a plain 正職 may not reassign to another
	// member (that is its own 403, unrelated to this ticket). Who moved the task
	// is immaterial here — what matters is the resulting row, asserted below.
	if rec := reassign(t, api, task.ID, memberTarget("m-exec"),
		"owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}

	// The fixture IS the premise — assert it rather than assume it. Without
	// this the test silently reverts to "some other member" the moment the
	// create or reassign semantics move.
	after := readTask(t, api, task.ID)
	if after.CreatorID != "m-creator" {
		t.Fatalf("fixture broken: creator = %q, want m-creator", after.CreatorID)
	}
	if after.ExecutorID != "m-exec" {
		t.Fatalf("fixture broken: executor = %q, want m-exec", after.ExecutorID)
	}

	// THE case owner ruled on: the creator, who is no longer the executor, is
	// refused — and refused for the RIGHT REASON, not merely "some 4xx".
	rec := writeTaskDescription(t, api, task.ID, "m-creator", "agent",
		map[string]any{"description": "creator rewrite"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("creator status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec, "forbidden", executorGuardRefusal)
	if got := readTaskDescription(t, api, task.ID); got != "" {
		t.Fatalf("refused write still landed: %q", got)
	}

	// A member who is NEITHER creator nor executor is refused the same way —
	// so the 403 above is not an artefact of some creator-specific branch.
	putActiveMember(t, api, "m-stranger", "Stranger", KindStaff)
	rec = writeTaskDescription(t, api, task.ID, "m-stranger", "agent",
		map[string]any{"description": "stranger rewrite"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger status = %d, want 403", rec.Code)
	}
	assertErrorEnvelope(t, rec, "forbidden", executorGuardRefusal)

	// Positive controls: the route is not simply broken for everyone.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "executor rewrite"}).Code; got != http.StatusOK {
		t.Fatalf("executor status = %d, want 200", got)
	}
	putMemberRow(t, api, "m-mira", KindStaff, adminRoleKey)
	if got := writeTaskDescription(t, api, task.ID, "m-mira", "agent",
		map[string]any{"description": "admin rewrite"}).Code; got != http.StatusOK {
		t.Fatalf("admin status = %d, want 200", got)
	}
	if got := writeTaskDescription(t, api, task.ID, "owner", "owner",
		map[string]any{"description": "owner rewrite"}).Code; got != http.StatusOK {
		t.Fatalf("owner status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "owner rewrite" {
		t.Fatalf("description read back = %q, want owner rewrite", got)
	}
}

// Ruling 2: a CLOSED task's description is still editable, and by the same
// people. The artifact-set control in the same case is what makes this a
// statement about a REASONED difference rather than an oversight — the two
// writes face the same terminal task and only one of them is frozen.
func TestTaskDescriptionEditableOnAClosedTaskWhileArtifactsAreFrozen(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	terminateTask(t, api, task.ID)

	const corrected = "結案後才發現票面寫錯,照樣改得動"
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": corrected}).Code; got != http.StatusOK {
		t.Fatalf("closed-task description status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != corrected {
		t.Fatalf("closed-task description read back = %q, want %q", got, corrected)
	}

	// The control: the SAME caller on the SAME closed task cannot touch the
	// deliverable set. If someone ever "harmonises" the two by adding a
	// terminal gate here, the description case above goes red; if someone
	// removes the artifact freeze instead, this half goes red.
	rec := httptest.NewRecorder()
	api.HandleAddTaskArtifactApiTasksTaskIdArtifactPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/artifact",
			map[string]any{"kind": "link", "label": "pr", "url": "https://example.invalid/pr/1"},
			"m-exec", "agent"),
		task.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("closed-task artifact status = %d, want 409 (the frozen set)", rec.Code)
	}
}

// Ruling 3: the trail rides the ALREADY-shipped document-history mechanism, so
// the generic list route serves it. The retained revision must be the text the
// write replaced — not the new text, which is the mistake a snapshot taken
// after the write would make.
func TestTaskDescriptionEditRetainsThePreviousTextInSharedHistory(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	for _, text := range []string{"first wording", "second wording", "third wording"} {
		if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
			map[string]any{"description": text}).Code; got != http.StatusOK {
			t.Fatalf("write %q: status %d", text, got)
		}
	}

	rec := listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("list history: %d %s", rec.Code, rec.Body.String())
	}
	history := historyRowsFrom(t, api, docKindTaskDescription, task.ID, "m-exec", "agent", rec)
	// Two revisions, not three: the FIRST write replaced an empty description,
	// and an empty document is nothing to retain (the same rule every other
	// kind gets from its row being absent).
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2: %+v", len(history), history)
	}
	// Newest first (ORDER BY id DESC), and each entry holds the text it
	// replaced.
	if got := history[0].Content["description"]; got != "second wording" {
		t.Fatalf("newest revision = %q, want %q", got, "second wording")
	}
	if got := history[1].Content["description"]; got != "first wording" {
		t.Fatalf("oldest revision = %q, want %q", got, "first wording")
	}
	if history[0].ActorId != "m-exec" {
		t.Fatalf("revision actor = %q, want m-exec", history[0].ActorId)
	}
}

// The partial-update shape (after update_task_manual): an ABSENT field changes
// nothing and versions nothing, while an explicit "" clears. Collapsing the two
// — a `default: ""` on the DTO, say — would let a body that never mentioned the
// description erase it.
func TestTaskDescriptionAbsentFieldIsANoOpButEmptyStringClears(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "the standing text"}).Code; got != http.StatusOK {
		t.Fatalf("seed write status = %d", got)
	}

	// Absent: unchanged, and no new revision.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{}).Code; got != http.StatusOK {
		t.Fatalf("absent-field status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "the standing text" {
		t.Fatalf("absent field changed the text: %q", got)
	}
	// Re-writing the SAME text is a no-op too — it must not spend one of the
	// three retained slots recording that nothing changed.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "the standing text"}).Code; got != http.StatusOK {
		t.Fatalf("same-text status = %d, want 200", got)
	}
	rec := listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")
	if n := len(historyRowsFrom(t, api, docKindTaskDescription, task.ID, "m-exec", "agent", rec)); n != 0 {
		t.Fatalf("no-op writes retained %d revisions, want 0", n)
	}

	// Explicit "": cleared, and THAT is a real change, so it versions.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": ""}).Code; got != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "" {
		t.Fatalf("explicit empty string did not clear: %q", got)
	}
	rec = listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")
	history := historyRowsFrom(t, api, docKindTaskDescription, task.ID, "m-exec", "agent", rec)
	if len(history) != 1 || history[0].Content["description"] != "the standing text" {
		t.Fatalf("clear did not retain what it erased: %+v", history)
	}
}

// TestTaskDescriptionIsTrimmedOnThisDoorToo pins the one behaviour T-646a
// CHANGED on an already-shipped route: the description is trimmed, before it is
// stored AND before the unchanged-value comparison (owner card
// rc-0fb94a25a8a8, 2026-08-16, option ①). Until T-646a this route stored what
// it was given.
//
// 🔴 It lives HERE, on the route's own file, and that placement is the whole
// point of the test. The independent review of T-646a demonstrated the hole by
// measurement: with every trim assertion living on the new update_task door,
// this handler could be reverted to its pre-T-646a inline body — untrimmed
// store, untrimmed compare — and the ENTIRE Go suite stayed green while the
// mutant harness still reported 6/6. The owner's ruling was silently revertible
// on the door the cockpit actually calls. This test is what turns red.
//
// Three claims, and the third is the one no read-back can see:
//
//	① the STORED value is trimmed;
//	② a whitespace-only description therefore trims to "" and CLEARS, which is
//	   the same answer an explicit "" gets;
//	③ the unchanged-value COMPARISON is made on the trimmed value, so a resend
//	   differing only by surrounding whitespace spends none of the three
//	   retained revisions. A handler that trimmed on the way in but compared the
//	   RAW value would store identical text and pass ① and ② unnoticed.
func TestTaskDescriptionIsTrimmedOnThisDoorToo(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "  有前後空白的敘述\t\n"}).Code; got != http.StatusOK {
		t.Fatalf("seed write status = %d", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "有前後空白的敘述" {
		t.Fatalf("① stored description was not trimmed: %q", got)
	}

	before := len(historyRowsFrom(t, api, docKindTaskDescription, task.ID, "m-exec", "agent",
		listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")))

	// ③ same text, different surrounding whitespace ⇒ no change, no revision.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "\n  有前後空白的敘述  "}).Code; got != http.StatusOK {
		t.Fatalf("whitespace-only resend status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "有前後空白的敘述" {
		t.Fatalf("whitespace-only resend changed the text: %q", got)
	}
	after := len(historyRowsFrom(t, api, docKindTaskDescription, task.ID, "m-exec", "agent",
		listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")))
	if after != before {
		t.Fatalf("③ a whitespace-only resend burned a revision: %d → %d", before, after)
	}

	// ② whitespace-only trims to "" and therefore CLEARS — named here so the
	// consequence is a decision on the record, not a surprise found in
	// production.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "   \t "}).Code; got != http.StatusOK {
		t.Fatalf("whitespace-only clear status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "" {
		t.Fatalf("② a whitespace-only description must CLEAR, got %q", got)
	}
}

// An unknown key is refused rather than dropped — the update_task_manual
// posture, and the reason the whole strict-decoder guard exists: a caller who
// reaches for `text` must be told, not silently ignored while believing the
// correction landed.
func TestTaskDescriptionUnknownKeyIsRefused(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"text": "wrong field name"}).Code; got != http.StatusUnprocessableEntity {
		t.Fatalf("unknown key status = %d, want 422", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "" {
		t.Fatalf("refused body still wrote: %q", got)
	}
}

// Restoring an earlier wording goes through the SAME per-task gate as writing
// one. Without this, the generic restore route would be a side door that let
// any agent put text back onto a task it may not edit.
func TestTaskDescriptionRestoreIsGatedLikeTheEdit(t *testing.T) {
	api := newTasksTestServer(t)
	putMemberRow(t, api, "m-exec", KindStaff, "")
	putMemberRow(t, api, "m-other", KindStaff, "")
	task := createAdHocTask(t, api, "m-exec")
	for _, text := range []string{"original wording", "replacement wording"} {
		if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
			map[string]any{"description": text}).Code; got != http.StatusOK {
			t.Fatalf("write %q: status %d", text, got)
		}
	}
	rec := listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")
	history := historyRowsFrom(t, api, docKindTaskDescription, task.ID, "m-exec", "agent", rec)
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1: %+v", len(history), history)
	}
	id := history[0].Id

	restore := func(caller, scope string) int {
		r := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(r,
			taskReq(t, "POST", "/api/document-history/task_description/"+task.ID+"/x/restore",
				nil, caller, scope),
			docKindTaskDescription, task.ID, id)
		return r.Code
	}
	if got := restore("m-other", "agent"); got != http.StatusForbidden {
		t.Fatalf("stranger restore status = %d, want 403", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "replacement wording" {
		t.Fatalf("refused restore still landed: %q", got)
	}
	if got := restore("m-exec", "agent"); got != http.StatusOK {
		t.Fatalf("executor restore status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "original wording" {
		t.Fatalf("restore read back = %q, want original wording", got)
	}
}

// An unknown task is a 404 on both faces — the route must not mint history for
// a task that does not exist, nor report a write that never happened.
func TestTaskDescriptionUnknownTaskIs404(t *testing.T) {
	api := newTasksTestServer(t)
	if got := writeTaskDescription(t, api, "t-nope", "m-exec", "agent",
		map[string]any{"description": "into the void"}).Code; got != http.StatusNotFound {
		t.Fatalf("unknown task status = %d, want 404", got)
	}
}
