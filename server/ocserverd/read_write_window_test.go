package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The paths below read a task, decide from it, and write the whole row back.
// If the task is closed between that read and that write, the whole-row upsert
// carries the stale status and closed_ts back and reopens it.
//
// A race that depends on timing is not a test, so the close is staged by a
// driver seam instead: once the handler has read the row it is about to decide
// from (armAfter), the NEXT thing the write connection is asked to do — begin a
// transaction or run a write — first closes the task through a third,
// independent connection. That is exactly the gap: a handler that reads outside
// its transaction has already decided when the close lands, while one that
// re-reads inside the transaction sees it, because the close commits before
// BEGIN. The seam never sleeps and fires exactly once; every test asserts it
// fired, so a path that no longer crosses the gap cannot pass by accident.

const (
	windowClosedTS    = 1800000000.0
	windowClosedSQL   = `UPDATE task SET status = 'done', closed_ts = 1800000000, updated_ts = 1800000000 WHERE id = ?`
	windowAnsweredSQL = `UPDATE reply_card SET status = 'answered', answered_ts = 1800000000,
		answer_option_idxs = '[1]', answer_text = 'south — the north yard is flooded' WHERE id = ?`
)

type windowHook struct {
	mu       sync.Mutex
	armAfter string
	armed    bool
	fired    int
	fire     func()
}

func (h *windowHook) sawSQL(query string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.armAfter != "" && strings.Contains(query, h.armAfter) {
		h.armAfter = ""
		h.armed = true
		return
	}
	verb := strings.ToUpper(strings.TrimSpace(query))
	for _, w := range []string{"INSERT", "UPDATE", "DELETE", "REPLACE"} {
		if strings.HasPrefix(verb, w) {
			h.fireLocked()
			return
		}
	}
}

func (h *windowHook) sawBegin() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fireLocked()
}

func (h *windowHook) fireLocked() {
	if !h.armed {
		return
	}
	h.armed = false
	h.fired++
	h.fire()
}

func (h *windowHook) closeTaskAfterRead(t *testing.T, path, taskID, armAfter string) {
	t.Helper()
	closer, err := sql.Open("sqlite", sqliteWriteDSN(path))
	if err != nil {
		t.Fatalf("open closer: %v", err)
	}
	t.Cleanup(func() { closer.Close() })
	h.mu.Lock()
	defer h.mu.Unlock()
	h.armAfter = armAfter
	h.fire = func() {
		if _, err := closer.Exec(windowClosedSQL, taskID); err != nil {
			t.Errorf("close %s behind the handler: %v", taskID, err)
		}
	}
}

// answerCardAfterRead is closeTaskAfterRead for a card: the owner answers it
// through the third connection once the handler has read it.
func (h *windowHook) answerCardAfterRead(t *testing.T, path, cardID, armAfter string) {
	t.Helper()
	answerer, err := sql.Open("sqlite", sqliteWriteDSN(path))
	if err != nil {
		t.Fatalf("open answerer: %v", err)
	}
	t.Cleanup(func() { answerer.Close() })
	h.mu.Lock()
	defer h.mu.Unlock()
	h.armAfter = armAfter
	h.fire = func() {
		if _, err := answerer.Exec(windowAnsweredSQL, cardID); err != nil {
			t.Errorf("answer %s behind the handler: %v", cardID, err)
		}
	}
}

func (h *windowHook) wantFiredOnce(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fired != 1 {
		t.Fatalf("the close was staged %d times, want exactly 1: the request never crossed the read→write gap", h.fired)
	}
}

type windowConnector struct {
	dsn  string
	drv  driver.Driver
	hook *windowHook
}

func (c windowConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.drv.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return windowConn{inner: conn, hook: c.hook}, nil
}

func (c windowConnector) Driver() driver.Driver { return c.drv }

type windowConn struct {
	inner driver.Conn
	hook  *windowHook
}

func (c windowConn) Prepare(query string) (driver.Stmt, error) {
	c.hook.sawSQL(query)
	return c.inner.Prepare(query)
}

func (c windowConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.hook.sawSQL(query)
	if pc, ok := c.inner.(driver.ConnPrepareContext); ok {
		return pc.PrepareContext(ctx, query)
	}
	return c.inner.Prepare(query)
}

func (c windowConn) Close() error { return c.inner.Close() }

func (c windowConn) Begin() (driver.Tx, error) {
	c.hook.sawBegin()
	return c.inner.Begin() //nolint:staticcheck // the wrapped driver's own fallback
}

func (c windowConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.hook.sawBegin()
	if bt, ok := c.inner.(driver.ConnBeginTx); ok {
		return bt.BeginTx(ctx, opts)
	}
	return c.inner.Begin() //nolint:staticcheck // the wrapped driver's own fallback
}

func (c windowConn) CheckNamedValue(nv *driver.NamedValue) error {
	if nvc, ok := c.inner.(driver.NamedValueChecker); ok {
		return nvc.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// windowDAL opens the database the handlers run on with the seam on every
// connection. "split pools" is serve time (one write connection, a read pool);
// "one connection" is the NewDAL shape, where a read issued inside an open
// transaction waits on the connection that transaction holds.
func windowDAL(t *testing.T, shape string) (*DAL, *windowHook, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "window.db")
	plain, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := runMigrations(plain); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	drv := plain.Driver()
	plain.Close()

	hook := &windowHook{}
	wdb := sql.OpenDB(windowConnector{dsn: sqliteWriteDSN(path), drv: drv, hook: hook})
	wdb.SetMaxOpenConns(1)
	t.Cleanup(func() { wdb.Close() })
	switch shape {
	case "split pools":
		rdb := sql.OpenDB(windowConnector{dsn: sqliteReadDSN(path), drv: drv, hook: hook})
		rdb.SetMaxOpenConns(sqliteMaxReadConns)
		t.Cleanup(func() { rdb.Close() })
		return NewDALPools(wdb, rdb), hook, path
	case "one connection":
		return NewDAL(wdb), hook, path
	}
	t.Fatalf("unknown DAL shape %q", shape)
	return nil, nil, ""
}

var windowDALShapes = []string{"split pools", "one connection"}

// windowRequestDeadline bounds every request a window test drives. A handler
// that, inside its transaction, reads through s.dal instead of the tx waits for
// a connection the transaction itself holds on the one-connection DAL, and a
// path that takes a lock it already holds never returns either; both would
// otherwise hang until the suite's -timeout (15m in CI) and panic the whole run.
const windowRequestDeadline = 10 * time.Second

// windowRequest is apiRequest under windowRequestDeadline: the request runs on
// its own goroutine (which never calls t.Fatal) and the test fails by name when
// no answer arrives in time.
func windowRequest(t *testing.T, h http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() { answered <- apiRequest(t, h, method, target, token, body) }()
	select {
	case rec := <-answered:
		return rec
	case <-time.After(windowRequestDeadline):
		t.Fatalf("%s %s did not answer within %s: inside its transaction it waits on a second "+
			"connection (s.dal instead of the tx) or on a lock it already holds", method, target, windowRequestDeadline)
		return nil
	}
}

// windowJSON is apiJSON over windowRequest.
func windowJSON(t *testing.T, h http.Handler, method, target, token, body string) (int, map[string]any) {
	t.Helper()
	rec := windowRequest(t, h, method, target, token, body)
	var parsed any
	if raw := rec.Body.Bytes(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("non-JSON body (%d): %s", rec.Code, raw)
		}
	}
	data, _ := parsed.(map[string]any)
	return rec.Code, data
}

func windowOpenTask(id string) Task {
	task := dalTestTask(id)
	task.Status = TaskStatusInProgress
	task.Lock = ""
	task.ExecutorID = apiTestPlainAgentID
	task.WaitingReason = ""
	task.ClosedTS = 0
	task.DuplicateOf = ""
	task.ReassignedFrom = ""
	task.ReassignedFromKind = ""
	return task
}

func windowClosed(task Task) Task {
	task.Status = TaskStatusDone
	task.ClosedTS = windowClosedTS
	task.UpdatedTS = windowClosedTS
	return task
}

func windowPendingStep(id, taskID string) TaskStep {
	step := dalTestStep(id, taskID)
	step.Status = StepStatusPending
	step.ReplyCardID = ""
	step.WaitingReason = ""
	step.StartedTS = 0
	step.FinishedTS = 0
	return step
}

