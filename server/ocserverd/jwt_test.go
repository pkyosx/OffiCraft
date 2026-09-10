package main

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestB64uDecode(t *testing.T) {
	t.Run("decodes an unpadded base64url segment", func(t *testing.T) {
		got, err := b64uDecode("SGVsbG8")
		if err != nil {
			t.Fatalf("b64uDecode: %v", err)
		}
		if string(got) != "Hello" {
			t.Fatalf("decoded = %q, want Hello", got)
		}
	})

	t.Run("rejects malformed base64url and wraps the invalid-token error", func(t *testing.T) {
		got, err := b64uDecode("!")
		if got != nil {
			t.Fatalf("decoded = %q, want nil", got)
		}
		if err == nil || !errors.Is(err, errInvalidToken) {
			t.Fatalf("err = %v, want an invalid-token error", err)
		}
	})
}

func TestHs256Sign(t *testing.T) {
	got := hs256Sign("The quick brown fox jumps over the lazy dog", []byte("key"))
	want, err := hex.DecodeString("f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8")
	if err != nil {
		t.Fatalf("decode expected digest: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HMAC = %x, want %x", got, want)
	}
}

func TestMintJWT(t *testing.T) {
	t.Run("mints a verifiable expiring token with an optional machine claim", func(t *testing.T) {
		got, err := mintJWT("agent-1", "agent", 600, []byte("secret"), 1700000000, "m-box")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
		wantToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJhZ2VudC0xIiwic2NvcGUiOiJhZ2VudCIsImlhdCI6MTcwMDAwMDAwMCwiZXhwIjoxNzAwMDAwNjAwLCJtYWNoaW5lX2lkIjoibS1ib3gifQ.m7FLXHUemflTv3ZU3jmVIXD2t0iwvG4gSnyKMe3QBow"
		if got != wantToken {
			t.Fatalf("token = %q, want %q", got, wantToken)
		}
		claims, err := verifyJWT(got, []byte("secret"), 1700000000)
		if err != nil {
			t.Fatalf("verify minted token: %v", err)
		}
		want := map[string]any{
			"sub":        "agent-1",
			"scope":      "agent",
			"iat":        float64(1700000000),
			"exp":        float64(1700000600),
			"machine_id": "m-box",
		}
		if !reflect.DeepEqual(claims, want) {
			t.Fatalf("claims = %#v, want %#v", claims, want)
		}
	})

	t.Run("omits an empty machine claim", func(t *testing.T) {
		token, err := mintJWT("agent-1", "agent", 600, []byte("secret"), 1700000000, "")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
		claims, err := verifyJWT(token, []byte("secret"), 1700000000)
		if err != nil {
			t.Fatalf("verify minted token: %v", err)
		}
		if _, ok := claims["machine_id"]; ok {
			t.Fatalf("claims unexpectedly carry machine_id: %v", claims)
		}
	})

	t.Run("refuses an empty identity", func(t *testing.T) {
		if _, err := mintJWT("", "agent", 600, []byte("secret"), 1700000000, ""); err == nil {
			t.Fatal("empty sub must be refused")
		}
	})
}

func TestMintJWTWithoutExpiry(t *testing.T) {
	token, err := mintJWTWithoutExpiry("m-box", "agent", []byte("secret"), 1700000000, "")
	if err != nil {
		t.Fatalf("mintJWTWithoutExpiry: %v", err)
	}
	claims, err := verifyJWT(token, []byte("secret"), 9999999999)
	if err != nil {
		t.Fatalf("verify permanent token: %v", err)
	}
	want := map[string]any{
		"sub":   "m-box",
		"scope": "agent",
		"iat":   float64(1700000000),
	}
	if !reflect.DeepEqual(claims, want) {
		t.Fatalf("claims = %#v, want %#v", claims, want)
	}
}

func TestMintJWTClaims(t *testing.T) {
	exp := int64(1700000600)
	token, err := mintJWTClaims(jwtClaims{
		Sub: "agent-1", Scope: "agent", Iat: 1700000000, Exp: &exp, MachineID: "m-box",
	}, []byte("secret"))
	if err != nil {
		t.Fatalf("mintJWTClaims: %v", err)
	}
	claims, err := verifyJWT(token, []byte("secret"), 1700000000)
	if err != nil {
		t.Fatalf("verify claims token: %v", err)
	}
	if claims["sub"] != "agent-1" || claims["scope"] != "agent" ||
		claims["iat"] != float64(1700000000) || claims["exp"] != float64(1700000600) ||
		claims["machine_id"] != "m-box" {
		t.Fatalf("claims = %#v, want all supplied claims", claims)
	}
	if _, err := mintJWTClaims(jwtClaims{Sub: "agent-1"}, nil); !errors.Is(err, errNoSigningKey) {
		t.Fatalf("empty signing key err = %v, want errNoSigningKey", err)
	}
}

