package main

// T-b6d9 — 「所有換手都可以給他機會收尾」 for STAFF members: the twin of the
// outsource-worker rule that landed as T-98f4 rule 2 (server/CLAUDE.md, section
// 「所有 owner 動詞都給收尾機會」).
//
// THE DISCRIMINATOR IS ONE FIELD. The 〈停止〉 wake an agent prints
// is fanned by cli/ocagent's recycleHook.maybeRecycle, and its gate is hard-
// wired to `desired_state == online ∧ refocus_since > 0` on the member row.
// A verb that does not stamp refocus_since is therefore INVISIBLE to the agent
// — there is no partial credit, no shorter warning, nothing. Before this
// ticket the staff verbs split three ways:
//
//	重啟   (refocus_member / restart_self) — stamps, dispatches NOTHING, and the
//	       reconcile recycle arm waits for report_stopped or the epoch's grace
//	       (recycleGraceFor — which since T-ed79 answers "no clock" for every
//	       cause except 加速停止, the second context threshold).
//	       FULL wind-down. UNCHANGED by this ticket (the sentinel).
//	改機器 (relocate)                       — stamped nothing; reconcileMemberNow
//	       took decideUp's relocate arm and dispatched a robust STOP ON THE
//	       SPOT. Zero warning, zero grace, not even a stopping_since, so the
//	       member simply vanished from the cockpit mid-thought.
//	換模型 (update_member runtime/model/effort) — stamped nothing and dispatched
//	       nothing: a clean 200 while the running session kept the OLD value
//	       until something else happened to respawn it.
//
// Both are now routed through this ONE funnel, exactly as the three outsource
// verbs funnel through respawnWorkerForOwnerOp. The caller writes its change
// onto the row, asks armMemberOwnerOpHandover whether there is anything to
// wind down, and persists the epoch — and the single member delta the agent
// wakes on still carries the new values, because the caller set them on the
// struct this write fans.
//
// ⚠️ THE VALUE AND THE EPOCH NO LONGER LAND IN THE SAME WRITE (T-55). The
// columns these verbs move — desired_machine_id, model, runtime, effort — left
// PutMember's SET list, so each one lands through its sole writer: the value in
// one write, the epoch in another. The two faces order that pair OPPOSITELY and
// both are deliberate. HandleUpdateMember writes the epoch FIRST, because its
// wind-down is gated on "the value actually changed" and a value landing early
// would shut that gate on the retry. HandleRelocateMember writes the pin FIRST,
// because its wind-down is unconditional and the retry converges either way.
// Each face argues its own order at the call site; this funnel does not decide
// it, and no longer promises the pair is atomic.
// The 收口 is the pre-existing §4.5 machinery:
// the agent's own report_stopped (→ dispatchRobustStopNow) or decideUp's
// recycle arm at recycleGraceFor(refocus_op). Either way the next tick's plain START re-mints
// the boot frame off the row, which is where the new machine / model now live.
//
// ── memberHasStateToFlush vs workerHasStateToFlush, cell by cell ─────────────
// 🔴 READ THIS FIRST, BECAUSE THE SENTENCE THAT USED TO OPEN IT IS NO LONGER
// TRUE OF THE CODE. It said the staff predicate was "COPIED, not re-derived"
// from the worker one. The COPY IS GONE: the arms that agree — a live session,
// and this epoch's wind-down not already collected — are ONE expression,
// hasUncollectedOnlineOwnerOpState (below), which both predicates call. Neither
// side re-derives anything.
//
// What is left on each side is a SHELL of guards that do NOT agree, and this
// table is the record of why they must not be flattened into the shared call.
// The reason the original sentence gave still applies to the shells: both HIGH
// findings in the worker predicate's review were the SAME boundary drawn wrong,
// so the danger this table guards against is somebody deriving that boundary a
// second time and drawing it differently — not somebody sharing the one that
// was already argued. Sharing the core is the opposite of re-deriving it.
// worker_spawn.go's comment block is still the authority for WHY each arm
// exists; this is the mapping of what differs:
//
//	worker: desired_state == offline → held_down, never winds down
//	  staff: SAME RULE, DIFFERENT PLACE, and that is the one asymmetry a reader
//	  has to hold. On the staff side it is inside the predicate, spelled
//	  aRefocusStampWouldReachTheAgent. On the worker side the predicate does not
//	  carry it at all: respawnWorkerForOwnerOp's FIRST gate answers held_down
//	  for a desired-offline worker and returns BEFORE workerHasStateToFlush is
//	  ever consulted, so a guard inside the predicate would be unreachable.
//	  It is load-bearing on both sides: an explicit 停止 dominates every other
//	  owner verb, and a refocus stamp on a desired-offline row is pure noise —
//	  decideUp is not even reached (decideDown owns it) and the agent's own gate
//	  re-checks desired_state == online, so nothing would ever read the marker.
//	  🔴 THE WORKER SIDE'S COMPENSATION IS PINNED, not assumed:
//	  TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt drives a desired-offline
//	  worker that is ONLINE — the state in which the shared core answers YES —
//	  through the 換 model face and requires a held_down receipt, no epoch, and
//	  zero frames. Without that test the compensation is decoration.
//
//	worker: !hub.IsOnline → immediate
//	  staff: SAME — and "same" is now literal: this arm lives in
//	  hasUncollectedOnlineOwnerOpState, which both shells call. hub.IsOnline is
//	  the exact same authority reconcileOne feeds into obs.Online, so "the
//	  recycle arm can fire" and "we opened a wind-down" can never disagree.
//
//	worker: Status != active (never claimed its task) → immediate
//	  staff: NO ANALOGUE, deliberately. assigned→active WAS the get_my_task
//	  claim, a lifecycle step a staff member simply does not have; there is no
//	  state in which a live staff session has PROVABLY never been handed work.
//	  Omitting it errs toward winding down — the safe direction. It used to be
//	  priced as "at most one grace CEILING"; since T-ed79 there is no ceiling on
//	  this funnel at all (below), so the price is different, not smaller.
//	  🔴 T-4595 RESOLVED THIS ASYMMETRY THE OTHER WAY: get_my_task is retired
//	  and the flip moved to report_waking, the FIRST boot verb — so "active" no
//	  longer proves a worker was ever handed task content, and the worker arm
//	  was DELETED rather than kept as a stale proof. Both predicates now agree,
//	  and they agree on THIS side of the argument: the safe direction. The cost
//	  is the one this paragraph already priced for staff — a wind-down window
//	  that ends when the session answers report_stopped (T-ed79: and NOT before,
//	  since neither staff owner-verb is on a clock).
//
//	worker: RefocusSince > 0 ∧ StoppedSince > 0 (this epoch already collected)
//	  staff: SHARED, not copied — the epoch scoping included — because the
//	  two-latch hazard it was written for exists here identically:
//	  HandleReportStoppedApiSelfStoppedPost latches StoppedSince on the FIRST
//	  stopped-report whether or not a handover is in flight, and only sets
//	  recycleKill when refocus_since > 0. Read GLOBALLY, an ordinary
//	  deactivate→report_stopped would leave a latch that claims "already
//	  collected" and shoot every later 改機器 / 換模型 on the spot — the
//	  STALE-LATCH HIGH, in staff clothing. Pairing it with RefocusSince > 0 asks the
//	  question actually meant (is THIS epoch's wind-down collected?), and the
//	  stale latch heals itself because arming the next epoch zeroes it.
//	  Dropping the StoppedSince half instead resurrects the 收口-window HIGH: a verb
//	  arriving inside the collect window would open a SECOND wind-down that
//	  dispatches nothing, while the in-flight respawn boots on the OLD value.
//
//	worker: ownerOpDisplacesTheSession deny-list (重啟 skips the wind-down)
//	  staff: N/A — 重啟 is not in this funnel. refocus_member / restart_self ARE
//	  the wind-down (they stamp and return), and activate is a wake, not a
//	  displacement. The staff funnel carries only 改機器 and 換模型, and both act
//	  on a session the owner wants to keep running.
//
// The active+online cell is an honest fallback, not a positive detection: the
// server has zero visibility into an agent's transcript, so any finer test
// (context pct, uptime, message counts) would be a guess dressed as a
// criterion, and guessing wrong silently discards a round of learnings.
// 🔴 THERE IS NO CEILING ON THIS FUNNEL ANY MORE (T-ed79). This used to read
// "the grace is a CEILING — the 收口 fires the instant the agent answers
// report_stopped", which priced the wait as "at most RecycleGrace". Both staff
// owner-verbs are 停止 now (winddownKindFor answers soft), so report_stopped is
// not merely the EARLY exit, it is the ONLY one apart from the owner's
// force-stop. A session with nothing to save still ends in seconds — that half
// is unchanged, and it is what makes the honest fallback affordable — but a
// session that never answers stays up until the owner presses the button.
//
// Cost, recorded honestly: after 改機器 / 換模型 the member keeps running on the
// OLD machine / OLD model until it answers, with the cockpit showing 換手中 for
// that whole time (refocus_since > 0 — the same projection 重新聚焦 already
// uses). That is the trade the owner asked for, and it is now unbounded on
// purpose rather than bounded by RecycleGrace.
//
// Agent-facing surface is unchanged (root CLAUDE.md §9c): same member-topic
// delta, same refetch, same 〈停止〉 wake out of the same recycleHook. No new
// tool, no new step ⇒ seeds/ needs no companion change.

