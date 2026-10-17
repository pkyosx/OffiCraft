-- +goose Up
-- '' = an ordinary card; otherwise the author's suggested name for the value.
-- The value an owner answers with never lands in reply_card: answer_text stays
-- as typed and the ciphertext lives in secret_one_time until retrieved or kept.
ALTER TABLE reply_card ADD COLUMN secret_name TEXT NOT NULL DEFAULT '';
-- 'none' until a value is given; then 'pending' -> 'consumed' | 'kept' | 'expired'.
ALTER TABLE reply_card ADD COLUMN secret_state TEXT NOT NULL DEFAULT 'none'
    CHECK (secret_state IN ('none', 'pending', 'consumed', 'kept', 'expired'));
-- Whether the latest answer carried a value (ReplyCardAnswerDTO.secret_provided).
ALTER TABLE reply_card ADD COLUMN answer_secret_provided INTEGER NOT NULL DEFAULT 0;

-- One row per card with a pending one-time value; the row is DELETED on its
-- retrieval, on a keep (which re-seals it into secret under the new name's AAD)
-- or at expires_ts, so the ciphertext does not outlive its use.
-- reply_card.secret_state keeps the outcome.
CREATE TABLE secret_one_time (
    card_id     TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    -- the card's author: the only member who may retrieve or keep it.
    member_id   TEXT NOT NULL,
    nonce       BLOB NOT NULL,
    -- AES-256-GCM under the same key file as secret.value_ct; AAD is card_id.
    value_ct    BLOB NOT NULL,
    key_version INTEGER NOT NULL DEFAULT 1,
    created_ts  REAL NOT NULL DEFAULT 0.0,
    expires_ts  REAL NOT NULL
);
CREATE INDEX idx_secret_one_time_member ON secret_one_time (member_id);
CREATE INDEX idx_secret_one_time_expires ON secret_one_time (expires_ts);

-- +goose Down
DROP INDEX IF EXISTS idx_secret_one_time_expires;
DROP INDEX IF EXISTS idx_secret_one_time_member;
DROP TABLE IF EXISTS secret_one_time;
ALTER TABLE reply_card DROP COLUMN answer_secret_provided;
ALTER TABLE reply_card DROP COLUMN secret_state;
ALTER TABLE reply_card DROP COLUMN secret_name;
