package main

// worker_spawn.go — the M3 Phase 6 outsource-worker WAKE/RECLAIM lifecycle:
// the flesh behind the scheduler's notifyWorkerSpawn seam (outsource_sched.go)
// plus the dismissal hook the close-out report fires. Since the A案 P7d fold a
// worker IS a member row (kind='outsource', migrations/00025 — 外包＝正職), and
// since the A案 P6+P5b convergence (owner-gated, rc-25d6557629b5 選項①) its
// WIRE + RESCUE machinery are the member's too:
//
//   - the spawn rides the MEMBER `start` verb (spec/sse.md §7 — member_id ==
//     the ow- id, role "outsource-worker", session member-<ow-id>); the retired
//     worker_start/worker_stop verbs and the worker-<ow-id> session namespace
//     survive only as the warden-side legacy-kill transition guard;
//   - the RESCUE path (a spawn that silently fails, a ghost session wedging
//     the retry loop) runs through the SAME pure reconcile FSM the members use
//     (reconcileDecide — start_timeout / backoff / circuit / zombie-takeover;
//     see reconcileWorkerLiveness), retiring the bespoke one-shot
//     recoverStuckWorker ghost-clear.
//
// What stays outsource-specific is PLACEMENT + BOOT CONTENT: pickWorkerWarden
// (a worker has no durable machine binding; placement is decided at spawn
// time) and buildWorkerBootContext (a worker has no role doc / lessons shard —
// never the member buildBootContext fold).
//
// Wake chain (SPEC §4 / contract §A.4, ruling H8):
//
//	scheduler assigns (worker row 'assigned')
//	  → notifyWorkerSpawn: assemble the worker boot context (the shared seeds +
//	    the type manual; T-4595) + SERVER-MINT the worker
//	    token (sub == ow-id; unknown-sub auth floors to the agent class —
//	    contract §H; the token rides ONLY the directed warden frame → a 0600
//	    workdir file, never a log/chat/transcript)
//	  → push a member `start` frame onto an ONLINE warden's FIFO (fail-closed:
//	    no online warden → nothing enqueued, the cadence retries)
//	  → the warden boots tmux session member-<ow-id> running claude with the
//	    worker's own .mcp.json; the worker's first report_waking flips it
//	    'active' (T-4595 moved that flip off the retired get_my_task) — that
//	    flip, not a warden receipt, is the observable wake signal.
//
// Reclaim (SPEC §6.3 second half):
//
//	the task lands terminal → closeTask releases the worker row (panel row
//	disappears, §4.1) but the SESSION deliberately lives on so the worker can
//	run its close-out duties (learnings write-back, temp cleanup, the close-out
//	report). The reclaim then fires from either of:
//	  * the CLOSE-OUT HOOK (dismissOutsourceWorkersForTask) — the seam the
//	    close-out report handler calls the moment the worker reports done;
//	  * the GRACE BACKSTOP — a released worker whose session was never
//	    reclaimed (close-out never arrived: crashed worker, pre-close-out-tool
//	    era) is reclaimed workerReclaimGraceSecs after release by the cadence.
//	Both push a member `stop` frame keyed on the ow- id; the warden's ladder
//	targets EXACTLY member-<ow-id> (plus the legacy worker-<ow-id> residual —
//	the P5b transition sweep, cli/ocwarden command.go), nothing else.
//
// Bookkeeping is IN-MEMORY only (workerSpawnAt / workerSpawnTarget /
// workerSpawnAttempts / workerReclaimed / workerReconcileStates, all under
// outsourceMu): a restart forgets pacing and FSM state (worst
// case one extra start — the warden's clobber guard refuses a live
// session) and forgets which reclaims already went out (worst case one extra
// stop per released worker — stopping an absent session is a clean
// no-op). Durable truth stays in the worker row alone.

import (
	"strings"
)

const (
	// legacyWardenCmdWorkerStart is the RETIRED worker verb (P5b convergence:
	// workers now ride the member `start`/`stop` verbs). Kept ONLY so the
	// receipt fold still recognises a receipt from an old warden build during
	// the transition window (api_monitoring.go).
	legacyWardenCmdWorkerStart = "worker_start"
	// workerBootRoleLabel is the descriptive role label stamped into the worker
	// start frame (the warden's append-system-prompt "你是 <ow-id>(role=…)").
	// A worker has no role_key / role doc — this is presentation only, and it
	// mirrors cli/ocwarden worker.go workerBootRole.
	workerBootRoleLabel = "outsource-worker"
	// workerSpawnRetrySecs paces re-dispatch of the worker start for a worker
	// still sitting in 'assigned' (booted but not yet claimed, or the frame
	// was lost with its dying connection). Mirrors the reconcile start
	// timeout (lifecycle §4.4 start_timeout 90s): a healthy boot claims well
	// within it; a lost frame is re-pushed right after it.
	workerSpawnRetrySecs = 90.0
	// workerReclaimGraceSecs is the backstop window between a worker's
	// release (task terminal) and the forced session reclaim, giving the
	// worker time to run its §6.3 close-out duties. Mirrors stop_grace /
	// recycle_grace (120s). A close-out report reclaims IMMEDIATELY via the
	// dismissal hook; the grace only catches workers that never report.
	workerReclaimGraceSecs = 120.0
	// reassignHandoverTimeoutSecs bounds how long a task may sit in `reassigning`
	// before the handover-timeout reaper (outsource_sched.go runOutsourceTick)
	// gives up on the successor's takeover report and reclaims the PREDECESSOR
	// outsource worker's leaked session (T-ba04). Deliberately generous — a real
	// handover dialogue (successor boots, reads up, asks, predecessor answers)
	// can take many minutes; this only bounds the resource leak when the report
	// never comes. Owner-tunable decision (flagged for review): 30 minutes.
	reassignHandoverTimeoutSecs = 1800.0
	// workerSpawnCooldownSecs benches a machine for a worker after that machine
	// FAILED to boot it (a refused start receipt, or an FSM zombie-takeover
	// ghost-reap off it). While benched, resolveWorkerPlacement refuses that
	// machine for that worker — this is a PAUSE, not the 換機 rotation it was
	// under automatic placement: there is no other host to rotate to now that a
	// worker only ever boots where it was placed. Sized at 3× the re-dispatch
	// pace so a known-bad boot is retried after a few cycles, not every 90s.
	workerSpawnCooldownSecs = 3 * workerSpawnRetrySecs
)

// ── boot context (worker-specific assembly — NEVER the member fold) ──────────

// buildWorkerBootContext assembles the worker boot context as the STAFF boot
// context minus slot 3 — T-4595, owner-ruled:
//
//  1. 系統互動   — the shared seed, byte-for-byte identical to staff's;
//  2. 使用者自訂 — the owner's additive block, skipped entirely when blank;
//  3. the persona — staff read 角色說明 → 判準 → 長期筆記 here (the 判準 block is
//     itself skipped when that role's insight folds blank). A worker has no role,
//     so it reads NOTHING here. That is the entire difference.
//  4. 啟動程序   — the boot-sequence seed for the worker's OWN runtime, which
//     carries that runtime's 執行環境 section. Recency-authoritative, LAST.
//
// Not one word is written for outsource readers anywhere in this document; the
// assembled result is byte-for-byte the staff fold with slot 3 taken out, and
// TestWorkerBootContextIsTheStaffFoldMinusThePersona asserts exactly that.
//
// WHAT THIS ASSEMBLY NO LONGER CONTAINS, and why (all T-4595):
//
//   - the outsource OVERLAY (seeds/worker_context.md) — deleted, file and all.
//     Every paragraph in it was either false (it told workers report_waking was
//     not in their boot sequence, while HandleReportWakingApiSelfWakingPost
//     routes an outsource caller straight through workerReportWaking; it told
//     them they had no roster teammates, while §11 says the opposite), a
//     restatement of the shared seed that could drift from it silently, or a
//     difference nobody could name a harm for.
//   - the BOUND TASK in full. The boot sequence has the worker read its task
//     itself, which serves the LIVE row; the copy pasted here was a
//     spawn-time snapshot, stale by construction. Staff boot contexts have never
//     carried a task either.
//   - the TYPE MANUAL in full. Staff pull a manual with get_task_manual at the
//     moment they plan a task's steps (§10.2 says 先讀手冊 there); outsource now
//     does the same. Handing it at boot froze it at spawn AND put it in a slot
//     staff have nothing in.
//   - the 你的身分 block. Identity arrives the way it always has for staff: the
//     launcher's --append-system-prompt already opens with 你是 <ow-id>(role=
//     outsource-worker) (cli/ocwarden/spawn.go buildAppendSystemPrompt, fed the
//     worker's own id and workerBootRoleLabel). The remaining fields of that
//     block have staff equivalents that are NOT in a boot context either: model
//     and effort ride the runtime's own status reporting, and the owner's chat
//     id is substituted into the shared seed's {OWNER_ID} placeholders.
//   - the outsource-only RUNTIME TAIL (「# Runtime 開機最後一步」). It was a
//     second, hand-written copy of the 執行環境 section the runtime's own
//     boot-sequence seed already carries — listener ownership, the disabled
//     interactive prompt, the automatic context reporting — for an audience
//     staff does not have. Two copies of one instruction can only drift, and a
//     tail written for outsource readers is exactly what "not one word" forbids.
func (s *apiServer) buildWorkerBootContext(w OutsourceWorker, t Task, manual *TaskManual) (string, error) {
	head, err := s.workerSharedHead()
	if err != nil {
		return "", err
	}
	bootSeq, err := s.workerBootSequence(w.Runtime)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString("\n\n")
	b.WriteString(bootSeq)
	b.WriteString("\n")
	return b.String(), nil
}

// ── warden targeting ─────────────────────────────────────────────────────────

// pickWorkerWarden resolves the warden (= machine) a worker session boots on.
// Placement is an EXPLICIT owner decision (owner ruling 2026-07-25): the ONLY
// admissible preference is a concrete machine id, honoured only while that
// warden is genuinely dispatchable — a warden kind, active on the roster,
// online, not benched for this worker by a recent boot failure, and capable of
// the worker's runtime.
//
// Anything else returns "" and the caller fails closed:
//
//   - preferred == "" — no machine was ever chosen. There is no automatic
//     placement to fall back on; the owner must pin one.
//   - the pinned machine is offline, benched, or cannot run this runtime. The
//     old "fall back to the idlest OTHER host" arm is gone: booting a worker
//     somewhere the owner did not choose is the silent mis-placement this
//     change removes, and an offline pin is now a visible stall instead.
//
// The literal "auto" was never a warden id — IsOnline("auto") is always false,
// so an "auto" placement was an unreachable destination reconcile never healed.
// Migration 00035 normalizes stored values to "" and every write path now
// rejects it, so it is NOT special-cased here: it simply names no machine and
// falls out with everything else that does not resolve.
//
// Callers hold s.outsourceMu (the cooldown map read below shares it).
func (s *apiServer) pickWorkerWarden(w OutsourceWorker, preferred string, now float64) string {
	id, _ := s.resolveWorkerPlacement(w, preferred, now)
	return id
}

