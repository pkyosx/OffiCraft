package main

// migration_00059_custom_theme_table.go — T-83ef, the storage half of moving
// custom themes OUT of the settings row they have lived in since T-16a1 P2.
//
// 🔴 WHY THIS EXISTS — stated as of the world this migration was written
// AGAINST, which is the world it runs on and the one it removes. Every saved
// theme, INCLUDING every image the owner picked (avatars, logo, nav icons,
// canvas backgrounds — all base64 data: URIs), WAS serialised into ONE settings
// value under the key `display.custom_themes`, written by whole-array replace.
// Two consequences, both measured rather than argued:
//
//   - Editing ONE theme resent ALL of them. There was no partial write path at
//     all (api_settings.go: json.Marshal(newCustomThemes) → PutSetting).
//   - That one value dominated the settings payload — measured at 98% of it.
//     🔴 The exact byte counts are DELIBERATELY not repeated here. The pair that
//     used to be quoted (639,270 total / 626,721 for the themes) was measured in
//     2026-07 and was already stale by 2026-08, when the same field measured
//     1,592,133 bytes — see frontend/src/lib/sharedSnapshot.ts, which now carries
//     that history with its dates attached. A number copied into a second file
//     goes stale twice as quietly: the reader cannot tell a figure someone
//     measured from a figure someone inherited. The RATIO is the load-bearing
//     part and it is what this paragraph keeps.
//     The same weight is what made the `get_settings` MCP tool unusable for
//     agents that only wanted a boolean out of it.
//
// One theme per ROW is what makes "write one theme" expressible at all.
//
// 🔴 WHY THE BUNDLE IS STORED AS THE ELEMENT'S RAW JSON, NOT AS COLUMNS. The
// ticket's hard requirement is that the moved data reads back BYTE-FOR-BYTE
// identical before anything old is removed. Copying each array element's own
// bytes verbatim makes that a mechanical fact: re-joining the rows in order
// reproduces the original array string exactly — WHEN NOTHING WAS SKIPPED, which
// is every healthy install and is the state the skip record below reports. On an
// install that DID leave something behind the rejoin is necessarily shorter than
// the legacy value, and comparing them proves nothing; the skip record, not the
// diff, is what speaks for those. A diff proves it in the first case. Unpacking
// into columns would round-trip through unmarshal/marshal, where key order,
// whitespace and Unicode escaping are all free to change — the reassembled
// bytes could differ while nothing is actually wrong, and then the one check
// that is supposed to authorise the switch can no longer be run at all.
//
// 🔴 WHY IT IS A GO MIGRATION. Splitting a JSON array requires parsing JSON.
// SQLite cannot, so a .sql migration cannot express this. It is the second Go
// migration in this repo (00054 was the first); Go migrations cannot live under
// migrations/ because that directory is embedded as *.sql — runMigrations in
// migrate.go says so, and `grep -rn AddNamedMigrationContext server/ocserverd`
// is how you find them.
//
// 🔴 THE OLD SETTINGS ROW IS NOT TOUCHED. Up copies; it does not move and it
// does not delete. Both representations exist after this migration, which is
// what makes Down a genuine retreat (drop the table and the older binary reads
// exactly what it read before, because it was never edited) and what lets the
// byte-for-byte comparison be run against a live pair rather than a backup.
// Retiring `display.custom_themes` is a SEPARATE decision on a later change,
// once the new path has actually carried the owner's data.
//
// 🔴 WHILE BOTH EXIST, THE LEGACY ROW IS THE TRUTH — and whoever writes the
// per-theme endpoints has to know that before writing them. This migration is a
// one-way copy: nothing keeps the two representations in step afterwards. So a
// write that lands ONLY in `custom_theme` makes `GET /api/settings` and the new
// table disagree, silently, with the settings face still serving the pre-upgrade
// answer. Two ways out, and the choice belongs to the change that adds the
// endpoints, not to this one: either that change retires the legacy row in the
// same package (so there is only one truth), or its write path maintains BOTH
// until it does. The same file carries the retirement precondition below.
//
// 🔴 RETIREMENT PRECONDITION — AND IT IS A QUERY, NOT A THING TO REMEMBER.
//
// Up is deliberately lossy-tolerant: it SKIPS elements it cannot key (see the
// failure posture) rather than failing, which is safe only for as long as the
// legacy row is still there to hold them. So the change that eventually deletes
// `display.custom_themes` must refuse while anything was left behind.
//
// A sentence in this header cannot enforce that. Whoever retires the legacy row
// months from now has no reason to open this file, and if they do not, the
// skipped themes vanish at that moment with nothing raising a word. So every
// skip is RECORDED IN THE DATABASE under customThemeSkipRecordKey, and the rule
// for that later change is:
//
//	if the key exists → REFUSE, print what is in it, and print the way out.
//	if it is absent   → nothing was left behind; retiring is safe.
//
// 🔴 AND HERE IS WHAT THAT RULE DOES *NOT* COVER, stated plainly because the
// wording above reads like a complete guard and is not one. The receipt is read
// by whoever DELETES the legacy row. But a theme stops being reachable the
// moment the legacy row stops being SERVED — and that is a different, EARLIER
// event: T-83ef cuts `custom_themes` off the settings wire while deliberately
// leaving the row in the database as a rollback path. From that release on, a
// skipped theme is gone from the product, and nothing has read this receipt yet.
// The guard is anchored to deletion; the user-visible loss happens at the wire
// cut. Do not read "refuse while the key exists" as protecting the themes — it
// protects the BYTES, and only at the later event.
//
// Two things stop that from being a live hazard today, and they are of different
// kinds — do not merge them:
//   - MEASURED, on the install this ticket moved: its four themes all carried
//     usable ids and the receipt row did not exist. That is one install at one
//     moment, re-checked against the land candidate (T-83ef step 11); it says
//     nothing about anyone else's database.
//   - MECHANICAL, and this one is general: the FIRST version of the validator
//     (92628c80, the same commit that introduced this settings key) already
//     required every bundle's id to match `^[a-z0-9][a-z0-9-]{1,63}$`, byte for
//     byte the rule in force today, and the stored value is always
//     json.Marshal's output rather than caller-supplied text. So no release of
//     this product could ever store an element Up would fail to key: a receipt
//     can only come from a hand-edited or corrupted database.
//
// Moving the guard to the earlier event is a real change with a real cost and is
// NOT done here — it is recorded as follow-up work, not quietly assumed.
//
// ⚠️ THE RULE IS "KEY EXISTS", NOT "ROW COUNT EQUALS ARRAY LENGTH". An earlier
// draft said the latter and it was unsatisfiable by construction: on an install
// that skipped something, the counts can NEVER match, so that phrasing would
// have locked those installs out of retirement forever with no way forward. A
// gate with no exit gets bypassed by hand, which is worse than no gate — it also
// looks like someone is guarding. Hence the exit is part of the rule: resolve
// the listed elements (re-add them under usable ids, or decide they are not
// wanted), DELETE the record key, then retire.
//
// ⚠️ HOW MUCH OF THAT EXIT EXISTS TODAY, stated plainly so nobody plans against
// a capability that is not here: deleting the key is straightforward (the
// retiring migration does it itself). "Re-add them under usable ids" has NO
// executable path at the time of writing — there is no per-theme write endpoint
// yet, and `PATCH /api/settings` writes the legacy row, not this table. Whoever
// retires the legacy row either arrives after those endpoints exist, or does the
// re-adding in their own migration. This is a limitation of the exit, not a
// reason to skip it.
//
// ORDER IS RECORDED, NOT DERIVED. `order_idx` carries each element's position in
// the legacy array, and the reassembly sorts by it.
//
// ⚠️ BE ACCURATE ABOUT WHAT THAT BUYS TODAY, because the obvious justification is
// false and was measured false: with the current write path, `ORDER BY order_idx`
// and `ORDER BY rowid` CANNOT diverge. Both a new row's order_idx (MAX+1) and its
// rowid (max+1) move the same way, so delete-then-re-add, editing through the
// upsert's conflict path, and VACUUM all leave the two orderings identical
// (measured on this tree, 2026-08-17, all four cases). The column is kept because
// the list order is a FACT THIS MIGRATION KNOWS and rowid order is an accident
// that currently happens to agree — and because inserting at a position, or an
// import that reorders, needs a column that means position. Do NOT write that
// dropping it would reshuffle the owner's list; today it would not.
//
// FAILURE POSTURE — and it distinguishes two cases that a first draft of this
// migration wrongly treated alike:
//
//   - THE VALUE IS NOT A JSON ARRAY → the migration FAILS. This adds no new
//     blast radius: such an install ALREADY cannot start. loadAuthSettings
//     unmarshals this row into []ThemeBundleDTO and returns
//     "not a valid theme-bundle array", and server.go answers that with
//     `FATAL: load settings` and rc 1. It is dead either way, and failing loudly
//     during migrate is the better of two deaths.
//
//   - AN ELEMENT CANNOT BE KEYED → that element is SKIPPED and the migration
//     succeeds. 🔴 This is the case the first draft got wrong, and the review
//     that caught it was right about why: those rows PARSE, so such an install
//     BOOTS FINE TODAY. Failing the migration would turn "your themes are a bit
//     odd" into "your station does not come up after the upgrade", which is a
//     far worse outcome than the one being prevented — and it would be caused by
//     data the product refuses to WRITE but has never refused to HOLD.
//
//     "Cannot be keyed" covers five shapes: not an object, no id, blank id, an
//     id an earlier element already used, and — the one that is not obvious —
//     AN ELEMENT WHOSE id GO AND SQLITE READ DIFFERENTLY.
//
//     🔴 THAT LAST ONE IS NOT THEORETICAL, AND IT IS THE TRAP THE CHECK
//     CONSTRAINT SET. The key is derived by Go's decoder while the constraint
//     re-derives it with SQLite's `json_extract`, and the two do not agree on
//     every input. Measured on this tree (2026-08-17): `{"id":"a","id":"b"}`
//     (duplicate JSON key) gives Go "b" and SQLite "a"; `{"id":"a\ud800b"}` (a
//     lone surrogate) gives Go a U+FFFD replacement and SQLite the original
//     bytes. Both PARSE, so both boot today — and both would have failed the
//     CHECK, failed the migration, and bricked the upgrade. Exactly the class
//     this posture exists to prevent, re-entering through the guard added to
//     enforce it. So the id is re-derived with SQLITE's OWN reader before the
//     insert, and a disagreement is a skip like any other.
//
//     Skipping loses nothing WHILE THE LEGACY ROW IS STILL THERE, which is
//     exactly why the retirement precondition above is not optional.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("00059_custom_theme_table.go",
		upCustomThemeTable, downCustomThemeTable)
}

