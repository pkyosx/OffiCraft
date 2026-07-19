-- +goose Up
-- A案 P7d+P7e — fold the outsource_worker table INTO member, one shot
-- (一步到位：搬資料 + DROP 舊表在同一支 migration；owner rc-69dd122e9c73 拍板
-- 推翻兩段式。資料回復不走 Down — 靠部署前的 DB 備份).
--
-- Design constitution: 外包＝正職 — the only difference is an outsource member
-- is minted/released alongside its task. 00017–00021 already aligned every
-- shared worker column onto the member vocabulary and 00024 widened the kind
-- CHECK, so this step is: four ADDITIVE member columns + a full-table
-- INSERT…SELECT + the codename-uniqueness assert + DROP TABLE.
--
-- Column map (outsource_worker → member):
--   id                → id            (ow- ids kept verbatim; the PK collides
--                                      with nothing — member ids are m-/mira/
--                                      m-server-self, so a collision would be
--                                      corruption and rightly fails the insert)
--   codename          → codename      (NEW; also mirrored into name for display)
--   model/effort      → model/effort
--   task_id           → linked_task_id (00024's prepared binding)
--   status            → roster_status + activated_ts (see below)
--   created_ts        → created_ts    (NEW — member had no birth stamp; the
--                                      worker stuck-projection and list order
--                                      need it durable)
--   released_ts       → released_ts   (NEW)
--   desired_state / desired_machine_id / refocus_since / banked_cost /
--   last_op / last_op_ok / last_op_log / last_op_reason / last_op_at
--                     → same-named member columns, verbatim (00017–00021 twins).
--   spawn_attempts / last_spawn_ts / last_spawn_target
--                     → NOT migrated (棄搬 by spec): outsource spawn
--                       observability moves to the server's in-memory maps,
--                       the member-reconcile posture. A restart forgets the
--                       attempt count / last target — accepted trade-off.
--
-- status mapping: released → roster_status='removed' (the member soft-delete —
-- released rows are KEPT so the codename fold still sees every codename ever
-- issued and never reuses one); assigned/active → roster_status='active'.
-- activated_ts (NEW) preserves the assigned↔active distinction the DTO's
-- frozen status field still serves: 0 = never claimed its task (assigned),
-- >0 = first GET /api/self/task claim happened (active). Migrated active rows
-- get created_ts as the best available approximation (the true claim time was
-- never recorded).
ALTER TABLE member ADD COLUMN codename TEXT;
ALTER TABLE member ADD COLUMN created_ts REAL NOT NULL DEFAULT 0.0;
ALTER TABLE member ADD COLUMN released_ts REAL NOT NULL DEFAULT 0.0;
ALTER TABLE member ADD COLUMN activated_ts REAL NOT NULL DEFAULT 0.0;

INSERT INTO member (id, name, kind, role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason, linked_task_id,
    codename, created_ts, released_ts, activated_ts)
  SELECT id, codename, 'outsource', '', model, effort,
    desired_state, desired_machine_id, 0.0, 0.0,
    0.0, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at,
    CASE WHEN status = 'released' THEN 'removed' ELSE 'active' END,
    last_op_reason, task_id,
    codename, created_ts, released_ts,
    CASE WHEN status = 'active'
         THEN (CASE WHEN created_ts > 0 THEN created_ts ELSE 1.0 END)
         ELSE 0.0 END
  FROM outsource_worker;

-- Codename uniqueness ASSERT: codenames are never reused by contract (§A.4),
-- so a duplicate here is corruption — the UNIQUE index creation then FAILS the
-- whole migration. The partial index (codename IS NOT NULL) leaves the many
-- NULL non-outsource rows out and stays as a durable forward guarantee.
CREATE UNIQUE INDEX idx_member_codename ON member (codename)
  WHERE codename IS NOT NULL;

DROP TABLE outsource_worker;

-- +goose Down
-- HONEST schema-only rollback: the Up moved the worker rows and dropped the
-- source table, so Down can only restore the SHAPE — it recreates an EMPTY
-- outsource_worker table (full post-00021 column set, so the older Downs below
-- it keep working) and removes the four member columns. The kind='outsource'
-- member rows are DELETED (pre-00025 code cannot see them and 00024's Down
-- would otherwise squash them into fake assistants); their data is NOT copied
-- back — data restoration is the DB backup's job, by owner decree
-- (rc-69dd122e9c73: 一步到位、回退靠備份).
DROP INDEX idx_member_codename;
CREATE TABLE outsource_worker (
    id                 TEXT PRIMARY KEY,
    codename           TEXT NOT NULL,
    model              TEXT NOT NULL DEFAULT '',
    effort             TEXT NOT NULL DEFAULT 'medium',
    task_id            TEXT NOT NULL,
    status             TEXT NOT NULL CHECK (status IN ('assigned', 'active', 'released'))
                           DEFAULT 'assigned',
    created_ts         REAL NOT NULL DEFAULT 0.0,
    released_ts        REAL NOT NULL DEFAULT 0.0,
    spawn_attempts     INTEGER NOT NULL DEFAULT 0,
    last_spawn_ts      REAL NOT NULL DEFAULT 0.0,
    last_spawn_target  TEXT NOT NULL DEFAULT '',
    last_op            TEXT NOT NULL DEFAULT '',
    last_op_ok         INTEGER,
    last_op_log        TEXT NOT NULL DEFAULT '',
    last_op_reason     TEXT NOT NULL DEFAULT '',
    last_op_at         REAL NOT NULL DEFAULT 0.0,
    desired_machine_id TEXT NOT NULL DEFAULT '',
    refocus_since      REAL NOT NULL DEFAULT 0.0,
    desired_state      TEXT NOT NULL DEFAULT 'online',
    banked_cost        REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_outsource_worker_task ON outsource_worker (task_id);
DELETE FROM member WHERE kind = 'outsource';
ALTER TABLE member DROP COLUMN activated_ts;
ALTER TABLE member DROP COLUMN released_ts;
ALTER TABLE member DROP COLUMN created_ts;
ALTER TABLE member DROP COLUMN codename;
