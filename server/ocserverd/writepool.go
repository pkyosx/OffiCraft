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
	"time"
)

// writeConnWaitLimit bounds only the wait for the write pool's single
// connection. A wait that long means a deadlock, not load: a transaction that
// asks for the connection it already holds, or one holding it while waiting on
// a mutex whose holder waits here. Unbounded, either one stops every write on
// the station with nothing in the log; bounded, one request fails and whatever
// it held is released.
const writeConnWaitLimit = 30 * time.Second

var errWriteConnWait = errors.New("timed out waiting for the write connection")

// writePool is the only way to the write connection. Every method bounds the
// wait and never the work.
//
// ⚠️ Do not "simplify" to BeginTx(ctx)/ExecContext(ctx) with a deadline ctx:
// database/sql rolls a transaction back when its BeginTx ctx ends, and the
// sqlite driver interrupts a statement when its ctx ends, so a long but healthy
// transaction or VACUUM would be killed at the deadline. A ctx given to
// DB.Conn governs the acquisition only.
type writePool struct {
	raw   *sql.DB
	limit time.Duration
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

func (p *writePool) Exec(query string, args ...any) (sql.Result, error) {
	c, err := p.conn()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.ExecContext(context.Background(), query, args...)
}

func (p *writePool) Begin() (*sql.Tx, error) {
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
	return tx, nil
}

func (p *writePool) Query(query string, args ...any) (*sql.Rows, error) {
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
