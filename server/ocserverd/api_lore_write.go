package main

// api_lore_write.go — T-33. The door that lets a lore entry exist at all.
//
// 🔴 WHY THIS ROUTE IS THE ONE THAT MATTERED. Every ruling the owner made on
// 2026-09-01 — write lore first and skip the rest of the list, review AFTER the
// write, mark what has been reviewed — is a rule about writing entries, and
// until this handler existed the station served no way to write one. The consequence was not "the feature is incomplete": the
// entry table was empty, an empty subject directory renders as nothing at all,
// and so no member had ever seen the directory. A rule about a tool nobody has
// is worse than no rule, because it reads like a capability.
//
// 🔴 THIS FILE HOLDS NO POLICY. Which fields are refused, which subject key
// resolves onto which entity, whether a supersede is legal — all of it lives in
// CreateLoreEntry, where the transaction is. This layer supplies the one fact
// only it can know (WHO is asking, from the verified token) and maps named
// errors onto status codes. A second copy of any of those rules here would be a
// second answer to a question that already has one.

import (
	"errors"
	"net/http"
)

// writeLoreWriteError maps the write seam's named errors onto the wire.
//
// 🔴 EVERY REFUSAL IS A 4xx THAT SAYS WHAT IS WRONG, and the default is 500
// rather than 400. A DAL error nobody anticipated is not the caller's fault, and
// reporting it as one would send a writer off to edit a body that was fine.
func writeLoreWriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrLoreHeadingBlank),
		// 🔴 標題超過 140 個字元是 422 而不是 500：送太長標題的人自己改得掉，
		// 而訊息指名了是 heading 這一格、上限多少、他送來的是多少。漏掉這一行
		// 的代價不是「錯誤碼難看」—— 沒有列舉的錯誤會掉到 internalError 變成
		// 500，而 500 的意思是「伺服器壞了，你重試」，重試永遠會失敗。
		errors.Is(err, ErrLoreHeadingTooLong),
		errors.Is(err, ErrLoreContentBlank),
		// 🔴 星等超出 0..3 是 422 而不是 500。資料庫的 CHECK 也會擋，但它回來的是
		// 一句 driver 訊息，只能被報成「伺服器出事了」——而送 star=7 的人是可以
		// 自己修好的。這裡的 422 指名了是哪一格、以及三級各是什麼意思。
		errors.Is(err, ErrLoreImpactStarsRange),
		// 第 5 格的四種拒絕。它們是 422 而不是 500：一筆事件缺時間、缺主動語態的
		// 「事」，或人／地／物寫成不是 `type:name`／型別沒被核准，都是寫入者可以
		// 自己修好的東西，而且錯誤訊息會指名是哪一格。
		errors.Is(err, ErrLoreEventTimeMissing),
		errors.Is(err, ErrLoreEventWhatBlank),
		errors.Is(err, ErrLoreEventKeyMalformed),
		errors.Is(err, ErrLoreEventKeyUnknownType),
		errors.Is(err, ErrLoreSubjectsEmpty),
		errors.Is(err, ErrLoreSubjectBlank),
		errors.Is(err, ErrLoreSubjectMalformed),
		errors.Is(err, ErrLoreSubjectUnknownType),
		errors.Is(err, ErrLoreOriginBlank),
		errors.Is(err, ErrLoreOriginMalformed),
		errors.Is(err, ErrLoreOriginUnknownType),
		errors.Is(err, ErrLoreSupersedesSelf),
		errors.Is(err, ErrLoreEntityMergeCycle):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrLoreEntryUnknown):
		// The only id this handler is given is `supersedes`, so an unknown entry
		// here is always that one: the caller named a predecessor that is not
		// there. 404 rather than 422 keeps it the same answer the governance
		// routes give for the same mistake.
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrLoreActorBlank):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		internalError(w, err)
	}
}

