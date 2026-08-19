package main

// api_outsource.go — the 外包 panel read face (M3 contract §C.4) + the detail
// panel's runtime projection and 改機器 operation (T-f190). The panel is a live
// view of every NOT-yet-released worker joined to its one bound task (title +
// status); a task hitting a terminal state releases its worker (api_tasks.go
// closeTask) and the row drops off here — the DB row itself is the audit trail.
// Chat with a worker rides the EXISTING chat surface unchanged (the worker id
// is just a chat peer — contract §E, zero new wiring); worker minting/assignment
// is the Phase 2 scheduler's.
//
// T-f190 aligns the outsource DETAIL panel with the member detail panel: the DTO
// now folds the worker's REAL machine (last_spawn_target resolved), Claude
// account, context %, live cost, and last warden receipt — all from the SAME
// per-actor telemetry/gauge maps the member roster reads (keyed by actor id;
// see api_monitoring.go). The owner or admin agent can 改機器 via POST .../relocate,
// mirroring the member activate machine-bind; a single GET .../{id} backs the
// panel's post-relocate refresh.

import (
	"net/http"
	"strings"
)

// projectWorker builds one worker DTO with the T-f190 runtime fold. Shared by
// the list loop and the single GET so both serve the identical projection.
// tele/gauge are the snapshot maps (keyed by actor id); machineNames resolves a
// warden id to its owner-edited display label; accountDisplay is the shared
// raw→readable account fold (account_display.go — "" ⇒ the DTO serves null,
// never the raw credential key). Callers pass the worker's bound task (nil =
// honest empty). typeNames resolves the bound task's type_key to the manual's
// display label (T-a3e4; a miss leaves task_type_name "" and the client falls
// back to the raw key).
func (s *apiServer) projectWorker(
	worker OutsourceWorker, task *Task, unread int, now float64,
	tele, gauge map[string]map[string]any, machineNames map[string]string,
	accountDisplay func(string) string, typeNames map[string]string,
) outsourceWorkerDTO {
	spawnTarget, spawnAt := s.workerSpawnObs(worker.ID)
	// T-c23a: the spawn observation is IN-MEMORY (P7d fold) — a server re-exec
	// forgets it, and a HEALTHY live worker is never re-dispatched, so the
	// machine cell would read 「尚未分配」 forever while the session keeps
	// working. Fall back to the restart-proof observed host — the live SSE
	// machine claim, then the worker's self-reported telemetry `machine` —
	// the SAME precedence the member roster's observedHost fold and
	// resolveWorkerKillTarget already trust. Display-only: the identity-sweep
	// 正身 check keeps reading the strict dispatch memory (workerSpawnObs),
	// so no kill decision widens. "" when nothing is observed — the panel's
	// honest 「尚未分配」, never fabricated.
	machineObserved := spawnTarget
	if machineObserved == "" {
		machineObserved = s.observedWorkerHost(worker.ID, tele[worker.ID])
	}
	return newOutsourceWorkerDTO(worker, task, outsourceWorkerProjection{
		cfg:         s.reconcileCfg,
		unread:      unread,
		now:         now,
		online:      s.hub.IsOnline(worker.ID),
		tele:        tele[worker.ID],
		gaugeEntry:  gauge[worker.ID],
		spawnTarget: machineObserved,
		spawnAt:     spawnAt,
		machineDisplay: func(id string) string {
			if name := machineNames[id]; name != "" {
				return name
			}
			return id // honest fall-back to the raw id, never fabricated
		},
		accountDisplay: accountDisplay,
		delegatedBy:    s.workerDelegatedName(task),
		typeDisplay:    func(key string) string { return typeNames[key] },
	})
}

