package main

// Share links: an HMAC over exactly what they grant, with no expiry, no
// per-link revocation and no stored state (owner-approved minimal design).
//
// KEY: domain-separated from the signing key (SHA-256 over a versioned label +
// the key), NEVER the JWT key used raw — a share sig must not be convertible
// into JWT-signed material.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
)

const shareSigLen = 32

func deriveShareKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("officraft.share.hmac.v1:"), secret...))
	return sum[:]
}

func shareSigFor(secret []byte, attachmentID string) string {
	mac := hmac.New(sha256.New, deriveShareKey(secret))
	mac.Write([]byte(attachmentID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:shareSigLen]
}

func verifyShareSig(secret []byte, attachmentID, sig string) bool {
	return hmac.Equal([]byte(shareSigFor(secret, attachmentID)), []byte(sig))
}

// The comparison sig covers both addresses AND both column labels; leaving the
// labels out would let a recipient relabel a column under a valid sig. Its
// label differs from the attachment label so neither sig replays as the other.
func deriveDiffKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("officraft.diff.hmac.v1:"), secret...))
	return sum[:]
}

func diffSigPayload(before, after, labelBefore, labelAfter string) string {
	return url.Values{
		"after":        {after},
		"before":       {before},
		"label_after":  {labelAfter},
		"label_before": {labelBefore},
	}.Encode()
}

func diffSigFor(secret []byte, before, after, labelBefore, labelAfter string) string {
	mac := hmac.New(sha256.New, deriveDiffKey(secret))
	mac.Write([]byte(diffSigPayload(before, after, labelBefore, labelAfter)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:shareSigLen]
}

func verifyDiffSig(secret []byte, before, after, labelBefore, labelAfter, sig string) bool {
	return hmac.Equal([]byte(diffSigFor(secret, before, after, labelBefore, labelAfter)), []byte(sig))
}

// 🔴 Both sig kinds live exactly as long as the ring key that signed them:
// removing a key kills every link made under it at once, with no grace period —
// intentionally, since a key removed as possibly leaked must not keep its
// reading authority alive through either kind of link.
func shareSigForRing(kr *keyring, attachmentID string) string {
	secret := kr.signingSecret()
	if len(secret) == 0 {
		return ""
	}
	return shareSigFor(secret, attachmentID)
}

// No early exit (here and in verifyDiffSigAnyKey): the time taken must not say
// WHICH key matched, or whether an early key matched at all.
func verifyShareSigAnyKey(kr *keyring, attachmentID, sig string) bool {
	ok := false
	for _, secret := range kr.verifySecrets() {
		if verifyShareSig(secret, attachmentID, sig) {
			ok = true
		}
	}
	return ok
}

func diffSigForRing(kr *keyring, before, after, labelBefore, labelAfter string) string {
	secret := kr.signingSecret()
	if len(secret) == 0 {
		return ""
	}
	return diffSigFor(secret, before, after, labelBefore, labelAfter)
}

func verifyDiffSigAnyKey(kr *keyring, before, after, labelBefore, labelAfter, sig string) bool {
	ok := false
	for _, secret := range kr.verifySecrets() {
		if verifyDiffSig(secret, before, after, labelBefore, labelAfter, sig) {
			ok = true
		}
	}
	return ok
}
