package main

// warden_command_delivery_test.go — T-66a2, the two halves of "a command frame
// vanished and nobody could tell".
//
// L1: DrainWardenCommands empties the FIFO in one shot, then the stream loop
// writes the frames one at a time and returns on the first write error. Every
// already-popped frame from that point on used to be discarded with no log, no
// receipt and no field — so a LOST order and an order NEVER SENT rendered
// identically, which is exactly why the incident had six "successful" dispatches
// and zero explanations. The at-most-once contract for start/stop/uninstall is
// deliberate (reconcile re-decides from presence within one cadence) and is kept
// here; what is fixed is (a) the silence, for every verb, and (b) `update`,
// which has NO re-decision path anywhere and is the verb most likely to be in
// flight when the stream dies, because the upgrade endpoint re-execs the server.
//
// RECEIPT OVERWRITE: last_op* is one slot with two blind writers. The dispatch
// diagnosis ("nothing ever came back" — the clue that decides whether to go look
// at that machine) was erased outright by the next execution receipt.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── helpers ─────────────────────────────────────────────────────────────────

// failAfterWrites is a ResponseWriter+Flusher whose Write succeeds `ok` times
// and fails forever after — the shape of a connection that dies mid-drain
// (a reset peer, a tripped write deadline). Write #1 is the handler's
// ": connected" preamble, so ok=N means N-1 loop frames land.
type failAfterWrites struct {
	mu     sync.Mutex
	ok     int
	n      int
	hdr    http.Header
	frames [][]byte
}

func newFailAfterWrites(ok int) *failAfterWrites {
	return &failAfterWrites{ok: ok, hdr: http.Header{}}
}

func (c *failAfterWrites) Header() http.Header { return c.hdr }
func (c *failAfterWrites) WriteHeader(int)     {}
func (c *failAfterWrites) Flush()              {}

func (c *failAfterWrites) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	if c.n > c.ok {
		return 0, errors.New("connection reset by peer")
	}
	frame := make([]byte, len(p))
	copy(frame, p)
	c.frames = append(c.frames, frame)
	return len(p), nil
}

func (c *failAfterWrites) written() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.frames))
	copy(out, c.frames)
	return out
}

// cmdFrame builds one warden-command wire frame for the given verb/target. The
// digest reader only ever looks at rpc + args.member_id, which is exactly what
// the real STOP/UNINSTALL/UPDATE frames carry (buildTargetFrame) and what a
// START frame carries alongside its secret cargo.
func cmdFrame(t *testing.T, verb, memberID string) []byte {
	t.Helper()
	frame, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC: verb, Args: wardenTargetArgs{MemberID: memberID},
	})
	if err != nil {
		t.Fatalf("build %s frame: %v", verb, err)
	}
	return frame
}

// runWardenStream drives the REAL /api/events handler as wardenID over w until
// `until` reports the interesting moment has passed (or the handler returns on
// its own, which a failing write makes it do), then cancels the request context
// and joins the handler goroutine.
func runWardenStream(t *testing.T, api *apiServer, wardenID string,
	w http.ResponseWriter, until func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/api/events", nil)
	claims := map[string]any{"sub": wardenID, "scope": "agent"}
	req = req.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	done := make(chan struct{})
	go func() {
		api.HandleEventsApiEventsGet(w, req)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-done:
			return
		default:
		}
		if until() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the SSE handler never reached the expected state")
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the SSE handler never returned after cancellation")
	}
}

// ── decode: the accounting can read a frame back without touching the token ──

func TestDecodeWardenCommandFrame(t *testing.T) {
	frame := cmdFrame(t, reconcileCmdUpdate, "mach-a")
	digest, ok := decodeWardenCommandFrame(frame)
	if !ok || digest.Verb != reconcileCmdUpdate || digest.MemberID != "mach-a" {
		t.Fatalf("digest must name verb+target, got %+v (ok=%v)", digest, ok)
	}
	// A START's real frame shape (the one carrying the secret) must decode too.
	start, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC:  reconcileCmdStart,
		Args: wardenStartArgs{MemberID: "m-1", MemberToken: "s3cret"},
	})
	if err != nil {
		t.Fatalf("build start frame: %v", err)
	}
	digest, ok = decodeWardenCommandFrame(start)
	if !ok || digest.Verb != reconcileCmdStart || digest.MemberID != "m-1" {
		t.Fatalf("start digest wrong: %+v (ok=%v)", digest, ok)
	}
	if _, ok := decodeWardenCommandFrame([]byte("data: not-json\n\n")); ok {
		t.Fatal("garbage must not decode as a command frame")
	}
	if _, ok := decodeWardenCommandFrame([]byte(": heartbeat\n\n")); ok {
		t.Fatal("a heartbeat must not decode as a command frame")
	}
}

