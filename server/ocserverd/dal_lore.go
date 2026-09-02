package main

// dal_lore.go — T-33, the access layer for the tables migration
// 00063 introduces. Same convention as dal_tasks.go / dal_task_artifacts.go:
// explicit per-table methods, no generic repository.
//
// 🔴 SCOPE: THIS IS THE L1 SEAM AND THE TWO JOIN TABLES, NOTHING MORE. The
// revision journal (L0), the governance counters (L2), the recall log, feedback
// and governance events all have tables in 00063 and NO functions here yet —
// they belong to the rounds that write the paths that use them. An empty seam is
// honest; a speculative one has to be maintained before anyone can say whether
// it has the right shape.
//
// 🔴 THERE IS NO DELETE IN THIS FILE, AND THERE IS NOT MEANT TO BE. "Throwing an
// entry away" is spelled `status='retired'` — it stops being retrieved and keeps
// existing. The owner chose that (rc-559af60bfba4) knowing what it costs: there
// is no way to remove a secret that gets written into an entry. Adding a hard
// delete here would quietly re-open a decision that was made deliberately, so if
// an erase path is ever wanted it arrives as its own ticket with its own
// tombstone design — not as a convenience method.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// LoreEntry mirrors the lore_entry table — the L1
// layer, and the ONLY layer whose columns are ever folded into a boot context.
//
// 🔴 THERE IS NO TrustScope FIELD, DELIBERATELY. An entry's class is derived
// from its action names by memoryTrustScope() at read time; a field here would
// be a second copy that drifts the first time an entry's actions change. See
// memory_trust_scope.go.
//
// 🔴 Origin IS AN L1 FIELD, NOT AN L2 ONE, and that placement is load-bearing:
// a human origin sorts ahead within its tier and is exempt from the count cap,
// so the assembler must be able to SELECT it. L2 (lore_meta) is
// defined by the assembler NOT being able to see it, which is precisely why
// origin cannot live there.
type LoreEntry struct {
	ID string

	// 🔴 THE SIX BODY FIELDS, FIXED. There is no free-form field here and none
	// is to be added: the owner's complaint was that entries were each growing
	// their own undefined sections, and an `extra` column would rebuild exactly
	// that. Fixing the shape is what makes an eroded entry visible — see
	// IsDegraded below.
	Label        string // one-line title
	Symptoms     string // what I would be SEEING; the situation, not a category name
	Short        string // the compressed body: mechanism and why — THIS is what enters a context
	Falsify      string // how to show this entry does NOT hold
	Instance     string // one case that really happened
	ResidualRisk string // what this entry does NOT protect against

	Status     string // 'active' | 'superseded' | 'retired' | 'underspecified'
	Supersedes string // id of the entry this replaces; the replaced row is re-statused, never deleted
	EditableBy string // 'agent' | 'owner-gated'
	Origin     string // a subject key — `human:Seth`, `agent:Kyle`. WHO this knowledge came from.
	CreatedTS  float64
	UpdatedTS  float64
}

// IsDegraded reports an entry that has been eroded back into a slogan: both
// Falsify and Instance are empty, so it asserts something while offering neither
// a way to check it nor a case where it happened.
//
// 🔴 THIS FUNCTION IS ONLY EXPRESSIBLE BECAUSE THE BODY FIELDS ARE FIXED. Under
// free-form prose, "the falsification section is gone" and "the author never
// wrote one" are the same observation, so erosion is undetectable — which is the
// silent loss this whole ticket is about. Two named columns turn it into a
// boolean anyone can compute.
//
// ⚠️ IT IS DELIBERATELY NOT WIRED TO ANYTHING YET — no UI, no alert, no
// retrieval filter. What to DO about a degraded entry is a later round's
// decision, and hanging a behaviour off it now would decide that by accident.
//
// 🔴 2026-09-02 的裁定（rc-714eea33c6ed）把 `falsify` 與 `instance` 變成寫入必填
// 之後，這個判斷仍然要留著，而且不會永遠是 false：那道裁定只擋新寫入，站上在它
// 之前寫下的條目兩格可以都是空的，這個函式是唯一看得見它們的東西。
//
// It is BOTH fields, not either: an entry with a concrete instance but no
// falsifier is thin, not empty, and calling it degraded would flag most honest
// first drafts.
func (e LoreEntry) IsDegraded() bool {
	return strings.TrimSpace(e.Falsify) == "" && strings.TrimSpace(e.Instance) == ""
}

