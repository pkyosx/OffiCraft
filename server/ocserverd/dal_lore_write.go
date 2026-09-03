package main

// dal_lore_write.go — T-33. The write seam: turning one agent's experience into
// a lore entry, its subjects, its actions and the L0 original that outlives
// every later rewrite.
//
// 🔴 WHY THIS IS NOT IN dal_lore.go. That file is the L1 row seam — PutLoreEntry
// writes one table and says so. Creating an entry is not one table: it is the
// entry, the join rows, the revision journal and (when the write supersedes an
// older entry) a governance act, and every one of them has to happen or none of
// them may. Putting the composite beside the single-row upsert would make the
// two look interchangeable, and the day someone reaches for the cheaper one the
// store gets an entry with no original behind it.
//
// 🔴 THE WHOLE POINT OF THE REVISION ROW. This ticket exists because compression
// today leaves no trace: an entry gets tightened, the wording that explained it
// is gone, and the number of entries does not move. `content` is what enters a
// context; lore_revision is what the agent reads when it stops believing the
// compressed version. An entry written WITHOUT its revision row would look identical
// in every context and every count, and the loss would only be discovered by
// somebody going to look for the original and finding there never was one.
// ⇒ The entry and its first revision are written in ONE transaction.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var (
	// 🔴 只剩兩格必填：第 1 格（trigger，錯誤在 dal_lore.go 的
	// ErrLoreTriggerBlank）與第 2 格（content）。舊的 ErrLoreSymptomsBlank /
	// ErrLoreShortBlank / ErrLoreFalsifyBlank / ErrLoreInstanceBlank 都沒了，
	// 連同它們的欄位一起——五格裡沒有 falsify、沒有 instance。
	// ⚠️ 2026-09-02 rc-714eea33c6ed 把 falsify / instance 變成必填的那道裁定，
	// 因此在這一版**沒有對應的欄位可以套用**。它不是被推翻，是被格式改版讓它
	// 沒有落點了。第 3、4 格是選填，第 5 格是 0..N。
	ErrLoreContentBlank       = errors.New("lore: `content` is blank")
	ErrLoreSubjectBlank       = errors.New("lore: a subject key is blank")
	ErrLoreSubjectMalformed   = errors.New("lore: a subject key is not `type:name`")
	ErrLoreSubjectUnknownType = errors.New("lore: a subject key names an unapproved type prefix")
	ErrLoreSubjectsEmpty      = errors.New("lore: the entry names no subject")
	ErrLoreActionBlank        = errors.New("lore: an action name is blank")
	ErrLoreEntityMergeCycle   = errors.New("lore: the subject's merge chain does not end")
	ErrLoreSupersedesSelf     = errors.New("lore: an entry cannot supersede itself")
)

// LoreGovSupersede is the journal kind written when a new entry takes over from
// an older one.
//
// 🔴 IT IS A GOVERNANCE ACT, NOT A COLUMN UPDATE, for the same reason retiring
// is: it changes whether an existing entry is still the answer, and "why did
// this stop being used" has to be answerable afterwards. The `supersedes`
// column on the new entry records the pointer; only the journal records WHO
// pointed it and WHEN.
const LoreGovSupersede = "supersede"

// LoreWrite is one request to create an entry — 五格（四個欄位 + 0..N 筆事件）、
// the axes it is filed under, and the verified identity of whoever is writing.
//
// 🔴 ActorID IS NOT A BODY FIELD ANYWHERE ABOVE THIS. It comes from the verified
// token subject. `Origin` is a different thing and IS caller-supplied: origin
// says whose knowledge this is (`human:Seth` for something the owner said),
// actor says who typed it. Collapsing the two would make it impossible to record
// what a human told an agent, which is the origin class the assembler treats as
// exempt from the count cap.
type LoreWrite struct {
	Trigger    string
	Content    string
	RetireWhen string
	Problem    string

	// Events 是第 5 格。0 筆是合法的，而且 0 筆跟「有事件但人／地／物空著」是
	// 兩件完全不同的事，兩件都看得出來。
	Events []LoreEvent

	Origin     string
	Supersedes string
	Subjects   []string
	Actions    []string

	ActorID string
}

