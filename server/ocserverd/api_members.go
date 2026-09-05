package main

// api_members.go — the roster + presence + lifecycle handlers
// (handlers.handle_list_members … handle_dismiss_member + the three
// self-report presence tools). Every durable write funnels through the DAL
// and fans a member delta through the hub (the Python Repository
// commit-funnel behaviour).
//
// Reconcile dispatch note: activate/deactivate fire the EVENT-DRIVEN
// single-member reconcile (reconcile.go reconcileMemberNow — the Python
// _dispatch_reconcile_now click seam, sharing the cadence's store so the 30s
// tick stays an idempotent backstop); force-stop and the first stopped-report
// of a refocus-marked member fire the immediate robust STOP
// (dispatchRobustStopNow — handlers._dispatch_robust_stop_now).

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// minSelfRestartSecs is the restart_self minimum-liveness floor (T-4c71): a
// self-triggered recycle within this many seconds of the session connecting is
// refused (429), so a freshly respawned agent cannot immediately self-restart
// and spin a respawn storm. Owner-approved at 10 minutes — a flat floor, kept
// distinct from the context-high boot-storm guard's MinBootSecs.
const minSelfRestartSecs = 600.0

// putMember validates + persists a member and fans the member delta: a
// dismiss (roster_status=removed, the soft delete) rides as op=remove
// (deleted:true, payload null — Repository.put_member parity); every other
// write is a patch carrying the partial convenience payload (spec/sse.md
// §2.2: {id, name, status, desired_state, owner_id}).
func (s *apiServer) putMember(m Member, trigger string) error {
	if err := ValidateMember(m); err != nil {
		return err
	}
	if err := s.dal.PutMember(m); err != nil {
		return err
	}
	s.publishMemberPatch(m, trigger)
	return nil
}

// persistMemberOpReceipt stores the five last_op* columns of an ALREADY-STAMPED
// member row through their sole writer, then fans the member delta (T-55).
//
// It exists so the two halves cannot drift apart. Every caller below used to get
// the delta for free from putMember; the columns left PutMember's SET list, so a
// caller that wrote them and forgot publishMemberPatch would lose the cockpit
// refresh SILENTLY — nothing red, the panel simply stops updating. Binding the
// write and the fan into one call is what makes forgetting impossible rather
// than merely discouraged.
//
// On a write failure NOTHING is fanned: a delta announcing a receipt the
// database does not hold is worse than no delta at all.
//
// ⚠️ MEMBER ROWS ONLY. The outsource half deliberately does not fan a member
// patch for an `ow-` id (its changes travel on the outsource_worker projection),
// so worker callers write through s.dal.SetMemberOpReceipt directly and keep
// whatever publish they already had.
func (s *apiServer) persistMemberOpReceipt(m Member, trigger string) error {
	if err := s.dal.SetMemberOpReceipt(m.ID, m.LastOp, m.LastOpOK, m.LastOpLog,
		m.LastOpReason, m.LastOpAt); err != nil {
		return err
	}
	s.publishMemberPatch(m, trigger)
	return nil
}

// persistMemberWindDownAnchors stores the four wind-down anchor columns of an
// ALREADY-STAMPED member row through their sole writer (T-55).
//
// 🔴 CALL IT BEFORE THE WHOLE-ROW WRITE, WHICH IS THE OPPOSITE ORDER FROM
// persistMemberOpReceipt NEXT DOOR — and the difference is load-bearing, not
// tidiness. putMember fans the member delta, and publishMemberPatch says what
// keys on that delta: the wind-down / recycle hook in cli/ocagent
// (shouldWindDown). These four columns ARE what that hook reads. Fan the delta
// first and the agent refetches a row whose anchors have not landed yet, reads
// "no wind-down in progress", and carries on — a wrong answer produced by
// nothing but ordering, on a path where the retry is the agent never stopping.
// A receipt has no such reader, which is why the receipt may land after.
//
// ⚠️ SO DO NOT "UNIFY" THE TWO. Reordering this to match the receipt reintroduces
// that race and NOTHING GOES RED: every test still sees both writes land.
//
// On a row that does not exist yet the UPDATE is a clean no-op and PutMember's
// INSERT carries the four itself, so the order is correct for a new row too.
//
// ⚠️ WHAT THIS ORDER COSTS, said plainly because an independent review had to ask
// for it. The failure used to be atomic: one write, so it either happened or it
// did not. Now the anchors are already DURABLE when the row write fails, and that
// is a state that could not exist before: the row is on a wind-down rung with
// whatever clock that rung carries, NO delta was fanned, and the owner got a 500.
// It is bounded and recoverable — an equal-rank verb re-stamps, and any later
// write to the row fans a delta that wakes the hook — but the ladder gate
// (winddownStageMayAdvanceTo) refuses a LOWER rung, so after a failed 加速停止 the
// owner's plain 重新聚焦 bounces until 加速停止 is pressed again.
//
// The order is still the right side of the trade: the failure it prevents (the
// agent reads "no wind-down" and never stops) is unbounded and silent, while the
// one it creates is bounded, visible as a 500, and re-armable. That is the
// argument — not that this direction is free.
func (s *apiServer) persistMemberWindDownAnchors(m Member) error {
	return s.dal.SetMemberWindDownAnchors(m.ID, m.StoppingSince, m.StoppedSince,
		m.RefocusSince, m.RefocusOp)
}

// persistWorkerWindDownAnchors is the outsource face of the call above. It reads
// the four columns straight off the worker rather than going through
// memberFromWorker, which mints activated_ts as a side effect — a projection this
// call has no business triggering. There is no second table behind it:
// DAL.PutOutsourceWorker IS PutMember(memberFromWorker(w)), so a worker row is a
// member row with kind='outsource' and these four columns are the same columns.
func (s *apiServer) persistWorkerWindDownAnchors(w OutsourceWorker) error {
	return s.dal.SetMemberWindDownAnchors(w.ID, w.StoppingSince, w.StoppedSince,
		w.RefocusSince, w.RefocusOp)
}

// publishMemberPatch fans the member delta and nothing else. It is putMember's
// wire half, split out so a SINGLE-COLUMN writer (AddMemberBankedCost and the
// setters beside it) can keep the push a caller used to get for free from the
// whole-row write, WITHOUT dragging a stale snapshot of every other column back
// into the database with it. Marking a column insertOnly (so a whole-row write
// stops carrying it) and forgetting this call is a silent loss: nothing goes red,
// the cockpit simply stops converging.
func (s *apiServer) publishMemberPatch(m Member, trigger string) {
	op := "patch"
	if m.RosterStatus == RosterStatusRemoved {
		op = "remove"
	}
	// A member delta reaches ITS OWN connection (the wind-down / recycle hooks
	// key on a member delta naming self — cli/ocagent shouldWindDown) plus the
	// owner cockpit; other agents ignore it (spec/sse.md §4).
	s.hub.Publish("member", op, "member", wireOwnerID+"::"+m.ID, s.offboardDeltaPayload(m),
		audienceMembers(m.ID), trigger)
}

// memberDeltaPayload is the member delta's partial convenience payload
// (repository._member_payload — the client reconciles by refetch).
func memberDeltaPayload(m Member) map[string]any {
	return map[string]any{
		"id":            m.ID,
		"name":          m.Name,
		"status":        m.RosterStatus,
		"desired_state": m.DesiredState,
		"owner_id":      wireOwnerID,
	}
}

// offboardDeltaPayload is memberDeltaPayload plus the offboard notice, and it is
// the whole of "改回真的推播" (owner 2026-08-16, card rc-66b82a584c4d): the
// SERVER composes the sentence and carries the 〈停止〉 steps in the frame it
// pushes, instead of the agent fetching them back over HTTP once it notices it
// is being collected.
//
// The notice rides ONLY a member that is actually being wound down — a refocus
// epoch, or a graceful 下線 (offboardKindOf decides). Attaching it to every
// member delta would put a document fold and a couple of kilobytes on every
// roster change. ⚠️ Within those states it rides EVERY write to that row, not
// just the first: the client is what de-duplicates, by keying on the sentence
// it last printed.
//
// An empty notice omits the key rather than sending "": the client's fallback
// arms on the key being absent, and an empty string would read as "the server
// sent me a notice and it said nothing".
func (s *apiServer) offboardDeltaPayload(m Member) map[string]any {
	payload := memberDeltaPayload(m)
	kind, carries := offboardKindOf(m, nowSecs())
	if !carries {
		return payload
	}
	if notice := s.offboardNoticeFor(m, kind); notice != "" {
		payload["offboard_notice"] = notice
	}
	return payload
}

