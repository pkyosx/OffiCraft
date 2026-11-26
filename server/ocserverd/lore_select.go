package main

// 🔴 THE ONLY PLACE THAT DECIDES WHICH LORE ENTRIES A READER GETS. Every exit
// goes through selectLoreEntries:
//   1. the STAFF boot document        — selectMemberLore,   assets.go buildBootContext
//   2. the OUTSOURCE boot document    — selectMemberLore,   worker_spawn.go buildWorkerBootContext
//   3. GET /api/task-manuals/{key}    — selectLoreForScope, api_taskmanuals.go writeTaskManual
//   4. the cockpit's numbers, GET /api/lore and GET /api/lore/budgets — both, api_lore.go
//
// Exit 1 is conditional: /api/bootstrap with only a role calls buildBootContext
// with no member, and then it emits no 傳承 block.
//
// A second implementation is forbidden: a divergence shows up as "the entry I
// wrote is in the manual but not in my boot doc", and every face looks correct
// on its own. A new exit calls this function and joins the list above.

import (
	"strings"
	"unicode/utf8"
)

type loreLister interface {
	ListLoreEntriesLive(scopeKind, scopeKey string) ([]LoreEntry, error)
}

type loreSelection struct {
	Entries []LoreEntry
	// The cockpit draws its 上限線 immediately above this entry.
	FirstDroppedID string
	// One per scope kind, listed in the order of selectLoreEntries' walk
	// argument (a member: everyone, then agent) — not the order the entries
	// interleave in — so a member's own list can draw its line above its own
	// first dropped entry even when the overall first drop is an everyone entry.
	Segments []loreSegment

	UsedChars int

	CapChars int
}

type loreSegment struct {
	ScopeKind   string
	EntryCount  int
	Chars       int
	FittedCount int
	FittedChars int

	FirstDroppedID    string
	FirstDroppedChars int
}

func (sel loreSelection) segment(scopeKind string) loreSegment {
	for _, seg := range sel.Segments {
		if seg.ScopeKind == scopeKind {
			return seg
		}
	}
	return loreSegment{ScopeKind: scopeKind}
}

func (sel loreSelection) firstDropped() loreSegment {
	for _, seg := range sel.Segments {
		if seg.FirstDroppedID != "" && seg.FirstDroppedID == sel.FirstDroppedID {
			return seg
		}
	}
	return loreSegment{}
}

func (sel loreSelection) entryCount() int {
	n := 0
	for _, seg := range sel.Segments {
		n += seg.EntryCount
	}
	return n
}

func (sel loreSelection) droppedCount() int {
	n := 0
	for _, seg := range sel.Segments {
		n += seg.EntryCount - seg.FittedCount
	}
	return n
}

// 🔴 CHARACTERS, NOT BYTES: entries are Chinese, so a byte-measured cap would
// silently admit about a third of what the owner set. The rendering scaffolding
// is deliberately not counted, so the settings number means what it says.
func loreEntryChars(e LoreEntry) int {
	return utf8.RuneCountInString(e.Title) + utf8.RuneCountInString(e.Body)
}

// The order (pinned first, then newest EFFECTIVE first) comes from
// ListLoreEntriesLive's ORDER BY; this function does not re-sort. A member's
// two scopes are merged by the same rule in selectMemberLore.
//
// 🔴 The walk STOPS at the first entry that does not fit; it does not skip it and
// look for smaller ones. Everything below is OLDER, and a non-contiguous loaded
// set leaves the cockpit's 上限線 no honest place to be drawn.
func selectLoreForScope(lister loreLister, scopeKind, scopeKey string, capChars int) (loreSelection, error) {
	if lister == nil || capChars <= 0 || (scopeKey == "" && scopeKind != LoreScopeEveryone) {
		// capChars <= 0 is "no room", not "unlimited" — a mis-loaded setting must
		// not become an unbounded boot document.
		return selectLoreEntries(nil, capChars), nil
	}
	entries, err := lister.ListLoreEntriesLive(scopeKind, scopeKey)
	if err != nil {
		return loreSelection{Entries: []LoreEntry{}, CapChars: capChars}, err
	}
	return selectLoreEntries(entries, capChars, scopeKind), nil
}

