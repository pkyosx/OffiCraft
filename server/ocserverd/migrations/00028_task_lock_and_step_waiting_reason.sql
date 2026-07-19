-- +goose Up
-- T-9ca5 「任務狀態全推導」lock base. Two additive columns + one data move.
--
-- task.lock — an ORTHOGONAL dimension to status. Since the owner's "任務狀態全推
-- 導" ruling, task.status is PURELY derived from the steps (DeriveTaskStatus); a
-- lock is a SYSTEM hold layered on top that the derivation never sets nor clears.
-- Today the only lock is 'reassigning' (the handover hold while a new executor
-- reads up), which USED to be a status value. Moving it here lets the cockpit
-- show the honest derived status AND the reassigning lock badge together.
--
-- task_step.waiting_reason — waiting_external moved DOWN to the step level
-- (T-9ca5): the agent parks a step in waiting_external with a reason, and the
-- task's display waiting_reason is derived from it. This retires the task-level
-- waiting_reason as the write target (that column stays as the derived display).
--
-- No CHECK churn: task.status lost its DB-level CHECK in 00011 and task_step.status
-- in 00016 (both are Go-guarded closed sets now), so dropping 'reassigning' from
-- the status vocabulary needs no schema rebuild — only the data move below. Both
-- ADD COLUMNs carry a constant DEFAULT '' (cheap metadata op, not a table rebuild);
-- existing rows read '' for lock / waiting_reason.
ALTER TABLE task ADD COLUMN lock TEXT NOT NULL DEFAULT '';
ALTER TABLE task_step ADD COLUMN waiting_reason TEXT NOT NULL DEFAULT '';

-- Move any task currently held in status='reassigning' onto the new lock
-- dimension. Its derived status can't be computed in SQL (it needs the step
-- fold), so land a safe in_progress here; the server's boot reconcile
-- (RecomputeTaskStatus over every task) corrects it to the exact derived value
-- on first start. A reassigning task always has an executor + non-terminal work,
-- so in_progress is the honest neighbourhood.
UPDATE task SET lock = 'reassigning', status = 'in_progress' WHERE status = 'reassigning';

-- DOWN-PUSH the live task-level waiting_external onto its current step — the
-- ticket's core "等待外部下放到步驟". A task currently in status='waiting_external'
-- carries its reason at the TASK level (the retired model); the derived model
-- reads waiting_external from the STEPS, so without this move the boot reconcile
-- would re-derive these live tasks to in_progress and drop the reason. For each
-- such task, mark its CURRENT step (the first non-terminal step, order_idx→id)
-- waiting_external and copy the reason there. task.status and task.waiting_reason
-- are left UNTOUCHED: the boot reconcile recomputes the display waiting_reason
-- from this same step to the identical value (per-row equal, no data loss).
UPDATE task_step
SET status = 'waiting_external',
    waiting_reason = (SELECT t.waiting_reason FROM task t WHERE t.id = task_step.task_id)
WHERE task_id IN (SELECT id FROM task WHERE status = 'waiting_external')
  AND id = (
    SELECT s2.id FROM task_step s2
    WHERE s2.task_id = task_step.task_id
      AND s2.status NOT IN ('done', 'superseded')
    ORDER BY s2.order_idx, s2.id
    LIMIT 1
  );

-- +goose Down
-- Reverse the down-push: an older binary has no step-level waiting_external, so
-- restore those steps to in_progress (the state a step under a task-level
-- waiting_external held). task.status / task.waiting_reason are the retired
-- model's own fields — untouched by Up, so the old world is intact.
UPDATE task_step SET status = 'in_progress', waiting_reason = '' WHERE status = 'waiting_external';
-- Restore the pre-lock world an older binary expects: reassigning back as a
-- status, columns gone. Any task holding the reassigning lock returns to
-- status='reassigning' (the old binary's handover hold); other locks don't
-- exist yet, so this is exhaustive.
UPDATE task SET status = 'reassigning' WHERE lock = 'reassigning';
ALTER TABLE task_step DROP COLUMN waiting_reason;
ALTER TABLE task DROP COLUMN lock;
