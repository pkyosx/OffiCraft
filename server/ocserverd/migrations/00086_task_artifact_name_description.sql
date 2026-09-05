-- +goose Up
-- 00086 was taken at the moment this file was committed (Kyle, 2026-09-06,
-- c-cb5b265cbea2). It is NOT main's max+1: main's highest is 00080, and
-- 00081-00085 are held by branches still in flight (00081-00084 by T-33's lore
-- format work, 00085 by the upgrade-migration notice), scanned across all 437
-- remote refs rather than against main. 00086 is the smallest FREE number.
-- The gaps stay gaps: a later migration takes a HIGHER number and never fills
-- one in, because a migration numbered BELOW a database's current version and
-- not yet applied trips goose's missing-migration check (00071 explains this at
-- length) — which stops an already-running station from booting, while a fresh
-- install cannot tell anything is wrong.
--
-- T-92: a pinned deliverable's single `label` was doing two jobs — the short title
-- a human picks a row by, and the prose an agent reads to decide whether this is
-- the artifact it wants. Splitting them is what lets a task response stop carrying
-- the text at all (it carries `artifact_count` after this package). Owner rulings,
-- verbatim, chat ids on the ticket: c-bfb7bfef4d40 / c-17ca5c359a3c split label
-- into name + description; c-0d0a576f68af sets 48 / 256 and 「舊資料不截斷」;
-- rc-85b07ab98651 「現在開始任務產物都需要有個名字，舊的不管」; c-59fc5834d967
-- 「連結也走 attachment」 with the reason in c-8f9bf9329495 — the classification
-- must be consistent, not bent around a transport limit.
--
-- WHY A REBUILD AND NOT ALTERs. Two of the three changes cannot be an ALTER in
-- SQLite: `url` is DROPPED, and `attachment_id` loses its DEFAULT ''. The
-- create/copy/drop/rename shape is 00076's, which is 00024's, which is 00013's.
--
-- 🔴 THE INDEX IS THE TRAP, exactly as in 00076. `DROP TABLE` takes
-- idx_task_artifact_task and idx_task_artifact_history_artifact with it and
-- RENAME does not bring them back. Both directions below rebuild them explicitly.
-- Miss one and nothing raises: the queries keep answering, just by scan.
--
-- 🔴 HISTORY ROWS ARE COPIED WITH THEIR `id`. task_artifact_history.id is
-- INTEGER PRIMARY KEY AUTOINCREMENT and the version order is `ORDER BY id DESC`.
-- Let the rebuild re-number and every artifact's versions silently reorder.
--
-- MEASURED ON A READ-ONLY SNAPSHOT OF THE LIVE DB, 2026-09-05 (re-measure before
-- landing — these are facts about data, not about code):
--   task_artifact 3,240 rows: file 2,357, image 179, link 704
--   task_artifact_history 71 rows: file 62, link 9
--   link rows with a blank url .................. 0   (so the mint below cannot miss)
--   file/image rows with a blank attachment_id .. 0
--   file/image rows whose blob is already gone .. 1   ⚠️ see the note under CHECK
--   distinct link urls across both tables ....... 641 (704+9 rows share them)
--   bytes those urls occupy ..................... 36,873
--   labels longer than 256 ...................... 313  → they land in `description`
--                                                        WHOLE; nothing is truncated
--   link urls NOT starting http:// or https:// . 0   🔴 asked for by Kyle before
--                                                   this ran, and the right thing
--                                                   to ask: once a url is a blob,
--                                                   `kind` is the only thing left
--                                                   that can tell a placeholder
--                                                   from a real target. Also 0 with
--                                                   leading/trailing space, 0 with
--                                                   an inner space, 0 with a
--                                                   newline; lengths 31..118.
--                                                   RE-MEASURE BEFORE LANDING.
--   link labels longer than 48 .................. 339  → their `name` IS cut to 48;
--                                                        the full text survives in
--                                                        `description`. This is the
--                                                        one visible change: a link
--                                                        row's cockpit title gets
--                                                        shorter. Files are untouched.
--
-- ⚠️ NO `CHECK (attachment_id <> '')`, DELIBERATELY. The approved schema (spec v6
-- §1, the artifact the owner signed off on rc-210fc77beea1) says NOT NULL and says
-- in prose that the column is never blank; it does not ask for a CHECK, and today's
-- data would satisfy one. Writing a constraint the approved design did not ask for
-- is scope I do not have. The invariant is enforced at the write door instead.
-- Flagged here so a reviewer can overrule it cheaply.

