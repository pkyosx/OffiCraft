package main

// avatar_bundle.go — T-16a1 P5: server-side validation of a theme bundle's
// optional `avatars` overlay (per-member-type avatar images). The overlay is
// `{ <kind>: "<data-URI>" }`:
//
//   - the KIND key is `member` (正職) or `outsource` (外包) — a closed set;
//   - the VALUE is an EMBEDDED image: a base64 `data:` URI so the picture
//     travels INSIDE the bundle on export/import (owner ruling: the image
//     follows the theme). It is NOT an arbitrary string. This is a NEW attack
//     surface (an image the browser will render), so the value passes a strict
//     gate before it is ever stored:
//       1. it MUST be `data:image/<mime>;base64,<base64>` (no other data-URI
//          form — no `text/html`, no `image/svg+xml`, no `;charset`, no bare
//          URL);
//       2. mime ∈ {image/png, image/jpeg, image/webp} — a RASTER whitelist.
//          SVG is REJECTED (it can carry <script>/onload → XSS);
//       3. the base64 must decode cleanly;
//       4. the DECODED byte size ≤ maxAvatarBytes (64 KiB), and the raw string
//          length ≤ maxAvatarValueLen (a cheap pre-filter);
//       5. the leading MAGIC BYTES must match the declared mime (PNG 89 50 4E
//          47, JPEG FF D8 FF, WEBP `RIFF....WEBP`) — so a value that declares
//          image/png but carries an SVG/script/other payload is rejected.
//
// The value is applied on the client as an <img src="data:...">; even so we
// admit only the raster whitelist + verify magic bytes, so the declared mime is
// the real mime. This mirrors, rule for rule, the client validator in
// frontend/src/lib/themeBundle.ts (shared with the mock API), so an avatars
// overlay rejected offline is rejected online for the identical reason.
// avatars is OPTIONAL: an absent overlay is fine; a present one is validated in
// full. Any violation is a 422 — never silently dropped, never stored. No image
// library is used — the check is stdlib base64 + a hand-rolled magic-byte
// prefix compare (no heavy dependency, no pixel decode).

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

// strictBase64Re is the exact standard-base64 alphabet + padding the client
// regex admits (^[A-Za-z0-9+/]+={0,2}$). It is applied to the data-URI payload
// BEFORE base64.StdEncoding.DecodeString (which is lenient about ASCII
// whitespace) so the server rejects the identical byte the client rejects.
var strictBase64Re = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)

const (
	// maxAvatarBytes caps the DECODED image size. An avatar is a small
	// roster/chat glyph, not a photo — 64 KiB is generous for a crisp raster
	// icon, and the real guard against bloating the single custom_themes JSON
	// row. The twin of MAX_AVATAR_BYTES on the client.
	maxAvatarBytes = 64 * 1024
	// maxAvatarValueLen caps the raw data-URI string length (bytes). base64
	// inflates ~4/3, so 64 KiB decoded ≈ 87.4 KiB encoded; this cap sits above
	// that with margin and is a cheap pre-filter BEFORE we decode, so a
	// pathologically long string is rejected without allocating its decode.
	maxAvatarValueLen = 96 * 1024
)

// avatarKindAllowed is the closed set of member-type keys an avatars overlay
// may carry. Any other key is a 422.
var avatarKindAllowed = map[string]bool{"member": true, "outsource": true}

// avatarMimeMagic maps each whitelisted RASTER mime to a predicate over the
// decoded bytes: the leading magic bytes that mime's format must begin with.
// SVG is deliberately ABSENT (no entry ⇒ not whitelisted ⇒ rejected) — it is a
// script-bearing XSS surface, not a raster image.
var avatarMimeMagic = map[string]func([]byte) bool{
	// PNG: 89 50 4E 47 0D 0A 1A 0A
	"image/png": func(b []byte) bool {
		return len(b) >= 8 &&
			b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47 &&
			b[4] == 0x0D && b[5] == 0x0A && b[6] == 0x1A && b[7] == 0x0A
	},
	// JPEG: FF D8 FF
	"image/jpeg": func(b []byte) bool {
		return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
	},
	// WEBP: "RIFF" .... "WEBP" (the RIFF container tag + form type)
	"image/webp": func(b []byte) bool {
		return len(b) >= 12 &&
			b[0] == 'R' && b[1] == 'I' && b[2] == 'F' && b[3] == 'F' &&
			b[8] == 'W' && b[9] == 'E' && b[10] == 'B' && b[11] == 'P'
	},
}

