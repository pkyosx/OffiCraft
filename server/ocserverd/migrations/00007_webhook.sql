-- +goose Up
-- webhook_endpoint — M4 回呼端點: an external trigger inlet bound to ONE member.
-- A member may carry MANY endpoints. The server identifies (member · endpoint ·
-- purpose) SOLELY from the opaque, high-entropy token (the PK) — the public /in
-- path carries nothing else (no member/endpoint/purpose in the URL), so
-- changing the path or any param cannot forge identity or purpose (SPEC §2 安全).
--
-- No FK by decree (00001 convention): member_id is attribution only. Deleting a
-- member does not cascade — a stale endpoint simply stops delivering (its
-- member resolves absent at /in time and the post is dropped).
--
-- status is a REVOCATION toggle, not a lifecycle: 'disabled' → /in ignores the
-- call (可復), a row DELETE → permanent revocation (SPEC §2 安全). endpoint_id is
-- user-chosen, immutable after creation, and UNIQUE per member (the management
-- address key); purpose is editable free text (the 用途守衛 rides it into
-- meta.purpose per delivered event, never into any seed).
CREATE TABLE webhook_endpoint (
    -- opaque high-entropy secret; the ONLY identity key /in consults (不可猜、
    -- 可撤銷). PRIMARY KEY ⇒ globally unique across every member's endpoints.
    token        TEXT PRIMARY KEY,
    -- the member this inlet delivers to (attribution; no FK).
    member_id    TEXT NOT NULL,
    -- user-chosen identifier, immutable after create, no whitespace/special
    -- chars (domain regex ^[A-Za-z0-9_-]+$); unique per member.
    endpoint_id  TEXT NOT NULL,
    -- editable free-text description the member judges incoming payloads against.
    purpose      TEXT NOT NULL DEFAULT '',
    -- closed set: 'enabled' (受理) | 'disabled' (忽略, 可復).
    status       TEXT NOT NULL CHECK (status IN ('enabled', 'disabled'))
                     DEFAULT 'enabled',
    created_ts   REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_webhook_member ON webhook_endpoint (member_id);
CREATE UNIQUE INDEX idx_webhook_member_endpoint
    ON webhook_endpoint (member_id, endpoint_id);

-- +goose Down
DROP INDEX IF EXISTS idx_webhook_member_endpoint;
DROP INDEX IF EXISTS idx_webhook_member;
DROP TABLE IF EXISTS webhook_endpoint;
