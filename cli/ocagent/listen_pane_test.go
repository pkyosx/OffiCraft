package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		{"-L", socket, "copy-mode", "-q", "-t", session},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
		{"-L", socket, "copy-mode", "-q", "-t", session},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
		{"-L", socket, "copy-mode", "-q", "-t", session},
		{"-L", socket, "send-keys", "-t", session, "Enter"},
	}
}

// undeliveredOf is the argv a delivery of payload must produce when tmux refuses
// the paste at call index failAt (0 = set-buffer, 1 = paste-buffer): nothing more
// of the payload, then one typed id-only line and its Enters.
func undeliveredOf(socket, session, payload string, failAt int, notice string) [][]string {
	calls := deliveryOf(socket, session, payload)[:failAt+1]
	return append(calls,
		[]string{"-L", socket, "copy-mode", "-q", "-t", session},
		[]string{"-L", socket, "send-keys", "-t", session, "-l", notice},
		[]string{"-L", socket, "copy-mode", "-q", "-t", session},
		[]string{"-L", socket, "send-keys", "-t", session, "Enter"},
		[]string{"-L", socket, "copy-mode", "-q", "-t", session},
		[]string{"-L", socket, "send-keys", "-t", session, "Enter"},
		[]string{"-L", socket, "copy-mode", "-q", "-t", session},
		[]string{"-L", socket, "send-keys", "-t", session, "Enter"},
	)
}

