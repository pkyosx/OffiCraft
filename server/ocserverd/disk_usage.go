package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type serverDiskSample struct {
	DatabaseBytes *int
	BackupsBytes  *int
	MeasuredAt    float64
	// DBInStationRoot: the station root's own measurement (the warden's
	// root_bytes) already counts the database and backups.
	DBInStationRoot bool
}

// allocatedBytes is what du counts (allocated blocks), not st_size: a sparse
// or preallocated file differs between the two.
func allocatedBytes(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}

func fileAllocatedBytes(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	return allocatedBytes(info), nil
}

func treeAllocatedBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += allocatedBytes(info)
		return nil
	})
	return total, err
}

func resolvedDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// measureServerDisk reads sizes only; it never removes anything. A missing
// -wal/-shm or backups directory counts as 0; a database file that cannot be
// read leaves database_bytes null.
func measureServerDisk(dbPath, stationRoot string, now time.Time) serverDiskSample {
	sample := serverDiskSample{
		MeasuredAt: float64(now.UnixNano()) / 1e9,
	}
	if stationRoot != "" {
		sample.DBInStationRoot = pathWithin(resolvedDir(filepath.Dir(dbPath)), resolvedDir(stationRoot))
	}
	if db, err := fileAllocatedBytes(dbPath); err == nil {
		total := db
		ok := true
		for _, suffix := range []string{"-wal", "-shm"} {
			n, err := fileAllocatedBytes(dbPath + suffix)
			if err != nil && !os.IsNotExist(err) {
				ok = false
				break
			}
			total += n
		}
		if ok {
			v := int(total)
			sample.DatabaseBytes = &v
		}
	}
	backups, err := treeAllocatedBytes(backupDirFor(dbPath))
	if err == nil || os.IsNotExist(err) {
		if err != nil {
			backups = 0
		}
		v := int(backups)
		sample.BackupsBytes = &v
	}
	return sample
}

func (s *apiServer) recordServerDisk(dbPath, stationRoot string, now time.Time) {
	sample := measureServerDisk(dbPath, stationRoot, now)
	s.serverDisk.Store(&sample)
}

// serverDiskWakeEvery bounds each wait so an owner's interval change applies
// within it, not after a wait of up to a day already begun.
const serverDiskWakeEvery = time.Minute

// runServerDiskUsage measures immediately, then again once the current
// interval setting has passed since the last measurement.
func (s *apiServer) runServerDiskUsage(dbPath, stationRoot string, clock func() time.Time, sleep func(time.Duration) bool) {
	for {
		last := clock()
		s.recordServerDisk(dbPath, stationRoot, last)
		for {
			wait := last.Add(time.Duration(s.diskUsageInterval()) * time.Second).Sub(clock())
			if wait <= 0 {
				break
			}
			if !sleep(min(wait, serverDiskWakeEvery)) {
				return
			}
		}
	}
}

func (s *apiServer) startServerDiskUsage(dbPath, stationRoot string) {
	go s.runServerDiskUsage(dbPath, stationRoot, time.Now, func(d time.Duration) bool {
		time.Sleep(d)
		return true
	})
}

func stationRootFor(namespace string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return officraftRootPath(home, namespace)
}

// diskByteCount accepts only a whole, non-negative JSON number: the warden's
// report is stored unvalidated, and a wrong-typed field reads as not measured.
func diskByteCount(v any) *int {
	f, ok := v.(float64)
	if !ok || f < 0 || f != float64(int64(f)) {
		return nil
	}
	n := int(f)
	return &n
}

func diskStamp(v any) *float64 {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	return &f
}

func addBytes(parts ...*int) *int {
	total := 0
	for _, p := range parts {
		if p == nil {
			return nil
		}
		total += *p
	}
	return &total
}

func orZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// machineDiskUsage folds the warden's stored report (nil when none) and, on the
// server's own machine, the server's database measurement.
func machineDiskUsage(report map[string]any, self *serverDiskSample, isSelf bool, roster map[string]Member) *machineDiskUsageDTO {
	if !isSelf {
		self = nil
	}
	if report == nil && self == nil {
		return nil
	}
	out := &machineDiskUsageDTO{Members: []machineDiskUsageMemberDTO{}}
	if self != nil {
		out.DatabaseBytes = self.DatabaseBytes
		out.BackupsBytes = self.BackupsBytes
		stamp := self.MeasuredAt
		out.DatabaseMeasuredAt = &stamp
	}
	if report == nil {
		return out
	}
	out.MeasuredAt = diskStamp(report["measured_at"])
	out.DiskFreeBytes = diskByteCount(report["disk_free_bytes"])
	out.DiskTotalBytes = diskByteCount(report["disk_total_bytes"])
	root := diskByteCount(report["root_bytes"])
	claude := diskByteCount(report["claude_conversation_bytes"])
	codex := diskByteCount(report["codex_conversation_bytes"])
	out.ClaudeConversationBytes = claude
	out.CodexConversationBytes = codex
	out.ConversationBytes = addBytes(claude, codex)

	if list, ok := report["members"].([]any); ok {
		workspace := 0
		allSized := true
		for _, raw := range list {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := entry["member_id"].(string)
			if id == "" {
				continue
			}
			row := machineDiskUsageMemberDTO{
				MemberID:          id,
				WorkspaceBytes:    diskByteCount(entry["workspace_bytes"]),
				ConversationBytes: diskByteCount(entry["conversation_bytes"]),
				RosterStatus:      string(Unknown),
			}
			row.TotalBytes = orZero(row.WorkspaceBytes) + orZero(row.ConversationBytes)
			if row.WorkspaceBytes == nil {
				allSized = false
			} else {
				workspace += *row.WorkspaceBytes
			}
			if m, known := roster[id]; known {
				name := m.Name
				row.Name = &name
				row.RosterStatus = string(Active)
				if m.RosterStatus != RosterStatusActive {
					row.RosterStatus = string(Removed)
				}
			}
			out.Members = append(out.Members, row)
		}
		sort.SliceStable(out.Members, func(i, j int) bool {
			a, b := out.Members[i], out.Members[j]
			if a.TotalBytes != b.TotalBytes {
				return a.TotalBytes > b.TotalBytes
			}
			return a.MemberID < b.MemberID
		})
		if allSized {
			out.WorkspaceBytes = &workspace
		}
	}

	// A total missing any part would read as a smaller station, not an unknown one.
	out.TotalBytes = addBytes(root, claude, codex)
	if out.TotalBytes != nil && self != nil && !self.DBInStationRoot {
		out.TotalBytes = addBytes(out.TotalBytes, self.DatabaseBytes, self.BackupsBytes)
	}

	other := addBytes(root)
	if other != nil && out.WorkspaceBytes == nil {
		other = nil
	}
	if other != nil {
		v := *other - *out.WorkspaceBytes
		if isSelf {
			switch {
			case self == nil:
				// Not measured yet: the root may still hold the database.
				other = nil
			case self.DBInStationRoot:
				if inner := addBytes(self.DatabaseBytes, self.BackupsBytes); inner != nil {
					v -= *inner
				} else {
					other = nil
				}
			}
		}
		if other != nil {
			if v < 0 {
				v = 0
			}
			other = &v
		}
	}
	out.OtherBytes = other
	return out
}
