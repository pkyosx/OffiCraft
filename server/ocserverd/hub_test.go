package main

// hub_test.go — the behaviour of server/ocserverd/hub.go: what the connection
// registry projects, exactly which listeners a Publish reaches and with which
// wire text, and what the per-warden command FIFO holds, hands back and writes
// to stderr on every one of its paths.

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"
)

// hubTestStderr runs fn with os.Stderr redirected and answers with everything
// written to it — the hub's operator-visible half is stderr and nothing else.
func hubTestStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = w
	collected := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(r)
		collected <- string(raw)
	}()
	fn()
	os.Stderr = saved
	w.Close()
	out := <-collected
	r.Close()
	return out
}

// hubTestFrames drains one listener and decodes every buffered frame, so a
// connection's whole backlog is compared at once. An empty backlog answers the
// empty slice, never nil.
func hubTestFrames(t *testing.T, l *hubListener) []any {
	t.Helper()
	out := []any{}
	for {
		raw := l.pop()
		if raw == nil {
			return out
		}
		out = append(out, apiDecodeSSEFrame(t, raw))
	}
}

// hubTestWantFrames asserts one connection received exactly want, in publish
// order. No frame at all is the zero-argument call.
func hubTestWantFrames(t *testing.T, path string, l *hubListener, want ...map[string]any) {
	t.Helper()
	expected := make([]any, len(want))
	for i := range want {
		expected[i] = want[i]
	}
	apiWantValue(t, path, any(hubTestFrames(t, l)), any(expected))
}

// hubTestCommandFrame is one warden-command wire frame in the shape
// decodeWardenCommandFrame reads back: the topic envelope, the rpc verb and
// the member the order acts on.
func hubTestCommandFrame(t *testing.T, verb, memberID string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"topic": wardenCommandTopic,
		"data":  map[string]any{"rpc": verb, "args": map[string]any{"member_id": memberID}},
	})
	if err != nil {
		t.Fatalf("marshal command frame: %v", err)
	}
	return []byte("data: " + string(raw) + "\n\n")
}

// hubTestStore is the injectable wardenCommandStore: it records every call and
// can be told to fail any one of them.
type hubTestStore struct {
	rows      []WardenCommand
	deleted   []string
	sweeps    []float64
	sweptN    int64
	sweepErr  error
	putErr    error
	deleteErr error
	listErr   error
}

func (s *hubTestStore) PutWardenCommand(c WardenCommand) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.rows = append(s.rows, c)
	return nil
}

func (s *hubTestStore) DeleteWardenCommand(wardenID, verb, memberID string) error {
	s.deleted = append(s.deleted, wardenID+"/"+verb+"/"+memberID)
	return s.deleteErr
}

func (s *hubTestStore) ListWardenCommands() ([]WardenCommand, error) {
	return s.rows, s.listErr
}

func (s *hubTestStore) DeleteWardenCommandsBefore(cutoff float64) (int64, error) {
	s.sweeps = append(s.sweeps, cutoff)
	return s.sweptN, s.sweepErr
}

// hubTestNow pins the hub clock so the stamps a test reads back are literals.
const hubTestNow = 1700000000.0

func hubTestPinned() *Hub {
	h := NewHub()
	h.clock = func() time.Time { return time.Unix(1700000000, 0) }
	return h
}

func TestPush(t *testing.T) {
	t.Run("pushed frames stack up in call order and the backlog starts empty", func(t *testing.T) {
		l := &hubListener{}
		if got := l.pop(); got != nil {
			t.Fatalf("a fresh listener must hold nothing, got %q", got)
		}
		l.push([]byte("first"))
		l.push([]byte("second"))
		l.push([]byte("first"))
		var got []string
		for {
			raw := l.pop()
			if raw == nil {
				break
			}
			got = append(got, string(raw))
		}
		want := []string{"first", "second", "first"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("backlog = %q, want %q", got, want)
		}
	})

	t.Run("push stores the caller's bytes without copying them, so the wire text is whatever the publisher built", func(t *testing.T) {
		l := &hubListener{}
		frame := []byte("id: 7\ndata: {}\n\n")
		l.push(frame)
		got := l.pop()
		if string(got) != "id: 7\ndata: {}\n\n" {
			t.Fatalf("popped %q, want the frame as pushed", got)
		}
	})
}

func TestPop(t *testing.T) {
	t.Run("pop returns the oldest frame each call and nil once the backlog is drained", func(t *testing.T) {
		l := &hubListener{}
		l.push([]byte("a"))
		l.push([]byte("b"))
		if got := l.pop(); string(got) != "a" {
			t.Fatalf("first pop = %q, want %q", got, "a")
		}
		if got := l.pop(); string(got) != "b" {
			t.Fatalf("second pop = %q, want %q", got, "b")
		}
		if got := l.pop(); got != nil {
			t.Fatalf("third pop = %q, want nil", got)
		}
	})

	t.Run("a drained listener accepts new frames again rather than staying empty", func(t *testing.T) {
		l := &hubListener{}
		l.push([]byte("a"))
		l.pop()
		if got := l.pop(); got != nil {
			t.Fatalf("want nil on the empty backlog, got %q", got)
		}
		l.push([]byte("c"))
		if got := l.pop(); string(got) != "c" {
			t.Fatalf("pop after refill = %q, want %q", got, "c")
		}
	})
}

