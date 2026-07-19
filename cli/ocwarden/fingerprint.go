// fingerprint.go — the heartbeat's binary content fingerprints (T-5f01).
//
// Every 30s telemetry cycle rides an extra `binaries` field: the 12-hex
// sha256 prefixes of the LIVE on-disk ocwarden (our own executable) and its
// sibling ocagent. The server compares them against the hashes of its own
// embedded prebuilts (the exact bytes /api/{warden,agent}/binary serves and
// the self-update swaps in verbatim) to render the machine table's
// "current"/"stale" verdict. Deliberately CONTENT hashes, never an embedded
// version stamp — the same swap-oracle reasoning as selfupdate.go's header
// (a stamped sha would loop and would flap CI's committed-prebuilt parity).
//
// Hashing a multi-MB binary every 30s is avoidable waste, so results are
// cached per path and invalidated on (size, mtime) change: a self-update swap
// rewrites the file (fresh mtime) and an ocwarden swap exec-in-places this
// process anyway, so the cache can never serve a stale fingerprint for longer
// than one cycle. A missing/unreadable path is simply omitted (the server
// reads an absent fingerprint as unknown, never a verdict).
package main

import (
	"os"
	"time"
)

// fpCacheEntry is one cached fingerprint keyed by the stat identity that
// invalidates it.
type fpCacheEntry struct {
	size  int64
	mtime time.Time
	hash  string
}

// binFingerprinter computes the {binary name → content-hash prefix} map the
// telemetry payload carries. Single-goroutine by contract: only the telemetry
// producer loop calls collect (no lock needed). The fs seams are injectable
// so tests drive it with fakes and no real multi-MB reads.
type binFingerprinter struct {
	paths    map[string]string // binary name → live path ("" entries skipped)
	stat     func(string) (os.FileInfo, error)
	readFile func(string) ([]byte, error)
	cache    map[string]fpCacheEntry
}

// newBinFingerprinter targets the SAME live paths the self-update loop keeps
// current: ocwarden = our own executable (symlinks resolved), ocagent = the
// home sibling (selfUpdateAgentPath — unconditional, so a not-yet-populated
// sibling reads as absent until the first self-update tick materializes it).
func newBinFingerprinter(executable func() (string, error)) *binFingerprinter {
	return &binFingerprinter{
		paths: map[string]string{
			"ocwarden": resolveSelfExe(executable),
			"ocagent":  selfUpdateAgentPath(executable),
		},
		stat:     os.Stat,
		readFile: os.ReadFile,
		cache:    map[string]fpCacheEntry{},
	}
}

// collect returns the current fingerprints, re-hashing only paths whose
// (size, mtime) stat identity changed since the last call. Fail-soft per
// binary: a stat/read fault drops that entry (and its stale cache) rather
// than reporting a wrong or outdated hash.
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
