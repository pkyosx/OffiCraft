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
	spawnTarget, _ := s.workerSpawnObs(worker.ID)
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
		cfg:         s.reconcileConfigLive(),
		unread:      unread,
		now:         now,
		online:      s.hub.IsOnline(worker.ID),
		tele:        tele[worker.ID],
		gaugeEntry:  gauge[worker.ID],
		spawnTarget: machineObserved,
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
// floor). Writes the worker's pinned desired_machine_id, then puts it through
// relocateWorkerNow → respawnWorkerForOwnerOp, WITHOUT touching lifecycle (the
// worker stays assigned/active — a relocate is a placement change, not a state
// change). Returns the freshly-projected worker so the panel adopts the new pin
// immediately.
//
// 🔴 IT DOES NOT KILL THE SESSION HERE, and this comment used to say it did.
// Since T-98f4 a LIVE worker with anything to flush gets the graceful wind-down:
// it keeps running ON THE OLD MACHINE until its own report_stopped (or the
// owner's force-stop), and the kill+respawn onto the new pin happens at that
// 收口. The immediate 殺舊 session + 清 pacing + 重生 path is what a worker with
// nothing to flush takes. The old sentence described the verb this endpoint had
// BEFORE that change; it is retracted here rather than deleted, because the same
// claim also stood on the wire (spec/openapi.json) and in the MCP tool list, and
// a reader who met it there should be able to find where it was withdrawn. 404 for an unknown / already-released worker (a released worker
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
	// The ow- row IS a member row (00025 folded the table), so the worker pin
	// goes through the SAME sole writer as the staff pin (T-55) — PutMember's
	// SET list no longer carries desired_machine_id, and PutOutsourceWorker is
	// PutMember. Without this line the relocate would answer 200 and move
	// nothing.
	if err := s.dal.SetMemberDesiredMachineID(worker.ID, machineID); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	outcome := s.relocateWorkerNow(*worker)
	// Re-read the row so the response reflects the spawn stamp relocateWorkerNow
	// wrote (last_spawn_target = the new machine) — not the pre-dispatch row.
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	// The pin always lands (persisted above), so a relocate never FAILS on
	// dispatch — but we OBSERVE it, the staff relocate's rule verbatim (T-8655 /
	// T-927a). Two different non-landings answered the same clean 200 here: a
	// wind-down opened by design, and a move that could not be dispatched at all.
	s.writeWorkerProjectionWith(w, r, *worker, func(dto *outsourceWorkerDTO) {
		if outcome.Pending() {
			pending := true
			dto.RelocationPending = &pending
		}
		if outcome.WoundDown {
			deferred := true
			dto.RelocationDeferred = &deferred
		}
	})
}

// writeWorkerProjection re-reads the per-request join maps (machine names, bound
// task, the caller's unread count, the telemetry/gauge snapshots) and writes the
// worker DTO — the shared post-op response fold for every owner lifecycle op
// (relocate / refocus / stop / restart / model), so all serve the identical
// projection the list + single GET do. Call WITHOUT s.outsourceMu held.
func (s *apiServer) writeWorkerProjection(w http.ResponseWriter, r *http.Request, worker OutsourceWorker) {
	s.writeWorkerProjectionWith(w, r, worker, nil)
}

