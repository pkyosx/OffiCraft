package main

// dal.go — the durable data-access layer over the goose-migrated SQLite store
// (the Go twin of the retired Python service/repository.py's durable half). One struct +
// exactly the CRUD/upsert/query surface each table actually serves; the SSE
// commit-funnel fan-out stays a service concern and is NOT here.
//
// Single-owner by decree (card 4019a601): no owner parameter anywhere — every
// table keys on its natural identity (see migrations/00001_schema.sql).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// DAL owns the migrated *sql.DB handle. Construct with NewDAL over a database
// openSQLite'd + runMigrations'd by the caller.
type DAL struct {
	db *sql.DB
}

func NewDAL(db *sql.DB) *DAL {
	return &DAL{db: db}
}

// ── members ──────────────────────────────────────────────────────────────────

// Member mirrors the member table: one roster row per AI colleague — an agent
// OR a warden (machine = the kind=='warden' row; its id IS the machine_id).
// Intent only; presence/location are observed and never stored.
type Member struct {
	ID               string
	Name             string
	Kind             string // closed set: "assistant" | "warden" | "outsource" (schema CHECK)
	RoleKey          string
	Model            string
	Effort           string
	DesiredState     string
	DesiredMachineID string
	WakingSince      float64
	StoppingSince    float64
	StoppedSince     float64
	RefocusSince     float64
	BankedCost       float64
	LastOp           string
	LastOpOK         *bool // nil = no op reported yet (three-valued)
	LastOpLog        string
	LastOpReason     string // structured "<code>: <detail>" cause; "" = none reported
	LastOpAt         float64
	RosterStatus     string  // "active" | "removed" (dismiss is a SOFT delete)
	LinkedTaskID     *string // task binding (migrations/00024); nil = unbound. Outsource members carry their bound task id here.
	// ── A案 P7d (migrations/00025 — the outsource_worker fold) ────────────────
	// Codename is the outsource display codename (O-7 / S-12 / H-3), globally
	// unique and never reused (partial UNIQUE index); "" (stored NULL) on every
	// non-outsource member. CreatedTS is the row's birth stamp (0.0 on
	// pre-00025 non-outsource rows). ReleasedTS / ActivatedTS carry the
	// outsource lifecycle anchors: released → roster_status='removed' +
	// released_ts; activated_ts > 0 = the worker claimed its task (the durable
	// assigned↔active distinction the frozen worker DTO status still serves).
	Codename    string
	CreatedTS   float64
	ReleasedTS  float64
	ActivatedTS float64
}

// RosterStatusRemoved is the soft-delete lifecycle value (the Python
// MEMBER_STATUS_REMOVED twin).
const (
	RosterStatusActive  = "active"
	RosterStatusRemoved = "removed"
)

const memberColumns = `id, name, kind, role_key, model, effort,
	desired_state, desired_machine_id,
	waking_since, stopping_since, stopped_since, refocus_since, banked_cost,
	last_op, last_op_ok, last_op_log, last_op_reason, last_op_at, roster_status,
	linked_task_id, codename, created_ts, released_ts, activated_ts`

func scanMember(row interface{ Scan(...any) error }) (Member, error) {
	var m Member
	var lastOpOK sql.NullBool
	var linkedTaskID, codename sql.NullString
	err := row.Scan(
		&m.ID, &m.Name, &m.Kind, &m.RoleKey, &m.Model, &m.Effort,
		&m.DesiredState, &m.DesiredMachineID,
		&m.WakingSince, &m.StoppingSince, &m.StoppedSince, &m.RefocusSince,
		&m.BankedCost,
		&m.LastOp, &lastOpOK, &m.LastOpLog, &m.LastOpReason, &m.LastOpAt, &m.RosterStatus,
		&linkedTaskID, &codename, &m.CreatedTS, &m.ReleasedTS, &m.ActivatedTS,
	)
	if err != nil {
		return Member{}, err
	}
	if lastOpOK.Valid {
		m.LastOpOK = &lastOpOK.Bool
	}
	if linkedTaskID.Valid {
		m.LinkedTaskID = &linkedTaskID.String
	}
	m.Codename = codename.String
	return m, nil
}

