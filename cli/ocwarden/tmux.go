package main

import "strings"

const (
	// Must match defaultTmuxSocket in cli/ocagent/listen.go; nothing checks the
	// two agree.
	tmuxSocket = "officraft"
	// kill.go's isMemberSession guard matches this prefix.
	memberSessionPrefix = "member-"
)

func memberSessionName(memberID string) string {
	return memberSessionPrefix + strings.ToLower(memberID)
}

// listenerSessionName: 🔴 the prefix must NOT be "member-": isMemberSession
// would treat "member-kyle-listen" as a member and resolve it to a workdir that
// does not exist.
func listenerSessionName(memberID string) string {
	return "listen-" + strings.ToLower(memberID)
}

func tmuxClassifyAbsent(errText string) bool {
	s := strings.ToLower(errText)
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "can't find session") ||
		strings.Contains(s, "session not found") ||
		(strings.Contains(s, "error connecting") && strings.Contains(s, "no such file or directory"))
}

// tmuxHasSession: nil means the probe itself broke; callers read it as UNKNOWN,
// conservatively alive.
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
