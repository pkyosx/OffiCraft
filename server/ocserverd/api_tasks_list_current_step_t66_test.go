package main

// api_tasks_list_current_step_t66_test.go — T-66: the light task list carries
// `current_step_id` / `current_step_name`, so 「這張票現在卡在哪一步」 no longer
// costs a get_task per row.
//
// 🔴 WHAT THIS FILE PINS, and why each part is load-bearing:
//
//   1. AGREEMENT WITH get_task. The list value is computed by a SQL twin
//      (dal.AllTaskCurrentStep, one grouped window query) of the in-memory rule
//      (domain.CurrentStep, which resumeTasksFor also calls). Two definitions of
//      "the current step" is how they drift, so the assertion is not "the list
//      says step 2" — a literal that would still pass if BOTH sides were wrong —
//      but "the list agrees with the step rows get_task returns", derived from
//      the response, not from the fixture's expectations.
//   2. THE TWO EMPTY CASES. "" is a real answer, not a failure: an empty plan
//      and a fully-finished plan both mean THERE IS NO CURRENT STEP. The
//      tempting bug is to fall back to the first row, which would tell an agent
//      to re-do finished work, so both are pinned separately.
//   3. superseded IS TERMINAL. A superseded row is frozen replan history
//      (T-1aea) — it must be SKIPPED even when it sits first in order_idx.
//      Without this case a `status != done` implementation passes everything
//      else here.
//
// The query-count side (this must never become a per-task step read) is a
// different property and lives in api_perf_query_count_test.go, whose
// `task_step` barrel was added by this same ticket.

import (
	"fmt"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
)

// t66Server builds a bare api server on a fresh DAL — the same shape the other
// task tests use.
func t66Server(t *testing.T) *apiServer {
	t.Helper()
	return &apiServer{dal: newTestDAL(t), hub: NewHub()}
}

// t66PutTask seeds one live task.
func t66PutTask(t *testing.T, s *apiServer, id, title string) {
	t.Helper()
	if err := s.dal.PutTask(Task{
		ID: id, Title: title, Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-1", CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
}

// t66PutStep seeds one step. dod is deliberately non-empty on every row: the
// light list must carry the step's id and NAME and never its dod, and a test
// whose fixtures had empty dod could not tell the difference.
func t66PutStep(t *testing.T, s *apiServer, taskID, id string, idx int, name, status string) {
	t.Helper()
	if err := s.dal.PutTaskStep(TaskStep{
		ID: id, TaskID: taskID, OrderIdx: idx, Name: name,
		DoD:    "DoD for " + name + " — fat text the light list must not carry",
		Status: status,
	}); err != nil {
		t.Fatal(err)
	}
}

// t66ListRow reads the light list and returns the one row for taskID.
func t66ListRow(t *testing.T, s *apiServer, taskID string) taskListItemDTO {
	t.Helper()
	for _, row := range listTaskRows(t, s, HandleListTasksApiTasksGetParams{}) {
		if row.ID == taskID {
			return row
		}
	}
	t.Fatalf("task %s missing from the light list", taskID)
	return taskListItemDTO{}
}

// t66CurrentStepPerGetTask derives the current step from what GET
// /api/tasks/{id} actually returned — the OTHER endpoint's answer, in its own
// step order, using the shared terminal rule. This is the oracle: it never
// reads the list's own value, so agreement is evidence rather than tautology.
func t66CurrentStepPerGetTask(t *testing.T, s *apiServer, taskID string) (id, name string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "owner", "owner"), taskID)
	if rec.Code != 200 {
		t.Fatalf("get_task %s → %d: %s", taskID, rec.Code, rec.Body.String())
	}
	full := decodeBody[taskDTO](t, rec)
	for _, st := range full.Steps {
		if !StepIsTerminal(st.Status) {
			return st.ID, st.Name
		}
	}
	return "", ""
}

