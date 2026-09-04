package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// T-48 — the chat drain, with the LOCAL LEDGER REMOVED (owner, rc-224dee5770dd:
// 「拔掉 —— 一件事只留一個說法（server 的未讀）」).
//
// 🔴 WHAT THIS FILE REPLACED AND WHAT IS THEREFORE NO LONGER GUARDED ANYWHERE.
// It was listen_chatseen_test.go, and roughly half of it asserted behaviour that
// no longer exists. Those assertions are not "relaxed", they are GONE with the
// thing they described, and nothing stands in for them:
//
//   - the SILENT FIRST BASELINE (a machine's first listen swallowed the whole
//     inbox without printing it) — deleted, and INVERTED: guardrail A below
//     asserts the opposite, that a first-ever listener prints its unread mail.
//   - chatSeenPath / loadChatSeen / persist and their corrupt-file, empty-array,
//     unwritable-cursor and prune-the-aged-out tests — there is no file, so
//     there is nothing to spell, load, repair, bound or warn about. The
//     `chat-seen 寫不進去` warning is gone with its writer; the OTHER warning,
//     the one for a mark-read receipt that did not land, is NOT (it guards the
//     receipt, not the ledger) and keeps its test in listen_markread_test.go.
//   - the "old build vs new build" differential on the `primed` bit — there is
//     no bit to flip. Guardrail A is the direct assertion it was a proxy for.
//
// What survived was rewritten against a server that keeps its OWN unread set
// (unreadChatServer, listen_markread_test.go), because that is now the only
// party that can answer "has this been surfaced".
// ---------------------------------------------------------------------------

// mutableChatServer serves a chat list the test can swap mid-run, and counts
// how many /api/chat refetches landed. It has no unread bookkeeping — use it
// only where "what does the NEXT drain see" is not the question.
type mutableChatServer struct {
	*httptest.Server
	list  atomic.Value // string
	hits  int32
	conns int32 // /api/events dials
}