// windowHeldCard is a task parked in waiting_owner on a step held by a waiting
// card from kip.
func windowHeldCard(t *testing.T, d *DAL) (Task, TaskStep, ReplyCard) {
	t.Helper()
	task := windowOpenTask("T-1")
	task.Status = TaskStatusWaitingOwner
	step := dalTestStep("ts-1", task.ID)
	step.Status = StepStatusWaitingOwner
	step.ReplyCardID = "rc-1"
	step.WaitingReason = ""
	step.FinishedTS = 0
	card := ReplyCard{
		ID: "rc-1", FromMember: apiTestPlainAgentID, Kind: replyCardKindDecision,
		Summary: "which yard", Options: []ReplyCardOption{{Text: "north"}, {Text: "south"}},
		SelectMode: replyCardSelectModeSingle, Status: replyCardStatusWaiting,
		CreatedTS: 1700000100, ChatMessageID: "c-1", TaskID: task.ID, TaskStepID: step.ID,
	}
	dalPutTask(t, d, task)
	if err := d.PutTaskStep(step); err != nil {
		t.Fatalf("PutTaskStep: %v", err)
	}
	if err := d.PutReplyCard(card); err != nil {
		t.Fatalf("PutReplyCard: %v", err)
	}
	return task, step, card
}

func windowWantStep(t *testing.T, d *DAL, want TaskStep) {
	t.Helper()
	got, err := d.GetTaskStep(want.ID)
	if err != nil || got == nil {
		t.Fatalf("GetTaskStep(%q): %#v, %v", want.ID, got, err)
	}
	if *got != want {
		t.Fatalf("GetTaskStep(%q):\n got %+v\nwant %+v", want.ID, *got, want)
	}
}

func windowWantCardStatus(t *testing.T, d *DAL, cardID, want string) {
	t.Helper()
	got, err := d.GetReplyCard(cardID)
	if err != nil || got == nil {
		t.Fatalf("GetReplyCard(%q): %#v, %v", cardID, got, err)
	}
	if got.Status != want {
		t.Fatalf("card %s: status %q, want %q", cardID, got.Status, want)
	}
}

func windowWantDeps(t *testing.T, d *DAL, taskID string, want []string) {
	t.Helper()
	got, err := d.ListTaskDeps(taskID)
	if err != nil {
		t.Fatalf("ListTaskDeps: %v", err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("deps of %s: got %v, want %v", taskID, got, want)
	}
}

func windowRefuseTaskWrites(t *testing.T, d *DAL) {
	t.Helper()
	if _, err := d.wdb.Exec(
		`CREATE TRIGGER refuse_task_update BEFORE UPDATE ON task
		 BEGIN SELECT RAISE(FAIL, 'the task write fails'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

const windowTaskWriteFails = "internal error: constraint failed: the task write fails (1811)"

func TestSetTaskPriorityDecidesFromTheRowItWrites(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			dashboard := apiTestListen(t, api, "")
			hook.closeTaskAfterRead(t, path, task.ID, "FROM task WHERE id")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			hook.wantFiredOnce(t)
			if status != http.StatusConflict {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
			dalWantTask(t, d, windowClosed(task))
			dashboard.wantFrames()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"a write that fails leaves the row and fans nothing", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			windowRefuseTaskWrites(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowTaskWriteFails)
			dalWantTask(t, d, task)
			dashboard.wantFrames()
		})
	}
}

func TestSetTaskDepsDecidesFromTheRowItWrites(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			dalPutTask(t, d, windowOpenTask("T-2"))
			dashboard := apiTestListen(t, api, "")
			hook.closeTaskAfterRead(t, path, task.ID, "FROM task WHERE id")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-2"]}`)

			hook.wantFiredOnce(t)
			if status != http.StatusConflict {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
			dalWantTask(t, d, windowClosed(task))
			windowWantDeps(t, d, "T-1", nil)
			dashboard.wantFrames()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"a task write that fails takes the new edges back with it", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			dalPutTask(t, d, windowOpenTask("T-2"))
			dalPutTask(t, d, windowOpenTask("T-3"))
			if err := d.AddTaskDep("T-1", "T-2"); err != nil {
				t.Fatalf("AddTaskDep: %v", err)
			}
			windowRefuseTaskWrites(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-3"]}`)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowTaskWriteFails)
			windowWantDeps(t, d, "T-1", []string{"T-2"})
			dalWantTask(t, d, task)
			dashboard.wantFrames()
		})
	}
}

func TestUpdateStepStatusDecidesFromTheRowItWrites(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			step := windowPendingStep("ts-1", task.ID)
			if err := d.PutTaskStep(step); err != nil {
				t.Fatalf("PutTaskStep: %v", err)
			}
			dashboard := apiTestListen(t, api, "")
			hook.closeTaskAfterRead(t, path, task.ID, "FROM task_step WHERE id")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-1/status", owner, `{"status":"in_progress"}`)

			hook.wantFiredOnce(t)
			if status != http.StatusConflict {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
			dalWantTask(t, d, windowClosed(task))
			windowWantStep(t, d, step)
			dashboard.wantFrames()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"a task write that fails takes the step write back with it", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			step := windowPendingStep("ts-1", task.ID)
			if err := d.PutTaskStep(step); err != nil {
				t.Fatalf("PutTaskStep: %v", err)
			}
			windowRefuseTaskWrites(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-1/status", owner, `{"status":"in_progress"}`)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowTaskWriteFails)
			windowWantStep(t, d, step)
			dalWantTask(t, d, task)
			dashboard.wantFrames()
		})
	}
}

func TestAnsweringACardDecidesFromTheRowItWrites(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task, step, _ := windowHeldCard(t, d)
			dashboard := apiTestListen(t, api, "")
			hook.closeTaskAfterRead(t, path, task.ID, "FROM reply_card WHERE id")

			status, data := windowJSON(t, h, "POST", "/api/reply-cards/rc-1/answer", owner, `{"option_idxs":[0]}`)

			hook.wantFiredOnce(t)
			if status != http.StatusConflict {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict",
				"task 'T-1' is already closed (done) — this card is orphaned and can no longer be answered")
			dalWantTask(t, d, windowClosed(task))
			windowWantStep(t, d, step)
			windowWantCardStatus(t, d, "rc-1", replyCardStatusWaiting)
			dashboard.wantFrames()
		})
	}
	// The failing-write case for this route is TestAnsweringACardAnnouncesOnlyAfterTheWriteLands.
}

func TestExpiringACardDecidesFromTheRowItWrites(t *testing.T) {
	expiredFrame := map[string]any{
		"seq": 1, "topic": "reply_card", "op": "patch",
		"data": map[string]any{
			"entity": "reply_card", "key": "owner::rc-1", "epoch": 1, "deleted": false,
			"payload": map[string]any{"id": "rc-1", "from": "kip", "status": "expired"},
		},
		"ts": apiAnyNumber, "trigger": "owner",
	}
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task, step, _ := windowHeldCard(t, d)
			dashboard := apiTestListen(t, api, "")
			hook.closeTaskAfterRead(t, path, task.ID, "FROM reply_card WHERE id")

			status, data := windowJSON(t, h, "POST", "/api/reply-cards/rc-1/expire", owner, ``)

			hook.wantFiredOnce(t)
			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			apiWantBody(t, data, map[string]any{
				"id": "rc-1", "status": "expired", "task_id": "T-1", "step_id": "ts-1",
				"expired_ts": apiAnyNumber, "answered_ts": nil, "answer": nil,
			})
			dalWantTask(t, d, windowClosed(task))
			windowWantStep(t, d, step)
			windowWantCardStatus(t, d, "rc-1", replyCardStatusExpired)
			dashboard.wantFrames(expiredFrame)
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"a task write that fails leaves the card waiting and fans nothing", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task, step, _ := windowHeldCard(t, d)
			windowRefuseTaskWrites(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := windowJSON(t, h, "POST", "/api/reply-cards/rc-1/expire", owner, ``)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowTaskWriteFails)
			windowWantCardStatus(t, d, "rc-1", replyCardStatusWaiting)
			windowWantStep(t, d, step)
			dalWantTask(t, d, task)
			dashboard.wantFrames()
		})
	}
}

var windowKipRemovedFrame = map[string]any{
	"seq": 1, "topic": "member", "op": "remove",
	"data": map[string]any{
		"entity": "member", "key": "owner::kip", "epoch": 1, "deleted": true, "payload": nil,
	},
	"ts": apiAnyNumber, "trigger": "owner",
}