func TestNewHub(t *testing.T) {
	t.Run("a fresh hub holds no connection, no queued command and no loss note, and its counters start from zero", func(t *testing.T) {
		h := NewHub()
		if got := h.OnlineMembers(); len(got) != 0 {
			t.Fatalf("OnlineMembers on a fresh hub = %v, want empty", got)
		}
		if got := h.AgentsOnMachine("m-1"); got != nil {
			t.Fatalf("AgentsOnMachine on a fresh hub = %#v, want nil", got)
		}
		if got := h.PendingWardenCommands("w1"); got != 0 {
			t.Fatalf("PendingWardenCommands on a fresh hub = %d, want 0", got)
		}
		if got := h.DrainWardenCommands("w1"); got != nil {
			t.Fatalf("DrainWardenCommands on a fresh hub = %#v, want nil", got)
		}
		if _, ok := h.UndeliveredCommandSince("kip", 0); ok {
			t.Fatalf("a fresh hub must carry no loss note for anybody")
		}

		l, err := h.Connect("kip", "m-1")
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		if l.Gen != 1 {
			t.Fatalf("the first connection's Gen = %d, want 1", l.Gen)
		}
		h.Publish("member", "upsert", "member", "kip", nil, audienceAll(), "owner")
		hubTestWantFrames(t, "first-publish", l, map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "upsert",
			"data": map[string]any{
				"entity": "member", "key": "kip", "epoch": 1,
				"deleted": false, "payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("the default clock is the wall clock, so an enqueue stamps roughly now rather than the zero time", func(t *testing.T) {
		h := NewHub()
		store := &hubTestStore{}
		hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		before := float64(time.Now().Add(-time.Minute).Unix())
		h.EnqueueWardenCommandFor("w1", "m-9", hubTestCommandFrame(t, "update", "m-9"))
		if len(store.rows) != 1 {
			t.Fatalf("want one persisted row, got %+v", store.rows)
		}
		if store.rows[0].EnqueuedTS < before {
			t.Fatalf("EnqueuedTS = %v, want a wall-clock stamp at or after %v", store.rows[0].EnqueuedTS, before)
		}
	})
}

func TestConnect(t *testing.T) {
	t.Run("the first connection for a member is admitted with generation 1 and projects it online with its machine claim", func(t *testing.T) {
		h := hubTestPinned()
		out := hubTestStderr(t, func() {
			l, err := h.Connect("kip", "m-1")
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if l.MemberID != "kip" || l.MachineID != "m-1" || l.Gen != 1 {
				t.Fatalf("listener = member %q machine %q gen %d, want kip on m-1 at gen 1", l.MemberID, l.MachineID, l.Gen)
			}
			select {
			case <-l.kicked:
				t.Fatalf("a freshly admitted listener must not be kicked")
			default:
			}
		})
		if out != "" {
			t.Fatalf("a plain admit must print nothing, got %q", out)
		}
		if !h.IsOnline("kip") || h.MachineOf("kip") != "m-1" {
			t.Fatalf("kip online=%v machine=%q, want true / m-1", h.IsOnline("kip"), h.MachineOf("kip"))
		}
	})

	t.Run("the owner connection is admitted, never projects online and is exempt from takeover", func(t *testing.T) {
		h := hubTestPinned()
		first, err := h.Connect("", "")
		if err != nil {
			t.Fatalf("Connect owner: %v", err)
		}
		out := hubTestStderr(t, func() {
			second, err := h.Connect("", "")
			if err != nil {
				t.Fatalf("Connect second owner: %v", err)
			}
			if second.Gen != 2 {
				t.Fatalf("second owner Gen = %d, want 2", second.Gen)
			}
		})
		if out != "" {
			t.Fatalf("a second owner connection must not be reported as a takeover, got %q", out)
		}
		select {
		case <-first.kicked:
			t.Fatalf("the first owner connection must not be kicked")
		default:
		}
		if got := h.OnlineMembers(); len(got) != 0 {
			t.Fatalf("OnlineMembers with two owner connections = %v, want empty", got)
		}
	})

	t.Run("a second connection for the same member takes the slot over: the incumbent is kicked and only the newcomer is fanned to", func(t *testing.T) {
		h := hubTestPinned()
		old, err := h.Connect("kip", "m-1")
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		var fresh *hubListener
		out := hubTestStderr(t, func() {
			fresh, err = h.Connect("kip", "m-2")
			if err != nil {
				t.Fatalf("takeover Connect: %v", err)
			}
		})
		want := "[sse] takeover: member=kip old_gen=1 new_gen=2 incumbent_age=0s (kicks_in_window=1)\n"
		if out != want {
			t.Fatalf("takeover log:\n got %q\nwant %q", out, want)
		}
		select {
		case <-old.kicked:
		default:
			t.Fatalf("the displaced listener's kicked channel must be closed")
		}
		if fresh.Gen != 2 || h.MachineOf("kip") != "m-2" {
			t.Fatalf("gen=%d machine=%q, want 2 / m-2", fresh.Gen, h.MachineOf("kip"))
		}
		h.Publish("member", "upsert", "member", "kip", nil, audienceAll(), "server")
		hubTestWantFrames(t, "kicked", old)
		hubTestWantFrames(t, "fresh", fresh, map[string]any{
			"seq": 1, "topic": "member", "op": "upsert",
			"data": map[string]any{
				"entity": "member", "key": "kip", "epoch": 1,
				"deleted": false, "payload": nil,
			},
			"ts": apiAnyNumber, "trigger": "server",
		})
		if !h.IsOnline("kip") {
			t.Fatalf("the member must stay online across the handover")
		}
	})

	t.Run("the fourth takeover inside the window is refused with the throttled 409 error and leaves the incumbent connected", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("kip", "m-1")
		var incumbent *hubListener
		out := hubTestStderr(t, func() {
			for i := 2; i <= 4; i++ {
				l, err := h.Connect("kip", "m-1")
				if err != nil {
					t.Fatalf("takeover %d: %v", i, err)
				}
				incumbent = l
			}
			l, err := h.Connect("kip", "m-9")
			if l != nil {
				t.Fatalf("the over-budget connect must be refused, got a listener")
			}
			if !errors.Is(err, errDualSSEThrottled) {
				t.Fatalf("err = %v, want errDualSSEThrottled", err)
			}
			if err.Error() != "member already holds a live SSE connection (takeover throttled: too many handovers; dual live clients suspected)" {
				t.Fatalf("refusal text = %q", err.Error())
			}
		})
		want := "[sse] takeover: member=kip old_gen=1 new_gen=2 incumbent_age=0s (kicks_in_window=1)\n" +
			"[sse] takeover: member=kip old_gen=2 new_gen=3 incumbent_age=0s (kicks_in_window=2)\n" +
			"[sse] takeover: member=kip old_gen=3 new_gen=4 incumbent_age=0s (kicks_in_window=3)\n" +
			"[sse] takeover throttled: member=kip kicks=3 window=1m0s — refusing with 409 (two live clients suspected)\n"
		if out != want {
			t.Fatalf("takeover trail:\n got %q\nwant %q", out, want)
		}
		select {
		case <-incumbent.kicked:
			t.Fatalf("a refused connect must not kick the incumbent")
		default:
		}
		if h.MachineOf("kip") != "m-1" {
			t.Fatalf("the incumbent's claim must survive, MachineOf = %q", h.MachineOf("kip"))
		}
		h.Publish("member", "upsert", "member", "kip", nil, audienceAll(), "server")
		hubTestWantFrames(t, "incumbent", incumbent, map[string]any{
			"seq": 1, "topic": "member", "op": "upsert",
			"data": map[string]any{
				"entity": "member", "key": "kip", "epoch": 1,
				"deleted": false, "payload": nil,
			},
			"ts": apiAnyNumber, "trigger": "server",
		})
	})

	t.Run("the kick budget slides: once the window has passed the next connect takes over again instead of being refused", func(t *testing.T) {
		h := NewHub()
		now := time.Unix(1700000000, 0)
		h.clock = func() time.Time { return now }
		hubTestStderr(t, func() {
			for i := 0; i < 4; i++ {
				h.Connect("kip", "m-1")
			}
			if _, err := h.Connect("kip", "m-1"); !errors.Is(err, errDualSSEThrottled) {
				t.Fatalf("the fifth connect must be throttled, got %v", err)
			}
			now = now.Add(takeoverWindow + time.Second)
			l, err := h.Connect("kip", "m-2")
			if err != nil || l == nil {
				t.Fatalf("after the window slides the connect must succeed, got %v", err)
			}
		})
		if h.MachineOf("kip") != "m-2" {
			t.Fatalf("MachineOf = %q, want the post-window claim m-2", h.MachineOf("kip"))
		}
	})
}

func TestDisconnect(t *testing.T) {
	t.Run("removing a member's only connection reports the last-disconnect edge and drops the online projection", func(t *testing.T) {
		h := hubTestPinned()
		l, _ := h.Connect("kip", "m-1")
		if !h.Disconnect(l) {
			t.Fatalf("Disconnect of the only listener must report true")
		}
		if h.IsOnline("kip") || h.MachineOf("kip") != "" {
			t.Fatalf("kip online=%v machine=%q, want offline with no claim", h.IsOnline("kip"), h.MachineOf("kip"))
		}
		if got := h.OnlineMembers(); len(got) != 0 {
			t.Fatalf("OnlineMembers = %v, want empty", got)
		}
	})

	t.Run("a second Disconnect of the same listener reports false — the edge fires once", func(t *testing.T) {
		h := hubTestPinned()
		l, _ := h.Connect("kip", "m-1")
		h.Disconnect(l)
		if h.Disconnect(l) {
			t.Fatalf("the repeat Disconnect must report false")
		}
	})

	t.Run("a kicked listener's own Disconnect reports false because the takeover already removed it and the newcomer holds the slot", func(t *testing.T) {
		h := hubTestPinned()
		old, _ := h.Connect("kip", "m-1")
		var fresh *hubListener
		hubTestStderr(t, func() { fresh, _ = h.Connect("kip", "m-2") })
		if h.Disconnect(old) {
			t.Fatalf("the displaced listener must not report the last-disconnect edge")
		}
		if !h.IsOnline("kip") {
			t.Fatalf("the member must still be online through the newcomer")
		}
		if !h.Disconnect(fresh) {
			t.Fatalf("disconnecting the surviving listener must report the edge")
		}
		if h.IsOnline("kip") {
			t.Fatalf("kip must be offline once its last listener is gone")
		}
	})

	t.Run("an owner connection never reports the edge, and a nil listener is a no-op", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		if h.Disconnect(owner) {
			t.Fatalf("an owner Disconnect must report false")
		}
		if h.Disconnect(nil) {
			t.Fatalf("Disconnect(nil) must report false")
		}
		agent, _ := h.Connect("kip", "m-1")
		if !h.Disconnect(agent) {
			t.Fatalf("an agent Disconnect must report the edge — the contrast to the owner case")
		}
	})
}

func TestIsOnline(t *testing.T) {
	t.Run("only a member holding a live listener reads online; the owner connection and an unknown id never do", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("", "")
		l, _ := h.Connect("kip", "m-1")
		for id, want := range map[string]bool{
			"kip": true,
			"ana": false,
			"":    false,
			"KIP": false,
		} {
			if got := h.IsOnline(id); got != want {
				t.Fatalf("IsOnline(%q) = %v, want %v", id, got, want)
			}
		}
		h.Disconnect(l)
		if h.IsOnline("kip") {
			t.Fatalf("IsOnline(kip) must fall back to false once the connection is gone")
		}
	})

	t.Run("a listener with no machine claim still projects its member online — the claim is not the online fact", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("kip", "")
		if !h.IsOnline("kip") {
			t.Fatalf("a claim-less connection must still project online")
		}
		if got := h.MachineOf("kip"); got != "" {
			t.Fatalf("MachineOf = %q, want the blank claim", got)
		}
	})
}

