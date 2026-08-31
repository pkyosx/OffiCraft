package main

// dal_world_state_memory.go — T-33, the access layer for the tables migration
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

// WorldStateMemoryEntry mirrors the world_state_memory_entry table — the L1
// layer, and the ONLY layer whose columns are ever folded into a boot context.
//
// 🔴 THERE IS NO TrustScope FIELD, DELIBERATELY. An entry's class is derived
// from its action names by memoryTrustScope() at read time; a field here would
// be a second copy that drifts the first time an entry's actions change. See
// memory_trust_scope.go.
//
// 🔴 Origin IS AN L1 FIELD, NOT AN L2 ONE, and that placement is load-bearing:
// a human origin sorts ahead within its tier and is exempt from the count cap,
// so the assembler must be able to SELECT it. L2 (world_state_memory_meta) is
// defined by the assembler NOT being able to see it, which is precisely why
// origin cannot live there.
type WorldStateMemoryEntry struct {
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
	Visibility string // 'shared' | 'private' — a coarse wall, not a retrieval dimension
	OwnerScope string // who may see it while Visibility is 'private'
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
// It is BOTH fields, not either: an entry with a concrete instance but no
// falsifier is thin, not empty, and calling it degraded would flag most honest
// first drafts.
func (e WorldStateMemoryEntry) IsDegraded() bool {
	return strings.TrimSpace(e.Falsify) == "" && strings.TrimSpace(e.Instance) == ""
}

const worldStateMemoryEntryColumns = `id, label, symptoms, short, falsify, instance, residual_risk,
	status, supersedes, visibility, owner_scope, editable_by, origin,
	created_ts, updated_ts`

func scanWorldStateMemoryEntry(row interface{ Scan(...any) error }) (WorldStateMemoryEntry, error) {
	var e WorldStateMemoryEntry
	err := row.Scan(
		&e.ID, &e.Label, &e.Symptoms, &e.Short, &e.Falsify, &e.Instance, &e.ResidualRisk,
		&e.Status, &e.Supersedes, &e.Visibility, &e.OwnerScope, &e.EditableBy, &e.Origin,
		&e.CreatedTS, &e.UpdatedTS,
	)
	return e, err
}

// worldStateMemoryLabelMaxRunes caps the label, in runes (the repo's length unit
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
const worldStateMemoryLabelMaxRunes = 40

// worldStateMemoryLabelError enforces that cap. A blank label is NOT refused
// here: an entry can legitimately be written before it has been named, and
// refusing that would push writers into inventing a name, which is worse than an
// empty one — an invented name looks exactly like a chosen one.
func worldStateMemoryLabelError(label string) error {
	if n := len([]rune(label)); n > worldStateMemoryLabelMaxRunes {
		return fmt.Errorf("%w: %d runes, max %d — a label is a NAME, put the sentence in `short`",
			ErrWorldStateMemoryLabelTooLong, n, worldStateMemoryLabelMaxRunes)
	}
	return nil
}

// worldStateMemoryOriginError validates an origin as a subject key.
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
func (d *DAL) worldStateMemoryOriginError(origin string) error {
	if strings.TrimSpace(origin) == "" {
		return ErrWorldStateMemoryOriginBlank
	}
	prefix, name, found := strings.Cut(origin, ":")
	if !found || prefix == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: %q is not `type:name`", ErrWorldStateMemoryOriginMalformed, origin)
	}
	var one int
	err := d.rdb.QueryRow(`SELECT 1 FROM entity_type WHERE type = ?`, prefix).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", ErrWorldStateMemoryOriginUnknownType, prefix)
	}
	return err
}

// ErrWorldStateMemoryEntryIDBlank refuses a write with no id. It is a NAMED
// error rather than a database message because the layer above has to turn it
// into a 400 that says what is wrong, and matching on a driver's wording is
// matching on something nobody promised to keep stable.
var (
	ErrWorldStateMemoryEntryIDBlank      = errors.New("world state memory: the entry id is blank")
	ErrWorldStateMemoryLabelTooLong      = errors.New("world state memory: the label is too long")
	ErrWorldStateMemoryOriginBlank       = errors.New("world state memory: the origin is blank")
	ErrWorldStateMemoryOriginMalformed   = errors.New("world state memory: the origin is not a `type:name` subject key")
	ErrWorldStateMemoryOriginUnknownType = errors.New("world state memory: the origin names an unapproved type prefix")
)

