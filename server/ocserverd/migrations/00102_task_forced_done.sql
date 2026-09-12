-- +goose Up
-- T-182: a task no longer closes itself. Every step done lands it in
-- `ready_for_done` — open, not terminal — and one of four explicit actions ends
-- it. Three of those (mark_task_done / mark_task_terminated /
-- mark_task_duplicated) need no new column: the status itself and the existing
-- closed_ts say everything there is to say.
--
-- force_task_done is the exception, and these two columns are why. It closes a
-- task OVER its own precondition, so the steps do not agree that the work is
-- finished — a forced close is the one close nobody can reconstruct from the
-- record afterwards. Who forced it and the reason they gave are therefore
-- stored on the task and served on every read (forced_done_by /
-- forced_done_reason), so a `done` task always says whether it got there by
-- itself.
--
-- No CHECK on task.status is touched or needed: migrations/00011 dropped the
-- status CHECK, which is exactly why `ready_for_done` costs no schema change.
--
-- ⚠️ 00102 is main's max+1 measured across every ref and worktree in this
-- clone on 2026-09-11 (the numbering is sparse — 93, 100, 101 — because
-- branches take numbers and land out of order). If another branch takes it
-- first, re-take main's max+1 and rename this file: nothing in the schema or
-- the code depends on the number. Do NOT reach BACKWARDS into a gap — a
-- migration numbered below a database's current version trips goose's
-- missing-migration check.
ALTER TABLE task ADD COLUMN forced_done_by TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN forced_done_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task DROP COLUMN forced_done_reason;
ALTER TABLE task DROP COLUMN forced_done_by;