func TestOnlineMembers(t *testing.T) {
	t.Run("the set holds every connected member id and never the owner connection", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("", "")
		h.Connect("kip", "m-1")
		ana, _ := h.Connect("ana", "")
		want := map[string]bool{"kip": true, "ana": true}
		if got := h.OnlineMembers(); !reflect.DeepEqual(got, want) {
			t.Fatalf("OnlineMembers = %v, want %v", got, want)
		}
		h.Disconnect(ana)
		if got := h.OnlineMembers(); !reflect.DeepEqual(got, map[string]bool{"kip": true}) {
			t.Fatalf("after ana leaves OnlineMembers = %v, want just kip", got)
		}
	})

	t.Run("with no agent connected the answer is an empty map, not nil", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("", "")
		got := h.OnlineMembers()
		if got == nil {
			t.Fatalf("OnlineMembers must answer an allocated map")
		}
		if !reflect.DeepEqual(got, map[string]bool{}) {
			t.Fatalf("OnlineMembers = %v, want an empty map", got)
		}
	})

	t.Run("the returned map is the caller's own — mutating it cannot corrupt the registry", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("kip", "m-1")
		got := h.OnlineMembers()
		got["ghost"] = true
		if !reflect.DeepEqual(h.OnlineMembers(), map[string]bool{"kip": true}) {
			t.Fatalf("the registry was corrupted through the returned map: %v", h.OnlineMembers())
		}
	})
}

func TestMachineOf(t *testing.T) {
	t.Run("a connected member answers its token's machine claim, and every id with no live claim answers the empty string", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("", "m-owner")
		h.Connect("kip", "m-1")
		h.Connect("ana", "")
		for id, want := range map[string]string{
			"kip": "m-1",
			"ana": "",
			"zoe": "",
			"":    "",
		} {
			if got := h.MachineOf(id); got != want {
				t.Fatalf("MachineOf(%q) = %q, want %q", id, got, want)
			}
		}
	})

	t.Run("the claim follows the takeover: the surviving connection's machine is the answer", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("kip", "m-1")
		hubTestStderr(t, func() { h.Connect("kip", "m-2") })
		if got := h.MachineOf("kip"); got != "m-2" {
			t.Fatalf("MachineOf(kip) = %q, want the newcomer's claim m-2", got)
		}
	})
}

func TestMachinesOf(t *testing.T) {
	t.Run("a connected member answers the one-element set of its live claim; a member with none answers nil", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("kip", "m-1")
		if got := h.MachinesOf("kip"); !reflect.DeepEqual(got, []string{"m-1"}) {
			t.Fatalf("MachinesOf(kip) = %#v, want [m-1]", got)
		}
		if got := h.MachinesOf("ana"); got != nil {
			t.Fatalf("MachinesOf(ana) = %#v, want nil", got)
		}
	})

	t.Run("a blank claim and the blank member id both drop out rather than becoming an empty entry in the set", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("", "m-owner")
		h.Connect("ana", "")
		if got := h.MachinesOf("ana"); got != nil {
			t.Fatalf("MachinesOf on a claim-less connection = %#v, want nil", got)
		}
		if got := h.MachinesOf(""); got != nil {
			t.Fatalf("MachinesOf(\"\") = %#v, want nil", got)
		}
	})

	t.Run("the dual-SSE takeover keeps it single-valued: after moving machines only the surviving claim is reported", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("kip", "m-1")
		hubTestStderr(t, func() { h.Connect("kip", "m-2") })
		if got := h.MachinesOf("kip"); !reflect.DeepEqual(got, []string{"m-2"}) {
			t.Fatalf("MachinesOf(kip) = %#v, want just the surviving claim [m-2]", got)
		}
	})
}

func TestAgentsOnMachine(t *testing.T) {
	t.Run("every agent whose live claim names the machine is listed, and a machine nobody claims answers nil", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("", "m-1")
		h.Connect("kip", "m-1")
		h.Connect("ana", "m-2")
		if got := h.AgentsOnMachine("m-1"); !reflect.DeepEqual(got, []string{"kip"}) {
			t.Fatalf("AgentsOnMachine(m-1) = %#v, want [kip] — the owner connection is not an agent", got)
		}
		if got := h.AgentsOnMachine("m-2"); !reflect.DeepEqual(got, []string{"ana"}) {
			t.Fatalf("AgentsOnMachine(m-2) = %#v, want [ana]", got)
		}
		if got := h.AgentsOnMachine("m-9"); got != nil {
			t.Fatalf("AgentsOnMachine(m-9) = %#v, want nil", got)
		}
	})

	t.Run("the blank machine id is never a wildcard: it answers nil even while claim-less connections exist", func(t *testing.T) {
		h := hubTestPinned()
		h.Connect("kip", "")
		if got := h.AgentsOnMachine(""); got != nil {
			t.Fatalf("AgentsOnMachine(\"\") = %#v, want nil", got)
		}
	})

	t.Run("two agents on one machine are both listed, and a disconnect removes only its own", func(t *testing.T) {
		h := hubTestPinned()
		kip, _ := h.Connect("kip", "m-1")
		h.Connect("ana", "m-1")
		got := h.AgentsOnMachine("m-1")
		if len(got) != 2 {
			t.Fatalf("AgentsOnMachine(m-1) = %#v, want both agents", got)
		}
		seen := map[string]bool{got[0]: true, got[1]: true}
		if !reflect.DeepEqual(seen, map[string]bool{"kip": true, "ana": true}) {
			t.Fatalf("AgentsOnMachine(m-1) = %#v, want kip and ana", got)
		}
		h.Disconnect(kip)
		if got := h.AgentsOnMachine("m-1"); !reflect.DeepEqual(got, []string{"ana"}) {
			t.Fatalf("after kip leaves = %#v, want [ana]", got)
		}
	})
}

