package main

// avatar_bundle.go — T-16a1 P5: server-side validation of a theme bundle's
// optional `avatars` overlay (per-member-type avatar images). The overlay is
// `{ <kind>: "<data-URI>" }`:
//
//   - the KIND key is `member` (正職), `outsource` (外包), `owner` (the human
//     CEO) or `assistant` (a member whose role is assistant, e.g. Mira) — a
//     closed set (owner/assistant added in T-ea81);
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
//       4. the DECODED byte size and the raw data-URI string length are both
//          capped — by a PER-PURPOSE pair, not one global pair (T-72da):
//          avatars / logo / navIcons use maxAvatarBytes (64 KiB) +
//          maxAvatarValueLen, backgrounds use maxBackgroundBytes (512 KiB) +
//          maxBackgroundValueLen. See the const block for why they differ;
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

// The image caps come in PAIRS — (decoded byte cap, raw data-URI string cap) —
// and there are TWO pairs, one per purpose (T-72da). They are deliberately NOT
// one number, and must not be "tidied up" back into one:
//
//   - an AVATAR / logo / nav-icon is a 30–40 px glyph rendered in a roster row,
//     the top bar or a tab. 64 KiB is already generous for a crisp raster icon
//     at that size, and nothing legitimate needs more;
//   - a canvas BACKGROUND is stretched or tiled across the WHOLE viewport. At
//     that size 64 KiB is visibly mushy — the owner hit that ceiling with a real
//     background and reported it three times (owner ruling 2026-08-03, which
//     overturned the T-081b decision that backgrounds must share the avatar cap;
//     that decision's premise was that both went through ONE gate, which is
//     exactly what this split removes).
//
// The two are held apart so relaxing the wallpaper does NOT relax the glyph:
// avatars stay at 64 KiB. Both pairs are twinned by MAX_AVATAR_BYTES /
// MAX_AVATAR_VALUE_LEN / MAX_BACKGROUND_BYTES / MAX_BACKGROUND_VALUE_LEN on the
// client, and that twinning is enforced — not merely asserted in prose — by
// bin/tests/fixtures/image-cap-cases.tsv and its two mirror tests.
const (
	// maxAvatarBytes caps the DECODED image size of an avatar / logo / nav
	// icon. A small roster/chat glyph, not a photo, and the real guard against
	// bloating a theme. (It said "the single custom_themes JSON row" until
	// T-83ef gave themes their own table — one row each now, so the cap bounds
	// what ONE theme weighs rather than what one shared row does. The number
	// and the reason for it are unchanged.) The twin of MAX_AVATAR_BYTES on the
	// client.
	maxAvatarBytes = 64 * 1024
	// maxAvatarValueLen caps the raw data-URI string length (bytes) for those
	// same three. base64 inflates ~4/3, so 64 KiB decoded ≈ 87.4 KiB encoded;
	// this cap sits above that with margin and is a cheap pre-filter BEFORE we
	// decode, so a pathologically long string is rejected without allocating
	// its decode.
	maxAvatarValueLen = 96 * 1024
	// maxBackgroundBytes caps the DECODED size of a `backgrounds` image, which
	// covers the whole viewport rather than a 30–40 px glyph — see the block
	// comment above for why this is 8× the avatar cap. The twin of
	// MAX_BACKGROUND_BYTES on the client.
	maxBackgroundBytes = 512 * 1024
	// maxBackgroundValueLen is the string-length pre-filter that MUST move with
	// maxBackgroundBytes: it runs BEFORE the decode, so leaving it at the avatar
	// value cap would reject every large background with "data URI is too long"
	// and the 512 KiB decoded cap below would never be reached. 512 KiB decoded
	// ≈ 682.7 KiB encoded (×4/3); 704 KiB sits above that with margin.
	maxBackgroundValueLen = 704 * 1024
)

