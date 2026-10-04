package main

import (
	"math"
	"strings"
)

// 「所有換手都可以給他機會收尾」 for STAFF members — the twin of the outsource
// rule (server/AGENTS.md 「所有 owner 動詞都給收尾機會」).
//
// THE DISCRIMINATOR IS ONE FIELD. The 〈停止〉 wake an agent prints is fanned by
// cli/ocagent's recycleHook.maybeRecycle, gated on `desired_state == online ∧
// refocus_since > 0`. A verb that does not stamp refocus_since is INVISIBLE to
// the agent.
//
// 改機器 / 換模型 funnel through here, as the outsource verbs funnel through
// respawnWorkerForOwnerOp: the caller writes its change, asks
// armMemberOwnerOpHandover whether there is anything to wind down, and persists
// the epoch. The value and the epoch land in SEPARATE writes (not atomic); each
// caller argues its own order at its call site. The 收口 is the agent's
// report_stopped or decideUp's recycle arm; the next tick's plain START re-mints
// the boot frame off the row, where the new machine / model now live.
//
// The active+online answer is an honest fallback, not a detection: the server
// cannot see an agent's transcript, so a finer test (context pct, uptime) would be
// a guess, and guessing wrong silently discards a round of close-out work.
//
// There is NO clock on this funnel: both staff owner-verbs are 停止, so the member
// keeps running on the OLD machine / model (cockpit shows 換手中) until it answers
// report_stopped or the owner force-stops — the trade the owner asked for.

const (
	memberOpRelocate     = "relocate"
	memberOpRuntimeModel = "runtime/model" // 換 model / runtime / effort
)

// Values are the closed set MemberDTO.refocus_op serves to the cockpit; stamped
// and cleared in lockstep with refocus_since.
const (
	refocusOpContextHigh = "context_high" // second context threshold — 加速停止
	// The FIRST context threshold (notice_pct): a plain 停止.
	refocusOpContextNotice = "context_notice"
	refocusOpRefocus       = "refocus"
	refocusOpRestartSelf   = "restart_self"
	// The agent token is about to expire. It must open the wind-down while the token
	// still works: every MCP call of the offboard sequence (report_stopping,
	// post_chat, report_stopped) uses the same bearer token, so an expired session
	// cannot file its hand-off.
	refocusOpTokenExpiry = "token_expiry"
	// The owner-pressed middle rung of 停止 → 加速停止 → 強制停止. A clock the owner
	// asked for, so it does not reopen the ruling that 下線 carries no 兜底
	// (rc-27d1710174dd): nothing fires unless he presses it.
	refocusOpAcceleratedStop = "accelerated_stop"
)

// The staff SHELL around the shared ownerOpHandoverPlanFor. 🔴 The kind guard has no
// worker analogue: a worker routed through this shell answers false, which closes
// its every wind-down — workers ask ownerOpHandoverPlanFor directly.
func (s *apiServer) memberHasStateToFlush(m Member) bool {
	return memberHasStateToFlushGiven(m, s.hub.IsOnline(m.ID))
}

// memberHasStateToFlushGiven takes the session's presence from the caller, read
// before any transaction it holds (the hub's lock is refused inside one).
func memberHasStateToFlushGiven(m Member, online bool) bool {
	// Not redundant with the handlers' staffOnly: that is a per-call-site choice.
	if m.Kind != KindStaff {
		return false
	}
	return ownerOpHandoverPlanFor(m, online) == ownerOpPlanWindDown
}

// Server half of a CROSS-LAYER contract (root AGENTS.md §9c): maybeRecycle in
// cli/ocagent/listen_hooks.go first checks `desired_state == online`. A refocus
// stamp on a desired-offline member reaches nobody and is stranded — activate does
// not clear it, so the next wake can be robust-stopped on an expired epoch. Every
// refocus stamp site must check this BY NAME: it once held only by a PresenceState
// correlation that vanished silently when those gates changed.
func aRefocusStampWouldReachTheAgent(m Member) bool {
	return m.DesiredState == DesiredStateOnline
}

