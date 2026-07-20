-- +goose Up
-- T-23cf 「廢發包白名單」. Owner ruling: 發包 needs no per-agent authorization —
-- cost is already hard-capped by the scheduler's global parallel cap
-- (task.outsource_max_parallel; outsource_sched.go queues at the cap). The
-- spawn gate (outsource_gate.go) now admits any authenticated initiator and
-- no longer reads a delegation policy, so the 00026 whitelist/budget table has
-- ZERO consumers — drop it rather than keep a dead governance surface. A
-- future budget/記帳 pass (the gate's meterOutsourceDispatch seam) will bring
-- its own model; nothing here is worth carrying forward.
DROP TABLE outsource_delegation_policy;

-- +goose Down
-- Recreate the 00026 shape + its global-default seed row (first-batch posture:
-- nobody auto-delegates, every 發包 needs a per-task card).
CREATE TABLE outsource_delegation_policy (
    principal_id        TEXT PRIMARY KEY,
    allowed_roles       TEXT NOT NULL DEFAULT '[]',
    allowed_members     TEXT NOT NULL DEFAULT '[]',
    needs_per_task_card INTEGER NOT NULL DEFAULT 1,
    auto_budget_cap     REAL,
    budget_period       TEXT NOT NULL DEFAULT 'once'
                            CHECK (budget_period IN ('day', 'week', 'month', 'once')),
    on_exhaust          TEXT NOT NULL DEFAULT 'card'
                            CHECK (on_exhaust IN ('card', 'freeze')),
    updated_ts          REAL NOT NULL DEFAULT 0.0
);
INSERT INTO outsource_delegation_policy (principal_id, needs_per_task_card)
    VALUES ('__default__', 1);
