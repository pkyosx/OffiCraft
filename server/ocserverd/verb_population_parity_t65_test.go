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
	"strings"
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

// terminalState is TWO things, and after T-65 包③ it is worth saying which is
// which before reading a failure message. An earlier version of this comment
// said only the first half — 「the row as both populations project it」 — and
// that sentence was already false when `dispatched` was added; a third
// non-database column makes it false three times over.
//
//	(a) THE ROW READ BACK. The nine fields in the first block below. GetMember
//	    and GetOutsourceWorker read the SAME table (migration 00025 folded it),
//	    so a worker is folded through memberFromWorker and the two are literally
//	    comparable, column by column.
//
//	(b) WHAT THIS VERB DID OUTSIDE THE ROW, observed after the handler returned.
//	    `Dispatched` (the warden FIFO), `Cost` (a fold of live telemetry and the
//	    durable banked_cost) and `Noticed` (the member-topic SSE frame) are not
//	    columns of the row this struct's first block reads back — nothing in a
//	    GetMember can see any of them. They are here because a stop's whole
//	    point is a side effect: converge every column above and the two
//	    populations can still send a different kill, bank different money, and
//	    say a different thing to the agent, with the matrix green.
//
// The two halves fail differently and must be debugged differently. A mismatch
// in (a) means a handler wrote a different value; a mismatch in (b) means a
// handler CALLED something different — and the call may be several frames away
// from the handler (起來's staff `stop` frame comes out of a reconcile the
// handler fired, not out of the handler).
type terminalState struct {
	// ── the ROW, as both populations project it ──────────────────────────────
	// GetMember and GetOutsourceWorker read the SAME table (migration 00025
	// folded it), so a worker is folded through memberFromWorker and these
	// fields are literally comparable.
	Status           int
	DesiredState     string
	Stopping         anchorClass
	Stopped          anchorClass
	Refocus          anchorClass
	RefocusOp        string
	Waking           anchorClass
	RestartAfterStop bool
	DesiredMachineID string

	// ── what the verb did OUTSIDE the row (T-65 包③) ─────────────────────────
	// NOT read back from the database: observed from the warden's command FIFO
	// after the handler returned. 包③ converges the STOP verbs, and a stop's
	// whole point is a side effect — whether a kill was dispatched at all, and
	// under which RPC. None of that appears on the row, so a matrix built from
	// the row alone is green no matter what 包③ does to the dispatch.
	Dispatched dispatchSet

	// ── what the verb did to the MONEY (T-65 包③) ────────────────────────────
	// Also not a database column in the sense the nine above are: it is a fold
	// of TWO independently-read facts — the live telemetry figure (in-memory,
	// s.telemetry) and the durable banked_cost — taken after the handler
	// returned. A stop's other side effect is that it BANKS the dying session's
	// spend, and that is invisible to every column above: 強制停止 ends with the
	// same desired_state, the same anchors and the same one `stop` frame on both
	// sides, while only ONE of them has moved the owner-visible money.
	Cost costFold

	// ── what the verb SAID to the subject (T-65 包③) ─────────────────────────
	// The THIRD non-database column, and the only one whose reader is the AGENT
	// rather than the warden or the owner. Observed from a cockpit SSE
	// connection opened after the seeding and drained after the handler
	// returned. Plain owner-only roster refreshes are not lifecycle notices.
	Noticed noticeSet
}

// dispatchSet is every RPC this verb queued FOR THIS SUBJECT, sorted and joined
// with "+", so a whole dispatch outcome is one writable literal: "" for
// "dispatched nothing", "stop" for one frame, "stop+uninstall" for two. Sorted
// rather than in emission order on purpose — the ORDER two frames leave in is
// not a property this matrix is asserting, and making it one would turn an
// unrelated refactor into a red cell.
type dispatchSet string

const dispatchedNothing dispatchSet = ""

// costFold is what the verb did to the subject's LIVE telemetry cost, as one
// writable literal. It is a fold of two facts read back INDEPENDENTLY after the
// handler returned — whether the live figure is still in s.telemetry, and what
// the durable banked_cost says — because the failure this column exists to see
// is precisely the two disagreeing: bankLiveCost's own comment names "the old
// member-only fold silently destroyed a worker's cost here" (api_infra.go:906),
// and a column that only asked "did the live figure go away" would score that
// destruction as a successful bank.
type costFold string

const (
	// costUntouched — the live figure is still there. No fold ran.
	costUntouched costFold = "live"
	// costBanked — the live figure was popped AND banked_cost took exactly it.
	costBanked costFold = "banked"
	// costLost — the live figure was popped and banked_cost did NOT take it.
	// 🔴 No cell is expected to be this today; it is here so that the shape has
	// a NAME rather than being folded into "banked". Double-banking lands here
	// too (banked == 2×), which is the other half of the same accounting bug.
	costLost costFold = "lost"
)

// parityLiveCost is the live telemetry cost both seeders plant on the subject.
//
// 🔴 IT MUST BE NON-ZERO, and the reason is NARROWER than it first looks. The
// four-cell control below was actually run (T-65 包③); the first draft of this
// comment claimed 0.0 makes the column inert, and MEASURING IT PROVED THAT
// WRONG, so what is written here is the measurement and not the intuition:
//
//	seed  mutant                                            matrix
//	3.25  worker_spawn.go:1983 bankLiveCost dropped         RED
//	0.0   worker_spawn.go:1983 bankLiveCost dropped         RED
//	3.25  api_infra.go:939 banks 0 instead of cost          RED
//	0.0   api_infra.go:939 banks 0 instead of cost          GREEN  ← blind
//
// So a zero seed still catches "the fold never ran" — liveCostPresent keys on
// the KEY being there, not on its value. What it goes blind to is the costLost
// class: the pop happened and the money did not arrive. With banked==0 and
// parityLiveCost==0 that is indistinguishable from a correct bank. And that
// class is not hypothetical — it is the one bankLiveCost's own comment says
// already happened once ("the old member-only fold silently destroyed a
// worker's cost here", api_infra.go:906).
//
// Deleting the seeder calls outright is NOT a silent failure, also measured:
// every cell then reads costLost and the matrix fails loudly on all seven verbs,
// because banked(0) != parityLiveCost(3.25). That is the tripwire the exact
// comparison in classifyCost buys, and it is why this is == and not >=.
// 3.25 is exactly representable in float64, so a double-bank (6.5) also lands
// in costLost rather than rounding into costBanked.
const parityLiveCost = 3.25

// classifyCost folds the two reads. `liveStillPresent` is read from s.telemetry
// and `banked` off the row the terminal read already had in hand, so this adds
// no third database round-trip and — like every other expectation in this file —
// calls no production code to decide what production code should have produced.
func classifyCost(banked float64, liveStillPresent bool) costFold {
	switch {
	case liveStillPresent:
		return costUntouched
	case banked == parityLiveCost:
		return costBanked
	default:
		return costLost
	}
}

