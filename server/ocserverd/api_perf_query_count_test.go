package main

// api_perf_query_count_test.go — T-a3e4 的收尾:把「查詢次數」本身變成被守住的性質。
//
// 為什麼要多這一個檔案:`api_perf_status_set_test.go` 釘的是 dep 解析的
// 「答案」(被 filter 排除的 dep 也講得出名字),而那個性質**逐 dep
// `s.dal.GetTask(id)` 一樣答得出來**——只是每多一個 dep 就多一次 round trip。
// review 實測過:把 handler 的 join 改寫成逐 dep 查詢,整包 `go test` 一條都沒紅。
// 這是效能票,payload 修好卻換來 N+1 等於沒修,所以這裡量的是**次數**,不是答案。
//
// 量測面是 database/sql 的 **driver seam**,不是 DAL 上的欄位:計數器活在測試檔裡,
// production 一個 byte 都沒動,而且它是**文字比對 SQL**、不是掛在某個 DAL method 上,
// 所以 `GetTask`、手寫一句 `SELECT`、或任何別的 DAL method 走的都是同一條路。
//
// 🔴 **但它不是「看得到每一條」——它認的是一份具名的形狀清單,射程之外一律漏數,
// 而漏數的方向是誤綠。** 射程與邊界由 `TestTaskTableReadPatternKnowsItsOwnBoundary`
// 逐條釘住(那是護欄自己的測試:邊界要被斷言,不能只寫在註解裡)。**仍在射程外**的
// 已知形狀:讀取透過 VIEW / CTE 名字 / 子查詢別名間接發生(`FROM (SELECT … )` 之後
// 對別名取用)、`FROM /*註解*/ task` 這種中間夾註解、以及本 package 以外的碼。
// ⚠️ **部分漏數比 seam 全死更危險**:合法的 `ListTasks` 那一次仍會被數到,所以下面
// 那道 `> 0` 自檢**不會響**——一個用射程外寫法做的 N+1 會安靜地全綠。review 實測過
// 這件事(M7:`SELECT … FROM "task" WHERE id = ?`,舊 regexp 停在 1 次、測試全綠)。
//
// 🔴 反恆真:語料非空是**先斷言的**。如果那一跑根本沒有帶相依的任務,查詢次數當然
// 不會成長——那時「次數沒成長」什麼都沒證明。所以下面先確認回應裡真的有 dep、
// 兩個情境的 dep 數真的不同,而且計數器真的數到東西(> 0),才比較次數。

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// ── the driver seam ──────────────────────────────────────────────────────────

// taskTableRead matches a read of the `task` table itself.
//
// 涵蓋:`FROM` 與 `JOIN` 兩個動詞、可選的 schema 限定(`main.task`)、以及 SQLite
// 合法的四種 identifier 引號(`"task"` / `'task'` / 反引號 / `[task]`)。
// 排除:`task_step` / `task_dep` / `task_artifact`(輕量清單也會發的那幾句 grouped
// COUNT)——`_` 是 word character,所以 `task\b` 不匹配,加引號的分支也要求引號緊貼
// 在 `task` 之後,`"task_step"` 同樣不匹配。
//
// 🔴 這個 pattern 的射程就是這條護欄的射程,所以它自己有測試
// (`TestTaskTableReadPatternKnowsItsOwnBoundary`);射程外的形狀見檔頭。
//
// ⚠️ **它只認 `task` 這一張表,而且是寫死在上面那串字面值裡的。** 想拿這個
// counting driver 去量別的表(`chat_message`、`member`、`reply_card` …),
// 就得**自己換掉比對字串**——照抄整個 seam 但沿用這個 pattern,計數器會對
// 那張表恆回 0,而 `> 0` 那道自檢是拿**合法的那一次 `ListTasks`** 滿足的,
// 所以它**不會響**:你會拿到一份「零次查詢」的漂亮報告。換字串時連
// `TestTaskTableReadPatternKnowsItsOwnBoundary` 的兩側語料一起換,否則新的
// pattern 沒有任何東西在釘它的邊界(例:量 `chat_message` 時,`task` 這邊靠
// `\b` 排除 `task_step` 的那招對 `chat_message` 沒有對應物,得自己想)。
var taskTableRead = regexp.MustCompile(
	`(?i)\b(?:from|join)\s+(?:[a-z_][a-z0-9_]*\.)?` +
		"(?:\"task\"|'task'|`task`|\\[task\\]|task\\b)")

