package main

import (
	"fmt"
	"sort"
)

// receipt_watch.go — the receipt deadline. An accepted start/stop is answered
// only by a command_result receipt POSTed to /telemetry and folded onto last_op*
// (api_monitoring.go foldCommandResult). The warden's POST is best-effort
// (cli/ocwarden/command.go), so a lost receipt leaves no trace anywhere, and the
// warden cannot report that its own report failed: only the server, which is
// owed the receipt, can observe its absence. The absence says neither that the
// op failed nor that it ran.

// receiptMissingReasonCode is a DISPATCH-level diagnosis, deliberately ABSENT
// from spawnBlockedReasonCodes: a later START must not erase the record that the
// previous one went unanswered.
const receiptMissingReasonCode = "receipt_missing"

// receiptDeadlineSecs is derived from the warden's own budgets, not measured:
// the spawn's login check spawnCheckBudget 15s (its wait for a running periodic
// check included) + the whole boot-nudge loop 30s (it always runs all
// nudgeMaxAttempts × nudgeSettle, and the START receipt is POSTed only after
// Spawn returns) + commandReportTimeout 5s + up to one 30s lifecycle cadence
// ≈ 80s. So 90 leaves only ~10 s of slack: a merely slow cold start can stamp
// receipt_missing with nothing wrong. 🔴 Those warden constants live in another
// Go module and nothing links them — raising nudgeMaxAttempts by six consumes
// the slack outright. Erring long is the safe direction.
const receiptDeadlineSecs = 90.0

type pendingReceipt struct {
	RPC      string
	Warden   string
	Deadline float64
}

// armReceiptWatch: arm only after the enqueue was accepted — an unlanded frame
// is already explained by its own dispatch stamp.
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

func memberIDRawOf(commandResult map[string]any) string {
	id, _ := commandResult["member_id"].(string)
	return id
}

// noteReceiptArrived must be called for EVERY receipt the ingest sees, including
// ones the fold declines to write: the deadline asks whether the receipt channel
// works, not whether the fold wrote.
//
// 🔴 reporter is the machine that spoke (receiptReporterMachine). An identity
// sweep broadcasts a stop to every warden and every one answers, so matching on
// the target id alone let any healthy machine cancel a deadline owed by a
// specific, possibly dark, one.
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
		return
	}
	delete(s.receiptPending, targetID)
}

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

func (s *apiServer) stampReceiptMissing(targetID string, p pendingReceipt, now float64) {
	reason := receiptMissingReason(p)
	m, err := s.dal.GetMember(targetID)
	if err != nil {
		reconcileLog("%s: receipt-missing stamp read failed: %v", targetID, err)
		return
	}
	if m != nil && m.Kind != KindOutsource {
		// Receipt columns only, stamped on the row as it is inside the transaction:
		// the delta that follows carries that row, not this earlier read.
		s.stampMemberReceiptOnRow(targetID, "receipt-missing stamp", func(fresh *Member) bool {
			stampOpReceipt(&fresh.LastOp, &fresh.LastOpOK, &fresh.LastOpLog, &fresh.LastOpReason,
				&fresh.LastOpAt, p.RPC, reason, now)
			reconcileLog("%s: %s", targetID, reason)
			return true
		})
		return
	}
	w, err := s.dal.GetOutsourceWorker(targetID)
	if err != nil || w == nil || w.Status == WorkerStatusReleased {
		return
	}
	stampOpReceipt(&w.LastOp, &w.LastOpOK, &w.LastOpLog, &w.LastOpReason,
		&w.LastOpAt, p.RPC, reason, now)
	outsourceLog("%s: %s", targetID, reason)
	// The one call site in the package that writes a worker row WITHOUT holding
	// s.outsourceMu (the sweep runs in the reconcile half) — keep it a narrow
	// five-column write.
	if err := s.dal.SetMemberLastOp(w.ID, w.LastOp, w.LastOpOK, w.LastOpLog,
		w.LastOpReason, w.LastOpAt); err != nil {
		outsourceLog("%s: receipt-missing stamp persist failed: %v", targetID, err)
		return
	}
	s.publishOutsourceWorker(*w, triggerServer)
}
