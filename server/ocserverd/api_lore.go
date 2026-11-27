package main

// 傳承（lore）write/read faces, which are also the MCP tools (the tool surface IS
// the route table; see mcp.go). Writing is MCP-only by design: an entry is
// written by the agent that just learned the thing, and the cockpit gets the
// list, the budgets, the governance verbs and the in-place edit. Selection lives
// in lore_select.go; a read face here must call it, never its own ORDER BY.

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The default matches the cockpit's 捲到底載 30 筆.
const (
	loreListDefaultLimit = 30
	loreListMaxLimit     = 200
)

func (s *apiServer) callerRosterRow(r *http.Request) (*Member, error) {
	actor := currentActor(r)
	if actor == "" {
		return nil, nil
	}
	return s.dal.GetMember(actor)
}

func (s *apiServer) HandleWriteLoreEntryApiLorePost(w http.ResponseWriter, r *http.Request) {
	var body LoreEntryWriteDTO
	if !decodeJSONBodyStrict(w, r, &body, "title", "body") {
		return
	}
	title := strings.TrimSpace(body.Title)
	text := strings.TrimSpace(body.Body)
	if title == "" || text == "" {
		writeError(w, http.StatusBadRequest,
			"title and body must both be non-empty — an entry with no text is not a shorter entry")
		return
	}
	if msg := s.loreTextOverCap(title, text); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	// A missing type is refused rather than defaulted (owner rc-714fa3879796): the
	// message is how an agent that never read the new boot document learns the field.
	loreType := strings.TrimSpace(body.LoreType)
	if loreType == "" {
		writeError(w, http.StatusBadRequest, loreTypeMissingMsg)
		return
	}
	if !ValidLoreType(loreType) {
		writeError(w, http.StatusBadRequest, loreTypeInvalidMsg(loreType))
		return
	}

	taskID := ""
	if body.TaskId != nil {
		taskID = strings.TrimSpace(*body.TaskId)
	}

	// A task with no type (臨時任務) counts as no task — owner ruling 2026-09-07
	// 「臨時任務跟無關乎任何任務一樣都是給 NULL」.
	typeKey, untypedTask := "", false
	if taskID != "" {
		t, err := s.dal.GetTask(taskID)
		if err != nil {
			internalError(w, err)
			return
		}
		if t == nil {
			writeError(w, http.StatusBadRequest, "no such task: "+taskID)
			return
		}
		typeKey = strings.TrimSpace(t.TypeKey)
		untypedTask = typeKey == ""
	}

	scopeKind, scopeKey := "", ""
	if typeKey != "" {
		scopeKind, scopeKey = LoreScopeManual, typeKey
	} else {
		// Every writer files under their own member id (owner ruling
		// rc-a43100fd0486). Known cost: two members under one role do not share a
		// 傳承, and nothing enforces one-member-per-role.
		actor := currentActor(r)
		m, err := s.callerRosterRow(r)
		if err != nil {
			internalError(w, err)
			return
		}
		switch {
		case actor == "":
			writeError(w, http.StatusBadRequest,
				"this request carries no verified member identity, so there is no boot document "+
					"to file a lore entry under.")
			return
		case m == nil:
			writeError(w, http.StatusBadRequest,
				"you have no roster row, so there is no boot document of your own to write into. "+
					"A lore entry is filed under the writer's own boot document or under a "+
					"typed task's manual; pass a typed task's task_id to write task lore instead.")
			return
		default:
			scopeKind, scopeKey = LoreScopeAgent, m.ID
		}
	}

	now := nowSecs()
	entry, err := s.dal.CreateLoreEntryMintingID(LoreEntry{
		ScopeKind: scopeKind,
		ScopeKey:  scopeKey,
		Title:     title,
		Body:      text,

		AuthorID:     currentActor(r),
		SourceTaskID: taskID,
		State:        LoreStateActive,
		// effective_ts is the only one 提到最新 moves; created_ts keeps the original.
		EffectiveTS: now,
		CreatedTS:   now,
		UpdatedTS:   now,
		LoreType:    loreType,
	})
	if err != nil {
		internalError(w, err)
		return
	}
	// A bounded receipt, not the entry — owner 2026-09-07
	// 「不要回傳自己寫出去的 payload」.
	dto := LoreEntryWriteReceiptDTO{
		Id:        entry.ID,
		Seq:       entry.Seq,
		ScopeKind: entry.ScopeKind,
		ScopeKey:  entry.ScopeKey,
		CreatedTs: entry.CreatedTS,
	}
	if untypedTask {
		dto.ScopeNote = "Task " + taskID + " has no type, so this entry was written into your own boot document (" +
			scopeKind + " / " + scopeKey + "), not into any task manual."
	}
	writeJSON(w, http.StatusOK, dto)
}

