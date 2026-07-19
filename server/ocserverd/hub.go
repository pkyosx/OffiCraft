package main

// hub.go — the SSE hub (M3 step ④): the in-memory online/position projection
// the REST handlers read, PLUS the real delta fan-out (spec/sse.md).
//
//   - connection registry: a member's SSE connection projects it ONLINE for
//     the life of the connection (docs/design/state-model.md — online is a
//     pure connection projection, never a stored flag);
//   - the machine-claim projection (token machine_id → where the agent runs);
//   - the dual-SSE takeover (spec/sse.md §5.1): a member already holding a
//     live listener is atomically REPLACED by the new connection (kick the
//     old, admit the new, same critical section), clamped per member by an
//     anti-flap throttle whose excess falls back to the pre-stream 409;
//   - Publish: the commit-funnel fan-out (SSEHub.publish_change). Every
//     durable-write handler calls it exactly once per fenced write; it builds
//     the five-key frame envelope (spec/sse.md §2) and appends the wire text
//     to the listeners the frame is ADDRESSED to (buffer-backed, publish order
//     per connection — §4). Per-recipient routing (spec/sse.md §4, T-30d7):
//     each publish carries an Audience; an AGENT connection receives the frame
//     iff it is in that audience, while the owner/dashboard connection
//     (MemberID=="") ALWAYS receives it (the全局 cockpit view). This stops the
//     old全域廣播 where every online agent burned a wake on every other
//     agent's task/chat/member delta.
//   - the per-warden command FIFO (spec/sse.md §7): the NAT transport buffer
//     between a command producer and the addressed warden's drain loop.
//     In-memory only (restart drops it — harmless by contract).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// errDualSSE is the historical single-listener-per-member refusal message
// (spec/sse.md §5.1 pre-takeover). Connect no longer returns it — a second
// listener now TAKES OVER — but the wording is kept as the reference point the
// throttled variant below deliberately diverges from (client and serve logs
// must distinguish "old semantics leftover" from "anti-flap fallback").
var errDualSSE = errors.New("member already holds a live SSE connection")

// takeoverBurst / takeoverWindow: per-member anti-flap budget — a member may
// be kicked-and-replaced (takeover) at most takeoverBurst times within any
// takeoverWindow; an excess connect falls back to the pre-stream 409
// (errDualSSEThrottled). Burst=3: the zombie-slot scenario needs exactly 1;
// two live clients fighting over one member are clamped to 3 handovers per
// minute. Window=60s: deliberately < the client's refusal self-terminate
// grace (sseRefusalGrace 120s, cli/ocagent/listen.go), so a single legitimate
// client can never accumulate an unbroken 120s 409 run and kill itself — the
// window slides out and its next connect takes over successfully.
const (
	takeoverBurst  = 3
	takeoverWindow = 60 * time.Second
)

// errDualSSEThrottled is the takeover-over-budget 409 message. Kept distinct
// from errDualSSE so both client and serve logs can tell "anti-flap fallback"
// (two live clients suspected) apart at a glance. Wording is not contract
// (spec/sse.md §5.1) — the 409 status and pre-stream timing are.
var errDualSSEThrottled = errors.New(
	"member already holds a live SSE connection (takeover throttled: too many handovers; dual live clients suspected)")

// triggerServer is the frame `trigger` value for a server-internal producer
// (reconcile / scheduler / webhook fold — no acting request behind the write;
// spec/sse.md §2.3). Request-driven writes attribute the verified token sub
// instead ("owner" for owner scope — the sub IS the wireOwnerID literal — or
// the agent/worker member id; see requestTrigger in api_helpers.go).
const triggerServer = "server"

