package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ── T-52917b:遞增票號 ─────────────────────────────────────────────────────────
//
// 🔴 A MINTING COLLISION IS NOW LOUD, AND THIS TEST CATCHES IT TWICE OVER.
//
// HOW IT USED TO BE SILENT, and why the assertions below are shaped the way
// they are. task.id is a TEXT PRIMARY KEY, so two rows can never share an id.
// While the create path wrote through PutTask's `ON CONFLICT (id) DO UPDATE`
// upsert, a repeated mint therefore did not error: the second create silently
// OVERWROTE the first and the API still answered 200. The damage was an
// invisible MISSING ROW, so counting rows was the ONLY assertion that could see
// it — a test that scanned the returned ids for duplicates was evergreen.
//
// WHAT CHANGED (T-52917b review, 建議 1). CreateTaskMintingID no longer uses the
// conflict clause: it INSERTs (dal_tasks.go, taskWriteInsertOnly). A repeated
// mint now trips the primary key, the whole create transaction rolls back, and
// the request answers 500. So the collision surfaces FIRST at the status-code
// loop below, before the row count is ever consulted.
//
// 🔴 THE ROW COUNT STAYS ANYWAY, and is still the load-bearing assertion. The
// INSERT closes the "200 while a row vanishes" door only for THIS one statement.
// The row count is the assertion that does not care HOW a row went missing — it
// would still catch a future path that reintroduces an upsert, a swallowed
// error, or a rollback nobody reported. A status-code check alone would not.
//
// The property under test is UNIQUENESS, not contiguity. A gap (a burned
// number from a rolled-back transaction) is fine; two tasks called T-7 is not.

var taskSeqIDRe = regexp.MustCompile(`^T-[1-9][0-9]*$`)

// TestConcurrentCreatesMintDistinctIncrementalIDs drives N REAL create_task
// requests through the REAL handler concurrently and then asks the database how
// many task rows exist.
//
// N is deliberately small (32). This is a correctness probe, not a load test.
func TestConcurrentCreatesMintDistinctIncrementalIDs(t *testing.T) {
	const n = 32
	api := newTasksTestServer(t)

	var mu sync.Mutex
	ids := make([]string, 0, n)
	codes := make([]int, 0, n)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release them together so the mint window is actually contested
			rec := httptest.NewRecorder()
			api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
				map[string]any{
					"title":              "concurrent " + strconv.Itoa(i),
					"executor_member_id": "m-exec",
				}, "m-exec", "agent"))
			mu.Lock()
			defer mu.Unlock()
			codes = append(codes, rec.Code)
			if rec.Code == http.StatusOK {
				var out taskCreateResultDTO
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err == nil {
					ids = append(ids, out.TaskID)
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()

	// ⓪ Since 建議 1 this is where a minting collision announces itself: two
	// creates that mint the same number make the SECOND INSERT trip the TEXT
	// PRIMARY KEY, roll its transaction back and answer 500. Before that change
	// this loop stayed all-200 while rows quietly disappeared.
	for _, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("a create returned %d, want 200 — a non-200 here is a minting "+
				"COLLISION: two creates claimed the same number and the second "+
				"INSERT was refused by the id primary key. (Before T-52917b's "+
				"review this same fault answered 200 and lost a row instead.)", c)
		}
	}

	// ① 🔴 THE LOAD-BEARING ASSERTION. One create ⇒ one durable row. It outlives
	// ⓪ on purpose: ⓪ only sees a collision that this one INSERT refuses, while
	// this counts what actually survived, however it went missing.
	var rows int
	if err := api.dal.rdb.QueryRow(`SELECT COUNT(*) FROM task`).Scan(&rows); err != nil {
		t.Fatalf("count task rows: %v", err)
	}
	if rows != n {
		t.Fatalf("ROW COUNT %d, want %d — %d create_task calls all answered 200 but "+
			"%d task row(s) are missing. A create reported success and left no "+
			"durable row: either the create INSERT grew a conflict clause again "+
			"(taskWriteInsertOnly, dal_tasks.go) or an error on the write path is "+
			"being swallowed", rows, n, n, n-rows)
	}

	// ② the ids the API HANDED BACK must also be n distinct values. If the API
	// returned the same id twice while the table happens to hold n rows, the
	// caller was lied to about which task is theirs.
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("create_task returned id %q twice", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Fatalf("distinct returned ids = %d, want %d", len(seen), n)
	}

	// ③ the FORMAT: T-<遞增整數>, not "t-" + random hex.
	nums := map[int]bool{}
	for id := range seen {
		if !taskSeqIDRe.MatchString(id) {
			t.Fatalf("task id %q is not T-<遞增整數>", id)
		}
		v, err := strconv.Atoi(id[2:])
		if err != nil {
			t.Fatalf("task id %q: %v", id, err)
		}
		if nums[v] {
			t.Fatalf("number %d minted twice", v)
		}
		nums[v] = true
	}
	if len(nums) != n {
		t.Fatalf("distinct numbers = %d, want %d", len(nums), n)
	}
}

