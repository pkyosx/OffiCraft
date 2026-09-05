package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── T-2ebe 任務標題可編輯 guards ─────────────────────────────────────────────
//
// Shaped after api_tasks_description_test.go, and for the same reason: a title
// edit that answers 200 without changing what the NEXT reader sees is the exact
// failure this capability exists to prevent, so every case below re-reads the
// stored value through a real read path rather than trusting the write's own
// echo.
//
// The one place this file deliberately diverges from its description twin is the
// blank door: an explicit blank title is a 400, not a clear. That asymmetry is
// the ticket's single substantive decision, so it gets its own case with all
// three blank shapes.

// postTaskTitle drives the real handler. Named for the verb rather than
// writeTaskTitle so it is never confused with the apiServer method it exercises.
func postTaskTitle(t *testing.T, api *apiServer, taskID, caller, scope string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskTitleApiTasksTaskIdTitlePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/title", body, caller, scope),
		taskID)
	return rec
}

// readTaskTitle re-reads the title through get_task — the path the cockpit and
// every agent actually use, not the DAL.
func readTaskTitle(t *testing.T, api *apiServer, taskID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskDTO](t, rec).Title
}

func listTaskTitleHistory(t *testing.T, api *apiServer, taskID, caller, scope string) []historyRow {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		taskReq(t, "GET", "/api/document-history/task_title/"+taskID, nil, caller, scope),
		docKindTaskTitle, taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("list title history: %d %s", rec.Code, rec.Body.String())
	}
	return historyRowsFrom(t, api, docKindTaskTitle, taskID, caller, scope, rec)
}

