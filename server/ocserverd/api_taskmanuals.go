package main

// Task manuals: CONTENT writes are agent-floor (owner ruling); the `assignee`
// face is GOVERNANCE — below admin_agent it is a 403 from the in-handler gate,
// while delete's admin_agent floor sits on the route table.

import (
	"encoding/json"
	"net/http"
	"unicode/utf8"
)

// task_manual is the RETIRED four-field bundle kind: nothing writes or reads
// it. The constant survives solely so documentHistoryAllowed can refuse the old
// name by name instead of falling through to "unknown kind".
const (
	docKindTaskManual    = "task_manual"
	docKindTaskManualSop = "task_manual_sop"
)

// "{}" is the sentinel SaveWithDocumentHistories reads as "nothing worth
// retaining": without it the first SOP write of every (blank) manual would burn
// a version slot on an empty document.
func taskManualSopHistorySnapshot(m TaskManual) (string, error) {
	if m.SopMD == "" {
		return "{}", nil
	}
	return historyJSON(map[string]string{"sop_md": m.SopMD})
}

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

// writeTaskManual deliberately serves the WHOLE manual, sop_md included: it is
// the intake's type judgement and the planner's blueprint read.
func (s *apiServer) writeTaskManual(w http.ResponseWriter, m TaskManual) {
	dto, err := newTaskManualDTO(m, s.manualSopCap())
	if err != nil {
		internalError(w, err)
		return
	}
	// 🔴 Task lore rides out HERE and does NOT enter any boot document — it is
	// fetched when somebody opens the manual, where staff and outsource behave
	// identically.
	sel, err := selectLoreForScope(s.dal, LoreScopeManual, m.TypeKey, s.loreManualCap())
	if err != nil {
		internalError(w, err)
		return
	}
	dto.Lore = renderLoreBlock(sel)
	dto.LoreChars = len([]rune(dto.Lore))
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) writeTaskManualReceipt(w http.ResponseWriter, m TaskManual, wroteSop bool) {
	receipt := taskManualReceiptDTO{TypeKey: m.TypeKey, UpdatedTS: m.UpdatedTS}
	if wroteSop {
		n := utf8.RuneCountInString(m.SopMD)
		capChars := s.manualSopCap()
		sum := receiptSha256(m.SopMD)
		receipt.SopMdChars = &n
		receipt.SopMdCapChars = &capChars
		receipt.SopMdSha256 = &sum
	}
	writeJSON(w, http.StatusOK, receipt)
}

// validateManualAssignee: {} unsets; `copies` absent = 1. The old "auto"
// machine spelling is rejected outright — it named no machine, so every worker
// of that type silently never booted. "" = OK, else the 400 message.
func validateManualAssignee(assignee map[string]any) string {
	if len(assignee) == 0 {
		return ""
	}
	kind, _ := assignee["kind"].(string)
	switch kind {
	case TaskExecutorStaff:
		if memberID, _ := assignee["member_id"].(string); memberID == "" {
			return "assignee kind '" + TaskExecutorStaff + "' requires a member_id"
		}
	case TaskExecutorOutsource:
		if runtime, ok := assignee["runtime"]; ok {
			r, isStr := runtime.(string)
			if !isStr || !ValidRuntime(r) {
				return "assignee runtime must be 'claude' or 'codex'"
			}
		}
		if effort, ok := assignee["effort"]; ok {
			e, isStr := effort.(string)
			if !isStr || !validEffort(e) {
				return "assignee effort must be one of low, medium, high, xhigh, max"
			}
		}
		if copies, ok := assignee["copies"]; ok {
			if n, isNum := copies.(float64); !isNum || n < 0 {
				return "assignee copies must be a number >= 0 (0 = unlimited)"
			}
		}
		if machine, ok := assignee["machine"]; ok {
			m, isStr := machine.(string)
			if !isStr || m == "" {
				return "assignee machine must be a machine id"
			}
			if m == "auto" {
				return "assignee machine must be a machine id; \"auto\" is not a machine"
			}
		}
	default:
		// Derived, not written out again: a PRE-RENAME 'member' must be told it was RENAMED kind-vocab-guard:legacy
		// (owner ruling rc-7574cc804dd6).
		_, err := CanonicalTaskExecutorKind(kind)
		return "assignee kind: " + err.Error()
	}
	return ""
}

