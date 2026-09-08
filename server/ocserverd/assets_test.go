package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// seedRootWith builds a temp assetRoot whose seeds/ holds the given files.
func seedRootWith(t *testing.T, files map[string]string) assetRoot {
	t.Helper()
	dir := t.TempDir()
	if files != nil {
		if err := os.Mkdir(filepath.Join(dir, "seeds"), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, "seeds", name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return assetRoot(dir)
}

// EMBED-ONLY (T-e731): a stale seeds/*.md sitting under the CWD must never
// shadow the version-locked embed the binary was built with. Disk-first once
// served a frozen repo checkout's stale boot/worker/role/lessons seeds — the
// first crash of the disk-first trilogy. Mirrors serveBinary's "disk copy in
// CWD never shadows the embed" guard.
func TestReadSeedFileEmbedOnlyIgnoresDisk(t *testing.T) {
	root := seedRootWith(t, map[string]string{"boot_sequence.md": "STALE disk copy for {OWNER_ID}"})
	embedded := fstest.MapFS{"boot_sequence.md": {Data: []byte("fresh embedded copy for {OWNER_ID}")}}

	got, err := root.readSeedFileFrom("boot_sequence.md", embedded)
	if err != nil {
		t.Fatalf("readSeedFileFrom: %v", err)
	}
	if got != "fresh embedded copy for owner" {
		t.Fatalf("want the embedded copy to win over the stale on-disk seed (placeholder substituted), got %q", got)
	}
}

func TestReadSeedFileServesEmbed(t *testing.T) {
	root := seedRootWith(t, nil) // no seeds/ on disk at all
	embedded := fstest.MapFS{"boot_sequence.md": {Data: []byte("embedded copy for {OWNER_ID}")}}

	got, err := root.readSeedFileFrom("boot_sequence.md", embedded)
	if err != nil {
		t.Fatalf("readSeedFileFrom: %v", err)
	}
	if got != "embedded copy for owner" {
		t.Fatalf("want the embedded copy with the owner placeholder substituted, got %q", got)
	}
}

func TestReadSeedFileErrsWhenEmbedMiss(t *testing.T) {
	root := seedRootWith(t, nil)

	_, err := root.readSeedFileFrom("boot_sequence.md", fstest.MapFS{})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want fs.ErrNotExist when the embed misses, got %v", err)
	}
}

// 🔴 A TEST WAS DELETED HERE ON 2026-09-07, and the deletion is recorded rather
// than silent so that a future reader can tell "this guard was retired by a
// decision" from "this guard was never written".
//
// It was TestSystemInteractionSeedTeachesTheAdHocTaskRuleTheServerEnforces, and
// it required the shipped 系統互動 seed to carry a 「### 傳承寫入位置」 section
// teaching the rule api_lore.go actually runs. The owner ruled the section out
// of existence (card rc-61fde477ac54 圈 [1], verbatim: 「不就是不要放回去 —— 我
// 就是不要這一節，拿掉守它的那支測試」). A guard whose subject the owner has
// removed has nothing left to guard, so it goes with it.
//
// ⚠️ IT WAS GUARDING TWO THINGS, AND ONLY ONE OF THEM WAS RULED OUT. Its second
// half required a pair of RETIRED sentences — 「沒有你該寫的位置」 and
// 「寫入會被拒絕」 — to be ABSENT. Those describe a refusal the server stopped
// performing on 2026-09-07, and they are actively harmful independent of the
// section: a member reading 「沒有你該寫的位置」 stops before it writes, even
// with correct wording beside it. That protection was INCIDENTAL to the section
// (its search was scoped to the section's body, which no longer exists) and its
// loss was NOT part of the owner's ruling. Measured at the time of deletion:
// both phrases occur 0 times in seeds/system_interaction.md and in
// seedsdist/system_interaction.md — verified with a positive control on the same
// grep, so the zero is an absence and not a broken query. Nothing now stops them
// coming back. Whether to re-guard that, over the WHOLE seed rather than one
// section, is an open question for the owner and deliberately not answered here.

func TestBuildBootContextSelectsRuntimeBootSequence(t *testing.T) {
	s := newWorkerTestServer(t)
	for _, tc := range []struct {
		name    string
		runtime string
		want    string
		absent  string
	}{
		{"claude", RuntimeClaude, "# Claude Code 執行環境", "# Codex App Server 執行環境"},
		{"codex", RuntimeCodex, "# Codex App Server 執行環境", "# Claude Code 執行環境"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boot, err := s.buildBootContext("assistant", &Member{Runtime: tc.runtime})
			if err != nil {
				t.Fatalf("buildBootContext: %v", err)
			}
			if boot == nil || !strings.Contains(boot.Context, tc.want) {
				t.Fatalf("runtime boot context missing %q", tc.want)
			}
			if strings.Contains(boot.Context, tc.absent) {
				t.Fatalf("runtime boot context leaked other runtime tail %q", tc.absent)
			}
		})
	}
}

