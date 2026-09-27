package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The holder of each transaction below is stuck on something the pool cannot
// see and that never finishes by itself. Only the hold limit gives the
// connection back.

const holdTestLimit = 300 * time.Millisecond

const holdExpiredMsg = "internal error: write transaction rolled back: held longer than the limit (300ms)"

func holdDAL(t *testing.T, shape string) *DAL {
	t.Helper()
	d, _, _ := windowDAL(t, shape)
	d.wdb.holdLimit = holdTestLimit
	return d
}

// holdJSONAsync starts a request and answers a function that waits for it at
// most reentryAnswerWithin. The request goroutine never touches t.
func holdJSONAsync(t *testing.T, h http.Handler, method, target, token, body string) func() (int, map[string]any) {
	t.Helper()
	type answer struct {
		code int
		raw  []byte
	}
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	done := make(chan answer, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- answer{rec.Code, rec.Body.Bytes()}
	}()
	return func() (int, map[string]any) {
		t.Helper()
		select {
		case a := <-done:
			var data map[string]any
			if err := json.Unmarshal(a.raw, &data); err != nil {
				t.Fatalf("%s %s: non-JSON body (%d): %s", method, target, a.code, a.raw)
			}
			return a.code, data
		case <-time.After(reentryAnswerWithin):
			t.Fatalf("%s %s did not answer within %s", method, target, reentryAnswerWithin)
			return 0, nil
		}
	}
}

func holdWantExpiryLogged(t *testing.T, logs string) {
	t.Helper()
	reentryWantLogLine(t, logs,
		"[wdb] ERROR: rolled back a write transaction held longer than 300ms; begun at: (*DAL).inTx (dal.go:")
	reentryWantLogLine(t, logs, "<- (*apiServer).HandleSetTaskPriorityApiTasksTaskIdPriorityPost (api_tasks.go:")
}

func TestATransactionStuckOnAWaitThatNeverEndsIsRolledBackAtItsHoldLimit(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d := holdDAL(t, shape)
			_, h, _, owner := newAPITestServerOn(t, d)
			t1 := dalPutTask(t, d, windowOpenTask("T-1"))
			dalPutTask(t, d, windowOpenTask("T-2"))
			logs := recoveryCaptureLog(t)
			stuck := make(chan struct{})
			never := make(chan struct{})
			released := false
			t.Cleanup(func() {
				if !released {
					close(never)
				}
			})
			reentryInsideTheSetPriorityTx(t, d, func() error {
				if err := d.TouchTaskUpdatedTS("T-1", 1800000000); err != nil {
					return err
				}
				close(stuck)
				<-never
				return nil
			})

			answerA := holdJSONAsync(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)
			select {
			case <-stuck:
			case <-time.After(reentryAnswerWithin):
				t.Fatalf("the first request never got stuck inside its transaction")
			}
			status, body := reentryJSON(t, h, "POST", "/api/tasks/T-2/priority", owner, `{"priority":"high"}`)

			if status != http.StatusOK {
				t.Fatalf("a write behind the stuck transaction: want 200, got %d (%v)", status, body)
			}
			apiWantBody(t, body, map[string]any{"task_id": "T-2", "priority": "high", "frozen_by": ""})
			dalWantTask(t, d, t1)
			holdWantExpiryLogged(t, logs.String())

			close(never)
			released = true
			status, body = answerA()

			if status != http.StatusInternalServerError {
				t.Fatalf("the stuck request: want 500, got %d (%v)", status, body)
			}
			apiWantError(t, body, "internal_error", holdExpiredMsg)
			dalWantTask(t, d, t1)

			status, body = reentryJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			if status != http.StatusOK {
				t.Fatalf("the next write on T-1: want 200, got %d (%v)", status, body)
			}
			apiWantBody(t, body, map[string]any{"task_id": "T-1", "priority": "low", "frozen_by": ""})
		})
	}
}

func TestATransactionWaitingOnAPlainMutexWhoseHolderWaitsToWriteIsRolledBackAtItsHoldLimit(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d := holdDAL(t, shape)
			_, h, _, owner := newAPITestServerOn(t, d)
			t1 := dalPutTask(t, d, windowOpenTask("T-1"))
			dalPutTask(t, d, windowOpenTask("T-2"))
			logs := recoveryCaptureLog(t)
			// A lock the server's own txguard types do not cover: nothing refuses
			// it inside the transaction.
			var mu sync.RWMutex
			mu.Lock()
			held := true
			t.Cleanup(func() {
				if held {
					mu.Unlock()
				}
			})
			stuck := make(chan struct{})
			reentryInsideTheSetPriorityTx(t, d, func() error {
				if err := d.TouchTaskUpdatedTS("T-1", 1800000000); err != nil {
					return err
				}
				close(stuck)
				mu.Lock()
				defer mu.Unlock()
				return nil
			})

			// A: holds the write connection, waiting for mu.
			answerA := holdJSONAsync(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)
			select {
			case <-stuck:
			case <-time.After(reentryAnswerWithin):
				t.Fatalf("the first request never reached mu inside its transaction")
			}
			// B: this goroutine holds mu and waits for a write.
			status, body := reentryJSON(t, h, "POST", "/api/tasks/T-2/priority", owner, `{"priority":"high"}`)
			mu.Unlock()
			held = false

			if status != http.StatusOK {
				t.Fatalf("the writer holding mu: want 200, got %d (%v)", status, body)
			}
			apiWantBody(t, body, map[string]any{"task_id": "T-2", "priority": "high", "frozen_by": ""})
			holdWantExpiryLogged(t, logs.String())

			status, body = answerA()

			if status != http.StatusInternalServerError {
				t.Fatalf("the transaction waiting on mu: want 500, got %d (%v)", status, body)
			}
			apiWantError(t, body, "internal_error", holdExpiredMsg)
			dalWantTask(t, d, t1)

			status, body = reentryJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			if status != http.StatusOK {
				t.Fatalf("the next write on T-1: want 200, got %d (%v)", status, body)
			}
			apiWantBody(t, body, map[string]any{"task_id": "T-1", "priority": "low", "frozen_by": ""})
		})
	}
}