func TestMarshalJSON(t *testing.T) {
	t.Run("a whole-numbered float renders with an explicit decimal point so it can never read back as an int", func(t *testing.T) {
		for in, want := range map[float64]string{
			0:    "0.0",
			1:    "1.0",
			-3:   "-3.0",
			1e21: "1000000000000000000000.0",
		} {
			got, err := jsonFloat(in).MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON(%v): %v", in, err)
			}
			if string(got) != want {
				t.Fatalf("MarshalJSON(%v) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("a fractional value renders at full precision and is left exactly as strconv writes it", func(t *testing.T) {
		for in, want := range map[float64]string{
			1.5:                "1.5",
			0.000001:           "0.000001",
			1700000000.8804412: "1700000000.8804412",
		} {
			got, err := jsonFloat(in).MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON(%v): %v", in, err)
			}
			if string(got) != want {
				t.Fatalf("MarshalJSON(%v) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("the field carries the decimal point through a surrounding struct encode too", func(t *testing.T) {
		raw, err := json.Marshal(struct {
			Ts jsonFloat `json:"ts"`
		}{jsonFloat(5)})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(raw) != `{"ts":5.0}` {
			t.Fatalf("encoded = %s, want {\"ts\":5.0}", raw)
		}
	})
}

func TestAudienceMembers(t *testing.T) {
	t.Run("the named ids become the addressed set while blanks are dropped and repeats collapse", func(t *testing.T) {
		got := audienceMembers("kip", "", "ana", "kip")
		want := Audience{Members: map[string]bool{"kip": true, "ana": true}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("audienceMembers = %#v, want %#v", got, want)
		}
	})

	t.Run("addressing nobody yields an allocated empty set that reaches no agent, unlike the nil-set broadcast", func(t *testing.T) {
		empty := audienceMembers()
		if empty.All || empty.Members == nil || len(empty.Members) != 0 {
			t.Fatalf("audienceMembers() = %#v, want an allocated empty member set", empty)
		}
		if got := audienceAll(); !reflect.DeepEqual(got, Audience{All: true}) {
			t.Fatalf("audienceAll = %#v, want All with a nil member set", got)
		}
		if got := audienceOwnerOnly(); !reflect.DeepEqual(got, Audience{}) {
			t.Fatalf("audienceOwnerOnly = %#v, want the zero Audience", got)
		}
	})

	t.Run("only the addressed agent is fanned to, while an addressing of nobody reaches the owner alone", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		kip, _ := h.Connect("kip", "")
		ana, _ := h.Connect("ana", "")
		h.Publish("task", "upsert", "task", "T-1", nil, audienceMembers("kip", ""), "owner")
		h.Publish("task", "upsert", "task", "T-2", nil, audienceMembers(), "owner")
		frame := func(seq int, key string) map[string]any {
			return map[string]any{
				"seq": seq, "topic": "task", "op": "upsert",
				"data": map[string]any{
					"entity": "task", "key": key, "epoch": seq,
					"deleted": false, "payload": nil,
				},
				"ts": apiAnyNumber, "trigger": "owner",
			}
		}
		hubTestWantFrames(t, "owner", owner, frame(1, "T-1"), frame(2, "T-2"))
		hubTestWantFrames(t, "kip", kip, frame(1, "T-1"))
		hubTestWantFrames(t, "ana", ana)
	})
}

func TestPublish(t *testing.T) {
	t.Run("one publish builds the six-key envelope, numbers it from one and repeats the seq as the epoch and the id line", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		h.Publish("member", "upsert", "member", "kip", map[string]any{"name": "Kip"}, audienceAll(), "owner")
		raw := owner.pop()
		want := "id: 1\ndata: {\"seq\":1,\"topic\":\"member\",\"op\":\"upsert\"," +
			"\"data\":{\"entity\":\"member\",\"key\":\"kip\",\"epoch\":1,\"deleted\":false," +
			"\"payload\":{\"name\":\"Kip\"}},\"ts\":"
		if got := string(raw); len(got) < len(want) || got[:len(want)] != want {
			t.Fatalf("wire text:\n got %q\nwant the prefix %q", got, want)
		}
		if got := string(raw); got[len(got)-len(",\"trigger\":\"owner\"}\n\n"):] != ",\"trigger\":\"owner\"}\n\n" {
			t.Fatalf("wire text must end with the trigger key and a blank line, got %q", got)
		}
		if next := owner.pop(); next != nil {
			t.Fatalf("one publish must append exactly one frame, also got %q", next)
		}
	})

	t.Run("a remove drops the payload and stamps deleted, while the seq keeps climbing across publishes", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		h.Publish("task", "upsert", "task", "T-1", map[string]any{"n": 1}, audienceAll(), "owner")
		h.Publish("task", "remove", "task", "T-1", map[string]any{"n": 1}, audienceAll(), "owner")
		hubTestWantFrames(t, "frames", owner,
			map[string]any{
				"seq": 1, "topic": "task", "op": "upsert",
				"data": map[string]any{
					"entity": "task", "key": "T-1", "epoch": 1,
					"deleted": false, "payload": map[string]any{"n": float64(1)},
				},
				"ts": apiAnyNumber, "trigger": "owner",
			},
			map[string]any{
				"seq": 2, "topic": "task", "op": "remove",
				"data": map[string]any{
					"entity": "task", "key": "T-1", "epoch": 2,
					"deleted": true, "payload": nil,
				},
				"ts": apiAnyNumber, "trigger": "owner",
			})
	})

	t.Run("a blank trigger folds to server so the wire never carries an empty attribution", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		h.Publish("chat", "upsert", "chat", "c-1", nil, audienceAll(), "")
		hubTestWantFrames(t, "frames", owner, map[string]any{
			"seq": 1, "topic": "chat", "op": "upsert",
			"data": map[string]any{
				"entity": "chat", "key": "c-1", "epoch": 1,
				"deleted": false, "payload": nil,
			},
			"ts": apiAnyNumber, "trigger": "server",
		})
	})

	t.Run("a topic outside the closed vocabulary is dropped silently and burns no sequence number", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		out := hubTestStderr(t, func() {
			h.Publish("role", "upsert", "role", "engineer", nil, audienceAll(), "owner")
		})
		if out != "" {
			t.Fatalf("the drop is silent by design, stderr = %q", out)
		}
		hubTestWantFrames(t, "dropped", owner)
		h.Publish("role_def", "upsert", "role_def", "engineer", nil, audienceAll(), "owner")
		hubTestWantFrames(t, "accepted", owner, map[string]any{
			"seq": 1, "topic": "role_def", "op": "upsert",
			"data": map[string]any{
				"entity": "role_def", "key": "engineer", "epoch": 1,
				"deleted": false, "payload": nil,
			},
			"ts": apiAnyNumber, "trigger": "owner",
		})
	})

	t.Run("a payload that cannot be marshalled fans nothing out but has already consumed its sequence number", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		h.Publish("member", "upsert", "member", "kip", func() {}, audienceAll(), "owner")
		hubTestWantFrames(t, "unmarshalable", owner)
		h.Publish("member", "upsert", "member", "kip", nil, audienceAll(), "owner")
		hubTestWantFrames(t, "next", owner, map[string]any{
			"seq": 2, "topic": "member", "op": "upsert",
			"data": map[string]any{
				"entity": "member", "key": "kip", "epoch": 2,
				"deleted": false, "payload": nil,
			},
			"ts": apiAnyNumber, "trigger": "owner",
		})
	})

	t.Run("a filtered agent observes a gapped subsequence while the owner sees the whole run", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		kip, _ := h.Connect("kip", "")
		h.Publish("task", "upsert", "task", "T-1", nil, audienceOwnerOnly(), "owner")
		h.Publish("task", "upsert", "task", "T-2", nil, audienceMembers("kip"), "owner")
		h.Publish("task", "upsert", "task", "T-3", nil, audienceOwnerOnly(), "owner")
		frame := func(seq int, key string) map[string]any {
			return map[string]any{
				"seq": seq, "topic": "task", "op": "upsert",
				"data": map[string]any{
					"entity": "task", "key": key, "epoch": seq,
					"deleted": false, "payload": nil,
				},
				"ts": apiAnyNumber, "trigger": "owner",
			}
		}
		hubTestWantFrames(t, "owner", owner, frame(1, "T-1"), frame(2, "T-2"), frame(3, "T-3"))
		hubTestWantFrames(t, "kip", kip, frame(2, "T-2"))
	})
}

func TestPushDirected(t *testing.T) {
	t.Run("a live member takes the frame verbatim and nobody else is touched", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		kip, _ := h.Connect("kip", "")
		ana, _ := h.Connect("ana", "")
		if !h.PushDirected("kip", []byte("id: 0\ndata: {\"topic\":\"context\"}\n\n")) {
			t.Fatalf("PushDirected to a live member must report true")
		}
		if got := kip.pop(); string(got) != "id: 0\ndata: {\"topic\":\"context\"}\n\n" {
			t.Fatalf("kip received %q", got)
		}
		if got := kip.pop(); got != nil {
			t.Fatalf("exactly one frame must land, also got %q", got)
		}
		if got := owner.pop(); got != nil {
			t.Fatalf("a directed frame must not reach the owner connection, got %q", got)
		}
		if got := ana.pop(); got != nil {
			t.Fatalf("a directed frame must not reach another agent, got %q", got)
		}
	})

	t.Run("an offline member, the blank id and an empty frame are all refused and queue nothing", func(t *testing.T) {
		h := hubTestPinned()
		owner, _ := h.Connect("", "")
		kip, _ := h.Connect("kip", "")
		for _, c := range []struct {
			name   string
			member string
			frame  []byte
		}{
			{"offline member", "ana", []byte("x")},
			{"blank member id", "", []byte("x")},
			{"nil frame", "kip", nil},
			{"empty frame", "kip", []byte{}},
		} {
			if h.PushDirected(c.member, c.frame) {
				t.Fatalf("%s: PushDirected must report false", c.name)
			}
		}
		hubTestWantFrames(t, "kip", kip)
		hubTestWantFrames(t, "owner", owner)
		if got := h.PendingWardenCommands("kip"); got != 0 {
			t.Fatalf("a refused direct push must not fall back to the FIFO, pending = %d", got)
		}
	})
}

func TestPlanCommandPersistLocked(t *testing.T) {
	t.Run("a persistable verb addressed to a member assembles the row with the hub clock's stamp", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{}
		h.cmdStore = store
		got, ok := h.planCommandPersistLocked("w1", wardenCommandDigest{Verb: "update", MemberID: "m-9"}, []byte("FRAME"))
		if !ok {
			t.Fatalf("an update for a named member must be planned")
		}
		if got.store != wardenCommandStore(store) {
			t.Fatalf("the plan must carry the bound store")
		}
		want := WardenCommand{
			WardenID: "w1", Verb: "update", MemberID: "m-9",
			Frame: []byte("FRAME"), EnqueuedTS: hubTestNow,
		}
		if !reflect.DeepEqual(got.cmd, want) {
			t.Fatalf("planned row = %+v, want %+v", got.cmd, want)
		}
		if got.digest != (wardenCommandDigest{Verb: "update", MemberID: "m-9"}) {
			t.Fatalf("planned digest = %+v", got.digest)
		}
		if len(store.rows) != 0 {
			t.Fatalf("planning must do no I/O, the store holds %+v", store.rows)
		}
	})

	t.Run("no bound store, a non-persistable verb and a subject-less frame each plan nothing", func(t *testing.T) {
		unbound := hubTestPinned()
		if _, ok := unbound.planCommandPersistLocked("w1", wardenCommandDigest{Verb: "update", MemberID: "m-9"}, []byte("F")); ok {
			t.Fatalf("with no store bound nothing may be planned")
		}
		h := hubTestPinned()
		h.cmdStore = &hubTestStore{}
		for _, verb := range []string{"start", "stop", "uninstall", "renew", "none", ""} {
			if _, ok := h.planCommandPersistLocked("w1", wardenCommandDigest{Verb: verb, MemberID: "m-9"}, []byte("F")); ok {
				t.Fatalf("verb %q must not be planned for the durable queue", verb)
			}
		}
		if _, ok := h.planCommandPersistLocked("w1", wardenCommandDigest{Verb: "update", MemberID: ""}, []byte("F")); ok {
			t.Fatalf("a frame with no subject must not be planned")
		}
		if got, ok := h.planCommandPersistLocked("w1", wardenCommandDigest{Verb: "update", MemberID: "m-9"}, []byte("F")); !ok || got.cmd.MemberID != "m-9" {
			t.Fatalf("the contrast case must still plan, got %+v %v", got, ok)
		}
	})
}

