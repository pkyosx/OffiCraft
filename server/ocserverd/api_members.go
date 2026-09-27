package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Owner-approved 10-minute floor; deliberately distinct from the context-high
// boot-storm guard's MinBootSecs.
const minSelfRestartSecs = 600.0

// writeMemberOn is putMember's write on the caller's transaction; the caller
// publishes (publishMemberPatch) after commit.
func writeMemberOn(tx *sql.Tx, m Member) error {
	if err := ValidateMember(m); err != nil {
		return err
	}
	return putMemberOn(tx, m)
}

func (s *apiServer) putMember(m Member, trigger string) error {
	if err := ValidateMember(m); err != nil {
		return err
	}
	if err := s.dal.PutMember(m); err != nil {
		return err
	}
	s.publishMemberPatch(m, trigger)
	return nil
}

func (s *apiServer) putMemberOwnerOnly(m Member, trigger string) error {
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
	s.hub.Publish("member", op, "member", wireOwnerID+"::"+m.ID,
		memberDeltaPayload(m), audienceOwnerOnly(), trigger)
	return nil
}

// 🔴 A failed putMember rolls the latch back: the anchor write is already durable,
// and a non-zero prior would make every later collect read "already reported" and
// dispatch nothing, forever.
func (s *apiServer) collectMemberStop(m *Member, trigger string) error {
	_, prior := collectWindDownRow(windDownAnchorRowOfMember(m), nowSecs())
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		return err
	}
	if err := s.putMember(*m, trigger); err != nil {
		m.StoppedSince = prior
		if rerr := s.persistMemberWindDownAnchors(*m); rerr != nil {
			reconcileLog("collect %s: latch rollback failed: %v", m.ID, rerr)
		}
		return err
	}
	s.bankLiveCost(m.ID)
	s.dispatchRobustStopNow(m.ID)
	return nil
}

// Binds the last_op* write to the member delta: those columns left PutMember's SET
// list, so a caller that wrote them and forgot publishMemberPatch would silently
// stop the cockpit refreshing.
// ⚠️ MEMBER ROWS ONLY: worker callers write through s.dal.SetMemberLastOp directly
// and keep their own publish.
func (s *apiServer) persistMemberOpReceipt(m Member, trigger string) error {
	if err := s.dal.SetMemberLastOp(m.ID, m.LastOp, m.LastOpOK, m.LastOpLog,
		m.LastOpReason, m.LastOpAt); err != nil {
		return err
	}
	s.publishMemberPatch(m, trigger)
	return nil
}

// 🔴 CALL IT BEFORE THE WHOLE-ROW WRITE — the opposite order from
// persistMemberOpReceipt, and load-bearing. putMember fans the delta that the
// wind-down hook in cli/ocagent (shouldWindDown) keys on, and these columns are
// what it reads: fan first and the agent refetches a row without the anchors,
// reads "no wind-down", and never stops. DO NOT "unify" the two orders: that
// race reintroduced turns nothing red. (On a new row the UPDATE is a no-op and
// PutMember's INSERT carries the anchors.)
// ⚠️ Cost: if the row write then fails, the anchors are durable with no delta and
// a 500; the ladder gate refuses a LOWER rung, so after a failed 加速停止 a plain
// 重新聚焦 bounces until 加速停止 is pressed again. Bounded and visible, unlike the
// race it prevents.
func (s *apiServer) persistMemberWindDownAnchors(m Member) error {
	return s.persistWindDownAnchors(windDownAnchorRowOfMember(&m))
}

// A worker row IS a member row (DAL.PutOutsourceWorker is
// PutMember(memberFromWorker(w))), so these are the same four columns.
// 🔴 A windDownAnchorRow's aliasing is decided at the CALL SITE and both readings
// compile: windDownAnchorRowOf…(&x) on a value aliases a COPY (the caller must
// persist it); on an existing *Member / *OutsourceWorker it mutates THE CALLER'S
// row. Nothing type-checks the difference and no test distinguishes it — read the
// receiver's declaration before copying a call. Taking a value here is what keeps
// a persist call from mutating the caller's snapshot.
func (s *apiServer) persistWorkerWindDownAnchors(w OutsourceWorker) error {
	return s.persistWindDownAnchors(windDownAnchorRowOfWorker(&w))
}

// By pointer because openWindDownRow / collectWindDownRow / clearWindDownRow
// (member_ownerop_winddown.go) write through it — one body for both populations.
type windDownAnchorRow struct {
	ID            string
	StoppingSince *float64
	StoppedSince  *float64
	RefocusSince  *float64
	RefocusOp     *string
}

// Pure address-taking adapters; must stay that way. 🔴 The worker one must NOT go
// through memberFromWorker: it mints activated_ts as a side effect.
func windDownAnchorRowOfMember(m *Member) windDownAnchorRow {
	return windDownAnchorRow{
		ID:            m.ID,
		StoppingSince: &m.StoppingSince,
		StoppedSince:  &m.StoppedSince,
		RefocusSince:  &m.RefocusSince,
		RefocusOp:     &m.RefocusOp,
	}
}

