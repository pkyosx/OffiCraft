package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type backupHealthTestPut struct {
	Key   string
	Value string
}

type backupHealthTestStore struct {
	mu     sync.Mutex
	values map[string]string
	gets   []string
	puts   []backupHealthTestPut
	getErr error
	putErr error
}

func newBackupHealthTestStore() *backupHealthTestStore {
	return &backupHealthTestStore{values: map[string]string{}}
}

func (s *backupHealthTestStore) GetSetting(key string) (*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets = append(s.gets, key)
	if s.getErr != nil {
		return nil, s.getErr
	}
	value, ok := s.values[key]
	if !ok {
		return nil, nil
	}
	return &value, nil
}

func (s *backupHealthTestStore) PutSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts = append(s.puts, backupHealthTestPut{Key: key, Value: value})
	if s.putErr != nil {
		return s.putErr
	}
	s.values[key] = value
	return nil
}

func (s *backupHealthTestStore) seed(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

func (s *backupHealthTestStore) snapshot() (map[string]string, []string, []backupHealthTestPut) {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make(map[string]string, len(s.values))
	for key, value := range s.values {
		values[key] = value
	}
	gets := append([]string(nil), s.gets...)
	puts := append([]backupHealthTestPut(nil), s.puts...)
	return values, gets, puts
}

func backupHealthTestExpectStore(t *testing.T, store *backupHealthTestStore, wantValues map[string]string, wantGets []string, wantPuts []backupHealthTestPut) {
	t.Helper()
	values, gets, puts := store.snapshot()
	if !reflect.DeepEqual(values, wantValues) {
		t.Errorf("stored values = %#v, want %#v", values, wantValues)
	}
	if !reflect.DeepEqual(gets, wantGets) {
		t.Errorf("gets = %#v, want %#v", gets, wantGets)
	}
	if !reflect.DeepEqual(puts, wantPuts) {
		t.Errorf("puts = %#v, want %#v", puts, wantPuts)
	}
}

func backupHealthTestWriteBackups(t *testing.T, dbPath string, names ...string) {
	t.Helper()
	dir := backupDirFor(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create backup directory: %v", err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("backup"), 0o600); err != nil {
			t.Fatalf("write backup %q: %v", name, err)
		}
	}
}

func backupHealthTestBackupNames(t *testing.T, dbPath string) []string {
	t.Helper()
	entries, err := backupFilesIn(backupDirFor(dbPath))
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

func backupHealthTestReportJSON(t *testing.T, got BackupHealthDTO) string {
	t.Helper()
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	return string(raw)
}

func TestBaselineAt(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	t.Run("missing baseline is armed and persisted at now", func(t *testing.T) {
		store := newBackupHealthTestStore()
		got, err := newBackupHealthMonitor(store, "").baselineAt(now)
		if err != nil {
			t.Fatalf("baselineAt error: %v", err)
		}
		if !got.Equal(now) {
			t.Errorf("baseline = %v, want %v", got, now)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupWatchdogBaseline: "1788868800.000000"},
			[]string{settingBackupWatchdogBaseline},
			[]backupHealthTestPut{{Key: settingBackupWatchdogBaseline, Value: "1788868800.000000"}},
		)
	})

	t.Run("valid baseline is returned without being rewritten", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupWatchdogBaseline, "1788000000.000000")
		got, err := newBackupHealthMonitor(store, "").baselineAt(now)
		if err != nil {
			t.Fatalf("baselineAt error: %v", err)
		}
		if !got.Equal(time.Unix(1788000000, 0)) {
			t.Errorf("baseline = %v, want unix timestamp 1788000000", got)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupWatchdogBaseline: "1788000000.000000"},
			[]string{settingBackupWatchdogBaseline},
			nil,
		)
	})

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "corrupt baseline is rearmed", raw: "1788000000junk"},
		{name: "future baseline is rearmed", raw: "1788872400.000000"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			store := newBackupHealthTestStore()
			store.seed(settingBackupWatchdogBaseline, tc.raw)
			got, err := newBackupHealthMonitor(store, "").baselineAt(now)
			if err != nil {
				t.Fatalf("baselineAt error: %v", err)
			}
			if !got.Equal(now) {
				t.Errorf("baseline = %v, want %v", got, now)
			}
			backupHealthTestExpectStore(t, store,
				map[string]string{settingBackupWatchdogBaseline: "1788868800.000000"},
				[]string{settingBackupWatchdogBaseline},
				[]backupHealthTestPut{{Key: settingBackupWatchdogBaseline, Value: "1788868800.000000"}},
			)
		})
	}

	t.Run("baseline read failure is returned without a write", func(t *testing.T) {
		wantErr := errors.New("read baseline")
		store := newBackupHealthTestStore()
		store.getErr = wantErr
		got, err := newBackupHealthMonitor(store, "").baselineAt(now)
		if err != wantErr {
			t.Errorf("error = %v, want %v", err, wantErr)
		}
		if !got.IsZero() {
			t.Errorf("baseline = %v, want zero", got)
		}
		backupHealthTestExpectStore(t, store, map[string]string{}, []string{settingBackupWatchdogBaseline}, nil)
	})

	t.Run("baseline write failure is returned after the attempted write", func(t *testing.T) {
		wantErr := errors.New("write baseline")
		store := newBackupHealthTestStore()
		store.putErr = wantErr
		got, err := newBackupHealthMonitor(store, "").baselineAt(now)
		if err != wantErr {
			t.Errorf("error = %v, want %v", err, wantErr)
		}
		if !got.IsZero() {
			t.Errorf("baseline = %v, want zero", got)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{},
			[]string{settingBackupWatchdogBaseline},
			[]backupHealthTestPut{{Key: settingBackupWatchdogBaseline, Value: "1788868800.000000"}},
		)
	})
}

