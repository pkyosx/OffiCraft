package main

// version prints two build identities:
//   - the VCS stamp from debug.ReadBuildInfo(): survives `-ldflags "-s -w"` (verified empirically),
//     but absent — the lines read "unknown" — when built from a git worktree (.git is a file) or a
//     tarball.
//   - self-hash: the same hashPrefix content oracle the self-updater (selfupdate.go) uses, always
//     present; identical self-hash ⇒ byte-identical to the committed bin/ artifact.

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

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

func cmdVersion(out io.Writer) int {
	printVersion(out, debug.ReadBuildInfo, os.Executable, os.ReadFile)
	return 0
}