// HandleWriteLoreEntryApiLoreEntriesPost — POST /api/lore/entries.
//
// ⚠️ THE RECEIPT IS BUILT FROM WHAT THE WRITE RETURNED, NOT FROM THE REQUEST.
// `subject_ids` in particular is the list AFTER aliases resolved, merges were
// followed and duplicates collapsed — echoing the caller's keys back would hide
// exactly the case the field exists to reveal, which is two keys that turned out
// to be one subject.
func (s *apiServer) HandleWriteLoreEntryApiLoreEntriesPost(w http.ResponseWriter, r *http.Request) {
	var body LoreWriteDTO
	// 🔴 標題格與內容格在這裡被要求「必須出現」。第 3、4 格是選填，第 5 格是
	// 0..N——把它們列進來會讓「這條沒有後果可以寫」變成一個送不出去的請求。
	// ⚠️ 這份清單以前還有 "trigger"。`rc-9002654dd81c`（2026-09-06）把那一格併進
	// heading 之後它不再是一個合法的 key —— 送它會被 422 指名擋下來，而那是對的：
	// 一個被靜默忽略的 body key 會讓寫入者以為他寫下了一句沒有人存下來的話。
	// ⚠️ `impact_stars` 也不在這裡：省略它得到 0＝「還沒判」，那是一個合法的狀態，
	// 而要求它出現等於逼每一個寫入者當場判一個他還沒判的東西。
	if !decodeJSONBodyStrict(w, r, &body, "heading", "content", "origin", "subjects") {
		return
	}
	write := LoreWrite{
		Heading:    body.Heading,
		Content:    body.Content,
		RetireWhen: strOrEmpty(body.RetireWhen),
		Impact:     strOrEmpty(body.Impact),
		// 🔴 省略 impact_stars 折成 0，而 0 的意思是「還沒判」——**不是**「最輕」。
		// 這一層不替沒送的人補一個 1：那會讓「沒有人判過」與「判為沒弄壞任何東西」
		// 在資料庫裡永遠分不開，而 v8 的自檢正是靠這個差別找出誰漏填。
		ImpactStars: intOr(body.ImpactStars, 0),
		Origin:      body.Origin,
		Supersedes:  strOrEmpty(body.Supersedes),
		Subjects:    body.Subjects,
		ActorID:     currentActor(r),
	}
	// 🔴 第 5 格。人／地／物用 strOrEmpty 折成空字串，而空字串在這一層以下就是
	// 「沒有這一格」——**不會**被補成「未知」。省略一個 key 跟送一個空字串在這裡
	// 刻意是同一件事：兩者都是「我不知道」，而讓它們變成兩種不同的狀態只會逼下游
	// 去猜哪一種才算數。
	if body.Events != nil {
		write.Events = make([]LoreEvent, 0, len(*body.Events))
		for _, ev := range *body.Events {
			write.Events = append(write.Events, LoreEvent{
				HappenedTS: ev.HappenedTs,
				What:       ev.What,
				Actor:      strOrEmpty(ev.Actor),
				Place:      strOrEmpty(ev.Place),
				Object:     strOrEmpty(ev.Object),
			})
		}
	}
	got, err := s.dal.CreateLoreEntry(write, nowSecs())
	if err != nil {
		writeLoreWriteError(w, err)
		return
	}

	// 🔴 THE ROW IS READ BACK, AND IT IS NOT FOR A FIELD ON THE RECEIPT. It used
	// to be read back for `degraded`; that flag is gone (owner ruling
	// rc-1e32c690018d — see dal_lore.go). What is left is the post-condition
	// below: a create that answered without an error and left no row is the ONE
	// state the transaction exists to rule out, and reporting that as success is
	// how it would stay hidden.
	entry, err := s.dal.GetLoreEntry(got.EntryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if entry == nil {
		// A create that answered without an error and left no row is the one
		// state the transaction exists to rule out; reporting it as success
		// would hide it.
		internalError(w, errors.New("lore: the write left no entry at "+got.EntryID))
		return
	}

	// Both slices are non-nil so the wire carries `[]` rather than `null`. A
	// reader that has to treat null and empty as the same thing eventually
	// treats one of them wrongly.
	pending := make([]LorePendingEntityDTO, 0, len(got.Minted))
	for _, m := range got.Minted {
		pending = append(pending, LorePendingEntityDTO{
			EntityId: m.EntityID, Canonical: m.Canonical, Type: m.Type,
		})
	}
	subjects := got.SubjectIDs
	if subjects == nil {
		subjects = []string{}
	}
	writeJSON(w, http.StatusOK, LoreWriteReceiptDTO{
		EntryId:         got.EntryID,
		Sha256:          got.SHA256,
		RevisionId:      int(got.RevisionID),
		SubjectIds:      subjects,
		PendingEntities: pending,
		Superseded:      got.Superseded,
	})
}