// offboardKindOf answers the two questions every offboard delta turns on: does
// this member carry a notice at all, and is it the SOFT one or the FINAL call.
//
// The owner's ruling (2026-08-16) is that his own two buttons and the agent's
// own context pressure walk the SAME sequence, and that what tells the
// situations apart is whether there is still room:
//
//   - SOFT (停止) — 下線 (desired offline + a stopping anchor, the graceful
//     arm) and EVERY refocus cause except the one below: 重新聚焦, 改機器,
//     model/runtime, restart_self, and the FIRST context threshold. It says
//     work the sequence, then call report_stopped yourself; no countdown clause,
//     because on these arms there is no clock AT ALL — not now, and not later.
//     (restart_self stays in the list above because it is a refocus CAUSE — an
//     agent asking for its own recycle. It stopped being the verb the notice
//     ends with: owner c-5b3d8f192a0b / rc-5d044f0c1266, and since T-3201 that
//     sentence is the read-only head of 〈停止〉, not a Go literal.)
//   - FINAL (加速停止) — the two 加速停止 causes, and only those: the SECOND
//     context threshold (context_high) and the owner's own press
//     (accelerated_stop). The collection is already under way and the recycle
//     clock is running, so the sentence has to say so.
//
// 🔴 The membership is decided by winddownKindFor, not here and not in
// recycleGraceFor. Both of those used to carry their own copy of the list, and
// the copies were kept identical by hand (T-ed79).
//
// 🔴 The soft arm is the ONLY reason 下線 reaches the agent at all. Before this,
// a deactivate stamped stopping_since and nothing else, so the notice condition
// (refocus_since > 0) was false and the agent was collected having never been
// shown the sequence — while the client-side wind-down declared "durable state
// already server-side — nothing extra to flush" on its behalf, which was not
// true of any session holding an unwritten hand-off.
// There is NO soft→final promotion any more (owner 2026-08-19, card
// rc-c540367065ad). 重新聚焦 used to open soft and, ten minutes later, change
// its mind and say "you have 120 seconds" — a split only an agent that ran past
// the soft window ever saw. The owner's ruling is that 重新聚焦 is the same
// shape as 下線: no countdown in the sentence because no clock is running, and
// the collection is the agent's own stopped report or the owner's force-stop.
// The pair has to move together — recycleGraceFor is the clock, this is the
// sentence, and changing one without the other is what makes a silent deadline.
// ⚠️ `now` is READ BY NOTHING in here any more, and that is the invariant, not
// an oversight: after T-c996 neither soft arm turns on a clock, so there is no
// longer any time at which this answer changes. It stays in the signature
// because the question this function answers is still "what would this member
// be told AT time T" — and a later arm that does need a clock must be handed
// one here rather than reaching for a global one, which is how the sentence and
// the clock came apart in the first place.
func offboardKindOf(m Member, now float64) (kind string, carries bool) {
	_ = now
	if m.DesiredState == DesiredStateOffline {
		// Only the graceful arm: a member with no stopping anchor is not being
		// wound down (and a cancelled wake is force-stopped outright, which is
		// deliberately silent — see HandleForceStopMember).
		//
		// 🔴 THIS SOFT IS HARD-CODED, and deliberately does NOT read
		// winddownKindFor (T-ed79). That function answers "what does this
		// refocus_op mean", and this arm has no refocus_op to ask about: 下線 is
		// a desired_state transition, not a wind-down CAUSE, and it stamps no
		// epoch. Routing it through the single source would mean feeding it ""
		// and depending on the DEFAULT arm — which agrees today (soft) but
		// agrees by accident: the default exists so that an unruled *cause*
		// gets no deadline, and coupling 下線's ruling to it would let a future
		// change to the cause default silently move a ruling the owner made
		// about a different thing. Two rulings that coincide are not one ruling.
		//
		// It stays SOFT forever, because nothing collects it on a clock: the
		// owner ruled 下線 runs no countdown at all (rc-27d1710174dd), so a
		// notice claiming 120 seconds here would be a promise nobody keeps —
		// an agent would cut its hand-off short to beat a deadline that does
		// not exist. Escalation on this arm is the owner pressing force-stop,
		// and that path deliberately says nothing.
		//
		// 🔴 …and "deliberately says nothing" has to be enforced HERE, not just
		// asserted in prose. force-stop sets desired_state=offline AND stamps
		// stopping_since before it publishes, so on the sentence above alone the
		// member it just killed receives a full SOFT notice on its own stream —
		// telling a session that is about to be cut off to work the sequence and
		// call the close-out verb. Independent e2e verification observed exactly
		// that frame; the owner's ruling is that force-stop sends no message at
		// all.
		//
		// ⚠️ The VERB is deliberately not named in the sentence above. What the
		// e2e run saw on the wire was 「then call restart_self yourself」, because
		// that is what the notice said on the day it was observed; the seed now
		// closes on report_stopped. Naming today's verb here would put a word the
		// run never saw inside a claim about what the run observed — and the
		// observation being reported is the ARRIVAL of a soft notice at a member
		// being force-stopped, which is true under either wording.
		//
		// 🔴 RECONFIRMED 2026-08-18, and written down because it was nearly
		// reversed that day: the owner first said a force-stop should tell the
		// agent what to do (c-7b2163781ee2), was shown that this would overturn
		// the named ruling above, and chose silence again (c-5c8bc3d7362d).
		// Nothing in the code changed, which is exactly why the review is worth
		// recording — the next person to notice this arm should know it has been
		// looked at deliberately, not merely never revisited.
		//
		// 🔴 And it is enforced for BOTH kinds since T-c996. It used to be
		// enforced here for staff only: forcedEpochLive reads forced_stop_at,
		// OutsourceWorker had no such field, so the predicate was false for every
		// worker and the arm that must stay silent was the one that could still
		// speak. An outsource 停止 now stamps the same anchors (api_outsource.go)
		// — it kills on the spot, so it IS this shape, whatever it is named.
		if gracefulStopEpochOpen(m) {
			// 🔴 …WITH ONE EXCEPTION, AND ONLY ONE: the owner pressed 加速停止 on
			// this stop (T-ed79). The paragraphs above rule out a clock the
			// SERVER starts; this is a clock the owner started, on the rung
			// between "wait indefinitely" and "cut it off with no sentence at
			// all". It reads winddownKindFor rather than testing the constant
			// again, because the clock (decideDown) reads the same function —
			// which is the whole reason 下線's hard-coded soft above is safe to
			// leave hard-coded: the ONE arm that can carry a clock is the one
			// arm that asks the single source.
			if kind, clocked := winddownKindFor(m.RefocusOp); clocked {
				return kind, true
			}
			return offboardKindSoft, true
		}
		return "", false
	}
	if m.RefocusSince <= 0 {
		return "", false
	}
	// 🔴 ONE READ, not a second list (T-ed79). This arm used to spell the
	// judgement out again — 重新聚焦 soft, everything else final — beside a
	// recycleGraceFor that spelled out the same judgement in the other file.
	// The sentence and the clock now come from winddownKindFor, so a cause that
	// is told "no countdown" is, by construction, a cause nothing collects on a
	// clock. A countdown clause on an uncollected arm starts a timer in the
	// agent's head that nothing is counting; a clock on an unannounced arm cuts
	// a hand-off off with no warning at all. Both are the same bug, and they
	// are only reachable by making these two disagree.
	kind, _ = winddownKindFor(m.RefocusOp)
	return kind, true
}

const (
	offboardKindSoft  = "soft"
	offboardKindFinal = "final"
)

// forcedEpochLive: the stop this member is currently under was opened by a
// FORCE-stop, not by 下線. It is the one judgement that separates "cut off
// deliberately" from "working its close-out", and three places need it — the
// notice (a forced path says nothing), the SSE stop gate (a forced member's
// reconnect is refused) and deactivate (a forced epoch must not be re-stamped
// into a softer one). One definition, because two copies of this could disagree
// about the same member.
//
// stopping_since > 0 is what scopes it to a LIVE epoch: activate clears the stop
// anchors but deliberately KEEPS forced_stop_at as the durable record that a
// past session was cut off, so reading that column alone would treat every
// member ever force-stopped as permanently forced.
//
// 🔴 The >= is LOAD-BEARING, not defensive. force-stop stamps stopping_since
// and forced_stop_at from two nowSecs() calls with no I/O between them, and at
// 1.78e9 a float64 tick is ~238ns — so the two anchors landing on the SAME
// value is the NORMAL path, not a coincidence. Independent review measured it:
// every failure dump from the mutants that exercise the real handler shows the
// two columns identical. Tidying this into > breaks force-stop outright.
func forcedEpochLive(m Member) bool {
	return m.ForcedStopAt > 0.0 && m.StoppingSince > 0.0 &&
		m.ForcedStopAt >= m.StoppingSince
}

// gracefulStopEpochOpen is the COMPOUND that wraps forcedEpochLive: this row is
// under a 停止 that a session can still be working — an open stop epoch
// (stopping_since > 0) that was NOT opened by 強制停止.
//
// forcedEpochLive itself was never the duplicated rule; it has always had one
// definition, and the worker side calls that same definition through
// memberFromWorker. What WAS written out by hand, once per site, is this
// two-term question. The call sites below are the FIVE things a graceful stop
// epoch entitles a session to.
//
//   - the SENTENCE — offboardKindOf's desired-offline arm sends a 下線 notice
//     only for a stop the recipient can still act on (a forced session is cut
//     off deliberately and is told nothing).
//   - the CLOCK — winddownDeadlineOf answers 0 on the same two terms, so an
//     announced deadline and a collected one cannot come apart.
//   - the ESCALATION — 加速停止 (both faces: HandleAcceleratedStopMember… and
//     HandleAcceleratedStopOutsourceWorker…) refuses unless there is such an
//     epoch to escalate: nothing to accelerate on a member nobody asked to
//     stop, and no reader for a deadline addressed to a session already cut off.
//   - the COLLECT — the two worker-side arms that end a 停止 epoch without a
//     report the server can wait for: autoHandoverWorker's stop arm (session
//     confirmed gone, or the owner's accelerated deadline lapsed) and
//     workerReportStopped's 停止 arm. A forced epoch's kill already went out,
//     so there is nothing left for either to collect. Both used to write the
//     two terms out by hand; they were the copies this comment did not count.
//     ⚠️ The staff twin of the first of those — decideDown's `accelerated`
//     arm in reconcile.go — does NOT ask this question (it tests
//     StoppingSince > 0 with no forced term). That asymmetry is real and is
//     deliberately left alone here: closing it CHANGES BEHAVIOUR, which is
//     outside a convergence-only change. See the T-170e stage 2 report.
//   - the GATE — sseStopGateRefusal (api_infra.go) re-admits the SSE stream of a
//     session that is still working its offboard sequence, and refuses one whose
//     epoch was forced. Staff-only: the outsource kind returns before it. Its
//     enclosing guard already establishes stopping_since > 0 on that branch, so
//     it used to write only the forced term — which is why no grep for this
//     function's name could ever have found it.
//
// 🔴 HOW TO RE-CHECK THAT LIST, AND WHY THE OBVIOUS WAY DOES NOT WORK. An
// earlier draft of this comment said "grep `gracefulStopEpochOpen(` to re-count".
// That grep can only ever find sites that ALREADY call this function, so it is
// structurally incapable of finding the one thing the list can be wrong about: a
// site that spells the two terms out by hand. It has now missed twice — the draft
// before it claimed three sites while two worker-side ones were unfolded, and the
// draft after it claimed six while the GATE above was unfolded, because that site
// called forcedEpochLive and never named this function at all.
//
// The check that works is over the TERM, not over this function's name:
//
//	grep -rn 'forcedEpochLive(' --include='*.go' server/ | grep -v _test.go
//
// Every hit must be one of three things: forcedEpochLive's own declaration, this
// function's own body, or a site carrying the grep anchor STOP-EPOCH-TERM-AUDIT
// together with a written reason for asking the forced term WITHOUT the
// stopping_since term. Today exactly two sites are anchored that way —
// stopEpochAnchor below, and winddownStageOf in member_ownerop_winddown.go. Any
// OTHER hit is a hand-written copy of this compound, and either belongs in this
// function or owes the anchor and a reason.
//
// Those spellings used to be spellings of one judgement, and some of them were
// the negation of the others, which is how a reader checks them against each
// other and gets it wrong. TestOffboardKindOf_AFinalCallAlwaysHasAClock asserts
// the sentence and the clock coincide — it asserted the AGREEMENT of two copies
// because that was all it could do; they are now one expression.
//
// It is NOT the same question as "may this 停止 re-stamp stopping_since"
// (stopEpochAnchor): that one has no stopping_since>0 term at all, because
// opening the FIRST stop epoch is exactly the case where there is none yet.
func gracefulStopEpochOpen(m Member) bool {
	return m.StoppingSince > 0.0 && !forcedEpochLive(m)
}

// stopEpochAnchor answers what stopping_since must hold after a 停止 lands on
// this row: NOW for an ordinary stop, and the value it already carries when the
// epoch under way is a live FORCED one.
//
// 🔴 RE-STAMPING A FORCED EPOCH WOULD MOVE IT TO THE GRACEFUL SIDE. forcedEpochLive
// is scoped by `forced_stop_at >= stopping_since`, so pushing stopping_since to
// now — with forced_stop_at left where it was — flips the row out of "cut off
// deliberately" and into "working its close-out", and the arm that must stay
// silent becomes the arm that speaks (the ruling in offboardKindOf above, and
// T-c996 for why it binds both kinds).
//
// STOP-EPOCH-TERM-AUDIT: this asks forcedEpochLive WITHOUT a stopping_since > 0
// term, and that is the point — opening the FIRST stop epoch is exactly the case
// where there is no anchor yet, so it is not gracefulStopEpochOpen's compound
// and must not be folded into it.
//
// The two faces that stamp a stop epoch are the staff deactivate and the worker
// /stop. Since T-65 包③ they no longer write it out identically — they reach
// this function through ONE body, applyStopVerbRow, which is where the rest of
// 停止's row writes now live too. It still takes and returns a VALUE rather
// than mutating, because the worker face holds an OutsourceWorker and only
// projects a Member to ask the question — the answer goes back onto the worker
// row, not onto the projection.
func stopEpochAnchor(m Member, now float64) float64 {
	if forcedEpochLive(m) {
		return m.StoppingSince
	}
	return now
}

