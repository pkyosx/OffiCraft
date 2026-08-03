-- +goose Up
-- T-3809 角色誌拆三段：Duty / Insight / Learning. One additive table, NO data move.
--
-- WHY A THIRD BLOCK AT ALL (owner, 2026-07-28, verbatim): 「其他人不需要知道
-- Insight，但是 Insight 跟 Learning 也不應該混在一起，後者應該是基於環境學習到
-- 的一些 Q&A」. The three blocks are:
--   * Duty    — what this role is responsible for   → role_def.definition_md (UNTOUCHED)
--   * Insight — the judgement calls and trade-offs  → THIS TABLE (new, starts empty)
--   * Learning— environment Q&A (versions, paths…)  → lessons.text (UNTOUCHED)
--
-- 🔴 ZERO AUTOMATIC SPLIT (owner ruling, 2026-08-01, rc-87e850241ef4 option ②).
-- This migration deliberately contains no UPDATE and no INSERT…SELECT: today's
-- content STAYS where it is and each role moves its own Insight over by hand,
-- if and when it judges that worth doing. The owner accepted the named cost:
-- on the day this ships every role's Insight is EMPTY, so the pain this ticket
-- was opened for (one cap shared by things whose deletion costs differ tenfold)
-- is not one character better on day one. Splitting text by machine would have
-- been the alternative, and a wrong split is worse than a slow one — a judgement
-- call filed as an environment fact gets deleted at the next cap squeeze, and
-- the agent that deletes it will not know what it cost.
--
-- SINGLE KEY, unlike lessons. lessons is keyed (role_key, task_type) because it
-- carries a task_type axis; insight has no such axis, so its document_history
-- key is the BARE role_key. That difference is load-bearing at the cascade: the
-- lessons cascade matches history keys by the "<role>::" prefix (safe there —
-- the "::" terminator means r-abc:: can never hit r-abcdef::general), while a
-- single-key document has no terminator, so deleting a role's insight history
-- MUST use exact equality (document_key = ?) the way DeleteRoleDef does.
-- A prefix match here would take r-abcdef's history out with r-abc's.
--
-- NO SEED FILE, deliberately. lessons folds overlay ⊕ a shared file seed, so a
-- role that never wrote still reads non-empty. If insight had a seed, text==""
-- would never be true and 「這個角色還沒搬」 would stop being answerable — and
-- that question is the only observable this ticket delivers.
CREATE TABLE role_insight (
  role_key   TEXT PRIMARY KEY,
  text       TEXT NOT NULL DEFAULT '',
  tombstoned INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
-- Drop the table. Nothing to restore: Up moved no data out of anywhere, so an
-- older binary sees precisely the world it left behind — Duty and Learning were
-- never touched. Insight text written while the table existed is lost with it;
-- there is nowhere older to put it, because nowhere older ever held it.
DROP TABLE role_insight;
