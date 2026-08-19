// Command ocwarden is a self-contained Go port of officraft's Python
// `ocwarden run` subcommand: it collects host hardware telemetry via shell
// probes and POSTs it to the officraft monitoring endpoint on a throttled
// loop. `--once` runs a single collect->POST cycle then exits (the test hook).
//
// Design note: the shell seam is an injectable CmdRunner interface. Real runs
// use execRunner (os/exec); tests inject a fake runner + httptest.Server to
// drive the whole chain with zero subprocess and zero network.
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
	// commandResultPath is where the best-effort command_result receipt is POSTed —
	// the SAME telemetry ingest endpoint (the server folds command_result there onto
	// the durable member). Aliased for call-site clarity at the reporter.
	commandResultPath = telemetryPath
	userAgent         = "ocwarden/0.1"
	reportThrottle    = 30 * time.Second
	backoffStart      = 1 * time.Second
	backoffCap        = 60 * time.Second
	httpTimeout       = 10 * time.Second
	subprocessBudget  = 5 * time.Second
	// commandReportTimeout bounds the best-effort command_result POST (fleet remote-
	// ops stage 1). Deliberately SHORT and INDEPENDENT of the SSE/telemetry clients:
	// a slow/dead server must never stall the command reader after a kill/spawn. On
	// timeout the poster returns a falsy status and the reporter swallows it.
	commandReportTimeout = 5 * time.Second
	// shutdownGrace bounds how long realMain waits, after the root context is
	// cancelled, for the command reader loop to observe the cancellation and return.
	// A ctx-aware loop unwinds well within this; the bound exists purely so a single
	// wedged goroutine can never hang process exit forever.
	shutdownGrace = 5 * time.Second
)

// ---------------------------------------------------------------------------
// config (mirrors warden/config.py)
// ---------------------------------------------------------------------------

// Config is the resolved warden identity. Base always has a value; Token/ID are
// empty when unset (a mis-wired launch must degrade, never crash).
type Config struct {
	Base  string
	Token string
	ID    string
}

// loadConfig resolves OC_* env into a Config. Base is stripped of a trailing
// slash; ID defaults to the JWT `sub` claim of the token.
func loadConfig(env func(string) string) Config {
	base := env("OC_BASE")
	if base == "" {
		base = defaultBase
	}
	base = strings.TrimRight(base, "/")
	token := env("OC_TOKEN")
	id := env("OC_ID")
	if id == "" && token != "" {
		id = jwtSub(token)
	}
	return Config{Base: base, Token: token, ID: id}
}

// defaultTokfileRel is the token file path (relative to $HOME) the former
// bin/warden-go launcher defaulted to. Kept here so the folded-in resolution is
// byte-equivalent to the launcher's `${OC_WARDEN_TOKFILE:-$HOME/.officraft/warden/exec-warden.tok}`.
const defaultTokfileRel = "/.officraft/warden/exec-warden.tok"

// readTokfile resolves the exec-warden token from a token file, mirroring the
// retired bin/warden-go launcher. Path precedence: OC_WARDEN_TOKFILE if set,
// else $HOME + defaultTokfileRel (the launcher default; launchd supplies HOME via
// the plist EnvironmentVariables, exactly as `export HOME=...` did in the wrapper).
// Whitespace is trimmed to match the launcher's `OC_TOKEN="$(cat …)"` (command
// substitution strips trailing newlines). A missing/unreadable file yields "" —
// the binary then fail-safes exactly as an unset OC_TOKEN (run() logs + exits 0).
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

// tokfilePath resolves WHICH file holds the exec-warden token, and is the single
// owner of that derivation: readTokfile reads it, and the self-renewal loop writes
// it. Those two must name the same file or a renewal writes a credential nothing
// will ever read — so they share this function rather than each parsing the env.
// Empty means the path could not be derived (no HOME, malformed namespace), which
// every caller reads as "do nothing" rather than as a licence to guess a path.
func tokfilePath(env func(string) string) string {
	if path := env("OC_WARDEN_TOKFILE"); path != "" {
		return path
	}
	home := env("HOME")
	if home == "" {
		return ""
	}
	// The default follows the OC_NAMESPACE instance root (byte-identical to
	// $HOME + defaultTokfileRel for the empty namespace); an invalid namespace
	// fail-safes to "no token" like every other mis-wire.
	ns, err := namespaceFromEnv(env)
	if err != nil {
		return ""
	}
	return tokfileFor(home, ns)
}