func TestLoad(t *testing.T) {
	t.Run("missing verdict is nil and does not write", func(t *testing.T) {
		store := newBackupHealthTestStore()
		got, err := newBackupHealthMonitor(store, "").load()
		if err != nil {
			t.Fatalf("load error: %v", err)
		}
		if got != nil {
			t.Fatalf("state = %#v, want nil", got)
		}
		backupHealthTestExpectStore(t, store, map[string]string{}, []string{settingBackupHealth}, nil)
	})

	t.Run("valid verdict is decoded with every persisted field", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupHealth, `{"code":"failed","detail":"disk full","since_ts":1,"checked_ts":2,"newest_backup_ts":3}`)
		got, err := newBackupHealthMonitor(store, "").load()
		if err != nil {
			t.Fatalf("load error: %v", err)
		}
		want := &backupHealthState{Code: "failed", Detail: "disk full", SinceTS: 1, CheckedTS: 2, NewestBackupTS: 3}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("state = %#v, want %#v", got, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupHealth: `{"code":"failed","detail":"disk full","since_ts":1,"checked_ts":2,"newest_backup_ts":3}`},
			[]string{settingBackupHealth},
			nil,
		)
	})

	t.Run("malformed verdict returns the decode error without writing", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupHealth, "not-json")
		got, err := newBackupHealthMonitor(store, "").load()
		if got != nil {
			t.Errorf("state = %#v, want nil", got)
		}
		if err == nil || err.Error() != "invalid character 'o' in literal null (expecting 'u')" {
			t.Errorf("error = %v, want malformed JSON error", err)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupHealth: "not-json"},
			[]string{settingBackupHealth},
			nil,
		)
	})

	t.Run("verdict read failure returns nil without writing", func(t *testing.T) {
		wantErr := errors.New("read health")
		store := newBackupHealthTestStore()
		store.getErr = wantErr
		got, err := newBackupHealthMonitor(store, "").load()
		if got != nil {
			t.Errorf("state = %#v, want nil", got)
		}
		if err != wantErr {
			t.Errorf("error = %v, want %v", err, wantErr)
		}
		backupHealthTestExpectStore(t, store, map[string]string{}, []string{settingBackupHealth}, nil)
	})
}

func TestSave(t *testing.T) {
	t.Run("complete verdict is serialized and persisted", func(t *testing.T) {
		store := newBackupHealthTestStore()
		state := backupHealthState{Code: "failed", Detail: "disk full", SinceTS: 1.25, CheckedTS: 2.5, NewestBackupTS: 3.75}
		if err := newBackupHealthMonitor(store, "").save(state); err != nil {
			t.Fatalf("save error: %v", err)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupHealth: `{"code":"failed","detail":"disk full","since_ts":1.25,"checked_ts":2.5,"newest_backup_ts":3.75}`},
			nil,
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: `{"code":"failed","detail":"disk full","since_ts":1.25,"checked_ts":2.5,"newest_backup_ts":3.75}`}},
		)
	})

	t.Run("verdict write failure is returned after the attempted write", func(t *testing.T) {
		wantErr := errors.New("write health")
		store := newBackupHealthTestStore()
		store.putErr = wantErr
		state := backupHealthState{Code: "failed", Detail: "disk full"}
		err := newBackupHealthMonitor(store, "").save(state)
		if err != wantErr {
			t.Errorf("error = %v, want %v", err, wantErr)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{},
			nil,
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: `{"code":"failed","detail":"disk full","since_ts":0,"checked_ts":0,"newest_backup_ts":0}`}},
		)
	})
}