// TestTaskTitleRoundTripsThroughTheTaskView is the core assertion: the corrected
// wording is what the next reader sees. The read-back is a SECOND, independent
// call, so dropping the assignment in the handler or the column from
// SetTaskTitleOn cannot hide behind the response echo.
func TestTaskTitleRoundTripsThroughTheTaskView(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	const corrected = "更正:這張票要的是「標題可編輯」"

	rec := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": corrected})
	if rec.Code != http.StatusOK {
		t.Fatalf("write title: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[taskDTO](t, rec).Title; got != corrected {
		t.Fatalf("response title = %q, want %q", got, corrected)
	}
	if got := readTaskTitle(t, api, task.ID); got != corrected {
		t.Fatalf("title read back = %q, want %q", got, corrected)
	}
}

// TestTaskTitleNonExecutorIsRefusedAndNothingIsWritten pins BOTH halves: the
// refusal AND the absence of a write. A route that answers 403 and writes
// anyway is worse than one that writes openly, and a status-only assertion
// would be perfectly happy with it.
//
// The negative caller is a REAL creator who is no longer the executor (the
// ruling excluded creators specifically), asserted as such before the refusal is
// demanded, plus a bystander so the 403 is not an artefact of a creator branch.
func TestTaskTitleNonExecutorIsRefusedAndNothingIsWritten(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-creator", "Creator", KindStaff)
	putActiveMember(t, api, "m-exec", "Executor", KindStaff)

	task := createAdHocTask(t, api, "m-creator")
	if rec := reassign(t, api, task.ID, memberTarget("m-exec"),
		"owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	after := readTask(t, api, task.ID)
	if after.CreatorID != "m-creator" || after.ExecutorID != "m-exec" {
		t.Fatalf("fixture broken: creator=%q executor=%q", after.CreatorID, after.ExecutorID)
	}
	standing := after.Title

	rec := postTaskTitle(t, api, task.ID, "m-creator", "agent",
		map[string]any{"title": "creator rewrite"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("creator status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec, "forbidden", executorGuardRefusal)
	if got := readTaskTitle(t, api, task.ID); got != standing {
		t.Fatalf("refused write still landed: title = %q, want %q", got, standing)
	}

	putActiveMember(t, api, "m-stranger", "Stranger", KindStaff)
	rec = postTaskTitle(t, api, task.ID, "m-stranger", "agent",
		map[string]any{"title": "stranger rewrite"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger status = %d, want 403", rec.Code)
	}
	assertErrorEnvelope(t, rec, "forbidden", executorGuardRefusal)
	if got := readTaskTitle(t, api, task.ID); got != standing {
		t.Fatalf("refused stranger write still landed: title = %q", got)
	}

	// 🔴 Guard order, stated in the handler's comment and otherwise unguarded:
	// the blank-title 400 sits AFTER this gate on purpose. Swap the two blocks
	// and every other case in this file stays green, because no other case sends
	// a body that BOTH gates object to. A caller who may not touch this task must
	// learn that — not be handed a critique of a body it was never entitled to
	// submit, which also tells it the shape of a route it cannot use.
	for _, blank := range []string{"", "   "} {
		rec = postTaskTitle(t, api, task.ID, "m-stranger", "agent",
			map[string]any{"title": blank})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("non-executor blank %q status = %d, want 403 (the permission "+
				"gate runs BEFORE the blank check): %s", blank, rec.Code, rec.Body.String())
		}
		assertErrorEnvelope(t, rec, "forbidden", executorGuardRefusal)
		if got := readTaskTitle(t, api, task.ID); got != standing {
			t.Fatalf("refused blank write still landed: title = %q", got)
		}
	}

	// Positive controls: the route is not simply broken for everyone.
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": "executor rewrite"}).Code; got != http.StatusOK {
		t.Fatalf("executor status = %d, want 200", got)
	}
	putMemberRow(t, api, "m-mira", KindStaff, adminRoleKey)
	if got := postTaskTitle(t, api, task.ID, "m-mira", "agent",
		map[string]any{"title": "admin rewrite"}).Code; got != http.StatusOK {
		t.Fatalf("admin status = %d, want 200", got)
	}
	if got := readTaskTitle(t, api, task.ID); got != "admin rewrite" {
		t.Fatalf("title read back = %q, want admin rewrite", got)
	}
}

// TestTaskTitleUnknownTaskIs404 — the route must not mint history for a task
// that does not exist, nor report a write that never happened.
func TestTaskTitleUnknownTaskIs404(t *testing.T) {
	api := newTasksTestServer(t)
	rec := postTaskTitle(t, api, "t-nope", "m-exec", "agent",
		map[string]any{"title": "into the void"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestTaskTitleBlankIsRefusedAndLeavesTheStoredTitleAlone is the ticket's one
// deliberate divergence from the description twin: an explicit blank is a 400,
// NOT a clear. All three shapes an agent can actually send are covered, because
// a trim that is applied only to some of them would leave a real bypass — and
// the whole point of the refusal is that a task-list row must never be empty.
func TestTaskTitleBlankIsRefusedAndLeavesTheStoredTitleAlone(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	const standing = "站得住的標題"
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": standing}).Code; got != http.StatusOK {
		t.Fatalf("seed write status = %d", got)
	}

	for _, blank := range []string{"", "   ", "\t\n"} {
		rec := postTaskTitle(t, api, task.ID, "m-exec", "agent",
			map[string]any{"title": blank})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("blank %q status = %d, want 400: %s", blank, rec.Code, rec.Body.String())
		}
		// The message must NAME the field — "invalid request" would leave the
		// caller guessing which of its keys the server objected to.
		assertErrorEnvelope(t, rec, "validation_error", "title")
		if got := readTaskTitle(t, api, task.ID); got != standing {
			t.Fatalf("blank %q was refused but still wrote: title = %q", blank, got)
		}
	}
	// And it versioned nothing: a refused write must not spend a retained slot.
	if n := len(listTaskTitleHistory(t, api, task.ID, "m-exec", "agent")); n != 1 {
		t.Fatalf("history length = %d, want 1 (only the seed write)", n)
	}
}

// TestTaskTitleAbsentFieldIsANoOpThatVersionsNothing — the partial-update shape.
// The assertion is the revision COUNT, not merely a 200: a no-op that quietly
// retained a revision would spend one of the three slots recording that nothing
// changed, and a status-only test would never see it.
func TestTaskTitleAbsentFieldIsANoOpThatVersionsNothing(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	const standing = "the standing title"
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": standing}).Code; got != http.StatusOK {
		t.Fatalf("seed write status = %d", got)
	}
	before := len(listTaskTitleHistory(t, api, task.ID, "m-exec", "agent"))
	if before != 1 {
		t.Fatalf("fixture: history length = %d, want 1", before)
	}

	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{}).Code; got != http.StatusOK {
		t.Fatalf("absent-field status = %d, want 200", got)
	}
	if got := readTaskTitle(t, api, task.ID); got != standing {
		t.Fatalf("absent field changed the title: %q", got)
	}
	if after := len(listTaskTitleHistory(t, api, task.ID, "m-exec", "agent")); after != before {
		t.Fatalf("omitted field retained a revision: history %d → %d", before, after)
	}
}

// TestTaskTitleUnchangedValueVersionsNothing covers both shapes of "no change":
// the identical string, and one that differs ONLY by surrounding whitespace.
// The second is the reason the comparison happens AFTER trimming — a caller that
// re-sends a title with a stray trailing space has not changed anything, and
// must not burn a revision or fan a delta saying it did.
func TestTaskTitleUnchangedValueVersionsNothing(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	const standing = "the standing title"
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": standing}).Code; got != http.StatusOK {
		t.Fatalf("seed write status = %d", got)
	}
	before := len(listTaskTitleHistory(t, api, task.ID, "m-exec", "agent"))

	for _, resend := range []string{standing, "  " + standing + "  ", "\t" + standing + "\n"} {
		if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
			map[string]any{"title": resend}).Code; got != http.StatusOK {
			t.Fatalf("resend %q status = %d, want 200", resend, got)
		}
		if got := readTaskTitle(t, api, task.ID); got != standing {
			t.Fatalf("resend %q changed the stored title: %q", resend, got)
		}
		if after := len(listTaskTitleHistory(t, api, task.ID, "m-exec", "agent")); after != before {
			t.Fatalf("resending %q retained a revision: history %d → %d",
				resend, before, after)
		}
	}
}

// TestTaskTitleIsTrimmedBeforeItIsStored — the stored value is the trimmed one,
// asserted through a fresh read. A title stored with its padding would render a
// task-list row that looks indented for no reason, and would compare unequal to
// the same words typed again.
func TestTaskTitleIsTrimmedBeforeItIsStored(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	rec := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": "  \t整理過的標題\n  "})
	if rec.Code != http.StatusOK {
		t.Fatalf("write status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[taskDTO](t, rec).Title; got != "整理過的標題" {
		t.Fatalf("response title = %q, want the trimmed value", got)
	}
	if got := readTaskTitle(t, api, task.ID); got != "整理過的標題" {
		t.Fatalf("stored title = %q, want the trimmed value", got)
	}
}

// TestTaskTitleEditableOnAClosedTask — ruling: closing a task does not freeze
// its text. The artifact control on the SAME closed task is what makes this a
// statement about a REASONED difference rather than a missing terminal guard.
func TestTaskTitleEditableOnAClosedTask(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	terminateTask(t, api, task.ID)

	const corrected = "結案後才發現票名寫錯,照樣改得動"
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": corrected}).Code; got != http.StatusOK {
		t.Fatalf("closed-task title status = %d, want 200", got)
	}
	if got := readTaskTitle(t, api, task.ID); got != corrected {
		t.Fatalf("closed-task title read back = %q, want %q", got, corrected)
	}

	rec := httptest.NewRecorder()
	api.HandleAddTaskArtifactApiTasksTaskIdArtifactPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/artifact",
			map[string]any{"kind": "link", "name": "pr", "url": "https://example.invalid/pr/1"},
			"m-exec", "agent"),
		task.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("closed-task artifact status = %d, want 409 (the frozen set)", rec.Code)
	}
}

