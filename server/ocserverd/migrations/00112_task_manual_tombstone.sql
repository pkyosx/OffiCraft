-- +goose Up
-- A row over a built-in type_key overlays its seed; tombstoned = 1 means
-- "follow the seed". Existing rows default to 0 and read as before.
ALTER TABLE task_manual ADD COLUMN tombstoned INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE task_manual DROP COLUMN tombstoned;
