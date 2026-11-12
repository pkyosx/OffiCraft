-- +goose Up
-- SQLite cannot widen a CHECK in place, hence the rebuild. Nothing but the
-- provide, reveal and destroy code may read reply_card_secret: its ciphertext
-- never travels on a card row.
CREATE TABLE reply_card_rebuild (
    id                   TEXT PRIMARY KEY,
    from_member          TEXT NOT NULL DEFAULT '',
    kind                 TEXT NOT NULL CHECK (kind IN ('decision', 'action', 'secret')),
    summary              TEXT NOT NULL DEFAULT '',
    body                 TEXT NOT NULL DEFAULT '',
    options              TEXT NOT NULL DEFAULT '[]',
    select_mode          TEXT NOT NULL DEFAULT 'single',
    status               TEXT NOT NULL DEFAULT 'waiting',
    created_ts           REAL NOT NULL DEFAULT 0.0,
    answered_ts          REAL NOT NULL DEFAULT 0.0,
    expired_ts           REAL NOT NULL DEFAULT 0.0,
    chat_message_id      TEXT NOT NULL DEFAULT '',
    answer_option_idxs   TEXT,
    answer_text          TEXT NOT NULL DEFAULT '',
    answer_attachments   TEXT NOT NULL DEFAULT '[]',
    attachments          TEXT NOT NULL DEFAULT '[]',
    task_id              TEXT NOT NULL DEFAULT '',
    task_step_id         TEXT NOT NULL DEFAULT '',
    answer_reply_excerpt TEXT NOT NULL DEFAULT '',
    snoozed_until_ts     REAL NOT NULL DEFAULT 0.0,
    secret_provided_ts   REAL NOT NULL DEFAULT 0,
    secret_retrieved_ts  REAL NOT NULL DEFAULT 0,
    -- 0 means no ciphertext is scheduled for destruction.
    secret_destroy_after_ts REAL NOT NULL DEFAULT 0
);
INSERT INTO reply_card_rebuild (id, from_member, kind, summary, body, options,
    select_mode, status, created_ts, answered_ts, expired_ts, chat_message_id,
    answer_option_idxs, answer_text, answer_attachments, attachments,
    task_id, task_step_id, answer_reply_excerpt, snoozed_until_ts)
  SELECT id, from_member, kind, summary, body, options,
    select_mode, status, created_ts, answered_ts, expired_ts, chat_message_id,
    answer_option_idxs, answer_text, answer_attachments, attachments,
    task_id, task_step_id, answer_reply_excerpt, snoozed_until_ts
  FROM reply_card;
DROP TABLE reply_card;
ALTER TABLE reply_card_rebuild RENAME TO reply_card;
CREATE INDEX idx_reply_card_status ON reply_card (status);

-- 🔴 Same sealing as the secret table: AES-256-GCM under <data dir>/vault.key,
-- AAD = card_id, so a database copy alone cannot open a value and a row cannot
-- be moved onto another card.
CREATE TABLE reply_card_secret (
    card_id     TEXT PRIMARY KEY,
    nonce       BLOB NOT NULL,
    value_ct    BLOB NOT NULL,
    key_version INTEGER NOT NULL DEFAULT 1
);

-- +goose Down
-- ⚠️ Lossy: secret cards come back as 'action' cards and every sealed value is
-- gone. Rolling forward again does not restore either.
DROP TABLE reply_card_secret;
CREATE TABLE reply_card_rebuild (
    id                   TEXT PRIMARY KEY,
    from_member          TEXT NOT NULL DEFAULT '',
    kind                 TEXT NOT NULL CHECK (kind IN ('decision', 'action')),
    summary              TEXT NOT NULL DEFAULT '',
    body                 TEXT NOT NULL DEFAULT '',
    options              TEXT NOT NULL DEFAULT '[]',
    select_mode          TEXT NOT NULL DEFAULT 'single',
    status               TEXT NOT NULL DEFAULT 'waiting',
    created_ts           REAL NOT NULL DEFAULT 0.0,
    answered_ts          REAL NOT NULL DEFAULT 0.0,
    expired_ts           REAL NOT NULL DEFAULT 0.0,
    chat_message_id      TEXT NOT NULL DEFAULT '',
    answer_option_idxs   TEXT,
    answer_text          TEXT NOT NULL DEFAULT '',
    answer_attachments   TEXT NOT NULL DEFAULT '[]',
    attachments          TEXT NOT NULL DEFAULT '[]',
    task_id              TEXT NOT NULL DEFAULT '',
    task_step_id         TEXT NOT NULL DEFAULT '',
    answer_reply_excerpt TEXT NOT NULL DEFAULT '',
    snoozed_until_ts     REAL NOT NULL DEFAULT 0.0
);
INSERT INTO reply_card_rebuild (id, from_member, kind, summary, body, options,
    select_mode, status, created_ts, answered_ts, expired_ts, chat_message_id,
    answer_option_idxs, answer_text, answer_attachments, attachments,
    task_id, task_step_id, answer_reply_excerpt, snoozed_until_ts)
  SELECT id, from_member,
    CASE WHEN kind = 'secret' THEN 'action' ELSE kind END,
    summary, body, options,
    select_mode, status, created_ts, answered_ts, expired_ts, chat_message_id,
    answer_option_idxs, answer_text, answer_attachments, attachments,
    task_id, task_step_id, answer_reply_excerpt, snoozed_until_ts
  FROM reply_card;
DROP TABLE reply_card;
ALTER TABLE reply_card_rebuild RENAME TO reply_card;
CREATE INDEX idx_reply_card_status ON reply_card (status);
