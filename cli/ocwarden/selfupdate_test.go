package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// httpAnswer is one canned reply of the getter seam: a transport fault, or a
// status plus body.
type httpAnswer struct {
	status int
	body   string
	err    error
}

// servedPaths is the getter double — it answers from a per-path script and
// records every path fetched, so a test asserts what was NOT downloaded too.
type servedPaths struct {
	answers map[string]httpAnswer
	gets    []string
}

func (s *servedPaths) get(path string) (int, []byte, error) {
	s.gets = append(s.gets, path)
	answer, ok := s.answers[path]
	if !ok {
		return 0, nil, fmt.Errorf("nothing is served at %s", path)
	}
	if answer.err != nil {
		return 0, nil, answer.err
	}
	return answer.status, []byte(answer.body), nil
}

// swapOps is osUpdaterOps over a real temp directory with one injectable fault,
// so a refused swap can be judged against the directory it left behind.
type swapOps struct {
	inner   updaterOps
	failOn  string // "" | "write-temp" | "chmod" | "backup" | "rename" | "probe"
	probeOK bool
}

func (o swapOps) readFile(p string) ([]byte, error) { return o.inner.readFile(p) }
func (o swapOps) writeFile(p string, d []byte, m os.FileMode) error {
	if o.failOn == "backup" && strings.HasSuffix(p, ".prev") {
		return errors.New("no space left on device")
	}
	if o.failOn == "write-temp" && !strings.HasSuffix(p, ".prev") {
		return errors.New("read-only file system")
	}
	return o.inner.writeFile(p, d, m)
}
func (o swapOps) chmod(p string, m os.FileMode) error {
	if o.failOn == "chmod" {
		return errors.New("operation not permitted")
	}
	return o.inner.chmod(p, m)
}
func (o swapOps) rename(a, b string) error {
	if o.failOn == "rename" {
		return errors.New("cross-device link")
	}
	return o.inner.rename(a, b)
}
func (o swapOps) remove(p string) error { return o.inner.remove(p) }
func (o swapOps) probe(bin string) error {
	if o.failOn == "probe" {
		return errors.New("exec " + bin + " --help: exit status 134")
	}
	return nil
}

// dirState is the sorted "name=content" listing of dir — the whole observable
// filesystem outcome of a swap, or of a swap that was refused.
func dirState(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var state []string
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		state = append(state, entry.Name()+"="+string(data))
	}
	sort.Strings(state)
	return state
}

func TestHashPrefix(t *testing.T) {
	cases := []struct {
		data string
		want string
	}{
		{"warden-bytes-v1", "9c8d3cb7cabb"},
		{"warden-bytes-v2", "cf6022985580"},
		{"agent-bytes-v1", "7ce94980e3f1"},
		{"hello", "2cf24dba5fb0"},
		{"", "e3b0c44298fc"},
	}
	for _, c := range cases {
		if got := hashPrefix([]byte(c.data)); got != c.want {
			t.Errorf("hashPrefix(%q) = %q, want %q", c.data, got, c.want)
		}
	}
}

func TestHttpGetter(t *testing.T) {
	var seen []*http.Request
	reply := func(status int, body io.ReadCloser, err error) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seen = append(seen, r)
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: status, Body: body, Header: http.Header{}}, nil
		})}
	}
	body := func(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

	status, data, err := httpGetter(reply(200, body(`{"git_sha":"80eec5fe"}`), nil),
		"https://station.example", jwtWardenOne)(versionPath)
	if status != 200 || string(data) != `{"git_sha":"80eec5fe"}` || err != nil {
		t.Errorf("GET = (%d, %q, %v), want (200, %q, nil)", status, data, err, `{"git_sha":"80eec5fe"}`)
	}
	req := seen[len(seen)-1]
	if req.Method != http.MethodGet || req.URL.String() != "https://station.example/api/version" {
		t.Errorf("sent %s %s, want GET https://station.example/api/version", req.Method, req.URL)
	}
	if got := req.Header.Get("User-Agent"); got != "ocwarden/0.1" {
		t.Errorf("User-Agent = %q, want %q", got, "ocwarden/0.1")
	}
	if got := req.Header.Get("Authorization"); got != "Bearer "+jwtWardenOne {
		t.Errorf("Authorization = %q, want the warden's bearer line", got)
	}

	seen = nil
	if _, _, err := httpGetter(reply(200, body("x"), nil), "https://station.example", "")(versionPath); err != nil {
		t.Fatalf("unauthenticated GET: %v", err)
	}
	if got := seen[0].Header.Get("Authorization"); got != "" {
		t.Errorf("a warden with no credential must send no Authorization header, sent %q", got)
	}

	status, data, err = httpGetter(reply(503, body("upstream is down"), nil),
		"https://station.example", jwtWardenOne)(wardenBinaryPath)
	if status != 503 || string(data) != "upstream is down" || err != nil {
		t.Errorf("a live-but-bad reply = (%d, %q, %v), want (503, %q, nil)", status, data, err, "upstream is down")
	}

	refused := errors.New("dial tcp: connection refused")
	status, data, err = httpGetter(reply(0, nil, refused), "https://station.example", jwtWardenOne)(versionPath)
	if status != 0 || data != nil || err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("a transport fault = (%d, %q, %v), want (0, nil, connection refused)", status, data, err)
	}

	status, data, err = httpGetter(reply(200, io.NopCloser(brokenReader{}), nil),
		"https://station.example", jwtWardenOne)(versionPath)
	if status != 200 || data != nil || err == nil || err.Error() != "stream truncated" {
		t.Errorf("a truncated body = (%d, %q, %v), want (200, nil, stream truncated)", status, data, err)
	}

	status, data, err = httpGetter(reply(200, body("x"), nil), "http://station.example\n", "")(versionPath)
	if status != 0 || data != nil || err == nil {
		t.Errorf("an unbuildable request = (%d, %q, %v), want (0, nil, an error)", status, data, err)
	}
}