// resolveWorkerPlacement is pickWorkerWarden's body, additionally naming WHY a
// placement was refused. The two are one function on purpose: a reason derived
// separately from the decision drifts from it, and this reason is what the owner
// reads off the cockpit — it has to be the actual cause, not a plausible guess.
// The returned reason is "" exactly when a machine was resolved.
func (s *apiServer) resolveWorkerPlacement(w OutsourceWorker, preferred string, now float64) (string, string) {
	if preferred == "" {
		return "", placementReasonNoMachine + ": no machine is selected for this worker — " +
			"pick one on the worker (改機器) or on the task type's 手冊 assignee; " +
			"there is no automatic placement"
	}
	unavailable := func(detail string) (string, string) {
		return "", placementReasonUnavailable + ": machine '" + preferred + "' " + detail +
			"; no other machine is substituted"
	}
	members, err := s.dal.ListMembers()
	if err != nil {
		return unavailable("could not be looked up (server error)")
	}
	for _, m := range members {
		if m.ID != preferred {
			continue
		}
		if m.Kind != KindWarden || m.RosterStatus != RosterStatusActive {
			return unavailable("is not an active machine")
		}
		if !s.hub.IsOnline(m.ID) {
			return unavailable("is offline")
		}
		if s.workerMachineCoolingOn(w.ID, m.ID, now) {
			return unavailable("was just benched after a failed boot of this worker")
		}
		if !s.machineSupportsRuntime(m.ID, w.Runtime) {
			return unavailable("does not provide the '" + NormalizeRuntime(w.Runtime) + "' runtime")
		}
		return m.ID, ""
	}
	return unavailable("does not exist")
}

// resolveStickyWorkerPlacement is the T-98f4 THREE-TIER placement decision, and
// the only caller of resolveWorkerPlacement on the spawn path. `configured` is
// what the configuration currently says (the in-memory 發包 pick, the task row,
// the live 手冊 — notifyWorkerSpawn resolves that chain and hands the winner in).
// The tiers, in the owner's decision order:
//
//  1. w.DesiredMachineID — the owner's explicit 改機器. HARD: if that machine
//     cannot take the worker right now the spawn STALLS with a receipt and
//     nothing else is tried. An owner pin is an instruction, not a hint, and
//     silently booting somewhere else is the mis-placement the 2026-07-25 ruling
//     removed. It also outranks the sticky arm below by construction — a
//     relocate must always be able to MOVE a worker that is happily running.
//  2. w.LastMachineID — where the worker's last confirmed session ran. SOFT: a
//     preference, not a pin. When that machine is not dispatchable we fall
//     through to (3) rather than stalling. That asymmetry with (1) is the whole
//     safety argument of this feature: stickiness must never be able to strand a
//     worker on a laptop that was closed, and 「機器下線」 has to keep moving it.
//     Falling through is not the banned automatic placement either — (3) is
//     itself an owner-authored source, so no machine nobody chose is ever used.
//  3. configured — the pre-existing chain, unchanged, and still HARD. On a
//     worker that has never landed anywhere (LastMachineID == "") this function
//     is exactly the old one, which is what makes 手冊 govern the birthplace.
//
// ⚠️ OPEN OWNER QUESTION, deliberately NOT decided here (flagged 2026-07-27, an
// owner ruling is pending): rule 1 says a hand-moved worker is pulled back by no
// configuration, but the relocate handler's machine_id defaults to "" and is
// written UNCONDITIONALLY, so a relocate body that omits the field CLEARS the
// pin (the wire DTO documents that as intended). Those two statements are in
// tension. This change touches neither the handler nor that default — it only
// softens the consequence: with the pin cleared, tier (2) now keeps the worker
// where it currently is instead of dropping it straight back onto the 手冊, so
// the accidental clear no longer teleports a running worker. Whichever way the
// owner rules, the fix belongs in relocateWorkerByID, not here.
//
// One reason-message subtlety: when the sticky machine is unavailable and there
// is no OTHER configured destination to fall to, the receipt reports the STICKY
// machine's unavailability. Reporting the empty configured chain instead would
// say "no machine is selected for this worker", which is false — one was, it is
// just offline — and would send the owner to the wrong screen.
// Callers hold s.outsourceMu.
func (s *apiServer) resolveStickyWorkerPlacement(w OutsourceWorker, configured string, now float64) (string, string) {
	if w.DesiredMachineID != "" {
		return s.resolveWorkerPlacement(w, w.DesiredMachineID, now)
	}
	if w.LastMachineID != "" {
		id, why := s.resolveWorkerPlacement(w, w.LastMachineID, now)
		if id != "" {
			return id, ""
		}
		if configured == "" || configured == w.LastMachineID {
			return "", why
		}
	}
	return s.resolveWorkerPlacement(w, configured, now)
}

// The structured last_op_reason codes a refused spawn writes (the
// "<code>: <detail>" convention member.last_op_reason already uses). They exist
// so a stalled worker names its own cause on the cockpit instead of rendering as
// an unexplained grey row. The first two are placement verdicts; the rest are the
// arms notifyWorkerSpawn used to abandon LOG-ONLY — a worker that fails to boot
// for one of those looked identical to one nobody ever tried to boot, which is
// exactly the diagnosis-free blank the owner hits on a relocate that goes
// nowhere. EVERY non-dispatch now leaves a receipt.
const (
	placementReasonNoMachine   = "no_machine_selected"
	placementReasonUnavailable = "machine_unavailable"
	spawnReasonNoLiveTask      = "no_live_task"
	spawnReasonBootContext     = "boot_context_failed"
	spawnReasonNoSecret        = "no_signing_secret"
	spawnReasonTokenMint       = "token_mint_failed"
	spawnReasonFrameBuild      = "frame_build_failed"
	spawnReasonWardenLost      = "warden_unreachable"
	spawnReasonRespawnDeferred = "respawn_deferred"
	spawnReasonWakeTimeout     = "wake_timeout"
	spawnReasonNeverCollected  = "never_collected"
	spawnReasonHeldDown        = "held_down"
	// KNOWN LIMITATION — the receipt is a SINGLE SLOT, not a history (owner ruling:
	// keep it single for now, the structural fix is tracked on its own ticket). All
	// of the codes here write the same last_op/last_op_reason pair, so the NEWEST
	// truth overwrites the previous one. The concrete way that bites: a worker
	// stalls with never_collected (its machine's warden never picked the frame up),
	// the owner relocates it, the new machine collects the frame but the agent dies
	// booting → wake_timeout replaces it, and "that other machine never even
	// received it" is gone. Both readings were true; only the last survives. So this
	// is NOT the design end state — if you are here because a diagnosis lost its
	// earlier half, that is the known gap, not a new bug.
)

// spawnBlockedReasonCodes is the CLOSED set of "we did not dispatch" codes — the
// seam isPlacementBlockedReason matches on, so a landed START clears any of them.
// A new code of that kind must be added HERE or its stamp outlives its cause.
//
// wake_timeout, never_collected and held_down are DELIBERATELY absent:
//   - wake_timeout describes a start that WAS dispatched and failed to boot, so
//     the retry that follows must not erase why the previous one died (31751ae);
//   - never_collected is the same trap, sharper: the clear runs on every
//     successful DISPATCH, and this code exists precisely to say that dispatching
//     is not delivery — listing it would let each retry blank it and put the row
//     straight back to the permanent silence this whole change removes;
//   - held_down describes the owner's own 停止 standing, which no dispatch
//     invalidates — only a restart does, and that writes its own receipt.
var spawnBlockedReasonCodes = []string{
	placementReasonNoMachine, placementReasonUnavailable,
	spawnReasonNoLiveTask, spawnReasonBootContext, spawnReasonNoSecret,
	spawnReasonTokenMint, spawnReasonFrameBuild, spawnReasonWardenLost,
	spawnReasonRespawnDeferred,
}

// stampWorkerPlacementBlocked records WHY a worker was not dispatched, on the
// worker row the cockpit already reads (last_op / last_op_reason — the 「最近操作」
// fields). Writes only when the reason actually CHANGES, because the 30s cadence
// re-attempts the same blocked spawn indefinitely: an unconditional write would
// re-stamp last_op_at and fan an SSE delta every tick, turning one stalled
// worker into a permanent event stream. A CHANGED cause (the pin went offline,
// then the machine was deleted) always writes — the guard suppresses repetition,
// never news. clearWorkerPlacementBlock is its counterpart on the success path,
// so a block that follows a successful dispatch is news again rather than being
// mistaken for the same old block.
//
// The row is RE-READ before writing: this is a whole-row write on a snapshot the
// tick loaded earlier, and closeTask releases workers WITHOUT holding
// outsourceMu — persisting the stale copy would resurrect a released worker back
// to 'assigned'. A worker that vanished or was released in the meantime is left
// alone: there is nothing left to explain.
//
// Best-effort by contract (the stampWakeObservability rule): a persistence
// failure is logged and never changes the dispatch decision — observability must
// not be able to stall the control loop.
func (s *apiServer) stampWorkerPlacementBlocked(w *OutsourceWorker, reason string, now float64) {
	outsourceLog("spawn %s (%s): %s", w.ID, w.Codename, reason)
	fresh, err := s.dal.GetOutsourceWorker(w.ID)
	if err != nil || fresh == nil || fresh.Status == WorkerStatusReleased {
		return
	}
	if fresh.LastOp == reconcileCmdStart && fresh.LastOpReason == reason {
		return // already stamped with this exact cause — do not churn the row
	}
	ok := false
	fresh.LastOp = reconcileCmdStart
	fresh.LastOpOK = &ok
	fresh.LastOpLog = ""
	fresh.LastOpReason = reason
	fresh.LastOpAt = now
	if err := s.dal.PutOutsourceWorker(*fresh); err != nil {
		outsourceLog("spawn %s: placement-blocked stamp persist failed: %v", w.ID, err)
		return
	}
	s.publishOutsourceWorker(*fresh, triggerServer)
}

// clearWorkerPlacementBlock drops a placement-blocked stamp once a start has
// actually been dispatched. Without it the stamp outlives its cause: a worker
// blocked, then dispatched, then blocked again for the SAME reason would match
// the anti-churn guard above and write nothing, leaving the cockpit showing a
// last_op_at from the first block — "stalled an hour ago" and "stalled right
// now" would render identically, which is the silence this whole change removes.
// Only a placement stamp is cleared; a warden's own receipt is never touched.
func (s *apiServer) clearWorkerPlacementBlock(workerID string) {
	fresh, err := s.dal.GetOutsourceWorker(workerID)
	if err != nil || fresh == nil || fresh.LastOp != reconcileCmdStart {
		return
	}
	if !isPlacementBlockedReason(fresh.LastOpReason) {
		return
	}
	fresh.LastOpReason = ""
	fresh.LastOpLog = ""
	// The whole stamp was server-written, so its verdict goes with its reason:
	// leaving last_op_ok=false behind would render this just-dispatched worker as a
	// FAILED start with nothing to explain it — a fresh blank of the same kind. nil
	// is the honest "no receipt yet"; the warden's own receipt fills it in.
	fresh.LastOpOK = nil
	if err := s.dal.PutOutsourceWorker(*fresh); err != nil {
		outsourceLog("spawn %s: placement-block clear failed: %v", workerID, err)
	}
}

// workerMachineKey is the workerMachineCooldown map key: one bench per
// (worker, machine) pair — a host benched for one worker still hosts others.
func workerMachineKey(workerID, machineID string) string {
	return workerID + "|" + machineID
}

// workerMachineCoolingOn reports whether machineID is currently benched for
// workerID (a boot failure within workerSpawnCooldownSecs). Callers hold
// s.outsourceMu.
func (s *apiServer) workerMachineCoolingOn(workerID, machineID string, now float64) bool {
	until, ok := s.workerMachineCooldown[workerMachineKey(workerID, machineID)]
	return ok && now < until
}

