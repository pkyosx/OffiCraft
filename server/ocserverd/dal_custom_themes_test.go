package main

// T-83ef — the per-theme write seam.
//
// These tests are about ONE property above all others: writing a theme must
// touch that theme and nothing else. The thing being replaced could not do
// that, and every regression this file can catch is a slide back towards it —
// a write that rewrites its neighbours, a write that moves the list order, a
// delete that renumbers everyone.

import (
	"errors"
	"strings"
	"testing"
)

func t83efPut(t *testing.T, d *DAL, id, bundle string) {
	t.Helper()
	if err := d.PutCustomTheme(id, bundle); err != nil {
		t.Fatalf("put %s: %v", id, err)
	}
}

func t83efList(t *testing.T, d *DAL) []CustomTheme {
	t.Helper()
	got, err := d.ListCustomThemes()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return got
}

func TestCustomThemeWriteTouchesOnlyThatTheme(t *testing.T) {
	d := newTestDAL(t)
	t83efPut(t, d, "alpha", `{"id":"alpha","name":"A"}`)
	t83efPut(t, d, "beta", `{"id":"beta","name":"B"}`)
	t83efPut(t, d, "gamma", `{"id":"gamma","name":"C"}`)

	before := t83efList(t, d)
	t83efPut(t, d, "beta", `{"id":"beta","name":"B edited"}`)
	after := t83efList(t, d)

	if len(after) != 3 {
		t.Fatalf("after editing one theme the list holds %d, want 3", len(after))
	}
	for i := range after {
		if after[i].ID != before[i].ID {
			t.Fatalf("position %d changed from %q to %q — editing a theme must not reorder the list",
				i, before[i].ID, after[i].ID)
		}
		if after[i].OrderIdx != before[i].OrderIdx {
			t.Fatalf("theme %q moved from order_idx %d to %d",
				after[i].ID, before[i].OrderIdx, after[i].OrderIdx)
		}
		if after[i].ID == "beta" {
			continue
		}
		// The neighbours: byte-identical, because they were not the write.
		if after[i].Bundle != before[i].Bundle {
			t.Fatalf("theme %q was rewritten by a write aimed at beta.\n before: %s\n  after: %s",
				after[i].ID, before[i].Bundle, after[i].Bundle)
		}
		if after[i].UpdatedAt != before[i].UpdatedAt {
			t.Fatalf("theme %q got a new updated_at from a write aimed at beta", after[i].ID)
		}
	}
	got, err := d.GetCustomTheme("beta")
	if err != nil || got == nil {
		t.Fatalf("get beta: %v (nil=%v)", err, got == nil)
	}
	if want := `{"id":"beta","name":"B edited"}`; got.Bundle != want {
		t.Fatalf("beta bundle is %s, want %s", got.Bundle, want)
	}
}

func TestCustomThemeAppendsNewButKeepsExistingPosition(t *testing.T) {
	d := newTestDAL(t)
	t83efPut(t, d, "first", `{"id":"first"}`)
	t83efPut(t, d, "second", `{"id":"second"}`)

	// Re-writing the FIRST theme must not send it to the bottom. This is the
	// single most likely bug in an upsert that also has to append: putting
	// order_idx in the DO UPDATE SET clause looks harmless and quietly reorders
	// the owner's list on every colour edit.
	t83efPut(t, d, "first", `{"id":"first","name":"still first"}`)
	t83efPut(t, d, "third", `{"id":"third"}`)

	got := t83efList(t, d)
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("list holds %d themes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("position %d is %q, want %q", i, got[i].ID, want[i])
		}
	}
}

func TestCustomThemeDeleteRemovesOneAndReportsWhetherItExisted(t *testing.T) {
	d := newTestDAL(t)
	t83efPut(t, d, "keep-a", `{"id":"keep-a"}`)
	t83efPut(t, d, "drop", `{"id":"drop"}`)
	t83efPut(t, d, "keep-b", `{"id":"keep-b"}`)

	removed, err := d.DeleteCustomTheme("drop")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !removed {
		t.Fatal("delete of an existing theme reported that nothing was removed")
	}
	// The distinction a handler needs to answer 404 rather than a cheerful 204.
	removed, err = d.DeleteCustomTheme("drop")
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if removed {
		t.Fatal("delete of an absent theme reported a removal")
	}

	got := t83efList(t, d)
	want := []string{"keep-a", "keep-b"}
	if len(got) != len(want) {
		t.Fatalf("list holds %d themes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("position %d is %q, want %q", i, got[i].ID, want[i])
		}
	}
	// The survivors keep the order_idx values they had. Renumbering them would
	// be a whole-set write hiding inside a single delete — the exact shape this
	// ticket removed.
	if got[0].OrderIdx != 0 || got[1].OrderIdx != 2 {
		t.Fatalf("survivors were renumbered: order_idx %d and %d, want 0 and 2",
			got[0].OrderIdx, got[1].OrderIdx)
	}
}

