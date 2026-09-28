package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Both deadlocks are staged through a driver seam on the write connection: the
// first write statement a handler issues inside its transaction runs stall
// first, while that transaction holds the only write connection. The seam fires
// once and never sleeps; how long anything waits is decided by the pool. Each
// wait is one the pool cannot see through: another goroutine's write, and a
// plain sync.Mutex (the server's own locks refuse a transaction holder
// outright, lock_in_tx_test.go).

const (
	stallTestWaitLimit = 300 * time.Millisecond
	// How long a test gives a request to answer. Far above stallTestWaitLimit,
	// far below the package timeout: an unbounded wait fails here, by name.
	stallTestAnswerWithin = 5 * time.Second
)

type stallSeam struct {
	mu      sync.Mutex
	stall   func() error
	fired   int
	firedOn string
}

func (s *stallSeam) arm(stall func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stall = stall
}

func (s *stallSeam) sawSQL(query string) error {
	verb := strings.ToUpper(strings.TrimSpace(query))
	if !strings.HasPrefix(verb, "INSERT") && !strings.HasPrefix(verb, "UPDATE") {
		return nil
	}
	s.mu.Lock()
	stall := s.stall
	s.stall = nil
	if stall != nil {
		s.fired++
		s.firedOn = strings.TrimSpace(query)
	}
	s.mu.Unlock()
	if stall == nil {
		return nil
	}
	return stall()
}

func (s *stallSeam) wantFiredOnce(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fired != 1 {
		t.Fatalf("the stall fired %d times, want exactly 1: the request never wrote inside its transaction", s.fired)
	}
	if !strings.HasPrefix(s.firedOn, "INSERT INTO task (") {
		t.Fatalf("the stall fired on %q, want the handler's task write inside its transaction", s.firedOn)
	}
}

type stallConnector struct {
	dsn  string
	drv  driver.Driver
	seam *stallSeam
}

func (c stallConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.drv.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return stallConn{inner: conn, seam: c.seam}, nil
}

func (c stallConnector) Driver() driver.Driver { return c.drv }

type stallConn struct {
	inner driver.Conn
	seam  *stallSeam
}

func (c stallConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c stallConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.seam.sawSQL(query); err != nil {
		return nil, err
	}
	if pc, ok := c.inner.(driver.ConnPrepareContext); ok {
		return pc.PrepareContext(ctx, query)
	}
	return c.inner.Prepare(query)
}

func (c stallConn) Close() error { return c.inner.Close() }

func (c stallConn) Begin() (driver.Tx, error) {
	return c.inner.Begin() //nolint:staticcheck // the wrapped driver's own fallback
}

func (c stallConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if bt, ok := c.inner.(driver.ConnBeginTx); ok {
		return bt.BeginTx(ctx, opts)
	}
	return c.inner.Begin() //nolint:staticcheck // the wrapped driver's own fallback
}

