package main

// role_def is an OWNER OVERLAY over the file seeds; reset tombstones it.

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

func (s *apiServer) HandleGetGlobalContextApiGlobalContextGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) HandleReplaceGlobalContextApiGlobalContextPost(w http.ResponseWriter, r *http.Request) {
	var body GlobalContextReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	text := body.Text
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
	// The receipt is READ BACK from the fold: is_default is known only to the
	// fold, and assembling it here would have to guess.
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, globalContextReceiptOf(dto))
}

func globalContextReceiptOf(dto *globalContextDTO) globalContextReceiptDTO {
	return globalContextReceiptDTO{
		IsDefault: dto.IsDefault,
		SizeChars: utf8.RuneCountInString(dto.Text),
		Sha256:    receiptSha256(dto.Text),
	}
}

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

// Shared by GET /api/roles and the peek_doc_sizes overview so the two cannot
// disagree about which roles exist.
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
	// Left UNSET when the caller names none (the cockpit's 招攬新成員 sends only a
	// name): resolveEmptyRuntimeForPlacement picks at placement time instead of
	// pinning every new member to claude at birth.
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
		for _, m := range members { // removed rows included — names never repeat
			taken = append(taken, m.Name)
		}
		memberName = PickMemberName(taken, nil)
	}
	roleKey := "r-" + newHexID(12)
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
	if err := s.dal.inTx(func(tx *writeTx) error {
		if err := putRoleDefOn(tx, RoleDef{
			RoleKey:      roleKey,
			Name:         name,
			DefinitionMD: CustomRoleTemplateMD,
			Tombstoned:   false,
		}); err != nil {
			return err
		}
		return createMemberRowOn(tx, member)
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+roleKey, nil, audienceOwnerOnly(), requestTrigger(r))
	s.publishMemberPatch(member, requestTrigger(r))
	writeJSON(w, http.StatusOK, roleCreateResultDTO{
		RoleKey:    roleKey,
		MemberID:   member.ID,
		MemberName: member.Name,
	})
}

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
	// Duty cap: checked HERE and at the document-history restore door
	// (api_document_history.go, case "role_definition") — either alone is
	// decorative (edit down, then restore a longer revision). The receipt below
	// reuses this read, so the size reported is the one the write was judged by.
	cap := s.dutyCap()
	if DocCapBlocked(cap, current.DefinitionMD, definitionMD) {
		writeError(w, http.StatusBadRequest,
			docCapRefusal(cap, "role definition doc", current.DefinitionMD, definitionMD))
		return
	}
	// The NAME is not versioned (owner ruling T-1f39): a rename-only write
	// retains no revision.
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

func (s *apiServer) HandleResetRoleApiRolesRoleResetPost(w http.ResponseWriter, r *http.Request, role string) {
	if seedRoleName(role) == "" {
		existing, err := s.foldRoleDefDTO(role)
		if err != nil {
			internalError(w, err)
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "role '"+role+"' not found")
			return
		}
		writeError(w, http.StatusConflict, "reset is not applicable to role '"+role+
			"': it was created on this station and has no shipped version — only shipped roles can be reset")
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

func (s *apiServer) HandleDeleteRoleApiRolesRoleDelete(w http.ResponseWriter, r *http.Request, role string) {
	if seedRoleName(role) != "" {
		writeError(w, http.StatusForbidden,
			"role '"+role+"' is a built-in seed role and cannot be deleted")
		return
	}
	// Presence is read before the transaction: the hub's lock is refused inside one.
	online := s.hub.OnlineMembers()
	// roleMembers answers the role's members, or the 404 / 409 that refuses the
	// deletion.
	roleMembers := func(q sqlReader) ([]Member, error) {
		overlay, err := getRoleDefOn(q, role)
		if err != nil {
			return nil, err
		}
		if overlay == nil || overlay.Tombstoned {
			return nil, refuseInTx(http.StatusNotFound, "role '"+role+"' not found")
		}
		all, err := listMembersOn(q)
		if err != nil {
			return nil, err
		}
		var members []Member
		var live []string
		for _, m := range all {
			if m.RoleKey != role {
				continue
			}
			members = append(members, m)
			if m.RosterStatus == RosterStatusActive && online[m.ID] {
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
			return nil, refuseInTx(http.StatusConflict, msg+" — stop them before deleting")
		}
		return members, nil
	}
	if _, err := roleMembers(s.dal.rdb); err != nil {
		writeTxError(w, err)
		return
	}
	// The members are listed again inside the transaction that dismisses them: one
	// hired into the role or brought online in between is judged as it stands.
	var dismissed []Member
	deletedInsight := 0
	err := s.dal.inTx(func(tx *writeTx) error {
		members, err := roleMembers(tx)
		if err != nil {
			return err
		}
		dismissed = nil
		now := nowSecs()
		for i := range members {
			if err := dismissStaffOn(tx, &members[i], now); err != nil {
				return err
			}
			dismissed = append(dismissed, members[i])
		}
		if deletedInsight, err = deleteInsightForRoleOn(tx, role); err != nil {
			return err
		}
		_, err = deleteRoleDefOn(tx, role)
		return err
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	removedIDs := []string{}
	for _, m := range dismissed {
		s.finishStaffDismissal(m, requestTrigger(r))
		removedIDs = append(removedIDs, m.ID)
	}
	if deletedInsight > 0 {
		s.hub.Publish("insight", "patch", "insight", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	}
	s.hub.Publish("role_def", "remove", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, roleDeleteResultDTO{
		Role:             role,
		RemovedMemberIDs: removedIDs,
	})
}
