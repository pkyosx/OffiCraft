package main

// member_op_reasons_ted79_test.go — T-ed79 parity #4 and #14: a staff member
// that did not move must say why, in the SAME vocabulary a worker uses.
//
// The worker side has carried a closed family of last_op_reason codes since the
// relocate-that-goes-nowhere report: "EVERY non-dispatch now leaves a receipt".
// Staff shared the FIELD (last_op / last_op_reason), the CLEARING seam
// (isPlacementBlockedReason) and the cockpit renderer — and produced exactly two
// of the codes. Everything else came back as a clean 200 with nothing on the row,
// or as a reconcileLog line on the server's stderr that no owner will ever read.
//
// The gates below are the ones an owner actually presses (#4 is the first two:
// 「被按住時沒有為什麼沒動」).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// heldDownMember is desired-offline WITHOUT a stop anchor — since T-14 項目 7
// that is one specific member, not "a member the owner stopped": a new hire
// before its first 活化. A real 停止 leaves stopping_since behind forever
// (nothing but 活化 and the queued-restart consumer clears it), and on that row
// an owner verb queues a start instead of parking a held_down receipt. Use
// stoppedMember for that half.
func heldDownMember(t *testing.T, s *apiServer, id string) {
	t.Helper()
	m := testAgent(id)
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
}

// stoppedMember is the row shape a converged 停止 actually leaves: desired
// offline AND the anchor still on the row.
func stoppedMember(t *testing.T, s *apiServer, id string) {
	t.Helper()
	m := testAgent(id)
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-a"
	m.StoppingSince = 9990
	putTestMember(t, s, m)
}

func reasonOf(t *testing.T, s *apiServer, id string) string {
	t.Helper()
	m, err := s.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("re-read member %s: %v", id, err)
	}
	return m.LastOpReason
}

// ── #4 / G1: a launch-intent edit on a member the owner is holding down ──────

func TestUpdateMemberOnAHeldDownMemberLeavesAReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	heldDownMember(t, s, "m-held")

	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-held",
			map[string]any{"model": "claude-opus-4-9"}, wireOwnerID, "owner"), "m-held")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	got := reasonOf(t, s, "m-held")
	if !strings.HasPrefix(got, spawnReasonHeldDown+":") {
		t.Errorf("last_op_reason = %q, want a %q receipt. The value WAS saved and "+
			"nothing was started, and there are three different ways to be in that "+
			"state — held down, offline, already collected — which all collapse into "+
			"one clean 200. The worker face has written this receipt since the "+
			"reason-code family landed.", got, spawnReasonHeldDown)
	}
	if !strings.Contains(got, askForActivate) {
		t.Errorf("last_op_reason = %q dropped 「%s」 — a member nobody has ever asked "+
			"to stop is still waiting on the owner", got, askForActivate)
	}

	// 🔴 THE OTHER HALF, since T-14 項目 7 — same held_down: prefix, opposite
	// instruction. Unlike relocate, PATCH runs no reconcile, so this row does
	// still carry memberRestartQueuedReceipt verbatim.
	stoppedMember(t, s, "m-stopped")
	rec = httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-stopped",
			map[string]any{"model": "claude-opus-4-9"}, wireOwnerID, "owner"), "m-stopped")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if got, want := reasonOf(t, s, "m-stopped"), memberRestartQueuedReceipt(memberOpModel); got != want {
		t.Errorf("the PATCH /api/members/{id} receipt call site (api_members.go, the "+
			"memberOpModel gate) wrote last_op_reason = %q, want %q: 換 model on a "+
			"stopped member is the 重啟, so the owner is told it will come back up "+
			"rather than to 活化 it", got, want)
	}
}

// ── #4 / G2: a 改機器 on a member the owner is holding down ──────────────────

