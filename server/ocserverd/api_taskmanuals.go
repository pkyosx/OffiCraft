package main

// api_taskmanuals.go — the 設定 › 任務手冊 surface (M3 contract §C.5): the
// shared read face, the agent-floor CONTENT writes (create a manual, partial
// edit of purpose / fields / SOP / learnings — owner ruling 2026-07-13:
// agents author manual content), the OWNER-ONLY governance face (the
// assignee setting — an agent supplying `assignee` on create/edit is a 403
// from the in-handler gate; delete stays requires=owner on the route table),
// and the AGENT's learnings write-back (whole-doc replace, the
// replace_lessons shape). Manuals ship EMPTY (SPEC §5.1: no seed, no
// tombstone); delete is refused while non-terminal tasks of the type exist.

import (
	"encoding/json"
	"net/http"
)

// resolveTaskManual returns the manual for typeKey (errNotFound when absent).
func (s *apiServer) resolveTaskManual(typeKey string) (*TaskManual, error) {
	m, err := s.dal.GetTaskManual(typeKey)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errNotFound
	}
	return m, nil
}

// manualDisplayLabel renders a manual/type reference for human- and
// agent-facing PROSE (boot context, nudges): the display name with the
// ADDRESSING type_key kept in parentheses — agents still call
// get_task_manual / write_task_learnings / create_task by key, so the key
// must never vanish from the text. Falls back to the bare key when no
// distinct display name exists (legacy manuals where display == key, or
// none at all).
func manualDisplayLabel(displayName, typeKey string) string {
	name := trimString(displayName)
	if name == "" || name == typeKey {
		return typeKey
	}
	return name + "（" + typeKey + "）"
}

