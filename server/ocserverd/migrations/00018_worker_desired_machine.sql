-- +goose Up
-- T-f190 — outsource-worker OWNER-PINNED machine placement. Until now a worker
-- had NO durable machine binding by design (placement was decided fresh at each
-- spawn from the manual's "auto" | machine-id preference, landing in the
-- in-memory pacing maps + the durable last_spawn_target OBSERVATION). The owner
-- cockpit now offers the same 改機器 operation the member detail panel carries,
-- so a worker needs a durable DESIRED placement the owner sets — the twin of
-- member.desired_machine_id. notifyWorkerSpawn prefers this pin over the manual
-- preference; the relocate handler (POST /api/outsource-workers/{id}/relocate)
-- writes it, kills the old session, and clears pacing so the next tick re-spawns
-- on the chosen machine (no lifecycle change — the worker stays assigned/active).
--
-- Closed values: "" (unpinned — fall back to the manual's placement preference,
-- the pre-f190 behaviour), "auto" (owner explicitly chose idlest-online), or a
-- concrete machine id.
ALTER TABLE outsource_worker ADD COLUMN desired_machine_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE outsource_worker DROP COLUMN desired_machine_id;
