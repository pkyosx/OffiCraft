-- +goose Up
-- ⚠️ THE NUMBER 00071 IS PROVISIONAL AND WILL BE RE-TAKEN AT MERGE TIME.
-- It is main's max+1 as of 2026-09-03, chosen so the PR's cloud CI can run at
-- all: GitHub tests the MERGE of this branch into main, so while this file
-- carried 00070 — taken by #387 the moment it landed — every Go job died with
-- `panic: goose: duplicate version 70`, before a single assertion ran.
-- Other branches are still queued at the door for these same numbers (#394
-- wants two, #389 one), so the final number is whatever main's max+1 is when
-- this actually merges. Nothing in the schema or the code depends on the
-- number; re-take it, rename this file, and re-run CI.
-- 🔴 Do NOT reach past a queued number to close a gap: a migration numbered
-- BELOW a database's current version and not yet applied trips goose's
-- missing-migration check (allowMissing=false), which is why the number is
-- taken last and not reserved early.
--
-- T-60 makes a pinned deliverable REPLACEABLE: the same artifact id keeps
-- pointing at the card while its content is swapped. The live row stays in
-- task_artifact; this is the append-only pre-write journal of the versions it
-- replaced, keyed by that stable artifact id — the same shape as
-- document_history (00043) and retained to the same depth
-- (documentHistoryKeepDefault, three), so an artifact's history can never grow
-- without bound.
--
-- No foreign key to task_artifact on purpose: the rows outlive nothing. The
-- remove path (remove_task_artifact) deletes them in the same transaction that
-- deletes the live row, so an owner-less version is never left behind, and the
-- blobs only those versions referenced are collected with them.
CREATE TABLE task_artifact_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    artifact_id   TEXT NOT NULL,
    kind          TEXT NOT NULL,
    attachment_id TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT '',
    label         TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL,
    created_by    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_task_artifact_history_artifact
    ON task_artifact_history (artifact_id, id DESC);

-- +goose Down
DROP TABLE task_artifact_history;