// The owner verbs that funnel through armMemberOwnerOpHandover, named so the
// log tags cannot drift from their call sites.
const (
	memberOpRelocate = "relocate"      // 改機器
	memberOpModel    = "runtime/model" // 換 model / runtime / effort
)

// The remaining causes that stamp refocus_since WITHOUT going through
// armMemberOwnerOpHandover. Together with the two above these are the closed
// set MemberDTO.refocus_op serves: the cockpit needs the cause to say "winding
// down so your change can take effect" instead of "last refocus", which reads
// as history. They are stamped and cleared in lockstep with refocus_since —
// a cause outliving its window would be worse than none.
const (
	refocusOpContextHigh = "context_high" // second context threshold — 加速停止
	// refocusOpContextNotice is the FIRST context threshold (notice_pct): a
	// plain 停止, opened where the agent is only ASKED to wind down. It is named
	// after the setting it fires on so the two cannot drift apart in a reader's
	// head. Before T-ed79 the first threshold sent an SSE band and opened no
	// wind-down at all, so an agent that ignored one frame met the SECOND
	// threshold with no close-out started and 120 seconds to do it in.
	refocusOpContextNotice = "context_notice"
	refocusOpRefocus       = "refocus"      // owner pressed 重新聚焦
	refocusOpRestartSelf   = "restart_self" // the agent asked for its own handover
	// refocusOpTokenExpiry is the TOKEN-LIFETIME cause (T-ed79): the session's
	// agent token is about to expire, so the wind-down is opened while the token
	// still works. It is a plain 停止 — the agent is shown the sequence and
	// collected by its own stopped report or by the owner's force-stop — for the
	// same reason 重新聚焦 is: nothing here is an emergency the owner asked to be
	// cut short, and a countdown would only make the close-out worse.
	//
	// 🔴 WHY IT HAS TO EXIST AT ALL: an expired agent token does not degrade
	// gracefully. Every MCP call the offboard sequence makes — report_stopping,
	// post_chat, the lesson write, report_stopped — goes through the same bearer
	// token, so a session that reaches expiry mid-thought cannot file the
	// hand-off it is being asked for; it can only fail. Renewal used to depend
	// on the agent noticing on its own.
	refocusOpTokenExpiry = "token_expiry"
	// refocusOpAcceleratedStop is the OWNER-PRESSED 加速停止 (T-ed79, owner
	// 2026-08-21 「停止 → 加速停止 → 強制停止」). It is the middle rung of a
	// three-step escalation the owner walks by hand: 停止 asks and waits
	// forever, 加速停止 says "you now have until T", 強制停止 cuts the session
	// off with no sentence at all.
	//
	// 🔴 IT IS A CLOCK THE OWNER ASKED FOR, WHICH IS WHY IT DOES NOT REOPEN THE
	// RULING 下線 CARRIES NO 兜底 (rc-27d1710174dd 「不要兜底：只有你按強制下線
	// 才收它」). That ruling is about the SERVER deciding time is up on its own.
	// Nothing here fires unless the owner presses the button, so the escalation
	// is still his — this only gives his hand a rung between "wait indefinitely"
	// and "kill now", which is the rung he asked for.
	refocusOpAcceleratedStop = "accelerated_stop"
)

