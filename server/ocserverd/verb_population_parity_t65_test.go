package main

// verb_population_parity_t65_test.go — T-65 包①: the 動詞 × 人口 matrix.
//
// A staff member and an outsource worker live in the SAME member table, but the
// seven lifecycle verbs are TWO SEPARATE handler families. There is no way to
// feed one handler both populations — every member-side verb resolves through
// s.resolveMember(id, staffOnly), and api_helpers.go's
// `if scope == staffOnly && m.Kind == KindOutsource` 404s an ow- id outright.
// So the matrix is not "one handler, two inputs": it is
//
//	動詞 × 人口 ⇒ each population's OWN handler, same seeded start state,
//	             then COMPARE THE TERMINAL ROW field by field.
//
// Every field that is expected to end up the same is asserted equal, and every
// field that ends up DIFFERENT must appear in knownDivergences with a sentence
// saying why it is different TODAY.
//
// 🔴 WHAT IN THIS FILE IS ACTUALLY SENSITIVE TO PRODUCTION CODE — read this
// before trusting a green run here as a behaviour guard:
//
//	ONLY block ① of TestVerbPopulationParityMatrix (`gotStaff != c.wantStaff` /
//	`gotOutsource != c.wantOutsource`, 7 verbs × 2 populations = 14 assertions)
//	and TestAcceleratedStopWorkerHasAnExtraLifecycleGate call a handler and
//	compare what came back. Those are the mutant killers.
//
// Everything else here — the `UNDOCUMENTED DIVERGENCE` and `STALE WHITELIST
// ROW` branches, the orphan-row check, and
// TestVerbPopulationParityWhitelistIsExplained — compares LITERALS DECLARED IN
// THIS FILE against OTHER LITERALS DECLARED IN THIS FILE (`c.wantStaff` vs
// `c.wantOutsource`, and knownDivergences against the case list). Not one of
// their operands is read out of a handler, so their verdict cannot change when
// production code changes. Independent review measured both directions:
// introducing a brand-new divergence in api_members.go raised block ①, never
// `UNDOCUMENTED DIVERGENCE`; and genuinely CONVERGING a whitelisted cell in
// api_outsource.go raised block ①, never `STALE WHITELIST ROW`.
//
// They are kept because they are still worth their line count — as a lint THIS
// FILE runs on ITSELF. They catch a human editing the whitelist wrong: a row
// added for a cell no case exercises, a row left behind after its two literals
// were converged by hand, a divergence introduced into the literals without a
// reason written next to it. That is a real failure mode (whoever converges a
// verb in a later T-65 package edits both literals AND this whitelist), and the
// orphan check has been demonstrated to fire on it. Just do not read them as a
// guard over the handlers: converging a verb for real is caught by block ①,
// and the whitelist edit that must follow is caught by these.
//
// 🔴 TWO RULES THIS FILE IS BOUND BY, both learned the expensive way:
//
//  1. Assertions go through the REAL handler seam — the method routes.go's
//     `Handler:` field points at — never the pure helpers underneath
//     (armRefocusEpoch / stopEpochAnchor / respawnWorkerForOwnerOp). T-14 PR ①
//     shipped a parity test over the pure function; both CALL SITES could then
//     have their guards deleted with the whole suite still green.
//  2. Every expected value below is a LITERAL, transcribed by hand from the
//     assignment in the handler. Nothing here calls production code to compute
//     what production code should produce — an expectation derived that way is
//     true by construction and kills no mutant.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// ── the observable terminal state ────────────────────────────────────────────

// anchorClass buckets a float anchor relative to the instant the verb ran. The
// three buckets are what the divergences actually turn on: "was it cleared",
// "was it (re)stamped now", and — for 強制停止 — "was a FUTURE anchor pulled
// back to now". A raw float would make every row unwritable as a literal.
type anchorClass string

const (
	anchorZero   anchorClass = "zero"   // cleared / never set
	anchorPast   anchorClass = "past"   // <= the instant the verb ran
	anchorFuture anchorClass = "future" // still ahead of it (an untouched future stamp)
)

