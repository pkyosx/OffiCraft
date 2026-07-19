-- +goose Up
-- A案 P0 (schema prep only, NO behaviour change): widen the member.kind closed
-- set to admit 'outsource' and add a nullable linked_task_id, so a future step
-- can fold the outsource_worker roster into the one member mechanism. NOTHING
-- writes kind='outsource' yet — this migration only makes the shape legal.
--
-- Two schema moves, one rebuild (the 00013 reply_card precedent):
--   1. WIDEN the kind CHECK to {'assistant','warden','outsource'}. SQLite
--      cannot alter a CHECK in place, hence the create/copy/drop/rename
--      rebuild. Every current column is carried over verbatim — including
--      last_op_reason (added later by 00006 ALTER, so it physically trails the
--      00001 columns; the named INSERT…SELECT makes column order irrelevant).
--   2. ADD linked_task_id TEXT — nullable, default NULL, NO foreign key
--      (free-form attribution TEXT, mirroring task.executor_id / 00023's
--      reassigned_from). NULL = not bound to any task (every existing row).
-- member carries no indexes and no FKs, so the rebuild is a plain
-- create/copy/drop/rename.
CREATE TABLE member_rebuild (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL CHECK (kind IN ('assistant', 'warden', 'outsource')),
    role_key           TEXT NOT NULL DEFAULT '',
    model              TEXT NOT NULL DEFAULT '',
    effort             TEXT NOT NULL DEFAULT 'medium',
    desired_state      TEXT NOT NULL DEFAULT 'offline',
    desired_machine_id TEXT NOT NULL DEFAULT 'm-server-self',
    waking_since       REAL NOT NULL DEFAULT 0.0,
    stopping_since     REAL NOT NULL DEFAULT 0.0,
    stopped_since      REAL NOT NULL DEFAULT 0.0,
    refocus_since      REAL NOT NULL DEFAULT 0.0,
    banked_cost        REAL NOT NULL DEFAULT 0.0,
    last_op            TEXT NOT NULL DEFAULT '',
    last_op_ok         INTEGER,
    last_op_log        TEXT NOT NULL DEFAULT '',
    last_op_at         REAL NOT NULL DEFAULT 0.0,
    roster_status      TEXT NOT NULL DEFAULT 'active',
    last_op_reason     TEXT NOT NULL DEFAULT '',
    -- linked_task_id: the task this (outsource) member is bound to; NULL = none.
    -- Free-form TEXT, no FK (mirrors task.executor_id). Read/written by nothing
    -- yet — the P0 field is pure schema prep.
    linked_task_id     TEXT
);
INSERT INTO member_rebuild (id, name, kind, role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason)
  SELECT id, name, kind, role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason
  FROM member;
DROP TABLE member;
ALTER TABLE member_rebuild RENAME TO member;

-- +goose Down
-- Reverse: drop linked_task_id and restore the two-value kind CHECK (another
-- rebuild). 'outsource' cannot exist after rollback, so squash any such row
-- back to 'assistant' — the honest legacy reading: under the old set an
-- outsource member was not representable, and the generic agent colleague
-- ('assistant') is its closest pre-A案 kind. linked_task_id is dropped (the
-- column did not exist before this migration).
CREATE TABLE member_rebuild (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL CHECK (kind IN ('assistant', 'warden')),
    role_key           TEXT NOT NULL DEFAULT '',
    model              TEXT NOT NULL DEFAULT '',
    effort             TEXT NOT NULL DEFAULT 'medium',
    desired_state      TEXT NOT NULL DEFAULT 'offline',
    desired_machine_id TEXT NOT NULL DEFAULT 'm-server-self',
    waking_since       REAL NOT NULL DEFAULT 0.0,
    stopping_since     REAL NOT NULL DEFAULT 0.0,
    stopped_since      REAL NOT NULL DEFAULT 0.0,
    refocus_since      REAL NOT NULL DEFAULT 0.0,
    banked_cost        REAL NOT NULL DEFAULT 0.0,
    last_op            TEXT NOT NULL DEFAULT '',
    last_op_ok         INTEGER,
    last_op_log        TEXT NOT NULL DEFAULT '',
    last_op_at         REAL NOT NULL DEFAULT 0.0,
    roster_status      TEXT NOT NULL DEFAULT 'active',
    last_op_reason     TEXT NOT NULL DEFAULT ''
);
INSERT INTO member_rebuild (id, name, kind, role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason)
  SELECT id, name,
    CASE WHEN kind = 'outsource' THEN 'assistant' ELSE kind END,
    role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason
  FROM member;
DROP TABLE member;
ALTER TABLE member_rebuild RENAME TO member;
