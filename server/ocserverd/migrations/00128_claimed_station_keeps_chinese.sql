-- +goose Up
-- An unset display.language now reads as English. A station that was already
-- running read it as Chinese, so it is pinned to zh here and keeps what its
-- owner has been seeing. "Already running" means the owner password is set:
-- on a fresh install migrate runs before anyone claims the station, and a
-- station installed but never claimed must stay unset so the assistant still
-- asks the owner which language to use.
INSERT INTO setting (key, value, updated_at)
SELECT 'display.language', 'zh', 0
WHERE COALESCE((SELECT value FROM setting WHERE key = 'auth.password_hash'), '') <> ''
  AND COALESCE((SELECT value FROM setting WHERE key = 'display.language'), '') = ''
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- +goose Down
-- Nothing to undo: an older binary reads zh exactly as it read unset.
SELECT 1;
