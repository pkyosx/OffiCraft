-- +goose Up
-- T-33 — 傳承（lore）的地基. Twelve tables: the three memory layers, the
-- events table that is the entry's fifth cell, the two join tables that keep the
-- retrieval axes apart, the ontology (entity / entity_alias / entity_type), and
-- the three journals (recall / feedback / governance).
--
-- 🔴 THE NAME IS `lore_*`, NOT `memory_*`. The detail design used
-- `memory_*` as an explicit PLACEHOLDER pending a naming ruling ("暫且叫做");
-- the owner ruled on 2026-08-31 that the thing is called 「傳承」 / `lore`,
-- so the tables carry that name from their first version. Renaming a table
-- after a station has data in it costs a rebuild migration; naming it right
-- once costs nothing.
--
-- 🔴 THE THREE LAYERS ARE THREE TABLES, AND THAT IS THE WHOLE POINT of the
-- split. Only `lore_entry` (L1) is ever folded into a boot
-- context. `lore_revision` (L0) holds the full prose nobody reads
-- at boot, and `lore_meta` (L2) holds the governance counters that
-- MUST NEVER reach a context. Being a separate table is what makes "the
-- assembler cannot see L2" a fact about the SELECT list rather than a promise
-- in a comment.
--
-- 🔴 `trust_scope` IS DELIBERATELY NOT A COLUMN ANYWHERE BELOW. It is derived
-- from the entry's actions at read time by memoryTrustScope() — the single
-- function in the tree that answers the question. Storing it would create a
-- second truth that drifts from the actions it was derived from, and a stale
-- stored value is exactly the silent failure this ticket exists to kill. See
-- memory_trust_scope.go for the derivation and its fail-closed rule.
--
-- 🔴 `origin` LIVES ON L1, NOT ON L2, AND THAT IS NOT A STYLE CHOICE. It says
-- WHO this piece of knowledge came from — `human:Seth`, `agent:Kyle` — and the
-- ranking rule makes a human origin a hard axis: those entries sort ahead within
-- their tier and survive the count cap. A field that participates in ordering
-- and truncation is a field the assembler must be able to SELECT — so it cannot
-- live in the table whose defining property is that the assembler cannot see it.
--
-- 🔴 THERE IS NO ERASE PATH IN THIS SCHEMA, BY RULING. "丟掉" means
-- `status='retired'` — the row stops being retrieved and keeps existing. The
-- owner chose that option knowing its cost (rc-559af60bfba4): a secret written
-- into an entry has no tool to remove it today. Nothing below, and nothing in
-- the DAL, offers a hard delete.

CREATE TABLE entity_type (
    type        TEXT PRIMARY KEY,
    approved_by TEXT NOT NULL DEFAULT '',
    approved_ts REAL NOT NULL DEFAULT 0.0
);
-- A CLOSED list, gated on a human, because it is short, changes rarely, and is
-- global. The primary-key layer below is the opposite — it does NOT gate writes,
-- because gating there would push an agent into either forcing a near-miss key
-- (silent) or not writing at all (the disease this ticket treats).
--
-- 🔴 THIS TABLE IS THE ONE AND ONLY COPY OF THE TYPE-PREFIX LIST. Subjects and
-- `origin` are the same shape — `type:name` — so they are validated against the
-- same rows, read at run time (loreOriginError in
-- dal_lore.go). A second list hard-coded in Go would be a copy that
-- drifts the first time a type is approved, and the two would then disagree about
-- what is writable, silently, in whichever direction the reader happened to use.
--
-- 🔴 `agent`, NOT `member`, AND THAT IS THE OWNER'S WORDING (2026-08-31): "member
-- 這個詞用來代表 ai 也有點怪怪的 叫做 agent?". The pair that has to work is
-- `agent:` vs `human:`, and `member:` does not make that cut — the OWNER is a
-- member too, so `member` fails to exclude the very thing `human` names. `agent`
-- / `human` splits on WHAT IT IS, which needs no background knowledge to read.
-- ⚠️ The rest of the system says "member" for the same population. That vocabulary
-- gap is KNOWN AND ACCEPTED; do not "align" it back.
INSERT INTO entity_type (type) VALUES
  ('agent'),('human'),('role'),('repo'),('machine'),
  ('model'),('tool'),('service'),('doc'),('task-type');

