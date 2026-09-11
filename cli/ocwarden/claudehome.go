package main

// claudehome.go — WHICH claude.json the spawned child reads, decided by the
// warden instead of predicted from the environment (T-180).
//
// The pre-trust write (spawn.go pretrustWorkdir) and the child's read have to
// land on the same file, or the trust dialog fires, eats the boot nudge, and the
// member is dead within a second while every receipt says the spawn succeeded.
//
// Two environment variables decide the child's answer, and claude's own layout
// is NOT symmetric between them (measured 2026-09-11 with claude 2.1.268):
//
//	CLAUDE_CONFIG_DIR unset → $HOME/.claude.json, everything else $HOME/.claude/
//	CLAUDE_CONFIG_DIR=D     → D/.claude.json, and D holds projects/ sessions/
//	                          backups/ .credentials.json too
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
// it. That is what makes the pairing causal rather than predicted.
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

// ClaudeJSONPath is the one file both ends use: what pretrustWorkdir writes and
// what the child reads. It is a function of the two exported values, which is
// the whole guarantee — there is no second resolution anywhere that could
// disagree with this one.
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
		return claudeHome{}, fmt.Errorf("OC_CLAUDE_JSON=%q resolves to %q, whose filename is not %s: the child reads <config home>/%s and nothing else, so a file under another name can never be the file it reads — point OC_CLAUDE_JSON at a directory's %s",
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
