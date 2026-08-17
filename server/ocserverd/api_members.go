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
	"io"
	"net/http"
	"strings"
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
	s.hub.Publish("member", op, "member", wireOwnerID+"::"+m.ID, s.offboardDeltaPayload(m),
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

// offboardDeltaPayload is memberDeltaPayload plus the offboard notice, and it is
// the whole of "改回真的推播" (owner 2026-08-16, card rc-66b82a584c4d): the
// SERVER composes the sentence and carries the 下線程序 steps in the frame it
// pushes, instead of the agent fetching them back over HTTP once it notices it
// is being collected.
//
// The notice rides ONLY a member that is actually being wound down — a refocus
// epoch, or a graceful 下線 (offboardKindOf decides). Attaching it to every
// member delta would put a document fold and a couple of kilobytes on every
// roster change. ⚠️ Within those states it rides EVERY write to that row, not
// just the first: the client is what de-duplicates, by keying on the sentence
// it last printed.
//
// An empty notice omits the key rather than sending "": the client's fallback
// arms on the key being absent, and an empty string would read as "the server
// sent me a notice and it said nothing".
func (s *apiServer) offboardDeltaPayload(m Member) map[string]any {
	payload := memberDeltaPayload(m)
	kind, carries := offboardKindOf(m, nowSecs())
	if !carries {
		return payload
	}
	if notice := s.offboardNoticeFor(m, kind); notice != "" {
		payload["offboard_notice"] = notice
	}
	return payload
}

// offboardKindOf answers the two questions every offboard delta turns on: does
// this member carry a notice at all, and is it the SOFT one or the FINAL call.
//
// The owner's ruling (2026-08-16) is that his own two buttons and the agent's
// own context pressure walk the SAME sequence, and that what tells the
// situations apart is whether there is still room:
//
//   - SOFT — 下線 (desired offline + a stopping anchor, the graceful arm) and
//     重新聚焦. It says work the sequence, then call restart_self yourself; no
//     countdown clause, because at this point there is not one.
//   - FINAL — every other refocus cause (context_high, 改機器, model/runtime,
//     restart_self): the collection is already under way and the 120s recycle
//     clock is running, so the sentence has to say so.
//
// 🔴 The soft arm is the ONLY reason 下線 reaches the agent at all. Before this,
// a deactivate stamped stopping_since and nothing else, so the notice condition
// (refocus_since > 0) was false and the agent was collected having never been
// shown the sequence — while the client-side wind-down declared "durable state
// already server-side — nothing extra to flush" on its behalf, which was not
// true of any session holding an unwritten hand-off.
// The soft→final promotion is DERIVED FROM TIME, not written down: the same
// anchor and the same soft window that decide when the collection is forced
// decide which sentence the agent is being sent. A stored flag would be a
// second copy of that judgement, free to disagree with the clock actually
// collecting the session — and the disagreement would read as a notice
// promising 120 seconds while several minutes remained, or the reverse.
func offboardKindOf(m Member, now float64) (kind string, carries bool) {
	softExpired := func(anchor float64) string {
		if now >= anchor+SoftOffboardGraceSecs {
			return offboardKindFinal
		}
		return offboardKindSoft
	}
	if m.DesiredState == DesiredStateOffline {
		// Only the graceful arm: a member with no stopping anchor is not being
		// wound down (and a cancelled wake is force-stopped outright, which is
		// deliberately silent — see HandleForceStopMember).
		//
		// 🔴 And it stays SOFT forever, because nothing collects it on a clock:
		// the owner ruled 下線 runs no countdown at all (rc-27d1710174dd), so a
		// notice claiming 120 seconds here would be a promise nobody keeps —
		// an agent would cut its hand-off short to beat a deadline that does
		// not exist. Escalation on this arm is the owner pressing force-stop,
		// and that path deliberately says nothing.
		//
		// 🔴 …and "deliberately says nothing" has to be enforced HERE, not just
		// asserted in prose. force-stop sets desired_state=offline AND stamps
		// stopping_since before it publishes, so on the sentence above alone the
		// member it just killed receives a full SOFT notice on its own stream —
		// telling a session that is about to be cut off to work the sequence and
		// call restart_self. Independent e2e verification observed exactly that
		// frame; the owner's ruling is that force-stop sends no message at all.
		if m.StoppingSince > 0 && !forcedEpochLive(m) {
			return offboardKindSoft, true
		}
		return "", false
	}
	if m.RefocusSince <= 0 {
		return "", false
	}
	if m.RefocusOp == refocusOpRefocus {
		return softExpired(m.RefocusSince), true
	}
	return offboardKindFinal, true
}

const (
	offboardKindSoft  = "soft"
	offboardKindFinal = "final"
)

// forcedEpochLive: the stop this member is currently under was opened by a
// FORCE-stop, not by 下線. It is the one judgement that separates "cut off
// deliberately" from "working its close-out", and three places need it — the
// notice (a forced path says nothing), the SSE stop gate (a forced member's
// reconnect is refused) and deactivate (a forced epoch must not be re-stamped
// into a softer one). One definition, because two copies of this could disagree
// about the same member.
//
// stopping_since > 0 is what scopes it to a LIVE epoch: activate clears the stop
// anchors but deliberately KEEPS forced_stop_at as the durable record that a
// past session was cut off, so reading that column alone would treat every
// member ever force-stopped as permanently forced.
//
// 🔴 The >= is LOAD-BEARING, not defensive. force-stop stamps stopping_since
// and forced_stop_at from two nowSecs() calls with no I/O between them, and at
// 1.78e9 a float64 tick is ~238ns — so the two anchors landing on the SAME
// value is the NORMAL path, not a coincidence. Independent review measured it:
// every failure dump from the mutants that exercise the real handler shows the
// two columns identical. Tidying this into > breaks force-stop outright.
func forcedEpochLive(m Member) bool {
	return m.ForcedStopAt > 0.0 && m.StoppingSince > 0.0 &&
		m.ForcedStopAt >= m.StoppingSince
}

// offboardCloserFor names the tool that ACTUALLY ends this member's sequence.
// A member still wanted online is being handed over and re-starts itself; one
// the owner has taken down is not coming back, and restart_self refuses it by
// design (it is a RE-start). Telling it otherwise would be an instruction that
// can only answer 409 — on an arm where nothing collects it on a clock, so it
// would sit there refused until someone pressed force-stop.
func offboardCloserFor(m Member) string {
	if m.DesiredState == DesiredStateOffline {
		return offboardCloserReportStopped
	}
	return offboardCloserRestartSelf
}

// offboardNoticeFor composes the sentence for a member that is being wound
// down: the ONE approved sentence, plus the 120-second clause when this is the
// final call. It reads the session's own gauge so the agent is told where it
// actually is, not just that it is over the line — the owner's requirement that
// the notice carry 「他現在 context / round 狀況，以及我們兩個系統數字是多少」.
func (s *apiServer) offboardNoticeFor(m Member, kind string) string {
	cfg := s.ctxHighConfig()
	// The gauge is absent on a server assembled without one (and a session that
	// never reported has no entry either). Degrade to "?" for the position
	// rather than dropping the notice: WHERE the session is is useful, being
	// told it is being collected is essential.
	var record map[string]any
	if s.gauge != nil {
		record = s.gauge.Get(m.ID)
	}
	var where string
	if NormalizeRuntime(m.Runtime) == RuntimeCodex {
		final := s.codexCompactionThresholdSetting()
		notice := s.codexNoticeRoundSetting()
		if notice < 1 {
			notice = final - 1
		}
		round := "?"
		if record != nil {
			if v, ok := asNumber(record["compaction_count"]); ok {
				round = fmt.Sprintf("%d", int(v))
			}
		}
		if kind == offboardKindFinal {
			round = fmt.Sprintf("%d", final)
		}
		where = fmt.Sprintf("compaction round %s (your limits: round %d / round %d)",
			round, notice, final)
	} else {
		pct := "?"
		if record != nil {
			if v, ok := asNumber(record["context_pct"]); ok {
				pct = fmt.Sprintf("%v", formatPct(v))
			}
		}
		where = fmt.Sprintf("context %s%% (your limits: %d%% / %d%%)",
			pct, cfg.NoticePct, cfg.HandoverPct)
	}
	return offboardNotice(where, offboardCloserFor(m), kind == offboardKindFinal,
		s.offboardText())
}

// resolveAvatarMember admits active staff and outsource rows but rejects
// wardens: a machine is infrastructure, not a person with a visual identity.
func (s *apiServer) resolveAvatarMember(memberID string) (*Member, error) {
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved {
		return nil, errNotFound
	}
	return m, nil
}

func (s *apiServer) publishMemberAvatarChanged(m Member, trigger string) {
	if m.Kind == KindOutsource {
		s.publishOutsourceWorker(workerFromMember(m), trigger)
		return
	}
	s.hub.Publish("member", "patch", "member", wireOwnerID+"::"+m.ID,
		s.offboardDeltaPayload(m), audienceMembers(m.ID), trigger)
}

func memberAvatarResult(m Member, mime string, filename *string) MemberAvatarDTO {
	url := memberAvatarURL(m.AvatarAttachmentID)
	result := MemberAvatarDTO{MemberId: m.ID, AvatarUrl: &url}
	if mime != "" {
		result.Mime = &mime
	}
	result.Filename = filename
	return result
}

// PUT /api/members/{member_id}/avatar — raw raster bytes, owner-only at the
// route table. A fresh ava- id makes every replacement cache-safe.
func (s *apiServer) HandlePutMemberAvatarApiMembersMemberIdAvatarPut(
	w http.ResponseWriter,
	r *http.Request,
	memberID string,
	params HandlePutMemberAvatarApiMembersMemberIdAvatarPutParams,
) {
	m, err := s.resolveAvatarMember(memberID)
	if err != nil {
		writeResolveError(w, err, "member", memberID)
		return
	}
	if m.Kind == KindWarden {
		writeError(w, http.StatusUnprocessableEntity, "a machine cannot have a personal avatar")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAvatarBytes+1))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not read avatar image")
		return
	}
	if len(raw) > maxAvatarBytes {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("avatar image is too large (max %d bytes)", maxAvatarBytes))
		return
	}
	if len(raw) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "avatar image is empty")
		return
	}
	actualMime := sniffAttachmentMime(raw)
	if _, ok := avatarMimeMagic[actualMime]; !ok {
		writeError(w, http.StatusUnprocessableEntity,
			"avatar must be PNG, JPEG, or WEBP raster bytes")
		return
	}
	if params.Mime != nil {
		declared := strings.TrimSpace(*params.Mime)
		if declared != "" && declared != actualMime {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("avatar mime %q does not match image bytes %q", declared, actualMime))
			return
		}
	}
	var filename *string
	if params.Filename != nil {
		trimmed := strings.TrimSpace(*params.Filename)
		if trimmed != "" {
			filename = &trimmed
		}
	}
	avatar := ChatAttachment{
		ID:       "ava-" + newHexID(12),
		Mime:     actualMime,
		Data:     raw,
		Filename: filename,
	}
	if err := s.dal.ReplaceMemberAvatar(m.ID, avatar); err != nil {
		internalError(w, err)
		return
	}
	m.AvatarAttachmentID = avatar.ID
	s.publishMemberAvatarChanged(*m, requestTrigger(r))
	writeJSON(w, http.StatusOK, memberAvatarResult(*m, actualMime, filename))
}

