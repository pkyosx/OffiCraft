package main

// dal_lore_governance.go — T-33. Retiring an entry, reviving one, and the
// journal row every such act leaves behind.
//
// 🔴 WHY THIS IS A SEPARATE FILE FROM dal_lore.go: that file is the L1 seam and
// says so in its own header — "there is no delete in this file, and there is not
// meant to be". Retiring is not a delete, but it IS a governance act with an
// actor and a reason, and folding it in beside PutLoreEntry would make the
// write path look like one more column update. It is not: it is the only place
// in this tree where WHO is asking changes whether the write is allowed.
//
// 🔴 NO MIGRATION IS NEEDED FOR ANY OF THIS, AND THAT IS ON PURPOSE. `status`
// already accepts 'retired' (00081) and `lore_governance_event` already carries
// the four columns a retirement has to answer — who, when, why, replaced by
// what. The retirement REASON therefore lives in the journal, not on the entry:
// an entry can be retired, revived and retired again for a different reason, and
// a column would only ever remember the last one. See LatestLoreGovernanceEvent.

import (
	"database/sql"
	"errors"
	"fmt"
)

// The three retirement reasons. They are NOT three spellings of one thing —
// owner ruling 2026-08-31 (ta-c568dfd29844 D11):
//
//	expired    the situation changed; this entry no longer applies. IT MAY COME BACK.
//	merged     folded into another entry; the content still exists over there.
//	falsified  the entry's claim WAS NEVER TRUE. IT SHOULD NOT COME BACK.
//
// 🔴 THE DIFFERENCE IS LOAD-BEARING IN TWO DIRECTIONS AT ONCE — which is exactly
// why this is not decoration. It says whether the entry may return, AND it says
// who is allowed to file it (below).
const (
	LoreRetireExpired   = "expired"
	LoreRetireMerged    = "merged"
	LoreRetireFalsified = "falsified"
)

// Governance event kinds written by this file.
const (
	LoreGovRetire = "retire"
	LoreGovRevive = "revive"
)

var (
	ErrLoreRetireReasonUnknown = errors.New("lore: unknown retirement reason")
	ErrLoreRetireOwnerOnly     = errors.New("lore: only the owner may retire an entry as falsified")
	ErrLoreReviveOwnerOnly     = errors.New("lore: only the owner may revive a retired entry")
	ErrLoreEntryUnknown        = errors.New("lore: no entry carries that id")
	ErrLoreEntryNotRetired     = errors.New("lore: the entry is not retired")
	ErrLoreActorBlank          = errors.New("lore: the acting id is blank")
)

// loreRetireNeedsOwner answers "may an ordinary agent file this reason itself?"
// and it is the SINGLE place that question is answered.
//
// 🔴 THE SPLIT, AND WHY IT RUNS THIS WAY ROUND (owner ruling + one derivation,
// marked as such):
//
//   - 'falsified' says the entry was WRONG. That is a judgement about truth, and
//     the owner ruled that overturning a memory needs him. ✅ HIS RULING.
//   - 'expired' and 'merged' claim nothing about truth — they are tidying. If
//     even "this one went stale" had to wait for the owner, the tidying would
//     never happen and the store would only ever grow, which is the exact
//     opposite of 「精而非多」. ⚠️ THAT HALF IS A READING, NOT HIS WORDS.
//
// A reason nothing recognises is REFUSED rather than defaulted. Defaulting an
// unknown reason to the permissive side would let a typo ('falsifed') retire an
// entry as if it were merely stale, and nothing downstream could tell the
// difference afterwards — the journal would simply carry the typo.
func loreRetireNeedsOwner(reason string) (bool, error) {
	switch reason {
	case LoreRetireExpired, LoreRetireMerged:
		return false, nil
	case LoreRetireFalsified:
		return true, nil
	default:
		return false, fmt.Errorf("%w: %q", ErrLoreRetireReasonUnknown, reason)
	}
}

// LoreGovernanceEvent is one row of the journal — the answer to "who did what to
// this entry, when, and why".
type LoreGovernanceEvent struct {
	ID         int64
	Kind       string
	Target     string
	ActorID    string
	Reason     string
	ReplacedBy string
	CreatedTS  float64
}

