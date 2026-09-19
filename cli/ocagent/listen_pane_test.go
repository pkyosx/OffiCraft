package main

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// recordTmux records the argv of every tmux call delivery makes.
type recordTmux struct {
	calls [][]string
	fail  map[int]bool // call index → return an error
}

func (r *recordTmux) run(args ...string) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	if r.fail[len(r.calls)-1] {
		return errors.New("tmux refused")
	}
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
		if want := deliveryOf("officraft", "member-m1", line); !reflect.DeepEqual(rec.calls, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.calls, want)
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
			{"the reconnect", "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events", true},
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
				if !reflect.DeepEqual(rec.calls, want) {
					t.Errorf("tmux calls =\n%v\nwant\n%v", rec.calls, want)
				}
				// Swallowed or not, the listener's own log keeps every line — it is
				// the only place a swallowed one can still be read.
				if got := log.String(); got != tc.line+"\n" {
					t.Errorf("log = %q, want %q", got, tc.line+"\n")
				}
			})
		}
	})

	t.Run("lines that pile up while a delivery runs go out as ONE paste", func(t *testing.T) {
		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)

		// The wind-down and recycle hooks print the owner's 〈停止〉 document one
		// line per Write. Seventeen separate pastes would be seventeen turns on
		// the model, each one's Enter racing the next one's paste.
		lines := []string{"[ocagent] recycle: 你被收回了", "[ocagent] recycle: 1. 收尾", "[ocagent] recycle: 2. 回報"}
		for _, line := range lines {
			w.Write([]byte(line + "\n"))
		}
		w.drain()

		want := deliveryOf("officraft", "member-m1", strings.Join(lines, "\n"))
		if !reflect.DeepEqual(rec.calls, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.calls, want)
		}
	})

	t.Run("half a line waits for the rest of it", func(t *testing.T) {
		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)

		w.Write([]byte("[ocagent] chat #c-1 from "))
		w.drain()
		if len(rec.calls) != 0 {
			t.Fatalf("half a line was delivered: %v", rec.calls)
		}

		w.Write([]byte("Owner: 看一下這個\n"))
		w.drain()
		want := deliveryOf("officraft", "member-m1", "[ocagent] chat #c-1 from Owner: 看一下這個")
		if !reflect.DeepEqual(rec.calls, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.calls, want)
		}
	})

	t.Run("a paste the tmux on this host rejects is retried without the flags", func(t *testing.T) {
		var log bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{1: true}} // the -d -p paste
		w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})

		w.Write([]byte("[ocagent] chat #c-1\n"))
		w.drain()

		want := deliveryOf("officraft", "member-m1", "[ocagent] chat #c-1")
		// The bare retry is inserted after the rejected paste; everything else is
		// unchanged, so the expected argv is the normal delivery plus that one.
		want = append(want[:2:2],
			append([][]string{{"-L", "officraft", "paste-buffer", "-t", "member-m1", "-b", "oc-listen-deliver-member-m1"}},
				want[2:]...)...)
		if !reflect.DeepEqual(rec.calls, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.calls, want)
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

		bufA, bufB := recA.calls[0][4], recB.calls[0][4]
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

	t.Run("the pump delivers without anyone draining it, and stopping flushes the tail", func(t *testing.T) {
		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)
		stop := w.start()

		w.Write([]byte("[ocagent] listen: giving up — 30 attempts\n"))
		stop()

		want := deliveryOf("officraft", "member-m1", "[ocagent] listen: giving up — 30 attempts")
		if !reflect.DeepEqual(rec.calls, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.calls, want)
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
		if !reflect.DeepEqual(rec.calls, want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.calls, want)
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
		if len(rec.calls) != 0 {
			t.Errorf("tmux was used without --deliver-tmux: %v", rec.calls)
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