// terminalState is the row as both populations project it. GetMember and
// GetOutsourceWorker read the SAME table (migration 00025 folded it), so a
// worker is folded through memberFromWorker and the two are literally
// comparable.
type terminalState struct {
	Status           int
	DesiredState     string
	Stopping         anchorClass
	Stopped          anchorClass
	Refocus          anchorClass
	RefocusOp        string
	Waking           anchorClass
	RestartAfterStop bool
	DesiredMachineID string
}

// parityFields is the compared field set, in a stable order so a failure names
// the same field every run.
var parityFields = []string{
	"http_status", "desired_state", "stopping_since", "stopped_since",
	"refocus_since", "refocus_op", "waking_since", "restart_after_stop",
	"desired_machine_id",
}

func (s terminalState) field(name string) any {
	switch name {
	case "http_status":
		return s.Status
	case "desired_state":
		return s.DesiredState
	case "stopping_since":
		return s.Stopping
	case "stopped_since":
		return s.Stopped
	case "refocus_since":
		return s.Refocus
	case "refocus_op":
		return s.RefocusOp
	case "waking_since":
		return s.Waking
	case "restart_after_stop":
		return s.RestartAfterStop
	case "desired_machine_id":
		return s.DesiredMachineID
	}
	panic("unknown parity field " + name)
}

// ── the known-divergence whitelist ───────────────────────────────────────────

// knownDivergence is ONE cell of the matrix that does not match today, with the
// reason it does not. `why` is the whole point of the row: a divergence with no
// explanation is a bug nobody has looked at yet, and this file refuses to hold
// one silently — where the code carries no explanation the row says exactly
// that, rather than inventing one.
type knownDivergence struct {
	verb  string
	field string
	why   string
}

