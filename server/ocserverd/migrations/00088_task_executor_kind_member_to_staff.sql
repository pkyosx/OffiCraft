-- +goose Up
-- Rename the TASK EXECUTOR kind 'member' to 'staff' — the whole vocabulary
-- move, in one migration.
--
-- 🔴 THE NUMBER IS ONLY TRUE AS OF THE LAST SCAN. Re-scan for a collision
-- across ALL remote branches before this lands, and scan BOTH sources:
-- `migrations/*.sql` AND the `AddNamedMigrationContext("NNNNN` family (Go
-- migrations). At the time of writing, main's highest .sql was 00086 and the
-- only higher number on any remote branch was 00087 (T-79, in flight), so this
-- took 00088 — but the number is re-taken at merge time, not inherited from
-- this comment.
--
-- owner rulings (both answered, option_idxs=[0], free text empty):
--   rc-5471a679bd22, 2026-09-06 09:54 — 「一次改乾淨：任務面改成 staff。收舊詞的入口
--   同時認新舊兩種拼法…吐出去的一律是 staff。線上成員的落差當代價接受」
--   rc-7574cc804dd6, 2026-09-06 11:27 — 「舊拼法直接擋下來，訊息裡明寫「member 已改名
--   為 staff」（跟上次改這個詞一致，不留別名）。代價：線上成員第一次多一次來回」
-- The second ruling REPLACES the "accept both spellings" half of the first: the
-- ingest seams REJECT the old value outright and say it was renamed. No alias.
--
-- WHY A REBUILD AND NOT AN UPDATE. Two things have to change and only one of
-- them is data. `task.executor_kind` carries a CHECK pinning the legal set to
-- {'member','outsource'} AND a DEFAULT of 'member' (both from 00011). SQLite
-- cannot alter either in place, so renaming the set means
-- create/copy/drop/rename — the same shape 00076 used for `member.kind`.
-- A bare UPDATE would be rejected by the old CHECK.
--
-- 🔴 THE DEFAULT IS THE HALF THAT IS EASY TO MISS. Changing only the CHECK
-- leaves `DEFAULT 'member'`, and every INSERT that omits the column then
-- violates the new CHECK — a hard failure at create_task time, not a silent
-- one, but a total one. Both are changed below.
--
-- 🔴 THE COLUMN LIST BELOW IS THE WHOLE RISK. Anything added to `task` by a
-- migration numbered BELOW this one and not named here is dropped for every
-- existing row, silently. The 33 columns below were not counted by hand and
-- were not derived from reading the migration files: they were read back from
-- `pragma_table_info('task')` on a database produced by running this binary's
-- own `migrate` against an empty throwaway DSN. Counting them from the SQL text
-- gives the wrong answer — a `CREATE TABLE` that appears in both an Up and a
-- Down block is counted twice.
--
-- ⚠️ 00076's header calls out a PARTIAL UNIQUE INDEX as "the trap in this one".
-- `task` is milder: it carries `idx_task_status` and `idx_task_dedupe`, both
-- plain and non-unique (verified against the same migrated database). They are
-- still destroyed by DROP TABLE and are still rebuilt explicitly below —
-- RENAME does not bring them back.
--
-- 🔴 THIS FILE IS THE CORRECTION TO A SENTENCE IN 00076, WHICH CANNOT BE EDITED.
-- 00076's header (the "WHAT THIS MIGRATION DOES *NOT* TOUCH" block) says of
-- these two columns: "They are not the member.kind axis and must not be swept
-- up by a global rename." The first half is FALSE and this migration acts on
-- that: they ARE the same axis under a second spelling — an executor of kind
-- 'member' could only ever be a roster member of kind 'staff', because reassign
-- refuses warden targets ("machines never execute tasks") and outsource targets
-- outright. The second half stays TRUE for the rename 00076 was performing:
-- 'assistant' was never one of these columns' values, so 00076 was right to
-- leave them alone.
-- The correction lives HERE rather than in 00076 because a released migration's
-- bytes are frozen: goose records one row per version and never revisits it, so
-- an edit reaches new installs only and silently gives two stations different
-- schemas. `migration.lock` enforces that — editing 00076, even by one comment
-- character, changes its content hash and TestMigrationLockGrowsOnlyAtItsTail
-- refuses the branch (measured, not assumed: the guard was run and it named the
-- line). A reader who follows 00076's claim arrives here.
--
-- SECOND COLUMN, SAME VOCABULARY, NO CHECK TO STOP IT. `reassigned_from_kind`
-- (00023) speaks the SAME closed set as executor_kind but has NO CHECK and
-- admits '' (never reassigned). The database will not refuse a stale 'member'
-- there, so the data move below is the only thing that renames it. Leaving it
-- behind would keep exactly the two-vocabulary split this ticket exists to end.
CREATE TABLE task_rebuild (
    id             TEXT PRIMARY KEY,
    type_key       TEXT NOT NULL DEFAULT '',
    title          TEXT NOT NULL DEFAULT '',
    dedupe_key     TEXT NOT NULL DEFAULT '',
    inputs         TEXT NOT NULL DEFAULT '{}',
    description    TEXT NOT NULL DEFAULT '',
    -- the closed set is enforced in code now (domain.ValidTaskStatus); no CHECK.
    status         TEXT NOT NULL DEFAULT 'not_started',
    priority       TEXT NOT NULL CHECK (priority IN
                       ('high', 'mid', 'low', 'frozen'))
                       DEFAULT 'mid',
    executor_kind  TEXT NOT NULL CHECK (executor_kind IN ('staff', 'outsource'))
                       DEFAULT 'staff',
    executor_id    TEXT NOT NULL DEFAULT '',
    waiting_reason TEXT NOT NULL DEFAULT '',
    created_ts     REAL NOT NULL DEFAULT 0.0,
    updated_ts     REAL NOT NULL DEFAULT 0.0,
    closed_ts      REAL NOT NULL DEFAULT 0.0,
    closeout_ts    REAL NOT NULL DEFAULT 0.0,
    creator_id     TEXT NOT NULL DEFAULT '',
    -- the ORIGINAL task's id this one duplicates; '' unless status='duplicated'.
    -- Depth-1 by construction (the mark_duplicate handler rejects pointing at a
    -- task that is itself duplicated, and rejects marking a task already used as
    -- an original) so the cockpit "重複於 <task id>" link resolves in one hop.
    duplicate_of   TEXT NOT NULL DEFAULT '',
    reassigned_from TEXT NOT NULL DEFAULT '',
    reassigned_from_kind TEXT NOT NULL DEFAULT '',
    lock TEXT NOT NULL DEFAULT '',
    outsource_model TEXT NOT NULL DEFAULT '',
    outsource_effort TEXT NOT NULL DEFAULT '',
    outsource_machine TEXT NOT NULL DEFAULT '',
    handoff         TEXT NOT NULL DEFAULT '',
    handoff_note    TEXT NOT NULL DEFAULT '',
    handoff_task_id TEXT NOT NULL DEFAULT '',
    outsource_runtime TEXT NOT NULL DEFAULT 'claude',
    outsource_dispatched INTEGER NOT NULL DEFAULT 0,
    frozen_by TEXT NOT NULL DEFAULT '',
    handover_note    TEXT NOT NULL DEFAULT '',
    handover_note_ts REAL NOT NULL DEFAULT 0,
    handover_note_by TEXT NOT NULL DEFAULT '',
    kickoff_notified_to TEXT NOT NULL DEFAULT ''
);

