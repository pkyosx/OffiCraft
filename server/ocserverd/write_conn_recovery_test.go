package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Each test here takes the write connection away from its user for a while
// and then gives it back; what is checked is that the user fails for that
// while and works again afterwards, instead of failing from then on.

// recoveryLog is the standard logger captured behind a lock, so a test can read
// it while other goroutines are still writing, and wait for one line.
type recoveryLog struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	watches []recoveryWatch
}

type recoveryWatch struct {
	needle string
	seen   chan struct{}
}

func recoveryCaptureLog(t *testing.T) *recoveryLog {
	t.Helper()
	l := &recoveryLog{}
	oldWriter, oldFlags, oldPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(l)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})
	return l
}

func (l *recoveryLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Write(p)
	kept := l.watches[:0]
	for _, w := range l.watches {
		if strings.Contains(l.buf.String(), w.needle) {
			close(w.seen)
			continue
		}
		kept = append(kept, w)
	}
	l.watches = kept
	return len(p), nil
}

func (l *recoveryLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// once answers a channel closed when a line containing needle is logged.
func (l *recoveryLog) once(needle string) <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := make(chan struct{})
	if strings.Contains(l.buf.String(), needle) {
		close(seen)
		return seen
	}
	l.watches = append(l.watches, recoveryWatch{needle: needle, seen: seen})
	return seen
}

// recoveryWithin runs call under reentryAnswerWithin, so a wait with no limit
// fails by name instead of hanging the suite.
func recoveryWithin(t *testing.T, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	select {
	case <-done:
	case <-time.After(reentryAnswerWithin):
		t.Fatalf("%s did not return within %s", what, reentryAnswerWithin)
	}
}

// recoveryLockDatabase takes SQLite's write lock through a handle of its own,
// the way a shell sqlite3 or another process would, and answers its release.
func recoveryLockDatabase(t *testing.T, path string) (release func()) {
	t.Helper()
	other, err := sql.Open("sqlite", sqliteWriteDSN(path))
	if err != nil {
		t.Fatalf("open another handle: %v", err)
	}
	t.Cleanup(func() { other.Close() })
	conn, err := other.Conn(context.Background())
	if err != nil {
		t.Fatalf("another handle's connection: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatalf("take the write lock: %v", err)
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			if _, err := conn.ExecContext(context.Background(), `ROLLBACK`); err != nil {
				t.Errorf("release the write lock: %v", err)
			}
			conn.Close()
		})
	}
	t.Cleanup(release)
	return release
}

// liveConn sends requests to h through a real HTTP server, all over one
// keep-alive connection: net/http serves consecutive requests on one connection
// from the same goroutine, so whatever a request leaves behind on its goroutine
// is there for the next one.
type liveConn struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
	token  string
}

func newLiveConn(t *testing.T, h http.Handler, token string) *liveConn {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	transport := &http.Transport{MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1}
	t.Cleanup(transport.CloseIdleConnections)
	return &liveConn{t: t, srv: srv, client: &http.Client{Transport: transport, Timeout: reentryAnswerWithin}, token: token}
}

// do answers the status, the JSON body, and whether the request went over a
// connection an earlier request had used. A transport error is returned, not
// failed on: a handler that panics answers with a dropped connection.
func (c *liveConn) do(method, target, body string) (int, map[string]any, bool, error) {
	c.t.Helper()
	var reused bool
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace),
		method, c.srv.URL+target, strings.NewReader(body))
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, reused, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, reused, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		c.t.Fatalf("%s %s: non-JSON body (%d): %s", method, target, resp.StatusCode, raw)
	}
	return resp.StatusCode, data, reused, nil
}

func TestARequestRefusedByAnotherHandlesWriteLockLeavesTheNextRequestOnItsConnectionWorking(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d, _, path := windowDAL(t, shape)
			_, h, _, owner := newAPITestServerOn(t, d)
			t1 := dalPutTask(t, d, windowOpenTask("T-1"))
			conn := newLiveConn(t, h, owner)
			release := recoveryLockDatabase(t, path)

			status, body, _, err := conn.do("POST", "/api/tasks/T-1/priority", `{"priority":"low"}`)

			if err != nil {
				t.Fatalf("while another handle holds the write lock: %v", err)
			}
			if status != http.StatusInternalServerError {
				t.Fatalf("while another handle holds the write lock: want 500, got %d (%v)", status, body)
			}
			apiWantError(t, body, "internal_error", "internal error: database is locked (5) (SQLITE_BUSY)")
			dalWantTask(t, d, t1)

			release()
			status, body, reused, err := conn.do("POST", "/api/tasks/T-1/priority", `{"priority":"low"}`)

			if err != nil {
				t.Fatalf("after the lock is released: %v", err)
			}
			if !reused {
				t.Fatalf("the second request did not go over the first one's connection, so it was not served where the first one failed")
			}
			if status != http.StatusOK {
				t.Fatalf("after the lock is released, on the same connection: want 200, got %d (%v)", status, body)
			}
			apiWantBody(t, body, map[string]any{"task_id": "T-1", "priority": "low", "frozen_by": ""})
		})
	}
}

