package main

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordTmux records the argv of every tmux call delivery makes. It is safe for
// the pump goroutine to write while a test reads, and a test can hold ONE call
// open (hold/held) so it can queue more lines while a delivery is in flight.
type recordTmux struct {
	mu    sync.Mutex
	calls [][]string
	fail  map[int]bool // call index → return an error

	hold  chan struct{} // closed by the test to release the held call
	held  chan struct{} // closed once the held call has been entered
	holdN int           // which call index to hold
}

func (r *recordTmux) run(args ...string) error {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string(nil), args...))
	n := len(r.calls) - 1
	fail := r.fail[n]
	r.mu.Unlock()
	if r.hold != nil && n == r.holdN {
		close(r.held)
		<-r.hold
	}
	if fail {
		return errors.New("tmux refused")
	}
	return nil
}

func (r *recordTmux) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

// awaitHeld waits for the held call to be entered, with a deadline: a writer
// that never wakes its pump would otherwise leave the test blocked here until
// the whole binary times out, which reads as CI being slow rather than as this
// guard firing.
func (r *recordTmux) awaitHeld(t *testing.T) {
	t.Helper()
	select {
	case <-r.held:
	case <-time.After(2 * time.Second):
		t.Fatal("no delivery ever started — the writer never woke its pump")
	}
}

// awaitCalls waits until at least n calls have landed, so a test can observe the
// pump WITHOUT stopping it — the difference between "delivery happens" and
// "delivery happens only because stopping flushes".
func (r *recordTmux) awaitCalls(t *testing.T, n int) [][]string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := r.snapshot(); len(got) >= n {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("only %d tmux call(s) landed, want at least %d", len(r.snapshot()), n)
	return nil
}

// deliveryOf is the complete argv a delivery of payload must produce — typed out
// rather than built from the constants it is checking, so a change to
// paneEnterAttempts has to be re-justified against a literal instead of agreeing
// with itself. Independent review dropped it from 3 to 0 (every event pasted and
// never submitted) against a loop-built expectation, with the package green.
func deliveryOf(socket, session, payload string) [][]string {
	buffer := "oc-listen-deliver-" + session
	return [][]string{
		{"-L", socket, "set-buffer", "-b", buffer, payload},
		{"-L", socket, "paste-buffer", "-t", session, "-b", buffer, "-d", "-p"},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
	}
}

// degradedDeliveryOf is the argv one line must produce on the path this host's
// tmux takes when it rejects the flags: its own set-buffer, a bare paste, and
// its own Enters. Typed out rather than derived from deliveryOf, so a change to
// either path has to be re-justified against a literal.
func degradedDeliveryOf(socket, session, line string) [][]string {
	buffer := "oc-listen-deliver-" + session
	return [][]string{
		{"-L", socket, "set-buffer", "-b", buffer, line},
		{"-L", socket, "paste-buffer", "-t", session, "-b", buffer},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
	}
}

