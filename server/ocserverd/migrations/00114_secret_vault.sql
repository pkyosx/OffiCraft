-- +goose Up
-- 🔴 value_ct is AES-256-GCM ciphertext under a 32-byte key that lives in
-- <data dir>/vault.key (0600), never in the database: a copy of this database
-- (including every VACUUM INTO backup) cannot recover a value. The key file is
-- deliberately not named officraft.db.*, so nothing that copies the database
-- files by that prefix carries the key along. Nothing may write a plaintext
-- value into any column, log or chat row of this database.
-- Only the owner writes rows; every agent and admin agent may reveal every row.
CREATE TABLE secret (
    -- 'sec-' + newHexID(12), minted on create, never changes.
    id          TEXT PRIMARY KEY,
    -- free-text label; not unique, not the key.
    name        TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    -- 12-byte GCM nonce, fresh on every write of value_ct.
    nonce       BLOB NOT NULL,
    -- ciphertext || 16-byte tag; the AAD is the id, so a row's ciphertext
    -- cannot be swapped onto another row.
    value_ct    BLOB NOT NULL,
    -- which key file version sealed value_ct (1 today); lets a key rotation
    -- re-seal rows one by one.
    key_version INTEGER NOT NULL DEFAULT 1,
    created_ts  REAL NOT NULL DEFAULT 0.0,
    updated_ts  REAL NOT NULL DEFAULT 0.0
);

-- Append-only. Outlives the secret it records, so a deleted secret's history
-- stays readable. A refused attempt is a 'denied' row.
CREATE TABLE secret_access_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    action      TEXT NOT NULL CHECK (action IN ('reveal', 'denied')),
    -- the id as sent; on a denied row it may name no secret, '' when none was sent.
    secret_id   TEXT NOT NULL DEFAULT '',
    member_id   TEXT NOT NULL,
    -- the machine_id claim of the caller's token; '' when the token has none.
    machine_id  TEXT NOT NULL DEFAULT '',
    ts          REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_secret_access_log_ts ON secret_access_log (ts);
CREATE INDEX idx_secret_access_log_secret_member ON secret_access_log (secret_id, member_id, ts);

-- +goose Down
DROP INDEX IF EXISTS idx_secret_access_log_secret_member;
DROP INDEX IF EXISTS idx_secret_access_log_ts;
DROP TABLE IF EXISTS secret_access_log;
DROP TABLE IF EXISTS secret;
