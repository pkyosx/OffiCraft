-- +goose Up
-- T-182: 〈任務可結案〉 is sent to the executor every time the task ARRIVES in
-- `ready_for_done`, and the notice says which arrival this is. A task leaves
-- that state whenever somebody adds a step and comes back when the step is
-- done, so one task can produce several arrivals — and without a number the
-- second notice is byte-identical to the first, which reads as a duplicate
-- delivery rather than as news.
--
-- The count has to be DURABLE and it has to live on the task. Counting sends
-- would need the send site to be the only writer (a restart mid-derivation
-- would lose one), and counting the chat rows already posted would make the
-- notice's own text depend on a message anybody may delete.
--
-- 0 means "never arrived" — including every task written before this column
-- existed, which is honest: those arrived under the old rule, where a finished
-- step set closed the task outright and there was no ready_for_done to arrive
-- in.
--
-- ⚠️ 00103 is max+1 measured across every ref and worktree in this clone on
-- 2026-09-11 (the numbering is sparse — 93, 100, 101, 102 — because branches
-- take numbers and land out of order). If another branch takes it first,
-- re-take max+1 and rename this file: nothing in the schema or the code depends
-- on the number. Do NOT reach BACKWARDS into a gap — a migration numbered below
-- a database's current version trips goose's missing-migration check.
ALTER TABLE task ADD COLUMN ready_for_done_visits INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE task DROP COLUMN ready_for_done_visits;