// Dismissing a member retires every waiting card it asked, through the same
// sweep that closing a task and releasing a worker use.
func TestSweepingAMembersCardsDecidesFromTheRowItWrites(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the sweep read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task, step, _ := windowHeldCard(t, d)
			kip := apiTestListen(t, api, apiTestPlainAgentID)
			hook.closeTaskAfterRead(t, path, task.ID, "FROM reply_card")

			status, data := windowJSON(t, h, "DELETE", "/api/members/kip", owner, ``)

			hook.wantFiredOnce(t)
			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			dalWantTask(t, d, windowClosed(task))
			windowWantStep(t, d, step)
			windowWantCardStatus(t, d, "rc-1", replyCardStatusExpired)
			kip.wantFrames(windowKipRemovedFrame, map[string]any{
				"seq": 2, "topic": "reply_card", "op": "patch",
				"data": map[string]any{
					"entity": "reply_card", "key": "owner::rc-1", "epoch": 2, "deleted": false,
					"payload": map[string]any{"id": "rc-1", "from": "kip", "status": "expired"},
				},
				"ts": apiAnyNumber, "trigger": "owner",
			})
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": a card answered after the sweep read it stays answered", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			windowHeldCard(t, d)
			kip := apiTestListen(t, api, apiTestPlainAgentID)
			hook.answerCardAfterRead(t, path, "rc-1", "FROM reply_card")

			status, data := windowJSON(t, h, "DELETE", "/api/members/kip", owner, ``)

			hook.wantFiredOnce(t)
			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			got, err := d.GetReplyCard("rc-1")
			if err != nil || got == nil {
				t.Fatalf("GetReplyCard(rc-1): %#v, %v", got, err)
			}
			if got.Status != "answered" || got.AnsweredTS != 1800000000 || got.ExpiredTS != 0 ||
				len(got.AnswerOptionIdxs) != 1 || got.AnswerOptionIdxs[0] != 1 ||
				got.AnswerText != "south — the north yard is flooded" {
				t.Fatalf("the answer landed in the window and must stand, got status=%q answered_ts=%v expired_ts=%v idxs=%v text=%q",
					got.Status, got.AnsweredTS, got.ExpiredTS, got.AnswerOptionIdxs, got.AnswerText)
			}
			kip.wantFrames(windowKipRemovedFrame)
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"a task write that fails leaves the card waiting", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task, step, _ := windowHeldCard(t, d)
			windowRefuseTaskWrites(t, d)
			kip := apiTestListen(t, api, apiTestPlainAgentID)

			status, data := windowJSON(t, h, "DELETE", "/api/members/kip", owner, ``)

			if status != http.StatusOK {
				t.Fatalf("the dismissal itself lands: want 200, got %d (%v)", status, data)
			}
			windowWantCardStatus(t, d, "rc-1", replyCardStatusWaiting)
			windowWantStep(t, d, step)
			dalWantTask(t, d, task)
			kip.wantFrames(windowKipRemovedFrame)
		})
	}
}

func TestOpeningACardDecidesFromTheRowItWrites(t *testing.T) {
	body := `{"kind":"decision","summary":"which yard","options":[{"text":"north"},{"text":"south"}],` +
		`"linked_task":{"task_id":"T-1","step_id":"ts-1"}}`
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, _ := newAPITestServerOn(t, d)
			kip := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			step := windowPendingStep("ts-1", task.ID)
			if err := d.PutTaskStep(step); err != nil {
				t.Fatalf("PutTaskStep: %v", err)
			}
			dashboard := apiTestListen(t, api, "")
			pushes := apiTestWebPushSink(t, api)
			hook.closeTaskAfterRead(t, path, task.ID, "FROM task_step WHERE id")

			status, data := windowJSON(t, h, "POST", "/api/reply-cards", kip, body)

			hook.wantFiredOnce(t)
			if status != http.StatusConflict {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict",
				"a card can only bind to an in_progress or waiting_owner task (is done)")
			dalWantTask(t, d, windowClosed(task))
			windowWantStep(t, d, step)
			windowWantNoCards(t, d)
			dashboard.wantFrames()
			pushes()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"a task write that fails takes the card, its message and the hold back with it", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, _ := newAPITestServerOn(t, d)
			kip := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			step := windowPendingStep("ts-1", task.ID)
			if err := d.PutTaskStep(step); err != nil {
				t.Fatalf("PutTaskStep: %v", err)
			}
			windowRefuseTaskWrites(t, d)
			dashboard := apiTestListen(t, api, "")
			pushes := apiTestWebPushSink(t, api)

			status, data := windowJSON(t, h, "POST", "/api/reply-cards", kip, body)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowTaskWriteFails)
			windowWantNoCards(t, d)
			dalWantTask(t, d, task)
			windowWantStep(t, d, step)
			dashboard.wantFrames()
			pushes()
		})
	}
}

func windowWantNoCards(t *testing.T, d *DAL) {
	t.Helper()
	var cards, chats int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM reply_card`).Scan(&cards); err != nil {
		t.Fatalf("count cards: %v", err)
	}
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM chat_message`).Scan(&chats); err != nil {
		t.Fatalf("count chat: %v", err)
	}
	if cards != 0 || chats != 0 {
		t.Fatalf("want no card and no companion message, got %d cards and %d messages", cards, chats)
	}
}

// execAfterRead is closeTaskAfterRead for any write: stmt runs through a third
// connection once the handler has read the row it decides from.
func (h *windowHook) execAfterRead(t *testing.T, path, armAfter, stmt string, args ...any) {
	t.Helper()
	other, err := sql.Open("sqlite", sqliteWriteDSN(path))
	if err != nil {
		t.Fatalf("open third connection: %v", err)
	}
	t.Cleanup(func() { other.Close() })
	h.mu.Lock()
	defer h.mu.Unlock()
	h.armAfter = armAfter
	h.fire = func() {
		if _, err := other.Exec(stmt, args...); err != nil {
			t.Errorf("write behind the handler: %v", err)
		}
	}
}

func windowPlanStep(id, taskID string, orderIdx int) TaskStep {
	step := windowPendingStep(id, taskID)
	step.OrderIdx = orderIdx
	step.ParallelGroup = ""
	step.IsGate = false
	return step
}

func windowPutSteps(t *testing.T, d *DAL, steps ...TaskStep) {
	t.Helper()
	for _, st := range steps {
		if err := d.PutTaskStep(st); err != nil {
			t.Fatalf("PutTaskStep(%s): %v", st.ID, err)
		}
	}
}

func windowWantSteps(t *testing.T, d *DAL, taskID string, want ...TaskStep) {
	t.Helper()
	got, err := d.ListTaskSteps(taskID)
	if err != nil {
		t.Fatalf("ListTaskSteps: %v", err)
	}
	if want == nil {
		want = []TaskStep{}
	}
	if got == nil {
		got = []TaskStep{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("steps of %s:\n got %+v\nwant %+v", taskID, got, want)
	}
}

// windowBoundWorker is an outsource worker bound to the task, as the scheduler
// leaves one: a close releases it.
func windowBoundWorker(t *testing.T, d *DAL, taskID string) {
	t.Helper()
	if err := d.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-abc123", Codename: "Contractor", TaskID: taskID, Status: WorkerStatusAssigned,
		Runtime: "claude", Model: "sonnet", Effort: "medium",
	}); err != nil {
		t.Fatalf("PutOutsourceWorker: %v", err)
	}
}

func windowWantWorkerStatus(t *testing.T, d *DAL, id, want string) {
	t.Helper()
	w, err := d.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("GetOutsourceWorker(%q): %#v, %v", id, w, err)
	}
	if w.Status != want {
		t.Fatalf("worker %s: status %q, want %q", id, w.Status, want)
	}
}

// windowTaskCard is a waiting card bound to the task but to no step.
func windowTaskCard(t *testing.T, d *DAL, taskID string) {
	t.Helper()
	if err := d.PutReplyCard(ReplyCard{
		ID: "rc-1", FromMember: apiTestPlainAgentID, Kind: replyCardKindDecision,
		Summary: "which yard", Options: []ReplyCardOption{{Text: "north"}, {Text: "south"}},
		SelectMode: replyCardSelectModeSingle, Status: replyCardStatusWaiting,
		CreatedTS: 1700000100, ChatMessageID: "c-1", TaskID: taskID,
	}); err != nil {
		t.Fatalf("PutReplyCard: %v", err)
	}
}

