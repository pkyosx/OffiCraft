package main

import (
	"bytes"
	"context"

	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// jwtWardenOne is a three-part token whose payload decodes to {"sub":"warden-1"}.
const jwtWardenOne = "header.eyJzdWIiOiJ3YXJkZW4tMSJ9.signature"

const jwtNoSub = "header.eyJub3N1YiI6MX0.sig"

// roundTripFunc is the http seam: it answers a request without a listening server.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoadConfig(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{"a loopback base keeps its scheme and loses its trailing slash", map[string]string{
			"OC_BASE": "http://127.0.0.1:7755/", "OC_TOKEN": jwtWardenOne,
		}, Config{Base: "http://127.0.0.1:7755", Token: jwtWardenOne, ID: "warden-1"}},
		{"an explicit OC_ID wins over the token subject", map[string]string{
			"OC_BASE": "http://127.0.0.1:7755", "OC_TOKEN": jwtWardenOne, "OC_ID": "machine-7",
		}, Config{Base: "http://127.0.0.1:7755", Token: jwtWardenOne, ID: "machine-7"}},
		{"a remote base is re-schemed and stripped to its authority", map[string]string{
			"OC_BASE": "http://oc.example.com/api/?x=1", "OC_TOKEN": jwtWardenOne,
		}, Config{Base: "https://oc.example.com", Token: jwtWardenOne, ID: "warden-1"}},
		{"an unset OC_BASE is left empty, never guessed", map[string]string{}, Config{}},
		{"a token with no sub leaves the id empty", map[string]string{
			"OC_TOKEN": jwtNoSub,
		}, Config{Token: jwtNoSub}},
	}
	for _, c := range cases {
		got := loadConfig(func(k string) string { return c.env[k] })
		if got != c.want {
			t.Errorf("%s: config = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestReadTokfile(t *testing.T) {
	reads := []string{}
	readFile := func(path string) ([]byte, error) {
		reads = append(reads, path)
		switch path {
		case "/Users/eva/.officraft/warden/exec-warden.tok":
			return []byte("  jwt-from-file\n"), nil
		case "/custom/warden.tok":
			return []byte("jwt-custom"), nil
		}
		return nil, os.ErrNotExist
	}

	got := readTokfile(func(k string) string { return map[string]string{"HOME": "/Users/eva"}[k] }, readFile)
	if got != "jwt-from-file" {
		t.Errorf("token = %q, want %q (whitespace trimmed)", got, "jwt-from-file")
	}
	if want := []string{"/Users/eva/.officraft/warden/exec-warden.tok"}; !reflect.DeepEqual(reads, want) {
		t.Errorf("reads = %v, want %v", reads, want)
	}

	env := map[string]string{"HOME": "/Users/eva", "OC_WARDEN_TOKFILE": "/custom/warden.tok"}
	if got := readTokfile(func(k string) string { return env[k] }, readFile); got != "jwt-custom" {
		t.Errorf("token = %q, want %q", got, "jwt-custom")
	}

	if got := readTokfile(func(k string) string { return map[string]string{"HOME": "/nobody"}[k] }, readFile); got != "" {
		t.Errorf("a missing token file = %q, want \"\"", got)
	}
	if got := readTokfile(func(string) string { return "" }, readFile); got != "" {
		t.Errorf("no derivable path = %q, want \"\"", got)
	}
}

func TestTokfilePath(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"the launcher default", map[string]string{"HOME": "/Users/eva"},
			"/Users/eva/.officraft/warden/exec-warden.tok"},
		{"OC_WARDEN_TOKFILE wins", map[string]string{
			"HOME": "/Users/eva", "OC_WARDEN_TOKFILE": "/custom/warden.tok", "OC_NAMESPACE": "lab",
		}, "/custom/warden.tok"},
		{"a namespaced instance has its own file", map[string]string{
			"HOME": "/Users/eva", "OC_NAMESPACE": "lab",
		}, "/Users/eva/.officraft-lab/warden/exec-warden.tok"},
		{"no HOME derives nothing", map[string]string{}, ""},
		{"a malformed namespace derives nothing", map[string]string{
			"HOME": "/Users/eva", "OC_NAMESPACE": "LAB",
		}, ""},
	}
	for _, c := range cases {
		if got := tokfilePath(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%s: tokfilePath = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTokfileEnv(t *testing.T) {
	base := map[string]string{"HOME": "/Users/eva", "OC_BASE": "http://127.0.0.1:7755", "OC_CLAUDE_BIN": "/bin/claude"}
	readFile := func(path string) ([]byte, error) {
		if path == "/Users/eva/.officraft/warden/exec-warden.tok" {
			return []byte("jwt-from-file\n"), nil
		}
		return nil, os.ErrNotExist
	}

	folded := tokfileEnv(func(k string) string { return base[k] }, readFile)
	if got := folded("OC_TOKEN"); got != "jwt-from-file" {
		t.Errorf("OC_TOKEN = %q, want the token file's contents", got)
	}
	for _, k := range []string{"HOME", "OC_BASE", "OC_CLAUDE_BIN", "OC_ABSENT"} {
		if got, want := folded(k), base[k]; got != want {
			t.Errorf("%s = %q, want %q (passed straight through)", k, got, want)
		}
	}

	explicit := map[string]string{"HOME": "/Users/eva", "OC_TOKEN": "jwt-explicit"}
	folded = tokfileEnv(func(k string) string { return explicit[k] }, readFile)
	if got := folded("OC_TOKEN"); got != "jwt-explicit" {
		t.Errorf("OC_TOKEN = %q, want the explicitly-set token", got)
	}

	empty := tokfileEnv(func(string) string { return "" }, readFile)
	if got := empty("OC_TOKEN"); got != "" {
		t.Errorf("OC_TOKEN = %q, want \"\" when no file can be derived", got)
	}
}

func TestJwtSub(t *testing.T) {
	cases := []struct{ token, want string }{
		{jwtWardenOne, "warden-1"},
		{jwtNoSub, ""},
		{"", ""},
		{"onlyone", ""},
		{"a.b", ""},
		{"a.b.c.d", ""},
		{"header.!!notbase64!!.sig", ""},
		{"header.aGVsbG8.sig", ""},
	}
	for _, c := range cases {
		if got := jwtSub(c.token); got != c.want {
			t.Errorf("jwtSub(%q) = %q, want %q", c.token, got, c.want)
		}
	}
}

func TestParseBattery(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		pct   int
		pctOK bool
		ac    bool
		acOK  bool
	}{
		{"charged on AC", "Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t87%; charged; 0:00 remaining present: true",
			87, true, true, true},
		{"discharging", "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t42%; discharging; 3:11 remaining present: true",
			42, true, false, true},
		{"AC attached wording", "AC attached; 100%", 100, true, true, true},
		{"desktop with no battery", "Now drawing from 'AC Power'", 0, false, true, true},
		{"unreadable output", "", 0, false, false, false},
		{"an out-of-range percentage is dropped", "Now drawing from 'AC Power'\n 999%", 0, false, true, true},
	}
	for _, c := range cases {
		pct, pctOK, ac, acOK := parseBattery(c.text)
		if pct != c.pct || pctOK != c.pctOK || ac != c.ac || acOK != c.acOK {
			t.Errorf("%s: got (%d,%v,%v,%v), want (%d,%v,%v,%v)",
				c.name, pct, pctOK, ac, acOK, c.pct, c.pctOK, c.ac, c.acOK)
		}
	}
}

func TestParseCPUPct(t *testing.T) {
	cases := []struct {
		name string
		text string
		want float64
		ok   bool
	}{
		{"a real top line", "CPU usage: 12.50% user, 7.50% sys, 80.00% idle\nPhysMem: 12G used", 20, true},
		{"fully idle", "CPU usage: 0.00% user, 0.00% sys, 100.00% idle", 0, true},
		{"fully busy", "CPU usage: 60.00% user, 40.00% sys, 0.00% idle", 100, true},
		{"rounded to one decimal", "CPU usage: 1.00% user, 0.00% sys, 87.34% idle", 12.7, true},
		{"an over-100 idle is clamped", "CPU usage: 0% user, 0% sys, 140.00% idle", 0, true},
		{"no idle figure", "PhysMem: 12G used (2G wired), 4G unused.", 0, false},
		{"empty", "", 0, false},
	}
	for _, c := range cases {
		got, ok := parseCPUPct(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestParseVMStat(t *testing.T) {
	size, counts, ok := parseVMStat(realVMStat)
	if !ok || size != 16384 {
		t.Fatalf("got (page size %v, ok %v), want (16384, true)", size, ok)
	}
	want := map[string]float64{
		"Pages free": 230372, "Pages active": 1482390, "Pages inactive": 1467600,
		"Pages speculative": 17506, "Pages throttled": 0, "Pages wired down": 283654,
		"Pages purgeable": 37518, "Translation faults": 4174779662,
		"File-backed pages": 1197391, "Anonymous pages": 1770105,
		"Pages stored in compressor": 1415705, "Pages occupied by compressor": 652133,
		"Swapins": 0, "Swapouts": 0,
	}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("counts = %v, want %v", counts, want)
	}

	if _, _, ok := parseVMStat("Pages free: 230372.\n"); ok {
		t.Error("a dump with no page size must be unusable")
	}
	if _, _, ok := parseVMStat("Mach Virtual Memory Statistics: (page size of 0 bytes)\n"); ok {
		t.Error("a zero page size must be unusable")
	}

	_, counts, ok = parseVMStat("Mach Virtual Memory Statistics: (page size of 4096 bytes)\n" +
		"Pages free:\n230372.\nPages wired down: 283654.\n")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got := map[string]float64{"Pages wired down": 283654}; !reflect.DeepEqual(counts, got) {
		t.Errorf("counts = %v, want %v (a label never adopts the next line's number)", counts, got)
	}
}

func TestParseMemTotalBytes(t *testing.T) {
	cases := []struct {
		text string
		want float64
		ok   bool
	}{
		{realMemTotal, 68719476736, true},
		{"17179869184", 17179869184, true},
		{"0", 0, false},
		{"", 0, false},
		{"-1", 0, false},
		{"NaN", 0, false},
		{"+Inf", 0, false},
		{"0x10", 0, false},
		{"6.8e10", 0, false},
	}
	for _, c := range cases {
		got, ok := parseMemTotalBytes(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("parseMemTotalBytes(%q) = (%v, %v), want (%v, %v)", c.text, got, ok, c.want, c.ok)
		}
	}
}

func TestParseRAMPct(t *testing.T) {
	got, ok := parseRAMPct(realVMStat, realMemTotal)
	if !ok || got != 63.6 {
		t.Errorf("got (%v, %v), want (63.6, true)", got, ok)
	}

	noPurgeable := strings.ReplaceAll(realVMStat, "Pages purgeable:                               37518.\n", "")
	got, ok = parseRAMPct(noPurgeable, realMemTotal)
	if !ok || got != 64.5 {
		t.Errorf("without the purgeable correction got (%v, %v), want (64.5, true)", got, ok)
	}

	for _, missing := range []string{"Anonymous pages", "Pages wired down", "Pages occupied by compressor"} {
		var kept []string
		for _, line := range strings.Split(realVMStat, "\n") {
			if !strings.HasPrefix(line, missing+":") {
				kept = append(kept, line)
			}
		}
		if _, ok := parseRAMPct(strings.Join(kept, "\n"), realMemTotal); ok {
			t.Errorf("a dump without %q must omit itself", missing)
		}
	}

	if _, ok := parseRAMPct(realVMStat, "0"); ok {
		t.Error("an unusable denominator must omit the reading")
	}
	if _, ok := parseRAMPct("nothing here", realMemTotal); ok {
		t.Error("an unusable vm_stat must omit the reading")
	}

	tiny := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n" +
		"Pages wired down: 1000000.\nAnonymous pages: 1000000.\n" +
		"Pages occupied by compressor: 1000000.\nPages purgeable: 0.\n"
	if got, ok := parseRAMPct(tiny, "1073741824"); !ok || got != 100 {
		t.Errorf("an impossible ratio got (%v, %v), want (100, true)", got, ok)
	}
}

func TestCollectHardware(t *testing.T) {
	got := collectHardware(fakeRunner{out: fakeProbes}, "darwin")
	want := map[string]any{"battery_pct": 87, "ac_power": true, "cpu_pct": 20.0, "ram_pct": 63.6}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hardware = %v, want %v", got, want)
	}

	if got := collectHardware(fakeRunner{out: fakeProbes}, "linux"); !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("a non-darwin host = %v, want an empty map", got)
	}

	if got := collectHardware(fakeRunner{}, "darwin"); !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("a host where every probe fails = %v, want an empty map", got)
	}

	partial := map[string]string{
		"pmset -g batt": "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t42%; discharging",
		"vm_stat":       realVMStat,
	}
	got = collectHardware(fakeRunner{out: partial}, "darwin")
	if want := (map[string]any{"battery_pct": 42, "ac_power": false}); !reflect.DeepEqual(got, want) {
		t.Errorf("hardware = %v, want %v (ram needs BOTH memory probes)", got, want)
	}
}

func TestReadMachineName(t *testing.T) {
	if got := readMachineName(fakeRunner{out: fakeProbes}); got != "Seth's MacBook Pro" {
		t.Errorf("machine = %q, want %q", got, "Seth's MacBook Pro")
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname on this host: %v", err)
	}
	if got := readMachineName(fakeRunner{}); got != hostname {
		t.Errorf("a failed scutil = %q, want the OS hostname %q", got, hostname)
	}
	blank := fakeRunner{out: map[string]string{"scutil --get ComputerName": "  \n"}}
	if got := readMachineName(blank); got != hostname {
		t.Errorf("a blank ComputerName = %q, want the OS hostname %q", got, hostname)
	}
}

func TestBuildTelemetryPayload(t *testing.T) {
	got, err := buildTelemetryPayload("warden-1", "Seth's MacBook Pro",
		map[string]any{"cpu_pct": 20.0},
		map[string]string{"ocwarden": "sha-w"},
		map[string]any{"present": true},
		"anchor", "in_effect",
		map[string]any{"claude": true})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	want := map[string]any{
		"machine":        "Seth's MacBook Pro",
		"hardware":       map[string]any{"cpu_pct": 20.0},
		"binaries":       map[string]string{"ocwarden": "sha-w"},
		"claude":         map[string]any{"present": true},
		"warden_shape":   "anchor",
		"cutover_effect": "in_effect",
		"runtimes":       map[string]any{"claude": true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("payload = %v, want %v", got, want)
	}

	got, err = buildTelemetryPayload("warden-1", "", nil, nil, nil, "", "", nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("payload = %v, want an empty body (the id is never a body key)", got)
	}

	for _, id := range []string{"", "   "} {
		got, err := buildTelemetryPayload(id, "Seth's MacBook Pro", map[string]any{"cpu_pct": 20.0},
			nil, nil, "", "")
		if got != nil {
			t.Errorf("payload = %v, want nil", got)
		}
		wantErr := "agent id is required (an unidentified warden has nothing to report)"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
	}
}

func TestHttpPoster(t *testing.T) {
	var got *http.Request
	var body []byte
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r
		body, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"stored":1}`)),
			Header:     http.Header{},
		}, nil
	})}

	post := httpPoster(client, "http://127.0.0.1:7755", "jwt-warden")
	status, reply := post("/api/monitoring/telemetry", map[string]any{"machine": "Seth's MacBook Pro"})
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if want := (map[string]any{"ok": true, "stored": 1.0}); !reflect.DeepEqual(reply, want) {
		t.Errorf("reply = %v, want %v", reply, want)
	}
	if got.Method != http.MethodPost || got.URL.String() != "http://127.0.0.1:7755/api/monitoring/telemetry" {
		t.Errorf("request = %s %s, want POST http://127.0.0.1:7755/api/monitoring/telemetry", got.Method, got.URL)
	}
	wantHeaders := map[string]string{
		"User-Agent":    "ocwarden/0.1",
		"Accept":        "application/json",
		"Content-Type":  "application/json",
		"Authorization": "Bearer jwt-warden",
	}
	for k, want := range wantHeaders {
		if v := got.Header.Get(k); v != want {
			t.Errorf("header %s = %q, want %q", k, v, want)
		}
	}
	if string(body) != `{"machine":"Seth's MacBook Pro"}` {
		t.Errorf("body = %s, want the marshalled payload", body)
	}

	tokenless := httpPoster(client, "http://127.0.0.1:7755", "")
	tokenless("/api/monitoring/telemetry", map[string]any{})
	if v := got.Header.Get("Authorization"); v != "" {
		t.Errorf("Authorization = %q, want no header at all", v)
	}

	failing := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}
	if status, reply := httpPoster(failing, "http://127.0.0.1:7755", "t")("/x", map[string]any{}); status != 0 || reply != nil {
		t.Errorf("a transport fault = (%d, %v), want (0, nil)", status, reply)
	}

	if status, reply := post("/x", map[string]any{"ch": make(chan int)}); status != 0 || reply != nil {
		t.Errorf("an unmarshallable payload = (%d, %v), want (0, nil)", status, reply)
	}

	if status, reply := httpPoster(client, "://bad", "t")("/x", map[string]any{}); status != 0 || reply != nil {
		t.Errorf("an unbuildable request = (%d, %v), want (0, nil)", status, reply)
	}

	nonJSON := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader("<html>bad gateway")), Header: http.Header{}}, nil
	})}
	if status, reply := httpPoster(nonJSON, "http://127.0.0.1:7755", "t")("/x", map[string]any{}); status != 502 || reply != nil {
		t.Errorf("a non-JSON body = (%d, %v), want (502, nil)", status, reply)
	}
}

func TestErrorMessageOf(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"the server envelope", map[string]any{"error": map[string]any{
			"code": "unprocessable", "message": "  agent_id: unknown field  ",
		}}, "agent_id: unknown field"},
		{"no envelope", map[string]any{"ok": true}, ""},
		{"a non-object envelope", map[string]any{"error": "boom"}, ""},
		{"a non-string message", map[string]any{"error": map[string]any{"message": 42}}, ""},
		{"a nil body", nil, ""},
	}
	for _, c := range cases {
		if got := errorMessageOf(c.body); got != c.want {
			t.Errorf("%s: errorMessageOf = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNextBackoff(t *testing.T) {
	cases := []struct{ cur, want time.Duration }{
		{0, 2 * time.Second},
		{-time.Second, 2 * time.Second},
		{time.Second, 2 * time.Second},
		{2 * time.Second, 4 * time.Second},
		{16 * time.Second, 32 * time.Second},
		{32 * time.Second, 60 * time.Second},
		{60 * time.Second, 60 * time.Second},
		{5 * time.Minute, 60 * time.Second},
	}
	for _, c := range cases {
		if got := nextBackoff(c.cur); got != c.want {
			t.Errorf("nextBackoff(%v) = %v, want %v", c.cur, got, c.want)
		}
	}
}

func TestRunOnce(t *testing.T) {
	cfg := Config{Base: "http://127.0.0.1:7755", Token: "jwt-warden", ID: "warden-1"}
	hardware := func() map[string]any { return map[string]any{"cpu_pct": 20.0} }
	machine := func() string { return "Seth's MacBook Pro" }

	t.Run("a full cycle posts the assembled body", func(t *testing.T) {
		var paths []string
		var payloads []map[string]any
		post := func(path string, payload map[string]any) (int, map[string]any) {
			paths = append(paths, path)
			payloads = append(payloads, payload)
			return 200, map[string]any{"ok": true}
		}
		got := runOnce(cfg, hardware, machine, post,
			func() map[string]string { return map[string]string{"ocwarden": "sha-w"} },
			func() map[string]any { return map[string]any{"present": true} },
			func() string { return "anchor" },
			func() string { return "in_effect" },
			func() map[string]any { return map[string]any{"claude": true} })
		if want := (ReportResult{Posted: true, Status: 200, Reason: "posted"}); got != want {
			t.Errorf("result = %+v, want %+v", got, want)
		}
		if want := []string{"/api/monitoring/telemetry"}; !reflect.DeepEqual(paths, want) {
			t.Errorf("paths = %v, want %v", paths, want)
		}
		want := []map[string]any{{
			"machine":        "Seth's MacBook Pro",
			"hardware":       map[string]any{"cpu_pct": 20.0},
			"binaries":       map[string]string{"ocwarden": "sha-w"},
			"claude":         map[string]any{"present": true},
			"warden_shape":   "anchor",
			"cutover_effect": "in_effect",
			"runtimes":       map[string]any{"claude": true},
		}}
		if !reflect.DeepEqual(payloads, want) {
			t.Errorf("payload = %v, want %v", payloads, want)
		}
	})

	t.Run("a mis-wired warden posts nothing", func(t *testing.T) {
		post := func(string, map[string]any) (int, map[string]any) {
			t.Error("a mis-wired warden must not POST")
			return 200, nil
		}
		for _, c := range []struct {
			name string
			cfg  Config
		}{
			{"no token", Config{ID: "warden-1"}},
			{"no id", Config{Token: "jwt-warden"}},
		} {
			got := runOnce(c.cfg, hardware, machine, post, nil, nil, nil, nil)
			if want := (ReportResult{Reason: "no OC_TOKEN/OC_ID"}); got != want {
				t.Errorf("%s: result = %+v, want %+v", c.name, got, want)
			}
		}
	})

	t.Run("a cycle that probed nothing skips the POST", func(t *testing.T) {
		post := func(string, map[string]any) (int, map[string]any) {
			t.Error("an empty body must never be POSTed")
			return 200, nil
		}
		got := runOnce(cfg, func() map[string]any { return nil }, machine, post,
			func() map[string]string { return nil }, func() map[string]any { return nil },
			func() string { return "" }, func() string { return "" })
		if want := (ReportResult{Reason: "no hardware probed (skip POST)"}); got != want {
			t.Errorf("result = %+v, want %+v", got, want)
		}
	})

	t.Run("a fingerprints-only cycle still posts", func(t *testing.T) {
		var payloads []map[string]any
		post := func(_ string, payload map[string]any) (int, map[string]any) {
			payloads = append(payloads, payload)
			return 200, nil
		}
		got := runOnce(cfg, func() map[string]any { return nil }, func() string { return "" }, post,
			func() map[string]string { return map[string]string{"ocagent": "sha-a"} }, nil, nil, nil)
		if want := (ReportResult{Posted: true, Status: 200, Reason: "posted"}); got != want {
			t.Errorf("result = %+v, want %+v", got, want)
		}
		want := []map[string]any{{"binaries": map[string]string{"ocagent": "sha-a"}}}
		if !reflect.DeepEqual(payloads, want) {
			t.Errorf("payload = %v, want %v", payloads, want)
		}
	})

	t.Run("a refusal carries the server's own explanation", func(t *testing.T) {
		post := func(string, map[string]any) (int, map[string]any) {
			return 422, map[string]any{"error": map[string]any{
				"code": "unprocessable", "message": "agent_id: unknown field",
			}}
		}
		got := runOnce(cfg, hardware, machine, post, nil, nil, nil, nil)
		want := ReportResult{Status: 422, Reason: "post status 422: agent_id: unknown field"}
		if got != want {
			t.Errorf("result = %+v, want %+v", got, want)
		}
	})

	t.Run("a transport fault reports the falsy status", func(t *testing.T) {
		post := func(string, map[string]any) (int, map[string]any) { return 0, nil }
		got := runOnce(cfg, hardware, machine, post, nil, nil, nil, nil)
		if want := (ReportResult{Status: 0, Reason: "post status 0"}); got != want {
			t.Errorf("result = %+v, want %+v", got, want)
		}
	})
}

func TestRun(t *testing.T) {
	cfg := Config{Base: "http://127.0.0.1:7755", Token: "jwt-warden", ID: "warden-1"}
	hardware := func() map[string]any { return map[string]any{"cpu_pct": 20.0} }
	machine := func() string { return "Seth's MacBook Pro" }

	t.Run("a mis-wired warden logs one line and exits clean", func(t *testing.T) {
		var out bytes.Buffer
		rc := run(context.Background(), Config{}, hardware, machine,
			func(string, map[string]any) (int, map[string]any) {
				t.Error("a mis-wired warden must not POST")
				return 200, nil
			}, nil, nil, nil, nil,
			func(context.Context, time.Duration) bool { t.Error("must not sleep"); return true },
			1, &out)
		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		want := "[ocwarden] run: no OC_TOKEN/OC_ID — nothing to report; exiting.\n"
		if out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("a single successful cycle waits the throttle and says nothing", func(t *testing.T) {
		var out bytes.Buffer
		var waits []time.Duration
		posts := 0
		rc := run(context.Background(), cfg, hardware, machine,
			func(string, map[string]any) (int, map[string]any) { posts++; return 200, nil },
			nil, nil, nil, nil,
			func(_ context.Context, d time.Duration) bool { waits = append(waits, d); return true },
			1, &out)
		if rc != 0 || posts != 1 {
			t.Errorf("rc = %d, posts = %d, want 0 and 1", rc, posts)
		}
		if want := []time.Duration{30 * time.Second}; !reflect.DeepEqual(waits, want) {
			t.Errorf("waits = %v, want %v", waits, want)
		}
		if out.String() != "" {
			t.Errorf("out = %q, want silence", out.String())
		}
	})

	t.Run("a refused heartbeat is logged and backs off, doubling each time", func(t *testing.T) {
		var out bytes.Buffer
		var waits []time.Duration
		rc := run(context.Background(), cfg, hardware, machine,
			func(string, map[string]any) (int, map[string]any) {
				return 422, map[string]any{"error": map[string]any{"message": "agent_id: unknown field"}}
			}, nil, nil, nil, nil,
			func(_ context.Context, d time.Duration) bool { waits = append(waits, d); return true },
			3, &out)
		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}; !reflect.DeepEqual(waits, want) {
			t.Errorf("waits = %v, want %v", waits, want)
		}
		line := "[ocwarden] telemetry: post status 422: agent_id: unknown field (report NOT stored)\n"
		if want := strings.Repeat(line, 3); out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
	})

	t.Run("a server that is simply down stays quiet on the throttle", func(t *testing.T) {
		var out bytes.Buffer
		var waits []time.Duration
		run(context.Background(), cfg, hardware, machine,
			func(string, map[string]any) (int, map[string]any) { return 0, nil },
			nil, nil, nil, nil,
			func(_ context.Context, d time.Duration) bool { waits = append(waits, d); return true },
			2, &out)
		if want := []time.Duration{30 * time.Second, 30 * time.Second}; !reflect.DeepEqual(waits, want) {
			t.Errorf("waits = %v, want %v", waits, want)
		}
		if out.String() != "" {
			t.Errorf("out = %q, want silence (a down server is expected)", out.String())
		}
	})

	t.Run("a cancelled context ends the loop without a cycle", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var out bytes.Buffer
		rc := run(ctx, cfg, hardware, machine,
			func(string, map[string]any) (int, map[string]any) {
				t.Error("a cancelled run must not POST")
				return 200, nil
			}, nil, nil, nil, nil,
			func(context.Context, time.Duration) bool { t.Error("must not sleep"); return true },
			0, &out)
		if rc != 0 || out.String() != "" {
			t.Errorf("rc = %d, out = %q, want 0 and silence", rc, out.String())
		}
	})

	t.Run("a cancellation during the wait ends the forever loop", func(t *testing.T) {
		var out bytes.Buffer
		posts := 0
		rc := run(context.Background(), cfg, hardware, machine,
			func(string, map[string]any) (int, map[string]any) { posts++; return 200, nil },
			nil, nil, nil, nil,
			func(context.Context, time.Duration) bool { return false },
			0, &out)
		if rc != 0 || posts != 1 {
			t.Errorf("rc = %d, posts = %d, want 0 and 1", rc, posts)
		}
	})
}

func TestSleepUntil(t *testing.T) {
	start := time.Now()
	if !sleepUntil(context.Background(), 30*time.Millisecond) {
		t.Error("a full sleep must report true")
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("returned after %v, want at least 30ms", elapsed)
	}

	if !sleepUntil(context.Background(), 0) {
		t.Error("a non-positive duration on a live ctx must report true")
	}
	if !sleepUntil(context.Background(), -time.Second) {
		t.Error("a negative duration on a live ctx must report true")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepUntil(cancelled, time.Hour) {
		t.Error("a cancelled ctx must report false")
	}
	if sleepUntil(cancelled, 0) {
		t.Error("a cancelled ctx must report false even with no duration")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	start = time.Now()
	if sleepUntil(ctx, time.Hour) {
		t.Error("a cancellation mid-sleep must report false")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("returned after %v, want promptly after the cancel", elapsed)
	}
}

func TestWaitGraceful(t *testing.T) {
	var empty sync.WaitGroup
	start := time.Now()
	waitGraceful(&empty, time.Hour)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("an empty group took %v, want an immediate return", elapsed)
	}

	var drains sync.WaitGroup
	drains.Add(1)
	go func() { time.Sleep(10 * time.Millisecond); drains.Done() }()
	start = time.Now()
	waitGraceful(&drains, time.Hour)
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond || elapsed > 5*time.Second {
		t.Errorf("a draining group took %v, want ~10ms", elapsed)
	}

	var wedged sync.WaitGroup
	wedged.Add(1)
	start = time.Now()
	waitGraceful(&wedged, 30*time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 30*time.Millisecond || elapsed > 5*time.Second {
		t.Errorf("a wedged group took %v, want the 30ms grace bound", elapsed)
	}
	wedged.Done()
}

func TestWireUpdaterSeams(t *testing.T) {
	transport := &sseTransport{}
	up := &updater{kick: make(chan struct{}, 1)}
	wireUpdaterSeams(transport, up)

	transport.onConnect()
	if len(up.kick) != 1 {
		t.Error("a reconnect must wake the self-update reconcile")
	}
	if up.renewDemanded.Load() {
		t.Error("a reconnect must not raise the credential-renewal demand")
	}
	<-up.kick

	transport.deps.Update()
	if len(up.kick) != 1 {
		t.Error("the update verb must wake the self-update reconcile")
	}
	if up.renewDemanded.Load() {
		t.Error("the update verb must not raise the credential-renewal demand")
	}
	<-up.kick

	transport.deps.Renew()
	if !up.renewDemanded.Load() {
		t.Error("the renew verb must raise the credential-renewal demand on the updater it was given")
	}
	if len(up.kick) != 1 {
		t.Error("the renew verb must also wake the poll loop")
	}
}

func TestRealMain(t *testing.T) {
	t.Run("no verb prints the usage banner", func(t *testing.T) {
		want := "usage: ocwarden {run [--once] | install | teardown [--canonical]}\n" +
			"  run       officraft per-machine hardware telemetry + command producer.\n" +
			"  install   install + start the launchd warden job on this machine.\n" +
			"  teardown  stop + remove a namespaced warden; canonical requires --canonical.\n"
		for _, argv := range [][]string{{}, {"serve"}, {"--help"}} {
			var out bytes.Buffer
			rc := realMain(argv, func(string) string { return "" }, &out)
			if rc != 0 || out.String() != want {
				t.Errorf("%v: rc = %d, out = %q", argv, rc, out.String())
			}
		}
	})

	t.Run("an unknown teardown flag is refused with its own usage line", func(t *testing.T) {
		var out bytes.Buffer
		rc := realMain([]string{"teardown", "--yolo"}, func(string) string { return "" }, &out)
		if rc != 2 || out.String() != "usage: ocwarden teardown [--canonical]\n" {
			t.Errorf("rc = %d, out = %q, want 2 and the teardown usage", rc, out.String())
		}
	})

	t.Run("an unknown run flag is a parse refusal", func(t *testing.T) {
		var out bytes.Buffer
		rc := realMain([]string{"run", "--yolo"}, func(string) string { return "" }, &out)
		if rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		if !strings.Contains(out.String(), "flag provided but not defined: -yolo") {
			t.Errorf("out = %q, want the flag refusal", out.String())
		}
	})

	t.Run("a malformed namespace is refused before anything is derived", func(t *testing.T) {
		var out bytes.Buffer
		env := map[string]string{"OC_NAMESPACE": "LAB", "OC_BASE": "http://127.0.0.1:7755"}
		rc := realMain([]string{"run", "--once"}, func(k string) string { return env[k] }, &out)
		want := "[ocwarden] FATAL: OC_NAMESPACE must match [a-z0-9-]{1,16}, got: \"LAB\"\n"
		if rc != 1 || out.String() != want {
			t.Errorf("rc = %d, out = %q, want 1 and %q", rc, out.String(), want)
		}
	})

	t.Run("an unset station address stops the run", func(t *testing.T) {
		var out bytes.Buffer
		rc := realMain([]string{"run", "--once"}, func(string) string { return "" }, &out)
		if rc != 1 {
			t.Errorf("rc = %d, want 1", rc)
		}
		first := "[ocwarden] FATAL: OC_BASE is not set — this warden was never told which station to talk to.\n"
		last := "[ocwarden] --once: refusing and exiting non-zero (no sentinel written; the launchd path halts instead)\n"
		if !strings.HasPrefix(out.String(), first) || !strings.HasSuffix(out.String(), last) {
			t.Errorf("out = %q, want the station-address refusal", out.String())
		}
	})

	t.Run("a station address with no credential exits clean without reporting", func(t *testing.T) {
		var out bytes.Buffer
		env := map[string]string{"OC_BASE": "http://127.0.0.1:7755", "HOME": t.TempDir()}
		rc := realMain([]string{"run", "--once"}, func(k string) string { return env[k] }, &out)
		want := "[ocwarden] run: no OC_TOKEN/OC_ID — nothing to report; exiting.\n"
		if rc != 0 || out.String() != want {
			t.Errorf("rc = %d, out = %q, want 0 and %q", rc, out.String(), want)
		}
	})
}