// validAvatarValue reports whether v is an admissible embedded avatar image:
// a `data:image/<whitelisted mime>;base64,<base64>` URI that decodes within the
// byte cap and whose magic bytes match the declared mime. Returns a specific
// reason on failure so the 422 body is actionable.
func validAvatarValue(v string) error {
	if v == "" {
		return fmt.Errorf("must not be empty")
	}
	if len(v) > maxAvatarValueLen {
		return fmt.Errorf("data URI is too long (max %d bytes)", maxAvatarValueLen)
	}
	const prefix = "data:"
	if !strings.HasPrefix(v, prefix) {
		return fmt.Errorf("must be a base64 data: URI")
	}
	// Split "data:<meta>,<data>" — exactly one comma splits header from payload.
	comma := strings.IndexByte(v, ',')
	if comma < 0 {
		return fmt.Errorf("must be a base64 data: URI")
	}
	meta := v[len(prefix):comma] // e.g. "image/png;base64"
	payload := v[comma+1:]
	// The meta MUST be exactly "<mime>;base64" — no charset, no other params,
	// and base64 encoding is mandatory (a plain/URL-encoded data URI is refused).
	if !strings.HasSuffix(meta, ";base64") {
		return fmt.Errorf("must be base64-encoded (data:<mime>;base64,...)")
	}
	mime := strings.TrimSuffix(meta, ";base64")
	magic, ok := avatarMimeMagic[mime]
	if !ok {
		return fmt.Errorf(
			"mime %q is not an allowed image type (only image/png, image/jpeg, image/webp)", mime)
	}
	// STRICT base64 pre-check BEFORE decoding — the character-for-character twin
	// of the client regex ^[A-Za-z0-9+/]+={0,2}$ (+ length%4==0). Go's
	// base64.StdEncoding.DecodeString SILENTLY SKIPS ASCII whitespace (\n, \r,
	// space, tab), so a payload with embedded newlines would decode on the
	// server yet be rejected by the client's strict regex — a double-ended
	// asymmetry that breaks the "reject offline ⇔ reject online" guarantee (even
	// though the skipped-whitespace result is still a valid raster, i.e. no XSS).
	// Rejecting the exact same alphabet the client does keeps the twins honest.
	if !strictBase64Re.MatchString(payload) || len(payload)%4 != 0 {
		return fmt.Errorf("invalid base64 image data")
	}
	// Decode the base64 payload (strict standard alphabet, padding required).
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("invalid base64 image data")
	}
	if len(raw) == 0 {
		return fmt.Errorf("decoded image is empty")
	}
	if len(raw) > maxAvatarBytes {
		return fmt.Errorf("decoded image is too large (max %d bytes)", maxAvatarBytes)
	}
	if !magic(raw) {
		return fmt.Errorf(
			"image bytes do not match declared mime %q (magic-byte check failed)", mime)
	}
	return nil
}

// validateAvatars validates one bundle's optional avatars overlay. `where` is
// the caller's bundle locator (e.g. "custom_themes[2]") for a precise 422
// message. A nil overlay is admissible (avatars is optional).
func validateAvatars(avatars *map[string]string, where string) error {
	if avatars == nil {
		return nil
	}
	for kind, value := range *avatars {
		if !avatarKindAllowed[kind] {
			return fmt.Errorf(
				"%s: avatar kind %q is not allowed (only member, outsource)", where, kind)
		}
		if err := validAvatarValue(value); err != nil {
			return fmt.Errorf("%s: avatars[%s] %v", where, kind, err)
		}
	}
	return nil
}