// hubListener is one open SSE connection. MemberID is "" for the owner
// (dashboard) connection — the owner never projects online. buf is this
// connection's delta backlog: SSE wire-text frames in publish order (the
// Python Listener deque).
type hubListener struct {
	MemberID  string
	MachineID string
	// Gen is this connection's process-local generation (Hub.connGen at admit
	// time): strictly increasing per successful Connect, never on the wire,
	// resets with the process (like Hub.seq). "New connection always wins" is
	// decided by the map handover under h.mu; Gen exists for log attribution
	// (who kicked whom, how long the incumbent lived) and test monotonicity.
	Gen int64
	// kicked is closed by Connect when a takeover displaces this listener —
	// the handler selects on it and returns. Closed at most once BY
	// CONSTRUCTION: the close happens under h.mu strictly after the listener
	// left the map, so a second takeover can never find it again.
	kicked chan struct{}
	// attachedAt is the admit time (hub clock) — the takeover marker's
	// incumbent_age input.
	attachedAt time.Time

	mu  sync.Mutex
	buf [][]byte
}

// push appends one wire-text frame to the listener's backlog (publish side).
func (l *hubListener) push(frame []byte) {
	l.mu.Lock()
	l.buf = append(l.buf, frame)
	l.mu.Unlock()
}

// pop removes and returns the oldest buffered frame, or nil when the backlog
// is empty (the stream loop's per-tick drain).
func (l *hubListener) pop() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) == 0 {
		return nil
	}
	frame := l.buf[0]
	l.buf = l.buf[1:]
	return frame
}

// Hub is the in-memory connection registry (single-owner: no owner keying).
type Hub struct {
	mu        sync.Mutex
	listeners map[*hubListener]bool
	// seq is the single process-local delta counter: pre-increment per publish
	// (first frame is 1), serves BOTH seq and epoch (spec/sse.md §2.1). It
	// deliberately resets on restart — clients are contracted to full-resync.
	seq int64
	// wardenCmds is the per-warden command FIFO (spec/sse.md §7), keyed by the
	// warden MEMBER id (the drain side's verified token sub). Values are
	// ready-to-write SSE wire text. Unbounded (a warden reconnect drains it).
	wardenCmds map[string][][]byte
	// connGen is the hub-wide monotonic connection generation (pre-increment
	// under h.mu; first connection is 1). Process-local like seq: an
	// exec/restart resets it — generations only need to compare within one
	// process lifetime.
	connGen int64
	// kicks is the per-member takeover timestamp trail (the anti-flap window
	// accounting). Appended on takeover, trimmed to the window on judgement —
	// naturally bounded at takeoverBurst entries per member, no leak.
	kicks map[string][]time.Time
	// clock is injectable for the throttle tests; NewHub defaults to time.Now.
	clock func() time.Time
}

func NewHub() *Hub {
	return &Hub{
		listeners:  map[*hubListener]bool{},
		wardenCmds: map[string][][]byte{},
		kicks:      map[string][]time.Time{},
		clock:      time.Now,
	}
}

// Connect registers a listener. memberID "" = the owner connection (always
// admitted, never projected online, exempt from takeover and throttle). A
// non-empty memberID that already holds a live listener TAKES OVER
// (spec/sse.md §5.1): the old listener is removed and its kicked channel
// closed IN THE SAME critical section the new listener is inserted in — the
// slot changes hands atomically, so the member stays online throughout and
// every Publish lands on exactly one of the two connections; the new
// connection never waits for the old handler to return. Anti-flap: more than
// takeoverBurst takeovers within takeoverWindow falls back to
// errDualSSEThrottled — the caller maps it to a 409 BEFORE the stream starts.
func (h *Hub) Connect(memberID, machineID string) (*hubListener, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.clock()
	var old *hubListener
	if memberID != "" {
		for l := range h.listeners {
			if l.MemberID == memberID {
				old = l
				break
			}
		}
		if old != nil {
			// Trim the window-expired kick stamps, then judge the budget.
			recent := h.kicks[memberID][:0]
			for _, t := range h.kicks[memberID] {
				if now.Sub(t) < takeoverWindow {
					recent = append(recent, t)
				}
			}
			if len(recent) >= takeoverBurst {
				h.kicks[memberID] = recent
				fmt.Fprintf(os.Stderr,
					"[sse] takeover throttled: member=%s kicks=%d window=%s — refusing with 409 (two live clients suspected)\n",
					memberID, len(recent), takeoverWindow)
				return nil, errDualSSEThrottled
			}
			h.kicks[memberID] = append(recent, now)
			delete(h.listeners, old) // slot handover: same critical section as the insert below
			close(old.kicked)        // tell the old handler to return (≤ssePoll observable)
		}
	}
	h.connGen++
	l := &hubListener{
		MemberID:   memberID,
		MachineID:  machineID,
		Gen:        h.connGen,
		kicked:     make(chan struct{}),
		attachedAt: now,
	}
	h.listeners[l] = true
	if old != nil {
		fmt.Fprintf(os.Stderr,
			"[sse] takeover: member=%s old_gen=%d new_gen=%d incumbent_age=%s (kicks_in_window=%d)\n",
			memberID, old.Gen, l.Gen,
			now.Sub(old.attachedAt).Round(time.Millisecond), len(h.kicks[memberID]))
	}
	return l, nil
}