// writeWorkerProjectionWith is writeWorkerProjection plus the RESPONSE-ONLY
// pending flags an owner verb owes its caller (T-ed79 #5/#12). `overlay` is nil
// for every read face and for the verbs that have nothing to defer; the flags it
// sets are never persisted and never appear on a list/GET, exactly as their
// MemberDTO twins do not.
func (s *apiServer) writeWorkerProjectionWith(w http.ResponseWriter, r *http.Request,
	worker OutsourceWorker, overlay func(*outsourceWorkerDTO)) {
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
	dto := s.projectWorker(
		worker, task, unread[worker.ID], nowSecs(),
		tele, s.gauge.Snapshot(), machineNames, accountDisplay,
		s.taskTypeDisplayNames())
	if overlay != nil {
		overlay(&dto)
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/outsource-workers/{id}/refocus — the cockpit's 換手 (owner/admin agent since T-6020,
// route Requires=owner). The worker twin of refocus_member, member-shaped since
// T-ea82: stamp refocus_since + fan the SOP 預告 at the worker's own session
// (openWorkerHandoverGrace) and RETURN — the kill+respawn is owned by the 收口
// drivers, which for THIS handler are exactly TWO: the worker's report_stopped,
// (T-72dd: the offline fallback that used to be named here is gone — an offline
// worker has no session to collect). 🔴 There is NO grace deadline
// on this path — it stamps refocusOpRefocus below, and 重新聚焦 runs no clock
// (winddownKindFor). This used to name "the 120s grace deadline" as a third
// driver: a driver that does not exist here, and precisely the one an owner
// would sit and wait for. So a live worker gets to flush its handoff
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
	// 🔴 THIS USED TO BE A FLAT 409 「refocus requires a live worker — this one is
	// stopped (restart it first)」, and T-65 包② is where that refusal is paid off.
	// Owner 2026-08-30 (rc-bc1b029a3aa2): 「一個重啟的 intention 遇上一個更強硬的
	// 下線規則 他的方式是沿用強硬下線規則 但是附加上線規則」. The refocus STAMP
	// genuinely would not reach the agent — there is no session to hand over —
	// but that was never a reason to refuse the OWNER'S intent, only a reason not
	// to write it as a refocus epoch. So the stop keeps its stage and all four of
	// its anchors, and the only thing recorded is 「起來」.
	//
	// The 409 SURVIVES for the one case the queue cannot serve: a worker nobody
	// has ever asked to stop (aStopWasEverAskedFor, inside the queue helper) has
	// no 下線 for an 上線 rule to be added to.
	if worker.DesiredState == DesiredStateOffline {
		if !s.queueWorkerRestartAfterStop(worker, refocusOpRefocus, nowSecs()) {
			s.outsourceMu.Unlock()
			writeError(w, http.StatusConflict,
				"refocus requires a live worker — this one is stopped and has never "+
					"been asked to stop, so there is no wind-down for a 起來 to be "+
					"queued behind (重啟 it when you want it to run)")
			return
		}
		if err := s.persistWorkerRestartIntent(*worker); err != nil {
			s.outsourceMu.Unlock()
			internalError(w, err)
			return
		}
		s.publishOutsourceWorker(*worker, requestTrigger(r))
		s.outsourceMu.Unlock()
		// The worker may ALREADY be converged offline (a stop that landed before
		// the owner pressed this), in which case the queued start is spendable on
		// this very tick rather than up to a cadence later — the staff face's
		// reconcileMemberNow, one population along. AFTER the unlock: the tick
		// takes outsourceMu itself.
		s.outsourceTickNow()
		if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
			worker = fresh
		}
		s.writeWorkerProjection(w, r, *worker)
		return
	}
	if worker.Status != WorkerStatusActive || !s.hub.IsOnline(worker.ID) {
		s.outsourceMu.Unlock()
		writeError(w, http.StatusConflict,
			"refocus requires the worker to be online (no live session to hand over)")
		return
	}
	// 🔴 The ladder only goes forward (owner, 2026-08-24), and this site is the
	// reason the guard cannot live in the owner-verb funnel alone: 換手 does NOT
	// go through respawnWorkerForOwnerOp — it is a FOURTH stamp site, and it used
	// to hand-write the same four fields the funnel used to. 重新聚焦 is 停止 —
	// stage 1 — so pressing it on a worker already in 加速停止 pushed the stage
	// BACK and cleared the deadline with it, leaving a worker that had been told
	// it was counting down no longer counting. Refused rather than silently
	// downgraded, exactly as HandleRefocusMember refuses it for staff: the owner
	// pressed a button, so he gets an answer.
	//
	// Stamped through the SHARED armRefocusEpoch on a memberFromWorker projection
	// (a worker row IS a member row, and the projection carries all five fields
	// this decision reads), with only the four it mutates folded back. A
	// hand-written copy of a shared decision stays equal to it exactly until
	// somebody edits the original — which is what happened here.
	proj := memberFromWorker(*worker)
	if !armRefocusEpoch(&proj, refocusOpRefocus, nowSecs()) {
		s.outsourceMu.Unlock()
		writeError(w, http.StatusConflict,
			"refocus is 停止 and this worker is already further along the "+
				"wind-down ladder (下線 → 加速 → 強制); a later stage is never "+
				"replaced by an earlier one")
		return
	}
	worker.RefocusSince = proj.RefocusSince
	worker.RefocusOp = proj.RefocusOp
	worker.StoppingSince = proj.StoppingSince // a new epoch never inherits a stale latch
	worker.StoppedSince = proj.StoppedSince
	if err := s.persistWorkerWindDownAnchors(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
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

// POST /api/outsource-workers/{id}/accelerated-stop — the symmetric twin of the
// member 加速停止 (T-ed79, owner 2026-08-21 「停止 → 加速停止 → 強制停止」).
//
// 🔴 IT NOW COVERS BOTH ARMS, because since T-ed79 the worker's 停止 is itself a
// close-out that waits (see the /stop handler below). It used to 409 on
// desired_state=offline and say so in its own comment — correct while 停止 killed
// on the spot, and a DEAD MIDDLE RUNG the moment it stopped doing that: the owner
// would have had 停止 → (409) → 強制停止, i.e. no rung between "wait forever" and
// "cut it off". The two arms are exactly the member twin's:
//
//   - 下線 (desired_state=offline + stopping_since): re-stamp stopping_since from
//     THIS press and write the cause. autoHandoverWorker's stop arm then collects
//     at stopping_since + the grace, and offboardKindOf answers `final` off the
//     same refocus_op, so the sentence quotes exactly that instant.
//   - 換手 (desired online + refocus_since): re-stamp refocus_since and write the
//     cause — the promotion shape the context arm already uses.
//
// A force-stopped epoch is refused on both arms: that session was cut off
// deliberately and is not working a close-out, so a deadline addressed to it has
// no reader.
//
// The other gates mirror the worker refocus above cell for cell (released/unknown
// → 404; not active-and-online → 409) plus the escalation gate the member twin
// carries: no open epoch → 409, because an escalation with nothing to escalate is
// a mistake and not a stop.
//
// The anchor is re-stamped from THIS press for the reason the member arm
// documents: the deadline is anchor + grace, so promoting in place would quote an
// instant already gone. The OTHER wind-down anchors are deliberately NOT cleared
// — unlike the refocus handler above, which opens a NEW epoch, this promotes the
// one in flight, and zeroing stopped_since would erase a worker's own "I am
// done".
// acceleratedStopWorkerNeedsAnOpenWindDownMsg names the rung BELOW this one, for
// the reason its member twin does: a 409 that only says "no" leaves the owner
// guessing which of three buttons he was supposed to press first. It names both
// openers because a worker has two (停止 and 重新聚焦), and both are real.
const acceleratedStopWorkerNeedsAnOpenWindDownMsg = "加速停止 escalates a wind-down " +
	"that is already open — this worker has not been asked to stop. Press 停止 or " +
	"重新聚焦 first"

func (s *apiServer) HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost(w http.ResponseWriter, r *http.Request, id string) {
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
	if worker.Status != WorkerStatusActive || !s.hub.IsOnline(worker.ID) {
		s.outsourceMu.Unlock()
		writeError(w, http.StatusConflict,
			"加速停止 requires the worker to be online (no live session to accelerate)")
		return
	}
	switch {
	case worker.DesiredState == DesiredStateOffline:
		if !gracefulStopEpochOpen(memberFromWorker(*worker)) {
			s.outsourceMu.Unlock()
			writeError(w, http.StatusConflict, acceleratedStopWorkerNeedsAnOpenWindDownMsg)
			return
		}
		worker.StoppingSince = nowSecs()
	case worker.RefocusSince > 0.0:
		worker.RefocusSince = nowSecs()
	default:
		s.outsourceMu.Unlock()
		writeError(w, http.StatusConflict, acceleratedStopWorkerNeedsAnOpenWindDownMsg)
		return
	}
	worker.RefocusOp = refocusOpAcceleratedStop
	// 後蓋前 (T-65 包②). 加速停止 is a 下線 verb, so it cancels a queued 起來 like
	// the other two — and the ladder it advances is left alone, which is the split
	// the owner's ruling turns on: 「下線用多強」 is a ratchet, 「要不要起來」 is
	// last-writer-wins, and only the second is touched here.
	clearWorkerRestartIntent(worker)
	if err := s.persistWorkerWindDownAnchors(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	// 🔴 THE FINAL SENTENCE NEEDS ITS OWN FRAME, and this call is the only thing
	// that fans one. publishOutsourceWorker below is the OWNER cockpit's patch —
	// audience owner-only, payload {id, codename, status} — so it reaches the
	// worker's stream never and carries offboard_notice never. There is no
	// worker-side putMember re-fanning offboardDeltaPayload on every write, which
	// is the property the member promotion actually relies on; the worker's only
	// fan-out of the 預告 is right here. Without it this press starts the
	// autoHandoverWorker clock ("stop-accelerated-deadline") while the last thing
	// the worker heard was the 停止 SOFT sentence — a clock with no sentence,
	// which is the exact harm this ticket exists to remove.
	//
	// Same call the 停止 opener makes, for the same reasons: it re-reads liveness
	// itself, so a disconnect racing the online gate above lands on the collect
	// that matches the persisted intent instead of a respawn.
	s.openWorkerHandoverGrace(*worker, requestTrigger(r))
	// Re-read so the response/delta carry whatever the grace open banked.
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}

// POST /api/outsource-workers/{id}/stop — the cockpit's 停止 (owner/admin agent
// since T-6020), and since T-ed79 a GRACEFUL CLOSE-OUT rather than a kill
// (owner 2026-08-21 「往正職靠：外包那顆改成優雅停止，強制殺移到第三顆按鈕」).
//
// It is the worker twin of a member deactivate, and now that is true of what it
// DOES and not merely of what it writes:
//
//   - desired_state="offline" — a DIRECT mirror of member.desired_state, which
//     makes every scheduler auto-spawn branch skip the worker (stuck-recovery
//     and the paced re-dispatch must NOT quietly revive an owner-held-down
//     worker). Written FIRST, and it is what makes everything below safe: the
//     collect at the end of this close-out kills without re-spawning.
//   - stopping_since — the stop epoch's anchor. It is what offboardKindOf's
//     desired-offline arm reads to attach the SOFT 〈停止〉 notice to the delta,
//     so this stamp is the whole reason the worker hears anything at all.
//   - NO forced_stop_at. That anchor belongs to 強制停止 (below), and both of
//     its reasons are false here: it exists to keep the notice SILENT (this verb
//     needs the notice to arrive) and to record that a session was CUT OFF (this
//     session is being asked to close itself out).
//   - NO kill. openWorkerHandoverGrace fans the member-topic 預告 at the
//     worker's OWN session — the same machinery the 換手 arm has used since
//     T-ea82, client-side unchanged — and the 收口 belongs to the worker's own
//     report_stopped (workerReportStopped's stop arm). There is NO deadline
//     unless the owner presses 加速停止, exactly as on the staff 下線 arm
//     (rc-27d1710174dd 「不要兜底」).
//
// 🔴 THE CLEARED REFOCUS IS THE SAME LINE WITH A DIFFERENT MEANING. It used to
// be "an explicit stop supersedes a handover" — the stop threw the close-out
// away and killed. Since 停止 IS a close-out, nothing is superseded: the worker
// keeps working the same 〈停止〉 it was already working, and all that changes
// is that no new session follows it. The epoch is still cleared, and now for a
// mechanical reason instead of a semantic one: autoHandoverWorker's in-flight
// arm collects a refocus epoch by kill+RESPAWN, which would revive a worker the
// owner just held down.
//
// An OFFLINE worker takes the immediate kill instead (the D6 rule
// openWorkerHandoverGrace already applies to every other arm): no session can
// hear the 預告, so a window would only park a dead worker forever.
//
// The bound task stays in its own status — a stop pauses the worker, it does not
// close or reassign the task. Idempotent (re-stopping stays offline and re-opens
// nothing). 404 unknown/released.
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
	worker.RefocusSince = 0.0                 // see the 🔴 note above — mechanical, not semantic
	worker.RefocusOp = ""                     // …and its cause goes with it
	// 後蓋前 (T-65 包②): a 起來 queued by an earlier 重新聚焦 / 改機器 / 換 model
	// is CANCELLED by this press. Without it the tick would bring the worker back
	// up moments after this stop lands, which is the exact negative control the
	// owner's ruling turns on.
	clearWorkerRestartIntent(worker)
	// The stop epoch's anchor. NOT "the member deactivate's rule verbatim" any
	// more — it is that rule, the same function the staff deactivate calls
	// (stopEpochAnchor, api_members.go), which is where the reason a forced
	// epoch's anchor must not move is written down.
	worker.StoppingSince = stopEpochAnchor(memberFromWorker(*worker), nowSecs())
	if err := s.persistWorkerWindDownAnchors(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	// 預告 + wait (online), or the immediate kill (offline — nothing can hear
	// it). openWorkerHandoverGrace re-reads liveness itself and routes a
	// desired-offline worker to the stop collect, so the race between this
	// handler and a disconnect cannot end in a respawn.
	s.openWorkerHandoverGrace(*worker, requestTrigger(r))
	// Re-read so the response/delta carry whatever the grace open banked.
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}

// POST /api/outsource-workers/{id}/force-stop — the THIRD rung of the owner's
// escalation 停止 → 加速停止 → 強制停止, and the worker twin of
// HandleForceStopMember (T-ed79, owner 2026-08-21 「強制殺移到第三顆按鈕」).
//
// This is the body the /stop verb used to have, moved to its own button rather
// than removed: it stamps forced_stop_at + stopping_since and kills the session
// on the spot. Both anchors, together, because forcedEpochLive scopes the record
// to a LIVE epoch by requiring forced_stop_at >= stopping_since — stamping one
// without the other leaves a worker that had already announced its own wind-down
// (report_stopping) still reading as "working its close-out", which is the arm
// that speaks (T-c996).
//
// It sends NOTHING: the recipient is about to stop existing, so a sentence meant
// to change its behaviour has no reader — the same ruling api_members.go
// enforces for staff, and forced_stop_at is what enforces it here.
//
// Idempotent (re-forcing stays offline and re-kills harmlessly). 404
// unknown/released. No online gate: a worker whose session is already gone still
// needs its intent held down and its record written.
func (s *apiServer) HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost(w http.ResponseWriter, r *http.Request, id string) {
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
	worker.DesiredState = DesiredStateOffline
	worker.RefocusSince = 0.0 // nothing is being waited for any more
	worker.RefocusOp = ""
	clearWorkerRestartIntent(worker) // 後蓋前 — 重新聚焦 → 強制停止 ends DOWN (T-65 包②)
	forcedAt := nowSecs()
	worker.ForcedStopAt = forcedAt
	if worker.StoppingSince <= 0.0 || worker.StoppingSince > forcedAt {
		worker.StoppingSince = forcedAt
	}
	if err := s.persistWorkerWindDownAnchors(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	s.stopWorkerNow(*worker)
	// Re-read so the response/delta carry the cost the kill just banked.
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}

// POST /api/outsource-workers/{id}/restart — the cockpit's 重啟 (owner/admin agent since T-6020),
// the inverse of stop: set desired_state back to "online" and re-dispatch (重啟 =
// 再 dispatch — a fresh worker_start onto the pinned / preferred machine). It NEVER
// 409s: the old "409 when the worker is actually ALIVE" over-spawn guard is GONE
// (T-ed79 #10 — see the 🔴 note in the body below), and a live worker now gets a
// session_alive RECEIPT and a 200 instead. There is also NO desired-offline gate,
// so 重啟 on a worker that is mid-加速停止 answers 200 too. A worker whose session
// died on its own keeps desired_state=online and IS restartable; 404
// unknown/released is the only refusal this handler writes (a store failure still
// answers 500).
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
	// 🔴 THE OVER-SPAWN GUARD IS GONE (T-ed79 #10, owner 2026-08-21 「往正職靠：
	// 外包也不擋」). It used to 409 a worker that was still ALIVE — first on pure
	// INTENT (`DesiredState != offline`, T-7526 corrected that to liveness), then
	// on liveness. Neither test has a staff twin: 活化 on a live member is simply
	// honoured, and the owner ruled the two verbs must behave the same.
	//
	// WHAT MAKES THAT SAFE is the ORDER, not the guard: respawnWorkerForOwnerOp
	// →	respawnWorkerNow kills the current session BEFORE it dispatches the
	// fresh one, so "restart a live worker" is a displacement, never a second
	// copy. Behind that, the warden's own local clobber-guard refuses to stomp a
	// tmux session that is still there (cli/ocwarden/spawn.go), so even a kill
	// that has not taken effect yet cannot end in two live sessions.
	//
	// 🔴 WHAT THE OWNER WOULD OTHERWISE HAVE LOST is the SENTENCE. The 409 told
	// him something true and actionable; without it a restart pressed on a live
	// worker would surface, if anything, as a warden-level "session_already_exists"
	// bounce. That is the exact diagnosis-free blank #4/#12/#14 of this same
	// ticket exist to remove, so the fact becomes a RECEIPT in the same
	// reason-code family instead — stamped only when it is TRUE of this worker,
	// and cleared by the landed START (spawnBlockedReasonCodes) so it never
	// outlives the restart it describes.
	sessionAliveReceipt := s.hub.IsOnline(id)
	if sessionAliveReceipt {
		// Stamped onto the in-memory row rather than written through
		// stampWorkerPlacementBlocked: that helper re-reads and writes on its own,
		// and this handler's own write would then race it.
		// ⚠️ NOT one write any more, and it is no longer the rule every owner verb
		// here follows: since T-55 the receipt columns land through
		// SetMemberOpReceipt below, and the 換 model verb stores its three launch
		// intents through their own setters (see the 🔴 block there).
		stampWorkerOpReceipt(worker, spawnReasonSessionAlive+
			": this worker was still running — 重啟 is replacing that session, not "+
			"starting a first one. If it does not come back, its previous session "+
			"was still holding the slot", nowSecs())
	}
	worker.DesiredState = DesiredStateOnline
	// 🔴 A RESTART STARTS A NEW SESSION, SO IT STARTS FROM A CLEAN SHEET
	// (T-ed79 parity #11). This handler used to write desired_state and NOTHING
	// else, and worker_spawn.go names the leftover it produces by name: "NOTHING
	// clears the second one — clearWorkerRefocus is only reachable while
	// refocus_since > 0, and the restart handler writes desired_state and nothing
	// else — so it outlives the whole stop→restart cycle."
	//
	// WHICH ANCHORS, and why exactly these:
	//   * refocus_since / refocus_op / stopping_since / stopped_since all date
	//     the session being REPLACED. Carried into the next one they are read as
	//     facts about THAT one — and the pair (refocus > 0 ∧ stopped > 0) is read
	//     by workerHasStateToFlush as "this epoch's wind-down is already
	//     collected", which shoots the next 改機器 / 換 model on the spot with no
	//     close-out. The epoch scoping in that predicate heals a stale
	//     stopped_since ALONE; it cannot heal a stale PAIR, because a stale pair
	//     is indistinguishable from a real collected epoch.
	//   * forced_stop_at is deliberately KEPT — the staff activate's rule
	//     verbatim. It does not describe this session; it describes the one
	//     BEFORE it, and the reader who needs it most is the one that comes after
	//     (dal.go, migrations/00057). Its max() upsert would fight a clear here
	//     anyway.
	//
	// This is the SET, not the count: the staff activate clears two anchors
	// (stopping/waking) because those are the two a member carries. Copying the
	// staff LIST would have cleared neither of the two the code above points at.
	//
	// 🔴 A worker DOES carry waking_since as of T-14 — this comment used to say it
	// does not, and that stopped being true the moment the projection was unified.
	// It is deliberately NOT cleared here: notifyWorkerSpawn stamps a fresh anchor
	// on the re-dispatch this restart is about to trigger. ⚠️ Known residue: if
	// that re-dispatch fails outright and the previous anchor is still inside
	// WakingTTLSecs, the row reads 喚醒中 until the TTL lapses. Self-healing, and
	// the staff arm has no equivalent hole because activate zeroes waking_since —
	// so this is the same 正職／外包 divergence T-14 exists to delete, one layer up.
	worker.RefocusSince = 0.0
	worker.RefocusOp = ""
	worker.StoppingSince = 0.0
	worker.StoppedSince = 0.0
	// 後蓋前 (T-65 包②) — and here the reason is 「it is being spent RIGHT NOW」
	// rather than 「it is cancelled」: this handler does the very thing a queued
	// 起來 asks for. Leaving the flag armed would fire a SECOND start after the
	// next 下線, one the owner never asked for.
	clearWorkerRestartIntent(worker)
	// The whole-row write still carries desired_state; WHICH columns it no longer
	// carries is not restated here — this sentence has already gone stale twice as
	// T-55 moved one batch after another, so the answer lives in exactly one place
	// now: singleColumnOwnedFields, which is also what reddens if a column goes
	// back. The four anchors above and the receipt each have their own writer, and
	// both run before the row write.
	//
	// The receipt's order carries no convergence argument here (its gate is hub
	// liveness, not a stored value). The ANCHORS' order does — see
	// persistMemberWindDownAnchors.
	if err := s.persistWorkerWindDownAnchors(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	// 🔴 BEFORE THE RESPAWN, AND THE REASON IS STRONGER THAN "the response should
	// see it". respawnWorkerForOwnerOp WRITES RECEIPTS OF ITS OWN — the held-down
	// arm through stampWorkerPlacementBlocked, the deferred arm through
	// respawnWorkerNow. Move this write after it and the handler's snapshot,
	// taken at the top of the request, lands on top of the receipt the respawn
	// just wrote: the owner is shown the older sentence, on a 200, with nothing
	// red. Placing it first also happens to give the re-read below a row that
	// already carries the receipt, which is the weaker reason this comment used
	// to give on its own.
	//
	// ⚠️ NO TEST HOLDS THIS ORDER. An independent review moved the write past the
	// respawn and the whole suite stayed green. Named gap, not a claim of safety.
	//
	// No publish of its own: the publishOutsourceWorker below fans the projection
	// once, for both writes.
	if sessionAliveReceipt {
		if err := s.dal.SetMemberOpReceipt(worker.ID, worker.LastOp, worker.LastOpOK,
			worker.LastOpLog, worker.LastOpReason, worker.LastOpAt); err != nil {
			s.outsourceMu.Unlock()
			internalError(w, err)
			return
		}
	}
	outcome := s.respawnWorkerForOwnerOp(*worker, ownerOpRestart)
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	// 🔴 THE RETURN VALUE THAT USED TO BE DROPPED (T-ed79 #12). The intent is
	// persisted above so the restart never FAILS on dispatch — and that is exactly
	// what made the silence dangerous: a 重啟 whose worker_start never went out
	// (no kill target for the session it replaces, a warden that would not take
	// it, an unbuildable frame) answered a clean 200 with zero signal, which is
	// the shape T-ba62 called 「整個 bug」 when it fixed the staff twin. WHICH
	// cause is on last_op_reason, in the shared reason-code family (#14).
	s.writeWorkerProjectionWith(w, r, *worker, func(dto *outsourceWorkerDTO) {
		if outcome.Pending() {
			pending := true
			dto.ActivationPending = &pending
		}
	})
}

// POST /api/outsource-workers/{id}/model — the owner cockpit's runtime/model
// edit (owner/admin agent since T-6020), the worker twin of the member runtime/model/effort edit.
// Persist the new values; when the worker is ACTIVE + online AND a launch intent
// actually changed, hand it over so the new model takes effect on the next
// session, otherwise (assigned / stopped / nothing changed) only persist — the
// next spawn / restart bakes it in ("active 時 kill+respawn 立即生效, assigned 時
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
	// The three LAUNCH INTENTS, compared old-against-new — the staff face's rule
	// verbatim (HandleUpdateMember, T-b6d9). Only a value that ACTUALLY changed
	// can be stale in the running session, so only a value that actually changed
	// is worth a session for. Re-saving what the worker is already running on used
	// to open a wind-down and end in kill+respawn: a round of work thrown away to
	// store a value that was already stored, which is exactly the shape a cockpit
	// dialog produces every time the owner opens it and presses save.
	//
	// Compared on the SAME normalised form that gets persisted (trimmed), so
	// " sonnet" against "sonnet" is honestly not a change; an unset field is
	// nothing at all, never an implicit blank.
	launchIntentChanged := false
	if body.Model != nil {
		model := strings.TrimSpace(*body.Model) // blank ⇒ launcher default
		launchIntentChanged = launchIntentChanged || model != worker.Model
		worker.Model = model
	}
	if body.Runtime != nil {
		runtime := string(*body.Runtime)
		if !ValidRuntime(runtime) {
			s.outsourceMu.Unlock()
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
		launchIntentChanged = launchIntentChanged || runtime != worker.Runtime
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
		launchIntentChanged = launchIntentChanged || effort != worker.Effort
		worker.Effort = effort
	}
	// The values this request means to store, held apart from `worker` because
	// the re-read below replaces that pointer.
	wantModel, wantRuntime, wantEffort := worker.Model, NormalizeRuntime(worker.Runtime), worker.Effort
	if err := s.dal.PutOutsourceWorker(*worker); err != nil {
		s.outsourceMu.Unlock()
		internalError(w, err)
		return
	}
	// Take effect immediately only for a LIVE session whose launch intent actually
	// CHANGED; an assigned worker adopts the new model at its next spawn. Whether the owner wants it running at all
	// is deliberately NOT re-asked here — respawnWorkerForOwnerOp owns that single
	// branch point for all three owner verbs, and asking twice is how the two
	// copies drift (this one used to skip silently, leaving no receipt).
	// 🔴 THIS RUNS BEFORE THE SETTERS, SO THE FRAME IT MAY DISPATCH READS THE
	// VALUE, NOT THE ROW — and that is now a load-bearing invariant.
	// respawnWorkerForOwnerOp has two arms: the wind-down arm dispatches nothing,
	// but the immediate arm (reached when the epoch has already been collected
	// while the session is still online) kills and re-dispatches a START right
	// here. It takes `*worker` BY VALUE and every step below it does too
	// (respawnWorkerForOwnerOpNow → respawnWorkerNow → notifyWorkerSpawn), so the
	// frame carries the intent this request just set, which has not reached the
	// row yet.
	// ⇒ notifyWorkerSpawn and everything under it must NEVER re-read the member
	// row for the launch spec. Doing so would dispatch the OLD model while the
	// setter below stores the new one, and the worker would run the old value
	// with a 200, no receipt and nothing red — T-b6d9's bug through a third door.
	// Pinned by TestSetWorkerModel_ImmediateRespawnCarriesTheNewModel.
	if launchIntentChanged && worker.Status == WorkerStatusActive && s.hub.IsOnline(worker.ID) {
		s.respawnWorkerForOwnerOp(*worker, ownerOpModel)
	} else if launchIntentChanged && worker.DesiredState == DesiredStateOffline {
		// 🔴 THE FUNNEL IS UNREACHABLE FROM HERE FOR EXACTLY THE ROWS THIS BRANCH
		// SERVES, which is why the T-65 包② stamp could not live in
		// respawnWorkerForOwnerOp's held-down arm alone. The gate above additionally
		// requires an ACTIVE worker with a LIVE session; a worker whose stop has
		// CONVERGED has neither, so it never enters the funnel and the held-down arm
		// never runs. 改機器 has no such gate (relocateWorkerNow is unconditional) —
		// that is the whole asymmetry, and it is measured rather than assumed:
		// TestSetModelOnStoppedAnchoredWorkerQueuesTheStart drives this branch with
		// the session gone.
		//
		// Owner 2026-08-30: 「change model / machine 只是帶起來的方式不一樣而已」 —
		// so the new value is not merely stored and forgotten; the worker comes back
		// up on it. aStopWasEverAskedFor (inside the helper) keeps a worker nobody
		// ever asked to stop from being booted by an edit.
		if s.queueWorkerRestartAfterStop(worker, ownerOpModel, nowSecs()) {
			if err := s.persistWorkerRestartIntent(*worker); err != nil {
				s.outsourceMu.Unlock()
				internalError(w, err)
				return
			}
		}
	}
	// Same seam as the staff face (T-55): the three intents left PutMember's SET
	// list, so PutOutsourceWorker no longer carries them and each one lands
	// through its sole writer — only for a field this request actually carried.
	//
	// 🔴 AFTER the respawn, and for the reason HandleUpdateMember spells out at
	// length: one write became two, and only this order fails convergently. Store
	// the value FIRST and a failure here leaves the new value on the row with no
	// wind-down — and the retry cannot heal it, because launchIntentChanged
	// compares the request against the STORED value, which now already matches, so
	// the second attempt opens no session either. The worker would run the old
	// model until something unrelated respawned it. This way round a failure
	// leaves the OLD value with a wind-down already open: one wasted recycle onto
	// the value it was already running, and the retry still sees a change.
	//
	// The whole sequence holds outsourceMu, so the collection that would act on
	// that wind-down cannot land between the two writes.
	if body.Model != nil {
		if err := s.dal.SetMemberModel(id, wantModel); err != nil {
			s.outsourceMu.Unlock()
			internalError(w, err)
			return
		}
	}
	if body.Runtime != nil {
		// NORMALISED, matching memberFromWorker: the worker projection has
		// always stored NormalizeRuntime(w.Runtime), and the sole writer must
		// not quietly start storing a second form on the same column. (Today
		// ValidRuntime already narrows this to claude|codex, so the call is
		// identity — it is here so the property survives the next runtime.)
		if err := s.dal.SetMemberRuntime(id, wantRuntime); err != nil {
			s.outsourceMu.Unlock()
			internalError(w, err)
			return
		}
	}
	if body.Effort != nil {
		if err := s.dal.SetMemberEffort(id, wantEffort); err != nil {
			s.outsourceMu.Unlock()
			internalError(w, err)
			return
		}
	}
	// Re-read AFTER both writes so the projection the owner gets back is the row
	// as it now stands — the respawn stamps its own fields, and the setters land
	// after it.
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	s.outsourceMu.Unlock()

	s.writeWorkerProjection(w, r, *worker)
}
