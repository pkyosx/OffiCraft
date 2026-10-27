-- +goose Up
-- The owner's chat-list groups, one ordered set per cockpit tab ('staff' or
-- 'outsource'). A member in no group is ungrouped, which is not stored.
CREATE TABLE chat_group (
    id         TEXT PRIMARY KEY,
    tab        TEXT NOT NULL,
    name       TEXT NOT NULL,
    position   INTEGER NOT NULL,
    created_ts REAL NOT NULL DEFAULT 0.0
);

-- No REFERENCES on group_id: this station never turns foreign_keys on, so a
-- declared key would not cascade. Deleting a group must delete its rows here and
-- in chat_group_task_type in the same transaction.
CREATE TABLE chat_group_member (
    member_id TEXT PRIMARY KEY,
    group_id  TEXT NOT NULL,
    position  INTEGER NOT NULL
);

-- Outsource auto-join rules: a worker whose task has type_key joins group_id
-- when it is assigned. The primary key is what keeps a type in one group.
CREATE TABLE chat_group_task_type (
    type_key TEXT PRIMARY KEY,
    group_id TEXT NOT NULL,
    position INTEGER NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS chat_group_task_type;
DROP TABLE IF EXISTS chat_group_member;
DROP TABLE IF EXISTS chat_group;