-- Named column lists on BOTH sides: the trailing columns arrived as later
-- ALTERs and so trail the 00004/00011 columns physically. Naming them makes
-- physical order irrelevant. 'member' -> 'staff' happens here, value by value,
-- in BOTH kind columns; 'outsource' and '' pass through untouched.
INSERT INTO task_rebuild (id, type_key, title, dedupe_key, inputs, description,
    status, priority, executor_kind, executor_id, waiting_reason, created_ts,
    updated_ts, closed_ts, closeout_ts, creator_id, duplicate_of,
    reassigned_from, reassigned_from_kind, lock, outsource_model,
    outsource_effort, outsource_machine, handoff, handoff_note, handoff_task_id,
    outsource_runtime, outsource_dispatched, frozen_by, handover_note,
    handover_note_ts, handover_note_by, kickoff_notified_to)
  SELECT id, type_key, title, dedupe_key, inputs, description,
    status, priority,
    CASE executor_kind WHEN 'member' THEN 'staff' ELSE executor_kind END,
    executor_id, waiting_reason, created_ts,
    updated_ts, closed_ts, closeout_ts, creator_id, duplicate_of,
    reassigned_from,
    CASE reassigned_from_kind WHEN 'member' THEN 'staff' ELSE reassigned_from_kind END,
    lock, outsource_model,
    outsource_effort, outsource_machine, handoff, handoff_note, handoff_task_id,
    outsource_runtime, outsource_dispatched, frozen_by, handover_note,
    handover_note_ts, handover_note_by, kickoff_notified_to
  FROM task;