// avatarKindAllowed is the closed set of member-type keys an avatars overlay
// may carry. Any other key is a 422. Extended in T-ea81: owner (the human CEO)
// and assistant (a member whose role is assistant, e.g. Mira) join the original
// member / outsource kinds.
var avatarKindAllowed = map[string]bool{
	"member": true, "outsource": true, "owner": true, "assistant": true,
}

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

// validImageValue reports whether v is an admissible embedded image: a
// `data:image/<whitelisted mime>;base64,<base64>` URI that decodes within
// maxDecoded bytes, whose raw string is at most maxValueLen bytes, and whose
// magic bytes match the declared mime. Returns a specific reason on failure so
// the 422 body is actionable. Everything EXCEPT the two size caps is identical
// for every purpose — the mime allowlist, the SVG refusal, the strict base64
// alphabet and the magic-byte check are the security boundary and are shared;
// only "how big" is per-purpose (T-72da).
func validImageValue(v string, maxDecoded, maxValueLen int) error {
	if v == "" {
		return fmt.Errorf("must not be empty")
	}
	if len(v) > maxValueLen {
		return fmt.Errorf("data URI is too long (max %d bytes)", maxValueLen)
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
	if len(raw) > maxDecoded {
		return fmt.Errorf("decoded image is too large (max %d bytes)", maxDecoded)
	}
	if !magic(raw) {
		return fmt.Errorf(
			"image bytes do not match declared mime %q (magic-byte check failed)", mime)
	}
	return nil
}

// validAvatarValue is the image gate at the AVATAR caps (64 KiB) — the gate for
// avatars, logo and navIcons, i.e. the small glyphs. Backgrounds use
// validBackgroundValue instead.
func validAvatarValue(v string) error {
	return validImageValue(v, maxAvatarBytes, maxAvatarValueLen)
}

// validBackgroundValue is the same image gate at the BACKGROUND caps (512 KiB).
// Only `backgrounds` values come through here; see the const block for why a
// full-viewport image is allowed more room than a 30–40 px glyph.
func validBackgroundValue(v string) error {
	return validImageValue(v, maxBackgroundBytes, maxBackgroundValueLen)
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
				"%s: avatar kind %q is not allowed (only member, outsource, owner, assistant)", where, kind)
		}
		if err := validAvatarValue(value); err != nil {
			return fmt.Errorf("%s: avatars[%s] %v", where, kind, err)
		}
	}
	return nil
}

// navIconKeyAllowed is the closed set of nav-tab keys a navIcons overlay may
// carry — the five nav tabs of App.tsx (`Tab` type). Any other key is a 422.
var navIconKeyAllowed = map[string]bool{
	"office": true, "replies": true, "tasks": true, "monitor": true, "guide": true,
}

// validateLogo validates a bundle's optional single studio-logo image (T-ea81).
// nil is admissible (logo is optional); a present logo passes the SAME strict
// image gate as an avatar value (validAvatarValue — data-URI / raster-mime /
// 64 KiB size / magic-byte). A logo is a top-bar glyph, so it keeps the avatar
// caps; only `backgrounds` was relaxed in T-72da.
func validateLogo(logo *string, where string) error {
	if logo == nil {
		return nil
	}
	if err := validAvatarValue(*logo); err != nil {
		return fmt.Errorf("%s: logo %v", where, err)
	}
	return nil
}

// backgroundKeyAllowed is the closed set of chrome zones a backgrounds overlay
// may carry (T-081b). It holds exactly ONE zone: `canvas`, the outermost canvas
// beside the centred content column. The zone tokens above it (topbar / nav /
// main) are deliberately NOT here — they sit under text, and text over a busy
// tiled pattern has no readability guarantee. Any other key is a 422.
var backgroundKeyAllowed = map[string]bool{"canvas": true}

