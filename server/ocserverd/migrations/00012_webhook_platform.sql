-- +goose Up
-- M4 webhook 平台驗證: teach webhook_endpoint two per-endpoint verification
-- fields so the PUBLIC /in inlet can apply a platform preset (Slack / GitHub
-- signed-webhook HMAC) instead of the token-only default.
--
-- platform is a closed set mirroring status's CHECK-constrained toggle:
-- 'generic' (現行, URL token only) | 'slack' | 'github'. NOT NULL DEFAULT
-- 'generic' so every row that predates this migration auto-classifies as the
-- current token-only behaviour (向後相容, no back-fill needed).
ALTER TABLE webhook_endpoint ADD COLUMN platform TEXT NOT NULL DEFAULT 'generic'
    CHECK (platform IN ('generic', 'slack', 'github'));

-- signing_secret is the write-only shared secret the server recomputes the
-- platform HMAC under (Slack Signing Secret / GitHub webhook secret). Nullable:
-- 'generic' endpoints and every pre-existing row carry NULL (no secret). It is
-- NEVER echoed on any wire — only the derived has_signing_secret boolean is.
ALTER TABLE webhook_endpoint ADD COLUMN signing_secret TEXT;

-- +goose Down
ALTER TABLE webhook_endpoint DROP COLUMN signing_secret;
ALTER TABLE webhook_endpoint DROP COLUMN platform;
