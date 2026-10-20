-- +goose Up
-- Owner passkeys (WebAuthn). The table is read on every sign-in, never cached,
-- so a delete or `ocserverd passkey-reset` takes effect on the next request.
CREATE TABLE passkey (
    -- 'pk-' + newHexID(12); the wire id. Not the WebAuthn credential id.
    id                TEXT PRIMARY KEY,
    credential_id     BLOB NOT NULL UNIQUE,
    -- host (no port) the passkey was registered on; sign-in matches it too.
    rp_id             TEXT NOT NULL,
    origin            TEXT NOT NULL,
    -- COSE-encoded.
    public_key        BLOB NOT NULL,
    attestation_type  TEXT NOT NULL DEFAULT 'none',
    -- 16 bytes; all zero for most synced passkeys.
    aaguid            BLOB,
    -- stays 0 for synced passkeys, which disables the regression check.
    sign_count        INTEGER NOT NULL DEFAULT 0,
    -- set when a sign-in presented a counter that went backwards (refused).
    clone_warning_ts  REAL,
    -- JSON array of WebAuthn transport names.
    transports        TEXT NOT NULL DEFAULT '[]',
    attachment        TEXT NOT NULL DEFAULT '',
    -- BE must not change between sign-ins (the verifier refuses it); BS may.
    backup_eligible   INTEGER NOT NULL DEFAULT 0,
    backup_state      INTEGER NOT NULL DEFAULT 0,
    name              TEXT NOT NULL,
    created_ts        REAL NOT NULL DEFAULT 0.0,
    last_used_ts      REAL
);
CREATE INDEX passkey_rp_id ON passkey(rp_id);

-- +goose Down
DROP TABLE IF EXISTS passkey;
