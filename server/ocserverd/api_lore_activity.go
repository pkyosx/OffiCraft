package main

// api_lore_activity.go — T-33: 「這個成員這一任上線之後，去翻了哪些記憶？」
//
// 🔴 THIS IS THE FIRST READER THE RECALL JOURNAL HAS EVER HAD. Three write
// points file a row on every retrieval (api_lore_search.go:119,
// api_lore_read.go:109 and :194) and, until this route, `FROM lore_recall_log`
// occurred nowhere outside the tests. migrations/00082 wrote down at length why
// the anchor is stamped onto the row; this file is the half that spends it.
//
// 🔴 IT IS NOT AN MCP TOOL, DELIBERATELY. It is a COCKPIT panel — the owner
// looking at one member and asking what that member has been reading. Handing
// the same route to agents as a tool would put an agent's own reading history in
// front of it, which is a different feature with a different question behind it
// and has been ruled on by nobody. The route row therefore carries neither
// MCPTool nor an x-mcp block in the spec.
//
// 🔴 THE SCOPE IS ONE SESSION, AND THE SESSION IS 「這一任」. The owner asked for
// a panel that 「每次重新上線就清空」; the mechanism that delivers that is not a
// delete, it is this filter — the journal stays append-only (00082 is explicit
// that it must), and 「清空」 is what a reader scoped to the CURRENT anchor sees
// the moment a new session stamps a new one.

import (
	"encoding/json"
	"log"
	"net/http"
)

// loreActivityDTO is the whole body of GET /api/members/{member_id}/lore-activity.
//
// 🔴 SessionActive IS A FIELD AND NOT AN INFERENCE FROM len(rows). A member who
// is not running right now, and a member who is running and has read nothing,
// are two different answers to the owner's question, and on the wire they would
// otherwise be the same empty array. The 「這一任還沒開始／已結束」 case is the
// one where showing 「沒有取用紀錄」 would be an outright false statement: there
// is no 這一任 to have a record in.
type loreActivityDTO struct {
	MemberID string `json:"member_id"`
	// SessionActive is member.session_boot_ts > 0 — the member is anchored to a
	// live session right now. false means Rows is empty BECAUSE THERE IS NO
	// SESSION, not because the session was quiet.
	SessionActive bool `json:"session_active"`
	// SessionBootTS is the anchor every row's SinceBootSecs was measured
	// against, 0 when SessionActive is false. It is emitted so a reader can
	// check the subtraction rather than trust it.
	SessionBootTS float64 `json:"session_boot_ts"`
	// Rows is oldest-first and is NEVER null — an empty journal is `[]`.
	Rows []loreActivityRowDTO `json:"rows"`
}

// loreActivityRowDTO is ONE HEADING on the panel.
//
// 🔴 ONE ROW PER ENTRY, NOT PER RETRIEVAL. A search returns several entries in
// a single journal row; the owner asked for a list of headings, so a retrieval
// that returned four entries becomes four lines here. That flattening is why
// two lines can share a CreatedTS to the microsecond — they are the same event
// seen from the entry's side, and collapsing them back would hide three of the
// four things that were actually put in front of the agent.
type loreActivityRowDTO struct {
	CreatedTS float64 `json:"created_ts"`
	// SinceBootSecs is created_ts − session_boot_ts, computed from the anchor
	// STAMPED ON THE ROW (00082): 「上線後多久」, and it stays true forever
	// because it never consults the member's own cell, which the next session
	// overwrites.
	SinceBootSecs float64 `json:"since_boot_secs"`
	// Door is which write point filed this — `search`, `entry-read` or
	// `revision-read` (the loreRecallQuery* constants in lore_recall.go).
	// 「搜到了這幾條」 and 「我把這一條打開了」 are different events and the panel
	// must not merge them.
	Door    string `json:"door"`
	EntryID string `json:"entry_id"`
	// Heading is the entry's 標題 as it stands NOW, or "" when the entry can no
	// longer be found. HeadingFound is what separates those, and the row is
	// emitted either way.
	Heading string `json:"heading"`
	// HeadingFound = the entry id still resolves to a lore_entry row.
	//
	// 🔴 A `false` ROW IS NEVER DROPPED AND NEVER GIVEN AN INVENTED TITLE.
	// Dropping it would render 「他讀過這一條，而這一條後來查不到了」 as 「他沒讀過
	// 任何東西」 — the journal's whole value is that it records what happened at
	// a moment, and an entry's later disappearance does not un-happen the read.
	//
	// ⚠️ MEASURED 2026-09-06: `false` IS AN ANOMALY, NOT THE ORDINARY END OF AN
	// ENTRY'S LIFE. There is no `DELETE FROM lore_entry` anywhere in the tree,
	// and retirement is explicitly not a delete, so nothing on a healthy station
	// produces this state. The wording it drives must therefore say 「這個 id 查
	// 不到」 and must NOT say 「這一條被刪掉了」 — the second names a mechanism
	// that does not exist and would send its reader looking for it.
	HeadingFound bool `json:"heading_found"`
	// Status is the entry's lifecycle value, "" exactly when HeadingFound is
	// false — and BOTH are decided by the same map hit, so the invariant cannot
	// be broken by one of them being computed twice.
	//
	// 🔴 IT IS DISPLAY, NOT ERROR HANDLING. A retired entry is still reachable
	// by id (GetLoreEntry has no status filter; 「retired」 means no longer
	// RETRIEVED — search and the boot directory skip it), so a deep link lands
	// on it perfectly well. This field exists so the panel can mark it 「已退役」
	// rather than let a retired memory read as a live one.
	Status string `json:"status"`
}

