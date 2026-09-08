// Skeleton generated from server/ocserverd/wording_bundle.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
	"fmt"
	"log"
	"reflect"
	"strings"
	"testing"
)

func TestValidateWording(t *testing.T) {
	if err := validateWording(nil, "theme[0]"); err != nil {
		t.Fatalf("nil wording must be accepted: %v", err)
	}

	valid := map[string]map[string]string{
		"zh": {"common.apply": "套用"},
		"en": {"common.apply": "Apply"},
	}
	if err := validateWording(&valid, "theme[0]"); err != nil {
		t.Fatalf("valid wording must be accepted: %v", err)
	}

	unknown := map[string]map[string]string{
		"zh": {"common.apply": "套用", "old.themeName": "舊名稱"},
	}
	if err := validateWording(&unknown, "theme[0]"); err != nil {
		t.Fatalf("unknown wording codes must be dropped rather than rejected: %v", err)
	}
	wantUnknown := map[string]map[string]string{"zh": {"common.apply": "套用"}}
	if !reflect.DeepEqual(unknown, wantUnknown) {
		t.Fatalf("validated wording = %#v, want %#v", unknown, wantUnknown)
	}

	for _, tc := range []struct {
		name string
		word map[string]map[string]string
		want string
	}{
		{
			name: "unsupported language",
			word: map[string]map[string]string{"ja": {"common.apply": "適用"}},
			want: `theme[0]: wording language "ja" is not allowed (only zh, en)`,
		},
		{
			name: "control character",
			word: map[string]map[string]string{"zh": {"common.apply": "套\n用"}},
			want: `theme[0]: wording[zh][common.apply] must not contain control characters`,
		},
		{
			name: "empty after trimming",
			word: map[string]map[string]string{"zh": {"common.apply": "   "}},
			want: `theme[0]: wording[zh][common.apply] must be 1..200 characters after trimming`,
		},
		{
			name: "more than two hundred runes",
			word: map[string]map[string]string{"zh": {"common.apply": strings.Repeat("字", 201)}},
			want: `theme[0]: wording[zh][common.apply] must be 1..200 characters after trimming`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateWording(&tc.word, "theme[0]"); err == nil || err.Error() != tc.want {
				t.Fatalf("validateWording error = %v, want %q", err, tc.want)
			}
		})
	}

	entries := make(map[string]string, 2001)
	for i := 0; i < 2001; i++ {
		entries[fmt.Sprintf("unknown.%04d", i)] = "x"
	}
	overCap := map[string]map[string]string{"zh": entries}
	if err := validateWording(&overCap, "theme[0]"); err == nil || err.Error() != `theme[0]: wording[zh] holds more than 2000 entries` {
		t.Fatalf("over-cap wording error = %v", err)
	}
}

func TestDropUnknownWordingCodes(t *testing.T) {
	var output bytes.Buffer
	previousWriter, previousFlags := log.Writer(), log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	wording := map[string]map[string]string{
		"zh": {
			"common.apply":   "套用",
			"old.themeName":  "舊名稱",
			"typo.not.a.key": "拼字錯誤",
		},
		"en": {"obsolete.code": "obsolete"},
	}
	dropUnknownWordingCodes(wording, "theme[0]")
	want := map[string]map[string]string{"zh": {"common.apply": "套用"}, "en": {}}
	if !reflect.DeepEqual(wording, want) {
		t.Fatalf("wording after dropping unknown codes = %#v, want %#v", wording, want)
	}
	if got := output.String(); got != `[theme] theme[0]: dropped 3 unrecognised wording code(s): "obsolete.code", "old.themeName", "typo.not.a.key"`+"\n" {
		t.Fatalf("drop log = %q, want one sorted log line", got)
	}

	output.Reset()
	forged := "bad\nforged log line"
	long := "long" + strings.Repeat("z", 200)
	wording = map[string]map[string]string{"zh": {forged: "x", long: "y"}}
	dropUnknownWordingCodes(wording, "theme[1]")
	got := output.String()
	if strings.Count(strings.TrimSuffix(got, "\n"), "\n") != 0 {
		t.Fatalf("drop log escaped into more than one line: %q", got)
	}
	if !strings.Contains(got, `\n`) || strings.Contains(got, "forged log line\n") {
		t.Fatalf("drop log did not safely quote a newline-containing code: %q", got)
	}
	if strings.Contains(got, strings.Repeat("z", 85)) {
		t.Fatalf("drop log did not truncate a long code: %q", got)
	}

	output.Reset()
	many := make(map[string]string, 11)
	for i := 0; i < 11; i++ {
		many[fmt.Sprintf("junk.%02d", i)] = "x"
	}
	dropUnknownWordingCodes(map[string]map[string]string{"zh": many}, "theme[2]")
	if got := output.String(); strings.Count(got, "junk.") != 10 || !strings.Contains(got, "dropped 11 ") {
		t.Fatalf("drop log must name ten codes and retain the complete count: %q", got)
	}
}

func TestValidateWordingValue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "ordinary text", value: "文字", want: ""},
		{name: "surrounding whitespace is trimmed for length", value: "  text  ", want: ""},
		{name: "two hundred runes are accepted", value: strings.Repeat("字", 200), want: ""},
		{name: "empty text is refused", value: "", want: "must be 1..200 characters after trimming"},
		{name: "whitespace-only text is refused", value: "   ", want: "must be 1..200 characters after trimming"},
		{name: "two hundred one runes are refused", value: strings.Repeat("字", 201), want: "must be 1..200 characters after trimming"},
		{name: "a newline is refused before trimming", value: "a\nb", want: "must not contain control characters"},
		{name: "a NUL is refused", value: "a\x00b", want: "must not contain control characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWordingValue(tc.value)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("validateWordingValue(%q) = %v, want nil", tc.value, err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("validateWordingValue(%q) = %v, want %q", tc.value, err, tc.want)
			}
		})
	}
}
