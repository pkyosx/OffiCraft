package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// capturedPost is one POST the fake server saw.
type capturedPost struct {
	path string
	auth string
	body string
}

// contextServer captures EVERY POST (path/auth/body) and replies 200 {}.
func contextServer(t *testing.T) (*httptest.Server, *[]capturedPost) {
	t.Helper()
	var posts []capturedPost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		posts = append(posts, capturedPost{
			path: r.URL.Path,
			auth: r.Header.Get("Authorization"),
			body: string(raw),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &posts
}

// isolatedHome returns an env func that pins OC_AGENT_HOME (via Config.Home, set
// directly) — here we just build a Config with a temp Home so the throttle stamp
// lands in a scratch dir, and HOME points nowhere real so readClaudeAccount misses.
func testEnv(extra map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := extra[k]; ok {
			return v
		}
		return ""
	}
}

func findPost(posts []capturedPost, path string) *capturedPost {
	for i := range posts {
		if posts[i].path == path {
			return &posts[i]
		}
	}
	return nil
}

// TestContextReportPostsContextPct: a real used_percentage ⇒ POST /api/agent/context
// {context_pct} authed with the agent token, and the status line prints
// the rounded pct.
func TestContextReportPostsContextPct(t *testing.T) {
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "tok-k", ID: "kyle", Home: t.TempDir()}
	payload := `{"context_window":{"used_percentage":42.4}}`

	var out, errOut bytes.Buffer
	rc := cmdContextReport(srv.Client(), cfg, testEnv(nil), 1000.0, strings.NewReader(payload), &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	p := findPost(*posts, "/api/agent/context")
	if p == nil {
		t.Fatalf("no POST to /api/agent/context; posts=%v", *posts)
	}
	if p.auth != "Bearer tok-k" {
		t.Errorf("auth = %q, want Bearer tok-k", p.auth)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(p.body), &decoded); err != nil {
		t.Fatalf("body not JSON: %q", p.body)
	}
	// Identity is the verified JWT sub, NEVER a body key: the frozen ingest schema
	// refuses unknown fields, so a self-reported agent_id 422s the whole report.
	if _, present := decoded["agent_id"]; present {
		t.Errorf("agent_id must not be on the wire; body = %q", p.body)
	}
	if decoded["context_pct"] != 42.4 {
		t.Errorf("context_pct = %v, want 42.4", decoded["context_pct"])
	}
	// int(round(42.4)) = 42 — the context segment renders the rounded pct.
	if got := stripANSI(out.String()); !strings.Contains(got, "42%") {
		t.Errorf("status line = %q, want to contain 42%%", got)
	}
}

// stripANSI removes CSI colour sequences (\x1b[…m) so a test can assert the
// visible status-line layout without coupling to the ANSI colour bytes.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// TestContextReportBankersRounding: int(round(42.5)) is 42 under Python's banker's
// rounding (round-half-to-even), NOT 43.
func TestContextReportBankersRounding(t *testing.T) {
	cfg := Config{BaseConfigured: true, Base: "http://127.0.0.1:1", Token: "", ID: "", Home: t.TempDir()}
	var out, errOut bytes.Buffer
	// No token ⇒ no POST, but the status line still renders from the payload pct.
	cmdContextReport(defaultHTTPClient(), cfg, testEnv(nil), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":42.5}}`), &out, &errOut)
	if got := stripANSI(out.String()); !strings.Contains(got, "42%") || strings.Contains(got, "43%") {
		t.Errorf("42.5 ⇒ %q, want 42%% (banker's rounding)", got)
	}
	out.Reset()
	cmdContextReport(defaultHTTPClient(), cfg, testEnv(nil), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":43.5}}`), &out, &errOut)
	if got := stripANSI(out.String()); !strings.Contains(got, "44%") {
		t.Errorf("43.5 ⇒ %q, want 44%% (banker's rounding)", got)
	}
}

// TestContextReportNullPctSkipsContextPost: used_percentage null ⇒ no /api/agent/context
// POST + status line shows "?". (Telemetry is pct-independent — none here.)
func TestContextReportNullPctSkipsContextPost(t *testing.T) {
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	var out, errOut bytes.Buffer
	rc := cmdContextReport(srv.Client(), cfg, testEnv(nil), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":null}}`), &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if findPost(*posts, "/api/agent/context") != nil {
		t.Errorf("null pct must NOT POST context; posts=%v", *posts)
	}
	// null pct ⇒ the context segment is DROPPED (no fabricated 0, no "?" shell).
	// With no other source in this payload the line is empty.
	if got := stripANSI(out.String()); strings.Contains(got, "%") {
		t.Errorf("null pct must not render a context %%; got %q", got)
	}
}

// TestContextReportClampsPct: values outside 0–100 clamp.
func TestContextReportClampsPct(t *testing.T) {
	if v, ok := statuslinePct(`{"context_window":{"used_percentage":150}}`); !ok || v != 100 {
		t.Errorf("150 ⇒ (%v,%v), want (100,true)", v, ok)
	}
	if v, ok := statuslinePct(`{"context_window":{"used_percentage":-5}}`); !ok || v != 0 {
		t.Errorf("-5 ⇒ (%v,%v), want (0,true)", v, ok)
	}
	// A bool is excluded (isinstance(pct, bool)).
	if _, ok := statuslinePct(`{"context_window":{"used_percentage":true}}`); ok {
		t.Errorf("bool used_percentage must be skipped")
	}
	// Junk / missing.
	if _, ok := statuslinePct(`not json`); ok {
		t.Errorf("junk payload must be skipped")
	}
}

// TestContextReportTelemetry: rate_limits/cost/tokens ⇒ POST /api/monitoring/telemetry
// with runtime, the passed-through windows, cost (incl 0.0), and the machine tag.
// Also pins the two identity rules the wire depends on: no self-reported agent_id,
// and an unreadable account is OMITTED rather than sent as a literal "unknown".
func TestContextReportTelemetry(t *testing.T) {
	srv, posts := contextServer(t)
	home := t.TempDir()
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: home}
	// A transcript for tokens: one assistant row dated today (UTC).
	today := time.Now().UTC().Format("2006-01-02")
	transcript := filepath.Join(home, "t.jsonl")
	line := `{"type":"assistant","timestamp":"` + today + `T10:00:00Z","message":{"usage":{"input_tokens":100,"cache_creation_input_tokens":10,"output_tokens":20,"cache_read_input_tokens":5}}}`
	if err := os.WriteFile(transcript, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := `{
		"context_window":{"used_percentage":null},
		"rate_limits":{
			"five_hour":{"used_percentage":30,"resets_at":1720000000},
			"seven_day":{"used_percentage":60,"resets_at":1720500000}
		},
		"cost":{"total_cost_usd":0.0},
		"transcript_path":"` + transcript + `"
	}`
	var out, errOut bytes.Buffer
	// HOME → an empty temp dir so readClaudeAccount misses (no ~/.claude*.json) —
	// never the developer's real account id.
	rc := cmdContextReport(srv.Client(), cfg,
		testEnv(map[string]string{"OC_HOST": "lab-1", "HOME": t.TempDir()}),
		1000.0, strings.NewReader(payload), &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	// pct null ⇒ no context POST, but telemetry POST fires.
	if findPost(*posts, "/api/agent/context") != nil {
		t.Errorf("null pct must not POST context")
	}
	p := findPost(*posts, "/api/monitoring/telemetry")
	if p == nil {
		t.Fatalf("no telemetry POST; posts=%v", *posts)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(p.body), &d); err != nil {
		t.Fatalf("telemetry body not JSON: %q", p.body)
	}
	if _, present := d["agent_id"]; present {
		t.Errorf("agent_id must not be on the wire; body = %q", p.body)
	}
	if d["runtime"] != "claude" {
		t.Errorf("runtime = %v, want claude", d["runtime"])
	}
	// An unreadable account is ABSENT. A literal "unknown" would mint a phantom
	// account row in the owner's monitoring fold that reads as a real account.
	if _, present := d["account"]; present {
		t.Errorf("unreadable account must be omitted, got %v", d["account"])
	}
	if d["machine"] != "lab-1" {
		t.Errorf("machine = %v, want lab-1 (OC_HOST)", d["machine"])
	}
	// cost 0.0 is KEPT (real zero, not omitted).
	if cost, ok := d["cost"].(float64); !ok || cost != 0.0 {
		t.Errorf("cost = %v, want 0.0 kept", d["cost"])
	}
	rl, ok := d["rate_limits"].(map[string]any)
	if !ok {
		t.Fatalf("rate_limits missing/not object: %v", d["rate_limits"])
	}
	fh, _ := rl["five_hour"].(map[string]any)
	if fh["used_percentage"] != float64(30) || fh["resets_at"] != float64(1720000000) {
		t.Errorf("five_hour = %v", fh)
	}
	tok, ok := d["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens missing: %v", d["tokens"])
	}
	if tok["burned"] != float64(110) || tok["output"] != float64(20) || tok["cache_read"] != float64(5) {
		t.Errorf("tokens = %v, want burned=110 output=20 cache_read=5", tok)
	}
}

// TestContextReportUnmeasuredUsageStillReportsIdentity: a payload with NO usage
// source (no rate_limits, no cost, no transcript) must STILL report identity.
// Usage and identity used to share one gate, so a session whose usage was not yet
// measurable reported nothing at all — and the cockpit kept showing whatever
// account a previous runtime had left behind, which is worse than showing nothing.
func TestContextReportUnmeasuredUsageStillReportsIdentity(t *testing.T) {
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	home := writeClaudeJSON(t,
		`{"userID":"acct-9","oauthAccount":{"emailAddress":"e@x.io","organizationUuid":"org-9"}}`)
	var out, errOut bytes.Buffer
	cmdContextReport(srv.Client(), cfg, testEnv(map[string]string{"HOME": home}), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":10}}`), &out, &errOut)

	p := findPost(*posts, "/api/monitoring/telemetry")
	if p == nil {
		t.Fatalf("unmeasured usage must still report identity; posts=%v", *posts)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(p.body), &d); err != nil {
		t.Fatalf("telemetry body not JSON: %q", p.body)
	}
	if d["account"] != "acct-9/org-9" || d["runtime"] != "claude" {
		t.Errorf("identity = account %v runtime %v, want acct-9/org-9 + claude",
			d["account"], d["runtime"])
	}
	// The unmeasured fields stay ABSENT — never fabricated as zero.
	for _, key := range []string{"rate_limits", "cost", "tokens"} {
		if _, present := d[key]; present {
			t.Errorf("%s has no source ⇒ must be omitted, got %v", key, d[key])
		}
	}
	// and the context POST still fired (pct present)
	if findPost(*posts, "/api/agent/context") == nil {
		t.Errorf("pct present ⇒ context POST expected")
	}
}

// TestContextReportThrottle: a fresh stamp within the window suppresses BOTH POSTs,
// but the status line still prints.
func TestContextReportThrottle(t *testing.T) {
	srv, posts := contextServer(t)
	home := t.TempDir()
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: home}
	// Pre-seed the stamp at now-5 (< 30s window ⇒ throttled).
	stamp := reportStampPath(cfg)
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stamp, []byte("995.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":50}}`), &out, &errOut)
	if len(*posts) != 0 {
		t.Errorf("throttled ⇒ no POSTs, got %v", *posts)
	}
	if got := stripANSI(out.String()); !strings.Contains(got, "50%") {
		t.Errorf("status line = %q, want to contain 50%%", got)
	}
}

// TestReportStatePathsAreLiteralAndPerAgent pins the two cadence-state files to
// their EXACT locations, spelled out here rather than computed by calling the
// helpers under test.
//
// That is the whole point of the test. Every other assertion about these files
// asks reportStampPath / reportBackoffPath where the file is and then checks
// there, so the test and the code read the path from one source and agree with
// each other no matter what that source says — a helper that dropped a path
// component entirely would keep the whole suite green.
//
// 🔴 The dropped component that matters is the PER-AGENT one. cfg.Home in
// production is the SHARED agents root, so `<home>/context_report.backoff`
// (no id) would be one file for every agent on the box: one agent's outage would
// back off — and so silence — every other agent on that machine. Same blast
// radius for the stamp, whose absence is also the "this reporter is delivering"
// evidence. If you are changing these paths, change the literals here too, on
// purpose.
func TestReportStatePathsAreLiteralAndPerAgent(t *testing.T) {
	cfg := Config{Home: filepath.Join("/scratch", "agents"), ID: "Kyle"}
	// Spelled out component by component: <home>/<lowercased id>/<file>.
	wantDir := filepath.Join("/scratch", "agents", "kyle")
	if got := reportStampPath(cfg); got != filepath.Join(wantDir, "context_report.stamp") {
		t.Errorf("reportStampPath = %q, want %q", got, filepath.Join(wantDir, "context_report.stamp"))
	}
	if got := reportBackoffPath(cfg); got != filepath.Join(wantDir, "context_report.backoff") {
		t.Errorf("reportBackoffPath = %q, want %q", got, filepath.Join(wantDir, "context_report.backoff"))
	}
	// An id-less agent gets its own "anon" segment — still NOT the bare home.
	anon := Config{Home: filepath.Join("/scratch", "agents")}
	wantAnon := filepath.Join("/scratch", "agents", "anon")
	if got := reportStampPath(anon); got != filepath.Join(wantAnon, "context_report.stamp") {
		t.Errorf("anon reportStampPath = %q, want %q", got, filepath.Join(wantAnon, "context_report.stamp"))
	}
	if got := reportBackoffPath(anon); got != filepath.Join(wantAnon, "context_report.backoff") {
		t.Errorf("anon reportBackoffPath = %q, want %q", got, filepath.Join(wantAnon, "context_report.backoff"))
	}
	// Two agents under ONE home must never share either file.
	a := Config{Home: filepath.Join("/scratch", "agents"), ID: "kyle"}
	b := Config{Home: filepath.Join("/scratch", "agents"), ID: "robin"}
	if reportBackoffPath(a) == reportBackoffPath(b) {
		t.Errorf("two agents share a backoff record at %q — one agent's outage would silence the other",
			reportBackoffPath(a))
	}
	if reportStampPath(a) == reportStampPath(b) {
		t.Errorf("two agents share a throttle stamp at %q", reportStampPath(a))
	}
}

// TestContextReportStampAdvances: after a non-throttled report the stamp is written
// with `now`, so the next tick would be throttled.
func TestContextReportStampAdvances(t *testing.T) {
	srv, _ := contextServer(t)
	home := t.TempDir()
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: home}
	var out, errOut bytes.Buffer
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 2000.0,
		strings.NewReader(`{"context_window":{"used_percentage":50}}`), &out, &errOut)
	raw, err := os.ReadFile(reportStampPath(cfg))
	if err != nil {
		t.Fatalf("stamp not written: %v", err)
	}
	got, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil || got != 2000.0 {
		t.Errorf("stamp = %q, want 2000", raw)
	}
}

// TestContextReportNoTokenNoPost: no OC_TOKEN/OC_ID ⇒ no POST, no stamp, but the
// status line still renders (dual-use).
func TestContextReportNoTokenNoPost(t *testing.T) {
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "", ID: "", Home: t.TempDir()}
	var out, errOut bytes.Buffer
	rc := cmdContextReport(srv.Client(), cfg, testEnv(nil), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":77}}`), &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if len(*posts) != 0 {
		t.Errorf("no token ⇒ no POST, got %v", *posts)
	}
	if got := stripANSI(out.String()); !strings.Contains(got, "77%") {
		t.Errorf("status line = %q, want to contain 77%%", got)
	}
}

// TestContextReportBestEffortOnFault: an unreachable base still exits 0 + prints.
func TestContextReportBestEffortOnFault(t *testing.T) {
	cfg := Config{BaseConfigured: true, Base: "http://127.0.0.1:1", Token: "t", ID: "kyle", Home: t.TempDir()}
	var out, errOut bytes.Buffer
	rc := cmdContextReport(defaultHTTPClient(), cfg, testEnv(nil), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":30}}`), &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (best-effort)", rc)
	}
	if got := stripANSI(out.String()); !strings.Contains(got, "30%") {
		t.Errorf("status line = %q, want to contain 30%%", got)
	}
}

