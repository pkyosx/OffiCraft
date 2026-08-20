package main

// api_taskmanuals.go — the 設定 › 任務手冊 surface (M3 contract §C.5): the
// shared read face, the agent-floor CONTENT writes (create a manual, partial
// edit of purpose / fields / SOP / learnings — owner ruling 2026-07-13:
// agents author manual content), the GOVERNANCE face (the assignee setting —
// a caller below admin_agent supplying `assignee` on create/edit is a 403 from
// the in-handler gate; delete is requires=admin_agent on the route table —
// both floors lowered from owner by T-6020, owner ruling 2026-07-26),
// and the AGENT's learnings write-back (whole-doc replace, the
// replace_lessons shape). Manuals ship EMPTY (SPEC §5.1: no seed, no
// tombstone); delete is refused while non-terminal tasks of the type exist.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"unicode/utf8"
)

// The two live manual history kinds, plus the RETIRED four-field bundle name.
// Nothing writes, reads or stores task_manual any more: T-1f39 split SOP and
// learnings into their own series and migration 00045 deleted the stranded
// rows. The constant survives solely so documentHistoryAllowed can refuse the
// old name by name instead of falling through to "unknown kind".
const (
	docKindTaskManual          = "task_manual"
	docKindTaskManualSop       = "task_manual_sop"
	docKindTaskManualLearnings = "task_manual_learnings"
)

// The two split snapshots carry ONE field each, and answer "{}" — the sentinel
// SaveWithDocumentHistories reads as "nothing worth retaining" — when that
// field is empty. A manual ships blank, so without this the first SOP write of
// every manual would burn a version slot on an empty document, which is the
// rule the four-field bundle already applied to a manual that did not exist.
func taskManualSopHistorySnapshot(m TaskManual) (string, error) {
	if m.SopMD == "" {
		return "{}", nil
	}
	return historyJSON(map[string]string{"sop_md": m.SopMD})
}

func taskManualLearningsHistorySnapshot(m TaskManual) (string, error) {
	if m.Learnings == "" {
		return "{}", nil
	}
	return historyJSON(map[string]string{"learnings": m.Learnings})
}

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
	dto, err := newTaskManualDTO(m, s.manualSopCap(), s.manualLearningsCap())
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
// = 1); `machine`, when present, must be a MACHINE ID — the type's workers boot
// there and nowhere else. Absent leaves the type without a placement, which is a
// legal (if not-yet-runnable) manual: the dispatcher or a per-worker 改機器 can
// still name one. The old "auto" spelling is rejected outright — it named no
// machine, so every worker of that type silently never booted. "" = OK, else the
// 400 message.
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
		if runtime, ok := assignee["runtime"]; ok {
			r, isStr := runtime.(string)
			if !isStr || !ValidRuntime(r) {
				return "assignee runtime must be 'claude' or 'codex'"
			}
		}
		if effort, ok := assignee["effort"]; ok {
			e, isStr := effort.(string)
			if !isStr || !validEffort(e) {
				return "assignee effort must be one of low, medium, high, max"
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
		return "assignee kind must be 'member' or 'outsource'"
	}
	return ""
}

// resolveManualAssigneeMachine confirms an outsource assignee's `machine` names a
// machine that actually exists, writing the resolve error and returning false
// when it does not. validateManualAssignee can only check the SHAPE (it is pure);
// a stale or hand-typed id is shaped fine and strands every future worker of the
// type — the same reasoning that makes a nonexistent relocate pin a 404.
func (s *apiServer) resolveManualAssigneeMachine(w http.ResponseWriter, assignee map[string]any) bool {
	if kind, _ := assignee["kind"].(string); kind != TaskExecutorOutsource {
		return true
	}
	machineID, _ := assignee["machine"].(string)
	if machineID == "" {
		return true
	}
	if _, err := s.resolveMachine(machineID); err != nil {
		writeResolveError(w, err, "machine", machineID)
		return false
	}
	return true
}

// callerMaySetAssignee enforces the assignee governance gate (owner ruling
// 2026-07-13, floor lowered by T-6020 owner ruling 2026-07-26): the assignee
// face — who/what executes a type (member binding / outsource headcount /
// machine placement) — is GOVERNANCE, so it admits the governance classes
// {owner, admin_agent} (root CLAUDE.md「核心不變量／授權單一化」) and nothing below, even though the
// manual CONTENT fields are agent-editable. False → the caller writes the 403.
func (s *apiServer) callerMaySetAssignee(r *http.Request) bool {
	return principalAtLeast(s.principalOfRequest(r), principalAdminAgent)
}

const assigneeGovernanceMsg = "assignee is owner/admin-agent governance — " +
	"a plain agent may not set who executes a task type"

