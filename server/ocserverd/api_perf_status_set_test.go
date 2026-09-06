package main

// api_perf_status_set_test.go — T-a3e4, the two halves of "勾什麼就問什麼":
//
//   * GET /api/tasks?statuses=…  (repeatable) — answer the SET the cockpit's
//     狀態 dropdown ticked, instead of shipping every live task and letting the
//     browser hide most of them.
//   * TaskListItemDTO.dep_tasks — resolve each dep to the facts the
//     「等 <task id> <標題>」 row prints, so naming an already-CLOSED blocker no
//     longer requires the client to download the closed population.
//
// Measured baseline this replaces (origin/main e7120c5, the live workshop DB):
// GET /api/tasks = 408,482 B (703 rows) vs ?open=true = 17,295 B — and the page
// asked for the first one on every task SSE delta, because one live task with a
// dep was enough to switch the fast path off.
//
// Iron rule kept from api_perf_params_test.go: the DEFAULT path and the existing
// ?status= / ?open= behaviour are byte-for-byte unchanged (asserted here, not
// assumed), and each new filter is a STRICT narrowing — the negative assertions
// are the load-bearing ones.

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func strsptr(v ...string) *[]string { return &v }

// listTaskRows reads the light list and returns the decoded rows (the id-only
// helper next door cannot see dep_tasks).
func listTaskRows(
	t *testing.T, s *apiServer, params HandleListTasksApiTasksGetParams,
) []taskListItemDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListTasksApiTasksGet(rec, perfReq("owner", "owner"), params)
	if rec.Code != 200 {
		t.Fatalf("list tasks → %d: %s", rec.Code, rec.Body.String())
	}
	var rows []taskListItemDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rows
}

func listTasksCode(
	t *testing.T, s *apiServer, params HandleListTasksApiTasksGetParams,
) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListTasksApiTasksGet(rec, perfReq("owner", "owner"), params)
	return rec.Code, rec.Body.String()
}

func idsOf(rows []taskListItemDTO) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r.ID] = true
	}
	return out
}

// ── A. ?statuses= answers exactly the ticked set ─────────────────────────────

func TestTasksStatusSetReturnsExactlyThoseStatuses(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedTasksMix(t, s) // 4 live (one per non-terminal status) + 3 terminal

	rows := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusNotStarted, TaskStatusDone),
	})
	got := idsOf(rows)
	// The VALUES are the contract: a filter that returned "fewer rows" but the
	// wrong states would satisfy any count-based assertion.
	if len(rows) != 2 || !got["t-open1"] || !got["t-done1"] {
		t.Fatalf("statuses=[not_started,done] must return exactly those two: %v", got)
	}
	// MUTANT: delete the `len(statusSet) > 0 && !taskStatusSetMatch(...)` guard in
	// the handler and every one of these leaks back in.
	for _, id := range []string{"t-open2", "t-open3", "t-open4", "t-term1", "t-dup1"} {
		if got[id] {
			t.Fatalf("statuses set leaked %s: %v", id, got)
		}
	}

	// A single-element set is the same rule, not a special case.
	one := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusWaitingExternal),
	})
	if len(one) != 1 || one[0].ID != "t-open4" {
		t.Fatalf("statuses=[waiting_external] → %v", idsOf(one))
	}
}

func TestTasksStatusSetMatchesTheReassigningLock(t *testing.T) {
	// 🔴 The one value in the set vocabulary that is NOT a status column: T-9ca5
	// moved reassigning onto the orthogonal task.lock, but the cockpit's 狀態
	// dropdown still lists 轉派中 and its DEFAULT view ticks it. If the server
	// matched only the column, the default request would silently drop every
	// handover-locked task — or the page would have to give up filtering and
	// download the archive again, which is the bug being fixed.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	mk := func(id, status, lock string) {
		if err := s.dal.PutTask(Task{
			ID: id, Title: id, Status: status, Lock: lock,
			Priority: TaskPriorityMid, ExecutorKind: TaskExecutorStaff,
			ExecutorID: "m-1", CreatedTS: 1000, UpdatedTS: 1000,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("t-locked", TaskStatusInProgress, TaskLockReassigning)
	mk("t-plain", TaskStatusInProgress, TaskLockNone)
	mk("t-waiting", TaskStatusWaitingOwner, TaskLockNone)

	only := idsOf(listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusReassigning),
	}))
	if !only["t-locked"] {
		t.Fatal("statuses=[reassigning] must match the handover LOCK")
	}
	if only["t-plain"] || only["t-waiting"] {
		t.Fatalf("statuses=[reassigning] must not match unlocked rows: %v", only)
	}

	// It is an OR, not a replacement: a locked task whose own status is also
	// ticked appears once, and a set without reassigning ignores the lock.
	both := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusInProgress, TaskStatusReassigning),
	})
	if len(both) != 2 {
		t.Fatalf("locked row must not be duplicated: %v", idsOf(both))
	}
	waiting := idsOf(listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusWaitingOwner),
	}))
	if waiting["t-locked"] {
		t.Fatalf("a set without reassigning must not pull locked rows: %v", waiting)
	}
}