func hasUncollectedOnlineOwnerOpState(refocusSince, stoppedSince float64, online bool) bool {
	return online && !(refocusSince > 0.0 && stoppedSince > 0.0)
}

// ownerOpHandoverPlan is what an owner verb that leaves the member running does
// with its session — one answer for staff and workers; only the row, the lock and
// the write differ per population.
type ownerOpHandoverPlan int

const (
	// Wanted online, live session, epoch not yet collected: open a graceful wind-down.
	ownerOpPlanWindDown ownerOpHandoverPlan = iota
	// Wanted offline and a stop was asked for: queue a 起來 behind it (rc-bc1b029a3aa2).
	ownerOpPlanQueueBehindStop
	// Wanted offline and never asked to stop: the change is saved, nothing starts.
	ownerOpPlanHeldDown
	// Wanted online with nothing to flush: no session, or this epoch is collected.
	ownerOpPlanNothingToFlush
)

// 🔴 Wanted-offline is decided BEFORE state-to-flush. A session can still be up
// while its stop is in flight; asking flush first would stamp a refocus epoch that
// reaches no agent and skip the queue behind the stop.
func ownerOpHandoverPlanFor(m Member, online bool) ownerOpHandoverPlan {
	if !aRefocusStampWouldReachTheAgent(m) {
		if aStopWasEverAskedFor(m) {
			return ownerOpPlanQueueBehindStop
		}
		return ownerOpPlanHeldDown
	}
	if hasUncollectedOnlineOwnerOpState(m.RefocusSince, m.StoppedSince, online) {
		return ownerOpPlanWindDown
	}
	return ownerOpPlanNothingToFlush
}

// ownerOpRow is the columns an owner verb writes, by pointer, for both populations
// (the stopVerbRow pattern). The same aliasing rule as windDownAnchorRow applies.
type ownerOpRow struct {
	Anchors          windDownAnchorRow
	RestartAfterStop *bool
	LastOp           *string
	LastOpOK         **bool
	LastOpLog        *string
	LastOpReason     *string
	LastOpAt         *float64
}

func ownerOpRowOfMember(m *Member) ownerOpRow {
	return ownerOpRow{
		Anchors:          windDownAnchorRowOfMember(m),
		RestartAfterStop: &m.RestartAfterStop,
		LastOp:           &m.LastOp,
		LastOpOK:         &m.LastOpOK,
		LastOpLog:        &m.LastOpLog,
		LastOpReason:     &m.LastOpReason,
		LastOpAt:         &m.LastOpAt,
	}
}

func ownerOpRowOfWorker(w *OutsourceWorker) ownerOpRow {
	return ownerOpRow{
		Anchors:          windDownAnchorRowOfWorker(w),
		RestartAfterStop: &w.RestartAfterStop,
		LastOp:           &w.LastOp,
		LastOpOK:         &w.LastOpOK,
		LastOpLog:        &w.LastOpLog,
		LastOpReason:     &w.LastOpReason,
		LastOpAt:         &w.LastOpAt,
	}
}

// queueRestartBehindStop stamps the queued 起來 and its note in memory; the
// caller persists both (the receipt columns land through SetMemberLastOp only).
// online is the caller's hub.IsOnline read: stopping_since outlives a finished
// stop, and a force-stop never writes stopped_since, so only the session says
// whether the stop is still running.
func queueRestartBehindStop(row ownerOpRow, op string, online bool, now float64) {
	*row.RestartAfterStop = true
	if online {
		stampOwnerOpNote(row, restartQueuedNote(op), now)
		return
	}
	stampOwnerOpNote(row, restartingNote(op), now)
}

// applyStoppedOwnerOp is what an owner verb that leaves the member running does to
// one wanted offline, staff and workers alike: queue a 起來 behind a stop that was
// asked for, or only note that the change waits for 喚醒. Answers false, writing
// nothing, when the member is wanted online.
func applyStoppedOwnerOp(row ownerOpRow, snapshot Member, op string, online bool, now float64) bool {
	switch ownerOpHandoverPlanFor(snapshot, online) {
	case ownerOpPlanQueueBehindStop:
		queueRestartBehindStop(row, op, online, now)
	case ownerOpPlanHeldDown:
		stampOwnerOpNote(row, heldDownNote(op), now)
	default:
		return false
	}
	return true
}

