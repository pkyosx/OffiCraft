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
	"sync/atomic"
	"time"

	"ocserverd/txguard"
)

// writeConnWaitLimit bounds only the wait for the write pool's single
// connection. A wait that long means a deadlock, not load. Unbounded, that
// stops every write on the station with nothing in the log; bounded, one
// operation fails.
const writeConnWaitLimit = 30 * time.Second

// writeTxHoldLimit bounds how long one transaction may hold the connection.
// Whatever its holder is stuck on — another goroutine, a lock outside txguard,
// a network call, a statement that does not end — the transaction is rolled
// back at the limit and the connection goes back to the pool. No transaction
// on this pool is meant to take more than milliseconds.
const writeTxHoldLimit = 30 * time.Second

var (
	errWriteConnWait  = errors.New("timed out waiting for the write connection")
	errWriteTxExpired = errors.New("write transaction rolled back: held longer than the limit")
)

// writePool is the only way to the write connection. Every wait for it is
// bounded, and so is every hold of it inside a transaction or an open result
// set. A single statement outside a transaction is not: that is where the
// VACUUM INTO backup runs, and it must not be cut short.
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
// ⚠️ The hold limit is the BeginTx ctx on purpose: when it ends database/sql
// rolls the transaction back and returns the connection even while the holder
// is blocked on something else, and the sqlite driver interrupts a statement
// running under it. Do not move a deadline ctx onto DB.Conn's work or onto a
// statement outside a transaction: that would cut short a healthy VACUUM.
type writePool struct {
	raw       *sql.DB
	limit     time.Duration
	holdLimit time.Duration
	// beforeCommit is a test seam: it runs on the holding goroutine inside the
	// outermost transaction, just before COMMIT, and its error aborts the commit.
	beforeCommit func() error

	mu sync.Mutex
	// held has the live transaction and, until their owners call Commit or
	// Rollback, the ones that expired: a goroutine whose transaction was rolled
	// back under it must get errors, not have its next write autocommit alone.
	held map[int64]*txHolding
}

// txHolding is one outermost transaction and its goroutine.
type txHolding struct {
	p       *writePool
	gid     int64
	tx      *sql.Tx
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func() bool
	depth   int
	expired atomic.Bool
	left    atomic.Bool
	begunAt []uintptr
}

func newWritePool(db *sql.DB) *writePool {
	return &writePool{raw: db, limit: writeConnWaitLimit, holdLimit: writeTxHoldLimit, held: map[int64]*txHolding{}}
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
func releaseWhenDone(c *sql.Conn, cancel context.CancelFunc) {
	go func() {
		_ = c.Close()
		cancel()
	}()
}

// mine is the calling goroutine's transaction on this pool, live or expired.
func (p *writePool) mine() *txHolding {
	p.mu.Lock()
	empty := len(p.held) == 0
	p.mu.Unlock()
	if empty {
		return nil
	}
	gid := txguard.GoID()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.held[gid]
}

func (h *txHolding) err() error {
	return fmt.Errorf("%w (%s)", errWriteTxExpired, h.p.holdLimit)
}

func (h *txHolding) exec(query string, args ...any) (sql.Result, error) {
	if h.expired.Load() {
		return nil, h.err()
	}
	return h.tx.ExecContext(h.ctx, query, args...)
}

func (h *txHolding) query(query string, args ...any) (*sql.Rows, error) {
	if h.expired.Load() {
		return nil, h.err()
	}
	return h.tx.QueryContext(h.ctx, query, args...)
}

// After expiry the row's Scan reports that the transaction is done.
func (h *txHolding) queryRow(query string, args ...any) *sql.Row {
	return h.tx.QueryRowContext(h.ctx, query, args...)
}

// expire runs when the hold limit passes with the transaction still open.
// database/sql has already begun the rollback on its own goroutine.
func (h *txHolding) expire() {
	h.expired.Store(true)
	log.Printf("[wdb] ERROR: rolled back a write transaction held longer than %s; begun at: %s",
		h.p.holdLimit, formatCallers(h.begunAt))
	h.leave()
}

func (h *txHolding) leave() {
	if h.left.CompareAndSwap(false, true) {
		txguard.Leave(h.gid)
	}
}

// end follows the owner's Commit or Rollback: nothing of this transaction is
// left. stopped is what h.stop answered before it: false means the expiry had
// already fired. A deadline that passed after h.stop but before database/sql
// checked it is an expiry too, found here.
func (h *txHolding) end(stopped bool, err error) error {
	if stopped && err != nil && h.ctx.Err() != nil {
		h.expire()
	}
	h.cancel()
	h.p.mu.Lock()
	if h.p.held[h.gid] == h {
		delete(h.p.held, h.gid)
	}
	h.p.mu.Unlock()
	h.leave()
	if h.expired.Load() {
		return h.err()
	}
	return err
}

func (p *writePool) Exec(query string, args ...any) (sql.Result, error) {
	if h := p.mine(); h != nil {
		return h.exec(query, args...)
	}
	c, err := p.conn()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.ExecContext(context.Background(), query, args...)
}

func (p *writePool) Begin() (*writeTx, error) {
	if h := p.mine(); h != nil {
		h.p.mu.Lock()
		h.depth++
		sp := fmt.Sprintf("wtx_%d", h.depth)
		h.p.mu.Unlock()
		if _, err := h.exec("SAVEPOINT " + sp); err != nil {
			h.unnest()
			return nil, err
		}
		return &writeTx{h: h, savepoint: sp}, nil
	}
	gid := txguard.GoID()
	begunAt := make([]uintptr, 16)
	begunAt = begunAt[:runtime.Callers(2, begunAt)]
	c, err := p.conn()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.holdLimit)
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		cancel()
		_ = c.Close()
		return nil, err
	}
	releaseWhenDone(c, func() {})
	h := &txHolding{p: p, gid: gid, tx: tx, ctx: ctx, cancel: cancel, begunAt: begunAt}
	p.mu.Lock()
	p.held[gid] = h
	p.mu.Unlock()
	txguard.Enter(gid)
	h.stop = context.AfterFunc(ctx, h.expire)
	return &writeTx{h: h, outer: true}, nil
}