func TestNewestScheduledBackup(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	t.Run("returns the newest past scheduled backup and ignores other files", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		backupHealthTestWriteBackups(t, dbPath,
			"officraft-20260908-130000-scheduled.db",
			"officraft-20260908-110000-scheduled.db",
			"officraft-20260908-120000-manual.db",
			"officraft-20260908-115000-premigration.db",
			"officraft-20260908-not-a-time-scheduled.db",
		)
		before := backupHealthTestBackupNames(t, dbPath)
		got, ok := newestScheduledBackup(dbPath, now)
		after := backupHealthTestBackupNames(t, dbPath)
		if !ok {
			t.Fatal("newestScheduledBackup reported no backup")
		}
		if !got.Equal(time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC)) {
			t.Errorf("newest backup = %v, want 2026-09-08 11:00:00 UTC", got)
		}
		if !reflect.DeepEqual(after, before) {
			t.Errorf("backup names after read = %#v, want unchanged %#v", after, before)
		}
	})

	t.Run("returns no evidence when only future or non-scheduled files exist", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		backupHealthTestWriteBackups(t, dbPath,
			"officraft-20260908-130000-scheduled.db",
			"officraft-20260908-120000-manual.db",
		)
		got, ok := newestScheduledBackup(dbPath, now)
		if ok {
			t.Fatalf("newestScheduledBackup returned %v, want no backup", got)
		}
		if !got.IsZero() {
			t.Errorf("newest backup = %v, want zero", got)
		}
	})

	t.Run("returns no evidence when the backup directory is absent", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		got, ok := newestScheduledBackup(dbPath, now)
		if ok {
			t.Fatalf("newestScheduledBackup returned %v, want no backup", got)
		}
		if !got.IsZero() {
			t.Errorf("newest backup = %v, want zero", got)
		}
	})
}