func TestRunCommandPersists(t *testing.T) {
	t.Run("every planned row reaches its store and a silent run leaves stderr clean", func(t *testing.T) {
		store := &hubTestStore{}
		out := hubTestStderr(t, func() {
			runCommandPersists([]commandStoreWrite{
				{store: store, cmd: WardenCommand{WardenID: "w1", Verb: "update", MemberID: "m-9", Frame: []byte("A"), EnqueuedTS: 1.5}},
				{store: store, cmd: WardenCommand{WardenID: "w2", Verb: "update", MemberID: "m-8", Frame: []byte("B"), EnqueuedTS: 2.5}},
			})
		})
		if out != "" {
			t.Fatalf("a successful run prints nothing, got %q", out)
		}
		want := []WardenCommand{
			{WardenID: "w1", Verb: "update", MemberID: "m-9", Frame: []byte("A"), EnqueuedTS: 1.5},
			{WardenID: "w2", Verb: "update", MemberID: "m-8", Frame: []byte("B"), EnqueuedTS: 2.5},
		}
		if !reflect.DeepEqual(store.rows, want) {
			t.Fatalf("stored rows = %+v, want %+v", store.rows, want)
		}
	})

	t.Run("a store that refuses the write is named on stderr and does not stop the rest of the batch", func(t *testing.T) {
		failing := &hubTestStore{putErr: errors.New("database is locked")}
		working := &hubTestStore{}
		out := hubTestStderr(t, func() {
			runCommandPersists([]commandStoreWrite{
				{store: failing, cmd: WardenCommand{WardenID: "w1", Verb: "update", MemberID: "m-9"},
					digest: wardenCommandDigest{Verb: "update", MemberID: "m-9"}},
				{store: working, cmd: WardenCommand{WardenID: "w2", Verb: "update", MemberID: "m-8", Frame: []byte("B")}},
			})
		})
		want := "[sse] warden command queue persist FAILED: warden=w1 verb=update target=m-9 — " +
			"database is locked (the command is still queued in memory and will be delivered " +
			"if this process survives; it will NOT survive a restart)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if len(failing.rows) != 0 {
			t.Fatalf("the refusing store must hold nothing, got %+v", failing.rows)
		}
		if len(working.rows) != 1 || working.rows[0].WardenID != "w2" {
			t.Fatalf("the batch must continue past the failure, got %+v", working.rows)
		}
	})

	t.Run("an empty plan writes nothing and prints nothing", func(t *testing.T) {
		out := hubTestStderr(t, func() { runCommandPersists(nil) })
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
	})
}

func TestNoteCommandStoreFailure(t *testing.T) {
	t.Run("the note names the operation, the warden, the verb and the target, and says exactly what was lost", func(t *testing.T) {
		out := hubTestStderr(t, func() {
			noteCommandStoreFailure("persist", "w1",
				wardenCommandDigest{Verb: "update", MemberID: "m-9"}, errors.New("disk full"))
		})
		want := "[sse] warden command queue persist FAILED: warden=w1 verb=update target=m-9 — " +
			"disk full (the command is still queued in memory and will be delivered if this " +
			"process survives; it will NOT survive a restart)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
	})

	t.Run("the clear operation reuses the same line with its own verb word, and never prints the frame body", func(t *testing.T) {
		out := hubTestStderr(t, func() {
			noteCommandStoreFailure("clear", "w2",
				wardenCommandDigest{Verb: "start", MemberID: ""}, errors.New("boom"))
		})
		want := "[sse] warden command queue clear FAILED: warden=w2 verb=start target= — " +
			"boom (the command is still queued in memory and will be delivered if this " +
			"process survives; it will NOT survive a restart)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
	})
}

func TestBindWardenCommandStore(t *testing.T) {
	t.Run("binding sweeps a day back, rehydrates each pending frame under its own warden and reports both halves", func(t *testing.T) {
		h := hubTestPinned()
		frame := hubTestCommandFrame(t, "update", "m-9")
		store := &hubTestStore{
			sweptN: 2,
			rows: []WardenCommand{
				{WardenID: "w1", Verb: "update", MemberID: "m-9", Frame: frame, EnqueuedTS: 1},
			},
		}
		out := hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		want := "[sse] warden command queue: dropped 2 command(s) older than 24h0m0s\n" +
			"[sse] warden command restored across restart: warden=w1 verb=update target=m-9\n" +
			"[sse] warden command queue: 1 command(s) survived the restart and will be " +
			"delivered when the addressed warden reconnects\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if !reflect.DeepEqual(store.sweeps, []float64{hubTestNow - 86400}) {
			t.Fatalf("expiry cutoff = %v, want one sweep at now-24h", store.sweeps)
		}
		if got := h.PendingWardenCommands("w1"); got != 1 {
			t.Fatalf("pending after restore = %d, want 1", got)
		}
		if got := h.PendingWardenCommandsFor("w1", "m-9"); got != 1 {
			t.Fatalf("the restored frame must keep its subject tag, per-subject depth = %d", got)
		}
		drained := h.DrainWardenCommands("w1")
		if !reflect.DeepEqual(drained, []wardenCmd{{Subject: "m-9", Frame: frame}}) {
			t.Fatalf("restored queue = %+v", drained)
		}
	})

	t.Run("a store with nothing pending sweeps but stays silent and leaves every queue empty", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{}
		out := hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		if !reflect.DeepEqual(store.sweeps, []float64{hubTestNow - 86400}) {
			t.Fatalf("sweeps = %v", store.sweeps)
		}
		if got := h.PendingWardenCommands("w1"); got != 0 {
			t.Fatalf("pending = %d, want 0", got)
		}
	})

	t.Run("a row with no warden and a row with no frame are skipped rather than restored", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{rows: []WardenCommand{
			{WardenID: "", Verb: "update", MemberID: "m-8", Frame: hubTestCommandFrame(t, "update", "m-8")},
			{WardenID: "w2", Verb: "update", MemberID: "m-7", Frame: nil},
		}}
		out := hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		if out != "" {
			t.Fatalf("nothing was restorable, stderr = %q", out)
		}
		if got := h.PendingWardenCommands(""); got != 0 {
			t.Fatalf("the warden-less row must not be queued, pending = %d", got)
		}
		if got := h.PendingWardenCommands("w2"); got != 0 {
			t.Fatalf("the frame-less row must not be queued, pending = %d", got)
		}
	})

	t.Run("binding the same store twice cannot duplicate an order — the identical frame is recognised", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{rows: []WardenCommand{
			{WardenID: "w1", Verb: "update", MemberID: "m-9", Frame: hubTestCommandFrame(t, "update", "m-9")},
		}}
		hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		out := hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		if out != "" {
			t.Fatalf("the second bind restores nothing, stderr = %q", out)
		}
		if got := h.PendingWardenCommands("w1"); got != 1 {
			t.Fatalf("pending after rebinding = %d, want the single order", got)
		}
	})

	t.Run("a failing sweep and a failing list are both reported and the store is still bound for later writes", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{
			sweepErr: errors.New("sweep boom"),
			listErr:  errors.New("no such table"),
		}
		out := hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		want := "[sse] warden command queue expiry sweep FAILED: sweep boom (stale commands may be replayed)\n" +
			"[sse] warden command queue restore FAILED: no such table — commands pending at the " +
			"last shutdown are lost; an upgrade click may need to be repeated\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		h.EnqueueWardenCommandFor("w1", "m-9", hubTestCommandFrame(t, "update", "m-9"))
		if len(store.rows) != 1 || store.rows[0].MemberID != "m-9" {
			t.Fatalf("the store must still be bound for new enqueues, rows = %+v", store.rows)
		}
	})

	t.Run("binding nil leaves the hub storeless: an update enqueue is queued in memory and persisted nowhere", func(t *testing.T) {
		h := hubTestPinned()
		out := hubTestStderr(t, func() { h.BindWardenCommandStore(nil) })
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		h.EnqueueWardenCommandFor("w1", "m-9", hubTestCommandFrame(t, "update", "m-9"))
		if got := h.PendingWardenCommands("w1"); got != 1 {
			t.Fatalf("the in-memory FIFO must still work, pending = %d", got)
		}
	})
}