// benchWorkerMachine benches machineID for workerID until now+cooldown — called
// the moment a placement on that machine is judged to have FAILED (a refused
// worker_start receipt, or a stuck-worker ghost cleared off it). Callers hold
// s.outsourceMu.
func (s *apiServer) benchWorkerMachine(workerID, machineID string, now float64) {
	if machineID == "" {
		return
	}
	s.workerMachineCooldown[workerMachineKey(workerID, machineID)] = now + workerSpawnCooldownSecs
}

// firstNonEmpty returns the first non-empty argument ("" when there is none) —
// the placement arms read as a priority list rather than a stack of ifs.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ── wake dispatch (fills the outsource_sched.go seam) ────────────────────────

// notifyWorkerSpawn dispatches ONE member `start` frame (P5b convergence: the
// worker rides the member verb; the warden derives session member-<ow-id>)
// toward an online warden, paced by workerSpawnRetrySecs so the cadence can
// call it idempotently for every still-'assigned' worker (re-push after a lost
// frame; the warden's clobber guard refuses a duplicate against a live
// session). FAIL-CLOSED at every step: no online warden / no task / a fold or
// mint fault → nothing is enqueued, nothing is stamped, the next tick retries.
// Returns whether a frame was actually enqueued; a successful dispatch also
// stamps the worker's shared-FSM state (phase starting, last command start) so
// reconcileWorkerLiveness treats this start as in flight instead of doubling it.
//
// Callers hold s.outsourceMu (the tick already does — the spawn maps share
// its serialization).
func (s *apiServer) notifyWorkerSpawn(w OutsourceWorker, now float64) bool {
	if last, ok := s.workerSpawnAt[w.ID]; ok && now-last < workerSpawnRetrySecs {
		return false // recently dispatched — give the boot/claim time before re-pushing
	}
	t, err := s.dal.GetTask(w.TaskID)
	if err != nil || t == nil || TaskIsTerminal(t.Status) {
		// No live task to work — never boot a worker into a void. Stamped, not
		// merely logged: this arm repeats on EVERY tick forever, so log-only made
		// a permanently unbootable worker indistinguishable from a booting one.
		s.stampWorkerPlacementBlocked(&w, spawnReasonNoLiveTask+": bound task "+w.TaskID+
			" is missing or already closed — nothing left to boot this worker for", now)
		return false
	}
	var manual *TaskManual
	if t.TypeKey != "" {
		if m, err := s.dal.GetTaskManual(t.TypeKey); err == nil {
			manual = m
		}
	}
	// The machine id is consumed at SPAWN TIME — here, where warden liveness is
	// known — never at admission. Three owner-authored sources feed it, in
	// priority order: (1) the durable OWNER-PINNED desired_machine_id from a
	// 改機器 relocate (T-f190) wins — the most recent explicit placement, and it
	// survives restart; (2) else the reassign dialog's machine pick carried in
	// workerMachinePref (T-160e, in-memory); then the two DURABLE task-side
	// sources, whose order depends on which of them decided (see below): (3) the
	// task row's outsource_machine and (4) the type manual's assignee. There is no
	// fifth arm: when none of them names a machine the worker has no placement and
	// is not booted (owner ruling 2026-07-25 — a machine nobody chose is not a
	// destination). What T-8a67 changed is that a machine now much more rarely
	// goes unnamed: the create-time snapshot of the CREATOR's own placement lands
	// in arm (3) for a manual-driven task, so a manual that names none no longer
	// strands its workers.
	//
	// Arm (3) is load-bearing, not belt-and-braces: workerMachinePref is
	// in-memory by design, so a restart forgets it. It used to degrade to the old
	// automatic placement — the worker still booted, just somewhere else. With no
	// automatic arm left, forgetting would mean a PERMANENT stall for every
	// in-flight ad-hoc 發包 (no type manual to fall back to). Reading the task row
	// is what makes "the resolved spec is re-read on handover/rebirth, never
	// re-derived" true rather than aspirational.
	// T-98f4 sticky placement: the OWNER PIN is separated out from the rest of
	// the chain, because the two now rank on opposite sides of the last landing.
	// Owner ruling 2026-07-27, in his words: 「換手應該在原地,除非我有特別指定
	// 要換去別處」 and 「手冊上的機器只對 outsource worker 第一次起來生效,一旦
	// 我手動改到其他電腦上,再次換手應該活在其他電腦上」. Read as a decision
	// order that is exactly three lines:
	//
	//   1. relocated by the owner  → live on the machine he moved it to, forever,
	//      pulled back by no configuration (the pin, unchanged from before);
	//   2. never relocated, FIRST boot → the configured chain below (手冊 / task
	//      row) decides — the 手冊 governs the BIRTHPLACE;
	//   3. never relocated, later boot → stay where it actually ran last round.
	//
	// So the 手冊 decides where a worker is born and the last landing decides
	// where it lives afterwards; an owner relocate IS the act of changing that
	// landing. What is NOT a decision arm remains not one: nothing here ever
	// invents a machine nobody chose (owner ruling 2026-07-25).
	machinePref := s.workerMachinePref[w.ID]
	if machinePref == "" {
		manualMachine := ""
		if manual != nil {
			if spec := outsourceSpecOf(*manual); spec != nil {
				manualMachine = spec.Machine
			}
		}
		// Arms (3) and (4) swap by WHAT the task row carries (T-8a67,
		// task.outsource_dispatched): an explicit 發包 target outranks the manual —
		// the dispatcher named this placement for this task — but a manual-driven
		// task's row holds only a creator SNAPSHOT, which is the weaker claim of
		// the two and must lose to the live manual. Same two sources either way;
		// only their order tracks which of them actually decided.
		if t.OutsourceDispatched {
			machinePref = firstNonEmpty(t.OutsourceMachine, manualMachine)
		} else {
			machinePref = firstNonEmpty(manualMachine, t.OutsourceMachine)
		}
	}
	warden, blocked := s.resolveStickyWorkerPlacement(w, machinePref, now)
	if warden == "" {
		// Either nobody chose a machine, or the chosen one cannot take the worker
		// right now (offline / benched after a failed boot / wrong runtime).
		// Nothing is dispatched, and the stall is stamped onto the worker row so
		// the cockpit says WHY instead of showing an unexplained grey worker —
		// this used to be log-only, which made "no machine chosen" and "booting"
		// look identical to the owner.
		s.stampWorkerPlacementBlocked(&w, blocked, now)
		return false
	}
	persona, err := s.buildWorkerBootContext(w, *t, manual)
	if err != nil {
		s.stampWorkerPlacementBlocked(&w, spawnReasonBootContext+
			": could not assemble the worker's boot context: "+err.Error(), now)
		return false
	}
	if len(s.secret) == 0 {
		s.stampWorkerPlacementBlocked(&w, spawnReasonNoSecret+
			": the server has no JWT signing secret, so no worker token can be minted", now)
		return false
	}
	// Server-mint (the auth authority — never the warden, never the worker):
	// sub == the ow- id; the authz resolver floors an unknown sub to the
	// agent class (contract §H), which is exactly a worker's ceiling. The
	// token rides ONLY this directed frame; the warden lands it in a 0600
	// workdir file — never a log, never chat, never a transcript.
	//
	// machine_id claim = `warden`, the machine this worker is ACTUALLY dispatched
	// to (the resolved machine id — always concrete, never a placeholder),
	// mirroring the member token (api_auth.go mintMemberToken burns
	// DesiredMachineID). So the worker's live SSE now projects hub.MachineOf ==
	// its host, the same WHERE an agent's does.
	token, err := s.mintAgentToken(w.ID, warden, s.agentTokenTTLValue())
	if err != nil {
		s.stampWorkerPlacementBlocked(&w, spawnReasonTokenMint+
			": minting this worker's session token failed: "+err.Error(), now)
		return false
	}
	// P5b convergence: the SAME member `start` shape the reconcile producer
	// dispatches (spec/sse.md §7) — member_id is the ow- id, the warden derives
	// session member-<ow-id> and the agents/ workdir. Role is presentation only
	// (a worker has no role doc); task_type stays "" (the worker's whole context
	// is the server-assembled persona).
	frame, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC: reconcileCmdStart,
		Args: wardenStartArgs{
			MemberID:       w.ID,
			PersonaContext: persona,
			MemberToken:    token,
			Role:           workerBootRoleLabel,
			Runtime:        NormalizeRuntime(w.Runtime),
			Model:          w.Model,
			Effort:         w.Effort,
		},
	})
	if err != nil {
		s.stampWorkerPlacementBlocked(&w, spawnReasonFrameBuild+
			": could not build this worker's start frame: "+err.Error(), now)
		return false
	}
	if !s.enqueueToWarden(w.ID, warden, frame) {
		// The picked warden dropped offline between pickWorkerWarden and the
		// enqueue — the same fail-closed reachability gate the member dispatch
		// sits behind. No PACING stamp (the next tick retries a fresh pick
		// unthrottled), but a row receipt: this used to be the one refusal that
		// left the cockpit with nothing at all to read.
		s.stampWorkerPlacementBlocked(&w, spawnReasonWardenLost+": machine '"+warden+
			"' went offline between the placement decision and the dispatch", now)
		return false
	}
	if s.workerStopPending[w.ID] == warden {
		// A fresh START just landed on the machine the parked kill targeted:
		// drop the parking so a late re-fire can never shoot the NEW session.
		// A still-lingering ghost there is the clobber-guard + FSM zombie-takeover
		// path's to clear, not the stale parked stop's.
		delete(s.workerStopPending, w.ID)
	}
	// 🔴 A LANDED START BEGINS A NEW SESSION — drop the previous session's
	// boot_ts anchor here, at the dispatch, exactly like the member producer does
	// (reconcile.go, right after its own accepted enqueue). T-4235: the anchor is
	// DURABLE now, so an anchor left behind is not forgotten by the next re-exec —
	// the fresh session's connect adopts its predecessor's hours-old anchor and
	// the respawn-storm floor (restart_self's minimum liveness, the context-high
	// suppressor, the worker auto-handover loop-break) is waved through.
	//
	// It lives HERE and not only in respawnWorkerNow because respawnWorkerNow is
	// NOT the only path that begins a worker session, and the other two were the
	// ones that silently kept the anchor:
	//   - the FSM RESCUE arm (reconcileWorkerLiveness's START) — the ONLY way back
	//     for a worker whose session died on its own (crash / machine reboot / a
	//     report_stopped outside a handover). It dispatches a real START and the
	//     warden opens a brand-new session, with no kill and therefore no
	//     respawnWorkerNow anywhere on the path;
	//   - the owner-op FALL-THROUGH (respawnWorkerForOwnerOpNow) — respawnWorkerNow
	//     returns false BEFORE its clear when an ACTIVE worker has no kill target,
	//     and the caller then dispatches the start anyway.
	// Putting it on the dispatch makes "a start landed ⇒ the old anchor is gone"
	// true by construction for every present and future caller, instead of being a
	// rule each new caller has to remember. respawnWorkerNow keeps its own clear:
	// there the SESSION ENDS at the kill, even when the re-dispatch never lands.
	//
	// Ordering matters: the placement-block clear / stamp below re-READ the row
	// (GetOutsourceWorker) and write it back whole, so they must run AFTER this
	// single-column write, never before — otherwise the whole-row write would put
	// the stale anchor straight back.
	s.clearSessionBootTS(w.ID)
	s.workerSpawnAt[w.ID] = now
	s.workerSpawnTarget[w.ID] = warden
	// The warden now owes a command_result for this worker START — same deadline
	// as the member arm (receipt_watch.go); a worker start rides the member verb
	// since P5b, so its receipt keys on this same id.
	s.armReceiptWatch(w.ID, reconcileCmdStart, warden, now)
	// Spawn observability is IN-MEMORY since the P7d fold (the member-reconcile
	// posture; the former durable spawn_attempts / last_spawn_ts /
	// last_spawn_target columns were deliberately not carried into the member
	// table): a restart forgets the attempt count and the last target — worst
	// case one extra dispatch and a briefly blank machine cell, both healed by
	// the very next dispatch. The delta still fans so the cockpit re-reads the
	// projection (its machine field folds from these maps).
	s.workerSpawnAttempts[w.ID]++
	// Stamp the shared-FSM state so reconcileWorkerLiveness sees this start as
	// IN FLIGHT (phase starting, within start_timeout) — without this a dispatch
	// from the assignment loop / respawnWorkerNow would look like "no start ever
	// went out" to the FSM, whose fresh START would then bounce off the warden
	// clobber-guard and mis-read the healthy boot as a zombie.
	st := s.workerReconcileStates[w.ID]
	st.Phase = reconcilePhaseStarting
	st.LastCommand = reconcileCmdStart
	st.LastCommandAt = now
	s.workerReconcileStates[w.ID] = st
	// The start landed: any placement-blocked explanation on the row is now
	// history, and leaving it would make the NEXT block look like the same one.
	s.clearWorkerPlacementBlock(w.ID)
	s.publishOutsourceWorker(w, triggerServer)
	outsourceLog("spawn %s (%s) dispatched → warden %s (task %s, attempt %d)",
		w.ID, w.Codename, warden, t.ID, s.workerSpawnAttempts[w.ID])
	return true
}

