package main

// warden_command_persistence_test.go — T-66a2 L3: the warden command FIFO was
// process-local memory, so a server restart emptied it.
//
// For START / STOP / UNINSTALL that is harmless by contract (reconcile
// re-derives all three from observed presence within one cadence). For
// `update` it is not: nothing anywhere re-derives an owner's upgrade click —
// and `POST /api/update/upgrade` deliberately re-execs the server, so the
// restart that loses the command is CAUSED BY the command. These tests pin the
// restart survival, the deliberate non-persistence of everything else, the
// clearing on write, the staleness bound, and the visibility of a store
// failure.
//
// "Restart" here means: assemble a SECOND apiServer over the SAME store, which
// is exactly what a re-exec does. No process is started or stopped.

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── helpers ─────────────────────────────────────────────────────────────────

// persistTestDAL opens one temp sqlite store that several "server lifetimes"
// share — the durable thing a restart is supposed to find again.
func persistTestDAL(t *testing.T) *DAL {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "warden-cmd-queue.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return NewDAL(db)
}

// bootServer assembles a fresh apiServer over dal — one server lifetime.
// Calling it twice is the restart.
func bootServer(t *testing.T, dal *DAL) *apiServer {
	t.Helper()
	return newAPIServer(dal, NewHub(), []byte(interopSecret), 3600, "../..")
}

// alwaysWrites is a ResponseWriter whose writes never fail — the healthy
// connection the sentinel needs (failAfterWrites, from the delivery test, is
// the dying one).
func alwaysWrites() *failAfterWrites { return newFailAfterWrites(1 << 20) }

// deliverPending drives the REAL SSE handler as wardenID until `want` command
// frames have reached the socket, then returns everything written. The
// predicate counts WRITES rather than probing the FIFO — a probe would drain it
// out from under the handler.
func deliverPending(t *testing.T, api *apiServer, wardenID string, want int) [][]byte {
	t.Helper()
	w := alwaysWrites()
	runWardenStream(t, api, wardenID, w, func() bool {
		return len(w.written()) >= want+1 // +1: the handler's ": connected" preamble
	})
	return w.written()
}

// containsWrite reports whether one of the raw socket writes IS this frame.
// Distinct from hub.containsFrame, which searches the QUEUE — since T-e0e3 the
// queue holds wardenCmd{Subject, Frame}, while what the stream loop wrote is
// bare wire text with no subject attached.
func containsWrite(writes [][]byte, frame []byte) bool {
	for _, w := range writes {
		if bytes.Equal(w, frame) {
			return true
		}
	}
	return false
}

// assertNotStoredAnywhere fails if `secret` appears in ANY column of ANY row of
// ANY table in the database. This is the canary behind "a member_token must
// never be written at rest" — the reason START is excluded from the durable
// set. Checking only the queue table would be circular (that table is asserted
// empty two lines earlier); the claim is about the whole store.
func assertNotStoredAnywhere(t *testing.T, dal *DAL, secret string) {
	t.Helper()
	if where, found := findStoredSecret(t, dal, secret); found {
		t.Fatalf("secret found at rest in %s", where)
	}
}

// findStoredSecret is the scan itself, split out so the canary can be proved
// capable of firing (TestWardenCommandQueue_SecretScanCanaryCanFire) instead of
// being trusted because it is green.
func findStoredSecret(t *testing.T, dal *DAL, secret string) (string, bool) {
	t.Helper()
	tables, err := dal.rdb.Query(
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, n)
	}
	tables.Close()
	if len(names) == 0 {
		t.Fatal("the schema is empty — this canary would pass vacuously")
	}
	scanned := 0
	for _, table := range names {
		rows, err := dal.rdb.Query(`SELECT * FROM "` + table + `"`)
		if err != nil {
			t.Fatalf("select from %s: %v", table, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatalf("columns of %s: %v", table, err)
		}
		for rows.Next() {
			cells := make([]any, len(cols))
			into := make([]any, len(cols))
			for i := range cells {
				into[i] = &cells[i]
			}
			if err := rows.Scan(into...); err != nil {
				rows.Close()
				t.Fatalf("scan %s: %v", table, err)
			}
			for i, cell := range cells {
				scanned++
				if strings.Contains(fmt.Sprintf("%v", cell), secret) {
					rows.Close()
					return table + "." + cols[i], true
				}
			}
		}
		rows.Close()
	}
	if scanned == 0 {
		t.Fatal("no cell was inspected — this canary would pass vacuously")
	}
	return "", false
}

