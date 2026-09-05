package main

// api_lore_read.go — T-33, hop ③: the route that hands back what was
// compressed away.
//
// 🔴 THIS IS THE HOP THE TICKET WAS OPENED FOR. The owner's words when he
// opened it were 「原始資訊可以保留讓我們可以重新判定一些東西」. Before this
// file, the original WAS being kept — the entry and its first revision are one
// transaction — and no path served it. That state is worse than it sounds: the
// database satisfies the requirement, every count agrees, and no agent can act
// on any of it. 「保留」 was true of the store and false of every reader.
//
// 🔴 ADDRESSING IS ENTIRELY IN THE PATH, AND THAT IS THE DESIGN, NOT A HABIT.
// This station's router ignores an undeclared query parameter on every route it
// serves and answers 200. So `?revision=3` would have been a way for a caller to
// ask for one revision and quietly be handed the latest one, with the response
// looking exactly right. A path that does not match is a 404, which is loud.

import (
	"net/http"
	"strconv"
)

// HandleGetLoreEntryApiLoreEntriesEntryIdGet — GET /api/lore/entries/{entry_id}.
func (s *apiServer) HandleGetLoreEntryApiLoreEntriesEntryIdGet(
	w http.ResponseWriter, r *http.Request, entryID string) {
	entry, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "lore entry '"+entryID+"' not found")
		return
	}

	// 🔴 A RETIRED ENTRY IS STILL READABLE BY ID, and that is the ruling rather
	// than an oversight. `retired` means "no longer RETRIEVED" — search and the
	// boot directory exclude it — and nothing else. Hiding it here as well would
	// make retirement a delete through the back door, and the one path that could
	// answer "what did the thing we stopped using actually say" would be the path
	// that refuses.
	original, sha, writtenBy := "", "", ""
	if rev, err := s.dal.LatestLoreRevision(entryID); err != nil {
		internalError(w, err)
		return
	} else if rev != nil {
		original, sha, writtenBy = rev.Body, rev.SHA256, rev.ActorID
	}

	rows, err := s.dal.ListLoreRevisions(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	revisions := make([]LoreRevisionRowDTO, 0, len(rows))
	for _, row := range rows {
		revisions = append(revisions, LoreRevisionRowDTO{
			RevisionId:  int(row.ID),
			ActorId:     row.ActorID,
			CreatedTs:   row.CreatedTS,
			ShrinkChars: row.ShrinkChars,
			Sha256:      row.SHA256,
		})
	}

	subjects, _, err := s.dal.loreSubjectKeys(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if subjects == nil {
		subjects = []string{}
	}
	// 🔴 `events`是一次**明確的**讀取，不是 GetLoreEntry 順手帶回來的。LoreEntry
	// 裡刻意沒有 Events 欄位（見 dal_lore.go），所以每一個想要事件的呼叫者都必須
	// 自己說一次要——包括這一條。
	events, err := s.dal.ListLoreEvents(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	// 非 nil，讓線上是 `[]` 而不是 `null`：一個要把 null 跟空陣列當同一件事處理的
	// 讀者，遲早會有一邊處理錯。
	eventDTOs := make([]LoreEventDTO, 0, len(events))
	for _, ev := range events {
		// 人／地／物原樣送出。空的就是空的——這一層不會在渲染時補「未知」，
		// 否則「查不出是誰」跟「還沒有人去查」在線上就再也分不開了。
		eventDTOs = append(eventDTOs, LoreEventDTO{
			HappenedTs: ev.HappenedTS,
			What:       ev.What,
			Actor:      ev.Actor,
			Place:      ev.Place,
			Object:     ev.Object,
		})
	}

	// 🔴 THE ROW IS FILED HERE, AFTER THE 404s, AND THAT IS WHAT MAKES IT
	// COUNTABLE. Above this line the entry may not exist; below it, the original
	// is genuinely about to be handed over. A row for an id that named nothing
	// would say an entry was used when nothing was — the same non-event rule the
	// boot fold's own preview exclusion follows.
	//
	// One read is ONE ROW even when the same actor opens the same entry three
	// times in a row. That repetition is the measurement, not a duplicate:
	// inside one session it says the `content` cell is not enough and the agent
	// keeps coming back to the原文; across sessions it says the entry carries
	// weight. The session anchor on the row is what tells those apart.
	s.recordLoreRecall(LoreRecall{
		ActorID: currentActor(r),
		Query:   loreRecallQueryEntryRead,
		Returned: encodeLoreRecallReturned(loreRecallReturned{
			Entries: []string{entry.ID},
		}),
	}, loreAnchorFromRoster)

	writeJSON(w, http.StatusOK, LoreEntryDetailDTO{
		EntryId:    entry.ID,
		Heading:    entry.Heading,
		Content:    entry.Content,
		RetireWhen: entry.RetireWhen,
		Impact:     entry.Impact,
		// 🔴 星等與審核旗標都原樣送出，而 0 就是 0 —— 這一層不會把「還沒判」
		// 折成 1，也不會把它藏起來。讀的人要分得出「沒有人判過」與「判為最輕」，
		// 而唯一能讓他分得出來的，就是這裡不去動它。
		ImpactStars: entry.ImpactStars,
		// ⚠️ `reviewed` 讀得到、寫不到：這一版沒有任何路由蓋得了章（見 dal_lore.go
		// 上 loreEntryColumns 的說明），所以它對每一條都是 false。它照樣送出來，
		// 因為一個永遠是 false 的欄位，跟一個不存在的欄位，對前端是兩件事。
		Reviewed:   entry.Reviewed,
		Events:     eventDTOs,
		Status:     entry.Status,
		Supersedes: entry.Supersedes,
		Origin:     entry.Origin,
		Subjects:   subjects,
		Original:   original,
		Sha256:     sha,
		WrittenBy:  writtenBy,
		Revisions:  revisions,
	})
}

// HandleGetLoreRevisionApiLoreEntriesEntryIdRevisionsRevisionIdGet —
// GET /api/lore/entries/{entry_id}/revisions/{revision_id}.
func (s *apiServer) HandleGetLoreRevisionApiLoreEntriesEntryIdRevisionsRevisionIdGet(
	w http.ResponseWriter, r *http.Request, entryID string, revisionID string) {
	// A non-numeric revision id is a 404 rather than a 422: this is an ADDRESS,
	// and an address that names nothing is "not found" whatever it is made of.
	// Answering 422 would tell a caller its request was malformed when the
	// honest answer is that nothing lives there.
	//
	// ⚠️ THIS BRANCH IS NOT LOAD-BEARING, AND SAYING SO IS THE POINT. Mutating it
	// away (`n, _ := ...; if false {`) leaves EVERY test green — measured, not
	// assumed — because ParseInt answers 0 on failure and no revision carries id
	// 0, so the scoped lookup below returns nil and the same 404 comes out of the
	// other door. It is kept because "garbage parses to a value no row can have"
	// is an implicit contract that nothing states and nothing tests, whereas this
	// form says what it means. It is NOT kept under the pretence that a test
	// would catch its removal — a guard nothing can distinguish is exactly the
	// always-green defence this ticket keeps finding.
	//
	// 🔴 WHAT IS TESTED IS THE PREMISE, NOT THE GUARD:
	// TestLoreRevisionIdsNeverStartAtZero pins "no revision carries id 0", which
	// is the only reason the sentence above is safe. That test cannot fail today.
	// The day it DOES fail, this branch has started carrying load — and the
	// failure says so — instead of the whole argument quietly becoming false in a
	// comment nobody re-reads.
	n, err := strconv.ParseInt(revisionID, 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound,
			"lore entry '"+entryID+"' has no revision '"+revisionID+"'")
		return
	}
	rev, err := s.dal.GetLoreRevision(entryID, n)
	if err != nil {
		internalError(w, err)
		return
	}
	// 🔴 ONE 404 COVERS BOTH "no such entry" AND "that revision belongs to a
	// different entry", and the wording says so. The lookup is scoped to the
	// entry on purpose — revision ids are global, so an unscoped read would serve
	// another entry's original through this address and a mistyped entry id would
	// hand back somebody else's text with nothing to signal it.
	if rev == nil {
		writeError(w, http.StatusNotFound,
			"lore entry '"+entryID+"' has no revision '"+revisionID+"'")
		return
	}
	// A revision read is a SEPARATE row kind from an entry read, not the same
	// event seen twice. Opening the entry hands back the latest original beside
	// the six fields; coming back here names ONE revision, which is an agent
	// working out what an entry used to say — the strongest form of 「短版不夠
	// 用」 the journal can observe.
	s.recordLoreRecall(LoreRecall{
		ActorID: currentActor(r),
		Query:   loreRecallQueryRevisionRead,
		Returned: encodeLoreRecallReturned(loreRecallReturned{
			Entries: []string{rev.EntryID},
		}),
	}, loreAnchorFromRoster)

	writeJSON(w, http.StatusOK, LoreRevisionDTO{
		RevisionId:  int(rev.ID),
		EntryId:     rev.EntryID,
		Body:        rev.Body,
		Sha256:      rev.SHA256,
		ActorId:     rev.ActorID,
		CreatedTs:   rev.CreatedTS,
		ShrinkChars: rev.ShrinkChars,
	})
}
