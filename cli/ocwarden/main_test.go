package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRunner is the mock shell seam: it maps an argv key -> canned stdout, so a
// test drives the darwin probe path on any OS with zero subprocess.
type fakeRunner struct{ out map[string]string }

func (f fakeRunner) Run(name string, args ...string) (string, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	if s, ok := f.out[key]; ok {
		return s, nil
	}
	return "", os.ErrNotExist
}

// sample probe fixtures (trimmed real macOS output).
var fakeProbes = map[string]string{
	"pmset -g batt":             "Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t87%; charged; 0:00 remaining present: true",
	"top -l1 -n0":               "CPU usage: 12.50% user, 7.50% sys, 80.00% idle\nPhysMem: 12G used (2G wired), 4G unused.",
	"scutil --get ComputerName": "Seth's MacBook Pro\n",
}

// TestFullChain_MockShellToHTTPServer exercises the whole run_once chain:
// mock runner feeds fake pmset/top/scutil -> collect -> build payload -> POST to
// an httptest.Server -> assert the received request body + headers.
func TestFullChain_MockShellToHTTPServer(t *testing.T) {
	var gotAuth, gotUA, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		if r.URL.Path != telemetryPath {
			t.Errorf("path = %q, want %q", r.URL.Path, telemetryPath)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	runner := fakeRunner{out: fakeProbes}
	cfg := Config{Base: srv.URL, Token: "tok-123", ID: "agent-xyz"}
	collect := func() map[string]any { return collectHardware(runner, "darwin") }
	machine := func() string { return readMachineName(runner) }
	post := httpPoster(srv.Client(), cfg.Base, cfg.Token)

	binaries := func() map[string]string {
		return map[string]string{"ocwarden": "aaaabbbbcccc", "ocagent": "ddddeeeeffff"}
	}
	claude := func() map[string]any {
		return map[string]any{"version": "2.1.211", "cred_file": true, "sub_readable": true, "keychain": false}
	}
	res := runOnce(cfg, collect, machine, post, binaries, claude)

	if !res.Posted || res.Status != 200 {
		t.Fatalf("runOnce = %+v, want posted 200", res)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want Bearer tok-123", gotAuth)
	}
	if gotUA != userAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, userAgent)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody["agent_id"] != "agent-xyz" {
		t.Errorf("agent_id = %v, want agent-xyz", gotBody["agent_id"])
	}
	if gotBody["machine"] != "Seth's MacBook Pro" {
		t.Errorf("machine = %v, want Seth's MacBook Pro", gotBody["machine"])
	}
	hw, ok := gotBody["hardware"].(map[string]any)
	if !ok {
		t.Fatalf("hardware missing/wrong type: %v", gotBody["hardware"])
	}
	if hw["battery_pct"] != float64(87) { // JSON numbers decode to float64
		t.Errorf("battery_pct = %v, want 87", hw["battery_pct"])
	}
	if hw["ac_power"] != true {
		t.Errorf("ac_power = %v, want true", hw["ac_power"])
	}
	if hw["cpu_pct"] != 20.0 { // 100 - 80 idle
		t.Errorf("cpu_pct = %v, want 20", hw["cpu_pct"])
	}
	if hw["ram_pct"] != 75.0 { // 12G / (12G+4G)
		t.Errorf("ram_pct = %v, want 75", hw["ram_pct"])
	}
	bins, ok := gotBody["binaries"].(map[string]any)
	if !ok {
		t.Fatalf("binaries missing/wrong type: %v", gotBody["binaries"])
	}
	if bins["ocwarden"] != "aaaabbbbcccc" || bins["ocagent"] != "ddddeeeeffff" {
		t.Errorf("binaries = %v, want the injected fingerprints", bins)
	}
	cl, ok := gotBody["claude"].(map[string]any)
	if !ok {
		t.Fatalf("claude missing/wrong type: %v", gotBody["claude"])
	}
	if cl["version"] != "2.1.211" || cl["cred_file"] != true ||
		cl["sub_readable"] != true || cl["keychain"] != false {
		t.Errorf("claude = %v, want the injected probe", cl)
	}
}

