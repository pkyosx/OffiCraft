package main

// claudetrust.go — T-180, fifth shape: the guarantee is stated on the RESULT.
//
// WHAT WENT WRONG FOUR TIMES. The pre-trust write (spawn.go pretrustWorkdir) and
// the claude child's read have to land on the same file, or the trust dialog
// fires, eats the boot nudge, and the member is dead within a second while every
// receipt still says the spawn succeeded. Four independent reviews each broke the
// previous fix the SAME way — one more route was found that sends claude to a
// different file, and every check we had still reported success:
//
//	1. the flag went to a file with no reader (the child's HOME was not ours)
//	2. CLAUDE_CONFIG_DIR moves the whole config directory
//	3. CLAUDE_CODE_CUSTOM_OAUTH_URL changes the FILENAME read inside it
//	4. <config dir>/.config.json, when it EXISTS, is read INSTEAD of .claude.json
//
// The first three are environment variables, and claudehome.go answers that whole
// family structurally (the CLAUDE_* purge). The fourth is not an input at all —
// it is a file's existence — so no amount of environment hygiene can reach it.
// Every version so far pinned its guarantee to a MIDDLE quantity ("the child's
// environment holds nothing we did not put there"), and a middle quantity can
// always be bypassed by a route that does not pass through it.
//
// WHAT THIS FILE DOES INSTEAD. It states the guarantee on the result we actually
// want:
//
//	the file the spawned claude really reads is one that already carries our
//	trust flag for this workdir
//
// and it establishes that by ASKING THE CLAUDE BINARY ITSELF, once per spawn,
// under the same environment prologue the child will run under. There is no
// model of claude's resolution logic here to drift out of date: the resolution is
// performed by claude, on the machine, at the moment of the spawn.
//
// 🔴 WHY NOT PORT claude's RESOLVER TO GO. It would be a fifth prediction. The
// 2.1.268 resolver is three functions deep (a `.config.json` existence test in
// front of a CLAUDE_CONFIG_DIR-or-homedir join with a suffix chosen by yet
// another variable), and the day it changes, a Go copy of it keeps returning the
// old answer with nothing anywhere turning red. Copying it is how this defect
// comes back a fifth time, not how it stops.
//
// WHY THE PROBE CANNOT DRIFT SILENTLY. It is a POSITIVE-ANSWER-ONLY check: the
// spawn proceeds only when claude's own output names this workdir as a project it
// holds. Anything else — a changed output format, a removed subcommand, a crash,
// a timeout, an unresolvable binary, or a genuine "I read a different file" — is
// NOT a positive answer, so it refuses the spawn by name. Drift therefore shows
// up as a loud refusal naming both files, never as a silent pass. That direction
// is the whole design: this ticket exists to kill a silent failure, so every
// unknown has to land on the refusing side.
//
// THE QUESTION IS A READ, AND THE VERB HAS NO DESTRUCTIVE READING. An earlier
// draft of this file asked `claude project purge <dir> --dry-run`, because that
// is the only command that reports claude's own view of projects[<dir>]. It was
// rejected, correctly: `--dry-run` being honoured is today's behaviour, not a
// contract, and this whole ticket exists because claude's behaviour has differed
// from our assumption four times running. A destructive verb on the spawn path
// bets on exactly the thing we keep losing, and the losing side is irreversible.
//
// `claude project` registers exactly ONE subcommand — `purge` — in 2.1.268. That
// is not a reading of --help: it is the commander registration inside the binary,
// `R.command("project")…command("purge [path]")`, with no list/show/info/status.
// So the question had to move to a family that has a read verb. `claude mcp get`
// resolves the SAME config file (project-scoped MCP servers live in
// projects[<cwd>].mcpServers there) and, asked for a name that does not exist,
// answers by listing the names it does hold:
//
//	No MCP server named "…". Configured servers: <names from the resolved config>
//
// MEASURED 2026-09-11, claude 2.1.268, darwin-arm64:
//   - asked for an ABSENT name it connects to NOTHING (a seeded server whose
//     command creates a marker file left no marker; asking for the name that
//     EXISTS created it, so the control is not blind), and
//   - it changes NOTHING in the config home: name, size, mtime and inode of every
//     file and directory two levels deep were byte-identical before and after,
//     not even a backups/ rotation.
//
// The cost is one ~0.1s subprocess per spawn, and it works while logged OUT.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// claudeShadowConfigName is the fourth-round finding, kept here as a NAME so the
// refusal can say what to look for. When this file exists inside the config
// directory, claude 2.1.268 reads it and never opens .claude.json — measured in
// both directions (with it present the trust record landed in it and .claude.json
// was not touched by a single byte; with it absent the record landed in
// .claude.json). It is deliberately NOT used to compute anything: the probe asks
// claude, it does not look for this file.
const claudeShadowConfigName = ".config.json"

// claudeTrustProbeFallbackShell runs the probe when tmux cannot be asked which
// shell it will launch the child under (a server that is not up yet cannot answer).
// The prologue is POSIX and behaves the same here; see claudehome.go's shell notes
// for what was and was not verified about that.
const claudeTrustProbeFallbackShell = "/bin/sh"

