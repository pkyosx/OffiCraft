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
//	  → notifyWorkerSpawn: assemble the worker boot context (worker_context.md
//	    seed + identity + the bound task + its manual) + SERVER-MINT the worker
//	    token (sub == ow-id; unknown-sub auth floors to the agent class —
//	    contract §H; the token rides ONLY the directed warden frame → a 0600
//	    workdir file, never a log/chat/transcript)
//	  → push a member `start` frame onto an ONLINE warden's FIFO (fail-closed:
//	    no online warden → nothing enqueued, the cadence retries)
//	  → the warden boots tmux session member-<ow-id> running claude with the
//	    worker's own .mcp.json; the worker's first get_my_task flips it
//	    'active' (Phase 1) — that flip, not a warden receipt, is the
//	    observable wake signal.
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
	"fmt"
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
	// ghost-reap off it). While benched, pickWorkerWarden skips the machine for
	// that worker so the re-spawn lands elsewhere — 換機重試 (T-9ccf DoD②).
	// Sized at 3× the re-dispatch pace so a benched host is retried only after
	// a few full retry cycles elsewhere, never every 90s tick.
	workerSpawnCooldownSecs = 3 * workerSpawnRetrySecs
	// workerPlacementRamCeiling is the ram_pct above which a candidate warden is
	// deprioritized in "auto" placement (a host whose memory is near-exhausted is
	// where a fresh claude boot is likeliest to wedge — recon O-19 hypothesis 1).
	// Soft, not fail-closed: an over-ceiling host is still eligible when nothing
	// healthier is online, and an UNKNOWN ram (stale/absent telemetry) never
	// deprioritizes — honest-unknown, the same backward-compat posture as the
	// claude/bin probes.
	workerPlacementRamCeiling = 90.0
)

// ── boot context (worker-specific assembly — NEVER the member fold) ──────────

// buildWorkerBootContext assembles the worker persona: the worker_context.md
// seed (identity rules + task-lifecycle policy + boot procedure) followed by
// the concrete assignment — who the worker is, the bound task in full, and
// the type manual (Q1/Q2/Q3 + learnings). Deliberately NOT buildBootContext:
// a worker has no role doc, no lessons shard, no member boot sequence, and
// borrowing the member fold would drag all three in.
func (s *apiServer) buildWorkerBootContext(w OutsourceWorker, t Task, manual *TaskManual) (string, error) {
	seed, err := s.root.readSeedFile("worker_context.md")
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(seed))

	b.WriteString("\n\n---\n\n# 你的身分\n\n")
	fmt.Fprintf(&b, "- worker id（token sub、聊天定址都用它）：`%s`\n", w.ID)
	fmt.Fprintf(&b, "- 代號：%s\n", w.Codename)
	fmt.Fprintf(&b, "- 模型：%s\n", w.Model)
	fmt.Fprintf(&b, "- 投入度（effort）：%s\n", w.Effort)
	fmt.Fprintf(&b, "- 雇主（owner）聊天 id：`%s`\n", wireOwnerID)

	b.WriteString("\n# 你的任務（就這一張）\n\n")
	fmt.Fprintf(&b, "- 編號：%s（task id `%s`）\n", TaskNo(t.ID), t.ID)
	fmt.Fprintf(&b, "- 標題：%s\n", t.Title)
	// 顯示名為主、括號保留 type_key(T-fa76):the worker addresses the manual
	// by key (get_task_manual / write_task_learnings), so the key stays.
	typeLabel := t.TypeKey
	if manual != nil {
		typeLabel = manualDisplayLabel(manual.DisplayName, t.TypeKey)
	}
	fmt.Fprintf(&b, "- 類型：%s\n", typeLabel)
	fmt.Fprintf(&b, "- 優先權：%s\n", t.Priority)
	if t.DedupeKey != "" {
		fmt.Fprintf(&b, "- 識別鍵：%s\n", t.DedupeKey)
	}
	if len(t.Inputs) > 0 {
		b.WriteString("- 輸入欄位：\n")
		for _, name := range sortedKeys(t.Inputs) {
			fmt.Fprintf(&b, "  - %s: %v\n", name, t.Inputs[name])
		}
	}
	if strings.TrimSpace(t.Description) != "" {
		b.WriteString("\n## 任務描述\n\n")
		b.WriteString(strings.TrimSpace(t.Description))
		b.WriteString("\n")
	}

	// T-ba04: a task minted onto you while it is in `reassigning` is a TAKEOVER,
	// not a fresh assignment — you have a predecessor to hand over WITH. Print
	// who they are + the handover protocol up front, because a headless worker
	// reads chat only after it boots (the paired handover chat message is also
	// posted, but the boot context is what it sees first).
	if t.Lock == TaskLockReassigning && t.ReassignedFrom != "" {
		b.WriteString("\n## ⚠️ 你是接手這張任務（轉派交接）\n\n")
		fmt.Fprintf(&b, "這張任務目前掛著「轉派中」鎖（`reassigning` lock）——你是 owner 轉派後的**接手人**，"+
			"有一位**前任**在等你交接：\n")
		fmt.Fprintf(&b, "- 前任：%s（聊天 id `%s`）\n",
			s.executorLabel(t.ReassignedFromKind, t.ReassignedFrom), t.ReassignedFrom)
		b.WriteString("\n**接手序列（先交接、確認完成，才由你自己認領）：**\n")
		b.WriteString("1. 先 `post_chat` 給前任，問清楚目前進度、在飛事項、要注意的坑；來回確認到你有把握接得住。\n")
		b.WriteString("2. 確認交接完成後，**由你自己**呼叫 `claim_task`（認領）解除轉派鎖" +
			"——只有你這個新負責人動得了；server 不會自動幫你解。任務狀態一律照步驟推導，不必也不能自己報。\n")
		b.WriteString("3. 未完成的節點已被 server 退回「待辦」——照實況續推，或照常 `submit_plan` 重規劃" +
			"（已完成／已取代的節點會保留）。\n")
	}

	if manual != nil {
		fmt.Fprintf(&b, "\n# 任務手冊：%s\n",
			manualDisplayLabel(manual.DisplayName, manual.TypeKey))
		if strings.TrimSpace(manual.Purpose) != "" {
			b.WriteString("\n## 這是什麼任務（Q1）\n\n" + strings.TrimSpace(manual.Purpose) + "\n")
		}
		if fields, err := ParseManualFields(manual.Fields); err == nil && len(fields) > 0 {
			b.WriteString("\n## 需要哪些資訊（Q2）\n\n")
			for _, f := range fields {
				req := "選填"
				if f.Required {
					req = "必填"
				}
				key := ""
				if f.IsKey {
					key = "、識別鍵"
				}
				fmt.Fprintf(&b, "- %s（%s%s）\n", f.Name, req, key)
			}
		}
		if strings.TrimSpace(manual.SopMD) != "" {
			b.WriteString("\n## 該怎麼做（Q3 SOP——規劃 steps 的藍本）\n\n" +
				strings.TrimSpace(manual.SopMD) + "\n")
		}
		if strings.TrimSpace(manual.Learnings) != "" {
			b.WriteString("\n## 學習經驗（前人踩坑，先讀再動手）\n\n" +
				strings.TrimSpace(manual.Learnings) + "\n")
		}
	} else {
		// A worker only ever exists for a manual-typed task, but the manual
		// may have been deleted/renamed since assignment — say so honestly
		// rather than fabricating an empty manual.
		b.WriteString("\n# 任務手冊\n\n（該類型的手冊目前不存在——照任務描述與 owner 的指示規劃。）\n")
	}
	return b.String() + "\n", nil
}

