package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ocserverd/txguard"
)

// lockInTxStaged counts the refusals tests stage on purpose. Any other refusal
// fails the run, including one on a goroutine no test was watching.
var lockInTxStaged atomic.Int64

func TestMain(m *testing.M) {
	code := m.Run()
	if got, staged := txguard.Violations(), lockInTxStaged.Load(); got != staged {
		fmt.Fprintf(os.Stderr, "FAIL: %d lock(s) refused inside a write transaction, %d staged by tests; "+
			"the others are real — see the [lock] ERROR lines\n", got, staged)
		code = 1
	}
	os.Exit(code)
}

// A real deadlock never answers; these tests fail by name instead of hanging
// the suite until its -timeout.
const reentryAnswerWithin = 10 * time.Second

func reentryJSON(t *testing.T, h http.Handler, method, target, token, body string) (int, map[string]any) {
	t.Helper()
	answered := make(chan []byte, 1)
	codes := make(chan int, 1)
	go func() {
		rec := apiRequest(t, h, method, target, token, body)
		codes <- rec.Code
		answered <- rec.Body.Bytes()
	}()
	select {
	case code := <-codes:
		raw := <-answered
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatalf("%s %s: non-JSON body (%d): %s", method, target, code, raw)
		}
		return code, data
	case <-time.After(reentryAnswerWithin):
		t.Fatalf("%s %s did not answer within %s: it is waiting on the write connection or a lock it cannot get",
			method, target, reentryAnswerWithin)
		return 0, nil
	}
}

// reentryInsideTheSetPriorityTx runs fn once, on the handler's goroutine, inside
// the transaction that sets T-1's priority to low, just before it commits. It
// recognises that transaction by reading the priority through the write pool,
// which on that goroutine is the transaction itself.
func reentryInsideTheSetPriorityTx(t *testing.T, d *DAL, fn func() error) {
	t.Helper()
	var fired atomic.Bool
	d.wdb.beforeCommit = func() error {
		if fired.Load() {
			return nil
		}
		var priority string
		if err := d.wdb.QueryRow(`SELECT priority FROM task WHERE id = 'T-1'`).Scan(&priority); err != nil {
			return err
		}
		if priority != TaskPriorityLow {
			return nil
		}
		fired.Store(true)
		return fn()
	}
	t.Cleanup(func() {
		if !fired.Load() {
			t.Errorf("the staged work never ran inside the set-priority transaction")
		}
	})
}

func reentryCard() (ReplyCard, ChatMessage) {
	msg := ChatMessage{
		ID: "c-1", Sender: apiTestPlainAgentID, Recipient: wireOwnerID,
		Body: "which yard", TS: 1700000100, Meta: map[string]any{},
	}
	card := ReplyCard{
		ID: "rc-1", FromMember: apiTestPlainAgentID, Kind: replyCardKindDecision,
		Summary: "which yard", Options: []ReplyCardOption{{Text: "north"}, {Text: "south"}},
		SelectMode: replyCardSelectModeSingle, Status: replyCardStatusWaiting,
		CreatedTS: 1700000100, ChatMessageID: "c-1",
	}
	return card, msg
}

func reentryWantCard(t *testing.T, d *DAL, landed bool) {
	t.Helper()
	card, err := d.GetReplyCard("rc-1")
	if err != nil {
		t.Fatalf("GetReplyCard: %v", err)
	}
	msgs, err := d.ListChatByIDs([]string{"c-1"})
	if err != nil {
		t.Fatalf("ListChatByIDs: %v", err)
	}
	if landed && (card == nil || len(msgs) != 1) {
		t.Fatalf("want the card and its chat message written, got card=%v messages=%d", card, len(msgs))
	}
	if !landed && (card != nil || len(msgs) != 0) {
		t.Fatalf("want neither the card nor its chat message, got card=%v messages=%d", card, len(msgs))
	}
}