// brokenReader is a body that fails mid-stream.
type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("stream truncated") }

func TestProbe(t *testing.T) {
	argv := "/opt/warden/.ocwarden.selfupdate.1 --help"

	runner := &wardenRunner{script: map[string]wardenRun{argv: {out: "usage: ocwarden ...\n"}}}
	ops := osUpdaterOps{runner: runner}
	if err := ops.probe("/opt/warden/.ocwarden.selfupdate.1"); err != nil {
		t.Errorf("a binary that runs must verify: %v", err)
	}
	if !reflect.DeepEqual(runner.calls, []string{argv}) {
		t.Errorf("ran %v, want %v", runner.calls, []string{argv})
	}

	faults := []struct {
		name string
		res  wardenRun
		want string
	}{
		{"a wrong-arch download cannot exec", wardenRun{err: errors.New("exec format error")},
			"exec /opt/warden/.ocwarden.selfupdate.1 --help: exec format error"},
		{"a silent binary is not a verified one", wardenRun{out: "  \n\t"},
			"health probe: /opt/warden/.ocwarden.selfupdate.1 --help produced no output"},
	}
	for _, c := range faults {
		ops := osUpdaterOps{runner: &wardenRunner{script: map[string]wardenRun{argv: c.res}}}
		err := ops.probe("/opt/warden/.ocwarden.selfupdate.1")
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestExecInPlace(t *testing.T) {
	cases := []struct {
		name     string
		execSelf func() error
		wantLog  []string
	}{
		{
			name:     "an unwired exec seam falls straight through to the exit",
			execSelf: nil,
			wantLog:  []string{"[ocwarden] self-update: ocwarden replaced — exec'ing the new binary in place (same PID)"},
		},
		{
			name:     "a refused exec is logged and then exits",
			execSelf: func() error { return errors.New("text file busy") },
			wantLog: []string{
				"[ocwarden] self-update: ocwarden replaced — exec'ing the new binary in place (same PID)",
				"[ocwarden] in-place exec failed (text file busy) — falling back to exit(0)",
			},
		},
		{
			name:     "an exec that returns at all has failed",
			execSelf: func() error { return nil },
			wantLog: []string{
				"[ocwarden] self-update: ocwarden replaced — exec'ing the new binary in place (same PID)",
				"[ocwarden] in-place exec failed (<nil>) — falling back to exit(0)",
			},
		},
	}
	for _, c := range cases {
		var log []string
		var exits []int
		u := &updater{
			execSelf: c.execSelf,
			exit:     func(code int) { exits = append(exits, code) },
			logf:     func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) },
		}
		u.execInPlace("self-update: ocwarden replaced")
		if !reflect.DeepEqual(log, c.wantLog) {
			t.Errorf("%s: log =\n  %#v\nwant\n  %#v", c.name, log, c.wantLog)
		}
		if !reflect.DeepEqual(exits, []int{0}) {
			t.Errorf("%s: exited %v, want [0]", c.name, exits)
		}
	}
}

// pacer is the wait seam under test control: a send on elapsed is "the poll
// interval ran out", and every requested delay is recorded.
type pacer struct {
	mu      sync.Mutex
	waits   []time.Duration
	elapsed chan struct{}
}

func (p *pacer) sleep(ctx context.Context, d time.Duration) bool {
	p.mu.Lock()
	p.waits = append(p.waits, d)
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		return false
	case <-p.elapsed:
		return true
	}
}

func (p *pacer) seen() []time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Duration(nil), p.waits...)
}

