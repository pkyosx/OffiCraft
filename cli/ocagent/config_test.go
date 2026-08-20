package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// makeJWT forges an UNSIGNED-payload JWT (header.payload.sig) whose payload
// carries the given claims — enough to exercise jwtSub, which never verifies.
func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return "h." + payload + ".s"
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg := loadConfig(func(string) string { return "" })
	if cfg.Base != defaultBase {
		t.Errorf("Base = %q, want %q", cfg.Base, defaultBase)
	}
	if cfg.Token != "" || cfg.ID != "" {
		t.Errorf("Token/ID should be empty on a bare env, got %q/%q", cfg.Token, cfg.ID)
	}
}

func TestLoadConfigTrimsBaseAndDerivesID(t *testing.T) {
	tok := makeJWT(t, map[string]any{"sub": "kyle"})
	env := map[string]string{
		"OC_BASE":  "https://oc.example.com/",
		"OC_TOKEN": tok,
	}
	cfg := loadConfig(func(k string) string { return env[k] })
	if cfg.Base != "https://oc.example.com" {
		t.Errorf("Base trailing slash not trimmed: %q", cfg.Base)
	}
	if cfg.ID != "kyle" {
		t.Errorf("ID should derive from JWT sub, got %q", cfg.ID)
	}
}

func TestLoadConfigExplicitIDWins(t *testing.T) {
	tok := makeJWT(t, map[string]any{"sub": "kyle"})
	env := map[string]string{"OC_TOKEN": tok, "OC_ID": "explicit"}
	cfg := loadConfig(func(k string) string { return env[k] })
	if cfg.ID != "explicit" {
		t.Errorf("explicit OC_ID should win, got %q", cfg.ID)
	}
}

func TestJWTSubMalformed(t *testing.T) {
	for _, tok := range []string{"", "not-a-jwt", "a.b", "a.b.c.d", "h..s"} {
		if got := jwtSub(tok); got != "" {
			t.Errorf("jwtSub(%q) = %q, want empty", tok, got)
		}
	}
}

func TestUsageListsAllPlaneA(t *testing.T) {
	var b strings.Builder
	usage(&b)
	for _, s := range planeASubcommands {
		if !strings.Contains(b.String(), s.name) {
			t.Errorf("usage missing subcommand %q", s.name)
		}
	}
}

// goldenUsage is the ENTIRE text `usage()` writes, spelled out here as a literal.
//
// 🔴 IT IS A LITERAL ON PURPOSE, AND IT MUST NOT BE BUILT FROM planeASubcommands.
// The expectation has to be able to disagree with the code. An expectation
// assembled by walking the same slice the renderer walks moves whenever the slice
// moves, so it can never report that the slice moved — which is the one regression
// this package exists to catch (a `version` entry that quietly goes away again).
//
// The previous version of this test asked only `strings.Contains(got, "version")`.
// A partial keyword match is not a test of a fixed output, and two mutants proved
// it did not even hold the line it claimed to hold:
//
//   - rename the help entry to `version-info` and leave dispatch alone: the
//     substring "version" is still there, so it passed — while --help advertised a
//     name realMain does not accept.
//   - delete the `version` entry AND word `upload`'s help as "prints the att id and
//     version": the substring is still there, so it passed — with no `version` in
//     the subcommand list at all, i.e. exactly the regression it was added for.
//
// Comparing the whole output verbatim closes both of those, and any wording drift
// with them. When this fails because the help text legitimately changed, update the
// literal by hand — that edit is the review moment.
const goldenUsage = `usage: ocagent <subcommand> [flags]
  officraft agent-runtime (Plane A) thin shell.

subcommands:
  listen          hold the SSE downlink: chat (refetch) + work wakes
  context-report  statusLine reporter: stdin statusLine JSON → POST /api/agent/context
  suicide         self-terminate: kill my own tmux session (OC_SESSION) → SSE drops → offline
  download        fetch a chat attachment blob to a local file (streaming; --out <dir>)
  upload          stream a local file into the attachment store (prints the att id; --mime <type>)
  clean           get rid of a file or folder I made: quarantines it under my workdir (never rm)
  version         print this build's identity: build.sha, VCS stamp when present, self-hash
`