// ── ITEM 1 counterfactual: the stream loop hands undelivered frames back ─────

// TestEventsHandler_UndeliveredWardenCommandsAreAccountedFor drives the real
// handler over a connection that dies after the first drained frame. Before the
// fix the handler simply returned: the remaining frames were gone with no trace
// at all. Now the update frame is requeued (it has no re-decision path) and
// every loss is named on stderr and left as a note the wake diagnosis can read.
//
// MUTANT: delete the s.hub.ReturnUndeliveredCommands(...) call in api_infra.go
// (return straight away, as HEAD did) → this test goes RED on the requeue.
func TestEventsHandler_UndeliveredWardenCommandsAreAccountedFor(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "mach-a", Kind: KindWarden,
		DesiredState: DesiredStateOnline})

	// FIFO: [stop m-1] [update mach-a] [start m-2].
	api.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, "stop", "m-1"))
	api.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdUpdate, "mach-a"))
	api.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdStart, "m-2"))

	// ok=2 → the ": connected" preamble plus exactly ONE command frame land;
	// the update and the start die on the wire.
	w := newFailAfterWrites(2)
	logs := captureStderr(t, func() {
		// The failing write returns the handler on its own.
		runWardenStream(t, api, "mach-a", w, func() bool { return false })
	})

	if got := len(w.written()); got != 2 {
		t.Fatalf("expected preamble + 1 delivered frame, got %d writes", got)
	}

	// The update must be BACK on the FIFO, ready for the next connection.
	pending := api.hub.DrainWardenCommands("mach-a")
	if len(pending) != 1 {
		t.Fatalf("exactly the update frame must be requeued, got %d frames back", len(pending))
	}
	digest, ok := decodeWardenCommandFrame(pending[0].Frame)
	if !ok || digest.Verb != reconcileCmdUpdate {
		t.Fatalf("the requeued frame must be the update, got %+v", digest)
	}

	// The START stays dropped (at-most-once by contract) but is no longer
	// SILENT: a note explains it and stderr names it.
	note, lost := api.hub.UndeliveredCommandSince("m-2", 0)
	if !lost || note.Verb != reconcileCmdStart || note.Warden != "mach-a" || note.Requeued {
		t.Fatalf("the dropped START must leave a non-requeued note, got %+v (lost=%v)", note, lost)
	}
	for _, want := range []string{
		"verb=start target=m-2", "DROPPED",
		"verb=update target=mach-a", "REQUEUED",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("stderr must contain %q so the loss is readable from outside;\ngot: %s",
				want, logs)
		}
	}
	if strings.Contains(logs, "s3cret") || strings.Contains(logs, "member_token") {
		t.Fatalf("the accounting must never print frame cargo; got: %s", logs)
	}
}

// A warden that flaps repeatedly must accumulate ONE pending update, not one per
// flap — the requeue is a retry, not a leak.
func TestReturnUndeliveredCommands_UpdateRequeueIsIdempotent(t *testing.T) {
	h := NewHub()
	frame := cmdFrame(t, reconcileCmdUpdate, "m-1")
	captureStderr(t, func() {
		for i := 0; i < 5; i++ {
			pending := h.DrainWardenCommands("mach-a")
			pending = append(pending, wardenCmd{Subject: "m-1", Frame: frame})
			h.ReturnUndeliveredCommands("mach-a", pending)
		}
	})
	if got := len(h.DrainWardenCommands("mach-a")); got != 1 {
		t.Fatalf("five flaps must leave ONE pending update, got %d", got)
	}
}

