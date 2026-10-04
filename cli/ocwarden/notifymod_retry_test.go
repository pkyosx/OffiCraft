package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadHooksModulesFlag(t *testing.T) {
	// 2026-10-02T02:59:00Z, five minutes before the read below.
	const at = `"cachedGrowthBookFeaturesAt":1790909940000`
	now := time.Date(2026, 10, 2, 3, 4, 0, 0, time.UTC)
	const stamp = " cachedGrowthBookFeaturesAt=2026-10-02T02:59:00Z (5m0s before this read)"
	const path = "/Users/wardenowner/.claude.json"
	for _, tc := range []struct {
		name  string
		raw   string
		err   error
		value string
		want  string
	}{
		{"cached true", `{"cachedGrowthBookFeatures":{"tengu_plugin_hooks_modules":true,"other":1},` + at + `}`,
			nil, "true", path + " tengu_plugin_hooks_modules=true" + stamp},
		{"cached false", `{"cachedGrowthBookFeatures":{"tengu_plugin_hooks_modules":false},` + at + `}`,
			nil, "false", path + " tengu_plugin_hooks_modules=false" + stamp},
		{"no file", "", os.ErrNotExist, "absent", path + " tengu_plugin_hooks_modules=absent (no such file)"},
		{"a file that cannot be read", "", errors.New("permission denied"),
			"unreadable", path + " tengu_plugin_hooks_modules=unreadable (permission denied)"},
		{"no feature cache", `{"projects":{}}`, nil, "absent",
			path + " tengu_plugin_hooks_modules=absent (no cachedGrowthBookFeatures) cachedGrowthBookFeaturesAt=absent"},
		{"a feature cache without the flag", `{"cachedGrowthBookFeatures":{"other":true},` + at + `}`,
			nil, "absent", path + " tengu_plugin_hooks_modules=absent" + stamp},
		{"malformed JSON", `{"cachedGrowthBookFeatures":{"tengu_plugin_hooks_modules":tr`, nil,
			"unreadable", path + " tengu_plugin_hooks_modules=unreadable (not a JSON object)"},
		{"a JSON array", `[1,2]`, nil, "unreadable", path + " tengu_plugin_hooks_modules=unreadable (not a JSON object)"},
		{"a feature cache that is not an object", `{"cachedGrowthBookFeatures":"x"}`, nil, "unreadable",
			path + " tengu_plugin_hooks_modules=unreadable (cachedGrowthBookFeatures is not an object) cachedGrowthBookFeaturesAt=absent"},
		{"a stamp that is not a number is shown as it is",
			`{"cachedGrowthBookFeatures":{"tengu_plugin_hooks_modules":true},"cachedGrowthBookFeaturesAt":"yesterday"}`,
			nil, "true", path + ` tengu_plugin_hooks_modules=true cachedGrowthBookFeaturesAt="yesterday"`},
		{"an odd value is compacted and capped",
			`{"cachedGrowthBookFeatures":{"tengu_plugin_hooks_modules":{"rule":  "` + strings.Repeat("x", 80) + `"}}}`,
			nil, "…" + strings.Repeat("x", 62) + `"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var asked []string
			d := SpawnDeps{
				ClaudeHome: claudeHome{Home: "/Users/wardenowner"},
				Now:        func() time.Time { return now },
				ReadFile: func(p string) ([]byte, error) {
					asked = append(asked, p)
					return []byte(tc.raw), tc.err
				},
			}
			got := d.readHooksModulesFlag()
			if got.Value != tc.value {
				t.Errorf("value = %q, want %q", got.Value, tc.value)
			}
			if tc.want != "" && got.String() != tc.want {
				t.Errorf("rendered =\n%s\nwant\n%s", got, tc.want)
			}
			if len(asked) != 1 || asked[0] != path {
				t.Errorf("read %v, want exactly %s", asked, path)
			}
		})
	}

	t.Run("a redirected config home is the file read, because the launch line exports it", func(t *testing.T) {
		var asked string
		d := SpawnDeps{
			ClaudeHome: claudeHome{Home: "/Users/wardenowner", ConfigDir: "/Volumes/cfg"},
			ReadFile:   func(p string) ([]byte, error) { asked = p; return nil, os.ErrNotExist },
		}
		d.readHooksModulesFlag()
		if asked != "/Volumes/cfg/.claude.json" {
			t.Errorf("read %q, want /Volumes/cfg/.claude.json", asked)
		}
	})

	t.Run("with no ReadFile seam the real file is read and left byte-for-byte and mtime unchanged", func(t *testing.T) {
		home := t.TempDir()
		file := filepath.Join(home, ".claude.json")
		body := []byte(`{"cachedGrowthBookFeatures":{"tengu_plugin_hooks_modules":false}}`)
		if err := os.WriteFile(file, body, 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(file, old, old); err != nil {
			t.Fatal(err)
		}
		d := SpawnDeps{ClaudeHome: claudeHome{Home: home}}
		if got := d.readHooksModulesFlag(); got.Value != "false" {
			t.Errorf("value = %q, want false", got.Value)
		}
		after, err := os.ReadFile(file)
		if err != nil || string(after) != string(body) {
			t.Errorf("file after the read = %q (%v), want it unchanged", after, err)
		}
		if st, err := os.Stat(file); err != nil || !st.ModTime().Equal(old) {
			t.Errorf("mtime moved: %v %v", st.ModTime(), err)
		}
	})
}

func TestNotifyModRetriedNote(t *testing.T) {
	got := notifyModRetriedNote(hooksModulesFlag{Value: "false"}, hooksModulesFlag{Value: "true"})
	want := "warden 已自動重啟 Claude Code 再試一次，仍沒有載入（啟動前 Claude Code 快取的 tengu_plugin_hooks_modules：第 1 次 false，第 2 次 true）。"
	if got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}
