package main

// jwt.go — HS256 JWT mint/verify, the byte-level Go twin of
// the retired Python plumbing/auth.py. One self-describing, stateless identity token for
// every gated surface; the signing keys live in the DB settings store as a RING
// (keyring.go) — many keys verify, exactly one signs.
//
// INTEROP CONTRACT (locked by jwt_test.go): given the same inputs, mintJWT
// produces the IDENTICAL compact token the Python `plumbing.auth.mint`
// produces — same header ({"alg":"HS256","typ":"JWT"}), same claim ORDER
// (sub, scope, iat, exp[, machine_id]), same compact JSON (no spaces), same
// unpadded base64url — so a token minted by either daemon verifies on the
// other under the shared secret. Warden credentials are the sole exception:
// their server-only mint path intentionally omits exp, so a deleted machine's
// roster row — not a timer — is their revocation seam.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors mirroring plumbing.auth's JwtError hierarchy. errExpiredToken
// wraps errInvalidToken so a single errors.Is(err, errInvalidToken) at the gate
// still catches it (the ExpiredToken(InvalidToken) subclassing on the Python side).
var (
	errInvalidToken = errors.New("invalid token")
	errExpiredToken = fmt.Errorf("%w: expired", errInvalidToken)
)

// jwtHeaderSeg is the constant encoded header: base64url of the exact bytes
// Python emits for {"alg":"HS256","typ":"JWT"} (compact, this key order).
const jwtHeaderJSON = `{"alg":"HS256","typ":"JWT"}`