const loreEntryColumns = `id, label, symptoms, short, falsify, instance, residual_risk,
	status, supersedes, editable_by, origin,
	created_ts, updated_ts`

func scanLoreEntry(row interface{ Scan(...any) error }) (LoreEntry, error) {
	var e LoreEntry
	err := row.Scan(
		&e.ID, &e.Label, &e.Symptoms, &e.Short, &e.Falsify, &e.Instance, &e.ResidualRisk,
		&e.Status, &e.Supersedes, &e.EditableBy, &e.Origin,
		&e.CreatedTS, &e.UpdatedTS,
	)
	return e, err
}

// loreLabelMaxRunes caps the label, in runes (the repo's length unit
// everywhere else, and the only one under which a name in Chinese and a name in
// English are counted the same way).
//
// 🔴 THE CAP IS A REFUSAL, NEVER A TRUNCATION. `label` is a NAME: it is what a
// reader scans a list by and what a merge or a supersede points at, so a name
// that changes silently breaks whatever was pointing at it. Trimming an
// over-long label to fit would be the system quietly editing an identifier —
// the exact silent loss this ticket exists to make impossible.
//
// ⚠️ 40 是佔位數字，不是算出來的. It is a placeholder, not a measured value; it
// has to be calibrated after the trial.
const loreLabelMaxRunes = 40

// loreLabelError enforces that cap. A blank label is NOT refused
// here: an entry can legitimately be written before it has been named, and
// refusing that would push writers into inventing a name, which is worse than an
// empty one — an invented name looks exactly like a chosen one.
func loreLabelError(label string) error {
	if n := len([]rune(label)); n > loreLabelMaxRunes {
		return fmt.Errorf("%w: %d runes, max %d — a label is a NAME, put the sentence in `short`",
			ErrLoreLabelTooLong, n, loreLabelMaxRunes)
	}
	return nil
}

// loreOriginError validates an origin as a subject key.
//
// 🔴 ORIGIN AND SUBJECT ARE THE SAME SHAPE, `type:name`, AND THEY DRAW ON THE
// SAME LIST — the `entity_type` table, read here at run time. That table is the
// ONE copy of the type-prefix vocabulary: the subject side reaches it through
// `entity.type REFERENCES entity_type(type)`, this side reads it directly. A Go slice repeating it
// would be a second copy that drifts the moment a type is approved, and the two
// would then disagree about what is writable, silently, depending on which
// reader you asked.
//
// 🔴 FAIL-CLOSED AND BY NAME. An unrecognised prefix REFUSES the write and says
// which prefix it was; it is never quietly accepted, and never quietly rewritten
// into something that parses. A blank origin is refused too: there is no default
// author, and "unspecified" written as if it were a person would be a claim
// nobody made.
func (d *DAL) loreOriginError(origin string) error {
	if strings.TrimSpace(origin) == "" {
		return ErrLoreOriginBlank
	}
	prefix, name, found := strings.Cut(origin, ":")
	if !found || prefix == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: %q is not `type:name`", ErrLoreOriginMalformed, origin)
	}
	var one int
	err := d.rdb.QueryRow(`SELECT 1 FROM entity_type WHERE type = ?`, prefix).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", ErrLoreOriginUnknownType, prefix)
	}
	return err
}

// ErrLoreEntryIDBlank refuses a write with no id. It is a NAMED
// error rather than a database message because the layer above has to turn it
// into a 400 that says what is wrong, and matching on a driver's wording is
// matching on something nobody promised to keep stable.
var (
	ErrLoreEntryIDBlank      = errors.New("lore: the entry id is blank")
	ErrLoreLabelTooLong      = errors.New("lore: the label is too long")
	ErrLoreOriginBlank       = errors.New("lore: the origin is blank")
	ErrLoreOriginMalformed   = errors.New("lore: the origin is not a `type:name` subject key")
	ErrLoreOriginUnknownType = errors.New("lore: the origin names an unapproved type prefix")
)

