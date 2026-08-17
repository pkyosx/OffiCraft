-- +goose Up
-- T-791e 系統互動 / 啟動程序 become EDITABLE (owner, 2026-08-13, verbatim:
-- 「我們可以把系統互動改成可以修改嗎 跟銀月的 insight 一樣是有 history / restore
-- to default」「不用每次都改 code」「啟動程序也是一樣」).
--
-- Until now the boot context's first and last blocks were `go:embed` seeds with
-- NO owner-editable representation at all: changing one word cost a release.
-- This table is the OVERLAY, exactly the shape role_insight has — a row means
-- "someone edited this block", tombstoned means "follow the shipped seed", and
-- no row at all means the same. The seed is never written to, so a reset can
-- always reach the factory text (that is the whole point of the overlay shape,
-- not an incidental property of it).
--
-- 🔴 THREE DOCUMENTS, THREE ROWS, NOTHING SHARED — and the reason is NOT that
-- the texts differ. The two boot sequences say the OPPOSITE thing in step 3:
-- the claude one tells the agent to run its own `ocagent listen`, the codex one
-- forbids exactly that because the App Server sidecar owns the listener. Serving
-- one where the other belongs is how a worker ends up unable to come online at
-- all (that already happened once — see bootSequenceSeedName in assets.go). So
-- the composite key is (doc_kind, doc_key):
--
--   ('system_interaction', 'global')
--   ('boot_sequence',      'claude')
--   ('boot_sequence',      'codex')
--
-- document_history uses those same two strings as its kind/key, and the key of
-- a boot_sequence row is the RUNTIME — chosen through bootSequenceSeedName, the
-- one place in the tree that decides which runtime gets which sequence.
--
-- NO DATA MOVE: an installation that has never edited anything has zero rows
-- here and reads byte-identical boot contexts to the one it read before.
CREATE TABLE boot_document (
  doc_kind   TEXT NOT NULL,
  doc_key    TEXT NOT NULL,
  text       TEXT NOT NULL DEFAULT '',
  tombstoned INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (doc_kind, doc_key)
);

-- +goose Down
-- Drop the overlay. Nothing is lost that the binary below this migration could
-- have read: every block falls back to the embedded seed, which is where it was
-- read from before Up ran. Edits written while the table existed go with it —
-- there is nowhere older to put them, because nowhere older ever held them.
DROP TABLE boot_document;