// ownerOpStartsAtOnce is whether an owner verb on a member whose stop has already
// finished starts it within the same request, staff and workers alike; otherwise
// the next tick spends the queued 起來.
// ⚠️ A model/runtime change must wait: the 喚醒 dialog saves the model and wakes
// right after, so a start here puts a second start and a kill on the warden; and
// the worker hand-off runs before the new values are stored, so a start from it
// boots the old model.
func ownerOpStartsAtOnce(op string) bool {
	return op != memberOpRuntimeModel
}

// reconcileAfterOwnerOp is the instant tick a staff owner verb runs after its
// write. On a member the verb only noted (wanted offline) that tick's one effect
// is spending the queued 起來, so ownerOpStartsAtOnce decides whether it runs.
func (s *apiServer) reconcileAfterOwnerOp(memberID, op string, noted bool) reconcileDecision {
	if noted && !ownerOpStartsAtOnce(op) {
		return reconcileDecision{}
	}
	return s.reconcileMemberNow(memberID)
}

// outsourceTickAfterOwnerOp is the worker face: the tick spends a queued 起來
// whose stop has finished. Callers must not hold outsourceMu (the tick takes it).
func (s *apiServer) outsourceTickAfterOwnerOp(op string) {
	if ownerOpStartsAtOnce(op) {
		s.outsourceTickNow()
	}
}

// Owner ruling: the three notes an owner verb leaves on a stopped member are ✓ with
// a note, never ✗ — the change was saved in every case. Do not stamp them through
// stampOpReceipt.
func stampOwnerOpNote(row ownerOpRow, reason string, now float64) {
	stampOpNoteReceipt(row.LastOp, row.LastOpOK, row.LastOpLog, row.LastOpReason, row.LastOpAt,
		reconcileCmdStart, reason, now)
}

func restartQueuedNote(op string) string {
	return ownerOpReasonRestartQueued + ": the " + op + " was saved; this member is still " +
		"being stopped, and it will be started again to apply it once that stop completes"
}

func restartingNote(op string) string {
	return ownerOpReasonRestarting + ": the " + op + " was saved; this member had already " +
		"stopped, so it is being started again to apply it"
}

func heldDownNote(op string) string {
	return spawnReasonHeldDown + ": the " + op + " was saved; this member stays stopped — " +
		"press 喚醒 when you want it to run"
}

const restartAfterStopSpentNote = ownerOpReasonRestarting + ": the stop has completed, so " +
	"this member is being started again to apply what was saved while it was stopping"

// noteRestartAfterStopSpent is the queued 起來's note at the moment it is spent. A
// restarting note already on the row was written for a stop that had finished
// before the verb, and says more than this one.
func noteRestartAfterStopSpent(row ownerOpRow, now float64) {
	if strings.HasPrefix(*row.LastOpReason, ownerOpReasonRestarting+":") {
		return
	}
	stampOwnerOpNote(row, restartAfterStopSpentNote, now)
}

// clearStoppedOwnerOpNote is 喚醒's half of the ruling: the note goes, and the next
// line on 最近操作 is the machine's own start result. Answers whether it cleared one.
func clearStoppedOwnerOpNote(row ownerOpRow) bool {
	if *row.LastOp != reconcileCmdStart || !isStoppedOwnerOpNote(*row.LastOpReason) {
		return false
	}
	*row.LastOp = ""
	*row.LastOpOK = nil
	*row.LastOpLog = ""
	*row.LastOpReason = ""
	*row.LastOpAt = 0.0
	return true
}

func isStoppedOwnerOpNote(reason string) bool {
	for _, code := range []string{ownerOpReasonRestartQueued, ownerOpReasonRestarting, spawnReasonHeldDown} {
		if strings.HasPrefix(reason, code+":") {
			return true
		}
	}
	return false
}

