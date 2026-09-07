// Skeleton generated from server/ocserverd/outsource_gate.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestOutsourceSpawnGate(t *testing.T) {
	t.Skip("TODO: outsourceSpawnGate is THE choke (④): authenticate the 發包, meter it (⑦), and decide admit vs deny.")
}

func TestMeterOutsourceDispatch(t *testing.T) {
	t.Skip("TODO: meterOutsourceDispatch is the ⑦ accounting seam — the SINGLE point every admitted dispatch (owner/admin included) passes, so quota/記帳 can never be bypassed by scope.")
}

func TestResolveDispatchInitiator(t *testing.T) {
	t.Skip("TODO: resolveDispatchInitiator classifies a dispatch initiator from its actor id (the verified token sub / a task's creator) where no *http.Request is in hand (the scheduler tick's typed-outsource auto-spawn): the owner literal → owner scope with a nil member; else the caller's member row → classifyMember.")
}
