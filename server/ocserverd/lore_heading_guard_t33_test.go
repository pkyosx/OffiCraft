package main

// T-33 — the one line every member reads on every boot.
//
// This file exists because the other seven references to the heading all import
// loreSectionH1, so their wants move with the literal and none of them can see a
// rename. Everything below is written against the ASSEMBLED boot documents and
// spells the heading out by hand, so a rename has to survive a reader here.

import (
	"strings"
	"testing"
)

// loreHeadingAsShipped is the heading as members actually read it today,
// written out here on purpose and NOT imported from lore_fold.go.
//
// Importing the constant is what makes the other references blind: the want and
// the value are then the same object, so they agree no matter what it says.
// Spelled out, this line is a second opinion.
const loreHeadingAsShipped = "# 傳承：對象目錄（Lore — Subject Index）"

// loreHeadingSays is the sentence a failure prints. It is the whole point of
// this guard: red here is not "an expectation drifted", it is "the first line of
// a section in every member's boot document is about to change".
const loreHeadingSays = "你正在改變每一個成員（與每一個外包 worker）開機時讀到的那一行。" +
	"這不是一個期望值跑掉了——這是一次對外可見的改名，必須是刻意的、而且要有人裁定。" +
	"若這次改名確實是裁定過的，請連同 lore_fold.go 的常數與這裡的 loreHeadingAsShipped 一起改，" +
	"並在 commit 訊息裡寫出是誰、在哪一次裁定的。不要只把這裡的期望值對齊過去。"

// loreHeadingRequiredTokens are the two words the owner ruled on
// (rc-7864232a353e：中文叫「傳承」、英文前綴用 lore). They are asserted
// separately from the exact literal so that a rename which keeps the decided
// vocabulary and one which contradicts the ruling fail with different messages.
var loreHeadingRequiredTokens = []string{"傳承", "Lore"}

// loreHeadingRetiredTokens are names this section has already stopped using.
// 世界狀態記憶 / world_state_memory was the previous generation's explicitly
// provisional name, retired in d5342444. A heading that carries one of them
// again is not a rename, it is a revert of a decision.
var loreHeadingRetiredTokens = []string{
	"世界狀態記憶",
	"world state memory",
	"world_state_memory",
	"world-state-memory",
}

// loreHeadingInBootDoc digs the section's heading out of an assembled boot
// document, structurally, without knowing what the heading says.
//
// It finds a subject line the fixture is known to produce and walks back to the
// nearest H1 above it. That is deliberately the long way round: it means this
// test reads what the production assembler actually wrote into the document,
// so dropping the heading, or dropping the whole fold call from one of the two
// boot paths, is just as red as rewriting the literal.
func loreHeadingInBootDoc(t *testing.T, docName, doc string) string {
	t.Helper()
	const fixtureSubjectLine = "- agent:Kyle "
	lines := strings.Split(doc, "\n")
	subject := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, fixtureSubjectLine) {
			subject = i
			break
		}
	}
	if subject < 0 {
		t.Fatalf("%s 開機檔裡找不到目錄的內容行（%q）——這一段根本沒有進到這份文件，"+
			"下面所有關於標題的斷言都會是空的。%s",
			docName, fixtureSubjectLine, loreHeadingSays)
	}
	for i := subject; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "# ") {
			return lines[i]
		}
	}
	t.Fatalf("%s 開機檔裡目錄的內容行上面沒有任何 H1 標題——這一段是無頭的，"+
		"讀的人會把它接到上一段去。%s", docName, loreHeadingSays)
	return ""
}

// TestLoreHeadingEveryMemberReadsOnBoot pins the heading in the place it
// matters: the two documents buildBootContext and buildWorkerBootContext
// actually hand out.
//
// Four invariants, on purpose, because they fail differently and a reader needs
// to know which one they broke: it is the string that ships; it carries the
// vocabulary the owner ruled on; it carries no retired name; and both boot
// paths say the identical thing.
func TestLoreHeadingEveryMemberReadsOnBoot(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)

	bc, err := s.buildBootContext("", nil)
	if err != nil || bc == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	worker, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-heading", Runtime: RuntimeClaude}, Task{ID: "t-1"}, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}

	headings := map[string]string{}
	for _, doc := range []struct{ name, text string }{
		{"staff（成員）", bc.Context},
		{"outsource（外包 worker）", worker},
	} {
		h := loreHeadingInBootDoc(t, doc.name, doc.text)
		headings[doc.name] = h

		if h != loreHeadingAsShipped {
			t.Errorf("%s 開機檔的傳承段標題變了：\n  現在 = %q\n  原本 = %q\n%s",
				doc.name, h, loreHeadingAsShipped, loreHeadingSays)
		}
		for _, tok := range loreHeadingRequiredTokens {
			if !strings.Contains(h, tok) {
				t.Errorf("%s 開機檔的傳承段標題 %q 不含 %q。這兩個字是 owner 在 "+
					"rc-7864232a353e 裁定的名字（中文「傳承」、英文 lore）；"+
					"拿掉它等於把裁定推翻，那需要另一次裁定，不是一次改字。%s",
					doc.name, h, tok, loreHeadingSays)
			}
		}
		lower := strings.ToLower(h)
		for _, tok := range loreHeadingRetiredTokens {
			if strings.Contains(lower, strings.ToLower(tok)) {
				t.Errorf("%s 開機檔的傳承段標題 %q 用回了已經退役的名字 %q。"+
					"那個名字在 d5342444 被換掉，因為它是上一代自己講明「暫且叫做」的；"+
					"改回去是把一次定名倒轉。%s", doc.name, h, tok, loreHeadingSays)
			}
		}
	}

	// 🔴 THERE USED TO BE A FOURTH CHECK HERE — "the two boot paths print the
	// same heading" — AND IT IS GONE BECAUSE IT COULD NOT FAIL. Both documents
	// are assembled by ONE function reading ONE constant, so the two strings are
	// the same object's output twice; and the other way it could have differed —
	// one path not folding the section at all — dies earlier, in
	// loreHeadingInBootDoc's t.Fatalf, so the comparison is never reached.
	//
	// It was removed rather than annotated because a check that cannot produce a
	// different outcome is decoration, and decoration in a guard is worse than an
	// absent check: it reads, to the next person, as coverage that exists. That
	// is the exact failure this whole ticket is about. (Found in review by Kyle.)
	//
	// What actually holds the property: one assembler, one constant — structure,
	// not assertion. If a per-path axis ever comes back (§ the actorID parameter
	// that foldLoreSectionWithSurfacing still takes), this comparison becomes
	// meaningful and should be restored WITH a mutant proving it fails.
}