// chatEvent renders a chat event the way the listener prints it.
func chatEvent(t *testing.T, id, body string) string {
	t.Helper()
	var out bytes.Buffer
	printChatLine(&out, map[string]any{"id": id, "from": "owner", "body": body}, 0)
	return strings.TrimSuffix(out.String(), "\n")
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
		// 🔴 EVERY CASE GETS A FRESH WRITER, so this table cannot see WHO SPENT THE
		// BOOT SWALLOW. The cases that need that are below, in
		// a_quoted_transport_line_does_not_spend_the_boot_swallow.
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

	t.Run("a quoted transport line does not spend the boot swallow", func(t *testing.T) {
		// Two writers, two lines each, because the question is about ORDER: the
		// quoted line arrives first, the real boot connect second. Members working
		// on this feature paste these exact strings at each other, and the quoted
		// copy reaches this writer INDENTED (renderMessageBody), so a comparison
		// that trims first mistakes it for this binary's own notice.
		//
		// Spending the swallow early is silent: the boot connect is then forwarded
		// into a pane that is still starting, which costs the member the turn the
		// swallow exists to save. Both of the comparisons that can spend it are
		// covered here — the connect one and the disconnect one.
		boot := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		for _, quotedNotice := range []string{
			boot,
			"[ocagent] listen: disconnected — dial tcp: connection refused",
		} {
			t.Run(quotedNotice, func(t *testing.T) {
				var log bytes.Buffer
				w, rec := newRecordingPaneWriter(&log)

				quoted := quotedInBody(t, quotedNotice)
				w.Write([]byte(quoted + "\n"))
				w.drain()

				// The quoted line is chat, so it goes out; the swallow is untouched.
				want := deliveryOf("officraft", "member-m1", quoted)
				if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
					t.Fatalf("the quoted line did not reach the member:\n%v", got)
				}

				w.Write([]byte(boot + "\n"))
				w.drain()
				if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
					t.Errorf("the boot connect was pasted into a booting pane: %v", got)
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

	t.Run("an event up to the paste ceiling goes out whole and one byte more goes out as its id", func(t *testing.T) {
		// Owner ruling rc-62ede5d63772: the ceiling counts every byte pasted,
		// the header included.
		header := "[ocagent] chat from owner (#c-1): "
		atCeiling := header + strings.Repeat("a", 8192-len(header))
		overCeiling := atCeiling + "a"

		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)
		w.Write([]byte(atCeiling + "\n"))
		w.drain()
		if want := deliveryOf("officraft", "member-m1", atCeiling); !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("an 8192-byte event was not pasted whole: %d call(s)", len(rec.snapshot()))
		}

		w, rec = newRecordingPaneWriter(&log)
		w.Write([]byte(overCeiling + "\n"))
		w.drain()
		notice := header + strings.Repeat("a", 166) +
			"… [這則通知約 1 行／8193 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"
		if want := deliveryOf("officraft", "member-m1", notice); !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("tmux calls =\n%q\nwant\n%q", rec.snapshot(), want)
		}
	})

	t.Run("a thousand-line message reaches the pane as one id-only line", func(t *testing.T) {
		var b strings.Builder
		for i := 0; i < 1194; i++ {
			fmt.Fprintf(&b, "line %04d xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", i)
		}
		event := chatEvent(t, "c-6feb08ebdbb6", b.String())

		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)
		w.Write([]byte(event + "\n"))
		w.drain()

		notice := "[ocagent] chat from owner (#c-6feb08ebdbb6): line 0000 xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" +
			" [這則通知約 1194 行／53770 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"
		if want := deliveryOf("officraft", "member-m1", notice); !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("tmux calls =\n%q\nwant\n%q", rec.snapshot(), want)
		}
		if got := log.String(); got != event+"\n" {
			t.Errorf("the listener's own log lost the full text: %d bytes, want %d", len(got), len(event)+1)
		}
	})

	t.Run("the notice keeps at most 200 characters of the first line, cut between characters", func(t *testing.T) {
		event := chatEvent(t, "c-2", strings.Repeat("界", 4000))

		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)
		w.Write([]byte(event + "\n"))
		w.drain()

		notice := "[ocagent] chat from owner (#c-2): " + strings.Repeat("界", 166) +
			"… [這則通知約 1 行／4034 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"
		if want := deliveryOf("officraft", "member-m1", notice); !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("tmux calls =\n%q\nwant\n%q", rec.snapshot(), want)
		}
	})

	t.Run("an oversized reply card or task points at its own read tool", func(t *testing.T) {
		big := strings.Repeat("b", 9000)
		for _, tc := range []struct{ event, notice string }{
			{"[ocagent] reply-card rc-1 answered: " + big,
				"[ocagent] reply-card rc-1 answered: " + strings.Repeat("b", 164) +
					"… [這則通知約 1 行／9036 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_reply_card 讀全文]"},
			{"[ocagent] task T-1 " + big,
				"[ocagent] task T-1 " + strings.Repeat("b", 181) +
					"… [這則通知約 1 行／9019 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_task 讀全文]"},
		} {
			var log bytes.Buffer
			w, rec := newRecordingPaneWriter(&log)
			w.Write([]byte(tc.event + "\n"))
			w.drain()
			if want := deliveryOf("officraft", "member-m1", tc.notice); !reflect.DeepEqual(rec.snapshot(), want) {
				t.Errorf("tmux calls =\n%q\nwant\n%q", rec.snapshot(), want)
			}
		}
	})

	t.Run("two events that fill the ceiling only without their joining newline go out as two pastes", func(t *testing.T) {
		first := "[ocagent] chat from owner (#c-1): " + strings.Repeat("a", 4096-34)
		second := "[ocagent] chat from owner (#c-2): " + strings.Repeat("b", 4096-34)

		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)
		w.Write([]byte(first + "\n" + second + "\n"))
		w.drain()

		want := append(deliveryOf("officraft", "member-m1", first), deliveryOf("officraft", "member-m1", second)...)
		if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("got %d call(s), want %d: 8193 bytes were pasted at once", len(got), len(want))
		}
	})

	t.Run("queued events that together pass the ceiling are split between events, never inside one", func(t *testing.T) {
		first := chatEvent(t, "c-1", "甲\n"+strings.Repeat("a", 5000))
		second := chatEvent(t, "c-2", "乙\n"+strings.Repeat("b", 5000))
		third := "[ocagent] chat from owner (#c-3): 短的"

		var log bytes.Buffer
		w, rec := newRecordingPaneWriter(&log)
		w.Write([]byte(first + "\n" + second + "\n" + third + "\n"))
		w.drain()

		want := append(deliveryOf("officraft", "member-m1", first),
			deliveryOf("officraft", "member-m1", second+"\n"+third)...)
		if got := rec.snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("got %d call(s), want %d: the batch was not split at the event boundary", len(got), len(want))
		}
	})

	t.Run("a refused paste types one id-only line and none of the text", func(t *testing.T) {
		events := []string{
			"[ocagent] chat from owner (#c-1, ↩#c-0, 3s ago): 第一則",
			"[ocagent] reply-card rc-2 answered: 好",
			"[ocagent] task T-3 changed",
			"[ocagent] signal context-high: 80%\n    內文提到 T-99 與 rc-98",
		}
		payload := strings.Join(events, "\n")
		notice := "[ocagent] listen: 4 event(s) could not be pasted into this pane (#c-1, rc-2, T-3, no id)" +
			" - none of their text was typed, read them with get_chat / get_reply_card / get_task"
		for _, tc := range []struct {
			name   string
			failAt int
		}{
			{"set-buffer refused", 0},
			{"paste-buffer refused", 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var log bytes.Buffer
				rec := &recordTmux{fail: map[int]bool{tc.failAt: true}}
				w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})
				w.Write([]byte(payload + "\n"))
				w.drain()

				if want := undeliveredOf("officraft", "member-m1", payload, tc.failAt, notice); !reflect.DeepEqual(rec.snapshot(), want) {
					t.Errorf("tmux calls =\n%q\nwant\n%q", rec.snapshot(), want)
				}
				wantLog := payload + "\n" +
					"[ocagent] listen: tmux refused a paste into member-m1 (tmux refused); typing an id-only notice instead\n"
				if got := log.String(); got != wantLog {
					t.Errorf("log = %q, want %q", got, wantLog)
				}
			})
		}
	})

	t.Run("a pane that refuses the id-only line too gets no Enter and the log keeps the line", func(t *testing.T) {
		var log bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{1: true, 3: true}} // the paste, then the typed notice
		w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})
		line := "[ocagent] chat from owner (#c-1): 在嗎"
		w.Write([]byte(line + "\n"))
		w.drain()

		notice := "[ocagent] listen: 1 event(s) could not be pasted into this pane (#c-1)" +
			" - none of their text was typed, read them with get_chat / get_reply_card / get_task"
		if want := undeliveredOf("officraft", "member-m1", line, 1, notice)[:4]; !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("tmux calls =\n%q\nwant\n%q", rec.snapshot(), want)
		}
		wantLog := line + "\n" +
			"[ocagent] listen: tmux refused a paste into member-m1 (tmux refused); typing an id-only notice instead\n" +
			"[ocagent] listen: the id-only notice did not reach member-m1 either (tmux refused): " + notice + "\n"
		if got := log.String(); got != wantLog {
			t.Errorf("log = %q, want %q", got, wantLog)
		}
	})

	t.Run("the listener's own log is written from two goroutines", func(t *testing.T) {
		// 🔴 THIS CASE EXISTS BECAUSE -race COULD NOT SEE THE BUG. inner is written
		// by the SSE scan loop (Write) and by the PUMP (the refused-paste note), and
		// no other case in this file makes those two touch it without ordering — so
		// moving that write back outside the lock stayed green under
		// `-race -count=3`, measured. A throwaway probe reported the race the
		// moment the two were made to collide; this builds that collision.
		var log bytes.Buffer
		rec := &recordTmux{
			fail:  map[int]bool{1: true},
			hold:  make(chan struct{}),
			held:  make(chan struct{}),
			holdN: 1,
		}
		w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})
		stop := w.start()

		w.Write([]byte("[ocagent] chat #c-1\n"))
		rec.awaitHeld(t)

		// Bounded on purpose: an unbounded writer would keep refilling the queue
		// and stop()'s drain would never return.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 64; i++ {
				w.Write([]byte("[ocagent] chat #c-x\n"))
			}
		}()
		close(rec.hold) // the held paste now fails ⇒ the pump writes its note
		<-done
		stop()

		if got := log.String(); !strings.Contains(got, "tmux refused a paste into member-m1") {
			t.Errorf("the refused-paste note never reached the log:\n%s", got)
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

	t.Run("a real pane that asks for bracketed paste receives each message as ONE paste", func(t *testing.T) {
		// The receiver turns bracketed paste on the way a TUI does: tmux adds the
		// markers only for a program that asked, and with them a multi-line paste
		// is one input rather than one per line.
		bin := resolveTmuxBin()
		if bin == "" {
			t.Skip("tmux not installed")
		}
		var under strings.Builder
		for i := 0; under.Len() < 6400; i++ {
			fmt.Fprintf(&under, "第 %03d 行 xxxxxxxxxx\n", i)
		}
		underEvent := chatEvent(t, "c-1", under.String())
		if len(underEvent) > 8192 || strings.Count(underEvent, "\n") < 100 {
			t.Fatalf("the near-ceiling case is %d bytes / %d lines — it would test nothing", len(underEvent), strings.Count(underEvent, "\n"))
		}
		var over strings.Builder
		for i := 0; i < 1194; i++ {
			fmt.Fprintf(&over, "line %04d xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", i)
		}
		overEvent := chatEvent(t, "c-2", over.String())
		overNotice := "[ocagent] chat from owner (#c-2): line 0000 xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" +
			" [這則通知約 1194 行／53759 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"

		for _, tc := range []struct {
			name, event, pasted string
		}{
			{"a message just under the ceiling is pasted whole", underEvent, underEvent},
			{"a thousand-line message is pasted as its id-only line", overEvent, overNotice},
		} {
			t.Run(tc.name, func(t *testing.T) {
				socket := fmt.Sprintf("oc-test-%d-%d", os.Getpid(), time.Now().UnixNano())
				dir := t.TempDir()
				out, ready := filepath.Join(dir, "received.bin"), filepath.Join(dir, "ready")
				tmux := func(args ...string) string {
					t.Helper()
					got, err := exec.Command(bin, append([]string{"-L", socket, "-f", "/dev/null"}, args...)...).CombinedOutput()
					if err != nil {
						t.Fatalf("tmux %v: %v: %s", args, err, got)
					}
					return strings.TrimSpace(string(got))
				}
				tmux("new-session", "-d", "-s", "member-m1",
					fmt.Sprintf("stty raw -echo; printf '\\033[?2004h'; touch '%s'; exec cat > '%s'", ready, out))
				socketPath := tmux("display-message", "-p", "#{socket_path}")
				t.Cleanup(func() {
					_ = exec.Command(bin, "-L", socket, "kill-server").Run()
					_ = os.Remove(socketPath)
				})
				deadline := time.Now().Add(3 * time.Second)
				for _, err := os.Stat(ready); err != nil; _, err = os.Stat(ready) {
					if time.Now().After(deadline) {
						t.Fatal("the receiver never started")
					}
					time.Sleep(20 * time.Millisecond)
				}
				// tmux has no format for "this pane asked for bracketed paste", so the
				// request written just before the ready file gets a moment to be read.
				time.Sleep(300 * time.Millisecond)

				w := newPaneWriter(io.Discard, socket, "member-m1", nil, func(time.Duration) {})
				w.Write([]byte(tc.event + "\n"))
				w.drain()

				want := "\x1b[200~" + strings.ReplaceAll(tc.pasted, "\n", "\r") + "\x1b[201~\r\r\r"
				var got []byte
				for deadline = time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
					if got, _ = os.ReadFile(out); string(got) == want {
						return
					}
				}
				t.Errorf("the pane received %d bytes, %d paste(s), want %d bytes in one paste then three Enters\ngot  %.300q\nwant %.300q",
					len(got), strings.Count(string(got), "\x1b[200~"), len(want), got, want)
			})
		}
	})

	t.Run("an event is submitted into a real pane whatever mode someone left it in", func(t *testing.T) {
		bin := resolveTmuxBin()
		if bin == "" {
			t.Skip("tmux not installed")
		}
		for _, tc := range []struct {
			name  string
			enter []string
		}{
			{"no mode", nil},
			{"copy-mode", []string{"copy-mode"}},
			{"view-mode", []string{"run-shell", "echo 一段指令輸出"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				socket := fmt.Sprintf("oc-test-%d-%d", os.Getpid(), time.Now().UnixNano())
				out := filepath.Join(t.TempDir(), "received.txt")
				tmux := func(args ...string) string {
					t.Helper()
					got, err := exec.Command(bin, append([]string{"-L", socket, "-f", "/dev/null"}, args...)...).CombinedOutput()
					if err != nil {
						t.Fatalf("tmux %v: %v: %s", args, err, got)
					}
					return strings.TrimSpace(string(got))
				}
				tmux("new-session", "-d", "-s", "member-m1", fmt.Sprintf("cat >> '%s'", out))
				socketPath := tmux("display-message", "-p", "#{socket_path}")
				t.Cleanup(func() {
					_ = exec.Command(bin, "-L", socket, "kill-server").Run()
					_ = os.Remove(socketPath)
				})
				// With vi mode-keys (tmux picks them from EDITOR/VISUAL) Enter itself
				// leaves copy-mode, and the copy-mode case would pass without the fix.
				tmux("set-option", "-g", "mode-keys", "emacs")
				if len(tc.enter) > 0 {
					tmux(append([]string{tc.enter[0], "-t", "member-m1"}, tc.enter[1:]...)...)
				}
				if len(tc.enter) > 0 && tmux("display-message", "-p", "-t", "member-m1", "#{pane_in_mode}") == "0" {
					t.Fatal("the pane never entered a mode — this case would test nothing")
				}

				w := newPaneWriter(io.Discard, socket, "member-m1", nil, func(time.Duration) {})
				line := "[ocagent] chat #c-1 from Owner: 進度？"
				w.Write([]byte(line + "\n"))
				w.drain()

				deadline := time.Now().Add(2 * time.Second)
				var got []byte
				for time.Now().Before(deadline) {
					got, _ = os.ReadFile(out)
					if bytes.Contains(got, []byte(line+"\n")) {
						return
					}
					time.Sleep(20 * time.Millisecond)
				}
				t.Errorf("the pane's program received %q, want it to contain %q", got, line+"\n")
			})
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

		rc := cmdListen([]string{"--deliver-tmux"}, Config{}, testEnv(map[string]string{}), &out, io.Discard, start, nil)

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
		// runListen the original writer. Every other test stays green while
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

		rc := cmdListen([]string{"--once", "--deliver-tmux"}, Config{},
			testEnv(map[string]string{"OC_SESSION": "member-m1", "OC_TMUX_SOCKET": "lab"}), &out, io.Discard, start, rec.run)

		if rc != 7 {
			t.Errorf("rc = %d, want the run's own answer 7", rc)
		}
		want := deliveryOf("lab", "member-m1", "[ocagent] chat #c-9 from Owner: 看一下")
		if !reflect.DeepEqual(rec.snapshot(), want) {
			t.Errorf("tmux calls =\n%v\nwant\n%v", rec.snapshot(), want)
		}
	})

	t.Run("without the flag the run writes to the caller's own writer and touches no tmux", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{}}
		start := func(_ Config, _ func(string) string, _ bool, sink io.Writer) int {
			io.WriteString(sink, "[ocagent] chat #c-9\n")
			return 0
		}

		rc := cmdListen(nil, Config{}, testEnv(map[string]string{"OC_SESSION": "member-m1"}), &out, &errOut, start, rec.run)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if got := out.String(); got != "[ocagent] chat #c-9\n" {
			t.Errorf("out = %q, want the line itself", got)
		}
		if len(rec.snapshot()) != 0 {
			t.Errorf("tmux was used without --deliver-tmux: %v", rec.snapshot())
		}
		if errOut.String() != "" {
			t.Errorf("errOut = %q, want nothing", errOut.String())
		}
	})

	t.Run("an unknown flag is refused", func(t *testing.T) {
		var out bytes.Buffer
		started := false
		start := func(Config, func(string) string, bool, io.Writer) int { started = true; return 0 }

		if rc := cmdListen([]string{"--nope"}, Config{}, testEnv(nil), &out, io.Discard, start, nil); rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		if started {
			t.Error("the listener was started on a flag parse error")
		}
	})

	modEnv := map[string]string{
		"OC_SESSION": "member-m1", "OC_LISTEN_ACK": "1", "OC_LISTEN_ACK_FILE": "/w/m1/.officraft-listen-ack",
	}
	without := func(key string) map[string]string {
		env := map[string]string{}
		for k, v := range modEnv {
			if k != key {
				env[k] = v
			}
		}
		return env
	}

	t.Run("--deliver-mod refuses to start without what it needs, saying so on stderr", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			argv    []string
			env     map[string]string
			refusal string
		}{
			{"no session", []string{"--deliver-mod"}, without("OC_SESSION"),
				"[ocagent] listen: --deliver-mod needs OC_SESSION (the session to deliver into, and the session " +
					"this listener must die with); refusing to start.\n"},
			{"no ack file", []string{"--deliver-mod"}, without("OC_LISTEN_ACK_FILE"),
				"[ocagent] listen: --deliver-mod needs OC_LISTEN_ACK=1 and OC_LISTEN_ACK_FILE (the file the mod " +
					"answers each batch in); refusing to start.\n"},
			{"acks not asked for", []string{"--deliver-mod"}, without("OC_LISTEN_ACK"),
				"[ocagent] listen: --deliver-mod needs OC_LISTEN_ACK=1 and OC_LISTEN_ACK_FILE (the file the mod " +
					"answers each batch in); refusing to start.\n"},
			{"both routes at once", []string{"--deliver-mod", "--deliver-tmux"}, modEnv,
				"[ocagent] listen: --deliver-tmux and --deliver-mod are two routes into the same member; " +
					"pick one. Refusing to start.\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var out, errOut bytes.Buffer
				rec := &recordTmux{fail: map[int]bool{}}
				started := false
				start := func(Config, func(string) string, bool, io.Writer) int { started = true; return 0 }

				rc := cmdListen(tc.argv, Config{}, testEnv(tc.env), &out, &errOut, start, rec.run)

				if rc != 2 {
					t.Errorf("rc = %d, want 2", rc)
				}
				if started {
					t.Error("the listener was started anyway")
				}
				if errOut.String() != tc.refusal {
					t.Errorf("errOut = %q, want %q", errOut.String(), tc.refusal)
				}
				if out.String() != "" || len(rec.snapshot()) != 0 {
					t.Errorf("out = %q, tmux calls = %v; want neither", out.String(), rec.snapshot())
				}
			})
		}
	})

	t.Run("--deliver-mod turns what the run prints into frames on stdout and diagnostics on stderr", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rec := &recordTmux{fail: map[int]bool{}}
		start := func(_ Config, _ func(string) string, once bool, sink io.Writer) int {
			if !once {
				t.Error("--once was not carried through")
			}
			io.WriteString(sink, "[ocagent] listen: retrying in 4s\n")
			io.WriteString(sink, "[ocagent] chat #c-9 from Owner: 看一下\n")
			return 7
		}

		rc := cmdListen([]string{"--once", "--deliver-mod"}, Config{}, testEnv(modEnv), &out, &errOut, start, rec.run)

		if rc != 7 {
			t.Errorf("rc = %d, want the run's own answer 7", rc)
		}
		if want := `{"submit":"[ocagent] chat #c-9 from Owner: 看一下"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
		if want := "[ocagent] listen: retrying in 4s\n"; errOut.String() != want {
			t.Errorf("errOut = %q, want %q", errOut.String(), want)
		}
		if len(rec.snapshot()) != 0 {
			t.Errorf("tmux was used under --deliver-mod: %v", rec.snapshot())
		}
	})
}
