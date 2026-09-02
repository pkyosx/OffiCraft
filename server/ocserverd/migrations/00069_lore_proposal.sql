-- +goose Up
-- T-33 — 提案有地方存了：一份完整的新版本，而不是一段描述.
--
-- 🔴 WHAT WAS ACTUALLY MISSING. `lore_feedback` (00066) exists and holds
-- `entry_id / verdict / shape / actor_id / note / created_ts`. That is enough to
-- say 「這條幫倒忙」 plus one sentence, and it is NOT enough to hold a proposal:
-- there is nowhere to put WHAT the agent thinks it should say instead, and
-- nowhere to put WHICH VERSION he was looking at when he said it. Measured, not
-- assumed: `grep -rn lore_feedback server/ocserverd/*.go` returns nothing at
-- all, so the table has never been written, never been read, and has no API.
-- ⚠️ THIS MIGRATION DOES NOT TOUCH IT. Whether a bare thumbs-up/thumbs-down
-- journal is still wanted beside proposals is not a question anybody has ruled
-- on, and answering it by deleting the table here would be deciding it quietly.
-- It stays exactly as it is, still unused, and that is reported rather than
-- tidied.
--
-- ── WHY A WHOLE VERSION AND NOT A PATCH ─────────────────────────────────────
--
-- Owner, 2026-09-02, verbatim: 「我覺得讓 agent submit new full version 即可 /
-- diff view 我們自己產出」. A patch leaves room between 「他描述的改動」 and
-- 「套下去實際變成什麼」, and that gap looks completely normal from the outside —
-- a reviewer reads a plausible description, approves it, and something else
-- lands. Storing the whole version removes the second artefact: the diff a
-- reviewer sees is computed from the exact bytes that would be written, so there
-- is no version in between for the two to disagree about.
--
-- Hence `body` + `sha256` here are rendered by THE SAME loreRevisionBody /
-- loreSHA256 the L0 journal uses (dal_lore_write.go). One renderer, so a
-- proposal's digest and an accepted revision's digest are comparable at all; two
-- renderers would make 「這份提案就是那一版」 unanswerable.
--
-- ── base_revision_id / base_sha256: 過期提案是跟 PR 一模一樣的坑 ─────────────
--
-- 🔴 THESE TWO COLUMNS ARE THE POINT OF THE TABLE, not bookkeeping. A proposal
-- written on Monday and reviewed on Friday may have had the entry rewritten
-- underneath it on Wednesday. Applying it then silently discards Wednesday's
-- work, and NOTHING about the result looks wrong. The base digest is what lets
-- that be caught: it is compared at submit time (loud refusal, see
-- ErrLoreProposalStale) and recomputed at read time (`stale` on every row), so
-- the fact is in hand both when the proposal is written and when somebody comes
-- to act on it.
--
-- The digest is the anchor rather than the revision id ALONE because the id
-- answers 「哪一列」 and the digest answers 「那一列說了什麼」 — and it is the
-- second question an accept has to be sure about. Both are stored: the id makes
-- the row findable, the digest makes the comparison possible.
--
-- ── NO `status` COLUMN, DELIBERATELY ────────────────────────────────────────
--
-- Accepting or declining a proposal is 仲裁, which is a different piece of work
-- and is NOT in this change. A status column added now could only ever hold one
-- value, which is a column no test can distinguish from a correct one. When the
-- arbitration path arrives, `lore_governance_event` (00066: kind / target /
-- actor_id / reason / replaced_by) is already the right shape for recording the
-- verdict, so this table may well need no new column at all.
--
-- ── NO UNIQUE CONSTRAINT ON (entry_id, actor_id) ────────────────────────────
--
-- Two proposals from the same agent on the same entry are two proposals: the
-- first may have gone stale and been rewritten against the newer version. The
-- journal keeps both, for the same reason lore_revision keeps every revision.
CREATE TABLE lore_proposal (
    id               TEXT PRIMARY KEY,
    entry_id         TEXT NOT NULL REFERENCES lore_entry(id),
    -- 'update' carries a whole replacement version below; 'remove' proposes the
    -- entry stop being retrieved and carries NONE of the body fields. The two
    -- are one table because they answer the same review, and because the base
    -- digest matters identically to both — a removal argued from a version that
    -- has since been rewritten is exactly as wrong as an edit against one.
    kind             TEXT NOT NULL CHECK (kind IN ('update','remove')),
    base_revision_id INTEGER NOT NULL,
    base_sha256      TEXT NOT NULL,
    -- ── 三格，必填 ───────────────────────────────────────────────────────────
    -- The reasoning is the owner's own on `falsify` (rc-714eea33c6ed): a field
    -- that may be left blank is a field that will be, and 「他建議改掉」 with no
    -- account of where it came from is a vote rather than a proposal.
    -- ⚠️ AND IT CARRIES THE SAME ACCEPTED, UNSOLVED COST: nothing at this layer
    -- can tell a real account from an invented one. It refuses an EMPTY cell,
    -- which is all a column can do.
    encountered      TEXT NOT NULL,  -- 他是在做什麼的時候撈到這條的
    fault            TEXT NOT NULL   -- 他認為錯在哪
                     CHECK (fault IN ('stale','never-true','misled')),
    evidence         TEXT NOT NULL,  -- 他實際看到的東西
    -- ── the proposed version, in full (blank throughout on a 'remove') ───────
    label            TEXT NOT NULL DEFAULT '',
    symptoms         TEXT NOT NULL DEFAULT '',
    short            TEXT NOT NULL DEFAULT '',
    falsify          TEXT NOT NULL DEFAULT '',
    instance         TEXT NOT NULL DEFAULT '',
    residual_risk    TEXT NOT NULL DEFAULT '',
    -- The rendered whole version and its digest. Stored rather than re-rendered
    -- on read for one reason: the digest a reviewer approved must be the digest
    -- of what was submitted, even if the renderer changes afterwards.
    body             TEXT NOT NULL DEFAULT '',
    sha256           TEXT NOT NULL DEFAULT '',
    actor_id         TEXT NOT NULL,
    created_ts       REAL NOT NULL DEFAULT 0.0
);
-- The listing reads an entry's proposals NEWEST FIRST, and 'newest' is
-- created_ts: the id is random hex and orders nothing. The index carries the
-- same tie-break the query does so the two never disagree.
CREATE INDEX idx_lore_proposal_entry ON lore_proposal (entry_id, created_ts DESC, id DESC);

-- +goose Down
-- ⚠️ 有損，而且說在前面：every proposal filed while this table existed is gone.
-- There is nowhere else in the schema that carries one, so a retreat past this
-- migration loses them for good. It is written this way rather than not written
-- at all because a Down that cannot run is worse than one whose cost is stated:
-- the round trip is exercised in migration_00069_lore_proposal_test.go.
-- The lore_entry and lore_revision rows a proposal pointed at are untouched.
DROP INDEX idx_lore_proposal_entry;
DROP TABLE lore_proposal;
