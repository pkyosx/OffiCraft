package main

// api_members.go — the roster + presence + lifecycle handlers
// (handlers.handle_list_members … handle_dismiss_member + the three
// self-report presence tools). Every durable write funnels through the DAL
// and fans a member delta through the hub (the Python Repository
// commit-funnel behaviour).
//
// Reconcile dispatch note: activate/deactivate fire the EVENT-DRIVEN
// single-member reconcile (reconcile.go reconcileMemberNow — the Python
// _dispatch_reconcile_now click seam, sharing the cadence's store so the 30s
// tick stays an idempotent backstop); force-stop and the first stopped-report
// of a refocus-marked member fire the immediate robust STOP
// (dispatchRobustStopNow — handlers._dispatch_robust_stop_now).

import (
	"errors"
	"fmt"
	"net/http"
)

// minSelfRestartSecs is the restart_self minimum-liveness floor (T-4c71): a
// self-triggered recycle within this many seconds of the session connecting is
// refused (429), so a freshly respawned agent cannot immediately self-restart
// and spin a respawn storm. Owner-approved at 10 minutes — a flat floor, kept
// distinct from the context-high boot-storm guard's MinBootSecs.
const minSelfRestartSecs = 600.0

// putMember validates + persists a member and fans the member delta: a
// dismiss (roster_status=removed, the soft delete) rides as op=remove
// (deleted:true, payload null — Repository.put_member parity); every other
// write is a patch carrying the partial convenience payload (spec/sse.md
// §2.2: {id, name, status, desired_state, owner_id}).
func (s *apiServer) putMember(m Member, trigger string) error {
	if err := ValidateMember(m); err != nil {
		return err
	}
	if err := s.dal.PutMember(m); err != nil {
		return err
	}
	op := "patch"
	if m.RosterStatus == RosterStatusRemoved {
		op = "remove"
	}
	// A member delta reaches ITS OWN connection (the wind-down / recycle hooks
	// key on a member delta naming self — cli/ocagent shouldWindDown) plus the
	// owner cockpit; other agents ignore it (spec/sse.md §4).
	s.hub.Publish("member", op, "member", wireOwnerID+"::"+m.ID, memberDeltaPayload(m),
		audienceMembers(m.ID), trigger)
	return nil
}

// memberDeltaPayload is the member delta's partial convenience payload
// (repository._member_payload — the client reconciles by refetch).
func memberDeltaPayload(m Member) map[string]any {
	return map[string]any{
		"id":            m.ID,
		"name":          m.Name,
		"status":        m.RosterStatus,
		"desired_state": m.DesiredState,
		"owner_id":      wireOwnerID,
	}
}

