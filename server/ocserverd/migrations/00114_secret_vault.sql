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
    updated_ts  REAL NOT NULL DEFAULT 0.0,
    -- set by a successful reveal only; NULL until the first one.
    last_accessed_ts REAL
);

-- +goose Down
DROP TABLE IF EXISTS secret;
