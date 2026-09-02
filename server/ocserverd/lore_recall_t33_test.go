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
	"strconv"
	"strings"
	"testing"
)

// loreRecallRow is one journal row with `returned` already parsed, which is the
// shape every assertion below wants.
type loreRecallRow struct {
	ActorID       string
	Query         string
	SubjectID     string
	Hop           int
	CreatedTS     float64
	SessionBootTS float64
	SessionState  string
	Subjects      []string `json:"subjects"`
	Omitted       int      `json:"omitted"`
	Entries       []string `json:"entries"`
	QueryText     string   `json:"query"`
	SubjectKey    string   `json:"subject"`
	Actions       []string `json:"actions"`
	Total         int      `json:"total"`
	Truncated     bool     `json:"truncated"`
}

// readLoreRecalls reads the whole journal, oldest first. It goes through raw
// SQL rather than a DAL reader on purpose: the assertion is about what actually
// landed in the TABLE, and a reader written next to the writer could agree with
// it about a column the schema never received.
func readLoreRecalls(t *testing.T, s *apiServer) []loreRecallRow {
	t.Helper()
	return readLoreRecallsOf(t, s.dal)
}

// readLoreRecallsOf is the same read against a bare DAL, which is all the route
// tests are handed (loreGovStack builds the wired stack and returns the store,
// never the apiServer).
func readLoreRecallsOf(t *testing.T, dal *DAL) []loreRecallRow {
	t.Helper()
	rows, err := dal.rdb.Query(
		`SELECT actor_id, query, subject_id, hop, returned, created_ts,
		        session_boot_ts, session_state
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
			&returned, &r.CreatedTS, &r.SessionBootTS, &r.SessionState); err != nil {
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
	// 🔴 A BOOT FOLD IS 'unanchored', NEVER 'unrecorded' AND NEVER SOMEBODY
	// ELSE'S ANCHOR. The document goes to a session that has not connected yet,
	// so it genuinely has none — and this call sits one line before
	// clearSessionBootTS in reconcileOne, so a writer that asked the roster
	// would file the OUTGOING session's anchor here and it would look right.
	if r.SessionState != loreRecallSessionUnanchored || r.SessionBootTS != 0 {
		t.Errorf("session = %q/%v, want %q/0 — a boot fold has no anchor yet, and "+
			"saying so is different from not having looked",
			r.SessionState, r.SessionBootTS, loreRecallSessionUnanchored)
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

// TestBootstrapWithoutASigningSecretJournalsNothing pins the case the handler's
// own comment always claimed and the code did not implement: a bootstrap that
// names a member but mints NO TOKEN is not a delivery.
//
// 🔴 WHY THIS IS A REAL CASE AND NOT A CONTRIVED ONE. Minting needs a member AND
// a signing secret. A station running without a secret answers 200, hands back
// the boot document, and sets `token: null` — and NOBODY CAN BOOT ON THAT, there
// is no credential to connect with. The journal used to key off `member != nil`,
// so it filed those as real wakes: precisely the non-event the whole journal
// exists to keep out, sitting in the one table that is supposed to answer "who
// actually saw this".
//
// ⚠️ FOUND IN REVIEW BY A HUMAN-DRIVEN READ (Kyle), NOT BY A TEST. Seven other
// tests covered this file and every one of them ran on a server that happens to
// have a secret, so the branch was never exercised. That is the reason this test
// exists rather than a comment: the next person needs the machine to say it.
func TestBootstrapWithoutASigningSecretJournalsNothing(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)
	s.secret = nil

	rec := httptest.NewRecorder()
	s.HandleBootstrapApiBootstrapPost(rec, taskReq(t, "POST", "/api/bootstrap",
		map[string]any{"member_id": seedMiraID, "role": seedRoleAssistant},
		wireOwnerID, "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap without a secret: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[bootstrapDTO](t, rec)
	if got.Token != nil {
		t.Fatalf("a station with no secret must not mint a token — the premise of "+
			"this test is gone, re-read the handler (token=%q)", *got.Token)
	}
	// Positive control: the directory really is in this response, so the absence
	// of a journal row below cannot be explained by "there was nothing to record".
	if !strings.Contains(got.Context, loreSectionH1) {
		t.Fatalf("this response carries no 對象目錄 — the assertion below would be vacuous")
	}
	if rows := readLoreRecalls(t, s); len(rows) != 0 {
		t.Fatalf("a bootstrap that minted no token filed %d recall row(s) — "+
			"沒有人拿得到憑證，那份文件不會被任何 agent 讀到，這條紀錄是假的", len(rows))
	}
}

// ── the retrieval half (T-33, 2026-09-02) ───────────────────────────────────
//
// 🔴 THE MEASUREMENT THESE TESTS REPLACED. Written against the code as it stood
// before this round, all three of the "journals" tests below saw ZERO rows: a
// search, an entry read and a revision read left the table exactly as they found
// it. The only writer was the boot fold. That is the state the owner's question
// 「agent 到底有沒有用到我們寫的記憶」 was being asked against, and it is why it
// could not be answered.

// loreRecallStack is loreGovStack plus a seeded entry and an ANCHORED session
// for the agent, which is the state every retrieval assertion below needs: an
// unanchored actor would make the anchor cells trivially right for the wrong
// reason.
func loreRecallStack(t *testing.T, bootTS float64) (url, tok string, dal *DAL, entryID string) {
	t.Helper()
	url, dal, tok, _, _ = loreGovStack(t)
	entryID = loreSearchSeed(t, url, tok, "repo:officraft", "the fold happens in one place")
	if err := dal.SetMemberSessionBootTS("m-lore-agent", bootTS); err != nil {
		t.Fatalf("anchor the agent's session: %v", err)
	}
	// The seeding write must not itself have filed anything, or every count
	// below is measuring the wrong thing.
	if got := readLoreRecallsOf(t, dal); len(got) != 0 {
		t.Fatalf("seeding wrote %d journal rows; the retrieval assertions need 0", len(got))
	}
	return url, tok, dal, entryID
}

// TestLoreSearchRouteJournalsTheRetrieval — hop ② is a use of the memory, so it
// is one row: who, when, on what axes, and WHICH ENTRIES came back.
func TestLoreSearchRouteJournalsTheRetrieval(t *testing.T) {
	url, tok, dal, entryID := loreRecallStack(t, 4321)

	st, body := rosterREST(t, url, tok, "POST", "/api/lore/search",
		`{"subject":"repo:officraft","query":"fold","actions":["build"],"limit":5}`)
	if st != 200 {
		t.Fatalf("search: %d %s", st, body)
	}
	if got := loreSearchBody(t, body); len(got.Entries) != 1 {
		t.Fatalf("the search returned %d entries — with no hit this test would "+
			"pass on an empty journal row", len(got.Entries))
	}

	rows := readLoreRecallsOf(t, dal)
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 journal row for 1 search, got %d", len(rows))
	}
	r := rows[0]
	if r.ActorID != "m-lore-agent" || r.Query != loreRecallQuerySearch {
		t.Errorf("actor/query = %q/%q, want %q/%q", r.ActorID, r.Query,
			"m-lore-agent", loreRecallQuerySearch)
	}
	if len(r.Entries) != 1 || r.Entries[0] != entryID {
		t.Errorf("entries = %v, want [%s] — 「撈到哪幾條」 is the whole point; a count "+
			"could never answer whether ONE entry is ever used", r.Entries, entryID)
	}
	if r.QueryText != "fold" || r.SubjectKey != "repo:officraft" ||
		len(r.Actions) != 1 || r.Actions[0] != "build" {
		t.Errorf("asked-for axes = %q/%q/%v — a hit list nobody can interpret",
			r.QueryText, r.SubjectKey, r.Actions)
	}
	if r.SubjectID == "" {
		t.Errorf("subject_id is empty — the subject RESOLVED, and the entity it " +
			"resolved onto is what makes two callers' searches comparable")
	}
	if r.Total != 1 || r.Truncated {
		t.Errorf("total/truncated = %d/%v — without them a truncated retrieval "+
			"reads as a complete one", r.Total, r.Truncated)
	}
	if r.SessionState != loreRecallSessionAnchored || r.SessionBootTS != 4321 {
		t.Fatalf("session = %q/%v, want %q/4321", r.SessionState, r.SessionBootTS,
			loreRecallSessionAnchored)
	}
}

// A search whose subject names nothing is still a use of the memory, and the
// row has to exist — otherwise a typo'd subject is indistinguishable from a
// search nobody ever ran.
func TestLoreSearchRouteJournalsASubjectThatResolvedToNothing(t *testing.T) {
	url, tok, dal, _ := loreRecallStack(t, 4321)

	st, body := rosterREST(t, url, tok, "POST", "/api/lore/search",
		`{"subject":"repo:offcraft"}`)
	if st != 200 {
		t.Fatalf("search: %d %s", st, body)
	}
	if got := loreSearchBody(t, body); got.SubjectResolved {
		t.Fatalf("the fixture subject resolved after all; this test proves nothing")
	}
	rows := readLoreRecallsOf(t, dal)
	if len(rows) != 1 {
		t.Fatalf("want 1 row for a search that ran and found no subject, got %d", len(rows))
	}
	if len(rows[0].Entries) != 0 || rows[0].SubjectKey != "repo:offcraft" {
		t.Errorf("row = %v / %q — it must say WHAT was asked even though nothing "+
			"came back", rows[0].Entries, rows[0].SubjectKey)
	}
	if rows[0].SubjectID != "" {
		t.Errorf("subject_id = %q, want empty — nothing resolved, so naming an "+
			"entity would be an invention", rows[0].SubjectID)
	}
}

// TestLoreEntryReadJournalsTheRetrieval — 讀單條一次一列, and reading the same
// entry three times is THREE rows.
//
// 🔴 THE REPETITION IS THE SIGNAL, NOT NOISE. Same actor + same session + same
// entry, several times over, is the station's only evidence that an entry's
// `short` form does not carry its weight — the agent keeps going back to the
// original. De-duplicating here (or an upsert, or a counter) would delete
// exactly that, and it would look like tidying.
func TestLoreEntryReadJournalsTheRetrieval(t *testing.T) {
	url, tok, dal, entryID := loreRecallStack(t, 999)

	for i := 0; i < 3; i++ {
		st, body := rosterREST(t, url, tok, "GET", "/api/lore/entries/"+entryID, "")
		if st != 200 {
			t.Fatalf("read %d: %d %s", i, st, body)
		}
	}
	rows := readLoreRecallsOf(t, dal)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows for 3 reads, got %d — the journal is append-only and "+
			"repeated reads are the measurement", len(rows))
	}
	for i, r := range rows {
		if r.Query != loreRecallQueryEntryRead {
			t.Errorf("row %d query = %q, want %q", i, r.Query, loreRecallQueryEntryRead)
		}
		if len(r.Entries) != 1 || r.Entries[0] != entryID {
			t.Errorf("row %d entries = %v, want [%s]", i, r.Entries, entryID)
		}
		if r.SessionState != loreRecallSessionAnchored || r.SessionBootTS != 999 {
			t.Errorf("row %d session = %q/%v, want %q/999 — without the anchor these "+
				"three rows cannot be told from three DIFFERENT sessions reading it "+
				"once each, which is the opposite conclusion",
				i, r.SessionState, r.SessionBootTS, loreRecallSessionAnchored)
		}
	}
}

// An entry id that names nothing files NOTHING. A 404 is not a use of the
// memory, and a journal padded with reads that did not happen cannot be used to
// argue that anything is unused.
func TestLoreEntryReadDoesNotJournalA404(t *testing.T) {
	url, tok, dal, _ := loreRecallStack(t, 999)

	if st, _ := rosterREST(t, url, tok, "GET", "/api/lore/entries/le-nothing", ""); st != 404 {
		t.Fatalf("want 404 for an unknown entry, got %d", st)
	}
	if rows := readLoreRecallsOf(t, dal); len(rows) != 0 {
		t.Fatalf("a 404 filed %d rows, want 0", len(rows))
	}
}

// Reading one REVISION is its own row kind: an agent working out what an entry
// used to say is the strongest form of 「短版不夠用」 the journal can observe, and
// folding it in with an ordinary entry read would hide it.
func TestLoreRevisionReadJournalsTheRetrieval(t *testing.T) {
	url, tok, dal, entryID := loreRecallStack(t, 77)

	st, body := rosterREST(t, url, tok, "GET", "/api/lore/entries/"+entryID, "")
	if st != 200 {
		t.Fatalf("read entry: %d %s", st, body)
	}
	detail := struct {
		Revisions []struct {
			RevisionId int `json:"revision_id"`
		} `json:"revisions"`
	}{}
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Revisions) == 0 {
		t.Fatalf("the seeded entry has no revision to read; this test proves nothing")
	}
	revID := detail.Revisions[0].RevisionId

	st, body = rosterREST(t, url, tok, "GET",
		"/api/lore/entries/"+entryID+"/revisions/"+strconv.Itoa(revID), "")
	if st != 200 {
		t.Fatalf("read revision: %d %s", st, body)
	}

	rows := readLoreRecallsOf(t, dal)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (one entry read, one revision read), got %d", len(rows))
	}
	if rows[0].Query != loreRecallQueryEntryRead ||
		rows[1].Query != loreRecallQueryRevisionRead {
		t.Fatalf("markers = %q, %q — the two doors must stay separable",
			rows[0].Query, rows[1].Query)
	}
	if len(rows[1].Entries) != 1 || rows[1].Entries[0] != entryID {
		t.Errorf("revision row entries = %v, want [%s] — the row names the ENTRY, "+
			"which is the thing whose usefulness is being measured",
			rows[1].Entries, entryID)
	}
	if rows[1].SessionState != loreRecallSessionAnchored || rows[1].SessionBootTS != 77 {
		t.Errorf("session = %q/%v, want %q/77", rows[1].SessionState,
			rows[1].SessionBootTS, loreRecallSessionAnchored)
	}
}

// 🔴 THIS IS THE TEST THE WHOLE COLUMN EXISTS FOR. member.session_boot_ts is ONE
// CELL: the actor's next session overwrites it. A row that only carried an
// absolute timestamp, meaning to join the anchor back later, answers about
// WHICHEVER session happens to be current when the question is asked — so the
// first session's rows silently lose their basis and render exactly like rows
// nobody ever wrote.
//
// Here the same actor reads once under anchor 100, the roster is re-anchored to
// 500 (a new session), and the actor reads again. The first row must still say
// 100. If it does not, 「開機後多久」 is unanswerable for every session but the
// last one — which is the failure that has no error message.
func TestLoreRecallKeepsTheAnchorTheReadActuallyHappenedUnder(t *testing.T) {
	url, tok, dal, entryID := loreRecallStack(t, 100)

	if st, body := rosterREST(t, url, tok, "GET", "/api/lore/entries/"+entryID, ""); st != 200 {
		t.Fatalf("first read: %d %s", st, body)
	}
	if err := dal.SetMemberSessionBootTS("m-lore-agent", 500); err != nil {
		t.Fatalf("re-anchor: %v", err)
	}
	if st, body := rosterREST(t, url, tok, "GET", "/api/lore/entries/"+entryID, ""); st != 200 {
		t.Fatalf("second read: %d %s", st, body)
	}

	rows := readLoreRecallsOf(t, dal)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].SessionBootTS != 100 {
		t.Errorf("the FIRST row's anchor = %v, want 100 — the previous session's "+
			"basis was overwritten in `member`, and this row is the only place it "+
			"still exists", rows[0].SessionBootTS)
	}
	if rows[1].SessionBootTS != 500 {
		t.Errorf("the SECOND row's anchor = %v, want 500", rows[1].SessionBootTS)
	}
	if rows[0].SessionBootTS == rows[1].SessionBootTS {
		t.Errorf("both rows carry the same anchor — 「同一任 session 內反覆讀同一條」" +
			"(這條記憶寫得不好) and 「不同 session 反覆讀到」(這條記憶好) are opposite " +
			"conclusions, and this cell is the only thing that separates them")
	}
}

// An actor the roster does not know — the owner reading through the cockpit, a
// warden, an id from a previous life — is 'unanchored', not 'unrecorded'. We
// looked and there was nothing, which is a different fact from nobody looking,
// and 'unrecorded' is reserved for the second so a future writer that forgets
// the anchor is VISIBLE rather than indistinguishable from this.
func TestLoreRecallSaysUnanchoredWhenTheActorHasNoSession(t *testing.T) {
	url, dal, _, ownerTok, _ := loreGovStack(t)
	entryID := loreSearchSeed(t, url, ownerTok, "repo:officraft", "the fold happens in one place")

	if st, body := rosterREST(t, url, ownerTok, "GET", "/api/lore/entries/"+entryID, ""); st != 200 {
		t.Fatalf("read: %d %s", st, body)
	}
	rows := readLoreRecallsOf(t, dal)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].SessionState != loreRecallSessionUnanchored || rows[0].SessionBootTS != 0 {
		t.Errorf("session = %q/%v, want %q/0", rows[0].SessionState,
			rows[0].SessionBootTS, loreRecallSessionUnanchored)
	}
	if rows[0].SessionState == loreRecallSessionUnrecorded {
		t.Errorf("'unrecorded' is reserved for a row nobody stamped at all")
	}
}

// The DEFAULT is what protects the distinction above: a row that reaches the
// table without going through recordLoreRecall says so in its own cell instead
// of masquerading as a read that happened outside a session.
func TestLoreRecallRowWrittenWithoutAnAnchorReadsAsUnrecorded(t *testing.T) {
	_, dal, _, _, _ := loreGovStack(t)

	if err := dal.InsertLoreRecall(LoreRecall{
		ActorID: "m-lore-agent", Query: "hand-written", CreatedTS: 1, Returned: "{}",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows := readLoreRecallsOf(t, dal)
	if len(rows) != 1 || rows[0].SessionState != loreRecallSessionUnrecorded {
		t.Fatalf("session_state = %+v, want %q", rows, loreRecallSessionUnrecorded)
	}
}

// 🔴 THE ORDERING TRAP, PINNED. In reconcileOne the surfacing is journalled the
// line BEFORE clearSessionBootTS, so at that instant member.session_boot_ts
// still holds the OUTGOING session's anchor. A writer that simply asked the
// roster would file that number as this boot's own — a plausible-looking anchor
// belonging to a session that had already ended, and no error anywhere.
//
// ⚠️ MEASURED: without this test the trap is invisible. Switching the boot fold
// to loreAnchorFromRoster left the entire lore + worker + migration selection
// GREEN, because every other fixture dispatches a member whose anchor is already
// 0 — the mutant and the correct code produce identical rows there.
func TestBootFoldRowNeverCarriesTheOutgoingSessionsAnchor(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	seedManySubjects(t, s, 3)
	m := wakeSeedAssistant(t, s)
	// The member's PREVIOUS session, still stamped on the row at dispatch time.
	if err := s.dal.SetMemberSessionBootTS(m.ID, 1700000000); err != nil {
		t.Fatalf("stamp the outgoing session's anchor: %v", err)
	}
	if got, _ := s.dal.GetMember(m.ID); got == nil || got.SessionBootTS != 1700000000 {
		t.Fatalf("the fixture did not take; this test would prove nothing")
	}

	decision := s.reconcileOne(m, reconcileState{}, 1000)
	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got %q (%s)", decision.Command, decision.Reason)
	}
	rows := readLoreRecalls(t, s)
	if len(rows) != 1 {
		t.Fatalf("want 1 boot-fold row, got %d", len(rows))
	}
	if rows[0].SessionBootTS != 0 || rows[0].SessionState != loreRecallSessionUnanchored {
		t.Fatalf("the boot row carries %v/%q — that is the session that just ENDED. "+
			"A boot fold goes to a session that has not connected yet, so its honest "+
			"answer is %q/0", rows[0].SessionBootTS, rows[0].SessionState,
			loreRecallSessionUnanchored)
	}
}