// Disconnect unregisters l (the online projection drops with it). It reports
// lastForMember: true iff l was actually removed from the map by THIS call AND
// no other listener keeps the member online afterwards — the §5.2
// last-disconnect edge gate. A kicked listener's deferred Disconnect returns
// false (the takeover already removed it and the new listener still holds the
// slot), so the edge hooks never fire while the member is still online. Owner
// listeners (MemberID=="") always report false.
func (h *Hub) Disconnect(l *hubListener) (lastForMember bool) {
	if l == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	removed := h.listeners[l]
	delete(h.listeners, l)
	if !removed || l.MemberID == "" {
		return false
	}
	for other := range h.listeners {
		if other.MemberID == l.MemberID {
			return false
		}
	}
	return true
}

// IsOnline reports the live SSE-connection projection for one member — the
// SINGLE online source (SSEHub.is_online).
func (h *Hub) IsOnline(memberID string) bool {
	if memberID == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for l := range h.listeners {
		if l.MemberID == memberID {
			return true
		}
	}
	return false
}

// OnlineMembers returns the set of member ids currently holding a live SSE
// connection (SSEHub.online_members).
func (h *Hub) OnlineMembers() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]bool{}
	for l := range h.listeners {
		if l.MemberID != "" {
			out[l.MemberID] = true
		}
	}
	return out
}

