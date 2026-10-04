package main

import (
	"fmt"
	"net/http"
	"strconv"
	"unicode/utf8"
)

// Per-role Insight doc, beside Duty (role_def.definition_md). Deliberately not
// sharing handlers with other documents: shared code names the wrong document in
// the 403 and anchor-miss messages.

// Seed is the role's own seeds/insight_<roleKey>.md (per-role, never one shared
// file); a role with no seed file reads empty.
func (s *apiServer) foldInsightDTO(roleKey string) (*insightDTO, error) {
	overlay, err := s.dal.GetInsight(roleKey)
	if err != nil {
		return nil, err
	}
	seedMD, hasSeed, err := s.root.seedInsightMD(roleKey)
	if err != nil {
		return nil, err
	}
	text, isDefault := FoldInsight(overlay, seedMD, hasSeed)
	return &insightDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      s.insightCap(),
		RoleKey:       roleKey,
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     isDefault,
		// From the seed-file probe (the value reset's 409 is decided by), not derived
		// from isDefault: they answer different questions (see wire.go).
		HasSeed: hasSeed,
	}, nil
}

// Shared by every face that writes insight, including api_document_history.go's
// restore of kind "insight".
//
// READ is open to any authenticated identity — owner ruling rc-dc171587220c:
// Insight is separate, not private.
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
	if memberRole != roleKey {
		writeError(w, http.StatusForbidden,
			"an agent may only write its own role's insight")
		return false
	}
	return true
}

// Following the default retains nothing; see userContextHistorySnapshot.
func insightHistorySnapshot(current *Insight) (string, error) {
	if current == nil || current.Tombstoned {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

// Re-reads inside the write transaction rather than trusting a value folded
// earlier, or racing writers retain the same ancestor and lose the one between.
func insightSnapshotIn(roleKey string) func(sqlRowQuerier) (string, error) {
	return func(q sqlRowQuerier) (string, error) {
		current, err := getInsightOn(q, roleKey)
		if err != nil {
			return "", err
		}
		return insightHistorySnapshot(current)
	}
}

func (s *apiServer) HandleGetInsightApiInsightRoleKeyGet(w http.ResponseWriter, r *http.Request, roleKey string) {
	dto, err := s.foldInsightDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/insight/{role_key}/reset — tombstone the overlay back to the seed.
// 🔴 The seed check is on the FILE, not on RoleDefDTO.IsSeed or IsDefault.
// 🔴 No doc cap on this path, matching reset_role: the factory text is not
// caller-authored. (The restore in api_document_history.go does check the cap.)
func (s *apiServer) HandleResetInsightApiInsightRoleKeyResetPost(w http.ResponseWriter, r *http.Request, roleKey string) {
	// Authz before the 404/409 (like replace/patch, unlike reset_role), so the
	// status code does not reveal which roles ship a seed.
	if !s.insightWriteAuthz(w, r, roleKey) {
		return
	}
	_, hasSeed, err := s.root.seedInsightMD(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	if !hasSeed {
		role, err := s.foldRoleDefDTO(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if role == nil {
			writeError(w, http.StatusNotFound, "role '"+roleKey+"' not found")
			return
		}
		writeError(w, http.StatusConflict, "reset is not applicable to the insight of role '"+roleKey+
			"': it has no shipped version — only roles that ship an insight can be reset")
		return
	}
	if err := s.dal.SaveWithDocumentHistory("insight", roleKey, currentActor(r), insightSnapshotIn(roleKey), func(ex sqlExecer) error {
		return putInsightOn(ex, Insight{RoleKey: roleKey, Tombstoned: true})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("insight", "patch", "insight", wireOwnerID+"::"+roleKey, nil, audienceOwnerOnly(), requestTrigger(r))
	dto, err := s.foldInsightDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, insightReceiptOf(dto))
}

func insightReceiptOf(dto *insightDTO) insightReceiptDTO {
	return insightReceiptDTO{
		RoleKey:   dto.RoleKey,
		IsDefault: dto.IsDefault,
		HasSeed:   dto.HasSeed,
		SizeChars: dto.SizeChars,
		CapChars:  dto.CapChars,
		Sha256:    receiptSha256(dto.Text),
	}
}

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
	if !(body.AllowShrink != nil && *body.AllowShrink) {
		if WholeDocWipeBlocked(current.Text, text) {
			writeError(w, http.StatusBadRequest, docWipeRefusal("insight doc", ""))
			return
		}
	}
	cap := s.insightCap()
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
	// Re-probe: a write does not change has_seed, and hard-coding false would hide
	// the cockpit's reset offer.
	_, hasSeed, err := s.root.seedInsightMD(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, insightReceiptDTO{
		RoleKey:   roleKey,
		IsDefault: false,
		HasSeed:   hasSeed,
		SizeChars: utf8.RuneCountInString(text),
		CapChars:  cap,
		Sha256:    receiptSha256(text),
	})
}

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
		// Neither old nor new is malformed: folding nil→"" would route it into the
		// empty-old append branch and answer 200 over an unchanged doc.
		if e.Old == nil && e.New == nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
				"edits[%d]: neither old nor new was given — an edit needs at least one of them "+
					"(empty old appends new); nothing was written", i))
			return
		}
		edits[i] = LessonsEdit{Old: strOrEmpty(e.Old), New: strOrEmpty(e.New)}
	}
	// This document's own read tool: an anchor-miss message naming another
	// document's tool sends the caller to re-read the wrong document.
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
	cap := s.insightCap()
	if DocCapBlocked(cap, current.Text, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "insight doc", current.Text, next))
		return
	}
	// 🔴 Gate on the text, not applied > 0: applied counts edits that moved the
	// intermediate result, and a no-op write burns one of the three history
	// slots. Full reasoning at ApplyDocEdits (domain.go).
	if next != current.Text {
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
	}
	writeJSON(w, http.StatusOK, insightPatchResultDTO{
		RoleKey:       roleKey,
		AppliedEdits:  applied,
		SizeChars:     utf8.RuneCountInString(next),
		CapChars:      cap,
		Sha256:        receiptSha256(next),
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
	})
}
