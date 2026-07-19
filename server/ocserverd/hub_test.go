package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// parseSSEFrame splits one "id: N\ndata: {...}\n\n" wire text into the id
// line and the decoded JSON envelope.
func parseSSEFrame(t *testing.T, raw []byte) (string, map[string]any) {
	t.Helper()
	text := string(raw)
	if !strings.HasSuffix(text, "\n\n") {
		t.Fatalf("frame must end with a blank line: %q", text)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "id: ") || !strings.HasPrefix(lines[1], "data: ") {
		t.Fatalf("frame shape: %q", text)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &envelope); err != nil {
		t.Fatalf("frame data is not JSON: %v", err)
	}
	return strings.TrimPrefix(lines[0], "id: "), envelope
}

func TestPublish(t *testing.T) {
	t.Run("frame envelope: id==seq==epoch, six keys, pre-increment from 1", func(t *testing.T) {
		h := NewHub()
		l, err := h.Connect("", "")
		if err != nil {
			t.Fatal(err)
		}
		h.Publish("chat", "patch", "chat", "owner::c-1", map[string]any{"id": "c-1", "from": "owner", "to": "m-1"}, audienceAll(), "owner")
		id, envelope := parseSSEFrame(t, l.pop())
		if id != "1" {
			t.Fatalf("first seq must be 1: id %q", id)
		}
		for _, k := range []string{"seq", "topic", "op", "data", "ts", "trigger"} {
			if _, ok := envelope[k]; !ok {
				t.Fatalf("envelope missing %q: %v", k, envelope)
			}
		}
		if len(envelope) != 6 {
			t.Fatalf("envelope must carry exactly six keys: %v", envelope)
		}
		if envelope["trigger"] != "owner" {
			t.Fatalf("trigger must carry the publish actor verbatim: %v", envelope)
		}
		if envelope["seq"] != float64(1) || envelope["topic"] != "chat" || envelope["op"] != "patch" {
			t.Fatalf("envelope values: %v", envelope)
		}
		inner := envelope["data"].(map[string]any)
		if inner["epoch"] != envelope["seq"] {
			t.Fatalf("epoch must equal seq: %v", inner)
		}
		if inner["deleted"] != false || inner["key"] != "owner::c-1" || inner["entity"] != "chat" {
			t.Fatalf("inner: %v", inner)
		}
		payload := inner["payload"].(map[string]any)
		if payload["from"] != "owner" || payload["to"] != "m-1" || payload["id"] != "c-1" {
			t.Fatalf("payload: %v", payload)
		}
	})

	t.Run("remove op forces deleted:true payload:null", func(t *testing.T) {
		h := NewHub()
		l, _ := h.Connect("", "")
		h.Publish("member", "remove", "member", "owner::m-9", map[string]any{"id": "m-9"}, audienceAll(), "owner")
		_, envelope := parseSSEFrame(t, l.pop())
		inner := envelope["data"].(map[string]any)
		if inner["deleted"] != true || inner["payload"] != nil {
			t.Fatalf("remove must ride deleted:true payload:null: %v", inner)
		}
	})

	t.Run("seq strictly monotonic in publish order per listener", func(t *testing.T) {
		h := NewHub()
		l, _ := h.Connect("", "")
		for range 3 {
			h.Publish("chat", "patch", "chat", "k", nil, audienceAll(), "owner")
		}
		var seqs []float64
		for {
			frame := l.pop()
			if frame == nil {
				break
			}
			_, envelope := parseSSEFrame(t, frame)
			seqs = append(seqs, envelope["seq"].(float64))
		}
		if len(seqs) != 3 || !(seqs[0] < seqs[1] && seqs[1] < seqs[2]) {
			t.Fatalf("seqs: %v", seqs)
		}
	})

	t.Run("blank trigger folds to server attribution (never an empty field)", func(t *testing.T) {
		// spec/sse.md §2.3: a producer that forgot its attribution must not
		// mint an empty trigger — the client's echo rule reads blank as
		// unknown, but OUR wire always names an actor ("server" fallback).
		h := NewHub()
		l, _ := h.Connect("", "")
		h.Publish("member", "patch", "member", "k", nil, audienceAll(), "")
		_, envelope := parseSSEFrame(t, l.pop())
		if envelope["trigger"] != triggerServer {
			t.Fatalf("blank trigger must fold to %q: %v", triggerServer, envelope)
		}
	})

	t.Run("agent trigger rides verbatim (the listener's echo key)", func(t *testing.T) {
		h := NewHub()
		l, _ := h.Connect("m-a", "")
		h.Publish("task", "patch", "task", "k", nil, audienceMembers("m-a"), "m-a")
		_, envelope := parseSSEFrame(t, l.pop())
		if envelope["trigger"] != "m-a" {
			t.Fatalf("agent trigger must ride verbatim: %v", envelope)
		}
	})

	t.Run("closed topic set enforced at the seam", func(t *testing.T) {
		h := NewHub()
		l, _ := h.Connect("", "")
		h.Publish("machine_alias", "patch", "machine_alias", "k", nil, audienceAll(), "owner")
		if frame := l.pop(); frame != nil {
			t.Fatalf("a non-spec topic must be dropped: %q", frame)
		}
	})

	t.Run("audienceAll fans to every listener; a disconnected listener gets nothing", func(t *testing.T) {
		h := NewHub()
		a, _ := h.Connect("", "")
		b, _ := h.Connect("m-1", "")
		h.Disconnect(b)
		h.Publish("member", "patch", "member", "k", nil, audienceAll(), "owner")
		if a.pop() == nil {
			t.Fatal("live listener must receive the frame")
		}
		if b.pop() != nil {
			t.Fatal("disconnected listener must not receive the frame")
		}
	})
}