// memberHasStateToFlush answers the one question the rule turns on: is there
// anything for this member to wind down, or should the owner's verb take
// effect immediately?
//
// The ANSWER is hasUncollectedOnlineOwnerOpState, the one expression the worker
// twin calls too. What is written here is the staff SHELL — two guards that the
// worker side does not have in this function, each for a reason the cell-by-cell
// mapping above states. Do not flatten them into the shared call: the kind guard
// has no worker analogue at all, and the worker's desired-offline equivalent is
// its caller's first gate, which returns before this question is asked.
func (s *apiServer) memberHasStateToFlush(m Member) bool {
	// Staff only. A warden runs no ocagent and would never read the marker;
	// an outsource row has its own funnel (respawnWorkerForOwnerOp) and does not
	// reach these handlers, because the owner-op verbs that lead here each pass
	// staffOnly. 🔴 That upstream refusal is now a per-call-site CHOICE rather
	// than a property of the resolver, which is exactly why this guard is not
	// redundant: it refuses here rather than relying on every future caller
	// making the same choice.
	if m.Kind != KindStaff {
		return false
	}
	if !aRefocusStampWouldReachTheAgent(m) {
		return false
	}
	return hasUncollectedOnlineOwnerOpState(m.RefocusSince, m.StoppedSince, s.hub.IsOnline(m.ID))
}

// aRefocusStampWouldReachTheAgent is the server half of a CROSS-LAYER contract
// (root CLAUDE.md §9c; T-ccc7). The agent prints the 〈停止〉 wake
// from cli/ocagent/listen_hooks.go maybeRecycle, whose FIRST condition is
// `desired_state == online`. So stamping refocus_since on a member the server
// has already decided should be offline is not a weaker signal — it is NO
// signal: the agent returns early and prints nothing, while reconcile only
// reads RefocusSince on the decideUp arm a desired-offline member never takes.
// The stamp is then stranded: activate does not clear it, so the marker
// outlives the stop and the next wake can be robust-stopped on an epoch that
// expired while nobody was listening.
//
// Every site that stamps a member's refocus epoch must satisfy this, and all of
// them now say so BY NAME.
//
// 🔴 Two of them used not to. POST /members/{id}/refocus and POST /self/refocus
// were protected by a CORRELATION: they gated on PresenceState, which stops
// projecting online once StoppingSince > 0, and every path that sets desired
// offline sets that anchor in the same write. Adding the explicit check was
// measured to be dead code at the time (T-ccc7), and that measurement was
// true — of that code.
//
// It stopped being true the moment those gates had to change: an agent working
// its offboard sequence reports stopping FIRST (step 1), which made
// PresenceState project `stopping` and had both endpoints refusing the very
// caller the notice tells them to be (T-a9d6). Moving them onto the live-session
// fact removed the correlation, and with it — silently — the invariant that had
// been riding on it. The existing T-ccc7 tests caught that within one run, which
// is the whole reason this predicate is named rather than implied: a protection
// that holds by coincidence disappears the instant somebody replaces the
// coincidence, and nothing about the edit looks like it touched the invariant.
// The third site, the context-high auto-stamp in reconcile.go, had no proxy at
// all and stamped members on their way offline until T-ccc7.
func aRefocusStampWouldReachTheAgent(m Member) bool {
	return m.DesiredState == DesiredStateOnline
}

func hasUncollectedOnlineOwnerOpState(refocusSince, stoppedSince float64, online bool) bool {
	return online && !(refocusSince > 0.0 && stoppedSince > 0.0)
}