// customThemeTableDDL is the new home: one row per saved theme, keyed by the
// theme's own id (the same slug the wire has always used, ^[a-z0-9][a-z0-9-]{1,63}$).
//
// `bundle` is the theme's JSON OBJECT as text — the element's own bytes on the
// way in from the legacy array, and the marshalled DTO on every write after
// that. `updated_at` is 0 for migrated rows on purpose: nothing knows when the
// owner last edited a theme that lived inside a shared settings value, and
// stamping migrate time would invent a fact that reads like an edit.
//
// 🔴 THE CHECK IS THE ONLY THING THAT MAKES `theme_id` AND `bundle` ONE FACT
// RATHER THAN TWO. The id is stored twice by construction — once as the key,
// once inside the JSON — and nothing about the Go types stops a caller writing
// `PutCustomTheme("blue", {"id":"red"})`. Measured before this constraint
// existed: that write was accepted, so was a bundle that was not JSON at all,
// and so was an empty theme_id. The rows this migration creates always satisfy
// it (the key is DERIVED from the bundle's own id), so the constraint costs
// nothing here; it exists for every write that comes after, and it is free to
// add only while the table is empty. Losing it means the cockpit can be served a
// theme whose id disagrees with the id it is filed under, which is precisely the
// shape that makes "delete theme X" delete something else.
//
// ⚠️ THE THREE CONDITIONS ARE THREE NAMED CONSTRAINTS, NOT ONE. SQLite reports
// the constraint it names, and a single anonymous CHECK covering all three
// printed the same text — the first condition's — for every violation, so a
// bundle that was not JSON at all was reported as a blank-id problem, pointing
// whoever reads that log at the wrong field. Named, each violation says which
// fact was broken.
//
// 🔴 THE ID COMPARISON USES `IS`, NOT `=`, AND THAT IS THE WHOLE CONSTRAINT.
// SQLite fails a CHECK only when it evaluates to FALSE; NULL is not FALSE, so it
// PASSES. `json_extract` answers NULL for a bundle with no `$.id` at all — which
// is every bundle whose id key is missing, misspelled, or capitalised
// differently, plus every bundle that is a number, a string, an array or JSON
// null. With `=`, all of those were ACCEPTED (measured: six such rows went in
// clean), and the comment below promising this constraint makes the id "one fact
// rather than two" was false for exactly the inputs most likely to occur.
// `IS` is SQLite's NULL-safe equality: NULL IS 'blue' is FALSE, so the row is
// refused. `theme_id` also has to be NOT NULL explicitly — a TEXT PRIMARY KEY in
// SQLite does NOT imply it, a legacy quirk, and a NULL key would otherwise slip
// past both this constraint and the primary key.
const customThemeTableDDL = `
CREATE TABLE custom_theme (
  theme_id   TEXT NOT NULL PRIMARY KEY,
  bundle     TEXT NOT NULL,
  order_idx  INTEGER NOT NULL,
  updated_at REAL NOT NULL DEFAULT 0,
  CONSTRAINT custom_theme_id_not_blank CHECK (theme_id <> ''),
  CONSTRAINT custom_theme_bundle_is_json CHECK (json_valid(bundle)),
  CONSTRAINT custom_theme_id_matches_bundle
    CHECK (json_extract(bundle, '$.id') IS theme_id)
)`