// tokfileEnv wraps env so an unset OC_TOKEN falls back to the token file
// (OC_WARDEN_TOKFILE, else $HOME/.officraft/warden/exec-warden.tok). This folds the
// former bin/warden-go wrapper (read tokfile → export OC_TOKEN → exec `ocwarden
// run`) INTO the binary: launchd points OC_WARDEN_TOKFILE at the exec-warden
// token file and the binary self-resolves the token, no launcher script. An
// explicitly-set OC_TOKEN always wins (unchanged behaviour); every other key is
// passed straight through, so downstream env reads (HOME, OC_CLAUDE_BIN, …) are
// untouched.
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

// jwtSub reads the `sub` claim of a JWT WITHOUT verifying (identity-display
// only). A malformed token yields "".
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

// ---------------------------------------------------------------------------
// command runner seam (mirrors the injectable Runner in warden/hardware.py)
// ---------------------------------------------------------------------------

// CmdRunner runs one argv and returns its stdout, or an error on ANY failure
// (nonzero exit, timeout, missing binary). This is the single shell seam;
// tests inject a fake, real runs use execRunner.
type CmdRunner interface {
	Run(name string, args ...string) (string, error)
}

// execRunner is the real (os/exec) runner: timeout-boxed, stdout on success.
type execRunner struct{ timeout time.Duration }

// newCmdRunner is the SINGLE production construction point for the real exec
// runner, and — like newHostSeam (install.go) — it is a package-level var so the
// test binary can rebind it in TestMain (hostseam_test.go). Production code must
// obtain its runner from here, never by writing `execRunner{…}` inline.
var newCmdRunner = func(timeout time.Duration) CmdRunner { return execRunner{timeout: timeout} }

// Run execs one argv. THIS IS THE PROCESS CHOKE POINT OF THE WHOLE BINARY: every
// launchctl bootout/bootstrap/kickstart, every plutil, every tmux and probe call
// that is not already behind a seam ends up here, and here is the last place a
// caller can still be stopped BEFORE the machine is touched.
//
// WHY refuseInTestBinary IS HERE AND NOT ONLY ON THE SEAM CONSTRUCTORS
// -------------------------------------------------------------------
// The static guards in hostseam_test.go pin two IDENTIFIERS (realSysOps,
// realHostSeam). Independent review defeated them with a mutant that never
// writes either name: an inline `sysOps{run: execRunner{…}.Run, rename: os.Rename,
// …}` composite literal in teardownCmd. All the source scans stayed green, no
// refusal fired, and the test binary issued a REAL
// `launchctl bootout gui/<uid>/com.officraft.ocwarden` against the developer
// machine's live warden — the runtime tests then failed, but only afterwards
// ("no host seam was constructed"), which is detection, not defence.
//
// A guard on the seam CONSTRUCTORS can always be routed around, because a caller
// can assemble the struct itself. A guard on the exec syscall cannot: however the
// struct was assembled, the subprocess still has to be started here. So a test
// binary that reaches a real exec dies here, before exec.Command runs.
func (r execRunner) Run(name string, args ...string) (string, error) {
	refuseInTestBinary("execRunner.Run(" + name + ")")
	to := r.timeout
	if to == 0 {
		to = subprocessBudget
	}
	var out, errb bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	// Capture stderr into the returned error so callers that need to CLASSIFY a
	// non-zero exit (e.g. the tmux three-way probe distinguishing "can't find
	// session" from a broken probe) can read it. Telemetry only checks err==nil,
	// so this is behaviourally invisible to the existing hardware chain.
	cmd.Stderr = &errb
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			if msg := strings.TrimSpace(errb.String()); msg != "" {
				return "", fmt.Errorf("%w: %s", err, msg)
			}
			return "", err
		}
		return out.String(), nil
	case <-time.After(to):
		_ = cmd.Process.Kill()
		<-done
		return "", fmt.Errorf("timeout after %s", to)
	}
}

