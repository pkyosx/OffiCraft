package main

import (
	"bytes"
	"errors"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// TestVersionSubcommand asserts the `version` alias set prints a NON-EMPTY build
// identifier and exits 0 — the operational contract Seth needs ("easily distinguish
// if the cli is the right version"). Runs via realMain so the dispatch wiring is
// covered, not just printVersion in isolation.
func TestVersionSubcommand(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			rc := realMain([]string{arg}, func(string) string { return "" }, strings.NewReader(""), &out)
			if rc != 0 {
				t.Fatalf("%s: exit code = %d, want 0", arg, rc)
			}
			got := out.String()
			if !strings.Contains(got, "ocagent") {
				t.Errorf("%s: output missing binary name; got:\n%s", arg, got)
			}
			// self-hash must always be present and non-empty (it never depends on VCS
			// stamping), which is the always-available identity line.
			if !strings.Contains(got, "self-hash:") {
				t.Errorf("%s: output missing self-hash line; got:\n%s", arg, got)
			}
		})
	}
}

// TestPrintVersionReadsVCS asserts a stamped build's vcs.revision is surfaced, using
// an injected BuildInfo so the assertion doesn't depend on how the test binary was
// built (worktree test runs are unstamped).
func TestPrintVersionReadsVCS(t *testing.T) {
	var out bytes.Buffer
	bi := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeefcafe"},
			{Key: "vcs.time", Value: "2026-07-10T00:00:00Z"},
			{Key: "vcs.modified", Value: "false"},
		}}, true
	}
	exe := func() (string, error) { return "/proc/self/fake", nil }
	read := func(string) ([]byte, error) { return []byte("binary-bytes"), nil }
	printVersion(&out, bi, exe, read)
	got := out.String()
	if !strings.Contains(got, "deadbeefcafe") {
		t.Errorf("vcs.revision not surfaced; got:\n%s", got)
	}
	// self-hash of the fixed bytes must be a stable non-empty prefix.
	if !strings.Contains(got, "self-hash:") || strings.Contains(got, "self-hash:    \n") {
		t.Errorf("self-hash empty; got:\n%s", got)
	}
}

// TestSelfHashDeterministic asserts identical bytes hash identically (the byte-parity
// oracle a human relies on) and unavailable-executable degrades gracefully.
func TestSelfHashDeterministic(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte("same-bytes"), nil }
	exe := func() (string, error) { return "x", nil }
	a := selfHash(exe, read)
	b := selfHash(exe, read)
	if a != b {
		t.Errorf("self-hash not deterministic: %q vs %q", a, b)
	}
	if len(a) != selfHashPrefixLen {
		t.Errorf("self-hash prefix len = %d, want %d", len(a), selfHashPrefixLen)
	}
}

// TestPrintVersion_ReportsWhetherThisBuildWasStamped covers the build.sha line,
// which shipped with no test at all: deleting the Fprintf that prints it left the
// whole package green and the shell guard green.
//
// 🔴 IT IS ALSO AN ORACLE, NOT JUST A DISPLAY. bin/tests/agent-build-sha-guard.sh
// asks a freshly built ocagent for this value and compares it to the tree's sha,
// precisely because grepping the binary CANNOT do that job — Go auto-embeds
// vcs.revision (the full sha) when built from a clone, and the short sha is a
// prefix of it, so a substring search matches an unstamped build too. That guard
// is only as good as this line, so the two renderings are pinned here.
//
// Both go through the same TrimSpace as the connection line: the two must not
// disagree about what counts as absent, which is a disagreement no reader would
// find until they were already confused about something else.
func TestPrintVersion_ReportsWhetherThisBuildWasStamped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stamp string
		want  string
	}{
		{"stamped", "cafe1234beef", "  build.sha:    cafe1234beef"},
		{"unstamped", "", "  build.sha:    unstamped (not built by bin/build-bindist)"},
		{"blank is not a stamp", "  \t ", "  build.sha:    unstamped (not built by bin/build-bindist)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := buildSHA
			buildSHA = tc.stamp
			t.Cleanup(func() { buildSHA = prev })

			var out bytes.Buffer
			printVersion(&out, func() (*debug.BuildInfo, bool) { return nil, false },
				func() (string, error) { return "", errors.New("no exe") },
				func(string) ([]byte, error) { return nil, errors.New("no read") })

			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("version output must contain %q; got:\n%s", tc.want, out.String())
			}
		})
	}
}

