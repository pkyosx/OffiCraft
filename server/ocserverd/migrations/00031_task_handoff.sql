-- +goose Up
-- T-74f8 「交棒閘」— the ball must never be dropped at task close.
--
-- Fact base (T-8a1e post-mortem): a task's creator DOES get the close event
-- (publishTask fans to executor AND creator), but an SSE delta is a line that
-- washes away — nothing durable says "there is a ball on your side". And the
-- moment the LAST step is reported done the task derives to done, closed_ts
-- stamps, and submit_plan is a permanent 409: the system deletes the handover
-- path at exactly the instant the executor would want it.
--
-- So the handover INTENT becomes a first-class, persisted field on the task,
-- declared in the SAME request that would close it (api_tasks.go
-- HandleUpdateTaskStepStatus… — the gate runs BEFORE the step write, so a
-- refused close leaves the plan still editable).
--
--   handoff        '' = never declared (the pre-column world, and every task
--                  whose creator IS its executor — the gate never asks those).
--                  Closed set otherwise (domain.go ValidHandoff):
--                    'return_to_creator' — the server minted a durable
--                                          follow-up task ON the creator;
--                    'follow_up'         — a successor task exists and now
--                                          carries a dep on this one;
--                    'none'              — explicitly declared finished, no
--                                          successor (handoff_note carries the
--                                          WHY — the audit trail).
--   handoff_note   the one-line reason/handover text (required for 'none').
--   handoff_task_id the successor task id for 'follow_up' / the minted
--                  follow-up id for 'return_to_creator'; '' for 'none'.
--
-- All three are constant-DEFAULT ADD COLUMNs (cheap metadata op, no table
-- rebuild); existing rows read '' and are therefore un-declared, which is the
-- honest state — the gate only ever fires on a FUTURE close.
ALTER TABLE task ADD COLUMN handoff         TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN handoff_note    TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN handoff_task_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task DROP COLUMN handoff_task_id;
ALTER TABLE task DROP COLUMN handoff_note;
ALTER TABLE task DROP COLUMN handoff;