// TestBuildTelemetryPayload_ClaudeField: the claude probe rides the payload
// only when non-empty (T-97ee) — an empty probe omits the field entirely, so
// an old-style heartbeat is byte-identical to before the probe existed.
func TestBuildTelemetryPayload_ClaudeField(t *testing.T) {
	probe := map[string]any{"version": "2.1.211", "cred_file": true, "sub_readable": false, "keychain": true}
	payload, err := buildTelemetryPayload("agent-1", "m", map[string]any{"cpu_pct": 1.0}, nil, probe)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got, ok := payload["claude"].(map[string]any)
	if !ok || got["version"] != "2.1.211" || got["sub_readable"] != false {
		t.Fatalf("claude = %v, want the probe map", payload["claude"])
	}

	payload, err = buildTelemetryPayload("agent-1", "m", map[string]any{"cpu_pct": 1.0}, nil, map[string]any{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, present := payload["claude"]; present {
		t.Fatalf("empty probe must omit the claude field, got %v", payload["claude"])
	}
	payload, err = buildTelemetryPayload("agent-1", "m", map[string]any{"cpu_pct": 1.0}, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, present := payload["claude"]; present {
		t.Fatalf("nil probe must omit the claude field, got %v", payload["claude"])
	}
}

// TestRunOnce_ClaudeOnlyCyclePosts: a probe-only cycle is a valid POST (the
// claude field is first-class, same as a fingerprints-only cycle).
func TestRunOnce_ClaudeOnlyCyclePosts(t *testing.T) {
	var gotBody map[string]any
	post := func(path string, payload map[string]any) (int, map[string]any) {
		gotBody = payload
		return 200, nil
	}
	res := runOnce(Config{Base: "x", Token: "t", ID: "i"},
		func() map[string]any { return map[string]any{} },
		func() string { return "m" },
		post, nil,
		func() map[string]any { return map[string]any{"cred_file": false, "sub_readable": false} })
	if !res.Posted {
		t.Fatalf("claude-only cycle must post, got %+v", res)
	}
	cl, _ := gotBody["claude"].(map[string]any)
	if cl["cred_file"] != false {
		t.Fatalf("claude fold = %v, want the probe", gotBody["claude"])
	}
}

func TestParseBattery(t *testing.T) {
	pct, ok, ac, acOK := parseBattery("Now drawing from 'AC Power' 87%;")
	if !ok || pct != 87 || !acOK || !ac {
		t.Fatalf("got pct=%d ok=%v ac=%v acOK=%v", pct, ok, ac, acOK)
	}
	_, ok, _, acOK = parseBattery("desktop, no battery here")
	if ok || acOK {
		t.Fatalf("expected no pct/ac for desktop, got ok=%v acOK=%v", ok, acOK)
	}
	_, _, ac, acOK = parseBattery("Now drawing from 'Battery Power' 55%")
	if !acOK || ac {
		t.Fatalf("expected ac=false, got ac=%v acOK=%v", ac, acOK)
	}
}

func TestParseCPUAndRAM(t *testing.T) {
	if v, ok := parseCPUPct("CPU usage: 5.00% user, 5.00% sys, 90.00% idle"); !ok || v != 10.0 {
		t.Errorf("cpu = %v ok=%v, want 10", v, ok)
	}
	if _, ok := parseCPUPct("garbage"); ok {
		t.Errorf("cpu should fail on garbage")
	}
	if v, ok := parseRAMPct("PhysMem: 8G used (1G wired), 8G unused."); !ok || v != 50.0 {
		t.Errorf("ram = %v ok=%v, want 50", v, ok)
	}
}

func TestCollectHardware_NonDarwinEmpty(t *testing.T) {
	hw := collectHardware(fakeRunner{out: fakeProbes}, "linux")
	if len(hw) != 0 {
		t.Fatalf("non-darwin should be empty, got %v", hw)
	}
}

func TestCollectHardware_OmitOnProbeFailure(t *testing.T) {
	// runner with no fixtures -> every probe errors -> every field omitted.
	hw := collectHardware(fakeRunner{out: map[string]string{}}, "darwin")
	if len(hw) != 0 {
		t.Fatalf("all probes failed, expected empty, got %v", hw)
	}
}

func TestRunOnce_SkipsWhenNoToken(t *testing.T) {
	res := runOnce(Config{Base: "x", Token: "", ID: ""},
		func() map[string]any { return map[string]any{"cpu_pct": 1.0} },
		func() string { return "m" },
		func(string, map[string]any) (int, map[string]any) {
			t.Fatal("post must not be called without token")
			return 0, nil
		}, nil, nil)
	if res.Posted || res.Status != 0 {
		t.Fatalf("expected skip, got %+v", res)
	}
}

func TestRunOnce_SkipsEmptyHardware(t *testing.T) {
	res := runOnce(Config{Base: "x", Token: "t", ID: "i"},
		func() map[string]any { return map[string]any{} },
		func() string { return "m" },
		func(string, map[string]any) (int, map[string]any) {
			t.Fatal("post must not be called with empty hardware")
			return 0, nil
		}, nil, nil)
	if res.Reason != "no hardware probed (skip POST)" {
		t.Fatalf("expected empty-hw skip, got %+v", res)
	}
}

func TestRunLoop_Once(t *testing.T) {
	posts := 0
	slept := 0
	rc := run(context.Background(), Config{Base: "x", Token: "t", ID: "i"},
		func() map[string]any { return map[string]any{"cpu_pct": 5.0} },
		func() string { return "m" },
		func(string, map[string]any) (int, map[string]any) { posts++; return 200, nil },
		nil, nil,
		func(context.Context, time.Duration) bool { slept++; return true },
		1, io.Discard)
	if rc != 0 || posts != 1 || slept != 1 {
		t.Fatalf("once loop: rc=%d posts=%d slept=%d, want 0/1/1", rc, posts, slept)
	}
}

// ---------------------------------------------------------------------------
// cancellation seam / graceful shutdown
// ---------------------------------------------------------------------------

// waitBounded fails the test if cond does not become true within a short bound,
// so a shutdown regression (a loop that ignores ctx and hangs) fails fast instead
// of dragging out to the 10min `go test` timeout.
func waitBounded(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

// TestSleepUntil_ElapsesFully: with an uncancelled ctx the full duration elapses
// and it reports true (the normal-interval path the --once byte behaviour rides on).
func TestSleepUntil_ElapsesFully(t *testing.T) {
	start := time.Now()
	if ok := sleepUntil(context.Background(), 20*time.Millisecond); !ok {
		t.Fatalf("sleepUntil should report full elapse (true)")
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("sleepUntil returned too early (%s); should have waited the interval", elapsed)
	}
}

// TestSleepUntil_EarlyWakeOnCancel: a ctx cancelled MID-sleep wakes the sleeper
// immediately (well before the interval) and reports false — this is what stops a
// shutdown from waiting out a full 30s telemetry interval.
func TestSleepUntil_EarlyWakeOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	start := time.Now()
	if ok := sleepUntil(ctx, 30*time.Second); ok {
		t.Fatalf("sleepUntil should report cancellation (false), not full elapse")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("sleepUntil did not wake early on cancel; waited %s of a 30s interval", elapsed)
	}
}

// TestSleepUntil_AlreadyCancelled: a ctx already cancelled on entry is an immediate
// no-op false — the loop's "cancelled between cycles" fast path.
func TestSleepUntil_AlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok := sleepUntil(ctx, 30*time.Second); ok {
		t.Fatalf("sleepUntil on a cancelled ctx must return false immediately")
	}
}

// TestRun_CtxCancelStopsForeverLoop: the forever (iterations<=0) telemetry loop
// exits cleanly and promptly when its root ctx is cancelled — the graceful-shutdown
// contract for the foreground producer loop. The injected sleep seam is ctx-aware
// (mirrors sleepUntil) so cancelling ctx wakes it.
func TestRun_CtxCancelStopsForeverLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var posts int32
	done := make(chan int, 1)
	go func() {
		rc := run(ctx, Config{Base: "x", Token: "t", ID: "i"},
			func() map[string]any { return map[string]any{"cpu_pct": 5.0} },
			func() string { return "m" },
			func(string, map[string]any) (int, map[string]any) { atomic.AddInt32(&posts, 1); return 200, nil },
			nil, nil,
			func(c context.Context, d time.Duration) bool { return sleepUntil(c, d) },
			0, io.Discard)
		done <- rc
	}()
	// Let it turn the loop at least once, then cancel and assert a bounded exit.
	waitBounded(t, func() bool { return atomic.LoadInt32(&posts) >= 1 }, "forever loop to POST at least once")
	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("run rc = %d, want 0", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("forever telemetry loop did not exit within 2s of ctx cancel (leak/hang)")
	}
}

// TestWaitGraceful_ReturnsWhenLoopsExit models realMain's shutdown join: two
// ctx-aware loops registered on a WaitGroup exit when the root ctx is cancelled, and
// waitGraceful returns (drained, not timed out) — proving no goroutine is leaked.
func TestWaitGraceful_ReturnsWhenLoopsExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-ctx.Done() }() // ctx-aware fake loop
	}
	cancel()
	start := time.Now()
	waitGraceful(&wg, 2*time.Second) // grace is the CEILING, not the expected wait
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waitGraceful should return as soon as loops drain, took %s", elapsed)
	}
}

