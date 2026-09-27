package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// Slack's own recommended 5-minute replay window.
const slackMaxSkewSecs int64 = 5 * 60

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