// workerSpawnObs reads the in-memory spawn observation (last dispatch target +
// timestamp) for one worker under the scheduler lock — the projection seam for
// the HTTP read faces (projectWorker) and the identity-sweep 正身 check, which
// never hold s.outsourceMu. Callers already holding the lock read the maps
// directly instead (this helper would deadlock there).
func (s *apiServer) workerSpawnObs(workerID string) (target string, at float64) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	return s.workerSpawnTarget[workerID], s.workerSpawnAt[workerID]
}

// ── worker liveness reconcile (A案 P6 — the member FSM, retired one-shots) ───

// reconcileWorkerLiveness runs ONE non-stopped worker through the SHARED pure
// member reconcile FSM (reconcileDecide) for the spawn/rescue path — the P6
// convergence that retires the bespoke recoverStuckWorker one-shot ghost-clear:
//
//   - a not-online worker gets a START, paced by the FSM's start_timeout /
//     exponential backoff (a repeatedly failing spawn slows down instead of
//     hammering every 90s);
//   - a START that bounced off the warden clobber-guard (last_op receipt
//     "start" + reason session_already_exists — a live-but-presence-deaf ghost
//     session squatting the slot, the O-19 wedge) triggers the ZOMBIE TAKEOVER:
//     a robust member `stop` toward the last spawn target reaps the ghost, and
//     the next tick's plain START lands on a clean slot;
//   - an online worker converges (failure bookkeeping resets).
//
// Refocus / relocation are DELIBERATELY masked out of the observation: the
// outsource tick's autoHandoverWorker and the event-driven relocate handler own
// those (kill+respawn immediately) — feeding them into decideUp would double
// the machinery. FSM state lives in workerReconcileStates under outsourceMu
// (restart amnesia is the contract, exactly like the member store).
//
// Callers hold s.outsourceMu.
func (s *apiServer) reconcileWorkerLiveness(w OutsourceWorker, now float64) {
	st, ok := s.workerReconcileStates[w.ID]
	if !ok {
		st = newReconcileState()
	}
	obs := memberObservation{
		MemberID:     w.ID,
		Desired:      DesiredStateOnline,
		Online:       s.hub.IsOnline(w.ID),
		LastOpKind:   canonicalWorkerLastOp(w.LastOp),
		LastOpReason: w.LastOpReason,
	}
	decision := reconcileDecide(obs, st, s.reconcileCfg, now)
	switch decision.Command {
	case reconcileCmdStart:
		// FSM-decided START: drop the flat pacing stamp (the FSM's own
		// start_timeout/backoff already paced this decision) so notifyWorkerSpawn
		// dispatches now. An undelivered dispatch keeps the PRIOR state so the
		// next tick retries — the member producer's never-record-an-undelivered
		// -command discipline (notifyWorkerSpawn stamps the in-flight state on
		// success itself).
		s.workerReconcileStates[w.ID] = decision.State
		delete(s.workerSpawnAt, w.ID)
		if !s.notifyWorkerSpawn(w, now) {
			s.workerReconcileStates[w.ID] = st
		}
	case reconcileCmdStop:
		// Zombie takeover (the only STOP decideUp can reach with the masked
		// observation): reap the ghost on the last spawn target. An unreachable
		// target parks the kill (stopWorkerSessionOrPark — never lost); no known
		// target keeps the prior state so the next tick retries.
		target := s.workerSpawnTarget[w.ID]
		if target == "" {
			s.workerReconcileStates[w.ID] = st
			return
		}
		s.workerReconcileStates[w.ID] = decision.State
		s.stopWorkerSessionOrPark(target, w.ID)
		// This target kept a ghost the clobber-guard refused to overwrite — bench
		// it for this worker so no respawn lands on it while the reap is still in
		// flight. STALE-COMMENT FIX (T-98f4): this used to claim the respawn
		// "rotates to a different warden". It does not, and has not since the
		// 2026-07-25 no-automatic-placement ruling deleted every rotation arm —
		// benching now simply makes the next resolve answer machine_unavailable
		// and the worker STANDS STILL until workerSpawnCooldownSecs expires (the
		// constant's own comment already says so). Behaviour unchanged here; only
		// the sentence that mis-described it. Worth knowing while reading the
		// sticky placement above: stickiness makes the standstill more likely to
		// recur on the SAME machine, which is the accepted cost of staying put.
		s.benchWorkerMachine(w.ID, target, now)
		delete(s.workerSpawnAt, w.ID) // the respawn must not be throttled
		outsourceLog("rescue %s (%s): %s — robust stop → %s, %s benched",
			w.ID, w.Codename, decision.Reason, target, target)
	default:
		s.workerReconcileStates[w.ID] = decision.State
	}
	// A START that was DISPATCHED and never produced a session is the one failure
	// the worker path had no durable record of at all. The member producer has
	// turned this exact FSM signal into a receipt since T-ba62
	// (stampWakeObservability arm (b)); reconcileWorkerLiveness simply never read
	// it, so a worker whose boot silently failed kept a blank last_op forever
	// while spawn observability — being in-memory by contract — lost the machine
	// cell on the next re-exec. That is the X-46 shape: dispatched six times,
	// nothing to show for any of them. Stamped AFTER the switch so the re-START
	// this same decision may carry has already run its success-path clear; the
	// code is deliberately NOT in spawnBlockedReasonCodes, because the retry that
	// follows a wake timeout must not erase the explanation for it (the very
	// regression 31751ae fixed on the member side).
	if decision.StartTimedOut {
		target := s.workerSpawnTarget[w.ID]
		// WHICH failure this was is not a detail — the two have different culprits
		// and different fixes, and a receipt that conflates them sends the owner to
		// the wrong machine. Enqueueing a frame proves only that it was appended to
		// the hub's per-warden FIFO (EnqueueWardenCommandFor is IsOnline + a map
		// append); nothing on that path observes a reader. The backlog is the one
		// in-process fact that tells them apart: THIS worker's start frame still
		// sitting there ⇒ NOBODY has collected it, so the runtime on the far machine
		// was never even asked to start and looking at it is a wild goose chase.
		//
		// PER-SUBJECT, not per-machine (T-e0e3 review C.1). One machine's FIFO is
		// shared by every member and worker placed there, so the queue DEPTH answers
		// "does that machine owe anybody a frame" — a different question. Reading the
		// depth made a healthy, actively-draining warden get accused of not running
		// whenever any OTHER member on the same box happened to have a frame in
		// flight, and the message even talks the owner OUT of looking at the runtime,
		// which is where the real fault then was. Systematically, not rarely: the
		// reconcile and outsource cadences are both 30s and start microseconds apart,
		// so on a multi-agent machine the read lands in exactly that window.
		//
		// Residual, deliberately NOT claimed by either message: a frame that WAS
		// popped can still be lost after the pop (the drain deletes the whole FIFO
		// before writing it, and a socket write that "succeeds" is only a write into
		// a buffer — there is no ack anywhere on this path). An empty backlog
		// therefore means "collected", not "delivered".
		switch {
		case target == "":
			// No recorded destination at all. The two arms below each name a machine
			// and assert something about it; with no target they would name '' and
			// assert it — "collected by machine ''" is a confident claim made from
			// zero evidence, pointing at a log that has no host to live on. Say only
			// what is known (T-7fa1 BLOCKER-1: an honest observation beats a
			// confident wrong cause).
			s.stampWorkerPlacementBlocked(&w, spawnReasonWakeTimeout+": the start "+
				"window elapsed with no session, and this server no longer has a "+
				"record of which machine the start was sent to (the spawn ledger is "+
				"in-memory and a server restart clears it) — retry 改機器 to place it "+
				"again", now)
		case s.hub.PendingWardenCommandsFor(target, w.ID) > 0:
			s.stampWorkerPlacementBlocked(&w, spawnReasonNeverCollected+": the start "+
				"frame for this worker is still queued for machine '"+target+"' — that "+
				"machine's warden has not picked it up, so nothing has tried to boot "+
				"yet; check that ocwarden is running and holding its connection there",
				now)
		case undeliveredWorkerStart(s.hub, w.ID, s.workerSpawnAt[w.ID]):
			// The frame was popped off the FIFO and then LOST: the warden's stream
			// died mid-drain, so ReturnUndeliveredCommands dropped it (at-most-once —
			// only `update` is ever put back). The backlog is therefore 0 and the
			// arm above cannot see it, yet nothing on that machine was ever asked to
			// start. Without this arm the receipt falls through to `default` and
			// tells the owner, confidently, to go check the runtime on a machine that
			// never received the order — the exact "points at a healthy machine"
			// failure this whole change exists to prevent (c441f1a).
			//
			// The note is anchored to THIS spawn attempt, so an older loss can never
			// explain the attempt now lapsing (same rule as the member side).
			//
			// ⚠️ KNOWN LIMITATION (pre-existing ordering, NOT introduced here, and
			// deliberately NOT fixed by reordering): this whole stamp runs AFTER the
			// switch that may have re-dispatched a START, and a re-START goes through
			// EnqueueWardenCommandFor, which DELETES cmdUndelivered[w.ID]. So on a
			// single tick that both times out AND re-dispatches, the only evidence
			// this arm reads is destroyed before it is read, and the receipt falls
			// back to the wrong `default` message. Reachable in principle —
			// reconcile.go's decideUp returns Command=start together with
			// StartTimedOut=true — but masked on the ordinary path, where a live
			// backoff from registerStartFailure returns earlier (decisionNone with
			// StartTimedOut carried); no production-reachable instance has been
			// constructed. Moving the stamp before the switch is NOT the fix: the
			// stamp sits here precisely so the re-START's success-path clear has
			// already run, which is the clear-on-success behaviour 31751ae repaired.
			// A real fix needs the receipt to stop being a single slot (owner has
			// ruled that structural change to a separate ticket).
			//
			// The sibling arm has the SAME ordering skew in the opposite direction:
			// a re-START on this tick pushes a fresh frame, so PendingWardenCommandsFor
			// can read > 0 for a worker whose original frame was in fact collected.
			// That asymmetry predates this arm and is not repaired here either.
			// `never_collected`, not `wake_timeout`: both mean "nothing tried to
			// boot", and like its sibling it is deliberately kept OUT of
			// spawnBlockedReasonCodes so the retry that follows cannot erase it.
			s.stampWorkerPlacementBlocked(&w, spawnReasonNeverCollected+": the start "+
				"frame for this worker never reached machine '"+target+"' — that "+
				"machine's SSE stream failed mid-delivery and the frame was dropped "+
				"server-side, so nothing there was ever asked to boot; the machine's "+
				"connection is the suspect, not the runtime on it", now)
		default:
			s.stampWorkerPlacementBlocked(&w, spawnReasonWakeTimeout+": the start was "+
				"collected by machine '"+target+"' but this worker never came "+
				"online within the start window — check that the '"+
				NormalizeRuntime(w.Runtime)+"' runtime actually runs and is logged in on "+
				"that machine (warden log: ocwarden.err.log)", now)
		}
	}
}