// PutWorldStateMemoryEntry creates or replaces ONE entry.
//
// 🔴 created_ts IS NOT IN THE UPDATE CLAUSE. An edit is not a birth: an entry
// that gets its wording tightened must keep the moment it first appeared,
// because that timestamp is what the staleness judgement reads. The VALUES
// expression still carries a created_ts — SQLite evaluates it before it finds
// the conflict — and it is discarded there, which is why the column is absent
// from DO UPDATE SET rather than being set to itself.
func (d *DAL) PutWorldStateMemoryEntry(e WorldStateMemoryEntry) error {
	if e.ID == "" {
		return ErrWorldStateMemoryEntryIDBlank
	}
	if e.Status == "" {
		e.Status = "active"
	}
	if e.Visibility == "" {
		e.Visibility = "shared"
	}
	if e.EditableBy == "" {
		e.EditableBy = "agent"
	}
	if err := worldStateMemoryLabelError(e.Label); err != nil {
		return err
	}
	if err := d.worldStateMemoryOriginError(e.Origin); err != nil {
		return err
	}
	_, err := d.wdb.Exec(`
		INSERT INTO world_state_memory_entry (`+worldStateMemoryEntryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			label = excluded.label, symptoms = excluded.symptoms,
			short = excluded.short, falsify = excluded.falsify,
			instance = excluded.instance, residual_risk = excluded.residual_risk,
			status = excluded.status,
			supersedes = excluded.supersedes, visibility = excluded.visibility,
			owner_scope = excluded.owner_scope, editable_by = excluded.editable_by,
			origin = excluded.origin, updated_ts = excluded.updated_ts`,
		e.ID, e.Label, e.Symptoms, e.Short, e.Falsify, e.Instance, e.ResidualRisk,
		e.Status, e.Supersedes, e.Visibility, e.OwnerScope, e.EditableBy, e.Origin,
		e.CreatedTS, e.UpdatedTS)
	return err
}

// GetWorldStateMemoryEntry returns one entry, or nil when no entry carries that
// id. A missing entry is not an error: "does this id exist" is a question the
// callers ask on purpose, and folding it into an error would make them parse one.
func (d *DAL) GetWorldStateMemoryEntry(id string) (*WorldStateMemoryEntry, error) {
	e, err := scanWorldStateMemoryEntry(d.rdb.QueryRow(
		`SELECT `+worldStateMemoryEntryColumns+` FROM world_state_memory_entry WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListWorldStateMemoryEntriesBySubject returns the entries filed against one
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
func (d *DAL) ListWorldStateMemoryEntriesBySubject(entityID string) ([]WorldStateMemoryEntry, error) {
	rows, err := d.rdb.Query(`
		SELECT `+worldStateMemoryEntryColumns+` FROM world_state_memory_entry e
		JOIN world_state_memory_subject s ON s.entry_id = e.id
		WHERE s.entity_id = ? AND e.status <> 'retired'
		ORDER BY e.created_ts, e.id`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorldStateMemoryEntry
	for rows.Next() {
		e, err := scanWorldStateMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountWorldStateMemoryEntriesBySubject counts what the list above would return.
//
// It exists as its own query because the wake-time subject roster needs "how
// many does this subject have" for every subject and the bodies for none of
// them; loading rows to call len() on them would put the entire memory table on
// a per-wake path. The two share their predicate word for word ON PURPOSE — a
// count that disagrees with its own list is a bug that shows up as a number
// nobody can reconcile.
func (d *DAL) CountWorldStateMemoryEntriesBySubject(entityID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(`
		SELECT COUNT(*) FROM world_state_memory_entry e
		JOIN world_state_memory_subject s ON s.entry_id = e.id
		WHERE s.entity_id = ? AND e.status <> 'retired'`, entityID).Scan(&n)
	return n, err
}

// PutWorldStateMemorySubject files an entry against one subject.
//
// 🔴 ONE ENTRY CAN CARRY MANY SUBJECTS — that is why this is a join table and
// not a column. A memory about "how the boot context is assembled" is about the
// repo AND about the member who wrote it AND about the machine it ran on, and a
// single-subject column would force a writer to pick one and lose the rest.
//
// Re-filing an existing pair is a no-op rather than an error: the pair IS the
// primary key, so "already filed" is the state the caller asked for.
func (d *DAL) PutWorldStateMemorySubject(entryID, entityID string) error {
	_, err := d.wdb.Exec(`
		INSERT INTO world_state_memory_subject (entry_id, entity_id) VALUES (?, ?)
		ON CONFLICT (entry_id, entity_id) DO NOTHING`, entryID, entityID)
	return err
}

// ListWorldStateMemorySubjects returns one entry's subject entity ids, sorted so
// the order is a property of the data rather than of the insert history.
func (d *DAL) ListWorldStateMemorySubjects(entryID string) ([]string, error) {
	return d.worldStateMemoryStrings(`
		SELECT entity_id FROM world_state_memory_subject
		WHERE entry_id = ? ORDER BY entity_id`, entryID)
}

// PutWorldStateMemoryAction files an action name against an entry.
//
// 🔴 THE ACTION NAME IS NOT VALIDATED AGAINST A LIST, AND MUST NOT BE. The
// action axis is an OPEN set — a writer mints a new name every time a new kind
// of experience is recorded — so a closed vocabulary here would refuse exactly
// the writes the mechanism exists to capture. The safety lives at READ time
// instead: memoryTrustScope() fails closed on a name it does not recognise and
// reports it. See memory_trust_scope.go.
func (d *DAL) PutWorldStateMemoryAction(entryID, action string) error {
	_, err := d.wdb.Exec(`
		INSERT INTO world_state_memory_action (entry_id, action) VALUES (?, ?)
		ON CONFLICT (entry_id, action) DO NOTHING`, entryID, action)
	return err
}

// ListWorldStateMemoryActions returns one entry's action names, sorted. This is
// what feeds memoryTrustScope.
func (d *DAL) ListWorldStateMemoryActions(entryID string) ([]string, error) {
	return d.worldStateMemoryStrings(`
		SELECT action FROM world_state_memory_action
		WHERE entry_id = ? ORDER BY action`, entryID)
}

// worldStateMemoryStrings is the shared single-column read for the two join
// tables above — the same six lines twice is the shape that lets one of them
// quietly forget rows.Err().
func (d *DAL) worldStateMemoryStrings(query string, args ...any) ([]string, error) {
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
