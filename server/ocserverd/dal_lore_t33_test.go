package main

// T-33 — the L1 write seam and its two join tables.

import (
	"errors"
	"strings"
	"testing"
)

func t33Entity(t *testing.T, d *DAL, id, typ, canonical string) {
	t.Helper()
	if _, err := d.wdb.Exec(
		`INSERT INTO entity (id, type, canonical) VALUES (?, ?, ?)`, id, typ, canonical); err != nil {
		t.Fatalf("seed entity %s: %v", id, err)
	}
}

func t33Entry(id string) LoreEntry {
	return LoreEntry{
		ID: id,
		// 🔴 標題格照 v8 的要求寫「發生了什麼」，而不是「我要做 X」。
		// ⚠️ 這裡以前還有一格 `Trigger`，刻意寫成跟 Heading 不同的句子，好讓一個
		// 「把 heading 接到 trigger 上」的錯誤露出來。`rc-9002654dd81c`
		// （2026-09-06「合併成 heading 一格」）之後只剩一格，那個對調的錯誤在構造
		// 上不存在了 —— 不是這個 fixture 放鬆了守衛。
		Heading:     "前端畫面接的是假資料，而畫面上看不出來",
		Origin:      "agent:O-197",
		Content:     "the fold happens in one place",
		RetireWhen:  "等前端不再有假資料模式",
		Impact:      "T-33 slot 3：兩個區塊對同一件事說法不一樣",
		ImpactStars: 2,
		CreatedTS:   100,
		UpdatedTS:   100,
	}
}

func t33Put(t *testing.T, d *DAL, e LoreEntry) {
	t.Helper()
	if err := d.PutLoreEntry(e); err != nil {
		t.Fatalf("put %s: %v", e.ID, err)
	}
}

