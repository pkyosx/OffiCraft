-- +goose Up
-- T-53 — the ACCOUNT's own spend accumulator (owner ruling rc-5c5d7c7c6dcd,
-- 2026-09-02, option 0「分開：帳號卡自己一份數字，清它不動成員」).
--
-- WHY A TABLE AT ALL. Until this ruling the account card's 估計$ was DERIVED:
-- a fold that summed the live+banked figures of whichever actors were on the
-- account right now. The owner asked for the account figure and the per-member
-- figure to be clearable INDEPENDENTLY, and a derived number cannot be cleared
-- without clearing what it derives from. So the account gets a number of its
-- own, and this is where it lives. The account dimension exists nowhere else in
-- the database — it is a free telemetry string — which is also why the rows
-- here cannot be seeded from anything: see the deploy note below.
--
-- WHY AN ACCUMULATOR RATHER THAN 「the sum, minus what you cleared」, which is
-- the cheaper shape and was the first design: an actor's spend can LEAVE the
-- sum (removing a member hard-deletes the row AND its telemetry entry; the
-- per-actor reset clears one on purpose), and the sum would then sit below the
-- cleared watermark. The card would read 「沒花錢」 while spending continued, for
-- as long as it took the sum to climb back — silently under-reporting with
-- nothing anywhere to flag it. An accumulator cannot enter that state: money
-- already spent does not leave it. That is the same reasoning the monitoring
-- fold already applies to a released worker's spend.
--
--   account      the stable telemetry account tag, exactly as reported and as
--                the cockpit's account card is keyed by ("<identifier>/<org
--                uuid>", or a bare identifier). No roster row backs it, so
--                there is no foreign key to point at and an unknown tag is not
--                an error anywhere.
--   accumulated  spend reported under that account SINCE THE LAST ZEROING, in
--                the same unit the telemetry `cost` field uses. Every report
--                that raises an actor's cost adds the INCREASE here; a report
--                that is LOWER than the last one is a new session, and its
--                whole value is new spend.
--
-- 🔴 DEPLOY NOTE: every account starts at 0 the moment this lands, and nothing
-- can be back-filled. Historical spend is stored per ACTOR, and which account
-- an actor's past spend belonged to was never recorded — the account tag lives
-- only on the in-memory telemetry entry, and only for the CURRENT session. So
-- the upgrade reads, to the owner, as one free zeroing of every account card.
-- He was told that in those words before ruling (rc-5c5d7c7c6dcd).
CREATE TABLE account_spend (
    account     TEXT PRIMARY KEY,
    accumulated REAL NOT NULL DEFAULT 0.0
);

-- +goose Down
-- ⚠️ LOSSY, and there is nothing to be done about it: the accumulated figures
-- are not derivable from anything else (that is the whole reason the table
-- exists), so rolling back discards them and rolling forward again starts every
-- account from 0. The account cards simply go back to summing their actors.
DROP TABLE IF EXISTS account_spend;
