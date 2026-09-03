package main

// api_lore_proposal.go — T-33. 提案的門：一個 agent 說「這條應該長這樣」，
// 而審核看到的差異就是會落地的東西.
//
// 🔴 THIS FILE HOLDS NO POLICY, and in particular it does NOT re-derive the
// staleness check. Which digest counts as current, and what happens when the one
// a proposer read is not it, is answered by CreateLoreProposal / ListLoreProposals
// and by nothing else. This layer supplies the one fact only it can know — WHO is
// asking, from the verified token — and maps named errors onto status codes. A
// second copy of the comparison here would be a second answer to the question the
// whole feature turns on, and the two would drift silently, in the direction
// where everything still looks fine.
//
// 🔴 409 IS THE STALENESS CODE AND IT IS NOT 422. A 422 says "your request is
// malformed"; the proposal is not malformed, it is aimed at a version of the
// world that has moved. 409 Conflict is the same answer the revive route gives
// for 「the entry is not in the state you believe it is in」, and a proposer that
// retries a 409 unchanged will get the same answer forever — which is correct,
// because the fix is to go and re-read.

import (
	"errors"
	"net/http"
)

// writeLoreProposalError maps the proposal seam's named errors onto the wire.
//
// The default is 500 rather than 400 for the reason the write seam gives: a DAL
// error nobody anticipated is not the caller's fault, and reporting it as one
// sends a proposer off to edit a body that was fine.
func writeLoreProposalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrLoreEntryUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrLoreProposalStale):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrLoreActorBlank):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrLoreProposalKindUnknown),
		errors.Is(err, ErrLoreProposalFaultUnknown),
		errors.Is(err, ErrLoreProposalEncountered),
		errors.Is(err, ErrLoreProposalEvidence),
		errors.Is(err, ErrLoreProposalBaseBlank),
		errors.Is(err, ErrLoreProposalRemoveBody),
		errors.Is(err, ErrLoreProposalNoChange),
		// 🔴 一份 `update` 被前兩格的**寫入路徑錯誤**擋下來，用的是寫入路徑自己的
		// 錯誤值而不是提案專屬的新錯誤：接受一份提案等於走一次普通寫入，所以寫入
		// 會拒絕的東西在這裡就要被拒絕，否則它會躺在佇列裡，看起來跟一份可以被
		// 接受的提案一模一樣。
		errors.Is(err, ErrLoreTriggerBlank),
		errors.Is(err, ErrLoreContentBlank):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		internalError(w, err)
	}
}

// HandleProposeLoreChangeApiLoreEntriesEntryIdProposalsPost — POST
// /api/lore/entries/{entry_id}/proposals.
//
// ⚠️ `base_sha256` COMES FROM THE BODY AND MUST. It is the digest the proposer
// says he read, and the whole check is that his claim and the store agree. Taking
// it from the store here — "helpfully" filling in the current one — would make
// every submission match, the refusal unreachable, and the test that covers it
// green forever.
func (s *apiServer) HandleProposeLoreChangeApiLoreEntriesEntryIdProposalsPost(w http.ResponseWriter, r *http.Request, entryID string) {
	var body LoreProposeDTO
	if !decodeJSONBodyStrict(w, r, &body, "kind", "base_sha256", "encountered", "fault", "evidence") {
		return
	}
	got, err := s.dal.CreateLoreProposal(LoreProposal{
		EntryID:     entryID,
		Kind:        body.Kind,
		BaseSHA256:  body.BaseSha256,
		Encountered: body.Encountered,
		Fault:       body.Fault,
		Evidence:    body.Evidence,
		// 提案這一批帶得動的是**四格**。第 5 格沒有欄位可以帶：CreateLoreProposal
		// 渲染 body 時接上條目**目前**的事件，暫定語意是「四格改成這樣，事件維持
		// 現狀」。
		//
		// 🔴🔴 這個語意是**暫定的，而且已知是要被換掉的**：負責人 2026-09-03 在卡
		// rc-e5c34500face 裁定「改得動 —— 提案就該帶完整的新版本，包含所有事件」。
		// 這一批沒有做，因為帶事件需要一張新表（lore_proposal_event），範圍另計。
		// ⇒ 不要在這一層自己補一個 events 欄位（沒有地方存），也不要讓其他邏輯去
		//   假設「事件不會被提案改動」——那個假設已經知道是錯的。
		Trigger:    strOrEmpty(body.Trigger),
		Content:    strOrEmpty(body.Content),
		RetireWhen: strOrEmpty(body.RetireWhen),
		Problem:    strOrEmpty(body.Problem),
		ActorID:    currentActor(r),
	}, nowSecs())
	if err != nil {
		writeLoreProposalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, LoreProposalReceiptDTO{
		ProposalId:     got.ProposalID,
		BaseRevisionId: int(got.BaseRevisionID),
		BaseSha256:     got.BaseSHA256,
		Sha256:         got.SHA256,
	})
}

// HandleListLoreProposalsApiLoreEntriesEntryIdProposalsGet — GET
// /api/lore/entries/{entry_id}/proposals.
//
// 🔴 AN ENTRY NOBODY HAS PROPOSED ANYTHING FOR ANSWERS 200 WITH AN EMPTY LIST,
// not 404. 「沒有人提案」 and 「沒有這條」 are different facts and a reviewer
// working a queue has to be able to tell them apart; the 404 here belongs to the
// entry not existing, which is what ErrLoreEntryNoOriginal / the lookup below
// answer.
func (s *apiServer) HandleListLoreProposalsApiLoreEntriesEntryIdProposalsGet(w http.ResponseWriter, r *http.Request, entryID string) {
	entry, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "lore entry '"+entryID+"' not found")
		return
	}
	list, err := s.dal.ListLoreProposals(entryID)
	if err != nil {
		writeLoreProposalError(w, err)
		return
	}
	out := LoreProposalListDTO{
		EntryId:           list.EntryID,
		CurrentRevisionId: int(list.CurrentRevisionID),
		CurrentSha256:     list.CurrentSHA256,
		// Non-nil so the wire carries `[]` rather than `null`: a reader that has
		// to treat the two as the same thing eventually treats one wrongly.
		Proposals: make([]LoreProposalDTO, 0, len(list.Proposals)),
	}
	for _, p := range list.Proposals {
		out.Proposals = append(out.Proposals, LoreProposalDTO{
			ProposalId:     p.ID,
			Kind:           p.Kind,
			Fault:          p.Fault,
			Encountered:    p.Encountered,
			Evidence:       p.Evidence,
			BaseRevisionId: int(p.BaseRevisionID),
			BaseSha256:     p.BaseSHA256,
			Stale:          p.Stale,
			Trigger:        p.Trigger,
			Content:        p.Content,
			RetireWhen:     p.RetireWhen,
			Problem:        p.Problem,
			Body:           p.Body,
			Sha256:         p.SHA256,
			ActorId:        p.ActorID,
			CreatedTs:      p.CreatedTS,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