// armRefocusEpochOnRow decides the epoch on snapshot and writes only the four anchor
// columns through row.
func armRefocusEpochOnRow(row windDownAnchorRow, snapshot Member, op string, now float64) bool {
	if !armRefocusEpoch(&snapshot, op, now) {
		return false
	}
	*row.RefocusSince = snapshot.RefocusSince
	*row.RefocusOp = snapshot.RefocusOp
	*row.StoppingSince = snapshot.StoppingSince
	*row.StoppedSince = snapshot.StoppedSince
	return true
}

type refocusVerdict int

const (
	refocusArmed refocusVerdict = iota
	refocusRefusedWantedOffline
	refocusRefusedNoSession
	refocusRefusedLadder
)

// applyRefocusVerb is 重新聚焦 for both populations.
// ⚠️ Owner ruling: a member wanted offline (stopping or stopped) is refused, never
// queued behind its stop the way 改機器 / 換模型 are; 喚醒 is how it comes back.
func applyRefocusVerb(row ownerOpRow, snapshot Member, online bool, now float64) refocusVerdict {
	switch ownerOpHandoverPlanFor(snapshot, online) {
	case ownerOpPlanQueueBehindStop, ownerOpPlanHeldDown:
		return refocusRefusedWantedOffline
	}
	if !online {
		return refocusRefusedNoSession
	}
	// The ladder only goes forward (owner, 2026-08-24): a backward press would clear
	// a deadline an agent was told about. Refused, not silently downgraded.
	if !armRefocusEpochOnRow(row.Anchors, snapshot, refocusOpRefocus, now) {
		return refocusRefusedLadder
	}
	return refocusArmed
}

func refocusWantedOfflineRefusalMsg(noun string) string {
	return "refocus requires the " + noun + " to be online; this " + noun +
		" is stopping or stopped — press 喚醒 to bring it back"
}

func refocusLadderRefusalMsg(noun string) string {
	return "refocus is 停止 and this " + noun + " is already further along the " +
		"wind-down ladder (下線 → 加速 → 強制); a later stage is never " +
		"replaced by an earlier one"
}

// winddownKindFor is THE judgement about a wind-down cause: both the clock
// (recycleGraceFor) and the sentence (offboardKindOf) read it. FINAL is the
// positive condition and the default is SOFT (owner model, T-ed79): only the two
// causes below carry a deadline; everything else is collected by its own stopped
// report or the owner's force-stop.
func winddownKindFor(op string) (kind string, clocked bool) {
	// They share ONE grace (stop.accelerated_grace_secs, folded onto cfg.RecycleGrace)
	// because they are one verb with two triggers — 「統一在第二門檻跟加速停止使用」.
	if op == refocusOpContextHigh || op == refocusOpAcceleratedStop {
		return offboardKindFinal, true
	}
	return offboardKindSoft, false
}

// armRefocusEpoch mutates m and persists nothing. Its four columns are
// insert-only; they land through setMemberWindDownAnchorsOn.
//
// 🔴 A NEW epoch must never inherit the previous wind-down's stopped_since:
// decideUp's recycle arm reads stopped_since > 0 with a refocus marker present as
// "collected" and robust-stops the member ON THE SPOT, with no close-out.
func armRefocusEpoch(m *Member, op string, now float64) bool {
	if !winddownStageMayAdvanceTo(*m, op) {
		return false
	}
	m.RefocusSince = now
	m.RefocusOp = op
	m.StoppingSince = 0.0
	m.StoppedSince = 0.0
	return true
}

// Owner's ladder (2026-08-24): 「下線 → 加速 → 強制。後者一旦發出我們就不該發出前者」.
func winddownStageRankOf(op string) int {
	if kind, _ := winddownKindFor(op); kind == offboardKindFinal {
		return winddownStageAccelerated
	}
	return winddownStageStop
}

const (
	winddownStageNone        = 0
	winddownStageStop        = 1
	winddownStageAccelerated = 2
	winddownStageForced      = 3
)