// taskTypeDisplayNames folds the manuals into type_key → display label, the
// resolution behind outsourceWorkerDTO.task_type_name (T-a3e4). ONE query for
// the whole response — the panel used to pull the entire manuals list itself
// just to translate one key per row. A manual with a blank display_name is
// omitted, so the client's raw-key fallback still applies (same rule the FE's
// own typeNames map used). Best-effort: a lookup fault degrades to an empty
// map — a missing label costs the raw key, never the response.
func (s *apiServer) taskTypeDisplayNames() map[string]string {
	out := map[string]string{}
	manuals, err := s.dal.ListTaskManuals()
	if err != nil {
		return out
	}
	for _, m := range manuals {
		if m.DisplayName != "" {
			out[m.TypeKey] = m.DisplayName
		}
	}
	return out
}

// workerDelegatedName resolves the MEMBER display name behind a task's creator,
// for the detail panel's 委託人 line (T-f190 item 2). Returns "" for the owner,
// an empty creator (pre-column / server-scheduled), or an unknown/removed
// member — the client then renders the owner label or an honest fallback from
// creator_id, NEVER a fabricated name. Best-effort: a lookup fault degrades to "".
func (s *apiServer) workerDelegatedName(task *Task) string {
	if task == nil || task.CreatorID == "" || task.CreatorID == wireOwnerID {
		return ""
	}
	if m, err := s.dal.GetMember(task.CreatorID); err == nil && m != nil {
		return m.Name
	}
	return ""
}

// GET /api/outsource-workers — live workers (assigned + active), each with
// its bound task's title and status, plus the CALLER's unread chat count for
// that worker's conversation (the same UnreadCounts watermark inverse the
// member roster serves — owner report 2026-07-14: 外包列也要有未讀紅點).
func (s *apiServer) HandleListOutsourceWorkersApiOutsourceWorkersGet(w http.ResponseWriter, r *http.Request) {
	actor := currentActor(r)
	workers, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		internalError(w, err)
		return
	}
	messages, err := s.dal.ListChat()
	if err != nil {
		internalError(w, err)
		return
	}
	receipts, err := s.dal.ListChatReads(actor, "")
	if err != nil {
		internalError(w, err)
		return
	}
	machineNames, err := s.dal.MachineDisplayNames()
	if err != nil {
		internalError(w, err)
		return
	}
	unread := UnreadCounts(messages, receipts, actor)
	// Runtime facts fold from the SAME per-actor maps the member session loop
	// reads (api_monitoring.go): telemetry (account/cost) + gauge (context_pct),
	// snapshot once for the whole list.
	tele := s.telemetry.Snapshot()
	gauge := s.gauge.Snapshot()
	accountDisplay, err := s.accountDisplayFold(r, tele)
	if err != nil {
		internalError(w, err)
		return
	}
	now := nowSecs()
	typeNames := s.taskTypeDisplayNames()
	out := []outsourceWorkerDTO{}
	for _, worker := range workers {
		if worker.Status == WorkerStatusReleased {
			continue
		}
		task, err := s.dal.GetTask(worker.TaskID)
		if err != nil {
			internalError(w, err)
			return
		}
		out = append(out, s.projectWorker(worker, task, unread[worker.ID], now, tele, gauge, machineNames, accountDisplay, typeNames))
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/outsource-workers/{id} — read ONE worker (the same projection the
// list serves), for the detail panel's post-relocate refresh (T-f190). 404 when
// the worker id is unknown (a released row still reads — the panel that reached
// it via a stale route renders 「已釋放」, never a blank).
func (s *apiServer) HandleGetOutsourceWorkerApiOutsourceWorkersIdGet(w http.ResponseWriter, r *http.Request, id string) {
	actor := currentActor(r)
	worker, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		internalError(w, err)
		return
	}
	if worker == nil {
		writeResolveError(w, errNotFound, "outsource worker", id)
		return
	}
	messages, err := s.dal.ListChat()
	if err != nil {
		internalError(w, err)
		return
	}
	receipts, err := s.dal.ListChatReads(actor, "")
	if err != nil {
		internalError(w, err)
		return
	}
	machineNames, err := s.dal.MachineDisplayNames()
	if err != nil {
		internalError(w, err)
		return
	}
	task, err := s.dal.GetTask(worker.TaskID)
	if err != nil {
		internalError(w, err)
		return
	}
	unread := UnreadCounts(messages, receipts, actor)
	tele := s.telemetry.Snapshot()
	gauge := s.gauge.Snapshot()
	accountDisplay, err := s.accountDisplayFold(r, tele)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK,
		s.projectWorker(*worker, task, unread[worker.ID], nowSecs(), tele, gauge, machineNames, accountDisplay,
			s.taskTypeDisplayNames()))
}

