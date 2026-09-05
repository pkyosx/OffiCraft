package main

// dal_lore_proposal_v8cells_t33_test.go — T-33. 提案帶得動 v8 的標題與星等，而這
// 一檔守的是「帶得動」到底是什麼意思：不是欄位存得下，是**核可之後條目與原文層
// 都跟著變**。
//
// 🔴 這一檔存在的理由是一次真的發生過的失效，不是假想的：渲染器先被加上 heading，
// 而 lore_proposal 那時候沒有那一欄 ⇒ 每一次核可都寫下一份宣稱「這條沒有標題」
// 的原文，而條目上的標題其實還在。owner 2026-09-05 於 rc-bbccbeb3d9e6 逐字裁
// 「任何修改都是提案的一環」之後，兩欄才補上。
//
// ⚠️ 這一檔刻意不重複測「摘要含不含這兩格」——那是 dal_lore_write_t33_test.go 的
// 事。這裡測的是**核可那一步**，也就是唯一會寫回條目的那條路。

import (
	"errors"
	"strings"
	"testing"
)

// 核可之後，條目上的標題與星等變成提案主張的那一組。
//
// 🔴 seed 與 proposal 的標題刻意不同，而那是這支測試的全部鑑別力：兩邊一樣的話，
// 一個「根本沒有寫回標題」的 UPDATE 會讀回來完全正確。
func TestAcceptingAProposalWritesTheHeadingAndStarsOntoTheEntry(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	before := t33Get(t, d, entryID)
	p := t33Propose(entryID)
	p.BaseSHA256 = sha
	if p.Heading == before.Heading {
		t.Fatal("fixture: 提案與 seed 的標題一樣，這支測試會對一個不寫回標題的實作說 OK")
	}
	if p.ImpactStars == before.ImpactStars {
		t.Fatal("fixture: 提案與 seed 的星等一樣，同上")
	}

	filed, err := d.CreateLoreProposal(p, 2000)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := d.ApplyLoreProposal(filed.ProposalID, "owner", 3000); err != nil {
		t.Fatalf("accept: %v", err)
	}

	after := t33Get(t, d, entryID)
	if after.Heading != p.Heading {
		t.Errorf("條目的標題沒有跟著核可走: got %q, want %q", after.Heading, p.Heading)
	}
	if after.ImpactStars != p.ImpactStars {
		t.Errorf("條目的星等沒有跟著核可走: got %d, want %d", after.ImpactStars, p.ImpactStars)
	}

	// 🔴 原文層也要對得上，而它是這一組裡**最容易靜默壞掉**的一半：條目更新了、
	// 原文層卻記著別的東西，兩者不會互相檢查，而原文層正是「agent 起疑時回去讀
	// 當初寫了什麼」的唯一來源（本票硬條件 4）。
	rev, err := d.LatestLoreRevision(entryID)
	if err != nil || rev == nil {
		t.Fatalf("latest revision: %+v %v", rev, err)
	}
	if !strings.Contains(rev.Body, "heading:\n"+p.Heading+"\n") {
		t.Errorf("原文層沒有記下核可後的標題:\n%s", rev.Body)
	}
	// 陰性對照：舊的標題不可以還留在最新那一版原文裡。少了這一句，一個「兩份都
	// 寫進去」的實作也會通過。
	if strings.Contains(rev.Body, before.Heading) {
		t.Errorf("最新那一版原文裡還有核可前的標題 %q:\n%s", before.Heading, rev.Body)
	}
}

// 一份標題空白的提案，在**核可**那一步被擋下來。
//
// 🔴 這一道不是形狀檢查的重複。形狀檢查只看得到走過它的提案，而在 heading 這一欄
// 存在**之前**送出的提案（試用站上就有 27 份）從來沒有經過那道門，讀回來的
// heading 是空字串。核可它們會把條目上好端端的標題清成空的 —— 用「沒有人送」
// 冒充「有人主張要清掉」。
//
// ⚠️ 它擋的是既有資料而不是使用者的輸入，所以它會永遠存在，不是過渡措施。
func TestAcceptingRefusesAProposalFiledBeforeTheHeadingCellExisted(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)
	before := t33Get(t, d, entryID)

	p := t33Propose(entryID)
	p.BaseSHA256 = sha
	filed, err := d.CreateLoreProposal(p, 2000)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	// 直接把那一列改成「這一欄存在之前送的」樣子。走 DAL 送不出這種提案 —— 那
	// 正是重點：這一列不是使用者打得出來的，它是**已經躺在資料庫裡**的。
	if _, err := d.wdb.Exec(
		`UPDATE lore_proposal SET heading = '' WHERE id = ?`, filed.ProposalID); err != nil {
		t.Fatalf("age the proposal back to before the cell existed: %v", err)
	}

	_, err = d.ApplyLoreProposal(filed.ProposalID, "owner", 3000)
	if !errors.Is(err, ErrLoreHeadingBlank) {
		t.Fatalf("一份沒有標題的提案被核可了（它會把條目的標題清成空的）: %v", err)
	}
	// 🔴 而且它必須是**什麼都沒做**地拒絕，不是拒絕在半路。少了這一句，一個先
	// UPDATE 再檢查的實作也會通過，而那時候標題已經沒了。
	after := t33Get(t, d, entryID)
	if after.Heading != before.Heading {
		t.Errorf("拒絕之後條目的標題還是被動到了: %q → %q", before.Heading, after.Heading)
	}
	if after.Content != before.Content {
		t.Errorf("拒絕之後條目的內容被動到了: %q → %q", before.Content, after.Content)
	}
}
