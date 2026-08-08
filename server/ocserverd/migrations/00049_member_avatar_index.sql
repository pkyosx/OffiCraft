-- +goose Up
-- Theme-linked avatar identity: every staff/outsource member stores only a
-- stable non-negative slot. The active theme supplies the actual image.
ALTER TABLE member
    ADD COLUMN avatar_index INTEGER NOT NULL DEFAULT 0
    CHECK (avatar_index >= 0);

-- The rejected personal-avatar model owned these dedicated ava- blobs through
-- member.avatar_attachment_id. Delete the bytes before dropping their sole
-- pointer so the migration leaves no orphan attachments.
DELETE FROM chat_attachment
 WHERE id IN (
    SELECT avatar_attachment_id
      FROM member
     WHERE avatar_attachment_id LIKE 'ava-%'
 );
ALTER TABLE member DROP COLUMN avatar_attachment_id;

-- +goose Down
-- Personal image bytes were intentionally removed by Up and cannot be
-- reconstructed. Restore a valid empty legacy pointer, never a dangling one.
ALTER TABLE member
    ADD COLUMN avatar_attachment_id TEXT NOT NULL DEFAULT '';
ALTER TABLE member DROP COLUMN avatar_index;