// TestUsagePrintsExactlyTheAdvertisedText pins the full help text, byte for byte,
// on every path that reaches it without being asked to do anything else.
//
// Those paths are checked together because they are the surfaces a person or an
// agent lands on when they are asking "what can this thing do?" — the bare
// invocation and the three help spellings — and a change that reached only some of
// them would leave the rest answering a different question. Their exit codes are
// pinned too: bare is a usage ERROR (2), an explicit help request is not (0).
func TestUsagePrintsExactlyTheAdvertisedText(t *testing.T) {
	var direct strings.Builder
	usage(&direct)
	if direct.String() != goldenUsage {
		t.Errorf("usage() output changed.\n--- got ---\n%s\n--- want ---\n%s", direct.String(), goldenUsage)
	}

	render := func(argv []string) (int, string) {
		var b strings.Builder
		rc := realMain(argv, func(string) string { return "" }, strings.NewReader(""), &b)
		return rc, b.String()
	}
	for _, tc := range []struct {
		name   string
		argv   []string
		wantRC int
	}{
		{"bare invocation", nil, 2},
		{"--help", []string{"--help"}, 0},
		{"-h", []string{"-h"}, 0},
		{"help", []string{"help"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc, got := render(tc.argv)
			if got != goldenUsage {
				t.Errorf("help text differs from the golden.\n--- got ---\n%s\n--- want ---\n%s", got, goldenUsage)
			}
			if rc != tc.wantRC {
				t.Errorf("exit code = %d, want %d", rc, tc.wantRC)
			}
		})
	}
}

