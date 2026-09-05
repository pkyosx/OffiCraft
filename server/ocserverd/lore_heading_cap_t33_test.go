package main

// lore_heading_cap_t33_test.go — T-33. 標題格的 140 字元硬上限。
//
// 🔴 負責人 2026-09-05 逐字：「我們標題規定 140 字元好了」。
//
// 🔴 這一檔的重點不是「有沒有上限」，是**上限是不是 140、而且數的是不是 rune**。
// 三個點，缺一個都會讓一個錯的實作全綠：
//
//	140 剛好      ⇒ 通過      少了它，「上限其實是 139」看不出來
//	141           ⇒ 被拒      少了它，「上限其實是 141」看不出來
//	141 個中文字  ⇒ 被拒      少了它，**用 len() 數位元組的實作會全綠**
//
// 🔴 第三個點是這一檔最重要的一支，而它的鑑別力可以量：141 個中文字是 141 個
// rune、423 個 byte。用 rune 數 ⇒ 141 > 140 ⇒ 拒絕（正確）；用 byte 數 ⇒
// 423 > 140 ⇒ 也拒絕。⚠️ 所以**「141 個中文字被拒」本身分不出兩種實作** ——
// 分得出來的是它的另一半：**140 個中文字必須通過**（rune 數 140 ⇒ 通過；byte
// 數 420 ⇒ 被拒）。這一支兩半都測，而那個「剛好」的中文案例就是抓 len() 的那根針。
//
// ⚠️ 這個上限在射程內，不是裝飾：實測 24 條照 v8 新格式重寫的標題最長 130 個
// rune，離上限只剩 10 個。

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// t33Heading 產一句剛好 n 個 rune 的標題。ASCII —— 這一支刻意不含中文，中文由
// 下面那支專門的測試負責。
func t33Heading(n int) string {
	s := strings.Repeat("a", n)
	if utf8.RuneCountInString(s) != n {
		panic("fixture: 這個字串不是 n 個 rune")
	}
	return s
}

// t33HeadingHan 產一句剛好 n 個**中文字**的標題。每個中文字在 UTF-8 是 3 個
// byte，所以 n 個字 = n 個 rune = 3n 個 byte，而那個差距正是要拿來分辨數法的。
func t33HeadingHan(n int) string {
	s := strings.Repeat("記", n)
	if utf8.RuneCountInString(s) != n {
		panic("fixture: 這個字串不是 n 個 rune")
	}
	if len(s) != 3*n {
		panic("fixture: 中文字不是 3 個 byte，這支測試的鑑別力沒了")
	}
	return s
}

// ── 點 ①：140 個 rune 剛好，通過 ────────────────────────────────────────────
//
// 🔴 這一支守的是「上限沒有被悄悄調鬆或調緊」的下半邊。只測 141 被拒的話，一個
// 把上限寫成 100（或把比較寫成 >=）的實作會全綠。
func TestLoreHeadingCapAcceptsExactly140Runes(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	w := t33Write()
	w.Heading = t33Heading(140)
	if utf8.RuneCountInString(w.Heading) != 140 {
		t.Fatalf("fixture 壞了：標題是 %d 個 rune，不是 140", utf8.RuneCountInString(w.Heading))
	}

	got, err := d.CreateLoreEntry(w, 1000)
	if err != nil {
		t.Fatalf("剛好 140 個字元的標題被擋下來了 —— 上限被寫成小於 140，"+
			"或比較用的是 >= 而不是 >: %v", err)
	}
	entry := t33Get(t, d, got.EntryID)
	if entry == nil {
		t.Fatal("寫入回報成功，但條目不在表裡")
	}
	// 🔴 存下來的標題必須是**整句**。這一句擋的是「拒絕」被實作成「截斷」：
	// 截斷會回報成功，而列表上那一條看起來仍然像寫完了。
	if entry.Heading != w.Heading {
		t.Errorf("標題被動過了（長度 %d → %d）—— 這道門是拒絕，不是裁切",
			utf8.RuneCountInString(w.Heading), utf8.RuneCountInString(entry.Heading))
	}
}

// ── 點 ②：141 個 rune 被拒，而且錯誤訊息指名是 heading 這一格 ───────────────
//
// 🔴 「指名是哪一格」不是禮貌，是這道門為什麼在 DAL 而不在 SQLite CHECK 的**唯一
// 理由**（00084 逐字寫過這個判斷）：CHECK 只會回一句 "CHECK constraint failed"，
// 說不出是哪一格。所以這支測試同時斷言錯誤值、訊息裡有 `heading`、有上限 140、
// 也有他實際送來的 141 —— 少任何一項，寫的人就得自己猜。
func TestLoreHeadingCapRefusesOneRuneOverAndNamesTheCell(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	w := t33Write()
	w.Heading = t33Heading(141)

	_, err := d.CreateLoreEntry(w, 1000)
	if !errors.Is(err, ErrLoreHeadingTooLong) {
		t.Fatalf("141 個字元的標題沒有被 ErrLoreHeadingTooLong 擋下來: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"heading", "140", "141"} {
		if !strings.Contains(msg, want) {
			t.Errorf("錯誤訊息裡沒有 %q —— 缺了它，寫入者不知道是哪一格／上限多少／"+
				"他送來的是多少。訊息是：%s", want, msg)
		}
	}
	// 🔴 是哪一格不能靠猜：訊息不可以只說「太長了」而讓人以為是 content 或 trigger。
	if strings.Contains(msg, "trigger") || strings.Contains(msg, "content") {
		t.Errorf("錯誤訊息同時提到了別的格，指名就失效了：%s", msg)
	}
	// 一筆被拒的寫入不留任何東西。
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("被拒的寫入留下了 %d 條條目", n)
	}
}

