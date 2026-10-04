package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	codexMetaReadLimit = 4096
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
	root           string
	claudeProjects string
	codexSessions  string
	goos           string
	run            stdoutRunner
	statfs         func(path string) (free, total int64, err error)
	now            func() time.Time
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
	// The spawned claude reads CLAUDE_CONFIG_DIR only when the warden exports it
	// (an OC_CLAUDE_JSON redirect); every other CLAUDE_* is purged from its
	// launch line, so the warden's own environment does not decide this.
	projects := filepath.Join(home, ".claude", "projects")
	if ch, err := resolveClaudeHome(env, os.Getwd); err == nil && ch.ConfigDir != "" {
		projects = filepath.Join(ch.ConfigDir, "projects")
	}
	// The member's env file can also set CODEX_HOME, but reading it takes an
	// interactive shell per measurement (codexRealHome); a redirect made only
	// there goes unmeasured.
	codexHome := strings.TrimSpace(env("CODEX_HOME"))
	if !filepath.IsAbs(codexHome) {
		codexHome = filepath.Join(home, ".codex")
	}
	return diskUsageProbe{
		root:           officraftRootFor(home, ns),
		claudeProjects: projects,
		codexSessions:  filepath.Join(filepath.Clean(codexHome), "sessions"),
		goos:           goos,
		run:            keep,
		statfs:         statfsBytes,
		now:            time.Now,
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

// claudeProjectName is Claude Code's directory name for a working directory:
// every character outside [A-Za-z0-9] becomes '-'.
func claudeProjectName(path string) string {
	b := []byte(path)
	for i, c := range b {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

func (p diskUsageProbe) measure() map[string]any {
	start := p.now()
	usage := map[string]any{}
	agentsDir := filepath.Join(p.root, "agents")

	var ids []string
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				ids = append(ids, e.Name())
			}
		}
	}
	workspace := map[string]int64{}
	if sizes := p.du("-k", "-d", "2", p.root); sizes != nil {
		if total, ok := sizes[p.root]; ok {
			usage["root_bytes"] = total
			for _, id := range ids {
				if n, ok := sizes[filepath.Join(agentsDir, id)]; ok {
					workspace[id] = n
				}
			}
		}
	}

	conversation := map[string]int64{}
	claudeTotal, claudeOK := p.claudeConversations(agentsDir, ids, conversation)
	codexTotal, codexOK := p.codexConversations(agentsDir, conversation)
	if claudeOK {
		usage["claude_conversation_bytes"] = claudeTotal
	}
	if codexOK {
		usage["codex_conversation_bytes"] = codexTotal
	}

	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	for id := range conversation {
		set[id] = true
	}
	ordered := make([]string, 0, len(set))
	for id := range set {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	members := make([]any, 0, len(ordered))
	for _, id := range ordered {
		m := map[string]any{"member_id": id}
		if n, ok := workspace[id]; ok {
			m["workspace_bytes"] = n
		}
		// A share from only one runtime would read as the member's whole history.
		if claudeOK && codexOK {
			m["conversation_bytes"] = conversation[id]
		}
		members = append(members, m)
	}
	usage["members"] = members

	if free, total, err := p.statfs(p.root); err == nil {
		usage["disk_free_bytes"] = free
		usage["disk_total_bytes"] = total
	}
	end := p.now()
	usage["measured_at"] = float64(end.Unix())
	usage["took_secs"] = round1(end.Sub(start).Seconds())
	return usage
}

func (p diskUsageProbe) claudeConversations(agentsDir string, ids []string, share map[string]int64) (int64, bool) {
	entries, err := os.ReadDir(p.claudeProjects)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, true
	}
	if err != nil {
		return 0, false
	}
	prefix := claudeProjectName(agentsDir) + "-"
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			dirs = append(dirs, filepath.Join(p.claudeProjects, e.Name()))
		}
	}
	if len(dirs) == 0 {
		return 0, true
	}
	sizes := p.du(append([]string{"-sk"}, dirs...)...)
	if sizes == nil {
		return 0, false
	}
	encoded := make(map[string]string, len(ids))
	for _, id := range ids {
		encoded[id] = claudeProjectName(filepath.Join(agentsDir, id))
	}
	var total int64
	for _, dir := range dirs {
		n, ok := sizes[dir]
		if !ok {
			continue
		}
		total += n
		name := filepath.Base(dir)
		// An id can be another id plus '-' and more (ow, ow-3), so the longest
		// match wins.
		owner := ""
		for id, enc := range encoded {
			if (name == enc || strings.HasPrefix(name, enc+"-")) && len(enc) > len(encoded[owner]) {
				owner = id
			}
		}
		if owner != "" {
			share[owner] += n
		}
	}
	return total, true
}

func (p diskUsageProbe) codexConversations(agentsDir string, share map[string]int64) (int64, bool) {
	if _, err := os.Stat(p.codexSessions); errors.Is(err, fs.ErrNotExist) {
		return 0, true
	}
	var total int64
	err := filepath.WalkDir(p.codexSessions, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("no block count")
		}
		cwd := codexSessionCwd(path)
		rel, err := filepath.Rel(agentsDir, cwd)
		if cwd == "" || err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
		n := int64(st.Blocks) * 512
		total += n
		id, _, _ := strings.Cut(rel, string(filepath.Separator))
		share[id] += n
		return nil
	})
	if err != nil {
		return 0, false
	}
	return total, true
}

// codexSessionCwd reads the cwd of the session_meta record codex writes as a
// rollout's first line. That line also carries the full base instructions, so
// it is far longer than the read and is never decoded whole.
func codexSessionCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head, _ := io.ReadAll(io.LimitReader(f, codexMetaReadLimit))
	if i := bytes.IndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	}
	if !bytes.Contains(head, []byte(`"type":"session_meta"`)) {
		return ""
	}
	i := bytes.Index(head, []byte(`"cwd":`))
	if i < 0 {
		return ""
	}
	var cwd string
	if err := json.NewDecoder(bytes.NewReader(head[i+len(`"cwd":`):])).Decode(&cwd); err != nil {
		return ""
	}
	if !filepath.IsAbs(cwd) {
		return ""
	}
	return filepath.Clean(cwd)
}

// diskUsageReporter measures in its own goroutine and hands the heartbeat the
// latest finished measurement: a measurement takes minutes, the heartbeat 30 s.
type diskUsageReporter struct {
	measure func() map[string]any

	mu       sync.Mutex
	latest   map[string]any
	interval time.Duration
}

func newDiskUsageReporter(measure func() map[string]any) *diskUsageReporter {
	return &diskUsageReporter{measure: measure, interval: defaultDiskUsageInterval}
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

// run measures at once, then once per interval; measurements never overlap
// because this loop is the only caller of measure.
func (r *diskUsageReporter) run(ctx context.Context, sleep func(context.Context, time.Duration) bool) {
	for ctx.Err() == nil {
		usage := r.measure()
		r.mu.Lock()
		r.latest = usage
		wait := r.interval
		r.mu.Unlock()
		if !sleep(ctx, wait) {
			return
		}
	}
}