// liveCostPresent reports whether the subject's live telemetry cost survived the
// verb. memStore.Get returns a COPY (hub.go:1095-1108), so this is a read with
// no side effect and may be called in any order relative to dispatchedFor.
func liveCostPresent(api *apiServer, subjectID string) bool {
	_, ok := api.telemetry.Get(subjectID)["cost"]
	return ok
}

// noticeSet is what this verb SAID to the subject on its own SSE stream, as one
// writable literal. It folds every member-topic frame addressed to the subject
// that the verb fanned, into two answers.
//
// 🔴 TWO CLASSES, NOT FOUR, AND THE REASON IS NAMED. The obvious third shape
// is soft-vs-final — 「你有 N 秒」 versus 「照自己的節奏收尾」 — and this column
// deliberately does NOT distinguish them. Telling them apart without breaking
// this file's rule 2 (every expectation is a hand-transcribed LITERAL, nothing
// calls production code to decide what production code should produce) would
// need a FINAL-only substring written out here by hand. There is no such
// substring in Go to transcribe: winddownNoticeText (api_bootdocs.go:572-604)
// picks a DOCUMENT — s.offboardSpec() for soft, s.acceleratedStopSpec() for
// final — and the words come out of the seeded 〈停止〉 / 〈加速停止〉 documents,
// which the owner can edit at runtime (api_bootdocs.go's whole point, T-3201:
// 「the owner went looking for the words an agent is sent and could not find
// them」). A literal copied out of a seed file is not a literal transcribed
// from an assignment: the owner rewording one sentence of 〈加速停止〉 would
// redden this matrix for a reason that is not a behaviour change on either
// handler. So the column asks the question that IS decided in Go — does a
// notice ride this frame at all (offboardKindOf's `carries`, api_members.go:227)
// — and leaves the wording to the tests that own it
// (offboard_selfdriven_ta9d6_test.go, offboard_discriminator_t0974_test.go).
type noticeSet string

const (
	// noticedNothing — the verb fanned NO member-topic frame for this subject.
	// This is the whole outsource side of the file today, and 🔴 THE REASON IS
	// NOT THE ONE THIS COMMENT USED TO GIVE. It said openWorkerHandoverGrace was
	// "the worker's ONLY member-topic publisher"; MEASURED (T-65 包⑤) that is
	// FALSE. openWorkerHandoverGrace holds the only DIRECT hub.Publish("member")
	// in the worker package (worker_spawn.go:2258), but SIX further worker-id
	// publishes ride s.putMember(memberFromWorker(w)), which fans
	// publishMemberPatch UNCONDITIONALLY (api_members.go): the two collect
	// funnels (:2295, :2336) and the three self-report folds (:2403, :2432,
	// :2495/:2525). Denominator: hub.Publish("member" = 4 in production out of 40
	// hub.Publish( calls; a bogus topic matches 0.
	//
	// What actually keeps these cells silent is NARROWER and holds on its own:
	// the owner-verb handlers under test are in api_outsource.go, which calls
	// putMember NOWHERE (0 call sites — positive control: worker_spawn.go has 6),
	// and PutOutsourceWorker deliberately fans no member patch (dal.go:512).
	// ⚠️ THE DIFFERENCE MATTERS FOR THE NEXT EDITOR, not for today's colour: the
	// old sentence said the silence was a property of the WORKER, so a handler
	// that gained a putMember would read as still-silent. The measured one says
	// it is a property of THIS HANDLER FAMILY, which is a thing an edit can break.
	noticedNothing noticeSet = ""
	// noticedNotice — at least one member-topic frame carried offboard_notice:
	// the subject was shown the wind-down sequence.
	noticedNotice noticeSet = "notice"
)

