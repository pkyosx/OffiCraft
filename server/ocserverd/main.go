// Command ocserverd is the officraft server daemon — the production
// implementation (a Go port of the retired Python original; historical
// rollback = git tag py-final). It carries the oc.toml config reader, the
// HS256 JWT mint/verify, the declarative RouteSpec table + fail-closed boot
// assertions, the REST/SSE/MCP surfaces, the reconcile producer, and the
// goose migration base over modernc.org/sqlite (cgo-free).
//
// Naming (root CLAUDE.md §10): folder server/ocserverd/ = module ocserverd =
// binary ocserverd. The committed prebuilt lives at bin/ocserverd (CI step 1
// keeps it in parity with this source). Distinct from bin/ocserver, the bash
// server INSTALLER — the "d" is the daemon itself.
package main

import (
	"fmt"
	"io"
	"os"
)

// subcommands is the plumbing-only CLI surface. Kept as data so the usage text
// and the dispatch stay in one place (mirrors cli/ocagent/main.go).
var subcommands = []struct{ name, help string }{
	{"serve", "run the server (default): read oc.toml, bind loopback:[server].port"},
	{"migrate", "apply goose migrations to the resolved [storage] DSN (sqlite)"},
	{"set-password", "store the owner password's argon2id hash in DB settings ($OC_NEW_PASSWORD)"},
	{"claim-token", "print the one-shot first-run claim code (exit 3 once a password is set)"},
}

func usage(out io.Writer) {
	fmt.Fprintln(out, "usage: ocserverd [subcommand] [flags]")
	fmt.Fprintln(out, "  officraft Go server daemon (plumbing skeleton).")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "subcommands:")
	for _, s := range subcommands {
		fmt.Fprintf(out, "  %-13s %s\n", s.name, s.help)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "flags (serve):")
	fmt.Fprintln(out, "  --no-reconcile   do not run the reconcile producer (no cadence loop,")
	fmt.Fprintln(out, "                   no warden-command dispatch) — the shadow-deploy kill-switch")
	fmt.Fprintln(out, "  --no-outsource   do not run the outsource-assignment scheduler (no cadence,")
	fmt.Fprintln(out, "                   no event-driven assignment) — the --no-reconcile mirror")
}

// realMain is the testable entrypoint: argv WITHOUT the program name, an env
// accessor, and the output sink. Returns the process exit code (mirrors
// cli/ocagent/main.go realMain).
func realMain(argv []string, env func(string) string, out io.Writer) int {
	// Default subcommand is serve (the zero-argument canonical start, mirroring
	// bin/serve). A leading flag (e.g. `ocserverd --no-reconcile`) also means
	// serve — except the help flags, which route to usage (exit 0).
	cmd, rest := "serve", argv
	if len(argv) > 0 {
		switch {
		case argv[0] == "-h" || argv[0] == "--help":
			cmd, rest = "help", nil
		case argv[0] != "" && argv[0][0] != '-':
			cmd, rest = argv[0], argv[1:]
		}
	}

	switch cmd {
	case "serve":
		noReconcile, noOutsource, bad := parseServeFlags(rest, out)
		if bad {
			return 2
		}
		return cmdServe(env, noReconcile, noOutsource, out)

	case "migrate":
		if len(rest) != 0 {
			fmt.Fprintln(out, "[ocserverd] migrate takes no arguments")
			return 2
		}
		return cmdMigrate(env, out)

	case "set-password":
		if len(rest) != 0 {
			fmt.Fprintf(out, "[ocserverd] set-password takes no arguments (the password rides $%s)\n", envNewPassword)
			return 2
		}
		return cmdSetPassword(env, out)

	case "claim-token":
		if len(rest) != 0 {
			fmt.Fprintln(out, "[ocserverd] claim-token takes no arguments")
			return 2
		}
		return cmdClaimToken(env, out)

	case "-h", "--help", "help":
		usage(out)
		return 0

	default:
		fmt.Fprintf(out, "[ocserverd] unknown subcommand %q\n\n", cmd)
		usage(out)
		return 2
	}
}

// parseServeFlags parses the serve flag surface by hand (two boolean flags;
// the stdlib FlagSet would print its own usage on error, diverging from
// usage()). Returns (noReconcile, noOutsource, bad-args).
func parseServeFlags(args []string, out io.Writer) (bool, bool, bool) {
	noReconcile := false
	noOutsource := false
	for _, a := range args {
		switch a {
		case "--no-reconcile", "-no-reconcile":
			// The shadow-deployment kill-switch (spec/lifecycle.md Appendix B #1):
			// disables the reconcile producer wholesale — cadence loop AND every
			// event-driven warden-command dispatch — so a shadow server can never
			// wake or kill a real agent. The rest of the server runs unchanged.
			noReconcile = true
		case "--no-outsource", "-no-outsource":
			// The outsource-scheduler kill-switch (the --no-reconcile mirror,
			// M3 contract §B.4): disables the assignment producer wholesale —
			// cadence AND the event-driven create_task tick — so a shadow server
			// never mints workers against the production queue.
			noOutsource = true
		default:
			fmt.Fprintf(out, "[ocserverd] unknown serve flag %q\n\n", a)
			usage(out)
			return false, false, true
		}
	}
	return noReconcile, noOutsource, false
}

func main() {
	os.Exit(realMain(os.Args[1:], os.Getenv, os.Stdout))
}
