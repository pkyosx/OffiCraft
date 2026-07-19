-- +goose Up
-- T-02c9: the `duplicated` terminal status + the `duplicate_of` pointer so a
-- task's executor can mark it a duplicate of the original and close it, instead
-- of the owner terminating each shell by hand (the T-02b3/T-35af dead-end:
-- whoever finds the duplicate has no terminate power). Two schema moves, one
-- rebuild:
--   1. DROP the DB-level status CHECK (owner-approved design point 4): the
--      seven-state closed set now lives ONLY in code (domain.ValidTaskStatus),
--      so a future terminal state costs zero schema churn. SQLite cannot DROP a
--      CHECK in place, hence the create/copy/swap rebuild;
--   2. ADD duplicate_of — the ORIGINAL task's id, '' unless status='duplicated'.
-- priority/executor_kind keep their CHECKs (only the STATUS whitelist moved to
-- code). task carries no FKs, so the rebuild is a plain create/copy/drop/rename.
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
    executor_kind  TEXT NOT NULL CHECK (executor_kind IN ('member', 'outsource'))
                       DEFAULT 'member',
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
    -- an original) so the cockpit "重複於 T-xxxx" link resolves in one hop.
    duplicate_of   TEXT NOT NULL DEFAULT ''
);
INSERT INTO task_rebuild (id, type_key, title, dedupe_key, inputs, description,
    status, priority, executor_kind, executor_id, waiting_reason, created_ts,
    updated_ts, closed_ts, closeout_ts, creator_id)
  SELECT id, type_key, title, dedupe_key, inputs, description,
    status, priority, executor_kind, executor_id, waiting_reason, created_ts,
    updated_ts, closed_ts, closeout_ts, creator_id FROM task;
DROP TABLE task;
ALTER TABLE task_rebuild RENAME TO task;
CREATE INDEX idx_task_status ON task (status);
CREATE INDEX idx_task_dedupe ON task (type_key, dedupe_key);

-- +goose Down
-- Reverse: drop duplicate_of and restore the status CHECK (another rebuild).
-- 'duplicated' will not exist after rollback, so squash any such rows to
-- 'terminated' (the closest legacy terminal) before the CHECK would reject them.
CREATE TABLE task_rebuild (
    id             TEXT PRIMARY KEY,
    type_key       TEXT NOT NULL DEFAULT '',
    title          TEXT NOT NULL DEFAULT '',
    dedupe_key     TEXT NOT NULL DEFAULT '',
    inputs         TEXT NOT NULL DEFAULT '{}',
    description    TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL CHECK (status IN
                       ('not_started', 'in_progress', 'waiting_owner',
                        'waiting_external', 'done', 'terminated'))
                       DEFAULT 'not_started',
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
    creator_id     TEXT NOT NULL DEFAULT ''
);
INSERT INTO task_rebuild (id, type_key, title, dedupe_key, inputs, description,
    status, priority, executor_kind, executor_id, waiting_reason, created_ts,
    updated_ts, closed_ts, closeout_ts, creator_id)
  SELECT id, type_key, title, dedupe_key, inputs, description,
    CASE WHEN status = 'duplicated' THEN 'terminated' ELSE status END,
    priority, executor_kind, executor_id, waiting_reason, created_ts,
    updated_ts, closed_ts, closeout_ts, creator_id FROM task;
DROP TABLE task;
ALTER TABLE task_rebuild RENAME TO task;
CREATE INDEX idx_task_status ON task (status);
CREATE INDEX idx_task_dedupe ON task (type_key, dedupe_key);