// sortedKeys returns m's keys sorted — deterministic boot-context emission.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ { // tiny n: insertion sort, no new import
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// ── warden targeting ─────────────────────────────────────────────────────────

// pickWorkerWarden picks the warden (= machine) a worker session boots on,
// honouring the manual's spawn placement preference (spec TaskManualDTO
// `machine`, parsed by outsourceSpecOf into outsourceTypeSpec.Machine):
//
//   - preferred == a concrete machine id → spawn THERE when that warden is
//     currently online; when it is OFFLINE, fall back to "auto" automatically
//     (the spec-promised behaviour: 指定的機器若當下離線,會自動改用「自動分配」).
//   - preferred == "auto" (or "") → the IDLEST online machine: the one whose
//     live agent-session count (hub.AgentsOnMachine — the same per-machine
//     count the monitoring surface projects) is lowest; ties keep the prior
//     precedence (server-self first, then lexicographic).
//   - no online active warden → "" (fail-closed: the caller dispatches
//     nothing and the cadence retries).
//
// A worker has no durable machine binding by design — any online warden can
// host a temporary session; placement is decided HERE, at spawn time, where
// liveness is actually known (never at scheduler admission time).
//
// T-9ccf DoD②: the "auto" arm now folds two more spawn-time facts on top of
// idleness, so a repeatedly-failing placement rotates off the bad host:
//   - a machine benched for THIS worker (workerMachineCooldown — it just failed
//     to boot it) is SKIPPED outright; when EVERY online warden is benched the
//     pick returns "" and the worker waits, visible as spawn_state=stuck,
//     rather than hammering a known-bad host;
//   - among the rest, a broken-creds host (claude sub explicitly unreadable) and
//     a RAM-exhausted host (ram_pct over the ceiling) are DEPRIORITIZED — chosen
//     only when nothing healthier is online (soft, never fail-closed on unknown
//     telemetry), then idlest-first as before.
//
// Callers hold s.outsourceMu (the cooldown map read below shares it).
func (s *apiServer) pickWorkerWarden(w OutsourceWorker, preferred string, now float64) string {
	members, err := s.dal.ListMembers()
	if err != nil {
		return ""
	}
	type cand struct {
		id      string
		penalty int // creds-broken (2) + ram-high (1): lower is healthier
		load    int
	}
	var cands []cand
	for _, m := range members {
		if m.Kind != KindWarden || m.RosterStatus != RosterStatusActive ||
			!s.hub.IsOnline(m.ID) {
			continue
		}
		if s.workerMachineCoolingOn(w.ID, m.ID, now) {
			continue // benched for this worker after a recent boot failure — 換機
		}
		if preferred != "" && preferred != "auto" && m.ID == preferred {
			return m.ID // the requested machine is online + not benched — honour it
		}
		penalty := 0
		if _, _, subReadable := s.machineClaudeInfo(m.ID); subReadable != nil && !*subReadable {
			penalty += 2 // creds unreadable — a claude boot cannot even authenticate
		}
		if ram := s.machineRamPct(m.ID); ram != nil && *ram > workerPlacementRamCeiling {
			penalty += 1 // memory near-exhausted — the boot is likeliest to wedge here
		}
		cands = append(cands, cand{id: m.ID, penalty: penalty, load: s.agentLoadOn(m.ID)})
	}
	// "auto" (or a preferred host that is offline/benched — the fallback arm):
	// healthiest (lowest penalty), then idlest, then the stable order tie-break.
	best := cand{}
	haveBest := false
	for _, c := range cands {
		better := !haveBest || c.penalty < best.penalty ||
			(c.penalty == best.penalty && c.load < best.load) ||
			(c.penalty == best.penalty && c.load == best.load && wardenOrderBefore(c.id, best.id))
		if better {
			best, haveBest = c, true
		}
	}
	if !haveBest {
		return ""
	}
	return best.id
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

// machineRamPct is the freshest reported ram_pct for a warden host (its own
// telemetry entry's hardware fold — the same source the monitoring machine row
// projects). nil = honest unknown (no hardware telemetry yet).
func (s *apiServer) machineRamPct(machineID string) *float64 {
	if s.telemetry == nil {
		return nil
	}
	entry := s.telemetry.Get(machineID)
	if entry == nil {
		return nil
	}
	hw, _ := entry["hardware"].(map[string]any)
	if hw == nil {
		return nil
	}
	return teleNum(hw["ram_pct"])
}

// agentLoadOn counts the live agent SSE sessions machine wardenID currently
// hosts — the idleness metric for "auto" placement. The warden's OWN
// connection is excluded (it is transport, not workload). Since the A案 P1
// change, a worker token now DOES carry its host as the machine_id claim (see
// notifyWorkerSpawn), so a live worker session on wardenID counts toward its
// load here — the metric now sees workers as the workload they are, not the
// blind spot the claim-less token used to leave.
func (s *apiServer) agentLoadOn(wardenID string) int {
	n := 0
	for _, id := range s.hub.AgentsOnMachine(wardenID) {
		if id != wardenID {
			n++
		}
	}
	return n
}

// wardenOrderBefore is the load-tie-break order: the server-self warden (the
// deployment's always-there host) first, then lexicographic — exactly the
// pre-machine-preference precedence, so a tie changes nothing.
func wardenOrderBefore(a, b string) bool {
	if a == ServerSelfHost {
		return true
	}
	if b == ServerSelfHost {
		return false
	}
	return a < b
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
		return false // no live task to work — never boot a worker into a void
	}
	var manual *TaskManual
	if t.TypeKey != "" {
		if m, err := s.dal.GetTaskManual(t.TypeKey); err == nil {
			manual = m
		}
	}
	// Placement preference ("auto" | machine id) is consumed at SPAWN TIME —
	// here, where warden liveness is known — never at admission. Two owner
	// overrides feed it, in priority order (batch-land integration of T-160e
	// + T-f190): (1) the durable OWNER-PINNED desired_machine_id from a 改機器
	// relocate (T-f190) wins — the most recent explicit placement, and it
	// survives restart; (2) else the reassign dialog's machine pick carried in
	// workerMachinePref (T-160e, in-memory); (3) else the manual's assignee;
	// (4) else "auto". An unpinned, unreassigned worker behaves as before.
	machinePref := w.DesiredMachineID
	if machinePref == "" {
		machinePref = s.workerMachinePref[w.ID]
	}
	if machinePref == "" {
		machinePref = "auto"
		if manual != nil {
			if spec := outsourceSpecOf(*manual); spec != nil {
				machinePref = spec.Machine
			}
		}
	}
	warden := s.pickWorkerWarden(w, machinePref, now)
	if warden == "" {
		// No online warden, OR every online warden is benched for this worker
		// (all recently failed to boot it — DoD② 換機): honestly wait rather than
		// hammer a known-bad host; the worker stays visible as a non-online
		// presence (waking window elapsed → offline).
		outsourceLog("spawn %s (%s): no eligible warden — fail-closed, will retry",
			w.ID, w.Codename)
		return false
	}
	persona, err := s.buildWorkerBootContext(w, *t, manual)
	if err != nil {
		outsourceLog("spawn %s: boot-context fold failed: %v", w.ID, err)
		return false
	}
	if len(s.secret) == 0 {
		outsourceLog("spawn %s: no signing secret — fail-closed", w.ID)
		return false
	}
	// Server-mint (the auth authority — never the warden, never the worker):
	// sub == the ow- id; the authz resolver floors an unknown sub to the
	// agent class (contract §H), which is exactly a worker's ceiling. The
	// token rides ONLY this directed frame; the warden lands it in a 0600
	// workdir file — never a log, never chat, never a transcript.
	//
	// machine_id claim = `warden`, the machine this worker is ACTUALLY dispatched
	// to (server-picked — the resolved id even when machinePref was "auto"/""),
	// mirroring the member token (api_auth.go mintMemberToken burns
	// DesiredMachineID). So the worker's live SSE now projects hub.MachineOf ==
	// its host, the same WHERE an agent's does.
	token, err := s.mintAgentToken(w.ID, warden, s.authTokenTTL())
	if err != nil {
		outsourceLog("spawn %s: token mint failed: %v", w.ID, err)
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
			Model:          w.Model,
			Effort:         w.Effort,
		},
	})
	if err != nil {
		outsourceLog("spawn %s: frame build failed: %v", w.ID, err)
		return false
	}
	if !s.enqueueToWarden(w.ID, warden, frame) {
		// The picked warden dropped offline between pickWorkerWarden and the
		// enqueue — the same fail-closed reachability gate the member dispatch
		// sits behind. No pacing/row stamp: the next tick retries a fresh pick.
		return false
	}
	if s.workerStopPending[w.ID] == warden {
		// A fresh START just landed on the machine the parked kill targeted:
		// drop the parking so a late re-fire can never shoot the NEW session.
		// A still-lingering ghost there is the clobber-guard + FSM zombie-takeover
		// path's to clear, not the stale parked stop's.
		delete(s.workerStopPending, w.ID)
	}
	s.workerSpawnAt[w.ID] = now
	s.workerSpawnTarget[w.ID] = warden
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
		// DoD② 換機: this target kept a ghost the clobber-guard refused to
		// overwrite — bench it for this worker so the respawn rotates to a
		// different warden while the reap lands.
		s.benchWorkerMachine(w.ID, target, now)
		delete(s.workerSpawnAt, w.ID) // the respawn must not be throttled
		outsourceLog("rescue %s (%s): %s — robust stop → %s, %s benched",
			w.ID, w.Codename, decision.Reason, target, target)
	default:
		s.workerReconcileStates[w.ID] = decision.State
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
// Callers hold s.outsourceMu.
func (s *apiServer) relocateWorkerNow(w OutsourceWorker) {
	s.respawnWorkerNow(w, "relocate")
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
		outsourceLog("auto-handover deferred %s (%s): reason=%s — no kill target "+
			"(spawn memory empty, sse offline); no kill, no respawn — tick retries",
			w.ID, w.Codename, reason)
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
	pct := actionableContextPct(record, ctxhigh.StaleGuard)
	if bandFor(pct, ctxhigh.WarnPct, ctxhigh.HandoverPct) != levelHandover {
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
	outsourceLog("auto-handover %s (%s): context %.0f%% ≥ handover band — graceful refocus (grace opened)",
		w.ID, w.Codename, *pct)
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
// five-step handover SOP — the member machinery verbatim, zero client change)
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
		memberDeltaPayload(memberFromWorker(w)), audienceMembers(w.ID), trigger)
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
// recycle markers (the durable loop-break, member parity) and honour the model
// self-report. waking_since itself is NOT carried on the worker vocabulary —
// the worker's observable wake signal stays the get_my_task claim. Takes
// s.outsourceMu.
func (s *apiServer) workerReportWaking(id string, model *string, trigger string) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	w, err := s.resolveLiveWorker(id)
	if err != nil {
		return nil, err
	}
	w.RefocusSince = 0.0
	w.StoppingSince = 0.0
	w.StoppedSince = 0.0
	if model != nil {
		w.Model = *model
	}
	m := memberFromWorker(*w)
	if err := s.putMember(m, trigger); err != nil {
		return nil, err
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
// "結束後續已處理完" — the server then fires the outsource worker(s) bound to
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
}
