// Skeleton generated from server/ocserverd/slacksig.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestVerifySlackSignature(t *testing.T) {
	t.Skip("TODO: verifySlackSignature reports whether xSlackSignature is a valid Slack v0 signature for rawBody under secret at xSlackTimestamp, within the replay window of now (unix seconds).")
}

func TestSlackURLVerificationChallenge(t *testing.T) {
	t.Skip("TODO: slackURLVerificationChallenge extracts the challenge string from a Slack url_verification handshake body ({\"type\":\"url_verification\",\"challenge\":..}).")
}