// claudeTrustProbeSeedPrefix names the entry the probe puts into the file it just
// wrote, and then reads back through claude. A witness has to be something WE put
// in THAT file: the trust flag itself is not echoed by any read command claude
// 2.1.268 has.
//
// The entry is a project-scoped MCP server under a name derived from the workdir,
// pointing at /usr/bin/false. It is written after the trust flag and removed again
// before the child launches, through the same atomic read-modify-write the trust
// flag uses (editClaudeProjectEntry), so it can never truncate or clobber a live
// config. A crash inside that window leaves one inert entry that the next spawn's
// clear removes; claude would show it as a server that fails to start, which is
// visible and harmless — deliberately preferred to a verb that could delete.
const (
	claudeTrustProbeSeedPrefix = "oc-pretrust-probe-"
	// claudeTrustProbeInertCommand exits 1 immediately and reads nothing, so even
	// a leftover seed cannot do anything if some other code path starts it.
	claudeTrustProbeInertCommand = "/usr/bin/false"
	// claudeTrustProbeMissingName is the name the probe ASKS about, chosen so it
	// is never seeded and — load-bearing — so it does not contain the seed prefix.
	// claude echoes the name it was asked for; a query name carrying the prefix
	// would answer the witness check with our own question.
	claudeTrustProbeMissingName = "oc-trust-probe-missing"
)

// claudeTrustProbeSeedName derives the witness name from the workdir, so an entry
// left over from another agent cannot answer this spawn's question.
func claudeTrustProbeSeedName(workdir string) string {
	sum := sha256.Sum256([]byte(workdir))
	return claudeTrustProbeSeedPrefix + hex.EncodeToString(sum[:6])
}

// claudeTrustProbeArgs is the READ question put to the claude binary: "what MCP
// servers do you hold for this project?", asked in the form that makes claude
// answer without connecting to any of them.
//
// ⚠️ `get` and `list` are the only verbs this may ever be. TestClaudeTrustProbeArgs
// pins that no verb here can add, remove or reset anything.
func claudeTrustProbeArgs() []string {
	return []string{"mcp", "get", claudeTrustProbeMissingName}
}

// claudeTrustWitness is the string claude prints only when the config file IT
// resolved is the file we seeded — the workdir-derived server name, echoed back in
// its "Configured servers:" list.
func claudeTrustWitness(workdir string) string {
	return claudeTrustProbeSeedName(workdir)
}

// claudeTrustProbeScript renders the shell line the probe runs. Its environment
// prologue is claudeChildEnvPrologue + claudeHomeExportPairs — THE SAME TWO
// helpers buildLaunchCommandWithEnv uses, not a copy of them — so the probe
// cannot end up measuring an environment the launch line does not produce.
func claudeTrustProbeScript(claudeBin, workdir, envRendered string, ch claudeHome) string {
	s := claudeChildEnvPrologue(workdir, envRendered, ch)
	if pairs := claudeHomeExportPairs(ch); len(pairs) > 0 {
		kvs := make([]string, 0, len(pairs))
		for _, p := range pairs {
			kvs = append(kvs, p[0]+"="+shellQuote(p[1]))
		}
		s += "export " + strings.Join(kvs, " ") + "; "
	}
	parts := make([]string, 0, 5)
	parts = append(parts, shellQuote(claudeBin))
	for _, a := range claudeTrustProbeArgs() {
		parts = append(parts, shellQuote(a))
	}
	return s + "exec " + strings.Join(parts, " ")
}

// verifyClaudeSeesPretrust seeds a witness into the file pre-trust just wrote,
// asks claude to read its own project config back, and returns nil ONLY when the
// witness comes back. Every other outcome returns an error naming the file we
// wrote and the workdir we wrote it for, plus whatever claude actually said — the
// two halves an operator needs to tell "it reads a different file" from "the
// question stopped working".
//
// The seed is removed on every exit path. A clear that fails is LOUD on warden
// stderr but does not fail the spawn: the leftover is one inert entry, and
// refusing to launch a member over cosmetic residue would trade a real outage for
// a tidiness problem. That is the one best-effort step in this file, and it is
// best-effort about CLEANUP, never about the answer.
func verifyClaudeSeesPretrust(ask func(script string) (string, error), logf func(string, ...any), claudeBin, workdir, envRendered string, ch claudeHome) error {
	wrote := ch.ClaudeJSONPath()
	if claudeBin == "" {
		return fmt.Errorf("no claude executable is resolved, so nothing can be asked which config file it will read; the trust flag for %s was written to %s and may have no reader", workdir, wrote)
	}
	if ask == nil {
		return fmt.Errorf("the pre-trust probe has no way to run %s, so nothing establishes that it reads %s (the trust flag for %s was written there)", claudeBin, wrote, workdir)
	}
	seed := claudeTrustProbeSeedName(workdir)
	if err := seedTrustProbeWitness(wrote, workdir, seed); err != nil {
		return fmt.Errorf("the pre-trust witness could not be written into %s (%v), so the spawned claude cannot be asked whether it reads that file", wrote, err)
	}
	defer func() {
		if err := clearTrustProbeWitness(wrote, workdir, seed); err != nil && logf != nil {
			logf("[ocwarden spawn] the pre-trust probe left %q in %s (%v); it is inert (%s) and the next spawn clears it",
				seed, wrote, err, claudeTrustProbeInertCommand)
		}
	}()

	out, err := ask(claudeTrustProbeScript(claudeBin, workdir, envRendered, ch))
	if strings.Contains(out, claudeTrustWitness(workdir)) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s could not be asked which config it reads for %s (%v); the trust flag was written to %s, and an unanswered question is refused rather than spawned into — claude said: %s",
			claudeBin, workdir, err, wrote, oneLineExcerpt(out))
	}
	return fmt.Errorf("%s does NOT read %s: the witness written there for %s never came back, so the file it reads is a different one — look for a %s in the config directory, which claude reads instead of .claude.json whenever it exists; claude said: %s",
		claudeBin, wrote, workdir, claudeShadowConfigName, oneLineExcerpt(out))
}

