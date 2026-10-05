package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultDiskUsageInterval = time.Hour
	minDiskUsageIntervalSecs = 600
	maxDiskUsageIntervalSecs = 86400
	// A whole-root du at background priority took 100–430 s on a 35 GB station
	// under load; past this the measurement is dropped rather than left holding
	// the next one back.
	diskUsageDuTimeout = 15 * time.Minute
	// The reporter re-reads its interval at least this often, so an owner's
	// change applies within it rather than after the wait already begun.
	diskUsageWakeEvery = time.Minute
)

func diskUsageIntervalFromReceipt(body map[string]any) time.Duration {
	secs, ok := body["disk_usage_interval_secs"].(float64)
	if !ok || secs != float64(int64(secs)) ||
		secs < minDiskUsageIntervalSecs || secs > maxDiskUsageIntervalSecs {
		return defaultDiskUsageInterval
	}
	return time.Duration(secs) * time.Second
}

type diskUsageProbe struct {
	root  string
	goos  string
	run   stdoutRunner
	lstat func(path string) (fs.FileInfo, error)
	// readDir lists a directory for the old-binary scan.
	readDir func(path string) ([]fs.DirEntry, error)
	statfs  func(path string) (free, total int64, err error)
	now     func() time.Time
}

func newDiskUsageProbe(env func(string) string, runner CmdRunner, goos string) (diskUsageProbe, bool) {
	home := strings.TrimSpace(env("HOME"))
	ns, err := namespaceFromEnv(env)
	if home == "" || !filepath.IsAbs(home) || err != nil {
		return diskUsageProbe{}, false
	}
	keep, ok := withRunTimeout(runner, diskUsageDuTimeout).(stdoutRunner)
	if !ok {
		return diskUsageProbe{}, false
	}
	return diskUsageProbe{
		root:    officraftRootFor(home, ns),
		goos:    goos,
		run:     keep,
		lstat:   os.Lstat,
		readDir: os.ReadDir,
		statfs:  statfsBytes,
		now:     time.Now,
	}, true
}

func statfsBytes(path string) (int64, int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize), nil
}

// du runs at background CPU and IO priority on macOS: a whole-root walk reads
// every inode of the station while members are working on the same disk.
func (p diskUsageProbe) du(args ...string) map[string]int64 {
	name := "du"
	if strings.HasPrefix(p.goos, "darwin") {
		name, args = "taskpolicy", append([]string{"-b", "du"}, args...)
	}
	// An unreadable subdirectory makes du exit 1 with every other line still valid.
	out, err := p.run.RunKeepStdout(name, args...)
	sizes := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		kb, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, perr := strconv.ParseInt(strings.TrimSpace(kb), 10, 64)
		if perr != nil || n < 0 {
			continue
		}
		sizes[path] = n * 1024
	}
	if err != nil && len(sizes) == 0 {
		return nil
	}
	return sizes
}

// The directories under the root that hold OffiCraft's own logs: the warden's
// on every machine, the server's (and autodeploy's) where the server runs.
var diskUsageLogDirs = []string{filepath.Join("warden", "log"), filepath.Join("server", "log")}

// Where an upgrade leaves the binary it replaced (ocserverd.bak,
// ocwarden.prev) and where older ones were kept by hand.
var diskUsageBinaryDirs = []string{"bin", "warden"}

var officraftBinaries = []string{"ocserverd", "ocwarden", "ocagent", "officraft"}

// isOldBinary: a binary's name, a dot and a suffix. officraft.probe is the
// cutover's staging copy of the anchor, not an old version. The name alone
// also matches the warden's state files (ocwarden.no-base); oldBinaryBytes
// tells those apart by the executable bit.
func isOldBinary(name string) bool {
	for _, b := range officraftBinaries {
		if suffix, ok := strings.CutPrefix(name, b+"."); ok && suffix != "" {
			return name != "officraft.probe"
		}
	}
	return false
}

// dirBytes reads one directory's size off the whole-root du. A directory that
// does not exist is a measured 0; one that exists but du did not size (it
// could not read it, or the walk failed) is unknown.
func (p diskUsageProbe) dirBytes(sizes map[string]int64, rel string) (int64, bool) {
	path := filepath.Join(p.root, rel)
	if _, err := p.lstat(path); errors.Is(err, fs.ErrNotExist) {
		return 0, true
	}
	if sizes == nil {
		return 0, false
	}
	n, ok := sizes[path]
	return n, ok
}

