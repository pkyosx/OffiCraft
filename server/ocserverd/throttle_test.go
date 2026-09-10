// Skeleton generated from server/ocserverd/throttle.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestBegin(t *testing.T) {
	t.Run("a fresh gate admits four callers and blocks the fifth", func(t *testing.T) {
		var throttle credentialThrottle
		releases := make([]func(), 0, 4)
		for i := 0; i < 4; i++ {
			release, wait, blocked := throttle.begin()
			if blocked || wait != 0 || release == nil {
				t.Fatalf("admission %d = (release=%v, wait=%v, blocked=%v), want an immediate admission", i, release != nil, wait, blocked)
			}
			releases = append(releases, release)
		}
		release, wait, blocked := throttle.begin()
		if release != nil || wait != time.Second || !blocked {
			t.Fatalf("fifth admission = (release=%v, wait=%v, blocked=%v), want (nil, 1s, true)", release != nil, wait, blocked)
		}
		for _, release := range releases {
			release()
		}
		if release, wait, blocked := throttle.begin(); blocked || release == nil || wait != 0 {
			t.Fatalf("admission after releasing all slots = (release=%v, wait=%v, blocked=%v), want an immediate admission", release != nil, wait, blocked)
		} else {
			release()
		}
	})

	t.Run("concurrent callers cannot reserve more than four slots", func(t *testing.T) {
		var throttle credentialThrottle
		const callers = 16
		start := make(chan struct{})
		results := make(chan bool, callers)
		releases := make(chan func(), callers)
		var group sync.WaitGroup
		group.Add(callers)
		for i := 0; i < callers; i++ {
			go func() {
				defer group.Done()
				<-start
				release, _, blocked := throttle.begin()
				results <- !blocked
				if !blocked {
					releases <- release
				}
			}()
		}
		close(start)
		group.Wait()
		close(results)
		close(releases)
		admitted := 0
		for got := range results {
			if got {
				admitted++
			}
		}
		if admitted != 4 {
			t.Fatalf("concurrent admissions = %d, want 4", admitted)
		}
		for release := range releases {
			release()
		}
	})

	t.Run("releasing the same slot repeatedly does not create extra capacity", func(t *testing.T) {
		var throttle credentialThrottle
		release, _, blocked := throttle.begin()
		if blocked {
			t.Fatal("the first admission was blocked")
		}
		for i := 0; i < 5; i++ {
			release()
		}
		admitted := 0
		releases := make([]func(), 0, 4)
		for i := 0; i < 5; i++ {
			next, _, blocked := throttle.begin()
			if blocked {
				continue
			}
			admitted++
			releases = append(releases, next)
		}
		if admitted != 4 {
			t.Fatalf("admissions after repeated release = %d, want 4", admitted)
		}
		for _, next := range releases {
			next()
		}
	})
}

func TestFailureFloor(t *testing.T) {
	var server apiServer
	if got := server.failureFloor(); got != 3*time.Second {
		t.Fatalf("zero-value failureFloor = %v, want 3s", got)
	}
	server.credentialFailureFloor = 5 * time.Millisecond
	if got := server.failureFloor(); got != 5*time.Millisecond {
		t.Fatalf("overridden failureFloor = %v, want 5ms", got)
	}
}

func TestHoldFailureFloor(t *testing.T) {
	server := &apiServer{credentialFailureFloor: 40 * time.Millisecond}

	started := time.Now().Add(-20 * time.Millisecond)
	call := time.Now()
	server.holdFailureFloor(started)
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatalf("holdFailureFloor returned after %v, before the 40ms deadline", elapsed)
	}
	if waited := time.Since(call); waited >= 40*time.Millisecond {
		t.Fatalf("holdFailureFloor waited %v after half the floor was already spent, want less than one full floor", waited)
	}

	call = time.Now()
	server.holdFailureFloor(time.Now().Add(-80 * time.Millisecond))
	if waited := time.Since(call); waited >= 20*time.Millisecond {
		t.Fatalf("holdFailureFloor waited %v after the deadline had passed, want no material wait", waited)
	}
}

func TestWriteThrottled(t *testing.T) {
	t.Run("the response uses the shared 429 error envelope", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeThrottled(rec, 42*time.Second)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got != "42" {
			t.Fatalf("Retry-After = %q, want %q", got, "42")
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", got, "application/json")
		}
		if got := rec.Body.String(); got != `{"error":{"code":"client_error","message":"too many failed credential attempts; retry in 42s"}}` {
			t.Fatalf("body = %q, want the complete throttling envelope", got)
		}
		var decoded map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("response body is not JSON: %v", err)
		}
	})

	for _, tc := range []struct {
		name string
		wait time.Duration
		want string
	}{
		{name: "negative wait is floored at one second", wait: -time.Second, want: "1"},
		{name: "zero wait is floored at one second", wait: 0, want: "1"},
		{name: "a subsecond wait rounds up to one second", wait: time.Millisecond, want: "1"},
		{name: "the normal burst wait is one second", wait: time.Second, want: "1"},
		{name: "a fractional second rounds up", wait: 1500 * time.Millisecond, want: "2"},
		{name: "a five minute wait is rendered in whole seconds", wait: 5 * time.Minute, want: "300"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeThrottled(rec, tc.wait)
			if got := rec.Header().Get("Retry-After"); got != tc.want {
				t.Fatalf("Retry-After for %v = %q, want %q", tc.wait, got, tc.want)
			}
		})
	}
}
