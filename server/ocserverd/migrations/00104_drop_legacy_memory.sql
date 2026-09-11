-- +goose Up
-- T-186 步驟三 — 資料庫層. The backend (4f9a151a) and the frontend (b0a09a0d)
-- already removed every reader and writer of the legacy memory surface: the
-- five MCP tools, the `learnings` field on the task-manual wire, the two
-- document-history kinds, and the two settings caps. Nothing in the tree reads
-- any of the four things below any more (`git grep` over non-test Go finds no
-- `FROM lessons` / `INTO lessons` / `task_manual.learnings`; the positive
-- control on the same query shape still finds `FROM role_insight`). This
-- migration is what makes that true of the DATABASE as well.
--
-- 🔴 THE ORDER IS document_history FIRST, THEN THE STORAGE IT RESTORES INTO.
-- 00061 and 00062 both state the rule and this file inherits it: a retained
-- revision is a live路徑 back into the table it came from, so a migration that
-- removed the table and left the revisions would leave rows that name a
-- restore target which no longer exists. Here the code-side door is already
-- shut (documentHistoryAllowed no longer lists either kind, so list/restore
-- answer 400), but the rows themselves would sit in the journal forever,
-- countable by list_document_history's neighbours and restorable again the
-- moment anyone re-added the kind. Nothing would go red either way — this is
-- the 「第二道門」 both earlier migrations had to be told about explicitly.
--
-- 🔴 BOTH KINDS, AND EXACT EQUALITY RATHER THAN A PREFIX. `task_manual_learnings`
-- shares its prefix with `task_manual_sop`, which SURVIVES this change and
-- holds the manual's live history. `LIKE 'task_manual%'` would take both; the
-- IN-list cannot. 00045 made the same choice for the same reason. Every other
-- document_kind — insight, global_context, role_definition, task_description,
-- task_title, the boot-document kinds and task_manual_sop — is untouched.
DELETE FROM document_history
 WHERE document_kind IN ('lessons', 'task_manual_learnings');

-- 🔴 THE TABLE. 00001 created it, 00062 rebuilt it as
-- (role_key PRIMARY KEY, text, tombstoned). No Go code opens it since
-- 4f9a151a — DeleteLessonsForRole, which 00061 cites as the cascade precedent,
-- was deleted in that commit and survives only inside migration comments.
DROP TABLE lessons;

-- 🔴 THE COLUMN — PLAIN `ALTER TABLE ... DROP COLUMN`, AND THAT IS MEASURED ON
-- THE DRIVER THAT ACTUALLY RUNS IT, NOT ON THE sqlite3 CLI. 00062 could not use
-- ALTER because task_type was half of a PRIMARY KEY and SQLite refuses to drop
-- such a column; `learnings` (00004:130, `TEXT NOT NULL DEFAULT ''`) is an
-- ordinary column, task_manual carries no index, view or trigger naming it, and
-- so the create/copy/drop/rename rebuild 00062 needed is not needed here.
--
-- The server does not use the sqlite3 CLI: migrate.go runs goose over
-- modernc.org/sqlite v1.53.0, a pure-Go reimplementation, and "the CLI can do
-- it" is not evidence about that engine. It is pinned by a test that runs
-- through this very driver — TestMigration00104DropsTheLegacyMemoryStorage —
-- so the claim is checked by the same code path that ships.
ALTER TABLE task_manual DROP COLUMN learnings;

-- 🔴 THE TWO CAP ROWS. `doc.cap_chars.learning` capped the role lessons doc and
-- `doc.cap_chars.manual_learnings` (seeded by 00049 from the pre-split
-- `doc.cap_chars.manual`) capped the manual's learnings. settings.go no longer
-- declares either key, and an undeclared row is not merely unread: get_settings
-- enumerates the key space code-side, so these two would be invisible AND
-- undeletable through the API — a row nobody can see and nobody can remove.
-- The station this ships to has them (they are written rows, not defaults);
-- a station that never raised a cap has no row here and this is a no-op, which
-- is the same idempotent-by-shape property 00061 relies on.
DELETE FROM setting
 WHERE key IN ('doc.cap_chars.learning', 'doc.cap_chars.manual_learnings');

