package main

// claudehome.go — WHICH claude.json the spawned child reads, decided by the
// warden instead of predicted from the environment (T-180).
//
// The pre-trust write (spawn.go pretrustWorkdir) and the child's read have to
// land on the same file, or the trust dialog fires, eats the boot nudge, and the
// member is dead within a second while every receipt says the spawn succeeded.
//
// HOW MANY INPUTS DECIDE THE CHILD'S ANSWER: more than anyone here can list.
// An earlier draft of this comment said "two environment variables decide it"
// and named them; that sentence was pinned to one afternoon's measurement and
// was already false by the next review. HOME and CLAUDE_CONFIG_DIR place the
// config home, CLAUDE_CODE_CUSTOM_OAUTH_URL swaps the FILENAME read inside it,
// and `strings` on claude 2.1.268 lists 841 distinct CLAUDE_*/ANTHROPIC_* names
// whose membership changes every release. The layout is not symmetric between
// the two placing variables either (measured 2026-09-11, claude 2.1.268):
//
//	CLAUDE_CONFIG_DIR unset → $HOME/.claude.json, everything else $HOME/.claude/
//	CLAUDE_CONFIG_DIR=D     → D/.claude.json, and D holds projects/ sessions/
//	                          backups/ .credentials.json too
//
// So the guarantee cannot be "we know every input", and — measured 2026-09-11 —
// it cannot be about inputs at all: `<config dir>/.config.json`, when that file
// merely EXISTS, is read INSTEAD of .claude.json, and no environment is involved
// in that at all. Two things therefore carry the load, and neither is a
// prediction:
//
//	INPUT SIDE   the child's environment holds no CLAUDE_* we did not put there
//	             — the family purge at the bottom of this file.
//	RESULT SIDE  the file the spawned claude REALLY reads carries the flag —
//	             established per spawn by asking the claude binary itself, and a
//	             spawn whose answer is anything other than yes is refused by
//	             name. See claudetrust.go.
//
// The earlier shape of this fix PREDICTED what the child's environment would
// become (HOME read back, "last export wins" modelled in Go) and refused a run
// whose prediction did not match the write target. Two independent reviews each
// walked through it, the second one with CLAUDE_CONFIG_DIR: it moves the whole
// config home while HOME does not move at all, so every comparison stayed green
// and the original defect came back whole. A prediction cannot be made complete
// by adding the variable that was just found — the next variable has the same
// shape.
//
// So the launch line STATES both instead: it exports HOME and either exports
// CLAUDE_CONFIG_DIR or unsets it, AFTER it has sourced the agent env render, and
// the file pre-trust writes is derived from those same two values by
// ClaudeJSONPath. Whatever the owner's interactive shell, ~/.zshrc or agent env
// file carried is then irrelevant — not because we checked, because we overwrote
// it. That is causal rather than predicted FOR THE ENVIRONMENT, which is all it
// was ever able to be: overwriting a variable does nothing about a file whose
// existence alone moves the read, which is why the result-side check exists.
//
// ⚠️ CLAUDE_CONFIG_DIR is exported ONLY for an explicit OC_CLAUDE_JSON redirect,
// and moving it MOVES THE CREDENTIALS FILE with it (.credentials.json lives in
// the config home) — a child pointed at a fresh directory is a LOGGED-OUT child.
// The default path therefore unsets the variable rather than setting it to
// anything, so the child's layout is byte-for-byte the default one.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// claudeJSONName is the file claude reads its trust list from, inside whichever
// directory is the config home.
const claudeJSONName = ".claude.json"

// claudeHome is the child's config location as the LAUNCH LINE WILL STATE IT.
// Home is exported verbatim; an empty ConfigDir means the launch line UNSETS
// CLAUDE_CONFIG_DIR (the default layout), a non-empty one means it exports that
// directory.
type claudeHome struct {
	Home      string
	ConfigDir string
}

