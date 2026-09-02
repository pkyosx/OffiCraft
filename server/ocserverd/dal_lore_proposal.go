package main

// dal_lore_proposal.go — T-33. 回饋與提案：一個 agent 讀到一條幫倒忙的傳承，
// 提出「應該長這樣」，而審核看到的差異就是會落地的東西.
//
// 🔴 WHY A WHOLE VERSION AND NOT A PATCH — this is the owner's ruling of
// 2026-09-02 and the reason the table is shaped the way it is. Verbatim: 「我覺得
// 讓 agent submit new full version 即可 / diff view 我們自己產出」. A patch keeps
// TWO artefacts around — what the proposer said he was changing, and what
// applying it actually produces — and the gap between them looks completely
// normal to a reviewer: plausible description, approve, something else lands.
// A whole version has no second artefact. The diff is computed from the exact
// bytes that would be written, so there is no intermediate version for the two
// to disagree about.
//
// 🔴 WHAT THIS FILE DOES NOT DO, said plainly because a reader will look for it:
// there is NO accept path here. Accepting or declining is 仲裁, a separate piece
// of work. What this file owes that work is the one fact it cannot reconstruct
// afterwards — WHICH VERSION the proposer was looking at — and it records it as
// a digest, checks it at submit time, and recomputes it on every read.
//
// 🔴 過期提案是跟 PR 一模一樣的坑, and it is the whole reason `base_sha256`
// exists. A proposal written on Monday, reviewed on Friday, with the entry
// rewritten underneath it on Wednesday: applying it discards Wednesday silently,
// and the result looks entirely correct. The digest is compared in two places on
// purpose —
//
//   * at SUBMIT (CreateLoreProposal): the proposer names the digest he read; a
//     mismatch is refused 409 rather than stored, because a proposal that was
//     already stale when it was filed can only ever mislead a reviewer.
//   * at READ (ListLoreProposals): every row is re-compared against the entry's
//     CURRENT latest revision, because the interesting case is the one that went
//     stale AFTER it was filed — submit-time checking alone cannot see it.
//
// One comparison would have been the wrong number in either direction: only at
// submit and Friday's reviewer is blind; only at read and a proposal can be
// filed against a version nobody is looking at any more.

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrLoreProposalKindUnknown  = errors.New("lore: the proposal `kind` is not `update` or `remove`")
	ErrLoreProposalFaultUnknown = errors.New("lore: the proposal `fault` is not `stale`, `never-true` or `misled`")
	ErrLoreProposalEncountered  = errors.New("lore: `encountered` is blank — say what you were doing when this entry reached you")
	ErrLoreProposalEvidence     = errors.New("lore: `evidence` is blank — say what you actually saw, not what you think")
	ErrLoreProposalBaseBlank    = errors.New("lore: `base_sha256` is blank — a proposal has to name the version it was written against")
	ErrLoreProposalRemoveBody   = errors.New("lore: a `remove` proposal carries body fields — a removal proposes no new version, and a version nobody will ever write is exactly the description/result gap this shape exists to remove")
	ErrLoreProposalNoChange     = errors.New("lore: the proposed version is identical to the one it was written against — there is nothing to review")
	ErrLoreProposalStale        = errors.New("lore: this entry changed while you were reviewing it")
	ErrLoreEntryNoOriginal      = errors.New("lore: the entry has no preserved original to propose against")
)

// loreProposalKinds / loreProposalFaults are the two closed sets, declared once
// here and mirrored by a CHECK constraint in 00069. The CHECK is the backstop;
// this is what produces an error a caller can read.
//
// 🔴 `fault` IS THE OWNER'S THREE, NOT lore_feedback's `shape`. He named them on
// 2026-09-02: 過時（當時對現在不對）／本來就錯／害他走錯路. They are three
// different repairs — a stale entry wants rewriting against today, an entry that
// was never true wants retiring with `falsified`, and one that MISLED wants its
// symptoms fixed so it stops being retrieved for situations it does not describe
// — so an undifferentiated 「這條不好」 tells a reviewer nothing about what to do.
var (
	loreProposalKinds  = map[string]bool{"update": true, "remove": true}
	loreProposalFaults = map[string]bool{"stale": true, "never-true": true, "misled": true}
)

// LoreProposal is one submitted proposal, as sent.
type LoreProposal struct {
	EntryID string
	Kind    string

	// BaseSHA256 is the digest of the revision the proposer actually read. It is
	// caller-supplied and that is the entire mechanism: a value the server filled
	// in for itself would always match and would prove nothing.
	BaseSHA256 string

	Encountered string
	Fault       string
	Evidence    string

	Label        string
	Symptoms     string
	Short        string
	Falsify      string
	Instance     string
	ResidualRisk string

	ActorID string
}

// LoreProposalResult is what the submission actually stored, read back off the
// rendering rather than echoed.
type LoreProposalResult struct {
	ProposalID     string
	BaseRevisionID int64
	BaseSHA256     string
	SHA256         string
}