func windDownAnchorRowOfWorker(w *OutsourceWorker) windDownAnchorRow {
	return windDownAnchorRow{
		ID:            w.ID,
		StoppingSince: &w.StoppingSince,
		StoppedSince:  &w.StoppedSince,
		RefocusSince:  &w.RefocusSince,
		RefocusOp:     &w.RefocusOp,
	}
}

func (s *apiServer) persistWindDownAnchors(row windDownAnchorRow) error {
	return s.dal.SetMemberWindDownAnchors(row.ID, *row.StoppingSince,
		*row.StoppedSince, *row.RefocusSince, *row.RefocusOp)
}

// Split from putMember for single-column writers: marking a column insertOnly and
// forgetting this call silently stops the cockpit converging.
func (s *apiServer) publishMemberPatch(m Member, trigger string) {
	op := "patch"
	if m.RosterStatus == RosterStatusRemoved {
		op = "remove"
	}
	// Reaches the member's own connection (cli/ocagent shouldWindDown keys on a delta
	// naming self) plus the owner cockpit (spec/sse.md §4).
	s.hub.Publish("member", op, "member", wireOwnerID+"::"+m.ID, s.offboardDeltaPayload(m),
		audienceMembers(m.ID), trigger)
}

func memberDeltaPayload(m Member) map[string]any {
	return map[string]any{
		"id":            m.ID,
		"name":          m.Name,
		"status":        m.RosterStatus,
		"desired_state": m.DesiredState,
		"owner_id":      wireOwnerID,
	}
}

// Owner ruling (2026-08-16, rc-66b82a584c4d): the server pushes the notice in the
// frame rather than the agent fetching it. ⚠️ It rides EVERY write to a
// wound-down row; the client de-duplicates by the sentence it last printed. An
// empty notice omits the key: the client's fallback arms on the key being absent.
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

// Kinds come from winddownKindFor, which the clock (recycleGraceFor / decideDown)
// also reads: the sentence and the clock must move together, or a countdown is
// announced that nothing collects, or a hand-off is cut with no warning. No
// soft→final promotion (owner, rc-c540367065ad).
// ⚠️ `now` is deliberately unread: no arm turns on a clock today, and a future arm
// that needs one must take it from here rather than a global.
func offboardKindOf(m Member, now float64) (kind string, carries bool) {
	_ = now
	if m.DesiredState == DesiredStateOffline {
		// 🔴 This SOFT is hard-coded, deliberately NOT via winddownKindFor: 下線 is not a
		// wind-down cause and runs no countdown (owner, rc-27d1710174dd). Routing it
		// through the cause default would couple two rulings that merely coincide.
		// 🔴 Force-stop sends NO notice (owner ruling, reconfirmed c-5c8bc3d7362d; do not
		// change). Force-stop stamps offline + stopping_since before publishing, so
		// without the gracefulStopEpochOpen test below a killed member would receive a
		// full soft notice.
		if gracefulStopEpochOpen(m) {
			// The one exception: an owner-pressed 加速停止 on this stop. It asks
			// winddownKindFor because the clock (decideDown) reads the same function.
			if kind, clocked := winddownKindFor(m.RefocusOp); clocked {
				return kind, true
			}
			return offboardKindSoft, true
		}
		return "", false
	}
	if m.RefocusSince <= 0 {
		return "", false
	}
	kind, _ = winddownKindFor(m.RefocusOp)
	return kind, true
}

const (
	offboardKindSoft  = "soft"
	offboardKindFinal = "final"
)

// stopping_since > 0 scopes it to a LIVE epoch: activate keeps forced_stop_at as
// the durable record of a past cut-off.
// 🔴 The >= is LOAD-BEARING: force-stop stamps both anchors from two nowSecs()
// calls with no I/O between, so equal values are the NORMAL path (measured).
// Tidying it into > breaks force-stop.
func forcedEpochLive(m Member) bool {
	return m.ForcedStopAt > 0.0 && m.StoppingSince > 0.0 &&
		m.ForcedStopAt >= m.StoppingSince
}

// ⚠️ decideDown's `accelerated` arm (reconcile.go) tests StoppingSince > 0 with no
// forced term. The asymmetry is deliberate; closing it changes behaviour.
// 🔴 Re-check call sites by the TERM, not this name — a hand-written copy never
// names this function:
//
//	grep -rn 'forcedEpochLive(' --include='*.go' server/ | grep -v _test.go
//
// Every hit must be forcedEpochLive's declaration, this body, or a site carrying
// the anchor STOP-EPOCH-TERM-AUDIT with a written reason.
func gracefulStopEpochOpen(m Member) bool {
	return m.StoppingSince > 0.0 && !forcedEpochLive(m)
}

