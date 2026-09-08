// Skeleton generated from server/ocserverd/assets.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

func TestSeedsdistFS(t *testing.T) {
	data, err := fs.ReadFile(seedsdistFS(), "role_def_assistant.md")
	if err != nil {
		t.Fatalf("ReadFile(role_def_assistant.md): %v", err)
	}
	if got := string(data); got != apiTestAssistantSeedDefinitionMD {
		t.Fatalf("assistant seed = %q, want %q", got, apiTestAssistantSeedDefinitionMD)
	}
}

func TestBindistFS(t *testing.T) {
	for _, name := range []string{"ocwarden", "ocagent", "officraft", "mcp-catalog.json"} {
		data, err := fs.ReadFile(bindistFS(), name)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("ReadFile(%q) returned an empty asset", name)
		}
	}
}

func TestDocsdistFS(t *testing.T) {
	data, err := fs.ReadFile(docsdistFS(), "quickstart.md")
	if err != nil {
		t.Fatalf("ReadFile(quickstart.md): %v", err)
	}
	if !strings.Contains(string(data), "#") {
		t.Fatalf("quickstart.md has no heading: %q", data)
	}
}

func TestSeedRoleName(t *testing.T) {
	for _, tc := range []struct {
		roleKey string
		want    string
	}{
		{roleKey: "assistant", want: "Assistant"},
		{roleKey: "engineer"},
		{roleKey: ""},
	} {
		t.Run(tc.roleKey, func(t *testing.T) {
			if got := seedRoleName(tc.roleKey); got != tc.want {
				t.Fatalf("seedRoleName(%q) = %q, want %q", tc.roleKey, got, tc.want)
			}
		})
	}
}

func TestSeedBlockMD(t *testing.T) {
	root := assetRoot("unused by embedded reads")
	text, hasSeed, err := root.seedBlockMD("role_def_assistant.md")
	if err != nil {
		t.Fatalf("seedBlockMD(existing): %v", err)
	}
	if !hasSeed || text != apiTestAssistantSeedDefinitionMD {
		t.Fatalf("seedBlockMD(existing) = (%q, %v), want the assistant seed and true", text, hasSeed)
	}

	text, hasSeed, err = root.seedBlockMD("does-not-exist.md")
	if err != nil {
		t.Fatalf("seedBlockMD(missing): %v", err)
	}
	if text != "" || hasSeed {
		t.Fatalf("seedBlockMD(missing) = (%q, %v), want empty and false", text, hasSeed)
	}
}

func TestSeedRoleDefinitionMD(t *testing.T) {
	root := assetRoot("unused by embedded reads")
	text, hasSeed, err := root.seedRoleDefinitionMD("assistant")
	if err != nil {
		t.Fatalf("seedRoleDefinitionMD(assistant): %v", err)
	}
	if !hasSeed || text != apiTestAssistantSeedDefinitionMD {
		t.Fatalf("seedRoleDefinitionMD(assistant) = (%q, %v), want the assistant seed and true", text, hasSeed)
	}

	text, hasSeed, err = root.seedRoleDefinitionMD("engineer")
	if err != nil {
		t.Fatalf("seedRoleDefinitionMD(engineer): %v", err)
	}
	if text != "" || hasSeed {
		t.Fatalf("seedRoleDefinitionMD(engineer) = (%q, %v), want empty and false", text, hasSeed)
	}
}

func TestSeedInsightMDFrom(t *testing.T) {
	embedded := fstest.MapFS{
		"insight_assistant.md": &fstest.MapFile{Data: []byte("assistant insight")},
		"insight_engineer.md":  &fstest.MapFile{Data: []byte("engineer insight")},
	}
	root := assetRoot("unused by embedded reads")
	for _, tc := range []struct {
		roleKey string
		want    string
	}{
		{roleKey: "assistant", want: "assistant insight"},
		{roleKey: "engineer", want: "engineer insight"},
	} {
		t.Run(tc.roleKey, func(t *testing.T) {
			text, hasSeed, err := root.seedInsightMDFrom(tc.roleKey, embedded)
			if err != nil {
				t.Fatalf("seedInsightMDFrom(%q): %v", tc.roleKey, err)
			}
			if !hasSeed || text != tc.want {
				t.Fatalf("seedInsightMDFrom(%q) = (%q, %v), want (%q, true)",
					tc.roleKey, text, hasSeed, tc.want)
			}
		})
	}

	for _, roleKey := range []string{"missing", "../assistant", "assistant/other"} {
		text, hasSeed, err := root.seedInsightMDFrom(roleKey, embedded)
		if err != nil {
			t.Fatalf("seedInsightMDFrom(%q): %v", roleKey, err)
		}
		if text != "" || hasSeed {
			t.Fatalf("seedInsightMDFrom(%q) = (%q, %v), want empty and false", roleKey, text, hasSeed)
		}
	}
}