// GET /api/members — the roster (soft-removed rows omitted). online is the
// live SSE projection; machine the OBSERVED position; unread_count the pure
// inverse of the caller's chat_read watermark.
//
// ?fields=light (T-cf91) is the ADDITIVE identity-only projection for surfaces
// that render ONLY a member's name + role (the 請示卡頁 attributes each card to
// its asker and needs nothing else). It SKIPS the whole-chat scan +
// per-member chat_read watermark math (UnreadCounts over ListChat) and the
// per-member presence / observed-host derivation (hub + telemetry lookups) —
// none of which the name/role view reads. The light DTO keeps the SAME
// memberDTO wire shape (no new response schema — additive), but the fields
// those skipped computations feed are HONEST-EMPTY: unread_count 0, presence
// "", machine "", last_op* untouched-from-row. A consumer must not read those
// off a light response — the value is "not computed", not "known zero". The
// default (no fields param, or any value other than "light") is byte-for-byte
// the full roster as before. This mirrors the roster hook's matching change:
// the light consumer also stops treating chat SSE deltas as a roster refetch
// trigger (a message never changes a name or role), so a company-wide chat
// line no longer re-pulls this endpoint at all.
func (s *apiServer) HandleListMembersApiMembersGet(w http.ResponseWriter, r *http.Request, params HandleListMembersApiMembersGetParams) {
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	light := trimmedOrEmpty(params.Fields) == "light"

	// unread rides the caller's chat_read watermark over the whole chat stream —
	// the single most expensive part of this handler and exactly what the light
	// projection exists to avoid. Only compute it on the full path.
	var unread map[string]int
	if !light {
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
		unread = UnreadCounts(messages, receipts, actor)
	}

	out := []memberDTO{}
	for _, m := range members {
		if m.RosterStatus == RosterStatusRemoved {
			continue
		}
		roleName, err := s.memberRoleName(m)
		if err != nil {
			internalError(w, err)
			return
		}
		if light {
			out = append(out, s.newMemberLightDTO(m, roleName))
			continue
		}
		out = append(out, s.newMemberDTO(m, roleName, s.observedHost(m), unread[m.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/members — hire. The server mints the id; a blank name is 422. A
// body carrying kind/role_key is PRIVILEGE-BEARING (warden = machine
// principal, assistant = admin principal) and demands an admin_agent caller.
func (s *apiServer) HandleHireMemberApiMembersPost(w http.ResponseWriter, r *http.Request) {
	var body MemberHireDTO
	if !decodeJSONBodyRequired(w, r, &body, "name") {
		return
	}
	name := trimString(body.Name)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "member requires a name")
		return
	}
	privileged := trimmedOrEmpty(body.Kind) != "" || trimmedOrEmpty(body.RoleKey) != ""
	if privileged && !principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		writeError(w, http.StatusForbidden,
			"hiring with kind/role_key is privilege-bearing; "+
				"it requires an owner or an admin-role caller")
		return
	}
	if body.Effort != nil && !validEffort(*body.Effort) {
		writeError(w, http.StatusUnprocessableEntity,
			"effort must be one of [high low medium]; got '"+*body.Effort+"'")
		return
	}
	// The Go kind is a CLOSED set: the Python bare hire's kind="" folds to
	// "assistant" at this ingest seam (CanonicalKind — owner-approved mapping);
	// a kind outside the closed set is refused.
	kind, err := CanonicalKind(strOrEmpty(body.Kind))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	effort := strOrEmpty(body.Effort)
	if effort == "" {
		effort = "medium"
	}
	m := Member{
		ID:               "m-" + newHexID(12),
		Name:             name,
		Kind:             kind,
		RoleKey:          strOrEmpty(body.RoleKey),
		Model:            strOrEmpty(body.Model),
		Effort:           effort,
		DesiredState:     DesiredStateOffline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}
	if err := s.putMember(m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, m)
}

// GET /api/members/{member_id} — one roster member (removed → 404); machine
// is the OBSERVED position. SELF-READ exception (T-ea82): an outsource worker
// reading its OWN row (memberId == the verified sub) resolves — the ocagent
// recycle/wind-down hooks refetch GET /api/members/<self> and must see the
// worker's desired_state/refocus_since; any OTHER ow- target keeps the
// pre-fold 404 (resolveMember).
func (s *apiServer) HandleGetMemberApiMembersMemberIdGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if errors.Is(err, errNotFound) && memberId == currentActor(r) {
		m, err = s.resolveSelf(r)
	}
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.newMemberDTO(*m, roleName, s.observedHost(*m), 0))
}

