-- +goose Up
-- T-33 — 傳承（lore）的地基. Eleven tables: the three memory layers, the
-- two join tables that keep the retrieval axes apart, the ontology (entity /
-- entity_alias / entity_type), and the three journals (recall / feedback /
-- governance).
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
    -- 🔴 THE SIX BODY FIELDS ARE FIXED, AND THERE IS DELIBERATELY NO FREE-FORM
    -- COLUMN AMONG THEM. The owner looked at a sample of entries and said it
    -- plainly: every card had grown its OWN differently-named sections, none of
    -- them defined anywhere. He was right, and the fix is not a formatting
    -- convention — it is these columns.
    --
    -- 🔑 WHY THIS IS THE TICKET'S OWN SUBJECT, NOT A LAYOUT PREFERENCE: with the
    -- fields fixed, "this entry got polished away" becomes VISIBLE. An entry
    -- whose falsify and instance are both empty is, at a glance, a slogan. Free
    -- form cannot do that — a missing section and a section the author never
    -- wrote look identical, which is precisely the disease this ticket treats:
    -- something disappeared and nothing reported it. See IsDegraded() in
    -- dal_lore.go, which is that check made cheap.
    -- 🔴 `label` IS A NAME, NOT A SENTENCE, AND THAT IS WHY IT IS CAPPED. It is
    -- what a reader scans a list by, and what a merge or a supersede POINTS AT.
    -- A sentence invites the next author to tidy the wording — and when the name
    -- changes, the thing pointing at it stops pointing at anything. The cap is
    -- enforced as a REFUSAL in the DAL (not a truncation, not a warning): silently
    -- shortening a name is the same silent loss this ticket exists to kill.
    -- ⚠️ 40 是佔位數字，不是算出來的 — it is a placeholder, not a measured value,
    -- and it has to be calibrated after the trial.
    label         TEXT NOT NULL DEFAULT '',  -- one-line NAME, max 40 runes (see loreLabelMaxRunes)
    symptoms      TEXT NOT NULL DEFAULT '',  -- what I would be SEEING; the situation, not a category name
    short         TEXT NOT NULL DEFAULT '',  -- the compressed body: the mechanism and why
    falsify       TEXT NOT NULL DEFAULT '',  -- how to show this entry does NOT hold
    instance      TEXT NOT NULL DEFAULT '',  -- one case that really happened
    residual_risk TEXT NOT NULL DEFAULT '',  -- what this entry does NOT protect against
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
DROP TABLE lore_entry;
DROP TABLE entity_alias;
DROP TABLE entity;
DROP TABLE entity_type;
