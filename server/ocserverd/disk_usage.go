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
	// OldCopiesBytes: database copies kept beside the database by hand or by
	// an old release (officraft.db.bak-pre-v…, retreat-*/), which nothing
	// rotates or deletes.
	OldCopiesBytes *int
	MeasuredAt     float64
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
// read leaves the database size null.
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
	backups, strays, backupsErr := backupDirBytes(backupDirFor(dbPath))
	if backupsErr == nil {
		v := int(backups)
		sample.BackupsBytes = &v
	}
	// Whatever in backups/ the engine does not rotate stays until someone
	// deletes it, like the copies beside the database.
	if copies, err := oldDatabaseCopiesBytes(dbPath); err == nil && backupsErr == nil {
		v := int(copies + strays)
		sample.OldCopiesBytes = &v
	}
	return sample
}

// backupDirBytes splits the backup directory into what the backup engine
// rotates (backupFilesIn's files, both pools) and everything else: a
// .partial (and its -journal) left by a snapshot that died half-written,
// which no later run removes, and anything put there by hand. A directory
// that does not exist holds neither. A snapshot being written right now is
// briefly a .partial too and counts as a stray until it is renamed.
func backupDirBytes(dir string) (rotated, strays int64, err error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		n, err := treeAllocatedBytes(filepath.Join(dir, e.Name()))
		if err != nil {
			return 0, 0, err
		}
		if !e.IsDir() && isEngineBackup(e.Name()) {
			rotated += n
		} else {
			strays += n
		}
	}
	return rotated, strays, nil
}

