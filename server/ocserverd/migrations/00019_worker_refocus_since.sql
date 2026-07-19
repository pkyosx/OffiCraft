-- +goose Up
-- T-32e1/T-f190 lifecycle — outsource-worker REFOCUS marker, the durable twin
-- of member.refocus_since (migrations/00006). Until now a worker had no way to
-- record an in-flight context handover: a member's refocus (manual button OR
-- the context-high auto-stamp) writes refocus_since and the reconcile loop-break
-- clears it on respawn; a worker had neither the column nor the mechanism. The
-- owner mental model is "外包只是系統會幫我產生跟刪除的正職員工" — so a worker
-- reuses the SAME marker semantics:
--   * the owner's 換手 (POST /api/outsource-workers/{id}/refocus) stamps it,
--     then kills+respawns the session (relocateWorkerNow) so the fresh worker
--     picks the bound task back up from its task plan / step notes;
--   * the context-high auto-handover (outsource tick, ACTIVE-worker branch)
--     stamps it on its own when the worker's gauge crosses the HANDOVER band —
--     the automatic counterpart of the button, reusing the member bandFor /
--     bootStormFor guards;
--   * while set it is the anti-loop COOLDOWN (a worker mid-handover is skipped),
--     cleared by the tick's loop-break the moment a fresh session boots after
--     the stamp (gauge boot_ts > refocus_since) — the worker analog of
--     clearRecycleMarkersOnRespawn.
-- Durable (not in-memory) so the cooldown + loop-break survive a server restart
-- exactly like the member marker; additive column, pure default, reversible.
ALTER TABLE outsource_worker ADD COLUMN refocus_since REAL NOT NULL DEFAULT 0.0;

-- +goose Down
ALTER TABLE outsource_worker DROP COLUMN refocus_since;
