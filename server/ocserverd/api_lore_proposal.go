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
	// 🔴 一份 `remove` 提案沒有版本可以寫，這是**那份提案的狀態**不對，不是請求
	// 本身壞掉 —— 所以是 409，不是 422。422 對呼叫者說「你的 body 有問題」，而他
	// 改 body 改一輩子也不會讓這份提案變成可以核可的：他要走的是 retire_lore_entry。
	// 這跟核可對象審核那條路遇到「這個 entity 不是 pending」時回 409 是同一句話。
	case errors.Is(err, ErrLoreProposalNotUpdate):
		writeError(w, http.StatusConflict, err.Error())
	// 沒有這份提案，跟沒有這條條目一樣是 404：兩者都是「你指的東西不存在」。
	case errors.Is(err, ErrLoreProposalUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrLoreProposalKindUnknown),
		errors.Is(err, ErrLoreProposalFaultUnknown),
		errors.Is(err, ErrLoreProposalEncountered),
		errors.Is(err, ErrLoreProposalEvidence),
		errors.Is(err, ErrLoreProposalBaseBlank),
		errors.Is(err, ErrLoreProposalRemoveBody),
		errors.Is(err, ErrLoreProposalRemoveEvents),
		// 🔴 漏了 `events` 是 422，跟少了一格 `content` 同一個層級 —— 而它是
		// 一個**很容易被讀成成功**的錯誤：不擋的話，一份沒提到第 5 格的提案會
		// 主張把事件全刪掉，而那個主張沒有任何人寫下來過。
		errors.Is(err, ErrLoreProposalEventsMissing),
		errors.Is(err, ErrLoreProposalNoChange),
		// 第 5 格逐列的四種拒絕，跟寫入路徑用**同一組**錯誤值：核可一份提案等於
		// 走一次普通寫入，所以寫入會拒絕的事件在這裡就要被拒絕，否則它會躺在
		// 佇列裡，看起來跟一份可以被核可的提案一模一樣。
		errors.Is(err, ErrLoreEventTimeMissing),
		errors.Is(err, ErrLoreEventWhatBlank),
		errors.Is(err, ErrLoreEventKeyMalformed),
		errors.Is(err, ErrLoreEventKeyUnknownType),
		// 🔴 一份 `update` 被前兩格的**寫入路徑錯誤**擋下來，用的是寫入路徑自己的
		// 錯誤值而不是提案專屬的新錯誤：接受一份提案等於走一次普通寫入，所以寫入
		// 會拒絕的東西在這裡就要被拒絕，否則它會躺在佇列裡，看起來跟一份可以被
		// 接受的提案一模一樣。
		// 🔴 標題與星等走同一條路，而它們是這一批新加的：一份 heading 空白的
		// 提案是**呼叫者改得掉**的錯（他補一句就好），所以是 422。漏掉這兩行的
		// 代價不是「錯誤碼難看」—— 沒有被列舉的錯誤會掉到最後的 internalError，
		// 呼叫者收到 500，而 500 的意思是「伺服器壞了，你重試」，那會讓一個
		// 補得好的提案永遠不被補。
		errors.Is(err, ErrLoreHeadingBlank),
		// 🔴 標題超長走同一條路，而且它有**兩個**進入點：送出時的形狀檢查，以及
		// 核可時 ApplyLoreProposal 再擋的那一次。只擋前者等於留一條繞過的路 ——
		// 核可一份提案會把 heading 寫回條目，所以那一步就是一次寫入。
		errors.Is(err, ErrLoreHeadingTooLong),
		errors.Is(err, ErrLoreImpactStarsRange),
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
		// 提案帶的是**完整的新版本**：六格 + 第 5 格的整份事件清單（負責人
		// 2026-09-03 裁定，卡 rc-e5c34500face；2026-09-05 rc-bbccbeb3d9e6 逐字
		// 「任何修改都是提案的一環」把標題與星等一併收進來）。
		// 🔴「完整」現在對整條條目是真的成立的，而在此之前它不是：標題與星等收
		// 不到，核可寫下的原文因此宣稱條目沒有標題。
		Heading:     strOrEmpty(body.Heading),
		Trigger:     strOrEmpty(body.Trigger),
		Content:     strOrEmpty(body.Content),
		RetireWhen:  strOrEmpty(body.RetireWhen),
		Impact:      strOrEmpty(body.Impact),
		ImpactStars: intOr(body.ImpactStars, 0),
		Events:      loreProposeEvents(body.Events),
		ActorID:     currentActor(r),
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
	out.CurrentEvents = loreEventDTOs(list.CurrentEvents)
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
			Heading:        p.Heading,
			Trigger:        p.Trigger,
			Content:        p.Content,
			RetireWhen:     p.RetireWhen,
			Impact:         p.Impact,
			ImpactStars:    p.ImpactStars,
			Events:         loreEventDTOs(p.Events),
			EventsAdded:    loreEventDTOs(p.EventsAdded),
			EventsRemoved:  loreEventDTOs(p.EventsRemoved),
			Body:           p.Body,
			Sha256:         p.SHA256,
			ActorId:        p.ActorID,
			CreatedTs:      p.CreatedTS,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// HandleAcceptLoreProposalApiLoreEntriesEntryIdProposalsProposalIdAcceptPost —
