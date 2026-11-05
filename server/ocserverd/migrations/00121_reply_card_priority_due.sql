-- +goose Up
-- Every card stored before these columns reads as mid with no due date, which is
-- what the owner's queue treated them as.
ALTER TABLE reply_card ADD COLUMN priority TEXT NOT NULL DEFAULT 'mid'
	CHECK (priority IN ('high', 'mid', 'low'));
-- Epoch seconds; 0 = no due date.
ALTER TABLE reply_card ADD COLUMN due_ts REAL NOT NULL DEFAULT 0.0;

-- +goose Down
ALTER TABLE reply_card DROP COLUMN due_ts;
ALTER TABLE reply_card DROP COLUMN priority;