// ---------------------------------------------------------------------------
// hardware parsers (IO-free) + collector
// ---------------------------------------------------------------------------

var (
	battPctRe = regexp.MustCompile(`(\d{1,3})%`)
	cpuIdleRe = regexp.MustCompile(`CPU usage:.*?([\d.]+)%\s*idle`)
	// vm_stat's header ("Mach Virtual Memory Statistics: (page size of 16384
	// bytes)") — the page size is NOT assumed: it is 4096 on Intel and 16384 on
	// Apple silicon, and hardcoding either turns every page count into a number
	// four times wrong on the other architecture.
	vmPageSizeRe = regexp.MustCompile(`page size of (\d+) bytes`)
	// One vm_stat counter line: `Pages free:  230372.` — label, colon, count,
	// optional trailing period. The separators are `[ \t]`, NOT `\s`: Go's `\s`
	// matches a newline, so `\s+` would let a label whose own line carries no
	// number reach across and adopt the NEXT line's digits (a counter that
	// silently became someone else's value, which is worse than a missing one).
	// Pinned by TestParseVMStat_ALabelNeverAdoptsTheNextLinesNumber.
	vmCounterRe = regexp.MustCompile(`(?m)^[ \t]*"?([A-Za-z][^":]*?)"?:[ \t]*(\d+)\.?[ \t]*$`)
)

// parseBattery: `pmset -g batt` -> (pct, pctOK, ac, acOK). pct is the first
// NN% token clamped to 0..100; ac is true on AC / false on Battery / unknown->!ok.
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

// parseCPUPct: `top -l1 -n0` -> busy% = 100-idle (1dp, clamped), ok=false when absent.
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

// parseVMStat: `vm_stat` -> page size in bytes + every counter line by label.
// ok=false when the header carries no page size, because every count below is
// meaningless without it.
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

// parseMemTotalBytes: `sysctl -n hw.memsize` -> installed physical memory in
// bytes. This is the ONLY honest denominator for a "percent of RAM" reading.
//
// ParseUint, deliberately not ParseFloat. A byte count is an integer, and
// ParseFloat additionally accepts "NaN", "+Inf" and hex-float forms — none of
// which `v <= 0` rejects, because NaN compares false against everything. A NaN
// denominator survives the clamp (math.Min(100, NaN) is NaN) and then makes
// json.Marshal fail on the WHOLE heartbeat, which httpPoster reports as status 0
// — indistinguishable from the server being unreachable. That is the shape where
// hardware, binaries, claude and runtimes all go null at once and the reporter
// still looks healthy; the parser is the cheap place to close it.
func parseMemTotalBytes(text string) (float64, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil || v == 0 {
		return 0, false
	}
	return float64(v), true
}

