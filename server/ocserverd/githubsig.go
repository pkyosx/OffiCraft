package main

// The MAC is over the EXACT raw request body. Only the SHA-256 header is
// verified; GitHub's legacy SHA-1 X-Hub-Signature is ignored per current
// guidance.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func verifyGithubSignature(secret, xHubSignature256 string, rawBody []byte) bool {
	if secret == "" || xHubSignature256 == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(xHubSignature256))
}
