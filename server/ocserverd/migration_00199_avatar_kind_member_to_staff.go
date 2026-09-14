package main

// migration_00199_avatar_kind_member_to_staff.go — T-57, the DATA half of
// renaming the avatar kind `member` to `staff`.
//
// 🔴 THE NUMBER 00199 IS PROVISIONAL AND MUST BE REALLOCATED BEFORE THIS MERGES.
// Taking a migration number is the LAST action before a merge, not the first:
// the door is shared, and a number picked at the start of a branch is a number
// somebody else has taken by the end of it. 00199 is deliberately far above the
// live range (main's maximum was 00106 when this was written) so it is obviously
// a placeholder rather than a plausible neighbour.
//
// Whoever reallocates it must change SEVEN things, in one pass. The count is
// spelled out because an earlier draft of this paragraph listed five and quietly
// omitted two of them — and a checklist that is one item short reads exactly
// like a complete one:
//
//  1. this file's NAME;
//  2. the string passed to AddNamedMigrationContext below;
//  3. every "00199" inside the error and log messages in this file;
//  4. the VALUE of avatarKindSkipRecordKey — it carries the number INSIDE a
//     settings key, so a rename here changes a string that has already been
//     written into real databases by any station that ran the placeholder;
//  5. the TEST FILE'S OWN NAME (migration_00199_avatar_kind_test.go);
//  6. the expectations inside that test file, including the filename literal
//     readMigration00199Source() opens — it reads this source by hard-coded
//     path and will fail to find it the moment item 1 happens;
//  7. then regenerate server/ocserverd/migration.lock with bin/gen-migration-lock.
//
// `grep -rn 00199 server/ocserverd` enumerates 1-6; item 7 is not greppable.
// 🔴 A number BELOW the station's current version makes goose return an error
// and a number that COLLIDES makes it panic while collecting migrations — both
// of which mean the server does not come up. The acceptance test is "the server
// starts", not "the number reads nicely".
//
// ── WHY THIS EXISTS ─────────────────────────────────────────────────────────
//
// The avatar kind `member` is an INTERNAL code, and T-57 renames it to `staff`
// so it stops colliding with the unrelated member-category vocabulary (where
// `staff` already means 正職). The code half of that rename is cheap. The data
// half is this file, and without it the rename produces the WORST available
// failure rather than a loud one:
//
//	a saved custom theme stores its whole bundle as raw JSON in custom_theme.bundle,
//	with the picked images under `avatars`. The READ path does not validate avatar
//	kinds — an unknown key is handed to the cockpit untouched and GET answers 200 —
//	so after the code rename the cockpit looks for `avatars.staff`, does not find it,
//	and paints the built-in glyph. The owner's chosen 正職 avatar disappears, and
//	NOTHING logs, errors or 422s. That silence is the ticket.
//
// So the rewrite must happen, and it must happen in the same package as the code
// rename — a station that upgrades the binary without this migration is exactly
// the broken state above.
//
// ── WHY GO AND NOT .sql ─────────────────────────────────────────────────────
//
// `avatars` is a dynamic KEY inside a JSON document, not a column value. SQLite
// can express the rename with json_set/json_remove, but the two exception cases
// below (a row that already carries BOTH keys; a bundle that is not the expected
// shape) have to be DETECTED and SKIPPED WITH A RECEIPT rather than silently
// no-op'd, and that decision does not fit in an UPDATE ... WHERE. Go migrations
// cannot live under migrations/ (that directory is embedded as *.sql); find them
// with `grep -rn AddNamedMigrationContext server/ocserverd`, not by listing the
// directory.
//
// ── THE TWO CHECK CONSTRAINTS THIS MUST NOT BREAK ───────────────────────────
//
// custom_theme carries `custom_theme_bundle_is_json CHECK (json_valid(bundle))`
// and `custom_theme_id_matches_bundle CHECK (json_extract(bundle,'$.id') IS
// theme_id)`. This migration re-marshals the bundle, so:
//   - json_valid holds because the value written is the output of a successful
//     json.Marshal over a map decoded from a value json.Unmarshal accepted;
//   - the id survives byte-for-byte because every field other than `avatars` is
//     carried as json.RawMessage and written back verbatim, `$.id` included.
//
// ⚠️ The bundle's KEY ORDER is not preserved (Go marshals a map in sorted key
// order). Nothing in the tree depends on it: the read path unmarshals into a
// DTO, and the byte-for-byte requirement that 00059 carried was about proving a
// copy against the legacy settings row, which no longer exists here.
//
// ── FAILURE POSTURE: SKIP WITH A RECEIPT, NEVER FAIL THE UPGRADE ────────────
//
// A row this migration cannot confidently rewrite is SKIPPED and RECORDED, not
// fatal. The reason is the one 00059 already paid for: such rows PARSE, so the
// station carrying them boots today; turning "one of your themes is odd" into
// "your station does not come up after the upgrade" is a strictly worse outcome.
// But a silent skip would reintroduce the very bug this ticket is about, so
// every skip lands in the database under avatarKindSkipRecordKey and is also
// announced on stderr. ABSENCE of that row is the "nothing was left behind"
// answer, so the healthy install pays nothing.
//
// Two skip shapes, both named in the plan this was built from:
//
//   - BOTH KEYS PRESENT (`member` AND `staff` in one avatars object). The import
//     path refuses such a bundle, but the DATABASE has never refused to hold one.
//     Overwriting `staff` with the old `member` image would destroy a picture the
//     owner can still see; dropping `member` would throw away the one this
//     migration exists to carry. Neither is ours to choose, so the row is left
//     exactly as it is and the receipt says so.
//   - NOT THE EXPECTED SHAPE (bundle is not a JSON object, or `avatars` is
//     present but is not an object of strings). Nothing to rename.
//
// A row with no `avatars` at all, or with `avatars` that has no `member` key, is
// not a skip — it is simply untouched, and untouched is the correct answer.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("00199_avatar_kind_member_to_staff.go",
		upAvatarKindMemberToStaff, downAvatarKindMemberToStaff)
}

