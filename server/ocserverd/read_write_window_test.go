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

			status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			hook.wantFiredOnce(t)
			if status != http.StatusConflict {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
			dalWantTask(t, d, windowClosed(task))
			dashboard.wantFrames()
		})
	}

	t.Run("a write that fails leaves the row and fans nothing", func(t *testing.T) {
		d, _, _ := windowDAL(t, "split pools")
		api, h, _, owner := newAPITestServerOn(t, d)
		task := dalPutTask(t, d, windowOpenTask("T-1"))
		windowRefuseTaskWrites(t, d)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

		if status != http.StatusInternalServerError {
			t.Fatalf("want 500, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error", windowTaskWriteFails)
		dalWantTask(t, d, task)
		dashboard.wantFrames()
	})
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

			status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-2"]}`)

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

	t.Run("a task write that fails takes the new edges back with it", func(t *testing.T) {
		d, _, _ := windowDAL(t, "split pools")
		api, h, _, owner := newAPITestServerOn(t, d)
		task := dalPutTask(t, d, windowOpenTask("T-1"))
		dalPutTask(t, d, windowOpenTask("T-2"))
		dalPutTask(t, d, windowOpenTask("T-3"))
		if err := d.AddTaskDep("T-1", "T-2"); err != nil {
			t.Fatalf("AddTaskDep: %v", err)
		}
		windowRefuseTaskWrites(t, d)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-3"]}`)

		if status != http.StatusInternalServerError {
			t.Fatalf("want 500, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error", windowTaskWriteFails)
		windowWantDeps(t, d, "T-1", []string{"T-2"})
		dalWantTask(t, d, task)
		dashboard.wantFrames()
	})
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

			status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-1/status", owner, `{"status":"in_progress"}`)

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

	t.Run("a task write that fails takes the step write back with it", func(t *testing.T) {
		d, _, _ := windowDAL(t, "split pools")
		api, h, _, owner := newAPITestServerOn(t, d)
		task := dalPutTask(t, d, windowOpenTask("T-1"))
		step := windowPendingStep("ts-1", task.ID)
		if err := d.PutTaskStep(step); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		windowRefuseTaskWrites(t, d)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-1/status", owner, `{"status":"in_progress"}`)

		if status != http.StatusInternalServerError {
			t.Fatalf("want 500, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error", windowTaskWriteFails)
		windowWantStep(t, d, step)
		dalWantTask(t, d, task)
		dashboard.wantFrames()
	})
}

func TestAnsweringACardDecidesFromTheRowItWrites(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape+": a task closed after the handler read it stays closed", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task, step, _ := windowHeldCard(t, d)
			dashboard := apiTestListen(t, api, "")
			hook.closeTaskAfterRead(t, path, task.ID, "FROM reply_card WHERE id")

			status, data := apiJSON(t, h, "POST", "/api/reply-cards/rc-1/answer", owner, `{"option_idxs":[0]}`)

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

			status, data := apiJSON(t, h, "POST", "/api/reply-cards/rc-1/expire", owner, ``)

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

	t.Run("a task write that fails leaves the card waiting and fans nothing", func(t *testing.T) {
		d, _, _ := windowDAL(t, "split pools")
		api, h, _, owner := newAPITestServerOn(t, d)
		task, step, _ := windowHeldCard(t, d)
		windowRefuseTaskWrites(t, d)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/rc-1/expire", owner, ``)

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

			status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ``)

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

			status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ``)

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

	t.Run("a task write that fails leaves the card waiting", func(t *testing.T) {
		d, _, _ := windowDAL(t, "split pools")
		api, h, _, owner := newAPITestServerOn(t, d)
		task, step, _ := windowHeldCard(t, d)
		windowRefuseTaskWrites(t, d)
		kip := apiTestListen(t, api, apiTestPlainAgentID)

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ``)

		if status != http.StatusOK {
			t.Fatalf("the dismissal itself lands: want 200, got %d (%v)", status, data)
		}
		windowWantCardStatus(t, d, "rc-1", replyCardStatusWaiting)
		windowWantStep(t, d, step)
		dalWantTask(t, d, task)
		kip.wantFrames(windowKipRemovedFrame)
	})
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

			status, data := apiJSON(t, h, "POST", "/api/reply-cards", kip, body)

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

	t.Run("a task write that fails takes the card, its message and the hold back with it", func(t *testing.T) {
		d, _, _ := windowDAL(t, "split pools")
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

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", kip, body)

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

				status, data := apiJSON(t, h, "POST", door.path, token, door.body)

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

			status, data := apiJSON(t, h, "POST", door.path, token, door.body)

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

			status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, toMira)

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

			status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, toMira)

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

	t.Run("a task write that fails takes the card expiry and the step reset back with it", func(t *testing.T) {
		d, _, _ := windowDAL(t, "split pools")
		api, h, _, owner := newAPITestServerOn(t, d)
		task, step, _ := windowHeldCard(t, d)
		windowRefuseTaskWrites(t, d)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, toMira)

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

				status, data := apiJSON(t, h, "POST", door.path, owner, door.body)

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

			status, data := apiJSON(t, h, "POST", door.path, owner, door.body)

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
// holds the only connection. The request runs on its own goroutine under a
// deadline so that mistake fails here by name instead of as a suite timeout.
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

			answered := make(chan *httptest.ResponseRecorder, 1)
			go func() { answered <- apiRequest(t, h, "POST", "/api/tasks/T-1/claim", owner, "") }()
			var rec *httptest.ResponseRecorder
			select {
			case rec = <-answered:
			case <-time.After(10 * time.Second):
				t.Fatalf("the claim did not answer within 10s: the worker dismissal waits on a lock " +
					"or a connection it already holds")
			}

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

				status, data := apiJSON(t, h, "POST", dr.path, kip, dr.body)

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
