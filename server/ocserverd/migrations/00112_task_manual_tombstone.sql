-- +goose Up
-- Built-in task manuals ship as seeds and are folded in at read time, the
-- role_def / role_insight shape: a row over a built-in type_key is an edit
-- overlaying the seed, and tombstoned = 1 means "follow the seed". A reset
-- writes the tombstone instead of deleting the row, so it never takes the
-- manual's SOP document_history with it (DeleteTaskManual does).
-- Existing rows are all station-created manuals: they default to 0 and read
-- exactly as before.
ALTER TABLE task_manual ADD COLUMN tombstoned INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE task_manual DROP COLUMN tombstoned;
