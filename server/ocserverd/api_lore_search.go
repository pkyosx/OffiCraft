package main

// api_lore_search.go — T-33, hop ② over the wire.
//
// 🔴 WHY THE WHOLE SELECTION LIVES IN THE BODY, AND WHY THAT IS NOT ABOUT THE
// VERB. This station's router IGNORES an undeclared query parameter on every
// route it serves and answers 200 (there is a test that fires a real request to
// pin exactly that). The JSON body decoder does the opposite: an undeclared key
// is a 422 that names it. `POST …?typo=1` is therefore just as silent as
// `GET …?typo=1` — what protects this hop is which SIDE the conditions sit on.
// Moving a condition to the query string, for any reason, removes that
// protection while leaving the verb unchanged and the tests green.
//
// 🔴 WHY THE ANSWER CARRIES `applied`. The tier labels this route emits mean
// "matched every axis you asked on" — a meaning that only exists beside the
// axes that were asked. The design's own words are "both axes intersect", and
// under the commonest call (one axis) those two readings disagree. Shipping the
// label without the axes would let the older reading survive as a silent
// misinterpretation, so `applied` is a required part of every response and not
// a debugging extra. ⚠️ The change of meaning is Kyle's ruling of 2026-09-01,
// not the design's text, and it can be overturned.

import (
	"errors"
	"net/http"
)

// loreQueryMatchLiteral names the kind of matching `query` got.
//
// 🔴 IT IS A VALUE ON THE WIRE RATHER THAN A SENTENCE IN A DOCUMENT because the
// filter is literal and the thing callers want is semantic. Two entries about
// the same situation were measured to share almost no words, so a literal filter
// reports them as unrelated while looking exactly like a filter that worked.
// Naming the kind means the day it becomes semantic, the answer says so instead
// of quietly changing.
const loreQueryMatchLiteral = "literal-substring"

func writeLoreSearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrLoreSearchLimitRange),
		errors.Is(err, ErrLoreSearchActionBlank),
		errors.Is(err, ErrLoreSearchSubjectBlank),
		errors.Is(err, ErrLoreEntityMergeCycle):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		internalError(w, err)
	}
}

// HandleSearchLoreEntriesApiLoreSearchPost — POST /api/lore/search.
func (s *apiServer) HandleSearchLoreEntriesApiLoreSearchPost(w http.ResponseWriter, r *http.Request) {
	var body LoreSearchDTO
	if !decodeJSONBodyStrict(w, r, &body) {
		return
	}
	search := LoreSearch{
		SubjectKey: strOrEmpty(body.Subject),
		Query:      strOrEmpty(body.Query),
	}
	if body.Actions != nil {
		search.Actions = *body.Actions
	}
	if body.Limit != nil {
		search.Limit = *body.Limit
	}
	if body.ForceTrustAnalogy != nil {
		search.ForceTrustAnalogy = *body.ForceTrustAnalogy
	}

	got, err := s.dal.SearchLore(search)
	if err != nil {
		writeLoreSearchError(w, err)
		return
	}

	// `tiered_by` is built from the SAME predicate the DAL tiers on, read off the
	// request that was actually decoded. Recomputing it from the DTO by hand here
	// would be a second answer to "which axes counted", and the two would drift
	// the first time an axis is added — with the symptom being a tier label that
	// disagrees with the axes printed beside it.
	tieredSubject, tieredAction := search.suppliedAxes()
	tieredBy := []string{}
	if tieredSubject {
		tieredBy = append(tieredBy, "subject")
	}
	if tieredAction {
		tieredBy = append(tieredBy, "actions")
	}
	appliedActions := search.Actions
	if appliedActions == nil {
		appliedActions = []string{}
	}
	limit := search.Limit
	if limit == 0 {
		limit = loreSearchLimitDefault
	}

	entries := make([]LoreSearchHitDTO, 0, len(got.Hits))
	for _, h := range got.Hits {
		subjects := h.Subjects
		if subjects == nil {
			subjects = []string{}
		}
		actions := h.Actions
		if actions == nil {
			actions = []string{}
		}
		entries = append(entries, LoreSearchHitDTO{
			EntryId: h.Entry.ID,
			// 🔴 一筆 hit 只帶第 1、2 格。第 3、4、5 格要用 get_lore_entry 讀——
			// 搜尋的答案是拿來「挑」的，把每一條的事件整包塞進結果列表是一個沒有
			// 人做過的大小決定。
			Trigger:       h.Entry.Trigger,
			Content:       h.Entry.Content,
			Origin:        h.Entry.Origin,
			Subjects:      subjects,
			Actions:       actions,
			Tier:          h.Tier,
			TierNote:      h.TierNote,
			TrustScope:    h.TrustScope,
			TrustFellBack: h.TrustFellBack,
		})
	}
	unmapped := got.UnmappedActions
	if unmapped == nil {
		unmapped = []string{}
	}
	// 🔴 JOURNALLED AFTER THE ANSWER IS BUILT AND BEFORE IT IS WRITTEN, and only
	// on a search that really ran — every refusal above returned already. This is
	// hop ② actually being USED, which until now left no trace anywhere: the
	// journal recorded the boot directory being put in front of an agent and
	// nothing about the agent going and looking something up. A search that
	// resolved to no subject at all (`subject_resolved: false`) still files a
	// row: 「我問了，這個對象不存在」 is a use of the memory and a signal about the
	// ontology, and dropping it would make a typo'd subject look like a search
	// nobody ran.
	s.recordLoreRecall(LoreRecall{
		ActorID:   currentActor(r),
		Query:     loreRecallQuerySearch,
		SubjectID: got.SubjectEntityID,
		Returned: encodeLoreRecallReturned(loreRecallReturned{
			Entries:   loreSearchHitIDs(got.Hits),
			Query:     search.Query,
			Subject:   search.SubjectKey,
			Actions:   search.Actions,
			Total:     got.Total,
			Truncated: got.Truncated,
		}),
	}, loreAnchorFromRoster)

	writeJSON(w, http.StatusOK, LoreSearchResultDTO{
		Entries: entries,
		Applied: LoreSearchAppliedDTO{
			Subject:           search.SubjectKey,
			Actions:           appliedActions,
			Query:             search.Query,
			QueryMatch:        loreQueryMatchLiteral,
			Limit:             limit,
			ForceTrustAnalogy: search.ForceTrustAnalogy,
			TieredBy:          tieredBy,
		},
		Total:             got.Total,
		Truncated:         got.Truncated,
		SubjectResolved:   got.SubjectResolved,
		UnresolvedSubject: got.UnresolvedSubject,
		UnmappedActions:   unmapped,
	})
}

// loreSearchHitIDs is 「撈到哪幾條」 for the journal. It reads the ids off the
// HITS that were actually assembled, not off the DTO slice built for the wire —
// the two agree today, and reading the hits keeps them agreeing the day the wire
// shape starts filtering or renaming anything.
func loreSearchHitIDs(hits []LoreSearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Entry.ID)
	}
	return out
}
