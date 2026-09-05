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
	// returned. A verb can converge on all nine row columns, on the dispatch and
	// on the money, and still differ on whether the session about to end was
	// TOLD to close out — which is the difference between a hand-off and a
	// yank, and it appears in no column above.
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
// that the verb fanned, into three answers.
//
// 🔴 THREE CLASSES, NOT FOUR, AND THE REASON IS NAMED. The obvious fourth shape
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
	// This is the whole outsource side of the file today: PutOutsourceWorker
	// deliberately fans no member patch (dal.go:512), so a worker's only
	// member-topic frame in the entire package is openWorkerHandoverGrace's
	// (worker_spawn.go:2259).
	noticedNothing noticeSet = ""
	// noticedPlain — a member-topic frame arrived and carried no
	// offboard_notice. 🔴 NO CELL IS EXPECTED TO BE THIS TODAY; it is here so
	// that the shape has a NAME rather than collapsing into noticedNothing,
	// which is the same reason costLost exists above. The two are wildly
	// different facts about a stop — 「the cockpit was told and the agent was
	// told nothing」 versus 「nobody was told anything」 — and a column that
	// folded them together would score a lost 預告 as a lost connection.
	noticedPlain noticeSet = "delta"
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
		if out == noticedNothing {
			out = noticedPlain
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

	{
		verb: "起來", field: "dispatched",
		why: "🔴 FIRST ROW ON A NON-DATABASE COLUMN (T-65 包③). The two sides send " +
			"different frames for OPPOSITE REASONS, and only one of them is a decision: " +
			"外包 restart DISPLACES the session by design — respawnWorkerForOwnerOpNow " +
			"kills the old one and re-dispatches a fresh START (worker_spawn.go:1958-1963), " +
			"and ownerOpRestart is the single op that skips the wind-down arm " +
			"(:1595 with ownerOpDisplacesTheSession at :1678). 正職 activate dispatches " +
			"its one STOP BY ACCIDENT: it clears stopping_since and waking_since and " +
			"deliberately clears NEITHER refocus_since NOR stopped_since " +
			"(api_members.go:1051-1057), so the reconcile it fires at :1086 walks into " +
			"decideUp's recycle arm — online, an open refocus epoch, dump already done " +
			"(reconcile.go:562, AgentStopped at :1235) — and kills the session it just " +
			"told to come up. NOTHING IN 起來 ASKED FOR THAT FRAME; it is a consequence " +
			"of the clear-set complement the three rows above already document, now " +
			"visible one layer further out. " +
			"⚠️ SCOPE: 包③ converges the STOP verbs; 起來 is not one of them, so this row " +
			"is whitelisted rather than deleted. It is NOT a permanent difference and " +
			"must not be read as one — whoever converges 起來 owns it, and the question " +
			"to answer then is 「should activate clear the refocus epoch」, not 「should " +
			"activate stop dispatching」. " +
			"📌 THIS ROW IS WHY THE COLUMN EXISTS: the divergence is invisible to all nine " +
			"database columns above it — both sides end desired=online with the same " +
			"anchors — and no test in this package saw it before the column was added.",
	},

	{
		verb: "起來", field: "banked_cost",
		why: "SAME ROOT AS THE `dispatched` ROW ABOVE, one column further out: 外包 " +
			"restart displaces the session, so respawnWorkerForOwnerOpNow reaches " +
			"respawnWorkerNow, which banks the dying session's live cost BEFORE the kill " +
			"(worker_spawn.go:1954, 「so the respawn never zeroes the visible spend」). " +
			"正職 activate ends no session of its own — the one STOP frame it emits is the " +
			"reconcile recycle arm's, dispatched by dispatchRobustStopNow, and THAT " +
			"function banks nothing (reconcile.go:2916-2941 is six statements and " +
			"bankLiveCost is not one of them). ⚠️ SCOPE: 包③ converges the STOP verbs and " +
			"起來 is not one of them, so this row is whitelisted, not deleted. It is NOT a " +
			"permanent difference: whoever converges 起來 owns it.",
	},

	{
		verb: "起來", field: "noticed",
		why: "🔴 THE THIRD ROW ON THE SAME ROOT, and the one that reaches the AGENT. " +
			"正職 activate leaves refocus_since AS READ (api_members.go:1051-1057), so " +
			"the row its putMember fans at :1071 is still inside a refocus epoch — and " +
			"offboardKindOf's ONLINE arm carries a notice on refocus_since > 0 alone " +
			"(api_members.go:300-303), with winddownKindFor answering soft for 重新聚焦. " +
			"So the 起來 press ends by handing the agent 「work the sequence, then call " +
			"report_stopped yourself」: the member was told to come UP and, in the same " +
			"breath, shown a wind-down. 外包 restart says nothing at all — ownerOpRestart " +
			"skips the wind-down arm (worker_spawn.go:1595, :1678), so " +
			"openWorkerHandoverGrace (the worker's ONLY member-topic publisher, :2259) is " +
			"never called, and PutOutsourceWorker deliberately fans no member patch " +
			"(dal.go:512). ⚠️ SCOPE: 包③ converges the STOP verbs and 起來 is not one of " +
			"them, so this is whitelisted, not deleted — the same scope the two rows above " +
			"carry. It is NOT a permanent difference, and the question whoever converges " +
			"起來 has to answer is still the ONE question all three rows ask: 「should " +
			"activate clear the refocus epoch」. Answer it yes and the dispatch, the bank " +
			"and this sentence all fall out together; answer it per-column and you will " +
			"fix three symptoms of one cause three times.",
	},

	// ── 停止 / 加速停止 / 重新聚焦 / 改機器 / 換 model on banked_cost ────────
	// NO ROWS. The RESULT is measured — the three parity tests are green, so both
	// cells really do read costUntouched on all five. The MECHANISM behind it is
	// NOT one story, and an earlier draft of this comment told it as one. Split:
	//
	//  停止 / 加速停止 / 重新聚焦 — structural. The handler calls
	//  openWorkerHandoverGrace directly (api_outsource.go:736 / :643 / :529) and
	//  its ONLINE arm publishes a 預告 and returns without reaching either kill
	//  funnel, so no fold can run on either population.
	//
	//  🔴 改機器 / 換 model — NOT structural, and do not read this row as if it
	//  were. They never touch openWorkerHandoverGrace directly: they enter the
	//  owner-op funnel at worker_spawn.go:1595-1604, which has THREE exits that
	//  DO bank — workerHasStateToFlush false (:1604 → respawnWorkerForOwnerOpNow
	//  → respawnWorkerNow → bankLiveCost at :1954), and openOwnerOpHandover's two
	//  persist-failure fallbacks (:1836, :1842, the same respawn). The ladder
	//  refusal (:1827) reaches no fold either way. So these two cells are
	//  costUntouched because TODAY'S FIXTURE lands on the happy path, not because
	//  banking is unreachable — change what workerHasStateToFlush answers and
	//  they start banking, silently, with this comment still claiming they cannot.
	//
	// 🔴 AND DO NOT RE-DERIVE THIS FROM `dispatched`. An earlier draft offered the
	// `dispatchedNothing` literals on these five as corroboration — "no kill left,
	// so no kill banked". That inference is BACKWARDS: stopWorkerNow banks BEFORE
	// the kill and skips the enqueue entirely when the target is empty
	// (worker_spawn.go:1983 then :1985-1994), which is a banked-but-dispatched-
	// nothing path. An empty dispatch cell is evidence about the FIFO and nothing
	// at all about the money — which is the whole reason this column exists.
	//
	// ⚠️ Nothing mechanical guards the paragraph above: TestVerbPopulationParity-
	// WhitelistIsExplained checks that each divergence ROW carries a why, and this
	// is prose, not a row. It is a universal negative maintained by hand.

	// ── 重新聚焦 (refocus) — THE ROW CONVERGED IN T-65 包②; THE SENTENCE DID NOT ──
	// Both rows that stood here are DELETED rather than widened: 「重新聚焦｜
	// http_status」 (200 vs 409) and 「重新聚焦｜restart_after_stop」 (staff-only
	// column). The worker face now takes the same aStopWasEverAskedFor branch the
	// member face does — see queueWorkerRestartAfterStop (member_ownerop_winddown.go)
	// and the 🔴 block in the worker refocus handler. Block ① is what proves it:
	// the two literals for this verb are now identical, so a regression on either
	// side reddens the matrix rather than being absorbed by a whitelist row.
	//
	// ⚠️ AND THIS HEADER USED TO END 「no rows left」, WHICH IS NO LONGER TRUE. 包②
	// converged every DATABASE column of this verb and the two sides still say
	// different things on the wire — which is the whole argument for a
	// non-database column existing at all, restated by the verb that was already
	// declared finished.
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

	// ── 強制停止 (force-stop) ──────────────────────────────────────────────
	// ── 停止（離線起點）: what 包③ did NOT converge, and why ───────────────
	//
	// 🔴 ALL THREE ROWS BELOW ARE ONE FACT WEARING THREE HATS: on a subject with
	// no session, the worker verb COLLECTS its own close-out on the spot
	// (openWorkerHandoverGrace → collectWorkerStop) and the staff verb hands the
	// subject to the reconcile tick instead. They are three rows and not one
	// because the whitelist is keyed per FIELD, and a reader who converges one of
	// them must be told which of the three they just changed.
	//
	// 包③ deliberately did NOT converge this. The collect funnel is 包⑤'s whole
	// subject (worker_spawn.go's ~810 lines against member_ownerop_winddown.go's
	// ~666), and pulling it forward would mean either giving the staff verb a
	// collect it does not have — a behaviour change, not a convergence — or
	// taking the worker's away, which recon坐實 would silently strand it:
	// runOutsourceTick `continue`s on desired_state=offline, so a stopped worker
	// NEVER REACHES THE FSM at all and nothing downstream would ever collect it.
	{
		verb: "停止（離線起點）", field: "stopped_since",
		why: "外包 openWorkerHandoverGrace's `!hub.IsOnline` arm calls collectWorkerStop, " +
			"which latches StoppedSince = nowSecs() before it kills. 正職 writes no " +
			"stopped_since at all: decideDown's `!obs.Online ⇒ converged offline` branch " +
			"is what ends a staff stop, and the tick reaches an offline member. The worker " +
			"tick does not — runOutsourceTick skips desired_state=offline outright — which " +
			"is WHY the worker side collects inline. Converging this belongs to 包⑤ " +
			"(收口漏斗合一), where both funnels are on the table at once.",
	},
	{
		verb: "停止（離線起點）", field: "dispatched",
		why: "the same collect, seen from the warden FIFO: collectWorkerStop ends in " +
			"stopWorkerNow, so ONE stop frame leaves for the worker's machine. 正職 " +
			"dispatches nothing — cancellingWake is false for an offline member " +
			"(waking_since is unset), so dispatchRobustStopNow is not called, and " +
			"reconcileMemberNow's converged-offline branch queues no RPC. ⚠️ There is a " +
			"SECOND, unmeasured divergence hiding under this cell: the two populations " +
			"resolve WHICH machine a kill goes to through different functions in " +
			"different order — resolveWorkerKillTarget asks s.workerSpawnTarget first " +
			"and falls back to hub.MachineOf (empty ⇒ dispatch NOTHING), while " +
			"memberKillTargetWarden asks hub.MachineOf first and falls back to the " +
			"PINNED machine. This fixture cannot see it: both seeds pin and connect the " +
			"same machine, so both resolvers answer parityMachineA. Named here rather " +
			"than left unwritten — it is 包⑤'s to settle.",
	},
	{
		verb: "停止（離線起點）", field: "banked_cost",
		why: "collectWorkerStop's putMember runs the fold that pops the live telemetry " +
			"figure into banked_cost; the staff verb's putMember does not, because a " +
			"staff stop is not finished yet at this point — the money is banked when the " +
			"session actually ends. 🔴 THIS IS THE ROW THAT WOULD BE READ WRONG IF THE " +
			"OTHER TWO WERE CONVERGED CARELESSLY: recon on 包③ established that routing " +
			"the worker through the staff dispatch (dispatchRobustStopNow) drops " +
			"bankLiveCost entirely, which is a SILENT loss of owner-visible money — " +
			"costLost exists as a distinct literal for exactly that outcome, and it must " +
			"never become the answer here.",
	},
	{
		verb: "強制停止", field: "banked_cost",
		why: "🔴 THIS IS THE ONE 包③ IS ABOUT, AND IT IS THE ONLY ROW IN THIS WHITELIST " +
			"THAT COSTS THE OWNER MONEY RATHER THAN CORRECTNESS. Both sides end with the " +
			"same desired_state, the same anchors and the SAME single `stop` frame — the " +
			"nine row columns and `dispatched` all agree — and only the money differs. " +
			"外包 force-stop goes through stopWorkerNow, which banks the dying session's " +
			"live cost before the kill (worker_spawn.go:1983). 正職 force-stop calls " +
			"dispatchRobustStopNow (api_members.go:1488), which banks nothing; the staff " +
			"figure is folded LATER, and only if an SSE last-disconnect edge arrives " +
			"(api_infra.go:879-882, the sole member-side call site). " +
			"⚠️ SCOPE — READ THIS BEFORE 「CONVERGING」 IT: the safe direction is NOT " +
			"free. Moving 外包 onto dispatchRobustStopNow would delete the ONLY " +
			"unconditional bank on a stop and leave the money to the disconnect edge — " +
			"which never arrives for a subject that is ALREADY offline, and that is " +
			"precisely the population openWorkerHandoverGrace's offline arm serves. " +
			"That direction turns a visible divergence into a silent, conditional loss. " +
			"📌 MEASURED, not reasoned (T-65 包③ recon): commenting out worker_spawn.go:1983 " +
			"leaves `go test -run '(?i)(stop|bank|cost|kill)'` at 133/133 GREEN while a " +
			"probe shows banked_cost=0 with the live figure stranded in telemetry. " +
			"Nothing in this repository was watching that line before this column. " +
			"⚠️ DO NOT REUSE THAT `-run` AS THIS COLUMN'S VERIFICATION SCOPE — it is " +
			"quoted here as the BLINDNESS being fixed, not as the command that checks " +
			"the fix. TestVerbPopulationParity* carries none of stop/bank/cost/kill in " +
			"its NAME, so that pattern never runs this matrix at all: with :1983 " +
			"commented out it prints `ok` over 133 tests while the very cell below is " +
			"the one that would have failed (measured 2026-09-06, T-65 包③). Verify " +
			"this column with a pattern that matches `parity` — 136 tests, and the " +
			"mutant then fails as 強制停止 want Cost:banked / got Cost:live.",
	},
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
	{
		verb: "強制停止", field: "noticed",
		why: "🔴 THIS ROW IS NOT A DESIGN DIFFERENCE — IT IS A RULED INVARIANT BEING " +
			"VIOLATED ON THE 正職 SIDE, and it is the SAME missing arm the stopping_since " +
			"row above describes, now visible as something an agent actually receives. " +
			"The ruling is that force-stop SAYS NOTHING (api_members.go:249-276: 「the " +
			"recipient is about to stop existing, so a sentence meant to change its " +
			"behaviour has no one to change」; reconfirmed 2026-08-18 after the owner " +
			"nearly reversed it, c-7b2163781ee2 → c-5c8bc3d7362d). Its enforcement is " +
			"forcedEpochLive = ForcedStopAt > 0 && StoppingSince > 0 && ForcedStopAt >= " +
			"StoppingSince. This case seeds stopping_since in the FUTURE and the staff " +
			"handler has only `if m.StoppingSince <= 0.0 { m.StoppingSince = nowSecs() }` " +
			"(api_members.go:1458), so the future stamp survives while ForcedStopAt is " +
			"stamped at nowSecs() — the third term is FALSE, forcedEpochLive is false, " +
			"gracefulStopEpochOpen is TRUE, and the putMember at :1479 hands the session " +
			"it is about to kill the full soft sequence. 外包 is silent by TWO independent " +
			"guards: its force-stop reaches no member-topic publisher at all (the only " +
			"openWorkerHandoverGrace call sites are api_outsource.go:529/:643/:736 and " +
			"worker_spawn.go:1846/:2560, none on this path), and its pull-back keeps " +
			"forcedEpochLive true so a frame would carry nothing anyway. " +
			"⚠️ WHY IT IS WHITELISTED RATHER THAN FIXED HERE: the fix is a production " +
			"change on the 正職 handler (give it the pull-back arm), which is what the " +
			"stopping_since row already says the fix is — ONE edit closes BOTH rows, and " +
			"it belongs to whoever takes that decision, not to the column that found it. " +
			"⚠️ AND THE REACHABILITY CAVEAT ON THE ROW ABOVE APPLIES VERBATIM AND IS NOT " +
			"A DISMISSAL: independent review's grep found no production path that stamps " +
			"a FUTURE staff stopping_since, so today this is reachable through the " +
			"fixture and not known to be reachable in production. That is the reach of " +
			"one grep. What THIS row adds is the cost if it ever is: the stopping_since " +
			"row could be read as a cosmetic anchor difference, and it is not — the same " +
			"missing arm makes a killed staff session receive a 預告 telling it to work " +
			"its close-out, which is precisely the frame T-a9d6 removed.",
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
				// activate leaves refocus_since/stopped_since AS READ (api_members.go:1051-1057),
				// so reconcileMemberNow (:1086) meets decideUp's recycle arm — online +
				// an open refocus epoch whose dump is done (reconcile.go:562, AgentStopped
				// at :1235) — and fires ONE stop. An ACCIDENT of what activate does not
				// clear, not a designed part of 起來.
				Dispatched: "stop",
				Cost:       costUntouched,
				// activate clears stopping_since and waking_since and leaves
				// refocus_since AS READ (api_members.go:1051-1057), so the row putMember
				// fans at :1071 is still inside a refocus epoch. offboardKindOf's ONLINE
				// arm carries a notice on refocus_since > 0 alone (api_members.go:300-303),
				// and winddownKindFor(重新聚焦) answers soft — so 起來 ends by handing the
				// agent the wind-down sequence. Same clear-set complement as the four
				// whitelist rows on this verb, one layer further out again.
				Noticed: noticedNotice,
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
				// restart DISPLACES the session by design: respawnWorkerForOwnerOpNow kills the
				// old one and re-dispatches a fresh START (worker_spawn.go:1958-1963).
				// ownerOpRestart is the one op that skips the wind-down arm (:1595, :1678).
				// SORTED, so "start+stop" — the emission order is stop-then-start.
				Dispatched: "start+stop",
				// respawnWorkerForOwnerOpNow → respawnWorkerNow banks BEFORE the kill
				// (worker_spawn.go:1954); the start+stop above is the same call's evidence.
				Cost: costBanked,
				// NOTHING is said. ownerOpRestart skips the wind-down arm
				// (worker_spawn.go:1595 with ownerOpDisplacesTheSession at :1678), so
				// openWorkerHandoverGrace — the worker's ONLY member-topic publisher
				// (worker_spawn.go:2259) — is never called, and PutOutsourceWorker
				// deliberately fans no member patch (dal.go:512).
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
				seedParityMemberOffline(t, api, "m-parity-stop-off", nil)
				notices := watchMemberDeltas(t, api)
				code := postMember(t, api, "m-parity-stop-off", "deactivate", nil,
					api.HandleDeactivateMemberApiMembersMemberIdDeactivatePost)
				return memberTerminal(t, api, "m-parity-stop-off", code, notices)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorkerOffline(t, api, nil)
				notices := watchMemberDeltas(t, api)
				code := postWorker(t, api, id, "stop", nil,
					api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
				return workerTerminal(t, api, id, code.Code, notices)
			},
			// 正職: the five row writes are applyStopVerbRow's, same as the online
			// row. cancellingWake is false (an offline member with waking_since=0
			// is not waking), so no dispatchRobustStopNow; reconcileMemberNow then
			// lands in decideDown's FIRST branch — `!obs.Online` ⇒ converged
			// offline — which dispatches nothing and latches nothing.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				Dispatched:       dispatchedNothing,
				Cost:             costUntouched,
				// the notice rides the handler's own putMember, exactly as online.
				Noticed: noticedNotice,
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
			// 正職: `if m.StoppingSince <= 0.0 { m.StoppingSince = nowSecs() }` —
			// the future stamp is NOT touched.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorFuture, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
				// the ONLY unconditional dispatch on either side: dispatchRobustStopNow
				// (api_members.go:1487 → reconcile.go:2919-2930) with no state test in front
				// of it. This handler never calls reconcileMemberNow at all.
				Dispatched: "stop",
				Cost:       costUntouched,
				// 🔴 A SOFT 預告 IS FANNED AT A MEMBER BEING KILLED, AND THAT IS A REAL
				// DEFECT THIS COLUMN FOUND — not a property anybody chose. force-stop is
				// ruled SILENT (api_members.go:249-276, reconfirmed 2026-08-18 after the
				// owner nearly reversed it), and the enforcement is forcedEpochLive:
				// ForcedStopAt > 0 && StoppingSince > 0 && ForcedStopAt >= StoppingSince.
				// This case seeds stopping_since in the FUTURE (parityFuture = 4.0e9) and
				// the staff handler has only `if m.StoppingSince <= 0.0 { … }`
				// (api_members.go:1458), so the future stamp survives while ForcedStopAt is
				// stamped at nowSecs() — the third term is FALSE, forcedEpochLive is false,
				// gracefulStopEpochOpen is TRUE, and the putMember at :1479 hands the
				// dying session the full 「work the sequence, then call report_stopped
				// yourself」 text. ⚠️ The literal is what the handler DOES today, not what
				// it should do — see the 強制停止|noticed whitelist row, which is where the
				// direction of the fix is argued.
				Noticed: noticedNotice,
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
				// silent, and by TWO independent guards rather than one — which is what
				// makes the staff cell above a defect rather than a coin-flip. The worker
				// force-stop reaches no member-topic publisher at all (grep: the only
				// openWorkerHandoverGrace call sites are api_outsource.go:529/:643/:736 and
				// worker_spawn.go:1846/:2560, none of them on this path), AND the
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
