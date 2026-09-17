-- +goose Up
-- T-236 — admit scope_kind='everyone' on lore_entry (所有人: rides every
-- member's boot document; scope_key is '').
--
-- Number: highest taken on origin/main and on every remote branch at the time
-- of writing was 00107 (both sources scanned: migrations/*.sql and the
-- AddNamedMigrationContext family). Re-scan before this lands.
--
-- SQLite cannot alter a CHECK in place, so this is create/copy/drop/rename, the
-- 00088 shape. 'role' STAYS in the set: 00100 deliberately left orphan rows at
-- 'role' and they must remain storable (see domain.go's LoreScope* note).
--
-- The column list is the whole of 00093's table; no later migration altered
-- it. The one index 00093 created is destroyed by DROP TABLE and rebuilt below.
CREATE TABLE lore_entry_rebuild (
    id             TEXT PRIMARY KEY,
    seq            INTEGER NOT NULL,
    scope_kind     TEXT    NOT NULL
                           CHECK (scope_kind IN ('role', 'agent', 'manual', 'everyone')),
    scope_key      TEXT    NOT NULL,
    title          TEXT    NOT NULL,
    body           TEXT    NOT NULL,
    author_id      TEXT    NOT NULL DEFAULT '',
    source_task_id TEXT    NOT NULL DEFAULT '',
    state          TEXT    NOT NULL DEFAULT 'active'
                           CHECK (state IN ('active', 'pinned', 'retired')),
    retire_reason  TEXT    NOT NULL DEFAULT '',
    effective_ts   REAL    NOT NULL,
    created_ts     REAL    NOT NULL,
    updated_ts     REAL    NOT NULL
);

INSERT INTO lore_entry_rebuild (id, seq, scope_kind, scope_key, title, body,
    author_id, source_task_id, state, retire_reason,
    effective_ts, created_ts, updated_ts)
  SELECT id, seq, scope_kind, scope_key, title, body,
    author_id, source_task_id, state, retire_reason,
    effective_ts, created_ts, updated_ts
  FROM lore_entry;

DROP TABLE lore_entry;
ALTER TABLE lore_entry_rebuild RENAME TO lore_entry;

CREATE INDEX idx_lore_entry_scope_state_effective
    ON lore_entry (scope_kind, scope_key, state, effective_ts DESC);

-- +goose Down
-- 🔴 Not value-lossless: an 'everyone' row has no representation under the old
-- CHECK. Rather than drop such rows, the Down refuses (the INSERT fails on the
-- CHECK) so a rollback cannot silently delete lore; move those entries to
-- another scope first.
CREATE TABLE lore_entry_rebuild (
    id             TEXT PRIMARY KEY,
    seq            INTEGER NOT NULL,
    scope_kind     TEXT    NOT NULL CHECK (scope_kind IN ('role', 'agent', 'manual')),
    scope_key      TEXT    NOT NULL,
    title          TEXT    NOT NULL,
    body           TEXT    NOT NULL,
    author_id      TEXT    NOT NULL DEFAULT '',
    source_task_id TEXT    NOT NULL DEFAULT '',
    state          TEXT    NOT NULL DEFAULT 'active'
                           CHECK (state IN ('active', 'pinned', 'retired')),
    retire_reason  TEXT    NOT NULL DEFAULT '',
    effective_ts   REAL    NOT NULL,
    created_ts     REAL    NOT NULL,
    updated_ts     REAL    NOT NULL
);

INSERT INTO lore_entry_rebuild (id, seq, scope_kind, scope_key, title, body,
    author_id, source_task_id, state, retire_reason,
    effective_ts, created_ts, updated_ts)
  SELECT id, seq, scope_kind, scope_key, title, body,
    author_id, source_task_id, state, retire_reason,
    effective_ts, created_ts, updated_ts
  FROM lore_entry;

DROP TABLE lore_entry;
ALTER TABLE lore_entry_rebuild RENAME TO lore_entry;

CREATE INDEX idx_lore_entry_scope_state_effective
    ON lore_entry (scope_kind, scope_key, state, effective_ts DESC);
