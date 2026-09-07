// Skeleton generated from cli/ocwarden/command.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestParseCommandFrame(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- parse — topic envelope → *Command.")
}

func TestTruncLog(t *testing.T) {
	t.Skip("TODO: truncLog clamps s to the command-result log cap (bytes).")
}

func TestReport(t *testing.T) {
	t.Skip("TODO: report is the nil-safe emit of ONE CommandResult.")
}

func TestDispatchCommand(t *testing.T) {
	t.Skip("TODO: dispatchCommand executes ONE parsed command.")
}

func TestArgString(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- arg extraction — GUARDED map reads (never a blind type assertion on untrusted input).")
}

func TestStartParamsFromArgs(t *testing.T) {
	t.Skip("TODO: startParamsFromArgs builds StartParams from the start args.")
}

func TestStopSessionFromArgs(t *testing.T) {
	t.Skip("TODO: stopSessionFromArgs resolves the tmux session name the robust stop must target.")
}
