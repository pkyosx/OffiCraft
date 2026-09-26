package main

// Spawn-time "is claude logged in?" gate: a logged-out claude still launches a TUI,
// so tmux spawn and start() succeed while the member sits in
// waking→timeout→backoff forever — the likeliest state of a brand-new install.
//
// 🔴 SECURITY CONTRACT: this file may only produce EXISTENCE conclusions — never
// read, hold, log or return a credential value, token fragment, file body, or
// even a credential PATH:
//   - the credentials file is os.Stat'ed, NEVER opened;
//   - `security find-generic-password` runs WITHOUT -w, so the payload is never
//     returned (and no keychain ACL prompt is tripped);
//   - env credentials are compared, never captured into the summary.
// Any change that makes a value reachable is a security bug.

import "strings"

// The CLAUDE_CODE_USE_* flags select Bedrock/Vertex, where no local claude login
// exists; counting them keeps this gate from false-refusing such a host.
var claudeCredEnvKeys = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	// Listing it here also lets it through the spawn line's CLAUDE_* purge,
	// because claudeEnvAllowedNames derives from this list.
	"CLAUDE_CODE_OAUTH_TOKEN",
}

type claudeCredStatus struct {
	Present bool
	Summary string
}

func probeClaudeCreds(
	env func(string) string,
	exists func(path string) bool,
	runner CmdRunner,
	goos string,
) claudeCredStatus {
	parts := make([]string, 0, len(claudeCredEnvKeys)+2)
	present := false
	mark := func(name string, ok bool) {
		if ok {
			present = true
			parts = append(parts, name+"=SET")
			return
		}
		parts = append(parts, name+"=unset")
	}

	if home := env("HOME"); home != "" && exists != nil {
		mark("cred_file", exists(strings.TrimRight(home, "/")+claudeCredFileRel))
	}
	if strings.HasPrefix(goos, "darwin") && runner != nil {
		_, err := runner.Run("security", "find-generic-password", "-s", claudeKeychainService)
		mark("keychain", err == nil)
	}
	for _, k := range claudeCredEnvKeys {
		mark(k, strings.TrimSpace(env(k)) != "")
	}
	return claudeCredStatus{Present: present, Summary: strings.Join(parts, " ")}
}