// seedTrustProbeWitness writes the witness entry into the file pre-trust wrote.
func seedTrustProbeWitness(claudeJSONPath, workdir, name string) error {
	return editClaudeProjectEntry(claudeJSONPath, workdir, func(entry map[string]any) {
		servers, ok := entry["mcpServers"].(map[string]any)
		if !ok {
			servers = map[string]any{}
			entry["mcpServers"] = servers
		}
		servers[name] = map[string]any{
			"type":    "stdio",
			"command": claudeTrustProbeInertCommand,
			"args":    []any{},
		}
	})
}

// clearTrustProbeWitness removes it again. An mcpServers map left EMPTY by the
// removal is dropped, which is how the entry looked before any probe ran in the
// common case; an owner who had written an explicitly empty map loses only that
// empty map, which means the same thing.
func clearTrustProbeWitness(claudeJSONPath, workdir, name string) error {
	return editClaudeProjectEntry(claudeJSONPath, workdir, func(entry map[string]any) {
		servers, ok := entry["mcpServers"].(map[string]any)
		if !ok {
			return
		}
		delete(servers, name)
		if len(servers) == 0 {
			delete(entry, "mcpServers")
		}
	})
}

// oneLineExcerpt folds claude's answer onto one line and bounds it, so a refusal
// stays a one-line last_op_reason. Only the probe's own output reaches here, and
// the probe is scoped to one workdir, so it carries no other project's paths.
func oneLineExcerpt(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if s == "" {
		return "(nothing)"
	}
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// tmuxLaunchShell asks tmux which shell it will run the launch line under, so the
// probe's prologue executes in the same dialect the child's does. A tmux server
// that is not up yet cannot answer; that case falls back to POSIX sh and SAYS SO
// on warden stderr rather than pretending it knew.
func tmuxLaunchShell(r CmdRunner, socket string, logf func(string, ...any)) string {
	if r == nil {
		return claudeTrustProbeFallbackShell
	}
	out, err := r.Run("tmux", "-L", socket, "show-options", "-gv", "default-shell")
	shell := strings.TrimSpace(out)
	if err == nil && strings.HasPrefix(shell, "/") {
		return shell
	}
	// 🔴 SAY IT OUT LOUD. This is a KNOWN, ACCEPTED divergence, not a neutral
	// default: the probe is about to measure the child's environment in a
	// different shell than the child will run in. It is accepted because the tmux
	// server is usually not up before the FIRST spawn on a host, and refusing
	// there would make every host's first member unlaunchable. Accepted is not the
	// same as invisible — an operator debugging a pre-trust refusal has to be able
	// to see that this spawn's probe ran in the wrong dialect.
	if logf != nil {
		reason := fmt.Sprintf("answered %q, which is not an absolute path", shell)
		if err != nil {
			reason = fmt.Sprintf("could not be asked (%v)", err)
		}
		logf("[ocwarden spawn] pre-trust probe shell: tmux -L %s %s, so the probe runs under %s while the child will run under tmux's default-shell — a shell-specific difference in the CLAUDE_* purge would not be visible to this spawn's probe",
			socket, reason, claudeTrustProbeFallbackShell)
	}
	return claudeTrustProbeFallbackShell
}

// newClaudePretrustVerifier wires the real probe. It is built once per warden and
// bound per spawn (the workdir and the rendered env file only exist once
// StartParams names a member), mirroring how Pretrust itself is bound.
func newClaudePretrustVerifier(runner CmdRunner, socket, claudeBin string, ch claudeHome, logf func(string, ...any)) func(workdir, envRendered string) error {
	return func(workdir, envRendered string) error {
		shell := tmuxLaunchShell(runner, socket, logf)
		return verifyClaudeSeesPretrust(func(script string) (string, error) {
			return runner.Run(shell, "-c", script)
		}, logf, claudeBin, workdir, envRendered, ch)
	}
}