// TestSequentialCreatesMintAscendingIDs pins the plain, uncontended case: the
// first task on a fresh database is T-1 and each next one is larger.
func TestSequentialCreatesMintAscendingIDs(t *testing.T) {
	api := newTasksTestServer(t)
	prev := 0
	for i := 0; i < 3; i++ {
		task := createAdHocTask(t, api, "m-exec")
		if !taskSeqIDRe.MatchString(task.ID) {
			t.Fatalf("task id %q is not T-<遞增整數>", task.ID)
		}
		v, _ := strconv.Atoi(task.ID[2:])
		if i == 0 && v != 1 {
			t.Fatalf("first task on a fresh db is %q, want T-1", task.ID)
		}
		if v <= prev {
			t.Fatalf("id %q did not ascend past %d", task.ID, prev)
		}
		prev = v
	}
}

// TestLegacyRandomHexTaskIDsStayUsable is the 舊票不動 guard: a task whose id is
// the OLD "t-" + 12 random hex must still be readable and drivable, and minting
// a new task must not disturb it.
func TestLegacyRandomHexTaskIDsStayUsable(t *testing.T) {
	api := newTasksTestServer(t)
	const legacy = "t-72dd79b666d0"
	now := nowSecs()
	if err := api.dal.PutTask(Task{
		ID: legacy, Title: "legacy task", Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-exec", CreatedTS: now, UpdatedTS: now,
	}); err != nil {
		t.Fatalf("seed legacy task: %v", err)
	}

	// readable by its exact id
	got, err := api.dal.GetTask(legacy)
	if err != nil || got == nil {
		t.Fatalf("GetTask(%q) = %v, %v — a legacy task must stay readable", legacy, got, err)
	}
	if got.ID != legacy {
		t.Fatalf("id came back as %q, want %q — legacy ids must not be rewritten", got.ID, legacy)
	}

	// readable through the REAL get_task endpoint too, by the same exact id
	if view := getTaskView(t, api, legacy); view.ID != legacy {
		t.Fatalf("get_task answered id %q, want %q", view.ID, legacy)
	}

	// drivable: a plan lands on it through the real endpoint
	view := submitPlan(t, api, legacy, "m-exec", []map[string]any{
		{"name": "step one", "dod": "done"},
	})
	if len(view.Steps) != 1 {
		t.Fatalf("legacy task took %d steps, want 1 — it must stay drivable", len(view.Steps))
	}

	// and DRIVEN: a step transition through the real endpoint moves it. This is
	// the half a read-only check would miss — the write path resolves the task by
	// the same byte-exact id and must not care what shape it is.
	if code := reportStepStatus(t, api, legacy, view.Steps[0].ID,
		"m-exec", StepStatusInProgress, "").Code; code != http.StatusOK {
		t.Fatalf("driving a legacy task's step answered %d, want 200", code)
	}
	driven := getTaskView(t, api, legacy)
	if driven.Steps[0].Status != StepStatusInProgress {
		t.Fatalf("legacy task step is %q, want %q — a legacy task must stay drivable",
			driven.Steps[0].Status, StepStatusInProgress)
	}

	// and minting a NEW task leaves it alone
	fresh := createAdHocTask(t, api, "m-exec")
	if fresh.ID == legacy {
		t.Fatalf("new mint collided with the legacy id")
	}
	still, err := api.dal.GetTask(legacy)
	if err != nil || still == nil || still.Title != "legacy task" {
		t.Fatalf("legacy task damaged by a new mint: %v %v", still, err)
	}
}

