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

// receiptDeadlineSecs is every command's deadline but start's.
const receiptDeadlineSecs = 90.0

// startReceiptDeadlineSecs: the START receipt is POSTed only after Spawn
// returns, and the spawn path can spend, in order (the warden's own budgets):
//   - the login check, spawnCheckBudget 15s (its wait for a running periodic
//     check included; cli/ocwarden/loginprobe.go);
//   - the interactive shell env capture, interactiveEnvTimeout 10s +
//     interactiveEnvWaitDelay 2s;
//   - Codex only, resolving a model family word: `codex --version` (5s) and the
//     model list, codexAppResponseTimeout 30s;
//   - Claude only, the prompt-file probe, claudePromptFileProbeBudget 2s +
//     subprocessWaitDelay 2s;
//   - Claude only, the version probe that picks the notification route,
//     claudeVersionProbeBudget 2s + subprocessWaitDelay 2s;
//   - Claude only, reaping leftover ocagent processes in the workdir, up to
//     (sweepTermPolls + sweepKillPolls) × sweepPollInterval = 25 × 200ms = 5s;
//   - Claude only, the boot-nudge loop, which always runs all nudgeMaxAttempts ×
//     nudgeSettle = 30s. On the notification-mod route the same 30s polls the
//     mod's markers instead (waitForNotifyMod), ending early once the mod loads;
//     no poll starts past the 30s, but the last one can run up to 17s over: a
//     nudgeSettle sleep 1s, a pane capture and the one-time /reload-plugins send
//     (copy-mode, two send-keys), each tmux call notifyModCaptureBudget 2s +
//     subprocessWaitDelay 2s. When the mod did not load, the warden's paste fallback
//     adds fallbackNudgeAttempts × nudgeSettle = 3s, and before that its capture of
//     the member pane for the warden log, notifyModCaptureBudget 2s +
//     subprocessWaitDelay 2s (cli/ocwarden/notifymod.go);
//   - Claude only, when the first launch never ran the mod, ONE restart before
//     that fallback (retryNotifyMod, cli/ocwarden/notifymod_retry.go): a capture
//     of the first pane (4s), its teardown — stop()'s ladder, a handful of tmux /
//     ps / lsof calls and up to 25 × 200ms = 5s of sweep — and a second wait of
//     the same 30s + 17s.
//
// After those, commandReportTimeout 5s, plus up to one 30s lifecycle cadence
// before the deadline is read.
//
// 150s because, measured on a station (warden log "received start frame" to
// "dispatched start OK"), a normal start takes 3s and a restart whose mod still
// does not load, with its paste fallback, 69s; by the budgets above a Claude
// start whose mod did not load is ≈ 129s at worst without the restart. A restart
// that then succeeds has not been measured. The restart path's own budget worst
// case, ≈ 185s, is PAST 150s, so a restart that is slow at every step can be
// stamped receipt_missing with nothing wrong: known, and left as is.
// 🔴 Those warden constants live in another Go module and nothing links them;
// raising any of them widens that gap. Erring long is the safe direction.
const startReceiptDeadlineSecs = 150.0

func receiptDeadlineFor(rpc string) float64 {
	if rpc == reconcileCmdStart {
		return startReceiptDeadlineSecs
	}
	return receiptDeadlineSecs
}

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
		Deadline: now + receiptDeadlineFor(rpc),
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
		receiptMissingReasonCode, p.RPC, where, receiptDeadlineFor(p.RPC))
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
