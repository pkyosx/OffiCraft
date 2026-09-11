package main

// api_roles.go — the role journal: user-custom context block and role
// definitions. role_def is an OWNER OVERLAY over the file seeds; reset is an
// idempotent tombstone; a custom role hard-deletes with a complete cascade.

import (
	"encoding/json"
	"net/http"
	"sort"
	"unicode/utf8"
)

func historyJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// ── user-custom context block ────────────────────────────────────────────────

// GET /api/global-context — the folded user-custom ADDITIVE block.
func (s *apiServer) HandleGetGlobalContextApiGlobalContextGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/global-context — whole-block replace ({text}).
func (s *apiServer) HandleReplaceGlobalContextApiGlobalContextPost(w http.ResponseWriter, r *http.Request) {
	var body GlobalContextReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	text := body.Text
	// T-2d99 wipe guard: emptying a block that had content needs to be said
	// out loud. /api/global-context/reset is the dedicated way back to empty.
	if !(body.AllowShrink != nil && *body.AllowShrink) {
		current, err := s.foldUserContextDTO()
		if err != nil {
			internalError(w, err)
			return
		}
		if WholeDocWipeBlocked(current.Text, text) {
			writeError(w, http.StatusBadRequest,
				docWipeRefusal("global context", ", or use reset_global_context"))
			return
		}
	}
	if err := s.dal.SaveWithDocumentHistory("global_context", "global", currentActor(r), userContextSnapshotIn, func(ex sqlExecer) error {
		return putUserContextOn(ex, UserContext{Text: text, Tombstoned: false})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil, audienceOwnerOnly(), requestTrigger(r))
	// T-91: the receipt is READ BACK from the fold rather than assembled from
	// `text`. is_default is the one field a caller cannot predict from the verb
	// it called, and only the fold knows it — assembling it here would have to
	// guess, which is exactly the mistake the old `IsDefault: false` made.
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, globalContextReceiptOf(dto))
}

// globalContextReceiptOf reduces the read face's DTO to the write face's
// receipt. Both write verbs go through it so they cannot answer with two
// different shapes for one document.
func globalContextReceiptOf(dto *globalContextDTO) globalContextReceiptDTO {
	return globalContextReceiptDTO{
		IsDefault: dto.IsDefault,
		SizeChars: utf8.RuneCountInString(dto.Text),
		Sha256:    receiptSha256(dto.Text),
	}
}

// POST /api/global-context/reset — idempotent tombstone back to empty.
func (s *apiServer) HandleResetGlobalContextApiGlobalContextResetPost(w http.ResponseWriter, r *http.Request) {
	if err := s.dal.SaveWithDocumentHistory("global_context", "global", currentActor(r), userContextSnapshotIn, func(ex sqlExecer) error {
		return putUserContextOn(ex, UserContext{Text: "", Tombstoned: true})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil, audienceOwnerOnly(), requestTrigger(r))
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, globalContextReceiptOf(dto))
}

// ── role definitions ─────────────────────────────────────────────────────────

// listRoleKeys is the role roster in wire order: seed roles FIRST, then every
// custom role (non-tombstoned overlay with no file seed). Shared by GET
// /api/roles and the peek_doc_sizes overview so the two can never disagree
// about which roles exist.
func (s *apiServer) listRoleKeys() ([]string, error) {
	keys := []string{}
	seeds := map[string]bool{}
	for _, roleKey := range seedRoleKeys() {
		seeds[roleKey] = true
		keys = append(keys, roleKey)
	}
	overlays, err := s.dal.ListRoleDefs()
	if err != nil {
		return nil, err
	}
	for _, overlay := range overlays {
		if seeds[overlay.RoleKey] || overlay.Tombstoned {
			continue
		}
		keys = append(keys, overlay.RoleKey)
	}
	return keys, nil
}

