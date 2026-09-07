package main

// api_lore.go — T-33 傳承（lore）: the four write/read faces, which are also the
// four MCP tools (the tool surface IS the route table; see mcp.go).
//
// WRITING IS MCP-ONLY BY DESIGN. There is no cockpit compose form and this file
// builds none: an entry is written by the agent that just learned the thing, in
// the moment, out of the work — not typed into a box afterwards by somebody
// reconstructing it. What the cockpit gets is the LIST plus the three
// state-moving verbs, which is the governance half.
//
// 🔴 THE SELECTION RULE IS NOT IN THIS FILE. Reading lore for a reader is
// selectLoreForScope (lore_select.go), called by the two folds. Nothing here
// re-implements it, and a future read face must call it rather than write its
// own ORDER BY.

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// loreListDefaultLimit / loreListMaxLimit bound one page of the list face.
// The default matches the cockpit's 捲到底載 30 筆; the ceiling exists so a
// caller cannot ask for the whole table in one answer and hand an agent a
// payload it has no budget for.
const (
	loreListDefaultLimit = 30
	loreListMaxLimit     = 200
)

// callerLoreRoleKey resolves the role a caller's own lore would be filed under:
// the role_key on the caller's roster row, read by the VERIFIED token subject
// and never from a client field (root CLAUDE.md §14).
//
// It returns "" when the caller has no role to file under, which is a real and
// ordinary state — an outsource worker has none by construction, and the owner
// has no roster row at all. "" is a REFUSAL, never a licence to pick a role.
func (s *apiServer) callerLoreRoleKey(r *http.Request) (string, error) {
	actor := currentActor(r)
	if actor == "" {
		return "", nil
	}
	m, err := s.dal.GetMember(actor)
	if err != nil || m == nil {
		return "", err
	}
	return m.RoleKey, nil
}

// POST /api/lore — write_lore_entry.
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

	// 🔴 THE CAPS ARE CHECKED BEFORE ANY SCOPE IS RESOLVED AND BEFORE ANY WRITE.
	// Spec §5: over-cap writes NOTHING. Truncating instead would store a sentence
	// that stops, and the reader downstream cannot tell a cut lesson from a
	// finished one.
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

	scopeKind, scopeKey := "", ""
	if taskID == "" {
		// ── the ROLE arm ────────────────────────────────────────────────────
		roleKey, err := s.callerLoreRoleKey(r)
		if err != nil {
			internalError(w, err)
			return
		}
		if roleKey == "" {
			writeError(w, http.StatusBadRequest,
				"you have no role, so there is no 角色傳承 to write into. "+
					"A 傳承 entry is filed either under the writer's role or under a "+
					"task's type; pass task_id to write into that task type's 任務傳承 instead.")
			return
		}
		scopeKind, scopeKey = LoreScopeRole, roleKey
	} else {
		// ── the MANUAL arm ──────────────────────────────────────────────────
		t, err := s.dal.GetTask(taskID)
		if err != nil {
			internalError(w, err)
			return
		}
		if t == nil {
			writeError(w, http.StatusBadRequest, "no such task: "+taskID)
			return
		}
		if strings.TrimSpace(t.TypeKey) == "" {
			// 🔴 THIS DOES NOT FALL BACK TO THE ROLE ARM, and the refusal is the
			// whole point of the branch. A 臨時任務 carries no type, so there is no
			// manual for its lesson to belong to. Filing it under the writer's role
			// instead would charge every future boot of that role for a lesson about
			// a one-off piece of work, while the task types that genuinely needed
			// such a lesson still received nothing — and the write would answer 200,
			// so nobody would ever look. The boot document already tells agents that
			// a 臨時任務 has no place to write to; this is that sentence enforced.
			writeError(w, http.StatusBadRequest,
				"這張任務沒有類型，沒有可寫的位置 — task "+taskID+" carries no type_key "+
					"(a 臨時任務), so there is no 任務傳承 to file this under. It is NOT "+
					"filed under your role instead. Write it against a typed task, or "+
					"omit task_id to write 角色傳承 deliberately.")
			return
		}
		scopeKind, scopeKey = LoreScopeManual, strings.TrimSpace(t.TypeKey)
	}

	now := nowSecs()
	entry, err := s.dal.CreateLoreEntryMintingID(LoreEntry{
		ScopeKind: scopeKind,
		ScopeKey:  scopeKey,
		Title:     title,
		Body:      text,
		// PINNED AT WRITE TIME (spec §1) — the roster is re-read for display,
		// never for authorship.
		AuthorID:     currentActor(r),
		SourceTaskID: taskID,
		State:        LoreStateActive,
		// All three timestamps start equal. effective_ts is the only one a later
		// 提到最新 moves; created_ts is what makes that reversible.
		EffectiveTS: now,
		CreatedTS:   now,
		UpdatedTS:   now,
	})
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newLoreEntryDTO(entry))
}

