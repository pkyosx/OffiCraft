-- +goose Up
-- The owner's 建議回覆 for the reply cards of one task type, in display order. A
-- type with none has no rows. type_key may name a built-in manual that has no
-- task_manual row, so there is no join to it; deleting a manual deletes its rows
-- here in the same transaction.
CREATE TABLE task_manual_suggested_reply (
    type_key TEXT NOT NULL,
    position INTEGER NOT NULL,
    text     TEXT NOT NULL,
    PRIMARY KEY (type_key, position)
);

-- +goose Down
DROP TABLE IF EXISTS task_manual_suggested_reply;
