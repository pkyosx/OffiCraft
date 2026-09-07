// Skeleton generated from cli/ocwarden/interactiveenv.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestCaptureInteractiveEnv(t *testing.T) {
	t.Skip("TODO: captureInteractiveEnv is the real capture seam: it runs the interactive shell under a Go context timeout and returns its raw NUL-delimited stdout.")
}

func TestParseNulEnv(t *testing.T) {
	t.Skip("TODO: parseNulEnv parses NUL-delimited `KEY=VALUE` records into pairs, in the order the shell emitted them.")
}

func TestJoinInts(t *testing.T) {
	t.Skip("TODO: joinInts renders positions for the malformed-record warning.")
}

func TestMergeAgentEnv(t *testing.T) {
	t.Skip("TODO: mergeAgentEnv layers override ON TOP of base and returns the result.")
}

func TestOverriddenKeyNames(t *testing.T) {
	t.Skip("TODO: overriddenKeyNames returns the sorted names present in BOTH layers — the names the env file actually overrode.")
}