// TestCustomThemeListOrderComesFromOrderIdxNotRowid is the ONLY thing standing
// between `ORDER BY order_idx` and someone simplifying it to `ORDER BY rowid`.
//
// 🔴 IT HAS TO CHEAT TO SAY ANYTHING, and that is the finding, not a weakness of
// the test. Through the product's own write path the two orderings cannot
// disagree — order_idx is MAX+1 and rowid is max+1, so they move together
// through every sequence of appends, deletes, re-adds and edits (measured; an
// independent review demonstrated the equivalence, and swapping this query to
// `ORDER BY rowid` left every other test in this file green). The only state
// that separates them is one the product cannot reach on its own, so the rows
// are seeded DIRECTLY with positions that contradict their insertion order.
//
// That is exactly the situation the column exists for: a future insert-at-
// position or a reordering import produces this state legitimately, and the day
// it does, this query has to already be reading the column rather than the
// accident that agreed with it.
func TestCustomThemeListOrderComesFromOrderIdxNotRowid(t *testing.T) {
	d := newTestDAL(t)
	// Inserted last-to-first by position: rowid order is the REVERSE of the
	// order these rows claim to be in.
	for _, seed := range []struct {
		id  string
		idx int
	}{{"third", 2}, {"second", 1}, {"first", 0}} {
		if _, err := d.wdb.Exec(
			`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, ?, 0)`,
			seed.id, `{"id":"`+seed.id+`"}`, seed.idx); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}
	got := t83efList(t, d)
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("list holds %d themes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("position %d is %q, want %q — the list is being read in insertion order, not stored order",
				i, got[i].ID, want[i])
		}
	}
}

