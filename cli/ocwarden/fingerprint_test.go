package main

import (
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"
)

// fakeFileInfo is a minimal fs.FileInfo for the stat seam.
type fakeFileInfo struct {
	size  int64
	mtime time.Time
	dir   bool
}

func (f fakeFileInfo) Name() string       { return "x" }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() fs.FileMode  { return 0o755 }
func (f fakeFileInfo) ModTime() time.Time { return f.mtime }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

// fakeFS drives the fingerprinter with in-memory files and counts reads so
// the mtime/size cache is provable.
type fakeFS struct {
	files map[string][]byte
	infos map[string]fakeFileInfo
	reads map[string]int
}

func (f *fakeFS) stat(p string) (os.FileInfo, error) {
	info, ok := f.infos[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return info, nil
}

func (f *fakeFS) read(p string) ([]byte, error) {
	data, ok := f.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	f.reads[p]++
	return data, nil
}

func newFakeFP(f *fakeFS, paths map[string]string) *binFingerprinter {
	return &binFingerprinter{
		paths:    paths,
		stat:     f.stat,
		readFile: f.read,
		cache:    map[string]fpCacheEntry{},
	}
}

func TestBinFingerprinter_ReportsBothHashesAndOmitsMissing(t *testing.T) {
	warden := []byte("warden-bytes")
	fsys := &fakeFS{
		files: map[string][]byte{"/w/ocwarden": warden},
		infos: map[string]fakeFileInfo{"/w/ocwarden": {size: int64(len(warden)), mtime: time.Unix(1, 0)}},
		reads: map[string]int{},
	}
	fp := newFakeFP(fsys, map[string]string{"ocwarden": "/w/ocwarden", "ocagent": "/w/ocagent"})
	got := fp.collect()
	if len(got) != 1 || got["ocwarden"] != hashPrefix(warden) {
		t.Fatalf("collect = %v, want only ocwarden=%s (missing sibling omitted)", got, hashPrefix(warden))
	}
	if len(got["ocwarden"]) != selfUpdateHashPrefixLen {
		t.Fatalf("hash length = %d, want %d", len(got["ocwarden"]), selfUpdateHashPrefixLen)
	}
}

func TestBinFingerprinter_CacheSkipsRehashUntilStatChanges(t *testing.T) {
	warden := []byte("warden-v1")
	info := fakeFileInfo{size: int64(len(warden)), mtime: time.Unix(10, 0)}
	fsys := &fakeFS{
		files: map[string][]byte{"/w/ocwarden": warden},
		infos: map[string]fakeFileInfo{"/w/ocwarden": info},
		reads: map[string]int{},
	}
	fp := newFakeFP(fsys, map[string]string{"ocwarden": "/w/ocwarden"})
	first := fp.collect()
	second := fp.collect()
	if fsys.reads["/w/ocwarden"] != 1 {
		t.Fatalf("reads = %d, want 1 (second collect must hit the cache)", fsys.reads["/w/ocwarden"])
	}
	if first["ocwarden"] != second["ocwarden"] {
		t.Fatalf("cached hash drifted: %v vs %v", first, second)
	}

	// A swap rewrites the file → new bytes + new mtime → re-hash.
	swapped := []byte("warden-v2-longer")
	fsys.files["/w/ocwarden"] = swapped
	fsys.infos["/w/ocwarden"] = fakeFileInfo{size: int64(len(swapped)), mtime: time.Unix(20, 0)}
	third := fp.collect()
	if fsys.reads["/w/ocwarden"] != 2 {
		t.Fatalf("reads = %d, want 2 (stat change must invalidate)", fsys.reads["/w/ocwarden"])
	}
	if third["ocwarden"] != hashPrefix(swapped) || third["ocwarden"] == first["ocwarden"] {
		t.Fatalf("post-swap hash = %v, want fresh hash of the new bytes", third)
	}
}

func TestBinFingerprinter_ReadFaultDropsEntryAndStaleCache(t *testing.T) {
	warden := []byte("warden-v1")
	fsys := &fakeFS{
		files: map[string][]byte{"/w/ocwarden": warden},
		infos: map[string]fakeFileInfo{"/w/ocwarden": {size: int64(len(warden)), mtime: time.Unix(10, 0)}},
		reads: map[string]int{},
	}
	fp := newFakeFP(fsys, map[string]string{"ocwarden": "/w/ocwarden"})
	if got := fp.collect(); got["ocwarden"] == "" {
		t.Fatalf("precondition: first collect should hash, got %v", got)
	}
	// File vanishes (stat fails): the entry AND its cache must drop — a stale
	// cached hash must never be reported for a path that no longer reads.
	delete(fsys.infos, "/w/ocwarden")
	if got := fp.collect(); len(got) != 0 {
		t.Fatalf("collect after stat fault = %v, want empty", got)
	}
	if _, ok := fp.cache["ocwarden"]; ok {
		t.Fatal("stale cache entry survived a stat fault")
	}
}

func TestNewBinFingerprinter_TargetsSelfAndSibling(t *testing.T) {
	fp := newBinFingerprinter(func() (string, error) { return "/inst/dir/ocwarden", nil })
	if fp.paths["ocwarden"] == "" || fp.paths["ocagent"] == "" {
		t.Fatalf("paths = %v, want both ocwarden and the ocagent sibling", fp.paths)
	}
	if fp.paths["ocagent"] != "/inst/dir/ocagent" {
		t.Fatalf("ocagent path = %q, want the home sibling", fp.paths["ocagent"])
	}
	// An unresolvable executable degrades to empty paths (skipped), no error.
	fp = newBinFingerprinter(func() (string, error) { return "", fmt.Errorf("nope") })
	if got := fp.collect(); len(got) != 0 {
		t.Fatalf("collect with no self path = %v, want empty", got)
	}
}
