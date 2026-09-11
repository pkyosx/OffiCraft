package main

// api_taskmanuals.go — the 設定 › 任務手冊 surface (M3 contract §C.5): the
// shared read face, the agent-floor CONTENT writes (create a manual, partial
// edit of purpose / fields / SOP — owner ruling 2026-07-13:
// agents author manual content), the GOVERNANCE face (the assignee setting —
// a caller below admin_agent supplying `assignee` on create/edit is a 403 from
// the in-handler gate; delete is requires=admin_agent on the route table —
// both floors lowered from owner by T-6020, owner ruling 2026-07-26).
// Manuals ship EMPTY (SPEC §5.1: no seed, no tombstone); delete is refused
// while non-terminal tasks of the type exist.

import (
	"encoding/json"
	"net/http"
	"unicode/utf8"
)

// The live manual history kind, plus the RETIRED four-field bundle name.
// Nothing writes, reads or stores task_manual any more: T-1f39 split it into
// per-document series and migration 00045 deleted the stranded rows. The
// constant survives solely so documentHistoryAllowed can refuse the old name by
// name instead of falling through to "unknown kind".
const (
	docKindTaskManual    = "task_manual"
	docKindTaskManualSop = "task_manual_sop"
)

// The snapshot carries ONE field, and answers "{}" — the sentinel
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

// writeTaskManual is the common single-manual READ response tail.
//
// 🔴 T-91 LEFT IT ALONE ON PURPOSE. It still serves GET /api/task-manuals/{key}
// — the intake's type judgement and the planner's blueprint read — with the
// whole manual, sop_md included. The WRITE faces moved onto the receipt tails
// below.
func (s *apiServer) writeTaskManual(w http.ResponseWriter, m TaskManual) {
	dto, err := newTaskManualDTO(m, s.manualSopCap())
	if err != nil {
		internalError(w, err)
		return
	}
	// 傳承 (T-33) — the MANUAL exit. The type's lore rides out on `lore`, its
	// OWN field.
	//
	// 🔴 THIS DOES NOT ENTER ANY BOOT DOCUMENT. Task lore is fetched when
	// somebody opens the manual, which is where staff and outsource behave
	// identically — the role/outsource asymmetry lives on the OTHER exit and
	// stops there.
	sel, err := selectLoreForScope(s.dal, LoreScopeManual, m.TypeKey, s.loreManualCap())
	if err != nil {
		internalError(w, err)
		return
	}
	dto.Lore = renderLoreBlock(sel)
	dto.LoreChars = len([]rune(dto.Lore))
	writeJSON(w, http.StatusOK, dto)
}

// writeTaskManualReceipt answers create_task_manual and update_task_manual
// (T-91). The whole taskManualDTO — both capped documents in full — used to
// ride back from every one of these writes.
//
// 🔴 wroteSop IS THE POINT OF THE SIGNATURE. The document's triple is reported
// ONLY when THIS call wrote it: update_task_manual is a partial, so a caller
// that changed only the display name never touched the document, and reporting
// a size and a hash for an untouched document invites exactly the wrong
// conclusion. Absence is spelled with pointers rather than zeroes, because 0 is
// indistinguishable from an empty document that WAS written.
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

// validateManualAssignee checks an incoming assignee object: {} unsets; a
// populated object must carry a legal kind — "staff" (with a non-blank
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
		// Derived, not written out again: a caller sending the PRE-RENAME
		// 'member' must be told it was RENAMED rather than merely rejected kind-vocab-guard:legacy
		// (owner ruling rc-7574cc804dd6).
		_, err := CanonicalTaskExecutorKind(kind)
		return "assignee kind: " + err.Error()
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
// {owner, admin_agent} (root CLAUDE.md §4) and nothing below, even though the
// manual CONTENT fields are agent-editable. False → the caller writes the 403.
func (s *apiServer) callerMaySetAssignee(r *http.Request) bool {
	return principalAtLeast(s.principalOfRequest(r), principalAdminAgent)
}

const assigneeGovernanceMsg = "assignee is owner/admin-agent governance — " +
	"a plain agent may not set who executes a task type"

