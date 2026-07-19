-- +goose Up
-- reply_card — one 等我回覆卡 (reply card): an ask the OWNER must answer
-- before the initiating agent proceeds. The state machine is deliberately
-- minimal: status 'waiting' -> 'answered' via an answer, and NOTHING else —
-- no close, no skip, no reopen (a revised answer keeps 'answered'; an agent
-- that still needs a call opens a NEW card). Extension (e.g. a future
-- auto-approve timeout) is new columns/values, not a redesign — which is why
-- there are no speculative dead columns here.
--
-- The card rides the chat stream: creating a card also posts one ordinary
-- chat_message (initiator -> owner) whose meta carries reply_card_id, and
-- chat_message_id below is the reverse link (the jump-to-origin anchor).
-- Like chat_message.meta, options / answer_attachments are JSON TEXT: options
-- is the frozen quick-reply wording (index 0 = the AI recommendation) and
-- answer_attachments are the light [{id, mime, filename}] refs into the
-- SHARED chat_attachment blob store (one attachment mechanism, not two).
CREATE TABLE reply_card (
    id                 TEXT PRIMARY KEY,
    -- the initiating member (the verified JWT sub at create time).
    from_member        TEXT NOT NULL DEFAULT '',
    -- closed set: 'decision' (needs the owner's call) | 'action' (needs the
    -- owner to do something first).
    kind               TEXT NOT NULL CHECK (kind IN ('decision', 'action')),
    summary            TEXT NOT NULL DEFAULT '',
    body               TEXT NOT NULL DEFAULT '',
    -- JSON array of 1..4 option strings; [0] is ALWAYS the AI recommendation.
    options            TEXT NOT NULL DEFAULT '[]',
    status             TEXT NOT NULL CHECK (status IN ('waiting', 'answered'))
                            DEFAULT 'waiting',
    created_ts         REAL NOT NULL DEFAULT 0.0,
    -- epoch seconds of the LATEST answer (0.0 while waiting; a revised answer
    -- re-stamps it — the 24h recently-answered window keys off this).
    answered_ts        REAL NOT NULL DEFAULT 0.0,
    -- the chat message this card rides in (jump-to-origin).
    chat_message_id    TEXT NOT NULL DEFAULT '',
    -- the answer: option index (NULL = free text only), typed text, and the
    -- light attachment refs (JSON, chat_attachment ids).
    answer_option_idx  INTEGER,
    answer_text        TEXT NOT NULL DEFAULT '',
    answer_attachments TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX idx_reply_card_status ON reply_card (status);

-- +goose Down
DROP INDEX IF EXISTS idx_reply_card_status;
DROP TABLE IF EXISTS reply_card;