// undeliveredWorkerStart reports whether workerID's START frame was drained off
// the warden FIFO and then lost before it reached the socket, during the spawn
// attempt anchored at spawnAt. A zero anchor answers false: with no recorded
// attempt every note is "older than this one", and a receipt must not borrow a
// previous failure to explain the current lapse.
func undeliveredWorkerStart(h *Hub, workerID string, spawnAt float64) bool {
	if workerID == "" || spawnAt <= 0 {
		return false
	}
	note, lost := h.UndeliveredCommandSince(workerID, spawnAt)
	return lost && note.Verb == reconcileCmdStart
}

// canonicalWorkerLastOp folds a worker row's last_op verb onto the reconcile
// vocabulary: the legacy worker_start receipts (old warden builds, pre-P5b)
// read as `start` so the zombie-takeover clobber detection keeps working across
// the transition window.
func canonicalWorkerLastOp(op string) string {
	if op == legacyWardenCmdWorkerStart {
		return reconcileCmdStart
	}
	return op
}

// enqueueWorkerStop builds and enqueues ONE member `stop` frame toward target
// for workerID — the shared "殺舊 session" primitive behind the FSM zombie
// takeover (reconcileWorkerLiveness), reclaimWorkerSession (retire), and
// relocateWorkerNow (owner 改機器). P5b convergence: the frame is the member
// {member_id} stop; the warden derives member-<ow-id> (and additionally sweeps
// the legacy worker-<ow-id> residual — the transition guard), so a warden
// without either session no-ops; nothing else can be killed by construction.
// The enqueue rides the same fail-closed reachability gate as member dispatch
// (enqueueToWarden) — an offline target gets nothing (no ghost STOP into a dead
// buffer). Returns whether the frame was enqueued (false on a frame-build fault
// or an unreachable target). Callers hold s.outsourceMu.
func (s *apiServer) enqueueWorkerStop(target, workerID string) bool {
	frame, ok := buildTargetFrame(reconcileCmdStop, workerID)
	if !ok {
		return false
	}
	if !s.enqueueToWarden(workerID, target, frame) {
		return false
	}
	if s.workerStopPending[workerID] == target {
		delete(s.workerStopPending, workerID) // the owed kill just went out
	}
	// The kill landed on a warden's FIFO: a command_result is now owed for it
	// (receipt_watch.go). Without this arm a worker STOP that executed but whose
	// receipt never came back left the row reading whatever it read before —
	// the exact silence this watch exists to break.
	s.armReceiptWatch(workerID, reconcileCmdStop, target, nowSecs())
	return true
}

// stopWorkerSessionOrPark fires the worker_stop toward target and, when the
// fail-closed gate refuses it (target unreachable), PARKS it in
// workerStopPending so the scheduler tick re-fires it once the target is back —
// a live-worker kill is never silently lost (owner ruling: 殘活 session 零容忍).
// The shared caller seam for the kill sites that must not treat a refusal as
// success (respawnWorkerNow, stopWorkerNow). Callers hold s.outsourceMu.
func (s *apiServer) stopWorkerSessionOrPark(target, workerID string) {
	if s.enqueueWorkerStop(target, workerID) {
		return
	}
	s.workerStopPending[workerID] = target
	outsourceLog("worker_stop %s: target %s unreachable — parked, tick will re-fire",
		workerID, target)
}

// retryPendingWorkerStop re-fires a parked worker_stop (see workerStopPending)
// once per tick until the target warden is reachable and drains it; the
// successful enqueue clears the parking (inside enqueueWorkerStop). Killing an
// absent session is a clean no-op by construction, so a late retry can never
// hurt. Callers hold s.outsourceMu.
func (s *apiServer) retryPendingWorkerStop(workerID string) {
	target := s.workerStopPending[workerID]
	if target == "" {
		return
	}
	if s.enqueueWorkerStop(target, workerID) {
		outsourceLog("worker_stop %s: parked kill re-fired → %s", workerID, target)
	}
}

// ── relocate (owner 改機器 — T-f190) ──────────────────────────────────────────

// relocateWorkerNow re-spawns a worker onto its freshly-pinned desired_machine_id
// using the SAME 殺舊 session + 清 pacing + 重生 semantics the FSM zombie-
// takeover uses, DELIBERATELY without touching lifecycle (status stays assigned/active —
// a relocate is a placement change, not a state change):
//  1. worker_stop to the CURRENT last_spawn_target (when still online) to clear
//     the session on the old machine — the same primitive the ghost-clear fires;
//  2. drop the spawn pacing stamp so the re-dispatch is not throttled;
//  3. dispatch immediately via notifyWorkerSpawn — which now prefers the pin
//     (machinePref = desired_machine_id) so the fresh session lands on the chosen
//     machine. Immediate rather than tick-deferred because the scheduler tick only
//     re-spawns 'assigned' workers, so an ACTIVE worker would otherwise never move.
//
// Owner-chosen placement, so — unlike the ghost recovery — the old machine is NOT
// benched (the owner may relocate back) and no ghost-kill cooldown is stamped.
//
// An owner relocate must ALWAYS end in either a dispatch or a receipt. That is
// why the O-28 deferral respawnWorkerNow applies to an ACTIVE worker with no kill
// target is not simply inherited here: a relocate is an explicit placement
// decision, and swallowing it left the worker with the new pin, no session, and
// last_op/last_op_reason BLANK — the cockpit's 「尚未分配機器」 with nothing to
// explain it (the X-46 report).
//
// WHY THAT IS SAFE — and it is NOT the warden clobber-guard (T-e0e3 review B.1
// corrected the reasoning that used to stand here; the old wording pointed at a
// gate that is not on this path, which would have sent the next reader looking in
// the wrong place):
//
//   - the clobber-guard is PER-MACHINE and local. A relocate is by definition a
//     move to ANOTHER machine, so the old session lives on A while the new START
//     goes to B, and B's guard cannot see A. It does not apply here at all.
//   - what DOES apply is identitySweepOnConnect (api_infra.go / reconcile.go): a
//     session that comes up on the desired machine reaps that same id's sessions
//     on every other machine. Cross-machine single-session is enforced there.
//   - and decisively: this dispatch introduces NO new hazard class. The outsource
//     cadence tick already fires reconcileWorkerLiveness → START for an ACTIVE
//     worker with DesiredState != offline and no live SSE (outsource_sched.go),
//     i.e. in exactly this state, and it did so before this change. The
//     fall-through only removes the up-to-30s blind window before that happens.
//
// Two accepted asymmetries, recorded rather than hidden (each has its own ticket):
// the cadence path additionally masks on RefocusSince == 0.0 and this one does
// not; and dropping the pacing stamp bypasses workerSpawnRetrySecs and the FSM
// backoff — acceptable only because this is owner-explicit and thus throttled by
// a human's click rate.
// Callers hold s.outsourceMu.
func (s *apiServer) relocateWorkerNow(w OutsourceWorker) {
	s.respawnWorkerForOwnerOp(w, ownerOpRelocate)
}

// respawnWorkerForOwnerOp is the ONE path behind every owner verb that changes a
// worker in place and is expected to leave it running: relocate (改機器), restart
// (重啟), and the runtime/model change. All three previously called
// respawnWorkerNow directly and all three DISCARDED its bool, so all three could
// end in "nothing happened, nothing written" — a receipt that looks like it was
// caught is worse than no backstop at all.
//
// There is exactly ONE branch point in here, and it is the intent question:
// **does the owner currently want this worker running?** `desired_state=offline`
// is an explicit 停止 and it DOMINATES every other owner verb — the same
// member-parity invariant the scheduler tick already honours (its assigned branch
// `continue`s on it; TestStoppedWorker_TickNeverRevives pins it). Placement and
// model are recorded, nothing is started, and the row says exactly that instead of
// letting one owner action quietly overturn another. Restart cannot reach that arm
// by construction (it flips desired_state to online first, and 409s otherwise),
// which is the point: the intent lives in the state, not in per-entry copies.
//
// Everything after the branch is shared: kill the old session and make sure a
// start is genuinely ATTEMPTED, with a receipt on every refusal.
// Callers hold s.outsourceMu.
func (s *apiServer) respawnWorkerForOwnerOp(w OutsourceWorker, op string) {
	if w.DesiredState == DesiredStateOffline {
		s.stampWorkerPlacementBlocked(&w, spawnReasonHeldDown+": the "+op+" was saved, "+
			"but nothing was started — this worker is stopped; 重啟 it when you want it "+
			"to run", nowSecs())
		return
	}
	// T-98f4 rule 2 — 「我建議所有換手都可以給他機會收尾」. All three verbs used to
	// go straight to the kill: no refocus stamp, no 預告, no grace. The 換手
	// (refocus) path has had the full wind-down since T-ea82, and there is no
	// principled reason a 改機器 or a 換 model should throw away the session's
	// in-flight state when a 換手 does not — from the worker's side all four are
	// the same event (this session ends, a new one continues the task).
	if !ownerOpRevivesStoppedWorker(op) && s.workerHasStateToFlush(w) {
		s.openOwnerOpHandover(w, op)
		return
	}
	s.respawnWorkerForOwnerOpNow(w, op)
}

// The owner verbs that funnel through respawnWorkerForOwnerOp, named so the
// wind-down table below cannot drift from its call sites (they were bare string
// literals scattered across two files). `op` is still a free log tag elsewhere.
const (
	ownerOpRelocate = "relocate"      // 改機器
	ownerOpRestart  = "restart"       // 重啟
	ownerOpModel    = "runtime/model" // 換 model / runtime / effort
)

// ownerOpRevivesStoppedWorker distinguishes the ONE verb that acts on a worker
// the owner has ALREADY stopped from the ones that act on a worker he wants to
// keep running. 重啟 only reaches this code with desired_state just flipped
// offline→online, i.e. the session it would displace is one 停止 already
// dispatched a kill for: winding it down would mean fanning an SOP 預告 at a
// session under a standing kill order and then waiting out the full deadline for
// an answer that is never coming — the exact "owner waits for nothing" the rule
// exists to prevent (the D6 argument openWorkerHandoverGrace already makes for
// an offline worker; here the session is merely not dead YET).
//
// Deliberately a DENY-list, not an allow-list: a verb added later gets the
// wind-down by default, because 「所有換手都給收尾機會」 is the rule and skipping
// it is the exception that has to be argued for.
func ownerOpRevivesStoppedWorker(op string) bool { return op == ownerOpRestart }

