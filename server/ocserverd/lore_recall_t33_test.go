package main

// lore_recall_t33_test.go — T-33, 設計 v29 §3.12.x: a boot-time 對象目錄 that
// really reached somebody is a RECALL, and it is journalled with the number of
// subjects it had to drop.
//
// The two things every test here is really defending:
//
//   - the row appears for the three paths that hand a boot document to an
//     agent, and NOT for the two that render a preview for the owner;
//   - `omitted` is carried out of the assembly instead of being spent on one
//     notice line and thrown away. It is the only number that can ever tell the
//     station its caps are too small.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// loreRecallRow is one journal row with `returned` already parsed, which is the
// shape every assertion below wants.
type loreRecallRow struct {
	ActorID   string
	Query     string
	SubjectID string
	Hop       int
	CreatedTS float64
	Subjects  []string `json:"subjects"`
	Omitted   int      `json:"omitted"`
}

// readLoreRecalls reads the whole journal, oldest first. It goes through raw
// SQL rather than a DAL reader on purpose: the assertion is about what actually
// landed in the TABLE, and a reader written next to the writer could agree with
// it about a column the schema never received.
func readLoreRecalls(t *testing.T, s *apiServer) []loreRecallRow {
	t.Helper()
	rows, err := s.dal.rdb.Query(
		`SELECT actor_id, query, subject_id, hop, returned, created_ts
		 FROM lore_recall_log ORDER BY id`)
	if err != nil {
		t.Fatalf("read lore_recall_log: %v", err)
	}
	defer rows.Close()
	var out []loreRecallRow
	for rows.Next() {
		var r loreRecallRow
		var returned string
		if err := rows.Scan(&r.ActorID, &r.Query, &r.SubjectID, &r.Hop,
			&returned, &r.CreatedTS); err != nil {
			t.Fatalf("scan recall row: %v", err)
		}
		if err := json.Unmarshal([]byte(returned), &r); err != nil {
			t.Fatalf("decode returned %q: %v", returned, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate lore_recall_log: %v", err)
	}
	return out
}

// assertBootFoldRow pins the fixed cells every boot-fold row must carry, so the
// per-test assertions can be about the interesting ones.
func assertBootFoldRow(t *testing.T, r loreRecallRow, wantActor string) {
	t.Helper()
	if r.ActorID != wantActor {
		t.Errorf("actor_id = %q, want %q — the row must name the READER", r.ActorID, wantActor)
	}
	if r.Query != loreRecallQueryBoot {
		t.Errorf("query = %q, want %q — the marker is what keeps this path "+
			"separable from a deliberate lookup", r.Query, loreRecallQueryBoot)
	}
	if r.SubjectID != "" {
		t.Errorf("subject_id = %q, want empty — a directory covers many subjects, "+
			"so naming one of them would be a lie", r.SubjectID)
	}
	if r.Hop != 0 {
		t.Errorf("hop = %d, want 0 — nothing was traversed to reach this", r.Hop)
	}
	if r.CreatedTS <= 0 {
		t.Errorf("created_ts = %v — an unstamped row cannot be read as history", r.CreatedTS)
	}
}

// countSeededSubjectsIn counts how many of seedManySubjects' bulk canonicals a
// document actually printed. It counts the FIXTURE's own names in the finished
// text rather than re-running the assembler, so the expected `omitted` below is
// derived independently of the code under test — re-folding would make the
// assertion agree with any number the fold happened to produce.
func countSeededSubjectsIn(doc string) int {
	return strings.Count(doc, "- agent:zz-bulk-") +
		strings.Count(doc, "- agent:zzzz-owner-said-this")
}

// TestMemberStartJournalsTheSurfacing is the member half of the ticket: a START
// that is really queued for a warden files exactly one row, and that row's
// `omitted` is the number of subjects the caps dropped — checked against a
// count taken from the DISPATCHED document, not from a second call to the fold.
func TestMemberStartJournalsTheSurfacing(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	total := loreSubjectIndexMaxSubjects + 7 + 1 // bulk + the human-origin one
	seedManySubjects(t, s, loreSubjectIndexMaxSubjects+7)
	m := wakeSeedAssistant(t, s)

	decision := s.reconcileOne(m, reconcileState{}, 1000)
	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got %q (%s) — with no dispatch this "+
			"test proves nothing", decision.Command, decision.Reason)
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want exactly one START frame, got %d", len(frames))
	}
	_, args := decodeWardenFrame(t, frames[0].Frame)
	persona, _ := args["persona_context"].(string)
	if !strings.Contains(persona, loreSectionH1) {
		t.Fatalf("the dispatched document carries no 對象目錄 — nothing was surfaced, " +
			"so the journal assertions below would be vacuous")
	}
	printed := countSeededSubjectsIn(persona)
	wantOmitted := total - printed
	if wantOmitted <= 0 {
		t.Fatalf("the fixture did not overflow the caps (%d of %d printed) — "+
			"this test cannot say anything about `omitted`", printed, total)
	}

	got := readLoreRecalls(t, s)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 journal row for 1 dispatched boot, got %d", len(got))
	}
	assertBootFoldRow(t, got[0], seedMiraID)
	if got[0].Omitted != wantOmitted {
		t.Errorf("omitted = %d, want %d — 被截掉幾個就是這條紀錄唯一問得出來的「漏」，"+
			"寫錯等於沒寫", got[0].Omitted, wantOmitted)
	}
	if len(got[0].Subjects) != printed {
		t.Errorf("returned.subjects has %d names, the document printed %d",
			len(got[0].Subjects), printed)
	}
}