// watchMemberDeltas opens the COCKPIT connection the `noticed` column is read
// from, and it is called AFTER the seeding and BEFORE the handler on purpose:
// the buffer then holds exactly the frames the verb under test fanned, with no
// drain step that could swallow one of them (the same ordering
// api_bootdocs_offboard_tc9c0_test.go:51 uses, and for the same reason).
//
// 🔴 WHY THE OWNER LISTENER RATHER THAN THE SUBJECT'S OWN. Two candidates were
// on the table and one of them perturbs the fixture:
//
//   - the subject's own listener. seedParityMember's connectOnlineMachine
//     already makes one and throws it away, so the staff side could just catch
//     it — but seedParityWorker cannot: newActiveWorker discards the listener
//     inside itself (worker_lifecycle_test.go:108) and getting one back means
//     either changing a helper a dozen other tests share, or a second
//     hub.Connect on the same id, which is a TAKEOVER: Connect (hub.go:186)
//     kicks the incumbent (deletes it from the map and closes its `kicked`
//     channel) and spends one of the member's takeoverBurst stamps. It would
//     NOT move IsOnline (the new listener is inserted in the same critical
//     section the old one leaves, so the member is online throughout) and it
//     would not move MachineOf as long as the takeover passed the same
//     machineID — the worker's is "" — but 「would not move it as long as you
//     remember to pass the right string」 is exactly the silent fixture coupling
//     TestVerbPopulationParityFixtureLandsBothPopulationsOnOneWarden exists to
//     complain about.
//
//   - the OWNER/cockpit connection (MemberID == ""), which is what this is. It
//     is 全量 by contract (hub.go:503-508: an empty MemberID takes every frame),
//     so ONE connection sees both populations and the two arms of a row are read
//     through the same seam. And it is INERT with respect to every projection
//     this matrix asserts: Connect's takeover block is skipped outright for an
//     empty memberID (hub.go:192), IsOnline / OnlineMembers / MachineOf all skip
//     blank-id listeners, and Disconnect reports false for them, so no
//     last-disconnect edge fires. It cannot move a single one of the other ten
//     columns.
func watchMemberDeltas(t *testing.T, api *apiServer) *hubListener {
	t.Helper()
	l, err := api.hub.Connect("", "")
	if err != nil {
		t.Fatalf("open the cockpit connection the noticed column reads: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(l) })
	return l
}

// noticedFor drains the cockpit connection and folds what this verb said TO THIS
// SUBJECT. Frames addressed at somebody else are dropped rather than counted,
// for the same reason dispatchedFor drops them: the fixture holds a warden and a
// neighbour, and a matrix that counted their deltas would go red for a reason
// that has nothing to do with the verb under test.
//
// 🔴 IT IS AN OBSERVATION, NOT A RE-DERIVATION. This file's rule 1 forbids
// calling the helpers underneath the handler seam, and there is a ready-made
// temptation here: `api.offboardDeltaPayload(m)["offboard_notice"]` is how
// worker_forced_stop_parity_tc996_test.go:75/113/143 asks this question. That is
// legitimate THERE and forbidden HERE — it asks 「what WOULD this row be told if
// somebody fanned a delta now」, which is a different question from 「what WAS
// this subject told」, and the two come apart at exactly the interesting place:
// a verb that stamps a notice-bearing row and never publishes reads as a full
// 預告 through the helper and as noticedNothing through the wire. The mutant in
// this column's commit message is precisely that shape.
//
// DRAINING IS DESTRUCTIVE. Called EXACTLY ONCE per verb run, from inside
// memberTerminal / workerTerminal, for the same reason dispatchedFor is.
func noticedFor(t *testing.T, l *hubListener, subjectID string) noticeSet {
	t.Helper()
	out := noticedNothing
	key := wireOwnerID + "::" + subjectID
	for raw := l.pop(); raw != nil; raw = l.pop() {
		_, envelope := parseSSEFrame(t, raw)
		if envelope["topic"] != "member" {
			continue
		}
		data, _ := envelope["data"].(map[string]any)
		if data == nil || data["key"] != key {
			continue
		}
		payload, _ := data["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		if _, carries := payload["offboard_notice"]; carries {
			return noticedNotice
		}
	}
	return out
}

// parityFields is the compared field set, in a stable order so a failure names
// the same field every run.
var parityFields = []string{
	"http_status", "desired_state", "stopping_since", "stopped_since",
	"refocus_since", "refocus_op", "waking_since", "restart_after_stop",
	"desired_machine_id",
	// T-65 包③ — the first column that is NOT a database column. Everything
	// above is read back off the row; this one is observed from the warden
	// FIFO. Kept LAST so an existing failure still names the same field.
	"dispatched",
	// T-65 包③ — the SECOND non-database column, appended for the same reason
	// `dispatched` was: a field added in the middle would renumber nothing but
	// would change which name an existing failure prints first.
	"banked_cost",
	// T-65 包③ — the THIRD non-database column, appended last for the third
	// time and for the same reason. ⚠️ THE ORDER OF THIS LIST IS NOT THE ORDER
	// OF terminalState's fields and does not have to be: what it fixes is which
	// field name a failure prints FIRST, so that a pre-existing red cell keeps
	// reading the way it read yesterday when a column is added. The nine
	// database columns come first, then the three observed side effects in the
	// order they were added — dispatched (the FIFO), banked_cost (the money),
	// noticed (the sentence).
	"noticed",
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
	case "dispatched":
		return s.Dispatched
	case "banked_cost":
		return s.Cost
	case "noticed":
		return s.Noticed
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
	{
		verb: "重新聚焦", field: "noticed",
		why: "包② CONVERGED THE ROW AND THE SENTENCE DID NOT COME WITH IT. Both faces " +
			"now answer 200 through the same queue-the-起來 branch and write the same " +
			"restart_after_stop; what differs is that the 正職 branch writes the row TWICE " +
			"through publishers that fan (putMember at api_members.go:1613 and " +
			"persistMemberOpReceipt at :1619, both reaching publishMemberPatch at :129), " +
			"and the row is desired-offline with stopping_since in the past and no forced " +
			"epoch — so gracefulStopEpochOpen is true and EVERY one of those deltas carries " +
			"the soft 預告 of the stop ALREADY in flight. The 外包 branch returns at " +
			"api_outsource.go:455 after persistWorkerRestartIntent + publishOutsourceWorker, " +
			"and publishOutsourceWorker is owner-audience with a {id, codename, status} " +
			"payload — it carries offboard_notice never (the handler's own 🔴 block at " +
			":632-635 says exactly this), so the :529 openWorkerHandoverGrace on the OTHER " +
			"branch is the only thing that could have spoken and it is not on this path. " +
			"⚠️ WHAT THIS ROW IS AND IS NOT: it is NOT 「the worker should be told too」. " +
			"Neither side OPENED anything here — both queued a 起來 behind a stop that was " +
			"already running — so the honest reading is that 正職 RE-announces a sentence " +
			"the agent has already had, on a press that changed nothing it describes, and " +
			"the client de-duplicates by keying on the text (api_members.go:160-164). The " +
			"defensible convergence is therefore in EITHER direction and nobody has ruled " +
			"which; this row exists so that the next person to look does not have to " +
			"rediscover that 包② left a difference behind.",
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
	seedParityLiveCost(t, api, id)
}

// seedParityLiveCost plants the live telemetry cost the banked_cost column is
// measured against, on BOTH populations, through the same one writer.
//
// 🔴 READ-MODIFY-WRITE, not a bare Set — and read WHY carefully, because the
// reason is a FORECAST, not a fact on the ground. memStore.Set REPLACES the
// whole entry (hub.go:1111-1115). Today NO fixture in this file puts any other
// key on either subject's telemetry: measured — both populations' entries are
// nil at the moment this helper runs, so the `entry == nil` arm is currently
// always taken and the Get result is never used. Replacing the whole body with
// a bare Set is green today (measured too).
//
// It is written this way so that the day a fixture DOES seed telemetry, this
// helper does not silently overwrite it — that would move what the gauge-driven
// passes see, i.e. move the OTHER nine columns from inside a helper that is
// only supposed to be about money. Do not "simplify" it back to a bare Set on
// the strength of the green: the green is what this shape is buying.
func seedParityLiveCost(t *testing.T, api *apiServer, id string) {
	t.Helper()
	entry := api.telemetry.Get(id)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["cost"] = parityLiveCost
	api.telemetry.Set(id, entry)
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
	seedParityLiveCost(t, api, id)
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

// dispatchedFor drains the ONE warden both populations are placed on and reports
// what this verb queued FOR THIS SUBJECT. Two things make one drain legitimate
// for both sides, and both are asserted rather than assumed:
//
//   - the placement — TestVerbPopulationParityFixtureLandsBothPopulationsOnOneWarden
//     pins that the two kill-target resolvers answer the same warden here, so a
//     single FIFO sees both.
//   - the subject key — a worker's stop frame is built by the SAME
//     buildTargetFrame the member side uses (worker_spawn.go:1307), and its
//     wardenTargetArgs carries exactly one field, `member_id`
//     (reconcile.go:1094-1098: "the warden keys the kill/removal on member_id
//     alone"). So ONE predicate covers both populations; there is no separate
//     worker_id key to miss.
//
// Frames addressed at somebody else are dropped rather than counted: the fixture
// seeds a second member and a worker on one machine, and a matrix that counted
// the neighbour's kill would go red for a reason that has nothing to do with the
// verb under test.
//
// DRAINING IS DESTRUCTIVE — the FIFO is emptied. Call this EXACTLY ONCE per
// verb run, which is why it lives inside memberTerminal / workerTerminal (one
// call each, at the end) rather than being available to case bodies.
func dispatchedFor(t *testing.T, api *apiServer, subjectID string) dispatchSet {
	t.Helper()
	var rpcs []string
	for _, f := range drainFrames(t, api, parityMachineA) {
		if id, _ := f.Args["member_id"].(string); id == subjectID {
			rpcs = append(rpcs, f.RPC)
		}
	}
	sort.Strings(rpcs)
	return dispatchSet(strings.Join(rpcs, "+"))
}

// memberTerminal / workerTerminal read the row back and bucket its anchors
// against the instant the READ happens — deliberately AFTER the handler has
// returned. Sampling `now` before the call instead makes every anchor the
// handler stamps land in the FUTURE bucket, which is a harness artefact that
// looks exactly like a behaviour change.
func memberTerminal(t *testing.T, api *apiServer, id string, code int, notices *hubListener) terminalState {
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
		Dispatched:       dispatchedFor(t, api, id),
		Cost:             classifyCost(m.BankedCost, liveCostPresent(api, id)),
		Noticed:          noticedFor(t, notices, id),
	}
}

func workerTerminal(t *testing.T, api *apiServer, id string, code int, notices *hubListener) terminalState {
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
		Dispatched:       dispatchedFor(t, api, id),
		// memberFromWorker folds banked_cost the same way it folds the anchors —
		// migration 00025 made it the SAME column — so the two populations'
		// money is literally comparable, exactly as the nine row fields are.
		Cost: classifyCost(m.BankedCost, liveCostPresent(api, id)),
		// The subject key is the WORKER id, not a member id derived from it:
		// worker_spawn.go:2259 addresses the frame at wireOwnerID+"::"+w.ID, the
		// same shape publishMemberPatch uses for a staff row, so one predicate
		// covers both populations here exactly as it does in dispatchedFor.
		Noticed: noticedFor(t, notices, id),
	}
}

// seedParityMemberOffline / seedParityWorkerOffline are the online seeds with
// the SESSION removed and nothing else changed — same row, same machine, same
// live cost. 包③ needs them because every case above starts ONLINE, and 停止 is
// the verb whose two implementations part company precisely on liveness: the
// staff handler hands an offline subject to the reconcile tick, the worker
// handler collects it on the spot.
//
// 🔴 THE SEED ITSELF NEEDS A POSITIVE CONTROL, and it has one:
// TestParityOfflineSeedIsActuallyOffline below. Without it "the handler did
// nothing because the subject was offline" and "I failed to make the subject
// offline" are the same green.
func seedParityMemberOffline(t *testing.T, api *apiServer, id string, mutate func(*Member)) {
	t.Helper()
	m := testAgent(id)
	m.DesiredMachineID = parityMachineA
	m.Model = "claude-sonnet-4-5"
	if mutate != nil {
		mutate(&m)
	}
	putTestMember(t, api, m)
	// …and NO connectOnlineMachine. That one missing line is the whole seed.
	seedParityLiveCost(t, api, id)
}

func seedParityWorkerOffline(t *testing.T, api *apiServer, mutate func(*OutsourceWorker)) string {
	t.Helper()
	// newActiveWorker's `online` parameter is the worker twin of the missing
	// connect above — it is the helper newActiveOnlineWorker wraps, not a
	// separate path minted here.
	id := newActiveWorker(t, api, false)
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("seed offline worker: %v", err)
	}
	if mutate != nil {
		mutate(w)
	}
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put offline worker: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
	seedParityLiveCost(t, api, id)
	return id
}

// parityStopOffSeed is what the 停止（離線起點）row seeds ON TOP of "no session",
// and every line of it is there to buy discrimination the row did not have.
//
// 🔴 WHY IT IS NOT `nil` (independent review, caught after the row shipped green).
// With a nil mutate the three columns 停止 CLEARS start at their ZERO values —
// refocus_since 0, refocus_op "", restart_after_stop false. A row seeded that way
// can see 「the verb wrote the WRONG value」 but is blind to 「the verb failed to
// CLEAR」, because not-clearing and correctly-clearing produce the same literal.
// Measured both directions: mutating a shared write to a non-zero value reddened
// all four cells, while mis-wiring an adapter so a clear never happened reddened
// only the ONLINE 停止 row — this one stayed green. Seeding them non-zero is the
// one-line fix, and the expected values do not move: the verb clears them, so the
// terminal row is the same one it was. What changes is that it is now the same
// for a REASON the test can tell apart from an accident.
//
// restart_after_stop=true is the sharpest of the three: leave the clear out and the
// tick this handler kicks would consume the queued 起來 and bring an offline member
// straight back up — the exact negative control the owner's 後蓋前 ruling turns on.
func parityStopOffSeed(m *Member) {
	m.RefocusSince = parityPast
	m.RefocusOp = refocusOpRefocus
	m.RestartAfterStop = true
}

// TestParityOfflineSeedIsActuallyOffline is the positive/negative control for
// the two seeds above, on ONE server in ONE run: the offline seeds must read
// offline and the online seeds must read online through the SAME predicate the
// handlers consult. It asserts nothing about 停止 — its whole job is to stop a
// broken seed from being read as converged behaviour.
func TestParityOfflineSeedIsActuallyOffline(t *testing.T) {
	api := newParityServer(t)
	seedParityMemberOffline(t, api, "m-seedctl-off", nil)
	seedParityMember(t, api, "m-seedctl-on", nil)
	offWorker := seedParityWorkerOffline(t, api, nil)
	onWorker := seedParityWorker(t, api, nil)

	for _, c := range []struct {
		id   string
		want bool
	}{
		{"m-seedctl-off", false},
		{"m-seedctl-on", true},
		{offWorker, false},
		{onWorker, true},
	} {
		if got := api.hub.IsOnline(c.id); got != c.want {
			t.Fatalf("hub.IsOnline(%s) = %v, want %v", c.id, got, c.want)
		}
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
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-up", "activate", nil,
					api.HandleActivateMemberApiMembersMemberIdActivatePost)
				return memberTerminal(t, api, "m-parity-up", code, notices)
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
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "restart", nil,
					api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
				return workerTerminal(t, api, id, code.Code, notices)
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
				// A live activate only clears the stop/wake anchors and consumes the
				// queued restart intent. It preserves the active wind-down epoch,
				// skips reconcile, and therefore neither kills nor banks the session.
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// The persisted row refresh is owner-only, so preserving the epoch does
				// not re-announce a lifecycle notice to the live member.
				Noticed: noticedNothing,
			},
			// 外包 restart, LIVE ARM (T-65 包④ — this seed holds a live session, so
			// `sessionAliveReceipt` is true): worker.DesiredState = DesiredStateOnline;
			// worker.StoppingSince = 0.0; worker.WakingSince = 0.0;
			// clearWorkerRestartIntent. refocus_since / refocus_op / stopped_since are
			// INSIDE `if !sessionAliveReceipt` and are therefore NOT written here.
			//
			// 🔴 FIVE CELLS CONVERGED ON THIS ROW AND THEIR WHITELIST ENTRIES ARE GONE
			// (waking_since, stopped_since, refocus_since, refocus_op, banked_cost).
			// Not five independent fixes — ONE: 喚醒 on a running worker now leaves the
			// session alone, so there is no displacement, so there is no replaced epoch
			// to reset and no dying session's cost to bank. waking_since converged
			// separately, by the clear 包④ added on BOTH arms.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorPast,
				Refocus: anchorPast, RefocusOp: refocusOpRefocus,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// NOTHING is dispatched: `outcome = ownerOpOutcome{AlreadyRunning: true}`
				// and respawnWorkerForOwnerOp is only called under `if
				// !sessionAliveReceipt` (api_outsource.go). The kill lives behind that
				// call, so not making it IS the behaviour change.
				// The staff and worker live arms both leave the running session alone.
				Dispatched: dispatchedNothing,
				// No session ends, so respawnWorkerNow's bank-before-kill is never
				// reached and the live figure is still on the row.
				Cost: costUntouched,
				// NOTHING is said: openWorkerHandoverGrace is not called on either arm
				// of this handler, api_outsource.go calls putMember nowhere at all
				// (0 call sites, measured T-65 包⑤), and PutOutsourceWorker
				// deliberately fans no member patch (dal.go:512). ⚠️ NOT because
				// openWorkerHandoverGrace is "the worker's ONLY member-topic
				// publisher" — that claim is false, see noticedNothing above.
				Noticed: noticedNothing,
			},
		},
		{
			verb: "起來（離線起點）",
			note: "the SAME two handlers as 起來 above, on a subject with NO SESSION. " +
				"包④ split 外包 喚醒 on exactly this bit — 「正在跑就不動它」 — so the verb " +
				"now has two behaviours and the matrix needs two rows to see both. This " +
				"is the arm that still REPLACES a session, and therefore the arm the " +
				"clean sheet (T-ed79 #11) is a claim about. 🔴 A SEPARATE verb label on " +
				"purpose: the whitelist is keyed (verb, field), so folding this into 起來 " +
				"would make its three rows read as excuses for the LIVE row too — and " +
				"those three cells converged there in 包④ and must keep being converged.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMemberOffline(t, api, "m-parity-up-off", func(m *Member) {
					m.DesiredState = DesiredStateOffline
					m.StoppingSince = parityPast
					m.StoppedSince = parityPast
					m.RefocusSince = parityPast
					m.RefocusOp = refocusOpRefocus
					m.WakingSince = parityPast
				})
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-up-off", "activate", nil,
					api.HandleActivateMemberApiMembersMemberIdActivatePost)
				return memberTerminal(t, api, "m-parity-up-off", code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorkerOffline(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
					w.StoppedSince = parityPast
					w.RefocusSince = parityPast
					w.RefocusOp = refocusOpRefocus
					w.WakingSince = parityPast
				})
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "restart", nil,
					api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
				return workerTerminal(t, api, id, code.Code, notices)
			},
			// 正職 activate on an offline generation clears the complete old
			// wind-down row, banks the old live cost, and uses stop-before-start
			// before reconcile dispatches the replacement.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				// 🔴 PAST, NOT ZERO, AND IT IS THE START THAT PUTS IT BACK. activate's
				// own `m.WakingSince = 0.0` really does run — the anchor read here is a
				// FRESH one, stamped by the spawn notify on the START below. On the
				// live 起來 row no START is dispatched, so the clear is all there is
				// and that cell reads zero. Same handler, same assignment, two
				// readings: the difference is the frame, not the write.
				Waking: anchorPast, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// decideUp on a member that is NOT online: no recycle arm to walk into
				// (that arm is what produces the lone `stop` on the live 起來 row), so
				// the reconcile does the plain thing and starts it.
				Dispatched: "start+stop",
				Cost:       costBanked,
				// The old row is cleared before the persisted update, so no lifecycle
				// notice is emitted for the replaced generation.
				Noticed: noticedNothing,
			},
			// 外包 restart, NOT-RUNNING ARM: worker.DesiredState = DesiredStateOnline;
			// worker.StoppingSince = 0.0; worker.WakingSince = 0.0; and then, inside
			// `if !sessionAliveReceipt`, worker.RefocusSince = 0.0; worker.RefocusOp =
			// ""; worker.StoppedSince = 0.0 (api_outsource.go). Unchanged from before
			// 包④ except for the waking_since clear.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				// Same story as the 正職 cell: the handler's own clear runs, and
				// notifyWorkerSpawn then stamps a fresh anchor on the re-dispatch. That
				// re-stamp is the exact argument 包④ retired as a reason NOT to clear —
				// it only ever held on the arm that HAS a re-dispatch, which is this one.
				Waking: anchorPast, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// respawnWorkerForOwnerOpNow → respawnWorkerNow resolves the remembered
				// placement and kills it BEFORE dispatching the fresh START. SORTED, so
				// "start+stop" — the emission order is stop-then-start.
				Dispatched: "start+stop",
				// respawnWorkerNow banks the dying session's live cost before the kill
				// (「so the respawn never zeroes the visible spend」); the start+stop
				// above is the same call's evidence.
				Cost: costBanked,
				// openWorkerHandoverGrace is not called by this handler on either arm,
				// api_outsource.go calls putMember nowhere at all (0 call sites,
				// measured T-65 包⑤), and PutOutsourceWorker deliberately fans no
				// member patch (dal.go:512). ⚠️ NOT because openWorkerHandoverGrace is
				// "the worker's ONLY member-topic publisher" — see noticedNothing.
				Noticed: noticedNothing,
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
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-stop", "deactivate", nil,
					api.HandleDeactivateMemberApiMembersMemberIdDeactivatePost)
				return memberTerminal(t, api, "m-parity-stop", code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.RefocusSince = parityPast
					w.RefocusOp = refocusOpRefocus
				})
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "stop", nil,
					api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
				return workerTerminal(t, api, id, code.Code, notices)
			},
			// 正職: desired offline + clearMemberHandoverMarker + clearRestartIntent
			// + StoppingSince = stopEpochAnchor(...) → now (no forced epoch live).
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// the member is SSE-online ⇒ deriveLiveness returns online, never waking
				// (domain.go:194 tests Online BEFORE WakePending at :197) ⇒ cancellingWake
				// is false ⇒ no dispatchRobustStopNow (api_members.go:1421). The tick then
				// parks in decideDown's soft-offboard arm (reconcile.go:845).
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// desired offline + stopping_since = stopEpochAnchor -> now, and no forced
				// epoch, so gracefulStopEpochOpen (api_members.go:412) is true and
				// offboardKindOf's offline arm hands back soft. The 預告 rides the
				// putMember at :1416.
				Noticed: noticedNotice,
			},
			// 外包: desired offline; RefocusSince = 0.0; RefocusOp = "";
			// StoppingSince = stopEpochAnchor(memberFromWorker(...)) → the same now.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// openWorkerHandoverGrace's "if offline, kill now" arm is unreachable on a live
				// worker (worker_spawn.go:2245-2258); the online path publishes a 預告 and
				// dispatches nothing.
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// the same sentence by a different road: the online arm of
				// openWorkerHandoverGrace (api_outsource.go:736) publishes the member-topic
				// 預告 itself (worker_spawn.go:2259). The worker's row is the same
				// graceful shape — stopEpochAnchor, no forced epoch — so the same soft
				// notice is composed off it.
				Noticed: noticedNotice,
			},
		},
		{
			verb: "停止（離線起點）",
			note: "the SAME two handlers as 停止 above, on a subject with NO SESSION. " +
				"包③ converged what the two write to the ROW (applyStopVerbRow); this row " +
				"exists because that is NOT all a stop does, and liveness is exactly where " +
				"the two halves that were NOT converged part company. 🔴 A SEPARATE verb " +
				"label on purpose: the whitelist is keyed (verb, field), so folding this " +
				"into 停止 would make its three rows read as excuses for the ONLINE row " +
				"too — which has no divergences at all and must keep having none.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMemberOffline(t, api, "m-parity-stop-off", parityStopOffSeed)
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-stop-off", "deactivate", nil,
					api.HandleDeactivateMemberApiMembersMemberIdDeactivatePost)
				return memberTerminal(t, api, "m-parity-stop-off", code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorkerOffline(t, api, func(w *OutsourceWorker) {
					w.RefocusSince = parityPast
					w.RefocusOp = refocusOpRefocus
					w.RestartAfterStop = true
				})
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "stop", nil,
					api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
				return workerTerminal(t, api, id, code.Code, notices)
			},
			// 正職: the five row writes are applyStopVerbRow's, same as the online
			// row. An offline member now takes the same collect funnel as the worker.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorPast,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				Dispatched:       dispatchSet(reconcileCmdStop),
				Cost:             costBanked,
				Noticed:          noticedNotice,
			},
			// 外包: the same five row writes, and then openWorkerHandoverGrace takes
			// its `!hub.IsOnline` arm. desired_state is already offline (this verb
			// just wrote it), so that arm routes to collectWorkerStop, which is
			// THREE more things in one call: it latches stopped_since, it kills the
			// session (one `stop` frame), and its putMember banks the live cost.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorPast,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				Dispatched:       dispatchSet(reconcileCmdStop),
				Cost:             costBanked,
				// 🔴 SAME VALUE, DIFFERENT ROAD — and the road matters for whoever
				// converges this later. On the staff side the notice rides the
				// handler's own putMember. Here openWorkerHandoverGrace's offline arm
				// publishes NOTHING (it returns before the hub.Publish); the delta is
				// fanned by collectWorkerStop's putMember, AFTER stopped_since is
				// latched. Deleting that collect would take the notice with it.
				Noticed: noticedNotice,
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
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-accel", "accelerated-stop", nil,
					api.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost)
				return memberTerminal(t, api, "m-parity-accel", code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
				})
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "accelerated-stop", nil,
					api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost)
				return workerTerminal(t, api, id, code.Code, notices)
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
				// the handler 409s an offline member (api_members.go:1540) and its own comment
				// at :1580-1583 says the reconcile it fires "dispatches nothing on this pass —
				// the deadline is in the future by construction" (reconcile.go:836-841).
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// the escalation's whole point is that the sentence quotes the clock the
				// owner just started: refocus_op = 加速停止 makes winddownKindFor answer
				// final+clocked, so the putMember at :1573 carries a notice.
				Noticed: noticedNotice,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: refocusOpAcceleratedStop,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// same shape: 409 unless active+online (api_outsource.go:592), then the same
				// online arm of openWorkerHandoverGrace.
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// api_outsource.go:643, and its own 🔴 block says why the call is there:
				// publishOutsourceWorker is owner-only and carries offboard_notice never,
				// so without this one call the press would start the collect clock while
				// the last thing the worker heard was the 停止 sentence.
				Noticed: noticedNotice,
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
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-force", "force-stop", nil,
					api.HandleForceStopMemberApiMembersMemberIdForceStopPost)
				return memberTerminal(t, api, "m-parity-force", code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.StoppingSince = parityFuture
				})
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "force-stop", nil,
					api.HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost)
				return workerTerminal(t, api, id, code.Code, notices)
			},
			// 正職 now clamps a future stamp to the force-stop instant.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// the ONLY unconditional dispatch on either side: dispatchRobustStopNow
				// (api_members.go:1487 → reconcile.go:2919-2930) with no state test in front
				// of it. This handler never calls reconcileMemberNow at all.
				Dispatched: "stop",
				// The staff handler banks before dispatching the kill, matching
				// stopWorkerNow's ordering on the worker side.
				Cost:    costBanked,
				Noticed: noticedNothing,
			},
			// 外包: `if worker.StoppingSince <= 0.0 || worker.StoppingSince > forcedAt
			// { worker.StoppingSince = forcedAt }` — the second arm pulls it back.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// stopWorkerNow → resolveWorkerKillTarget (worker_spawn.go:1979-1992). Same RPC
				// and same count as 正職, but the FAIL MODE differs: an unresolvable target
				// here skips the enqueue and only logs (:1988-1991), where 正職 enqueues
				// anyway and arms a retry. The fixture resolves, so the cells agree.
				Dispatched: "stop",
				// stopWorkerNow banks BEFORE the kill (worker_spawn.go:1983).
				Cost: costBanked,
				// silent, and by TWO independent guards rather than one. The worker
				// force-stop reaches no member-topic publisher at all (grep, T-65 包⑤: the
				// five openWorkerHandoverGrace call sites are api_outsource.go:483/:597/:691
				// and worker_spawn.go:1845/:2573, none of them on this path, AND
				// api_outsource.go calls putMember nowhere — the second half is load-bearing
				// because the grace publisher is not the worker's only one), AND the
				// stopping_since pull-back keeps forcedEpochLive true, so even a frame
				// would carry nothing.
				Noticed: noticedNothing,
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
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-refocus", "refocus", nil,
					api.HandleRefocusMemberApiMembersMemberIdRefocusPost)
				return memberTerminal(t, api, "m-parity-refocus", code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
				})
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "refocus", nil,
					api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
				return workerTerminal(t, api, id, code.Code, notices)
			},
			// 正職: !aRefocusStampWouldReachTheAgent && aStopWasEverAskedFor →
			// stampRestartIntent(m) and a 200. The stop keeps its stage and anchors.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: true,
				DesiredMachineID: parityMachineA,
				// takes the queue-the-起來 branch (api_members.go:1608); the member is still
				// online so the tick reaches decideDown's soft arm and spends nothing.
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// the queue-the-起來 branch still writes the row twice — putMember at
				// api_members.go:1613 and persistMemberOpReceipt at :1619, both of which
				// fan through publishMemberPatch. The row is desired-offline with
				// stopping_since in the past and no forced epoch, so each delta carries
				// the soft 預告 of the stop ALREADY in flight. 重新聚焦 opened no epoch
				// here (that is the 包② convergence) — it re-announced the old one.
				Noticed: noticedNotice,
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
				// queues the 起來 (api_outsource.go:455) and its follow-up outsourceTickNow is
				// gated off wholesale by noOutsource (outsource_sched.go:744-748).
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// the worker face answers the same 200 through the same branch and says
				// NOTHING: api_outsource.go:455 returns after queueWorkerRestartAfterStop
				// and persistWorkerRestartIntent, so the :529 openWorkerHandoverGrace on
				// the other branch is never reached, and publishOutsourceWorker is
				// owner-only. 包② converged the ROW here; the sentence did not come with it.
				Noticed: noticedNothing,
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
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-move", "relocate",
					map[string]any{"machine_id": parityMachineB},
					api.HandleRelocateMemberApiMembersMemberIdRelocatePost)
				return memberTerminal(t, api, "m-parity-move", code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, nil)
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "relocate",
					map[string]any{"machine_id": parityMachineB},
					api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
				return workerTerminal(t, api, id, code.Code, notices)
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
				// armMemberOwnerOpHandover stamps an UNCLOCKED epoch, so decideUp parks in
				// "awaiting agent dump" (reconcile.go:562-602) and the relocation-STOP arm
				// below it is never reached.
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// armMemberOwnerOpHandover stamped refocus_since with op=relocate and the
				// member stays desired-online, so offboardKindOf's online arm carries a
				// soft notice on the putMember at :1235 — 「a handover is being opened,
				// work the sequence」, which is exactly what an unclocked epoch means.
				Noticed: noticedNotice,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: ownerOpRelocate,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineB,
				// relocate does not displace, and the worker has state to flush, so it takes
				// openOwnerOpHandover (worker_spawn.go:1595-1603) — 預告 only.
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// the wind-down arm ends in openOwnerOpHandover -> openWorkerHandoverGrace
				// (worker_spawn.go:1846), whose online arm publishes the member-topic 預告.
				// Same op (relocate), same soft answer out of winddownKindFor.
				Noticed: noticedNotice,
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
				notices := watchMemberDeltas(t, api)
				rec := httptest.NewRecorder()
				api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
					taskReq(t, "PATCH", "/api/members/m-parity-model",
						map[string]any{"model": "claude-opus-4-8"}, wireOwnerID, "owner"),
					"m-parity-model")
				return memberTerminal(t, api, "m-parity-model", rec.Code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, nil)
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "model",
					map[string]any{"model": "claude-opus-4-8"},
					api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
				return workerTerminal(t, api, id, code.Code, notices)
			},
			// Both gate on 「a launch intent actually changed」 and then open the same
			// wind-down with op = "runtime/model" (memberOpModel == ownerOpModel).
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: memberOpModel,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// 🔴 STRUCTURAL, not fixture-dependent: this handler body (api_members.go:820-1026)
				// contains NO enqueue, no dispatchRobustStopNow and no reconcileMemberNow.
				// NO fixture can make this cell non-empty.
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// ⚠️ NOT structural the way `dispatched` is on this cell. That literal is
				// empty because the handler body contains no enqueue at all; this one is
				// non-empty because the SAME body ends in putMember (api_members.go:940),
				// and putMember's fan is offboardDeltaPayload — so the refocus epoch
				// armMemberOwnerOpHandover just stamped (op=model, desired online) puts a
				// soft notice on the wire without this handler mentioning notices at all.
				Noticed: noticedNotice,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: ownerOpModel,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// reaches respawnWorkerForOwnerOp (api_outsource.go:1066) but comes out on the
				// wind-down arm. ⚠️ ASYMMETRY THIS CELL HIDES: unlike 正職 above, this face
				// DOES own a dispatch site — with an already-collected epoch it would send
				// STOP+START (worker_spawn.go:1604). Equal here, not equal by construction.
				Dispatched: dispatchedNothing,
				Cost:       costUntouched,
				// same road as 改機器 above: respawnWorkerForOwnerOp comes out on the
				// wind-down arm and openWorkerHandoverGrace publishes the 預告.
				Noticed: noticedNotice,
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

