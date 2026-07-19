// worker.go — the LEGACY outsource-worker session shape (M3 Phase 6, retired
// by the A案 P5b naming convergence).
//
// Since P5b an outsource worker rides the MEMBER verbs and namespace: the
// server pushes a plain `start` (member_id == the ow- id), the executor boots
// tmux session `member-<ow-id>` under the agents/ workdir root, and every kill
// is the plain member `stop`. What remains here is ONLY the transition guard
// for the retired `worker-<ow-id>` namespace — sessions (and workers/ workdirs)
// spawned by a pre-P5b build must never become unkillable:
//
//   - the kill ladder's outer gate still admits worker-* (kill.go stop());
//   - a member `stop` additionally sweeps the derived legacy worker-<id>
//     session (command.go rpcStop — exact name, never a pattern);
//   - the legacy `worker_stop` verb stays accepted as an alias (an old server
//     reclaiming through a new warden);
//   - workerWorkdirForSession / defaultWorkerHome keep resolving the legacy
//     workers/ workdir so the sweep's lsof leg still reaps a detached
//     `ocagent listen` anchored there.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// workerSessionPrefix namespaces every LEGACY outsource-worker tmux session:
	// "worker-<ow-id>" (id lowercased). DISJOINT from memberSessionPrefix by
	// construction — new spawns never mint it; it survives only as the kill
	// side's transition target.
	workerSessionPrefix = "worker-"
)

// workerSessionName is the canonical tmux session name a worker runs under:
// "worker-<ow-id>" (id lowercased — same normalization as members).
func workerSessionName(workerID string) string {
	return workerSessionPrefix + strings.ToLower(workerID)
}

// isWorkerSession reports whether session is a real worker-<id> tmux session
// (prefix + a non-empty id). The worker twin of isMemberSession.
func isWorkerSession(session string) bool {
	return strings.HasPrefix(session, workerSessionPrefix) && len(session) > len(workerSessionPrefix)
}

// workerWorkdirForSession derives the worker's durable workdir from its session
// name (worker-<id> → <workerHome>/<id>) — the sweep's workdir leg for the
// worker stop. "" when the name is not a worker session or home is unknown.
func workerWorkdirForSession(home, session string) string {
	if home == "" || !isWorkerSession(session) {
		return ""
	}
	return agentWorkdir(home, strings.TrimPrefix(session, workerSessionPrefix))
}

// defaultWorkerHome resolves the per-worker state base: the `workers/` SIBLING
// of the agents home. When OC_AGENT_HOME overrides the agents base (namespaced
// instances export it), workers follow as its sibling; otherwise
// ~/.officraft[-<ns>]/workers. Worker state NEVER lands under agents/ — the
// two populations stay physically disjoint, mirroring the session namespaces.
func defaultWorkerHome(env func(string) string) string {
	if h := env("OC_AGENT_HOME"); h != "" {
		return filepath.Join(filepath.Dir(h), "workers")
	}
	ns, _ := namespaceFromEnv(env)
	home, _ := os.UserHomeDir()
	return filepath.Join(officraftRootFor(home, ns), "workers")
}

// workerStopSessionFromArgs resolves the tmux session the LEGACY worker_stop
// alias targets: derived from worker_id ONLY (identity-addressed, never a raw
// session name — the EXACT-kill contract has one derivation, one guard).
func workerStopSessionFromArgs(args map[string]any) (string, error) {
	if id, ok := argString(args, "worker_id"); ok && strings.TrimSpace(id) != "" {
		return workerSessionName(id), nil
	}
	return "", fmt.Errorf("command: worker_stop missing worker_id")
}

// legacyWorkerSessionFromArgs derives the RETIRED worker-<id> session name from
// a member stop's member_id — the P5b transition sweep target (command.go
// rpcStop). Only an OUTSOURCE identity (the ow- prefix — the one namespace the
// retired worker-* sessions were ever minted for) yields one; a staff member id
// or a purely session-addressed stop reads "" (no sweep, never a guess).
func legacyWorkerSessionFromArgs(args map[string]any) string {
	id, ok := argString(args, "member_id")
	if !ok || strings.TrimSpace(id) == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(id)), "ow-") {
		return ""
	}
	return workerSessionName(id)
}
