// Skeleton generated from cli/ocwarden/worker.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestWorkerWorkdirForSession(t *testing.T) {
	t.Skip("TODO: workerWorkdirForSession derives the worker's durable workdir from its session name (worker-<id> → <workerHome>/<id>) — the sweep's workdir leg for the worker stop.")
}

func TestDefaultWorkerHome(t *testing.T) {
	t.Skip("TODO: defaultWorkerHome resolves the per-worker state base: the `workers/` SIBLING of the agents home.")
}

func TestWorkerStopSessionFromArgs(t *testing.T) {
	t.Skip("TODO: workerStopSessionFromArgs resolves the tmux session the LEGACY worker_stop alias targets: derived from worker_id ONLY (identity-addressed, never a raw session name — the EXACT-kill contract has one derivation, one guard).")
}

func TestLegacyWorkerSessionFromArgs(t *testing.T) {
	t.Skip("TODO: legacyWorkerSessionFromArgs derives the RETIRED worker-<id> session name from a member stop's member_id — the P5b transition sweep target (command.go rpcStop).")
}