CREATE TABLE entity (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL REFERENCES entity_type(type),
    canonical   TEXT NOT NULL,
    display     TEXT NOT NULL DEFAULT '',
    ref_kind    TEXT NOT NULL DEFAULT '',
    ref_id      TEXT NOT NULL DEFAULT '',
    pending     INTEGER NOT NULL DEFAULT 0,
    merged_into TEXT NOT NULL DEFAULT '',
    created_ts  REAL NOT NULL DEFAULT 0.0,
    created_by  TEXT NOT NULL DEFAULT ''
);
-- 🔴 主名全域唯一. This is not tidiness — it is the precondition for the index
-- being worth anything. Without it the ontology grows a family of near-synonym
-- primary names; the index does not BREAK, it becomes useless, which is harder
-- to notice.
CREATE UNIQUE INDEX idx_entity_canonical ON entity (canonical);

CREATE TABLE entity_alias (
    alias     TEXT NOT NULL,
    entity_id TEXT NOT NULL REFERENCES entity(id),
    PRIMARY KEY (alias, entity_id)
);
-- 🔴 PRIMARY KEY (alias, entity_id) — DELIBERATELY NOT UNIQUE(alias). The owner
-- was explicit: the primary name is unique, aliases may repeat. "Kyle" being
-- both the canonical of agent:Kyle and an alias of human:KyleHsia is CORRECT,
-- not a data error. Ambiguity is therefore computed at resolve time and never
-- stored — hence no `is_ambiguous` column: a stored flag would be a second truth
-- that goes stale the moment a new alias lands.
CREATE INDEX idx_entity_alias_alias ON entity_alias (alias);