-- ── 1. One text/uri-list blob per DISTINCT link target ───────────────────────
-- RFC 2483: a uri-list is one URI per line. These carry exactly the url's bytes,
-- so the blob can say what it is without a second field somewhere else saying it.
-- DEDUPED on purpose: 704+9 link rows point at 641 distinct urls, and a blob that
-- two artifacts share is collected only when BOTH stop pointing at it — which is
-- what the collector already computes (dal.go collectSurvivingBlobRefs asks six
-- sources, and source ④ `task_artifact.attachment_id` has NO kind filter, so link
-- rows begin voting the moment they have a blob and the collector needs no change).
CREATE TEMP TABLE t92_link_blob (url TEXT PRIMARY KEY, att_id TEXT NOT NULL);

-- 'att-' + 12 lowercase hex characters is the shape api_helpers.go newHexID(12)
-- mints; randomblob(6) is those same six bytes. A collision with an existing id
-- would abort this migration on the PRIMARY KEY — loudly, which is the right
-- failure for the one-in-2^48 case.
INSERT INTO t92_link_blob (url, att_id)
SELECT u, 'att-' || lower(hex(randomblob(6))) FROM (
    SELECT url AS u FROM task_artifact         WHERE kind = 'link' AND url <> ''
    UNION
    SELECT url      FROM task_artifact_history WHERE kind = 'link' AND url <> ''
);

INSERT INTO chat_attachment (id, mime, data, filename)
SELECT att_id, 'text/uri-list', CAST(url AS BLOB), NULL FROM t92_link_blob;

-- ── 2. task_artifact ─────────────────────────────────────────────────────────
CREATE TABLE task_artifact_rebuild (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('file', 'image', 'link')),
    -- every kind now points at a blob; `url` as a COLUMN is gone, and the url an
    -- API response carries is computed from this id.
    attachment_id TEXT NOT NULL,
    -- display name. New writes must supply one (48 runes); OLD ROWS ARE LEFT AS
    -- THEY ARE, which for file/image means EMPTY — the read path derives a name
    -- from the blob's filename rather than copying it into a second column that
    -- could then go stale when the content is replaced.
    name          TEXT NOT NULL DEFAULT '',
    -- the prose half of the old label. New writes cap at 256; migrated values are
    -- NOT truncated, so 313 rows arrive longer than that and readers must not
    -- assume the cap.
    description   TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL DEFAULT 0.0,
    created_by    TEXT NOT NULL DEFAULT ''
);

-- substr() over TEXT counts CHARACTERS in SQLite, so `substr(label,1,48)` is 48
-- runes — the same unit the write cap uses, not 48 bytes.
INSERT INTO task_artifact_rebuild
       (id, task_id, kind, attachment_id, name, description, created_ts, created_by)
SELECT a.id, a.task_id, a.kind,
       CASE WHEN a.kind = 'link'
            THEN (SELECT b.att_id FROM t92_link_blob b WHERE b.url = a.url)
            ELSE a.attachment_id END,
       CASE WHEN a.kind = 'link' THEN substr(a.label, 1, 48) ELSE '' END,
       a.label,
       a.created_ts, a.created_by
  FROM task_artifact a;

DROP TABLE task_artifact;
ALTER TABLE task_artifact_rebuild RENAME TO task_artifact;
CREATE INDEX idx_task_artifact_task ON task_artifact (task_id);

-- ── 3. task_artifact_history — same shape, and the id comes along ────────────
CREATE TABLE task_artifact_history_rebuild (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    artifact_id   TEXT NOT NULL,
    kind          TEXT NOT NULL,
    attachment_id TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL,
    created_by    TEXT NOT NULL DEFAULT ''
);