// STOP-EPOCH-TERM-AUDIT: asks forcedEpochLive WITHOUT a stopping_since > 0 term
// on purpose — it reads which RUNG the member is on; gracefulStopEpochOpen asks a
// different question and is not its negation.
func winddownStageOf(m Member) int {
	if forcedEpochLive(m) {
		return winddownStageForced
	}
	if m.RefocusSince <= 0.0 {
		return winddownStageNone
	}
	return winddownStageRankOf(m.RefocusOp)
}

// 🔴 EQUAL RANK IS ALLOWED on purpose: re-stamping the same stage is a re-arm
// that several callers do deliberately; the owner's rule is only about a LOWER
// stage arriving after a higher one.
func winddownStageMayAdvanceTo(m Member, op string) bool {
	return winddownStageRankOf(op) >= winddownStageOf(m)
}

// The one caller is reconcileOne (memberObservation.HandoverArmable): the decideUp
// relocation backstop must know whether an epoch it asks for would be REFUSED,
// or it re-decides identically forever.
func (s *apiServer) memberOwnerOpHandoverArmable(m Member, op string) bool {
	probe := m
	return s.memberHasStateToFlush(m) && armRefocusEpoch(&probe, op, nowSecs())
}

// cfg is the caller's reconcileConfigLive(), read before any transaction it
// holds: that read takes settingsMu, which txguard refuses inside a write
// transaction (the request answers 500).
func (s *apiServer) armMemberOwnerOpHandover(m *Member, op string, cfg reconcileConfig, online bool) bool {
	if !memberHasStateToFlushGiven(*m, online) {
		return false
	}
	if !armRefocusEpoch(m, op, nowSecs()) {
		reconcileLog("recycle: %s %s — wind-down NOT re-opened: member is already "+
			"further along the ladder (下線 → 加速 → 強制)", op, m.ID)
		return false
	}
	if grace, clocked := recycleGraceFor(op, cfg); clocked {
		reconcileLog("recycle: %s %s — wind-down opened (collect on stopped-report or +%.0fs)",
			op, m.ID, grace)
	} else {
		reconcileLog("recycle: %s %s — wind-down opened (collect on stopped-report or force-stop; no clock)",
			op, m.ID)
	}
	return true
}

// applyMemberOwnerOpPlan is the staff shell of an owner verb that leaves the member
// running. It writes cur in memory only; noted says a receipt was stamped and
// must be persisted.
func (s *apiServer) applyMemberOwnerOpPlan(cur *Member, op string, cfg reconcileConfig, online bool) (windDown, noted bool) {
	if ownerOpHandoverPlanFor(*cur, online) == ownerOpPlanWindDown {
		return s.armMemberOwnerOpHandover(cur, op, cfg, online), false
	}
	return false, applyStoppedOwnerOp(ownerOpRowOfMember(cur), *cur, op, online, nowSecs())
}

// 「要不要起來」 is split out of desired_state (owner ruling rc-bc1b029a3aa2:
// 「一個重啟的 intention 遇上一個更強硬的下線規則 他的方式是沿用強硬下線規則 但是附加上線規則」).
// Two questions, two rules:
//
//	要不要起來  LAST WRITER WINS (後蓋前): every 下線 verb clears it, every 重啟
//	            verb sets it.
//	下線用多強  RATCHET (winddownStageMayAdvanceTo), untouched.
//
// 喚醒 remains the ONE thing that cancels a stop outright rather than queueing a
// start behind it — deliberately.

// 🔴 NOT "a stop is in flight": stopping_since stays > 0 forever after a converged
// stop (decideDown resets only the in-memory reconcileState; only 喚醒 and
// consumeRestartAfterStop clear the anchor). What it separates is a member ever
// ASKED to stop from one never activated: editing a new hire's machine or model
// must not boot it, while a long-stopped member IS brought back up
// (rc-bc1b029a3aa2).
//
// ⚠️ TestRelocateNeverStoppedWorker_SavesPinWithoutReviving is BLIND to this gate
// (its fixture never sets stopping_since). If it goes red, the gate stopped being
// consulted — not "the spec flipped".
func aStopWasEverAskedFor(m Member) bool {
	return m.StoppingSince > 0.0
}

