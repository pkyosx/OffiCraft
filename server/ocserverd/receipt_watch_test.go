package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// T-b36a step 3 — a start/stop whose command_result never comes back must stop
// being indistinguishable from one that went perfectly.
//
// The warden POSTs its receipt best-effort: a DNS fault, a refused connection,
// a 500 from the server all collapse into a return value the start/stop callers
// discarded. Nothing was written anywhere — and the warden's own log file has,
// measured, no readers at all, so "log it louder on the host" is not a signal.
// The server is the side that is OWED the receipt, so the server is the only
// side that can observe its absence. These tests pin that observation all the
// way out to the JSON the cockpit reads.

// receiptDTOReason marshals the member DTO the cockpit's member detail panel
// consumes and returns its last_op_reason string. Asserting on the SERVED JSON
// (not the DB struct) is the point: the reader chain is
// last_op_reason → frontend/src/api/mappers.ts → MemberDetailPanel →
// AgentDetailPanel's `!lastOpOk && lastOpReason` block. A stamp that stopped at
// the row would be another log with no reader.
func receiptDTOReason(t *testing.T, s *apiServer, memberID string) (string, bool) {
	t.Helper()
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil {
		t.Fatalf("reload member %s: %v", memberID, err)
	}
	raw, err := json.Marshal(s.newMemberDTO(*m, "", "", 0))
	if err != nil {
		t.Fatalf("marshal member DTO: %v", err)
	}
	var dto struct {
		LastOpReason string `json:"last_op_reason"`
		LastOpOK     *bool  `json:"last_op_ok"`
		LastOp       string `json:"last_op"`
	}
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatalf("unmarshal member DTO: %v", err)
	}
	// The FE only renders the reason when the op is NOT ok — a reason stamped
	// with ok=true would be invisible in the exact panel it was written for.
	rendered := dto.LastOpOK != nil && !*dto.LastOpOK
	return dto.LastOpReason, rendered
}

// The load-bearing case: a START lands on a warden, no receipt ever arrives,
// and the deadline turns that silence into a named reason on the wire the
// cockpit reads.
func TestReceiptDeadline_UnansweredStartSurfacesOnTheMemberWire(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-quiet")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	dec := s.reconcileMemberNow("m-quiet")
	if dec.Command != reconcileCmdStart {
		t.Fatalf("expected a landed START, got %q (%s)", dec.Command, dec.Reason)
	}

	// NEGATIVE CONTROL, and it must run before the positive one: inside the
	// window the silence is not yet news. Without this the test below would
	// pass just as well against a stamp that fires unconditionally.
	s.sweepLapsedReceipts(nowSecs())
	if reason, _ := receiptDTOReason(t, s, "m-quiet"); strings.Contains(reason, receiptMissingReasonCode) {
		t.Fatalf("a receipt still inside its window must not be stamped; got %q", reason)
	}

	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	reason, rendered := receiptDTOReason(t, s, "m-quiet")
	if !strings.HasPrefix(reason, receiptMissingReasonCode+":") {
		t.Fatalf("an unanswered START must carry the %s code on the member wire; got %q",
			receiptMissingReasonCode, reason)
	}
	if !rendered {
		t.Fatalf("the stamp must set last_op_ok=false — the cockpit only renders the "+
			"reason for a NOT-ok op, so an ok stamp reaches nobody; got %q", reason)
	}
	if !strings.Contains(reason, "mach-live") {
		t.Fatalf("the reason must name the machine the frame went to; got %q", reason)
	}
	// The whole point of the sentence: the outcome is UNKNOWN, not failed.
	// A reason the owner reads as "the start failed" would send them to
	// re-fire against a member that may already be running.
	if !strings.Contains(reason, "UNKNOWN") {
		t.Fatalf("the reason must say the outcome is unknown rather than failed; got %q", reason)
	}
}

// The discriminating half: a receipt that ARRIVES disarms the deadline, so a
// healthy fleet is never stamped. Without this the feature would be a
// permanent false alarm on every member.
func TestReceiptDeadline_ArrivedReceiptDisarmsTheStamp(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-answers")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	if dec := s.reconcileMemberNow("m-answers"); dec.Command != reconcileCmdStart {
		t.Fatalf("expected a landed START, got %q", dec.Command)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "m-answers",
		"rpc":       reconcileCmdStart,
		"ok":        true,
		"reason":    "",
		"log":       "spawned",
	}, triggerServer)

	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	if reason, _ := receiptDTOReason(t, s, "m-answers"); strings.Contains(reason, receiptMissingReasonCode) {
		t.Fatalf("an answered START must never be stamped receipt_missing; got %q", reason)
	}
}

