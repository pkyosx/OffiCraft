package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// envMap turns a fixture map into the env lookup the gate takes.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// noBaseGolden is the refusal a person finds when they go looking, verbatim.
var noBaseGolden = strings.Join([]string{
	"[ocwarden] FATAL: OC_BASE is not set — this warden was never told which station to talk to.",
	"[ocwarden]   It is NOT guessing an address, and it will NOT start any members on this machine.",
	"[ocwarden]   Guessing would be worse than stopping: something else may well be listening on",
	"[ocwarden]   http://127.0.0.1:7755 (a station host, a trial station), and members started against it",
	"[ocwarden]   would quietly join the WRONG station while every screen looked normal.",
	"[ocwarden]   This machine WILL still be listed, and will simply never come online.",
	"[ocwarden]   Do not read its presence in the list as evidence that this is not the problem.",
	"[ocwarden]   To fix: re-run `ocwarden install` with OC_BASE set to the station URL.",
	"[ocwarden]   Setting OC_BASE alone is not enough — this process must be restarted to pick it up.",
	"[ocwarden]   Halting here (staying alive, doing nothing) rather than exiting; see ocwarden.no-base",
	"",
}, "\n")

func TestBaseFromEnv(t *testing.T) {
	cases := []struct {
		name           string
		raw            string
		want           string
		wantConfigured bool
	}{
		{"unset", "", "", false},
		{"whitespace only is not a station address", "   ", "", false},
		{"a tab is not a station address", "\t\n", "", false},
		{"loopback is a legitimate station address", "http://127.0.0.1:7755", "http://127.0.0.1:7755", true},
		{"a real host is re-schemed to https", "http://oc.example.com", "https://oc.example.com", true},
		{"an unparseable value is still configured", "ftp://x", "ftp://x", true},
		{"a bare word is still configured", "notaurl", "notaurl", true},
	}
	for _, c := range cases {
		env := envMap(map[string]string{"OC_BASE": c.raw})
		base, configured := baseFromEnv(env)
		if base != c.want || configured != c.wantConfigured {
			t.Errorf("%s: baseFromEnv = (%q, %v), want (%q, %v)", c.name, base, configured, c.want, c.wantConfigured)
		}
	}
}

func TestNoBaseMessage(t *testing.T) {
	if got := noBaseMessage(noBaseSentinelName); got != noBaseGolden {
		t.Errorf("noBaseMessage(%q) =\n%s\nwant\n%s", noBaseSentinelName, got, noBaseGolden)
	}
	where := noBaseMessage("/Users/eva/.officraft/warden/ocwarden.no-base")
	wantLast := "[ocwarden]   Halting here (staying alive, doing nothing) rather than exiting; see /Users/eva/.officraft/warden/ocwarden.no-base\n"
	if !strings.HasSuffix(where, wantLast) {
		t.Errorf("noBaseMessage last line = %q, want it to end with %q", where, wantLast)
	}
}

func TestNoBaseSentinelPath(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    string
		wantErr string
	}{
		{"main instance", map[string]string{"HOME": "/Users/eva"},
			"/Users/eva/.officraft/warden/ocwarden.no-base", ""},
		{"namespaced instance", map[string]string{"HOME": "/Users/eva", "OC_NAMESPACE": "lab"},
			"/Users/eva/.officraft-lab/warden/ocwarden.no-base", ""},
		{"HOME unset", map[string]string{}, "", "HOME is not set"},
		{"invalid namespace", map[string]string{"HOME": "/Users/eva", "OC_NAMESPACE": "Lab"},
			"", `OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "Lab"`},
	}
	for _, c := range cases {
		got, err := noBaseSentinelPath(envMap(c.env))
		if got != c.want {
			t.Errorf("%s: path = %q, want %q", c.name, got, c.want)
		}
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: err = %v, want nil", c.name, err)
		case c.wantErr != "" && (err == nil || err.Error() != c.wantErr):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
}

