package main

// version reports enough to distinguish WHICH build of this binary is running —
// the operational need is "is the ocwarden an eva self-update pulled the same
// version as the one committed in bin/?". Two facts answer that, printed together
// because each covers a gap the other leaves:
//
//   - VCS stamp (vcs.revision / vcs.time / vcs.modified) from debug.ReadBuildInfo().
//     Go 1.18+ `go build` auto-embeds this and it SURVIVES `-ldflags "-s -w"` (strip
//     drops the symbol table / DWARF, not the buildinfo blob — verified empirically).
//     Human-readable "which commit", but only present when the build ran with the
//     repo's `.git` as a DIRECTORY; a git WORKTREE (.git is a file) or a tarball build
//     yields no VCS settings, in which case these lines read "unknown".
//
//   - self-hash: hashPrefix(self bytes) — the SAME content-hash oracle the self-updater
//     uses (selfupdate.go) to decide "the live binary already IS the served one". It is
//     ALWAYS present and is the exact value to eyeball-compare a self-updated binary
//     against the committed bin/ artifact: identical self-hash ⇒ byte-identical build.
//
// Kept OUT of the `run`/install/teardown usage banner on purpose: CI's committed-
// prebuilt parity gate (bin/ci.sh 7d) compares the COMMITTED prebuilt's --help against
// a fresh build's --help; folding a build-varying hash into that banner would flap it.

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// selfHash returns the content-hash prefix of this running binary's own bytes, via
// os.Executable(), reusing selfupdate.go's hashPrefix so the value is directly
// comparable to what the self-updater logs/announces. A "(unavailable: ...)" string
// is returned rather than failing, since the VCS lines may still carry identity.
func selfHash(exe func() (string, error), read func(string) ([]byte, error)) string {
	path, err := exe()
	if err != nil {
		return fmt.Sprintf("unavailable: %v", err)
	}
	data, err := read(path)
	if err != nil {
		return fmt.Sprintf("unavailable: %v", err)
	}
	return hashPrefix(data)
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
	rev, when, modified := "unknown", "unknown", "unknown"
	if info, ok := buildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.time":
				when = s.Value
			case "vcs.modified":
				modified = s.Value
			}
		}
	}
	fmt.Fprintln(out, "ocwarden")
	fmt.Fprintf(out, "  vcs.revision: %s\n", rev)
	fmt.Fprintf(out, "  vcs.time:     %s\n", when)
	fmt.Fprintf(out, "  vcs.modified: %s\n", modified)
	fmt.Fprintf(out, "  self-hash:    %s\n", selfHash(exe, read))
}

// cmdVersion is the dispatch entry: wires the real providers and returns exit 0.
func cmdVersion(out io.Writer) int {
	printVersion(out, debug.ReadBuildInfo, os.Executable, os.ReadFile)
	return 0
}
