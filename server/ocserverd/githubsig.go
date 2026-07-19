package main

// githubsig.go — GitHub signed-webhook verification for the PUBLIC /in inlet
// (platform == "github").
//
// GitHub signs each webhook delivery with an HMAC-SHA256 over the EXACT raw
// request body keyed by the secret configured on the webhook, sent as
// `X-Hub-Signature-256: sha256=<hex>`. We recompute the MAC over the same raw
// bytes and compare in constant time (crypto/hmac.Equal) — same discipline as
// sharesig.go / slacksig.go. (GitHub also sends a legacy SHA-1
// X-Hub-Signature; we verify the SHA-256 header only, per current guidance.)

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// verifyGithubSignature reports whether xHubSignature256 is a valid GitHub
// sha256 signature for rawBody under secret. Missing inputs or a MAC mismatch →
// false. Constant-time compare (hmac.Equal).
func verifyGithubSignature(secret, xHubSignature256 string, rawBody []byte) bool {
	if secret == "" || xHubSignature256 == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(xHubSignature256))
}
