package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runtimeprobe_codex_error_test.go — the codex arm must not SWALLOW the
// error it turns into logged_in:false.
//
// WHY THIS FILE EXISTS AT ALL. `codexCap["logged_in"] = err == nil` is correct
// and stays. The defect was everything it did NOT do: err went nowhere, so one
// false stood for four different worlds — signed out, timed out (execRunner's
// 5s subprocessBudget), codex crashed, codex missing. The server's placement
// gate fail-closes on that false, so on 2026-09-05 five members on the server
// host were unstartable for hours while `codex login status` exited 0 in every
// context two agents could construct. Nobody could get further, because the one
// artefact that would have said which world we were in had never been written
// down anywhere.
//
// So the assertion here is not "logged_in is false" — the old code already did
// that. It is "the failure left a trace a human can read", and it is written as
// a test rather than a comment because a comment cannot fail.

// codexLoginFailsRunner stages the exact shape of the incident: `--version`
// SUCCEEDS (so the capability map still carries a version and the entry looks
// healthy at a glance) while `login status` FAILS. Reproducing that asymmetry
// matters — a runner that failed both would also pass a weaker test while
// telling us nothing about the case we actually hit.
type codexLoginFailsRunner struct{ loginErr error }

func (r codexLoginFailsRunner) Run(name string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.HasSuffix(name, "codex") && joined == "--version":
		return "codex-cli 0.153.0", nil
	case strings.HasSuffix(name, "codex") && joined == "login status":
		return "", r.loginErr
	default:
		return "", errors.New("unexpected")
	}
}

// stagedCodexEnv resolves OC_CODEX_BIN only, so the codex arm runs and the
// claude arm stays out of the way.
func stagedCodexEnv(t *testing.T) func(string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("stage a resolvable codex: %v", err)
	}
	return func(key string) string {
		switch key {
		case "OC_CODEX_BIN":
			return bin
		case "HOME":
			return dir
		}
		return ""
	}
}

// TestCodexLoginFailureIsLoggedNotSwallowed is the whole point of this file.
func TestCodexLoginFailureIsLoggedNotSwallowed(t *testing.T) {
	loginErr := errors.New("exit status 1: some stderr the host produced")
	var lines []string
	logf := func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}

	got := collectRuntimeCapabilities(stagedCodexEnv(t), codexLoginFailsRunner{loginErr: loginErr},
		map[string]any{}, logf)

	capability, ok := got["codex"].(map[string]any)
	if !ok {
		t.Fatalf("collector emitted no codex entry: %#v", got)
	}
	// Precondition: without these two the arm under test never ran and every
	// assertion below would pass vacuously.
	if installed, _ := capability["installed"].(bool); !installed {
		t.Fatalf("precondition: the staged codex did not resolve; capability = %#v", capability)
	}
	if version, _ := capability["version"].(string); version != "0.153.0" {
		t.Fatalf("precondition: --version must still succeed (that is the incident's "+
			"shape); capability = %#v", capability)
	}
	// The verdict itself is unchanged behaviour, asserted so a future edit
	// cannot "fix" the logging by making the probe optimistic instead.
	if loggedIn, _ := capability["logged_in"].(bool); loggedIn {
		t.Fatalf("a failed `login status` must still report logged_in:false; capability = %#v",
			capability)
	}
	if len(lines) == 0 {
		t.Fatal("the login-status failure was swallowed: logged_in went false and NOTHING " +
			"was logged, which is exactly the state that left five members unstartable " +
			"with no readable cause")
	}
	joined := strings.Join(lines, "\n")
	// The err's TEXT is the payload. A log line that merely says "probe failed"
	// would satisfy a len>0 check while reproducing the original problem.
	if !strings.Contains(joined, loginErr.Error()) {
		t.Fatalf("the log line must carry the error text (that is the only thing nobody "+
			"had ever seen); got %q", joined)
	}
}

// TestCodexLoginFailureWithNilLogfDoesNotPanic pins the nil-skip: every test
// caller passes nil, and a probe that panicked without a logger would take the
// whole warden heartbeat down.
func TestCodexLoginFailureWithNilLogfDoesNotPanic(t *testing.T) {
	got := collectRuntimeCapabilities(stagedCodexEnv(t),
		codexLoginFailsRunner{loginErr: errors.New("boom")}, map[string]any{}, nil)
	capability, ok := got["codex"].(map[string]any)
	if !ok {
		t.Fatalf("collector emitted no codex entry: %#v", got)
	}
	if loggedIn, _ := capability["logged_in"].(bool); loggedIn {
		t.Fatalf("capability = %#v", capability)
	}
}
