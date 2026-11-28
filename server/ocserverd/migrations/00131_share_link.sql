-- +goose Up
-- Share links that open one attachment or one comparison without a login.
-- Revoking a link deletes its row; expired rows are pruned on a cadence.
CREATE TABLE share_link (
    -- 'sl-' + newHexID(12); the wire id. Not the credential in the URL.
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN ('attachment', 'diff')),
    -- hex SHA-256 of the URL credential; the credential itself is never stored,
    -- so a copy of this database cannot be replayed as a link.
    token_hash    TEXT NOT NULL UNIQUE,
    attachment_id TEXT,
    diff_before   TEXT,
    diff_after    TEXT,
    label_before  TEXT,
    label_after   TEXT,
    created_by    TEXT NOT NULL,
    created_ts    REAL NOT NULL DEFAULT 0.0,
    -- NULL: never expires, stays valid until revoked.
    expires_ts    REAL
);
CREATE INDEX share_link_attachment ON share_link(attachment_id);
CREATE INDEX share_link_diff ON share_link(diff_before, diff_after);

-- +goose Down
DROP TABLE IF EXISTS share_link;
