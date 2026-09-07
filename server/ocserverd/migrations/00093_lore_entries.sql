-- +goose Up
-- T-33 傳承（lore）— the entry table, and the counter its display numbers come
-- from.
--
-- 🔴 THE NUMBER. This took 00093, not `main's highest + 1` (= 00089). The
-- committed .sql files on main stop at 00088, but the number has to be unique
-- across every branch that will ever merge, not across the one in hand: a scan
-- of all 454 `refs/remotes/origin/*` at the time of writing found the highest
-- taken number to be 00092. As with 00088, the number is only true as of that
-- scan — re-scan BOTH sources (`migrations/*.sql` AND the
-- `AddNamedMigrationContext("NNNNN` family) before this lands.
--
-- WHAT A LORE ENTRY IS. One short thing somebody learned, written once and
-- never edited, that later boots of the same ROLE (or later readers of the same
-- task TYPE) are handed for free. Two scopes, one table:
--
--   scope_kind='role'   ⇒ scope_key is a role_key   — rides the STAFF boot doc.
--   scope_kind='manual' ⇒ scope_key is a task manual type_key — rides
--                         GET /api/task-manuals/{type_key}.
--
-- 🔴 THERE IS NO EDIT PATH, BY DECREE. `title` and `body` are written once and
-- no API changes them. What CAN move is `state`, `retire_reason` and
-- `effective_ts` — and that is the whole mutable surface. This is why the two
-- timestamps below are not one timestamp.
--
-- 🔴 created_ts VS effective_ts — THE POINT OF HAVING BOTH. `created_ts` is when
-- the entry was written and NEVER changes; `effective_ts` starts equal to it and
-- is what 「提到最新」 overwrites. Folding them into one column would make
-- 「提到最新」 destroy the only record of when the entry was actually written,
-- and there would be no way back. Keeping created_ts is what makes the bump
-- REVERSIBLE — the original value is still on the row.
--
-- 🔴 state IS EXCLUSIVE, WHICH IS WHY IT IS ONE COLUMN AND NOT THREE FLAGS.
-- active / pinned / retired: an entry is exactly one of them. Two booleans
-- (`pinned`, `retired`) would admit the pinned-AND-retired row, which the
-- selection order has no answer for, and nothing would refuse to write it. The
-- CHECK is what makes exclusivity a schema fact.
CREATE TABLE lore_entries (
    -- 'L-' + seq. TEXT rather than the integer so it is the same shape as every
    -- other id on the wire and so the display number is never re-derived at a
    -- second site.
    id             TEXT PRIMARY KEY,
    -- The globally ascending number behind the id. It is stored as its own
    -- INTEGER column rather than parsed back out of the id whenever it is
    -- needed: it is the stable tie-break in the selection order, and a sort that
    -- has to CAST a substring is a sort that goes wrong on the first id that
    -- does not match the assumed shape.
    seq            INTEGER NOT NULL,
    scope_kind     TEXT    NOT NULL CHECK (scope_kind IN ('role', 'manual')),
    scope_key      TEXT    NOT NULL,
    title          TEXT    NOT NULL,
    body           TEXT    NOT NULL,
    -- The writer's member id, PINNED AT WRITE TIME. Not a foreign key and never
    -- re-resolved: a member who leaves the roster does not retroactively unwrite
    -- what they wrote. The cockpit decides what it can still render (an avatar,
    -- a chat link) from the LIVE roster; this column is the record.
    author_id      TEXT    NOT NULL DEFAULT '',
    -- The task the writer was in when they wrote it. Pure provenance — nothing
    -- reads it to make a decision, and it is '' for a role entry written outside
    -- any task.
    source_task_id TEXT    NOT NULL DEFAULT '',
    state          TEXT    NOT NULL DEFAULT 'active'
                           CHECK (state IN ('active', 'pinned', 'retired')),
    -- Only meaningful while state='retired'. '' = the retirement carried no
    -- reason, which the cockpit renders as no row at all rather than an empty
    -- one.
    retire_reason  TEXT    NOT NULL DEFAULT '',
    effective_ts   REAL    NOT NULL,
    created_ts     REAL    NOT NULL,
    updated_ts     REAL    NOT NULL
);

-- The selection index, in the order the selector reads: narrow to one scope,
-- drop the retired, then walk newest-effective first. `state` sits before
-- `effective_ts` because the selector filters on it and does not range over it.
CREATE INDEX idx_lore_entries_scope_state_effective
    ON lore_entries (scope_kind, scope_key, state, effective_ts DESC);

-- The display-number counter, the same one-row shape (and the same
-- compare-and-set claim) as task_id_seq from 00060. `next` is the number the
-- NEXT entry will be called, so a fresh database mints L-1.
--
-- The property is UNIQUENESS, not contiguity: a rolled-back write returns its
-- number and nobody is hurt by the gap, while two entries called L-7 would make
-- the id ambiguous on every face that shows one.
CREATE TABLE lore_entries_seq (
    id   INTEGER PRIMARY KEY CHECK (id = 1),
    next INTEGER NOT NULL
);

INSERT INTO lore_entries_seq (id, next) VALUES (1, 1);

-- +goose Down
DROP TABLE lore_entries_seq;
DROP INDEX idx_lore_entries_scope_state_effective;
DROP TABLE lore_entries;