// GET /api/task-manuals — the type rows. The catalogue and the bodies are
// separate reads: this answers WHICH types exist and how big each one's long
// document is; the SOP itself comes one type at a time from get_task_manual.
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
	// Read the cap ONCE for the whole listing: per-row reads could straddle a
	// PATCH and hand back one list quoting two different caps for the same
	// segment.
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
	// The document is not written on a create — a fresh manual starts with an
	// empty SOP — so no triple rides back. type_key IS news on this face when
	// the caller sent only a display_name: the id was minted here.
	s.writeTaskManualReceipt(w, m, false)
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
// fields (purpose / fields / sop_md); assignee stays the
// GOVERNANCE face — a caller below admin_agent supplying it is a 403 (T-6020).
func (s *apiServer) HandleUpdateTaskManualApiTaskManualsTypeKeyPost(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskManualUpdateDTO
	// T-2d99 (mirror direction): strict decode, but NO required names. This is
	// a partial update — "only supplied fields change" is the contract, so an
	// absent key must stay legal. What must NOT stay legal is an UNKNOWN key: a
	// misspelled field name answering 200 while dropping the write is the bug
	// class this guards. Unknown key ⇒ 422, no write.
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
	// of its patch); every sop_md door has to carry the cap or the cap is a
	// suggestion. Validated BEFORE any field is applied, so a refusal leaves the
	// whole partial update unwritten (the handler's existing posture). The cap
	// is read exactly once, so the field is not judged twice against two
	// different values of its own setting.
	sopCap := s.manualSopCap()
	if body.SopMd != nil && DocCapBlocked(sopCap, m.SopMD, *body.SopMd) {
		writeError(w, http.StatusBadRequest, docCapRefusal(sopCap, "sop_md doc", m.SopMD, *body.SopMd))
		return
	}
	// All validated — apply the partial update. The versioned field is
	// remembered as it stood so the write below can retain a revision only when
	// this call actually changes it (T-1f39).
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
	// Reported per DOCUMENT THE BODY NAMED, not per document that changed:
	// a caller that resent the SOP unchanged still asked about the SOP, and the
	// size/hash it gets back are the honest answer to that question. A caller
	// that never mentioned it gets nothing about it at all.
	s.writeTaskManualReceipt(w, *m, body.SopMd != nil)
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

// POST /api/task-manuals/{type_key}/sop/patch — anchor-addressed patch of a
// type's SOP (T-1667). ApplyDocEdits is the SHARED engine, so the
// anchor/append/atomicity semantics are byte-identical to the other patch faces
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
// upsert: it writes back purpose, fields, display_name and assignee from the
// copy resolveTaskManual read at the top of this request, not just sop_md. So
// an interleaving in the same window also REVERTS a concurrent write to any of
// those other fields — an update_task_manual landing between this face's read
// and its write is silently undone, and the caller of that write already got
// its 200. The step-note twin does not have this: SetTaskStepNote
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
// the only ones rewritten to the description above. patch_insight still
// describes the anchor as an "optimistic lock" in its comments and on the wire.
// Realigning it is a follow-up; do not read this comment as a claim that every
// patch face now agrees.
//
// Semantics: edits apply IN ORDER; a non-empty old must match exactly once
// (0/>1 → flat 400 naming the failing edit index and get_task_manual as the
// re-read, WHOLE batch rejected, zero writes); an empty old appends. A patch
// that wipes the doc, or shrinks a substantial doc to <10%, is refused without
// allow_shrink=true. The sop_md cap is judged on the RESULT and allow_shrink is
// not a bypass — the same posture the other patch faces take. Same agent floor as
// update_task_manual's content fields. Unknown type → 404.
func (s *apiServer) HandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskSopPatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if !requireNonEmptyEdits(w, body.Edits) {
		return
	}
	// Target first, content second, so an unknown type_key answers 404 rather
	// than this face ruling on the edits of a manual that does not exist.
	m, err := s.resolveTaskManual(typeKey)
	if err != nil {
		writeResolveError(w, err, "task manual", typeKey)
		return
	}
	edits, ok := decodePatchEdits(w, body.Edits)
	if !ok {
		return
	}
	// get_task_manual: the anchor-miss message tells the caller
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
	// error the earlier faces were built on. The SOP has its own independent
	// version series on one manual (T-1f39), so this face burns the SOP series'
	// slots specifically. The receipt below stays outside the gate and unchanged.
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
