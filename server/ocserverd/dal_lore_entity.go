package main

// dal_lore_entity.go — T-33. 對象審核: the queue of subject entities an agent
// minted, and the two acts that empty it — 核可 and 合併.
//
// 🔴 WHY THIS QUEUE EXISTS AT ALL, AND WHY IT IS NOT A GATE ON THE WRITE.
// loreResolveSubject mints any subject key it does not recognise and parks it
// `pending = 1`. That is deliberate and it is the ticket's own ruling: gating
// the write is what pushes an agent into forcing a near-miss key onto an
// existing subject (silent), or into not writing at all (the disease the ticket
// treats). The cost of an ungated mint is that a typo becomes part of the
// ontology — and `pending = 1` is where that cost was parked. Until this file
// existed the column was a queue nothing could read and nothing could clear.
//
// 🔴 WHAT AN UNWORKED QUEUE ACTUALLY COSTS, measured rather than feared:
// ListLoreSubjectRoster — the boot subject directory — filters `pending = 0`,
// so an entry filed ONLY against a pending subject is invisible to every
// agent's wake. The write reports success, the row exists, and no reader can
// reach it by subject. That is the same shape of silent loss the whole ticket
// is about, which is why these two acts are governance acts with a journal row
// rather than column updates.
//
// 🔴 NO MIGRATION, AND THAT IS NOT A COINCIDENCE. `entity.pending`,
// `entity.merged_into` and `entity_alias` already exist (00081) and are already
// READ on every write (loreResolveSubject) and every search (loreEntityIDForKey).
// Recording a merge through those columns therefore repairs the ontology for
// every path at once; a new table would have been a second answer to 「which
// subject is this really」, and the two would disagree the first time one reader
// was updated and the other was not.
//
// 🔴 THERE IS NO REJECT / 駁回 HERE, BY OMISSION ON PURPOSE. The owner ruled on
// approving and on merging (rc-139a5ab99a19) and on nothing else. What happens
// to a minted name nobody wants — dropped, retired, left parked forever — has
// never been decided, and shipping an exit for it would decide it by accident,
// in the direction that destroys rows. An unapproved name costs a line in this
// queue; a wrong erase path costs the thing it erased.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Governance event kinds written by this file. They are namespaced `entity-`
// because `lore_governance_event.target` is one column holding two KINDS of id
// — entry ids from the retire/revive/supersede acts, entity ids from these two
// — and a reader that cannot tell which it is holding cannot join either.
const (
	LoreGovEntityApprove = "entity-approve"
	LoreGovEntityMerge   = "entity-merge"
)

var (
	ErrLoreEntityUnknown       = errors.New("lore: no subject entity carries that id")
	ErrLoreEntityNotPending    = errors.New("lore: the subject entity is not awaiting review")
	ErrLoreEntityMergeSelf     = errors.New("lore: a subject entity cannot be merged into itself")
	ErrLoreEntityTargetPending = errors.New("lore: the merge target is itself awaiting review")
	ErrLoreEntityTargetMerged  = errors.New("lore: the merge target has itself been merged away")
)

// LorePendingEntryRef is ONE entry filed under a pending subject, reduced to
// what a reviewer can read on a queue row.
//
// 🔴 IT IS A REFERENCE, NOT AN ENTRY, AND THE NAME SAYS SO. It carries the id
// so the reviewer can open the real thing, 標題格 so he knows which one to
// open, and the status so he knows what he is counting. Adding `content` here
// would turn the queue into a second reader of the memory table — with its own
// truncation rule, its own ordering, and its own opportunity to disagree with
// the one that already exists.
//
// 🔴 這一行帶的是 `Heading`，以前帶的是 `Trigger`，而換掉它是 owner 2026-09-06
// 於 `rc-9002654dd81c` 逐字裁定的一部分：「待審畫面改顯示 heading」。修的不是
// 用詞：待審畫面顯示 trigger、列表顯示 heading、搜尋結果兩格都回 ⇒ **同一條記憶
// 在三個畫面上用不同的話代表自己**，而審核者比對的正是「這是不是我剛剛在列表上
// 看到的那一條」。
type LorePendingEntryRef struct {
	EntryID string
	// Heading 標題格 — 「發生了什麼」。上限 140 個 rune，見 loreHeadingMaxRunes。
	// 合併之後它同時是這條的名字、也是搜尋掃得到的那一軸。
	Heading string
	Status  string // 'active' | 'superseded' | 'underspecified' (never 'retired')
}

