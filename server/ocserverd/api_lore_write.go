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
	case errors.Is(err, ErrLoreSymptomsBlank),
		errors.Is(err, ErrLoreShortBlank),
		errors.Is(err, ErrLoreFalsifyBlank),
		errors.Is(err, ErrLoreInstanceBlank),
		errors.Is(err, ErrLoreSubjectsEmpty),
		errors.Is(err, ErrLoreSubjectBlank),
		errors.Is(err, ErrLoreSubjectMalformed),
		errors.Is(err, ErrLoreSubjectUnknownType),
		errors.Is(err, ErrLoreActionBlank),
		errors.Is(err, ErrLoreLabelTooLong),
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
	if !decodeJSONBodyStrict(w, r, &body, "symptoms", "short", "falsify", "instance", "origin", "subjects") {
		return
	}
	write := LoreWrite{
		Label:        strOrEmpty(body.Label),
		Symptoms:     body.Symptoms,
		Short:        body.Short,
		Falsify:      body.Falsify,
		Instance:     body.Instance,
		ResidualRisk: strOrEmpty(body.ResidualRisk),
		Origin:       body.Origin,
		Supersedes:   strOrEmpty(body.Supersedes),
		Subjects:     body.Subjects,
		ActorID:      currentActor(r),
	}
	if body.Actions != nil {
		write.Actions = *body.Actions
	}
	got, err := s.dal.CreateLoreEntry(write, nowSecs())
	if err != nil {
		writeLoreWriteError(w, err)
		return
	}

	// 🔴 `degraded` IS READ BACK OFF THE STORED ROW, not computed from the body
	// that was sent. The two agree today; the point is that they will still agree
	// the day the write seam starts normalising a field, because this asks the
	// entry rather than the request.
	//
	// ⚠️ 2026-09-02 之後這條路徑上的 `degraded` 一律是 false——`falsify` 與
	// `instance` 兩格空白已經在 CreateLoreEntry 被擋掉了。這個旗標不能因此拿掉：
	// 站上還有那道裁定之前寫下的條目，兩格都是空的，而這個旗標是唯一看得見它們的
	// 東西。
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
		Degraded:        entry.IsDegraded(),
	})
}
