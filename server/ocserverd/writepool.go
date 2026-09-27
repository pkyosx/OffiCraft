package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"ocserverd/txguard"
)

// writeConnWaitLimit bounds only the wait for the write pool's single
// connection. A wait that long means a deadlock, not load: the transaction's
// holder is waiting on something — another goroutine, a lock outside txguard —
// that is itself waiting here. Unbounded, that stops every write on the station
// with nothing in the log; bounded, one operation fails and whatever it held is
// released.
const writeConnWaitLimit = 30 * time.Second

var errWriteConnWait = errors.New("timed out waiting for the write connection")

// writePool is the only way to the write connection. Every method bounds the
// wait and never the work.
//
// A goroutine that holds the pool's transaction and asks the pool again — an
// Exec, a Query, a DAL method, a nested inTx — runs on that transaction instead
// of waiting for the connection it holds. "The same goroutine" is decided from
// the runtime's goroutine id, never from a context the caller passes, so no
// caller can opt out of it. A nested Begin opens a SAVEPOINT: an inner failure
// undoes only the inner writes, and the outer caller decides whether the whole
// transaction goes (every caller today returns the error, which rolls it all
// back).
//
// ⚠️ Do not "simplify" to BeginTx(ctx)/ExecContext(ctx) with a deadline ctx:
// database/sql rolls a transaction back when its BeginTx ctx ends, and the
// sqlite driver interrupts a statement when its ctx ends, so a long but healthy
// transaction or VACUUM would be killed at the deadline. A ctx given to
// DB.Conn governs the acquisition only.
type writePool struct {
	raw   *sql.DB
	limit time.Duration
	// beforeCommit is a test seam: it runs on the holding goroutine inside the
	// outermost transaction, just before COMMIT, and its error aborts the commit.
	beforeCommit func() error

	mu     sync.Mutex
	holder int64
	tx     *sql.Tx
	depth  int
}

func newWritePool(db *sql.DB) *writePool {
	return &writePool{raw: db, limit: writeConnWaitLimit}
}

func (p *writePool) conn() (*sql.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.limit)
	defer cancel()
	c, err := p.raw.Conn(ctx)
	if err == nil {
		return c, nil
	}
	if ctx.Err() == nil {
		return nil, err
	}
	log.Printf("[wdb] ERROR: gave up after %s waiting for the write connection; caller: %s", p.limit, writeConnCaller())
	return nil, fmt.Errorf("%w (%s): %w", errWriteConnWait, p.limit, err)
}

// releaseWhenDone hands the connection back once the tx or rows are finished:
// sql.Conn.Close blocks until then, so it runs on its own goroutine.
func releaseWhenDone(c *sql.Conn) {
	go func() { _ = c.Close() }()
}

// mine is the pool's open transaction when the calling goroutine holds it.
func (p *writePool) mine() *sql.Tx {
	p.mu.Lock()
	tx, holder := p.tx, p.holder
	p.mu.Unlock()
	if tx == nil || holder != txguard.GoID() {
		return nil
	}
	return tx
}

func (p *writePool) Exec(query string, args ...any) (sql.Result, error) {
	if tx := p.mine(); tx != nil {
		return tx.Exec(query, args...)
	}
	c, err := p.conn()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.ExecContext(context.Background(), query, args...)
}

func (p *writePool) Begin() (*writeTx, error) {
	gid := txguard.GoID()
	p.mu.Lock()
	if p.tx != nil && p.holder == gid {
		p.depth++
		tx, sp := p.tx, fmt.Sprintf("wtx_%d", p.depth)
		p.mu.Unlock()
		if _, err := tx.Exec("SAVEPOINT " + sp); err != nil {
			p.unnest()
			return nil, err
		}
		return &writeTx{p: p, tx: tx, savepoint: sp}, nil
	}
	p.mu.Unlock()
	c, err := p.conn()
	if err != nil {
		return nil, err
	}
	tx, err := c.BeginTx(context.Background(), nil)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	releaseWhenDone(c)
	p.mu.Lock()
	p.holder, p.tx, p.depth = gid, tx, 0
	p.mu.Unlock()
	txguard.Enter(gid)
	return &writeTx{p: p, tx: tx, gid: gid}, nil
}

func (p *writePool) unnest() {
	p.mu.Lock()
	p.depth--
	p.mu.Unlock()
}