// LorePendingEntity is ONE line of the review queue.
//
// 🔴 THERE IS NO Display FIELD, AND ITS ABSENCE IS THE HONEST ANSWER. The
// column exists in 00081 and NOTHING in this tree writes it, so a `display` on
// this struct could only ever be "" — and an empty string reads as 「we looked
// and it has no name」 rather than 「no path can fill this yet」. Name is the name
// half of Canonical, split at read time: a stored second copy of a substring of
// the key is exactly the kind of duplicate truth this ticket keeps refusing.
type LorePendingEntity struct {
	ID        string
	Type      string
	Canonical string
	Name      string
	CreatedTS float64

	// Entries is how many lore entries are filed under this subject.
	//
	// 🔴 IT IS COUNTED, AND IT IS COUNTED WITH THE SAME PREDICATE AS EVERY OTHER
	// READER — `status <> 'retired'`, word for word what ListLoreEntriesBySubject,
	// CountLoreEntriesBySubject and ListLoreSubjectRoster use. A review number
	// that disagreed with what the subject actually serves after approval would
	// be worse than no number: a reviewer would approve a name on the strength
	// of a count, and find nothing behind it.
	Entries int

	// EntriesEver is the SAME count with the retired predicate REMOVED — every
	// entry ever filed under this subject, retired ones included.
	//
	// 🔴 IT EXISTS BECAUSE `entries: 0` HAD TWO MEANINGS AND ONE APPEARANCE, which
	// is the owner's own complaint about this screen (2026-09-04): 「為什麼核可的可
	// 見內容這麼少 我根本無從審核起」. A subject with zero RETRIEVABLE entries is
	// either of two completely different things:
	//
	//   - NEVER USED (Entries == 0 && EntriesEver == 0) — minted once and never
	//     written against again. That is the shape of a TYPO: the writer wrote
	//     `repo:offcraft`, saw it was wrong, and wrote `repo:officraft` the next
	//     time. ListPendingLoreEntities' own comment already calls this the single
	//     most interesting row in the queue — and until this field existed the
	//     queue could not tell a reviewer he was looking at it.
	//   - EMPTIED BY RETIREMENT (Entries == 0 && EntriesEver > 0) — the name was
	//     genuinely used and everything filed under it has since been retired.
	//     Nothing there says the name is wrong, and folding it away on the
	//     strength of a 0 would merge a real subject into another one.
	//
	// Two dispositions that rendered as one line. This is the number that splits
	// them.
	//
	// ⚠️ IT USED TO HAVE A SECOND READER: loreSuggestionFor fed on it. That rule
	// is gone (owner 2026-09-05, see the tombstone below), and this field stays
	// anyway — it was always evidence FOR THE REVIEWER first, and splitting those
	// two dispositions on the row is exactly the 「一眼就可以判斷的資訊」 half of
	// the ask that survived.
	//
	// 🔴 IT DELIBERATELY DOES NOT REPLACE `Entries`. `Entries` is what the subject
	// will actually SERVE after approval and stays counted with the boot
	// directory's predicate word for word. One number that meant either thing
	// depending on who read it is exactly what this pair exists to end.
	EntriesEver int

	// CreatedBy is the actor id loreResolveSubject stamped on the mint — WHO wrote
	// the entry whose subject list minted this name.
	//
	// 🔴 THE COLUMN HAS BEEN WRITTEN SINCE 00081 AND NOTHING SERVED IT. The review
	// question is 「is this name a typo」, and after the name itself the most useful
	// evidence is who minted it: a name minted by the agent that already owns the
	// neighbouring subjects reads very differently from one minted once by a caller
	// nothing else in the queue came from.
	// ⚠️ IT IS SERVED RAW — an actor id, not a resolved display name. Resolving it
	// here would be a second answer to 「who is m-…」 beside the member surfaces that
	// already own that question, and the two would disagree the first time one was
	// updated. ⚠️ It is "" for a row minted before the column carried anything, and
	// "" is served as "" rather than as an invented 「unknown」.
	CreatedBy string

	// EntryRefs is EVERY entry the count in `Entries` counted — same predicate,
	// same ordering, one line each: id, 標題格 (`Heading`), and status.
	//
	// 🔴 IT IS THE ANSWER TO 「我根本無從審核起」 AND `SampleShort` IS NOT. A single
	// 120-rune sample of the FIRST entry answers 「what is one of these about」; the
	// question a reviewer actually has is 「what is filed under this name」, and for
	// a subject with five entries the sample showed him one twenty-fifth of it with
	// no way to see the rest. 標題格 is the cell that was designed to be read
	// alone — 「發生了什麼」, capped at 140 runes so it fits on one line — so a
	// list of headings is the cheapest thing that answers the real question.
	// ⚠️ 這裡以前列的是 `Trigger`，而列表畫面列的是 `Heading` ⇒ 審核者在這裡看到
	// 的那一行，跟他在列表上看到的同一條記憶不是同一句話。owner 2026-09-06
	// (`rc-9002654dd81c`) 逐字「待審畫面改顯示 heading」把兩邊對齊了。
	//
	// 🔴 `Status` RIDES ALONG because the list is NOT all-active: retired rows are
	// excluded (that is the shared predicate), but `superseded` and
	// `underspecified` are returned by ListLoreEntriesBySubject on purpose, and a
	// reviewer counting 「三條」 that are all superseded is looking at a different
	// subject from one with three active entries.
	//
	// ⚠️ THE BODIES ARE NOT HERE. `Heading` plus `SampleShort` is what fits on a
	// queue row; whoever wants the text opens the entry. This is a review packet,
	// not a second reader.
	EntryRefs []LorePendingEntryRef

	// ── the 功課 the owner asked for (round 2, 2026-09-02) ──────────────────
	//
	// 🔴 `Suggestion` / `MergeTarget` STOOD HERE AND WERE REMOVED (owner ruling
	// 2026-09-05). They carried the mechanical rule's verdict — 「approve」,
	// 「merge into that id」, or empty. The owner's objection was to the RULE, not
	// to the packet: 「ai 會笨到產生大小寫不一樣的對象嗎」 — the case/width/`_-`
	// fold was solving a mistake the writers do not actually make, so the column
	// spent a queue slot telling him which button to press on evidence he could
	// read faster himself.
	//
	// 🔴 IT IS COMING BACK, DIFFERENTLY, AND THAT IS WHY THIS SAYS SO INSTEAD OF
	// VANISHING. The replacement he ruled for is 「請 AI 判一輪、人可以同意或回
	// comment 讓它重判」 — a judgement with a reason attached and a way to argue
	// back, not a fold. That is ANOTHER TICKET; this package only removes, so
	// that the queue is never carrying two answers to 「what should I press」 at
	// once. Whoever lands that ticket should read the removed rule in this file's
	// history before rebuilding it — the parts worth keeping are the refusal to
	// guess (empty was a real answer) and the named evidence, not the fold.
	//
	// ⚠️ `Similar` STAYS, AND IS THE POINT. 「像哪些既有名字，以及每一個為什麼像」
	// is the homework; 「你該按哪顆鈕」 is what left.
	Similar []LoreEntitySimilar
	// SampleShort is the FIRST entry's 第 2 格 (`content`), trimmed — a sample,
	// never the field.
	//
	// ⚠️ THE NAME IS LEFT OVER FROM 六格. The column it reads is `content`;
	// `short` no longer exists. It was NOT renamed in this round because
	// `sample_short` is on the wire and the cockpit reads it — renaming it is a
	// wire change that belongs with the frontend round, not a tidy-up smuggled
	// into this one. It is named here rather than quietly tolerated.
	SampleShort string
}

