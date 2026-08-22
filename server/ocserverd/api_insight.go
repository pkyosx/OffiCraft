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

// foldInsightDTO resolves the per-role insight doc: overlay ⊕ this role's OWN
// file seed (T-e1e3 — `seeds/insight_<roleKey>.md`, PER-ROLE, never one shared
// file). A role with no seed file still reads genuinely empty; a role with one
// reads the factory wording with is_default=true.
//
// 🔴 T-3809 shipped the opposite stance and this comment used to state it
// verbatim ("There is no file seed to fold against … an unwritten doc is
// genuinely empty"). T-e1e3 OVERTURNS it on the owner's ruling: every studio
// that installs OffiCraft must get an assistant that already knows how to
// manage context, and that knowledge belongs to Insight by his own division
// (Duty = what she does, Insight = how). The old sentence is deleted rather
// than left standing, because a comment describing a design the code no longer
// has is worse than no comment.
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
		// Straight from the seed-FILE probe above (T-6501) — the same value the
		// reset route's 404 is decided by, so a caller can never be offered a
		// reset this server would refuse. Deliberately not derived from
		// isDefault: those answer different questions (see wire.go).
		HasSeed: hasSeed,
	}, nil
}

// insightWriteAuthz enforces the per-role insight WRITE authz shared by EVERY
// face that writes this document — replace_insight, patch_insight,
// reset_insight (T-6501), and api_document_history.go's restore of kind
// "insight": a caller at or above principalAdminAgent
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

// POST /api/insight/{role_key}/reset — tombstone the overlay back to this
// role's own factory seed (T-6501). The exact counterpart of reset_role on the
// Duty block, and it exists because until now there was NO path at all back to
// `seeds/insight_<role_key>.md`: the seed shipped, and once a role had written
// its own insight nothing could call the factory wording back.
//
// A role with no seed file → 404, the same rule reset_role applies (there must
// be a factory version to reset TO). Idempotent: resetting an already-default
// doc writes a tombstone over a tombstone and answers the same DTO.
//
// 🔴 THE SEED CHECK IS ON THE FILE, NOT ON IsSeed. `RoleDefDTO.IsSeed` means
// "this role HAS a factory version available"; it does NOT mean "what you are
// reading right now IS the factory version" — that is IsDefault. On 2026-08-04
// that distinction misled two people in a row on this very document, so it is
// written down here rather than left to be re-derived. Insight has its own
// per-role seed roster anyway (the presence of the file IS the roster, see
// assets.go seedInsightMD), so the answer must come from there.
//
// 🔴 NO DOC CAP IS CHECKED ON THIS PATH, deliberately, matching reset_role.
// Both handlers must behave the same way here or the office grows a state
// nobody predicts: a Duty resets fine while the very same gesture on Insight is
// refused by a cap the OWNER set afterwards — i.e. a user setting blocking the
// way back to factory content. The factory text is part of the product, not a
// document the caller authored, so a ceiling on what people WRITE has no
// business judging it. (The restore door in api_document_history.go is the
// opposite case and does check the cap: there the caller is putting back text a
// person wrote.)
func (s *apiServer) HandleResetInsightApiInsightRoleKeyResetPost(w http.ResponseWriter, r *http.Request, roleKey string) {
	// Authz BEFORE the 404, matching replace_insight / patch_insight rather
	// than reset_role: those two answer the write gate first, and a caller with
	// no business writing this role's insight should not learn from the status
	// code which roles ship a seed.
	if !s.insightWriteAuthz(w, r, roleKey) {
		return
	}
	_, hasSeed, err := s.root.seedInsightMD(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	if !hasSeed {
		writeError(w, http.StatusNotFound,
			"role '"+roleKey+"' has no factory insight to reset to")
		return
	}
	// SaveWithDocumentHistory, NOT a bare putInsightOn: the overlay this reset
	// discards is retained as a revision, so a reset is recoverable from the
	// version history exactly like every other write to this document. Dropping
	// it would make reset the one destructive write with no way back.
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
			writeError(w, http.StatusBadRequest, docWipeRefusal("insight doc", ""))
			return
		}
	}
	// Hard cap, checked UNCONDITIONALLY — allow_shrink governs the opposite
	// direction and is not a bypass. One read, reused by the response below, so
	// the number the caller is told is provably the one its write was judged
	// against.
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
	// has_seed answers "is there a factory version to fall back to", which a
	// WRITE does not change — so it must be re-probed rather than assumed
	// false. Hard-coding false here would tell the cockpit "no reset available"
	// the instant a seeded role saved an edit, i.e. exactly when the reset
	// starts being worth offering.
	_, hasSeed, err := s.root.seedInsightMD(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, insightDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      cap,
		RoleKey:       roleKey,
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
		HasSeed:       hasSeed,
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
	cap := s.insightCap()
	if DocCapBlocked(cap, current.Text, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "insight doc", current.Text, next))
		return
	}
	// 🔴 `next` byte-identical to the stored doc → there is nothing to write and
	// nothing to retain. The gate is that text comparison and NOT applied > 0:
	// `applied` counts edits that moved the INTERMEDIATE result, so a batch
	// whose edits undo one another reports applied != 0 over a document that
	// never changed. Writing anyway burns one of the THREE document history
	// slots on a snapshot of text nobody replaced, silently shortening the
	// owner's undo path. Full reasoning at the patch_lessons twin (api_roles.go,
	// HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost). The receipt below
	// stays outside the gate and unchanged.
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
