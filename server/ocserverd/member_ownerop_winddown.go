package main

// T-b6d9 — 「所有換手都可以給他機會收尾」 for STAFF members: the twin of the
// outsource-worker rule that landed as T-98f4 rule 2 (server/CLAUDE.md, section
// 「所有 owner 動詞都給收尾機會」).
//
// THE DISCRIMINATOR IS ONE FIELD. The 下線程序 wake an agent prints
// is fanned by cli/ocagent's recycleHook.maybeRecycle, and its gate is hard-
// wired to `desired_state == online ∧ refocus_since > 0` on the member row.
// A verb that does not stamp refocus_since is therefore INVISIBLE to the agent
// — there is no partial credit, no shorter warning, nothing. Before this
// ticket the staff verbs split three ways:
//
//	重啟   (refocus_member / restart_self) — stamps, dispatches NOTHING, and the
//	       reconcile recycle arm waits for report_stopped or the epoch's grace
//	       (recycleGraceFor: 120s — except 重新聚焦, which runs no clock at all).
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
// wind down, and persists ONCE — so the new pin / model and the refocus epoch
// land in the same write, and the single member delta the agent wakes on
// already carries the new values. The 收口 is the pre-existing §4.5 machinery:
// the agent's own report_stopped (→ dispatchRobustStopNow) or decideUp's
// recycle arm at recycleGraceFor(refocus_op). Either way the next tick's plain START re-mints
// the boot frame off the row, which is where the new machine / model now live.
//
// ── memberHasStateToFlush vs workerHasStateToFlush, cell by cell ─────────────
// The worker predicate was reviewed twice and each round found a HIGH in it, so
// it is COPIED, not re-derived. Its comment block (worker_spawn.go) is the
// authority for WHY; this is the mapping:
//
//	worker: desired_state == offline → held_down, never winds down
//	  staff: SAME, and it is load-bearing here too. An explicit 停止 dominates
//	  every other owner verb, and a refocus stamp on a desired-offline member is
//	  pure noise: decideUp is not even reached (decideDown owns it) and the
//	  agent's own gate re-checks desired_state == online, so nothing would ever
//	  read the marker.
//
//	worker: !hub.IsOnline → immediate
//	  staff: SAME predicate, and hub.IsOnline is the exact same authority
//	  reconcileOne feeds into obs.Online — so "the recycle arm can fire" and
//	  "we opened a wind-down" can never disagree.
//
//	worker: Status != active (never claimed its task) → immediate
//	  staff: NO ANALOGUE, deliberately. assigned→active WAS the get_my_task
//	  claim, a lifecycle step a staff member simply does not have; there is no
//	  state in which a live staff session has PROVABLY never been handed work.
//	  Omitting it errs toward winding down, i.e. toward the grace — the safe
//	  direction, and the wait is a CEILING not a duration (below).
//	  🔴 T-4595 RESOLVED THIS ASYMMETRY THE OTHER WAY: get_my_task is retired
//	  and the flip moved to report_waking, the FIRST boot verb — so "active" no
//	  longer proves a worker was ever handed task content, and the worker arm
//	  was DELETED rather than kept as a stale proof. Both predicates now agree,
//	  and they agree on THIS side of the argument: the safe direction. The cost
//	  is the one this paragraph already priced for staff — at most one grace
//	  ceiling, cut short the instant the session answers report_stopped.
//
//	worker: RefocusSince > 0 ∧ StoppedSince > 0 (this epoch already collected)
//	  staff: COPIED VERBATIM, including the epoch scoping, because the two-latch
//	  hazard it was written for exists here identically:
//	  HandleReportStoppedApiSelfStoppedPost latches StoppedSince on the FIRST
//	  stopped-report whether or not a handover is in flight, and only sets
//	  recycleKill when refocus_since > 0. Read GLOBALLY, an ordinary
//	  deactivate→report_stopped would leave a latch that claims "already
//	  collected" and shoot every later 改機器 / 換模型 on the spot — the round-2
//	  HIGH, in staff clothing. Pairing it with RefocusSince > 0 asks the
//	  question actually meant (is THIS epoch's wind-down collected?), and the
//	  stale latch heals itself because arming the next epoch zeroes it.
//	  Dropping the StoppedSince half instead resurrects the round-1 HIGH: a verb
//	  arriving inside the collect window would open a SECOND wind-down that
//	  dispatches nothing, while the in-flight respawn boots on the OLD value.
//
//	worker: ownerOpRevivesStoppedWorker deny-list (重啟 skips the wind-down)
//	  staff: N/A — 重啟 is not in this funnel. refocus_member / restart_self ARE
//	  the wind-down (they stamp and return), and activate is a wake, not a
//	  displacement. The staff funnel carries only 改機器 and 換模型, and both act
//	  on a session the owner wants to keep running.
//
// The active+online cell is an honest fallback, not a positive detection: the
// server has zero visibility into an agent's transcript, so any finer test
// (context pct, uptime, message counts) would be a guess dressed as a
// criterion, and guessing wrong silently discards a round of learnings.
// The grace is a CEILING — the 收口 fires the instant the agent answers
// report_stopped, so a session with nothing to save ends in seconds.
//
// Cost, recorded honestly: after 改機器 / 換模型 the member lives at most one
// grace window longer on the OLD machine / OLD model, and the cockpit shows
// 換手中 for that window (refocus_since > 0 — the same projection 重新聚焦
// already uses). That is the trade the owner asked for.
//
// Agent-facing surface is unchanged (root CLAUDE.md §9c): same member-topic
// delta, same refetch, same 下線程序 wake out of the same recycleHook. No new
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
	refocusOpContextHigh = "context_high" // reconcile's context-pressure handover
	refocusOpRefocus     = "refocus"      // owner pressed 重新聚焦
	refocusOpRestartSelf = "restart_self" // the agent asked for its own handover
)

