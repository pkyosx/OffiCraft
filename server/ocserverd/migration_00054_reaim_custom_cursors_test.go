package main

// migration_00054_reaim_custom_cursors_test.go — the end-to-end half of
// retiring `custom`'s lookback window.
//
// 🔴 THE CLAIM UNDER TEST IS ABOUT AN UPGRADE, SO IT IS TESTED AS ONE: a
// database left in the state round 1 produced, then the migration, then a REAL
// runScheduledMessageTick against the real DAL and the real delivery path. The
// unit-level facts (currentSlotKey returned "", slotIsAfterCursor reads "" as
// fire, the derivation can now see the old slot) each looked innocuous on their
// own; the thing that matters is what the first tick after the upgrade actually
// does, and only a tick can answer that.
//
// The window this closes is narrow and permanent: a `custom` row created while
// round 1 was deployed carries an empty cursor, and the first tick on the new
// binary would deliver an occurrence up to a year old. "Missed slots are not
// backfilled" is this feature's stated behaviour, so that delivery would be a
// visible, unexplainable message arriving out of nowhere.

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// scheduledStackOn is scheduledStack against a database the caller already
// prepared — the seam this file needs, because the whole point is to migrate a
// database that was populated BEFORE the migration ran.
func scheduledStackOn(t *testing.T, db *sql.DB) *apiServer {
	t.Helper()
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	api := newAPIServer(dal, NewHub(), singleKeyring(secret), 3600, "../..")
	h, err := buildHandler(specsFor(api), api.keys, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return api
}

// TestMigration00054ReAimsEmptyCustomCursorsSoTheFirstTickSendsNothing is the
// end-to-end upgrade.
//
// Red when: the re-aim is dropped or narrowed (the annual schedule delivers a
// year-old occurrence on the first tick), or it reaches rows it must not (a row
// that already carries a cursor is moved, or a parked non-custom row is aimed
// as though it were running its custom sets).
func TestMigration00054ReAimsEmptyCustomCursorsSoTheFirstTickSendsNothing(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "reaim.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// The world as round 1 left it: schema at 53, and a once-a-year `custom`
	// schedule whose cursor creation could not fill in, because a 70-day
	// window cannot see 1 January from anywhere but January.
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 53); err != nil {
		t.Fatalf("up to 53: %v", err)
	}

	const staleCursor = "2001-01-01T09:00+00:00"
	seed := func(id, cadence, months, days, cursor string) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO scheduled_message
			  (id, member_id, label, body, cadence, day_of_week, day_of_month, hour, minute,
			   custom_months, custom_days, custom_hours, custom_minutes, timezone, status,
			   last_fired_slot, last_fired_ts, created_ts)
			VALUES (?, 'mira', 'label '||?, 'body '||?, ?, 3, 1, 9, 0, ?, ?, '9', '0', 'UTC', 'enabled',
			        ?, 0, 1785000000.25)`,
			id, id, id, cadence, months, days, cursor); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	// (a) The row this migration is for.
	seed("sch-annual", ScheduledMessageCadenceCustom, "1", "1", "")
	// (b) A row that has already been aimed. Its cursor is deliberately an
	// ANCIENT slot, so "left alone" and "re-aimed" are trivially distinguishable
	// — and so is the tick's reaction to each.
	seed("sch-aimed", ScheduledMessageCadenceCustom, "1", "1", staleCursor)
	// (c) A row PARKED under another cadence: it keeps its custom sets, but the
	// cursor it carries belongs to the cadence it is actually running.
	seed("sch-parked", ScheduledMessageCadenceDaily, "1", "1", "")

	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}
	dal := NewDAL(db)
	after := func(id string) ScheduledMessage {
		t.Helper()
		got, err := dal.GetScheduledMessage(id)
		if err != nil || got == nil {
			t.Fatalf("read %s: %v %v", id, got, err)
		}
		return *got
	}

	annual := after("sch-annual")
	if annual.LastFiredSlot == "" {
		t.Fatal("the annual schedule still carries an empty cursor after the migration — the first tick " +
			"will deliver an occurrence up to a year old, which is precisely what 'missed slots are not " +
			"backfilled' rules out")
	}
	// The cursor must be the slot that is CURRENT, not some future one: aiming
	// past `now` would silently swallow the next real occurrence too.
	wantSlot, ok := mostRecentSlot(annual, time.Now())
	if !ok || annual.LastFiredSlot != slotKey(wantSlot) {
		t.Fatalf("cursor is %q, want the currently elapsed slot %q (ok=%v) — anything later than that "+
			"skips a delivery instead of preventing a stale one", annual.LastFiredSlot, slotKey(wantSlot), ok)
	}
	if got := after("sch-aimed").LastFiredSlot; got != staleCursor {
		t.Fatalf("a row that already carried a cursor was moved from %q to %q — re-aiming an aimed row "+
			"either re-sends or skips", staleCursor, got)
	}
	if got := after("sch-parked").LastFiredSlot; got != "" {
		t.Fatalf("a row parked under another cadence was aimed at %q using sets it is not running", got)
	}

	// And now the thing the unit facts could not answer: what does the first
	// tick actually do?
	api := scheduledStackOn(t, db)
	api.runScheduledMessageTick(nowSecs())
	if n := len(chatsFrom(t, api, "sched:sch-annual")); n != 0 {
		t.Fatalf("the first tick after the upgrade delivered %d message(s) for a schedule whose last "+
			"occurrence is up to a year old", n)
	}

	// 🔴 Positive control: the assertion above is only worth anything if this
	// tick COULD have delivered. Put the row back in the exact pre-migration
	// state and tick again — that must send exactly one message, which is the
	// behaviour the migration exists to prevent.
	stale := after("sch-annual")
	stale.LastFiredSlot = ""
	if err := dal.PutScheduledMessage(stale); err != nil {
		t.Fatalf("restore the pre-migration cursor: %v", err)
	}
	api.runScheduledMessageTick(nowSecs())
	if n := len(chatsFrom(t, api, "sched:sch-annual")); n != 1 {
		t.Fatalf("with the cursor back at its pre-migration value the tick delivered %d message(s), want 1 — "+
			"this test cannot tell a working migration from a tick that never fires anything", n)
	}
	// The already-aimed row is the other half of the same control: its cursor
	// names an occurrence from 2001, so the tick MUST deliver for it. A test in
	// which nothing is ever due proves nothing about a test in which nothing is
	// delivered.
	if n := len(chatsFrom(t, api, "sched:sch-aimed")); n != 1 {
		t.Fatalf("the row aimed at a 2001 slot delivered %d message(s) across two ticks, want 1", n)
	}
}