func TestTasksStatusSetIgnoresAResidualLockOnAClosedTask(t *testing.T) {
	// 🔴 Found by review, not by me. `closeTask` never clears `t.Lock` and the
	// terminate guard only reads the STATUS, so "reassign, then change your mind
	// and terminate" leaves `status=terminated, lock=reassigning` in the DB
	// permanently. The 狀態 dropdown ticks 轉派中 BY DEFAULT ⇒ without the
	// terminal guard in taskStatusSetMatch, a default view the owner never
	// touched surfaces a CLOSED task — and one the old ?open=true path could not
	// have returned, so it is also a divergence from the behaviour being replaced.
	//
	// The parity assertion below is the point: the default set and ?open=true must
	// AGREE about this row. Asserting only "not returned" would still pass if the
	// set filter had gone wrong in some other direction.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	mk := func(id, status, lock string, closed float64) {
		if err := s.dal.PutTask(Task{
			ID: id, Title: id, Status: status, Lock: lock,
			Priority: TaskPriorityMid, ExecutorKind: TaskExecutorStaff,
			ExecutorID: "m-1", CreatedTS: 1000, UpdatedTS: 1000, ClosedTS: closed,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("t-residue", TaskStatusTerminated, TaskLockReassigning, 2000) // the residue
	mk("t-live-handover", TaskStatusInProgress, TaskLockReassigning, 0)

	// The DEFAULT cockpit ask: the five non-terminal states, 轉派中 included.
	defaultSet := strsptr(TaskStatusNotStarted, TaskStatusInProgress,
		TaskStatusWaitingOwner, TaskStatusWaitingExternal, TaskStatusReassigning)
	got := idsOf(listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: defaultSet,
	}))
	// MUTANT: drop `&& !TaskIsTerminal(t.Status)` from taskStatusSetMatch → red.
	if got["t-residue"] {
		t.Fatalf("a TERMINATED task with a residual handover lock must not read as "+
			"轉派中 in the default view: %v", got)
	}
	// The other direction, in the same test: a real in-flight handover still shows.
	if !got["t-live-handover"] {
		t.Fatalf("a genuinely reassigning (open) task must still be returned: %v", got)
	}

	// Parity with the path this replaces — both must exclude the residue row.
	open := idsOf(listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Open: strptr("true"),
	}))
	if open["t-residue"] {
		t.Fatalf("?open=true must not return the residue row either: %v", open)
	}
	if got["t-residue"] != open["t-residue"] {
		t.Fatalf("default set and ?open=true disagree about the residue row: "+
			"set=%v open=%v", got["t-residue"], open["t-residue"])
	}

	// Ticking 已終止 explicitly IS how you see it — the row is not unreachable,
	// it is just not 轉派中.
	byStatus := idsOf(listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusTerminated),
	}))
	if !byStatus["t-residue"] {
		t.Fatalf("statuses=[terminated] must still return it: %v", byStatus)
	}
}

func TestTasksStatusSetRejectsAnUnknownValueByName(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedTasksMix(t, s)
	code, body := listTasksCode(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusDone, "nonsense"),
	})
	if code != 400 {
		t.Fatalf("unknown status in the set must 400, got %d: %s", code, body)
	}
	// Naming the offender is the point: silently dropping it would narrow the
	// answer without telling anyone.
	if !strings.Contains(body, "nonsense") {
		t.Fatalf("400 body must name the rejected value: %s", body)
	}
}