// winddownKindFor is THE judgement about a wind-down cause, and the only one.
// The clock (recycleGraceFor) and the sentence (offboardKindOf) both read it,
// so the two cannot disagree about the same member — a clock nobody announces,
// or a countdown nobody is counting, is now a change to ONE line rather than
// two files that happen to agree.
//
// 🔴 FINAL IS THE POSITIVE CONDITION, and the default is SOFT. It used to be
// the other way round — everything fell through to "on the clock" and 重新聚焦
// was carved out as the single exception — which meant every new cause arrived
// carrying a deadline by accident, including ones the owner had ruled must
// carry none. Owner model (T-ed79): the only 加速停止 is the one context
// pressure opens at the SECOND threshold; 停止 is everything else — the agent
// is shown the sequence and collected by its own stopped report or by the owner
// pressing force-stop. Adding a cause to the final set is now something you
// have to TYPE, on this line, where the ruling is written down.
//
// (force-stop is not a kind here at all: it sends nothing and removes the
// member on the spot — see HandleForceStopMember.)
func winddownKindFor(op string) (kind string, clocked bool) {
	// TWO causes are 加速停止, and they are the two the owner named: the one
	// context pressure opens at the SECOND threshold, and the one he presses
	// himself. They share ONE grace (stop.accelerated_grace_secs, folded onto
	// cfg.RecycleGrace by reconcileConfigLive) because they are the same verb
	// with two triggers — 「統一在第二門檻跟加速停止使用」 — so an owner tuning
	// the number can never end up with the automatic and the manual arm
	// counting different seconds.
	if op == refocusOpContextHigh || op == refocusOpAcceleratedStop {
		return offboardKindFinal, true
	}
	return offboardKindSoft, false
}

// armRefocusEpoch is the ONE way a refocus epoch is opened. It MUTATES m and
// persists nothing; the caller writes.
//
// ⚠️ AND THE EPOCH NO LONGER RIDES THE CALLER'S putMember — it is the part that
// stopped riding it. T-55's third batch moved the four columns this function
// writes out of PutMember's DO UPDATE SET, so the caller persists the epoch
// through persistMemberWindDownAnchors (BEFORE its row write, for the reason
// argued there) and the row write carries everything else. An earlier version of
// this paragraph said the epoch and the rest "land together"; that is now exactly
// backwards, and it is the third sentence in this ticket to go stale by restating
// which columns a write carries. The standing answer is singleColumnOwnedFields.
// Do not read anything here as a promise that a handler using this is atomic.
//
// 🔴 The two zeroed anchors are the whole reason this is a named function and
// not four hand-written lines. A NEW epoch must never inherit the PREVIOUS
// wind-down's latch, and the reader that latch feeds is destructive:
//
//   - decideUp's recycle arm reads AgentStopped = stopped_since > 0 and, with a
//     refocus marker present, robust-stops the member ON THE SPOT — zero grace,
//     no close-out. A stale stopped_since therefore turns the very next epoch
//     stamped on that member into an immediate kill, in the same tick, whatever
//     opened it.
//
// 🔴 A SECOND bullet used to stand here — "the SSE stop gate (api_infra.go)
// refuses a reconnect once stopped_since is set, so a stale latch also rejects
// the NEXT close-out's reconnect". It is FALSE inside this function's range and
// always was: that gate requires `desired_state == offline`, while every caller
// of armRefocusEpoch is behind aRefocusStampWouldReachTheAgent, which is
// `DesiredState == DesiredStateOnline`. The gate can never see one of these
// rows. The remaining bullet is real, and it is enough on its own — one true
// destructive reader justifies the shared function; a second, invented one only
// teaches the next reader a protection that will not be there when they rely on
// it.
//
// Three of the four stamp sites used to write refocus_since/refocus_op alone
// (POST /members/{id}/refocus, restart_self's staff arm, and the context
// auto-stamp); only the owner-verb funnel below cleared the anchors. Sharing
// one function is what makes "a fresh epoch starts from a clean sheet" a
// property of the operation rather than of whoever remembered it.
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

// winddownStageRankOf ranks ONE cause on the owner's three-step ladder
// (2026-08-24, verbatim): 「下線 → 加速 → 強制。後者一旦發出我們就不該發出前者」.
//
// 🔴 THE RANK IS DERIVED, NOT LISTED. It reads winddownKindFor — already THE
// judgement about a cause — rather than carrying a second table of causes
// beside it. A second list would be a second truth source: adding a cause to
// one and not the other produces a member whose stage the ladder and the clock
// disagree about, and nothing would report it. The ladder therefore has exactly
// as many causes as winddownKindFor does, forever, without anyone maintaining
// that.
//
// Stage 3 (強制停止) is deliberately NOT a cause: force-stop sends nothing and
// is a property of the MEMBER (forcedEpochLive), not of a refocus op — which is
// why the member-level reading below is a separate function.
func winddownStageRankOf(op string) int {
	if kind, _ := winddownKindFor(op); kind == offboardKindFinal {
		return winddownStageAccelerated
	}
	return winddownStageStop
}

const (
	// winddownStageNone is "no wind-down is open on this member" — below every
	// real stage, so the very first stamp of an epoch always advances.
	winddownStageNone        = 0
	winddownStageStop        = 1 // 停止
	winddownStageAccelerated = 2 // 加速停止
	winddownStageForced      = 3 // 強制停止
)

// winddownStageOf reads how far along the ladder this member ALREADY is.
//
// STOP-EPOCH-TERM-AUDIT: this asks forcedEpochLive WITHOUT a stopping_since > 0
// term, deliberately. It is reading which RUNG the member is on, and "forced" is
// the top rung; the graceful-epoch compound (gracefulStopEpochOpen) asks the
// opposite question — whether a session still has an epoch it can work — and
// negating this one would not answer it. Not a copy of that compound.
func winddownStageOf(m Member) int {
	if forcedEpochLive(m) {
		return winddownStageForced
	}
	if m.RefocusSince <= 0.0 {
		return winddownStageNone
	}
	return winddownStageRankOf(m.RefocusOp)
}

