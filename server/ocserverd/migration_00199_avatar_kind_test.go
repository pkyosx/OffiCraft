package main

// migration_00199_avatar_kind_test.go — T-57.
//
// 🔴 THE FIRST TEST IN THIS FILE IS THE ONE THE TICKET IS ABOUT, and it is
// written the way it is on purpose. The bug T-57 removes is that a station whose
// CODE says `staff` while its DATA still says `member` shows the built-in glyph
// instead of the owner's picture, and NOTHING objects — no 422, no log line, no
// red test. So the test asserts the whole round trip: an old bundle goes into
// the table, the migration runs, and the theme is read back THROUGH THE
// PRODUCTION READ PATH and asked for the image under the name the cockpit looks
// for today. Emptying upAvatarKindMemberToStaff makes exactly this go red.
//
// ⚠️ The fixture deliberately carries a value that is NOT also present under the
// new key, so "migrated" and "was already fine" cannot return the same answer.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const avatarKindTestImage = "data:image/png;base64,AAAA-the-owners-正職-picture"

func TestMigration00199AvatarKindRename(t *testing.T) {
	t.Run("an old bundle's member image is readable as staff after the migration", func(t *testing.T) {
		d := newAPITestDAL(t)
		seedAvatarKindTheme(t, d, "dusk", map[string]string{
			"member":    avatarKindTestImage,
			"outsource": "data:image/png;base64,BBBB",
		})

		runAvatarKindMigration(t, d, upAvatarKindMemberToStaff)

		avatars := readAvatarsThroughDAL(t, d, "dusk")
		if got := avatars["staff"]; got != avatarKindTestImage {
			t.Fatalf("after the migration avatars[\"staff\"] = %q, want the owner's picture %q — "+
				"the cockpit asks for `staff` and would paint the built-in glyph instead, silently",
				got, avatarKindTestImage)
		}
		if _, still := avatars["member"]; still {
			t.Fatalf("avatars still carries the retired key \"member\": %v", avatars)
		}
		if got := avatars["outsource"]; got != "data:image/png;base64,BBBB" {
			t.Fatalf("the untouched kinds must survive: avatars = %v", avatars)
		}
		if r := skipReceipt(t, d); r != nil {
			t.Fatalf("a clean rename must leave NO skip receipt, got %q", *r)
		}
	})

	t.Run("down puts it back, so a rollback and the older binary still agree", func(t *testing.T) {
		d := newAPITestDAL(t)
		seedAvatarKindTheme(t, d, "dusk", map[string]string{"member": avatarKindTestImage})

		runAvatarKindMigration(t, d, upAvatarKindMemberToStaff)
		runAvatarKindMigration(t, d, downAvatarKindMemberToStaff)

		avatars := readAvatarsThroughDAL(t, d, "dusk")
		if got := avatars["member"]; got != avatarKindTestImage {
			t.Fatalf("after down avatars[\"member\"] = %q, want %q — an empty or partial down "+
				"leaves the older binary looking for a key the data no longer has",
				got, avatarKindTestImage)
		}
		if _, still := avatars["staff"]; still {
			t.Fatalf("down must remove the new key, got %v", avatars)
		}
	})

	t.Run("the id and every other field survive, so both CHECK constraints hold", func(t *testing.T) {
		d := newAPITestDAL(t)
		seedAvatarKindTheme(t, d, "dusk", map[string]string{"member": avatarKindTestImage})

		runAvatarKindMigration(t, d, upAvatarKindMemberToStaff)

		// Reaching the row at all proves custom_theme_id_matches_bundle and
		// custom_theme_bundle_is_json were not tripped by the rewrite: SQLite
		// would have refused the UPDATE and the migration would have errored.
		var bundle string
		if err := d.wdb.QueryRow(
			`SELECT bundle FROM custom_theme WHERE theme_id = 'dusk'`).Scan(&bundle); err != nil {
			t.Fatalf("read back the rewritten bundle: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(bundle), &doc); err != nil {
			t.Fatalf("the rewritten bundle is not JSON: %v", err)
		}
		if doc["id"] != "dusk" || doc["name"] != "Dusk" {
			t.Fatalf("the rewrite must not disturb any field but avatars, got %v", doc)
		}
	})

	t.Run("a bundle carrying BOTH keys is left alone and RECORDED, not silently skipped", func(t *testing.T) {
		d := newAPITestDAL(t)
		seedAvatarKindTheme(t, d, "dusk", map[string]string{
			"member": avatarKindTestImage,
			"staff":  "data:image/png;base64,CCCC",
		})

		var log bytes.Buffer
		withAvatarKindMigrationLog(t, &log, func() {
			runAvatarKindMigration(t, d, upAvatarKindMemberToStaff)
		})

		avatars := readAvatarsThroughDAL(t, d, "dusk")
		if avatars["member"] != avatarKindTestImage || avatars["staff"] != "data:image/png;base64,CCCC" {
			t.Fatalf("neither image may be discarded, got %v", avatars)
		}
		receipt := skipReceipt(t, d)
		if receipt == nil {
			t.Fatalf("a skipped row MUST leave a receipt — a silent skip is the bug this ticket removes")
		}
		var skips []avatarKindSkip
		if err := json.Unmarshal([]byte(*receipt), &skips); err != nil {
			t.Fatalf("skip receipt is not the documented shape: %v (%q)", err, *receipt)
		}
		if len(skips) != 1 || skips[0].ThemeID != "dusk" {
			t.Fatalf("skip receipt = %+v, want one entry naming theme dusk", skips)
		}
		if !strings.Contains(log.String(), "dusk") {
			t.Fatalf("the skip must also be announced where an operator watching the upgrade sees it, got %q", log.String())
		}
	})

	t.Run("a malformed avatars overlay is skipped, and the upgrade still succeeds", func(t *testing.T) {
		d := newAPITestDAL(t)
		// avatars as an ARRAY: not a shape the write path produces, but one the
		// database has never refused to hold. Failing here would brick an
		// upgrade over data that boots fine today.
		seedAvatarKindRaw(t, d, "odd", `{"id":"odd","name":"Odd","avatars":[1,2,3]}`)
		seedAvatarKindTheme(t, d, "dusk", map[string]string{"member": avatarKindTestImage})

		var log bytes.Buffer
		withAvatarKindMigrationLog(t, &log, func() {
			runAvatarKindMigration(t, d, upAvatarKindMemberToStaff)
		})

		// The healthy row is still migrated: one odd theme must not stop the rest.
		if got := readAvatarsThroughDAL(t, d, "dusk")["staff"]; got != avatarKindTestImage {
			t.Fatalf("a skipped row must not stop the others, dusk avatars[staff] = %q", got)
		}
		if r := skipReceipt(t, d); r == nil || !strings.Contains(*r, "odd") {
			t.Fatalf("the odd row must be recorded, receipt = %v", r)
		}
	})

	t.Run("a clean re-run DELETES a stale receipt rather than leaving it asserting a loss", func(t *testing.T) {
		d := newAPITestDAL(t)
		seedAvatarKindRaw(t, d, "odd", `{"id":"odd","name":"Odd","avatars":[1,2,3]}`)

		var log bytes.Buffer
		withAvatarKindMigrationLog(t, &log, func() {
			runAvatarKindMigration(t, d, upAvatarKindMemberToStaff)
		})
		if skipReceipt(t, d) == nil {
			t.Fatalf("precondition: the first run must have left a receipt")
		}

		// Repair the row the way an operator would, then re-run.
		if _, err := d.wdb.Exec(
			`UPDATE custom_theme SET bundle = ? WHERE theme_id = 'odd'`,
			`{"id":"odd","name":"Odd"}`); err != nil {
			t.Fatalf("repair the odd row: %v", err)
		}
		withAvatarKindMigrationLog(t, &log, func() {
			runAvatarKindMigration(t, d, upAvatarKindMemberToStaff)
		})
		if r := skipReceipt(t, d); r != nil {
			t.Fatalf("a clean re-run must REMOVE the stale receipt, still %q", *r)
		}
	})

	t.Run("a theme with no avatars overlay is untouched and is not a skip", func(t *testing.T) {
		d := newAPITestDAL(t)
		seedAvatarKindRaw(t, d, "plain", `{"id":"plain","name":"Plain","colors":{"bg":"#101010"}}`)

		runAvatarKindMigration(t, d, upAvatarKindMemberToStaff)

		var bundle string
		if err := d.wdb.QueryRow(
			`SELECT bundle FROM custom_theme WHERE theme_id = 'plain'`).Scan(&bundle); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if bundle != `{"id":"plain","name":"Plain","colors":{"bg":"#101010"}}` {
			t.Fatalf("a bundle with nothing to rename must be left byte-for-byte alone, got %q", bundle)
		}
		if r := skipReceipt(t, d); r != nil {
			t.Fatalf("nothing to rename is not a skip, receipt = %q", *r)
		}
	})
}

// TestMigration00199ProvisionalNumberIsDeclared is the mechanical half of the
// "do not merge on a provisional number" rule. It cannot allocate the number,
// but it CAN refuse to let the placeholder pass as a decision: the file must say
// out loud that 00199 is provisional. Whoever reallocates deletes this test with
// the marker.
func TestMigration00199ProvisionalNumberIsDeclared(t *testing.T) {
	src := readMigration00199Source(t)
	if !strings.Contains(src, "PROVISIONAL AND MUST BE REALLOCATED BEFORE THIS MERGES") {
		t.Fatalf("migration 00199 carries a provisional number; the file must declare it so the " +
			"number is reallocated at merge time rather than shipped as picked")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func readMigration00199Source(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("migration_00199_avatar_kind_member_to_staff.go")
	if err != nil {
		t.Fatalf("read the migration source: %v", err)
	}
	return string(b)
}

func seedAvatarKindTheme(t *testing.T, d *DAL, id string, avatars map[string]string) {
	t.Helper()
	blob, err := json.Marshal(map[string]any{
		"id": id, "name": "Dusk", "colors": map[string]string{"bg": "#101010"},
		"avatars": avatars,
	})
	if err != nil {
		t.Fatalf("marshal fixture bundle: %v", err)
	}
	seedAvatarKindRaw(t, d, id, string(blob))
}

// seedAvatarKindRaw writes the row with raw SQL ON PURPOSE. PutCustomTheme runs
// the write-path validators, which after T-57 REFUSE an `avatars.member` key —
// the fixture this migration exists for is precisely a row the product will no
// longer write but has always been willing to hold.
func seedAvatarKindRaw(t *testing.T, d *DAL, id, bundle string) {
	t.Helper()
	if _, err := d.wdb.Exec(
		`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, 0, 0)`,
		id, bundle); err != nil {
		t.Fatalf("seed custom_theme row %q: %v", id, err)
	}
}

// readAvatarsThroughDAL reads the theme back the way the server does, so the
// assertion is about what the cockpit would be served and not about a string in
// a column this test wrote itself.
func readAvatarsThroughDAL(t *testing.T, d *DAL, id string) map[string]string {
	t.Helper()
	theme, err := d.GetCustomTheme(id)
	if err != nil {
		t.Fatalf("GetCustomTheme(%q): %v", id, err)
	}
	if theme == nil {
		t.Fatalf("GetCustomTheme(%q) = nil, want the seeded theme", id)
	}
	var doc struct {
		Avatars map[string]string `json:"avatars"`
	}
	if err := json.Unmarshal([]byte(theme.Bundle), &doc); err != nil {
		t.Fatalf("theme %q bundle is not JSON: %v", id, err)
	}
	return doc.Avatars
}

func runAvatarKindMigration(t *testing.T, d *DAL, migration func(context.Context, *sql.Tx) error) {
	t.Helper()
	tx, err := d.wdb.Begin()
	if err != nil {
		t.Fatalf("begin migration transaction: %v", err)
	}
	if err := migration(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("migration: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration transaction: %v", err)
	}
}

func withAvatarKindMigrationLog(t *testing.T, w *bytes.Buffer, fn func()) {
	t.Helper()
	previous := avatarKindMigrationLog
	avatarKindMigrationLog = w
	defer func() { avatarKindMigrationLog = previous }()
	fn()
}

func skipReceipt(t *testing.T, d *DAL) *string {
	t.Helper()
	v, err := d.GetSetting(avatarKindSkipRecordKey)
	if err != nil {
		t.Fatalf("GetSetting(%q): %v", avatarKindSkipRecordKey, err)
	}
	return v
}