func TestTasksStatusSetEmptyAndBlankMeanNoConstraint(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	openIDs, terminalIDs := seedTasksMix(t, s)
	total := len(openIDs) + len(terminalIDs)
	// `?statuses=` (one blank value) and an empty repetition both mean 所有狀態 —
	// the SAME thing an omitted param means. A blank that filtered would make
	// 清除篩選 return nothing at all.
	for _, params := range []HandleListTasksApiTasksGetParams{
		{Statuses: strsptr("")},
		{Statuses: strsptr()},
		{Statuses: strsptr(" ")},
		{},
	} {
		if n := len(listTaskRows(t, s, params)); n != total {
			t.Fatalf("%+v must not filter: want %d rows, got %d", params, total, n)
		}
	}
}

func TestTasksLegacyStatusAndOpenParamsAreUnchanged(t *testing.T) {
	// The frozen half. A live client sends `?status=` / `?open=true` / nothing;
	// none of those may behave differently now that a second filter exists.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	openIDs, terminalIDs := seedTasksMix(t, s)

	single := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Status: strptr(TaskStatusDone),
	})
	if len(single) != 1 || single[0].ID != "t-done1" {
		t.Fatalf("?status=done → %v", idsOf(single))
	}
	// ?status=reassigning was, and stays, a 400: ValidTaskStatus rejects it. The
	// SET accepts it (previous test) — widening the single param instead would
	// have changed an answer a live client already relies on.
	if code, _ := listTasksCode(t, s, HandleListTasksApiTasksGetParams{
		Status: strptr(TaskStatusReassigning),
	}); code != 400 {
		t.Fatalf("?status=reassigning must still 400, got %d", code)
	}
	if n := len(listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Open: strptr("true"),
	})); n != len(openIDs) {
		t.Fatalf("?open=true → %d rows, want %d", n, len(openIDs))
	}
	if n := len(listTaskRows(t, s, HandleListTasksApiTasksGetParams{})); n != len(openIDs)+len(terminalIDs) {
		t.Fatalf("unfiltered → %d rows, want %d", n, len(openIDs)+len(terminalIDs))
	}

	// Every filter present is ANDed — including the two status filters, which
	// intersect rather than one silently winning.
	both := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Status:   strptr(TaskStatusDone),
		Statuses: strsptr(TaskStatusNotStarted, TaskStatusDone),
	})
	if len(both) != 1 || both[0].ID != "t-done1" {
		t.Fatalf("status=done AND statuses=[not_started,done] → %v", idsOf(both))
	}
	openAndSet := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Open:     strptr("true"),
		Statuses: strsptr(TaskStatusNotStarted, TaskStatusDone),
	})
	if len(openAndSet) != 1 || openAndSet[0].ID != "t-open1" {
		t.Fatalf("open=true AND statuses=[not_started,done] → %v", idsOf(openAndSet))
	}
}

// ── B. dep_tasks: the server names the blockers ──────────────────────────────

