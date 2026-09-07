// Skeleton generated from server/ocserverd/keyring.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestSingleKeyring(t *testing.T) {
	t.Skip("TODO: singleKeyring is the one-key ring: the shape every install has until it rotates for the first time, and the shape a test wanting \"the old single secret\" asks for.")
}

func TestNewKeyID(t *testing.T) {
	t.Skip("TODO: newKeyID draws a fresh random key id.")
}

func TestNewSigningKeyBytes(t *testing.T) {
	t.Skip("TODO: newSigningKeyBytes mints a fresh random signing key.")
}

func TestSigningSecret(t *testing.T) {
	t.Skip("TODO: signingSecret returns the key that signs, or nil when the ring is empty.")
}

func TestVerifyCandidates(t *testing.T) {
	t.Skip("TODO: verifyCandidates returns every key that may verify a token, the SIGNING key first (the common case costs one HMAC).")
}

func TestVerifySecrets(t *testing.T) {
	t.Skip("TODO: verifySecrets is the bytes-only projection of verifyCandidates, kept because sharesig.go and several tests want the keys and have no use for the ids.")
}

func TestActiveKeyID(t *testing.T) {
	t.Skip("TODO: activeKeyID names the key that SIGNS right now, \"\" when the ring is empty.")
}

func TestLoadKeyring(t *testing.T) {
	t.Skip("TODO: loadKeyring reads the ring from the settings store, falling back to the single pre-ring key when this install has never rotated.")
}

func TestPersist(t *testing.T) {
	t.Skip("TODO: persist writes the ring to the settings store.")
}

func TestPersistRing(t *testing.T) {
	t.Skip("TODO: persistRing writes an EXPLICIT key set, taking no lock of its own — so a caller holding the write lock can persist the state it is about to install without releasing it.")
}

func TestRotate(t *testing.T) {
	t.Skip("TODO: ── the two operator actions ───────────────────────────────────────────────── rotate mints a new key, appends it and makes it the signing key.")
}

func TestRemove(t *testing.T) {
	t.Skip("TODO: remove drops one retired key.")
}
