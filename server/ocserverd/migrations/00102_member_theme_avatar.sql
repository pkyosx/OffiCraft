-- +goose Up
-- Theme-linked avatar identity. A member's face is a CHOICE inside one theme,
-- so the row is an association, not a column on member: one member holds at
-- most one choice per theme, and the themes never overwrite each other.
--
-- The association is SPARSE on purpose. A member with no row here has made no
-- explicit choice in that theme, and the client renders the first image of the
-- matching pool. That first render is NOT written back; only an owner pick
-- creates a row. This is what makes "the default" distinguishable from "the
-- owner chose the first image", which a NOT NULL column could never express.
--
-- icon_id is a STABLE image identity, never an array position. A position
-- would silently rebind a member to a different face when another pool item is
-- removed, and would collide two members onto one face.
CREATE TABLE member_theme_avatar (
    member_id TEXT NOT NULL,
    theme_id  TEXT NOT NULL,
    icon_id   TEXT NOT NULL,
    PRIMARY KEY (member_id, theme_id)
);

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
DROP TABLE member_theme_avatar;