// ListMembers returns the STAFF roster (ANY roster_status — soft-removed rows
// included; callers filter, mirroring repository.list_members). kind='outsource'
// rows are EXCLUDED by design (A案 P7d): the merged storage keeps outsource
// members out of every member-surface fold (REST list, reconcile, boot-context
// rosters, monitoring) so the wire behaviour matches the pre-merge two-table
// world — the outsource projection reads them through ListOutsourceWorkers
// (dal_tasks.go). Behavioural roster convergence is a later, owner-gated step.
func (d *DAL) ListMembers() ([]Member, error) {
	rows, err := d.db.Query(`SELECT ` + memberColumns +
		` FROM member WHERE kind != 'outsource' ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMember returns one roster member by id, or nil if absent.
func (d *DAL) GetMember(id string) (*Member, error) {
	row := d.db.QueryRow(`SELECT `+memberColumns+` FROM member WHERE id = ?`, id)
	m, err := scanMember(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// PutMember upserts a member row (the repository.put_member twin; the SSE
// delta is the service layer's job).
func (d *DAL) PutMember(m Member) error {
	var lastOpOK any
	if m.LastOpOK != nil {
		lastOpOK = *m.LastOpOK
	}
	var linkedTaskID any
	if m.LinkedTaskID != nil {
		linkedTaskID = *m.LinkedTaskID
	}
	// "" stores NULL so the partial UNIQUE codename index never trips on the
	// many codename-less staff rows (NULLs are mutually distinct in SQLite).
	var codename any
	if m.Codename != "" {
		codename = m.Codename
	}
	_, err := d.db.Exec(`
		INSERT INTO member (`+memberColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name, kind = excluded.kind,
			role_key = excluded.role_key, model = excluded.model,
			effort = excluded.effort, desired_state = excluded.desired_state,
			desired_machine_id = excluded.desired_machine_id,
			waking_since = excluded.waking_since,
			stopping_since = excluded.stopping_since,
			stopped_since = excluded.stopped_since,
			refocus_since = excluded.refocus_since,
			banked_cost = excluded.banked_cost,
			last_op = excluded.last_op, last_op_ok = excluded.last_op_ok,
			last_op_log = excluded.last_op_log,
			last_op_reason = excluded.last_op_reason,
			last_op_at = excluded.last_op_at,
			roster_status = excluded.roster_status,
			linked_task_id = excluded.linked_task_id,
			codename = excluded.codename,
			created_ts = excluded.created_ts,
			released_ts = excluded.released_ts,
			activated_ts = excluded.activated_ts`,
		m.ID, m.Name, m.Kind, m.RoleKey, m.Model, m.Effort,
		m.DesiredState, m.DesiredMachineID,
		m.WakingSince, m.StoppingSince, m.StoppedSince, m.RefocusSince,
		m.BankedCost,
		m.LastOp, lastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt, m.RosterStatus,
		linkedTaskID, codename, m.CreatedTS, m.ReleasedTS, m.ActivatedTS,
	)
	return err
}

// HardDeleteMember PHYSICALLY deletes a member row (the custom-role cascade
// path) — NOT the roster_status="removed" soft-remove, which stays the
// audit-preserving dismiss seam. Returns true iff a row was deleted.
func (d *DAL) HardDeleteMember(id string) (bool, error) {
	res, err := d.db.Exec(`DELETE FROM member WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ── chat ─────────────────────────────────────────────────────────────────────

// ChatMessage mirrors the chat_message table. Meta is the free-form JSON blob;
// meta["attachments"] refs are the ONLY attachment linkage (no FK by decree).
type ChatMessage struct {
	ID        string
	Sender    string
	Recipient string
	Body      string
	TS        float64
	Meta      map[string]any
}

func scanChat(row interface{ Scan(...any) error }) (ChatMessage, error) {
	var m ChatMessage
	var meta string
	if err := row.Scan(&m.ID, &m.Sender, &m.Recipient, &m.Body, &m.TS, &meta); err != nil {
		return ChatMessage{}, err
	}
	if err := json.Unmarshal([]byte(meta), &m.Meta); err != nil {
		return ChatMessage{}, fmt.Errorf("chat_message %s: bad meta JSON: %w", m.ID, err)
	}
	return m, nil
}

// ListChat returns the whole chat stream, oldest→newest. Equal-ts messages
// tie-break on id so the stream order is total — the SAME (ts, id) order the
// keyset scrollback cursor (ListChatBefore) pages by, so a page boundary can
// never straddle an undefined ordering.
func (d *DAL) ListChat() ([]ChatMessage, error) {
	rows, err := d.db.Query(
		`SELECT id, sender, recipient, body, ts, meta FROM chat_message ORDER BY ts, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		m, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListChatBefore returns the most recent `limit` messages strictly OLDER than
// the (beforeTS, beforeID) keyset cursor, optionally filtered to a
// participant (sender OR recipient; "" = no filter), oldest→newest — the
// scrollback history page. "Older" is the stream's total (ts, id) order
// (`ts < :bts OR (ts = :bts AND id < :bid)`), so equal-ts collisions never
// drop or duplicate a message across page boundaries; messages are immutable,
// so a cursor stays valid forever. The LIMIT lives in SQL (never a full-table
// pull). A NEGATIVE limit disables the cap; limit 0 reads nothing.
func (d *DAL) ListChatBefore(participant string, beforeTS float64, beforeID string, limit int) ([]ChatMessage, error) {
	if limit == 0 {
		return nil, nil
	}
	query := `
		SELECT id, sender, recipient, body, ts, meta FROM chat_message
		WHERE (ts < ? OR (ts = ? AND id < ?))`
	args := []any{beforeTS, beforeTS, beforeID}
	if participant != "" {
		query += ` AND (sender = ? OR recipient = ?)`
		args = append(args, participant, participant)
	}
	query += ` ORDER BY ts DESC, id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var newestFirst []ChatMessage
	for rows.Next() {
		m, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		newestFirst = append(newestFirst, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// SQL gave the page newest-first (DESC walks back from the cursor);
	// re-sort ascending like the rest of the chat surface.
	out := make([]ChatMessage, len(newestFirst))
	for i, m := range newestFirst {
		out[len(newestFirst)-1-i] = m
	}
	return out, nil
}

// ListChatInvolving returns the most recent `limit` messages involving
// `participant` (sender OR recipient), oldest→newest — the bounded
// wake-snapshot read. A blank participant / non-positive limit reads nothing.
func (d *DAL) ListChatInvolving(participant string, limit int) ([]ChatMessage, error) {
	if participant == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := d.db.Query(`
		SELECT id, sender, recipient, body, ts, meta FROM chat_message
		WHERE sender = ? OR recipient = ?
		ORDER BY ts DESC LIMIT ?`, participant, participant, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var newestFirst []ChatMessage
	for rows.Next() {
		m, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		newestFirst = append(newestFirst, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// SQL gave the newest `limit` (ts DESC); re-sort ascending like the rest
	// of the chat surface.
	out := make([]ChatMessage, len(newestFirst))
	for i, m := range newestFirst {
		out[len(newestFirst)-1-i] = m
	}
	return out, nil
}

// PutChat upserts a chat message.
func (d *DAL) PutChat(m ChatMessage) error {
	meta := m.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	blob, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`
		INSERT INTO chat_message (id, sender, recipient, body, ts, meta)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			sender = excluded.sender, recipient = excluded.recipient,
			body = excluded.body, ts = excluded.ts, meta = excluded.meta`,
		m.ID, m.Sender, m.Recipient, m.Body, m.TS, string(blob))
	return err
}

// refIDsFromJSON collects the non-empty attachment ids of one refs JSON array
// ([{id, mime, filename}, …] — chat meta["attachments"] and reply-card
// answer_attachments share the shape). Non-conforming JSON yields nothing.
func refIDsFromJSON(blob string, into map[string]bool) {
	var refs []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(blob), &refs); err != nil {
		return
	}
	for _, ref := range refs {
		if ref.ID != "" {
			into[ref.ID] = true
		}
	}
}

// DeleteChatInvolving HARD-deletes every message involving memberID (sender OR
// recipient) plus the attachment blobs those messages reference through their
// meta["attachments"] refs (the only linkage), so no blob is orphaned. A blob
// is deleted ONLY when no surviving record still references it: ref-form
// post_chat lets one blob ride several messages (and a reply-card blob —
// answer- or question-side — could be re-referenced from chat), so the cascade
// re-checks the survivors — surviving chat messages AND reply-card refs (both
// answer_attachments and the T-5e8a question-side attachments) — before
// dropping a blob.
// Returns (deletedMessages, deletedAttachments).
func (d *DAL) DeleteChatInvolving(memberID string) (int, int, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	candidates := map[string]bool{}
	if err := collectChatMetaRefs(tx,
		`SELECT meta FROM chat_message WHERE sender = ? OR recipient = ?`,
		candidates, memberID, memberID); err != nil {
		return 0, 0, err
	}

	res, err := tx.Exec(
		`DELETE FROM chat_message WHERE sender = ? OR recipient = ?`,
		memberID, memberID)
	if err != nil {
		return 0, 0, err
	}
	deletedMsgs, err := res.RowsAffected()
	if err != nil {
		return 0, 0, err
	}

	surviving := map[string]bool{}
	if len(candidates) > 0 {
		if err := collectChatMetaRefs(tx,
			`SELECT meta FROM chat_message`, surviving); err != nil {
			return 0, 0, err
		}
		// A card references blobs from BOTH sides: answer_attachments (the
		// owner's answer) AND attachments (the T-5e8a question-side refs the
		// card was opened with). Both keep a blob alive — the question refs
		// are also stamped into the companion chat message's meta, so the
		// companion's deletion puts them on the candidate list; the surviving
		// card must veto that.
		rows, err := tx.Query(`SELECT answer_attachments, attachments FROM reply_card`)
		if err != nil {
			return 0, 0, err
		}
		for rows.Next() {
			var answerBlob, questionBlob string
			if err := rows.Scan(&answerBlob, &questionBlob); err != nil {
				rows.Close()
				return 0, 0, err
			}
			refIDsFromJSON(answerBlob, surviving)
			refIDsFromJSON(questionBlob, surviving)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, 0, err
		}
		rows.Close()
	}

	var deletedAtts int64
	for id := range candidates {
		if surviving[id] {
			continue
		}
		res, err := tx.Exec(`DELETE FROM chat_attachment WHERE id = ?`, id)
		if err != nil {
			return 0, 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, 0, err
		}
		deletedAtts += n
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return int(deletedMsgs), int(deletedAtts), nil
}

// collectChatMetaRefs folds every attachment id referenced by the
// meta["attachments"] of the messages a query returns into `into`.
// Non-conforming meta (free-form JSON) contributes nothing.
func collectChatMetaRefs(tx *sql.Tx, query string, into map[string]bool, args ...any) error {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			return err
		}
		var meta struct {
			Attachments []struct {
				ID string `json:"id"`
			} `json:"attachments"`
		}
		if err := json.Unmarshal([]byte(blob), &meta); err == nil {
			for _, ref := range meta.Attachments {
				if ref.ID != "" {
					into[ref.ID] = true
				}
			}
		}
	}
	return rows.Err()
}

// ── chat attachments ─────────────────────────────────────────────────────────

// ChatAttachment mirrors the chat_attachment table (blob apart from the
// message; the message meta refs are the only linkage). Filename nil = pasted
// image with no name.
type ChatAttachment struct {
	ID       string
	Mime     string
	Data     []byte
	Filename *string
}

// PutChatAttachment stores an attachment blob (no SSE delta even at the
// service layer — the message record carries the light refs).
func (d *DAL) PutChatAttachment(a ChatAttachment) error {
	var filename any
	if a.Filename != nil {
		filename = *a.Filename
	}
	_, err := d.db.Exec(`
		INSERT INTO chat_attachment (id, mime, data, filename)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			mime = excluded.mime, data = excluded.data,
			filename = excluded.filename`,
		a.ID, a.Mime, a.Data, filename)
	return err
}

// GetChatAttachment returns one attachment blob by id, or nil if absent.
func (d *DAL) GetChatAttachment(id string) (*ChatAttachment, error) {
	var a ChatAttachment
	var filename sql.NullString
	err := d.db.QueryRow(
		`SELECT id, mime, data, filename FROM chat_attachment WHERE id = ?`, id,
	).Scan(&a.ID, &a.Mime, &a.Data, &filename)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if filename.Valid {
		a.Filename = &filename.String
	}
	return &a, nil
}

// ── chat read receipts (per-conversation last-read watermark) ────────────────

// ChatRead mirrors the chat_read table: one watermark per (reader, peer) —
// the composite PK is the natural identity.
type ChatRead struct {
	ReaderID   string
	PeerID     string
	LastReadTS float64
}

// ListChatReads returns read receipts, optionally filtered by reader and/or
// peer (empty string = no filter).
func (d *DAL) ListChatReads(reader, peer string) ([]ChatRead, error) {
	query := `SELECT reader_id, peer_id, last_read_ts FROM chat_read WHERE 1=1`
	var args []any
	if reader != "" {
		query += ` AND reader_id = ?`
		args = append(args, reader)
	}
	if peer != "" {
		query += ` AND peer_id = ?`
		args = append(args, peer)
	}
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatRead
	for rows.Next() {
		var r ChatRead
		if err := rows.Scan(&r.ReaderID, &r.PeerID, &r.LastReadTS); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutChatRead upserts a read receipt on the composite (reader, peer) key.
// MONOTONIC: the stored last_read_ts only ever ADVANCES — a stale/equal report
// is a no-op (never rewinds, so a re-ordered report can't un-read a message).
// Returns the EFFECTIVE (possibly pre-existing, higher) watermark plus whether
// the watermark actually ADVANCED (repository.put_chat_read parity: stale/equal
// = "no write, no fan", so the caller must not publish a delta then). The
// effective ts alone cannot distinguish "advanced to exactly ts" from "already
// at ts" — the write's row count carries the signal.
func (d *DAL) PutChatRead(r ChatRead) (ChatRead, bool, error) {
	res, err := d.db.Exec(`
		INSERT INTO chat_read (reader_id, peer_id, last_read_ts)
		VALUES (?, ?, ?)
		ON CONFLICT (reader_id, peer_id) DO UPDATE SET
			last_read_ts = excluded.last_read_ts
			WHERE excluded.last_read_ts > chat_read.last_read_ts`,
		r.ReaderID, r.PeerID, r.LastReadTS)
	if err != nil {
		return ChatRead{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ChatRead{}, false, err
	}
	var eff ChatRead
	err = d.db.QueryRow(`
		SELECT reader_id, peer_id, last_read_ts FROM chat_read
		WHERE reader_id = ? AND peer_id = ?`,
		r.ReaderID, r.PeerID,
	).Scan(&eff.ReaderID, &eff.PeerID, &eff.LastReadTS)
	return eff, n > 0, err
}

// DeleteChatReadsInvolving HARD-deletes every receipt involving memberID (as
// reader OR peer) — the custom-role cascade sibling of DeleteChatInvolving.
// Returns the deleted count.
func (d *DAL) DeleteChatReadsInvolving(memberID string) (int, error) {
	res, err := d.db.Exec(
		`DELETE FROM chat_read WHERE reader_id = ? OR peer_id = ?`,
		memberID, memberID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ── user-custom context block (single-row table) ─────────────────────────────

// UserContext mirrors the user_context table: the owner's user-custom ADDITIVE
// boot-context block (one row total; tombstoned = reset marker).
type UserContext struct {
	Text       string
	Tombstoned bool
}

// userContextRowID pins the single-row table (schema CHECK (id = 1)).
const userContextRowID = 1

// GetUserContext returns the block, or nil if never written (no row = the
// block is skipped when assembling boot context).
func (d *DAL) GetUserContext() (*UserContext, error) {
	var uc UserContext
	err := d.db.QueryRow(
		`SELECT text, tombstoned FROM user_context WHERE id = ?`, userContextRowID,
	).Scan(&uc.Text, &uc.Tombstoned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &uc, nil
}

// PutUserContext upserts the single block row.
func (d *DAL) PutUserContext(uc UserContext) error {
	_, err := d.db.Exec(`
		INSERT INTO user_context (id, text, tombstoned) VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			text = excluded.text, tombstoned = excluded.tombstoned`,
		userContextRowID, uc.Text, uc.Tombstoned)
	return err
}

// ── role definitions (overlay per role) ──────────────────────────────────────

// RoleDef mirrors the role_def table: the role-definition overlay a read-time
// fold lays over the file seed. Self-contained (name + definition_md);
// tombstoned = reset-to-seed marker.
type RoleDef struct {
	RoleKey      string
	Name         string
	DefinitionMD string
	Tombstoned   bool
}

// ListRoleDefs returns every overlay row (any tombstone state).
func (d *DAL) ListRoleDefs() ([]RoleDef, error) {
	rows, err := d.db.Query(
		`SELECT role_key, name, definition_md, tombstoned FROM role_def`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoleDef
	for rows.Next() {
		var rd RoleDef
		if err := rows.Scan(&rd.RoleKey, &rd.Name, &rd.DefinitionMD, &rd.Tombstoned); err != nil {
			return nil, err
		}
		out = append(out, rd)
	}
	return out, rows.Err()
}

// GetRoleDef returns one overlay by role key, or nil if never edited.
func (d *DAL) GetRoleDef(roleKey string) (*RoleDef, error) {
	var rd RoleDef
	err := d.db.QueryRow(
		`SELECT role_key, name, definition_md, tombstoned FROM role_def WHERE role_key = ?`,
		roleKey,
	).Scan(&rd.RoleKey, &rd.Name, &rd.DefinitionMD, &rd.Tombstoned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rd, nil
}

// PutRoleDef upserts a role-definition overlay.
func (d *DAL) PutRoleDef(rd RoleDef) error {
	_, err := d.db.Exec(`
		INSERT INTO role_def (role_key, name, definition_md, tombstoned)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (role_key) DO UPDATE SET
			name = excluded.name, definition_md = excluded.definition_md,
			tombstoned = excluded.tombstoned`,
		rd.RoleKey, rd.Name, rd.DefinitionMD, rd.Tombstoned)
	return err
}

// DeleteRoleDef PHYSICALLY deletes an overlay row (custom-role hard delete —
// a custom role has no file seed to fall back to) — NOT the tombstone reset
// (PutRoleDef with Tombstoned), which stays the seed-role reset seam.
// Returns true iff a row was deleted.
func (d *DAL) DeleteRoleDef(roleKey string) (bool, error) {
	res, err := d.db.Exec(`DELETE FROM role_def WHERE role_key = ?`, roleKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ── lessons (per-role; composite (role_key, task_type) key) ──────────────────

// Lessons mirrors the lessons table: the per-role learnings overlay (agents
// sharing a role share one doc). TaskType is currently a single fixed key.
type Lessons struct {
	RoleKey    string
	TaskType   string
	Text       string
	Tombstoned bool
}

// GetLessons returns the overlay for (roleKey, taskType), or nil if never
// edited.
func (d *DAL) GetLessons(roleKey, taskType string) (*Lessons, error) {
	var l Lessons
	err := d.db.QueryRow(`
		SELECT role_key, task_type, text, tombstoned FROM lessons
		WHERE role_key = ? AND task_type = ?`, roleKey, taskType,
	).Scan(&l.RoleKey, &l.TaskType, &l.Text, &l.Tombstoned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// PutLessons upserts a per-role lessons overlay.
func (d *DAL) PutLessons(l Lessons) error {
	_, err := d.db.Exec(`
		INSERT INTO lessons (role_key, task_type, text, tombstoned)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (role_key, task_type) DO UPDATE SET
			text = excluded.text, tombstoned = excluded.tombstoned`,
		l.RoleKey, l.TaskType, l.Text, l.Tombstoned)
	return err
}

// DeleteLessonsForRole HARD-deletes every overlay for roleKey (all task
// types) — the custom-role cascade: per-role lessons have no meaning without
// the role. Returns the deleted count.
func (d *DAL) DeleteLessonsForRole(roleKey string) (int, error) {
	res, err := d.db.Exec(`DELETE FROM lessons WHERE role_key = ?`, roleKey)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ── display-name overlays (account_alias / machine_alias) ────────────────────

// AccountAlias mirrors the account_alias table: account tag -> display name.
type AccountAlias struct {
	Account     string
	DisplayName string
}

// MachineAlias mirrors the machine_alias table: machine id (the warden's
// member.id) -> display name.
type MachineAlias struct {
	MachineID   string
	DisplayName string
}

// GetAccountAlias returns one overlay by account tag, or nil if never edited.
func (d *DAL) GetAccountAlias(account string) (*AccountAlias, error) {
	var a AccountAlias
	err := d.db.QueryRow(
		`SELECT account, display_name FROM account_alias WHERE account = ?`, account,
	).Scan(&a.Account, &a.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AccountDisplayNames maps account tag -> display_name (the fold input; empty
// display names are skipped — absence folds to the id itself).
func (d *DAL) AccountDisplayNames() (map[string]string, error) {
	return d.displayNames(
		`SELECT account, display_name FROM account_alias WHERE display_name != ''`)
}

// PutAccountAlias upserts an account display-name overlay.
func (d *DAL) PutAccountAlias(a AccountAlias) error {
	_, err := d.db.Exec(`
		INSERT INTO account_alias (account, display_name) VALUES (?, ?)
		ON CONFLICT (account) DO UPDATE SET display_name = excluded.display_name`,
		a.Account, a.DisplayName)
	return err
}

// GetMachineAlias returns one overlay by machine id, or nil if never edited.
func (d *DAL) GetMachineAlias(machineID string) (*MachineAlias, error) {
	var a MachineAlias
	err := d.db.QueryRow(
		`SELECT machine_id, display_name FROM machine_alias WHERE machine_id = ?`,
		machineID,
	).Scan(&a.MachineID, &a.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// MachineDisplayNames maps machine_id -> display_name (the fold input; empty
// display names are skipped).
func (d *DAL) MachineDisplayNames() (map[string]string, error) {
	return d.displayNames(
		`SELECT machine_id, display_name FROM machine_alias WHERE display_name != ''`)
}

// PutMachineAlias upserts a machine display-name overlay.
func (d *DAL) PutMachineAlias(a MachineAlias) error {
	_, err := d.db.Exec(`
		INSERT INTO machine_alias (machine_id, display_name) VALUES (?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET display_name = excluded.display_name`,
		a.MachineID, a.DisplayName)
	return err
}

// ── reply cards (等我回覆卡) ─────────────────────────────────────────────────

// ReplyCard mirrors the reply_card table (migrations/00003): one ask the owner
// must answer. Options is the frozen quick-reply wording ([0] = the AI pick);
// AnswerAttachments are light refs into the shared chat_attachment store, the
// same shape as chat meta["attachments"].
type ReplyCard struct {
	ID                string
	FromMember        string
	Kind              string // closed set: "decision" | "action" (schema CHECK)
	Summary           string
	Body              string
	Options           []string
	Status            string // "waiting" | "answered" | "expired" (closed set in code; migrations/00013 dropped the CHECK)
	CreatedTS         float64
	AnsweredTS        float64 // 0.0 while waiting; latest answer time after
	ExpiredTS         float64 // 0.0 unless expired; the owner's expire stamp
	ChatMessageID     string
	AnswerOptionIdx   *int // nil = free-text-only answer (or not answered yet)
	AnswerText        string
	AnswerAttachments []any // [{id, mime, filename}] refs (chat_attachment ids)
	// Attachments are the QUESTION-side refs the initiator opened the card
	// with (T-5e8a; migrations/00015) — the same [{id, mime, filename}] shape
	// into the same shared chat_attachment store as AnswerAttachments.
	Attachments []any
	// The M3 gate linkage (migrations/00004): the task/step this card was
	// armed FROM ("" = a plain chat 請示). Immutable birth marks — the step's
	// reply_card_id points the other way at the CURRENT card only.
	TaskID     string
	TaskStepID string
}

const replyCardColumns = `id, from_member, kind, summary, body, options,
	status, created_ts, answered_ts, expired_ts, chat_message_id,
	answer_option_idx, answer_text, answer_attachments, attachments,
	task_id, task_step_id`

func scanReplyCard(row interface{ Scan(...any) error }) (ReplyCard, error) {
	var c ReplyCard
	var options, answerAttachments, attachments string
	var optionIdx sql.NullInt64
	err := row.Scan(
		&c.ID, &c.FromMember, &c.Kind, &c.Summary, &c.Body, &options,
		&c.Status, &c.CreatedTS, &c.AnsweredTS, &c.ExpiredTS, &c.ChatMessageID,
		&optionIdx, &c.AnswerText, &answerAttachments, &attachments,
		&c.TaskID, &c.TaskStepID,
	)
	if err != nil {
		return ReplyCard{}, err
	}
	if err := json.Unmarshal([]byte(options), &c.Options); err != nil {
		return ReplyCard{}, fmt.Errorf("reply_card %s: bad options JSON: %w", c.ID, err)
	}
	if err := json.Unmarshal([]byte(answerAttachments), &c.AnswerAttachments); err != nil {
		return ReplyCard{}, fmt.Errorf("reply_card %s: bad answer_attachments JSON: %w", c.ID, err)
	}
	if err := json.Unmarshal([]byte(attachments), &c.Attachments); err != nil {
		return ReplyCard{}, fmt.Errorf("reply_card %s: bad attachments JSON: %w", c.ID, err)
	}
	if optionIdx.Valid {
		idx := int(optionIdx.Int64)
		c.AnswerOptionIdx = &idx
	}
	return c, nil
}

// ListReplyCards returns every card, oldest→newest (callers filter/sort per
// pane — the waiting/answered projections are handler concerns).
func (d *DAL) ListReplyCards() ([]ReplyCard, error) {
	rows, err := d.db.Query(
		`SELECT ` + replyCardColumns + ` FROM reply_card ORDER BY created_ts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReplyCard
	for rows.Next() {
		c, err := scanReplyCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetReplyCard returns one card by id, or nil if absent.
func (d *DAL) GetReplyCard(id string) (*ReplyCard, error) {
	row := d.db.QueryRow(
		`SELECT `+replyCardColumns+` FROM reply_card WHERE id = ?`, id)
	c, err := scanReplyCard(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// PutReplyCard upserts a card row (the SSE delta is the handler's job).
func (d *DAL) PutReplyCard(c ReplyCard) error {
	options := c.Options
	if options == nil {
		options = []string{}
	}
	answerAttachments := c.AnswerAttachments
	if answerAttachments == nil {
		answerAttachments = []any{}
	}
	attachments := c.Attachments
	if attachments == nil {
		attachments = []any{}
	}
	optionsBlob, err := json.Marshal(options)
	if err != nil {
		return err
	}
	answerAttachmentsBlob, err := json.Marshal(answerAttachments)
	if err != nil {
		return err
	}
	attachmentsBlob, err := json.Marshal(attachments)
	if err != nil {
		return err
	}
	var optionIdx any
	if c.AnswerOptionIdx != nil {
		optionIdx = *c.AnswerOptionIdx
	}
	_, err = d.db.Exec(`
		INSERT INTO reply_card (`+replyCardColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			from_member = excluded.from_member, kind = excluded.kind,
			summary = excluded.summary, body = excluded.body,
			options = excluded.options, status = excluded.status,
			created_ts = excluded.created_ts, answered_ts = excluded.answered_ts,
			expired_ts = excluded.expired_ts,
			chat_message_id = excluded.chat_message_id,
			answer_option_idx = excluded.answer_option_idx,
			answer_text = excluded.answer_text,
			answer_attachments = excluded.answer_attachments,
			attachments = excluded.attachments,
			task_id = excluded.task_id, task_step_id = excluded.task_step_id`,
		c.ID, c.FromMember, c.Kind, c.Summary, c.Body, string(optionsBlob),
		c.Status, c.CreatedTS, c.AnsweredTS, c.ExpiredTS, c.ChatMessageID,
		optionIdx, c.AnswerText, string(answerAttachmentsBlob),
		string(attachmentsBlob), c.TaskID, c.TaskStepID,
	)
	return err
}

// ── webhook endpoints (M4 回呼端點) ────────────────────────────────────────────

// WebhookStatus closed set — the revocation toggle (migrations/00007).
const (
	WebhookStatusEnabled  = "enabled"
	WebhookStatusDisabled = "disabled"
)

// WebhookPlatform closed set — the /in verification preset (migrations/00012).
// 'generic' is the pre-existing token-only behaviour; 'slack'/'github' apply
// that platform's signed-webhook HMAC. Fixed at creation (immutable).
const (
	WebhookPlatformGeneric = "generic"
	WebhookPlatformSlack   = "slack"
	WebhookPlatformGithub  = "github"
)

// WebhookDropReason closed set — the coarse classification stamped on
// last_drop_reason by the /in inlet's silent-drop paths (migrations/00014).
// An unknown token has no endpoint row to record against, by construction.
const (
	WebhookDropReasonSigFailed  = "sig_failed"
	WebhookDropReasonDisabled   = "disabled"
	WebhookDropReasonMemberGone = "member_gone"
)

// WebhookEndpoint mirrors the webhook_endpoint table: one external trigger
// inlet bound to a member. Token is the opaque secret + PK (the ONLY identity
// key /in consults); EndpointID is the user-chosen, per-member-unique,
// immutable address key.
type WebhookEndpoint struct {
	Token      string
	MemberID   string
	EndpointID string
	Purpose    string
	Status     string
	CreatedTS  float64
	// Platform is the /in verification preset (generic/slack/github); Platform
	// == generic keeps the token-only behaviour. SigningSecret is the write-only
	// HMAC shared secret (empty == none); it is NEVER echoed on any wire.
	Platform      string
	SigningSecret string
	// Observability counters (migrations/00014). LastReceivedTS is stamped by
	// ANY /in call resolving to this token (delivered or dropped alike);
	// DeliveredCount counts verified payloads that landed as a chat;
	// DroppedCount counts silent drops, LastDropReason their latest coarse
	// classification (WebhookDropReason* set). Owner-facing only — the /in
	// HTTP response never reflects them.
	LastReceivedTS float64
	DeliveredCount int64
	DroppedCount   int64
	LastDropReason string
}

const webhookColumns = `token, member_id, endpoint_id, purpose, status, created_ts, platform, signing_secret, last_received_ts, delivered_count, dropped_count, last_drop_reason`

func scanWebhook(row interface{ Scan(...any) error }) (WebhookEndpoint, error) {
	var e WebhookEndpoint
	// signing_secret is a nullable column (generic + pre-00012 rows carry NULL);
	// fold NULL → "" so the struct field stays a plain string.
	var signingSecret sql.NullString
	err := row.Scan(&e.Token, &e.MemberID, &e.EndpointID, &e.Purpose, &e.Status,
		&e.CreatedTS, &e.Platform, &signingSecret,
		&e.LastReceivedTS, &e.DeliveredCount, &e.DroppedCount, &e.LastDropReason)
	e.SigningSecret = signingSecret.String
	return e, err
}

// GetWebhookByToken returns the endpoint a token identifies, or nil when no
// row matches (the /in silent-drop path — an unknown token reveals nothing).
func (d *DAL) GetWebhookByToken(token string) (*WebhookEndpoint, error) {
	if token == "" {
		return nil, nil
	}
	row := d.db.QueryRow(
		`SELECT `+webhookColumns+` FROM webhook_endpoint WHERE token = ?`, token)
	e, err := scanWebhook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetWebhookByMemberEndpoint returns the endpoint addressed by (member,
// endpoint_id) — the management-route resolver — or nil when absent.
func (d *DAL) GetWebhookByMemberEndpoint(memberID, endpointID string) (*WebhookEndpoint, error) {
	row := d.db.QueryRow(
		`SELECT `+webhookColumns+` FROM webhook_endpoint
		 WHERE member_id = ? AND endpoint_id = ?`, memberID, endpointID)
	e, err := scanWebhook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListWebhooksByMember returns a member's endpoints, oldest→newest.
func (d *DAL) ListWebhooksByMember(memberID string) ([]WebhookEndpoint, error) {
	rows, err := d.db.Query(
		`SELECT `+webhookColumns+` FROM webhook_endpoint
		 WHERE member_id = ? ORDER BY created_ts`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookEndpoint
	for rows.Next() {
		e, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PutWebhookEndpoint upserts an endpoint row (keyed on the token PK). Create
// passes a fresh token; purpose/status edits re-put the SAME token. The
// per-member endpoint_id UNIQUE index rejects a duplicate id at the DB floor.
func (d *DAL) PutWebhookEndpoint(e WebhookEndpoint) error {
	// Empty SigningSecret writes SQL NULL (no secret) so has_signing_secret is
	// false; a non-empty value stores the shared secret verbatim.
	var signingSecret any
	if e.SigningSecret != "" {
		signingSecret = e.SigningSecret
	}
	platform := e.Platform
	if platform == "" {
		platform = WebhookPlatformGeneric
	}
	_, err := d.db.Exec(`
		INSERT INTO webhook_endpoint (`+webhookColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (token) DO UPDATE SET
			member_id = excluded.member_id, endpoint_id = excluded.endpoint_id,
			purpose = excluded.purpose, status = excluded.status,
			created_ts = excluded.created_ts, platform = excluded.platform,
			signing_secret = excluded.signing_secret,
			last_received_ts = excluded.last_received_ts,
			delivered_count = excluded.delivered_count,
			dropped_count = excluded.dropped_count,
			last_drop_reason = excluded.last_drop_reason`,
		e.Token, e.MemberID, e.EndpointID, e.Purpose, e.Status, e.CreatedTS,
		platform, signingSecret,
		e.LastReceivedTS, e.DeliveredCount, e.DroppedCount, e.LastDropReason)
	return err
}

// TouchWebhookReceived stamps last_received_ts only — the /in paths that prove
// the caller reached us but neither deliver nor drop (the Slack
// url_verification challenge, a verified GitHub ping).
func (d *DAL) TouchWebhookReceived(token string, ts float64) error {
	_, err := d.db.Exec(
		`UPDATE webhook_endpoint SET last_received_ts = ? WHERE token = ?`, ts, token)
	return err
}

// MarkWebhookDelivered counts one verified, chat-delivered payload (atomic
// increment — never a read-modify-write, so concurrent /in calls can't lose
// counts) and stamps last_received_ts.
func (d *DAL) MarkWebhookDelivered(token string, ts float64) error {
	_, err := d.db.Exec(
		`UPDATE webhook_endpoint
		 SET delivered_count = delivered_count + 1, last_received_ts = ?
		 WHERE token = ?`, ts, token)
	return err
}

// MarkWebhookDropped counts one silent drop with its coarse reason
// (WebhookDropReason* set) and stamps last_received_ts.
func (d *DAL) MarkWebhookDropped(token, reason string, ts float64) error {
	_, err := d.db.Exec(
		`UPDATE webhook_endpoint
		 SET dropped_count = dropped_count + 1, last_drop_reason = ?,
		     last_received_ts = ?
		 WHERE token = ?`, reason, ts, token)
	return err
}

// SetWebhookStatus flips one endpoint's status (the enable/disable toggle).
func (d *DAL) SetWebhookStatus(token, status string) error {
	_, err := d.db.Exec(
		`UPDATE webhook_endpoint SET status = ? WHERE token = ?`, status, token)
	return err
}

// DeleteWebhookEndpoint permanently revokes an endpoint (idempotent). Its
// request-log rows go with it — the ring buffer is debug data FOR an endpoint,
// never an orphaned archive of a dead token.
func (d *DAL) DeleteWebhookEndpoint(token string) error {
	if _, err := d.db.Exec(
		`DELETE FROM webhook_request_log WHERE token = ?`, token); err != nil {
		return err
	}
	_, err := d.db.Exec(`DELETE FROM webhook_endpoint WHERE token = ?`, token)
	return err
}

// WebhookRequestLog is one debug row of the per-endpoint /in ring buffer
// (migrations/00014): the raw request as received, with its resolved outcome
// ('delivered' | 'dropped:<reason>' | 'challenge' | 'ping'). Headers is the
// JSON-serialised header map (≤4 KiB), Body the raw payload text (≤16 KiB);
// Truncated marks that either was cut at its cap.
type WebhookRequestLog struct {
	TS        float64
	Outcome   string
	Headers   string
	Body      string
	Truncated bool
}

// webhookRequestLogKeep is the ring-buffer depth: only the newest N requests
// per endpoint survive — this is a debug peephole, not an audit archive.
const webhookRequestLogKeep = 5

// InsertWebhookRequestLog appends one /in request row and trims the endpoint's
// ring buffer to the newest webhookRequestLogKeep rows (id order = insert
// order; the AUTOINCREMENT id is the ring's clock).
func (d *DAL) InsertWebhookRequestLog(token string, l WebhookRequestLog) error {
	if _, err := d.db.Exec(`
		INSERT INTO webhook_request_log (token, ts, outcome, headers, body, truncated)
		VALUES (?, ?, ?, ?, ?, ?)`,
		token, l.TS, l.Outcome, l.Headers, l.Body, l.Truncated); err != nil {
		return err
	}
	_, err := d.db.Exec(`
		DELETE FROM webhook_request_log
		WHERE token = ? AND id NOT IN (
			SELECT id FROM webhook_request_log
			WHERE token = ? ORDER BY id DESC LIMIT ?)`,
		token, token, webhookRequestLogKeep)
	return err
}

// ListWebhookRequestLogs returns an endpoint's ring buffer, newest→oldest
// (at most webhookRequestLogKeep rows by construction; LIMIT is belt-and-braces).
func (d *DAL) ListWebhookRequestLogs(token string) ([]WebhookRequestLog, error) {
	rows, err := d.db.Query(`
		SELECT ts, outcome, headers, body, truncated FROM webhook_request_log
		WHERE token = ? ORDER BY id DESC LIMIT ?`,
		token, webhookRequestLogKeep)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookRequestLog
	for rows.Next() {
		var l WebhookRequestLog
		if err := rows.Scan(&l.TS, &l.Outcome, &l.Headers, &l.Body, &l.Truncated); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ── settings ─────────────────────────────────────────────────────────────────

// GetSetting returns one settings value by key, or nil when the key was never
// written (the code-side default then applies — see settings.go for the
// closed key set).
func (d *DAL) GetSetting(key string) (*string, error) {
	var v string
	err := d.db.QueryRow(`SELECT value FROM setting WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// PutSetting upserts one settings value, stamping updated_at.
func (d *DAL) PutSetting(key, value string) error {
	_, err := d.db.Exec(`
		INSERT INTO setting (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, nowSecs())
	return err
}

// DeleteSetting removes one settings value (idempotent — deleting an absent
// key is a no-op). Consumes the one-shot first-run claim token.
func (d *DAL) DeleteSetting(key string) error {
	_, err := d.db.Exec(`DELETE FROM setting WHERE key = ?`, key)
	return err
}

func (d *DAL) displayNames(query string) (map[string]string, error) {
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return nil, err
		}
		out[key] = name
	}
	return out, rows.Err()
}
