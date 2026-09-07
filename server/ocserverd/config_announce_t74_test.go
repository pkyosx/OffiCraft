package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// announceEnv builds the env lookup these tests hand the commands: nothing set
// unless the test says so, which is exactly the shape of the machine the owner
// complained about (a normal install has neither variable and no config file).
func announceEnv(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

// TestAnnouncementNamesTheDatabaseAndWhereItCameFrom is the acceptance the
// owner actually asked for, in his words: the complaint was that it acts on the
// real database "silently". So the assertion is not "it printed something" — it
// is that BOTH halves are on screen before anything happens: which config file
// (or that there is none, and where it looked) and which database, each with
// the source of that answer.
func TestAnnouncementNamesTheDatabaseAndWhereItCameFrom(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "explicit.db")

	// A real config file carrying [storage].dsn — the arm the first version of
	// this table did not have, and the one a seeded mutant walked straight
	// through: swapping dsnSource's first two branches announced
	// "[storage].dsn in the config file" for a DSN that came from the
	// environment, and every test here stayed green.
	cfgPath := filepath.Join(dir, "oc.toml")
	if err := os.WriteFile(cfgPath, []byte("[server]\nnamespace = \"probe\"\n\n[storage]\ndsn = \"sqlite:////tmp/from-config.db\"\n"), 0o600); err != nil {
		t.Fatalf("write probe config: %v", err)
	}

	cases := []struct {
		name       string
		env        map[string]string
		wantConfig string
		wantSource string
		wantFile   string
		wantHowTo  bool
	}{
		{
			// The dangerous one. No config file, no override: the resolution is
			// entirely implicit and that is precisely when it must be loudest.
			name:       "nothing set at all",
			env:        map[string]string{},
			wantConfig: "config file = none",
			wantSource: "the built-in default",
			wantHowTo:  true,
		},
		{
			name:       "database overridden by env",
			env:        map[string]string{envDatabaseURL: "sqlite:///" + db},
			wantConfig: "config file = none",
			wantSource: "$" + envDatabaseURL,
			wantHowTo:  true,
		},
		{
			// PRECEDENCE. Both are set and they disagree; the announcement must
			// name the one that actually won.
			name:       "env wins over the config file, and the line says so",
			env:        map[string]string{envConfigPath: cfgPath, envDatabaseURL: "sqlite:////tmp/from-env.db"},
			wantConfig: "config file = " + cfgPath,
			wantSource: "$" + envDatabaseURL,
			wantFile:   "/tmp/from-env.db",
		},
		{
			// Same config file, no env: now the OTHER branch must be named.
			name:       "config file supplies the dsn",
			env:        map[string]string{envConfigPath: cfgPath},
			wantConfig: "config file = " + cfgPath,
			wantSource: "[storage].dsn in the config file",
			wantFile:   "/tmp/from-config.db",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			_, dsn, rc := announceResolution("migrate", announceEnv(tc.env), &out)
			if rc != 0 {
				t.Fatalf("rc=%d, want 0 — a missing config file is not an error (owner ruling rc-d961ee5e790c [0]: do not block, say it out loud). out=%s", rc, out.String())
			}
			got := out.String()
			if !strings.Contains(got, tc.wantConfig) {
				t.Errorf("no config-file line matching %q in:\n%s", tc.wantConfig, got)
			}
			if !strings.Contains(got, "(DSN "+dsn+")") {
				t.Errorf("the database line does not carry the DSN that was actually returned (%q) in:\n%s", dsn, got)
			}
			// THE FILE, not just the DSN. sqlite:///x is relative and
			// sqlite:////x is absolute — one character apart — so a line that
			// only echoes the DSN cannot tell two different databases apart.
			if wantPath, ok := sqliteFilePath(dsn); ok {
				abs, err := filepath.Abs(wantPath)
				if err != nil {
					t.Fatalf("abs %s: %v", wantPath, err)
				}
				if !strings.Contains(got, "database    = "+abs) {
					t.Errorf("the database line does not name the ABSOLUTE file that will be opened (%q) in:\n%s", abs, got)
				}
			}
			if tc.wantFile != "" && !strings.Contains(got, tc.wantFile) {
				t.Errorf("expected the announced file to be %q in:\n%s", tc.wantFile, got)
			}
			// The instruction half. "config file = none" is a fact people
			// have read past; the run that has no config file must also say
			// what to type to give it one — and the run that HAS one must not,
			// or "always printed" and "printed when it is missing" are the same
			// output.
			howTo := "set " + envConfigPath + "=/path/to/oc.toml"
			if tc.wantHowTo && !strings.Contains(got, howTo) {
				t.Errorf("no config file was found, but nothing told the reader how to specify one (looked for %q) in:\n%s", howTo, got)
			}
			if !tc.wantHowTo && strings.Contains(got, howTo) {
				t.Errorf("a config file WAS found at %s, yet the how-to-specify-one line still printed in:\n%s — printing it unconditionally makes it noise and makes the no-config case indistinguishable", tc.wantConfig, got)
			}
			if !strings.Contains(got, tc.wantSource) {
				t.Errorf("no source attribution %q in:\n%s — a path with no source reads as \"that is what I asked for\", which is the misreading this whole change exists to stop", tc.wantSource, got)
			}
		})
	}
}