// countQueued reports how many rows the durable queue holds.
func countQueued(t *testing.T, dal *DAL) int {
	t.Helper()
	rows, err := dal.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	return len(rows)
}

// ── the guard: an update outlives the process that accepted it ──────────────

// TestWardenCommandQueue_UpdateSurvivesServerRestart is THE regression guard.
// Enqueue an upgrade, never drain it, restart, and it must still be there AND
// still go out on the wire.
//
// MUTANT: delete the planCommandPersistLocked/runCommandPersists pair from
// EnqueueWardenCommand (hub.go) → the second lifetime's FIFO is empty → RED.
func TestWardenCommandQueue_UpdateSurvivesServerRestart(t *testing.T) {
	dal := persistTestDAL(t)
	putGateMember(t, dal, Member{ID: "mach-a", Kind: KindWarden,
		DesiredState: DesiredStateOnline})

	// ── lifetime 1: the owner clicks upgrade, the warden never drains it.
	first := bootServer(t, dal)
	first.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdUpdate, "mach-a"))
	if n := countQueued(t, dal); n != 1 {
		t.Fatalf("the update must be recorded durably, got %d row(s)", n)
	}

	// ── the restart (an upgrade re-execs the server — this IS the race).
	second := bootServer(t, dal)

	// The restored frame must stay ATTRIBUTABLE to the member it acts on: one
	// machine's queue is shared by everybody placed there, so an untagged
	// rehydration makes the per-subject read blind to exactly the frame the
	// restart preserved. Read BEFORE the drain below — the drain pops, so a
	// read placed after it measures an empty queue and asserts nothing.
	//
	// MUTANT: drop the `Subject: c.MemberID` tag in BindWardenCommandStore
	// (hub.go) → the per-subject read returns 0 → RED.
	if got := second.hub.PendingWardenCommandsFor("mach-a", "mach-a"); got != 1 {
		t.Fatalf("the restored frame must still be attributable to its subject, got %d", got)
	}

	pending := second.hub.DrainWardenCommands("mach-a")
	if len(pending) != 1 {
		t.Fatalf("the pending update must survive the restart, got %d frame(s)", len(pending))
	}
	digest, ok := decodeWardenCommandFrame(pending[0].Frame)
	if !ok || digest.Verb != reconcileCmdUpdate || digest.MemberID != "mach-a" {
		t.Fatalf("the restored frame must be the update for mach-a, got %+v (ok=%v)", digest, ok)
	}

	// Surviving in a map is not the point — it has to reach the warden. Drive
	// the real handler on a THIRD lifetime (the drain above consumed the
	// second's copy) and watch the frame go out.
	third := bootServer(t, dal)
	delivered := deliverPending(t, third, "mach-a", 1)
	if !containsWrite(delivered, pending[0].Frame) {
		t.Fatalf("the restored update must be written to the warden, got %d write(s)", len(delivered))
	}
	if n := countQueued(t, dal); n != 0 {
		t.Fatalf("a written command must be cleared, got %d row(s) left", n)
	}
}

// ── the sentinel: the ordinary path is untouched and never doubles up ────────

// TestWardenCommandQueue_DeliveredUpdateIsNotResentAfterRestart pins the happy
// path: enqueue → drain → write → the durable row is gone, so a later restart
// does NOT replay it. (Persistence that never forgets would turn every restart
// into a fleet-wide upgrade storm.)
func TestWardenCommandQueue_DeliveredUpdateIsNotResentAfterRestart(t *testing.T) {
	dal := persistTestDAL(t)
	putGateMember(t, dal, Member{ID: "mach-a", Kind: KindWarden,
		DesiredState: DesiredStateOnline})

	api := bootServer(t, dal)
	frame := cmdFrame(t, reconcileCmdUpdate, "mach-a")
	api.hub.EnqueueWardenCommand("mach-a", frame)

	if delivered := deliverPending(t, api, "mach-a", 1); !containsWrite(delivered, frame) {
		t.Fatal("the update must reach the warden on the ordinary path")
	}
	if n := countQueued(t, dal); n != 0 {
		t.Fatalf("a written command must leave no durable row, got %d", n)
	}

	restarted := bootServer(t, dal)
	if got := restarted.hub.DrainWardenCommands("mach-a"); len(got) != 0 {
		t.Fatalf("an already-written update must NOT be replayed after a restart, got %d frame(s)", len(got))
	}
}