// HandleGetMemberLoreActivityApiMembersMemberIdLoreActivityGet serves the panel.
//
// Permission is the same floor as GET /api/members/{member_id}/resume-summary —
// authGated + principalAdminAgent, declared on the route row. This is another
// control-others read of one member's private working history, so it sits on
// that floor and not a millimetre below it.
func (s *apiServer) HandleGetMemberLoreActivityApiMembersMemberIdLoreActivityGet(
	w http.ResponseWriter, r *http.Request, memberId string,
) {
	// anyMember for the same reason the resume-summary door uses it: a
	// contractor is a member whose activity the owner may look at.
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}

	// 🔴 NO ANCHOR ⇒ AN EMPTY LIST AND A FLAG, NEVER AN ERROR AND NEVER A
	// SILENT EMPTY. A stopped member is a perfectly good answer to this
	// question; it is just a DIFFERENT answer from 「這一任什麼都沒讀」.
	if m.SessionBootTS <= 0 {
		writeJSON(w, http.StatusOK, loreActivityDTO{
			MemberID:      m.ID,
			SessionActive: false,
			SessionBootTS: 0,
			Rows:          []loreActivityRowDTO{},
		})
		return
	}

	events, err := s.dal.ListLoreRecallForSession(m.ID, m.SessionBootTS)
	if err != nil {
		internalError(w, err)
		return
	}

	// Flatten first so the heading lookup is ONE query for the whole panel
	// rather than one per line.
	rows := make([]loreActivityRowDTO, 0, len(events))
	ids := make([]string, 0, len(events))
	for _, e := range events {
		for _, id := range loreRecallEntryIDs(e) {
			rows = append(rows, loreActivityRowDTO{
				CreatedTS: e.CreatedTS,
				// The row's OWN anchor, not m.SessionBootTS — identical today
				// because the filter demands it, and the honest source if that
				// filter is ever widened.
				SinceBootSecs: e.CreatedTS - e.SessionBootTS,
				Door:          e.Query,
				EntryID:       id,
			})
			ids = append(ids, id)
		}
	}
	headings, err := s.dal.LoreHeadingsByID(ids)
	if err != nil {
		internalError(w, err)
		return
	}
	// 🔴 ONE MAP HIT SETS ALL THREE CELLS. The wire promises
	// `status == "" if and only if heading_found == false`; deciding the two
	// from separate lookups is what would let that promise break silently.
	for i := range rows {
		lbl, ok := headings[rows[i].EntryID]
		rows[i].Heading, rows[i].Status, rows[i].HeadingFound = lbl.Heading, lbl.Status, ok
	}

	writeJSON(w, http.StatusOK, loreActivityDTO{
		MemberID:      m.ID,
		SessionActive: true,
		SessionBootTS: m.SessionBootTS,
		Rows:          rows,
	})
}

// loreRecallEntryIDs pulls the entry ids out of one journal row's `returned`
// payload.
//
// 🔴 AN UNREADABLE `returned` YIELDS NO ROWS AND A LOG LINE, NOT A PANIC AND NOT
// A FABRICATED LINE. encodeLoreRecallReturned already files the row with an
// empty payload when marshalling fails (lore_recall.go), so this shape really
// does occur; the honest rendering of 「這一列存在但我讀不出它撈到什麼」 is that
// it contributes no heading, and the log line is where a reader finds out it
// happened.
func loreRecallEntryIDs(e LoreRecallEvent) []string {
	if e.Returned == "" {
		return nil
	}
	var p loreRecallReturned
	if err := json.Unmarshal([]byte(e.Returned), &p); err != nil {
		log.Printf("[lore] activity: recall row %d has an unreadable returned "+
			"payload: %v — skipping its entries", e.ID, err)
		return nil
	}
	return p.Entries
}