// GET /api/task-manuals — the type rows. The catalogue and the bodies are
// separate reads: this answers WHICH types exist and how big each one's two
// long documents are; the SOP and the learnings themselves come one type at a
// time from get_task_manual.
//
// This used to answer with the FULL manual of every type by default and offer
// the light row behind ?view=list. A default is where the cost actually lands,
// so the light row IS the answer now and the parameter is gone: an opt-in flag
// left the expensive shape as the thing every naive caller got.
//
// It is NOT the old ?view=list row verbatim — that one blanked `fields` and
// `assignee` too, which merely forced a per-row second request for two small
// bounded values. Only the two free-form markdown blobs are dropped, and their
// SIZES and CAPS stay on every row.
func (s *apiServer) HandleListTaskManualsApiTaskManualsGet(w http.ResponseWriter, r *http.Request) {
	manuals, err := s.dal.ListTaskManuals()
	if err != nil {
		internalError(w, err)
		return
	}
	out := []taskManualListItemDTO{}
	// Read each cap ONCE for the whole listing: per-row reads could straddle a
	// PATCH and hand back one list quoting two different caps for the same
	// segment. The two segments' caps are still read independently — they are
	// different settings, and a list reporting one number for both is the bug
	// T-30f1 exists to remove.
	sopCapChars := s.manualSopCap()
	learningsCapChars := s.manualLearningsCap()
	for _, m := range manuals {
		dto, err := newTaskManualListItemDTO(m, sopCapChars, learningsCapChars)
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
// assignee is the GOVERNANCE face: a caller below admin_agent supplying it is
// a 403 (T-6020); owner/admin_agent get theirs validated and applied.
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
// GOVERNANCE face — a caller below admin_agent supplying it is a 403 (T-6020).
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
		if !s.resolveManualAssigneeMachine(w, *body.Assignee) {
			return
		}
	}
	// T-3351 hard cap. This handler is ONE OF TWO write faces for sop_md (the
	// other is patch_task_sop, T-1667, which judges the SAME cap on the RESULT
	// of its patch), and a SECOND write face for learnings (spelled `learnings`
	// here, `text` in write_task_learnings) — capping only the
	// learnings-specific seams would have left both an uncapped door onto the
	// same document and sop_md with no gate at all; every sop_md door has to
	// carry the cap or the cap is a suggestion. Validated BEFORE any field is
	// applied, so a refusal leaves the whole partial update unwritten (the
	// handler's existing posture).
	// Each field is judged against ITS OWN cap, read once (T-30f1). Until then
	// both were judged against one read of one shared cap, and the reason given
	// was that two reads could straddle a concurrent PATCH and judge one doc by
	// a cap the other never saw — true only while there was a single number to
	// straddle. sop_md and learnings now answer to two independent settings, so
	// sharing a read would mean judging one document by the other's budget.
	// Each cap is still read exactly once, so neither field is judged twice
	// against two different values of its own setting.
	sopCap := s.manualSopCap()
	learningsCap := s.manualLearningsCap()
	if body.SopMd != nil && DocCapBlocked(sopCap, m.SopMD, *body.SopMd) {
		writeError(w, http.StatusBadRequest, docCapRefusal(sopCap, "sop_md doc", m.SopMD, *body.SopMd))
		return
	}
	if body.Learnings != nil && DocCapBlocked(learningsCap, m.Learnings, *body.Learnings) {
		writeError(w, http.StatusBadRequest, docCapRefusal(learningsCap, "learnings doc", m.Learnings, *body.Learnings))
		return
	}
	// All validated — apply the partial update. The two versioned fields are
	// remembered as they stood so the write below can retain a revision only
	// for the ones this call actually changes (T-1f39).
	sopBefore, learningsBefore := m.SopMD, m.Learnings
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
	streams := taskManualHistoryStreams(typeKey, currentActor(r),
		m.SopMD != sopBefore, m.Learnings != learningsBefore)
	if err := s.dal.SaveWithDocumentHistories(streams, func(ex sqlExecer) error {
		return putTaskManualOn(ex, *m)
	}); err != nil {
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
	// T-3351 hard cap. Unconditional — allow_shrink governs the opposite
	// direction (shrinking too far) and is not a bypass for this one.
	if cap := s.manualLearningsCap(); DocCapBlocked(cap, m.Learnings, body.Text) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "learnings doc", m.Learnings, body.Text))
		return
	}
	m.Learnings = body.Text
	m.UpdatedTS = nowSecs()
	if err := s.dal.SaveWithDocumentHistories(
		taskManualHistoryStreams(typeKey, currentActor(r), false, true),
		func(ex sqlExecer) error {
			return putTaskManualOn(ex, *m)
		}); err != nil {
		internalError(w, err)
		return
	}
	s.publishTaskManual(typeKey, requestTrigger(r))
	s.writeTaskManual(w, *m)
}