// TestWaitGraceful_BoundedWhenWedged proves the grace bound: a goroutine that NEVER
// finishes cannot hang process exit — waitGraceful returns after ~grace regardless.
func TestWaitGraceful_BoundedWhenWedged(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1) // never Done — a wedged loop
	start := time.Now()
	waitGraceful(&wg, 50*time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond {
		t.Fatalf("waitGraceful returned before the grace bound (%s)", elapsed)
	}
	if elapsed > time.Second {
		t.Fatalf("waitGraceful did not honour its grace bound; took %s", elapsed)
	}
}

func TestLoadConfig_JWTSubFallback(t *testing.T) {
	// build a token with sub=jwt-sub-id; header.payload.sig
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"jwt-sub-id"}`))
	token := "h." + payload + ".s"
	env := map[string]string{"OC_TOKEN": token, "OC_BASE": "http://x/"}
	cfg := loadConfig(func(k string) string { return env[k] })
	if cfg.ID != "jwt-sub-id" {
		t.Errorf("id = %q, want jwt-sub-id (from jwt sub)", cfg.ID)
	}
	if cfg.Base != "http://x" {
		t.Errorf("base = %q, want trailing slash stripped", cfg.Base)
	}
}

// TestTokfileEnv_ExplicitTokenWins: a set OC_TOKEN is passed straight through and
// the token file is NOT consulted (explicit env always wins — the folded launcher
// only supplied a FALLBACK).
func TestTokfileEnv_ExplicitTokenWins(t *testing.T) {
	env := func(k string) string {
		if k == "OC_TOKEN" {
			return "explicit-token"
		}
		if k == "OC_WARDEN_TOKFILE" {
			return "/should/not/be/read"
		}
		return ""
	}
	readFile := func(string) ([]byte, error) {
		t.Fatal("token file must not be read when OC_TOKEN is set")
		return nil, nil
	}
	if got := tokfileEnv(env, readFile)("OC_TOKEN"); got != "explicit-token" {
		t.Errorf("OC_TOKEN = %q, want explicit-token", got)
	}
}

// TestTokfileEnv_FallbackToTokfile: with OC_TOKEN unset, the wrapper reads
// OC_WARDEN_TOKFILE and trims whitespace (mirrors the launcher's `$(cat …)`), and
// loadConfig then derives OC_ID from the token's jwt sub — proving the whole fold
// (launcher tokfile read → OC_ID derivation) works end-to-end through the binary.
func TestTokfileEnv_FallbackToTokfile(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"tok-sub"}`))
	token := "h." + payload + ".s"
	tokPath := t.TempDir() + "/exec-warden.tok"
	if err := os.WriteFile(tokPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{"OC_WARDEN_TOKFILE": tokPath, "OC_BASE": "http://x"}
	env := func(k string) string { return base[k] }
	cfg := loadConfig(tokfileEnv(env, os.ReadFile))
	if cfg.Token != token {
		t.Errorf("token = %q, want %q (trailing newline trimmed)", cfg.Token, token)
	}
	if cfg.ID != "tok-sub" {
		t.Errorf("id = %q, want tok-sub (derived from tokfile jwt sub)", cfg.ID)
	}
}

