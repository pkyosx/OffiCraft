package main

// slacksig.go — Slack signed-webhook verification for the PUBLIC /in inlet
// (platform == "slack").
//
// Slack signs every request-body delivery with an HMAC-SHA256 over the exact
// string "v0:{timestamp}:{raw body}" keyed by the app's Signing Secret, sent as
// `X-Slack-Signature: v0=<hex>` alongside `X-Slack-Request-Timestamp`. We
// recompute that MAC over the SAME raw bytes we received and compare in
// constant time (crypto/hmac.Equal) — the identical constant-time discipline as
// sharesig.go, so a mismatch leaks nothing through timing.
//
// The timestamp is bounded to ±5 minutes of now to blunt replay of a captured
// (still-valid-HMAC) request. Before signature check, a Slack `url_verification`
// handshake is answered synchronously (Slack sends it once at subscription time
// to prove the endpoint echoes its challenge).

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// slackMaxSkewSecs bounds |now - X-Slack-Request-Timestamp| (Slack's own
// recommended 5-minute replay window).
const slackMaxSkewSecs int64 = 5 * 60

// verifySlackSignature reports whether xSlackSignature is a valid Slack v0
// signature for rawBody under secret at xSlackTimestamp, within the replay
// window of now (unix seconds). Any missing input, an unparseable/expired
// timestamp, or a MAC mismatch → false. Constant-time compare (hmac.Equal).
func verifySlackSignature(secret, xSlackSignature, xSlackTimestamp string, rawBody []byte, now int64) bool {
	if secret == "" || xSlackSignature == "" || xSlackTimestamp == "" {
		return false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(xSlackTimestamp), 10, 64)
	if err != nil {
		return false
	}
	skew := now - ts
	if skew < 0 {
		skew = -skew
	}
	if skew > slackMaxSkewSecs {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + xSlackTimestamp + ":"))
	mac.Write(rawBody)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(xSlackSignature))
}

// slackURLVerificationChallenge extracts the challenge string from a Slack
// url_verification handshake body ({"type":"url_verification","challenge":..}).
// Reports (challenge, true) only when the body parses and type is exactly
// "url_verification"; otherwise ("", false). Non-JSON / other event types fall
// through to the normal signature-verified delivery path.
func slackURLVerificationChallenge(rawBody []byte) (string, bool) {
	var probe struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(rawBody, &probe); err != nil {
		return "", false
	}
	if probe.Type != "url_verification" {
		return "", false
	}
	return probe.Challenge, true
}