// TestReportWakingKeepsAStaffStopAnchorButClearsTheWorkerOne is the SECOND
// divergence the matrix above cannot express, recorded the same one-sided way
// TestAcceleratedStopWorkerHasAnExtraLifecycleGate is.
//
// 🔴 WHY IT IS A TEST RATHER THAN A knownDivergences ROW, which is where T-65
// 包⑤ was asked to put it: the whitelist is keyed (verb, field) and the orphan
// check above rejects any row whose verb no case in parityCases runs.
// report_waking is an AGENT SELF-REPORT, not one of the nine owner verbs this
// matrix drives, so a row for it would be documentation the orphan check would
// (correctly) redden. This is the file's own escape hatch for exactly that.
//
// THE DIVERGENCE. Both faces clear refocus_since / refocus_op / stopped_since
// unconditionally. On stopping_since they part:
//
//	正職 HandleReportWakingApiSelfWakingPost — clears it ONLY under
//	  `DesiredState == DesiredStateOnline`. T-7526 put that guard there: clearing
//	  unconditionally erased the only mark a mid-wake 取消 left behind, so an
//	  agent that was already booting when the cancel landed came up painting a
//	  fresh green over an intent that is still offline.
//	外包 workerReportWaking — clears it UNCONDITIONALLY. No guard.
//
// ⚠️ WHAT THIS TEST IS AND IS NOT. It PINS today's behaviour on both sides; it
// does NOT claim the worker side is correct. The reasoning that produced the
// staff guard applies to a worker word for word, so the likely reading is that
// 外包 is the defective side — but 「likely」 is not a ruling, and T-65 包⑤ was
// scoped to ZERO behaviour change, so fixing it there would have been the one
// thing the package promised not to do.
//
// ⚠️ REACHABILITY IS NOT MEASURED. Whether a real deployment gets a worker
// report_waking while desired_state is offline was not established — the seed
// below constructs the state directly. So this is a divergence in the CODE with
// its production reach unknown, which is exactly what a whitelist row would
// have had to say too.
func TestReportWakingKeepsAStaffStopAnchorButClearsTheWorkerOne(t *testing.T) {
	t.Run("正職: the anchor of a cancelled wake SURVIVES", func(t *testing.T) {
		api := newParityServer(t)
		seedParityMemberOffline(t, api, "m-waking-off", func(m *Member) {
			// 🔴 DESIRED-offline, not just session-offline. seedParityMemberOffline's
			// name means "no SSE connection" and testAgent is desired ONLINE, so
			// without this line the guard's own condition is satisfied and the anchor
			// is cleared — measured, and it is the mid-wake 取消 state that is the
			// whole subject: the owner said DOWN while this session was still booting.
			m.DesiredState = DesiredStateOffline
			m.StoppingSince = parityPast
		})
		rec := httptest.NewRecorder()
		api.HandleReportWakingApiSelfWakingPost(rec,
			taskReq(t, "POST", "/api/self/waking", map[string]any{}, "m-waking-off", "agent"))
		if rec.Code != http.StatusOK {
			t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
		}
		m, err := api.dal.GetMember("m-waking-off")
		if err != nil || m == nil {
			t.Fatalf("read back: %v", err)
		}
		if m.StoppingSince != parityPast {
			t.Fatalf("staff stopping_since = %v, want it UNTOUCHED at %v — the "+
				"`if m.DesiredState == DesiredStateOnline` guard (T-7526) is what "+
				"keeps a mid-wake 取消 visible", m.StoppingSince, parityPast)
		}
		if m.StoppedSince != 0.0 || m.RefocusSince != 0.0 || m.RefocusOp != "" {
			t.Fatalf("the OTHER three anchors are cleared unconditionally on both "+
				"sides; that half is not the divergence: %+v", *m)
		}
	})
	t.Run("外包: the same anchor is CLEARED", func(t *testing.T) {
		api := newParityServer(t)
		id := seedParityWorker(t, api, func(w *OutsourceWorker) {
			w.DesiredState = DesiredStateOffline
			w.StoppingSince = parityPast
			// All four seeded non-zero, because this block carries a second job:
			// it is the only guard over clearWindDownRow's ALL-FOUR-OR-NONE
			// contract. Measured (T-65 包⑤, second round): deleting the RefocusOp
			// write from that shared body left a 777-test scope entirely GREEN
			// before these three lines existed — and the body is shared by FIVE
			// call sites now, so one silent edit moves all five at once. A partial
			// clear leaves the pair (refocus_since > 0 ∧ stopped_since > 0), which
			// workerHasStateToFlush reads as "already collected" and which shoots
			// the next owner-op with no close-out.
			w.StoppedSince = parityPast
			w.RefocusSince = parityPast
			w.RefocusOp = refocusOpRefocus
		})
		rec := httptest.NewRecorder()
		api.HandleReportWakingApiSelfWakingPost(rec,
			taskReq(t, "POST", "/api/self/waking", map[string]any{}, id, "agent"))
		if rec.Code != http.StatusOK {
			t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
		}
		w, err := api.dal.GetOutsourceWorker(id)
		if err != nil || w == nil {
			t.Fatalf("read back: %v", err)
		}
		if w.StoppingSince != 0.0 {
			t.Fatalf("worker stopping_since = %v, want 0 — workerReportWaking "+
				"clears it with no desired_state guard. If this is red because "+
				"somebody ADDED the guard, that is the fix this row is waiting "+
				"for: delete this block and say so.", w.StoppingSince)
		}
		if w.StoppedSince != 0.0 || w.RefocusSince != 0.0 || w.RefocusOp != "" {
			t.Fatalf("clearWindDownRow left part of the epoch behind: "+
				"stopped_since=%v refocus_since=%v refocus_op=%q — ALL FOUR or "+
				"none. A surviving (refocus_since > 0 ∧ stopped_since > 0) pair is "+
				"indistinguishable from a genuinely collected epoch, and nothing "+
				"downstream can heal a stale PAIR.",
				w.StoppedSince, w.RefocusSince, w.RefocusOp)
		}
	})
}

