package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultBase   = "http://127.0.0.1:7755"
	telemetryPath = "/api/monitoring/telemetry"
	// The SAME telemetry ingest endpoint: the server folds command_result there onto
	// the durable member.
	commandResultPath = telemetryPath
	userAgent         = "ocwarden/0.1"
	reportThrottle    = 30 * time.Second
	backoffStart      = 1 * time.Second
	backoffCap        = 60 * time.Second
	httpTimeout       = 10 * time.Second
	subprocessBudget  = 5 * time.Second
	// Deliberately SHORT and INDEPENDENT of the SSE/telemetry clients: a slow/dead
	// server must never stall the command reader after a kill/spawn.
	commandReportTimeout = 5 * time.Second

	shutdownGrace = 5 * time.Second
)

// Base stays EMPTY when OC_BASE is unset — filling it with defaultBase was the
// T-88 defect. The station-address gate (basegate.go) refuses an empty Base.
type Config struct {
	Base  string
	Token string
	ID    string
}

func loadConfig(env func(string) string) Config {
	base, _ := baseFromEnv(env)
	base = strings.TrimRight(base, "/")
	token := env("OC_TOKEN")
	id := env("OC_ID")
	if id == "" && token != "" {
		id = jwtSub(token)
	}
	return Config{Base: base, Token: token, ID: id}
}

const defaultTokfileRel = "/.officraft/warden/exec-warden.tok"

func readTokfile(env func(string) string, readFile func(string) ([]byte, error)) string {
	path := tokfilePath(env)
	if path == "" {
		return ""
	}
	raw, err := readFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// tokfilePath is the single owner of which file holds the token: readTokfile reads
// it and the self-renewal loop writes it, and they must name the same file or a
// renewal writes a credential nothing reads.
func tokfilePath(env func(string) string) string {
	if path := env("OC_WARDEN_TOKFILE"); path != "" {
		return path
	}
	home := env("HOME")
	if home == "" {
		return ""
	}
	ns, err := namespaceFromEnv(env)
	if err != nil {
		return ""
	}
	return tokfileFor(home, ns)
}

func tokfileEnv(env func(string) string, readFile func(string) ([]byte, error)) func(string) string {
	return func(k string) string {
		v := env(k)
		if k == "OC_TOKEN" && v == "" {
			if tok := readTokfile(env, readFile); tok != "" {
				return tok
			}
		}
		return v
	}
}

func jwtSub(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return ""
	}
	if sub, ok := claims["sub"].(string); ok {
		return sub
	}
	return ""
}

type CmdRunner interface {
	Run(name string, args ...string) (string, error)
}

type execRunner struct{ timeout time.Duration }

var newCmdRunner = func(timeout time.Duration) CmdRunner { return execRunner{timeout: timeout} }

// exec is the process choke point of the whole binary, so refuseInTestBinary lives
// HERE, not only on the seam constructors: a caller can assemble the struct itself
// (an inline `sysOps{run: execRunner{…}.Run, …}` once made a test binary issue a
// REAL `launchctl bootout` against the developer's live warden), but it cannot
// avoid starting the subprocess here.
func (r execRunner) Run(name string, args ...string) (string, error) {
	return r.exec("execRunner.Run", false, name, args...)
}

// RunKeepStdout also returns stdout from a non-zero exit (`claude auth status`
// answers a logged-out host with exit 1 AND its verdict on stdout). stdout never
// enters the error: it can carry the account's email and organization.
func (r execRunner) RunKeepStdout(name string, args ...string) (string, error) {
	return r.exec("execRunner.RunKeepStdout", true, name, args...)
}

func (r execRunner) exec(caller string, keepStdout bool, name string, args ...string) (string, error) {
	refuseInTestBinary(caller + "(" + name + ")")
	to := r.timeout
	if to == 0 {
		to = subprocessBudget
	}
	var out, errb bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	// stderr goes into the returned error so callers can CLASSIFY a non-zero exit
	// (e.g. the tmux three-way probe telling "can't find session" from a broken probe).
	cmd.Stderr = &errb
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			kept := ""
			if keepStdout {
				kept = out.String()
			}
			if msg := strings.TrimSpace(errb.String()); msg != "" {
				return kept, fmt.Errorf("%w: %s", err, msg)
			}
			return kept, err
		}
		return out.String(), nil
	case <-time.After(to):
		_ = cmd.Process.Kill()
		<-done
		return "", fmt.Errorf("timeout after %s", to)
	}
}