// TestReturnUndeliveredCommands_RequeuedFrameLeadsFramesThatArrivedLater: a
// restored frame goes back at the HEAD of the FIFO, ahead of anything enqueued
// while the doomed write was still in flight.
//
// This is an ORDER property, and order is the one thing a count assertion cannot
// see: `append(queue, back...)` restores exactly the right NUMBER of frames and
// is wrong in the way that is hardest to notice. It matters because a warden
// command sequence is order-significant — a `stop` that overtakes its own `start`
// reaps the session that start was meant to create.
//
// hub.go states this rule in a comment; before T-66a2 a test on the (now removed)
// RequeueWardenCommands enforced it. Nothing did afterwards: reversing the append
// left the whole suite green, on this branch AND on trunk. This is that guard,
// restored at the layer that survived.
func TestReturnUndeliveredCommands_RequeuedFrameLeadsFramesThatArrivedLater(t *testing.T) {
	h := NewHub()
	h.EnqueueWardenCommandFor("mach-a", "m-lost", cmdFrame(t, reconcileCmdUpdate, "m-lost"))

	pending := h.DrainWardenCommands("mach-a")
	if len(pending) != 1 {
		t.Fatalf("precondition: want 1 drained frame, got %d", len(pending))
	}
	// A NEW command is enqueued while the doomed write is still in flight — it is
	// therefore NEWER than the frame that failed, and must queue behind it.
	h.EnqueueWardenCommandFor("mach-a", "m-later", cmdFrame(t, reconcileCmdUpdate, "m-later"))
	captureStderr(t, func() { h.ReturnUndeliveredCommands("mach-a", pending) })

	back := h.DrainWardenCommands("mach-a")
	want := []string{"m-lost", "m-later"}
	if len(back) != len(want) {
		t.Fatalf("want %d queued frames, got %d", len(want), len(back))
	}
	for i := range want {
		digest, ok := decodeWardenCommandFrame(back[i].Frame)
		if !ok || digest.MemberID != want[i] {
			t.Fatalf("frame[%d] acts on %q, want %q — the restored frame must lead the "+
				"one that arrived during the failed write, or a later command can "+
				"overtake the one it depends on", i, digest.MemberID, want[i])
		}
	}
}

// A fresh dispatch clears the stale loss note: the diagnosis explains ONE lost
// dispatch, never the attempt now in flight.
func TestEnqueueWardenCommand_ClearsStaleUndeliveredNote(t *testing.T) {
	h := NewHub()
	captureStderr(t, func() {
		h.ReturnUndeliveredCommands("mach-a",
			[]wardenCmd{{Subject: "m-1", Frame: cmdFrame(t, reconcileCmdStart, "m-1")}})
	})
	if _, lost := h.UndeliveredCommandSince("m-1", 0); !lost {
		t.Fatal("the loss must be noted first")
	}
	h.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdStart, "m-1"))
	if _, lost := h.UndeliveredCommandSince("m-1", 0); lost {
		t.Fatal("a fresh dispatch must clear the stale loss note")
	}
}

// ── ITEM 1 sentinel: the normal delivery path is untouched ──────────────────

// TestEventsHandler_DeliveredWardenCommandsLeaveNoResidue is the sentinel: when
// every frame writes successfully, all of them reach the wire in FIFO order, the
// FIFO ends empty, nothing is requeued and no loss note exists. A fix that
// requeued or logged on the happy path would fail here.
func TestEventsHandler_DeliveredWardenCommandsLeaveNoResidue(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "mach-a", Kind: KindWarden,
		DesiredState: DesiredStateOnline})
	api.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, "stop", "m-1"))
	api.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdUpdate, "mach-a"))
	api.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdStart, "m-2"))

	// Every write succeeds; the stream is cancelled once all three have landed.
	w := newFailAfterWrites(1 << 30)
	logs := captureStderr(t, func() {
		runWardenStream(t, api, "mach-a", w, func() bool { return len(w.written()) >= 4 })
	})

	written := w.written()
	if len(written) != 4 {
		t.Fatalf("all three frames must land, got %d writes", len(written))
	}
	for i, verb := range []string{"stop", reconcileCmdUpdate, reconcileCmdStart} {
		digest, ok := decodeWardenCommandFrame(written[i+1])
		if !ok || digest.Verb != verb {
			t.Fatalf("frame %d must be %q in FIFO order, got %+v", i, verb, digest)
		}
	}
	if got := api.hub.DrainWardenCommands("mach-a"); len(got) != 0 {
		t.Fatalf("a fully delivered drain must leave the FIFO empty, got %d", len(got))
	}
	for _, id := range []string{"m-1", "m-2", "mach-a"} {
		if _, lost := api.hub.UndeliveredCommandSince(id, 0); lost {
			t.Fatalf("delivered frames must leave no loss note (%s)", id)
		}
	}
	if strings.Contains(logs, "warden command undelivered") {
		t.Fatalf("the happy path must stay quiet; got: %s", logs)
	}
}

// ── ITEM 1(b)/ITEM 2: the wake diagnosis stops blaming the wrong machine ────

