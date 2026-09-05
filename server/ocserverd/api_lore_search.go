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
// 🔴 WHY THE ANSWER STILL CARRIES `applied`. It no longer explains a tier —
// owner removed the action axis and the T1/T2 tier with it on 2026-09-05 — but
// it is still the only way a caller can tell "this filter was applied" from
// "this filter was dropped on the floor", and `query_match` in particular says
// the `query` filter is LITERAL rather than semantic. It is a required part of
// every response, not a debugging extra.

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
	if body.Limit != nil {
		search.Limit = *body.Limit
	}

	got, err := s.dal.SearchLore(search)
	if err != nil {
		writeLoreSearchError(w, err)
		return
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
		entries = append(entries, LoreSearchHitDTO{
			EntryId: h.Entry.ID,
			// 🔴 一筆 hit 帶的是**標題**，不是內容 —— 這是 owner 2026-09-05 定的
			// 四層深度的第 ② 層，他逐字：
			//   「agent 在 resume summary 看到 target list，在做跟 target 有關的
			//     事情時查一下有跟該 target 相關的什麼記憶 (list of titles)，然後
			//     覺得有相關的或是重要的就自己再去 read 讀進來 content。」
			// 以及「title 應該就是 agent 透過 target 會看到的列表 因為這會決定
			// 他們要不要看內容」。
			//
			// 🔴 `content` 從這裡拿掉了，而那是這一層存在的**全部理由**：帶著它，
			// 一次查詢就把每一條的整段內容倒進 agent 的 context —— 那正是這張票
			// 要治的病（開機無條件整份載入）。量過：27 條標題全列出來是 512 字元
			// ≈ 130 tokens；同樣 27 條的 content 是兩個數量級以上。
			// ⚠️ 破壞性改變。射程內沒有真的使用者（origin/main 上 lore 的檔案數
			// 是 0），所以代價是量得到的零，不是「應該還好」。
			//
			// ⚠️ `trigger` 留著是我的判斷，不是他講的：它是這條被撈出來的**理由**
			// （對象 × 活動），少了它，一串標題說不出自己為什麼在這裡。它很短，
			// 跟 content 不同一個量級。可以推翻。
			Trigger: h.Entry.Trigger,
			Heading: h.Entry.Heading,
			// 星等要在這一層，因為它就是重要性（owner：「評分也改了不用 用星等
			// 取代 因為 impact 本就是重要性」）—— 一串標題如果不帶重要性，agent
			// 只能照順序看，而順序不是重要性。
			ImpactStars: h.Entry.ImpactStars,
			Origin:      h.Entry.Origin,
			Subjects:    subjects,
		})
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
			Total:     got.Total,
			Truncated: got.Truncated,
		}),
	}, loreAnchorFromRoster)

	writeJSON(w, http.StatusOK, LoreSearchResultDTO{
		Entries: entries,
		Applied: LoreSearchAppliedDTO{
			Subject:    search.SubjectKey,
			Query:      search.Query,
			QueryMatch: loreQueryMatchLiteral,
			Limit:      limit,
		},
		Total:             got.Total,
		Truncated:         got.Truncated,
		SubjectResolved:   got.SubjectResolved,
		UnresolvedSubject: got.UnresolvedSubject,
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
