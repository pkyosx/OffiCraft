package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// T-86: the OC_BASE half of the mis-wire guard.
//
// The defect: loadConfig replaces an unset OC_BASE with defaultBase, so every
// subcommand downstream holds an address it was never given. Three subcommands
// now refuse on it; context-report says so and carries on, because its
// fail-safe (always print the status line, always exit 0) is what keeps a
// mis-wired agent's TUI alive and it runs on every turn.
//
// Every test here asks BOTH questions the ticket requires of each message: does
// it NAME the variable, and does it print NO VALUE. The second is the one that
// would rot silently, so it is asserted against a base value planted in the
// Config specifically to be searched for.
// ---------------------------------------------------------------------------

// plantedBase is a value no message has any reason to contain. It is what a
// leak would print: the tests below put it in Config.Base with
// BaseConfigured=false — the exact state loadConfig produces for an unset
// OC_BASE, except that the fallback address is replaced by something a
// substring search can tell apart from every other string in the output.
const plantedBase = "http://planted-base-value.invalid:65535"

func misWiredBase() Config {
	return Config{Base: plantedBase, BaseConfigured: false}
}

// assertNamesBaseWithoutValue is the pair of assertions the ticket asks for on
// every new message: the variable is named, and no value is echoed.
func assertNamesBaseWithoutValue(t *testing.T, subcommand, stderr string) {
	t.Helper()
	if !strings.Contains(stderr, "OC_BASE") {
		t.Errorf("%s: the message must NAME the variable that is missing; got %q",
			subcommand, stderr)
	}
	if !strings.Contains(stderr, "[ocagent] "+subcommand+":") {
		t.Errorf("%s: the message must say which subcommand refused; got %q",
			subcommand, stderr)
	}
	if strings.Contains(stderr, plantedBase) {
		t.Errorf("%s: the message printed the resolved base value. OC_* values must "+
			"never reach a terminal; got %q", subcommand, stderr)
	}
	// The loopback default is a value too, and it is the one a well-meaning
	// "tell them what it fell back to" edit would add.
	if strings.Contains(stderr, "127.0.0.1:7755") {
		t.Errorf("%s: the message printed the built-in fallback address; got %q",
			subcommand, stderr)
	}
}

func TestUploadWithoutConfiguredBaseNamesItAndSendsNothing(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(path, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := misWiredBase()
	cfg.Token = "tok"
	var out, errOut strings.Builder
	rc := cmdUpload(srv.Client(), cfg, path, "", &out, &errOut)

	if rc == 0 {
		t.Errorf("upload with no OC_BASE must exit non-zero, got %d", rc)
	}
	if reached {
		t.Error("upload with no OC_BASE reached a server; it must send nothing at all")
	}
	if out.String() != "" {
		t.Errorf("upload refusal must leave stdout empty (callers capture the "+
			"attachment id from it); got %q", out.String())
	}
	assertNamesBaseWithoutValue(t, "upload", errOut.String())
}

func TestDownloadWithoutConfiguredBaseNamesItAndFetchesNothing(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))
	defer srv.Close()

	cfg := misWiredBase()
	cfg.Token = "tok"
	var out, errOut strings.Builder
	rc := cmdDownload(srv.Client(), cfg, "att-0123456789ab", t.TempDir(), &out, &errOut)

	if rc == 0 {
		t.Errorf("download with no OC_BASE must exit non-zero, got %d", rc)
	}
	if reached {
		t.Error("download with no OC_BASE reached a server; it must fetch nothing at all")
	}
	if out.String() != "" {
		t.Errorf("download refusal must leave stdout empty (callers capture the "+
			"landed path from it); got %q", out.String())
	}
	assertNamesBaseWithoutValue(t, "download", errOut.String())
}

