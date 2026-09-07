package main

// lore_select.go — T-33 傳承（lore）: 挑條目的邏輯，只有這一份.
//
// 🔴 THIS FILE IS THE ONLY PLACE THAT DECIDES WHICH LORE ENTRIES A READER GETS.
// There are two exits — the staff boot document (scope_kind='role', assets.go
// buildBootContext) and GET /api/task-manuals/{type_key} (scope_kind='manual',
// api_taskmanuals.go writeTaskManual) — and BOTH call selectLoreForScope. A
// second implementation is forbidden, and not as a style preference: the two
// exits are read by different audiences at different moments, so a divergence
// between them shows up as "the entry I wrote is in the manual but not in my
// boot doc", which nobody can debug from the outside because both faces look
// correct on their own. The rule is one function, two call sites.
//
// If you are here to add a THIRD exit: call this function. If you are here
// because this rule is inconvenient, the thing to change is this function, not
// your call site.

import (
	"strings"
	"unicode/utf8"
)

// loreLister is the read this selection needs and nothing more. It is an
// interface rather than *DAL so the rule can be tested against a handful of
// in-memory entries without a database — and, more to the point, so a test can
// feed it an ordering that a real query would not produce and check that the
// stop rule still holds.
type loreLister interface {
	ListLoreEntriesLive(scopeKind, scopeKey string) ([]LoreEntry, error)
}

// loreSelection is what one exit is handed: the entries that fit, and where the
// line falls.
type loreSelection struct {
	// Entries are the chosen entries, in the order they are to be rendered.
	Entries []LoreEntry
	// FirstDroppedID is the id of the FIRST entry that did not fit, or "" when
	// every live entry fitted. The cockpit draws its named 上限線 immediately
	// above this entry; without it the line would have to be re-derived from a
	// count, and a count cannot say which row it falls above.
	FirstDroppedID string
	// UsedChars is the character total of Entries — the same unit the cap is
	// expressed in, so the two are comparable without re-measuring.
	UsedChars int
	// CapChars is the cap that was in force for THIS selection. Carried back so
	// a caller reporting "n of m characters" cannot report a different m from
	// the one the selection was actually made against.
	CapChars int
}

// loreEntryChars is the size ONE entry costs against the cap: its title plus
// its body, counted in Unicode CHARACTERS.
//
// 🔴 CHARACTERS, NOT BYTES. Every cap in this tree is expressed in runes
// (utf8.RuneCountInString, never len()), and here the difference is not
// academic: these entries are written in Chinese, where a byte count is roughly
// three times the character count, so a byte-measured cap would admit about a
// third of what the owner set it to and there would be no error anywhere —
// entries would simply stop appearing.
//
// The rendering scaffolding around an entry (the heading marker, the blank
// line) is deliberately NOT counted. The cap is a budget over what somebody
// WROTE; charging it for punctuation this file chose would make the number the
// owner sets in the settings page mean something different from what it says.
func loreEntryChars(e LoreEntry) int {
	return utf8.RuneCountInString(e.Title) + utf8.RuneCountInString(e.Body)
}

// selectLoreForScope is the whole rule, in one place (spec §2):
//
//  1. take the entries of this scope that are NOT retired;
//  2. in order: pinned first, then the rest, newest EFFECTIVE first within each
//     group (produced by ListLoreEntriesLive's ORDER BY — this function does not
//     re-sort, so there is one definition of the order, not two);
//  3. accumulate title+body characters. An entry that does not fit is NOT
//     truncated and NOT skipped over — the walk STOPS THERE;
//  4. report the entries taken and the id of the first one left out.
//
// 🔴 STEP 3 STOPS, IT DOES NOT KEEP TRYING. The tempting version — skip the
// entry that does not fit and carry on looking for smaller ones further down —
// is wrong twice. Everything below is OLDER, so what it buys is old material at
// the price of the newest thing that was left out; and it makes the loaded set
// non-contiguous, which means the cockpit's 上限線 no longer separates "in" from
// "out" and there is no honest place left to draw it. A reader would see an
// entry below the line that was in fact loaded.
//
// 🔴 AN ENTRY THAT IS LARGER THAN THE WHOLE CAP ends the selection at position
// zero rather than being truncated. Half a lesson is not a smaller lesson — it
// is a sentence that stops, and the reader cannot tell it was cut. The write
// face refuses over-cap titles and bodies (spec §5) so the entry-level caps make
// this reachable only by lowering a cap after the fact, which is exactly the
// case the owner said should affect nothing already stored.
func selectLoreForScope(lister loreLister, scopeKind, scopeKey string, capChars int) (loreSelection, error) {
	sel := loreSelection{Entries: []LoreEntry{}, CapChars: capChars}
	if lister == nil || scopeKey == "" || capChars <= 0 {
		// capChars <= 0 is "no room", not "unlimited". Reading a zero as
		// unbounded is how a mis-loaded setting turns into an unbounded boot
		// document, and the loader's floor means the value is never legitimately
		// zero anyway.
		return sel, nil
	}
	entries, err := lister.ListLoreEntriesLive(scopeKind, scopeKey)
	if err != nil {
		return sel, err
	}
	for _, e := range entries {
		cost := loreEntryChars(e)
		if sel.UsedChars+cost > capChars {
			sel.FirstDroppedID = e.ID
			break
		}
		sel.UsedChars += cost
		sel.Entries = append(sel.Entries, e)
	}
	return sel, nil
}