// ── 點 ③：純中文 —— 這一支是本檔最重要的守衛 ───────────────────────────────
//
// 🔴 它抓的是一個**只用英文測就永遠看不到**的實作錯誤：用 len() 數位元組。
// 140 個中文字 = 140 個 rune = 420 個 byte。
//   - utf8.RuneCountInString ⇒ 140 ≤ 140 ⇒ 通過（正確）
//   - len()                  ⇒ 420 > 140 ⇒ 被拒（錯，而且錯得很難查：寫的人
//     會看到「我明明只寫了 140 個字」）
//
// 141 個中文字兩種數法都會被拒，所以**上半邊（剛好 140 通過）才是鑑別力所在**。
// 兩半都在這一支裡，是因為它們是同一件事的兩面，拆開會讓人只跑到會綠的那一半。
func TestLoreHeadingCapRefusesOneRuneOverInChinese(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	// 上半邊：140 個中文字必須寫得進去。這是抓 len() 的那一針。
	ok := t33Write()
	ok.Heading = t33HeadingHan(140)
	if _, err := d.CreateLoreEntry(ok, 1000); err != nil {
		t.Fatalf("140 個中文字的標題被擋下來了。它是 140 個字元（rune）、420 個 "+
			"byte —— 這個拒絕的意思幾乎一定是實作用了 len() 數位元組，而不是 "+
			"utf8.RuneCountInString 數字元。錯誤：%v", err)
	}

	// 下半邊：141 個中文字必須被拒，而且是被上限這道門拒的。
	over := t33Write()
	over.Heading = t33HeadingHan(141)
	_, err := d.CreateLoreEntry(over, 2000)
	if !errors.Is(err, ErrLoreHeadingTooLong) {
		t.Fatalf("141 個中文字的標題沒有被 ErrLoreHeadingTooLong 擋下來: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "141") {
		t.Errorf("錯誤訊息報的長度不是 141 個字元 —— 它報的可能是位元組數（423）。"+
			"訊息是：%s", msg)
	}
	if n := t33CountEntries(t, d); n != 1 {
		t.Fatalf("表裡有 %d 條，應該只有那條 140 個中文字的", n)
	}
}

// ── 提案那條路 ──────────────────────────────────────────────────────────────
//
// 🔴 核可一份提案會把 heading 寫回條目（ApplyLoreProposal 的那支 UPDATE），
// 所以它就是一次寫入。只擋 POST /api/lore/entries 等於替這個上限留了一條繞得
// 過去的路，而繞過去之後條目上就會躺著一句超過上限的標題 —— 從外面看跟一句合法
// 的標題長得一模一樣。
//
// 兩個進入點都測：送出時的形狀檢查，以及**核可時**（後者擋的是已經躺在資料庫裡
// 的列，形狀檢查從來沒看過它們）。
func TestLoreHeadingCapAlsoClosesTheProposalRoute(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)
	before := t33Get(t, d, entryID)

	// ① 送出時：一份標題 141 個中文字的提案存不進去。
	over := t33Propose(entryID)
	over.BaseSHA256 = sha
	over.Heading = t33HeadingHan(141)
	if _, err := d.CreateLoreProposal(over, 2000); !errors.Is(err, ErrLoreHeadingTooLong) {
		t.Fatalf("一份標題超過上限的提案被存下來了（它會躺在佇列裡，看起來跟一份"+
			"核可得了的提案一模一樣）: %v", err)
	}

	// ② 核可時：一份**已經在資料庫裡**、標題超長的提案不可以被核可。走 DAL 送
	// 不出這種列，所以直接改那一欄 —— 那正是重點，這一列不是使用者打得出來的。
	ok := t33Propose(entryID)
	ok.BaseSHA256 = sha
	filed, err := d.CreateLoreProposal(ok, 2000)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := d.wdb.Exec(`UPDATE lore_proposal SET heading = ? WHERE id = ?`,
		t33HeadingHan(141), filed.ProposalID); err != nil {
		t.Fatalf("把那一列改成超長標題: %v", err)
	}
	if _, err := d.ApplyLoreProposal(filed.ProposalID, "owner", 3000); !errors.Is(err, ErrLoreHeadingTooLong) {
		t.Fatalf("核可把一句超過上限的標題寫回條目了: %v", err)
	}
	// 🔴 而且是**什麼都沒做**地拒絕。少了這一句，一個先 UPDATE 再檢查的實作
	// 也會通過，而那時候條目上的標題已經被換掉了。
	after := t33Get(t, d, entryID)
	if after.Heading != before.Heading {
		t.Errorf("拒絕之後條目的標題被動到了: %q → %q", before.Heading, after.Heading)
	}
	if after.Content != before.Content {
		t.Errorf("拒絕之後條目的內容被動到了: %q → %q", before.Content, after.Content)
	}
}
