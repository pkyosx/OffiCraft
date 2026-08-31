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
//     so it reads none of those three. That is the entire difference.
//     🔴 SINCE T-33 THIS SLOT IS NO LONGER EMPTY FOR A WORKER: the lore
//     對象目錄 sits at the tail of slot 3 on BOTH sides. It is not
//     role-specific — it is the station's subject directory — so subtracting it
//     from the worker would be writing (by omission) a document for outsource
//     readers, which is the one thing T-4595 forbids. The invariant is therefore
//     "staff MINUS the role-specific documents", not "staff minus slot 3"; see
//     TestWorkerBootContextIsTheStaffFoldMinusThePersona.
//  4. 啟動步驟   — the boot-sequence seed for the worker's OWN runtime, which
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

	// 傳承（lore）對象目錄 (T-33) — the tail of slot 3, at the SAME
	// relative position the staff fold puts it (assets.go, after 長期筆記 and
	// before 啟動步驟). So slot 3 is no longer empty for a worker: it holds
	// nothing ROLE-specific, and the directory is not role-specific — it is the
	// station's, and both audiences read it.
	//
	// 🔴 THIS CALL BELONGS HERE, NOT IN workerSharedHead. That function returns
	// the SHARED SEED head — bytes every reader gets identically — and this
	// directory is per-actor (the `private` wall is filtered by the reader id
	// passed below). Hanging it off the shared head would turn a per-person
	// document into "the shared core"; TestDirectoryIsNotInTheSharedHead turns
	// red when it does.
	// ⚠️ Measured: TestWorkerSharedHeadMatchesUnfilteredSeedAssembly does NOT
	// catch that move — its fixture has an empty ontology, so the section folds
	// to "" and the equality never sees the difference.
	memorySection, err := s.foldLoreSection(w.ID)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString("\n\n")
	if memorySection != "" {
		b.WriteString(memorySection)
		b.WriteString("\n\n")
	}
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
	members, err := s.dal.ListMembersIncludingOutsource()
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
	// ── added by T-ed79 so the STAFF side has the same vocabulary (#14) ──────
	// The three diagnoses decideUp used to reach and then only reconcileLog to
	// stderr. All three answer the same owner question — "I pressed 活化 and it
	// is still grey" — and all three are invalidated by a landed START, so all
	// three are in the closed set below.
	spawnReasonCircuitOpen   = "circuit_open"
	spawnReasonBackoff       = "backoff"
	spawnReasonZombieSuspect = "zombie_suspect"
	// spawnReasonSessionAlive is the receipt 重啟 leaves when it was pressed on a
	// worker that was STILL RUNNING (T-ed79 #10). That case used to be a flat
	// 409 with a clear sentence; the owner ruled 外包也不擋 (外包對齊正職 — staff
	// 活化 has no such guard), so the verb now goes through. The SENTENCE is what
	// had to survive that: without it the owner would have traded one clear
	// refusal for a warden-level "session_already_exists" bounce, which is the
	// opposite direction from every other ruling in this ticket.
	spawnReasonSessionAlive = "session_alive"
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
//     invalidates — only a restart does, and that writes its own receipt;
//   - session_alive is the same shape (T-ed79 #10): it records what the owner's
//     重啟 FOUND — a session that was still running and is being displaced — and
//     the dispatch that follows is the very thing it describes, not a refutation
//     of it. Clearing it on the landed START would blank the receipt in exactly
//     the case where it came true.
var spawnBlockedReasonCodes = []string{
	placementReasonNoMachine, placementReasonUnavailable,
	spawnReasonNoLiveTask, spawnReasonBootContext, spawnReasonNoSecret,
	spawnReasonTokenMint, spawnReasonFrameBuild, spawnReasonWardenLost,
	spawnReasonRespawnDeferred,
	spawnReasonCircuitOpen, spawnReasonBackoff, spawnReasonZombieSuspect,
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
// stampWorkerOpReceipt writes one receipt onto an IN-MEMORY worker the caller is
// about to persist itself — the twin of stampMemberOpReceipt, and the reason it
// exists is the same: an owner verb's explanation and the change it explains
// must be ONE row write and ONE delta. stampWorkerPlacementBlocked is the other
// half of the pair, for callers (the tick) that own no write of their own.
//
// "Twin" is now literal rather than descriptive: both shells hand the same five
// columns to stampOpReceipt (reconcile.go), which is where the receipt is
// actually decided. What is left here is which struct the columns come off.
func stampWorkerOpReceipt(w *OutsourceWorker, reason string, now float64) {
	stampOpReceipt(&w.LastOp, &w.LastOpOK, &w.LastOpLog, &w.LastOpReason, &w.LastOpAt,
		reconcileCmdStart, reason, now)
}

// sessionAliveWakeNote is the plain-language half of the let-pass below, and
// the SENTINEL that makes composing idempotent: the cadence re-stamps every 30s,
// so a compose that could run twice would grow the receipt without bound and fan
// an SSE delta on every tick. Kept as one const so the guard and the text can
// never drift apart.
//
// It says the one thing the generic wake_timeout sentence gets actively wrong.
// "The runtime did not come up" and "the runtime was never asked to come up,
// because the old session is still holding the seat" have opposite fixes, and
// only the second is true here — the warden looked, found a live tmux session,
// and deliberately refused to kill it.
const sessionAliveWakeNote = " — the start window then lapsed, but that is NOT a " +
	"runtime failure: the previous session is still running and the warden refused " +
	"to stomp it, so nothing new was ever started. Do not go looking for a broken " +
	"runtime on that machine; deal with the live session — 重啟 this worker to " +
	"displace it, or stop it first."

// wakeTimeoutOverWardenReceipt is the SYMMETRIC half of the rule
// clearWorkerPlacementBlock already states out loud: "a warden's own receipt is
// never touched". That protection was ONE-WAY. clearWorkerPlacementBlock guards
// itself with isPlacementBlockedReason, so it declines to clear a warden
// receipt; stampWorkerPlacementBlocked guarded nothing at all — its only check
// is the anti-churn "same exact string" test, so any DIFFERENT string won.
//
// 🔴 WHAT THIS IS, STATED HONESTLY: a SYMMETRIC DEFENCE, not a repair of an
// observed failure. NO REACHABLE PRODUCTION INSTANCE HAS BEEN CONSTRUCTED.
//
// An earlier draft of this comment asserted the field consequence outright —
// "the warden's receipt is written, then ~90s later wake_timeout replaces it
// with 'check that the runtime runs and is logged in'". That claim did not
// survive review, and it is exactly the kind of confident-but-unverified causal
// story this whole change exists to delete, so it is retracted here rather than
// quietly softened.
//
// WHY IT IS NOT REACHABLE TODAY. wake_timeout is stamped only when a decision
// carries StartTimedOut, and reconcile.go sets `startTimedOut = true` at ONE
// place (reconcile.go:636), inside the `st.LastCommand == start` block. That
// block opens with the clobber check at reconcile.go:587 — `obs.LastOpKind ==
// start && HasPrefix(obs.LastOpReason, spawnClobberReasonPrefix)` — and BOTH of
// its arms return early: the zombie-suspect arm returns decisionNone inside the
// reconnect-confirm grace, the zombie-takeover arm returns the robust STOP after
// it. Line 636 is therefore unreachable while the row carries the clobber
// prefix. The condition that would make this let-pass fire and the condition
// that routes the tick away from wake_timeout are THE SAME CONDITION.
//
// Both production call sites hand the FSM a row that carries the prefix if it is
// there at all: outsource_sched.go:447 passes the tick's own snapshot, :492
// passes a deliberate re-read. Nor is the gap a race — runOutsourceTick holds
// s.outsourceMu across the whole worker loop, and foldWorkerCommandResult (the
// only writer of the clobber prefix) holds the same lock for its whole body, so
// a fold cannot land between building the observation and re-reading for the
// stamp.
//
// WHY IT IS KEPT ANYWAY. "I could not construct a path" is not "proved
// unreachable", the asymmetry it removes is real and one line long
// (clearWorkerPlacementBlock guards; its twin did not), and the reachability
// above rests entirely on an early return in a different file that a future FSM
// change could reorder without anyone thinking about this function. A defence
// that costs one pure function and cannot misfire — see the narrowness note
// below — is worth keeping ahead of that. It is NOT worth advertising as a bug
// fix, which is why the claim above is retracted rather than restated.
//
// ⚠️ AND IT IS PARTLY BLIND EVEN WHEN IT DOES FIRE. The FSM reads the verb
// through canonicalWorkerLastOp (worker_spawn.go:1139), which folds a legacy
// `worker_start` receipt (pre-P5b warden builds) onto `start`; the gate below
// compares fresh.LastOp against reconcileCmdStart RAW, with no fold. A row
// written by an old warden would therefore never compose. Left unfolded
// deliberately: adding the fold would widen an already-unreachable branch on
// the strength of a transition window that is itself closing, and it is better
// recorded here than silently "fixed" into a shape nobody has exercised.
//
// The composition KEEPS the warden's line verbatim and IN FRONT. That is not
// courtesy: reconcile.go:588 and api_monitoring.go dispatch on
// HasPrefix(reason, spawnClobberReasonPrefix), so a rewrite that dropped the
// prefix would silently disarm the zombie-takeover path.
//
// Deliberately NARROW. It fires only for a wake_timeout stamp landing on a
// start-verb row that already carries the warden's clobber refusal. Every other
// reason, and every other prior receipt, is stamped exactly as before — a
// blanket "wake_timeout never overwrites" would trade this silence for a
// different one (see TestWakeTimeout_StillStampsWhenThereIsNoWardenReceiptToProtect).
func wakeTimeoutOverWardenReceipt(fresh OutsourceWorker, reason string) string {
	if !strings.HasPrefix(reason, spawnReasonWakeTimeout+":") {
		return reason
	}
	if fresh.LastOp != reconcileCmdStart ||
		!strings.HasPrefix(fresh.LastOpReason, spawnClobberReasonPrefix+":") {
		return reason
	}
	if strings.Contains(fresh.LastOpReason, sessionAliveWakeNote) {
		// Already composed on an earlier tick. Returning the row's own string
		// lets the anti-churn guard match and write nothing.
		return fresh.LastOpReason
	}
	return fresh.LastOpReason + sessionAliveWakeNote
}

func (s *apiServer) stampWorkerPlacementBlocked(w *OutsourceWorker, reason string, now float64) {
	outsourceLog("spawn %s (%s): %s", w.ID, w.Codename, reason)
	fresh, err := s.dal.GetOutsourceWorker(w.ID)
	if err != nil || fresh == nil || fresh.Status == WorkerStatusReleased {
		return
	}
	// Let-pass BEFORE the anti-churn compare, so a second tick sees the composed
	// string on both sides and correctly writes nothing.
	reason = wakeTimeoutOverWardenReceipt(*fresh, reason)
	if fresh.LastOp == reconcileCmdStart && fresh.LastOpReason == reason {
		return // already stamped with this exact cause — do not churn the row
	}
	stampOpReceipt(&fresh.LastOp, &fresh.LastOpOK, &fresh.LastOpLog, &fresh.LastOpReason,
		&fresh.LastOpAt, reconcileCmdStart, reason, now)
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
	// (a worker has no role doc).
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
	if s.workerStopLanded[w.ID].Target == warden {
		// Same rule, same reason, for the kill that DID go out (T-ed79 #6): the
		// session about to claim this machine is the one this START is creating,
		// so presence there can no longer be read as "the old kill failed".
		// A START toward a DIFFERENT machine leaves the arm standing — that is
		// the 改機器 shape, where the old box still owes us a dead session.
		delete(s.workerStopLanded, w.ID)
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
	// 🔴 A LANDED START STAMPS waking_since — the STAFF rule, verbatim, from the
	// same seam (stampWakeObservability). It is what makes 「喚醒中」 ONE
	// projection instead of two: PresenceState reads this column for both kinds,
	// and a worker whose wake is in flight reads waking whether or not this
	// process is the one that dispatched it.
	//
	// It has to be DURABLE and it has to be HERE. Durable, because the anchor it
	// replaces (workerSpawnAt, one line down) is reborn empty by every re-exec,
	// so a long-lived worker dispatched before a restart fell to 「離線」 mid-wake.
	// Here, because "the server asked" is the honest start of the window — a
	// worker that never boots never reports, and waiting for its own
	// report_waking is exactly the reason the staff arm stopped waiting for one.
	//
	// Ordering: this is a single-column write, so it must run BEFORE the
	// placement-block clear/stamp below, which re-read the row and write it back
	// whole. Same rule as clearSessionBootTS one line up, same reason.
	if err := s.dal.SetMemberWakingSince(w.ID, now); err != nil {
		outsourceLog("spawn %s: waking anchor persist failed: %v", w.ID, err)
	}
	// …and onto the copy this dispatch publishes, so the delta's presence is the
	// one that was just persisted rather than the pre-dispatch snapshot.
	w.WakingSince = now
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
	// 🔴 THE STOP ANCHOR BELONGS TO AN EPOCH, NOT TO THE WORKER (T-72dd).
	//
	// decideUp's recycle arm de-dupes on `st.LastCommand == stop` plus StopRetry,
	// which is right WITHIN one wind-down (a STOP that did not land is re-sent,
	// not doubled). Across two, it is wrong: a worker handed over twice in quick
	// succession would have its SECOND collect swallowed by the FIRST one's
	// anchor, and the new epoch — a genuinely different wind-down, with its own
	// 預告 already fanned — would simply never be collected.
	//
	// An epoch stamped AFTER the last stop went out is by definition not the one
	// that stop was for, so the anchor is stale and is dropped. Measured on the
	// two-handovers-in-one-second fixture (TestRefocusWorker_BanksCostAcrossRespawn),
	// where the second handover otherwise banks nothing and never collects.
	if st.LastCommand == reconcileCmdStop && w.RefocusSince > st.LastCommandAt {
		st.LastCommand = reconcileCmdNone
		st.LastCommandAt = 0.0
	}
	obs := workerObservation(w, s.hub.IsOnline(w.ID))
	decision := reconcileDecide(obs, st, s.reconcileConfigLive(), now)
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
		// 🔴 THE FSM DECIDED A COLLECT — collectWorkerHandover EXECUTES it
		// (T-72dd). The DECISION is the shared FSM's and only the shared FSM's;
		// what happens next is the worker path's existing 收口 funnel, and it is
		// kept because it does things a bare stop+start does not:
		//
		//   * it banks the dying session's live cost (bankLiveCost) so a handover
		//     does not zero the owner-visible spend;
		//   * it re-spawns onto the SAME task (respawnWorkerNow logs and honours
		//     w.TaskID) — the owner's rule that a worker's life follows its task;
		//   * it CLEARS session boot_ts, which is what lets the loop-break fire
		//     at all (it keys on boot_ts > refocus_since). A plain FSM stop+start
		//     would leave the old anchor in place, the loop-break would never
		//     fire, and the epoch would never clear — an endless recycle;
		//   * it rolls the epoch back when there is no kill target, instead of
		//     circling forever.
		//
		// So: one decider (decideUp), one executor (this funnel), and the two are
		// no longer allowed to disagree about WHEN — which is what having two
		// deciders meant.
		if decision.StopKind == stopKindRecycle {
			s.workerReconcileStates[w.ID] = decision.State
			if s.collectWorkerHandover(w, "fsm-recycle", triggerServer) {
				// 🔴 RESTORE THE STOP ANCHOR THE RESPAWN JUST CLOBBERED.
				//
				// Measured, and it is not obvious: collectWorkerHandover re-spawns
				// through notifyWorkerSpawn, which stamps this same FSM slot to
				// LastCommand=start — deliberately, so the fresh boot is not
				// mis-read as a zombie. That overwrites the stop anchor THIS
				// decision just wrote, and the recycle arm's de-dupe is
				// `LastCommand != stop` — so the next tick reads "no stop has been
				// dispatched", collects again immediately, and the second kill
				// lands on whatever session is up by then.
				//
				// One slot cannot hold both facts. The stop is the one that must
				// win here, because it is the one with a de-dupe keyed on it: with
				// the anchor back, a still-online worker is re-collected only past
				// StopRetry, which is the at-least-once re-dispatch the arm was
				// designed for (a STOP that did not land), NOT a double collect.
				//
				// The epoch itself is deliberately LEFT OPEN — it is ended the
				// ordinary way, by autoHandoverWorker's loop-break once the respawn
				// boots (respawnWorkerNow clears session boot_ts precisely so that
				// fires). Tearing it down here instead would make an owner verb
				// arriving in the kill→boot window look like a fresh worker and
				// open a second wind-down on a session already collected — the
				// "owner waits for nothing" shape T-98f4 removed.
				st := s.workerReconcileStates[w.ID]
				st.LastCommand = reconcileCmdStop
				st.LastCommandAt = now
				s.workerReconcileStates[w.ID] = st
			}
			return
		}
		// Zombie takeover: reap the ghost on the last spawn target. An unreachable
		// target parks the kill (stopWorkerSessionOrPark — never lost); no known
		// target keeps the prior state so the next tick retries.
		target := s.workerSpawnTarget[w.ID]
		if target == "" {
			s.workerReconcileStates[w.ID] = st
			return
		}
		s.workerReconcileStates[w.ID] = decision.State
		s.stopWorkerSessionOrPark(target, w.ID, now)
		delete(s.workerSpawnAt, w.ID) // the respawn must not be throttled
		// 🔴 BENCHING BELONGS TO THE TAKEOVER, AND THE RECYCLE NEVER GETS HERE.
		//
		// This block used to be guarded by `decision.StopKind ==
		// stopKindZombieTakeover`. That guard was DEAD CODE and it has been
		// removed rather than left reading like a live ruling — which is the exact
		// "說得跟做的不一樣" this ticket exists to delete. It could never fire:
		// recycle returns above (it routes to a different executor), and every
		// other StopKind is unreachable on the worker path —
		//   * relocate     — workerObservation leaves TargetMachine/RunningMachine
		//                    empty, so decideUp's relocation arm is masked;
		//   * winddown     — decideDown is only reached with desired offline, which
		//                    both tick call sites refuse to reconcile;
		//   * robust_resend — needs st.RobustStopPendingAt, and the only writer
		//                    (noteRobustStopDispatched) writes s.reconcileStates,
		//                    never s.workerReconcileStates.
		// So the ONLY STOP that arrives here is the takeover, and the single line
		// that says "a recycle must not bench" is the early return above — which
		// is load-bearing and tested: delete it and the recycle falls into this
		// block, benches, and
		// TestWorkerStopArm_OnlyZombieTakeoverBenchesTheMachine_T72dd goes red.
		//
		// ⚠️ If a later change makes any of the three kinds above reachable here,
		// the guard has to come BACK — with a test that reaches it. Benching is
		// only correct for a slot known to be wedged.
		//
		// Why the takeover benches at all: that target kept a ghost the
		// clobber-guard refused to overwrite, so a respawn onto it would bounce off
		// the same ghost. Benching makes the next resolve answer machine_unavailable
		// and the worker STANDS STILL until the cooldown expires (T-98f4: it does
		// NOT rotate to another warden — the 2026-07-25 no-automatic-placement
		// ruling deleted every rotation arm). That standstill is the accepted cost
		// of not spinning against a wedged slot.
		s.benchWorkerMachine(w.ID, target, now)
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
				"that machine (warden log: ocwarden.out.log)", now)
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

// workerObservation projects an outsource worker row onto the SHARED member
// reconcile input (memberObservation) — the whole of what reconcileWorkerLiveness
// lets the staff FSM see about a worker.
//
// 🔴 IT USED TO LIE, and that lie is the bug T-72dd exists to remove. Desired was
// hard-wired to online, and RefocusSince / RefocusOp / AgentStopped — the exact
// three fields decideUp's RECYCLE arm reads — were never filled at all. So the
// shared 收口 T-ed79 gave staff was structurally unreachable for a worker: the
// FSM was asked the question with the answer deleted, and answered "online:
// converged" for a worker whose agent had already filed its dump-done. The
// handover then waited for a collector that could not exist.
//
// The four fields now come off the row, which is legitimate because the row IS a
// member row (memberFromWorker) and carries every one of them durably.
//
// What is STILL deliberately masked, and why:
//
//   - TargetMachine / RunningMachine stay zero, so decideUp's RELOCATION arm
//     stays out of reach. 改機器 for a worker is owned by relocateWorkerNow /
//     the owner-op handover funnel, which stamps a refocus epoch and is collected
//     by the arm ABOVE it. Feeding the machine pair too would give the same move
//     two collectors racing each other.
//   - StoppingSince stays zero: it is read by decideDown's 加速停止 arm only, and
//     the worker 加速停止 verb is a separate ticket (T-72dd step 3). Feeding it
//     here would open a KILL path in the same commit that opens the read path,
//     which is precisely what this commit is scoped not to do.
func workerObservation(w OutsourceWorker, online bool) memberObservation {
	return memberObservation{
		MemberID: w.ID,
		// The row's real intent, not a wired-open constant. Both tick call sites
		// already refuse to reconcile an offline-desired worker
		// (outsource_sched.go), so this changes nothing they can reach today; it
		// stops the FSM being TOLD something false, which is what makes every
		// other field below safe to trust.
		Desired:      w.DesiredState,
		Online:       online,
		RefocusSince: w.RefocusSince,
		RefocusOp:    w.RefocusOp,
		AgentStopped: w.StoppedSince > 0.0,
		LastOpKind:   canonicalWorkerLastOp(w.LastOp),
		LastOpReason: w.LastOpReason,
	}
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

// stopWorkerSessionOrPark fires the worker_stop toward target and covers BOTH
// ways that kill can fail to end the session — owner ruling: 殘活 session 零容忍.
//
//   - the gate REFUSES it (target unreachable): parked in workerStopPending, and
//     the scheduler tick re-fires it once the target is back;
//   - the gate ACCEPTS it and the session outlives it anyway: armed in
//     workerStopLanded, and the tick re-pushes it once the session is still
//     there past stop_retry (T-ed79 #6, retryUnlandedWorkerStop). Landing on a
//     FIFO is NOT delivery — nothing on that path observes a reader — so the
//     accepted case needed a backstop exactly as much as the refused one. This
//     is the member producer's at-least-once posture (dispatchRobustStopNow
//     arms RobustStopPendingAt unconditionally), and it reads the SAME judgment
//     function the member cadence reads (robustStopRetryStep).
//
// The shared caller seam for the kill sites that must not treat a refusal as
// success (respawnWorkerNow, stopWorkerNow, the FSM zombie takeover).
// Callers hold s.outsourceMu.
func (s *apiServer) stopWorkerSessionOrPark(target, workerID string, now float64) {
	if s.enqueueWorkerStop(target, workerID) {
		s.workerStopLanded[workerID] = workerStopDispatch{Target: target, At: now}
		return
	}
	// The refusal supersedes whatever went out before it: the kill is owed again
	// from scratch, and the parked path is the one that owns it now.
	delete(s.workerStopLanded, workerID)
	s.workerStopPending[workerID] = target
	outsourceLog("worker_stop %s: target %s unreachable — parked, tick will re-fire",
		workerID, target)
}

// workerStopDispatch is ONE worker_stop a warden accepted, awaiting proof that
// the session it addressed actually died.
type workerStopDispatch struct {
	Target string  // the machine the frame was handed to
	At     float64 // when it went out — the stop_retry clock
}

// noteWorkerStopNoSuchSession folds ONE no_such_session stop receipt onto the
// armed worker_stop retry — the receipt half of a judgment that until now read
// PRESENCE ALONE (retryUnlandedWorkerStop below).
//
// 🔴 WHY A RECEIPT OUTRANKS PRESENCE HERE. The retry's question is "did my kill
// reach the session it addressed?", and presence can only answer a weaker one:
// "is this id online on that machine?". Those come apart exactly when it
// matters — a stale or rebound SSE claim keeps the id looking alive on the very
// machine the kill went to, so the retry re-dispatches forever and the two
// worlds it must separate ("the blade missed" vs "the blade never arrived")
// render identically. A no_such_session receipt FROM THE TARGET collapses that:
// it proves the frame was delivered, executed, and found nothing to kill. That
// is the strongest evidence this system produces about a stop, and it is owed
// its own conclusion — stop retrying.
//
// 🔴 WHY THE MACHINE COMPARISON IS LOAD-BEARING, not defensive polish. An
// identity sweep broadcasts stop to EVERY warden, and every warden that never
// hosted the session answers no_such_session as a matter of routine. Those
// receipts are true statements about somebody else's tmux. Folding them would
// abandon a genuinely undelivered kill on the word of a machine that was never
// involved — turning a re-dispatch bug into a 殘活 session bug, which is the
// worse of the two (owner ruling: 殘活 session 零容忍).
//
// reporter == "" is UNKNOWN, not "nobody" (receiptReporterMachine), so it can
// never match and the retry keeps its old behaviour. Fail-closed by
// construction: we only ever act on a positive identification.
//
// Takes s.outsourceMu itself — the receipt path (foldCommandResult) holds no
// scheduler lock, exactly as foldWorkerCommandResult beneath it relies on.
func (s *apiServer) noteWorkerStopNoSuchSession(workerID, reporter string) {
	if workerID == "" || reporter == "" {
		return
	}
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	armed, ok := s.workerStopLanded[workerID]
	if !ok || armed.Target != reporter {
		return
	}
	delete(s.workerStopLanded, workerID)
	outsourceLog("worker_stop %s: %s reported no_such_session — the kill reached the "+
		"machine it was aimed at and found nothing to kill; retry disarmed",
		workerID, reporter)
}

// retryPendingWorkerStop re-fires a parked worker_stop (see workerStopPending)
// once per tick until the target warden is reachable and drains it; the
// successful enqueue clears the parking (inside enqueueWorkerStop). Killing an
// absent session is a clean no-op by construction, so a late retry can never
// hurt. With nothing parked it falls through to the OTHER half of the same
// promise: a kill that did leave and did not take (T-ed79 #6). The parked half
// keeps its per-tick pace — it has never even reached a warden, so there is no
// kill ladder to wait out. Callers hold s.outsourceMu.
func (s *apiServer) retryPendingWorkerStop(workerID string, now float64) {
	target := s.workerStopPending[workerID]
	if target == "" {
		s.retryUnlandedWorkerStop(workerID, now)
		return
	}
	if s.enqueueWorkerStop(target, workerID) {
		s.workerStopLanded[workerID] = workerStopDispatch{Target: target, At: now}
		outsourceLog("worker_stop %s: parked kill re-fired → %s", workerID, target)
	}
}

// retryUnlandedWorkerStop re-pushes a worker_stop the warden ACCEPTED but whose
// session is demonstrably still running — the outsource twin of the member
// cadence's robust-stop arm, judged by the same robustStopRetryStep.
//
// 🔴 WHAT COUNTS AS "still running" IS NARROWER THAN PRESENCE, and it has to be.
// hub.IsOnline keys on the WORKER id, not on a session: after a restart or a
// 改機器 the same id is online again on a session this STOP never addressed, and
// reading presence alone would keep firing at the old machine for as long as the
// worker lives. The machine claim is what tells the two apart — the same fact
// the member re-dispatch aims by (obs.RunningMachine). A worker online somewhere
// ELSE means the addressed session is gone from our point of view; the residual
// (if any) belongs to the cross-machine identity sweep, not to this retry.
//
// The complementary hazard — a restart IN PLACE, where the fresh session claims
// the very machine the kill was aimed at — is disarmed at the dispatch instead:
// notifyWorkerSpawn drops this arm when its START targets the same machine,
// exactly as it already drops a parked kill there.
//
// Callers hold s.outsourceMu.
func (s *apiServer) retryUnlandedWorkerStop(workerID string, now float64) {
	armed, ok := s.workerStopLanded[workerID]
	if !ok {
		return
	}
	alive := s.hub.IsOnline(workerID) && s.hub.MachineOf(workerID) == armed.Target
	switch robustStopRetryStep(armed.At, alive, s.reconcileConfigLive().StopRetry, now) {
	case robustStopDone:
		delete(s.workerStopLanded, workerID)
	case robustStopResend:
		outsourceLog("worker_stop %s: session still live on %s past stop_retry — "+
			"the kill did not take, re-dispatching", workerID, armed.Target)
		s.stopWorkerSessionOrPark(armed.Target, workerID, now)
	}
}

// ── relocate (owner 改機器 — T-f190) ──────────────────────────────────────────

// relocateWorkerNow moves a worker onto its freshly-pinned desired_machine_id,
// DELIBERATELY without touching lifecycle (status stays assigned/active — a
// relocate is a placement change, not a state change).
//
// 🔴 READ THIS BEFORE THE NUMBERED LIST. Since T-98f4 it delegates to
// respawnWorkerForOwnerOp, so the three steps below are the arm taken by a
// worker with NOTHING TO FLUSH (offline, or an epoch already collected). A live
// worker takes the graceful arm instead: a wind-down is opened, the session
// keeps running on the OLD machine, and steps 1-3 happen at the 收口 — its own
// report_stopped, or the owner's force-stop. The list used to be presented as
// what this function unconditionally does, which is why the same claim reached
// the wire description and the MCP tool list and stood there unread.
//
// The immediate arm, using the SAME 殺舊 session + 清 pacing + 重生 semantics the
// FSM zombie-takeover uses:
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
func (s *apiServer) relocateWorkerNow(w OutsourceWorker) ownerOpOutcome {
	return s.respawnWorkerForOwnerOp(w, ownerOpRelocate)
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
// by construction, and the construction is ONE assignment: its handler sets
// DesiredState = DesiredStateOnline on the row it then passes here BY VALUE
// (HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost in api_outsource.go
// — it is the ONLY assignment to DesiredState in that handler, and nothing
// between it and the sole ownerOpRestart call site touches that field again; the
// four wind-down anchors it zeroes next, and the PutOutsourceWorker + error branch
// after them, all leave DesiredState alone), so the field this function branches
// on is never offline for 重啟. That is the point: the intent lives in the state,
// not in per-verb copies.
//
// ⚠️ It does NOT "409 otherwise". That over-spawn guard is GONE (T-ed79 #10, owner
// 2026-08-21 「往正職靠：外包也不擋」 — the 🔴 note inside that handler's body, just
// after its not-found checks).
// 重啟 on a still-live worker is accepted and stamps a session_alive RECEIPT
// instead. Nothing here depends on the 409; the assignment above is load-bearing
// on its own.
//
// Everything after the branch is shared: kill the old session and make sure a
// start is genuinely ATTEMPTED, with a receipt on every refusal.
// Callers hold s.outsourceMu.
func (s *apiServer) respawnWorkerForOwnerOp(w OutsourceWorker, op string) ownerOpOutcome {
	if w.DesiredState == DesiredStateOffline {
		s.stampWorkerPlacementBlocked(&w, spawnReasonHeldDown+": the "+op+" was saved, "+
			"but nothing was started — this worker is stopped; 重啟 it when you want it "+
			"to run", nowSecs())
		return ownerOpOutcome{HeldDown: true}
	}
	// T-98f4 rule 2 — 「我建議所有換手都可以給他機會收尾」. All three verbs used to
	// go straight to the kill: no refocus stamp, no 預告, no grace. The 換手
	// (refocus) path has had the full wind-down since T-ea82, and there is no
	// principled reason a 改機器 or a 換 model should throw away the session's
	// in-flight state when a 換手 does not — from the worker's side all four are
	// the same event (this session ends, a new one continues the task).
	if !ownerOpDisplacesTheSession(op) && s.workerHasStateToFlush(w) {
		// A ladder refusal still answers WoundDown, and deliberately: a wind-down
		// IS open on this worker — a HIGHER one — so nothing may be dispatched
		// here either. Falling through to the immediate arm would kill the very
		// session that is mid-way through the 加速停止 it was given a deadline for,
		// which is a strictly worse outcome than the bug this guard closes.
		s.openOwnerOpHandover(w, op)
		return ownerOpOutcome{WoundDown: true}
	}
	return ownerOpOutcome{Dispatched: s.respawnWorkerForOwnerOpNow(w, op)}
}

// ownerOpOutcome is WHICH of the three things an owner verb did — the answer the
// three callers used to throw away (T-ed79 #5/#12). Exactly one field is true.
//
// It exists because the HTTP faces owe their caller the same distinction the
// staff faces already make. A relocate that opened a wind-down and a relocate
// whose dispatch bounced off an unreachable warden both answer 200 with the new
// pin on the row; only the second is a failure worth alerting on, and until this
// type they were the same answer. MemberDTO has carried
// relocation_pending / relocation_deferred / activation_pending for exactly this
// reason since T-8655 / T-927a / T-ba62; the worker DTO carried none of them.
type ownerOpOutcome struct {
	// Dispatched: a worker_start actually went out to a warden.
	Dispatched bool
	// WoundDown: a graceful wind-down was opened instead. Nothing has been sent
	// YET BY DESIGN — the move/model lands at the 收口. NOT a failure.
	WoundDown bool
	// HeldDown: desired_state is offline, so the change was saved and nothing was
	// started. The row carries the held_down receipt.
	HeldDown bool
}

// Pending reports whether the owner's verb has NOT landed yet — the union of the
// two non-dispatch arms, which is exactly what relocation_pending /
// activation_pending mean on the staff side ("scheduled, not yet landed").
func (o ownerOpOutcome) Pending() bool { return !o.Dispatched }

// The owner verbs that funnel through respawnWorkerForOwnerOp, named so the
// wind-down table below cannot drift from its call sites (they were bare string
// literals scattered across two files). `op` is still a free log tag elsewhere.
const (
	ownerOpRelocate = "relocate"      // 改機器
	ownerOpRestart  = "restart"       // 重啟
	ownerOpModel    = "runtime/model" // 換 model / runtime / effort
)

// ownerOpDisplacesTheSession names the ONE verb that is not itself a request for
// a close-out. 重啟 is not a wind-down CAUSE — it is a kill+respawn. It does not
// ask the current session to flush and hand over, it DISPLACES it
// (respawnWorkerForOwnerOp → respawnWorkerForOwnerOpNow → respawnWorkerNow, which
// kills the session on the resolved target BEFORE it re-dispatches). 改機器 /
// 換 model are the opposite verb: they mean "the same session's work must survive
// this change", which is exactly what T-98f4 rule 2 buys with the 預告 + window.
//
// 🔴 IT USED TO BE CALLED ownerOpRevivesStoppedWorker, and that name carried a
// framework this comment has spent its whole life contradicting: that the verb
// it names arrives at a worker the owner has ALREADY STOPPED, so what it does is
// revive one. Measured, it does not — the name was the last place that claim
// still lived, and it is renamed rather than annotated because a name is read by
// people who never open the body (T-170e stage 2 ⑥).
//
// It can arrive at ANY live worker: its handler
// (HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost, api_outsource.go)
// has exactly two preconditions — the row exists, and it is not released — and NO
// desired-offline gate. Pressed on a worker with desired_state="online" that is
// mid-加速停止 it answers 200, and the refocus_since / refocus_op /
// stopping_since / stopped_since it zeroes just before calling in here take that
// epoch's deadline with them.
//
// That clear is correct, for the reason written at that call site (T-ed79 #11):
// those four anchors DATE THE SESSION BEING REPLACED, and carrying them into the
// successor is what makes the NEXT 改機器 / 換 model read them as "this epoch's
// wind-down is already collected" (workerHasStateToFlush, below) and shoot itself
// on the spot. So the skip is a clean sheet for a new session, not a way around
// the ladder: once the session the ladder was counting for is gone there is no
// step left to stand on, and fanning an SOP 預告 at a session that is about to be
// killed regardless would only wait out a deadline for an answer that changes
// nothing.
//
// Deliberately a DENY-list, not an allow-list: a verb added later gets the
// wind-down by default, because 「所有換手都給收尾機會」 is the rule and skipping
// it is the exception that has to be argued for.
func ownerOpDisplacesTheSession(op string) bool { return op == ownerOpRestart }

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
// down — the safe direction". The half of that argument that still carries over
// verbatim is the one that matters here: the 收口 fires the instant the worker
// answers report_stopped, so a session with nothing to save still ends in
// seconds.
//
// 🔴 THE OTHER HALF NO LONGER HOLDS. It used to read "the wait is a CEILING not
// a duration; StoppingTimeoutSecs (120s) is only the ceiling". Since T-ed79 the
// owner verbs on this funnel are 停止 — recycleGraceFor answers "no clock" for
// relocate and runtime/model, so the collect's graceExpired test never fires for
// them. There is no 120 s ceiling behind this wait; report_stopped is the only
// 收口 driver on these ops, plus the owner's force-stop. (T-72dd: "the offline
// fallback" that used to be named here is gone — an offline worker has no
// session to collect and is re-STARTed by the shared FSM instead.)
// A reader who prices this arm at "at most 120 s" is pricing the wrong thing.
//
// Everything else (online) opens the window. That is deliberately where the judgement is made, because the
// only party that can see the agent's unsaved state is the agent. The server has
// zero visibility into a transcript; any finer server-side test (context pct,
// time since boot, message counts) would be a GUESS dressed as a criterion, and
// guessing wrong here silently discards a round of learnings. Recorded honestly:
// for the online case this is the 「照舊等滿但可提早結束」 fallback, not a
// positive detection of unsaved work.
// ⚠️ THE EPOCH GUARD ON THAT THIRD ARM — the STALE-LATCH finding, which is the
// hole the 收口-window finding's own fix opened. stopped_since is latched in TWO places and only one of them
// is a handover: collectWorkerHandover latches it as the 收口 of a refocus
// epoch, and workerReportStopped's ELSE arm latches it for a report arriving
// outside any handover (an ordinary 停止 where the worker says it has finished).
// The second one is latched with NO epoch to clear it — clearWorkerRefocus is
// only reachable while refocus_since > 0, so nothing on the handover machinery
// ever sees it. (Until T-ed79 parity #11 the restart handler wrote desired_state
// and nothing else, so it outlived the whole stop→restart cycle too; 重啟 now
// clears the anchors, which closes that ROUTE but not the state — an ordinary
// stopped-report on a desired-online worker still produces it, and that is the
// fixture TestOwnerOp_OrdinaryStopRestartStillWindsDownLater uses.) Read
// GLOBALLY, that stale
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
// 🔴 THE ANSWER IS SHARED WITH THE STAFF TWIN and the two shells are NOT.
// hasUncollectedOnlineOwnerOpState (member_ownerop_winddown.go) is the whole of
// what this function decides, and memberHasStateToFlush calls the same
// expression. What that file's cell-by-cell table records is the part that must
// stay apart: this predicate carries NO desired-offline arm, because
// respawnWorkerForOwnerOp's first gate answers held_down and returns before this
// is consulted — pinned by TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt, which
// drives a desired-offline worker that is ONLINE (the state this predicate
// answers YES for) and requires a receipt, no epoch and zero frames. Merging the
// shells was measured: handing this function the staff shell closes the whole
// worker wind-down window, because every caller is behind that gate.
// Callers hold s.outsourceMu.
func (s *apiServer) workerHasStateToFlush(w OutsourceWorker) bool {
	return hasUncollectedOnlineOwnerOpState(
		w.RefocusSince, w.StoppedSince, s.hub.IsOnline(w.ID))
}

// openOwnerOpHandover puts an owner verb through the graceful wind-down: stamp a
// fresh refocus epoch (stale wind-down latches cleared — a new epoch never
// inherits an old latch) and fan the SOP 預告, exactly as workerRestartSelf and
// the context-high auto-handover do. NO kill goes out here; the 收口 belongs to
// the worker's own report_stopped or the owner escalating to 加速停止 — NOT to a
// grace deadline, because these ops carry no clock (see the 🔴 note above:
// recycleGraceFor answers "not clocked" for relocate and runtime/model, so the
// collect's graceExpired test never fires for them). T-72dd: the "confirmed-
// offline fallback" that used to be listed here is gone with autoHandoverWorker's
// collect arms — an offline worker has no session to kill and is simply
// re-STARTed by the shared FSM. By then the caller's new pin / model is already on the row,
// so the respawn picks it up. A persist fault falls back to the immediate path rather than dropping
// the owner's verb on the floor. Callers hold s.outsourceMu.
//
// 🔴 THE EPOCH IS STAMPED BY armRefocusEpoch, NOT BY HAND, and that is the whole
// of T-170e stage 1 ①. These four fields used to be written here literally, and
// a hand-written copy of a shared decision only stays equal to it for as long as
// nobody edits the original. The original grew a ladder — 下線 → 加速 → 強制,
// 「後者一旦發出我們就不該發出前者」 (winddownStageMayAdvanceTo) — and this copy
// did not, so a 換 model landing on a worker already in 加速停止 pushed the stage
// back to 停止 and took the DEADLINE with it: the worker had been told a time,
// and that time silently stopped existing. Staff were guarded the whole while,
// through armMemberOwnerOpHandover.
//
// The projection is the same one runOutsourceTick already feeds the context
// thresholds: a worker row IS a member row (memberFromWorker carries all five
// fields this decision reads — the four anchors plus forced_stop_at), so only
// the four the arm mutates are folded back, never the whole Member.
//
// Returns false when the ladder REFUSES the move. That is not a failure and the
// caller must not treat it as one: exactly as on the staff side, the owner's
// change is already on the row, a wind-down is already open at a HIGHER stage,
// and the only thing that does not happen is the stage moving backwards.
func (s *apiServer) openOwnerOpHandover(w OutsourceWorker, op string) bool {
	proj := memberFromWorker(w)
	if !armRefocusEpoch(&proj, op, nowSecs()) {
		outsourceLog("%s %s (%s): wind-down NOT re-opened — this worker is already "+
			"further along the ladder (下線 → 加速 → 強制) at %s; the change is saved "+
			"and the open wind-down keeps its own deadline",
			op, w.ID, w.Codename, w.RefocusOp)
		return false
	}
	w.RefocusSince = proj.RefocusSince
	w.RefocusOp = proj.RefocusOp
	w.StoppingSince = proj.StoppingSince
	w.StoppedSince = proj.StoppedSince
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		outsourceLog("%s %s (%s): refocus stamp failed (%v) — falling back to an "+
			"immediate respawn so the owner's action is not lost", op, w.ID, w.Codename, err)
		s.respawnWorkerForOwnerOpNow(w, op)
		return true
	}
	s.publishOutsourceWorker(w, triggerServer)
	s.openWorkerHandoverGrace(w, triggerServer)
	if grace, clocked := recycleGraceFor(op, s.reconcileConfigLive()); clocked {
		outsourceLog("%s %s (%s): wind-down opened — collect on stopped-report or +%.0fs",
			op, w.ID, w.Codename, grace)
	} else {
		outsourceLog("%s %s (%s): wind-down opened — collect on stopped-report ONLY "+
			"(this op runs no clock)", op, w.ID, w.Codename)
	}
	return true
}

// respawnWorkerForOwnerOpNow is the IMMEDIATE arm (nothing to wind down): the
// pre-T-98f4 body, unchanged. Callers hold s.outsourceMu.
func (s *apiServer) respawnWorkerForOwnerOpNow(w OutsourceWorker, op string) bool {
	if s.respawnWorkerNow(w, op) {
		return true
	}
	// Deferred: no kill target on an ACTIVE worker. respawnWorkerNow has already
	// stamped the deferral receipt; drop the pacing stamp its early return skipped
	// and attempt the start anyway. notifyWorkerSpawn either dispatches (clearing
	// that receipt) or replaces it with its own cause, so the row is never blank.
	delete(s.workerSpawnAt, w.ID)
	return s.notifyWorkerSpawn(w, nowSecs())
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
	// honoured (the shared FSM collects on the agent's stopped-report, or on a
	// clocked cause's deadline — T-72dd).
	outsourceLog("handover %s (%s): reason=%s — killing session on %q then re-spawning same task %s",
		w.ID, w.Codename, reason, old, w.TaskID)
	// Bank the dying session's live cost BEFORE the kill (T-ba6b — the same
	// bankLiveCost fold the SSE disconnect edge runs; pop-after-fold keeps a
	// later edge idempotent), so the respawn never zeroes the visible spend.
	s.bankLiveCost(w.ID)
	if old != "" { // "" here ⇒ non-active, no session to kill (guarded above)
		s.stopWorkerSessionOrPark(old, w.ID, nowSecs())
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
		s.stopWorkerSessionOrPark(old, w.ID, nowSecs())
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

// workerOfflineConfirmGraceSecs is how long a worker must be CONTINUOUSLY
// offline before the tick treats "its session is gone" as a fact and collects
// its wind-down (T-ed79 #13, owner 2026-08-21 rc-7df3deb21b3b, verbatim:
// 「反過來但是不要三分鐘這麼久 他重新連上線應該不需要這麼長」).
//
// 🔴 WHY IT HAS TO EXIST. hub.IsOnline is an instantaneous map lookup — zero
// TTL, zero linger, and the listener is deleted the moment the SSE handler
// returns. The collect arms below used to fire on ONE such sample. An ocagent
// reconnects with a 1s→15s backoff while the tick samples every 30s, so an
// ordinary network blip that happens to be sampled killed a live worker in the
// middle of writing its hand-off, and the 收口 for that round was gone.
//
// 🔴 WHY 90. It is the worst case of an HONEST reconnect, added up from the
// constants that produce it: the agent's 45s idle-read watchdog
// (cli/ocagent/listen.go) + the 15s backoff cap + one 30s cadence tick ≈ 90s.
// A worker still offline after that is not reconnecting, it is gone.
// ZombieConfirmGrace (reconcile.go) is the same 90 with a full extra START
// window of slack on top = 180s; the owner asked for shorter, so this is that
// derivation with the doubling removed — not a rounder number.
//   - DO NOT go below 90: that starts cutting off workers that are alive and
//     have simply not noticed the socket died yet.
//   - DO NOT reuse ZombieConfirmGrace: that window answers a different question
//     (is this presence-deaf session a zombie worth taking over) and an owner
//     tuning one must not silently move the other.
//   - If a later ruling names a different number, it must remain a change to
//     THIS ONE LINE. Nothing else may hard-code 90.
const workerOfflineConfirmGraceSecs = 90.0

// workerSessionConfirmedGone maintains the continuous-offline anchor for ONE
// worker and answers the only question the collect arms are allowed to ask:
// has this worker been offline long enough that "the session is gone" is a
// fact rather than one sample?
//
// It is called ONCE per tick per ACTIVE worker, before any arm branches, so the
// anchor is maintained on every path — including the ones that return without
// collecting anything. An anchor that is only advanced by the arm that reads it
// would arm itself on the first tick that happens to reach that arm, which is
// not the same clock at all.
//
// An online observation is liveness proof and DROPS the anchor: a reconnect
// inside the window cancels the collect outright, and a LATER disconnect starts
// a fresh window rather than resuming the one the reconnect already answered.
// That is the half that makes this a confirmation and not merely a delay.
// Callers hold s.outsourceMu.
func (s *apiServer) workerSessionConfirmedGone(workerID string, now float64) bool {
	if s.hub.IsOnline(workerID) {
		delete(s.workerOfflineSince, workerID)
		return false
	}
	since, armed := s.workerOfflineSince[workerID]
	if !armed {
		s.workerOfflineSince[workerID] = now
		return false
	}
	return now-since >= workerOfflineConfirmGraceSecs
}

// ── context-high auto-handover (ACTIVE-worker tick branch — T-32e1) ──────────

// autoHandoverWorker is what is LEFT of the ACTIVE-worker branch of the
// outsource tick after T-72dd took its two decisions away. It no longer decides
// when a handover opens, and it no longer decides when one is collected:
//
//   - the context thresholds moved to stampContextHighRecycle, the SAME pass
//     staff go through (runOutsourceTick feeds it the memberFromWorker
//     projection). This function used to carry its own copy of that ruling —
//     one threshold, no promotion — and that copy is the drift this ticket
//     deleted;
//   - the handover 收口 moved to the SHARED FSM (decideUp's recycle arm, reached
//     through reconcileWorkerLiveness). Its old collect arms — "grace deadline
//     passed" and "session confirmed gone" — are gone, because two collectors
//     keyed on one stopped_since latch do not merely double a kill: the second
//     one lands on the session the first one respawned.
//
// What remains is two things it is the right place for:
//
//	(0) THE 停止 EPOCH (desired_state=offline). Untouched by T-72dd, and still
//	    de-bounced by workerOfflineConfirmGraceSecs — the shared FSM never sees
//	    a desired-offline worker (both tick call sites refuse to reconcile one),
//	    so this arm is that intent's only driver.
//	(1) THE REFOCUS LOOP-BREAK: clear the epoch once a session has booted AFTER
//	    the stamp (boot_ts > refocus_since — respawnWorkerNow clears the anchor
//	    precisely so this can fire). It is an OBSERVATION that the respawn
//	    landed, not a collect decision, which is why it stays here.
//
// 🔴 The caller MUST re-read the row between this function and the FSM: the
// loop-break can close the epoch, and a stale snapshot would have the FSM
// collect a wind-down that is already over — killing the session that just came
// up. See outsource_sched.go and
// TestTickReReadsRowBeforeFSM_SoTheLoopBreakIsNotOverruled_T72dd.
//
// Truth is the worker ROW status (the caller routes only ACTIVE, non-stopped
// workers here — never the mere existence of a gauge entry, which a released
// worker's leftover would falsely satisfy). Callers hold s.outsourceMu.
func (s *apiServer) autoHandoverWorker(w OutsourceWorker, now float64) {
	// The de-bounced liveness verdict, taken ONCE and before any branch so the
	// anchor advances on every path (workerSessionConfirmedGone). The 停止 collect
	// below reads THIS and never hub.IsOnline directly: a single instantaneous
	// sample is what used to cut a live worker off mid-close-out (T-ed79 #13).
	// (T-72dd: the handover collect that was the other reader is gone — the
	// shared FSM owns it now, and it needs no de-bounce because its recycle arm
	// requires an ONLINE session and so cannot fire on a blip at all.)
	sessionGone := s.workerSessionConfirmedGone(w.ID, now)
	// (0) THE 停止 EPOCH (T-ed79), and it is FIRST for a reason: the loop-break
	// below — and the shared FSM after it — treat this worker as one that should
	// be RUNNING, which would revive a worker the owner has held down. A
	// desired-offline worker leaves here whatever else is on its row.
	//
	// What collects a 停止 is the worker's own report_stopped
	// (workerReportStopped's stop arm) — same as staff. The two cases handled
	// here are the ones no report can ever answer:
	//
	//   * the session is CONFIRMED gone (workerOfflineConfirmGraceSecs of
	//     continuous offline, not one sample — T-ed79 #13). Nothing is left to
	//     flush and no report is coming, so waiting further is pure waste (the D6
	//     rule, verbatim from the handover arm).
	//   * the owner pressed 加速停止, which stamped refocus_op=accelerated_stop
	//     and re-anchored stopping_since from HIS press. That is the ONLY clock
	//     on this arm — recycleGraceFor answers "not clocked" for a plain 停止,
	//     so the ordinary case waits indefinitely, which is the owner's ruling
	//     (rc-27d1710174dd) and not an oversight.
	//
	// A live FORCED epoch is excluded on both: its kill already went out.
	// (gracefulStopEpochOpen, api_members.go, is the "open stop epoch that is not
	// a forced one" half — the same call the sentence, the clock and the two
	// 加速停止 faces ask. StoppedSince is this site's own extra term: a report
	// already in hand means nothing here is waiting for one.)
	if w.DesiredState == DesiredStateOffline {
		if w.StoppedSince <= 0.0 && gracefulStopEpochOpen(memberFromWorker(w)) {
			if sessionGone {
				s.collectWorkerStop(w, "stop-session-gone", triggerServer)
			} else if grace, clocked := recycleGraceFor(
				w.RefocusOp, s.reconcileConfigLive()); clocked &&
				now >= w.StoppingSince+grace {
				s.collectWorkerStop(w, "stop-accelerated-deadline", triggerServer)
			}
		}
		return
	}
	record := s.gauge.Get(w.ID)
	// (1) mid-handover: the LOOP-BREAK, and nothing else. Clear the epoch once a
	// session booted AFTER the stamp (respawn landed — boot_ts is stamped on the
	// fresh SSE connect). The paced re-dispatch that used to live here is the
	// shared FSM's START now, which paces it properly (start_timeout + backoff)
	// instead of a flat stamp.
	if w.RefocusSince > 0.0 {
		if bootTS, ok := gaugeBootTS(record); ok && bootTS > w.RefocusSince {
			s.clearWorkerRefocus(w.ID, "respawn landed")
			return
		}
		// 🔴 AND THAT IS ALL THIS ARM DOES NOW (T-72dd). What used to live here —
		// "collect on confirmed-offline", "collect on the grace deadline", and
		// the paced re-dispatch after the collect — has moved to the SHARED FSM
		// (decideUp's recycle arm, reached through reconcileWorkerLiveness).
		//
		// It had to move, not be duplicated. Two collectors keyed on the same
		// stopped_since latch do not merely double the kill: collectWorkerHandover
		// killed AND respawned synchronously, so the fresh session could be up
		// while refocus_since was still set and stopped_since still latched — and
		// the FSM, reading exactly those two fields, would then robust-STOP the
		// REPLACEMENT. That is a kill landing on a healthy, seconds-old agent, and
		// it is the failure mode this consolidation exists to make impossible.
		//
		// The FSM covers both of the old arms: a session confirmed gone is not
		// online, so decideUp skips the recycle arm and re-STARTs it (there was
		// nothing left to kill anyway); a clocked cause past its deadline is the
		// recycle arm's own graceExpired test, reading the SAME recycleGraceFor.
		// The loop-break stays here because it is not a collect decision — it is
		// the observation that the respawn already landed.
		return
	}
	// (2) THE THRESHOLD ARM IS GONE (T-72dd). It used to re-implement the
	// context-pressure ruling for workers: ONE threshold (handover_pct), one
	// kind (context_high), and no promotion — a copy of a decision that already
	// lived in stampContextHighRecycle, and the copy T-ed79 did not update when
	// staff gained the second threshold and the promotion. Workers are now fed
	// through that same staff pass from runOutsourceTick, so this function no
	// longer decides WHEN a handover opens at all; it only observes that one
	// already landed.
}

// clearWorkerRefocus zeroes a worker's refocus_since AND the graceful-handover
// wind-down anchors (stopping/stopped — a stale stopped_since latch bleeding
// into the next handover epoch would make the collect re-dispatch a
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
// 〈停止〉 handover wake — the member machinery verbatim, zero client change)
// and RETURN — the kill is owned by the 收口 driver, which since T-72dd is ONE
// thing: decideUp's recycle arm, reached through reconcileWorkerLiveness. It
// collects when the agent's own report_stopped has latched stopped_since, or —
// on the ops that ARE on a clock — when recycleGraceFor's deadline passes, which
// it reads from the same winddownKindFor everything else does. 重新聚焦 opens no
// such deadline itself (T-fe5e), so on that arm the collect waits for the agent's
// report or for the owner escalating to 加速停止, which re-stamps the anchor and
// IS a clock.
//
// 🔴 THIS USED TO NAME autoHandoverWorker's in-flight arm and a
// "confirmed-offline fallback" as the other two drivers. BOTH ARE GONE (T-72dd):
// that arm's collect decisions were the second copy this ticket deleted, and it
// now does nothing but the loop-break. An offline worker is no longer
// "collected" at all — there is no live session to kill, so the FSM simply
// re-STARTs it. An OFFLINE worker skips the window
// entirely and takes the legacy immediate kill+respawn: no session can hear the
// 預告, so a grace would only waste the full deadline (D6). Callers hold
// s.outsourceMu and have already persisted the refocus stamp.
//
// ⚠️ THIS ENTRY-POINT SAMPLE IS DELIBERATELY NOT DE-BOUNCED, unlike the tick
// arms in autoHandoverWorker (workerOfflineConfirmGraceSecs, T-ed79 #13). It
// runs once, synchronously, at the instant the owner pressed the button, and
// there is no window to wait out here without parking the owner's verb: an
// already-dead worker would sit for 90s doing nothing before anything happened.
// The staff twin makes the identical judgement off the identical predicate
// (member_ownerop_winddown.go: 「worker: !hub.IsOnline → immediate / staff: SAME
// predicate」), so the two sides stay aligned on this one — #13 changed the
// SAMPLED arm, which is the one a blip can hit without anybody pressing
// anything, and left this one alone on purpose.
func (s *apiServer) openWorkerHandoverGrace(w OutsourceWorker, trigger string) {
	if !s.hub.IsOnline(w.ID) {
		// 🔴 WHICH COLLECT depends on the INTENT, not on which caller got here
		// (T-ed79). The 停止 arm routes through this same function now, and its
		// collect must never re-spawn: the owner has held the worker down. Read
		// off desired_state so the branch cannot come apart from the intent the
		// caller persisted, and so a worker that disconnects in the race between
		// the stop handler's own liveness check and this call still lands here.
		if w.DesiredState == DesiredStateOffline {
			s.collectWorkerStop(w, "stop-offline", trigger)
			return
		}
		s.collectWorkerHandover(w, "handover-offline", trigger)
		return
	}
	s.hub.Publish("member", "patch", "member", wireOwnerID+"::"+w.ID,
		s.offboardDeltaPayload(memberFromWorker(w)), audienceMembers(w.ID), trigger)
	// The log quotes the clock this epoch is ACTUALLY collected on. 重新聚焦 runs
	// none (T-fe5e), and a line that named 120 s anyway would be the same lie the
	// notice used to tell — read by whoever is debugging why nothing was collected.
	if grace, clocked := recycleGraceFor(w.RefocusOp, s.reconcileConfigLive()); clocked {
		outsourceLog("handover %s (%s): grace opened — SOP nudge fanned, collect on "+
			"stopped-report or +%.0fs", w.ID, w.Codename, grace)
	} else {
		outsourceLog("handover %s (%s): grace opened — SOP nudge fanned, collect on "+
			"stopped-report ONLY (%s runs no clock)", w.ID, w.Codename, w.RefocusOp)
	}
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

// collectWorkerStop is the 收口 of a 停止 epoch (T-ed79) — the twin of
// collectWorkerHandover for a worker the owner has HELD DOWN.
//
// The whole difference is the last line, and it is the difference the owner
// pressed the button for: latch the same durable dump-done marker, then kill
// through stopWorkerNow (no re-dispatch) instead of respawnWorkerNow. Sharing
// the handover funnel here would re-spawn a worker that was just stopped, which
// is the one outcome 停止 must never produce.
//
// There is no rollback arm and no deferral: stopWorkerNow has no "no kill target"
// failure to defer to — a missing target only means the session is already gone,
// and desired_state=offline is what keeps it that way. Callers hold s.outsourceMu.
func (s *apiServer) collectWorkerStop(w OutsourceWorker, reason, trigger string) {
	if w.StoppedSince <= 0.0 {
		w.StoppedSince = nowSecs()
	}
	if err := s.putMember(memberFromWorker(w), trigger); err != nil {
		outsourceLog("stop collect %s (%s): stopped latch failed: %v", w.ID, reason, err)
		return
	}
	s.stopWorkerNow(w)
	outsourceLog("stop collect %s (%s): close-out collected — session killed, held down",
		w.ID, reason)
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
// waking_since is deliberately NOT re-stamped here: since T-14 the anchor is
// stamped at the START DISPATCH (notifyWorkerSpawn), which is the staff rule
// (stampWakeObservability) and the whole point of the convergence — a wake
// that never produces a boot report must still read 「喚醒中」 for its window.
// The arriving report is what ends that window by bringing the session ONLINE,
// and online dominates waking in deriveLiveness.
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
		// 🔴 TWO 收口 ARMS, and the second one is the cell this ticket had to
		// prove (T-ed79). The first arm alone was correct only while 停止 killed
		// on the spot: it requires `desired online ∧ refocus_since > 0`, and a
		// 停止 epoch is NEITHER — desired_state is offline and there is no
		// refocus epoch (the stop clears it). With the graceful stop, a report
		// arriving on that arm fell through to the bare latch below, which
		// dispatches nothing at all: the worker would have said it was finished
		// and then sat there alive on a closed-out session forever, which is
		// strictly worse than the kill this verb used to do.
		switch {
		case w.DesiredState == DesiredStateOnline && w.RefocusSince > 0.0:
			// 🔴 LATCH ONLY — the kill is NOT dispatched here any more (T-72dd).
			//
			// This arm used to call collectWorkerHandover, which killed AND
			// respawned synchronously. With the shared FSM now reading
			// stopped_since (AgentStopped) that becomes a double collect, and the
			// dangerous half is not the duplicate kill but WHICH session the
			// second one lands on: the synchronous respawn can already be online
			// while refocus_since is still set and stopped_since still latched, so
			// the FSM's recycle arm would robust-STOP the fresh replacement.
			//
			// Latching is enough, and it is exactly the staff contract: the report
			// records "I am done" durably, and decideUp's recycle arm — which
			// keys on that very latch — collects on the next tick and the plain
			// START after it respawns. One decider, one kill.
			//
			// Cost, stated plainly: the collect is no longer instantaneous, it
			// waits for the next outsource tick. That is the price of being able
			// to prove only one kill goes out, and it is the same latency staff
			// have.
			w.StoppedSince = nowSecs()
			if err := s.putMember(memberFromWorker(*w), trigger); err != nil {
				return nil, err
			}
			m := memberFromWorker(*w)
			return &m, nil
		case w.DesiredState == DesiredStateOffline &&
			gracefulStopEpochOpen(memberFromWorker(*w)):
			// The 停止 arm: kill, never re-spawn. The forced epoch is excluded
			// because a force-stopped session was cut off rather than asked —
			// its kill already went out and nothing is waiting for a report.
			// That exclusion is not spelled out here: it is the second half of
			// gracefulStopEpochOpen (api_members.go), the same call every other
			// site asks.
			s.collectWorkerStop(*w, "stopped-report", trigger)
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
//
// 🔴 THE LADDER GUARD LIVES HERE because the divergence it closes lives inside
// ONE handler: HandleRestartSelfApiSelfRefocusPost dispatches an outsource
// caller to this funnel and returns EARLY, seven lines above the armRefocusEpoch
// check its staff arm falls through to. So restart_self was ladder-guarded for
// staff and unguarded for workers, in the same function, and an agent already in
// 加速停止 could talk its way back to 停止 — taking back the deadline it was
// counting to — purely by being a worker. Stamping through the shared
// armRefocusEpoch on a memberFromWorker projection is what makes "armRefocusEpoch
// is the ONE way an epoch is opened" true of this site too.
func (s *apiServer) workerRestartSelf(id string, now float64, trigger string) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	w, err := s.resolveLiveWorker(id)
	if err != nil {
		return nil, err
	}
	proj := memberFromWorker(*w)
	if !armRefocusEpoch(&proj, refocusOpRestartSelf, now) {
		return nil, errWindDownLadderBackwards
	}
	w.RefocusSince = proj.RefocusSince
	w.RefocusOp = proj.RefocusOp
	w.StoppingSince = proj.StoppingSince
	w.StoppedSince = proj.StoppedSince
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
	} else if members, err := s.dal.ListMembersIncludingOutsource(); err == nil {
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
