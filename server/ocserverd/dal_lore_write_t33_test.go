package main

// T-33 — the write seam. Every test here asks the same question in a different
// place: after this call, is there an entry that can be REACHED and an original
// that can be READ, or only a row that looks finished?

import (
	"errors"
	"strings"
	"testing"
)

func t33Write() LoreWrite {
	return LoreWrite{
		// ⚠️ 這裡以前還有一格 `Trigger`，跟 Heading 刻意寫成不同的句子，好讓一個
		// 把兩格接反的錯誤露出來。`rc-9002654dd81c`（2026-09-06「合併成 heading
		// 一格」）之後只剩一格。
		Heading:     "開機脈絡在兩個地方各組了一次，兩份內容不一樣",
		Content:     "the fold happens in one place",
		RevisitWhen: "等組裝路徑不只一條",
		Impact:      "T-33 slot 3：兩個區塊對同一件事說法不一樣",
		ImpactStars: 2,
		Origin:      "agent:O-197",
		Subjects:    []string{"repo:officraft"},
		ActorID:     "ow-e27260b9ed05",
	}
}

func t33Create(t *testing.T, d *DAL, w LoreWrite) LoreWriteResult {
	t.Helper()
	got, err := d.CreateLoreEntry(w, 1000)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return got
}

func t33CountEntries(t *testing.T, d *DAL) int {
	t.Helper()
	var n int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM lore_entry`).Scan(&n); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	return n
}

// The happy path, checked on every table the write is supposed to touch — not
// on the receipt it returned. A receipt that agrees with itself proves nothing.
func TestLoreCreateWritesEntrySubjectsAndOriginal(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	got := t33Create(t, d, t33Write())

	entry := t33Get(t, d, got.EntryID)
	if entry == nil {
		t.Fatal("the entry the receipt names is not in the table")
	}
	if entry.Content != "the fold happens in one place" || entry.Status != "active" {
		t.Fatalf("entry came back wrong: %+v", *entry)
	}
	subjects, err := d.ListLoreSubjects(got.EntryID)
	if err != nil {
		t.Fatalf("list subjects: %v", err)
	}
	if len(subjects) != 1 || subjects[0] != "e-repo" {
		t.Fatalf("subject rows: got %v, want [e-repo]", subjects)
	}
	// 🔴 THE ORIGINAL. This is the assertion the whole file exists for: an entry
	// written with no revision behind it is invisible in every count and every
	// context, and only shows up when somebody goes looking for the original.
	rev, err := d.LatestLoreRevision(got.EntryID)
	if err != nil {
		t.Fatalf("latest revision: %v", err)
	}
	if rev == nil {
		t.Fatal("the entry was written with NO L0 original")
	}
	if rev.SHA256 != got.SHA256 || rev.SHA256 != loreSHA256(rev.Body) {
		t.Fatalf("the stored digest does not hash the stored body: %q", rev.SHA256)
	}
	if !strings.Contains(rev.Body, "the fold happens in one place") ||
		!strings.Contains(rev.Body, "等組裝路徑不只一條") {
		t.Fatalf("the original does not carry the body it was written from:\n%s", rev.Body)
	}
	if rev.ActorID != "ow-e27260b9ed05" {
		t.Fatalf("revision actor: got %q", rev.ActorID)
	}

	var metaActor string
	if err := d.rdb.QueryRow(
		`SELECT source_actor_id FROM lore_meta WHERE entry_id = ?`, got.EntryID).Scan(&metaActor); err != nil {
		t.Fatalf("lore_meta row: %v", err)
	}
	if metaActor != "ow-e27260b9ed05" {
		t.Fatalf("lore_meta actor: got %q", metaActor)
	}
}

// A blank field renders as a NAMED empty section rather than disappearing.
// Skipping blanks would hash the same bytes for "never written" and "deleted",
// which is the collapse this ticket exists to prevent.
func TestLoreRevisionBodyNamesEveryFieldEvenWhenBlank(t *testing.T) {
	body := loreRevisionBody(LoreEntry{Content: "only this one is set"}, nil)
	for _, name := range []string{"content", "revisit_when", "impact", "events"} {
		if !strings.Contains(body, name+":\n") {
			t.Fatalf("the rendered original drops the %q section:\n%s", name, body)
		}
	}
	// 🔴 標題與星等**在**原文裡，而這一段以前釘的是相反的事（「它們不在，那是
	// 一個已知的洞」）。洞被填掉的方式不是把它們從渲染器拿掉，是讓提案帶得動
	// 它們（owner rc-bbccbeb3d9e6 逐字「任何修改都是提案的一環」，00084 補了
	// lore_proposal 的兩欄）。
	// 少了這兩格的後果不是「少記一格」：核可寫進 lore_revision 的是提案渲染出來
	// 的那串 body，所以每一次核可都會留下一份宣稱「這條沒有標題」的原文，而條目
	// 上的標題其實還在 —— 一份主動說謊的原文，比一份沒答案的更糟。
	full := loreRevisionBody(LoreEntry{Heading: "H", ImpactStars: 3}, nil)
	for _, name := range []string{"heading", "impact_stars"} {
		if !strings.Contains(full, name+":\n") {
			t.Fatalf("原文漏了 %q 這一格 —— 核可之後它會被記成不存在:\n%s", name, full)
		}
	}
	// 🔴 值也要在，不只是欄名。只斷言欄名的話，一個把值換成空字串的改動會通過，
	// 而那正是這一段原本在防的失效形狀（摘要因為標題而變，讀的人卻不知道哪一格變了）。
	if !strings.Contains(full, "heading:\nH\n") || !strings.Contains(full, "impact_stars:\n3\n") {
		t.Fatalf("欄名在但值沒進去:\n%s", full)
	}
	// 🔴 換掉標題必須換掉摘要。這一條是 base_sha256 那整套機制對標題成立的唯一
	// 保證：不成立的話，一份基於舊標題寫的提案會顯示成「還是最新的」，而審核者
	// 按下核可時，那條的標題已經不是他讀過的那一個。星等同理。
	if loreSHA256(full) == loreSHA256(loreRevisionBody(LoreEntry{Heading: "H2", ImpactStars: 3}, nil)) {
		t.Fatal("換掉標題之後摘要一個位元組都沒變 —— 標題不在雜湊裡")
	}
	if loreSHA256(full) == loreSHA256(loreRevisionBody(LoreEntry{Heading: "H", ImpactStars: 1}, nil)) {
		t.Fatal("換掉星等之後摘要一個位元組都沒變 —— 星等不在雜湊裡")
	}
	if loreSHA256(body) == loreSHA256(loreRevisionBody(LoreEntry{}, nil)) {
		t.Fatal("an entry with a body and an entirely empty one hash the same")
	}
	// 🔴 `events`也在雜湊裡。少了這一條，事件可以被一次改寫整批弄掉而 digest
	// 不動——L0 原文層就對`events`失效了，而且沒有任何東西會報。
	withEvent := loreRevisionBody(LoreEntry{Content: "only this one is set"},
		[]LoreEvent{{HappenedTS: 1700000000, What: "Seth 換掉了那個檔案"}})
	if loreSHA256(withEvent) == loreSHA256(body) {
		t.Fatal("加了一筆事件之後 digest 沒變 —— `events`不在 L0 原文層裡")
	}
	// 同一組事件、不同送進來的順序，必須雜湊出同一串：否則 base_sha256 會因為
	// 一個沒有人看得見的差異報「過期」。
	a := loreRevisionBody(LoreEntry{}, []LoreEvent{
		{HappenedTS: 2, What: "b"}, {HappenedTS: 1, What: "a"}})
	bb := loreRevisionBody(LoreEntry{}, []LoreEvent{
		{HappenedTS: 1, What: "a"}, {HappenedTS: 2, What: "b"}})
	if loreSHA256(a) != loreSHA256(bb) {
		t.Fatalf("事件的送入順序改變了 digest:\n%q\n%q", a, bb)
	}
}

// 🔴 兩格空白都被拒：標題格 heading 與內容格 content，而且每一格是它自己的具名
// 錯誤——沒有哪一格會被折進別格的錯誤裡，不然寫的人只會知道「被擋了」而不知道
// 要補哪一格。
//
// ⚠️ 這裡以前是三格，第三格是 `trigger`；`rc-9002654dd81c`（2026-09-06）把它併進
// heading。少的是一個**欄位**，不是一道門：合併之後空的 heading 同時意味著「這條
// 撈不到」（它是搜尋唯一掃得到的那一軸）與「它在清單上跟一條寫完的長得一模一樣」，
// 兩種都是「沒有人會回來補」。
func TestLoreCreateRefusesTheTwoCellsThatMakeAnEntryReadable(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	for _, tc := range []struct {
		name   string
		mangle func(*LoreWrite)
		want   error
	}{
		{"blank heading", func(w *LoreWrite) { w.Heading = "" }, ErrLoreHeadingBlank},
		{"whitespace heading", func(w *LoreWrite) { w.Heading = " \t " }, ErrLoreHeadingBlank},
		{"blank content", func(w *LoreWrite) { w.Content = "" }, ErrLoreContentBlank},
		{"whitespace content", func(w *LoreWrite) { w.Content = "   " }, ErrLoreContentBlank},
	} {
		w := t33Write()
		tc.mangle(&w)
		_, err := d.CreateLoreEntry(w, 1000)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("a refused write left %d entries behind", n)
	}

	// 🔴 `revisit_when`與`impact`是**選填**，空著必須寫得進去。少了這一半，一個把兩格
	// 也變成必填的實作會讓上面全綠——而那就是擅自把選填改成必填。
	optional := t33Write()
	optional.RevisitWhen = ""
	optional.Impact = ""
	// ⚠️ 星等**不會**跟著歸零。負責人 2026-09-06 裁定「不允許給 0」之後，0 在新
	// 條目上不再是一個合法的值，所以「第 3、4 格是選填」這件事只能用一個真的星等
	// 來問；0 被拒的那一半移到下面它自己的斷言。
	optRes, err := d.CreateLoreEntry(optional, 1000)
	if err != nil {
		t.Fatalf("第 3、4 格是選填，空著必須收: %v", err)
	}
	// 🔴 「收下了」不等於「原樣收下」。少了下面這幾行，一個把空的第 3、4 格
	// 回填成「未知」的實作會讓上面全綠 —— 那是把「還沒有人去想」寫成「有人
	// 想過、結論是未知」，而兩者從此分不開。這一段是 2026-09-04 的陰性對照
	// 補上的：當時把回填塞進 CreateLoreEntry，整套測試 rc=0，一支都沒說話。
	landed := t33Get(t, d, optRes.EntryID)
	if landed == nil {
		t.Fatal("第 3、4 格空著的條目沒有落地")
	}
	if landed.RevisitWhen != "" || landed.Impact != "" {
		t.Fatalf("空著的第 3、4 格被發明了預設值: revisit_when=%q impact=%q", landed.RevisitWhen, landed.Impact)
	}
	// 🔴 送進來的星等必須原樣落地。把它換成別的值等於替寫入者做了一次他沒做的
	// 判定，而之後沒有任何人查得出來原本判的是幾。
	if landed.ImpactStars != optional.ImpactStars {
		t.Fatalf("落地的星等不是送進來的那個: impact_stars=%d, want %d", landed.ImpactStars, optional.ImpactStars)
	}

	// 🔴 負責人 2026-09-06「不允許給 0」，這一段從「省略折成 0」改成「0 被拒」：
	// 這支測試以前把 ImpactStars 歸零然後斷言它原樣落地，那個前提已經被推翻。
	// 錯誤必須是 ErrLoreImpactStarsUnjudged 而不是 ErrLoreImpactStarsRange：
	// 「我判了 0」跟「我送了 7」要修的東西不一樣，而 0 那個人要回去重新想一次。
	unjudged := t33Write()
	unjudged.ImpactStars = 0
	if _, err := d.CreateLoreEntry(unjudged, 1000); !errors.Is(err, ErrLoreImpactStarsUnjudged) {
		t.Fatalf("impact_stars=0 被新條目收下了（或報成了別的錯）: %v", err)
	}
	// 🔴 `reviewed` 不由寫入者帶進來，所以一條剛寫好的條目一定是沒蓋過章的。
	// 少了這一行，一個把 reviewed 接上請求體的實作會讓 agent 自己蓋自己的章，
	// 而且全綠。
	if landed.Reviewed {
		t.Fatal("一條剛寫進來的條目就已經是 reviewed —— 蓋章的那一欄被寫入路徑碰到了")
	}

	// 完整的寫入照樣成功。少了這一半，一個把每一筆寫入都拒掉的實作也會讓上面全綠。
	got := t33Create(t, d, t33Write())
	if entry := t33Get(t, d, got.EntryID); entry == nil {
		t.Fatal("a complete entry must land")
	}
}

// 🔴 `events`：每一筆事件的**時**與**事**都必填，而且每一格是它自己的具名錯誤。
// 一筆壞事件會讓整筆寫入被拒，條目本體一列都不留——事件跟條目是一起進去或一起
// 不進去。
func TestLoreCreateRefusesAnEventWithoutItsTimeOrItsWhat(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	good := LoreEvent{HappenedTS: 1700000000, What: "Seth 把畫面切成假資料"}
	for _, tc := range []struct {
		name string
		ev   LoreEvent
		want error
	}{
		{"no happened_ts", LoreEvent{What: "Seth 換掉了那個檔案"}, ErrLoreEventTimeMissing},
		{"zero happened_ts", LoreEvent{HappenedTS: 0, What: "x"}, ErrLoreEventTimeMissing},
		{"negative happened_ts", LoreEvent{HappenedTS: -1, What: "x"}, ErrLoreEventTimeMissing},
		{"blank what", LoreEvent{HappenedTS: 1700000000}, ErrLoreEventWhatBlank},
		{"whitespace what", LoreEvent{HappenedTS: 1700000000, What: "  \n"}, ErrLoreEventWhatBlank},
	} {
		w := t33Write()
		w.Events = []LoreEvent{good, tc.ev}
		_, err := d.CreateLoreEntry(w, 1000)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, err, tc.want)
		}
		if n := t33CountEntries(t, d); n != 0 {
			t.Fatalf("%s: 一筆壞事件之後留下了 %d 條條目", tc.name, n)
		}
		var evs int
		if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM lore_event`).Scan(&evs); err != nil {
			t.Fatalf("count events: %v", err)
		}
		if evs != 0 {
			t.Fatalf("%s: 一筆壞事件之後留下了 %d 列事件", tc.name, evs)
		}
	}

	// 🔴 反面：時＋事都給的事件必須收，人／地／物空著也必須收。少了這一半，一個
	// 把所有事件都拒掉的實作也會讓上面全綠。
	ok := t33Write()
	ok.Events = []LoreEvent{good}
	res, err := d.CreateLoreEntry(ok, 1000)
	if err != nil {
		t.Fatalf("一筆只有時與事的事件必須被接受: %v", err)
	}
	evs, err := d.ListLoreEvents(res.EntryID)
	if err != nil || len(evs) != 1 || evs[0].What != good.What {
		t.Fatalf("事件沒有落地: %+v %v", evs, err)
	}

	// 0 筆事件也是合法的：`events`是選填。
	none := t33Write()
	none.Events = nil
	if _, err := d.CreateLoreEntry(none, 1000); err != nil {
		t.Fatalf("0 筆事件是合法的: %v", err)
	}
}

