package main

// api_doc_sizes.go — peek_doc_sizes (GET /api/doc-sizes): a role's insight size
// is reported by no other listing, so this serves all three capped segments in
// one call. It deliberately carries NO document text, so its size depends only
// on how many roles and manuals exist.

import (
	"net/http"
	"unicode/utf8"
)

// Each segment is reported against ITS OWN cap (T-ae38 split them). Caps are
// read ONCE for the whole listing, so one response cannot straddle a PATCH
// /api/settings. Sizes come from the same fold* helpers as get_role /
// get_insight; only their per-call cap is replaced by the once-read value.
func (s *apiServer) HandlePeekDocSizesApiDocSizesGet(w http.ResponseWriter, r *http.Request) {
	dutyCap := s.dutyCap()
	insightCap := s.insightCap()
	sopCap := s.manualSopCap()

	roleKeys, err := s.listRoleKeys()
	if err != nil {
		internalError(w, err)
		return
	}
	roles := []roleDocSizesDTO{}
	for _, roleKey := range roleKeys {
		duty, err := s.foldRoleDefDTO(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if duty == nil {
			// Same fail-quiet posture as GET /api/roles.
			continue
		}
		insight, err := s.foldInsightDTO(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		roles = append(roles, roleDocSizesDTO{
			RoleKey: roleKey,
			Duty:    docSizeDTO{SizeChars: duty.SizeChars, CapChars: dutyCap},
			Insight: docSizeDTO{SizeChars: insight.SizeChars, CapChars: insightCap},
		})
	}

	manuals, err := s.foldTaskManuals()
	if err != nil {
		internalError(w, err)
		return
	}
	taskManuals := []taskManualDocSizesDTO{}
	for _, m := range manuals {
		taskManuals = append(taskManuals, taskManualDocSizesDTO{
			TypeKey: m.TypeKey,
			Sop:     docSizeDTO{SizeChars: utf8.RuneCountInString(m.SopMD), CapChars: sopCap},
		})
	}

	writeJSON(w, http.StatusOK, docSizesDTO{Roles: roles, TaskManuals: taskManuals})
}