// TestMintRetryExhaustionIs500WithNoOrphanRow pins the one branch of the mint
// that had ZERO coverage: what a caller sees when mintTaskNumber's
// compare-and-set loop runs out of attempts (T-52917b review, 建議 3).
//
// 🔴 HOW THE EXHAUSTION IS FORCED, and why it is not a fake. The loop exits
// through this branch when the CAS reports RowsAffected=0 mintRetryLimit times
// running. Under production settings that is unreachable — the write pool is one
// connection and Begin is IMMEDIATE, so nothing can move the counter between
// this transaction's read and its claim (see the long comment on mintRetryLimit;
// an external writer does not even get past BEGIN). Rather than stub the loop
// out, the test makes the DATABASE refuse the claim: a BEFORE UPDATE trigger on
// task_id_seq that RAISE(IGNORE)s. The UPDATE is then skipped and sqlite3_changes
// reports 0 — the exact signal the loop reads as "somebody moved it" — so the
// REAL loop runs its REAL bound and exits through the REAL error path.
//
// What is pinned is the OUTCOME the ticket cares about, measured end to end
// through the real create handler:
//   - the request fails LOUDLY: HTTP 500, wire code "internal_error"
//   - it leaves ZERO orphan rows, because mint and insert share one transaction
func TestMintRetryExhaustionIs500WithNoOrphanRow(t *testing.T) {
	api := newTasksTestServer(t)

	// positive control FIRST: without the trigger this very request is a 200.
	// Without it, a test that always 500s (a typo'd body, a missing member)
	// would look exactly like a pass.
	if rec := postCreateTask(t, api, "control"); rec.Code != http.StatusOK {
		t.Fatalf("control create answered %d, want 200 — the 500 asserted below "+
			"would not be evidence of anything", rec.Code)
	}
	var before int
	if err := api.dal.rdb.QueryRow(`SELECT COUNT(*) FROM task`).Scan(&before); err != nil {
		t.Fatalf("count: %v", err)
	}
	if before != 1 {
		t.Fatalf("control left %d rows, want 1", before)
	}

	// now make every CAS report 0 rows.
	if _, err := api.dal.wdb.Exec(`
		CREATE TRIGGER t52917b_refuse_claim BEFORE UPDATE ON task_id_seq
		BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatalf("install refusing trigger: %v", err)
	}
	// prove the trigger really does what the test needs, so a SQLite that stopped
	// honouring RAISE(IGNORE) turns this test red instead of green-for-free.
	res, err := api.dal.wdb.Exec(`UPDATE task_id_seq SET next = next + 1 WHERE id = 1`)
	if err != nil {
		t.Fatalf("probe update: %v", err)
	}
	if ra, err := res.RowsAffected(); err != nil || ra != 0 {
		t.Fatalf("with the refusing trigger installed an UPDATE reported %d rows "+
			"(err %v), want 0 — this test cannot exhaust the retry loop without "+
			"it, and would be asserting nothing", ra, err)
	}

	rec := postCreateTask(t, api, "exhausted")

	// ① loud, not silent.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a create whose mint cannot claim a number answered %d, want 500 "+
			"— running out of retries must FAIL, never fall through to a task "+
			"with an unclaimed id", rec.Code)
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v (body %q)", err, rec.Body.String())
	}
	if envelope.Error.Code != "internal_error" {
		t.Fatalf("error code %q, want %q", envelope.Error.Code, "internal_error")
	}
	// the message must name the counter, or the on-call reading the 500 has
	// nothing to go on.
	if !strings.Contains(envelope.Error.Message, "task_id_seq") {
		t.Fatalf("500 message %q does not mention task_id_seq — an exhausted mint "+
			"must say what ran out", envelope.Error.Message)
	}

	// ② 🔴 ZERO ORPHANS. mint and insert share one transaction, so a failed mint
	// rolls the whole create back. A row here would mean a task exists that the
	// caller was told does not.
	var after int
	if err := api.dal.rdb.QueryRow(`SELECT COUNT(*) FROM task`).Scan(&after); err != nil {
		t.Fatalf("count: %v", err)
	}
	if after != before {
		t.Fatalf("task rows went %d → %d across a create that answered 500 — a "+
			"failed mint must leave NO orphan row", before, after)
	}
}

// postCreateTask drives the REAL create handler once and hands back the recorder.
func postCreateTask(t *testing.T, api *apiServer, title string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": title, "executor_member_id": "m-exec"},
		"m-exec", "agent"))
	return rec
}

// TestGetTaskAcceptsTheTaskNoTheAgentWasHanded is T-1's guard, and it exists
// because prose alone does not stay true.
//
// 🔴 WHAT WENT WRONG THAT THIS PREVENTS. Before T-5291 the display number was
// the id's first hex quartet, so 「get_task("T-6f44")」 really did 404, and
// seeds/task_closeout.md correctly told every closing agent to detour through
// list_tasks to trade its number for an id. T-5291 made TaskNo(id) the identity
// function — and NOTHING IN THE TREE NOTICED. The seed kept teaching the
// detour, and kept asserting 「⚠️ `get_task` 只吃 `id`，餵票號會 404」, for as
// long as it took a human to spot it by hand: a live agent-facing sentence,
// loaded on every close-out, costing one wasted call each time and false while
// it did so. T-1 rewrote the sentence. THIS is the part that keeps it rewritten.
//
// The property is deliberately NOT "task_no equals id" — that is a restatement
// of TaskNo's body and would pass even if the get_task ROUTE stopped resolving
// the thing agents are handed. It is the end-to-end claim the seed now makes:
// the identifier the API reports as this task's 票號, fed verbatim to the REAL
// get_task handler, answers 200 with THIS task. Whatever a future change does
// to id shape, minting or routing, the doc is only true while this passes.
//
// 🔴 BOTH NUMBER SHAPES, because the seed sentence promises both. Legacy tasks
// keep "t-"+12-hex ids, new ones are T-<遞增整數>, and with no delete path the
// two coexist permanently — which is exactly why the seed says 「票號就是 id」
// rather than 「票號都是 T-<數字>」. A guard that only covered fresh mints would
// leave the half of the claim about old tickets unpinned.
func TestGetTaskAcceptsTheTaskNoTheAgentWasHanded(t *testing.T) {
	api := newTasksTestServer(t)

	// the legacy shape, seeded directly so its id is genuinely the old form.
	const legacy = "t-72dd79b666d0"
	now := nowSecs()
	if err := api.dal.PutTask(Task{
		ID: legacy, Title: "legacy task", Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-exec", CreatedTS: now, UpdatedTS: now,
	}); err != nil {
		t.Fatalf("seed legacy task: %v", err)
	}

	fresh := createAdHocTask(t, api, "m-exec")
	if !taskSeqIDRe.MatchString(fresh.ID) {
		t.Fatalf("the freshly minted task is %q, not T-<遞增整數> — this test would "+
			"not be covering the new shape at all", fresh.ID)
	}

	for _, tc := range []struct {
		name   string
		taskID string
	}{
		{"newly minted T-<遞增整數>", fresh.ID},
		{"legacy t-+12-hex", legacy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// ① read the number the way an AGENT gets it: off the task, as the API
			// reports it. Not TaskNo(id) — calling the function here would make the
			// test agree with the implementation by construction.
			view := getTaskView(t, api, tc.taskID)
			taskNo := view.TaskNo
			if taskNo == "" {
				t.Fatalf("the API reports no task_no for %q — an agent handed nothing "+
					"cannot read its ticket at all", tc.taskID)
			}

			// ② 🔴 THE LOAD-BEARING CALL. The 票號, verbatim, straight into the real
			// get_task route — no list_tasks in between. This is the whole of what
			// seeds/task_closeout.md now promises.
			rec := httptest.NewRecorder()
			api.HandleGetTaskApiTasksTaskIdGet(rec,
				taskReq(t, "GET", "/api/tasks/"+taskNo, nil, "owner", "owner"), taskNo)
			if rec.Code != http.StatusOK {
				t.Fatalf("get_task(%q) — the task_no the API itself reported — answered "+
					"%d, want 200. seeds/task_closeout.md tells every closing agent "+
					"「先 `get_task` 讀這張票（票號就是 id，直接餵給它）」, and that sentence "+
					"is now FALSE. Either restore the route so a 票號 resolves, or take "+
					"the seed back to the owner (the last such rewrite was rc-63068f315a7c) "+
					"— do not leave the document teaching a call that fails. Body: %s",
					taskNo, rec.Code, rec.Body.String())
			}

			// ③ 200 is not enough: it must be THIS task. A route that resolved every
			// number to some task would satisfy ② and still lie to the caller.
			got := decodeBody[taskDTO](t, rec)
			if got.ID != tc.taskID {
				t.Fatalf("get_task(%q) answered 200 but handed back task %q, want %q — "+
					"the number an agent is shown must address the ticket it is shown on",
					taskNo, got.ID, tc.taskID)
			}
		})
	}
}
