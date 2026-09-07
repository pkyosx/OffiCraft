// Skeleton generated from cli/ocwarden/namespace.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestNamespaceFromEnv(t *testing.T) {
	t.Skip("TODO: namespaceFromEnv reads + validates OC_NAMESPACE.")
}

func TestWardenLabelFor(t *testing.T) {
	t.Skip("TODO: wardenLabelFor derives the launchd label: canonical for the empty namespace, dot-suffixed otherwise (label syntax uses dots).")
}

func TestOfficraftRootFor(t *testing.T) {
	t.Skip("TODO: officraftRootFor derives the per-machine data root: ~/.officraft for the empty namespace, ~/.officraft-<ns> otherwise (path syntax uses a dash).")
}

func TestTmuxSocketFor(t *testing.T) {
	t.Skip("TODO: tmuxSocketFor derives the tmux -L socket: the shared canonical socket for the empty namespace, dash-suffixed otherwise.")
}
