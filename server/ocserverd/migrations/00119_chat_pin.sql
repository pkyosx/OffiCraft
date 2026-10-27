-- +goose Up
-- The owner's pinned chat-list members. One list for staff and outsource; each
-- cockpit tab shows its own kind's rows in this order.
CREATE TABLE chat_pin (
    member_id TEXT PRIMARY KEY,
    position  INTEGER NOT NULL
);

-- Seeded once here, never re-inserted at boot: an owner's unpin must stick.
INSERT INTO chat_pin (member_id, position) VALUES ('mira', 0);

-- +goose Down
DROP TABLE IF EXISTS chat_pin;