// TestTaskTableReadPatternKnowsItsOwnBoundary 直接對字串斷言 pattern 的兩側。
//
// 為什麼要有這一條:M7 反例證明了「N+1 只要換一種合法的 identifier 寫法就整條漏數」,
// 而漏數的方向是**誤綠**。護欄的邊界必須被釘住,不能只寫在註解裡——這正是這張票的
// 精神:不要用推論代替覆蓋。
func TestTaskTableReadPatternKnowsItsOwnBoundary(t *testing.T) {
	mustMatch := []string{
		// 現行 DAL 的兩種真實寫法(改動時這兩條會先紅)。
		"SELECT id FROM task ORDER BY created_ts",
		"SELECT id FROM task WHERE id = ?",
		// M7 反例本體:雙引號 identifier,SQLite 合法。
		`SELECT title, status FROM "task" WHERE id = ?`,
		"SELECT title FROM 'task' WHERE id = ?",
		"SELECT title FROM `task` WHERE id = ?",
		"SELECT title FROM [task] WHERE id = ?",
		// schema 限定。
		"SELECT id FROM main.task WHERE id = ?",
		`SELECT id FROM main."task" WHERE id = ?`,
		// JOIN 動詞(alias 與否都算)。
		"SELECT t.id FROM task_dep d JOIN task t ON t.id = d.dep_id",
		"SELECT t.id FROM task_dep d JOIN task ON task.id = d.dep_id",
		// 大小寫與換行(SQL 常這樣排版)。
		"select id\n\tfrom\n\ttask\n\twhere id = ?",
	}
	for _, q := range mustMatch {
		if !taskTableRead.MatchString(q) {
			t.Errorf("必須算成 task 表讀取,卻漏數了(誤綠方向): %q", q)
		}
	}

	mustNotMatch := []string{
		// 輕量清單自己發的三句 grouped COUNT — 數進去會讓基準膨脹、把 N+1 藏在雜訊裡。
		"SELECT task_id, COUNT(*) FROM task_step GROUP BY task_id",
		"SELECT task_id, dep_id FROM task_dep",
		"SELECT task_id, COUNT(*) FROM task_artifact GROUP BY task_id",
		`SELECT task_id FROM "task_step"`,
		"SELECT task_id FROM main.task_dep",
		"SELECT id FROM task_manual",
		"SELECT id FROM taskish",
		// 只是提到 task 這個字,不是從它讀。
		"SELECT task_id FROM chat_message",
		"UPDATE task SET title = ?",
	}
	for _, q := range mustNotMatch {
		if taskTableRead.MatchString(q) {
			t.Errorf("不該算成 task 表讀取,卻數進去了: %q", q)
		}
	}
}

