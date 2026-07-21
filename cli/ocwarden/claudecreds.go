package main

// claudecreds.go — the SPAWN-TIME "is claude actually logged in?" gate (T-ba62).
//
// WHY this file exists: `claude` being INSTALLED and `claude` being USABLE are
// two different facts, and the second one had no gate anywhere in the warden.
// A logged-OUT claude still launches a TUI, so tmux new-session succeeds, the
// boot nudge is delivered into a login prompt that will never accept it, and
// start() returns OK:true. The member then sits in waking→timeout→backoff
// forever while every receipt the owner can see says the spawn succeeded. That
// is the single most likely state of a BRAND NEW install (the user has not run
// `claude` even once yet), so onboarding automation without this gate just
// delivers people into an unexplainable dead end faster.
//
// 🔴 SECURITY CONTRACT — read before touching anything here:
// this file may only ever produce EXISTENCE conclusions. It must NEVER read,
// hold, log, or return a credential value, a token fragment, a file body, or
// even a credential PATH. Every probe is deliberately chosen so the secret is
// never requested in the first place:
//   - the credentials file is os.Stat'ed, NEVER opened (claudeprobe.go reads it
//     for subscriptionType; this gate does not need even that much);
//   - the macOS keychain lookup runs `security find-generic-password` WITHOUT
//     -w, so only item METADATA is queried and the payload is never returned
//     (and no keychain ACL prompt is tripped);
//   - environment credentials are tested with `!= ""` — the value is compared,
//     never captured into the summary.
// The only thing that leaves this file is the literal word SET or unset per
// source name. Any change that makes a value reachable is a security bug.

import "strings"

// claudeCredEnvKeys are the environment-carried claude credentials, in the
// order they appear in the summary. ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN
// are direct credentials; the two CLAUDE_CODE_USE_* flags select a managed
// cloud auth path (Bedrock / Vertex) where the actual credential lives in the
// cloud SDK chain and no local claude login exists at all — treating them as
// "credentialed" is what keeps this gate from false-refusing such a host.
var claudeCredEnvKeys = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
}

// claudeCredStatus is the value-free verdict of the presence probe. Summary is
// a space-joined "<source>=SET|unset" list and is SAFE TO LOG BY CONSTRUCTION —
// it is assembled from constant strings only (see the security contract above).
type claudeCredStatus struct {
	Present bool
	Summary string
}

// probeClaudeCreds answers "does this host hold ANY claude credential?" purely
// by existence. Injectable seams (env / stat / runner / goos) so tests never
// touch a real keychain or a real home directory.
//
// exists must be a STAT-ONLY probe (never a read). runner runs the metadata-only
// keychain lookup; a nil runner or a non-darwin goos simply drops that source
// (an honest absent signal, never a fabricated one).
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
		// STAT ONLY. The path is built here and immediately discarded — it is
		// never formatted into the summary.
		mark("cred_file", exists(strings.TrimRight(home, "/")+claudeCredFileRel))
	}
	if strings.HasPrefix(goos, "darwin") && runner != nil {
		// NO -w: metadata-only, the secret payload is never requested.
		_, err := runner.Run("security", "find-generic-password", "-s", claudeKeychainService)
		mark("keychain", err == nil)
	}
	for _, k := range claudeCredEnvKeys {
		mark(k, strings.TrimSpace(env(k)) != "")
	}
	return claudeCredStatus{Present: present, Summary: strings.Join(parts, " ")}
}