// LoreProposalRow is one proposal as served, with `Stale` computed at read time
// against the entry as it stands NOW.
type LoreProposalRow struct {
	ID             string
	EntryID        string
	Kind           string
	BaseRevisionID int64
	BaseSHA256     string
	Encountered    string
	Fault          string
	Evidence       string
	Label          string
	Symptoms       string
	Short          string
	Falsify        string
	Instance       string
	ResidualRisk   string
	Body           string
	SHA256         string
	ActorID        string
	CreatedTS      float64

	// Stale: the entry's latest revision is no longer the one this proposal was
	// written against. NOT stored — a stored flag would be right on the day it
	// was written and wrong every day after, which is the second-truth failure
	// this whole ticket is about.
	Stale bool
}

// LoreProposalList is an entry's proposals together with the version they are
// all being compared against.
//
// 🔴 CurrentSHA256 TRAVELS WITH THE LIST rather than being left for the reader to
// fetch. `Stale` is a comparison, and a comparison served without the thing it
// compared against cannot be checked by whoever reads it.
type LoreProposalList struct {
	EntryID           string
	CurrentRevisionID int64
	CurrentSHA256     string
	Proposals         []LoreProposalRow
}

// loreProposalEntry renders a proposal's six fields as a LoreEntry so the
// SHARED renderer can digest them.
//
// 🔴 ONE RENDERER, AND THAT IS LOAD-BEARING. loreRevisionBody is what the L0
// journal stores and what `sha256` on a revision digests. A proposal rendered by
// a second, near-identical function would produce a digest that could not be
// compared with a revision's — and 「這份提案就是那一版」 would stop being an
// answerable question the moment the two drifted by one newline.
func loreProposalEntry(p LoreProposal) LoreEntry {
	return LoreEntry{
		Label:        p.Label,
		Symptoms:     p.Symptoms,
		Short:        p.Short,
		Falsify:      p.Falsify,
		Instance:     p.Instance,
		ResidualRisk: p.ResidualRisk,
	}
}

// loreProposalShapeError validates everything that can be decided WITHOUT
// looking at the entry.
//
// 🔴 SHAPE IS CHECKED BEFORE THE DIGEST, DELIBERATELY. The staleness refusal
// tells a proposer to go and rebuild his version on what is there now; sending
// him to do that on a body that would have been refused anyway wastes the trip.
// So a malformed proposal against a moved entry answers 422 (fix this), and a
// well-formed one answers 409 (rebase this).
func loreProposalShapeError(p LoreProposal) error {
	if p.ActorID == "" {
		return ErrLoreActorBlank
	}
	if !loreProposalKinds[p.Kind] {
		return fmt.Errorf("%w: %q", ErrLoreProposalKindUnknown, p.Kind)
	}
	if !loreProposalFaults[p.Fault] {
		return fmt.Errorf("%w: %q", ErrLoreProposalFaultUnknown, p.Fault)
	}
	if strings.TrimSpace(p.Encountered) == "" {
		return ErrLoreProposalEncountered
	}
	if strings.TrimSpace(p.Evidence) == "" {
		return ErrLoreProposalEvidence
	}
	if strings.TrimSpace(p.BaseSHA256) == "" {
		return ErrLoreProposalBaseBlank
	}
	if p.Kind == "remove" {
		// A removal proposes no new version. Carrying one would put a version on
		// the reviewer's screen that no accept path would ever write — the
		// description/result gap in miniature, inside the shape built to close it.
		for _, f := range []string{p.Label, p.Symptoms, p.Short, p.Falsify, p.Instance, p.ResidualRisk} {
			if strings.TrimSpace(f) != "" {
				return ErrLoreProposalRemoveBody
			}
		}
		return nil
	}
	// 🔴 AN `update` IS HELD TO THE SAME FIELD RULES AS A WRITE, and the errors
	// are the WRITE's errors rather than new ones. Accepting a proposal means
	// writing a version through the ordinary write path, so a proposal that path
	// would refuse is a proposal that can never be accepted — and it would sit in
	// the queue looking exactly like one that could.
	if strings.TrimSpace(p.Symptoms) == "" {
		return ErrLoreSymptomsBlank
	}
	if strings.TrimSpace(p.Short) == "" {
		return ErrLoreShortBlank
	}
	if strings.TrimSpace(p.Falsify) == "" {
		return ErrLoreFalsifyBlank
	}
	if strings.TrimSpace(p.Instance) == "" {
		return ErrLoreInstanceBlank
	}
	return loreLabelError(p.Label)
}