// legacyCustomThemesKey is the settings key the array has lived under. It is
// spelled out here rather than referencing settingDisplayCustomThemes because a
// migration must keep describing the schema as it was AT THIS VERSION: if that
// constant is renamed or retired later, this migration must still run the same
// way on an install upgrading from before it existed.
const legacyCustomThemesKey = "display.custom_themes"

// customThemeSkipRecordKey is where Up records what it could not move. It is the
// mechanical half of the retirement precondition at the top of this file — the
// half a comment cannot be.
//
// A settings key rather than a table because this is a one-shot RECEIPT, not
// state anything reads at runtime. Settings are looked up one key at a time
// (dal.go GetSetting / DeleteSetting are both `WHERE key = ?`) and nothing in the
// tree enumerates the key space or scans it by prefix — verified independently
// on origin/main — so an extra row is inert to settings load, to
// `GET /api/settings` and to the cockpit. It cannot surface anywhere by accident.
//
// 🔴 THE ROW EXISTS ONLY WHEN SOMETHING WAS LEFT BEHIND. Its ABSENCE is the
// "nothing was lost" answer, which is the state every healthy install is in and
// therefore the one that must cost nothing to be in.
//
// Value: a JSON array of {"index", "reason"}. The index locates the element in
// the legacy row, which still holds it.
const customThemeSkipRecordKey = "display.custom_themes.skipped_by_00059"

