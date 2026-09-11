package main

import (
	"errors"
	"strings"
	"testing"
)

// The fixtures keep HOME and the cwd DIFFERENT on purpose. A relative
// OC_CLAUDE_JSON resolves against the cwd and a `~` one against HOME, and a
// fixture that spelled the two the same could not tell those apart — which is
// exactly how the previous shape's `~` defect stayed green.
const (
	fixtureHome = "/Users/wardenowner"
	fixtureCwd  = "/srv/warden-cwd"
)

func fixtureEnv(home, claudeJSON string) func(string) string {
	return func(k string) string {
		switch k {
		case "HOME":
			return home
		case "OC_CLAUDE_JSON":
			return claudeJSON
		}
		return ""
	}
}

func fixtureGetwd() (string, error) { return fixtureCwd, nil }

func TestResolveClaudeHome(t *testing.T) {
	t.Run("no override states the default layout", func(t *testing.T) {
		got, err := resolveClaudeHome(fixtureEnv(fixtureHome, ""), fixtureGetwd)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got.Home != fixtureHome || got.ConfigDir != "" {
			t.Errorf("got %+v, want HOME stated and CLAUDE_CONFIG_DIR unset", got)
		}
		if want := fixtureHome + "/.claude.json"; got.ClaudeJSONPath() != want {
			t.Errorf("ClaudeJSONPath = %q, want %q", got.ClaudeJSONPath(), want)
		}
	})

	t.Run("an empty or relative HOME cannot be stated and is refused", func(t *testing.T) {
		if _, err := resolveClaudeHome(fixtureEnv("", ""), fixtureGetwd); err == nil {
			t.Error("an empty HOME must be refused — the launch line would state nothing")
		}
		if _, err := resolveClaudeHome(fixtureEnv("relative/home", ""), fixtureGetwd); err == nil {
			t.Error("a relative HOME must be refused")
		}
	})

	t.Run("an override naming the default file leaves the layout alone", func(t *testing.T) {
		// Exporting CLAUDE_CONFIG_DIR=$HOME here would move projects/, sessions/
		// AND .credentials.json out of $HOME/.claude — a logged-out child.
		for _, spelling := range []string{fixtureHome + "/.claude.json", "~/.claude.json", fixtureHome + "/x/../.claude.json"} {
			got, err := resolveClaudeHome(fixtureEnv(fixtureHome, spelling), fixtureGetwd)
			if err != nil {
				t.Fatalf("%s: err = %v, want nil", spelling, err)
			}
			if got.ConfigDir != "" {
				t.Errorf("%s: ConfigDir = %q, want empty so the default layout is untouched", spelling, got.ConfigDir)
			}
			if want := fixtureHome + "/.claude.json"; got.ClaudeJSONPath() != want {
				t.Errorf("%s: ClaudeJSONPath = %q, want %q", spelling, got.ClaudeJSONPath(), want)
			}
		}
	})

	t.Run("a tilde override is expanded against HOME, not left literal", func(t *testing.T) {
		got, err := resolveClaudeHome(fixtureEnv(fixtureHome, "~/box/.claude.json"), fixtureGetwd)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if want := fixtureHome + "/box"; got.ConfigDir != want {
			t.Errorf("ConfigDir = %q, want %q", got.ConfigDir, want)
		}
		if strings.Contains(got.ClaudeJSONPath(), "~") {
			t.Errorf("ClaudeJSONPath = %q still carries a ~ — os.ReadFile does not expand it, so this is a write into a literal ~ directory", got.ClaudeJSONPath())
		}
	})

	t.Run("a relative override resolves against the cwd, not HOME", func(t *testing.T) {
		got, err := resolveClaudeHome(fixtureEnv(fixtureHome, "box/.claude.json"), fixtureGetwd)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if want := fixtureCwd + "/box"; got.ConfigDir != want {
			t.Errorf("ConfigDir = %q, want %q — the write end resolves a relative path against the cwd, so this end must too", got.ConfigDir, want)
		}
	})

	t.Run("an unreadable cwd refuses a relative override", func(t *testing.T) {
		_, err := resolveClaudeHome(fixtureEnv(fixtureHome, "box/.claude.json"),
			func() (string, error) { return "", errors.New("no cwd") })
		if err == nil {
			t.Fatal("a relative override with no cwd must be refused, not guessed")
		}
	})

	t.Run("an absolute override becomes the stated config dir", func(t *testing.T) {
		got, err := resolveClaudeHome(fixtureEnv(fixtureHome, "/tmp/box/x/../.claude.json"), fixtureGetwd)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got.ConfigDir != "/tmp/box" || got.ClaudeJSONPath() != "/tmp/box/.claude.json" {
			t.Errorf("got %+v (json %q), want the cleaned /tmp/box", got, got.ClaudeJSONPath())
		}
		if got.Home != fixtureHome {
			t.Errorf("Home = %q, want the run's own HOME even under a redirect", got.Home)
		}
	})

	t.Run("an override under another filename can never be read and is refused", func(t *testing.T) {
		_, err := resolveClaudeHome(fixtureEnv(fixtureHome, "/tmp/throwaway.json"), fixtureGetwd)
		if err == nil {
			t.Fatal("pre-trust writes <config home>/.claude.json, so a valve pointing at any other name must be refused")
		}
		for _, want := range []string{"/tmp/throwaway.json", ".claude.json"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, want it to name %q", err, want)
			}
		}
	})
}

