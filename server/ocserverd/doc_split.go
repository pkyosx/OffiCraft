package main

// doc_split.go — the read-only head / owner-editable body split every event
// procedure carries (T-3201, second package).
//
// 🔴 WHY A LINE IN THE DOCUMENT AND NOT A SECOND FIELD. The owner's ruling is
// that he must SEE the half he cannot edit — 「以前 global context 是固定內容我們
// 也是會顯示 只是不給改」 — and the thing he must see is the TEMPLATE, braces and
// all, because the whole reason this ticket exists is that he went looking for
// `restart_self` in 〈下線程序〉 and could not find it: the word lived in the Go
// line that wrapped the document, not in the document. A marker line inside the
// stored text puts both halves in the one textarea he already reads, and costs
// the wire contract nothing.
//
// The split answers the owner's own criterion for where to cut, verbatim:
// 「有變數的部分通常就是說明發生什麼了，我們會需要修改的通常是接下來他應該採取
// 什麼步驟」. Head = what happened, program-generated, may carry {variables}.
// Body = what to do next, owner-editable, zero variables.
//
// 🔴 "通常" IS NOT "ALWAYS", AND THREE DOCUMENTS PROVE IT. task_closeout names
// {type_key} and {manual_label} in the MIDDLE of its instructions, and both
// takeover documents append 「交接備註：{note}」 AFTER theirs. Splitting those at
// a prefix would move bytes, and moving bytes is a content change this package
// is not allowed to make. They are therefore left unsplit (Split=false) and the
// conflict is the owner's to rule on — see the registry comments on those three
// kinds. Guessing here would have been the one failure mode nobody could see
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
// 🔴 join IS PER DOCUMENT AND IS NOT COSMETIC. Today's three send shapes really
// are three: buildBootContext joins its blocks with a blank line, offboardNotice
// staples the document under its sentence with a single "\n", and the task
// notices run head and body together inside ONE paragraph with nothing between
// them. Rendering all of them the same way would change what every agent reads
// on at least two of the three — which is exactly the silent content change the
// verbatim test in api_bootdocs_split_t3201_test.go exists to catch.
func DocRendered(text, join string) string {
	head, body, split := DocSplitHeadBody(text)
	if !split {
		return text
	}
	return head + join + body
}

// DocBody is the owner-editable half alone. The offboard read site wants this
// one rather than DocRendered: offboardNotice already builds the head sentence
// in Go, so handing it the whole document would staple the head under itself.
func DocBody(text string) string {
	_, body, split := DocSplitHeadBody(text)
	if !split {
		return text
	}
	return body
}

// docHeadEditRefusal answers a write whose read-only half does not match the
// shipped one — including a write that dropped the marker entirely, which is
// the same offence spelled differently (a document with no boundary has no
// read-only half left).
//
// It says what the two halves ARE rather than pointing at a permission, for the
// reason bootDocReadOnlyRefusal does: no principal can edit the head, so
// sending the reader to look for a role to grant wastes the only sentence they
// get. And it says outright that nothing was written — the owner has a textarea
// full of work and needs to know whether to retype it.
func docHeadEditRefusal(docName string) string {
	return "the " + docName + " has a read-only head above the line `" + docBodyMarker +
		"` — it is the part the server fills in and shows you, and no caller may " +
		"change it or remove the line; edit only the text below it. Nothing was written."
}

// docBodyVarRefusal answers a write whose BODY names a variable.
//
// 🔴 THE BODY IS WHERE THE OWNER TYPES, AND THAT IS THE WHOLE ARGUMENT. A name
// in the head is filled by the code that sends the document; a name in the body
// is filled by nobody, and reaches an agent with the braces still in it. Zero
// variables below the line is what makes the editable half impossible to get
// wrong, so the refusal explains the rule rather than just naming the offender.
func docBodyVarRefusal(docName string, bad []string) string {
	return "the " + docName + " uses " + docVarNameList(bad) + " below the line `" +
		docBodyMarker + "` — the editable half carries no variables at all, because " +
		"nothing fills them there and they would reach an agent with the braces " +
		"still in them. Put facts that vary in the read-only head, or write them out. " +
		"Nothing was written."
}