func TestRelocateAHeldDownMemberLeavesAReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	putWarden(t, s, "mach-b")
	heldDownMember(t, s, "m-heldmove")

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-heldmove/relocate",
			map[string]any{"machine_id": "mach-b"}, wireOwnerID, "owner"), "m-heldmove")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	if got := reasonOf(t, s, "m-heldmove"); !strings.HasPrefix(got, spawnReasonHeldDown+":") {
		t.Errorf("last_op_reason = %q, want a %q receipt: the pin was stored and "+
			"nothing was moved, and the row says nothing about which of those two "+
			"halves happened", got, spawnReasonHeldDown)
	}
	if got := reasonOf(t, s, "m-heldmove"); !strings.Contains(got, askForActivate) {
		t.Errorf("last_op_reason = %q dropped 「%s」 — a member nobody has ever asked "+
			"to stop is still waiting on the owner", got, askForActivate)
	}

	// 🔴 THE OTHER HALF, since T-14 項目 7. Every receipt in this family opens with
	// the SAME held_down: prefix, so the assertion above cannot tell them apart
	// and would stay green if the two branches were swapped. What separates them
	// is the one clause the owner acts on.
	//
	// 🔴 WHICH RECEIPT LANDS HERE IS NOT memberRestartQueuedReceipt, and that is
	// the handler, not the test: relocate runs the event-driven reconcile before
	// it returns, the member is already converged (no session, anchor set), so
	// consumeRestartAfterStop spends the intent and overwrites the receipt in the
	// same request. memberRestartQueuedReceipt(relocate) is only ever READ on a
	// member whose stop is still landing. Asserting the exact sentence would pin
	// that race; asserting the clause pins what the owner is told either way.
	stoppedMember(t, s, "m-stoppedmove")
	rec = httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-stoppedmove/relocate",
			map[string]any{"machine_id": "mach-b"}, wireOwnerID, "owner"), "m-stoppedmove")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	if got := reasonOf(t, s, "m-stoppedmove"); strings.Contains(got, askForActivate) {
		t.Errorf("last_op_reason = %q still tells the owner to 活化. Since T-14 項目 7 "+
			"改機器 on a stopped member IS the 重啟 — nothing more is needed from him", got)
	}

	// …and the row the queued sentence is actually READ on: a stop still
	// landing. The converged row above has its receipt overwritten by
	// consumeRestartAfterStop inside the same request, so it can only ever pin
	// the clause; with a live session the intent is not spendable yet and the
	// relocate call site's own sentence survives to be observed verbatim.
	// Without this row that call site is pinned by nothing.
	stoppedMember(t, s, "m-stoplanding")
	connectOnlineMachine(t, s, "m-stoplanding", "mach-a")
	rec = httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-stoplanding/relocate",
			map[string]any{"machine_id": "mach-b"}, wireOwnerID, "owner"), "m-stoplanding")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	if got, want := reasonOf(t, s, "m-stoplanding"), memberRestartQueuedReceipt(memberOpRelocate); got != want {
		t.Errorf("the POST /api/members/{id}/relocate receipt call site (api_members.go, "+
			"the memberOpRelocate gate) wrote last_op_reason = %q, want %q: the stop is "+
			"still landing, so the owner is told it is honoured as-is and the member "+
			"comes back up — not to 活化 it himself", got, want)
	}
}

// askForActivate is the T-ed79 clause this ticket makes conditional: it is the
// whole owner-facing difference between the two held_down receipts, which are
// otherwise prefix-identical.
const askForActivate = "活化 it when you want it to run"

// ── #14 / G3: activation_pending must say WHICH pending ─────────────────────

func TestActivatePendingNamesItsCause(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-gone") // a machine row, but its warden holds no live SSE
	m := testAgent("m-nowhere")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-gone"
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-nowhere/activate", nil, wireOwnerID, "owner"),
		"m-nowhere")
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["activation_pending"] != true {
		t.Fatalf("fixture: this activate was supposed to end pending, body=%s",
			rec.Body.String())
	}
	got := reasonOf(t, s, "m-nowhere")
	if strings.TrimSpace(got) == "" {
		t.Errorf("activation_pending=true with an EMPTY last_op_reason. The flag says " +
			"'nothing has been dispatched yet' and the code itself lists at least four " +
			"different states that reach it (backoff, circuit-open, an unbuildable " +
			"frame, an unreachable warden). One bit cannot answer 'why'.")
	}
}

// ── #14 / G4: the diagnoses that only ever reached stderr ────────────────────

func tickWithState(t *testing.T, s *apiServer, id string, st reconcileState, now float64) {
	t.Helper()
	m, _ := s.dal.GetMember(id)
	s.reconcileMu.Lock()
	s.reconcileStates[id] = st
	s.reconcileTickMemberLocked(*m, now)
	s.reconcileMu.Unlock()
}

