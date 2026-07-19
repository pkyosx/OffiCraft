package main

// store.go — SQLite persistence (metadata) + on-disk blob store (binaries).
//
// Layout under [storage].data_dir:
//
//	updater.db      — SQLite metadata (credentials + releases)
//	blobs/<sha256>  — the published binaries, content-addressed by their hash
//
// SQLite posture mirrors server/ocserverd/migrate.go openSQLite: the cgo-free
// modernc driver, ONE pooled connection (single-writer store; a second pooled
// conn only manufactures SQLITE_BUSY between our own requests) plus a busy
// timeout as the belt for external openers (the management subcommands open
// the same file).
//
// Schema is applied idempotently at open (CREATE TABLE IF NOT EXISTS) rather
// than through goose: two tables in an M1 skeleton whose data model is still
// awaiting three owner design cards — a migration chain would freeze exactly
// the shape we expect to adjust. Revisit once the design cards are answered.

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the cgo-free "sqlite" driver
)

const schemaSQL = `
-- credential — publish tokens AND invite codes in one generic table (kind
-- discriminates). Generic ON PURPOSE: the invite-code semantics card is still
-- open, so the M1 reading ("per-person, long-lived, revocable") must be
-- adjustable without a reshape. Only the sha256 hash of a token is stored —
-- the plaintext is printed once at mint and never persisted.
-- The last_* columns are the downstream-fleet monitoring record (one invite ==
-- one downstream OC, owner ruling): GET /api/latest stamps WHEN that invite
-- last checked and WHAT version/sha it self-reported (see RecordInviteCheck).
CREATE TABLE IF NOT EXISTS credential (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    kind          TEXT    NOT NULL CHECK (kind IN ('publish', 'invite')),
    name          TEXT    NOT NULL DEFAULT '',
    secret_hash   TEXT    NOT NULL UNIQUE,
    created_at    REAL    NOT NULL,
    revoked_at    REAL,
    last_check_at REAL,
    last_version  TEXT    NOT NULL DEFAULT '',
    last_sha      TEXT    NOT NULL DEFAULT '',
    last_channel  TEXT    NOT NULL DEFAULT ''
);

-- release — one row per published version. version is stored as an opaque
-- unique string; its SHAPE (vYYMMDD-NNNN, server-generated — see version.go)
-- is enforced at the publish handler, not by the schema, so the column never
-- needs a reshape if the format evolves again.
--
-- ga_at is the dual-channel stamp (owner decided 2026-07-14): every publish
-- lands in the BETA channel (ga_at NULL); POST /api/promote stamps ga_at and
-- the row becomes the GA channel's candidate. One column instead of a channel
-- table: exactly two fixed channels exist, and a stamp keeps the release row
-- immutable in every other respect (no demote — forward-fix by promoting a
-- newer version, same posture as "published versions are immutable").
-- serial is the pure monotonic release serial (r-N, T-e9d1): minted at publish
-- as MAX(serial)+1, UNIQUE so a raced double-allocation fails the insert (the
-- handler retries). NULLable in the raw schema ONLY so a pre-serial DB's rows
-- survive the ADD COLUMN and get back-filled at open (backfillSerials); every
-- INSERT this daemon does sets it. It coexists with version — version stays the
-- immutable download key, serial is the human-facing "how new" glance.
CREATE TABLE IF NOT EXISTS release (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    version      TEXT    NOT NULL UNIQUE,
    serial       INTEGER UNIQUE,
    git_sha      TEXT    NOT NULL DEFAULT '',
    sha256       TEXT    NOT NULL,
    size         INTEGER NOT NULL,
    notes        TEXT    NOT NULL DEFAULT '',
    blob_path    TEXT    NOT NULL,
    published_at REAL    NOT NULL,
    ga_at        REAL
);

-- setting — generic key/value (mirrors ocserverd's settings posture). Holds
-- the portal's argon2id password hash + the one-shot first-run claim token;
-- values that are secrets are ALWAYS stored hashed or consumed on use, never
-- echoed by any read path.
CREATE TABLE IF NOT EXISTS setting (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- portal_session — one row per live portal login. Only the sha256 hash of the
-- session token is stored (same posture as credential.secret_hash); expires_at
-- bounds the session, expired rows are ignored by lookup and purged lazily.
CREATE TABLE IF NOT EXISTS portal_session (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT    NOT NULL UNIQUE,
    created_at REAL    NOT NULL,
    expires_at REAL    NOT NULL
);
`