// PATCH /api/members/{member_id} — partial edit (name/model/effort). Blank
// name / unknown effort → 422; runtime fields untouched.
func (s *apiServer) HandleUpdateMemberApiMembersMemberIdPatch(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if body.Name != nil {
		name := trimString(*body.Name)
		if name == "" {
			writeError(w, http.StatusUnprocessableEntity, "member name cannot be blank")
			return
		}
		m.Name = name
	}
	if body.Model != nil {
		m.Model = *body.Model
	}
	if body.Effort != nil {
		if !validEffort(*body.Effort) {
			writeError(w, http.StatusUnprocessableEntity,
				"effort must be one of [high low medium]; got '"+*body.Effort+"'")
			return
		}
		m.Effort = *body.Effort
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/activate — write desired_state=online intent.
// ALWAYS FORCE-REVIVE: both winding-down anchors clear unconditionally.
func (s *apiServer) HandleActivateMemberApiMembersMemberIdActivatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberActivateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m.StoppingSince = 0.0
	m.WakingSince = 0.0
	m.DesiredState = DesiredStateOnline
	if body.MachineId != nil {
		m.DesiredMachineID = *body.MachineId
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven reconcile (the Python _dispatch_reconcile_now click seam):
	// decide + dispatch the START NOW, not on a later tick; the shared
	// reconcile store makes the cadence an idempotent backstop. Best-effort —
	// never fails the activate.
	s.reconcileMemberNow(m.ID)
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/relocate — the owner cockpit's 改機器 for a roster
// member (admin-gated, route Requires=admin_agent — parity with the member
// lifecycle family). The member twin of the outsource-worker relocate: write the
// owner-pinned desired_machine_id, then run the SAME event-driven reconcile the
// activate click uses (reconcileMemberNow) — a LIVE member is auto-migrated onto
// the chosen machine (fbc5280: the online-converged reconcile branch robust-STOPs
// the old session so the next tick re-spawns on the pin), an offline member just
// re-pins so the next wake lands there. PLACEMENT ONLY — unlike activate it NEVER
// touches desired_state (or the stopping/waking anchors): a relocate is not a
// wake. 404 for an unknown / removed member; a concrete (non-"", non-"auto") pin
// that names no real machine is a 404, so a stale/typo'd id never pins the member
// to a placement that can never boot (the worker-relocate reasoning).
func (s *apiServer) HandleRelocateMemberApiMembersMemberIdRelocatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberRelocateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	machineID := ""
	if body.MachineId != nil {
		machineID = *body.MachineId
	}
	if machineID != "" && machineID != "auto" {
		if _, err := s.resolveMachine(machineID); err != nil {
			writeResolveError(w, err, "machine", machineID)
			return
		}
	}
	m, err := s.resolveMember(memberId)
	if err != nil {
		// P7c (gate rc-2786636f30e5, 外包對齊正職): the tool's semantics are "move
		// one agent" — an id that names no STAFF member falls through to the
		// outsource projection, so an admin agent's MCP relocate_member moves a
		// worker with the same verb. Since the P7d fold both live in the member
		// table, but resolveMember deliberately excludes kind='outsource', so
		// an ow- id still routes HERE — onto the worker relocate core (worker
		// spawn machinery), never the member reconcile path. The id namespaces
		// stay disjoint ("m-…"/named roster ids vs "ow-…"), so no shadowing.
		if errors.Is(err, errNotFound) {
			if worker, werr := s.dal.GetOutsourceWorker(memberId); werr == nil &&
				worker != nil && worker.Status != WorkerStatusReleased {
				s.relocateWorkerByID(w, r, memberId, machineID)
				return
			}
		}
		writeResolveError(w, err, "member", memberId)
		return
	}
	// The ONLY mutation is the placement pin — desired_state and the winding-down
	// anchors are deliberately left untouched (the activate contrast).
	m.DesiredMachineID = machineID
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven reconcile: an online member whose running machine no longer
	// matches the fresh pin is migrated NOW (STOP old session → re-spawn on the
	// pin next tick); an offline member is a no-op here (nothing to move). The
	// pin is already persisted so the relocate never FAILS on dispatch — but we
	// OBSERVE it: a decided recycle STOP / START that the warden could not accept
	// (old/new machine unreachable) surfaces relocation_pending=true, so the
	// caller sees "move scheduled, not yet landed" instead of a silent 200
	// success (T-8655). The cadence retries the pinned move regardless.
	dec := s.reconcileMemberNow(m.ID)
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	dto := s.newMemberDTO(*m, roleName, "", 0)
	if dec.DispatchUnlanded {
		pending := true
		dto.RelocationPending = &pending
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/members/{member_id}/deactivate — desired_state=offline + an
// UNCONDITIONAL stopping_since re-stamp (each call restarts the grace clock).
func (s *apiServer) HandleDeactivateMemberApiMembersMemberIdDeactivatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m.DesiredState = DesiredStateOffline
	m.StoppingSince = nowSecs()
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven reconcile: arm the 120s grace clock immediately (a graceful
	// stop dispatches NOTHING inside the grace; the eventual robust stop stays
	// the cadence's job).
	s.reconcileMemberNow(m.ID)
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/force-stop — STOP intent now (stamps
// stopping_since only if unset) + the immediate robust-STOP dispatch straight
// to the member's warden, bypassing BOTH the 120s grace clock AND the ~30s
// cadence (handlers.handle_force_stop_member).
func (s *apiServer) HandleForceStopMemberApiMembersMemberIdForceStopPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m.DesiredState = DesiredStateOffline
	if m.StoppingSince <= 0.0 {
		m.StoppingSince = nowSecs()
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.dispatchRobustStopNow(m.ID)
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/refocus — online-only (409 otherwise); stamps
// refocus_since.
func (s *apiServer) HandleRefocusMemberApiMembersMemberIdRefocusPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if PresenceState(*m, nowSecs(), s.hub.IsOnline(m.ID)) != MemberPresenceOnline {
		writeError(w, http.StatusConflict,
			"refocus requires the member to be online (§3.4 #14)")
		return
	}
	m.RefocusSince = nowSecs()
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// DELETE /api/members/{member_id} — dismiss: a SOFT delete (roster_status=
// removed + desired_state=offline); the audit row survives.
func (s *apiServer) HandleDismissMemberApiMembersMemberIdDelete(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m.RosterStatus = RosterStatusRemoved
	m.DesiredState = DesiredStateOffline
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// T-4166: the asker is gone, so no answer can ever be delivered to its
	// waiting cards — retire them instead of leaving them in the owner's
	// 等我回覆 pane forever (each one pins the cockpit red dot on a member that
	// no longer exists). Same sweep the reassign / task-close seams use.
	//
	// BEST-EFFORT (review B5): putMember above ALREADY persisted the dismissal,
	// and there is no transaction to roll it back. 500-ing here would report
	// "dismiss failed" for a member that IS dismissed. Log instead — matching
	// expireWaitingCardsFromMember's own contract and the worker-dismissal twin.
	if _, err := s.expireWaitingCardsFromMember(m.ID, nowSecs(), requestTrigger(r)); err != nil {
		taskLog("dismiss %s: reply-card sweep failed (cards left waiting): %v", m.ID, err)
	}
	s.writeMemberDTO(w, *m)
}

// ── self-report presence (identity from token, NO member_id target) ──────────

// resolveSelf is the caller's own live member (404 when it has no roster row
// — e.g. the owner's sub has none: self-report is agent-only by construction).
// Unlike resolveMember it does NOT fold kind='outsource' onto errNotFound:
// since the graceful worker handover (T-ea82) an outsource worker walks the
// SAME five-step SOP as a member and reports its own presence through these
// self endpoints — only the member_id-target admin surface keeps the pre-fold
// ow- 404.
func (s *apiServer) resolveSelf(r *http.Request) (*Member, error) {
	m, err := s.dal.GetMember(currentActor(r))
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved {
		return nil, errNotFound
	}
	return m, nil
}

// POST /api/self/waking — the boot report: stamps waking_since and clears ALL
// the recycle markers; an optional model self-report writes the true model.
func (s *apiServer) HandleReportWakingApiSelfWakingPost(w http.ResponseWriter, r *http.Request) {
	var body ReportWakingDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		// Worker fold (T-ea82): clear the recycle markers under outsourceMu via
		// the worker funnel — a member-path putMember here would race the tick's
		// read-modify-write and could lose the fold.
		fresh, werr := s.workerReportWaking(m.ID, body.Model, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	m.WakingSince = nowSecs()
	m.RefocusSince = 0.0
	m.StoppedSince = 0.0
	m.StoppingSince = 0.0
	if body.Model != nil {
		m.Model = *body.Model
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/stopping — stamps the caller's stopping_since IF UNSET
// (waking_since deliberately NOT cleared; stopping dominates the projection).
func (s *apiServer) HandleReportStoppingApiSelfStoppingPost(w http.ResponseWriter, r *http.Request) {
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		fresh, werr := s.workerReportStopping(m.ID, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	if m.StoppingSince <= 0.0 {
		m.StoppingSince = nowSecs()
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/stopped — anchors stopped_since ONCE (never re-stamped).
// The FIRST stopped-report of a refocus-marked, still-desired-online agent
// (graceful dump complete) fires the event-driven recycle kill so
// kill→respawn happens immediately, not on the next ~30s tick
// (handlers._dispatch_recycle_kill).
func (s *apiServer) HandleReportStoppedApiSelfStoppedPost(w http.ResponseWriter, r *http.Request) {
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		// Worker 收口 (T-ea82): the first stopped-report of a refocus-marked
		// worker runs the collect funnel (kill+respawn NOW) — the member
		// recycle-kill shape, riding the worker's own kill funnel instead of
		// dispatchRobustStopNow.
		fresh, werr := s.workerReportStopped(m.ID, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	recycleKill := false
	if m.StoppedSince <= 0.0 {
		m.StoppedSince = nowSecs()
		if m.DesiredState == DesiredStateOnline && m.RefocusSince > 0.0 {
			recycleKill = true
		}
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Dispatch AFTER putMember so the marker persistence + member-delta fan
	// (→ agent RecycleHook) has already landed before the STOP.
	if recycleKill {
		s.dispatchRobustStopNow(m.ID)
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/refocus — restart_self(): the agent's SELF-TRIGGERED recycle
// (identity from token, NO member_id). A self-op is only ever able to restart
// the CALLER, so it is strictly weaker than the admin-gated refocus_member —
// zero privilege-escalation surface. The EFFECT is identical to refocus_member:
// stamp the caller's refocus_since and fan the member delta; the standard §4.5
// recycle orchestration (the agent's own RecycleHook → five-step SOP →
// report_stopped → server kill/respawn) carries the rest. Nothing is dispatched
// here (same as refocus_member — no reconcileMemberNow).
//
// Two abuse guards refuse LOUDLY (readable by the agent):
//   - ONLINE-ONLY (409): a self-restart is meaningless with no live session
//     (mirrors refocus_member's gate).
//   - MINIMUM-LIVENESS (429): a call within minSelfRestartSecs of this session
//     connecting is refused — the server-authoritative boot_ts (stamped on the
//     SSE first-connect edge, onFirstConnect) is the anchor; reusing the
//     bootStormTripped loop-guard so a missing boot_ts (server-restart amnesia)
//     FAILS OPEN, never a false 429 on a long-lived session.
func (s *apiServer) HandleRestartSelfApiSelfRefocusPost(w http.ResponseWriter, r *http.Request) {
	var body RestartSelfDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	now := nowSecs()
	if PresenceState(*m, now, s.hub.IsOnline(m.ID)) != MemberPresenceOnline {
		writeError(w, http.StatusConflict,
			"restart_self requires you to be online (no live session to recycle)")
		return
	}
	secsSinceBoot := gaugeSecsSinceBoot(s.gauge.Get(m.ID), now)
	if bootStormTripped(secsSinceBoot, minSelfRestartSecs) {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf(
			"restart_self refused: only %.0fs since this session started; the "+
				"minimum-liveness floor is %.0fs (prevents a respawn storm)",
			*secsSinceBoot, minSelfRestartSecs))
		return
	}
	if m.Kind == KindOutsource {
		// Worker fold (T-ea82): stamp the refocus epoch + open the graceful
		// window via the worker funnel (the same shape the owner's refocus
		// button takes) — the standard SOP → stopped-report → collect carries
		// the rest.
		fresh, werr := s.workerRestartSelf(m.ID, now, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		if reason := trimmedOrEmpty(body.Reason); reason != "" {
			reconcileLog("recycle: %s self-restart (restart_self); reason: %s", m.ID, reason)
		} else {
			reconcileLog("recycle: %s self-restart (restart_self)", m.ID)
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	m.RefocusSince = now
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Distinguish a self-restart from an owner refocus on the operator log
	// (both stamp refocus_since identically; the reason is the differentiator).
	if reason := trimmedOrEmpty(body.Reason); reason != "" {
		reconcileLog("recycle: %s self-restart (restart_self); reason: %s", m.ID, reason)
	} else {
		reconcileLog("recycle: %s self-restart (restart_self)", m.ID)
	}
	s.writeMemberDTO(w, *m)
}
