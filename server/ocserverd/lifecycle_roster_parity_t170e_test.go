package main

import (
	"sort"
	"testing"
)

// lifecycle_roster_parity_t170e_test.go — T-170e stage 3. The mechanism that
// makes 「剩餘部分的一樣是程式碼階層的一樣，不是又複製了兩份程式碼寫一樣的行為」
// testable rather than aspirational.
//
// The failure this file exists to catch, in the owner's words, is somebody
// giving a formality to 正職 and 完全不管外包. Before the shared list that could
// not fail: the worker roster never passes through runReconcileTick (then via
// ListMembers' `WHERE kind != 'outsource'`, since T-14 項目 6 via that half's
// driver guard), so a staff-only pass and a pass that does not
// exist are INDISTINGUISHABLE from a worker's side. It happened twice already
// (the token-expiry lead and the survived-stop sweep, both missing until stage
// 1), and neither was visible in any test.
//
// Two faces, and both are needed:
//
//	face ① — DECLARED reach. lifecycleRosterPasses is the ONE list; this test
//	  reads back, BY NAME, which rows each pass says it is for. A pass that
//	  quietly narrows itself to staff fails here naming itself.
//	face ② — REAL reach. A declaration nothing honours is prose. Every pass the
//	  list declares for every kind is driven through the OUTSOURCE producer
//	  against a fixture where it must fire, and the subtest is named after the
//	  pass, so the red line says WHICH formality stopped reaching a worker.

// ── face ①: the reach table is declared, in one place, by name ───────────────

// The reach every pass is contracted to have. Changing this map is how a
// divergence gets introduced ON PURPOSE — and it is a diff a reviewer can see,
// which is the entire difference between this and the two hand-maintained call
// lists it replaced.
//
// "everyKind" is the default and the point: a formality is for the whole roster
// unless somebody writes down why it is not.
var lifecyclePassContractedReach = map[string]string{
	lifecyclePassContextHigh:   "everyKind",
	lifecyclePassTokenExpiry:   "everyKind",
	lifecyclePassStaleStopping: "everyKind",
	// The one honest exception. A worker's loop-break is autoHandoverWorker arm
	// (1) and it asks a DIFFERENT question (gauge boot_ts > refocus_since, not
	// "desired online ∧ not online"); two collectors on one latch is the
	// double-kill shape T-72dd removed. See the comment on the pass itself.
	lifecyclePassRecycleBreak: "staffOnly",
	// Warden-only, and it always was — the pass's own loop opens with
	// `m.Kind != KindWarden { continue }`.
	lifecyclePassUninstallIntent: "wardenOnly",
}

func TestLifecycleRosterPasses_TheReachOfEveryFormalityIsDeclaredByName(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true

	staff := Member{ID: "m-reach", Kind: KindAssistant, RosterStatus: RosterStatusActive}
	warden := Member{ID: "mach-reach", Kind: KindWarden, RosterStatus: RosterStatusActive}
	worker := Member{ID: "ow-reach", Kind: KindOutsource, RosterStatus: RosterStatusActive}

	seen := map[string]bool{}
	for _, p := range api.lifecycleRosterPasses() {
		if seen[p.Name] {
			t.Fatalf("pass %q appears twice in lifecycleRosterPasses — the list is the "+
				"single source of truth and cannot say two things", p.Name)
		}
		seen[p.Name] = true

		want, known := lifecyclePassContractedReach[p.Name]
		if !known {
			t.Fatalf("a formality named %q was added to lifecycleRosterPasses with no "+
				"entry in lifecyclePassContractedReach. Every pass has to say who it "+
				"is for — 正職 only, or the whole roster — because a pass that reaches "+
				"only 正職 is invisible from the 外包 side and cannot fail out loud",
				p.Name)
		}
		got := "everyKind"
		switch {
		case p.AppliesTo(staff) && p.AppliesTo(worker) && p.AppliesTo(warden):
			got = "everyKind"
		case p.AppliesTo(staff) && p.AppliesTo(warden) && !p.AppliesTo(worker):
			got = "staffOnly"
		case p.AppliesTo(warden) && !p.AppliesTo(staff) && !p.AppliesTo(worker):
			got = "wardenOnly"
		default:
			got = "unclassified"
		}
		if got != want {
			t.Errorf("formality %q now reaches %q, contracted %q — 外包＝正職 is a "+
				"code-level sameness (migration 00025). If this narrowing is "+
				"deliberate, say so in lifecyclePassContractedReach WITH the reason, "+
				"the way recycle_loop_break does", p.Name, got, want)
		}
	}

	missing := []string{}
	for name := range lifecyclePassContractedReach {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("contracted formalities missing from lifecycleRosterPasses: %v — "+
			"a pass that leaves the list stops running for BOTH sides silently", missing)
	}
}

