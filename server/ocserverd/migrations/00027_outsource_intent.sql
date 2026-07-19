-- +goose Up
-- outsource_intent — the durable landing for a DISPATCHED-but-not-yet-approved
-- 發包 (節點9 ⑤). When the single spawn gate returns `pending`, the task moves
-- to `pending_outsource_approval`, an owner reply card opens, and the dispatch
-- proposal is parked HERE for the Pass-2 approve handler to mint the worker
-- from (target model/effort/machine + the version the owner approves).
--
-- One row per task (task_id PRIMARY KEY): a re-dispatch overwrites in place and
-- bumps `version` (the approve-time CAS epoch — OutsourceIntentIdempotencyKey /
-- CanApproveOutsourceIntent). `issued_by` is the dispatch initiator (發起者) —
-- the verified token sub, whose delegation policy the gate resolved. Purely
-- additive: no existing table is touched, so the older Downs stay valid.
CREATE TABLE outsource_intent (
    task_id     TEXT PRIMARY KEY,
    -- intent epoch; bumped on each re-dispatch of the same task.
    version     INTEGER NOT NULL DEFAULT 1,
    model       TEXT NOT NULL DEFAULT '',
    effort      TEXT NOT NULL DEFAULT '',
    -- spawn placement preference ('auto' | machine id), carried to the Pass-2
    -- approve→spawn seam.
    machine     TEXT NOT NULL DEFAULT '',
    -- the actor that dispatched (發起者) — the verified token sub.
    issued_by   TEXT NOT NULL DEFAULT '',
    created_ts  REAL NOT NULL DEFAULT 0.0,
    updated_ts  REAL NOT NULL DEFAULT 0.0
);

-- +goose Down
DROP TABLE outsource_intent;
