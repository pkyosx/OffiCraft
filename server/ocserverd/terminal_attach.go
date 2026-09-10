package main

// terminal_attach.go — the ONE place the owner-facing 「attach a terminal to this
// agent」 shell command is composed (T-139). Every client displays and copies the
// string this file returns, verbatim; none of them may assemble one out of the
// parts again.
//
// It exists because the command is NOT a constant. Its `tmux -L` socket is the
// bare `officraft` only on the main instance and `officraft-<ns>` on a
// namespaced one, so the cockpit's hardcoded `tmux -L officraft attach -t
// member-<id>` was correct for exactly one station and silently dropped the
// owner of any other into a DIFFERENT tmux server's sessions.
//
// 🔴 THE ASSUMPTION THIS RESTS ON, NAMED (owner 2026-09-08, C1 of
// `rc-0869c4822345`): the tmux server holding this row's session is the warden
// that THIS station installed, so this station's own [server].namespace keys it.
// That is what makes a socket name derivable here at all — the server never
// asks the host which socket it actually opened.
//
// WHEN IT BREAKS: an agent whose session lives on a warden installed by some
// OTHER station (a different OC_NAMESPACE, or a hand-installed warden) gets a
// socket name that does not exist on that host; `tmux attach` then fails to
// find the server and the owner sees an error instead of a session.
//
// WHY THAT IS STILL AN IMPROVEMENT: it is not worse than today under ANY
// station. Today the string is a client-side literal that says `officraft`
// unconditionally — already wrong for every namespaced station, including the
// one that the row does belong to. The cheap assumption makes the common case
// (one station, its own wardens) right and leaves the uncommon case exactly as
// broken as it already was.
//
// 🔴 THE SOCKET RULE IS NOT INVENTED HERE. It mirrors cli/ocwarden/namespace.go
// (`tmuxSocketFor`), whose package comment states the constraint in as many
// words: the four namespaced axes derive from ONE suffix and "nothing else may
// invent a suffix". This is a second reader of that rule, across a module
// boundary that forbids importing it — never a second rule.

import "strings"

const (
	// tmuxSocketBase is cli/ocwarden's `tmuxSocket`: the socket name the MAIN
	// (unnamespaced) instance's warden opens, byte-identical.
	tmuxSocketBase = "officraft"
	// agentSessionPrefix is cli/ocwarden's `memberSessionPrefix`. Outsource
	// workers ride it too: since the P5b naming convergence a worker boots the
	// tmux session `member-<ow-id>` (cli/ocwarden/worker.go), and `worker-<id>`
	// survives only as the kill side's legacy transition target — so ONE
	// derivation serves both DTOs.
	agentSessionPrefix = "member-"
)

// tmuxSocketForNamespace derives this station's `tmux -L` socket: the shared
// canonical name for the empty namespace, dash-suffixed otherwise. Mirrors
// cli/ocwarden/namespace.go tmuxSocketFor.
func tmuxSocketForNamespace(ns string) string {
	if ns == "" {
		return tmuxSocketBase
	}
	return tmuxSocketBase + "-" + ns
}

// agentTmuxSessionName is the canonical session name an agent — staff member or
// outsource worker — is spawned under: "member-<id>", id lowercased. Mirrors
// cli/ocwarden/tmux.go memberSessionName, including the lowercasing.
func agentTmuxSessionName(agentID string) string {
	return agentSessionPrefix + strings.ToLower(agentID)
}

// terminalAttachCommand composes the COMPLETE, ready-to-paste attach command
// for one agent row on this station. Unconditional: it never asks whether the
// row currently HAS a session, because the cockpit has always shown this line
// unconditionally and making it conditional would delete something the owner
// can see today (owner 2026-09-08). The empty string is therefore reserved for
// a different fact entirely — a server too old to serve the field at all — and
// clients key their fallback off exactly that.
func terminalAttachCommand(ns, agentID string) string {
	return "tmux -L " + tmuxSocketForNamespace(ns) + " attach -t " + agentTmuxSessionName(agentID)
}