CREATE TABLE lore_entry (
    id            TEXT PRIMARY KEY,
    -- 🔴 五格，固定，而且刻意沒有自由欄位。負責人 2026-09-03 定案的格式是
    -- 「什麼時候要記起來／內容／什麼時候不需要了／之前發生過什麼問題／相關的完整
    -- 資訊」。前四格是下面這四個欄位；第五格不是欄位，是 lore_event 那張表（一條
    -- 條目底下掛 0..N 筆事件），見本檔案 lore_entry 之後。
    --
    -- 🔴 這一版取代了原本的六格（label / symptoms / short / falsify / instance /
    -- residual_risk）。00081 與 00083 都是這個分支自己引入、還沒進 main 的，線上
    -- 零資料，所以是直接改欄位宣告，而不是疊一支 ALTER —— 疊 ALTER 會在一個從來
    -- 沒有過舊欄位的資料庫上留下一段假的歷史。
    --
    -- 🔑 WHY THIS IS THE TICKET'S OWN SUBJECT, NOT A LAYOUT PREFERENCE: with the
    -- fields fixed, "this entry got polished away" becomes VISIBLE. Free form
    -- cannot do that — a missing section and a section the author never wrote
    -- look identical, which is precisely the disease this ticket treats:
    -- something disappeared and nothing reported it. See IsDegraded() in
    -- dal_lore.go, which is that check made cheap.
    --
    -- 🔴 `trigger` 兼任這條條目的標題，而且沒有長度上限。舊的 `label`（一行名字、
    -- 上限 40 runes）不見了：五格裡根本沒有「名字」這一格，而負責人自己示範的好
    -- 例子就是把第一格當標題寫的 ——「【什麼時候要記起來】我要確認一個 OffiCraft
    -- 前端畫面接的是真後端，還是假資料」—— 那一行遠遠超過 40 runes。留著上限就是
    -- 讓示範用的寫法寫不進來。
    -- ⚠️ 「第一格兼任標題、因此拿掉 label 與 40 runes 上限」是實作判斷，不是負責人
    -- 的裁定。它被寫在這裡而不是默默做掉，就是為了讓下一個人看得見它可以被推翻。
    --
    -- 🔴 `trigger` 是唯一「空值必須被拒絕」的一格，而且拒絕發生在 DAL
    -- （loreTriggerError，dal_lore.go）。這裡沒有 CHECK (trigger <> '') 是因為
    -- SQLite 的 CHECK 訊息不能告訴呼叫者哪一格空了；欄位層的 NOT NULL 擋住 NULL，
    -- 語意層的必填擋住空字串，兩者不是同一件事。
    trigger       TEXT NOT NULL DEFAULT '',  -- 什麼時候要記起來；形狀是「我要做 X」。兼任標題，無長度上限
    content       TEXT NOT NULL DEFAULT '',  -- 內容：唯一會進開機脈絡的一格
    -- 🔴 `retire_when` 是自由文字，不是封閉值域，而且刻意沒有 CHECK。「什麼時候
    -- 不需要了」可能是「等 X 上線」「等某人回答」「這個 repo 不再用 goose」——任何
    -- 列舉都會在第一個沒想到的情況把寫入者逼去挑一個最接近的錯答案，而挑錯的跟
    -- 挑對的長得一模一樣。
    retire_when   TEXT NOT NULL DEFAULT '',  -- 什麼時候不需要了（選填，自由文字）
    -- 🔴 `problem` 是選填，但它是主體：一條沒有問題撐著的條目就是一句口號。
    -- 它同時是 IsDegraded() 目前的（暫定）判準，見 dal_lore.go。
    problem       TEXT NOT NULL DEFAULT '',  -- 之前發生過什麼問題（選填）
    status        TEXT NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','superseded','retired','underspecified')),
    supersedes    TEXT NOT NULL DEFAULT '',
    -- 🔴 THERE IS NO `visibility` / `owner_scope` PAIR HERE, BY RULING. The
    -- draft carried a coarse private/shared wall with a scope string beside it;
    -- the owner ruled on 2026-08-31 (rc-26c1fd0c6b3c, option [3]) 「不要私密條目
    -- 了，全部共享」 — every entry is shared. Keeping the columns "just in case"
    -- would leave a half-enforced wall that every future reader has to decide
    -- whether to honour, which is worse than no wall at all.
    editable_by   TEXT NOT NULL DEFAULT 'agent'
                  CHECK (editable_by IN ('agent','owner-gated')),
    -- 🔴 `origin` IS A SUBJECT KEY, NOT AN ENUM — `human:Seth`, `agent:Kyle`,
    -- `agent:O-197`. The owner replaced the enum himself (2026-08-31, verbatim:
    -- "origin:agent / origin:human ?"), and the draft value `derived` is GONE:
    -- it meant "some agent worked this out", which `agent:<who>` says while also
    -- naming who. The value set is therefore OPEN and a CHECK constraint cannot
    -- express it — validation is the shared `type:name` format check against
    -- entity_type above, done in the DAL, fail-closed and by name.
    origin        TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL DEFAULT 0.0,
    updated_ts    REAL NOT NULL DEFAULT 0.0
);
-- Every body field is NOT NULL DEFAULT '': the column always EXISTS, and an
-- author who has nothing to put there leaves it empty. That is the difference
-- that matters — an empty column is a countable, queryable absence, whereas a
-- section that was never written leaves nothing behind to count.
CREATE INDEX idx_lore_entry_status ON lore_entry (status);