// ClaudeJSONPath is the file this end WRITES: pretrustWorkdir's target, derived
// from the two values the launch line exports.
//
// 🔴 IT IS NOT A PREDICTION OF WHAT THE CHILD READS, and the earlier sentence
// here that said no second resolution could disagree with it was measured wrong
// on 2026-09-11: claude reads `<config dir>/.config.json` instead of
// `.claude.json` whenever that file exists, which this function cannot see and
// no environment hygiene can reach. What closes the gap is asking claude, after
// the write, whether it can see the flag — claudetrust.go — not anything
// asserted here.
func (c claudeHome) ClaudeJSONPath() string {
	if c.ConfigDir != "" {
		return filepath.Join(c.ConfigDir, claudeJSONName)
	}
	return filepath.Join(c.Home, claudeJSONName)
}

// resolveClaudeHome decides the child's config location from the warden's own
// environment. getwd is the seam for the one relative-path case below.
//
// OC_CLAUDE_JSON (the PoC safety valve that points a live run at a throwaway
// file) is resolved to an ABSOLUTE path HERE, once. That placement is the fix
// for a real defect in the predicting shape: it expanded `~` against HOME for
// the comparison while the write end passed the raw string to os.ReadFile, which
// expands nothing — so `OC_CLAUDE_JSON=~/.claude.json` compared equal and then
// wrote to a literal `./~/.claude.json`. One resolution, used by both ends,
// cannot drift like that.
func resolveClaudeHome(env func(string) string, getwd func() (string, error)) (claudeHome, error) {
	home := strings.TrimSpace(env("HOME"))
	if home == "" {
		return claudeHome{}, fmt.Errorf("HOME is empty, so the config home the spawned claude reads ($HOME/%s) cannot be stated on the launch line — set HOME in the warden's environment", claudeJSONName)
	}
	if !filepath.IsAbs(home) {
		return claudeHome{}, fmt.Errorf("HOME=%q is not an absolute path, so the launch line cannot state the child's config home — set HOME to an absolute path", home)
	}
	home = filepath.Clean(home)

	raw := strings.TrimSpace(env("OC_CLAUDE_JSON"))
	if raw == "" {
		return claudeHome{Home: home}, nil
	}
	abs, err := absClaudeJSONPath(raw, home, getwd)
	if err != nil {
		return claudeHome{}, err
	}
	if filepath.Base(abs) != claudeJSONName {
		return claudeHome{}, fmt.Errorf("OC_CLAUDE_JSON=%q resolves to %q, whose filename is not %s: pre-trust writes the workdir into <config home>/%s, so a file under another name can never be the trust file the child reads — point OC_CLAUDE_JSON at a directory's %s",
			raw, abs, claudeJSONName, claudeJSONName, claudeJSONName)
	}
	if abs == filepath.Join(home, claudeJSONName) {
		// The override names the default file. Leave CLAUDE_CONFIG_DIR unset:
		// exporting $HOME would move projects/, sessions/ AND the credentials
		// file out of $HOME/.claude, which is a logged-out child.
		return claudeHome{Home: home}, nil
	}
	return claudeHome{Home: home, ConfigDir: filepath.Dir(abs)}, nil
}

// absClaudeJSONPath expands a leading ~ against the run's OWN home (not the
// process's) and resolves a relative path against the cwd, so the single
// resolution above is absolute.
func absClaudeJSONPath(raw, home string, getwd func() (string, error)) (string, error) {
	switch {
	case raw == "~":
		return filepath.Clean(home), nil
	case strings.HasPrefix(raw, "~/"):
		return filepath.Join(home, raw[2:]), nil
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), nil
	}
	if getwd == nil {
		return "", fmt.Errorf("OC_CLAUDE_JSON=%q is a relative path and this process cannot resolve it — give an absolute path", raw)
	}
	wd, err := getwd()
	if err != nil {
		return "", fmt.Errorf("OC_CLAUDE_JSON=%q is a relative path and the warden's cwd cannot be read (%v) — give an absolute path", raw, err)
	}
	return filepath.Join(wd, raw), nil
}

