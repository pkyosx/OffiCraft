package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// T-7fb2 — every transcript line carries the time the event happened.
//
// These assert the BYTES that actually reach the transcript, never "the writer
// was constructed": a wrapper that is built and then not wired up prints
// exactly what an unwrapped one prints, and a test that only checks the
// construction stays green through that.
// ---------------------------------------------------------------------------

var (
	stampLocalRe = regexp.MustCompile(`\[ts=\d+\.\d{3} local\]`)
	stampFrameRe = regexp.MustCompile(`\[ts=\d+\.\d{3}\]`)
)

// The PRODUCTION entry — not a hand-built listener. cmdListen is what
// main.go calls, so this is the one test that fails if the wrapper stops being
// applied on the real path (remove the wrap in cmdListen ⇒ red).
func TestCmdListen_StampsTheProductionPath(t *testing.T) {
	var out bytes.Buffer
	if rc := cmdListen(Config{ID: "kyle"}, noEnv, false, false, &out); rc != 0 {
		t.Fatalf("rc = %d want 0", rc)
	}
	got := out.String()
	if !strings.Contains(got, "no OC_ID/OC_TOKEN") {
		t.Fatalf("expected the mis-wire line, got %q", got)
	}
	if !stampLocalRe.MatchString(got) {
		t.Fatalf("production line carries no local stamp: %q", got)
	}
}

// A frame-derived line must report the SERVER's ts, not this machine's clock —
// that is the whole point: a reconnect can deliver a frame long after it
// happened, and the local clock would silently report the delivery instead.
func TestDispatch_StampsTheServerFrameTime(t *testing.T) {
	var raw bytes.Buffer
	stamper := &eventStamper{clock: func() time.Time { return time.Unix(9999999999, 0) }}
	l := &listener{
		cfg:   Config{ID: "kyle"},
		stamp: stamper,
		out:   &stampWriter{inner: &raw, stamp: stamper.suffix},
	}

	l.dispatch([]byte(`{"seq":42,"topic":"action","op":"patch","data":{},"ts":1752192000.123,"trigger":"owner"}`))

	got := raw.String()
	if !strings.Contains(got, "[ts=1752192000.123]") {
		t.Fatalf("frame line must carry the SERVER ts, got %q", got)
	}
	if strings.Contains(got, "local") {
		t.Fatalf("a frame-derived line must not be labelled local: %q", got)
	}
	// The local clock is 9999999999 — if it leaked in, the stamp came from the
	// wrong source even though a stamp is present.
	if strings.Contains(got, "9999999999") {
		t.Fatalf("frame line reported the LOCAL clock: %q", got)
	}
}

// A frame with no `ts` (older server, malformed) must fall back to the local
// clock AND say so, rather than print a confident wrong number or nothing.
func TestDispatch_FallsBackToLabelledLocalTime(t *testing.T) {
	var raw bytes.Buffer
	stamper := &eventStamper{clock: func() time.Time { return time.Unix(1785717600, 0) }}
	l := &listener{
		cfg:   Config{ID: "kyle"},
		stamp: stamper,
		out:   &stampWriter{inner: &raw, stamp: stamper.suffix},
	}

	l.dispatch([]byte(`{"seq":42,"topic":"action","op":"patch","data":{},"trigger":"owner"}`))

	got := raw.String()
	if !strings.Contains(got, "[ts=1785717600.000 local]") {
		t.Fatalf("a ts-less frame must fall back to a LABELLED local stamp, got %q", got)
	}
}

// The stamper must be cleared on the way out of a frame, or every later
// connection-level line would keep reporting one stale frame's time forever.
func TestDispatch_ClearsTheFrameTimeOnTheWayOut(t *testing.T) {
	var raw bytes.Buffer
	stamper := &eventStamper{clock: func() time.Time { return time.Unix(1785717600, 0) }}
	l := &listener{
		cfg:   Config{ID: "kyle"},
		stamp: stamper,
		out:   &stampWriter{inner: &raw, stamp: stamper.suffix},
	}
	l.dispatch([]byte(`{"seq":1,"topic":"action","op":"patch","data":{},"ts":1752192000.123}`))
	raw.Reset()

	l.logf("listen: stream ended: %v", "EOF")

	got := raw.String()
	if !strings.Contains(got, "[ts=1785717600.000 local]") {
		t.Fatalf("a post-frame connection line must revert to the local clock, got %q", got)
	}
}

// A multi-line event block is ONE event: exactly one stamp, on the header
// line. Stamping every physical line would bury a chat body in timestamps and
// break the existing invariant that only an event's first line starts at
// column 0 (see renderMessageBody).
func TestStampWriter_OneStampOnTheHeaderLineOfAMultiLineEvent(t *testing.T) {
	var raw bytes.Buffer
	w := &stampWriter{inner: &raw, stamp: func() string { return "[ts=1.000]" }}

	block := "[ocagent] reply-card rc-7 answered | asked: pick one\n    option A\n    option B\n"
	n, err := w.Write([]byte(block))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != len(block) {
		t.Fatalf("Write reported %d want %d (io.Writer contract)", n, len(block))
	}

	got := raw.String()
	if c := strings.Count(got, "[ts="); c != 1 {
		t.Fatalf("want exactly 1 stamp for one event block, got %d: %q", c, got)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if !strings.HasSuffix(lines[0], "[ts=1.000]") {
		t.Fatalf("the stamp belongs on the header line, got %q", lines[0])
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "    ") {
			t.Fatalf("continuation line lost its indent: %q", l)
		}
	}
}

// The one way this change could damage something OUTSIDE itself: ocwarden
// classifies ocagent's stdout by PREFIX. A stamp at the front would silently
// reclassify every listener line. This pins the suffix position against that.
func TestStampWriter_KeepsTheOcagentPrefixIntactForOcwarden(t *testing.T) {
	var raw bytes.Buffer
	stamper := &eventStamper{clock: func() time.Time { return time.Unix(1785717600, 0) }}
	w := &stampWriter{inner: &raw, stamp: stamper.suffix}

	w.Write([]byte("[ocagent] listen: connected — streaming http://x/api/events\n")) //nolint:errcheck

	line := strings.TrimRight(raw.String(), "\n")
	// The two literals cli/ocwarden/codex_session.go matches on.
	if !strings.HasPrefix(strings.TrimSpace(line), "[ocagent] listen:") {
		t.Fatalf("ocwarden's transport-diagnostic prefix no longer matches: %q", line)
	}
	if !strings.HasPrefix(strings.TrimSpace(line), "[ocagent] listen: connected") {
		t.Fatalf("ocwarden's connected-line prefix no longer matches: %q", line)
	}
	if !stampLocalRe.MatchString(line) {
		t.Fatalf("the line under test carries no stamp, so it proves nothing: %q", line)
	}
}