// TestTaskTitleHistoryRetainsAndRestoresThePreviousTitle — the trail rides the
// already-shipped document-history mechanism, and the retained revision must be
// the title the write REPLACED (the mistake a snapshot taken after the write
// would make). The restore then puts that exact value back, read through
// get_task.
func TestTaskTitleHistoryRetainsAndRestoresThePreviousTitle(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	created := task.Title
	if created == "" {
		t.Fatal("fixture: a created task must already have a title")
	}

	for _, title := range []string{"first wording", "second wording"} {
		if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
			map[string]any{"title": title}).Code; got != http.StatusOK {
			t.Fatalf("write %q: status %d", title, got)
		}
	}

	history := listTaskTitleHistory(t, api, task.ID, "m-exec", "agent")
	// Two revisions, one per real change — and unlike the description twin the
	// FIRST edit retains one too, because a task can never have had a blank
	// title to begin with.
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2: %+v", len(history), history)
	}
	if got := history[0].Content["title"]; got != "first wording" {
		t.Fatalf("newest revision = %q, want %q", got, "first wording")
	}
	if got := history[1].Content["title"]; got != created {
		t.Fatalf("oldest revision = %q, want the created title %q", got, created)
	}
	if history[0].ActorId != "m-exec" {
		t.Fatalf("revision actor = %q, want m-exec", history[0].ActorId)
	}

	// The restore puts back the PREVIOUS title, not the current one.
	rec := httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
		taskReq(t, "POST", "/api/document-history/task_title/"+task.ID+"/x/restore",
			nil, "m-exec", "agent"),
		docKindTaskTitle, task.ID, history[0].Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	if got := readTaskTitle(t, api, task.ID); got != "first wording" {
		t.Fatalf("restored title = %q, want %q", got, "first wording")
	}
}