func TestVerifyJWT(t *testing.T) {
	t.Run("accepts a valid token and returns its claims", func(t *testing.T) {
		token, err := mintJWT("agent-1", "agent", 600, []byte("secret"), 1700000000, "")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
		claims, err := verifyJWT(token, []byte("secret"), 1700000000)
		if err != nil || claims["sub"] != "agent-1" {
			t.Fatalf("verifyJWT = %#v, %v", claims, err)
		}
	})

	t.Run("rejects a token after its expiry", func(t *testing.T) {
		token, err := mintJWT("agent-1", "agent", 1, []byte("secret"), 1700000000, "")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
		claims, err := verifyJWT(token, []byte("secret"), 1700000001)
		if claims != nil || !errors.Is(err, errExpiredToken) {
			t.Fatalf("expired verification = %#v, %v; want nil and errExpiredToken", claims, err)
		}
	})

	t.Run("rejects malformed and incorrectly signed tokens", func(t *testing.T) {
		for _, token := range []string{"not-a-jwt", "a.b.c"} {
			if claims, err := verifyJWT(token, []byte("secret"), 1700000000); claims != nil || err == nil {
				t.Fatalf("verifyJWT(%q) = %#v, %v; want an error", token, claims, err)
			}
		}
		valid, err := mintJWT("agent-1", "agent", 600, []byte("secret"), 1700000000, "")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
		if claims, err := verifyJWT(valid, []byte("wrong"), 1700000000); claims != nil || !errors.Is(err, errInvalidToken) {
			t.Fatalf("wrong-secret verification = %#v, %v; want invalid-token error", claims, err)
		}
	})

	t.Run("rejects a signed payload without an identity", func(t *testing.T) {
		exp := int64(1700000600)
		token, err := mintJWTClaims(jwtClaims{Scope: "agent", Iat: 1700000000, Exp: &exp}, []byte("secret"))
		if err != nil {
			t.Fatalf("mintJWTClaims: %v", err)
		}
		if claims, err := verifyJWT(token, []byte("secret"), 1700000000); claims != nil || err == nil {
			t.Fatalf("missing-sub verification = %#v, %v; want an error", claims, err)
		}
	})
}

func TestDeriveSecretFromPassword(t *testing.T) {
	first := deriveSecretFromPassword("correct horse battery staple")
	second := deriveSecretFromPassword("correct horse battery staple")
	other := deriveSecretFromPassword("different password")
	if !reflect.DeepEqual(first, second) {
		t.Fatal("the same password must derive the same secret")
	}
	if reflect.DeepEqual(first, other) {
		t.Fatal("different passwords must derive different secrets")
	}
	if len(first) != 32 {
		t.Fatalf("derived secret length = %d, want 32", len(first))
	}
	if got := hex.EncodeToString(first); got != "68888ad912b4ee8bb851cec06cde2f12220ac705e03dd2fb274d54dfe0d26ed8" {
		t.Fatalf("derived secret = %s, want the domain-separated SHA-256 digest", got)
	}
}

func TestVerifyJWTAnyKey(t *testing.T) {
	kr := newKeyring([]signingKey{
		{ID: "k-old", Key: []byte("old-secret")},
		{ID: "k-new", Key: []byte("new-secret")},
	}, "k-new")
	oldToken, err := mintJWT("agent-1", "agent", 600, []byte("old-secret"), 1700000000, "")
	if err != nil {
		t.Fatalf("mint old token: %v", err)
	}
	claims, keyID, err := verifyJWTAnyKey(kr, oldToken, 1700000000)
	if err != nil || keyID != "k-old" || claims["sub"] != "agent-1" {
		t.Fatalf("old-key verification = %#v, %q, %v", claims, keyID, err)
	}
	newToken, err := mintJWT("agent-1", "agent", 600, []byte("new-secret"), 1700000000, "")
	if err != nil {
		t.Fatalf("mint new token: %v", err)
	}
	if _, keyID, err := verifyJWTAnyKey(kr, newToken, 1700000000); err != nil || keyID != "k-new" {
		t.Fatalf("new-key verification = %q, %v; want k-new", keyID, err)
	}
	if claims, keyID, err := verifyJWTAnyKey(kr, "not-a-jwt", 1700000000); claims != nil || keyID != "" || err == nil {
		t.Fatalf("invalid verification = %#v, %q, %v; want empty identity and error", claims, keyID, err)
	}
	if claims, keyID, err := verifyJWTAnyKey(newKeyring(nil, ""), newToken, 1700000000); claims != nil || keyID != "" || err == nil {
		t.Fatalf("empty-ring verification = %#v, %q, %v; want empty identity and error", claims, keyID, err)
	}
}
