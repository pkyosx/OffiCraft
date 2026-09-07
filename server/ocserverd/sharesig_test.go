// Skeleton generated from server/ocserverd/sharesig.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestDeriveShareKey(t *testing.T) {
	t.Skip("TODO: deriveShareKey domain-separates the share-link HMAC key from the server signing secret (same versioned-label construction as the JWT-side deriveSecretFromPassword).")
}

func TestShareSigFor(t *testing.T) {
	t.Skip("TODO: shareSigFor computes the truncated base64url HMAC-SHA256 of one attachment id under the derived share key.")
}

func TestDeriveDiffKey(t *testing.T) {
	t.Skip("TODO: ── the comparison link's signature (T-59) ─────────────────────────────────── A comparison is a URL, not an attachment, and it comes in two flavours: the INTERNAL one (no sig, a normal logged-in reader) and the EXTERNAL one (a server-minted sig, readable with no login at all).")
}

func TestDiffSigPayload(t *testing.T) {
	t.Skip("TODO: diffSigPayload is the CANONICAL form the HMAC is taken over: all four fields, always present (an omitted label signs as empty), percent-encoded and sorted by key.")
}

func TestDiffSigFor(t *testing.T) {
	t.Skip("TODO: diffSigFor computes the truncated base64url HMAC-SHA256 over that canonical form.")
}

func TestShareSigForRing(t *testing.T) {
	t.Skip("TODO: ── multi-key share signatures (T-62) ──────────────────────────────────────── 🔴 A SHARE SIGNATURE IS A FUNCTION OF THE SIGNING KEY, so the key ring governs share links exactly as it governs tokens: new links are signed under the key that currently signs, and an existing link keeps working while the key that produced it is still IN the ring.")
}

func TestVerifyShareSigAnyKey(t *testing.T) {
	t.Skip("TODO: verifyShareSigAnyKey accepts a signature produced under ANY key still in the ring.")
}

func TestDiffSigForRing(t *testing.T) {
	t.Skip("TODO: diffSigForRing computes the signature for a NEW comparison link under the key that currently signs.")
}

func TestVerifyDiffSigAnyKey(t *testing.T) {
	t.Skip("TODO: verifyDiffSigAnyKey accepts a comparison signature produced under ANY key still in the ring.")
}