// writeTaskManual is the common single-manual response tail.
func (s *apiServer) writeTaskManual(w http.ResponseWriter, m TaskManual) {
	dto, err := newTaskManualDTO(m)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// validateManualAssignee checks an incoming assignee object: {} unsets; a
// populated object must carry a legal kind — "member" (with a non-blank
// member_id) or "outsource". Outsource knobs (spec TaskManualUpdateDTO):
// `copies` >= 0 where 0 = 無限 (unlimited per-type parallel copies; absent
// = 1); `machine` is the spawn placement preference — "auto" or a machine
// id, any non-blank string (absent = "auto"). "" = OK, else the 400 message.
func validateManualAssignee(assignee map[string]any) string {
	if len(assignee) == 0 {
		return ""
	}
	kind, _ := assignee["kind"].(string)
	switch kind {
	case TaskExecutorMember:
		if memberID, _ := assignee["member_id"].(string); memberID == "" {
			return "assignee kind 'member' requires a member_id"
		}
	case TaskExecutorOutsource:
		if copies, ok := assignee["copies"]; ok {
			if n, isNum := copies.(float64); !isNum || n < 0 {
				return "assignee copies must be a number >= 0 (0 = unlimited)"
			}
		}
		if machine, ok := assignee["machine"]; ok {
			if m, isStr := machine.(string); !isStr || m == "" {
				return "assignee machine must be \"auto\" or a machine id"
			}
		}
	default:
		return "assignee kind must be 'member' or 'outsource'"
	}
	return ""
}

// callerMaySetAssignee enforces the owner-only assignee governance gate
// (owner ruling 2026-07-13): the assignee face — who/what executes a type
// (member binding / outsource headcount / machine placement) — stays owner
// governance even though manual CONTENT is agent-editable. False → the
// caller writes the 403.
func (s *apiServer) callerMaySetAssignee(r *http.Request) bool {
	return principalAtLeast(s.principalOfRequest(r), principalOwner)
}

const assigneeOwnerOnlyMsg = "assignee is owner-only governance — " +
	"only the owner may set who executes a task type"

// GET /api/task-manuals — the type cards (full DTOs; the list view shows
// type_key + purpose, the rest rides along — one DTO, no second shape).
//
// ?view=list (T-ec2c) is the ADDITIVE light projection for the surfaces that
// only need the type identity — the tasks/office 類型 filter (listTaskTypes
// reads type_key / display_name / purpose) — NOT the full manual body. It
// keeps the SAME taskManualDTO wire shape (additive, no new response schema)
// but HONEST-EMPTIES the heavy authored blobs the identity view never shows:
// sop_md, learnings (both free-form markdown, the bulk of a manual's bytes),
// fields, and assignee. The full-body consumers (設定 › 任務手冊 detail via the
// per-type GET, and any caller omitting the param) are byte-for-byte
// unchanged. The matching FE change stops the office 外包 panel from
// re-pulling this endpoint on chat SSE deltas at all (a chat line changes no
// manual) — together the office page's per-message re-download collapses.
func (s *apiServer) HandleListTaskManualsApiTaskManualsGet(w http.ResponseWriter, r *http.Request, params HandleListTaskManualsApiTaskManualsGetParams) {
	manuals, err := s.dal.ListTaskManuals()
	if err != nil {
		internalError(w, err)
		return
	}
	list := trimmedOrEmpty(params.View) == "list"
	out := []taskManualDTO{}
	for _, m := range manuals {
		if list {
			out = append(out, newTaskManualListItemDTO(m))
			continue
		}
		dto, err := newTaskManualDTO(m)
		if err != nil {
			internalError(w, err)
			return
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/task-manuals — create a blank manual (agent floor; owner ruling
// 2026-07-13: any agent may author a new task type). T-fa76 owner ruling:
// the type id is the SYSTEM's, the label is the human's — the caller passes
// `display_name` and the server MINTS "tm-"+hex12 as the type_key (returned
// in the DTO; later calls address by it). An explicit `type_key` is the
// LEGACY compat path (deprecated): taken verbatim as the id (duplicate →
// 409), with a blank display_name backfilled to it so old MCP callers'
// manuals still carry a display face. Both blank → 400. The optional
// assignee is the owner-only governance face: a non-owner supplying it is a
// 403; the owner's assignee is validated and applied.
func (s *apiServer) HandleCreateTaskManualApiTaskManualsPost(w http.ResponseWriter, r *http.Request) {
	var body TaskManualCreateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.Assignee != nil && !s.callerMaySetAssignee(r) {
		writeError(w, http.StatusForbidden, assigneeOwnerOnlyMsg)
		return
	}
	typeKey := trimString(strOrEmpty(body.TypeKey))
	displayName := trimString(strOrEmpty(body.DisplayName))
	if typeKey == "" {
		// The system-key path: the display name is the only user input; the
		// id is minted server-side (the r-/m- role-create posture).
		if displayName == "" {
			writeError(w, http.StatusBadRequest,
				"display_name must not be blank")
			return
		}
		typeKey = "tm-" + newHexID(12)
	} else if displayName == "" {
		// Legacy path backfill: the key doubles as the label so every manual
		// has a display face (the UI still falls back || typeKey anyway).
		displayName = typeKey
	}
	assigneeBlob := "{}"
	if body.Assignee != nil {
		if problem := validateManualAssignee(*body.Assignee); problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
		blob, err := json.Marshal(*body.Assignee)
		if err != nil {
			internalError(w, err)
			return
		}
		assigneeBlob = string(blob)
	}
	existing, err := s.dal.GetTaskManual(typeKey)
	if err != nil {
		internalError(w, err)
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict,
			"task manual '"+typeKey+"' already exists")
		return
	}
	m := TaskManual{
		TypeKey:     typeKey,
		DisplayName: displayName,
		Fields:      "[]",
		Assignee:    assigneeBlob,
		UpdatedTS:   nowSecs(),
	}
	if err := s.dal.PutTaskManual(m); err != nil {
		internalError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	s.writeTaskManual(w, m)
}

// GET /api/task-manuals/{type_key} — one manual in full (the intake's
// type-judgement AND the planner's blueprint read).
func (s *apiServer) HandleGetTaskManualApiTaskManualsTypeKeyGet(w http.ResponseWriter, r *http.Request, typeKey string) {
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	s.writeTaskManual(w, *m)
}

// POST /api/task-manuals/{type_key} — the partial manual edit (only supplied
// fields change — the role-def edit posture). Agent floor for the CONTENT
// fields (purpose / fields / sop_md / learnings); assignee stays the
// owner-only governance face — a non-owner supplying it is a 403.
func (s *apiServer) HandleUpdateTaskManualApiTaskManualsTypeKeyPost(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskManualUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.Assignee != nil && !s.callerMaySetAssignee(r) {
		writeError(w, http.StatusForbidden, assigneeOwnerOnlyMsg)
		return
	}
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	if body.Fields != nil {
		for _, f := range *body.Fields {
			if trimString(f.Name) == "" {
				writeError(w, http.StatusBadRequest,
					"field name must not be blank")
				return
			}
			// K1: an identity-key field must be required. A key that can be left
			// empty has no dedupe basis — the same root cause create_task's is_key
			// check guards — so it is rejected at the manual as well.
			isKey := f.IsKey != nil && *f.IsKey
			required := f.Required != nil && *f.Required
			if isKey && !required {
				writeError(w, http.StatusBadRequest,
					"identity-key field '"+trimString(f.Name)+
						"' must be required")
				return
			}
		}
	}
	if body.Assignee != nil {
		if problem := validateManualAssignee(*body.Assignee); problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
	}
	// All validated — apply the partial update.
	if body.DisplayName != nil {
		m.DisplayName = trimString(*body.DisplayName)
	}
	if body.Purpose != nil {
		m.Purpose = *body.Purpose
	}
	if body.SopMd != nil {
		m.SopMD = *body.SopMd
	}
	if body.Learnings != nil {
		m.Learnings = *body.Learnings
	}
	if body.Fields != nil {
		fields := make([]ManualField, 0, len(*body.Fields))
		for _, f := range *body.Fields {
			fields = append(fields, ManualField{
				Name:     trimString(f.Name),
				Required: f.Required != nil && *f.Required,
				IsKey:    f.IsKey != nil && *f.IsKey,
			})
		}
		blob, err := json.Marshal(fields)
		if err != nil {
			internalError(w, err)
			return
		}
		m.Fields = string(blob)
	}
	if body.Assignee != nil {
		blob, err := json.Marshal(*body.Assignee)
		if err != nil {
			internalError(w, err)
			return
		}
		m.Assignee = string(blob)
	}
	m.UpdatedTS = nowSecs()
	if err := s.dal.PutTaskManual(*m); err != nil {
		internalError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	s.writeTaskManual(w, *m)
}

// DELETE /api/task-manuals/{type_key} — hard delete (no seed to fall back
// to). Refused (409) while NON-terminal tasks of the type exist (SPEC §5.1);
// closed tasks never block.
func (s *apiServer) HandleDeleteTaskManualApiTaskManualsTypeKeyDelete(w http.ResponseWriter, r *http.Request, typeKey string) {
	if _, err := s.resolveTaskManual(typeKey); err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	open, err := s.dal.CountOpenTasksOfType(typeKey)
	if err != nil {
		internalError(w, err)
		return
	}
	if open > 0 {
		writeError(w, http.StatusConflict,
			"task manual '"+typeKey+"' still has open tasks — close them first")
		return
	}
	deleted, err := s.dal.DeleteTaskManual(typeKey)
	if err != nil {
		internalError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	writeJSON(w, http.StatusOK, taskManualDeleteResultDTO{
		TypeKey: typeKey, Deleted: deleted,
	})
}

// POST /api/task-manuals/{type_key}/learnings — the agent's task-close
// write-back: whole-doc replace (the replace_lessons posture — the agent
// reads, folds its experience in, writes the whole doc back).
func (s *apiServer) HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskLearningsReplaceDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	m.Learnings = strOrEmpty(body.Text)
	m.UpdatedTS = nowSecs()
	if err := s.dal.PutTaskManual(*m); err != nil {
		internalError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	s.writeTaskManual(w, *m)
}