// DELETE /api/members/{member_id}/avatar — idempotent fallback restoration.
func (s *apiServer) HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete(
	w http.ResponseWriter,
	r *http.Request,
	memberID string,
) {
	m, err := s.resolveAvatarMember(memberID)
	if err != nil {
		writeResolveError(w, err, "member", memberID)
		return
	}
	if m.Kind == KindWarden {
		writeError(w, http.StatusUnprocessableEntity, "a machine cannot have a personal avatar")
		return
	}
	if err := s.dal.DeleteMemberAvatar(m.ID); err != nil {
		internalError(w, err)
		return
	}
	m.AvatarAttachmentID = ""
	s.publishMemberAvatarChanged(*m, requestTrigger(r))
	writeJSON(w, http.StatusOK, memberAvatarResult(*m, "", nil))
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
	members, err := s.dal.ListMembersIncludingOutsource()
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
		var err error
		// The SAME computation the single-member handler runs (api_helpers.go) —
		// one field, one answer, whichever endpoint you ask.
		unread, err = s.unreadCountsForRequest(r)
		if err != nil {
			internalError(w, err)
			return
		}
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
			"effort must be one of [high low max medium]; got '"+*body.Effort+"'")
		return
	}
	runtime := RuntimeClaude
	if body.Runtime != nil {
		runtime = string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
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
		Runtime:          runtime,
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
	// unread_count is COMPUTED here, exactly as the list computes it. Handing
	// newMemberDTO a literal 0 (what this line used to do) made the roster badge
	// a one-way ratchet: the cockpit re-reads one member on a chat delta, so the
	// badge the delta was announcing was zeroed instead of raised. Pinned by
	// api_members_unread_parity_test.go.
	unread, err := s.unreadCountsForRequest(r)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.newMemberDTO(*m, roleName, s.observedHost(*m), unread[m.ID]))
}