func TestReadMCPCatalogFrom(t *testing.T) {
	embedded := fstest.MapFS{"mcp-catalog.json": {Data: []byte(`{"tools":["embed"]}`)}}

	// EMBED-ONLY (T-e731): a stale spec/mcp-catalog.json under the CWD must
	// never shadow the embed — disk-first once served a frozen checkout's stale
	// tools/list descriptor surface (the second crash of the trilogy).
	t.Run("ignores a stale on-disk spec/mcp-catalog.json, serves the embed", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "spec"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "spec", "mcp-catalog.json"),
			[]byte(`{"tools":["STALE disk"]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := assetRoot(dir).readMCPCatalogFrom(embedded)
		if err != nil || string(got) != `{"tools":["embed"]}` {
			t.Fatalf("want the embed to win over the stale disk copy, got %q (%v)", got, err)
		}
	})

	t.Run("serves the embed", func(t *testing.T) {
		got, err := assetRoot(t.TempDir()).readMCPCatalogFrom(embedded)
		if err != nil || string(got) != `{"tools":["embed"]}` {
			t.Fatalf("want the embedded copy, got %q (%v)", got, err)
		}
	})

	t.Run("errs when the embed misses", func(t *testing.T) {
		_, err := assetRoot(t.TempDir()).readMCPCatalogFrom(fstest.MapFS{})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("want fs.ErrNotExist, got %v", err)
		}
	})
}

func TestMaterializeBinary(t *testing.T) {
	t.Run("writes an executable file and reuses identical bytes", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "bin")
		got, err := materializeBinary(dir, "ocwarden", []byte("v1"))
		if err != nil {
			t.Fatalf("materializeBinary: %v", err)
		}
		info, err := os.Stat(got)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("want a 0755 file, got %v (%v)", info, err)
		}
		again, err := materializeBinary(dir, "ocwarden", []byte("v1"))
		if err != nil || again != got {
			t.Fatalf("identical bytes must reuse the path: %q (%v)", again, err)
		}
	})

	t.Run("replaces a stale cached binary", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "bin")
		if _, err := materializeBinary(dir, "ocwarden", []byte("v1")); err != nil {
			t.Fatal(err)
		}
		got, err := materializeBinary(dir, "ocwarden", []byte("v2"))
		if err != nil {
			t.Fatalf("materializeBinary: %v", err)
		}
		raw, _ := os.ReadFile(got)
		if string(raw) != "v2" {
			t.Fatalf("stale cache must be replaced, got %q", raw)
		}
	})
}

// seedExcerpt renders a seed for a failure message WITHOUT dumping it.
//
// 🔴 Seeds in this corpus reach ~22 KB (system_interaction.md). Pasting one into
// `go test` output buries the one line that matters and makes the failure hard
// to read in CI logs, so a failure names the first line and the size instead.
func seedExcerpt(name, text string) string {
	first := strings.SplitN(text, "\n", 2)[0]
	if len([]rune(first)) > 120 {
		first = string([]rune(first)[:120]) + "…"
	}
	return fmt.Sprintf("%s (%d runes) first line: %q", name, len([]rune(text)), first)
}

// 🔴 NO SHIPPED SEED MAY HARDCODE A MEMBER'S DISPLAY NAME.
//
// `seeds/role_def_assistant.md` opened with 「# 助理 — Mira」. Mira is the
// out-of-box display name of the seed MEMBER row (dbseed.go) — a label the
// owner may change at any moment through PATCH /api/members/{id}. Seeds are
// baked into the binary and do not change with it, so the day the owner renames
// her, the FACTORY VERSION of that document — the very text the 初始版本 row
// offers to restore — describes a person who does not exist.
//
// A role definition says what the role DOES; who currently holds it is a fact
// about the roster, not about the duty.
//
// 🔴 SCOPE IS EVERY STAGED `*.md`, NOT JUST THE ROLE DEFINITION. This started as
// a role-definition-only loop, which left `seeds/insight_<key>.md` — embedded in
// the same binary, restored by the same 初始版本 row, stale for the same reason —
// out of range. The reason ("a shipped file cannot track a mutable roster
// label") holds verbatim for every file in the corpus, so the corpus IS the
// scope: a seed added tomorrow is covered without anyone remembering to add it
// to a list. Every file is clean today, so widening cost nothing.
//
// ⚠️ IT READS THE STAGED EMBED, NOT `seeds/`. The corpus is `seedsdistFS()` —
// what `bin/build-seedsdist` copied into `seedsdist/`. Editing `seeds/*.md` and
// running `go test` straight away tests the PREVIOUS staged copy and goes green
// on a seed you just broke. Run `bash bin/build-seedsdist` first. (CI is safe:
// bin/ci.sh stages before it tests. The exposed party is a developer at a
// terminal — and the reviewer of this ticket hit exactly that false green.)
//
// The name is read back from the seeded member row rather than written as a
// literal here, so the assertion tracks whatever dbseed actually ships instead
// of pinning a string that could drift out from under it.
func TestNoShippedSeedHardcodesTheMembersDisplayName(t *testing.T) {
	api := newTasksTestServer(t)
	// The out-of-box roster is what ships alongside the seed files; boot it here
	// so the name under test is the one production really starts with.
	if err := seedOutOfBox(api.dal); err != nil {
		t.Fatal(err)
	}
	seeded, err := api.dal.GetMember(seedMiraID)
	if err != nil {
		t.Fatal(err)
	}
	if seeded == nil || seeded.Name == "" {
		t.Fatal("fixture: no seeded assistant member — this test would be vacuous")
	}

	names, err := fs.Glob(seedsdistFS(), "*.md")
	if err != nil {
		t.Fatalf("list staged seeds: %v", err)
	}
	// ── anti-vacuity ────────────────────────────────────────────────────────
	// A broken glob, or an unstaged seedsdist/, yields an empty corpus and every
	// assertion below passes by never running. The two files this ticket is
	// actually about must be present by name.
	seen := map[string]string{}
	for _, name := range names {
		text, err := api.root.readSeedFile(name)
		if err != nil {
			t.Fatalf("%s: staged seed unreadable: %v", name, err)
		}
		seen[name] = text
	}
	for _, must := range []string{"role_def_assistant.md", "insight_assistant.md"} {
		if _, ok := seen[must]; !ok {
			t.Fatalf("staged seed corpus is missing %s (it holds %v) — run `bash bin/build-seedsdist`; "+
				"without it this test asserts nothing", must, names)
		}
	}
	// Positive control for the Contains probe below: this assertion really can
	// see inside a staged seed's bytes.
	if !strings.Contains(seen["role_def_assistant.md"], "助理") {
		t.Fatalf("fixture — the staged role definition does not read like a duty document: %s",
			seedExcerpt("role_def_assistant.md", seen["role_def_assistant.md"]))
	}

	for _, name := range names {
		if strings.Contains(seen[name], seeded.Name) {
			t.Errorf("the shipped seed %s hardcodes the member display name %q. "+
				"Rename the member and this factory text starts describing nobody. "+
				"Describe the FUNCTION instead.\n%s",
				name, seeded.Name, seedExcerpt(name, seen[name]))
		}
	}
}