// TestDiffWithoutConfiguredBaseRefusesInsteadOfMintingALoopbackLink is the
// regression this ticket exists for. The refusal message was already written;
// what was missing was any path that reached it, and the observable result was
// a comparison URL pointing at the caller's own machine, printed on stdout with
// exit 0 — a link that says it worked and that nobody else can open.
func TestDiffWithoutConfiguredBaseRefusesInsteadOfMintingALoopbackLink(t *testing.T) {
	client := http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("diff made a request it should not have: %s", r.URL)
		return nil, nil
	})}

	var out, errOut strings.Builder
	rc := cmdDiff(&client, misWiredBase(), "att-0123456789ab", "att-ba9876543210",
		"", "", false, &out, &errOut)

	if rc == 0 {
		t.Errorf("diff with no OC_BASE must exit non-zero, got %d", rc)
	}
	if out.String() != "" {
		t.Errorf("diff with no OC_BASE must print no link at all; got %q", out.String())
	}
	assertNamesBaseWithoutValue(t, "diff", errOut.String())
}

// TestDiffExternalWithoutConfiguredBaseRefusesBeforeMinting covers the other
// flavour: --external asks the server to SIGN a link that never expires, so the
// refusal has to land before the request is made.
func TestDiffExternalWithoutConfiguredBaseRefusesBeforeMinting(t *testing.T) {
	client := http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("diff --external made a request it should not have: %s", r.URL)
		return nil, nil
	})}

	cfg := misWiredBase()
	cfg.Token = "tok"
	var out, errOut strings.Builder
	rc := cmdDiff(&client, cfg, "att-0123456789ab", "att-ba9876543210",
		"", "", true, &out, &errOut)

	if rc == 0 {
		t.Errorf("diff --external with no OC_BASE must exit non-zero, got %d", rc)
	}
	if out.String() != "" {
		t.Errorf("diff --external with no OC_BASE must print nothing; got %q", out.String())
	}
	assertNamesBaseWithoutValue(t, "diff", errOut.String())
}

// TestConfiguredBaseAddsNoMessageAnywhere is the other half of every assertion
// above, and it is the one that keeps the guard from becoming noise: a
// correctly wired agent must see the byte-identical output it saw before. It
// matters most for context-report, which runs on every turn of every
// conversation.
func TestConfiguredBaseAddsNoMessageAnywhere(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var errOut strings.Builder
	var out strings.Builder
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	cmdContextReport(srv.Client(), cfg, func(string) string { return "" }, 1.0,
		strings.NewReader(`{"context_window":{"used_percentage":10}}`), &out, &errOut)

	if strings.Contains(errOut.String(), "OC_BASE") {
		t.Errorf("a correctly wired agent must not be told anything about OC_BASE; "+
			"context-report runs every turn, so this line would accumulate. got %q",
			errOut.String())
	}
}

// TestContextReportWithoutConfiguredBaseKeepsStdoutAndExitCode pins the shape
// Kyle ruled for this subcommand (T-86, option 丙): the signal goes to stderr
// and NOTHING else moves. Both halves are load-bearing — statusLine reads
// stdout, and a non-zero exit here would break the TUI status line on every
// turn — and both are the kind of thing a later edit would "tidy" into a
// refusal without realising what it costs.
func TestContextReportWithoutConfiguredBaseKeepsStdoutAndExitCode(t *testing.T) {
	const payload = `{"context_window":{"used_percentage":10}}`

	// The baseline is not hand-written: it is what THIS build prints on the same
	// input when OC_BASE is configured, so the comparison cannot drift away from
	// the real status line.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var wiredOut, wiredErr strings.Builder
	wiredCfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	wiredRC := cmdContextReport(srv.Client(), wiredCfg, func(string) string { return "" }, 1.0,
		strings.NewReader(payload), &wiredOut, &wiredErr)

	misCfg := misWiredBase()
	misCfg.Token, misCfg.ID, misCfg.Home = "t", "kyle", t.TempDir()
	var misOut, misErr strings.Builder
	misRC := cmdContextReport(srv.Client(), misCfg, func(string) string { return "" }, 1.0,
		strings.NewReader(payload), &misOut, &misErr)

	if misRC != 0 {
		t.Errorf("context-report must still exit 0 with no OC_BASE (the statusLine "+
			"fail-safe); got %d", misRC)
	}
	if wiredRC != 0 {
		t.Fatalf("baseline run did not exit 0 (got %d); the comparison below would "+
			"be meaningless", wiredRC)
	}
	if misOut.String() != wiredOut.String() {
		t.Errorf("context-report's stdout must be byte-identical with and without "+
			"OC_BASE — statusLine reads it. wired=%q mis-wired=%q",
			wiredOut.String(), misOut.String())
	}

	// Only the GUARD line is judged for "prints no value". The lines after it are
	// this subcommand's pre-existing POST-failure diagnostics, which quote the
	// request URL — and should: that is the operator's own configured value shown
	// back to them in a network error, which is how they find the mistake. What
	// must never appear is a value in the line that reports the variable MISSING,
	// because there the value is one this binary invented.
	guardLine, _, found := strings.Cut(misErr.String(), "\n")
	if !found {
		t.Fatalf("expected the guard line on stderr, got %q", misErr.String())
	}
	assertNamesBaseWithoutValue(t, "context-report", guardLine)
}