const loreTypeMissingMsg = "lore_type is required — nothing was written. Set it to the entry's type: " +
	"instruction_conflict (instruction conflict), instruction_supplement (instruction supplement), owner_decision (owner decision), " +
	"owner_preference (owner preference) or other. Put the type in lore_type, not as a prefix in the title."

func loreOverCapMsg(field string, got, capChars int) string {
	return field + " is " + strconv.Itoa(got) + " characters, over the " + strconv.Itoa(capChars) +
		"-character limit — nothing was written. It is refused whole rather than truncated."
}

func loreTypeInvalidMsg(loreType string) string {
	return "lore_type must be one of " + loreTypeList + " — got " +
		strconv.Quote(loreType) + "; nothing was written"
}

// loreTextOverCap checks whichever of title and text is non-empty; "" means both fit.
func (s *apiServer) loreTextOverCap(title, text string) string {
	if n, capChars := utf8.RuneCountInString(title), s.loreTitleCap(); title != "" && n > capChars {
		return loreOverCapMsg("title", n, capChars)
	}
	if n, capChars := utf8.RuneCountInString(text), s.loreBodyCap(); text != "" && n > capChars {
		return loreOverCapMsg("body", n, capChars)
	}
	return ""
}

const (
	loreGovernanceRefusalPin = "Pin / unpin is an admin decision — a pinned entry " +
		"sorts ahead of every other entry in its scope and therefore survives the " +
		"cap at the expense of everyone else's, so who pins is not the writer's " +
		"call. Ask the owner or an admin."
	loreEditRefusalPinned = "a pinned entry is edited only by an admin — it sorts ahead of " +
		"every other entry in its scope and survives the cap at the expense of everyone " +
		"else's, so while it is pinned its text is not the writer's call. Ask the owner " +
		"or an admin."
	loreGovernanceRefusalOwn = "you may only Retire, Activate, Bump to latest or Edit an entry you WROTE — " +
		"this one has a different author, and an entry is governed by the member " +
		"who wrote it. An admin or the owner can act on any entry."
)

// 🔴 Does NOT cover pinning: pin has a higher floor (owner:「置頂只有你跟 admin」),
// checked at the state door.
func (s *apiServer) callerMayGovernLore(r *http.Request, e LoreEntry) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return true
	}
	actor := currentActor(r)
	return actor != "" && actor == e.AuthorID
}

// The route table's `Requires` is one minimum per route, so it carries the lower
// (agent) floor; the admin-only pin floor is checked here in the body.
func (s *apiServer) HandleSetLoreEntryStateApiLoreEntryIdStatePost(w http.ResponseWriter, r *http.Request, entryID string) {
	var body LoreEntryStateDTO
	if !decodeJSONBodyStrict(w, r, &body, "state") {
		return
	}
	state := strings.TrimSpace(body.State)
	if !ValidLoreState(state) {
		writeError(w, http.StatusBadRequest,
			"state must be one of "+LoreStateActive+", "+LoreStatePinned+", "+
				LoreStateRetired+" — got "+strconv.Quote(state))
		return
	}
	reason := ""
	if body.RetireReason != nil {
		reason = strings.TrimSpace(*body.RetireReason)
	}
	if state != LoreStateRetired {
		reason = ""
	}

	refusal := func(current *LoreEntry) error {
		// Both directions: otherwise an author could un-pin an owner-pinned entry.
		return s.loreAuthorReachRefusal(r, entryID, current,
			state == LoreStatePinned, loreGovernanceRefusalPin)
	}
	s.writeLoreEntryDecided(w, entryID, refusal, func() (bool, error) {
		return s.dal.SetLoreEntryState(entryID, state, reason, nowSecs())
	})
}

