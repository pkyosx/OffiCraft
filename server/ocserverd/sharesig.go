package main

// sharesig.go — file-level share-link signatures (the ?sig= credential on
// GET /api/chat/attachment/{attachment_id}).
//
// Design (owner-approved minimal version): a share link is the attachment's
// serve URL carrying an HMAC-SHA256 over EXACTLY that attachment id. No expiry,
// no per-link revocation, no stored state of its own — it grants nothing beyond
// reading the one blob it names (any other id fails the HMAC; any other route
// never consults sigs at all).
//
// It is NOT permanent. That word was true until T-62 and is the first thing a
// reader of this file meets, so it is corrected here rather than only in the
// block below: a sig lives exactly as long as the key that signed it stays in
// the signing-key ring, and every sig made under a key dies the moment an owner
// removes it. See the multi-key section at the bottom.
//
// KEY: derived from a server signing key via domain separation (SHA-256 over a
// versioned label + the key), NEVER the JWT key used raw — a share sig must not
// be confusable with, or convertible into, any JWT-signed material, and the
// derivation needs no key of its own (matching deriveSecretFromPassword's
// pattern in jwt.go).
//
// ⚠️ "the key is stable exactly as long as the signing secret is" used to end
// that sentence, back when there WAS one signing secret and it never changed.
// Since T-62 there is a RING (keyring.go): a new sig is made under the key that
// currently signs, an existing sig verifies against every key still in the
// ring, and removing a key ends every sig made under it at that instant. So a
// share link is no longer permanent-by-construction — it is exactly as durable
// as the key behind it, which is now something a person can end on purpose.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// shareSigLen truncates the base64url HMAC output: 32 chars = 192 bits,
// far beyond brute force while keeping the URL short.
const shareSigLen = 32

// deriveShareKey domain-separates the share-link HMAC key from the server
// signing secret (same versioned-label construction as the JWT-side
// deriveSecretFromPassword).
func deriveShareKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("officraft.share.hmac.v1:"), secret...))
	return sum[:]
}

// shareSigFor computes the truncated base64url HMAC-SHA256 of one attachment
// id under the derived share key.
func shareSigFor(secret []byte, attachmentID string) string {
	mac := hmac.New(sha256.New, deriveShareKey(secret))
	mac.Write([]byte(attachmentID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:shareSigLen]
}

// verifyShareSig reports whether sig authorizes reading attachmentID —
// constant-time compare, deny on anything else (empty inputs included:
// an empty id/secret still yields a real HMAC the caller cannot guess).
func verifyShareSig(secret []byte, attachmentID, sig string) bool {
	return hmac.Equal([]byte(shareSigFor(secret, attachmentID)), []byte(sig))
}

// ── multi-key share signatures (T-62) ────────────────────────────────────────
//
// 🔴 A SHARE SIGNATURE IS A FUNCTION OF THE SIGNING KEY, so the key ring
// governs share links exactly as it governs tokens: new links are signed under
// the key that currently signs, and an existing link keeps working while the
// key that produced it is still IN the ring. Removing that key invalidates
// every link made under it, with no grace period and no notice to whoever holds
// the link — the same act, and the same instant, as the tokens it revokes.
// That coupling is intentional: a key being removed because it may have leaked
// must not leave the file-reading half of its authority alive.

// shareSigForRing computes the signature for a NEW link under the key that
// currently signs. Returns "" when the ring has no signing key.
func shareSigForRing(kr *keyring, attachmentID string) string {
	secret := kr.signingSecret()
	if len(secret) == 0 {
		return ""
	}
	return shareSigFor(secret, attachmentID)
}

// verifyShareSigAnyKey accepts a signature produced under ANY key still in the
// ring. Constant-time per key (verifyShareSig), deny when the ring is empty.
func verifyShareSigAnyKey(kr *keyring, attachmentID, sig string) bool {
	ok := false
	for _, secret := range kr.verifySecrets() {
		// No early exit: every key is compared so the time taken does not say
		// WHICH key matched, or whether an early key matched at all.
		if verifyShareSig(secret, attachmentID, sig) {
			ok = true
		}
	}
	return ok
}