func TestDecideBackupHealth(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name              string
		baseline          time.Time
		newest            time.Time
		hasNewest         bool
		lastFailureTS     float64
		lastFailureDetail string
		want              backupHealthState
	}{
		{
			name:      "fresh scheduled backup is healthy",
			baseline:  now.Add(-time.Hour),
			newest:    time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC),
			hasNewest: true,
			want:      backupHealthState{CheckedTS: 1788868800, NewestBackupTS: 1788865200},
		},
		{
			name:     "no backup inside the grace window stays unknown",
			baseline: now.Add(-11 * time.Hour),
			want:     backupHealthState{CheckedTS: 1788868800},
		},
		{
			name:     "no backup beyond the grace window is never ran",
			baseline: now.Add(-12*time.Hour - time.Minute),
			want: backupHealthState{
				Code:      "never_ran",
				Detail:    "no scheduled backup has ever landed (watching for 12h1m0s)",
				CheckedTS: 1788868800,
			},
		},
		{
			name:      "backup exactly at the freshness boundary stays healthy",
			baseline:  now.Add(-time.Hour),
			newest:    time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
			hasNewest: true,
			want:      backupHealthState{CheckedTS: 1788868800, NewestBackupTS: 1788825600},
		},
		{
			name:      "backup older than the freshness boundary is stale",
			baseline:  now.Add(-time.Hour),
			newest:    time.Date(2026, 9, 7, 23, 59, 0, 0, time.UTC),
			hasNewest: true,
			want: backupHealthState{
				Code:           "stale",
				Detail:         "newest scheduled backup is 12h1m0s old (alarm after 12h0m0s)",
				CheckedTS:      1788868800,
				NewestBackupTS: 1788825540,
			},
		},
		{
			name:              "a failure with no newer backup is unhealthy",
			baseline:          now.Add(-time.Hour),
			lastFailureTS:     1788868800,
			lastFailureDetail: "scheduled backup failed",
			want: backupHealthState{
				Code: "failed", Detail: "scheduled backup failed", CheckedTS: 1788868800,
			},
		},
		{
			name:              "a failure newer than the backup is unhealthy",
			baseline:          now.Add(-time.Hour),
			newest:            time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC),
			hasNewest:         true,
			lastFailureTS:     1788867000,
			lastFailureDetail: "new failure",
			want: backupHealthState{
				Code: "failed", Detail: "new failure", CheckedTS: 1788868800, NewestBackupTS: 1788865200,
			},
		},
		{
			name:              "a failure older than a fresh backup is cleared",
			baseline:          now.Add(-time.Hour),
			newest:            time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC),
			hasNewest:         true,
			lastFailureTS:     1788865000,
			lastFailureDetail: "old failure",
			want:              backupHealthState{CheckedTS: 1788868800, NewestBackupTS: 1788865200},
		},
		{
			name:              "an old failure does not replace a stale backup verdict",
			baseline:          now.Add(-time.Hour),
			newest:            time.Date(2026, 9, 7, 23, 59, 0, 0, time.UTC),
			hasNewest:         true,
			lastFailureTS:     1788820000,
			lastFailureDetail: "old failure",
			want: backupHealthState{
				Code:           "stale",
				Detail:         "newest scheduled backup is 12h1m0s old (alarm after 12h0m0s)",
				CheckedTS:      1788868800,
				NewestBackupTS: 1788825540,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := decideBackupHealth(now, tc.baseline, tc.newest, tc.hasNewest, tc.lastFailureTS, tc.lastFailureDetail)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("health = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	t.Run("first evaluation persists an unknown no-backup verdict", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		store := newBackupHealthTestStore()
		got, err := newBackupHealthMonitor(store, dbPath).evaluate(now)
		if err != nil {
			t.Fatalf("evaluate error: %v", err)
		}
		want := backupHealthState{CheckedTS: 1788868800}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("health = %#v, want %#v", got, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788868800.000000",
				settingBackupHealth:           `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":0}`,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{
				{Key: settingBackupWatchdogBaseline, Value: "1788868800.000000"},
				{Key: settingBackupHealth, Value: `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":0}`},
			},
		)
	})

	t.Run("continuing failure keeps the original incident start", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		store := newBackupHealthTestStore()
		store.seed(settingBackupWatchdogBaseline, "1788000000.000000")
		store.seed(settingBackupHealth, `{"code":"failed","detail":"disk full","since_ts":1788861600,"checked_ts":1788867000,"newest_backup_ts":0}`)
		got, err := newBackupHealthMonitor(store, dbPath).evaluate(now)
		if err != nil {
			t.Fatalf("evaluate error: %v", err)
		}
		want := backupHealthState{Code: "failed", Detail: "disk full", SinceTS: 1788861600, CheckedTS: 1788868800}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("health = %#v, want %#v", got, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788000000.000000",
				settingBackupHealth:           `{"code":"failed","detail":"disk full","since_ts":1788861600,"checked_ts":1788868800,"newest_backup_ts":0}`,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: `{"code":"failed","detail":"disk full","since_ts":1788861600,"checked_ts":1788868800,"newest_backup_ts":0}`}},
		)
	})

	t.Run("corrupt prior verdict is replaced by filesystem evidence", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		backupHealthTestWriteBackups(t, dbPath, "officraft-20260907-235900-scheduled.db")
		store := newBackupHealthTestStore()
		store.seed(settingBackupWatchdogBaseline, "1788000000.000000")
		store.seed(settingBackupHealth, "not-json")
		got, err := newBackupHealthMonitor(store, dbPath).evaluate(now)
		if err != nil {
			t.Fatalf("evaluate error: %v", err)
		}
		want := backupHealthState{
			Code: "stale", Detail: "newest scheduled backup is 12h1m0s old (alarm after 12h0m0s)",
			SinceTS: 1788868800, CheckedTS: 1788868800, NewestBackupTS: 1788825540,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("health = %#v, want %#v", got, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788000000.000000",
				settingBackupHealth:           `{"code":"stale","detail":"newest scheduled backup is 12h1m0s old (alarm after 12h0m0s)","since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":1788825540}`,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: `{"code":"stale","detail":"newest scheduled backup is 12h1m0s old (alarm after 12h0m0s)","since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":1788825540}`}},
		)
	})

	t.Run("baseline read failure returns no verdict and does not write", func(t *testing.T) {
		wantErr := errors.New("read baseline")
		store := newBackupHealthTestStore()
		store.getErr = wantErr
		got, err := newBackupHealthMonitor(store, filepath.Join(t.TempDir(), "officraft.db")).evaluate(now)
		if err != wantErr {
			t.Errorf("error = %v, want %v", err, wantErr)
		}
		if !reflect.DeepEqual(got, backupHealthState{}) {
			t.Errorf("health = %#v, want zero state", got)
		}
		backupHealthTestExpectStore(t, store, map[string]string{}, []string{settingBackupWatchdogBaseline}, nil)
	})

	t.Run("verdict write failure returns the derived verdict and attempted write", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		backupHealthTestWriteBackups(t, dbPath, "officraft-20260907-235900-scheduled.db")
		wantErr := errors.New("write health")
		store := newBackupHealthTestStore()
		store.seed(settingBackupWatchdogBaseline, "1788000000.000000")
		store.putErr = wantErr
		got, err := newBackupHealthMonitor(store, dbPath).evaluate(now)
		if err != wantErr {
			t.Errorf("error = %v, want %v", err, wantErr)
		}
		want := backupHealthState{
			Code: "stale", Detail: "newest scheduled backup is 12h1m0s old (alarm after 12h0m0s)",
			SinceTS: 1788868800, CheckedTS: 1788868800, NewestBackupTS: 1788825540,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("health = %#v, want %#v", got, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupWatchdogBaseline: "1788000000.000000"},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: `{"code":"stale","detail":"newest scheduled backup is 12h1m0s old (alarm after 12h0m0s)","since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":1788825540}`}},
		)
	})
}

