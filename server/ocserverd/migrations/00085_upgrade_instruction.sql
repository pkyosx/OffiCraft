-- +goose Up
-- T-79 — upgrade_instruction: the owner's standing instructions to the
-- assistant, handed over at station upgrade time and cleared by the assistant
-- ticking them off.
--
-- WHY A ROW AND NOT A MESSAGE. The first cut of T-79 sent the assistant a
-- generated message at every upgrade: what changed, which files, a compare
-- link. The owner rejected it twice, and the second rejection named the missing
-- half exactly — "換版通知，我是要我們可以指定這次換版要送什麼訊息給特助的功能，
-- 不是固定訊息". A generated message answers "what changed"; it cannot carry
-- "here is what I want you to DO about it", which is the thing he asked for
-- first. He then fixed the shape himself: "交代單在db 中 是一筆筆 record 執行完
-- 以後 Mira會打勾" (2026-09-05). So the unit is a ROW with a completion state,
-- not a message: a message is consumed by whoever happens to read it and
-- nothing records that the work behind it ever happened.
--
-- WHY DELIVERY IS NOT BOUND TO A COMMIT. The earlier design proposal bound each
-- instruction to the commit it was written for, so only the upgrade actually
-- carrying that commit would deliver it. Its whole justification was that
-- binding to "the next upgrade" would hand the instruction to an unrelated one:
-- measured on 2026-09-05, this station upgraded NINE times in a day, every one
-- of them silent. That justification dies the moment an un-ticked row persists.
-- An instruction handed to an unrelated upgrade is not lost — it is still open,
-- and it is handed over again at the next one, and the one after that, until
-- somebody ticks it. So delivery is simply "every open instruction, every
-- upgrade", and the owner may write one whenever he likes without having to
-- reason about which upgrade will pick it up.
--
-- WHY done IS A FLAG AND NOT A DELETE. Two readers need it. The assistant needs
-- to stop being handed work she has already done, which a delete would also
-- give. The owner needs to see how many instructions are still waiting — the
-- one failure mode this design has is an instruction that is never acted on,
-- and without a visible open count that failure is completely silent. done_by /
-- done_ts are here because "it was ticked" and "who ticked it, when" are not
-- the same fact, and only the second one survives a disagreement.
--
-- WHO MAY WRITE WHAT is a handler concern, not a schema one, and is deliberately
-- NOT expressed here: SQLite cannot check a caller identity, and a CHECK
-- constraint naming a member id would be a second, silently diverging copy of a
-- rule that already lives in one place. The identity a handler acts on comes
-- from the verified token's sub (currentActor), never from the request body —
-- server/CLAUDE.md.
--
-- 🔴 NUMBER: 00085, and it is deliberately NOT one of the gaps below it.
-- Swept 2026-09-05 across BOTH sources (migrations/*.sql AND
-- goose.AddNamedMigrationContext) over EVERY remote branch, not just main:
-- main's max was 00080, and the in-flight T-33 branches held 00081/00082/00083
-- (t-33/lore-land, t-33/lore-format-v8) plus 00084 (t-33/lore-format-v8), so
-- 00085 is the smallest safe number. Confirmed by the OffiCraft developer, who
-- re-ran the sweep himself and allocated 00085 to this ticket.
--
-- 🔴 THIS FILE MUST LAND AFTER T-33, AND THAT IS MEASURED, NOT ASSUMED. The
-- production database has 00081/00082/00083 recorded as APPLIED while those
-- files exist only on the T-33 branches (a known mis-run, tracked elsewhere).
-- Measured on goose v3.27.2 + modernc.org/sqlite v1.53.0, calling goose exactly
-- as migrate.go does and with no allowMissing:
--   * a version the database calls applied whose FILE is absent is ignored —
--     this file lands fine on today's production state, exit 0;
--   * a file whose version is BELOW the database's current version and was
--     never applied FAILS — "found 1 missing migrations before current version
--     85: version 84" — and it does not heal: the database stays at 85, 00084
--     stays unapplied, and every subsequent start hits the same error.
-- So if this file landed before T-33, T-33's 00084 arriving afterwards would
-- stop the station from starting, permanently. Raw output of all four
-- conditions (including the positive and negative controls, and the six things
-- the experiment did NOT measure) is pinned on T-79 as an artifact. Land order
-- is the OffiCraft developer's call and he holds it.
CREATE TABLE upgrade_instruction (
    id         TEXT PRIMARY KEY,
    body       TEXT NOT NULL,
    created_ts REAL NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    done       INTEGER NOT NULL DEFAULT 0,
    done_ts    REAL NOT NULL DEFAULT 0,
    done_by    TEXT NOT NULL DEFAULT ''
);
-- The only hot read is "the open ones, oldest first" — the upgrade hand-over
-- and the cockpit's waiting count are the same query. done leads the index so
-- that read never touches a finished row.
CREATE INDEX idx_upgrade_instruction_open
    ON upgrade_instruction (done, created_ts);

-- +goose Down
DROP TABLE upgrade_instruction;
