-- +goose Up
-- T-1aa4: the `expired` terminal status + its `expired_ts` stamp so the OWNER
-- can retire a stale ask without answering it (標為過期 — NOT an answer; the
-- agent reopens a fresh card if the question still matters). Two schema moves,
-- one rebuild (the 00011 task precedent):
--   1. DROP the DB-level status CHECK (00003 predicted this: "Extension is new
--      columns/values, not a redesign"): the three-state closed set now lives
--      ONLY in code (api_replycards.go replyCardStatus* constants), so a future
--      status costs zero schema churn. SQLite cannot DROP a CHECK in place,
--      hence the create/copy/swap rebuild;
--   2. ADD expired_ts — epoch seconds of the owner's expire action (0.0 unless
--      status='expired'; the 24h recently-expired pane keys off it).
-- kind keeps its CHECK (only the STATUS whitelist moved to code). reply_card
-- carries no FKs and one index, so the rebuild is a plain
-- create/copy/drop/rename.
CREATE TABLE reply_card_rebuild (
    id                 TEXT PRIMARY KEY,
    from_member        TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL CHECK (kind IN ('decision', 'action')),
    summary            TEXT NOT NULL DEFAULT '',
    body               TEXT NOT NULL DEFAULT '',
    options            TEXT NOT NULL DEFAULT '[]',
    -- the closed set is enforced in code now (replyCardStatus*); no CHECK.
    status             TEXT NOT NULL DEFAULT 'waiting',
    created_ts         REAL NOT NULL DEFAULT 0.0,
    answered_ts        REAL NOT NULL DEFAULT 0.0,
    -- epoch seconds of the owner's expire action; 0.0 unless status='expired'.
    expired_ts         REAL NOT NULL DEFAULT 0.0,
    chat_message_id    TEXT NOT NULL DEFAULT '',
    answer_option_idx  INTEGER,
    answer_text        TEXT NOT NULL DEFAULT '',
    answer_attachments TEXT NOT NULL DEFAULT '[]',
    task_id            TEXT NOT NULL DEFAULT '',
    task_step_id       TEXT NOT NULL DEFAULT ''
);
INSERT INTO reply_card_rebuild (id, from_member, kind, summary, body, options,
    status, created_ts, answered_ts, chat_message_id,
    answer_option_idx, answer_text, answer_attachments, task_id, task_step_id)
  SELECT id, from_member, kind, summary, body, options,
    status, created_ts, answered_ts, chat_message_id,
    answer_option_idx, answer_text, answer_attachments, task_id, task_step_id
  FROM reply_card;
DROP TABLE reply_card;
ALTER TABLE reply_card_rebuild RENAME TO reply_card;
CREATE INDEX idx_reply_card_status ON reply_card (status);

-- +goose Down
-- Reverse: drop expired_ts and restore the two-state status CHECK (another
-- rebuild). 'expired' will not exist after rollback, so squash any such rows
-- back to 'waiting' — the honest legacy reading: the ask was never answered,
-- so under the old model it is still waiting.
CREATE TABLE reply_card_rebuild (
    id                 TEXT PRIMARY KEY,
    from_member        TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL CHECK (kind IN ('decision', 'action')),
    summary            TEXT NOT NULL DEFAULT '',
    body               TEXT NOT NULL DEFAULT '',
    options            TEXT NOT NULL DEFAULT '[]',
    status             TEXT NOT NULL CHECK (status IN ('waiting', 'answered'))
                            DEFAULT 'waiting',
    created_ts         REAL NOT NULL DEFAULT 0.0,
    answered_ts        REAL NOT NULL DEFAULT 0.0,
    chat_message_id    TEXT NOT NULL DEFAULT '',
    answer_option_idx  INTEGER,
    answer_text        TEXT NOT NULL DEFAULT '',
    answer_attachments TEXT NOT NULL DEFAULT '[]',
    task_id            TEXT NOT NULL DEFAULT '',
    task_step_id       TEXT NOT NULL DEFAULT ''
);
INSERT INTO reply_card_rebuild (id, from_member, kind, summary, body, options,
    status, created_ts, answered_ts, chat_message_id,
    answer_option_idx, answer_text, answer_attachments, task_id, task_step_id)
  SELECT id, from_member, kind, summary, body, options,
    CASE WHEN status = 'expired' THEN 'waiting' ELSE status END,
    created_ts, answered_ts, chat_message_id,
    answer_option_idx, answer_text, answer_attachments, task_id, task_step_id
  FROM reply_card;
DROP TABLE reply_card;
ALTER TABLE reply_card_rebuild RENAME TO reply_card;
CREATE INDEX idx_reply_card_status ON reply_card (status);
