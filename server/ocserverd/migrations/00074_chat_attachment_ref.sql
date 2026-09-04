-- +goose Up
-- =============================================================================
-- chat_attachment_ref — the gallery's index over chat_message.meta.attachments
--
-- WHAT KEEPS IT TRUE: three triggers on chat_message, below. NOT Go code.
-- That choice is deliberate; the reasoning is in the trigger block.
--
-- 🔴 NOT A LIVENESS SOURCE FOR THE BLOB STORE. DO NOT ADD IT TO
--    collectSurvivingBlobRefs (dal.go). It indexes EXACTLY ONE of the five
--    places a blob can be referenced from — chat_message.meta — and says
--    nothing about the other four (reply_card.answer_attachments,
--    reply_card.attachments, task_artifact.attachment_id, member avatars).
--    Wiring it into the liveness verdict re-opens T-62a8 (a MISSING source)
--    wearing a different shape. A test pins this: it seeds a row that ONLY
--    this table knows about and asserts the blob is still collected.
--
-- 🔴 sender/recipient/ts ARE A SNAPSHOT, AND THE ROW THEY COPY IS NOT
--    IMMUTABLE. putChatOn (dal.go:1207) is an UPSERT —
--        INSERT ... ON CONFLICT (id) DO UPDATE SET ..., meta = excluded.meta
--    so re-posting an existing id replaces the message wholesale, attachment
--    list included. `git grep "UPDATE chat_message"` finds ZERO hits and that
--    measurement is TRUE — it is simply blind to this, because the rewrite is
--    spelled as a conflict branch, not as an UPDATE statement. (O-206 caught
--    this in review after I had written the opposite here.) Measured: the
--    DO UPDATE branch DOES fire AFTER UPDATE triggers, so the hook covers it.
--
-- 🔴 THE ARRAY GUARD IS NOT DECORATION. Go reads this key with a type
--    assertion — `m.Meta["attachments"].([]any)` — so a non-array value is
--    silently skipped today. json_each does NOT filter that way: over a JSON
--    OBJECT it iterates happily and j.key comes back a STRING, which this
--    column would accept (SQLite type affinity) and `ord` would stop meaning
--    "which position". Every statement below therefore carries
--    the array check below, so SQL matches Go exactly
--    and nothing that is invisible today becomes visible. Measured on live
--    data 2026-09-04: 2,177 messages carry the key, 0 are not arrays, 0 have
--    a non-object element — today, and post_chat's own meta docs admit a
--    caller can store any shape under that key.
--
-- 🔴 AND THAT CHECK IS SPELLED AS A `CASE`, NOT AS A `WHERE`, FOR A REASON THAT
--    COST A RED CI. json_type() does not return some other type for MALFORMED
--    json — it RAISES: "SQL logic error: malformed JSON (1)". And SQLite's AND
--    does NOT short-circuit, so `WHERE json_valid(x) AND json_type(x,...)`
--    raises too. A raise inside an AFTER INSERT trigger FAILS THE INSERT, so a
--    message Go would have stored (its reader is a type assertion — non-JSON
--    meta is silently skipped) would instead be REFUSED. That is a behaviour
--    change in the worst possible direction, and it is reachable: meta is
--    free-form, and api_chat_reply_to_chat_t4e95_test.go:518 seeds exactly such
--    a row on purpose ("{this is not json") to pin an unrelated invariant.
--    CASE ... WHEN ... THEN ... ELSE DOES short-circuit (verified), so a
--    malformed row falls through to '[]' and stores nothing — exactly like Go.
--    Verified both ways on a throwaway DB: the WHERE form fails that INSERT
--    with rc=1 (that is what turned go-checks red on 7fa36cb5); the CASE form
--    stores the row with rc=0 and adds no index rows.
--
-- ord IS THE ATTACHMENT'S POSITION IN ITS MESSAGE, AND IT IS NOT DENSE.
--    It is json_each's 0-based array index, so a filtered-out element (empty
--    id) leaves a HOLE: a message whose refs are [{id:""},{id:"att-4"}] stores
--    exactly one row, at ord = 1. That is correct — ord means "where it sat in
--    the posted array", which is the order api_chat.go already promises to
--    preserve — but do not write code that assumes 0,1,2,... with no gaps.
--    It is the second half of the primary key BECAUSE (attachment_id,
--    message_id) is NOT safe: resolveChatAttachmentInputs (api_chat.go:457)
--    appends every item with no de-duplication, so one legal API call can post
--    the same blob id twice on one message. Measured live 2026-09-04: 0 such
--    rows out of 2,930 — today, not guarded.
--
-- ⚠️ THE THREE TRIGGERS ARE WRAPPED IN goose's StatementBegin/StatementEnd
--    have to be: goose splits a migration on semicolons, and a trigger body
--    CONTAINS semicolons, so without the markers goose hands SQLite a
--    truncated CREATE TRIGGER and the migration dies with
--    "SQL logic error: incomplete input (1)". Measured on a throwaway DB —
--    main-only binary to v70, then this binary against the SAME db: it
--    failed exactly that way before the markers were added. This is the
--    repo's FIRST migration with a trigger, so there was no precedent to copy.
--    (And do not spell those markers with their leading plus sign anywhere in
--    a comment: goose parses ANY comment line starting with that sequence as
--    an annotation and rejects the file with "invalid annotation". Measured
--    the same way — this file failed that way once too.)
-- =============================================================================
CREATE TABLE chat_attachment_ref (
    message_id    TEXT    NOT NULL,
    ord           INTEGER NOT NULL,
    attachment_id TEXT    NOT NULL,
    sender        TEXT    NOT NULL,
    recipient     TEXT    NOT NULL,
    ts            REAL    NOT NULL,
    mime          TEXT    NOT NULL DEFAULT '',
    filename      TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (message_id, ord)
) WITHOUT ROWID;