DROP TABLE task;
ALTER TABLE task_rebuild RENAME TO task;

-- 🔴 Rebuild the two indexes the DROP just destroyed. Neither is unique and
-- neither is partial, so unlike 00076's there is no WHERE clause to lose — but
-- losing the indexes themselves turns every status filter and every dedupe
-- probe into a table scan, and nothing raises when that happens.
CREATE INDEX idx_task_status ON task (status);
CREATE INDEX idx_task_dedupe ON task (type_key, dedupe_key);

-- 🔴 THIRD PLACE THE SAME VOCABULARY IS STORED, AND IT IS NOT A COLUMN.
-- `task_manual.assignee` is a JSON blob whose shape is
-- {"kind":"member","member_id":…} (see 00001_schema.sql's comment on the
-- column). It is the SAME closed set — create_task reads that `kind` and
-- compares it against the very constant this migration renames.
--
-- MISSING THIS IS SILENT AND IT BREAKS REAL TYPES. After the rename, a stored
-- 'member' matches NEITHER arm of create_task's switch, so the manual's
-- assignee stops binding an executor and every create of that type answers
-- 400 "executor_member_id is required for a type with no manual assignee" —
-- a type that worked yesterday simply stops accepting tasks, and no CHECK,
-- no test and no error names the assignee as the cause. Verified against the
-- live station rather than assumed: get_task_manual(tm-05f7c776d6ff) answers
-- assignee {"kind":"member","member_id":"m-f663f3c5de9a"} today.
--
-- json_set is used rather than a blob rewrite so every OTHER key in the object
-- (member_id, and any unknown key the validator stores verbatim) is preserved
-- byte-for-byte, and the WHERE clause keeps rows that are not on this axis —
-- an outsource assignee, an unset '{}' — completely untouched.
UPDATE task_manual
   SET assignee = json_set(assignee, '$.kind', 'staff')
 WHERE json_valid(assignee)
   AND json_extract(assignee, '$.kind') = 'member';

