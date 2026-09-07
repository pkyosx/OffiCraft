// Skeleton generated from cli/ocwarden/teardown.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestResolvedLabel(t *testing.T) {
	t.Skip("TODO: resolvedLabel is the launchd label teardown acts on: the namespace-derived p.label when resolved, else the canonical wardenLabel.")
}

func TestResolveTeardownPaths(t *testing.T) {
	t.Skip("TODO: resolveTeardownPaths derives the removal targets from HOME + uid (+ the OC_NAMESPACE instance key).")
}

func TestRemoveFileTo(t *testing.T) {
	t.Skip("TODO: removeFileTo deletes one path idempotently, emitting its outcome through the injected logf sink (so both the CLI streaming logger and doTeardown's capture buffer share ONE removal body): a missing file is success (the teardown idempotence contract), any other error is logged as a warning and teardown continues (best-effort cleanup — never abort a teardown on a single stubborn file).")
}

func TestRemoveFile(t *testing.T) {
	t.Skip("TODO: removeFile is the installer-method shim kept for the existing CLI path: it streams through i.logf and discards the per-file verdict (the CLI teardown is best-effort and idempotent — it does not gate on a single stubborn file).")
}

func TestDoTeardown(t *testing.T) {
	t.Skip("TODO: doTeardown is the PURE teardown core, extracted so it can be driven BOTH by the `ocwarden teardown` CLI (teardownCmd) AND by the server-directed uninstall RPC (dispatchCommand).")
}

func TestRunTeardown(t *testing.T) {
	t.Skip("TODO: runTeardown executes the symmetric teardown for the CLI entry point by delegating to the pure doTeardown core and STREAMING its captured transcript through i.out (so the CLI's live output is unchanged).")
}

func TestValidateTeardownTarget(t *testing.T) {
	t.Skip("TODO: validateTeardownTarget makes the destructive CLI fail closed.")
}

func TestTeardownCmd(t *testing.T) {
	t.Skip("TODO: teardownCmd is the thin `ocwarden teardown` entry point.")
}