// TestTaskListCurrentStepAgreesWithGetTask is the core pin: for a plan with a
// finished head, a superseded row and two live rows, the list's pointer is the
// SAME step get_task shows as current.
func TestTaskListCurrentStepAgreesWithGetTask(t *testing.T) {
	s := t66Server(t)
	const taskID = "t-cur0000000001"
	t66PutTask(t, s, taskID, "有計畫的票")
	// order_idx ascending; the first two are terminal, so the answer is s3.
	t66PutStep(t, s, taskID, "ts-c1", 1, "盤點現況", StepStatusDone)
	t66PutStep(t, s, taskID, "ts-c2", 2, "舊的做法(已被改寫)", StepStatusSuperseded)
	t66PutStep(t, s, taskID, "ts-c3", 3, "實作", StepStatusInProgress)
	t66PutStep(t, s, taskID, "ts-c4", 4, "驗收", StepStatusPending)

	wantID, wantName := t66CurrentStepPerGetTask(t, s, taskID)
	// 語料自證:oracle 必須真的指到東西,否則下面的等式在兩邊都空時恆真。
	if wantID == "" || wantName == "" {
		t.Fatalf("語料不合格:get_task 沒有當前步驟可比(id=%q name=%q)", wantID, wantName)
	}
	if wantID != "ts-c3" {
		t.Fatalf("oracle 本身就錯了:預期 ts-c3(第一個非終態),得到 %q — "+
			"superseded 是凍結的改寫歷史,不是工作中的節點", wantID)
	}

	row := t66ListRow(t, s, taskID)
	if row.CurrentStepID != wantID || row.CurrentStepName != wantName {
		t.Fatalf("list 的當前步驟和 get_task 不一致:list=(%q, %q) get_task=(%q, %q)",
			row.CurrentStepID, row.CurrentStepName, wantID, wantName)
	}
	// 名字要是真的名字,不是 id 的複製品。
	if row.CurrentStepName != "實作" {
		t.Fatalf("current_step_name 應為步驟名 '實作',得到 %q", row.CurrentStepName)
	}
}

// TestTaskListCurrentStepIsEmptyWithoutAPlan pins empty case #1: a task with no
// steps at all. Both fields must be "" — the honest 「還沒有計畫」.
func TestTaskListCurrentStepIsEmptyWithoutAPlan(t *testing.T) {
	s := t66Server(t)
	const taskID = "t-cur0000000002"
	t66PutTask(t, s, taskID, "還沒排計畫的票")

	wantID, wantName := t66CurrentStepPerGetTask(t, s, taskID)
	if wantID != "" || wantName != "" {
		t.Fatalf("oracle 不合格:沒有步驟卻算出當前步驟 (%q, %q)", wantID, wantName)
	}
	row := t66ListRow(t, s, taskID)
	if row.CurrentStepID != "" || row.CurrentStepName != "" {
		t.Fatalf("計畫為空時兩格都該是空字串,得到 (%q, %q) — "+
			"不能拿第一步當預設,那會是憑空發明的工作",
			row.CurrentStepID, row.CurrentStepName)
	}
	// 反恆真:同一次回應裡,一張有計畫的票必須仍然講得出當前步驟,
	// 否則「兩格是空的」也可能只是欄位整條壞掉。
	const liveID = "t-cur0000000003"
	t66PutTask(t, s, liveID, "有計畫的票")
	t66PutStep(t, s, liveID, "ts-d1", 1, "動工", StepStatusInProgress)
	if got := t66ListRow(t, s, liveID); got.CurrentStepID != "ts-d1" {
		t.Fatalf("對照組壞了:有計畫的票也沒有當前步驟(%q)— 這一跑什麼都沒證明",
			got.CurrentStepID)
	}
}