-- ── 第五格：相關的完整資訊 ───────────────────────────────────────────────────
--
-- 🔴 這張表放在 00081 而不是另開一支新號碼，是刻意的，而且理由是流程性的：
-- migration 號碼由 Kyle 統一發，我不能自己挑一個。00081 是這個分支自己引入、
-- 還沒進 main 的一支，站上零資料，所以把 lore_entry 的第五格加在它自己的建表
-- migration 裡沒有任何遷移成本，而且讀的人在同一個檔案裡就看得到完整的五格。
--
-- 🔴 一條條目掛 0..N 筆事件，所以它是一張表而不是欄位。第五格是「相關的完整
-- 資訊」，一條條目可以是好幾次事件累積出來的；壓成一個欄位就等於要寫入者自己
-- 發明一套分隔符號，而那正是這張票要治的病（每張卡各自長出自己的段落）。
CREATE TABLE lore_event (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id    TEXT NOT NULL REFERENCES lore_entry(id),
    -- 🔴 時 = 事件發生的時間，不是這一列被寫下的時間。這兩件事會差好幾天：
    -- 一個 agent 今天回頭補記上週撞到的事，happened_ts 是上週。刻意沒有
    -- created_ts 欄位——多一個時間欄位就多一個會被讀錯的時間，而目前沒有任何
    -- 路徑需要「這一列什麼時候被寫下」。真的需要時再加，並且要說得出誰要讀它。
    happened_ts REAL NOT NULL,
    -- 🔴 事 = 一律主動語態，讓「人」永遠是動作者。「畫面被改成假資料」把動作者
    -- 藏起來了，「Seth 把畫面改成假資料」沒有。這一層擋不了被動語態（沒有任何
    -- 欄位擋得住），所以它是寫在這裡的規則，不是假裝成約束的東西。
    what        TEXT NOT NULL,
    -- 🔴 人／地／物：有才填，而且「空著」必須看得出來。
    -- 這三格是 NOT NULL DEFAULT ''，空字串就是「這一格沒有東西」——不要用
    -- 「未知」「n/a」「unknown」之類的字串把它填滿。「查不出是誰」跟「還沒有人
    -- 去查」必須長得不一樣，而一旦有人往裡面塞了一個佔位字串，這兩件事就永遠
    -- 分不開了。這是負責人明確要的。
    actor       TEXT NOT NULL DEFAULT '',  -- 人：`human:` / `agent:` 前綴
    place       TEXT NOT NULL DEFAULT '',  -- 地：`machine:` 前綴。語意＝這個動作在哪台機器上發生
    object      TEXT NOT NULL DEFAULT ''   -- 物：`service:` 等前綴。語意＝被動到的是什麼
);
-- 🔴 沒有 CHECK 限制人／地／物的前綴，理由跟 origin 一模一樣：前綴的值域是
-- entity_type 那張表，而那張表是會長出新列的。前綴的檢查在 DAL，讀 entity_type
-- 做，而且只在該格「非空」時才做——空著是合法的，對空字串做前綴檢查就等於把
-- 選填變成必填。
--
-- 讀取一律 ORDER BY (entry_id, happened_ts, id)：事件的順序是事情發生的順序，
-- 不是誰先被寫進來的順序。id 只是同一個時刻的 tie-break。
CREATE INDEX idx_lore_event_entry ON lore_event (entry_id, happened_ts, id);

CREATE TABLE lore_revision (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id     TEXT NOT NULL REFERENCES lore_entry(id),
    body         TEXT NOT NULL,
    sha256       TEXT NOT NULL,
    actor_id     TEXT NOT NULL DEFAULT '',
    created_ts   REAL NOT NULL DEFAULT 0.0,
    shrink_chars INTEGER NOT NULL DEFAULT 0
);
-- 🔴 THIS IS A SEPARATE JOURNAL FROM `document_history` ON PURPOSE. That table
-- (00043) keeps only the most recent few revisions of a document — write three
-- more times and the old one is gone forever. L0 is the ground truth this whole
-- mechanism stands on, so it is append-only with NO depth limit; building it on
-- a table that rolls off would put the foundation on something that discards.
--
-- `shrink_chars` is where "every action that makes content smaller must leave a
-- record of what was lost" becomes a column. Compression today leaves no trace
-- at all: the entry count is unchanged, so the loss is invisible.
CREATE INDEX idx_lore_revision_entry ON lore_revision (entry_id, id DESC);

CREATE TABLE lore_meta (
    entry_id           TEXT PRIMARY KEY REFERENCES lore_entry(id),
    created_ts         REAL NOT NULL DEFAULT 0.0,
    last_recalled_ts   REAL NOT NULL DEFAULT 0.0,
    recall_count       INTEGER NOT NULL DEFAULT 0,
    surfaced_count     INTEGER NOT NULL DEFAULT 0,
    unique_query_count INTEGER NOT NULL DEFAULT 0,
    importance         INTEGER NOT NULL DEFAULT 0,
    confirmed_count    INTEGER NOT NULL DEFAULT 0,
    found_stale_count  INTEGER NOT NULL DEFAULT 0,
    source_task_id     TEXT NOT NULL DEFAULT '',
    source_chat_id     TEXT NOT NULL DEFAULT '',
    source_actor_id    TEXT NOT NULL DEFAULT ''
);
-- 🔴 NO `origin` COLUMN HERE. The design draft listed one on both L1 and L2;
-- keeping both would be two truths about the same fact, with the retrieval path
-- reading one and the governance UI the other. It lives on L1 (see above),
-- because that is the copy that has to participate in ordering.
--
-- 🔴 `recall_count == 0` IS NOT, ON ITS OWN, GROUNDS FOR RETIRING AN ENTRY —
-- an entry that was never surfaced cannot have been recalled, so retiring on
-- that number lets the retrieval path silently confirm its own choices. A CHECK
-- constraint cannot express this; it is a rule for whatever writes the
-- retirement path, and it is owed a mutant test when that path is written.