// TestBuildSHAOrUnstamped_NeverGuessesFromVCSMetadata is the case the guard's
// oracle rests on. build.sha answers ONE question — did bin/build-bindist stamp
// this binary — and a build with no stamp is unstamped no matter how much VCS
// metadata Go embedded alongside it.
//
// 🔴 Review made this concrete: with buildSHAOrUnstamped falling back to
// vcs.revision, `ocagent version` reported the correct sha while buildSHA was
// empty, so the connection line printed no [agent …] segment at all — and the
// shell guard, which asks version for exactly this value, went green on a fleet
// that could not name itself. Both defences blind at once, from one plausible
// two-line "improvement".
func TestBuildSHAOrUnstamped_NeverGuessesFromVCSMetadata(t *testing.T) {
	prev := buildSHA
	buildSHA = ""
	t.Cleanup(func() { buildSHA = prev })

	withRevision := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			{Key: "vcs.modified", Value: "false"},
		}}, true
	}
	if got := buildSHAOrUnstamped(withRevision); got != "unstamped (not built by bin/build-bindist)" {
		t.Errorf("build.sha = %q with an EMPTY stamp and vcs.revision present. It must "+
			"stay unstamped: the connection line reads the same empty buildSHA and "+
			"prints nothing, so any other answer here makes `ocagent version` and the "+
			"line disagree — and the build-sha guard trusts this value to tell it "+
			"whether bin/build-bindist ran at all", got)
	}
}

// ── The VCS block: present when real, absent when there is nothing to say ────
//
// These two are a PAIR and neither proves anything alone. The present-case test
// alone stays green if the lines are printed unconditionally; the absent-case
// test alone stays green if they are never printed at all. Together they pin the
// only rendering that is honest in both.
//
// 🔴 The absent case is the one that shipped. Measured on ~/.officraft/warden/ocagent
// — the binary the warden hands every agent — the three vcs lines all read
// "unknown" under one real build.sha, so what the fleet saw was four lines of
// which one was true. `unknown` and an empty field are both placeholders that
// look like answers; the connection line already settled this argument for
// [station …] / [agent …] by printing nothing (listen_run.go), and this block
// follows it.

func TestPrintVersion_ReportsTheVCSStampWhenTheBuildCarriesOne(t *testing.T) {
	var out bytes.Buffer
	printVersion(&out, func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			{Key: "vcs.time", Value: "2026-08-19T18:31:01Z"},
			{Key: "vcs.modified", Value: "false"},
		}}, true
	},
		func() (string, error) { return "", errors.New("no exe") },
		func(string) ([]byte, error) { return nil, errors.New("no read") })

	for _, want := range []string{
		"  vcs.revision: 0123456789abcdef0123456789abcdef01234567",
		"  vcs.time:     2026-08-19T18:31:01Z",
		"  vcs.modified: false",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("a stamped build must report %q verbatim; got:\n%s", want, out.String())
		}
	}
}

func TestPrintVersion_SaysNothingAboutVCSWhenTheBuildCarriesNoStamp(t *testing.T) {
	// The three shapes of "no stamp" Go actually produces: no BuildInfo at all,
	// BuildInfo with no vcs keys (a worktree build), and a key present but blank.
	for _, tc := range []struct {
		name string
		bi   func() (*debug.BuildInfo, bool)
	}{
		{"no build info", func() (*debug.BuildInfo, bool) { return nil, false }},
		{"no vcs settings", func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "-compiler", Value: "gc"}}}, true
		}},
		{"blank vcs values", func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "  \t "},
				{Key: "vcs.time", Value: ""},
				{Key: "vcs.modified", Value: " "},
			}}, true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			printVersion(&out, tc.bi,
				func() (string, error) { return "", errors.New("no exe") },
				func(string) ([]byte, error) { return nil, errors.New("no read") })
			got := out.String()

			for _, forbidden := range []string{"vcs.revision", "vcs.time", "vcs.modified"} {
				if strings.Contains(got, forbidden) {
					t.Errorf("an unstamped build printed a %s line. Absent must be said by "+
						"SILENCE — not by \"unknown\", not by an empty field; both look like "+
						"answers and that is what the shipped binary showed the fleet. Got:\n%s",
						forbidden, got)
				}
			}
			if strings.Contains(got, "unknown") {
				t.Errorf("the word \"unknown\" is back in the version block:\n%s", got)
			}
			// The block must not collapse to nothing: the two lines that are always
			// knowable have to survive, or this test would also pass on a version
			// command that printed no facts at all.
			if !strings.Contains(got, "build.sha:") || !strings.Contains(got, "self-hash:") {
				t.Errorf("the always-available lines went missing with the vcs block:\n%s", got)
			}
		})
	}
}

