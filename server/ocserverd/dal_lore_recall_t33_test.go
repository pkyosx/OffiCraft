package main

// dal_lore_recall_t33_test.go — T-33: the READ side of lore_recall_log.
//
// 🔴 THE ONE TEST THIS FILE EXISTS FOR is TestListLoreRecallForSessionSeparates
// TheSessions. Everything the activity panel promises rests on 「這一任」 really
// meaning this one — the owner asked for a panel that 「每次重新上線就清空」, and
// the mechanism that delivers it is a FILTER, not a delete. A filter that
// quietly matched every session would render exactly like a working one on any
// station whose members have only ever booted once, which is every test fixture
// and every fresh install.

import "testing"

// seedRecall files one journal row directly, so the read side is measured
// against what is actually IN the table rather than against the writer's idea of
// it. recordLoreRecall is deliberately not used: it resolves the anchor from the
// roster, which is the very thing these rows are pretending to be historical
// about.
func seedRecall(t *testing.T, d *DAL, actor, query, entryID string, createdTS, bootTS float64, state string) {
	t.Helper()
	r := LoreRecall{
		ActorID:   actor,
		Query:     query,
		CreatedTS: createdTS,
		Returned: encodeLoreRecallReturned(loreRecallReturned{
			Entries: []string{entryID},
		}),
		SessionBootTS: bootTS,
		SessionState:  state,
	}
	if err := d.InsertLoreRecall(r); err != nil {
		t.Fatalf("seed recall %s@%v: %v", entryID, createdTS, err)
	}
}

// TestListLoreRecallForSessionSeparatesTheSessions — two rows for the SAME
// actor and the SAME entry, one stamped with this session's anchor and one with
// the previous session's. Only the current one may come back.
//
// 🔴 THE TWO ROWS ARE OTHERWISE IDENTICAL ON PURPOSE. Same actor, same door,
// same entry: `session_boot_ts` is the ONLY cell that separates them, which is
// the whole claim migrations/00082 makes about why that column had to exist.
// If the fixture varied anything else, a filter that keyed off the wrong column
// could still pass.
func TestListLoreRecallForSessionSeparatesTheSessions(t *testing.T) {
	d := newTestDAL(t)

	const thisBoot, prevBoot = 2000.0, 1000.0

	// The previous session's read — real, journalled, and NOT this session's.
	seedRecall(t, d, "m-1", loreRecallQueryEntryRead, "le-old",
		prevBoot+30, prevBoot, loreRecallSessionAnchored)
	// This session's read.
	seedRecall(t, d, "m-1", loreRecallQueryEntryRead, "le-new",
		thisBoot+45, thisBoot, loreRecallSessionAnchored)

	got, err := d.ListLoreRecallForSession("m-1", thisBoot)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("這一任應該只有 1 列，拿到 %d 列：%+v", len(got), got)
	}
	if got[0].SessionBootTS != thisBoot {
		t.Errorf("回來的那一列錨在 %v，應該是這一任的 %v",
			got[0].SessionBootTS, thisBoot)
	}
	// Name the entry, not just the count: a filter that returned the WRONG one
	// row would satisfy a bare length check perfectly.
	if ids := loreRecallEntryIDs(got[0]); len(ids) != 1 || ids[0] != "le-new" {
		t.Errorf("回來的是 %v，應該是這一任讀的 le-new（上一任讀的是 le-old）", ids)
	}
}