// TestTaskTitleRestoreIsGatedLikeTheEdit — without this, the generic restore
// route would be a side door letting any agent put a title back onto a task it
// may not edit.
func TestTaskTitleRestoreIsGatedLikeTheEdit(t *testing.T) {
	api := newTasksTestServer(t)
	putMemberRow(t, api, "m-exec", KindStaff, "")
	putMemberRow(t, api, "m-other", KindStaff, "")
	task := createAdHocTask(t, api, "m-exec")
	for _, title := range []string{"original wording", "replacement wording"} {
		if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
			map[string]any{"title": title}).Code; got != http.StatusOK {
			t.Fatalf("write %q: status %d", title, got)
		}
	}
	history := listTaskTitleHistory(t, api, task.ID, "m-exec", "agent")
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2: %+v", len(history), history)
	}
	id := history[0].Id

	restore := func(caller, scope string) int {
		r := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(r,
			taskReq(t, "POST", "/api/document-history/task_title/"+task.ID+"/x/restore",
				nil, caller, scope),
			docKindTaskTitle, task.ID, id)
		return r.Code
	}
	if got := restore("m-other", "agent"); got != http.StatusForbidden {
		t.Fatalf("stranger restore status = %d, want 403", got)
	}
	if got := readTaskTitle(t, api, task.ID); got != "replacement wording" {
		t.Fatalf("refused restore still landed: %q", got)
	}
	if got := restore("m-exec", "agent"); got != http.StatusOK {
		t.Fatalf("executor restore status = %d, want 200", got)
	}
	if got := readTaskTitle(t, api, task.ID); got != "original wording" {
		t.Fatalf("restore read back = %q, want original wording", got)
	}
}