// TestMemberStartThatIsNotDispatchedJournalsNothing — the reason the row is
// filed at the dispatch and not at the fold. A member whose warden is offline
// still gets a document assembled for it; nobody ever reads it, and the next
// tick folds again. Journalling at assembly time would count one boot many
// times over and would count boots that never happened at all.
func TestMemberStartThatIsNotDispatchedJournalsNothing(t *testing.T) {
	s := newReconcileTestServer(t)
	seedLoreDirectoryFixture(t, s)
	m := wakeSeedAssistant(t, s) // no connectOnline: the warden is not there

	decision := s.reconcileOne(m, reconcileState{}, 1000)
	if decision.Command == reconcileCmdStart {
		t.Fatalf("precondition: with no online warden nothing may be dispatched")
	}
	if got := readLoreRecalls(t, s); len(got) != 0 {
		t.Errorf("an undelivered boot document filed %d recall row(s) — "+
			"沒有人讀到的組裝不是浮上來", len(got))
	}
}

// TestBootstrapPreviewDoesNotJournal — /api/bootstrap serves two different
// things through one handler: a warden spawn (member_id present, a token is
// minted) and the cockpit's preview (no member_id). Only the first is a
// surfacing. The preview arm is asserted FIRST and the spawn arm second on the
// same server, so "no rows" cannot be an artefact of a fixture that never
// produced a directory at all.
func TestBootstrapPreviewDoesNotJournal(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)

	rec := httptest.NewRecorder()
	s.HandleBootstrapApiBootstrapPost(rec, taskReq(t, "POST", "/api/bootstrap",
		map[string]any{"role": seedRoleAssistant}, wireOwnerID, "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(decodeBody[bootstrapDTO](t, rec).Context, loreSectionH1) {
		t.Fatalf("the preview carries no 對象目錄 — the absence below would be vacuous")
	}
	if got := readLoreRecalls(t, s); len(got) != 0 {
		t.Fatalf("a UI preview filed %d recall row(s) — 沒有人開機，這條紀錄是假的", len(got))
	}

	rec = httptest.NewRecorder()
	s.HandleBootstrapApiBootstrapPost(rec, taskReq(t, "POST", "/api/bootstrap",
		map[string]any{"member_id": seedMiraID, "role": seedRoleAssistant},
		wireOwnerID, "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("spawn bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	got := readLoreRecalls(t, s)
	if len(got) != 1 {
		t.Fatalf("a warden spawn must file exactly 1 row, got %d", len(got))
	}
	assertBootFoldRow(t, got[0], seedMiraID)
	if len(got[0].Subjects) == 0 {
		t.Error("returned.subjects is empty for a directory that was really sent")
	}
}

// TestWorkerBootContextPreviewDoesNotJournal — the outsource twin of the case
// above: GET /api/outsource-workers/{id}/boot-context re-runs the production
// fold for the cockpit panel, and no worker boots because of it.
func TestWorkerBootContextPreviewDoesNotJournal(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	seedLoreDirectoryFixture(t, api)
	workerID := assignOneWorker(t, api)

	rec := httptest.NewRecorder()
	api.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet(rec,
		taskReq(t, "GET", "/api/outsource-workers/"+workerID+"/boot-context",
			nil, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("boot-context: %d %s", rec.Code, rec.Body.String())
	}
	// Positive control: the preview really did assemble a directory, so the
	// empty journal below is a decision and not an empty ontology.
	got := decodeBody[WorkerBootContextDTO](t, rec)
	if !strings.Contains(got.Context, loreSectionH1) {
		t.Fatalf("the preview carries no 對象目錄 — the absence below would be vacuous")
	}
	if rows := readLoreRecalls(t, api); len(rows) != 0 {
		t.Errorf("the boot-context PREVIEW filed %d recall row(s) — "+
			"註解逐字寫著 this preview，沒有人開機", len(rows))
	}
}

// TestWorkerSpawnJournalsTheSurfacing — the outsource spawn really does file
// one, actor = the ow- id, and the pacing that suppresses the second dispatch
// suppresses the second row with it: one delivered document, one row.
func TestWorkerSpawnJournalsTheSurfacing(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000rec1", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-rec1",
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-rec1", Codename: "O-R1", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredMachineID: ServerSelfHost,
	})

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.notifyWorkerSpawn(w, nowSecs()) // paced: not dispatched, so not journalled
	s.outsourceMu.Unlock()

	if frames := s.hub.DrainWardenCommands(ServerSelfHost); len(frames) != 1 {
		t.Fatalf("precondition: want exactly 1 dispatched START, got %d", len(frames))
	}
	got := readLoreRecalls(t, s)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 journal row for 1 dispatched worker boot, got %d", len(got))
	}
	assertBootFoldRow(t, got[0], w.ID)
	if len(got[0].Subjects) == 0 {
		t.Error("returned.subjects is empty for a directory that was really sent")
	}
	if got[0].Omitted != 0 {
		t.Errorf("omitted = %d for a 3-subject fixture that fits both caps", got[0].Omitted)
	}
}