// TestCustomThemeWriteRefusesAMismatchedPairBeforeTheTableDoes is about WHICH
// LAYER says no, not about whether anything does.
//
// The table's custom_theme_id_matches_bundle constraint would catch every case
// below — as a failed statement, surfacing from the driver, which a handler can
// only turn into a 500 and a log line. The caller deserves a 400 naming the
// field, and only a check ABOVE the write can produce one. So each case asserts
// the named error, not merely that the write failed: `errors.Is` is the
// difference between "the handler can answer" and "the handler can string-match
// a database message and hope".
//
// 🔴 THE DUPLICATE-KEY AND LONE-SURROGATE CASES ARE THE REASON THIS IS NOT
// PARANOIA. A handler deriving the key from its decoded DTO — the obvious way to
// write one — produces exactly those pairs from input that Go accepted happily,
// because Go's decoder and SQLite's json_extract disagree about them. Migration
// 00059 meets the same disagreement and skips those elements; an endpoint cannot
// skip, so it needs this answer.
func TestCustomThemeWriteRefusesAMismatchedPairBeforeTheTableDoes(t *testing.T) {
	d := newTestDAL(t)
	// 🔴 EACH CASE NAMES THE ERROR IT EXPECTS, not merely "some named error".
	// Answering a blank id with the MISMATCH error sends the caller to look at
	// the wrong field — the same defect the table had while its three facts
	// shared one anonymous CHECK, one layer up. An earlier version of this test
	// asserted only ErrCustomThemeIDMismatch and froze that defect in place.
	// 🔴 AND NAMING THE SENTINEL IS NOT ENOUGH WHERE TWO CAUSES SHARE ONE.
	// "no id at all" and "an id that disagrees" both answer
	// ErrCustomThemeIDMismatch, so `errors.Is` cannot tell them apart and the
	// branch that produces the more accurate sentence was, by measurement,
	// removable with every case here still green. What separates them is the
	// SENTENCE, so wantMsg pins that: without it the no-id case falls through to
	// the mismatch wording, which reports `bundle says ""` — telling the caller
	// their id is an empty string when the truth is that the field is absent.
	// Same class of defect this test's own header warns about, one level deeper.
	for _, tc := range []struct {
		name, id, bundle string
		want             error
		wantMsg          string
	}{
		{"bundle's id is a different theme", "blue", `{"id":"red"}`, ErrCustomThemeIDMismatch, `bundle says "red"`},
		{"bundle is not JSON", "blue", `not json at all`, ErrCustomThemeBundleNotJSON, ""},
		{"bundle has no id", "blue", `{"name":"nameless"}`, ErrCustomThemeIDMismatch, "the bundle carries no id"},
		{"empty key", "", `{"id":""}`, ErrCustomThemeIDBlank, ""},
		{"duplicate id key — Go reads b, SQLite reads a", "b", `{"id":"a","id":"b"}`, ErrCustomThemeIDMismatch, ""},
		{"lone surrogate — Go substitutes U+FFFD, SQLite keeps the bytes", "a�b", `{"id":"a\ud800b"}`, ErrCustomThemeIDMismatch, ""},
		// The numeric shapes below are the ones that USED TO PASS this check and
		// then be refused by the table — a 500 where a 400 belongs. They are not
		// reachable through the wire (the DTO's id is a string), which is why
		// they were invisible; they are here because the check's own description
		// claims it answers what the constraint would answer, and until the
		// comparison moved into SQLite that claim was false for exactly these.
		{"id is a float that Go and SQLite render differently", "1", `{"id":1.0}`, ErrCustomThemeIDMismatch, ""},
		{"id is an exponent", "1e+100", `{"id":1e100}`, ErrCustomThemeIDMismatch, ""},
		{"id is negative zero", "-0", `{"id":-0.0}`, ErrCustomThemeIDMismatch, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := d.PutCustomTheme(tc.id, tc.bundle)
			if err == nil {
				t.Fatal("accepted a pair whose key and bundle id disagree")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("refused, but with the wrong named error — the handler will point the caller at the wrong field.\n want: %v\n  got: %v", tc.want, err)
			}
			// Where two causes share a sentinel, the sentence is the only thing
			// that tells the caller WHICH field to go and look at.
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("right sentinel, wrong diagnosis — the caller is sent to the wrong field.\n want the sentence to contain: %q\n                          got: %v", tc.wantMsg, err)
			}
			// And nothing was written: a refusal that half-lands is worse than
			// one that does not refuse at all.
			if got, gerr := d.GetCustomTheme(tc.id); gerr != nil || got != nil {
				t.Fatalf("the refused write left a row behind (err=%v, row=%v)", gerr, got != nil)
			}
		})
	}
	// The control. Without it, a check that refused EVERYTHING would pass every
	// case above.
	if err := d.PutCustomTheme("green", `{"id":"green","name":"G"}`); err != nil {
		t.Fatalf("a legitimate pair was refused: %v", err)
	}

	// 🔴 THE PARITY DIRECTION THE CASES ABOVE CANNOT TEST. Every case above
	// asserts a REFUSAL, and a check that is too STRICT refuses too — so they
	// cannot tell "agrees with the table" from "stricter than the table", and
	// being stricter is its own failure: a write the database would have taken,
	// turned away by the layer in front of it.
	//
	// This pair is the separator, measured rather than assumed: the table
	// ACCEPTS it, because a TEXT column's affinity converts the extracted
	// integer 42 to "42" before comparing. Without the CAST that reproduces that
	// conversion, the pre-check compares INTEGER 42 against TEXT "42", calls it
	// a mismatch, and refuses a row the table was happy to store. (Removing the
	// CAST reddens nothing among the refusal cases — verified.)
	if err := d.PutCustomTheme("42", `{"id":42}`); err != nil {
		t.Fatalf("the pre-check is STRICTER than the constraint it stands in front of: the table accepts this pair, the check refused it (%v)", err)
	}
	if got, err := d.GetCustomTheme("42"); err != nil || got == nil {
		t.Fatalf("the write reported success but stored nothing (err=%v)", err)
	}
}

func TestCustomThemeGetAndCountOnAnEmptyTable(t *testing.T) {
	d := newTestDAL(t)
	got, err := d.GetCustomTheme("nobody")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("an id nobody carries returned %+v, want nil", *got)
	}
	n, err := d.CountCustomThemes()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("count is %d on an empty table, want 0", n)
	}
	// COALESCE in the append expression: MAX over no rows is NULL, and NULL
	// would fail the NOT NULL column rather than mean "first".
	t83efPut(t, d, "only", `{"id":"only"}`)
	list := t83efList(t, d)
	if len(list) != 1 || list[0].OrderIdx != 0 {
		t.Fatalf("first theme into an empty table: got %+v, want one row at order_idx 0", list)
	}
}

func TestCustomThemeBundleIsStoredAsGivenWithoutDecoding(t *testing.T) {
	d := newTestDAL(t)
	// A key the DTO does not declare, and formatting encoding/json would not
	// produce. Both survive only if this layer treats the bundle as text —
	// which is what keeps the migration's byte-for-byte guarantee intact
	// underneath every later write.
	const raw = `{"id":"odd", "name":"spaced","fieldTheDTODoesNotKnow":1}`
	t83efPut(t, d, "odd", raw)
	got, err := d.GetCustomTheme("odd")
	if err != nil || got == nil {
		t.Fatalf("get: %v (nil=%v)", err, got == nil)
	}
	if got.Bundle != raw {
		t.Fatalf("bundle came back changed.\n stored: %s\n    got: %s", raw, got.Bundle)
	}
}