// TestTaskTitleEditFansATaskDelta guards the headline acceptance criterion.
//
// Delete `s.publishTask(*t, requestTrigger(r))` from the edit handler and the
// route still answers 200, still stores the correction, still retains the
// revision — and the whole Go package stays green. The only symptom is that an
// ALREADY-OPEN cockpit task list goes on showing the stale title, which is the
// one thing this capability exists to fix.
//
// The description edit is the positive control: it fans from its own,
// independent publishTask call, so a listener that received nothing at all
// would otherwise be indistinguishable from a broken fixture.
//
// The no-op arm is here for the opposite mutant: a handler that fanned
// unconditionally (before the unchanged-value early return) would satisfy the
// first assertion while spraying a delta for every re-send.
func TestTaskTitleEditFansATaskDelta(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	// Connect AFTER the seed write so the queue holds only what we drive below.
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": "the standing title"}).Code; got != http.StatusOK {
		t.Fatalf("seed title write status = %d", got)
	}
	listener, err := api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	// Positive control first: the description edit has always fanned.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "some prose"}).Code; got != http.StatusOK {
		t.Fatalf("control description write status = %d", got)
	}
	if raw := listener.pop(); raw == nil {
		t.Fatal("control: a description edit fanned NO frame — the listener or the " +
			"publish seam is broken, so a missing title frame below proves nothing")
	}

	// The real assertion.
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": "the corrected title"}).Code; got != http.StatusOK {
		t.Fatalf("title edit status = %d", got)
	}
	raw := listener.pop()
	if raw == nil {
		t.Fatal("a successful title edit fanned NO frame: the route answered 200 and " +
			"stored the correction, so nothing else in the build will tell you — " +
			"every open task list is now showing the stale title. Put " +
			"s.publishTask back at the end of the edit handler.")
	}
	_, envelope := parseSSEFrame(t, raw)
	if envelope["topic"] != "task" {
		t.Fatalf("edit fanned topic=%v, want \"task\"", envelope["topic"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("frame data is not an object: %v", envelope["data"])
	}
	if want := wireOwnerID + "::" + task.ID; data["key"] != want {
		t.Fatalf("frame key = %v, want %q", data["key"], want)
	}

	// And the arms that changed nothing fan nothing: a delta claiming a task
	// moved when it did not is noise every open cockpit pays for.
	for _, noop := range []map[string]any{
		{"title": "the corrected title"},     // identical
		{"title": "  the corrected title  "}, // identical after trimming
		{},                                   // field omitted entirely
	} {
		if got := postTaskTitle(t, api, task.ID, "m-exec", "agent", noop).Code; got != http.StatusOK {
			t.Fatalf("no-op %v status = %d, want 200", noop, got)
		}
		if extra := listener.pop(); extra != nil {
			t.Fatalf("no-op %v fanned a frame: %s", noop, extra)
		}
	}
}

// TestTaskTitleRestoreFansATaskDelta guards the SILENT arm.
//
// publishDocumentHistoryRestore is a switch with no default: drop task_title
// from its case and the restore still answers 200, still changes the database,
// still returns the DTO — and the only symptom is that every open cockpit card
// keeps showing the old title until someone reloads by hand. Nothing else in the
// build goes red. The description half is the positive control: task_title
// shares that arm with it, so a listener that received NOTHING at all would
// otherwise be indistinguishable from a broken fixture.
func TestTaskTitleRestoreFansATaskDelta(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	for _, title := range []string{"first wording", "second wording"} {
		if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
			map[string]any{"title": title}).Code; got != http.StatusOK {
			t.Fatalf("seed title write %q: status %d", title, got)
		}
	}
	for _, text := range []string{"first prose", "second prose"} {
		if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
			map[string]any{"description": text}).Code; got != http.StatusOK {
			t.Fatalf("seed description write %q: status %d", text, got)
		}
	}

	// Connect AFTER the seed writes so the queue holds only restore frames.
	listener, err := api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	restore := func(kind string, id int64) {
		t.Helper()
		r := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(r,
			taskReq(t, "POST", "/api/document-history/"+kind+"/"+task.ID+"/x/restore",
				nil, "m-exec", "agent"),
			kind, task.ID, id)
		if r.Code != http.StatusOK {
			t.Fatalf("restore %s: %d %s", kind, r.Code, r.Body.String())
		}
	}

	// Positive control first: the description arm has always been present.
	descHistory := historyRowsFrom(t, api, docKindTaskDescription, task.ID, "m-exec", "agent", func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
			taskReq(t, "GET", "/api/document-history/task_description/"+task.ID, nil,
				"m-exec", "agent"),
			docKindTaskDescription, task.ID)
		return rec
	}())
	if len(descHistory) == 0 {
		t.Fatal("description kept no revision — the control cannot run")
	}
	restore(docKindTaskDescription, descHistory[0].Id)
	if raw := listener.pop(); raw == nil {
		t.Fatal("control: restoring a description fanned NO frame — the listener or " +
			"the publish seam is broken, so a missing title frame below proves nothing")
	}

	// The real assertion.
	titleHistory := listTaskTitleHistory(t, api, task.ID, "m-exec", "agent")
	if len(titleHistory) == 0 {
		t.Fatal("title kept no revision to restore")
	}
	restore(docKindTaskTitle, titleHistory[0].Id)
	raw := listener.pop()
	if raw == nil {
		t.Fatal("restoring a title fanned NO frame: the restore answered 200 and " +
			"changed the database, so nothing else in the build will tell you — " +
			"every open card is now showing a stale title. Put docKindTaskTitle " +
			"back in publishDocumentHistoryRestore.")
	}
	_, envelope := parseSSEFrame(t, raw)
	if envelope["topic"] != "task" {
		t.Fatalf("restore fanned topic=%v, want \"task\"", envelope["topic"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("frame data is not an object: %v", envelope["data"])
	}
	if want := wireOwnerID + "::" + task.ID; data["key"] != want {
		t.Fatalf("frame key = %v, want %q", data["key"], want)
	}
}