// POST /api/task-manuals/{type_key}/learnings/patch — anchor-addressed patch of
// a type's learnings (T-9ffd; the patch_lessons twin for task manuals).
// ApplyDocEdits is the SHARED engine — it is generic over the doc text, so the
// anchor/append/atomicity semantics are byte-identical to patch_lessons. The
// one thing it is NOT generic over is which tool re-reads this doc: that is a
// required argument, and this face passes get_task_manual.
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
	// get_task_manual, NOT get_lessons (T-2fbf): a manual's learnings is served
	// by the manual, and an agent sent to re-read its ROLE's lessons will never
	// find the anchor it missed — it re-anchors against the wrong document and
	// misses again, with no error and no signal that it was misdirected.
	next, applied, err := ApplyDocEdits(m.Learnings, edits, "get_task_manual")
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
	// T-3351 hard cap, judged on the RESULT of the patch (not the patch's own
	// size). Unconditional: allow_shrink is not a bypass.
	// One read, reused by the receipt below (see api_roles.go).
	cap := s.manualLearningsCap()
	if DocCapBlocked(cap, m.Learnings, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "learnings doc", m.Learnings, next))
		return
	}
	// 🔴 `next` byte-identical to the stored learnings → there is nothing to
	// write and nothing to retain. The gate is that text comparison and NOT
	// applied > 0: `applied` counts edits that moved the INTERMEDIATE result, so
	// a batch whose edits undo one another reports applied != 0 over a document
	// that never changed. Writing anyway burns one of the THREE document history
	// slots on a snapshot of text nobody replaced (and bumps updated_ts for a
	// change that did not happen), silently shortening the owner's undo path.
	// Full reasoning at the patch_lessons twin (api_roles.go,
	// HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost). This is the same
	// "did the field actually change" gate update_task_manual already applies
	// above via taskManualHistoryStreams; the receipt below stays outside the
	// gate and unchanged.
	if next != m.Learnings {
		m.Learnings = next
		m.UpdatedTS = nowSecs()
		if err := s.dal.SaveWithDocumentHistories(
			taskManualHistoryStreams(typeKey, currentActor(r), false, true),
			func(ex sqlExecer) error {
				return putTaskManualOn(ex, *m)
			}); err != nil {
			internalError(w, err)
			return
		}
		s.publishTaskManual(typeKey, requestTrigger(r))
	}
	sum := sha256.Sum256([]byte(next))
	writeJSON(w, http.StatusOK, taskLearningsPatchResultDTO{
		TypeKey:      typeKey,
		AppliedEdits: applied,
		SizeChars:    utf8.RuneCountInString(next),
		CapChars:     cap,
		Sha256:       hex.EncodeToString(sum[:]),
	})
}