func TestMarkWardenCommandWritten(t *testing.T) {
	t.Run("a written update clears exactly its own durable row, keyed by warden, verb and target", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{}
		hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		out := hubTestStderr(t, func() {
			h.MarkWardenCommandWritten("w1", hubTestCommandFrame(t, "update", "m-9"))
		})
		if out != "" {
			t.Fatalf("a successful clear prints nothing, got %q", out)
		}
		if !reflect.DeepEqual(store.deleted, []string{"w1/update/m-9"}) {
			t.Fatalf("deletes = %v, want one w1/update/m-9", store.deleted)
		}
	})

	t.Run("a non-persistable verb, an unparseable frame and a subject-less frame clear nothing at all", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{}
		hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		h.MarkWardenCommandWritten("w1", hubTestCommandFrame(t, "start", "kip"))
		h.MarkWardenCommandWritten("w1", []byte("data: not-json\n\n"))
		h.MarkWardenCommandWritten("w1", hubTestCommandFrame(t, "update", ""))
		if store.deleted != nil {
			t.Fatalf("nothing may be cleared, deletes = %v", store.deleted)
		}
		h.MarkWardenCommandWritten("w1", hubTestCommandFrame(t, "update", "m-9"))
		if !reflect.DeepEqual(store.deleted, []string{"w1/update/m-9"}) {
			t.Fatalf("the contrast case must clear, deletes = %v", store.deleted)
		}
	})

	t.Run("with no store bound the call is a no-op instead of a panic", func(t *testing.T) {
		h := hubTestPinned()
		out := hubTestStderr(t, func() {
			h.MarkWardenCommandWritten("w1", hubTestCommandFrame(t, "update", "m-9"))
		})
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
	})

	t.Run("a delete the store refuses is named on stderr as the clear half of the same failure line", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{deleteErr: errors.New("database is locked")}
		hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		out := hubTestStderr(t, func() {
			h.MarkWardenCommandWritten("w1", hubTestCommandFrame(t, "update", "m-9"))
		})
		want := "[sse] warden command queue clear FAILED: warden=w1 verb=update target=m-9 — " +
			"database is locked (the command is still queued in memory and will be delivered " +
			"if this process survives; it will NOT survive a restart)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
	})
}

func TestEnqueueWardenCommand(t *testing.T) {
	t.Run("the frame lands on the named warden's FIFO with no subject, so only the bare depth can see it", func(t *testing.T) {
		h := hubTestPinned()
		frame := hubTestCommandFrame(t, "start", "kip")
		h.EnqueueWardenCommand("w1", frame)
		if got := h.PendingWardenCommands("w1"); got != 1 {
			t.Fatalf("pending = %d, want 1", got)
		}
		if got := h.PendingWardenCommandsFor("w1", "kip"); got != 0 {
			t.Fatalf("an untagged enqueue must not answer a per-subject read, got %d", got)
		}
		if got := h.DrainWardenCommands("w1"); !reflect.DeepEqual(got, []wardenCmd{{Subject: "", Frame: frame}}) {
			t.Fatalf("drained = %+v, want one untagged frame", got)
		}
	})

	t.Run("a blank warden id is dropped: no queue is created for it", func(t *testing.T) {
		h := hubTestPinned()
		h.EnqueueWardenCommand("", hubTestCommandFrame(t, "start", "kip"))
		if got := h.PendingWardenCommands(""); got != 0 {
			t.Fatalf("pending = %d, want 0", got)
		}
		if got := h.DrainWardenCommands(""); got != nil {
			t.Fatalf("drain = %+v, want nil", got)
		}
		h.EnqueueWardenCommand("w1", hubTestCommandFrame(t, "start", "kip"))
		if got := h.PendingWardenCommands("w1"); got != 1 {
			t.Fatalf("the named contrast case must queue, pending = %d", got)
		}
	})
}

func TestEnqueueWardenCommandFor(t *testing.T) {
	t.Run("the subject rides with the frame and each warden keeps its own FIFO in enqueue order", func(t *testing.T) {
		h := hubTestPinned()
		first := hubTestCommandFrame(t, "start", "kip")
		second := hubTestCommandFrame(t, "stop", "ana")
		elsewhere := hubTestCommandFrame(t, "start", "zoe")
		h.EnqueueWardenCommandFor("w1", "kip", first)
		h.EnqueueWardenCommandFor("w1", "ana", second)
		h.EnqueueWardenCommandFor("w2", "zoe", elsewhere)
		if got := h.DrainWardenCommands("w1"); !reflect.DeepEqual(got, []wardenCmd{
			{Subject: "kip", Frame: first}, {Subject: "ana", Frame: second},
		}) {
			t.Fatalf("w1 queue = %+v", got)
		}
		if got := h.DrainWardenCommands("w2"); !reflect.DeepEqual(got, []wardenCmd{{Subject: "zoe", Frame: elsewhere}}) {
			t.Fatalf("w2 queue = %+v", got)
		}
	})

	t.Run("a fresh dispatch for a member erases the stale note that its previous frame was never delivered", func(t *testing.T) {
		h := hubTestPinned()
		hubTestStderr(t, func() {
			h.ReturnUndeliveredCommands("w1", []wardenCmd{{Subject: "kip", Frame: hubTestCommandFrame(t, "start", "kip")}})
		})
		if _, ok := h.UndeliveredCommandSince("kip", 0); !ok {
			t.Fatalf("the loss note must exist before the new dispatch")
		}
		h.EnqueueWardenCommandFor("w1", "kip", hubTestCommandFrame(t, "start", "kip"))
		if note, ok := h.UndeliveredCommandSince("kip", 0); ok {
			t.Fatalf("the stale note must be gone, got %+v", note)
		}
	})

	t.Run("an update is mirrored into the durable store on enqueue while a start is deliberately not", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{}
		hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		update := hubTestCommandFrame(t, "update", "m-9")
		h.EnqueueWardenCommandFor("w1", "m-9", update)
		h.EnqueueWardenCommandFor("w1", "kip", hubTestCommandFrame(t, "start", "kip"))
		want := []WardenCommand{{
			WardenID: "w1", Verb: "update", MemberID: "m-9",
			Frame: update, EnqueuedTS: hubTestNow,
		}}
		if !reflect.DeepEqual(store.rows, want) {
			t.Fatalf("persisted rows = %+v, want only the update", store.rows)
		}
		if got := h.PendingWardenCommands("w1"); got != 2 {
			t.Fatalf("both frames must be live in the FIFO, pending = %d", got)
		}
	})

	t.Run("a blank warden id queues nothing and leaves any existing loss note alone", func(t *testing.T) {
		h := hubTestPinned()
		hubTestStderr(t, func() {
			h.ReturnUndeliveredCommands("w1", []wardenCmd{{Subject: "kip", Frame: hubTestCommandFrame(t, "start", "kip")}})
		})
		h.EnqueueWardenCommandFor("", "kip", hubTestCommandFrame(t, "start", "kip"))
		if got := h.PendingWardenCommands(""); got != 0 {
			t.Fatalf("pending = %d, want 0", got)
		}
		if _, ok := h.UndeliveredCommandSince("kip", 0); !ok {
			t.Fatalf("a refused enqueue must not clear the note")
		}
	})
}

func TestPendingWardenCommands(t *testing.T) {
	t.Run("the depth counts every uncollected frame on that warden and falls to zero once they are drained", func(t *testing.T) {
		h := hubTestPinned()
		if got := h.PendingWardenCommands("w1"); got != 0 {
			t.Fatalf("empty depth = %d, want 0", got)
		}
		h.EnqueueWardenCommandFor("w1", "kip", hubTestCommandFrame(t, "start", "kip"))
		h.EnqueueWardenCommandFor("w1", "ana", hubTestCommandFrame(t, "stop", "ana"))
		h.EnqueueWardenCommandFor("w2", "zoe", hubTestCommandFrame(t, "start", "zoe"))
		if got := h.PendingWardenCommands("w1"); got != 2 {
			t.Fatalf("w1 depth = %d, want 2", got)
		}
		if got := h.PendingWardenCommands("w2"); got != 1 {
			t.Fatalf("w2 depth = %d, want 1", got)
		}
		h.DrainWardenCommands("w1")
		if got := h.PendingWardenCommands("w1"); got != 0 {
			t.Fatalf("w1 depth after the drain = %d, want 0", got)
		}
		if got := h.PendingWardenCommands("w2"); got != 1 {
			t.Fatalf("draining w1 must not touch w2, depth = %d", got)
		}
	})

	t.Run("reading the depth never pops, and the blank warden id always reads zero", func(t *testing.T) {
		h := hubTestPinned()
		h.EnqueueWardenCommandFor("w1", "kip", hubTestCommandFrame(t, "start", "kip"))
		for i := 0; i < 3; i++ {
			if got := h.PendingWardenCommands("w1"); got != 1 {
				t.Fatalf("read %d = %d, want a stable 1", i, got)
			}
		}
		if got := h.PendingWardenCommands(""); got != 0 {
			t.Fatalf("blank warden depth = %d, want 0", got)
		}
	})
}

