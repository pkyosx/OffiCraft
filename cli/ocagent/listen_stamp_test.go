package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// stampClock returns a clock stuck at one instant, for the local-clock branch.
func stampClock(unixNanos int64) func() time.Time {
	return func() time.Time { return time.Unix(0, unixNanos) }
}

// failingWriter reports err on every Write and records nothing.
type failingWriter struct{ err error }

func (w *failingWriter) Write(p []byte) (int, error) { return 0, w.err }

func TestEnter(t *testing.T) {
	t.Run("a frame with a ts parks it and the returned func clears it", func(t *testing.T) {
		s := &eventStamper{clock: stampClock(1_500_000_000_000_000_000)}

		done := s.enter(map[string]any{"ts": 1787148244.692})
		if got := s.suffix(); got != "[ts=1787148244.692]" {
			t.Errorf("suffix while parked = %q, want %q", got, "[ts=1787148244.692]")
		}

		done()
		if got := s.suffix(); got != "[ts=1500000000.000 local]" {
			t.Errorf("suffix after clearing = %q, want %q", got, "[ts=1500000000.000 local]")
		}
	})

	t.Run("a frame carrying no ts parks nothing and falls back to the local clock", func(t *testing.T) {
		s := &eventStamper{clock: stampClock(1_500_000_000_000_000_000)}

		done := s.enter(map[string]any{"topic": "chat"})

		if got := s.suffix(); got != "[ts=1500000000.000 local]" {
			t.Errorf("suffix = %q, want the local-clock form", got)
		}
		done()
		if got := s.suffix(); got != "[ts=1500000000.000 local]" {
			t.Errorf("suffix after clearing = %q, want the local-clock form", got)
		}
	})

	t.Run("a ts that is not a number parks nothing", func(t *testing.T) {
		s := &eventStamper{clock: stampClock(1_500_000_000_000_000_000)}

		s.enter(map[string]any{"ts": "1787148244.692"})

		if got := s.suffix(); got != "[ts=1500000000.000 local]" {
			t.Errorf("suffix = %q, want the local-clock form", got)
		}
	})

	t.Run("a nil stamper hands back a usable no-op", func(t *testing.T) {
		var s *eventStamper

		done := s.enter(map[string]any{"ts": 1787148244.692})

		if done == nil {
			t.Fatal("enter returned a nil func — the deferred call site would panic")
		}
		done()
		if got := s.suffix(); got != "" {
			t.Errorf("suffix = %q, want empty — a nil stamper stamps nothing", got)
		}
	})
}

func TestSuffix(t *testing.T) {
	t.Run("a parked frame ts is rendered to three decimals with no clock label", func(t *testing.T) {
		s := &eventStamper{frameTS: 1787148244.692, clock: stampClock(1_500_000_000_000_000_000)}

		if got := s.suffix(); got != "[ts=1787148244.692]" {
			t.Errorf("suffix = %q, want %q", got, "[ts=1787148244.692]")
		}
	})

	t.Run("no parked frame reports this machine's clock and says so", func(t *testing.T) {
		s := &eventStamper{clock: stampClock(1_787_148_244_692_000_000)}

		if got := s.suffix(); got != "[ts=1787148244.692 local]" {
			t.Errorf("suffix = %q, want %q", got, "[ts=1787148244.692 local]")
		}
	})

	t.Run("no injected clock falls back to the real one, still labelled local", func(t *testing.T) {
		s := &eventStamper{}

		got := s.suffix()

		if !strings.HasPrefix(got, "[ts=") || !strings.HasSuffix(got, " local]") {
			t.Errorf("suffix = %q, want the [ts=<seconds> local] form", got)
		}
	})

	t.Run("a nil stamper renders nothing at all", func(t *testing.T) {
		var s *eventStamper

		if got := s.suffix(); got != "" {
			t.Errorf("suffix = %q, want empty", got)
		}
	})
}

func TestWrite(t *testing.T) {
	const stamp = "[ts=1787148244.692 local]"

	t.Run("a single line is stamped at its end", func(t *testing.T) {
		var inner strings.Builder
		w := &stampWriter{inner: &inner, stamp: func() string { return stamp }}

		n, err := w.Write([]byte("[ocagent] listen: connected\n"))

		if err != nil {
			t.Fatalf("Write err = %v, want nil", err)
		}
		if n != 28 {
			t.Errorf("n = %d, want 28 — the io.Writer contract reports the caller's own length", n)
		}
		want := "[ocagent] listen: connected [ts=1787148244.692 local]\n"
		if inner.String() != want {
			t.Errorf("wrote %q, want %q", inner.String(), want)
		}
	})

	t.Run("only the first line of a multi-line block is stamped", func(t *testing.T) {
		var inner strings.Builder
		w := &stampWriter{inner: &inner, stamp: func() string { return stamp }}

		n, err := w.Write([]byte("[ocagent] chat from boss: head\n    second\n    third\n"))

		if err != nil {
			t.Fatalf("Write err = %v, want nil", err)
		}
		if n != 52 {
			t.Errorf("n = %d, want 52", n)
		}
		want := "[ocagent] chat from boss: head [ts=1787148244.692 local]\n    second\n    third\n"
		if inner.String() != want {
			t.Errorf("wrote %q, want %q", inner.String(), want)
		}
	})

	t.Run("a write with no newline is stamped at the end of what it wrote", func(t *testing.T) {
		var inner strings.Builder
		w := &stampWriter{inner: &inner, stamp: func() string { return stamp }}

		n, err := w.Write([]byte("[ocagent] listen: connected"))

		if err != nil {
			t.Fatalf("Write err = %v, want nil", err)
		}
		if n != 27 {
			t.Errorf("n = %d, want 27", n)
		}
		want := "[ocagent] listen: connected [ts=1787148244.692 local]"
		if inner.String() != want {
			t.Errorf("wrote %q, want %q", inner.String(), want)
		}
	})

	t.Run("an empty stamp passes the bytes through untouched", func(t *testing.T) {
		var inner strings.Builder
		w := &stampWriter{inner: &inner, stamp: func() string { return "" }}

		n, err := w.Write([]byte("[ocagent] listen: connected\n"))

		if err != nil {
			t.Fatalf("Write err = %v, want nil", err)
		}
		if n != 28 {
			t.Errorf("n = %d, want 28", n)
		}
		if inner.String() != "[ocagent] listen: connected\n" {
			t.Errorf("wrote %q, want the input unchanged", inner.String())
		}
	})

	t.Run("no stamp func at all passes the bytes through untouched", func(t *testing.T) {
		var inner strings.Builder
		w := &stampWriter{inner: &inner}

		n, err := w.Write([]byte("plain\n"))

		if err != nil {
			t.Fatalf("Write err = %v, want nil", err)
		}
		if n != 6 {
			t.Errorf("n = %d, want 6", n)
		}
		if inner.String() != "plain\n" {
			t.Errorf("wrote %q, want %q", inner.String(), "plain\n")
		}
	})

	t.Run("a failing inner writer surfaces its error and claims no bytes", func(t *testing.T) {
		boom := errors.New("pipe closed")
		w := &stampWriter{inner: &failingWriter{err: boom}, stamp: func() string { return stamp }}

		n, err := w.Write([]byte("[ocagent] listen: connected\n"))

		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
		if n != 0 {
			t.Errorf("n = %d, want 0", n)
		}
	})
}