// TestPublishAudience pins the per-recipient routing contract (spec/sse.md §4,
// T-30d7): an AGENT connection receives a frame iff it is addressed; the
// owner/dashboard connection (MemberID=="") receives EVERY frame regardless.
// These are the bidirectional assertions owner asked for — unrelated members
// get nothing, related members get theirs, owner gets全量.
func TestPublishAudience(t *testing.T) {
	// setup: one owner connection + three agents (a, b, c).
	newFleet := func() (h *Hub, owner, a, b, c *hubListener) {
		h = NewHub()
		owner, _ = h.Connect("", "")
		a, _ = h.Connect("m-a", "")
		b, _ = h.Connect("m-b", "")
		c, _ = h.Connect("m-c", "")
		return
	}
	got := func(l *hubListener) bool { return l.pop() != nil }

	t.Run("audienceMembers: only the addressed agents + owner receive", func(t *testing.T) {
		h, owner, a, b, c := newFleet()
		h.Publish("chat", "patch", "chat", "k", nil, audienceMembers("m-a", "m-b"), "owner")
		if !got(a) || !got(b) {
			t.Fatal("addressed agents must receive")
		}
		if got(c) {
			t.Fatal("an UNADDRESSED agent must receive nothing (the Slack-Seth waste)")
		}
		if !got(owner) {
			t.Fatal("the owner/dashboard connection must be全量")
		}
	})

	t.Run("audienceOwnerOnly: no agent receives, owner still does", func(t *testing.T) {
		h, owner, a, b, c := newFleet()
		h.Publish("monitoring", "signal", "monitoring", "k", nil, audienceOwnerOnly(), "w-1")
		if got(a) || got(b) || got(c) {
			t.Fatal("an owner-only topic must reach NO agent")
		}
		if !got(owner) {
			t.Fatal("the owner/dashboard connection must still receive owner-only frames")
		}
	})

	t.Run("member self-delta reaches the subject (wind-down correctness)", func(t *testing.T) {
		// The graceful wind-down / recycle hooks fire ONLY on a member delta
		// naming self (cli/ocagent shouldWindDown); dropping it would break
		// graceful stop. Guard it explicitly.
		h, owner, a, b, c := newFleet()
		h.Publish("member", "patch", "member", "owner::m-b", nil, audienceMembers("m-b"), "owner")
		if !got(b) {
			t.Fatal("a member's OWN delta must reach it — else graceful stop/recycle breaks")
		}
		if got(a) || got(c) {
			t.Fatal("another member's delta must not wake unrelated agents")
		}
		if !got(owner) {
			t.Fatal("owner cockpit must see every member delta")
		}
	})

	t.Run("audienceMembers drops blank ids (unassigned executor / absent creator)", func(t *testing.T) {
		h, owner, a, b, c := newFleet()
		// A task with executor m-a but no creator ("") — only m-a + owner.
		h.Publish("task", "patch", "task", "k", nil, audienceMembers("m-a", ""), "m-a")
		if !got(a) {
			t.Fatal("the executor must receive")
		}
		if got(b) || got(c) {
			t.Fatal("a blank id must not widen the audience")
		}
		if !got(owner) {
			t.Fatal("owner全量")
		}
	})
}