// customThemeSkip is one element Up could not move.
type customThemeSkip struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

func upCustomThemeTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, customThemeTableDDL); err != nil {
		return err
	}
	var raw string
	err := tx.QueryRowContext(ctx,
		`SELECT value FROM setting WHERE key = ?`, legacyCustomThemesKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && raw == "") {
		// No themes were ever saved. The table exists and is empty, which is the
		// same state a fresh install starts in.
		return nil
	}
	if err != nil {
		return err
	}
	var elements []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &elements); err != nil {
		return fmt.Errorf("migration 00059: setting %s is not a JSON array: %w",
			legacyCustomThemesKey, err)
	}
	type row struct {
		id     string
		bundle []byte
		idx    int
	}
	rows := make([]row, 0, len(elements))
	seen := make(map[string]bool, len(elements))
	var skipped []customThemeSkip
	skip := func(i int, why string) {
		skipped = append(skipped, customThemeSkip{Index: i, Reason: why})
		announceCustomThemeSkip(i, why)
	}
	for i, el := range elements {
		// Only the id is decoded. Everything else stays as the bytes that were
		// stored — see the byte-for-byte note at the top of this file.
		var head struct {
			ID string `json:"id"`
		}
		// Every `skip` below is a SKIP, not a failure, and the reason is the same
		// in all of them: an element shaped like this parses, so the install
		// carrying it starts today. See the failure posture at the top.
		if err := json.Unmarshal(el, &head); err != nil {
			skip(i, "not a JSON object")
			continue
		}
		if head.ID == "" {
			skip(i, "no usable id")
			continue
		}
		if seen[head.ID] {
			// The write path has always refused duplicates
			// (validateThemeBundles), so this is corruption rather than a
			// supported state — but the primary key would turn it into a failed
			// migration, and a station that will not start is a worse answer than
			// a theme left behind in the legacy row.
			skip(i, "id "+head.ID+" already used by an earlier element")
			continue
		}
		// 🔴 ASK THE DATABASE WHAT IT THINKS THE id IS, BEFORE TRUSTING GO'S
		// ANSWER. The custom_theme_id_matches_bundle constraint re-derives the id
		// with SQLite's own JSON reader, and the two readers do not agree on
		// every input that both accept — a duplicate `id` key (Go takes the last,
		// SQLite the first) and a lone surrogate escape (Go substitutes U+FFFD,
		// SQLite keeps the bytes) are both measured examples. Letting the INSERT
		// discover that would fail the migration, which is the exact brick this
		// posture exists to avoid. So the disagreement is detected here and
		// treated as what it is: an element that cannot be keyed.
		var dbID sql.NullString
		if err := tx.QueryRowContext(ctx,
			`SELECT json_extract(?, '$.id')`, string(el)).Scan(&dbID); err != nil {
			skip(i, "the database's JSON reader cannot read this element")
			continue
		}
		if !dbID.Valid || dbID.String != head.ID {
			skip(i, "the database's JSON reader and Go disagree about this element's id")
			continue
		}
		seen[head.ID] = true
		rows = append(rows, row{id: head.ID, bundle: el, idx: i})
	}
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, ?, 0)`,
			r.id, string(r.bundle), r.idx); err != nil {
			return fmt.Errorf("migration 00059: insert theme %q: %w", r.id, err)
		}
	}
	return recordCustomThemeSkips(ctx, tx, skipped)
}

// recordCustomThemeSkips writes the receipt the retirement precondition reads.
// No skips ⇒ NO ROW, because absence is the "nothing was lost" answer and the
// healthy case must not pay for the unhealthy one.
//
// 🔴 "NO ROW" MEANS IT DELETES, NOT MERELY THAT IT DOES NOT WRITE. An earlier
// version simply returned early when there was nothing to record, which made the
// receipt a claim about the FIRST time this migration ever ran rather than about
// the state it just produced. The failing sequence is short and reachable: Up on
// a database with a bad element leaves a receipt; Down drops the table; the
// legacy value is repaired; Up runs again and moves everything cleanly — and the
// stale receipt is still sitting there saying something was left behind. Under
// the "key exists ⇒ refuse" rule that install is now blocked from retiring the
// legacy row FOREVER, for a reason that is not true.
//
// That is the same failure the rule itself was rewritten to remove (a gate with
// no exit), re-entering through a different door, so the receipt is written the
// way every other idempotent projection is: it REPLACES what the last run said.
func recordCustomThemeSkips(ctx context.Context, tx *sql.Tx, skipped []customThemeSkip) error {
	if len(skipped) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM setting WHERE key = ?`, customThemeSkipRecordKey)
		return err
	}
	blob, err := json.Marshal(skipped)
	if err != nil {
		return fmt.Errorf("migration 00059: record skips: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, 0)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		customThemeSkipRecordKey, string(blob)); err != nil {
		return fmt.Errorf("migration 00059: record skips: %w", err)
	}
	return nil
}

