package main

// handover_notice_restart_t6ebc_test.go — T-6ebc guard on the THIRD case of
// 「只通知一次」: a station re-exec.
//
// T-c382 established once-per-SESSION and proved the two cases a single
// long-lived process can show you: many quiet ticks on one connection, and a
// mid-session SSE reconnect. Both stayed green while the notice still repeated
// in production, because the case that produced it is invisible to a test that
// never lets the PROCESS die: the claim lived in a process-local map, the
// station re-execs on every version bump, and the AGENTS survive that — they
// just reconnect with the same anchor and were told, verbatim, "this is the
// ONLY notice you get before it" a second time. Measured 2026-08-16: 23:07 and
// 23:28, across v0.5.156-beta.1 → v0.5.157-beta.1.
//
// 🔴 So the fixture here is not "a fresh apiServer" for tidiness — the SECOND
// apiServer over the SAME database IS the bug. A test that reuses one server
// cannot fail on this defect no matter what it asserts.

import (
	"path/filepath"
	"testing"
)

// restartableNoticeServers returns two apiServers backed by ONE database: the
// process before the re-exec and the process after it. Everything durable is
// shared; everything in-memory is not — which is exactly the asymmetry a
// version bump creates.
func restartableNoticeServers(t *testing.T, memberID string) (*apiServer, *apiServer) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "handover-notice-restart.db")
	newServer := func() *apiServer {
		db, err := openSQLite(dsn)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		if err := runMigrations(db); err != nil {
			t.Fatalf("goose up: %v", err)
		}
		return newAPIServer(NewDAL(db), NewHub(), singleKeyring([]byte("notice-restart-secret")), 3600,
			assetRoot(t.TempDir()))
	}
	before := newServer()
	// The agent has a member row — that is what makes the claim durable at all,
	// and every real agent has one.
	if err := before.dal.PutMember(Member{
		ID: memberID, Name: "restart-probe", Kind: KindStaff,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return before, newServer()
}

func TestHandoverNotice_SurvivesAStationReExec(t *testing.T) {
	before, after := restartableNoticeServers(t, "m-1")

	if !before.claimHandoverNotice("m-1", noticeGauge(1000)) {
		t.Fatal("the first tick of a fresh session must claim its one notice")
	}

	// 🔴 THE RE-EXEC. New process, empty map, same database — and the agent is
	// still connected on the same session, so its gauge carries the same anchor.
	// A process-local claim hands out a second "only notice" right here.
	if after.claimHandoverNotice("m-1", noticeGauge(1000)) {
		t.Fatal("a station re-exec must NOT re-notify a session that was already " +
			"told — that is what made 「this is the ONLY notice you get」 a lie")
	}

	// And the claim must be READ from the durable half, not merely written to
	// it: repeated ticks after the re-exec stay silent too.
	for i := 0; i < 10; i++ {
		if after.claimHandoverNotice("m-1", noticeGauge(1000)) {
			t.Fatalf("tick %d after the re-exec re-notified", i)
		}
	}

	// A genuinely NEW session is still entitled to its own notice AFTER a
	// re-exec. Without this, "remembers correctly" and "went permanently mute
	// for this agent" are the same green.
	if !after.claimHandoverNotice("m-1", noticeGauge(2000)) {
		t.Fatal("a new session must get its own notice, re-exec or not")
	}
}

func TestHandoverNotice_ClaimIsReleasedAtTheSessionBoundary(t *testing.T) {
	before, after := restartableNoticeServers(t, "m-1")

	if !before.claimHandoverNotice("m-1", noticeGauge(1000)) {
		t.Fatal("first claim of the session")
	}
	// The session ends (spawn/stop boundary). This is the ONLY thing that
	// releases the claim.
	before.clearSessionBootTS("m-1")

	// Assert the DURABLE half directly. Going through claimHandoverNotice with a
	// new anchor would pass whether or not the clear happened — a stale claim
	// carrying the OLD anchor never matches a new one, so the release would look
	// identical to no release at all until some future session happened to reuse
	// the value.
	m, err := before.dal.GetMember("m-1")
	if err != nil || m == nil {
		t.Fatalf("read member back: %v", err)
	}
	if m.HandoverNoticedTS != 0 {
		t.Fatalf("the session boundary must release the durable claim, still %v",
			m.HandoverNoticedTS)
	}

	// And the next session gets its notice even across a re-exec.
	if !after.claimHandoverNotice("m-1", noticeGauge(3000)) {
		t.Fatal("the session after the boundary must get its own notice")
	}
}

// TestHandoverNotice_ClaimSurvivesAWholeRowUpsert is the guard the design
// decision needed and did not have (found by independent review, T-ffdf).
//
// The claim column is deliberately insert-only — a whole-row write never lands
// it on an existing row — and
// until this test existed the ONLY thing enforcing that was a comment. Adding
// `handover_noticed_ts = excluded.handover_noticed_ts` to that SET list left
// the whole suite green — a change that reads like tidying up an oversight,
// and no test would have argued.
//
// 🔴 It is not theoretical. memberFromWorker rebuilds a Member from scratch and
// does NOT carry this column (OutsourceWorker has no such field), so every
// PutOutsourceWorker sends a zero for it. With the column in the SET list, each
// worker status write would silently release that worker's claim — and the bug
// this ticket exists to fix walks back in through a different door, for exactly
// the population that churns member rows the most.
func TestHandoverNotice_ClaimSurvivesAWholeRowUpsert(t *testing.T) {
	api, _ := restartableNoticeServers(t, "m-1")
	const anchor = 4242.0

	if err := api.dal.SetMemberHandoverNoticedTS("m-1", anchor); err != nil {
		t.Fatalf("stamp claim: %v", err)
	}

	// A whole-row writer holding a snapshot that predates the claim — which is
	// every writer, since nothing but the single-column setter ever sets it.
	m, err := api.dal.GetMember("m-1")
	if err != nil || m == nil {
		t.Fatalf("read member: %v", err)
	}
	stale := *m
	stale.HandoverNoticedTS = 0
	stale.Name = "renamed by an unrelated write"
	if err := api.dal.PutMember(stale); err != nil {
		t.Fatalf("whole-row upsert: %v", err)
	}

	after, err := api.dal.GetMember("m-1")
	if err != nil || after == nil {
		t.Fatalf("read back: %v", err)
	}
	if after.HandoverNoticedTS != anchor {
		t.Fatalf("a whole-row upsert must NOT move the notice claim: %v → %v. "+
			"The column must stay insert-only (its constructor in "+
			"dal_member_patch.go); only "+
			"SetMemberHandoverNoticedTS may move it",
			anchor, after.HandoverNoticedTS)
	}
	// Positive control: the same write DID land its other change, so the
	// assertion above is not passing because nothing was written at all.
	if after.Name != "renamed by an unrelated write" {
		t.Fatalf("the upsert itself must have landed; got name %q", after.Name)
	}
}

// TestHandoverNotice_TwoRacingClaimsOnlyOneSends guards the double-check that
// moving the database read OUT of the lock made necessary (independent review,
// T-ffdf I2: removing it left the suite green).
//
// Two ticks can now miss the cache together, both read the database, both find
// no claim, and both arrive believing they may send. Only one may. The check
// that decides this lives inside the lock and is the whole reason
// rememberHandoverClaim returns a bool instead of nothing — a signature that
// looks like an over-engineered setter until you delete the branch and watch
// nothing complain.
//
// No goroutines needed: the interleaving that matters is just "two callers
// reach the claim step with the same anchor", which is two ordinary calls.
func TestHandoverNotice_TwoRacingClaimsOnlyOneSends(t *testing.T) {
	api, _ := restartableNoticeServers(t, "m-1")

	if !api.rememberHandoverClaim("m-1", 1000) {
		t.Fatal("the first caller to reach the claim step must win it")
	}
	if api.rememberHandoverClaim("m-1", 1000) {
		t.Fatal("the second caller racing on the SAME anchor must lose — both " +
			"sending is the duplicate notice this ticket exists to remove")
	}
	// A different anchor is a different session and is entitled to its own.
	if !api.rememberHandoverClaim("m-1", 2000) {
		t.Fatal("a new session's anchor must still be claimable")
	}
}

// TestHandoverNotice_ADatabaseFailureFallsTowardSending pins the DIRECTION the
// gate errs in when it cannot read the durable claim (independent review, T-ffdf:
// flipping this to silence left the suite green).
//
// The two failure modes are not symmetric and that asymmetry is the whole
// decision: a duplicate notice costs one repeated sentence and someone will say
// so, while a swallowed notice means an agent never learns it is near the
// handover ceiling — and NOTHING reports a message that was never sent. So a
// database hiccup must not be able to mute the one notice a session gets.
//
// 🔴 If this test is ever in the way, the change it is blocking is a RULING
// about which error is cheaper, not a refactor. Get the ruling first.
func TestHandoverNotice_ADatabaseFailureFallsTowardSending(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "handover-notice-dbfail.db")
	open := func() (*apiServer, func() error) {
		db, err := openSQLite(dsn)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if err := runMigrations(db); err != nil {
			t.Fatalf("goose up: %v", err)
		}
		return newAPIServer(NewDAL(db), NewHub(), singleKeyring([]byte("notice-dbfail-secret")), 3600,
			assetRoot(t.TempDir())), db.Close
	}

	before, closeBefore := open()
	if err := before.dal.PutMember(Member{
		ID: "m-1", Name: "dbfail-probe", Kind: KindStaff,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if !before.claimHandoverNotice("m-1", noticeGauge(1000)) {
		t.Fatal("first claim of the session")
	}
	if err := closeBefore(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// A new process (empty cache) whose database is unreachable. The claim IS
	// on disk — this session was already told — but the gate cannot see it.
	after, closeAfter := open()
	if err := closeAfter(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !after.claimHandoverNotice("m-1", noticeGauge(1000)) {
		t.Fatal("a database read failure must fall toward SENDING: a repeated " +
			"sentence gets reported, a silently swallowed notice never does")
	}
}

// TestHandoverNotice_WithoutAMemberRowIsCacheOnly pins the KNOWN LIMIT rather
// than leaving it to be discovered as a surprise: the durable claim lives on
// the member row, so an id with no row (every T-c382 unit fixture, and nothing
// in production) degrades to the old process-local behaviour. It is recorded
// here so a future reader does not mistake those green tests for coverage of
// the re-exec case — they are not, and that is why this file exists.
func TestHandoverNotice_WithoutAMemberRowIsCacheOnly(t *testing.T) {
	before, after := restartableNoticeServers(t, "m-1")

	if !before.claimHandoverNotice("m-nonexistent", noticeGauge(1000)) {
		t.Fatal("an id with no member row still claims within the process")
	}
	if before.claimHandoverNotice("m-nonexistent", noticeGauge(1000)) {
		t.Fatal("...and the in-process cache still dedups it")
	}
	if !after.claimHandoverNotice("m-nonexistent", noticeGauge(1000)) {
		t.Fatal("KNOWN LIMIT changed: an id with no member row now survives a " +
			"re-exec. If that is intended, this test should be replaced rather " +
			"than relaxed — and the real fixtures should grow member rows")
	}
}