// A member's boot document carries the everyone scope and the member's own
// agent scope as ONE list in live order: an own pinned entry rides ahead of an
// unpinned everyone entry, and a newer own entry ahead of an older everyone one.
// 🔴 Owner ruling: neither scope goes first as a block; do not put everyone
// entries back in front.
func selectMemberLore(lister loreLister, memberID string, capChars int) (loreSelection, error) {
	if lister == nil || memberID == "" || capChars <= 0 {
		return selectLoreEntries(nil, capChars), nil
	}
	everyone, err := lister.ListLoreEntriesLive(LoreScopeEveryone, "")
	if err != nil {
		return loreSelection{Entries: []LoreEntry{}, CapChars: capChars}, err
	}
	own, err := lister.ListLoreEntriesLive(LoreScopeAgent, memberID)
	if err != nil {
		return loreSelection{Entries: []LoreEntry{}, CapChars: capChars}, err
	}
	return selectLoreEntries(mergeLoreLive(everyone, own), capChars, LoreScopeEveryone, LoreScopeAgent), nil
}

// loreLiveBefore is ListLoreEntriesLive's ORDER BY (dal_lore.go) for entries
// that come from two scopes.
//
// 🔴 CHANGE THIS AND BOTH SQL ORDER BYs TOGETHER: if they drift apart, a
// member's boot document interleaves its two scopes by a different rule than
// each scope is sorted by, or than the cockpit lists them in, and every list
// still looks plausibly ordered on its own.
func loreLiveBefore(a, b LoreEntry) bool {
	if ap, bp := a.State == LoreStatePinned, b.State == LoreStatePinned; ap != bp {
		return ap
	}
	if a.EffectiveTS != b.EffectiveTS {
		return a.EffectiveTS > b.EffectiveTS
	}
	return a.Seq > b.Seq
}

// mergeLoreLive merges two lists that are each already in live order; it keeps
// each list's own order, so a scope's fitted entries stay a prefix of that
// scope and its first dropped entry still has an honest place for the line.
func mergeLoreLive(a, b []LoreEntry) []LoreEntry {
	out := make([]LoreEntry, 0, len(a)+len(b))
	for len(a) > 0 && len(b) > 0 {
		if loreLiveBefore(b[0], a[0]) {
			out, b = append(out, b[0]), b[1:]
		} else {
			out, a = append(out, a[0]), a[1:]
		}
	}
	return append(append(out, a...), b...)
}

// walk names the scope kinds the segments are reported in, so a scope with no
// live entries still answers its (zero) segment.
func selectLoreEntries(entries []LoreEntry, capChars int, walk ...string) loreSelection {
	sel := loreSelection{Entries: []LoreEntry{}, CapChars: capChars, Segments: []loreSegment{}}
	segOf := map[string]int{}
	for _, kind := range walk {
		segOf[kind] = len(sel.Segments)
		sel.Segments = append(sel.Segments, loreSegment{ScopeKind: kind})
	}
	stopped := false
	for _, e := range entries {
		i, ok := segOf[e.ScopeKind]
		if !ok {
			i = len(sel.Segments)
			segOf[e.ScopeKind] = i
			sel.Segments = append(sel.Segments, loreSegment{ScopeKind: e.ScopeKind})
		}
		seg := &sel.Segments[i]
		cost := loreEntryChars(e)
		seg.EntryCount++
		seg.Chars += cost
		if !stopped {
			if capChars > 0 && sel.UsedChars+cost <= capChars {
				sel.UsedChars += cost
				sel.Entries = append(sel.Entries, e)
				seg.FittedCount++
				seg.FittedChars += cost
				continue
			}
			stopped = true
			sel.FirstDroppedID = e.ID
		}
		if seg.FirstDroppedID == "" {
			seg.FirstDroppedID = e.ID
			seg.FirstDroppedChars = cost
		}
	}
	return sel
}

const loreBlockHeading = "# 傳承"

// 🔴 other has no label on purpose (owner ruling: the boot document does not say
// 其他), and its [工作原則] entries still carry that prefix in the title.
var loreTypeBootLabels = map[string]string{
	LoreTypeInstructionConflict:   "指示衝突",
	LoreTypeInstructionSupplement: "指示補充",
	LoreTypeOwnerDecision:         "Owner 決策",
	LoreTypeOwnerPreference:       "Owner 偏好",
}

// "" for an empty selection is load-bearing: both exits append this
// unconditionally, and an empty heading would claim there is no lore.
//
// 🔴 The entry's id is rendered: it is the only handle an agent has to retire or
// bump the entry.
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
		if label, ok := loreTypeBootLabels[e.LoreType]; ok {
			b.WriteString("[" + label + "] ")
		}
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