// TestReconcile_UndeliveredStartBlamesTheStreamNotTheMachine: when the START
// frame never reached the machine, the wake_timeout text "the START was
// dispatched but the agent never came online — check that claude runs ... on the
// target machine" is FALSE in its first clause and sends the owner to the wrong
// machine. It must name the delivery failure instead.
//
// MUTANT: drop the UndeliveredCommandSince branch in stampWakeObservability →
// this test goes RED (the reason still says "check that claude").
func TestReconcile_UndeliveredStartBlamesTheStreamNotTheMachine(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	now := nowSecs()
	s.reconcileMu.Lock()
	first := s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()
	if first.Command != reconcileCmdStart {
		t.Fatalf("expected a START, got %q", first.Command)
	}

	// The warden's stream dies mid-delivery: the frame is popped and never
	// written — exactly what api_infra.go now hands back.
	captureStderr(t, func() {
		pending := s.hub.DrainWardenCommands("mach-live")
		if len(pending) == 0 {
			t.Fatal("the START frame must be on the FIFO")
		}
		s.hub.ReturnUndeliveredCommands("mach-live", pending)
	})

	reloaded, _ := s.dal.GetMember("m-boot")
	s.reconcileMu.Lock()
	second := s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()
	if !second.StartTimedOut {
		t.Fatalf("the lapsed START must be reported as timed out; got %+v", second)
	}

	got, _ := s.dal.GetMember("m-boot")
	if !strings.HasPrefix(got.LastOpReason, wakeTimeoutReasonCode+":") {
		t.Fatalf("still a wake_timeout receipt, got %q", got.LastOpReason)
	}
	for _, want := range []string{"never reached machine", "mach-live", "dropped server-side"} {
		if !strings.Contains(got.LastOpReason, want) {
			t.Errorf("the reason must say the frame never arrived (%q), got %q", want, got.LastOpReason)
		}
	}
	if strings.Contains(got.LastOpReason, "check that claude runs") {
		t.Fatalf("an undelivered START must NOT send the owner to the target machine, got %q",
			got.LastOpReason)
	}
}

// Sentinel pair for the above (alongside TestReconcile_StartTimeoutWritesReceipt,
// which pins the delivered wording): a START that WAS delivered and simply never
// came up must keep the original, correct "go look at that machine" text — a
// mutant that always claimed non-delivery would fail here.
func TestReconcile_DeliveredStartTimeoutKeepsMachineAdvice(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	now := nowSecs()
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()
	// The warden drained it successfully — no frames handed back.
	if got := s.hub.DrainWardenCommands("mach-live"); len(got) == 0 {
		t.Fatal("the START frame must be on the FIFO")
	}

	reloaded, _ := s.dal.GetMember("m-boot")
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()

	got, _ := s.dal.GetMember("m-boot")
	if !strings.Contains(got.LastOpReason, "check that claude runs") {
		t.Fatalf("a DELIVERED start's lapse must keep the target-machine advice, got %q",
			got.LastOpReason)
	}
}

// A loss note from a PREVIOUS attempt must never explain the current one.
func TestReconcile_StaleUndeliveredNoteDoesNotExplainANewWake(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	// An old loss, recorded well before this wake ever started.
	captureStderr(t, func() {
		s.hub.ReturnUndeliveredCommands("mach-live",
			[]wardenCmd{{Subject: "m-boot", Frame: cmdFrame(t, reconcileCmdStart, "m-boot")}})
	})

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	now := nowSecs() + 3600 // this wake happens an hour after that loss
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()
	s.hub.DrainWardenCommands("mach-live") // delivered fine this time

	reloaded, _ := s.dal.GetMember("m-boot")
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()

	got, _ := s.dal.GetMember("m-boot")
	if strings.Contains(got.LastOpReason, "never reached machine") {
		t.Fatalf("a stale loss note must not be attributed to a new wake, got %q",
			got.LastOpReason)
	}
}

// ── ITEM 2 counterfactual: the receipt no longer erases the dispatch clue ────

func wakeTimedOutMember(t *testing.T, dal *DAL, id string) {
	t.Helper()
	no := false
	putGateMember(t, dal, Member{
		ID: id, Kind: KindStaff, DesiredState: DesiredStateOnline,
		LastOp: reconcileCmdStart, LastOpOK: &no, LastOpAt: 1000,
		LastOpReason: wakeTimeoutReasonCode + ": the START never reached machine " +
			"\"mach-a\" — its SSE stream failed mid-delivery",
		LastOpLog: "",
	})
}

