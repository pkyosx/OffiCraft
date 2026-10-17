-- +goose Up
-- 🔴 value_ct is AES-256-GCM ciphertext under a 32-byte key that lives in a file
-- beside the database, never in it: a copy of this database (including every
-- VACUUM INTO backup) cannot recover a value. Nothing may write a plaintext
-- value into any column, log or chat row of this database.
-- No per-member grants: every agent and admin agent may use every row.
CREATE TABLE secret (
    -- env-var style, ^[A-Z][A-Z0-9_]{0,63}$; the owner-facing address.
    name        TEXT PRIMARY KEY,
    -- 12-byte GCM nonce, fresh on every write of value_ct.
    nonce       BLOB NOT NULL,
    -- ciphertext || 16-byte tag; the AAD is the name, so a row's ciphertext
    -- cannot be swapped onto another name.
    value_ct    BLOB NOT NULL,
    -- which key file version sealed value_ct (1 today); lets a key rotation
    -- re-seal rows one by one.
    key_version INTEGER NOT NULL DEFAULT 1,
    created_ts  REAL NOT NULL DEFAULT 0.0,
    updated_ts  REAL NOT NULL DEFAULT 0.0
);

-- Append-only. Outlives the secret and the reply card it records, so a deleted
-- secret's history stays readable. A refused attempt is a 'denied' row.
CREATE TABLE secret_access_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    action      TEXT NOT NULL
        CHECK (action IN ('reveal', 'one_time', 'keep', 'denied')),
    -- '' only on a denied attempt that named no secret.
    name        TEXT NOT NULL DEFAULT '',
    member_id   TEXT NOT NULL,
    -- the machine_id claim of the caller's token; '' when the token has none.
    machine_id  TEXT NOT NULL DEFAULT '',
    -- the reply card of a one_time or keep (or a denied attempt on a card).
    card_id     TEXT NOT NULL DEFAULT '',
    ts          REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_secret_access_log_ts ON secret_access_log (ts);
CREATE INDEX idx_secret_access_log_name_member ON secret_access_log (name, member_id, ts);

-- +goose Down
DROP INDEX IF EXISTS idx_secret_access_log_name_member;
DROP INDEX IF EXISTS idx_secret_access_log_ts;
DROP TABLE IF EXISTS secret_access_log;
DROP TABLE IF EXISTS secret;
