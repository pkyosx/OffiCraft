package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// The account card's own figure and its 歸零 button (T-53, owner ruling
// rc-5c5d7c7c6dcd 「分開：帳號卡自己一份數字，清它不動成員」).
//
// The account figure used to be a fold over whichever actors were on the
// account. The owner asked for it to be clearable WITHOUT clearing the members
// it happens to contain, so it is now an accumulator of its own, fed by the
// increase each telemetry report brings.
//
// 🔴 THE TWO THINGS THIS FILE EXISTS TO CATCH, because both fail silently:
//   - the account button reaching into member figures (or vice versa) — that is
//     precisely what the ruling separated;
//   - the accumulator mis-reading a session restart, which under-reports spend
//     for as long as it takes the new session to pass the old figure.

func doResetAccountCost(t *testing.T, s *apiServer, account string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleResetAccountCostApiAccountsCostResetPost(rec,
		taskReq(t, "POST", "/api/accounts/cost/reset",
			map[string]any{"account": account}, wireOwnerID, "owner"))
	return rec
}

// accountCostOf reads the account card's figure the way the cockpit does — off
// the monitoring wire — rather than out of the table. A test that read the
// table could not tell "the number is zero" apart from "the number never
// reached the card".
func accountCostOf(t *testing.T, s *apiServer, account string) any {
	t.Helper()
	row := accountRow(t, monitoringOf(t, doGetMonitoring(s,
		map[string]any{"sub": "owner", "scope": "owner"})), account)
	return row["cost"]
}

// The trajectory Kyle asked for, and the one place three plausible
// implementations differ: report 5 → 8 → SESSION RESTART → 2 must leave the
// account at 10 (5 + 3 + 2).
//
// 🔴 MUTANTS, both of which look reasonable in review:
//   - skip a decrease ("that must be noise") → 8, and every penny the new
//     session spends is invisible until it passes 8;
//   - add the difference anyway → 2, and the account figure walks BACKWARDS,
//     which is the silent-lie shape this whole design avoids.
func TestAccountSpend_ASessionRestartCountsFromZeroRatherThanGoingBackwards(t *testing.T) {
	s := costResetServer(t)
	seedRegisteredMachine(t, s, "m-seth-m5")
	seedWorker(t, s, "ow-7", "S7", 0, WorkerStatusActive)

	report := func(cost string) {
		t.Helper()
		if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
			`{"runtime":"claude","account":"seth-m5-claude","cost":`+cost+`}`); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", cost, rec.Code, rec.Body.String())
		}
	}

	report("5")
	if got := accountCostOf(t, s, "seth-m5-claude"); got != 5.0 {
		t.Fatalf("after the first report the account = %v, want 5", got)
	}
	report("8")
	if got := accountCostOf(t, s, "seth-m5-claude"); got != 8.0 {
		t.Fatalf("after a rise to 8 the account = %v, want 8 — a cumulative report "+
			"brings an INCREASE of 3, not another 8", got)
	}

	// The restart. A fresh session counts its own cost from zero, so the next
	// report is LOWER than the last — the case the whole accumulator turns on.
	report("2")
	if got := accountCostOf(t, s, "seth-m5-claude"); got != 10.0 {
		t.Errorf("after a session restart reporting 2 the account = %v, want 10 "+
			"(5 + 3 + 2). 8 means the restart's spend is being dropped on the "+
			"floor; 2 means the earlier sessions were erased; anything below 8 "+
			"means the figure walked backwards", got)
	}
}

