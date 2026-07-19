-- +goose Up
-- The Go target schema: the retired Python dal/models.py's tables ported to SQLite and
-- reshaped per the ontology blueprint (single-owner decree, card 4019a601):
--   * the `owner` table is GONE — runtime scope was already the DEFAULT_OWNER
--     constant, the table was only a seed ritual;
--   * every `owner_id` column and `{owner}::` scoped string key is GONE —
--     each table keys on its natural, real-world identity;
--   * no per-row schema_version — the goose migration version IS the schema
--     version (DB-level, single source);
--   * ZERO authz columns — RBAC never touches the DB: the principal class is
--     DERIVED from existing fields (kind=='warden' → machine,
--     role_key=='assistant' → admin_agent) by the single resolver over the
--     code-side route table. Do not invent a permission table or is_admin.

-- member — one roster row per AI colleague: an agent OR a warden (kind).
-- Stores INTENT only (docs/design/state-model.md): presence/online and the
-- actual machine are OBSERVED, live in the SSE-connection projection, and are
-- never written here; waking/stopping/stopped_since are the durable anchors
-- the derived presence machine reads.
--
-- machine = warden member: a machine is NOT a first-class entity in this
-- schema. Onboarding a machine = the server minting a member row with
-- kind='warden' whose id IS the machine_id, so identity (JWT sub), the SSE
-- command band, presence (self-attested SSE online), telemetry ingest, and
-- RBAC (machine principal class) all reuse the one member mechanism. The
-- "machines list" is just a filtered view of this roster.
CREATE TABLE member (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL DEFAULT '',
    -- closed set: 'assistant' (an agent colleague) | 'warden' (the machine
    -- executor — see the machine = warden member note above).
    kind               TEXT NOT NULL CHECK (kind IN ('assistant', 'warden')),
    -- launch intent for the NEXT wake: role binding + runtime knobs.
    role_key           TEXT NOT NULL DEFAULT '',
    model              TEXT NOT NULL DEFAULT '',
    effort             TEXT NOT NULL DEFAULT 'medium',
    -- desired-state reconciliation targets (intent: online/offline/uninstall,
    -- and WHICH machine should reconcile this member).
    desired_state      TEXT NOT NULL DEFAULT 'offline',
    desired_machine_id TEXT NOT NULL DEFAULT 'm-server-self',
    -- presence anchors (epoch seconds, 0.0 = none): feed the DERIVED
    -- offline/waking/online/stopping/stopped projection, never presence itself.
    waking_since       REAL NOT NULL DEFAULT 0.0,
    stopping_since     REAL NOT NULL DEFAULT 0.0,
    stopped_since      REAL NOT NULL DEFAULT 0.0,
    refocus_since      REAL NOT NULL DEFAULT 0.0,
    -- historical cumulative cost (USD) banked when a session ends — the one
    -- durable telemetry exception (history is intent, live values are not).
    banked_cost        REAL NOT NULL DEFAULT 0.0,
    -- warden op receipts. last_op_ok NULL = "no op reported yet", distinct
    -- from a recorded false (three-valued logic feeds the presence shortcut).
    last_op            TEXT NOT NULL DEFAULT '',
    last_op_ok         INTEGER,
    last_op_log        TEXT NOT NULL DEFAULT '',
    last_op_at         REAL NOT NULL DEFAULT 0.0,
    -- roster lifecycle 'active' | 'removed' (dismiss is a SOFT delete so the
    -- audit trail survives). Named roster_status — matching the wire — so it
    -- can never be misread as presence (which is derived, never stored).
    roster_status      TEXT NOT NULL DEFAULT 'active'
);

