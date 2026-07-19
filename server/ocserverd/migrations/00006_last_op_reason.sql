-- +goose Up
-- last_op_reason: the STRUCTURED cause of the most recent warden op receipt
-- ("<code>: <detail>" from SpawnOutcome.Reason, e.g. "session_already_exists:
-- tmux session ... is already live"). Fixes the 2026-07-13 Mira incident where
-- reconcile-driven starts were refused warden-side but the owner only ever saw
-- "✕ 啟動 失敗" — the WHY (a one-line summary, distinct from the free-form
-- last_op_log dump) was never persisted. '' = the receipt carried no reason
-- (old warden, or a successful op) — the FE then renders status-only as before.
ALTER TABLE member ADD COLUMN last_op_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE member DROP COLUMN last_op_reason;