// GET /api/outsource-workers/{id}/boot-context — the worker detail panel's
// initial-prompt PREVIEW (T-ba6b), the worker twin of the member panel's
// POST /api/bootstrap {role} preview. Nothing is stored at spawn time (the
// persona rides the worker_start frame and is dropped — worker_spawn.go), so
// the server re-runs the SAME buildWorkerBootContext fold and returns the text
// — NO token is minted (parity with the member preview's no-member_id branch).
//
// 🔴 T-4595 CHANGED WHAT "PREVIEW" MEANS HERE, and the old caveat is now the
// wrong one. A worker's boot context is the staff fold minus the persona slot:
// it does NOT contain the bound task or the type manual any more, so it does
// not vary with them. The honest caveat is no longer "this is today's rows, not
// the spawn-time text" — it is that the SEEDS may have changed since spawn.
//
// The worker and its bound task are still resolved, because the 404 contract
// below is unchanged and is what tells the cockpit the row is stale; they are
// still handed to the fold so that reinstating any per-task text shows up here
// too rather than only on the spawn path.
//
// 404 for an unknown worker or a gone bound task; a RELEASED worker still reads
// (its rows are the audit trail, same as the single GET).
func (s *apiServer) HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet(w http.ResponseWriter, r *http.Request, id string) {
	worker, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		internalError(w, err)
		return
	}
	if worker == nil {
		writeResolveError(w, errNotFound, "outsource worker", id)
		return
	}
	task, err := s.dal.GetTask(worker.TaskID)
	if err != nil {
		internalError(w, err)
		return
	}
	if task == nil {
		writeResolveError(w, errNotFound, "task", worker.TaskID)
		return
	}
	// Manual is best-effort, and since T-4595 the fold does not render it at
	// all — it is resolved and passed so this preview keeps taking exactly the
	// same inputs the spawn path takes.
	var manual *TaskManual
	if task.TypeKey != "" {
		if m, err := s.dal.GetTaskManual(task.TypeKey); err == nil {
			manual = m
		}
	}
	context, err := s.buildWorkerBootContext(*worker, *task, manual)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WorkerBootContextDTO{Context: context})
}

// POST /api/outsource-workers/{id}/relocate — the cockpit's 改機器 for a worker
// (route Requires=admin_agent since P7c — 外包對齊正職, the exact member relocate
// floor). Writes the worker's pinned desired_machine_id, then re-spawns it onto
// the chosen machine using the SAME 殺舊 session + 清 pacing semantics
// the shared-FSM zombie-takeover uses (relocateWorkerNow), WITHOUT touching lifecycle (the
// worker stays assigned/active — a relocate is a placement change, not a state
// change). Returns the freshly-projected worker so the panel adopts the new pin
// immediately. 404 for an unknown / already-released worker (a released worker
// has no session to move). machine_id is REQUIRED since owner 2026-07-27
// (relocateNeedsMachineMsg): absent key ⇒ 422, explicit null / "" ⇒ 400.
func (s *apiServer) HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(w http.ResponseWriter, r *http.Request, id string) {
	var body OutsourceWorkerRelocateDTO
	if !decodeJSONBodyRequired(w, r, &body, "machine_id") {
		return
	}
	if body.MachineId == "" {
		writeError(w, http.StatusBadRequest, relocateNeedsMachineMsg)
		return
	}
	s.relocateWorkerByID(w, r, id, body.MachineId)
}

