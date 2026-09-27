package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	errInvalidToken = errors.New("invalid token")
	errExpiredToken = fmt.Errorf("%w: expired", errInvalidToken)
)

const jwtHeaderJSON = `{"alg":"HS256","typ":"JWT"}`

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

func mintJWT(sub, scope string, ttl int64, secret []byte, now int64, machineID string) (string, error) {
	if sub == "" {
		return "", fmt.Errorf("%w: mint requires a non-empty sub (identity id)", errInvalidToken)
	}
	exp := now + ttl
	return mintJWTClaims(jwtClaims{Sub: sub, Scope: scope, Iat: now, Exp: &exp, MachineID: machineID}, secret)
}

// mintJWTWithoutExpiry has NO production caller left (T-fc53 第二段); it exists
// so tests can mint the exp-less shape verifyJWT must keep accepting. A
// production caller would mint a credential nothing can ever expire — not
// without an owner ruling.
func mintJWTWithoutExpiry(sub, scope string, secret []byte, now int64, machineID string) (string, error) {
	if sub == "" {
		return "", fmt.Errorf("%w: mint requires a non-empty sub (identity id)", errInvalidToken)
	}
	return mintJWTClaims(jwtClaims{Sub: sub, Scope: scope, Iat: now, MachineID: machineID}, secret)
}

func mintJWTClaims(claims jwtClaims, secret []byte) (string, error) {
	// 🔴 THE EMPTY-KEY REFUSAL LIVES HERE, at the one seam every mint passes
	// through, not at each caller: an empty key is a valid HMAC key, so without
	// it the server would sign a credential under nothing and say 200.
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

// verifyJWT: a MISSING exp is still accepted — a backwards-compatibility rule
// (T-fc53 第二段) for warden credentials issued before exp was stamped; for
// those, a deleted machine's roster row, not a timer, is the revocation seam.
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
		exp, ok := rawExp.(float64)
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

// deriveSecretFromPassword is kept for the one-shot oc.toml → DB import:
// existing installs' tokens are signed with this derived key, so it is what gets
// imported (zero token invalidation).
func deriveSecretFromPassword(password string) []byte {
	sum := sha256.Sum256(append([]byte("officraft.jwt.hs256.v1:"), password...))
	return sum[:]
}

// verifyJWTAnyKey returns the ID OF THE KEY THAT VERIFIED because the JWT
// header carries no kid, and "how many machines are still on the outgoing key"
// needs it.
//
// 🔴 The id is returned on SUCCESS ONLY and is for the caller's internal record
// against the identity that just authenticated — never a response body or a
// refusal. The returned error is the LAST key's on purpose: a refusal must never
// say which key failed.
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
		if errors.Is(err, errExpiredToken) {
			return nil, "", err
		}
		lastErr = err
	}
	return nil, "", lastErr
}