// pacedUpdater builds an updater whose wait is driven by the returned pacer.
func pacedUpdater() (*updater, *pacer) {
	p := &pacer{elapsed: make(chan struct{})}
	return &updater{kick: make(chan struct{}, 1), sleep: p.sleep}, p
}

func TestWaitNext(t *testing.T) {
	u, p := pacedUpdater()

	go func() { p.elapsed <- struct{}{} }()
	if !u.waitNext(context.Background(), 15*time.Minute) {
		t.Error("an elapsed poll interval must run a cycle")
	}
	if got := p.seen(); !reflect.DeepEqual(got, []time.Duration{15 * time.Minute}) {
		t.Errorf("waited %v, want [15m0s]", got)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if u.waitNext(cancelled, time.Minute) {
		t.Error("a cancelled context must stop the loop")
	}

	u.kick <- struct{}{}
	if !u.waitNext(context.Background(), time.Hour) {
		t.Error("a kick must cut the wait short")
	}

	stopped := &updater{sleep: func(context.Context, time.Duration) bool { return false }}
	if stopped.waitNext(context.Background(), time.Minute) {
		t.Error("a wait that reports it was cancelled mid-sleep must stop the loop")
	}
}

func TestKick(t *testing.T) {
	unwired := &updater{}
	unwired.Kick()

	u, p := pacedUpdater()
	u.Kick()
	u.Kick()

	if !u.waitNext(context.Background(), time.Hour) {
		t.Fatal("the first wait after a kick must run a cycle")
	}

	woke := make(chan bool, 1)
	go func() { woke <- u.waitNext(context.Background(), time.Hour) }()
	select {
	case <-woke:
		t.Fatal("two kicks stacked two wakes; they must coalesce into one")
	case <-time.After(50 * time.Millisecond):
	}
	p.elapsed <- struct{}{}
	if !<-woke {
		t.Error("the coalesced wait must still end on its timer")
	}
}

func TestRenewNow(t *testing.T) {
	u, _ := pacedUpdater()
	u.renew = func() (int, map[string]any, error) {
		t.Error("RenewNow must not ask the station for anything itself")
		return 0, nil, nil
	}
	u.writeTok = func(string, string) error {
		t.Error("RenewNow must not write a credential itself")
		return nil
	}

	u.RenewNow()

	if !u.renewDemanded.Load() {
		t.Error("RenewNow must record the station's demand")
	}
	if !u.waitNext(context.Background(), time.Hour) {
		t.Error("RenewNow must wake the poll loop")
	}
}

func TestCheckOnce(t *testing.T) {
	served := func(sha, warden, agent string) map[string]httpAnswer {
		return map[string]httpAnswer{
			versionPath:      {status: 200, body: `{"git_sha":"` + sha + `"}`},
			wardenBinaryPath: {status: 200, body: warden},
			agentBinaryPath:  {status: 200, body: agent},
		}
	}
	newUpdater := func(paths *servedPaths, root string) *updater {
		return &updater{
			get:       paths.get,
			ops:       swapOps{inner: osUpdaterOps{}},
			selfPath:  filepath.Join(root, "ocwarden"),
			agentPath: filepath.Join(root, "ocagent"),
			logf:      func(string, ...any) {},
			now:       func() time.Time { return time.Date(2026, 9, 8, 10, 30, 0, 0, time.UTC) },
		}
	}
	stageLive := func(t *testing.T) string {
		root := t.TempDir()
		stageBinary(t, filepath.Join(root, "ocwarden"), "warden-bytes-v1")
		stageBinary(t, filepath.Join(root, "ocagent"), "agent-bytes-v1")
		return root
	}

	t.Run("an unmoved server sha downloads nothing", func(t *testing.T) {
		root := stageLive(t)
		paths := &servedPaths{answers: served("80eec5fe", "served-warden-v9", "served-agent-v9")}
		u := newUpdater(paths, root)
		u.lastSHA = "80eec5fe"
		swapped, err := u.checkOnce()
		if swapped || err != nil {
			t.Errorf("checkOnce = (%v, %v), want (false, nil)", swapped, err)
		}
		if !reflect.DeepEqual(paths.gets, []string{versionPath}) {
			t.Errorf("fetched %v, want only %v", paths.gets, versionPath)
		}
		if got, want := dirState(t, root), []string{"ocagent=agent-bytes-v1", "ocwarden=warden-bytes-v1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the gate must leave the binaries alone: %v, want %v", got, want)
		}
	})

	t.Run("a moved sha reconciles ocagent before ocwarden and records the sha", func(t *testing.T) {
		root := stageLive(t)
		paths := &servedPaths{answers: served("aa11bb22", "served-warden-v9", "served-agent-v9")}
		u := newUpdater(paths, root)
		swapped, err := u.checkOnce()
		if !swapped || err != nil {
			t.Errorf("checkOnce = (%v, %v), want (true, nil)", swapped, err)
		}
		want := []string{versionPath, agentBinaryPath, wardenBinaryPath}
		if !reflect.DeepEqual(paths.gets, want) {
			t.Errorf("fetched %v, want %v", paths.gets, want)
		}
		state := dirState(t, root)
		wantState := []string{
			"ocagent.prev=agent-bytes-v1", "ocagent=served-agent-v9",
			"ocwarden.prev=warden-bytes-v1", "ocwarden=served-warden-v9",
		}
		if !reflect.DeepEqual(state, wantState) {
			t.Errorf("directory =\n  %v\nwant\n  %v", state, wantState)
		}
		if u.lastSHA != "aa11bb22" {
			t.Errorf("lastSHA = %q, want %q", u.lastSHA, "aa11bb22")
		}
		paths.gets = nil
		if swapped, err := u.checkOnce(); swapped || err != nil || !reflect.DeepEqual(paths.gets, []string{versionPath}) {
			t.Errorf("the next cycle = (%v, %v) after fetching %v, want (false, nil) after only the gate",
				swapped, err, paths.gets)
		}
	})

	t.Run("an ocagent-only swap never reports the warden as replaced", func(t *testing.T) {
		root := stageLive(t)
		paths := &servedPaths{answers: served("aa11bb22", "warden-bytes-v1", "served-agent-v9")}
		u := newUpdater(paths, root)
		swapped, err := u.checkOnce()
		if swapped || err != nil {
			t.Errorf("checkOnce = (%v, %v), want (false, nil)", swapped, err)
		}
		want := []string{"ocagent.prev=agent-bytes-v1", "ocagent=served-agent-v9", "ocwarden=warden-bytes-v1"}
		if got := dirState(t, root); !reflect.DeepEqual(got, want) {
			t.Errorf("directory = %v, want %v", got, want)
		}
	})

	t.Run("a failed ocagent reconcile never reaches the ocwarden download", func(t *testing.T) {
		root := stageLive(t)
		answers := served("aa11bb22", "served-warden-v9", "")
		answers[agentBinaryPath] = httpAnswer{status: 500, body: "boom"}
		paths := &servedPaths{answers: answers}
		u := newUpdater(paths, root)
		swapped, err := u.checkOnce()
		if swapped || err == nil || err.Error() != "download ocagent: status 500" {
			t.Errorf("checkOnce = (%v, %v), want (false, download ocagent: status 500)", swapped, err)
		}
		if want := []string{versionPath, agentBinaryPath}; !reflect.DeepEqual(paths.gets, want) {
			t.Errorf("fetched %v, want %v", paths.gets, want)
		}
		if u.lastSHA != "" {
			t.Errorf("a failed cycle must not record the sha, got %q", u.lastSHA)
		}
		want := []string{"ocagent=agent-bytes-v1", "ocwarden=warden-bytes-v1"}
		if got := dirState(t, root); !reflect.DeepEqual(got, want) {
			t.Errorf("directory = %v, want %v", got, want)
		}
	})

	t.Run("a failed ocwarden reconcile leaves the sha unrecorded", func(t *testing.T) {
		root := stageLive(t)
		answers := served("aa11bb22", "", "agent-bytes-v1")
		answers[wardenBinaryPath] = httpAnswer{status: 200, body: ""}
		paths := &servedPaths{answers: answers}
		u := newUpdater(paths, root)
		swapped, err := u.checkOnce()
		if swapped || err == nil || err.Error() != "download ocwarden: empty body" {
			t.Errorf("checkOnce = (%v, %v), want (false, download ocwarden: empty body)", swapped, err)
		}
		if u.lastSHA != "" {
			t.Errorf("a failed cycle must not record the sha, got %q", u.lastSHA)
		}
	})

	t.Run("an unreachable version gate stops the cycle before any download", func(t *testing.T) {
		root := stageLive(t)
		answers := served("aa11bb22", "served-warden-v9", "served-agent-v9")
		answers[versionPath] = httpAnswer{err: errors.New("dial tcp: connection refused")}
		paths := &servedPaths{answers: answers}
		u := newUpdater(paths, root)
		swapped, err := u.checkOnce()
		want := "GET /api/version: dial tcp: connection refused"
		if swapped || err == nil || err.Error() != want {
			t.Errorf("checkOnce = (%v, %v), want (false, %s)", swapped, err, want)
		}
		if !reflect.DeepEqual(paths.gets, []string{versionPath}) {
			t.Errorf("fetched %v, want only the gate", paths.gets)
		}
	})

	t.Run("an unresolved path is skipped rather than downloaded to", func(t *testing.T) {
		root := stageLive(t)
		paths := &servedPaths{answers: served("aa11bb22", "served-warden-v9", "served-agent-v9")}
		u := newUpdater(paths, root)
		u.agentPath = ""
		u.selfPath = ""
		swapped, err := u.checkOnce()
		if swapped || err != nil {
			t.Errorf("checkOnce = (%v, %v), want (false, nil)", swapped, err)
		}
		if !reflect.DeepEqual(paths.gets, []string{versionPath}) {
			t.Errorf("fetched %v, want only the gate", paths.gets)
		}
	})

	t.Run("a station that names no sha is never gated on", func(t *testing.T) {
		root := stageLive(t)
		paths := &servedPaths{answers: served("", "warden-bytes-v1", "agent-bytes-v1")}
		u := newUpdater(paths, root)
		for round := 1; round <= 2; round++ {
			if swapped, err := u.checkOnce(); swapped || err != nil {
				t.Errorf("round %d: checkOnce = (%v, %v), want (false, nil)", round, swapped, err)
			}
		}
		want := []string{versionPath, agentBinaryPath, wardenBinaryPath,
			versionPath, agentBinaryPath, wardenBinaryPath}
		if !reflect.DeepEqual(paths.gets, want) {
			t.Errorf("fetched %v, want %v", paths.gets, want)
		}
	})
}