type windowCloseDoor struct {
	name, path, body string
	status           string // the status the door closes to
	agent            bool   // driven by kip, the task's executor, instead of the owner
	openAs           string // the open status the door accepts
}

var windowCloseDoors = []windowCloseDoor{
	{name: "mark done", path: "/api/tasks/T-1/mark-done", status: TaskStatusDone,
		agent: true, openAs: TaskStatusReadyForDone},
	{name: "terminate", path: "/api/tasks/T-1/mark-terminated", status: TaskStatusTerminated,
		openAs: TaskStatusInProgress},
	{name: "force done", path: "/api/tasks/T-1/force-done", body: `{}`, status: TaskStatusDone,
		openAs: TaskStatusInProgress},
	{name: "duplicated", path: "/api/tasks/T-1/mark-duplicated", body: `{"duplicate_of":"T-2"}`,
		status: TaskStatusDuplicated, openAs: TaskStatusInProgress},
}

func TestClosingATaskDecidesFromTheRowItWrites(t *testing.T) {
	for _, door := range windowCloseDoors {
		for _, shape := range windowDALShapes {
			t.Run(door.name+", "+shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
				d, hook, path := windowDAL(t, shape)
				api, h, _, token := newAPITestServerOn(t, d)
				if door.agent {
					token = apiTestAgentToken(t, api, apiTestPlainAgentID, "")
				}
				open := windowOpenTask("T-1")
				open.Status = door.openAs
				task := dalPutTask(t, d, open)
				dalPutTask(t, d, windowOpenTask("T-2"))
				dashboard := apiTestListen(t, api, "")
				hook.closeTaskAfterRead(t, path, task.ID, "FROM task WHERE id")

				status, data := windowJSON(t, h, "POST", door.path, token, door.body)

				hook.wantFiredOnce(t)
				if status != http.StatusConflict {
					t.Fatalf("want 409, got %d (%v)", status, data)
				}
				apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
				dalWantTask(t, d, windowClosed(task))
				dashboard.wantFrames()
			})
		}

		t.Run(door.name+": a close that fails to land releases no worker, retires no card and fans nothing", func(t *testing.T) {
			d, _, _ := windowDAL(t, "split pools")
			api, h, _, token := newAPITestServerOn(t, d)
			if door.agent {
				token = apiTestAgentToken(t, api, apiTestPlainAgentID, "")
			}
			open := windowOpenTask("T-1")
			open.Status = door.openAs
			task := dalPutTask(t, d, open)
			dalPutTask(t, d, windowOpenTask("T-2"))
			windowBoundWorker(t, d, task.ID)
			windowTaskCard(t, d, task.ID)
			windowRefuseTaskWrites(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := windowJSON(t, h, "POST", door.path, token, door.body)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowTaskWriteFails)
			dalWantTask(t, d, task)
			windowWantWorkerStatus(t, d, "ow-abc123", WorkerStatusAssigned)
			windowWantCardStatus(t, d, "rc-1", replyCardStatusWaiting)
			dashboard.wantFrames()
		})
	}
}

func TestReassigningATaskDecidesFromTheRowItWrites(t *testing.T) {
	const toMira = `{"target":{"kind":"staff","member_id":"mira"}}`
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			step := windowPlanStep("ts-1", task.ID, 0)
			step.Status = StepStatusInProgress
			step.StartedTS = 1700000001
			windowPutSteps(t, d, step)
			dashboard := apiTestListen(t, api, "")
			hook.closeTaskAfterRead(t, path, task.ID, "FROM task WHERE id")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, toMira)

			hook.wantFiredOnce(t)
			if status != http.StatusConflict {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
			dalWantTask(t, d, windowClosed(task))
			windowWantSteps(t, d, task.ID, step)
			dashboard.wantFrames()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": a task handed to the same member after the handler read it is not handed over twice", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			dashboard := apiTestListen(t, api, "")
			hook.execAfterRead(t, path, "FROM task WHERE id",
				`UPDATE task SET executor_id = 'mira', updated_ts = 1800000000 WHERE id = ?`, task.ID)

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, toMira)

			hook.wantFiredOnce(t)
			if status != http.StatusConflict {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict", "member 'mira' is already the task's executor")
			want := task
			want.ExecutorID = "mira"
			want.UpdatedTS = 1800000000
			dalWantTask(t, d, want)
			dashboard.wantFrames()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": a target dismissed after the handler checked it is not handed the task", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			dashboard := apiTestListen(t, api, "")
			hook.execAfterRead(t, path, "FROM member WHERE id",
				`UPDATE member SET roster_status = 'removed' WHERE id = 'mira'`)

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, toMira)

			hook.wantFiredOnce(t)
			if status != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%v)", status, data)
			}
			apiWantError(t, data, "validation_error", "target member 'mira' is not an active roster member")
			dalWantTask(t, d, task)
			dashboard.wantFrames()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"a step reset that fails takes the card expiry back with it", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			step := windowPlanStep("ts-1", task.ID, 0)
			step.Status = StepStatusInProgress
			step.StartedTS = 1700000001
			windowPutSteps(t, d, step)
			windowTaskCard(t, d, task.ID)
			if _, err := d.wdb.Exec(
				`CREATE TRIGGER refuse_step_update BEFORE UPDATE ON task_step
				 BEGIN SELECT RAISE(FAIL, 'the step write fails'); END`); err != nil {
				t.Fatalf("create trigger: %v", err)
			}
			dashboard := apiTestListen(t, api, "")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, toMira)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", "internal error: constraint failed: the step write fails (1811)")
			windowWantCardStatus(t, d, "rc-1", replyCardStatusWaiting)
			windowWantSteps(t, d, task.ID, step)
			dalWantTask(t, d, task)
			dashboard.wantFrames()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"a task write that fails takes the card expiry and the step reset back with it", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task, step, _ := windowHeldCard(t, d)
			windowRefuseTaskWrites(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, toMira)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowTaskWriteFails)
			windowWantCardStatus(t, d, "rc-1", replyCardStatusWaiting)
			windowWantStep(t, d, step)
			dalWantTask(t, d, task)
			dashboard.wantFrames()
		})
	}
}

type windowPlanDoor struct {
	name, path, body string
}

var windowPlanDoors = []windowPlanDoor{
	{"submit plan", "/api/tasks/T-1/plan", `{"steps":[{"name":"ship the crate","dod":"the crate is on the truck"}]}`},
	{"insert step", "/api/tasks/T-1/steps", `{"name":"ship the crate","dod":"the crate is on the truck"}`},
	{"delete step", "/api/tasks/T-1/steps/ts-2/delete", ``},
	{"reorder steps", "/api/tasks/T-1/steps/reorder", `{"step_ids":["ts-2","ts-1"]}`},
}

func TestEditingAPlanDecidesFromTheRowItWrites(t *testing.T) {
	for _, door := range windowPlanDoors {
		for _, shape := range windowDALShapes {
			t.Run(door.name+", "+shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
				d, hook, path := windowDAL(t, shape)
				api, h, _, owner := newAPITestServerOn(t, d)
				task := dalPutTask(t, d, windowOpenTask("T-1"))
				first, second := windowPlanStep("ts-1", task.ID, 0), windowPlanStep("ts-2", task.ID, 1)
				windowPutSteps(t, d, first, second)
				dashboard := apiTestListen(t, api, "")
				hook.closeTaskAfterRead(t, path, task.ID, "FROM task WHERE id")

				status, data := windowJSON(t, h, "POST", door.path, owner, door.body)

				hook.wantFiredOnce(t)
				if status != http.StatusConflict {
					t.Fatalf("want 409, got %d (%v)", status, data)
				}
				apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
				dalWantTask(t, d, windowClosed(task))
				windowWantSteps(t, d, task.ID, first, second)
				dashboard.wantFrames()
			})
		}

		t.Run(door.name+": a task write that fails takes the step writes back with it", func(t *testing.T) {
			d, _, _ := windowDAL(t, "split pools")
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			first, second := windowPlanStep("ts-1", task.ID, 0), windowPlanStep("ts-2", task.ID, 1)
			windowPutSteps(t, d, first, second)
			windowRefuseTaskWrites(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := windowJSON(t, h, "POST", door.path, owner, door.body)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowTaskWriteFails)
			dalWantTask(t, d, task)
			windowWantSteps(t, d, task.ID, first, second)
			dashboard.wantFrames()
		})
	}
}