var knownDivergences = []knownDivergence{
	// ── 起來 (activate ↔ restart) ──────────────────────────────────────────
	{
		verb: "起來", field: "waking_since",
		why: "正職 activate assigns m.WakingSince = 0.0 (api_members.go, " +
			"HandleActivateMember…) and waking_since is NOT in singleColumnOwnedFields, " +
			"so PutMember carries the clear. (That registry is declared in the TEST file " +
			"single_column_writes_t14_test.go — `var singleColumnOwnedFields` at the top " +
			"of it, with a length assertion pinning it at 17 entries; grepping only " +
			"non-test .go files will not find it. Its last four entries are the wind-down " +
			"anchors stopping_since / stopped_since / refocus_since / refocus_op, and " +
			"waking_since is absent from all 17.) 外包 restart deliberately does NOT clear it: " +
			"its 🔴 block says notifyWorkerSpawn stamps a fresh anchor on the re-dispatch, " +
			"and names the residue it leaves (a failed re-dispatch reads 喚醒中 until the " +
			"TTL lapses) as 「the same 正職／外包 divergence T-14 exists to delete」.",
	},
	{
		verb: "起來", field: "stopped_since",
		why: "外包 restart assigns worker.StoppedSince = 0.0 — 「A RESTART STARTS A NEW " +
			"SESSION, SO IT STARTS FROM A CLEAN SHEET」 (T-ed79 #11): the anchor dates the " +
			"session being REPLACED, and the pair (refocus>0 ∧ stopped>0) is read by " +
			"workerHasStateToFlush as 「already collected」. 正職 activate touches neither: " +
			"clearMemberHandoverMarker's own comment states 「activate clears stopping_since " +
			"and waking_since and deliberately clears NEITHER refocus_since nor " +
			"stopped_since」. The two clear sets are complements, not one set of two sizes.",
	},
	{
		verb: "起來", field: "refocus_since",
		why: "Same clear-set complement as stopped_since above: 外包 restart zeroes " +
			"worker.RefocusSince, 正職 activate leaves it and persistMemberWindDownAnchors " +
			"writes back the value it read.",
	},
	{
		verb: "起來", field: "refocus_op",
		why: "The cause travels with its epoch: 外包 restart zeroes worker.RefocusOp " +
			"alongside RefocusSince; 正職 activate writes back what it read.",
	},

	// ── 重新聚焦 (refocus) — CONVERGED IN T-65 包②, no rows left ────────────
	// Both rows that stood here are DELETED rather than widened: 「重新聚焦｜
	// http_status」 (200 vs 409) and 「重新聚焦｜restart_after_stop」 (staff-only
	// column). The worker face now takes the same aStopWasEverAskedFor branch the
	// member face does — see queueWorkerRestartAfterStop (member_ownerop_winddown.go)
	// and the 🔴 block in the worker refocus handler. Block ① is what proves it:
	// the two literals for this verb are now identical, so a regression on either
	// side reddens the matrix rather than being absorbed by a whitelist row.

	// ── 強制停止 (force-stop) ──────────────────────────────────────────────
	{
		verb: "強制停止", field: "stopping_since",
		why: "外包 force-stop pulls a FUTURE anchor back: `if worker.StoppingSince <= 0.0 " +
			"|| worker.StoppingSince > forcedAt { worker.StoppingSince = forcedAt }` " +
			"(api_outsource.go). 正職 force-stop has only the first arm: `if " +
			"m.StoppingSince <= 0.0 { m.StoppingSince = nowSecs() }` (api_members.go), so " +
			"a future stamp survives. 🔴 THE SECOND ARM IS LOAD-BEARING AND ITS REASON IS " +
			"ON THE RECORD: `git log -S 'worker.StoppingSince > forcedAt' -- " +
			"server/ocserverd/api_outsource.go` returns exactly one commit, 7bc889c3 " +
			"(T-c996, #245), whose message says the two anchors are stamped together " +
			"because 「forcedEpochLive scopes the record to a live epoch (forced_stop_at " +
			">= stopping_since); one without the other leaves a worker that announced its " +
			"own wind-down reading as \"still working its close-out\", which is the arm " +
			"that speaks」. forcedEpochLive (api_members.go) is `ForcedStopAt > 0 && " +
			"StoppingSince > 0 && ForcedStopAt >= StoppingSince`, so the pull-back is what " +
			"keeps that invariant true: force-stop sets ForcedStopAt = forcedAt, and a " +
			"stopping_since ahead of forcedAt would make forcedEpochLive FALSE. " +
			"⇒ THE DIRECTION OF THIS ROW IS THE OPPOSITE OF WHAT IT LOOKS LIKE: the side " +
			"that may be defective is 正職, which lacks the arm — a staff row whose " +
			"stopping_since sat in the future would come out of 強制停止 reading as a " +
			"GRACEFUL wind-down still in progress (notice sent, deadline granted, " +
			"加速停止 admitted), which is precisely the state T-c996 removed. " +
			"⚠️ REACHABILITY, stated with its scope: independent review grepped every " +
			"`.StoppingSince = ` assignment in non-test server code and found the staff-" +
			"side writers all write nowSecs() / now / 0.0 / stopEpochAnchor(…, nowSecs()) " +
			"— that grep found NO production path that stamps a FUTURE staff " +
			"stopping_since. That is the reach of one grep, not a proof of impossibility. " +
			"So today the divergence is unreachable in effect; if a path is ever found or " +
			"added, the fix belongs on the 正職 side (give it the second arm), not here.",
	},
}

func divergenceIndex() map[[2]string]knownDivergence {
	idx := make(map[[2]string]knownDivergence, len(knownDivergences))
	for _, d := range knownDivergences {
		idx[[2]string{d.verb, d.field}] = d
	}
	return idx
}

// ── the matrix ───────────────────────────────────────────────────────────────