// TestWardenCommandQueue_ReDecidableVerbsStayVolatile pins the deliberate
// scope: START / STOP / UNINSTALL are re-derived from presence by the reconcile
// producer, so persisting them would add a second, staler source of truth — and
// a START frame carries a live member_token that must never be written at rest.
func TestWardenCommandQueue_ReDecidableVerbsStayVolatile(t *testing.T) {
	dal := persistTestDAL(t)
	// Real roster rows so the whole-database scan below has cells to inspect —
	// a canary over an empty schema proves nothing.
	putGateMember(t, dal, Member{ID: "mach-a", Kind: KindWarden})
	putGateMember(t, dal, Member{ID: "m-1", Kind: KindAssistant})
	api := bootServer(t, dal)

	start, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC:  reconcileCmdStart,
		Args: wardenStartArgs{MemberID: "m-1", MemberToken: "s3cret-token"},
	})
	if err != nil {
		t.Fatalf("build start frame: %v", err)
	}
	api.hub.EnqueueWardenCommand("mach-a", start)
	api.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdStop, "m-1"))
	api.hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdUninstall, "m-2"))

	rows, err := dal.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("no re-decidable verb may be stored, got %d row(s): %+v", len(rows), rows)
	}
	// The load-bearing half: "a member_token never lands at rest" is the reason
	// START is excluded from the durable set, so it must be checked against the
	// WHOLE database, not against the queue table we just asserted is empty
	// (scanning an empty slice proves nothing — it is the assertion that isn't
	// there). Sweep every column of every row of every table.
	assertNotStoredAnywhere(t, dal, "s3cret-token")

	restarted := bootServer(t, dal)
	if got := restarted.hub.DrainWardenCommands("mach-a"); len(got) != 0 {
		t.Fatalf("volatile verbs must not survive a restart, got %d frame(s)", len(got))
	}
}

// TestWardenCommandQueue_StaleCommandsExpire pins the growth bound's time half:
// a command nobody could ever drain must not be replayed forever. (The size
// half is structural — the natural key allows one pending row per
// warden+verb+target, so the queue is bounded by the roster.)
func TestWardenCommandQueue_StaleCommandsExpire(t *testing.T) {
	dal := persistTestDAL(t)
	now := float64(time.Now().UnixNano()) / 1e9
	fresh := cmdFrame(t, reconcileCmdUpdate, "mach-fresh")
	if err := dal.PutWardenCommand(WardenCommand{
		WardenID: "mach-stale", Verb: reconcileCmdUpdate, MemberID: "mach-stale",
		Frame: cmdFrame(t, reconcileCmdUpdate, "mach-stale"),
		// One second past the TTL — the boundary, not a decade ago.
		EnqueuedTS: now - wardenCommandTTL.Seconds() - 1,
	}); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	if err := dal.PutWardenCommand(WardenCommand{
		WardenID: "mach-fresh", Verb: reconcileCmdUpdate, MemberID: "mach-fresh",
		Frame: fresh, EnqueuedTS: now,
	}); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}

	api := bootServer(t, dal)
	if got := api.hub.DrainWardenCommands("mach-stale"); len(got) != 0 {
		t.Fatalf("a command older than the TTL must not be restored, got %d frame(s)", len(got))
	}
	if got := api.hub.DrainWardenCommands("mach-fresh"); len(got) != 1 {
		t.Fatalf("a fresh command must still be restored, got %d frame(s)", len(got))
	}
	if n := countQueued(t, dal); n != 1 {
		t.Fatalf("the expired row must be swept from the store, got %d row(s)", n)
	}
}