const (
	kindPublish = "publish"
	kindInvite  = "invite"
)

// The two fixed release channels. Every publish is immediately visible on
// beta; ga shows only promoted releases (ga_at stamped). There is no channel
// table — see the release schema comment.
const (
	channelBeta = "beta"
	channelGA   = "ga"
)

// Store bundles the SQLite handle with the data dir that anchors the blob
// store beside it.
type Store struct {
	db      *sql.DB
	dataDir string
}

// Credential is one publish-token / invite-code row (hash only, never the
// plaintext). RevokedAt nil = live. The Last* fields are the fleet-monitoring
// record for invites (one invite == one downstream OC): LastCheckAt nil =
// never checked; LastVersion/LastSHA are whatever the client self-reported on
// its most recent update check ("" = it reported nothing); LastChannel is the
// channel that check followed.
type Credential struct {
	ID          int64
	Kind        string
	Name        string
	CreatedAt   float64
	RevokedAt   *float64
	LastCheckAt *float64
	LastVersion string
	LastSHA     string
	LastChannel string
}

// Release is one published version's metadata; the bytes live at BlobPath.
// GAAt nil = beta-only; non-nil = promoted to GA at that instant.
type Release struct {
	ID          int64
	Version     string
	Serial      int64 // the r-N serial (0 only on a not-yet-backfilled legacy row)
	GitSHA      string
	SHA256      string
	Size        int64
	Notes       string
	BlobPath    string
	PublishedAt float64
	GAAt        *float64
}

// openStore creates the data dir (+ blobs/) as needed, opens the SQLite file
// and applies the idempotent schema.
func openStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "blobs"), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	dbPath := filepath.Join(dataDir, "updater.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema to %s: %w", dbPath, err)
	}
	for _, alter := range []struct{ table, column, decl string }{
		// The dual-channel stamp (pre-channel DBs lack it).
		{"release", "ga_at", "REAL"},
		// The pure monotonic release serial (r-N, T-e9d1; pre-serial DBs lack it).
		{"release", "serial", "INTEGER"},
		// The fleet-monitoring record (pre-portal DBs lack all four).
		{"credential", "last_check_at", "REAL"},
		{"credential", "last_version", "TEXT NOT NULL DEFAULT ''"},
		{"credential", "last_sha", "TEXT NOT NULL DEFAULT ''"},
		{"credential", "last_channel", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(db, alter.table, alter.column, alter.decl); err != nil {
			db.Close()
			return nil, fmt.Errorf("upgrade schema of %s: %w", dbPath, err)
		}
	}
	s := &Store{db: db, dataDir: dataDir}
	if err := s.backfillSerials(); err != nil {
		db.Close()
		return nil, fmt.Errorf("backfill release serials of %s: %w", dbPath, err)
	}
	return s, nil
}