// TestUnknownSubcommandPrintsExactlyTheUnknownBlock pins the OTHER fixed output
// realMain owns: the default arm. Its first line is not decoration —
// seeds/system_interaction.md 附錄 A tells the reader that an ocagent too old to
// know `clean` 「會回 `unknown subcommand`」 and that the right move is to skip
// the item, so the exact phrase is a promise the seed makes on this binary's
// behalf. Until now nothing asserted it POSITIVELY: the only mention of it
// anywhere in the tests was a NEGATIVE `strings.Contains(out, "unknown
// subcommand")` in clean_test.go, which a rename turns vacuously true.
//
// 🔴 THE EXPECTATION IS A LITERAL, AND IT MUST STAY ONE. Not
// fmt.Sprintf(...%q..., name), and not anything derived from planeASubcommands —
// an expectation assembled from the format string the production code uses moves
// with the production code, which is precisely how the phrase slipped out of
// coverage. goldenUsage is fine to concatenate: it is a literal in this file too.
//
// Two mutants this closes, both of which were fully GREEN before it existed:
//
//   - default arm alone: `unknown subcommand` → `unrecognised subcommand`. The
//     whole package passed while the seed's instruction became wrong. Worse, the
//     same one-word edit silently DISARMED
//     TestEveryAdvertisedSubcommandActuallyDispatches below — that test builds its
//     control with the same format string, so the control stopped matching real
//     output and its equality check became false for every subcommand.
//   - that rename PLUS deleting `case "suicide":` from the switch, leaving --help
//     still advertising it. Also green: the dispatch test was already disarmed by
//     the first half, and `suicide` is the one advertised subcommand with no
//     realMain-level test of its own to catch it (the other six each have one).
//
// So this is not only a pin on the phrase — it is what makes the dispatch test's
// silence trustworthy. If its control ever stops describing real output, this
// test reddens first and says why.
func TestUnknownSubcommandPrintsExactlyTheUnknownBlock(t *testing.T) {
	for _, tc := range []struct {
		argv string
		// want is spelled out here; see the 🔴 above before "simplifying" it.
		want string
	}{
		{"nope", "[ocagent] unknown subcommand \"nope\"\n\n" + goldenUsage},
		// A flag-shaped token is not special either: --version/-v are real cases,
		// so an unrecognised one has to land in the same arm and say the same thing.
		{"--nope", "[ocagent] unknown subcommand \"--nope\"\n\n" + goldenUsage},
	} {
		t.Run(tc.argv, func(t *testing.T) {
			var b strings.Builder
			rc := realMain([]string{tc.argv}, func(string) string { return "" }, strings.NewReader(""), &b)
			if got := b.String(); got != tc.want {
				t.Errorf("unknown-subcommand output changed.\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
			}
			if rc != 2 {
				t.Errorf("exit code = %d, want 2 (an unknown subcommand is a usage error)", rc)
			}
		})
	}
}

// TestEveryAdvertisedSubcommandActuallyDispatches is the other half, and it is a
// LOGIC assertion rather than a text one: every name the help text advertises must
// be a name realMain really accepts.
//
// 🔴 THE GOLDEN ABOVE CANNOT COVER THIS. A mutant that renames the help entry to
// `version-info` without touching the switch leaves --help advertising a
// subcommand the binary answers "unknown subcommand" to; and a mutant that renames
// only the switch case leaves the golden perfectly happy while the advertised name
// stops working. The list and the dispatch are two things and they have to be
// checked against each other, not each against itself.
//
// probeArg is a flag no subcommand defines, and what it buys differs by arm:
//
//   - listen / context-report / suicide / download / upload / clean each build a
//     flag.FlagSet(ContinueOnError) and parse the remaining args BEFORE doing any
//     work, so the probe makes them fail fast — no SSE connect, no tmux kill, no
//     network, no file moved.
//   - `version` / `--version` / `-v` is NOT such an arm: it has no FlagSet and
//     ignores the rest of argv entirely, so this test really does run cmdVersion.
//     That is acceptable rather than accidental — cmdVersion only reads build
//     metadata and self-hashes the running binary, so it touches nothing outside
//     the process — but it means the probe is not what protects that arm.
//
// The default (unknown-subcommand) arm never looks at flags at all, so it still
// prints its full block, and that block — the whole of it, verbatim — is the
// thing being ruled out. Note the control below is the arm's LIVE rendering, so
// this test alone cannot notice the block changing; that is
// TestUnknownSubcommandPrintsExactlyTheUnknownBlock's job, above.
func TestEveryAdvertisedSubcommandActuallyDispatches(t *testing.T) {
	const probeArg = "--ocagent-no-such-flag"

	for _, s := range planeASubcommands {
		t.Run(s.name, func(t *testing.T) {
			var b strings.Builder
			realMain([]string{s.name, probeArg}, func(string) string { return "" }, strings.NewReader(""), &b)

			// The control is the unknown arm's REAL rendering, not goldenUsage:
			// this test is about dispatch, and it must keep working while the help
			// text is what is broken. (Built from goldenUsage it went blind exactly
			// when a renamed entry changed usage() — the M3 case — because the stale
			// literal then no longer matched the block the default arm printed.)
			var u strings.Builder
			usage(&u)
			unknown := fmt.Sprintf("[ocagent] unknown subcommand %q\n\n", s.name) + u.String()
			if b.String() == unknown {
				t.Errorf("--help advertises %q, but realMain(%q) falls through to the "+
					"unknown-subcommand arm. The help text is a promise about what this "+
					"binary accepts; an entry the switch does not handle makes it a lie.",
					s.name, s.name)
			}
		})
	}
}

func TestRealMainListenMisWireExitsZero(t *testing.T) {
	// listen is now implemented (Phase 4). With no OC_ID/OC_TOKEN it degrades to one
	// quiet line + exit 0 (the mis-wire guard, mirroring cmd_listen) — never the old
	// "not implemented" stub. The full SSE behaviour is covered in listen_test.go.
	env := func(string) string { return "" }
	var b strings.Builder
	if rc := realMain([]string{"listen"}, env, strings.NewReader(""), &b); rc != 0 {
		t.Errorf("listen mis-wire rc = %d, want 0", rc)
	}
	if !strings.Contains(b.String(), "no OC_ID/OC_TOKEN") {
		t.Errorf("listen mis-wire should print the guard line, got %q", b.String())
	}
}

func TestRealMainNoArgs(t *testing.T) {
	var b strings.Builder
	if rc := realMain(nil, func(string) string { return "" }, strings.NewReader(""), &b); rc != 2 {
		t.Errorf("no-args rc = %d, want 2", rc)
	}
	if !strings.Contains(b.String(), "usage:") {
		t.Errorf("no-args should print usage, got %q", b.String())
	}
}
