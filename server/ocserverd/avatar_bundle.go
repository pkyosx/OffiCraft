package main

// Server-side gate for a theme bundle's embedded images (avatars, logo,
// navIcons, backgrounds). Values are base64 `data:` URIs so the picture travels
// inside the bundle on export/import (owner ruling). An image the browser
// renders is an attack surface: only a RASTER whitelist is admitted and the
// magic bytes must match the declared mime — SVG is rejected (it can carry
// <script>/onload → XSS).
//
// Avatar kinds: staff (正職), outsource (外包), owner (the human CEO), assistant
// (a member whose role is assistant).
//
// Mirrors, rule for rule, the client validator in frontend/src/lib/themeBundle.ts,
// so a bundle rejected offline is rejected online for the identical reason.

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Twin of the client regex. Applied BEFORE base64.StdEncoding.DecodeString,
// which silently skips ASCII whitespace, so the server rejects the identical
// byte the client rejects.
var strictBase64Re = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)

// Two (decoded bytes, data-URI length) cap pairs, one per purpose, and they must
// not be merged back into one: a full-viewport background at 64 KiB is visibly
// blurry (owner ruling 2026-08-03), while relaxing the glyph cap is not wanted.
// Twinned with MAX_AVATAR_BYTES / MAX_AVATAR_VALUE_LEN / MAX_BACKGROUND_BYTES /
// MAX_BACKGROUND_VALUE_LEN on the client, enforced by
// bin/tests/fixtures/image-cap-cases.tsv and its two mirror tests.
const (
	maxAvatarBytes = 64 * 1024

	maxAvatarValueLen = 96 * 1024

	maxBackgroundBytes = 512 * 1024
	// MUST move with maxBackgroundBytes: it runs before the decode, so leaving it
	// at the avatar value cap would reject every large background.
	maxBackgroundValueLen = 704 * 1024
)

var avatarKindAllowed = map[string]bool{
	"staff": true, "outsource": true, "owner": true, "assistant": true,
}

// Owner ruling: the member→staff rename is a hard cut (old bundles are not
// migrated), but an old bundle must fail with a message that names the rename,
// not silently fall back to the built-in glyph.
var avatarKindRetired = map[string]string{"member": "staff"}

// Twin of AVATAR_KINDS_PROSE in frontend/src/lib/themeBundleCore.ts.
func avatarKindsAllowedList() string {
	kinds := make([]string, 0, len(avatarKindAllowed))
	for k := range avatarKindAllowed {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return strings.Join(kinds, ", ")
}

var avatarMimeMagic = map[string]func([]byte) bool{
	"image/png": func(b []byte) bool {
		return len(b) >= 8 &&
			b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47 &&
			b[4] == 0x0D && b[5] == 0x0A && b[6] == 0x1A && b[7] == 0x0A
	},
	"image/jpeg": func(b []byte) bool {
		return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
	},
	"image/webp": func(b []byte) bool {
		return len(b) >= 12 &&
			b[0] == 'R' && b[1] == 'I' && b[2] == 'F' && b[3] == 'F' &&
			b[8] == 'W' && b[9] == 'E' && b[10] == 'B' && b[11] == 'P'
	},
}

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
	comma := strings.IndexByte(v, ',')
	if comma < 0 {
		return fmt.Errorf("must be a base64 data: URI")
	}
	meta := v[len(prefix):comma]
	payload := v[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return fmt.Errorf("must be base64-encoded (data:<mime>;base64,...)")
	}
	mime := strings.TrimSuffix(meta, ";base64")
	magic, ok := avatarMimeMagic[mime]
	if !ok {
		return fmt.Errorf(
			"mime %q is not an allowed image type (only image/png, image/jpeg, image/webp)", mime)
	}
	if !strictBase64Re.MatchString(payload) || len(payload)%4 != 0 {
		return fmt.Errorf("invalid base64 image data")
	}
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

func validAvatarValue(v string) error {
	return validImageValue(v, maxAvatarBytes, maxAvatarValueLen)
}

func validBackgroundValue(v string) error {
	return validImageValue(v, maxBackgroundBytes, maxBackgroundValueLen)
}

func validateAvatars(avatars *map[string]string, where string) error {
	if avatars == nil {
		return nil
	}
	for kind, value := range *avatars {
		if !avatarKindAllowed[kind] {
			if renamedTo, retired := avatarKindRetired[kind]; retired {
				return fmt.Errorf(
					"%s: avatar kind %q was renamed to %q — this theme bundle was exported by an "+
						"older version of OffiCraft and is no longer importable. Re-export it from "+
						"the theme editor after upgrading, or rename the key by hand (only %s)",
					where, kind, renamedTo, avatarKindsAllowedList())
			}
			return fmt.Errorf(
				"%s: avatar kind %q is not allowed (only %s)", where, kind, avatarKindsAllowedList())
		}
		if err := validAvatarValue(value); err != nil {
			return fmt.Errorf("%s: avatars[%s] %v", where, kind, err)
		}
	}
	return nil
}

// Twin of NAV_ICON_KEYS in frontend/src/lib/themeBundleCore.ts (the App.tsx nav
// tabs): a key one side accepts and the other rejects half-imports a theme pack.
var navIconKeyAllowed = map[string]bool{
	"office": true, "replies": true, "tasks": true, "lore": true, "monitor": true, "guide": true,
}

func validateLogo(logo *string, where string) error {
	if logo == nil {
		return nil
	}
	if err := validAvatarValue(*logo); err != nil {
		return fmt.Errorf("%s: logo %v", where, err)
	}
	return nil
}

// Only the outer `canvas` zone: topbar / nav / main sit under text, and text
// over a busy tiled pattern has no readability guarantee.
var backgroundKeyAllowed = map[string]bool{"canvas": true}

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

// `tile` is the default for an unlisted zone. `sides` is not mirrored — a theme
// wanting symmetry bakes it into the image (owner 2026-07-27). `cover`'s
// readability risk was accepted by the owner (rc-f0e23286d75e).
var backgroundModeAllowed = map[string]bool{"tile": true, "sides": true, "cover": true}

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