// taskStepTableRead matches a read of the `task_step` table itself — the SECOND
// barrel of this guard (T-66).
//
// 🔴 WHY IT HAD TO EXIST. `taskTableRead` above deliberately EXCLUDES
// `task_step` (see its mustNotMatch corpus), so until this pattern was added the
// counting driver was blind to step reads: a per-task `ListTaskSteps` inside the
// list handler — the dumbest possible N+1, one round trip per row on an
// UNCAPPED endpoint — left `TestTaskListTaskReadsDoNotGrowWithDepCount` fully
// GREEN. That was measured, not reasoned about: the planted N+1 left the guard
// at `--- PASS`, and the raw transcript is pinned to task T-66 as an artifact
// (it is NOT a file in this repo — do not go looking for one).
// The hole was in the PATTERN, not the seam.
//
// It is a SEPARATE counter rather than a widening of `taskTableRead` on purpose:
// the two tables have different constant budgets (one `task` read vs. the light
// list's several grouped `task_step` statements), so folding them together would
// have meant loosening the `task` bound to fit the step traffic — trading a
// tight assertion for a slack one.
//
// ⚠️ Same射程 caveats as `taskTableRead`: reads through a VIEW, a CTE name, or a
// subquery ALIAS (`FROM (SELECT … ) x` then `FROM x`) are NOT counted, and the
// miss direction is 誤綠. The inner `FROM task_step` of a subquery IS counted,
// because the literal text is still in the statement.
//
// 🔴 WHAT THIS GUARD DOES AND DOES NOT SEE — read this before trusting it.
// The property it守住 is 「次數不隨 dep 數 / 母體成長」, measured by running the
// SAME request against two databases whose POPULATION sizes differ. So it sees
// exactly one shape of N+1: a per-row read over a set that GROWS with the
// fixture.
// 🔴 IT USED TO BE BLIND TO THE MOST NATURAL PLACEMENT OF ALL, and that gap is
// now closed — this paragraph replaces an earlier one that said the gap would be
// left open. The measurement, not the reasoning: the test's only request carried
// `?statuses=in_progress`, which returns ONE row in BOTH runs, so a
// `ListTaskSteps(t.ID)` planted immediately before the newTaskListItemDTO call —
// i.e. right where a per-row step read would naturally be written — cost exactly
// 1 extra query in both, the two tallies stayed equal, the constant bound
// (`few.step > 3`) still passed, and the whole guard came back GREEN on a real
// N+1. Only reads placed BEFORE the filters (e.g. inside the byID build) grew
// with the fixture and reddened it.
//
// The second axis it needed was not a bigger population but a bigger ANSWER, so
// the test now also measures an UNFILTERED request over the same two
// populations, where the loop body really does execute once per returned row
// (listTaskReadsUnfilteredFor). Both axes are kept: the filtered runs are the
// ones that catch a read placed before the filters, which the unfiltered runs
// cannot tell apart from honest per-population work.
//
// 執行者判斷 (T-66; 沒有卡號,沒有人向 owner 請示過這一格): both shapes are worth
// one measurement each, and a guard that names its own blind spot in a comment
// is worse than no comment once the blind spot is gone.
var taskStepTableRead = regexp.MustCompile(
	`(?i)\b(?:from|join)\s+(?:[a-z_][a-z0-9_]*\.)?` +
		"(?:\"task_step\"|'task_step'|`task_step`|\\[task_step\\]|task_step\\b)")

// TestTaskStepTableReadPatternKnowsItsOwnBoundary is taskStepTableRead's own
// edge test — the same discipline TestTaskTableReadPatternKnowsItsOwnBoundary
// applies to the `task` pattern. A guard whose射程 is only described in a comment
// is not a guard.
func TestTaskStepTableReadPatternKnowsItsOwnBoundary(t *testing.T) {
	mustMatch := []string{
		// The two real light-list statements (these two ARE the budget).
		"SELECT task_id, COUNT(*) FROM task_step GROUP BY task_id",
		"SELECT task_id, id, name FROM task_step WHERE status != ? AND status != ?",
		// The N+1 shape this pattern exists to catch (dal.ListTaskSteps).
		"SELECT id, task_id, name FROM task_step WHERE task_id = ? ORDER BY order_idx, id",
		// The four SQLite identifier quotings, and a schema qualifier.
		`SELECT task_id FROM "task_step"`,
		"SELECT task_id FROM 'task_step'",
		"SELECT task_id FROM `task_step`",
		"SELECT task_id FROM [task_step]",
		"SELECT task_id FROM main.task_step",
		`SELECT task_id FROM main."task_step"`,
		// JOIN, and the window-function form's INNER FROM (the alias itself is
		// out of射程; the inner text is not).
		"SELECT s.id FROM task t JOIN task_step s ON s.task_id = t.id",
		"SELECT task_id, id FROM (SELECT task_id, id FROM task_step) WHERE rn = 1",
		// Case + newlines.
		"select task_id\n\tfrom\n\ttask_step\n\twhere task_id = ?",
	}
	for _, q := range mustMatch {
		if !taskStepTableRead.MatchString(q) {
			t.Errorf("必須算成 task_step 讀取,卻漏數了(誤綠方向): %q", q)
		}
	}

	mustNotMatch := []string{
		// The OTHER tables — including `task` itself, which has its own counter.
		"SELECT id FROM task ORDER BY created_ts",
		`SELECT title FROM "task" WHERE id = ?`,
		"SELECT task_id, dep_id FROM task_dep",
		"SELECT task_id, COUNT(*) FROM task_artifact GROUP BY task_id",
		"SELECT id FROM task_step_archive",
		// Merely naming the column, not reading the table.
		"SELECT step_id FROM reply_card",
		// Writes are not reads.
		"UPDATE task_step SET status = ?",
	}
	for _, q := range mustNotMatch {
		if taskStepTableRead.MatchString(q) {
			t.Errorf("不該算成 task_step 讀取,卻數進去了: %q", q)
		}
	}
}

