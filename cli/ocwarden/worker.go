// worker.go — the LEGACY outsource-worker session shape. An outsource worker now
// rides the MEMBER verbs: the server pushes a plain `start` (member_id == the ow- id),
// the session is `member-<ow-id>` under agents/, and every kill is the plain member
// `stop`. What remains here is ONLY the transition guard for the retired
// `worker-<ow-id>` namespace — sessions (and workers/ workdirs) spawned by an older
// build must never become unkillable:
//
//   - the kill ladder's outer gate still admits worker-* (kill.go stop());
//   - a member `stop` additionally sweeps the derived legacy worker-<id> session
//     (command.go rpcStop — exact name, never a pattern);
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
	workerSessionPrefix = "worker-"
)

func workerSessionName(workerID string) string {
	return workerSessionPrefix + strings.ToLower(workerID)
}

func isWorkerSession(session string) bool {
	return strings.HasPrefix(session, workerSessionPrefix) && len(session) > len(workerSessionPrefix)
}

func workerWorkdirForSession(home, session string) string {
	if home == "" || !isWorkerSession(session) {
		return ""
	}
	return agentWorkdir(home, strings.TrimPrefix(session, workerSessionPrefix))
}

func defaultWorkerHome(env func(string) string) string {
	if h := env("OC_AGENT_HOME"); h != "" {
		return filepath.Join(filepath.Dir(h), "workers")
	}
	ns, _ := namespaceFromEnv(env)
	home, _ := os.UserHomeDir()
	return filepath.Join(officraftRootFor(home, ns), "workers")
}

// Derived from worker_id ONLY, never a raw session name: the EXACT-kill contract
// has one derivation, one guard.
func workerStopSessionFromArgs(args map[string]any) (string, error) {
	if id, ok := argString(args, "worker_id"); ok && strings.TrimSpace(id) != "" {
		return workerSessionName(id), nil
	}
	return "", fmt.Errorf("command: worker_stop missing worker_id")
}

// Only an ow- (outsource) id yields a name — the one namespace the retired
// worker-* sessions were ever minted for; anything else reads "" (never a guess).
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
