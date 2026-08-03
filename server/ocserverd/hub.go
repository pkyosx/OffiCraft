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
//     In memory, with a DURABLE MIRROR for the one verb a restart would lose
//     outright (`update` — T-66a2 L3, see the warden-command section below).

import (
	"bytes"
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
	// ready-to-write SSE wire text, each tagged with the member/worker the frame
	// ACTS ON. Unbounded (a warden reconnect drains it).
	//
	// The subject tag is load-bearing, not bookkeeping: ONE machine's queue is
	// shared by every member and worker on it, so a bare depth answers "does this
	// machine owe anybody a frame", never "is THIS worker's start frame still
	// waiting". Reading the first as the second is how a receipt could accuse a
	// healthy warden of not running while it was demonstrably draining somebody
	// else's frame (T-e0e3 review C.1).
	wardenCmds map[string][]wardenCmd
	// cmdUndelivered is the per-ADDRESSED-MEMBER note that a drained command
	// frame never reached the socket (T-66a2). One entry per member, overwritten
	// by the next loss and deleted by the next dispatch — bounded by the roster,
	// volatile like every other hub store.
	cmdUndelivered map[string]undeliveredCommand
	// cmdStore is the DURABLE mirror of wardenCmds for the verbs that cannot be
	// re-derived after a process death (T-66a2 L3; today: `update` alone —
	// persistableCommandVerb). nil = no store bound (the route-shape harness
	// builds a DAL-less server), in which case the FIFO stays purely in-memory
	// exactly as before.
	cmdStore wardenCommandStore
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
		listeners:      map[*hubListener]bool{},
		wardenCmds:     map[string][]wardenCmd{},
		cmdUndelivered: map[string]undeliveredCommand{},
		kicks:          map[string][]time.Time{},
		clock:          time.Now,
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

// sseTopics is the CLOSED 13-topic vocabulary (spec/sse.md §3.1; reply_card
// joined in the M2 reply-card batch; task / outsource_worker / task_manual in
// the M3 task batch; insight in T-3809). Enforced at the publish seam (the
// mechanism §8 recommends): a topic outside the set is dropped, so a typo can
// never mint a phantom wire topic.
//
// ⚠️ That drop is SILENT by design, and it has bitten: a restore published
// "role" instead of "role_def" and fanned nothing at all, with a 200 on the
// wire and no error anywhere (the case is documented at
// publishDocumentHistoryRestore). Adding a topic here is therefore only half
// the work — every switch that maps a document kind to a topic has to learn it
// too, and Go will not tell you when one of them did not.
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
	"insight":          true,
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

// ── the durable half of the FIFO (T-66a2 L3) ────────────────────────────────
//
// WHY ONLY `update` IS PERSISTED. The queue is process-local memory, so a
// server restart used to empty it. For START / STOP / UNINSTALL that is
// genuinely harmless: the reconcile producer re-derives all three from observed
// presence within one 30s cadence, so a restart costs at most one tick. For
// `update` — the owner's upgrade click — NOTHING anywhere re-derives it, and
// `POST /api/update/upgrade` deliberately re-execs the server, which makes
// "restart between enqueue and drain" a DESIGNED-IN event rather than a
// hypothetical race. Persisting the three self-healing verbs as well would add
// a second, slower source of truth for decisions the producer already owns
// (and a replayed STOP from before the restart is actively worse than a lost
// one — the world has moved on), and a START frame carries a member_token: a
// live secret we must not write at rest, and one that would be expired by the
// time anything replayed it. So the durable set is exactly the verbs with no
// compensating re-decision, which today means `update` alone.
//
// WHAT THIS DOES *NOT* BUY. There is still NO delivery guarantee and no ack in
// this band (the acknowledgement item is explicitly deferred). The only event
// the server can observe is "the frame was written to the socket without
// error", which is not "the warden received it" and certainly not "the warden
// acted on it". A frame is therefore forgotten on a successful WRITE, and the
// honest statement of what survives a restart is "a command nobody has managed
// to write yet", not "a command that has not been carried out".
//
// DUPLICATES ARE EXPECTED AND HARMLESS. A crash after the write but before the
// delete replays the command on the next boot. `update` is a kick into the
// warden's existing content-hash self-update reconcile: if the bytes already
// match, it swaps nothing and restarts nothing, so a redundant kick is a cheap
// no-op. That idempotence is what makes persisting this verb (and only this
// verb) safe to replay at all.

// wardenCommandStore is the durable-queue surface the hub needs; *DAL is the
// only implementation. An interface rather than a *DAL field so the hub keeps
// no database dependency and the persistence tests can inject a store that
// fails on demand.
type wardenCommandStore interface {
	PutWardenCommand(WardenCommand) error
	DeleteWardenCommand(wardenID, verb, memberID string) error
	ListWardenCommands() ([]WardenCommand, error)
	DeleteWardenCommandsBefore(cutoff float64) (int64, error)
}

// wardenCommandTTL bounds how long an undrained command may sit in the durable
// queue. The queue itself is already bounded by its natural key (one pending
// row per warden+verb+target — the roster, not the traffic), so this is not a
// size cap: it is a staleness cap. A day-old upgrade click names a build that
// has almost certainly been superseded, and the warden's own 15-minute
// self-update poll has had ~96 chances to converge without it, so replaying it
// would be archaeology, not recovery.
const wardenCommandTTL = 24 * time.Hour

// persistableCommandVerb reports whether a verb outlives the process. See the
// section comment above: only the verbs with no compensating re-decision.
func persistableCommandVerb(verb string) bool {
	return verb == reconcileCmdUpdate
}

// commandStoreWrite is one durable-queue write PLANNED under h.mu and EXECUTED
// after it is released.
//
// WHY THE TWO PHASES ARE NOT OPTIONAL: h.mu is the single lock guarding the
// listener registry, so Publish (the exit of every durable-write handler) and
// Connect (the entry of every SSE connection) both take it. The store is
// SQLite behind ONE pooled connection with a 5s busy timeout, so a single
// contended write can park for seconds — measured at 4.9s of server-wide
// Publish stall when the persist ran inside the critical section. The hub had
// no database dependency before this work and must not acquire a blocking one:
// nothing here may hold h.mu across store I/O.
type commandStoreWrite struct {
	store  wardenCommandStore
	cmd    WardenCommand
	digest wardenCommandDigest
}

// planCommandPersistLocked decides whether a frame needs a durable row and
// assembles the write — pure bookkeeping, NO I/O. Caller holds h.mu.
func (h *Hub) planCommandPersistLocked(
	wardenID string, digest wardenCommandDigest, frame []byte,
) (commandStoreWrite, bool) {
	if h.cmdStore == nil || digest.MemberID == "" || !persistableCommandVerb(digest.Verb) {
		return commandStoreWrite{}, false
	}
	return commandStoreWrite{
		store:  h.cmdStore,
		digest: digest,
		cmd: WardenCommand{
			WardenID:   wardenID,
			Verb:       digest.Verb,
			MemberID:   digest.MemberID,
			Frame:      frame,
			EnqueuedTS: float64(h.clock().UnixNano()) / 1e9,
		},
	}, true
}

// runCommandPersists executes planned writes with NO hub lock held. Best effort
// by construction: the in-memory FIFO is the live path and has already accepted
// the frame, so a store failure NEVER fails the dispatch — it only downgrades it
// to the pre-T-66a2 behaviour (lost on restart). It must not be silent though,
// which is what noteCommandStoreFailure is for.
//
// KNOWN, PRE-EXISTING RACE — NOT caused by doing this outside the lock, and
// NOT fixable by moving it back in. A persist and the clearing delete for the
// SAME natural key can interleave, so a second upgrade click landing as the
// first one's write completes can have its row removed: click #2 is live in the
// FIFO but has no durable row, and a restart loses it. Measured reproducible on
// e5b480c too, where BOTH the Put and the Delete ran inside h.mu — the lock
// only serialises the two operations, it never prevented the "Put completes,
// then a same-key Delete lands" ORDER. This is a property of clearing by
// natural key on write success, and the two-phase locking here only changes the
// width of the window. Anyone tempted to fix it by pulling the store call back
// into the critical section will not fix it; they will only restore the ~5s
// server-wide Publish freeze.
//
// The sharpest form, stated plainly: this band has no ack, so "written to the
// wire" is not "delivered". The reason a user clicks upgrade a SECOND time is
// very often that the first click silently did nothing — so the click that
// loses its restart insurance is precisely the one the user considers
// necessary, not a redundant one. That stays inside the accepted "no ack"
// envelope, but it is not a harmless duplicate and must not be described as
// one. Closing it needs per-command identity + acknowledgement, which is the
// deferred item.
//
// RESIDUAL, NAMED: the CALLER of the dispatch still waits for its own store
// write (up to the busy timeout — measured 5.1s against an externally locked
// database). In practice that is the owner's upgrade button and the return path
// of an already-dying SSE connection handler; a reconcile tick never touches
// the store at all, because planCommandPersistLocked returns false for the only
// verbs reconcile dispatches. Synchronous is kept for a POSTCONDITION, not for
// error reporting (a failure is a stderr line either way and the caller gets
// nothing back): when EnqueueWardenCommand returns, the row IS there. The guard
// tests read the store immediately after enqueue and depend on exactly that,
// and going async would spawn an unbounded, unjoined, shutdown-unaware goroutine
// per enqueue.
func runCommandPersists(writes []commandStoreWrite) {
	for _, w := range writes {
		if err := w.store.PutWardenCommand(w.cmd); err != nil {
			noteCommandStoreFailure("persist", w.cmd.WardenID, w.digest, err)
		}
	}
}

// noteCommandStoreFailure is the OUTSIDE-VISIBLE trace of a durable-queue
// failure. Deliberately the same stderr channel and "[sse] warden command …"
// prefix the undelivered accounting uses, so the operator reading a delivery
// problem finds both halves in one place. It names warden + verb + target and
// NEVER the frame body (an `args` payload can carry a token). The wire cannot
// carry this — the HTTP/MCP surface is frozen — and lying about it in the
// dispatch response would be worse: the command IS enqueued and WILL be
// delivered if the process survives; all that was lost is its restart
// insurance, which is exactly what this line says.
func noteCommandStoreFailure(op, wardenID string, digest wardenCommandDigest, err error) {
	fmt.Fprintf(os.Stderr,
		"[sse] warden command queue %s FAILED: warden=%s verb=%s target=%s — %v "+
			"(the command is still queued in memory and will be delivered if this "+
			"process survives; it will NOT survive a restart)\n",
		op, wardenID, digest.Verb, digest.MemberID, err)
}

// BindWardenCommandStore attaches the durable queue and REHYDRATES the FIFO
// from it — the whole point of the exercise, called once during server
// assembly (newAPIServer). Restoring at assembly time rather than at first
// drain means the queue is whole before any warden can connect, and it makes
// "the server restarted" testable as "build a second apiServer over the same
// DAL", which is what the guard test does.
//
// Expired rows are swept first (wardenCommandTTL). A restored frame is appended
// only if an identical one is not already queued, so binding twice cannot
// duplicate an order.
//
// BOTH STORE CALLS HAPPEN BEFORE h.mu IS TAKEN — the lock only installs the
// store and appends the already-fetched frames. See commandStoreWrite for why
// no hub path may block on SQLite while holding the listener lock.
//
// KNOWN LIMITATION — restore does NOT re-check reachability. The dispatch path
// (enqueueToWarden) refuses to enqueue for a warden that is not online, but a
// restored frame skips that gate: it was already accepted by a live dispatch
// before the restart, and re-deriving reachability at boot would only ask a
// question nothing can answer yet (no warden has reconnected). The cost is that
// a command for a machine deleted from the roster while the server was down is
// still rehydrated into memory and still sits in the store. Nothing will ever
// drain it, so it is inert, and the TTL sweep removes it within a day — but it
// IS carried, and no roster removal prunes it today.
//
// KNOWN LIMITATION — this is a SIDE EFFECT of assembly. Two apiServers built
// over one live DAL each restore the same rows, so the same command ends up in
// two different hubs. Production assembles exactly once (cmdServe), so this is
// not a live bug; it is a trap for any future code that builds a second server
// over a running store.
func (h *Hub) BindWardenCommandStore(store wardenCommandStore) {
	if store == nil {
		return
	}
	cutoff := float64(h.clock().Add(-wardenCommandTTL).UnixNano()) / 1e9
	if expired, err := store.DeleteWardenCommandsBefore(cutoff); err != nil {
		fmt.Fprintf(os.Stderr,
			"[sse] warden command queue expiry sweep FAILED: %v (stale commands may be replayed)\n", err)
	} else if expired > 0 {
		fmt.Fprintf(os.Stderr,
			"[sse] warden command queue: dropped %d command(s) older than %s\n",
			expired, wardenCommandTTL)
	}
	pending, listErr := store.ListWardenCommands()

	h.mu.Lock()
	defer h.mu.Unlock()
	h.cmdStore = store
	if listErr != nil {
		fmt.Fprintf(os.Stderr,
			"[sse] warden command queue restore FAILED: %v — commands pending at the "+
				"last shutdown are lost; an upgrade click may need to be repeated\n", listErr)
		return
	}
	restored := 0
	// Append in list order: ListWardenCommands is contracted to return enqueue
	// order, and spec/sse.md §7 requires the drain to pop in FIFO order — the
	// rebuilt queue must not reorder what the crash interrupted.
	for _, c := range pending {
		if c.WardenID == "" || len(c.Frame) == 0 || containsFrame(h.wardenCmds[c.WardenID], c.Frame) {
			continue
		}
		// Subject = the persisted row's MemberID: that column IS the subject
		// (planCommandPersistLocked fills it from the frame's args.member_id,
		// the same id enqueueToWarden passes as the subject), so a rehydrated
		// frame stays attributable to the member/worker it acts on. Restoring it
		// untagged would make PendingWardenCommandsFor blind to exactly the
		// frames the restart was supposed to save.
		h.wardenCmds[c.WardenID] = append(h.wardenCmds[c.WardenID],
			wardenCmd{Subject: c.MemberID, Frame: c.Frame})
		restored++
		fmt.Fprintf(os.Stderr,
			"[sse] warden command restored across restart: warden=%s verb=%s target=%s\n",
			c.WardenID, c.Verb, c.MemberID)
	}
	if restored > 0 {
		fmt.Fprintf(os.Stderr,
			"[sse] warden command queue: %d command(s) survived the restart and will be "+
				"delivered when the addressed warden reconnects\n", restored)
	}
}

// MarkWardenCommandWritten forgets a persisted command once the stream loop has
// written it to the warden's socket without error.
//
// READ THIS BEFORE TRUSTING IT: a successful write is NOT an acknowledgement.
// It means the bytes entered our side of the connection, nothing more — the
// peer may never read them. This band has no ack (that item is deferred), so
// "written" is the strongest event the server can observe, and it is what the
// durable queue is cleared on. The residual exposure is a frame written into a
// socket the warden never drained, which this design does not recover; what it
// does recover is the much larger case of a command that was never written at
// all because the process died first.
//
// The delete runs with NO hub lock held (only the store handle is read under
// h.mu) — this is called from inside the SSE write loop, and blocking every
// listener behind a SQLite delete is exactly the coupling commandStoreWrite
// forbids.
func (h *Hub) MarkWardenCommandWritten(wardenID string, frame []byte) {
	digest, ok := decodeWardenCommandFrame(frame)
	if !ok || digest.MemberID == "" || !persistableCommandVerb(digest.Verb) {
		return
	}
	h.mu.Lock()
	store := h.cmdStore
	h.mu.Unlock()
	if store == nil {
		return
	}
	if err := store.DeleteWardenCommand(wardenID, digest.Verb, digest.MemberID); err != nil {
		// A failed delete leaves a row that will be replayed on the next boot.
		// Harmless (see the duplicate note above) but worth naming.
		noteCommandStoreFailure("clear", wardenID, digest, err)
	}
}

// wardenCmd is one queued warden command: the ready-to-write SSE wire text plus
// the id of the member/worker it acts on. Subject is "" only for a frame whose
// enqueuer genuinely had no subject; it is never a wildcard.
type wardenCmd struct {
	Subject string
	Frame   []byte
}

// EnqueueWardenCommand appends one directed command frame (SSE wire text) to
// wardenID's FIFO backlog (spec/sse.md §7 — the NAT transport's server half).
// The frame is drained ONLY by the connection whose verified token sub is
// wardenID, never the owner fan-out (the riding member_token is a secret).
func (h *Hub) EnqueueWardenCommand(wardenID string, frame []byte) {
	h.EnqueueWardenCommandFor(wardenID, "", frame)
}

// EnqueueWardenCommandFor is EnqueueWardenCommand with the frame's SUBJECT — the
// member or worker id the command acts on — recorded alongside it, so a later
// reader can ask "is THIS one's frame still waiting" instead of only "is anything
// waiting". Every production enqueue knows its subject (they all come through
// enqueueToWarden, whose first argument is exactly that id); the untagged
// EnqueueWardenCommand remains for callers that genuinely have no subject.
func (h *Hub) EnqueueWardenCommandFor(wardenID, subject string, frame []byte) {
	if wardenID == "" {
		return
	}
	var writes []commandStoreWrite
	h.mu.Lock()
	h.wardenCmds[wardenID] = append(h.wardenCmds[wardenID],
		wardenCmd{Subject: subject, Frame: frame})
	// A FRESH dispatch supersedes any earlier "this member's frame never made
	// it" note (T-66a2): the note exists to explain ONE lost dispatch, and the
	// reader (stampWakeObservability) must never attribute a stale loss to the
	// attempt now in flight.
	if digest, ok := decodeWardenCommandFrame(frame); ok && digest.MemberID != "" {
		delete(h.cmdUndelivered, digest.MemberID)
		if w, queued := h.planCommandPersistLocked(wardenID, digest, frame); queued {
			writes = append(writes, w)
		}
	}
	h.mu.Unlock()
	// Durable write AFTER the unlock: the command is already live in the FIFO,
	// so nothing waits on this — least of all Publish.
	runCommandPersists(writes)
}

// PendingWardenCommands reports how many command frames are STILL sitting in
// wardenID's FIFO — i.e. enqueued by the server and not yet collected by any
// connection claiming that warden. Read-only (it never pops), and it is the only
// in-process observable that separates "the frame was never even picked up" from
// "it was picked up and the boot failed": EnqueueWardenCommand succeeding means
// only that the frame was appended to this map, never that anybody read it.
func (h *Hub) PendingWardenCommands(wardenID string) int {
	if wardenID == "" {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.wardenCmds[wardenID])
}

// PendingWardenCommandsFor is the PER-SUBJECT backlog: how many of wardenID's
// still-uncollected frames act on `subject`. This — not the queue depth — is what
// a per-worker receipt may assert on, because one machine's FIFO is shared by
// every member and worker placed there: a start frame for THIS worker that has
// been collected leaves the queue non-empty whenever anybody ELSE on that machine
// is also waiting, and a receipt reading that depth would tell the owner the
// machine's warden is not running while it was, at that moment, draining.
//
// An empty subject can never match a tagged frame (and never matches at all): the
// question "is this nameless thing's frame queued" has no honest answer, so the
// caller must not ask it — see the target=="" arm in reconcileWorkerLiveness.
func (h *Hub) PendingWardenCommandsFor(wardenID, subject string) int {
	if wardenID == "" || subject == "" {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.wardenCmds[wardenID] {
		if c.Subject == subject {
			n++
		}
	}
	return n
}

// DrainWardenCommands pops and returns ALL of wardenID's pending command
// frames in FIFO order (nil when none). The pop is destructive by design —
// at-most-once onto the downstream — but the caller now owes the frames back:
// whatever it could NOT write must be handed to ReturnUndeliveredCommands, so
// the loss is accounted for instead of being garbage-collected in silence.
//
// The DURABLE rows are deliberately NOT deleted here. A drain is only the
// intention to write; the row is cleared per frame by MarkWardenCommandWritten
// once the write actually succeeded, so a process that dies mid-drain still
// finds its undrained `update` on the next boot.
func (h *Hub) DrainWardenCommands(wardenID string) []wardenCmd {
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

// undeliveredCommand is the note left behind when a command frame was popped
// off the FIFO but never reached the socket. Keyed by the ADDRESSED member (the
// frame's args.member_id — the subject of the order), not by the warden, because
// the only consumer is the wake diagnosis for that member. Volatile, like every
// other hub store; it explains one dispatch, not a history.
type undeliveredCommand struct {
	Verb     string  // the rpc that was lost ("start" / "stop" / "update" / ...)
	Warden   string  // the warden whose stream died mid-delivery
	At       float64 // epoch seconds of the loss
	Requeued bool    // true → the frame was put back and WILL be retried
}

// ReturnUndeliveredCommands accounts for frames that DrainWardenCommands popped
// but the stream loop could not write (the connection died mid-drain). Before
// T-66a2 those frames were simply discarded by the returning handler: no log,
// no receipt, no field — a lost order was indistinguishable from one that was
// never sent, which is the whole reason nobody could tell where to look.
//
// Two deliberately different treatments:
//
//   - `update` is RE-ENQUEUED at the FRONT of the FIFO. It is the one verb with
//     NO compensating re-decision anywhere: the 30s reconcile cadence re-derives
//     start/stop/uninstall from observed presence, but nothing re-derives "the
//     owner asked this machine to upgrade". It is also the verb most likely to
//     be in flight when the stream dies, because POST /api/update/upgrade
//     deliberately re-execs the server. The self-update it kicks is idempotent
//     (content-hash swap oracle), so a redundant retry is a cheap no-op.
//   - EVERY OTHER VERB STAYS DROPPED. That is not an oversight: at-most-once
//     into a dying connection is the contract this band was built on, and
//     redelivering a stale START/STOP after the world has moved on is worse than
//     losing it (reconcile will re-decide from live presence within one cadence).
//     What changes is that the drop is now LOUD: one stderr line per frame plus
//     a note the wake diagnosis reads, so an outside observer can tell "never
//     delivered" from "delivered and failed".
//
// Returns how many frames were requeued and how many were dropped.
func (h *Hub) ReturnUndeliveredCommands(wardenID string, undelivered []wardenCmd) (requeued, dropped int) {
	if wardenID == "" || len(undelivered) == 0 {
		return 0, 0
	}
	var writes []commandStoreWrite
	h.mu.Lock()
	at := float64(h.clock().UnixNano()) / 1e9
	var back []wardenCmd
	for _, cmd := range undelivered {
		frame := cmd.Frame
		digest, ok := decodeWardenCommandFrame(frame)
		verb := digest.Verb
		if !ok || verb == "" {
			// An unparseable frame is still a loss worth naming; it just cannot
			// be attributed to a verb or a member.
			verb = "unknown"
		}
		retry := verb == reconcileCmdUpdate &&
			!containsFrame(h.wardenCmds[wardenID], frame) && !containsFrame(back, frame)
		if retry {
			// The subject tag rides back with the frame: a requeued command is
			// still THAT member's command, and a per-subject backlog read after
			// a requeue must keep seeing it.
			back = append(back, cmd)
			requeued++
			// Second chance at durability: the row normally already exists (the
			// enqueue wrote it and only a successful WRITE clears it), and the
			// store's conflict rule makes this a no-op in that case. It matters
			// when the original persist failed — the frame is provably still
			// wanted, so try again rather than leave it uninsured. Planned
			// here, executed after the unlock (see commandStoreWrite).
			if w, queued := h.planCommandPersistLocked(wardenID, digest, frame); queued {
				writes = append(writes, w)
			}
		} else {
			dropped++
		}
		if digest.MemberID != "" {
			h.cmdUndelivered[digest.MemberID] = undeliveredCommand{
				Verb: verb, Warden: wardenID, At: at, Requeued: retry,
			}
			// KNOWN, DELIBERATE: a note written on the REQUEUE path is never
			// cleared when the retry succeeds. Only EnqueueWardenCommand deletes
			// notes, and a requeue writes to the FIFO directly (it is a retry of
			// a frame already dispatched, not a new dispatch). Clearing here
			// would erase the note in the same critical section that writes it,
			// making the Requeued flag dead; clearing on successful redelivery
			// would need per-frame delivery tracking — the ack/correlation-id
			// machinery that is explicitly DEFERRED. It cannot mislead today:
			// the only consumer (stampWakeObservability) gates on
			// `note.Verb == start`, and a requeue only ever happens for
			// `update`. A future reader adding a second consumer must not assume
			// a requeued note means "still lost".
		}
		action := "DROPPED (at-most-once contract — reconcile re-decides from presence)"
		if retry {
			action = "REQUEUED (update has no re-decision path)"
		}
		// Never print the frame itself: a START carries the member_token.
		fmt.Fprintf(os.Stderr,
			"[sse] warden command undelivered: warden=%s verb=%s target=%s — %s\n",
			wardenID, verb, digest.MemberID, action)
	}
	if len(back) > 0 {
		h.wardenCmds[wardenID] = append(back, h.wardenCmds[wardenID]...)
	}
	h.mu.Unlock()
	runCommandPersists(writes)
	return requeued, dropped
}

// UndeliveredCommandSince reports the loss note for memberID iff it is NEWER
// than since (the caller's own dispatch anchor) — an older note describes some
// previous attempt and must never be used to explain this one.
func (h *Hub) UndeliveredCommandSince(memberID string, since float64) (undeliveredCommand, bool) {
	if memberID == "" {
		return undeliveredCommand{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	note, ok := h.cmdUndelivered[memberID]
	if !ok || note.At < since {
		return undeliveredCommand{}, false
	}
	return note, true
}

// containsFrame reports whether an identical frame is already queued — the
// requeue de-dup. Warden command frames for the same verb+target are byte
// identical (update carries no token and no timestamp), so a warden that flaps
// repeatedly accumulates ONE pending update, not one per flap.
//
// Ignoring Subject is safe rather than sloppy: the subject IS the frame's
// args.member_id (enqueueToWarden passes the same id it builds the frame with),
// so byte-identical frames necessarily carry the same subject and collapsing
// them cannot merge two different members' orders. The only way to break that
// is an untagged EnqueueWardenCommand (subject ""), which has no production
// caller — it is reached from tests only. Anyone adding a real untagged caller
// must compare Subject here too.
func containsFrame(queue []wardenCmd, frame []byte) bool {
	for _, q := range queue {
		if bytes.Equal(q.Frame, frame) {
			return true
		}
	}
	return false
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