// workerHasStateToFlush answers the ONE question rule 2 turns on: is there
// anything for this worker to wind down, or should the owner's verb take effect
// immediately? The owner's ask was 「有東西要存才等,沒有就立刻走」 — he must not
// wait out a grace window just to change a model.
//
// The server can prove exactly TWO negatives, and both are structural rather
// than guessed:
//
//   - NO LIVE SESSION (!hub.IsOnline). Nothing can hear the 預告 and nothing
//     exists to flush; waiting would burn the whole deadline for certain. This
//     is the pre-existing D6 rule openWorkerHandoverGrace already applies.
//   - THIS EPOCH'S WIND-DOWN IS ALREADY COLLECTED (RefocusSince > 0 ∧
//     StoppedSince > 0 — read the epoch guard note below; the RefocusSince half
//     is load-bearing, not decoration). 收口 latched: the
//     flush is OVER and collectWorkerHandover has ALREADY dispatched this
//     epoch's kill + re-start, carrying whatever pin/model the row held at that
//     moment. The old session stays hub.IsOnline until its warden reaps it, so
//     without this arm an owner verb landing in that window would open a SECOND
//     wind-down: openOwnerOpHandover zeroes the collected latch, re-stamps the
//     epoch — and dispatches NOTHING. The in-flight start (OLD model / OLD
//     machine) then boots, its boot_ts beats the fresh refocus_since,
//     autoHandoverWorker's loop-break calls clearWorkerRefocus, and the owner's
//     change reaches NO session at all: the cockpit shows the new value while
//     the worker runs the old one — and for 改機器 the worker stays on the old
//     machine indefinitely, because the FSM rescue is gated on !IsOnline.
//     A collected worker has nothing left to flush, so the verb goes out NOW.
//
// 🔴 T-4595 REMOVED A THIRD NEGATIVE, DELIBERATELY, AND THE COST IS BOUNDED.
// There used to be a "NEVER CLAIMED ITS TASK" arm (Status != active, i.e.
// activated_ts == 0): the assigned→active flip WAS the get_my_task claim, so a
// non-active worker had provably never been handed its task content. get_my_task
// is retired and that flip now lives in report_waking (workerReportWaking), which
// is the FIRST verb of the boot sequence — so "active" no longer proves anything
// about whether the worker ever received task content, it only proves the session
// said hello. Keeping the arm would have meant reading a stale proof; keeping the
// flip on a later verb would have meant inventing an outsource-only lifecycle step
// that staff do not have (the exact thing the owner's 「兩邊機制一樣」 rule forbids).
// So the arm is gone, and an owner verb on a freshly-booted worker that has nothing
// to save now waits for the wind-down instead of firing instantly. STAFF ALREADY
// PAY EXACTLY THIS: member_ownerop_winddown.go spells out that the staff twin has
// NO ANALOGUE of this arm on purpose, and that omitting it "errs toward winding
// down, i.e. toward the grace — the safe direction, and the wait is a CEILING not
// a duration". Both halves of that argument carry over verbatim: the 收口 fires the
// instant the worker answers report_stopped, so a session with nothing to save
// still ends in seconds; StoppingTimeoutSecs (120s) is only the ceiling.
//
// Everything else (online) opens the window — and the WAIT IS NOT THE
// DEADLINE. StoppingTimeoutSecs is a ceiling, not a duration: the 收口 fires the
// instant the worker answers report_stopped, so a session with nothing to save
// ends in seconds. That is deliberately where the judgement is made, because the
// only party that can see the agent's unsaved state is the agent. The server has
// zero visibility into a transcript; any finer server-side test (context pct,
// time since boot, message counts) would be a GUESS dressed as a criterion, and
// guessing wrong here silently discards a round of learnings. Recorded honestly:
// for the online case this is the 「照舊等滿但可提早結束」 fallback, not a
// positive detection of unsaved work.
// ⚠️ THE EPOCH GUARD ON THAT THIRD ARM (review round 3 — the hole round 2's
// own fix opened). stopped_since is latched in TWO places and only one of them
// is a handover: collectWorkerHandover latches it as the 收口 of a refocus
// epoch, and workerReportStopped's ELSE arm latches it for a report arriving
// outside any handover (an ordinary 停止 where the worker says it has finished).
// NOTHING clears the second one — clearWorkerRefocus is only reachable while
// refocus_since > 0, and the restart handler writes desired_state and nothing
// else — so it outlives the whole stop→restart cycle. Read GLOBALLY, that stale
// latch claims "already collected" forever, and every later 改機器 / 換 model on
// that worker is shot on the spot, for the rest of its life. Pairing it with
// RefocusSince > 0 asks the question that was actually meant: is THIS epoch's
// wind-down collected? An epoch is the only thing a 收口 can belong to. The
// stale latch then heals by itself, because opening the next epoch zeroes it
// (openOwnerOpHandover). Sentinels: TestOwnerOp_VerbAfterTheCollectIsNotSwallowed
// (the arm must exist) and TestOwnerOp_OrdinaryStopRestartStillWindsDownLater
// (it must not over-reach).
//
// The full input table for this predicate lives at the top of
// worker_ownerop_winddown_t98f4_test.go — every combination of
// active/online/refocus/stopped with its expected verdict. Both HIGH defects in
// this票 were mis-drawn boundaries of THIS function; a change here that the
// table does not cover means the table is now wrong too.
// Callers hold s.outsourceMu.
func (s *apiServer) workerHasStateToFlush(w OutsourceWorker) bool {
	return hasUncollectedOnlineOwnerOpState(
		w.RefocusSince, w.StoppedSince, s.hub.IsOnline(w.ID))
}

// openOwnerOpHandover puts an owner verb through the graceful wind-down: stamp a
// fresh refocus epoch (stale wind-down latches cleared — a new epoch never
// inherits an old latch) and fan the SOP 預告, exactly as workerRestartSelf and
// the context-high auto-handover do. NO kill goes out here; the 收口 belongs to
// the worker's own report_stopped or autoHandoverWorker's grace deadline, and by
// then the caller's new pin / model is already on the row, so the respawn picks
// it up. A persist fault falls back to the immediate path rather than dropping
// the owner's verb on the floor. Callers hold s.outsourceMu.
func (s *apiServer) openOwnerOpHandover(w OutsourceWorker, op string) {
	w.RefocusSince = nowSecs()
	w.RefocusOp = op
	w.StoppingSince = 0.0
	w.StoppedSince = 0.0
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		outsourceLog("%s %s (%s): refocus stamp failed (%v) — falling back to an "+
			"immediate respawn so the owner's action is not lost", op, w.ID, w.Codename, err)
		s.respawnWorkerForOwnerOpNow(w, op)
		return
	}
	s.publishOutsourceWorker(w, triggerServer)
	s.openWorkerHandoverGrace(w, triggerServer)
	outsourceLog("%s %s (%s): wind-down opened — collect on stopped-report or +%.0fs",
		op, w.ID, w.Codename, StoppingTimeoutSecs)
}

// respawnWorkerForOwnerOpNow is the IMMEDIATE arm (nothing to wind down): the
// pre-T-98f4 body, unchanged. Callers hold s.outsourceMu.
func (s *apiServer) respawnWorkerForOwnerOpNow(w OutsourceWorker, op string) {
	if s.respawnWorkerNow(w, op) {
		return
	}
	// Deferred: no kill target on an ACTIVE worker. respawnWorkerNow has already
	// stamped the deferral receipt; drop the pacing stamp its early return skipped
	// and attempt the start anyway. notifyWorkerSpawn either dispatches (clearing
	// that receipt) or replaces it with its own cause, so the row is never blank.
	delete(s.workerSpawnAt, w.ID)
	s.notifyWorkerSpawn(w, nowSecs())
}

// resolveWorkerKillTarget resolves the warden a worker kill frame is addressed
// to: the in-memory spawn target when this server run remembers the dispatch,
// else the worker's live SSE machine claim (hub.MachineOf — the restart-proof
// ground truth the member relocation STOP already dispatches on,
// reconcileOne's DispatchWarden). "" ⇒ neither source knows: spawn memory lost
// to a server restart AND no live connection right now. Callers hold
// s.outsourceMu.
func (s *apiServer) resolveWorkerKillTarget(workerID string) string {
	if t := s.workerSpawnTarget[workerID]; t != "" {
		return t
	}
	return s.hub.MachineOf(workerID)
}

// observedWorkerHost resolves a worker's RESTART-PROOF observed host for the
// read-path projection (T-c23a — the cockpit machine cell), when the in-memory
// spawn observation is empty (server re-exec forgot the dispatch, and a healthy
// live worker never re-dispatches): the live SSE machine claim (hub.MachineOf,
// the same ground truth resolveWorkerKillTarget and the member observedHost
// fold trust), else the worker's self-reported telemetry `machine`. Honest ""
// when neither observes anything. tele is the worker's OWN telemetry entry
// (nil-safe). Read-only — never feeds a kill/sweep decision.
func (s *apiServer) observedWorkerHost(workerID string, tele map[string]any) string {
	if host := s.hub.MachineOf(workerID); host != "" {
		return host
	}
	if m, _ := tele["machine"].(string); m != "" {
		return m
	}
	return ""
}

// respawnWorkerNow is the shared 殺舊 session + 清 pacing + 立即重生 primitive
// behind every owner/auto operation that moves a LIVE worker to a fresh session
// on the same bound task: relocate (改機器), refocus (換手), model change, and the
// context-high auto-handover. It (1) worker_stop's the CURRENT kill target
// (spawn memory, else the live SSE machine claim — clearing the old session;
// unreachable target → the kill parks and the tick re-fires it, never lost),
// (2) drops the spawn pacing stamp so the re-dispatch is not
// throttled, and (3) re-dispatches immediately via notifyWorkerSpawn (which
// honours the pin / manual pref). DELIBERATELY does not touch lifecycle (status
// stays assigned/active) nor the refocus/stopped markers — the caller owns those.
// Immediate rather than tick-deferred because the tick's assigned branch only
// re-spawns 'assigned' workers, so an ACTIVE worker would otherwise never move.
//
// An ACTIVE worker with NO kill target at all (server-restart amnesia + SSE
// offline) ⇒ the WHOLE cycle defers — no kill, no respawn — and returns false
// so a caller that stamped refocus_since rolls it back: active means a session
// was claimed, and respawning over an unkilled session is the O-28
// double-active (two live workers fighting one SSE slot); a deferred handover
// just retries next tick. A non-active (never-claimed 尚未分配) worker has no
// session to kill, so an empty target only skips the stop and the respawn
// proceeds (the relocate-before-first-dispatch shape). `reason` is a short log
// tag. Callers hold s.outsourceMu.
func (s *apiServer) respawnWorkerNow(w OutsourceWorker, reason string) bool {
	old := s.resolveWorkerKillTarget(w.ID)
	if old == "" && w.Status == WorkerStatusActive {
		outsourceLog("%s deferred %s (%s): no kill target "+
			"(spawn memory empty, sse offline); no kill, no respawn — tick retries",
			reason, w.ID, w.Codename)
		// The deferral is a REFUSED start, so it owes the cockpit a receipt like
		// every other one: the owner-explicit callers (relocate / restart / model
		// change) discard this bool, and log-only left the worker showing 「尚未分配
		// 機器」 with a blank last_op forever — nothing to diagnose (the X-46 report).
		// A landed START clears it (clearWorkerPlacementBlock), and the anti-churn
		// guard keeps the auto-handover retry loop from re-stamping every tick.
		s.stampWorkerPlacementBlocked(&w, spawnReasonRespawnDeferred+": the "+reason+
			" could not clear this worker's previous session — it is marked active but "+
			"neither the server's spawn memory nor a live connection knows which machine "+
			"it is on; retrying", nowSecs())
		return false
	}
	// Traceable handover record BEFORE the kill (member換手 shape: notify→reclaim→
	// respawn). The graceful "notify worker to flush handoff, then reclaim"
	// handshake landed with T-ea82 (openWorkerHandoverGrace / collectWorkerHandover)
	// — by the time a refocus reaches this funnel the grace has already been
	// honoured (stopped-report / timeout / offline fallback).
	outsourceLog("handover %s (%s): reason=%s — killing session on %q then re-spawning same task %s",
		w.ID, w.Codename, reason, old, w.TaskID)
	// Bank the dying session's live cost BEFORE the kill (T-ba6b — the same
	// bankLiveCost fold the SSE disconnect edge runs; pop-after-fold keeps a
	// later edge idempotent), so the respawn never zeroes the visible spend.
	s.bankLiveCost(w.ID)
	if old != "" { // "" here ⇒ non-active, no session to kill (guarded above)
		s.stopWorkerSessionOrPark(old, w.ID)
	}
	// The kill ends the current session: drop its boot_ts so the fresh session's
	// connect re-stamps an anchor NEWER than refocus_since — the autoHandoverWorker
	// loop-break keys on boot_ts > refocus_since (T-8fb2: onFirstConnect now only
	// stamps when absent, so a respawn MUST clear it or the loop-break never fires).
	s.clearSessionBootTS(w.ID)
	delete(s.workerSpawnAt, w.ID)     // clear pace so the re-dispatch is not throttled
	s.notifyWorkerSpawn(w, nowSecs()) // re-dispatch now → lands on the pinned machine
	outsourceLog("%s %s (%s): old session %q cleared, re-spawn dispatched",
		reason, w.ID, w.Codename, old)
	return true
}

