package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// githubSign mirrors GitHub's X-Hub-Signature-256 construction (sha256={hex of
// HMAC over the raw body}) as an independent test oracle.
func githubSign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyGithubSignature(t *testing.T) {
	secret := "github-webhook-secret"
	body := `{"action":"opened","number":42}`
	sig := githubSign(secret, body)

	if !verifyGithubSignature(secret, sig, []byte(body)) {
		t.Fatal("a freshly minted GitHub signature must verify")
	}
	// Tampered body → reject.
	if verifyGithubSignature(secret, sig, []byte(body+"x")) {
		t.Fatal("a body tamper must not verify")
	}
	// Tampered signature → reject.
	if verifyGithubSignature(secret, sig[:len(sig)-1]+"0", []byte(body)) {
		t.Fatal("a tampered signature must not verify")
	}
	// Wrong secret → reject.
	if verifyGithubSignature("other-secret", sig, []byte(body)) {
		t.Fatal("a signature must not verify under another secret")
	}
	// Empty inputs → reject.
	if verifyGithubSignature("", sig, []byte(body)) {
		t.Fatal("empty secret must not verify")
	}
	if verifyGithubSignature(secret, "", []byte(body)) {
		t.Fatal("empty signature must not verify")
	}
}
