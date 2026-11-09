-- +goose Up
-- When the owner deleted the message (epoch seconds); 0 = not deleted. Any
-- sender's row may carry it.
ALTER TABLE chat_message ADD COLUMN deleted_ts REAL NOT NULL DEFAULT 0;

-- Trial builds kept the time in meta.recalled_ts. It moves to the column and the
-- key leaves meta, so there is one source of truth. ⚠️ Keep the CASE: meta is
-- free-form and may not be JSON, json_type raises on malformed input, and
-- SQLite's AND does not short-circuit.
UPDATE chat_message
   SET deleted_ts = json_extract(meta, '$.recalled_ts'),
       meta = json_remove(meta, '$.recalled_ts')
 WHERE CASE WHEN NOT json_valid(meta) THEN 0
            WHEN json_type(meta, '$.recalled_ts') IN ('integer', 'real')
            THEN json_extract(meta, '$.recalled_ts') > 0
            ELSE 0 END;

-- Keeps the member unread count a covering range once it excludes deleted
-- messages; readers say `deleted_ts = 0`, since only an equality lets the third
-- column narrow the range. idx_chat_message_recipient_sender_ts stays: the
-- owner's reads do not filter on deleted_ts and need its (sender, ts) range.
CREATE INDEX idx_chat_message_recipient_sender_deleted_ts
    ON chat_message (recipient, sender, deleted_ts, ts);

-- +goose Down
DROP INDEX IF EXISTS idx_chat_message_recipient_sender_deleted_ts;
UPDATE chat_message
   SET meta = json_set(meta, '$.recalled_ts', deleted_ts)
 WHERE deleted_ts > 0
   AND CASE WHEN NOT json_valid(meta) THEN 0 ELSE json_type(meta) = 'object' END;
ALTER TABLE chat_message DROP COLUMN deleted_ts;
