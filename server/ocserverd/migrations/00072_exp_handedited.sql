-- +goose Up
-- EXPERIMENT (T75 migration-lock number-collision test, arm 5): a migration
-- deliberately numbered BELOW the tree's current max (00080) into an empty gap
-- (00072), added by HAND-EDITING migration.lock instead of running
-- bin/gen-migration-lock, to test whether bypassing the generator and
-- inserting the entry into its sorted position defeats the append-only
-- ordering check. Disposable — never intended to ship.
CREATE TABLE exp_handedited_marker (
    id INTEGER PRIMARY KEY,
    note TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE exp_handedited_marker;