var (
	battPctRe = regexp.MustCompile(`(\d{1,3})%`)
	cpuIdleRe = regexp.MustCompile(`CPU usage:.*?([\d.]+)%\s*idle`)
	// The page size is read, not assumed: 4096 on Intel, 16384 on Apple silicon.
	vmPageSizeRe = regexp.MustCompile(`page size of (\d+) bytes`)
	// Separators are `[ \t]`, NOT `\s`: Go's `\s` matches a newline, so a label whose
	// line carries no number would adopt the NEXT line's digits.
	vmCounterRe = regexp.MustCompile(`(?m)^[ \t]*"?([A-Za-z][^":]*?)"?:[ \t]*(\d+)\.?[ \t]*$`)
)

func parseBattery(text string) (pct int, pctOK bool, ac bool, acOK bool) {
	if m := battPctRe.FindStringSubmatch(text); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil && v >= 0 && v <= 100 {
			pct, pctOK = v, true
		}
	}
	switch {
	case strings.Contains(text, "'AC Power'") || strings.Contains(text, "AC attached"):
		ac, acOK = true, true
	case strings.Contains(text, "'Battery Power'"):
		ac, acOK = false, true
	}
	return
}

func parseCPUPct(text string) (float64, bool) {
	m := cpuIdleRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	idle, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return round1(math.Max(0, math.Min(100, 100-idle))), true
}

func parseVMStat(text string) (pageSize float64, counts map[string]float64, ok bool) {
	m := vmPageSizeRe.FindStringSubmatch(text)
	if m == nil {
		return 0, nil, false
	}
	size, err := strconv.ParseFloat(m[1], 64)
	if err != nil || size <= 0 {
		return 0, nil, false
	}
	counts = map[string]float64{}
	for _, line := range vmCounterRe.FindAllStringSubmatch(text, -1) {
		if v, err := strconv.ParseFloat(line[2], 64); err == nil {
			counts[strings.TrimSpace(line[1])] = v
		}
	}
	return size, counts, true
}

// ParseUint, deliberately not ParseFloat: ParseFloat accepts "NaN", which passes
// `v <= 0` and the clamp, then makes json.Marshal fail on the WHOLE heartbeat —
// reported as status 0, indistinguishable from an unreachable server.
func parseMemTotalBytes(text string) (float64, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil || v == 0 {
		return 0, false
	}
	return float64(v), true
}