func reentryRefuseCards(t *testing.T, d *DAL) {
	t.Helper()
	if _, err := d.wdb.Exec(`CREATE TRIGGER refuse_cards BEFORE INSERT ON reply_card
		BEGIN SELECT RAISE(ABORT, 'reply cards are refused'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

func TestADALWriteInsideAHandlersTransactionJoinsIt(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			_, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			card, msg := reentryCard()
			reentryInsideTheSetPriorityTx(t, d, func() error {
				if err := d.TouchTaskUpdatedTS("T-1", 1800000000); err != nil {
					return err
				}
				return d.PutReplyCardWithChat(card, msg, nil)
			})

			status, body := reentryJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, body)
			}
			apiWantBody(t, body, map[string]any{"task_id": "T-1", "priority": "low", "frozen_by": ""})
			task.Priority = TaskPriorityLow
			task.FrozenBy = ""
			task.UpdatedTS = 1800000000
			dalWantTask(t, d, task)
			reentryWantCard(t, d, true)
		})
	}
}

func TestAFailedDALWriteInsideAHandlersTransactionRollsBackTheWholeRequest(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			_, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			reentryRefuseCards(t, d)
			card, msg := reentryCard()
			reentryInsideTheSetPriorityTx(t, d, func() error {
				if err := d.TouchTaskUpdatedTS("T-1", 1800000000); err != nil {
					return err
				}
				return d.PutReplyCardWithChat(card, msg, nil)
			})

			status, body := reentryJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, body)
			}
			apiWantError(t, body, "internal_error",
				"internal error: constraint failed: reply cards are refused (1811)")
			dalWantTask(t, d, task)
			reentryWantCard(t, d, false)
		})
	}
}

func TestAFailedNestedTransactionLeavesNoneOfItsWritesWhenTheOuterCarriesOn(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			_, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			reentryRefuseCards(t, d)
			card, msg := reentryCard()
			var innerErr error
			reentryInsideTheSetPriorityTx(t, d, func() error {
				innerErr = d.PutReplyCardWithChat(card, msg, nil)
				return nil
			})

			status, body := reentryJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, body)
			}
			if innerErr == nil || innerErr.Error() != "constraint failed: reply cards are refused (1811)" {
				t.Fatalf("the nested write must fail on the refused card, got %v", innerErr)
			}
			got, err := d.GetTask("T-1")
			if err != nil || got == nil {
				t.Fatalf("GetTask: %v %v", got, err)
			}
			task.Priority = TaskPriorityLow
			task.FrozenBy = ""
			task.UpdatedTS = got.UpdatedTS
			dalWantTask(t, d, task)
			reentryWantCard(t, d, false)
		})
	}
}

func TestALockTakenInsideAHandlersTransactionFailsTheRequestAtOnce(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			task := dalPutTask(t, d, windowOpenTask("T-1"))
			logs := apiCaptureStandardLog(t)
			// Nobody else holds settingsMu: the refusal does not depend on a
			// second goroutine, which is what separates it from the deadlock.
			reentryInsideTheSetPriorityTx(t, d, func() error {
				if err := d.TouchTaskUpdatedTS("T-1", 1800000000); err != nil {
					return err
				}
				_ = api.outsourceParallelCap()
				return nil
			})

			status, body := reentryJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, body)
			}
			lockInTxStaged.Add(1)
			refusal, _ := body["error"].(map[string]any)
			if refusal["code"] != "internal_error" {
				t.Fatalf("want error code internal_error, got %v", body)
			}
			const wantMsg = "internal error: a lock was taken while holding the write transaction (at (*apiServer).outsourceParallelCap (api_stub.go:"
			if msg, _ := refusal["message"].(string); !strings.HasPrefix(msg, wantMsg) {
				t.Fatalf("message:\n got %q\nwant prefix %q", msg, wantMsg)
			}
			reentryWantLogLine(t, logs.String(),
				"[lock] ERROR: refused a lock taken while this goroutine holds the write transaction; "+
					"the transaction is rolled back; at: (*apiServer).outsourceParallelCap (api_stub.go:")
			dalWantTask(t, d, task)

			status, body = reentryJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)

			if status != http.StatusOK {
				t.Fatalf("the next request must land: want 200, got %d (%v)", status, body)
			}
			apiWantBody(t, body, map[string]any{"task_id": "T-1", "priority": "low", "frozen_by": ""})
		})
	}
}

func reentryWantLogLine(t *testing.T, logs, prefix string) {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, prefix) {
			return
		}
	}
	t.Fatalf("no log line containing %q; log was:\n%s", prefix, logs)
}
