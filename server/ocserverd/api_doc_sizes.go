package main

// api_doc_sizes.go — the capped-document SIZE overview (peek_doc_sizes;
// GET /api/doc-sizes).
//
// WHY IT EXISTS: one of the three capped segments — a role's insight — is
// reported by NO listing on this station at any price. The manual's sop_md size
// and cap are already on list_task_manuals' light view, and the role
// definition's size and cap already ride every list_roles row; so those two are
// cheap-but-scattered, while insight is simply unavailable in bulk. This route
// serves all three from one place so "which long-lived document is nearly
// full?" is a single call.
//
// WHAT IT DELIBERATELY DOES NOT CARRY: any document text. That is not a
// nice-to-have — it is the entire property. The response size is a function of
// how many roles and manuals exist and of nothing else, so a station whose docs
// grow does not make this call more expensive.

import (
	"net/http"
	"unicode/utf8"
)

// HandlePeekDocSizesApiDocSizesGet answers GET /api/doc-sizes.
//
// Each capped segment is reported against ITS OWN cap. They stopped sharing a
// number when T-ae38 split one cap into several; a listing that quoted one
// number for all of them would be wrong the first time an owner raised any
// single one, and wrong silently — every row would still look plausible.
//
// The caps are read ONCE each for the whole listing, the same discipline
// HandleListTaskManualsApiTaskManualsGet follows: per-row reads could straddle a
// PATCH /api/settings and hand back one response quoting two different caps for
// the same segment.
//
// The SIZES come from the very same fold* helpers the per-document GETs use
// (foldRoleDefDTO / foldInsightDTO), so a size reported here cannot drift from
// what get_role / get_insight report for the same document. Only the cap field is replaced with the once-read value — the
// helpers read their own cap per call, which is right for a single-document read
// and wrong for a listing.
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
			// Same fail-quiet posture as GET /api/roles: a key that no longer
			// folds is simply not a role, so it is not a row here either.
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

	manuals, err := s.dal.ListTaskManuals()
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
