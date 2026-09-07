// Skeleton generated from cli/ocwarden/agentenv.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestLoadAgentEnv(t *testing.T) {
	t.Skip("TODO: loadAgentEnv reads and parses the agent env file at path.")
}

func TestParseAgentEnv(t *testing.T) {
	t.Skip("TODO: parseAgentEnv is the PURE parser — split out from the I/O so the format contract is testable without touching a filesystem.")
}

func TestParseAgentEnvValue(t *testing.T) {
	t.Skip("TODO: parseAgentEnvValue turns the raw right-hand side into the final value, plus a warning string (\"\" when there is nothing to say).")
}

func TestRenderAgentEnvFile(t *testing.T) {
	t.Skip("TODO: renderAgentEnvFile renders validated pairs into the content of the workdir 0600 file the launch line sources.")
}

func TestAgentEnvKeyNames(t *testing.T) {
	t.Skip("TODO: agentEnvKeyNames returns just the KEY NAMES, for a log line that proves what was loaded WITHOUT printing a single value.")
}
