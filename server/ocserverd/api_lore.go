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

// callerRosterRow reads the caller's OWN roster row, resolved from the VERIFIED
// token subject and never from a client field (root CLAUDE.md §14).
//
// nil is a real and ordinary answer, not an error: the owner has no roster row
// at all. It is distinct from a row whose RoleKey is "", which is what an
// outsource member has by construction — and the write face treats those two as
// different outcomes, so this returns the row rather than a role key. Answering
// with the key alone collapsed them into one "" and was why an outsource member
// used to be refused with a sentence about roles.
func (s *apiServer) callerRosterRow(r *http.Request) (*Member, error) {
	actor := currentActor(r)
	if actor == "" {
		return nil, nil
	}
	return s.dal.GetMember(actor)
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

	// 🔴 ONE QUESTION DECIDES THE SCOPE, and it is asked here: what is the
	// EFFECTIVE RELATED TASK? The named task when it carries a type; NULL
	// otherwise — and "otherwise" covers BOTH naming no task and naming a
	// 臨時任務, because a task with no type is not a place an entry can hang.
	// The owner corrected this file's earlier rule to exactly that on 2026-09-07
	// (「臨時任務跟無關乎任何任務一樣都是給 NULL」); see LoreScope* in domain.go.
	//
	// untypedTask records that the second door was reached THROUGH a named task
	// rather than by naming none. Nothing about the write changes — it is what
	// lets the response say so, below.
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
		// ── the MANUAL arm ──────────────────────────────────────────────────
		scopeKind, scopeKey = LoreScopeManual, typeKey
	} else {
		// ── the WRITER'S OWN BOOT DOCUMENT arm ──────────────────────────────
		// 🔴 WHY THERE IS ONLY ONE SCOPE HERE NOW. This used to fork: staff filed
		// under their ROLE (LoreScopeRole, keyed by role_key), outsource members
		// under THEMSELVES. Owner collapsed it on 2026-09-07 (card
		// rc-a43100fd0486 [0]) — 「只有成員跟任務傳承兩種」 — so both file under
		// the writer's own member id and the role scope no longer exists.
		//
		// It is the same set of readers, not a widening: staff are one-to-one
		// with their role in the roster as it stands (owner c-712174eb0720), so
		// the role key and the member id named the same one boot document. What
		// the collapse buys is that the column now holds ONE kind of thing — a
		// member id — where before it held a role key on some rows and a member
		// id on others with no field saying which.
		//
		// ⚠️ AND WHAT IT COSTS, stated so nobody re-derives it as a bug: two
		// members under one role would no longer share a 傳承. Nothing enforces
		// one-member-per-role (member.role_key carries no UNIQUE index and the
		// hire face does not check), so that is a fact about today's roster, not
		// a guarantee. The owner was told this in writing before choosing.
		actor := currentActor(r)
		m, err := s.callerRosterRow(r)
		if err != nil {
			internalError(w, err)
			return
		}
		// 🔴 THE TWO REFUSALS ARE ORDERED BY WHAT THE CALLER CAN DO ABOUT THEM,
		// and they are kept apart even though one scope now serves every writer.
		// callerRosterRow returns (nil, nil) for BOTH "no verified identity" and
		// "verified, but no such roster row", so a single nil test would answer
		// one sentence to two different problems — and the one the owner hits
		// (he has a token and no roster row) would read as if his token were bad.
		// Testing `actor` first is what keeps them distinguishable.
		switch {
		case actor == "":
			writeError(w, http.StatusBadRequest,
				"this request carries no verified member identity, so there is no 開機檔 "+
					"to file a 傳承 entry under.")
			return
		case m == nil:
			// The owner has no roster row at all. Nothing to file under, and
			// inventing one would be a scope nobody reads.
			writeError(w, http.StatusBadRequest,
				"you have no roster row, so there is no 開機檔 of your own to write into. "+
					"A 傳承 entry is filed under the writer's own boot document or under a "+
					"typed task's manual; pass a typed task's task_id to write 任務傳承 instead.")
			return
		default:
			// 🔴 m.ID, NOT m.RoleKey, AND NOT `actor`. Not RoleKey because the
			// role scope is gone (see domain.go) — staff and outsource file the
			// same way now, which is why this arm has no branch left in it. And
			// m.ID rather than the token subject because m is the row we actually
			// resolved: the two are equal by construction today (callerRosterRow
			// looks the row up BY the subject), and writing the resolved row's own
			// id means a future change to that lookup cannot quietly start filing
			// entries under a key no roster row carries.
			scopeKind, scopeKey = LoreScopeAgent, m.ID
		}
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
	// 🔴 A BOUNDED RECEIPT, NOT THE ENTRY. The title and the body are what this
	// caller just sent, and for an agent the second copy lands in its context
	// window (owner 2026-09-07: 「不要回傳自己寫出去的 payload」). What comes back
	// is only what the server decided: the minted id and seq, WHERE it was filed,
	// and the stamp. list_lore_entries serves the entry itself.
	dto := LoreEntryWriteReceiptDTO{
		Id:        entry.ID,
		Seq:       entry.Seq,
		ScopeKind: entry.ScopeKind,
		ScopeKey:  entry.ScopeKey,
		CreatedTs: entry.CreatedTS,
	}
	// Design §5, owner-approved: SAY where it went, but only in the one case the
	// writer could not have predicted. It named a task, that task carries no
	// type, so the effective related task was NULL and this landed in the
	// writer's own boot document. Every other write already reads its own answer
	// off scope_kind / scope_key.
	if untypedTask {
		dto.ScopeNote = "任務 " + taskID + " 沒有類型，所以這一筆寫進了你自己的開機檔（" +
			scopeKind + " / " + scopeKey + "），不是任何一本任務手冊。"
	}
	writeJSON(w, http.StatusOK, dto)
}