func TestPendingWardenCommandsFor(t *testing.T) {
	t.Run("only the frames tagged with that subject are counted while the shared queue stays deeper", func(t *testing.T) {
		h := hubTestPinned()
		h.EnqueueWardenCommandFor("w1", "kip", hubTestCommandFrame(t, "start", "kip"))
		h.EnqueueWardenCommandFor("w1", "kip", hubTestCommandFrame(t, "stop", "kip"))
		h.EnqueueWardenCommandFor("w1", "ana", hubTestCommandFrame(t, "start", "ana"))
		if got := h.PendingWardenCommands("w1"); got != 3 {
			t.Fatalf("queue depth = %d, want 3", got)
		}
		if got := h.PendingWardenCommandsFor("w1", "kip"); got != 2 {
			t.Fatalf("kip's backlog = %d, want 2", got)
		}
		if got := h.PendingWardenCommandsFor("w1", "ana"); got != 1 {
			t.Fatalf("ana's backlog = %d, want 1", got)
		}
		if got := h.PendingWardenCommandsFor("w1", "zoe"); got != 0 {
			t.Fatalf("a subject with nothing queued = %d, want 0", got)
		}
	})

	t.Run("a blank subject and a blank warden both read zero even while an untagged frame is queued", func(t *testing.T) {
		h := hubTestPinned()
		h.EnqueueWardenCommand("w1", hubTestCommandFrame(t, "start", "kip"))
		if got := h.PendingWardenCommands("w1"); got != 1 {
			t.Fatalf("the untagged frame must be in the queue, depth = %d", got)
		}
		if got := h.PendingWardenCommandsFor("w1", ""); got != 0 {
			t.Fatalf("the blank subject must never match, got %d", got)
		}
		if got := h.PendingWardenCommandsFor("", "kip"); got != 0 {
			t.Fatalf("the blank warden must read zero, got %d", got)
		}
	})
}

func TestDrainWardenCommands(t *testing.T) {
	t.Run("the whole backlog comes back in FIFO order and the queue is emptied by the pop", func(t *testing.T) {
		h := hubTestPinned()
		first := hubTestCommandFrame(t, "start", "kip")
		second := hubTestCommandFrame(t, "stop", "ana")
		h.EnqueueWardenCommandFor("w1", "kip", first)
		h.EnqueueWardenCommandFor("w1", "ana", second)
		want := []wardenCmd{{Subject: "kip", Frame: first}, {Subject: "ana", Frame: second}}
		if got := h.DrainWardenCommands("w1"); !reflect.DeepEqual(got, want) {
			t.Fatalf("drained = %+v, want %+v", got, want)
		}
		if got := h.PendingWardenCommands("w1"); got != 0 {
			t.Fatalf("depth after the drain = %d, want 0", got)
		}
		if got := h.DrainWardenCommands("w1"); got != nil {
			t.Fatalf("the second drain = %+v, want nil", got)
		}
	})

	t.Run("an empty queue and the blank warden id both answer nil rather than an empty slice", func(t *testing.T) {
		h := hubTestPinned()
		if got := h.DrainWardenCommands("w1"); got != nil {
			t.Fatalf("empty drain = %#v, want nil", got)
		}
		if got := h.DrainWardenCommands(""); got != nil {
			t.Fatalf("blank warden drain = %#v, want nil", got)
		}
	})

	t.Run("the durable row survives the drain — only a written frame clears it", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{}
		hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		h.EnqueueWardenCommandFor("w1", "m-9", hubTestCommandFrame(t, "update", "m-9"))
		h.DrainWardenCommands("w1")
		if len(store.rows) != 1 {
			t.Fatalf("the durable row must survive the drain, rows = %+v", store.rows)
		}
		if store.deleted != nil {
			t.Fatalf("a drain must delete nothing, deletes = %v", store.deleted)
		}
	})
}

func TestReturnUndeliveredCommands(t *testing.T) {
	t.Run("an undelivered update is requeued at the front while every other verb is dropped, each named on stderr", func(t *testing.T) {
		h := hubTestPinned()
		update := hubTestCommandFrame(t, "update", "m-9")
		start := hubTestCommandFrame(t, "start", "kip")
		junk := []byte("data: not-json\n\n")
		var requeued, dropped int
		out := hubTestStderr(t, func() {
			requeued, dropped = h.ReturnUndeliveredCommands("w1", []wardenCmd{
				{Subject: "m-9", Frame: update},
				{Subject: "kip", Frame: start},
				{Subject: "", Frame: junk},
			})
		})
		if requeued != 1 || dropped != 2 {
			t.Fatalf("requeued=%d dropped=%d, want 1 and 2", requeued, dropped)
		}
		want := "[sse] warden command undelivered: warden=w1 verb=update target=m-9 — REQUEUED (update has no re-decision path)\n" +
			"[sse] warden command undelivered: warden=w1 verb=start target=kip — DROPPED (at-most-once contract — reconcile re-decides from presence)\n" +
			"[sse] warden command undelivered: warden=w1 verb=unknown target= — DROPPED (at-most-once contract — reconcile re-decides from presence)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if got := h.DrainWardenCommands("w1"); !reflect.DeepEqual(got, []wardenCmd{{Subject: "m-9", Frame: update}}) {
			t.Fatalf("queue after the return = %+v, want only the requeued update with its subject", got)
		}
	})

	t.Run("the requeued frame goes ahead of what is already queued rather than behind it", func(t *testing.T) {
		h := hubTestPinned()
		waiting := hubTestCommandFrame(t, "update", "m-2")
		returned := hubTestCommandFrame(t, "update", "m-9")
		h.EnqueueWardenCommandFor("w1", "m-2", waiting)
		hubTestStderr(t, func() {
			h.ReturnUndeliveredCommands("w1", []wardenCmd{{Subject: "m-9", Frame: returned}})
		})
		want := []wardenCmd{{Subject: "m-9", Frame: returned}, {Subject: "m-2", Frame: waiting}}
		if got := h.DrainWardenCommands("w1"); !reflect.DeepEqual(got, want) {
			t.Fatalf("queue = %+v, want the returned frame first", got)
		}
	})

	t.Run("an update already sitting in the queue is not requeued a second time — it is counted as dropped", func(t *testing.T) {
		h := hubTestPinned()
		frame := hubTestCommandFrame(t, "update", "m-9")
		h.EnqueueWardenCommandFor("w1", "m-9", frame)
		var requeued, dropped int
		hubTestStderr(t, func() {
			requeued, dropped = h.ReturnUndeliveredCommands("w1", []wardenCmd{{Subject: "m-9", Frame: frame}})
		})
		if requeued != 0 || dropped != 1 {
			t.Fatalf("requeued=%d dropped=%d, want 0 and 1", requeued, dropped)
		}
		if got := h.PendingWardenCommands("w1"); got != 1 {
			t.Fatalf("depth = %d, want the single un-duplicated order", got)
		}
	})

	t.Run("every returned frame leaves a loss note on its subject, flagged with whether it will be retried", func(t *testing.T) {
		h := hubTestPinned()
		hubTestStderr(t, func() {
			h.ReturnUndeliveredCommands("w1", []wardenCmd{
				{Subject: "m-9", Frame: hubTestCommandFrame(t, "update", "m-9")},
				{Subject: "kip", Frame: hubTestCommandFrame(t, "start", "kip")},
			})
		})
		note, ok := h.UndeliveredCommandSince("m-9", 0)
		if !ok || note != (undeliveredCommand{Verb: "update", Warden: "w1", At: hubTestNow, Requeued: true}) {
			t.Fatalf("m-9 note = %+v (%v)", note, ok)
		}
		note, ok = h.UndeliveredCommandSince("kip", 0)
		if !ok || note != (undeliveredCommand{Verb: "start", Warden: "w1", At: hubTestNow, Requeued: false}) {
			t.Fatalf("kip note = %+v (%v)", note, ok)
		}
	})

	t.Run("a requeued update is offered to the durable store again so a failed first persist gets its second chance", func(t *testing.T) {
		h := hubTestPinned()
		store := &hubTestStore{}
		hubTestStderr(t, func() { h.BindWardenCommandStore(store) })
		frame := hubTestCommandFrame(t, "update", "m-9")
		hubTestStderr(t, func() {
			h.ReturnUndeliveredCommands("w1", []wardenCmd{
				{Subject: "m-9", Frame: frame},
				{Subject: "kip", Frame: hubTestCommandFrame(t, "start", "kip")},
			})
		})
		want := []WardenCommand{{
			WardenID: "w1", Verb: "update", MemberID: "m-9",
			Frame: frame, EnqueuedTS: hubTestNow,
		}}
		if !reflect.DeepEqual(store.rows, want) {
			t.Fatalf("persisted rows = %+v, want only the requeued update", store.rows)
		}
	})

	t.Run("a blank warden id and an empty return list change nothing and print nothing", func(t *testing.T) {
		h := hubTestPinned()
		frame := hubTestCommandFrame(t, "update", "m-9")
		var out string
		var requeued, dropped int
		out = hubTestStderr(t, func() {
			requeued, dropped = h.ReturnUndeliveredCommands("", []wardenCmd{{Subject: "m-9", Frame: frame}})
		})
		if requeued != 0 || dropped != 0 || out != "" {
			t.Fatalf("blank warden: requeued=%d dropped=%d stderr=%q", requeued, dropped, out)
		}
		out = hubTestStderr(t, func() {
			requeued, dropped = h.ReturnUndeliveredCommands("w1", nil)
		})
		if requeued != 0 || dropped != 0 || out != "" {
			t.Fatalf("empty list: requeued=%d dropped=%d stderr=%q", requeued, dropped, out)
		}
		if got := h.PendingWardenCommands("w1"); got != 0 {
			t.Fatalf("nothing may be queued, depth = %d", got)
		}
		if _, ok := h.UndeliveredCommandSince("m-9", 0); ok {
			t.Fatalf("no loss note may be written by a refused return")
		}
	})
}