func TestAScheduledBackupGivesUpWhileTheWriteConnectionIsHeldAndTheNextOneLands(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d, _, path := windowDAL(t, shape)
			d.wdb.limit = stallTestWaitLimit
			logs := recoveryCaptureLog(t)
			first := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
			tx, err := d.wdb.Begin()
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			held := true
			t.Cleanup(func() {
				if held {
					_ = tx.Rollback()
				}
			})

			var taken bool
			recoveryWithin(t, "a backup tick while the write connection is held", func() {
				taken = backupTick(d.wdb, path, first, nil)
			})

			if taken {
				t.Fatalf("the tick reports a backup taken while the write connection was held")
			}
			if got := backupHealthTestBackupNames(t, path); len(got) != 0 {
				t.Fatalf("backups after the failed tick = %v, want none", got)
			}
			reentryWantLogLine(t, logs.String(),
				"[wdb] ERROR: gave up after 300ms waiting for the write connection; caller: runDatabaseBackup (backup.go:")
			reentryWantLogLine(t, logs.String(),
				"[backup] FAILED (scheduled): vacuum into ")

			_ = tx.Rollback()
			held = false
			recoveryWithin(t, "the next backup tick", func() {
				taken = backupTick(d.wdb, path, first.Add(backupInterval), nil)
			})

			if !taken {
				t.Fatalf("the next tick took no backup after the write connection was released; log:\n%s", logs.String())
			}
			want := []string{"officraft-20260908-180000-scheduled.db"}
			if got := backupHealthTestBackupNames(t, path); !reflect.DeepEqual(got, want) {
				t.Fatalf("backups = %v, want %v", got, want)
			}
		})
	}
}

func TestABackupThatCouldNotReadItsRetainSettingDeletesNoOldBackups(t *testing.T) {
	for _, shape := range windowDALShapes {
		t.Run(shape, func(t *testing.T) {
			d, _, path := windowDAL(t, shape)
			d.wdb.limit = 500 * time.Millisecond
			if err := d.PutSetting(settingBackupRetain, "10"); err != nil {
				t.Fatalf("set retain: %v", err)
			}
			old := []string{
				"officraft-20260901-120000-scheduled.db",
				"officraft-20260902-120000-scheduled.db",
				"officraft-20260903-120000-scheduled.db",
				"officraft-20260904-120000-scheduled.db",
				"officraft-20260905-120000-scheduled.db",
				"officraft-20260906-120000-scheduled.db",
			}
			backupHealthTestWriteBackups(t, path, old...)
			logs := recoveryCaptureLog(t)
			retainGaveUp := logs.once("waiting for the write connection; caller: liveBackupRetain (backup.go:")
			tx, err := d.wdb.Begin()
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			held := true
			t.Cleanup(func() {
				if held {
					_ = tx.Rollback()
				}
			})

			// The connection is held while the tick reads the retain setting and
			// given back as soon as that read gives up, so the snapshot itself runs.
			tick := make(chan bool, 1)
			go func() { tick <- backupTick(d.wdb, path, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), nil) }()
			select {
			case <-retainGaveUp:
			case <-time.After(reentryAnswerWithin):
				t.Fatalf("the retain read never gave up; log:\n%s", logs.String())
			}
			_ = tx.Rollback()
			held = false
			var taken bool
			select {
			case taken = <-tick:
			case <-time.After(reentryAnswerWithin):
				t.Fatalf("the tick did not return within %s", reentryAnswerWithin)
			}

			if !taken {
				t.Fatalf("the snapshot did not land after the connection was released; log:\n%s", logs.String())
			}
			want := []string{
				"officraft-20260908-120000-scheduled.db",
				"officraft-20260906-120000-scheduled.db",
				"officraft-20260905-120000-scheduled.db",
				"officraft-20260904-120000-scheduled.db",
				"officraft-20260903-120000-scheduled.db",
				"officraft-20260902-120000-scheduled.db",
				"officraft-20260901-120000-scheduled.db",
			}
			if got := backupHealthTestBackupNames(t, path); !reflect.DeepEqual(got, want) {
				t.Fatalf("backups = %v, want %v", got, want)
			}
			reentryWantLogLine(t, logs.String(), "[backup] could not read the retain setting in time (context deadline exceeded); deleting no old backups this round")
		})
	}
}
