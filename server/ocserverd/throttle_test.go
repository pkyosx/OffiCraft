// Skeleton generated from server/ocserverd/throttle.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestBegin(t *testing.T) {
	t.Skip("TODO: begin is THE gate every credential seam must call.")
}

func TestFailureFloor(t *testing.T) {
	t.Skip("TODO: failureFloor is the wall-clock this server spends on a refused front-door credential attempt.")
}

func TestHoldFailureFloor(t *testing.T) {
	t.Skip("TODO: holdFailureFloor blocks until `started` plus the floor has elapsed, then returns.")
}

func TestWriteThrottled(t *testing.T) {
	t.Skip("TODO: writeThrottled answers a rate-limited credential attempt: 429 with a Retry-After header (HTTP's own vocabulary for this, so a client does not have to parse prose) through the SAME error envelope every other refusal uses.")
}