// queryCounter counts task-table reads while armed. Arming is explicit so that
// migrations and seeding (which run before the request under test) cannot
// contribute — the number has to be "what ONE list request costs".
type queryCounter struct {
	mu      sync.Mutex
	armed   bool
	n       int
	seenSQL []string
	// The task_step barrel (T-66): its own tally, because the two tables have
	// different constant budgets — see taskStepTableRead.
	nStep       int
	seenStepSQL []string
}

func (c *queryCounter) note(q string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.armed {
		return
	}
	flat := strings.Join(strings.Fields(q), " ")
	if taskTableRead.MatchString(q) {
		c.n++
		c.seenSQL = append(c.seenSQL, flat)
	}
	if taskStepTableRead.MatchString(q) {
		c.nStep++
		c.seenStepSQL = append(c.seenStepSQL, flat)
	}
}

// queryTally is one armed window's reading: how many statements touched `task`
// and how many touched `task_step`, plus the statements themselves (they are
// what makes a failure readable).
type queryTally struct {
	task      int
	taskStmts []string
	step      int
	stepStmts []string
}

// arm resets and starts counting; the returned func stops it and reports the
// tally.
func (c *queryCounter) arm() func() queryTally {
	c.mu.Lock()
	c.armed, c.n, c.seenSQL = true, 0, nil
	c.nStep, c.seenStepSQL = 0, nil
	c.mu.Unlock()
	return func() queryTally {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.armed = false
		return queryTally{
			task: c.n, taskStmts: c.seenSQL,
			step: c.nStep, stepStmts: c.seenStepSQL,
		}
	}
}

var (
	countingOnce     sync.Once
	countingInnerErr error
	countingRegistry struct {
		mu sync.Mutex
		m  map[string]*queryCounter
	}
)

const countingDriverName = "sqlite-taskquerycount"

// registerCountingDriver wraps the real sqlite driver ONCE under a second name.
// The inner driver is taken from a throwaway *sql.DB rather than imported, so
// this file stays independent of which sqlite package the server picks.
func registerCountingDriver() error {
	countingOnce.Do(func() {
		probe, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			countingInnerErr = err
			return
		}
		inner := probe.Driver()
		probe.Close()
		countingRegistry.m = map[string]*queryCounter{}
		sql.Register(countingDriverName, countingDriver{inner: inner})
	})
	return countingInnerErr
}

func counterFor(dsn string) *queryCounter {
	countingRegistry.mu.Lock()
	defer countingRegistry.mu.Unlock()
	c := countingRegistry.m[dsn]
	if c == nil {
		c = &queryCounter{}
		countingRegistry.m[dsn] = c
	}
	return c
}

type countingDriver struct{ inner driver.Driver }

func (d countingDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &countingConn{inner: c, ctr: counterFor(dsn)}, nil
}

type countingConn struct {
	inner driver.Conn
	ctr   *queryCounter
}

func (c *countingConn) Prepare(q string) (driver.Stmt, error) {
	st, err := c.inner.Prepare(q)
	if err != nil {
		return nil, err
	}
	return &countingStmt{inner: st, sql: q, ctr: c.ctr}, nil
}

func (c *countingConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	if p, ok := c.inner.(driver.ConnPrepareContext); ok {
		st, err := p.PrepareContext(ctx, q)
		if err != nil {
			return nil, err
		}
		return &countingStmt{inner: st, sql: q, ctr: c.ctr}, nil
	}
	return c.Prepare(q)
}

func (c *countingConn) Close() error              { return c.inner.Close() }
func (c *countingConn) Begin() (driver.Tx, error) { return c.inner.Begin() }

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.inner.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.inner.Begin()
}

type countingStmt struct {
	inner driver.Stmt
	sql   string
	ctr   *queryCounter
}

func (s *countingStmt) Close() error  { return s.inner.Close() }
func (s *countingStmt) NumInput() int { return s.inner.NumInput() }

func (s *countingStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.inner.Exec(args)
}

func (s *countingStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.ctr.note(s.sql)
	return s.inner.Query(args)
}

