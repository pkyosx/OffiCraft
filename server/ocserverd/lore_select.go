package main

// 🔴 THE ONLY PLACE THAT DECIDES WHICH LORE ENTRIES A READER GETS. Every exit
// goes through selectLoreEntries:
//   1. the STAFF boot document        — selectMemberLore,   assets.go buildBootContext
//   2. the OUTSOURCE boot document    — selectMemberLore,   worker_spawn.go buildWorkerBootContext
//   3. GET /api/task-manuals/{key}    — selectLoreForScope, api_taskmanuals.go writeTaskManual
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
	// Per scope_kind, so a member's own list can draw its line above its own
	// first dropped entry even when the overall first drop is an everyone entry.
	FirstDroppedByKind map[string]string

	UsedChars int

	CapChars int
}

// 🔴 CHARACTERS, NOT BYTES: entries are Chinese, so a byte-measured cap would
// silently admit about a third of what the owner set. The rendering scaffolding
// is deliberately not counted, so the settings number means what it says.
func loreEntryChars(e LoreEntry) int {
	return utf8.RuneCountInString(e.Title) + utf8.RuneCountInString(e.Body)
}

// The order (pinned first, then newest EFFECTIVE first) comes from
// ListLoreEntriesLive's ORDER BY; this function does not re-sort.
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
	return selectLoreEntries(entries, capChars), nil
}

// everyone scope first, then the member's own agent scope, under one budget —
// owner ruling T-236.
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
	return selectLoreEntries(append(everyone, own...), capChars), nil
}

func selectLoreEntries(entries []LoreEntry, capChars int) loreSelection {
	sel := loreSelection{Entries: []LoreEntry{}, CapChars: capChars,
		FirstDroppedByKind: map[string]string{}}
	stopped := false
	for _, e := range entries {
		if !stopped {
			cost := loreEntryChars(e)
			if capChars > 0 && sel.UsedChars+cost <= capChars {
				sel.UsedChars += cost
				sel.Entries = append(sel.Entries, e)
				continue
			}
			stopped = true
			sel.FirstDroppedID = e.ID
		}
		if _, seen := sel.FirstDroppedByKind[e.ScopeKind]; !seen {
			sel.FirstDroppedByKind[e.ScopeKind] = e.ID
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