INSERT INTO task_artifact_history_rebuild
       (id, artifact_id, kind, attachment_id, name, description, created_ts, created_by)
SELECT h.id, h.artifact_id, h.kind,
       CASE WHEN h.kind = 'link'
            THEN (SELECT b.att_id FROM t92_link_blob b WHERE b.url = h.url)
            ELSE h.attachment_id END,
       CASE WHEN h.kind = 'link' THEN substr(h.label, 1, 48) ELSE '' END,
       h.label,
       h.created_ts, h.created_by
  FROM task_artifact_history h;

DROP TABLE task_artifact_history;
ALTER TABLE task_artifact_history_rebuild RENAME TO task_artifact_history;
CREATE INDEX idx_task_artifact_history_artifact
    ON task_artifact_history (artifact_id, id DESC);

DROP TABLE t92_link_blob;

-- +goose Down
-- Reverse: put `url` and `label` back, blank the link rows' attachment_id, and
-- collect the blobs this migration minted.
--
-- 🔴 THIS ROLLBACK IS LOSSY IN ONE DIRECTION AND THE LOSS IS SILENT. `label` is
-- restored from `description`, which is right for every row this migration wrote —
-- but any artifact CREATED OR EDITED while the new schema was live may carry a
-- `name` that is not a prefix of its `description`, and that name has nowhere to go.
-- Going down after real use loses those names. Stated rather than guarded because a
-- Down is a rollback path, not a supported mode.
CREATE TABLE task_artifact_rebuild (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('file', 'image', 'link')),
    attachment_id TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT '',
    label         TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL DEFAULT 0.0,
    created_by    TEXT NOT NULL DEFAULT ''
);

INSERT INTO task_artifact_rebuild
       (id, task_id, kind, attachment_id, url, label, created_ts, created_by)
SELECT a.id, a.task_id, a.kind,
       CASE WHEN a.kind = 'link' THEN '' ELSE a.attachment_id END,
       CASE WHEN a.kind = 'link'
            THEN COALESCE((SELECT CAST(c.data AS TEXT) FROM chat_attachment c
                            WHERE c.id = a.attachment_id), '')
            ELSE '' END,
       -- name is dropped here: for every row this migration created it is a prefix
       -- of description, so nothing of the ORIGINAL data is lost. See the warning.
       a.description,
       a.created_ts, a.created_by
  FROM task_artifact a;

CREATE TABLE task_artifact_history_rebuild (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    artifact_id   TEXT NOT NULL,
    kind          TEXT NOT NULL,
    attachment_id TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT '',
    label         TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL,
    created_by    TEXT NOT NULL DEFAULT ''
);

INSERT INTO task_artifact_history_rebuild
       (id, artifact_id, kind, attachment_id, url, label, created_ts, created_by)
SELECT h.id, h.artifact_id, h.kind,
       CASE WHEN h.kind = 'link' THEN '' ELSE h.attachment_id END,
       CASE WHEN h.kind = 'link'
            THEN COALESCE((SELECT CAST(c.data AS TEXT) FROM chat_attachment c
                            WHERE c.id = h.attachment_id), '')
            ELSE '' END,
       h.description,
       h.created_ts, h.created_by
  FROM task_artifact_history h;

-- Collect the minted blobs BEFORE the old tables are gone, because that is what
-- identifies them: a text/uri-list blob whose only referents are link artifacts.
-- A .uri file someone genuinely UPLOADED is kind='file', so it is not caught here.
DELETE FROM chat_attachment
 WHERE mime = 'text/uri-list'
   AND id IN (SELECT attachment_id FROM task_artifact         WHERE kind = 'link'
              UNION
              SELECT attachment_id FROM task_artifact_history WHERE kind = 'link');

DROP TABLE task_artifact;
ALTER TABLE task_artifact_rebuild RENAME TO task_artifact;
CREATE INDEX idx_task_artifact_task ON task_artifact (task_id);

DROP TABLE task_artifact_history;
ALTER TABLE task_artifact_history_rebuild RENAME TO task_artifact_history;
CREATE INDEX idx_task_artifact_history_artifact
    ON task_artifact_history (artifact_id, id DESC);
