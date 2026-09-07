// Skeleton generated from cli/ocwarden/tmux.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestTmuxClassifyAbsent(t *testing.T) {
	t.Skip("TODO: tmuxClassifyAbsent reports whether a tmux non-zero error text is the benign \"positively absent\" case (session missing, or no server ever started on the socket) as opposed to a broken/unclassifiable probe.")
}

func TestTmuxHasSession(t *testing.T) {
	t.Skip("TODO: tmuxHasSession is the THREE-WAY session probe over the runner seam: *bool -> true : present (tmux exited 0) *bool -> false : POSITIVELY absent (ran cleanly, or benign no-server) nil : probe BROKEN (binary missing / timeout / unclassifiable) → callers read this as UNKNOWN, conservatively alive.")
}

func TestTmuxPanePID(t *testing.T) {
	t.Skip("TODO: tmuxPanePID returns the session's pane pid (#{pane_pid}), or \"\" on any fault or non-numeric output.")
}