// verbCase is one row of 動詞 × 人口: a start state both populations can be
// seeded into, the two handler calls, and the two LITERAL terminal states.
type verbCase struct {
	verb string
	// note says what the seeded start state is, so a failure reads without
	// scrolling back up to the seeder.
	note string
	// runStaff / runOutsource each seed their own population into the shared
	// start state, call the population's own routes.go handler, and read the row
	// back. They return the terminal state ONLY — no expectation logic.
	runStaff     func(t *testing.T) terminalState
	runOutsource func(t *testing.T) terminalState
	// wantStaff / wantOutsource are hand-transcribed literals. See rule 2 in the
	// file header: nothing below is computed by the code under test.
	wantStaff     terminalState
	wantOutsource terminalState
}

// ── fixtures ─────────────────────────────────────────────────────────────────

const (
	parityMachineA = ServerSelfHost
	parityMachineB = "m-parity-b"
	parityPast     = 1000.0 // seeded anchors sit far in the past
	parityFuture   = 4.0e9  // …and the 強制停止 case needs one far in the future
)

// newParityServer is ONE server that must hold BOTH populations. Whether that
// works at all was the first open question of this package: the staff fixtures
// come from reconcile_test.go (seedOutOfBox roles + a real docs root) and the
// worker fixtures from worker_lifecycle_test.go (a seeded+connected warden and
// an outsource manual). They are combined here rather than run on two servers so
// that the two arms of a row cannot silently diverge on their ENVIRONMENT.
func newParityServer(t *testing.T) *apiServer {
	t.Helper()
	api := newReconcileTestServer(t)
	api.noOutsource = true // no background outsource tick racing the handler calls
	seedLiveWorkerEnv(t, api)
	seedMachine(t, api, parityMachineB)
	return api
}

// seedParityMember plants a staff member in the shared start state. The four
// wind-down anchors go through putTestMember's second write (their sole writer,
// T-55) — a whole-row PutMember would silently drop them and the assertion would
// be made against a state that was never planted.
func seedParityMember(t *testing.T, api *apiServer, id string, mutate func(*Member)) {
	t.Helper()
	m := testAgent(id)
	m.DesiredMachineID = parityMachineA
	m.Model = "claude-sonnet-4-5"
	if mutate != nil {
		mutate(&m)
	}
	putTestMember(t, api, m)
	connectOnlineMachine(t, api, id, parityMachineA)
}

// seedParityWorker plants the outsource twin. newActiveOnlineWorker already
// builds an active + online worker pinned to parityMachineA; the anchors it does
// NOT carry are planted afterwards through seedWorkerAnchors, the same sole
// writer, for the same reason.
func seedParityWorker(t *testing.T, api *apiServer, mutate func(*OutsourceWorker)) string {
	t.Helper()
	id := newActiveOnlineWorker(t, api)
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("seed worker: %v", err)
	}
	if mutate != nil {
		mutate(w)
	}
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
	return id
}

func classify(v, at float64) anchorClass {
	switch {
	case v <= 0.0:
		return anchorZero
	case v > at:
		return anchorFuture
	default:
		return anchorPast
	}
}

// memberTerminal / workerTerminal read the row back and bucket its anchors
// against the instant the READ happens — deliberately AFTER the handler has
// returned. Sampling `now` before the call instead makes every anchor the
// handler stamps land in the FUTURE bucket, which is a harness artefact that
// looks exactly like a behaviour change.
func memberTerminal(t *testing.T, api *apiServer, id string, code int) terminalState {
	t.Helper()
	at := nowSecs()
	m, err := api.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("read back member %s: %v", id, err)
	}
	return terminalState{
		Status:           code,
		DesiredState:     m.DesiredState,
		Stopping:         classify(m.StoppingSince, at),
		Stopped:          classify(m.StoppedSince, at),
		Refocus:          classify(m.RefocusSince, at),
		RefocusOp:        m.RefocusOp,
		Waking:           classify(m.WakingSince, at),
		RestartAfterStop: m.RestartAfterStop,
		DesiredMachineID: m.DesiredMachineID,
	}
}

