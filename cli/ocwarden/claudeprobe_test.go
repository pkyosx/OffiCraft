// Skeleton generated from cli/ocwarden/claudeprobe.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestNewClaudeProber(t *testing.T) {
	t.Skip("TODO: newClaudeProber wires the real seams.")
}

func TestCollect(t *testing.T) {
	t.Skip("TODO: collect returns the current claude probe map (empty = nothing probed → buildTelemetryPayload omits the field).")
}

func TestVersion(t *testing.T) {
	t.Skip("TODO: version resolves the claude binary (the spawn chain) and returns its `--version` first token (\"2.1.211 (Claude Code)\" → \"2.1.211\"), \"\" on any miss (unresolved / stat fault / exec fault / empty output) — fail-soft, the payload just omits the field.")
}

func TestClaudeCredSubscriptionType(t *testing.T) {
	t.Skip("TODO: claudeCredSubscriptionType extracts claudeAiOauth.subscriptionType from the credentials file at path, \"\" on any miss (no file / bad json / field blank).")
}