// TestListLoreRecallForSessionDropsUnanchoredRows — the second half of the
// filter, and the half that is NOT implied by the first.
//
// 🔴 'unanchored' AND 'unrecorded' BOTH CARRY session_boot_ts = 0. So a filter
// written as a bare `session_boot_ts = ?` passes the test above (2000 ≠ 1000)
// and still sweeps every boot fold and every pre-column row into the answer the
// moment somebody asks about an anchor of 0 — which is exactly the member this
// route answers 「這一任還沒開始／已結束」 for. The DAL refuses bootTS <= 0
// outright AND filters on session_state; this pins the state half.
func TestListLoreRecallForSessionDropsUnanchoredRows(t *testing.T) {
	d := newTestDAL(t)

	// A boot fold: honestly anchorless (lore_fold.go writes these).
	seedRecall(t, d, "m-1", loreRecallQueryBoot, "le-fold",
		500, 0, loreRecallSessionUnanchored)
	// A pre-column row: nothing was recorded either way.
	seedRecall(t, d, "m-1", loreRecallQueryEntryRead, "le-ancient",
		400, 0, loreRecallSessionUnrecorded)
	// This session's real read.
	seedRecall(t, d, "m-1", loreRecallQueryEntryRead, "le-live",
		2010, 2000, loreRecallSessionAnchored)

	got, err := d.ListLoreRecallForSession("m-1", 2000)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("只有 1 列是 anchored 且屬於這一任，拿到 %d 列：%+v", len(got), got)
	}

	// The positive control for the anchorless rows: they ARE in the table, so
	// their absence above is the filter working and not an insert that failed.
	if asked, err := d.ListLoreRecallForSession("m-1", 0); err != nil {
		t.Fatalf("list bootTS=0: %v", err)
	} else if len(asked) != 0 {
		t.Errorf("bootTS<=0 必須回空（沒有錨就沒有這一任），拿到 %d 列", len(asked))
	}
	var all int
	if err := d.rdb.QueryRow(
		`SELECT COUNT(*) FROM lore_recall_log`).Scan(&all); err != nil {
		t.Fatalf("count: %v", err)
	}
	if all != 3 {
		t.Fatalf("陽性對照：表裡應該有 3 列（不然上面的「沒回來」可能只是沒寫進去），實際 %d", all)
	}
}

// TestListLoreRecallForSessionScopesToTheActor — one member's session anchor
// must not drag in another member's rows that happen to share the number.
// Anchors are epoch seconds and two members really can boot in the same second.
func TestListLoreRecallForSessionScopesToTheActor(t *testing.T) {
	d := newTestDAL(t)
	seedRecall(t, d, "m-1", loreRecallQueryEntryRead, "le-mine", 2010, 2000, loreRecallSessionAnchored)
	seedRecall(t, d, "m-2", loreRecallQueryEntryRead, "le-theirs", 2011, 2000, loreRecallSessionAnchored)

	got, err := d.ListLoreRecallForSession("m-1", 2000)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("只該拿到自己的那一列，拿到 %d 列：%+v", len(got), got)
	}
	if ids := loreRecallEntryIDs(got[0]); ids[0] != "le-mine" {
		t.Errorf("拿到 %v，應該只有 le-mine", ids)
	}
}

// TestLoreHeadingsByIDReportsTheMissAndTheStatus — the map contract.
//
// 🔴 A MISS MUST BE VISIBLE, and a RETIRED ENTRY MUST NOT BE A MISS. Those are
// two different facts that a status-filtered lookup would collapse into one:
// retirement stops an entry being RETRIEVED (search skips it), not being read by
// id, so a retired entry the member really did read must come back with its
// heading and `retired`, while an id that resolves to nothing must come back
// absent — the caller renders those with different words.
func TestLoreHeadingsByIDReportsTheMissAndTheStatus(t *testing.T) {
	d := newTestDAL(t)

	live := t33Entry("le-live")
	t33Put(t, d, live)

	gone := t33Entry("le-retired")
	gone.Heading = "退役了但當時真的被讀過"
	gone.Status = "retired"
	t33Put(t, d, gone)

	got, err := d.LoreHeadingsByID([]string{"le-live", "le-retired", "le-nope", "le-live"})
	if err != nil {
		t.Fatalf("headings: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("應該命中 2 顆（重複的 le-live 只算一次），拿到 %d：%+v", len(got), got)
	}
	if lbl, ok := got["le-retired"]; !ok {
		t.Error("退役的條目不該從查詢裡消失 —— 那會把「讀過但退役了」偽裝成「查無此條目」")
	} else if lbl.Status != "retired" || lbl.Heading != gone.Heading {
		t.Errorf("退役條目回 %+v，應該帶著原標題與 retired", lbl)
	}
	if _, ok := got["le-nope"]; ok {
		t.Error("不存在的 id 不該有命中")
	}
	if lbl := got["le-live"]; lbl.Status != "active" {
		t.Errorf("活著的條目 status = %q，應該是 active", lbl.Status)
	}
}