func TestNoteScheduledOutcome(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	t.Run("run error is persisted immediately when no backup landed", func(t *testing.T) {
		store := newBackupHealthTestStore()
		m := newBackupHealthMonitor(store, filepath.Join(t.TempDir(), "officraft.db"))
		m.noteScheduledOutcome(backupResult{}, errors.New("VACUUM INTO failed"), now)
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788868800.000000",
				settingBackupHealth:           `{"code":"failed","detail":"scheduled backup failed: VACUUM INTO failed — no new retreat point was created","since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":0}`,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{
				{Key: settingBackupWatchdogBaseline, Value: "1788868800.000000"},
				{Key: settingBackupHealth, Value: `{"code":"failed","detail":"scheduled backup failed: VACUUM INTO failed — no new retreat point was created","since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":0}`},
			},
		)
	})

	t.Run("skip is persisted immediately with the newest scheduled evidence", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		backupHealthTestWriteBackups(t, dbPath, "officraft-20260908-100000-scheduled.db")
		store := newBackupHealthTestStore()
		store.seed(settingBackupWatchdogBaseline, "1788000000.000000")
		newBackupHealthMonitor(store, dbPath).noteScheduledOutcome(backupResult{Skipped: "only 1 MB free"}, nil, now)
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788000000.000000",
				settingBackupHealth:           `{"code":"failed","detail":"scheduled backup skipped: only 1 MB free — no new retreat point was created","since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":1788861600}`,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: `{"code":"failed","detail":"scheduled backup skipped: only 1 MB free — no new retreat point was created","since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":1788861600}`}},
		)
	})

	t.Run("successful scheduled outcome clears a prior incident", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		backupHealthTestWriteBackups(t, dbPath, "officraft-20260908-110000-scheduled.db")
		store := newBackupHealthTestStore()
		store.seed(settingBackupWatchdogBaseline, "1788000000.000000")
		store.seed(settingBackupHealth, `{"code":"failed","detail":"old failure","since_ts":1788861600,"checked_ts":1788867000,"newest_backup_ts":0}`)
		newBackupHealthMonitor(store, dbPath).noteScheduledOutcome(backupResult{Reason: backupReasonScheduled}, nil, now)
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788000000.000000",
				settingBackupHealth:           `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":1788865200}`,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":1788865200}`}},
		)
	})

	t.Run("repeated failure keeps the original incident start", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupWatchdogBaseline, "1788000000.000000")
		store.seed(settingBackupHealth, `{"code":"failed","detail":"old failure","since_ts":1788861600,"checked_ts":1788867000,"newest_backup_ts":0}`)
		newBackupHealthMonitor(store, filepath.Join(t.TempDir(), "officraft.db")).noteScheduledOutcome(backupResult{}, errors.New("disk full"), now)
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788000000.000000",
				settingBackupHealth:           `{"code":"failed","detail":"scheduled backup failed: disk full — no new retreat point was created","since_ts":1788861600,"checked_ts":1788868800,"newest_backup_ts":0}`,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: `{"code":"failed","detail":"scheduled backup failed: disk full — no new retreat point was created","since_ts":1788861600,"checked_ts":1788868800,"newest_backup_ts":0}`}},
		)
	})

	t.Run("run error takes precedence over skip detail", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupWatchdogBaseline, "1788000000.000000")
		newBackupHealthMonitor(store, filepath.Join(t.TempDir(), "officraft.db")).noteScheduledOutcome(
			backupResult{Skipped: "only 1 MB free"},
			errors.New("VACUUM INTO failed"),
			now,
		)
		want := `{"code":"failed","detail":"scheduled backup failed: VACUUM INTO failed — no new retreat point was created","since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":0}`
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788000000.000000",
				settingBackupHealth:           want,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{{Key: settingBackupHealth, Value: want}},
		)
	})

	t.Run("baseline read failure leaves the store unchanged", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.getErr = errors.New("read baseline")
		newBackupHealthMonitor(store, filepath.Join(t.TempDir(), "officraft.db")).noteScheduledOutcome(backupResult{}, nil, now)
		backupHealthTestExpectStore(t, store, map[string]string{}, []string{settingBackupWatchdogBaseline}, nil)
	})

	t.Run("nil monitor ignores a scheduled outcome", func(t *testing.T) {
		var monitor *backupHealthMonitor
		monitor.noteScheduledOutcome(backupResult{}, errors.New("ignored"), now)
	})
}

