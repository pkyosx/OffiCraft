-- +goose Up
-- scope ⑤⑥ — the 放行策略 (delegation policy) durable model for the first batch
-- of "agent 發包給外包 + owner 每筆核可閘". PURELY additive: one new table + one
-- seed row, no existing table touched, so the older Downs stay valid.
--
-- The policy answers, per delegation initiator (or the global default): may
-- this principal 發包 at all (allowed_to_delegate = a role whitelist + an owner-
-- named member whitelist), must every 發包 pass through the owner
-- (needs_per_task_card — first-batch default TRUE), and — only when a card is
-- NOT required — the 免卡 spend envelope (auto_budget_cap over budget_period,
-- plus what happens on_exhaust).
--
-- Read model: one global-default row (principal_id = '__default__') plus
-- optional per-principal override rows keyed by the initiator's member id. The
-- first batch runs on the global default alone; per-principal overrides are the
-- owner's future knob (ResolveDelegationPolicy prefers an override, falls back
-- to the default).
CREATE TABLE outsource_delegation_policy (
    -- '__default__' = the global default; else the initiator member id whose
    -- override this row is.
    principal_id        TEXT PRIMARY KEY,
    -- allowed_to_delegate, expressed as two JSON-array whitelists: the role
    -- keys allowed to 發包, and the explicitly owner-named member ids. '[]' =
    -- nobody via that arm (deny-by-default; the owner scope is always allowed
    -- in code and is never listed here).
    allowed_roles       TEXT NOT NULL DEFAULT '[]',
    allowed_members     TEXT NOT NULL DEFAULT '[]',
    -- every 發包 needs the owner's per-task card. First-batch posture: TRUE.
    needs_per_task_card INTEGER NOT NULL DEFAULT 1,
    -- 免卡 spend ceiling; NULL = uncapped. Only meaningful when a card is NOT
    -- required, so unused while needs_per_task_card stays TRUE.
    auto_budget_cap     REAL,
    -- the window the cap is measured over.
    budget_period       TEXT NOT NULL DEFAULT 'once'
                            CHECK (budget_period IN ('day', 'week', 'month', 'once')),
    -- what happens when the 免卡 budget is exhausted: fall back to a per-task
    -- owner card, or freeze the delegation.
    on_exhaust          TEXT NOT NULL DEFAULT 'card'
                            CHECK (on_exhaust IN ('card', 'freeze')),
    updated_ts          REAL NOT NULL DEFAULT 0.0
);

-- The global-default row — first-batch posture: nobody auto-delegates (both
-- whitelists empty), every 發包 requires the owner's per-task card. The column
-- DEFAULTs already encode this; the explicit seed makes the resolver return a
-- concrete default row rather than the code fallback.
INSERT INTO outsource_delegation_policy (principal_id, needs_per_task_card)
    VALUES ('__default__', 1);

-- +goose Down
DROP TABLE outsource_delegation_policy;