// TestEmptyDirectoryJournalsNothing — an empty ontology folds to "", the
// section does not exist in the document, and a recall row for it would be a
// record of nothing being put in front of anyone.
func TestEmptyDirectoryJournalsNothing(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000rec2", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-rec2",
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-rec2", Codename: "O-R2", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredMachineID: ServerSelfHost,
	})

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()

	if frames := s.hub.DrainWardenCommands(ServerSelfHost); len(frames) != 1 {
		t.Fatalf("precondition: the boot itself must still happen, got %d frames", len(frames))
	}
	if got := readLoreRecalls(t, s); len(got) != 0 {
		t.Errorf("an empty directory filed %d recall row(s) — 沒有目錄就沒有浮上來", len(got))
	}
}

// TestJournalWriteFailureDoesNotBlockTheBoot — fail-open, injected at the only
// place that can really fail: the table is gone, so every insert errors. The
// boot must still be dispatched, with the directory intact in the document.
// Booting an agent is the thing; recording that we did is bookkeeping about it.
func TestJournalWriteFailureDoesNotBlockTheBoot(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)
	connectWarden(t, s, ServerSelfHost)
	if _, err := s.dal.wdb.Exec(`DROP TABLE lore_recall_log`); err != nil {
		t.Fatalf("drop lore_recall_log: %v", err)
	}
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000rec3", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-rec3",
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-rec3", Codename: "O-R3", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredMachineID: ServerSelfHost,
	})

	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()

	if !dispatched {
		t.Fatal("a failed journal write blocked the dispatch — 開機比紀錄重要")
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want the START to be enqueued anyway, got %d frames", len(frames))
	}
	_, args := decodeWardenFrame(t, frames[0].Frame)
	persona, _ := args["persona_context"].(string)
	if !strings.Contains(persona, loreSectionH1) {
		t.Error("the dispatched document lost its 對象目錄 when the journal failed")
	}
}
