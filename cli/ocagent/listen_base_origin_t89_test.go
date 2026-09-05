package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// T-89. The bug was NEVER "listen prints nothing when OC_BASE is unset". It is that
// loadConfig invents an address, and the transport lines then describe the invented
// one in exactly the bytes a member that was TOLD that address prints. These tests
// pin the segment that says which of the two happened — and, more importantly, pin
// that saying it did not turn a noisy failure into a quiet one.

// t89Listener builds a listener whose only variable is BaseConfigured, so a
// difference between two transcripts can come from nothing else.
func t89Listener(t *testing.T, srv *httptest.Server, configured bool, out *syncBuf) *listener {
	t.Helper()
	cfgTempDir = t.TempDir()
	return newTestListener(srv, Config{
		Base: srv.URL, Token: "tok", ID: "kyle", BaseConfigured: configured,
	}, out)
}

func TestBaseAddressOrigin_ConfiguredIsTheEmptyString(t *testing.T) {
	// The whole "invisible on a healthy machine" guarantee is this one value.
	if got := baseAddressOrigin(true); got != "" {
		t.Fatalf("a configured base must add NOTHING to any line, got %q", got)
	}
	if got := baseAddressOrigin(false); got == "" {
		t.Fatal("an unconfigured base must say so; got the empty string, which is " +
			"the exact silence this ticket exists to remove")
	}
}

func TestConnectedLine_SaysTheAddressWasGuessedOnlyWhenItWas(t *testing.T) {
	srv := httptest.NewServer(stationHandler("9f3c1ab77e40", true))
	defer srv.Close()

	var lines [2]string
	for i, configured := range []bool{true, false} {
		out := &syncBuf{}
		l := t89Listener(t, srv, configured, out)
		if _, _, _, err := l.connectOnce(context.Background()); err != nil {
			t.Fatalf("connectOnce(configured=%v): %v", configured, err)
		}
		lines[i] = connectedLine(t, out.String())
	}
	configuredLine, guessedLine := lines[0], lines[1]

	// NEGATIVE CONTROL first: a member that was told its address must produce a line
	// with no trace of this change at all. Without this half, a segment printed
	// unconditionally would pass the positive assertion below.
	if strings.Contains(configuredLine, "GUESSED") {
		t.Fatalf("a CONFIGURED member must not be accused of guessing:\n%s", configuredLine)
	}
	if !strings.Contains(guessedLine, "GUESSED") {
		t.Fatalf("an UNCONFIGURED member's connect line must say the address was "+
			"invented — this line is otherwise byte-identical to the configured "+
			"one, which is the whole defect:\n%s", guessedLine)
	}
	if !strings.Contains(guessedLine, "OC_BASE") {
		t.Fatalf("the line must name the thing the reader has to set, or it reports "+
			"a problem nobody can act on:\n%s", guessedLine)
	}

	// The sidecar contract: the head is matched from column 0 and must not move.
	for _, line := range lines {
		if !strings.HasPrefix(line, agentLinePrefix+noticeConnected) {
			t.Fatalf("the notice head must stay at column 0 (cli/ocwarden/"+
				"codex_session.go matches it with HasPrefix):\n%s", line)
		}
	}
	// And the two sha segments must still be last — FIVE existing tests assert it.
	if !strings.HasSuffix(guessedLine, " [station 9f3c1ab77e40]") {
		t.Fatalf("the origin segment must be inserted BEFORE the sha segments, not "+
			"appended; five existing tests assert this line ends with the station "+
			"sha:\n%s", guessedLine)
	}
}