// 🔴 Re-stamping a live FORCED epoch to now would flip forcedEpochLive's >= and move
// the row to the graceful side, so the arm that must stay silent would speak.
// STOP-EPOCH-TERM-AUDIT: asks forcedEpochLive WITHOUT stopping_since > 0 on
// purpose — opening the FIRST stop epoch is exactly when there is no anchor yet.
func stopEpochAnchor(m Member, now float64) float64 {
	if forcedEpochLive(m) {
		return m.StoppingSince
	}
	return now
}

// No {where}/position clause (owner decision, T-6f44 #4): an agent that received
// one closed out no differently.
func (s *apiServer) offboardNoticeFor(m Member, kind string) string {
	notice := s.winddownNoticeText(kind, winddownDeadlineOf(m, s.reconcileConfigLive()))
	if notice == "" {
		return ""
	}
	if clause := s.offboardManualWriteBackFor(m); clause != "" {
		notice += "\n\n" + clause
	}
	return notice
}

// OUTSOURCE ONLY, per the owner's ruling: a 正職 outlives any one task, so naming
// one task's manual would be the wrong address.
func (s *apiServer) offboardManualWriteBackFor(m Member) string {
	if m.Kind != KindOutsource || m.LinkedTaskID == nil || *m.LinkedTaskID == "" {
		return ""
	}
	t, err := s.dal.GetTask(*m.LinkedTaskID)
	if err != nil || t == nil {
		return ""
	}
	// An ad-hoc task (no type) has no manual — the same criterion
	// decideTaskCloseNudge uses.
	if t.TypeKey == "" {
		return ""
	}
	// The document, not a Go copy (owner decision, T-6f44 #6). BODY only: the worker's
	// ticket has not necessarily ended, so the opening 「任務 {task_no} 已結束。」 would
	// be a false claim here.
	return s.taskEventBodyText(docKindTaskCloseout)
}

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
	s.publishMemberPatch(m, trigger)
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

// A fresh ava- id makes every replacement cache-safe.
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

