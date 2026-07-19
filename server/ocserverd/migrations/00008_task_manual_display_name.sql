-- +goose Up
-- display_name: the owner/agent-editable label for a task type. type_key stays
-- the immutable internal id; display_name is the mutable presentation face
-- (parallels the purpose content field). '' = no label set — the FE then falls
-- back to rendering type_key.
ALTER TABLE task_manual ADD COLUMN display_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task_manual DROP COLUMN display_name;
