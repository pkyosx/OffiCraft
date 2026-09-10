// Skeleton generated from server/ocserverd/main.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
	"testing"
)

const mainUsageText = `usage: ocserverd <subcommand> [flags]
  officraft Go server daemon (plumbing skeleton).

subcommands:
  serve                run the server (must be spelled out): read oc.toml, bind loopback:[server].port
  migrate              apply goose migrations to the resolved [storage] DSN (sqlite)
  backup               take one online snapshot of this instance's database (single consistent file)
  set-password         store the owner password's argon2id hash in DB settings ($OC_NEW_PASSWORD)
  claim-token          print the one-shot first-run claim code (exit 3 once a password is set)
  mfa-disable          clear the owner's TOTP second factor (lost-authenticator recovery)
  migration-lock       --write / --check server/ocserverd/migration.lock (run from that directory)
  theme-name-verdicts  <cases.json> <verdicts.json>: this side of the Go/TS theme-name parity check
  sse-topics           <out.json>: render hub.go's closed SSE topic vocabulary (spec/sse-topics.json)

flags (serve):
  --no-reconcile   do not run the reconcile producer (its half of the
                   cadence tick is skipped, no warden-command dispatch)
                   — the shadow-deploy kill-switch
  --no-outsource   do not run the outsource-assignment scheduler (its half
                   of the cadence tick is skipped, no event-driven
                   assignment) — the --no-reconcile mirror
`

func TestUsage(t *testing.T) {
	var out bytes.Buffer
	usage(&out)

	if out.String() != mainUsageText {
		t.Fatalf("usage output:\n got %q\nwant %q", out.String(), mainUsageText)
	}
}

func TestRealMain(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		code int
		want string
	}{
		{
			name: "no subcommand refuses to start the server and prints the complete usage",
			argv: nil,
			code: 2,
			want: "[ocserverd] no subcommand given — `serve` is no longer implied; nothing was read or written\n\n" + mainUsageText,
		},
		{
			name: "help exits successfully with the complete usage",
			argv: []string{"--help"},
			code: 0,
			want: mainUsageText,
		},
		{
			name: "an unknown subcommand refuses to run and names the requested command",
			argv: []string{"not-a-command"},
			code: 2,
			want: "[ocserverd] unknown subcommand \"not-a-command\"\n\n" + mainUsageText,
		},
		{
			name: "a rescue subcommand with arguments is rejected before it touches the database",
			argv: []string{"migrate", "extra"},
			code: 2,
			want: "[ocserverd] migrate takes no arguments\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if got := realMain(tc.argv, func(string) string { return "" }, &out); got != tc.code {
				t.Fatalf("realMain(%q) = %d, want %d", tc.argv, got, tc.code)
			}
			if out.String() != tc.want {
				t.Fatalf("realMain(%q) output:\n got %q\nwant %q", tc.argv, out.String(), tc.want)
			}
		})
	}
}

func TestParseServeFlags(t *testing.T) {
	cases := []struct {
		name            string
		args            []string
		wantNoReconcile bool
		wantNoOutsource bool
		wantBad         bool
		wantOutput      string
	}{
		{name: "no flags keep both producers enabled"},
		{name: "long flags disable their respective producers", args: []string{"--no-reconcile", "--no-outsource"}, wantNoReconcile: true, wantNoOutsource: true},
		{name: "short aliases disable their respective producers", args: []string{"-no-reconcile", "-no-outsource"}, wantNoReconcile: true, wantNoOutsource: true},
		{name: "repeated flags remain idempotent", args: []string{"--no-reconcile", "--no-reconcile"}, wantNoReconcile: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			gotNoReconcile, gotNoOutsource, gotBad := parseServeFlags(tc.args, &out)
			if gotNoReconcile != tc.wantNoReconcile || gotNoOutsource != tc.wantNoOutsource || gotBad != tc.wantBad {
				t.Fatalf("parseServeFlags(%q) = (%v, %v, %v), want (%v, %v, %v)",
					tc.args, gotNoReconcile, gotNoOutsource, gotBad,
					tc.wantNoReconcile, tc.wantNoOutsource, tc.wantBad)
			}
			if out.String() != tc.wantOutput {
				t.Fatalf("parseServeFlags(%q) output = %q, want %q", tc.args, out.String(), tc.wantOutput)
			}
		})
	}

	var out bytes.Buffer
	noReconcile, noOutsource, bad := parseServeFlags([]string{"--not-a-serve-flag"}, &out)
	if noReconcile || noOutsource || !bad {
		t.Fatalf("unknown serve flag result = (%v, %v, %v), want (false, false, true)", noReconcile, noOutsource, bad)
	}
	want := "[ocserverd] unknown serve flag \"--not-a-serve-flag\"\n\n" + mainUsageText
	if out.String() != want {
		t.Fatalf("unknown serve flag output:\n got %q\nwant %q", out.String(), want)
	}
}