func newMutableChatServer(t *testing.T, list string) *mutableChatServer {
	t.Helper()
	m := &mutableChatServer{}
	m.list.Store(list)
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			atomic.AddInt32(&m.hits, 1)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(m.list.Load().(string)))
			return
		}
		if strings.HasPrefix(r.URL.Path, eventsPath) {
			// open, say nothing, close ⇒ the listener re-dials forever, and
			// every dial is one reconnect the drain has to cover.
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

func (m *mutableChatServer) dials() int32 { return atomic.LoadInt32(&m.conns) }

func (m *mutableChatServer) setList(s string) { m.list.Store(s) }

// msgsJSON renders a chat RESPONSE BODY carrying the given message ids — the
// T-48 envelope, not a bare array, because that is what GET /api/chat answers
// and what fetchChat reads. Every use of this helper is a server writing a
// response, so the envelope belongs here rather than at 25 call sites.
func msgsJSON(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"from":"boss","to":"kyle","body":"body-%s"}`, id, id))
	}
	return chatBody("[" + strings.Join(parts, ",") + "]")
}

// chatBody wraps a ChatMessageDTO array literal in the T-48 GET /api/chat
// envelope. A test server that writes a bare array is not serving what the
// route serves, and fetchChat would answer nil — a fault, deliberately, rather
// than "no messages".
func chatBody(list string) string { return `{"messages":` + list + `}` }

// chatPage renders ONE page of the T-48 unread walk: the messages, plus the
// cursor that continues it. An empty `next` mints no cursor at all, which is how
// the wire says "this is the end".
func chatPage(next string, msgs ...string) string {
	body := chatBody("[" + strings.Join(msgs, ",") + "]")
	if next == "" {
		return body
	}
	return strings.TrimSuffix(body, "}") + `,"next_cursor":` + strconv.Quote(next) + "}"
}

// mustReturn fails the test if fn has not returned within the deadline.
//
// 🔴 THE POINT IS THAT A LOOP GUARD'S TEST GOES RED RATHER THAN HANGING. What
// the guards under test prevent is a walk that NEVER comes back, and a test that
// merely blocks on that proves nothing: it looks like a slow suite, gets killed
// by the runner with no attribution, and the next person reruns it. So the wait
// has a deadline of its own and the failure names what it means.
func mustReturn(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("%s never returned — the unread walk is spinning, which is this "+
			"member's only thread gone and this member permanently deaf", what)
	}
}

// emptyChatOrList answers the empty body for `path`: the chat envelope on
// /api/chat, a bare array anywhere else. The stub servers below answer several
// routes from one branch, and only one of them changed shape.
func emptyChatOrList(path string) string {
	if strings.HasPrefix(path, "/api/chat") {
		return chatBody("[]")
	}
	return "[]"
}

// bootListener wires a listener the way runListen does for everything this file
// cares about, minus the parts a test must own (the SSE cursor path and the
// reply-card ledger, which get a home under cfg.Home).
func bootListener(t *testing.T, srv *httptest.Server, cfg Config, out *syncBuf) *listener {
	t.Helper()
	l := newTestListener(srv, cfg, out)
	l.cursorPath = filepath.Join(cfg.Home, "cursor")
	l.replySeen = loadReplyCardSeen(filepath.Join(cfg.Home, "replycards-seen"))
	return l
}

// runUntil starts l.run, waits for cond, and stops it. Returns what was printed.
func runUntil(t *testing.T, l *listener, out *syncBuf, cond func() bool, what string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, cond, what)
	cancel()
	<-done
	return out.String()
}

// ---------------------------------------------------------------------------
// GUARDRAIL A — a first-ever listener, with NO local state of any kind, PRINTS
// the unread mail waiting for it.
// ---------------------------------------------------------------------------

// 🔴 THIS IS THE INVERSION THE WHOLE REMOVAL IS FOR. The old build's first drain
// on a machine ran SILENT: it marked the entire inbox read and printed nothing,
// so a message that arrived before this member's first listen was seen by
// nobody, ever, while its sender's ✓ lit. There is no longer any such state — no
// file, no `primed` bit, no `silent` argument — so the drain has nothing to be
// silent about.
//
// Observed at the run() level, because that is where the wiring lives, and
// bounded on the DIAL COUNT rather than on the line: a build that regressed to
// silence prints nothing at all, and waiting for a line that never comes turns
// its failure into a timeout — which is what a deadlock, a hung dial and a
// mis-wired fake also look like. Dials give it a positively-observed moment to
// be judged at, so the regression reports as a count.
func TestListenerRun_FirstEverOnThisMachine_PrintsTheUnreadInbox(t *testing.T) {
	home := t.TempDir()
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{
		{"m1", "boss", "kyle", now - 300},
		{"m2", "alice", "kyle", now - 200},
	})
	cfg := markCfg(srv.URL, home)

	// The negative control that gives this test its meaning: there is nothing
	// on disk under this member's home, and nothing that could be.
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("precondition: a virgin home, got %v (%v)", entries, err)
	}

	out := &syncBuf{}
	l := bootListener(t, srv.Server, cfg, out)
	got := runUntil(t, l, out, func() bool { return srv.dials() >= 3 },
		"the listener dialled 3 times, so the connect drain has certainly run")

	for _, want := range []string{"chat from boss (#m1", "chat from alice (#m2"} {
		if n := strings.Count(got, want); n != 1 {
			t.Fatalf("a FIRST-EVER listener printed %q %d times across %d dials, want "+
				"exactly 1 — an inbox nobody has read must be read out, and read out "+
				"once; out = %q", want, n, srv.dials(), got)
		}
	}
	if ids := srv.unreadIDs("kyle"); len(ids) != 0 {
		t.Fatalf("still unread after being printed: %v — the receipt must trail the "+
			"line so the next drain does not print it again", ids)
	}
}

// The other half of guardrail A, at the drain level and across TWO processes:
// each message is printed exactly once, by whichever process was up when it
// arrived, with nothing carried between them but the server's own unread set.
func TestDrainChat_AcrossTwoProcesses_PrintsEachLineExactlyOnce(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{{"m1", "boss", "kyle", now - 300}})
	cfg := markCfg(srv.URL, t.TempDir())

	var out1 bytes.Buffer
	if n := drainChat(srv.Client(), cfg, &out1, &drainWarner{}, nil); n != 1 {
		t.Fatalf("process #1 count = %d want 1; out = %q", n, out1.String())
	}

	// …this process dies, and m2/m3 arrive with nobody listening.
	srv.add(unreadRow{"m2", "boss", "kyle", now - 200}, unreadRow{"m3", "alice", "kyle", now - 100})

	var out2 bytes.Buffer
	if n := drainChat(srv.Client(), cfg, &out2, &drainWarner{}, nil); n != 2 {
		t.Fatalf("process #2 backfill = %d want 2; out = %q", n, out2.String())
	}
	for _, id := range []string{"m2", "m3"} {
		if c := strings.Count(out2.String(), "(#"+id); c != 1 {
			t.Fatalf("%s printed %d times in process #2, want 1; out = %q", id, c, out2.String())
		}
	}
	if strings.Contains(out2.String(), "(#m1") {
		t.Fatalf("process #2 re-printed what process #1 had already receipted; out = %q", out2.String())
	}

	// …and a third process with nothing owed says nothing.
	var out3 bytes.Buffer
	if n := drainChat(srv.Client(), cfg, &out3, &drainWarner{}, nil); n != 0 || out3.Len() != 0 {
		t.Fatalf("process #3: n=%d out=%q, want 0 and silence", n, out3.String())
	}
}

// A listener that DROPS and re-dials many times drains on EVERY dial and still
// prints each line exactly ONCE. The dedup is the server's unread set — the
// receipt filed by whichever drain printed the line — and not the number of
// drains, so this is worth pinning at run() level.
func TestListenerRun_Reconnects_NeverRePrintHistory(t *testing.T) {
	home := t.TempDir()
	now := float64(time.Now().Unix())
	srv := newUnreadChatServer(t, []unreadRow{{"m1", "boss", "kyle", now - 300}})
	cfg := markCfg(srv.URL, home)

	out := &syncBuf{}
	l := bootListener(t, srv.Server, cfg, out)
	got := runUntil(t, l, out, func() bool {
		return strings.Contains(out.String(), "chat from boss (#m1") && srv.dials() >= 5
	}, "the connect drain printed m1 and the stream re-dialled repeatedly")

	if n := strings.Count(got, "chat from boss (#m1"); n != 1 {
		t.Fatalf("m1 printed %d times across %d reconnects — want exactly 1; out = %q",
			n, srv.dials(), got)
	}
	// Negative control on the control: the test only means something if the
	// listener really did re-dial many times.
	if c := srv.dials(); c < 5 {
		t.Fatalf("only %d reconnects — this test proved nothing", c)
	}
}

// THE THING THE RECONNECT DRAIN BUYS. /api/events has no replay, so a message
// fanned while this listener held no stream is gone from the stream for good. No
// delta is ever dispatched here: the reconnect drain is the only path left.
//
// 🔴 WAIT ON THE DIAL COUNT, NOT ON THE LINE — a build with no reconnect drain
// never prints m2 at all, and waiting on the output would turn that into a
// timeout instead of a counted difference.
func TestListenerRun_MessageArrivingDuringTheOutage_PrintsOnReconnect(t *testing.T) {
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
	if strings.Contains(out.String(), "chat from") {
		t.Fatalf("precondition: nothing was owed at boot, got %q", out.String())
	}

	// m2 is fanned INTO THE GAP: no dispatch, no chat frame on the wire.
	srv.add(unreadRow{"m2", "boss", "kyle", now - 10})
	staged := srv.dials()
	waitForCond(t, func() bool { return srv.dials() >= staged+3 },
		"three more reconnects happened after the message landed in the gap")
	cancel()
	<-done

	if got := strings.Count(out.String(), "chat from boss (#m2"); got != 1 {
		t.Fatalf("a message that arrived during the outage printed %d times across %d "+
			"reconnects — want exactly 1; out = %q", got, srv.dials(), out.String())
	}
}

// ---------------------------------------------------------------------------
// The backfill is COMPLETE, and the walk that fetches it cannot spin forever.
// ---------------------------------------------------------------------------

// 🔴 EVERY UNREAD LINE PRINTS, across as many pages as it takes, and no line is
// summarised away. There used to be a 20-line print cap that kept the newest and
// announced the rest as 略過 N 則.
func TestDrainChat_LongBacklogAcrossPages_PrintsEveryLine(t *testing.T) {
	const total = 47 // deliberately far more than one page and than the old cap
	ids := make([]string, 0, total)
	msgs := make([]string, 0, total)
	for i := 1; i <= total; i++ {
		id := fmt.Sprintf("m%02d", i)
		ids = append(ids, id)
		msgs = append(msgs, tsMsg(id, "boss", "kyle", float64(i)))
	}
	pages := []string{
		chatPage("c1", msgs[:20]...),
		chatPage("c2", msgs[20:40]...),
		chatPage("", msgs[40:]...),
	}
	srv := newMarkReadServer(t, "[]")
	srv.serveChat(func(q url.Values, nth int) (int, string) {
		if nth > len(pages) {
			return 200, chatPage("")
		}
		return 200, pages[nth-1]
	})
	cfg := markCfg(srv.URL, t.TempDir())

	var out bytes.Buffer
	var n int
	mustReturn(t, "drainChat over a three-page backlog", func() {
		n = drainChat(srv.Client(), cfg, &out, &drainWarner{}, nil)
	})
	if n != total {
		t.Fatalf("returned count = %d want the full unread count %d", n, total)
	}

	got := out.String()
	if lines := strings.Count(got, "[ocagent] chat from "); lines != total {
		t.Fatalf("printed %d chat lines, want all %d — a backfill that prints a "+
			"subset is the bug this replaced", lines, total)
	}
	for _, id := range ids {
		if !strings.Contains(got, "#"+id) {
			t.Fatalf("%s was never printed; out = %q", id, got)
		}
	}
	// No line may stand in for messages the session was not shown.
	for _, banned := range []string{"略過", "只補印", "至少"} {
		if strings.Contains(got, banned) {
			t.Fatalf("the drain still summarises part of the backlog away (%q); out = %q",
				banned, got)
		}
	}
}

// 🔴 LOOP GUARD ①. "Page until next_cursor is empty" is a promise the SERVER
// keeps, and a server that hands back a cursor that never advances would spin
// this walk forever — on the listener's only thread, so this member would
// receive nothing ever again. It stops, it keeps what it did get, and it SAYS SO:
// a silent stop is indistinguishable from a finished backfill.
func TestDrainChat_CursorThatNeverAdvances_StopsSaysSoAndKeepsWhatItGot(t *testing.T) {
	srv := newMarkReadServer(t, "[]")
	srv.serveChat(func(q url.Values, nth int) (int, string) {
		return 200, chatPage("stuck", tsMsg(fmt.Sprintf("m%d", nth), "boss", "kyle", float64(nth)))
	})
	cfg := markCfg(srv.URL, t.TempDir())
	var out bytes.Buffer

	mustReturn(t, "drainChat against a server whose cursor never advances", func() {
		drainChat(srv.Client(), cfg, &out, &drainWarner{}, nil)
	})

	got := out.String()
	if !strings.Contains(got, "沒有前進") || !strings.Contains(got, "get_chat") {
		t.Fatalf("a walk cut short by a stuck cursor must say so and say what to do; "+
			"out = %q", got)
	}
	if !strings.Contains(got, "#m1") || !strings.Contains(got, "#m2") {
		t.Fatalf("what the walk DID fetch must still print; out = %q", got)
	}
	// page 1 mints "stuck", page 2 hands the same token back ⇒ stop.
	if n := len(srv.chatQueries()); n != 2 {
		t.Fatalf("the walk made %d requests, want 2 — it must stop the first time a "+
			"cursor it has already followed comes back", n)
	}
}

// 🔴 LOOP GUARD ②, the second net. A server that mints a FRESH token every time
// defeats guard ① completely, and is just as fatal. The page ceiling stops it,
// and stopping there is also said out loud — the reader must not read a ceiling
// as an inbox.
func TestDrainChat_EndlessFreshCursors_StopsAtThePageCeilingAndSaysSo(t *testing.T) {
	srv := newMarkReadServer(t, "[]")
	srv.serveChat(func(q url.Values, nth int) (int, string) {
		return 200, chatPage(fmt.Sprintf("c%d", nth),
			tsMsg(fmt.Sprintf("m%d", nth), "boss", "kyle", float64(nth)))
	})
	cfg := markCfg(srv.URL, t.TempDir())
	var out bytes.Buffer

	mustReturn(t, "drainChat against a server that never stops issuing cursors", func() {
		drainChat(srv.Client(), cfg, &out, &drainWarner{}, nil)
	})

	got := out.String()
	if !strings.Contains(got, "分頁上限") || !strings.Contains(got, "get_chat") {
		t.Fatalf("stopping at the ceiling must say it was a CEILING, not an inbox; "+
			"out = %q", got)
	}
	if n := len(srv.chatQueries()); n != chatUnreadMaxPages {
		t.Fatalf("the walk made %d requests, want the ceiling %d", n, chatUnreadMaxPages)
	}
}

// A fetch fault prints nothing, files nothing, and therefore costs nothing: the
// window is still unread on the server and the next healthy drain prints all of
// it. This used to be phrased as "the baseline file is untouched"; the file is
// gone, and this is what that assertion was really protecting.
func TestDrainChat_FetchFault_LeavesTheWholeWindowUnread(t *testing.T) {
	now := float64(time.Now().Unix())
	rows := []unreadRow{{"m1", "boss", "kyle", now - 300}, {"m2", "boss", "kyle", now - 200}}
	fail := int32(1)
	srv := newUnreadChatServer(t, rows)
	real := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&fail) == 1 && strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(503)
			return
		}
		real.ServeHTTP(w, r)
	})
	cfg := markCfg(srv.URL, t.TempDir())

	var out bytes.Buffer
	// It used to want SILENCE here. Silence was the bug: a drain that fetched
	// nothing prints exactly what a drain that found nothing prints, so the
	// reader concludes there is no new chat. The window really is untouched
	// (asserted below) — but that has to be SAID, and it has to say it is not
	// "no messages", because that is the wrong conclusion it exists to prevent.
	if n := drainChat(srv.Client(), cfg, &out, &drainWarner{}, nil); n != 0 {
		t.Fatalf("faulting drain returned n=%d, want 0; out = %q", n, out.String())
	}
	// COUNTED, NOT MATCHED (owner, 2026-08-20 bars partial keyword comparison).
	// "Did it announce itself" is a question about whether the drain wrote a line
	// at all — that is the behaviour, and silence is the bug. The two
	// strings.Contains checks that used to stand here asked about the WORDING.
	//
	// 🔴 WHAT IS NO LONGER GUARDED, said plainly: nothing now pins that the line
	// rules out the wrong conclusion ("this is not 'no new messages'"). Someone
	// can rewrite that sentence into one that reads as a quiet no-op and this
	// test stays green. Pinning it would mean comparing the whole string, which
	// is what the ruling asks for and what nobody will keep in step; the honest
	// state is that the sentence is reviewed by people, not by CI.
	if out.Len() == 0 {
		t.Fatal("a faulted drain printed nothing — silence is exactly what it must " +
			"not do, because it is what a drain that FOUND nothing looks like")
	}
	if got := len(srv.unreadIDs("kyle")); got != 2 {
		t.Fatalf("a faulted drain moved the server's unread set to %d rows, want 2 — "+
			"it printed nothing, so it must have receipted nothing", got)
	}

	atomic.StoreInt32(&fail, 0)
	out.Reset()
	if n := drainChat(srv.Client(), cfg, &out, &drainWarner{}, nil); n != 2 {
		t.Fatalf("the drain after the fault printed %d, want the whole window (2); out = %q",
			n, out.String())
	}
}

// A total fetch fault is announced ONCE per episode, and its end is announced
// too.
//
// 🔴 WHY THIS IS A LATCH AND NOT A PRINT. The drain runs on every reconnect, and
// a stream that accepts then immediately closes reconnects for ever at
// listenBackoffCap (15s). Pair that with a refusing /api/chat and an unlatched
// fault line is emitted every ≤15 seconds without limit — and because the line
// does NOT wear the "[ocagent] listen:" head, cli/ocwarden's
// actionableCodexListenerLine forwards it, so on the codex path each copy is a
// model turn. The line is worth having; one per outage is what it is worth.
//
// EVERYTHING HERE IS COUNTED, NEVER MATCHED (owner, 2026-08-20 bars partial
// keyword comparison). "Was it said again" is a question about how many lines a
// drain wrote, and the message's wording is not this test's business.
func TestDrainChat_TotalFetchFault_AnnouncedOncePerEpisode(t *testing.T) {
	now := float64(time.Now().Unix())
	rows := []unreadRow{{"m1", "boss", "kyle", now - 300}, {"m2", "boss", "kyle", now - 200}}
	fail := int32(1)
	srv := newUnreadChatServer(t, rows)
	real := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&fail) == 1 && strings.HasPrefix(r.URL.Path, "/api/chat") &&
			r.URL.Path != markReadPath {
			w.WriteHeader(503)
			return
		}
		real.ServeHTTP(w, r)
	})
	cfg := markCfg(srv.URL, t.TempDir())
	// ONE warner for the whole test — a listener does not get a fresh one per
	// reconnect, and the reconnect is the case this latch exists for.
	warn := &drainWarner{}

	drain := func(what string, wantN, wantLines int) {
		t.Helper()
		var out bytes.Buffer
		if n := drainChat(srv.Client(), cfg, &out, warn, nil); n != wantN {
			t.Fatalf("%s returned n=%d, want %d; out = %q", what, n, wantN, out.String())
		}
		if got := nonEmptyLines(out.String()); got != wantLines {
			t.Fatalf("%s wrote %d lines, want %d; out = %q", what, got, wantLines, out.String())
		}
	}

	drain("the first faulting drain", 0, 1)  // the announcement
	drain("the second faulting drain", 0, 0) // …and silence, for the rest of the episode

	// Recovered: the episode ends with a line of its own, because the silence
	// above cannot otherwise be told apart from a drain that is still failing.
	atomic.StoreInt32(&fail, 0)
	drain("the recovery drain", 2, 3) // the recovery line + the 2 rows it owed

	// A drain with no episode open closes nothing and says nothing.
	drain("a healthy drain", 0, 0)

	// A SECOND episode is announced again — a latch that never reopens would
	// silence every fault after the first for the life of the process.
	atomic.StoreInt32(&fail, 1)
	drain("the next episode's first drain", 0, 1)
}

// ---------------------------------------------------------------------------
// THE DRAIN HANGS OFF THE CONNECT, AND OFF NOTHING ELSE (owner, 2026-09-02:
// 「啟動的時候好像不用做，就連上 SSE 的時候統一做就好」).
// ---------------------------------------------------------------------------

// A listener whose API answers but whose STREAM will not open must drain no chat
// at all. That state is BROKEN, and an inbox printed into a session that is about
// to receive no events is not a rescue — it makes a deaf machine look like a
// working one. Since the removal it would be worse than before: a boot drain
// would also RECEIPT what it printed there, lighting the sender's ✓ for a
// session that can never act on it.
func TestListenerRun_APIUpButStreamNeverOpens_DrainsNoChat(t *testing.T) {
	home := t.TempDir()
	var chatHits, dials int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			atomic.AddInt32(&chatHits, 1)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(msgsJSON("m1", "m2")))
			return
		}
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			w.WriteHeader(404)
			return
		}
		// The stream is the ONLY thing that is down: a 5xx is retried forever,
		// so this listener dials, fails, and never connects.
		atomic.AddInt32(&dials, 1)
		w.WriteHeader(503)
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle", Home: home}

	out := &syncBuf{}
	l := bootListener(t, srv, cfg, out)
	got := runUntil(t, l, out, func() bool { return atomic.LoadInt32(&dials) >= 3 },
		"the listener dialled 3 times and never got a stream")

	if n := atomic.LoadInt32(&chatHits); n != 0 {
		t.Fatalf("/api/chat was refetched %d times without a single open stream, "+
			"want 0 — the chat drain hangs off the connect, not off process start", n)
	}
	if strings.Contains(got, "chat from boss") {
		t.Fatalf("a listener that never connected printed chat; out = %q", got)
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL D — SCOPE. The reply-card ledger is a DIFFERENT type, a different
// file and a different route, and the owner's ruling was about chat.
// ---------------------------------------------------------------------------

// 🔴 `replycards-seen` MUST SURVIVE THE CHAT-LEDGER REMOVAL. A reply card has no
// server-side "unread set" to take over the job — nothing would replace it — so
// pulling it out alongside chatSeen would leave answered cards either re-printed
// on every reconnect or never printed at all. This asserts the two halves that a
// removal would break: the file is still spelled beside the SSE cursor, and a
// NEW process really does read it back and print only what it has not seen.
func TestReplyCardSeen_SurvivesTheChatLedgerRemoval(t *testing.T) {
	if got, want := replyCardSeenPath(Config{Home: "/h", ID: "M-Kyle"}),
		filepath.Join("/h", "m-kyle", "replycards-seen"); got != want {
		t.Fatalf("replyCardSeenPath = %q want %q — the reply-card ledger keeps its own "+
			"file beside the SSE cursor", got, want)
	}

	home := t.TempDir()
	cfg := Config{Token: "t", ID: "kyle", Home: home}

	status := 200
	list := `[` + answeredCardJSON("rc-a", 100, "old?") + `]`
	srv := drainListServer(t, &status, &list)
	cfg.Base = srv.URL
	path := replyCardSeenPath(cfg)
	var out bytes.Buffer

	// process #1: no ledger yet ⇒ prime silently.
	if n := drainReplyCards(srv.Client(), cfg, loadReplyCardSeen(path), &out); n != 0 || out.Len() != 0 {
		t.Fatalf("first run must baseline silently: n=%d out=%q", n, out.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the reply-card ledger must be on disk after the first drain: %v", err)
	}

	// process #2: a NEW answer landed while the agent was dead. A build that
	// stopped LOADING the ledger comes up unprimed here and prints nothing at
	// all — which is what this count catches.
	list = `[` + answeredCardJSON("rc-b", 200, "new?") + `,` + answeredCardJSON("rc-a", 100, "old?") + `]`
	if n := drainReplyCards(srv.Client(), cfg, loadReplyCardSeen(path), &out); n != 1 {
		t.Fatalf("a reloaded ledger must print exactly the card answered while this "+
			"member was down: n=%d out=%q", n, out.String())
	}
	if got := out.String(); !strings.Contains(got, "rc-b") || strings.Contains(got, "rc-a") {
		t.Fatalf("the reloaded ledger printed the wrong set; out = %q", got)
	}
}