// parseRAMPct reports memory USED as a percent of installed physical memory,
// built from the same three constituents Activity Monitor's "Memory Used" is
// composed of:
//
//	(App Memory + Wired + Compressed) / hw.memsize
//	App Memory = Anonymous pages - Pages purgeable
//
// Deliberately NOT claimed to be byte-identical to what Activity Monitor prints:
// no macOS interface returns that aggregate, so nothing here or in CI can verify
// such a claim. What IS measured: on a 64 GiB box this computed 44.09 GB where
// Activity Monitor showed 44.72 GB, and the three constituents were confirmed
// mutually exclusive on real output (Anonymous + File-backed came to exactly
// active + inactive + speculative, i.e. Anonymous excludes both wired and
// compressor, so nothing is double-counted).
//
// It replaces a reading taken from `top`'s PhysMem line — `used / (used +
// unused)` — which was wrong twice over, and wrong in the same direction both
// times, so the cockpit reported a machine at 99% while Activity Monitor showed
// it half empty with memory pressure green and zero swap:
//
//  1. DENOMINATOR. `used + unused` is not installed memory. On a 64 GiB box
//     those two summed to 64281 MiB against a real 65536 MiB, inflating the
//     ratio on its own.
//  2. SEMANTICS, and this is the big one. macOS counts RECLAIMABLE file cache
//     inside `used`, and a machine that has done any file I/O parks tens of GB
//     there on purpose. Cache is not consumption: the kernel hands it back on
//     demand, which is why pressure stays green while `used` reads ~99%. On the
//     measured box 20.23 GB of the "used" total was cache — 19.62 GB of
//     file-backed pages plus 0.61 GB purgeable.
//
// Every input is a page COUNT, and the page size is read from vm_stat's own
// header (4096 on Intel, 16384 on Apple silicon) rather than assumed.
//
// The three constituent counters are REQUIRED — without any one of them the
// answer is not "approximately used", it is a different quantity, so the probe
// omits itself and the cockpit shows a blank it already knows how to explain.
// `Pages purgeable` is treated as an optional CORRECTION (it subtracts volatile
// allocations an app has told the kernel it may drop): missing it overstates by
// well under a percentage point, which does not warrant discarding the reading.
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
	// Purgeable is a subset of Anonymous on every macOS this runs on, so this
	// cannot go negative today; the floor is here because the two counters are
	// read from separate lines of a tool we do not control, and a negative App
	// Memory would silently DEFLATE the reading — the one direction that turns a
	// genuinely full machine into a quiet one.
	app := anonymous - counts["Pages purgeable"]
	if app < 0 {
		app = 0
	}
	used := (app + wired + compressed) * pageSize
	// One acknowledged inconsistency: everywhere else this function prefers to
	// omit over reporting something plausible-but-wrong, and here an impossible
	// ratio (more consumed than installed) is clamped to a clean 100% instead —
	// which is the very number that was used to raise the false alarm. Clamping
	// wins because the arithmetic cannot exceed 100 unless a counter and the
	// denominator disagree about the machine, which no observed macOS does; a
	// 140% would be a rendering bug on top of a data bug. Revisit if any real
	// machine is ever seen hitting the ceiling.
	return round1(math.Max(0, math.Min(100, used/total*100.0))), true
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// collectHardware collects this host's snapshot via the injectable runner.
// Non-darwin -> empty map. Each probe is independent and omit-on-fail; never panics.
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
	// ram_pct needs BOTH memory probes: vm_stat for what is actually consumed,
	// hw.memsize for what the box actually has. Either one missing omits the key
	// rather than falling back to the old top-based reading — the cockpit already
	// distinguishes "never measured" from a value, and a gauge that reads 99% on a
	// healthy machine is worse than a blank one (it was used to raise an alarm).
	if vmStat, err := r.Run("vm_stat"); err == nil {
		if memTotal, err := r.Run("sysctl", "-n", "hw.memsize"); err == nil {
			if ram, ok := parseRAMPct(vmStat, memTotal); ok {
				hw["ram_pct"] = ram
			}
		}
	}
	return hw
}

// readMachineName: `scutil --get ComputerName` via the runner, falling back to
// the OS hostname. Never fatal — a read fault degrades to "".
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

// ---------------------------------------------------------------------------
// telemetry payload (mirrors warden/telemetry.py)
// ---------------------------------------------------------------------------

// buildTelemetryPayload assembles the POST body. Errors on an empty agent id (the
// one hard requirement — a warden with no identity has nothing to report), but
// the id is NOT a body key: the reporting identity is the verified JWT sub, and
// the frozen AgentTelemetryIngestDTO (additionalProperties:false) does not
// declare `agent_id`. The server refuses unknown fields on mutable writes, so
// sending it 422s the ENTIRE heartbeat — hardware, binaries, claude probe and
// runtime capabilities together, which is exactly how every machine row went
// silently null. machine/hardware/binaries/claude are included only when
// non-empty. binaries carries the live ocwarden/ocagent content fingerprints
// (fingerprint.go) the server folds into the machine rows' bin_status verdict
// (T-5f01); claude carries the local claude CLI probe (claudeprobe.go) the server
// folds into the machine rows' claude_* columns (T-97ee). warden_shape carries
// the launchd SHAPE verdict (cutover.go detectShape) the fleet reads to tell a
// converted machine from an unconverted one (T-ff5d); it is conditional for the
// same reason as the others — an always-present key would mean this producer can
// never send a body that fails the server's "at least one field" check by
// accident, but it would also mean a warden that cannot answer still asserts
// something, and absent vs "unknown" are different claims on that wire.
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

// ---------------------------------------------------------------------------
// http seam (mirrors warden/http.py) — injectable via the Poster func type
// ---------------------------------------------------------------------------