func (s *countingStmt) ExecContext(
	ctx context.Context, args []driver.NamedValue,
) (driver.Result, error) {
	if e, ok := s.inner.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, args)
	}
	vals, err := namedToValues(args)
	if err != nil {
		return nil, err
	}
	return s.inner.Exec(vals)
}

func (s *countingStmt) QueryContext(
	ctx context.Context, args []driver.NamedValue,
) (driver.Rows, error) {
	s.ctr.note(s.sql)
	if q, ok := s.inner.(driver.StmtQueryContext); ok {
		return q.QueryContext(ctx, args)
	}
	vals, err := namedToValues(args)
	if err != nil {
		return nil, err
	}
	return s.inner.Query(vals)
}

func namedToValues(args []driver.NamedValue) ([]driver.Value, error) {
	out := make([]driver.Value, len(args))
	for _, a := range args {
		if a.Name != "" {
			return nil, errors.New("counting driver: named args unsupported")
		}
		if a.Ordinal < 1 || a.Ordinal > len(out) {
			return nil, fmt.Errorf("counting driver: bad ordinal %d", a.Ordinal)
		}
		out[a.Ordinal-1] = a.Value
	}
	return out, nil
}

// newCountingDAL is newTestDAL with the counting driver in front of it. Same
// DSN as openSQLite so the file behaves identically (WAL, immediate txlock).
func newCountingDAL(t *testing.T) (*DAL, *queryCounter) {
	t.Helper()
	if err := registerCountingDriver(); err != nil {
		t.Fatalf("register counting driver: %v", err)
	}
	path := filepath.Join(t.TempDir(), "querycount.db")
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(" + sqliteJournalMode + ")&_txlock=immediate"
	ctr := counterFor(dsn)
	db, err := sql.Open(countingDriverName, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return NewDAL(db), ctr
}

// ── the test ─────────────────────────────────────────────────────────────────

// seedDepFanout creates one live task blocked by depN DONE tasks (plus the
// blockers themselves, which the status filter will exclude from the response
// — that is the shape the dep join exists for). Returns the blocked task id.
func seedDepFanout(t *testing.T, s *apiServer, depN int) string {
	t.Helper()
	blockedID := "t-fanout000001"
	if err := s.dal.PutTask(Task{
		ID: blockedID, Title: "被擋的", Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-1", CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	deps := make([]string, 0, depN)
	for i := 0; i < depN; i++ {
		id := fmt.Sprintf("t-dep%010d", i)
		if err := s.dal.PutTask(Task{
			ID: id, Title: fmt.Sprintf("阻擋者 %d", i), Status: TaskStatusDone,
			Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
			ExecutorID: "m-1", CreatedTS: 100, UpdatedTS: 100, ClosedTS: 200,
		}); err != nil {
			t.Fatal(err)
		}
		deps = append(deps, id)
	}
	if err := s.dal.ReplaceTaskDeps(blockedID, deps); err != nil {
		t.Fatal(err)
	}
	return blockedID
}

// listTaskReadsFor runs ONE ?statuses=in_progress list request against a fresh
// database seeded with depN deps, and returns the query tally (task AND
// task_step reads, with the statements) plus the resolved deps observed on the
// wire.
func listTaskReadsFor(t *testing.T, depN int) (tally queryTally, resolvedDeps int) {
	t.Helper()
	dal, ctr := newCountingDAL(t)
	s := &apiServer{dal: dal, hub: NewHub()}
	blockedID := seedDepFanout(t, s, depN)

	stop := ctr.arm()
	rows := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusInProgress),
	})
	tally = stop()

	if len(rows) != 1 || rows[0].ID != blockedID {
		t.Fatalf("depN=%d: expected only the blocked row, got %v", depN, idsOf(rows))
	}
	// 🔴 語料自證:每個 dep 都必須真的被解析出來(有標題)。沒有這一段,一個
	// 「dep 一個都沒帶」的跑法會讓次數斷言恆真地過。
	for _, d := range rows[0].DepTasks {
		if d.Title == "" || d.Status == "" {
			t.Fatalf("depN=%d: dep %s 沒被解析,語料不合格:%+v", depN, d.ID, d)
		}
		resolvedDeps++
	}
	if resolvedDeps != depN {
		t.Fatalf("depN=%d: 只解析到 %d 個 dep — 語料不合格", depN, resolvedDeps)
	}
	return tally, resolvedDeps
}

