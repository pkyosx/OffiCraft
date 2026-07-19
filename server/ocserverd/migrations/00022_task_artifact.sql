-- +goose Up
-- T-3dc5: a task's curated ARTIFACT SET — the deliverables an executing agent
-- pins onto its task card (files, images, and links such as a PR url). A NEW
-- table, not columns on task, for the same reason M3 kept its five tables
-- (00003/00004 spirit — no speculative columns): an artifact carries per-row
-- identity/ordering/attribution and there may be many per task, so it is its
-- own row, queried and indexed on its own, never a JSON blob smeared onto task.
--
-- File / image artifacts REUSE the shared chat_attachment blob store (one blob
-- mechanism, not two — the chat/reply-card precedent): the agent first uploads
-- bytes via POST /api/chat/attachments to mint an att- id, then registers the
-- artifact here with attachment_id pointing at that blob. The LINK kind is the
-- part the chat-attachment model cannot express — chat_attachment.data is BLOB
-- NOT NULL, so a blob-less URL has no home there; kind='link' + url is how a
-- pure URL (a PR link) lives on a task without a blob.
--
-- No FK on task_id: the chat_attachment precedent (00001_schema.sql) — the
-- id/attribution IS the belonging edge, a second FK edge would be a second
-- source of truth. A task delete is not a path in this system (tasks reach a
-- terminal status, rows are retained as the audit trail), so there is no
-- cascade to model. Pure additive, reversible; an older server ignores the
-- table entirely (it only reads task_artifact through the T-3dc5 handlers).
CREATE TABLE task_artifact (
    id            TEXT PRIMARY KEY,          -- "ta-" + hex (api_tasks.go mint)
    task_id       TEXT NOT NULL,             -- the owning task (no FK — see above)
    kind          TEXT NOT NULL CHECK (kind IN ('file', 'image', 'link')),
    -- kind ∈ {file, image} → the chat_attachment.id holding the bytes; '' for link.
    attachment_id TEXT NOT NULL DEFAULT '',
    -- kind = link → the pure URL (a PR link); '' for file/image.
    url           TEXT NOT NULL DEFAULT '',
    -- display label: a link's title (e.g. "PR #123"), or an override name; the
    -- blob's own filename is the fallback for file/image (resolved read-time).
    label         TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL DEFAULT 0.0,
    -- the verified sub of the registrar (§14 caller identity); '' on none.
    created_by    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_task_artifact_task ON task_artifact (task_id);

-- +goose Down
DROP INDEX idx_task_artifact_task;
DROP TABLE task_artifact;