-- Column order matches the gallery's ORDER BY EXACTLY (ts DESC, message_id
-- ASC, ord ASC — the CURRENT display order, deliberately unchanged here).
-- Measured: a SINGLE-SIDED query is fully satisfied by these — no
-- "USE TEMP B-TREE FOR ORDER BY" in the plan.
-- 🔴 `sender = ? OR recipient = ?` does NOT get that: SQLite answers it with
--    MULTI-INDEX OR and then a TEMP B-TREE over every matching row — it throws
--    away the ordering the indexes already gave it. Issue TWO single-sided
--    queries and merge the two already-sorted streams, and put
--    `AND sender <> recipient` on the second, or a self-message comes back
--    twice (verified by seeding one: OR = 1 row, UNION ALL = 2).
CREATE INDEX idx_chat_attachment_ref_sender
    ON chat_attachment_ref(sender, ts DESC, message_id ASC, ord ASC);
CREATE INDEX idx_chat_attachment_ref_recipient
    ON chat_attachment_ref(recipient, ts DESC, message_id ASC, ord ASC);

-- NOT for the gallery, and NOT load-bearing today. 🔴 THERE IS NO READER: at the
-- time this landed, `git grep chat_attachment_ref` over the Go tree found the
-- gallery read path and nothing keyed on attachment_id — the only such query
-- lives in dal_blob_liveness_test.go. The single DELETE is the trigger
-- chat_attachment_ref_ad, which is keyed on message_id, not this column; blob
-- collection goes through DeleteChatInvolving and deletes chat_attachment.
--
-- It is kept for the blob-collection path that will want it, at the cost of one
-- index maintained on every write. An earlier revision of this comment claimed
-- it "serves the DELETE path" and said "do not drop it as unused" — that
-- asserted a path that did not exist AND pre-emptively waved off the check that
-- would have caught it. Replaced with what was actually measured.
--
-- ⇒ WHEN TO REVISIT: next time anyone touches this schema, re-run that grep. If
-- there is still no production reader keyed on attachment_id, drop the index.
CREATE INDEX idx_chat_attachment_ref_attachment
    ON chat_attachment_ref(attachment_id);

