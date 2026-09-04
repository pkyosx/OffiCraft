-- +goose Up
-- 🔴 THE NUMBER IS STILL ONLY TRUE AS OF THE LAST SCAN. See the header of
-- 00075_chat_message_recipient_sender_ts.sql: BOTH constraints written there
-- apply to this file too, and they are two, not one -- ① re-scan for a
-- COLLISION across all remote branches, and ② this package LANDS LAST, because
-- its numbers sit above the in-flight 00071 (#400) and 00074 (#407). The pair
-- moved together from 00072/00073 on 2026-09-04.
--
-- ⚠️ THIS FILE HAS NO ORDER DEPENDENCE ON 00075. The line here used to read
-- "This one MUST stay ABOVE 00075", and it was false: 00075 creates an index on
-- `chat_message`, while the DROP TABLE below drops `member`, which cannot touch
-- it. Swapping the two is not a data bug.
-- 🔴 THE COLUMN LIST BELOW IS THE WHOLE RISK. Anything added to `member` by a
-- migration numbered BELOW this one and not named here is dropped for every
-- existing row, silently. That is not hypothetical: #387 landed
-- 00070_member_restart_after_stop.sql while this pack was in flight, and on the
-- renumber all three guards in migration_00076_member_kind_staff_test.go went
-- red naming restart_after_stop before the column was added here.
-- Rename the member kind 'assistant' to 'staff' — the whole vocabulary move, in
-- one migration, on the same package as the T-48 chat work.
--
-- owner ruling 2026-09-02 (rc-324858f69524): 「就在這包做，接受重建成員表」 — he was
-- told the cost (a full member table rebuild) and took it rather than deferring
-- to a ticket of its own.
--
-- WHY A REBUILD AND NOT AN UPDATE. Two things have to change, and only one of
-- them is data. `member.kind` carries a CHECK constraint pinning the legal set
-- to {'assistant','warden','outsource'} (00024). SQLite cannot alter a CHECK in
-- place, so widening/renaming the set means create/copy/drop/rename — the same
-- shape 00024 itself used, and 00013 before it. A bare UPDATE would be rejected
-- by the old CHECK.
--
-- 🔴 THE INDEX IS THE TRAP IN THIS ONE. `member` DOES carry an index today:
--
--     CREATE UNIQUE INDEX idx_member_codename ON member (codename)
--       WHERE codename IS NOT NULL
--
-- `DROP TABLE member` takes it with it, and `RENAME` does NOT bring it back.
-- Miss it and codenames silently become duplicable — nothing raises, nothing
-- logs, and the next collision is a live data bug rather than an error. Both
-- directions below rebuild it explicitly.
--
-- ⚠️ 00024's comment says member "carries no indexes and no FKs". That was true
-- WHEN IT WAS WRITTEN and is false today (the index above arrived later). Read
-- the schema, not the older migration's prose.
--
-- FOREIGN KEYS: none point at member. Verified by querying
-- pragma_foreign_key_list over every table in a migrated database — the only FK
-- in the whole schema is webhook_request_log -> webhook_endpoint, which doubles
-- as the positive control proving the query finds FKs when they exist.
--
-- 🔑 WHAT THIS MIGRATION DOES *NOT* TOUCH, and why that is deliberate:
--   · `task.executor_kind` / `task.reassigned_from_kind` speak a DIFFERENT
--     vocabulary — 'member' / 'outsource', never 'assistant'. Confirmed against
--     live data: executor_kind is {member:918, outsource:307}, and
--     reassigned_from_kind is {'':1103, member:98, outsource:24}. They are not
--     the member.kind axis and must not be swept up by a global rename.
--   · `role_key` values that happen to read "assistant" are the ROLE axis, not
--     the kind axis (see 00061/00062, which discuss `assistant::general`
--     document keys). Renaming those would break authorization, which keys off
--     role_key — not off kind.
--   member is the ONLY table in the schema whose CHECK mentions 'assistant'.
CREATE TABLE member_rebuild (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL DEFAULT '',
    kind                 TEXT NOT NULL CHECK (kind IN ('staff', 'warden', 'outsource')),
    role_key             TEXT NOT NULL DEFAULT '',
    model                TEXT NOT NULL DEFAULT '',
    effort               TEXT NOT NULL DEFAULT 'medium',
    desired_state        TEXT NOT NULL DEFAULT 'offline',
    desired_machine_id   TEXT NOT NULL DEFAULT 'm-server-self',
    waking_since         REAL NOT NULL DEFAULT 0.0,
    stopping_since       REAL NOT NULL DEFAULT 0.0,
    stopped_since        REAL NOT NULL DEFAULT 0.0,
    refocus_since        REAL NOT NULL DEFAULT 0.0,
    banked_cost          REAL NOT NULL DEFAULT 0.0,
    last_op              TEXT NOT NULL DEFAULT '',
    last_op_ok           INTEGER,
    last_op_log          TEXT NOT NULL DEFAULT '',
    last_op_at           REAL NOT NULL DEFAULT 0.0,
    roster_status        TEXT NOT NULL DEFAULT 'active',
    last_op_reason       TEXT NOT NULL DEFAULT '',
    linked_task_id       TEXT,
    codename             TEXT,
    created_ts           REAL NOT NULL DEFAULT 0.0,
    released_ts          REAL NOT NULL DEFAULT 0.0,
    activated_ts         REAL NOT NULL DEFAULT 0.0,
    runtime              TEXT NOT NULL DEFAULT 'claude',
    last_machine_id      TEXT NOT NULL DEFAULT '',
    avatar_attachment_id TEXT NOT NULL DEFAULT '',
    actual_model         TEXT NOT NULL DEFAULT '',
    actual_runtime       TEXT NOT NULL DEFAULT '',
    actual_effort        TEXT NOT NULL DEFAULT '',
    refocus_op           TEXT NOT NULL DEFAULT '',
    session_boot_ts      REAL NOT NULL DEFAULT 0,
    forced_stop_at       REAL NOT NULL DEFAULT 0,
    handover_noticed_ts  REAL NOT NULL DEFAULT 0,
    agent_iat_floor      REAL NOT NULL DEFAULT 0,
    restart_after_stop   INTEGER NOT NULL DEFAULT 0
);

-- Named column lists on BOTH sides: the trailing columns arrived as later ALTERs
-- and so trail the 00001 columns physically. Naming them makes physical order
-- irrelevant. 'assistant' -> 'staff' happens here, value by value; 'warden' and
-- 'outsource' pass through untouched.
INSERT INTO member_rebuild (id, name, kind, role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason, linked_task_id,
    codename, created_ts, released_ts, activated_ts, runtime, last_machine_id,
    avatar_attachment_id, actual_model, actual_runtime, actual_effort,
    refocus_op, session_boot_ts, forced_stop_at, handover_noticed_ts,
    agent_iat_floor, restart_after_stop)
  SELECT id, name,
    CASE kind WHEN 'assistant' THEN 'staff' ELSE kind END,
    role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason, linked_task_id,
    codename, created_ts, released_ts, activated_ts, runtime, last_machine_id,
    avatar_attachment_id, actual_model, actual_runtime, actual_effort,
    refocus_op, session_boot_ts, forced_stop_at, handover_noticed_ts,
    agent_iat_floor, restart_after_stop
  FROM member;

DROP TABLE member;
ALTER TABLE member_rebuild RENAME TO member;

-- 🔴 Rebuild the index the DROP just destroyed. The WHERE clause is not
-- decoration: it makes the index PARTIAL, so rows with a NULL codename are
-- exempt from the uniqueness rule. Drop the WHERE and every codename-less row
-- collides with every other one.
CREATE UNIQUE INDEX idx_member_codename ON member (codename)
  WHERE codename IS NOT NULL;

-- +goose Down
-- Reverse: restore the {'assistant','warden','outsource'} CHECK and map
-- 'staff' back to 'assistant'.
--
-- This rollback is VALUE-LOSSLESS, and that is worth stating plainly because
-- the neighbouring rebuilds are not. 'staff' and 'assistant' are the same
-- population under two names, so the mapping is 1:1 in both directions and no
-- row becomes unrepresentable — unlike 00024's Down, which had to squash real
-- 'outsource' rows into fake assistants because the older CHECK had no way to
-- express them.
--
-- 🔴 What IS destructible here is the SCHEMA, not the data: this Down also does
-- DROP TABLE, so it must recreate idx_member_codename too. A Down that forgets
-- the index leaves a database that looks fine and enforces nothing.
CREATE TABLE member_rebuild (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL DEFAULT '',
    kind                 TEXT NOT NULL CHECK (kind IN ('assistant', 'warden', 'outsource')),
    role_key             TEXT NOT NULL DEFAULT '',
    model                TEXT NOT NULL DEFAULT '',
    effort               TEXT NOT NULL DEFAULT 'medium',
    desired_state        TEXT NOT NULL DEFAULT 'offline',
    desired_machine_id   TEXT NOT NULL DEFAULT 'm-server-self',
    waking_since         REAL NOT NULL DEFAULT 0.0,
    stopping_since       REAL NOT NULL DEFAULT 0.0,
    stopped_since        REAL NOT NULL DEFAULT 0.0,
    refocus_since        REAL NOT NULL DEFAULT 0.0,
    banked_cost          REAL NOT NULL DEFAULT 0.0,
    last_op              TEXT NOT NULL DEFAULT '',
    last_op_ok           INTEGER,
    last_op_log          TEXT NOT NULL DEFAULT '',
    last_op_at           REAL NOT NULL DEFAULT 0.0,
    roster_status        TEXT NOT NULL DEFAULT 'active',
    last_op_reason       TEXT NOT NULL DEFAULT '',
    linked_task_id       TEXT,
    codename             TEXT,
    created_ts           REAL NOT NULL DEFAULT 0.0,
    released_ts          REAL NOT NULL DEFAULT 0.0,
    activated_ts         REAL NOT NULL DEFAULT 0.0,
    runtime              TEXT NOT NULL DEFAULT 'claude',
    last_machine_id      TEXT NOT NULL DEFAULT '',
    avatar_attachment_id TEXT NOT NULL DEFAULT '',
    actual_model         TEXT NOT NULL DEFAULT '',
    actual_runtime       TEXT NOT NULL DEFAULT '',
    actual_effort        TEXT NOT NULL DEFAULT '',
    refocus_op           TEXT NOT NULL DEFAULT '',
    session_boot_ts      REAL NOT NULL DEFAULT 0,
    forced_stop_at       REAL NOT NULL DEFAULT 0,
    handover_noticed_ts  REAL NOT NULL DEFAULT 0,
    agent_iat_floor      REAL NOT NULL DEFAULT 0,
    restart_after_stop   INTEGER NOT NULL DEFAULT 0
);

INSERT INTO member_rebuild (id, name, kind, role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason, linked_task_id,
    codename, created_ts, released_ts, activated_ts, runtime, last_machine_id,
    avatar_attachment_id, actual_model, actual_runtime, actual_effort,
    refocus_op, session_boot_ts, forced_stop_at, handover_noticed_ts,
    agent_iat_floor, restart_after_stop)
  SELECT id, name,
    CASE kind WHEN 'staff' THEN 'assistant' ELSE kind END,
    role_key, model, effort,
    desired_state, desired_machine_id, waking_since, stopping_since,
    stopped_since, refocus_since, banked_cost, last_op, last_op_ok,
    last_op_log, last_op_at, roster_status, last_op_reason, linked_task_id,
    codename, created_ts, released_ts, activated_ts, runtime, last_machine_id,
    avatar_attachment_id, actual_model, actual_runtime, actual_effort,
    refocus_op, session_boot_ts, forced_stop_at, handover_noticed_ts,
    agent_iat_floor, restart_after_stop
  FROM member;

DROP TABLE member;
ALTER TABLE member_rebuild RENAME TO member;

CREATE UNIQUE INDEX idx_member_codename ON member (codename)
  WHERE codename IS NOT NULL;