// Poster POSTs a payload to path and returns (status, body-or-nil). A falsy
// status (0) means a transport failure (DNS/refused/timeout).
type Poster func(path string, payload map[string]any) (int, map[string]any)

// httpPoster builds the real POST-to-{base}{path} closure with a Bearer token.
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
			return 0, nil // connection refused / DNS / timeout — a falsy status
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var obj map[string]any
		_ = json.Unmarshal(raw, &obj)
		return resp.StatusCode, obj
	}
}

// ---------------------------------------------------------------------------
// producer (mirrors warden/producer.py)
// ---------------------------------------------------------------------------

// ReportResult is the outcome of one runOnce cycle.
type ReportResult struct {
	Posted bool
	Status int // 0 = no POST attempted
	Reason string
}

// errorMessageOf pulls `error.message` out of the server's error envelope
// ({"error":{"code","message"}}). Any other shape yields "".
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

// runOnce runs ONE collect->build->POST cycle. Fail-safe: no token/id -> skip;
// nothing collected at all -> skip (a bodyless telemetry POST is a guaranteed
// 400). binaries (nil-safe) adds the live binary fingerprints; claude
// (nil-safe, symmetric) adds the local claude CLI probe (claudeprobe.go). A
// fingerprints-only or probe-only cycle (e.g. a non-darwin host with no
// hardware probes) still posts — both are first-class telemetry fields, and so
// is shape (nil-safe, same shape as the other two).
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
		return ReportResult{Posted: true, Status: 200, Reason: "posted"}
	}
	// Carry the server's own explanation, not just the code. A schema refusal names
	// the offending key, which is the difference between a one-line diagnosis and a
	// multi-hour hunt.
	reason := fmt.Sprintf("post status %d", status)
	if detail := errorMessageOf(body); detail != "" {
		reason += ": " + detail
	}
	return ReportResult{Status: status, Reason: reason}
}

// run is the throttled producer loop. iterations<=0 means forever; 1 = --once.
// A mis-wire (no token/id) logs one line and returns 0.
//
// CTX-AWARE: every inter-cycle wait goes through the ctx-aware sleep seam, so a
// cancelled ctx (SIGINT/SIGTERM) ends the loop promptly instead of sleeping out a
// full 30s interval. --once semantics are unchanged: with a fresh (uncancelled)
// ctx the single collect->POST cycle runs and the trailing sleep elapses exactly as
// before (byte-identical wire behaviour; tests inject an instant sleep seam).
func run(ctx context.Context, cfg Config, collect func() map[string]any, machine func() string, post Poster,
	binaries func() map[string]string, claude func() map[string]any, shape func() string,
	effect func() string, sleep func(context.Context, time.Duration) bool, iterations int, out io.Writer,
	runtimes ...func() map[string]any) int {

	if cfg.Token == "" || cfg.ID == "" {
		fmt.Fprintln(out, "[ocwarden] run: no OC_TOKEN/OC_ID — nothing to report; exiting.")
		return 0
	}
	backoff := backoffStart
	for count := 0; iterations <= 0 || count < iterations; count++ {
		if ctx.Err() != nil {
			return 0 // cancelled between cycles → clean exit
		}
		result := runOnce(cfg, collect, machine, post, binaries, claude, shape, effect, runtimes...)
		wait := backoff
		if result.Posted || result.Status == 0 {
			backoff = backoffStart
			wait = reportThrottle
		} else {
			// A server REFUSAL (any non-200) must leave a trace. runOnce has always
			// computed the verdict and the loop has always thrown it away, so a
			// warden whose every heartbeat was rejected — leaving the whole machine
			// row null in the cockpit — looked identical to a healthy one from the
			// outside, for as long as it ran. Transport faults (status 0) stay quiet:
			// a server being down is expected and would spam the log.
			fmt.Fprintf(out, "[ocwarden] telemetry: %s (report NOT stored)\n", result.Reason)
			backoff = nextBackoff(backoff)
		}
		if !sleep(ctx, wait) {
			return 0 // cancelled during the inter-cycle wait → clean exit
		}
	}
	return 0
}

