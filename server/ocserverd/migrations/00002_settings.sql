-- +goose Up
-- setting — the DB-side server settings store (owner-password-in-db design,
-- B1). Plain key-value: the READER holds the schema — the key space is a
-- closed set defined code-side (settings.go: key, type, default, range), so
-- an absent key means "use the code default" and the table normally carries
-- only the values that were ever written (plus the two always-present auth
-- items: the argon2id password hash and the JWT signing secret — plaintext
-- passwords are never stored).
CREATE TABLE setting (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at REAL NOT NULL DEFAULT 0.0
);

-- +goose Down
DROP TABLE IF EXISTS setting;