// ── dual-SSE takeover + anti-flap throttle (spec/sse.md §5.1, T-b315) ────────

// kickedClosed reports whether l's kicked channel has been closed.
func kickedClosed(l *hubListener) bool {
	select {
	case <-l.kicked:
		return true
	default:
		return false
	}
}

func listenerCount(h *Hub) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.listeners)
}

func TestConnectTakeover(t *testing.T) {
	h := NewHub()
	a, err := h.Connect("m-1", "mach-a")
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}
	b, err := h.Connect("m-1", "mach-b")
	if err != nil {
		t.Fatalf("takeover must admit the new connection, got %v", err)
	}
	if !kickedClosed(a) {
		t.Fatal("the displaced listener's kicked channel must be closed")
	}
	if kickedClosed(b) {
		t.Fatal("the new listener must not be kicked")
	}
	if b.Gen <= a.Gen {
		t.Fatalf("generation must be strictly increasing across the handover: old=%d new=%d", a.Gen, b.Gen)
	}
	if n := listenerCount(h); n != 1 {
		t.Fatalf("exactly one listener must hold the slot after takeover, got %d", n)
	}
	if !h.IsOnline("m-1") {
		t.Fatal("the member must stay online across the handover")
	}
	// The machine claim follows the NEW connection (reconnect-from-elsewhere).
	if got := h.MachineOf("m-1"); got != "mach-b" {
		t.Fatalf("MachineOf must reflect the new connection's claim: %q", got)
	}
	if agents := h.AgentsOnMachine("mach-a"); len(agents) != 0 {
		t.Fatalf("the displaced claim must be gone: %v", agents)
	}
}

func TestMachinesOfReflectsTakeoverSingleLiveMachine(t *testing.T) {
	// The set generalization of MachineOf. Dual-SSE takeover keeps ONE live
	// listener per member, so after a reconnect-from-elsewhere the set carries
	// exactly the NEW machine — never the displaced one (design-note §4 叉口 I:
	// set-shaped API, single-valued in practice today).
	h := NewHub()
	if got := h.MachinesOf("m-1"); got != nil {
		t.Fatalf("no connection → empty set, got %v", got)
	}
	h.Connect("m-1", "mach-a")
	if got := h.MachinesOf("m-1"); len(got) != 1 || got[0] != "mach-a" {
		t.Fatalf("one connection → {mach-a}, got %v", got)
	}
	h.Connect("m-1", "mach-b") // takeover from elsewhere
	got := h.MachinesOf("m-1")
	if len(got) != 1 || got[0] != "mach-b" {
		t.Fatalf("after takeover the set must be exactly the NEW machine {mach-b}, got %v", got)
	}
}

func TestMachinesOfDropsBlankClaims(t *testing.T) {
	// An owner connection (member "") and a claim-less listener contribute
	// nothing to any member's appearance set.
	h := NewHub()
	h.Connect("", "")    // owner/dashboard
	h.Connect("m-2", "") // agent with no machine token
	if got := h.MachinesOf(""); got != nil {
		t.Fatalf("owner id has no appearance set, got %v", got)
	}
	if got := h.MachinesOf("m-2"); got != nil {
		t.Fatalf("a blank claim must not appear in the set, got %v", got)
	}
}

