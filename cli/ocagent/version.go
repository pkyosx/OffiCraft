package main

// version reports enough to distinguish WHICH build of this binary is running —
// the operational need is "is the ocagent eva self-update pulled the same version
// as the one committed in bin/?". Two facts answer that, and we print BOTH because
// each covers a gap the other leaves:
//
//   - VCS stamp (vcs.revision / vcs.time / vcs.modified) from debug.ReadBuildInfo().
//     Go 1.18+ `go build` auto-embeds this and it SURVIVES `-ldflags "-s -w"` (strip
//     drops the symbol table / DWARF, not the buildinfo blob — verified empirically).
//     Human-readable "which commit" — WHEN IT IS THERE, WHICH ON THE SHIPPING PATH
//     IT IS NOT. Measured on the binary the warden actually hands every agent
//     (~/.officraft/warden/ocagent, the bindist copy bin/build-bindist produced on
//     the deploy path): build.sha d45b94bc, and no vcs settings at all. That is the
//     population — nobody runs a binary built anywhere else. Two other shapes were
//     measured only to bound when a stamp CAN appear: a `git clone --no-local` build
//     carries a real revision/time/modified, a git WORKTREE build (.git is a file)
//     carries none. The shipped one behaves like the second.
//
//     So when there are none the three lines are NOT PRINTED — same rule the
//     connection line's [station …] / [agent …] segments follow. Printing three
//     "unknown"s beside one real fact, which is what every agent saw, reads as a
//     malfunction; a reader forced to decide which of four lines to believe is
//     worse off than one shown only the facts this build actually has.
//
//   - self-hash: sha256(os.Executable())[:12]. This is the SAME content-hash oracle
//     the self-updater uses (selfupdate.go hashPrefix) to decide "the live binary
//     already IS the served one". It is ALWAYS present (any built binary can read its
//     own bytes) and is the exact value to eyeball-compare a self-updated binary
//     against the committed bin/ artifact: identical self-hash ⇒ byte-identical build.
//
// The subcommand IS listed in `usage()`/--help: a build-identity command nobody
// can discover answers nobody's question.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

// selfHashPrefixLen mirrors ocwarden's selfUpdateHashPrefixLen: the first 12 hex
// chars of sha256 are enough to eyeball "which build" without the full digest.
const selfHashPrefixLen = 12

// selfHash returns the first selfHashPrefixLen hex chars of sha256 of this running
// binary's own bytes, via os.Executable(). This is the content-hash the self-updater
// compares against; printing it lets a human confirm two binaries are byte-identical.
// A "(unavailable: ...)" string is returned rather than failing the whole command,
// because the VCS lines may still carry useful identity.
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

// printVersion writes the version block and is the testable core of the `version`
// subcommand. buildInfo is injected (debug.ReadBuildInfo) so tests drive it without
// depending on how the test binary itself was stamped.
func printVersion(
	out io.Writer,
	buildInfo func() (*debug.BuildInfo, bool),
	exe func() (string, error),
	read func(string) ([]byte, error),
) {
	fmt.Fprintln(out, "ocagent")
	// build.sha is the link-time stamp bin/build-bindist applies, and it is the
	// line the connection line quotes. It is here because vcs.revision above goes
	// "unknown" in exactly the builds this fleet ships — a git WORKTREE (.git is a
	// file) or a tarball yields no VCS settings, as this file's own header says —
	// so without it `ocagent version` would know LESS about the running build than
	// its own log line does, and this is the first place a person looks.
	fmt.Fprintf(out, "  build.sha:    %s\n", buildSHAOrUnstamped(buildInfo))
	for _, line := range vcsLines(buildInfo) {
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "  self-hash:    %s\n", selfHash(exe, read))
}

// vcsLines renders the VCS stamp Go auto-embeds, or NOTHING when this build shape
// carries none.
//
// 🔴 THE ABSENT CASE IS THE SHIPPED CASE, AND IT USED TO LIE. The measurement that
// decides this is not "does some tree shape produce a stamp" — a `git clone` build
// does, a worktree build does not, and neither is what anyone runs. It is what
// ~/.officraft/warden/ocagent, the copy the warden hands every agent, reports: one
// real build.sha under three lines of "unknown". Four lines, one true. The reader's
// most likely conclusion is "this binary is broken" — the opposite of what the block
// exists to say — and that is what was in front of the whole fleet, not an edge case.
//
// So: no value ⇒ the segment is not printed at all. That is the SAME rule the
// connection line applies to its [station …] and [agent …] segments (listen_run.go)
// — absent is said by silence, never by a placeholder that looks like an answer.
// Per-key rather than all-or-nothing, and TrimSpace like the other two, so a
// partially stamped build shows exactly the keys it really has.
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

// buildSHAOrUnstamped names the absent case rather than printing an empty field.
// "unstamped" is a FACT about this binary — it was not built by bin/build-bindist
// — and it is the same fact the connection line states by omitting its segment.
// The two must not disagree about what absent means, so both trim first.
//
// 🔴 IT TAKES buildInfo SO THAT A FALLBACK CANNOT HIDE FROM ITS TEST. This line is
// the oracle bin/tests/agent-build-sha-guard.sh asks — it compares this value to
// the tree's sha precisely because grepping the binary cannot (Go auto-embeds
// vcs.revision, and the short sha is a prefix of it). Review measured what that
// buys if the function reaches for vcs.revision itself when buildSHA is empty:
// `ocagent version` reports the right sha, the connection line prints no segment
// at all because buildSHA really is empty, and BOTH new defences go blind at once
// — guard 11 ok, go test rc=0. The parameter exists so that case is reachable from
// a test; the answer must stay "unstamped" however much VCS metadata is lying
// around, because the question is "did bin/build-bindist stamp this", not "can
// this binary guess which commit it came from".
func buildSHAOrUnstamped(buildInfo func() (*debug.BuildInfo, bool)) string {
	if sha := strings.TrimSpace(buildSHA); sha != "" {
		return sha
	}
	_ = buildInfo
	return "unstamped (not built by bin/build-bindist)"
}

// cmdVersion is the dispatch entry: wires the real providers and returns exit 0.
func cmdVersion(out io.Writer) int {
	printVersion(out, debug.ReadBuildInfo, os.Executable, os.ReadFile)
	return 0
}