// announceCustomThemeSkip prints what was left behind, and it is the CONVENIENCE
// half of the pair — recordCustomThemeSkips is the half anything can rely on.
//
// ⚠️ NOTHING GUARDS THIS FUNCTION, and pretending otherwise would be the same
// mistake as the order_idx justification above: emptying its body leaves every
// test green, because a line of log output has no consumer a test can be. It is
// here so a person watching an upgrade sees it; the migration's promise that a
// skip is never silent rests on the database row, not on this.
//
// It writes to STDERR, which is where goose's own migration lines go — goose v3
// logs through the standard library's `log` package and nothing in this tree
// calls goose.SetLogger, so `OK 00059_...` is on stderr too. An earlier version
// of this comment said stdout; that was wrong, and the operational conclusion
// (an operator sees both) survived only because the two streams land in the same
// file either way.
func announceCustomThemeSkip(i int, why string) {
	fmt.Fprintf(os.Stderr, "[migration 00059] SKIPPED %s[%d]: %s — it stays in the legacy row\n",
		legacyCustomThemesKey, i, why)
}

// downCustomThemeTable drops the table, and that loses nothing the binary below
// this migration could have read: `display.custom_themes` was copied, never
// moved, so the older binary finds its themes exactly where it left them.
//
// ⚠️ The one thing it DOES lose is theme edits made through the new per-theme
// write path while this migration was applied — those rows have no older place
// to be put back into, because the legacy row is not maintained in parallel.
// That is the accepted cost of a one-way copy, and it is why retiring the
// legacy row is a separate, later decision rather than part of this change:
// while both exist, a downgrade costs at most the edits made since the upgrade.
// It also removes the skip receipt, and that is not tidying. The receipt
// describes the contents of a table that is about to stop existing; leaving it
// behind would let a later Up — possibly on repaired data, with nothing skipped
// at all — start life already carrying a claim that something was lost.
func downCustomThemeTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE custom_theme`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM setting WHERE key = ?`, customThemeSkipRecordKey)
	return err
}
