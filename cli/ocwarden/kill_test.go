// Skeleton generated from cli/ocwarden/kill.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestParseKillablePID(t *testing.T) {
	t.Skip("TODO: parseKillablePID validates a pane-pid string for the IRREVERSIBLE kill path.")
}

func TestKillSession(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- killSession — the GATED tmux kill helper (byte-for-byte port of the origin's tmux_kill_session).")
}

func TestDescendantPIDs(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- descendantPIDs walks the process tree via `ps -eo pid=,ppid=` and returns every descendant of root (children, grandchildren, …), NEVER root itself.")
}

func TestEscalateKill(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- escalateKill — the IRREVERSIBLE hard-kill escalation, run ONLY after the lightweight kill-session failed to take.")
}

func TestSnapshotMemberPIDs(t *testing.T) {
	t.Skip("TODO: snapshotMemberPIDs captures the member's full process footprint: the live pane pid + every descendant (tree walked while links are intact) + any ocagent process whose cwd is the member's workdir.")
}

func TestLivePIDs(t *testing.T) {
	t.Skip("TODO: livePIDs filters pids down to the ones signal-0 proves alive AND ours (err==nil).")
}

func TestSweepPIDs(t *testing.T) {
	t.Skip("TODO: sweepPIDs reaps every snapshot pid still alive: EXACT-PID SIGTERM (graceful — the ocagent listener traps SIGTERM) → grace poll → SIGKILL the survivors → poll until signal-0 proves all gone.")
}

func TestOcagentPIDsByCwd(t *testing.T) {
	t.Skip("TODO: ocagentPIDsByCwd is the production listenPIDs seam: lsof-discover every ocagent process whose cwd is EXACTLY workdir (the member's durable per-agent dir — the spawn shim cd's there before exec, so a member's `ocagent listen` inherits it; other members' listeners live in OTHER workdirs and never match).")
}

func TestMemberWorkdirForSession(t *testing.T) {
	t.Skip("TODO: memberWorkdirForSession derives the member's durable workdir from its session name (member-<id> → <home>/<id>, the same agentWorkdir the spawn used).")
}

func TestStop(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- stop — the SINGLE robust stop RPC surface (Seth-ruled), with a built-in escalation ladder.")
}