// Banking is the fold that ends a session by moving the live figure into the
// actor's durable column, and it DELETES the live key on the way. The
// accumulator must not use that key as its baseline, or the first report after a
// reconnect finds no baseline, reads as a new session, and credits its whole
// cumulative figure a SECOND time.
//
// This one is worth a test of its own because it would be a double-count this
// code manufactured, not the reconnect bias the ticket already documents and
// leaves alone.
func TestAccountSpend_BankingASessionDoesNotMakeTheNextReportCountTwice(t *testing.T) {
	s := costResetServer(t)
	seedRegisteredMachine(t, s, "m-seth-m5")
	seedWorker(t, s, "ow-7", "S7", 0, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":6}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}

	// End the session the way the SSE disconnect edge does.
	s.bankLiveCost("ow-7")
	if _, present := liveCostOf(s, "ow-7"); present {
		t.Fatal("premise failed: banking is supposed to remove the live figure")
	}

	// The same session's next report, unchanged at 6.
	if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":6}`); rec.Code != 200 {
		t.Fatalf("re-ingest: %d %s", rec.Code, rec.Body.String())
	}
	if got := accountCostOf(t, s, "seth-m5-claude"); got != 6.0 {
		t.Errorf("account = %v, want 6 — the same 6 was reported twice with a bank "+
			"in between, and 12 means the accumulator lost its baseline when "+
			"banking removed the live figure", got)
	}
}

// A failed write must be RECOVERABLE, not silently swallowed (found by
// independent review, T-56). Accrual is best-effort on purpose — a bookkeeping
// failure must not take the monitoring ingest down with it — but "best effort"
// only means anything if the delta survives to the next report. Advance the
// baseline before the write succeeds and the failed increment is subtracted from
// a report that was never credited: gone for good, behind a 200.
//
// 🔴 MUTANT: move `entry[accountSpendAccountedKey] = cost` back above the
// AddAccountSpend call → this goes RED with 4 instead of 9.
func TestAccountSpend_AFailedWriteIsCarriedIntoTheNextReport(t *testing.T) {
	// Two servers, ONE telemetry store: the first has a dead write pool (the
	// failing DB), the second is healthy. That is how a transient failure looks
	// to the accrual — same actor, same entry, same baseline — without needing a
	// pool that can be reopened.
	shared := newMemStore()
	path := filepath.Join(t.TempDir(), "account-spend-dead.db")
	wdb, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open write pool: %v", err)
	}
	if err := runMigrations(wdb); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	rdb, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open read pool: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })
	dead := &apiServer{dal: NewDALPools(wdb, rdb), hub: NewHub(),
		telemetry: shared, gauge: newMemStore()}
	seedWorker(t, dead, "ow-7", "S7", 0, WorkerStatusActive)
	if err := wdb.Close(); err != nil {
		t.Fatalf("close write pool: %v", err)
	}

	// The report that cannot be banked. The ingest still answers 200 — that is
	// the deliberate part — but the 5 must not be treated as counted.
	if rec := doIngestTelemetry(dead, "ow-7", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":5}`); rec.Code != 200 {
		t.Fatalf("ingest must stay up when bookkeeping fails: %d %s", rec.Code, rec.Body.String())
	}

	live := costResetServer(t)
	live.telemetry = shared
	seedRegisteredMachine(t, live, "m-seth-m5")
	seedWorker(t, live, "ow-7", "S7", 0, WorkerStatusActive)
	for _, cost := range []string{"8", "9"} {
		if rec := doIngestTelemetry(live, "ow-7", "m-seth-m5",
			`{"runtime":"claude","account":"seth-m5-claude","cost":`+cost+`}`); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", cost, rec.Code, rec.Body.String())
		}
	}

	if got := accountCostOf(t, live, "seth-m5-claude"); got != 9.0 {
		t.Errorf("account = %v, want 9 — the session has spent 9 in total and the "+
			"one report that failed to bank must come back with the next one. 4 "+
			"means the baseline moved on a write that never happened, and those 5 "+
			"are gone with nothing but a stderr line to say so", got)
	}
}