// oldDatabaseCopiesBytes sizes the entries beside the database named after it
// plus a dot and a suffix (its -wal and -shm use a dash), and the retreat-*
// directories there.
func oldDatabaseCopiesBytes(dbPath string) (int64, error) {
	dir, base := filepath.Dir(dbPath), filepath.Base(dbPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base+".") && !(e.IsDir() && strings.HasPrefix(name, "retreat-")) {
			continue
		}
		n, err := treeAllocatedBytes(filepath.Join(dir, name))
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
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

// Display order of the categories the server knows; any other key a warden
// sends follows them by key, and other comes last.
var diskCategoryOrder = []string{"database", "backups", "workspaces", "logs", "old_version_backups", "old_database_copies"}

const (
	diskCategoryOldVersions = "old_version_backups"
	diskCategoryOldCopies   = "old_database_copies"
	diskCategoryOther       = "other"
)

// The rows only the server fills; a warden entry with one of these keys, or
// one that names one as its parent (a parent's size is the sum of its parts,
// so a part would replace the server's number), is dropped.
var serverDiskCategories = map[string]bool{
	"database": true, "backups": true, "workspaces": true, diskCategoryOldCopies: true, diskCategoryOther: true,
}

// wardenDiskCategories reads the report's categories. An entry without a key
// or an in_root flag is dropped (its bytes stay in other); bytes that are not
// a whole non-negative number read as a failed probe. The first entry of a
// key wins.
func wardenDiskCategories(report map[string]any) []machineDiskUsageCategoryDTO {
	list, _ := report["categories"].([]any)
	seen := map[string]bool{}
	var out []machineDiskUsageCategoryDTO
	for _, raw := range list {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key, _ := entry["key"].(string)
		inRoot, flagged := entry["in_root"].(bool)
		if key == "" || !flagged || seen[key] || serverDiskCategories[key] {
			continue
		}
		seen[key] = true
		c := machineDiskUsageCategoryDTO{Key: key, Bytes: diskByteCount(entry["bytes"]), InRoot: inRoot}
		if parent, ok := entry["parent_key"].(string); ok && parent != "" && parent != key {
			if serverDiskCategories[parent] {
				continue
			}
			c.ParentKey = &parent
		}
		out = append(out, c)
	}
	return out
}

// orderDiskCategories adds a row for every parent named by a part (the sum of
// its parts, null when one is null) and puts the rows in display order, each
// part right after its parent.
func orderDiskCategories(rows []machineDiskUsageCategoryDTO) []machineDiskUsageCategoryDTO {
	top := map[string]*machineDiskUsageCategoryDTO{}
	parts := map[string][]machineDiskUsageCategoryDTO{}
	for i := range rows {
		r := rows[i]
		if r.ParentKey == nil {
			if _, dup := top[r.Key]; !dup {
				top[r.Key] = &r
			}
			continue
		}
		parts[*r.ParentKey] = append(parts[*r.ParentKey], r)
	}
	for parent, list := range parts {
		var sum *int
		zero := 0
		sum = &zero
		for _, p := range list {
			sum = addBytes(sum, p.Bytes)
		}
		// A reported row with the parent's own key is replaced: the parent is
		// what its parts add up to.
		top[parent] = &machineDiskUsageCategoryDTO{Key: parent, Bytes: sum, InRoot: list[0].InRoot}
		sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	}
	rank := func(key string) int {
		for i, k := range diskCategoryOrder {
			if k == key {
				return i
			}
		}
		return len(diskCategoryOrder)
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if ri, rj := rank(keys[i]), rank(keys[j]); ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	out := make([]machineDiskUsageCategoryDTO, 0, len(rows)+len(parts))
	for _, k := range keys {
		out = append(out, *top[k])
		out = append(out, parts[k]...)
	}
	return out
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
	out := &machineDiskUsageDTO{
		Categories: []machineDiskUsageCategoryDTO{},
		Members:    []machineDiskUsageMemberDTO{},
	}
	var rows []machineDiskUsageCategoryDTO
	if self != nil {
		stamp := self.MeasuredAt
		out.DatabaseMeasuredAt = &stamp
		rows = append(rows,
			machineDiskUsageCategoryDTO{Key: "database", Bytes: self.DatabaseBytes, InRoot: self.DBInStationRoot},
			machineDiskUsageCategoryDTO{Key: "backups", Bytes: self.BackupsBytes, InRoot: self.DBInStationRoot})
	}
	var root *int
	if report != nil {
		out.MeasuredAt = diskStamp(report["measured_at"])
		out.DiskFreeBytes = diskByteCount(report["disk_free_bytes"])
		out.DiskTotalBytes = diskByteCount(report["disk_total_bytes"])
		root = diskByteCount(report["root_bytes"])
		rows = append(rows, machineDiskUsageCategoryDTO{Key: "workspaces", Bytes: diskMembers(report, out, roster), InRoot: true})
		rows = append(rows, wardenDiskCategories(report)...)
	}
	if self != nil {
		// Copies inside the root are what old_version_backups names; outside it
		// they are a row of their own, added to the total like the database.
		if self.DBInStationRoot {
			rows = mergeOldCopies(rows, self.OldCopiesBytes)
		} else {
			rows = append(rows, machineDiskUsageCategoryDTO{Key: diskCategoryOldCopies, Bytes: self.OldCopiesBytes})
		}
	}
	out.Categories = orderDiskCategories(rows)

	if report == nil {
		return out
	}
	// A total or other missing any part would read as a smaller station, or a
	// larger other, than it is; either is null instead.
	total, other := root, root
	for _, c := range out.Categories {
		if c.ParentKey != nil {
			continue
		}
		if c.InRoot {
			other = subBytes(other, c.Bytes)
		} else {
			total = addBytes(total, c.Bytes)
		}
	}
	// Not measured yet, the database may still be in the root.
	if isSelf && self == nil {
		other = nil
	}
	if other != nil && *other < 0 {
		zero := 0
		other = &zero
	}
	out.TotalBytes = total
	out.Categories = append(out.Categories, machineDiskUsageCategoryDTO{Key: diskCategoryOther, Bytes: other, InRoot: true})
	return out
}

// mergeOldCopies adds the server's old database copies, inside the root, to
// old_version_backups: as one more part when the warden sent that row as
// parts (the parent is their sum, so a number added to it would be lost),
// added to it when the warden sent it whole, or as the row itself when the
// warden did not send it.
func mergeOldCopies(rows []machineDiskUsageCategoryDTO, copies *int) []machineDiskUsageCategoryDTO {
	for _, r := range rows {
		if r.ParentKey != nil && *r.ParentKey == diskCategoryOldVersions {
			parent := diskCategoryOldVersions
			return append(rows, machineDiskUsageCategoryDTO{Key: diskCategoryOldCopies, ParentKey: &parent, Bytes: copies, InRoot: true})
		}
	}
	for i := range rows {
		if rows[i].Key == diskCategoryOldVersions && rows[i].ParentKey == nil {
			rows[i].Bytes = addBytes(rows[i].Bytes, copies)
			return rows
		}
	}
	return append(rows, machineDiskUsageCategoryDTO{Key: diskCategoryOldVersions, Bytes: copies, InRoot: true})
}

func subBytes(from, part *int) *int {
	if from == nil || part == nil {
		return nil
	}
	v := *from - *part
	return &v
}

// diskMembers fills out.Members from the report and returns their workspaces'
// sum, null when the list is missing or a workspace was not sized.
func diskMembers(report map[string]any, out *machineDiskUsageDTO, roster map[string]Member) *int {
	list, ok := report["members"].([]any)
	if !ok {
		return nil
	}
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
			MemberID:       id,
			WorkspaceBytes: diskByteCount(entry["workspace_bytes"]),
			RosterStatus:   string(Unknown),
		}
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
		a, b := out.Members[i].WorkspaceBytes, out.Members[j].WorkspaceBytes
		if (a == nil) != (b == nil) {
			return b == nil
		}
		if a != nil && *a != *b {
			return *a > *b
		}
		return out.Members[i].MemberID < out.Members[j].MemberID
	})
	if !allSized {
		return nil
	}
	return &workspace
}