// loreOverCapMsg names the field, what was sent and what is allowed. All three,
// because "too long" alone leaves the caller to guess by bisection, and the cap
// is a setting it cannot read without admin capability.
func loreOverCapMsg(field string, got, capChars int) string {
	return field + " is " + strconv.Itoa(got) + " characters, over the " + strconv.Itoa(capChars) +
		"-character limit — nothing was written. A 傳承 entry cannot be edited " +
		"after it is written, so it is refused whole rather than truncated."
}

// loreGovernanceRefusalPin / loreGovernanceRefusalOwn are the two refusals the
// governance verbs answer. They are constants so the message a caller reads is
// the same wherever the check is made, and so a test can pin the DISTINCTION
// rather than a substring that happens to appear in both.
const (
	loreGovernanceRefusalPin = "置頂／取消置頂 is an admin decision — a pinned entry " +
		"sorts ahead of every other entry in its scope and therefore survives the " +
		"cap at the expense of everyone else's, so who pins is not the writer's " +
		"call. Ask the owner or an admin agent."
	// ⚠️ THIS MESSAGE USED TO QUOTE THE GLOBAL CONTEXT VERBATIM 「只寫你自己那
	// 一份，也只處置你自己寫的那幾筆。」 so a refused caller could go and read the
	// rule. The owner removed that sentence from the document (2026-09-07), and
	// a quotation of a sentence that no longer exists is worse than no
	// quotation: it sends the reader looking for something they will not find,
	// and nothing tells them the pointer is stale. So the message states the
	// rule itself rather than citing a place.
	loreGovernanceRefusalOwn = "you may only 失效 or 提到最新 an entry you WROTE — " +
		"this one has a different author, and an entry is governed by the member " +
		"who wrote it. An admin agent or the owner can act on any entry."
)

// callerMayGovernLore is the ONE predicate behind both governance verbs, and it
// is written once for the same reason the selector is: retire and bump ask the
// SAME question of the SAME caller about the SAME row, so two copies could only
// ever drift into one of them being wider than the owner's ruling.
//
// Admin capability (an admin agent, or the owner) is unrestricted. Everyone
// else may act only on an entry they are the recorded author of — and author_id
// is the value PINNED at write time, so the answer does not change when the
// roster does.
//
// 🔴 IT DOES NOT COVER PINNING. Pinning has a HIGHER floor than "your own"
// (owner: 「置頂只有你跟 admin」), so it is a separate check at the one door
// that can perform it — folding it in here would make it look like a writer
// could pin their own entry, which is exactly what was ruled out.
func (s *apiServer) callerMayGovernLore(r *http.Request, e LoreEntry) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return true
	}
	actor := currentActor(r)
	return actor != "" && actor == e.AuthorID
}