// ListPendingLoreEntities returns the whole review queue, oldest first.
//
// Oldest-first because the queue is worked in the order it filled, and because
// the oldest parked name is the one that has been unreachable the longest.
//
// 🔴 THE COUNT RIDES A CORRELATED SUBQUERY, NOT A JOIN. A join would drop a
// pending entity with ZERO entries — and that row is the single most
// interesting one in the queue: a subject key that was minted and never used is
// a typo the writer corrected on its next attempt, so the count that reveals it
// must be able to come back 0 rather than remove the line.
//
// 🔴 `merged_into = ”` IS IN THE PREDICATE ALONGSIDE `pending = 1`, the same
// pair ListLoreSubjectRoster uses. MergeLoreEntity below clears `pending` when
// it sets `merged_into`, so today the second clause changes nothing — it is
// here so that a future path which merges WITHOUT clearing pending cannot put a
// dead name back in front of a reviewer with nothing to signal it.
//
// 🔴 THE SAMPLE RIDES THE SAME STATEMENT, and it is ORDER BY … LIMIT 1 rather
// than a second round trip per row. It is the FIRST entry by the same ordering
// ListLoreEntriesBySubject uses, so 「the first one」 means one thing in this
// tree rather than two.
//
// 🔴 THE SAMPLE IS NO LONGER THE ONLY THING A REVIEWER SEES UNDER A NAME. It
// stays on the row (it is on the wire and the cockpit reads it), but EntryRefs
// beside it lists EVERY entry the count counted — see loreEntryRefsForSubject
// for why that reuses ListLoreEntriesBySubject and what the extra query costs.
//
// 🔴 COST, STATED: this was TWO statements — the queue, and the approved-subject
// list the similarity is computed against — plus O(pending × approved) fold and
// edit-distance work in Go. It is now 2 + N, the N being one
// ListLoreEntriesBySubject per pending row. That is affordable HERE and would
// not be on a boot path: this route is an admin console's work queue, driven by
// a person who has sat down to review, not by every agent on every wake. It is
// deliberately NOT wired into ListLoreSubjectRoster for that reason, and the
// extra N does not change which side of that line it is on.
func (d *DAL) ListPendingLoreEntities() ([]LorePendingEntity, error) {
	rows, err := d.rdb.Query(`
		SELECT n.id, n.type, n.canonical, n.created_ts, n.created_by,
		       (SELECT COUNT(*) FROM lore_entry e
		         JOIN lore_subject s ON s.entry_id = e.id
		        WHERE s.entity_id = n.id AND e.status <> 'retired'),
		       (SELECT COUNT(*) FROM lore_entry e
		         JOIN lore_subject s ON s.entry_id = e.id
		        WHERE s.entity_id = n.id),
		       (SELECT e.content FROM lore_entry e
		         JOIN lore_subject s ON s.entry_id = e.id
		        WHERE s.entity_id = n.id AND e.status <> 'retired'
		        ORDER BY e.created_ts, e.id LIMIT 1)
		FROM entity n
		WHERE n.pending = 1 AND n.merged_into = ''
		ORDER BY n.created_ts, n.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LorePendingEntity
	for rows.Next() {
		var r LorePendingEntity
		var short sql.NullString
		if err := rows.Scan(&r.ID, &r.Type, &r.Canonical, &r.CreatedTS, &r.CreatedBy,
			&r.Entries, &r.EntriesEver, &short); err != nil {
			return nil, err
		}
		r.Name = loreEntityName(r.Canonical)
		// A subject with no entry has no sample, and that is "" rather than a
		// placeholder sentence: the row already says `entries: 0`, and prose
		// invented here would be prose a reviewer could mistake for content.
		r.SampleShort = loreSampleShort(short.String)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	approved, err := d.listApprovedLoreEntities()
	if err != nil {
		return nil, err
	}
	for i := range out {
		for _, cand := range approved {
			if reason := loreSimilarReason(out[i].Canonical, cand.Canonical); reason != "" {
				out[i].Similar = append(out[i].Similar, LoreEntitySimilar{
					EntityID: cand.ID, Canonical: cand.Canonical, Reason: reason,
				})
			}
		}
		// The suggestion was computed HERE, from `Similar` plus `EntriesEver > 0`.
		// Removed with the field (owner 2026-09-05); the evidence it read is still
		// gathered above and still served, because that is the half he wanted.
		refs, err := d.loreEntryRefsForSubject(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].EntryRefs = refs
	}
	return out, nil
}

// loreEntryRefsForSubject reduces ListLoreEntriesBySubject to the three cells a
// queue row can show.
//
// 🔴 IT REUSES THAT SEAM RATHER THAN WRITING A NARROWER QUERY BESIDE IT. The
// retired predicate, the ordering and the ruling about `superseded` /
// `underspecified` all live in ListLoreEntriesBySubject, and a second SELECT
// here would be a second place for all three to drift — with the drift showing
// up as a review packet that disagrees with the subject it describes. The price
// is that whole entry bodies are loaded and three fields kept; that is stated
// below and it is the cheaper mistake.
//
// 🔴 COST, STATED, BECAUSE IT IS THE ONE THING THIS ADDS: ListPendingLoreEntities
// was TWO statements and is now 2 + N, one extra round trip per pending row. That
// is affordable for the same reason the O(pending × approved) similarity fold in
// the same function is: this route is a PERSON's work queue in an admin console,
// entered when somebody sits down to review, NOT a boot path — no agent's wake
// touches it, and ListLoreSubjectRoster (which every wake does touch) is
// deliberately not wired to any of this. N is the length of the review backlog;
// if that ever grows to where 2+N matters, the queue itself is the emergency.
func (d *DAL) loreEntryRefsForSubject(entityID string) ([]LorePendingEntryRef, error) {
	entries, err := d.ListLoreEntriesBySubject(entityID)
	if err != nil {
		return nil, err
	}
	// Non-nil so a subject with no entries carries an EMPTY list rather than a
	// nil one: `[]` on the wire is 「we looked and there is nothing filed」,
	// which is precisely the fact this round exists to make sayable.
	refs := make([]LorePendingEntryRef, 0, len(entries))
	for _, e := range entries {
		refs = append(refs, LorePendingEntryRef{
			EntryID: e.ID, Heading: e.Heading, Status: e.Status,
		})
	}
	return refs, nil
}

// listApprovedLoreEntities is the candidate set the similarity is computed
// against: everything that is IN the ontology and still stands.
//
// 🔴 IT IS THE SAME PREDICATE THE BOOT DIRECTORY USES — `pending = 0` AND an
// empty `merged_into`. Offering a merged-away subject as a merge target would
// offer as a merge candidate a subject the merge route itself refuses 422,
// which is worse than offering nothing: it reads as homework and it is a dead
// end. (This held when the candidate came with a computed suggestion attached;
// it holds just as hard now that the reviewer is the only one choosing, because
// he is the one who would spend the click.)
func (d *DAL) listApprovedLoreEntities() ([]LoreEntity, error) {
	rows, err := d.rdb.Query(`
		SELECT id, type, canonical FROM entity
		WHERE pending = 0 AND merged_into = ''
		ORDER BY canonical, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoreEntity
	for rows.Next() {
		var e LoreEntity
		if err := rows.Scan(&e.ID, &e.Type, &e.Canonical); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// loreEntityName is the name half of a `type:name` key. A key that somehow
// carries no colon answers with itself rather than with "": the queue's job is
// to show a reviewer what is parked, and a blank name would hide the very row
// that is malformed.
func loreEntityName(canonical string) string {
	if _, name, found := strings.Cut(canonical, ":"); found {
		return name
	}
	return canonical
}

// loreEntityState is the pair every act below has to read before it may write:
// is this entity still awaiting review, and has it already been merged away.
// Reading both in one statement is what keeps "unknown", "already approved" and
// "already merged" three different answers instead of one.
func loreEntityState(tx *sql.Tx, entityID string) (canonical string, pending bool, mergedInto string, err error) {
	var p int
	err = tx.QueryRow(
		`SELECT canonical, pending, merged_into FROM entity WHERE id = ?`, entityID).
		Scan(&canonical, &p, &mergedInto)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, "", fmt.Errorf("%w: %q", ErrLoreEntityUnknown, entityID)
	}
	if err != nil {
		return "", false, "", err
	}
	return canonical, p == 1, mergedInto, nil
}

// ApproveLoreEntity publishes one minted subject into the ontology.
//
// 🔴 APPROVING AN ALREADY-APPROVED ENTITY IS REFUSED, NOT TREATED AS A NO-OP,
// and it is the same rule ReviveLoreEntry follows for the same reason: the
// caller believes the entity is in a state it is not, and answering "done"
// would confirm a belief that is wrong. A merged-away entity is refused through
// the same door — it is not awaiting review either, and approving it would
// un-hide a name the merge deliberately folded away.
//
// 🔴 THE STATE CHANGE AND THE JOURNAL ROW ARE ONE TRANSACTION, exactly as they
// are for a retirement. An approval publishes a name to every agent's boot
// directory; an approval nobody can attribute is the hole the journal exists to
// close.
//
// ⚠️ WHO may call this is NOT decided here. The owner's ruling (「待審，我跟 mira
// 有 admin 權限的才行」, rc-139a5ab99a19) is a statement about principal CLASS,
// which the route table can express exactly (`Requires: principalAdminAgent`)
// and this layer cannot see at all. Re-deriving it from an `actorIsOwner`-style
// flag here would be a second answer to one question — unlike the retire split,
// which is per-REASON and therefore genuinely unsayable in the route table.
func (d *DAL) ApproveLoreEntity(entityID, actorID, reason string, nowTS float64) error {
	if actorID == "" {
		return ErrLoreActorBlank
	}
	return d.inTx(func(tx *sql.Tx) error {
		_, pending, mergedInto, err := loreEntityState(tx, entityID)
		if err != nil {
			return err
		}
		if !pending || mergedInto != "" {
			return fmt.Errorf("%w: %q", ErrLoreEntityNotPending, entityID)
		}
		if _, err := tx.Exec(`UPDATE entity SET pending = 0 WHERE id = ?`, entityID); err != nil {
			return err
		}
		return insertLoreGovernanceEvent(tx, LoreGovernanceEvent{
			Kind: LoreGovEntityApprove, Target: entityID, ActorID: actorID,
			Reason: reason, CreatedTS: nowTS,
		})
	})
}

// MergeLoreEntity folds a pending subject into an existing approved one.
//
// 🔴 IT WRITES BOTH HALVES OF THE MECHANISM THAT ALREADY EXISTS, and one
// without the other is worse than neither:
//
//   - `merged_into` is what makes the old KEY resolve onto the survivor —
//     loreResolveSubject and loreEntityIDForKey both walk it. Without it a later
//     write or search naming the old key lands back on a subject the boot
//     directory hides.
//   - the `entity_alias` row is what stops the old key being MINTED AGAIN. The
//     resolver looks up canonical, then alias, and mints when neither hits; an
//     alias-less merge would produce a fresh pending entity under the same
//     spelling the next time anyone wrote that key, and the review would have
//     bought nothing.
//   - re-filing the `lore_subject` rows is what makes the entries ALREADY under
//     the source reachable. 🔴 THIS ONE IS THE HALF THAT IS EASY TO MISS AND THE
//     ONE THAT LOSES DATA: those join rows name the source's ENTITY ID, and
//     nothing resolves an id through `merged_into` — resolution happens on the
//     KEY, on the way in. So a merge that wrote only the first two columns would
//     leave every existing entry filed against an entity the boot directory
//     hides and that no key now reaches: the entries would exist, the merge
//     would report success, and the knowledge would be gone from every retrieval
//     path. Measured, not feared — the search assertion in
//     dal_lore_entity_t33_test.go went red on exactly that version.
//
// 🔴 THE RE-FILING IS AN INSERT, NEVER A MOVE. The source's own rows are LEFT
// WHERE THEY ARE: nothing in this schema deletes, and a join row is the record
// that this entry was once filed under that name. The survivor gains the
// filings; the source keeps its history; and because the source is hidden from
// every reader by `merged_into`, its copies cost a row and change no answer.
//
// 🔴 THE TARGET IS CHECKED AND EVERY REFUSAL IS NAMED. Merging into a target
// that does not exist, is itself pending, or has itself been merged away are
// three different mistakes, and all three would otherwise "succeed": the source
// would end up pointing at a name the boot directory ALSO hides, i.e. a subject
// no reader can follow, reported as a repair. The self-merge is refused for the
// same reason — `merged_into` pointing at its own row is a cycle, and
// loreResolveSubject answers a cycle by refusing the WRITE that touches it.
//
// ⚠️ THE TWO TYPE PREFIXES ARE NOT REQUIRED TO MATCH, AND THAT IS DELIBERATE
// RATHER THAN AN OVERSIGHT. The commonest thing in this queue after a spelling
// slip is a name minted under the WRONG prefix — `agent:Seth` for the person
// the ontology already carries as `human:Seth` — and a same-type rule would
// refuse exactly the merge that repairs it, leaving the reviewer with approve
// (publishes the duplicate) or nothing.
//
// ⚠️ THE TARGET IS NOT FOLLOWED THROUGH ITS OWN CHAIN, deliberately. A target
// that has been merged away is REFUSED rather than silently redirected onto its
// survivor: the caller named a subject to keep, and quietly keeping a different
// one is precisely the kind of helpfulness this store cannot afford.
func (d *DAL) MergeLoreEntity(entityID, into, actorID, reason string, nowTS float64) error {
	if actorID == "" {
		return ErrLoreActorBlank
	}
	if strings.TrimSpace(into) == "" {
		return fmt.Errorf("%w: %q", ErrLoreEntityUnknown, into)
	}
	if entityID == into {
		return fmt.Errorf("%w: %q", ErrLoreEntityMergeSelf, entityID)
	}
	return d.inTx(func(tx *sql.Tx) error {
		canonical, pending, mergedInto, err := loreEntityState(tx, entityID)
		if err != nil {
			return err
		}
		if !pending || mergedInto != "" {
			return fmt.Errorf("%w: %q", ErrLoreEntityNotPending, entityID)
		}
		_, targetPending, targetMerged, err := loreEntityState(tx, into)
		if err != nil {
			return err
		}
		if targetPending {
			return fmt.Errorf("%w: %q", ErrLoreEntityTargetPending, into)
		}
		if targetMerged != "" {
			return fmt.Errorf("%w: %q was merged into %q", ErrLoreEntityTargetMerged, into, targetMerged)
		}
		if _, err := tx.Exec(
			`UPDATE entity SET merged_into = ?, pending = 0 WHERE id = ?`, into, entityID); err != nil {
			return err
		}
		// The alias PRIMARY KEY is (alias, entity_id), so re-filing the same
		// pair is the state the caller asked for rather than an error.
		if _, err := tx.Exec(`
			INSERT INTO entity_alias (alias, entity_id) VALUES (?, ?)
			ON CONFLICT (alias, entity_id) DO NOTHING`, canonical, into); err != nil {
			return err
		}
		// Every entry the source carries becomes an entry of the survivor. The
		// conflict clause covers an entry that was already filed under both.
		if _, err := tx.Exec(`
			INSERT INTO lore_subject (entry_id, entity_id)
			SELECT entry_id, ? FROM lore_subject WHERE entity_id = ?
			ON CONFLICT (entry_id, entity_id) DO NOTHING`, into, entityID); err != nil {
			return err
		}
		return insertLoreGovernanceEvent(tx, LoreGovernanceEvent{
			Kind: LoreGovEntityMerge, Target: entityID, ActorID: actorID,
			Reason: reason, ReplacedBy: into, CreatedTS: nowTS,
		})
	})
}

// LoreEntity mirrors the row the two acts above write to.
type LoreEntity struct {
	ID         string
	Type       string
	Canonical  string
	Pending    bool
	MergedInto string
	CreatedTS  float64
	CreatedBy  string
}

// GetLoreEntity reads one entity back, or nil when no entity carries that id.
//
// It exists so the two acts above can be RECEIPTED from the stored row rather
// than from the request — the same rule writeLoreGovernanceReceipt follows, and
// for the same reason: an echo would report the state the caller asked for on a
// write that did not happen.
func (d *DAL) GetLoreEntity(id string) (*LoreEntity, error) {
	var e LoreEntity
	var pending int
	err := d.rdb.QueryRow(`
		SELECT id, type, canonical, pending, merged_into, created_ts, created_by
		FROM entity WHERE id = ?`, id).Scan(
		&e.ID, &e.Type, &e.Canonical, &pending, &e.MergedInto, &e.CreatedTS, &e.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Pending = pending == 1
	return &e, nil
}

// ── the review packet (T-33 round 2, owner ruling 2026-09-02) ────────────────
//
// 🔴 THE OWNER'S WORDS ARE THE SHAPE OF THIS WHOLE SECTION: 「我希望 agent 做完
// 功課以後給建議並提出我一眼就可以判斷的資訊，我還是做最後的裁決，lore 的品質
// 優於數量」. Two halves, and BOTH are load-bearing:
//
//   - the homework is DONE HERE, not by the reviewer. A queue row that says only
//     「repo:offcraft, 2 entries」 makes the owner open two other screens to find
//     out whether that name is a typo of something the ontology already carries.
//   - the VERDICT is still his. Nothing below approves or merges anything, and
//     the two acts stay behind the admin floor.
//
// 🔴 THE 「給建議」 HALF WAS BUILT AS A MECHANICAL RULE AND THE OWNER TOOK IT
// BACK OUT (2026-09-05): 「ai 會笨到產生大小寫不一樣的對象嗎」. What remains of
// his sentence is the OTHER half — 「提出我一眼就可以判斷的資訊」 — and that is
// what everything below now serves: who minted it, when, how many entries, how
// many ever, every entry by 第 1 格, a sample, and which existing names resemble
// it WITH the reason each was offered. A suggestion returns in another ticket as
// an AI judgement he can agree with or send back by comment.
//
// 🔴 THE REASONS ARE STILL NAMED, AND THAT SURVIVES THE REMOVAL INTACT: 「0.87」
// tells a reviewer nothing he can check, whereas 「same_normalized」 tells him
// exactly what to look at. That was always the argument for the evidence, not
// for the verdict, which is why it outlived the verdict.

// The similarity reasons. They are NAMES OF EVIDENCE, and the vocabulary is
// closed here because the cockpit and the MCP surface both render them.
const (
	LoreSimilarSameNormalized = "same_normalized" // identical after case / width / _-­ folding
	LoreSimilarEditDistance1  = "edit_distance_1"
	LoreSimilarEditDistance2  = "edit_distance_2"
	LoreSimilarPrefix         = "prefix"    // one name starts with the other
	LoreSimilarSubstring      = "substring" // one name contains the other
)

// 🔴 `LoreSuggestApprove` / `LoreSuggestMerge` STOOD HERE (removed 2026-09-05).
// They were the closed vocabulary of the mechanical suggestion — see the
// tombstone on LorePendingEntity for the owner's ruling and for the AI-judgement
// ticket that replaces it. Nothing else in the package spoke those two words, so
// they left with the rule rather than lingering as a vocabulary for a field that
// no longer exists.

// loreFuzzyMinRunes is the floor under which the fuzzy reasons are not reported
// at all. Below it every short name is 「similar」 to every other: `agent:Al`
// and `agent:Ax` are one edit apart and have nothing to do with each other, and
// a queue full of those is a queue nobody reads.
//
// ⚠️ 3 是佔位數字，不是算出來的 — a placeholder, to be
// calibrated once there is a real queue to calibrate against. same_normalized
// is NOT subject to it: two names that fold to the same string are the same
// name at any length.
const loreFuzzyMinRunes = 3

// loreSampleShortRunes caps the sample body carried into the queue row.
//
// 🔴 THIS ONE IS A TRUNCATION AND IT ANNOUNCES ITSELF. A SAMPLE's only job is to
// let a reviewer see what the subject is about without opening it, so trimming it
// costs nothing — as long as the trim is visible. A truncated sample ends in an
// ellipsis so nobody mistakes it for the entry.
// ⚠️ 射程要講準：**內容那幾格**沒有長度上限（舊的 40-rune `label` 上限跟著
// `label` 一起走了），所以這裡是 lore 這一面**唯一**的截斷。標題格 `heading`
// 自 owner 2026-09-05 的裁定起**有**上限（140 個 rune），但那一道是**拒絕**、
// 不是截斷 —— 兩件事不要混：拒絕會回到寫入者手上，截斷不會。
const loreSampleShortRunes = 120

// LoreEntitySimilar is one existing subject that resembles a pending one, WITH
// the reason it was offered.
//
// 🔴 THE REASON IS THE PAYLOAD, NOT AN ANNOTATION. A number would be a
// judgement wearing a number's clothes: nobody can check 「0.87」, and everybody
// can check 「these two fold to the same string」.
type LoreEntitySimilar struct {
	EntityID  string
	Canonical string
	Reason    string
}

// loreFoldKey folds a subject key or name for comparison: full-width ASCII to
// half-width, upper to lower, and `_` and `-` to one character.
//
// 🔴 THE THREE FOLDS ARE THE OWNER'S OWN LIST (大小寫／全半形／底線連字號) and
// nothing else is folded. Stripping punctuation or spaces in general would make
// two genuinely different names collide, and a collision here puts a name in
// front of the reviewer as a MERGE candidate — the act that moves knowledge
// under a different name. (It used to also PRODUCE a merge suggestion outright;
// that rule was removed 2026-09-05. Folding too eagerly is cheaper now that a
// human reads every candidate, but it is still the fold that decides what he is
// shown, so the list stays the owner's own three and nothing more.)
func loreFoldKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '　': // ideographic space
			r = ' '
		case r >= '！' && r <= '～': // full-width ASCII
			r -= 0xFEE0
		}
		if r == '_' {
			r = '-'
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// loreEditDistance is Levenshtein over runes, with an EARLY CEILING: it stops
// as soon as the answer is known to exceed `max`. Only 1 and 2 are reportable
// reasons, so computing an exact 37 for two unrelated names is work whose result
// is discarded.
func loreEditDistance(a, b string, max int) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) < len(br) {
		ar, br = br, ar
	}
	if len(ar)-len(br) > max {
		return max + 1
	}
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		best := cur[0]
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = prev[j] + 1
			if v := cur[j-1] + 1; v < cur[j] {
				cur[j] = v
			}
			if v := prev[j-1] + cost; v < cur[j] {
				cur[j] = v
			}
			if cur[j] < best {
				best = cur[j]
			}
		}
		if best > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

// loreSimilarReason answers "how does this pending key resemble that existing
// one", or "" when it does not.
//
// 🔴 THE COMPARISON IS WITHIN ONE TYPE, AND THAT IS THE SCHEMA'S OWN RULING
// RATHER THAN A CHOICE MADE HERE. 00081 says it in as many words: 「Kyle being
// both the canonical of agent:Kyle and an alias of human:KyleHsia is CORRECT,
// not a data error」. So an identical NAME under two different type prefixes is
// not evidence of a duplicate, and offering it as one would push a reviewer
// toward merging two things the design says are two things.
// ⚠️ The cost is stated rather than hidden: a name genuinely minted under the
// WRONG prefix (`agent:Seth` for the person the ontology carries as
// `human:Seth`) will NOT be offered as a candidate. The merge route still
// accepts it — this function is simply silent there, which is the direction that
// leaves the decision with the reviewer instead of aiming it.
//
// The precedence is strongest-first and only ONE reason is returned: a row that
// listed every way two names resemble each other would bury the one that
// decides anything.
func loreSimilarReason(pendingKey, existingKey string) string {
	pt, pn, err := loreSubjectTypeAndName(pendingKey)
	if err != nil {
		return ""
	}
	et, en, err := loreSubjectTypeAndName(existingKey)
	if err != nil || loreFoldKey(pt) != loreFoldKey(et) {
		return ""
	}
	a, b := loreFoldKey(pn), loreFoldKey(en)
	if a == b {
		return LoreSimilarSameNormalized
	}
	if len([]rune(a)) < loreFuzzyMinRunes || len([]rune(b)) < loreFuzzyMinRunes {
		return ""
	}
	switch loreEditDistance(a, b, 2) {
	case 1:
		return LoreSimilarEditDistance1
	case 2:
		return LoreSimilarEditDistance2
	}
	if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
		return LoreSimilarPrefix
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return LoreSimilarSubstring
	}
	return ""
}

