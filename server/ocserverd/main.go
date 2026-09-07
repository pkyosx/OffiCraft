// Command ocserverd is the officraft server daemon — the production
// implementation (a Go port of the retired Python original, whose history is
// not in this repo — there is no rollback anchor). It carries the oc.toml config reader, the
// HS256 JWT mint/verify, the declarative RouteSpec table + fail-closed boot
// assertions, the REST/SSE/MCP surfaces, the reconcile producer, and the
// goose migration base over modernc.org/sqlite (cgo-free).
//
// Naming (root CLAUDE.md §10): folder server/ocserverd/ = module ocserverd =
// binary ocserverd. Distinct from bin/ocserver, the bash
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
	{"serve", "run the server (must be spelled out): read oc.toml, bind loopback:[server].port"},
	{"migrate", "apply goose migrations to the resolved [storage] DSN (sqlite)"},
	{"backup", "take one online snapshot of this instance's database (single consistent file)"},
	{"set-password", "store the owner password's argon2id hash in DB settings ($OC_NEW_PASSWORD)"},
	{"claim-token", "print the one-shot first-run claim code (exit 3 once a password is set)"},
	{"mfa-disable", "clear the owner's TOTP second factor (lost-authenticator recovery)"},
	{"migration-lock", "--write / --check server/ocserverd/migration.lock (run from that directory)"},
	{"theme-name-verdicts", "<cases.json> <verdicts.json>: this side of the Go/TS theme-name parity check"},
}

// noSubcommand is the internal dispatch key for "the argv named no subcommand"
// — zero arguments, or a leading flag other than -h/--help.
const noSubcommand = "\x00no-subcommand"

func usage(out io.Writer) {
	fmt.Fprintln(out, "usage: ocserverd <subcommand> [flags]")
	fmt.Fprintln(out, "  officraft Go server daemon (plumbing skeleton).")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "subcommands:")
	for _, s := range subcommands {
		fmt.Fprintf(out, "  %-20s %s\n", s.name, s.help)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "flags (serve):")
	fmt.Fprintln(out, "  --no-reconcile   do not run the reconcile producer (its half of the")
	fmt.Fprintln(out, "                   cadence tick is skipped, no warden-command dispatch)")
	fmt.Fprintln(out, "                   — the shadow-deploy kill-switch")
	fmt.Fprintln(out, "  --no-outsource   do not run the outsource-assignment scheduler (its half")
	fmt.Fprintln(out, "                   of the cadence tick is skipped, no event-driven")
	fmt.Fprintln(out, "                   assignment) — the --no-reconcile mirror")
}

// realMain is the testable entrypoint: argv WITHOUT the program name, an env
// accessor, and the output sink. Returns the process exit code (mirrors
// cli/ocagent/main.go realMain).
func realMain(argv []string, env func(string) string, out io.Writer) int {
	// The subcommand must be named. serve is NOT the default any more (T-107):
	// starting a station opens the database, snapshots it, and runs migrations,
	// so a bare `ocserverd` — or one carrying only flags, e.g. a mistyped
	// `ocserverd --no-reconcile` — must not be able to reach that path. Both of
	// those now print the subcommand list and exit 2 without touching anything.
	// The help flags keep their own route (usage, exit 0), and every rescue
	// subcommand is reached exactly as before.
	cmd, rest := noSubcommand, []string(nil)
	switch {
	case len(argv) == 0: // no subcommand
	case argv[0] == "-h" || argv[0] == "--help":
		cmd = "help"
	case argv[0] != "" && argv[0][0] != '-':
		cmd, rest = argv[0], argv[1:]
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

	case "backup":
		if len(rest) != 0 {
			fmt.Fprintln(out, "[ocserverd] backup takes no arguments (the database comes from the resolved DSN)")
			return 2
		}
		return cmdBackup(env, out)

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

	case "mfa-disable":
		if len(rest) != 0 {
			fmt.Fprintln(out, "[ocserverd] mfa-disable takes no arguments (the database comes from the resolved DSN)")
			return 2
		}
		return cmdMFADisable(env, out)

	// migration-lock and theme-name-verdicts are the two DEVELOPMENT subcommands:
	// they touch no database and no config, and they exist because both of them
	// need code that is only reachable from inside package main — the go:embed
	// migration FS and the AST of this package for the first, the theme-bundle
	// validator for the second. Before T-125 both lived in _test.go files behind
	// a build tag for exactly that reason, which made two things that are not
	// tests look like tests. bin/gen-migration-lock, bin/check-migration-lock and
	// frontend/src/lib/themeName.parity.test.ts are their callers.
	case "migration-lock":
		return cmdMigrationLock(rest, out)

	case "theme-name-verdicts":
		return cmdThemeNameVerdicts(rest, out)

	case "-h", "--help", "help":
		usage(out)
		return 0

	case noSubcommand:
		fmt.Fprintln(out, "[ocserverd] no subcommand given — `serve` is no longer implied; nothing was read or written")
		fmt.Fprintln(out, "")
		usage(out)
		return 2

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
			// disables the reconcile producer wholesale — ITS HALF of the one
			// cadence tick AND the event-driven warden-command dispatch it owns.
			// Since T-14 item 5 the loop itself is mounted either way and the
			// flag is read at the call site (runLifecycleTick), so "wholesale"
			// means "that producer does nothing", not "no goroutine exists". The
			// rest of the server runs unchanged; §4.1 enumerates what the flag
			// does NOT cover.
			noReconcile = true
		case "--no-outsource", "-no-outsource":
			// The outsource-scheduler kill-switch (the --no-reconcile mirror,
			// M3 contract §B.4): disables the assignment producer wholesale —
			// its half of the cadence tick AND the event-driven create_task tick
			// — so a shadow server never mints workers against the production
			// queue. Same shape as above: skipped at the call site, never read
			// inside runOutsourceTick (lifecycle_tick.go carries the ruling).
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
