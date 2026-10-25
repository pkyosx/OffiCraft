-- +goose Up
-- Minutes before due_ts the task counts as due soon; 0 = follow the workspace
-- setting, which every task stored before this column did.
ALTER TABLE task ADD COLUMN due_soon_lead_minutes INTEGER NOT NULL DEFAULT 0;

-- Serves the task count (sqlOpenTaskDueFacts in dal_tasks.go) from the open
-- rows alone. ⚠️ SQLite uses a partial index only when the query's WHERE repeats
-- this one, so a change to sqlTaskTerminalStatuses needs a migration rebuilding
-- this index; until then the count silently goes back to scanning every task.
CREATE INDEX idx_task_open_due ON task (due_ts, due_soon_lead_minutes, status)
	WHERE status NOT IN ('done', 'terminated', 'duplicated');

-- +goose Down
DROP INDEX idx_task_open_due;
ALTER TABLE task DROP COLUMN due_soon_lead_minutes;