CREATE TABLE lore_subject (
    entry_id  TEXT NOT NULL REFERENCES lore_entry(id),
    entity_id TEXT NOT NULL REFERENCES entity(id),
    PRIMARY KEY (entry_id, entity_id)
);
CREATE INDEX idx_lore_subject_entity ON lore_subject (entity_id, entry_id);

CREATE TABLE lore_action (
    entry_id TEXT NOT NULL REFERENCES lore_entry(id),
    action   TEXT NOT NULL,
    PRIMARY KEY (entry_id, action)
);
CREATE INDEX idx_lore_action_action ON lore_action (action, entry_id);
-- 🔴 TWO TABLES, NOT ONE TAG BAG. Ranking has to tell "same subject, different
-- action" apart from "same action, different subject" — that is the T1/T2
-- distinction. Flattened into one tag set, the two are indistinguishable and the
-- ordering cannot be computed at all.
--
-- 🔴 AND THE TWO AXES ARE DIFFERENT SHAPES ON PURPOSE: subjects saturate (the
-- world contains a finite number of things we name), actions do NOT (every new
-- kind of experience can mint a new action name). Anything that assumes the
-- action set is closed — a build-time exhaustiveness guard, most of all — is
-- blind to exactly the names that appear after it was written.

CREATE TABLE lore_recall_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id   TEXT NOT NULL DEFAULT '',
    query      TEXT NOT NULL DEFAULT '',
    subject_id TEXT NOT NULL DEFAULT '',
    hop        INTEGER NOT NULL DEFAULT 0,
    returned   TEXT NOT NULL DEFAULT '',
    created_ts REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_lore_recall_log_ts ON lore_recall_log (created_ts DESC);

CREATE TABLE lore_feedback (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id   TEXT NOT NULL REFERENCES lore_entry(id),
    verdict    TEXT NOT NULL CHECK (verdict IN ('helpful','harmful')),
    shape      TEXT NOT NULL DEFAULT ''
               CHECK (shape IN ('','restated','stale','mis-subject')),
    actor_id   TEXT NOT NULL DEFAULT '',
    note       TEXT NOT NULL DEFAULT '',
    created_ts REAL NOT NULL DEFAULT 0.0
);
-- `shape` is mandatory on a `harmful` verdict (enforced above this layer) because
-- the three shapes have three different repairs: a restated entry wants merging,
-- a stale one wants retiring, a mis-subject one wants its subject fixed. An
-- undifferentiated "this was bad" count tells nobody which to do.

CREATE TABLE lore_governance_event (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL,
    target      TEXT NOT NULL,
    actor_id    TEXT NOT NULL DEFAULT '',
    reason      TEXT NOT NULL DEFAULT '',
    replaced_by TEXT NOT NULL DEFAULT '',
    created_ts  REAL NOT NULL DEFAULT 0.0
);
-- The four questions a retirement has to answer — who, when, why, replaced by
-- what — are these four columns. `kind` is deliberately NOT a CHECK list: the
-- governance vocabulary is still being written, and a CHECK here would turn
-- "a new kind of event" into a migration.

-- +goose Down
-- A real Down: this migration only CREATES, so dropping is exact — nothing that
-- existed before it is touched. It is not a rollback path for a live station
-- with data (that path is a retreat of the code, not of the schema); it exists
-- so the reversibility check has something true to run.
DROP TABLE lore_governance_event;
DROP TABLE lore_feedback;
DROP TABLE lore_recall_log;
DROP TABLE lore_action;
DROP TABLE lore_subject;
DROP TABLE lore_meta;
DROP TABLE lore_revision;
DROP TABLE lore_event;
DROP TABLE lore_entry;
DROP TABLE entity_alias;
DROP TABLE entity;
DROP TABLE entity_type;
