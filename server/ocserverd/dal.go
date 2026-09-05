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
	"sort"
	"strconv"
	"strings"
)

// DAL owns the migrated database handles. Construct with NewDALPools over a
// write pool openSQLite'd + runMigrations'd by the caller plus a read pool from
// openSQLiteReadPool (or with NewDAL when one handle serves both — the CLI
// one-shots and unit tests).
//
// TWO POOLS, NOT ONE (T-dd7a). The single handle this replaced was capped at ONE
// connection, so every request in the process serialised at the database and the
// whole server ran one request at a time (the owner's DevTools trace: a 0.1 kB
// endpoint took 904 ms while a 407 kB one took 85 ms in the same session — size
// did not predict time, position in the queue did). The two handles are NOT
// interchangeable and the split is deliberately asymmetric:
//
//   - wdb — the WRITE pool. Capped at ONE connection and its transactions open
//     BEGIN IMMEDIATE (openSQLite's `_txlock=immediate`). Both halves matter: the
//     cap makes our own writers queue in Go, and IMMEDIATE takes the write lock
//     up front so `busy_timeout` can actually wait out an external writer.
//     A DEFERRED transaction that reads and then writes (SaveWithDocumentHistory
//     is exactly that shape) has to UPGRADE its lock, and SQLite answers an
//     upgrade conflict with an immediate SQLITE_BUSY that busy_timeout does not
//     apply to — retrying would deadlock, so the engine does not wait.
//   - rdb — the READ pool. Several connections, and opened READ-ONLY at the
//     SQLite level, so a write that is wired here does not quietly go to the
//     wrong place: it fails, loudly, with "attempt to write a readonly database".
//
// 🔴 The field is NOT called `db` on purpose. Every one of the ~90 call sites had
// to name a pool when this landed, and a new one cannot compile without naming
// one either — the classification is exhaustive BY CONSTRUCTION rather than by a
// list someone has to remember to update.
//
// ⚠️ The compiler forces you to CHOOSE a pool; it cannot tell you that you chose
// right, and there is exactly one shape where choosing wrong also type-checks
// silently: `sqlExecer` (write seam) and `sqlQuerier` (read seam) are both
// satisfied by *sql.DB, so `putChatOn(d.rdb, m)` compiles as happily as
// `putChatOn(d.wdb, m)` — and the SQL lives in the seam, not at the call site, so
// reading the call site tells you nothing either. That hole is covered by
// TestReadPoolIsNeverHandedToAWriteSeam (wal_pool_test.go), and in production by
// the read pool being mode=ro so such a write fails on its first attempt instead
// of intermittently under load.
type DAL struct {
	wdb *sql.DB
	rdb *sql.DB
}

// PushSubscription is one browser endpoint registered by the single owner.
// Endpoint is the browser-provided natural key; re-subscribing replaces its
// short-lived encryption material rather than accumulating duplicate rows.
type PushSubscription struct {
	Endpoint       string
	P256dh         string
	Auth           string
	ExpirationTime *float64
}

// NewDAL serves both roles from ONE handle. That is the correct shape for the
// CLI one-shots (migrate / set-password / claim-token) and for unit tests: a
// single-threaded caller gains nothing from a reader pool, and routing reads
// through the write pool is SLOW, never WRONG. serve uses NewDALPools.
func NewDAL(db *sql.DB) *DAL {
	return &DAL{wdb: db, rdb: db}
}

// NewDALPools is the serve-time constructor: writes go to w (one connection,
// BEGIN IMMEDIATE), reads to r (several connections, read-only).
func NewDALPools(w, r *sql.DB) *DAL {
	return &DAL{wdb: w, rdb: r}
}

// ── members ──────────────────────────────────────────────────────────────────

// Member mirrors the member table: one roster row per AI colleague — an agent
// OR a warden (machine = the kind=='warden' row; its id IS the machine_id).
// Intent only; presence/location are observed and never stored.
type Member struct {
	ID          string
	Name        string
	Kind        string // closed set: "staff" | "warden" | "outsource" (schema CHECK)
	RoleKey     string
	Runtime     string
	Model       string
	ActualModel string
	Effort      string
	// ActualRuntime / ActualEffort are the REPORTED twins of Runtime / Effort —
	// what the member's own telemetry says it is running, durably persisted the
	// way ActualModel already is. "" means nothing has ever reported one; they
	// NEVER fall back to the configured value, because a value that stands in for
	// a missing report is indistinguishable from a change that already took
	// effect (T-7f28).
	ActualRuntime    string
	ActualEffort     string
	DesiredState     string
	DesiredMachineID string
	// LastMachineID is the durable STICKY-PLACEMENT anchor (T-98f4,
	// migrations/00039): the machine a confirmed session of this entity last
	// connected from (the SSE token's machine claim, stamped in onFirstConnect),
	// "" when it has never landed anywhere. Read by the outsource placement chain
	// as a PREFERENCE below the owner pin and above the configured (task row /
	// 手冊) arms — 手冊 decides the birthplace, the last landing decides every
	// rebirth after it. Unlike DesiredMachineID it never stalls a worker: an
	// undispatchable last landing falls through to the configured chain.
	LastMachineID string
	// SessionBootTS is the durable SESSION-START anchor (T-4235,
	// migrations/00051): unix seconds of the moment this session's FIRST SSE
	// connect landed, 0 when no session is anchored (offline, or the last one
	// ended at a real spawn/stop boundary). It is the DURABLE twin of the gauge's
	// boot_ts, written and cleared by the SAME two functions (onFirstConnect /
	// clearSessionBootTS) so the two stores can never drift apart.
	//
	// WHY IT HAS TO BE DURABLE: the gauge is in-memory by contract and a station
	// re-exec empties it, but the AGENTS survive that re-exec — they just
	// reconnect. Without this column that reconnect stamped a brand-new anchor,
	// so a session alive for hours read as seconds old and the min-liveness floor
	// refused its restart_self. This column is what the reconnect RESTORES the
	// gauge from, so all three boot-storm consumers (restart_self,
	// stampContextHighRecycle, autoHandoverWorker) see the real session age.
	//
	// It is NOT on the wire — no DTO field, no spec change.
	SessionBootTS float64
	WakingSince   float64
	StoppingSince float64
	StoppedSince  float64
	RefocusSince  float64
	// RefocusOp names the operation that opened the window RefocusSince stamps
	// ("relocate" | "runtime/model" | "context_notice" | "context_high" |
	// "refocus" | "restart_self" | "token_expiry" | "accelerated_stop"), ""
	// when none is in flight. Stamped and cleared in lockstep with RefocusSince.
	// 🔴 This list is a COPY and nothing checks it: the closed set lives in
	// refocusOp* (member_ownerop_winddown.go), which is where a new cause goes,
	// and winddownKindFor is what decides what each one MEANS. Read those.
	RefocusOp string
	// ForcedStopAt is the durable record that a session was CUT OFF rather than
	// collected (migrations/00057): unix seconds of the last force-stop, 0 when
	// there has never been one. Force-stop is the one offboard path that sends
	// no notice, so the session it kills leaves exactly what a session with
	// nothing to write leaves — no hand-off, no fresh step note, no folded
	// lesson. This column is what tells those two apart.
	//
	// 🔴 NOT cleared on the next boot, unlike every other lifecycle anchor: it
	// describes the session BEFORE this one, and that is precisely who needs to
	// read it. Written through SetMemberForcedStopAt and through a whole-row
	// write; BOTH go through mfForcedStopAt, which declares the column
	// forward-only, so every writer lands it as max(): forcedEpochLive has three
	// readers — notice suppression, the SSE stop gate, and deactivate — that use
	// it to distinguish a deliberate cut-off from a session working its close-out.
	// The targeted update is best-effort, so a failed update must not leave a
	// force-stopped session looking like a close-out. max() also prevents a stale
	// snapshot from erasing an existing record.
	ForcedStopAt float64
	// RestartAfterStop is the SECOND owner intent, split out of DesiredState
	// (T-14 項目 7, migrations/00070). DesiredState answers 「下線用多強」; this
	// answers 「下線之後要不要起來」, and the two are independent:
	//
	//	false  nothing is waiting to come back up.
	//	true   the owner's last action was a 重啟 intent (重新聚焦 / 改機器 /
	//	       換 model) landing on a member that is already on its way down.
	//	       The stop in flight is honoured AS IS — a 強制停止 stays a
	//	       強制停止 — and the member is started again once it has converged
	//	       offline.
	//
	// LAST WRITER WINS, deliberately, and only for this field: every 下線 verb
	// clears it, every 重啟 verb sets it. The wind-down LADDER above it stays a
	// ratchet (winddownStageMayAdvanceTo) — the two rules are different because
	// they answer different questions, which is exactly what one column could
	// not express.
	//
	// Consumed by consumeRestartAfterStop at the converged-offline edge of the
	// reconcile tick, and cleared in the same write that flips DesiredState back
	// to online. Not on the wire.
	RestartAfterStop bool
	// HandoverNoticedTS is the durable twin of the in-memory handover-notice
	// claim (T-6ebc, migrations/00058): the session anchor whose one-and-only
	// advance notice has already been sent, 0 when none has. It holds the ANCHOR
	// rather than a flag so a genuinely new session (new anchor) still earns its
	// own notice while a reconnect (restored anchor) stays quiet.
	//
	// WHY IT HAS TO BE DURABLE: the claim used to live only in a process-local
	// map, and a station re-exec empties that while the AGENTS survive it — so
	// every agent still in the high band was told "this is the ONLY notice you
	// get" a second time, which made that sentence false.
	//
	// Written through SetMemberHandoverNoticedTS only; PutMember's upsert
	// deliberately does NOT carry it, so no whole-row snapshot can revive a
	// claim that a session boundary just cleared. Not on the wire.
	HandoverNoticedTS float64
	// AgentIatFloor is the per-member CREDENTIAL FLOOR (T-14 項目 4B,
	// migrations/00063): the `iat` of the token that last reported waking for
	// this member, 0 when none has. requireAuth refuses any agent-scope token
	// whose iat is STRICTLY LESS THAN it — so the moment a new generation says
	// it is up, every credential minted for an earlier one is dead.
	//
	// It holds the WAKING CALLER'S OWN iat rather than now() on purpose: with a
	// strictly-less-than comparison that is what makes the session raising the
	// floor immune to its own stamp, whatever the gap between mint and boot or
	// the skew between the two clocks.
	//
	// 🔴 Warden rows carry it like any other row, and the READ side declines to
	// consult it for them (authz.go agentIatFloorRefusal). A warden credential
	// has no exp, so a floor above one could never expire out of the way.
	//
	// Written through SetMemberAgentIatFloor only; a whole-row write carries it
	// on INSERT but never onto an EXISTING row (mfAgentIatFloor declares it
	// insertOnly), so no whole-row snapshot taken before a wake can put an older
	// floor back over a newer one. Not on the wire.
	AgentIatFloor float64
	// TokenKeyID is WHICH signing key (keyring.go) this station last VERIFIED a
	// credential of this member's with, "" when it has never verified one.
	//
	// 🔴 IT IS THE STATION'S OWN OBSERVATION, NEVER A SELF-REPORT (T-80). The
	// value is taken from verifyJWTAnyKey — the key that actually produced a
	// matching HMAC — and there is no wire field, no claim and no heartbeat block
	// a machine could use to say which key it holds. That is load-bearing rather
	// than incidental: this column's ONLY purpose is to answer "is it safe to
	// press remove on the outgoing key", and an answer a machine could assert
	// would be an answer an out-of-date, wrong or hostile machine could assert.
	//
	// 🔴 BUT "CANNOT REPORT IT" IS THE LITERAL CLAIM, NOT THE WHOLE ONE, AND THE
	// DIFFERENCE IS WHY THE OBSERVATION MOVED. A machine cannot choose the VALUE,
	// but it can choose WHICH CREDENTIAL IT PRESENTS: /api/machines/renew-credential
	// is zero-argument self-service, so a warden can mint a fresh credential and
	// present it ONCE while continuing to run on the old one. Recorded at the auth
	// gate that was enough to read as converged with nothing written to disk —
	// which is the exact failure this ticket exists to abolish, rebuilt by the
	// mechanism meant to abolish it. So the observation is taken at ONE place, in
	// api_infra.go's SSE handler after hub.Connect: the credential the process is
	// actually RUNNING ON, not one it merely showed us. The residual is honest and
	// bounded — presenting a fresh credential on a stream you then keep open is
	// running on it, and doing it repeatedly costs a whole connection each time.
	//
	// A credential that has not been presented on a stream since the last rotation
	// therefore leaves a STALE value here, which is the honest reading: nothing has
	// proved that machine moved.
	//
	// The JWT header is a constant and carries no kid, so credential verification
	// is the one and only moment in the process where this fact exists at all.
	//
	// Written through SetMemberTokenKeyID only; a whole-row write carries it on
	// INSERT but never onto an existing row (mfTokenKeyID declares it insertOnly),
	// for memberFromWorker's reason — it rebuilds a Member from zero and would
	// send "" on every PutOutsourceWorker. Only warden rows are ever stamped, but
	// the column is on the member table like every other per-identity fact.
	//
	// ON the wire, but only as MachineDTO.token_key_id (GET /api/machines) — a
	// key ID is outside-safe by construction (keyring.go: ids are random, never
	// derived from key material).
	TokenKeyID   string
	BankedCost   float64
	LastOp       string
	LastOpOK     *bool // nil = no op reported yet (three-valued)
	LastOpLog    string
	LastOpReason string // structured "<code>: <detail>" cause; "" = none reported
	LastOpAt     float64
	RosterStatus string  // "active" | "removed" (dismiss is a SOFT delete)
	LinkedTaskID *string // task binding (migrations/00024); nil = unbound. Outsource members carry their bound task id here.
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
	// AvatarAttachmentID points at this stable member id's one personal image
	// in the shared byte store. Empty means no personal image.
	AvatarAttachmentID string
}