// listTaskReadsUnfilteredFor is listTaskReadsFor's UNFILTERED twin: the same
// seeded population, but the request carries NO ?statuses=, so EVERY seeded task
// comes back on the wire and the handler's per-row loop body really does run
// once per returned row.
//
// 🔴 WHY BOTH EXIST, and what the filtered one alone cannot see. The filtered run
// narrows the ANSWER to one row, so a per-row read that sits after the filter
// `continue`s — the most natural place to write one, next to the row projection
// — fires once no matter how large the population is, and every filtered
// assertion stays green on a real N+1. That was measured, not reasoned about.
// Keep BOTH: the filtered run is the one that catches a read placed BEFORE the
// filters (e.g. inside the byID build), which the unfiltered run cannot
// distinguish from honest once-per-population work.
func listTaskReadsUnfilteredFor(t *testing.T, depN int) (tally queryTally, rowsOut int) {
	t.Helper()
	dal, ctr := newCountingDAL(t)
	s := &apiServer{dal: dal, hub: NewHub()}
	seedDepFanout(t, s, depN)

	stop := ctr.arm()
	rows := listTaskRows(t, s, HandleListTasksApiTasksGetParams{})
	tally = stop()

	// 🔴 語料自證:整個母體都必須真的落到回應上。這一支的全部價值就在「回幾列」
	// 會隨 depN 成長;如果它其實也只回一列,下面的等式就退化成 filtered 那一組,
	// 什麼新的都沒測到。
	if len(rows) != depN+1 {
		t.Fatalf("depN=%d: 未過濾的清單該回 %d 列(1 張被擋的 + %d 張阻擋者),"+
			"得到 %d — 語料不合格", depN, depN+1, depN, len(rows))
	}
	return tally, len(rows)
}

