package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// wdb is the write pool: ONE connection, transactions BEGIN IMMEDIATE
// (openSQLite in migrate.go, `_txlock=immediate`). rdb is the read pool, opened
// mode=ro (openSQLiteReadPool), so a write wired to rdb — which type-checks,
// since *sql.DB satisfies sqlExecer — fails loudly instead of landing.
type DAL struct {
	wdb *sql.DB
	rdb *sql.DB
}

type PushSubscription struct {
	Endpoint       string
	P256dh         string
	Auth           string
	ExpirationTime *float64
}

// NewDAL is for unit tests and the CLI one-shots (migrate / set-password /
// claim-token): reads and writes share ONE connection, so a read via s.dal or
// d.rdb inside an inTx closure waits on that connection forever, hanging with no
// error. serve splits the pools (NewDALPools), so this never happens there.
func NewDAL(db *sql.DB) *DAL {
	return &DAL{wdb: db, rdb: db}
}

func NewDALPools(w, r *sql.DB) *DAL {
	return &DAL{wdb: w, rdb: r}
}

type Member struct {
	ID          string
	Name        string
	Kind        string
	RoleKey     string
	Runtime     string
	Model       string
	ActualModel string
	Effort      string
	// ActualRuntime / ActualEffort: "" = never reported. Never fall back to
	// Runtime / Effort: a stand-in is indistinguishable from a change that
	// already took effect.
	ActualRuntime    string
	ActualEffort     string
	DesiredState     string
	DesiredMachineID string
	// LastMachineID: the machine the last confirmed session connected from
	// (stamped in onFirstConnect), "" = never. Read by the outsource spawn
	// placement chain (a preference below the owner pin) and by shutdown.go
	// killTargetChain.
	LastMachineID string
	// SessionBootTS is the durable twin of the in-memory gauge's boot_ts, written
	// by the same functions (onFirstConnect / clearSessionBootTS /
	// restoreRefusedStartAnchor). It must be durable: a station re-exec empties the
	// gauge but agents survive and reconnect, and a fresh anchor would make an
	// hours-old session look seconds old to the min-liveness floor (restart_self).
	SessionBootTS float64
	WakingSince   float64
	StoppingSince float64
	StoppedSince  float64
	RefocusSince  float64
	// RefocusOp: the closed set is refocusOp* in member_ownerop_winddown.go, and
	// winddownKindFor decides what each value means.
	RefocusOp string
	// ForcedStopAt: unix secs of the last force-stop, 0 = never. Unlike the other
	// lifecycle anchors it is NOT cleared on the next boot: it describes the
	// previous session, which is who reads it (forcedEpochLive).
	ForcedStopAt float64
	// RestartAfterStop: start again once converged offline — independent of
	// DesiredState (how hard to stop). Last writer wins by design (every stop verb
	// clears it, every restart verb sets it), unlike the wind-down ladder, which is
	// a ratchet. Consumed by consumeRestartAfterStop.
	RestartAfterStop bool
	// HandoverNoticedTS: the session anchor whose one advance handover notice was
	// sent, 0 = none. An anchor, not a flag, so a new session earns its own notice
	// while a reconnect stays quiet; durable because a station re-exec empties the
	// in-memory claim while the agents survive it.
	HandoverNoticedTS float64
	// AgentIatFloor: iat of the token that last reported waking; requireAuth
	// refuses agent tokens whose iat is strictly below it. It holds the waking
	// caller's OWN iat, not now(), so the session raising it stays valid whatever
	// the mint/boot gap or clock skew. Not consulted for wardens
	// (authz.go agentIatFloorRefusal).
	AgentIatFloor float64
	// TokenKeyID: the signing key (keyring.go) this station last verified a
	// credential of this member's with — the station's own observation, never a
	// self-report. Recorded only in api_infra.go's SSE handler after hub.Connect,
	// not at the auth gate: renew-credential lets a machine mint and present a
	// fresh credential once while still running on the old one. On the wire only
	// as MachineDTO.token_key_id.
	TokenKeyID         string
	BankedCost         float64
	LastOp             string
	LastOpOK           *bool
	LastOpLog          string
	LastOpReason       string
	LastOpAt           float64
	RosterStatus       string
	LinkedTaskID       *string
	Codename           string
	CreatedTS          float64
	ReleasedTS         float64
	ActivatedTS        float64
	AvatarAttachmentID string
	// TaskID and Status stay empty on a Member; only the OutsourceWorker view
	// fills them. They live here so
	// that conversion stays a plain type conversion.
	TaskID string
	Status string
}

const (
	RosterStatusActive  = "active"
	RosterStatusRemoved = "removed"
)

const memberColumns = `id, name, kind, role_key, runtime, model, actual_model, effort,
	actual_runtime, actual_effort,
	desired_state, desired_machine_id, last_machine_id, session_boot_ts,
	waking_since, stopping_since, stopped_since, refocus_since, refocus_op, banked_cost,
	last_op, last_op_ok, last_op_log, last_op_reason, last_op_at, roster_status,
	linked_task_id, codename, created_ts, released_ts, activated_ts,
	avatar_attachment_id, forced_stop_at, handover_noticed_ts, agent_iat_floor,
	restart_after_stop, token_key_id`