// 🔴 舊條目照樣讀得回來。五格的必填只擋**新寫入**，一列在必填出現以前寫下的
// 條目不會因此變成讀不到。
//
// ⚠️ 這一支曾經是用 `degraded` 來斷言「舊條目還看得見」的。那個旗標已經被裁定
// 拿掉（rc-1e32c690018d），所以斷言換成它本來就該是的東西：那一列讀得回來，而且
// 讀回來的就是寫進去的。
func TestLoreEntriesWrittenBeforeTheRequirementStillReadBack(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	legacy := LoreEntry{
		ID: "lore-legacy-01", Content: "y",
		Origin: "agent:O-197", CreatedTS: 1000, UpdatedTS: 1000,
	}
	// 🔴 這一列是用**原始 INSERT** 種下去的，不是 PutLoreEntry，而那正是它要模擬
	// 的東西：v8 之前的條目沒有標題格，而 PutLoreEntry 現在會拒絕空標題。走
	// PutLoreEntry 就種不出一列 v8 之前的條目，只種得出一列今天合法的條目——
	// 那樣這支測試就不再是在問它宣稱要問的問題。
	if _, err := d.wdb.Exec(`
		INSERT INTO lore_entry (id, content, origin, status, editable_by,
			created_ts, updated_ts)
		VALUES (?, ?, ?, 'active', 'agent', ?, ?)`,
		legacy.ID, legacy.Content, legacy.Origin,
		legacy.CreatedTS, legacy.UpdatedTS); err != nil {
		t.Fatalf("seed a pre-requirement entry: %v", err)
	}
	got := t33Get(t, d, legacy.ID)
	if got == nil {
		t.Fatal("an entry written before the requirement stopped being readable")
	}
	if got.Content != legacy.Content ||
		got.RevisitWhen != "" || got.Impact != "" {
		t.Fatalf("a pre-requirement entry did not read back as written: %+v", got)
	}
	// 🔴 v8 加的三格在一列 v8 之前的條目上讀回來是零值，而且**讀得回來**：
	// 空標題不會讓這一列讀不到（必填只擋新寫入），星等是 0＝還沒判，章沒蓋過。
	// 少了這三行，一個對舊列直接爆掉、或把 0 當成 1 的讀取路徑會讓上面全綠。
	// ⚠️ 這一列以前是靠種一個 `trigger` 來模擬「v8 之前的條目」的。那一格已經被
	// `rc-9002654dd81c` 併進 heading ⇒ 現在種的是一列**連 heading 都空著**的條目，
	// 而那仍然是這支測試要問的形狀：一列擋不住今天的必填、卻必須照樣讀得回來。
	if got.Heading != "" || got.ImpactStars != 0 || got.Reviewed {
		t.Fatalf("一列 v8 之前的條目讀回來時被補了 v8 的欄位: heading=%q stars=%d reviewed=%v",
			got.Heading, got.ImpactStars, got.Reviewed)
	}
}