// ── face ②: the declared reach is real, measured on the 外包 producer ─────────

// Each subtest is named after the pass. The point of the naming is the red
// line: "the outsource producer no longer runs token_expiry_winddown" is a
// finding; "a worker field did not change" is a puzzle.
func TestLifecycleRosterPasses_TheOutsourceProducerReallyRunsEveryEveryKindPass(t *testing.T) {
	// Every everyKind pass, with a fixture in which it MUST fire and the
	// observable it leaves behind. Driven through runOutsourceTick — the real
	// 外包 producer entry point, not a hand-copy of it.
	cases := []struct {
		pass  string
		arm   func(t *testing.T, api *apiServer, w *OutsourceWorker, now float64)
		check func(t *testing.T, got OutsourceWorker)
	}{
		{
			pass: lifecyclePassContextHigh,
			arm: func(t *testing.T, api *apiServer, w *OutsourceWorker, now float64) {
				ctxhigh := api.ctxHighConfig()
				w.SessionBootTS = now - (ctxhigh.MinBootSecs + 3600)
				api.gauge.Set(w.ID, map[string]any{
					"boot_ts":        w.SessionBootTS,
					"context_pct":    ctxhigh.HandoverPct + 1,
					"context_pct_ts": now,
				})
			},
			check: func(t *testing.T, got OutsourceWorker) {
				if got.RefocusOp != refocusOpContextHigh {
					t.Fatalf("refocus_op=%q, want %q — a worker over the handover line "+
						"was not wound down. The 外包 producer stopped running the "+
						"context-threshold formality that 正職 gets",
						got.RefocusOp, refocusOpContextHigh)
				}
			},
		},
		{
			pass: lifecyclePassTokenExpiry,
			arm: func(t *testing.T, api *apiServer, w *OutsourceWorker, now float64) {
				// One second inside the lead.
				w.SessionBootTS = now + tokenExpiryLeadSecs - 1 - float64(api.agentTokenTTLValue())
			},
			check: func(t *testing.T, got OutsourceWorker) {
				if got.RefocusOp != refocusOpTokenExpiry {
					t.Fatalf("refocus_op=%q, want %q — a worker whose token is about to "+
						"die was not asked to close out while the MCP calls that close "+
						"it out still work. The 外包 producer stopped running the "+
						"token-expiry formality that 正職 gets",
						got.RefocusOp, refocusOpTokenExpiry)
				}
			},
		},
		{
			pass: lifecyclePassStaleStopping,
			arm: func(t *testing.T, api *apiServer, w *OutsourceWorker, now float64) {
				// Ancient anchor, no gauge record — provably past that stop.
				w.StoppingSince = now - 10*SoftOffboardGraceSecs
			},
			check: func(t *testing.T, got OutsourceWorker) {
				if got.StoppingSince != 0.0 {
					t.Fatalf("stopping_since=%v on a worker that is desired-online AND "+
						"observed online AND silent for 10x the close-out window — the "+
						"cockpit reads 停止中 forever. The 外包 producer stopped running "+
						"the survived-stop formality that 正職 gets", got.StoppingSince)
				}
			},
		},
	}

	// Guard against this table quietly falling behind the list: every everyKind
	// pass must have a fixture here.
	covered := map[string]bool{}
	for _, c := range cases {
		covered[c.pass] = true
	}
	for name, reach := range lifecyclePassContractedReach {
		if reach == "everyKind" && !covered[name] {
			t.Fatalf("formality %q is contracted for every kind but has no 外包 fixture "+
				"in this table — a declaration nothing measures is prose", name)
		}
	}

	for _, c := range cases {
		t.Run(c.pass, func(t *testing.T) {
			api := newTasksTestServer(t)
			api.noOutsource = true
			workerID := newActiveOnlineWorker(t, api)
			now := nowSecs()

			w, err := api.dal.GetOutsourceWorker(workerID)
			if err != nil || w == nil {
				t.Fatalf("load worker: %v", err)
			}
			c.arm(t, api, w, now)
			if err := api.dal.PutOutsourceWorker(*w); err != nil {
				t.Fatalf("seed worker: %v", err)
			}
			seedWorkerAnchors(t, api, *w)

			api.runOutsourceTick(now)

			got, err := api.dal.GetOutsourceWorker(workerID)
			if err != nil || got == nil {
				t.Fatalf("reload worker: %v", err)
			}
			c.check(t, *got)
		})
	}
}

