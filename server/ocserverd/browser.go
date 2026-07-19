package main

// browser.go — first-run browser pop: when serve boots with an outstanding
// claim token (no owner password yet), cmdServe fires a one-shot best-effort
// attempt to open the SPA setup page with the claim code in the query string,
// so the owner only has to pick a password. Everything degrades silently: any
// failure (headless, unsupported OS, opener missing) falls back to printing
// the full clickable URL — never a bare token, never a crash.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// firstRunBrowserDelay gives ListenAndServe (which blocks after the pop is
// scheduled) a beat to bind before the browser hits the page.
const firstRunBrowserDelay = 500 * time.Millisecond

// firstRunSetupURL carries the one-shot claim code to the SPA's FirstRunPage
// (which reads ?code= — hash routing keeps the query segment free). Claim
// tokens are base64url (settings.go), query-safe verbatim.
func firstRunSetupURL(addr, claimToken string) string {
	return fmt.Sprintf("http://%s/?code=%s", addr, claimToken)
}

// browserOpener picks the platform opener command; run is the exec seam for
// tests (the real wiring is runBrowserCommand).
type browserOpener struct {
	goos string
	run  func(name string, arg ...string) error
}

func (b browserOpener) open(url string) error {
	switch b.goos {
	case "darwin":
		return b.run("open", url)
	case "linux":
		return b.run("xdg-open", url)
	default:
		return fmt.Errorf("no browser opener for GOOS %q", b.goos)
	}
}

func runBrowserCommand(name string, arg ...string) error {
	return exec.Command(name, arg...).Run()
}

// shouldAutoOpenBrowser gates the pop to interactive runs: stdout must be a
// TTY (service/headless/piped runs get the printed URL instead), and
// OC_NO_OPEN_BROWSER non-empty is the opt-out valve.
func shouldAutoOpenBrowser(env func(string) string, stdoutTTY bool) bool {
	return stdoutTTY && env("OC_NO_OPEN_BROWSER") == ""
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// popFirstRunBrowser attempts the open and reports the outcome on the serve
// log: success prints a no-token confirmation, any failure prints the full
// clickable URL. Never returns an error — serve must not care.
func popFirstRunBrowser(b browserOpener, setupURL string, out io.Writer) {
	if err := b.open(setupURL); err != nil {
		fmt.Fprintf(out, "[ocserverd]   %s\n", setupURL)
		return
	}
	fmt.Fprintln(out, "[ocserverd] opened the setup page in your browser — choose a password there to claim the server")
}
