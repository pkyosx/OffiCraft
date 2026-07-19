-- +goose Up
-- M4 webhook 可觀測性: per-endpoint delivery counters so the owner can see
-- whether an endpoint is alive WITHOUT weakening the /in inlet's deliberate
-- silent-200 face (the HTTP response stays byte-identical for every outcome;
-- only the DB + the owner-facing DTO learn anything).
--
-- last_received_ts is stamped by ANY /in request that resolves to this token
-- (delivered, dropped, challenge/ping alike). delivered_count counts payloads
-- that passed verification AND landed as a chat; dropped_count counts
-- non-deliverable calls, with last_drop_reason holding a coarse closed-set
-- classification ('sig_failed' | 'disabled' | 'member_gone'). An unknown token
-- has no row to count against, by construction. All columns default to the
-- zero value — no back-fill, pre-existing rows read as "never received".
ALTER TABLE webhook_endpoint ADD COLUMN last_received_ts REAL NOT NULL DEFAULT 0.0;
ALTER TABLE webhook_endpoint ADD COLUMN delivered_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE webhook_endpoint ADD COLUMN dropped_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE webhook_endpoint ADD COLUMN last_drop_reason TEXT NOT NULL DEFAULT '';

-- webhook_request_log — the per-endpoint debug ring buffer: the last 5 raw
-- requests /in resolved to an endpoint token, whatever the outcome
-- ('delivered' | 'dropped:<reason>' | 'challenge' | 'ping'). headers is the
-- JSON-serialised request header map (truncated at 4 KiB), body the raw
-- payload text (truncated at 16 KiB); truncated=1 marks that either was cut.
-- The DAL trims to the newest 5 rows per token on every insert; deleting an
-- endpoint deletes its log rows (token references webhook_endpoint's PK).
-- Owner-facing debug data only — never on any public or agent wire.
CREATE TABLE webhook_request_log (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    token     TEXT NOT NULL REFERENCES webhook_endpoint (token),
    ts        REAL NOT NULL,
    outcome   TEXT NOT NULL,
    headers   TEXT NOT NULL DEFAULT '',
    body      TEXT NOT NULL DEFAULT '',
    truncated INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_webhook_request_log_token ON webhook_request_log (token, id);

-- +goose Down
DROP TABLE IF EXISTS webhook_request_log;
ALTER TABLE webhook_endpoint DROP COLUMN last_drop_reason;
ALTER TABLE webhook_endpoint DROP COLUMN dropped_count;
ALTER TABLE webhook_endpoint DROP COLUMN delivered_count;
ALTER TABLE webhook_endpoint DROP COLUMN last_received_ts;
