package main

import (
	"fmt"
	"sort"
)

// ── the receipt deadline (T-b36a step 3) ─────────────────────────────────────
//
// THE HOLE THIS CLOSES. A start/stop frame that a warden ACCEPTS is answered by
// exactly one thing: a command_result receipt POSTed back to /telemetry, folded
// onto last_op* (api_monitoring.go foldCommandResult). That receipt is the only
// evidence the op ever executed. On the warden the POST is best-effort — the
// reporter swallows a transport fault or a non-2xx into a return value that the
// start/stop callers historically discarded (cli/ocwarden/command.go), so a
// receipt that never lands leaves NOTHING anywhere: no member row change, no
// server log, and (measured, 2026-07-28) no reader on the warden's own log file.
// From the cockpit "the op ran and we never heard back" and "the op ran fine"
// rendered IDENTICALLY — the same shape of silence stampWakeObservability and
// stampWorkerPlacementBlocked were written to remove, one layer further out.
//
// WHY SERVER-SIDE. The warden cannot report that its own report failed — the
// failing channel IS the report. Only the side that is OWED the receipt can
// observe its absence. This is the same move reconcile.go's wake_timeout makes:
// the server holds a deadline of its own and writes what the deadline proved.
//
// WHAT IT DOES NOT KNOW. The absence of a receipt does NOT say the op failed,
// nor that it ran. The frame may have died in the SSE downlink, the op may have
// run and the POST been refused, or the warden may have died between the two.
// The reason text says exactly that much and no more.

// receiptMissingReasonCode is the structured code the deadline writes. Like
// wake_timeout it is a DISPATCH-level diagnosis (nothing came back), not an
// execution outcome, so it is deliberately ABSENT from spawnBlockedReasonCodes:
// a later START must not erase the record that the previous one went unanswered.
const receiptMissingReasonCode = "receipt_missing"

// receiptDeadlineSecs is how long a landed start/stop has to be answered by its
// command_result before the absence is stamped.
//
// DERIVED FROM THE WARDEN'S OWN BUDGETS, not from a measured latency
// distribution — there is no end-to-end measurement of frame→receipt today
// (the warden log carries only warden-side clocks and the server fold logs
// nothing), and inventing one from a single host would be worse than naming the
// derivation. Worst honest round trip: claudeProbeBudget 20s + nudgeSettle 1s
// (spawn) or the ~5s kill ladder (stop), + commandReportTimeout 5s for the POST
// itself, + up to one lifecycleCadenceSecs 30s of sweep granularity ≈ 56s. 90s
// leaves a full extra tick of slack on top, so a stamp means the receipt is
// genuinely gone rather than merely slow. Erring long is the safe direction: a
// late stamp costs nothing, a premature one would cry wolf on a healthy fleet.
const receiptDeadlineSecs = 90.0

// pendingReceipt is one outstanding start/stop awaiting its command_result.
type pendingReceipt struct {
	RPC      string  // the dispatched verb (start / stop) — for the reason text
	Warden   string  // the machine the frame was handed to ("" when unresolved)
	Deadline float64 // absolute epoch secs after which the absence is stamped
}

// armReceiptWatch records that a start/stop frame LANDED on a warden's FIFO and
// a receipt is now owed. Callers must only arm after the enqueue was accepted:
// an unlanded frame is already explained by its own dispatch stamp, and arming
// there would blame the receipt channel for a refusal that never left the server.
//
// One slot per target: a re-dispatch replaces the previous watch rather than
// stacking, so the deadline always describes the LATEST outstanding op. That is
// the same single-slot posture last_op* itself has (known limitation, tracked on
// its own ticket) — worth being explicit about rather than pretending otherwise.
func (s *apiServer) armReceiptWatch(targetID, rpc, warden string, now float64) {
	if targetID == "" || rpc == "" {
		return
	}
	s.receiptMu.Lock()
	defer s.receiptMu.Unlock()
	s.receiptPending[targetID] = pendingReceipt{
		RPC:      rpc,
		Warden:   warden,
		Deadline: now + receiptDeadlineSecs,
	}
}

// memberIDRawOf pulls the member_id out of a raw command_result map — the same
// read foldCommandResult does, hoisted so the deadline disarm can run before the
// fold's routing without duplicating the type assertion.
func memberIDRawOf(commandResult map[string]any) string {
	id, _ := commandResult["member_id"].(string)
	return id
}

// noteReceiptArrived disarms the watch for a target whose command_result just
// arrived. Called for EVERY receipt the ingest folds — including the ones the
// fold deliberately declines to write (a no-op stop receipt, an unknown member):
// what disarms the deadline is the receipt CHANNEL working, which those prove
// just as well as a folded one. Tying it to a successful fold instead would
// stamp receipt_missing on members whose receipt arrived and was read.
//
// 🔴 reporter IS THE MACHINE THAT SPOKE (receiptReporterMachine), and comparing
// it is the whole point. The watch has recorded which machine it is waiting on
// since it was written — pendingReceipt.Warden, right there in the struct — and
// then matched on the TARGET ID ALONE, which is the id of the thing being
// stopped, not of the machine that owes the answer. An identity sweep
// broadcasts a stop to every warden in the fleet and every one of them answers,
// so ANY healthy machine's polite receipt cancelled the deadline that was
// waiting on a specific, possibly dark, one. The deadline asks "did THAT
// machine's report channel work"; a different machine's report is not an answer
// to it.
//
// Both "" cases stay permissive, deliberately:
//   - p.Warden == "": the dispatch could not resolve a machine, so there is
//     nobody to compare against and the watch never claimed to wait on anyone.
//   - reporter == "": UNKNOWN speaker (see receiptReporterMachine). No evidence
//     either way ⇒ keep the pre-existing behaviour rather than invent a
//     receipt_missing stamp out of an identity we failed to resolve.
//
// Only a KNOWN mismatch — both sides named, and different — declines to disarm.
func (s *apiServer) noteReceiptArrived(targetID, reporter string) {
	if targetID == "" {
		return
	}
	s.receiptMu.Lock()
	defer s.receiptMu.Unlock()
	p, armed := s.receiptPending[targetID]
	if !armed {
		return
	}
	if p.Warden != "" && reporter != "" && p.Warden != reporter {
		return // someone else's machine answered; the one we wait on still owes us
	}
	delete(s.receiptPending, targetID)
}

