package main

// hub.go — the SSE hub: online/machine projection plus delta fan-out (spec/sse.md).
// Online is purely this connection projection, never a stored flag
// (docs/design/state-model.md).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"ocserverd/txguard"
)

var errDualSSE = errors.New("member already holds a live SSE connection")

// Burst=3: the zombie-slot takeover needs exactly 1. Window=60s is deliberately
// shorter than the client's sseRefusalGrace (120s, cli/ocagent/listen.go), so a
// single legitimate client can never accumulate 120s of 409s and self-terminate.
const (
	takeoverBurst  = 3
	takeoverWindow = 60 * time.Second
)

// Wording is not contract (spec/sse.md §5.1); the 409 status and pre-stream timing are.
var errDualSSEThrottled = errors.New(
	"member already holds a live SSE connection (takeover throttled: too many handovers; dual live clients suspected)")

const triggerServer = "server"

// hubListener is one open SSE connection. MemberID is "" for the owner
// (dashboard) connection.
type hubListener struct {
	MemberID  string
	MachineID string

	Gen int64

	kicked chan struct{}

	attachedAt time.Time

	mu  txguard.Mutex
	buf [][]byte
}

func (l *hubListener) push(frame []byte) {
	l.mu.Lock()
	l.buf = append(l.buf, frame)
	l.mu.Unlock()
}

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

type Hub struct {
	mu        txguard.Mutex
	listeners map[*hubListener]bool
	// seq serves both seq and epoch (spec/sse.md §2.1). It resets on restart by
	// design — clients are contracted to full-resync.
	seq int64

	wardenCmds map[string][]wardenCmd

	cmdUndelivered map[string]undeliveredCommand
	// cmdStore is nil when no DAL is bound (the route-shape harness).
	cmdStore wardenCommandStore

	connGen int64

	kicks map[string][]time.Time

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

// Connect: a member already holding a live listener is TAKEN OVER (spec/sse.md
// §5.1) in one critical section, so the member never drops offline. The caller
// maps errDualSSEThrottled to a 409 BEFORE the stream starts.
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
			delete(h.listeners, old)
			close(old.kicked)
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

// Disconnect's lastForMember gates the §5.2 last-disconnect edge hooks. A kicked
// listener reports false: the takeover already removed it from the map.
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

// sseTopics is the single source of the closed topic vocabulary: bin/gen-sse-topics
// renders it into spec/sse-topics.json (drift-sse-topics gate), which the
// conformance suite reads. A change here also means updating spec/sse.md §3.1's
// hand-written table.
//
// ⚠️ Publish drops an unknown topic SILENTLY (a restore once published "role"
// instead of "role_def": 200 on the wire, nothing fanned). Every switch mapping a
// document kind to a topic must learn a new topic too; Go will not tell you.
var sseTopics = map[string]bool{
	"member":         true,
	"chat":           true,
	"chat_read":      true,
	"reply_card":     true,
	"task":           true,
	"task_manual":    true,
	"global_context": true,
	"role_def":       true,
	"insight":        true,
	"context":        true,
	"monitoring":     true,
}

// jsonFloat: the frame ts is contractually a float; a bare integer literal would
// json-parse as int.
type jsonFloat float64

func (f jsonFloat) MarshalJSON() ([]byte, error) {
	s := strconv.FormatFloat(float64(f), 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return []byte(s), nil
}

type sseFrameData struct {
	Entity  string `json:"entity"`
	Key     string `json:"key"`
	Epoch   int64  `json:"epoch"`
	Deleted bool   `json:"deleted"`
	Payload any    `json:"payload"`
}

// sseFrame.Trigger is attribution only (spec/sse.md §2.3): it never changes
// fan-out; the ocagent listener uses it to drop its own echoes (trigger == self).
type sseFrame struct {
	Seq     int64        `json:"seq"`
	Topic   string       `json:"topic"`
	Op      string       `json:"op"`
	Data    sseFrameData `json:"data"`
	Ts      jsonFloat    `json:"ts"`
	Trigger string       `json:"trigger"`
}

type Audience struct {
	All     bool
	Members map[string]bool
}

func audienceAll() Audience { return Audience{All: true} }

func audienceOwnerOnly() Audience { return Audience{} }

func audienceMembers(ids ...string) Audience {
	m := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			m[id] = true
		}
	}
	return Audience{Members: m}
}

