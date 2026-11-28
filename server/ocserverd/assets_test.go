package main

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
	for _, name := range []string{
		"darwin-arm64/ocwarden", "darwin-arm64/ocagent", "darwin-arm64/officraft",
		"linux-amd64/ocwarden", "linux-amd64/ocagent", "linux-amd64/officraft",
		"linux-arm64/ocwarden", "linux-arm64/ocagent", "linux-arm64/officraft",
		"mcp-catalog.json",
	} {
		data, err := fs.ReadFile(bindistFS(), name)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("ReadFile(%q) returned an empty asset", name)
		}
	}
}

func TestReadMCPCatalogFrom(t *testing.T) {
	root := assetRoot("a disk path that must not be consulted")
	want := []byte(`{"tools":[]}`)
	embedded := fstest.MapFS{
		"mcp-catalog.json": &fstest.MapFile{Data: want},
	}

	got, err := root.readMCPCatalogFrom(embedded)
	if err != nil {
		t.Fatalf("readMCPCatalogFrom: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readMCPCatalogFrom = %q, want %q", got, want)
	}

	if _, err := root.readMCPCatalogFrom(fstest.MapFS{}); err == nil {
		t.Fatal("readMCPCatalogFrom with no catalog = nil error, want a missing-file error")
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

func TestSeedRoleKeys(t *testing.T) {
	if got := seedRoleKeys(); !reflect.DeepEqual(got, []string{seedRoleAssistant}) {
		t.Fatalf("seedRoleKeys() = %v, want [%q]", got, seedRoleAssistant)
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

func TestSeedInsightMD(t *testing.T) {
	root := assetRoot("unused by embedded reads")
	text, hasSeed, err := root.seedInsightMD(seedRoleAssistant)
	if err != nil {
		t.Fatalf("seedInsightMD(assistant): %v", err)
	}
	if !hasSeed || !strings.Contains(text, "# Intake Point of Contact") {
		t.Fatalf("seedInsightMD(assistant) = (%q, %v), want the embedded assistant insight", text, hasSeed)
	}
	if strings.Contains(text, ownerPlaceholder) {
		t.Fatalf("seedInsightMD(assistant) left the owner placeholder in the returned seed")
	}

	text, hasSeed, err = root.seedInsightMD("engineer")
	if err != nil {
		t.Fatalf("seedInsightMD(engineer): %v", err)
	}
	if text != "" || hasSeed {
		t.Fatalf("seedInsightMD(engineer) = (%q, %v), want empty and false", text, hasSeed)
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

// The User Language defaults and glossary exactly as this build ships them, written out so
// the assembled block is compared with bytes, not with what the code computes.
const (
	apiTestUserLanguageZh = "除非是技術專有名詞可以用英文，其餘預設（包含報告）用繁體中文 (台灣)。"
	apiTestUserLanguageEn = "Use English for everything you write to the user, including reports."
)

const apiTestUserLanguageGlossary = "If the user writes one of the Chinese screen names in this table, it means the English term in the same row.\n\n" +
	"| English (this document) | Chinese on screen |\n" +
	"| --- | --- |\n" +
	"| Office | 辦公室 |\n" +
	"| Reply card／Ask | 請示卡／請示 |\n" +
	"| Task | 任務 |\n" +
	"| Task type | 任務類型 |\n" +
	"| Step | 步驟 |\n" +
	"| Task manuals | 任務手冊 |\n" +
	"| Files | 檔案庫 |\n" +
	"| Secrets | 密鑰 |\n" +
	"| Lore | 傳承 |\n" +
	"| Member lore | 成員傳承 |\n" +
	"| Task lore | 任務傳承 |\n" +
	"| Instruction conflict | 指示衝突 |\n" +
	"| Instruction supplement | 指示補充 |\n" +
	"| Owner decision | Owner 決策 |\n" +
	"| Owner preference | Owner 偏好 |\n" +
	"| Monitor | 監控 |\n" +
	"| Settings | 設定 |\n" +
	"| Global context | 全域情境 |\n" +
	"| System interaction | 系統互動 |\n" +
	"| User additions | 使用者自訂 |\n" +
	"| Boot steps | 啟動步驟 |\n" +
	"| Roles | 角色 |\n" +
	"| Duty | 職責 |\n" +
	"| Insight | 判準 |\n" +
	"| General | 一般設定 |\n" +
	"| Advanced | 進階設定 |\n" +
	"| System update & backup | 系統更新與備份 |\n" +
	"| Theme | 主題 |\n" +
	"| Preferences | 偏好設定 |\n" +
	"| Member | 成員 |\n" +
	"| Staff | 正職 |\n" +
	"| Outsource | 外包 |\n" +
	"| Assistant | 特助 |\n" +
	"| Wake | 喚醒 |\n" +
	"| Refocus | 重新聚焦 |\n" +
	"| Stop | 停止 |\n" +
	"| Accelerated stop | 加速停止 |\n" +
	"| Machines | 機器 |\n" +
	"| Webhook endpoints | 回呼端點 |\n" +
	"| Signing keys | 簽章金鑰 |\n" +
	"| User guide | 使用說明 |"

const (
	apiTestUserLanguageBlockZh = "# User Language\n\n" + apiTestUserLanguageZh + "\n\n" + apiTestUserLanguageGlossary
	apiTestUserLanguageBlockEn = "# User Language\n\n" + apiTestUserLanguageEn + "\n\n" + apiTestUserLanguageGlossary
)

func TestUserLanguageBlock(t *testing.T) {
	block := func(t *testing.T, api *apiServer) string {
		t.Helper()
		got, err := api.userLanguageBlock()
		if err != nil {
			t.Fatalf("userLanguageBlock: %v", err)
		}
		return got
	}
	setLanguage := func(t *testing.T, h http.Handler, owner, lang string) {
		t.Helper()
		if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"display_language":"`+lang+`"}`); status != 200 {
			t.Fatalf("PATCH display_language %q: %d %v", lang, status, data)
		}
	}
	write := func(t *testing.T, h http.Handler, owner, body string) {
		t.Helper()
		if status, data := apiJSON(t, h, "POST", "/api/boot-docs/user_language/global", owner,
			`{"body":`+strconv.Quote(body)+`,"allow_shrink":true}`); status != 200 {
			t.Fatalf("replace user_language/global: %d %v", status, data)
		}
	}

	t.Run("an unedited document assembles the English default when the language is unset or en, and the Chinese default for zh", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if got := block(t, api); got != apiTestUserLanguageBlockEn {
			t.Fatalf("unset:\n%s\nwant:\n%s", got, apiTestUserLanguageBlockEn)
		}
		setLanguage(t, h, owner, "en")
		if got := block(t, api); got != apiTestUserLanguageBlockEn {
			t.Fatalf("en:\n%s\nwant:\n%s", got, apiTestUserLanguageBlockEn)
		}
		setLanguage(t, h, owner, "zh")
		if got := block(t, api); got != apiTestUserLanguageBlockZh {
			t.Fatalf("zh:\n%s\nwant:\n%s", got, apiTestUserLanguageBlockZh)
		}
	})

	t.Run("an edit saved with developer mode off is the block under every language", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		write(t, h, owner, "Reply in Klingon.\n")
		want := "# User Language\n\nReply in Klingon.\n\n" + apiTestUserLanguageGlossary
		for _, lang := range []string{"en", "zh", ""} {
			setLanguage(t, h, owner, lang)
			if got := block(t, api); got != want {
				t.Fatalf("display_language %q:\n%s\nwant:\n%s", lang, got, want)
			}
		}
	})

	t.Run("saving the Chinese default while the language is en reads as the English default", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		write(t, h, owner, "EDITED")
		setLanguage(t, h, owner, "en")
		write(t, h, owner, "\n"+apiTestUserLanguageZh+"\n\n")
		if got := block(t, api); got != apiTestUserLanguageBlockEn {
			t.Fatalf("en:\n%s\nwant:\n%s", got, apiTestUserLanguageBlockEn)
		}
		setLanguage(t, h, owner, "zh")
		if got := block(t, api); got != apiTestUserLanguageBlockZh {
			t.Fatalf("zh:\n%s\nwant:\n%s", got, apiTestUserLanguageBlockZh)
		}
	})

	t.Run("a reset with developer mode off follows the language again", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		write(t, h, owner, "EDITED")
		if status, data := apiJSON(t, h, "POST", "/api/boot-docs/user_language/global/reset", owner, ""); status != 200 {
			t.Fatalf("reset: %d %v", status, data)
		}
		if got := block(t, api); got != apiTestUserLanguageBlockEn {
			t.Fatalf("unset after reset:\n%s\nwant:\n%s", got, apiTestUserLanguageBlockEn)
		}
		setLanguage(t, h, owner, "zh")
		if got := block(t, api); got != apiTestUserLanguageBlockZh {
			t.Fatalf("zh after reset:\n%s\nwant:\n%s", got, apiTestUserLanguageBlockZh)
		}
	})

	t.Run("an emptied document leaves the glossary under the heading", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		write(t, h, owner, "")
		if got, want := block(t, api), "# User Language\n\n"+apiTestUserLanguageGlossary; got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
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
		parts := []string{
			strings.TrimSpace(readSeed(systemInteractionSeedMD)),
			apiTestUserLanguageBlockEn,
		}
		if strings.TrimSpace(userText) != "" {
			parts = append(parts, userAdditionsTitle+"\n\n"+strings.TrimSpace(userText))
		}
		parts = append(parts,
			"# Role: Assistant\n\n"+strings.TrimSpace(apiTestAssistantSeedDefinitionMD),
			"# Insight (assistant)\n\n"+strings.TrimSpace(readSeed("insight_assistant.md")),
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
		"darwin-arm64/ocwarden": &fstest.MapFile{Data: []byte("warden")},
		"darwin-arm64/ocagent":  &fstest.MapFile{Data: []byte("agent")},
		"linux-amd64/ocwarden":  &fstest.MapFile{Data: []byte("binary payload")},
		"mcp-catalog.json":      &fstest.MapFile{Data: []byte("{}")},
	})
	want := map[string]map[string]string{
		"darwin/arm64": {"ocwarden": "8bdb247a2a76", "ocagent": "d4f0bc5a29de"},
		"linux/amd64":  {"ocwarden": "ba8f38fbdbe5"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindistBinaryHashesFrom = %#v, want %#v", got, want)
	}

	got = bindistBinaryHashesFrom(fstest.MapFS{
		"darwin-arm64/ocwarden": &fstest.MapFile{},
		"Darwin-ARM64/ocwarden": &fstest.MapFile{Data: []byte("warden")},
	})
	if len(got) != 0 {
		t.Fatalf("bindistBinaryHashesFrom(empty/missing/not a platform) = %#v, want empty", got)
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