// takeLapsedReceipts removes and returns every watch whose deadline has passed.
// Removal is unconditional: the stamp is written once per dispatch, never
// re-stamped every tick (that would churn last_op_at and fan an SSE delta each
// 30s for a member nobody is touching). The next dispatch arms a fresh watch.
// Returned in a stable id order so the sweep is deterministic under test.
func (s *apiServer) takeLapsedReceipts(now float64) map[string]pendingReceipt {
	s.receiptMu.Lock()
	defer s.receiptMu.Unlock()
	var lapsed map[string]pendingReceipt
	for id, p := range s.receiptPending {
		if now < p.Deadline {
			continue
		}
		if lapsed == nil {
			lapsed = map[string]pendingReceipt{}
		}
		lapsed[id] = p
		delete(s.receiptPending, id)
	}
	return lapsed
}

// receiptMissingReason is the owner-facing sentence. It names the verb, the
// machine, and — the load-bearing half — the fact that the outcome is UNKNOWN
// rather than bad, so nobody reads it as "the stop failed" and re-fires blindly.
func receiptMissingReason(p pendingReceipt) string {
	where := "the target machine"
	if p.Warden != "" {
		where = fmt.Sprintf("machine %q", p.Warden)
	}
	return fmt.Sprintf(
		"%s: the %s was handed to %s but no receipt came back within %.0fs — "+
			"the op may or may not have run; this row's last state is UNKNOWN, not "+
			"failed. Suspect the machine's link to the server (the receipt POST) "+
			"before suspecting the op itself",
		receiptMissingReasonCode, p.RPC, where, receiptDeadlineSecs)
}

// sweepLapsedReceipts stamps every lapsed watch onto the row the cockpit reads.
// Routed exactly like foldCommandResult does: a roster member gets the member
// stamp, an outsource worker (whose receipts key on the SAME id since the P5b
// verb convergence) gets the worker stamp. A target that no longer exists is
// dropped — there is nothing left to explain.
//
// Best-effort by contract (the stampWakeObservability rule): a persistence
// failure is logged and never propagates — observability must not be able to
// stall the control loop.
func (s *apiServer) sweepLapsedReceipts(now float64) {
	lapsed := s.takeLapsedReceipts(now)
	if len(lapsed) == 0 {
		return
	}
	ids := make([]string, 0, len(lapsed))
	for id := range lapsed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s.stampReceiptMissing(id, lapsed[id], now)
	}
}

// stampReceiptMissing writes ONE lapsed watch onto its target row.
//
// The two arms below differ ONLY in which row they load, log and persist. The
// receipt itself — the five last_op* columns — was written out by hand twice in
// this one function, once per arm, and is now stampOpReceipt's (reconcile.go)
// on both. The op verb is p.RPC rather than a START: a lapsed watch names the
// call it was waiting on, which is why that core takes the verb as a parameter.
// Sentinels: TestStampReceiptMissing_MemberArmWritesTheFiveReceiptFields and
// TestStampReceiptMissing_WorkerArmWritesTheFiveReceiptFields, one per arm and
// both pinned to absolute values.
func (s *apiServer) stampReceiptMissing(targetID string, p pendingReceipt, now float64) {
	reason := receiptMissingReason(p)
	m, err := s.dal.GetMember(targetID)
	if err != nil {
		reconcileLog("%s: receipt-missing stamp read failed: %v", targetID, err)
		return
	}
	if m != nil && m.Kind != KindOutsource {
		if m.RosterStatus != RosterStatusActive {
			return
		}
		stampOpReceipt(&m.LastOp, &m.LastOpOK, &m.LastOpLog, &m.LastOpReason,
			&m.LastOpAt, p.RPC, reason, now)
		reconcileLog("%s: %s", targetID, reason)
		if err := s.putMember(*m, triggerServer); err != nil {
			reconcileLog("%s: receipt-missing stamp persist failed: %v", targetID, err)
		}
		return
	}
	w, err := s.dal.GetOutsourceWorker(targetID)
	if err != nil || w == nil || w.Status == WorkerStatusReleased {
		return
	}
	stampOpReceipt(&w.LastOp, &w.LastOpOK, &w.LastOpLog, &w.LastOpReason,
		&w.LastOpAt, p.RPC, reason, now)
	outsourceLog("%s: %s", targetID, reason)
	if err := s.dal.PutOutsourceWorker(*w); err != nil {
		outsourceLog("%s: receipt-missing stamp persist failed: %v", targetID, err)
		return
	}
	s.publishOutsourceWorker(*w, triggerServer)
}