// RetireLoreEntry stops an entry being retrieved and records why.
//
// `replacedBy` names the entry that takes over, and it is meaningful for
// 'merged' above all; it is stored as sent and never validated as an id,
// because the journal's job is to record what the actor said, not to re-derive
// it later.
//
// 🔴 THE STATUS CHANGE AND THE JOURNAL ROW ARE ONE TRANSACTION. Split, the
// failure mode is an entry that is retired with no recorded reason — and "we
// cannot tell why this stopped being used" is precisely the hole this ticket was
// opened to close (design §3.15.3, Kyle c-4242e10962d3).
//
// ⚠️ ALREADY-RETIRED IS NOT AN ERROR, IT IS A SECOND EVENT. Retiring a retired
// entry for a new reason is a real act — an entry parked as 'expired' can later
// be judged false — and the journal keeps both rows. What it must not do is
// silently overwrite the first reason, which is why the reason was never a
// column.
func (d *DAL) RetireLoreEntry(entryID, reason, actorID, replacedBy string, actorIsOwner bool, nowTS float64) error {
	if actorID == "" {
		return ErrLoreActorBlank
	}
	needsOwner, err := loreRetireNeedsOwner(reason)
	if err != nil {
		return err
	}
	if needsOwner && !actorIsOwner {
		return fmt.Errorf("%w (actor %q)", ErrLoreRetireOwnerOnly, actorID)
	}
	return d.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE lore_entry SET status = 'retired', updated_ts = ? WHERE id = ?`,
			nowTS, entryID)
		if err != nil {
			return err
		}
		// A zero-row UPDATE is how "no such entry" arrives here: SQLite does not
		// consider it an error, so it has to be turned into one, or a retire of a
		// typo'd id would answer 200 and leave nothing behind but a journal row
		// pointing at nothing.
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return fmt.Errorf("%w: %q", ErrLoreEntryUnknown, entryID)
		}
		return insertLoreGovernanceEvent(tx, LoreGovernanceEvent{
			Kind: LoreGovRetire, Target: entryID, ActorID: actorID,
			Reason: reason, ReplacedBy: replacedBy, CreatedTS: nowTS,
		})
	})
}

// ReviveLoreEntry brings a retired entry back into retrieval.
//
// 🔴 OWNER ONLY, AND THAT IS A DERIVATION, NOT HIS WORDS (design §3.15.3).
// Reviving asserts the entry holds after all — the same class of judgement as
// overturning one — so it sits on the same side of the line as 'falsified'.
// ⚠️ If that reading is wrong the cost is one-directional and cheap to fix: a
// revive that should have been an agent's is a message to the owner. The reverse
// (an agent quietly reviving something he retired as false) is not.
//
// 🔑 THIS FUNCTION IS THE REASON RETIREMENT MAY BE CALLED REVERSIBLE AT ALL.
// Until it existed, "retire is not a delete, you can bring it back" was a claim
// with nothing behind it, and the design says so in as many words. Anything that
// relies on retirement being reversible must therefore not ship ahead of this.
func (d *DAL) ReviveLoreEntry(entryID, actorID, reason string, actorIsOwner bool, nowTS float64) error {
	if actorID == "" {
		return ErrLoreActorBlank
	}
	if !actorIsOwner {
		return fmt.Errorf("%w (actor %q)", ErrLoreReviveOwnerOnly, actorID)
	}
	return d.inTx(func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRow(`SELECT status FROM lore_entry WHERE id = ?`, entryID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %q", ErrLoreEntryUnknown, entryID)
		}
		if err != nil {
			return err
		}
		// Reviving something that was never retired is refused rather than treated
		// as a no-op: it means the caller believes the entry is in a state it is
		// not, and answering "done" would confirm a belief that is wrong.
		if status != "retired" {
			return fmt.Errorf("%w: %q is %q", ErrLoreEntryNotRetired, entryID, status)
		}
		if _, err := tx.Exec(
			`UPDATE lore_entry SET status = 'active', updated_ts = ? WHERE id = ?`,
			nowTS, entryID); err != nil {
			return err
		}
		return insertLoreGovernanceEvent(tx, LoreGovernanceEvent{
			Kind: LoreGovRevive, Target: entryID, ActorID: actorID,
			Reason: reason, CreatedTS: nowTS,
		})
	})
}

func insertLoreGovernanceEvent(tx *sql.Tx, e LoreGovernanceEvent) error {
	_, err := tx.Exec(`
		INSERT INTO lore_governance_event
			(kind, target, actor_id, reason, replaced_by, created_ts)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.Kind, e.Target, e.ActorID, e.Reason, e.ReplacedBy, e.CreatedTS)
	return err
}

// ListLoreGovernanceEvents returns one entry's governance history, oldest first.
//
// Oldest-first and not newest-first because the sequence is the point: parked as
// stale, brought back, judged false reads as a story in that order and as a pile
// of rows in the other.
func (d *DAL) ListLoreGovernanceEvents(target string) ([]LoreGovernanceEvent, error) {
	rows, err := d.rdb.Query(`
		SELECT id, kind, target, actor_id, reason, replaced_by, created_ts
		FROM lore_governance_event WHERE target = ? ORDER BY id`, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoreGovernanceEvent
	for rows.Next() {
		var e LoreGovernanceEvent
		if err := rows.Scan(&e.ID, &e.Kind, &e.Target, &e.ActorID,
			&e.Reason, &e.ReplacedBy, &e.CreatedTS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LatestLoreGovernanceEvent answers "why is this entry in the state it is in"
// with the most recent act, or nil when nothing has ever been done to it.
//
// 🔴 THIS IS WHAT MAKES THE REASON RECOVERABLE WITHOUT A COLUMN ON THE ENTRY.
// The three reasons mean opposite things about whether the entry may return, so
// a reader that cannot get at the latest one is back where the ticket started:
// looking at something that stopped being used and having to investigate from
// scratch why.
func (d *DAL) LatestLoreGovernanceEvent(target string) (*LoreGovernanceEvent, error) {
	var e LoreGovernanceEvent
	err := d.rdb.QueryRow(`
		SELECT id, kind, target, actor_id, reason, replaced_by, created_ts
		FROM lore_governance_event WHERE target = ? ORDER BY id DESC LIMIT 1`,
		target).Scan(&e.ID, &e.Kind, &e.Target, &e.ActorID,
		&e.Reason, &e.ReplacedBy, &e.CreatedTS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}