func TestWriteNoBaseSentinel(t *testing.T) {
	home := t.TempDir()
	where := writeNoBaseSentinel(envMap(map[string]string{"HOME": home}), "recorded body\n", os.MkdirAll, os.WriteFile)
	wantPath := filepath.Join(home, ".officraft", "warden", "ocwarden.no-base")
	if where != wantPath {
		t.Errorf("where = %q, want %q", where, wantPath)
	}
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if string(body) != "recorded body\n" {
		t.Errorf("sentinel body = %q, want %q", body, "recorded body\n")
	}
	info, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat sentinel: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("sentinel perms = %o, want 644", info.Mode().Perm())
	}

	var mkdirs, writes int
	countingMkdir := func(string, os.FileMode) error { mkdirs++; return nil }
	countingWrite := func(string, []byte, os.FileMode) error { writes++; return nil }

	refused := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"HOME unset", map[string]string{}, "no sentinel written (HOME is not set)"},
		{"invalid namespace", map[string]string{"HOME": home, "OC_NAMESPACE": "Lab"},
			`no sentinel written (OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "Lab")`},
	}
	for _, c := range refused {
		if got := writeNoBaseSentinel(envMap(c.env), "b", countingMkdir, countingWrite); got != c.want {
			t.Errorf("%s: where = %q, want %q", c.name, got, c.want)
		}
	}
	if mkdirs != 0 || writes != 0 {
		t.Errorf("an unresolvable path touched the filesystem: %d mkdir, %d write", mkdirs, writes)
	}

	failMkdir := func(string, os.FileMode) error { return errors.New("mkdir denied") }
	if got := writeNoBaseSentinel(envMap(map[string]string{"HOME": home}), "b", failMkdir, countingWrite); got != "no sentinel written (mkdir denied)" {
		t.Errorf("where = %q, want %q", got, "no sentinel written (mkdir denied)")
	}
	if writes != 0 {
		t.Errorf("a failed mkdir still wrote the file (%d writes)", writes)
	}
	failWrite := func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	if got := writeNoBaseSentinel(envMap(map[string]string{"HOME": home}), "b", countingMkdir, failWrite); got != "no sentinel written (disk full)" {
		t.Errorf("where = %q, want %q", got, "no sentinel written (disk full)")
	}
}

func TestStationAddressGate(t *testing.T) {
	frozen := func() time.Time { return time.Date(2026, 9, 8, 4, 5, 6, 0, time.UTC) }

	t.Run("a configured base carries on", func(t *testing.T) {
		home := t.TempDir()
		var out bytes.Buffer
		blocked := 0
		rc, stop := stationAddressGate(
			envMap(map[string]string{"HOME": home, "OC_BASE": "http://127.0.0.1:7755"}),
			&out, false, frozen, func() { blocked++ })
		if rc != 0 || stop {
			t.Errorf("gate = (%d, %v), want (0, false)", rc, stop)
		}
		if out.String() != "" {
			t.Errorf("out = %q, want empty", out.String())
		}
		if blocked != 0 {
			t.Errorf("blocked %d times, want 0", blocked)
		}
		if _, err := os.Stat(filepath.Join(home, ".officraft", "warden", "ocwarden.no-base")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("a configured warden left a sentinel behind (%v)", err)
		}
	})

	t.Run("--once refuses and returns", func(t *testing.T) {
		home := t.TempDir()
		var out bytes.Buffer
		blocked := 0
		rc, stop := stationAddressGate(envMap(map[string]string{"HOME": home}), &out, true, frozen, func() { blocked++ })
		if rc != 1 || !stop {
			t.Errorf("gate = (%d, %v), want (1, true)", rc, stop)
		}
		want := noBaseGolden + "[ocwarden] --once: refusing and exiting non-zero (no sentinel written; the launchd path halts instead)\n"
		if out.String() != want {
			t.Errorf("out =\n%s\nwant\n%s", out.String(), want)
		}
		if blocked != 0 {
			t.Errorf("the --once hook parked %d times, want 0", blocked)
		}
		if _, err := os.Stat(filepath.Join(home, ".officraft", "warden", "ocwarden.no-base")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("--once wrote a sentinel (%v)", err)
		}
	})

	t.Run("the launchd path halts", func(t *testing.T) {
		home := t.TempDir()
		var out bytes.Buffer
		blocked := 0
		rc, stop := stationAddressGate(envMap(map[string]string{"HOME": home}), &out, false, frozen, func() { blocked++ })
		if rc != 1 || !stop {
			t.Errorf("gate = (%d, %v), want (1, true)", rc, stop)
		}
		if blocked != 1 {
			t.Errorf("blocked %d times, want 1", blocked)
		}
		sentinel := filepath.Join(home, ".officraft", "warden", "ocwarden.no-base")
		if _, err := os.Stat(sentinel); err != nil {
			t.Errorf("the halting path left no sentinel: %v", err)
		}
		if !strings.HasSuffix(out.String(), "[ocwarden] halted: no station address; "+sentinel+"\n") {
			t.Errorf("out =\n%s\nwant it to end with the sentinel path %s", out.String(), sentinel)
		}
	})
}

