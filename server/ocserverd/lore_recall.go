package main

// lore_recall.go — T-33: the ONE writer of lore_recall_log, and the one place
// the session anchor is resolved.
//
// 🔴 WHAT THIS FILE EXISTS TO ANSWER, IN THE OWNER'S OWN WORDS:「agent 到底有沒
// 有用到我們寫的記憶？用了之後有沒有幫上忙？」 Before it, the journal had one
// writer — the boot fold — and it filed ONE row per wake for the whole subject
// directory. Every search, and every read of a single entry, wrote nothing at
// all. The table therefore recorded what was PUT IN FRONT of an agent and never
// what the agent went back and READ, which is the half the question is about.
//
// 🔴 EVERY RETRIEVAL IS ONE ROW. A search is a row, opening one entry is a row,
// reading one entry's original is a row. Not one row per session, not one row
// per entry with a counter — the journal's value is that it says what happened
// at a moment in time, and a counter throws away the moments.
//
// 🔴 AND EVERY ROW CARRIES ITS OWN SESSION ANCHOR. The reasoning is set out at
// length in migrations/00067; the short form is that member.session_boot_ts is a
// single cell the actor's next session overwrites, so an anchor that is not
// stamped INTO the row is an anchor that is gone by the time anybody asks. It is
// in hand at write time. It is one cell. There is no reason to defer it and one
// very good reason not to: the deferred version fails silently, rendering
// identically to 「那一次沒記到」.
//
// 🔴 NOTHING HERE DE-DUPLICATES, AND NOTHING MAY. The table is append-only by
// design — 「同一個成員開機兩次是兩個事件，不是同一個事件被看到兩次」 — and
// repeated reads are now the SIGNAL rather than noise: the same entry read twice
// inside one session says the short form is not carrying its weight, while the
// same entry read across two sessions says it is. Collapsing them would delete
// the distinction this whole column pair was added to make.
//
// ⚠️ THE OWNER'S OWN READS ARE JOURNALLED TOO, and that is not an oversight. One
// route serves the cockpit and the agents alike, `actor_id` already says which
// is which, and inventing a "don't log humans" rule here would be a policy
// nobody ruled on — filtering it at read time is a decision that can be changed,
// a row never written is not.

import (
	"encoding/json"
	"log"
)

// The `query` cell is a PATH MARKER — the DAL's own comment calls it "whatever
// the writing path calls itself", and loreRecallQueryBoot has meant exactly that
// since the journal existed. The caller's literal search text rides in
// `returned` beside the axes it was asked with, so this column keeps answering
// one question: which door filed this row.
//
// 🔴 THEY MUST STAY DISTINGUISHABLE FROM EACH OTHER AND FROM boot-fold. A
// directory nobody asked for and a deliberate lookup are very different events,
// and so are "I searched and got these" and "I opened this one" — the first is
// retrieval finding something, the second is somebody choosing it.
const (
	loreRecallQuerySearch       = "search"
	loreRecallQueryEntryRead    = "entry-read"
	loreRecallQueryRevisionRead = "revision-read"
)

// The three session states. See migrations/00067 for why 0 alone could not
// carry them: 'unrecorded' and 'unanchored' both have session_boot_ts == 0 and
// mean opposite things — nobody looked, versus somebody looked and there was
// none.
const (
	loreRecallSessionUnrecorded = "unrecorded"
	loreRecallSessionAnchored   = "anchored"
	loreRecallSessionUnanchored = "unanchored"
)

// loreRecallAnchorMode says how this row's anchor is to be established.
//
// 🔴 IT IS A PARAMETER RATHER THAN AN ALWAYS-RESOLVE, BECAUSE ALWAYS-RESOLVE IS
// WRONG ON THE BOOT PATH AND WRONG QUIETLY. recordLoreSurfacing runs at the
// dispatch of a START — and in reconcileOne it runs the line BEFORE
// clearSessionBootTS. Asking the roster there returns the OUTGOING session's
// anchor, which would be filed as this row's own: a number that looks perfectly
// plausible and belongs to a session that had already ended. The boot fold's
// honest answer is that the session it was assembled for has not connected yet
// and therefore has no anchor at all.
type loreRecallAnchorMode int

