// Skeleton generated from server/ocserverd/slacksig.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestVerifySlackSignature(t *testing.T) {
	const (
		secret = "slack-signing-secret"
		body   = `{"type":"event_callback","event":{"type":"message"}}`
		valid  = "v0=6e49a3cfd72cf460bc10c9c3284f2ab9090f590d4a55e559e868bd21756459a4"
	)
	now := int64(1700000000)
	for _, tc := range []struct {
		name      string
		secret    string
		signature string
		timestamp string
		body      []byte
		now       int64
		want      bool
	}{
		{name: "the exact body and current timestamp verify", secret: secret, signature: valid, timestamp: "1700000000", body: []byte(body), now: now, want: true},
		{name: "the replay window includes exactly five minutes", secret: secret, signature: "v0=ad57873921e6b7a5f7d860692010c4eb27e2daf886d6d9defd8ca842a767cd73", timestamp: "1700000300", body: []byte(body), now: now, want: true},
		{name: "a timestamp beyond the replay window is refused", secret: secret, signature: "v0=23f75b7fad004b61962399e0feb6f092539c5404df2469642c2f8ce137f010a2", timestamp: "1700000400", body: []byte(body), now: now, want: false},
		{name: "an old timestamp is refused even with its matching MAC", secret: secret, signature: "v0=93451556571a8a104e8f4ad90697512c2e009417c33c4bd46f39f596e4529794", timestamp: "1699990000", body: []byte(body), now: now, want: false},
		{name: "changing one body byte invalidates the signature", secret: secret, signature: valid, timestamp: "1700000000", body: []byte(body + "x"), now: now, want: false},
		{name: "changing one signature byte is refused", secret: secret, signature: valid[:len(valid)-1] + "0", timestamp: "1700000000", body: []byte(body), now: now, want: false},
		{name: "the same signature does not verify under another secret", secret: "other-secret", signature: valid, timestamp: "1700000000", body: []byte(body), now: now, want: false},
		{name: "an empty secret is refused", secret: "", signature: valid, timestamp: "1700000000", body: []byte(body), now: now, want: false},
		{name: "an empty signature is refused", secret: secret, signature: "", timestamp: "1700000000", body: []byte(body), now: now, want: false},
		{name: "an empty timestamp is refused", secret: secret, signature: valid, timestamp: "", body: []byte(body), now: now, want: false},
		{name: "a non-numeric timestamp is refused", secret: secret, signature: valid, timestamp: "not-a-number", body: []byte(body), now: now, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifySlackSignature(tc.secret, tc.signature, tc.timestamp, tc.body, tc.now); got != tc.want {
				t.Fatalf("verifySlackSignature(%q, %q, %q, %q, %d) = %v, want %v", tc.secret, tc.signature, tc.timestamp, tc.body, tc.now, got, tc.want)
			}
		})
	}
}

func TestSlackURLVerificationChallenge(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		want      string
		wantFound bool
	}{
		{name: "a url verification body returns its challenge", body: `{"type":"url_verification","challenge":"abc123"}`, want: "abc123", wantFound: true},
		{name: "an empty challenge is still the parsed handshake shape", body: `{"type":"url_verification","challenge":""}`, want: "", wantFound: true},
		{name: "a normal event body is not a handshake", body: `{"type":"event_callback"}`, want: "", wantFound: false},
		{name: "malformed JSON is not a handshake", body: "not-json", want: "", wantFound: false},
		{name: "a non-string challenge is not accepted as a handshake", body: `{"type":"url_verification","challenge":42}`, want: "", wantFound: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, found := slackURLVerificationChallenge([]byte(tc.body))
			if got != tc.want || found != tc.wantFound {
				t.Fatalf("slackURLVerificationChallenge(%q) = (%q, %v), want (%q, %v)", tc.body, got, found, tc.want, tc.wantFound)
			}
		})
	}
}