// MachineOf returns the live SSE machine claim for a member (the token's
// WHERE), or "" when the member holds no connection / no claim.
func (h *Hub) MachineOf(memberID string) string {
	if memberID == "" {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for l := range h.listeners {
		if l.MemberID == memberID {
			return l.MachineID
		}
	}
	return ""
}

// MachinesOf returns the DISTINCT machine claims a member is live on right now —
// the set generalization of MachineOf (which returns just the first). It is the
// "actual appearances across wardens" input to the single-session convergence
// (kyle-a-p2-singlesession-design.md §5.1): for each machine in this set that is
// NOT in the member's allowed set, the residual session there is reaped.
//
// NOTE (design-note §4 叉口 I): dual-SSE takeover (Connect) keeps at most ONE
// live listener per member, so today this returns ≤1 machine — set-valued by
// construction but single-valued in practice. It is deliberately set-shaped so
// that (a) it speaks the owner's set-difference vocabulary, and (b) if the
// dual-SSE handshake is ever relaxed to admit concurrent per-machine listeners,
// this query surfaces the extra appearances with ZERO caller change. Blank
// claims (an owner connection, or a listener with no machine token) are dropped.
func (h *Hub) MachinesOf(memberID string) []string {
	if memberID == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for l := range h.listeners {
		if l.MemberID != memberID || l.MachineID == "" || seen[l.MachineID] {
			continue
		}
		seen[l.MachineID] = true
		out = append(out, l.MachineID)
	}
	return out
}

// AgentsOnMachine returns the member ids whose live SSE carries a machine
// claim for machineID (the teardown guard input — SSEHub.agents_on_machine).
func (h *Hub) AgentsOnMachine(machineID string) []string {
	if machineID == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for l := range h.listeners {
		if l.MemberID != "" && l.MachineID == machineID {
			out = append(out, l.MemberID)
		}
	}
	return out
}

// sseTopics is the CLOSED 12-topic vocabulary (spec/sse.md §3.1; reply_card
// joined in the M2 reply-card batch; task / outsource_worker / task_manual in
// the M3 task batch). Enforced at the publish seam (the mechanism §8
// recommends): a topic outside the set is dropped, so a typo can never mint a
// phantom wire topic.
var sseTopics = map[string]bool{
	"member":           true,
	"chat":             true,
	"chat_read":        true,
	"reply_card":       true,
	"task":             true,
	"outsource_worker": true,
	"task_manual":      true,
	"global_context":   true,
	"role_def":         true,
	"lessons":          true,
	"context":          true,
	"monitoring":       true,
}

// jsonFloat marshals a float64 so it ALWAYS reads back as a float (a bare
// integer literal would json-parse as int; the frame ts is contractually a
// float unix-epoch timestamp).
type jsonFloat float64

func (f jsonFloat) MarshalJSON() ([]byte, error) {
	s := strconv.FormatFloat(float64(f), 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return []byte(s), nil
}

// sseFrameData is the inner delta object (spec/sse.md §2).
type sseFrameData struct {
	Entity  string `json:"entity"`
	Key     string `json:"key"`
	Epoch   int64  `json:"epoch"`
	Deleted bool   `json:"deleted"`
	Payload any    `json:"payload"`
}

// sseFrame is the six-key delta envelope (spec/sse.md §2): the five frozen
// M1 keys plus `trigger` (spec §2.3, T-f39c) — the verified actor whose
// action caused the durable write ("owner" / "server" / an agent-scope sub).
// Attribution metadata ONLY: it never changes fan-out; the ocagent listener
// uses it client-side to drop its own echoes (trigger == self).
type sseFrame struct {
	Seq     int64        `json:"seq"`
	Topic   string       `json:"topic"`
	Op      string       `json:"op"`
	Data    sseFrameData `json:"data"`
	Ts      jsonFloat    `json:"ts"`
	Trigger string       `json:"trigger"`
}

// Audience selects which listeners a published frame reaches (spec/sse.md §4 —
// per-recipient routing, T-30d7). It constrains AGENT connections only: the
// owner/dashboard connection (MemberID=="") ALWAYS receives every frame (the
// 全局 cockpit view), independent of Audience. Members is the set of agent
// member ids the frame is addressed to; All is the system-broadcast escape
// hatch (spec §4 "owner_id is None") that reaches every agent as well.
type Audience struct {
	All     bool
	Members map[string]bool
}

// audienceAll is the system broadcast: every listener (owner + all agents).
func audienceAll() Audience { return Audience{All: true} }

// audienceOwnerOnly reaches ONLY owner/dashboard connections — no agent gets
// it. Used by the topics no agent consumes on the wire (chat_read /
// outsource_worker / task_manual / global_context / role_def / lessons /
// context / monitoring): fanning them to agents was pure wake waste.
func audienceOwnerOnly() Audience { return Audience{} }

// audienceMembers addresses a specific set of agent member ids (the owner is
// always included by Publish regardless). Blank ids are dropped — an
// unassigned executor, an absent creator on a pre-column row, or an empty peer
// simply narrows the set, never widens it.
func audienceMembers(ids ...string) Audience {
	m := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			m[id] = true
		}
	}
	return Audience{Members: m}
}

