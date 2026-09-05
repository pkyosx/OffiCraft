package main

// dal_lore_proposal_t33_test.go — T-33. 提案這一層的問題只有一個：審核看到的
// 差異，是不是就是會落地的東西；而它會不會在沒有人發現的情況下不是.
//
// 🔴 THE DIGEST TESTS ARE THE POINT OF THIS FILE. Every other assertion here is
// about a refusal a reader would notice anyway (a blank field, an unknown enum).
// The base-digest comparison is the one whose failure is SILENT: drop it and
// every proposal is still filed, still listed, still reads correctly, and the
// day one of them is applied it quietly reverts somebody else's edit.

import (
	"errors"
	"strings"
	"testing"
)

// t33Propose is a well-formed `update` proposal with the base digest left blank
// for each test to fill in — blank is refused, so a test that forgets to set it
// cannot accidentally pass.
//
// 🔴 Events IS AN EMPTY SLICE, NOT nil, AND THAT IS NOT COSMETIC. nil means
// 「這份提案沒說第 5 格」 and is refused; an empty slice is the claim 「這條條目
// 不該有事件」. The default seed (t33Write) writes an entry with no events, so
// this baseline proposes exactly the fifth cell that is already there.
func t33Propose(entryID string) LoreProposal {
	return LoreProposal{
		Events:  []LoreEvent{},
		EntryID: entryID,
		Kind:    "update",
		// 🔴 標題與第 1 格刻意不是同一句話，跟 t33Write() 同樣的理由：v8 的標題
		// 寫「發生了什麼」，第 1 格寫「我要做 X」。寫成同一句，一個把兩格接反的
		// 錯誤就沒有任何測試看得見。
		// ⚠️ 它也刻意跟 t33Write() 的標題**不一樣**：核可之後條目上的標題必須
		// 變成這一句，而如果兩邊一樣，「核可有沒有寫回標題」就無從分辨。
		Heading:     "組裝器搬過家，而條目還指著舊檔名",
		ImpactStars: 3,
		Encountered: "T-33 slot 4, wiring the proposal route",
		Fault:       "stale",
		Evidence:    "the entry names dal_lore.go, and the function moved to dal_lore_write.go in 8282fdef",
		Content:     "the fold happens in one place, and that place is lore_fold.go",
		RetireWhen:  "等組裝路徑不只一條",
		Impact:      "T-33 slot 3：條目指到 dal_lore.go，函式其實已經搬走",
		ActorID:     "ow-e27260b9ed05",
	}
}

// t33SeedForProposal writes a real entry through the ordinary write path and
// hands back its id and the digest of the original that was preserved with it.
//
// It goes through CreateLoreEntry rather than PutLoreEntry on purpose: a
// proposal is bound to an L0 revision, and PutLoreEntry writes none — an entry
// seeded that way would exercise the ErrLoreEntryNoOriginal arm and nothing else.
func t33SeedForProposal(t *testing.T, d *DAL) (string, string) {
	t.Helper()
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	got := t33Create(t, d, t33Write())
	return got.EntryID, got.SHA256
}

