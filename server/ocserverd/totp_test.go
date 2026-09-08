// Skeleton generated from server/ocserverd/totp.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestNewTOTPSecret(t *testing.T) {
	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatalf("newTOTPSecret: %v", err)
	}
	if len(secret) != 32 || strings.Contains(secret, "=") {
		t.Fatalf("secret = %q, want 32 unpadded base32 characters", secret)
	}
	raw, err := totpBase32.DecodeString(secret)
	if err != nil {
		t.Fatalf("DecodeString(new secret): %v", err)
	}
	if len(raw) != totpSecretLen {
		t.Fatalf("decoded secret length = %d, want %d", len(raw), totpSecretLen)
	}
}

func TestDecodeTOTPSecret(t *testing.T) {
	want := []byte("a test secret")
	encoded := totpBase32.EncodeToString(want)
	for _, in := range []string{encoded, strings.ToLower(encoded), encoded[:4] + " -\t" + encoded[4:]} {
		got, err := decodeTOTPSecret(in)
		if err != nil {
			t.Fatalf("decodeTOTPSecret(%q): %v", in, err)
		}
		if string(got) != string(want) {
			t.Fatalf("decodeTOTPSecret(%q) = %x, want %x", in, got, want)
		}
	}
	for _, in := range []string{"", " -\t", "not base32!"} {
		if got, err := decodeTOTPSecret(in); err == nil || got != nil {
			t.Fatalf("decodeTOTPSecret(%q) = (%x, %v), want an error and nil bytes", in, got, err)
		}
	}
}

func TestTotpCodeAt(t *testing.T) {
	key := []byte("12345678901234567890")
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for counter, expected := range want {
		if got := totpCodeAt(key, int64(counter)); got != expected {
			t.Fatalf("totpCodeAt(counter %d) = %q, want %q", counter, got, expected)
		}
	}
}

func TestTotpVerify(t *testing.T) {
	secret := totpBase32.EncodeToString([]byte("12345678901234567890"))
	now := int64(100*totpStepSecs + 7)
	current := now / totpStepSecs
	currentCode := totpCodeAt([]byte("12345678901234567890"), current)
	if gotStep, ok := totpVerify(secret, currentCode, now, 0); !ok || gotStep != current {
		t.Fatalf("totpVerify(current) = (%d, %v), want (%d, true)", gotStep, ok, current)
	}
	formatted := currentCode[:3] + " " + currentCode[3:]
	if gotStep, ok := totpVerify(secret, formatted, now, 0); !ok || gotStep != current {
		t.Fatalf("totpVerify(formatted) = (%d, %v), want current step", gotStep, ok)
	}
	for _, step := range []int64{current - totpSkewSteps, current + totpSkewSteps} {
		code := totpCodeAt([]byte("12345678901234567890"), step)
		if gotStep, ok := totpVerify(secret, code, now, 0); !ok || gotStep != step {
			t.Fatalf("totpVerify(step %d) = (%d, %v), want (%d, true)", step, gotStep, ok, step)
		}
	}
	if gotStep, ok := totpVerify(secret, "000000", now, 0); ok || gotStep != 0 {
		t.Fatalf("totpVerify(wrong code) = (%d, %v), want (0, false)", gotStep, ok)
	}
	if gotStep, ok := totpVerify(secret, currentCode, now, current); ok || gotStep != 0 {
		t.Fatalf("totpVerify(replayed step) = (%d, %v), want (0, false)", gotStep, ok)
	}
	if gotStep, ok := totpVerify(secret, currentCode, now, current-1); !ok || gotStep != current {
		t.Fatalf("totpVerify(at floor boundary) = (%d, %v), want current step", gotStep, ok)
	}
	for _, code := range []string{"", "   ", "000-000"} {
		if gotStep, ok := totpVerify(secret, code, now, 0); ok || gotStep != 0 {
			t.Fatalf("totpVerify(%q) = (%d, %v), want (0, false)", code, gotStep, ok)
		}
	}
	if gotStep, ok := totpVerify("not base32!", currentCode, now, 0); ok || gotStep != 0 {
		t.Fatalf("totpVerify(invalid secret) = (%d, %v), want (0, false)", gotStep, ok)
	}
}

func TestTotpEnrollmentURI(t *testing.T) {
	got := totpEnrollmentURI("SECRET123", "Acme:Org", "alice:admin@example.com")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", got, err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("URI scheme/host = %q/%q, want otpauth/totp", u.Scheme, u.Host)
	}
	if want := "/" + url.PathEscape("Acme Org:alice admin@example.com"); u.EscapedPath() != want {
		t.Fatalf("escaped label path = %q, want %q", u.EscapedPath(), want)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"secret":    "SECRET123",
		"issuer":    "Acme Org",
		"algorithm": "SHA1",
		"digits":    "6",
		"period":    "30",
	} {
		if got := q.Get(key); got != want {
			t.Fatalf("query %s = %q, want %q", key, got, want)
		}
	}
	if strings.Count(u.Path, ":") != 1 || strings.Contains(q.Get("issuer"), ":") {
		t.Fatalf("URI label separator handling = path %q, issuer %q; want one path separator and no issuer colon", u.Path, q.Get("issuer"))
	}
}
