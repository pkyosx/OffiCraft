package main

// jwt.go — HS256 JWT mint/verify, the byte-level Go twin of
// the retired Python plumbing/auth.py. One self-describing, stateless identity token for
// every gated surface; the signing secret lives in the DB settings store
// (settings.go).
//
// INTEROP CONTRACT (locked by jwt_test.go): given the same inputs, mintJWT
// produces the IDENTICAL compact token the Python `plumbing.auth.mint`
// produces — same header ({"alg":"HS256","typ":"JWT"}), same claim ORDER
// (sub, scope, iat, exp[, machine_id]), same compact JSON (no spaces), same
// unpadded base64url — so a token minted by either daemon verifies on the
// other under the shared secret.

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
	Exp       int64  `json:"exp"`
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
	claims := jwtClaims{Sub: sub, Scope: scope, Iat: now, Exp: now + ttl, MachineID: machineID}
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
// compare (hmac.Equal), exp present + not in the past (errExpiredToken), and a
// non-empty sub.
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

	exp, ok := claims["exp"].(float64) // encoding/json numbers land as float64
	if !ok {
		return nil, fmt.Errorf("%w: token has no numeric exp", errInvalidToken)
	}
	if float64(now) >= exp {
		return nil, errExpiredToken
	}
	if sub, _ := claims["sub"].(string); sub == "" {
		return nil, fmt.Errorf("%w: token has no sub (identity id)", errInvalidToken)
	}
	return claims, nil
}

// ── Secret derivation ────────────────────────────────────────────────────────
//
// The signing secret itself now lives in the DB settings store (settings.go:
// loadAuthSettings — migrated in for existing installs, minted for fresh
// ones). The old resolveSecret ladder and its var/jwt_secret fallback file
// are retired with it.

// deriveSecretFromPassword is a domain-separated SHA-256 of the owner
// password — the historical config-less signing secret (retired Python twin:
// derive_secret_from_password). Kept for the one-shot oc.toml → DB migration:
// existing installs' tokens are all signed with this derived key, so it is
// what gets imported into the DB (zero token invalidation).
func deriveSecretFromPassword(password string) []byte {
	sum := sha256.Sum256(append([]byte("officraft.jwt.hs256.v1:"), password...))
	return sum[:]
}
