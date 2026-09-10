package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// stageBinary writes content at path with an executable mode and returns path.
func stageBinary(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestNewBinFingerprinter(t *testing.T) {
	root := t.TempDir()
	warden := stageBinary(t, filepath.Join(root, "warden", "ocwarden"), "warden-bytes-v1")
	agent := filepath.Join(root, "warden", "ocagent")
	anchor := stageBinary(t, filepath.Join(root, "Applications", "officraft"), "anchor-bytes-v1")

	exe := func() (string, error) { return warden, nil }

	got := newBinFingerprinter(exe, anchor).collect()
	want := map[string]string{"ocwarden": "9c8d3cb7cabb", "officraft": "6e5e45678a55"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a not-yet-downloaded ocagent sibling must read as absent: collect = %v, want %v", got, want)
	}

	stageBinary(t, agent, "agent-bytes-v1")
	got = newBinFingerprinter(exe, anchor).collect()
	want = map[string]string{
		"ocwarden":  "9c8d3cb7cabb",
		"ocagent":   "7ce94980e3f1",
		"officraft": "6e5e45678a55",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collect = %v, want %v", got, want)
	}

	got = newBinFingerprinter(exe, "").collect()
	want = map[string]string{"ocwarden": "9c8d3cb7cabb", "ocagent": "7ce94980e3f1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("an unresolvable anchor path must be skipped: collect = %v, want %v", got, want)
	}

	broken := func() (string, error) { return "", errors.New("no executable") }
	got = newBinFingerprinter(broken, anchor).collect()
	want = map[string]string{"officraft": "6e5e45678a55"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("an unnameable executable must leave only the anchor: collect = %v, want %v", got, want)
	}

	link := filepath.Join(root, "bin", "ocwarden")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir link dir: %v", err)
	}
	if err := os.Symlink(warden, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got = newBinFingerprinter(func() (string, error) { return link, nil }, anchor).collect()
	want = map[string]string{
		"ocwarden":  "9c8d3cb7cabb",
		"ocagent":   "7ce94980e3f1",
		"officraft": "6e5e45678a55",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the sibling must be looked for beside the RESOLVED executable: collect = %v, want %v", got, want)
	}
}

func TestBinFingerprinterCollect(t *testing.T) {
	epoch := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	stats := map[string]probeFileInfo{
		"/w/ocwarden": {size: 15, mtime: epoch},
		"/w/ocagent":  {size: 14, mtime: epoch},
	}
	bodies := map[string]string{
		"/w/ocwarden": "warden-bytes-v1",
		"/w/ocagent":  "agent-bytes-v1",
	}
	var statErr, readErr map[string]error
	var reads []string

	newFP := func() *binFingerprinter {
		return &binFingerprinter{
			paths: map[string]string{"ocwarden": "/w/ocwarden", "ocagent": "/w/ocagent", "officraft": ""},
			stat: func(p string) (os.FileInfo, error) {
				if err := statErr[p]; err != nil {
					return nil, err
				}
				info, ok := stats[p]
				if !ok {
					return nil, os.ErrNotExist
				}
				return info, nil
			},
			readFile: func(p string) ([]byte, error) {
				reads = append(reads, p)
				if err := readErr[p]; err != nil {
					return nil, err
				}
				return []byte(bodies[p]), nil
			},
			cache: map[string]fpCacheEntry{},
		}
	}

	fp := newFP()
	first := fp.collect()
	second := fp.collect()
	want := map[string]string{"ocwarden": "9c8d3cb7cabb", "ocagent": "7ce94980e3f1"}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Errorf("collect = %v then %v, want %v twice", first, second, want)
	}
	if wantReads := []string{"/w/ocagent", "/w/ocwarden"}; len(reads) != 2 ||
		!reflect.DeepEqual(sortedCopy(reads), wantReads) {
		t.Errorf("an unchanged (size, mtime) must be served from cache: reads = %v, want %v", reads, wantReads)
	}

	reads = nil
	bodies["/w/ocwarden"] = "warden-bytes-v2"
	stats["/w/ocwarden"] = probeFileInfo{size: 15, mtime: epoch.Add(time.Minute)}
	got := fp.collect()
	want = map[string]string{"ocwarden": "cf6022985580", "ocagent": "7ce94980e3f1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a same-size swap with a fresh mtime must re-hash: collect = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(reads, []string{"/w/ocwarden"}) {
		t.Errorf("only the changed path must be re-read: reads = %v, want [/w/ocwarden]", reads)
	}

	faults := []struct {
		name     string
		arrange  func()
		wantHash map[string]string
	}{
		{"a stat fault drops the entry", func() {
			statErr = map[string]error{"/w/ocagent": errors.New("stat: permission denied")}
		}, map[string]string{"ocwarden": "cf6022985580"}},
		{"a directory is never fingerprinted", func() {
			stats["/w/ocagent"] = probeFileInfo{size: 14, mtime: epoch, dir: true}
		}, map[string]string{"ocwarden": "cf6022985580"}},
		{"a read fault drops the entry", func() {
			stats["/w/ocagent"] = probeFileInfo{size: 14, mtime: epoch.Add(time.Hour)}
			readErr = map[string]error{"/w/ocagent": errors.New("read: input/output error")}
		}, map[string]string{"ocwarden": "cf6022985580"}},
		{"an empty file is not a fingerprint", func() {
			bodies["/w/ocagent"] = ""
			stats["/w/ocagent"] = probeFileInfo{size: 0, mtime: epoch}
		}, map[string]string{"ocwarden": "cf6022985580"}},
	}
	for _, c := range faults {
		statErr, readErr = nil, nil
		stats["/w/ocagent"] = probeFileInfo{size: 14, mtime: epoch}
		bodies["/w/ocagent"] = "agent-bytes-v1"
		fp := newFP()
		if primed := fp.collect(); primed["ocagent"] != "7ce94980e3f1" {
			t.Fatalf("%s: priming collect = %v", c.name, primed)
		}
		c.arrange()
		if got := fp.collect(); !reflect.DeepEqual(got, c.wantHash) {
			t.Errorf("%s: collect = %v, want %v", c.name, got, c.wantHash)
		}
		if _, cached := fp.cache["ocagent"]; cached {
			t.Errorf("%s: the stale cache entry survived the fault", c.name)
		}
	}
}

// sortedCopy returns s ordered, so a map-ordered read log compares stably.
func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