func (h *txHolding) unnest() {
	h.p.mu.Lock()
	h.depth--
	h.p.mu.Unlock()
}

func (p *writePool) Query(query string, args ...any) (*sql.Rows, error) {
	if h := p.mine(); h != nil {
		return h.query(query, args...)
	}
	c, err := p.conn()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.holdLimit)
	rows, err := c.QueryContext(ctx, query, args...)
	if err != nil {
		cancel()
		_ = c.Close()
		return nil, err
	}
	releaseWhenDone(c, cancel)
	return rows, nil
}

// QueryRow has no error of its own to return: after a timed-out wait, Scan
// reports context.DeadlineExceeded, and the log line says which wait it was.
func (p *writePool) QueryRow(query string, args ...any) *sql.Row {
	if h := p.mine(); h != nil {
		return h.queryRow(query, args...)
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
	ctx, cancel := context.WithTimeout(context.Background(), p.holdLimit)
	row := c.QueryRowContext(ctx, query, args...)
	releaseWhenDone(c, cancel)
	return row
}

func (p *writePool) Close() error { return p.raw.Close() }

// writeTx is a transaction on the write pool, or a SAVEPOINT inside one when
// its goroutine already held the pool's transaction.
type writeTx struct {
	h         *txHolding
	outer     bool
	savepoint string
	done      bool
}

func (t *writeTx) Exec(query string, args ...any) (sql.Result, error) {
	return t.h.exec(query, args...)
}

func (t *writeTx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.h.query(query, args...)
}

func (t *writeTx) QueryRow(query string, args ...any) *sql.Row {
	return t.h.queryRow(query, args...)
}

func (t *writeTx) Commit() error {
	if t.done {
		return sql.ErrTxDone
	}
	if !t.outer {
		t.done = true
		_, err := t.h.exec("RELEASE " + t.savepoint)
		t.h.unnest()
		return err
	}
	if hook := t.h.p.beforeCommit; hook != nil && !t.h.expired.Load() {
		if err := hook(); err != nil {
			return err
		}
	}
	t.done = true
	stopped := t.h.stop()
	return t.h.end(stopped, t.h.tx.Commit())
}

func (t *writeTx) Rollback() error {
	if t.done {
		return sql.ErrTxDone
	}
	t.done = true
	if !t.outer {
		_, err := t.h.exec("ROLLBACK TO " + t.savepoint)
		_, relErr := t.h.exec("RELEASE " + t.savepoint)
		t.h.unnest()
		return errors.Join(err, relErr)
	}
	stopped := t.h.stop()
	return t.h.end(stopped, t.h.tx.Rollback())
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
	return formatCallers(pcs[:runtime.Callers(3, pcs)])
}

// formatCallers names the first three frames outside writepool.go.
func formatCallers(pcs []uintptr) string {
	frames := runtime.CallersFrames(pcs)
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