func TestServerSHA(t *testing.T) {
	cases := []struct {
		name    string
		answer  httpAnswer
		want    string
		wantErr string
	}{
		{"the station names its commit", httpAnswer{status: 200, body: `{"git_sha":"80eec5fe","env":"prod"}`}, "80eec5fe", ""},
		{"a version body with no git_sha", httpAnswer{status: 200, body: `{"env":"prod"}`}, "", ""},
		{"an unauthenticated warden", httpAnswer{status: 401, body: `{"error":"nope"}`}, "", "GET /api/version: status 401"},
		{"a proxy answering html", httpAnswer{status: 200, body: "<html>502</html>"}, "", "decode /api/version: invalid character '<' looking for beginning of value"},
		{"an unreachable station", httpAnswer{err: errors.New("dial tcp: i/o timeout")}, "", "GET /api/version: dial tcp: i/o timeout"},
	}
	for _, c := range cases {
		paths := &servedPaths{answers: map[string]httpAnswer{versionPath: c.answer}}
		got, err := (&updater{get: paths.get}).serverSHA()
		if got != c.want {
			t.Errorf("%s: serverSHA = %q, want %q", c.name, got, c.want)
		}
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: err = %v, want nil", c.name, err)
		case c.wantErr != "" && (err == nil || err.Error() != c.wantErr):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
}

func TestReconcileBinary(t *testing.T) {
	const stamp = "2026-09-08T10:30:00Z"
	newUpdater := func(root string, answer httpAnswer, failOn string) (*updater, *servedPaths, *[]string) {
		paths := &servedPaths{answers: map[string]httpAnswer{wardenBinaryPath: answer}}
		var log []string
		u := &updater{
			get:  paths.get,
			ops:  swapOps{inner: osUpdaterOps{}, failOn: failOn},
			logf: func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) },
			now:  func() time.Time { return time.Date(2026, 9, 8, 10, 30, 0, 0, time.UTC) },
		}
		_ = root
		return u, paths, &log
	}

	t.Run("bytes that already match are left alone", func(t *testing.T) {
		root := t.TempDir()
		live := stageBinary(t, filepath.Join(root, "ocwarden"), "warden-bytes-v1")
		u, _, log := newUpdater(root, httpAnswer{status: 200, body: "warden-bytes-v1"}, "")
		swapped, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden")
		if swapped || err != nil {
			t.Errorf("reconcileBinary = (%v, %v), want (false, nil)", swapped, err)
		}
		if got := dirState(t, root); !reflect.DeepEqual(got, []string{"ocwarden=warden-bytes-v1"}) {
			t.Errorf("directory = %v, want [ocwarden=warden-bytes-v1]", got)
		}
		if u.lastSwap != nil || len(*log) != 0 {
			t.Errorf("a no-op swap announced %v and logged %v", u.lastSwap, *log)
		}
	})

	t.Run("different bytes are verified, backed up, then swapped in", func(t *testing.T) {
		root := t.TempDir()
		live := stageBinary(t, filepath.Join(root, "ocwarden"), "warden-bytes-v1")
		u, _, log := newUpdater(root, httpAnswer{status: 200, body: "served-warden-v9"}, "")
		swapped, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden")
		if !swapped || err != nil {
			t.Errorf("reconcileBinary = (%v, %v), want (true, nil)", swapped, err)
		}
		want := []string{"ocwarden.prev=warden-bytes-v1", "ocwarden=served-warden-v9"}
		if got := dirState(t, root); !reflect.DeepEqual(got, want) {
			t.Errorf("directory = %v, want %v", got, want)
		}
		wantEvent := &selfUpdateEvent{Binary: "ocwarden", OldHash: "9c8d3cb7cabb", NewHash: "49fdd223b5a8", At: stamp}
		if !reflect.DeepEqual(u.lastSwap, wantEvent) {
			t.Errorf("lastSwap = %+v, want %+v", u.lastSwap, wantEvent)
		}
		wantLog := []string{fmt.Sprintf(
			"[ocwarden] self-update: replaced ocwarden at %s (backup: %s.prev)", live, live)}
		if !reflect.DeepEqual(*log, wantLog) {
			t.Errorf("log = %#v, want %#v", *log, wantLog)
		}
	})

	t.Run("a binary that is not on disk yet is written with no backup", func(t *testing.T) {
		root := t.TempDir()
		live := filepath.Join(root, "ocagent")
		u, _, _ := newUpdater(root, httpAnswer{status: 200, body: "served-agent-v9"}, "")
		u.get = (&servedPaths{answers: map[string]httpAnswer{
			agentBinaryPath: {status: 200, body: "served-agent-v9"}}}).get
		swapped, err := u.reconcileBinary(agentBinaryPath, live, "ocagent")
		if !swapped || err != nil {
			t.Errorf("reconcileBinary = (%v, %v), want (true, nil)", swapped, err)
		}
		if got := dirState(t, root); !reflect.DeepEqual(got, []string{"ocagent=served-agent-v9"}) {
			t.Errorf("directory = %v, want [ocagent=served-agent-v9]", got)
		}
		wantEvent := &selfUpdateEvent{Binary: "ocagent", OldHash: "", NewHash: "ee72e4ab28cf", At: stamp}
		if !reflect.DeepEqual(u.lastSwap, wantEvent) {
			t.Errorf("lastSwap = %+v, want %+v", u.lastSwap, wantEvent)
		}
	})

	refusals := []struct {
		name    string
		answer  httpAnswer
		failOn  string
		wantErr string
	}{
		{"a corrupt download fails its health probe", httpAnswer{status: 200, body: "served-warden-v9"}, "probe",
			"verify ocwarden failed — keeping current binary: exec "},
		{"the station refuses the download", httpAnswer{status: 403, body: "denied"}, "",
			"download ocwarden: status 403"},
		{"the station serves nothing", httpAnswer{status: 200, body: ""}, "",
			"download ocwarden: empty body"},
		{"the station is unreachable", httpAnswer{err: errors.New("dial tcp: i/o timeout")}, "",
			"download ocwarden: dial tcp: i/o timeout"},
		{"the temp file cannot be written", httpAnswer{status: 200, body: "served-warden-v9"}, "write-temp",
			"write temp ocwarden: read-only file system"},
		{"the temp file cannot be made executable", httpAnswer{status: 200, body: "served-warden-v9"}, "chmod",
			"chmod temp ocwarden: operation not permitted"},
		{"the retreat copy cannot be written", httpAnswer{status: 200, body: "served-warden-v9"}, "backup",
			"backup current ocwarden -> "},
		{"the atomic swap fails", httpAnswer{status: 200, body: "served-warden-v9"}, "rename",
			"atomic swap ocwarden -> "},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			live := stageBinary(t, filepath.Join(root, "ocwarden"), "warden-bytes-v1")
			before := dirState(t, root)
			u, _, log := newUpdater(root, c.answer, c.failOn)
			swapped, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden")
			if swapped {
				t.Error("a refused reconcile must never report a swap")
			}
			if err == nil || !strings.HasPrefix(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want one starting %q", err, c.wantErr)
			}
			after := dirState(t, root)
			if c.failOn == "rename" {
				before = append([]string{"ocwarden.prev=warden-bytes-v1"}, before...)
			}
			if !reflect.DeepEqual(after, before) {
				t.Errorf("the refused swap left the directory as\n  %v\nwant\n  %v", after, before)
			}
			if u.lastSwap != nil || len(*log) != 0 {
				t.Errorf("a refused swap announced %v and logged %v", u.lastSwap, *log)
			}
		})
	}
}