// avatarKindMigrationLog is where the skip announcements go.
//
// 🔴 IT IS A VARIABLE SO THE ANNOUNCEMENT CAN BE ASSERTED ON — the same reason
// 00100 made its log a variable. A report no test can see is a report the next
// person deletes as noise with every test still green. Production behaviour is
// unchanged: os.Stderr, where goose's own lines already go.
var avatarKindMigrationLog io.Writer = os.Stderr

// The two kind names, as LITERALS. Deliberately not references to
// avatarKindAllowed / avatarKindRetired in avatar_bundle.go: a migration must
// keep naming the world as it was when it ran, and those identifiers are free to
// be renamed or deleted by a later change without this file's meaning changing.
const (
	avatarKindLegacy  = "member"
	avatarKindRenamed = "staff"
)

// avatarKindSkipRecordKey is where a skip is recorded. A setting key rather than
// a table because this is a one-shot RECEIPT, not runtime state — the same shape
// 00059 established for `display.custom_themes.skipped_by_00059`. Settings are
// read one key at a time and nothing enumerates the key space, so an extra row
// is inert to settings load, to GET /api/settings and to the cockpit.
//
// 🔴 NOTHING IN THIS TREE READS THIS ROW TODAY, AND THAT IS STATED HERE SO THE
// NEXT PERSON DOES NOT ASSUME A SAFETY NET THAT IS NOT THERE. Measured, not
// argued: the only mentions of this key anywhere are this file and its test
// (`grep -rn avatarKindSkipRecordKey`). There is no startup check, no admin
// surface, no health signal and no API face that surfaces it — so on a real
// station a skip is visible ONLY in the stderr line that scrolls past during
// the upgrade, plus this row for whoever later thinks to query the setting
// table by hand. The row is written so the evidence EXISTS at all, which is
// strictly better than a silent skip; it is not a mechanism that will bring a
// skip to anybody's attention on its own. Giving it a reader is a separate
// decision and a separate ticket — do not quietly add one here, and do not
// quietly write a sentence claiming it already has one.
//
// 🔴 REALLOCATING THE NUMBER MUST RENAME THIS KEY TOO — it carries 00199.
const avatarKindSkipRecordKey = "display.custom_theme.avatar_kind_skipped_by_00199"

// avatarKindSkip is one custom_theme row the rewrite declined to touch.
type avatarKindSkip struct {
	ThemeID string `json:"theme_id"`
	Reason  string `json:"reason"`
}

func upAvatarKindMemberToStaff(ctx context.Context, tx *sql.Tx) error {
	return renameAvatarKind(ctx, tx, avatarKindLegacy, avatarKindRenamed)
}

