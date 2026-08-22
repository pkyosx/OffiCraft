// image_cap_mirror_test.go — the server's half of the theme-bundle image-cap
// mirror confrontation (T-72da). See frontend/src/lib/imageCap.test.ts for the
// twin and bin/tests/fixtures/image-cap-cases.tsv for the reasoning; the short
// version:
//
// validImageValue here is the AUTHORITY — it is what actually refuses a theme
// bundle with an HTTP 422. The cockpit carries a hand-transcribed copy in
// TypeScript so a picked file is refused at the file picker instead of after a
// round trip. There is no compiler between the two, and a divergence produces
// no error anywhere: it produces a cockpit that either refuses an image the
// server would have taken, or takes one the server will refuse — silently,
// in one direction or the other.
//
// Before T-72da there were two loose numbers tied together only by two comments
// saying "the twin of"; T-72da split the caps per purpose and made it four. So
// both copies are checked against the SAME table, and a drift names the side
// that drifted rather than merely reporting that two things differ.
// Deliberately NOT a mock of one side inside the other's test — that would only
// prove the mock agrees with itself.
//
// The rows are driven through the real validateAvatars / validateLogo /
// validateNavIcons / validateBackgrounds entry points rather than the bare
// gate, so a cap that is raised but wired to the wrong call site still fails
// here.
package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

const imageCapCasesPath = "../../bin/tests/fixtures/image-cap-cases.tsv"

type imageCapRow struct {
	line     int
	name     string
	purpose  string
	decoded  int
	accepted bool
}

type imageCapDecls struct {
	avatarBytes        int
	avatarValueLen     int
	backgroundBytes    int
	backgroundValueLen int
}

// loadImageCapCases parses the shared table. Missing/unparseable = FATAL, never
// skip: a guard that goes green when it could not read its fixture is a lie.
func loadImageCapCases(t *testing.T) ([]imageCapRow, imageCapDecls) {
	t.Helper()
	f, err := os.Open(imageCapCasesPath)
	if err != nil {
		t.Fatalf("open %s: %v — this table is the only thing keeping avatar_bundle.go's image caps aligned with frontend/src/lib/themeBundleCore.ts", imageCapCasesPath, err)
	}
	defer f.Close()

	var decls imageCapDecls
	seen := map[string]bool{}
	declInto := map[string]*int{
		"avatar_bytes":         &decls.avatarBytes,
		"avatar_value_len":     &decls.avatarValueLen,
		"background_bytes":     &decls.backgroundBytes,
		"background_value_len": &decls.backgroundValueLen,
	}

	var rows []imageCapRow
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") && strings.Contains(line, "\t") {
			name := strings.TrimSpace(strings.TrimPrefix(line[:strings.IndexByte(line, '\t')], "# "))
			if into, ok := declInto[name]; ok {
				v, err := strconv.Atoi(strings.TrimSpace(line[strings.IndexByte(line, '\t')+1:]))
				if err != nil {
					t.Fatalf("%s:%d: unparseable `# %s` line: %v", imageCapCasesPath, n, name, err)
				}
				*into = v
				seen[name] = true
				continue
			}
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			t.Fatalf("%s:%d: want 4 tab-separated columns, got %d: %q", imageCapCasesPath, n, len(cols), line)
		}
		if cols[0] == "case" {
			continue // the header row
		}
		decoded, err := strconv.Atoi(cols[2])
		if err != nil {
			t.Fatalf("%s:%d: decoded_bytes: %v", imageCapCasesPath, n, err)
		}
		accepted, err := strconv.ParseBool(cols[3])
		if err != nil {
			t.Fatalf("%s:%d: accepted: %v", imageCapCasesPath, n, err)
		}
		rows = append(rows, imageCapRow{
			line: n, name: cols[0], purpose: cols[1], decoded: decoded, accepted: accepted,
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", imageCapCasesPath, err)
	}
	for name := range declInto {
		if !seen[name] {
			t.Fatalf("%s carries no `# %s<TAB><n>` line — that threshold would go untested", imageCapCasesPath, name)
		}
	}
	if len(rows) < 8 {
		t.Fatalf("%s carries %d rows — too few to prove anything", imageCapCasesPath, len(rows))
	}
	return rows, decls
}

// pngOfSize builds a decoded payload of exactly n bytes that begins with the PNG
// signature, so the magic-byte check (orthogonal to size) keeps passing and a
// row can only go red for the reason it is testing.
func pngOfSize(t *testing.T, n int) []byte {
	t.Helper()
	if n < len(pngBytes) {
		t.Fatalf("row wants %d bytes, too small to carry a PNG signature", n)
	}
	b := make([]byte, n)
	copy(b, pngBytes)
	return b
}