// winddownStageMayAdvanceTo answers the owner's rule: a stamp that would move
// this member BACKWARDS down the ladder is refused.
//
// 🔴 EQUAL RANK IS ALLOWED, and that is not an oversight. The rule he wrote is
// about a LOWER stage arriving after a higher one; re-stamping the same stage
// is a re-arm (a fresh epoch on a clean sheet), which several callers do on
// purpose and which takes nothing away from the agent. Refusing it would turn
// this guard into a behaviour change nobody asked for, on paths that are not
// what he was describing.
//
// What this actually stops: 重新聚焦 / restart_self / 換機器 / 換 model landing
// on a member that is already in 加速停止 — each of which used to succeed, push
// the stage back to 停止, AND clear the deadline with it, so an agent counting
// down silently stopped counting.
func winddownStageMayAdvanceTo(m Member, op string) bool {
	return winddownStageRankOf(op) >= winddownStageOf(m)
}

// memberOwnerOpHandoverArmable answers, WITHOUT mutating anything, the question
// armMemberOwnerOpHandover answers by doing: would a wind-down epoch for `op`
// actually be stamped on this member, or does one of the two gates refuse?
//
// 🔴 IT ASKS BY CALLING, NOT BY RE-LISTING. The gates are memberHasStateToFlush
// and armRefocusEpoch's ladder rule; writing either of them out again here
// would be a second copy of a ruling that already has one home, and the day the
// two disagree nothing would report it — the same disease reconcileDecision.
// StopKind's comment describes. So the probe runs the REAL pair against a
// throwaway copy of the row and reports what they said; the copy is discarded,
// which is why the `now` handed to armRefocusEpoch is irrelevant.
//
// The ONE caller is reconcileOne, building memberObservation.HandoverArmable:
// the decideUp relocation backstop has to know whether an epoch it asks for
// would be REFUSED, because "ask for an epoch nobody stamps" is a tick that
// changes nothing and re-decides identically forever. See the arm itself.
func (s *apiServer) memberOwnerOpHandoverArmable(m Member, op string) bool {
	probe := m
	return s.memberHasStateToFlush(m) && armRefocusEpoch(&probe, op, nowSecs())
}

// armMemberOwnerOpHandover stamps a FRESH refocus epoch on the member when
// there is state to flush, and reports whether it did. It MUTATES m and
// persists nothing: the caller folds the epoch into its own putMember.
//
// ⚠️ THE EPOCH IS ALL THAT WRITE CARRIES. This sentence used to say the owner's
// change and the epoch were "one atomic write and one delta"; T-55 made both
// halves false, in two steps. The first batch moved the launch intents out of
// PutMember's SET list, so the CHANGE stopped riding that write; the second
// moved the five receipt columns, so the RECEIPT stopped riding it too and the
// handler now fans two member deltas rather than one. Nothing here is atomic
// with anything: a caller performs two or three writes and argues their order at
// its own call site (HandleUpdateMember has the one that actually matters).
//
// The same correction was made to armRefocusEpoch above, to this file's header,
// and to both stampOpReceipt shells — this function was the one that got missed,
// and it is the one HandleUpdateMember actually calls.
//
// The stale wind-down anchors are cleared with the stamp — a new epoch never
// inherits an old latch, which is also what makes the "already collected" arm
// above self-healing.
func (s *apiServer) armMemberOwnerOpHandover(m *Member, op string) bool {
	if !s.memberHasStateToFlush(*m) {
		return false
	}
	// The ladder rule applies to the owner-verb funnel too: 換機器 / 換 model are
	// 停止, so neither may land on a member that is already in 加速停止 and hand
	// it back the slower procedure. Reporting false here is what makes the
	// caller fold nothing into its write — the owner's change still applies,
	// the wind-down stage simply does not move backwards with it.
	if !armRefocusEpoch(m, op, nowSecs()) {
		reconcileLog("recycle: %s %s — wind-down NOT re-opened: member is already "+
			"further along the ladder (下線 → 加速 → 強制)", op, m.ID)
		return false
	}
	if grace, clocked := recycleGraceFor(op, s.reconcileConfigLive()); clocked {
		reconcileLog("recycle: %s %s — wind-down opened (collect on stopped-report or +%.0fs)",
			op, m.ID, grace)
	} else {
		reconcileLog("recycle: %s %s — wind-down opened (collect on stopped-report or force-stop; no clock)",
			op, m.ID)
	}
	return true
}

// ── 「要不要起來」, split out of desired_state (T-14 項目 7) ──────────────────
//
// Owner 2026-08-30, rc-bc1b029a3aa2, verbatim: 「一個重啟的 intention 遇上一個
// 更強硬的下線規則 他的方式是沿用強硬下線規則 但是附加上線規則」, and
// 「我們強制下線以後已經不需要退回軟下線，如果我已經到強硬下線的狀態下按下
// refocus 我只需要在下線後把人帶起來」.
//
// 🔴 ONE COLUMN WAS ANSWERING TWO QUESTIONS. desired_state said both HOW HARD
// the member is being taken down and WHETHER it should be running afterwards,
// so a 重啟 verb arriving on a member already on its way down had exactly two
// spellings available, and both were wrong:
//
//	重新聚焦 → 409. aRefocusStampWouldReachTheAgent is right about what it
//	  actually says (a refocus STAMP on a desired-offline row reaches no agent),
//	  so the handler refused the whole verb — and the owner's "bring it back
//	  up" was refused with it.
//	改機器 / 換 model → a clean 200, the value stored, a held_down receipt, and
//	  nothing else. The owner's intent was recorded as a placement/launch value
//	  and discarded as an intent.
//
// The two questions now have two answers, and they follow DIFFERENT rules,
// which is why they could never share a column:
//
//	要不要起來  LAST WRITER WINS (後蓋前). Every 下線 verb clears it, every
//	            重啟 verb sets it. That is what makes 強制停止 → 重新聚焦 and
//	            重新聚焦 → 強制停止 different, which the owner named explicitly.
//	下線用多強  RATCHET (winddownStageMayAdvanceTo), untouched by this change.
//	            A 重啟 landing on a 加速停止 still does not hand the member back
//	            the slower procedure.
//
// 活化 remains the ONE thing that cancels a stop outright rather than queueing
// a start behind it — that exception is deliberate and predates this split.