// loreAuthorReachRefusal is the reach the state and edit doors share: admin
// capability anywhere; otherwise the author only, and never on a pinned entry
// (nor into one, when pinning is what is asked).
func (s *apiServer) loreAuthorReachRefusal(r *http.Request, entryID string, current *LoreEntry,
	pinning bool, pinnedRefusal string) error {
	if current == nil {
		return refuseInTx(http.StatusNotFound, "no such lore entry: "+entryID)
	}
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return nil
	}
	if pinning || current.State == LoreStatePinned {
		return refuseInTx(http.StatusForbidden, pinnedRefusal)
	}
	if !s.callerMayGovernLore(r, *current) {
		return refuseInTx(http.StatusForbidden, loreGovernanceRefusalOwn)
	}
	return nil
}

func (s *apiServer) HandleEditLoreEntryApiLoreEntryIdPatch(w http.ResponseWriter, r *http.Request, entryID string) {
	var body LoreEntryEditDTO
	if !decodeJSONBodyStrict(w, r, &body) {
		return
	}
	if body.Title == nil && body.Body == nil && body.LoreType == nil {
		writeError(w, http.StatusBadRequest,
			"send at least one of title, body or lore_type — nothing was changed")
		return
	}
	var title, text, loreType *string
	if body.Title != nil {
		v := strings.TrimSpace(*body.Title)
		if v == "" {
			writeError(w, http.StatusBadRequest,
				"title must not be blank — leave it out to keep the current title; nothing was changed")
			return
		}
		title = &v
	}
	if body.Body != nil {
		v := strings.TrimSpace(*body.Body)
		if v == "" {
			writeError(w, http.StatusBadRequest,
				"body must not be blank — leave it out to keep the current body; nothing was changed")
			return
		}
		text = &v
	}
	if body.LoreType != nil {
		v := strings.TrimSpace(*body.LoreType)
		if !ValidLoreType(v) {
			writeError(w, http.StatusBadRequest, loreTypeInvalidMsg(v))
			return
		}
		loreType = &v
	}

	reach := s.loreEditRefusal(r, entryID)
	titleCap, bodyCap := s.loreTitleCap(), s.loreBodyCap()
	refusal := func(current *LoreEntry) error {
		if err := reach(current); err != nil {
			return err
		}
		if msg := loreEditOverCap(titleCap, bodyCap, current, title, text); msg != "" {
			return refuseInTx(http.StatusBadRequest, msg)
		}
		return nil
	}
	e, ok := s.decideLoreEntryWrite(w, entryID, refusal,
		s.editLoreEntryText(currentActor(r), entryID, title, text, loreType))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, LoreEntryEditReceiptDTO{Id: e.ID, UpdatedTs: e.UpdatedTS})
}

// loreEditOverCap holds an edited title or body to the shrink-only rule the
// capped documents follow (DocCapBlocked): a cap lowered under an entry that is
// already stored must not make that entry uneditable. A field left out or sent
// unchanged is not judged.
func loreEditOverCap(titleCap, bodyCap int, current *LoreEntry, title, body *string) string {
	for _, f := range []struct {
		name, before string
		after        *string
		capChars     int
	}{
		{"title", current.Title, title, titleCap},
		{"body", current.Body, body, bodyCap},
	} {
		if f.after == nil || *f.after == f.before {
			continue
		}
		if DocCapBlocked(f.capChars, f.before, *f.after) {
			return docCapRefusal(f.capChars, "lore entry "+f.name, f.before, *f.after)
		}
	}
	return ""
}

func (s *apiServer) loreEditRefusal(r *http.Request, entryID string) func(*LoreEntry) error {
	return func(current *LoreEntry) error {
		return s.loreAuthorReachRefusal(r, entryID, current, false, loreEditRefusalPinned)
	}
}

const docKindLoreEntry = "lore_entry"

// loreEntrySnapshotIn retains the pre-edit title and body only when the edit
// changes one of them, so a type-only or same-text edit spends no slot.
func loreEntrySnapshotIn(entryID string, title, body *string) func(sqlRowQuerier) (string, error) {
	return func(q sqlRowQuerier) (string, error) {
		current, err := getLoreEntryOn(q, entryID)
		if err != nil || current == nil {
			return "{}", err
		}
		if (title == nil || *title == current.Title) && (body == nil || *body == current.Body) {
			return "{}", nil
		}
		return historyJSON(map[string]string{"title": current.Title, "body": current.Body})
	}
}