// downAvatarKindMemberToStaff is the genuine reverse, and it is NOT optional.
//
// 🔴 AN EMPTY DOWN WOULD BE THE BUG, NOT A SHORTCUT. The whole point of this
// migration is that the code and the data turn over together. Rolling back to
// the older binary with the data already renamed leaves that binary looking for
// `avatars.member` against rows that now say `staff` — which is the same silent
// disappearing avatar, just pointed the other way.
func downAvatarKindMemberToStaff(ctx context.Context, tx *sql.Tx) error {
	return renameAvatarKind(ctx, tx, avatarKindRenamed, avatarKindLegacy)
}

// renameAvatarKind moves avatars[from] to avatars[to] in every custom_theme
// bundle, and writes the skip receipt for the rows it declined. Both directions
// are the same operation with the two names swapped, which is what makes down a
// real retreat rather than a second, separately-wrong implementation.
func renameAvatarKind(ctx context.Context, tx *sql.Tx, from, to string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT theme_id, bundle FROM custom_theme ORDER BY theme_id`)
	if err != nil {
		return fmt.Errorf("migration 00199: read custom_theme: %w", err)
	}
	type pending struct {
		themeID string
		bundle  string
	}
	var all []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.themeID, &p.bundle); err != nil {
			rows.Close()
			return fmt.Errorf("migration 00199: scan custom_theme row: %w", err)
		}
		all = append(all, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("migration 00199: iterate custom_theme: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("migration 00199: close custom_theme: %w", err)
	}

	var skipped []avatarKindSkip
	skip := func(themeID, why string) {
		skipped = append(skipped, avatarKindSkip{ThemeID: themeID, Reason: why})
		fmt.Fprintf(avatarKindMigrationLog,
			"migration 00199: theme %q left as it is — %s\n", themeID, why)
	}

	for _, p := range all {
		// Only `avatars` is decoded past the top level. Every other field stays
		// as the bytes that were stored, which is what keeps `$.id` — and with
		// it the custom_theme_id_matches_bundle CHECK — untouched.
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(p.bundle), &doc); err != nil {
			skip(p.themeID, "its bundle is not a JSON object")
			continue
		}
		rawAvatars, ok := doc["avatars"]
		if !ok {
			continue // no avatars overlay at all: nothing to rename.
		}
		var avatars map[string]string
		if err := json.Unmarshal(rawAvatars, &avatars); err != nil {
			skip(p.themeID, "its avatars overlay is not an object of strings")
			continue
		}
		img, hasFrom := avatars[from]
		if !hasFrom {
			continue // already in the target vocabulary, or never had this kind.
		}
		if _, hasTo := avatars[to]; hasTo {
			// 🔴 NEITHER IMAGE IS OURS TO DISCARD. See the failure posture at
			// the top of this file.
			skip(p.themeID, fmt.Sprintf(
				"its avatars overlay carries BOTH %q and %q; renaming would overwrite one of two "+
					"images the owner picked, so both are left in place", from, to))
			continue
		}
		delete(avatars, from)
		avatars[to] = img
		reAvatars, err := json.Marshal(avatars)
		if err != nil {
			return fmt.Errorf("migration 00199: re-encode avatars for theme %q: %w", p.themeID, err)
		}
		doc["avatars"] = reAvatars
		reBundle, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("migration 00199: re-encode bundle for theme %q: %w", p.themeID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE custom_theme SET bundle = ? WHERE theme_id = ?`,
			string(reBundle), p.themeID); err != nil {
			return fmt.Errorf("migration 00199: rewrite theme %q: %w", p.themeID, err)
		}
	}
	return recordAvatarKindSkips(ctx, tx, skipped)
}

// recordAvatarKindSkips writes the receipt, and DELETES it when there is nothing
// to record.
//
// 🔴 "NO SKIPS" MEANS IT DELETES, NOT MERELY THAT IT DOES NOT WRITE — the trap
// 00059 documented. Returning early would make the receipt a claim about the
// FIRST time this migration ever ran: up on a database with an odd row leaves a
// receipt, down retreats, the row is repaired, up runs again cleanly, and the
// stale receipt is still there asserting a loss that no longer exists.
func recordAvatarKindSkips(ctx context.Context, tx *sql.Tx, skipped []avatarKindSkip) error {
	if len(skipped) == 0 {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM setting WHERE key = ?`, avatarKindSkipRecordKey)
		return err
	}
	blob, err := json.Marshal(skipped)
	if err != nil {
		return fmt.Errorf("migration 00199: record skips: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, 0)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		avatarKindSkipRecordKey, string(blob)); err != nil {
		return fmt.Errorf("migration 00199: record skips: %w", err)
	}
	return nil
}