func TestReport(t *testing.T) {
	t.Run("nil monitor reports that watching is disabled", func(t *testing.T) {
		var monitor *backupHealthMonitor
		got := monitor.report()
		want := `{"code":"","detail":"backup health is not being watched on this server","stale_after_secs":43200,"status":"unknown"}`
		if raw := backupHealthTestReportJSON(t, got); raw != want {
			t.Errorf("report = %s, want %s", raw, want)
		}
	})

	t.Run("absent verdict reports an unevaluated watchdog", func(t *testing.T) {
		store := newBackupHealthTestStore()
		got := newBackupHealthMonitor(store, "").report()
		want := `{"code":"","detail":"the backup watchdog has not reported yet","stale_after_secs":43200,"status":"unknown"}`
		if raw := backupHealthTestReportJSON(t, got); raw != want {
			t.Errorf("report = %s, want %s", raw, want)
		}
		backupHealthTestExpectStore(t, store, map[string]string{}, []string{settingBackupHealth}, nil)
	})

	t.Run("evaluated without a backup remains unknown and names the missing evidence", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupHealth, `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":0}`)
		got := newBackupHealthMonitor(store, "").report()
		want := `{"checked_ts":1788868800,"code":"","detail":"no scheduled backup has landed yet","stale_after_secs":43200,"status":"unknown"}`
		if raw := backupHealthTestReportJSON(t, got); raw != want {
			t.Errorf("report = %s, want %s", raw, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupHealth: `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":0}`},
			[]string{settingBackupHealth},
			nil,
		)
	})

	t.Run("healthy verdict includes timestamps and computed age", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupHealth, `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":1788865200}`)
		got := newBackupHealthMonitor(store, "").report()
		want := `{"checked_ts":1788868800,"code":"","detail":"","newest_backup_age_secs":3600,"newest_backup_ts":1788865200,"stale_after_secs":43200,"status":"healthy"}`
		if raw := backupHealthTestReportJSON(t, got); raw != want {
			t.Errorf("report = %s, want %s", raw, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupHealth: `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":1788865200}`},
			[]string{settingBackupHealth},
			nil,
		)
	})

	t.Run("unhealthy verdict includes its code detail and incident start", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupHealth, `{"code":"failed","detail":"disk full","since_ts":1788861600,"checked_ts":1788868800,"newest_backup_ts":0}`)
		got := newBackupHealthMonitor(store, "").report()
		want := `{"checked_ts":1788868800,"code":"failed","detail":"disk full","since_ts":1788861600,"stale_after_secs":43200,"status":"unhealthy"}`
		if raw := backupHealthTestReportJSON(t, got); raw != want {
			t.Errorf("report = %s, want %s", raw, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupHealth: `{"code":"failed","detail":"disk full","since_ts":1788861600,"checked_ts":1788868800,"newest_backup_ts":0}`},
			[]string{settingBackupHealth},
			nil,
		)
	})

	t.Run("malformed verdict fails closed as unevaluated", func(t *testing.T) {
		store := newBackupHealthTestStore()
		store.seed(settingBackupHealth, "not-json")
		got := newBackupHealthMonitor(store, "").report()
		want := `{"code":"","detail":"the backup watchdog has not reported yet","stale_after_secs":43200,"status":"unknown"}`
		if raw := backupHealthTestReportJSON(t, got); raw != want {
			t.Errorf("report = %s, want %s", raw, want)
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{settingBackupHealth: "not-json"},
			[]string{settingBackupHealth},
			nil,
		)
	})
}