// seedBlockedTask creates one live task blocked by a DONE task and a dangling id.
func seedBlockedTask(t *testing.T, s *apiServer) (blockedID, doneID, ghostID string) {
	t.Helper()
	blockedID, doneID, ghostID = "t-blocked00001", "t-done000000002", "t-ghost0000dead"
	mk := func(id, title, status string, closed float64) {
		if err := s.dal.PutTask(Task{
			ID: id, Title: title, Status: status, Priority: TaskPriorityMid,
			ExecutorKind: TaskExecutorStaff, ExecutorID: "m-1",
			CreatedTS: 1000, UpdatedTS: 1000, ClosedTS: closed,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk(blockedID, "被擋的", TaskStatusInProgress, 0)
	mk(doneID, "先把 SSE 重連補起來", TaskStatusDone, 2000)
	if err := s.dal.ReplaceTaskDeps(blockedID, []string{doneID, ghostID}); err != nil {
		t.Fatal(err)
	}
	return
}

func TestTaskListNamesADepTheFilterExcluded(t *testing.T) {
	// 🔴 The load-bearing one. The request asks ONLY for in_progress, so the DONE
	// blocker is not in the response at all — and its title/status/number must
	// still be on the wire. That combination is exactly what the client could not
	// have before: it had to widen the fetch to the whole archive to name it.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	blockedID, doneID, ghostID := seedBlockedTask(t, s)

	rows := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusInProgress),
	})
	if len(rows) != 1 || rows[0].ID != blockedID {
		t.Fatalf("expected only the blocked row: %v", idsOf(rows))
	}
	deps := rows[0].DepTasks
	if len(deps) != 2 {
		t.Fatalf("dep_tasks must carry one entry per dep, got %d: %+v", len(deps), deps)
	}
	// Order mirrors `deps`, so the client can pair positionally OR by id.
	if deps[0].ID != doneID || deps[1].ID != ghostID {
		t.Fatalf("dep_tasks order/ids drifted: %+v", deps)
	}
	// MUTANT: pass nil for byID in the handler (or drop the join) and the title
	// and status below go empty — the 「等 <task id>」 row degrades to a bare number,
	// which is the owner-reported bug T-1d82 was opened for.
	if deps[0].Title != "先把 SSE 重連補起來" {
		t.Fatalf("closed dep must be NAMED: %+v", deps[0])
	}
	if deps[0].Status != TaskStatusDone {
		t.Fatalf("closed dep must carry its real status: %+v", deps[0])
	}
	if deps[0].TaskNo != TaskNo(doneID) {
		t.Fatalf("dep task_no: got %q want %q", deps[0].TaskNo, TaskNo(doneID))
	}

	// A dep whose task is GONE: number still derived (every other surface prints
	// that form), title/status EMPTY. Never a plausible-looking 尚未執行 —
	// absence of a status is what tells the client to say 查無此任務.
	if deps[1].TaskNo != TaskNo(ghostID) {
		t.Fatalf("missing dep must still carry its derived number: %+v", deps[1])
	}
	if deps[1].Title != "" || deps[1].Status != "" {
		t.Fatalf("missing dep must stay honest-empty: %+v", deps[1])
	}
}

func TestTaskListDepTasksIsAlwaysAnArray(t *testing.T) {
	// A task with no deps serves [], not null: the field is a list of facts, and
	// null would read as "no answer" in a client that distinguishes the two.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	if err := s.dal.PutTask(Task{
		ID: "t-nodeps000001", Title: "獨立", Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorStaff,
		ExecutorID: "m-1", CreatedTS: 1, UpdatedTS: 1,
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.HandleListTasksApiTasksGet(rec, perfReq("owner", "owner"),
		HandleListTasksApiTasksGetParams{})
	if !strings.Contains(rec.Body.String(), `"dep_tasks":[]`) {
		t.Fatalf("dep_tasks must serialise as [] for a dep-less task: %s", rec.Body.String())
	}
}

// ⚠️ NOT covered here: a query COUNT. This suite has no instrumentation for the
// number of SQL round trips, so nothing below can fail if someone reimplements
// the join as a per-dep GetTask. What IS pinned is the property that makes the
// single-read implementation the only easy one: deps resolve to tasks the filters
// EXCLUDED from the response (above), which no per-row lookup of the response
// itself could do. Recorded so the next reader does not mistake this file for
// proof of the query count.

// ── C. the outsource panel's row facts ride the worker DTO ───────────────────

func TestOutsourceWorkerCarriesItsBoundTaskFacts(t *testing.T) {
	// The panel used to join these itself out of the UNFILTERED task list (the
	// whole history, re-pulled on every worker/task/chat delta) plus the manuals
	// list. Both downloads are gone, so the fields have to be here — and be
	// RIGHT, which is why every assertion below is a value.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-review", DisplayName: "程式碼審查", UpdatedTS: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.dal.PutTask(Task{
		ID: "t-bound0000001", Title: "審這支 PR", TypeKey: "tm-review",
		Status: TaskStatusInProgress, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-1",
		CreatedTS: 4242, UpdatedTS: 4242,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-1", Codename: "O-7", Model: "claude-opus-5", Effort: "medium",
		TaskID: "t-bound0000001", Status: WorkerStatusActive, CreatedTS: 9,
	}); err != nil {
		t.Fatal(err)
	}
	// A second worker whose task does NOT exist — the honest-empty contrast, and
	// the reason the panel keeps a fallback sort key.
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-2", Codename: "O-8", TaskID: "t-vanished0001",
		Status: WorkerStatusAssigned, CreatedTS: 11,
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.HandleListOutsourceWorkersApiOutsourceWorkersGet(rec, perfReq("owner", "owner"))
	if rec.Code != 200 {
		t.Fatalf("list workers → %d: %s", rec.Code, rec.Body.String())
	}
	var rows []outsourceWorkerDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]outsourceWorkerDTO{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	got, ok := byID["ow-1"]
	if !ok {
		t.Fatalf("ow-1 missing: %+v", rows)
	}
	// MUTANT: drop the four task_* assignments in newOutsourceWorkerDTO and each
	// of these goes empty/zero — the panel's row loses its number, its role line
	// and its sort key.
	if got.TaskNo != TaskNo("t-bound0000001") {
		t.Fatalf("task_no: got %q", got.TaskNo)
	}
	if got.TaskCreatedTS != 4242 {
		t.Fatalf("task_created_ts (the sort key): got %v", got.TaskCreatedTS)
	}
	if got.TaskTypeKey != "tm-review" {
		t.Fatalf("task_type_key: got %q", got.TaskTypeKey)
	}
	// The manual's human label, resolved server-side (one query for the list).
	if got.TaskTypeName != "程式碼審查" {
		t.Fatalf("task_type_name: got %q", got.TaskTypeName)
	}

	unresolved := byID["ow-2"]
	if unresolved.TaskNo != "" || unresolved.TaskCreatedTS != 0 ||
		unresolved.TaskTypeKey != "" || unresolved.TaskTypeName != "" {
		t.Fatalf("an unresolvable task must leave every joined field empty: %+v", unresolved)
	}
}

