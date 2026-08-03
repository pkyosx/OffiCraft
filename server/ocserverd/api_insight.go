package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"unicode/utf8"
)

// Per-role INSIGHT doc (T-3809) — the third block of the role journal, beside
// Duty (role_def.definition_md) and Learning (lessons.text).
//
// WHY IT IS ITS OWN FILE AND ITS OWN FUNCTIONS, not a task_type-less branch
// through the lessons handlers: every place the two documents differ is a place
// where sharing code would have produced a message or a key that is FALSE for
// one of them. The two that bit in review were the 403 wording and the
// anchor-miss wording — both name a document, and naming the wrong one sends
// the reader somewhere with confidence. The duplication is the cheaper error.

// foldInsightDTO resolves the per-role insight doc. There is no file seed to
// fold against (see FoldInsight): an unwritten doc is genuinely empty.
func (s *apiServer) foldInsightDTO(roleKey string) (*insightDTO, error) {
	overlay, err := s.dal.GetInsight(roleKey)
	if err != nil {
		return nil, err
	}
	text, isDefault := FoldInsight(overlay)
	return &insightDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      s.docCap(),
		RoleKey:       roleKey,
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     isDefault,
	}, nil
}

// insightWriteAuthz enforces the per-role insight WRITE authz shared by
// replace_insight and patch_insight: a caller at or above principalAdminAgent
// (owner, and the admin agent) writes ANY role's insight; everyone else writes
// ONLY its own member's role_key (read from the roster by the verified sub,
// never a client field). Answers the error itself and reports whether the
// caller may proceed.
//
// READ is not gated here and is not gated anywhere: the owner ruled on
// 2026-08-02 (rc-dc171587220c, option ①, verbatim 「包含 Insight：這一輪不關任何
// 讀取」) that this release closes nothing on the read face. Insight is SEPARATE,
// not private — and the delivery has to say so, because a document called
// "insight" reads as confidential whether or not anyone promised it.
//
// 🔴 WHY THIS IS NOT lessonsWriteAuthz WITH A DIFFERENT ARGUMENT. That function
// hard-codes the word "lessons" into its 403 body. Reusing it would answer a
// refused insight write with "an agent may only write its own role's lessons" —
// true-sounding, wrong document, and the caller's next move (go look at
// lessons) is wasted. The design ruled on this explicitly; the same defect
// hides in the anchor-miss message, which is why ApplyDocEdits takes the tool
// name to re-read with rather than baking in get_lessons.
func (s *apiServer) insightWriteAuthz(w http.ResponseWriter, r *http.Request, roleKey string) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
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
	// An empty memberRole can never equal the path's role_key (which is always
	// non-empty), so every roleless caller — outsource workers above all — is
	// refused here. That is the intended posture, not an accident of the
	// comparison: a worker has no role, so it has no insight of its own to
	// curate, and it must not curate someone else's.
	if memberRole != roleKey {
		writeError(w, http.StatusForbidden,
			"an agent may only write its own role's insight")
		return false
	}
	return true
}

// insightHistorySnapshot renders the retained revision of an insight doc.
func insightHistorySnapshot(current *Insight) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

// insightSnapshotIn is what SaveWithDocumentHistory calls from INSIDE the write
// transaction. Like its lessons twin it deliberately re-reads rather than trust
// a value the handler folded earlier: the retained revision must be the state
// this write replaced, or two writers racing on one document both retain the
// same ancestor and the revision written in between becomes unrecoverable.
func insightSnapshotIn(roleKey string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getInsightOn(q, roleKey)
		if err != nil {
			return "", err
		}
		return insightHistorySnapshot(current)
	}
}

