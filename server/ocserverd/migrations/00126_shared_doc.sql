-- +goose Up
-- A shared document: the task artifact's shape (00086) without the task. The
-- current version lives on the shared_doc row; earlier ones in shared_doc_history.
--
-- ⚠️ No FKs, as with task_artifact. A blob referenced only from these two tables
-- survives GC only because collectSurvivingBlobRefs (dal.go) lists both of them;
-- leave either out and its blobs are deleted with no error.
CREATE TABLE shared_doc (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN ('file', 'image', 'link')),
    attachment_id TEXT NOT NULL,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL,
    created_by    TEXT NOT NULL,
    updated_ts    REAL NOT NULL,
    updated_by    TEXT NOT NULL,
    archived_ts   REAL,
    archived_by   TEXT
);
CREATE INDEX idx_shared_doc_name ON shared_doc (name, id);

-- Version order is `ORDER BY id DESC`; a restore deletes the restored row and
-- inserts the displaced current version as a new row, so ids are never reused.
CREATE TABLE shared_doc_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    doc_id        TEXT NOT NULL,
    kind          TEXT NOT NULL,
    attachment_id TEXT NOT NULL,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL,
    created_by    TEXT NOT NULL
);
CREATE INDEX idx_shared_doc_history_doc
    ON shared_doc_history (doc_id, id DESC);

-- +goose Down
DROP TABLE IF EXISTS shared_doc_history;
DROP TABLE IF EXISTS shared_doc;