// loreBlockHeading is the title of the appended block. ONE string, used by both
// exits, so the two faces cannot end up calling the same thing two names.
const loreBlockHeading = "# 傳承"

// renderLoreBlock turns a selection into the text that gets appended at an exit,
// or "" when nothing was selected.
//
// "" for an empty selection is load-bearing: both exits append this
// unconditionally, and a heading with nothing under it reads as "the traditions
// for this role are empty" when the truth may be "the cap is zero" or "they are
// all retired". An absent section makes no claim at all.
//
// 🔴 The entry's id is rendered. It is what an agent has to quote back to retire
// or bump an entry (spec §5 takes an entry_id), and it is the only handle it is
// given — an entry it can read but cannot name is one it can never act on.
func renderLoreBlock(sel loreSelection) string {
	if len(sel.Entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(loreBlockHeading)
	for _, e := range sel.Entries {
		b.WriteString("\n\n## ")
		b.WriteString(e.ID)
		b.WriteString(" ")
		b.WriteString(strings.TrimSpace(e.Title))
		if e.State == LoreStatePinned {
			b.WriteString("（置頂）")
		}
		body := strings.TrimSpace(e.Body)
		if body != "" {
			b.WriteString("\n\n")
			b.WriteString(body)
		}
	}
	return b.String()
}

// stripTrailingLoreBlock removes 傳承 block(s) that were APPENDED to a document
// by renderLoreBlock and have since been written back into the document itself.
// It is idempotent, and it loops until nothing is left to strip so a document
// that already accumulated several copies heals in one write rather than one
// copy per write.
//
// 🔴 WHY THIS EXISTS AT ALL — the loop it closes. renderLoreBlock appends to
// what a reader is SERVED: the staff boot document's 長期筆記, and the
// `learnings` field of GET /api/task-manuals/{type_key}. The documented
// procedure an agent follows before editing either is 「更新前先讀取對應位置的
// 最新內容，合併同主題，然後只把改動的那幾段送回去」 — read the latest, merge,
// send it back. What it just read ENDS WITH the appended block, so the block
// goes back in as document text. The next read then appends a fresh block after
// the copy that is now stored, and the document grows by one block per cycle.
//
// ⚠️ NOTHING ERRORS ANYWHERE ALONG THAT PATH. The write succeeds, the read
// looks right, and the only symptom is a document that keeps getting longer
// until it eats its own cap.
//
// 🔴 THIS TREE HAS ALREADY HAD THIS BUG ONCE, in the other direction: the
// 長期筆記 title self-heal in assets.go exists because a generation wrote its
// own boot-context fragment back through replace_lessons, and the drift was
// measured at +38 characters. That is the same shape with a smaller payload.
// This function is the same posture as that self-heal — strip on the way in,
// loop until clean, never error — applied to the whole appended block.
//
// ⚠️ WHAT IT CANNOT DISTINGUISH. A document whose own last section is genuinely
// headed `# 傳承` loses that section. That is the same trade the title self-heal
// makes, and it is the right way round: the cost of stripping a real section is
// one heading a human can retype, and the cost of NOT stripping is an unbounded
// document nobody is told about.
// 🔴 A DOCUMENT WITH NO BLOCK COMES BACK BYTE-FOR-BYTE, trailing whitespace and
// all. This function sits on EVERY lessons / learnings write in the tree, so an
// unconditional TrimRight here would silently reformat every document anybody
// saves — measured: it turned 29 existing patch/restore tests red by removing a
// trailing newline that had always survived a round trip. Whitespace is only
// tidied at the SEAM this function actually cuts.
func stripTrailingLoreBlock(text string) string {
	out := text
	for {
		trimmed := strings.TrimRight(out, " \t\r\n")
		cut := -1
		if strings.HasPrefix(trimmed, loreBlockHeading) {
			cut = 0
		} else if i := strings.LastIndex(trimmed, "\n"+loreBlockHeading); i >= 0 {
			cut = i
		}
		if cut < 0 {
			return out
		}
		// The heading must END a line (or the text), or `# 傳承的由來` — a
		// different heading that merely starts with the same runes — would be
		// treated as the appended block and take the rest of the document with it.
		rest := trimmed[cut:]
		rest = strings.TrimPrefix(rest, "\n")
		rest = rest[len(loreBlockHeading):]
		if rest != "" && !strings.HasPrefix(rest, "\n") {
			return out
		}
		out = strings.TrimRight(trimmed[:cut], " \t\r\n")
	}
}