// oldBinaryBytes adds up the allocated blocks of the old binaries in bin/ and
// warden/; a directory that does not exist holds none. Only executable
// regular files count: an upgrade renames the binary it replaces and a copy
// kept by hand keeps its mode, while a state file named after a binary
// (ocwarden.no-base, or one added later) is never executable, so the bit
// needs no list of names to keep current. Symlinks and directories are not
// copies of a binary.
func (p diskUsageProbe) oldBinaryBytes() (int64, bool) {
	var total int64
	for _, rel := range diskUsageBinaryDirs {
		entries, err := p.readDir(filepath.Join(p.root, rel))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, false
		}
		for _, e := range entries {
			if !isOldBinary(e.Name()) {
				continue
			}
			info, err := p.lstat(filepath.Join(p.root, rel, e.Name()))
			if err != nil {
				return 0, false
			}
			if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				continue
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return 0, false
			}
			total += int64(st.Blocks) * 512
		}
	}
	return total, true
}

// category is one entry of the report's categories; a failed probe reports
// null bytes rather than leaving the category out, so the server can tell it
// from a warden that does not measure it.
func category(key string, bytes int64, ok bool) map[string]any {
	c := map[string]any{"key": key, "bytes": nil, "in_root": true}
	if ok {
		c["bytes"] = bytes
	}
	return c
}

func (p diskUsageProbe) measure() map[string]any {
	start := p.now()
	usage := map[string]any{}
	agentsDir := filepath.Join(p.root, "agents")

	sizes := p.du("-k", "-d", "2", p.root)
	// An agents directory that cannot be listed leaves members out: an empty
	// list would read as workspaces of 0.
	entries, listErr := p.readDir(agentsDir)
	if listErr == nil || errors.Is(listErr, fs.ErrNotExist) {
		members := []any{}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			m := map[string]any{"member_id": e.Name()}
			if n, ok := sizes[filepath.Join(agentsDir, e.Name())]; ok {
				m["workspace_bytes"] = n
			}
			members = append(members, m)
		}
		usage["members"] = members
	}
	if total, ok := sizes[p.root]; ok {
		usage["root_bytes"] = total
	}

	var logs int64
	logsOK := true
	for _, rel := range diskUsageLogDirs {
		n, ok := p.dirBytes(sizes, rel)
		logs += n
		logsOK = logsOK && ok
	}
	releases, releasesOK := p.dirBytes(sizes, "release-backups")
	binaries, binariesOK := p.oldBinaryBytes()
	usage["categories"] = []any{
		category("logs", logs, logsOK),
		category("old_version_backups", releases+binaries, releasesOK && binariesOK),
	}

	if free, total, err := p.statfs(p.root); err == nil {
		usage["disk_free_bytes"] = free
		usage["disk_total_bytes"] = total
	}
	end := p.now()
	usage["measured_at"] = float64(end.Unix())
	usage["took_secs"] = round1(end.Sub(start).Seconds())
	return usage
}

// diskUsageReporter measures in its own goroutine and hands the heartbeat the
// latest finished measurement: a measurement takes minutes, the heartbeat 30 s.
type diskUsageReporter struct {
	measure func() map[string]any
	now     func() time.Time

	mu       sync.Mutex
	latest   map[string]any
	interval time.Duration
}

func newDiskUsageReporter(measure func() map[string]any) *diskUsageReporter {
	return &diskUsageReporter{measure: measure, now: time.Now, interval: defaultDiskUsageInterval}
}

func (r *diskUsageReporter) snapshot() map[string]any {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest
}

func (r *diskUsageReporter) setInterval(d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interval = d
}

// run measures at once, then again once the interval has passed since the last
// measurement finished; measurements never overlap because this loop is the
// only caller of measure.
func (r *diskUsageReporter) run(ctx context.Context, sleep func(context.Context, time.Duration) bool) {
	for ctx.Err() == nil {
		usage := r.measure()
		finished := r.now()
		r.mu.Lock()
		r.latest = usage
		r.mu.Unlock()
		for {
			r.mu.Lock()
			wait := finished.Add(r.interval).Sub(r.now())
			r.mu.Unlock()
			if wait <= 0 {
				break
			}
			if !sleep(ctx, min(wait, diskUsageWakeEvery)) {
				return
			}
		}
	}
}