// LoreMintedEntity is a subject key that named nothing and was therefore
// created — parked as `pending = 1`, which is the review queue.
//
// 🔴 IT IS REPORTED BACK RATHER THAN SWALLOWED. Minting is the right behaviour
// (gating it is what pushes a writer into forcing a near-miss key onto an
// existing subject), but a mint the writer did not intend is a typo that has
// just become part of the ontology. Naming it in the response is what lets the
// writer see `repo:offcraft` the moment it happens instead of a month later in
// a directory nobody reconciles.
type LoreMintedEntity struct {
	EntityID  string
	Canonical string
	Type      string
}

// LoreWriteResult is what the write actually did — read back, never echoed.
type LoreWriteResult struct {
	EntryID    string
	SubjectIDs []string
	Minted     []LoreMintedEntity
	RevisionID int64
	SHA256     string
	Superseded string
}

// loreRevisionBody renders 五格 — the four cells AND the events — into the text
// the L0 journal stores.
//
// 🔴 事件**在 body 裡面**，因此也在 sha256 裡面。少了它，第 5 格就不在 L0 原文
// 層，而「agent 不再相信 content 那一版，回去看原本說了什麼」對事件就永遠問不到
// 答案——那正是 L0 存在的理由。代價是 lore_proposal 目前提不動事件，見
// dal_lore_proposal.go 裡 loreProposalEntry 呼叫處的說明。
//
// 🔴 WHAT "原文" MEANS HERE IS A READING, NOT A RULING — and it is flagged rather
// than hidden. The design calls lore_revision the L0 原文層 and calls `body` 完整
// 原文, but the write endpoint it specifies carries no separate raw-material
// field. The only text that exists at write time is 五格, so the original this
// journal preserves is THE ENTRY AS IT WAS WRITTEN, in full, against the one
// cell (`content`) that later enters a context. That makes the journal answer
// the question the design puts to it — "the agent stops believing the compressed
// version, what did it originally say" — for every cell, not just that one.
// ⚠️ The alternative reading is that L0 should hold the raw conversation the
// entry was distilled from. That would need a request field the approved design
// does not have, so it is NOT decided here.
//
// The rendering is stable and total: every cell appears with its name, in a
// fixed order, blank or not, and so does the `events:` block even when it is
// empty. A renderer that skipped blanks would hash the same bytes for "the
// author never wrote this cell" and "this cell was deleted" — the exact collapse
// this ticket is about.
func loreRevisionBody(e LoreEntry, events []LoreEvent) string {
	var b strings.Builder
	for _, f := range []struct{ name, value string }{
		{"trigger", e.Trigger},
		{"content", e.Content},
		{"retire_when", e.RetireWhen},
		{"problem", e.Problem},
	} {
		b.WriteString(f.name)
		b.WriteString(":\n")
		b.WriteString(f.value)
		b.WriteString("\n\n")
	}
	// 🔴 `events:` 這一行**永遠**印出來，即使一筆事件都沒有。理由跟上面四格
	// 「空白也照印」是同一個：一條沒有事件的條目，跟一條事件被某次改寫弄丟的
	// 條目，在渲染結果裡必須不一樣。跳過空區塊的渲染器會讓這兩者雜湊出同一串。
	b.WriteString("events:\n")
	// 🔴 排序是 (happened_ts, what, actor, place, object)，不是呼叫者給的順序。
	// 同一組事件用不同順序送進來必須雜湊出同一串，否則 base_sha256 會因為
	// 一個沒有人看得見的差異而報「過期」。id 不參與：新寫入時還沒有 id。
	sorted := append([]LoreEvent(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, c := sorted[i], sorted[j]
		if a.HappenedTS != c.HappenedTS {
			return a.HappenedTS < c.HappenedTS
		}
		if a.What != c.What {
			return a.What < c.What
		}
		if a.Actor != c.Actor {
			return a.Actor < c.Actor
		}
		if a.Place != c.Place {
			return a.Place < c.Place
		}
		return a.Object < c.Object
	})
	for _, ev := range sorted {
		// 五欄一列，用 tab 分開，空著就是空著——**不填「未知」**。一列裡看到
		// 兩個相鄰的 tab，就是「這一格沒有東西」，而那是要看得見的事實。
		b.WriteString(strings.Join([]string{
			strconv.FormatFloat(ev.HappenedTS, 'f', -1, 64),
			ev.What, ev.Actor, ev.Place, ev.Object,
		}, "\t"))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

func loreSHA256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// loreSubjectTypeAndName splits a subject key and refuses anything that is not
// `type:name`.
//
// It is the same shape as an origin, deliberately: the design says origin and
// subject draw on one vocabulary, and two parsers for one shape is two places to
// disagree about whether `agent:` with nothing after it is a subject.
func loreSubjectTypeAndName(key string) (string, string, error) {
	if strings.TrimSpace(key) == "" {
		return "", "", ErrLoreSubjectBlank
	}
	prefix, name, found := strings.Cut(key, ":")
	if !found || prefix == "" || strings.TrimSpace(name) == "" {
		return "", "", fmt.Errorf("%w: %q", ErrLoreSubjectMalformed, key)
	}
	return prefix, name, nil
}

// loreResolveSubject turns one subject key into the entity id an entry is filed
// against, minting the entity when the key names nothing yet.
//
// 🔴 AN ALIAS RESOLVES, AND A MERGED-AWAY ENTITY IS FOLLOWED. Filing against a
// merged-away entity would be filing against a name the boot directory
// deliberately hides (`merged_into = ”` is in its predicate), so the entry
// would exist and the directory would never mention the subject — a write that
// reports success and produces something no reader can reach.
//
// 🔴 THE MERGE CHAIN IS WALKED WITH A CEILING, NOT A `for {}`. A cycle in
// `merged_into` is possible (nothing in the schema forbids A→B→A) and an
// unbounded walk would hang the request rather than refuse it.
func loreResolveSubject(tx *sql.Tx, key, actorID string, nowTS float64) (string, *LoreMintedEntity, error) {
	typ, _, err := loreSubjectTypeAndName(key)
	if err != nil {
		return "", nil, err
	}
	var one int
	err = tx.QueryRow(`SELECT 1 FROM entity_type WHERE type = ?`, typ).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, fmt.Errorf("%w: %q", ErrLoreSubjectUnknownType, typ)
	}
	if err != nil {
		return "", nil, err
	}

	var id string
	err = tx.QueryRow(`SELECT id FROM entity WHERE canonical = ?`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRow(`SELECT entity_id FROM entity_alias WHERE alias = ?`, key).Scan(&id)
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		minted := LoreMintedEntity{EntityID: "en-" + newHexID(12), Canonical: key, Type: typ}
		if _, err := tx.Exec(`
			INSERT INTO entity (id, type, canonical, pending, created_ts, created_by)
			VALUES (?, ?, ?, 1, ?, ?)`,
			minted.EntityID, typ, key, nowTS, actorID); err != nil {
			return "", nil, err
		}
		return minted.EntityID, &minted, nil
	case err != nil:
		return "", nil, err
	}

	seen := map[string]bool{id: true}
	for hop := 0; hop < 8; hop++ {
		var into string
		if err := tx.QueryRow(`SELECT merged_into FROM entity WHERE id = ?`, id).Scan(&into); err != nil {
			return "", nil, err
		}
		if into == "" {
			return id, nil, nil
		}
		if seen[into] {
			return "", nil, fmt.Errorf("%w: %q", ErrLoreEntityMergeCycle, key)
		}
		seen[into] = true
		id = into
	}
	return "", nil, fmt.Errorf("%w: %q", ErrLoreEntityMergeCycle, key)
}

// CreateLoreEntry writes one entry, its axes, its L0 original and — when it
// supersedes an older entry — the journal row recording that act.
//
// 🔴 EVERYTHING OR NOTHING. The failure this transaction rules out is an entry
// that is in a context tomorrow with no original behind it and no subject to
// reach it by. Half of this write is not a smaller version of it; it is a row
// that looks finished and is not.
//
// 🔴 兩格在空白時被拒：第 1 格 `trigger` 與第 2 格 `content`。理由是同一個——
// 沒有它這一列誰都讀不到：`content` 是唯一會進開機脈絡的一格，`trigger`
// （什麼時候要記起來）是讀者找到它的那一軸，而且兼任標題。
//
// 🔴 第 3 格 `retire_when` 與第 4 格 `problem` 是**選填**，這一層不會替它們補
// 任何東西。第 4 格「它是主體」是寫作上的重量，不是欄位上的必填——把它變成必填
// 會把填不出來的人逼去掰一個問題，而掰的跟真的長得一模一樣。
//
// ⚠️ 2026-09-02 rc-714eea33c6ed（falsify / instance 純 required）在這一版**沒有
// 落點**：五格裡這兩格都不存在。不要把它當成被推翻——它是被格式改版讓它沒有欄位
// 可以套了，而「五格裡要不要有一格是那個意思」是負責人的事。
//
// 🔴 第 5 格的每一筆事件都要有**時**與**事**；人／地／物空著照收，而且不會被
// 填滿。事件驗證在寫入 transaction **之前**跑完：一筆壞事件不該讓條目本體寫進
// 去一半。
func (d *DAL) CreateLoreEntry(w LoreWrite, nowTS float64) (LoreWriteResult, error) {
	var out LoreWriteResult
	if w.ActorID == "" {
		return out, ErrLoreActorBlank
	}
	if err := loreTriggerError(w.Trigger); err != nil {
		return out, err
	}
	if strings.TrimSpace(w.Content) == "" {
		return out, ErrLoreContentBlank
	}
	if err := d.loreOriginError(w.Origin); err != nil {
		return out, err
	}
	if len(w.Subjects) == 0 {
		return out, ErrLoreSubjectsEmpty
	}
	for _, a := range w.Actions {
		if strings.TrimSpace(a) == "" {
			return out, ErrLoreActionBlank
		}
	}
	for _, ev := range w.Events {
		if err := d.loreEventError(ev); err != nil {
			return out, err
		}
	}

	entry := LoreEntry{
		ID:         "lore-" + newHexID(12),
		Trigger:    w.Trigger,
		Content:    w.Content,
		RetireWhen: w.RetireWhen,
		Problem:    w.Problem,
		Status:     "active",
		Supersedes: w.Supersedes,
		EditableBy: "agent",
		Origin:     w.Origin,
		CreatedTS:  nowTS,
		UpdatedTS:  nowTS,
	}
	if entry.Supersedes == entry.ID {
		return out, ErrLoreSupersedesSelf
	}
	body := loreRevisionBody(entry, w.Events)
	sum := loreSHA256(body)

	err := d.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			INSERT INTO lore_entry (`+loreEntryColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.ID, entry.Trigger, entry.Content, entry.RetireWhen, entry.Problem,
			entry.Status, entry.Supersedes,
			entry.EditableBy, entry.Origin, entry.CreatedTS, entry.UpdatedTS); err != nil {
			return err
		}

		// 第 5 格：0..N 筆，寫在同一個 transaction 裡。事件跟條目一起進去或
		// 一起不進去——一條有 content 卻掉了事件的條目，在任何畫面上都跟一條
		// 本來就沒有事件的條目長得一模一樣。
		for _, ev := range w.Events {
			if _, err := tx.Exec(`
				INSERT INTO lore_event (entry_id, happened_ts, what, actor, place, object)
				VALUES (?, ?, ?, ?, ?, ?)`,
				entry.ID, ev.HappenedTS, ev.What, ev.Actor, ev.Place, ev.Object); err != nil {
				return err
			}
		}

		filed := map[string]bool{}
		for _, key := range w.Subjects {
			entityID, minted, err := loreResolveSubject(tx, key, w.ActorID, nowTS)
			if err != nil {
				return err
			}
			if minted != nil {
				out.Minted = append(out.Minted, *minted)
			}
			// A key sent twice, or two keys that resolve through an alias onto
			// the same subject, file one row — the pair is the primary key, so
			// the second INSERT would be a no-op anyway. Deduping HERE is what
			// keeps the reported subject list equal to what was actually filed.
			if filed[entityID] {
				continue
			}
			filed[entityID] = true
			if _, err := tx.Exec(`
				INSERT INTO lore_subject (entry_id, entity_id) VALUES (?, ?)
				ON CONFLICT (entry_id, entity_id) DO NOTHING`, entry.ID, entityID); err != nil {
				return err
			}
			out.SubjectIDs = append(out.SubjectIDs, entityID)
		}

		for _, action := range w.Actions {
			if _, err := tx.Exec(`
				INSERT INTO lore_action (entry_id, action) VALUES (?, ?)
				ON CONFLICT (entry_id, action) DO NOTHING`, entry.ID, action); err != nil {
				return err
			}
		}

		// 🔴 shrink_chars IS 0 HERE AND THAT IS NOT A PLACEHOLDER. It records how
		// much a rewrite REMOVED, and this endpoint only ever creates — there is
		// no previous revision to be shorter than. Computing it against nothing
		// would be code no test could distinguish from correct, so the shrink
		// arrives with the edit path that gives it a meaning.
		res, err := tx.Exec(`
			INSERT INTO lore_revision (entry_id, body, sha256, actor_id, created_ts, shrink_chars)
			VALUES (?, ?, ?, ?, ?, 0)`,
			entry.ID, body, sum, w.ActorID, nowTS)
		if err != nil {
			return err
		}
		if out.RevisionID, err = res.LastInsertId(); err != nil {
			return err
		}

		// 🔴 lore_meta CARRIES THE ACTOR, AND ONLY THE ACTOR. Its two other
		// provenance columns — source_task_id and source_chat_id — stay blank
		// because the approved request shape has no field that could fill them,
		// and the field set is closed. That is a real gap, reported rather than
		// papered over: `lore_get` promises provenance{task_id, chat_id,
		// actor_id} and two thirds of it can never be non-empty until the write
		// shape gains a way to say where the knowledge came from.
		if _, err := tx.Exec(`
			INSERT INTO lore_meta (entry_id, created_ts, source_actor_id)
			VALUES (?, ?, ?)`, entry.ID, nowTS, w.ActorID); err != nil {
			return err
		}

		if entry.Supersedes != "" {
			res, err := tx.Exec(`
				UPDATE lore_entry SET status = 'superseded', updated_ts = ?
				WHERE id = ? AND status <> 'retired'`, nowTS, entry.Supersedes)
			if err != nil {
				return err
			}
			// A supersede pointing at nothing is refused, not recorded. The
			// pointer is how a reader gets from the new entry back to what it
			// replaced; one that names no row is a dead end that looks like a
			// trail, and the whole write is rolled back rather than leaving it.
			if n, err := res.RowsAffected(); err != nil {
				return err
			} else if n == 0 {
				return fmt.Errorf("%w: supersedes %q", ErrLoreEntryUnknown, entry.Supersedes)
			}
			out.Superseded = entry.Supersedes
			if err := insertLoreGovernanceEvent(tx, LoreGovernanceEvent{
				Kind: LoreGovSupersede, Target: entry.Supersedes, ActorID: w.ActorID,
				ReplacedBy: entry.ID, CreatedTS: nowTS,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return LoreWriteResult{}, err
	}
	out.EntryID = entry.ID
	out.SHA256 = sum
	return out, nil
}

// LoreRevision is one row of the L0 journal.
type LoreRevision struct {
	ID          int64
	EntryID     string
	Body        string
	SHA256      string
	ActorID     string
	CreatedTS   float64
	ShrinkChars int
}

// LatestLoreRevision returns the newest original recorded for an entry, or nil
// when the entry has none.
//
// 🔴 nil IS NOT THE SAME AS AN EMPTY BODY and the caller must not flatten them.
// An entry with no revision is an entry written by a path that did not preserve
// its original — exactly the state CreateLoreEntry exists to make impossible —
// and answering with an empty string would report it as an entry whose original
// happened to be blank.
func (d *DAL) LatestLoreRevision(entryID string) (*LoreRevision, error) {
	var r LoreRevision
	err := d.rdb.QueryRow(`
		SELECT id, entry_id, body, sha256, actor_id, created_ts, shrink_chars
		FROM lore_revision WHERE entry_id = ? ORDER BY id DESC LIMIT 1`, entryID).Scan(
		&r.ID, &r.EntryID, &r.Body, &r.SHA256, &r.ActorID, &r.CreatedTS, &r.ShrinkChars)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ── the L0 journal, read side (T-33, hop ③) ─────────────────────────────────

// LoreRevisionRow is ONE line of an entry's revision catalogue: which revision,
// when, by whom, and how much that write REMOVED — never the text.
//
// 🔴 THE BODY IS ABSENT ON PURPOSE, and it is the same rule the document
// history catalogue follows: a list is how a reader CHOOSES a revision, and
// choosing does not need the prose. Carrying every body in the list would put
// the entire journal — which has no depth limit at all — into one response.
type LoreRevisionRow struct {
	ID          int64
	ActorID     string
	CreatedTS   float64
	ShrinkChars int
	SHA256      string
}

// ListLoreRevisions returns an entry's revision catalogue, OLDEST FIRST.
//
// Oldest-first because the sequence is the point: an entry written, tightened,
// tightened again reads as a story in that order and as a pile of rows in the
// other — and `shrink_chars` only means anything against the one before it.
func (d *DAL) ListLoreRevisions(entryID string) ([]LoreRevisionRow, error) {
	rows, err := d.rdb.Query(`
		SELECT id, actor_id, created_ts, shrink_chars, sha256
		FROM lore_revision WHERE entry_id = ? ORDER BY id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoreRevisionRow
	for rows.Next() {
		var r LoreRevisionRow
		if err := rows.Scan(&r.ID, &r.ActorID, &r.CreatedTS, &r.ShrinkChars, &r.SHA256); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetLoreRevision returns ONE revision in full, and it is scoped to the entry
// the caller addressed.
//
// 🔴 THE entry_id IN THE PATH IS A CONSTRAINT, NOT DECORATION. Revision ids are
// global, so a lookup by id alone would serve any entry's original through any
// entry's URL — the address would stop meaning what it says, and a reader that
// mis-typed the entry id would get somebody else's text with no sign anything
// was wrong. Scoping it makes that mistake a 404, which is loud.
func (d *DAL) GetLoreRevision(entryID string, revisionID int64) (*LoreRevision, error) {
	var r LoreRevision
	err := d.rdb.QueryRow(`
		SELECT id, entry_id, body, sha256, actor_id, created_ts, shrink_chars
		FROM lore_revision WHERE entry_id = ? AND id = ?`, entryID, revisionID).Scan(
		&r.ID, &r.EntryID, &r.Body, &r.SHA256, &r.ActorID, &r.CreatedTS, &r.ShrinkChars)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
