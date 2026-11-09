-- +goose Up
-- Notices sent before the server marked them get meta.delete_notice, so
-- hide_delete_notices finds them by marker like the new ones. A row qualifies
-- only if it is a system message replying to a message that is in fact deleted
-- and its body is exactly the notice text for that id (「收回」 is the wording
-- trial builds used). ⚠️ Keep the CASE: meta is free-form and may not be JSON,
-- json_type raises on malformed input, and SQLite's AND does not short-circuit.
UPDATE chat_message
   SET meta = json_set(meta, '$.delete_notice',
                       json_object('message_id', json_extract(meta, '$.reply_to')))
 WHERE sender = 'system'
   AND CASE WHEN NOT json_valid(meta) THEN 0
            WHEN json_type(meta) IS NOT 'object' THEN 0
            WHEN json_type(meta, '$.delete_notice') IS NOT NULL THEN 0
            WHEN json_type(meta, '$.reply_to') IS NOT 'text' THEN 0
            ELSE body IN (
                   'Owner 刪除了訊息 #' || json_extract(meta, '$.reply_to') ||
                   '，該則內容已作廢。若沒讀過可以直接忽略；需要原文時，用 get_chat ids=' ||
                   json_extract(meta, '$.reply_to') || ' 讀回。',
                   'Owner 收回了訊息 #' || json_extract(meta, '$.reply_to') ||
                   '，該則內容已作廢。若沒讀過可以直接忽略；需要原文時，用 get_chat ids=' ||
                   json_extract(meta, '$.reply_to') || ' 讀回。')
                 AND EXISTS (SELECT 1 FROM chat_message d
                              WHERE d.id = json_extract(chat_message.meta, '$.reply_to')
                                AND d.deleted_ts > 0)
       END;

-- +goose Down
UPDATE chat_message
   SET meta = json_remove(meta, '$.delete_notice')
 WHERE sender = 'system'
   AND CASE WHEN NOT json_valid(meta) THEN 0
            ELSE json_type(meta, '$.delete_notice') IS NOT NULL END;