// relocateWorkerByID is the shared 改機器 core: validate the pin, persist it,
// kill+re-dispatch, respond with the fresh projection. Called by the worker
// route handler and by the member relocate fallback (relocate_member accepts a
// worker id — P7c), so both faces serve identical semantics.
func (s *apiServer) relocateWorkerByID(w http.ResponseWriter, r *http.Request, id, machineID string) {
	// machine_id must name a real machine — reject a hand-typed / stale id with
	// an honest 404 rather than pinning the worker to a placement that can never
	// boot. "auto" is no longer exempt: waving it through pinned the worker to a
	// pseudo-machine dispatch could never reach, the same hole a nonexistent
	// concrete id was already 404'd for. "" no longer clears the pin either
	// (owner 2026-07-27, relocateNeedsMachineMsg); both callers refuse it before
	// they get here, and this arm keeps the core fail-closed on its own.
	if machineID == "" {
		writeError(w, http.StatusBadRequest, relocateNeedsMachineMsg)
		return
	}
	if _, err := s.resolveMachine(machineID); err != nil {
		writeResolveError(w, err, "machine", machineID)
		return
	}

	s.outsourceMu.Lock()
	worker, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if worker == nil || worker.Status == WorkerStatusReleased {
		s.outsourceMu.Unlock()
		writeResolveError(w, errNotFound, "outsource worker", id)
		return
	}
	worker.DesiredMachineID = machineID
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	s.relocateWorkerNow(*worker)
	// Re-read the row so the response reflects the spawn stamp relocateWorkerNow
	// wrote (last_spawn_target = the new machine) — not the pre-dispatch row.
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}

// writeWorkerProjection re-reads the per-request join maps (machine names, bound
// task, the caller's unread count, the telemetry/gauge snapshots) and writes the
// worker DTO — the shared post-op response fold for every owner lifecycle op
// (relocate / refocus / stop / restart / model), so all serve the identical
// projection the list + single GET do. Call WITHOUT s.outsourceMu held.
func (s *apiServer) writeWorkerProjection(w http.ResponseWriter, r *http.Request, worker OutsourceWorker) {
	machineNames, err := s.dal.MachineDisplayNames()
	if err != nil {
		internalError(w, err)
		return
	}
	task, err := s.dal.GetTask(worker.TaskID)
	if err != nil {
		internalError(w, err)
		return
	}
	actor := currentActor(r)
	messages, err := s.dal.ListChat()
	if err != nil {
		internalError(w, err)
		return
	}
	receipts, err := s.dal.ListChatReads(actor, "")
	if err != nil {
		internalError(w, err)
		return
	}
	unread := UnreadCounts(messages, receipts, actor)
	tele := s.telemetry.Snapshot()
	accountDisplay, err := s.accountDisplayFold(r, tele)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.projectWorker(
		worker, task, unread[worker.ID], nowSecs(),
		tele, s.gauge.Snapshot(), machineNames, accountDisplay,
		s.taskTypeDisplayNames()))
}