// POST /api/lore/entries/{entry_id}/proposals/{proposal_id}/accept.
//
// 🔑 THIS IS THE RULING ARRIVING. ApplyLoreProposal has been able to land a
// proposal since the DAL shipped, and nothing could reach it: the mechanism was
// complete and 「誰可以按」 was open, so the whole feature was unreachable rather
// than half-built. The owner closed that on rc-a896af93d4f9 — 「你 ＋ 銀月（沿用
// 現有前例）」 — and this route is that sentence, spelled principalAdminAgent in
// the route table and nowhere else.
//
// 🔴 THE CLASS GATE IS NOT RE-DERIVED HERE, for the same reason the entity
// review routes do not re-derive theirs: `Requires` is where the ruling is
// written down, and a second copy in this function would be a second answer that
// drifts the first time one of them is edited. What this layer supplies is the
// one fact only it can know — WHO is asking, from the VERIFIED token — and the
// mapping of the seam's named errors onto status codes.
//
// 🔴 actorID COMES FROM THE TOKEN AND CAN NEVER COME FROM A BODY. This route has
// no body at all, and that is deliberate rather than minimal: the one record an
// acceptance leaves is the new revision's actor_id, so a body field that could
// name somebody else would be a forged signature on the only evidence there is.
//
// 🔴 BOTH HALVES OF THE ADDRESS ARE CHECKED, AND THE MISMATCH IS NAMED. Proposal
// ids are global, so ignoring `entry_id` would make
// `/entries/<anything>/proposals/<real id>/accept` rewrite the entry the proposal
// actually belongs to while the path said otherwise — the same failure the
// revision route's entry scoping exists to prevent, except this one WRITES. The
// refusal says which entry the proposal is really filed against; a silent 404
// would leave a reviewer re-typing the id he already had right.
//
// ⚠️ WHAT IS NOT HERE, AND WHY. There is no decline/退回 exit and no arbitration
// journal: the owner ruled on WHO may accept and on nothing else, so both would
// be this layer deciding for him. The whole record of a verdict is the new
// lore_revision row, whose actor_id is the accepter.
func (s *apiServer) HandleAcceptLoreProposalApiLoreEntriesEntryIdProposalsProposalIdAcceptPost(
	w http.ResponseWriter, r *http.Request, entryID, proposalID string,
) {
	p, err := s.dal.GetLoreProposal(proposalID)
	if err != nil {
		internalError(w, err)
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound,
			"lore: no proposal carries the id '"+proposalID+"'")
		return
	}
	if p.EntryID != entryID {
		writeError(w, http.StatusNotFound,
			"lore: proposal '"+proposalID+"' is filed against entry '"+p.EntryID+
				"', not '"+entryID+"' — the entry id in the path is a CONSTRAINT, not "+
				"decoration: proposal ids are global, so accepting it through this "+
				"address would rewrite an entry the path does not name")
		return
	}
	applied, err := s.dal.ApplyLoreProposal(proposalID, currentActor(r), nowSecs())
	if err != nil {
		writeLoreProposalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, LoreProposalAppliedDTO{
		ProposalId:  applied.ProposalID,
		EntryId:     applied.EntryID,
		RevisionId:  int(applied.RevisionID),
		Sha256:      applied.SHA256,
		EventsAfter: applied.EventsAfter,
	})
}

// loreProposeEvents turns the wire's `events` into the seam's, KEEPING THE
// DIFFERENCE BETWEEN 「沒送」 AND 「送了一個空陣列」.
//
// 🔴 nil in ⇒ nil out, and that is the whole point of this function existing
// instead of a `range` at the call site. A missing key means the proposal never
// said anything about 第 5 格, and CreateLoreProposal refuses an `update` like
// that (ErrLoreProposalEventsMissing). An empty array is a CLAIM — 「這條不該有
// 事件」 —— and it is accepted, with events_removed showing the reviewer exactly
// what it would delete. Folding the two into one empty slice here would make a
// forgotten field clear the fifth cell where nobody can see it.
//
// 人／地／物 用 strOrEmpty 折成空字串，跟寫入路徑同一條規則：省略一個 key 跟送
// 一個空字串是同一件事（「我不知道」），而且**不會**被補成「未知」。
func loreProposeEvents(in *[]LoreEventInputDTO) []LoreEvent {
	if in == nil {
		return nil
	}
	out := make([]LoreEvent, 0, len(*in))
	for _, ev := range *in {
		out = append(out, LoreEvent{
			HappenedTS: ev.HappenedTs,
			What:       ev.What,
			Actor:      strOrEmpty(ev.Actor),
			Place:      strOrEmpty(ev.Place),
			Object:     strOrEmpty(ev.Object),
		})
	}
	return out
}

// loreEventDTOs renders events onto the wire, non-nil so the JSON carries `[]`
// rather than `null` — a reader that has to treat the two as the same thing
// eventually treats one wrongly.
//
// 人／地／物 原樣送出。空的就是空的：這一層不會在渲染時補「未知」，否則
// 「查不出是誰」跟「還沒有人去查」在線上就再也分不開了。
func loreEventDTOs(evs []LoreEvent) []LoreEventDTO {
	out := make([]LoreEventDTO, 0, len(evs))
	for _, ev := range evs {
		out = append(out, LoreEventDTO{
			HappenedTs: ev.HappenedTS,
			What:       ev.What,
			Actor:      ev.Actor,
			Place:      ev.Place,
			Object:     ev.Object,
		})
	}
	return out
}
