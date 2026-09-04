package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// T-48: the listener files the read receipt itself, because GET /api/chat no
// longer does it as a side effect. Three things must hold: nothing is marked
// before the line is printed, one batch marks EACH sender to its own
// watermark, and both drain entrances (cold-start backfill, live delta after a
// reconnect) file receipts.
// ---------------------------------------------------------------------------

type markCall struct {
	Peer       string  `json:"peer"`
	LastReadTS float64 `json:"last_read_ts"`
}

// markReadServer serves a swappable /api/chat list and RECORDS every
// POST /api/chat/mark-read body. `status` is what the mark-read route answers.
type markReadServer struct {
	*httptest.Server
	mu    sync.Mutex
	list  string
	calls []markCall
	conns int32 // /api/events dials, read atomically
	// bodies are the mark-read request bodies EXACTLY as they came off the
	// wire. calls is the decoded convenience view; the wire test needs the raw
	// text, because an undeclared key is invisible once it has been decoded
	// into markCall's fixed fields.
	bodies []string
	status int
	// beforeMark, if set, runs on the mark-read route before the call is
	// recorded — the hook the "print first" guardrail uses to snapshot what the
	// session had already been told at the moment the receipt was filed.
	beforeMark func()
	// chatAnswer, if set, OWNS the /api/chat route: it is handed the query and
	// the 1-based request number and returns (status, verbatim body). That is
	// what lets a test serve a real multi-page unread walk — and the pathological
	// walks that must not spin the client forever. Nil ⇒ `list` in one page.
	chatAnswer func(q url.Values, nth int) (int, string)
	// queries records every /api/chat request line, so a test can assert WHAT
	// was asked for and not only what came back.
	queries []url.Values
}