func TestClock(t *testing.T) {
	pinned := time.Date(2026, 9, 8, 10, 30, 0, 0, time.UTC)
	if got := (&updater{now: func() time.Time { return pinned }}).clock(); !got.Equal(pinned) {
		t.Errorf("clock = %v, want %v", got, pinned)
	}

	before := time.Now()
	got := (&updater{}).clock()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("an unwired clock must read the wall clock: %v is not within [%v, %v]", got, before, after)
	}
}

func TestAnnounceSelfUpdate(t *testing.T) {
	event := &selfUpdateEvent{Binary: "ocwarden", OldHash: "9c8d3cb7cabb", NewHash: "cf6022985580", At: "2026-09-08T10:30:00Z"}

	cases := []struct {
		name      string
		swap      *selfUpdateEvent
		agentID   string
		status    int
		wired     bool
		wantPosts []string
		wantLog   []string
	}{
		{
			name: "no swap, no announcement", swap: nil, agentID: "warden-1", status: 200, wired: true,
		},
		{
			name: "an unidentified warden announces nothing", swap: event, agentID: "  ", status: 200, wired: true,
		},
		{
			name: "an unwired poster announces nothing", swap: event, agentID: "warden-1", wired: false,
		},
		{
			name: "a delivered announcement names both hashes", swap: event, agentID: "warden-1", status: 200, wired: true,
			wantPosts: []string{selfUpdateReportPath},
			wantLog:   []string{"[ocwarden] self-update: announced ocwarden swap 9c8d3cb7cabb->cf6022985580"},
		},
		{
			name: "a refused announcement is swallowed", swap: event, agentID: "warden-1", status: 422, wired: true,
			wantPosts: []string{selfUpdateReportPath},
			wantLog:   []string{"[ocwarden] self-update: announce POST returned status 422 (ignored; swap already applied)"},
		},
		{
			name: "an unreachable station is swallowed", swap: event, agentID: "warden-1", status: 0, wired: true,
			wantPosts: []string{selfUpdateReportPath},
			wantLog:   []string{"[ocwarden] self-update: announce POST returned status 0 (ignored; swap already applied)"},
		},
	}
	for _, c := range cases {
		var posts []string
		var log []string
		u := &updater{
			lastSwap: c.swap,
			agentID:  c.agentID,
			logf:     func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) },
		}
		if c.wired {
			u.post = func(path string, payload map[string]any) (int, map[string]any) {
				posts = append(posts, path)
				return c.status, nil
			}
		}
		u.announceSelfUpdate()
		if !reflect.DeepEqual(posts, c.wantPosts) {
			t.Errorf("%s: posted to %v, want %v", c.name, posts, c.wantPosts)
		}
		if !reflect.DeepEqual(log, c.wantLog) {
			t.Errorf("%s: log = %#v, want %#v", c.name, log, c.wantLog)
		}
	}
}