// A subject key nobody has ever used mints a PENDING entity and says so.
// Pending is the review queue, so the new subject must NOT reach the boot
// directory until somebody approves it.
func TestLoreCreateMintsUnknownSubjectAsPendingAndReportsIt(t *testing.T) {
	d := newTestDAL(t)
	w := t33Write()
	w.Subjects = []string{"repo:offcraft-typo"}

	got := t33Create(t, d, w)
	if len(got.Minted) != 1 || got.Minted[0].Canonical != "repo:offcraft-typo" {
		t.Fatalf("the mint was not reported back: %+v", got.Minted)
	}

	roster, err := d.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if len(roster) != 0 {
		t.Fatalf("an unapproved subject reached the boot directory: %+v", roster)
	}

	// Discrimination: approve it and the SAME query must now return it. Without
	// this half, a roster that is broken in some other way would pass above.
	if _, err := d.wdb.Exec(`UPDATE entity SET pending = 0 WHERE id = ?`, got.Minted[0].EntityID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	roster, err = d.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster after approval: %v", err)
	}
	if len(roster) != 1 || roster[0].Canonical != "repo:offcraft-typo" || roster[0].Entries != 1 {
		t.Fatalf("approved subject did not appear: %+v", roster)
	}
}

// An unapproved type prefix is refused BY NAME, and the whole write is rolled
// back — no orphan entry, no orphan revision.
func TestLoreCreateRefusesUnknownSubjectTypeAndWritesNothing(t *testing.T) {
	d := newTestDAL(t)
	w := t33Write()
	w.Subjects = []string{"repo:officraft", "vendor:acme"}

	_, err := d.CreateLoreEntry(w, 1000)
	if !errors.Is(err, ErrLoreSubjectUnknownType) {
		t.Fatalf("unknown subject type: got %v", err)
	}
	if !strings.Contains(err.Error(), "vendor") {
		t.Fatalf("the refusal does not name the prefix: %v", err)
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("the refused write left %d entries behind", n)
	}
	var revs int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM lore_revision`).Scan(&revs); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if revs != 0 {
		t.Fatalf("the refused write left %d originals behind", revs)
	}
}

// A malformed subject key is refused rather than minted. Minting `officraft`
// with no type would put a key into the ontology that no reader can parse.
func TestLoreCreateRefusesMalformedSubjectKey(t *testing.T) {
	d := newTestDAL(t)
	for _, bad := range []string{"officraft", "repo:", ":officraft", "   "} {
		w := t33Write()
		w.Subjects = []string{bad}
		if _, err := d.CreateLoreEntry(w, 1000); err == nil {
			t.Fatalf("subject %q was accepted", bad)
		}
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("refused writes left %d entries behind", n)
	}
}

// Filing against a merged-away subject follows the merge to the survivor.
// Filing against the merged-away row itself would produce an entry the boot
// directory can never mention, because that predicate hides merged rows.
func TestLoreCreateFilesAgainstTheSurvivorOfAMerge(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-old", "repo", "repo:oldname")
	t33Entity(t, d, "e-new", "repo", "repo:officraft")
	if _, err := d.wdb.Exec(`UPDATE entity SET merged_into = 'e-new' WHERE id = 'e-old'`); err != nil {
		t.Fatalf("merge: %v", err)
	}

	w := t33Write()
	w.Subjects = []string{"repo:oldname"}
	got := t33Create(t, d, w)
	if len(got.SubjectIDs) != 1 || got.SubjectIDs[0] != "e-new" {
		t.Fatalf("subject did not follow the merge: %v", got.SubjectIDs)
	}
	if len(got.Minted) != 0 {
		t.Fatalf("a known subject was minted again: %+v", got.Minted)
	}
}

// A merge cycle is refused, not walked. Nothing in the schema forbids A→B→A.
func TestLoreCreateRefusesAMergeChainThatDoesNotEnd(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-a", "repo", "repo:a")
	t33Entity(t, d, "e-b", "repo", "repo:b")
	if _, err := d.wdb.Exec(
		`UPDATE entity SET merged_into = CASE id WHEN 'e-a' THEN 'e-b' ELSE 'e-a' END`); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	w := t33Write()
	w.Subjects = []string{"repo:a"}
	if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, ErrLoreEntityMergeCycle) {
		t.Fatalf("merge cycle: got %v", err)
	}
}

// An alias resolves onto the same subject, and the duplicate files ONE row.
func TestLoreCreateResolvesAliasesAndDeduplicatesSubjects(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	if _, err := d.wdb.Exec(
		`INSERT INTO entity_alias (alias, entity_id) VALUES ('repo:oc', 'e-repo')`); err != nil {
		t.Fatalf("alias: %v", err)
	}
	w := t33Write()
	w.Subjects = []string{"repo:officraft", "repo:oc", "repo:officraft"}

	got := t33Create(t, d, w)
	if len(got.SubjectIDs) != 1 || got.SubjectIDs[0] != "e-repo" {
		t.Fatalf("subjects: got %v, want one e-repo", got.SubjectIDs)
	}
	subjects, err := d.ListLoreSubjects(got.EntryID)
	if err != nil {
		t.Fatalf("list subjects: %v", err)
	}
	if len(subjects) != 1 {
		t.Fatalf("filed %d subject rows for one subject", len(subjects))
	}
}

// Superseding re-statuses the old entry AND leaves a journal row. The pointer
// alone would say what replaced it and never who decided that, or when.
func TestLoreCreateSupersedesTheEntryItReplacesAndJournalsIt(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	old := t33Create(t, d, t33Write())

	w := t33Write()
	w.Supersedes = old.EntryID
	got := t33Create(t, d, w)

	prev := t33Get(t, d, old.EntryID)
	if prev == nil || prev.Status != "superseded" {
		t.Fatalf("the replaced entry kept status %+v", prev)
	}
	events, err := d.ListLoreGovernanceEvents(old.EntryID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].Kind != LoreGovSupersede ||
		events[0].ReplacedBy != got.EntryID || events[0].ActorID != "ow-e27260b9ed05" {
		t.Fatalf("the supersede left no usable journal row: %+v", events)
	}
}

// Superseding an id that names nothing rolls the WHOLE write back. A pointer
// into empty space is a dead end that looks like a trail.
func TestLoreCreateRefusesToSupersedeAnEntryThatDoesNotExist(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	w := t33Write()
	w.Supersedes = "lore-nope"

	if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, ErrLoreEntryUnknown) {
		t.Fatalf("supersedes a ghost: got %v", err)
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("the refused write left %d entries behind", n)
	}
}

// 🔴 一條**已退休**的條目不准被 supersede，而這條規則不是為了整齊：supersede
// 會把舊那筆的狀態改成 `superseded`，而 `superseded` 是讀取端**照樣會回傳**的
// 狀態（dal_lore.go 自己寫明），`retired` 則是每一條讀取路徑都濾掉的。
//
// ⇒ 拿一條退休條目去 supersede，等於把它**弄回搜尋與每個人的開機目錄** ——
// 而那正是 ReviveLoreEntry 只有 owner 能按的原因（見
// TestLoreReviveIsOwnerOnlyAndBringsTheEntryBack）。任何一般 agent 用一次
// 普通寫入就能達成，而且 journal 記下的是 `supersede` 不是 `revive`，所以
// LatestLoreGovernanceEvent 事後連「它為什麼又活了」都答不出來。
//
// 2026-09-04 的陰性對照：把 CreateLoreEntry 的 `AND status <> 'retired'`
// 拆掉，整套 `go test ./...` **rc=0** —— 這道閘當時一支測試都沒有。
func TestLoreCreateWillNotSupersedeARetiredEntryBackIntoView(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	gone := t33Create(t, d, t33Write())
	if err := d.RetireLoreEntry(gone.EntryID, LoreRetireFalsified, "owner", "", true, 500); err != nil {
		t.Fatalf("retire: %v", err)
	}

	w := t33Write()
	w.Supersedes = gone.EntryID
	if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, ErrLoreEntryUnknown) {
		t.Fatalf("supersede a retired entry: got %v, want ErrLoreEntryUnknown", err)
	}
	// 🔴 最重要的一句：那條退休條目必須**還是** retired。它一旦變成
	// superseded 就重新讀得到，而開那道門只有 owner 有權。
	if got := t33Get(t, d, gone.EntryID); got == nil || got.Status != "retired" {
		t.Fatalf("一次普通寫入把退休條目弄回來了: %+v", got)
	}
	// 整筆寫入回滾，不是「舊那筆沒動、新那筆照樣進去」。
	if n := t33CountEntries(t, d); n != 1 {
		t.Fatalf("被退回的寫入留下了東西：現在共 %d 筆，只該剩那條退休的", n)
	}

	// 🔑 陽性對照：同一段碼在對象**還活著**時必須成功。少了這一半，上面的
	// 拒絕也可能只是因為 supersede 整條路壞掉了 —— 而那看起來一模一樣。
	alive := t33Create(t, d, t33Write())
	ok := t33Write()
	ok.Supersedes = alive.EntryID
	if _, err := d.CreateLoreEntry(ok, 1100); err != nil {
		t.Fatalf("陽性對照：supersede 一筆還活著的條目應該要成功: %v", err)
	}
	if got := t33Get(t, d, alive.EntryID); got == nil || got.Status != "superseded" {
		t.Fatalf("陽性對照：活著的那筆沒有被改成 superseded: %+v", got)
	}
}

// 🔴 這裡曾經有一支 TestLoreCreateAcceptsALongTriggerWholeRatherThanTrimmingIt，
// 它守的是「`heading`沒有長度上限，長的要整段寫進去不截斷」。它跟著 `trigger` 那一格
// 一起沒了（`rc-9002654dd81c`，2026-09-06「合併成 heading 一格」），而**它守的性質
// 真的消失了**：合併之後第一格就是 heading，而 heading 有 140 個 rune 的硬上限。
// ⚠️ 沒有留一支改寫成 heading 的替身，因為那會變成另一支測試：heading 的上限與
// 「拒絕而不是截斷」已經由 lore_heading_cap_t33_test.go 守著，在這裡再寫一次只會
// 讓兩份各自漂移。

// An entry with no subject is refused. It would exist, be counted, and be
// reachable by nothing — the boot directory is indexed by subject.
func TestLoreCreateRefusesAnEntryNobodyCouldEverFind(t *testing.T) {
	d := newTestDAL(t)
	w := t33Write()
	w.Subjects = nil
	if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, ErrLoreSubjectsEmpty) {
		t.Fatalf("no subject: got %v", err)
	}
}

// A blank origin and an unapproved origin prefix are both refused — the same
// rule PutLoreEntry enforces, asserted through THIS path so the composite
// cannot quietly stop applying it.
func TestLoreCreateEnforcesTheOriginRuleOnItsOwnPath(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	blank := t33Write()
	blank.Origin = ""
	if _, err := d.CreateLoreEntry(blank, 1000); !errors.Is(err, ErrLoreOriginBlank) {
		t.Fatalf("blank origin: got %v", err)
	}
	unknown := t33Write()
	unknown.Origin = "vendor:acme"
	if _, err := d.CreateLoreEntry(unknown, 1000); !errors.Is(err, ErrLoreOriginUnknownType) {
		t.Fatalf("unknown origin type: got %v", err)
	}
}

// 🔴 THIS TEST LOOKS LIKE A TAUTOLOGY TODAY AND THAT IS THE POINT.
//
// api_lore_read.go carries an early return that turns an unparsable revision id
// into a 404. Mutating that branch away leaves every test green — measured —
// because strconv.ParseInt answers 0 on failure and the scoped lookup then finds
// nothing, so the same 404 comes out of the other door. The guard is therefore
// not load-bearing, and it is only SAFE to say that because of ONE unstated
// premise: no revision ever carries id 0.
//
// That premise is what this test pins. Nothing else does: it is a property of
// `INTEGER PRIMARY KEY AUTOINCREMENT`, and a comment describing it would not
// raise its hand on the day somebody changes how revision ids are allocated.
//
// ⇒ The value of this test is not today, when it cannot fail. It is the day it
// DOES fail — because on that day the meaning of the failure is "the read
// route's parse guard has just started carrying load; go and test it properly".
func TestLoreRevisionIdsNeverStartAtZero(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	first := t33Create(t, d, t33Write())
	if first.RevisionID == 0 {
		t.Fatal("a revision was allocated id 0 — the read route's parse guard is now " +
			"load-bearing (a garbage id parses to 0 and would ADDRESS this row), and " +
			"api_lore_read.go says in as many words that it is not")
	}
	// Not merely non-zero: the ids come from AUTOINCREMENT, so the first one is
	// 1. Asserting the actual start makes a change of allocation scheme visible
	// rather than merely a change that happens to skip zero.
	if first.RevisionID != 1 {
		t.Fatalf("the first revision has id %d, want 1 — revision ids are no longer "+
			"plain AUTOINCREMENT, so anything reasoning about their values (see "+
			"api_lore_read.go) has to be re-read", first.RevisionID)
	}
	second := t33Create(t, d, t33Write())
	if second.RevisionID <= first.RevisionID {
		t.Fatalf("revision ids are not increasing: %d then %d", first.RevisionID, second.RevisionID)
	}
}

// 🔴 人／地／物的前綴：**非空時**才檢查，空著永遠放行。
//
// ⚠️ 這道檢查是實作判斷，不是負責人的裁定——規格只說了這三格「有 `human:` /
// `machine:` / `service:` 等前綴」。不檢查的話前綴就只是裝飾（`Seth` 跟
// `human:Seth` 都會進來），檢查得太兇又會把「查不出是誰」逼成「編一個人出來」。
// 這一條同時釘住兩邊：非空的壞前綴被拒，空的一律收。
func TestLoreEventKeyPrefixesAreCheckedOnlyWhenTheCellIsFilled(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	for _, tc := range []struct {
		name string
		ev   LoreEvent
		want error
	}{
		{"actor with no prefix", LoreEvent{HappenedTS: 1, What: "x", Actor: "Seth"}, ErrLoreEventKeyMalformed},
		{"place with an empty name", LoreEvent{HappenedTS: 1, What: "x", Place: "machine:"}, ErrLoreEventKeyMalformed},
		{"object with an unapproved type", LoreEvent{HappenedTS: 1, What: "x", Object: "vendor:acme"}, ErrLoreEventKeyUnknownType},
	} {
		w := t33Write()
		w.Events = []LoreEvent{tc.ev}
		if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}

	// 🔴 反面，而且它比上面重要：三格全空必須被接受。少了這一半，一個把選填
	// 偷偷變成必填的實作也會讓上面全綠——而那正是「查不出是誰」被逼成「編一個」
	// 的那條路。
	ok := t33Write()
	ok.Events = []LoreEvent{{HappenedTS: 1, What: "有人重開了前端"}}
	if _, err := d.CreateLoreEntry(ok, 1000); err != nil {
		t.Fatalf("人/地/物 全空的事件必須被接受: %v", err)
	}
}

// TestHeadingChangeMovesTheRevisionDigest is the OUTCOME guard for the hole the
// v8 pack found and closed: `heading` was writable through PutLoreEntry's upsert
// while loreRevisionBody left it out, so an entry could have its title replaced
// and still hash to the same revision — a proposal built on the old title would
// keep looking current to whoever was about to approve it.
//
// 🔴 IT ASSERTS THE OUTCOME, NOT A SPELLING. It changes ONE field through the
// same seam a future "edit this entry" route would reach for, then asks the
// renderer whether the digest moved. An AST scan naming PutLoreEntry would pin a
// spelling instead, and this tree already learned where that ends —— see the
// warning at the top of api_chat_attachment_wiring_test.go: 「Enumerating ways of
// writing something has no end… never treat its green as evidence.」
//
// ⚠️ WHAT IT DOES NOT COVER: `impact_stars` is in the same position — writable
// through the same upsert, absent from the body. It is left out on purpose, not
// missed: whether a star rating is part of the version a reviewer approved is
// the same undecided question as the heading was, and it is on the card. When
// that comes back, this test is where the answer lands.
func TestHeadingChangeMovesTheRevisionDigest(t *testing.T) {
	base := LoreEntry{
		ID:      "le-heading-digest",
		Heading: "遷移在沒有設定檔的情況下打到了正式庫",
		Content: "零參數等於 serve，serve 啟動就跑 migration。",
		Impact:  "14 張表進了正式庫。",
		Origin:  "agent:O-197",
	}
	before := loreRevisionBody(base, nil)

	renamed := base
	renamed.Heading = "在工作目錄裡跑執行檔會靜默套用遷移"
	after := loreRevisionBody(renamed, nil)

	if before == after {
		t.Fatalf("換掉標題之後，渲染出來的 body 一個位元組都沒變 —— "+
			"digest 不會動，一份基於舊標題的提案不會變 stale，而審核者看到的 "+
			"base digest 仍然「是最新的」。\nbody:\n%s", before)
	}
	if !strings.Contains(before, base.Heading) {
		t.Fatalf("body 裡找不到標題本身；這一支就不是在量標題有沒有進 body:\n%s", before)
	}
}