// resolveManualAssigneeMachine: a stale or hand-typed machine id is shaped fine
// but strands every future worker of the type.
func (s *apiServer) resolveManualAssigneeMachine(w http.ResponseWriter, assignee map[string]any) bool {
	if err := manualAssigneeMachineOn(s.dal.rdb, assignee); err != nil {
		writeTxError(w, err)
		return false
	}
	return true
}

func manualAssigneeMachineOn(q sqlRowQuerier, assignee map[string]any) error {
	if kind, _ := assignee["kind"].(string); kind != TaskExecutorOutsource {
		return nil
	}
	machineID, _ := assignee["machine"].(string)
	if machineID == "" {
		return nil
	}
	_, err := resolveMachineOn(q, machineID)
	return notFoundRefusal(err, "machine", machineID)
}

func (s *apiServer) callerMaySetAssignee(r *http.Request) bool {
	return principalAtLeast(s.principalOfRequest(r), principalAdminAgent)
}

const assigneeGovernanceMsg = "assignee is owner/admin-agent governance — " +
	"a plain agent may not set who executes a task type"

func (s *apiServer) HandleListTaskManualsApiTaskManualsGet(w http.ResponseWriter, r *http.Request) {
	manuals, err := s.dal.ListTaskManuals()
	if err != nil {
		internalError(w, err)
		return
	}
	out := []taskManualListItemDTO{}
	// Read the cap ONCE: per-row reads could straddle a PATCH and quote two
	// different caps for the same segment in one list.
	sopCapChars := s.manualSopCap()
	for _, m := range manuals {
		dto, err := newTaskManualListItemDTO(m, sopCapChars)
		if err != nil {
			internalError(w, err)
			return
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/task-manuals — the server MINTS the type_key from display_name
// (owner ruling: the id is the system's, the label the human's). An explicit
// `type_key` is the deprecated LEGACY path kept for old MCP callers.
func (s *apiServer) HandleCreateTaskManualApiTaskManualsPost(w http.ResponseWriter, r *http.Request) {
	var body TaskManualCreateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.Assignee != nil && !s.callerMaySetAssignee(r) {
		writeError(w, http.StatusForbidden, assigneeGovernanceMsg)
		return
	}
	typeKey := trimString(strOrEmpty(body.TypeKey))
	displayName := trimString(strOrEmpty(body.DisplayName))
	if typeKey == "" {
		if displayName == "" {
			writeError(w, http.StatusBadRequest,
				"display_name must not be blank")
			return
		}
		typeKey = "tm-" + newHexID(12)
	} else if displayName == "" {
		displayName = typeKey
	}
	assigneeBlob := "{}"
	if body.Assignee != nil {
		if problem := validateManualAssignee(*body.Assignee); problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
		if !s.resolveManualAssigneeMachine(w, *body.Assignee) {
			return
		}
		blob, err := json.Marshal(*body.Assignee)
		if err != nil {
			internalError(w, err)
			return
		}
		assigneeBlob = string(blob)
	}
	taken := func() error {
		existing, err := s.dal.GetTaskManual(typeKey)
		if err == nil && existing != nil {
			err = refuseInTx(http.StatusConflict, "task manual '"+typeKey+"' already exists")
		}
		return err
	}
	if err := taken(); err != nil {
		writeTxError(w, err)
		return
	}
	m := TaskManual{
		TypeKey:     typeKey,
		DisplayName: displayName,
		Fields:      "[]",
		Assignee:    assigneeBlob,
		UpdatedTS:   nowSecs(),
	}
	err := s.dal.inTx(func(tx *writeTx) error {
		if body.Assignee != nil {
			if err := manualAssigneeMachineOn(tx, *body.Assignee); err != nil {
				return err
			}
		}
		if err := taken(); err != nil {
			return err
		}
		return putTaskManualOn(tx, m)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	s.writeTaskManualReceipt(w, m, false)
}

func (s *apiServer) HandleGetTaskManualApiTaskManualsTypeKeyGet(w http.ResponseWriter, r *http.Request, typeKey string) {
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	s.writeTaskManual(w, *m)
}

func (s *apiServer) HandleUpdateTaskManualApiTaskManualsTypeKeyPost(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskManualUpdateDTO
	if !decodeJSONBodyStrict(w, r, &body) {
		return
	}
	if body.Assignee != nil && !s.callerMaySetAssignee(r) {
		writeError(w, http.StatusForbidden, assigneeGovernanceMsg)
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
			// No dedupe basis without it — the same root cause create_task's is_key
			// check guards.
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
		if !s.resolveManualAssigneeMachine(w, *body.Assignee) {
			return
		}
	}
	// ONE OF TWO write faces for sop_md (patch_task_sop judges the same cap on
	// its RESULT): every sop_md door must carry the cap or it is a suggestion.
	sopCap := s.manualSopCap()
	if body.SopMd != nil && DocCapBlocked(sopCap, m.SopMD, *body.SopMd) {
		writeError(w, http.StatusBadRequest, docCapRefusal(sopCap, "sop_md doc", m.SopMD, *body.SopMd))
		return
	}
	sopBefore := m.SopMD
	if body.DisplayName != nil {
		m.DisplayName = trimString(*body.DisplayName)
	}
	if body.Purpose != nil {
		m.Purpose = *body.Purpose
	}
	if body.SopMd != nil {
		m.SopMD = *body.SopMd
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
	streams := taskManualHistoryStreams(typeKey, currentActor(r), m.SopMD != sopBefore)
	if err := s.dal.SaveWithDocumentHistories(streams, func(ex sqlExecer) error {
		return putTaskManualOn(ex, *m)
	}); err != nil {
		internalError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	// Reported per document the BODY NAMED, not per document that changed: a
	// caller that resent the SOP unchanged still asked about it.
	s.writeTaskManualReceipt(w, *m, body.SopMd != nil)
}

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

// POST /api/task-manuals/{type_key}/sop/patch — anchor-addressed SOP patch on
// the shared ApplyDocEdits engine.
//
// WHY IT EXISTS: update_task_manual is a whole-doc replace, so two writers lose
// each other's work silently — the shrink guard does not fire because the stale
// copy is typically the LONGER one. Here the caller never sends a base copy, and
// a concurrent move of the anchor becomes a visible 400.
//
// 🔴 STILL OPEN: the read (read pool) and the write (write pool) share no
// transaction, so interleaved patches lose an edit. Wider than on the
// patch_step_note twin: putTaskManualOn is a WHOLE-ROW upsert, so the same
// window also REVERTS a concurrent update_task_manual (which already got its 200)
// and RESURRECTS a concurrently deleted manual.
func (s *apiServer) HandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskSopPatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if !requireNonEmptyEdits(w, body.Edits) {
		return
	}
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	edits, ok := decodePatchEdits(w, body.Edits)
	if !ok {
		return
	}
	// get_task_manual: the re-read the anchor-miss message names.
	next, applied, err := ApplyDocEdits(m.SopMD, edits, "get_task_manual")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowShrink := body.AllowShrink != nil && *body.AllowShrink
	if !allowShrink && LessonsShrinkBlocked(m.SopMD, next) {
		writeError(w, http.StatusBadRequest,
			"patch would empty (or shrink to under a tenth of) the sop_md doc — pass allow_shrink=true if this is intended, or use update_task_manual; nothing was written")
		return
	}
	cap := s.manualSopCap()
	if DocCapBlocked(cap, m.SopMD, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "sop_md doc", m.SopMD, next))
		return
	}
	// 🔴 The gate is the text comparison, NOT applied > 0: edits that undo one
	// another report applied != 0 over an unchanged document, and writing anyway
	// burns one of the THREE history slots (see ApplyDocEdits in domain.go).
	if next != m.SopMD {
		m.SopMD = next
		m.UpdatedTS = nowSecs()
		if err := s.dal.SaveWithDocumentHistories(
			taskManualHistoryStreams(typeKey, currentActor(r), true),
			func(ex sqlExecer) error {
				return putTaskManualOn(ex, *m)
			}); err != nil {
			internalError(w, err)
			return
		}
		s.publishTaskManual(typeKey, requestTrigger(r))
	}
	writeJSON(w, http.StatusOK, taskSopPatchResultDTO{
		TypeKey:      typeKey,
		AppliedEdits: applied,
		SizeChars:    utf8.RuneCountInString(next),
		CapChars:     cap,
		Sha256:       receiptSha256(next),
	})
}