func TestNextSelfUpdateBackoff(t *testing.T) {
	cases := []struct {
		cur, ceiling, want time.Duration
	}{
		{time.Minute, 30 * time.Minute, 2 * time.Minute},
		{8 * time.Minute, 30 * time.Minute, 16 * time.Minute},
		{16 * time.Minute, 30 * time.Minute, 30 * time.Minute},
		{30 * time.Minute, 30 * time.Minute, 30 * time.Minute},
		{time.Second, 30 * time.Minute, 2 * time.Minute},
		{0, 30 * time.Minute, 2 * time.Minute},
		{-5 * time.Second, 30 * time.Minute, 2 * time.Minute},
		{time.Minute, 90 * time.Second, 90 * time.Second},
	}
	for _, c := range cases {
		if got := nextSelfUpdateBackoff(c.cur, c.ceiling); got != c.want {
			t.Errorf("nextSelfUpdateBackoff(%s, %s) = %s, want %s", c.cur, c.ceiling, got, c.want)
		}
	}
}

func TestResolveSelfExe(t *testing.T) {
	root := t.TempDir()
	real := stageBinary(t, filepath.Join(root, "warden", "ocwarden"), "warden-bytes-v1")
	link := filepath.Join(root, "bin", "ocwarden")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	viaLink := resolveSelfExe(func() (string, error) { return link, nil })
	if strings.Contains(viaLink, filepath.Join(root, "bin")) {
		t.Errorf("resolveSelfExe kept the symlink path: %q", viaLink)
	}
	if filepath.Base(filepath.Dir(viaLink)) != "warden" || filepath.Base(viaLink) != "ocwarden" {
		t.Errorf("resolveSelfExe = %q, want the real .../warden/ocwarden", viaLink)
	}
	if !sameFile(t, viaLink, real) {
		t.Errorf("resolveSelfExe = %q, which is not the file at %q", viaLink, real)
	}

	if got := resolveSelfExe(func() (string, error) { return "/no/such/dir/ocwarden", nil }); got != "/no/such/dir/ocwarden" {
		t.Errorf("an unresolvable symlink must fall back to the raw path, got %q", got)
	}
	if got := resolveSelfExe(func() (string, error) { return "", errors.New("no /proc/self/exe") }); got != "" {
		t.Errorf("an unnameable executable = %q, want \"\"", got)
	}
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	fa, err := os.Stat(a)
	if err != nil {
		t.Fatalf("stat %s: %v", a, err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		t.Fatalf("stat %s: %v", b, err)
	}
	return os.SameFile(fa, fb)
}

func TestSelfUpdateAgentPath(t *testing.T) {
	got := selfUpdateAgentPath(func() (string, error) { return "/opt/officraft/warden/ocwarden", nil })
	if got != "/opt/officraft/warden/ocagent" {
		t.Errorf("selfUpdateAgentPath = %q, want %q", got, "/opt/officraft/warden/ocagent")
	}

	root := t.TempDir()
	real := stageBinary(t, filepath.Join(root, "warden", "ocwarden"), "warden-bytes-v1")
	link := filepath.Join(root, "ocwarden")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got = selfUpdateAgentPath(func() (string, error) { return link, nil })
	if filepath.Base(got) != "ocagent" || filepath.Base(filepath.Dir(got)) != "warden" {
		t.Errorf("selfUpdateAgentPath = %q, want the sibling beside the RESOLVED warden", got)
	}

	if got := selfUpdateAgentPath(func() (string, error) { return "", errors.New("no /proc/self/exe") }); got != "" {
		t.Errorf("an unnameable executable = %q, want \"\"", got)
	}
}

func TestBuildSelfUpdater(t *testing.T) {
	home := t.TempDir()
	exePath := stageBinary(t, filepath.Join(home, ".officraft", "warden", "ocwarden"), "warden-bytes-v1")
	cfg := Config{Base: "https://station.example", Token: jwtWardenOne, ID: "warden-1"}

	var execCalls [][]string
	execImage := func(path string, argv, envv []string) error {
		execCalls = append(execCalls, append([]string{path}, argv...))
		return errors.New("exec did not take")
	}
	var log []string
	logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }

	u := buildSelfUpdater(cfg, rawEnv{lookup: envMap(map[string]string{"HOME": home, "OC_TOKEN": jwtWardenOne})},
		logf, func() (string, error) { return exePath, nil }, execImage)

	if !sameFile(t, u.selfPath, exePath) {
		t.Errorf("selfPath = %q, want the running executable %q", u.selfPath, exePath)
	}
	if want := filepath.Join(filepath.Dir(u.selfPath), "ocagent"); u.agentPath != want {
		t.Errorf("agentPath = %q, want %q", u.agentPath, want)
	}
	if u.interval != 15*time.Minute || u.backoffStart != time.Minute || u.backoffCap != 30*time.Minute {
		t.Errorf("pacing = (%s, %s, %s), want (15m0s, 1m0s, 30m0s)", u.interval, u.backoffStart, u.backoffCap)
	}
	if cap(u.kick) != 1 {
		t.Errorf("the kick channel holds %d wakes, want 1 (coalescing)", cap(u.kick))
	}
	if u.agentID != "warden-1" {
		t.Errorf("agentID = %q, want %q", u.agentID, "warden-1")
	}
	for name, wired := range map[string]bool{
		"get": u.get != nil, "ops": u.ops != nil, "sleep": u.sleep != nil, "exit": u.exit != nil,
		"post": u.post != nil, "now": u.now != nil, "execSelf": u.execSelf != nil,
		"renew": u.renew != nil, "verify": u.verify != nil, "writeTok": u.writeTok != nil,
	} {
		if !wired {
			t.Errorf("buildSelfUpdater left %s unwired", name)
		}
	}
	if _, ok := u.ops.(osUpdaterOps); !ok {
		t.Errorf("ops = %T, want osUpdaterOps", u.ops)
	}
	if want := filepath.Join(home, ".officraft", "warden", "exec-warden.tok"); u.tokfilePath != want {
		t.Errorf("tokfilePath = %q, want %q", u.tokfilePath, want)
	}
	if u.token != jwtWardenOne || u.envToken != jwtWardenOne {
		t.Error("the credential this process runs on, and the one its starter exported, must both be carried")
	}

	if err := u.execSelf(); err == nil || err.Error() != "exec did not take" {
		t.Errorf("execSelf = %v, want the injected exec's error", err)
	}
	want := append([]string{u.selfPath}, os.Args...)
	if len(execCalls) != 1 || !reflect.DeepEqual(execCalls[0], want) {
		t.Errorf("execSelf ran %v, want one exec of %v", execCalls, want)
	}

	u.logf("hello %s", "there")
	if !reflect.DeepEqual(log, []string{"hello there"}) {
		t.Errorf("log = %#v, want [hello there]", log)
	}

	bare := buildSelfUpdater(cfg, rawEnv{lookup: envMap(map[string]string{"HOME": home})},
		logf, func() (string, error) { return exePath, nil }, execImage)
	if bare.envToken != "" {
		t.Errorf("envToken = %q, want \"\" when nobody exported one", bare.envToken)
	}
}
