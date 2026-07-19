-- +goose Up
-- T-5e8a: question-side attachments on a reply card (開卡帶附件). The column
-- holds the SAME light-ref JSON shape answer_attachments already holds —
-- [{id, mime, filename}] refs into the shared chat_attachment blob store (one
-- blob mechanism, not two; the card row never carries bytes). Pure additive:
-- NOT NULL DEFAULT '[]', no back-fill — every pre-existing card honestly reads
-- "no question attachments", and an older server ignores the extra column.
ALTER TABLE reply_card ADD COLUMN attachments TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE reply_card DROP COLUMN attachments;
