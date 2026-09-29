package main

// Which documents split is declared per kind in bootDocRegistry (`Split`) and
// mirrored in bin/tests/fixtures/boot-doc-registry.tsv (`has_head`), read by
// the cockpit's mock.boot-doc-registry.test.ts and by
// conformance/test_rest_happy.py; nothing in Go reads it.
// A marker line inside the stored text (not a second field) because the owner
// must SEE the half he cannot edit. By owner ruling (2026-08-23) the head cannot
// be written back: the write face takes the body alone, the read face names the
// head, the server joins them. Head = what happened (program-generated, may
// carry {variables}); body = what to do next (owner-editable, kept literally).
// Where to cut is never guessed — each document needing it got its own ruling
// (rc-0c36d8739b8f, rc-812aa13fb165).

import "strings"

// One exact line so splitting is a byte comparison: a fuzzy marker would let
// the owner create a second boundary by accident.
// ⚠️ The same bytes are copied into frontend/src/api/docSplit.ts and the seeds/*.md
// that split — change them together.
const docBodyMarker = "<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->"

const docBodySep = "\n\n" + docBodyMarker + "\n\n"

func DocSplitHeadBody(text string) (head, body string, split bool) {
	return strings.Cut(text, docBodySep)
}

func DocJoinHeadBody(head, body string) string {
	return head + docBodySep + body
}

// join is per document and not cosmetic: 加速停止 and 任務收尾 staple the body
// on with a single "\n", the other task notices take a blank line; rendering
// them alike silently changes what an agent reads.
func DocRendered(text, join string) string {
	head, body, split := DocSplitHeadBody(text)
	if !split {
		// Lenient on purpose — do not tighten here: this function cannot know
		// whether the kind declared Split (eventNoticeText refuses for notices), and a
		// boot fold must still boot without its title-line head.
		// TestSystemInteractionText_AMarkerLessOverlayStillBoots pins it.
		return text
	}
	return head + join + body
}