// TestTaskListCurrentStepIsEmptyWhenEveryStepIsFinished pins empty case #2:
// every step has reached a TERMINAL state (done, or superseded). Falling back to
// the first row here would point an agent at work that is already finished.
func TestTaskListCurrentStepIsEmptyWhenEveryStepIsFinished(t *testing.T) {
	s := t66Server(t)
	const taskID = "t-cur0000000004"
	t66PutTask(t, s, taskID, "全部做完的票")
	t66PutStep(t, s, taskID, "ts-e1", 1, "盤點現況", StepStatusDone)
	t66PutStep(t, s, taskID, "ts-e2", 2, "舊的做法(已被改寫)", StepStatusSuperseded)
	t66PutStep(t, s, taskID, "ts-e3", 3, "實作", StepStatusDone)

	wantID, wantName := t66CurrentStepPerGetTask(t, s, taskID)
	if wantID != "" || wantName != "" {
		t.Fatalf("oracle 不合格:全部終態卻算出當前步驟 (%q, %q)", wantID, wantName)
	}
	// 語料自證:這張票真的有步驟(不是空計畫走錯了分支)。
	row := t66ListRow(t, s, taskID)
	if row.ProgressTotal == 0 {
		t.Fatalf("語料不合格:這一跑的票根本沒有步驟,測不到「全部完成」這個情境")
	}
	if row.CurrentStepID != "" || row.CurrentStepName != "" {
		t.Fatalf("全部完成時兩格都該是空字串,得到 (%q, %q) — "+
			"回退到第一步會叫人重做已完成的工作",
			row.CurrentStepID, row.CurrentStepName)
	}
}

// stepStatusConstDecl pulls the step-status VOCABULARY out of domain.go itself
// rather than out of a list typed here.
//
// 🔴 WHY THE CORPUS IS DERIVED. The test below claims that the SQL rule
// (dal.AllTaskCurrentStep, which hard-codes `status != done AND status !=
// superseded`) and the Go rule (domain.CurrentStep → StepIsTerminal) agree.
// A HAND-TYPED fixture list makes that claim only about the statuses somebody
// remembered to type. Add a THIRD terminal state to StepIsTerminal tomorrow and
// the SQL will NOT follow — but if the new status never appears in a fixture,
// no row disagrees and this test stays green while the two rules have already
// drifted. Deriving the corpus from the constant block means a new
// `StepStatusX = "x"` walks into the fixtures on its own.
//
// The regexp is deliberately anchored on the `StepStatus… = "…"` const form,
// which is the only shape that block has ever used; if it is ever reshaped the
// floor assertion below (the six statuses known when this was written must all
// come back) reddens instead of the corpus silently emptying.
var stepStatusConstDecl = regexp.MustCompile(`StepStatus[A-Za-z]+\s*=\s*"([a-z_]+)"`)

