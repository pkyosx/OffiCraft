package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// firstRunBrowserDelay: ListenAndServe blocks after the pop is scheduled, so
// this gives it a beat to bind before the browser hits the page.
const firstRunBrowserDelay = 500 * time.Millisecond

// firstRunSetupURL: the SPA's FirstRunPage reads ?code= (hash routing keeps the
// query free); claim tokens are base64url, so query-safe verbatim. The scheme
// goes through schemeForHost (base_scheme_t78.go), not a literal, so it stays
// inside the mirror-guarded canonical block.
func firstRunSetupURL(addr, claimToken string) string {
	return fmt.Sprintf("%s://%s/?code=%s", schemeForHost(addr), addr, claimToken)
}

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

func shouldAutoOpenBrowser(env func(string) string, stdoutTTY bool) bool {
	return stdoutTTY && env("OC_NO_OPEN_BROWSER") == ""
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func popFirstRunBrowser(b browserOpener, setupURL string, out io.Writer) {
	if err := b.open(setupURL); err != nil {
		fmt.Fprintf(out, "[ocserverd]   %s\n", setupURL)
		return
	}
	fmt.Fprintln(out, "[ocserverd] opened the setup page in your browser — choose a password there to claim the server")
}
