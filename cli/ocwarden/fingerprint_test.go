// Skeleton generated from cli/ocwarden/fingerprint.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestNewBinFingerprinter(t *testing.T) {
	t.Skip("TODO: newBinFingerprinter targets the SAME live paths the self-update loop keeps current: ocwarden = our own executable (symlinks resolved), ocagent = the home sibling (selfUpdateAgentPath — unconditional, so a not-yet-populated sibling reads as absent until the first self-update tick materializes it).")
}