// A NEW GENERATION counts from zero, and the waking report is where the server
// is told so (found by independent review, T-56).
//
// Without an explicit boundary the accrual can only GUESS from the numbers, and
// the guess fails in both directions: a restart whose first report lands at or
// above the previous session's figure looks like an increase (under-credit), and
// a session that switched account looks the same as one that carried on — where
// crediting the whole figure to the new account would invent money already
// banked against the old one. Waking settles it.
//
// 🔴 MUTANT: delete the startAccountSpendSession call from the waking handler →
// this goes RED, the second account credited 0 instead of 10.
func TestAccountSpend_AWakingReportStartsANewAccountingRun(t *testing.T) {
	s := costResetServer(t)
	seedRegisteredMachine(t, s, "m-seth-m5")
	seedWorker(t, s, "ow-7", "S7", 0, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
		`{"runtime":"claude","account":"first-account","cost":10}`); rec.Code != 200 {
		t.Fatalf("first ingest: %d %s", rec.Code, rec.Body.String())
	}

	if rec := reportWaking(t, s, "ow-7", "opus"); rec.Code != 200 {
		t.Fatalf("waking: %d %s", rec.Code, rec.Body.String())
	}

	// The new generation's own cumulative figure — the same 10, which without a
	// boundary is indistinguishable from "no new spend".
	if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
		`{"runtime":"claude","account":"second-account","cost":10}`); rec.Code != 200 {
		t.Fatalf("second ingest: %d %s", rec.Code, rec.Body.String())
	}

	if got := accountCostOf(t, s, "second-account"); got != 10.0 {
		t.Errorf("second account = %v, want 10 — a session that announced itself "+
			"counts from zero, so its whole figure is new spend", got)
	}
	// And the first account keeps what it was already credited: the boundary
	// starts a new run, it does not move spend between accounts.
	if got := accountCostOf(t, s, "first-account"); got != 10.0 {
		t.Errorf("first account = %v, want 10 untouched", got)
	}
}

// 🔴 ACCEPTED BOUNDARY — recorded here rather than left to be re-discovered
// (independent review T-56 asked for it to be named). A reporter that never
// announces waking has only the decrease fallback, so a new generation whose
// first report lands AT OR ABOVE the previous generation's last one is credited
// the DIFFERENCE instead of its whole figure. That is an under-count: the
// account card reads low, permanently, and nothing flags it.
//
// It is accepted rather than fixed, because the wire carries no other signal
// that a generation began. Every OffiCraft member calls report_waking as step 1
// of its boot sequence, so the gap covers only a reporter outside that contract,
// and closing it would mean going back to guessing from the numbers — the guess
// this boundary replaced.
//
// So this test PINS THE LOW NUMBER ON PURPOSE. If it ever wants to be 22, a real
// boundary signal has been added: delete this test, do not loosen it.
func TestAccountSpend_AReporterThatNeverWakesUnderCountsAndThatIsAccepted(t *testing.T) {
	s := costResetServer(t)
	seedRegisteredMachine(t, s, "m-seth-m5")
	seedWorker(t, s, "ow-7", "S7", 0, WorkerStatusActive)

	report := func(cost string) {
		t.Helper()
		if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
			`{"runtime":"claude","account":"no-waking","cost":`+cost+`}`); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", cost, rec.Code, rec.Body.String())
		}
	}
	report("10")
	// A new generation counting from zero that never said so — and whose first
	// report happens to exceed the previous generation's last one.
	report("12")

	if got := accountCostOf(t, s, "no-waking"); got != 12.0 {
		t.Errorf("account = %v, want 12 — the two generations really spent 22, and "+
			"the missing 10 IS the accepted boundary this test records", got)
	}
}