func TestClaudeHomeEntryGate(t *testing.T) {
	t.Run("no override lets the run through even with no HOME", func(t *testing.T) {
		// A warden that spawns nothing still posts telemetry; the spawn itself
		// refuses an unstatable config home (start → claude_home_unresolved).
		if err := claudeHomeEntryGate(fixtureEnv("", ""), fixtureGetwd); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("an override that can be stated lets the run through", func(t *testing.T) {
		if err := claudeHomeEntryGate(fixtureEnv(fixtureHome, "/tmp/box/.claude.json"), fixtureGetwd); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("an override that cannot be honoured is refused", func(t *testing.T) {
		if err := claudeHomeEntryGate(fixtureEnv(fixtureHome, "/tmp/throwaway.json"), fixtureGetwd); err == nil {
			t.Error("a valve the launch line cannot point the child at must be refused at entry")
		}
		if err := claudeHomeEntryGate(fixtureEnv("", "/tmp/box/.claude.json"), fixtureGetwd); err == nil {
			t.Error("an override with no HOME to resolve against must be refused at entry")
		}
	})
}

func TestResolvedClaudeHome(t *testing.T) {
	var logs []string
	logf := func(format string, a ...any) { logs = append(logs, format) }

	if got := resolvedClaudeHome(fixtureEnv("", ""), logf); got.Home != "" {
		t.Errorf("got %+v, want the zero value a spawn refuses on", got)
	}
	if len(logs) != 1 {
		t.Errorf("logs = %v, want one line saying claude spawns will be refused", logs)
	}
	if got := resolvedClaudeHome(fixtureEnv(fixtureHome, ""), logf); got.Home != fixtureHome {
		t.Errorf("got %+v, want the resolved home", got)
	}
}

func TestClaudeEnvAllowedNames(t *testing.T) {
	t.Run("the whitelist is derived from the credential list, not typed out", func(t *testing.T) {
		// Mu-C, from the fourth review: replacing the derivation with the same two
		// literal names passed every test there was. What the derivation buys is
		// that ADDING a credential source cannot leave the launch line stripping
		// it back out — so the assertion has to be about a name that is not in
		// the list today.
		restore := claudeCredEnvKeys
		t.Cleanup(func() { claudeCredEnvKeys = restore })
		claudeCredEnvKeys = append(append([]string{}, restore...), "CLAUDE_CODE_USE_SOMETHING_NEW", "ANTHROPIC_SOMETHING_NEW")

		got := claudeEnvAllowedNames()
		var sawNew, sawAnthropic bool
		for _, k := range got {
			switch k {
			case "CLAUDE_CODE_USE_SOMETHING_NEW":
				sawNew = true
			case "ANTHROPIC_SOMETHING_NEW":
				sawAnthropic = true
			}
		}
		if !sawNew {
			t.Errorf("allowed = %v: a newly declared CLAUDE_* credential must survive the purge without anyone editing this list", got)
		}
		if sawAnthropic {
			t.Errorf("allowed = %v: the purge only deletes CLAUDE_*, so letting an ANTHROPIC_* name through is meaningless noise", got)
		}
		if !strings.Contains(claudeEnvPurgeFragment(), "CLAUDE_CODE_USE_SOMETHING_NEW=*") {
			t.Errorf("the rendered fragment does not carry the derived name:\n%s", claudeEnvPurgeFragment())
		}
	})

	t.Run("an empty credential list renders a fragment that still purges", func(t *testing.T) {
		restore := claudeCredEnvKeys
		t.Cleanup(func() { claudeCredEnvKeys = restore })
		claudeCredEnvKeys = nil
		if got := claudeEnvAllowedNames(); len(got) != 0 {
			t.Errorf("allowed = %v, want none", got)
		}
		if frag := claudeEnvPurgeFragment(); !strings.Contains(frag, "CLAUDE_*=*") || strings.Contains(frag, "=*) continue") {
			t.Errorf("with nothing whitelisted the fragment must still delete the family and skip the empty allow-case:\n%s", frag)
		}
	})
}