// PutLoreEntry creates or replaces ONE entry.
//
// 🔴 created_ts IS NOT IN THE UPDATE CLAUSE. An edit is not a birth: an entry
// that gets its wording tightened must keep the moment it first appeared,
// because that timestamp is what the staleness judgement reads. The VALUES
// expression still carries a created_ts — SQLite evaluates it before it finds
// the conflict — and it is discarded there, which is why the column is absent
// from DO UPDATE SET rather than being set to itself.
func (d *DAL) PutLoreEntry(e LoreEntry) error {
	if e.ID == "" {
		return ErrLoreEntryIDBlank
	}
	if e.Status == "" {
		e.Status = "active"
	}
	if e.EditableBy == "" {
		e.EditableBy = "agent"
	}
	if err := loreLabelError(e.Label); err != nil {
		return err
	}
	if err := d.loreOriginError(e.Origin); err != nil {
		return err
	}
	_, err := d.wdb.Exec(`
		INSERT INTO lore_entry (`+loreEntryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			label = excluded.label, symptoms = excluded.symptoms,
			short = excluded.short, falsify = excluded.falsify,
			instance = excluded.instance, residual_risk = excluded.residual_risk,
			status = excluded.status,
			supersedes = excluded.supersedes, editable_by = excluded.editable_by,
			origin = excluded.origin, updated_ts = excluded.updated_ts`,
		e.ID, e.Label, e.Symptoms, e.Short, e.Falsify, e.Instance, e.ResidualRisk,
		e.Status, e.Supersedes, e.EditableBy, e.Origin,
		e.CreatedTS, e.UpdatedTS)
	return err
}

// GetLoreEntry returns one entry, or nil when no entry carries that
// id. A missing entry is not an error: "does this id exist" is a question the
// callers ask on purpose, and folding it into an error would make them parse one.
func (d *DAL) GetLoreEntry(id string) (*LoreEntry, error) {
	e, err := scanLoreEntry(d.rdb.QueryRow(
		`SELECT `+loreEntryColumns+` FROM lore_entry WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListLoreEntriesBySubject returns the entries filed against one
// entity, oldest→newest with an id tiebreak so the order is deterministic.
//
// 🔴 `retired` ROWS ARE EXCLUDED HERE, AND THAT IS THE RULING, NOT AN
// OPTIMISATION: retired means "no longer retrieved" and nothing else — the row
// stays in the table, readable by the governance surfaces that ask for it by id.
// Filtering in the query rather than at the caller is what stops a future second
// retrieval path from forgetting.
//
// ⚠️ 'superseded' AND 'underspecified' ARE STILL RETURNED. Neither has a ruling
// on retrieval, and silently dropping them here would decide it by accident. The
// ranking layer is where that belongs once somebody decides it.
func (d *DAL) ListLoreEntriesBySubject(entityID string) ([]LoreEntry, error) {
	rows, err := d.rdb.Query(`
		SELECT `+loreEntryColumns+` FROM lore_entry e
		JOIN lore_subject s ON s.entry_id = e.id
		WHERE s.entity_id = ? AND e.status <> 'retired'
		ORDER BY e.created_ts, e.id`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoreEntry
	for rows.Next() {
		e, err := scanLoreEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountLoreEntriesBySubject counts what the list above would return.
//
// It exists as its own query because the wake-time subject roster needs "how
// many does this subject have" for every subject and the bodies for none of
// them; loading rows to call len() on them would put the entire memory table on
// a per-wake path. The two share their predicate word for word ON PURPOSE — a
// count that disagrees with its own list is a bug that shows up as a number
// nobody can reconcile.
func (d *DAL) CountLoreEntriesBySubject(entityID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(`
		SELECT COUNT(*) FROM lore_entry e
		JOIN lore_subject s ON s.entry_id = e.id
		WHERE s.entity_id = ? AND e.status <> 'retired'`, entityID).Scan(&n)
	return n, err
}

// PutLoreSubject files an entry against one subject.
//
// 🔴 ONE ENTRY CAN CARRY MANY SUBJECTS — that is why this is a join table and
// not a column. A memory about "how the boot context is assembled" is about the
// repo AND about the member who wrote it AND about the machine it ran on, and a
// single-subject column would force a writer to pick one and lose the rest.
//
// Re-filing an existing pair is a no-op rather than an error: the pair IS the
// primary key, so "already filed" is the state the caller asked for.
func (d *DAL) PutLoreSubject(entryID, entityID string) error {
	_, err := d.wdb.Exec(`
		INSERT INTO lore_subject (entry_id, entity_id) VALUES (?, ?)
		ON CONFLICT (entry_id, entity_id) DO NOTHING`, entryID, entityID)
	return err
}

// ListLoreSubjects returns one entry's subject entity ids, sorted so
// the order is a property of the data rather than of the insert history.
func (d *DAL) ListLoreSubjects(entryID string) ([]string, error) {
	return d.loreStrings(`
		SELECT entity_id FROM lore_subject
		WHERE entry_id = ? ORDER BY entity_id`, entryID)
}

// PutLoreAction files an action name against an entry.
//
// 🔴 THE ACTION NAME IS NOT VALIDATED AGAINST A LIST, AND MUST NOT BE. The
// action axis is an OPEN set — a writer mints a new name every time a new kind
// of experience is recorded — so a closed vocabulary here would refuse exactly
// the writes the mechanism exists to capture. The safety lives at READ time
// instead: memoryTrustScope() fails closed on a name it does not recognise and
// reports it. See memory_trust_scope.go.
func (d *DAL) PutLoreAction(entryID, action string) error {
	_, err := d.wdb.Exec(`
		INSERT INTO lore_action (entry_id, action) VALUES (?, ?)
		ON CONFLICT (entry_id, action) DO NOTHING`, entryID, action)
	return err
}

// ListLoreActions returns one entry's action names, sorted. This is
// what feeds memoryTrustScope.
func (d *DAL) ListLoreActions(entryID string) ([]string, error) {
	return d.loreStrings(`
		SELECT action FROM lore_action
		WHERE entry_id = ? ORDER BY action`, entryID)
}

// loreStrings is the shared single-column read for the two join
// tables above — the same six lines twice is the shape that lets one of them
// quietly forget rows.Err().
func (d *DAL) loreStrings(query string, args ...any) ([]string, error) {
	rows, err := d.rdb.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ── the subject roster (T-33, the boot-context directory) ───────────────────

// LoreSubjectRosterRow is ONE line of the wake-time 對象目錄: an
// entity that has at least one retrievable entry filed against it, plus how many.
//
// 🔴 THERE IS NO BODY FIELD ON THIS STRUCT, AND THAT IS THE POINT. The boot
// context gets a DIRECTORY — "these subjects exist, this many entries each" —
// and never a `short` / `symptoms` / `falsify` cell. Carrying a body field here
// would put the entries themselves one careless `+=` away from every boot
// document in the fleet, which is a size decision nobody has made. An agent that
// wants an entry reads it deliberately.
type LoreSubjectRosterRow struct {
	EntityID  string
	Type      string
	Canonical string
	Display   string
	Entries   int
	// HumanOrigin is true when at least one of this subject's entries came from
	// a `human:` origin. It rides on the SAME grouped query rather than a second
	// pass, because the assembler needs it for every row it truncates against —
	// a per-subject follow-up query would turn one boot query into N.
	HumanOrigin bool
}

// ListLoreSubjectRoster returns the whole directory in ONE grouped
// query.
//
// 🔴 COST: THIS IS A BOOT-PATH QUERY — every agent, every wake, forever. It is
// therefore ONE statement with no per-subject follow-up: the count and the
// human-origin flag are both aggregates of the same GROUP BY, never a loop over
// CountLoreEntriesBySubject. Approved as an addition to the per-wake
// query set by the owner on reply card rc-e5a9efbed9da (2026-08-31, option [0]);
// see the COST DISCIPLINE block above resumeFloorParts in api_chat.go, where the
// tree keeps that list.
//
// 🔴 `retired` IS EXCLUDED WITH THE SAME PREDICATE THE LIST AND THE COUNT USE.
// Three readers of one rule is already one too many; they are kept word for word
// identical so a directory that disagrees with the list it indexes is impossible
// rather than merely unlikely.
//
// 🔴 THE ENTITY SIDE IS FILTERED TOO — `pending = 0` AND an EMPTY `merged_into` —
// AND BOTH ARE CORRECTNESS, NOT TIDINESS. `entity` is written by agents without
// a gate (that is deliberate: gating the write is what pushes an agent into
// forcing a near-miss key), so `pending = 1` is the whole review queue. Without
// this predicate every unreviewed name an agent invented is published into EVERY
// agent's boot document the moment one entry is filed against it — the directory
// would be asserting, to the whole fleet, that a name nobody has approved is part
// of the ontology.
//
// `merged_into` is the other half of the same story: a merged-away entity keeps
// existing (nothing in this schema deletes), so it and its merge target would
// each take a line. That is a subject counted twice under two names — and
// because the boot directory is TRUNCATED, the duplicate also eats a slot that a
// real subject needed. A wrong number is worse than no number; a wrong number
// that silently drops a real subject is worse again.
//
// 🔴 THERE IS NO PER-READER FILTER LEFT IN THIS QUERY, AND THAT IS THE RULING,
// NOT AN OVERSIGHT. The private/shared wall is gone (rc-26c1fd0c6b3c, option
// [3]: 「不要私密條目了，全部共享」), so every reader sees the same directory.
// `actorID` is therefore currently unused — it is kept in the signature because
// the callers are the two boot paths and a per-actor axis is the shape this
// query is expected to grow again; removing it would churn them both twice.
func (d *DAL) ListLoreSubjectRoster(actorID string) ([]LoreSubjectRosterRow, error) {
	rows, err := d.rdb.Query(`
		SELECT n.id, n.type, n.canonical, n.display,
		       COUNT(*),
		       MAX(CASE WHEN e.origin LIKE 'human:%' THEN 1 ELSE 0 END)
		FROM lore_entry e
		JOIN lore_subject s ON s.entry_id = e.id
		JOIN entity n ON n.id = s.entity_id
		WHERE e.status <> 'retired'
		  AND n.pending = 0
		  AND n.merged_into = ''
		GROUP BY n.id, n.type, n.canonical, n.display
		ORDER BY n.type, n.canonical, n.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoreSubjectRosterRow
	for rows.Next() {
		var r LoreSubjectRosterRow
		var human int
		if err := rows.Scan(&r.EntityID, &r.Type, &r.Canonical, &r.Display, &r.Entries, &human); err != nil {
			return nil, err
		}
		r.HumanOrigin = human == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── the recall journal (T-33, 設計 v29 §3.12.x) ──────────────────────────────

// LoreRecall is ONE row of lore_recall_log: a record that lore was put in front
// of somebody. It is the raw shape of the table, not a view of it — `query` is
// whatever the writing path calls itself, `returned` is whatever that path
// decided is worth being able to count later.
type LoreRecall struct {
	ActorID   string
	Query     string
	SubjectID string
	Hop       int
	Returned  string
	CreatedTS float64
}

// InsertLoreRecall appends one row. It is APPEND-ONLY on purpose: the journal's
// value is that it says what actually happened at a moment in time, so nothing
// here updates or de-duplicates. Two boots of the same member are two events,
// not one event seen twice.
func (d *DAL) InsertLoreRecall(r LoreRecall) error {
	_, err := d.wdb.Exec(`
		INSERT INTO lore_recall_log
			(actor_id, query, subject_id, hop, returned, created_ts)
		VALUES (?, ?, ?, ?, ?, ?)`,
		r.ActorID, r.Query, r.SubjectID, r.Hop, r.Returned, r.CreatedTS)
	return err
}