// sleepUntil is the ctx-aware sleep seam used by the telemetry loop. It waits for
// d but returns early (false) the instant ctx is cancelled, so a
// shutdown signal need not wait out a full interval. It returns true iff the whole
// duration elapsed. A non-positive d is a no-op that still honours a cancelled ctx.
// A single timer per call keeps it -race clean (no shared mutable state).
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

// waitGraceful blocks until wg is drained OR grace elapses, whichever comes first.
// It is the graceful-shutdown join point: after the root ctx is cancelled the
// background loops unwind and Done their WaitGroup; this waits for that (so no
// goroutine leaks), but the grace bound guarantees a wedged goroutine can never
// hang process exit. With an empty group (the --once path) it returns immediately.
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

// ---------------------------------------------------------------------------
// cli (mirrors warden/cli.py)
// ---------------------------------------------------------------------------

func realMain(argv []string, env func(string) string, out io.Writer) int {
	// Subcommand dispatch. `run` is the telemetry/command producer (below). `install`
	// and `teardown` own the launchd job lifecycle (the Go replacement of the retired
	// flip-era bash installer); both are self-contained entry points that resolve
	// their own env contract.
	if len(argv) > 0 {
		switch argv[0] {
		case "install":
			// --force overrides the one-warden-per-machine guard (replace a warden bound
			// to a different machine id without an explicit teardown first).
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
			// Internal step of the automatic legacy->anchor shape migration
			// (T-ff5d), spawned detached by a legacy-shape `run`. Deliberately
			// absent from the usage banner below: it is not an operator verb.
			return cutoverCmd(env, out)
		case "codex-session":
			return runCodexSession(argv[1:], env, out)
		case "version", "--version", "-v":
			// Print WHICH build this is (git sha/time/dirty when stamped + always a
			// content self-hash) so a human can tell an eva self-updated binary apart
			// from the committed bin/ artifact. Deliberately NOT part of the usage
			// banner because build identity is reported through the dedicated version command.
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

	// Validate the instance namespace BEFORE any derivation: a malformed
	// OC_NAMESPACE silently folding back to the main instance's socket/paths
	// would cross-wire two instances — refuse loudly instead.
	if _, err := namespaceFromEnv(env); err != nil {
		fmt.Fprintf(out, "[ocwarden] FATAL: %v\n", err)
		return 1
	}

	// Resolve OC_TOKEN from the token file when unset (folds in the retired
	// bin/warden-go launcher: OC_WARDEN_TOKFILE → token → OC_ID via jwtSub). An
	// explicit OC_TOKEN still wins; a missing file leaves OC_TOKEN empty and the
	// loops below fail-safe (no token/id → log + clean exit).
	// renv is env with OC_TOKEN folded in from the tokfile; both loadConfig and
	// the shape check below need the resolved token (the launchd warden's env
	// carries OC_WARDEN_TOKFILE, never the token itself).
	renv := tokfileEnv(env, os.ReadFile)
	cfg := loadConfig(renv)
	runner := newCmdRunner(subprocessBudget)
	collect := func() map[string]any { return collectHardware(runner, runtime.GOOS) }
	machine := func() string { return readMachineName(runner) }
	post := httpPoster(&http.Client{Timeout: httpTimeout}, cfg.Base, cfg.Token)
	// The install paths this machine's launchd job is (or would be) wired to.
	// Resolved ONCE here because two heartbeat signals and the one-shot cutover
	// hook below all need the anchor path, and resolvePaths is the single owner of
	// that derivation. A resolve fault (no HOME, no OC_TOKEN, malformed namespace)
	// leaves anchorPath empty, which every consumer reads as "cannot say".
	var wardenPathsOrNil *wardenPaths
	anchorPath := ""
	// The tmux socket this instance's agents live on. Derived from the SAME
	// wardenPaths as anchorPath so a namespaced instance judges its own carriers
	// rather than the canonical instance's; empty when paths did not resolve,
	// which every consumer reads as "cannot say".
	agentSocket := ""
	if exe, err := os.Executable(); err == nil {
		if p, perr := resolvePaths(renv, exe, os.Getuid()); perr == nil {
			wardenPathsOrNil = &p
			anchorPath = p.anchorPath
			agentSocket = tmuxSocketFor(p.namespace)
		}
	}
	// The live-binary fingerprints riding each heartbeat (T-5f01) — cached by
	// stat identity, so most cycles cost two stats, not two multi-MB hashes.
	// Since T-ff5d the never-replaced TCC anchor is fingerprinted alongside them.
	fingerprints := newBinFingerprinter(os.Executable, anchorPath)
	// Which launchd SHAPE this warden is actually running under (T-ff5d), read
	// from the PARENT process every cycle. Without it the fleet cannot tell a
	// machine that completed the legacy->anchor migration from one that never
	// started it — the whole point of shipping the migration.
	wardenShapeOf := newShapeReporter(anchorPath, os.Getppid())
	// Whether that cutover is actually IN EFFECT for the processes that carry
	// agents (T-17b4). warden_shape above answers "who is WARDEN's parent"; this
	// answers "were the tmux servers the agents live in created under the anchor
	// identity", which is the question people were reading off the shape badge and
	// which it cannot answer. Three-valued and fail-closed — see cutovereffect.go.
	cutoverEffectOf := newCutoverEffectReporter(anchorPath, agentSocket, os.Getppid())
	// The local claude CLI probe riding the heartbeat (T-97ee) — TTL-cached
	// (claudeprobe.go), so most cycles cost nothing; the tokfile-wrapped env is
	// NOT needed here (the probe reads HOME/OC_CLAUDE_BIN, never OC_TOKEN).
	claudeProbe := newClaudeProber(env, runner, runtime.GOOS)

	iters := 0
	if *once {
		iters = 1
	}

	// Signal-driven root context: SIGINT/SIGTERM cancels ctx, and every long-lived
	// loop below observes that cancellation to shut down GRACEFULLY — no hard kill of
	// an in-flight SSE read / dispatch / spawn, no leaked goroutine. On --once
	// (iters==1) the background loops are never started and run() returns after a
	// single cycle, so this wiring is inert; defer stop() always unregisters the
	// signal handlers. (signal.NotifyContext == cancel-the-ctx on the listed signals.)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Background loops register with wg so that, on shutdown, realMain waits for them
	// to observe cancellation and return before the process exits (no goroutine leak).
	var wg sync.WaitGroup

	// Phase 4a-②b "ears": the outbound SSE command reader (GET /api/events → command
	// dispatch). Starts unconditionally whenever a real token/id is present and this is
	// not a --once run (iters==0): server-orchestrated STOP is the single, unconditional
	// path. Presence is NOT the warden's concern: the server projects it from its own
	// SSE-connection view, so the warden only spawns/kills + reports hardware telemetry.
	// Every runtime log line is prefixed with a UTC RFC3339 timestamp so warden
	// events (command reader, dispatch/spawn/kill, self-update) can be correlated
	// in time when debugging an agent — launchd captures this stream verbatim to
	// ocwarden.out.log, which otherwise carries no per-line time. UTC RFC3339
	// matches the other timestamps in this codebase (command_result.At, ocagent
	// context-report).
	logf := func(format string, args ...any) {
		fmt.Fprintf(out, "%s "+format+"\n", append([]any{time.Now().UTC().Format(time.RFC3339)}, args...)...)
	}
	// T-ff5d: one-shot launchd SHAPE check. Self-update is a syscall.Exec in-place
	// swap (same PID), so a machine that has just updated is running this code
	// immediately — the conversion does not wait for a launchd restart. Skipped on
	// --once (a test hook, never a launchd job) and when there is no token to hand
	// the installer. Best-effort by contract: it never fails the warden.
	if cfg.Token != "" && iters == 0 && wardenPathsOrNil != nil {
		maybeStartAnchorCutover(*wardenPathsOrNil, os.Getppid(), logf)
	}

	if cfg.Token != "" && cfg.ID != "" && iters == 0 {
		transport := newCommandTransport(cfg, env, runner, logf)

		// Self-update: poll the authoritative server on its own cadence and, when a
		// fresh committed prebuilt is served, verify + atomically swap ocwarden/ocagent
		// in place — then (ocwarden swap only) exec the new binary in place, same PID
		// (launchd KeepAlive does NOT relaunch an exited warden; see selfupdate.go).
		// Same start gate as the command reader (real token/id, not --once) and
		// same ctx-aware graceful-shutdown contract (registered with wg). A separate
		// goroutine from the 30s telemetry loop: binary reconcile runs at a much slower
		// cadence and must not be entangled with the well-tested telemetry cycle.
		// rawEnv{} names which of the two views this is; renv would not compile here.
		//
		// 🔴 NOTHING GUARDS THIS CALL SITE, AND THAT IS A STATEMENT, NOT AN OVERSIGHT.
		// Delete these three lines, or just the `go up.run(ctx)` below, and the whole
		// package stays green: self-update and credential renewal stop fleet-wide,
		// silently, and with credentials carrying a 30-day life the machines start
		// falling off the roster a month later. The third and cheapest-looking edit
		// is `rawEnv{lookup: renv}` — one identifier, reads like a typo, compiles
		// (the struct only stops the bare `renv`, which is deliberate: it makes the
		// substitution a visible decision instead of an accident) — and it hands the
		// renewal path the tokfile-folded view, whose envToken reports the token FILE
		// as if somebody had exported OC_TOKEN, tripping the infinite-exec guard on
		// every launchd warden and stopping the fleet renewing with one log line per
		// machine. renewwiring_reached_test.go catches that substitution inside
		// newSelfUpdater; nothing catches it here. What newSelfUpdater BUILDS is now
		// asserted by using it (renewwiring_reached_test.go); whether realMain calls
		// it is not asserted by anything.
		//
		// Why there is no check here: the only kind available is a syntax check over
		// this file, and this repo has now written that check twice and had review
		// walk through it four times in one round without renaming anything — it
		// reads identifiers, and every way to disable the feature leaves the
		// identifiers alone. Adding a fifth version would be pretending. The real
		// instrument is an integration test that runs realMain against a fake station
		// and observes a renewal happen, and it does not exist because this block is
		// gated on `iters == 0` — the forever-loop path, the one no test enters (the
		// tests that call realMain all pass `run --once`). That test is worth writing;
		// it is not a comment's worth of work, so it is named here rather than faked.
		up := newSelfUpdater(cfg, rawEnv{lookup: env}, logf)

		// 方案A (T-c93d): wire the SSE transport's connect hook to the updater's Kick
		// so every successful (re)connect triggers an immediate self-update check —
		// a server redeploy drops the stream, the warden reconnects within ~1s, and
		// the fresh binary propagates in seconds instead of up to the 15m poll. The
		// 15m timer stays as the backstop. MUST be set BEFORE transport.run starts
		// (connectOnce reads onConnect from its own goroutine — no post-start race).
		transport.onConnect = up.Kick

		// T-5f01: the `update` warden-command verb (owner's one-click upgrade)
		// dispatches through the SAME Kick — an owner click and a reconnect are
		// just two producers of the one coalesced self-update wake.
		transport.deps.Update = up.Kick

		wg.Add(1)
		go func() { defer wg.Done(); transport.run(ctx) }()
		logf("[ocwarden] command reader: enabled (SSE %s%s)", cfg.Base, eventsPath)

		wg.Add(1)
		go func() { defer wg.Done(); up.run(ctx) }()
		logf("[ocwarden] self-update: enabled (poll %s; %s + %s; reconnect-kick on)", selfUpdateInterval, wardenBinaryPath, agentBinaryPath)
	}

	runtimeProbe := func() map[string]any {
		return collectRuntimeCapabilities(env, runner, claudeProbe.collect())
	}
	rc := run(ctx, cfg, collect, machine, post, fingerprints.collect, claudeProbe.collect,
		wardenShapeOf, cutoverEffectOf, sleepUntil, iters, out, runtimeProbe)

	// Graceful shutdown: the root ctx is now cancelled — either a signal fired, or
	// run() returned on its own (--once, or a mis-wire). Stop relaying signals, then
	// wait (bounded by shutdownGrace) for the background loops to unwind. On --once wg
	// is empty so this returns immediately; the process then exits with no leaked
	// goroutine and nothing hard-killed mid-flight.
	stop()
	waitGraceful(&wg, shutdownGrace)
	return rc
}

func main() {
	os.Exit(realMain(os.Args[1:], os.Getenv, os.Stdout))
}