// memberHasStateToFlush answers the one question the rule turns on: is there
// anything for this member to wind down, or should the owner's verb take
// effect immediately? See the cell-by-cell mapping above — this is
// workerHasStateToFlush with the staff substitutions, nothing else.
func (s *apiServer) memberHasStateToFlush(m Member) bool {
	// Staff only. A warden runs no ocagent and would never read the marker;
	// an outsource row has its own funnel (respawnWorkerForOwnerOp) and never
	// reaches these handlers anyway (resolveMember folds kind=outsource onto
	// errNotFound). Both are refused here rather than relied upon upstream.
	if m.Kind != KindAssistant {
		return false
	}
	if !aRefocusStampWouldReachTheAgent(m) {
		return false
	}
	return hasUncollectedOnlineOwnerOpState(m.RefocusSince, m.StoppedSince, s.hub.IsOnline(m.ID))
}

// aRefocusStampWouldReachTheAgent is the server half of a CROSS-LAYER contract
// (root CLAUDE.md §9c; T-ccc7). The agent prints the 下線程序 wake
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

// armMemberOwnerOpHandover stamps a FRESH refocus epoch on the member when
// there is state to flush, and reports whether it did. It MUTATES m and
// persists nothing: the caller folds this into its own single putMember so the
// owner's change and the epoch are one atomic write and one delta.
//
// The stale wind-down anchors are cleared with the stamp — a new epoch never
// inherits an old latch, which is also what makes the "already collected" arm
// above self-healing.
func (s *apiServer) armMemberOwnerOpHandover(m *Member, op string) bool {
	if !s.memberHasStateToFlush(*m) {
		return false
	}
	m.RefocusSince = nowSecs()
	m.RefocusOp = op
	m.StoppingSince = 0.0
	m.StoppedSince = 0.0
	if grace, clocked := recycleGraceFor(op, s.reconcileCfg); clocked {
		reconcileLog("recycle: %s %s — wind-down opened (collect on stopped-report or +%.0fs)",
			op, m.ID, grace)
	} else {
		reconcileLog("recycle: %s %s — wind-down opened (collect on stopped-report or force-stop; no clock)",
			op, m.ID)
	}
	return true
}