// ── stop / restart (owner-explicit 停止/重啟 — T-f190 lifecycle) ───────────────

// stopWorkerNow kills the worker's CURRENT session and clears the spawn pacing
// WITHOUT re-dispatching — the owner-explicit 停止. The caller has already set
// desired_state="offline" (which suppresses every auto-spawn branch in the tick),
// so this only fires the kill; clearing the pace stamp means a later restart is
// never throttled. Killing an absent session is a clean no-op by construction
// (the frame addresses the worker's own derived session name). Callers hold
// s.outsourceMu.
func (s *apiServer) stopWorkerNow(w OutsourceWorker) {
	old := s.resolveWorkerKillTarget(w.ID)
	// Bank the dying session's live cost before the kill (T-ba6b, see
	// respawnWorkerNow — the shared bankLiveCost fold, idempotent per edge).
	s.bankLiveCost(w.ID)
	if old != "" {
		s.stopWorkerSessionOrPark(old, w.ID)
	} else {
		// No respawn follows a stop, so a missing target is only loud, not
		// deferred — desired_state=offline already holds the worker down, and a
		// residual session (if any) has no addressable home this instant.
		outsourceLog("stop %s (%s): no kill target (spawn memory empty, sse offline) — "+
			"kill skipped", w.ID, w.Codename)
	}
	// Session end → drop boot_ts so a later restart's connect re-stamps (T-8fb2).
	s.clearSessionBootTS(w.ID)
	delete(s.workerSpawnAt, w.ID) // a later restart re-dispatches unthrottled
	outsourceLog("stop %s (%s): session %q killed, held down (no re-spawn)",
		w.ID, w.Codename, old)
}

// ── context-high auto-handover (ACTIVE-worker tick branch — T-32e1) ──────────

// autoHandoverWorker is the ACTIVE-worker branch of the outsource tick: the
// context-high auto-refocus, the worker counterpart of the member producer's
// stampContextHighRecycle. It (1) runs the in-flight arm — the refocus
// LOOP-BREAK (clears the markers once a fresh session has booted after the
// stamp), the graceful-flush 收口 (T-ea82: collect on grace timeout, or
// immediately when the session died mid-grace), and the paced re-dispatch heal
// once collected — and (2) when the worker is NOT already handing over, stamps
// refocus_since + fans the SOP 預告 (openWorkerHandoverGrace — no synchronous
// kill) the moment its gauge crosses the HANDOVER band, reusing the SAME
// ctxHighConfig the members use (bandFor thresholds, the boot-storm loop-guard,
// and the stale-pct guard). Truth is the worker ROW status (the caller routes only ACTIVE,
// non-stopped workers here — never the mere existence of a gauge entry, which a
// released worker's leftover entry would falsely satisfy); a nil pct (no gauge,
// or stale) is a fail-safe no-op, never a kill on empty data. Callers hold
// s.outsourceMu.
func (s *apiServer) autoHandoverWorker(w OutsourceWorker, now float64) {
	record := s.gauge.Get(w.ID)
	// (1) mid-handover: refocus_since is the cooldown. Clear it once a session
	// booted AFTER the stamp (respawn landed — boot_ts is stamped on the fresh
	// SSE connect); otherwise keep the paced re-dispatch alive (a lost respawn
	// frame heals here, exactly like the assigned branch's retry loop) — never
	// re-stamp a second handover on top of the first.
	if w.RefocusSince > 0.0 {
		if bootTS, ok := gaugeBootTS(record); ok && bootTS > w.RefocusSince {
			s.clearWorkerRefocus(w.ID, "respawn landed")
			return
		}
		// T-ea82 graceful flush: stopped_since==0 ⇒ the grace window is still
		// OPEN — the old session is alive walking its SOP and NO kill has been
		// dispatched yet, so a re-dispatch here would be the exact
		// spawn-without-a-kill double-active the O-28 defer prevents. Collect
		// (kill+respawn) only when the session died mid-grace (offline — nothing
		// left can flush, waiting out the deadline is pure waste, D6) or the
		// deadline passed; otherwise keep waiting for the stopped-report.
		if w.StoppedSince <= 0.0 {
			if !s.hub.IsOnline(w.ID) {
				s.collectWorkerHandover(w, "grace-offline", triggerServer)
			} else if now >= w.RefocusSince+StoppingTimeoutSecs {
				s.collectWorkerHandover(w, "grace-timeout", triggerServer)
			}
			return
		}
		// Collected (stopped_since latched ⇒ the kill+respawn went out): keep
		// the paced re-dispatch alive so a lost respawn frame heals, exactly
		// like the assigned branch's retry loop.
		s.notifyWorkerSpawn(w, now)
		return
	}
	// (2) handover check — the IDENTICAL guards to the member auto-stamp.
	ctxhigh := s.ctxHighConfig()
	if !shouldAutoRefocus(w.Runtime, record, ctxhigh, s.codexCompactionThreshold) {
		return // below the line, or no actionable pct (nil gauge / stale) — no-op
	}
	if bootStormTripped(gaugeSecsSinceBoot(record, now), ctxhigh.MinBootSecs) {
		return // fresh boot already over the line → suppress (loop-guard)
	}
	if !s.hub.IsOnline(w.ID) {
		return // only-online (symmetric with the manual refocus gate)
	}
	// Re-read before stamping so we fold onto the freshest row (a receipt fold may
	// have raced the tick) and never resurrect a released/stopped worker, then
	// stamp refocus_since durably + kill+respawn — the automatic 換手.
	fresh, err := s.dal.GetOutsourceWorker(w.ID)
	if err != nil || fresh == nil ||
		fresh.Status == WorkerStatusReleased || fresh.DesiredState == DesiredStateOffline {
		return
	}
	fresh.RefocusSince = now
	fresh.RefocusOp = refocusOpContextHigh
	fresh.StoppingSince = 0.0 // a new handover epoch never inherits a stale latch
	fresh.StoppedSince = 0.0
	if err := s.dal.PutOutsourceWorker(*fresh); err != nil {
		outsourceLog("auto-handover %s: refocus stamp failed: %v", w.ID, err)
		return
	}
	s.publishOutsourceWorker(*fresh, triggerServer)
	// T-ea82 graceful flush: stamp + 預告, NO synchronous kill — the member換手
	// shape. The 收口 (kill+respawn) is owned by the worker's own stopped-report
	// and this branch's grace deadline (the in-flight arm above).
	s.openWorkerHandoverGrace(*fresh, triggerServer)
	outsourceLog("auto-handover %s (%s, %s): runtime handover signal — graceful refocus (grace opened)",
		w.ID, w.Codename, NormalizeRuntime(w.Runtime))
}

// clearWorkerRefocus zeroes a worker's refocus_since AND the graceful-handover
// wind-down anchors (stopping/stopped — a stale stopped_since latch bleeding
// into the next handover epoch would make the in-flight arm re-dispatch a
// spawn WITHOUT a kill) — the handover loop-break (respawn landed). `reason`
// is a short log tag. Best-effort + re-read to avoid clobbering a raced row;
// never resurrects a released row. Callers hold s.outsourceMu.
func (s *apiServer) clearWorkerRefocus(id, reason string) {
	fresh, err := s.dal.GetOutsourceWorker(id)
	if err != nil || fresh == nil ||
		(fresh.RefocusSince == 0.0 && fresh.StoppingSince == 0.0 && fresh.StoppedSince == 0.0) {
		return
	}
	fresh.RefocusSince = 0.0
	fresh.RefocusOp = ""
	fresh.StoppingSince = 0.0
	fresh.StoppedSince = 0.0
	if err := s.dal.PutOutsourceWorker(*fresh); err != nil {
		outsourceLog("refocus clear %s (%s): persist failed: %v", id, reason, err)
		return
	}
	s.publishOutsourceWorker(*fresh, triggerServer)
	outsourceLog("refocus clear %s: cleared refocus_since (%s)", id, reason)
}

// ── graceful handover (T-ea82 — member-shaped 預告→寬限→收口 for workers) ──────

// openWorkerHandoverGrace turns a freshly-stamped refocus into the member-shaped
// graceful window: fan the member-topic 預告 delta at the worker's OWN session
// (its ocagent recycleHook refetches GET /api/members/<self> and prints the
// 下線程序 handover wake — the member machinery verbatim, zero client change)
// and RETURN — the kill is owned by the 收口 drivers (the worker's own
// report_stopped, or the StoppingTimeoutSecs grace deadline in
// autoHandoverWorker's in-flight arm). An OFFLINE worker skips the window
// entirely and takes the legacy immediate kill+respawn: no session can hear the
// 預告, so a grace would only waste the full deadline (D6). Callers hold
// s.outsourceMu and have already persisted the refocus stamp.
func (s *apiServer) openWorkerHandoverGrace(w OutsourceWorker, trigger string) {
	if !s.hub.IsOnline(w.ID) {
		s.collectWorkerHandover(w, "handover-offline", trigger)
		return
	}
	s.hub.Publish("member", "patch", "member", wireOwnerID+"::"+w.ID,
		s.offboardDeltaPayload(memberFromWorker(w)), audienceMembers(w.ID), trigger)
	outsourceLog("handover %s (%s): grace opened — SOP nudge fanned, collect on "+
		"stopped-report or +%.0fs", w.ID, w.Codename, StoppingTimeoutSecs)
}