// PATCH /api/members/{member_id} — partial edit (name/runtime/model/effort).
// Blank name or an unknown runtime/effort is rejected.
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
	// The three LAUNCH INTENTS are tracked separately from the display name:
	// only they are baked into a boot frame, so only they can be stale in a
	// running session — renaming a member must never recycle it.
	launchIntentChanged := false
	if body.Model != nil {
		launchIntentChanged = launchIntentChanged || *body.Model != m.Model
		m.Model = *body.Model
	}
	if body.Runtime != nil {
		runtime := string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
		launchIntentChanged = launchIntentChanged || runtime != m.Runtime
		m.Runtime = runtime
	}
	if body.Effort != nil {
		if !validEffort(*body.Effort) {
			writeError(w, http.StatusUnprocessableEntity,
				"effort must be one of [high low max medium]; got '"+*body.Effort+"'")
			return
		}
		launchIntentChanged = launchIntentChanged || *body.Effort != m.Effort
		m.Effort = *body.Effort
	}
	// T-b6d9: a launch intent used to be written and then simply ignored by the
	// live session — the owner pressed 儲存, got a 200, and the member went on
	// running the OLD model until something unrelated respawned it. Now the
	// change opens the SAME graceful wind-down 重新聚焦 has always had, so the
	// member finishes what it was doing and comes back on the new value. Same
	// single write: the epoch and the new value can never land apart.
	if launchIntentChanged {
		s.armMemberOwnerOpHandover(m, memberOpModel)
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
	// The machine bind is held to the SAME rule as every other placement write
	// face: any non-blank id must name a real machine. activate is the one that
	// most needs it — it flips desired_state online in the same call, so an
	// unreachable pin here manufactures exactly the "wants to be online, can
	// never be dispatched, never heals" member this validation exists to prevent.
	// "" still clears the pin (the member then waits for a placement).
	if body.MachineId != nil && *body.MachineId != "" {
		if _, err := s.resolveMachine(*body.MachineId); err != nil {
			writeResolveError(w, err, "machine", *body.MachineId)
			return
		}
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
	// reconcile store makes the cadence an idempotent backstop. The intent is
	// already persisted so the activate never FAILS on dispatch — but we OBSERVE
	// it (T-ba62): a decided START the warden could not accept (machine offline,
	// warden never installed, warden's SSE down) surfaces activation_pending=true,
	// exactly as relocate has reported relocation_pending since T-8655. Dropping
	// this return value was the whole bug: an activate against an unreachable
	// warden answered a clean 200 with zero signal, so "waking" and "nothing was
	// dispatched and nothing will be until the next cadence tick" looked identical.
	dec := s.reconcileMemberNow(m.ID)
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	dto := s.newMemberDTO(*m, roleName, "", 0)
	// POSITIVE determination (T-ba62 review R4), not a list of known failures.
	// `dec.DispatchUnlanded` alone was wrong: reconcileOne ALSO downgrades a
	// START to none when buildStartFrame cannot assemble a payload (missing
	// persona / token) and does NOT set DispatchUnlanded there — so a reachable
	// warden plus an unbuildable frame answered a clean 200 with no pending flag
	// and nothing dispatched. Ask instead whether a START actually went out; an
	// already-online member needs none, and every other outcome — backoff,
	// circuit-open, and failure modes not yet invented — is honestly "nothing
	// has been dispatched yet".
	if dec.Command != reconcileCmdStart && !s.hub.IsOnline(m.ID) {
		pending := true
		dto.ActivationPending = &pending
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/members/{member_id}/relocate — the owner cockpit's 改機器 for a roster
// member (admin-gated, route Requires=admin_agent — parity with the member
// lifecycle family). The member twin of the outsource-worker relocate: write the
// owner-pinned desired_machine_id, then run the SAME event-driven reconcile the
// activate click uses (reconcileMemberNow). A LIVE member is auto-migrated onto
// the chosen machine, but SINCE T-b6d9 GRACEFULLY: the pin is written together
// with a refocus epoch, the agent gets the ordinary 下線程序 wake, and
// the kill+re-spawn happens at the 收口 (its own report_stopped, or the recycle
// arm's RecycleGrace ceiling). It used to be an immediate robust STOP with no
// warning at all (fbc5280). An offline member just re-pins so the next wake
// lands there — no epoch, nothing to wind down. PLACEMENT ONLY — unlike activate it NEVER
// touches desired_state (or the stopping/waking anchors): a relocate is not a
// wake. 404 for an unknown / removed member; any non-"" machine_id that names no
// real machine is a 404, so a stale/typo'd id never pins the member to a
// placement that can never boot (the worker-relocate reasoning). machine_id is
// REQUIRED since owner 2026-07-27 (relocateNeedsMachineMsg): an absent key is a
// 422 and an explicit null / "" is a 400 — a relocate names a destination and no
// longer doubles as an unpin. The literal "auto" is NOT exempt from the resolve: it used to be
// waved through as a pseudo-machine, which pinned the member to a destination
// dispatch could never reach (IsOnline("auto") is always false) and reconcile
// never healed — the very hole a nonexistent concrete id was already 404'd for.
func (s *apiServer) HandleRelocateMemberApiMembersMemberIdRelocatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberRelocateDTO
	if !decodeJSONBodyRequired(w, r, &body, "machine_id") {
		return
	}
	if body.MachineId == "" {
		writeError(w, http.StatusBadRequest, relocateNeedsMachineMsg)
		return
	}
	machineID := body.MachineId
	if _, err := s.resolveMachine(machineID); err != nil {
		writeResolveError(w, err, "machine", machineID)
		return
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
	// The placement pin is the only INTENT mutation — desired_state is
	// deliberately left untouched (the activate contrast).
	m.DesiredMachineID = machineID
	// T-b6d9: a LIVE member used to be robust-STOPped on the spot by the
	// reconcile below — no 預告, no grace, not even a stopping_since, so it just
	// vanished from the cockpit with whatever it was mid-way through. It now
	// gets the same wind-down 重新聚焦 has always had; the winding-down anchors
	// ARE written in that case, and only in that case. Same putMember, so the
	// new pin and the epoch land together and the delta the agent wakes on
	// already names the destination.
	windDown := s.armMemberOwnerOpHandover(m, memberOpRelocate)
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven reconcile: with a wind-down open this decides "awaiting agent
	// dump" and dispatches NOTHING (the 收口 owns the move); it still runs so the
	// tick state is advanced here rather than up to 30s later, and it remains the
	// path that migrates a member nobody stamped for (the decideUp relocate arm,
	// now a backstop). An offline member is a no-op here (nothing to move). The
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
	// relocation_pending means what it has always meant — "move scheduled, not
	// yet landed". T-b6d9 adds a SECOND way to be in that state: a wind-down was
	// opened, so nothing has been dispatched YET and the member is still on the
	// old machine until the 收口. Reporting a clean landed 200 there would be the
	// same silent false-success T-8655 removed for the unreachable-warden case.
	if dec.DispatchUnlanded || windDown {
		pending := true
		dto.RelocationPending = &pending
	}
	// …and WHICH of the two it is (T-927a). The wind-down case is a deliberate
	// deferral, not a delivery failure, so the caller must be able to hold back
	// the "nothing was dispatched" alert for it. Reported separately rather than
	// by narrowing relocation_pending: that field's meaning is on the frozen
	// wire and existing readers depend on it covering both.
	if windDown {
		deferred := true
		dto.RelocationDeferred = &deferred
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
	// 🔴 CANCELLING A WAKE IS NOT A GRACEFUL STOP (T-7526). Read BEFORE the
	// mutation below — stamping stopping_since is itself what ends the waking
	// projection.
	//
	// decideDown's first branch is `if !obs.Online { converged offline }`, and a
	// waking member is BY DEFINITION not online (deriveLiveness projects waking
	// only when !Online). So for the whole waking window the cadence dispatched
	// NOTHING: the process the earlier START already put on the machine booted
	// anyway, connected, went green, and only then — as a now-online member with
	// desired_state=offline — did decideDown arm its 120s grace. The owner's
	// 取消 read as "the button did nothing", which is exactly what it did.
	//
	// There is also nothing to wind down: a member that has not connected has
	// taken no work, so the grace window it cannot enter would buy nothing.
	cancellingWake := PresenceState(*m, nowSecs(), s.hub.IsOnline(m.ID)) ==
		MemberPresenceWaking
	m.DesiredState = DesiredStateOffline
	// …UNCONDITIONAL with ONE exception: a stop epoch that a FORCE-stop opened
	// must not be re-stamped into a softer one. The SSE stop gate separates
	// "close-out in flight" (admit the reconnect) from "cut off deliberately"
	// (refuse it) by comparing the two anchors — forced_stop_at >= this epoch's
	// stopping_since means forced — so re-stamping stopping_since to now would
	// move a force-stopped member to the ADMIT side, and the 下線 arm runs no
	// clock, so nothing would collect it afterwards. Found by independent
	// review; reachable through the API/MCP surface (the cockpit offers no 下線
	// button in stopping/stopped, but that is a UI fact, not a gate).
	//
	// The three conditions are each load-bearing. `stopping_since > 0` is what
	// keeps this narrow to a LIVE forced epoch: activate clears the stop anchors
	// but deliberately KEEPS forced_stop_at (it is the durable record that a
	// past session was cut off), so testing forced_stop_at alone would strip the
	// soft-offboard admission from every member that was ever force-stopped.
	//
	// Consequence, deliberate: a forced epoch's anchor stops moving, so this
	// call no longer restarts the grace clock for it. Nothing reads it that way
	// — the 下線 arm returns decisionNone while the soft grace is on, and
	// offboardKindOf answers soft for desired-offline without consulting the
	// anchor's age.
	if !forcedEpochLive(*m) {
		m.StoppingSince = nowSecs()
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if cancellingWake {
		// The same immediate robust STOP force-stop uses. NOT widened to the
		// online case: a live member's stop keeps its graceful grace.
		s.dispatchRobustStopNow(m.ID)
	}
	// Event-driven reconcile: arm the 120s grace clock immediately (a graceful
	// stop dispatches NOTHING inside the grace; the eventual robust stop stays
	// the cadence's job). Still armed after a cancel — the raw dispatch above
	// does not touch the reconcile store, and the cadence STOP arm is its
	// idempotent backstop.
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
	// The record that this session was cut off (T-a9d6). Force-stop sends no
	// notice — the recipient is about to stop existing, so a sentence meant to
	// change its behaviour has no one to change — and that silence is exactly
	// why the fact has to be written down: everything a killed session leaves
	// behind is indistinguishable from what a session with nothing to say
	// leaves behind. Stamped on the member itself so the NEXT generation and the
	// cockpit can both see it; its own column, so no later snapshot erases it.
	m.ForcedStopAt = nowSecs()
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.SetMemberForcedStopAt(m.ID, m.ForcedStopAt); err != nil {
		// Best-effort, and deliberately not fatal: the kill below is the point
		// of the call and the member IS being force-stopped. Reporting a
		// failure here would say "force-stop failed" about a member that is
		// about to be killed anyway — the same shape as the dismiss sweep.
		taskLog("force-stop %s: forced_stop_at not recorded: %v", m.ID, err)
	}
	s.dispatchRobustStopNow(m.ID)
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/refocus — needs a live session (409 otherwise);
// stamps refocus_since.
//
// The gate is the SSE connection rather than the presence projection, for the
// same reason restart_self's is: a member that has begun closing out projects
// `stopping`, and refusing the owner there would mean 重新聚焦 stops working on
// an agent that is mid-hand-off — the moment he is most likely to press it.
func (s *apiServer) HandleRefocusMemberApiMembersMemberIdRefocusPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if !s.hub.IsOnline(m.ID) || !aRefocusStampWouldReachTheAgent(*m) {
		writeError(w, http.StatusConflict,
			"refocus requires the member to have a live session and to be wanted "+
				"online (§3.4 #14)")
		return
	}
	m.RefocusSince = nowSecs()
	m.RefocusOp = refocusOpRefocus
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
// SAME 下線程序 wake as a member and reports its own presence through these
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
// recycle markers. The reported model is stored separately from the owner's
// launch configuration.
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
	m.RefocusOp = ""
	m.StoppedSince = 0.0
	// 🔴 …but NOT the stop trace of a member the owner has already cancelled
	// (T-7526). Clearing a stale anchor is right for an ORDINARY boot; doing it
	// unconditionally erased the only mark a mid-wake 取消 left behind, so the
	// agent that was already booting when the cancel landed came up painting a
	// fresh green over an intent that is still offline. The intent itself
	// (desired_state) is what says whether this boot is wanted.
	if m.DesiredState == DesiredStateOnline {
		m.StoppingSince = 0.0
	}
	if body.Model != nil {
		m.ActualModel = *body.Model
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
// That FIRST report fires the event-driven collect, so kill→respawn happens
// immediately rather than on the next ~30s tick. It no longer matters what
// opened the offboard: an agent that says it is done is collected either way
// (owner rc-b08d49dc3b03), and desired_state alone decides whether a new
// generation follows.
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
	// 🔴 A stopped-report is now ALWAYS collected (owner 2026-08-16, card
	// rc-b08d49dc3b03 option ①: 「收掉並重生」).
	//
	// It used to be collected only when something was already collecting it —
	// a refocus epoch was in flight. That was sound while the offboard sequence
	// was shown ONLY to a session being collected: the last step always had a
	// receiver. Then the notice began telling agents to close out on their own
	// (T-c382) and the sequence became a document any session could work
	// (T-c9c0), which opened a path nobody was waiting at the end of: an agent
	// finished its close-out, reported stopped, and NOTHING happened. It stayed
	// alive holding a session it had already declared finished — and the sweep
	// that clears stale stopping anchors erased the evidence, painting it green
	// again (the owner's T-2123 report, and the previous generation of THIS
	// member lived it).
	//
	// desired_state decides what follows, and neither arm needs a special case
	// here: online respawns on the next tick's plain START, offline stays down.
	recycleKill := m.StoppedSince <= 0.0
	if m.StoppedSince <= 0.0 {
		m.StoppedSince = nowSecs()
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
// recycle orchestration (the agent's own RecycleHook → 下線程序 wake →
// report_stopped → server kill/respawn) carries the rest. Nothing is dispatched
// here (same as refocus_member — no reconcileMemberNow).
//
// Two abuse guards refuse LOUDLY (readable by the agent):
//   - LIVE-SESSION-ONLY (409): a self-restart is meaningless with no live
//     session to recycle. 🔴 The test is the SSE connection, not the presence
//     projection. Those differ for exactly the caller this endpoint exists for:
//     the offboard notice says 「work the sequence below, then call
//     restart_self yourself」, step 1 of that sequence is report_stopping, and
//     that stamps the anchor which makes PresenceState project `stopping`. So
//     an agent doing precisely what it was told was refused — and once a
//     close-out's anchor stopped being swept away every tick (T-2123) the
//     refusal lasted the whole soft window instead of clearing on the next
//     tick. A session holding an open stream has something to recycle; that is
//     the whole question here.
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
	if !s.hub.IsOnline(m.ID) || !aRefocusStampWouldReachTheAgent(*m) {
		writeError(w, http.StatusConflict,
			"restart_self requires a live session to recycle, on a member that is "+
				"still wanted online")
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
	m.RefocusOp = refocusOpRestartSelf
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