// parseRAMPct uses the constituents of Activity Monitor's "Memory Used":
//
//	(App Memory + Wired + Compressed) / hw.memsize
//	App Memory = Anonymous pages - Pages purgeable
//
// Not claimed byte-identical to Activity Monitor (no macOS interface returns that
// aggregate); measured 44.09 GB vs its 44.72 GB on a 64 GiB box, constituents
// confirmed mutually exclusive on real output.
//
// Do not go back to `top`'s PhysMem `used / (used + unused)`: macOS counts
// reclaimable file cache as `used`, so the cockpit showed 99% on a machine
// Activity Monitor showed half empty.
//
// The three constituents are REQUIRED (without one it is a different quantity);
// `Pages purgeable` is an optional correction worth well under a percentage point.
func parseRAMPct(vmStat, memTotal string) (float64, bool) {
	total, ok := parseMemTotalBytes(memTotal)
	if !ok {
		return 0, false
	}
	pageSize, counts, ok := parseVMStat(vmStat)
	if !ok {
		return 0, false
	}
	anonymous, hasAnonymous := counts["Anonymous pages"]
	wired, hasWired := counts["Pages wired down"]
	compressed, hasCompressed := counts["Pages occupied by compressor"]
	if !hasAnonymous || !hasWired || !hasCompressed {
		return 0, false
	}
	// The floor stays although purgeable is a subset of Anonymous today: a negative
	// App Memory would silently DEFLATE the reading.
	app := anonymous - counts["Pages purgeable"]
	if app < 0 {
		app = 0
	}
	used := (app + wired + compressed) * pageSize
	return round1(math.Max(0, math.Min(100, used/total*100.0))), true
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func collectHardware(r CmdRunner, platform string) map[string]any {
	hw := map[string]any{}
	if !strings.HasPrefix(platform, "darwin") {
		return hw
	}
	if batt, err := r.Run("pmset", "-g", "batt"); err == nil {
		if pct, ok, ac, acOK := parseBattery(batt); true {
			if ok {
				hw["battery_pct"] = pct
			}
			if acOK {
				hw["ac_power"] = ac
			}
		}
	}
	if top, err := r.Run("top", "-l1", "-n0"); err == nil {
		if cpu, ok := parseCPUPct(top); ok {
			hw["cpu_pct"] = cpu
		}
	}
	if vmStat, err := r.Run("vm_stat"); err == nil {
		if memTotal, err := r.Run("sysctl", "-n", "hw.memsize"); err == nil {
			if ram, ok := parseRAMPct(vmStat, memTotal); ok {
				hw["ram_pct"] = ram
			}
		}
	}
	return hw
}

func readMachineName(r CmdRunner) string {
	if name, err := r.Run("scutil", "--get", "ComputerName"); err == nil {
		if s := strings.TrimSpace(name); s != "" {
			return s
		}
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return ""
}

// agent_id is NOT a body key: the reporting identity is the verified JWT sub, and
// AgentTelemetryIngestDTO (additionalProperties:false) does not declare it —
// sending it 422s the ENTIRE heartbeat. Keys are sent only when non-empty: absent
// and "unknown" are different claims on that wire.
func buildTelemetryPayload(agentID, machine string, hardware map[string]any,
	binaries map[string]string, claude map[string]any, shape, effect string,
	runtimes ...map[string]any) (map[string]any, error) {
	aid := strings.TrimSpace(agentID)
	if aid == "" {
		return nil, fmt.Errorf("agent id is required (an unidentified warden has nothing to report)")
	}
	payload := map[string]any{}
	if machine != "" {
		payload["machine"] = machine
	}
	if len(hardware) > 0 {
		payload["hardware"] = hardware
	}
	if len(binaries) > 0 {
		payload["binaries"] = binaries
	}
	if len(claude) > 0 {
		payload["claude"] = claude
	}
	if shape != "" {
		payload["warden_shape"] = shape
	}
	if effect != "" {
		payload["cutover_effect"] = effect
	}
	if len(runtimes) > 0 && len(runtimes[0]) > 0 {
		payload["runtimes"] = runtimes[0]
	}
	return payload, nil
}

type Poster func(path string, payload map[string]any) (int, map[string]any)

func httpPoster(client *http.Client, base, token string) Poster {
	return func(path string, payload map[string]any) (int, map[string]any) {
		body, err := json.Marshal(payload)
		if err != nil {
			return 0, nil
		}
		req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(body))
		if err != nil {
			return 0, nil
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var obj map[string]any
		_ = json.Unmarshal(raw, &obj)
		return resp.StatusCode, obj
	}
}

type ReportResult struct {
	Posted bool
	Status int
	Reason string
	// Set only on an accepted heartbeat, from its receipt.
	LoginCheckInterval time.Duration
}

func errorMessageOf(body map[string]any) string {
	envelope, _ := body["error"].(map[string]any)
	message, _ := envelope["message"].(string)
	return strings.TrimSpace(message)
}

func nextBackoff(cur time.Duration) time.Duration {
	if cur < backoffStart {
		cur = backoffStart
	}
	next := cur * 2
	if next > backoffCap {
		return backoffCap
	}
	return next
}

func runOnce(cfg Config, collect func() map[string]any, machine func() string, post Poster,
	binaries func() map[string]string, claude func() map[string]any, shape func() string,
	effect func() string, runtimes ...func() map[string]any) ReportResult {
	if cfg.Token == "" || cfg.ID == "" {
		return ReportResult{Reason: "no OC_TOKEN/OC_ID"}
	}
	hardware := collect()
	var bins map[string]string
	if binaries != nil {
		bins = binaries()
	}
	var cl map[string]any
	if claude != nil {
		cl = claude()
	}
	var shp string
	if shape != nil {
		shp = shape()
	}
	var eff string
	if effect != nil {
		eff = effect()
	}
	var runtimeCaps map[string]any
	if len(runtimes) > 0 && runtimes[0] != nil {
		runtimeCaps = runtimes[0]()
	}
	if len(hardware) == 0 && len(bins) == 0 && len(cl) == 0 && shp == "" && eff == "" && len(runtimeCaps) == 0 {
		return ReportResult{Reason: "no hardware probed (skip POST)"}
	}
	payload, err := buildTelemetryPayload(cfg.ID, machine(), hardware, bins, cl, shp, eff, runtimeCaps)
	if err != nil {
		return ReportResult{Reason: "build rejected: " + err.Error()}
	}
	status, body := post(telemetryPath, payload)
	if status == 200 {
		return ReportResult{Posted: true, Status: 200, Reason: "posted",
			LoginCheckInterval: loginCheckIntervalFromReceipt(body)}
	}
	reason := fmt.Sprintf("post status %d", status)
	if detail := errorMessageOf(body); detail != "" {
		reason += ": " + detail
	}
	return ReportResult{Status: status, Reason: reason}
}

func run(ctx context.Context, cfg Config, collect func() map[string]any, machine func() string, post Poster,
	binaries func() map[string]string, claude func() map[string]any, shape func() string,
	effect func() string, sleep func(context.Context, time.Duration) bool, iterations int, out io.Writer,
	loginInterval func(time.Duration), runtimes ...func() map[string]any) int {

	if cfg.Token == "" || cfg.ID == "" {
		fmt.Fprintln(out, "[ocwarden] run: no OC_TOKEN/OC_ID — nothing to report; exiting.")
		return 0
	}
	backoff := backoffStart
	for count := 0; iterations <= 0 || count < iterations; count++ {
		if ctx.Err() != nil {
			return 0
		}
		result := runOnce(cfg, collect, machine, post, binaries, claude, shape, effect, runtimes...)
		if result.Posted && loginInterval != nil {
			loginInterval(result.LoginCheckInterval)
		}
		wait := backoff
		if result.Posted || result.Status == 0 {
			backoff = backoffStart
			wait = reportThrottle
		} else {
			// A refusal must leave a trace: a warden whose every heartbeat was rejected
			// (machine row null in the cockpit) otherwise looks healthy. Transport faults
			// (status 0) stay quiet: a server being down is expected.
			fmt.Fprintf(out, "[ocwarden] telemetry: %s (report NOT stored)\n", result.Reason)
			backoff = nextBackoff(backoff)
		}
		if !sleep(ctx, wait) {
			return 0
		}
	}
	return 0
}

func sleepUntil(ctx context.Context, d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func waitGraceful(wg *sync.WaitGroup, grace time.Duration) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// A function rather than three lines in realMain because the serve branch is
// never entered by tests (`run --once` returns first), so a deleted wiring line
// stayed green — and is silent in production until a release day or a key
// removal.
//
// renew is NOT Kick: a bare Kick finds the credential "not due", because the
// station's demand is not a property of the token. RenewNow raises the demand
// first; wiring Renew to Kick compiles, runs, logs nothing and renews nothing.
func wireUpdaterSeams(transport *sseTransport, up *updater) {
	// A server redeploy drops the stream and the warden reconnects within ~1s, so the
	// fresh binary propagates in seconds instead of up to the 15m poll.
	transport.onConnect = up.Kick
	transport.deps.Update = up.Kick
	transport.deps.Renew = up.RenewNow
}

func realMain(argv []string, env func(string) string, out io.Writer) int {
	if len(argv) > 0 {
		switch argv[0] {
		case "install":
			force := false
			for _, a := range argv[1:] {
				if a == "--force" || a == "-force" {
					force = true
				}
			}
			return installCmd(env, out, force)
		case "teardown":
			canonical := false
			for _, a := range argv[1:] {
				if a == "--canonical" {
					canonical = true
					continue
				}
				fmt.Fprintf(out, "usage: ocwarden teardown [--canonical]\n")
				return 2
			}
			return teardownCmd(env, out, canonical)
		case cutoverSubcmd:
			// Internal step of the legacy->anchor migration, spawned detached by a
			// legacy-shape `run`; deliberately absent from the usage banner.
			return cutoverCmd(env, out)
		case "codex-session":
			return runCodexSession(argv[1:], env, out)
		case "version", "--version", "-v":
			return cmdVersion(out)
		}
	}

	fs := flag.NewFlagSet("ocwarden run", flag.ContinueOnError)
	fs.SetOutput(out)
	once := fs.Bool("once", false, "run a single collect->POST cycle then exit (test hook)")

	if len(argv) == 0 || argv[0] != "run" {
		fmt.Fprintln(out, "usage: ocwarden {run [--once] | install | teardown [--canonical]}")
		fmt.Fprintln(out, "  run       officraft per-machine hardware telemetry + command producer.")
		fmt.Fprintln(out, "  install   install + start the launchd warden job on this machine.")
		fmt.Fprintln(out, "  teardown  stop + remove a namespaced warden; canonical requires --canonical.")
		return 0
	}
	if err := fs.Parse(argv[1:]); err != nil {
		return 2
	}

	// Validate the namespace BEFORE any derivation: a malformed OC_NAMESPACE silently
	// folding back to the main instance's socket/paths would cross-wire two instances.
	if _, err := namespaceFromEnv(env); err != nil {
		fmt.Fprintf(out, "[ocwarden] FATAL: %v\n", err)
		return 1
	}

	if err := claudeHomeEntryGate(env, os.Getwd); err != nil {
		fmt.Fprintf(out, "[ocwarden] FATAL: %v\n", err)
		return 1
	}

	// renv folds OC_TOKEN in from the token file (the launchd env carries
	// OC_WARDEN_TOKFILE, never the token itself).
	renv := tokfileEnv(env, os.ReadFile)

	// T-88 STATION-ADDRESS GATE. Its position is load-bearing: everything below talks
	// to the station or decides where members point, and once that has run with a
	// guessed address the damage is done. Deleting it still compiles and starts; a
	// test exists solely to make that deletion red.
	if rc, stop := stationAddressGate(renv, out, *once, time.Now, gateBlock); stop {
		return rc
	}

	cfg := loadConfig(renv)
	runner := newCmdRunner(subprocessBudget)
	collect := func() map[string]any { return collectHardware(runner, runtime.GOOS) }
	machine := func() string { return readMachineName(runner) }
	post := httpPoster(&http.Client{Timeout: httpTimeout}, cfg.Base, cfg.Token)
	var wardenPathsOrNil *wardenPaths
	anchorPath := ""
	agentSocket := ""
	if exe, err := os.Executable(); err == nil {
		if p, perr := resolvePaths(renv, exe, os.Getuid()); perr == nil {
			wardenPathsOrNil = &p
			anchorPath = p.anchorPath
			agentSocket = tmuxSocketFor(p.namespace)
		}
	}
	fingerprints := newBinFingerprinter(os.Executable, anchorPath)
	wardenShapeOf := newShapeReporter(anchorPath, os.Getppid())
	cutoverEffectOf := newCutoverEffectReporter(anchorPath, agentSocket, os.Getppid())
	claudeProbe := newClaudeProber(env, runner, runtime.GOOS)
	launchEnv := &launchEnvCache{}

	iters := 0
	if *once {
		iters = 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	// Presence is NOT the warden's concern: the server projects it from its own
	// SSE-connection view.
	// Log lines carry a UTC RFC3339 prefix because launchd captures this stream
	// verbatim to ocwarden.out.log, which has no per-line time.
	logf := func(format string, args ...any) {
		fmt.Fprintf(out, "%s "+format+"\n", append([]any{time.Now().UTC().Format(time.RFC3339)}, args...)...)
	}
	if cfg.Token != "" && iters == 0 && wardenPathsOrNil != nil {
		maybeStartAnchorCutover(*wardenPathsOrNil, os.Getppid(), logf)
	}

	if cfg.Token != "" && cfg.ID != "" && iters == 0 {
		transport := newCommandTransport(cfg, env, runner, launchEnv, logf)

		// 🔴 NOTHING GUARDS THIS CALL SITE. Deleting these lines, or `go up.run(ctx)`,
		// leaves the package green and silently stops self-update and credential renewal
		// fleet-wide (credentials carry a 30-day life). `rawEnv{lookup: renv}` compiles
		// too and hands renewal the tokfile-folded view, tripping the infinite-exec guard
		// on every launchd warden. Only an integration test running realMain's serve
		// branch against a fake station would catch these; it does not exist.
		up := newSelfUpdater(cfg, rawEnv{lookup: env}, logf)

		// MUST happen BEFORE transport.run starts: connectOnce reads onConnect from its
		// own goroutine.
		wireUpdaterSeams(transport, up)

		wg.Add(1)
		go func() { defer wg.Done(); transport.run(ctx) }()
		logf("[ocwarden] command reader: enabled (SSE %s%s)", cfg.Base, eventsPath)

		wg.Add(1)
		go func() { defer wg.Done(); up.run(ctx) }()
		logf("[ocwarden] self-update: enabled (poll %s; %s + %s; reconnect-kick on)", selfUpdateInterval, wardenBinaryPath, agentBinaryPath)
	}

	var keep stdoutRunner
	if k, ok := runner.(stdoutRunner); ok {
		keep = k
	}
	login := newLoginProber(env, runner, keep, runtime.GOOS, launchEnv, logf)
	runtimeProbe := func() map[string]any {
		return collectRuntimeCapabilities(env, runner, claudeProbe.collect(), login.state())
	}
	rc := run(ctx, cfg, collect, machine, post, fingerprints.collect, claudeProbe.collect,
		wardenShapeOf, cutoverEffectOf, sleepUntil, iters, out, login.setInterval, runtimeProbe)

	stop()
	waitGraceful(&wg, shutdownGrace)
	return rc
}

func main() {
	os.Exit(realMain(os.Args[1:], os.Getenv, os.Stdout))
}