// ?fields=light: unread_count, presence, machine and last_op* are NOT computed
// (honest-empty); a consumer must not read them as known values.
func (s *apiServer) HandleListMembersApiMembersGet(w http.ResponseWriter, r *http.Request, params HandleListMembersApiMembersGetParams) {
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	light := trimmedOrEmpty(params.Fields) == "light"

	var unread map[string]int
	if !light {
		var err error
		unread, err = s.unreadCountsForRequest(r)
		if err != nil {
			internalError(w, err)
			return
		}
	}
	var machineNames map[string]string
	var tele, gauge map[string]map[string]any
	var accountDisplay func(string) string
	var typeNames map[string]string
	now := nowSecs()
	if !light {
		machineNames, err = s.dal.MachineDisplayNames()
		if err != nil {
			internalError(w, err)
			return
		}
		tele = s.telemetry.Snapshot()
		gauge = s.gauge.Snapshot()
		accountDisplay, err = s.accountDisplayFold(r, tele)
		if err != nil {
			internalError(w, err)
			return
		}
		typeNames = s.taskTypeDisplayNames()
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
		if m.Kind == KindOutsource {
			worker := workerFromMember(m)
			task, err := s.dal.GetTask(worker.TaskID)
			if err != nil {
				internalError(w, err)
				return
			}
			out = append(out, s.projectWorker(worker, task, unread[m.ID], now, tele, gauge, machineNames, accountDisplay, typeNames))
			continue
		}
		out = append(out, s.newMemberDTO(m, roleName, s.observedHost(m), unread[m.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

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
			"effort must be one of [high low max medium xhigh]; got '"+*body.Effort+"'")
		return
	}
	// Left UNSET when none is named, so resolveEmptyRuntimeForPlacement can pick by
	// the machine it lands on; hard-coding claude makes that resolver unreachable.
	runtime := ""
	if body.Runtime != nil {
		runtime = string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
	}
	kind, err := CanonicalKind(strOrEmpty(body.Kind))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// Staff only — owner ruling rc-3989498e0c8f ("make it a protection, not a
	// comment"). Wardens and outsource workers have their own birth paths.
	if kind != KindStaff {
		writeError(w, http.StatusUnprocessableEntity,
			"POST /api/members hires staff only; got kind '"+kind+"'. "+
				"A warden is born by onboarding its machine through POST /api/machines "+
				"(the machine id becomes the warden's member id); an outsource worker is "+
				"minted by the outsource scheduler when a task is handed out. "+
				"Neither can be created here")
		return
	}
	// A staff member without a role is undefined (owner, rc-beac3f5b355c). Checked on
	// the TRIMMED value to match the privilege gate above, or a whitespace role_key
	// would slip through both.
	if kind == KindStaff {
		roleKey := strOrEmpty(body.RoleKey)
		if trimmedOrEmpty(body.RoleKey) == "" {
			writeError(w, http.StatusUnprocessableEntity,
				"a staff member requires a role_key; hire the member through "+
					"POST /api/roles, which mints a role and its member together")
			return
		}
		available, err := s.hireRoleKeyAvailable(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if !available {
			writeError(w, http.StatusUnprocessableEntity, "role '"+roleKey+"' not found")
			return
		}
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
	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: m.ID})
}

func (s *apiServer) HandleGetMemberApiMembersMemberIdGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMemberForItemRead(memberId)
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
	// Computed, not a literal 0: the cockpit re-reads one member on a chat delta, and
	// 0 zeroed the badge the delta announced.
	unread, err := s.unreadCountsForRequest(r)
	if err != nil {
		internalError(w, err)
		return
	}
	if m.Kind == KindOutsource {
		worker := workerFromMember(*m)
		task, err := s.dal.GetTask(worker.TaskID)
		if err != nil {
			internalError(w, err)
			return
		}
		machineNames, err := s.dal.MachineDisplayNames()
		if err != nil {
			internalError(w, err)
			return
		}
		tele := s.telemetry.Snapshot()
		gauge := s.gauge.Snapshot()
		accountDisplay, err := s.accountDisplayFold(r, tele)
		if err != nil {
			internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s.projectWorker(worker, task, unread[m.ID], nowSecs(), tele, gauge, machineNames, accountDisplay, s.taskTypeDisplayNames()))
		return
	}
	writeJSON(w, http.StatusOK, s.newMemberDTO(*m, roleName, s.observedHost(*m), unread[m.ID]))
}

func (s *apiServer) HandleUpdateMemberApiMembersMemberIdPatch(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if m.Kind == KindOutsource {
		if body.Name != nil {
			writeError(w, http.StatusUnprocessableEntity,
				"an outsource worker's codename is task-bound and cannot be renamed")
			return
		}
		s.handleSetOutsourceWorkerModel(w, r, memberId, body)
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
	// Only launch intents are baked into a boot frame, so only they recycle; a rename
	// must never recycle.
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
		// Compare NORMALIZED: "" is claude (what buildStartFrame stamps), so a raw compare
		// charges a wind-down for a no-op save. The write below still lands ("" →
		// "claude" is a stated intent); only the recycle is withheld.
		launchIntentChanged = launchIntentChanged ||
			NormalizeRuntime(runtime) != NormalizeRuntime(m.Runtime)
		m.Runtime = runtime
	}
	if body.Effort != nil {
		if !validEffort(*body.Effort) {
			writeError(w, http.StatusUnprocessableEntity,
				"effort must be one of [high low max medium xhigh]; got '"+*body.Effort+"'")
			return
		}
		launchIntentChanged = launchIntentChanged || *body.Effort != m.Effort
		m.Effort = *body.Effort
	}
	heldDown := false
	if launchIntentChanged {
		heldDown = !s.armMemberOwnerOpHandover(m, memberOpRuntimeModel) &&
			m.DesiredState == DesiredStateOffline
		if heldDown {
			if aStopWasEverAskedFor(*m) {
				stampRestartIntent(m)
				stampMemberOpReceipt(m, memberRestartQueuedReceipt(memberOpRuntimeModel), nowSecs())
			} else {
				stampMemberOpReceipt(m, memberHeldDownReceipt(memberOpRuntimeModel), nowSecs())
			}
		}
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// 🔴 The receipt must land BEFORE the launch-intent setters below. The gate reads
	// the STORED intent: after the setters, a failure here leaves the new value with
	// no receipt and the retry never re-enters this branch — held down silently,
	// permanently. Gated on heldDown so this snapshot never overwrites a receipt a
	// reconcile tick stamped meanwhile.
	if heldDown {
		if err := s.persistMemberOpReceipt(*m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
	}
	// Setters only for fields this request carried: restating another field would
	// write back this handler's stale read.
	// 🔴 They run AFTER the whole-row write (and the epoch anchors land before it).
	// Setters first + a failure = new model stored with NO epoch, never converging;
	// this way a failure leaves the epoch open on the OLD value — one wasted recycle,
	// and the retry still sees a difference. ⚠️ A 500 here does not mean nothing was
	// stored: a `name` in the same request has already landed.
	if body.Model != nil {
		if err := s.dal.SetMemberModel(m.ID, m.Model); err != nil {
			internalError(w, err)
			return
		}
	}
	if body.Runtime != nil {
		if err := s.dal.SetMemberRuntime(m.ID, m.Runtime); err != nil {
			internalError(w, err)
			return
		}
	}
	if body.Effort != nil {
		if err := s.dal.SetMemberEffort(m.ID, m.Effort); err != nil {
			internalError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: m.ID})
}

func (s *apiServer) HandleActivateMemberApiMembersMemberIdActivatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberActivateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if m.Kind == KindOutsource {
		s.handleRestartOutsourceWorker(w, r, memberId, body)
		return
	}
	sessionAlive := s.hub.IsOnline(m.ID)
	if body.MachineId != nil && *body.MachineId != "" {
		if _, err := s.resolveMachine(*body.MachineId); err != nil {
			writeResolveError(w, err, "machine", *body.MachineId)
			return
		}
	}
	m.StoppingSince = 0.0
	m.WakingSince = 0.0
	m.DesiredState = DesiredStateOnline
	// 活化 spends the queued 起來, or it would fire a second start after the next 下線.
	clearRestartIntent(m)
	if body.MachineId != nil {
		m.DesiredMachineID = *body.MachineId
		// The pin moves through its sole writer, so an activate without machine_id leaves
		// it alone instead of writing back this handler's read (which used to undo a
		// concurrent relocate).
		if err := s.dal.SetMemberDesiredMachineID(m.ID, m.DesiredMachineID); err != nil {
			internalError(w, err)
			return
		}
	}
	if !sessionAlive {
		clearWindDownRow(windDownAnchorRowOfMember(m))
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	put := s.putMember
	if sessionAlive {
		put = s.putMemberOwnerOnly
	}
	if err := put(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	dec := reconcileDecision{}
	if !sessionAlive {
		s.bankLiveCost(m.ID)
		s.dispatchRobustStopNow(m.ID)
		dec = s.reconcileMemberNow(m.ID)
	}
	receipt := memberActivateReceiptDTO{ID: m.ID}
	// POSITIVE determination: reconcileOne also downgrades a START to none when
	// buildStartFrame fails, WITHOUT setting DispatchUnlanded — so ask whether a START
	// went out, not whether a known failure fired.
	if dec.Command != reconcileCmdStart && !s.hub.IsOnline(m.ID) {
		receipt.ActivationPending = true
		// A stall arm that names no code gets the generic reason; a new arm should name
		// itself at the decision site, not be guessed here.
		reason := dec.ReasonCode
		if reason == "" {
			reason = spawnReasonWardenLost + ": 活化 was recorded, but nothing has been " +
				"dispatched yet — the machine's warden did not take the start. It will " +
				"be retried; if it stays here, check that machine"
		}
		s.stampMemberOpBlocked(m.ID, reason, nowSecs())
		receipt.LastOpReason = reason
	}
	writeJSON(w, http.StatusOK, receipt)
}

// A live member gets a graceful wind-down; the move happens at the 收口 (its own
// report_stopped or the owner's force-stop). No recycle-grace ceiling:
// winddownKindFor answers soft for a relocate. Unlike 活化 (which cancels the
// stop), 改機器 queues behind it. "auto" is NOT exempt from the machine resolve:
// IsOnline("auto") is always false, so it pinned members to an unreachable
// destination.
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
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		// An id naming no STAFF member falls through to the outsource worker relocate
		// (gate rc-2786636f30e5): resolveMember deliberately excludes kind='outsource', and
		// ow- ids never collide with staff ids.
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
	m.DesiredMachineID = machineID
	if err := s.dal.SetMemberDesiredMachineID(m.ID, machineID); err != nil {
		internalError(w, err)
		return
	}
	// The pin lands BEFORE the epoch — the reverse of the launch-intent faces, on
	// purpose: relocate arms unconditionally, so a retry re-dispatches regardless, and
	// the delta the agent wakes on already names the destination.
	windDown := s.armMemberOwnerOpHandover(m, memberOpRelocate)
	heldDown := !windDown && m.DesiredState == DesiredStateOffline
	if heldDown {
		// Owner (2026-08-30): 改機器 is a 重啟 intent, so a stopped member comes back up on
		// the new pin.
		if aStopWasEverAskedFor(*m) {
			stampRestartIntent(m)
			stampMemberOpReceipt(m, memberRestartQueuedReceipt(memberOpRelocate), nowSecs())
		} else {
			stampMemberOpReceipt(m, memberHeldDownReceipt(memberOpRelocate), nowSecs())
		}
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if heldDown {
		if err := s.persistMemberOpReceipt(*m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
	}
	dec := s.reconcileMemberNow(m.ID)
	receipt := agentRelocateReceiptDTO{ID: m.ID}
	// relocation_pending also covers an opened wind-down (nothing dispatched yet).
	if dec.DispatchUnlanded || windDown {
		receipt.RelocationPending = true
	}
	// Deferred is reported separately because relocation_pending's meaning is on the
	// frozen wire and existing readers rely on it covering both cases.
	if windDown {
		receipt.RelocationDeferred = true
	}
	writeJSON(w, http.StatusOK, receipt)
}

// Twin of the worker receipt respawnWorkerForOwnerOp writes. Only the held-down
// case gets a receipt: an offline member picks the value up at its next wake, and
// stamping it would be noise.
func memberHeldDownReceipt(op string) string {
	return spawnReasonHeldDown + ": the " + op + " was saved, but nothing was " +
		"started — this member is stopped; 活化 it when you want it to run"
}

// 🔴 The harm is on the NEXT generation: activate clears neither refocus_since nor
// stopped_since, so a leftover marker survives 下線 → 活化 and decideUp's recycle
// arm robust-stops the brand-new session on its first tick. stopping_since /
// stopped_since / forced_stop_at are left alone: forced_stop_at is the durable
// cut-off record the next generation reads.
func clearMemberHandoverMarker(m *Member) {
	m.RefocusSince = 0.0
	m.RefocusOp = ""
}

// One body for both populations (they share the member table). The guard against
// a mis-wired adapter lives at the handler seam (TestVerbPopulationParityMatrix
// block ①), deliberately not on this helper: a helper-level parity test once left
// both call sites deletable with the suite green.
type stopVerbRow struct {
	DesiredState     *string
	RefocusSince     *float64
	RefocusOp        *string
	RestartAfterStop *bool
	StoppingSince    *float64
}

func stopVerbRowOfMember(m *Member) stopVerbRow {
	return stopVerbRow{
		DesiredState:     &m.DesiredState,
		RefocusSince:     &m.RefocusSince,
		RefocusOp:        &m.RefocusOp,
		RestartAfterStop: &m.RestartAfterStop,
		StoppingSince:    &m.StoppingSince,
	}
}

func stopVerbRowOfWorker(w *OutsourceWorker) stopVerbRow {
	return stopVerbRow{
		DesiredState:     &w.DesiredState,
		RefocusSince:     &w.RefocusSince,
		RefocusOp:        &w.RefocusOp,
		RestartAfterStop: &w.RestartAfterStop,
		StoppingSince:    &w.StoppingSince,
	}
}

// `snapshot` is the row BEFORE this call: stopEpochAnchor must read the pre-stop
// anchors, not a half-mutated row.
//   - refocus_since/refocus_op cleared: on staff, the destructive next-generation
//     reader armRefocusEpoch describes; on workers, autoHandoverWorker would
//     kill+RESPAWN a worker the owner just held down. Both reasons hold.
//   - restart_after_stop cleared: 後蓋前 — any queued 起來 is cancelled.
//
// 🔴 activate does NOT clear stopped_since, so a new session can come up ONLINE
// carrying the previous generation's report with no epoch (下線 → 活化).
func applyStopVerbRow(row stopVerbRow, snapshot Member, now float64) {
	*row.DesiredState = DesiredStateOffline
	*row.RefocusSince = 0.0
	*row.RefocusOp = ""
	*row.RestartAfterStop = false
	*row.StoppingSince = stopEpochAnchor(snapshot, now)
}

func (s *apiServer) HandleDeactivateMemberApiMembersMemberIdDeactivatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if m.Kind == KindOutsource {
		s.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost(w, r, memberId)
		return
	}
	// 🔴 Cancelling a wake is not a graceful stop (T-7526). Read BEFORE the mutation:
	// stamping stopping_since ends the waking projection. A waking member is not
	// online, and decideDown's first branch treats !Online as converged, so without
	// this the cadence dispatched nothing and the cancel did nothing.
	sessionAlive := s.hub.IsOnline(m.ID)
	cancellingWake := PresenceState(*m, nowSecs(), sessionAlive) ==
		MemberPresenceWaking
	applyStopVerbRow(stopVerbRowOfMember(m), *m, nowSecs())
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if !sessionAlive && !cancellingWake {
		if err := s.collectMemberStop(m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
	}
	if cancellingWake {
		// Not widened to the online case: a live member gets the soft window and is
		// collected by its own report_stopped or the owner's 加速停止 / 強制停止.
		s.dispatchRobustStopNow(m.ID)
	}
	// Arms no clock (owner ruling). Still run after a cancel: the raw dispatch above
	// does not touch the reconcile store.
	s.reconcileMemberNow(m.ID)
	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: m.ID})
}

func (s *apiServer) HandleForceStopMemberApiMembersMemberIdForceStopPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if m.Kind == KindOutsource {
		s.HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost(w, r, memberId)
		return
	}
	m.DesiredState = DesiredStateOffline
	clearMemberHandoverMarker(m)
	clearRestartIntent(m)
	forcedAt := nowSecs()
	if m.StoppingSince <= 0.0 || m.StoppingSince > forcedAt {
		m.StoppingSince = forcedAt
	}
	// Force-stop sends no notice, so this record is the only trace a session was cut
	// off; forward-only, so a stale snapshot cannot erase it.
	m.ForcedStopAt = forcedAt
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.SetMemberForcedStopAt(m.ID, m.ForcedStopAt); err != nil {
		// Not fatal: the kill below is the point, and "force-stop failed" would be false.
		taskLog("force-stop %s: forced_stop_at not recorded: %v", m.ID, err)
	}
	// Bank before the kill, matching the worker funnel; bankLiveCost pops, so the later
	// disconnect edge is idempotent.
	s.bankLiveCost(m.ID)
	s.dispatchRobustStopNow(m.ID)
	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: m.ID})
}