// aStopWasEverAskedFor is the ONE thing this gate actually establishes, and the
// name says it because the previous name did not.
//
// 🔴 IT WAS CALLED aStopIsInFlight AND THAT WAS FALSE. It claimed to separate a
// member 收工中 from one 靜止, and to preserve T-ed79 #4/#14 (a 重啟 verb on a
// member the owner has stopped SAVES and starts nothing) for the second. It does
// not: decideDown's converged branch resets only the in-memory reconcileState —
// the DB anchor is cleared by 活化 and by consumeRestartAfterStop and by nothing
// else — so `stopping_since > 0` stays true forever on a member stopped last
// week. Measured: after five converged ticks the row still reads
// stopping_since=1.7883547e+09. TestRelocateMember_PlacementOnly kept passing
// only because its fixture (testAgent) never set the anchor at all.
//
// WHAT IT REALLY SEPARATES is a member that has been ASKED TO STOP at least once
// from one that never has — in practice, a freshly hired member that has never
// been 活化'd. That distinction is still worth drawing, and on its own terms:
// the owner's ruling adds 上線 to a 下線 rule, and a member nobody has ever asked
// to stop has no 下線 for it to be added to. Editing a new hire's machine or
// model before its first 活化 must not boot it.
//
// 🔴 WHAT IS NO LONGER TRUE, stated rather than hidden: 改機器 / 換 model / 重新
// 聚焦 on a member that was stopped and has long since converged DOES now bring
// it back up. T-ed79's staff held_down receipt (「活化 it when you want it to
// run」) survives only for the never-activated case. That is the owner's
// 2026-08-30 ruling applied where it actually lands — 「最後一個動作是重啟 ⇒ 最終
// 在線上」 says nothing about how long ago the 下線 was — and it is pinned by
// TestRelocateAfterAConvergedStopWakesTheMember below, whose fixture carries the
// REAL row shape a converged stop leaves behind.
// ⚠️ IT USED TO SAY 「IT COVERS THE STAFF MEMBER FACE ONLY, AND NOBODY HAS RULED
// ON THE OTHER ONE」, and both halves of that have since stopped being true.
// T-65 包② put the question to the owner and he answered it (rc-bc1b029a3aa2,
// 2026-08-30: 「一個重啟的 intention 遇上一個更強硬的下線規則 他的方式是沿用強硬
// 下線規則 但是附加上線規則」), so this gate now has SIX call sites: the three
// staff ones in api_members.go (換 model, 改機器, 重新聚焦) plus the three worker
// ones, which reach it through queueWorkerRestartAfterStop below — the worker
// refocus handler, the model handler's held-down branch (api_outsource.go), and
// respawnWorkerForOwnerOp's held-down arm (worker_spawn.go).
//
// 🔴 THE ONE CLAIM WORTH KEEPING FROM THE OLD TEXT, because it is a trap rather
// than a fact about the past: TestRelocateNeverStoppedWorker_SavesPinWithoutReviving
// is BLIND to all of that. Its fixture reaches desired_state=offline by writing
// the field directly, so it never acquires a stopping_since anchor, so this gate
// is FALSE for it and it stays green both before and after the change. It is not
// a signal in either direction. If it ever goes red, the reading is NOT 「the
// spec flipped」 — it means this gate stopped being consulted and workers nobody
// ever asked to stop are being booted by an edit. The anchored fixture lives in
// outsource_restart_after_stop_t65_test.go (seedStoppedAnchoredWorker).
func aStopWasEverAskedFor(m Member) bool {
	return m.StoppingSince > 0.0
}

// stampRestartIntent records 「下線之後把人帶起來」 on a member whose stop is
// already in flight. It writes ONLY the second intent: the stop, its stage, and
// its anchors are all left exactly as the 下線 verb set them, which is the
// owner's 「沿用強硬下線規則 但是附加上線規則」 in one line.
func stampRestartIntent(m *Member) {
	m.RestartAfterStop = true
}

// clearRestartIntent is the other half of 後蓋前, and it is the half that makes
// the negative control hold: 重新聚焦 → 強制停止 must still end with the member
// DOWN. Every 下線 verb calls it, so the last thing the owner pressed is the
// one that decides.
func clearRestartIntent(m *Member) {
	m.RestartAfterStop = false
}

// memberRestartQueuedReceipt is what the owner reads on the row after a 重啟
// verb landed on a member that was already going down. It replaces
// memberHeldDownReceipt for exactly this case — that sentence says 「活化 it
// when you want it to run」, which is now false: nothing more is needed from him.
func memberRestartQueuedReceipt(op string) string {
	return spawnReasonHeldDown + ": the " + op + " was saved and this member is " +
		"still being stopped — the stop in flight is honoured as-is, and it will " +
		"be started again once it is down"
}