// 🔴 THE HALF THAT IS EASY TO MISS. On a machine that is not the station host an
// unconfigured listener never connects at all, so the connect line above is never
// reached and the disconnect notice is the ONLY transport line that member ever
// prints. Without the segment there it reads "the station is down" when the truth
// is "nobody told me which station".
func TestDisconnectNotice_CarriesTheOriginToo(t *testing.T) {
	for _, tc := range []struct{ configured, want bool }{{true, false}, {false, true}} {
		out := &syncBuf{}
		l := &listener{cfg: Config{Base: "http://127.0.0.1:1", BaseConfigured: tc.configured}, out: out}
		l.noteDisconnect("connect failed: %v", "dial tcp: refused")
		got := strings.Contains(out.String(), "GUESSED")
		if got != tc.want {
			t.Fatalf("disconnect notice with BaseConfigured=%v: GUESSED present=%v, want %v\n%s",
				tc.configured, got, tc.want, out.String())
		}
	}
}

// 🔴 THE CORE ASSERTION OF THIS TICKET, AND THE ONLY ONE THAT WOULD SURVIVE
// SOMEBODY "TIDYING" THIS INTO THE SHAPE THE OTHER THREE SUBCOMMANDS USE.
//
// The owner's ruling (rc-55a969718c98, option [1]) is that an unconfigured listen
// must SPEAK, not LEAVE: a listener that exits is a member that goes quiet, and from
// the outside a quiet member is indistinguishable from a dead one. Both shapes the
// earlier deferral in cmdListen proposed were refusals, and the debounced one ends
// at selfTerminate(), which kills the member's tmux session.
//
// So this test does not look at any string. It asserts that an unconfigured listener
// keeps dialling: a mutant that prints the segment and then returns/exits goes red
// here and NOWHERE ELSE.
func TestUnconfiguredBase_KeepsRetryingAndNeverGoesQuiet(t *testing.T) {
	cfgTempDir = t.TempDir()

	const letItTryAtLeast = 4

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		// Every dial fails: this is the NON-station machine, where the invented
		// loopback address has nothing listening behind it. 502 is a plain
		// failure, deliberately NOT the authoritative 409/401 refusal — this test
		// must not travel down the fail-closed path it is not about.
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	out := &syncBuf{}
	l := t89Listener(t, srv, false, out)

	// Stop it from the OUTSIDE, and only after it has dialled several times. A
	// listener that decided to leave on its own would never reach the count.
	ctx, cancel := context.WithCancel(context.Background())
	l.sleep = func(time.Duration) {
		if attempts >= letItTryAtLeast {
			cancel()
		}
	}

	// 🔴 THE CONSTANT IS PART OF THE ASSERTION, SO IT GETS ASSERTED. Independent
	// review found that setting letItTryAtLeast to 0 — one token, in this file —
	// makes `attempts < 0` unsatisfiable and kills the only check that catches a
	// print-and-exit listener, while every other line below stays green. A number
	// that decides whether a test can fail is not a knob; 1 would also pass for a
	// listener that dialled once and left.
	if letItTryAtLeast < 2 {
		t.Fatalf("letItTryAtLeast = %d: below 2 this test cannot distinguish a "+
			"listener that keeps retrying from one that announced and left, which "+
			"is the only thing it exists to detect", letItTryAtLeast)
	}

	// rc is deliberately NOT asserted: every one of run()'s returns goes through
	// stopRetrying, which returns 0 unconditionally, so `rc != 0` is a check that
	// can never fire. A dead assertion is worse than no assertion — it reads like
	// coverage. (Also found by independent review.)
	_ = l.run(ctx)

	if attempts < letItTryAtLeast {
		t.Fatalf("an unconfigured listener must KEEP DIALLING, not announce and "+
			"leave: it stopped after %d attempts (wanted at least %d). This is the "+
			"whole ruling — a listener that exits is a member that goes quiet, and "+
			"a quiet member is indistinguishable from a dead one.\n%s",
			attempts, letItTryAtLeast, out.String())
	}
	transcript := out.String()
	if !strings.Contains(transcript, "GUESSED") {
		t.Fatalf("the one transport line this member ever prints must say the "+
			"address was invented:\n%s", transcript)
	}
	// It said it ONCE, not once per retry: the origin segment rides the existing
	// debounce rather than adding a second, unthrottled voice beside it.
	if n := strings.Count(transcript, "GUESSED"); n != 1 {
		t.Fatalf("the origin segment must inherit the once-per-outage debounce; "+
			"printed %d times across %d dials:\n%s", n, attempts, transcript)
	}
	// And the give-up line is what ends it — proving the exit came from the ctx
	// cancel above and not from anything this ticket added.
	if !strings.Contains(transcript, noticeGivingUp) {
		t.Fatalf("leaving an open outage must print the give-up line:\n%s", transcript)
	}
}