// offboardNoticeFor is the WHOLE wind-down sentence for this member: the
// document its arm reads, plus the manual write-back clause when the member has
// one.
//
// 🔴 IT USED TO RESOLVE {where} TOO — this session's own position — and that is
// gone (T-6f44, owner's decision 4). What it composed was a clause like
// 「context 55% (your limits: 55% / 65%)」, and the finding that removed it is
// first-hand rather than argued: an agent received exactly that, read it, and
// closed out no differently for it. Being told where you are is not being told
// what to do, and this sentence is the one an agent acts on.
//
// The gauge read and the two arms of English formatting went with it. Keeping
// them behind a discarded argument would have left a live-looking computation
// nothing could observe — including its own regression tests, which would have
// gone on passing while the value never reached anybody.
func (s *apiServer) offboardNoticeFor(m Member, kind string) string {
	notice := s.winddownNoticeText(kind, winddownDeadlineOf(m, s.reconcileConfigLive()))
	// A notice that could not be rendered is not sent at all — the caller omits
	// the key, and the agent's client falls back. Appending the write-back
	// clause to "" would send that clause on its own, with no sequence under it.
	if notice == "" {
		return ""
	}
	if clause := s.offboardManualWriteBackFor(m); clause != "" {
		notice += "\n\n" + clause
	}
	return notice
}

// offboardManualWriteBackFor resolves the 記憶回寫 clause for THIS member: the
// worker's bound task decides whether there is a 手冊 to write back into, and
// offboardManualWriteBack composes the sentence.
//
// 🔴 OUTSOURCE ONLY, deliberately. A 正職 has a role of its own and its learnings
// may belong to the ROLE rather than to any one task's type — which document a
// staff member writes into is ruled by the boot doc's 「記憶與學習」 section, and
// naming one document here would overrule it from the wrong place. A worker has
// no role and outlives nothing: its one task IS its memory, which is why the
// owner's ruling names 外包 and this gate does too.
//
// Best-effort by construction: an unreadable task row, a task with no type, or a
// deleted manual all fall back to saying LESS (no clause, or the bare key as the
// label) — never to blocking the 預告, which is the message that actually has to
// arrive.
func (s *apiServer) offboardManualWriteBackFor(m Member) string {
	if m.Kind != KindOutsource || m.LinkedTaskID == nil || *m.LinkedTaskID == "" {
		return ""
	}
	t, err := s.dal.GetTask(*m.LinkedTaskID)
	if err != nil || t == nil {
		return ""
	}
	// 🔴 AN AD-HOC TASK HAS NO MANUAL, SO IT IS ASKED FOR NOTHING. This gate is
	// NOT a policy of T-6f44 — it is the criterion the tree already carried
	// (decideTaskCloseNudge stays silent on an empty type for the same reason),
	// and it survived the rewrite below on purpose. Sending the close-out text
	// anyway would point a worker at a 任務手冊 that does not exist, on the one
	// path where it has no way to answer back.
	//
	// It reads type_key here even though the DOCUMENT no longer interpolates it:
	// the question this gate asks ("is there a manual at all?") is not the
	// question the document asks ("which manual, agent — go read the ticket").
	if t.TypeKey == "" {
		return ""
	}
	// 🔴 THE DOCUMENT, NOT A SECOND COPY OF IT (T-6f44, owner's decision 6).
	// This used to be a Go string that said almost exactly what 〈任務結案〉 says
	// — hardcoded, unversioned, and the one text of the pair the owner could not
	// edit. Two texts for one rule is how one of them silently loses a clause,
	// and the one in code is always the one that loses it unnoticed.
	//
	// It needs no variable beyond the ticket number any more: the document now
	// tells the agent to read the task and take type_key off it, which is
	// exactly the lookup this function used to perform on the agent's behalf.
	// So the manual read is gone with the string — nothing here has to know
	// which manual it is for the agent to be told.
	// The BODY only — see taskEventBodyText. This worker is being wound down;
	// its ticket has NOT necessarily ended, so the document's opening sentence
	// (「任務 {task_no} 已結束。」) is a claim this path cannot make. The body says
	// what to do and names no task, and the worker has exactly one, so 「這張票」
	// is unambiguous to the only reader that gets this.
	return s.taskEventBodyText(docKindTaskCloseout)
}

// resolveAvatarMember admits active staff and outsource rows but rejects
// wardens: a machine is infrastructure, not a person with a visual identity.
func (s *apiServer) resolveAvatarMember(memberID string) (*Member, error) {
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved {
		return nil, errNotFound
	}
	return m, nil
}

func (s *apiServer) publishMemberAvatarChanged(m Member, trigger string) {
	if m.Kind == KindOutsource {
		s.publishOutsourceWorker(workerFromMember(m), trigger)
		return
	}
	s.hub.Publish("member", "patch", "member", wireOwnerID+"::"+m.ID,
		s.offboardDeltaPayload(m), audienceMembers(m.ID), trigger)
}

func memberAvatarResult(m Member, mime string, filename *string) MemberAvatarDTO {
	url := memberAvatarURL(m.AvatarAttachmentID)
	result := MemberAvatarDTO{MemberId: m.ID, AvatarUrl: &url}
	if mime != "" {
		result.Mime = &mime
	}
	result.Filename = filename
	return result
}

// PUT /api/members/{member_id}/avatar — raw raster bytes, owner-only at the
// route table. A fresh ava- id makes every replacement cache-safe.
func (s *apiServer) HandlePutMemberAvatarApiMembersMemberIdAvatarPut(
	w http.ResponseWriter,
	r *http.Request,
	memberID string,
	params HandlePutMemberAvatarApiMembersMemberIdAvatarPutParams,
) {
	m, err := s.resolveAvatarMember(memberID)
	if err != nil {
		writeResolveError(w, err, "member", memberID)
		return
	}
	if m.Kind == KindWarden {
		writeError(w, http.StatusUnprocessableEntity, "a machine cannot have a personal avatar")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAvatarBytes+1))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not read avatar image")
		return
	}
	if len(raw) > maxAvatarBytes {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("avatar image is too large (max %d bytes)", maxAvatarBytes))
		return
	}
	if len(raw) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "avatar image is empty")
		return
	}
	actualMime := sniffAttachmentMime(raw)
	if _, ok := avatarMimeMagic[actualMime]; !ok {
		writeError(w, http.StatusUnprocessableEntity,
			"avatar must be PNG, JPEG, or WEBP raster bytes")
		return
	}
	if params.Mime != nil {
		declared := strings.TrimSpace(*params.Mime)
		if declared != "" && declared != actualMime {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("avatar mime %q does not match image bytes %q", declared, actualMime))
			return
		}
	}
	var filename *string
	if params.Filename != nil {
		trimmed := strings.TrimSpace(*params.Filename)
		if trimmed != "" {
			filename = &trimmed
		}
	}
	avatar := ChatAttachment{
		ID:       "ava-" + newHexID(12),
		Mime:     actualMime,
		Data:     raw,
		Filename: filename,
	}
	if err := s.dal.ReplaceMemberAvatar(m.ID, avatar); err != nil {
		internalError(w, err)
		return
	}
	m.AvatarAttachmentID = avatar.ID
	s.publishMemberAvatarChanged(*m, requestTrigger(r))
	writeJSON(w, http.StatusOK, memberAvatarResult(*m, actualMime, filename))
}

// DELETE /api/members/{member_id}/avatar — idempotent fallback restoration.
func (s *apiServer) HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete(
	w http.ResponseWriter,
	r *http.Request,
	memberID string,
) {
	m, err := s.resolveAvatarMember(memberID)
	if err != nil {
		writeResolveError(w, err, "member", memberID)
		return
	}
	if m.Kind == KindWarden {
		writeError(w, http.StatusUnprocessableEntity, "a machine cannot have a personal avatar")
		return
	}
	if err := s.dal.DeleteMemberAvatar(m.ID); err != nil {
		internalError(w, err)
		return
	}
	m.AvatarAttachmentID = ""
	s.publishMemberAvatarChanged(*m, requestTrigger(r))
	writeJSON(w, http.StatusOK, memberAvatarResult(*m, "", nil))
}

// GET /api/members — the roster (soft-removed rows omitted). online is the
// live SSE projection; machine the OBSERVED position; unread_count the pure
// inverse of the caller's chat_read watermark.
//
// ?fields=light (T-cf91) is the ADDITIVE identity-only projection for surfaces
// that render ONLY a member's name + role (the 請示卡頁 attributes each card to
// its asker and needs nothing else). It SKIPS the per-member unread count
// (UnreadCountsFor — one SQL aggregate since T-48, a whole-table scan folded in
// Go before that) and the per-member presence / observed-host derivation
// (hub + telemetry lookups) —
// none of which the name/role view reads. The light DTO keeps the SAME
// memberDTO wire shape (no new response schema — additive), but the fields
// those skipped computations feed are HONEST-EMPTY: unread_count 0, presence
// "", machine "", last_op* untouched-from-row. A consumer must not read those
// off a light response — the value is "not computed", not "known zero". The
// default (no fields param, or any value other than "light") is byte-for-byte
// the full roster as before. This mirrors the roster hook's matching change:
// the light consumer also stops treating chat SSE deltas as a roster refetch
// trigger (a message never changes a name or role), so a company-wide chat
// line no longer re-pulls this endpoint at all.
func (s *apiServer) HandleListMembersApiMembersGet(w http.ResponseWriter, r *http.Request, params HandleListMembersApiMembersGetParams) {
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	light := trimmedOrEmpty(params.Fields) == "light"

	// unread compares each sender's messages against the caller's chat_read
	// watermark — one SQL aggregate (T-48; it used to be the whole chat table
	// folded in Go), and still the most expensive part of this handler, which is
	// exactly what the light projection exists to avoid. Only on the full path.
	var unread map[string]int
	if !light {
		var err error
		// The SAME computation the single-member handler runs (api_helpers.go) —
		// one field, one answer, whichever endpoint you ask.
		unread, err = s.unreadCountsForRequest(r)
		if err != nil {
			internalError(w, err)
			return
		}
	}

	out := []memberDTO{}
	for _, m := range members {
		if m.RosterStatus == RosterStatusRemoved {
			continue
		}
		roleName, err := s.memberRoleName(m)
		if err != nil {
			internalError(w, err)
			return
		}
		if light {
			out = append(out, s.newMemberLightDTO(m, roleName))
			continue
		}
		out = append(out, s.newMemberDTO(m, roleName, s.observedHost(m), unread[m.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/members — hire. The server mints the id; a blank name is 422. A
// body carrying kind/role_key is PRIVILEGE-BEARING (warden = machine
// principal, assistant = admin principal) and demands an admin_agent caller.
func (s *apiServer) HandleHireMemberApiMembersPost(w http.ResponseWriter, r *http.Request) {
	var body MemberHireDTO
	if !decodeJSONBodyRequired(w, r, &body, "name") {
		return
	}
	name := trimString(body.Name)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "member requires a name")
		return
	}
	privileged := trimmedOrEmpty(body.Kind) != "" || trimmedOrEmpty(body.RoleKey) != ""
	if privileged && !principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		writeError(w, http.StatusForbidden,
			"hiring with kind/role_key is privilege-bearing; "+
				"it requires an owner or an admin-role caller")
		return
	}
	if body.Effort != nil && !validEffort(*body.Effort) {
		writeError(w, http.StatusUnprocessableEntity,
			"effort must be one of [high low max medium]; got '"+*body.Effort+"'")
		return
	}
	// UNSET when the caller names none. The empty runtime is the durable
	// "nobody has picked yet" (T-b3d0), and it is what lets
	// resolveEmptyRuntimeForPlacement choose against the machine this member is
	// actually placed on — a codex-only box hires a Codex member. Hard-coding
	// claude here made that resolver unreachable from every creation path.
	runtime := ""
	if body.Runtime != nil {
		runtime = string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
	}
	// The Go kind is a CLOSED set: the Python bare hire's kind="" folds to
	// "staff" at this ingest seam (CanonicalKind — owner-approved mapping);
	// a kind outside the closed set is refused.
	kind, err := CanonicalKind(strOrEmpty(body.Kind))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	effort := strOrEmpty(body.Effort)
	if effort == "" {
		effort = "medium"
	}
	m := Member{
		ID:               "m-" + newHexID(12),
		Name:             name,
		Kind:             kind,
		RoleKey:          strOrEmpty(body.RoleKey),
		Runtime:          runtime,
		Model:            strOrEmpty(body.Model),
		Effort:           effort,
		DesiredState:     DesiredStateOffline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}
	if err := s.putMember(m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, m)
}

// GET /api/members/{member_id} — one roster member (removed → 404); machine
// is the OBSERVED position. SELF-READ exception (T-ea82): an outsource worker
// reading its OWN row (memberId == the verified sub) resolves — the ocagent
// recycle/wind-down hooks refetch GET /api/members/<self> and must see the
// worker's desired_state/refocus_since. Since 2026-08-28 the item door is
// anyMember, so an ow- target resolves for ANY caller — the self-read branch
// below is now only the fallback for a row this scope cannot see.
func (s *apiServer) HandleGetMemberApiMembersMemberIdGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, anyMember)
	if errors.Is(err, errNotFound) && memberId == currentActor(r) {
		m, err = s.resolveSelf(r)
	}
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	// unread_count is COMPUTED here, exactly as the list computes it. Handing
	// newMemberDTO a literal 0 (what this line used to do) made the roster badge
	// a one-way ratchet: the cockpit re-reads one member on a chat delta, so the
	// badge the delta was announcing was zeroed instead of raised. Pinned by
	// api_members_unread_parity_test.go.
	unread, err := s.unreadCountsForRequest(r)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.newMemberDTO(*m, roleName, s.observedHost(*m), unread[m.ID]))
}