-- +goose Down
-- Reverse: restore the {'member','outsource'} CHECK and the 'member' DEFAULT,
-- and map 'staff' back to 'member' in both kind columns.
--
-- This rollback is VALUE-LOSSLESS. 'staff' and 'member' are the same population
-- under two names on this axis, so the mapping is 1:1 in both directions and no
-- row becomes unrepresentable.
--
-- 🔴 What IS destructible here is the SCHEMA, not the data: this Down also does
-- DROP TABLE, so it must recreate both indexes too. A Down that forgets them
-- leaves a database that looks fine and silently scans.
CREATE TABLE task_rebuild (
    id             TEXT PRIMARY KEY,
    type_key       TEXT NOT NULL DEFAULT '',
    title          TEXT NOT NULL DEFAULT '',
    dedupe_key     TEXT NOT NULL DEFAULT '',
    inputs         TEXT NOT NULL DEFAULT '{}',
    description    TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'not_started',
    priority       TEXT NOT NULL CHECK (priority IN
                       ('high', 'mid', 'low', 'frozen'))
                       DEFAULT 'mid',
    executor_kind  TEXT NOT NULL CHECK (executor_kind IN ('member', 'outsource'))
                       DEFAULT 'member',
    executor_id    TEXT NOT NULL DEFAULT '',
    waiting_reason TEXT NOT NULL DEFAULT '',
    created_ts     REAL NOT NULL DEFAULT 0.0,
    updated_ts     REAL NOT NULL DEFAULT 0.0,
    closed_ts      REAL NOT NULL DEFAULT 0.0,
    closeout_ts    REAL NOT NULL DEFAULT 0.0,
    creator_id     TEXT NOT NULL DEFAULT '',
    duplicate_of   TEXT NOT NULL DEFAULT '',
    reassigned_from TEXT NOT NULL DEFAULT '',
    reassigned_from_kind TEXT NOT NULL DEFAULT '',
    lock TEXT NOT NULL DEFAULT '',
    outsource_model TEXT NOT NULL DEFAULT '',
    outsource_effort TEXT NOT NULL DEFAULT '',
    outsource_machine TEXT NOT NULL DEFAULT '',
    handoff         TEXT NOT NULL DEFAULT '',
    handoff_note    TEXT NOT NULL DEFAULT '',
    handoff_task_id TEXT NOT NULL DEFAULT '',
    outsource_runtime TEXT NOT NULL DEFAULT 'claude',
    outsource_dispatched INTEGER NOT NULL DEFAULT 0,
    frozen_by TEXT NOT NULL DEFAULT '',
    handover_note    TEXT NOT NULL DEFAULT '',
    handover_note_ts REAL NOT NULL DEFAULT 0,
    handover_note_by TEXT NOT NULL DEFAULT '',
    kickoff_notified_to TEXT NOT NULL DEFAULT ''
);

INSERT INTO task_rebuild (id, type_key, title, dedupe_key, inputs, description,
    status, priority, executor_kind, executor_id, waiting_reason, created_ts,
    updated_ts, closed_ts, closeout_ts, creator_id, duplicate_of,
    reassigned_from, reassigned_from_kind, lock, outsource_model,
    outsource_effort, outsource_machine, handoff, handoff_note, handoff_task_id,
    outsource_runtime, outsource_dispatched, frozen_by, handover_note,
    handover_note_ts, handover_note_by, kickoff_notified_to)
  SELECT id, type_key, title, dedupe_key, inputs, description,
    status, priority,
    CASE executor_kind WHEN 'staff' THEN 'member' ELSE executor_kind END,
    executor_id, waiting_reason, created_ts,
    updated_ts, closed_ts, closeout_ts, creator_id, duplicate_of,
    reassigned_from,
    CASE reassigned_from_kind WHEN 'staff' THEN 'member' ELSE reassigned_from_kind END,
    lock, outsource_model,
    outsource_effort, outsource_machine, handoff, handoff_note, handoff_task_id,
    outsource_runtime, outsource_dispatched, frozen_by, handover_note,
    handover_note_ts, handover_note_by, kickoff_notified_to
  FROM task;

DROP TABLE task;
ALTER TABLE task_rebuild RENAME TO task;

CREATE INDEX idx_task_status ON task (status);
CREATE INDEX idx_task_dedupe ON task (type_key, dedupe_key);

-- Mirror of the Up's third move: put the assignee vocabulary back.
UPDATE task_manual
   SET assignee = json_set(assignee, '$.kind', 'member')
 WHERE json_valid(assignee)
   AND json_extract(assignee, '$.kind') = 'staff';
