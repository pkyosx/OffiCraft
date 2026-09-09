package main

// doc_split.go — the read-only head / owner-editable body split SOME documents
// carry (T-3201, second package).
//
// ⚠️ T-6f44: this used to say "every event procedure carries". It no longer
// does, and the sentence outlived the fact: owner's decision 4 took the head
// off 系統互動, both 啟動步驟 and 〈停止〉. Which documents split is declared per
// kind in bootDocRegistry (`Split`) and mirrored in
// bin/tests/fixtures/boot-doc-registry.tsv (`has_head`), which server, cockpit
// and conformance all read — so there is one answer rather than a sentence
// here that has to be kept true by hand.
//
// 🔴 WHY A LINE IN THE DOCUMENT AND NOT A SECOND FIELD. The owner's ruling is
// that he must SEE the half he cannot edit — 「以前 global context 是固定內容我們
// 也是會顯示 只是不給改」 — and the thing he must see is the TEMPLATE, braces and
// all, because the whole reason this ticket exists is that he went looking for
// `restart_self` in 〈停止〉 and could not find it: the word lived in the Go
// line that wrapped the document, not in the document. A marker line inside the
// stored text puts both halves in the one textarea he already reads.
//
// ⚠️ THIS PARAGRAPH USED TO END 「and costs the wire contract nothing」, and that
// clause is now FALSE by ruling, not by drift. The owner's 2026-08-23 ruling —
// 「唯讀區應該無法回寫，讀取有這個 key，回寫沒有這個 key，沒有人有任何方式可以
// 回寫」— is precisely the decision to PAY that cost: the write face takes the
// body alone, the read face names the head, and the server joins them. The
// marker survives all of it because it is still the only thing that says where
// the head ENDS — in the seed, which is where the head now comes from.
//
// The split answers the owner's own criterion for where to cut, verbatim:
// 「有變數的部分通常就是說明發生什麼了，我們會需要修改的通常是接下來他應該採取
// 什麼步驟」. Head = what happened, program-generated, may carry {variables}.
// Body = what to do next, owner-editable, and preserved literally even when it
// contains brace-delimited examples or other text that resembles a variable.
//
// 🔴 "通常" IS NOT "ALWAYS", AND THREE DOCUMENTS PROVED IT — EACH ONE COST A
// RULING. task_closeout named {type_key} and {manual_label} in the MIDDLE of
// its instructions, and both takeover documents appended 「交接備註：{note}」
// AFTER theirs, so on all three no prefix cut left an unfilled variable in the
// shipped body. None
// of them was split by guessing: the takeover pair lost the {note} slot because
// the owner ruled the handover note belongs on the task alone
// (rc-0c36d8739b8f — the reassign already writes HandoverNote and the successor
// reads it with get_task), and task_closeout was reworded sentence by sentence
// under his approval (rc-812aa13fb165), moving both names up into the head.
// Guessing instead would have been the one failure mode nobody could see
// afterwards: a document that reads fine and no longer says what it said.

import "strings"

// docBodyMarker separates the two halves inside a stored document. It is an
// HTML comment so a Markdown renderer swallows it, and it is one exact line so
// splitting is a byte comparison rather than a heuristic — a fuzzy marker
// ("any line of dashes") would let an owner create a second boundary by
// accident and silently move the wall his edits are refused at.
const docBodyMarker = "<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->"

// docBodySep is the marker as it appears on disk: a blank line either side, so
// the two halves read as paragraphs in the editor.
const docBodySep = "\n\n" + docBodyMarker + "\n\n"

// DocSplitHeadBody cuts a stored document at the marker. split=false means the
// text carries no marker at all, which is a legitimate state (the kinds that
// cannot be split) and NOT an error — the caller decides whether the kind was
// supposed to have one.
func DocSplitHeadBody(text string) (head, body string, split bool) {
	return strings.Cut(text, docBodySep)
}

// DocJoinHeadBody writes the two halves back into the on-disk shape.
func DocJoinHeadBody(head, body string) string {
	return head + docBodySep + body
}

// DocRendered is what a READER gets: the marker line disappears and the two
// halves are joined with the separator this document's send site uses today.
//
// 🔴 join IS PER DOCUMENT AND IS NOT COSMETIC. The live send shapes really are
// four: buildBootContext joins its blocks with a blank line, the wind-down
// notices staple the body under the sentence with a single "\n", 轉派程序 runs
// head and body together inside ONE paragraph with nothing between them, and
// 解除阻擋 takes a blank line because its body is a bullet LIST rather than a
// sentence continuing the head. Rendering all of them the same way would change
// what an agent reads on three of the four — which is exactly the silent content
// change the verbatim test in api_bootdocs_split_t3201_test.go exists to catch.
func DocRendered(text, join string) string {
	head, body, split := DocSplitHeadBody(text)
	if !split {
		// 🔴 THE LENIENT BRANCH IS DELIBERATE — DO NOT TIGHTEN IT HERE. This
		// function takes a text and a join; it cannot know whether the KIND
		// declared Split, so it has no standing to call a missing marker a
		// fault. The strictness lives one layer up, in eventNoticeText, which
		// holds the spec: a NOTICE whose kind declares Split and whose stored
		// text has no marker is refused there, because a notice's read-only
		// head IS its sentence and the body alone is the instructions with the
		// facts sliced off.
		//
		// A BOOT FOLD is the opposite and must keep landing here: its head is
		// only the document's TITLE line, so a reader that gets the body
		// without it still boots. Refusing at this line would turn one stale
		// pre-marker overlay into agents that cannot start — far worse than the
		// hole the notice-side refusal closes. Pinned by
		// TestSystemInteractionText_AMarkerLessOverlayStillBoots; tightening
		// here reddens it.
		return text
	}
	return head + join + body
}
