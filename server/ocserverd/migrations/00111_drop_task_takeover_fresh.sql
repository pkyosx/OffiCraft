-- +goose Up
-- T-239 retired the boot document kind 'task_takeover_fresh' (〈新任務〉): a staff
-- successor now always receives 'task_takeover_with_predecessor'. The server no
-- longer registers the kind, so an owner edit of it and its retained revisions
-- are rows nothing can read, list, restore or delete through the API.
-- Exact equality on the kind, the same fail-closed shape as 00045 and 00105:
-- every other boot document and document_history kind is untouched.
DELETE FROM document_history
 WHERE document_kind = 'task_takeover_fresh';

DELETE FROM boot_document
 WHERE doc_kind = 'task_takeover_fresh';

-- +goose Down
-- NOT REVERSIBLE. The Up kept no copy of the deleted rows. An older binary that
-- still registers the kind reads its shipped seed when no overlay row exists, so
-- no schema has to come back; the rollback is an explicit no-op.
SELECT 1;