// POST /api/lore/{entry_id}/state — set_lore_entry_state.
//
// 🔴 WHY THE TWO FLOORS ARE ENFORCED IN THE BODY AND NOT ON THE ROUTE. This one
// door performs three transitions with two different floors: 置頂 and its undo
// are admin-only, while 失效 and 生效 are open to the entry's own author. The
// route table's `Requires` is a single minimum for the whole row, so it carries
// the LOWER of the two (agent) and the higher one is checked here, against the
// body and against the row being moved. Splitting pinning onto its own route
// was the alternative; it would have put one state machine behind two doors
// that could then disagree about the transitions.
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

	// The row is read BEFORE the decision, because both floors are properties of
	// the row: who wrote it, and whether this move pins or un-pins it.
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
		// 🔴 BOTH DIRECTIONS. Moving an entry INTO pinned and moving a pinned
		// entry OUT are the same decision seen from two sides: if un-pinning were
		// open to the author, any writer could demote an entry the owner pinned,
		// and the admin-only floor on pinning would buy nothing.
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
//
// Author-only at the agent floor, admin unrestricted — the same predicate the
// state door uses. 提到最新 moves an entry ahead of other people's entries under
// a shared cap, so it is a claim on somebody else's room; making it open to any
// agent would let one caller quietly push everyone else's lessons out of every
// boot of a role it does not even hold.
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

// writeLoreEntryByID re-reads and answers with a bounded receipt built from the
// STORED row rather than from a copy the handler assembled out of what it just
// sent. The re-read is the point: the two faces cannot then disagree about what
// actually landed, which is the only way a caller can verify a write it did not
// watch.
//
// 🔴 THE RECEIPT CARRIES THE MUTABLE HALF ONLY. Both governance verbs answer
// through here, and neither can change the title, the body, the author or the
// scope — echoing those back would hand the caller a payload it never sent, on
// a call whose whole content is one state word (owner 2026-09-07). The four
// fields are what these two doors actually move; list_lore_entries serves the
// rest.
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