// POST /api/outsource-workers/{id}/refocus — the cockpit's 換手 (owner/admin agent since T-6020,
// route Requires=owner). The worker twin of refocus_member, member-shaped since
// T-ea82: stamp refocus_since + fan the SOP 預告 at the worker's own session
// (openWorkerHandoverGrace) and RETURN — the kill+respawn is owned by the 收口
// drivers (the worker's report_stopped, the 120s grace deadline, or the offline
// fallback inside the grace open), so a live worker gets to flush its handoff
// (step notes / learnings / baton) before the session is taken. ONLINE-ONLY
// (409 otherwise — a context handover is meaningless with no live session, the
// exact member gate); 404 for an unknown / released worker; 409 for a stopped
// worker (restart it first). The refocus_since marker doubles as the tick's
// auto-handover cooldown and is cleared by the loop-break once the respawn lands.
func (s *apiServer) HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost(w http.ResponseWriter, r *http.Request, id string) {
	s.outsourceMu.Lock()
	worker, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if worker == nil || worker.Status == WorkerStatusReleased {
		s.outsourceMu.Unlock()
		writeResolveError(w, errNotFound, "outsource worker", id)
		return
	}
	if worker.DesiredState == DesiredStateOffline {
		s.outsourceMu.Unlock()
		writeError(w, http.StatusConflict,
			"refocus requires a live worker — this one is stopped (restart it first)")
		return
	}
	if worker.Status != WorkerStatusActive || !s.hub.IsOnline(worker.ID) {
		s.outsourceMu.Unlock()
		writeError(w, http.StatusConflict,
			"refocus requires the worker to be online (no live session to hand over)")
		return
	}
	worker.RefocusSince = nowSecs()
	worker.RefocusOp = refocusOpRefocus
	worker.StoppingSince = 0.0 // a new handover epoch never inherits a stale latch
	worker.StoppedSince = 0.0
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	// Graceful flush (T-ea82): 預告 only — no synchronous kill. When the online
	// gate raced a disconnect, the grace open itself falls back to the immediate
	// kill+respawn (nothing can hear the 預告).
	s.openWorkerHandoverGrace(*worker, requestTrigger(r))
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}

// POST /api/outsource-workers/{id}/stop — the cockpit's 停止 (owner/admin agent since T-6020). The
// worker twin of a member deactivate: set desired_state="offline" (a DIRECT mirror
// of member.desired_state, which makes every scheduler auto-spawn branch skip the
// worker — stuck-recovery and the paced re-dispatch must NOT quietly revive an
// owner-held-down worker), clear any in-flight refocus (an explicit stop supersedes
// a handover), and kill the session WITHOUT re-dispatching. The bound task stays in
// its own status — a stop pauses the worker, it does not close or reassign the task.
// Idempotent (re-stopping stays offline and re-kills harmlessly). 404 unknown/released.
func (s *apiServer) HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost(w http.ResponseWriter, r *http.Request, id string) {
	s.outsourceMu.Lock()
	worker, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if worker == nil || worker.Status == WorkerStatusReleased {
		s.outsourceMu.Unlock()
		writeResolveError(w, errNotFound, "outsource worker", id)
		return
	}
	worker.DesiredState = DesiredStateOffline // owner-explicit stop intent (member parity)
	worker.RefocusSince = 0.0                 // an explicit stop supersedes any in-flight handover
	worker.RefocusOp = ""                     // …and its cause goes with it
	// 🔴 This verb is the FORCED shape, not the graceful one (T-c996). It kills
	// the session below with no grace, no warning and no 預告 — exactly what
	// 強制下線 does to a staff member — so it stamps the same two anchors that
	// handler stamps, and for the same two reasons: forced_stop_at is what makes
	// offboardKindOf stay silent (the owner's ruling that a session about to
	// stop existing is told nothing), and it is the only durable record that
	// this session was cut off rather than collected — without it, what a killed
	// worker leaves behind is indistinguishable from what a worker with nothing
	// to say leaves behind.
	//
	// Both anchors, together: forcedEpochLive scopes the record to a LIVE epoch
	// by requiring forced_stop_at >= stopping_since, so stamping one without the
	// other leaves a worker that had already announced its own wind-down
	// (report_stopping) still reading as "working its close-out" — which is the
	// arm that speaks.
	forcedAt := nowSecs()
	worker.ForcedStopAt = forcedAt
	if worker.StoppingSince <= 0.0 || worker.StoppingSince > forcedAt {
		worker.StoppingSince = forcedAt
	}
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	s.stopWorkerNow(*worker)
	// Re-read so the response/delta carry the cost the stop just banked.
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}