func t33Get(t *testing.T, d *DAL, id string) *LoreEntry {
	t.Helper()
	got, err := d.GetLoreEntry(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return got
}

func TestLoreEntryRoundTrips(t *testing.T) {
	d := newTestDAL(t)
	want := t33Entry("me-aaa")
	want.Origin = "human:Seth"
	want.Status = "underspecified"
	want.EditableBy = "owner-gated"
	want.Supersedes = "me-old"
	t33Put(t, d, want)

	got := t33Get(t, d, "me-aaa")
	if got == nil {
		t.Fatal("get returned nil for an entry that was just written")
	}
	if *got != want {
		t.Fatalf("round trip lost something:\n got %+v\nwant %+v", *got, want)
	}
}

// A missing id is not an error — the callers ask "does this exist" on purpose.
func TestLoreGetMissingEntryIsNilNotError(t *testing.T) {
	d := newTestDAL(t)
	got, err := d.GetLoreEntry("me-nope")
	if err != nil {
		t.Fatalf("get missing: unexpected error %v", err)
	}
	if got != nil {
		t.Fatalf("get missing: want nil, got %+v", got)
	}
}

// 🔴 origin is a SUBJECT KEY, and an unapproved type prefix is REFUSED BY NAME.
// This is the fail-closed half: the value set is open (any agent or human can be
// an origin), so nothing but the type vocabulary can be checked — and a prefix
// that quietly passed would let an entry claim an author from a category nobody
// approved, with the ranking rules still honouring it.
func TestLoreOriginMustBeAKnownTypePrefix(t *testing.T) {
	d := newTestDAL(t)
	for _, ok := range []string{"agent:O-197", "human:Seth", "role:assistant"} {
		e := t33Entry("me-" + ok)
		e.Origin = ok
		if err := d.PutLoreEntry(e); err != nil {
			t.Fatalf("origin %q must be accepted: %v", ok, err)
		}
	}
	e := t33Entry("me-bad-prefix")
	e.Origin = "wizard:Merlin"
	err := d.PutLoreEntry(e)
	if !errors.Is(err, ErrLoreOriginUnknownType) {
		t.Fatalf("err = %v, want ErrLoreOriginUnknownType", err)
	}
	// 🔴 IT MUST SAY WHICH PREFIX. "rejected" without the name sends the reader
	// hunting through a whole entry for the one word that was wrong.
	if !strings.Contains(err.Error(), "wizard") {
		t.Fatalf("the refusal must name the offending prefix, got %q", err)
	}
}

// 🔴 `member:` IS GONE — the type is `agent:`. The pair that has to work is
// agent/human, and `member` does not make that cut: the owner is a member too,
// so it fails to exclude the very thing `human` names.
func TestLoreOriginMemberPrefixIsNoLongerAType(t *testing.T) {
	d := newTestDAL(t)
	e := t33Entry("me-member")
	e.Origin = "member:Kyle"
	if err := d.PutLoreEntry(e); !errors.Is(err, ErrLoreOriginUnknownType) {
		t.Fatalf("err = %v, want the `member` prefix to be refused", err)
	}
}

// A blank or shapeless origin is refused rather than defaulted. There is no
// default author, and an "unspecified" written as though it were a person would
// be a claim nobody made — while still counting as a ranking axis.
func TestLoreOriginBlankOrMalformedIsRefused(t *testing.T) {
	d := newTestDAL(t)
	for _, tc := range []struct {
		origin string
		want   error
	}{
		{"", ErrLoreOriginBlank},
		{"   ", ErrLoreOriginBlank},
		{"Seth", ErrLoreOriginMalformed},
		{"human:", ErrLoreOriginMalformed},
		{":Seth", ErrLoreOriginMalformed},
	} {
		e := t33Entry("me-origin")
		e.Origin = tc.origin
		if err := d.PutLoreEntry(e); !errors.Is(err, tc.want) {
			t.Fatalf("origin %q: err = %v, want %v", tc.origin, err, tc.want)
		}
	}
}

// 🔴 ONE COPY OF THE TYPE VOCABULARY. The prefixes origin accepts are exactly the
// rows of entity_type — the same list subjects are checked against. This test is
// what would catch a second, hard-coded list being introduced in Go: approve a
// type in the table alone, and the write must start passing.
func TestLoreOriginTypesComeFromTheEntityTypeTable(t *testing.T) {
	d := newTestDAL(t)
	e := t33Entry("me-newtype")
	e.Origin = "vendor:Acme"
	if err := d.PutLoreEntry(e); !errors.Is(err, ErrLoreOriginUnknownType) {
		t.Fatalf("err = %v, want the unapproved prefix to be refused first", err)
	}
	if _, err := d.wdb.Exec(`INSERT INTO entity_type (type) VALUES ('vendor')`); err != nil {
		t.Fatalf("approve type: %v", err)
	}
	if err := d.PutLoreEntry(e); err != nil {
		t.Fatalf("after approving the type the same write must pass, got %v", err)
	}
}

// 🔴 這裡曾經有兩支 trigger 的守衛，它們跟著那一格一起被 `rc-9002654dd81c`
// （2026-09-06「合併成 heading 一格」）拿掉了，而兩支的下場**不一樣**，所以分開講：
//
//   * TestLoreTriggerIsRequiredAndBlankIsRefusedAtTheRowSeam 守的是「第一格空白要
//     在最底層的 upsert 縫被拒、而且拒絕的那一筆不可以落地」。那件事**還在**，
//     只是那一格現在叫 heading ⇒ 它整支併進了下面的
//     TestPutLoreEntryRefusesABlankHeading（三種空白 ＋ 不落地的檢查都搬過去了）。
//   * TestLoreTriggerHasNoLengthCap 守的是「第一格沒有長度上限」。那個性質
//     **真的消失了**，不是搬家：合併之後這一格就是 heading，而 heading 有 140 個
//     rune 的硬上限（owner 2026-09-05）。這裡沒有留一支恆真的替身，因為一支永遠
//     為真的測試在畫面上跟一支真的守衛長得一模一樣。

// origin is L1, which means it must be readable through the ordinary entry read
// — the one the assembler uses. A field that could only be reached through a
// governance query could not participate in ordering or truncation, which is the
// whole reason it was moved out of the meta table.
func TestLoreOriginSurvivesOnTheEntryRead(t *testing.T) {
	d := newTestDAL(t)
	e := t33Entry("me-ccc")
	e.Origin = "human:Seth"
	t33Put(t, d, e)
	t33Entity(t, d, "e-1", "repo", "repo:officraft")
	if err := d.PutLoreSubject("me-ccc", "e-1"); err != nil {
		t.Fatalf("file subject: %v", err)
	}
	list, err := d.ListLoreEntriesBySubject("e-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Origin != "human:Seth" {
		t.Fatalf("origin did not survive the by-subject read: %+v", list)
	}
}

// 🔴 An edit keeps the birth timestamp. created_ts is what the staleness
// judgement reads; letting an edit reset it would make any entry look freshly
// minted the moment somebody tightened its wording.
func TestLorePutIsAnUpsertThatKeepsCreatedTS(t *testing.T) {
	d := newTestDAL(t)
	t33Put(t, d, t33Entry("me-ddd"))

	edited := t33Entry("me-ddd")
	edited.Content = "tightened"
	edited.CreatedTS = 999
	edited.UpdatedTS = 500
	t33Put(t, d, edited)

	got := t33Get(t, d, "me-ddd")
	if got.Content != "tightened" || got.UpdatedTS != 500 {
		t.Fatalf("the edit did not land: %+v", got)
	}
	if got.CreatedTS != 100 {
		t.Fatalf("CreatedTS = %v, want the original 100 — an edit is not a birth", got.CreatedTS)
	}
}

func TestLorePutRefusesABlankID(t *testing.T) {
	d := newTestDAL(t)
	err := d.PutLoreEntry(LoreEntry{})
	if !errors.Is(err, ErrLoreEntryIDBlank) {
		t.Fatalf("err = %v, want ErrLoreEntryIDBlank", err)
	}
}

// 🔴 ONE ENTRY, MANY SUBJECTS — the reason subjects are a join table.
func TestLoreEntryCanCarryManySubjects(t *testing.T) {
	d := newTestDAL(t)
	t33Put(t, d, t33Entry("me-eee"))
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	t33Entity(t, d, "e-mem", "member", "member:Kyle")
	for _, ent := range []string{"e-repo", "e-mem"} {
		if err := d.PutLoreSubject("me-eee", ent); err != nil {
			t.Fatalf("file %s: %v", ent, err)
		}
	}
	// Re-filing an existing pair is the state the caller asked for, not an error.
	if err := d.PutLoreSubject("me-eee", "e-repo"); err != nil {
		t.Fatalf("re-file: %v", err)
	}
	subs, err := d.ListLoreSubjects("me-eee")
	if err != nil {
		t.Fatalf("list subjects: %v", err)
	}
	if len(subs) != 2 || subs[0] != "e-mem" || subs[1] != "e-repo" {
		t.Fatalf("subjects = %v, want the two filed entities, sorted", subs)
	}
	for _, ent := range []string{"e-repo", "e-mem"} {
		list, err := d.ListLoreEntriesBySubject(ent)
		if err != nil {
			t.Fatalf("list by %s: %v", ent, err)
		}
		if len(list) != 1 || list[0].ID != "me-eee" {
			t.Fatalf("list by %s = %+v, want the one entry", ent, list)
		}
	}
}

// 🔴 retired means "no longer retrieved", and the row still exists. Both halves
// are asserted: a test that only checked the list would also pass against a hard
// delete, which is the path this ticket deliberately does not build.
func TestLoreRetiredIsUnretrievedButNotGone(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	for _, id := range []string{"me-live", "me-dead"} {
		t33Put(t, d, t33Entry(id))
		if err := d.PutLoreSubject(id, "e-repo"); err != nil {
			t.Fatalf("file %s: %v", id, err)
		}
	}
	dead := t33Entry("me-dead")
	dead.Status = "retired"
	t33Put(t, d, dead)

	list, err := d.ListLoreEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "me-live" {
		t.Fatalf("list = %+v, want only the active entry", list)
	}
	n, err := d.CountLoreEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1 — the count must agree with the list", n)
	}
	if got := t33Get(t, d, "me-dead"); got == nil || got.Status != "retired" {
		t.Fatalf("the retired entry must still be readable by id, got %+v", got)
	}
}

