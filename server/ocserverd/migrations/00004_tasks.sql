-- +goose Up
-- M3 task system — five tables + the reply_card gate linkage. A task is a
-- WORKFLOW (steps with a Definition of Done), not a single action; its state
-- is REPORTED by the executing agent (the server validates transitions, never
-- auto-advances — SPEC 核心名詞: 狀態由 agent 回報、UI 只呈現). Executors are
-- roster members OR anonymous outsource workers (a worker is NOT a member row
-- — an unknown JWT sub classifies as a plain agent, so the worker never
-- pollutes the roster and never enters reconcile).

-- task — one task card. No speculative columns (00003 spirit): the display
-- number (T-XXXX) derives from id, transient projections ("等待指派" /
-- "規劃中") derive from executor_id + step count, elapsed derives from
-- created_ts.
CREATE TABLE task (
    id             TEXT PRIMARY KEY,
    -- task type = task_manual.type_key; '' = ad-hoc (自由代辦, no manual).
    type_key       TEXT NOT NULL DEFAULT '',
    title          TEXT NOT NULL DEFAULT '',
    -- the manual-defined identity-key VALUE (composite keys join in the
    -- manual's field order); '' for ad-hoc. Dedupe compares (type_key,
    -- dedupe_key) against NON-terminal tasks only — a finished periodic task
    -- (sync-jira) can always reopen.
    dedupe_key     TEXT NOT NULL DEFAULT '',
    -- the manual's "需要哪些資訊" field values, free-form JSON object (the
    -- schema lives in the manual, like chat_message.meta).
    inputs         TEXT NOT NULL DEFAULT '{}',
    description    TEXT NOT NULL DEFAULT '',
    -- the six-state machine (SPEC 核心名詞). done/terminated are TERMINAL.
    status         TEXT NOT NULL CHECK (status IN
                       ('not_started', 'in_progress', 'waiting_owner',
                        'waiting_external', 'done', 'terminated'))
                       DEFAULT 'not_started',
    -- frozen is a PRIORITY (pause-pushing), never a status (SPEC §3.3).
    priority       TEXT NOT NULL CHECK (priority IN
                       ('high', 'mid', 'low', 'frozen'))
                       DEFAULT 'mid',
    -- executor track. "unassigned" is NOT a third kind: an outsource task
    -- awaiting the scheduler is executor_kind='outsource' AND executor_id=''.
    executor_kind  TEXT NOT NULL CHECK (executor_kind IN ('member', 'outsource'))
                       DEFAULT 'member',
    -- member.id or outsource_worker.id; free TEXT, no FK (chat sender/
    -- recipient precedent — identity is attribution, not referential).
    executor_id    TEXT NOT NULL DEFAULT '',
    -- the one-line "what are we waiting for"; non-empty ONLY while
    -- status='waiting_external' (set on entry, cleared on exit).
    waiting_reason TEXT NOT NULL DEFAULT '',
    created_ts     REAL NOT NULL DEFAULT 0.0,
    updated_ts     REAL NOT NULL DEFAULT 0.0,
    -- stamped on entering a terminal status; 0.0 = still open.
    closed_ts      REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_task_status ON task (status);
CREATE INDEX idx_task_dedupe ON task (type_key, dedupe_key);

-- task_dep — "blocked by" edges (SPEC §3.5): PURE display markers, never a
-- status change (a blocked task stays in_progress). Multiple deps per task;
-- replaced wholesale by set_task_deps.
CREATE TABLE task_dep (
    task_id    TEXT NOT NULL,
    blocked_by TEXT NOT NULL,
    PRIMARY KEY (task_id, blocked_by)
);

