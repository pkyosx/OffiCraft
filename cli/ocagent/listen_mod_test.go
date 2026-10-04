package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer lets a test read what the pump goroutine writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestModWriter(t *testing.T) {
	t.Run("an event becomes one submit frame and nothing reaches stderr", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		line := "[ocagent] chat #c-1 from Owner: 看一下這個"
		n, err := w.Write([]byte(line + "\n"))
		if err != nil || n != len(line)+1 {
			t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(line)+1)
		}
		w.drain()

		if want := `{"submit":"[ocagent] chat #c-1 from Owner: 看一下這個"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
		if diag.String() != "" {
			t.Errorf("diag = %q, want nothing", diag.String())
		}
	})

	t.Run("a payload with quotes, markup and continuation lines stays one JSON line", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		w.Write([]byte("[ocagent] chat from owner (#c-2): 看 \"這個\" <b>&\n    第二行\ttab\\\n"))
		w.drain()

		want := `{"submit":"[ocagent] chat from owner (#c-2): 看 \"這個\" <b>&\n    第二行\ttab\\"}` + "\n"
		if out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("transport chatter goes to stderr while the notices the ruling names are submitted", func(t *testing.T) {
		for _, tc := range []struct {
			name, line, out, diag string
		}{
			{"a line this binary did not write", "something else entirely",
				`{"submit":"something else entirely"}` + "\n", ""},
			{"the first disconnect", "[ocagent] listen: disconnected — dial tcp: connection refused",
				`{"submit":"[ocagent] listen: disconnected — dial tcp: connection refused"}` + "\n", ""},
			{"giving up", "[ocagent] listen: giving up — 30 attempts",
				`{"submit":"[ocagent] listen: giving up — 30 attempts"}` + "\n", ""},
			{"the boot connect", "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events",
				"", "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events\n"},
			{"other transport chatter", "[ocagent] listen: retrying in 4s",
				"", "[ocagent] listen: retrying in 4s\n"},
			{"a blank line", "   ", "", "   \n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var out, diag bytes.Buffer
				w := newModWriter(&out, &diag)
				w.Write([]byte(tc.line + "\n"))
				w.drain()

				if out.String() != tc.out {
					t.Errorf("out = %q, want %q", out.String(), tc.out)
				}
				if diag.String() != tc.diag {
					t.Errorf("diag = %q, want %q", diag.String(), tc.diag)
				}
			})
		}
	})

	t.Run("a later connect is submitted once the boot connect was swallowed", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		boot := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		back := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [same station]"
		w.Write([]byte(boot + "\n" + back + "\n"))
		w.drain()

		if want := `{"submit":"` + back + `"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
		if want := boot + "\n"; diag.String() != want {
			t.Errorf("diag = %q, want %q", diag.String(), want)
		}
	})

	t.Run("each batch marker follows exactly the payloads printed before it", func(t *testing.T) {
		// The mod acks a batch once every submit before its marker settled; a
		// marker emitted ahead of a payload would mark that payload read on the
		// station before it was ever submitted.
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		w.Write([]byte("[ocagent] chat from owner (#c-1): 甲\n"))
		w.Write([]byte("[ocagent] chat from owner (#c-2): 乙\n"))
		w.Write([]byte("[ocagent] listen: batch 1 [ts=1759381200.000 local]\n"))
		w.Write([]byte("[ocagent] chat from owner (#c-3): 丙\n"))
		w.Write([]byte("[ocagent] listen: batch 2 [ts=1759381201.000 local]\n"))
		w.drain()

		want := `{"submit":"[ocagent] chat from owner (#c-1): 甲\n[ocagent] chat from owner (#c-2): 乙"}` + "\n" +
			`{"batch":"1"}` + "\n" +
			`{"submit":"[ocagent] chat from owner (#c-3): 丙"}` + "\n" +
			`{"batch":"2"}` + "\n"
		if out.String() != want {
			t.Errorf("out =\n%s\nwant\n%s", out.String(), want)
		}
		if diag.String() != "" {
			t.Errorf("diag = %q, want nothing", diag.String())
		}
	})

	t.Run("an empty batch still gets its marker", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)
		w.Write([]byte("[ocagent] listen: batch 4\n"))
		w.drain()

		if want := `{"batch":"4"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("a chat body quoting a batch marker is chat, not a marker", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)
		quoted := quotedInBody(t, "[ocagent] listen: batch 9")
		w.Write([]byte("[ocagent] chat from owner (#c-5): 一位成員貼給另一位：\n" + quoted + "\n"))
		w.drain()

		want := `{"submit":"[ocagent] chat from owner (#c-5): 一位成員貼給另一位：\n    [ocagent] listen: batch 9"}` + "\n"
		if out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("half a line waits for the rest of it", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		w.Write([]byte("[ocagent] chat #c-1 from "))
		w.drain()
		if out.String() != "" {
			t.Fatalf("half a line was emitted: %q", out.String())
		}

		w.Write([]byte("Owner: 看一下這個\n"))
		w.drain()
		if want := `{"submit":"[ocagent] chat #c-1 from Owner: 看一下這個"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("an event past the ceiling is submitted as its id-only notice", func(t *testing.T) {
		header := "[ocagent] chat from owner (#c-1): "
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)
		w.Write([]byte(header + strings.Repeat("a", 8193-len(header)) + "\n"))
		w.drain()

		want := `{"submit":"[ocagent] chat from owner (#c-1): ` + strings.Repeat("a", 166) +
			`… [這則通知約 1 行／8193 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"}` + "\n"
		if out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("queued events that together pass the ceiling become two submits split between events", func(t *testing.T) {
		first := "[ocagent] chat from owner (#c-1): " + strings.Repeat("a", 4096-34)
		second := "[ocagent] chat from owner (#c-2): " + strings.Repeat("b", 4096-34)
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)
		w.Write([]byte(first + "\n" + second + "\n"))
		w.drain()

		want := `{"submit":"` + first + `"}` + "\n" + `{"submit":"` + second + `"}` + "\n"
		if out.String() != want {
			t.Errorf("got %d frame(s), want 2", strings.Count(out.String(), "\n"))
		}
	})

	t.Run("a write is emitted with nobody stopping or draining the writer", func(t *testing.T) {
		// Observed BEFORE stop(): stopping flushes, so looking only afterwards is
		// green even when Write never wakes the pump.
		var out, diag lockedBuffer
		w := newModWriter(&out, &diag)
		stop := w.start()
		defer stop()

		w.Write([]byte("[ocagent] chat #c-1 from Owner: 看一下這個\n"))

		want := `{"submit":"[ocagent] chat #c-1 from Owner: 看一下這個"}` + "\n"
		deadline := time.Now().Add(2 * time.Second)
		for out.String() != want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})
}