// 'superseded' and 'underspecified' are NOT filtered — no ruling says they are,
// and dropping them here would decide that by accident, invisibly.
func TestLoreOnlyRetiredIsFilteredFromRetrieval(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	for _, st := range []string{"active", "superseded", "underspecified", "retired"} {
		e := t33Entry("me-" + st)
		e.Status = st
		t33Put(t, d, e)
		if err := d.PutLoreSubject(e.ID, "e-repo"); err != nil {
			t.Fatalf("file %s: %v", e.ID, err)
		}
	}
	n, err := d.CountLoreEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3 — only 'retired' is excluded", n)
	}
}

// The count and the list answer the same question, so they must never disagree.
func TestLoreCountAgreesWithList(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	for _, id := range []string{"me-1", "me-2", "me-3"} {
		t33Put(t, d, t33Entry(id))
		if err := d.PutLoreSubject(id, "e-repo"); err != nil {
			t.Fatalf("file %s: %v", id, err)
		}
	}
	list, err := d.ListLoreEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	n, err := d.CountLoreEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != len(list) {
		t.Fatalf("count = %d, list = %d", n, len(list))
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
	// An entity nobody filed anything against answers zero, not an error.
	if n, err := d.CountLoreEntriesBySubject("e-unused"); err != nil || n != 0 {
		t.Fatalf("count for an unused entity = %d, %v; want 0, nil", n, err)
	}
}

// 六格裡存在 lore_entry 上的那幾格 survive a write and a read, by name. A column
// dropped from the INSERT list or transposed in the scan would otherwise show up
// much later as an entry that lost one cell.
//
// 🔴 每一格的值都不一樣，而且**沒有一個是零值**：兩格值相同的話，把它們對調的
// bug 會讀回來完全正確。impact_stars 用 3、reviewed 用 true，理由同上——用 0 和
// false 的話，一個根本沒寫進去的欄位會跟一個寫對了的欄位長得一模一樣。
func TestLoreEntryCellsRoundTripByName(t *testing.T) {
	d := newTestDAL(t)
	e := LoreEntry{
		ID:          "me-five",
		Heading:     "HD",
		Content:     "CO",
		RetireWhen:  "RW",
		Impact:      "IM",
		ImpactStars: 3,
		Reviewed:    true,
		Origin:      "agent:O-197",
	}
	t33Put(t, d, e)
	got := t33Get(t, d, "me-five")
	if got.Heading != "HD" || got.Content != "CO" ||
		got.RetireWhen != "RW" || got.Impact != "IM" ||
		got.ImpactStars != 3 || !got.Reviewed {
		t.Fatalf("a body cell was lost or transposed: %+v", *got)
	}
}

// 🔴 星等的值域擋在 DAL，不是只擋在 CHECK 上，而錯誤是具名的：CHECK 只回得出
// 「constraint failed」，上層只能把它報成 500，而送錯星等的人是可以自己修好的。
//
// 0 也一起被斷言是**合法**的，而且那不是順手：0 的意思是「還沒判」，把它擋掉
// 等於逼每一條既有條目與每一次沒填的寫入當場被判一個等級。
func TestLoreImpactStarsRefusesWhatIsNotAStar(t *testing.T) {
	d := newTestDAL(t)
	for _, stars := range []int{-1, 4, 7} {
		e := t33Entry("me-stars")
		e.ImpactStars = stars
		if err := d.PutLoreEntry(e); !errors.Is(err, ErrLoreImpactStarsRange) {
			t.Fatalf("impact_stars=%d 被收下了: %v", stars, err)
		}
	}
	for _, stars := range []int{0, 1, 2, 3} {
		e := t33Entry("me-stars")
		e.ImpactStars = stars
		if err := d.PutLoreEntry(e); err != nil {
			t.Fatalf("impact_stars=%d 是合法的，卻被擋了: %v", stars, err)
		}
	}
}

// 🔴 標題格空白被拒，而且是在**這個原始的 upsert 縫**上，不只在 CreateLoreEntry
// 上。只擋在寫入路徑等於留一個側門，而從側門進來的無標題條目跟正門進來的長得
// 一模一樣。
//
// 🔴 合併之後（`rc-9002654dd81c`）這一支同時扛著原本 trigger 那支守的東西：一條
// 空標題的條目**既撈不到**（heading 是搜尋唯一掃得到的那一軸）**又在清單上跟一條
// 寫完的長得一樣**。所以三種空白與「被拒的那一筆不可以落地」都在這裡驗，而不是
// 只驗一種空白就算數。
func TestPutLoreEntryRefusesABlankHeading(t *testing.T) {
	d := newTestDAL(t)
	for _, blank := range []string{"", "   ", "\n\t"} {
		e := t33Entry("me-noheading")
		e.Heading = blank
		if err := d.PutLoreEntry(e); !errors.Is(err, ErrLoreHeadingBlank) {
			t.Fatalf("heading = %q: err = %v, want ErrLoreHeadingBlank", blank, err)
		}
		if got := t33Get(t, d, "me-noheading"); got != nil {
			t.Fatalf("被拒的寫入不可以落地, got %+v", got)
		}
	}
}

// 🔴 `events`：人／地／物空著是合法的，而且「空著」看得出來——不是「未知」。
//
// 這一條釘的是負責人明確要的那件事：「查不出是誰」跟「還沒有人去查」必須長得
// 不一樣。只要有人在任何一層塞了佔位字串進去，這一條就會紅。
func TestLoreEventEmptyCellsStayEmptyAndAreNotFilledIn(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	w := t33Write()
	w.Events = []LoreEvent{
		{HappenedTS: 1700000000, What: "Seth 把畫面切成假資料", Actor: "human:Seth",
			Place: "machine:seth-m5", Object: "service:ocserverd"},
		// 一筆只有時與事的事件：人／地／物都不知道。這是合法的。
		{HappenedTS: 1700000100, What: "有人重開了前端"},
	}
	res, err := d.CreateLoreEntry(w, 1000)
	if err != nil {
		t.Fatalf("create with events: %v", err)
	}
	got, err := d.ListLoreEvents(res.EntryID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %+v", got)
	}
	// 順序是事情發生的順序。
	if got[0].HappenedTS != 1700000000 || got[1].HappenedTS != 1700000100 {
		t.Fatalf("events must come back in happened_ts order: %+v", got)
	}
	if got[0].Actor != "human:Seth" || got[0].Place != "machine:seth-m5" ||
		got[0].Object != "service:ocserverd" {
		t.Fatalf("人/地/物 did not survive the round trip: %+v", got[0])
	}
	if got[1].Actor != "" || got[1].Place != "" || got[1].Object != "" {
		t.Fatalf("空著的人/地/物被填上了東西——「查不出是誰」跟「還沒有人去查」"+
			"從此分不開了: %+v", got[1])
	}
}