func TestOutsourceWorkerOnAClosedTaskStillCarriesItsLabels(t *testing.T) {
	// 🔴 票上的驗收條件之一:綁在**已結案**任務上的外包,其任務標籤仍要正確顯示。
	// 這一格以前只是「碼路徑上沒有狀態條件、結構上安全」——一個沒有測試的推論。
	// 而它剛好是最容易被「順手最佳化」掉的一格:任務清單那一側整批在學習
	// 「別載入已結案母體」,同一句直覺套到 worker 的 join 上就會把這裡弄壞,
	// 而面板上的後果是列標籤與排序鍵一起消失(task_created_ts 是排序鍵)。
	//
	// 為什麼 worker 還 active 而任務已結案:close 只把 worker 標成 released 是
	// close-out **回報之後**的事(§6.3),中間那個窗口正是面板上看得到的狀態。
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-review", DisplayName: "程式碼審查", UpdatedTS: 1,
	}); err != nil {
		t.Fatal(err)
	}
	// 兩種終態各一張:done 與 terminated。壓成一種就分不出「擋的是 done」
	// 與「擋的是終態」。
	mk := func(taskID, workerID, codename, status string, created float64) {
		if err := s.dal.PutTask(Task{
			ID: taskID, Title: "審這支 PR", TypeKey: "tm-review",
			Status: status, Priority: TaskPriorityMid,
			ExecutorKind: TaskExecutorOutsource, ExecutorID: workerID,
			CreatedTS: created, UpdatedTS: created, ClosedTS: created + 1,
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.dal.PutOutsourceWorker(OutsourceWorker{
			ID: workerID, Codename: codename, Model: "claude-opus-5",
			Effort: "medium", TaskID: taskID, Status: WorkerStatusActive,
			CreatedTS: created,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("t-closed0000a1", "ow-done", "O-11", TaskStatusDone, 5150)
	mk("t-closed0000b2", "ow-term", "O-12", TaskStatusTerminated, 5250)

	rec := httptest.NewRecorder()
	s.HandleListOutsourceWorkersApiOutsourceWorkersGet(rec, perfReq("owner", "owner"))
	if rec.Code != 200 {
		t.Fatalf("list workers → %d: %s", rec.Code, rec.Body.String())
	}
	var rows []outsourceWorkerDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]outsourceWorkerDTO{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	// 語料自證:兩個 worker 都必須真的在回應裡,否則下面每一條斷言都是對
	// 零值做比較(那時它什麼都沒證明,只證明了 map 是空的)。
	if len(byID) != 2 {
		t.Fatalf("兩個綁已結案任務的 worker 都必須列出來,got %d: %+v", len(byID), rows)
	}
	for _, tc := range []struct {
		workerID, taskID string
		created          float64
	}{
		{"ow-done", "t-closed0000a1", 5150},
		{"ow-term", "t-closed0000b2", 5250},
	} {
		got := byID[tc.workerID]
		// MUTANT:在 worker 的 task join 上加任何 `TaskIsTerminal` 條件
		// (handler 的 GetTask 之後、或 projectWorker 裡把 task 換成 nil),
		// 這四條全部歸零/歸空,而既有那條 in_progress 的測試照樣綠。
		if got.TaskNo != TaskNo(tc.taskID) {
			t.Fatalf("%s: 已結案任務的 task_no 不見了: %q", tc.workerID, got.TaskNo)
		}
		if got.TaskCreatedTS != tc.created {
			t.Fatalf("%s: 已結案任務的 task_created_ts(排序鍵)不見了: %v",
				tc.workerID, got.TaskCreatedTS)
		}
		if got.TaskTypeKey != "tm-review" {
			t.Fatalf("%s: 已結案任務的 task_type_key 不見了: %q",
				tc.workerID, got.TaskTypeKey)
		}
		if got.TaskTypeName != "程式碼審查" {
			t.Fatalf("%s: 已結案任務的 task_type_name 不見了: %q",
				tc.workerID, got.TaskTypeName)
		}
	}

	// 單筆讀取面(面板 relocate 後的 refresh)走同一個 projection,一併釘住——
	// 兩個面各有自己的 taskTypeDisplayNames() 呼叫點,漂開不會有人知道。
	one := httptest.NewRecorder()
	s.HandleGetOutsourceWorkerApiOutsourceWorkersIdGet(
		one, perfReq("owner", "owner"), "ow-done")
	if one.Code != 200 {
		t.Fatalf("get worker → %d: %s", one.Code, one.Body.String())
	}
	var solo outsourceWorkerDTO
	if err := json.Unmarshal(one.Body.Bytes(), &solo); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if solo.TaskNo != TaskNo("t-closed0000a1") || solo.TaskTypeName != "程式碼審查" ||
		solo.TaskCreatedTS != 5150 || solo.TaskTypeKey != "tm-review" {
		t.Fatalf("單筆讀取面也必須帶已結案任務的四欄: %+v", solo)
	}
}

func TestOutsourceWorkerTypeNameFallsBackToTheRawKey(t *testing.T) {
	// A manual with no display name (or none at all) must leave task_type_name
	// empty so the client prints the raw key — the same honest fallback it had
	// when it held the manuals list itself. Filling it with the key here would
	// hide a deleted manual behind a label that looks authored.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	if err := s.dal.PutTask(Task{
		ID: "t-nomanual0001", Title: "無手冊", TypeKey: "tm-deleted",
		Status: TaskStatusInProgress, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-9",
		CreatedTS: 7, UpdatedTS: 7,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-9", Codename: "O-9", TaskID: "t-nomanual0001",
		Status: WorkerStatusActive, CreatedTS: 7,
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.HandleListOutsourceWorkersApiOutsourceWorkersGet(rec, perfReq("owner", "owner"))
	var rows []outsourceWorkerDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 worker, got %d", len(rows))
	}
	if rows[0].TaskTypeKey != "tm-deleted" {
		t.Fatalf("task_type_key must still be served: %+v", rows[0])
	}
	if rows[0].TaskTypeName != "" {
		t.Fatalf("task_type_name must stay empty for an unnamed manual: %q",
			rows[0].TaskTypeName)
	}
}

// ── the empty-state basis: /api/tasks/count carries the unfiltered total ─────

func TestTaskCountCarriesOpenAndTotal(t *testing.T) {
	// Once the list answers a status SET, an empty list cannot tell 「什麼都沒有」
	// from 「這幾個狀態裡沒有」 — and 目前沒有任務 is a claim about the workshop.
	// This count is the cheap basis for that claim; without it the page would
	// either lie or have to widen the list fetch again.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	openIDs, terminalIDs := seedTasksMix(t, s)
	rec := httptest.NewRecorder()
	s.HandleTaskCountApiTasksCountGet(rec, perfReq("owner", "owner"))
	if rec.Code != 200 {
		t.Fatalf("count → %d: %s", rec.Code, rec.Body.String())
	}
	var got taskCountDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Open != len(openIDs) {
		t.Fatalf("open: got %d want %d", got.Open, len(openIDs))
	}
	// MUTANT: serve Total: got.Open (or drop the field) and this goes red — the
	// two numbers are DIFFERENT here on purpose, so an accidental alias reddens.
	if got.Total != len(openIDs)+len(terminalIDs) {
		t.Fatalf("total: got %d want %d", got.Total, len(openIDs)+len(terminalIDs))
	}
}