// howToLine returns the single instruction line out of an announcement, so a
// test can assert about THAT sentence rather than about the whole buffer —
// "the built-in default" is a legitimate phrase on the database line and a lie
// on this one, and a whole-buffer Contains cannot tell those apart.
func howToLine(t *testing.T, announcement string) string {
	t.Helper()
	const marker = "to point this run at a config file"
	for _, line := range strings.Split(announcement, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	t.Fatalf("no line containing %q in:\n%s", marker, announcement)
	return ""
}

// TestHowToLineDoesNotClaimEverythingIsDefaultWhenEnvSuppliesTheDSN pins the
// one half of the instruction line that had NO guard, and it had none in the
// worst possible way: an independent reviewer replaced the whole sentence with
// "without one, the server will refuse to start and your database will be
// deleted." and every test in this package stayed green. The half that was
// unguarded was the half that was false.
//
// The false claim was "without one, every config value is the built-in
// default", and the run that disproves it is the run this announcement exists
// for: no config file, someone else's DSN in the environment. resolveDSN's
// FIRST branch is $OC_DATABASE_URL, so the database line printed immediately
// below says "from $OC_DATABASE_URL" — the announcement contradicted itself
// one line later, and told the person in the wrong directory that nothing in
// his environment mattered.
func TestHowToLineDoesNotClaimEverythingIsDefaultWhenEnvSuppliesTheDSN(t *testing.T) {
	db := filepath.Join(t.TempDir(), "env.db")
	var out bytes.Buffer
	_, _, rc := announceResolution("migrate", announceEnv(map[string]string{envDatabaseURL: "sqlite:///" + db}), &out)
	if rc != 0 {
		t.Fatalf("rc=%d, want 0. out=%s", rc, out.String())
	}
	got := out.String()
	// Positive control: this run really is the contradicting one. If the
	// database stopped coming from the environment, the assertion below would
	// be about nothing.
	if !strings.Contains(got, "from $"+envDatabaseURL) {
		t.Fatalf("positive control failed: %s was set, yet the database line does not attribute the DSN to it, so there is no contradiction left to detect:\n%s", envDatabaseURL, got)
	}
	line := howToLine(t, got)
	if !strings.Contains(line, "$"+envDatabaseURL) {
		t.Errorf("the instruction line does not name %s:\n\t%s\n\nyet the line directly under it says the database came from exactly that variable. Any sentence here that describes what happens \"without a config file\" without naming the variable that outranks the config file is asserting something this very run disproves — the original wording (\"without one, every config value is the built-in default\") is the case in point, and it was addressed at the one reader who most needed it to be true.", envDatabaseURL, line)
	}
	if strings.Contains(line, "every config value is the built-in default") {
		t.Errorf("the instruction line still claims every config value is the built-in default:\n\t%s\n\n$%s is set on this run and resolveDSN reads it FIRST.", line, envDatabaseURL)
	}
}

// TestHowToLineDoesNotSaySwitchDirectoriesWhenOCConfigIsSet pins the other
// half. configPath returns $OC_CONFIG unconditionally and never falls back to
// ./oc.toml, so "or run from a directory containing oc.toml" is an instruction
// that does nothing for the reader whose $OC_CONFIG names a file that is not
// there — measured: standing IN a directory that has an oc.toml, with
// $OC_CONFIG pointing at an absent file, the announcement said "config file =
// none" and then told him to go and stand where he was already standing.
func TestHowToLineDoesNotSaySwitchDirectoriesWhenOCConfigIsSet(t *testing.T) {
	const cwdAdvice = "run from a directory containing oc.toml"
	absent := filepath.Join(t.TempDir(), "absent", "oc.toml")

	var set bytes.Buffer
	_, _, rc := announceResolution("migrate", announceEnv(map[string]string{envConfigPath: absent}), &set)
	if rc != 0 {
		t.Fatalf("rc=%d, want 0 — a missing config file is not an error. out=%s", rc, set.String())
	}
	if !strings.Contains(set.String(), "config file = none") {
		t.Fatalf("positive control failed: %s pointed at %s, which does not exist, yet the announcement did not report a missing config file:\n%s", envConfigPath, absent, set.String())
	}
	if line := howToLine(t, set.String()); strings.Contains(line, cwdAdvice) {
		t.Errorf("$%s is set (to %s, which is absent) and the instruction line still tells the reader to change directory:\n\t%s\n\nconfigPath returns $%s unconditionally and never falls back to ./oc.toml, so the reader can be standing in a directory that HAS an oc.toml and this advice will still do nothing for him.", envConfigPath, absent, line, envConfigPath)
	}

	// The negative arm, without which "never say it" and "say it only when
	// $OC_CONFIG is unset" are the same output: with the variable unset, the
	// CWD route is real and must still be offered.
	var unset bytes.Buffer
	if _, _, rc := announceResolution("migrate", announceEnv(map[string]string{}), &unset); rc != 0 {
		t.Fatalf("rc=%d, want 0. out=%s", rc, unset.String())
	}
	if line := howToLine(t, unset.String()); !strings.Contains(line, cwdAdvice) {
		t.Errorf("$%s is unset, so ./oc.toml IS consulted, yet the instruction line no longer offers that route:\n\t%s", envConfigPath, line)
	}
}

// TestAnnouncementIsNotAnError pins the half of the owner's ruling that is easy
// to lose in a later "tighten this up" pass: the FIRST plan was to refuse, and
// it was withdrawn because a normal install has no config file by design and
// two documented rescue commands would have been blocked. If someone turns this
// back into a refusal, this goes red and names the ruling.
func TestAnnouncementIsNotAnError(t *testing.T) {
	var out bytes.Buffer
	_, _, rc := announceResolution("mfa-disable", announceEnv(map[string]string{}), &out)
	if rc != 0 {
		t.Fatalf("announceResolution refused with rc=%d when no config file is present. That is the plan the owner REJECTED on rc-d961ee5e790c: install.sh:1174 names the no-config outcome CFG_SRC=\"none\", and docs/guide/mobile.md tells the user to run `ocserverd mfa-disable` on the machine when their authenticator is gone — refusing here breaks the rescue path. out=%s", rc, out.String())
	}
	got := out.String()
	if strings.Contains(got, "FATAL") {
		t.Errorf("the announcement printed FATAL for the ordinary no-config case:\n%s", got)
	}
}

// TestEveryDSNResolutionIsAnnounced is the call-site guard, and it is the one
// that keeps this change true a year from now. Bundling loadConfig + resolveDSN
// + the announcement into one helper only helps if nobody resolves a DSN any
// other way — a test that only exercises the helper would stay green while a
// new subcommand quietly opened the real database without a word.
//
// So: outside this file's own helper (and outside tests), no non-test file in
// this package may call resolveDSN. AST-walked rather than grepped, so that a
// mention inside a comment or a string does not count and a real call cannot
// hide behind formatting.
func TestEveryDSNResolutionIsAnnounced(t *testing.T) {
	const home = "config_announce_t74.go"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	var offenders, muted []string
	scanned, controlHits, writerHits := 0, 0, 0
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, n, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		scanned++
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			// SECOND RULE, and it is the one that keeps the announcement
			// AUDIBLE rather than merely present. Seeded and measured: changing
			// cmdServe's writer to io.Discard silences serve — the command the
			// owner named — with one word, and every test in this package stays
			// green, because nothing asserts that a real command prints. The
			// only defence was a comment saying it is a no-op by construction,
			// and a comment is a report, not a guard.
			if id.Name == "announceResolution" {
				if len(call.Args) == 3 {
					if w, ok := call.Args[2].(*ast.Ident); !ok || w.Name != "out" {
						muted = append(muted, n+":"+fset.Position(call.Pos()).String())
					} else {
						writerHits++
					}
				} else {
					muted = append(muted, n+":"+fset.Position(call.Pos()).String()+" (unexpected arity)")
				}
				return true
			}
			if id.Name != "resolveDSN" {
				return true
			}
			if n == home {
				controlHits++
				return true
			}
			offenders = append(offenders, n+":"+fset.Position(call.Pos()).String())
			return true
		})
	}
	// Anti-vacuity, both directions: an empty file list, or a walk that cannot
	// see the one call we KNOW is there, would make this trivially green.
	if scanned < 40 {
		t.Fatalf("only %d non-test .go files scanned — too few to be this package, so a clean result would mean nothing", scanned)
	}
	if controlHits == 0 {
		t.Fatalf("positive control failed: the AST walk found ZERO resolveDSN calls in %s, where announceResolution definitely calls it. The walk is broken, not the package.", home)
	}
	if writerHits < 4 {
		t.Fatalf("only %d call(s) to announceResolution passed the command's own writer — there are four command seats (backup, migrate, openAuthDAL, serve), so anything less means the walk missed some or a seat stopped announcing", writerHits)
	}
	if len(muted) > 0 {
		t.Errorf("announceResolution is called with a writer that is not the command's own `out`: %v\n\nThat silences the announcement while every other test stays green. If a caller genuinely has no writer, it has no business resolving a database either.", muted)
	}
	if len(offenders) > 0 {
		t.Errorf("resolveDSN is called outside %s: %v\n\nEvery command must learn its database THROUGH announceResolution, because that is the only thing that also says so out loud. If you need the DSN somewhere new, call announceResolution — do not resolve it quietly.", home, offenders)
	}
}
