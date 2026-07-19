package main

import (
	"strings"
	"testing"
)

func TestDeriveShareKey(t *testing.T) {
	secret := []byte("test-secret")
	key := deriveShareKey(secret)
	if len(key) != 32 {
		t.Fatalf("share key must be 32 bytes, got %d", len(key))
	}
	if string(key) == string(secret) {
		t.Fatal("share key must differ from the raw signing secret")
	}
	if string(deriveShareKey(secret)) != string(key) {
		t.Fatal("derivation must be deterministic")
	}
	if string(deriveShareKey([]byte("other-secret"))) == string(key) {
		t.Fatal("different secrets must derive different share keys")
	}
	if string(deriveShareKey(deriveSecretFromPassword("pw"))) == string(deriveSecretFromPassword("pw")) {
		t.Fatal("share key must not collide with the JWT-side derivation")
	}
}

func TestShareSigFor(t *testing.T) {
	secret := []byte("test-secret")
	sig := shareSigFor(secret, "att-abc123")
	if len(sig) != shareSigLen {
		t.Fatalf("sig must be %d chars, got %d (%q)", shareSigLen, len(sig), sig)
	}
	if strings.ContainsAny(sig, "+/=") {
		t.Fatalf("sig must be unpadded base64url, got %q", sig)
	}
	if shareSigFor(secret, "att-abc123") != sig {
		t.Fatal("sig must be deterministic")
	}
	if shareSigFor(secret, "att-other") == sig {
		t.Fatal("different attachment ids must sign differently")
	}
	if shareSigFor([]byte("other-secret"), "att-abc123") == sig {
		t.Fatal("different secrets must sign differently")
	}
}

func TestVerifyShareSig(t *testing.T) {
	secret := []byte("test-secret")
	sig := shareSigFor(secret, "att-abc123")
	if !verifyShareSig(secret, "att-abc123", sig) {
		t.Fatal("a freshly minted sig must verify")
	}
	if verifyShareSig(secret, "att-other", sig) {
		t.Fatal("a sig must not verify for another attachment id")
	}
	if verifyShareSig(secret, "att-abc123", sig[:len(sig)-1]+"X") {
		t.Fatal("a tampered sig must not verify")
	}
	if verifyShareSig(secret, "att-abc123", "") {
		t.Fatal("an empty sig must not verify")
	}
	if verifyShareSig([]byte("other-secret"), "att-abc123", sig) {
		t.Fatal("a sig must not verify under another secret")
	}
}