-- ── the write path ──────────────────────────────────────────────────────────
-- 🔴 WHY TRIGGERS AND NOT GO. A Go hook in putChatOn would be correct only
--    while every writer goes through that seam, and the day one does not
--    there is NO SIGNAL. Worse, it would not even be atomic on the main path:
--    putChatOn's three direct callers are PutChat (d.wdb — NOT in a tx) and
--    two tx callers, so on the PutChat path the upsert and the index writes
--    would be separate autocommitted statements. The wdb pool caps at one
--    connection, but that buys SERIALISATION, not atomicity. A trigger runs
--    inside the triggering statement, so it is atomic whatever the caller
--    does, and it cannot be bypassed.
--    The cost, stated plainly: triggers are INVISIBLE from the Go side. That
--    is covered by a test that INSERTs into chat_message directly and asserts
--    the index row appears — if that passes, no Go code is involved.
--
-- 🔴 WHY BARE `AFTER UPDATE` AND NOT `AFTER UPDATE OF ...` OR A `WHEN` CLAUSE.
--    This table snapshots FOUR columns (sender, recipient, ts, and the
--    attachment list inside meta). Narrowing the trigger to `OF meta` — or
--    `WHEN old.meta IS NOT new.meta` — makes a row that changes ts or sender
--    without touching meta go stale SILENTLY. Any correct narrowing must list
--    all four, and that list would then have to be kept in sync with the
--    table's columns with NOTHING reporting when they drift apart. Rebuilding
--    on every update is wasted work; a narrowing that can silently skip an
--    update is a defect. Take the wasted work.
-- +goose StatementBegin
CREATE TRIGGER chat_attachment_ref_ai AFTER INSERT ON chat_message
BEGIN
    INSERT INTO chat_attachment_ref
        (message_id, ord, attachment_id, sender, recipient, ts, mime, filename)
    SELECT NEW.id, j.key, json_extract(j.value, '$.id'),
           NEW.sender, NEW.recipient, NEW.ts,
           COALESCE(json_extract(j.value, '$.mime'), ''),
           COALESCE(json_extract(j.value, '$.filename'), '')
    FROM json_each(
        CASE WHEN json_valid(NEW.meta)
              AND json_type(NEW.meta, '$.attachments') = 'array'
             THEN json_extract(NEW.meta, '$.attachments')
             ELSE '[]'
        END
    ) j
    -- 🔴 THE ELEMENT'S SHAPE IS PART OF THE GUARD, AND THE `CASE` IS WHY IT
    -- HOLDS. `meta.attachments` is copied wholesale from the caller, so an
    -- element can be ANY JSON value. Measured, with the project's own driver:
    --   json_type(j.value)              on a text element -> malformed JSON
    --   json_type(j.value,'$.id')       on a text element -> malformed JSON
    -- so the obvious `json_type(j.value)='object'` guard RAISES BEFORE IT CAN
    -- REJECT. Two things make this form safe. `j.type` is json_each's own
    -- column and never parses. And CASE evaluates its WHENs in order and stops
    -- — measured: `j.type='object' AND json_type(...)` survives, the SAME two
    -- terms written the other way round raises, so AND's short-circuit here is
    -- an evaluation-order accident, not a guarantee. Do not flatten this back
    -- into an AND.
    -- Scope of the line above: flattening this into the WRONG order is caught by
    -- the parity case carrying a bare string element (the write fails outright).
    -- Flattening it into the RIGHT order is NOT caught by anything — it behaves
    -- identically today and only puts us back on evaluation-order luck. That
    -- half is this sentence and nothing else.
    -- Requiring '$.id' to be TEXT also restores what the pre-index Go handler
    -- did: its type assertion dropped a non-string id, where a bare extract
    -- would store `123` and the panel would serve /api/chat/attachment/123.
    WHERE CASE WHEN j.type <> 'object'                   THEN ''
               WHEN json_type(j.value, '$.id') <> 'text' THEN ''
               ELSE json_extract(j.value, '$.id') END <> '';
END;
-- +goose StatementEnd

-- DELETE-then-INSERT: the upsert replaces meta wholesale, so the previous
-- rows must go. This also means we never bet on "nobody re-uses an id".
-- 🔴 `IN (OLD.id, NEW.id)`, NOT `= NEW.id`. Today they are always equal — the
--    upsert's DO UPDATE SET does not touch id — but an UPDATE that DID change
--    the id would leave the OLD id's rows behind forever while the new id's
--    rows are inserted alongside them: orphans pointing at a message that no
--    longer goes by that name. Today's equality is a fact about today, not a
--    guard. Naming both costs nothing while they are equal. (O-206, review.)
-- +goose StatementBegin
CREATE TRIGGER chat_attachment_ref_au AFTER UPDATE ON chat_message
BEGIN
    DELETE FROM chat_attachment_ref WHERE message_id IN (OLD.id, NEW.id);
    INSERT INTO chat_attachment_ref
        (message_id, ord, attachment_id, sender, recipient, ts, mime, filename)
    SELECT NEW.id, j.key, json_extract(j.value, '$.id'),
           NEW.sender, NEW.recipient, NEW.ts,
           COALESCE(json_extract(j.value, '$.mime'), ''),
           COALESCE(json_extract(j.value, '$.filename'), '')
    FROM json_each(
        CASE WHEN json_valid(NEW.meta)
              AND json_type(NEW.meta, '$.attachments') = 'array'
             THEN json_extract(NEW.meta, '$.attachments')
             ELSE '[]'
        END
    ) j
    -- 🔴 THE ELEMENT'S SHAPE IS PART OF THE GUARD, AND THE `CASE` IS WHY IT
    -- HOLDS. `meta.attachments` is copied wholesale from the caller, so an
    -- element can be ANY JSON value. Measured, with the project's own driver:
    --   json_type(j.value)              on a text element -> malformed JSON
    --   json_type(j.value,'$.id')       on a text element -> malformed JSON
    -- so the obvious `json_type(j.value)='object'` guard RAISES BEFORE IT CAN
    -- REJECT. Two things make this form safe. `j.type` is json_each's own
    -- column and never parses. And CASE evaluates its WHENs in order and stops
    -- — measured: `j.type='object' AND json_type(...)` survives, the SAME two
    -- terms written the other way round raises, so AND's short-circuit here is
    -- an evaluation-order accident, not a guarantee. Do not flatten this back
    -- into an AND.
    -- Scope of the line above: flattening this into the WRONG order is caught by
    -- the parity case carrying a bare string element (the write fails outright).
    -- Flattening it into the RIGHT order is NOT caught by anything — it behaves
    -- identically today and only puts us back on evaluation-order luck. That
    -- half is this sentence and nothing else.
    -- Requiring '$.id' to be TEXT also restores what the pre-index Go handler
    -- did: its type assertion dropped a non-string id, where a bare extract
    -- would store `123` and the panel would serve /api/chat/attachment/123.
    WHERE CASE WHEN j.type <> 'object'                   THEN ''
               WHEN json_type(j.value, '$.id') <> 'text' THEN ''
               ELSE json_extract(j.value, '$.id') END <> '';