// Publish: a filtered agent connection sees a gapped subsequence of seq — expected,
// there is no replay and clients full-resync on reconnect (spec/sse.md §2.1).
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
		return
	}
	text := []byte("id: " + strconv.FormatInt(seq, 10) + "\ndata: " + string(raw) + "\n\n")
	for l := range h.listeners {
		if l.MemberID == "" || aud.All || aud.Members[l.MemberID] {
			l.push(text)
		}
	}
}

// PushDirected is best-effort at-most-once: no live connection → dropped.
// 🔴 No production caller today (the directed bands write on their own
// connection; warden commands use the FIFO). The task-close nudge left it for a
// durable chat row: dropping is only fine when the frame's subject stays true
// while the recipient is offline — ask that before adding a caller.
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

// ── the durable half of the FIFO ─────────────────────────────────────────────
//
// Only `update` is persisted. START / STOP / UNINSTALL are re-derived by the
// reconcile producer from observed presence within one 30s cadence; a replayed
// STOP is worse than a lost one, and a START frame carries a live member_token
// that must not be written at rest. Nothing re-derives `update` (the owner's
// upgrade click), and POST /api/update/upgrade re-execs the server, so "restart
// between enqueue and drain" is designed-in.
//
// There is no ack: a row is cleared on a successful socket WRITE, which is not
// delivery. A replay after a crash is harmless because `update` kicks the
// warden's content-hash self-update, a no-op when the bytes already match.

type wardenCommandStore interface {
	PutWardenCommand(WardenCommand) error
	DeleteWardenCommand(wardenID, verb, memberID string) error
	ListWardenCommands() ([]WardenCommand, error)
	DeleteWardenCommandsBefore(cutoff float64) (int64, error)
}

// wardenCommandTTL is a staleness cap, not a size cap (one row per
// warden+verb+target): the warden's own 15-minute self-update poll has had ~96
// chances to converge by then.
const wardenCommandTTL = 24 * time.Hour

func persistableCommandVerb(verb string) bool {
	return verb == reconcileCmdUpdate
}

// commandStoreWrite is planned under h.mu and executed after release. h.mu also
// guards Publish and Connect, and the store is SQLite on one pooled connection
// with a 5s busy timeout — persisting inside the lock measured a 4.9s
// server-wide Publish stall. Never hold h.mu across store I/O.
type commandStoreWrite struct {
	store  wardenCommandStore
	cmd    WardenCommand
	digest wardenCommandDigest
}

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

// runCommandPersists: a store failure never fails the dispatch (the in-memory
// FIFO already has the frame); only the restart insurance is lost.
//
// KNOWN RACE, not fixable by moving this back under h.mu: a persist and a
// same-key clearing delete can interleave, so a second upgrade click can lose its
// durable row (reproduced with both calls inside h.mu). Closing it needs
// per-command ack (deferred). Kept synchronous for the postcondition "the row
// exists when Enqueue returns" — the guard tests depend on it.
func runCommandPersists(writes []commandStoreWrite) {
	for _, w := range writes {
		if err := w.store.PutWardenCommand(w.cmd); err != nil {
			noteCommandStoreFailure("persist", w.cmd.WardenID, w.digest, err)
		}
	}
}

