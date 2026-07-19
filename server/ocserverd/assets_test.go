package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
