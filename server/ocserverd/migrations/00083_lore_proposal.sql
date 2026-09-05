-- +goose Up
-- T-33 — 提案有地方存了：一份完整的新版本，而不是一段描述.
--
-- 🔴 WHAT WAS ACTUALLY MISSING. `lore_feedback` (00081) exists and holds
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
-- arbitration path arrives, `lore_governance_event` (00081: kind / target /
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
    -- 🔴 這四格跟 lore_entry 的前四格一一對應，而且是同一次改格式改過來的：
    -- 舊的 label / symptoms / short / falsify / instance / residual_risk 六格
    -- 換成 trigger / content / retire_when / problem 四格 + lore_event 一張表。
    --
    -- 🔴 第五格（事件）在 `lore_proposal_event` 那張表裡，見這支 migration 的
    -- 下半段。一份 `update` 提案帶的是**完整的新版本，包含它自己的整份事件清單**
    -- —— 負責人 2026-09-03 的裁定（卡 rc-e5c34500face）：「改得動 —— 提案就該帶
    -- 完整的新版本，包含所有事件」。
    trigger          TEXT NOT NULL DEFAULT '',
    content          TEXT NOT NULL DEFAULT '',
    retire_when      TEXT NOT NULL DEFAULT '',
    problem          TEXT NOT NULL DEFAULT '',
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

-- ── 第五格：一份提案自己的事件清單 ─────────────────────────────────────────
--
-- 🔴 為什麼提案必須帶得動事件 —— 負責人 2026-09-03 裁定（卡 rc-e5c34500face），
-- 而他推翻的正是「第 5 格是機器串出來的事實，提案只是意見，意見不該改得動事實」
-- 這個講法。那個講法有一個洞：**機器串錯的時候，沒有任何一條路修得了它**。
-- 「重跑一次就好」不成立 —— 沒有經過 API 的動作蓋不到記錄者（負責人已裁定：那些
-- 格只能空著），所以人工補上去的事件會被重跑一起沖掉。⇒ 提案改得動事件，才是
-- 唯一修得了的路。
--
-- 🔴 它是一張表而不是一欄 JSON，理由跟 lore_event 一模一樣：一份提案掛 0..N 筆
-- 事件，壓成一欄就要自己發明分隔符號。而且兩張表同形狀，是為了讓「核可時把
-- lore_event 整批換成這一份」是一次欄位對欄位的搬運，不是一次格式翻譯。
--
-- 🔴 沒有 `id AUTOINCREMENT`，用 (proposal_id, seq)。lore_event 的 id 是全域的
-- 因為它要被 ORDER BY 當 tie-break；提案的事件永遠只在一份提案裡面被讀，seq 是
-- 送進來時的順序，只當同一個 happened_ts 的 tie-break 用。
--
-- ⚠️ 這張表**不驗證**人／地／物的前綴，跟 lore_event 一樣：值域在 entity_type，
-- 而那張表會長出新列。檢查在 DAL（loreEventError），而且只在非空時做。
--
-- 🔴 「一份 update 提案沒有任何事件」是一個**合法而且看得見的主張**：它說
-- 「這條條目不該有事件」。它跟「提案沒提到事件」不是同一件事 —— 後者在 API 層
-- 就被拒絕了（events 在 update 上是必填），因為讓它們長得一樣，就等於讓一次
-- 漏填在審核者看不見的地方清空第 5 格。
CREATE TABLE lore_proposal_event (
    proposal_id TEXT NOT NULL REFERENCES lore_proposal(id),
    seq         INTEGER NOT NULL,
    happened_ts REAL NOT NULL,
    what        TEXT NOT NULL,
    actor       TEXT NOT NULL DEFAULT '',
    place       TEXT NOT NULL DEFAULT '',
    object      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (proposal_id, seq)
);
-- 讀取一律 ORDER BY (proposal_id, happened_ts, seq)：事件的順序是事情發生的
-- 順序，不是誰先被送進來的順序。
CREATE INDEX idx_lore_proposal_event ON lore_proposal_event (proposal_id, happened_ts, seq);

-- +goose Down
-- ⚠️ 有損，而且說在前面：every proposal filed while this table existed is gone.
-- There is nowhere else in the schema that carries one, so a retreat past this
-- migration loses them for good. It is written this way rather than not written
-- at all because a Down that cannot run is worse than one whose cost is stated:
-- the round trip is exercised in migration_00083_lore_proposal_test.go.
-- The lore_entry and lore_revision rows a proposal pointed at are untouched.
DROP INDEX idx_lore_proposal_event;
DROP TABLE lore_proposal_event;
DROP INDEX idx_lore_proposal_entry;
DROP TABLE lore_proposal;