const acceleratedStopNeedsAnOpenWindDownMsg = "加速停止 escalates a wind-down that is " +
	"already open — this member has not been asked to stop. Press 停止 (deactivate) " +
	"or 重新聚焦 (refocus) first"

// Middle rung of 停止 → 加速停止 → 強制停止 (owner 2026-08-21). 🔴 It escalates, never
// initiates (409 otherwise): a member never asked to stop would get a deadline it
// never heard about. It does not reopen rc-27d1710174dd: that ruling forbids a
// clock the SERVER starts; this one is the owner's press.
// On the 換手 arm refocus_since is re-stamped: promoting in place would put the
// deadline at the ORIGINAL stamp, already past, collecting the member on the tick
// that announced it. A force-stopped epoch is refused (no reader).
func (s *apiServer) HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if m.Kind == KindOutsource {
		s.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost(w, r, memberId)
		return
	}
	// The notice travels on the member's own stream; a clock nobody hears is a silent
	// deadline.
	if !s.hub.IsOnline(m.ID) {
		writeError(w, http.StatusConflict,
			"加速停止 requires a live session — there is nothing to accelerate on a "+
				"member that is not connected")
		return
	}
	now := nowSecs()
	switch {
	case m.DesiredState == DesiredStateOffline:
		if !gracefulStopEpochOpen(*m) {
			writeError(w, http.StatusConflict, acceleratedStopNeedsAnOpenWindDownMsg)
			return
		}
		// The grace runs from THIS press. Other anchors untouched: zeroing stopped_since
		// would erase the agent's 「我收完了」 and cancel a collection it already earned.
		m.StoppingSince = now
	case m.RefocusSince > 0.0:
		m.RefocusSince = now
	default:
		writeError(w, http.StatusConflict, acceleratedStopNeedsAnOpenWindDownMsg)
		return
	}
	m.RefocusOp = refocusOpAcceleratedStop
	// On the 換手 arm desired_state stays ONLINE (a hurried handover, not a stop);
	// deliberately not widened — outside the owner's [0] ruling.
	clearRestartIntent(m)
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.reconcileMemberNow(m.ID)
	reconcileLog("加速停止: %s on the %s arm (collect at %.0f or on the stopped report)",
		m.ID, m.DesiredState, winddownDeadlineOf(*m, s.reconcileConfigLive()))
	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: m.ID})
}

