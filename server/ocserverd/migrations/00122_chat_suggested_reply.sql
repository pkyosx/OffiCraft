-- +goose Up
-- The owner's 建議回覆 for the chat with one member, in display order. A member
-- with none has no rows.
CREATE TABLE chat_suggested_reply (
    member_id TEXT NOT NULL,
    position  INTEGER NOT NULL,
    text      TEXT NOT NULL,
    PRIMARY KEY (member_id, position)
);

-- +goose Down
DROP TABLE IF EXISTS chat_suggested_reply;