func TestUndeliveredCommandSince(t *testing.T) {
	t.Run("a note stamped at or after the caller's anchor is handed back whole", func(t *testing.T) {
		h := hubTestPinned()
		hubTestStderr(t, func() {
			h.ReturnUndeliveredCommands("w1", []wardenCmd{{Subject: "kip", Frame: hubTestCommandFrame(t, "start", "kip")}})
		})
		want := undeliveredCommand{Verb: "start", Warden: "w1", At: hubTestNow, Requeued: false}
		for _, since := range []float64{0, hubTestNow - 1, hubTestNow} {
			got, ok := h.UndeliveredCommandSince("kip", since)
			if !ok || got != want {
				t.Fatalf("since=%v: got %+v (%v), want %+v", since, got, ok, want)
			}
		}
	})

	t.Run("a note older than the anchor is withheld, so a stale loss can never explain the dispatch in flight", func(t *testing.T) {
		h := hubTestPinned()
		hubTestStderr(t, func() {
			h.ReturnUndeliveredCommands("w1", []wardenCmd{{Subject: "kip", Frame: hubTestCommandFrame(t, "start", "kip")}})
		})
		got, ok := h.UndeliveredCommandSince("kip", hubTestNow+0.001)
		if ok || got != (undeliveredCommand{}) {
			t.Fatalf("got %+v (%v), want the zero note and false", got, ok)
		}
	})

	t.Run("a member with no note and the blank member id both answer the zero note", func(t *testing.T) {
		h := hubTestPinned()
		hubTestStderr(t, func() {
			h.ReturnUndeliveredCommands("w1", []wardenCmd{{Subject: "kip", Frame: hubTestCommandFrame(t, "start", "kip")}})
		})
		for _, id := range []string{"ana", ""} {
			got, ok := h.UndeliveredCommandSince(id, 0)
			if ok || got != (undeliveredCommand{}) {
				t.Fatalf("UndeliveredCommandSince(%q) = %+v (%v)", id, got, ok)
			}
		}
		if _, ok := h.UndeliveredCommandSince("kip", 0); !ok {
			t.Fatalf("the contrast case must still answer a note")
		}
	})
}

func TestContainsFrame(t *testing.T) {
	t.Run("an identical frame is recognised whatever subject it is tagged with, and a differing one is not", func(t *testing.T) {
		update := hubTestCommandFrame(t, "update", "m-9")
		other := hubTestCommandFrame(t, "update", "m-2")
		queue := []wardenCmd{{Subject: "kip", Frame: hubTestCommandFrame(t, "start", "kip")}, {Subject: "", Frame: update}}
		if !containsFrame(queue, update) {
			t.Fatalf("the byte-identical frame must be found regardless of its subject tag")
		}
		if containsFrame(queue, other) {
			t.Fatalf("a frame for another target must not match")
		}
	})

	t.Run("an empty queue matches nothing, and an empty frame matches only an empty one", func(t *testing.T) {
		frame := hubTestCommandFrame(t, "update", "m-9")
		if containsFrame(nil, frame) {
			t.Fatalf("the nil queue must match nothing")
		}
		if containsFrame([]wardenCmd{}, frame) {
			t.Fatalf("the empty queue must match nothing")
		}
		if containsFrame([]wardenCmd{{Frame: frame}}, nil) {
			t.Fatalf("an empty frame must not match a real one")
		}
		if !containsFrame([]wardenCmd{{Frame: []byte{}}}, nil) {
			t.Fatalf("bytes.Equal treats nil and the empty frame as the same value")
		}
	})
}

func TestGet(t *testing.T) {
	t.Run("a stored entry comes back with its fields, and an id nobody set answers nil", func(t *testing.T) {
		s := newMemStore()
		if got := s.Get("kip"); got != nil {
			t.Fatalf("Get on an empty store = %#v, want nil", got)
		}
		s.Set("kip", map[string]any{"context_pct": 42, "boot_ts": 1700000000.0})
		want := map[string]any{"context_pct": 42, "boot_ts": 1700000000.0}
		if got := s.Get("kip"); !reflect.DeepEqual(got, want) {
			t.Fatalf("Get = %#v, want %#v", got, want)
		}
		if got := s.Get("ana"); got != nil {
			t.Fatalf("Get on an absent id = %#v, want nil", got)
		}
	})

	t.Run("the answer is a copy: mutating it leaves the stored entry untouched", func(t *testing.T) {
		s := newMemStore()
		s.Set("kip", map[string]any{"context_pct": 42})
		got := s.Get("kip")
		got["context_pct"] = 99
		got["injected"] = true
		if again := s.Get("kip"); !reflect.DeepEqual(again, map[string]any{"context_pct": 42}) {
			t.Fatalf("the store was mutated through the returned map: %#v", again)
		}
	})
}

func TestSet(t *testing.T) {
	t.Run("a set entry is readable back and a second set for the same id replaces it wholesale", func(t *testing.T) {
		s := newMemStore()
		s.Set("kip", map[string]any{"context_pct": 42, "runtime": "claude"})
		s.Set("kip", map[string]any{"context_pct": 7})
		if got := s.Get("kip"); !reflect.DeepEqual(got, map[string]any{"context_pct": 7}) {
			t.Fatalf("Get after the replacing Set = %#v, want only the new field", got)
		}
	})

	t.Run("each id keeps its own entry, and an entry set to the empty map is present-but-empty rather than absent", func(t *testing.T) {
		s := newMemStore()
		s.Set("kip", map[string]any{"context_pct": 42})
		s.Set("ana", map[string]any{})
		if got := s.Get("ana"); got == nil || len(got) != 0 {
			t.Fatalf("Get(ana) = %#v, want an allocated empty entry", got)
		}
		if got := s.Get("kip"); !reflect.DeepEqual(got, map[string]any{"context_pct": 42}) {
			t.Fatalf("Get(kip) = %#v, want its own entry", got)
		}
		if got := s.Snapshot(); len(got) != 2 {
			t.Fatalf("Snapshot = %#v, want both ids", got)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("the deleted id reads back nil and leaves every other entry standing", func(t *testing.T) {
		s := newMemStore()
		s.Set("kip", map[string]any{"context_pct": 42})
		s.Set("ana", map[string]any{"context_pct": 7})
		s.Delete("kip")
		if got := s.Get("kip"); got != nil {
			t.Fatalf("Get after Delete = %#v, want nil", got)
		}
		if got := s.Get("ana"); !reflect.DeepEqual(got, map[string]any{"context_pct": 7}) {
			t.Fatalf("the other entry must survive, got %#v", got)
		}
		if got := s.Snapshot(); !reflect.DeepEqual(got, map[string]map[string]any{"ana": {"context_pct": 7}}) {
			t.Fatalf("Snapshot = %#v", got)
		}
	})

	t.Run("deleting an id that was never set, or deleting twice, changes nothing", func(t *testing.T) {
		s := newMemStore()
		s.Set("kip", map[string]any{"context_pct": 42})
		s.Delete("zoe")
		s.Delete("kip")
		s.Delete("kip")
		if got := s.Snapshot(); !reflect.DeepEqual(got, map[string]map[string]any{}) {
			t.Fatalf("Snapshot = %#v, want an empty store", got)
		}
	})
}

func TestSnapshot(t *testing.T) {
	t.Run("every entry is in the fold, and an empty store answers an allocated empty map", func(t *testing.T) {
		s := newMemStore()
		got := s.Snapshot()
		if got == nil || len(got) != 0 {
			t.Fatalf("Snapshot of an empty store = %#v, want an allocated empty map", got)
		}
		s.Set("kip", map[string]any{"context_pct": 42})
		s.Set("ana", map[string]any{"runtime": "codex", "logged_out": true})
		want := map[string]map[string]any{
			"kip": {"context_pct": 42},
			"ana": {"runtime": "codex", "logged_out": true},
		}
		if got := s.Snapshot(); !reflect.DeepEqual(got, want) {
			t.Fatalf("Snapshot = %#v, want %#v", got, want)
		}
	})

	t.Run("the fold is a copy at both levels: mutating it cannot reach the store", func(t *testing.T) {
		s := newMemStore()
		s.Set("kip", map[string]any{"context_pct": 42})
		got := s.Snapshot()
		got["kip"]["context_pct"] = 99
		got["ghost"] = map[string]any{"x": 1}
		want := map[string]map[string]any{"kip": {"context_pct": 42}}
		if again := s.Snapshot(); !reflect.DeepEqual(again, want) {
			t.Fatalf("the store was mutated through the snapshot: %#v", again)
		}
	})
}
