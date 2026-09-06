package main

// worker_pending_signals_ted79_test.go — T-ed79 parity #5 and #12: a worker
// owner-verb that has not landed must say so, exactly as the staff twin does.
//
// The staff side has carried relocation_pending (T-8655), relocation_deferred
// (T-927a) and activation_pending (T-ba62) for the same reason each time: a
// clean 200 with the new intent on the row is indistinguishable from a verb that
// actually took effect. The worker side carried none of the three — the panel
// parity doc listed it as A9 「外包端根本沒有訊號可顯示」 and deferred it because
// closing it means touching the frozen wire.
//
// WHERE THE THREE LIVE NOW (T-91, owner 2026-09-06): on the RECEIPTS these
// writes answer — AgentRelocateReceiptDTO and OutsourceRestartReceiptDTO — not
// on MemberDTO / OutsourceWorkerDTO, which no handler ever wrote them onto once
// the receipts existed and which therefore no longer declare them at all. Every
// assertion below reads a receipt body, which is why none of them moved.
//
// 🔴 THE T-ba62 SENTENCE IS ABOUT A BUG, NOT A NICETY: 「把這個回傳值丟掉就是整個
// bug —— 對一台連不上的機器按啟動，會回一個乾淨的成功、零訊號」. The same shape
// was still here on the worker side, unchanged.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func workerBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// ── #5: a 改機器 that opened a wind-down is PENDING and DEFERRED ─────────────

func TestRelocateWorker_WindDownIsPendingAndDeferred(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")

	body := workerBody(t, postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost))

	if body["relocation_pending"] != true {
		t.Errorf("relocation_pending = %v, want true: the pin is stored and the worker "+
			"is still on the OLD machine until its 收口 — reporting a clean landed 200 "+
			"is the same silent false-success T-8655 removed for staff",
			body["relocation_pending"])
	}
	if body["relocation_deferred"] != true {
		t.Errorf("relocation_deferred = %v, want true: this is a DELIBERATE deferral, "+
			"not a delivery failure, and a caller must be able to hold back the "+
			"\"nothing was dispatched\" alert for it (the T-927a distinction)",
			body["relocation_deferred"])
	}
}

// …and a move that actually went out is neither.
func TestRelocateWorker_DispatchedIsNotPending(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // no live session: nothing to flush
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	api.workerSpawnTarget[workerID] = ServerSelfHost

	body := workerBody(t, postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost))

	if body["relocation_pending"] == true {
		t.Error("a relocate whose start was dispatched must NOT report pending — a flag " +
			"that is always true says nothing")
	}
	if body["relocation_deferred"] == true {
		t.Error("nothing was deferred here")
	}
}

// ── #12: a 重啟 that dispatched nothing is PENDING ───────────────────────────

func TestRestartWorker_UndispatchedSurfacesActivationPending(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	// ACTIVE, no live session, and the server does not remember where its last
	// start went (a re-exec) ⇒ no kill target ⇒ the whole cycle defers.
	delete(api.workerSpawnTarget, workerID)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.DesiredState = DesiredStateOffline
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A pin that names no active machine. It goes through the pin's sole writer
	// since T-55 — desired_machine_id left PutMember's DO UPDATE SET, so the
	// whole-row seed above carries every other field but not this one.
	if err := api.dal.SetMemberDesiredMachineID(workerID, "m-nowhere"); err != nil {
		t.Fatalf("pin: %v", err)
	}

	body := workerBody(t, postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost))

	if body["activation_pending"] != true {
		t.Errorf("activation_pending = %v, want true. 重啟 against a machine that "+
			"cannot take the worker answered a clean 200 with zero signal — the exact "+
			"shape T-ba62 called 「整個 bug」 on the staff side, still here.",
			body["activation_pending"])
	}
	if body["last_op_reason"] == "" {
		t.Error("a pending restart must also name WHICH cause on last_op_reason — one " +
			"bit cannot answer why (#14)")
	}
}

func TestRestartWorker_DispatchedIsNotPending(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	api.workerSpawnTarget[workerID] = ServerSelfHost
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.DesiredState = DesiredStateOffline
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := workerBody(t, postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost))

	if body["activation_pending"] == true {
		t.Error("a restart whose worker_start was dispatched must NOT report pending")
	}
}