func TestSafeSeedRoleKey(t *testing.T) {
	for _, tc := range []struct {
		roleKey string
		want    bool
	}{
		{roleKey: "assistant", want: true},
		{roleKey: "A-1_2", want: true},
		{roleKey: "", want: false},
		{roleKey: "../assistant", want: false},
		{roleKey: "assistant/other", want: false},
		{roleKey: "assistant space", want: false},
		{roleKey: "助理", want: false},
		{roleKey: "assistant\x00", want: false},
	} {
		t.Run(tc.roleKey, func(t *testing.T) {
			if got := safeSeedRoleKey(tc.roleKey); got != tc.want {
				t.Fatalf("safeSeedRoleKey(%q) = %v, want %v", tc.roleKey, got, tc.want)
			}
		})
	}
}

func TestResolveBootRoleKey(t *testing.T) {
	member := &Member{RoleKey: "engineer"}
	for _, tc := range []struct {
		name   string
		role   string
		member *Member
		want   string
	}{
		{name: "explicit role wins", role: "designer", member: member, want: "designer"},
		{name: "member role is fallback", member: member, want: "engineer"},
		{name: "empty member role uses default", member: &Member{}, want: defaultBootRole},
		{name: "nil member uses default", want: defaultBootRole},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBootRoleKey(tc.role, tc.member); got != tc.want {
				t.Fatalf("resolveBootRoleKey(%q, %#v) = %q, want %q",
					tc.role, tc.member, got, tc.want)
			}
		})
	}
}

func TestFoldRoleDefDTO(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	got, err := api.foldRoleDefDTO("assistant")
	if err != nil {
		t.Fatalf("foldRoleDefDTO(seed): %v", err)
	}
	if got == nil {
		t.Fatal("foldRoleDefDTO(seed) = nil")
	}
	if got.Key != "assistant" || got.Name != "Assistant" ||
		got.DefinitionMD != apiTestAssistantSeedDefinitionMD || !got.IsDefault || !got.IsSeed ||
		got.SizeChars != utf8.RuneCountInString(apiTestAssistantSeedDefinitionMD) ||
		got.CapChars != api.dutyCap() || got.OwnerID != wireOwnerID ||
		got.SchemaVersion != wireSchemaVersion {
		t.Fatalf("seed role dto = %#v", got)
	}

	if err := d.PutRoleDef(RoleDef{
		RoleKey: "assistant", Name: "Custom Assistant", DefinitionMD: "# Custom\n" + "do this",
	}); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	got, err = api.foldRoleDefDTO("assistant")
	if err != nil {
		t.Fatalf("foldRoleDefDTO(overlay): %v", err)
	}
	if got == nil || got.Name != "Custom Assistant" || got.DefinitionMD != "# Custom\ndo this" ||
		got.IsDefault || !got.IsSeed || got.SizeChars != utf8.RuneCountInString(got.DefinitionMD) {
		t.Fatalf("overlay role dto = %#v", got)
	}

	got, err = api.foldRoleDefDTO("unknown")
	if err != nil {
		t.Fatalf("foldRoleDefDTO(unknown): %v", err)
	}
	if got != nil {
		t.Fatalf("foldRoleDefDTO(unknown) = %#v, want nil", got)
	}
}

