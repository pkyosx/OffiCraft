package main

import (
	"bytes"
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
		return errFakeTmux
	}
	return nil
}

var errFakeTmux = &tmuxTestError{}

type tmuxTestError struct{}

func (*tmuxTestError) Error() string { return "tmux refused" }

func newRecordingPaneWriter(inner *bytes.Buffer) (*paneWriter, *recordTmux) {
	rec := &recordTmux{fail: map[int]bool{}}
	return newPaneWriter(inner, "officraft", "member-m1", rec.run, func(time.Duration) {}), rec
}

func TestForwardToPane(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want bool
	}{
		{"an event block reaches the member", "[ocagent] chat #c-1 from Owner: 看一下這個", true},
		{"a line this binary did not write reaches the member", "something else entirely", true},
		{"the first disconnect reaches the member",
			"[ocagent] listen: disconnected — dial tcp: connection refused", true},
		{"the reconnect reaches the member",
			"[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events", true},
		{"giving up reaches the member",
			"[ocagent] listen: giving up — 30 attempts", true},
		{"the batch marker is protocol and is swallowed",
			"[ocagent] listen: batch tok-1 [ts=1 local]", false},
		{"other transport chatter is swallowed",
			"[ocagent] listen: retrying in 4s", false},
		{"a blank line is swallowed", "   ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := forwardToPane(tc.line); got != tc.want {
				t.Errorf("forwardToPane(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestPaneWriterDeliversAForwardedLine(t *testing.T) {
	var log bytes.Buffer
	w, rec := newRecordingPaneWriter(&log)

	line := "[ocagent] chat #c-1 from Owner: 看一下這個"
	n, err := w.Write([]byte(line + "\n"))
	if err != nil || n != len(line)+1 {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(line)+1)
	}

	// The log copy is not optional: the listener's own session is where a
	// swallowed or mis-delivered line can still be read.
	if got := log.String(); got != line+"\n" {
		t.Errorf("log = %q, want %q", got, line+"\n")
	}

	want := [][]string{
		{"-L", "officraft", "set-buffer", "-b", "oc-listen-deliver", line},
		{"-L", "officraft", "paste-buffer", "-t", "member-m1", "-b", "oc-listen-deliver", "-d", "-p"},
	}
	for i := 0; i < paneEnterAttempts; i++ {
		want = append(want, []string{"-L", "officraft", "send-keys", "-t", "member-m1", "Enter"})
	}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("tmux calls =\n%v\nwant\n%v", rec.calls, want)
	}
}

func TestPaneWriterSwallowsTransportChatter(t *testing.T) {
	var log bytes.Buffer
	w, rec := newRecordingPaneWriter(&log)

	chatter := "[ocagent] listen: batch tok-1 [ts=1 local]\n"
	if _, err := w.Write([]byte(chatter)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("a swallowed line must not reach the pane, calls = %v", rec.calls)
	}
	if log.String() != chatter {
		t.Errorf("log = %q, want the swallowed line to still be logged", log.String())
	}
}

func TestPaneWriterHoldsAPartialLine(t *testing.T) {
	var log bytes.Buffer
	w, rec := newRecordingPaneWriter(&log)

	if _, err := w.Write([]byte("[ocagent] chat #c-1 from ")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("half a line was delivered: %v", rec.calls)
	}
	if _, err := w.Write([]byte("Owner: 看一下這個\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(rec.calls) == 0 {
		t.Fatal("the completed line was never delivered")
	}
	if got := rec.calls[0][len(rec.calls[0])-1]; got != "[ocagent] chat #c-1 from Owner: 看一下這個" {
		t.Errorf("delivered %q, want the two halves joined", got)
	}
}

func TestPaneWriterRetriesTheRejectedPasteFlags(t *testing.T) {
	var log bytes.Buffer
	rec := &recordTmux{fail: map[int]bool{1: true}} // the -d -p paste
	w := newPaneWriter(&log, "officraft", "member-m1", rec.run, func(time.Duration) {})

	if _, err := w.Write([]byte("[ocagent] chat #c-1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(rec.calls) < 3 {
		t.Fatalf("calls = %v", rec.calls)
	}
	bare := rec.calls[2]
	want := []string{"-L", "officraft", "paste-buffer", "-t", "member-m1", "-b", "oc-listen-deliver"}
	if !reflect.DeepEqual(bare, want) {
		t.Errorf("a rejected paste must retry with bare flags, call 2 = %v, want %v", bare, want)
	}
}

// 🔴 The refusal this pins is the 假活 case: with no session name the listener
// can neither deliver nor ever self-exit, so it would hold the SSE open on
// behalf of a member that is gone and the station would never recycle it.
func TestListenSinkRefusesDeliveryWithNoSession(t *testing.T) {
	var out bytes.Buffer
	sink, ok := listenSink(&out, testEnv(map[string]string{}), true)
	if ok || sink != nil {
		t.Fatalf("listenSink = %v, %v; want a refusal", sink, ok)
	}
	if !strings.Contains(out.String(), "OC_SESSION") {
		t.Errorf("the refusal must name what is missing, got %q", out.String())
	}

	blank, ok := listenSink(&out, testEnv(map[string]string{"OC_SESSION": "   "}), true)
	if ok || blank != nil {
		t.Errorf("a blank OC_SESSION must refuse too, got %v, %v", blank, ok)
	}
}

func TestListenSinkWiresDeliveryToTheMemberSession(t *testing.T) {
	var out bytes.Buffer
	sink, ok := listenSink(&out, testEnv(map[string]string{
		"OC_SESSION": "member-m1", "OC_TMUX_SOCKET": "lab",
	}), true)
	if !ok {
		t.Fatal("listenSink refused a run that names its session")
	}
	pane, isPane := sink.(*paneWriter)
	if !isPane {
		t.Fatalf("sink = %T, want the pane writer — without it every event is printed to a stdout nobody reads", sink)
	}
	if pane.session != "member-m1" || pane.socket != "lab" {
		t.Errorf("pane writer targets %s on %s, want member-m1 on lab", pane.session, pane.socket)
	}

	// The default is unchanged: a listener the member mounted itself still
	// prints to its own stdout.
	plain, ok := listenSink(&out, testEnv(map[string]string{"OC_SESSION": "member-m1"}), false)
	if !ok || plain != &out {
		t.Errorf("without --deliver-tmux the sink must stay the caller's writer, got %T", plain)
	}
}