// TestWardenCommandQueue_ReEnqueueDoesNotRefreshTheExpiryAnchor: a command that
// keeps failing to reach a flapping warden gets requeued repeatedly. If each
// requeue reset enqueued_ts, the TTL above would never fire for exactly the
// commands it exists to bound.
func TestWardenCommandQueue_ReEnqueueDoesNotRefreshTheExpiryAnchor(t *testing.T) {
	dal := persistTestDAL(t)
	old := float64(time.Now().UnixNano())/1e9 - 3600
	frame := cmdFrame(t, reconcileCmdUpdate, "mach-a")
	if err := dal.PutWardenCommand(WardenCommand{
		WardenID: "mach-a", Verb: reconcileCmdUpdate, MemberID: "mach-a",
		Frame: frame, EnqueuedTS: old,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	api := bootServer(t, dal)
	api.hub.EnqueueWardenCommand("mach-a", frame)

	rows, err := dal.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the same order must stay ONE row, got %d", len(rows))
	}
	if rows[0].EnqueuedTS != old {
		t.Fatalf("enqueued_ts must stay the original anchor, got %v want %v",
			rows[0].EnqueuedTS, old)
	}
}

// ── "is the failure visible from outside?" ──────────────────────────────────

// failingCommandStore fails every write, succeeds at listing nothing.
type failingCommandStore struct{ err error }

func (f failingCommandStore) PutWardenCommand(WardenCommand) error { return f.err }
func (f failingCommandStore) DeleteWardenCommand(string, string, string) error {
	return f.err
}
func (f failingCommandStore) ListWardenCommands() ([]WardenCommand, error) { return nil, nil }
func (f failingCommandStore) DeleteWardenCommandsBefore(float64) (int64, error) {
	return 0, nil
}

// TestWardenCommandQueue_PersistFailureIsLoud: the durable write is best-effort
// (the in-memory FIFO has already accepted the command, so failing the dispatch
// would be a lie — it WILL be delivered if the process survives). What must
// never happen is the pre-T-66a2 pattern of a delivery guarantee quietly not
// being there: the loss of restart insurance gets its own named stderr line,
// on the same channel as the undelivered accounting, naming warden/verb/target
// and never the frame body.
func TestWardenCommandQueue_PersistFailureIsLoud(t *testing.T) {
	hub := NewHub()
	hub.BindWardenCommandStore(failingCommandStore{err: errors.New("disk on fire")})

	logs := captureStderr(t, func() {
		hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdUpdate, "mach-a"))
	})

	for _, want := range []string{"queue persist FAILED", "mach-a", reconcileCmdUpdate, "disk on fire"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("a failed durable write must be named on stderr (missing %q):\n%s", want, logs)
		}
	}
	// And the command is still live in this process — the failure costs the
	// restart insurance, not the dispatch.
	if got := hub.DrainWardenCommands("mach-a"); len(got) != 1 {
		t.Fatalf("a store failure must not drop the in-memory command, got %d frame(s)", len(got))
	}
}

// ── the canary is capable of firing ─────────────────────────────────────────