func TestRepeatedTakeoverKicksEachIncumbentExactlyOnce(t *testing.T) {
	// A→B→C: each displaced listener's channel closes exactly once (closing
	// happens strictly after the listener left the map, so a second takeover
	// can never re-close it — no panic possible).
	h := NewHub()
	a, _ := h.Connect("m-1", "")
	b, _ := h.Connect("m-1", "")
	c, err := h.Connect("m-1", "")
	if err != nil {
		t.Fatalf("second takeover: %v", err)
	}
	if !kickedClosed(a) || !kickedClosed(b) {
		t.Fatal("both displaced listeners must be kicked")
	}
	if kickedClosed(c) {
		t.Fatal("the incumbent must not be kicked")
	}
	if n := listenerCount(h); n != 1 {
		t.Fatalf("one listener after the chain, got %d", n)
	}
}

func TestTakeoverGenerationMonotonic(t *testing.T) {
	h := NewHub()
	var gens []int64
	admit := func(member string) {
		l, err := h.Connect(member, "")
		if err != nil {
			t.Fatalf("connect %q: %v", member, err)
		}
		gens = append(gens, l.Gen)
	}
	admit("")    // owner
	admit("m-1") // fresh
	admit("m-2") // fresh
	admit("m-1") // takeover
	admit("")    // second owner
	admit("m-2") // takeover
	for i := 1; i < len(gens); i++ {
		if gens[i] <= gens[i-1] {
			t.Fatalf("generations must be strictly increasing with no duplicates: %v", gens)
		}
	}
	if gens[0] != 1 {
		t.Fatalf("first generation must be 1 (pre-increment): %v", gens)
	}
}

func TestTakeoverThrottle(t *testing.T) {
	h := NewHub()
	now := time.Unix(1_752_192_000, 0)
	h.clock = func() time.Time { return now }
	incumbent, _ := h.Connect("m-1", "")
	for i := range takeoverBurst {
		l, err := h.Connect("m-1", "")
		if err != nil {
			t.Fatalf("takeover %d within the burst must be admitted: %v", i+1, err)
		}
		incumbent = l
	}
	// Takeover burst+1 inside the window: refused with the THROTTLED message.
	if _, err := h.Connect("m-1", ""); !errors.Is(err, errDualSSEThrottled) {
		t.Fatalf("over-budget takeover must return errDualSSEThrottled, got %v", err)
	}
	// The incumbent is unaffected by the refusal.
	if kickedClosed(incumbent) {
		t.Fatal("a throttled connect must not kick the incumbent")
	}
	if !h.IsOnline("m-1") {
		t.Fatal("the member must stay online through the refusal")
	}
	// Message wording distinguishes the throttle fallback from the old
	// blanket refusal (client/serve log diagnosability).
	if errDualSSEThrottled.Error() == errDualSSE.Error() {
		t.Fatal("the throttled message must differ from errDualSSE")
	}
}

func TestTakeoverWindowSlides(t *testing.T) {
	h := NewHub()
	now := time.Unix(1_752_192_000, 0)
	h.clock = func() time.Time { return now }
	h.Connect("m-1", "")
	for range takeoverBurst {
		if _, err := h.Connect("m-1", ""); err != nil {
			t.Fatalf("in-burst takeover: %v", err)
		}
	}
	if _, err := h.Connect("m-1", ""); !errors.Is(err, errDualSSEThrottled) {
		t.Fatalf("budget must be spent: %v", err)
	}
	// Slide past the window: the stamps expire and takeover works again.
	now = now.Add(takeoverWindow + time.Second)
	if _, err := h.Connect("m-1", ""); err != nil {
		t.Fatalf("takeover must succeed once the window slid out: %v", err)
	}
}