func newMarkReadServer(t *testing.T, list string) *markReadServer {
	t.Helper()
	m := &markReadServer{list: list, status: 200}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == markReadPath {
			m.mu.Lock()
			hook := m.beforeMark
			st := m.status
			m.mu.Unlock()
			if hook != nil {
				hook()
			}
			var c markCall
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Errorf("mark-read body is not JSON: %v (%s)", err, raw)
			}
			m.mu.Lock()
			m.calls = append(m.calls, c)
			m.bodies = append(m.bodies, string(raw))
			m.mu.Unlock()
			w.WriteHeader(st)
			_, _ = w.Write([]byte(`{"reader_id":"kyle","peer_id":"` + c.Peer + `","last_read_ts":0}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			m.mu.Lock()
			m.queries = append(m.queries, r.URL.Query())
			nth, answer, list := len(m.queries), m.chatAnswer, m.list
			m.mu.Unlock()
			if answer != nil {
				status, body := answer(r.URL.Query(), nth)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(chatBody(list)))
			return
		}
		if strings.HasPrefix(r.URL.Path, eventsPath) {
			atomic.AddInt32(&m.conns, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(m.Server.Close)
	return m
}

func (m *markReadServer) dials() int32 { return atomic.LoadInt32(&m.conns) }

func (m *markReadServer) setList(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list = s
}

func (m *markReadServer) snapshot() []markCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]markCall(nil), m.calls...)
	return out
}

// serveChat installs the /api/chat responder under the lock, so a test that sets
// it cannot race the handler goroutine that reads it.
func (m *markReadServer) serveChat(f func(q url.Values, nth int) (int, string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatAnswer = f
}

// chatQueries is every /api/chat request line this server saw, in order.
func (m *markReadServer) chatQueries() []url.Values {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]url.Values(nil), m.queries...)
}

func (m *markReadServer) rawBodies() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.bodies...)
}

// tsMsg builds one wire message with an explicit ts.
func tsMsg(id, from, to string, ts float64) string {
	return fmt.Sprintf(`{"id":%q,"from":%q,"to":%q,"body":"body-%s","ts":%g}`, id, from, to, id, ts)
}

func markCfg(base, home string) Config {
	return Config{Base: base, Token: "t", ID: "kyle", Home: home}
}

// ---------------------------------------------------------------------------
// GUARDRAIL ① — the receipt trails the print, never leads it.
// ---------------------------------------------------------------------------

// A drain that prints NOTHING files NOTHING.
//
// ⚠️ THIS TEST LOST ITS ORIGINAL SUBJECT. It used to drive the SILENT FIRST-RUN
// BASELINE — a drain that fetched a full inbox, printed none of it and had to
// file no receipt — which was exactly the shape that produced the bug (a
// listener merely coming up lit the ✓ on messages nobody had seen). That mode no
// longer exists: with the local ledger gone there is nothing that fetches lines
// and declines to print them, so the case cannot be constructed and is not
// guarded here or anywhere. What remains guarded is the invariant it was an
// instance of: no lines ⇒ no receipt.
func TestDrainChat_NothingPrinted_FilesNoReadReceipt(t *testing.T) {
	srv := newMarkReadServer(t, "[]")
	var out bytes.Buffer

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), &out, &drainWarner{}, nil)

	if out.Len() != 0 {
		t.Fatalf("precondition: an empty inbox must print nothing, got %q", out.String())
	}
	if calls := srv.snapshot(); len(calls) != 0 {
		t.Fatalf("a drain that printed nothing filed %d read receipt(s): %+v — "+
			"the ✓ would then mean 'a listener connected', not 'someone read it'", len(calls), calls)
	}
}

// A fetch fault prints nothing and must likewise leave no receipt behind.
func TestDrainChat_FetchFault_FilesNoReadReceipt(t *testing.T) {
	var marks int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == markReadPath {
			marks++
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()
	var out bytes.Buffer
	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), &out, &drainWarner{}, nil)
	if marks != 0 {
		t.Fatalf("a failed chat refetch filed %d read receipt(s), want 0", marks)
	}
}

// ORDER, not just presence: at the moment the receipt is filed the line it
// claims must ALREADY be on the session's stream. A process killed between the
// fetch and the print must leave no receipt, and the only way to pin that from
// the outside is to look at what had been printed when the POST landed.
func TestDrainChat_ReadReceiptIsFiledOnlyAfterTheLineIsPrinted(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	out := &syncBuf{}
	var printedWhenMarked string
	srv.mu.Lock()
	srv.beforeMark = func() { printedWhenMarked = out.String() }
	srv.mu.Unlock()

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), out, &drainWarner{}, nil)

	if len(srv.snapshot()) != 1 {
		t.Fatalf("precondition: want exactly one receipt, got %+v", srv.snapshot())
	}
	if !strings.Contains(printedWhenMarked, "chat from boss (#m1") {
		t.Fatalf("the read receipt was filed while the session had only seen %q — "+
			"the line must be printed BEFORE its receipt is filed", printedWhenMarked)
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL ② — one batch, several senders, each to its OWN watermark.
// ---------------------------------------------------------------------------

func TestDrainChat_MultipleSenders_EachMarkedToItsOwnWatermark(t *testing.T) {
	now := float64(time.Now().Unix())
	list := "[" + strings.Join([]string{
		tsMsg("a1", "alice", "kyle", now-90),
		tsMsg("b1", "bob", "kyle", now-80),
		tsMsg("a2", "alice", "kyle", now-70), // alice's newest
		tsMsg("c1", "carol", "other", now-5), // not for me: never printed, never marked
	}, ",") + "]"
	srv := newMarkReadServer(t, list)
	var out bytes.Buffer

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), &out, &drainWarner{}, nil)

	calls := srv.snapshot()
	sort.Slice(calls, func(i, j int) bool { return calls[i].Peer < calls[j].Peer })
	want := []markCall{
		{Peer: "alice", LastReadTS: now - 70},
		{Peer: "bob", LastReadTS: now - 80},
	}
	if len(calls) != len(want) {
		t.Fatalf("filed %d receipts %+v, want exactly %+v — one per sender in the batch",
			len(calls), calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("receipt #%d = %+v want %+v — bob must not be advanced to alice's ts "+
				"(nor alice pinned to her older message); full set %+v", i, calls[i], want[i], calls)
		}
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL B — 🔴 NOTHING IS EVER MARKED READ WITHOUT HAVING BEEN PRINTED.
// (The one exception, `sender == this member`, has its own tests further down.)
// ---------------------------------------------------------------------------

// A receipt is a per-sender WATERMARK: "everything at or below this ts is read".
// That makes it a claim about lines this drain never names, so the only safe
// value it can carry is the newest ts THIS DRAIN ACTUALLY PRINTED for that
// sender — never the newest it merely fetched.
//
// This is the core promise of the removal and the fix for the defect it was
// written against (R15/L1): with a local ledger in front of the print, a line
// could be swallowed by a silent prime, stay unprinted, and then be swept to
// read by a LATER line of the same sender riding that sender's watermark. The
// sender's ✓ lit for a message nobody had seen.
//
// Asserted BOTH ways, because either one alone is passable by a broken build:
//
//	① equality — every receipt's ts is EXACTLY the max printed ts of that sender
//	  (a watermark computed from the fetched set instead of the printed one, or
//	  from the batch's newest line instead of the sender's, breaks this);
//	② coverage — no message at or below a receipt is missing from the output
//	  (a build that prints a SUBSET and receipts the whole batch — the print cap
//	  this drain used to have — breaks this).
//
// The fixture is deliberately long and two-sendered: 32 rows across two pages,
// with alice holding one very old line and bob a long tail, so a per-batch
// watermark and a per-page one are both wrong in a way the assertions can see.
func TestDrainChat_EveryWatermarkEqualsWhatThatSenderActuallyPrinted(t *testing.T) {
	now := float64(time.Now().Unix())
	type wire struct {
		id, from string
		ts       float64
	}
	rows := []wire{{"a-old", "alice", now - 500}, {"b-old", "bob", now - 499}}
	for i := 0; i < 30; i++ { // well past the 20 lines the old cap would have kept
		rows = append(rows, wire{fmt.Sprintf("b-%d", i), "bob", now - float64(100-i)})
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, tsMsg(r.id, r.from, "kyle", r.ts))
	}
	pages := []string{chatPage("c1", lines[:20]...), chatPage("", lines[20:]...)}
	srv := newMarkReadServer(t, "[]")
	srv.serveChat(func(q url.Values, nth int) (int, string) {
		if nth > len(pages) {
			return 200, chatPage("")
		}
		return 200, pages[nth-1]
	})
	var out bytes.Buffer

	mustReturn(t, "drainChat over a two-page backlog", func() {
		drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), &out, &drainWarner{}, nil)
	})

	// What was PRINTED, read back off the stream — never off the fixture.
	printedHigh := map[string]float64{}
	for _, r := range rows {
		if strings.Contains(out.String(), "#"+r.id) && r.ts > printedHigh[r.from] {
			printedHigh[r.from] = r.ts
		}
	}
	filed := map[string]float64{}
	for _, c := range srv.snapshot() {
		if c.LastReadTS > filed[c.Peer] {
			filed[c.Peer] = c.LastReadTS
		}
	}

	// ① EQUALITY, in both directions: no sender receipted who printed nothing,
	// and no sender printed who was receipted at some other line's ts.
	if len(filed) != len(printedHigh) {
		t.Fatalf("receipts name %d senders %v but %d senders had lines printed %v",
			len(filed), filed, len(printedHigh), printedHigh)
	}
	for peer, want := range printedHigh {
		if got := filed[peer]; got != want {
			t.Fatalf("%s was marked read up to ts %v, but the newest line of theirs "+
				"this drain actually printed was ts %v — a watermark above what was "+
				"shown lights the ✓ on messages nobody read", peer, got, want)
		}
	}

	// ② COVERAGE: nothing swept in under a watermark went unprinted.
	for _, r := range rows {
		printed := strings.Contains(out.String(), "#"+r.id)
		if r.ts <= filed[r.from] && !printed {
			t.Fatalf("%s (from %s, ts %v) is marked read by a watermark of %v but was "+
				"never printed — the sender sees a ✓ for something nobody was shown",
				r.id, r.from, r.ts, filed[r.from])
		}
		if !printed {
			t.Fatalf("%s was never printed at all; the backfill is not complete", r.id)
		}
	}
}

// A sender whose message carries no usable ts has no watermark to report, so it
// is skipped rather than reported as 0 (which would be a silent no-op anyway).
//
// ⚠️ WHAT THIS COSTS SINCE THE LEDGER WENT. A row that is printed and NOT
// receipted stays in the server's unread set for ever, so it would be re-printed
// by every drain from here on — the local ledger used to absorb that. It is not
// reachable: the unread walk is `m.ts > COALESCE(last_read_ts, 0)`
// (dal.go listChatUnread), so a row with ts ≤ 0 is never in the unread set to
// begin with and this drain never sees one. The fixture below is hand-built. If
// that predicate ever changes, this is the line that turns into a loop.
func TestDrainChat_MessageWithoutTs_FilesNoReceipt(t *testing.T) {
	srv := newMarkReadServer(t, `[{"id":"m1","from":"boss","to":"kyle","body":"hi"}]`)
	var out bytes.Buffer
	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), &out, &drainWarner{}, nil)
	if !strings.Contains(out.String(), "#m1") {
		t.Fatalf("precondition: the line must still print, got %q", out.String())
	}
	if calls := srv.snapshot(); len(calls) != 0 {
		t.Fatalf("a ts-less message filed %+v, want no receipt", calls)
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL ③ — both entrances into a printing drain file receipts.
// ---------------------------------------------------------------------------

// THE CONNECT DRAIN through run(): the drain that sits before the connect loop
// surfaces whatever the server still has unread, and those lines reach the
// session — so that path must file receipts, each sender to its own watermark.
//
// Against a server that keeps its OWN unread set, because the question "did the
// receipt land" cannot be answered by a canned list.
func TestListenerRun_ConnectDrain_FilesReadReceipts(t *testing.T) {
	home := t.TempDir()
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{
		{"m1", "boss", "kyle", now - 300},
		{"m2", "alice", "kyle", now - 100},
	})
	cfg := markCfg(srv.URL, home)

	out := &syncBuf{}
	l := bootListener(t, srv.Server, cfg, out)
	got := runUntil(t, l, out, func() bool { return srv.dials() >= 3 },
		"the listener dialled 3 times, so the connect drain has certainly run")

	for _, want := range []string{"chat from boss (#m1", "chat from alice (#m2"} {
		if n := strings.Count(got, want); n != 1 {
			t.Fatalf("precondition: %q printed %d times, want 1; out = %q", want, n, got)
		}
	}
	for peer, ts := range map[string]float64{"boss": now - 300, "alice": now - 100} {
		rs := srv.receiptsFor(peer)
		if len(rs) != 1 || rs[0].LastReadTS != ts {
			t.Fatalf("the connect drain filed %+v for %s, want exactly one at ts %g — "+
				"a line the session actually read must light the ✓, and the ✓ of a "+
				"sender whose line printed once must not be re-filed on every reconnect",
				rs, peer, ts)
		}
	}
}

// RECONNECT: after the connect drain, a live `chat` delta drives another drain
// through dispatch. That path prints too, so it must mark too — and it must
// mark the NEW sender's own watermark, not re-file the earlier drain's.
func TestListener_ChatDeltaAfterReconnect_FilesReadReceipt(t *testing.T) {
	home := t.TempDir()
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{{"m1", "boss", "kyle", now - 300}})
	cfg := markCfg(srv.URL, home)

	out := &syncBuf{}
	l := bootListener(t, srv.Server, cfg, out)

	// Stop the loop BEFORE staging the delta. drainChat is single-goroutine by
	// construction (every real caller runs on the listen loop), and since the
	// reconnect path drains chat too, a delta dispatched from the test goroutine
	// while the loop is still re-dialling would be a second concurrent drain of
	// the same window — a race this test invented, not one the listener can have.
	runUntil(t, l, out, func() bool { return strings.Contains(out.String(), "chat from boss (#m1") },
		"the connect drain printed m1")

	// …then the delta, on the only goroutine left, exactly as dispatch sees it.
	srv.add(unreadRow{"m2", "alice", "kyle", now - 10})
	l.dispatch([]byte(`{"topic":"chat","data":{"id":"m2"}}`))

	if !strings.Contains(out.String(), "chat from alice (#m2") {
		t.Fatalf("precondition: the delta drain must print m2, got %q", out.String())
	}
	got := srv.receiptsFor("alice")
	if len(got) != 1 || got[0].LastReadTS != now-10 {
		t.Fatalf("the live-delta drain filed %+v for alice, want exactly one at ts %g",
			got, now-10)
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL C — 🔴 A RECEIPT THAT DID NOT LAND LEAVES THE BATCH PRINTABLE.
// ---------------------------------------------------------------------------

// A mark-read that comes back non-200 moved no watermark, so those rows are
// still in the server's unread set and the NEXT drain prints them again.
//
// This is the fix for R15/L2, and it is worth being precise about why it is
// structural rather than a check. The old build carried a local ledger of ids it
// had surfaced; a row that failed to be receipted was nonetheless recorded
// there, so it was suppressed locally for ever while the server went on holding
// it unread. The removal did not add a rule saying "do not record a row whose
// receipt failed" — it deleted the only place a row could be recorded at all.
// There is now exactly one writer of "this has been seen" (the receipt), one
// reader of it (the unread walk), and a write that fails is a write that did not
// happen.
//
// 🔴 THE SERVER IS THE ORACLE, NOT THE CLIENT. The assertion is on what
// GET /api/chat still owes this member, and the fake mark-read route records
// nothing on a non-200 — exactly as the real route cannot.
// nonEmptyLines counts what a drain actually wrote, without looking at what any
// of it says: the owner's 2026-08-20 ruling bars partial keyword matching, and
// "did a warning appear alongside the reprint" is a question about how many
// lines came out, not about their wording.
func nonEmptyLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func TestDrainChat_MarkReadRefused_TheSameBatchPrintsAgainNextDrain(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{
		{"m1", "boss", "kyle", now - 300},
		{"m2", "alice", "kyle", now - 200},
	})
	cfg := markCfg(srv.URL, t.TempDir())
	srv.refuseReceipts(500)

	var out bytes.Buffer
	warn := &drainWarner{}
	if n := drainChat(srv.Client(), cfg, &out, warn, nil); n != 2 {
		t.Fatalf("precondition: the first drain must print both lines, n=%d out=%q",
			n, out.String())
	}
	if got := srv.receiptsFor("boss"); len(got) != 1 {
		t.Fatalf("precondition: the receipt must have been ATTEMPTED, got %+v — "+
			"a drain that never tried cannot show anything about one that failed", got)
	}
	if ids := srv.unreadIDs("kyle"); len(ids) != 2 {
		t.Fatalf("a refused receipt moved the server's unread set to %v — it must "+
			"move nothing", ids)
	}
	// The first drain is also the ONLY one that explains itself: two printed
	// lines plus the single warning the refusal produced. Counted as lines and
	// not matched as text — what the warning SAYS is not this test's business,
	// but whether the reprints outlive it is.
	if got := nonEmptyLines(out.String()); got != 3 {
		t.Fatalf("the first refused drain wrote %d lines, want 3 (2 messages + the "+
			"one warning); out = %q", got, out.String())
	}

	// A NEW process (nothing is carried over — there is nothing that could be)
	// finds the same two lines still owed, and prints them.
	var out2 bytes.Buffer
	if n := drainChat(srv.Client(), cfg, &out2, &drainWarner{}, nil); n != 2 {
		t.Fatalf("the next drain printed %d lines, want the same 2 — a batch whose "+
			"receipt was refused must come back, or it is lost with nobody told; "+
			"out = %q", n, out2.String())
	}
	for _, id := range []string{"m1", "m2"} {
		if c := strings.Count(out2.String(), "(#"+id); c != 1 {
			t.Fatalf("%s came back %d times on the retry drain, want 1; out = %q",
				id, c, out2.String())
		}
	}

	// 🔴 THE SAME PROCESS, WHICH IS THE CASE THAT ACTUALLY HAPPENS. A listener
	// does not restart between drains: it reconnects, and every reconnect drains
	// again with the SAME warner. So the reprint comes back while the latch is
	// already spent — the batch repeats and the explanation does not. That
	// asymmetry is why warnMarkReadFailed's one line has to announce the reprints
	// in advance rather than describe a dark ✓; this asserts the asymmetry it is
	// written against, so a build that lost either half reddens here.
	var outSame bytes.Buffer
	if n := drainChat(srv.Client(), cfg, &outSame, warn, nil); n != 2 {
		t.Fatalf("the same process's next drain printed %d lines, want the same 2; "+
			"out = %q", n, outSame.String())
	}
	if got := nonEmptyLines(outSame.String()); got != 2 {
		t.Fatalf("the repeat drain wrote %d lines, want exactly the 2 messages and "+
			"nothing else — the warning is once per process, so these reprints "+
			"arrive with no explanation of their own and the FIRST warning has to "+
			"have covered them; out = %q", got, outSame.String())
	}

	// …and once the endpoint recovers, the SAME rows are receipted and stop
	// coming back. Without this the test would also pass on a build that simply
	// never marks anything read.
	srv.refuseReceipts(200)
	var out3 bytes.Buffer
	if n := drainChat(srv.Client(), cfg, &out3, &drainWarner{}, nil); n != 2 {
		t.Fatalf("the recovery drain printed %d, want 2; out = %q", n, out3.String())
	}
	if ids := srv.unreadIDs("kyle"); len(ids) != 0 {
		t.Fatalf("still unread after a receipt that DID land: %v", ids)
	}
}

// ---------------------------------------------------------------------------
// A receipt that does not land says so — once — instead of leaving a silently
// dark ✓ with no error anywhere.
// ---------------------------------------------------------------------------

func TestDrainChat_MarkReadRejected_WarnsOncePerProcess(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	srv.mu.Lock()
	srv.status = 422
	srv.mu.Unlock()
	cfg := markCfg(srv.URL, t.TempDir())
	warn := &drainWarner{}
	var out bytes.Buffer

	drainChat(srv.Client(), cfg, &out, warn, nil)
	if c := strings.Count(out.String(), "mark-read"); c != 1 {
		t.Fatalf("a rejected receipt warned %d times, want exactly 1; out = %q", c, out.String())
	}
	// 🔴 THE WARNING MUST NOT WEAR THE TRANSPORT HEAD. A codex member does not
	// read this pane: cli/ocwarden/codex_session.go filters every line starting
	// `[ocagent] listen:` out of the model's turn as transport diagnostics. This
	// warning is the only sign that a ✓ is永遠不會亮 — spelled with that head it
	// would be swallowed on the codex side and printed on the claude side, with
	// both suites still green. So the first column is asserted, not just the count.
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if !strings.Contains(line, "mark-read") {
			continue
		}
		if !strings.HasPrefix(line, agentLinePrefix) {
			t.Fatalf("the warning must start at column 0 with %q; got %q", agentLinePrefix, line)
		}
		if strings.HasPrefix(line, agentLinePrefix+"listen:") {
			t.Fatalf("the warning must NOT start with %q — the codex sidecar drops that "+
				"prefix as transport noise, so the warning would reach a codex member "+
				"never at all; got %q", agentLinePrefix+"listen:", line)
		}
	}

	srv.setList("[" + strings.Join([]string{
		tsMsg("m1", "boss", "kyle", now-30),
		tsMsg("m2", "boss", "kyle", now-20),
	}, ",") + "]")
	out.Reset()
	drainChat(srv.Client(), cfg, &out, warn, nil)
	if strings.Contains(out.String(), "mark-read") {
		t.Fatalf("the warning must not repeat every drain; second drain out = %q", out.String())
	}
}

// ---------------------------------------------------------------------------
// THE WIRE TEST — the receipt body against the frozen MarkChatReadDTO.
// ---------------------------------------------------------------------------

// TestDrainChat_MarkReadBodyMatchesFrozenSchema drives the REAL producer
// (drainChat → reportChatRead → postJSON) and confronts the bodies a real test
// server caught with the schema frozen in spec/openapi.json.
//
// It has to be the producer's own body, never one assembled here: MarkChatReadDTO
// declares additionalProperties:false and the server decodes with
// DisallowUnknownFields, so ONE key this listener sends that the schema does not
// declare rejects the whole receipt. The listener already prints before it
// reports and warns at most once per process, so a permanently-422 receipt looks
// from the outside exactly like a healthy one after the first line — the ✓ simply
// never lights. A hand-built body compared here would agree with itself and say
// nothing about that.
func TestDrainChat_MarkReadBodyMatchesFrozenSchema(t *testing.T) {
	declared := frozenIngestProperties(t, "MarkChatReadDTO")

	now := float64(time.Now().Unix())
	// Two senders, so the loop below confronts more than one produced body.
	srv := newMarkReadServer(t, "["+strings.Join([]string{
		tsMsg("a1", "alice", "kyle", now-90),
		tsMsg("b1", "bob", "kyle", now-80),
		tsMsg("a2", "alice", "kyle", now-70),
	}, ",")+"]")
	var out bytes.Buffer

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), &out, &drainWarner{}, nil)

	bodies := srv.rawBodies()
	if len(bodies) != 2 {
		t.Fatalf("precondition: want one receipt per sender (2), got %d: %q", len(bodies), bodies)
	}

	// Which uplinks this test actually walked against the frozen schema — not
	// which ones it meant to. Joined below against the manifest's own commitment.
	walked := map[string]int{}
	for _, body := range bodies {
		walked[markReadPath]++
		if bad := schemaViolations(body, declared); len(bad) > 0 {
			t.Errorf("mark-read body has keys the frozen schema refuses %v — the receipt "+
				"would 422 and the sender's ✓ would stay dark forever, with at most one "+
				"warning line ever printed; body=%s", bad, body)
		}
	}

	want := manifestUplinkPaths(t, "cli/ocagent/listen_markread_test.go")
	for route, rows := range want {
		if rows != 1 {
			t.Fatalf("cli/uplinks.json commits %d uplinks to %s through this wire test. "+
				"This join compares route SETS, so it cannot tell them apart — give the "+
				"second one its own assertion, or split the wire test.", rows, route)
		}
	}
	seen := map[string]int{}
	for route := range walked {
		seen[route] = 1
	}
	if !maps.Equal(seen, want) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but the producer posted to "+
			"%v — a committed uplink nobody compared is the gap this manifest exists to close.",
			want, walked)
	}
}

// The reconnect drain is a THIRD drain entrance, and the rule the other two obey
// holds here too: it prints, so it marks — and the receipt carries the message's
// own ts. Nothing is dispatched: a chat delta would let the delta path take the
// credit, and this test would then prove nothing.
func TestListenerRun_ReconnectBackfill_PrintsThenFilesReadReceipt(t *testing.T) {
	home := t.TempDir()
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, nil)
	cfg := markCfg(srv.URL, home)

	out := &syncBuf{}
	l := bootListener(t, srv.Server, cfg, out)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return srv.dials() >= 3 },
		"the listener is up and re-dialling with nothing owed")
	if got := srv.receiptsFor("alice"); len(got) != 0 {
		t.Fatalf("precondition: nothing was owed, so nothing may be marked yet: %+v", got)
	}

	// alice writes into the outage gap; no delta is ever fanned.
	srv.add(unreadRow{"m2", "alice", "kyle", now - 10})
	// Bounded on DIALS, not on the line — a build with no reconnect drain must
	// fail on a count rather than on a timeout.
	staged := srv.dials()
	waitForCond(t, func() bool { return srv.dials() >= staged+3 },
		"three more reconnects happened after alice's message landed in the gap")
	cancel()
	<-done

	if n := strings.Count(out.String(), "chat from alice (#m2"); n != 1 {
		t.Fatalf("the reconnect drain printed alice's message %d times, want exactly 1; out = %q",
			n, out.String())
	}
	got := srv.receiptsFor("alice")
	if len(got) != 1 || got[0].LastReadTS != now-10 {
		t.Fatalf("the reconnect drain filed %+v for alice, want exactly one at ts %g",
			got, now-10)
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL ④ — "printed" is not "delivered" when somebody else has to carry
// the line the rest of the way (T-48, the ack protocol).
//
// A claude member reads these lines directly, so printing IS delivery. A codex
// member reads them through the ocwarden sidecar, which turns each line into an
// App Server turn — and that turn can be REFUSED. The listener used to file the
// receipt and record the id regardless, so a refused message was marked read,
// dropped from the unread window, and never printed again: gone, with every
// party believing it had landed.
// ---------------------------------------------------------------------------

func ackEnv(v string) func(string) string {
	return func(key string) string {
		if key == listenAckEnv {
			return v
		}
		return ""
	}
}

// The claude path must not move by one byte: no marker on stdout, no wait for
// anything, receipt filed exactly as before.
func TestDrainChat_WithoutTheAckEnv_PrintsNoMarkerAndStillFilesTheReceipt(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	warn := &drainWarner{}
	var out bytes.Buffer

	// stdin is deliberately a reader that would BLOCK FOREVER if anything read
	// it: the claude path must never wait for an answer nobody is going to send.
	gate := newAckGate(ackEnv(""), blockingReader{})
	if gate != nil {
		t.Fatalf("OC_LISTEN_ACK is unset, so there is no consumer to ack — the gate " +
			"must not exist at all")
	}

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), &out, warn, gate)

	if strings.Contains(out.String(), noticeBatch) {
		t.Errorf("the claude transcript grew a protocol line it has no reader for: %q",
			out.String())
	}
	if calls := srv.snapshot(); len(calls) != 1 || calls[0].Peer != "boss" {
		t.Errorf("the claude path stopped filing its own receipts: %+v", calls)
	}
	// ⚠️ GONE WITH THE LEDGER: this used to also assert that m1 landed in the
	// local seen set. Nothing records anything locally now — "the next drain
	// does not print it again" is the server's job, done by the receipt asserted
	// one line above, and pinned end-to-end by
	// TestDrainChat_PrintedLineIsReceiptedAndDoesNotComeBack.
}

// A gate exists only when the parent process asked for one.
func TestNewAckGate_OnlyOnTheParentsExplicitRequest(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "": false, "0": false, "true": false} {
		got := newAckGate(ackEnv(value), strings.NewReader("")) != nil
		if got != want {
			t.Errorf("OC_LISTEN_ACK=%q ⇒ gate=%v, want %v — a gate nobody answers "+
				"hangs the drain, and a missing gate marks undelivered mail read",
				value, got, want)
		}
	}
}

// The ack path: marker out, verdict in, and only THEN the receipt.
func TestDrainChat_AckedBatch_FilesTheReceiptAndRecordsTheIDs(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	warn := &drainWarner{}
	var out bytes.Buffer

	gate := newAckGate(ackEnv("1"), strings.NewReader("ack 1\n"))
	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), &out, warn, gate)

	if !strings.Contains(out.String(), agentLinePrefix+noticeBatch+" 1") {
		t.Fatalf("the listener never told the sidecar where the batch ended, so no "+
			"ack can ever arrive and this drain hangs forever; out = %q", out.String())
	}
	if calls := srv.snapshot(); len(calls) != 1 {
		t.Errorf("an acked batch must still file its receipt: %+v", calls)
	}
	// The receipt above IS the record: nothing else was ever written.
}

// 🔴 THE WHOLE POINT. No ack ⇒ no receipt, no seen id, and the SAME message on
// the next drain.
// A consumer that goes quiet must not take this member's hearing with it: the
// wait for a verdict happens on the listener's ONLY thread, so an unbounded one
// turns a single unanswerable batch into a member that receives nothing ever
// again — and nothing anywhere says so.
func TestDrainChat_AckThatNeverArrives_TimesOutSaysSoAndKeepsTheMessageUnread(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	cfg := markCfg(srv.URL, t.TempDir())
	warn := &drainWarner{}
	var out bytes.Buffer

	// A pipe nobody ever writes to: the consumer is alive (stdin is open) and
	// simply never answers. A closed stdin is the OTHER case and already covered.
	answers, quiet := io.Pipe()
	defer quiet.Close()
	gate := newAckGate(ackEnv("1"), answers)
	gate.wait = 20 * time.Millisecond

	done := make(chan struct{})
	go func() {
		defer close(done)
		drainChat(srv.Client(), cfg, &out, warn, gate)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the drain never returned: an unanswered batch blocked the listener " +
			"forever, which is this member going permanently deaf")
	}

	if calls := srv.snapshot(); len(calls) != 0 {
		t.Errorf("a message whose delivery was never confirmed was marked read: %+v", calls)
	}
	// No local record can hold an unconfirmed line either, because there is no
	// local record at all: the empty receipt list above is the whole state.
	if !strings.Contains(out.String(), "等不到") {
		t.Errorf("the timeout must say so — a silent one is indistinguishable from a "+
			"delivery that worked; out = %q", out.String())
	}
	// The warning must NOT wear the transport head, or the sidecar swallows it
	// and the one party who could go and look never hears about it.
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.Contains(line, "等不到") && strings.HasPrefix(line, agentLinePrefix+"listen:") {
			t.Errorf("the timeout warning must not start with %q; got %q",
				agentLinePrefix+"listen:", line)
		}
	}
}

func TestDrainChat_UnackedBatch_LeavesTheMessageUnreadUnseenAndReprintable(t *testing.T) {
	now := float64(time.Now().Unix())
	// A MULTI-PAGE backfill, because "a batch" had to be redefined when the drain
	// started paging: the whole backfill is ONE batch, gated once at the end, so
	// a single refusal takes every page of it back rather than leaving the
	// session holding some pages and not others.
	byCursor := map[string]string{
		"":   chatPage("c1", tsMsg("m1", "boss", "kyle", now-30)),
		"c1": chatPage("c2", tsMsg("m2", "boss", "kyle", now-20)),
		"c2": chatPage("", tsMsg("m3", "boss", "kyle", now-10)),
	}
	srv := newMarkReadServer(t, "[]")
	srv.serveChat(func(q url.Values, _ int) (int, string) {
		page, ok := byCursor[q.Get("cursor")]
		if !ok {
			t.Errorf("unknown cursor %q", q.Get("cursor"))
			return 200, chatPage("")
		}
		return 200, page
	})
	home := t.TempDir()
	cfg := markCfg(srv.URL, home)
	warn := &drainWarner{}
	var out bytes.Buffer

	// An answer for a DIFFERENT batch must not be mistaken for this one's.
	gate := newAckGate(ackEnv("1"), strings.NewReader("ack 7\nnack 1\n"))
	mustReturn(t, "drainChat over a nacked three-page backfill", func() {
		drainChat(srv.Client(), cfg, &out, warn, gate)
	})

	for _, id := range []string{"m1", "m2", "m3"} {
		if !strings.Contains(out.String(), "chat from boss (#"+id) {
			t.Fatalf("precondition: every page must be printed before anyone can "+
				"fail to deliver it; %s missing from %q", id, out.String())
		}
	}
	// ONE gate for the whole backfill: a second marker would mean a long
	// catch-up blocks the listener once per page.
	if n := strings.Count(out.String(), agentLinePrefix+noticeBatch+" "); n != 1 {
		t.Fatalf("the backfill printed %d batch markers, want exactly 1 — every page "+
			"of one catch-up is one batch; out = %q", n, out.String())
	}
	if calls := srv.snapshot(); len(calls) != 0 {
		t.Errorf("a batch that never reached the agent was marked read: %+v — the "+
			"sender now sees a ✓ for something nobody was ever shown", calls)
	}
	// ⚠️ WHAT THIS REPLACED: an assertion that no undelivered id reached the
	// local seen set, which had to be paired with a hand-written "drop the ids
	// nobody confirmed" pass in drainChat. Both are gone. A nacked batch files
	// no receipt (asserted above), the server's unread set therefore never
	// moved, and the redelivery below is that fact rather than an arrangement.

	// The recovery this exists for: the next drain prints ALL of it again.
	var out2 bytes.Buffer
	gate2 := newAckGate(ackEnv("1"), strings.NewReader("ack 1\n"))
	var n int
	mustReturn(t, "the redelivery drain", func() {
		n = drainChat(srv.Client(), cfg, &out2, &drainWarner{}, gate2)
	})
	if n != 3 {
		t.Fatalf("the next drain reported %d unread, want 3", n)
	}
	for _, id := range []string{"m1", "m2", "m3"} {
		if !strings.Contains(out2.String(), "chat from boss (#"+id) {
			t.Fatalf("undelivered %s never came back; out = %q", id, out2.String())
		}
	}
	if calls := srv.snapshot(); len(calls) != 1 {
		t.Errorf("the re-delivered batch was acked, so its receipt must now be "+
			"filed exactly once: %+v", calls)
	}
}

// ---------------------------------------------------------------------------
// rc-dccab860be32 — "don't read me my own words back" moved from the API to
// this client, and with it the ONE exception to "printed, therefore read".
// ---------------------------------------------------------------------------

type unreadRow struct {
	id, from, to string
	ts           float64
}

// unreadChatServer is a MINIATURE of the real unread contract: it holds a fixed
// message list, answers ?unread=true&recipient=&limit= oldest-first from each
// sender's watermark, pages with a cursor, and ADVANCES that watermark on
// POST /api/chat/mark-read.
//
// A canned list cannot answer "what does the NEXT drain see", because the answer
// has to depend on what this drain reported. That question is the whole point of
// the self-sent receipt: without it the same rows come back forever.
type unreadChatServer struct {
	*httptest.Server
	mu   sync.Mutex
	rows []unreadRow
	high map[string]float64 // sender → this reader's watermark
	// conns counts /api/events dials, so a run()-level test can wait on a
	// bounded, positively-observed moment instead of on a line that may never
	// come. status is what mark-read answers (non-200 ⇒ the receipt is refused
	// and the watermark does not move — which is the whole of guardrail C).
	conns  int32
	status int
	calls  []markCall // every receipt this server was offered, in order
}

func newUnreadChatServer(t *testing.T, rows []unreadRow) *unreadChatServer {
	t.Helper()
	u := &unreadChatServer{rows: rows, high: map[string]float64{}, status: 200}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == markReadPath {
			var c markCall
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Errorf("mark-read body is not JSON: %v (%s)", err, raw)
			}
			u.mu.Lock()
			u.calls = append(u.calls, c)
			st := u.status
			// A REFUSED receipt moves nothing. That is not a test convenience:
			// the real route either records the watermark and answers 200 or
			// does neither, and a fake that advanced anyway would hide exactly
			// the failure guardrail C is about.
			if st == 200 && c.LastReadTS > u.high[c.Peer] {
				u.high[c.Peer] = c.LastReadTS
			}
			u.mu.Unlock()
			w.WriteHeader(st)
			_, _ = w.Write([]byte(`{"reader_id":"kyle","peer_id":"` + c.Peer + `","last_read_ts":0}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, eventsPath) {
			// open, say nothing, close ⇒ the listener re-dials forever, and every
			// dial is one reconnect drain the assertions can be bounded on.
			atomic.AddInt32(&u.conns, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(404)
			return
		}
		q := r.URL.Query()
		if q.Get("unread") != "true" {
			t.Errorf("this server only serves the unread walk; got %v", q)
		}
		open := u.unreadFor(q.Get("recipient"))
		start, _ := strconv.Atoi(q.Get("cursor"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		end := start + limit
		if limit <= 0 || end > len(open) {
			end = len(open)
		}
		lines := make([]string, 0, end-start)
		for _, row := range open[start:end] {
			lines = append(lines, tsMsg(row.id, row.from, row.to, row.ts))
		}
		next := ""
		if end < len(open) {
			next = strconv.Itoa(end)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(chatPage(next, lines...)))
	}))
	t.Cleanup(u.Server.Close)
	return u
}

func (u *unreadChatServer) dials() int32 { return atomic.LoadInt32(&u.conns) }

// receiptsFor is every mark-read this server was offered naming `peer`.
func (u *unreadChatServer) receiptsFor(peer string) []markCall {
	u.mu.Lock()
	defer u.mu.Unlock()
	var got []markCall
	for _, c := range u.calls {
		if c.Peer == peer {
			got = append(got, c)
		}
	}
	return got
}

// refuseReceipts makes POST /api/chat/mark-read answer `status` and record
// nothing — a receipt that never lands.
func (u *unreadChatServer) refuseReceipts(status int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status = status
}

// add appends a message to the server's list, as one arriving now.
func (u *unreadChatServer) add(rows ...unreadRow) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.rows = append(u.rows, rows...)
}