func scanMember(row interface{ Scan(...any) error }) (Member, error) {
	var m Member
	var lastOpOK sql.NullBool
	var linkedTaskID, codename sql.NullString
	err := row.Scan(
		&m.ID, &m.Name, &m.Kind, &m.RoleKey, &m.Runtime, &m.Model, &m.ActualModel, &m.Effort,
		&m.ActualRuntime, &m.ActualEffort,
		&m.DesiredState, &m.DesiredMachineID, &m.LastMachineID, &m.SessionBootTS,
		&m.WakingSince, &m.StoppingSince, &m.StoppedSince, &m.RefocusSince, &m.RefocusOp,
		&m.BankedCost,
		&m.LastOp, &lastOpOK, &m.LastOpLog, &m.LastOpReason, &m.LastOpAt, &m.RosterStatus,
		&linkedTaskID, &codename, &m.CreatedTS, &m.ReleasedTS, &m.ActivatedTS,
		&m.AvatarAttachmentID, &m.ForcedStopAt, &m.HandoverNoticedTS, &m.AgentIatFloor,
		&m.RestartAfterStop, &m.TokenKeyID,
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

func (d *DAL) ListMembers() ([]Member, error) {
	rows, err := d.rdb.Query(`SELECT ` + memberColumns +
		` FROM member ORDER BY name COLLATE NOCASE`)
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

func (d *DAL) GetMember(id string) (*Member, error) {
	row := d.rdb.QueryRow(`SELECT `+memberColumns+` FROM member WHERE id = ?`, id)
	m, err := scanMember(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// Account spend is its own accumulator, NOT a fold over the account's members:
// owner ruling rc-5c5d7c7c6dcd (account and member figures clear independently).
func (d *DAL) AddAccountSpend(account string, delta float64) error {
	if account == "" || delta <= 0 {
		return nil
	}
	_, err := d.wdb.Exec(`INSERT INTO account_spend (account, accumulated)
		VALUES (?, ?)
		ON CONFLICT(account) DO UPDATE SET accumulated = accumulated + excluded.accumulated`,
		account, delta)
	return err
}

func (d *DAL) ListAccountSpend() (map[string]float64, error) {
	rows, err := d.rdb.Query(`SELECT account, accumulated FROM account_spend`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var account string
		var accumulated float64
		if err := rows.Scan(&account, &accumulated); err != nil {
			return nil, err
		}
		out[account] = accumulated
	}
	return out, rows.Err()
}

func (d *DAL) ZeroAccountSpend(account string) (float64, error) {
	var had float64
	err := d.inTx(func(tx *sql.Tx) error {
		switch err := tx.QueryRow(`SELECT accumulated FROM account_spend WHERE account = ?`,
			account).Scan(&had); {
		case err == sql.ErrNoRows:
			had = 0
			return nil
		case err != nil:
			return err
		}
		_, err := tx.Exec(`UPDATE account_spend SET accumulated = 0 WHERE account = ?`, account)
		return err
	})
	if err != nil {
		return 0, err
	}
	return had, nil
}

// PutMember is a whole-row write: INSERT … DO NOTHING lands every column on a
// new row; on an existing row only the columns NOT flagged insertOnly in
// dal_member_patch.go are written. So it silently cannot move an insertOnly
// column (model, runtime, effort, desired_machine_id, banked_cost, last_op*,
// the wind-down anchors, …) — each has its own single-column setter.
// It fans no SSE delta; s.putMember pairs the write with publishMemberPatch.
func (d *DAL) PutMember(m Member) error {
	fields := memberWholeRow(m)
	return d.inTx(func(tx *sql.Tx) error {
		if err := insertMemberRowIfAbsent(tx, fields); err != nil {
			return err
		}
		return patchMemberOn(tx, m.ID, updatableMemberFields(fields)...)
	})
}

func (d *DAL) AddMemberBankedCost(id string, delta float64) error {
	_, err := d.wdb.Exec(
		`UPDATE member SET banked_cost = banked_cost + ? WHERE id = ?`, delta, id)
	return err
}

func (d *DAL) ZeroMemberBankedCost(id string) (float64, error) {
	var had float64
	err := d.inTx(func(tx *sql.Tx) error {
		switch err := tx.QueryRow(`SELECT banked_cost FROM member WHERE id = ?`, id).Scan(&had); {
		case err == sql.ErrNoRows:
			had = 0
			return nil
		case err != nil:
			return err
		}
		_, err := tx.Exec(`UPDATE member SET banked_cost = 0 WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	return had, nil
}

func (d *DAL) SetMemberHandoverNoticedTS(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE member SET handover_noticed_ts = ? WHERE id = ?`, ts, id)
	return err
}

// SetMemberForcedStopAt is forward-only (mfForcedStopAt): a value below the
// stored one, including 0, is a silent no-op — this seam cannot clear the record.
func (d *DAL) SetMemberForcedStopAt(id string, ts float64) error {
	return d.PatchMember(id, mfForcedStopAt(ts))
}

func (d *DAL) SetMemberSessionBootTS(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE member SET session_boot_ts = ? WHERE id = ?`, ts, id)
	return err
}

func (d *DAL) SetMemberWakingSince(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE member SET waking_since = ? WHERE id = ?`, ts, id)
	return err
}

func (d *DAL) SetMemberWindDownAnchors(id string, stoppingSince, stoppedSince,
	refocusSince float64, refocusOp string) error {
	_, err := d.wdb.Exec(
		`UPDATE member SET stopping_since = ?, stopped_since = ?,
			refocus_since = ?, refocus_op = ? WHERE id = ?`,
		stoppingSince, stoppedSince, refocusSince, refocusOp, id)
	return err
}

func (d *DAL) SetMemberDesiredMachineID(id, machineID string) error {
	_, err := d.wdb.Exec(
		`UPDATE member SET desired_machine_id = ? WHERE id = ?`, machineID, id)
	return err
}

func (d *DAL) SetMemberModel(id, model string) error {
	_, err := d.wdb.Exec(`UPDATE member SET model = ? WHERE id = ?`, model, id)
	return err
}

// SetMemberRuntime stores runtime raw, WITHOUT NormalizeRuntime: "" ("nobody
// has picked yet") must stay distinct from "claude" for
// resolveEmptyRuntimeForPlacement.
func (d *DAL) SetMemberRuntime(id, runtime string) error {
	_, err := d.wdb.Exec(`UPDATE member SET runtime = ? WHERE id = ?`, runtime, id)
	return err
}

func (d *DAL) SetMemberEffort(id, effort string) error {
	_, err := d.wdb.Exec(`UPDATE member SET effort = ? WHERE id = ?`, effort, id)
	return err
}

func (d *DAL) SetMemberLastOp(id, op string, ok *bool, log, reason string, at float64) error {
	var okVal any
	if ok != nil {
		okVal = *ok
	}
	_, err := d.wdb.Exec(
		`UPDATE member SET last_op = ?, last_op_ok = ?, last_op_log = ?,
			last_op_reason = ?, last_op_at = ? WHERE id = ?`,
		op, okVal, log, reason, at, id)
	return err
}

func (d *DAL) HardDeleteMember(id string) (bool, error) {
	tx, err := d.wdb.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var avatarID string
	err = tx.QueryRow(`SELECT avatar_attachment_id FROM member WHERE id = ?`, id).Scan(&avatarID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM member WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	if avatarID != "" {
		// Avatar blobs are dedicated (ava- ids, kept out of the general attachment
		// graph), which is why
		// Replace/DeleteMemberAvatar drop the old blob outright; this survivor check
		// only guards against legacy/corrupt cross-references.
		surviving := map[string]bool{}
		if err := collectSurvivingBlobRefs(tx, surviving); err != nil {
			return false, err
		}
		if !surviving[avatarID] {
			if _, err := tx.Exec(`DELETE FROM chat_attachment WHERE id = ?`, avatarID); err != nil {
				return false, err
			}
		}
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

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

func (d *DAL) ListChat() ([]ChatMessage, error) {
	rows, err := d.rdb.Query(
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

type chatListFilter struct {
	participant string
	sender      string
	recipient   string
}

func (f chatListFilter) appendSQL(query *string, args *[]any, col string) {
	if f.sender != "" {
		*query += ` AND ` + col + `sender = ?`
		*args = append(*args, f.sender)
	}
	if f.recipient != "" {
		*query += ` AND ` + col + `recipient = ?`
		*args = append(*args, f.recipient)
	}
}

// Do not fold the participant branches into `sender = ? OR recipient = ?`: the
// planner answers the OR with a multi-index OR plus a temp sort (measured: the
// busiest participant's latest page went 0.09 ms -> 33 ms). Keep `where`
// cursors in row-value form ((ts, id) < (?, ?)) so they stay index ranges.
func (f chatListFilter) selectSQL(from, where string, args []any, col string, desc bool, limit int) (string, []any) {
	dir := ""
	if desc {
		dir = " DESC"
	}
	branch := func(side string) (string, []any) {
		q := `SELECT ` + col + `id, ` + col + `sender, ` + col + `recipient, ` +
			col + `body, ` + col + `ts, ` + col + `meta FROM ` + from + ` WHERE (` + where + `)`
		a := append([]any(nil), args...)
		if side != "" {
			q += ` AND ` + col + side + ` = ?`
			a = append(a, f.participant)
		}
		f.appendSQL(&q, &a, col)
		q += ` ORDER BY ` + col + `ts` + dir + `, ` + col + `id` + dir
		if limit >= 0 {
			q += ` LIMIT ?`
			a = append(a, limit)
		}
		return q, a
	}
	if f.participant == "" {
		return branch("")
	}
	sq, sa := branch("sender")
	rq, ra := branch("recipient")
	q := `SELECT * FROM (` + sq + `) UNION SELECT * FROM (` + rq + `) ORDER BY ts` + dir + `, id` + dir
	a := append(sa, ra...)
	if limit >= 0 {
		q += ` LIMIT ?`
		a = append(a, limit)
	}
	return q, a
}

func (d *DAL) queryChats(query string, args []any, reverse bool) ([]ChatMessage, error) {
	rows, err := d.rdb.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var got []ChatMessage
	for rows.Next() {
		m, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		got = append(got, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !reverse {
		return got, nil
	}
	out := make([]ChatMessage, len(got))
	for i, m := range got {
		out[len(got)-1-i] = m
	}
	return out, nil
}

func (d *DAL) ListChatBefore(participant string, beforeTS float64, beforeID string, limit int) ([]ChatMessage, error) {
	return d.listChatBefore(chatListFilter{participant: participant}, beforeTS, beforeID, limit)
}

func (d *DAL) listChatBefore(f chatListFilter, beforeTS float64, beforeID string, limit int) ([]ChatMessage, error) {
	if limit == 0 {
		return nil, nil
	}
	query, args := f.selectSQL(`chat_message`, `(ts, id) < (?, ?)`,
		[]any{beforeTS, beforeID}, "", true, limit)
	return d.queryChats(query, args, true)
}

type chatAnchor struct {
	TS float64
	ID string
}

func (a chatAnchor) newerThan(b chatAnchor) bool {
	if a.TS != b.TS {
		return a.TS > b.TS
	}
	return a.ID > b.ID
}

// listChatWindow: with both anchors the window is anchored at end, so an
// over-wide window is truncated from the start_id side — the spec's own rule
// for that case (end_id in spec/openapi.json), not the start_id paragraph's.
func (d *DAL) listChatWindow(f chatListFilter, start, end *chatAnchor, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	where := `1=1`
	var args []any
	if start != nil {
		where += ` AND (ts, id) >= (?, ?)`
		args = append(args, start.TS, start.ID)
	}
	if end != nil {
		where += ` AND (ts, id) <= (?, ?)`
		args = append(args, end.TS, end.ID)
	}
	descending := end != nil
	query, args := f.selectSQL(`chat_message`, where, args, "", descending, limit)
	return d.queryChats(query, args, descending)
}

func (d *DAL) ListChatLatest(participant string, limit int) ([]ChatMessage, error) {
	return d.listChatLatest(chatListFilter{participant: participant}, limit)
}

func (d *DAL) listChatLatest(f chatListFilter, limit int) ([]ChatMessage, error) {
	if limit == 0 {
		return nil, nil
	}
	desc := limit > 0
	query, args := f.selectSQL(`chat_message`, `1=1`, nil, "", desc, limit)
	return d.queryChats(query, args, desc)
}

// listChatUnread does not exclude messages the reader sent to itself: owner
// ruling rc-dccab860be32 (hiding them is the printer's call — ocagent).
func (d *DAL) listChatUnread(reader string, f chatListFilter, after *chatAnchor, limit int) ([]ChatMessage, error) {
	if reader == "" || limit == 0 {
		return nil, nil
	}
	query := `
		SELECT m.id, m.sender, m.recipient, m.body, m.ts, m.meta FROM chat_message m
		LEFT JOIN chat_read r ON r.reader_id = ? AND r.peer_id = m.sender
		WHERE m.recipient = ? AND m.ts > COALESCE(r.last_read_ts, 0)`
	args := []any{reader, reader}
	if after != nil {
		query += ` AND (m.ts, m.id) > (?, ?)`
		args = append(args, after.TS, after.ID)
	}
	bySender := f.sender != ""
	if f.participant != "" && f.participant != reader {
		query += ` AND m.sender = ?`
		args = append(args, f.participant)
		bySender = true
	}
	f.appendSQL(&query, &args, "m.")
	// With a sender fixed, `+m.ts` keeps the planner on
	// idx_chat_message_recipient_sender_ts (plain m.ts: 4.7 ms -> 25 ms). Without
	// one, plain m.ts lets a capped page stop early on
	// idx_chat_message_recipient_ts (2.5 ms vs 20-45 ms with `+`).
	if bySender {
		query += ` ORDER BY +m.ts, m.id`
	} else {
		query += ` ORDER BY m.ts, m.id`
	}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	return d.queryChats(query, args, false)
}

// ListChatByIDs is deliberately caller-blind: owner ruling T-4e95 — a by-id
// read reaches exactly as far as the listing, which filters on participant.
func (d *DAL) ListChatByIDs(ids []string) ([]ChatMessage, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := d.rdb.Query(`
		SELECT id, sender, recipient, body, ts, meta FROM chat_message
		WHERE id IN (`+strings.Join(placeholders, ", ")+`)
		ORDER BY ts, id`, args...)
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

// ListChatInvolving is a global newest-N, not a per-conversation-line quota:
// owner ruling 「不要管每條對話線」 (the wake-snapshot packer walks it
// newest-first until its budget).
func (d *DAL) ListChatInvolving(participant string, limit int) ([]ChatMessage, error) {
	if participant == "" || limit <= 0 {
		return nil, nil
	}
	query, args := chatListFilter{participant: participant}.selectSQL(
		`chat_message`, `1=1`, nil, "", true, limit)
	return d.queryChats(query, args, true)
}

type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// sqlRowQuerier lets a Snapshot read the pre-write state inside the transaction
// that overwrites it. Reading it from d.rdb instead is a different point in
// time: two concurrent writers would retain the same old revision and lose one
// result from history.
type sqlRowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

type DocumentHistory struct {
	ID           int64
	DocumentKind string
	DocumentKey  string
	ContentJSON  string
	CreatedTS    float64
	ActorID      string
}

const documentHistoryKeepDefault = 3

// Ten is the owner's number for the owner-editable boot-context documents and
// event procedures; every other kind stays at the default on purpose.
var documentHistoryKeepByKind = map[string]int{
	docKindSystemInteraction:           10,
	docKindBootSequence:                10,
	docKindOffboard:                    10,
	docKindAcceleratedStop:             10,
	docKindTaskCloseout:                10,
	docKindTaskReassignPredecessor:     10,
	docKindTaskTakeoverWithPredecessor: 10,
	docKindTaskUnblocked:               10,
	docKindTaskReadyForDone:            10,
}

func documentHistoryKeepFor(kind string) int {
	if keep, ok := documentHistoryKeepByKind[kind]; ok {
		return keep
	}
	return documentHistoryKeepDefault
}

type documentHistoryStream struct {
	Kind     string
	Key      string
	ActorID  string
	Snapshot func(sqlRowQuerier) (string, error)
}

func (d *DAL) SaveWithDocumentHistory(kind, key, actorID string, snapshot func(sqlRowQuerier) (string, error), write func(sqlExecer) error) error {
	return d.SaveWithDocumentHistories([]documentHistoryStream{
		{Kind: kind, Key: key, ActorID: actorID, Snapshot: snapshot},
	}, write)
}

func (d *DAL) SaveWithDocumentHistories(streams []documentHistoryStream, write func(sqlExecer) error) error {
	return d.inTx(func(tx *sql.Tx) error {
		for _, stream := range streams {
			if err := retainDocumentVersion(tx, stream); err != nil {
				return err
			}
		}
		return write(tx)
	})
}

func retainDocumentVersion(tx *sql.Tx, stream documentHistoryStream) error {
	currentJSON, err := stream.Snapshot(tx)
	if err != nil {
		return err
	}
	if currentJSON == "" || currentJSON == "{}" {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO document_history
		(document_kind, document_key, content_json, created_ts, actor_id)
		VALUES (?, ?, ?, ?, ?)`, stream.Kind, stream.Key, currentJSON, nowSecs(), stream.ActorID); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM document_history
		WHERE document_kind = ? AND document_key = ? AND id NOT IN (
			SELECT id FROM document_history
			WHERE document_kind = ? AND document_key = ?
			ORDER BY id DESC LIMIT ?
		)`, stream.Kind, stream.Key, stream.Kind, stream.Key, documentHistoryKeepFor(stream.Kind))
	return err
}

func (d *DAL) ListDocumentHistory(kind, key string) ([]DocumentHistory, error) {
	rows, err := d.rdb.Query(`SELECT id, document_kind, document_key, content_json, created_ts, actor_id
		FROM document_history WHERE document_kind = ? AND document_key = ? ORDER BY id DESC`, kind, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DocumentHistory
	for rows.Next() {
		var h DocumentHistory
		if err := rows.Scan(&h.ID, &h.DocumentKind, &h.DocumentKey, &h.ContentJSON, &h.CreatedTS, &h.ActorID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (d *DAL) GetDocumentHistory(kind, key string, id int64) (*DocumentHistory, error) {
	var h DocumentHistory
	err := d.rdb.QueryRow(`SELECT id, document_kind, document_key, content_json, created_ts, actor_id
		FROM document_history WHERE document_kind = ? AND document_key = ? AND id = ?`, kind, key, id).
		Scan(&h.ID, &h.DocumentKind, &h.DocumentKey, &h.ContentJSON, &h.CreatedTS, &h.ActorID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (d *DAL) PutChat(m ChatMessage) error { return putChatOn(d.wdb, m) }

func putChatOn(ex sqlExecer, m ChatMessage) error {
	meta := m.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	blob, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = ex.Exec(`
		INSERT INTO chat_message (id, sender, recipient, body, ts, meta)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			sender = excluded.sender, recipient = excluded.recipient,
			body = excluded.body, ts = excluded.ts, meta = excluded.meta`,
		m.ID, m.Sender, m.Recipient, m.Body, m.TS, string(blob))
	return err
}

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

func (d *DAL) DeleteChatInvolving(memberID string) (int, int, error) {
	tx, err := d.wdb.Begin()
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

	deletedAtts, err := collectOrphanBlobs(tx, candidates)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return int(deletedMsgs), deletedAtts, nil
}

func collectOrphanBlobs(tx *sql.Tx, candidates map[string]bool) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}
	surviving := map[string]bool{}
	if err := collectSurvivingBlobRefs(tx, surviving); err != nil {
		return 0, err
	}
	var deleted int64
	for id := range candidates {
		if surviving[id] {
			continue
		}
		res, err := tx.Exec(`DELETE FROM chat_attachment WHERE id = ?`, id)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		deleted += n
	}
	return int(deleted), nil
}

// collectSurvivingBlobRefs is the only liveness verdict for chat_attachment
// blobs (collectOrphanBlobs, HardDeleteMember, dal_task_artifacts.go). A NEW
// non-derived column holding blob ids MUST be added here: a blob whose referrer
// this scan does not know is deleted under it, silently.
// chat_attachment_ref.attachment_id deliberately does NOT vote — it is a
// trigger-maintained index over chat_message.meta, so voting would keep a blob
// alive on the strength of its own referrer.
func collectSurvivingBlobRefs(tx *sql.Tx, into map[string]bool) error {
	if err := collectChatMetaRefs(tx, `SELECT meta FROM chat_message`, into); err != nil {
		return err
	}

	rows, err := tx.Query(`SELECT answer_attachments, attachments FROM reply_card`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var answerBlob, questionBlob string
		if err := rows.Scan(&answerBlob, &questionBlob); err != nil {
			rows.Close()
			return err
		}
		refIDsFromJSON(answerBlob, into)
		refIDsFromJSON(questionBlob, into)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// COALESCE here and in the task_artifact_history query is NOT a fail-safe:
	// both columns are NOT NULL today, and on a NULL the row stops voting either
	// way. If either column becomes nullable, the predicate must become
	// `attachment_id IS NULL OR attachment_id <> ''`.
	artRows, err := tx.Query(
		`SELECT attachment_id FROM task_artifact
		 WHERE COALESCE(attachment_id, '') <> ''`)
	if err != nil {
		return err
	}
	for artRows.Next() {
		var id string
		if err := artRows.Scan(&id); err != nil {
			return err
		}
		into[id] = true
	}
	if err := artRows.Err(); err != nil {
		artRows.Close()
		return err
	}
	artRows.Close()

	memberRows, err := tx.Query(
		`SELECT avatar_attachment_id FROM member
		 WHERE COALESCE(avatar_attachment_id, '') <> ''`)
	if err != nil {
		return err
	}
	for memberRows.Next() {
		var id string
		if err := memberRows.Scan(&id); err != nil {
			memberRows.Close()
			return err
		}
		into[id] = true
	}
	if err := memberRows.Err(); err != nil {
		memberRows.Close()
		return err
	}
	memberRows.Close()

	histRows, err := tx.Query(
		`SELECT attachment_id FROM task_artifact_history
		 WHERE COALESCE(attachment_id, '') <> ''`)
	if err != nil {
		return err
	}
	defer histRows.Close()
	for histRows.Next() {
		var id string
		if err := histRows.Scan(&id); err != nil {
			return err
		}
		into[id] = true
	}
	return histRows.Err()
}

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

// chat_attachment_ref is written ONLY by three triggers on chat_message
// (migration 00074); do not add a Go write path. Ord is not dense: refs without
// an id leave holes.
type ChatAttachmentRef struct {
	MessageID    string
	Ord          int
	AttachmentID string
	Sender       string
	Recipient    string
	TS           float64
	Mime         string
	Filename     string
}

const chatAttachmentRefColumns = `message_id, ord, attachment_id, sender, recipient, ts, mime, filename`

// Equal-ts messages order by ASCENDING id on purpose: that is the order the
// gallery showed before the index existed; flipping it is a visible change.
func chatAttachmentRefSortsFirst(a, b ChatAttachmentRef) bool {
	if a.TS != b.TS {
		return a.TS > b.TS
	}
	if a.MessageID != b.MessageID {
		return a.MessageID < b.MessageID
	}
	return a.Ord < b.Ord
}

func (d *DAL) listChatAttachmentRefsOneSided(column, peer, extra string) ([]ChatAttachmentRef, error) {
	rows, err := d.rdb.Query(
		`SELECT `+chatAttachmentRefColumns+` FROM chat_attachment_ref
		 WHERE `+column+` = ?`+extra+`
		 ORDER BY ts DESC, message_id ASC, ord ASC`, peer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatAttachmentRef
	for rows.Next() {
		var r ChatAttachmentRef
		if err := rows.Scan(&r.MessageID, &r.Ord, &r.AttachmentID,
			&r.Sender, &r.Recipient, &r.TS, &r.Mime, &r.Filename); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Two single-sided queries merged in Go, not `sender = ? OR recipient = ?`:
// measured, the OR form gets a multi-index OR plus a temp b-tree over every
// matching row, while each single-sided query is served in index order.
func (d *DAL) ListChatAttachmentRefsFor(peer string) ([]ChatAttachmentRef, error) {
	sent, err := d.listChatAttachmentRefsOneSided("sender", peer, "")
	if err != nil {
		return nil, err
	}
	received, err := d.listChatAttachmentRefsOneSided(
		"recipient", peer, " AND sender <> recipient")
	if err != nil {
		return nil, err
	}
	out := make([]ChatAttachmentRef, 0, len(sent)+len(received))
	i, j := 0, 0
	for i < len(sent) && j < len(received) {
		if chatAttachmentRefSortsFirst(received[j], sent[i]) {
			out = append(out, received[j])
			j++
			continue
		}
		out = append(out, sent[i])
		i++
	}
	out = append(out, sent[i:]...)
	return append(out, received[j:]...), nil
}

type ChatAttachment struct {
	ID       string
	Mime     string
	Data     []byte
	Filename *string
}

func (d *DAL) PutChatAttachment(a ChatAttachment) error {
	return putChatAttachmentOn(d.wdb, a)
}

func putChatAttachmentOn(ex sqlExecer, a ChatAttachment) error {
	var filename any
	if a.Filename != nil {
		filename = *a.Filename
	}
	_, err := ex.Exec(`
		INSERT INTO chat_attachment (id, mime, data, filename)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			mime = excluded.mime, data = excluded.data,
			filename = excluded.filename`,
		a.ID, a.Mime, a.Data, filename)
	return err
}

func (d *DAL) GetChatAttachment(id string) (*ChatAttachment, error) {
	var a ChatAttachment
	var filename sql.NullString
	err := d.rdb.QueryRow(
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

func (d *DAL) ReplaceMemberAvatar(memberID string, avatar ChatAttachment) error {
	tx, err := d.wdb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	if err := tx.QueryRow(
		`SELECT avatar_attachment_id FROM member WHERE id = ?`, memberID,
	).Scan(&previous); errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	} else if err != nil {
		return err
	}
	if err := putChatAttachmentOn(tx, avatar); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE member SET avatar_attachment_id = ? WHERE id = ?`,
		avatar.ID, memberID,
	); err != nil {
		return err
	}
	if previous != "" && previous != avatar.ID {
		if _, err := tx.Exec(`DELETE FROM chat_attachment WHERE id = ?`, previous); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DAL) DeleteMemberAvatar(memberID string) error {
	tx, err := d.wdb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	if err := tx.QueryRow(
		`SELECT avatar_attachment_id FROM member WHERE id = ?`, memberID,
	).Scan(&previous); errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE member SET avatar_attachment_id = '' WHERE id = ?`, memberID,
	); err != nil {
		return err
	}
	if previous != "" {
		if _, err := tx.Exec(`DELETE FROM chat_attachment WHERE id = ?`, previous); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type ChatRead struct {
	ReaderID   string
	PeerID     string
	LastReadTS float64
}

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
	rows, err := d.rdb.Query(query, args...)
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

// The recursive CTE is a loose index scan over
// idx_chat_message_recipient_sender_ts (one seek per distinct sender), so the
// cost follows the reader's senders, not the whole inbox.
func (d *DAL) UnreadCountsFor(reader string) (map[string]int, error) {
	rows, err := d.rdb.Query(`
		WITH RECURSIVE s(sender) AS (
			SELECT (SELECT min(sender) FROM chat_message WHERE recipient = ?)
			UNION ALL
			SELECT (SELECT min(sender) FROM chat_message WHERE recipient = ? AND sender > s.sender)
			FROM s WHERE s.sender IS NOT NULL)
		SELECT sender, n FROM (
			SELECT s.sender, (
				SELECT count(*) FROM chat_message m
				WHERE m.recipient = ? AND m.sender = s.sender
				AND m.ts > COALESCE((SELECT last_read_ts FROM chat_read WHERE reader_id = ? AND peer_id = s.sender), 0)
			) AS n
			FROM s WHERE s.sender IS NOT NULL)
		WHERE n > 0`, reader, reader, reader, reader)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var sender string
		var n int
		if err := rows.Scan(&sender, &n); err != nil {
			return nil, err
		}
		out[sender] = n
	}
	return out, rows.Err()
}

func (d *DAL) PutChatRead(r ChatRead) (ChatRead, bool, error) {
	res, err := d.wdb.Exec(`
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
	err = d.rdb.QueryRow(`
		SELECT reader_id, peer_id, last_read_ts FROM chat_read
		WHERE reader_id = ? AND peer_id = ?`,
		r.ReaderID, r.PeerID,
	).Scan(&eff.ReaderID, &eff.PeerID, &eff.LastReadTS)
	return eff, n > 0, err
}

func (d *DAL) DeleteChatReadsInvolving(memberID string) (int, error) {
	res, err := d.wdb.Exec(
		`DELETE FROM chat_read WHERE reader_id = ? OR peer_id = ?`,
		memberID, memberID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

type UserContext struct {
	Text       string
	Tombstoned bool
}

const userContextRowID = 1

func (d *DAL) GetUserContext() (*UserContext, error) { return getUserContextOn(d.rdb) }

func getUserContextOn(q sqlRowQuerier) (*UserContext, error) {
	var uc UserContext
	err := q.QueryRow(
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

func (d *DAL) PutUserContext(uc UserContext) error {
	return putUserContextOn(d.wdb, uc)
}

func putUserContextOn(ex sqlExecer, uc UserContext) error {
	_, err := ex.Exec(`
		INSERT INTO user_context (id, text, tombstoned) VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			text = excluded.text, tombstoned = excluded.tombstoned`,
		userContextRowID, uc.Text, uc.Tombstoned)
	return err
}

type RoleDef struct {
	RoleKey      string
	Name         string
	DefinitionMD string
	Tombstoned   bool
}

func (d *DAL) ListRoleDefs() ([]RoleDef, error) {
	rows, err := d.rdb.Query(
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

func (d *DAL) GetRoleDef(roleKey string) (*RoleDef, error) { return getRoleDefOn(d.rdb, roleKey) }

func getRoleDefOn(q sqlRowQuerier, roleKey string) (*RoleDef, error) {
	var rd RoleDef
	err := q.QueryRow(
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

func (d *DAL) PutRoleDef(rd RoleDef) error {
	return putRoleDefOn(d.wdb, rd)
}

func putRoleDefOn(ex sqlExecer, rd RoleDef) error {
	_, err := ex.Exec(`
		INSERT INTO role_def (role_key, name, definition_md, tombstoned)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (role_key) DO UPDATE SET
			name = excluded.name, definition_md = excluded.definition_md,
			tombstoned = excluded.tombstoned`,
		rd.RoleKey, rd.Name, rd.DefinitionMD, rd.Tombstoned)
	return err
}

func (d *DAL) DeleteRoleDef(roleKey string) (bool, error) {
	var deleted bool
	err := d.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM role_def WHERE role_key = ?`, roleKey)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		deleted = n > 0
		// History goes in the same tx: its read face is open to every authenticated
		// caller, and the guide promises 「永久移除」.
		_, err = tx.Exec(`DELETE FROM document_history
			WHERE document_kind = 'role_definition' AND document_key = ?`, roleKey)
		return err
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

type Insight struct {
	RoleKey    string
	Text       string
	Tombstoned bool
}

func (d *DAL) GetInsight(roleKey string) (*Insight, error) {
	return getInsightOn(d.rdb, roleKey)
}

func getInsightOn(q sqlRowQuerier, roleKey string) (*Insight, error) {
	var i Insight
	err := q.QueryRow(`
		SELECT role_key, text, tombstoned FROM role_insight
		WHERE role_key = ?`, roleKey,
	).Scan(&i.RoleKey, &i.Text, &i.Tombstoned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (d *DAL) PutInsight(i Insight) error {
	return putInsightOn(d.wdb, i)
}

func putInsightOn(ex sqlExecer, i Insight) error {
	_, err := ex.Exec(`
		INSERT INTO role_insight (role_key, text, tombstoned)
		VALUES (?, ?, ?)
		ON CONFLICT (role_key) DO UPDATE SET
			text = excluded.text, tombstoned = excluded.tombstoned`,
		i.RoleKey, i.Text, i.Tombstoned)
	return err
}

// Exact equality on the history key: an insight's key is the bare role_key (no
// terminator), so a prefix match deleting r-abc would also take r-abcdef's.
func (d *DAL) DeleteInsightForRole(roleKey string) (int, error) {
	var deleted int
	err := d.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM role_insight WHERE role_key = ?`, roleKey)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		deleted = int(n)
		_, err = tx.Exec(`DELETE FROM document_history
			WHERE document_kind = 'insight' AND document_key = ?`, roleKey)
		return err
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

type BootDocument struct {
	Kind       string
	Key        string
	Text       string
	Tombstoned bool
}

func (d *DAL) GetBootDocument(kind, key string) (*BootDocument, error) {
	return getBootDocumentOn(d.rdb, kind, key)
}

func getBootDocumentOn(q sqlRowQuerier, kind, key string) (*BootDocument, error) {
	var b BootDocument
	err := q.QueryRow(`
		SELECT doc_kind, doc_key, text, tombstoned FROM boot_document
		WHERE doc_kind = ? AND doc_key = ?`, kind, key,
	).Scan(&b.Kind, &b.Key, &b.Text, &b.Tombstoned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (d *DAL) PutBootDocument(b BootDocument) error {
	return putBootDocumentOn(d.wdb, b)
}

func putBootDocumentOn(ex sqlExecer, b BootDocument) error {
	_, err := ex.Exec(`
		INSERT INTO boot_document (doc_kind, doc_key, text, tombstoned)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (doc_kind, doc_key) DO UPDATE SET
			text = excluded.text, tombstoned = excluded.tombstoned`,
		b.Kind, b.Key, b.Text, b.Tombstoned)
	return err
}

type AccountAlias struct {
	Account     string
	DisplayName string
}

type MachineAlias struct {
	MachineID   string
	DisplayName string
}

func (d *DAL) GetAccountAlias(account string) (*AccountAlias, error) {
	var a AccountAlias
	err := d.rdb.QueryRow(
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

func (d *DAL) AccountDisplayNames() (map[string]string, error) {
	return d.displayNames(
		`SELECT account, display_name FROM account_alias WHERE display_name != ''`)
}

func (d *DAL) PutAccountAlias(a AccountAlias) error {
	_, err := d.wdb.Exec(`
		INSERT INTO account_alias (account, display_name) VALUES (?, ?)
		ON CONFLICT (account) DO UPDATE SET display_name = excluded.display_name`,
		a.Account, a.DisplayName)
	return err
}

func (d *DAL) GetMachineAlias(machineID string) (*MachineAlias, error) {
	var a MachineAlias
	err := d.rdb.QueryRow(
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

func (d *DAL) MachineDisplayNames() (map[string]string, error) {
	return d.displayNames(
		`SELECT machine_id, display_name FROM machine_alias WHERE display_name != ''`)
}

func (d *DAL) PutMachineAlias(a MachineAlias) error {
	_, err := d.wdb.Exec(`
		INSERT INTO machine_alias (machine_id, display_name) VALUES (?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET display_name = excluded.display_name`,
		a.MachineID, a.DisplayName)
	return err
}

type ReplyCardOption struct {
	Text   string `json:"text"`
	AIPick bool   `json:"ai_pick"`
}

type ReplyCard struct {
	ID            string
	FromMember    string
	Kind          string
	Summary       string
	Body          string
	Options       []ReplyCardOption
	SelectMode    string
	Status        string
	CreatedTS     float64
	AnsweredTS    float64
	ExpiredTS     float64
	ChatMessageID string
	// AnswerOptionIdxs are stored deduped and ascending.
	AnswerOptionIdxs  []int
	AnswerText        string
	AnswerAttachments []any
	Attachments       []any
	TaskID            string
	TaskStepID        string
}

const replyCardColumns = `id, from_member, kind, summary, body, options,
	select_mode, status, created_ts, answered_ts, expired_ts, chat_message_id,
	answer_option_idxs, answer_text, answer_attachments, attachments,
	task_id, task_step_id`

func scanReplyCard(row interface{ Scan(...any) error }) (ReplyCard, error) {
	var c ReplyCard
	var options, answerAttachments, attachments string
	var optionIdxs sql.NullString
	err := row.Scan(
		&c.ID, &c.FromMember, &c.Kind, &c.Summary, &c.Body, &options,
		&c.SelectMode, &c.Status, &c.CreatedTS, &c.AnsweredTS, &c.ExpiredTS,
		&c.ChatMessageID,
		&optionIdxs, &c.AnswerText, &answerAttachments, &attachments,
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
	if optionIdxs.Valid {
		if err := json.Unmarshal([]byte(optionIdxs.String), &c.AnswerOptionIdxs); err != nil {
			return ReplyCard{}, fmt.Errorf("reply_card %s: bad answer_option_idxs JSON: %w", c.ID, err)
		}
	}
	return c, nil
}

func (d *DAL) ListReplyCards() ([]ReplyCard, error) {
	rows, err := d.rdb.Query(
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

func (d *DAL) GetReplyCard(id string) (*ReplyCard, error) {
	row := d.rdb.QueryRow(
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

func (d *DAL) PutChatWithAttachments(m ChatMessage, atts []ChatAttachment) error {
	if len(atts) == 0 {
		return d.PutChat(m)
	}
	return d.inTx(func(tx *sql.Tx) error {
		for _, a := range atts {
			if err := putChatAttachmentOn(tx, a); err != nil {
				return err
			}
		}
		return putChatOn(tx, m)
	})
}

func (d *DAL) PutReplyCardWithChat(c ReplyCard, m ChatMessage, atts []ChatAttachment) error {
	return d.inTx(func(tx *sql.Tx) error {
		for _, a := range atts {
			if err := putChatAttachmentOn(tx, a); err != nil {
				return err
			}
		}
		if err := putChatOn(tx, m); err != nil {
			return err
		}
		return putReplyCardOn(tx, c)
	})
}

// A nil task is defence only: the caller answers 409 unless the task is
// in_progress|waiting_owner. Relax that 409 and this writes the step while
// leaving the task row untouched.
func (d *DAL) PutReplyCardWithChatStepAndTask(
	c ReplyCard, m ChatMessage, atts []ChatAttachment, st TaskStep, t *Task,
) error {
	return d.inTx(func(tx *sql.Tx) error {
		for _, a := range atts {
			if err := putChatAttachmentOn(tx, a); err != nil {
				return err
			}
		}
		if err := putChatOn(tx, m); err != nil {
			return err
		}
		if err := putReplyCardOn(tx, c); err != nil {
			return err
		}
		if err := putTaskStepOn(tx, st); err != nil {
			return err
		}
		if t == nil {
			return nil
		}
		return putTaskOn(tx, *t, taskWriteUpsert)
	})
}

// No production caller: answering a card through this would settle it while
// stranding its step and task — use PutReplyCardWithStepAndTask.
func (d *DAL) PutReplyCardWithAttachments(c ReplyCard, atts []ChatAttachment) error {
	if len(atts) == 0 {
		return d.PutReplyCard(c)
	}
	return d.inTx(func(tx *sql.Tx) error {
		for _, a := range atts {
			if err := putChatAttachmentOn(tx, a); err != nil {
				return err
			}
		}
		return putReplyCardOn(tx, c)
	})
}

// Publish only after this returns: a delta fanned out for a transaction that
// then rolls back announces something that did not happen.
func (d *DAL) PutReplyCardWithStepAndTask(c ReplyCard, atts []ChatAttachment, step *TaskStep, task *Task) error {
	return d.inTx(func(tx *sql.Tx) error {
		for _, a := range atts {
			if err := putChatAttachmentOn(tx, a); err != nil {
				return err
			}
		}
		if err := putReplyCardOn(tx, c); err != nil {
			return err
		}
		if step != nil {
			if err := putTaskStepOn(tx, *step); err != nil {
				return err
			}
		}
		if task != nil {
			if err := putTaskOn(tx, *task, taskWriteUpsert); err != nil {
				return err
			}
		}
		return nil
	})
}

// inTx runs on wdb: ONE connection, BEGIN IMMEDIATE (openSQLite, migrate.go).
// IMMEDIATE is what lets
// busy_timeout cover a writer on ANOTHER handle (ocserverd backup, a shell
// sqlite3): in WAL a DEFERRED read-then-write tx gets an instant SQLITE_BUSY on
// lock upgrade. Our own writers are serialised by the connection cap, not by it.
// Inside fn use only tx (the *On helpers): d.wdb, d.inTx and s.dal methods wait
// on the connection fn holds and deadlock.
func (d *DAL) inTx(fn func(tx *sql.Tx) error) error {
	tx, err := d.wdb.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DAL) PutReplyCard(c ReplyCard) error { return putReplyCardOn(d.wdb, c) }

func putReplyCardOn(ex sqlExecer, c ReplyCard) error {
	options := c.Options
	if options == nil {
		options = []ReplyCardOption{}
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
	selectMode := c.SelectMode
	if selectMode == "" {
		selectMode = replyCardSelectModeSingle
	}
	var optionIdxs any
	if len(c.AnswerOptionIdxs) > 0 {
		blob, err := json.Marshal(c.AnswerOptionIdxs)
		if err != nil {
			return err
		}
		optionIdxs = string(blob)
	}
	_, err = ex.Exec(`
		INSERT INTO reply_card (`+replyCardColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			from_member = excluded.from_member, kind = excluded.kind,
			summary = excluded.summary, body = excluded.body,
			options = excluded.options, select_mode = excluded.select_mode,
			status = excluded.status,
			created_ts = excluded.created_ts, answered_ts = excluded.answered_ts,
			expired_ts = excluded.expired_ts,
			chat_message_id = excluded.chat_message_id,
			answer_option_idxs = excluded.answer_option_idxs,
			answer_text = excluded.answer_text,
			answer_attachments = excluded.answer_attachments,
			attachments = excluded.attachments,
			task_id = excluded.task_id, task_step_id = excluded.task_step_id`,
		c.ID, c.FromMember, c.Kind, c.Summary, c.Body, string(optionsBlob),
		selectMode, c.Status, c.CreatedTS, c.AnsweredTS, c.ExpiredTS,
		c.ChatMessageID,
		optionIdxs, c.AnswerText, string(answerAttachmentsBlob),
		string(attachmentsBlob), c.TaskID, c.TaskStepID,
	)
	return err
}

const (
	WebhookStatusEnabled  = "enabled"
	WebhookStatusDisabled = "disabled"
)

const (
	WebhookPlatformGeneric = "generic"
	WebhookPlatformSlack   = "slack"
	WebhookPlatformGithub  = "github"
)

const (
	WebhookDropReasonSigFailed  = "sig_failed"
	WebhookDropReasonDisabled   = "disabled"
	WebhookDropReasonMemberGone = "member_gone"
	WebhookDropReasonOversize   = "oversize"
)

type WebhookEndpoint struct {
	Token      string
	MemberID   string
	EndpointID string
	Purpose    string
	Status     string
	CreatedTS  float64
	// SigningSecret is write-only: never echoed on any wire.
	Platform       string
	SigningSecret  string
	LastReceivedTS float64
	DeliveredCount int64
	DroppedCount   int64
	LastDropReason string
}

const webhookColumns = `token, member_id, endpoint_id, purpose, status, created_ts, platform, signing_secret, last_received_ts, delivered_count, dropped_count, last_drop_reason`

func scanWebhook(row interface{ Scan(...any) error }) (WebhookEndpoint, error) {
	var e WebhookEndpoint
	var signingSecret sql.NullString
	err := row.Scan(&e.Token, &e.MemberID, &e.EndpointID, &e.Purpose, &e.Status,
		&e.CreatedTS, &e.Platform, &signingSecret,
		&e.LastReceivedTS, &e.DeliveredCount, &e.DroppedCount, &e.LastDropReason)
	e.SigningSecret = signingSecret.String
	return e, err
}

func (d *DAL) GetWebhookByToken(token string) (*WebhookEndpoint, error) {
	if token == "" {
		return nil, nil
	}
	row := d.rdb.QueryRow(
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

func (d *DAL) GetWebhookByMemberEndpoint(memberID, endpointID string) (*WebhookEndpoint, error) {
	row := d.rdb.QueryRow(
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

func (d *DAL) ListWebhooksByMember(memberID string) ([]WebhookEndpoint, error) {
	rows, err := d.rdb.Query(
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

func (d *DAL) PutWebhookEndpoint(e WebhookEndpoint) error {
	var signingSecret any
	if e.SigningSecret != "" {
		signingSecret = e.SigningSecret
	}
	platform := e.Platform
	if platform == "" {
		platform = WebhookPlatformGeneric
	}
	_, err := d.wdb.Exec(`
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

func (d *DAL) TouchWebhookReceived(token string, ts float64) error {
	_, err := d.wdb.Exec(
		`UPDATE webhook_endpoint SET last_received_ts = ? WHERE token = ?`, ts, token)
	return err
}

func (d *DAL) MarkWebhookDelivered(token string, ts float64) error {
	_, err := d.wdb.Exec(
		`UPDATE webhook_endpoint
		 SET delivered_count = delivered_count + 1, last_received_ts = ?
		 WHERE token = ?`, ts, token)
	return err
}

func (d *DAL) MarkWebhookDropped(token, reason string, ts float64) error {
	_, err := d.wdb.Exec(
		`UPDATE webhook_endpoint
		 SET dropped_count = dropped_count + 1, last_drop_reason = ?,
		     last_received_ts = ?
		 WHERE token = ?`, reason, ts, token)
	return err
}

func (d *DAL) SetWebhookStatus(token, status string) error {
	_, err := d.wdb.Exec(
		`UPDATE webhook_endpoint SET status = ? WHERE token = ?`, status, token)
	return err
}

func (d *DAL) DeleteWebhookEndpoint(token string) error {
	if _, err := d.wdb.Exec(
		`DELETE FROM webhook_request_log WHERE token = ?`, token); err != nil {
		return err
	}
	_, err := d.wdb.Exec(`DELETE FROM webhook_endpoint WHERE token = ?`, token)
	return err
}

type WebhookRequestLog struct {
	TS        float64
	Outcome   string
	Headers   string
	Body      string
	Truncated bool
}

const webhookRequestLogKeep = 5

func (d *DAL) InsertWebhookRequestLog(token string, l WebhookRequestLog) error {
	if _, err := d.wdb.Exec(`
		INSERT INTO webhook_request_log (token, ts, outcome, headers, body, truncated)
		VALUES (?, ?, ?, ?, ?, ?)`,
		token, l.TS, l.Outcome, l.Headers, l.Body, l.Truncated); err != nil {
		return err
	}
	_, err := d.wdb.Exec(`
		DELETE FROM webhook_request_log
		WHERE token = ? AND id NOT IN (
			SELECT id FROM webhook_request_log
			WHERE token = ? ORDER BY id DESC LIMIT ?)`,
		token, token, webhookRequestLogKeep)
	return err
}

func (d *DAL) ListWebhookRequestLogs(token string) ([]WebhookRequestLog, error) {
	rows, err := d.rdb.Query(`
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

const (
	ScheduledMessageStatusEnabled  = "enabled"
	ScheduledMessageStatusDisabled = "disabled"
)

const (
	ScheduledMessageCadenceDaily   = "daily"
	ScheduledMessageCadenceWeekly  = "weekly"
	ScheduledMessageCadenceMonthly = "monthly"
	ScheduledMessageCadenceCustom  = "custom"
)

// LastFiredSlot is the delivered slot's identifier (slotKey), not a clock
// reading; the tick fires only for a slot strictly after it by parsed-instant
// ordering (slotIsAfterCursor), never by string inequality.
type ScheduledMessage struct {
	ID            string
	MemberID      string
	Label         string
	Body          string
	Cadence       string
	DayOfWeek     int
	DayOfMonth    int
	Hour          int
	Minute        int
	CustomMonths  []int
	CustomDays    []int
	CustomHours   []int
	CustomMinutes []int
	Timezone      string
	Status        string
	LastFiredSlot string
	LastFiredTS   float64
	CreatedTS     float64
}

const scheduledMessageColumns = `id, member_id, label, body, cadence, day_of_week, day_of_month, hour, minute, custom_months, custom_days, custom_hours, custom_minutes, timezone, status, last_fired_slot, last_fired_ts, created_ts`

func scanScheduledMessage(row interface{ Scan(...any) error }) (ScheduledMessage, error) {
	var m ScheduledMessage
	var months, days, hours, minutes string
	err := row.Scan(&m.ID, &m.MemberID, &m.Label, &m.Body, &m.Cadence,
		&m.DayOfWeek, &m.DayOfMonth, &m.Hour, &m.Minute,
		&months, &days, &hours, &minutes, &m.Timezone,
		&m.Status, &m.LastFiredSlot, &m.LastFiredTS, &m.CreatedTS)
	m.CustomMonths = parseIntSet(months)
	m.CustomDays, m.CustomHours, m.CustomMinutes = parseIntSet(days), parseIntSet(hours), parseIntSet(minutes)
	return m, err
}

// canonicalIntSet is the only producer of custom_* column values. The
// canonical form is a storage invariant: the PATCH re-aim test compares
// supplied against stored, so a reordered checkbox list would otherwise re-aim
// and swallow a delivery.
func canonicalIntSet(vals []int) string {
	sorted := sortedIntSet(vals)
	parts := make([]string, len(sorted))
	for i, v := range sorted {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

func sortedIntSet(vals []int) []int {
	if len(vals) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(vals))
	out := make([]int, 0, len(vals))
	for _, v := range vals {
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func parseIntSet(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		v, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

func (d *DAL) GetScheduledMessage(id string) (*ScheduledMessage, error) {
	row := d.rdb.QueryRow(
		`SELECT `+scheduledMessageColumns+` FROM scheduled_message WHERE id = ?`, id)
	m, err := scanScheduledMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (d *DAL) ListScheduledMessagesByMember(memberID string) ([]ScheduledMessage, error) {
	rows, err := d.rdb.Query(
		`SELECT `+scheduledMessageColumns+` FROM scheduled_message
		 WHERE member_id = ? ORDER BY created_ts, id`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledMessage
	for rows.Next() {
		m, err := scanScheduledMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DAL) ListAllEnabledScheduledMessages() ([]ScheduledMessage, error) {
	rows, err := d.rdb.Query(
		`SELECT `+scheduledMessageColumns+` FROM scheduled_message
		 WHERE status = ? ORDER BY created_ts, id`, ScheduledMessageStatusEnabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledMessage
	for rows.Next() {
		m, err := scanScheduledMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DAL) PutScheduledMessage(m ScheduledMessage) error {
	_, err := d.wdb.Exec(`
		INSERT INTO scheduled_message (`+scheduledMessageColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			member_id = excluded.member_id, label = excluded.label,
			body = excluded.body, cadence = excluded.cadence,
			day_of_week = excluded.day_of_week, day_of_month = excluded.day_of_month,
			hour = excluded.hour, minute = excluded.minute,
			custom_months = excluded.custom_months,
			custom_days = excluded.custom_days, custom_hours = excluded.custom_hours,
			custom_minutes = excluded.custom_minutes,
			timezone = excluded.timezone, status = excluded.status,
			last_fired_slot = excluded.last_fired_slot,
			last_fired_ts = excluded.last_fired_ts,
			created_ts = excluded.created_ts`,
		m.ID, m.MemberID, m.Label, m.Body, m.Cadence, m.DayOfWeek, m.DayOfMonth,
		m.Hour, m.Minute, canonicalIntSet(m.CustomMonths),
		canonicalIntSet(m.CustomDays), canonicalIntSet(m.CustomHours), canonicalIntSet(m.CustomMinutes),
		m.Timezone, m.Status, m.LastFiredSlot, m.LastFiredTS,
		m.CreatedTS)
	return err
}

// Neither this nor MarkScheduledMessageFired may become a whole-row
// PutScheduledMessage: the edit handler and the cadence tick each hold a
// snapshot, and re-putting it rolls back the other's write — a rolled-back
// cursor silently re-delivers a slot.
func (d *DAL) UpdateScheduledMessageSettings(m ScheduledMessage) error {
	_, err := d.wdb.Exec(`
		UPDATE scheduled_message SET
			label = ?, body = ?, cadence = ?, day_of_week = ?, day_of_month = ?,
			hour = ?, minute = ?,
			custom_months = ?, custom_days = ?, custom_hours = ?, custom_minutes = ?,
			timezone = ?, status = ?
		WHERE id = ?`,
		m.Label, m.Body, m.Cadence, m.DayOfWeek, m.DayOfMonth,
		m.Hour, m.Minute, canonicalIntSet(m.CustomMonths),
		canonicalIntSet(m.CustomDays), canonicalIntSet(m.CustomHours), canonicalIntSet(m.CustomMinutes),
		m.Timezone, m.Status, m.ID)
	return err
}

// AimScheduledMessageCursor is for an edit that moved the schedule: the slot
// it crossed is recorded as the cursor so it is never delivered.
func (d *DAL) AimScheduledMessageCursor(id, slot string) error {
	_, err := d.wdb.Exec(
		`UPDATE scheduled_message SET last_fired_slot = ? WHERE id = ?`, slot, id)
	return err
}

func (d *DAL) MarkScheduledMessageFired(id, slot string, ts float64) error {
	_, err := d.wdb.Exec(
		`UPDATE scheduled_message SET last_fired_slot = ?, last_fired_ts = ?
		 WHERE id = ?`, slot, ts, id)
	return err
}

func (d *DAL) DeleteScheduledMessage(id string) error {
	_, err := d.wdb.Exec(`DELETE FROM scheduled_message WHERE id = ?`, id)
	return err
}

func (d *DAL) GetSetting(key string) (*string, error) {
	var v string
	err := d.rdb.QueryRow(`SELECT value FROM setting WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (d *DAL) PutSetting(key, value string) error {
	_, err := d.wdb.Exec(`
		INSERT INTO setting (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, nowSecs())
	return err
}

func (d *DAL) PutPushSubscription(s PushSubscription) error {
	_, err := d.wdb.Exec(`
		INSERT INTO push_subscription (endpoint, p256dh, auth, expiration_time, created_ts, updated_ts)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh, auth = excluded.auth,
			expiration_time = excluded.expiration_time, updated_ts = excluded.updated_ts`,
		s.Endpoint, s.P256dh, s.Auth, s.ExpirationTime, nowSecs(), nowSecs())
	return err
}

func (d *DAL) ListPushSubscriptions() ([]PushSubscription, error) {
	rows, err := d.rdb.Query(`SELECT endpoint, p256dh, auth, expiration_time FROM push_subscription`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var s PushSubscription
		var expiration sql.NullFloat64
		if err := rows.Scan(&s.Endpoint, &s.P256dh, &s.Auth, &expiration); err != nil {
			return nil, err
		}
		if expiration.Valid {
			s.ExpirationTime = &expiration.Float64
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DAL) DeletePushSubscription(endpoint string) error {
	_, err := d.wdb.Exec(`DELETE FROM push_subscription WHERE endpoint = ?`, endpoint)
	return err
}

// Only verbs with no compensating re-decision are queued (today: update);
// START must never land here — see migrations/00034_warden_command_queue.sql.
type WardenCommand struct {
	WardenID   string
	Verb       string
	MemberID   string
	Frame      []byte
	EnqueuedTS float64
}

// DO NOTHING on conflict: a re-enqueue of the same (warden, verb, target) is
// the same order, and refreshing enqueued_ts would let it dodge the expiry
// sweep forever.
func (d *DAL) PutWardenCommand(c WardenCommand) error {
	_, err := d.wdb.Exec(`
		INSERT INTO warden_command_queue (warden_id, verb, member_id, frame, enqueued_ts)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (warden_id, verb, member_id) DO NOTHING`,
		c.WardenID, c.Verb, c.MemberID, string(c.Frame), c.EnqueuedTS)
	return err
}

func (d *DAL) DeleteWardenCommand(wardenID, verb, memberID string) error {
	_, err := d.wdb.Exec(`
		DELETE FROM warden_command_queue
		WHERE warden_id = ? AND verb = ? AND member_id = ?`,
		wardenID, verb, memberID)
	return err
}

func (d *DAL) ListWardenCommands() ([]WardenCommand, error) {
	rows, err := d.rdb.Query(`
		SELECT warden_id, verb, member_id, frame, enqueued_ts
		FROM warden_command_queue ORDER BY enqueued_ts, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WardenCommand
	for rows.Next() {
		var c WardenCommand
		var frame string
		if err := rows.Scan(&c.WardenID, &c.Verb, &c.MemberID, &frame, &c.EnqueuedTS); err != nil {
			return nil, err
		}
		c.Frame = []byte(frame)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DAL) DeleteWardenCommandsBefore(cutoff float64) (int64, error) {
	res, err := d.wdb.Exec(`DELETE FROM warden_command_queue WHERE enqueued_ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return n, nil
}

func (d *DAL) DeleteSetting(key string) error {
	_, err := d.wdb.Exec(`DELETE FROM setting WHERE key = ?`, key)
	return err
}

func (d *DAL) displayNames(query string) (map[string]string, error) {
	rows, err := d.rdb.Query(query)
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

// SetMemberAgentIatFloor is forward-only (mfAgentIatFloor, max()), so racing
// wakes cannot lower the floor. Two wakes in the same second stay
// indistinguishable: owner ruling 2026-08-28 「先不管搶同一秒的問題好了」.
func (d *DAL) SetMemberAgentIatFloor(id string, ts float64) error {
	return d.PatchMember(id, mfAgentIatFloor(ts))
}

// SetMemberTokenKeyID must NOT become forward-only: the observation
// legitimately moves backwards (a machine reinstalled with an older
// credential), which is what the owner needs to see before removing a key.
func (d *DAL) SetMemberTokenKeyID(id, keyID string) error {
	return d.PatchMember(id, mfTokenKeyID(keyID))
}
