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
		// 🔴 第 1 格照負責人示範的寫法：一整句「我要做 X」，遠超過舊的 40 runes
		// 上限。它同時就是這條條目的標題。
		Trigger:    "我要確認一個 OffiCraft 前端畫面接的是真後端，還是假資料",
		Origin:     "agent:O-197",
		Content:    "the fold happens in one place",
		RetireWhen: "等前端不再有假資料模式",
		Problem:    "T-33 slot 3：兩個區塊對同一件事說法不一樣",
		CreatedTS:  100,
		UpdatedTS:  100,
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

// 🔴 第 1 格必填，空值被拒絕——而且是在最底層的 upsert 縫就被拒絕，不是只在
// 寫入路徑上。空的 trigger 條目躺在表裡誰都撈不到，而它從外面看起來跟一條寫好
// 的條目一模一樣：那正是這張票要消滅的無聲損失。
func TestLoreTriggerIsRequiredAndBlankIsRefusedAtTheRowSeam(t *testing.T) {
	d := newTestDAL(t)
	for _, blank := range []string{"", "   ", "\n\t"} {
		e := t33Entry("me-notrigger")
		e.Trigger = blank
		err := d.PutLoreEntry(e)
		if !errors.Is(err, ErrLoreTriggerBlank) {
			t.Fatalf("trigger = %q: err = %v, want ErrLoreTriggerBlank", blank, err)
		}
		if got := t33Get(t, d, "me-notrigger"); got != nil {
			t.Fatalf("a refused write must not land, got %+v", got)
		}
	}
}

// 🔴 第 1 格**沒有長度上限**，而且這一條就是那個上限被拿掉的理由：負責人自己
// 示範的好例子超過舊的 40 runes 很多。留著上限等於讓示範用的寫法寫不進來。
func TestLoreTriggerHasNoLengthCap(t *testing.T) {
	d := newTestDAL(t)
	e := t33Entry("me-longtrigger")
	e.Trigger = "【什麼時候要記起來】我要確認一個 OffiCraft 前端畫面接的是真後端，還是假資料"
	if n := len([]rune(e.Trigger)); n <= 40 {
		t.Fatalf("這一條要證明的是「超過 40 runes 也收」，但範例只有 %d runes", n)
	}
	if err := d.PutLoreEntry(e); err != nil {
		t.Fatalf("負責人示範的第 1 格寫法被拒絕了: %v", err)
	}
	got := t33Get(t, d, "me-longtrigger")
	if got == nil || got.Trigger != e.Trigger {
		t.Fatalf("第 1 格必須原封不動落地（不截斷）, got %+v", got)
	}
	// 更長也一樣：沒有「其實還是有一個上限」這種東西。
	e.Trigger = strings.Repeat("界", 500)
	if err := d.PutLoreEntry(e); err != nil {
		t.Fatalf("500 runes 的第 1 格被拒絕了，表示還有一個沒說出來的上限: %v", err)
	}
}

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

// The action axis is OPEN: an unrecognised name must be storable, because
// refusing it would block exactly the writes this mechanism exists to capture.
// The safety is at read time (memoryTrustScope), and it is checked here end to
// end so the two halves cannot drift apart.
func TestLoreActionsAreOpenAndClassifiedOnRead(t *testing.T) {
	d := newTestDAL(t)
	t33Put(t, d, t33Entry("me-fff"))
	for _, a := range []string{"deploy", "an-action-nobody-mapped"} {
		if err := d.PutLoreAction("me-fff", a); err != nil {
			t.Fatalf("file action %q: %v", a, err)
		}
	}
	actions, err := d.ListLoreActions("me-fff")
	if err != nil {
		t.Fatalf("list actions: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions = %v, want both stored", actions)
	}
	v := memoryTrustScope(actions)
	if v.Scope != TrustScopeTrust || !v.FellBack() {
		t.Fatalf("a stored unmapped action must fail closed and say so: %+v", v)
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

// 五格的前四格 survive a write and a read, by name. A column dropped from the
// INSERT list or transposed in the scan would otherwise show up much later as an
// entry that lost one cell.
func TestLoreFiveCellsRoundTripByName(t *testing.T) {
	d := newTestDAL(t)
	e := LoreEntry{
		ID:         "me-five",
		Trigger:    "TR",
		Content:    "CO",
		RetireWhen: "RW",
		Problem:    "PR",
		Origin:     "agent:O-197",
	}
	t33Put(t, d, e)
	got := t33Get(t, d, "me-five")
	if got.Trigger != "TR" || got.Content != "CO" ||
		got.RetireWhen != "RW" || got.Problem != "PR" {
		t.Fatalf("a body cell was lost or transposed: %+v", *got)
	}
}

// 🔴 第 5 格：人／地／物空著是合法的，而且「空著」看得出來——不是「未知」。
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
