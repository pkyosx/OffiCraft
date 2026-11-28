-- +goose Up
-- When the entry was moved into retired (epoch seconds); 0 = not retired, or
-- retired before this column existed — those rows stay 0 because no record
-- says when they were retired.
ALTER TABLE lore_entry ADD COLUMN retired_ts REAL NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE lore_entry DROP COLUMN retired_ts;