// Gated on the SSE connection, not presence: a member mid-hand-off projects
// `stopping`, exactly when the owner is likeliest to press this.
func (s *apiServer) HandleRefocusMemberApiMembersMemberIdRefocusPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if m.Kind == KindOutsource {
		s.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost(w, r, memberId)
		return
	}
	// Owner (2026-08-30): refocus on a stopped member only records 「起來」; the stop in
	// flight keeps its stage and anchors.
	if !aRefocusStampWouldReachTheAgent(*m) && aStopWasEverAskedFor(*m) {
		stampRestartIntent(m)
		stampMemberOpReceipt(m, memberRestartQueuedReceipt(refocusOpRefocus), nowSecs())
		if err := s.putMember(*m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
		// The receipt must persist BEFORE the tick below: the tick can spend the intent and
		// stamp its own receipt, which a later persist would overwrite.
		if err := s.persistMemberOpReceipt(*m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
		s.reconcileMemberNow(m.ID)
		if fresh, err := s.dal.GetMember(m.ID); err == nil && fresh != nil {
			m = fresh
		}
		writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: m.ID})
		return
	}
	if !s.hub.IsOnline(m.ID) || !aRefocusStampWouldReachTheAgent(*m) {
		writeError(w, http.StatusConflict,
			"refocus requires the member to have a live session and to be wanted "+
				"online (§3.4 #14)")
		return
	}
	// The ladder only goes forward (owner, 2026-08-24): a backward press would clear a
	// deadline an agent was told about. Refused, not silently downgraded.
	if !armRefocusEpoch(m, refocusOpRefocus, nowSecs()) {
		writeError(w, http.StatusConflict,
			"refocus is 停止 and this member is already further along the "+
				"wind-down ladder (下線 → 加速 → 強制); a later stage is never "+
				"replaced by an earlier one")
		return
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: m.ID})
}