// t66AllStepStatuses returns every declared step status, in declaration order.
func t66AllStepStatuses(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("domain.go")
	if err != nil {
		t.Fatalf("read domain.go (the vocabulary source): %v", err)
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range stepStatusConstDecl.FindAllStringSubmatch(string(src), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	// 反恆真 #1:語料不能是空的,也不能只剩一兩個。
	if len(out) < 6 {
		t.Fatalf("只從 domain.go 抽出 %d 個 step 狀態(%v)— 抽取壞了,"+
			"這一跑的「每個狀態都測過」是假的", len(out), out)
	}
	// 反恆真 #2:抽出來的每一個都必須是真的 step 狀態(不是別的常數被誤抓)。
	for _, st := range out {
		if !ValidStepStatus(st) {
			t.Fatalf("抽到 %q,但 ValidStepStatus 不認它 — 抽取抓錯了東西", st)
		}
	}
	// 反恆真 #3:寫這支測試時已知的六個必須全在。這是唯一手列的東西,而它是
	// 一道 FLOOR(下限),不是語料本身 — 新增的狀態靠上面的抽取自己走進來。
	for _, want := range []string{
		StepStatusPending, StepStatusInProgress, StepStatusWaitingOwner,
		StepStatusWaitingExternal, StepStatusDone, StepStatusSuperseded,
	} {
		if !seen[want] {
			t.Fatalf("語料少了已知狀態 %q — 抽取沒抓到它,語料的涵蓋度是假的", want)
		}
	}
	return out
}

// TestTaskListCurrentStepRuleIsTheSharedOne asserts the single-source-of-truth
// claim directly: dal.AllTaskCurrentStep (SQL) and domain.CurrentStep (memory)
// must give the same answer — through the LIST WIRE, not just the DAL — for
// every declared step status, in two plan shapes each, plus the no-plan case.
//
// 🔴 THE MUTANT THIS EXISTS FOR: add a third terminal state to StepIsTerminal
// and leave dal.AllTaskCurrentStep's hard-coded `!= done AND != superseded`
// alone. Go then skips a step SQL still points at, and the two rules disagree on
// exactly the tasks whose plan contains that status — which is why the corpus
// has to contain EVERY status, derived, rather than the ones somebody typed.
// Verified by planting that mutant (add waiting_external to StepIsTerminal and
// leave the SQL alone): four assertions plus the anti-vacuity floor all fire.
// The transcript is pinned to task T-66 as an artifact — it is NOT a file in
// this repo, so do not go looking for one.
func TestTaskListCurrentStepRuleIsTheSharedOne(t *testing.T) {
	s := t66Server(t)
	statuses := t66AllStepStatuses(t)

	// Two plan shapes per status, so a status is exercised both as the HEAD of
	// the plan and as the row sitting behind a finished one (the position where
	// "first non-terminal" actually has to make a choice).
	type fixture struct {
		id    string
		shape string
		steps [][2]string // [stepID, status]
	}
	var fixtures []fixture
	for i, st := range statuses {
		alone := fixture{
			id:    fmt.Sprintf("t-st%02dalone01", i),
			shape: "只有一步(" + st + ")",
			steps: [][2]string{{fmt.Sprintf("ts-%02da1", i), st}},
		}
		behind := fixture{
			id:    fmt.Sprintf("t-st%02dbehind1", i),
			shape: "done 之後接一步(" + st + ")",
			steps: [][2]string{
				{fmt.Sprintf("ts-%02db1", i), StepStatusDone},
				{fmt.Sprintf("ts-%02db2", i), st},
			},
		}
		fixtures = append(fixtures, alone, behind)
	}
	// 沒有計畫的票 — 兩邊都必須說「沒有當前步驟」。
	fixtures = append(fixtures, fixture{id: "t-stnoplan001", shape: "沒有計畫"})

	for _, f := range fixtures {
		t66PutTask(t, s, f.id, "票 "+f.shape)
		for idx, st := range f.steps {
			t66PutStep(t, s, f.id, st[0], idx+1, "步驟 "+st[0], st[1])
		}
	}

	sqlSide, err := s.dal.AllTaskCurrentStep()
	if err != nil {
		t.Fatalf("AllTaskCurrentStep: %v", err)
	}
	nonEmpty := 0
	for _, f := range fixtures {
		steps, err := s.dal.ListTaskSteps(f.id)
		if err != nil {
			t.Fatal(err)
		}
		wantID, wantName := CurrentStep(steps) // the Go rule = the single source
		// (a) the SQL twin, read straight off the DAL …
		got := sqlSide[f.id] // absent = zero value = ("", ""), the rule's own answer
		if got.ID != wantID || got.Name != wantName {
			t.Errorf("%s [%s]:SQL 與 domain.CurrentStep 不一致:SQL=(%q, %q) domain=(%q, %q)"+
				" — 兩個「當前步驟」的定義已經漂移了",
				f.id, f.shape, got.ID, got.Name, wantID, wantName)
		}
		// (b) … and the same answer as it reaches the WIRE.
		row := t66ListRow(t, s, f.id)
		if row.CurrentStepID != wantID || row.CurrentStepName != wantName {
			t.Errorf("%s [%s]:list 回應與 domain.CurrentStep 不一致:wire=(%q, %q) domain=(%q, %q)",
				f.id, f.shape, row.CurrentStepID, row.CurrentStepName, wantID, wantName)
		}
		if wantID != "" {
			nonEmpty++
		}
	}

	// 反恆真:如果每一格都空,上面的等式全部是 ""=="",什麼都沒證明。今天有
	// 四個非終態 × 兩種計畫形狀 = 8 張票該指得出當前步驟;寫成不等式而不是
	// 等式,是為了新增一個非終態狀態時這裡不會無謂地紅。
	if nonEmpty < 8 {
		t.Fatalf("語料不合格:只有 %d 張票算得出當前步驟(預期至少 8),"+
			"兩邊都空的等式證明不了任何事", nonEmpty)
	}
	// 而且 SQL 那一邊不能替沒有當前步驟的票憑空生一列出來。
	if _, ok := sqlSide["t-stnoplan001"]; ok {
		t.Errorf("沒有計畫的票不該出現在 AllTaskCurrentStep 的結果裡")
	}
}