func clearRestartIntent(m *Member) {
	m.RestartAfterStop = false
}

// 🔴 Waits for the SESSION TO BE GONE (the same hub.IsOnline authority as
// decideDown's offline arm), not a clock or a stopped-report, so a 強制停止
// whose kill is still in flight is not restarted underneath itself.
//
// 🔴 Twelve fields change here in one tick. A column made insert-only stops
// landing through the changed-columns write with no error (the member still
// comes up; only the stored row lags, so ordinary tests miss it): give it its
// own setter call below, as last_op* and the wind-down anchors have.
//
// forced_stop_at is deliberately NOT cleared: it records that the PREVIOUS session
// was cut off and is never cleared by a boot (migrations/00057).
//
// m is the tick's read. The spend is judged again on the row inside the
// transaction that writes it, and is applied to that row: an owner verb or a
// dismissal written since the tick read the member stands. On success *m is the
// row as written.
func (s *apiServer) consumeRestartAfterStop(m *Member, now float64) bool {
	if !m.RestartAfterStop || m.RosterStatus != RosterStatusActive {
		return false
	}
	if m.DesiredState != DesiredStateOffline || s.hub.IsOnline(m.ID) {
		return false
	}
	var spent *Member
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := getMemberOn(tx, m.ID)
		if err != nil || cur == nil {
			return err
		}
		if !cur.RestartAfterStop || cur.RosterStatus != RosterStatusActive ||
			cur.DesiredState != DesiredStateOffline {
			return nil
		}
		before := *cur
		cur.RestartAfterStop = false
		cur.DesiredState = DesiredStateOnline
		clearWindDownRow(windDownAnchorRowOfMember(cur))
		cur.WakingSince = 0.0
		noteRestartAfterStopSpent(ownerOpRowOfMember(cur), now)
		if err := persistMemberRowOn(tx, before, *cur); err != nil {
			return err
		}
		if err := persistMemberOpReceiptOn(tx, *cur); err != nil {
			return err
		}
		spent = cur
		return nil
	})
	if err != nil {
		reconcileLog("%s: queued restart-after-stop persist failed, nothing landed: %v", m.ID, err)
		return false
	}
	if spent == nil {
		return false
	}
	*m = *spent
	s.publishMemberPatch(*m, triggerServer)
	reconcileLog("%s: stop converged and a restart was queued behind it — desired_state "+
		"back to online", m.ID)
	return true
}

// Worker face of the same ruling (rc-bc1b029a3aa2). Worker functions rather than
// calls into the staff ones: a worker's one-task restart/release semantics and
// outsourceMu serialization are not shared.

func applyWorkerStoppedOwnerOp(w *OutsourceWorker, op string, online bool, now float64) bool {
	return applyStoppedOwnerOp(ownerOpRowOfWorker(w), memberFromWorker(*w), op, online, now)
}

// applyWorkerStoppedOwnerOpOnRow applies op on the worker's row as it is inside
// the transaction, not on a caller's copy: a release, a 喚醒 or any other column
// written since the caller read the worker stands. fresh is that row afterwards
// (nil when the worker is gone or released).
func (s *apiServer) applyWorkerStoppedOwnerOpOnRow(id, op string, online bool, now float64) (fresh *OutsourceWorker, applied bool, err error) {
	err = s.dal.inTx(func(tx *writeTx) error {
		cur, err := getOutsourceWorkerOn(tx, id)
		if err != nil || cur == nil || cur.Status == WorkerStatusReleased {
			return err
		}
		fresh = cur
		before := *cur
		if !applyWorkerStoppedOwnerOp(cur, op, online, now) {
			return nil
		}
		applied = true
		return persistWorkerRestartIntentOn(tx, before, *cur)
	})
	if err != nil {
		return nil, false, err
	}
	return fresh, applied, nil
}