-- task_step — one workflow node on the task's vertical timeline. Each ROW is
-- one leaf for the progress count (a parallel group's items are separate rows
-- sharing parallel_group — flattened counting comes free). The gate
-- 預告/生效 split is a PROJECTION: is_gate=1 ∧ reply_card_id='' = announced
-- (dashed), is_gate=1 ∧ reply_card_id≠'' = armed (a live reply card exists).
-- Steps have no terminal state of their own: terminating the task freezes
-- them as they stand (audit trail).
CREATE TABLE task_step (
    id             TEXT PRIMARY KEY,
    task_id        TEXT NOT NULL,
    order_idx      INTEGER NOT NULL DEFAULT 0,
    name           TEXT NOT NULL DEFAULT '',
    -- the Definition of Done: what "finished" means for THIS node.
    dod            TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL CHECK (status IN
                       ('pending', 'in_progress', 'waiting_owner', 'done'))
                       DEFAULT 'pending',
    -- parallel group key; '' = a plain sequential node.
    parallel_group TEXT NOT NULL DEFAULT '',
    is_gate        INTEGER NOT NULL DEFAULT 0,
    -- the CURRENTLY armed reply card ('' = none); historical cards reverse-
    -- resolve via reply_card.task_step_id — two directions, one truth each.
    reply_card_id  TEXT NOT NULL DEFAULT '',
    started_ts     REAL NOT NULL DEFAULT 0.0,
    finished_ts    REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_task_step_task ON task_step (task_id);

-- outsource_worker — one anonymous, per-task AI worker. Its id IS its JWT sub
-- (never a member row: the authz resolver folds an unknown sub to the plain
-- `agent` class, so a worker gets agent-level access without ever appearing
-- on the roster). Codenames (O-7 / S-12 / H-3 = model prefix + global
-- ascending sequence) are never reused across tasks. Released rows are KEPT
-- (audit trail, like member soft delete).
CREATE TABLE outsource_worker (
    id          TEXT PRIMARY KEY,
    codename    TEXT NOT NULL,
    model       TEXT NOT NULL DEFAULT '',
    effort      TEXT NOT NULL DEFAULT 'medium',
    -- one worker binds ONE task (SPEC §4.1); task terminal state releases it.
    task_id     TEXT NOT NULL,
    -- lifecycle: assigned (scheduler picked it, not yet awake) → active
    -- (claimed its task via GET /api/self/task) → released (task closed).
    status      TEXT NOT NULL CHECK (status IN ('assigned', 'active', 'released'))
                    DEFAULT 'assigned',
    created_ts  REAL NOT NULL DEFAULT 0.0,
    released_ts REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_outsource_worker_task ON outsource_worker (task_id);

-- task_manual — one task type / playbook (設定 › 任務手冊). Ships EMPTY
-- (SPEC §5.1: no seed, no tombstone — pure owner data, the account_alias
-- family). learnings deliberately does NOT reuse the lessons table: lessons
-- shard per role_key, task learnings shard per type — different governance.
CREATE TABLE task_manual (
    type_key   TEXT PRIMARY KEY,
    -- Q1 "這是什麼任務" — how the intake decides a trigger belongs here.
    purpose    TEXT NOT NULL DEFAULT '',
    -- Q2 input fields: JSON array [{name, required, is_key}]; is_key marks
    -- the (possibly composite) dedupe identity key.
    fields     TEXT NOT NULL DEFAULT '[]',
    -- Q3 the SOP markdown the AI plans workflow steps from.
    sop_md     TEXT NOT NULL DEFAULT '',
    -- accumulated cross-task learnings (agent write-back on task close +
    -- owner hand edits) — whole-doc replace semantics.
    learnings  TEXT NOT NULL DEFAULT '',
    -- the type's executor setting (owner ruling ①): JSON
    -- {"kind":"member","member_id":…} or
    -- {"kind":"outsource","model":…,"effort":…,"copies":N}; {} = unset
    -- (create_task must then carry an explicit executor).
    assignee   TEXT NOT NULL DEFAULT '{}',
    updated_ts REAL NOT NULL DEFAULT 0.0
);

-- reply_card gains the gate linkage (00003 decree: extension is new columns,
-- not a redesign): task_id/task_step_id are the card's immutable birth marks
-- ('' = a plain chat 請示 with no task). The step side (reply_card_id above)
-- points at the CURRENT card only.
ALTER TABLE reply_card ADD COLUMN task_id      TEXT NOT NULL DEFAULT '';
ALTER TABLE reply_card ADD COLUMN task_step_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE reply_card DROP COLUMN task_step_id;
ALTER TABLE reply_card DROP COLUMN task_id;
DROP TABLE IF EXISTS task_manual;
DROP INDEX IF EXISTS idx_outsource_worker_task;
DROP TABLE IF EXISTS outsource_worker;
DROP INDEX IF EXISTS idx_task_step_task;
DROP TABLE IF EXISTS task_step;
DROP TABLE IF EXISTS task_dep;
DROP INDEX IF EXISTS idx_task_dedupe;
DROP INDEX IF EXISTS idx_task_status;
DROP TABLE IF EXISTS task;
