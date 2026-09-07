// Skeleton generated from cli/ocwarden/basegate.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestBaseFromEnv(t *testing.T) {
	t.Skip("TODO: baseFromEnv resolves OC_BASE and says WHETHER IT WAS THERE — the distinction the old loadConfig threw away.")
}

func TestNoBaseMessage(t *testing.T) {
	t.Skip("TODO: noBaseMessage is what a person finds when they go looking.")
}

func TestNoBaseSentinelPath(t *testing.T) {
	t.Skip("TODO: noBaseSentinelPath is where the record goes: the per-instance warden dir, derived from HOME and OC_NAMESPACE ONLY.")
}

func TestWriteNoBaseSentinel(t *testing.T) {
	t.Skip("TODO: writeNoBaseSentinel drops the record best-effort and returns where it went (or why it did not).")
}

func TestStationAddressGate(t *testing.T) {
	t.Skip("TODO: stationAddressGate is the ONE call site of this whole file, and it is one on purpose.")
}

func TestHaltNoBase(t *testing.T) {
	t.Skip("TODO: haltNoBase is the whole refusal: say it, record it, then stop doing anything at all until the process is signalled.")
}

func TestBlockUntilSignal(t *testing.T) {
	t.Skip("TODO: blockUntilSignal parks until SIGINT/SIGTERM.")
}