// quotedInBody is the CONTINUATION line of a chat body that quotes a transport
// line, produced by renderMessageBody — the renderer the real path uses. The
// indentation is what these cases turn on, and typed as a string literal it was
// four spaces nobody could see: anything that tidied the literal would have
// retired the case into a duplicate of "other transport chatter" and left the
// package green.
func quotedInBody(t *testing.T, transportLine string) string {
	t.Helper()
	lines := strings.Split(renderMessageBody("一位成員貼給另一位：\n"+transportLine, "get_chat"), "\n")
	if len(lines) != 2 {
		t.Fatalf("renderMessageBody produced %d lines, want 2: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[1], " ") {
		t.Fatalf("the continuation line is not indented (%q) — this case would test nothing", lines[1])
	}
	return lines[1]
}

func newRecordingPaneWriter(inner io.Writer) (*paneWriter, *recordTmux) {
	rec := &recordTmux{fail: map[int]bool{}}
	return newPaneWriter(inner, "officraft", "member-m1", rec.run, func(time.Duration) {}), rec
}

func TestPaneWriter(t *testing.T) {
	t.Run("an event reaches the member's pane, and the listener's own log keeps it too", func(t *testing.T) {
		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)

		line := "[ocagent] chat #c-1 from Owner: 看一下這個"
		n, err := w.Write([]byte(line + "\n"))
		if err != nil || n != len(line)+1 {
			t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(line)+1)
		}
		w.drain()

		if got := log.String(); got != line+"\n" {
			t.Errorf("log = %q, want %q", got, line+"\n")
		}
		if want := deliveryOf("officraft", "member-m1", line); !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.snapshot(), want)
		}
	})

	t.Run("transport chatter is swallowed while the notices the ruling names are not", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			line      string
			delivered bool
		}{
			{"a line this binary did not write", "something else entirely", true},
			{"the first disconnect", "[ocagent] listen: disconnected — dial tcp: connection refused", true},
			{"a chat body that quotes a transport line", quotedInBody(t, "[ocagent] listen: batch tok-1"), true},
			// The connect notice is matched by a SECOND prefix comparison
			// (shouldForward), which the column-0 rule has to hold for too. Trim
			// first and this line is swallowed AND spends the boot swallow, so the real
			// boot connect is forwarded instead — the opposite symptom, harder to
			// recognise, and the whole package stays green without this case.
			{"a chat body that quotes the connect notice", quotedInBody(t, "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"), true},
			{"giving up", "[ocagent] listen: giving up — 30 attempts", true},
			{"the end-of-batch marker, which is protocol", "[ocagent] listen: batch tok-1 [ts=1 local]", false},
			{"other transport chatter", "[ocagent] listen: retrying in 4s", false},
			{"a blank line", "   ", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var log bytes.Buffer
				w, rec := newRecordingPaneWriter(&log)
				w.Write([]byte(tc.line + "\n"))
				w.drain()

				var want [][]string
				if tc.delivered {
					want = deliveryOf("officraft", "member-m1", tc.line)
				}
				if !reflect.DeepEqual(rec.snapshot(), want) {
					t.Errorf("tmux calls =\n%v\nwant\n%v", rec.snapshot(), want)
				}
				// Swallowed or not, the listener's own log keeps every line — it is
				// the only place a swallowed one can still be read.
				if got := log.String(); got != tc.line+"\n" {
					t.Errorf("log = %q, want %q", got, tc.line+"\n")
				}
			})
		}
	})

	t.Run("the boot connect is swallowed and every later one reaches the member", func(t *testing.T) {
		// That line lands while the member is still running its boot turn, so
		// forwarding it spends a turn on a transport notice nobody asked for,
		// once per member per boot. A LATER connect means the stream came back,
		// which the owner's notice ruling says must reach the member.
		//
		// It is also the half that stops an OVERSHOOT ("never swallow at all"):
		// a_connect_that_answers_a_forwarded_disconnect passes under that mutant,
		// so the two are only a two-way guard while BOTH are alive.
		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)

		// Members working on this feature quote the connect notice at each other,
		// and that line is chat, not this binary's output. It is forwarded — and it
		// must NOT spend the boot swallow, or the real boot connect below is
		// forwarded and costs the member the turn this whole case exists to save.
		quoted := quotedInBody(t, "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events")
		w.Write([]byte(quoted + "\n"))
		w.drain()
		beforeBoot := deliveryOf("officraft", "member-m1", quoted)
		if got := rec.snapshot(); !reflect.DeepEqual(got, beforeBoot) {
			t.Fatalf("the quoted connect did not reach the member:\n%v", got)
		}

		boot := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		w.Write([]byte(boot + "\n"))
		w.drain()
		if got := rec.snapshot(); !reflect.DeepEqual(got, beforeBoot) {
			t.Fatalf("the boot connect was pasted into a booting pane: %v", got)
		}

		back := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [same station]"
		again := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [new station — was c67268a4]"
		w.Write([]byte(back + "\n" + again + "\n"))
		w.drain()
		want := append(beforeBoot, deliveryOf("officraft", "member-m1", back+"\n"+again)...)
		if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", got, want)
		}

		// The swallowed boot connect still has to be readable somewhere, and the
		// listener's own log is the only place it can be.
		if wantLog := quoted + "\n" + boot + "\n" + back + "\n" + again + "\n"; log.String() != wantLog {
			t.Errorf("log = %q, want %q", log.String(), wantLog)
		}
	})

	t.Run("a connect that answers a forwarded disconnect is not swallowed", func(t *testing.T) {
		// When the FIRST dial fails the member is told so, and that line ends by
		// promising 「the next transport line you see is either the reconnect or a
		// give-up」 (listen_run.go). Swallowing the reconnect because it happens to
		// be the first one this process printed leaves the member waiting for an
		// answer that was printed and thrown away — silence, which is what the
		// owner's notice ruling exists to prevent.
		//
		// 🔴 THIS CASE IS HALF A GUARD. "Never swallow anything" passes it too —
		// the_boot_connect_is_swallowed is what refuses that, so neither may be
		// retired as a duplicate of the other.
		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)

		// Built from the constants the production line is built from: a hand-typed
		// copy would keep passing after the real wording moved out from under it.
		down := agentLinePrefix + noticeDisconnected + " — dial tcp: connection refused" +
			" (retrying on the same schedule, quietly; the next transport line you see" +
			" is either the reconnect or a give-up)"
		w.Write([]byte(down + "\n"))
		w.drain()
		if want := deliveryOf("officraft", "member-m1", down); !reflect.DeepEqual(rec.snapshot(), want) {
			t.Fatalf("the disconnect itself did not reach the member:\n%v", rec.snapshot())
		}

		up := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		w.Write([]byte(up + "\n"))
		w.drain()
		want := append(deliveryOf("officraft", "member-m1", down),
			deliveryOf("officraft", "member-m1", up)...)
		if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", got, want)
		}
	})

	t.Run("lines that pile up while a delivery runs go out as ONE paste", func(t *testing.T) {
		// The wind-down and recycle hooks print the owner's 〈停止〉 document ONE
		// LINE PER WRITE, so the lines really do arrive while the previous one is
		// still being pasted. Seventeen separate pastes would be seventeen turns
		// on the model, each one's Enter racing the next one's paste.
		//
		// The condition has to be BUILT, not assumed: the first delivery is held
		// open inside tmux while the rest of the document is written.
		var log bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{}, hold: make(chan struct{}), held: make(chan struct{})}
		w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})
		stop := w.start()

		first := "[ocagent] recycle: 你被收回了"
		rest := []string{"[ocagent] recycle: 1. 收尾", "[ocagent] recycle: 2. 回報"}
		w.Write([]byte(first + "\n"))
		rec.awaitHeld(t) // the pump is now inside the first delivery
		for _, line := range rest {
			w.Write([]byte(line + "\n"))
		}
		close(rec.hold)
		stop()

		want := append(deliveryOf("officraft", "member-m1", first),
			deliveryOf("officraft", "member-m1", strings.Join(rest, "\n"))...)
		if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", got, want)
		}
	})

	t.Run("half a line waits for the rest of it", func(t *testing.T) {
		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)

		w.Write([]byte("[ocagent] chat #c-1 from "))
		w.drain()
		if len(rec.snapshot()) != 0 {
			t.Fatalf("half a line was delivered: %v", rec.snapshot())
		}

		w.Write([]byte("Owner: 看一下這個\n"))
		w.drain()
		want := deliveryOf("officraft", "member-m1", "[ocagent] chat #c-1 from Owner: 看一下這個")
		if !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.snapshot(), want)
		}
	})

	t.Run("a paste the tmux on this host rejects is re-sent one line at a time", func(t *testing.T) {
		// Measured on tmux 3.6b: the bare paste turns every newline in the buffer
		// into Enter. Re-sending a BATCH that way would submit one turn per line
		// with three stray Enters between them; re-sending line by line is the
		// pre-batch behaviour, which is the worst this path may degrade to.
		var log bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{1: true}} // the -d -p paste
		w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})

		lines := []string{"[ocagent] chat #c-1", "[ocagent] chat #c-2", "[ocagent] chat #c-3"}
		w.Write([]byte(strings.Join(lines, "\n") + "\n"))
		w.drain()

		// The rejected batch paste, then one complete delivery per line.
		want := deliveryOf("officraft", "member-m1", strings.Join(lines, "\n"))[:2]
		for _, line := range lines {
			want = append(want, degradedDeliveryOf("officraft", "member-m1", line)...)
		}
		if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", got, want)
		}
	})

	t.Run("a degraded delivery into a pane that is gone stops after the first line", func(t *testing.T) {
		// The fallback fires on ANY paste error, and a target that no longer exists
		// fails every one. Walking the whole batch would spend three paced Enters
		// per line while stop() waits for the drain.
		var log bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{1: true, 3: true}} // the -d -p paste, then the first bare one
		w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})

		lines := []string{"[ocagent] chat #c-1", "[ocagent] chat #c-2", "[ocagent] chat #c-3"}
		w.Write([]byte(strings.Join(lines, "\n") + "\n"))
		w.drain()

		// The rejected batch paste, then one set-buffer + one failed paste, and
		// nothing after it: no Enter, no second line.
		//
		// 🔴 HALF A GUARD, same shape as above: "always stop after the first line"
		// passes this too, and a_paste_the_tmux_on_this_host_rejects is what
		// refuses that.
		want := deliveryOf("officraft", "member-m1", strings.Join(lines, "\n"))[:2]
		want = append(want, degradedDeliveryOf("officraft", "member-m1", lines[0])[:2]...)
		if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", got, want)
		}
	})

	t.Run("two members on one socket do not share a buffer", func(t *testing.T) {
		// tmux buffer names live on the SERVER, one per -L socket, and every
		// member on a machine shares `officraft`. With one shared name, member
		// B's set-buffer overwrites A's and the first paste's -d deletes it:
		// measured with real tmux, A's pane received B's line and B received
		// nothing.
		var logA, logB bytes.Buffer
		recA := &recordTmux{fail: map[int]bool{}}
		recB := &recordTmux{fail: map[int]bool{}}
		a := newPaneWriter(&logA, "officraft", "member-a", recA.run, func(time.Duration) {})
		b := newPaneWriter(&logB, "officraft", "member-b", recB.run, func(time.Duration) {})

		a.Write([]byte("[ocagent] for A\n"))
		b.Write([]byte("[ocagent] for B\n"))
		a.drain()
		b.drain()

		bufA, bufB := recA.snapshot()[0][4], recB.snapshot()[0][4]
		if bufA == bufB {
			t.Fatalf("both members wrote the buffer %q — one overwrites the other", bufA)
		}
		if want := "oc-listen-deliver-member-a"; bufA != want {
			t.Errorf("A's buffer = %q, want %q", bufA, want)
		}
		if want := "oc-listen-deliver-member-b"; bufB != want {
			t.Errorf("B's buffer = %q, want %q", bufB, want)
		}
	})

	t.Run("a write reaches the pane with nobody stopping or draining the writer", func(t *testing.T) {
		// 🔴 THE OBSERVATION HAS TO HAPPEN BEFORE stop(). Stopping flushes, so a
		// test that writes, stops, and only then looks is green even when Write
		// never wakes the pump at all — and a listener that only delivers at
		// process exit is a member that hears nothing all day.
		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)
		stop := w.start()
		defer stop()

		line := "[ocagent] chat #c-1 from Owner: 看一下這個"
		w.Write([]byte(line + "\n"))

		want := deliveryOf("officraft", "member-m1", line)
		if got := rec.awaitCalls(t, len(want)); !reflect.DeepEqual(got, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", got, want)
		}
	})

	t.Run("stopping flushes what is still queued", func(t *testing.T) {
		// The last thing this listener says is usually the give-up line, and a
		// member told nothing cannot tell 還在重試 from 已經放棄. The queued line
		// is put in while the pump is held inside an earlier delivery, so it
		// cannot already be out by the time stop is called.
		var log bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{}, hold: make(chan struct{}), held: make(chan struct{})}
		w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})
		stop := w.start()

		w.Write([]byte("[ocagent] listen: disconnected — connection refused\n"))
		rec.awaitHeld(t)
		w.Write([]byte("[ocagent] listen: giving up — 30 attempts\n"))
		close(rec.hold)
		stop()

		want := append(deliveryOf("officraft", "member-m1", "[ocagent] listen: disconnected — connection refused"),
			deliveryOf("officraft", "member-m1", "[ocagent] listen: giving up — 30 attempts")...)
		if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", got, want)
		}
	})
}