// ── the entry filter, once ───────────────────────────────────────────────────

// lifecyclePolicyFor replaced FOUR hand-copies of one question (runReconcileTick,
// reconcileMemberNow, runOutsourceTick's projection filter, and the outsource
// copy inside workerTickPass — the last written POSITIVELY, so grepping for the
// staff phrasing never found it). This pins both arms against the behaviour the
// copies had, since the extraction may not quietly become stricter or looser.
func TestLifecyclePolicy_TheEntryFilterIsOneQuestionWithTwoAnswers(t *testing.T) {
	active := func(m Member) Member { m.RosterStatus = RosterStatusActive; return m }

	cases := []struct {
		name string
		m    Member
		want bool
	}{
		{"staff, on the roster", active(Member{Kind: KindAssistant,
			DesiredState: DesiredStateOnline}), true},
		{"staff, soft-removed", Member{Kind: KindAssistant,
			RosterStatus: RosterStatusRemoved, DesiredState: DesiredStateOnline}, false},
		{"warden, plain", active(Member{Kind: KindWarden,
			DesiredState: DesiredStateOnline}), false},
		{"warden, being uninstalled", active(Member{Kind: KindWarden,
			DesiredState: DesiredStateUninstall}), true},
		{"worker, active and not held down", active(Member{Kind: KindOutsource,
			ActivatedTS: 1000, DesiredState: DesiredStateOnline}), true},
		{"worker, still only assigned", active(Member{Kind: KindOutsource,
			ActivatedTS: 0, DesiredState: DesiredStateOnline}), false},
		{"worker, held down by the owner", active(Member{Kind: KindOutsource,
			ActivatedTS: 1000, DesiredState: DesiredStateOffline}), false},
		{"worker, released", Member{Kind: KindOutsource,
			RosterStatus: RosterStatusRemoved, ActivatedTS: 1000,
			DesiredState: DesiredStateOnline}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lifecyclePolicyFor(c.m).ShouldExist(); got != c.want {
				t.Fatalf("ShouldExist()=%v, want %v — the entry filter is the ONE slot "+
					"the 正職/外包 difference may live in, so it may not drift from what "+
					"the four hand-copies it replaced answered", got, c.want)
			}
		})
	}
}

// ── the fold-back's one live wire ────────────────────────────────────────────
//
// 🔴 THIS TEST EXISTS BECAUSE A PREVIOUS PASS OVER THIS TICKET CONCLUDED THE
// FOUR-FIELD FOLD-BACK WAS DEAD CODE — "delete it and the whole suite is still
// green". That was true, and it was not evidence: "no test fails" and "this arm
// has no test" are the same observation. Below is the arm.
//
// The narrow path: a worker whose wind-down epoch is PROMOTED in this same tick
// (context_notice → context_high re-stamps refocus_since to `now`, deliberately
// — promoting in place would put the deadline minutes in the past and collect
// the agent on the very tick that announced it), while the gauge's boot_ts is
// NEWER than the epoch it started from. autoHandoverWorker's loop-break, running
// later in the SAME tick off the snapshot, asks exactly `boot_ts > refocus_since`
// — so it reads the PRE-promotion value without the fold-back and clears the
// epoch the promotion just opened. The 加速停止 the second context threshold
// exists to open would be erased inside its own tick, and nothing would say so.
func TestWorkerFoldBack_APromotionSurvivesTheLoopBreakInTheSameTick(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	now := nowSecs()
	ctxhigh := api.ctxHighConfig()

	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("load worker: %v", err)
	}
	// An epoch opened by the FIRST threshold (the promotable one), stamped
	// BEFORE the current session booted.
	epochStart := now - 600
	bootTS := now - 300
	w.RefocusSince = epochStart
	w.RefocusOp = refocusOpContextNotice
	w.StoppingSince = 0
	w.StoppedSince = 0
	w.SessionBootTS = bootTS
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
	// …and the gauge now over the SECOND threshold, so the promotion arm fires.
	api.gauge.Set(workerID, map[string]any{
		"boot_ts":        bootTS,
		"context_pct":    ctxhigh.HandoverPct + 1,
		"context_pct_ts": now,
	})
	if bootTS <= epochStart {
		t.Fatalf("fixture: boot_ts (%v) must be NEWER than the epoch (%v) or the "+
			"loop-break arm under test is unreachable", bootTS, epochStart)
	}

	api.runOutsourceTick(now)

	got, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || got == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if got.RefocusOp != refocusOpContextHigh || got.RefocusSince != now {
		t.Fatalf("after ONE tick: refocus_op=%q refocus_since=%v, want %q at %v.\n"+
			"The second context threshold promoted this worker to 加速停止 and the "+
			"loop-break, running later in the SAME tick, wiped it — it was still "+
			"reading the pre-promotion refocus_since off the snapshot. That is the "+
			"four-field fold-back in runWorkerLifecyclePasses; it is NOT dead code, "+
			"and this is the arm that proves it.",
			got.RefocusOp, got.RefocusSince, refocusOpContextHigh, now)
	}
}

