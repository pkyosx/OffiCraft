package main

// migration_00086_blank_link_url_test.go — the row shape that would have taken a
// running station down at boot.
//
// 00086 mints one text/uri-list blob per distinct link url and then looks every
// link row's blob up in that temp table with NO fallback, into a column that has
// no DEFAULT. While the mint filtered on `url <> ''`, a single link row with a
// blank url produced NULL for a NOT NULL column: goose rolls the migration back,
// runMigrations returns an error, and the server EXITS AT BOOT WITHOUT LISTENING.
// Not a failed migration — an outage, on a station that was running a minute
// earlier.
//
// What stood between the tree and that outage was a sentence in the migration's
// own header — "link rows with a blank url .. 0" — measured once on one snapshot.
// Re-measured on 2026-09-06 it is still 0, and the API has refused a blank link
// url since the initial tree, so no supported path creates one. That is why this
// is a cheap fix rather than an urgent one; it is not why the fix is optional. A
// measurement describes one database and the migration meets every database.
//
// The mint no longer filters, so a blank url gets its own (empty) blob like any
// other target. This test seeds exactly that row and asserts the migration
// completes — it fails by ERRORING, which is precisely how the defect presented.

import (
	"testing"
)

// TestMigration00086SurvivesALinkRowWithABlankURL seeds one blank-url link into
// each artifact table and runs the migration over them.
func TestMigration00086SurvivesALinkRowWithABlankURL(t *testing.T) {
	db := t92World(t)

	if _, err := db.Exec(
		`INSERT INTO task_artifact (id, task_id, kind, attachment_id, url, label, created_ts, created_by)
		 VALUES ('ta-blank0001', 't-alpha', 'link', '', '', 'a link that points nowhere', 1, 'm-x')`,
	); err != nil {
		t.Fatalf("seed blank-url link: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO task_artifact_history (artifact_id, kind, attachment_id, url, label, created_ts, created_by)
		 VALUES ('ta-blank0001', 'link', '', '', 'an older nowhere', 1, 'm-x')`,
	); err != nil {
		t.Fatalf("seed blank-url link history: %v", err)
	}

	// Before the fix this call failed here, with
	// "NOT NULL constraint failed: task_artifact_rebuild.attachment_id" — a
	// message naming an internal rebuild table and not the offending row.
	t92Up(t, db)

	for _, c := range []struct{ table, where string }{
		{"task_artifact", `id = 'ta-blank0001'`},
		{"task_artifact_history", `artifact_id = 'ta-blank0001'`},
	} {
		var attID string
		if err := db.QueryRow(
			`SELECT attachment_id FROM ` + c.table + ` WHERE ` + c.where).Scan(&attID); err != nil {
			t.Fatalf("read back %s: %v", c.table, err)
		}
		if attID == "" {
			t.Fatalf("%s: the blank-url link came through with no blob — the invariant "+
				"00086 exists to establish is that EVERY kind is blob-backed", c.table)
		}

		// The blob is real, is the right media type, and holds the url it was
		// minted from: empty. A row that reached here with a dangling id would
		// satisfy the check above and still be broken.
		var mime string
		var data []byte
		if err := db.QueryRow(
			`SELECT mime, data FROM chat_attachment WHERE id = ?`, attID).Scan(&mime, &data); err != nil {
			t.Fatalf("%s: blob %s does not exist: %v", c.table, attID, err)
		}
		if mime != "text/uri-list" {
			t.Fatalf("%s: blob %s has mime %q, want text/uri-list", c.table, attID, mime)
		}
		if len(data) != 0 {
			t.Fatalf("%s: blob %s holds %q, want the empty url it was minted from",
				c.table, attID, data)
		}
	}
}