// Publish is the commit funnel → fan-out (SSEHub.publish_change): every
// durable-write handler calls it exactly once per fenced write. It builds one
// five-key frame (id == seq == epoch; op remove ⇒ deleted:true + payload
// null) and appends the wire text to the buffers of the listeners the frame is
// ADDRESSED to (spec/sse.md §4): the owner/dashboard connection always, plus
// every agent connection in aud. seq stays a single process-local counter
// (incremented once per publish, epoch==seq) — a filtered connection therefore
// observes a monotonic SUBSEQUENCE with gaps, which is expected and harmless
// (no replay; clients full-resync on reconnect, spec §2.1). Never fails into
// the durable write it follows: an unknown topic or a marshal fault drops the
// event silently (spec §3.1).
//
// trigger is the verified actor of the write (spec §2.3): "owner" / "server"
// / an agent-scope token sub. A blank trigger folds to triggerServer so the
// wire never carries an empty attribution from a producer that forgot it.
func (h *Hub) Publish(topic, op, entity, key string, payload any, aud Audience, trigger string) {
	if !sseTopics[topic] {
		return
	}
	if trigger == "" {
		trigger = triggerServer
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	seq := h.seq
	deleted := op == "remove"
	if deleted {
		payload = nil
	}
	frame := sseFrame{
		Seq:   seq,
		Topic: topic,
		Op:    op,
		Data: sseFrameData{
			Entity:  entity,
			Key:     key,
			Epoch:   seq,
			Deleted: deleted,
			Payload: payload,
		},
		Ts:      jsonFloat(float64(time.Now().UnixNano()) / 1e9),
		Trigger: trigger,
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		return // fan-out failure must never fail the write that triggered it
	}
	text := []byte("id: " + strconv.FormatInt(seq, 10) + "\ndata: " + string(raw) + "\n\n")
	for l := range h.listeners {
		// The owner/dashboard connection (MemberID=="") is全量 by contract;
		// an agent connection receives the frame iff addressed (spec §4).
		if l.MemberID == "" || aud.All || aud.Members[l.MemberID] {
			l.push(text)
		}
	}
}

// PushDirected appends one directed wire-text frame onto memberID's live
// listener buffer (the task-close nudge band, spec/sse.md §8). Best-effort
// at-most-once BY DESIGN: no live connection → the frame is dropped, never
// queued — a nudge is a reminder, not a command (contrast the warden FIFO
// below, which buffers across the NAT gap). Returns whether a listener took
// the frame.
func (h *Hub) PushDirected(memberID string, frame []byte) bool {
	if memberID == "" || len(frame) == 0 {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for l := range h.listeners {
		if l.MemberID == memberID {
			l.push(frame)
			return true
		}
	}
	return false
}

// EnqueueWardenCommand appends one directed command frame (SSE wire text) to
// wardenID's FIFO backlog (spec/sse.md §7 — the NAT transport's server half).
// The frame is drained ONLY by the connection whose verified token sub is
// wardenID, never the owner fan-out (the riding member_token is a secret).
func (h *Hub) EnqueueWardenCommand(wardenID string, frame []byte) {
	if wardenID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.wardenCmds[wardenID] = append(h.wardenCmds[wardenID], frame)
}

// DrainWardenCommands pops and returns ALL of wardenID's pending command
// frames in FIFO order (nil when none) — at-most-once onto the downstream: a
// frame drained into a dying connection is lost by design (recovery is
// re-decision from presence, never redelivery).
func (h *Hub) DrainWardenCommands(wardenID string) [][]byte {
	if wardenID == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	pending := h.wardenCmds[wardenID]
	if len(pending) == 0 {
		return nil
	}
	delete(h.wardenCmds, wardenID)
	return pending
}

// ── in-memory observation stores (context gauge + warden telemetry) ──────────
//
// lifecycle.md §3: both stores are VOLATILE BY DESIGN (restart amnesia is
// contract) and key on the VERIFIED token sub.

// memStore is a threadsafe map[member id]entry for the two ingest stores.
type memStore struct {
	mu      sync.Mutex
	entries map[string]map[string]any
}

func newMemStore() *memStore {
	return &memStore{entries: map[string]map[string]any{}}
}

// Get returns a COPY of the entry (nil when absent) — callers never mutate
// shared state without going through Set.
func (s *memStore) Get(id string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return nil
	}
	out := make(map[string]any, len(entry))
	for k, v := range entry {
		out[k] = v
	}
	return out
}

func (s *memStore) Set(id string, entry map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = entry
}

func (s *memStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
}

// Snapshot returns a shallow copy of the whole store (the monitoring fold
// input).
func (s *memStore) Snapshot() map[string]map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]map[string]any, len(s.entries))
	for id, entry := range s.entries {
		copied := make(map[string]any, len(entry))
		for k, v := range entry {
			copied[k] = v
		}
		out[id] = copied
	}
	return out
}