func (s *apiServer) editLoreEntryText(actor, entryID string, title, body, loreType *string) func() (bool, error) {
	return func() (bool, error) {
		wrote := false
		err := s.dal.SaveWithDocumentHistories([]documentHistoryStream{{
			Kind: docKindLoreEntry, Key: entryID, ActorID: actor,
			Snapshot: loreEntrySnapshotIn(entryID, title, body),
		}}, func(ex sqlExecer) error {
			var err error
			wrote, err = editLoreEntryTextOn(ex, entryID, title, body, loreType, nowSecs())
			return err
		})
		return wrote, err
	}
}

// restoreLoreEntryText is the edit's own write, held to the edit's caps.
func (s *apiServer) restoreLoreEntryText(r *http.Request, entryID string, content map[string]string) error {
	title, body := strings.TrimSpace(content["title"]), strings.TrimSpace(content["body"])
	if title == "" || body == "" {
		return errNotFound
	}
	reach := s.loreEditRefusal(r, entryID)
	titleCap, bodyCap := s.loreTitleCap(), s.loreBodyCap()
	refusal := func(current *LoreEntry) error {
		if err := reach(current); err != nil {
			return err
		}
		if loreEditOverCap(titleCap, bodyCap, current, &title, &body) != "" {
			return errDocumentHistoryCap
		}
		return nil
	}
	_, err := s.writeLoreEntryInTx(entryID, refusal,
		s.editLoreEntryText(currentActor(r), entryID, &title, &body, nil))
	return err
}

func (s *apiServer) loreEntryRestoreAuthz(w http.ResponseWriter, r *http.Request, entryID string) bool {
	current, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return false
	}
	if err := s.loreEditRefusal(r, entryID)(current); err != nil {
		writeTxError(w, err)
		return false
	}
	return true
}

func (s *apiServer) HandleBumpLoreEntryApiLoreEntryIdBumpPost(w http.ResponseWriter, r *http.Request, entryID string) {
	refusal := func(current *LoreEntry) error {
		if current == nil {
			return refuseInTx(http.StatusNotFound, "no such lore entry: "+entryID)
		}
		if !s.callerMayGovernLore(r, *current) {
			return refuseInTx(http.StatusForbidden, loreGovernanceRefusalOwn)
		}
		return nil
	}
	s.writeLoreEntryDecided(w, entryID, refusal, func() (bool, error) {
		return s.dal.BumpLoreEntryEffective(entryID, nowSecs())
	})
}

func (s *apiServer) writeLoreEntryDecided(w http.ResponseWriter, entryID string,
	refusal func(current *LoreEntry) error, write func() (bool, error)) {
	e, ok := s.decideLoreEntryWrite(w, entryID, refusal, write)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, LoreEntryStateReceiptDTO{
		Id:          e.ID,
		State:       e.State,
		EffectiveTs: e.EffectiveTS,
		UpdatedTs:   e.UpdatedTS,
	})
}

// decideLoreEntryWrite judges the entry on the read pool first, so a refusal
// never waits for the write connection, then judges it again on the row the
// transaction writes and answers the row read in that transaction. On false
// the error has been written.
func (s *apiServer) decideLoreEntryWrite(w http.ResponseWriter, entryID string,
	refusal func(current *LoreEntry) error, write func() (bool, error)) (*LoreEntry, bool) {
	current, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return nil, false
	}
	if err := refusal(current); err != nil {
		writeTxError(w, err)
		return nil, false
	}
	e, err := s.writeLoreEntryInTx(entryID, refusal, write)
	if err != nil {
		writeTxError(w, err)
		return nil, false
	}
	return e, true
}

func (s *apiServer) writeLoreEntryInTx(entryID string,
	refusal func(current *LoreEntry) error, write func() (bool, error)) (*LoreEntry, error) {
	var e *LoreEntry
	err := s.dal.inTx(func(*writeTx) error {
		current, err := s.dal.GetLoreEntry(entryID)
		if err != nil {
			return err
		}
		if err := refusal(current); err != nil {
			return err
		}
		if _, err := write(); err != nil {
			return err
		}
		e, err = s.dal.GetLoreEntry(entryID)
		if err == nil && e == nil {
			err = refuseInTx(http.StatusNotFound, "no such lore entry: "+entryID)
		}
		return err
	})
	return e, err
}