// POST /api/task-manuals/{type_key}/sop/patch — anchor-addressed patch of a
// type's SOP (T-1667; the patch_task_learnings twin for the OTHER long-form
// document a manual carries). ApplyDocEdits is the SHARED engine, so the
// anchor/append/atomicity semantics are byte-identical to the three patch faces
// that came before it.
//
// WHY THIS EXISTS — CONCURRENT OVERWRITE, not token economy. update_task_manual
// is the only other write face for sop_md and it is a whole-doc replace, so two
// writers on one manual lose each other's work by construction: the second one
// sends a copy it read before the first landed, and everything the first added
// is gone. Nothing catches it. The shrink guard does not fire, because the
// stale copy is typically the LONGER of the two (it was written by a session
// that had the whole SOP in context and re-typed all of it) — so the write does
// not even look like a deletion. The result is a silent loss with zero signal.
// The anchor closes that shape by construction rather than by locking: the
// caller sends only {old, new} and never a base copy, so "overwrite the whole
// doc from a base I read earlier" is not expressible on this wire at all, and
// the splice is matched against sop_md as it stands when this request reads it.
// A concurrent write that moved or duplicated the anchor turns this batch into
// a visible 400. Making the write cost scale with the CHANGE is the secondary
// benefit.
//
// WHAT IS STILL OPEN. Concurrent edits to DIFFERENT anchors survive TOGETHER
// once the two requests serialise — each is spliced onto whatever the doc says
// at its own read. What remains is the read-then-write gap INSIDE one request,
// which is milliseconds wide rather than session-long, but is not zero: an
// interleaving there still eats one side's edit silently.
// Concretely: the read above goes to the read pool, the write below to the
// write pool, with no transaction spanning the two and no version compare. Two
// patch requests interleaving in the server (A reads → B reads → A writes →
// B writes) lose A's edit. Closing it needs the read and the write under one
// transaction, or a version/etag compare at the write boundary. Tracked
// separately.
//
// 🔴 AND THAT WINDOW IS WIDER HERE THAN ON THE patch_step_note TWIN, which is
// why this caveat is not a copy of that one. putTaskManualOn is a WHOLE-ROW
// upsert: it writes back purpose, fields, display_name, assignee and learnings
// from the copy resolveTaskManual read at the top of this request, not just
// sop_md. So an interleaving in the same window also REVERTS a concurrent write
// to any of those other fields — a patch_task_learnings landing between this
// face's read and its write is silently undone, and the caller of that write
// already got its 200. The step-note twin does not have this: SetTaskStepNote
// is a SINGLE-column UPDATE, so its window can only cost the note itself.
// The narrow fix is to make this face write sop_md alone; that is out of
// T-1667's scope and is recorded here rather than done.
//
// Same shape, same cause: a manual DELETED concurrently between the read and
// the write is RESURRECTED by the upsert's INSERT arm and this face answers
// 200. That is pre-existing behaviour of update_task_manual (identical write
// seam), but this ticket opens a SECOND door onto it. Also not fixed here.
//
// Wording note: the two faces T-1667 added (this one and patch_step_note) are
// the only ones rewritten to the description above. patch_task_learnings above,
// and patch_lessons / patch_insight, still describe the anchor as an "optimistic
// lock" in their comments and on the wire. Realigning those three is a
// follow-up; do not read this comment as a claim that all five now agree.
//
// Semantics: edits apply IN ORDER; a non-empty old must match exactly once
// (0/>1 → flat 400 naming the failing edit index and get_task_manual as the
// re-read, WHOLE batch rejected, zero writes); an empty old appends. A patch
// that wipes the doc, or shrinks a substantial doc to <10%, is refused without
// allow_shrink=true. The sop_md cap is judged on the RESULT and allow_shrink is
// not a bypass — the same posture the learnings twin takes. Same agent floor as
// update_task_manual's content fields. Unknown type → 404.
func (s *apiServer) HandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskSopPatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if !requireNonEmptyEdits(w, body.Edits) {
		return
	}
	// Target first, content second — the order patch_task_learnings takes, so an
	// unknown type_key answers 404 on both faces rather than one of them ruling
	// on the edits of a manual that does not exist.
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	edits, ok := decodePatchEdits(w, body.Edits)
	if !ok {
		return
	}
	// get_task_manual, not get_lessons: the anchor-miss message tells the caller
	// where to look next, and naming the wrong document is worse than naming
	// none (the reason ApplyDocEdits takes the tool name as a parameter).
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
	// T-3351 hard cap, judged on the RESULT of the patch (not the patch's own
	// size). Unconditional: allow_shrink is not a bypass. One read, reused by
	// the receipt below.
	cap := s.manualSopCap()
	if DocCapBlocked(cap, m.SopMD, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "sop_md doc", m.SopMD, next))
		return
	}
	// 🔴 `next` byte-identical to the stored sop_md → there is nothing to write
	// and nothing to retain. The gate is that text comparison and NOT applied > 0:
	// `applied` counts edits that moved the INTERMEDIATE result, so a batch whose
	// edits undo one another reports applied != 0 over a document that never
	// changed. Writing anyway burns one of the THREE document history slots on a
	// snapshot of text nobody replaced (and bumps updated_ts for a change that did
	// not happen), silently shortening the owner's undo path. Full reasoning at
	// ApplyDocEdits (domain.go), which marks `applied > 0` as the exact reasoning
	// error the earlier faces were built on. SOP and learnings are two independent
	// version series on one manual (T-1f39), so this face burns the SOP series'
	// slots specifically. The receipt below stays outside the gate and unchanged.
	if next != m.SopMD {
		m.SopMD = next
		m.UpdatedTS = nowSecs()
		if err := s.dal.SaveWithDocumentHistories(
			taskManualHistoryStreams(typeKey, currentActor(r), true, false),
			func(ex sqlExecer) error {
				return putTaskManualOn(ex, *m)
			}); err != nil {
			internalError(w, err)
			return
		}
		s.publishTaskManual(typeKey, requestTrigger(r))
	}
	sum := sha256.Sum256([]byte(next))
	writeJSON(w, http.StatusOK, taskSopPatchResultDTO{
		TypeKey:      typeKey,
		AppliedEdits: applied,
		SizeChars:    utf8.RuneCountInString(next),
		CapChars:     cap,
		Sha256:       hex.EncodeToString(sum[:]),
	})
}
