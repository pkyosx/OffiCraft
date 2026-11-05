-- +goose Up
-- Epoch seconds until which the owner has put a waiting card aside; 0 = not
-- snoozed.
ALTER TABLE reply_card ADD COLUMN snoozed_until_ts REAL NOT NULL DEFAULT 0.0;

-- +goose Down
ALTER TABLE reply_card DROP COLUMN snoozed_until_ts;