// feedImagePurpose runs one value through the REAL bundle-field validator for
// that purpose. Returns nil when the field admits the value.
func feedImagePurpose(t *testing.T, purpose, value string) error {
	t.Helper()
	switch purpose {
	case "avatar":
		// A SINGLETON kind: T-cd6f moved member / outsource images into
		// avatarPools, so "member" is no longer an admissible `avatars` key and
		// probing with it would fail on the kind, never reaching the cap.
		m := map[string]string{"assistant": value}
		return validateAvatars(&m, "t")
	case "avatarpool":
		// The pool is the OTHER entry point into the same gate (T-cd6f) — where
		// member / outsource images actually live now. Without this purpose the
		// caps stay twinned only along the singleton path.
		m := map[string][]ThemeIconDTO{"member": {{Image: value}}}
		return validateAvatarPools(&m, "t")
	case "logo":
		return validateLogo(&value, "t")
	case "navicon":
		m := map[string]string{"office": value}
		return validateNavIcons(&m, "t")
	case "background":
		m := map[string]string{"canvas": value}
		return validateBackgrounds(&m, "t")
	}
	t.Fatalf("unknown purpose %q — the table and this switch have drifted", purpose)
	return nil
}

// TestImageCaps_MatchTheSharedTable — the confrontation.
func TestImageCaps_MatchTheSharedTable(t *testing.T) {
	rows, decls := loadImageCapCases(t)

	// The four thresholds are ON the table, so these constants are not a third
	// copy of themselves. Each names ITSELF in the failure, so a drift says
	// which of the four moved and on which side.
	for _, c := range []struct {
		name  string
		got   int
		table int
	}{
		{"maxAvatarBytes", maxAvatarBytes, decls.avatarBytes},
		{"maxAvatarValueLen", maxAvatarValueLen, decls.avatarValueLen},
		{"maxBackgroundBytes", maxBackgroundBytes, decls.backgroundBytes},
		{"maxBackgroundValueLen", maxBackgroundValueLen, decls.backgroundValueLen},
	} {
		if c.got != c.table {
			t.Errorf("SERVER SIDE DRIFTED: server/ocserverd/avatar_bundle.go %s = %d, the shared table %s says %d — the cockpit's frontend/src/lib/themeBundleCore.ts still follows the table, so the two now disagree about which images are accepted",
				c.name, c.got, imageCapCasesPath, c.table)
		}
	}

	// The whole point of T-72da is that the two purposes DIFFER. If a later
	// tidy-up collapses them back into one number every row below still passes
	// for one of the two purposes, so the table's own premise must fail loudly.
	if decls.avatarBytes == decls.backgroundBytes {
		t.Fatal("the shared table gives avatars and backgrounds the SAME decoded cap — the per-purpose split (owner ruling 2026-08-03) has been undone and these rows can no longer tell the two gates apart")
	}

	sawAvatarSide, sawBackgroundSide := false, false
	for _, r := range rows {
		value := dataURI("image/png", pngOfSize(t, r.decoded))
		err := feedImagePurpose(t, r.purpose, value)
		got := err == nil
		if got != r.accepted {
			t.Errorf("%s:%d %s: %s field %s a %d-byte image, the shared table says it must %s it (err=%v) — server/ocserverd has drifted from the shared rule",
				imageCapCasesPath, r.line, r.name, r.purpose,
				map[bool]string{true: "ACCEPTED", false: "REFUSED"}[got], r.decoded,
				map[bool]string{true: "accept", false: "refuse"}[r.accepted], err)
		}
		// A row that is over the avatar cap but accepted can only be a
		// background; a row at the same size refused can only be a glyph field.
		if r.decoded > decls.avatarBytes && r.accepted {
			sawBackgroundSide = true
		}
		if r.decoded > decls.avatarBytes && !r.accepted {
			sawAvatarSide = true
		}
	}
	// Without BOTH, the table could not tell a split gate from a single one:
	// only-relaxed rows pass with every cap raised to 512 KiB (the avatars would
	// have been relaxed too, the exact thing the split exists to prevent), and
	// only-refused rows pass with nothing relaxed at all.
	if !sawBackgroundSide {
		t.Fatal("no row proves the background cap was actually RAISED — every row past the avatar cap is a refusal, so this table would pass unchanged against the pre-T-72da single gate")
	}
	if !sawAvatarSide {
		t.Fatal("no row proves a GLYPH field still refuses a background-sized image — so this table would pass with all four caps raised, i.e. with avatars relaxed too")
	}
}