-- ── 飛在半空中的任務與成員，跨版本時會怎樣 ───────────────────────────────────
--
-- 🔴 THE DANGEROUS COMBINATION IS OLD BINARY + NEW SCHEMA, AND IT IS NOT
-- HYPOTHETICAL ON THIS STATION. Replacing the executable does not replace the
-- process: a listener that was not restarted keeps running the old code, and
-- `bin/migrate` moves the DATABASE without asking what is still running against
-- it. An old binary meeting a 104 schema does not degrade gracefully — its
-- reads name the table and the column directly, so `list_task_manuals` /
-- `get_task_manual` fail outright with "no such column: learnings" and any
-- lessons read with "no such table: lessons". Those are the manual surfaces
-- every member consults mid-task, so the blast radius is not the retired
-- feature, it is the manual. THE UPGRADE MUST THEREFORE MIGRATE AND RESTART AS
-- ONE OPERATION (bin/autodeploy does: build → migrate → restart), and a binary
-- rollback across this version must run the Down FIRST — after which the old
-- binary boots and answers with empty documents (see below).
--
-- NEW BINARY + OLD SCHEMA is harmless: the new code names neither the table nor
-- the column anywhere, so it runs unchanged on a 103 database and the migration
-- is what finally removes the storage. Nothing has to be sequenced for it.
--
-- IN-FLIGHT TASKS AND MEMBERS ARE NOT AFFECTED BY *THIS* STEP. No task, step or
-- member row holds anything that lived in the dropped storage: `learnings` was
-- an attribute of a task TYPE's manual and `lessons` of a ROLE, neither is part
-- of any task's or member's state, and no in-flight transition reads either.
-- What a mid-task agent DOES notice — a boot context written before the upgrade
-- still naming get_lessons / write_task_learnings, and those calls now answering
-- 404 — arrives with the BACKEND step (4f9a151a), from the moment the new binary
-- starts serving, and is already true before this migration runs. An agent that
-- hits it loses a tool call, not its task: its claim, its steps and its
-- handover are untouched, and a re-boot picks up the current tool list.
--
-- +goose Down
-- 🔴 THE STRUCTURE COMES BACK; THE CONTENT DOES NOT, AND THE TWO ARE SEPARATED
-- HERE RATHER THAN AVERAGED INTO ONE WORD. A `SELECT 1` no-op — what 00061 and
-- 00045 used — would be wrong for this migration even though it deletes just as
-- irreversibly, because those two only removed ROWS from tables that stayed.
-- This one removes a TABLE and a COLUMN, and a rollback exists to let an older
-- binary run: that binary issues `SELECT ... FROM lessons` and
-- `SELECT ..., learnings, ... FROM task_manual` on its very first read of
-- either surface, and against a schema this Down had left alone it would get
-- "no such table: lessons" / "no such column: learnings" instead of starting.
-- So the Down restores the SHAPE 00062 left behind, and the older binary boots
-- and answers with EMPTY documents.
--
-- 🔴 EMPTY, NOT RESTORED, AND NOBODY IS TOLD. The Up kept no copy of the
-- lessons rows, the learnings column's contents, or the retained revisions of
-- either. After a Down the old binary's get_lessons answers 200 with "", and
-- get_task_manual reports learnings "" — indistinguishable, from the wire, from
-- a station where nobody ever wrote one. That is the honest cost of rolling
-- back across this migration and it is stated here so it is not rediscovered
-- from a support thread. The document_history rows are gone for good too, so
-- the undo path that would otherwise have repopulated them is gone with them.
--
-- The two setting rows are NOT recreated: they were rows whose absence means
-- "use the code default" (settings.go), so an older binary reads its shipped
-- default for both — which is the correct behaviour, not a degradation, and is
-- the same choice 00048's Down made for these keys' predecessors.
CREATE TABLE lessons (
    role_key   TEXT PRIMARY KEY,
    text       TEXT NOT NULL DEFAULT '',
    tombstoned INTEGER NOT NULL DEFAULT 0
);
ALTER TABLE task_manual ADD COLUMN learnings TEXT NOT NULL DEFAULT '';
