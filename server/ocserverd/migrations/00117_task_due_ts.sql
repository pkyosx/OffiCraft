-- +goose Up
-- Epoch seconds; 0 = no due date, which every task stored before this column
-- honestly had.
ALTER TABLE task ADD COLUMN due_ts REAL NOT NULL DEFAULT 0.0;

-- +goose Down
ALTER TABLE task DROP COLUMN due_ts;
