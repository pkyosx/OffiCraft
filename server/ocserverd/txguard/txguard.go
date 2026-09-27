// Package txguard records which goroutines hold a write transaction and
// provides the only mutexes the server takes, which refuse to be taken by such
// a goroutine.
//
// With one write connection, a goroutine that holds it inside a transaction and
// then waits for a mutex can wait forever on a holder that is itself waiting
// for the connection. Refusing the lock turns that into an immediate failure of
// the one operation, whether or not anyone else holds the lock at that moment.
//
// The mutexes live in their own package so the unchecked sync.Mutex inside them
// cannot be reached from package main.
package txguard

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	holdersMu sync.Mutex
	holders   = map[int64]int{}
	// held is how many write transactions are open process-wide. At zero no
	// goroutine can be holding one, and a lock skips reading its goroutine id.
	held       atomic.Int64
	violations atomic.Int64
)

// GoID is the calling goroutine's id, read from the header runtime.Stack
// writes ("goroutine 123 [running]:"). Go has no API for it on purpose; it is
// used here only as an identity for "the same line of execution", never
// passed around, so a caller cannot hand in the wrong one.
func GoID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	b := buf[:n]
	const prefix = "goroutine "
	if len(b) < len(prefix) || string(b[:len(prefix)]) != prefix {
		panic("txguard: runtime.Stack no longer starts with \"goroutine <id>\": " + string(b))
	}
	b = b[len(prefix):]
	var id int64
	i := 0
	for ; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
		id = id*10 + int64(b[i]-'0')
	}
	if i == 0 {
		panic("txguard: runtime.Stack no longer starts with \"goroutine <id>\": " + string(buf[:n]))
	}
	return id
}

// Enter records that goroutine gid now holds a write transaction.
func Enter(gid int64) {
	held.Add(1)
	holdersMu.Lock()
	holders[gid]++
	holdersMu.Unlock()
}

// Leave records that goroutine gid no longer holds that transaction.
func Leave(gid int64) {
	holdersMu.Lock()
	if holders[gid] <= 1 {
		delete(holders, gid)
	} else {
		holders[gid]--
	}
	holdersMu.Unlock()
	held.Add(-1)
}

// Holding reports whether the calling goroutine holds a write transaction.
func Holding() bool {
	if held.Load() == 0 {
		return false
	}
	gid := GoID()
	holdersMu.Lock()
	n := holders[gid]
	holdersMu.Unlock()
	return n > 0
}

// OnRefusal, when set, runs on the refused goroutine just before it panics.
// Tests set it once, before any lock is taken, to say where a refusal came from.
var OnRefusal func(at string)

// Violations counts every refused lock since the process started.
func Violations() int64 { return violations.Load() }

// LockInTxError is the panic value of a refused lock. The transaction is rolled
// back by the deferred Rollback it unwinds through; whoever recovers it
// answers the one operation as failed.
type LockInTxError struct{ At string }

func (e *LockInTxError) Error() string {
	return "a lock was taken while holding the write transaction (at " + e.At + ")"
}

func refuseInTx() {
	if !Holding() {
		return
	}
	violations.Add(1)
	at := lockCaller()
	if OnRefusal != nil {
		OnRefusal(at)
	}
	log.Printf("[lock] ERROR: refused a lock taken while this goroutine holds the write transaction; "+
		"the transaction is rolled back; at: %s", at)
	panic(&LockInTxError{At: at})
}

// Mutex is sync.Mutex that panics with *LockInTxError instead of locking when
// the calling goroutine holds a write transaction. Its zero value is unlocked.
type Mutex struct{ m sync.Mutex }

func (l *Mutex) Lock() {
	refuseInTx()
	l.m.Lock()
}

func (l *Mutex) TryLock() bool {
	refuseInTx()
	return l.m.TryLock()
}

func (l *Mutex) Unlock() { l.m.Unlock() }

// Acquire is Lock for a critical section that unlocks on several paths: the
// returned unlock may be called any number of times and only the first counts,
// so it can also be deferred and a panic still releases the lock.
func (l *Mutex) Acquire() (unlock func()) {
	l.Lock()
	var once sync.Once
	return func() { once.Do(l.m.Unlock) }
}

// RWMutex is sync.RWMutex with the same refusal on Lock and RLock.
type RWMutex struct{ m sync.RWMutex }

func (l *RWMutex) Lock() {
	refuseInTx()
	l.m.Lock()
}

func (l *RWMutex) RLock() {
	refuseInTx()
	l.m.RLock()
}

// Acquire is Mutex.Acquire for the write lock.
func (l *RWMutex) Acquire() (unlock func()) {
	l.Lock()
	var once sync.Once
	return func() { once.Do(l.m.Unlock) }
}

func (l *RWMutex) Unlock()  { l.m.Unlock() }
func (l *RWMutex) RUnlock() { l.m.RUnlock() }

func lockCaller() string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var out []string
	for {
		f, more := frames.Next()
		if filepath.Base(f.File) != "txguard.go" && f.Function != "" {
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