// 🔴 THE LINE THAT HANDS THE FEATURE ITS INPUT. Every other test in this file
// builds a listener directly and therefore holds baseAddressOrigin correct while
// saying nothing about whether cmdListen ever passes the real flag in. Independent
// review named the mutant: set cfg.BaseConfigured = true on the way into the
// construction and all four of them stay green with the feature gone.
//
// newListener exists so that line is a unit. No network, no clock, no goroutine.
func TestNewListener_CarriesBaseConfiguredThroughUnchanged(t *testing.T) {
	cfgTempDir = t.TempDir()
	noEnv := func(string) string { return "" }

	for _, configured := range []bool{true, false} {
		cfg := Config{
			Base: "http://127.0.0.1:1", Token: "tok", ID: "kyle",
			Home: cfgTempDir, BaseConfigured: configured,
		}
		l := newListener(cfg, noEnv, &syncBuf{}, true, nil)

		if l.cfg.BaseConfigured != configured {
			t.Fatalf("newListener dropped BaseConfigured: cfg had %v, listener holds %v",
				configured, l.cfg.BaseConfigured)
		}
		// Tie the wiring to the OBSERVABLE, not just the field: what the listener
		// will actually print has to differ between the two arms.
		if got := baseAddressOrigin(l.cfg.BaseConfigured); (got == "") != configured {
			t.Fatalf("BaseConfigured=%v must decide whether the transport lines say "+
				"the address was invented; segment was %q", configured, got)
		}
	}
}

// 🔴 AND THE LINE ABOVE newListener, WHICH THE TEST ABOVE STILL DOES NOT COVER.
//
// Extracting newListener made the construction assertable, and I then seeded the
// mutant the review named — `cfg.BaseConfigured = true` inside cmdListen, just
// before the call — and every test above STAYED GREEN. The extraction had not
// closed the hole; it had moved it up one line. That is the whole lesson: a seam
// you can test is not the same as a seam that IS tested, and each fix at one layer
// creates the next layer's gap.
//
// So this drives cmdListen ITSELF end to end. once=true means one successful
// connect and out — no retry loop, no real sleep, no goroutine left behind. It is
// the only test in this file that would notice a mangled hand-off between the
// process entry point and the feature.
func TestCmdListen_HandsTheRealBaseConfiguredToTheListener(t *testing.T) {
	noEnv := func(string) string { return "" }

	for _, tc := range []struct {
		name            string
		configured      bool
		wantSaysGuessed bool
	}{
		{"an address nobody chose is named as invented", false, true},
		{"an address that was configured is never accused", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfgTempDir = t.TempDir()
			srv := httptest.NewServer(stationHandler("9f3c1ab77e40", true))
			defer srv.Close()

			out := &syncBuf{}
			rc := cmdListen(Config{
				Base: srv.URL, Token: "tok", ID: "kyle",
				Home: t.TempDir(), BaseConfigured: tc.configured,
			}, noEnv, true, out)

			if rc != 0 {
				t.Fatalf("listen degrades gracefully; rc = %d\n%s", rc, out.String())
			}
			transcript := out.String()
			if !strings.Contains(transcript, noticeConnected) {
				t.Fatalf("fixture broken: cmdListen never reached the connect line, so "+
					"this test would pass for any wiring at all:\n%s", transcript)
			}
			if got := strings.Contains(transcript, "GUESSED"); got != tc.wantSaysGuessed {
				t.Fatalf("cmdListen(BaseConfigured=%v): line says GUESSED = %v, want %v\n%s",
					tc.configured, got, tc.wantSaysGuessed, transcript)
			}
		})
	}
}
