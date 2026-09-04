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

func TestDiffSigFor(t *testing.T) {
	secret := []byte("test-secret")
	sig := diffSigFor(secret, "att-a", "att-b", "v1", "v2")
	if len(sig) != shareSigLen {
		t.Fatalf("sig must be %d chars, got %d (%q)", shareSigLen, len(sig), sig)
	}
	if diffSigFor(secret, "att-a", "att-b", "v1", "v2") != sig {
		t.Error("the same comparison must sign the same way")
	}
	// Everything that decides what the reader sees is covered — both addresses
	// AND both labels — so a recipient cannot swap a side or relabel a column.
	for name, other := range map[string]string{
		"a different before": diffSigFor(secret, "att-x", "att-b", "v1", "v2"),
		"a different after":  diffSigFor(secret, "att-a", "att-x", "v1", "v2"),
		"the sides swapped":  diffSigFor(secret, "att-b", "att-a", "v1", "v2"),
		"a relabelled left":  diffSigFor(secret, "att-a", "att-b", "vX", "v2"),
		"a relabelled right": diffSigFor(secret, "att-a", "att-b", "v1", "vX"),
		"another secret":     diffSigFor([]byte("other-secret"), "att-a", "att-b", "v1", "v2"),
	} {
		if other == sig {
			t.Errorf("%s must not sign the same", name)
		}
	}
	// The canonical form cannot be split into a different four: a value
	// carrying the separators is escaped before it is joined.
	if diffSigFor(secret, "a&after=b", "c", "", "") == diffSigFor(secret, "a", "b&x=c", "", "") {
		t.Error("the canonical form is ambiguous — a value containing & or = can forge another")
	}
}

// The two credentials are different in kind — one blob's bytes vs one resolved
// pair — so neither key may ever produce the other's signature.
func TestDiffSigAndShareSigCannotBeReplayedForEachOther(t *testing.T) {
	secret := []byte("test-secret")
	att := "att-0123456789ab"
	if verifyDiffSig(secret, att, att, "", "", shareSigFor(secret, att)) {
		t.Error("an attachment share sig verified as a diff sig")
	}
	if verifyShareSig(secret, att, diffSigFor(secret, att, att, "", "")) {
		t.Error("a diff sig verified as an attachment share sig")
	}
}

func TestVerifyDiffSig(t *testing.T) {
	secret := []byte("test-secret")
	sig := diffSigFor(secret, "att-a", "att-b", "", "")
	if !verifyDiffSig(secret, "att-a", "att-b", "", "", sig) {
		t.Error("the minted sig must verify")
	}
	if verifyDiffSig(secret, "att-a", "att-b", "", "", "") {
		t.Error("an empty sig must never verify")
	}
	if verifyDiffSig(secret, "att-a", "att-b", "", "", sig[:len(sig)-1]+"X") {
		t.Error("a tampered sig must not verify")
	}
	if verifyDiffSig([]byte("other-secret"), "att-a", "att-b", "", "", sig) {
		t.Error("another secret must not verify")
	}
}
