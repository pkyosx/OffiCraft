-- +goose Up
-- One index per side of a message, so a "chat involving X" read (built in
-- dal.go as a UNION of a sender branch and a recipient branch) costs work
-- proportional to X's own messages instead of walking idx_chat_message_ts.
--
-- ⚠️ With these indexes present, the plain `sender = ? OR recipient = ?` form
-- makes the planner pick a MULTI-INDEX OR plus a temp sort (T-240, measured
-- with the server's SQLite driver on a copy of production chat data, no
-- ANALYZE): the busiest participant's latest page went from 0.09 ms to 33 ms.
-- Small synthetic tables may still plan a ts-index scan. Keep the UNION form.
CREATE INDEX idx_chat_message_sender_ts ON chat_message (sender, ts, id);
CREATE INDEX idx_chat_message_recipient_ts ON chat_message (recipient, ts, id);

-- +goose Down
DROP INDEX IF EXISTS idx_chat_message_recipient_ts;
DROP INDEX IF EXISTS idx_chat_message_sender_ts;