// noteCommandStoreFailure logs to stderr because the HTTP/MCP surface is frozen;
// the command is still enqueued, only its restart insurance was lost.
func noteCommandStoreFailure(op, wardenID string, digest wardenCommandDigest, err error) {
	fmt.Fprintf(os.Stderr,
		"[sse] warden command queue %s FAILED: warden=%s verb=%s target=%s — %v "+
			"(the command is still queued in memory and will be delivered if this "+
			"process survives; it will NOT survive a restart)\n",
		op, wardenID, digest.Verb, digest.MemberID, err)
}

// BindWardenCommandStore rehydrates the FIFO; called once at assembly
// (newAPIServer) so the queue is whole before any warden connects.
// Known limitations: restore skips the online-warden gate (a command for a
// machine deleted while down is inert until the TTL sweep), and two apiServers
// over one live DAL would each restore the same rows.
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
	// ListWardenCommands returns enqueue order; the rebuilt queue must stay FIFO
	// (spec/sse.md §7).
	for _, c := range pending {
		if c.WardenID == "" || len(c.Frame) == 0 || containsFrame(h.wardenCmds[c.WardenID], c.Frame) {
			continue
		}
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
		noteCommandStoreFailure("clear", wardenID, digest, err)
	}
}

type wardenCmd struct {
	Subject string
	Frame   []byte
}

// EnqueueWardenCommand: drained ONLY by the connection whose verified token sub
// is wardenID, never the owner fan-out (a riding member_token is a secret).
func (h *Hub) EnqueueWardenCommand(wardenID string, frame []byte) {
	h.EnqueueWardenCommandFor(wardenID, "", frame)
}

func (h *Hub) EnqueueWardenCommandFor(wardenID, subject string, frame []byte) {
	if wardenID == "" {
		return
	}
	var writes []commandStoreWrite
	h.mu.Lock()
	h.wardenCmds[wardenID] = append(h.wardenCmds[wardenID],
		wardenCmd{Subject: subject, Frame: frame})
	if digest, ok := decodeWardenCommandFrame(frame); ok && digest.MemberID != "" {
		delete(h.cmdUndelivered, digest.MemberID)
		if w, queued := h.planCommandPersistLocked(wardenID, digest, frame); queued {
			writes = append(writes, w)
		}
	}
	h.mu.Unlock()
	runCommandPersists(writes)
}

func (h *Hub) PendingWardenCommands(wardenID string) int {
	if wardenID == "" {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.wardenCmds[wardenID])
}

// PendingWardenCommandsFor exists because one machine's FIFO is shared by every
// member and worker on it: queue depth never answers "is THIS worker's frame
// still waiting", and reading it that way accused a healthy, draining warden.
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

// DrainWardenCommands: the caller must hand unwritten frames to
// ReturnUndeliveredCommands. Durable rows are NOT deleted here — only
// MarkWardenCommandWritten clears them, so a process dying mid-drain keeps them.
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

type undeliveredCommand struct {
	Verb     string
	Warden   string
	At       float64
	Requeued bool
}

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
			verb = "unknown"
		}
		retry := verb == reconcileCmdUpdate &&
			!containsFrame(h.wardenCmds[wardenID], frame) && !containsFrame(back, frame)
		if retry {
			back = append(back, cmd)
			requeued++
			// A no-op under the store's conflict rule unless the original persist failed.
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
			// A requeue-path note is never cleared when the retry succeeds (that
			// needs the deferred ack). Harmless today because the consumers only act
			// on verb == start and only `update` is requeued; a new consumer must not
			// read a requeued note as "still lost".
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

// containsFrame may ignore Subject only because the subject IS the frame's
// args.member_id; frames for one verb+target are byte-identical (update carries
// no token or timestamp). A real untagged caller would need Subject compared too.
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
// Volatile by design (lifecycle.md §3: restart amnesia is contract), keyed on
// the verified token sub.

type memStore struct {
	mu      txguard.Mutex
	entries map[string]map[string]any
}

func newMemStore() *memStore {
	return &memStore{entries: map[string]map[string]any{}}
}

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