// RosterStatusRemoved is the soft-delete lifecycle value (the Python
// MEMBER_STATUS_REMOVED twin).
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

// ListMembers returns the WHOLE roster — every kind (staff, warden AND
// kind='outsource'), at ANY roster_status (soft-removed rows included; callers
// filter). One query, one member population.
//
// 🔴 IT USED TO CARRY `WHERE kind != 'outsource'`, AND THAT CLAUSE IS GONE
// (T-14 項目 6). Two things follow, and the second is the one that bites:
//
//  1. There is no longer a second function. `ListMembersIncludingOutsource`
//     existed solely because this one excluded contractors; with the clause
//     lifted the two queries were literally the same SELECT, so the twin was
//     deleted and its seven call sites now name this one. Nothing about those
//     seven changed — they always saw outsource rows.
//
//  2. EVERY OTHER CALLER NOW SEES ROWS IT NEVER SAW BEFORE. If you are reading
//     this because you are about to add a call site: this function does NOT
//     hand you "the staff". Decide, at YOUR call site, whether a contractor
//     row belongs in your fold, and if it does not, say so in code. A
//     lifecycle fold asks `lifecycleTickDriverFor(m) != driverReconcile`
//     (lifecycle_roster.go) — the named predicate that REPLACED this WHERE
//     clause, so the split is falsifiable by a parity test instead of being a
//     property of a SQL string in this file. A non-lifecycle fold that
//     genuinely wants staff only writes the kind test it means, next to the
//     reason it means it. And a fold that filters NOTHING is a decision too —
//     make it on purpose.
//
//     🔴 THE AUDIT OF TODAY'S CALL SITES IS NOT IN THIS COMMENT. It is
//     listMembersCallSiteLedger in roster_widening_ledger_t14i6_test.go: one
//     row per call site, saying whether PR ② widened it and what that fold
//     does with a contractor row. It lives there because an enumeration
//     written HERE is one nothing can check —
//     TestListMembersCallSitesAreEachOnTheRecord joins the ledger against an
//     AST scan in BOTH directions, so a caller with no row fails by name and a
//     row with no caller fails by name. This paragraph used to carry the list
//     itself, and it had already gone stale: it named three sites and did not
//     mention reconcile.go's dispatchIdentitySweepNow, which PR ② widened too.
//     Add your row there; do not re-grow the list here.
//
// ⚠️ "In this list" still does NOT mean "a member verb resolves". resolveMember
// (api_helpers.go) answers 404 for every ow- id under staffOnly scope, and that
// is deliberate. The two halves are only consistent when read together.
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

// GetMember returns one roster member by id, or nil if absent.
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

// runtime is stored EXACTLY as given, including "" (T-b3d0). It used to be
// normalized on the way in, which silently turned "no runtime chosen yet" into
// a durable "claude" — the seed assistant was born a Claude member on a box
// that may only have Codex. Every READER already calls NormalizeRuntime, so the
// "" == claude reading is unchanged; what is preserved now is the DIFFERENCE
// between "the owner picked claude" and "nobody has picked yet", which is what
// resolveEmptyRuntimeForPlacement needs at placement time.
//
// (The paragraph that used to sit here explained why PutMember's conflict clause
// left avatar_attachment_id alone. That clause is gone; the column is now
// declared insertOnly, and its reason lives on mfAvatarAttachmentID in
// dal_member_patch.go with the rest of the property table.)
// ── account spend (T-53, owner ruling rc-5c5d7c7c6dcd) ───────────────────────
//
// The ACCOUNT's own accumulated spend, the number the cockpit's account card
// shows since that ruling. It is deliberately NOT a fold over the actors on the
// account: the owner asked for the account figure and the per-member figure to
// be clearable independently, and a derived number cannot be cleared without
// clearing what it derives from. See migration 00069 for why an accumulator
// rather than a cleared watermark.

// AddAccountSpend adds newly reported spend to one account's accumulator,
// creating the row on first sight. Called from the telemetry ingest — the one
// place new spend becomes visible — and from nowhere else.
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

// ListAccountSpend reads every account's accumulator in one query — the read
// side folds thousands of actors but only ever a handful of accounts, so the
// monitoring handler takes the whole map rather than a query per account.
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

// ZeroAccountSpend sets one account's accumulator to 0 and answers with what it
// held — the receipt, and the last moment that figure exists anywhere (no
// per-charge ledger backs it). A row that does not exist answers 0: an account
// nobody has reported under and an account already at zero are the same state,
// and neither is an error.
//
// Read and write in ONE transaction so the figure reported as destroyed is
// exactly the figure that was destroyed, rather than one a concurrent report
// had already moved.
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

// PutMember writes one member row WHOLE — the shape 52 callers still use, kept
// exactly as it behaved before T-63 and now expressed as a thin shell over
// PatchMember.
//
// It is two statements in ONE transaction, and each answers a different half:
//
//   - INSERT … ON CONFLICT (id) DO NOTHING lands the whole row when there is no
//     row yet. This is the ONLY half that carries the insert-only columns, so a
//     member is born with the model, runtime, effort, machine and receipt it was
//     created with, and nothing afterwards can put a stale copy of those back.
//   - PatchMember(updatableMemberFields(...)) writes the rest onto a row that
//     already exists — the columns a whole-row writer is allowed to carry. That
//     set is not listed here: it is whatever is NOT flagged insertOnly in the
//     property table, so a column joins or leaves it by editing its own line.
//
// The end state is identical to the single upsert this replaced, on both paths.
// A brand-new row: the INSERT lands every column, then the patch rewrites the
// updatable ones with the same values (forced_stop_at through max(v, v) = v).
// An existing row: the INSERT does nothing, the patch writes exactly the columns
// the old conflict clause wrote.
//
// It fans NO SSE delta — s.putMember is the service-layer door that pairs the
// write with publishMemberPatch, and PutOutsourceWorker deliberately reaches
// this one without a member delta at all.
func (d *DAL) PutMember(m Member) error {
	fields := memberWholeRow(m)
	return d.inTx(func(tx *sql.Tx) error {
		if err := insertMemberRowIfAbsent(tx, fields); err != nil {
			return err
		}
		return patchMemberOn(tx, m.ID, updatableMemberFields(fields)...)
	})
}

// AddMemberBankedCost adds delta to ONLY member.banked_cost (T-14 項目 6) — the
// durable cumulative spend of a staff member OR an outsource worker, since P7d
// made both a row of the same table (outsource_worker.banked_cost is this
// column). A whole-row write carries the column on INSERT but never onto an
// existing row (mfBankedCost declares it insertOnly), so no whole-row write can
// move it. This function and ZeroMemberBankedCost (T-53, just below) are its
// two writers, and they are two by design: one accumulates, one resets.
//
// ACCUMULATES IN SQL, not in Go, for the reason SetMemberAgentIatFloor uses
// max() in SQL: a read-modify-write in Go loses to whichever caller writes
// last, and here the loser is money the owner has already been shown. The
// banking edges are exactly the ones that can overlap — an SSE last-disconnect
// racing a kill funnel (respawnWorkerNow / stopWorkerNow) on the same actor —
// so `banked_cost + ?` is what makes the fold loss-free rather than merely
// exactly-once.
//
// It does NOT fan a member delta: the delta is the service layer's job, the
// same split PutMember documents. bankLiveCost's member branch publishes one;
// its worker branch deliberately does not (a worker's changes ride the
// outsource_worker projection).
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) AddMemberBankedCost(id string, delta float64) error {
	_, err := d.wdb.Exec(
		`UPDATE member SET banked_cost = banked_cost + ? WHERE id = ?`, delta, id)
	return err
}

// ZeroMemberBankedCost sets ONE actor's member.banked_cost to 0 and answers with
// what it held — the receipt, and the last moment that figure exists anywhere
// (T-53, owner rulings rc-7dea0deefa63 / rc-1344cc76a24a: the reset is
// deliberately irreversible and no per-charge ledger backs the column). It
// serves a staff member and an outsource worker alike, because P7d made both a
// row of this table — the same reason AddMemberBankedCost above is one writer
// rather than two.
//
// 🔴 IT MUST BE A SINGLE-COLUMN WRITE, and that is not a style preference. The
// obvious shape — read the row, set BankedCost to 0, hand the whole row to
// PutMember — silently does NOTHING now: banked_cost is deliberately insert-only
// (T-14 項目 6), so the durable half of the reset
// would never land while the API still answered 200 with a receipt naming money
// it had not destroyed. The live half would go, the column would not, and the
// cockpit would grow the number back on its next read.
//
// Read and write in ONE transaction, like ZeroAccountSpend: the figure reported
// as destroyed is then exactly the figure that was destroyed, not one a
// concurrent banking edge had already moved. A missing row answers 0 — an actor
// that never banked anything and one already at zero are the same state, and
// neither is an error.
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