func (s *apiServer) HandleListLoreEntriesApiLoreGet(w http.ResponseWriter, r *http.Request, params HandleListLoreEntriesApiLoreGetParams) {
	// 🔴 When both wire spellings of an axis arrive, the PLURAL wins and the
	// singular is IGNORED — not ANDed, which would silently narrow a multi-value
	// request to the old field's single value.
	kinds, kindsPlural := loreFilterValues(params.ScopeKinds, params.ScopeKind)
	keys, _ := loreFilterValues(params.ScopeKeys, params.ScopeKey)
	states, statesPlural := loreFilterValues(params.States, params.State)
	authors, _ := loreFilterValues(params.AuthorIds, params.AuthorId)
	entryIDs, _ := loreFilterValues(params.EntryIds, params.EntryId)
	loreTypes, _ := loreFilterValues(params.LoreTypes, nil)
	f := loreListFilter{
		ScopeKinds: kinds, ScopeKeys: keys, States: states, AuthorIDs: authors,
		EntryIDs: entryIDs, LoreTypes: loreTypes, TitleQ: trimmedOrEmpty(params.Q),
	}
	// 'role' was collapsed into agent (rc-a43100fd0486) and is refused; the orphan
	// rows migrations/00100 left at scope_kind='role' still come back on any page
	// that does not constrain this axis.
	for _, k := range kinds {
		if !validLoreScopeTarget(k) {
			writeError(w, http.StatusBadRequest,
				loreFilterParamName("scope_kind", kindsPlural)+" must be "+
					loreScopeTargetList+" — got "+strconv.Quote(k))
			return
		}
	}
	for _, st := range states {
		if !ValidLoreState(st) {
			writeError(w, http.StatusBadRequest,
				loreFilterParamName("state", statesPlural)+" must be one of "+
					LoreStateActive+", "+LoreStatePinned+", "+LoreStateRetired+
					" — got "+strconv.Quote(st))
			return
		}
	}
	for _, lt := range loreTypes {
		if !ValidLoreType(lt) {
			writeError(w, http.StatusBadRequest,
				"lore_types must be one of "+loreTypeList+" — got "+strconv.Quote(lt))
			return
		}
	}
	// No 400 for entry_id, scope_key or author_id: they are open identifier
	// spaces, so an empty page is the true answer, not a typo report.

	limit := loreListDefaultLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > loreListMaxLimit {
		writeError(w, http.StatusBadRequest,
			"limit must be between 1 and "+strconv.Itoa(loreListMaxLimit))
		return
	}
	offset := 0
	if params.Offset != nil {
		offset = *params.Offset
	}
	if offset < 0 {
		writeError(w, http.StatusBadRequest, "offset must be 0 or more")
		return
	}

	entries, err := s.dal.ListLoreEntriesPage(f, limit, offset)
	if err != nil {
		internalError(w, err)
		return
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	facts, err := s.dal.LoreScopeFacts(ids)
	if err != nil {
		internalError(w, err)
		return
	}
	out := LoreEntryListDTO{Entries: make([]LoreEntryDTO, 0, len(entries)),
		Limit: limit, Offset: offset, BudgetSegments: []LoreBudgetSegmentDTO{}}
	for _, e := range entries {
		out.Entries = append(out.Entries, newLoreEntryDTO(e, facts[e.ID]))
	}

	// first_dropped_id and each entry's dropped come from the same
	// lore_select.go selection the folds run: the page is cut by limit/offset
	// and the other filters, so neither can be derived from its rows.
	// everyone and agent share one budget and interleave in one list, so an
	// agent page is read off the member's selection: on an agent-only page
	// first_dropped_id is the member's own first drop, on the member's whole
	// list (everyone + agent) the overall one.
	var sel loreSelection
	var line loreSegment
	answered := false
	switch {
	case len(f.ScopeKinds) == 1 && f.ScopeKinds[0] == LoreScopeEveryone && len(f.ScopeKeys) == 0:
		sel, err = selectLoreForScope(s.dal, LoreScopeEveryone, "", s.loreRoleCap())
		line, answered = sel.segment(LoreScopeEveryone), true
	case len(f.ScopeKinds) == 1 && f.ScopeKinds[0] == LoreScopeAgent && len(f.ScopeKeys) == 1:
		sel, err = selectMemberLore(s.dal, f.ScopeKeys[0], s.loreRoleCap())
		line, answered = sel.segment(LoreScopeAgent), true
	case len(f.ScopeKinds) == 2 && slices.Contains(f.ScopeKinds, LoreScopeEveryone) &&
		slices.Contains(f.ScopeKinds, LoreScopeAgent) && len(f.ScopeKeys) == 1:
		sel, err = selectMemberLore(s.dal, f.ScopeKeys[0], s.loreRoleCap())
		line, answered = sel.firstDropped(), true
	case len(f.ScopeKinds) == 1 && f.ScopeKinds[0] == LoreScopeManual && len(f.ScopeKeys) == 1:
		sel, err = selectLoreForScope(s.dal, f.ScopeKinds[0], f.ScopeKeys[0], s.loreManualCap())
		line, answered = sel.segment(LoreScopeManual), true
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if answered {
		out.CapChars = sel.CapChars
		out.UsedChars = sel.UsedChars
		out.FirstDroppedId = line.FirstDroppedID
		out.FirstDroppedChars = line.FirstDroppedChars
		out.BudgetSegments = loreBudgetSegmentDTOs(sel)
		for i := range out.Entries {
			if dropped, walked := sel.Dropped[out.Entries[i].Id]; walked {
				out.Entries[i].Dropped = &dropped
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func loreBudgetSegmentDTOs(sel loreSelection) []LoreBudgetSegmentDTO {
	out := make([]LoreBudgetSegmentDTO, 0, len(sel.Segments))
	for _, seg := range sel.Segments {
		out = append(out, LoreBudgetSegmentDTO{
			ScopeKind:   seg.ScopeKind,
			EntryCount:  seg.EntryCount,
			Chars:       seg.Chars,
			FittedCount: seg.FittedCount,
			FittedChars: seg.FittedChars,
		})
	}
	return out
}

func newLoreBudgetTargetDTO(scopeKind, scopeKey string, sel loreSelection) LoreBudgetTargetDTO {
	return LoreBudgetTargetDTO{
		ScopeKind:    scopeKind,
		ScopeKey:     scopeKey,
		CapChars:     sel.CapChars,
		UsedChars:    sel.UsedChars,
		EntryCount:   sel.entryCount(),
		DroppedCount: sel.droppedCount(),
	}
}

// Targets are every boot document a 傳承 entry can ride: everyone, each member
// on the roster but wardens (which boot no document), and each task manual the
// cockpit lists.
func (s *apiServer) HandleListLoreBudgetsApiLoreBudgetsGet(w http.ResponseWriter, r *http.Request) {
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	manuals, err := s.foldTaskManuals()
	if err != nil {
		internalError(w, err)
		return
	}
	roleCap, manualCap := s.loreRoleCap(), s.loreManualCap()

	out := LoreBudgetListDTO{Targets: []LoreBudgetTargetDTO{}}
	sel, err := selectLoreForScope(s.dal, LoreScopeEveryone, "", roleCap)
	if err != nil {
		internalError(w, err)
		return
	}
	out.Targets = append(out.Targets, newLoreBudgetTargetDTO(LoreScopeEveryone, "", sel))
	for _, m := range members {
		if m.RosterStatus == RosterStatusRemoved || m.Kind == KindWarden {
			continue
		}
		sel, err := selectMemberLore(s.dal, m.ID, roleCap)
		if err != nil {
			internalError(w, err)
			return
		}
		out.Targets = append(out.Targets, newLoreBudgetTargetDTO(LoreScopeAgent, m.ID, sel))
	}
	for _, tm := range manuals {
		sel, err := selectLoreForScope(s.dal, LoreScopeManual, tm.TypeKey, manualCap)
		if err != nil {
			internalError(w, err)
			return
		}
		out.Targets = append(out.Targets, newLoreBudgetTargetDTO(LoreScopeManual, tm.TypeKey, sel))
	}
	writeJSON(w, http.StatusOK, out)
}

const loreScopeTargetList = LoreScopeAgent + ", " + LoreScopeManual + " or " + LoreScopeEveryone

func validLoreScopeTarget(k string) bool {
	switch k {
	case LoreScopeAgent, LoreScopeManual, LoreScopeEveryone:
		return true
	}
	return false
}

func loreScopeOptions(f loreScopeFacts) []string {
	opts := make([]string, 0, 3)
	if f.TaskTypeKey != "" {
		opts = append(opts, LoreScopeManual)
	}
	if f.AuthorOnRoster {
		opts = append(opts, LoreScopeAgent)
	}
	return append(opts, LoreScopeEveryone)
}

// The admin floor is the route's (routes.go); nothing below re-checks it.
func (s *apiServer) HandleSetLoreEntryScopeApiLoreEntryIdScopePost(w http.ResponseWriter, r *http.Request, entryID string) {
	var body LoreEntryScopeDTO
	if !decodeJSONBodyStrict(w, r, &body, "scope_kind") {
		return
	}
	kind := strings.TrimSpace(body.ScopeKind)
	if !validLoreScopeTarget(kind) {
		writeError(w, http.StatusBadRequest,
			"scope_kind must be "+loreScopeTargetList+" — got "+strconv.Quote(kind))
		return
	}
	if _, err := s.loreScopeKey(entryID, kind); err != nil {
		writeTxError(w, err)
		return
	}
	var e *LoreEntry
	err := s.dal.inTx(func(*writeTx) error {
		key, err := s.loreScopeKey(entryID, kind)
		if err != nil {
			return err
		}
		if _, err := s.dal.SetLoreEntryScope(entryID, kind, key, nowSecs()); err != nil {
			return err
		}
		e, err = s.dal.GetLoreEntry(entryID)
		if err == nil && e == nil {
			err = refuseInTx(http.StatusNotFound, "no such lore entry: "+entryID)
		}
		return err
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, LoreEntryScopeReceiptDTO{
		Id:          e.ID,
		ScopeKind:   e.ScopeKind,
		ScopeKey:    e.ScopeKey,
		State:       e.State,
		EffectiveTs: e.EffectiveTS,
		UpdatedTs:   e.UpdatedTS,
	})
}

// loreScopeKey is the scope_key a move to kind files the entry under, or the
// refusal for it.
func (s *apiServer) loreScopeKey(entryID, kind string) (string, error) {
	current, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		return "", err
	}
	if current == nil {
		return "", refuseInTx(http.StatusNotFound, "no such lore entry: "+entryID)
	}
	allFacts, err := s.dal.LoreScopeFacts([]string{entryID})
	if err != nil {
		return "", err
	}
	facts := allFacts[entryID]
	switch kind {
	case LoreScopeAgent:
		if !facts.AuthorOnRoster {
			return "", refuseInTx(http.StatusBadRequest,
				"lore entry "+entryID+" has no author on the roster (author_id "+
					strconv.Quote(current.AuthorID)+"), so an agent scope would ride no "+
					"boot document; choose everyone or, if it has a task type, manual")
		}
		return current.AuthorID, nil
	case LoreScopeManual:
		if facts.TaskTypeKey == "" {
			return "", refuseInTx(http.StatusBadRequest,
				"lore entry "+entryID+" has no task type to key a manual scope to — its "+
					"source task carries no type, or it has no source task and its author "+
					"is not an outsource member bound to a typed task")
		}
		return facts.TaskTypeKey, nil
	}
	return "", nil
}

func loreFilterValues(plural *[]string, single *string) (vals []string, fromPlural bool) {
	if plural != nil {
		for _, v := range *plural {
			if v = strings.TrimSpace(v); v != "" {
				vals = append(vals, v)
			}
		}
	}
	if len(vals) > 0 {
		return vals, true
	}
	if single != nil {
		if v := strings.TrimSpace(*single); v != "" {
			return []string{v}, false
		}
	}
	return nil, false
}

func loreFilterParamName(singular string, fromPlural bool) string {
	if fromPlural {
		return singular + "s"
	}
	return singular
}

func newLoreEntryDTO(e LoreEntry, facts loreScopeFacts) LoreEntryDTO {
	taskTypeKey := facts.TaskTypeKey
	options := loreScopeOptions(facts)
	loreType := e.LoreType
	return LoreEntryDTO{
		Id:           e.ID,
		Seq:          e.Seq,
		ScopeKind:    e.ScopeKind,
		ScopeKey:     e.ScopeKey,
		Title:        e.Title,
		Body:         e.Body,
		AuthorId:     e.AuthorID,
		SourceTaskId: e.SourceTaskID,
		State:        e.State,
		RetireReason: e.RetireReason,
		EffectiveTs:  e.EffectiveTS,
		CreatedTs:    e.CreatedTS,
		UpdatedTs:    e.UpdatedTS,
		LoreType:     &loreType,
		TaskTypeKey:  &taskTypeKey,
		ScopeOptions: &options,
	}
}