// TestVCSLines_AreDecidedPerKeyNotAllOrNothing pins the sentence vcsLines' own
// doc comment makes and nothing tested: "Per-key rather than all-or-nothing […]
// so a partially stamped build shows exactly the keys it really has."
//
// 🔴 THIS CONTRACT HAD ZERO COVERAGE. The pair above only ever supplies all three
// keys or none of them, so rewriting the loop to bail out unless every key is
// present — return nothing whenever any one of them is missing — left the whole
// package green. A partially stamped build would then have gone silent about the
// revision it really does carry, which is the same "absent said by a placeholder"
// failure this block was rewritten to end, just wearing the other mask: the fix
// for printing facts that are not true must not turn into hiding facts that are.
//
// Each case compares the WHOLE returned slice, so it also pins that the output is
// ordered by the label table (revision, time, modified) and not by whatever order
// the build happened to record its settings in.
func TestVCSLines_AreDecidedPerKeyNotAllOrNothing(t *testing.T) {
	bi := func(kv ...[2]string) func() (*debug.BuildInfo, bool) {
		var settings []debug.BuildSetting
		for _, p := range kv {
			settings = append(settings, debug.BuildSetting{Key: p[0], Value: p[1]})
		}
		return func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Settings: settings}, true }
	}

	for _, tc := range []struct {
		name string
		bi   func() (*debug.BuildInfo, bool)
		want []string
	}{
		{
			// The case the mutant kills: one real key, the other two never recorded.
			name: "only vcs.revision was recorded",
			bi:   bi([2]string{"-compiler", "gc"}, [2]string{"vcs.revision", "0123456789abcdef"}),
			want: []string{"  vcs.revision: 0123456789abcdef"},
		},
		{
			name: "only vcs.modified was recorded",
			bi:   bi([2]string{"vcs.modified", "true"}),
			want: []string{"  vcs.modified: true"},
		},
		{
			// Recorded out of order, and the absent key is the MIDDLE one — the
			// output must still be label-ordered and must simply skip the gap.
			name: "revision and modified, no time, recorded out of order",
			bi:   bi([2]string{"vcs.modified", "false"}, [2]string{"vcs.revision", "cafebabe"}),
			want: []string{"  vcs.revision: cafebabe", "  vcs.modified: false"},
		},
		{
			// A key that IS present but blank is not a fact, and must drop out on its
			// own without taking its neighbours with it.
			name: "blank time between two real keys",
			bi: bi([2]string{"vcs.revision", "cafebabe"}, [2]string{"vcs.time", "  \t "},
				[2]string{"vcs.modified", "false"}),
			want: []string{"  vcs.revision: cafebabe", "  vcs.modified: false"},
		},
		{
			name: "all three recorded",
			bi: bi([2]string{"vcs.revision", "cafebabe"}, [2]string{"vcs.time", "2026-08-19T18:31:01Z"},
				[2]string{"vcs.modified", "false"}),
			want: []string{
				"  vcs.revision: cafebabe",
				"  vcs.time:     2026-08-19T18:31:01Z",
				"  vcs.modified: false",
			},
		},
		{
			name: "nothing recorded at all",
			bi:   bi([2]string{"-compiler", "gc"}),
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := vcsLines(tc.bi)
			if !slices.Equal(got, tc.want) {
				t.Errorf("vcsLines rendered the wrong set of keys.\n got: %q\nwant: %q\n"+
					"Each key stands or falls on its own: a build that recorded one real "+
					"key must show that key, and a missing or blank neighbour must not "+
					"silence it.", got, tc.want)
			}
		})
	}
}