// ── the fold-back's other half ───────────────────────────────────────────────
//
// The sibling above pins RefocusSince/RefocusOp end-to-end, through
// runOutsourceTick and a DAL re-read. This one pins the OTHER two folded
// fields, StoppingSince/StoppedSince, at runWorkerLifecyclePasses' own function
// boundary — because that is where their effect lives: nothing persists the
// fold-back, so its whole job is what the caller's slice says for the rest of
// the tick.
//
// It does NOT bypass the entry filter. It calls runWorkerLifecyclePasses, whose
// lifecyclePolicyFor door runs and admits, and it asserts that admission itself
// below — a row the door rejected would make the probe vacuous.
//
// The fixture is a stale wind-down latch on an ACTIVE desired-online worker
// whose gauge is over HandoverPct: stampContextHighRecycle → armRefocusEpoch
// sets RefocusSince=now and zeroes StoppingSince/StoppedSince on the ROSTER
// row. The precondition is checked by re-reading the DAL (the pass persists its
// own writes), so it holds under every fold-back mutant and the final assertion
// stays specific to the two lines under test.
//
// Fixture notes, both learned the hard way: SessionBootTS must be well OLDER
// than now or stampContextHighRecycle's boot-storm gate swallows the stamp, and
// StoppedSince must be older than SessionBootTS or the stale latch reads as
// already-collected and the row is skipped.
func TestWorkerFoldBack_AWindDownClearSurvivesTheLoopBreakInTheSameTick(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	now := nowSecs()
	ctxhigh := api.ctxHighConfig()

	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("load worker: %v", err)
	}
	bootTS := now - 5000
	w.RefocusSince = 0
	w.RefocusOp = ""
	w.StoppingSince = now - 6000 // stale latches the stamp pass must wipe
	w.StoppedSince = now - 6000
	w.SessionBootTS = bootTS
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
	api.gauge.Set(workerID, map[string]any{
		"boot_ts":        bootTS,
		"context_pct":    ctxhigh.HandoverPct + 1,
		"context_pct_ts": now,
	})
	if w.StoppedSince >= bootTS {
		t.Fatalf("fixture: stopped_since (%v) must be OLDER than boot_ts (%v) or "+
			"the stale latch reads as already collected and the row is skipped",
			w.StoppedSince, bootTS)
	}
	if !lifecyclePolicyFor(memberFromWorker(*w)).ShouldExist() {
		t.Fatalf("fixture: the entry filter rejected this row, so nothing below " +
			"exercises the fold-back — the probe would be vacuous")
	}

	ws := []OutsourceWorker{*w}
	api.outsourceMu.Lock()
	api.runWorkerLifecyclePasses(ws, now)
	api.outsourceMu.Unlock()

	row, rerr := api.dal.GetOutsourceWorker(workerID)
	if rerr != nil || row == nil {
		t.Fatalf("reload worker: %v", rerr)
	}
	if row.RefocusSince != now || row.StoppingSince != 0 || row.StoppedSince != 0 {
		t.Fatalf("precondition: the stamp pass did not run or did not persist "+
			"(refocus_since=%v stopping_since=%v stopped_since=%v); the assertion "+
			"below would not be about the fold-back",
			row.RefocusSince, row.StoppingSince, row.StoppedSince)
	}
	if ws[0].StoppingSince != 0 || ws[0].StoppedSince != 0 {
		t.Fatalf("FOLD-BACK GAP: after the passes ran, the caller's snapshot still "+
			"carries stopping_since=%v stopped_since=%v, want 0/0. armRefocusEpoch "+
			"zeroed them on the roster row; the two fold-back lines in "+
			"runWorkerLifecyclePasses are what put that on the worker row the rest "+
			"of the tick reads.",
			ws[0].StoppingSince, ws[0].StoppedSince)
	}
}
