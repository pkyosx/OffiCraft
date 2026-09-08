// Skeleton generated from server/ocserverd/theme_name_verdicts.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestThemeBundleNamed(t *testing.T) {
	got := themeBundleNamed("Midnight")
	want := []ThemeBundleDTO{{
		Id:     "midnight",
		Name:   "Midnight",
		Colors: map[string]string{"--color-bg": "#101018"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("themeBundleNamed returned %#v, want %#v", got, want)
	}
}

func TestCmdThemeNameVerdicts(t *testing.T) {
	t.Run("valid arguments write one verdict per case with restrictive file permissions", func(t *testing.T) {
		dir := t.TempDir()
		casesPath := filepath.Join(dir, "cases.json")
		verdictsPath := filepath.Join(dir, "verdicts.json")
		cases := `[{"k":"clean","n":"Midnight"},{"k":"hidden","n":"Mid\u200Bnight"},{"k":"empty","n":" \u3000 "},{"k":"spaces","n":"Deep\u3000Sea"}]`
		if err := os.WriteFile(casesPath, []byte(cases), 0o600); err != nil {
			t.Fatalf("write cases: %v", err)
		}
		var output bytes.Buffer
		if got := cmdThemeNameVerdicts([]string{casesPath, verdictsPath}, &output); got != 0 {
			t.Fatalf("cmdThemeNameVerdicts returned %d, want 0 (%s)", got, output.String())
		}
		want := `{"clean":"ACCEPT","empty":"REJECT: name must be 1..80 characters after trimming","hidden":"REJECT: name must not contain control, formatting, private-use, surrogate or line/paragraph separator characters","spaces":"ACCEPT"}`
		contents, err := os.ReadFile(verdictsPath)
		if err != nil {
			t.Fatalf("read verdicts: %v", err)
		}
		if string(contents) != want {
			t.Fatalf("verdicts = %q, want %q", contents, want)
		}
		info, err := os.Stat(verdictsPath)
		if err != nil {
			t.Fatalf("stat verdicts: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("verdict permissions = %o, want 600", got)
		}
		if !strings.Contains(output.String(), "wrote "+verdictsPath+": 4 verdict(s)") {
			t.Fatalf("success output = %q, want the written path and count", output.String())
		}
	})

	t.Run("wrong argument count prints usage and does not write a file", func(t *testing.T) {
		var output bytes.Buffer
		if got := cmdThemeNameVerdicts(nil, &output); got != 2 {
			t.Fatalf("wrong-argument result = %d, want 2", got)
		}
		want := "usage: ocserverd theme-name-verdicts <cases.json> <verdicts.json>\n" +
			"  cases.json    [{\"k\":\"<key>\",\"n\":\"<name>\"}, …] — the shared corpus\n" +
			"  verdicts.json written: {\"<key>\": \"ACCEPT\"|\"REJECT: <reason>\", …}\n" +
			"Driven by frontend/src/lib/themeName.parity.test.ts.\n"
		if output.String() != want {
			t.Fatalf("usage output = %q, want %q", output.String(), want)
		}
	})

	for _, tc := range []struct {
		name       string
		input      string
		wantText   string
		wantResult int
	}{
		{name: "missing cases file", input: "", wantResult: 1},
		{name: "malformed cases JSON", input: "not-json", wantResult: 1},
		{name: "empty corpus", input: "[]", wantResult: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			casesPath := filepath.Join(dir, "cases.json")
			verdictsPath := filepath.Join(dir, "verdicts.json")
			if tc.input != "" {
				if err := os.WriteFile(casesPath, []byte(tc.input), 0o600); err != nil {
					t.Fatalf("write cases: %v", err)
				}
			}
			var output bytes.Buffer
			if got := cmdThemeNameVerdicts([]string{casesPath, verdictsPath}, &output); got != tc.wantResult {
				t.Fatalf("cmdThemeNameVerdicts result = %d, want %d (%s)", got, tc.wantResult, output.String())
			}
			if _, err := os.Stat(verdictsPath); !os.IsNotExist(err) {
				t.Fatalf("failure wrote verdicts: err=%v", err)
			}
		})
	}

	t.Run("a destination that cannot be created reports a write failure", func(t *testing.T) {
		dir := t.TempDir()
		casesPath := filepath.Join(dir, "cases.json")
		if err := os.WriteFile(casesPath, []byte(`[{"k":"clean","n":"Midnight"}]`), 0o600); err != nil {
			t.Fatalf("write cases: %v", err)
		}
		var output bytes.Buffer
		missingParent := filepath.Join(dir, "missing", "verdicts.json")
		if got := cmdThemeNameVerdicts([]string{casesPath, missingParent}, &output); got != 1 {
			t.Fatalf("write failure result = %d, want 1", got)
		}
		if !strings.Contains(output.String(), "[theme-name-verdicts] write "+missingParent+":") {
			t.Fatalf("write failure output = %q", output.String())
		}
	})
}