// 🔴 THE TEST THIS WHOLE ROUND EXISTS FOR. A proposal written against a digest
// that is not the entry's current one is REFUSED, and the refusal names both
// digests so the proposer can see which version he was holding.
func TestLoreProposalRefusesADigestThatIsNotTheEntrysCurrentOne(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	p := t33Propose(entryID)
	p.BaseSHA256 = strings.Repeat("0", 64)
	_, err := d.CreateLoreProposal(p, 2000)
	if !errors.Is(err, ErrLoreProposalStale) {
		t.Fatalf("a proposal against a version nobody is holding was accepted: %v", err)
	}
	// The message has to carry BOTH digests. A refusal that says only "stale"
	// leaves the proposer unable to tell whether he read an older version or
	// mistyped one character, and those want different next steps.
	if !strings.Contains(err.Error(), sha) || !strings.Contains(err.Error(), p.BaseSHA256) {
		t.Fatalf("the refusal names neither what he sent nor what is there: %v", err)
	}
	var n int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM lore_proposal`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("the refused proposal was stored anyway: %d rows", n)
	}

	// 🔴 THE POSITIVE CONTROL, and it is not optional: without it a
	// CreateLoreProposal that refused EVERYTHING would pass the assertion above.
	p.BaseSHA256 = sha
	got, err := d.CreateLoreProposal(p, 2000)
	if err != nil {
		t.Fatalf("the same proposal against the CURRENT digest was refused: %v", err)
	}
	if got.BaseSHA256 != sha || got.BaseRevisionID == 0 {
		t.Fatalf("the receipt does not bind to the revision it matched: %+v", got)
	}
	if got.SHA256 == "" || got.SHA256 == sha {
		t.Fatalf("the proposed version digests to nothing, or to the base: %+v", got)
	}
}

// 🔴 THE OTHER HALF OF THE SAME MECHANISM, and the one submit-time checking
// cannot see: the proposal was fine when it was filed, and the entry moved
// AFTERWARDS. This is the shape a stale pull request has, and applying it
// unnoticed is what silently reverts whoever edited in between.
//
// The second revision is written directly, because nothing in the tree rewrites
// an entry yet — see the file header of api_lore_proposal.go and the report on
// this branch. That is precisely why the guard is here BEFORE the accept path
// exists: the accept path is what will start producing second revisions.
func TestLoreProposalGoesStaleWhenTheEntryMovesAfterItWasFiled(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	p := t33Propose(entryID)
	p.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(p, 2000); err != nil {
		t.Fatalf("file: %v", err)
	}

	before, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	if len(before.Proposals) != 1 {
		t.Fatalf("want one proposal, got %d", len(before.Proposals))
	}
	// The control: it is NOT stale yet. Without this the assertion below would
	// also pass for a `Stale` that is hard-coded true.
	if before.Proposals[0].Stale {
		t.Fatalf("a proposal filed against the current version reads as stale: %+v", before.Proposals[0])
	}
	if before.CurrentSHA256 != sha {
		t.Fatalf("current digest = %q, want %q", before.CurrentSHA256, sha)
	}

	// Somebody rewrites the entry.
	moved := t33Entry(entryID)
	moved.Content = "the fold happens in lore_fold.go, and the assembler cannot see L2"
	body := loreRevisionBody(moved, nil)
	if _, err := d.wdb.Exec(`
		INSERT INTO lore_revision (entry_id, body, sha256, actor_id, created_ts, shrink_chars)
		VALUES (?, ?, ?, 'somebody-else', 3000, 0)`,
		entryID, body, loreSHA256(body)); err != nil {
		t.Fatalf("second revision: %v", err)
	}

	after, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if !after.Proposals[0].Stale {
		t.Fatalf("the entry moved under this proposal and the list still calls it "+
			"current — applying it would discard the newer text with nothing to "+
			"show for it: %+v", after.Proposals[0])
	}
	// The digest it was compared against travels with the answer, so a reader
	// can re-derive `stale` instead of trusting it.
	if after.CurrentSHA256 == sha || after.CurrentSHA256 != loreSHA256(body) {
		t.Fatalf("current digest did not move with the entry: %q", after.CurrentSHA256)
	}
	if after.Proposals[0].BaseSHA256 != sha {
		t.Fatalf("the proposal's base digest was rewritten under it: %q", after.Proposals[0].BaseSHA256)
	}
}

// The proposal stores a WHOLE version, and the bytes stored are the bytes the
// shared renderer produces — which is the only reason a proposal's digest and a
// revision's digest can be compared at all.
func TestLoreProposalStoresTheWholeVersionUnderTheSharedRenderer(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	p := t33Propose(entryID)
	p.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(p, 2000); err != nil {
		t.Fatalf("file: %v", err)
	}
	list, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	row := list.Proposals[0]

	// 🔴 THE EXPECTATION IS BUILT WITHOUT loreProposalEntry, ON PURPOSE. Writing
	// `want := loreRevisionBody(loreProposalEntry(p))` would run the value under
	// test through the code under test on both sides of the comparison: a mapping
	// that dropped a field would drop it in `want` too and the assertion would
	// hold forever. Measured, not feared — that is exactly what the first version
	// of this test did, and a mutant that blanked a cell in the mapping walked
	// straight past it.
	//
	// 🔴 第二個參數是條目**目前**的事件，不是 nil：提案帶不動第 5 格，語意是
	// 「事件維持現狀」。用 nil 建期望值會讓「提案悄悄清空事件」變成通過的行為。
	seededEvents, evErr := d.ListLoreEvents(entryID)
	if evErr != nil {
		t.Fatalf("list events: %v", evErr)
	}
	want := loreRevisionBody(LoreEntry{
		Heading: p.Heading, Content: p.Content,
		RetireWhen: p.RetireWhen, Impact: p.Impact, ImpactStars: p.ImpactStars,
	}, seededEvents)
	if row.Body != want {
		t.Fatalf("stored body is not what the shared renderer produces:\n got %q\nwant %q", row.Body, want)
	}
	if row.SHA256 != loreSHA256(want) {
		t.Fatalf("stored digest does not digest the stored body: %q", row.SHA256)
	}
	// Every field present with its NAME AND ITS VALUE. The name alone would be
	// satisfied by a renderer that printed six headings over six blanks — which
	// is the shape a dropped field actually has.
	for _, f := range []struct{ section, value string }{
		{"heading:", p.Heading}, {"content:", p.Content},
		{"retire_when:", p.RetireWhen}, {"impact:", p.Impact},
	} {
		if !strings.Contains(row.Body, f.section+"\n"+f.value+"\n") {
			t.Fatalf("the stored version drops the %q section or its value: %q", f.section, row.Body)
		}
	}
	if row.Content != p.Content || row.Fault != "stale" || row.Encountered != p.Encountered ||
		row.Evidence != p.Evidence || row.ActorID != p.ActorID {
		t.Fatalf("the row is not what was proposed: %+v", row)
	}
}

// A `remove` proposes no version, and one that carries body fields is refused
// rather than stored with them ignored — a version on the reviewer's screen that
// no accept would ever write is the description/result gap this shape exists to
// close.
func TestLoreProposalRemoveCarriesNoVersion(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	bad := t33Propose(entryID)
	bad.Kind = "remove"
	bad.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(bad, 2000); !errors.Is(err, ErrLoreProposalRemoveBody) {
		t.Fatalf("a removal carrying a whole new version was accepted: %v", err)
	}

	good := LoreProposal{
		EntryID: entryID, Kind: "remove", BaseSHA256: sha,
		Encountered: "T-33 slot 4", Fault: "never-true",
		Evidence: "the function it names has never existed in this tree",
		ActorID:  "ow-e27260b9ed05",
	}
	got, err := d.CreateLoreProposal(good, 2000)
	if err != nil {
		t.Fatalf("a bare removal was refused: %v", err)
	}
	if got.SHA256 != "" {
		t.Fatalf("a removal proposed a version: %q", got.SHA256)
	}
	list, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if row := list.Proposals[0]; row.Kind != "remove" || row.Body != "" || row.Content != "" {
		t.Fatalf("the stored removal carries a version: %+v", row)
	}
}

// An `update` is held to the SAME field rules a write is, because accepting one
// means writing it through the ordinary write path — a proposal that path would
// refuse can never be accepted, and it would sit in the queue looking exactly
// like one that could.
func TestLoreProposalUpdateIsHeldToTheWritePathsFieldRules(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	for _, tc := range []struct {
		name string
		edit func(*LoreProposal)
		want error
	}{
		// ⚠️ 這一列以前是 {"blank trigger", …, ErrLoreTriggerBlank}。`trigger` 那一格
		// 被 `rc-9002654dd81c`（2026-09-06）併進 heading，而它守的東西沒有跟著走：
		// 「提案的第一格空白要被寫入路徑自己的錯誤擋下來」現在指的是 heading。
		{"blank heading", func(p *LoreProposal) { p.Heading = " " }, ErrLoreHeadingBlank},
		{"blank content", func(p *LoreProposal) { p.Content = "" }, ErrLoreContentBlank},
		{"unknown kind", func(p *LoreProposal) { p.Kind = "patch" }, ErrLoreProposalKindUnknown},
		{"unknown fault", func(p *LoreProposal) { p.Fault = "bad" }, ErrLoreProposalFaultUnknown},
		{"blank encountered", func(p *LoreProposal) { p.Encountered = "" }, ErrLoreProposalEncountered},
		{"blank evidence", func(p *LoreProposal) { p.Evidence = "\t" }, ErrLoreProposalEvidence},
		{"blank base digest", func(p *LoreProposal) { p.BaseSHA256 = "" }, ErrLoreProposalBaseBlank},
		{"no actor", func(p *LoreProposal) { p.ActorID = "" }, ErrLoreActorBlank},
	} {
		p := t33Propose(entryID)
		p.BaseSHA256 = sha
		tc.edit(&p)
		if _, err := d.CreateLoreProposal(p, 2000); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}

	// The positive control for the whole table: the unedited proposal lands.
	ok := t33Propose(entryID)
	ok.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(ok, 2000); err != nil {
		t.Fatalf("the unedited proposal was refused, so the table above proves nothing: %v", err)
	}
}

// A proposal identical to what is already there is refused: it costs a reviewer
// a read and can end in no change.
func TestLoreProposalRefusesAVersionIdenticalToTheBase(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	entry := t33Get(t, d, entryID)
	same := t33Propose(entryID)
	same.BaseSHA256 = sha
	same.Heading, same.Content = entry.Heading, entry.Content
	same.RetireWhen, same.Impact = entry.RetireWhen, entry.Impact
	// 🔴 星等也要抄過來，而它是這一批新加的一格。漏抄它的話這份提案就**不是**
	// 「一模一樣」了，摘要會不同、ErrLoreProposalNoChange 不會觸發，而測試會紅
	// 在一個看起來像「守衛失效」的地方 —— 那正是這一格加進摘要要防的事。
	same.ImpactStars = entry.ImpactStars
	if _, err := d.CreateLoreProposal(same, 2000); !errors.Is(err, ErrLoreProposalNoChange) {
		t.Fatalf("a proposal that changes nothing was filed: %v", err)
	}
}

// An entry nobody has proposed anything for lists EMPTY rather than failing:
// 「沒有人提案」 and 「沒有這條」 are different facts.
func TestLoreProposalListIsEmptyRatherThanMissing(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	list, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.Proposals == nil || len(list.Proposals) != 0 {
		t.Fatalf("want an empty list, got %+v", list.Proposals)
	}
	if list.CurrentSHA256 != sha || list.CurrentRevisionID == 0 {
		t.Fatalf("an empty list still has to say what the entry stands at: %+v", list)
	}

	// An entry that carries no L0 original at all is refused rather than
	// answered with an empty base — proposing against nothing is not a proposal.
	t33Put(t, d, t33Entry("lore-no-original"))
	if _, err := d.ListLoreProposals("lore-no-original"); !errors.Is(err, ErrLoreEntryNoOriginal) {
		t.Fatalf("an entry with no original listed anyway: %v", err)
	}
	p := t33Propose("lore-no-original")
	p.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(p, 2000); !errors.Is(err, ErrLoreEntryNoOriginal) {
		t.Fatalf("a proposal against an entry with no original was filed: %v", err)
	}
}

// An entry id nothing carries is a flat refusal, never a stored row pointing at
// nothing.
func TestLoreProposalRefusesAnUnknownEntry(t *testing.T) {
	d := newTestDAL(t)
	_, sha := t33SeedForProposal(t, d)
	p := t33Propose("lore-nobody-carries-this")
	p.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(p, 2000); !errors.Is(err, ErrLoreEntryUnknown) {
		t.Fatalf("a proposal against a non-existent entry: %v", err)
	}
}

// 🔴 THE ORDER IS ORDERED BY TIME, AND THE TEST MAKES TIME AND id DISAGREE.
//
// A proposal id is random hex, so on real data `ORDER BY id DESC` lands on the
// right answer roughly half the time and on the wrong one the other half — and
// both look identical to a reader, because every row is present and every field
// is right. Only the reading order is wrong, and the reviewer reads the first
// one. A test that files two proposals and trusts whatever ids came out cannot
// see this: it passes on the coin-flip. So the ids are forced into the order
// OPPOSITE to the filing order, which is the only arrangement in which
// id-ordering and time-ordering give different answers every single run.
func TestLoreProposalListsNewestFirstWhenIdOrderContradictsTime(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	older := t33Propose(entryID)
	older.BaseSHA256 = sha
	older.Content = "the fold happens in one place, and that place is lore_fold.go"
	gotOlder, err := d.CreateLoreProposal(older, 1000)
	if err != nil {
		t.Fatalf("filing the older proposal: %v", err)
	}
	newer := t33Propose(entryID)
	newer.BaseSHA256 = sha
	newer.Content = "the fold happens in lore_fold.go, and nothing else may assemble one"
	gotNewer, err := d.CreateLoreProposal(newer, 2000)
	if err != nil {
		t.Fatalf("filing the newer proposal: %v", err)
	}

	// Forced, not hoped for: the OLDER row gets the id that sorts LAST
	// descending, so an id-ordered read must lead with the replaced version.
	const olderID, newerID = "lp-ffffffffffff", "lp-000000000000"
	for _, r := range []struct{ from, to string }{
		{gotOlder.ProposalID, olderID}, {gotNewer.ProposalID, newerID},
	} {
		if _, err := d.wdb.Exec(`UPDATE lore_proposal SET id = ? WHERE id = ?`, r.to, r.from); err != nil {
			t.Fatalf("renaming %s: %v", r.from, err)
		}
	}

	list, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Proposals) != 2 {
		t.Fatalf("both proposals must be listed, got %d", len(list.Proposals))
	}
	if list.Proposals[0].ID != newerID {
		t.Fatalf("the list leads with the version its proposer already replaced: "+
			"got %s (created_ts %.0f), want %s (created_ts %.0f)",
			list.Proposals[0].ID, list.Proposals[0].CreatedTS,
			newerID, list.Proposals[1].CreatedTS)
	}
	if list.Proposals[0].CreatedTS <= list.Proposals[1].CreatedTS {
		t.Fatalf("the listing is not descending in time: %.0f then %.0f",
			list.Proposals[0].CreatedTS, list.Proposals[1].CreatedTS)
	}
}

// ── 第 5 格：提案帶得動它，而且核可時整批換掉 ────────────────────────────────
//
// 🔴 這一段取代了先前那支釘住暫定行為的測試
// （TestLoreProposalKeepsTheEntrysEventsRatherThanSilentlyClearingThem）。那支
// 釘的是「四格改成這樣，事件維持現狀」，而負責人 2026-09-03 在卡 rc-e5c34500face
// 裁定的是相反的東西：「改得動 —— 提案就該帶完整的新版本，包含所有事件」。
// 語意被換掉了，所以釘住舊語意的那支測試被換掉，不是被放寬。

// t33Event 是一筆最小的合法事件：時與事有，人／地／物空著。
func t33Event(ts float64, what string) LoreEvent {
	return LoreEvent{HappenedTS: ts, What: what}
}

// 🔴 提案帶的是它**自己**的事件，不是條目現有的。
//
// 這是這一批的核心斷言。舊語意底下，body 是拿條目現有的事件渲染出來的，所以一份
// 想改事件的提案在審核者眼前跟一份不想改的長得一模一樣。現在 body 就是提案主張的
// 那一份 —— 審核者比對的位元組，就是核可時會落地的位元組。
func TestLoreProposalBodyCarriesTheProposalsOwnEventsNotTheEntrysCurrentOnes(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	w := t33Write()
	w.Events = []LoreEvent{t33Event(1700000000, "Seth 把畫面切成假資料")}
	seeded, err := d.CreateLoreEntry(w, 1000)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	p := t33Propose(seeded.EntryID)
	p.BaseSHA256 = seeded.SHA256
	p.Events = []LoreEvent{t33Event(1700000500, "Kyle 把畫面接回真後端")}
	if _, err := d.CreateLoreProposal(p, 2000); err != nil {
		t.Fatalf("file: %v", err)
	}
	list, err := d.ListLoreProposals(seeded.EntryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	body := list.Proposals[0].Body
	if !strings.Contains(body, "Kyle 把畫面接回真後端") {
		t.Fatalf("提案自己帶的事件沒有進到它的完整版本裡:\n%s", body)
	}
	if strings.Contains(body, "Seth 把畫面切成假資料") {
		t.Fatalf("提案的版本裡混進了條目現有的事件 —— 審核者比對的就不是會落地的"+
			"那一份了:\n%s", body)
	}
	// 而且不是靠「事件根本不在 body 裡」蒙混過去的：拿掉事件的渲染必須不一樣。
	if loreSHA256(body) == loreSHA256(loreRevisionBody(LoreEntry{
		Heading: p.Heading, Content: p.Content, RetireWhen: p.RetireWhen, Impact: p.Impact,
	}, nil)) {
		t.Fatal("帶事件與不帶事件渲染出同一串 —— 第 5 格不在 digest 裡")
	}
	// 讀回來的那一份就是送進去的那一份。
	if got := list.Proposals[0].Events; len(got) != 1 || got[0].What != "Kyle 把畫面接回真後端" {
		t.Fatalf("提案的事件清單沒有被讀回來: %+v", got)
	}
}

// 🔴 只動第 5 格、四格一字未改的提案，**不是**「什麼都沒改」。
//
// ErrLoreProposalNoChange 比的是摘要，而摘要含事件，所以這種提案會活下來。它是
// 這整條路存在的理由：機器串錯了一筆事件，四格是對的，唯一要改的就是第 5 格。
func TestLoreProposalThatOnlyMovesEventsIsNotNoChange(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	w := t33Write()
	w.Events = []LoreEvent{t33Event(1700000000, "機器串錯的那一筆")}
	seeded, err := d.CreateLoreEntry(w, 1000)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 四格原封不動 —— 用寫入時的那一份。
	p := LoreProposal{
		EntryID: seeded.EntryID, Kind: "update", BaseSHA256: seeded.SHA256,
		Encountered: "在讀這條的時候發現第 5 格串錯了", Fault: "never-true",
		Evidence: "那台機器當天根本沒有被碰過",
		Heading:  w.Heading, Content: w.Content,
		RetireWhen: w.RetireWhen, Impact: w.Impact, ImpactStars: w.ImpactStars,
		Events:  []LoreEvent{t33Event(1700000000, "人工修好的那一筆")},
		ActorID: "ow-e27260b9ed05",
	}
	if _, err := d.CreateLoreProposal(p, 2000); err != nil {
		t.Fatalf("一份只改第 5 格的提案被當成「什麼都沒改」擋掉了 —— "+
			"機器串錯事件就沒有任何一條路修得了: %v", err)
	}

	// 反向對照：連事件也一字不改，才是真的什麼都沒改。沒有這一半，上面那個 nil
	// 只證明這道檢查被拿掉了，不證明它變準了。
	same := p
	same.Events = []LoreEvent{t33Event(1700000000, "機器串錯的那一筆")}
	if _, err := d.CreateLoreProposal(same, 2100); !errors.Is(err, ErrLoreProposalNoChange) {
		t.Fatalf("一份跟現況一模一樣的提案應該被拒絕: %v", err)
	}
}

// 🔴 一份 `update` 提案**沒說**第 5 格是拒絕，送一個空陣列是合法的主張。
//
// 兩者長得一樣的話，一次漏填就會在審核者完全看不見的地方清空事件 —— 而核可時
// 事件是整批換掉的，所以那次漏填會真的落地。
func TestLoreProposalUpdateMustSayWhatTheFifthCellShouldBe(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	missing := t33Propose(entryID)
	missing.BaseSHA256 = sha
	missing.Events = nil
	if _, err := d.CreateLoreProposal(missing, 2000); !errors.Is(err, ErrLoreProposalEventsMissing) {
		t.Fatalf("一份沒提到第 5 格的 update 被收下了: %v", err)
	}

	// 空陣列是主張，不是漏填 —— 它必須被收下。沒有這一半，上面那條就會被一個
	// 「所有 update 都拒絕」的實作滿足。
	empty := t33Propose(entryID)
	empty.BaseSHA256 = sha
	empty.Events = []LoreEvent{}
	if _, err := d.CreateLoreProposal(empty, 2100); err != nil {
		t.Fatalf("空陣列（「這條不該有事件」）應該是合法的主張: %v", err)
	}
}

// 一份 `remove` 不主張任何版本，第 5 格也一樣。
func TestLoreProposalRemoveCarriesNoEvents(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	p := LoreProposal{
		EntryID: entryID, Kind: "remove", BaseSHA256: sha,
		Encountered: "讀到它的時候", Fault: "misled", Evidence: "它被撈出來的情境跟它講的不是同一件事",
		Events:  []LoreEvent{t33Event(1700000000, "a version no accept would ever write")},
		ActorID: "ow-e27260b9ed05",
	}
	if _, err := d.CreateLoreProposal(p, 2000); !errors.Is(err, ErrLoreProposalRemoveEvents) {
		t.Fatalf("一份 remove 帶著事件被收下了: %v", err)
	}
	p.Events = nil
	if _, err := d.CreateLoreProposal(p, 2100); err != nil {
		t.Fatalf("不帶事件的 remove 應該可以: %v", err)
	}
}

// 提案的事件跟寫入路徑受**同一組**規則管。一份寫入會拒絕的事件，如果提案收得
// 下，它就會躺在佇列裡，看起來跟一份可以被核可的提案一模一樣。
func TestLoreProposalHoldsItsEventsToTheWritePathsRules(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	for _, tc := range []struct {
		name string
		ev   LoreEvent
		want error
	}{
		{"沒有時間", LoreEvent{What: "有事沒時"}, ErrLoreEventTimeMissing},
		{"沒有事", LoreEvent{HappenedTS: 1700000000}, ErrLoreEventWhatBlank},
		{"人不是 type:name", LoreEvent{HappenedTS: 1700000000, What: "x", Actor: "Seth"}, ErrLoreEventKeyMalformed},
		{"型別沒被核准", LoreEvent{HappenedTS: 1700000000, What: "x", Actor: "planet:mars"}, ErrLoreEventKeyUnknownType},
	} {
		p := t33Propose(entryID)
		p.BaseSHA256 = sha
		p.Events = []LoreEvent{tc.ev}
		if _, err := d.CreateLoreProposal(p, 2000); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
}

// 🔴 審核者看得出提案改了**哪幾筆**事件，而且看得出來的方式是可以自己重算的。
//
// 這是負責人選了「改得動」之後必須處理的顧慮：一份提案改得動事件，卻沒有任何一
// 面告訴審核者它改了哪幾筆，等於把一次刪除藏在一份看起來只改文字的提案裡。
// 「加了哪幾筆」在提案的清單裡看得到；「刪了哪幾筆」只表現成一個「不在」，那才
// 是他會漏掉的那一半。
func TestLoreProposalListShowsWhichEventsTheProposalMoves(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	kept := t33Event(1700000000, "留著不動的那一筆")
	wrong := t33Event(1700000100, "機器串錯的那一筆")
	fixed := t33Event(1700000100, "人工修好的那一筆")

	w := t33Write()
	w.Events = []LoreEvent{kept, wrong}
	seeded, err := d.CreateLoreEntry(w, 1000)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	p := t33Propose(seeded.EntryID)
	p.BaseSHA256 = seeded.SHA256
	p.Events = []LoreEvent{kept, fixed}
	if _, err := d.CreateLoreProposal(p, 2000); err != nil {
		t.Fatalf("file: %v", err)
	}

	list, err := d.ListLoreProposals(seeded.EntryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	row := list.Proposals[0]
	if len(row.EventsAdded) != 1 || row.EventsAdded[0].What != fixed.What {
		t.Fatalf("events_added 沒有指出他加了哪一筆: %+v", row.EventsAdded)
	}
	if len(row.EventsRemoved) != 1 || row.EventsRemoved[0].What != wrong.What {
		t.Fatalf("events_removed 沒有指出他刪了哪一筆 —— 刪除只表現成一個「不在」，"+
			"這正是審核者會漏掉的那一半: %+v", row.EventsRemoved)
	}
	// 🔴 原封不動的那一筆**不能**被報成「刪一筆、加一筆」。用 id 比對就會，因為
	// 提案的事件根本沒有 lore_event 的 id。那種噪音會讓人停止讀差異。
	for _, ev := range append(append([]LoreEvent{}, row.EventsAdded...), row.EventsRemoved...) {
		if ev.What == kept.What {
			t.Fatalf("沒有被動到的那一筆被算進差異裡了: %+v", row)
		}
	}
	// 🔴 兩份清單都在，所以這個差異是**可以被重算驗證的**，不是要人相信的。
	if len(list.CurrentEvents) != 2 || len(row.Events) != 2 {
		t.Fatalf("被比較的兩邊沒有一起送出來: current=%+v proposed=%+v",
			list.CurrentEvents, row.Events)
	}
}

// ── 核可：事件整批換成提案帶的那一份 ─────────────────────────────────────────

// 🔴 這一支釘的是負責人裁定的另一半：提案帶得動事件，**而且核可時真的落地**。
// 少了整批取代，提案就只加得動事件、刪不掉 —— 機器串錯的那一筆就永遠留著，而
// 那正是他推翻我的理由。
func TestApplyLoreProposalReplacesTheEntrysEventsWholesale(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	kept := t33Event(1700000000, "留著不動的那一筆")
	wrong := t33Event(1700000100, "機器串錯的那一筆")
	fixed := t33Event(1700000100, "人工修好的那一筆")

	w := t33Write()
	w.Events = []LoreEvent{kept, wrong}
	seeded, err := d.CreateLoreEntry(w, 1000)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	p := t33Propose(seeded.EntryID)
	p.BaseSHA256 = seeded.SHA256
	p.Events = []LoreEvent{kept, fixed}
	filed, err := d.CreateLoreProposal(p, 2000)
	if err != nil {
		t.Fatalf("file: %v", err)
	}

	applied, err := d.ApplyLoreProposal(filed.ProposalID, "human:Seth", 3000)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	events, err := d.ListLoreEvents(seeded.EntryID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("核可後第 5 格不是提案帶的那一份: %+v", events)
	}
	for _, ev := range events {
		if ev.What == wrong.What {
			t.Fatalf("核可後那筆串錯的事件還在 —— 事件是被『合併』而不是整批換掉的，"+
				"這樣提案就永遠刪不掉任何一筆: %+v", events)
		}
	}
	if events[1].What != fixed.What {
		t.Fatalf("提案帶的那一筆沒有落地: %+v", events)
	}

	// 四格也落地了，而且新的 L0 那一列就是審核者核可的那串位元組。
	entry, err := d.GetLoreEntry(seeded.EntryID)
	if err != nil || entry == nil {
		t.Fatalf("entry: %+v %v", entry, err)
	}
	if entry.Content != p.Content {
		t.Fatalf("四格沒有落地: %+v", entry)
	}
	rev, err := d.LatestLoreRevision(seeded.EntryID)
	if err != nil || rev == nil {
		t.Fatalf("revision: %+v %v", rev, err)
	}
	if rev.SHA256 != filed.SHA256 || rev.ID != applied.RevisionID {
		t.Fatalf("落地的那一版不是審核者看到的那一版: rev=%+v filed=%+v", rev, filed)
	}
	// 🔴 被刪掉的那一筆沒有消失無蹤：舊的 L0 原文原封不動留著它。這是
	// loreRevisionBody 把第 5 格算進 body 的理由，也是「整批換掉」可以被接受的
	// 前提 —— 換掉的是現況，不是紀錄。
	old, err := d.GetLoreRevision(seeded.EntryID, seeded.RevisionID)
	if err != nil || old == nil {
		t.Fatalf("old revision: %+v %v", old, err)
	}
	if !strings.Contains(old.Body, wrong.What) {
		t.Fatalf("被換掉的那一筆事件在 L0 原文裡也不見了:\n%s", old.Body)
	}

	// 落地之後，同一份提案就過期了：它是對著舊版本寫的。
	if _, err := d.ApplyLoreProposal(filed.ProposalID, "human:Seth", 3100); !errors.Is(err, ErrLoreProposalStale) {
		t.Fatalf("同一份提案被核可了兩次: %v", err)
	}
}

// 核可一份對著舊版本寫的提案會安靜地丟掉中間那個人的修改 —— 所以它被擋在
// 「按下核可」的那一刻，而不是只擋在提交與讀取的時候。
func TestApplyLoreProposalRefusesAProposalTheEntryHasMovedUnder(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	p := t33Propose(entryID)
	p.BaseSHA256 = sha
	filed, err := d.CreateLoreProposal(p, 2000)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	// 別人在中間改了它。
	moved := loreRevisionBody(t33Entry(entryID), nil)
	if _, err := d.wdb.Exec(`
		INSERT INTO lore_revision (entry_id, body, sha256, actor_id, created_ts, shrink_chars)
		VALUES (?, ?, ?, 'somebody-else', 2500, 0)`,
		entryID, moved, loreSHA256(moved)); err != nil {
		t.Fatalf("second revision: %v", err)
	}
	if _, err := d.ApplyLoreProposal(filed.ProposalID, "human:Seth", 3000); !errors.Is(err, ErrLoreProposalStale) {
		t.Fatalf("核可了一份對著舊版本寫的提案 —— 中間那個人的修改就這樣沒了: %v", err)
	}

	// 反向對照：沒有被動過的提案核可得了。沒有這一半，上面只證明這條路壞掉了。
	fresh := t33Propose(entryID)
	fresh.BaseSHA256 = loreSHA256(moved)
	fresh.Content = "a version written against what is there now"
	ok, err := d.CreateLoreProposal(fresh, 2600)
	if err != nil {
		t.Fatalf("file fresh: %v", err)
	}
	if _, err := d.ApplyLoreProposal(ok.ProposalID, "human:Seth", 3100); err != nil {
		t.Fatalf("一份對著現況寫的提案應該核可得了: %v", err)
	}
}

// 一份 `remove` 不主張任何版本，所以沒有東西可以套上去。它要的是 retire。
func TestApplyLoreProposalRefusesARemoval(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	filed, err := d.CreateLoreProposal(LoreProposal{
		EntryID: entryID, Kind: "remove", BaseSHA256: sha,
		Encountered: "讀到它的時候", Fault: "never-true", Evidence: "這件事沒有發生過",
		ActorID: "ow-e27260b9ed05",
	}, 2000)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := d.ApplyLoreProposal(filed.ProposalID, "human:Seth", 3000); !errors.Is(err, ErrLoreProposalNotUpdate) {
		t.Fatalf("一份 remove 被當成版本套上去了: %v", err)
	}
	if _, err := d.ApplyLoreProposal("lp-nobody-carries-this", "human:Seth", 3000); !errors.Is(err, ErrLoreProposalUnknown) {
		t.Fatalf("一個不存在的提案 id 沒有被指名: %v", err)
	}
}
