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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	// T-2d99 (mirror direction): strict decode, but NO required names. This is
	// a partial update — "only supplied fields change" is the contract, so an
	// absent key must stay legal. What must NOT stay legal is an UNKNOWN key:
	// this handler writes the SAME learnings document as write_task_learnings,
	// and the two tools spell the field differently (`learnings` here, `text`
	// there). The observed incident was that confusion in one direction; the
	// mirror — update_task_manual{text: "..."} — was answering 200 while
	// dropping the key, so the caller's new learnings silently vanished. That
	// is not a wipe (pointer fields, nil = unchanged) but it is the same bug
	// class: report success while doing nothing. Unknown key ⇒ 422, no write.
	if !decodeJSONBodyStrict(w, r, &body) {
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
	// T-2d99 — this is the handler that actually destroyed a manual. It used
	// the lenient decoder, so write_task_learnings{learnings: "..."} (the key
	// update_task_manual uses for THIS SAME document) had its only meaningful
	// key silently dropped, leaving body.Text nil → "" → the whole doc wiped,
	// with the 200 response echoing learnings: "". Strict + required now.
	var body TaskLearningsReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	// Belt to the strict decoder's braces: even a well-formed {"text": ""}
	// must not silently erase accumulated learnings.
	if !(body.AllowShrink != nil && *body.AllowShrink) && WholeDocWipeBlocked(m.Learnings, body.Text) {
		writeError(w, http.StatusBadRequest,
			"this would replace the existing learnings with an empty doc — pass allow_shrink=true "+
				"if that is intended; nothing was written")
		return
	}
	m.Learnings = body.Text
	m.UpdatedTS = nowSecs()
	if err := s.dal.PutTaskManual(*m); err != nil {
		internalError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	s.writeTaskManual(w, *m)
}

// POST /api/task-manuals/{type_key}/learnings/patch — anchor-addressed patch of
// a type's learnings (T-9ffd; the patch_lessons twin for task manuals).
// ApplyLessonsEdits is the SHARED engine — it is generic over the doc text, so
// the anchor/append/atomicity semantics are byte-identical to patch_lessons.
//
// Why this exists: the ONLY write face for learnings was whole-doc replace
// (write_task_learnings / update_task_manual.learnings). As a manual's
// learnings grows (30k chars observed on tm-05f7c776d6ff) re-typing the whole
// doc to add three lines stops fitting in one model output AND every re-type
// silently risks transcription loss (the tool answers 200 either way). This
// makes the write cost scale with the CHANGE, not the doc — and the unique
// anchor doubles as an optimistic lock under last-write-wins, so a concurrent
// write that moved the anchor turns the next patch into a 400 rather than a
// silent mis-splice. (It does NOT solve section-level concurrent overwrite of
// DIFFERENT anchors — that needs a version/etag lock, tracked separately.)
//
// Semantics: edits apply IN ORDER; a non-empty old must match exactly once
// (0/>1 → flat 400, WHOLE batch rejected, zero writes); an empty old appends.
// A patch that wipes the doc, or shrinks a substantial doc to <10%, is refused
// without allow_shrink=true (the r-76 wipe-guard posture). Same agent-floor
// authz as write_task_learnings (route Requires: principalAgent — manual
// CONTENT is agent-editable). Unknown type → 404.
func (s *apiServer) HandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskLearningsPatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if len(body.Edits) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"edits requires at least one {old, new} entry")
		return
	}
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	edits := make([]LessonsEdit, len(body.Edits))
	for i, e := range body.Edits {
		// T-2d99 shape (shared with patch_lessons): an edit carrying NEITHER old
		// NOR new is malformed, not a request to append nothing. Folding nil→""
		// would route it into the empty-old APPEND branch where appending "" is a
		// perfect no-op — the whole batch would answer 200 with an unchanged doc.
		// Refuse it; the whole batch is rejected and nothing is written, matching
		// the anchor-miss posture.
		if e.Old == nil && e.New == nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
				"edits[%d]: neither old nor new was given — an edit needs at least one of them "+
					"(empty old appends new); nothing was written", i))
			return
		}
		edits[i] = LessonsEdit{Old: strOrEmpty(e.Old), New: strOrEmpty(e.New)}
	}
	next, applied, err := ApplyLessonsEdits(m.Learnings, edits)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowShrink := body.AllowShrink != nil && *body.AllowShrink
	if !allowShrink && LessonsShrinkBlocked(m.Learnings, next) {
		writeError(w, http.StatusBadRequest,
			"patch would empty (or shrink to under a tenth of) the learnings doc — pass allow_shrink=true if this is intended, or use write_task_learnings; nothing was written")
		return
	}
	m.Learnings = next
	m.UpdatedTS = nowSecs()
	if err := s.dal.PutTaskManual(*m); err != nil {
		internalError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	sum := sha256.Sum256([]byte(next))
	writeJSON(w, http.StatusOK, taskLearningsPatchResultDTO{
		TypeKey:      typeKey,
		AppliedEdits: applied,
		Size:         len(next),
		Sha256:       hex.EncodeToString(sum[:]),
	})
}
