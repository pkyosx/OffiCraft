-- +goose Up
-- The passage of the card's question an answer quotes. '' = the answer quotes
-- nothing, which is what every answer stored before this column honestly did.
ALTER TABLE reply_card ADD COLUMN answer_reply_excerpt TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE reply_card DROP COLUMN answer_reply_excerpt;