// consumeRestartAfterStop is the ONE place the queued start is spent: the
// converged-offline edge of the reconcile tick, which is the first instant the
// stop the owner asked for is actually finished.
//
// 🔴 IT WAITS FOR THE SESSION TO BE GONE, not for a clock and not for a
// stopped-report. `!s.hub.IsOnline` is the same authority decideDown's first
// branch ("offline: converged") uses, so "the stop landed" and "the restart
// fires" can never disagree about the same member — and a 強制停止 whose kill is
// still in flight is not restarted underneath itself.
//
// 🔴 WHOLE-ROW DEPENDENT — T-55 TRIPWIRE. This function is the tree's heaviest
// single-write site: TWELVE fields land in one tick (seven assigned here —
// restart_after_stop, desired_state, stopping_since, stopped_since,
// waking_since, refocus_since, refocus_op — plus the five last_op* columns
// stampMemberOpReceipt writes). It relied on the whole-row writer to carry all
// twelve, so EVERY T-55 batch that marks a column insertOnly silently amputates
// a field here: the member is still brought up correctly and only the row's
// stored state is a batch behind, which is why no ordinary test notices. 批次B
// already did it to the five receipt columns and 批次C to the four wind-down
// anchors — both are repaired below through their sole writers; 批次D
// (desired_state + restart_after_stop) and 批次E (waking_since) will do it again.
//
// THE TRIPWIRE, so nobody has to read this comment first:
// TestConsumeRestartAfterStopPersistsEveryFieldItMutates
// (member_restart_after_stop_t14_test.go) runs this function against a real DAL,
// reads the row back, and requires the stored row to equal the member this
// function mutated — field by field, over reflect, enumerating nothing. Mark any
// column insertOnly without giving this site a writer for it and that test goes
// red NAMING THE FIELD.
//
// The anchors it clears are 活化's list minus forced_stop_at: that column is the
// durable record that the PREVIOUS session was cut off and is deliberately never
// cleared by a boot (migrations/00057). Clearing stopping_since is what closes
// the forced epoch, exactly as 活化 does.
func (s *apiServer) consumeRestartAfterStop(m *Member, now float64) bool {
	if !m.RestartAfterStop || m.RosterStatus != RosterStatusActive {
		return false
	}
	if m.DesiredState != DesiredStateOffline || s.hub.IsOnline(m.ID) {
		return false
	}
	m.RestartAfterStop = false
	m.DesiredState = DesiredStateOnline
	m.StoppingSince = 0.0
	m.StoppedSince = 0.0
	m.WakingSince = 0.0
	m.RefocusSince = 0.0
	m.RefocusOp = ""
	stampMemberOpReceipt(m, spawnReasonHeldDown+": the stop the owner asked for has "+
		"landed — starting this member again, which is what the 重啟 he pressed "+
		"during the wind-down asked for", now)
	// The four wind-down anchors left the whole-row writer in T-55 批次C
	// (insertOnly), so the four clears above are IN MEMORY ONLY until this lands.
	// BEFORE the row write, for the reason persistMemberWindDownAnchors spells
	// out: putMember fans the delta the agent's wind-down hook reads, and these
	// are the columns it reads.
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		reconcileLog("%s: queued restart-after-stop anchor persist failed: %v", m.ID, err)
		return false
	}
	if err := s.putMember(*m, triggerServer); err != nil {
		reconcileLog("%s: queued restart-after-stop persist failed: %v", m.ID, err)
		return false
	}
	// The five last_op* columns left the whole-row writer in T-55 批次B, so the
	// stamp above is IN MEMORY ONLY until this lands. Ordered after the row write
	// for the reason every other site orders it that way: the receipt explains a
	// change that is already stored, never one that failed. A failure here is not
	// fatal — the member IS up, and refusing to return true would re-arm an
	// intent that has already been spent — so it is logged and the tick moves on
	// with a stale explanation on the row.
	if err := s.persistMemberOpReceipt(*m, triggerServer); err != nil {
		reconcileLog("%s: restart-after-stop receipt persist failed: %v", m.ID, err)
	}
	reconcileLog("%s: stop converged and a 重啟 was queued behind it — desired_state "+
		"back to online", m.ID)
	return true
}

// ── the OUTSOURCE face of the same ruling (T-65 包②) ─────────────────────────
//
// 🔴 THE ASYMMETRY THE COMMENT ON aStopWasEverAskedFor NAMED IS CLOSED HERE, and
// it is closed by owner ruling rather than by symmetry-for-its-own-sake. That
// comment said the worker face 「keeps its own StoppingSince ladder and has no
// queued-「起來」 at all」 and that whether it should follow 「was never put to
// him」. It was put to him: rc-bc1b029a3aa2, 2026-08-30 — 「一個重啟的 intention
// 遇上一個更強硬的下線規則 他的方式是沿用強硬下線規則 但是附加上線規則」.
//
// TWO RULES, KEPT SEPARATE, exactly as the staff face keeps them:
//
//	下線用多強  RATCHET. Untouched here. A queued 起來 never rolls a 加速停止
//	            back to a 停止, and the three worker 下線 verbs keep their own
//	            ladder (armRefocusEpoch / stopEpochAnchor) unchanged.
//	要不要起來  LAST WRITER WINS. Every 下線 verb clears it, every 重啟 verb
//	            (重新聚焦 / 改機器 / 換 model) sets it.
//
// WHY THESE ARE WORKER FUNCTIONS RATHER THAN CALLS INTO THE STAFF ONES. The
// projection would allow it — an ow- row IS a member row — but the PERSISTENCE
// would not. putMember fans a MEMBER delta, and persistMemberOpReceipt's own
// header states the rule this obeys: an ow- id's changes travel on the
// outsource_worker projection, so worker callers write the receipt through
// s.dal.SetMemberOpReceipt and publish through publishOutsourceWorker. Routing a
// worker through consumeRestartAfterStop would fan the wrong topic at the wrong
// audience and re-open the question of whether putMember is safe under
// outsourceMu (measured: it is — api_members.go touches that mutex nowhere — but
// that is a property nothing enforces, and this way nothing has to).

