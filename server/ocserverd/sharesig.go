package main

// Signed links: an HMAC over exactly what they grant, with no stored state.
// Two kinds are minted today: the cockpit's one-hour preview link (attachment id
// + exp). The other two — an attachment sig and a comparison sig with no exp —
// are only verified, for links handed out before share links were stored
// (api_share_links.go); legacyShareSigsOpen decides whether they still open.
//
// KEY: domain-separated from the signing key (SHA-256 over a versioned label +
// the key), NEVER the JWT key used raw — a share sig must not be convertible
// into JWT-signed material.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
)

const shareSigLen = 32

// 🔴 THE ONE SWITCH for pre-stored-share-link `?sig=` URLs (attachment and
// comparison, no `exp`): true keeps every one of them opening for as long as
// its signing key stays in the ring. The owner has not ruled yet whether they
// should die with the upgrade; flipping this is the whole change.
const legacyShareSigsOpen = true

const (
	previewLinkTTLSecs = 60 * 60
	previewExpParam    = "exp"
)

func deriveShareKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("officraft.share.hmac.v1:"), secret...))
	return sum[:]
}

func shareSigFor(secret []byte, attachmentID string) string {
	mac := hmac.New(sha256.New, deriveShareKey(secret))
	mac.Write([]byte(attachmentID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:shareSigLen]
}

func verifyShareSig(secret []byte, attachmentID, sig string) bool {
	return hmac.Equal([]byte(shareSigFor(secret, attachmentID)), []byte(sig))
}

// The comparison sig covers both addresses AND both column labels; leaving the
// labels out would let a recipient relabel a column under a valid sig. Its
// label differs from the attachment label so neither sig replays as the other.
func deriveDiffKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("officraft.diff.hmac.v1:"), secret...))
	return sum[:]
}

func diffSigPayload(before, after, labelBefore, labelAfter string) string {
	return url.Values{
		"after":        {after},
		"before":       {before},
		"label_after":  {labelAfter},
		"label_before": {labelBefore},
	}.Encode()
}

func diffSigFor(secret []byte, before, after, labelBefore, labelAfter string) string {
	mac := hmac.New(sha256.New, deriveDiffKey(secret))
	mac.Write([]byte(diffSigPayload(before, after, labelBefore, labelAfter)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:shareSigLen]
}

func verifyDiffSig(secret []byte, before, after, labelBefore, labelAfter, sig string) bool {
	return hmac.Equal([]byte(diffSigFor(secret, before, after, labelBefore, labelAfter)), []byte(sig))
}

func derivePreviewKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("officraft.preview.hmac.v1:"), secret...))
	return sum[:]
}

func previewSigFor(secret []byte, attachmentID string, exp int64) string {
	mac := hmac.New(sha256.New, derivePreviewKey(secret))
	mac.Write([]byte(url.Values{"attachment_id": {attachmentID}, "exp": {strconv.FormatInt(exp, 10)}}.Encode()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:shareSigLen]
}

// 🔴 Every sig kind lives at most as long as the ring key that signed it:
// removing a key kills every link made under it at once, with no grace period —
// intentionally, since a key removed as possibly leaked must not keep its
// reading authority alive through any kind of link.
func previewSigForRing(kr *keyring, attachmentID string, exp int64) string {
	secret := kr.signingSecret()
	if len(secret) == 0 {
		return ""
	}
	return previewSigFor(secret, attachmentID, exp)
}

func verifyPreviewSigAnyKey(kr *keyring, attachmentID string, exp int64, sig string) bool {
	ok := false
	for _, secret := range kr.verifySecrets() {
		if hmac.Equal([]byte(previewSigFor(secret, attachmentID, exp)), []byte(sig)) {
			ok = true
		}
	}
	return ok
}

// No early exit (here and in verifyDiffSigAnyKey): the time taken must not say
// WHICH key matched, or whether an early key matched at all.
func verifyShareSigAnyKey(kr *keyring, attachmentID, sig string) bool {
	ok := false
	for _, secret := range kr.verifySecrets() {
		if verifyShareSig(secret, attachmentID, sig) {
			ok = true
		}
	}
	return ok
}

func verifyDiffSigAnyKey(kr *keyring, before, after, labelBefore, labelAfter, sig string) bool {
	ok := false
	for _, secret := range kr.verifySecrets() {
		if verifyDiffSig(secret, before, after, labelBefore, labelAfter, sig) {
			ok = true
		}
	}
	return ok
}