// TestWardenCommandQueue_SecretScanCanaryCanFire proves findStoredSecret is not
// green-by-construction: deliberately park a secret in the store and the scan
// must find it, naming the exact column. Without this, "no member_token at
// rest" would rest on a scan nobody has ever seen fail.
func TestWardenCommandQueue_SecretScanCanaryCanFire(t *testing.T) {
	dal := persistTestDAL(t)
	if err := dal.PutSetting("t66a2.canary", "s3cret-token"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	where, found := findStoredSecret(t, dal, "s3cret-token")
	if !found {
		t.Fatal("the whole-database scan failed to find a secret that IS at rest")
	}
	if !strings.HasPrefix(where, "setting.") {
		t.Fatalf("the scan must name the column it found, got %q", where)
	}
}

// ── FIFO order across the restart ───────────────────────────────────────────

// TestWardenCommandQueue_RestoreKeepsFIFOOrder: spec/sse.md §7 requires the
// drain to pop pending frames IN FIFO ORDER. Every other test here queues a
// single command, so that requirement was written down and guarded by nothing —
// reversing ListWardenCommands' ORDER BY left the suite fully green.
//
// MUTANT: reverse the ORDER BY in DAL.ListWardenCommands (or append restored
// frames in reverse in BindWardenCommandStore) → RED.
func TestWardenCommandQueue_RestoreKeepsFIFOOrder(t *testing.T) {
	dal := persistTestDAL(t)
	now := float64(time.Now().UnixNano()) / 1e9
	// Same warden, three targets, enqueued in a known order. (Today's `update`
	// always addresses the warden itself, so this is the generic queue shape
	// rather than a live dispatch pattern — the ordering contract is per-warden
	// and must not depend on that coincidence holding forever.)
	order := []string{"first", "second", "third"}
	for i, target := range order {
		if err := dal.PutWardenCommand(WardenCommand{
			WardenID: "mach-a", Verb: reconcileCmdUpdate, MemberID: target,
			Frame: cmdFrame(t, reconcileCmdUpdate, target), EnqueuedTS: now + float64(i),
		}); err != nil {
			t.Fatalf("seed %s: %v", target, err)
		}
	}

	// The DAL contract itself.
	rows, err := dal.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	if len(rows) != len(order) {
		t.Fatalf("expected %d rows, got %d", len(order), len(rows))
	}
	for i, want := range order {
		if rows[i].MemberID != want {
			t.Fatalf("ListWardenCommands must return enqueue order: position %d is %q, want %q",
				i, rows[i].MemberID, want)
		}
	}

	// And the rebuilt FIFO the drain actually pops.
	api := bootServer(t, dal)
	pending := api.hub.DrainWardenCommands("mach-a")
	if len(pending) != len(order) {
		t.Fatalf("expected %d restored frames, got %d", len(order), len(pending))
	}
	for i, want := range order {
		digest, ok := decodeWardenCommandFrame(pending[i].Frame)
		if !ok || digest.MemberID != want {
			t.Fatalf("the restored FIFO must keep enqueue order: position %d is %+v, want %q",
				i, digest, want)
		}
	}
}

// ── binding twice must not duplicate an order ───────────────────────────────

// TestWardenCommandQueue_BindingTwiceDoesNotDuplicate pins the claim made in
// BindWardenCommandStore's own comment. Nothing in production binds twice
// today, but the de-dup is the only thing standing between a future second bind
// and a doubled upgrade order.
//
// MUTANT: drop the containsFrame check in BindWardenCommandStore → RED.
func TestWardenCommandQueue_BindingTwiceDoesNotDuplicate(t *testing.T) {
	dal := persistTestDAL(t)
	if err := dal.PutWardenCommand(WardenCommand{
		WardenID: "mach-a", Verb: reconcileCmdUpdate, MemberID: "mach-a",
		Frame:      cmdFrame(t, reconcileCmdUpdate, "mach-a"),
		EnqueuedTS: float64(time.Now().UnixNano()) / 1e9,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	hub := NewHub()
	hub.BindWardenCommandStore(dal)
	hub.BindWardenCommandStore(dal)

	if got := hub.DrainWardenCommands("mach-a"); len(got) != 1 {
		t.Fatalf("binding twice must not duplicate the order, got %d frame(s)", len(got))
	}
}

// ── the requeue's second chance at durability ───────────────────────────────

// TestWardenCommandQueue_RequeueRetriesAFailedPersist: when the enqueue-time
// durable write fails, the frame is live but uninsured. The stream loop handing
// it back (connection died mid-drain) proves it is still wanted, so that path
// re-attempts the write. Nothing covered this — the whole retry could be
// deleted and the suite stayed green.
//
// MUTANT: delete the planCommandPersistLocked block in
// ReturnUndeliveredCommands (hub.go) → RED.
func TestWardenCommandQueue_RequeueRetriesAFailedPersist(t *testing.T) {
	store := newRecordingCommandStore()
	store.failPuts(1) // the enqueue-time write fails; later writes succeed
	hub := NewHub()
	hub.BindWardenCommandStore(store)

	frame := cmdFrame(t, reconcileCmdUpdate, "mach-a")
	captureStderr(t, func() { hub.EnqueueWardenCommand("mach-a", frame) })
	if n := store.count(); n != 0 {
		t.Fatalf("the failed persist must not have stored anything, got %d row(s)", n)
	}

	// The connection dies mid-drain and the frames come back.
	pending := hub.DrainWardenCommands("mach-a")
	captureStderr(t, func() { hub.ReturnUndeliveredCommands("mach-a", pending) })

	if n := store.count(); n != 1 {
		t.Fatalf("the requeue must retry the durable write, got %d row(s)", n)
	}
}

// ── the hub must never block on the store ───────────────────────────────────

// TestWardenCommandQueue_StoreIODoesNotBlockThePublishPath is the regression
// guard for the review finding: the first cut of this work did its durable
// writes INSIDE h.mu, the one lock Publish (the exit of every write handler)
// and Connect (the entry of every SSE connection) also take. With SQLite on a
// single pooled connection and a 5s busy timeout, one contended write stalled
// the whole server for 4.9s — a coupling the hub did not have before T-66a2.
//
// MUTANT: move the PutWardenCommand call back inside the critical section (or
// hold h.mu across runCommandPersists) → the measured stall jumps to the full
// store delay → RED.
func TestWardenCommandQueue_StoreIODoesNotBlockThePublishPath(t *testing.T) {
	const stall = 2 * time.Second
	store := newRecordingCommandStore()
	store.putDelay(stall)
	hub := NewHub()
	hub.BindWardenCommandStore(store)

	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.EnqueueWardenCommand("mach-a", cmdFrame(t, reconcileCmdUpdate, "mach-a"))
	}()
	// Wait until the store write is genuinely in progress, so the measurement
	// below cannot pass by simply racing ahead of it.
	select {
	case <-store.putStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the durable write never started")
	}

	start := time.Now()
	hub.Publish("member", "patch", "member", "owner::probe-1", nil, audienceAll(), "")
	blocked := time.Since(start)
	t.Logf("Publish blocked %s while a %s store write was in flight", blocked, stall)

	// Generous relative to the 4.9s the broken version produced, tight enough
	// that any lock-held store call fails it.
	if blocked > stall/4 {
		t.Fatalf("Publish must not wait on the durable queue: blocked %s during a %s store write",
			blocked, stall)
	}
	// Connect is the other side of the same lock.
	start = time.Now()
	if _, err := hub.Connect("probe-2", ""); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if blocked = time.Since(start); blocked > stall/4 {
		t.Fatalf("Connect must not wait on the durable queue: blocked %s", blocked)
	}
	<-done
}

// ── a store fake that can be slow, can fail, and remembers ──────────────────

// recordingCommandStore is an in-memory wardenCommandStore with the two knobs
// the tests above need: a per-Put delay (the slow-store probe) and a countdown
// of Puts that must fail (the second-chance probe).
type recordingCommandStore struct {
	mu         sync.Mutex
	rows       map[string]WardenCommand
	fails      int
	delay      time.Duration
	putStarted chan struct{}
	signalled  bool
}

func newRecordingCommandStore() *recordingCommandStore {
	return &recordingCommandStore{
		rows:       map[string]WardenCommand{},
		putStarted: make(chan struct{}),
	}
}

func (s *recordingCommandStore) failPuts(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails = n
}

func (s *recordingCommandStore) putDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

func (s *recordingCommandStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func commandKey(warden, verb, member string) string {
	return warden + "|" + verb + "|" + member
}

func (s *recordingCommandStore) PutWardenCommand(c WardenCommand) error {
	s.mu.Lock()
	delay, fail := s.delay, s.fails > 0
	if fail {
		s.fails--
	}
	if !s.signalled {
		s.signalled = true
		close(s.putStarted)
	}
	s.mu.Unlock()

	time.Sleep(delay) // OUTSIDE the fake's own lock: this models slow I/O
	if fail {
		return errors.New("store unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := commandKey(c.WardenID, c.Verb, c.MemberID)
	if _, exists := s.rows[key]; !exists { // mirrors ON CONFLICT DO NOTHING
		s.rows[key] = c
	}
	return nil
}

func (s *recordingCommandStore) DeleteWardenCommand(warden, verb, member string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, commandKey(warden, verb, member))
	return nil
}

func (s *recordingCommandStore) ListWardenCommands() ([]WardenCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WardenCommand, 0, len(s.rows))
	for _, c := range s.rows {
		out = append(out, c)
	}
	return out, nil
}

func (s *recordingCommandStore) DeleteWardenCommandsBefore(cutoff float64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for k, c := range s.rows {
		if c.EnqueuedTS < cutoff {
			delete(s.rows, k)
			n++
		}
	}
	return n, nil
}