// SetMemberHandoverNoticedTS writes ONLY member.handover_noticed_ts (T-6ebc):
// the session anchor whose one advance handover notice has been sent, or 0 to
// release the claim at a session boundary. It is the SOLE writer that moves the
// column — a whole-row write carries it on INSERT but never onto an existing
// row (mfHandoverNoticedTS declares it insertOnly), so no whole-row snapshot can
// revive a cleared claim.
//
// Single-column for the same two reasons SetMemberSessionBootTS is: the column
// is not on the wire, so a member delta on the connect edge would be pure
// churn; and the callers run on the SSE edge and inside the reconcile tick,
// next to HTTP faces writing member rows without reconcileMu, where a
// whole-row write would put a stale snapshot back over them.
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) SetMemberHandoverNoticedTS(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE member SET handover_noticed_ts = ? WHERE id = ?`, ts, id)
	return err
}

// SetMemberForcedStopAt stamps the force-stop record on ONE column. It is the
// BACKSTOP now, not the only writer: a whole-row write carries the column too,
// so the value lands in the same write that publishes the member — and a stale
// lifecycle snapshot still cannot erase it, because the column is forward-only.
//
// 🔴 Why the whole-row door had to carry it (independent review): the SSE stop
// gate now READS this column to tell "cut off deliberately" from "working its
// close-out". While this seam was the only writer, and its failure is
// deliberately non-fatal, a failed UPDATE left the column at 0 — and a
// force-stopped member then reconnected as if it were closing out, on an arm
// that runs no clock to collect it. A safety verdict must not hang on a
// best-effort write.
//
// 🔑 It goes through PatchMember rather than writing its own SQL (T-63). It used
// to be a plain `forced_stop_at = ?`, which could walk the stamp BACKWARDS while
// the whole-row door could not — two paths saying different things about one
// column. Naming the field here makes the column's own forwardOnly declaration
// apply to this path as well, so there is nothing left to keep in sync.
//
// ⚠️ WHAT THIS SEAM GAVE UP: it can no longer CLEAR the column. Passing 0, or
// any value below the stored one, is now a silent no-op. Nothing relies on that
// today — the sole non-test caller stamps nowSecs(), and the column is
// deliberately never cleared (it describes the session BEFORE this one) — but an
// "undo the force-stop record" would need its own seam rather than this one, and
// would not find out by being refused.
func (d *DAL) SetMemberForcedStopAt(id string, ts float64) error {
	return d.PatchMember(id, mfForcedStopAt(ts))
}

// SetMemberSessionBootTS writes ONLY member.session_boot_ts (T-4235). It is a
// targeted column UPDATE rather than a PutMember round-trip for two reasons,
// both load-bearing:
//
//   - NO SSE DELTA. The column is deliberately not on the wire (no DTO field),
//     so a member delta on the SSE first-connect edge and on every session
//     boundary would be pure churn — and the connect edge is the busiest edge
//     the fleet has. Two tests catch this directly if it regresses
//     (TestOutsourceWorkerSSEEdgesPublishCanonicalPresence,
//     TestEventsHandler_DeliveredWardenCommandsLeaveNoResidue).
//   - NO WHOLE-ROW WRITE. The callers (onFirstConnect / clearSessionBootTS) run
//     inside the reconcile tick and on the SSE edge, next to HTTP faces that
//     write member rows without holding reconcileMu. A whole-row write from
//     either of those would put a snapshot back over whatever landed meanwhile;
//     touching exactly one column cannot.
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) SetMemberSessionBootTS(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE member SET session_boot_ts = ? WHERE id = ?`, ts, id)
	return err
}