func (c stallConn) CheckNamedValue(nv *driver.NamedValue) error {
	if nvc, ok := c.inner.(driver.NamedValueChecker); ok {
		return nvc.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// stallDAL is the serve shape — one write connection, a separate read pool —
// with the seam on the write connection and the wait limit shortened.
func stallDAL(t *testing.T) (*DAL, *stallSeam) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stall.db")
	plain, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := runMigrations(plain); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	drv := plain.Driver()
	plain.Close()

	seam := &stallSeam{}
	wdb := sql.OpenDB(stallConnector{dsn: sqliteWriteDSN(path), drv: drv, seam: seam})
	wdb.SetMaxOpenConns(1)
	t.Cleanup(func() { wdb.Close() })
	rdb, err := openSQLiteReadPool(path)
	if err != nil {
		t.Fatalf("open read pool: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })
	d := NewDALPools(wdb, rdb)
	d.wdb.limit = stallTestWaitLimit
	return d, seam
}

type stallAnswer struct {
	status int
	body   map[string]any
}

// stallRequestAsync starts a request and answers a function that waits for it
// at most stallTestAnswerWithin. The request goroutine never touches t: an
// abandoned one may still be running after the test has ended.
func stallRequestAsync(t *testing.T, h http.Handler, token, target, body string) func() stallAnswer {
	t.Helper()
	req := httptest.NewRequest("POST", target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- rec
	}()
	return func() stallAnswer {
		t.Helper()
		select {
		case rec := <-done:
			var parsed map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				t.Fatalf("POST %s: non-JSON body (%d): %s", target, rec.Code, rec.Body.Bytes())
			}
			return stallAnswer{status: rec.Code, body: parsed}
		case <-time.After(stallTestAnswerWithin):
			t.Fatalf("POST %s did not answer within %s: its wait for the write connection has no limit", target, stallTestAnswerWithin)
			return stallAnswer{}
		}
	}
}

func stallRequest(t *testing.T, h http.Handler, token, target, body string) stallAnswer {
	t.Helper()
	return stallRequestAsync(t, h, token, target, body)()
}

func stallWantLogLine(t *testing.T, logs string, want ...string) {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if !strings.HasPrefix(line, "[wdb] ERROR: gave up after 300ms waiting for the write connection; caller: ") {
			continue
		}
		missing := false
		for _, w := range want {
			if !strings.Contains(line, w) {
				missing = true
			}
		}
		if !missing {
			return
		}
	}
	t.Fatalf("no log line saying the write-connection wait gave up in %v; log was:\n%s", want, logs)
}

const stallGaveUp = "internal error: timed out waiting for the write connection (300ms): context deadline exceeded"

func TestATransactionWaitingOnAnotherGoroutinesWriteFailsInsteadOfStoppingTheStation(t *testing.T) {
	d, seam := stallDAL(t)
	_, h, _, owner := newAPITestServerOn(t, d)
	task := dalPutTask(t, d, windowOpenTask("T-1"))
	logs := apiCaptureStandardLog(t)
	seam.arm(func() error {
		// A write on the transaction's own goroutine joins the transaction; one
		// handed to another goroutine does not, and waits for the connection
		// the transaction holds while the transaction waits for it.
		done := make(chan error, 1)
		go func() { done <- d.TouchTaskUpdatedTS("T-1", 1800000000) }()
		return <-done
	})

	started := time.Now()
	got := stallRequest(t, h, owner, "/api/tasks/T-1/priority", `{"priority":"low"}`)

	seam.wantFiredOnce(t)
	if got.status != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d (%v)", got.status, got.body)
	}
	if took := time.Since(started); took < stallTestWaitLimit {
		t.Fatalf("answered after %s, before the %s limit: it did not wait for the connection", took, stallTestWaitLimit)
	}
	apiWantError(t, got.body, "internal_error", stallGaveUp)
	stallWantLogLine(t, logs.String(), "caller: (*DAL).TouchTaskUpdatedTS (dal_tasks.go:")
	dalWantTask(t, d, task)

	got = stallRequest(t, h, owner, "/api/tasks/T-1/priority", `{"priority":"low"}`)

	if got.status != http.StatusOK {
		t.Fatalf("the next write must land: want 200, got %d (%v)", got.status, got.body)
	}
	apiWantBody(t, got.body, map[string]any{"task_id": "T-1", "priority": "low", "frozen_by": ""})
}

func TestAWriterHoldingAMutexGivesUpSoTheTransactionWaitingOnItFinishes(t *testing.T) {
	d, seam := stallDAL(t)
	_, h, _, owner := newAPITestServerOn(t, d)
	dalPutTask(t, d, windowOpenTask("T-1"))
	t2 := dalPutTask(t, d, windowOpenTask("T-2"))
	logs := apiCaptureStandardLog(t)

	var mu sync.Mutex
	mu.Lock()
	held := true
	t.Cleanup(func() {
		if held {
			mu.Unlock()
		}
	})
	inTx := make(chan struct{})
	seam.arm(func() error {
		close(inTx)
		mu.Lock()
		defer mu.Unlock()
		return nil
	})

	// A: holds the write connection inside its transaction, waiting for mu.
	answerA := stallRequestAsync(t, h, owner, "/api/tasks/T-1/priority", `{"priority":"low"}`)
	select {
	case <-inTx:
	case <-time.After(stallTestAnswerWithin):
		t.Fatalf("the first request never reached its transaction's write")
	}
	// B: holds mu, waiting for the write connection A holds.
	started := time.Now()
	gotB := stallRequest(t, h, owner, "/api/tasks/T-2/priority", `{"priority":"high"}`)
	took := time.Since(started)
	mu.Unlock()
	held = false
	gotA := answerA()

	seam.wantFiredOnce(t)
	if gotB.status != http.StatusInternalServerError {
		t.Fatalf("the writer holding the mutex: want 500, got %d (%v)", gotB.status, gotB.body)
	}
	if took < stallTestWaitLimit {
		t.Fatalf("B answered after %s, before the %s limit: it did not wait for the connection", took, stallTestWaitLimit)
	}
	apiWantError(t, gotB.body, "internal_error", stallGaveUp)
	stallWantLogLine(t, logs.String(), "caller: (*DAL).inTx (dal.go:",
		"(*apiServer).HandleSetTaskPriorityApiTasksTaskIdPriorityPost (api_tasks.go:")
	if gotA.status != http.StatusOK {
		t.Fatalf("the transaction waiting on the mutex must finish: want 200, got %d (%v)", gotA.status, gotA.body)
	}
	apiWantBody(t, gotA.body, map[string]any{"task_id": "T-1", "priority": "low", "frozen_by": ""})
	dalWantTask(t, d, t2)

	got := stallRequest(t, h, owner, "/api/tasks/T-2/priority", `{"priority":"high"}`)

	if got.status != http.StatusOK {
		t.Fatalf("the next write must land: want 200, got %d (%v)", got.status, got.body)
	}
	apiWantBody(t, got.body, map[string]any{"task_id": "T-2", "priority": "high", "frozen_by": ""})
}
