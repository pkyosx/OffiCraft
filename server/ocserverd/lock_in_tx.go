package main

import (
	"errors"
	"log"
	"net/http"

	"ocserverd/txguard"
)

// answerLockInTx turns a lock refused inside a write transaction into a 500 for
// that one request. By the time it is recovered here the panic has unwound
// through the transaction's deferred Rollback, so nothing the request wrote
// lands. Every other panic is re-raised unchanged.
func answerLockInTx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			var refused *txguard.LockInTxError
			if err, ok := v.(error); ok && errors.As(err, &refused) {
				internalError(w, refused)
				return
			}
			panic(v)
		}()
		next.ServeHTTP(w, r)
	})
}

// surviveLockInTx runs one round of background work; a lock refused inside
// its transaction ends that round (logged) instead of the process.
func surviveLockInTx(what string, fn func()) {
	defer recoverLockInTx(what)
	fn()
}

// recoverLockInTx must be deferred directly (recover only works there): it
// ends a background goroutine whose lock was refused instead of the process.
func recoverLockInTx(what string) {
	v := recover()
	if v == nil {
		return
	}
	var refused *txguard.LockInTxError
	if err, ok := v.(error); ok && errors.As(err, &refused) {
		log.Printf("[lock] ERROR: %s gave up: %v", what, refused)
		return
	}
	panic(v)
}