// writeClaudeJSON drops a ~/.claude/.claude.json under a fresh HOME and returns
// that HOME (the primary candidate readClaudeAccount reads first).
func writeClaudeJSON(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", ".claude.json"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

// writeClaudeCredentials drops a ~/.claude/.credentials.json under an existing
// HOME. Fixtures carry fake token fields; the account key must never read this
// file at all (T-f694).
func writeClaudeCredentials(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"),
		[]byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestReadClaudeAccount: the account key is "<accountUuid>/<organizationUuid>" (bare
// accountUuid when no org, "" when no account identity) — and it NEVER depends on the
// credentials file / subscriptionType (T-f694: the same account must key the
// same on every machine regardless of credential storage form).
func TestReadClaudeAccount(t *testing.T) {
	// The canonical key: accountUuid + org from .claude.json.
	teamHome := writeClaudeJSON(t,
		`{"userID":"device-1","oauthAccount":{"accountUuid":"acct-123","organizationUuid":"org-team"}}`)
	if got := readClaudeAccount(testEnv(map[string]string{"HOME": teamHome})); got != "acct-123/org-team" {
		t.Errorf("team key = %q, want acct-123/org-team", got)
	}

	secondDeviceHome := writeClaudeJSON(t,
		`{"userID":"device-2","oauthAccount":{"accountUuid":"acct-123","organizationUuid":"org-team"}}`)
	if got := readClaudeAccount(testEnv(map[string]string{"HOME": secondDeviceHome})); got != "acct-123/org-team" {
		t.Errorf("same account on another device = %q, want acct-123/org-team", got)
	}

	// REGRESSION (T-f694): the same accountUuid + org must produce the SAME key whatever
	// the credentials file says (any subscriptionType, blank, bad json) or
	// whether it exists at all — the plan never joins the key, so a file-creds
	// machine and a Keychain-only machine can no longer split into two rows.
	for _, credBody := range []string{
		`{"claudeAiOauth":{"accessToken":"fake-at","refreshToken":"fake-rt","subscriptionType":"team"}}`,
		`{"claudeAiOauth":{"accessToken":"fake-at","subscriptionType":"max"}}`,
		`{"claudeAiOauth":{"accessToken":"fake-at","subscriptionType":"pro"}}`,
		`{"claudeAiOauth":{"accessToken":"fake-at","subscriptionType":""}}`,
		`{not json`,
	} {
		home := writeClaudeJSON(t,
			`{"userID":"device-1","oauthAccount":{"accountUuid":"acct-123","organizationUuid":"org-team"}}`)
		writeClaudeCredentials(t, home, credBody)
		if got := readClaudeAccount(testEnv(map[string]string{"HOME": home})); got != "acct-123/org-team" {
			t.Errorf("credentials %s ⇒ %q, want acct-123/org-team (creds file must not affect the key)", credBody, got)
		}
	}

	// A personal login with no org degrades HONESTLY to the bare accountUuid — no
	// dangling "acct-123/" suffix.
	personalHome := writeClaudeJSON(t, `{"userID":"device-1","oauthAccount":{"accountUuid":"acct-123"}}`)
	if got := readClaudeAccount(testEnv(map[string]string{"HOME": personalHome})); got != "acct-123" {
		t.Errorf("personal key = %q, want acct-123 (bare, no trailing slash)", got)
	}

	// The same account in different orgs must not collide.
	otherOrgHome := writeClaudeJSON(t,
		`{"userID":"device-2","oauthAccount":{"accountUuid":"acct-123","organizationUuid":"org-personal"}}`)
	teamKey := readClaudeAccount(testEnv(map[string]string{"HOME": teamHome}))
	otherKey := readClaudeAccount(testEnv(map[string]string{"HOME": otherOrgHome}))
	if teamKey == otherKey {
		t.Errorf("same account, different org collided: both %q", teamKey)
	}

	// And the same account with-org vs without-org must NOT collide either.
	if teamKey == readClaudeAccount(testEnv(map[string]string{"HOME": personalHome})) {
		t.Errorf("same account, org vs no-org collided: %q", teamKey)
	}

	// A blank / non-string / null org is treated as absent ⇒ bare accountUuid (never
	// "acct-123/" or "acct-123/None").
	for _, body := range []string{
		`{"userID":"device-1","oauthAccount":{"accountUuid":"acct-123","organizationUuid":""}}`,
		`{"userID":"device-1","oauthAccount":{"accountUuid":"acct-123","organizationUuid":null}}`,
		`{"userID":"device-1","oauthAccount":{"accountUuid":"acct-123","organizationUuid":42}}`,
		`{"userID":"device-1","oauthAccount":{"accountUuid":"acct-123"}}`,
	} {
		home := writeClaudeJSON(t, body)
		if got := readClaudeAccount(testEnv(map[string]string{"HOME": home})); got != "acct-123" {
			t.Errorf("body %s ⇒ %q, want bare acct-123", body, got)
		}
	}

	// Split two-file layout: userID lives in ~/.claude/.claude.json while the
	// oauthAccount lives in ~/.claude.json — the account and org must still join
	// the key.
	splitHome := writeClaudeJSON(t, `{"userID":"device-1"}`)
	if err := os.WriteFile(filepath.Join(splitHome, ".claude.json"),
		[]byte(`{"oauthAccount":{"accountUuid":"acct-123","organizationUuid":"org-team"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readClaudeAccount(testEnv(map[string]string{"HOME": splitHome})); got != "acct-123/org-team" {
		t.Errorf("split-file key = %q, want acct-123/org-team", got)
	}

	legacyHome := writeClaudeJSON(t,
		`{"userID":"legacy-device","oauthAccount":{"organizationUuid":"org-team"}}`)
	if got := readClaudeAccount(testEnv(map[string]string{"HOME": legacyHome})); got != "legacy-device/org-team" {
		t.Errorf("legacy key = %q, want legacy-device/org-team", got)
	}

	// Missing everywhere ⇒ "".
	if got := readClaudeAccount(testEnv(map[string]string{"HOME": t.TempDir()})); got != "" {
		t.Errorf("no file ⇒ %q, want empty", got)
	}
}

// TestReadClaudeAccountLabel (T-260e): the human-readable owner-facing label —
// "<emailAddress>(<organizationName>)" from oauthAccount — with the same
// two-file layout + missing-field degradation discipline as readClaudeAccount
// (the T-713a lesson: real installs split fields across ~/.claude/.claude.json
// and ~/.claude.json, and every field resolves INDEPENDENTLY).
func TestReadClaudeAccountLabel(t *testing.T) {
	// Full oauthAccount ⇒ "email(Org)".
	fullHome := writeClaudeJSON(t,
		`{"userID":"acct-123","oauthAccount":{"emailAddress":"eva.cheng@gofreight.com","displayName":"Eva Cheng","organizationName":"GoFreight"}}`)
	if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": fullHome})); got != "eva.cheng@gofreight.com(GoFreight)" {
		t.Errorf("full label = %q, want eva.cheng@gofreight.com(GoFreight)", got)
	}

	// Missing organizationName ⇒ bare email (never a dangling "email()").
	noOrgHome := writeClaudeJSON(t,
		`{"oauthAccount":{"emailAddress":"eva.cheng@gofreight.com"}}`)
	if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": noOrgHome})); got != "eva.cheng@gofreight.com" {
		t.Errorf("no-org label = %q, want bare email", got)
	}

	// Missing emailAddress ⇒ displayName carries the label.
	noEmailHome := writeClaudeJSON(t,
		`{"oauthAccount":{"displayName":"Eva Cheng","organizationName":"GoFreight"}}`)
	if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": noEmailHome})); got != "Eva Cheng(GoFreight)" {
		t.Errorf("displayName fallback = %q, want Eva Cheng(GoFreight)", got)
	}

	// Blank / null / non-string fields are treated as absent (never "null" or
	// a stringified number in the label).
	for _, body := range []string{
		`{"oauthAccount":{"emailAddress":"","displayName":null,"organizationName":42}}`,
		`{"oauthAccount":{}}`,
		`{"userID":"acct-123"}`,
		`{}`,
	} {
		home := writeClaudeJSON(t, body)
		if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": home})); got != "" {
			t.Errorf("body %s ⇒ %q, want empty label", body, got)
		}
	}

	// Split two-file layout: the email lives in ~/.claude/.claude.json while the
	// organizationName lives in ~/.claude.json — both must still join the label.
	splitHome := writeClaudeJSON(t,
		`{"oauthAccount":{"emailAddress":"eva.cheng@gofreight.com"}}`)
	if err := os.WriteFile(filepath.Join(splitHome, ".claude.json"),
		[]byte(`{"oauthAccount":{"organizationName":"GoFreight"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": splitHome})); got != "eva.cheng@gofreight.com(GoFreight)" {
		t.Errorf("split-file label = %q, want eva.cheng@gofreight.com(GoFreight)", got)
	}

	// No file at all ⇒ "".
	if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": t.TempDir()})); got != "" {
		t.Errorf("no file ⇒ %q, want empty", got)
	}
}

// TestContextReportTelemetryAccountLabel (T-260e): an oauthAccount with an email
// rides the telemetry POST as account_label; a HOME without one OMITS the key
// entirely (the server must see absent, not "").
func TestContextReportTelemetryAccountLabel(t *testing.T) {
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	home := writeClaudeJSON(t,
		`{"userID":"acct-123","oauthAccount":{"emailAddress":"eva.cheng@gofreight.com","organizationName":"GoFreight","organizationUuid":"org-team"}}`)
	payload := `{"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":1720000000}}}`
	var out, errOut bytes.Buffer
	rc := cmdContextReport(srv.Client(), cfg,
		testEnv(map[string]string{"HOME": home}), 1000.0, strings.NewReader(payload), &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	p := findPost(*posts, "/api/monitoring/telemetry")
	if p == nil {
		t.Fatalf("no telemetry POST; posts=%v", *posts)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(p.body), &d); err != nil {
		t.Fatalf("telemetry body not JSON: %q", p.body)
	}
	if d["account_label"] != "eva.cheng@gofreight.com(GoFreight)" {
		t.Errorf("account_label = %v, want eva.cheng@gofreight.com(GoFreight)", d["account_label"])
	}
	// The account KEY dimension is unchanged by the label (hash/org key intact).
	if d["account"] != "acct-123/org-team" {
		t.Errorf("account = %v, want acct-123/org-team (key untouched by label)", d["account"])
	}

	// Missing oauthAccount ⇒ the key is OMITTED from the wire body.
	*posts = nil
	cfg2 := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	rc = cmdContextReport(srv.Client(), cfg2,
		testEnv(map[string]string{"HOME": t.TempDir()}), 1000.0, strings.NewReader(payload), &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	p = findPost(*posts, "/api/monitoring/telemetry")
	if p == nil {
		t.Fatalf("no telemetry POST; posts=%v", *posts)
	}
	if strings.Contains(p.body, "account_label") {
		t.Errorf("no oauthAccount ⇒ account_label must be omitted; body=%q", p.body)
	}
}

// TestContextReportViaRealMain: the dispatch path (no flags) drives the whole chain.
func TestContextReportViaRealMain(t *testing.T) {
	srv, posts := contextServer(t)
	env := testEnv(map[string]string{
		"OC_BASE":       srv.URL,
		"OC_TOKEN":      "t",
		"OC_ID":         "kyle",
		"OC_AGENT_HOME": t.TempDir(),
	})
	var out bytes.Buffer
	rc := realMain([]string{"context-report"}, env,
		strings.NewReader(`{"context_window":{"used_percentage":88}}`), &out)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if findPost(*posts, "/api/agent/context") == nil {
		t.Errorf("expected context POST via realMain; posts=%v", *posts)
	}
	if got := stripANSI(out.String()); !strings.Contains(got, "88%") {
		t.Errorf("status line = %q, want to contain 88%%", got)
	}
}

// ── statusline rendering (T-51a8) ───────────────────────────────────────────

// TestRenderStatuslineFull: every field present ⇒ the full owner layout
//
//	◆ <model> (1M context) ⚡med | <bar> N% | $X.XX | XmYYs | 5h:N%(rst:XhYm) 7d:N%(N%elapsed)
//
// The now/resets_at are chosen so the 5h countdown is exactly 3h7m and the 7d
// window is 85% elapsed.
func TestRenderStatuslineFull(t *testing.T) {
	const now = 1_000_000.0
	fiveReset := now + 3*3600 + 7*60 // 3h7m out
	sevenReset := now + 90720        // ⇒ 85% of the 7-day window elapsed
	payload := `{
		"model":{"display_name":"Opus 4.8","id":"claude-opus-4-8[1m]"},
		"effort":{"level":"medium"},
		"context_window":{"used_percentage":5},
		"cost":{"total_cost_usd":0.46,"total_duration_ms":14000},
		"rate_limits":{
			"five_hour":{"used_percentage":26,"resets_at":` + strconv.FormatFloat(fiveReset, 'f', -1, 64) + `},
			"seven_day":{"used_percentage":16,"resets_at":` + strconv.FormatFloat(sevenReset, 'f', -1, 64) + `}
		}
	}`
	got := renderStatusline(payload, now)

	// Colours must be present (Claude Code statusLine honours ANSI).
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("expected ANSI colour codes; got %q", got)
	}
	want := "◆ Opus 4.8 (1M context) ⚡med | █░░░░░░░░░ 5% | $0.46 | 0m14s | 5h:26%(rst:3h7m) 7d:16%(85%elapsed)"
	if plain := stripANSI(got); plain != want {
		t.Errorf("full statusline:\n got  %q\n want %q", plain, want)
	}
}

// TestRenderStatuslinePartialNull: partial/null fields drop ONLY their own
// segment — no panic, no empty shell. Here display_name is absent (no ◆ model),
// context pct is null (no bar), duration is absent (no time), and five_hour's
// resets_at is null (the WHOLE 5h window is skipped — task rule: any null ⇒ skip).
// What survives: ⚡med, cost, and the fully-present 7d window.
func TestRenderStatuslinePartialNull(t *testing.T) {
	const now = 1_000_000.0
	sevenReset := now + 302400 // ⇒ 50% elapsed
	payload := `{
		"model":{"id":"claude-sonnet-4"},
		"effort":{"level":"medium"},
		"context_window":{"used_percentage":null},
		"cost":{"total_cost_usd":1.5},
		"rate_limits":{
			"five_hour":{"used_percentage":30,"resets_at":null},
			"seven_day":{"used_percentage":40,"resets_at":` + strconv.FormatFloat(sevenReset, 'f', -1, 64) + `}
		}
	}`
	want := "⚡med | $1.50 | 7d:40%(50%elapsed)"
	if plain := stripANSI(renderStatusline(payload, now)); plain != want {
		t.Errorf("partial-null statusline:\n got  %q\n want %q", plain, want)
	}
}

// TestRenderStatuslineAllMissing: an empty object and junk both yield an empty
// line (never a panic, never a stray "◆"/separators).
func TestRenderStatuslineAllMissing(t *testing.T) {
	for _, payload := range []string{`{}`, `not json`, ``, `null`} {
		if got := renderStatusline(payload, 1000.0); got != "" {
			t.Errorf("payload %q ⇒ %q, want empty line", payload, got)
		}
	}
}

// TestModelEffort1M: the "(1M context)" hint is appended iff model.id carries the
// [1m] tier tag AND display_name doesn't already say so.
func TestModelEffort1M(t *testing.T) {
	cases := []struct {
		name, displayName, id, want string
	}{
		{"1m tag appends", "Opus 4.8", "claude-opus-4-8[1m]", "◆ Opus 4.8 (1M context)"},
		{"no tag no append", "Opus 4.8", "claude-opus-4-8", "◆ Opus 4.8"},
		{"already says 1M", "Opus 4.8 1M", "claude-opus-4-8[1m]", "◆ Opus 4.8 1M"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := map[string]any{"model": map[string]any{"display_name": c.displayName, "id": c.id}}
			if got := stripANSI(modelEffortSegment(obj)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestEffortLabel: medium abbreviates to "med"; other values pass through; a
// payload with no effort block (the model has no effort parameter) yields "".
func TestEffortLabel(t *testing.T) {
	cases := map[string]string{"medium": "med", "high": "high", "low": "low", "xhigh": "xhigh"}
	for in, want := range cases {
		obj := map[string]any{"effort": map[string]any{"level": in}}
		if got := effortLabel(obj); got != want {
			t.Errorf("effort.level=%q ⇒ %q, want %q", in, got, want)
		}
	}
	for _, obj := range []map[string]any{nil, {}, {"effort": map[string]any{}}, {"effort": "high"}} {
		if got := effortLabel(obj); got != "" {
			t.Errorf("no readable effort.level ⇒ %q, want \"\"", got)
		}
	}
}

// TestDurationSegment: sub-hour renders XmYYs, at/over an hour renders XhYYm.
func TestDurationSegment(t *testing.T) {
	cases := []struct {
		ms   float64
		want string
	}{
		{14000, "0m14s"},
		{125000, "2m05s"},
		{3_665_000, "1h01m"},
		{0, "0m00s"},
	}
	for _, c := range cases {
		obj := map[string]any{"cost": map[string]any{"total_duration_ms": c.ms}}
		if got := durationSegment(obj); got != c.want {
			t.Errorf("%vms ⇒ %q, want %q", c.ms, got, c.want)
		}
	}
}

// refusingServer replies 422 to every POST and records the paths it saw. The
// refusal body mimics the real schema refusal (DisallowUnknownFields).
func refusingServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"nope"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// driveTicks simulates the statusLine tick loop over VIRTUAL time: it calls
// cmdContextReport once every `step` virtual seconds from 0 to `until`, and
// returns the virtual times at which a burst actually went out on the wire.
//
// No real sleeping anywhere — `now` is already an injected parameter (the same
// time seam every throttle test in this file uses), so a 30-minute simulation
// runs in milliseconds. `count` reports how many requests the fake server has
// seen so far; a tick counts as "a burst went out" iff that number moved.
func driveTicks(t *testing.T, client httpClient, cfg Config, count func() int, until, step float64) []float64 {
	t.Helper()
	var attempts []float64
	last := count()
	// step is exactly representable in binary (0.5), so `now` accumulates exactly.
	for now := 0.0; now <= until; now += step {
		var out, errOut bytes.Buffer
		cmdContextReport(client, cfg, testEnv(nil), now,
			strings.NewReader(`{"context_window":{"used_percentage":50}}`), &out, &errOut)
		if n := count(); n != last {
			attempts = append(attempts, now)
			last = n
		}
	}
	return attempts
}

// gapsOf turns attempt timestamps into the intervals between them.
func gapsOf(attempts []float64) []float64 {
	var gaps []float64
	for i := 1; i < len(attempts); i++ {
		gaps = append(gaps, attempts[i]-attempts[i-1])
	}
	return gaps
}

func sameFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRefusedReportBacksOffToAnIntentionalCap pins BOTH halves of the failure
// path, because each half was a real bug in turn:
//
//  1. the throttle stamp is evidence of DELIVERY, not of attempt — a refused
//     burst must NOT advance it. (Before that fix a reporter whose every POST was
//     422'd looked, from the outside, exactly like a healthy one.)
//  2. leaving the stamp alone must not degrade into "re-POST on every tick". A
//     statusLine ticks several times a SECOND, so binding the stamp to delivery
//     switched the throttle fully OFF for a server that kept refusing: measured
//     against a real always-500 endpoint, the shipped binary sent an unbounded
//     ~0.4s-spaced burst stream for as long as the outage lasted.
//
// The retry spacing is therefore expected to GROW and to STOP growing. Both the
// growth and the ceiling are DELIBERATE, and this test is where that intent is
// recorded: 30s (never denser than a healthy reporter) → 60 → 120 → 240 → then
// pinned at the 300s cap forever. If you are here because this test went red
// after you changed reportBackoffCapSecs or reportBackoffSecs, the cap is not an
// accident you may drop — read their doc comments first.
func TestRefusedReportBacksOffToAnIntentionalCap(t *testing.T) {
	srv, seen := refusingServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	stamp := reportStampPath(cfg)
	payload := `{"context_window":{"used_percentage":50}}`

	var out, errOut bytes.Buffer
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 2000.0, strings.NewReader(payload), &out, &errOut)
	if len(*seen) != 2 {
		t.Fatalf("tick 1: expected both POSTs attempted, saw %v", *seen)
	}
	if raw, err := os.ReadFile(stamp); err == nil {
		t.Errorf("a refused burst must NOT stamp the throttle window; stamp = %q", raw)
	}
	// Tick 2, five seconds later. It must be SILENT: the refusal opens a backoff,
	// and this is the tick that used to re-POST (and did so several times a second).
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 2005.0, strings.NewReader(payload), &out, &errOut)
	if len(*seen) != 2 {
		t.Errorf("a tick 5s after a refusal must be held by the backoff, saw %v", *seen)
	}
	// ...yet the stamp is STILL absent. Backing off must never be implemented by
	// stamping the throttle window: that would re-tell the lie in (1).
	if raw, err := os.ReadFile(stamp); err == nil {
		t.Errorf("backing off must not stamp the window; stamp = %q", raw)
	}
	if strings.Count(errOut.String(), "FAILED") != 2 {
		t.Errorf("every refused POST must keep leaving a trace; stderr = %q", errOut.String())
	}

	// Now the curve, over 30 virtual minutes of ticking against a server that never
	// recovers. Attempts at 0, 30, 90, 210, 450, 750, 1050, 1350, 1650.
	srv2, seen2 := refusingServer(t)
	cfg2 := Config{BaseConfigured: true, Base: srv2.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	attempts := driveTicks(t, srv2.Client(), cfg2, func() int { return len(*seen2) }, 1800.0, 0.5)
	want := []float64{30, 60, 120, 240, 300, 300, 300, 300}
	if got := gapsOf(attempts); !sameFloats(got, want) {
		t.Errorf("retry spacing = %v, want %v (attempts at %v)", got, want, attempts)
	}
	// And the stamp is still untouched after 30 minutes of pure refusal.
	if raw, err := os.ReadFile(reportStampPath(cfg2)); err == nil {
		t.Errorf("30min of refusals must leave no stamp; stamp = %q", raw)
	}
}

// TestAcceptedBurstResetsTheFailureBackoff: the backoff must not outlive the
// outage that caused it. After several refusals have stretched the spacing, ONE
// accepted burst returns the reporter to the plain 30s cadence immediately —
// rather than making a recovered agent keep reporting at the outage's rate.
func TestAcceptedBurstResetsTheFailureBackoff(t *testing.T) {
	refuse := true
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if refuse {
			w.WriteHeader(422)
			_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"nope"}}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	payload := `{"context_window":{"used_percentage":50}}`
	tick := func(now float64) int {
		var out, errOut bytes.Buffer
		cmdContextReport(srv.Client(), cfg, testEnv(nil), now, strings.NewReader(payload), &out, &errOut)
		return len(seen)
	}

	// Four consecutive refusals (0, 30, 90, 210) stretch the spacing to 240s.
	tick(0)
	tick(30)
	tick(90)
	if n := tick(210); n != 8 {
		t.Fatalf("expected 4 refused bursts (8 POSTs), saw %d: %v", n, seen)
	}
	if got := reportBackoffSecs(readReportBackoff(reportBackoffPath(cfg)).failures); got != 240 {
		t.Fatalf("after 4 refusals the next wait should be 240s, got %v", got)
	}

	// The server recovers; the next permitted attempt (210+240=450) is accepted, and
	// the failure record is gone.
	refuse = false
	if n := tick(450); n != 10 {
		t.Fatalf("the attempt at +240s must go out, saw %d POSTs: %v", n, seen)
	}
	if _, err := os.Stat(reportBackoffPath(cfg)); err == nil {
		t.Errorf("a delivered burst must clear the failure record at %s", reportBackoffPath(cfg))
	}

	// The discriminating part. The outage returns, and ONE fresh refusal must cost
	// one fresh 30s step — NOT a resumption of the old curve. Without the reset the
	// count would carry on at 5, demanding the 300s cap, and this recovered agent
	// would be reporting ten times slower than it should for no live reason.
	refuse = true
	if n := tick(480); n != 12 {
		t.Fatalf("the post-recovery tick must go out, saw %d POSTs: %v", n, seen)
	}
	if got := reportBackoffSecs(readReportBackoff(reportBackoffPath(cfg)).failures); got != 30 {
		t.Errorf("a fresh refusal after recovery must be failure #1 (30s), got %v", got)
	}
	if n := tick(510); n != 14 {
		t.Errorf("the retry 30s after a FIRST refusal must go out; saw %d POSTs: %v", n, seen)
	}
}

// TestHealthyCadenceIsUnchangedByTheFailureBackoff is the SENTINEL: the failure
// backoff must be invisible on the success path. A reporter the server keeps
// accepting reports at exactly one burst per 30s window — not slower (a backoff
// leaking into the healthy path), not denser — for the whole run.
func TestHealthyCadenceIsUnchangedByTheFailureBackoff(t *testing.T) {
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}

	attempts := driveTicks(t, srv.Client(), cfg, func() int { return len(*posts) }, 600.0, 0.5)
	want := []float64{0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330, 360, 390, 420, 450, 480, 510, 540, 570, 600}
	if !sameFloats(attempts, want) {
		t.Errorf("healthy bursts at %v, want %v", attempts, want)
	}
	// ...and a healthy reporter never even creates the failure record.
	if _, err := os.Stat(reportBackoffPath(cfg)); err == nil {
		t.Errorf("the success path must not write a backoff record at %s", reportBackoffPath(cfg))
	}
}

// TestAcceptedReportStillThrottlesNextTick is the SENTINEL for the fix above:
// binding the stamp to delivery must not degrade into "POST on every tick". A
// 2xx burst stamps the window exactly as before, so a tick 5s later is silent.
func TestAcceptedReportStillThrottlesNextTick(t *testing.T) {
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	payload := `{"context_window":{"used_percentage":50}}`

	var out, errOut bytes.Buffer
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 2000.0, strings.NewReader(payload), &out, &errOut)
	if len(*posts) != 2 {
		t.Fatalf("tick 1: expected both POSTs, got %v", *posts)
	}
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 2005.0, strings.NewReader(payload), &out, &errOut)
	if len(*posts) != 2 {
		t.Errorf("an ACCEPTED burst must still throttle the next tick, got %v", *posts)
	}
}

// TestRefusedTelemetryAloneLeavesWindowOpen: the two POSTs ride ONE window, so a
// burst counts as delivered only when EVERY attempted POST was accepted. Here the
// context POST is accepted and only the telemetry POST is refused — the exact
// shape of the outage this ticket came from (a schema refusal on the telemetry
// body alone) — and the window must stay open for the retry.
func TestRefusedTelemetryAloneLeavesWindowOpen(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/monitoring/telemetry" {
			w.WriteHeader(422)
			_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"nope"}}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	payload := `{"context_window":{"used_percentage":50}}`
	var out, errOut bytes.Buffer
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 2000.0, strings.NewReader(payload), &out, &errOut)
	if _, err := os.ReadFile(reportStampPath(cfg)); err == nil {
		t.Errorf("a partially refused burst must not stamp the window")
	}
	// The retry is BACKED OFF, not cancelled: silent at +5s, out at +30s (the first
	// backoff step, which is exactly the healthy window — a refusing reporter is
	// never denser than a healthy one).
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 2005.0, strings.NewReader(payload), &out, &errOut)
	if len(seen) != 2 {
		t.Errorf("the +5s tick must be held by the backoff; saw %v", seen)
	}
	cmdContextReport(srv.Client(), cfg, testEnv(nil), 2030.0, strings.NewReader(payload), &out, &errOut)
	if len(seen) != 4 {
		t.Errorf("a partial refusal must still retry once the backoff elapses; saw %v", seen)
	}
	if _, err := os.ReadFile(reportStampPath(cfg)); err == nil {
		t.Errorf("still partially refused ⇒ still no stamp")
	}
}