// GET /api/roles — seed roles (folded with any owner edit) FIRST, then every
// custom role (non-tombstoned overlay with no file seed).
//
// The rows carry NO definition_md: a listing is where a caller picks a role,
// and the persona body is the bulk of the document. Each row still reports
// size_chars / cap_chars measured on the folded document, so "which definition
// is nearly full" is answerable without the text; get_role reads the one you
// picked. The fold itself is unchanged and shared with get_role, so the two
// faces cannot disagree about is_default / is_seed / the size.
func (s *apiServer) HandleListRolesApiRolesGet(w http.ResponseWriter, r *http.Request) {
	dtos := []roleDefListItemDTO{}
	keys, err := s.listRoleKeys()
	if err != nil {
		internalError(w, err)
		return
	}
	for _, roleKey := range keys {
		dto, err := s.foldRoleDefDTO(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if dto != nil {
			dtos = append(dtos, newRoleDefListItemDTO(*dto))
		}
	}
	writeJSON(w, http.StatusOK, dtos)
}

// GET /api/roles/{role} — one folded role definition (unknown → 404).
func (s *apiServer) HandleGetRoleApiRolesRoleGet(w http.ResponseWriter, r *http.Request, role string) {
	dto, err := s.foldRoleDefDTO(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if dto == nil {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/roles — create ONE custom role + its ONE founding member. The
// server mints both ids; the definition starts from the fixed template; the
// member starts offline; member_name omitted ⇒ picked from the name pool.
func (s *apiServer) HandleCreateRoleApiRolesPost(w http.ResponseWriter, r *http.Request) {
	var body RoleCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "name") {
		return
	}
	name := trimString(body.Name)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "role requires a name")
		return
	}
	if body.Effort != nil && !validEffort(*body.Effort) {
		writeError(w, http.StatusUnprocessableEntity,
			"effort must be one of [high low max medium xhigh]; got '"+*body.Effort+"'")
		return
	}
	// UNSET when the caller names none — the cockpit's 招攬新成員 sends only a
	// name, so this is THE path a founding member is born on. Leaving it empty
	// hands the choice to resolveEmptyRuntimeForPlacement at placement time
	// (T-ae8b), instead of pinning every new member to claude at birth.
	runtime := ""
	if body.Runtime != nil {
		runtime = string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
	}
	memberName := trimmedOrEmpty(body.MemberName)
	if memberName == "" {
		members, err := s.dal.ListMembers()
		if err != nil {
			internalError(w, err)
			return
		}
		taken := make([]string, 0, len(members))
		for _, m := range members { // removed rows included — audit names never double
			taken = append(taken, m.Name)
		}
		memberName = PickMemberName(taken, nil)
	}
	roleKey := "r-" + newHexID(12)
	if err := s.dal.PutRoleDef(RoleDef{
		RoleKey:      roleKey,
		Name:         name,
		DefinitionMD: CustomRoleTemplateMD,
		Tombstoned:   false,
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+roleKey, nil, audienceOwnerOnly(), requestTrigger(r))
	effort := strOrEmpty(body.Effort)
	if effort == "" {
		effort = "medium"
	}
	member := Member{
		ID:               "m-" + newHexID(12),
		Name:             memberName,
		Kind:             KindStaff,
		RoleKey:          roleKey,
		Runtime:          runtime,
		Model:            trimmedOrEmpty(body.Model),
		Effort:           effort,
		DesiredState:     DesiredStateOffline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}
	if err := s.putMember(member, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// T-91: the two MINTED IDS and the (possibly server-CHOSEN) member name are
	// the whole of what this write produced that the caller could not compute.
	// The role's definition_md is the shipped CustomRoleTemplateMD every custom
	// role starts on — a constant, readable through get_role — and the member
	// row is readable through get_member; neither is news.
	writeJSON(w, http.StatusOK, roleCreateResultDTO{
		RoleKey:    roleKey,
		MemberID:   member.ID,
		MemberName: member.Name,
	})
}

// POST /api/roles/{role} — edit ({name?, definition_md?}). Unknown → 404. A
// SEED role is name-locked (a supplied name is IGNORED, not rejected).
func (s *apiServer) HandleUpdateRoleApiRolesRolePost(w http.ResponseWriter, r *http.Request, role string) {
	var body RoleDefUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	current, err := s.foldRoleDefDTO(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	name := current.Name
	if body.Name != nil && seedRoleName(role) == "" {
		name = *body.Name
	}
	definitionMD := current.DefinitionMD
	if body.DefinitionMd != nil {
		definitionMD = *body.DefinitionMd
	}
	// T-ae38 hard cap on DUTY. Duty is the one role-journal segment that had no
	// cap at all until this ticket, and it is checked HERE and at the
	// document-history restore door (api_document_history.go, case
	// "role_definition") — BOTH, because either alone is decorative: edit down
	// to 999 and then restore a 4,000-char earlier revision and the cap is gone.
	// One read, reused by the response below, so the size the caller is told is
	// provably the number its write was judged against.
	//
	// Same three-line rule as every other capped doc (DocCapBlocked): an
	// already-over-cap Duty is never truncated, but its next write must come
	// out SHORTER. The shipped assistant seed now sits well UNDER the default
	// (see dutyCapCharsDefault), so on day one that rule binds nothing that
	// ships — it exists for hand-written Duties that grow past the cap.
	cap := s.dutyCap()
	if DocCapBlocked(cap, current.DefinitionMD, definitionMD) {
		writeError(w, http.StatusBadRequest,
			docCapRefusal(cap, "role definition doc", current.DefinitionMD, definitionMD))
		return
	}
	// The NAME is not versioned (owner ruling, T-1f39), so a write that only
	// renames the role retains nothing — otherwise a rename would push a real
	// revision of the TEXT out of the three retained slots without changing a
	// word of it. Same rule the task manual's two series follow.
	streams := roleDefHistoryStreams(role, currentActor(r), definitionMD != current.DefinitionMD)
	if err := s.dal.SaveWithDocumentHistories(streams, func(ex sqlExecer) error {
		return putRoleDefOn(ex, RoleDef{
			RoleKey:      role,
			Name:         name,
			DefinitionMD: definitionMD,
			Tombstoned:   false,
		})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	// T-91: `definition_md` no longer rides home — the caller sent it. It is
	// still assembled from the LOCALS rather than from a re-read, which is what
	// the cap comment above promises: the size the caller is told is provably
	// the number its write was judged against. `name` is the field that earns
	// its place here, because a rename of a seed role is silently ignored a few
	// lines up and this is the only place that says so.
	writeJSON(w, http.StatusOK, roleDefReceiptDTO{
		Key:       role,
		Name:      name,
		IsDefault: false, // an overlay now exists — FoldRoleDef reads that as not-default
		IsSeed:    seedRoleName(role) != "",
		SizeChars: utf8.RuneCountInString(definitionMD),
		CapChars:  cap,
		Sha256:    receiptSha256(definitionMD),
	})
}

// roleDefReceiptOf reduces the read face's DTO to the write face's receipt, for
// the verbs that ANSWER FROM A RE-READ (reset). update_role assembles its own
// from the values it just judged — see the comment there.
func roleDefReceiptOf(dto *roleDefDTO) roleDefReceiptDTO {
	return roleDefReceiptDTO{
		Key:       dto.Key,
		Name:      dto.Name,
		IsDefault: dto.IsDefault,
		IsSeed:    dto.IsSeed,
		SizeChars: dto.SizeChars,
		CapChars:  dto.CapChars,
		Sha256:    receiptSha256(dto.DefinitionMD),
	}
}

// POST /api/roles/{role}/reset — tombstone the overlay back to the seed
// (unknown SEED role → 404: there must be a seed to reset to).
func (s *apiServer) HandleResetRoleApiRolesRoleResetPost(w http.ResponseWriter, r *http.Request, role string) {
	if seedRoleName(role) == "" {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	current, err := s.foldRoleDefDTO(role)
	if err != nil || current == nil {
		internalError(w, err)
		return
	}
	if err := s.dal.SaveWithDocumentHistory("role_definition", role, currentActor(r), roleDefSnapshotIn(role), func(ex sqlExecer) error {
		return putRoleDefOn(ex, RoleDef{RoleKey: role, Tombstoned: true})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	dto, err := s.foldRoleDefDTO(role)
	if err != nil || dto == nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roleDefReceiptOf(dto))
}

// DELETE /api/roles/{role} — HARD-delete a CUSTOM role + everything it owns.
// Seed role → 403; unknown → 404; any online member → 409; then the complete
// cascade (members hard-deleted with their conversations, receipts,
// in-memory observation entries, and finally the overlay itself).
func (s *apiServer) HandleDeleteRoleApiRolesRoleDelete(w http.ResponseWriter, r *http.Request, role string) {
	if seedRoleName(role) != "" {
		writeError(w, http.StatusForbidden,
			"role '"+role+"' is a built-in seed role and cannot be deleted")
		return
	}
	overlay, err := s.dal.GetRoleDef(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if overlay == nil || overlay.Tombstoned {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	all, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	var members []Member
	var live []string
	for _, m := range all {
		if m.RoleKey != role {
			continue
		}
		members = append(members, m)
		if m.RosterStatus == RosterStatusActive && s.hub.IsOnline(m.ID) {
			live = append(live, m.ID)
		}
	}
	if len(live) > 0 {
		sort.Strings(live)
		msg := "role '" + role + "' has online member(s): "
		for i, id := range live {
			if i > 0 {
				msg += ", "
			}
			msg += id
		}
		writeError(w, http.StatusConflict, msg+" — stop them before deleting")
		return
	}
	deletedMsgs, deletedAtts, deletedReads := 0, 0, 0
	removedIDs := []string{}
	for _, m := range members {
		msgs, atts, err := s.dal.DeleteChatInvolving(m.ID)
		if err != nil {
			internalError(w, err)
			return
		}
		deletedMsgs += msgs
		deletedAtts += atts
		if msgs > 0 {
			// Cascade delta parity (repository.delete_chat_involving): fans
			// iff anything was deleted; refetch-only, no payload. Owner-only:
			// the payload carries no from/to to address peers (agents don't
			// act on a chat deletion — they re-list on their next fetch), and
			// the removed member m is being hard-deleted; the owner cockpit is
			// the one view that must refresh.
			s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+m.ID, nil, audienceOwnerOnly(), requestTrigger(r))
		}
		reads, err := s.dal.DeleteChatReadsInvolving(m.ID)
		if err != nil {
			internalError(w, err)
			return
		}
		deletedReads += reads
		if reads > 0 {
			s.hub.Publish("chat_read", "patch", "chat_read", wireOwnerID+"::"+m.ID, nil, audienceOwnerOnly(), requestTrigger(r))
		}
		s.telemetry.Delete(m.ID)
		s.gauge.Delete(m.ID)
		if _, err := s.dal.HardDeleteMember(m.ID); err != nil {
			internalError(w, err)
			return
		}
		s.hub.Publish("member", "remove", "member", wireOwnerID+"::"+m.ID, nil,
			audienceMembers(m.ID), requestTrigger(r))
		removedIDs = append(removedIDs, m.ID)
	}
	// T-3809: the role's insight goes with the role. Deliberately NOT reported
	// in the response DTO — that would be a new wire field, and the count
	// answers nothing a caller acts on; the delta below is what any open
	// surface actually needs.
	deletedInsight, err := s.dal.DeleteInsightForRole(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if deletedInsight > 0 {
		s.hub.Publish("insight", "patch", "insight", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	}
	if _, err := s.dal.DeleteRoleDef(role); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "remove", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, roleDeleteResultDTO{
		Role:                   role,
		RemovedMemberIDs:       removedIDs,
		DeletedChatMessages:    deletedMsgs,
		DeletedChatAttachments: deletedAtts,
		DeletedChatReads:       deletedReads,
	})
}
