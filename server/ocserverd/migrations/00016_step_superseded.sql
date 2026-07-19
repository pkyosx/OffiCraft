-- +goose Up
-- T-1aea: the `superseded` step terminal status — a replan (submit_plan) now
-- FREEZES a step whose latest bound reply card was already answered/expired
-- instead of deleting it (the question-and-answer history must survive the
-- replan), unless the fresh plan re-lists the node by name. One schema move,
-- the 00011/00013 template: DROP the DB-level status CHECK — the closed set
-- (now five states) lives ONLY in code (domain.ValidStepStatus), so a future
-- step state costs zero schema churn. SQLite cannot DROP a CHECK in place,
-- hence the create/copy/swap rebuild. task_step carries no FKs, so the
-- rebuild is a plain create/copy/drop/rename; every other column keeps its
-- 00004 shape and comments.
CREATE TABLE task_step_rebuild (
    id             TEXT PRIMARY KEY,
    task_id        TEXT NOT NULL,
    order_idx      INTEGER NOT NULL DEFAULT 0,
    name           TEXT NOT NULL DEFAULT '',
    -- the Definition of Done: what "finished" means for THIS node.
    dod            TEXT NOT NULL DEFAULT '',
    -- the closed set is enforced in code now (domain.ValidStepStatus); no CHECK.
    status         TEXT NOT NULL DEFAULT 'pending',
    -- parallel group key; '' = a plain sequential node.
    parallel_group TEXT NOT NULL DEFAULT '',
    is_gate        INTEGER NOT NULL DEFAULT 0,
    -- the CURRENTLY armed reply card ('' = none); historical cards reverse-
    -- resolve via reply_card.task_step_id — two directions, one truth each.
    reply_card_id  TEXT NOT NULL DEFAULT '',
    started_ts     REAL NOT NULL DEFAULT 0.0,
    finished_ts    REAL NOT NULL DEFAULT 0.0
);
INSERT INTO task_step_rebuild (id, task_id, order_idx, name, dod, status,
    parallel_group, is_gate, reply_card_id, started_ts, finished_ts)
  SELECT id, task_id, order_idx, name, dod, status,
    parallel_group, is_gate, reply_card_id, started_ts, finished_ts
  FROM task_step;
DROP TABLE task_step;
ALTER TABLE task_step_rebuild RENAME TO task_step;
CREATE INDEX idx_task_step_task ON task_step (task_id);

-- +goose Down
-- Reverse: restore the four-state status CHECK (another rebuild).
-- 'superseded' will not exist after rollback, so map any such rows to 'done'
-- (the closest legacy terminal — the row is finished history either way, and
-- keeping the row beats deleting audit trail) before the CHECK would reject
-- them.
CREATE TABLE task_step_rebuild (
    id             TEXT PRIMARY KEY,
    task_id        TEXT NOT NULL,
    order_idx      INTEGER NOT NULL DEFAULT 0,
    name           TEXT NOT NULL DEFAULT '',
    dod            TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL CHECK (status IN
                       ('pending', 'in_progress', 'waiting_owner', 'done'))
                       DEFAULT 'pending',
    parallel_group TEXT NOT NULL DEFAULT '',
    is_gate        INTEGER NOT NULL DEFAULT 0,
    reply_card_id  TEXT NOT NULL DEFAULT '',
    started_ts     REAL NOT NULL DEFAULT 0.0,
    finished_ts    REAL NOT NULL DEFAULT 0.0
);
INSERT INTO task_step_rebuild (id, task_id, order_idx, name, dod, status,
    parallel_group, is_gate, reply_card_id, started_ts, finished_ts)
  SELECT id, task_id, order_idx, name, dod,
    CASE WHEN status = 'superseded' THEN 'done' ELSE status END,
    parallel_group, is_gate, reply_card_id, started_ts, finished_ts
  FROM task_step;
DROP TABLE task_step;
ALTER TABLE task_step_rebuild RENAME TO task_step;
CREATE INDEX idx_task_step_task ON task_step (task_id);