func TestHaltNoBase(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	blocked := 0
	frozen := func() time.Time { return time.Date(2026, 9, 8, 4, 5, 6, 0, time.UTC) }

	rc := haltNoBase(envMap(map[string]string{"HOME": home}), &out, frozen, func() { blocked++ })
	if rc != 1 {
		t.Errorf("haltNoBase = %d, want 1", rc)
	}
	if blocked != 1 {
		t.Errorf("blocked %d times, want 1", blocked)
	}
	sentinel := filepath.Join(home, ".officraft", "warden", "ocwarden.no-base")
	if want := noBaseGolden + "[ocwarden] halted: no station address; " + sentinel + "\n"; out.String() != want {
		t.Errorf("out =\n%s\nwant\n%s", out.String(), want)
	}
	body, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if want := "2026-09-08T04:05:06Z\n" + noBaseGolden; string(body) != want {
		t.Errorf("sentinel body =\n%s\nwant\n%s", body, want)
	}

	var noHome bytes.Buffer
	blocked = 0
	rc = haltNoBase(envMap(map[string]string{}), &noHome, frozen, func() { blocked++ })
	if rc != 1 || blocked != 1 {
		t.Errorf("an unwritable sentinel changed the halt: rc=%d blocked=%d, want 1 and 1", rc, blocked)
	}
	if want := noBaseGolden + "[ocwarden] halted: no station address; no sentinel written (HOME is not set)\n"; noHome.String() != want {
		t.Errorf("out =\n%s\nwant\n%s", noHome.String(), want)
	}
}

func TestBlockUntilSignal(t *testing.T) {
	prev := notifyContext
	t.Cleanup(func() { notifyContext = prev })

	var watched []os.Signal
	var parent context.Context
	release := make(chan struct{})
	stopped := false
	notifyContext = func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		parent, watched = ctx, signals
		derived, cancel := context.WithCancel(ctx)
		go func() {
			<-release
			cancel()
		}()
		return derived, func() { stopped = true; cancel() }
	}

	returned := make(chan struct{})
	go func() {
		blockUntilSignal()
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("blockUntilSignal returned before any signal arrived")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("blockUntilSignal did not return after the context was cancelled")
	}

	if want := []os.Signal{syscall.SIGINT, syscall.SIGTERM}; !reflect.DeepEqual(watched, want) {
		t.Errorf("watched signals = %v, want %v", watched, want)
	}
	if parent != context.Background() {
		t.Errorf("parent context = %v, want context.Background()", parent)
	}
	if !stopped {
		t.Error("blockUntilSignal returned without releasing the signal registration")
	}
}