func workerTerminal(t *testing.T, api *apiServer, id string, code int) terminalState {
	t.Helper()
	at := nowSecs()
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back worker %s: %v", id, err)
	}
	// A worker row IS a member row; fold it so the two sides are the same type.
	m := memberFromWorker(*w)
	return terminalState{
		Status:           code,
		DesiredState:     m.DesiredState,
		Stopping:         classify(m.StoppingSince, at),
		Stopped:          classify(m.StoppedSince, at),
		Refocus:          classify(m.RefocusSince, at),
		RefocusOp:        m.RefocusOp,
		Waking:           classify(m.WakingSince, at),
		RestartAfterStop: m.RestartAfterStop,
		DesiredMachineID: m.DesiredMachineID,
	}
}

func postMember(t *testing.T, api *apiServer, id, op string, body any,
	h func(http.ResponseWriter, *http.Request, string)) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, taskReq(t, "POST", "/api/members/"+id+"/"+op, body, wireOwnerID, "owner"), id)
	return rec.Code
}

// ── the cases ────────────────────────────────────────────────────────────────

func parityCases() []verbCase {
	return []verbCase{
		{
			verb: "起來",
			note: "seed: desired offline, all four anchors + waking_since stamped in the past, " +
				"live session. 正職 → POST /activate, 外包 → POST /restart.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-up", func(m *Member) {
					m.DesiredState = DesiredStateOffline
					m.StoppingSince = parityPast
					m.StoppedSince = parityPast
					m.RefocusSince = parityPast
					m.RefocusOp = refocusOpRefocus
					m.WakingSince = parityPast
				})
				code := postMember(t, api, "m-parity-up", "activate", nil,
					api.HandleActivateMemberApiMembersMemberIdActivatePost)
				return memberTerminal(t, api, "m-parity-up", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
					w.StoppedSince = parityPast
					w.RefocusSince = parityPast
					w.RefocusOp = refocusOpRefocus
					w.WakingSince = parityPast
				})
				code := postWorker(t, api, id, "restart", nil,
					api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// 正職 activate: m.StoppingSince = 0.0; m.WakingSince = 0.0;
			// m.DesiredState = DesiredStateOnline; clearRestartIntent(m).
			// stopped_since / refocus_since / refocus_op are written back as read.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorPast,
				Refocus: anchorPast, RefocusOp: refocusOpRefocus,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			// 外包 restart: worker.DesiredState = DesiredStateOnline;
			// worker.RefocusSince = 0.0; worker.RefocusOp = "";
			// worker.StoppingSince = 0.0; worker.StoppedSince = 0.0.
			// waking_since is deliberately left alone (its own 🔴 note).
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorPast, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "停止",
			note: "seed: desired online, live session, an OPEN 換手 epoch. 正職 → " +
				"POST /deactivate, 外包 → POST /stop. 🟢 POSITIVE CONTROL ROW: both " +
				"sides stamp the epoch through the SAME pure function (stopEpochAnchor), " +
				"so if THIS row goes red the harness is broken, not the subject.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-stop", func(m *Member) {
					m.RefocusSince = parityPast
					m.RefocusOp = refocusOpRefocus
				})
				code := postMember(t, api, "m-parity-stop", "deactivate", nil,
					api.HandleDeactivateMemberApiMembersMemberIdDeactivatePost)
				return memberTerminal(t, api, "m-parity-stop", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.RefocusSince = parityPast
					w.RefocusOp = refocusOpRefocus
				})
				code := postWorker(t, api, id, "stop", nil,
					api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// 正職: desired offline + clearMemberHandoverMarker + clearRestartIntent
			// + StoppingSince = stopEpochAnchor(...) → now (no forced epoch live).
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			// 外包: desired offline; RefocusSince = 0.0; RefocusOp = "";
			// StoppingSince = stopEpochAnchor(memberFromWorker(...)) → the same now.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "加速停止",
			note: "seed: an OPEN 下線 epoch (desired offline + stopping_since in the past), " +
				"live session, worker Status=active. Both → POST /accelerated-stop.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-accel", func(m *Member) {
					m.DesiredState = DesiredStateOffline
					m.StoppingSince = parityPast
				})
				code := postMember(t, api, "m-parity-accel", "accelerated-stop", nil,
					api.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost)
				return memberTerminal(t, api, "m-parity-accel", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
				})
				code := postWorker(t, api, id, "accelerated-stop", nil,
					api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// Both: the desired-offline arm re-stamps its anchor from THIS press
			// (m.StoppingSince = now / worker.StoppingSince = nowSecs()) and writes
			// RefocusOp = refocusOpAcceleratedStop. 正職 additionally calls
			// clearRestartIntent — which is a no-op on a row that carries no queued
			// 起來, so the two terminal rows agree here.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: refocusOpAcceleratedStop,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: refocusOpAcceleratedStop,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "強制停止",
			note: "seed: desired online, live session, stopping_since stamped in the " +
				"FUTURE — the one start state that separates the two force-stop bodies.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-force", func(m *Member) {
					m.StoppingSince = parityFuture
				})
				code := postMember(t, api, "m-parity-force", "force-stop", nil,
					api.HandleForceStopMemberApiMembersMemberIdForceStopPost)
				return memberTerminal(t, api, "m-parity-force", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.StoppingSince = parityFuture
				})
				code := postWorker(t, api, id, "force-stop", nil,
					api.HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// 正職: `if m.StoppingSince <= 0.0 { m.StoppingSince = nowSecs() }` —
			// the future stamp is NOT touched.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorFuture, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			// 外包: `if worker.StoppingSince <= 0.0 || worker.StoppingSince > forcedAt
			// { worker.StoppingSince = forcedAt }` — the second arm pulls it back.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "重新聚焦",
			note: "seed: the owner has ALREADY stopped this row (desired offline + " +
				"stopping_since in the past) and the session is still live. Both → refocus.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-refocus", func(m *Member) {
					m.DesiredState = DesiredStateOffline
					m.StoppingSince = parityPast
				})
				code := postMember(t, api, "m-parity-refocus", "refocus", nil,
					api.HandleRefocusMemberApiMembersMemberIdRefocusPost)
				return memberTerminal(t, api, "m-parity-refocus", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
				})
				code := postWorker(t, api, id, "refocus", nil,
					api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// 正職: !aRefocusStampWouldReachTheAgent && aStopWasEverAskedFor →
			// stampRestartIntent(m) and a 200. The stop keeps its stage and anchors.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: true,
				DesiredMachineID: parityMachineA,
			},
			// 外包 (T-65 包②): the same branch, transcribed from the worker handler's
			// own assignment — `queueWorkerRestartAfterStop(worker, refocusOpRefocus,
			// …)` sets RestartAfterStop and touches nothing else, then answers 200.
			// The eager outsourceTickNow that follows is a no-op here twice over:
			// newParityServer sets noOutsource, and the seeded session is ONLINE so
			// the consume's `!hub.IsOnline` gate would refuse it anyway.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: true,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "改機器",
			note: "seed: desired online, live session, pinned to machine A; both are " +
				"relocated to machine B. 🟢 desired_machine_id is a POSITIVE CONTROL: " +
				"both sides write it through the SAME sole writer, SetMemberDesiredMachineID.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-move", nil)
				code := postMember(t, api, "m-parity-move", "relocate",
					map[string]any{"machine_id": parityMachineB},
					api.HandleRelocateMemberApiMembersMemberIdRelocatePost)
				return memberTerminal(t, api, "m-parity-move", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, nil)
				code := postWorker(t, api, id, "relocate",
					map[string]any{"machine_id": parityMachineB},
					api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
				return workerTerminal(t, api, id, code.Code)
			},
			// Both arm the wind-down through the SAME predicate
			// (hasUncollectedOnlineOwnerOpState: online ∧ ¬(refocus>0 ∧ stopped>0)) and
			// stamp the epoch through the SAME armRefocusEpoch with op="relocate".
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: memberOpRelocate,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineB,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: ownerOpRelocate,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineB,
			},
		},
		{
			verb: "換 model",
			note: "seed: desired online, live session, model=claude-sonnet-4-5; both are " +
				"moved to claude-opus-4-8. 正職 → PATCH /api/members/{id} (the model face " +
				"routes.go points at), 外包 → POST /api/outsource-workers/{id}/model.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-model", nil)
				rec := httptest.NewRecorder()
				api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
					taskReq(t, "PATCH", "/api/members/m-parity-model",
						map[string]any{"model": "claude-opus-4-8"}, wireOwnerID, "owner"),
					"m-parity-model")
				return memberTerminal(t, api, "m-parity-model", rec.Code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, nil)
				code := postWorker(t, api, id, "model",
					map[string]any{"model": "claude-opus-4-8"},
					api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// Both gate on 「a launch intent actually changed」 and then open the same
			// wind-down with op = "runtime/model" (memberOpModel == ownerOpModel).
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: memberOpModel,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: ownerOpModel,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
	}
}