// dismissOutsourceWorkerByID retires the worker's waiting cards while it holds
// outsourceMu, and each retirement is a transaction. Anything on that path that
// takes outsourceMu again (inside the transaction or not) never returns: the
// lock is not re-entrant, and on the one-connection DAL the transaction also
// holds the only connection. windowRequest's deadline makes that mistake fail
// here by name instead of as a suite timeout.
func TestDismissingAWorkerThatStillHasAWaitingCard(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape+": claiming a task handed over from the worker retires its card", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := windowOpenTask("T-1")
			task.Lock = TaskLockReassigning
			task.ExecutorID = "mira"
			task.ReassignedFrom = "ow-abc123"
			task.ReassignedFromKind = TaskExecutorOutsource
			dalPutTask(t, d, task)
			windowBoundWorker(t, d, task.ID)
			if err := d.PutReplyCard(ReplyCard{
				ID: "rc-1", FromMember: "ow-abc123", Kind: replyCardKindDecision,
				Summary: "which yard", Options: []ReplyCardOption{{Text: "north"}, {Text: "south"}},
				SelectMode: replyCardSelectModeSingle, Status: replyCardStatusWaiting,
				CreatedTS: 1700000100, ChatMessageID: "c-1",
			}); err != nil {
				t.Fatalf("PutReplyCard: %v", err)
			}
			dashboard := apiTestListen(t, api, "")

			rec := windowRequest(t, h, "POST", "/api/tasks/T-1/claim", owner, "")

			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %s: %v", rec.Body.String(), err)
			}
			apiWantBody(t, body, map[string]any{
				"task_id": "T-1", "title": "reconcile the yard ledger", "status": "in_progress",
				"executor_id": "mira", "executor_kind": "staff", "lock": "", "closed_ts": nil,
				"duplicate_of": "", "deps": []any{}, "progress_done": 0, "progress_total": 0,
				"artifact_count": 0, "description_size_chars": 24,
				"description_sha256": "477b16cea9a02abf3dd077a4bf3d2e6aa496fe22559806609fe817aadf3125ef",
			})
			windowWantCardStatus(t, d, "rc-1", replyCardStatusExpired)
			windowWantWorkerStatus(t, d, "ow-abc123", WorkerStatusReleased)
			dashboard.wantFrames(
				map[string]any{
					"seq": 1, "topic": "task", "op": "patch",
					"data": map[string]any{
						"entity": "task", "key": "owner::T-1", "epoch": 1, "deleted": false,
						"payload": map[string]any{"id": "T-1", "priority": "high", "status": "in_progress"},
					},
					"ts": apiAnyNumber, "trigger": "owner",
				},
				map[string]any{
					"seq": 2, "topic": "member", "op": "remove",
					"data": map[string]any{
						"entity": "member", "key": "owner::ow-abc123", "epoch": 2, "deleted": true, "payload": nil,
					},
					"ts": apiAnyNumber, "trigger": "owner",
				},
				map[string]any{
					"seq": 3, "topic": "reply_card", "op": "patch",
					"data": map[string]any{
						"entity": "reply_card", "key": "owner::rc-1", "epoch": 3, "deleted": false,
						"payload": map[string]any{"id": "rc-1", "from": "ow-abc123", "status": "expired"},
					},
					"ts": apiAnyNumber, "trigger": "owner",
				},
			)
		})
	}
}

// The authz half: the executor re-pointed after the handler read the row takes
// the caller's right to drive the task with it.
func TestTheDoorsJudgeTheCallerOnTheRowTheyWrite(t *testing.T) {
	type door struct{ name, path, body string }
	doors := []door{
		{"mark done", "/api/tasks/T-1/mark-done", ``},
		{"terminate", "/api/tasks/T-1/mark-terminated", ``},
		{"duplicated", "/api/tasks/T-1/mark-duplicated", `{"duplicate_of":"T-2"}`},
		{"reassign", "/api/tasks/T-1/reassign", `{"target":{"kind":"outsource"}}`},
	}
	for _, p := range windowPlanDoors {
		doors = append(doors, door{p.name, p.path, p.body})
	}
	doors = append(doors,
		door{"priority", "/api/tasks/T-1/priority", `{"priority":"low"}`},
		door{"deps", "/api/tasks/T-1/deps", `{"blocked_by":["T-2"]}`},
		door{"step status", "/api/tasks/T-1/steps/ts-1/status", `{"status":"in_progress"}`},
	)
	for _, dr := range doors {
		for _, shape := range windowDALShapes {
			t.Run(dr.name+", "+shape+": a task handed to someone else after the handler read it refuses its old executor", func(t *testing.T) {
				d, hook, path := windowDAL(t, shape)
				api, h, _, _ := newAPITestServerOn(t, d)
				kip := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
				open := windowOpenTask("T-1")
				if dr.name == "mark done" {
					open.Status = TaskStatusReadyForDone
				}
				task := dalPutTask(t, d, open)
				dalPutTask(t, d, windowOpenTask("T-2"))
				first, second := windowPlanStep("ts-1", task.ID, 0), windowPlanStep("ts-2", task.ID, 1)
				windowPutSteps(t, d, first, second)
				dashboard := apiTestListen(t, api, "")
				hook.execAfterRead(t, path, "FROM task WHERE id",
					`UPDATE task SET executor_id = 'mira', updated_ts = 1800000000 WHERE id = ?`, task.ID)

				status, data := windowJSON(t, h, "POST", dr.path, kip, dr.body)

				hook.wantFiredOnce(t)
				if status != http.StatusForbidden {
					t.Fatalf("want 403, got %d (%v)", status, data)
				}
				apiWantError(t, data, "forbidden", "caller is not the task's executor")
				want := task
				want.ExecutorID = "mira"
				want.UpdatedTS = 1800000000
				dalWantTask(t, d, want)
				windowWantSteps(t, d, task.ID, first, second)
				dashboard.wantFrames()
			})
		}
	}
}

// windowRefuseSetting makes every write to one setting row fail (insert, update
// and delete); the returned func lifts it so a retry can land.
func windowRefuseSetting(t *testing.T, d *DAL, key string) (lift func()) {
	t.Helper()
	stmts := []string{
		`CREATE TRIGGER refuse_setting_insert BEFORE INSERT ON setting WHEN NEW.key = '` + key + `'
		 BEGIN SELECT RAISE(FAIL, 'the setting write fails'); END`,
		`CREATE TRIGGER refuse_setting_update BEFORE UPDATE ON setting WHEN NEW.key = '` + key + `'
		 BEGIN SELECT RAISE(FAIL, 'the setting write fails'); END`,
		`CREATE TRIGGER refuse_setting_delete BEFORE DELETE ON setting WHEN OLD.key = '` + key + `'
		 BEGIN SELECT RAISE(FAIL, 'the setting write fails'); END`,
	}
	for _, stmt := range stmts {
		if _, err := d.wdb.Exec(stmt); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
	}
	return func() {
		for _, name := range []string{"refuse_setting_insert", "refuse_setting_update", "refuse_setting_delete"} {
			if _, err := d.wdb.Exec(`DROP TRIGGER ` + name); err != nil {
				t.Fatalf("drop trigger: %v", err)
			}
		}
	}
}

const windowSettingWriteFails = "internal error: constraint failed: the setting write fails (1811)"

func windowWantSetting(t *testing.T, d *DAL, key string, want *string) {
	t.Helper()
	got, err := d.GetSetting(key)
	if err != nil {
		t.Fatalf("GetSetting(%q): %v", key, err)
	}
	switch {
	case want == nil && got != nil:
		t.Fatalf("setting %q: got %q, want no row", key, *got)
	case want != nil && got == nil:
		t.Fatalf("setting %q: got no row, want %q", key, *want)
	case want != nil && *got != *want:
		t.Fatalf("setting %q: got %q, want %q", key, *got, *want)
	}
}

func windowSettingNow(t *testing.T, d *DAL, key string) *string {
	t.Helper()
	got, err := d.GetSetting(key)
	if err != nil {
		t.Fatalf("GetSetting(%q): %v", key, err)
	}
	return got
}