func (p *writePool) release(gid int64) {
	p.mu.Lock()
	p.holder, p.tx, p.depth = 0, nil, 0
	p.mu.Unlock()
	txguard.Leave(gid)
}

func (p *writePool) Query(query string, args ...any) (*sql.Rows, error) {
	if tx := p.mine(); tx != nil {
		return tx.Query(query, args...)
	}
	c, err := p.conn()
	if err != nil {
		return nil, err
	}
	rows, err := c.QueryContext(context.Background(), query, args...)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	releaseWhenDone(c)
	return rows, nil
}

// QueryRow has no error of its own to return: after a timed-out wait, Scan
// reports context.DeadlineExceeded, and the log line says which wait it was.
func (p *writePool) QueryRow(query string, args ...any) *sql.Row {
	if tx := p.mine(); tx != nil {
		return tx.QueryRow(query, args...)
	}
	c, err := p.conn()
	if errors.Is(err, errWriteConnWait) {
		expired, cancel := context.WithDeadline(context.Background(), time.Time{})
		defer cancel()
		return p.raw.QueryRowContext(expired, query, args...)
	}
	if err != nil {
		// Not a wait that ran out (a closed pool): let the pool report its own error.
		return p.raw.QueryRowContext(context.Background(), query, args...)
	}
	row := c.QueryRowContext(context.Background(), query, args...)
	releaseWhenDone(c)
	return row
}

func (p *writePool) Close() error { return p.raw.Close() }

// writeTx is a transaction on the write pool, or a SAVEPOINT inside one when
// its goroutine already held the pool's transaction.
type writeTx struct {
	p         *writePool
	tx        *sql.Tx
	gid       int64
	savepoint string
	done      bool
}

func (t *writeTx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.Exec(query, args...)
}

func (t *writeTx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.Query(query, args...)
}

func (t *writeTx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRow(query, args...)
}

func (t *writeTx) Commit() error {
	if t.done {
		return sql.ErrTxDone
	}
	if t.savepoint != "" {
		t.done = true
		_, err := t.tx.Exec("RELEASE " + t.savepoint)
		t.p.unnest()
		return err
	}
	if hook := t.p.beforeCommit; hook != nil {
		if err := hook(); err != nil {
			return err
		}
	}
	t.done = true
	err := t.tx.Commit()
	t.p.release(t.gid)
	return err
}

func (t *writeTx) Rollback() error {
	if t.done {
		return sql.ErrTxDone
	}
	t.done = true
	if t.savepoint != "" {
		_, err := t.tx.Exec("ROLLBACK TO " + t.savepoint)
		_, relErr := t.tx.Exec("RELEASE " + t.savepoint)
		t.p.unnest()
		return errors.Join(err, relErr)
	}
	err := t.tx.Rollback()
	t.p.release(t.gid)
	return err
}

// readPool is where DAL reads go. Over split pools it is the read-only pool;
// over one connection (NewDAL) it is the write pool, so a read inside a
// transaction runs on it and a read outside waits no longer than a write would.
type readPool struct {
	raw    *sql.DB
	shared *writePool
}

func (r *readPool) Query(query string, args ...any) (*sql.Rows, error) {
	if r.shared != nil {
		return r.shared.Query(query, args...)
	}
	return r.raw.Query(query, args...)
}

func (r *readPool) QueryRow(query string, args ...any) *sql.Row {
	if r.shared != nil {
		return r.shared.QueryRow(query, args...)
	}
	return r.raw.QueryRow(query, args...)
}

func (r *readPool) Close() error {
	if r.shared != nil {
		return r.shared.Close()
	}
	return r.raw.Close()
}

func writeConnCaller() string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var out []string
	for {
		f, more := frames.Next()
		if filepath.Base(f.File) != "writepool.go" && f.Function != "" {
			fn := f.Function
			if i := strings.LastIndex(fn, "/"); i >= 0 {
				fn = fn[i+1:]
			}
			if i := strings.Index(fn, "."); i >= 0 {
				fn = fn[i+1:]
			}
			out = append(out, fmt.Sprintf("%s (%s:%d)", fn, filepath.Base(f.File), f.Line))
			if len(out) == 3 {
				break
			}
		}
		if !more {
			break
		}
	}
	return strings.Join(out, " <- ")
}