// claudeHomeEntryGate is the process-entry refusal for an OC_CLAUDE_JSON that
// CANNOT be honoured (a name other than .claude.json, an unresolvable cwd, a
// HOME the launch line could not state). It refuses before any transport or
// spawn path exists, so an operator who mistyped the valve is told at start
// rather than per spawn.
//
// With the valve UNSET it returns nil even when HOME is unset: the warden also
// posts telemetry on hosts that spawn nothing, and a missing HOME is refused by
// the spawn itself (start() will not launch a claude whose config home is
// unresolved), not by refusing to run at all.
func claudeHomeEntryGate(env func(string) string, getwd func() (string, error)) error {
	if strings.TrimSpace(env("OC_CLAUDE_JSON")) == "" {
		return nil
	}
	_, err := resolveClaudeHome(env, getwd)
	return err
}

// resolvedClaudeHome is the wiring helper: it resolves once and reports a failure
// on warden stderr instead of returning an error no constructor can act on. The
// zero value it returns on failure is what start() refuses on, so a host that
// cannot state a config home spawns nothing rather than spawning blind.
func resolvedClaudeHome(env func(string) string, logf func(string, ...any)) claudeHome {
	ch, err := resolveClaudeHome(env, os.Getwd)
	if err != nil {
		if logf != nil {
			logf("[ocwarden spawn] claude config home unresolved (%v); every claude spawn will be refused", err)
		}
		return claudeHome{}
	}
	return ch
}

// ---------------------------------------------------------------------------
// The CLAUDE_* family purge — the STRUCTURAL half of the pairing above.
//
// Everything above states HOME (and CLAUDE_CONFIG_DIR) so the child cannot
// INHERIT a config home nobody chose. Three independent reviews then broke that
// shape three times, each in the same way: another environment variable was
// found that moves the file the child reads, and every check we had still
// reported success. CLAUDE_CONFIG_DIR moved the whole config directory;
// CLAUDE_CODE_CUSTOM_OAUTH_URL made the child read
// <config home>/.claude-custom-oauth.json while pre-trust wrote .claude.json.
//
// Naming the newly-found variable is not a fix, it is the next round of the
// same game: `strings` on claude 2.1.268 lists 841 distinct CLAUDE_*/ANTHROPIC_*
// names, and the set changes every release. So the launch line no longer names
// anything. It DELETES THE WHOLE CLAUDE_* FAMILY from the child's environment
// and lets through only the names in claudeEnvAllowedNames — a whitelist, so a
// variable we have never heard of is handled by construction rather than by
// having been predicted.
//
// WHY NOT ANTHROPIC_* TOO — measured, 2026-09-11, claude 2.1.268, not reasoned.
// Running `claude config ls` under a throwaway HOME and watching which directory
// the config layout appears in:
//
//	CLAUDE_CONFIG_DIR=D              → the layout lands in D          (moves it)
//	CLAUDE_CODE_CUSTOM_OAUTH_URL=... → nothing lands in $HOME at all  (moves it)
//	ANTHROPIC_CONFIG_DIR=D           → the layout lands in $HOME      (no effect)
//	CLAUDE_SECURESTORAGE_CONFIG_DIR=D→ the layout lands in $HOME      (no effect)
//
// So ANTHROPIC_* has no measured power over WHICH file is read, while it does
// carry the two direct credentials the spawn gate accepts (claudeCredEnvKeys).
// Purging it would log out every host that authenticates that way, to buy a
// guarantee no measurement supports. It is left alone deliberately.
//
// WHAT THE WHITELIST HOLDS. Only the CLAUDE_* names the warden ITSELF already
// recognises as a credential source — derived from claudeCredEnvKeys rather
// than re-typed, so the two lists cannot drift apart. Those two select a managed
// cloud auth path; dropping them turns a working Bedrock/Vertex host into a
// logged-out child.
// ---------------------------------------------------------------------------

// claudeEnvPurgePrefix is the family the launch line clears. One prefix, not a
// list of names: the whole point is that membership is decided by shape, so a
// name nobody here has ever seen is already covered.
const claudeEnvPurgePrefix = "CLAUDE_"