-- chat_message — one message. sender/recipient are the wire's from/to (FROM
-- is an SQL reserved word; the DTO maps). meta is free-form JSON and is the
-- SINGLE source of truth for attachment refs
-- (meta.attachments = [{id, mime, filename}]) — deliberately NO FK edge to
-- chat_attachment: a second belonging-edge would be a second source of truth.
CREATE TABLE chat_message (
    id        TEXT PRIMARY KEY,
    sender    TEXT NOT NULL DEFAULT '',
    recipient TEXT NOT NULL DEFAULT '',
    body      TEXT NOT NULL DEFAULT '',
    ts        REAL NOT NULL DEFAULT 0.0,
    meta      TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_chat_message_ts ON chat_message (ts);

-- chat_attachment — one attachment blob, stored apart from the message so the
-- list / SSE stream stays light. The owning message's meta refs are the only
-- linkage (see chat_message). filename NULL = pasted image with no name.
CREATE TABLE chat_attachment (
    id       TEXT PRIMARY KEY,
    mime     TEXT NOT NULL DEFAULT 'application/octet-stream',
    data     BLOB NOT NULL,
    filename TEXT
);

-- chat_read — one read WATERMARK (not a message) per (reader, peer)
-- conversation: the newest message ts the reader has seen. The composite PK
-- is the natural identity (Python hand-serialized it into a string key).
CREATE TABLE chat_read (
    reader_id    TEXT NOT NULL,
    peer_id      TEXT NOT NULL,
    last_read_ts REAL NOT NULL DEFAULT 0.0,
    PRIMARY KEY (reader_id, peer_id)
);
CREATE INDEX idx_chat_read_peer ON chat_read (peer_id);

-- Three content-doc tables follow (user_context / role_def / lessons). They
-- look alike (text + tombstoned) but are deliberately NOT one generic doc
-- table — three governance models, three tables:
--   * user_context is the owner's ADDITIVE block: empty seed, no row = block
--     skipped. Its sibling boot-context blocks (system-interaction /
--     boot-sequence) are read-only file seeds with NO table and NO write path
--     — protection by construction, not by validation;
--   * role_def is an overlay folded over a file seed at read time (seed roles
--     resettable, custom roles hard-deletable);
--   * lessons shards per role so one role's learnings can't pollute another
--     role's boot context.
-- `tombstoned` is the reset soft-delete marker: a tombstoned row reads as the
-- default (fold back to the seed / empty).

-- user_context — the owner's user-custom additive boot-context block. A
-- SINGLE-ROW table (id pinned to 1): single-owner by decree, one doc total.
-- The wire path is still the frozen legacy /api/global-context — do not infer
-- the table name from the route.
CREATE TABLE user_context (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    text       TEXT NOT NULL DEFAULT '',
    tombstoned INTEGER NOT NULL DEFAULT 0
);

-- role_def — the role-definition overlay, keyed by the bare role key. The
-- overlay is self-contained (full effective name + definition_md).
CREATE TABLE role_def (
    role_key      TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    definition_md TEXT NOT NULL DEFAULT '',
    tombstoned    INTEGER NOT NULL DEFAULT 0
);

-- lessons — per-role learnings overlay: agents sharing a role share one doc.
-- task_type is currently a single fixed key; the column stays because the
-- frozen wire (LessonsDTO) declares it and this doc family has grown an axis
-- before (office-wide → per-role).
CREATE TABLE lessons (
    role_key   TEXT NOT NULL,
    task_type  TEXT NOT NULL,
    text       TEXT NOT NULL DEFAULT '',
    tombstoned INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (role_key, task_type)
);

-- Display-name overlays: an account / machine has NO durable row of its own —
-- both are read-time folds over telemetry + roster (their existence is
-- OBSERVED, and observed state never lands in the DB). The only durable,
-- owner-owned fact is "what I call it": a stable key -> display_name patch.
-- Purely additive — no seed, no tombstone; a missing row displays the id.

-- account_alias — account is the telemetry account tag (stable dedupe key).
CREATE TABLE account_alias (
    account      TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT ''
);

-- machine_alias — machine_id IS the warden's member.id (the server-minted
-- machine identity; see the machine = warden member note on the member table).
CREATE TABLE machine_alias (
    machine_id   TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS machine_alias;
DROP TABLE IF EXISTS account_alias;
DROP TABLE IF EXISTS lessons;
DROP TABLE IF EXISTS role_def;
DROP TABLE IF EXISTS user_context;
DROP INDEX IF EXISTS idx_chat_read_peer;
DROP TABLE IF EXISTS chat_read;
DROP TABLE IF EXISTS chat_attachment;
DROP INDEX IF EXISTS idx_chat_message_ts;
DROP TABLE IF EXISTS chat_message;
DROP TABLE IF EXISTS member;