// GET /api/lore — list_lore_entries.
func (s *apiServer) HandleListLoreEntriesApiLoreGet(w http.ResponseWriter, r *http.Request, params HandleListLoreEntriesApiLoreGetParams) {
	// 🔴 EACH AXIS HAS TWO WIRE SPELLINGS AND THE PLURAL WINS. The singular
	// params are frozen wire and stay; the plural ones are what the cockpit's
	// multi-select filters send (owner rc-0376bf875757 [1]). When BOTH arrive for
	// one axis the plural set is the filter and the singular value is IGNORED —
	// not intersected with it, not added to it. A client sending both is one
	// mid-migration saying the same thing twice, and only the plural can carry
	// what it means; ANDing them instead would silently narrow a two-value
	// request down to whichever single value the old field still held.
	kinds, kindsPlural := loreFilterValues(params.ScopeKinds, params.ScopeKind)
	keys, _ := loreFilterValues(params.ScopeKeys, params.ScopeKey)
	states, statesPlural := loreFilterValues(params.States, params.State)
	authors, _ := loreFilterValues(params.AuthorIds, params.AuthorId)
	f := loreListFilter{
		ScopeKinds: kinds, ScopeKeys: keys, States: states, AuthorIDs: authors,
	}
	// A filter value outside its closed set is a 400 rather than a silently
	// empty page: "no entries match role_kind=roles" and "there are none" look
	// identical on the wire, and the caller would read the typo as an answer.
	//
	// 🔴 THAT HOLDS PER ELEMENT OF THE SET, not just for the first one. Dropping
	// one bad element out of three and answering 200 would narrow the page by an
	// axis the caller never asked to narrow by, and nothing on the wire would
	// say so — which is the same failure as the singular case, only harder to
	// notice because some rows still come back. The message names the offending
	// VALUE and the parameter that actually carried it.
	// 🔴 `role` IS NOT ON THIS LIST ANY MORE, AND ASKING FOR IT IS NOW A 400.
	// The scope was collapsed into `agent` (owner 2026-09-07, rc-a43100fd0486
	// [0]); a caller still sending it is holding a vocabulary the server no
	// longer has, and answering 200-with-no-rows would tell them their entries
	// were gone rather than that their filter was. Note what this does NOT do:
	// it does not hide the orphan rows migrations/00100 deliberately left at
	// scope_kind='role'. Those still come back on any page that does not
	// constrain this axis — which is the page the cockpit opens on — so an
	// orphan stays findable even though it can no longer be filtered FOR.
	for _, k := range kinds {
		if k != LoreScopeAgent && k != LoreScopeManual {
			writeError(w, http.StatusBadRequest,
				loreFilterParamName("scope_kind", kindsPlural)+" must be "+
					LoreScopeAgent+" or "+LoreScopeManual+" — got "+strconv.Quote(k))
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

	// 上限線: which entry is the first one the fold will NOT carry. The cockpit
	// draws a line above it and greys everything below.
	//
	// 🔴 THE ANSWER COMES FROM selectLoreForScope — the same function both folds
	// run — and NOT from a rule restated here or in the client. Two reasons, and
	// the second is the one that bites:
	//   1. It is not derivable from this page. The page is cut by limit/offset
	//      long before the budget is spent, so a client adding up the rows it can
	//      see would draw the line in the wrong place on every page but the first.
	//   2. A second copy of the picking rule drifts silently. Whoever later
	//      changes the sort, the exclusion of retired, or the tie-break would fix
	//      the fold and leave the line pointing at a different entry — and a line
	//      in the wrong place looks exactly like a line in the right place.
	// It is answered only when the filter converged on ONE scope: a budget belongs
	// to a scope, so a page spanning several has no single one to report, and 0/""
	// says that honestly instead of naming an arbitrary one.
	//
	// 🔴 「ONE SCOPE」 IS EXACTLY-ONE-OF-EACH, and the multi-select filters are why
	// that has to be said with a length and not with a non-empty test. A page
	// asked for `scope_kinds=role&scope_kinds=manual` spans two scopes and has
	// two different budgets behind it (role and manual are separate settings),
	// so there is no single cap_chars it could report and no single entry that
	// is 「the first one dropped」. Two or more on EITHER axis ⇒ 0 / "", the same
	// answer an unfiltered page gets, for the same reason.
	if len(f.ScopeKinds) == 1 && len(f.ScopeKeys) == 1 {
		scopeKind, scopeKey := f.ScopeKinds[0], f.ScopeKeys[0]
		capChars := s.loreRoleCap()
		if scopeKind == LoreScopeManual {
			capChars = s.loreManualCap()
		}
		sel, err := selectLoreForScope(s.dal, scopeKind, scopeKey, capChars)
		if err != nil {
			internalError(w, err)
			return
		}
		out.CapChars = capChars
		out.FirstDroppedId = sel.FirstDroppedID
	}
	writeJSON(w, http.StatusOK, out)
}

// loreFilterValues folds ONE axis's two wire spellings — the repeatable plural
// and the frozen singular — into the single set the query is built from, and
// reports which spelling the answer came from so a refusal can name the
// parameter the caller actually sent.
//
// 🔴 THE PLURAL WINS WHEN BOTH ARE PRESENT. See the call site for why they are
// not ANDed. A plural that is absent — or present but every element blank —
// counts as NOT GIVEN and falls through to the singular, so `?states=` alone
// reads as 「no constraint」 exactly like an omitted `?state=`; the singular is
// trimmed and an empty one is likewise no constraint. Both empty ⇒ nil, which
// loreInClause turns into no clause at all rather than `IN ()`.
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

// loreFilterParamName names the parameter a rejected value arrived on. Saying
// "scope_kind" when the caller sent `?scope_kinds=` would point them at a field
// they never filled in, which is a worse answer than "too long" — it is a
// confident wrong one.
func loreFilterParamName(singular string, fromPlural bool) string {
	if fromPlural {
		return singular + "s"
	}
	return singular
}

// newLoreEntryDTO is the ONE row→wire projection for the READ face, so every
// row of every page is built from the same fields. The three writes do NOT come
// through here any more: they answer bounded receipts (T-33, owner 2026-09-07),
// because what a write can tell a caller is what the SERVER decided, and the
// whole entry is what the caller already had.
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
