-- +goose Up
-- T-202 步驟三 — the one row this ticket exists to collect.
--
-- WHAT HAPPENED. On 2026-09-12 a member was hired through POST /api/members
-- with kind="outsource" while preparing a verification fixture. That door was
-- only ever meant to create staff; it said so in a COMMENT ("an outsource
-- worker is born through the spawn path, not here") and checked nothing, so
-- the hire succeeded. The row it created is bound to no task, which means the
-- scheduler refuses to start it on every tick (its last_op_reason reads, and
-- this was read off the live station rather than reasoned about:
-- "no_live_task: bound task  is missing or already closed — nothing left to
-- boot this worker for"). It cannot run, and no tool can collect it, so it
-- holds an outsource slot for good. owner 2026-09-13, rc-3989498e0c8f:
-- 「這是我們意外產出的 worker 我們可以直接處理掉這個worker 並且防止可以產生這種
-- worker 就好嗎 不用另外開可以移除的功能 避免又有其他問題產生」— collect THIS
-- row and shut the door; do NOT build a general "remove an outsource worker"
-- feature. The door is shut in the same package (api_members.go).
--
-- 🔴 THE THREE PRECONDITIONS ARE IN THE WHERE CLAUSE, NOT IN A COMMENT — which
-- is the whole lesson of this ticket. All three must hold or not one row moves:
--   * kind = 'outsource'        — a staff or warden row with this id would be
--                                 somebody else's member and must not be touched;
--   * roster_status = 'active'  — already-collected rows are left exactly as
--                                 they are, so re-running changes nothing;
--   * no live task binding      — linked_task_id NULL or ''. A worker that IS
--                                 bound to a task is doing its job; collecting
--                                 it would abandon the task mid-flight.
-- An UPDATE whose WHERE matches nothing is a SUCCESSFUL no-op in SQLite, so a
-- station that never had this row (every fresh install, every CI database) runs
-- this migration and is unchanged. That is the property being relied on, not a
-- happy accident: it is the same shape 00061 and 00105 use.
--
-- 🔴 released_ts IS STAMPED, AND WITH THE SAME CLOCK THE RELEASE PATH USES.
-- roster_status='removed' alone would leave released_ts at 0.0, which reads as
-- "removed but never released" — a state the release path never produces. The
-- row is a soft delete, kept forever, exactly like every released worker: the
-- codename fold reads removed rows so a codename is never reissued (00025).
-- Nothing is DELETEd here.
UPDATE member
   SET roster_status = 'removed',
       released_ts   = CAST(strftime('%s', 'now') AS REAL)
 WHERE id            = 'm-51110698e801'
   AND kind          = 'outsource'
   AND roster_status = 'active'
   AND (linked_task_id IS NULL OR linked_task_id = '');

-- ── 飛在半空中的任務與成員 ────────────────────────────────────────────────────
--
-- Nothing is in flight behind this row: it has never started and cannot start.
-- No task points at it (that is precondition three, checked rather than
-- assumed), no step names it, and it holds no session — its presence has read
-- "stopped" since the moment it was created. The only observable change is that
-- the roster stops listing it and the outsource slot it was holding comes back.
--
-- OLD BINARY + NEW SCHEMA is harmless here, unlike 00105: this migration adds
-- and drops nothing, so every column an older binary reads is still there and
-- still the same type. An older binary simply stops seeing one roster row.
--
-- +goose Down
-- 🔴 THE DOWN IS A DELIBERATE NO-OP, AND THAT IS A CHOICE, NOT AN OMISSION.
-- Reinstating the row would mean putting roster_status back to 'active' — and
-- the state it would go back to is the broken one this ticket exists to end: a
-- worker that can never boot and that no tool can collect. A rollback across
-- this migration is for letting an OLDER BINARY run, and an older binary runs
-- perfectly well without this row. So the Down leaves it collected.
--
-- If the row ever genuinely has to come back, it is one UPDATE by hand against
-- a known id, and the values to restore are in this file's Up. What is NOT
-- recoverable is released_ts's previous value (0.0) — stated here so it is not
-- rediscovered later.
SELECT 1;
