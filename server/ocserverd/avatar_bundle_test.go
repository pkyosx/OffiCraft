// Skeleton generated from server/ocserverd/avatar_bundle.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestValidImageValue(t *testing.T) {
	valid := []struct {
		name string
		mime string
		raw  []byte
	}{
		{name: "png", mime: "image/png", raw: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}},
		{name: "jpeg", mime: "image/jpeg", raw: []byte{0xff, 0xd8, 0xff}},
		{name: "webp", mime: "image/webp", raw: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			if err := validImageValue(imageDataURI(tt.mime, tt.raw), len(tt.raw), maxAvatarValueLen); err != nil {
				t.Fatalf("validImageValue: %v", err)
			}
		})
	}

	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	tests := []struct {
		name  string
		value string
		max   int
		want  string
	}{
		{name: "empty", value: "", max: maxAvatarBytes, want: "must not be empty"},
		{name: "not a data URI", value: "https://example.test/image.png", max: maxAvatarBytes, want: "base64 data"},
		{name: "missing payload separator", value: "data:image/png;base64", max: maxAvatarBytes, want: "base64 data"},
		{name: "missing base64 marker", value: "data:image/png," + base64.StdEncoding.EncodeToString(png), max: maxAvatarBytes, want: "base64-encoded"},
		{name: "svg is not an allowed mime", value: imageDataURI("image/svg+xml", png), max: maxAvatarBytes, want: "not an allowed image type"},
		{name: "invalid base64 alphabet", value: "data:image/png;base64,!!!!", max: maxAvatarBytes, want: "invalid base64"},
		{name: "invalid base64 length", value: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png) + "A", max: maxAvatarBytes, want: "invalid base64"},
		{name: "empty decoded payload", value: "data:image/png;base64,====", max: maxAvatarBytes, want: "invalid base64"},
		{name: "declared mime does not match magic", value: imageDataURI("image/jpeg", png), max: maxAvatarBytes, want: "magic-byte check failed"},
		{name: "raw value cap", value: strings.Repeat("x", maxAvatarValueLen+1), max: maxAvatarBytes, want: "data URI is too long"},
		{name: "decoded byte cap", value: imageDataURI("image/png", append(append([]byte{}, png...), bytes.Repeat([]byte{'x'}, maxAvatarBytes-len(png)+1)...)), max: maxAvatarBytes, want: "decoded image is too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validImageValue(tt.value, tt.max, maxAvatarValueLen)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validImageValue() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateAvatars(t *testing.T) {
	valid := imageDataURI("image/png", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	if err := validateAvatars(nil, "theme[0]"); err != nil {
		t.Fatalf("validateAvatars(nil): %v", err)
	}
	values := map[string]string{"member": valid, "outsource": valid, "owner": valid, "assistant": valid}
	if err := validateAvatars(&values, "theme[0]"); err != nil {
		t.Fatalf("validateAvatars(valid): %v", err)
	}
	for _, tt := range []struct {
		name string
		data map[string]string
		want string
	}{
		{name: "unknown kind", data: map[string]string{"robot": valid}, want: "not allowed"},
		{name: "invalid image", data: map[string]string{"member": "not-an-image"}, want: "avatars[member]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateAvatars(&tt.data, "theme[0]"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateAvatars() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateLogo(t *testing.T) {
	valid := imageDataURI("image/png", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	if err := validateLogo(nil, "theme[0]"); err != nil {
		t.Fatalf("validateLogo(nil): %v", err)
	}
	if err := validateLogo(&valid, "theme[0]"); err != nil {
		t.Fatalf("validateLogo(valid): %v", err)
	}
	invalid := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>"))
	if err := validateLogo(&invalid, "theme[0]"); err == nil || !strings.Contains(err.Error(), "logo") {
		t.Fatalf("validateLogo(invalid) = %v, want logo error", err)
	}
}

func TestValidateBackgrounds(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	large := append(append([]byte{}, png...), bytes.Repeat([]byte{'x'}, maxAvatarBytes-len(png)+1)...)
	valid := imageDataURI("image/png", large)
	if err := validateBackgrounds(nil, "theme[0]"); err != nil {
		t.Fatalf("validateBackgrounds(nil): %v", err)
	}
	backgrounds := map[string]string{"canvas": valid}
	if err := validateBackgrounds(&backgrounds, "theme[0]"); err != nil {
		t.Fatalf("validateBackgrounds(valid large image): %v", err)
	}
	for _, tt := range []struct {
		name string
		data map[string]string
		want string
	}{
		{name: "unknown zone", data: map[string]string{"topbar": valid}, want: "not allowed"},
		{name: "invalid image", data: map[string]string{"canvas": "not-an-image"}, want: "backgrounds[canvas]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateBackgrounds(&tt.data, "theme[0]"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateBackgrounds() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateBackgroundModes(t *testing.T) {
	valid := imageDataURI("image/png", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	backgrounds := map[string]string{"canvas": valid}
	if err := validateBackgroundModes(nil, &backgrounds, "theme[0]"); err != nil {
		t.Fatalf("validateBackgroundModes(nil): %v", err)
	}
	for _, mode := range []string{"tile", "sides", "cover"} {
		modes := map[string]string{"canvas": mode}
		if err := validateBackgroundModes(&modes, &backgrounds, "theme[0]"); err != nil {
			t.Fatalf("validateBackgroundModes(%q): %v", mode, err)
		}
	}
	for _, tt := range []struct {
		name        string
		modes       map[string]string
		backgrounds *map[string]string
		want        string
	}{
		{name: "unknown zone", modes: map[string]string{"nav": "tile"}, backgrounds: &backgrounds, want: "not allowed"},
		{name: "missing image", modes: map[string]string{"canvas": "tile"}, backgrounds: nil, want: "has no image"},
		{name: "empty image", modes: map[string]string{"canvas": "tile"}, backgrounds: func() *map[string]string { v := map[string]string{"canvas": ""}; return &v }(), want: "has no image"},
		{name: "unknown mode", modes: map[string]string{"canvas": "stretch"}, backgrounds: &backgrounds, want: "not a valid mode"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateBackgroundModes(&tt.modes, tt.backgrounds, "theme[0]"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateBackgroundModes() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateNavIcons(t *testing.T) {
	valid := imageDataURI("image/png", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	if err := validateNavIcons(nil, "theme[0]"); err != nil {
		t.Fatalf("validateNavIcons(nil): %v", err)
	}
	icons := map[string]string{"office": valid, "replies": valid, "tasks": valid, "monitor": valid, "guide": valid}
	if err := validateNavIcons(&icons, "theme[0]"); err != nil {
		t.Fatalf("validateNavIcons(valid): %v", err)
	}
	for _, tt := range []struct {
		name string
		data map[string]string
		want string
	}{
		{name: "unknown tab", data: map[string]string{"settings": valid}, want: "not allowed"},
		{name: "invalid image", data: map[string]string{"office": "not-an-image"}, want: "navIcons[office]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateNavIcons(&tt.data, "theme[0]"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateNavIcons() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func imageDataURI(mime string, raw []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}
