package main

// The ONE place the owner-facing attach command is composed; every client shows
// this string verbatim and none may assemble one from the parts.
//
// 🔴 Assumes the session's tmux server is a warden THIS station installed, so
// this station's own [server].namespace keys the socket (owner ruling C1 of
// `rc-0869c4822345`); a warden installed by another station gets a socket name
// that does not exist on that host.
//
// 🔴 The socket rule mirrors cli/ocwarden/namespace.go (`tmuxSocketFor`) across
// a module boundary that forbids importing it — a second reader, never a second
// rule.

import "strings"

const (
	// cli/ocwarden's `tmuxSocket`, byte-identical.
	tmuxSocketBase = "officraft"
	// cli/ocwarden's `memberSessionPrefix`; outsource workers also boot as
	// `member-<ow-id>` (cli/ocwarden/worker.go).
	memberSessionPrefix = "member-"
)

func tmuxSocketForNamespace(ns string) string {
	if ns == "" {
		return tmuxSocketBase
	}
	return tmuxSocketBase + "-" + ns
}

// Mirrors cli/ocwarden/tmux.go memberSessionName, including the lowercasing.
func memberTmuxSessionName(agentID string) string {
	return memberSessionPrefix + strings.ToLower(agentID)
}

// Unconditional, even when the row has no session (owner 2026-09-08): clients
// reserve the empty string for "server too old to serve the field" and key
// their fallback off it.
func terminalAttachCommand(ns, agentID string) string {
	return "tmux -L " + tmuxSocketForNamespace(ns) + " attach -t " + memberTmuxSessionName(agentID)
}