// ── the matrix test ──────────────────────────────────────────────────────────

// TestVerbPopulationParityMatrix has two halves that must not be confused.
//
// Block ① is the guard: for every 動詞 × 人口 cell it pins the terminal row the
// handler actually produced against a hand-written literal. That is the only
// part of this function that can see production code, and it is what kills a
// behaviour mutant on either side.
//
// Block ② and the orphan check are a lint over this file's own literals: fields
// whose two literals disagree MUST carry a knownDivergences row, fields whose
// two literals agree must NOT, and every whitelist row must name a cell the
// matrix runs. They keep the whitelist in step with the literals when a human
// edits either. They do not observe the handlers — see the file header.
func TestVerbPopulationParityMatrix(t *testing.T) {
	idx := divergenceIndex()
	seen := map[[2]string]bool{}

	for _, c := range parityCases() {
		c := c
		t.Run(c.verb, func(t *testing.T) {
			gotStaff := c.runStaff(t)
			gotOutsource := c.runOutsource(t)

			// ① each side against its own literal — the behaviour mutant killer.
			if gotStaff != c.wantStaff {
				t.Errorf("正職 %s terminal state changed.\n  start: %s\n   want: %+v\n    got: %+v",
					c.verb, c.note, c.wantStaff, gotStaff)
			}
			if gotOutsource != c.wantOutsource {
				t.Errorf("外包 %s terminal state changed.\n  start: %s\n   want: %+v\n    got: %+v",
					c.verb, c.note, c.wantOutsource, gotOutsource)
			}

			// ② the whitelist lint. Both operands below (`want`, `other`) are
			// literals declared in THIS file, and so is knownDivergences — this
			// loop cannot see production code at all. It exists to stop a human
			// editing the literals and the whitelist out of step. See the 🔴
			// block in the file header.
			for _, f := range parityFields {
				key := [2]string{c.verb, f}
				want := c.wantStaff.field(f)
				other := c.wantOutsource.field(f)
				d, listed := idx[key]
				if listed {
					seen[key] = true
				}
				switch {
				case want != other && !listed:
					t.Errorf("UNDOCUMENTED DIVERGENCE %s|%s: 正職 ends %v, 外包 ends %v.\n"+
						"  Either the two handlers were meant to converge and one of them "+
						"regressed, or this is a real difference that must be added to "+
						"knownDivergences with a sentence saying why it is different today.",
						c.verb, f, want, other)
				case want == other && listed:
					t.Errorf("STALE WHITELIST ROW %s|%s: both populations now end %v, so this "+
						"divergence is CLOSED. Delete the knownDivergences row (it still says: %s)",
						c.verb, f, want, d.why)
				}
				// There used to be a third branch here comparing the OBSERVED
				// rows (`gotStaff.field(f) != gotOutsource.field(f)`) on every
				// agreeing field, announcing itself as "the assertion that still
				// fires if both literals are edited in lockstep". That claim was
				// false and the branch was dead weight: reaching it needs
				// want == other, and if block ① passed then gotStaff == wantStaff
				// and gotOutsource == wantOutsource, so the two observed values
				// are equal by substitution. It could only ever print a second
				// line underneath a block ① failure — never fire alone. Deleted
				// rather than re-commented: a guard that cannot fail on its own is
				// exactly what this ticket exists to remove (cf. #401).
			}
		})
	}

	// Every whitelist row must belong to a cell this table actually exercises —
	// otherwise a row could be kept alive by a verb that no longer runs. This is
	// a lint over the two literal lists in this file (knownDivergences vs
	// parityCases), not a handler assertion; it fires when a human adds a row
	// naming a verb/field the matrix does not run. Demonstrated to fire: adding
	// a whitelist row for a verb that is not in parityCases reddens it.
	var orphans []string
	for k := range idx {
		if !seen[k] {
			orphans = append(orphans, fmt.Sprintf("%s|%s", k[0], k[1]))
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		t.Errorf("knownDivergences rows that no matrix cell covers: %v — a whitelist row "+
			"whose verb is not run is documentation, not a guard", orphans)
	}
}

// TestVerbPopulationParityWhitelistIsExplained keeps the whitelist honest: a row
// with no `why` is a divergence nobody has looked at, and the whole value of the
// table is that each surviving difference carries its reason.
//
// It is a lint over knownDivergences — a literal in this file — and touches no
// handler. It cannot detect anything about production code; it detects a human
// adding a whitelist row without writing down the reason.
func TestVerbPopulationParityWhitelistIsExplained(t *testing.T) {
	for _, d := range knownDivergences {
		if d.verb == "" || d.field == "" {
			t.Errorf("knownDivergences row with an empty verb/field: %+v", d)
		}
		if len(d.why) < 40 {
			t.Errorf("knownDivergences %s|%s has no real explanation (%q). A divergence "+
				"without a reason is a bug that has not been looked at yet; if the code "+
				"carries no explanation, say exactly that.", d.verb, d.field, d.why)
		}
	}
}

// TestAcceleratedStopWorkerHasAnExtraLifecycleGate is the one divergence the
// matrix above CANNOT express as a shared start state: 加速停止 on the worker
// side additionally requires `worker.Status != WorkerStatusActive` to be false,
// and a staff member has no Status column to be non-active in. It is asserted
// one-sidedly rather than dropped, because "no shared start state" is not the
// same as "not a divergence".
func TestAcceleratedStopWorkerHasAnExtraLifecycleGate(t *testing.T) {
	api := newParityServer(t)
	id := seedParityWorker(t, api, func(w *OutsourceWorker) {
		w.DesiredState = DesiredStateOffline
		w.StoppingSince = parityPast
		w.Status = WorkerStatusAssigned // online, open epoch — only Status refuses
	})
	rec := postWorker(t, api, id, "accelerated-stop", nil,
		api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost)
	if rec.Code != http.StatusConflict {
		t.Fatalf("加速停止 on a non-active but ONLINE worker with an open stop epoch = %d %s, "+
			"want 409. The worker handler gates on `worker.Status != WorkerStatusActive || "+
			"!s.hub.IsOnline(...)`; the staff twin gates on liveness ALONE, so this arm is "+
			"外包-only and has no member analogue.", rec.Code, rec.Body.String())
	}
	// NEGATIVE CONTROL: the same worker with Status=active is admitted, so the
	// 409 above is the Status gate and not some other refusal on the way in.
	api2 := newParityServer(t)
	id2 := seedParityWorker(t, api2, func(w *OutsourceWorker) {
		w.DesiredState = DesiredStateOffline
		w.StoppingSince = parityPast
	})
	if rec := postWorker(t, api2, id2, "accelerated-stop", nil,
		api2.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost); rec.Code != http.StatusOK {
		t.Fatalf("control: the SAME state with Status=active must be admitted, got %d %s",
			rec.Code, rec.Body.String())
	}
}
