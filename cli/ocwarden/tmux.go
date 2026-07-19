// Shared tmux read helpers + member-session naming — the READ-ONLY tmux surface
// the spawn (Phase 2) and kill (Phase 3) mechanisms both build on. These used to
// live alongside the presence loop; presence has been removed from the warden
// (the server now projects presence from its own SSE connection view), so only the
// helpers the spawn/kill/command surfaces actually consume remain here.
package main

import "strings"

const (
	// tmuxSocket is the shared -L socket name (spawn.DEFAULT_SOCKET). It MUST
	// match the Python daemon during the migration window so both read/adopt the
	// same server.
	tmuxSocket = "officraft"
	// memberSessionPrefix: every member-spawned tmux session is named
	// "member-<id>" (id lowercased) — the naming convention the spawn port binds
	// and the kill mechanism's isMemberSession guard checks against.
	memberSessionPrefix = "member-"
)

// memberSessionName is the canonical tmux session name a member is spawned
// under: "member-<id>" (id lowercased).
func memberSessionName(memberID string) string {
	return memberSessionPrefix + strings.ToLower(memberID)
}

// tmuxClassifyAbsent reports whether a tmux non-zero error text is the benign
// "positively absent" case (session missing, or no server ever started on the
// socket) as opposed to a broken/unclassifiable probe.
func tmuxClassifyAbsent(errText string) bool {
	s := strings.ToLower(errText)
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "can't find session") ||
		strings.Contains(s, "session not found") ||
		(strings.Contains(s, "error connecting") && strings.Contains(s, "no such file or directory"))
}

// tmuxHasSession is the THREE-WAY session probe over the runner seam:
//
//	*bool -> true  : present (tmux exited 0)
//	*bool -> false : POSITIVELY absent (ran cleanly, or benign no-server)
//	nil            : probe BROKEN (binary missing / timeout / unclassifiable) →
//	                 callers read this as UNKNOWN, conservatively alive.
func tmuxHasSession(r CmdRunner, socket, session string) *bool {
	_, err := r.Run("tmux", "-L", socket, "has-session", "-t", session)
	if err == nil {
		t := true
		return &t
	}
	if tmuxClassifyAbsent(err.Error()) {
		f := false
		return &f
	}
	return nil // unclassifiable → UNKNOWN, never "absent"
}

// tmuxPanePID returns the session's pane pid (#{pane_pid}), or "" on any fault
// or non-numeric output. Mirrors pane_pid.
func tmuxPanePID(r CmdRunner, socket, session string) string {
	out, err := r.Run("tmux", "-L", socket, "display-message", "-p", "-t", session, "#{pane_pid}")
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(out)
	if s == "" {
		return ""
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return s
}