func (s *apiServer) HandleDismissMemberApiMembersMemberIdDelete(w http.ResponseWriter, r *http.Request, memberId string) {
	if _, err := s.resolveMember(memberId, staffOnly); err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	var m Member
	err := s.dal.inTx(func(tx *sql.Tx) error {
		cur, err := resolveMemberOn(tx, memberId, staffOnly)
		if err != nil {
			return err
		}
		cur.RosterStatus = RosterStatusRemoved
		cur.DesiredState = DesiredStateOffline
		clearRestartIntent(cur)
		m = *cur
		return writeMemberOn(tx, m)
	})
	if err != nil {
		writeResolveTxError(w, err, "member", memberId)
		return
	}
	s.publishMemberPatch(m, requestTrigger(r))
	// The asker is gone, so its waiting cards are retired. Best-effort: the dismissal
	// has already committed in a transaction of its own.
	if _, err := s.expireWaitingCardsByAuthor(m.ID, nowSecs(), requestTrigger(r)); err != nil {
		taskLog("dismiss %s: reply-card sweep failed (cards left waiting): %v", m.ID, err)
	}
	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: m.ID})
}

// Does not exclude kind='outsource': workers report through these self endpoints
// too (T-ea82). Admin verbs refuse ow- rows by passing staffOnly.
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

