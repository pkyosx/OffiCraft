package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// submitFrames is what the mod reads for these payloads; the first TestModWriter
// case spells the same bytes out by hand.
func submitFrames(t *testing.T, payloads ...string) string {
	t.Helper()
	var out strings.Builder
	for _, p := range payloads {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(map[string]string{"submit": p}); err != nil {
			t.Fatal(err)
		}
		out.Write(buf.Bytes())
	}
	return out.String()
}

func chatEvent(t *testing.T, id, body string) string {
	t.Helper()
	var out bytes.Buffer
	printChatLine(&out, map[string]any{"id": id, "from": "owner", "body": body}, 0)
	return strings.TrimSuffix(out.String(), "\n")
}

// quotedInBody is the CONTINUATION line of a chat body that quotes a transport
// line, produced by renderMessageBody — the renderer the real path uses. The
// cases turn on its indentation, which a typed literal would hide.
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

func TestModWriter(t *testing.T) {
	t.Run("an event becomes one submit frame and nothing reaches stderr", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		line := "[ocagent] chat #c-1 from Owner: 看一下這個"
		n, err := w.Write([]byte(line + "\n"))
		if err != nil || n != len(line)+1 {
			t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(line)+1)
		}
		w.flushToMod()

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
		w.flushToMod()

		want := `{"submit":"[ocagent] chat from owner (#c-2): 看 \"這個\" <b>&\n    第二行\ttab\\"}` + "\n"
		if out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("transport chatter goes to stderr while the notices the ruling names are submitted and echoed there", func(t *testing.T) {
		for _, tc := range []struct {
			name, line, out, diag string
		}{
			{"a line this binary did not write", "something else entirely",
				`{"submit":"something else entirely"}` + "\n", ""},
			{"the first disconnect", "[ocagent] listen: disconnected — dial tcp: connection refused",
				`{"submit":"[ocagent] listen: disconnected — dial tcp: connection refused"}` + "\n",
				"[ocagent] listen: disconnected — dial tcp: connection refused\n"},
			{"giving up", "[ocagent] listen: giving up — 30 attempts",
				`{"submit":"[ocagent] listen: giving up — 30 attempts"}` + "\n",
				"[ocagent] listen: giving up — 30 attempts\n"},
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
				w.flushToMod()

				if out.String() != tc.out {
					t.Errorf("out = %q, want %q", out.String(), tc.out)
				}
				if diag.String() != tc.diag {
					t.Errorf("diag = %q, want %q", diag.String(), tc.diag)
				}
			})
		}
	})

	t.Run("under a boot connect, it is swallowed and every later one is submitted", func(t *testing.T) {
		// It is also the half that stops an OVERSHOOT ("never swallow at all"): the
		// forwarded-disconnect case below passes under that mutant, so the two are
		// only a two-way guard while BOTH are alive.
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		boot := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		back := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [same station]"
		again := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [new station — was c67268a4]"
		w.Write([]byte(boot + "\n" + back + "\n" + again + "\n"))
		w.flushToMod()

		if want := `{"submit":"` + back + `\n` + again + `"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
		if want := boot + "\n" + back + "\n" + again + "\n"; diag.String() != want {
			t.Errorf("diag = %q, want %q", diag.String(), want)
		}
	})

	t.Run("under a connect that answers a forwarded disconnect, it is not swallowed", func(t *testing.T) {
		// The first failed dial promises the member that the next transport line is
		// the reconnect or a give-up (listen_run.go); swallowing that reconnect
		// leaves the member waiting. 🔴 Half a guard: "never swallow anything"
		// passes it too, the boot-connect case above refuses that.
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		down := agentLinePrefix + noticeDisconnected + " — dial tcp: connection refused" +
			" (retrying on the same schedule, quietly; the next transport line you see" +
			" is either the reconnect or a give-up)"
		up := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		w.Write([]byte(down + "\n"))
		w.flushToMod()
		w.Write([]byte(up + "\n"))
		w.flushToMod()

		if want := submitFrames(t, down, up); out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("under a quoted transport line, the boot swallow is not spent", func(t *testing.T) {
		// The quoted copy reaches this writer INDENTED (renderMessageBody), so a
		// comparison that trims first mistakes it for this binary's own notice and
		// forwards the real boot connect into a member that is still booting.
		boot := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		for _, quotedNotice := range []string{
			boot,
			"[ocagent] listen: disconnected — dial tcp: connection refused",
		} {
			t.Run(quotedNotice, func(t *testing.T) {
				var out, diag bytes.Buffer
				w := newModWriter(&out, &diag)

				quoted := quotedInBody(t, quotedNotice)
				w.Write([]byte(quoted + "\n"))
				w.flushToMod()
				w.Write([]byte(boot + "\n"))
				w.flushToMod()

				if want := submitFrames(t, quoted); out.String() != want {
					t.Errorf("out = %q, want only the quoted line", out.String())
				}
			})
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
		w.flushToMod()

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

	t.Run("a payload taken before its marker was queued is still emitted ahead of that marker", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		w.Write([]byte("[ocagent] chat from owner (#c-1): 甲\n"))
		w.flushToMod()
		w.Write([]byte("[ocagent] listen: batch 1\n"))
		w.flushToMod()

		if want := `{"submit":"[ocagent] chat from owner (#c-1): 甲"}` + "\n" + `{"batch":"1"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("an empty batch still gets its marker", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)
		w.Write([]byte("[ocagent] listen: batch 4\n"))
		w.flushToMod()

		if want := `{"batch":"4"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("a chat body quoting a batch marker is chat, not a marker", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)
		quoted := quotedInBody(t, "[ocagent] listen: batch 9")
		w.Write([]byte("[ocagent] chat from owner (#c-5): 一位成員貼給另一位：\n" + quoted + "\n"))
		w.flushToMod()

		want := `{"submit":"[ocagent] chat from owner (#c-5): 一位成員貼給另一位：\n    [ocagent] listen: batch 9"}` + "\n"
		if out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("half a line waits for the rest of it", func(t *testing.T) {
		var out, diag bytes.Buffer
		w := newModWriter(&out, &diag)

		w.Write([]byte("[ocagent] chat #c-1 from "))
		w.flushToMod()
		if out.String() != "" {
			t.Fatalf("half a line was emitted: %q", out.String())
		}

		w.Write([]byte("Owner: 看一下這個\n"))
		w.flushToMod()
		if want := `{"submit":"[ocagent] chat #c-1 from Owner: 看一下這個"}` + "\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("under the ceiling, an event goes whole and one byte more goes as its id-only notice", func(t *testing.T) {
		// Owner ruling rc-62ede5d63772: the ceiling counts every byte delivered,
		// the header included.
		header := "[ocagent] chat from owner (#c-1): "
		atCeiling := header + strings.Repeat("a", 8192-len(header))
		bigChat := chatEvent(t, "c-2", strings.Repeat("界", 4000))
		var long strings.Builder
		for i := 0; i < 1194; i++ {
			fmt.Fprintf(&long, "line %04d xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", i)
		}
		big := strings.Repeat("b", 9000)
		for _, tc := range []struct{ name, event, submitted string }{
			{"an event at the ceiling", atCeiling, atCeiling},
			{"one byte over", atCeiling + "a", header + strings.Repeat("a", 166) +
				"… [這則通知約 1 行／8193 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"},
			{"a thousand-line message", chatEvent(t, "c-6feb08ebdbb6", long.String()),
				"[ocagent] chat from owner (#c-6feb08ebdbb6): line 0000 xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" +
					" [這則通知約 1194 行／53770 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"},
			{"a first line cut at 200 characters, between characters", bigChat,
				"[ocagent] chat from owner (#c-2): " + strings.Repeat("界", 166) +
					"… [這則通知約 1 行／4034 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"},
			{"a reply card", "[ocagent] reply-card rc-1 answered: " + big,
				"[ocagent] reply-card rc-1 answered: " + strings.Repeat("b", 164) +
					"… [這則通知約 1 行／9036 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_reply_card 讀全文]"},
			{"a task", "[ocagent] task T-1 " + big,
				"[ocagent] task T-1 " + strings.Repeat("b", 181) +
					"… [這則通知約 1 行／9019 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_task 讀全文]"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var out, diag bytes.Buffer
				w := newModWriter(&out, &diag)
				w.Write([]byte(tc.event + "\n"))
				w.flushToMod()

				if want := submitFrames(t, tc.submitted); out.String() != want {
					t.Errorf("out =\n%q\nwant\n%q", out.String(), want)
				}
			})
		}
	})

	t.Run("under queued events that together pass the ceiling, submits split between events, never inside one", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			events    []string
			submitted []string
		}{
			{"two that fit only without their joining newline",
				[]string{"[ocagent] chat from owner (#c-1): " + strings.Repeat("a", 4096-34),
					"[ocagent] chat from owner (#c-2): " + strings.Repeat("b", 4096-34)},
				[]string{"[ocagent] chat from owner (#c-1): " + strings.Repeat("a", 4096-34),
					"[ocagent] chat from owner (#c-2): " + strings.Repeat("b", 4096-34)}},
			{"multi-line events",
				[]string{chatEvent(t, "c-1", "甲\n"+strings.Repeat("a", 5000)),
					chatEvent(t, "c-2", "乙\n"+strings.Repeat("b", 5000)),
					"[ocagent] chat from owner (#c-3): 短的"},
				[]string{chatEvent(t, "c-1", "甲\n"+strings.Repeat("a", 5000)),
					chatEvent(t, "c-2", "乙\n"+strings.Repeat("b", 5000)) + "\n[ocagent] chat from owner (#c-3): 短的"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var out, diag bytes.Buffer
				w := newModWriter(&out, &diag)
				w.Write([]byte(strings.Join(tc.events, "\n") + "\n"))
				w.flushToMod()

				if want := submitFrames(t, tc.submitted...); out.String() != want {
					t.Errorf("got %d frame(s), want %d", strings.Count(out.String(), "\n"), len(tc.submitted))
				}
			})
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