// POST /api/outsource-workers/{id}/restart — the cockpit's 重啟 (owner/admin agent since T-6020),
// the inverse of stop: set desired_state back to "online" and re-dispatch (重啟 =
// 再 dispatch — a fresh worker_start onto the pinned / preferred machine). 409 ONLY
// when the worker is actually ALIVE (T-7526: not held down AND online — nothing to
// restart, and refusing there is what stops a hidden double-spawn). A worker whose
// session died on its own keeps desired_state=online and IS restartable; 404
// unknown/released.
func (s *apiServer) HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost(w http.ResponseWriter, r *http.Request, id string) {
	s.outsourceMu.Lock()
	worker, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if worker == nil || worker.Status == WorkerStatusReleased {
		s.outsourceMu.Unlock()
		writeResolveError(w, errNotFound, "outsource worker", id)
		return
	}
	// 🔴 The guard asks "is it ALIVE?", not "did anyone press STOP?" (T-7526).
	// It used to be `DesiredState != offline` alone — pure INTENT — so a worker
	// whose session died on its own still carried desired_state=online and the
	// ONE endpoint that could bring it back answered 409. Nothing else could
	// either (the panel's restart affordance keys off presence, which reads
	// offline there), so the owner had no way to revive it at all.
	// Both halves are load-bearing: a genuinely live worker is still refused, so
	// restart can never become a hidden double-spawn.
	if worker.DesiredState != DesiredStateOffline && s.hub.IsOnline(id) {
		s.outsourceMu.Unlock()
		writeError(w, http.StatusConflict, "worker is still online — nothing to restart")
		return
	}
	worker.DesiredState = DesiredStateOnline
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	s.respawnWorkerForOwnerOp(*worker, ownerOpRestart)
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}

// POST /api/outsource-workers/{id}/model — the owner cockpit's runtime/model
// edit (owner/admin agent since T-6020), the worker twin of the member runtime/model/effort edit.
// Persist the new values; when the worker is ACTIVE + online, kill+respawn
// so the new model takes effect NOW, otherwise (assigned / stopped) only persist —
// the next spawn / restart bakes it in ("active 時 kill+respawn 立即生效, assigned 時
// 下次 spawn 生效"). 404 unknown/released.
func (s *apiServer) HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost(w http.ResponseWriter, r *http.Request, id string) {
	var body OutsourceWorkerModelDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	s.outsourceMu.Lock()
	worker, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if worker == nil || worker.Status == WorkerStatusReleased {
		s.outsourceMu.Unlock()
		writeResolveError(w, errNotFound, "outsource worker", id)
		return
	}
	if body.Model != nil {
		worker.Model = strings.TrimSpace(*body.Model) // blank ⇒ launcher default
	}
	if body.Runtime != nil {
		runtime := string(*body.Runtime)
		if !ValidRuntime(runtime) {
			s.outsourceMu.Unlock()
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
		worker.Runtime = runtime
	}
	if body.Effort != nil {
		effort := strings.TrimSpace(*body.Effort)
		if !validEffort(effort) {
			s.outsourceMu.Unlock()
			writeError(w, http.StatusUnprocessableEntity,
				"effort must be one of [high low max medium]; got '"+effort+"'")
			return
		}
		worker.Effort = effort
	}
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	// Take effect immediately only for a LIVE session; an assigned worker adopts
	// the new model at its next spawn. Whether the owner wants it running at all
	// is deliberately NOT re-asked here — respawnWorkerForOwnerOp owns that single
	// branch point for all three owner verbs, and asking twice is how the two
	// copies drift (this one used to skip silently, leaving no receipt).
	if worker.Status == WorkerStatusActive && s.hub.IsOnline(worker.ID) {
		s.respawnWorkerForOwnerOp(*worker, ownerOpModel)
		if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
			worker = fresh
		}
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}