func TestArmBackupHealth(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	t.Run("arming evaluates immediately and persists the first verdict", func(t *testing.T) {
		store := newBackupHealthTestStore()
		got := armBackupHealth(store, filepath.Join(t.TempDir(), "officraft.db"), now)
		if got == nil {
			t.Fatal("armBackupHealth returned nil monitor")
		}
		backupHealthTestExpectStore(t, store,
			map[string]string{
				settingBackupWatchdogBaseline: "1788868800.000000",
				settingBackupHealth:           `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":0}`,
			},
			[]string{settingBackupWatchdogBaseline, settingBackupHealth},
			[]backupHealthTestPut{
				{Key: settingBackupWatchdogBaseline, Value: "1788868800.000000"},
				{Key: settingBackupHealth, Value: `{"code":"","detail":"","since_ts":0,"checked_ts":1788868800,"newest_backup_ts":0}`},
			},
		)
	})

	t.Run("arming logs a warning and still returns a monitor when the first read fails", func(t *testing.T) {
		wantErr := errors.New("read baseline")
		store := newBackupHealthTestStore()
		store.getErr = wantErr
		var got *backupHealthMonitor
		stderr := hubTestStderr(t, func() {
			got = armBackupHealth(store, filepath.Join(t.TempDir(), "officraft.db"), now)
		})
		if got == nil {
			t.Fatal("armBackupHealth returned nil monitor")
		}
		if stderr != "[backup] WARNING could not record backup health: read baseline\n" {
			t.Errorf("stderr = %q, want %q", stderr, "[backup] WARNING could not record backup health: read baseline\n")
		}
		backupHealthTestExpectStore(t, store, map[string]string{}, []string{settingBackupWatchdogBaseline}, nil)
	})
}

func TestStartBackupHealthWatchdog(t *testing.T) {
	if os.Getenv("OFFICRAFT_BACKUP_HEALTH_WATCHDOG_CHILD") == "1" {
		store := newBackupHealthTestStore()
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		startBackupHealthWatchdog(newBackupHealthMonitor(store, dbPath), 5*time.Millisecond)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			values, _, puts := store.snapshot()
			raw, ok := values[settingBackupHealth]
			if ok {
				var got backupHealthState
				if err := json.Unmarshal([]byte(raw), &got); err != nil {
					t.Fatalf("decode watchdog verdict: %v", err)
				}
				if got.Code != "" || got.Detail != "" || got.NewestBackupTS != 0 || got.CheckedTS <= 0 {
					t.Fatalf("watchdog verdict = %#v, want a fresh empty verdict", got)
				}
				if len(puts) < 2 {
					t.Fatalf("puts = %#v, want baseline and health writes", puts)
				}
				if puts[0].Key != settingBackupWatchdogBaseline || puts[1].Key != settingBackupHealth {
					t.Fatalf("first watchdog puts = %#v, want baseline then health", puts[:2])
				}
				if values[settingBackupWatchdogBaseline] == "" {
					t.Fatal("watchdog baseline write is empty")
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("watchdog did not persist a verdict")
	}

	t.Run("nil monitor does not start a watchdog", func(t *testing.T) {
		startBackupHealthWatchdog(nil, time.Millisecond)
	})

	t.Run("watchdog evaluates and persists on its tick", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := osexec.CommandContext(ctx, os.Args[0], "-test.run", "^TestStartBackupHealthWatchdog$")
		cmd.Env = append(os.Environ(), "OFFICRAFT_BACKUP_HEALTH_WATCHDOG_CHILD=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("watchdog child failed: %v\n%s", err, output)
		}
	})
}
