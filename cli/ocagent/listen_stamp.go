package main

import (
	"bytes"
	"fmt"
	"io"
	"time"
)

// The stamp is a SUFFIX because cli/ocwarden classifies ocagent's stdout by
// line PREFIX (`[ocagent] listen:` …) and pins those literals in its tests; a
// leading stamp would silently break that. Only the first line of a Write gets
// it, keeping renderMessageBody's invariant that only an event's header line
// starts at column 0.

// Single-goroutine: the SSE read loop dispatches one frame at a time (onData),
// so park/clear never interleave.
type eventStamper struct {
	clock   func() time.Time
	frameTS float64
}

func (s *eventStamper) enter(frame map[string]any) func() {
	if s == nil {
		return func() {}
	}
	ts, _ := frame["ts"].(float64)
	s.frameTS = ts
	return func() { s.frameTS = 0 }
}

func (s *eventStamper) suffix() string {
	if s == nil {
		return ""
	}
	if s.frameTS != 0 {
		return fmt.Sprintf("[ts=%.3f]", s.frameTS)
	}
	now := time.Now
	if s.clock != nil {
		now = s.clock
	}
	return fmt.Sprintf("[ts=%.3f local]", float64(now().UnixNano())/1e9)
}

type stampWriter struct {
	inner io.Writer
	stamp func() string
}

func (w *stampWriter) Write(p []byte) (int, error) {
	suffix := ""
	if w.stamp != nil {
		suffix = w.stamp()
	}
	if suffix == "" {
		return w.inner.Write(p)
	}

	var buf bytes.Buffer
	if i := bytes.IndexByte(p, '\n'); i >= 0 {
		buf.Write(p[:i])
		buf.WriteByte(' ')
		buf.WriteString(suffix)
		buf.Write(p[i:])
	} else {
		buf.Write(p)
		buf.WriteByte(' ')
		buf.WriteString(suffix)
	}

	if _, err := w.inner.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}