func TestFoldLessonsDTO(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	got, err := api.foldLessonsDTO("assistant")
	if err != nil {
		t.Fatalf("foldLessonsDTO(seed): %v", err)
	}
	if got == nil || got.RoleKey != "assistant" || got.Text != apiTestLessonsSeedText || !got.IsDefault ||
		got.SizeChars != utf8.RuneCountInString(apiTestLessonsSeedText) ||
		got.CapChars != api.learningCap() || got.OwnerID != wireOwnerID ||
		got.SchemaVersion != wireSchemaVersion {
		t.Fatalf("seed lessons dto = %#v", got)
	}

	if err := d.PutLessons(Lessons{RoleKey: "assistant", Text: "learned facts"}); err != nil {
		t.Fatalf("PutLessons: %v", err)
	}
	got, err = api.foldLessonsDTO("assistant")
	if err != nil {
		t.Fatalf("foldLessonsDTO(overlay): %v", err)
	}
	if got == nil || got.Text != "learned facts" || got.IsDefault || got.SizeChars != 13 {
		t.Fatalf("overlay lessons dto = %#v", got)
	}
}

func TestFoldUserContextDTO(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	got, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatalf("foldUserContextDTO(default): %v", err)
	}
	if got == nil || got.Text != "" || !got.IsDefault || got.OwnerID != wireOwnerID ||
		got.SchemaVersion != wireSchemaVersion || got.OrgName != api.orgNameSnapshot() {
		t.Fatalf("default user context dto = %#v", got)
	}

	if err := d.PutUserContext(UserContext{Text: "studio rule"}); err != nil {
		t.Fatalf("PutUserContext: %v", err)
	}
	got, err = api.foldUserContextDTO()
	if err != nil {
		t.Fatalf("foldUserContextDTO(overlay): %v", err)
	}
	if got == nil || got.Text != "studio rule" || got.IsDefault {
		t.Fatalf("overlay user context dto = %#v", got)
	}
}

func TestBootSequenceSeedName(t *testing.T) {
	for _, tc := range []struct {
		runtime string
		want    string
	}{
		{runtime: "", want: bootSequenceSeedClaude},
		{runtime: RuntimeClaude, want: bootSequenceSeedClaude},
		{runtime: RuntimeCodex, want: bootSequenceSeedCodex},
		{runtime: "opus", want: bootSequenceSeedClaude},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			if got := bootSequenceSeedName(tc.runtime); got != tc.want {
				t.Fatalf("bootSequenceSeedName(%q) = %q, want %q", tc.runtime, got, tc.want)
			}
		})
	}
}

func TestBootSequenceDocKey(t *testing.T) {
	for _, tc := range []struct {
		runtime string
		want    string
	}{
		{runtime: "", want: bootSequenceKeyClaude},
		{runtime: RuntimeClaude, want: bootSequenceKeyClaude},
		{runtime: RuntimeCodex, want: bootSequenceKeyCodex},
		{runtime: "opus", want: bootSequenceKeyClaude},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			if got := bootSequenceDocKey(tc.runtime); got != tc.want {
				t.Fatalf("bootSequenceDocKey(%q) = %q, want %q", tc.runtime, got, tc.want)
			}
		})
	}
}

