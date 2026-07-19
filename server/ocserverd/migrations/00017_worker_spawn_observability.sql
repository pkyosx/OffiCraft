-- +goose Up
-- T-9ccf — outsource-worker START-chain observability. Until now a worker row
-- carried only lifecycle (id/codename/model/effort/task_id/status/created_ts/
-- released_ts): every spawn attempt, its target machine, and any failure reason
-- lived ONLY in the server's in-memory pacing maps (workerSpawnAt/Target) — a
-- restart forgot them, and a warden-side spawn refusal (session_already_exists,
-- claude_bin_unresolved) was NEVER durably visible (O-19: a worker sat 'assigned'
-- forever while the cockpit stayed green). These ADDITIVE columns make the
-- START chain durable and projectable, mirroring the member last_op* fields
-- (migrations/00006) so the outsource surface reuses the member observation
-- channel rather than inventing a parallel one.

-- spawn_attempts: how many worker_start frames have been dispatched for this
-- worker (incremented each notifyWorkerSpawn dispatch). A climbing count with
-- status still 'assigned' is the machine-readable "stuck spawning" signal.
ALTER TABLE outsource_worker ADD COLUMN spawn_attempts INTEGER NOT NULL DEFAULT 0;
-- last_spawn_ts: epoch seconds of the most recent worker_start dispatch (the
-- durable twin of the in-memory pacing stamp — survives a server restart).
ALTER TABLE outsource_worker ADD COLUMN last_spawn_ts REAL NOT NULL DEFAULT 0.0;
-- last_spawn_target: the warden (machine id) the most recent worker_start was
-- dispatched to — so a repeatedly-failing placement is visible, not guessed.
ALTER TABLE outsource_worker ADD COLUMN last_spawn_target TEXT NOT NULL DEFAULT '';
-- last_op* — the folded warden command_result receipt (worker_start/worker_stop),
-- the worker twin of member.last_op*. last_op_ok is three-valued (NULL = no
-- receipt yet) exactly like member.last_op_ok.
ALTER TABLE outsource_worker ADD COLUMN last_op TEXT NOT NULL DEFAULT '';
ALTER TABLE outsource_worker ADD COLUMN last_op_ok INTEGER;
ALTER TABLE outsource_worker ADD COLUMN last_op_log TEXT NOT NULL DEFAULT '';
ALTER TABLE outsource_worker ADD COLUMN last_op_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE outsource_worker ADD COLUMN last_op_at REAL NOT NULL DEFAULT 0.0;

-- +goose Down
ALTER TABLE outsource_worker DROP COLUMN last_op_at;
ALTER TABLE outsource_worker DROP COLUMN last_op_reason;
ALTER TABLE outsource_worker DROP COLUMN last_op_log;
ALTER TABLE outsource_worker DROP COLUMN last_op_ok;
ALTER TABLE outsource_worker DROP COLUMN last_op;
ALTER TABLE outsource_worker DROP COLUMN last_spawn_target;
ALTER TABLE outsource_worker DROP COLUMN last_spawn_ts;
ALTER TABLE outsource_worker DROP COLUMN spawn_attempts;
