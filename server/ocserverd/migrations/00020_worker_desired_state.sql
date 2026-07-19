-- +goose Up
-- T-32e1/T-f190 lifecycle — outsource-worker DESIRED_STATE, a DIRECT mirror of
-- member.desired_state (migrations/00001), replacing the earlier bespoke
-- stopped_since single-column stop marker. Owner ruling "外包＝系統代管的正職員工":
-- a worker expresses its run intent exactly the way a member does, so stop/restart
-- reuse the member semantics and value domain rather than inventing a parallel one:
--   * 'online'  — the system intends the worker running (the default; a freshly
--                 assigned worker, and the state a restart returns it to);
--   * 'offline' — owner-explicit STOP: the session is killed and HELD DOWN. Every
--                 scheduler auto-revival path (9ccf stuck-recovery + paced
--                 re-dispatch, and the T-32e1 context auto-handover) keys on this
--                 — desired_state='offline' DOMINATES automatic respawn, exactly as
--                 an offline member is never reconciled back up.
-- (member's third value 'uninstall' has no worker analog — a worker is per-task and
-- reclaimed at task close, never "installed" — so it is simply never written here;
-- the column value domain still matches member's for a clean future convergence.)
-- stop = set 'offline' + kill session; restart = set 'online' + re-dispatch. The
-- bound task stays in its own status throughout. Durable so the hold-down intent
-- survives a restart (an in-memory flag would amnesia-revive the worker — the exact
-- failure this prevents); additive column, member-parity default, reversible.
ALTER TABLE outsource_worker ADD COLUMN desired_state TEXT NOT NULL DEFAULT 'online';

-- +goose Down
ALTER TABLE outsource_worker DROP COLUMN desired_state;