// unreadFor is what a caller has NOT marked read, oldest first — the server's
// own view, which is the only thing that can answer whether a receipt landed.
func (u *unreadChatServer) unreadFor(recipient string) []unreadRow {
	u.mu.Lock()
	defer u.mu.Unlock()
	var open []unreadRow
	for _, row := range u.rows {
		if row.to == recipient && row.ts > u.high[row.from] {
			open = append(open, row)
		}
	}
	return open
}

func (u *unreadChatServer) unreadIDs(recipient string) []string {
	var ids []string
	for _, row := range u.unreadFor(recipient) {
		ids = append(ids, row.id)
	}
	return ids
}

// A note this member wrote to itself is not news to it. The unread set carries
// it now (the API stopped filtering `sender == caller`), so this client is the
// only party left that can keep it out of the backfill.
func TestDrainChat_SelfSentMessage_IsNeverPrinted(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{
		{"self-1", "kyle", "kyle", now - 300},
		{"m1", "boss", "kyle", now - 200},
	})
	cfg := markCfg(srv.URL, t.TempDir())
	warn := &drainWarner{}
	var out bytes.Buffer

	n := drainChat(srv.Client(), cfg, &out, warn, nil)

	if !strings.Contains(out.String(), "#m1") {
		t.Fatalf("precondition: a real inbound line must print; out = %q", out.String())
	}
	if strings.Contains(out.String(), "#self-1") {
		t.Fatalf("the member was read its own note back; out = %q", out.String())
	}
	if n != 1 {
		t.Fatalf("unread count = %d, want 1 — a self-sent row is not unread mail", n)
	}
}