// The subtle one. A no-op stop receipt (T-9adc) is DELIBERATELY not folded onto
// last_op — the fold returns early. If the deadline were disarmed by a
// successful fold rather than by the receipt's ARRIVAL, every idempotent stop
// (identity sweeps broadcast these to every warden) would later be stamped
// receipt_missing even though its receipt was received and read.
func TestReceiptDeadline_NoOpStopReceiptStillDisarmsTheDeadline(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-noop")
	m.DesiredMachineID = "mach-live"
	m.DesiredState = DesiredStateOffline
	putTestMember(t, s, m)
	connectOnline(t, s, "m-noop") // online + desired offline ⇒ the STOP arm
	// 下線 no longer collects on a timer (owner's ruling — see decideDown), so
	// this test drives the TIMED wind-down explicitly: it is about receipts,
	// not about who decides time is up.
	s.reconcileCfg.SoftOffboardGrace = 0
	// Skip past the self-stop grace window so this tick dispatches the robust
	// STOP rather than opening the clock (decideDown's first observation).
	s.reconcileStates["m-noop"] = reconcileState{
		Phase:        reconcilePhaseStopping,
		LastCommand:  reconcileCmdNone,
		StopDeadline: nowSecs() - 1,
	}

	if dec := s.reconcileMemberNow("m-noop"); dec.Command != reconcileCmdStop {
		t.Fatalf("expected a landed STOP, got %q", dec.Command)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "m-noop",
		"rpc":       reconcileCmdStop,
		"ok":        true,
		"reason":    stopNoopReasonPrefix + ": stop was a no-op",
		"log":       "session=member-m-noop",
	}, triggerServer)

	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	if reason, _ := receiptDTOReason(t, s, "m-noop"); strings.Contains(reason, receiptMissingReasonCode) {
		t.Fatalf("a no-op stop receipt ARRIVED — the deadline must be disarmed by "+
			"arrival, not by a successful fold; got %q", reason)
	}
}

// A worker start rides the member verbs since P5b, so its unanswered receipt
// must land on the worker row (the cockpit's worker detail panel reads the same
// last_op_reason field) rather than on nothing.
func TestReceiptDeadline_UnansweredWorkerStartSurfacesOnTheWorkerRow(t *testing.T) {
	s := newReconcileTestServer(t)
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-quiet", Codename: "O-9", Runtime: "claude",
		Status: WorkerStatusActive,
	})

	s.armReceiptWatch("ow-quiet", reconcileCmdStart, "mach-live", nowSecs())

	s.sweepLapsedReceipts(nowSecs()) // negative control: still inside the window
	w, err := s.dal.GetOutsourceWorker("ow-quiet")
	if err != nil || w == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if strings.Contains(w.LastOpReason, receiptMissingReasonCode) {
		t.Fatalf("inside the window nothing may be stamped; got %q", w.LastOpReason)
	}

	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	w, err = s.dal.GetOutsourceWorker("ow-quiet")
	if err != nil || w == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if !strings.HasPrefix(w.LastOpReason, receiptMissingReasonCode+":") {
		t.Fatalf("an unanswered worker START must stamp the worker row; got %q", w.LastOpReason)
	}
	if w.LastOpOK == nil || *w.LastOpOK {
		t.Fatalf("the worker stamp must set last_op_ok=false; got %v", w.LastOpOK)
	}
}

// The stamp fires ONCE per dispatch, not every tick. A re-stamping sweep would
// re-write last_op_at and fan an SSE delta every 30s forever for a member
// nobody is touching — turning one lost receipt into a permanent event stream.
func TestReceiptDeadline_StampsOncePerDispatch(t *testing.T) {
	s := newReconcileTestServer(t)
	putTestMember(t, s, testAgent("m-once"))

	s.armReceiptWatch("m-once", reconcileCmdStop, "mach-live", nowSecs())
	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)

	m, err := s.dal.GetMember("m-once")
	if err != nil || m == nil {
		t.Fatalf("reload member: %v", err)
	}
	firstAt := m.LastOpAt
	if firstAt == 0 {
		t.Fatalf("the first sweep must stamp; last_op_at is still zero")
	}

	s.sweepLapsedReceipts(nowSecs() + 10*receiptDeadlineSecs)
	m, err = s.dal.GetMember("m-once")
	if err != nil || m == nil {
		t.Fatalf("reload member: %v", err)
	}
	if m.LastOpAt != firstAt {
		t.Fatalf("a lapsed watch must be consumed by its first sweep; last_op_at moved %v → %v",
			firstAt, m.LastOpAt)
	}
}

// An UNLANDED dispatch must not arm a watch: nothing left the server, so
// blaming the receipt channel would point the owner at the wrong suspect (the
// dispatch stamps already explain that case in their own words).
func TestReceiptDeadline_UnlandedDispatchArmsNothing(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-dead") // on the roster, never connected

	m := testAgent("m-unsent")
	m.DesiredMachineID = "mach-dead"
	putTestMember(t, s, m)

	dec := s.reconcileMemberNow("m-unsent")
	if !dec.DispatchUnlanded {
		t.Fatalf("expected an unlanded dispatch, got %+v", dec)
	}
	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	if reason, _ := receiptDTOReason(t, s, "m-unsent"); strings.Contains(reason, receiptMissingReasonCode) {
		t.Fatalf("an unlanded START must not be blamed on the receipt channel; got %q", reason)
	}
}