// ── T-65 包③: the fixture guard the side-effect columns stand on ─────────────

// TestVerbPopulationParityFixtureLandsBothPopulationsOnOneWarden — 包③ reads the
// stop verbs' SIDE EFFECTS (was a kill dispatched? to whom?) with
// drainFrames(t, api, parityMachineA), and that drains exactly ONE warden's FIFO.
// It sees BOTH populations only because the two placements are the same string
// TODAY, by two independent routes that never reference each other:
//
//	seedParityMember  → DesiredMachineID = parityMachineA, connectOnlineMachine(…, parityMachineA)
//	newActiveWorker   → DesiredMachineID = ServerSelfHost  (worker_lifecycle_test.go:96)
//	                    workerSpawnTarget[id] = ServerSelfHost (worker_lifecycle_test.go:113)
//	parityMachineA    = ServerSelfHost  (this file, :254)
//
// That is a COINCIDENCE, not a design, and it fails SILENTLY. Give parityMachineA
// an id of its own — the obvious edit the moment anyone wants a cross-machine row
// — and every outsource frame lands on a warden the matrix never drains. The
// matrix then reports "外包什麼都沒派", which is a fixture artefact wearing the
// costume of a real behavioural divergence, with nothing red anywhere.
//
// So this test asserts the coincidence itself, and it asserts it through the two
// PRODUCTION resolvers rather than by comparing the constants: the constants
// agreeing is not the property the matrix needs — the property it needs is that
// the two populations' kill frames are ADDRESSED to the same warden, and that is
// what these two functions answer.
//
// 🔴 The two resolvers are NOT the same function and do not agree in general
// (this is a real, previously unmeasured divergence, T-65 包③ recon):
//
//	resolveWorkerKillTarget  workerSpawnTarget[id]  → hub.MachineOf(id) → ""
//	memberKillTargetWarden   hub.MachineOf(id)      → wardenTargetOf(id)
//
// Different first choice, different last resort ("" = dispatch nothing, vs the
// pin). They agree HERE because the fixture puts a live SSE claim and a spawn
// target on one machine. Converging or whitelisting that divergence is 包③'s
// job; keeping the fixture honest while that happens is this test's job.
func TestVerbPopulationParityFixtureLandsBothPopulationsOnOneWarden(t *testing.T) {
	api := newParityServer(t)
	seedParityMember(t, api, "m-parity-placement", nil)
	workerID := seedParityWorker(t, api, nil)

	staffTarget := api.memberKillTargetWarden("m-parity-placement")
	workerTarget := api.resolveWorkerKillTarget(workerID)

	if staffTarget != parityMachineA {
		t.Fatalf("staff kill frames are addressed to %q, but the matrix drains %q — "+
			"drainFrames would miss every staff side effect", staffTarget, parityMachineA)
	}
	if workerTarget != parityMachineA {
		t.Fatalf("outsource kill frames are addressed to %q, but the matrix drains %q — "+
			"every outsource side effect would read as \"dispatched nothing\", which is a "+
			"FIXTURE artefact, not a behavioural divergence", workerTarget, parityMachineA)
	}
	if staffTarget != workerTarget {
		t.Fatalf("the two populations' kill frames go to different wardens (%q vs %q); "+
			"a one-warden drain cannot compare them", staffTarget, workerTarget)
	}
}
