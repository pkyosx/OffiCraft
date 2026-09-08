// Skeleton generated from server/ocserverd/githubsig.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestVerifyGithubSignature(t *testing.T) {
	const (
		secret = "github-webhook-secret"
		body   = `{"action":"opened","number":42}`
		sig    = "sha256=17e1a2914aed3f36630a70904e584c37ffaef0d874aea4e3cce44c9ae43bfa21"
	)
	for _, tc := range []struct {
		name   string
		secret string
		sig    string
		body   []byte
		want   bool
	}{
		{name: "the exact signed body verifies", secret: secret, sig: sig, body: []byte(body), want: true},
		{name: "changing one body byte invalidates the signature", secret: secret, sig: sig, body: []byte(`{"action":"opened","number":43}`), want: false},
		{name: "changing one signature byte is refused", secret: secret, sig: sig[:len(sig)-1] + "0", body: []byte(body), want: false},
		{name: "the same signature does not verify under another secret", secret: "other-secret", sig: sig, body: []byte(body), want: false},
		{name: "an empty secret is refused", secret: "", sig: sig, body: []byte(body), want: false},
		{name: "an empty signature is refused", secret: secret, sig: "", body: []byte(body), want: false},
		{name: "a signature without the sha256 prefix is refused", secret: secret, sig: sig[len("sha256="):], body: []byte(body), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifyGithubSignature(tc.secret, tc.sig, tc.body); got != tc.want {
				t.Fatalf("verifyGithubSignature(%q, %q, %q) = %v, want %v", tc.secret, tc.sig, tc.body, got, tc.want)
			}
		})
	}
}
