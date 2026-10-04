package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// --deliver-mod: stdout is the mod's protocol and nothing else, one JSON frame
// per line —
//
//	{"submit":"<payload>"}  one packed payload, to become one prompt
//	{"batch":"<token>"}     the ack gate's end-of-batch marker; the mod answers it
//	                        in OC_LISTEN_ACK_FILE once every submit before it settled
//
// in the order the listener printed them. Lines that are not forwarded go to
// stderr. The frame shapes are spelled again in cli/ocwarden/mod/hooks/register.ts.
type modFrame struct {
	Submit *string `json:"submit,omitempty"`
	Batch  *string `json:"batch,omitempty"`
}

// One queued unit: a forwarded line, or a batch marker (batch != "").
type modItem struct {
	line  string
	batch string
}

// 🔴 Lines are QUEUED for a pump, never emitted inline: Write runs inside the SSE
// scan loop, whose idle-read watchdog resets only on reads, and a stdout the mod
// is slow to read would block it.
type modWriter struct {
	out  io.Writer
	diag io.Writer

	// diagMu guards diag, outMu guards out, mu the queue; none is held while
	// taking another. ⚠️ Assumes Write has ONE calling goroutine (the SSE scan
	// loop): a second one could interleave a batch marker with lines it does not
	// close.
	diagMu  sync.Mutex
	outMu   sync.Mutex
	mu      sync.Mutex
	pending bytes.Buffer
	queued  []modItem
	filter  bootConnectFilter

	wake chan struct{}
}

func newModWriter(out, diag io.Writer) *modWriter {
	return &modWriter{out: out, diag: diag, wake: make(chan struct{}, 1)}
}

func (w *modWriter) start() func() {
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-w.wake:
				w.drain()
			case <-quit:
				w.drain()
				return
			}
		}
	}()
	return func() {
		close(quit)
		<-done
	}
}

func (w *modWriter) Write(p []byte) (int, error) {
	var unforwarded []string
	w.mu.Lock()
	w.pending.Write(p)
	for {
		line, ok := takeLine(&w.pending)
		if !ok {
			break
		}
		if token, isBatch := batchMarkerToken(line); isBatch {
			w.queued = append(w.queued, modItem{batch: token})
		} else if w.filter.shouldForward(line) {
			w.queued = append(w.queued, modItem{line: line})
		} else {
			unforwarded = append(unforwarded, line)
		}
	}
	queued := len(w.queued)
	w.mu.Unlock()

	if len(unforwarded) > 0 {
		w.diagMu.Lock()
		_, _ = io.WriteString(w.diag, strings.Join(unforwarded, "\n")+"\n")
		w.diagMu.Unlock()
	}
	if queued > 0 {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

// The listener stamps the line with a trailing `[ts=…]`, so the token is the
// first field after the head (same parse as codex_session.go's).
func batchMarkerToken(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, agentLinePrefix+noticeBatch+" ")
	if !ok {
		return "", false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

func (w *modWriter) drain() {
	for {
		w.mu.Lock()
		items := w.queued
		w.queued = nil
		w.mu.Unlock()
		if len(items) == 0 {
			return
		}
		var lines []string
		submitLines := func() {
			for _, payload := range packDeliveries(lines) {
				w.emit(modFrame{Submit: &payload})
			}
			lines = nil
		}
		for _, item := range items {
			if item.batch == "" {
				lines = append(lines, item.line)
				continue
			}
			submitLines()
			w.emit(modFrame{Batch: &item.batch})
		}
		submitLines()
	}
}

func (w *modWriter) emit(frame modFrame) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(frame); err != nil {
		w.note("listen: could not encode a frame for the mod (%v)\n", err)
		return
	}
	w.outMu.Lock()
	_, err := w.out.Write(buf.Bytes())
	w.outMu.Unlock()
	if err != nil {
		w.note("listen: the mod's stdout refused a frame (%v)\n", err)
	}
}

func (w *modWriter) note(format string, args ...any) {
	w.diagMu.Lock()
	defer w.diagMu.Unlock()
	fmt.Fprintf(w.diag, agentLinePrefix+format, args...)
}
