-- +goose Up
-- K1 one-shot data fix: every existing task_manual field marked is_key must
-- also be required (a key that can be left empty has no dedupe basis — the same
-- invariant the create/update-manual write path now enforces going forward).
-- Rebuild each affected manual's fields JSON, flipping required→true ONLY on
-- is_key elements and leaving every other element byte-for-byte equivalent.
--
-- Safety:
--   * json_valid(fields) guard — a malformed blob is skipped, never errors the
--     migration (ParseManualFields treats such a blob as corruption anyway).
--   * json('true') inserts a real JSON boolean (SQLite's bare `true` is integer
--     1, which would deserialize into Go's bool field as an error).
--   * idempotent — the EXISTS filter matches only manuals that still have an
--     is_key field whose required is not already true, so a second run is a
--     no-op.
UPDATE task_manual
SET fields = (
    SELECT json_group_array(
        CASE
            WHEN json_extract(value, '$.is_key') IN (1, 'true')
                THEN json_set(value, '$.required', json('true'))
            ELSE json(value)
        END
    )
    FROM json_each(task_manual.fields)
)
WHERE json_valid(fields)
    AND EXISTS (
        SELECT 1
        FROM json_each(task_manual.fields)
        WHERE json_extract(value, '$.is_key') IN (1, 'true')
            AND (json_extract(value, '$.required') IS NULL
                 OR json_extract(value, '$.required') IN (0, 'false'))
    );

-- +goose Down
-- Irreversible: the original per-field required flags were not recorded, so the
-- flip cannot be safely undone. Intentional no-op.
SELECT 1;