func TestBootSequenceSeedForKey(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want string
		ok   bool
	}{
		{key: bootSequenceKeyClaude, want: bootSequenceSeedClaude, ok: true},
		{key: bootSequenceKeyCodex, want: bootSequenceSeedCodex, ok: true},
		{key: "", ok: false},
		{key: "Codex", ok: false},
		{key: "opus", ok: false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			got, ok := bootSequenceSeedForKey(tc.key)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("bootSequenceSeedForKey(%q) = (%q, %v), want (%q, %v)",
					tc.key, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestBuildBootContext(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	readSeed := func(name string) string {
		t.Helper()
		data, err := fs.ReadFile(seedsdistFS(), name)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", name, err)
		}
		return strings.ReplaceAll(string(data), ownerPlaceholder, wireOwnerID)
	}
	wantContext := func(userText string) string {
		parts := []string{strings.TrimSpace(readSeed(systemInteractionSeedMD))}
		if strings.TrimSpace(userText) != "" {
			parts = append(parts, userAdditionsTitle+"\n\n"+strings.TrimSpace(userText))
		}
		parts = append(parts,
			"# Role: Assistant\n\n"+strings.TrimSpace(apiTestAssistantSeedDefinitionMD),
			"# Insight (assistant)\n\n"+strings.TrimSpace(readSeed("insight_assistant.md")),
			"# Lessons (assistant)\n\n"+strings.TrimSpace(apiTestLessonsSeedText),
			strings.TrimSpace(readSeed(bootSequenceSeedClaude)))
		return strings.Join(parts, "\n\n") + "\n"
	}

	member := &Member{ID: "mira", Name: "Mira", RoleKey: "assistant", Runtime: RuntimeClaude}
	got, err := api.buildBootContext("", member)
	if err != nil {
		t.Fatalf("buildBootContext(default): %v", err)
	}
	if got == nil || got.RoleKey != "assistant" || got.Name != "Mira" || got.Context != wantContext("") {
		t.Fatalf("default boot context = %#v", got)
	}

	if err := d.PutUserContext(UserContext{Text: "studio rule"}); err != nil {
		t.Fatalf("PutUserContext: %v", err)
	}
	got, err = api.buildBootContext("", member)
	if err != nil {
		t.Fatalf("buildBootContext(user context): %v", err)
	}
	if got == nil || got.Context != wantContext("studio rule") {
		t.Fatalf("boot context with user additions = %#v", got)
	}

	got, err = api.buildBootContext("unknown", nil)
	if err != nil {
		t.Fatalf("buildBootContext(unknown): %v", err)
	}
	if got != nil {
		t.Fatalf("buildBootContext(unknown) = %#v, want nil", got)
	}
}

func TestCatalogHashOf(t *testing.T) {
	got := catalogHashOf([]RouteSpec{
		{Method: "POST", Path: "/z"},
		{Method: "GET", Path: "/b"},
		{Method: "DELETE", Path: "/excluded", MCPExclude: true},
		{Method: "POST", Path: "/a"},
		{Method: "GET", Path: "/a"},
	})
	if got != "d74963713415145b" {
		t.Fatalf("catalogHashOf = %q, want %q", got, "d74963713415145b")
	}
}

func TestBinHashPrefix(t *testing.T) {
	if got := binHashPrefix([]byte("binary payload")); got != "ba8f38fbdbe5" {
		t.Fatalf("binHashPrefix = %q, want %q", got, "ba8f38fbdbe5")
	}
	if got := len(binHashPrefix(nil)); got != binHashPrefixLen {
		t.Fatalf("binHashPrefix(nil) length = %d, want %d", got, binHashPrefixLen)
	}
}

func TestBindistBinaryHashesFrom(t *testing.T) {
	got := bindistBinaryHashesFrom(fstest.MapFS{
		"ocwarden": &fstest.MapFile{Data: []byte("warden")},
		"ocagent":  &fstest.MapFile{Data: []byte("agent")},
	})
	want := map[string]string{
		"ocwarden": "8bdb247a2a76",
		"ocagent":  "d4f0bc5a29de",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindistBinaryHashesFrom = %#v, want %#v", got, want)
	}

	got = bindistBinaryHashesFrom(fstest.MapFS{
		"ocwarden": &fstest.MapFile{},
	})
	if len(got) != 0 {
		t.Fatalf("bindistBinaryHashesFrom(empty/missing) = %#v, want empty", got)
	}
}

func TestMaterializeBinary(t *testing.T) {
	dir := t.TempDir()
	path, err := materializeBinary(dir, "ocagent", []byte("v1"))
	if err != nil {
		t.Fatalf("materializeBinary(first): %v", err)
	}
	if path != filepath.Join(dir, "ocagent") {
		t.Fatalf("materializeBinary path = %q, want %q", path, filepath.Join(dir, "ocagent"))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(materialized): %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("materialized mode = %o, want 755", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(materialized): %v", err)
	}
	if string(data) != "v1" {
		t.Fatalf("materialized data = %q, want %q", data, "v1")
	}

	samePath, err := materializeBinary(dir, "ocagent", []byte("v1"))
	if err != nil {
		t.Fatalf("materializeBinary(identical): %v", err)
	}
	if samePath != path {
		t.Fatalf("identical materialization path = %q, want %q", samePath, path)
	}

	if _, err := materializeBinary(dir, "ocagent", []byte("v2")); err != nil {
		t.Fatalf("materializeBinary(replacement): %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(replacement): %v", err)
	}
	if string(data) != "v2" {
		t.Fatalf("replacement data = %q, want %q", data, "v2")
	}
}