// CreateLoreProposal files one proposal against the version its author read.
//
// The staleness refusal is the reason this function is not a plain INSERT.
func (d *DAL) CreateLoreProposal(p LoreProposal, nowTS float64) (LoreProposalResult, error) {
	var out LoreProposalResult
	if err := loreProposalShapeError(p); err != nil {
		return out, err
	}

	entry, err := d.GetLoreEntry(p.EntryID)
	if err != nil {
		return out, err
	}
	if entry == nil {
		return out, fmt.Errorf("%w: %q", ErrLoreEntryUnknown, p.EntryID)
	}
	base, err := d.LatestLoreRevision(p.EntryID)
	if err != nil {
		return out, err
	}
	if base == nil {
		// An entry with no L0 row is the state CreateLoreEntry's transaction
		// exists to rule out. Proposing against it would mean proposing against
		// nothing, so it is refused rather than defaulted to an empty base.
		return out, fmt.Errorf("%w: %q", ErrLoreEntryNoOriginal, p.EntryID)
	}

	// 🔴 THE COMPARISON. Everything else in this file is arrangements around it.
	if p.BaseSHA256 != base.SHA256 {
		return out, fmt.Errorf(
			"%w: entry %s — you wrote this against %s, but it now stands at %s "+
				"(revision %d). Re-read the entry and rebuild your version on what "+
				"is there now; filing it against the older text would discard "+
				"whoever changed it, silently",
			ErrLoreProposalStale, p.EntryID, p.BaseSHA256, base.SHA256, base.ID)
	}

	var body, sum string
	if p.Kind == "update" {
		body = loreRevisionBody(loreProposalEntry(p))
		sum = loreSHA256(body)
		// The digests are comparable because ONE renderer produced both — see
		// loreProposalEntry. A proposal that changes nothing is refused rather
		// than stored: it costs a reviewer a read and can end in no change.
		if sum == base.SHA256 {
			return out, ErrLoreProposalNoChange
		}
	}

	out.ProposalID = "lp-" + newHexID(12)
	_, err = d.wdb.Exec(`
		INSERT INTO lore_proposal (
			id, entry_id, kind, base_revision_id, base_sha256,
			encountered, fault, evidence,
			label, symptoms, short, falsify, instance, residual_risk,
			body, sha256, actor_id, created_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		out.ProposalID, p.EntryID, p.Kind, base.ID, base.SHA256,
		p.Encountered, p.Fault, p.Evidence,
		p.Label, p.Symptoms, p.Short, p.Falsify, p.Instance, p.ResidualRisk,
		body, sum, p.ActorID, nowTS)
	if err != nil {
		return LoreProposalResult{}, err
	}
	out.BaseRevisionID = base.ID
	out.BaseSHA256 = base.SHA256
	out.SHA256 = sum
	return out, nil
}

// ListLoreProposals serves an entry's proposals, NEWEST FIRST, each marked with
// whether it still stands against the entry as it is now.
//
// Newest first because a proposer who rewrote his proposal against a newer
// version wants the newer one read; oldest-first would lead with the one he
// himself replaced.
//
// 🔴 ORDERED BY created_ts, NOT BY id. A proposal id is "lp-" + newHexID(12) —
// random hex, carrying no time at all — so `ORDER BY id DESC` returns an
// arbitrary order that LOOKS like an order. It is the failure this whole route
// is least able to notice: every row is present, every field is right, and the
// only thing wrong is which one the reviewer reads first. id stays as the
// tie-break so two proposals filed in the same second still come back in a
// stable order rather than whatever the scan happens to produce.
func (d *DAL) ListLoreProposals(entryID string) (LoreProposalList, error) {
	out := LoreProposalList{EntryID: entryID, Proposals: []LoreProposalRow{}}
	current, err := d.LatestLoreRevision(entryID)
	if err != nil {
		return out, err
	}
	if current == nil {
		return out, fmt.Errorf("%w: %q", ErrLoreEntryNoOriginal, entryID)
	}
	out.CurrentRevisionID = current.ID
	out.CurrentSHA256 = current.SHA256

	rows, err := d.rdb.Query(`
		SELECT id, entry_id, kind, base_revision_id, base_sha256,
		       encountered, fault, evidence,
		       label, symptoms, short, falsify, instance, residual_risk,
		       body, sha256, actor_id, created_ts
		FROM lore_proposal WHERE entry_id = ? ORDER BY created_ts DESC, id DESC`, entryID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p LoreProposalRow
		if err := rows.Scan(
			&p.ID, &p.EntryID, &p.Kind, &p.BaseRevisionID, &p.BaseSHA256,
			&p.Encountered, &p.Fault, &p.Evidence,
			&p.Label, &p.Symptoms, &p.Short, &p.Falsify, &p.Instance, &p.ResidualRisk,
			&p.Body, &p.SHA256, &p.ActorID, &p.CreatedTS,
		); err != nil {
			return LoreProposalList{}, err
		}
		// 🔴 COMPUTED HERE, EVERY TIME, AGAINST THE DIGEST — not against the
		// revision id. An id comparison would answer the same today and would
		// start lying the day anything writes a revision row whose text is
		// unchanged: the proposal would read as stale while the words it was
		// written against are still exactly what is there.
		p.Stale = p.BaseSHA256 != current.SHA256
		out.Proposals = append(out.Proposals, p)
	}
	if err := rows.Err(); err != nil {
		return LoreProposalList{}, err
	}
	return out, nil
}