func windowWantStepNote(t *testing.T, d *DAL, stepID, want string) {
	t.Helper()
	got, err := d.GetTaskStep(stepID)
	if err != nil || got == nil {
		t.Fatalf("GetTaskStep(%q): %#v, %v", stepID, got, err)
	}
	if got.Note != want {
		t.Fatalf("step %s note: got %q, want %q", stepID, got.Note, want)
	}
}

// windowRefuse installs a trigger that makes event fail with what; the answer a
// handler gives for it is windowRefusal(what).
func windowRefuse(t *testing.T, d *DAL, name, event, what string) {
	t.Helper()
	if _, err := d.wdb.Exec(`CREATE TRIGGER ` + name + ` ` + event +
		` BEGIN SELECT RAISE(FAIL, '` + what + `'); END`); err != nil {
		t.Fatalf("create trigger %s: %v", name, err)
	}
}

func windowRefusal(what string) string {
	return "internal error: constraint failed: " + what + " (1811)"
}

// A lifecycle door re-reads the member row inside the transaction that writes
// it, and lands the wind-down anchors, the whole row and any receipt or setter
// together. The window closes on the row (dismissed, or a worker released,
// behind the handler); the failure makes the whole-row write die after the
// anchor write has already run in the same transaction.
type windowMemberDoor struct {
	name, method, path, body string
	worker                   bool // the target is ow-abc123 rather than kip
	self                     bool // driven by the target's own credential
	live                     bool // the target's session is connected
	prepare                  string
}

var windowMemberDoors = []windowMemberDoor{
	{name: "update", method: "PATCH", path: "/api/members/kip", body: `{"model":"claude-opus-5"}`},
	{name: "activate", method: "POST", path: "/api/members/kip/activate", body: `{}`},
	{name: "relocate", method: "POST", path: "/api/members/kip/relocate", body: `{"machine_id":"m-server-self"}`,
		prepare: `UPDATE member SET desired_machine_id = 'm-retired' WHERE id = 'kip'`},
	{name: "deactivate", method: "POST", path: "/api/members/kip/deactivate", body: `{}`},
	{name: "force stop", method: "POST", path: "/api/members/kip/force-stop", body: `{}`},
	{name: "accelerated stop", method: "POST", path: "/api/members/kip/accelerated-stop", body: `{}`, live: true,
		prepare: `UPDATE member SET desired_state = 'offline', stopping_since = 1700000000 WHERE id = 'kip'`},
	{name: "refocus", method: "POST", path: "/api/members/kip/refocus", body: `{}`, live: true,
		prepare: `UPDATE member SET desired_state = 'online' WHERE id = 'kip'`},
	{name: "report waking", method: "POST", path: "/api/self/waking", body: `{}`, self: true},
	{name: "report stopping", method: "POST", path: "/api/self/stopping", body: `{}`, self: true},
	{name: "report stopped", method: "POST", path: "/api/self/stopped", body: `{}`, self: true},
	{name: "restart self", method: "POST", path: "/api/self/refocus", body: `{}`, self: true, live: true,
		prepare: `UPDATE member SET desired_state = 'online' WHERE id = 'kip'`},

	{name: "worker stop", method: "POST", path: "/api/members/ow-abc123/deactivate", body: `{}`, worker: true},
	{name: "worker force stop", method: "POST", path: "/api/members/ow-abc123/force-stop", body: `{}`, worker: true},
	{name: "worker accelerated stop", method: "POST", path: "/api/members/ow-abc123/accelerated-stop", body: `{}`,
		worker: true, live: true,
		prepare: `UPDATE member SET desired_state = 'offline', stopping_since = 1700000000 WHERE id = 'ow-abc123'`},
	{name: "worker refocus", method: "POST", path: "/api/members/ow-abc123/refocus", body: `{}`, worker: true, live: true},
	{name: "worker restart", method: "POST", path: "/api/members/ow-abc123/activate", body: `{}`, worker: true},
	{name: "worker relocate", method: "POST", path: "/api/members/ow-abc123/relocate", body: `{"machine_id":"m-server-self"}`,
		worker: true, prepare: `UPDATE member SET desired_machine_id = 'm-retired' WHERE id = 'ow-abc123'`},
	{name: "worker model", method: "PATCH", path: "/api/members/ow-abc123", body: `{"model":"claude-opus-5"}`, worker: true},
	{name: "worker report waking", method: "POST", path: "/api/self/waking", body: `{}`, worker: true, self: true},
	{name: "worker report stopping", method: "POST", path: "/api/self/stopping", body: `{}`, worker: true, self: true},
	{name: "worker report stopped", method: "POST", path: "/api/self/stopped", body: `{}`, worker: true, self: true},
	{name: "worker restart self", method: "POST", path: "/api/self/refocus", body: `{}`, worker: true, self: true, live: true},
}

// windowMemberDoorStack is the server a door runs on, with the target in the
// state the door needs; it answers the target id and the credential to drive
// the door with.
func windowMemberDoorStack(t *testing.T, d *DAL, door windowMemberDoor) (*apiServer, http.Handler, string, string) {
	t.Helper()
	api, h, _, owner := newAPITestServerOn(t, d)
	id := apiTestPlainAgentID
	if door.worker {
		id = "ow-abc123"
		apiTestWorkerFixture(t, h, d, owner, id, WorkerStatusActive)
		apiTestWorkerWantedOnline(t, d, id)
	}
	if err := d.SetMemberDesiredMachineID(id, ServerSelfHost); err != nil {
		t.Fatalf("SetMemberDesiredMachineID: %v", err)
	}
	if door.prepare != "" {
		if _, err := d.wdb.Exec(door.prepare); err != nil {
			t.Fatalf("prepare %s: %v", door.name, err)
		}
	}
	apiTestListen(t, api, ServerSelfHost)
	if door.live {
		session, err := api.hub.Connect(id, ServerSelfHost)
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(session) })
	}
	token := owner
	if door.self {
		token = apiTestAgentToken(t, api, id, ServerSelfHost)
	}
	return api, h, id, token
}