// TestLoadConfigRecordsWhetherBaseWasConfigured pins the resolver end of the
// guard. Without this, BaseConfigured could be wired to a constant and every
// test above would still pass.
func TestLoadConfigRecordsWhetherBaseWasConfigured(t *testing.T) {
	cases := []struct {
		name       string
		ocBase     string
		configured bool
		base       string
	}{
		{"unset falls back and says so", "", false, defaultBase},
		{"a real station is configured", "https://oc.example.com", true, "https://oc.example.com"},
		{"the loopback address is a legitimate answer, not the fallback",
			"http://127.0.0.1:7755", true, "http://127.0.0.1:7755"},
		// The flag answers "was the fallback taken", NOT "is this base usable".
		// normalizeBase hands back a value it cannot re-scheme unchanged, so this
		// one reaches the subcommands as "http:" and fails on its first request —
		// loudly, with the operator's own value in the error. That is a different
		// failure from the one this ticket closes, and separating them is what
		// keeps the guard out of normalizeBase, a canonical block mirrored in
		// three modules.
		{"a malformed value is still a configuration, not a fallback",
			"http://", true, "http:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadConfig(func(k string) string {
				if k == "OC_BASE" {
					return tc.ocBase
				}
				return ""
			})
			if cfg.BaseConfigured != tc.configured {
				t.Errorf("BaseConfigured = %v, want %v", cfg.BaseConfigured, tc.configured)
			}
			if cfg.Base != tc.base {
				t.Errorf("Base = %q, want %q", cfg.Base, tc.base)
			}
		})
	}
}

// TestZeroConfigurationSurfaces pins the three ways of invoking this binary
// that name no subcommand. `help` is included because a survey once read the
// help OUTPUT, concluded the switch did not accept it, and proposed adding it;
// what the switch accepts is decided here and nowhere else.
func TestZeroConfigurationSurfaces(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		rc   int
	}{
		{"help is accepted and is not an error", []string{"help"}, 0},
		{"--help is accepted and is not an error", []string{"--help"}, 0},
		{"-h is accepted and is not an error", []string{"-h"}, 0},
		{"naming no subcommand at all is an error", nil, 2},
	}
	var usageText strings.Builder
	usage(&usageText)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			// No OC_* at all: none of these surfaces may depend on configuration.
			rc := realMain(tc.argv, func(string) string { return "" },
				strings.NewReader(""), &out)
			if rc != tc.rc {
				t.Errorf("rc = %d, want %d", rc, tc.rc)
			}
			if out.String() != usageText.String() {
				t.Errorf("output = %q, want the usage text %q", out.String(), usageText.String())
			}
			if strings.Contains(out.String(), "OC_BASE") {
				t.Errorf("a surface that needs no configuration must not mention "+
					"OC_BASE; got %q", out.String())
			}
		})
	}
}