// jwtClaims is the claim envelope; the struct FIELD ORDER is load-bearing — it
// reproduces Python's dict insertion order so the payload segment is
// byte-identical (see the interop contract above).
type jwtClaims struct {
	Sub       string `json:"sub"`
	Scope     string `json:"scope"`
	Iat       int64  `json:"iat"`
	Exp       *int64 `json:"exp,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
}

func b64uEncode(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func b64uDecode(seg string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return nil, fmt.Errorf("%w: bad base64url segment: %v", errInvalidToken, err)
	}
	return raw, nil
}

func hs256Sign(signingInput string, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}

// mintJWT mints an HS256 JWT for identity sub with scope and a ttl (seconds);
// exp = now + ttl. machineID is the optional machine binding claim (empty =
// omitted, mirroring the Python `if machine_id:` guard). `now` is explicit
// (unix seconds) — callers pass time.Now().Unix(); tests pin it.
func mintJWT(sub, scope string, ttl int64, secret []byte, now int64, machineID string) (string, error) {
	if sub == "" {
		return "", fmt.Errorf("%w: mint requires a non-empty sub (identity id)", errInvalidToken)
	}
	exp := now + ttl
	return mintJWTClaims(jwtClaims{Sub: sub, Scope: scope, Iat: now, Exp: &exp, MachineID: machineID}, secret)
}

// mintJWTWithoutExpiry mints a signed JWT with no exp claim. It is deliberately
// separate from mintJWT so ordinary callers must opt into a permanent token
// explicitly; its only production caller is mintWardenToken.
func mintJWTWithoutExpiry(sub, scope string, secret []byte, now int64, machineID string) (string, error) {
	if sub == "" {
		return "", fmt.Errorf("%w: mint requires a non-empty sub (identity id)", errInvalidToken)
	}
	return mintJWTClaims(jwtClaims{Sub: sub, Scope: scope, Iat: now, MachineID: machineID}, secret)
}

func mintJWTClaims(claims jwtClaims, secret []byte) (string, error) {
	// 🔴 THE EMPTY-KEY REFUSAL LIVES HERE, at the one seam every mint passes
	// through, and not at each caller. It was previously declared in keyring.go
	// (errNoSigningKey), documented as "a state the server must refuse to mint
	// in rather than silently sign with something else" — and wired to nothing;
	// five callers open-coded the check and two did not have it at all, one of
	// them the WARDEN path, whose credentials carry no exp. An empty key is a
	// perfectly valid HMAC key, so without this the server would have handed out
	// a permanent credential signed under nothing and said 200. (Found by
	// independent review; not reachable today because requireAuth refuses
	// everything while the ring is empty, which is precisely why it could sit
	// there unnoticed.)
	if len(secret) == 0 {
		return "", errNoSigningKey
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("%w: marshal claims: %v", errInvalidToken, err)
	}
	headerSeg := b64uEncode([]byte(jwtHeaderJSON))
	payloadSeg := b64uEncode(payload)
	signingInput := headerSeg + "." + payloadSeg
	sigSeg := b64uEncode(hs256Sign(signingInput, secret))
	return signingInput + "." + sigSeg, nil
}

// verifyJWT verifies an HS256 JWT and returns its claims, or an error.
//
// Checks, in Python-contract order: structural shape (3 dot-segments), the
// HS256 header alg (refusing an alg:none downgrade), a CONSTANT-TIME signature
// compare (hmac.Equal), an exp that is not in the past when present
// (errExpiredToken), and a non-empty sub. Missing exp is reserved for the
// server's warden-only mint path.
func verifyJWT(token string, secret []byte, now int64) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: token must be a JWT (header.payload.signature)", errInvalidToken)
	}
	headerSeg, payloadSeg, sigSeg := parts[0], parts[1], parts[2]

	headerRaw, err := b64uDecode(headerSeg)
	if err != nil {
		return nil, err
	}
	var header map[string]any
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return nil, fmt.Errorf("%w: bad header: %v", errInvalidToken, err)
	}
	if alg, _ := header["alg"].(string); alg != "HS256" {
		return nil, fmt.Errorf("%w: unsupported alg: %v", errInvalidToken, header["alg"])
	}

	expected := hs256Sign(headerSeg+"."+payloadSeg, secret)
	actual, err := b64uDecode(sigSeg)
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(expected, actual) {
		return nil, fmt.Errorf("%w: signature verification failed", errInvalidToken)
	}

	payloadRaw, err := b64uDecode(payloadSeg)
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return nil, fmt.Errorf("%w: bad payload: %v", errInvalidToken, err)
	}

	if rawExp, present := claims["exp"]; present {
		exp, ok := rawExp.(float64) // encoding/json numbers land as float64
		if !ok {
			return nil, fmt.Errorf("%w: token has no numeric exp", errInvalidToken)
		}
		if float64(now) >= exp {
			return nil, errExpiredToken
		}
	}
	if sub, _ := claims["sub"].(string); sub == "" {
		return nil, fmt.Errorf("%w: token has no sub (identity id)", errInvalidToken)
	}
	return claims, nil
}

// ── Secret derivation ────────────────────────────────────────────────────────
//
// The signing keys live in the DB settings store as a ring (keyring.go:
// loadKeyring). The pre-ring row settings.go:loadAuthSettings resolves —
// migrated in for existing installs, minted for fresh ones — is adopted as that
// ring's FIRST key rather than replaced. The old resolveSecret ladder and its
// var/jwt_secret fallback file are retired.

// deriveSecretFromPassword is a domain-separated SHA-256 of the owner
// password — the historical config-less signing secret (retired Python twin:
// derive_secret_from_password). Kept for the one-shot oc.toml → DB migration:
// existing installs' tokens are all signed with this derived key, so it is
// what gets imported into the DB (zero token invalidation).
func deriveSecretFromPassword(password string) []byte {
	sum := sha256.Sum256(append([]byte("officraft.jwt.hs256.v1:"), password...))
	return sum[:]
}

// ── multi-key verification ───────────────────────────────────────────────────

// verifyJWTAnyKey verifies a token against every key in the ring, the signing
// key first (keyring.verifyCandidates orders them). A token verifies if ANY key
// in the ring signed it — that is what lets a rotation happen without
// invalidating the tokens already in circulation, and what makes REMOVING a key
// the act that actually revokes them.
//
// It answers THREE values: the claims, the ID OF THE KEY THAT VERIFIED, and the
// error. The id exists because "which key is this credential signed by" is
// otherwise unanswerable after the fact — the JWT header is a constant and
// carries no kid, and nothing else in the process remembers. T-80's whole
// question ("how many machines are still on the outgoing key, i.e. is it safe to
// press remove") is that id, observed here and recorded per machine.
//
// 🔴 THE ID IS RETURNED ON SUCCESS ONLY, AND IT IS FOR THE CALLER'S INTERNAL USE.
// On every failure path it is "". It must not reach a response body, a refusal
// message or an unauthenticated surface: what the caller may do with it is
// record it against the identity that just authenticated.
//
// 🔴 THE RETURNED ERROR IS THE LAST KEY'S, AND THAT IS DELIBERATE. An expired
// token fails every key with errExpiredToken, so the caller still gets
// errExpiredToken rather than a generic refusal; a forged one fails every key
// with a signature error. What must never happen is a per-key error that tells
// the caller WHICH key failed — the refusal says a token did not verify, never
// anything about the ring. The key id added above does not weaken that by one
// bit: it is populated only when verification SUCCEEDED, so no refusal, and no
// error string, ever names a key.
func verifyJWTAnyKey(kr *keyring, token string, now int64) (map[string]any, string, error) {
	candidates := kr.verifyCandidates()
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("%w: server has no signing key", errInvalidToken)
	}
	var lastErr error
	for _, candidate := range candidates {
		claims, err := verifyJWT(token, candidate.Key, now)
		if err == nil {
			return claims, candidate.ID, nil
		}
		// An expired token is expired under every key; stop rather than pay an
		// HMAC per key to reach the same answer.
		if errors.Is(err, errExpiredToken) {
			return nil, "", err
		}
		lastErr = err
	}
	return nil, "", lastErr
}
