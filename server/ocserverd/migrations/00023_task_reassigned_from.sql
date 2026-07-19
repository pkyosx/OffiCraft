-- +goose Up
-- reassigned_from / reassigned_from_kind: the PREDECESSOR the task was handed
-- over from (T-ba04 转派交接握手). On every reassign the server stamps the OLD
-- executor here (reassigned_from = its id, reassigned_from_kind = 'member' |
-- 'outsource') so the NEW executor — and the cockpit 任務卡 — can name who to
-- hand over WITH. Mirrors creator_id / executor_id (00009): free-form
-- attribution TEXT, not FKs. Tasks never reassigned (or reassigned before this
-- column existed) carry '' / '' (no back-fill is possible — the DB never stored
-- a predecessor), which the 任務卡 renders as an honest absent 前任 row.
--
-- Pure additive, reversible; an older server ignores the columns entirely (they
-- are read only through the T-ba04 handover handlers + DTO projection).
ALTER TABLE task ADD COLUMN reassigned_from TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN reassigned_from_kind TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task DROP COLUMN reassigned_from_kind;
ALTER TABLE task DROP COLUMN reassigned_from;
