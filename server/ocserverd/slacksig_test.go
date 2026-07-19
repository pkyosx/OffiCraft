package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// slackSign is the test-side oracle: it constructs exactly what Slack sends
// (v0={hex of HMAC over "v0:{ts}:{body}"}) so the round-trip pins the server's
// recomputation against an independent implementation.
func slackSign(secret, ts, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySlackSignature(t *testing.T) {
	secret := "slack-signing-secret"
	body := `{"type":"event_callback","event":{"type":"message"}}`
	now := int64(1_700_000_000)
	ts := "1700000000"
	sig := slackSign(secret, ts, body)

	if !verifySlackSignature(secret, sig, ts, []byte(body), now) {
		t.Fatal("a freshly minted Slack signature must verify")
	}
	// Tampered body → different MAC → reject.
	if verifySlackSignature(secret, sig, ts, []byte(body+"x"), now) {
		t.Fatal("a body tamper must not verify")
	}
	// Tampered signature → reject.
	if verifySlackSignature(secret, sig[:len(sig)-1]+"0", ts, []byte(body), now) {
		t.Fatal("a tampered signature must not verify")
	}
	// Wrong secret → reject.
	if verifySlackSignature("other-secret", sig, ts, []byte(body), now) {
		t.Fatal("a signature must not verify under another secret")
	}
	// Expired timestamp (> 5 min skew) → reject even with a valid MAC for that ts.
	oldTs := "1699990000" // 10000s < now → beyond the 300s window
	oldSig := slackSign(secret, oldTs, body)
	if verifySlackSignature(secret, oldSig, oldTs, []byte(body), now) {
		t.Fatal("an expired timestamp must not verify")
	}
	// Future timestamp beyond the window → reject.
	futTs := "1700000400" // now+400 > 300
	futSig := slackSign(secret, futTs, body)
	if verifySlackSignature(secret, futSig, futTs, []byte(body), now) {
		t.Fatal("a far-future timestamp must not verify")
	}
	// Empty inputs → reject (never a valid guess).
	if verifySlackSignature("", sig, ts, []byte(body), now) {
		t.Fatal("empty secret must not verify")
	}
	if verifySlackSignature(secret, "", ts, []byte(body), now) {
		t.Fatal("empty signature must not verify")
	}
	if verifySlackSignature(secret, sig, "", []byte(body), now) {
		t.Fatal("empty timestamp must not verify")
	}
	// Unparseable timestamp → reject.
	if verifySlackSignature(secret, sig, "not-a-number", []byte(body), now) {
		t.Fatal("a non-numeric timestamp must not verify")
	}
}

func TestSlackURLVerificationChallenge(t *testing.T) {
	challenge, ok := slackURLVerificationChallenge(
		[]byte(`{"type":"url_verification","challenge":"abc123"}`))
	if !ok || challenge != "abc123" {
		t.Fatalf("url_verification must yield its challenge, got (%q,%v)", challenge, ok)
	}
	// A normal event body is NOT a handshake.
	if _, ok := slackURLVerificationChallenge([]byte(`{"type":"event_callback"}`)); ok {
		t.Fatal("a non-url_verification body must not be treated as a handshake")
	}
	// Non-JSON body → not a handshake.
	if _, ok := slackURLVerificationChallenge([]byte("PR #42 merged")); ok {
		t.Fatal("a non-JSON body must not be treated as a handshake")
	}
}
