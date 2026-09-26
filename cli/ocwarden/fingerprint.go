// fingerprint.go — the 12-hex sha256 prefixes of the LIVE on-disk ocwarden, its sibling ocagent and
// the TCC identity anchor officraft, sent in the telemetry `binaries` field. The server compares
// them with its own embedded prebuilts (the bytes /api/{warden,agent}/binary serves) to render the
// machine table's current/stale verdict; an absent entry reads as unknown, never a verdict.
// Deliberately CONTENT hashes, never an embedded version stamp (a stamped sha would loop — see
// selfupdate.go). The (size, mtime) cache cannot go stale for more than one cycle: a self-update
// swap rewrites the file, and an ocwarden swap exec-in-places this process.
package main

import (
	"os"
	"time"
)

type fpCacheEntry struct {
	size  int64
	mtime time.Time
	hash  string
}

// binFingerprinter is single-goroutine by contract: only the telemetry producer loop calls collect.
type binFingerprinter struct {
	paths    map[string]string
	stat     func(string) (os.FileInfo, error)
	readFile func(string) ([]byte, error)
	cache    map[string]fpCacheEntry
}

// officraft is fingerprinted because self-update never replaces it (that would void the machine's
// TCC grants), so this is the only way to see which anchor build a machine runs. anchorPath is
// passed in, not re-derived: resolvePaths (install.go) owns that derivation, and a hand-mirrored
// copy here is the kind of path drift this module has been bitten by.
func newBinFingerprinter(executable func() (string, error), anchorPath string) *binFingerprinter {
	return &binFingerprinter{
		paths: map[string]string{
			"ocwarden":  resolveSelfExe(executable),
			"ocagent":   selfUpdateAgentPath(executable),
			"officraft": anchorPath,
		},
		stat:     os.Stat,
		readFile: os.ReadFile,
		cache:    map[string]fpCacheEntry{},
	}
}

func (f *binFingerprinter) collect() map[string]string {
	out := map[string]string{}
	for name, path := range f.paths {
		if path == "" {
			continue
		}
		info, err := f.stat(path)
		if err != nil || info.IsDir() {
			delete(f.cache, name)
			continue
		}
		if entry, ok := f.cache[name]; ok &&
			entry.size == info.Size() && entry.mtime.Equal(info.ModTime()) {
			out[name] = entry.hash
			continue
		}
		data, err := f.readFile(path)
		if err != nil || len(data) == 0 {
			delete(f.cache, name)
			continue
		}
		hash := hashPrefix(data)
		f.cache[name] = fpCacheEntry{size: info.Size(), mtime: info.ModTime(), hash: hash}
		out[name] = hash
	}
	return out
}
