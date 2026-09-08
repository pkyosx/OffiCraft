-- +goose Up
-- T-140 — 退場即撤銷. An endpoint must not outlive the member it delivers to.
--
-- WHAT WAS TRUE BEFORE THIS FILE: 00007 wrote the rule down as a decree —
-- "Deleting a member does not cascade — a stale endpoint simply stops
-- delivering (its member resolves absent at /in time and the post is dropped)".
-- That bought silence at the inlet and nothing else: the ROW survived, holding
-- a live bearer token, for as long as the database did. Measured on the
-- production database 2026-09-08: 7 endpoint rows, 2 of them owned by member
-- ids that no longer exist anywhere in the schema.
--
-- 🔴 WHY A TRIGGER AND NOT GO CODE. A member leaves through MANY doors — staff
-- dismissal (api_members.go), worker release by task and by id (dal_tasks.go),
-- the role-deletion hard delete (dal.go HardDeleteMember), machine removal and
-- warden teardown (api_machines.go) — twelve measured paths, and the two lists
-- that enumerate them are hand-maintained. A revocation hung off any subset of
-- those call sites is one new exit door away from being wrong, and NOTHING
-- would report it: the endpoint would simply still be there. The trigger asks
-- the only question that cannot be forgotten — did this row stop being a live
-- member — so a door written next year inherits the revocation by
-- construction. Same reasoning, and the same cost, as 00074's index triggers:
-- this is INVISIBLE from the Go side, so its guard is a test that writes the
-- member row DIRECTLY in SQL and asserts the endpoint is gone. If that test
-- passes, no Go code was involved.
--
-- 🔴 THE REVOCATION IS PERMANENT AND IRREVERSIBLE (owner, rc-f57020712e81 圈
-- A1). A released contractor called back for a second task does not get its old
-- token back; whatever external system was posting to it has to be re-pointed
-- at a new one.
--
-- 🔴 STOPPING IS NOT LEAVING (owner, rc-5cbf12a807d1 圈 [0] A1). 停用 /
-- 加速停止 / 強制停止 / 重新聚焦 touch no roster_status, so no trigger below
-- can see them, and that is the ruling — a stopped member is coming back.

-- ── (1) the one-time clean-up of what the decree already left behind ─────────
-- Endpoints whose member row is gone outright, or is soft-removed. Logs first:
-- webhook_request_log.token references the endpoint's primary key.
DELETE FROM webhook_request_log WHERE token IN (
    SELECT w.token FROM webhook_endpoint w
    LEFT JOIN member m ON m.id = w.member_id
    WHERE m.id IS NULL OR m.roster_status = 'removed'
);
DELETE FROM webhook_endpoint WHERE token IN (
    SELECT w.token FROM webhook_endpoint w
    LEFT JOIN member m ON m.id = w.member_id
    WHERE m.id IS NULL OR m.roster_status = 'removed'
);

-- ── (2) the endpoint's own cascade ──────────────────────────────────────────
-- The ring buffer is debug data FOR an endpoint, never an orphaned archive of a
-- dead token (dal.go DeleteWebhookEndpoint says so and does it by hand for the
-- revoke verb). Stated once here so it also holds for the member triggers
-- below, and for any future deleter. BEFORE, so the child rows go while the
-- parent is still there — the FK is declared even though SQLite does not
-- enforce it by default in this process.
-- +goose StatementBegin
CREATE TRIGGER webhook_endpoint_bd_logs BEFORE DELETE ON webhook_endpoint
BEGIN
    DELETE FROM webhook_request_log WHERE token = OLD.token;
END;
-- +goose StatementEnd

-- ── (3) 退場 ⇒ the endpoints go ─────────────────────────────────────────────
-- 🔴 BARE `AFTER UPDATE` WITH A `WHEN`, NOT `AFTER UPDATE OF roster_status`.
-- Narrowing to the column would make the revocation depend on which columns a
-- writer happens to name in its UPDATE statement — PutMember patches a computed
-- field list — and a writer that reaches 'removed' by any other column list
-- would skip it SILENTLY. The `WHEN` is on the resulting STATE, not on the
-- transition (no `OLD.roster_status <> 'removed'`): re-running it on a row that
-- is already removed deletes nothing and costs one indexed lookup, whereas a
-- transition test is one more way to miss.
-- +goose StatementBegin
CREATE TRIGGER webhook_revoke_on_member_removed AFTER UPDATE ON member
FOR EACH ROW WHEN NEW.roster_status = 'removed'
BEGIN
    DELETE FROM webhook_endpoint WHERE member_id = NEW.id;
END;
-- +goose StatementEnd

-- The hard-delete door: a member whose ROLE is deleted loses its row outright
-- (dal.go HardDeleteMember), so it never passes through the update above.
-- +goose StatementBegin
CREATE TRIGGER webhook_revoke_on_member_deleted AFTER DELETE ON member
FOR EACH ROW
BEGIN
    DELETE FROM webhook_endpoint WHERE member_id = OLD.id;
END;
-- +goose StatementEnd

-- +goose Down
-- The deleted rows are NOT restored: this migration's whole point is that the
-- revocation is permanent, and a token that has been revoked must not come back
-- because someone stepped a schema version backwards.
DROP TRIGGER IF EXISTS webhook_revoke_on_member_deleted;
DROP TRIGGER IF EXISTS webhook_revoke_on_member_removed;
DROP TRIGGER IF EXISTS webhook_endpoint_bd_logs;
