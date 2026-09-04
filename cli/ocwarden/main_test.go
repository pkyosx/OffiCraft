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

// realVMStat is verbatim `vm_stat` from a 64 GiB Apple-silicon box (page size
// 16384), captured in the same second that the retired top-based reading called
// that box 98.9% full. Its counters are what the ram_pct expectations in this
// file are computed from:
//
//	App Memory = (1770105 - 37518) pages = 28.39 GB
//	Wired      =            283654 pages =  4.65 GB
//	Compressed =            652133 pages = 10.68 GB
//	Memory Used                          = 43.72 GB / 68.72 GB = 63.6%
//
// Trimmed to the counters a reader needs plus a few neighbours, because the
// parser has to pick its three out of a list it does not control.
const realVMStat = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                   230372.
Pages active:                                1482390.
Pages inactive:                              1467600.
Pages speculative:                             17506.
Pages throttled:                                   0.
Pages wired down:                             283654.
Pages purgeable:                               37518.
"Translation faults":                     4174779662.
File-backed pages:                           1197391.
Anonymous pages:                             1770105.
Pages stored in compressor:                  1415705.
Pages occupied by compressor:                 652133.
Swapins:                                           0.
Swapouts:                                          0.
`

// realMemTotal is `sysctl -n hw.memsize` from that same box: 64 GiB exactly.
const realMemTotal = "68719476736\n"

// sample probe fixtures (trimmed real macOS output).
//
// The `top` fixture deliberately keeps its PhysMem line even though nothing
// parses it any more: by the retired formula that line reads 75% (12G / (12G +
// 4G)), while the vm_stat/hw.memsize fixtures read 63.6%. The two answers differ,
// so a reading that ever drifts back to `top` shows up as a wrong number here
// rather than as a silently plausible one.
var fakeProbes = map[string]string{
	"pmset -g batt":             "Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t87%; charged; 0:00 remaining present: true",
	"top -l1 -n0":               "CPU usage: 12.50% user, 7.50% sys, 80.00% idle\nPhysMem: 12G used (2G wired), 4G unused.",
	"scutil --get ComputerName": "Seth's MacBook Pro\n",
	"vm_stat":                   realVMStat,
	"sysctl -n hw.memsize":      realMemTotal,
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
	res := runOnce(cfg, collect, machine, post, binaries, claude, nil, nil)

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
	// The reporting identity is the verified JWT sub, never a body key. The frozen
	// ingest schema does not declare agent_id and the server refuses unknown fields,
	// so sending it 422s the ENTIRE heartbeat — hardware, binaries, claude probe and
	// runtime capabilities together.
	if _, present := gotBody["agent_id"]; present {
		t.Errorf("agent_id must not be on the wire; body = %v", gotBody)
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
	// 43.72 GB used of 68.72 GB installed — Activity Monitor's "Memory Used" over
	// hw.memsize, NOT the 75% the fixture's PhysMem line would have produced.
	if hw["ram_pct"] != 63.6 {
		t.Errorf("ram_pct = %v, want 63.6", hw["ram_pct"])
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
	payload, err := buildTelemetryPayload("agent-1", "m", map[string]any{"cpu_pct": 1.0}, nil, probe, "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got, ok := payload["claude"].(map[string]any)
	if !ok || got["version"] != "2.1.211" || got["sub_readable"] != false {
		t.Fatalf("claude = %v, want the probe map", payload["claude"])
	}

	payload, err = buildTelemetryPayload("agent-1", "m", map[string]any{"cpu_pct": 1.0}, nil, map[string]any{}, "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, present := payload["claude"]; present {
		t.Fatalf("empty probe must omit the claude field, got %v", payload["claude"])
	}
	payload, err = buildTelemetryPayload("agent-1", "m", map[string]any{"cpu_pct": 1.0}, nil, nil, "", "")
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
		func() map[string]any { return map[string]any{"cred_file": false, "sub_readable": false} }, nil, nil)
	if !res.Posted {
		t.Fatalf("claude-only cycle must post, got %+v", res)
	}
	cl, _ := gotBody["claude"].(map[string]any)
	if cl["cred_file"] != false {
		t.Fatalf("claude fold = %v, want the probe", gotBody["claude"])
	}
}

// TestRunOnce_HeartbeatCarriesTheWardenShape drives the REAL collector
// (newShapeReporter -> detectShape) over the faked cutover seam and reads the
// verdict off the payload the poster would put on the wire — the whole point of
// T-ff5d being that the fleet, not just one machine's startup log, can tell a
// converted machine from an unconverted one.
//
// The expected values are LITERALS, not the shape consts: the consts are the
// producer's own vocabulary, and a test that compares the producer to itself
// would stay green if someone renamed the wire value the server validates
// against. Same for the "warden_shape" key.
func TestRunOnce_HeartbeatCarriesTheWardenShape(t *testing.T) {
	p := testPaths()
	for _, tc := range []struct {
		name      string
		parentExe string
		want      string
	}{
		{"launchd runs the anchor", p.anchorPath, "anchor"},
		{"launchd runs ocwarden directly", "/sbin/launchd", "legacy"},
		{"parent is neither", "/bin/zsh", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeCutover()
			f.files["__ppid_exe__"] = tc.parentExe
			defer swapCutoverOps(t, f)()

			var gotBody map[string]any
			res := runOnce(Config{Base: "x", Token: "t", ID: "i"},
				func() map[string]any { return map[string]any{"cpu_pct": 1.0} },
				func() string { return "m" },
				func(_ string, payload map[string]any) (int, map[string]any) {
					gotBody = payload
					return 200, nil
				}, nil, nil, newShapeReporter(p.anchorPath, 4242), nil)
			if !res.Posted {
				t.Fatalf("runOnce = %+v, want posted", res)
			}
			if got := gotBody["warden_shape"]; got != tc.want {
				t.Fatalf("warden_shape = %v, want %q; body = %v", got, tc.want, gotBody)
			}
		})
	}

	// A warden that reports no shape at all must OMIT the key. Absent and
	// "unknown" are different claims on this wire ("this build cannot report a
	// shape" vs "this build ran and could not tell"), and the server is
	// forbidden from inferring one from the other — so the producer must never
	// collapse them either.
	var gotBody map[string]any
	runOnce(Config{Base: "x", Token: "t", ID: "i"},
		func() map[string]any { return map[string]any{"cpu_pct": 1.0} },
		func() string { return "m" },
		func(_ string, payload map[string]any) (int, map[string]any) {
			gotBody = payload
			return 200, nil
		}, nil, nil, nil, nil)
	if _, present := gotBody["warden_shape"]; present {
		t.Fatalf("no shape collector must omit the key, got %v", gotBody["warden_shape"])
	}
}

// TestNewShapeReporter_UnresolvedAnchorReportsUnknownNeverLegacy is the
// dangerous-default guard. detectShape decides `legacy` from "the parent is
// launchd AND it is not the anchor", so an empty anchorPath — the paths could
// not be resolved — would make every launchd-parented warden, INCLUDING a
// correctly converted one, report `legacy` and invite a second migration.
func TestNewShapeReporter_UnresolvedAnchorReportsUnknownNeverLegacy(t *testing.T) {
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	defer swapCutoverOps(t, f)()

	if got := newShapeReporter("", 1)(); got != "unknown" {
		t.Fatalf("shape with no anchor path = %q, want %q", got, "unknown")
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

func TestParseCPUPct(t *testing.T) {
	if v, ok := parseCPUPct("CPU usage: 5.00% user, 5.00% sys, 90.00% idle"); !ok || v != 10.0 {
		t.Errorf("cpu = %v ok=%v, want 10", v, ok)
	}
	if _, ok := parseCPUPct("garbage"); ok {
		t.Errorf("cpu should fail on garbage")
	}
}

// TestParseRAMPct_MatchesActivityMonitorOnRealOutput is the arithmetic this
// change exists for, pinned against real captured output rather than a
// hand-rounded expectation.
func TestParseRAMPct_MatchesActivityMonitorOnRealOutput(t *testing.T) {
	got, ok := parseRAMPct(realVMStat, realMemTotal)
	if !ok {
		t.Fatalf("real macOS output must parse")
	}
	if got != 63.6 {
		t.Errorf("ram_pct = %v, want 63.6 — (App Memory 28.39 + Wired 4.65 + "+
			"Compressed 10.68) GB over hw.memsize 68.72 GB", got)
	}
}

// TestParseRAMPct_ExcludesReclaimableCache is the REGRESSION this change is
// about. The box in realVMStat parks 1197391 file-backed pages — 19.62 GB — in
// reclaimable cache, and the retired reading counted every one of them as
// consumption (which is how a green machine got reported at 98.9% and used to
// raise a resource alarm).
//
// Stated as a threshold-crossing rather than as a constant, so it says something
// the exact-value test next door does not: on THIS sample, folding the cache back
// in pushes the reading into alarm territory (>= 90%) while the honest reading
// stays far below it. The second assertion is the test's own precondition — if a
// future fixture had a small cache the first assertion would pass for free, and a
// test that cannot fail is indistinguishable from one that is satisfied.
func TestParseRAMPct_ExcludesReclaimableCache(t *testing.T) {
	got, ok := parseRAMPct(realVMStat, realMemTotal)
	if !ok {
		t.Fatalf("real macOS output must parse")
	}
	pageSize, counts, ok := parseVMStat(realVMStat)
	if !ok {
		t.Fatalf("real macOS output must parse")
	}
	total, _ := parseMemTotalBytes(realMemTotal)
	cacheShare := counts["File-backed pages"] * pageSize / total * 100

	const alarm = 90.0
	if got >= alarm {
		t.Errorf("ram_pct = %v on a sample carrying %.1f points of reclaimable "+
			"file cache: cache is still being counted as consumption", got, cacheShare)
	}
	if got+cacheShare < alarm {
		t.Fatalf("precondition: this fixture's cache is only worth %.1f points, so "+
			"even counting it as used would read %.1f%% and stay under the %v%% this "+
			"test claims to discriminate against — the assertion above would pass "+
			"for free. Use a cache-heavy sample.", cacheShare, got+cacheShare, alarm)
	}
}

// TestParseRAMPct_NeverDeflatesWhenPurgeableExceedsAnonymous is the negative
// control for the App Memory floor. Without it, `anonymous - purgeable` going
// negative would SUBTRACT from wired+compressed and report a machine as emptier
// than it is — and deleting the floor is invisible to every other test here,
// because no real sample can drive it.
func TestParseRAMPct_NeverDeflatesWhenPurgeableExceedsAnonymous(t *testing.T) {
	// Anonymous well below purgeable; wired + compressor are untouched, so the
	// honest floor answer is exactly their share of the box.
	inverted := strings.ReplaceAll(realVMStat,
		"Anonymous pages:                             1770105.",
		"Anonymous pages:                                  10.")
	if inverted == realVMStat {
		t.Fatalf("precondition: the anonymous line was not actually replaced")
	}
	got, ok := parseRAMPct(inverted, realMemTotal)
	if !ok {
		t.Fatalf("an invertible sample must still parse")
	}
	pageSize, counts, _ := parseVMStat(inverted)
	total, _ := parseMemTotalBytes(realMemTotal)
	floor := (counts["Pages wired down"] + counts["Pages occupied by compressor"]) *
		pageSize / total * 100
	if got < round1(floor) {
		t.Errorf("ram_pct = %v, want >= %.1f (wired + compressed alone): a negative "+
			"App Memory must not be allowed to cancel out memory that IS pinned",
			got, floor)
	}
}

// TestParseRAMPct_PurgeableIsAnOptionalCorrection: the three constituents are
// required (without one the number is a different quantity), but purgeable only
// trims volatile allocations and is worth well under a point — a vm_stat that
// stopped reporting it must degrade, not go blank.
func TestParseRAMPct_PurgeableIsAnOptionalCorrection(t *testing.T) {
	withPurgeable, ok := parseRAMPct(realVMStat, realMemTotal)
	if !ok {
		t.Fatalf("precondition: the real sample must parse")
	}
	stripped := strings.ReplaceAll(realVMStat, "Pages purgeable:                               37518.\n", "")
	if strings.Contains(stripped, "purgeable") {
		t.Fatalf("precondition: the purgeable line was not actually removed")
	}
	without, ok := parseRAMPct(stripped, realMemTotal)
	if !ok {
		t.Fatalf("a missing purgeable line must still produce a reading, not a blank")
	}
	if without <= withPurgeable || without-withPurgeable > 1.0 {
		t.Errorf("without purgeable = %v, with = %v: dropping the correction should "+
			"overstate by a fraction of a point, nothing more", without, withPurgeable)
	}
}

// TestParseRAMPct_OmitsRatherThanGuesses. Every input this reading cannot do
// without, one at a time. Reporting a plausible-but-wrong percent is the failure
// this whole change is undoing, so each case must come back ok=false and let
// collectHardware omit the key.
func TestParseRAMPct_OmitsRatherThanGuesses(t *testing.T) {
	for name, tc := range map[string]struct{ vmStat, memTotal string }{
		"no page size in the header": {
			strings.ReplaceAll(realVMStat, "(page size of 16384 bytes)", "(page size unknown)"),
			realMemTotal},
		"zero page size": {
			strings.ReplaceAll(realVMStat, "page size of 16384 bytes", "page size of 0 bytes"),
			realMemTotal},
		"no anonymous pages": {
			strings.ReplaceAll(realVMStat, "Anonymous pages", "Anonymouse pages"),
			realMemTotal},
		"no wired pages": {
			strings.ReplaceAll(realVMStat, "Pages wired down", "Pages wired up"),
			realMemTotal},
		"no compressor pages": {
			strings.ReplaceAll(realVMStat, "Pages occupied by compressor", "Pages held by compressor"),
			realMemTotal},
		"vm_stat did not run":    {"", realMemTotal},
		"hw.memsize did not run": {realVMStat, ""},
		"hw.memsize is not a number": {realVMStat,
			"hw.memsize: 68719476736\n"},
		"hw.memsize is zero": {realVMStat, "0\n"},
		// The float spellings ParseFloat would have accepted. NaN is the dangerous
		// one: it passes any `<= 0` check, survives the clamp, and then fails
		// json.Marshal on the WHOLE heartbeat — reported as status 0, which looks
		// exactly like the server being down.
		"hw.memsize is NaN":        {realVMStat, "NaN\n"},
		"hw.memsize is +Inf":       {realVMStat, "+Inf\n"},
		"hw.memsize is a hexfloat": {realVMStat, "0x1p40\n"},
		"hw.memsize is scientific": {realVMStat, "6.87e10\n"},
		"hw.memsize is negative":   {realVMStat, "-68719476736\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if v, ok := parseRAMPct(tc.vmStat, tc.memTotal); ok {
				t.Errorf("got %v ok=true; a reading this incomplete must be omitted, "+
					"because a wrong gauge is what this change is removing", v)
			}
		})
	}
}

// TestParseRAMPct_ClampsToARealPercent: teleNum on the server drops negatives and
// the cockpit renders whatever it is handed, so an impossible ratio (a machine
// reporting more consumed than installed, e.g. mid-resize or a truncated probe)
// must not leave here as 140%.
func TestParseRAMPct_ClampsToARealPercent(t *testing.T) {
	if v, ok := parseRAMPct(realVMStat, "4294967296\n"); !ok || v != 100.0 {
		t.Errorf("ram_pct = %v ok=%v, want a clamped 100", v, ok)
	}
}

// TestParseVMStat_ReadsThePageSizeItIsGiven. 4096 on Intel, 16384 on Apple
// silicon: assuming either makes every count on the other architecture wrong by
// 4x, which is a big enough error to look like a real memory problem.
func TestParseVMStat_ReadsThePageSizeItIsGiven(t *testing.T) {
	intel := strings.ReplaceAll(realVMStat, "page size of 16384 bytes", "page size of 4096 bytes")
	pageSize, counts, ok := parseVMStat(intel)
	if !ok || pageSize != 4096 {
		t.Fatalf("pageSize = %v ok=%v, want 4096", pageSize, ok)
	}
	if counts["Pages wired down"] != 283654 {
		t.Errorf("wired = %v, want 283654", counts["Pages wired down"])
	}
	// A quoted label ("Translation faults") must not derail the line scanner.
	if counts["Translation faults"] != 4174779662 {
		t.Errorf("quoted label parsed as %v", counts["Translation faults"])
	}
	// A QUARTER, not merely "smaller": a mutant that halves the reading, or one
	// that fails outright and returns (0, false), would satisfy `quarter < full`.
	quarter, quarterOK := parseRAMPct(intel, realMemTotal)
	full, fullOK := parseRAMPct(realVMStat, realMemTotal)
	if !quarterOK || !fullOK {
		t.Fatalf("both page sizes must produce a reading; got %v/%v %v/%v",
			quarter, quarterOK, full, fullOK)
	}
	if ratio := full / quarter; ratio < 3.9 || ratio > 4.1 {
		t.Errorf("16384/4096 reading ratio = %.3f (%v vs %v), want ~4: the page size "+
			"must scale every count, not merely change the answer", ratio, full, quarter)
	}
}

// TestParseVMStat_ALabelNeverAdoptsTheNextLinesNumber is the negative control for
// the `[ \t]` separators in vmCounterRe. Go's `\s` matches a newline, so the
// obvious `\s+` spelling lets a label whose own line carries no number reach
// across and adopt the following line's digits — a counter that silently becomes
// someone else's value, which is worse than one that is simply absent.
func TestParseVMStat_ALabelNeverAdoptsTheNextLinesNumber(t *testing.T) {
	split := strings.ReplaceAll(realVMStat,
		"Pages wired down:                             283654.",
		"Pages wired down:\n283654.")
	if split == realVMStat {
		t.Fatalf("precondition: the wired line was not actually split")
	}
	_, counts, ok := parseVMStat(split)
	if !ok {
		t.Fatalf("the header is intact, so the sample must still parse")
	}
	if v, present := counts["Pages wired down"]; present {
		t.Errorf("Pages wired down = %v: its own line carries no number, so it must "+
			"be ABSENT (letting parseRAMPct omit the reading) rather than silently "+
			"adopting the next line's digits", v)
	}
	// And the whole reading must omit itself rather than compute without wired.
	if v, ok := parseRAMPct(split, realMemTotal); ok {
		t.Errorf("ram_pct = %v: a missing constituent must omit, not approximate", v)
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

// TestCollectHardware_RAMNeedsBothMemoryProbes: ram_pct is now assembled from two
// independent commands, so each has to be able to fail ALONE without either
// blanking its siblings or — the case that matters — resurrecting a reading from
// whichever half did answer. `top` is still in the fixture set throughout, so a
// fallback to its PhysMem line would show up as a present ram_pct.
func TestCollectHardware_RAMNeedsBothMemoryProbes(t *testing.T) {
	for name, missing := range map[string]string{
		"vm_stat is unavailable":    "vm_stat",
		"hw.memsize is unavailable": "sysctl -n hw.memsize",
	} {
		t.Run(name, func(t *testing.T) {
			probes := map[string]string{}
			for key, value := range fakeProbes {
				if key != missing {
					probes[key] = value
				}
			}
			hw := collectHardware(fakeRunner{out: probes}, "darwin")
			if _, present := hw["ram_pct"]; present {
				t.Errorf("ram_pct = %v with %s missing; half an answer must be omitted, "+
					"not filled in from top", hw["ram_pct"], missing)
			}
			// The siblings are independent probes and must be untouched.
			if hw["cpu_pct"] != 20.0 || hw["battery_pct"] != 87 || hw["ac_power"] != true {
				t.Errorf("a failed memory probe disturbed its siblings: %v", hw)
			}
		})
	}
}

func TestRunOnce_SkipsWhenNoToken(t *testing.T) {
	res := runOnce(Config{Base: "x", Token: "", ID: ""},
		func() map[string]any { return map[string]any{"cpu_pct": 1.0} },
		func() string { return "m" },
		func(string, map[string]any) (int, map[string]any) {
			t.Fatal("post must not be called without token")
			return 0, nil
		}, nil, nil, nil, nil)
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
		}, nil, nil, nil, nil)
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
		nil, nil, nil, nil,
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
			nil, nil, nil, nil,
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
	// ⚠️ The SCHEME here moved http→https (T-78): "x" is not loopback, and the
	// stored scheme is no longer believed. What this test pins did NOT move —
	// the id still falls back to the JWT sub, and the trailing slash is still
	// stripped.
	if cfg.Base != "https://x" {
		t.Errorf("base = %q, want https://x (host decides the scheme; trailing slash stripped)", cfg.Base)
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