func TestOwnerConnectionsExempt(t *testing.T) {
	h := NewHub()
	var conns []*hubListener
	for i := 0; i < takeoverBurst+2; i++ {
		l, err := h.Connect("", "")
		if err != nil {
			t.Fatalf("owner connection %d must always be admitted: %v", i, err)
		}
		conns = append(conns, l)
	}
	for i, l := range conns {
		if kickedClosed(l) {
			t.Fatalf("owner connection %d must never be kicked", i)
		}
	}
	if n := listenerCount(h); n != takeoverBurst+2 {
		t.Fatalf("all owner connections must coexist: %d", n)
	}
	h.mu.Lock()
	kicks := len(h.kicks)
	h.mu.Unlock()
	if kicks != 0 {
		t.Fatalf("owner connections must never enter the throttle accounting: %d entries", kicks)
	}
}

func TestDisconnectLastForMember(t *testing.T) {
	h := NewHub()
	a, _ := h.Connect("m-1", "")
	b, _ := h.Connect("m-1", "") // takeover displaces a
	if h.Disconnect(a) {
		t.Fatal("a kicked listener's Disconnect must report last=false (the new listener still holds)")
	}
	if !h.IsOnline("m-1") {
		t.Fatal("the member must still be online after the kicked listener's cleanup")
	}
	if !h.Disconnect(b) {
		t.Fatal("removing the LAST listener must report last=true")
	}
	if h.Disconnect(b) {
		t.Fatal("a repeated Disconnect must be an idempotent last=false")
	}
	owner, _ := h.Connect("", "")
	if h.Disconnect(owner) {
		t.Fatal("an owner listener must always report last=false")
	}
	if h.Disconnect(nil) {
		t.Fatal("nil listener: last=false")
	}
}

func TestPublishDuringTakeover(t *testing.T) {
	// Every frame lands on EXACTLY one of old/new — no double delivery, no
	// dropped-slot window: before the handover the old listener holds the
	// slot, after it the new one does, and the handover itself is atomic
	// under the same lock Publish takes.
	h := NewHub()
	old, _ := h.Connect("m-1", "")
	h.Publish("chat", "patch", "chat", "k-before", nil, audienceMembers("m-1"), "owner")
	fresh, err := h.Connect("m-1", "")
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	h.Publish("chat", "patch", "chat", "k-after", nil, audienceMembers("m-1"), "owner")
	count := func(l *hubListener) int {
		n := 0
		for l.pop() != nil {
			n++
		}
		return n
	}
	if got := count(old); got != 1 {
		t.Fatalf("the pre-handover frame must land on the OLD listener only: %d", got)
	}
	if got := count(fresh); got != 1 {
		t.Fatalf("the post-handover frame must land on the NEW listener only: %d", got)
	}
}

func TestJsonFloat(t *testing.T) {
	whole, err := json.Marshal(jsonFloat(1752192000))
	if err != nil || string(whole) != "1752192000.0" {
		t.Fatalf("a whole-second ts must still read back as float: %s %v", whole, err)
	}
	frac, _ := json.Marshal(jsonFloat(1752192000.125))
	if string(frac) != "1752192000.125" {
		t.Fatalf("fractional ts: %s", frac)
	}
}

func TestEnqueueWardenCommand(t *testing.T) {
	h := NewHub()
	h.EnqueueWardenCommand("w-1", []byte("frame-1"))
	h.EnqueueWardenCommand("w-1", []byte("frame-2"))
	h.EnqueueWardenCommand("w-2", []byte("other"))

	drained := h.DrainWardenCommands("w-1")
	if len(drained) != 2 || string(drained[0]) != "frame-1" || string(drained[1]) != "frame-2" {
		t.Fatalf("drain must pop all pending in FIFO order: %q", drained)
	}
	if again := h.DrainWardenCommands("w-1"); again != nil {
		t.Fatalf("at-most-once: a second drain must be empty, got %q", again)
	}
	if other := h.DrainWardenCommands("w-2"); len(other) != 1 {
		t.Fatalf("queues are per-warden: %q", other)
	}
	if unknown := h.DrainWardenCommands("w-none"); unknown != nil {
		t.Fatalf("unknown warden drains nothing: %q", unknown)
	}
}