const (
	// loreAnchorFromRoster — a retrieval by somebody who is already running:
	// take the anchor off their member row as it stands right now.
	loreAnchorFromRoster loreRecallAnchorMode = iota
	// loreAnchorSessionNotStartedYet — a boot fold. The document is on its way
	// to a session that will stamp its anchor on its first connect, after this
	// row is written.
	loreAnchorSessionNotStartedYet
)

// loreSessionAnchor resolves the anchor for one actor.
//
// A member the roster does not know (the owner's token sub, a warden, an id from
// a previous life) is 'unanchored' rather than 'unrecorded': we looked, and
// there was nothing. That distinction is the entire reason the state column
// exists.
func (s *apiServer) loreSessionAnchor(actorID string) (float64, string) {
	if actorID == "" {
		return 0, loreRecallSessionUnanchored
	}
	m, err := s.dal.GetMember(actorID)
	if err != nil || m == nil || m.SessionBootTS <= 0 {
		return 0, loreRecallSessionUnanchored
	}
	return m.SessionBootTS, loreRecallSessionAnchored
}

// recordLoreRecall files ONE row, and every path that files one goes through
// here — the boot fold included.
//
// 🔴 FAIL-OPEN, the same rule the boot fold already followed: a journal write
// that fails is logged and swallowed. Serving an agent the lore it asked for
// matters; recording that we did is bookkeeping, and a station that answers 500
// on a read because a log table is unhappy has traded the thing for the record
// of the thing.
func (s *apiServer) recordLoreRecall(r LoreRecall, mode loreRecallAnchorMode) {
	switch mode {
	case loreAnchorSessionNotStartedYet:
		r.SessionBootTS, r.SessionState = 0, loreRecallSessionUnanchored
	default:
		r.SessionBootTS, r.SessionState = s.loreSessionAnchor(r.ActorID)
	}
	if r.CreatedTS == 0 {
		r.CreatedTS = nowSecs()
	}
	if err := s.dal.InsertLoreRecall(r); err != nil {
		log.Printf("[lore] recall journal: recording %q for %q failed: %v — "+
			"serving anyway", r.Query, r.ActorID, err)
	}
}

// loreRecallReturned is the `returned` payload for a RETRIEVAL row: which
// entries came back, and what was asked to get them.
//
// 🔴 THE ENTRY IDS ARE THE POINT — 「撈到哪幾條」. A count would say an agent
// retrieved four things and leave nobody able to ask whether any particular
// entry is ever used, which is the question the whole governance side of this
// feature is built on.
//
// The asked-for axes ride along because a hit list without them cannot be
// interpreted: the same four ids mean something different when they came back
// from a bare "everything" than from a named subject.
type loreRecallReturned struct {
	Entries []string `json:"entries"`
	// Query is the caller's LITERAL search text, empty on the read paths. It is
	// here and not in the `query` column because that column names the door.
	Query   string   `json:"query,omitempty"`
	Subject string   `json:"subject,omitempty"`
	Actions []string `json:"actions,omitempty"`
	// Total and Truncated say whether the ids listed are the whole answer. A
	// truncated retrieval that reads as complete would make an entry look
	// unreached when it was merely cut off.
	Total     int  `json:"total,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
}

// encodeLoreRecallReturned renders the payload, or "" when it cannot — a row
// with an unreadable `returned` is worse than the encoding error, so the caller
// files the row either way and the cells that did encode still count.
func encodeLoreRecallReturned(p loreRecallReturned) string {
	if p.Entries == nil {
		p.Entries = []string{}
	}
	b, err := json.Marshal(p)
	if err != nil {
		log.Printf("[lore] recall journal: encoding the returned set failed: %v", err)
		return ""
	}
	return string(b)
}