// queueWorkerRestartAfterStop is the WORKER half of stampRestartIntent, gate
// included: it records 「這一輪下線收口之後把它帶起來」 on a stopped worker and
// reports whether it did.
//
// The gate is aStopWasEverAskedFor on the member projection — the SAME predicate,
// not a copy of it — for the reason that predicate's own comment gives: a worker
// nobody has ever asked to stop has no 下線 for an 上線 rule to be added to, and
// editing its machine or model before its first start must not boot it.
//
// It mutates ONLY the flag and the receipt. The stop, its stage and its four
// anchors are left exactly as the 下線 verb wrote them — 「沿用強硬下線規則」 in
// one function.
func (s *apiServer) queueWorkerRestartAfterStop(w *OutsourceWorker, op string, now float64) bool {
	if w.DesiredState != DesiredStateOffline || !aStopWasEverAskedFor(memberFromWorker(*w)) {
		return false
	}
	w.RestartAfterStop = true
	stampOpReceipt(&w.LastOp, &w.LastOpOK, &w.LastOpLog, &w.LastOpReason, &w.LastOpAt,
		reconcileCmdStart, memberRestartQueuedReceipt(op), now)
	return true
}

// persistWorkerRestartIntent stores BOTH things queueWorkerRestartAfterStop
// mutated, because they land through two different writers and forgetting either
// one fails silently in a different way.
//
//   - the flag rides the whole-row write (mfRestartAfterStop is not insertOnly);
//   - the five last_op* columns left that write in T-55 批次B and land through
//     SetMemberOpReceipt.
//
// Order: the row first, the sentence second — every other site in this package
// orders it that way, and for the same reason: a receipt explains a change that
// is already stored, never one that failed.
func (s *apiServer) persistWorkerRestartIntent(w OutsourceWorker) error {
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		return err
	}
	return s.dal.SetMemberOpReceipt(w.ID, w.LastOp, w.LastOpOK, w.LastOpLog,
		w.LastOpReason, w.LastOpAt)
}

// clearWorkerRestartIntent is the 後蓋前 half that makes the negative control
// hold: 重新聚焦 → 強制停止 must still end with the worker DOWN. Every worker
// 下線 verb calls it, so the last button the owner pressed is the one that
// decides. It writes nothing on its own — the anchors those handlers already
// persist ride the same PutOutsourceWorker this flag does.
func clearWorkerRestartIntent(w *OutsourceWorker) {
	w.RestartAfterStop = false
}

// consumeWorkerRestartAfterStop is the worker twin of consumeRestartAfterStop:
// the ONE place a queued 起來 is spent, at the converged-offline edge.
//
// 🔴 WHERE IT IS CALLED FROM IS NOT A DETAIL. Its staff twin lives in
// reconcileOne, so the obvious worker home is reconcileWorkerLiveness — and that
// home is UNREACHABLE for the entire population this function exists for.
// runOutsourceTick's per-worker switch `continue`s on `DesiredState == offline`
// in the assigned branch and gates the active branch on
// `fresh.DesiredState != offline`; a stopped worker never reaches the FSM at
// all. That is the same shape as the bug being fixed one layer up — an intent
// recorded on a row nothing ever looks at again — so the call site is ABOVE both
// filters, in the tick's own loop.
//
// It waits for the SESSION TO BE GONE, not for a clock and not for a
// stopped-report, which is its twin's rule verbatim: a 強制停止 whose kill is
// still in flight must not be restarted underneath itself.
//
// The anchors it clears are the restart handler's list plus waking_since, which
// is its twin's list minus forced_stop_at — that column is the durable record
// that the PREVIOUS session was cut off and is deliberately never cleared by a
// boot (migrations/00057).
// Callers hold s.outsourceMu.
func (s *apiServer) consumeWorkerRestartAfterStop(w *OutsourceWorker, now float64) bool {
	if !w.RestartAfterStop || w.Status == WorkerStatusReleased {
		return false
	}
	if w.DesiredState != DesiredStateOffline || s.hub.IsOnline(w.ID) {
		return false
	}
	w.RestartAfterStop = false
	w.DesiredState = DesiredStateOnline
	w.StoppingSince = 0.0
	w.StoppedSince = 0.0
	w.WakingSince = 0.0
	w.RefocusSince = 0.0
	w.RefocusOp = ""
	stampOpReceipt(&w.LastOp, &w.LastOpOK, &w.LastOpLog, &w.LastOpReason, &w.LastOpAt,
		reconcileCmdStart, spawnReasonHeldDown+": the stop the owner asked for has "+
			"landed — starting this worker again, which is what the 重啟 he pressed "+
			"during the wind-down asked for", now)
	// BEFORE the row write, for the reason persistMemberWindDownAnchors spells
	// out: the four anchors left the whole-row writer in T-55 批次C, so the four
	// clears above are IN MEMORY ONLY until this lands.
	if err := s.persistWorkerWindDownAnchors(*w); err != nil {
		outsourceLog("%s: queued restart-after-stop anchor persist failed: %v", w.ID, err)
		return false
	}
	if err := s.dal.PutOutsourceWorker(*w); err != nil {
		outsourceLog("%s: queued restart-after-stop persist failed: %v", w.ID, err)
		return false
	}
	// Not fatal: the worker IS up, and refusing to return true would re-arm an
	// intent that has already been spent. Logged, and the tick moves on with a
	// stale explanation on the row — the same trade its staff twin makes.
	if err := s.dal.SetMemberOpReceipt(w.ID, w.LastOp, w.LastOpOK, w.LastOpLog,
		w.LastOpReason, w.LastOpAt); err != nil {
		outsourceLog("%s: queued restart-after-stop receipt persist failed: %v", w.ID, err)
	}
	s.publishOutsourceWorker(*w, triggerServer)
	return true
}
