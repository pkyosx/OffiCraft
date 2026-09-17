-- +goose Up
-- T-246: the handover declaration is gone from the wire and the code, so its
-- three columns (added by 00031, carried through 00088's rebuild) go too. None
-- is indexed or named by a view or trigger, so a plain DROP COLUMN suffices
-- (the same measured path 00105 uses on modernc.org/sqlite).
--
-- ⚠️ OLD BINARY + NEW SCHEMA fails every task read ("no such column: handoff"),
-- so migrate and restart as one operation; a binary rollback across this
-- version must run the Down first.
ALTER TABLE task DROP COLUMN handoff_task_id;
ALTER TABLE task DROP COLUMN handoff_note;
ALTER TABLE task DROP COLUMN handoff;

-- +goose Down
-- The shape comes back empty: the Up kept no copy of any declaration.
ALTER TABLE task ADD COLUMN handoff         TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN handoff_note    TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN handoff_task_id TEXT NOT NULL DEFAULT '';
