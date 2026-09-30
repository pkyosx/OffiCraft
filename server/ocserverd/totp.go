package main

// Hand-rolled on the standard library ON PURPOSE (root AGENTS.md, Lazy ladder
// rung 5/6).
//
// HMAC-SHA1, 6 digits, 30-second step is an interop constraint, not a choice:
// mainstream authenticators default to exactly that triple and several ignore
// the otpauth:// `algorithm`/`digits` parameters, so anything else yields a QR
// they accept and then compute the WRONG code from.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
)

const (
	totpStepSecs int64 = 30

	totpDigits = 6
	// RFC 4226 §4 recommendation.
	totpSecretLen = 20
	// Widening multiplies the codes valid at any instant (a brute-force gain);
	// 0 rejects a phone whose clock is 3 seconds off.
	totpSkewSteps int64 = 1
)

// Unpadded: authenticator apps choke on '=' padding.
var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func newTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return totpBase32.EncodeToString(raw), nil
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	cleaned := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(secret))
	raw, err := totpBase32.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("totp secret is not valid base32: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("totp secret is empty")
	}
	return raw, nil
}

func totpCodeAt(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	// RFC 4226 §5.4 dynamic truncation.
	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, truncated%mod)
}

func normalizeTOTPCode(code string) string {
	return strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(code))
}

// 🔴 The returned step is the replay defence and the caller MUST persist it as
// the next `minStep`; without that floor one code replays for ~90 seconds.
//
// The scan never breaks early, so the time taken does not depend on which step
// matched, nor on whether any did.
func totpVerify(secret, code string, now, minStep int64) (int64, bool) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return 0, false
	}
	want := normalizeTOTPCode(code)
	if want == "" {
		return 0, false
	}

	current := now / totpStepSecs
	matched := int64(0)
	found := false
	for step := current - totpSkewSteps; step <= current+totpSkewSteps; step++ {
		if step <= minStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCodeAt(key, step)), []byte(want)) == 1 && !found {
			matched = step
			found = true
		}
	}
	return matched, found
}

// The colon is the label's own `issuer:account` separator (Google's Key URI
// Format), so a colon inside an owner-supplied name would re-split the label.
func totpEnrollmentURI(secret, issuer, account string) string {
	issuer = strings.ReplaceAll(issuer, ":", " ")
	account = strings.ReplaceAll(account, ":", " ")
	// PathEscape, not QueryEscape: a space in the label must become %20, not '+'.
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", totpStepSecs))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
