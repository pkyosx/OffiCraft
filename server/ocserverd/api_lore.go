package main

// 傳承（lore）write/read faces, which are also the MCP tools (the tool surface IS
// the route table; see mcp.go). Writing is MCP-only by design: an entry is
// written by the agent that just learned the thing, and the cockpit gets only
// the list plus the governance verbs. Selection lives in lore_select.go; a read
// face here must call it, never its own ORDER BY.

import (
	"net/http"
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

	if n, capChars := utf8.RuneCountInString(title), s.loreTitleCap(); n > capChars {
		writeError(w, http.StatusBadRequest, loreOverCapMsg("title", n, capChars))
		return
	}
	if n, capChars := utf8.RuneCountInString(text), s.loreBodyCap(); n > capChars {
		writeError(w, http.StatusBadRequest, loreOverCapMsg("body", n, capChars))
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
				"this request carries no verified member identity, so there is no 開機檔 "+
					"to file a 傳承 entry under.")
			return
		case m == nil:
			writeError(w, http.StatusBadRequest,
				"you have no roster row, so there is no 開機檔 of your own to write into. "+
					"A 傳承 entry is filed under the writer's own boot document or under a "+
					"typed task's manual; pass a typed task's task_id to write 任務傳承 instead.")
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
		dto.ScopeNote = "任務 " + taskID + " 沒有類型，所以這一筆寫進了你自己的開機檔（" +
			scopeKind + " / " + scopeKey + "），不是任何一本任務手冊。"
	}
	writeJSON(w, http.StatusOK, dto)
}

func loreOverCapMsg(field string, got, capChars int) string {
	return field + " is " + strconv.Itoa(got) + " characters, over the " + strconv.Itoa(capChars) +
		"-character limit — nothing was written. A 傳承 entry cannot be edited " +
		"after it is written, so it is refused whole rather than truncated."
}

const (
	loreGovernanceRefusalPin = "置頂／取消置頂 is an admin decision — a pinned entry " +
		"sorts ahead of every other entry in its scope and therefore survives the " +
		"cap at the expense of everyone else's, so who pins is not the writer's " +
		"call. Ask the owner or an admin agent."
	loreGovernanceRefusalOwn = "you may only 失效, 生效 or 提到最新 an entry you WROTE — " +
		"this one has a different author, and an entry is governed by the member " +
		"who wrote it. An admin agent or the owner can act on any entry."
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

	current, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "no such lore entry: "+entryID)
		return
	}

	if !principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		// Both directions: otherwise an author could un-pin an owner-pinned entry.
		if state == LoreStatePinned || current.State == LoreStatePinned {
			writeError(w, http.StatusForbidden, loreGovernanceRefusalPin)
			return
		}
		if !s.callerMayGovernLore(r, *current) {
			writeError(w, http.StatusForbidden, loreGovernanceRefusalOwn)
			return
		}
	}

	reason := ""
	if body.RetireReason != nil {
		reason = strings.TrimSpace(*body.RetireReason)
	}
	if state != LoreStateRetired {
		reason = ""
	}

	ok, err := s.dal.SetLoreEntryState(entryID, state, reason, nowSecs())
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no such lore entry: "+entryID)
		return
	}
	s.writeLoreEntryByID(w, entryID)
}

func (s *apiServer) HandleBumpLoreEntryApiLoreEntryIdBumpPost(w http.ResponseWriter, r *http.Request, entryID string) {
	current, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "no such lore entry: "+entryID)
		return
	}
	if !s.callerMayGovernLore(r, *current) {
		writeError(w, http.StatusForbidden, loreGovernanceRefusalOwn)
		return
	}

	ok, err := s.dal.BumpLoreEntryEffective(entryID, nowSecs())
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no such lore entry: "+entryID)
		return
	}
	s.writeLoreEntryByID(w, entryID)
}

func (s *apiServer) writeLoreEntryByID(w http.ResponseWriter, entryID string) {
	e, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "no such lore entry: "+entryID)
		return
	}
	writeJSON(w, http.StatusOK, LoreEntryStateReceiptDTO{
		Id:          e.ID,
		State:       e.State,
		EffectiveTs: e.EffectiveTS,
		UpdatedTs:   e.UpdatedTS,
	})
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
	f := loreListFilter{
		ScopeKinds: kinds, ScopeKeys: keys, States: states, AuthorIDs: authors,
		EntryIDs: entryIDs,
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
		Limit: limit, Offset: offset}
	for _, e := range entries {
		out.Entries = append(out.Entries, newLoreEntryDTO(e, facts[e.ID]))
	}

	// 上限線 (the first entry the fold will NOT carry; the cockpit draws a line
	// above it) comes from the same lore_select.go selection the folds run: the
	// page is cut by limit/offset, so the line cannot be derived from its rows.
	// everyone and agent share one budget (T-236), so an agent line is read off
	// the member's selection.
	var sel loreSelection
	answered := false
	switch {
	case len(f.ScopeKinds) == 1 && f.ScopeKinds[0] == LoreScopeEveryone && len(f.ScopeKeys) == 0:
		sel, err = selectLoreForScope(s.dal, LoreScopeEveryone, "", s.loreRoleCap())
		answered = true
	case len(f.ScopeKinds) == 1 && f.ScopeKinds[0] == LoreScopeAgent && len(f.ScopeKeys) == 1:
		sel, err = selectMemberLore(s.dal, f.ScopeKeys[0], s.loreRoleCap())
		answered = true
	case len(f.ScopeKinds) == 1 && f.ScopeKinds[0] == LoreScopeManual && len(f.ScopeKeys) == 1:
		sel, err = selectLoreForScope(s.dal, f.ScopeKinds[0], f.ScopeKeys[0], s.loreManualCap())
		answered = true
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if answered {
		out.CapChars = sel.CapChars
		out.FirstDroppedId = sel.FirstDroppedByKind[f.ScopeKinds[0]]
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
	current, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "no such lore entry: "+entryID)
		return
	}

	allFacts, err := s.dal.LoreScopeFacts([]string{entryID})
	if err != nil {
		internalError(w, err)
		return
	}
	facts := allFacts[entryID]
	key := ""
	switch kind {
	case LoreScopeAgent:
		if !facts.AuthorOnRoster {
			writeError(w, http.StatusBadRequest,
				"lore entry "+entryID+" has no author on the roster (author_id "+
					strconv.Quote(current.AuthorID)+"), so an agent scope would ride no "+
					"boot document; choose everyone or, if it has a task type, manual")
			return
		}
		key = current.AuthorID
	case LoreScopeManual:
		key = facts.TaskTypeKey
		if key == "" {
			writeError(w, http.StatusBadRequest,
				"lore entry "+entryID+" has no task type to key a manual scope to — its "+
					"source task carries no type, or it has no source task and its author "+
					"is not an outsource member bound to a typed task")
			return
		}
	}

	if _, err := s.dal.SetLoreEntryScope(entryID, kind, key, nowSecs()); err != nil {
		internalError(w, err)
		return
	}
	e, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "no such lore entry: "+entryID)
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
		TaskTypeKey:  &taskTypeKey,
		ScopeOptions: &options,
	}
}
