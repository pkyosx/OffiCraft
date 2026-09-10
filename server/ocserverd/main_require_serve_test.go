package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// envOf builds the env lookup realMain takes. It lived in the old config_test.go,
// which T-125 replaced with a skeleton; it is kept here because this file is the
// only remaining caller.
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// lockedWriter is an io.Writer safe to read from the test goroutine while a
// realMain call that refused to return (a mutant that actually starts serving)
// is still writing into it.
type lockedWriter struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// serveWorld is a self-built pair of worlds: an empty data directory that
// NOTHING outside this test can reach, and an oc.toml whose port is already
// held by this test's own listener. The test never asks what this machine looks
// like — it constructs both the "database was touched" and the "database was
// not touched" outcome itself, so the guard keeps its discrimination on any
// machine. The held port is what stops a regressed binary from serving forever.
type serveWorld struct {
	dataDir string
	dbPath  string
	env     func(string) string
}

func newServeWorld(t *testing.T) serveWorld {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	cfgPath := filepath.Join(t.TempDir(), "oc.toml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("[server]\nport = %d\n", port)), 0o644); err != nil {
		t.Fatalf("write oc.toml: %v", err)
	}
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "officraft.db")
	return serveWorld{
		dataDir: dataDir,
		dbPath:  dbPath,
		env: envOf(map[string]string{
			envConfigPath:  cfgPath,
			envDatabaseURL: "sqlite:///" + dbPath,
		}),
	}
}

// entries lists what the data directory holds — the database file, its WAL, a
// pre-migration backup, anything. "Touched the database" is exactly "this is
// not empty any more".
func (w serveWorld) entries(t *testing.T) []string {
	t.Helper()
	des, err := os.ReadDir(w.dataDir)
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	names := make([]string, 0, len(des))
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names
}

// run calls realMain off the test goroutine so a regression that really starts
// serving fails the test instead of hanging it forever.
func (w serveWorld) run(t *testing.T, argv ...string) (int, string) {
	t.Helper()
	out := &lockedWriter{}
	rc := make(chan int, 1)
	go func() { rc <- realMain(argv, w.env, out) }()
	select {
	case code := <-rc:
		return code, out.String()
	case <-time.After(90 * time.Second):
		t.Fatalf("realMain(%q) never returned — it is running a server; output so far:\n%s", argv, out.String())
		return 0, ""
	}
}

// TestRealMainWithoutServeTouchesNoDatabase pins the T-107 ruling: serve is not
// implied any more. A bare `ocserverd`, and one carrying only flags, list the
// subcommands and exit without opening, migrating or creating anything — while
// `migrate` in the very same world does create the database, which is what
// makes the empty-directory assertion above a real observation rather than a
// vacuous one.
func TestRealMainWithoutServeTouchesNoDatabase(t *testing.T) {
	for _, argv := range [][]string{nil, {"--no-reconcile"}, {"--no-outsource", "--no-reconcile"}} {
		w := newServeWorld(t)
		rc, out := w.run(t, argv...)
		if rc == 0 {
			t.Fatalf("realMain(%q): a run that started nothing must not report success", argv)
		}
		if got := w.entries(t); len(got) != 0 {
			t.Fatalf("realMain(%q) touched the database: %v\noutput:\n%s", argv, got, out)
		}
		for _, name := range []string{"serve", "backup", "mfa-disable"} {
			if !strings.Contains(out, name) {
				t.Fatalf("realMain(%q): want the subcommand list to name %q, got:\n%s", argv, name, out)
			}
		}
	}

	// The other world: the same probe, on a call that IS allowed to touch the
	// database. Without this the assertion above would still pass if the probe
	// had gone blind (wrong directory, wrong DSN, a realMain that fails before
	// it reaches any storage at all).
	w := newServeWorld(t)
	if rc, out := w.run(t, "migrate"); rc != 0 {
		t.Fatalf("migrate must succeed in this world (rc %d):\n%s", rc, out)
	}
	if got := w.entries(t); len(got) == 0 {
		t.Fatal("migrate left the data directory empty — the probe cannot see a database being touched, so the assertions above prove nothing")
	}
}

// TestRealMainServeAndHelpUnchanged pins the two routes T-107 must NOT have
// moved: an explicit `serve` still reaches the server (it gets as far as the
// port this test is holding, which no non-serving path could reach), and the
// help flags still print usage and exit 0.
func TestRealMainServeAndHelpUnchanged(t *testing.T) {
	w := newServeWorld(t)
	rc, out := w.run(t, "serve", "--no-reconcile", "--no-outsource")
	if rc == 0 {
		t.Fatalf("serve should have failed on the held port, got rc 0:\n%s", out)
	}
	if got := w.entries(t); len(got) == 0 {
		t.Fatalf("explicit serve must still open the database:\n%s", out)
	}

	for _, flag := range []string{"-h", "--help"} {
		var out strings.Builder
		if rc := realMain([]string{flag}, envOf(nil), &out); rc != 0 {
			t.Fatalf("%s: want rc 0, got %d", flag, rc)
		}
		if !strings.Contains(out.String(), "subcommands:") {
			t.Fatalf("%s: want usage, got:\n%s", flag, out.String())
		}
	}
}