END;
-- +goose StatementEnd

-- Replaces the "DeleteChatInvolving must delete in the same tx" contract: it
-- is now structural. ⚠️ It fires PER ROW, and DeleteChatInvolving deletes in
-- bulk — the per-row cost is one indexed delete (message_id is the PK's first
-- column), but NOBODY HAS MEASURED that path at its real batch size.
-- +goose StatementBegin
CREATE TRIGGER chat_attachment_ref_ad AFTER DELETE ON chat_message
BEGIN
    DELETE FROM chat_attachment_ref WHERE message_id = OLD.id;
END;
-- +goose StatementEnd

-- ── backfill ────────────────────────────────────────────────────────────────
-- Runs inside runMigrations at boot, before the server serves — no concurrent
-- writer, so no race with the triggers.
-- 🔴 THE ACCEPTANCE CHECK IS AN EQUATION, NOT A COUNT. Any fixed number goes
--    stale the moment anyone posts an attachment — including the messages in
--    which we agreed on the number (measured: it moved 2,927 → 2,930 in two
--    hours, and all three were files sent while designing this table). After
--    migrating, on the same DB, this must return 1:
--      SELECT (SELECT count(*) FROM chat_attachment_ref)
--           = (SELECT count(*) FROM chat_message m, json_each(
--                  CASE WHEN json_valid(m.meta)
--                        AND json_type(m.meta,'$.attachments') = 'array'
--                       THEN json_extract(m.meta,'$.attachments')
--                       ELSE '[]' END) j
--              WHERE CASE WHEN j.type <> 'object'                   THEN ''
--                          WHEN json_type(j.value,'$.id') <> 'text' THEN ''
--                          ELSE json_extract(j.value,'$.id') END <> '');
--    ⚠️ THE `CASE` IS PART OF THE CHECK, NOT DECORATION. Copy this verbatim.
--    An earlier revision of this comment left the bare json_extract here after
--    the triggers moved to CASE; run THAT and it dies with "malformed JSON" on
--    the first corrupt row — and the person running it is VERIFYING a
--    migration, i.e. exactly the moment they will blame the migration rather
--    than the command. (O-206 caught it by running the comment.)
INSERT INTO chat_attachment_ref
    (message_id, ord, attachment_id, sender, recipient, ts, mime, filename)
SELECT m.id, j.key, json_extract(j.value, '$.id'), m.sender, m.recipient, m.ts,
       COALESCE(json_extract(j.value, '$.mime'), ''),
       COALESCE(json_extract(j.value, '$.filename'), '')
FROM chat_message m, json_each(
    CASE WHEN json_valid(m.meta)
          AND json_type(m.meta, '$.attachments') = 'array'
         THEN json_extract(m.meta, '$.attachments')
         ELSE '[]'
    END
) j
WHERE CASE WHEN j.type <> 'object'                   THEN ''
           WHEN json_type(j.value, '$.id') <> 'text' THEN ''
           ELSE json_extract(j.value, '$.id') END <> '';

-- +goose Down
-- 🔴 DROP THE TRIGGERS FIRST. They live on chat_message, so DROP TABLE does
--    NOT take them with it: roll back without this and the triggers survive
--    pointing at a table that is gone, so THE VERY NEXT MESSAGE INSERT FAILS
--    and chat stops working entirely. Verified both ways on a throwaway DB:
--    without these three lines the next INSERT dies with
--    "no such table: main.chat_attachment_ref" (rc=1); with them it succeeds
--    (rc=0). This only bites during a rollback — i.e. while something is
--    already going wrong. (O-206 caught this in review.)
DROP TRIGGER IF EXISTS chat_attachment_ref_ai;
DROP TRIGGER IF EXISTS chat_attachment_ref_au;
DROP TRIGGER IF EXISTS chat_attachment_ref_ad;
DROP TABLE IF EXISTS chat_attachment_ref;
