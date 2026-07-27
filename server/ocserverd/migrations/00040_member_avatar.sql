-- +goose Up
-- One stable member id owns at most one personal avatar pointer. The bytes
-- reuse chat_attachment's proven blob store, but avatar blobs are minted with
-- an ava- prefix and are never shared with chat messages. Empty means the
-- client follows the theme-avatar -> built-in-glyph fallback chain.
ALTER TABLE member ADD COLUMN avatar_attachment_id TEXT NOT NULL DEFAULT '';

-- +goose Down
-- Remove the dedicated bytes before dropping their only ownership pointer.
DELETE FROM chat_attachment
 WHERE id IN (
    SELECT avatar_attachment_id
      FROM member
     WHERE avatar_attachment_id != ''
 );
ALTER TABLE member DROP COLUMN avatar_attachment_id;