func TestStalledWakeDiagnosesLandOnTheRowNotOnlyOnStderr(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
		st   func() reconcileState
	}{
		{
			name: "circuit open",
			code: spawnReasonCircuitOpen,
			st: func() reconcileState {
				st := newReconcileState()
				st.CircuitOpen = true
				st.CircuitCooldownUntil = 9_000_000.0
				return st
			},
		},
		{
			name: "backoff",
			code: spawnReasonBackoff,
			st: func() reconcileState {
				st := newReconcileState()
				st.BackoffUntil = 9_000_000.0
				return st
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			putWarden(t, s, "mach-a")
			m := testAgent("m-stalled")
			m.DesiredState = DesiredStateOnline
			m.DesiredMachineID = "mach-a"
			putTestMember(t, s, m)

			tickWithState(t, s, "m-stalled", tc.st(), 1_000_000.0)

			got := reasonOf(t, s, "m-stalled")
			if !strings.HasPrefix(got, tc.code+":") {
				t.Errorf("last_op_reason = %q, want a %q receipt. This member wants to "+
					"be online and the tick knows exactly why it is not being started; "+
					"today that sentence goes to the server's stderr and the cockpit "+
					"shows an unexplained grey row — the exact blank the worker "+
					"reason-code family was written to remove.", got, tc.code)
			}
		})
	}
}

// …and the other direction: a member that IS converged owes nobody an
// explanation, so the tick must not start writing receipts at every member.
func TestAConvergedMemberIsNotStampedWithAReason(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-fine")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnlineMachine(t, s, "m-fine", "mach-a")

	tickWithState(t, s, "m-fine", newReconcileState(), 1_000_000.0)

	if got := reasonOf(t, s, "m-fine"); strings.TrimSpace(got) != "" {
		t.Errorf("a converged online member was stamped %q — 'online: converged' is "+
			"not a stall and owes no receipt; stamping it would turn every healthy "+
			"member into a permanent SSE event stream", got)
	}
}

// 🔴 The single-slot precedence rule. wake_timeout says the start was dispatched
// and the agent never came up; the very next tick is the BACK-OFF that follows
// it. Both want the one last_op_reason slot, and the retry's description must
// not erase the previous attempt's diagnosis.
func TestBackoffDoesNotEraseTheWakeTimeoutDiagnosis(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-lapsed")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.LastOp = reconcileCmdStart
	m.LastOpReason = wakeTimeoutReasonCode + ": dispatched, never came up"
	putTestMember(t, s, m)

	st := newReconcileState()
	st.BackoffUntil = 9_000_000.0
	tickWithState(t, s, "m-lapsed", st, 1_000_000.0)

	if got := reasonOf(t, s, "m-lapsed"); !strings.HasPrefix(got, wakeTimeoutReasonCode+":") {
		t.Errorf("last_op_reason = %q — the back-off receipt overwrote the wake_timeout "+
			"diagnosis. 'we are waiting to retry' is a description of the wait; "+
			"'the start went out and nothing came up' is the only sentence that says "+
			"what went wrong, and it is the one the retry loop would blank on every "+
			"tick.", got)
	}
}

// 🔴 …and the LIMIT of that rule. It protects one thing: a diagnosis of the
// PREVIOUS attempt must not be blanked by a description of the CURRENT wait.
// "backoff" and "circuit_open" are those descriptions. zombie_suspect and
// warden_unreachable are not — they are fresh, actionable findings about what is
// happening right now, strictly more informative than a stale wake_timeout, and
// a guard that looks only at what is ON the row swallows them too.
func TestAFreshDiagnosisIsNotSwallowedByAStickyOne(t *testing.T) {
	for _, tc := range []struct {
		name     string
		incoming string
		wantWins bool
	}{
		{"zombie suspect", spawnReasonZombieSuspect, true},
		{"warden unreachable", spawnReasonWardenLost, true},
		{"backoff", spawnReasonBackoff, false},
		{"circuit open", spawnReasonCircuitOpen, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			putWarden(t, s, "mach-a")
			m := testAgent("m-sticky")
			m.DesiredState = DesiredStateOnline
			m.DesiredMachineID = "mach-a"
			m.LastOp = reconcileCmdStart
			m.LastOpReason = wakeTimeoutReasonCode + ": dispatched, never came up"
			putTestMember(t, s, m)

			s.stampMemberOpBlocked("m-sticky", tc.incoming+": the fresh finding", 1_000_000.0)

			got := reasonOf(t, s, "m-sticky")
			won := strings.HasPrefix(got, tc.incoming+":")
			if won == tc.wantWins {
				return
			}
			if tc.wantWins {
				t.Errorf("last_op_reason = %q — %q was dropped in favour of a wake_timeout "+
					"from the previous attempt. The precedence rule exists to stop the RETRY "+
					"LOOP blanking a diagnosis, and this is not the retry loop: it is a new "+
					"finding about what is wrong NOW, and the owner is left reading a stale "+
					"sentence about a start that already failed.", got, tc.incoming)
				return
			}
			t.Errorf("last_op_reason = %q — %q is a description of the CURRENT wait and it "+
				"overwrote the only sentence that says what actually went wrong. Narrowing "+
				"the guard must not let go of the thing the guard is for.", got, tc.incoming)
		})
	}
}