func TestMemberLifecycleDoorsDecideFromTheRowTheyWrite(t *testing.T) {
	for _, door := range windowMemberDoors {
		for _, shape := range windowDALShapes {
			t.Run(door.name+", "+shape+": a member removed after the handler read it stays removed", func(t *testing.T) {
				d, hook, path := windowDAL(t, shape)
				api, h, id, token := windowMemberDoorStack(t, d, door)
				dashboard := apiTestListen(t, api, "")
				hook.execAfterRead(t, path, "FROM member WHERE id",
					`UPDATE member SET roster_status = 'removed' WHERE id = '`+id+`'`)

				status, data := windowJSON(t, h, door.method, door.path, token, door.body)

				hook.wantFiredOnce(t)
				if status != http.StatusNotFound {
					t.Fatalf("want 404, got %d (%v)", status, data)
				}
				apiWantError(t, data, "not_found", "member '"+id+"' not found")
				if got := apiTestMemberRow(t, d, id); got.RosterStatus != RosterStatusRemoved {
					t.Fatalf("roster_status: got %q, want removed", got.RosterStatus)
				}
				dashboard.wantFrames()
			})

			t.Run(door.name+", "+shape+": a row write that fails takes the anchors back with it, and the retry lands", func(t *testing.T) {
				d, _, _ := windowDAL(t, shape)
				api, h, id, token := windowMemberDoorStack(t, d, door)
				before := apiTestMemberRow(t, d, id)
				apiTestFailWholeRowWrite(t, d, id)
				dashboard := apiTestListen(t, api, "")

				status, data := windowJSON(t, h, door.method, door.path, token, door.body)

				if status != http.StatusInternalServerError {
					t.Fatalf("want 500, got %d (%v)", status, data)
				}
				apiWantError(t, data, "internal_error",
					"internal error: constraint failed: whole row unwritable (1811)")
				apiTestWantEqual(t, "the row after the failed write", apiTestMemberRow(t, d, id), before)
				dashboard.wantFrames()

				apiTestRestoreWholeRowWrite(t, d)
				if status, data := windowJSON(t, h, door.method, door.path, token, door.body); status != http.StatusOK {
					t.Fatalf("retry: want 200, got %d (%v)", status, data)
				}
			})
		}
	}

	// Each door's own refusal or decision, judged on the row as it stands in the
	// transaction: the window moves the row so that the answer changes.
	const stopped = `UPDATE member SET desired_state = 'offline', stopping_since = 1800000000,
		refocus_since = 0, refocus_op = '' WHERE id = '%ID%'`
	const stopOpen = `UPDATE member SET desired_state = 'offline', stopping_since = 1700000000 WHERE id = '%ID%'`
	for _, tc := range []struct {
		door   windowMemberDoor
		window string
		status int
		// refusal is the error message a refused door answers; "" means it answers 200.
		refusal string
		body    map[string]any
		row     func(t *testing.T, m Member)
	}{
		{
			door: windowMemberDoor{name: "update, a model someone already saved", method: "PATCH",
				path: "/api/members/kip", body: `{"model":"claude-opus-5"}`, live: true,
				prepare: `UPDATE member SET desired_state = 'online' WHERE id = 'kip'`},
			window: `UPDATE member SET model = 'claude-opus-5' WHERE id = '%ID%'`,
			status: http.StatusOK,
			row: func(t *testing.T, m Member) {
				if m.Model != "claude-opus-5" || m.RefocusSince != 0 || m.RefocusOp != "" {
					t.Fatalf("model=%q refocus_since=%v refocus_op=%q, want claude-opus-5 and no wind-down",
						m.Model, m.RefocusSince, m.RefocusOp)
				}
			},
		},
		{
			door: windowMemberDoor{name: "activate, a machine removed after the check", method: "POST",
				path: "/api/members/kip/activate", body: `{"machine_id":"%MACHINE%"}`},
			window: `UPDATE member SET roster_status = 'removed' WHERE id = '%MACHINE%'`,
			status: http.StatusNotFound, refusal: "machine '%MACHINE%' not found",
			row: func(t *testing.T, m Member) {
				if m.DesiredState == DesiredStateOnline || m.DesiredMachineID != ServerSelfHost {
					t.Fatalf("desired_state=%q desired_machine_id=%q, want not activated / %s", m.DesiredState, m.DesiredMachineID, ServerSelfHost)
				}
			},
		},
		{
			door: windowMemberDoor{name: "relocate, a machine removed after the check", method: "POST",
				path: "/api/members/kip/relocate", body: `{"machine_id":"%MACHINE%"}`},
			window: `UPDATE member SET roster_status = 'removed' WHERE id = '%MACHINE%'`,
			status: http.StatusNotFound, refusal: "machine '%MACHINE%' not found",
			row: func(t *testing.T, m Member) {
				if m.DesiredMachineID != ServerSelfHost {
					t.Fatalf("desired_machine_id=%q, want %s", m.DesiredMachineID, ServerSelfHost)
				}
			},
		},
		{
			door: windowMemberDoor{name: "deactivate, a wake begun after the read", method: "POST",
				path: "/api/members/kip/deactivate", body: `{}`},
			window: `UPDATE member SET desired_state = 'online', stopping_since = 0,
				waking_since = CAST(strftime('%s', 'now') AS REAL) WHERE id = '%ID%'`,
			status: http.StatusOK,
			row: func(t *testing.T, m Member) {
				if m.DesiredState != DesiredStateOffline || m.StoppingSince <= 0 || m.StoppedSince != 0 {
					t.Fatalf("desired_state=%q stopping_since=%v stopped_since=%v, want a cancelled wake: offline, a stop anchor, no close-out",
						m.DesiredState, m.StoppingSince, m.StoppedSince)
				}
			},
		},
		{
			door: windowMemberDoor{name: "accelerated stop, a wind-down withdrawn after the read", method: "POST",
				path: "/api/members/kip/accelerated-stop", body: `{}`, live: true,
				prepare: strings.ReplaceAll(stopOpen, "%ID%", "kip")},
			window: `UPDATE member SET desired_state = 'online', stopping_since = 0 WHERE id = '%ID%'`,
			status: http.StatusConflict, refusal: acceleratedStopNeedsAnOpenWindDownMsg,
			row: func(t *testing.T, m Member) {
				if m.RefocusOp != "" || m.StoppingSince != 0 {
					t.Fatalf("refocus_op=%q stopping_since=%v, want the row as the window left it", m.RefocusOp, m.StoppingSince)
				}
			},
		},
		{
			door: windowMemberDoor{name: "refocus, a stopped member activated after the read", method: "POST",
				path: "/api/members/kip/refocus", body: `{}`,
				prepare: strings.ReplaceAll(stopOpen, "%ID%", "kip")},
			window:  `UPDATE member SET desired_state = 'online', stopping_since = 0 WHERE id = '%ID%'`,
			status:  http.StatusConflict,
			refusal: "refocus requires the member to have a live session and to be wanted online (§3.4 #14)",
			row: func(t *testing.T, m Member) {
				if m.RestartAfterStop || m.RefocusSince != 0 {
					t.Fatalf("restart_after_stop=%v refocus_since=%v, want neither", m.RestartAfterStop, m.RefocusSince)
				}
			},
		},
		{
			door: windowMemberDoor{name: "report waking, a member stopped after the read", method: "POST",
				path: "/api/self/waking", body: `{}`, self: true,
				prepare: `UPDATE member SET desired_state = 'online' WHERE id = 'kip'`},
			window: stopped,
			status: http.StatusOK,
			row: func(t *testing.T, m Member) {
				if m.DesiredState != DesiredStateOffline || m.StoppingSince != 1800000000 {
					t.Fatalf("desired_state=%q stopping_since=%v, want the owner's stop kept (offline / 1800000000)",
						m.DesiredState, m.StoppingSince)
				}
			},
		},
		{
			door: windowMemberDoor{name: "report stopped, a close-out already reported after the read", method: "POST",
				path: "/api/self/stopped", body: `{}`, self: true},
			window: `UPDATE member SET stopped_since = 1800000000 WHERE id = '%ID%'`,
			status: http.StatusOK,
			body:   map[string]any{"stop_effect": "already_reported"},
			row: func(t *testing.T, m Member) {
				if m.StoppedSince != 1800000000 {
					t.Fatalf("stopped_since=%v, want the earlier report's 1800000000", m.StoppedSince)
				}
			},
		},
		{
			door: windowMemberDoor{name: "worker restart, a machine removed after the check", method: "POST",
				path: "/api/members/ow-abc123/activate", body: `{"machine_id":"%MACHINE%"}`, worker: true},
			window: `UPDATE member SET roster_status = 'removed' WHERE id = '%MACHINE%'`,
			status: http.StatusNotFound, refusal: "machine '%MACHINE%' not found",
			row: func(t *testing.T, m Member) {
				if m.DesiredMachineID != ServerSelfHost {
					t.Fatalf("desired_machine_id=%q, want %s", m.DesiredMachineID, ServerSelfHost)
				}
			},
		},
		{
			door: windowMemberDoor{name: "worker relocate, a machine removed after the check", method: "POST",
				path: "/api/members/ow-abc123/relocate", body: `{"machine_id":"%MACHINE%"}`, worker: true},
			window: `UPDATE member SET roster_status = 'removed' WHERE id = '%MACHINE%'`,
			status: http.StatusNotFound, refusal: "machine '%MACHINE%' not found",
			row: func(t *testing.T, m Member) {
				if m.DesiredMachineID != ServerSelfHost {
					t.Fatalf("desired_machine_id=%q, want %s", m.DesiredMachineID, ServerSelfHost)
				}
			},
		},
		{
			door: windowMemberDoor{name: "worker accelerated stop, a wind-down withdrawn after the read", method: "POST",
				path: "/api/members/ow-abc123/accelerated-stop", body: `{}`, worker: true, live: true,
				prepare: strings.ReplaceAll(stopOpen, "%ID%", "ow-abc123")},
			window: `UPDATE member SET desired_state = 'online', stopping_since = 0 WHERE id = '%ID%'`,
			status: http.StatusConflict, refusal: acceleratedStopWorkerNeedsAnOpenWindDownMsg,
			row: func(t *testing.T, m Member) {
				if m.RefocusOp != "" || m.StoppingSince != 0 {
					t.Fatalf("refocus_op=%q stopping_since=%v, want the row as the window left it", m.RefocusOp, m.StoppingSince)
				}
			},
		},
		{
			door: windowMemberDoor{name: "worker refocus, a worker held down after the read", method: "POST",
				path: "/api/members/ow-abc123/refocus", body: `{}`, worker: true, live: true},
			window: `UPDATE member SET desired_state = 'offline' WHERE id = '%ID%'`,
			status: http.StatusConflict,
			refusal: "refocus requires a live worker — this one is stopped and has never " +
				"been asked to stop, so there is no wind-down for a 起來 to be " +
				"queued behind (重啟 it when you want it to run)",
			row: func(t *testing.T, m Member) {
				if m.RefocusSince != 0 || m.RestartAfterStop {
					t.Fatalf("refocus_since=%v restart_after_stop=%v, want neither", m.RefocusSince, m.RestartAfterStop)
				}
			},
		},
		{
			door: windowMemberDoor{name: "worker model, a model someone already saved", method: "PATCH",
				path: "/api/members/ow-abc123", body: `{"model":"claude-opus-5"}`, worker: true,
				prepare: strings.ReplaceAll(stopOpen, "%ID%", "ow-abc123")},
			window: `UPDATE member SET model = 'claude-opus-5' WHERE id = '%ID%'`,
			status: http.StatusOK,
			row: func(t *testing.T, m Member) {
				if m.Model != "claude-opus-5" || m.RestartAfterStop {
					t.Fatalf("model=%q restart_after_stop=%v, want claude-opus-5 and no queued restart", m.Model, m.RestartAfterStop)
				}
			},
		},
		{
			door: windowMemberDoor{name: "worker report stopped, a close-out already reported after the read", method: "POST",
				path: "/api/self/stopped", body: `{}`, worker: true, self: true},
			window: `UPDATE member SET stopped_since = 1800000000 WHERE id = '%ID%'`,
			status: http.StatusOK,
			body:   map[string]any{"stop_effect": "already_reported"},
			row: func(t *testing.T, m Member) {
				if m.StoppedSince != 1800000000 {
					t.Fatalf("stopped_since=%v, want the earlier report's 1800000000", m.StoppedSince)
				}
			},
		},
	} {
		for _, shape := range windowDALShapes {
			t.Run(tc.door.name+", "+shape, func(t *testing.T) {
				d, hook, path := windowDAL(t, shape)
				_, h, id, token := windowMemberDoorStack(t, d, tc.door)
				fill := func(text string) string { return strings.ReplaceAll(text, "%ID%", id) }
				if strings.Contains(tc.door.body, "%MACHINE%") {
					machineID := apiTestOnboardMachine(t, h, token, "Studio Mac")
					fill = func(text string) string {
						return strings.ReplaceAll(strings.ReplaceAll(text, "%ID%", id), "%MACHINE%", machineID)
					}
				}
				hook.execAfterRead(t, path, "FROM member WHERE id", fill(tc.window))

				status, data := windowJSON(t, h, tc.door.method, tc.door.path, token, fill(tc.door.body))

				hook.wantFiredOnce(t)
				if status != tc.status {
					t.Fatalf("want %d, got %d (%v)", tc.status, status, data)
				}
				if tc.refusal != "" {
					apiWantError(t, data, errorCodeForStatus(tc.status), fill(tc.refusal))
				}
				for k, v := range tc.body {
					apiWantValue(t, k, data[k], v)
				}
				tc.row(t, apiTestMemberRow(t, d, id))
			})
		}
	}

	// A receipt that fails to land takes the row it explains back with it.
	for _, door := range []windowMemberDoor{
		{name: "update held down", method: "PATCH", path: "/api/members/kip", body: `{"model":"claude-opus-5"}`,
			prepare: `UPDATE member SET desired_state = 'offline' WHERE id = 'kip'`},
		{name: "relocate held down", method: "POST", path: "/api/members/kip/relocate", body: `{"machine_id":"m-server-self"}`,
			prepare: `UPDATE member SET desired_state = 'offline', desired_machine_id = 'm-retired' WHERE id = 'kip'`},
		{name: "refocus queued behind a stop", method: "POST", path: "/api/members/kip/refocus", body: `{}`,
			prepare: strings.ReplaceAll(stopOpen, "%ID%", "kip")},
		{name: "worker restart on a live session", method: "POST", path: "/api/members/ow-abc123/activate", body: `{}`,
			worker: true, live: true, prepare: strings.ReplaceAll(stopOpen, "%ID%", "ow-abc123")},
		{name: "worker refocus queued behind a stop", method: "POST", path: "/api/members/ow-abc123/refocus", body: `{}`,
			worker: true, prepare: strings.ReplaceAll(stopOpen, "%ID%", "ow-abc123")},
		{name: "worker model queued behind a stop", method: "PATCH", path: "/api/members/ow-abc123", body: `{"model":"claude-opus-5"}`,
			worker: true, prepare: strings.ReplaceAll(stopOpen, "%ID%", "ow-abc123")},
	} {
		for _, shape := range windowDALShapes {
			t.Run(door.name+", "+shape+": a receipt that fails to land takes the row back with it, and the retry lands", func(t *testing.T) {
				d, _, _ := windowDAL(t, shape)
				api, h, id, token := windowMemberDoorStack(t, d, door)
				before := apiTestMemberRow(t, d, id)
				windowRefuse(t, d, "refuse_receipt", "BEFORE UPDATE OF last_op ON member", "the receipt write fails")
				dashboard := apiTestListen(t, api, "")

				status, data := windowJSON(t, h, door.method, door.path, token, door.body)

				if status != http.StatusInternalServerError {
					t.Fatalf("want 500, got %d (%v)", status, data)
				}
				apiWantError(t, data, "internal_error", windowRefusal("the receipt write fails"))
				apiTestWantEqual(t, "the row after the failed receipt", apiTestMemberRow(t, d, id), before)
				dashboard.wantFrames()

				if _, err := d.wdb.Exec(`DROP TRIGGER refuse_receipt`); err != nil {
					t.Fatalf("drop trigger: %v", err)
				}
				if status, data := windowJSON(t, h, door.method, door.path, token, door.body); status != http.StatusOK {
					t.Fatalf("retry: want 200, got %d (%v)", status, data)
				}
				if got := apiTestMemberRow(t, d, id); got.LastOpReason == before.LastOpReason && got.LastOpAt == before.LastOpAt {
					t.Fatalf("the retry landed no receipt: last_op_reason=%q", got.LastOpReason)
				}
			})
		}
	}

	// A settings patch takes settingsMu and then waits for the write connection.
	// A door that opens a wind-down reads the live reconcile config, which takes
	// settingsMu too: read inside its transaction, the two wait on each other.
	for _, door := range []windowMemberDoor{
		{name: "update", method: "PATCH", path: "/api/members/kip", body: `{"model":"claude-opus-5"}`, live: true,
			prepare: `UPDATE member SET desired_state = 'online' WHERE id = 'kip'`},
		{name: "relocate", method: "POST", path: "/api/members/kip/relocate", body: `{"machine_id":"m-server-self"}`,
			live: true, prepare: `UPDATE member SET desired_state = 'online' WHERE id = 'kip'`},
	} {
		for _, shape := range windowDALShapes {
			t.Run(door.name+", "+shape+": a settings patch waiting for the write connection does not stall the wind-down", func(t *testing.T) {
				d, hook, _ := windowDAL(t, shape)
				api, h, id, token := windowMemberDoorStack(t, d, door)
				released := make(chan struct{})
				hook.mu.Lock()
				hook.armAfter = "FROM member WHERE id"
				hook.fire = func() {
					api.settingsMu.Lock()
					go func() {
						defer close(released)
						defer api.settingsMu.Unlock()
						if _, err := d.wdb.Exec(`SELECT 1`); err != nil {
							t.Errorf("the settings patch's write: %v", err)
						}
					}()
				}
				hook.mu.Unlock()

				status, data := windowJSON(t, h, door.method, door.path, token, door.body)

				hook.wantFiredOnce(t)
				if status != http.StatusOK {
					t.Fatalf("want 200, got %d (%v)", status, data)
				}
				<-released
				if got := apiTestMemberRow(t, d, id); got.RefocusOp == "" || got.RefocusSince <= 0 {
					t.Fatalf("want a wind-down opened, got refocus_op=%q refocus_since=%v", got.RefocusOp, got.RefocusSince)
				}
			})
		}
	}
}

// windowWithin runs a call that reaches a handler directly under
// windowRequestDeadline, so a request that waits on its own transaction fails
// by name instead of hanging the suite to its -timeout.
func windowWithin(t *testing.T, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	select {
	case <-done:
	case <-time.After(windowRequestDeadline):
		t.Fatalf("%s did not return within %s: inside its transaction it waits on a second "+
			"connection or on a lock it already holds", what, windowRequestDeadline)
	}
}
