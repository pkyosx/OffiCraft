-- +goose Up
-- T-e77f 「叫開工」— the de-duplication ledger of the outsource kickoff nudge.
--
-- The hole this column serves (measured on X-87 / t-a6fe65399dea, 2026-08-15):
-- a worker that correctly refused to advance a FROZEN task was never told when
-- the task was unfrozen, and a codex sidecar produces no further turn without
-- an inbound event — so the correct refusal became a permanent stall. The fix
-- posts a chat row on every "not advanceable → advanceable" transition, which
-- immediately raises the opposite risk: repeated priority writes, repeated
-- scheduler ticks and repeated dependency releases would each re-post.
--
--   kickoff_notified_to  '' = this task has NO outstanding kickoff notice
--                        (never sent, or cleared because the task went back to
--                        non-advanceable / changed hands). Otherwise the
--                        executor id the kickoff was last posted to — a second
--                        notice to that SAME executor is suppressed while the
--                        task stays advanceable.
--
-- Cleared (not merely ignored) whenever the task is observed non-advanceable —
-- frozen, blocked, terminal, unassigned — so the NEXT genuine transition back
-- into advanceable sends again. That is the difference between "notify once
-- per transition" and "notify once ever", and only the first one survives a
-- freeze/unfreeze cycle.
--
-- A constant-DEFAULT ADD COLUMN (cheap metadata op, no table rebuild).
ALTER TABLE task ADD COLUMN kickoff_notified_to TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task DROP COLUMN kickoff_notified_to;