// Two writers in one transaction: the flag rides the changed-columns write; the
// five last_op* columns land only through SetMemberLastOp.
func persistWorkerRestartIntentOn(tx *writeTx, before, after OutsourceWorker) error {
	if err := writeMemberChangesOn(tx, memberFromWorker(before), memberFromWorker(after)); err != nil {
		return err
	}
	return setMemberLastOpOn(tx, after.ID, after.LastOp, after.LastOpOK, after.LastOpLog,
		after.LastOpReason, after.LastOpAt)
}

func clearWorkerRestartIntent(w *OutsourceWorker) {
	w.RestartAfterStop = false
}

// 🔴 WHERE IT IS CALLED FROM IS NOT A DETAIL. The obvious home,
// reconcileWorkerLiveness, is UNREACHABLE for stopped workers (runOutsourceTick
// skips desired-offline workers before the FSM), so the call site is above both
// filters, in the tick's own loop. Same session-gone and forced_stop_at rules as
// consumeRestartAfterStop.
// Callers hold s.outsourceMu.
//
// w is the tick's list read. The spend is judged again on the row inside the
// transaction that writes it, and is applied to that row: an owner verb, a
// release or a landing written since the list read stands. On success *w is
// the row as written.
func (s *apiServer) consumeWorkerRestartAfterStop(w *OutsourceWorker, now float64) bool {
	if !w.RestartAfterStop || w.Status == WorkerStatusReleased {
		return false
	}
	if w.DesiredState != DesiredStateOffline || s.hub.IsOnline(w.ID) {
		return false
	}
	spent := false
	var cur *OutsourceWorker
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if cur, err = getOutsourceWorkerOn(tx, w.ID); err != nil || cur == nil {
			return err
		}
		if !cur.RestartAfterStop || cur.Status == WorkerStatusReleased ||
			cur.DesiredState != DesiredStateOffline {
			return nil
		}
		before := *cur
		cur.RestartAfterStop = false
		cur.DesiredState = DesiredStateOnline
		clearWindDownRow(windDownAnchorRowOfWorker(cur))
		cur.WakingSince = 0.0
		noteRestartAfterStopSpent(ownerOpRowOfWorker(cur), now)
		if err := persistWorkerRowOn(tx, before, *cur); err != nil {
			return err
		}
		if err := setMemberLastOpOn(tx, cur.ID, cur.LastOp, cur.LastOpOK, cur.LastOpLog,
			cur.LastOpReason, cur.LastOpAt); err != nil {
			return err
		}
		spent = true
		return nil
	})
	if err != nil {
		outsourceLog("%s: queued restart-after-stop persist failed, nothing landed: %v", w.ID, err)
		return false
	}
	if !spent {
		return false
	}
	*w = *cur
	s.publishOutsourceWorker(*w, triggerServer)
	return true
}

// collectWindDownRow is THE stopped_since latch of every close-out collect, for
// both populations (both report_stopped faces via decideStoppedReport,
// collectWorkerHandover, collectWorkerStop).
//
//   - `prior` is what collectWorkerHandover rolls the latch back to when the stop
//     finds no kill target. 🔴 IT MUST BE READ BEFORE THE STAMP: move the read
//     after it and the rollback silently "restores" the latch it should undo.
//   - 🔴 THE `<= 0` GUARD IS THE ONCE-ONLY: a stopped-report racing the grace
//     timeout can never double-collect (D4), and a repeat report never MOVES the
//     anchor.
//   - 🔴 IT TAKES NO LOCK AND MUST NEVER TAKE ONE: worker funnels run under
//     outsourceMu, the staff funnel under none, and the two mutexes are never held
//     at once (lifecycle_tick.go).
func collectWindDownRow(row windDownAnchorRow, now float64) (latched bool, prior float64) {
	prior = *row.StoppedSince
	if prior <= 0.0 {
		*row.StoppedSince = now
		return true, prior
	}
	return false, prior
}

// offlineConfirmGraceSecs: how long a stopped member or worker must be
// continuously offline before its session counts as gone. hub.IsOnline is an
// instantaneous sample and an ordinary reconnect blip used to kill a live session
// mid hand-off. 120 is an owner ruling (rc-dbee69264859). Floor ≈ 90: the agent's
// 45s idle-read watchdog (cli/ocagent/listen.go) + 15s backoff cap + one 30s tick.
// Do not derive it from WakingTTLSecs or reuse ZombieConfirmGrace.
const offlineConfirmGraceSecs = 120.0

