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
		// `events`的四種拒絕。它們是 422 而不是 500：一筆事件缺時間、缺主動語態的
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
		errors.Is(err, ErrLoreEntityMergeCycle):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrLoreEntryUnknown):
		// ⚠️ 這一支今天**不再收任何條目 id**：唯一一個是 `supersedes`，而那一格已經
		// 被 owner 2026-09-06「都改掉」拿掉了。這一行因此是一道今天到不了的分支，
		// 留著是因為它仍然是「這條路上如果冒出一個不存在的條目 id，答案是 404」的
		// 唯一一句話 —— 拿掉它，下一個往這條路加 id 參數的人會撞到 500。
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
	// 🔴 標題格與內容格在這裡被要求「必須出現」。`events`是 0..N。
	// ⚠️ 這份清單以前還有 "trigger"（`rc-9002654dd81c` 併進 heading）與
	// "impact_stars"（owner 2026-09-06「都改掉」拿掉了整格）。兩者今天都不是合法的
	// key —— 送它們會被 422 指名擋下來，而那是對的：一個被靜默忽略的 body key 會讓
	// 寫入者以為他寫下了一句沒有人存下來的話。
	if !decodeJSONBodyStrict(w, r, &body, "heading", "content", "subjects") {
		return
	}
	write := LoreWrite{
		Heading:  body.Heading,
		Content:  body.Content,
		Subjects: body.Subjects,
		ActorID:  currentActor(r),
	}
	// 🔴 `events`。人／地／物用 strOrEmpty 折成空字串，而空字串在這一層以下就是
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
	})
}
