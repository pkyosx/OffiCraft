-- +goose Up
-- T-35e0 order 17 「拆核可閘 → 外包上限自動排隊」. The explicit 發包 target moves
-- ONTO the task row: a create/reassign dispatch no longer mints a worker inline
-- nor parks a pending-approval intent — it lands an UNASSIGNED outsource task
-- (executor_kind='outsource', executor_id='') carrying its own model/effort/
-- machine here, and the existing scheduler (outsource_sched.go) picks it up under
-- the global parallel cap. Three additive columns, each a constant DEFAULT ''
-- (cheap metadata op, not a table rebuild); existing rows read '' (no target →
-- the scheduler falls back to the type manual's assignee spec).
--
-- No CHECK: app-layer guarded exactly like status/lock (00011/00016/00028), so a
-- vocabulary change costs zero schema churn.
ALTER TABLE task ADD COLUMN outsource_model TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN outsource_effort TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN outsource_machine TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task DROP COLUMN outsource_machine;
ALTER TABLE task DROP COLUMN outsource_effort;
ALTER TABLE task DROP COLUMN outsource_model;