// The ruling itself: pressing the account button clears the CARD and nothing
// else. This is the assertion the owner would make by hand.
func TestResetAccountCost_ClearsTheCardAndLeavesEveryMemberUntouched(t *testing.T) {
	s := costResetServer(t)
	seedRegisteredMachine(t, s, "m-seth-m5")
	m := fullMember("seth")
	m.BankedCost = 4.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	seedWorker(t, s, "ow-live", "S7", 0.25, WorkerStatusActive)
	for _, id := range []string{"seth", "ow-live"} {
		if rec := doIngestTelemetry(s, id, "m-seth-m5",
			`{"runtime":"claude","account":"seth-m5-claude","cost":1.5}`); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	// Premise: the card carries a figure, so "absent" below cannot pass because
	// the fixture was empty all along.
	if accountCostOf(t, s, "seth-m5-claude") == nil {
		t.Fatal("fixture is not discriminating — the account card shows nothing to clear")
	}

	receipt := monitoringOf(t, doResetAccountCost(t, s, "seth-m5-claude"))
	if receipt["cleared_cost"] != 3.0 {
		t.Errorf("receipt cleared_cost = %v, want the 3 that was destroyed (two "+
			"actors reporting 1.5 each), not the 0 left behind", receipt["cleared_cost"])
	}

	if got := accountCostOf(t, s, "seth-m5-claude"); got != nil {
		t.Errorf("account cost = %v, want absent after the reset", got)
	}

	// 🔴 The half the ruling is ABOUT. Every member figure must be exactly where
	// it was: the owner separated these two buttons on purpose.
	after, err := s.dal.GetMember("seth")
	if err != nil || after == nil {
		t.Fatalf("re-read member: %v", err)
	}
	if after.BankedCost != 4.0 {
		t.Errorf("member banked_cost = %v, want 4 untouched — the account button "+
			"reached into a member figure, which is the one thing this ruling "+
			"separated", after.BankedCost)
	}
	wk, err := s.dal.GetOutsourceWorker("ow-live")
	if err != nil || wk == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if wk.BankedCost != 0.25 {
		t.Errorf("worker banked_cost = %v, want 0.25 untouched", wk.BankedCost)
	}
	for _, id := range []string{"seth", "ow-live"} {
		if v, present := liveCostOf(s, id); !present || v != 1.5 {
			t.Errorf("%s live cost = %v present=%v, want 1.5 untouched", id, v, present)
		}
	}
}

// After a zeroing the card counts again from 0 — 「從 0 重新開始累積」 is what the
// owner asked for, and a card that stayed absent while spending continued would
// be the silent under-reporting this design exists to avoid.
func TestResetAccountCost_NewSpendAfterTheResetCountsFromZero(t *testing.T) {
	s := costResetServer(t)
	seedRegisteredMachine(t, s, "m-seth-m5")
	seedWorker(t, s, "ow-7", "S7", 0, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":10}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	doResetAccountCost(t, s, "seth-m5-claude")

	// The SAME session keeps reporting its own cumulative figure, which is
	// larger than before. Only the increase is new money.
	if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":12}`); rec.Code != 200 {
		t.Fatalf("re-ingest: %d %s", rec.Code, rec.Body.String())
	}
	if got := accountCostOf(t, s, "seth-m5-claude"); got != 2.0 {
		t.Errorf("account = %v, want 2 — the reset zeroed the accumulator, and the "+
			"session's cumulative 12 adds only the 2 it has spent since", got)
	}
}

// An account tag is a free telemetry string with no roster row, so there is
// nothing to 404 against: "no such account" and "that account has nothing to
// clear" are the same state, and both are successes. The second press is the
// likely one — the owner has just cleared it — so it must not look like an
// error.
func TestResetAccountCost_NothingToClearIsSuccessAndBlankIsRefused(t *testing.T) {
	s := costResetServer(t)

	receipt := monitoringOf(t, doResetAccountCost(t, s, "nobody-reports-here"))
	if got := receipt["account"]; got != "nobody-reports-here" {
		t.Errorf("account = %v, want it echoed back", got)
	}
	if receipt["cleared_cost"] != nil {
		t.Errorf("cleared_cost = %v, want null — null means there was nothing to "+
			"clear, while 0 would read as 'zero was cleared'", receipt["cleared_cost"])
	}

	if rec := doResetAccountCost(t, s, "   "); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("blank account = %d, want 422 — a blank tag matches no account and "+
			"is a caller mistake, not an empty account", rec.Code)
	}
}