// validateBackgrounds validates a bundle's optional outer-canvas background
// overlay (T-081b). A nil overlay is admissible. Each key must be an allowed
// zone; each value passes the same strict image gate as an avatar value for
// everything that is a SECURITY property — data-URI shape, raster-mime
// allowlist, SVG refusal, magic-byte check — but at the BACKGROUND size caps
// (validBackgroundValue: 512 KiB decoded, not the avatar's 64 KiB).
//
// T-081b ruled the opposite ("the cap is NOT relaxed: a tileable texture fits
// easily, a wallpaper is exactly what the cap must stop"). The owner overturned
// that on 2026-08-03: a canvas background is stretched across the whole
// viewport, and at 64 KiB his real background read as visibly blurry. That
// ruling's premise — one gate for all four image fields, so relaxing one
// relaxed the avatars too — no longer holds: validImageValue now takes the caps
// as parameters, and avatars / logo / navIcons keep 64 KiB.
func validateBackgrounds(backgrounds *map[string]string, where string) error {
	if backgrounds == nil {
		return nil
	}
	for key, value := range *backgrounds {
		if !backgroundKeyAllowed[key] {
			return fmt.Errorf(
				"%s: background zone %q is not allowed (only canvas)", where, key)
		}
		if err := validBackgroundValue(value); err != nil {
			return fmt.Errorf("%s: backgrounds[%s] %v", where, key, err)
		}
	}
	return nil
}

// backgroundModeAllowed is the closed set of display modes a background image
// may be laid down with (T-081b). `tile` repeats it over both axes — the ONLY
// behaviour before this field existed, hence the default for an unlisted zone;
// `sides` pins ONE copy against each viewport edge for art that reads as a pair
// of standing objects rather than a texture (not mirrored — a theme wanting
// symmetry bakes it into the image, owner 2026-07-27); `cover` scales one copy
// to fill the viewport, for a single scene the cockpit floats on. `cover` is
// only visible where the theme also makes the chrome zone colours translucent
// (the colour grammar already admits #RRGGBBAA / rgba()), which is where its
// readability risk lives — owner accepted it on rc-f0e23286d75e.
var backgroundModeAllowed = map[string]bool{"tile": true, "sides": true, "cover": true}

// validateBackgroundModes validates a bundle's optional per-zone display-mode
// map (T-081b). A nil map is admissible and means every zone tiles. Each key
// must be an allowed zone AND must carry an image in the same bundle's
// backgrounds — a mode on an imageless zone paints nothing, so it is a mistake
// worth naming rather than ignoring. Each value must be an allowed mode.
func validateBackgroundModes(
	modes *map[string]string, backgrounds *map[string]string, where string,
) error {
	if modes == nil {
		return nil
	}
	for key, value := range *modes {
		if !backgroundKeyAllowed[key] {
			return fmt.Errorf(
				"%s: background zone %q is not allowed (only canvas)", where, key)
		}
		if backgrounds == nil || (*backgrounds)[key] == "" {
			return fmt.Errorf(
				"%s: backgroundModes[%s] has no image in backgrounds[%s]",
				where, key, key)
		}
		if !backgroundModeAllowed[value] {
			return fmt.Errorf(
				"%s: backgroundModes[%s] %q is not a valid mode (only tile, sides, cover)",
				where, key, value)
		}
	}
	return nil
}

// validateNavIcons validates a bundle's optional per-tab nav-icon overlay
// (T-ea81). A nil overlay is admissible. Each key must be one of the five nav
// tabs (navIconKeyAllowed); each value passes the SAME strict image gate as an
// avatar value (validAvatarValue) at the avatar caps — a nav icon is a tab
// glyph, so it keeps 64 KiB; only `backgrounds` was relaxed in T-72da.
func validateNavIcons(navIcons *map[string]string, where string) error {
	if navIcons == nil {
		return nil
	}
	for key, value := range *navIcons {
		if !navIconKeyAllowed[key] {
			return fmt.Errorf(
				"%s: nav icon key %q is not allowed (only office, replies, tasks, monitor, guide)", where, key)
		}
		if err := validAvatarValue(value); err != nil {
			return fmt.Errorf("%s: navIcons[%s] %v", where, key, err)
		}
	}
	return nil
}