func TestTaskListTaskReadsDoNotGrowWithDepCount(t *testing.T) {
	const fewDeps, manyDeps = 2, 25

	few, fewResolved := listTaskReadsFor(t, fewDeps)
	many, manyResolved := listTaskReadsFor(t, manyDeps)

	// ── 語料非空(先於任何次數斷言)────────────────────────────────────────
	if fewResolved == 0 || manyResolved <= fewResolved {
		t.Fatalf("語料不合格:兩跑必須都帶 dep 且數量不同(few=%d many=%d)",
			fewResolved, manyResolved)
	}
	// 計數器自己也要自證活著:如果 seam 死了(driver 沒被套上、regexp 不match),
	// 0 == 0 會讓下面的等式恆真地過。BOTH barrels have to be alive — the step
	// one was added in T-66 precisely because a dead barrel reads as green.
	if few.task == 0 {
		t.Fatalf("計數器沒數到任何 task 讀取 — seam 壞了,這一跑什麼都沒證明")
	}
	if few.step == 0 {
		t.Fatalf("計數器沒數到任何 task_step 讀取 — 這一跑對 N+1 的 step 面什麼都沒證明" +
			"(輕量清單本來就會發 grouped 的 progress / current-step 各一句)")
	}

	// ── 被守住的性質(一)`task` 表 ───────────────────────────────────────
	// MUTANT(review 實測過的那一個):把 handler 建 byID 的那一段換成逐 dep
	// `s.dal.GetTask(id)`,many.task 會從 few.task 拉開 23 次,這裡就紅。
	if many.task != few.task {
		t.Fatalf("一次 list 請求的 task 讀取次數隨 dep 數成長了:%d 個 dep → %d 次,"+
			"%d 個 dep → %d 次(N+1)\nfew:\n  %s\nmany:\n  %s",
			fewDeps, few.task, manyDeps, many.task,
			strings.Join(few.taskStmts, "\n  "), strings.Join(many.taskStmts, "\n  "))
	}
	// 而且它是一個小常數,不是「同樣地大」。ListTasks 是唯一該打到 task 表的
	// 那一句;寬到 2 是留給未來一句合理的拆分,25 個 dep 的 N+1 進不來。
	if few.task > 2 {
		t.Fatalf("一次 list 請求打了 %d 次 task 表,預期 1 次(ListTasks):\n  %s",
			few.task, strings.Join(few.taskStmts, "\n  "))
	}

	// ── 被守住的性質(二)`task_step` 表(T-66)────────────────────────────
	// current_step_id/current_step_name 的來源必須是一句 grouped 查詢,和
	// AllTaskStepProgress 同一個形狀。MUTANT:在 handler 的迴圈裡放一句逐票
	// `s.dal.ListTaskSteps(t.ID)`,many.step 就會隨母體(1 + dep 數)成長。
	// 🔴 在這個 barrel 加進來之前,那顆 mutant 是全綠的 —— 種下去跑,護欄回
	// `--- PASS`;補上之後同一顆(shasum 逐字相同)回「2 個 dep → 5 次,25 個
	// dep → 28 次(N+1)」。兩段逐字輸出釘在 T-66 這張票上,**不是 repo 裡的
	// 檔案**,不要去找。
	if many.step != few.step {
		t.Fatalf("一次 list 請求的 task_step 讀取次數隨母體成長了:%d 個 dep → %d 次,"+
			"%d 個 dep → %d 次(N+1)\nfew:\n  %s\nmany:\n  %s",
			fewDeps, few.step, manyDeps, many.step,
			strings.Join(few.stepStmts, "\n  "), strings.Join(many.stepStmts, "\n  "))
	}
	// 常數上界:輕量清單目前該打 2 句 —— AllTaskStepProgress(進度)與
	// AllTaskCurrentStep(當前步驟)。寬到 3 留一句未來的合理拆分。
	if few.step > 3 {
		t.Fatalf("一次 list 請求打了 %d 次 task_step 表,預期 2 次"+
			"(AllTaskStepProgress + AllTaskCurrentStep):\n  %s",
			few.step, strings.Join(few.stepStmts, "\n  "))
	}

	// ── 被守住的性質(三)未過濾的母體 ────────────────────────────────────
	// 🔴 上面兩段量的是一個「只回一列」的請求(?statuses=in_progress):filter
	// 之後的迴圈體只跑一次,所以一句放在 filter 之後、緊鄰 newTaskListItemDTO
	// 的逐票查詢 —— N+1 最自然的落點 —— 在那裡是看不見的。那顆 mutant 實測是
	// 全綠的。這一段用同一個母體、但不帶任何 filter,讓迴圈體真的每張票都跑一
	// 次,那顆 mutant 才會紅。兩組都留著:filtered 那組守的是「放在 filter
	// 之前」的讀取,unfiltered 這組分不出來。
	fewU, fewRows := listTaskReadsUnfilteredFor(t, fewDeps)
	manyU, manyRows := listTaskReadsUnfilteredFor(t, manyDeps)

	// ── 語料非空(先於任何次數斷言)──────────────────────────────────────
	if manyRows <= fewRows {
		t.Fatalf("語料不合格:兩跑回的列數必須不同(few=%d many=%d),"+
			"否則這一組和 filtered 那一組量的是同一件事", fewRows, manyRows)
	}
	if fewU.task == 0 || fewU.step == 0 {
		t.Fatalf("計數器在未過濾這一跑什麼都沒數到(task=%d step=%d)— seam 壞了,"+
			"下面的等式會恆真地過", fewU.task, fewU.step)
	}

	// MUTANT(本輪實測過的那一顆):在 handler 迴圈裡、filter 之後、緊鄰
	// newTaskListItemDTO 放一句 `s.dal.ListTaskSteps(t.ID)`。filtered 那兩段全綠,
	// 這一句紅:3 列 → 5 次,26 列 → 28 次。
	if manyU.step != fewU.step {
		t.Fatalf("未過濾的 list 請求,task_step 讀取次數隨回傳列數成長了:%d 列 → %d 次,"+
			"%d 列 → %d 次(N+1)\nfew:\n  %s\nmany:\n  %s",
			fewRows, fewU.step, manyRows, manyU.step,
			strings.Join(fewU.stepStmts, "\n  "), strings.Join(manyU.stepStmts, "\n  "))
	}
	// `task` 表同理 —— 逐列 GetTask 也可以躲在 filter 後面。
	if manyU.task != fewU.task {
		t.Fatalf("未過濾的 list 請求,task 讀取次數隨回傳列數成長了:%d 列 → %d 次,"+
			"%d 列 → %d 次(N+1)\nfew:\n  %s\nmany:\n  %s",
			fewRows, fewU.task, manyRows, manyU.task,
			strings.Join(fewU.taskStmts, "\n  "), strings.Join(manyU.taskStmts, "\n  "))
	}
}