// SetMemberWakingSince writes ONLY member.waking_since (T-14) — the DURABLE
// wake anchor PresenceState projects 「喚醒中」 from, for BOTH kinds.
//
// Targeted column UPDATE for the second of SetMemberSessionBootTS's two reasons
// (NO WHOLE-ROW WRITE): the outsource caller (notifyWorkerSpawn) is mid-dispatch
// and the placement-block stamps immediately after it re-read the row and write
// it back whole, so a snapshot write here would be clobbered by them. Touching
// exactly one column cannot be. The first reason does NOT apply — presence IS on
// the wire, and the dispatch fans its own outsource_worker delta right after.
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) SetMemberWakingSince(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE member SET waking_since = ? WHERE id = ?`, ts, id)
	return err
}

// SetMemberWindDownAnchors writes the four wind-down anchor columns and NOTHING
// else (T-55) — stopping_since / stopped_since / refocus_since / refocus_op, the
// rung of the 下線 → 加速 → 強制 ladder a member is standing on plus the 換手
// epoch opened on it. It is the sole writer of all four; a whole-row write
// carries them on INSERT and never onto an existing row (their constructors
// declare them insertOnly).
//
// 🔑 IT DELIBERATELY DOES NOT ROUTE THROUGH PatchMember, and that is a decision
// rather than an oversight (T-63). Converging a setter onto the patch door earns
// something only when the setter RE-IMPLEMENTS a property already declared on
// the column — SetMemberForcedStopAt and SetMemberAgentIatFloor each wrote their
// own max() beside a forwardOnly declaration, which is one property with two
// representations that drift apart without anything going red. These four
// columns declare no such property: they are a plain assignment here and
// insert-only there, and insert-only has no meaning for a caller that names the
// column outright. So there is nothing here to keep in sync, and rewriting it
// would be churn.
//
// 🔴 FOUR COLUMNS, ONE WRITER — the same shape as SetMemberOpReceipt and for the
// same reason. armRefocusEpoch stamps all four at once, and the readers consume
// them together: StopIntent is stopping_since > 0, the ladder gate weighs the
// refocus epoch against both stop anchors, and the collect fires on
// stopped_since while refocus_op still names which verb opened the window. Four
// separate writers would manufacture, on every stamp, a moment in which the row
// describes a rung nobody ever stood on.
//
// ⚠️ forced_stop_at is NOT here and must not join: it is the durable record that
// a session was cut off, and a whole-row write is allowed to carry it precisely
// because the column is declared forwardOnly, so every writer lands it as
// max(). A setter that ASSIGNED it would hand every caller the ability to move
// it backwards — which is exactly what SetMemberForcedStopAt used to do before
// it was routed through that declaration.
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) SetMemberWindDownAnchors(id string, stoppingSince, stoppedSince,
	refocusSince float64, refocusOp string) error {
	_, err := d.wdb.Exec(
		`UPDATE member SET stopping_since = ?, stopped_since = ?,
			refocus_since = ?, refocus_op = ? WHERE id = ?`,
		stoppingSince, stoppedSince, refocusSince, refocusOp, id)
	return err
}

// SetMemberDesiredMachineID writes ONLY member.desired_machine_id (T-55) — the
// owner's placement pin, "" when the member waits for a placement. It is the
// SOLE writer that moves the column; a whole-row write carries it on INSERT and
// never onto an existing row (mfDesiredMachineID declares it insertOnly).
//
// The pin is written by three faces that do not share a lock — the member
// relocate, the member activate, and the worker relocate — and each of them
// reaches this row holding a snapshot read before the others landed. While the
// column rode the whole-row SET list, an activate that happened to be in flight
// put its own (older) pin back over a relocate that had just moved the member,
// and the member then booted on the machine the owner had just moved it OFF.
// Nothing goes red when that happens; the placement simply disagrees with what
// the cockpit shows.
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) SetMemberDesiredMachineID(id, machineID string) error {
	_, err := d.wdb.Exec(
		`UPDATE member SET desired_machine_id = ? WHERE id = ?`, machineID, id)
	return err
}

// SetMemberModel / SetMemberRuntime / SetMemberEffort write ONLY their own
// column (T-55) — the three LAUNCH INTENTS the owner edits in 成員設定 and in
// the outsource worker's twin face. Each is the SOLE writer of its column;
// a whole-row write carries all three on INSERT and none of them onto an
// existing row (their constructors declare them insertOnly).
//
// They are three writers rather than one because the two editing faces write
// them INDEPENDENTLY: every field arrives optional, and a save that carries
// only `effort` must not restate the model the row already holds — restating it
// is precisely how a stale snapshot lands. The faces also sit next to the
// owner-op wind-down, which re-reads and re-writes the same row while the edit
// is still being answered.
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) SetMemberModel(id, model string) error {
	_, err := d.wdb.Exec(`UPDATE member SET model = ? WHERE id = ?`, model, id)
	return err
}

// SetMemberRuntime writes ONLY member.runtime — see SetMemberModel.
//
// ⚠️ It stores what it is given, WITHOUT NormalizeRuntime: "" is a durable
// third state ("nobody has picked yet"), distinct from an owner who picked
// claude, and resolveEmptyRuntimeForPlacement depends on telling them apart.
// PutMember's INSERT binds the raw value for the same reason.
func (d *DAL) SetMemberRuntime(id, runtime string) error {
	_, err := d.wdb.Exec(`UPDATE member SET runtime = ? WHERE id = ?`, runtime, id)
	return err
}

// SetMemberEffort writes ONLY member.effort — see SetMemberModel.
func (d *DAL) SetMemberEffort(id, effort string) error {
	_, err := d.wdb.Exec(`UPDATE member SET effort = ? WHERE id = ?`, effort, id)
	return err
}

// SetMemberOpReceipt writes the five last_op* columns and NOTHING else (T-55) —
// the OP RECEIPT the cockpit renders as the ✓/✗ block under a member or worker.
// It is the sole writer of all five; a whole-row write carries them on INSERT
// and none of them onto an existing row (their constructors declare them
// insertOnly).
//
// 🔴 ONE WRITER FOR FIVE COLUMNS, not five writers — the opposite of the launch
// intents next door, and for the opposite reason. receiptRendersAsFailure reads
// last_op, last_op_at and last_op_ok TOGETHER to decide what the panel shows, so
// any moment in which some of the five have landed and the rest have not is a
// state the cockpit renders as a verdict nobody wrote. Five writers would
// manufacture that moment on every stamp. The launch intents split because their
// two editing faces genuinely write them independently; a receipt is always
// written whole.
//
// 🔴 ok is *bool BECAUSE THE COLUMN IS THREE-VALUED — nil / true / false, with
// nil meaning "nothing folded yet" and stored as SQL NULL (scanMember reads it
// back through sql.NullBool). It is NOT a bool with nil folded onto false:
// receiptRendersAsFailure treats nil and false alike for RENDERING, but three
// clear paths (clearWorkerPlacementBlock and the two converged clears) write nil
// back DELIBERATELY, and a bool signature would silently turn each of them into
// a written `false` — a verdict, where they meant to withdraw one.
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) SetMemberOpReceipt(id, op string, ok *bool, log, reason string, at float64) error {
	// nil interface binds SQL NULL, exactly as PutMember's own bind does — the
	// round trip that keeps the third state a third state.
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

// HardDeleteMember PHYSICALLY deletes a member row (the custom-role cascade
// path) — NOT the roster_status="removed" soft-remove, which stays the
// audit-preserving dismiss seam. Returns true iff a row was deleted.
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
		// The avatar id is dedicated by contract, but deletion must remain safe
		// even if a future writer drops that guard or old/corrupt data contains
		// another reference. The member row is already gone inside this tx, so
		// only a genuinely surviving record can veto collection here.
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

// chatListFilter is the set of participant-side narrowings every LISTING path
// of GET /api/chat shares: `with` (either side of a message) and the T-48
// one-sided `sender` / `recipient`. They AND; a blank field is no filter at all.
//
// It is ONE type rather than three parameters passed around because the listing
// queries must narrow IDENTICALLY. A filter honoured by the newest page and
// forgotten by the page after it does not fail — it hands the caller rows it
// promised to withhold, one page in, and nothing says so.
//
// The `caller_only` boolean used to be a fourth field here (owner ruling
// rc-09f6d801b2b8 removed it from the wire). `sender`/`recipient` naming the
// caller's own id is the replacement, and it is strictly more expressive: the
// flag matched EITHER side, so "only mine" could not say which.
type chatListFilter struct {
	participant string // ?with= — matches EITHER side
	sender      string // ?sender= — one side only
	recipient   string // ?recipient= — one side only
}

// appendSQL appends this filter's conjuncts to a WHERE clause that already ends
// in a condition. `col` prefixes the column names: "" for the single-table
// reads, "m." for the unread read, which joins chat_read and must say which
// table it means.
func (f chatListFilter) appendSQL(query *string, args *[]any, col string) {
	if f.participant != "" {
		*query += ` AND (` + col + `sender = ? OR ` + col + `recipient = ?)`
		*args = append(*args, f.participant, f.participant)
	}
	if f.sender != "" {
		*query += ` AND ` + col + `sender = ?`
		*args = append(*args, f.sender)
	}
	if f.recipient != "" {
		*query += ` AND ` + col + `recipient = ?`
		*args = append(*args, f.recipient)
	}
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
	return d.listChatBefore(chatListFilter{participant: participant}, beforeTS, beforeID, limit)
}

func (d *DAL) listChatBefore(f chatListFilter, beforeTS float64, beforeID string, limit int) ([]ChatMessage, error) {
	if limit == 0 {
		return nil, nil
	}
	query := `
		SELECT id, sender, recipient, body, ts, meta FROM chat_message
		WHERE (ts < ? OR (ts = ? AND id < ?))`
	args := []any{beforeTS, beforeTS, beforeID}
	f.appendSQL(&query, &args, "")
	query += ` ORDER BY ts DESC, id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := d.rdb.Query(query, args...)
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

// chatAnchor is one endpoint of a start_id/end_id window: the (ts, id) pair of
// a real message, resolved before any row is read so an unknown id can be
// refused by name rather than answered as an empty page. The pair — not the id
// alone — is what the window is expressed in, because the stream's order is
// (ts, id) and comparing on id alone would order nothing.
type chatAnchor struct {
	TS float64
	ID string
}

// newerThan reports whether a comes strictly after b in the stream's total
// (ts, id) order — the SAME comparison listChatBefore pages by, so "start is
// past end" here means exactly what "older than the cursor" means there.
func (a chatAnchor) newerThan(b chatAnchor) bool {
	if a.TS != b.TS {
		return a.TS > b.TS
	}
	return a.ID > b.ID
}

// listChatWindow answers the T-48 start_id/end_id window: the messages between
// the two anchors INCLUSIVE, oldest→newest, capped at `limit`.
//
// Either anchor may be nil, and which one is nil decides which END the cap eats
// from — that is the whole difference between the two parameters:
//
//   - start only  — the anchor and the limit-1 messages AFTER it. Ascending
//     LIMIT: the far (newest) end is what gets cut.
//   - end only    — the anchor and the limit-1 messages BEFORE it. Descending
//     LIMIT then reversed: the far (oldest) end is what gets cut, which is the
//     same walk listChatBefore does, only inclusive of the anchor.
//   - both        — one bounded window, anchored at END: descending LIMIT then
//     reversed, so an over-wide window keeps end_id and loses rows at the
//     start_id (older) side. This is spec's own wording — "a window wider than
//     200 rows is truncated from the `start_id` end" — and that sentence is the
//     ONLY place the both-anchors case is specified. The `start_id` paragraph
//     ("this message and the limit-1 that follow it") describes the start-only
//     window and says nothing about this case; reading it as a rule for both
//     was the mistake, and spec now carries a pointer saying so.
//     🔑 Either truncation falsifies one anchor's INCLUSIVE label. Spec already
//     chose which one; this code does not get to re-choose.
//
// A window with no anchor at all is not this function's business — the handler
// only reaches here once at least one was sent, and the anchorless request is
// the legacy path, which must not change.
//
// `limit` arrives already validated to 1..200 by the handler; this function
// still guards limit <= 0 as "read nothing" so it cannot be turned into an
// unbounded scan by a future caller that forgets.
func (d *DAL) listChatWindow(f chatListFilter, start, end *chatAnchor, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := `
		SELECT id, sender, recipient, body, ts, meta FROM chat_message
		WHERE 1=1`
	var args []any
	if start != nil {
		query += ` AND (ts > ? OR (ts = ? AND id >= ?))`
		args = append(args, start.TS, start.TS, start.ID)
	}
	if end != nil {
		query += ` AND (ts < ? OR (ts = ? AND id <= ?))`
		args = append(args, end.TS, end.TS, end.ID)
	}
	f.appendSQL(&query, &args, "")
	// end present ⇒ anchor there and walk backwards (descending LIMIT, then
	// reversed) so truncation eats the start_id/older side, which is what spec
	// specifies for the both-anchors case. start-only ⇒ walk forwards.
	descending := end != nil
	if descending {
		query += ` ORDER BY ts DESC, id DESC LIMIT ?`
	} else {
		query += ` ORDER BY ts, id LIMIT ?`
	}
	args = append(args, limit)
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
	if !descending {
		return got, nil
	}
	out := make([]ChatMessage, len(got))
	for i, m := range got {
		out[len(got)-1-i] = m
	}
	return out, nil
}

// ListChatLatest returns the most recent `limit` messages, oldest→newest —
// the CURSORLESS page GET /api/chat serves. `participant` narrows to a
// conversation line (sender OR recipient); "" disables the filter. limit 0 reads
// nothing, a NEGATIVE limit disables the cap.
//
// It replaces "ListChat() then filter and slice in Go": that pulled every row
// of chat_message into memory to hand back at most 30 of them (68.115ms on a
// measured 48,153-row table, against 0.697ms here). The result is IDENTICAL,
// not merely equivalent — "the newest N of the stream's total (ts, id) order"
// is the same set whether you take the last N ascending or the first N
// descending-then-reverse, which is exactly what the two paths do.
//
// 🔴 DELIBERATELY NO SINGLE-COLUMN INDEX on (sender, recipient) or (ts):
// measured on the same real table, adding one made the scrollback page
// (listChatBefore) 23× SLOWER, and ANALYZE changed nothing. This scan is the
// cheap side of that trade.
//
// ⚠️ T-48: a COMPOSITE index on (recipient, sender, ts) DOES now exist
// (migration 00075) — do not read the paragraph above as "this table carries no
// index". It is a different shape, added for a different query: it COVERS the
// unread count (2.7× there) and leaves this path untouched, because the
// scrollback query cannot use it at all and the planner keeps
// idx_chat_message_ts. The two statements are about two different indexes; the
// 23× result above still stands for the single-column one.
// 🔑 The 00075 numbers are SYNTHETIC (owner ruled it in on that basis, knowingly
// — rc-6b67aa1a331c). The 23× above is from a real-data copy. Do not quote them
// as if they came from the same measurement.
func (d *DAL) ListChatLatest(participant string, limit int) ([]ChatMessage, error) {
	return d.listChatLatest(chatListFilter{participant: participant}, limit)
}

func (d *DAL) listChatLatest(f chatListFilter, limit int) ([]ChatMessage, error) {
	if limit == 0 {
		return nil, nil
	}
	query := `SELECT id, sender, recipient, body, ts, meta FROM chat_message WHERE 1=1`
	var args []any
	f.appendSQL(&query, &args, "")
	if limit < 0 {
		// Uncapped: no need for the DESC walk + reverse, ask for the order the
		// caller wants directly.
		rows, err := d.rdb.Query(query+` ORDER BY ts, id`, args...)
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
	rows, err := d.rdb.Query(query+` ORDER BY ts DESC, id DESC LIMIT ?`,
		append(args, limit)...)
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
	out := make([]ChatMessage, len(newestFirst))
	for i, m := range newestFirst {
		out[len(newestFirst)-1-i] = m
	}
	return out, nil
}

// listChatUnread answers `GET /api/chat?unread=true`: the messages `reader` has
// not read yet, OLDEST FIRST, optionally continued from `after` (exclusive).
//
// 🔴 THE WATERMARK IS PER (reader, sender), AND THAT IS THE WHOLE POINT.
// chat_read holds one row per (reader_id, peer_id) pair, so each message is
// compared against ITS OWN sender's row — the LEFT JOIN is pinned on
// `r.peer_id = m.sender` in the ON clause, exactly as UnreadCountsFor pins it.
// Comparing every message against ONE watermark instead (the reader's newest
// row, say, or a single scalar) is the mutant this shape exists to refuse: a
// reader who is current with A and has never opened B would have B's whole
// history judged against A's high-water mark and silently dropped. The failure
// is a SHORT PAGE, which is indistinguishable from having nothing unread — no
// error, no count, nothing to notice.
//
// 🔴 COALESCE(..., 0) IS THE MISSING-ROW DEFAULT, and it must stay on a LEFT
// join: an INNER join would drop exactly the senders the reader has never
// opened, which are the ones whose unread matters most.
//
// 🔴 YOUR OWN MESSAGES ARE NOT EXCLUDED HERE, AND THAT IS AN OWNER RULING, NOT
// AN OVERSIGHT (rc-dccab860be32). This predicate used to carry `sender <> reader`.
// The owner's objection was about WHERE the rule lives: this function answers one
// mechanical question — is it newer than my watermark for whoever sent it — and
// "I do not need to be shown my own note" is a decision about PRINTING, not about
// reading. So a message you addressed to yourself comes back here as unread, and
// the drain is what declines to print it (and files the receipt anyway, which is
// the one place in T-48 where something is marked read without being shown; see
// cli/ocagent's note).
//
// ⚠️ `recipient = ?` already keeps out everything you sent to SOMEBODY ELSE, so
// the only messages this concerns are the ones you sent to yourself.
//
// 🔴 THIS FUNCTION WRITES NOTHING. Reading unread does not clear unread; that is
// PutChatRead's job, reached only from POST /api/chat/mark-read.
//
// `limit` keeps GET /api/chat's legacy semantics rather than the window path's
// 1..200 bound: 0 reads nothing and a NEGATIVE limit is uncapped. A blank reader
// reads nothing — an unauthenticated caller has no watermarks, and answering
// "every message addressed to nobody" would be a different question.
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
		query += ` AND (m.ts > ? OR (m.ts = ? AND m.id > ?))`
		args = append(args, after.TS, after.TS, after.ID)
	}
	f.appendSQL(&query, &args, "m.")
	query += ` ORDER BY m.ts, m.id`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := d.rdb.Query(query, args...)
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

// ListChatByIDs returns the messages carrying the given ids, oldest→newest in
// the stream's total (ts, id) order — the by-id re-read behind
// `get_chat?ids=` (T-a828). A blank id list reads nothing.
//
// 🔴 IT DOES NOT FILTER BY CALLER. Since T-4e95 nothing above it does either:
// the by-ids read reaches as far as the ordinary listing does, because the
// listing filters on `with` — a PARTICIPANT — not on the caller, and two doors
// onto the same rows must not disagree about who may open them. This function
// was already caller-blind before that ruling (a query that filtered here would
// have collapsed "no such message" and "not yours" into one empty row set,
// leaving the handler to guess); it is now caller-blind all the way up.
//
// Ids are matched exactly and the result carries at most one row per id, so a
// duplicated id cannot inflate the answer. Rows are returned for whichever ids
// exist; the caller compares the returned set against what it asked for.
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

// ListChatInvolving returns the most recent `limit` messages involving
// `participant` (sender OR recipient), oldest→newest — the bounded
// wake-snapshot read. A blank participant / non-positive limit reads nothing.
//
// 🔴 GLOBAL newest-N, and that is the point. A per-conversation-line quota
// (ListChatPerPeerInvolving, removed 2026-08-13) existed to feed a per-line
// floor in the packer; that floor was the reason the wake snapshot had no upper
// bound at all, and the owner removed it (「不要管每條對話線」). The packer now
// walks this one stream newest-first and stops at the budget, so a per-line read
// would only return rows nobody can spend — the measured member read 6,600 rows
// per wake to fill a 12,000-rune budget.
//
// COST: one query, one full chat_message scan (`sender`/`recipient` carry no
// index) — unchanged. What changed is the row count it hands back: bounded by
// `limit` outright rather than by limit × the caller's number of correspondents.
func (d *DAL) ListChatInvolving(participant string, limit int) ([]ChatMessage, error) {
	if participant == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := d.rdb.Query(`
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

// sqlExecer is the write seam shared by the standalone (d.wdb) and the
// transactional (*sql.Tx) forms of every chat-side upsert — ONE statement per
// record, reachable both ways, so the atomic variants below cannot drift from
// the single-write ones.
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// sqlQuerier is the read counterpart of sqlExecer. It exists so a document's
// pre-write state can be read from inside the very transaction that overwrites
// it: a snapshot read on d.rdb would be a different point in time from the
// write, and two writers folding from the same read would then retain the same
// old revision twice while one of their results became unrecoverable.
type sqlQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// DocumentHistory is one immutable pre-write snapshot. ContentJSON deliberately
// stores the complete editable document so a restore never assembles fields
// from revisions written at different times.
type DocumentHistory struct {
	ID           int64
	DocumentKind string
	DocumentKey  string
	ContentJSON  string
	CreatedTS    float64
	ActorID      string
}

// documentHistoryKeepDefault is how many pre-write snapshots a document keeps
// when nothing says otherwise.
const documentHistoryKeepDefault = 3

// documentHistoryKeepByKind raises the retained-version depth for the owner's
// editable boot-context/global documents: system_interaction, which has the
// single key `global`; boot_sequence, which has one key per runtime (`claude`,
// `codex`); and offboard (T-c9c0), a singleton keyed `global`. Four documents
// across the three kinds, which is why this table has three entries while the
// sentence below counts documents.
//
// WRITTEN DOWN BECAUSE IT IS A DECISION, NOT AN OVERSIGHT: those documents are
// the ones an owner can now retype at will from the cockpit,
// and the sequence that matters — "put back the version from
// before I broke it" — is exactly the one a handful of idle saves would push off
// the end of a three-deep list. Ten is the owner's number; every other kind
// stays at three deliberately, because nothing about this change made THEM
// easier to churn.
//
// The no-op guard in the handlers is the other half of the same protection (a
// save that changes nothing retains nothing at all); depth alone would still be
// spent by anyone who edits, reverts, and edits again.
var documentHistoryKeepByKind = map[string]int{
	docKindSystemInteraction: 10,
	docKindBootSequence:      10,
	docKindOffboard:          10,
	// ALL SIX event procedures (T-3201, widened T-6f44) are retyped from the same
	// text box and churn the same way, so they get the same depth. Two of them
	// used to be absent on the argument 「nothing can write them, so a depth for
	// them would be a number about a list that is always empty」 — true while they
	// were read-only, expired the moment decision 2 made them editable. Leaving
	// them out would have given two of the ten a three-deep history nobody chose.
	docKindAcceleratedStop:             10,
	docKindTaskCloseout:                10,
	docKindTaskReassignPredecessor:     10,
	docKindTaskTakeoverWithPredecessor: 10,
	docKindTaskTakeoverFresh:           10,
	docKindTaskUnblocked:               10,
}

// documentHistoryKeepFor answers the depth for one kind: the table above, else
// documentHistoryKeepDefault. A kind that is not listed is not a missing entry
// — three is the answer for every document that has not been argued up.
func documentHistoryKeepFor(kind string) int {
	if keep, ok := documentHistoryKeepByKind[kind]; ok {
		return keep
	}
	return documentHistoryKeepDefault
}

// documentHistoryStream addresses one retained-version series plus the reader
// that serializes its live state. One row can carry SEVERAL independent series:
// a task manual versions its SOP and its learnings separately (T-1f39), so a
// write touching both retains one revision in each — inside the single
// transaction that writes the row, never as two writes.
type documentHistoryStream struct {
	Kind     string
	Key      string
	ActorID  string
	Snapshot func(sqlQuerier) (string, error)
}

// SaveWithDocumentHistory atomically retains the current document (when it is
// non-empty), writes its replacement, and trims only snapshots older than the
// newest three. snapshot re-reads and serializes the live document from inside
// the transaction, so the retained revision is the state this write actually
// replaced — not whatever the caller happened to read earlier.
func (d *DAL) SaveWithDocumentHistory(kind, key, actorID string, snapshot func(sqlQuerier) (string, error), write func(sqlExecer) error) error {
	return d.SaveWithDocumentHistories([]documentHistoryStream{
		{Kind: kind, Key: key, ActorID: actorID, Snapshot: snapshot},
	}, write)
}

// SaveWithDocumentHistories is the several-streams form: every stream is
// retained and trimmed independently, then the single write lands. An empty
// stream list is a legal write that versions nothing — that is what an edit to
// an unversioned field (a manual's purpose or identifier fields) is.
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

// PutChat upserts a chat message.
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
// meta["attachments"] refs (the only message→blob linkage), so no blob is
// orphaned. A blob is deleted ONLY when no surviving record still references
// it: ref-form post_chat lets one blob ride several messages, a reply-card
// blob (answer- or question-side) could be re-referenced from chat, and a blob
// uploaded in chat can be PINNED onto a task card as a deliverable — so the
// cascade re-checks every survivor before dropping a blob.
//
// This is the widest GC path for the GENERAL attachment graph
// (`DeleteTaskArtifact` leaves the LIVE artifact's blob alone, and collects the
// blobs of its retained versions; `ReplaceTaskArtifact` collects the blob of a
// version it trims off the end). The T-c826 avatar lifecycle has its own
// single-owner delete paths; HardDeleteMember reuses this same survivor scan so
// corrupt/legacy cross-references still fail safe. As of T-60 the
// blob-referencing columns in the schema are exactly these six, and
// `collectSurvivingBlobRefs` reads all six:
//
//	chat_message.meta $.attachments[].id
//	reply_card.answer_attachments[].id
//	reply_card.attachments[].id          (T-5e8a question side)
//	task_artifact.attachment_id          (T-62a8 — EVERY kind since T-92: a link's
//	                                     target is a text/uri-list blob, so link rows
//	                                     vote too and this code needed no change)
//	member.avatar_attachment_id           (T-c826 — dedicated personal image)
//	task_artifact_history.attachment_id   (T-60 — a REPLACED artifact version)
//
// ⚠️ ONE column holds blob ids and deliberately does NOT vote:
// `chat_attachment_ref.attachment_id` (T-51, migration 00074). That table is a
// DERIVED index over source #1 above — every row of it restates a
// `chat_message.meta $.attachments[].id` that is already counted here — so
// letting it vote would keep a blob alive on the strength of its own referrer
// and re-open exactly the T-62a8 failure below.
// `TestDeleteChatInvolvingIgnoresTheGalleryIndex` pins that it never votes.
//
// ⚠️ Add another NON-DERIVED referencing column anywhere and it MUST be added
// here in the same commit; a blob whose only referrer is unknown to this scan is deleted
// out from under that referrer with no error and no receipt. The failure mode
// this closed was exactly that: a deliverable pinned on a task card went to a
// dead link when the chat message it was uploaded in was removed — and a
// terminal task's artifact set is frozen in every direction, so it could not
// be re-attached.
//
// ⚠️ Honest limit — this scan is NOT a general GC. It only ever considers the
// blobs the just-deleted messages referenced (`candidates`); nothing re-visits
// a blob later. So a blob spared here because a task_artifact held it becomes
// permanently uncollectable if that LIVE artifact is subsequently un-pinned
// (`DeleteTaskArtifact` leaves the live blob alone by decree, and un-pinning is
// refused once the task is terminal, so the window is narrow). That is a
// bounded disk leak accepted in exchange for not destroying a deliverable —
// the two are not symmetric: the leak is recoverable by a future sweep, the
// deletion is not recoverable at all. A real reachability sweep over the six
// columns below is the proper fix and does not exist yet. T-60 deliberately did
// NOT extend that leak to the versions a replace retires: those blobs are
// collected at the moment they stop being nameable (a trim, or the un-pin that
// deletes the whole series), which is the only moment anything still knows they
// existed.
//
// Returns (deletedMessages, deletedAttachments).
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

// collectOrphanBlobs deletes, from a set of candidate blob ids, exactly those
// that no still-stored record references — the single decision point every
// blob collection goes through, so "is this blob still someone's?" is answered
// by collectSurvivingBlobRefs and nowhere else. Call it AFTER the rows that
// referenced the candidates are gone, from inside that same transaction.
// Returns how many blobs were actually deleted.
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

// collectSurvivingBlobRefs folds every chat_attachment id that a STILL-STORED
// record references into `into` — the complete liveness verdict for the blob
// store (the six columns enumerated on DeleteChatInvolving). Called after the
// rows that referenced the candidates are deleted inside the same tx, so "still
// stored" is read against the post-delete state.
//
// Deliberately one function rather than three inline blocks: the bug this
// closed (T-62a8) was a MISSING source, and a missing source is far easier to
// notice against a list than against a stretch of procedural code.
func collectSurvivingBlobRefs(tx *sql.Tx, into map[string]bool) error {
	// 1. surviving chat messages — ref-form post_chat lets one blob ride
	//    several messages, so a blob can outlive the message it arrived on.
	if err := collectChatMetaRefs(tx, `SELECT meta FROM chat_message`, into); err != nil {
		return err
	}

	// 2+3. reply cards (cards live forever). A card references blobs from
	//      BOTH sides: answer_attachments (the owner's answer) AND
	//      attachments (the T-5e8a question-side refs the card was opened
	//      with). The question refs are also stamped into the companion chat
	//      message's meta, so the companion's deletion puts them on the
	//      candidate list; the surviving card must veto that.
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

	// 4. task artifacts (T-62a8) — a deliverable pinned onto a task card.
	//    task_artifact rows have no cascade of their own and a terminal
	//    task's set is frozen in every direction, so this reference is the
	//    strongest one in the schema: dropping its blob is unrecoverable.
	//    ⚠️ THE FILTER USED TO BE ABOUT LINKS, AND SINCE T-92 IT IS NOT. Link
	//    rows carried no blob and an empty attachment_id, and the filter kept
	//    them from voting for a nonexistent empty-id blob. T-92 gave every kind
	//    a blob, so link rows now vote like any other — this predicate did not
	//    change and did not have to, because it never named a kind. What it
	//    still guards is a blank id, which the schema no longer produces but
	//    which costs nothing to keep refusing.
	//
	//    COALESCE, not a bare `attachment_id <> ''`, is DEFENSIVE STYLE HERE
	//    AND NOTHING MORE — do not read it as a fail-safe, because it is not
	//    one. The column is today NOT NULL (00086 dropped its DEFAULT '' along
	//    with the `url` column; only that migration's Down block puts the
	//    default back), so NULL cannot occur and the two spellings are
	//    identical in practice. And on a NULL they would still agree, in the
	//    UNSAFE direction: `COALESCE(NULL,'') <> ''` is 0 and `NULL <> ''` is
	//    NULL, and WHERE keeps neither — the row stops voting either way.
	//    ⚠️ SO IF ANYONE EVER MAKES THIS COLUMN NULLABLE, changing nothing
	//    here reintroduces the original defect silently, and the COALESCE will
	//    not have saved you. The fail-safe direction of this predicate is
	//    "vote", and buying that takes a different predicate —
	//    `attachment_id IS NULL OR attachment_id <> ''` — not this wrapper.
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

	// 5. personal member avatars (T-c826). Avatar ids are isolated behind an
	//    ava- prefix and general attachment writers reject that prefix, but the
	//    liveness verdict must not rely on a distant string guard: if a blob is
	//    referenced by a surviving member row, deleting it is data loss.
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

	// 6. RETAINED ARTIFACT VERSIONS (T-60) — a deliverable that was REPLACED
	//    keeps its previous versions, and a retained version's blob is as real
	//    a referrer as the live row's: replacing an artifact must not delete
	//    the file the earlier version still points at, and neither must the
	//    removal of an unrelated chat message.
	//
	//    COALESCE with the same caveat source 4 spells out: it is defensive
	//    style over a NOT NULL column, NOT a fail-safe. Against a NULL it
	//    yields 0 where a bare `<> ''` yields NULL, and WHERE drops the row
	//    either way — so making this column nullable would need the predicate
	//    changed to `attachment_id IS NULL OR attachment_id <> ''`, not just
	//    left alone.
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

// ChatAttachmentRef mirrors one row of chat_attachment_ref — the gallery's
// index over chat_message.meta.attachments (migration 00074). Sender,
// Recipient and TS are a SNAPSHOT of the owning message, and Ord is the
// attachment's position in that message's posted array.
//
// 🔴 THIS DAL NEVER WRITES THIS TABLE. Three triggers on chat_message do
// (insert / update / delete), so the index stays true no matter which writer
// stores the message and whether or not it opened a transaction. Do not add a
// Go write path here: it would be correct only while every writer goes
// through it, and the day one does not there is no signal.
//
// ⚠️ Ord is NOT dense: a ref filtered out for having no id leaves a hole, so
// never assume 0,1,2,... without gaps.
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

// chatAttachmentRefBefore reports whether a sorts before b in the gallery's
// order: newest first, then the message stream's own (ts, id) tie-break, then
// the attachment's position inside its message.
//
// 🔴 The message tie-break is ASCENDING on purpose. The pre-index handler read
// the whole table in (ts, id) ASC and re-sorted it with a STABLE sort on ts
// DESC, so equal-ts messages kept ascending id order. Flipping it here would
// change what the panel shows for messages posted in the same second — a
// visible change nobody asked for.
func chatAttachmentRefBefore(a, b ChatAttachmentRef) bool {
	if a.TS != b.TS {
		return a.TS > b.TS
	}
	if a.MessageID != b.MessageID {
		return a.MessageID < b.MessageID
	}
	return a.Ord < b.Ord
}

// listChatAttachmentRefsOneSided reads the rows of ONE side of the
// conversation, already in gallery order. `extra` is appended to the WHERE.
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

// ListChatAttachmentRefsFor returns every attachment of the member's
// conversations (sender OR recipient), newest→oldest, read from the index
// instead of scanning chat_message.
//
// 🔴 TWO SINGLE-SIDED QUERIES MERGED IN GO, NOT `sender = ? OR recipient = ?`.
// Measured: the OR form makes SQLite answer with MULTI-INDEX OR and then a
// TEMP B-TREE over every matching row — it throws away the ordering the two
// indexes already provide. Each single-sided query is fully satisfied by its
// index (no "USE TEMP B-TREE FOR ORDER BY" in the plan), so both arrive sorted
// and merging them is linear.
//
// 🔴 The recipient side excludes sender = recipient. A self-message matches
// BOTH sides, so without this it comes back twice (verified by seeding one:
// OR form = 1 row, two-query form = 2). There are none today and nothing
// prevents one.
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
		if chatAttachmentRefBefore(received[j], sent[i]) {
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

// GetChatAttachment returns one attachment blob by id, or nil if absent.
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

// ReplaceMemberAvatar atomically stores a freshly minted dedicated avatar,
// switches the stable member pointer, and deletes the prior dedicated blob.
// The member must already exist; a vanished row is errNotFound.
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

// DeleteMemberAvatar atomically clears the pointer and deletes the owned blob.
// It is idempotent when the member already has no personal avatar.
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

// UnreadCountsFor is UnreadCounts (domain.go) computed BY THE DATABASE: for
// `reader`, the number of messages addressed to them, per sender, that are
// newer than that sender's read watermark. Senders with nothing unread are
// absent from the map — same shape the Go fold produces.
//
// It exists because both call sites (unreadCountsForRequest and the
// unread-count endpoint) used to read the WHOLE chat_message table and the
// reader's whole chat_read set into Go just to fold them down to a handful of
// integers: 69.098ms on the measured real table, against 22.303ms here.
//
// 🔴 The LEFT JOIN + COALESCE(..., 0) IS the Go zero-value default: "no receipt
// ⇒ watermark 0 ⇒ every addressed message counts". An INNER JOIN would silently
// drop exactly the peers a reader has never opened — the ones whose unread
// matters most. The join is pinned to `reader` in the ON clause (not the WHERE)
// so a missing receipt still yields the row.
//
// domain.UnreadCounts is NOT dead: it is the pure fold this must agree with,
// and the equivalence test drives both over the same fixtures.
func (d *DAL) UnreadCountsFor(reader string) (map[string]int, error) {
	rows, err := d.rdb.Query(`
		SELECT m.sender, COUNT(*) FROM chat_message m
		LEFT JOIN chat_read r ON r.reader_id = ? AND r.peer_id = m.sender
		WHERE m.recipient = ? AND m.ts > COALESCE(r.last_read_ts, 0)
		GROUP BY m.sender`, reader, reader)
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

// PutChatRead upserts a read receipt on the composite (reader, peer) key.
// MONOTONIC: the stored last_read_ts only ever ADVANCES — a stale/equal report
// is a no-op (never rewinds, so a re-ordered report can't un-read a message).
// Returns the EFFECTIVE (possibly pre-existing, higher) watermark plus whether
// the watermark actually ADVANCED (repository.put_chat_read parity: stale/equal
// = "no write, no fan", so the caller must not publish a delta then). The
// effective ts alone cannot distinguish "advanced to exactly ts" from "already
// at ts" — the write's row count carries the signal.
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

// DeleteChatReadsInvolving HARD-deletes every receipt involving memberID (as
// reader OR peer) — the custom-role cascade sibling of DeleteChatInvolving.
// Returns the deleted count.
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
func (d *DAL) GetUserContext() (*UserContext, error) { return getUserContextOn(d.rdb) }

func getUserContextOn(q sqlQuerier) (*UserContext, error) {
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

// PutUserContext upserts the single block row.
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

// GetRoleDef returns one overlay by role key, or nil if never edited.
func (d *DAL) GetRoleDef(roleKey string) (*RoleDef, error) { return getRoleDefOn(d.rdb, roleKey) }

func getRoleDefOn(q sqlQuerier, roleKey string) (*RoleDef, error) {
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

// PutRoleDef upserts a role-definition overlay.
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

// DeleteRoleDef PHYSICALLY deletes an overlay row (custom-role hard delete —
// a custom role has no file seed to fall back to) — NOT the tombstone reset
// (PutRoleDef with Tombstoned), which stays the seed-role reset seam.
// Returns true iff a row was deleted.
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
		// The retained versions go with the document, in the SAME transaction:
		// a delete that removed the role but left its history would leave a
		// readable echo of a deleted document behind (the history read face is
		// open to every authenticated caller), and would make the guide's
		// 「永久移除」 false.
		_, err = tx.Exec(`DELETE FROM document_history
			WHERE document_kind = 'role_definition' AND document_key = ?`, roleKey)
		return err
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// ── lessons (per-role; role_key is the WHOLE key) ────────────────────────────

// Lessons mirrors the lessons table: the per-role learnings overlay (agents
// sharing a role share one doc). RoleKey is the entire identity of the
// document — the (role_key, task_type) composite was dropped in T-2
// (00062_drop_lessons_task_type.sql).
type Lessons struct {
	RoleKey    string
	Text       string
	Tombstoned bool
}

// GetLessons returns the overlay for roleKey, or nil if never edited.
func (d *DAL) GetLessons(roleKey string) (*Lessons, error) {
	return getLessonsOn(d.rdb, roleKey)
}

func getLessonsOn(q sqlQuerier, roleKey string) (*Lessons, error) {
	var l Lessons
	err := q.QueryRow(`
		SELECT role_key, text, tombstoned FROM lessons
		WHERE role_key = ?`, roleKey,
	).Scan(&l.RoleKey, &l.Text, &l.Tombstoned)
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
	return putLessonsOn(d.wdb, l)
}

func putLessonsOn(ex sqlExecer, l Lessons) error {
	_, err := ex.Exec(`
		INSERT INTO lessons (role_key, text, tombstoned)
		VALUES (?, ?, ?)
		ON CONFLICT (role_key) DO UPDATE SET
			text = excluded.text, tombstoned = excluded.tombstoned`,
		l.RoleKey, l.Text, l.Tombstoned)
	return err
}

// DeleteLessonsForRole HARD-deletes roleKey's overlay — the custom-role
// cascade: per-role lessons have no meaning without the role. Returns the
// deleted count.
func (d *DAL) DeleteLessonsForRole(roleKey string) (int, error) {
	var deleted int
	err := d.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM lessons WHERE role_key = ?`, roleKey)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		deleted = int(n)
		// This role's lessons history, in the same transaction. The key is
		// now the BARE role_key (T-2 dropped the "::<task_type>" half), so
		// this is an exact equality rather than the prefix match it used to
		// be — the same shape DeleteInsightForRole right below has always
		// used. An equality cannot over-reach onto a neighbouring role whose
		// key merely starts with this one.
		_, err = tx.Exec(`DELETE FROM document_history
			WHERE document_kind = 'lessons' AND document_key = ?`, roleKey)
		return err
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// ── insight (per-role; SINGLE role_key key) ──────────────────────────────────

// Insight mirrors the role_insight table: the per-role judgement doc — the
// trade-offs and boundaries this role keeps reaching for (T-3809). It is the
// sibling of Lessons, deliberately NOT the same document: lessons record what
// happened and what to do next time, insight records how this role weighs a
// call. The owner's whole reason for asking was that the two were mixed.
//
// A single-column primary key, the same shape Lessons has carried since T-2 —
// hence a BARE role_key as the document_history key.
type Insight struct {
	RoleKey    string
	Text       string
	Tombstoned bool
}

// GetInsight returns the row for roleKey, or nil if never written.
func (d *DAL) GetInsight(roleKey string) (*Insight, error) {
	return getInsightOn(d.rdb, roleKey)
}

func getInsightOn(q sqlQuerier, roleKey string) (*Insight, error) {
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

// PutInsight upserts a per-role insight doc.
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

// DeleteInsightForRole HARD-deletes the insight doc for roleKey — the
// custom-role cascade twin of DeleteLessonsForRole. Returns the deleted count.
//
// 🔴 EXACT EQUALITY on the history key. An insight history key is the BARE
// role_key — no terminator — so a prefix match would delete r-abcdef's retained
// versions while deleting r-abc. Exact equality is the only safe shape for a
// single-key document. Until T-2 the lessons cascade above was the one
// exception (its key was composite, "<role>::<task_type>", so its prefix
// carried a "::" terminator); the axis is gone and both cascades are now the
// same equality, which is why this reads as the house shape rather than as a
// contrast.
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
		// The retained versions go with the document, in the SAME transaction
		// — same reason DeleteRoleDef gives: the history read face is open to
		// every authenticated caller, so leaving them behind would be a
		// readable echo of a deleted document.
		_, err = tx.Exec(`DELETE FROM document_history
			WHERE document_kind = 'insight' AND document_key = ?`, roleKey)
		return err
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// BootDocument mirrors the boot_document table: the owner's overlay over ONE
// shipped boot-context block (T-791e) — the 系統互動 seed, or one runtime's
// 啟動步驟 seed. Same three-state shape as Insight: no row / tombstoned row =
// "serve the embedded seed", a live row = "serve this instead".
//
// The seed itself is never written here, which is what makes the reset route
// reach factory text by construction rather than by anyone remembering to keep
// a copy.
type BootDocument struct {
	Kind       string
	Key        string
	Text       string
	Tombstoned bool
}

// GetBootDocument returns the overlay for (kind, key), or nil if never written.
func (d *DAL) GetBootDocument(kind, key string) (*BootDocument, error) {
	return getBootDocumentOn(d.rdb, kind, key)
}

func getBootDocumentOn(q sqlQuerier, kind, key string) (*BootDocument, error) {
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

// PutBootDocument upserts one boot-context block overlay.
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

// AccountDisplayNames maps account tag -> display_name (the fold input; empty
// display names are skipped — absence folds to the id itself).
func (d *DAL) AccountDisplayNames() (map[string]string, error) {
	return d.displayNames(
		`SELECT account, display_name FROM account_alias WHERE display_name != ''`)
}

// PutAccountAlias upserts an account display-name overlay.
func (d *DAL) PutAccountAlias(a AccountAlias) error {
	_, err := d.wdb.Exec(`
		INSERT INTO account_alias (account, display_name) VALUES (?, ?)
		ON CONFLICT (account) DO UPDATE SET display_name = excluded.display_name`,
		a.Account, a.DisplayName)
	return err
}

// GetMachineAlias returns one overlay by machine id, or nil if never edited.
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

// MachineDisplayNames maps machine_id -> display_name (the fold input; empty
// display names are skipped).
func (d *DAL) MachineDisplayNames() (map[string]string, error) {
	return d.displayNames(
		`SELECT machine_id, display_name FROM machine_alias WHERE display_name != ''`)
}

// PutMachineAlias upserts a machine display-name overlay.
func (d *DAL) PutMachineAlias(a MachineAlias) error {
	_, err := d.wdb.Exec(`
		INSERT INTO machine_alias (machine_id, display_name) VALUES (?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET display_name = excluded.display_name`,
		a.MachineID, a.DisplayName)
	return err
}

// ── reply cards (等我回覆卡) ─────────────────────────────────────────────────

// ReplyCardOption is one frozen quick-reply choice (migrations/00065). AIPick
// — not the option's POSITION — is what marks the AI's own recommendation. The
// old convention ("options[0] is the AI pick") lived in prose only: no code
// ever read index 0 as special, so nothing enforced it and nothing broke when
// it was ignored. This field is that fact made executable.
type ReplyCardOption struct {
	Text   string `json:"text"`
	AIPick bool   `json:"ai_pick"`
}

// ReplyCard mirrors the reply_card table (migrations/00003, 00065): one ask the
// owner must answer. Options is the frozen quick-reply wording, each choice
// carrying its own AIPick flag; AnswerAttachments are light refs into the
// shared chat_attachment store, the same shape as chat meta["attachments"].
type ReplyCard struct {
	ID         string
	FromMember string
	Kind       string // closed set: "decision" | "action" (schema CHECK)
	Summary    string
	Body       string
	Options    []ReplyCardOption
	// SelectMode is the closed set "single" | "multi" (closed in code, no
	// schema CHECK — the 00013 posture). It is ORTHOGONAL to Kind: Kind says
	// what the owner must DO, SelectMode says how many options the answer may
	// carry.
	SelectMode    string
	Status        string // "waiting" | "answered" | "expired" (closed set in code; migrations/00013 dropped the CHECK)
	CreatedTS     float64
	AnsweredTS    float64 // 0.0 while waiting; latest answer time after
	ExpiredTS     float64 // 0.0 unless expired; when the expire action ran
	ChatMessageID string
	// AnswerOptionIdxs are the circled options' indices, stored DEDUPED and
	// ASCENDING so that two answers circling the same options are byte-equal
	// whatever order the owner clicked them in. nil = the answer carried no
	// option at all (free text / attachments only, or not answered yet).
	//
	// ⚠️ nil and an EMPTY non-nil slice are the same thing to every reader
	// here, and a nil check does NOT catch the empty one — test emptiness with
	// len(), never with == nil.
	AnswerOptionIdxs  []int
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

// ListReplyCards returns every card, oldest→newest (callers filter/sort per
// pane — the waiting/answered projections are handler concerns).
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

// GetReplyCard returns one card by id, or nil if absent.
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

// PutChatWithAttachments writes the message AND every fresh blob it references
// in ONE transaction: a message never exists without the blobs its refs name,
// and a failed write leaves NOTHING behind (the pre-T-e2b2 shape wrote each
// blob first and the message last, so a failure between them left blobs no
// record could ever name — invisible to the gallery, invisible to the GC walk
// that starts from message meta).
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

// PutReplyCardWithChat writes the card, its companion chat message, and every
// fresh question-side blob in ONE transaction — the same all-or-nothing rule as
// PutChatWithAttachments, extended over the card row because the message's
// meta.reply_card_id points AT that row: a message whose card write failed is a
// permanently dangling ask in the owner's chat stream.
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

// PutReplyCardWithAttachments writes the card row and every fresh blob it names
// in ONE transaction — the answer-side twin of PutReplyCardWithChat (there is
// no companion message on this path; the card row IS the record that names the
// blobs).
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

// inTx runs fn inside a WRITE transaction, rolling back on any error (and on
// panic — an un-rolled-back tx would hold the write pool's single connection
// forever).
//
// 🔴 It runs on the write pool, and that pool's DSN carries `_txlock=immediate`,
// so this Begin is BEGIN IMMEDIATE — not the driver default BEGIN DEFERRED. The
// bodies handed to inTx routinely READ and then WRITE (SaveWithDocumentHistories
// snapshots the live document inside the very transaction that overwrites it),
// and a DEFERRED transaction has to upgrade a read lock into a write lock, which
// is refused with an instant SQLITE_BUSY that `busy_timeout` does NOT cover.
//
// ⚠️ That refusal is a WAL-mode behaviour, not a general SQLite one (measured:
// rollback journal + DEFERRED never failed; WAL + DEFERRED failed 2 of 8, at
// 0-1ms against a 5s timeout). WAL is exactly what T-dd7a turned on, so the two
// go together — see openSQLite in migrate.go for the numbers.
//
// ⚠️ What that protects is a writer on ANOTHER HANDLE (`ocserverd backup`, a shell
// sqlite3), NOT our own concurrent writers — the one-connection cap already
// serialises those in Go, so an in-process upgrade conflict cannot arise. Do not
// read "IMMEDIATE" here as the thing keeping our own writes safe; the cap is.
// Details, and the measurement that settles which of the two does what, are on
// openSQLite in migrate.go.
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

// PutReplyCard upserts a card row (the SSE delta is the handler's job).
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
	// nil AND empty both store as SQL NULL: "no option was circled" has one
	// representation on disk, so a round trip cannot turn [] into an answer.
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

// GetWebhookByMemberEndpoint returns the endpoint addressed by (member,
// endpoint_id) — the management-route resolver — or nil when absent.
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

// ListWebhooksByMember returns a member's endpoints, oldest→newest.
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

// TouchWebhookReceived stamps last_received_ts only — the /in paths that prove
// the caller reached us but neither deliver nor drop (the Slack
// url_verification challenge, a verified GitHub ping).
func (d *DAL) TouchWebhookReceived(token string, ts float64) error {
	_, err := d.wdb.Exec(
		`UPDATE webhook_endpoint SET last_received_ts = ? WHERE token = ?`, ts, token)
	return err
}

// MarkWebhookDelivered counts one verified, chat-delivered payload (atomic
// increment — never a read-modify-write, so concurrent /in calls can't lose
// counts) and stamps last_received_ts.
func (d *DAL) MarkWebhookDelivered(token string, ts float64) error {
	_, err := d.wdb.Exec(
		`UPDATE webhook_endpoint
		 SET delivered_count = delivered_count + 1, last_received_ts = ?
		 WHERE token = ?`, ts, token)
	return err
}

// MarkWebhookDropped counts one silent drop with its coarse reason
// (WebhookDropReason* set) and stamps last_received_ts.
func (d *DAL) MarkWebhookDropped(token, reason string, ts float64) error {
	_, err := d.wdb.Exec(
		`UPDATE webhook_endpoint
		 SET dropped_count = dropped_count + 1, last_drop_reason = ?,
		     last_received_ts = ?
		 WHERE token = ?`, reason, ts, token)
	return err
}

// SetWebhookStatus flips one endpoint's status (the enable/disable toggle).
func (d *DAL) SetWebhookStatus(token, status string) error {
	_, err := d.wdb.Exec(
		`UPDATE webhook_endpoint SET status = ? WHERE token = ?`, status, token)
	return err
}

// DeleteWebhookEndpoint permanently revokes an endpoint (idempotent). Its
// request-log rows go with it — the ring buffer is debug data FOR an endpoint,
// never an orphaned archive of a dead token.
func (d *DAL) DeleteWebhookEndpoint(token string) error {
	if _, err := d.wdb.Exec(
		`DELETE FROM webhook_request_log WHERE token = ?`, token); err != nil {
		return err
	}
	_, err := d.wdb.Exec(`DELETE FROM webhook_endpoint WHERE token = ?`, token)
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

// ListWebhookRequestLogs returns an endpoint's ring buffer, newest→oldest
// (at most webhookRequestLogKeep rows by construction; LIMIT is belt-and-braces).
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

// ── scheduled messages (T-f059 定期訊息) ───────────────────────────────────────

// ScheduledMessageStatus closed set — the revocation toggle, mirroring the
// webhook one (migrations/00050). `disabled` suspends firing and is reversible;
// DELETE is the permanent removal.
const (
	ScheduledMessageStatusEnabled  = "enabled"
	ScheduledMessageStatusDisabled = "disabled"
)

// ScheduledMessageCadence closed set — which day field the slot computation
// reads: weekly reads DayOfWeek, monthly reads DayOfMonth, daily reads neither.
//
// `custom` (T-49e7) reads NONE of those four. It reads the four explicit sets
// CustomMonths/CustomDays/CustomHours/CustomMinutes (months added in round 2)
// and fires at every wall-clock reading where all four hold at once, so it is
// the only cadence that can fire more
// than once a day — which is the whole point of it (owner card
// rc-4acc4013a0ae: "every 20 minutes", whose only alternative was 72 separate
// schedules a day).
const (
	ScheduledMessageCadenceDaily   = "daily"
	ScheduledMessageCadenceWeekly  = "weekly"
	ScheduledMessageCadenceMonthly = "monthly"
	ScheduledMessageCadenceCustom  = "custom"
)

// ScheduledMessage mirrors the scheduled_message table: one recurring
// wall-clock slot bound to a member, delivered down the ordinary chat path.
// The clock-driven twin of WebhookEndpoint.
//
// 🔴 LastFiredSlot holds the IDENTIFIER of the slot already delivered
// (slotKey, e.g. `2026-08-10T09:00+08:00`), NOT a clock reading. The tick
// recomputes the most recently elapsed slot and fires only when that slot is
// STRICTLY LATER than the one named here — an ORDERING test over the parsed
// instants (slotIsAfterCursor), never a string inequality. See
// migrations/00050 for why storing the slot, and not a "last run at"
// timestamp, is what makes restart-does-not-resend true by construction, and
// slotIsAfterCursor for why "different from last time" is the wrong question
// (a computation that ever moved backwards would differ, and therefore
// redeliver).
// LastFiredTS is the human-facing companion and takes NO part in the decision.
type ScheduledMessage struct {
	ID       string
	MemberID string
	Label    string
	Body     string
	Cadence  string
	// DayOfWeek is 0=Sunday..6=Saturday (weekly only); DayOfMonth is 1-31
	// (monthly only) and a month lacking the day is skipped, never clamped.
	DayOfWeek  int
	DayOfMonth int
	Hour       int
	Minute     int
	// The FOUR explicit sets `custom` intersects (T-49e7). Months are 1-12,
	// days 1-31, hours 0-23, minutes 0-59, all read in Timezone. Empty for
	// every other cadence, and never empty for `custom` — an empty set is
	// refused at the write (see migrations/00052 for why "all" and "never" must
	// not sit one keystroke apart).
	//
	// CustomMonths (round 2, migrations/00053) is the one set a REQUEST may
	// omit, and omitting it means all twelve — the resolution happens in the
	// handler, where nil and [] are still distinguishable. By the time a row
	// reaches this struct the months are always listed explicitly, so nothing
	// below the API layer ever has to read an absence as a meaning.
	//
	// 🔴 The Go side deals in []int; the COLUMN is a canonical comma-joined
	// string, and canonicalIntSet/parseIntSet are the only translation. The
	// canonical form (sorted ascending, deduplicated, no whitespace) is a
	// STORAGE invariant, not a formatting preference: the PATCH re-aim test
	// compares supplied against stored, and a cockpit that posts the whole
	// form back would otherwise re-aim — swallowing the crossed delivery —
	// merely because the user's checkbox order produced [20,0,40] this time.
	CustomMonths  []int
	CustomDays    []int
	CustomHours   []int
	CustomMinutes []int
	// Timezone is an IANA name. The wall clock is ALWAYS read in this zone —
	// there is deliberately no host-local fallback anywhere in this feature.
	Timezone      string
	Status        string
	LastFiredSlot string
	LastFiredTS   float64
	CreatedTS     float64
}

// The list is EXPLICIT, so it names the read order rather than inheriting the
// table's physical one: migrations/00053 appended custom_months at the end of
// the row (a constant-DEFAULT ADD COLUMN), and it is listed here beside the
// three sets it belongs with. scanScheduledMessage must match THIS order.
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

// canonicalIntSet renders a set for storage: sorted ascending, deduplicated,
// comma-joined, no whitespace ("0,20,40"); the empty set is "".
//
// 🔴 This is the ONLY place a custom_* column value is produced, and it runs on
// every write path, so the invariant holds regardless of what a caller handed
// the struct. parseIntSet(canonicalIntSet(x)) is sortedIntSet(x), and
// canonicalIntSet(parseIntSet(s)) == s for every s this function wrote — the
// round trip is identity on its own output, which is what makes the PATCH
// value comparison mean "same choice" rather than "same bytes as typed".
func canonicalIntSet(vals []int) string {
	sorted := sortedIntSet(vals)
	parts := make([]string, len(sorted))
	for i, v := range sorted {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

// sortedIntSet returns vals sorted ascending with duplicates collapsed, without
// mutating the input.
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

// parseIntSet reads a stored custom_* column back. An entry that is not a
// decimal integer is dropped rather than failing the read: the column is only
// ever written by canonicalIntSet, so anything else got there by a hand-edit,
// and refusing to load the row would take the whole schedule down with it.
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

// GetScheduledMessage returns one schedule by id, or nil when absent.
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

// ListScheduledMessagesByMember returns a member's schedules, oldest→newest.
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

// ListAllEnabledScheduledMessages returns every armed schedule across all
// members — the cadence tick's whole working set. Disabled rows are filtered in
// SQL: a suspended schedule must not even be considered, so the tick cannot
// accidentally advance its cursor.
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

// PutScheduledMessage upserts a schedule row (keyed on the id PK). Create
// passes a fresh id; an edit re-puts the SAME id.
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

// UpdateScheduledMessageSettings writes the OWNER-EDITABLE columns of an
// existing schedule and DELIBERATELY LEAVES last_fired_slot / last_fired_ts
// ALONE — they are not in the SET list at all, which is not the same thing as
// writing them back unchanged.
//
// 🔴 This is the mirror of the warning on MarkScheduledMessageFired, for the
// other side of the same race. An edit is a read-modify-write: the handler reads
// the row, applies the patch, and persists. If the tick delivers a slot in
// between, a whole-row re-put would carry the cursor the handler read BEFORE the
// delivery and roll it back — and the next tick would then send that slot again.
// A duplicate delivery is indistinguishable, in the chat log, from a correct
// one, so nothing would ever say so. Not naming the columns means a concurrent
// advance survives the edit; the edit and the cursor never contend.
//
// Re-aiming is the one case that MUST move the cursor, and it says so out loud
// through AimScheduledMessageCursor.
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

// AimScheduledMessageCursor points the delivery cursor at slot — what an edit
// that MOVED the schedule does so it never fires the slot it crossed.
//
// last_fired_ts is untouched on purpose: it records when a delivery actually
// happened, and re-aiming is not a delivery. (Writing back the value the handler
// read would be the same rollback UpdateScheduledMessageSettings exists to
// avoid.)
func (d *DAL) AimScheduledMessageCursor(id, slot string) error {
	_, err := d.wdb.Exec(
		`UPDATE scheduled_message SET last_fired_slot = ? WHERE id = ?`, slot, id)
	return err
}

// MarkScheduledMessageFired advances ONLY the delivery cursor (and its
// human-facing timestamp) after a slot really went out. Deliberately not a
// PutScheduledMessage of a struct read earlier in the tick: the tick's copy is
// a snapshot, and re-putting it would silently roll back any edit the owner
// made to the schedule while the tick was running.
func (d *DAL) MarkScheduledMessageFired(id, slot string, ts float64) error {
	_, err := d.wdb.Exec(
		`UPDATE scheduled_message SET last_fired_slot = ?, last_fired_ts = ?
		 WHERE id = ?`, slot, ts, id)
	return err
}

// DeleteScheduledMessage permanently removes a schedule (idempotent) — the
// operation `status = disabled` deliberately is NOT.
func (d *DAL) DeleteScheduledMessage(id string) error {
	_, err := d.wdb.Exec(`DELETE FROM scheduled_message WHERE id = ?`, id)
	return err
}

// ── settings ─────────────────────────────────────────────────────────────────

// GetSetting returns one settings value by key, or nil when the key was never
// written (the code-side default then applies — see settings.go for the
// closed key set).
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

// PutSetting upserts one settings value, stamping updated_at.
func (d *DAL) PutSetting(key, value string) error {
	_, err := d.wdb.Exec(`
		INSERT INTO setting (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, nowSecs())
	return err
}

// PutPushSubscription creates or refreshes one browser subscription.
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

// ListPushSubscriptions returns the current delivery targets. The table is
// deliberately owner-scoped by the studio's single-owner invariant.
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

// DeletePushSubscription is intentionally idempotent: browsers commonly
// unregister after a 404/410 delivery receipt and may retry during shutdown.
func (d *DAL) DeletePushSubscription(endpoint string) error {
	_, err := d.wdb.Exec(`DELETE FROM push_subscription WHERE endpoint = ?`, endpoint)
	return err
}

// ── warden command queue (T-66a2 L3: the durable half of the §7 FIFO) ────────

// WardenCommand is ONE pending warden-command frame held across a process
// death. Only the verbs with no compensating re-decision are stored (today:
// `update`) — see migrations/00034_warden_command_queue.sql for why START must
// never land here.
type WardenCommand struct {
	WardenID   string
	Verb       string
	MemberID   string
	Frame      []byte
	EnqueuedTS float64
}

// PutWardenCommand records one pending command. DO NOTHING on conflict — a
// re-enqueue of the same (warden, verb, target) is the SAME order, so it must
// neither duplicate the row nor refresh enqueued_ts (that would let a
// repeatedly-requeued command dodge the expiry sweep forever).
func (d *DAL) PutWardenCommand(c WardenCommand) error {
	_, err := d.wdb.Exec(`
		INSERT INTO warden_command_queue (warden_id, verb, member_id, frame, enqueued_ts)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (warden_id, verb, member_id) DO NOTHING`,
		c.WardenID, c.Verb, c.MemberID, string(c.Frame), c.EnqueuedTS)
	return err
}

// DeleteWardenCommand forgets one pending command (idempotent). Called when the
// frame has been WRITTEN to the warden's socket — which is NOT the same as
// delivered (there is no ack in this band; see hub.go MarkWardenCommandWritten).
func (d *DAL) DeleteWardenCommand(wardenID, verb, memberID string) error {
	_, err := d.wdb.Exec(`
		DELETE FROM warden_command_queue
		WHERE warden_id = ? AND verb = ? AND member_id = ?`,
		wardenID, verb, memberID)
	return err
}

// ListWardenCommands returns every surviving pending command in enqueue order
// — the FIFO the restore path rebuilds from.
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

// DeleteWardenCommandsBefore drops every command enqueued strictly before
// cutoff — the expiry sweep that keeps a never-drainable backlog from living
// forever. Returns how many rows it removed.
func (d *DAL) DeleteWardenCommandsBefore(cutoff float64) (int64, error) {
	res, err := d.wdb.Exec(`DELETE FROM warden_command_queue WHERE enqueued_ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil // the delete succeeded; an unsupported count is not a failure
	}
	return n, nil
}

// DeleteSetting removes one settings value (idempotent — deleting an absent
// key is a no-op). Consumes the one-shot first-run claim token.
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

// SetMemberAgentIatFloor raises this member's credential floor to ts (T-14 項目
// 4B) — the `iat` of the token that just reported waking. It is the SOLE writer
// that moves the column: a whole-row write carries it on INSERT but never onto
// an existing row (mfAgentIatFloor declares it insertOnly).
//
// FORWARD-ONLY, in SQL rather than in Go. A read-modify-write in Go loses to
// whichever caller writes last, and the loser here is a REVOCATION: two
// generations that wake close together would leave the floor at the older one's
// iat and hand the superseded session its credentials back. max() in the
// statement means the floor can only ever rise, whoever wins the race.
//
// ⚠️ What it still cannot do is separate two wakes in the SAME second — `iat`
// is whole seconds and the owner has ruled that out of scope for now
// (2026-08-28: 「先不管搶同一秒的問題好了」). max() makes the write safe; it does not
// make the resolution finer.
//
// A missing row is a clean no-op (0 rows affected, no error).
// 🔑 It goes through PatchMember rather than writing its own max() (T-63). The
// column's monotonicity is declared on mfAgentIatFloor, and while this seam kept
// a max() of its own the same property had TWO representations: both correct
// today, both self-consistent, and wrong only when read together — which nobody
// does. Nothing would have gone red on the day they diverged.
func (d *DAL) SetMemberAgentIatFloor(id string, ts float64) error {
	return d.PatchMember(id, mfAgentIatFloor(ts))
}

// SetMemberTokenKeyID records WHICH signing key this station just verified a
// credential of this member's with (T-80). It is the SOLE writer that moves the
// column: a whole-row write carries it on INSERT but never onto an existing row
// (mfTokenKeyID declares it insertOnly).
//
// NOT forward-only, and it must not become so. The value is an OBSERVATION and
// it legitimately moves in both directions: a machine reinstalled with an older
// credential goes backwards, and that is exactly the fact the owner needs to see
// before pressing remove. There is no ordering on key ids to be monotone about.
//
// A missing row is a clean no-op (0 rows affected, no error).
func (d *DAL) SetMemberTokenKeyID(id, keyID string) error {
	return d.PatchMember(id, mfTokenKeyID(keyID))
}