func TestRunListen(t *testing.T) {
	t.Run("--deliver-tmux with no session refuses to start and runs nothing", func(t *testing.T) {
		// 🔴 With no OC_SESSION this listener has nowhere to deliver AND its
		// self-exit probe is disabled, so it would hold the SSE open on behalf of
		// a member that no longer exists and the station would never recycle it.
		var out bytes.Buffer
		started := false
		start := func(Config, func(string) string, bool, io.Writer) int { started = true; return 0 }

		rc := runListen([]string{"--deliver-tmux"}, Config{}, testEnv(map[string]string{}), &out, start, nil)

		if rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		if started {
			t.Error("the listener was started anyway")
		}
		if !strings.Contains(out.String(), "OC_SESSION") {
			t.Errorf("the refusal must name what is missing, got %q", out.String())
		}
	})

	t.Run("--deliver-tmux sends what the run prints into the member's pane", func(t *testing.T) {
		// The mutant this exists for: ignore listenSink's answers and hand
		// cmdListen the original writer. Every other test stays green while
		// --deliver-tmux becomes a no-op — the member hears nothing and the
		// station still reads it as healthy, because the SSE is still held.
		var out bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{}}
		start := func(_ Config, _ func(string) string, once bool, sink io.Writer) int {
			if !once {
				t.Error("--once was not carried through")
			}
			io.WriteString(sink, "[ocagent] chat #c-9 from Owner: 看一下\n")
			return 7
		}

		rc := runListen([]string{"--once", "--deliver-tmux"}, Config{},
			testEnv(map[string]string{"OC_SESSION": "member-m1", "OC_TMUX_SOCKET": "lab"}), &out, start, rec.run)

		if rc != 7 {
			t.Errorf("rc = %d, want the run's own answer 7", rc)
		}
		want := deliveryOf("lab", "member-m1", "[ocagent] chat #c-9 from Owner: 看一下")
		if !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.snapshot(), want)
		}
	})

	t.Run("without the flag the run writes to the caller's own writer and touches no tmux", func(t *testing.T) {
		var out bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{}}
		start := func(_ Config, _ func(string) string, _ bool, sink io.Writer) int {
			io.WriteString(sink, "[ocagent] chat #c-9\n")
			return 0
		}

		rc := runListen(nil, Config{}, testEnv(map[string]string{"OC_SESSION": "member-m1"}), &out, start, rec.run)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if got := out.String(); got != "[ocagent] chat #c-9\n" {
			t.Errorf("out = %q, want the line itself", got)
		}
		if len(rec.snapshot()) != 0 {
			t.Errorf("tmux was used without --deliver-tmux: %v", rec.snapshot())
		}
	})

	t.Run("an unknown flag is refused", func(t *testing.T) {
		var out bytes.Buffer
		started := false
		start := func(Config, func(string) string, bool, io.Writer) int { started = true; return 0 }

		if rc := runListen([]string{"--nope"}, Config{}, testEnv(nil), &out, start, nil); rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		if started {
			t.Error("the listener was started on a flag parse error")
		}
	})
}