// PATCH /api/members/{member_id} — partial edit (name/runtime/model/effort).
// Blank name or an unknown runtime/effort is rejected.
func (s *apiServer) HandleUpdateMemberApiMembersMemberIdPatch(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if body.Name != nil {
		name := trimString(*body.Name)
		if name == "" {
			writeError(w, http.StatusUnprocessableEntity, "member name cannot be blank")
			return
		}
		m.Name = name
	}
	// The three LAUNCH INTENTS are tracked separately from the display name:
	// only they are baked into a boot frame, so only they can be stale in a
	// running session — renaming a member must never recycle it.
	launchIntentChanged := false
	if body.Model != nil {
		launchIntentChanged = launchIntentChanged || *body.Model != m.Model
		m.Model = *body.Model
	}
	if body.Runtime != nil {
		runtime := string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
		// COMPARE WHAT THE WIRE CARRIES, NOT WHAT THE COLUMN HOLDS. An unset
		// runtime is not a third value: NormalizeRuntime("") is claude, that is
		// what buildStartFrame stamps, and that is what the running session is
		// already on. A raw `runtime != m.Runtime` reads "" as different from
		// "claude" and charges the owner a wind-down + 重新聚焦 for a save that
		// changes nothing the agent could observe.
		//
		// THIS PACKAGE OPENED THAT READING. An earlier draft of this comment
		// claimed the defect predates T-b3d0; it does not. PutMember used to
		// bind NormalizeRuntime(m.Runtime), so every persisted row held a
		// concrete "claude" — the out-of-box assistant's included — and the raw
		// comparison never fired spuriously. The commit before this one is what
		// stopped normalizing on the way in, so that "nobody has picked yet"
		// stays distinguishable from "the owner picked claude"; that is what
		// makes "" durable, and therefore what makes this comparison wrong. The
		// damage is repaired inside the package that created it.
		//
		// seedOutOfBox never writing a runtime is true but does not carry the
		// claim on its own: it only covers what the seed literal contains, not
		// what the write path then stored. Rows that already exist keep
		// whatever is on disk (installs running the released code are on
		// "claude"); the rows that sit on "" are the ones written from here on
		// — a fresh seed, or a member dispatched before its machine has ever
		// reported capabilities (resolveEmptyRuntimeForPlacement leaves it
		// unset there by design). For those, the first owner to open
		// 成員設定 and press 儲存 on the runtime she was already running
		// would pay a recycle for a no-op edit.
		//
		// The WRITE below still lands: "" -> "claude" is a real intent the owner
		// stated, and persisting it stops placement from resolving that member
		// against some other machine later. Only the recycle is withheld.
		launchIntentChanged = launchIntentChanged ||
			NormalizeRuntime(runtime) != NormalizeRuntime(m.Runtime)
		m.Runtime = runtime
	}
	if body.Effort != nil {
		if !validEffort(*body.Effort) {
			writeError(w, http.StatusUnprocessableEntity,
				"effort must be one of [high low max medium]; got '"+*body.Effort+"'")
			return
		}
		launchIntentChanged = launchIntentChanged || *body.Effort != m.Effort
		m.Effort = *body.Effort
	}
	// T-b6d9: a launch intent used to be written and then simply ignored by the
	// live session — the owner pressed 儲存, got a 200, and the member went on
	// running the OLD model until something unrelated respawned it. Now the
	// change opens the SAME graceful wind-down 重新聚焦 has always had, so the
	// member finishes what it was doing and comes back on the new value.
	//
	// ⚠️ SINCE T-55 THE EPOCH AND THE NEW VALUE NO LONGER LAND TOGETHER. This
	// was one write, so they could not land apart; the three launch intents have
	// since left PutMember's SET list and land through their own setters, which
	// run AFTER the whole-row write below. Only that order converges on a partial
	// failure — the 🔴 block sitting above those setters is the whole argument,
	// and it is the one to read. Do not restore the claim that was here.
	heldDown := false
	if launchIntentChanged {
		// A member the owner has stopped takes the new value on its next 活化 and
		// NOTHING happens now — which is right, and used to be indistinguishable
		// from "it took effect".
		//
		// ⚠️ THE RECEIPT NO LONGER RIDES THE putMember BELOW EITHER. The sentence
		// here was corrected once already in T-55's first batch, to say the
		// EXPLANATION still travelled in one write even though the value no longer
		// did; the second batch moved the receipt columns out too, so both halves
		// are now separate writes and the write below carries neither. The stamp
		// is stored by the dal.SetMemberOpReceipt call after it — which must run
		// BEFORE the launch-intent setters, for the reason spelled out there.
		heldDown = !s.armMemberOwnerOpHandover(m, memberOpModel) &&
			m.DesiredState == DesiredStateOffline
		if heldDown {
			// 下線 → 重啟 (T-14 項目 7), the 換 model face of the same ruling —
			// but only for a member that has ever been asked to stop
			// (aStopWasEverAskedFor — a never-活化'd new hire is not one).
			if aStopWasEverAskedFor(*m) {
				stampRestartIntent(m)
				stampMemberOpReceipt(m, memberRestartQueuedReceipt(memberOpModel), nowSecs())
			} else {
				stampMemberOpReceipt(m, memberHeldDownReceipt(memberOpModel), nowSecs())
			}
		}
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// 🔴 THE RECEIPT NO LONGER RIDES THE WRITE ABOVE (T-55), AND IT MUST LAND
	// BEFORE THE LAUNCH-INTENT SETTERS BELOW — this is the one ordering on this
	// handler that a retry cannot repair if you get it wrong.
	//
	// The gate for the whole block is launchIntentChanged, which compares the
	// request against the STORED launch intent. Put this write after the setters
	// and a failure here leaves the new model on the row with no receipt — and
	// the owner's retry then finds nothing changed, takes neither branch, and
	// never stamps the explanation. The member would sit held-down with the
	// cockpit saying nothing about why, permanently. Before the setters, the
	// stored value still differs, so the retry re-enters this branch and stamps.
	//
	// Same shape as T-b6d9, one door along: what makes a retry work here is that
	// the thing the gate reads has not moved yet.
	//
	// Gated on heldDown because only that branch wrote a receipt. Persisting
	// unconditionally would push this handler's SNAPSHOT of the five columns back
	// over whatever a reconcile tick stamped meanwhile — the exact clobber this
	// ticket removes, re-introduced through the fix for it.
	if heldDown {
		if err := s.persistMemberOpReceipt(*m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
	}
	// The three launch intents left PutMember's DO UPDATE SET in T-55, so the
	// write above no longer carries them — their sole writers do, and ONLY for a
	// field this request actually carried. That asymmetry is the point: a save
	// that touches effort alone must not restate the model beside it, because the
	// model it would restate is the one this handler read before the owner-op
	// wind-down (or another face) touched the row.
	//
	// 🔴 THEY RUN AFTER THE WHOLE-ROW WRITE, AND THE ORDER IS THE WHOLE POINT.
	// What used to be one write is now two, so one of them can fail alone — and
	// the two orders fail differently. The wind-down epoch armed above is what
	// makes a launch-intent change TAKE EFFECT (T-b6d9: without it the member
	// runs on the old model until something unrelated respawns it), and it lands
	// EARLIER STILL: the four anchors it is made of left the whole-row write in
	// T-55's third batch, so persistMemberWindDownAnchors carries it and runs
	// ahead of that write, not with it. (This sentence said "with the whole-row
	// write" until that batch, which was true when it was written and is the
	// third time one line here has gone stale as the columns moved — the
	// standing answer to "which writer carries what" is
	// singleColumnOwnedFields, not a sentence.) Put the setters first and a failure here leaves
	// the new model stored with NO epoch — the exact bug T-b6d9 fixed, arriving
	// through a different door, and nothing ever converges it. This way round, a
	// failure leaves the epoch open with the OLD value: the member winds down and
	// comes back on what it was already running — one wasted recycle, and the
	// retry still sees a difference.
	//
	// ⚠️ The 500 is NOT "nothing was stored": the whole-row write above has
	// already landed, so a `name` carried by the SAME request is saved even
	// though the launch intent is not. What the failure guarantees is only that
	// the launch intent did not land — which is what makes the retry work.
	//
	// relocate and activate are deliberately NOT reordered this way. Their
	// wind-down is unconditional (relocate always arms one, activate always
	// force-revives), so their retry always re-dispatches and the residue
	// converges on its own. Only the launch-intent faces gate the wind-down on
	// "the value actually changed", and that gate is what a value landing early
	// closes.
	if body.Model != nil {
		if err := s.dal.SetMemberModel(m.ID, m.Model); err != nil {
			internalError(w, err)
			return
		}
	}
	if body.Runtime != nil {
		if err := s.dal.SetMemberRuntime(m.ID, m.Runtime); err != nil {
			internalError(w, err)
			return
		}
	}
	if body.Effort != nil {
		if err := s.dal.SetMemberEffort(m.ID, m.Effort); err != nil {
			internalError(w, err)
			return
		}
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/activate — write desired_state=online intent.
// ALWAYS FORCE-REVIVE: both winding-down anchors clear unconditionally.
func (s *apiServer) HandleActivateMemberApiMembersMemberIdActivatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberActivateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	// The machine bind is held to the SAME rule as every other placement write
	// face: any non-blank id must name a real machine. activate is the one that
	// most needs it — it flips desired_state online in the same call, so an
	// unreachable pin here manufactures exactly the "wants to be online, can
	// never be dispatched, never heals" member this validation exists to prevent.
	// "" still clears the pin (the member then waits for a placement).
	if body.MachineId != nil && *body.MachineId != "" {
		if _, err := s.resolveMachine(*body.MachineId); err != nil {
			writeResolveError(w, err, "machine", *body.MachineId)
			return
		}
	}
	m.StoppingSince = 0.0
	m.WakingSince = 0.0
	m.DesiredState = DesiredStateOnline
	// 活化 is 「要不要起來」 answered directly, so the queued answer is spent
	// rather than left behind to fire a second start after the next 下線.
	clearRestartIntent(m)
	if body.MachineId != nil {
		m.DesiredMachineID = *body.MachineId
		// desired_machine_id left PutMember's SET list in T-55: the pin moves
		// through its sole writer, and an activate that carries NO machine_id
		// now genuinely leaves it alone instead of writing back the value this
		// handler read — which is what used to undo a relocate that landed in
		// between.
		if err := s.dal.SetMemberDesiredMachineID(m.ID, m.DesiredMachineID); err != nil {
			internalError(w, err)
			return
		}
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven reconcile (the Python _dispatch_reconcile_now click seam):
	// decide + dispatch the START NOW, not on a later tick; the shared
	// reconcile store makes the cadence an idempotent backstop. The intent is
	// already persisted so the activate never FAILS on dispatch — but we OBSERVE
	// it (T-ba62): a decided START the warden could not accept (machine offline,
	// warden never installed, warden's SSE down) surfaces activation_pending=true,
	// exactly as relocate has reported relocation_pending since T-8655. Dropping
	// this return value was the whole bug: an activate against an unreachable
	// warden answered a clean 200 with zero signal, so "waking" and "nothing was
	// dispatched and nothing will be until the next cadence tick" looked identical.
	dec := s.reconcileMemberNow(m.ID)
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	dto := s.newMemberDTO(*m, roleName, "", 0)
	// POSITIVE determination (T-ba62 review R4), not a list of known failures.
	// `dec.DispatchUnlanded` alone was wrong: reconcileOne ALSO downgrades a
	// START to none when buildStartFrame cannot assemble a payload (missing
	// persona / token) and does NOT set DispatchUnlanded there — so a reachable
	// warden plus an unbuildable frame answered a clean 200 with no pending flag
	// and nothing dispatched. Ask instead whether a START actually went out; an
	// already-online member needs none, and every other outcome — backoff,
	// circuit-open, and failure modes not yet invented — is honestly "nothing
	// has been dispatched yet".
	if dec.Command != reconcileCmdStart && !s.hub.IsOnline(m.ID) {
		pending := true
		dto.ActivationPending = &pending
		// …and WHICH pending (T-ed79 #14). The flag is one bit and the comment
		// above lists at least four states that reach it. The tick has already
		// decided which one it is; stamp that on the row so the cockpit can say it
		// instead of showing a pending badge with nothing behind it. An arm that
		// named no code falls back to the generic "asked, nothing dispatched yet",
		// which is still strictly more than the blank it replaces — and is
		// deliberately NOT invented per-arm here: a new stall arm should name
		// itself at the decision site, not be guessed at from this end.
		reason := dec.ReasonCode
		if reason == "" {
			reason = spawnReasonWardenLost + ": 活化 was recorded, but nothing has been " +
				"dispatched yet — the machine's warden did not take the start. It will " +
				"be retried; if it stays here, check that machine"
		}
		s.stampMemberOpBlocked(m.ID, reason, nowSecs())
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/members/{member_id}/relocate — the owner cockpit's 改機器 for a roster
// member (admin-gated, route Requires=admin_agent — parity with the member
// lifecycle family). The member twin of the outsource-worker relocate: write the
// owner-pinned desired_machine_id, then run the SAME event-driven reconcile the
// activate click uses (reconcileMemberNow). A LIVE member is auto-migrated onto
// the chosen machine, but SINCE T-b6d9 GRACEFULLY: the pin is written together
// with a refocus epoch, the agent gets the ordinary 〈停止〉 wake, and
// the kill+re-spawn happens at the 收口 — which since T-ed79 is its own
// report_stopped or the owner's force-stop, and NOTHING ELSE. 🔴 A relocate has
// no RecycleGrace ceiling any more: winddownKindFor answers soft for it, so
// recycleGraceFor answers "no clock" and the recycle arm never times it out.
// This line used to name that ceiling — a window an owner would wait out and
// that never closes. It used to be an immediate robust STOP with no
// warning at all (fbc5280). An offline member opens no epoch — there is nothing to
// wind down — and since T-14 項目 7 it splits two ways: one that has been STOPPED
// at some point queues the owner's 「起來」 (restart_after_stop) and comes back up
// on the new pin, while one that has never been asked to stop (a new hire before
// its first 活化) just re-pins and waits, held_down. 🔴 THE OLD SENTENCE HERE —
// "PLACEMENT ONLY — unlike activate it NEVER touches desired_state" — is now true
// only of THIS handler's own write: it still never sets desired_state itself, but
// the queued intent it records makes the reconcile flip it at the converged-
// offline edge, so the member does end up woken. The activate contrast that
// remains is CANCELLATION vs QUEUEING: 活化 tears the stop down on the spot,
// 改機器 gets in line behind it. 404 for an unknown / removed member; any non-"" machine_id that names no
// real machine is a 404, so a stale/typo'd id never pins the member to a
// placement that can never boot (the worker-relocate reasoning). machine_id is
// REQUIRED since owner 2026-07-27 (relocateNeedsMachineMsg): an absent key is a
// 422 and an explicit null / "" is a 400 — a relocate names a destination and no
// longer doubles as an unpin. The literal "auto" is NOT exempt from the resolve: it used to be
// waved through as a pseudo-machine, which pinned the member to a destination
// dispatch could never reach (IsOnline("auto") is always false) and reconcile
// never healed — the very hole a nonexistent concrete id was already 404'd for.
func (s *apiServer) HandleRelocateMemberApiMembersMemberIdRelocatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberRelocateDTO
	if !decodeJSONBodyRequired(w, r, &body, "machine_id") {
		return
	}
	if body.MachineId == "" {
		writeError(w, http.StatusBadRequest, relocateNeedsMachineMsg)
		return
	}
	machineID := body.MachineId
	if _, err := s.resolveMachine(machineID); err != nil {
		writeResolveError(w, err, "machine", machineID)
		return
	}
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		// P7c (gate rc-2786636f30e5, 外包對齊正職): the tool's semantics are "move
		// one agent" — an id that names no STAFF member falls through to the
		// outsource projection, so an admin agent's MCP relocate_member moves a
		// worker with the same verb. Since the P7d fold both live in the member
		// table, but resolveMember deliberately excludes kind='outsource', so
		// an ow- id still routes HERE — onto the worker relocate core (worker
		// spawn machinery), never the member reconcile path. The id namespaces
		// stay disjoint ("m-…"/named roster ids vs "ow-…"), so no shadowing.
		if errors.Is(err, errNotFound) {
			if worker, werr := s.dal.GetOutsourceWorker(memberId); werr == nil &&
				worker != nil && worker.Status != WorkerStatusReleased {
				s.relocateWorkerByID(w, r, memberId, machineID)
				return
			}
		}
		writeResolveError(w, err, "member", memberId)
		return
	}
	// The placement pin is the only INTENT mutation — desired_state is
	// deliberately left untouched (the activate contrast).
	m.DesiredMachineID = machineID
	// The pin itself lands through its sole writer (T-55); PutMember no longer
	// carries the column, so the whole-row write below moves only the wind-down
	// anchors and the receipt.
	if err := s.dal.SetMemberDesiredMachineID(m.ID, machineID); err != nil {
		internalError(w, err)
		return
	}
	// T-b6d9: a LIVE member used to be robust-STOPped on the spot by the
	// reconcile below — no 預告, no grace, not even a stopping_since, so it just
	// vanished from the cockpit with whatever it was mid-way through. It now
	// gets the same wind-down 重新聚焦 has always had; the winding-down anchors
	// ARE written in that case, and only in that case.
	//
	// ⚠️ THE PIN NO LONGER RIDES THIS WRITE (T-55) — it landed through its sole
	// writer a few lines above, BEFORE the epoch. That is the REVERSE of the
	// launch-intent faces, and deliberately so: relocate arms its wind-down
	// unconditionally, so nothing here gates on "the value actually changed" and
	// a retry re-dispatches regardless — the residue converges on its own. The
	// delta the agent wakes on still names the destination, because the pin is
	// already on the row by the time this write fans it.
	windDown := s.armMemberOwnerOpHandover(m, memberOpRelocate)
	// Held down: the pin is stored and nothing is moved. Same receipt as the
	// model face — see memberHeldDownReceipt.
	heldDown := !windDown && m.DesiredState == DesiredStateOffline
	if heldDown {
		// 下線 → 重啟 (T-14 項目 7). Owner 2026-08-30: 「change model / machine
		// 只是帶起來的方式不一樣而已」 — 改機器 is a 重啟 intent, so the pin is
		// no longer stored and forgotten; the member comes back up on it, whether
		// the stop is still landing or landed a week ago. ⚠️ That second half is
		// a REAL change to the placement-only contract this handler's header
		// still describes for the stopped case; see aStopWasEverAskedFor.
		if aStopWasEverAskedFor(*m) {
			stampRestartIntent(m)
			stampMemberOpReceipt(m, memberRestartQueuedReceipt(memberOpRelocate), nowSecs())
		} else {
			stampMemberOpReceipt(m, memberHeldDownReceipt(memberOpRelocate), nowSecs())
		}
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// The receipt left the write above (T-55) and now lands through its sole
	// writer. Unlike the model face, the ORDER here carries no convergence
	// argument: this handler's wind-down is armed unconditionally, so nothing
	// gates on a stored value that an early landing could close — a retry
	// re-enters this branch whatever happened. Gated on heldDown for the reason
	// the model face gives: persisting unconditionally would push this handler's
	// snapshot of the five columns over whatever a tick stamped meanwhile.
	if heldDown {
		if err := s.persistMemberOpReceipt(*m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
	}
	// Event-driven reconcile: with a wind-down open this decides "awaiting agent
	// dump" and dispatches NOTHING (the 收口 owns the move); it still runs so the
	// tick state is advanced here rather than up to 30s later, and it remains the
	// path that migrates a member nobody stamped for (the decideUp relocate arm,
	// now a backstop — which since T-14 #4 opens this same wind-down itself
	// instead of robust-STOPping on its first pass, so the two doors behave
	// alike; it still kills at once only for a row no epoch can be stamped on). An offline member is a no-op here (nothing to move). The
	// pin is already persisted so the relocate never FAILS on dispatch — but we
	// OBSERVE it: a decided recycle STOP / START that the warden could not accept
	// (old/new machine unreachable) surfaces relocation_pending=true, so the
	// caller sees "move scheduled, not yet landed" instead of a silent 200
	// success (T-8655). The cadence retries the pinned move regardless.
	dec := s.reconcileMemberNow(m.ID)
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	dto := s.newMemberDTO(*m, roleName, "", 0)
	// relocation_pending means what it has always meant — "move scheduled, not
	// yet landed". T-b6d9 adds a SECOND way to be in that state: a wind-down was
	// opened, so nothing has been dispatched YET and the member is still on the
	// old machine until the 收口. Reporting a clean landed 200 there would be the
	// same silent false-success T-8655 removed for the unreachable-warden case.
	if dec.DispatchUnlanded || windDown {
		pending := true
		dto.RelocationPending = &pending
	}
	// …and WHICH of the two it is (T-927a). The wind-down case is a deliberate
	// deferral, not a delivery failure, so the caller must be able to hold back
	// the "nothing was dispatched" alert for it. Reported separately rather than
	// by narrowing relocation_pending: that field's meaning is on the frozen
	// wire and existing readers depend on it covering both.
	if windDown {
		deferred := true
		dto.RelocationDeferred = &deferred
	}
	writeJSON(w, http.StatusOK, dto)
}

// memberHeldDownReceipt is the sentence a staff owner-verb leaves on the row when
// it was SAVED and nothing was started, because the owner has this member held
// down (T-ed79 #4 / #14). It is the worker receipt's twin, verb-for-verb:
// respawnWorkerForOwnerOp has written exactly this for 改機器 / 換 model / 重啟
// since the reason-code family landed, and staff answered a clean 200 with an
// empty row.
//
// 🔴 WHY THE STATE NEEDS A NAME AT ALL. Three different situations reach the
// same silent 200 on these handlers — the owner pressed 停止 (this one), the
// member is simply offline, and this epoch's wind-down was already collected —
// and the owner has no way to tell them apart. Only the FIRST is one his own
// earlier action caused, and it is the only one a receipt can resolve for him
// ("重啟 it when you want it to run"). The other two are not stalls: an offline
// member picks the value up at its next wake, which is the ordinary story, and
// stamping a receipt for it would be noise on every edit made while a member is
// asleep.
func memberHeldDownReceipt(op string) string {
	return spawnReasonHeldDown + ": the " + op + " was saved, but nothing was " +
		"started — this member is stopped; 活化 it when you want it to run"
}

// clearMemberHandoverMarker zeroes the 換手 epoch a staff STOP has just made
// meaningless — the worker /stop's two lines, given a name (T-ed79 parity #9).
// Both staff stop verbs call it; the two reasons are different and both are
// worker-verbatim:
//
//   - 下線: a wind-down is a request to finish and come BACK. An explicit 停止
//     says no session follows, so there is nothing left to hand over to. The
//     epoch is not superseded, it is answered.
//   - 強制停止: the session is being cut off. Nothing is being waited for.
//
// 🔴 THE HARM IT REMOVES IS NOT ON THIS ROW, IT IS ON THE NEXT ONE. Neither stop
// verb is what reads refocus_since — decideDown owns a desired-offline member and
// never looks. The reader is the GENERATION AFTER: activate clears stopping_since
// and waking_since and deliberately clears NEITHER refocus_since nor
// stopped_since, so a marker left here survives 下線 → 活化 intact, and decideUp's
// recycle arm then reads "marker present, dump done" and robust-stops the
// brand-new session on its first tick — zero grace, no close-out, for an epoch
// that ended before it was born. armRefocusEpoch already describes this exact
// destructive reader; what was missing was anybody clearing the marker on the
// staff side.
//
// It does NOT touch stopping_since / stopped_since / forced_stop_at: those date
// the stop itself, and forced_stop_at in particular is the durable record that a
// session was cut off, which the next generation is precisely who needs to read
// (dal.go, migrations/00057).
func clearMemberHandoverMarker(m *Member) {
	m.RefocusSince = 0.0
	m.RefocusOp = ""
}

// ── 停止: ONE body, both populations (T-65 包③) ──────────────────────────────
//
// stopVerbRow names the five columns the 停止 verb writes, BY POINTER. It exists
// so those writes have exactly ONE body instead of two hand-kept copies.
//
// The two populations already live in the SAME table — DAL.PutOutsourceWorker IS
// PutMember(memberFromWorker(w)), and migration 00025 folded the rows — so these
// are not five member columns and five worker columns, they are five columns.
// What was still doubled was the Go: 正職 wrote them through
// clearMemberHandoverMarker + clearRestartIntent + two assignments, and 外包
// hand-copied the same five writes with clearWorkerRestartIntent in the middle.
// Nothing mechanical held the copies equal; the only thing that did was that
// whoever edited one of them remembered the other.
//
// 🔴 WHAT THIS BUYS, AND WHAT IT DOES NOT. It removes the drift where one side
// gains or loses a write and the other does not: for THE 停止 VERB there is no
// second copy left to forget. ⚠️ FOR 停止 ONLY — 強制停止 still writes four of
// these five columns twice (api_members.go's force-stop against
// api_outsource.go's), and its fifth, stopping_since, follows a DIFFERENT rule on
// each side, so folding it in would be a behaviour change rather than a
// convergence. That one belongs to 包④/包⑤. It does NOT make a MIS-WIRED adapter safe — stopVerbRowOfWorker could
// hand back a pointer to the wrong field and applyStopVerbRow would faithfully
// write the wrong column. That failure has to be caught one layer up, at the
// handler seam, and it is: block ① of TestVerbPopulationParityMatrix drives the
// real routes.go handler for both populations and compares the terminal rows.
// This is the same split T-14 PR ① paid for the hard way — a parity test over
// the pure helper left BOTH call sites deletable with the whole suite still
// green — so the helper is deliberately NOT where the guard lives.
type stopVerbRow struct {
	DesiredState     *string
	RefocusSince     *float64
	RefocusOp        *string
	RestartAfterStop *bool
	StoppingSince    *float64
}

// stopVerbRowOfMember / stopVerbRowOfWorker are pure address-taking adapters and
// must stay that way: any logic here would be logic that exists twice again,
// which is the thing this file just deleted.
func stopVerbRowOfMember(m *Member) stopVerbRow {
	return stopVerbRow{
		DesiredState:     &m.DesiredState,
		RefocusSince:     &m.RefocusSince,
		RefocusOp:        &m.RefocusOp,
		RestartAfterStop: &m.RestartAfterStop,
		StoppingSince:    &m.StoppingSince,
	}
}

func stopVerbRowOfWorker(w *OutsourceWorker) stopVerbRow {
	return stopVerbRow{
		DesiredState:     &w.DesiredState,
		RefocusSince:     &w.RefocusSince,
		RefocusOp:        &w.RefocusOp,
		RestartAfterStop: &w.RestartAfterStop,
		StoppingSince:    &w.StoppingSince,
	}
}

// applyStopVerbRow is THE body of 停止, for both populations. `snapshot` is the
// row as it stood BEFORE this call — stopEpochAnchor reads forced_stop_at and
// stopping_since off it to decide whether a FORCE-stop's epoch is live and must
// not be re-stamped into a softer one, and reading that off a row this function
// has already begun mutating would answer a different question.
//
// Each write below is here because BOTH populations made it, and the comment
// says which rule it is carrying:
//
//   - desired_state=offline — the owner saying DOWN. It is what makes every
//     auto-spawn branch skip the subject, on both sides.
//   - refocus_since / refocus_op cleared — 換手 marker. Two different sentences
//     arrive at the same two lines: on the staff side it is the destructive
//     reader armRefocusEpoch describes; on the worker side it is mechanical,
//     because autoHandoverWorker's in-flight arm collects a refocus epoch by
//     kill+RESPAWN, which would revive a worker the owner just held down.
//     🔴 THE TWO REASONS ARE BOTH STILL TRUE AND NEITHER IS REDUNDANT — this
//     函式 carries the write, not the justification, and removing either
//     population's reason from its handler would lose a fact.
//   - restart_after_stop cleared — 後蓋前: 下線 is the softest rung of the
//     ladder but it is the same statement about 「要不要起來」 as the hardest,
//     so a 起來 queued by an earlier 重新聚焦 / 改機器 / 換 model is CANCELLED.
//   - stopping_since = stopEpochAnchor(snapshot, now) — the stop epoch's anchor,
//     re-stamped UNLESS a live forced epoch owns it.
//
// 🔴 THE stopping_since WRITE IS THE ONE THAT IS NOT UNCONDITIONAL, and the
// reasoning below used to live at the staff call site. It moved here with the
// write itself (T-65 包③) — leaving it behind would have left a comment whose
// subject is no longer underneath it.
//
// …UNCONDITIONAL with ONE exception: a stop epoch that a FORCE-stop opened
// must not be re-stamped into a softer one. The SSE stop gate separates
// "close-out in flight" (admit the reconnect) from "cut off deliberately"
// (refuse it) by comparing the two anchors — forced_stop_at >= this epoch's
// stopping_since means forced — so re-stamping stopping_since to now would
// move a force-stopped member to the ADMIT side, and the 下線 arm runs no
// clock, so nothing would collect it afterwards. Found by independent
// review; reachable through the API/MCP surface (the cockpit offers no 下線
// button in stopping/stopped, but that is a UI fact, not a gate).
//
// The three conditions are each load-bearing. `stopping_since > 0` is what
// keeps this narrow to a LIVE forced epoch: activate clears stopping_since
// and waking_since but deliberately KEEPS forced_stop_at (it is the durable
// record that a past session was cut off), so testing forced_stop_at alone
// would strip the soft-offboard admission from every member that was ever
// force-stopped.
//
// 🔴 "the stop anchors", which is what this used to say, is one anchor too
// many: activate does NOT clear stopped_since. That is not a nit — it is the
// reason a brand-new session can come up ONLINE carrying the PREVIOUS
// generation's report with no epoch (下線 → 活化), which is exactly the state
// stampContextHighRecycle's boot_ts test exists to tell apart from a live
// session's own report. A reader who believes the shorter sentence will
// conclude that state is unreachable and write the wrong guard.
//
// Consequence, deliberate: a forced epoch's anchor stops moving, so this
// call no longer restarts the grace clock for it. Nothing reads it that way
// — the 下線 arm returns decisionNone while the soft grace is on, and
// offboardKindOf answers soft for desired-offline without consulting the
// anchor's age.
//
// 🔴 THAT LAST SENTENCE IS STAFF-SCOPED, and it did not have to say so while it
// lived at the staff call site. It does now: decideDown is the MEMBER reconcile
// path, and a stopped WORKER never reaches a tick at all — runOutsourceTick
// `continue`s on desired_state=offline, which is why the worker side collects
// inline instead. See the 停止（離線起點）rows in the parity whitelist. Caught
// by independent review, which is the failure mode this move creates: a
// paragraph that was true where it stood becomes a claim about both
// populations the moment it is hoisted into shared code.
//
// It writes NOTHING else, and in particular no stopped_since and no
// forced_stop_at: this verb opens a close-out, it does not end one, and the two
// populations end that close-out through machinery that is NOT converged here
// (see the whitelist rows for 停止 in verb_population_parity_t65_test.go).
func applyStopVerbRow(row stopVerbRow, snapshot Member, now float64) {
	*row.DesiredState = DesiredStateOffline
	*row.RefocusSince = 0.0
	*row.RefocusOp = ""
	*row.RestartAfterStop = false
	*row.StoppingSince = stopEpochAnchor(snapshot, now)
}

// POST /api/members/{member_id}/deactivate — desired_state=offline + an
// UNCONDITIONAL stopping_since re-stamp, with ONE exception.
//
// ⚠️ THE EXCEPTION IS NO LONGER "BELOW", which is what this line used to say:
// T-65 包③ moved the write, and the paragraph explaining it, UP into
// applyStopVerbRow. Independent review caught the dangling pointer.
//
// The re-stamp does NOT restart a countdown: since rc-27d1710174dd the 下線 arm
// runs no clock at all. What the anchor dates is the close-out epoch — which
// reconnect the SSE stop gate admits (api_infra.go), and, paired with
// ForcedStopAt, whether forcedEpochLive reads this stop as a deliberate cut-off.
//
// ⚠️ NOT clearStaleStoppingOnOnline, whatever an older version of this comment
// said: that sweep skips every member whose desired_state is offline, and this
// handler writes offline two statements from here. The sweep only ever sees the
// self-driven arm (report_stopping, which touches no desired_state at all).
func (s *apiServer) HandleDeactivateMemberApiMembersMemberIdDeactivatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	// 🔴 CANCELLING A WAKE IS NOT A GRACEFUL STOP (T-7526). Read BEFORE the
	// mutation below — stamping stopping_since is itself what ends the waking
	// projection.
	//
	// decideDown's first branch is `if !obs.Online { converged offline }`, and a
	// waking member is BY DEFINITION not online (deriveLiveness projects waking
	// only when !Online). So for the whole waking window the cadence dispatched
	// NOTHING: the process the earlier START already put on the machine booted
	// anyway, connected, went green, and only then — as a now-online member with
	// desired_state=offline — did decideDown even look at it (at the time that
	// armed a 120s grace; today that arm runs no clock at all). Either way the
	// owner's 取消 read as "the button did nothing", which is exactly what it did.
	//
	// There is also nothing to wind down: a member that has not connected has
	// taken no work, so the grace window it cannot enter would buy nothing.
	cancellingWake := PresenceState(*m, nowSecs(), s.hub.IsOnline(m.ID)) ==
		MemberPresenceWaking
	// 🔴 THE ROW WRITES LIVE IN applyStopVerbRow AND ARE SHARED WITH
	// HandleStopOutsourceWorker… (T-65 包③). The snapshot is taken BEFORE the
	// mutation on purpose: stopEpochAnchor has to read the PRE-stop
	// stopping_since / forced_stop_at to tell a live forced epoch from a soft
	// one, and this call is the last moment those are still the old values.
	//
	// What used to be written here, and the two rules that came with it, are
	// written down where the writes now are — including 後蓋前 (T-14 項目 7,
	// clearRestartIntent) and the 換手 marker (clearMemberHandoverMarker). Both
	// helpers still exist and are still called from the OTHER verbs; what is
	// gone is this verb's second copy of them.
	applyStopVerbRow(stopVerbRowOfMember(m), *m, nowSecs())
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if cancellingWake {
		// The same immediate robust STOP force-stop uses. NOT widened to the
		// online case: a live member gets the no-countdown soft window instead,
		// and it is collected by its own report_stopped or by the owner pressing
		// 加速停止 / 強制停止 — never from here.
		s.dispatchRobustStopNow(m.ID)
	}
	// Event-driven reconcile: move the member into `stopping` immediately rather
	// than on the next tick. It arms NO clock — decideDown's online arm returns
	// decisionNone for the whole soft window, so nothing here will ever collect
	// the member; that is the owner's ruling, not a gap. Still run after a cancel
	// — the raw dispatch above does not touch the reconcile store.
	s.reconcileMemberNow(m.ID)
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/force-stop — STOP intent now (stamps
// stopping_since only if unset) + the immediate robust-STOP dispatch straight
// to the member's warden, bypassing the ~30s cadence
// (handlers.handle_force_stop_member).
//
// There is no grace clock here to bypass: the SERVER arms none on the 下線 arm
// (owner ruling rc-27d1710174dd). Three things end a soft offboard, and this is
// the last of them: the agent's own report_stopped, the deadline the owner opens
// by pressing 加速停止 (that clock is HIS, armed only by the press, which is why
// it does not reopen the ruling), and this endpoint. See the endpoint's
// description in spec/openapi.json, which says the same at length.
func (s *apiServer) HandleForceStopMemberApiMembersMemberIdForceStopPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m.DesiredState = DesiredStateOffline
	clearMemberHandoverMarker(m)
	// 後蓋前 (T-14 項目 7): 強制停止 is the owner saying DOWN, so any 重啟 he
	// queued earlier in this wind-down is cancelled. This is what keeps
	// 重新聚焦 → 強制停止 different from 強制停止 → 重新聚焦.
	clearRestartIntent(m)
	if m.StoppingSince <= 0.0 {
		m.StoppingSince = nowSecs()
	}
	// The record that this session was cut off (T-a9d6). Force-stop sends no
	// notice — the recipient is about to stop existing, so a sentence meant to
	// change its behaviour has no one to change — and that silence is exactly
	// why the fact has to be written down: everything a killed session leaves
	// behind is indistinguishable from what a session with nothing to say
	// leaves behind. Stamped on the member itself so the NEXT generation and the
	// cockpit can both see it; PutMember persists it forward-only with max(), so
	// a stale snapshot cannot erase the record.
	m.ForcedStopAt = nowSecs()
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.SetMemberForcedStopAt(m.ID, m.ForcedStopAt); err != nil {
		// Best-effort, and deliberately not fatal: the kill below is the point
		// of the call and the member IS being force-stopped. Reporting a
		// failure here would say "force-stop failed" about a member that is
		// about to be killed anyway — the same shape as the dismiss sweep.
		taskLog("force-stop %s: forced_stop_at not recorded: %v", m.ID, err)
	}
	s.dispatchRobustStopNow(m.ID)
	s.writeMemberDTO(w, *m)
}

// acceleratedStopNeedsAnOpenWindDownMsg is the ONE wording of the refusal that
// makes this an escalation rather than a second stop button. It names the rung
// below it, because a 409 that only says "no" leaves the owner guessing which of
// three buttons he was supposed to press first.
const acceleratedStopNeedsAnOpenWindDownMsg = "加速停止 escalates a wind-down that is " +
	"already open — this member has not been asked to stop. Press 停止 (deactivate) " +
	"or 重新聚焦 (refocus) first"

// POST /api/members/{member_id}/accelerated-stop — the MIDDLE rung of the
// owner's escalation 停止 → 加速停止 → 強制停止 (owner 2026-08-21 「可以給我按鈕
// 嗎」＋「停止 → 加速停止 → 強制停止」).
//
// 🔴 IT ESCALATES, IT DOES NOT INITIATE, and the 409 below is the whole
// difference. A member that has not been asked to stop has been told nothing, so
// putting it on a clock would be a deadline it never heard about — the exact
// shape T-ed79 exists to remove. Pressing 停止 (or 重新聚焦) first is what makes
// the member a party to the countdown this endpoint starts.
//
// 🔴 IT DOES NOT REOPEN rc-27d1710174dd (「不要兜底：只有你按強制下線才收它」).
// That ruling is about the SERVER starting a clock on its own; decideDown still
// runs none. This clock exists only because the owner pressed the button, which
// is the same authority force-stop has always had — with a sentence attached
// instead of silence.
//
// BOTH arms are handled, because the ladder has to work wherever the owner
// started it:
//
//   - 下線 (desired_state=offline + stopping_since): re-stamp stopping_since from
//     THIS press and write the cause. decideDown then collects at
//     stopping_since + the grace, and offboardKindOf answers `final` off the same
//     refocus_op, so the sentence quotes exactly that instant.
//   - 換手 (desired online + refocus_since): re-stamp refocus_since and write the
//     cause — the same promotion shape stampContextHighRecycle uses for
//     context_notice → context_high, and re-stamping is load-bearing for the same
//     reason: promoting in place would put the deadline at the ORIGINAL stamp,
//     already in the past, and collect the member on the tick that announced it.
//
// A force-stopped epoch is refused: that session was cut off deliberately and is
// not working a close-out, so a deadline addressed to it has no reader.
func (s *apiServer) HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	// A live session is required for the same reason 重新聚焦 requires one: the
	// notice this write fans travels down the member's own stream, and a clock
	// nobody is listening to is a silent deadline.
	if !s.hub.IsOnline(m.ID) {
		writeError(w, http.StatusConflict,
			"加速停止 requires a live session — there is nothing to accelerate on a "+
				"member that is not connected")
		return
	}
	now := nowSecs()
	switch {
	case m.DesiredState == DesiredStateOffline:
		if !gracefulStopEpochOpen(*m) {
			writeError(w, http.StatusConflict, acceleratedStopNeedsAnOpenWindDownMsg)
			return
		}
		// The owner's grace runs from HIS press, not from a 停止 he may have
		// pressed hours ago. The other anchors are deliberately untouched: this
		// is a promotion of the close-out already in flight, not a new one, and
		// zeroing stopped_since here would erase an agent's 「我收完了」 and
		// cancel the collection it had already earned.
		m.StoppingSince = now
	case m.RefocusSince > 0.0:
		m.RefocusSince = now
	default:
		writeError(w, http.StatusConflict, acceleratedStopNeedsAnOpenWindDownMsg)
		return
	}
	m.RefocusOp = refocusOpAcceleratedStop
	// 後蓋前 (T-14 項目 7). 加速停止 is a 下線 rung, so it cancels a queued 重啟
	// like the other two. ⚠️ On the 換手 arm above it still leaves desired_state
	// ONLINE — that arm is a handover being hurried along, not a stop — so this
	// call changes nothing there. Deliberately NOT widened: converting that arm
	// into a stop is a behaviour change outside the owner's [0] ruling.
	clearRestartIntent(m)
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven, so the clock the owner just started is visible on the next
	// read rather than up to a cadence tick later. It dispatches nothing on this
	// pass — the deadline is in the future by construction.
	s.reconcileMemberNow(m.ID)
	reconcileLog("加速停止: %s on the %s arm (collect at %.0f or on the stopped report)",
		m.ID, m.DesiredState, winddownDeadlineOf(*m, s.reconcileConfigLive()))
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/refocus — needs a live session (409 otherwise);
// stamps refocus_since.
//
// The gate is the SSE connection rather than the presence projection, for the
// same reason restart_self's is: a member that has begun closing out projects
// `stopping`, and refusing the owner there would mean 重新聚焦 stops working on
// an agent that is mid-hand-off — the moment he is most likely to press it.
func (s *apiServer) HandleRefocusMemberApiMembersMemberIdRefocusPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	// 下線 → 重啟 (T-14 項目 7). The stamp genuinely would not reach the agent —
	// aRefocusStampWouldReachTheAgent is right — but that was never a reason to
	// refuse the OWNER'S intent, only a reason not to write it as a refocus
	// epoch. Owner 2026-08-30: 「如果我已經到強硬下線的狀態下按下 refocus 我只
	// 需要在下線後把人帶起來」. The stop in flight keeps its stage and its
	// anchors; the only thing recorded here is 「起來」.
	if !aRefocusStampWouldReachTheAgent(*m) && aStopWasEverAskedFor(*m) {
		stampRestartIntent(m)
		stampMemberOpReceipt(m, memberRestartQueuedReceipt(refocusOpRefocus), nowSecs())
		if err := s.putMember(*m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
		// The five last_op* columns left the whole-row writer in T-55 批次B, so the
		// stamp is IN MEMORY ONLY until this lands — the same seam the 換 model and
		// 改機器 faces above take. It MUST precede the tick below: that tick can
		// spend the intent and stamp its own receipt, and a persist after it would
		// push this handler's older snapshot back over the newer sentence.
		if err := s.persistMemberOpReceipt(*m, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
		// The member may ALREADY be converged offline (a stop that landed before
		// the owner pressed this), in which case the queued start is spendable on
		// this very tick rather than up to a cadence later.
		s.reconcileMemberNow(m.ID)
		if fresh, err := s.dal.GetMember(m.ID); err == nil && fresh != nil {
			m = fresh
		}
		s.writeMemberDTO(w, *m)
		return
	}
	if !s.hub.IsOnline(m.ID) || !aRefocusStampWouldReachTheAgent(*m) {
		writeError(w, http.StatusConflict,
			"refocus requires the member to have a live session and to be wanted "+
				"online (§3.4 #14)")
		return
	}
	// 🔴 The ladder only goes forward (owner, 2026-08-24). 重新聚焦 is 停止 —
	// stage 1 — so pressing it on a member already in 加速停止 used to push the
	// stage BACK and clear the deadline with it, leaving an agent that had been
	// told it was counting down no longer counting. Refused here rather than
	// silently downgraded: the owner pressed a button, so he gets an answer.
	if !armRefocusEpoch(m, refocusOpRefocus, nowSecs()) {
		writeError(w, http.StatusConflict,
			"refocus is 停止 and this member is already further along the "+
				"wind-down ladder (下線 → 加速 → 強制); a later stage is never "+
				"replaced by an earlier one")
		return
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// DELETE /api/members/{member_id} — dismiss: a SOFT delete (roster_status=
// removed + desired_state=offline); the audit row survives.
func (s *apiServer) HandleDismissMemberApiMembersMemberIdDelete(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m.RosterStatus = RosterStatusRemoved
	m.DesiredState = DesiredStateOffline
	clearRestartIntent(m)
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// T-4166: the asker is gone, so no answer can ever be delivered to its
	// waiting cards — retire them instead of leaving them in the owner's
	// 等我回覆 pane forever (each one pins the cockpit red dot on a member that
	// no longer exists). Same sweep the reassign / task-close seams use.
	//
	// BEST-EFFORT (review B5): putMember above ALREADY persisted the dismissal,
	// and there is no transaction to roll it back. 500-ing here would report
	// "dismiss failed" for a member that IS dismissed. Log instead — matching
	// expireWaitingCardsFromMember's own contract and the worker-dismissal twin.
	if _, err := s.expireWaitingCardsFromMember(m.ID, nowSecs(), requestTrigger(r)); err != nil {
		taskLog("dismiss %s: reply-card sweep failed (cards left waiting): %v", m.ID, err)
	}
	s.writeMemberDTO(w, *m)
}

// ── self-report presence (identity from token, NO member_id target) ──────────

// resolveSelf is the caller's own live member (404 when it has no roster row
// — e.g. the owner's sub has none: self-report is agent-only by construction).
// It takes no memberScope and never folds kind='outsource': since the graceful
// worker handover (T-ea82) an outsource worker walks the SAME 〈停止〉 wake as a
// member and reports its own presence through these self endpoints. The
// member_id-target ADMIN verbs are the ones that still refuse an ow- row, and
// they do it by passing staffOnly — a choice each of them makes by name.
func (s *apiServer) resolveSelf(r *http.Request) (*Member, error) {
	m, err := s.dal.GetMember(currentActor(r))
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved {
		return nil, errNotFound
	}
	return m, nil
}

// stampAgentIatFloor raises the caller's own member credential floor to the
// `iat` of the token the caller is holding right now (T-14 項目 4B). Called
// from report_waking and nowhere else: 「新的一輪一上線就失效」 (owner 2026-08-30,
// rc-fe6451abe579) names that report as the exact instant the previous
// generation's authority ends.
//
// 🔴 The caller's OWN iat, never nowSecs(). requireAuth compares with STRICTLY
// LESS THAN, so a floor equal to the caller's iat is a floor the caller passes
// by construction — no matter how many seconds passed between its mint and this
// call, and no matter how the two clocks differ. nowSecs() would refuse a token
// that merely took a while to get here.
//
// A token with no iat claim stamps nothing: there is no honest floor to derive
// from it, and inventing one from the clock is precisely the bug above.
func (s *apiServer) stampAgentIatFloor(r *http.Request) error {
	iat, ok := claimsFromContext(r.Context())["iat"].(float64)
	if !ok || iat <= 0 {
		return nil
	}
	return s.dal.SetMemberAgentIatFloor(currentActor(r), iat)
}

// POST /api/self/waking — the boot report: stamps waking_since and clears ALL
// recycle markers. The reported model is stored separately from the owner's
// launch configuration.
func (s *apiServer) HandleReportWakingApiSelfWakingPost(w http.ResponseWriter, r *http.Request) {
	var body ReportWakingDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	// T-14 項目 4B — raise this member's credential floor to THIS CALLER'S OWN
	// token iat, BEFORE either arm below commits anything. From here on every
	// token minted for an earlier generation of this member is refused at
	// requireAuth (agentIatFloorRefusal).
	//
	// It runs FIRST, and its failure is fatal, on purpose. Stamping after the
	// wake would leave a member reported-awake with the previous generation's
	// credentials still live and no signal that they are; stamping first means
	// the only failure mode is a floor raised for a wake that then 500s — and
	// that direction is harmless, because the floor is the caller's OWN iat, so
	// the caller retrying is not locked out by it.
	//
	// The stamp is unconditional across kinds. A warden row gets a floor like
	// any other, and the READ side is what exempts it — putting the exemption
	// here as well would make the safety property depend on two places agreeing.
	if err := s.stampAgentIatFloor(r); err != nil {
		internalError(w, err)
		return
	}
	// A new generation counts its spend from zero (T-53). Forget the account
	// accrual baseline here, before either arm, so the first cost this session
	// reports is credited in full to whichever account it names instead of being
	// read as an increase over the session that just ended.
	s.startAccountSpendSession(m.ID)
	if m.Kind == KindOutsource {
		// Worker fold (T-ea82): clear the recycle markers under outsourceMu via
		// the worker funnel — a member-path putMember here would race the tick's
		// read-modify-write and could lose the fold.
		fresh, werr := s.workerReportWaking(m.ID, body.Model, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	m.WakingSince = nowSecs()
	m.RefocusSince = 0.0
	m.RefocusOp = ""
	m.StoppedSince = 0.0
	// 🔴 …but NOT the stop trace of a member the owner has already cancelled
	// (T-7526). Clearing a stale anchor is right for an ORDINARY boot; doing it
	// unconditionally erased the only mark a mid-wake 取消 left behind, so the
	// agent that was already booting when the cancel landed came up painting a
	// fresh green over an intent that is still offline. The intent itself
	// (desired_state) is what says whether this boot is wanted.
	if m.DesiredState == DesiredStateOnline {
		m.StoppingSince = 0.0
	}
	if body.Model != nil {
		m.ActualModel = *body.Model
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/stopping — stamps the caller's stopping_since IF UNSET
// (waking_since deliberately NOT cleared; stopping dominates the projection).
func (s *apiServer) HandleReportStoppingApiSelfStoppingPost(w http.ResponseWriter, r *http.Request) {
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		fresh, werr := s.workerReportStopping(m.ID, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	if m.StoppingSince <= 0.0 {
		m.StoppingSince = nowSecs()
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/stopped — anchors stopped_since ONCE (never re-stamped).
// That FIRST report fires the event-driven collect, so kill→respawn happens
// immediately rather than on the next ~30s tick. It no longer matters what
// opened the offboard: an agent that says it is done is collected either way
// (owner rc-b08d49dc3b03), and desired_state alone decides whether a new
// generation follows.
func (s *apiServer) HandleReportStoppedApiSelfStoppedPost(w http.ResponseWriter, r *http.Request) {
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		// Worker 收口 (T-ea82): the first stopped-report of a refocus-marked
		// worker runs the collect funnel (kill+respawn NOW) — the member
		// recycle-kill shape, riding the worker's own kill funnel instead of
		// dispatchRobustStopNow.
		fresh, werr := s.workerReportStopped(m.ID, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	// 🔴 A stopped-report is now ALWAYS collected (owner 2026-08-16, card
	// rc-b08d49dc3b03 option ①: 「收掉並重生」).
	//
	// It used to be collected only when something was already collecting it —
	// a refocus epoch was in flight. That was sound while the offboard sequence
	// was shown ONLY to a session being collected: the last step always had a
	// receiver. Then the notice began telling agents to close out on their own
	// (T-c382) and the sequence became a document any session could work
	// (T-c9c0), which opened a path nobody was waiting at the end of: an agent
	// finished its close-out, reported stopped, and NOTHING happened. It stayed
	// alive holding a session it had already declared finished — and the sweep
	// that clears stale stopping anchors erased the evidence, painting it green
	// again (the owner's T-2123 report, and the previous generation of THIS
	// member lived it).
	//
	// desired_state decides what follows, and neither arm needs a special case
	// here: online respawns on the next tick's plain START, offline stays down.
	recycleKill := m.StoppedSince <= 0.0
	if m.StoppedSince <= 0.0 {
		m.StoppedSince = nowSecs()
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Dispatch AFTER putMember so the marker persistence + member-delta fan
	// (→ agent RecycleHook) has already landed before the STOP.
	if recycleKill {
		s.dispatchRobustStopNow(m.ID)
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/refocus — restart_self(): the agent's SELF-TRIGGERED recycle
// (identity from token, NO member_id). A self-op is only ever able to restart
// the CALLER, so it is strictly weaker than the admin-gated refocus_member —
// zero privilege-escalation surface. The EFFECT is identical to refocus_member:
// stamp the caller's refocus_since and fan the member delta; the standard §4.5
// recycle orchestration (the agent's own RecycleHook → 〈停止〉 wake →
// report_stopped → server kill/respawn) carries the rest. Nothing is dispatched
// here (same as refocus_member — no reconcileMemberNow).
//
// Two abuse guards refuse LOUDLY (readable by the agent):
//   - LIVE-SESSION-ONLY (409): a self-restart is meaningless with no live
//     session to recycle. 🔴 The test is the SSE connection, not the presence
//     projection. Those differ for exactly the caller this endpoint exists for:
//     the offboard notice says 「work the sequence below, then call
//     report_stopped yourself」, step 1 of that sequence is report_stopping, and
//     that stamps the anchor which makes PresenceState project `stopping`. So a
//     session that has merely STARTED its close-out already reads as stopping,
//     and a presence test here would refuse the caller this endpoint exists for
//     — and once a close-out's anchor stopped being swept away every tick
//     (T-2123) that refusal lasted the whole soft window instead of clearing on
//     the next tick. A session holding an open stream has something to
//     recycle; that is the whole question here.
//   - MINIMUM-LIVENESS (429): a call within minSelfRestartSecs of this session
//     connecting is refused — the server-authoritative boot_ts (stamped on the
//     SSE first-connect edge, onFirstConnect) is the anchor; reusing the
//     bootStormTripped loop-guard so a missing boot_ts (server-restart amnesia)
//     FAILS OPEN, never a false 429 on a long-lived session.
func (s *apiServer) HandleRestartSelfApiSelfRefocusPost(w http.ResponseWriter, r *http.Request) {
	var body RestartSelfDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	now := nowSecs()
	if !s.hub.IsOnline(m.ID) || !aRefocusStampWouldReachTheAgent(*m) {
		writeError(w, http.StatusConflict,
			"restart_self requires a live session to recycle, on a member that is "+
				"still wanted online")
		return
	}
	secsSinceBoot := gaugeSecsSinceBoot(s.gauge.Get(m.ID), now)
	if bootStormTripped(secsSinceBoot, minSelfRestartSecs) {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf(
			"restart_self refused: only %.0fs since this session started; the "+
				"minimum-liveness floor is %.0fs (prevents a respawn storm)",
			*secsSinceBoot, minSelfRestartSecs))
		return
	}
	if m.Kind == KindOutsource {
		// Worker fold (T-ea82): stamp the refocus epoch + open the graceful
		// window via the worker funnel (the same shape the owner's refocus
		// button takes) — the standard SOP → stopped-report → collect carries
		// the rest.
		fresh, werr := s.workerRestartSelf(m.ID, now, requestTrigger(r))
		if errors.Is(werr, errWindDownLadderBackwards) {
			// The SAME refusal the staff arm below writes, and deliberately the
			// same sentence: the two arms of this handler are one rule.
			writeError(w, http.StatusConflict,
				"restart_self is 停止 and you are already further along the "+
					"wind-down ladder (下線 → 加速 → 強制); finish the close-out you "+
					"were given instead")
			return
		}
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		if reason := trimmedOrEmpty(body.Reason); reason != "" {
			reconcileLog("recycle: %s self-restart (restart_self); reason: %s", m.ID, reason)
		} else {
			reconcileLog("recycle: %s self-restart (restart_self)", m.ID)
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	// Same ladder rule as the owner's refocus above. restart_self is 停止, so an
	// agent that is already in 加速停止 cannot talk its way back to the slower
	// procedure — which would also hand it back the deadline it was counting to.
	if !armRefocusEpoch(m, refocusOpRestartSelf, now) {
		writeError(w, http.StatusConflict,
			"restart_self is 停止 and you are already further along the "+
				"wind-down ladder (下線 → 加速 → 強制); finish the close-out you "+
				"were given instead")
		return
	}
	if err := s.persistMemberWindDownAnchors(*m); err != nil {
		internalError(w, err)
		return
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Distinguish a self-restart from an owner refocus on the operator log
	// (both stamp refocus_since identically; the reason is the differentiator).
	if reason := trimmedOrEmpty(body.Reason); reason != "" {
		reconcileLog("recycle: %s self-restart (restart_self); reason: %s", m.ID, reason)
	} else {
		reconcileLog("recycle: %s self-restart (restart_self)", m.ID)
	}
	s.writeMemberDTO(w, *m)
}