// backfillSerials assigns r-N serials to any legacy release rows that predate
// the serial column (serial IS NULL) — one-time, idempotent (a fresh DB and an
// already-migrated DB both find nothing to do). The order is publish order
// (published_at, id) so the oldest published build becomes r-1, matching the
// owner default "r-1 起算" (start the count from the very first release rather
// than continuing some external number). Runs inside a transaction so a crash
// mid-backfill leaves the rows untouched rather than half-numbered.
func (s *Store) backfillSerials() error {
	rows, err := s.db.Query(
		`SELECT id FROM release WHERE serial IS NULL ORDER BY published_at, id`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(ids) == 0 {
		return nil
	}
	next, err := s.NextSerial()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE release SET serial = ? WHERE id = ?`, next, id); err != nil {
			return err
		}
		next++
	}
	return tx.Commit()
}

// NextSerial is the next r-N to mint: MAX(serial)+1 over the committed rows (1
// on an empty table). Durable and gap-free by construction — see version.go's
// serial header for why this survives power loss without reuse.
func (s *Store) NextSerial() (int64, error) {
	row := s.db.QueryRow(`SELECT COALESCE(MAX(serial), 0) + 1 FROM release`)
	var next int64
	err := row.Scan(&next)
	return next, err
}

// ensureColumn back-fills one column onto a table created by an older schema
// (CREATE TABLE IF NOT EXISTS never touches an existing table). Idempotent by
// inspection — the schema here is still deliberately outside a goose chain
// (see the file header), so the few ALTERs this daemon has ever needed are
// applied the same way the schema is: at open.
func ensureColumn(db *sql.DB, table, column, decl string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name, typ  string
			notnull    int
			deflt      any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &deflt, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return rows.Err() // already present (fresh schema or already altered)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + decl)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

// blobsDir is where the published binaries live, content-addressed by sha256.
func (s *Store) blobsDir() string { return filepath.Join(s.dataDir, "blobs") }

func nowUnix() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// ── credentials ──────────────────────────────────────────────────────────────

// InsertCredential stores a freshly minted credential's HASH and returns its id.
func (s *Store) InsertCredential(kind, name, secretHash string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO credential (kind, name, secret_hash, created_at) VALUES (?, ?, ?, ?)`,
		kind, name, secretHash, nowUnix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// LiveCredentialByHash resolves a presented token (already hashed by the
// caller) of the required kind. Returns nil when unknown, revoked, or of the
// wrong kind — one indistinguishable "no" (the HTTP layer answers 401 either
// way; a caller must not learn WHICH check failed).
func (s *Store) LiveCredentialByHash(secretHash, kind string) (*Credential, error) {
	row := s.db.QueryRow(
		`SELECT id, kind, name, created_at, revoked_at FROM credential
		 WHERE secret_hash = ? AND kind = ? AND revoked_at IS NULL`,
		secretHash, kind)
	var c Credential
	if err := row.Scan(&c.ID, &c.Kind, &c.Name, &c.CreatedAt, &c.RevokedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// RevokeCredential stamps revoked_at on one live credential OF THE GIVEN KIND.
// ok=false when no live credential of that kind carries that id (unknown,
// already revoked, or of a different kind). The kind is a deliberate gate, not
// a lookup convenience: each management seam names the kind it administers, so
// revoke-publish-token can never take down an invite (nor the reverse) through
// an id typo.
func (s *Store) RevokeCredential(id int64, kind string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE credential SET revoked_at = ? WHERE id = ? AND kind = ? AND revoked_at IS NULL`,
		nowUnix(), id, kind)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RevokeInvite stamps revoked_at on one live invite (the invite face of
// RevokeCredential — same kind gate).
func (s *Store) RevokeInvite(id int64) (bool, error) {
	return s.RevokeCredential(id, kindInvite)
}

// ListCredentials returns every credential of one kind (live and revoked),
// oldest first, with the fleet-monitoring record attached (meaningful for
// invites; always blank for publish tokens, which never hit /api/latest).
func (s *Store) ListCredentials(kind string) ([]Credential, error) {
	rows, err := s.db.Query(
		`SELECT id, kind, name, created_at, revoked_at,
		        last_check_at, last_version, last_sha, last_channel
		 FROM credential WHERE kind = ? ORDER BY id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &c.CreatedAt, &c.RevokedAt,
			&c.LastCheckAt, &c.LastVersion, &c.LastSHA, &c.LastChannel); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListInvites returns every invite (the invite face of ListCredentials).
func (s *Store) ListInvites() ([]Credential, error) {
	return s.ListCredentials(kindInvite)
}

// RecordInviteCheck stamps one invite's fleet-monitoring record: it checked
// for updates NOW, following channel, self-reporting version/sha ("" when the
// client sent nothing — an old client's blank report is honest data too, it
// must overwrite a stale non-blank one rather than preserve it).
func (s *Store) RecordInviteCheck(id int64, version, sha, channel string) error {
	_, err := s.db.Exec(
		`UPDATE credential SET last_check_at = ?, last_version = ?, last_sha = ?, last_channel = ?
		 WHERE id = ?`, nowUnix(), version, sha, channel, id)
	return err
}

// ── settings (portal password hash + first-run claim token) ──────────────────

// GetSetting reads one setting (nil = unset).
func (s *Store) GetSetting(key string) (*string, error) {
	row := s.db.QueryRow(`SELECT value FROM setting WHERE key = ?`, key)
	var v string
	if err := row.Scan(&v); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// PutSetting writes one setting (upsert).
func (s *Store) PutSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO setting (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// DeleteSetting removes one setting (no-op when absent).
func (s *Store) DeleteSetting(key string) error {
	_, err := s.db.Exec(`DELETE FROM setting WHERE key = ?`, key)
	return err
}

// ── portal sessions ──────────────────────────────────────────────────────────

// InsertPortalSession stores a freshly minted session token's HASH with the
// given lifetime. Expired rows are purged here (lazy housekeeping — logins are
// rare, so this is the natural sweep point).
func (s *Store) InsertPortalSession(tokenHash string, ttl time.Duration) error {
	now := nowUnix()
	if _, err := s.db.Exec(
		`DELETE FROM portal_session WHERE expires_at <= ?`, now); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO portal_session (token_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		tokenHash, now, now+ttl.Seconds())
	return err
}

// LivePortalSession reports whether a presented session token (already hashed
// by the caller) is live — known and unexpired. One indistinguishable "no"
// otherwise, same posture as LiveCredentialByHash.
func (s *Store) LivePortalSession(tokenHash string) (bool, error) {
	row := s.db.QueryRow(
		`SELECT 1 FROM portal_session WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, nowUnix())
	var one int
	if err := row.Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ── releases ─────────────────────────────────────────────────────────────────

// InsertRelease records one published version's metadata, minting the r-N
// serial here (NextSerial) unless the caller already fixed one. A serial-UNIQUE
// collision (two publishes that raced on the same MAX+1) is retried with a
// freshly re-derived serial; the version-UNIQUE failure is left for the caller
// (409 for a client-chosen version, re-allocate for a server-generated one).
func (s *Store) InsertRelease(r Release) (Release, error) {
	r.PublishedAt = nowUnix()
	const maxSerialAttempts = 5
	for attempt := 1; ; attempt++ {
		if r.Serial == 0 {
			next, err := s.NextSerial()
			if err != nil {
				return r, err
			}
			r.Serial = next
		}
		res, err := s.db.Exec(
			`INSERT INTO release (version, serial, git_sha, sha256, size, notes, blob_path, published_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.Version, r.Serial, r.GitSHA, r.SHA256, r.Size, r.Notes, r.BlobPath, r.PublishedAt)
		if err == nil {
			r.ID, err = res.LastInsertId()
			return r, err
		}
		if isUniqueSerialErr(err) && attempt < maxSerialAttempts {
			r.Serial = 0 // lost the serial race — re-derive MAX+1 and retry
			continue
		}
		return r, err
	}
}

// MaxDailySerial returns the highest NNNN already published under one day's
// prefix (e.g. "v260713-"), 0 when none. Only rows matching the full
// vYYMMDD-NNNN shape are counted (GLOB pins 4 digits), so a legacy free-string
// row can never poison the CAST.
func (s *Store) MaxDailySerial(prefix string) (int, error) {
	row := s.db.QueryRow(
		`SELECT COALESCE(MAX(CAST(substr(version, ?) AS INTEGER)), 0)
		 FROM release WHERE version GLOB ?`,
		len(prefix)+1, prefix+"[0-9][0-9][0-9][0-9]")
	var maxSerial int
	err := row.Scan(&maxSerial)
	return maxSerial, err
}

// IsUniqueVersionErr reports whether an InsertRelease failure was the
// release.version UNIQUE constraint — the one insert failure a caller can
// meaningfully react to (409 for a client-chosen version, re-allocate for a
// server-generated one).
func IsUniqueVersionErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: release.version")
}

// isUniqueSerialErr reports the release.serial UNIQUE failure — the raced
// double-allocation InsertRelease retries internally (never surfaced to the
// HTTP layer, unlike the version collision).
func isUniqueSerialErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: release.serial")
}

// releaseCols is the shared release projection (order matches scanRelease).
const releaseCols = `id, version, COALESCE(serial, 0), git_sha, sha256, size, notes, blob_path, published_at, ga_at`

// ReleaseByVersion resolves one version (nil when unknown).
func (s *Store) ReleaseByVersion(version string) (*Release, error) {
	return s.scanRelease(s.db.QueryRow(
		`SELECT `+releaseCols+` FROM release WHERE version = ?`, version))
}

// ReleaseBySHA resolves the newest release built from a given git sha (nil when
// none). The T-e9d1 self-lookup seam: a downstream server self-reports its
// running git sha on the update check, and the updater answers "your build is
// r-N" from this. Newest-first (id DESC) so a re-publish of the same sha maps
// to its latest serial. An empty sha never matches a real build → nil.
func (s *Store) ReleaseBySHA(gitSHA string) (*Release, error) {
	if strings.TrimSpace(gitSHA) == "" {
		return nil, nil
	}
	return s.scanRelease(s.db.QueryRow(
		`SELECT `+releaseCols+` FROM release WHERE git_sha = ? ORDER BY id DESC LIMIT 1`, gitSHA))
}

// LatestRelease answers "what is newest" for one channel (nil when the channel
// is empty).
//
//   - beta: the most recently PUBLISHED row — "latest = last publish", not a
//     version-string ordering (for server-generated versions the two orders
//     coincide anyway, and publish order is the honest answer when a publisher
//     back-fills an older dated build). Every release is on beta, promoted or
//     not.
//   - ga: the most recently PROMOTED row (ga_at order, id as the tiebreak).
//     GA order is promote order, deliberately decoupled from publish order —
//     an older build promoted later IS the newest GA.
func (s *Store) LatestRelease(channel string) (*Release, error) {
	if channel == channelGA {
		return s.scanRelease(s.db.QueryRow(
			`SELECT ` + releaseCols + `
			 FROM release WHERE ga_at IS NOT NULL ORDER BY ga_at DESC, id DESC LIMIT 1`))
	}
	return s.scanRelease(s.db.QueryRow(
		`SELECT ` + releaseCols + ` FROM release ORDER BY id DESC LIMIT 1`))
}

// PromoteRelease stamps ga_at on one version (idempotent: an already-GA row is
// left with its ORIGINAL stamp — promote order must not be rewritable). Returns
// the row after the operation, nil when the version is unknown.
func (s *Store) PromoteRelease(version string) (*Release, error) {
	if _, err := s.db.Exec(
		`UPDATE release SET ga_at = ? WHERE version = ? AND ga_at IS NULL`,
		nowUnix(), version); err != nil {
		return nil, err
	}
	return s.ReleaseByVersion(version)
}

// ListReleases returns every published version, newest first.
func (s *Store) ListReleases() ([]Release, error) {
	rows, err := s.db.Query(
		`SELECT ` + releaseCols + ` FROM release ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var r Release
		if err := rows.Scan(&r.ID, &r.Version, &r.Serial, &r.GitSHA, &r.SHA256, &r.Size, &r.Notes, &r.BlobPath, &r.PublishedAt, &r.GAAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) scanRelease(row *sql.Row) (*Release, error) {
	var r Release
	err := row.Scan(&r.ID, &r.Version, &r.Serial, &r.GitSHA, &r.SHA256, &r.Size, &r.Notes, &r.BlobPath, &r.PublishedAt, &r.GAAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