// stopAwaitsCollect: a graceful 停止 epoch nobody has collected yet. A forced
// epoch is excluded because force-stop already sent its kill.
func stopAwaitsCollect(m Member) bool {
	return m.StoppedSince <= 0.0 && gracefulStopEpochOpen(m)
}

// sessionConfirmedGone is the one "the stopped session is gone" judgement for
// both populations. Both ticks call it once per tick for every desired-offline
// subject, before deciding whether to collect, so the anchor advances even on
// ticks that collect nothing. The anchor map takes no scheduler lock: the
// outsource tick calls this under outsourceMu, the member tick under reconcileMu.
func (s *apiServer) sessionConfirmedGone(memberID string, now float64) bool {
	if s.hub.IsOnline(memberID) {
		s.offlineConfirmSince.Delete(memberID)
		return false
	}
	since, armed := s.offlineConfirmSince.LoadOrStore(memberID, now)
	if !armed {
		return false
	}
	return now-since.(float64) >= offlineConfirmGraceSecs
}

// restartConfirmWindowOnStart: a START dispatch begins a new session, so an
// offline run anchored before it belongs to the previous one. Without this, a
// boot cancelled before it ever connects (onFirstConnect never fires) keeps the
// earlier stop's anchor, and the cancel's stop reads as already past the window
// and is collected on its first sample. Not cleared when a stop arms instead: a
// repeated 停止 press would restart a window the current session is already in.
func (s *apiServer) restartConfirmWindowOnStart(id string) {
	s.offlineConfirmSince.Delete(id)
}

// 🔴 外包 force-stop (api_outsource.go) deliberately does NOT use this: it adds a
// pull-back arm for a stamp in the future. Routing it here would break
// ForcedStopAt >= StoppingSince, which forcedEpochLive rests on; adding the arm
// here would change the three other sites (the parity whitelist's
// 強制停止|stopping_since row needs a deliberate decision).
func openWindDownRow(row windDownAnchorRow, now float64) {
	if *row.StoppingSince <= 0.0 {
		*row.StoppingSince = now
	}
}

// 🔴 ALL FOUR OR NONE: (refocus_since > 0 ∧ stopped_since > 0) reads as "this
// epoch is ALREADY collected", so a partial clear leaves a stale PAIR that shoots
// the next owner-op on the spot.
//
// ⚠️ waking_since and forced_stop_at must NOT be added: callers clear waking_since
// for their own reasons, and forced_stop_at is deliberately kept.
func clearWindDownRow(row windDownAnchorRow) {
	*row.StoppingSince = 0.0
	*row.StoppedSince = 0.0
	*row.RefocusSince = 0.0
	*row.RefocusOp = ""
}

// clearWindDownRowOnWake is report_waking's clear, for staff and workers alike.
// 🔴 stopping_since survives unless the subject is wanted online: it is the only
// trace of a stop that landed while the session was still booting.
// 🔴 A hand-off stamped after the waking session's credential was issued survives
// too: a late report_waking from the old session would otherwise erase the marker
// the agent's wake is gated on, and nobody would close the session out. iat is whole
// seconds, so the same second counts as before — a replacement session minted in
// the stamp's second must not be handed over again. This assumes the session runs on
// the credential minted at its dispatch; a long-lived /api/mint token would keep
// every later hand-off.
func clearWindDownRowOnWake(row windDownAnchorRow, desiredState string, sessionIat float64) {
	stoppingSince := *row.StoppingSince
	refocusSince, refocusOp := *row.RefocusSince, *row.RefocusOp
	clearWindDownRow(row)
	if desiredState != DesiredStateOnline {
		*row.StoppingSince = stoppingSince
	}
	if sessionIat > 0 && math.Floor(refocusSince) > sessionIat {
		*row.RefocusSince, *row.RefocusOp = refocusSince, refocusOp
	}
}
