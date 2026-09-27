// Command ocserverd is the officraft server daemon. Not to be confused with
// bin/ocserver, the bash server INSTALLER.
package main

import (
	"fmt"
	"io"
	"os"
)

var subcommands = []struct{ name, help string }{
	{"serve", "run the server (must be spelled out): read oc.toml, bind loopback:[server].port"},
	{"migrate", "apply goose migrations to the resolved [storage] DSN (sqlite)"},
	{"backup", "take one online snapshot of this instance's database (single consistent file)"},
	{"set-password", "store the owner password's argon2id hash in DB settings ($OC_NEW_PASSWORD)"},
	{"claim-token", "print the one-shot first-run claim code (exit 3 once a password is set)"},
	{"mfa-disable", "clear the owner's TOTP second factor (lost-authenticator recovery)"},
	{"migration-lock", "--write / --check server/ocserverd/migration.lock (run from that directory)"},
	{"theme-name-verdicts", "<cases.json> <verdicts.json>: this side of the Go/TS theme-name parity check"},
	{"sse-topics", "<out.json>: render hub.go's closed SSE topic vocabulary (spec/sse-topics.json)"},
}

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

func realMain(argv []string, env func(string) string, out io.Writer) int {
	// serve must be named: starting a station opens, snapshots and migrates
	// the database, so a bare `ocserverd` — or flags only, e.g. a mistyped
	// `ocserverd --no-reconcile` — must not reach it.
	cmd, rest := noSubcommand, []string(nil)
	switch {
	case len(argv) == 0:
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

	// Development subcommands: they touch no database or config, and live
	// here because they need package-main-only code (the go:embed migration
	// FS and this package's AST; the theme-bundle validator; the sseTopics
	// map). Callers: bin/gen-migration-lock, bin/check-migration-lock,
	// frontend/src/lib/themeName.parity.test.ts, bin/gen-sse-topics.
	case "migration-lock":
		return cmdMigrationLock(rest, out)

	case "theme-name-verdicts":
		return cmdThemeNameVerdicts(rest, out)

	case "sse-topics":
		return cmdSSETopics(rest, out)

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

// Parsed by hand: a stdlib FlagSet would print its own usage on error,
// diverging from usage().
func parseServeFlags(args []string, out io.Writer) (bool, bool, bool) {
	noReconcile := false
	noOutsource := false
	for _, a := range args {
		switch a {
		case "--no-reconcile", "-no-reconcile":
			// Shadow-deployment kill-switch (spec/lifecycle.md Appendix B
			// #1): the reconcile producer does nothing (its half of the
			// cadence tick and the warden-command dispatch); read at the call
			// site (runLifecycleTick). §4.1 lists what it does NOT cover.
			noReconcile = true
		case "--no-outsource", "-no-outsource":
			// The --no-reconcile mirror for the outsource scheduler, so a
			// shadow server never mints workers against the production queue;
			// skipped at the call site, never read inside runOutsourceTick.
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