// 🔴 loreSuggestionFor STOOD HERE, AND IT WAS THE WHOLE MECHANICAL RULE
// (removed by owner ruling 2026-09-05). It read two facts — the named similarity
// evidence and whether the subject had EVER carried an entry — and returned
// 「approve」, 「merge into that id」, or "" when the two facts disagreed.
//
// 🔴 WHY IT WENT: 「ai 會笨到產生大小寫不一樣的對象嗎」. The rule's strongest
// signal was `same_normalized`, i.e. two names identical once case, full/half
// width and `_`/`-` were folded — and the owner's judgement is that the writers
// minting these names do not in fact make that mistake, so the rule's confident
// half was answering a question nobody asks and its careful half (the empty
// string) was work the reviewer had to redo anyway.
//
// 🔴 WHAT REPLACES IT IS NOT A BETTER FOLD: 「請 AI 判一輪、人可以同意或回
// comment 讓它重判」 — a judgement that explains itself and can be argued with.
// That is a separate ticket. This package deliberately ships the queue with NO
// automatic verdict rather than shipping two.
//
// ⚠️ WHAT SURVIVED ON PURPOSE, because it is evidence and not a verdict:
// loreSimilarReason, the five reason constants, loreFoldKey, loreEditDistance
// and loreFuzzyMinRunes. The queue still tells the reviewer WHICH existing names
// resemble a pending one and WHY each was offered. Only 「and therefore press
// this」 is gone. The fold that lost its argument as a SUGGESTION is still the
// right way to surface a candidate for a human to look at.

// loreSampleShort trims one entry's 第 2 格 (`content`) to the sample cap, announcing the
// trim with an ellipsis so it cannot be mistaken for the whole field.
func loreSampleShort(short string) string {
	r := []rune(strings.TrimSpace(short))
	if len(r) <= loreSampleShortRunes {
		return string(r)
	}
	return string(r[:loreSampleShortRunes]) + "…"
}