// TestTokfileEnv_DefaultPathFromHome: OC_WARDEN_TOKFILE unset falls back to
// $HOME/.officraft/warden/exec-warden.tok — the exact default the retired bin/warden-go
// launcher used (`${OC_WARDEN_TOKFILE:-$HOME/.officraft/warden/exec-warden.tok}`).
func TestTokfileEnv_DefaultPathFromHome(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(home+"/.officraft/warden", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home+"/.officraft/warden/exec-warden.tok", []byte("home-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	if got := tokfileEnv(env, os.ReadFile)("OC_TOKEN"); got != "home-token" {
		t.Errorf("OC_TOKEN = %q, want home-token (from $HOME default tokfile)", got)
	}
}

// TestTokfileEnv_MissingTokfileFailsSafe: an unreadable/absent token file leaves
// OC_TOKEN empty (fail-safe — the run loops then log + exit 0, never crash).
func TestTokfileEnv_MissingTokfileFailsSafe(t *testing.T) {
	env := func(k string) string {
		if k == "OC_WARDEN_TOKFILE" {
			return t.TempDir() + "/does-not-exist.tok"
		}
		return ""
	}
	if got := tokfileEnv(env, os.ReadFile)("OC_TOKEN"); got != "" {
		t.Errorf("OC_TOKEN = %q, want empty (missing tokfile must fail-safe)", got)
	}
}