// claudeEnvAllowedNames are the CLAUDE_* names that survive the purge. Derived
// from claudeCredEnvKeys — the warden's own definition of "an env-carried claude
// credential" — so adding a credential source there cannot silently leave the
// launch line stripping it back out.
func claudeEnvAllowedNames() []string {
	out := make([]string, 0, len(claudeCredEnvKeys))
	for _, k := range claudeCredEnvKeys {
		if strings.HasPrefix(k, claudeEnvPurgePrefix) {
			out = append(out, k)
		}
	}
	return out
}

// claudeEnvPurgeFragment renders the shell fragment that performs the purge. It
// belongs on the launch line AFTER the agent env render is sourced — the render
// is one of the two places an unknown CLAUDE_* arrives from (the other is the
// warden's own environment), so a purge placed above it clears nothing.
//
// SHELL NOTES, each of which is a way this could have shipped broken:
//
//   - /usr/bin/env is ABSOLUTE for the same measured reason /bin/cat is on the
//     OC_TOKEN line: the owner's env file is sourced first and may leave PATH in
//     any state at all, and a bare `env` that fails to resolve makes this
//     fragment a silent no-op.
//   - The loop word-splits `$(/usr/bin/env)`, so a value containing spaces or
//     newlines produces junk words. That is SAFE here rather than merely
//     tolerated: a junk word either fails the CLAUDE_*=* pattern and is skipped,
//     or it looks like a CLAUDE_* assignment and causes one extra unset of a
//     variable this fragment was going to delete anyway.
//   - The name is checked against [!A-Za-z0-9_] before `unset`, so a malformed
//     word can never reach `unset` as a non-identifier and print a shell error
//     onto the agent's first line. KNOWN AND ACCEPTED CONSEQUENCE: a variable
//     whose NAME is not an identifier (`CLAUDE_WITH SPACES`, settable only via
//     execve, never by a shell) survives. POSIX `unset` cannot remove such a
//     name either, and claude looks its settings up by identifier names, so
//     this is a gap with no reachable exploit rather than a silent hole.
//   - Verified to behave identically under /bin/zsh, /bin/bash, /bin/sh and
//     /bin/dash, including the space-carrying-value case — by execution, not by
//     recollection: TestBuildLaunchCommandWithEnv runs the real prologue under
//     every one of those that exists on the host. tmux launches the child under
//     its default-shell (/bin/zsh on these machines), so a check pinned to
//     /bin/sh would be measuring a dialect production never uses.
//
// TWO WAYS THIS GOES SILENTLY DEAD, neither of which any test here can see,
// because both are states of the shell the owner's env file left behind:
//
//   - IFS. The loop depends on `$(/usr/bin/env)` word-splitting on whitespace.
//     An env file (or a .zshrc it sources) that sets IFS to something else makes
//     the whole output one word, which matches no pattern, and the purge unsets
//     NOTHING while still looking exactly like a purge that ran.
//   - `typeset -r` / `readonly`. A CLAUDE_* variable marked read-only survives
//     `unset`, and the shell prints its complaint on the agent's FIRST LINE —
//     so the variable keeps redirecting the child AND the member opens with an
//     error banner. The result-side check in claudetrust.go is what turns this
//     from a silent survival into a named refusal.
func claudeEnvPurgeFragment() string {
	var b strings.Builder
	b.WriteString("for __oc_e in $(/usr/bin/env); do case $__oc_e in ")
	if allowed := claudeEnvAllowedNames(); len(allowed) > 0 {
		for i, k := range allowed {
			if i > 0 {
				b.WriteString("|")
			}
			b.WriteString(k + "=*")
		}
		b.WriteString(") continue;; ")
	}
	b.WriteString(claudeEnvPurgePrefix + "*=*) ;; *) continue;; esac; ")
	b.WriteString("__oc_n=${__oc_e%%=*}; case $__oc_n in *[!A-Za-z0-9_]*) continue;; esac; ")
	b.WriteString(`unset "$__oc_n"; done; unset __oc_e __oc_n; `)
	return b.String()
}
