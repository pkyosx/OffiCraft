package main

// Claude can also select a config through its own settings, so these exports do
// not prove the trust flag was consumed; the member's wake acknowledgement and
// the server's timeout/retry path decide startup health.
//
// CLAUDE_CONFIG_DIR is exported only for an explicit redirect: changing it also
// moves Claude's credentials file, so a fresh config directory can be logged out.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const claudeJSONName = ".claude.json"

type claudeHome struct {
	Home      string
	ConfigDir string
}

func (c claudeHome) ClaudeJSONPath() string {
	if c.ConfigDir != "" {
		return filepath.Join(c.ConfigDir, claudeJSONName)
	}
	return filepath.Join(c.Home, claudeJSONName)
}

// OC_CLAUDE_JSON (the PoC valve pointing a live run at a throwaway file) is
// resolved to an ABSOLUTE path HERE, once, for both the trust write and the
// launch: os.ReadFile expands nothing, so a raw `~/.claude.json` once compared
// equal and then wrote to a literal `./~/.claude.json`.
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
		// Leave CLAUDE_CONFIG_DIR unset: exporting $HOME would move projects/,
		// sessions/ AND the credentials file out of $HOME/.claude — a logged-out child.
		return claudeHome{Home: home}, nil
	}
	return claudeHome{Home: home, ConfigDir: filepath.Dir(abs)}, nil
}

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

// With OC_CLAUDE_JSON unset this passes even when HOME is unset: the warden also
// posts telemetry on hosts that spawn nothing, and a missing HOME is refused by
// the spawn itself.
func claudeHomeEntryGate(env func(string) string, getwd func() (string, error)) error {
	if strings.TrimSpace(env("OC_CLAUDE_JSON")) == "" {
		return nil
	}
	_, err := resolveClaudeHome(env, getwd)
	return err
}

// The zero value returned on failure is what start() refuses on, so a host that
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

// The launch line DELETES THE WHOLE CLAUDE_* FAMILY from the child's environment
// except claudeEnvAllowedNames, instead of naming variables: other variables
// move the file the child reads (CLAUDE_CONFIG_DIR moves the whole config dir;
// CLAUDE_CODE_CUSTOM_OAUTH_URL makes it read .claude-custom-oauth.json), and
// claude 2.1.268 lists 841 CLAUDE_*/ANTHROPIC_* names that change every release.
//
// ANTHROPIC_* is left alone deliberately — measured, not reasoned (claude
// 2.1.268): ANTHROPIC_CONFIG_DIR does not move the config layout, and the family
// carries the direct credentials the spawn gate accepts (claudeCredEnvKeys), so
// purging it would log those hosts out.
const claudeEnvPurgePrefix = "CLAUDE_"

func claudeEnvAllowedNames() []string {
	out := make([]string, 0, len(claudeCredEnvKeys))
	for _, k := range claudeCredEnvKeys {
		if strings.HasPrefix(k, claudeEnvPurgePrefix) {
			out = append(out, k)
		}
	}
	return out
}

// The fragment must sit on the launch line AFTER the agent env render is
// sourced — the render is one of the places an unknown CLAUDE_* arrives from, so
// a purge placed above it clears nothing.
//
//   - /usr/bin/env is ABSOLUTE (as /bin/cat is on the OC_TOKEN line): the owner's
//     env file may leave PATH in any state, and an unresolved `env` makes the
//     fragment a silent no-op.
//   - The identifier check before `unset` keeps a malformed word from printing a
//     shell error onto the agent's first line.
//   - tmux launches the child under its default-shell (/bin/zsh on these
//     machines), not /bin/sh; TestBuildLaunchCommandWithEnv runs the prologue
//     under every shell on the host.
//
// TWO WAYS THIS GOES SILENTLY DEAD that no test here can see: an env file that
// changes IFS makes `$(/usr/bin/env)` one word and the purge unsets NOTHING; and
// a `readonly` CLAUDE_* survives `unset`, keeps redirecting the child, and puts
// the shell's complaint on the agent's FIRST LINE.
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