// 🔴 …AND IT IS STILL RECEIPTED, which is the one place this drain marks a line
// read without printing it. Nothing marks it if this drain does not: the server
// keeps returning what nobody has read, so every self-addressed note would pile
// up in every future unread walk until the backfill is mostly this member's own
// voice.
//
// The shape is two consecutive drains against a server that keeps its OWN
// unread set: that is the only view which can tell "receipted" apart from
// "merely suppressed by the printer".
func TestDrainChat_SelfSentMessage_IsStillMarkedReadSoItDoesNotComeBack(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{
		{"self-1", "kyle", "kyle", now - 300},
		{"m1", "boss", "kyle", now - 200},
	})
	cfg := markCfg(srv.URL, t.TempDir())
	var out bytes.Buffer

	drainChat(srv.Client(), cfg, &out, &drainWarner{}, nil)

	if got := srv.unreadIDs("kyle"); len(got) != 0 {
		t.Fatalf("still unread after the drain: %v — a self-sent row nobody receipts "+
			"comes back on every walk from here on", got)
	}

	var out2 bytes.Buffer
	if n := drainChat(srv.Client(), cfg, &out2, &drainWarner{}, nil); n != 0 || out2.Len() != 0 {
		t.Fatalf("the second drain fetched the same backlog again: n=%d out=%q",
			n, out2.String())
	}
}

// The self-sent receipt does not ride on the ack gate. The gate answers for what
// the sidecar had to DELIVER, and a self-sent row was delivered to nobody — so a
// refused batch must still not leave it piling up on the server forever.
func TestDrainChat_SelfSentMessage_IsReceiptedEvenWhenTheBatchIsNacked(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{
		{"self-1", "kyle", "kyle", now - 300},
		{"m1", "boss", "kyle", now - 200},
	})
	cfg := markCfg(srv.URL, t.TempDir())
	warn := &drainWarner{}
	var out bytes.Buffer

	gate := newAckGate(ackEnv("1"), strings.NewReader("nack 1\n"))
	mustReturn(t, "drainChat over a nacked batch alongside a self-sent row", func() {
		drainChat(srv.Client(), cfg, &out, warn, gate)
	})

	if got := srv.unreadIDs("kyle"); !slices.Equal(got, []string{"m1"}) {
		t.Fatalf("still unread = %v, want only m1 — the refused line must come back "+
			"and the self-sent one must not", got)
	}
}

// blockingReader never returns from Read: any wait on it is a hang, which is
// what the claude-path assertion above needs in order to mean anything.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }
