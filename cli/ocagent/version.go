package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

// Mirrors ocwarden selfupdate.go's selfUpdateHashPrefixLen, the self-updater's
// "already the served binary" oracle: equal self-hash ⇒ byte-identical build.
const selfHashPrefixLen = 12

func selfHash(exe func() (string, error), read func(string) ([]byte, error)) string {
	path, err := exe()
	if err != nil {
		return fmt.Sprintf("unavailable: %v", err)
	}
	data, err := read(path)
	if err != nil {
		return fmt.Sprintf("unavailable: %v", err)
	}
	sum := sha256.Sum256(data)
	full := hex.EncodeToString(sum[:])
	if len(full) > selfHashPrefixLen {
		return full[:selfHashPrefixLen]
	}
	return full
}

func printVersion(
	out io.Writer,
	buildInfo func() (*debug.BuildInfo, bool),
	exe func() (string, error),
	read func(string) ([]byte, error),
) {
	fmt.Fprintln(out, "ocagent")
	fmt.Fprintf(out, "  build.sha:    %s\n", buildSHAOrUnstamped(buildInfo))
	for _, line := range vcsLines(buildInfo) {
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "  self-hash:    %s\n", selfHash(exe, read))
}

// Measured: the binary the warden hands every agent (~/.officraft/warden/ocagent,
// from bin/build-bindist) carries NO vcs settings, and three "unknown" lines beside
// one real build.sha read as a broken binary. So a missing key prints no line —
// the same rule as the connection line's segments (listen_run.go).
func vcsLines(buildInfo func() (*debug.BuildInfo, bool)) []string {
	info, ok := buildInfo()
	if !ok || info == nil {
		return nil
	}
	labels := []struct{ key, label string }{
		{"vcs.revision", "  vcs.revision: "},
		{"vcs.time", "  vcs.time:     "},
		{"vcs.modified", "  vcs.modified: "},
	}
	var lines []string
	for _, l := range labels {
		for _, s := range info.Settings {
			if s.Key != l.key {
				continue
			}
			if v := strings.TrimSpace(s.Value); v != "" {
				lines = append(lines, l.label+v)
			}
			break
		}
	}
	return lines
}

// "Absent" must mean the same here as in the connection line's [agent …]
// segment (listen_run.go), so both trim first.
//
// 🔴 buildInfo is taken and ignored ON PURPOSE, so a test can prove there is no
// vcs.revision fallback. Measured: such a fallback blinds both
// bin/tests/agent-build-sha-guard.sh and the connection line at once (guard ok,
// go test rc=0). The question is "did bin/build-bindist stamp this".
func buildSHAOrUnstamped(buildInfo func() (*debug.BuildInfo, bool)) string {
	if sha := strings.TrimSpace(buildSHA); sha != "" {
		return sha
	}
	_ = buildInfo
	return "unstamped (not built by bin/build-bindist)"
}

func cmdVersion(out io.Writer) int {
	printVersion(out, debug.ReadBuildInfo, os.Executable, os.ReadFile)
	return 0
}