// Owner ruling rc-fe6451abe579: report_waking ends the previous generation's
// authority. 🔴 Use the caller's OWN iat, never nowSecs(): requireAuth compares
// STRICTLY LESS THAN, so the caller always passes its own floor. No iat → no floor.
func (s *apiServer) stampAgentIatFloor(r *http.Request) error {
	iat, ok := claimsFromContext(r.Context())["iat"].(float64)
	if !ok || iat <= 0 {
		return nil
	}
	return s.dal.SetMemberAgentIatFloor(currentActor(r), iat)
}

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
	// Runs FIRST and is fatal: stamping after the wake would leave the previous
	// generation's tokens live; a floor raised for a wake that then 500s is harmless
	// (own iat). Unconditional across kinds — the READ side exempts wardens.
	if err := s.stampAgentIatFloor(r); err != nil {
		internalError(w, err)
		return
	}
	// A new generation counts spend from zero, so its first cost is not read as an
	// increase over the previous session's.
	s.startAccountSpendSession(m.ID)
	if m.Kind == KindOutsource {
		// Through the worker funnel under outsourceMu: a member-path putMember would race
		// the tick's read-modify-write.
		fresh, werr := s.workerReportWaking(m.ID, body.Model, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeSelfReportReceipt(w, *fresh)
		return
	}
	m.WakingSince = nowSecs()
	m.RefocusSince = 0.0
	m.RefocusOp = ""
	m.StoppedSince = 0.0
	// 🔴 Not for a member the owner already cancelled (T-7526): that anchor is the only
	// trace of a mid-wake 取消.
	if m.DesiredState == DesiredStateOnline {
		m.StoppingSince = 0.0
	}
	if body.Model != nil {
		m.ActualModel = *body.Model
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeSelfReportReceipt(w, *m)
}

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
		s.writeSelfReportReceipt(w, *fresh)
		return
	}
	openWindDownRow(windDownAnchorRowOfMember(m), nowSecs())
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeSelfReportReceipt(w, *m)
}

func (s *apiServer) HandleReportStoppedApiSelfStoppedPost(w http.ResponseWriter, r *http.Request) {
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		fresh, stopEffect, werr := s.workerReportStopped(m.ID, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeSelfReportStopReceipt(w, *fresh, stopEffect)
		return
	}
	// 🔴 A stopped-report is ALWAYS collected (owner, rc-b08d49dc3b03 option ①).
	collect, stopEffect, prior := decideStoppedReport(windDownAnchorRowOfMember(m), nowSecs())
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		// 🔴 Roll the latch back.
		m.StoppedSince = prior
		if rerr := s.persistMemberWindDownAnchors(*m); rerr != nil {
			reconcileLog("stopped-report %s: latch rollback failed: %v", m.ID, rerr)
		}
		internalError(w, err)
		return
	}
	// Dispatch AFTER putMember so the delta (→ agent RecycleHook) lands before the STOP.
	if collect {
		s.dispatchRobustStopNow(m.ID)
	}
	s.writeSelfReportStopReceipt(w, *m, stopEffect)
}

func decideStoppedReport(row windDownAnchorRow, now float64) (collect bool, stopEffect string, prior float64) {
	latched, prior := collectWindDownRow(row, now)
	if !latched {
		return false, stopEffectAlreadyReported, prior
	}
	return true, stopEffectCollected, prior
}

// 🔴 The live-session guard tests the SSE connection, not presence: the notice's
// first step is report_stopping, which projects `stopping`, so a presence test
// would refuse exactly the caller this endpoint exists for.
// The minimum-liveness guard anchors on the server's boot_ts (SSE first-connect);
// a missing boot_ts (server restart) FAILS OPEN via bootStormTripped.
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
		fresh, werr := s.workerRestartSelf(m.ID, now, requestTrigger(r))
		if errors.Is(werr, errWindDownLadderBackwards) {
			// Deliberately the same sentence as the staff arm: one rule.
			writeError(w, http.StatusConflict,
				"restart_self is 停止 and you are already further along the "+
					"wind-down ladder (下線 → 加速 → 強制); finish the close-out you "+
					"were given instead")
			return
		}
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		if reason := trimmedOrEmpty(body.Reason); reason != "" {
			reconcileLog("recycle: %s self-restart (restart_self); reason: %s", m.ID, reason)
		} else {
			reconcileLog("recycle: %s self-restart (restart_self)", m.ID)
		}
		s.writeSelfReportReceipt(w, *fresh)
		return
	}
	if !armRefocusEpoch(m, refocusOpRestartSelf, now) {
		writeError(w, http.StatusConflict,
			"restart_self is 停止 and you are already further along the "+
				"wind-down ladder (下線 → 加速 → 強制); finish the close-out you "+
				"were given instead")
		return
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if reason := trimmedOrEmpty(body.Reason); reason != "" {
		reconcileLog("recycle: %s self-restart (restart_self); reason: %s", m.ID, reason)
	} else {
		reconcileLog("recycle: %s self-restart (restart_self)", m.ID)
	}
	s.writeSelfReportReceipt(w, *m)
}
