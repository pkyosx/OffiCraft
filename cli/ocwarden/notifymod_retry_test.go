package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

func TestNotifyModNotLoadedReason(t *testing.T) {
	t.Run("under a restart, the reason names both cached flag values", func(t *testing.T) {
		got := notifyModNotLoadedReason([]hooksModulesFlag{{Value: "false"}, {Value: "true"}})
		want := "notify_mod_not_loaded: 通知模組沒有載入，成員收不到 OffiCraft 訊息，已停止上線。" +
			"已自動重啟 Claude Code 一次仍沒載入；啟動前快取的開關：第 1 次 false、第 2 次 true。" +
			"常見原因：工作目錄未信任、disableAllHooks、--safe-mode、受管設定擋掉 --plugin-dir。"
		if got != want {
			t.Errorf("reason =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("under one attempt, the reason has no restart sentence", func(t *testing.T) {
		got := notifyModNotLoadedReason([]hooksModulesFlag{{Value: "false"}})
		want := "notify_mod_not_loaded: 通知模組沒有載入，成員收不到 OffiCraft 訊息，已停止上線。" +
			"常見原因：工作目錄未信任、disableAllHooks、--safe-mode、受管設定擋掉 --plugin-dir。"
		if got != want {
			t.Errorf("reason =\n%q\nwant\n%q", got, want)
		}
	})
}

// The station truncates last_op_reason at commandResultReasonMax bytes, read
// here from the server source so the two cannot drift apart.
func TestNotifyReasonsFitStationReasonCap(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "server", "ocserverd", "api_monitoring.go"))
	if err != nil {
		t.Fatalf("read the station's cap: %v", err)
	}
	m := regexp.MustCompile(`(?m)^const commandResultReasonMax = (\d+)$`).FindSubmatch(src)
	if m == nil {
		t.Fatal("commandResultReasonMax not found in server/ocserverd/api_monitoring.go")
	}
	limit, _ := strconv.Atoi(string(m[1]))
	// The longest values the flag read can yield: a capped JSON value, and the
	// longest of its own words.
	capped := hooksModulesFlag{Value: "…" + strings.Repeat("x", hooksModulesFlagValueCap)}
	unreadable := hooksModulesFlag{Value: "unreadable"}
	reasons := map[string]string{
		"not loaded, no restart":               notifyModNotLoadedReason([]hooksModulesFlag{unreadable}),
		"not loaded after restart, unreadable": notifyModNotLoadedReason([]hooksModulesFlag{unreadable, unreadable}),
		"not loaded after restart, capped":     notifyModNotLoadedReason([]hooksModulesFlag{capped, capped}),
		"too old, long version":                notifyClaudeTooOldReason(strings.Repeat("9", 16) + "." + strings.Repeat("9", 16)),
	}
	for name, reason := range reasons {
		if len(reason) > limit {
			t.Errorf("%s: %d bytes > the station's %d, so the owner sees it cut off: %q", name, len(reason), limit, reason)
		}
	}
}