// loreOverCapMsg names the field, what was sent and what is allowed. All three,
// because "too long" alone leaves the caller to guess by bisection, and the cap
// is a setting it cannot read without admin capability.
func loreOverCapMsg(field string, got, capChars int) string {
	return field + " is " + strconv.Itoa(got) + " characters, over the " + strconv.Itoa(capChars) +
		"-character limit — nothing was written. A 傳承 entry cannot be edited " +
		"after it is written, so it is refused whole rather than truncated."
}

// POST /api/lore/{entry_id}/state — set_lore_entry_state.
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
	// 🔴 A reason is stored ONLY with 'retired', and the two non-retired states
	// CLEAR whatever was there. An entry brought back to active while still
	// carrying "superseded by L-9" would be rendered beside that sentence, which
	// is a claim nobody made.
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

// POST /api/lore/{entry_id}/bump — bump_lore_entry (提到最新).
func (s *apiServer) HandleBumpLoreEntryApiLoreEntryIdBumpPost(w http.ResponseWriter, r *http.Request, entryID string) {
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

// writeLoreEntryByID re-reads and answers with the STORED row rather than with
// a copy the handler assembled from what it just sent. The two faces cannot
// then disagree about what actually landed — which is the only way a caller can
// verify a write it did not watch.
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
	writeJSON(w, http.StatusOK, newLoreEntryDTO(*e))
}

// GET /api/lore — list_lore_entries.
func (s *apiServer) HandleListLoreEntriesApiLoreGet(w http.ResponseWriter, r *http.Request, params HandleListLoreEntriesApiLoreGetParams) {
	f := loreListFilter{}
	if params.ScopeKind != nil {
		f.ScopeKind = strings.TrimSpace(*params.ScopeKind)
	}
	if params.ScopeKey != nil {
		f.ScopeKey = strings.TrimSpace(*params.ScopeKey)
	}
	if params.State != nil {
		f.State = strings.TrimSpace(*params.State)
	}
	if params.AuthorId != nil {
		f.AuthorID = strings.TrimSpace(*params.AuthorId)
	}
	// A filter value outside its closed set is a 400 rather than a silently
	// empty page: "no entries match role_kind=roles" and "there are none" look
	// identical on the wire, and the caller would read the typo as an answer.
	if f.ScopeKind != "" && f.ScopeKind != LoreScopeRole && f.ScopeKind != LoreScopeManual {
		writeError(w, http.StatusBadRequest,
			"scope_kind must be "+LoreScopeRole+" or "+LoreScopeManual+
				" — got "+strconv.Quote(f.ScopeKind))
		return
	}
	if f.State != "" && !ValidLoreState(f.State) {
		writeError(w, http.StatusBadRequest,
			"state must be one of "+LoreStateActive+", "+LoreStatePinned+", "+
				LoreStateRetired+" — got "+strconv.Quote(f.State))
		return
	}

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

	// 🔴 THE FILTER TRAVELS WITH THE PAGE, into the same query. Paging first and
	// filtering in the client makes 「捲到底沒有了」 and 「真的沒有了」 the same
	// picture, and makes the cockpit's 上限線 fall in the wrong place.
	entries, err := s.dal.ListLoreEntriesPage(f, limit, offset)
	if err != nil {
		internalError(w, err)
		return
	}
	out := LoreEntryListDTO{Entries: make([]LoreEntryDTO, 0, len(entries)),
		Limit: limit, Offset: offset}
	for _, e := range entries {
		out.Entries = append(out.Entries, newLoreEntryDTO(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// newLoreEntryDTO is the ONE row→wire projection, so every face answers with
// the same shape from the same fields.
func newLoreEntryDTO(e LoreEntry) LoreEntryDTO {
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
	}
}
