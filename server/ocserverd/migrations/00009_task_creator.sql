-- +goose Up
-- creator_id: the verified token sub (current_actor) of whoever created the
-- task — a roster member id, an outsource worker id, or the literal "owner"
-- (the cockpit token's sub). Named creator_id to mirror executor_id (both are
-- free-form attribution TEXT, not FKs). Tasks created before this column
-- existed carry '' (no back-fill is possible — the DB never stored a creator),
-- which the 任務卡 renders as an honest "—".
ALTER TABLE task ADD COLUMN creator_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task DROP COLUMN creator_id;
