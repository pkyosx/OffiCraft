// Skeleton generated from cli/ocagent/clean.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestCleanRoot(t *testing.T) {
	t.Skip("TODO: OC_BASE CLASSIFICATION: EXEMPT, and the apparent contradiction in the original proposal dissolves once the two variables are kept apart.")
}

func TestCanonicalise(t *testing.T) {
	t.Skip("TODO: canonicalise resolves a path through symlinks WITHOUT requiring it to exist: it walks up to the deepest ancestor that RESOLVES, resolves that, and re-attaches the tail it walked past.")
}

func TestIsUnder(t *testing.T) {
	t.Skip("TODO: isUnder reports whether path is strictly inside root (the root itself is not).")
}

func TestInsideRoot(t *testing.T) {
	t.Skip("TODO: insideRoot runs the whole in-my-workdir judgement on ONE path.")
}

func TestResolveInsideRoot(t *testing.T) {
	t.Skip("TODO: resolveInsideRoot proves one caller-named path lands strictly inside root and returns the path the move must actually operate on.")
}

func TestQuarantineDest(t *testing.T) {
	t.Skip("TODO: quarantineDest picks where one target lands under <root>/trash/, PRESERVING its path relative to the root so the move stays readable (\"what was this?\" is answerable a day later).")
}

func TestCmdClean(t *testing.T) {
	t.Skip("TODO: cmdClean is the whole command: validate EVERY path, then move.")
}