// collectWorkerHandover is the ONE 收口 funnel of the graceful worker handover:
// latch stopped_since (the durable dump-done marker — BOTH drivers key their
// once-only check on it, so a stopped-report racing the grace timeout can never
// double-collect, D4) then kill+respawn via the worker's single kill funnel.
//
// A deferred respawn (ACTIVE + no kill target — server-restart amnesia) splits
// on the session's liveness (review B1): the session GONE means this epoch can
// never self-heal — spawn memory is lost, a dead session's SSE never returns,
// and the tick's FSM rescue stays masked by refocus_since>0, so retrying the
// collect would circle forever (collect waits for a target, the target waits
// for a respawn, the respawn waits for the collect). Roll the WHOLE epoch back
// (clearWorkerRefocus — the base rollback semantics) so the ordinary FSM
// rescue re-spawns the worker next tick; there was nothing left to flush
// anyway. A session still ONLINE (a blank machine claim — no production shape,
// tokens carry the host) only rolls the latch back so the grace arm retries.
// Callers hold s.outsourceMu and pass a freshly-read row with refocus_since>0
// ∧ stopped_since==0.
func (s *apiServer) collectWorkerHandover(w OutsourceWorker, reason, trigger string) bool {
	prior := w.StoppedSince
	if w.StoppedSince <= 0.0 {
		w.StoppedSince = nowSecs()
	}
	if err := s.putMember(memberFromWorker(w), trigger); err != nil {
		outsourceLog("handover collect %s (%s): stopped latch failed: %v", w.ID, reason, err)
		return false
	}
	if !s.respawnWorkerNow(w, reason) {
		if !s.hub.IsOnline(w.ID) {
			s.clearWorkerRefocus(w.ID, "collect deferred, session gone — FSM rescue takes over")
			return false
		}
		w.StoppedSince = prior
		if err := s.dal.PutOutsourceWorker(w); err != nil {
			outsourceLog("handover collect %s (%s): latch rollback failed: %v",
				w.ID, reason, err)
		}
		return false
	}
	return true
}

// ── worker self-reports (T-ea82 — the /api/self presence verbs for ow- subs) ──

// resolveLiveWorker is the shared row lookup of the worker self-report folds:
// the caller's live (not released) worker row, errNotFound otherwise. Callers
// hold s.outsourceMu.
func (s *apiServer) resolveLiveWorker(id string) (*OutsourceWorker, error) {
	w, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		return nil, err
	}
	if w == nil || w.Status == WorkerStatusReleased {
		return nil, errNotFound
	}
	return w, nil
}

// workerReportWaking is report_waking for a kind='outsource' caller: clear the
// recycle markers (the durable loop-break, member parity). The boot-reported
// model is runtime telemetry, stored separately from the owner configuration.
// waking_since itself is NOT carried on the worker vocabulary.
//
// 🔴 T-4595 — THIS IS NOW THE assigned → active WRITE POINT, the only one.
// It used to live in get_my_task, which is retired: a worker's first boot verb
// is report_waking, exactly as a staff member's is, so the wake signal and the
// claim are the same event again instead of two. WorkerStatusAssigned is a
// projection of activated_ts == 0 (memberFromWorker stamps it), so the flip is
// durable through the ordinary putMember write below — and it publishes an
// outsource_worker delta so the cockpit panel moves on the same edge it always
// did. Idempotent: a repeat report on an already-active worker changes nothing
// and fans no worker delta.
// Takes s.outsourceMu.
func (s *apiServer) workerReportWaking(id string, model *string, trigger string) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	w, err := s.resolveLiveWorker(id)
	if err != nil {
		return nil, err
	}
	claimed := w.Status == WorkerStatusAssigned
	if claimed {
		w.Status = WorkerStatusActive
	}
	w.RefocusSince = 0.0
	w.RefocusOp = ""
	w.StoppingSince = 0.0
	w.StoppedSince = 0.0
	m := memberFromWorker(*w)
	if model != nil {
		m.ActualModel = *model
	}
	if err := s.putMember(m, trigger); err != nil {
		return nil, err
	}
	if claimed {
		// memberFromWorker minted the activated_ts; echo it back onto the row we
		// publish so the delta's status is the one that was just persisted.
		w.ActivatedTS = m.ActivatedTS
		s.publishOutsourceWorker(*w, trigger)
	}
	return &m, nil
}

// workerReportStopping is report_stopping for a kind='outsource' caller: stamp
// stopping_since IF UNSET (member parity — the cockpit may flip to 停止中, the
// server never kills on it). Takes s.outsourceMu.
func (s *apiServer) workerReportStopping(id, trigger string) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	w, err := s.resolveLiveWorker(id)
	if err != nil {
		return nil, err
	}
	if w.StoppingSince <= 0.0 {
		w.StoppingSince = nowSecs()
	}
	m := memberFromWorker(*w)
	if err := s.putMember(m, trigger); err != nil {
		return nil, err
	}
	return &m, nil
}

// workerReportStopped is report_stopped for a kind='outsource' caller — the
// event-driven 收口 of the graceful handover: the FIRST stopped-report of a
// refocus-marked, desired-online worker runs collectWorkerHandover
// (kill+respawn NOW, not on the next tick — the member recycle-kill shape); a
// repeat report, or one outside a handover, only anchors stopped_since once
// and never dispatches. Takes s.outsourceMu.
func (s *apiServer) workerReportStopped(id, trigger string) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	w, err := s.resolveLiveWorker(id)
	if err != nil {
		return nil, err
	}
	if w.StoppedSince <= 0.0 {
		if w.DesiredState == DesiredStateOnline && w.RefocusSince > 0.0 {
			s.collectWorkerHandover(*w, "stopped-report", trigger)
			if fresh, ferr := s.resolveLiveWorker(id); ferr == nil {
				w = fresh
			}
			m := memberFromWorker(*w)
			return &m, nil
		}
		w.StoppedSince = nowSecs()
		if err := s.putMember(memberFromWorker(*w), trigger); err != nil {
			return nil, err
		}
	}
	m := memberFromWorker(*w)
	return &m, nil
}

// workerRestartSelf is restart_self for a kind='outsource' caller: stamp a new
// refocus epoch (stale wind-down latches cleared) and open the graceful window
// — the exact effect of the owner's refocus button, minus the owner. The
// caller's online/min-liveness gates have already passed. Takes s.outsourceMu.
func (s *apiServer) workerRestartSelf(id string, now float64, trigger string) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	w, err := s.resolveLiveWorker(id)
	if err != nil {
		return nil, err
	}
	w.RefocusSince = now
	w.RefocusOp = refocusOpRestartSelf
	w.StoppingSince = 0.0
	w.StoppedSince = 0.0
	if err := s.dal.PutOutsourceWorker(*w); err != nil {
		return nil, err
	}
	s.publishOutsourceWorker(*w, trigger)
	s.openWorkerHandoverGrace(*w, trigger)
	m := memberFromWorker(*w)
	return &m, nil
}

// ── reclaim (fire the worker's session — SPEC §6.3 second half) ──────────────

// reclaimWorkerSession pushes the EXACT worker_stop for one worker: to the
// warden the spawn targeted when it is still online, else (restart amnesia /
// warden moved) to EVERY online warden — the frame addresses the worker's own
// derived session name, so a warden without that session no-ops; nothing else
// can be killed by construction. Marks the worker reclaimed only when at
// least one frame was enqueued (otherwise the backstop retries next tick).
//
// Callers hold s.outsourceMu.
func (s *apiServer) reclaimWorkerSession(w OutsourceWorker) {
	targets := []string{}
	if t := s.workerSpawnTarget[w.ID]; t != "" && s.hub.IsOnline(t) {
		targets = append(targets, t)
	} else if members, err := s.dal.ListMembers(); err == nil {
		for _, m := range members {
			if m.Kind == KindWarden && m.RosterStatus == RosterStatusActive &&
				s.hub.IsOnline(m.ID) {
				targets = append(targets, m.ID)
			}
		}
	}
	if len(targets) == 0 {
		outsourceLog("reclaim %s (%s): no online warden — will retry", w.ID, w.Codename)
		return
	}
	enqueued := false
	for _, warden := range targets {
		if s.enqueueWorkerStop(warden, w.ID) {
			enqueued = true
		}
	}
	if !enqueued {
		return // frame build failed for every target — retry next tick
	}
	s.workerReclaimed[w.ID] = true
	delete(s.workerReconcileStates, w.ID) // retired — drop its FSM bookkeeping
	outsourceLog("reclaim %s (%s) dispatched → warden(s) %s",
		w.ID, w.Codename, strings.Join(targets, ","))
}

// dismissOutsourceWorkersForTask is the CLOSE-OUT HOOK (SPEC §6.3 step 2):
// the close-out report handler calls it the moment a task's executor reports
// "收尾事項已處理完" — the server then fires the outsource worker(s) bound to
// that task: any not-yet-released row flips released (idempotent — closeTask
// usually already did this when the task landed terminal) and every bound
// worker's session is reclaimed NOW rather than waiting out the grace
// backstop.
//
// WIRED: the close-out report handler (POST /api/tasks/{id}/closeout,
// api_tasks.go) calls this on the FIRST successful report, right after
// closeout_ts is persisted. Safe for member-executed tasks (no worker rows →
// no-op) and safe to call repeatedly (release + reclaim are both idempotent).
// Takes outsourceMu itself — call it WITHOUT the scheduler lock held.
func (s *apiServer) dismissOutsourceWorkersForTask(taskID string, now float64, trigger string) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	released, err := s.dal.ReleaseWorkersForTask(taskID, now)
	if err != nil {
		outsourceLog("dismiss task %s: release failed: %v", taskID, err)
		return
	}
	for _, w := range released {
		s.publishOutsourceWorker(w, trigger)
	}
	workers, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		outsourceLog("dismiss task %s: worker read failed: %v", taskID, err)
		return
	}
	for _, w := range workers {
		if w.TaskID == taskID && !s.workerReclaimed[w.ID] {
			s.reclaimWorkerSession(w)
		}
	}
}

// dismissOutsourceWorkerByID fires ONE specific worker (release its row + kill
// its session) — the deferred-handover twin of dismissOutsourceWorkersForTask
// (T-ba04). The reassign path no longer dismisses the OLD outsource executor at
// reassign time (that killed the predecessor before any handover dialogue could
// happen); instead the predecessor stays live through the `reassigning` hold
// and is fired HERE, the moment the successor reports reassigning→in_progress
// (or the timeout reaper gives up on that report). By WORKER ID, never by
// task_id: an outsource→outsource takeover has already bound the NEW worker to
// the SAME task_id, so a by-task release would kill the successor too.
// Idempotent (release + reclaim are both idempotent). Takes outsourceMu itself
// — call it WITHOUT the scheduler lock held.
func (s *apiServer) dismissOutsourceWorkerByID(workerID string, now float64, trigger string) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	released, err := s.dal.ReleaseWorkerByID(workerID, now)
	if err != nil {
		outsourceLog("dismiss worker %s: release failed: %v", workerID, err)
		return
	}
	if released != nil {
		s.publishOutsourceWorker(*released, trigger)
	}
	if !s.workerReclaimed[workerID] {
		if w, err := s.dal.GetOutsourceWorker(workerID); err == nil && w != nil {
			s.reclaimWorkerSession(*w)
		}
	}
	// T-4166: a fired worker's waiting cards can never be consumed — the asker
	// is gone. Retire them (same sweep as reassign / task close / member
	// dismissal). Best-effort: a card write must never fail the dismissal.
	if _, err := s.expireWaitingCardsFromMember(workerID, now, trigger); err != nil {
		outsourceLog("dismiss worker %s: card sweep failed: %v", workerID, err)
	}
}
