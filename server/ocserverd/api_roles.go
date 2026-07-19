package main

// api_roles.go — the role journal: user-custom context block, role
// definitions, and per-role lessons (handlers.handle_get_global_context …
// handle_replace_lessons). role_def / lessons are OWNER OVERLAYS over the
// file seeds; reset is an idempotent tombstone; a custom role hard-deletes
// with a complete cascade.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
)

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
	if !decodeJSONBody(w, r, &body) {
		return
	}
	text := strOrEmpty(body.Text)
	if err := s.dal.PutUserContext(UserContext{Text: text, Tombstoned: false}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, globalContextDTO{
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
		OrgName:       s.orgNameSnapshot(),
	})
}

// POST /api/global-context/reset — idempotent tombstone back to empty.
func (s *apiServer) HandleResetGlobalContextApiGlobalContextResetPost(w http.ResponseWriter, r *http.Request) {
	if err := s.dal.PutUserContext(UserContext{Text: "", Tombstoned: true}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil, audienceOwnerOnly(), requestTrigger(r))
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── role definitions ─────────────────────────────────────────────────────────

// GET /api/roles — seed roles (folded with any owner edit) FIRST, then every
// custom role (non-tombstoned overlay with no file seed).
func (s *apiServer) HandleListRolesApiRolesGet(w http.ResponseWriter, r *http.Request) {
	dtos := []roleDefDTO{}
	seeds := map[string]bool{}
	for _, roleKey := range seedRoleKeys() {
		seeds[roleKey] = true
		dto, err := s.foldRoleDefDTO(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if dto != nil {
			dtos = append(dtos, *dto)
		}
	}
	overlays, err := s.dal.ListRoleDefs()
	if err != nil {
		internalError(w, err)
		return
	}
	for _, overlay := range overlays {
		if seeds[overlay.RoleKey] || overlay.Tombstoned {
			continue
		}
		dto, err := s.foldRoleDefDTO(overlay.RoleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if dto != nil {
			dtos = append(dtos, *dto)
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
			"effort must be one of [high low medium]; got '"+*body.Effort+"'")
		return
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
		Kind:             KindAssistant,
		RoleKey:          roleKey,
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
	roleDTO, err := s.foldRoleDefDTO(roleKey)
	if err != nil || roleDTO == nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roleCreateResultDTO{
		Role:   *roleDTO,
		Member: s.newMemberDTO(member, name, "", 0),
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
	if err := s.dal.PutRoleDef(RoleDef{
		RoleKey:      role,
		Name:         name,
		DefinitionMD: definitionMD,
		Tombstoned:   false,
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, roleDefDTO{
		Key:           role,
		Name:          name,
		DefinitionMD:  definitionMD,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
		IsSeed:        seedRoleName(role) != "",
	})
}

// POST /api/roles/{role}/reset — tombstone the overlay back to the seed
// (unknown SEED role → 404: there must be a seed to reset to).
func (s *apiServer) HandleResetRoleApiRolesRoleResetPost(w http.ResponseWriter, r *http.Request, role string) {
	if seedRoleName(role) == "" {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: role, Tombstoned: true}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	dto, err := s.foldRoleDefDTO(role)
	if err != nil || dto == nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// DELETE /api/roles/{role} — HARD-delete a CUSTOM role + everything it owns.
// Seed role → 403; unknown → 404; any online member → 409; then the complete
// cascade (members hard-deleted with their conversations, receipts, lessons,
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
	deletedLessons, err := s.dal.DeleteLessonsForRole(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if deletedLessons > 0 {
		// Cascade rides as patch keyed by the bare role
		// (repository.delete_lessons_for_role → _publish_overlay).
		s.hub.Publish("lessons", "patch", "lessons", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
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
		DeletedLessons:         deletedLessons,
	})
}

// ── lessons ──────────────────────────────────────────────────────────────────

// fillLessonsIdentityArgs folds the identity-derivable defaults into a
// get_lessons / replace_lessons / patch_lessons MCP call so an agent's lessons round-trip lands
// on the SAME per-role doc the boot context injects into its persona (T-d483).
//
// The two path params are REQUIRED by the route, so an MCP call that omits either
// one substitutes the empty string (mcp.go splitToolArguments) → the path degrades
// to /api/lessons/{role}/ (or //) which no longer matches the wildcard route, and
// the SPA fallback answers not_found — the reported learning-loop break. We close
// that here, once, at the tool boundary the agents actually use, mirroring
// buildBootContext's own key derivation:
//   - a blank task_type folds to the "general" seed bucket (seedLessonsTaskType);
//   - a blank role_key folds to the caller's OWN role — the roster's role_key for
//     the verified sub (resolveBootRoleKey, the same source the write authz reads).
//
// A non-agent caller (owner/machine) has no identity role, so a blank role_key is
// left untouched (that caller must name the role explicitly). The REST wire shape
// is unchanged — REST callers already pass both segments (conformance happy path).
func (s *apiServer) fillLessonsIdentityArgs(r *http.Request, name string, arguments map[string]any) {
	if name != "get_lessons" && name != "replace_lessons" && name != "patch_lessons" {
		return
	}
	if blankArg(arguments["task_type"]) {
		arguments["task_type"] = seedLessonsTaskType
	}
	if blankArg(arguments["role_key"]) && currentScope(r) == "agent" {
		member, err := s.dal.GetMember(currentActor(r))
		if err == nil && member != nil {
			arguments["role_key"] = resolveBootRoleKey("", member)
		}
	}
}

// blankArg reports whether an MCP argument is effectively unset — absent, null,
// or the empty string (the shapes splitToolArguments would turn into an empty
// path segment).
func blankArg(v any) bool {
	if v == nil {
		return true
	}
	str, ok := v.(string)
	return ok && str == ""
}

// lessonsWriteAuthz enforces the per-role lessons WRITE authz shared by
// replace_lessons and patch_lessons: an agent-scoped caller may write ONLY its
// own member's role_key (read from the roster by the verified sub, never a
// client field); any non-agent scope writes any role. Answers the error itself
// and reports whether the caller may proceed.
func (s *apiServer) lessonsWriteAuthz(w http.ResponseWriter, r *http.Request, roleKey string) bool {
	if currentScope(r) != "agent" {
		return true
	}
	member, err := s.dal.GetMember(currentActor(r))
	if err != nil {
		internalError(w, err)
		return false
	}
	memberRole := ""
	if member != nil {
		memberRole = member.RoleKey
	}
	if memberRole != roleKey {
		writeError(w, http.StatusForbidden,
			"an agent may only write its own role's lessons")
		return false
	}
	return true
}

// GET /api/lessons/{role_key}/{task_type} — the folded per-role lessons doc.
// READ is unrestricted for any authenticated identity.
func (s *apiServer) HandleGetLessonsApiLessonsRoleKeyTaskTypeGet(w http.ResponseWriter, r *http.Request, roleKey string, taskType string) {
	dto, err := s.foldLessonsDTO(roleKey, taskType)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/lessons/{role_key}/{task_type} — whole-doc replace. Per-role
// WRITE authz: an agent-scoped caller may write ONLY its own member's
// role_key (read from the roster by the verified sub, never a client field);
// any non-agent scope writes any role.
func (s *apiServer) HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(w http.ResponseWriter, r *http.Request, roleKey string, taskType string) {
	var body LessonsReplaceDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if !s.lessonsWriteAuthz(w, r, roleKey) {
		return
	}
	text := strOrEmpty(body.Text)
	if err := s.dal.PutLessons(Lessons{
		RoleKey:    roleKey,
		TaskType:   taskType,
		Text:       text,
		Tombstoned: false,
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("lessons", "patch", "lessons", wireOwnerID+"::"+roleKey+"::"+taskType, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, lessonsDTO{
		RoleKey:       roleKey,
		TaskType:      taskType,
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
	})
}

// POST /api/lessons/{role_key}/{task_type}/patch — anchor-addressed patch
// (T-8327). Write cost ∝ the CHANGE, not the doc: a whole-doc replace_lessons
// stops fitting in one model output as the doc grows (76k chars observed), so
// this is the primary write seam and replace stays the last resort.
//
// Semantics (spec/openapi.json is normative): edits apply IN ORDER against the
// doc get_lessons serves (overlay ⊕ seed fold); a non-empty `old` must match
// exactly once (0/>1 hits → flat 400, WHOLE batch rejected, zero writes — the
// unique anchor doubling as an optimistic lock under last-write-wins
// concurrency); an empty `old` appends. A patch that wipes the doc, or shrinks
// a substantial doc to <10%, is refused without allow_shrink=true (the r-76
// wipe-guard posture). Same per-role write authz as replace_lessons.
func (s *apiServer) HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost(w http.ResponseWriter, r *http.Request, roleKey string, taskType string) {
	var body LessonsPatchDTO
	if !decodeJSONBodyRequired(w, r, &body, "edits") {
		return
	}
	if len(body.Edits) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"edits requires at least one {old, new} entry")
		return
	}
	if !s.lessonsWriteAuthz(w, r, roleKey) {
		return
	}
	current, err := s.foldLessonsDTO(roleKey, taskType)
	if err != nil {
		internalError(w, err)
		return
	}
	edits := make([]LessonsEdit, len(body.Edits))
	for i, e := range body.Edits {
		edits[i] = LessonsEdit{Old: strOrEmpty(e.Old), New: strOrEmpty(e.New)}
	}
	next, err := ApplyLessonsEdits(current.Text, edits)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowShrink := body.AllowShrink != nil && *body.AllowShrink
	if !allowShrink && LessonsShrinkBlocked(current.Text, next) {
		writeError(w, http.StatusBadRequest,
			"patch would empty (or shrink to under a tenth of) the lessons doc — pass allow_shrink=true if this is intended, or use replace_lessons; nothing was written")
		return
	}
	if err := s.dal.PutLessons(Lessons{
		RoleKey:    roleKey,
		TaskType:   taskType,
		Text:       next,
		Tombstoned: false,
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("lessons", "patch", "lessons", wireOwnerID+"::"+roleKey+"::"+taskType, nil, audienceOwnerOnly(), requestTrigger(r))
	sum := sha256.Sum256([]byte(next))
	writeJSON(w, http.StatusOK, lessonsPatchResultDTO{
		RoleKey:       roleKey,
		TaskType:      taskType,
		AppliedEdits:  len(edits),
		Size:          len(next),
		Sha256:        hex.EncodeToString(sum[:]),
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
	})
}