// GET /api/insight/{role_key} — the per-role insight doc.
// READ is unrestricted for any authenticated identity (owner ruling above).
func (s *apiServer) HandleGetInsightApiInsightRoleKeyGet(w http.ResponseWriter, r *http.Request, roleKey string) {
	dto, err := s.foldInsightDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/insight/{role_key} — whole-doc replace. Per-role WRITE authz
// (insightWriteAuthz): admin capability writes any role, everyone else only its
// own member's role_key.
func (s *apiServer) HandleReplaceInsightApiInsightRoleKeyPost(w http.ResponseWriter, r *http.Request, roleKey string) {
	var body InsightReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	if !s.insightWriteAuthz(w, r, roleKey) {
		return
	}
	text := body.Text
	current, err := s.foldInsightDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	// Wipe guard, same posture replace_lessons carries: emptying a doc that had
	// content needs allow_shrink. It cannot fire on day one (every doc starts
	// empty, and empty → empty is not a wipe) — that is expected, not evidence
	// the guard works.
	if !(body.AllowShrink != nil && *body.AllowShrink) {
		if WholeDocWipeBlocked(current.Text, text) {
			writeError(w, http.StatusBadRequest,
				"this would replace the existing insight doc with an empty one — pass allow_shrink=true "+
					"if that is intended; nothing was written")
			return
		}
	}
	// Hard cap, checked UNCONDITIONALLY — allow_shrink governs the opposite
	// direction and is not a bypass. One read, reused by the response below, so
	// the number the caller is told is provably the one its write was judged
	// against.
	cap := s.docCap()
	if DocCapBlocked(cap, current.Text, text) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "insight doc", current.Text, text))
		return
	}
	if err := s.dal.SaveWithDocumentHistory("insight", roleKey, currentActor(r), insightSnapshotIn(roleKey), func(ex sqlExecer) error {
		return putInsightOn(ex, Insight{
			RoleKey:    roleKey,
			Text:       text,
			Tombstoned: false,
		})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("insight", "patch", "insight", wireOwnerID+"::"+roleKey, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, insightDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      cap,
		RoleKey:       roleKey,
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
	})
}

// POST /api/insight/{role_key}/patch — anchor-addressed patch. Semantics are
// patch_lessons': edits apply IN ORDER, a non-empty `old` must match exactly
// once (0/>1 hits → flat 400, WHOLE batch rejected, zero writes — the unique
// anchor doubling as an optimistic lock), an empty `old` appends. Same per-role
// write authz as replace_insight.
func (s *apiServer) HandlePatchInsightApiInsightRoleKeyPatchPost(w http.ResponseWriter, r *http.Request, roleKey string) {
	var body InsightPatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if len(body.Edits) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"edits requires at least one {old, new} entry")
		return
	}
	if !s.insightWriteAuthz(w, r, roleKey) {
		return
	}
	current, err := s.foldInsightDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	edits := make([]LessonsEdit, len(body.Edits))
	for i, e := range body.Edits {
		// An edit carrying NEITHER old NOR new is a malformed entry, not a
		// request to append nothing: folding nil→"" would route it into the
		// empty-old APPEND branch, where appending "" is a perfect no-op, so a
		// batch whose keys never parsed would answer 200 with an unchanged doc.
		if e.Old == nil && e.New == nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
				"edits[%d]: neither old nor new was given — an edit needs at least one of them "+
					"(empty old appends new); nothing was written", i))
			return
		}
		edits[i] = LessonsEdit{Old: strOrEmpty(e.Old), New: strOrEmpty(e.New)}
	}
	// get_insight, NOT get_lessons: an anchor-miss message that sends the caller
	// to re-read the wrong document is worse than a vague one.
	next, applied, err := ApplyDocEdits(current.Text, edits, "get_insight")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowShrink := body.AllowShrink != nil && *body.AllowShrink
	if !allowShrink && LessonsShrinkBlocked(current.Text, next) {
		writeError(w, http.StatusBadRequest,
			"patch would empty (or shrink to under a tenth of) the insight doc — pass allow_shrink=true if this is intended, or use replace_insight; nothing was written")
		return
	}
	// Cap judged on the RESULT of the patch, not the patch's own size: a small
	// patch onto a huge doc is exactly what grows it. Unconditional.
	cap := s.docCap()
	if DocCapBlocked(cap, current.Text, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "insight doc", current.Text, next))
		return
	}
	if err := s.dal.SaveWithDocumentHistory("insight", roleKey, currentActor(r), insightSnapshotIn(roleKey), func(ex sqlExecer) error {
		return putInsightOn(ex, Insight{
			RoleKey:    roleKey,
			Text:       next,
			Tombstoned: false,
		})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("insight", "patch", "insight", wireOwnerID+"::"+roleKey, nil, audienceOwnerOnly(), requestTrigger(r))
	sum := sha256.Sum256([]byte(next))
	writeJSON(w, http.StatusOK, insightPatchResultDTO{
		RoleKey:       roleKey,
		AppliedEdits:  applied,
		SizeChars:     utf8.RuneCountInString(next),
		CapChars:      cap,
		Sha256:        hex.EncodeToString(sum[:]),
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
	})
}
