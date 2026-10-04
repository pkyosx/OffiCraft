package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// diskTestStation lays out <root>/server/data like a real station. Sizes are
// written with real bytes because the measurement counts allocated blocks
// (APFS: 4 KiB each, directories 0), not st_size.
//
//	officraft.db      8192 B  -> 8192
//	officraft.db-wal  5000 B  -> 8192
//	backups/a.db      4096 B  -> 4096
//	backups/b.db         1 B  -> 4096
//	backups/pre/c.db 10000 B  -> 12288
func diskTestStation(t *testing.T) (root, dbPath string) {
	t.Helper()
	root = t.TempDir()
	data := filepath.Join(root, "server", "data")
	diskTestWrite(t, filepath.Join(data, "officraft.db"), 8192)
	diskTestWrite(t, filepath.Join(data, "officraft.db-wal"), 5000)
	diskTestWrite(t, filepath.Join(data, "backups", "a.db"), 4096)
	diskTestWrite(t, filepath.Join(data, "backups", "b.db"), 1)
	diskTestWrite(t, filepath.Join(data, "backups", "pre", "c.db"), 10000)
	return root, filepath.Join(data, "officraft.db")
}

func diskTestWrite(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i%251 + 1)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func diskTestInt(n int) *int { return &n }

func TestMeasureServerDisk(t *testing.T) {
	at := time.Unix(1759500000, 500_000_000)

	t.Run("under a database inside the station root, the database counts the db and wal blocks and backups count the whole tree", func(t *testing.T) {
		root, dbPath := diskTestStation(t)

		apiWantValue(t, "sample", any(measureServerDisk(dbPath, root, at)), any(serverDiskSample{
			DatabaseBytes:   diskTestInt(16384),
			BackupsBytes:    diskTestInt(20480),
			MeasuredAt:      1759500000.5,
			DBInStationRoot: true,
		}))
	})

	t.Run("under a shared-memory file beside the database, its blocks are added to the database", func(t *testing.T) {
		root, dbPath := diskTestStation(t)
		diskTestWrite(t, dbPath+"-shm", 100)

		apiWantValue(t, "sample", any(measureServerDisk(dbPath, root, at)), any(serverDiskSample{
			DatabaseBytes:   diskTestInt(20480),
			BackupsBytes:    diskTestInt(20480),
			MeasuredAt:      1759500000.5,
			DBInStationRoot: true,
		}))
	})

	t.Run("under a station root elsewhere or unknown, the database is reported outside it", func(t *testing.T) {
		_, dbPath := diskTestStation(t)
		for name, stationRoot := range map[string]string{"elsewhere": t.TempDir(), "unknown": ""} {
			apiWantValue(t, name, any(measureServerDisk(dbPath, stationRoot, at)), any(serverDiskSample{
				DatabaseBytes: diskTestInt(16384),
				BackupsBytes:  diskTestInt(20480),
				MeasuredAt:    1759500000.5,
			}))
		}
	})

	t.Run("under a station root named through a symlink, the database is still inside it", func(t *testing.T) {
		root, dbPath := diskTestStation(t)
		link := filepath.Join(t.TempDir(), "station")
		if err := os.Symlink(root, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		apiWantValue(t, "sample", any(measureServerDisk(dbPath, link, at)), any(serverDiskSample{
			DatabaseBytes:   diskTestInt(16384),
			BackupsBytes:    diskTestInt(20480),
			MeasuredAt:      1759500000.5,
			DBInStationRoot: true,
		}))
	})

	t.Run("under no backup directory, backups are 0; under no database file, the database is null while backups are still measured", func(t *testing.T) {
		root, dbPath := diskTestStation(t)
		if err := os.RemoveAll(backupDirFor(dbPath)); err != nil {
			t.Fatalf("remove backups: %v", err)
		}
		apiWantValue(t, "no backups", any(measureServerDisk(dbPath, root, at)), any(serverDiskSample{
			DatabaseBytes:   diskTestInt(16384),
			BackupsBytes:    diskTestInt(0),
			MeasuredAt:      1759500000.5,
			DBInStationRoot: true,
		}))

		root, dbPath = diskTestStation(t)
		if err := os.Remove(dbPath); err != nil {
			t.Fatalf("remove db: %v", err)
		}
		apiWantValue(t, "no database", any(measureServerDisk(dbPath, root, at)), any(serverDiskSample{
			BackupsBytes:    diskTestInt(20480),
			MeasuredAt:      1759500000.5,
			DBInStationRoot: true,
		}))
	})

	t.Run("a measurement deletes nothing", func(t *testing.T) {
		root, dbPath := diskTestStation(t)
		measureServerDisk(dbPath, root, at)
		for _, rel := range []string{"officraft.db", "officraft.db-wal", "backups/a.db", "backups/b.db", "backups/pre/c.db"} {
			if _, err := os.Stat(filepath.Join(filepath.Dir(dbPath), rel)); err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
		}
	})
}

func TestRunServerDiskUsage(t *testing.T) {
	t.Run("the first measurement lands before the first wait, and every wait re-reads the owner's interval", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		root, dbPath := diskTestStation(t)
		clocks := []time.Time{time.Unix(1759500000, 0), time.Unix(1759503600, 0)}
		calls := 0
		clock := func() time.Time {
			now := clocks[calls]
			calls++
			return now
		}
		var waits []time.Duration
		var seenAtFirstWait *serverDiskSample
		sleep := func(d time.Duration) bool {
			waits = append(waits, d)
			if len(waits) == 1 {
				seenAtFirstWait = api.serverDisk.Load()
				if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"disk_usage_interval_secs":1200}`); status != 200 {
					t.Fatalf("PATCH: %d %v", status, data)
				}
				return true
			}
			return false
		}

		api.runServerDiskUsage(dbPath, root, clock, sleep)

		apiWantValue(t, "waits", any(waits), any([]time.Duration{3600 * time.Second, 1200 * time.Second}))
		apiWantValue(t, "sample at the first wait", any(*seenAtFirstWait), any(serverDiskSample{
			DatabaseBytes:   diskTestInt(16384),
			BackupsBytes:    diskTestInt(20480),
			MeasuredAt:      1759500000,
			DBInStationRoot: true,
		}))
		apiWantValue(t, "latest sample", any(*api.serverDisk.Load()), any(serverDiskSample{
			DatabaseBytes:   diskTestInt(16384),
			BackupsBytes:    diskTestInt(20480),
			MeasuredAt:      1759503600,
			DBInStationRoot: true,
		}))
	})
}