// TestFoldCommandResult_CarriesSupersededDispatchClue: the "this machine never
// received anything" clue is what decides whether to go look at that machine.
// The next spawn receipt legitimately wins the slot, but must not destroy it.
//
// MUTANT: delete the supersededDispatchClue block in foldCommandResult (fold
// logText straight through, as HEAD did) → this test goes RED.
func TestFoldCommandResult_CarriesSupersededDispatchClue(t *testing.T) {
	api, dal := newGateTestAPI(t)
	wakeTimedOutMember(t, dal, "m-1")

	api.foldCommandResult(map[string]any{
		"member_id": "m-1", "rpc": "start", "ok": false,
		"reason": "spawn_failed: tmux new-session exited 1",
		"log":    "tmux: server exited unexpectedly",
		"at":     2000.0,
	}, triggerServer, "")

	got, err := dal.GetMember("m-1")
	if err != nil || got == nil {
		t.Fatalf("reload: %v", err)
	}
	// The receipt still wins the slot — it is the newer, execution-level truth.
	if got.LastOpReason != "spawn_failed: tmux new-session exited 1" {
		t.Fatalf("the receipt must own last_op_reason, got %q", got.LastOpReason)
	}
	// ...but the displaced dispatch clue survives, readable, in last_op_log.
	for _, want := range []string{
		"superseded dispatch diagnosis", wakeTimeoutReasonCode,
		"never reached machine", "tmux: server exited unexpectedly",
	} {
		if !strings.Contains(got.LastOpLog, want) {
			t.Errorf("last_op_log must still carry %q; got %q", want, got.LastOpLog)
		}
	}
}

// The carry-forward is bounded to ONE hop: the second receipt has no dispatch
// diagnosis left to carry, so the log never grows a chain.
func TestFoldCommandResult_SupersededClueDoesNotChain(t *testing.T) {
	api, dal := newGateTestAPI(t)
	wakeTimedOutMember(t, dal, "m-1")

	for i := 0; i < 3; i++ {
		api.foldCommandResult(map[string]any{
			"member_id": "m-1", "rpc": "start", "ok": false,
			"reason": "spawn_failed: again", "log": "boom", "at": 2000.0,
		}, triggerServer, "")
	}
	got, _ := dal.GetMember("m-1")
	if n := strings.Count(got.LastOpLog, "superseded dispatch diagnosis"); n != 0 {
		t.Fatalf("only the FIRST fold carries the clue; got %d copies in %q", n, got.LastOpLog)
	}
}

// The clue survives a log dump long enough to hit the clamp — prefixing, not
// appending, is the load-bearing choice.
func TestFoldCommandResult_SupersededClueSurvivesTheLogClamp(t *testing.T) {
	api, dal := newGateTestAPI(t)
	wakeTimedOutMember(t, dal, "m-1")

	api.foldCommandResult(map[string]any{
		"member_id": "m-1", "rpc": "start", "ok": false,
		"reason": "spawn_failed: x",
		"log":    strings.Repeat("z", 4*commandResultLogMax),
		"at":     2000.0,
	}, triggerServer, "")

	got, _ := dal.GetMember("m-1")
	if len(got.LastOpLog) > commandResultLogMax {
		t.Fatalf("the clamp must still hold, got %d bytes", len(got.LastOpLog))
	}
	if !strings.Contains(got.LastOpLog, "superseded dispatch diagnosis") {
		t.Fatalf("the clue must survive the clamp, got %q", got.LastOpLog[:80])
	}
}

// ── ITEM 2 sentinel: an ordinary receipt fold is byte-for-byte unchanged ────

func TestFoldCommandResult_OrdinaryReceiptUnchanged(t *testing.T) {
	api, dal := newGateTestAPI(t)
	no := false
	putGateMember(t, dal, Member{
		ID: "m-1", Kind: KindStaff, DesiredState: DesiredStateOnline,
		LastOp: "start", LastOpOK: &no, LastOpAt: 1000,
		LastOpReason: "spawn_failed: earlier failure", LastOpLog: "earlier log",
	})

	api.foldCommandResult(map[string]any{
		"member_id": "m-1", "rpc": "start", "ok": true,
		"reason": "", "log": "spawned", "at": 2000.0,
	}, triggerServer, "")

	got, _ := dal.GetMember("m-1")
	if got.LastOpLog != "spawned" {
		t.Fatalf("an ordinary fold must replace the log verbatim, got %q", got.LastOpLog)
	}
	if got.LastOpReason != "" || got.LastOp != "start" ||
		got.LastOpOK == nil || !*got.LastOpOK || got.LastOpAt != 2000.0 {
		t.Fatalf("an ordinary fold must be unchanged, got %+v", got)
	}
}
