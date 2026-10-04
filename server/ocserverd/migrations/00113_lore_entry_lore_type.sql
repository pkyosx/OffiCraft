-- +goose Up
-- lore_type: an entry's type tag, always one of the five (owner rc-8ff3a3d41a26:
-- an entry with no type is other). Entries written before the tag existed carried
-- the type as a title prefix; those are moved onto the tag here.
-- ADD COLUMN gives every existing row the DEFAULT, so every row starts as other.
ALTER TABLE lore_entry ADD COLUMN lore_type TEXT NOT NULL DEFAULT 'other'
    CHECK (lore_type IN ('instruction_conflict', 'instruction_supplement',
                         'owner_decision', 'owner_preference', 'other'));

-- Every title this migration rewrites, as it was, so the Down can put it back.
-- An entry has no edit path, so this table is the only copy of the old titles.
CREATE TABLE lore_entry_title_backup_00113 (
    id    TEXT PRIMARY KEY,
    title TEXT NOT NULL
);

-- 🔴 Prefix tests are substr() against the literal, never LIKE: '_' and '%' are
-- wildcards there, and only a title that STARTS with the prefix is tagged.
INSERT INTO lore_entry_title_backup_00113 (id, title)
  SELECT id, title FROM lore_entry
  WHERE substr(title, 1, length('[指示衝突] ')) = '[指示衝突] '
     OR substr(title, 1, length('[指示補充] ')) = '[指示補充] '
     OR substr(title, 1, length('[Owner 決策] ')) = '[Owner 決策] '
     OR substr(title, 1, length('[Owner 偏好] ')) = '[Owner 偏好] ';

-- Each UPDATE matches the ORIGINAL title in the backup, not the live one, so a
-- title that begins with a second prefix once the first is stripped is tagged and
-- stripped once. [工作原則] and every other title are left as other, title intact
-- (owner: 「工作原則可以不動前綴，只是label要標成其他」).
UPDATE lore_entry
  SET lore_type = 'instruction_conflict',
      title = substr(title, length('[指示衝突] ') + 1)
  WHERE id IN (SELECT id FROM lore_entry_title_backup_00113
               WHERE substr(title, 1, length('[指示衝突] ')) = '[指示衝突] ');

UPDATE lore_entry
  SET lore_type = 'instruction_supplement',
      title = substr(title, length('[指示補充] ') + 1)
  WHERE id IN (SELECT id FROM lore_entry_title_backup_00113
               WHERE substr(title, 1, length('[指示補充] ')) = '[指示補充] ');

UPDATE lore_entry
  SET lore_type = 'owner_decision',
      title = substr(title, length('[Owner 決策] ') + 1)
  WHERE id IN (SELECT id FROM lore_entry_title_backup_00113
               WHERE substr(title, 1, length('[Owner 決策] ')) = '[Owner 決策] ');

UPDATE lore_entry
  SET lore_type = 'owner_preference',
      title = substr(title, length('[Owner 偏好] ') + 1)
  WHERE id IN (SELECT id FROM lore_entry_title_backup_00113
               WHERE substr(title, 1, length('[Owner 偏好] ')) = '[Owner 偏好] ');

-- +goose Down
-- Restores the backed-up titles only. An entry written with a tag after the Up
-- carries no prefix and loses its tag here.
UPDATE lore_entry
  SET title = (SELECT b.title FROM lore_entry_title_backup_00113 b WHERE b.id = lore_entry.id)
  WHERE id IN (SELECT id FROM lore_entry_title_backup_00113);

ALTER TABLE lore_entry DROP COLUMN lore_type;

DROP TABLE lore_entry_title_backup_00113;
