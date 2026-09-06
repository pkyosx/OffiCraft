package main

// dal_lore_recall.go — T-33: the READ side of lore_recall_log.
//
// 🔴 WHY THIS FILE EXISTS AT ALL. Until it landed, `FROM lore_recall_log`
// appeared NOWHERE outside the tests: three write points file a row on every
// retrieval (api_lore_search.go, api_lore_read.go twice) and nothing ever read
// one back. A journal with no reader is indistinguishable, from the owner's
// side, from a journal that was never written — which is the exact failure
// migrations/00090 was opened to prevent, arriving one layer higher up.
//
// 🔴 IT IS A SEPARATE FILE FROM dal_lore.go DELIBERATELY. dal_lore.go is the
// WRITE-side home of the journal (LoreRecall / InsertLoreRecall) and is under
// review for unrelated reasons; splitting the read out keeps this addition from
// touching a file somebody else is holding. Nothing here is a second opinion
// about the table's shape — the struct below is the same row, minus the columns
// this read does not use.

import "strings"

// LoreRecallEvent is ONE journalled retrieval, as the activity panel needs it.
//
// 🔴 IT CARRIES `Returned` RAW, NOT DECODED. The JSON in that cell is
// loreRecallReturned's business (lore_recall.go), and decoding it here would
// put a second reader of that shape in the DAL where nothing would notice the
// two drifting apart. The DAL's job is to hand back the row.
//
// 🔴 SessionBootTS RIDES ALONG EVEN THOUGH THE CALLER PASSED IT IN. The
// 「上線後多久」 figure is created_ts − session_boot_ts, and the anchor that
// subtraction must use is the one STAMPED ON THE ROW, never the one the caller
// happened to be holding: those are the same number today only because the
// filter demands it, and a future caller that widens the filter would otherwise
// compute an offset against the wrong session with nothing to say so.
type LoreRecallEvent struct {
	ID int64
	// Query is the DOOR this retrieval came through — one of
	// loreRecallQuerySearch / loreRecallQueryEntryRead /
	// loreRecallQueryRevisionRead. See lore_recall.go: it is a path marker, not
	// the caller's search text (that rides inside Returned).
	Query         string
	SubjectID     string
	Returned      string
	CreatedTS     float64
	SessionBootTS float64
}

// ListLoreRecallForSession returns every ANCHORED retrieval one actor made
// during ONE session, oldest first.
//
// 🔴 BOTH HALVES OF THE FILTER ARE LOAD-BEARING AND NEITHER IMPLIES THE OTHER.
// `session_state = 'anchored'` throws out the rows whose anchor is absence
// rather than a number — an 'unanchored' boot fold and an 'unrecorded'
// pre-column row BOTH carry session_boot_ts = 0, so a bare
// `session_boot_ts = ?` would sweep every one of them in the moment a caller
// asked about a member whose anchor is 0. That is exactly the member this route
// answers 「這一任還沒開始／已結束」 for, so the bug would surface as a stopped
// member appearing to have read the whole boot fold's worth of history.
//
// 🔴 AND IT IS SCOPED TO ONE session_boot_ts RATHER THAN 「the latest N rows」.
// The whole reason 00090 stamps the anchor onto the row is that the member's
// own cell is overwritten by the NEXT session; filtering on the value the
// caller resolved a moment ago is what makes 「這一任」 mean this one and not
// 「whatever is most recent」.
//
// A bootTS of 0 (or less) is refused as an empty result rather than run: no
// anchored row can honestly carry it, so a query would return nothing anyway —
// but returning early says so at the seam instead of leaving the caller to
// infer it from emptiness.
func (d *DAL) ListLoreRecallForSession(actorID string, bootTS float64) ([]LoreRecallEvent, error) {
	if actorID == "" || bootTS <= 0 {
		return []LoreRecallEvent{}, nil
	}
	rows, err := d.rdb.Query(`
		SELECT id, query, subject_id, returned, created_ts, session_boot_ts
		  FROM lore_recall_log
		 WHERE actor_id = ?
		   AND session_state = ?
		   AND session_boot_ts = ?
		 ORDER BY created_ts ASC, id ASC`,
		actorID, loreRecallSessionAnchored, bootTS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LoreRecallEvent{}
	for rows.Next() {
		var e LoreRecallEvent
		if err := rows.Scan(&e.ID, &e.Query, &e.SubjectID, &e.Returned,
			&e.CreatedTS, &e.SessionBootTS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LoreEntryLabel is what the activity panel needs to know about ONE entry that
// was read: how it introduces itself, and where it stands now.
type LoreEntryLabel struct {
	Heading string
	// Status is the lifecycle value (`active` / `superseded` / `retired` /
	// `underspecified`, the CHECK set in migrations/00089). It is DISPLAY, not
	// error handling: a retired entry is still reachable by id, so the panel
	// uses this to say 「已退役」 beside a heading rather than to decide whether
	// the line may be shown.
	Status string
}

// LoreHeadingsByID looks up the 標題 and status of the given entry ids.
//
// 🔴 IT RETURNS A MAP AND NOT A LIST, BECAUSE A MISS IS AN ANSWER. An entry id
// journalled last hour can name an entry that can no longer be found; the caller
// must be able to say 「他讀過這一條，而這一條現在查不到」 rather than drop the
// row. A slice would have made the miss invisible — the caller would see a
// shorter list and have nothing to compare it against.
//
// 🔴 ONE MAP LOOKUP DECIDES BOTH FACTS, AND THAT IS THE WHOLE POINT OF RETURNING
// A STRUCT RATHER THAN TWO MAPS. The wire promises `status == "" if and only if
// heading_found == false`; two maps, or two queries, would let those two drift
// into a combination that compiles and cannot be true, and a reader facing
// `heading_found: true` beside `status: ""` would have to pick one to believe.
//
// 🔴 NO STATUS FILTER IN THE QUERY. `status` is retirement, and a retired entry
// is precisely the one whose read is most worth showing: 「retired」 means the
// entry is no longer RETRIEVED (search and the boot directory skip it), not that
// it cannot be read by id — GetLoreEntry has no status filter either. Filtering
// here would turn a retired entry's journalled read into a phantom.
func (d *DAL) LoreHeadingsByID(ids []string) (map[string]LoreEntryLabel, error) {
	out := map[string]LoreEntryLabel{}
	if len(ids) == 0 {
		return out, nil
	}
	// De-duplicate first: one entry read three times in a session is three
	// journal rows and one lookup.
	seen := map[string]bool{}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		args = append(args, id)
	}
	if len(args) == 0 {
		return out, nil
	}
	q := `SELECT id, heading, status FROM lore_entry WHERE id IN (?` +
		strings.Repeat(",?", len(args)-1) + `)`
	rows, err := d.rdb.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var lbl LoreEntryLabel
		if err := rows.Scan(&id, &lbl.Heading, &lbl.Status); err != nil {
			return nil, err
		}
		out[id] = lbl
	}
	return out, rows.Err()
}
