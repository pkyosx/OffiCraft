package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type bufferWriteCloser struct{ bytes.Buffer }

func (b *bufferWriteCloser) Close() error { return nil }

func TestBuildCodexLaunchCommandKeepsTokenOutOfArgv(t *testing.T) {
	got := buildCodexLaunchCommand(
		"/opt/officraft/ocwarden",
		"/opt/homebrew/bin/codex",
		"/tmp/member-m-1",
		"/tmp/member-m-1/persona.md",
		"/tmp/member-m-1/.oc-token",
		"m-1",
		"http://127.0.0.1:7755",
		"member-m-1",
		"officraft-e2e",
		"",
		"high",
		nil,
		"",
	)
	for _, want := range []string{
		`OC_TOKEN="$(/bin/cat /tmp/member-m-1/.oc-token)"`,
		"exec /opt/officraft/ocwarden codex-session",
		"--codex-bin /opt/homebrew/bin/codex",
		"--effort high",
		"OC_ID=m-1",
		"OC_TMUX_SOCKET=officraft-e2e",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("launch command missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Bearer ") {
		t.Fatalf("launch argv must not carry credential material: %s", got)
	}
}

func TestNormalizeCodexEffort(t *testing.T) {
	for input, want := range map[string]string{
		"": "medium", "low": "low", "medium": "medium", "high": "high",
		// max is the level T-dbd4 added. It has to survive VERBATIM: this func
		// is an allowlist, not a ladder, so a level missing from it does not get
		// nudged down a notch — it lands in the same catch-all as a typo.
		"max":     "max",
		"extreme": "medium",
	} {
		if got := normalizeCodexEffort(input); got != want {
			t.Errorf("%q: got %q want %q", input, got, want)
		}
	}
}

func TestCodexPersonaInstructionPreservesBlankModelDefault(t *testing.T) {
	blank := codexPersonaInstruction("/private/persona.md", "")
	for _, want := range []string{
		"Read /private/persona.md completely",
		"machine's Codex default applies",
		"If your role's boot sequence calls report_waking",
		"omit its optional model argument",
		"never guess",
	} {
		if !strings.Contains(blank, want) {
			t.Fatalf("blank-model instruction missing %q: %s", want, blank)
		}
	}
	explicit := codexPersonaInstruction("/private/persona.md", "gpt-5.6-terra")
	if !strings.Contains(explicit, "explicit OffiCraft launch model is gpt-5.6-terra") ||
		!strings.Contains(explicit, "pass that exact value") ||
		!strings.Contains(explicit, "Follow your role-specific boot sequence") {
		t.Fatalf("explicit-model instruction must pin report_waking: %s", explicit)
	}
}

func TestRequestUserInputBridgeCreatesOneCardPerQuestion(t *testing.T) {
	var payloads []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer member-token" {
			t.Errorf("authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode card: %v", err)
		}
		payloads = append(payloads, payload)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rc-created"})
	}))
	defer server.Close()

	out := &bufferWriteCloser{}
	session := &codexSession{in: out, base: server.URL, token: "member-token"}
	session.handleServerRequest(appServerMessage{
		"id": "server-request-7", "method": "item/tool/requestUserInput",
		"params": map[string]any{"questions": []any{
			map[string]any{
				"id": "q1", "header": "Choose", "question": "Which path?",
				"options": []any{map[string]any{"label": "A"}, map[string]any{"label": "B"}},
			},
			map[string]any{
				"id": "q2", "header": "Credential", "question": "Paste the token",
				"isSecret": true,
			},
		}},
	})
	if len(payloads) != 2 {
		t.Fatalf("created %d cards, want one per question", len(payloads))
	}
	if payloads[0]["bind"] != "" || payloads[1]["bind"] != "none" {
		t.Fatalf("only the first card may auto-bind: %#v", payloads)
	}
	if payloads[1]["kind"] != "action" ||
		!strings.Contains(payloads[1]["body"].(string), "不要把秘密貼進卡片") {
		t.Fatalf("secret request must become a no-secret action card: %#v", payloads[1])
	}
	response, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(response), &decoded); err != nil {
		t.Fatalf("decode App Server response: %v (%s)", err, response)
	}
	if decoded["id"] != "server-request-7" {
		t.Fatalf("server request id was not echoed exactly: %#v", decoded)
	}
}

func TestReportTokenUsageUsesLatestTurnForContextGauge(t *testing.T) {
	var contextBody map[string]any
	var telemetryBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode %s: %v", r.URL.Path, err)
		}
		switch r.URL.Path {
		case "/api/agent/context":
			contextBody = body
		case "/api/monitoring/telemetry":
			telemetryBody = body
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	session := &codexSession{base: server.URL, token: "member-token", effort: "low"}
	session.reportTokenUsage(map[string]any{
		"tokenUsage": map[string]any{
			"modelContextWindow": float64(1000),
			"last":               map[string]any{"totalTokens": float64(250)},
			"total": map[string]any{
				"inputTokens": float64(1100), "cachedInputTokens": float64(700),
				"outputTokens": float64(50), "reasoningOutputTokens": float64(20),
				"totalTokens": float64(1150),
			},
		},
	})
	if got := contextBody["context_pct"]; got != float64(25) {
		t.Fatalf("context_pct = %#v, want latest-turn 25 (not cumulative 115)", got)
	}
	if got := contextBody["compaction_count"]; got != float64(0) {
		t.Fatalf("compaction_count = %#v, want 0 before any compaction", got)
	}
	tokens, _ := telemetryBody["tokens"].(map[string]any)
	if got := tokens["totalTokens"]; got != float64(1150) {
		t.Fatalf("telemetry totalTokens = %#v, want cumulative thread total", got)
	}
}

func TestCodexPostsRecordRejectedResponsesWithoutChangingControlFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	var activity bytes.Buffer
	session := &codexSession{base: server.URL, token: "member-token", out: &activity}
	session.post("/api/monitoring/telemetry", map[string]any{"runtime": "codex"})
	if got := session.openReplyCard(map[string]any{"kind": "decision", "header": "test"}, ""); got != "" {
		t.Fatalf("rejected card id = %q, want empty", got)
	}
	for _, want := range []string{
		"Codex POST /api/monitoring/telemetry rejected with HTTP 422",
		"Codex POST /api/reply-cards rejected with HTTP 422",
	} {
		if !strings.Contains(activity.String(), want) {
			t.Fatalf("missing rejection activity %q in %q", want, activity.String())
		}
	}
}

// TestReportTokenUsageSendsSessionModel pins the codex half of the reported-model
// telemetry. This sidecar is the only thing on the codex path that knows which
// model the session is running, so without it the cockpit's 模型 column has no
// reported value for ANY codex session — and since that column no longer falls
// back to the configured launch model, "no reporter" now means "blank forever".
//
// The blank case is the load-bearing one: an empty s.model means the OffiCraft
// launch model was unset and the machine's own Codex default is in force, i.e.
// the name is genuinely unknown. It must be OMITTED, because sending "" would
// record that unknown as a reported blank — the exact "measured" vs "never
// measured" collapse the field exists to end.
//
// ⚠️ COVERAGE NOTE, same caveat as the ocagent twin: only the first case could
// have failed before this change — the sidecar sent no `model` key at all, so
// both `want: nil` cases were vacuously true. They guard omit-vs-blank within
// the new design (which the server's stamp guard relies on), not the fact that
// the field is sent.
func TestReportTokenUsageSendsSessionModel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model string
		want  any
	}{
		{name: "configured model is reported", model: "gpt-5-codex", want: "gpt-5-codex"},
		{name: "blank is omitted, never a reported blank", model: "", want: nil},
		{name: "whitespace is not a model name", model: "   ", want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var telemetryBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode %s: %v", r.URL.Path, err)
				}
				if r.URL.Path == "/api/monitoring/telemetry" {
					telemetryBody = body
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			session := &codexSession{base: server.URL, token: "member-token",
				effort: "low", model: tc.model}
			session.reportTokenUsage(map[string]any{
				"tokenUsage": map[string]any{
					"modelContextWindow": float64(1000),
					"last":               map[string]any{"totalTokens": float64(250)},
					"total":              map[string]any{"totalTokens": float64(1150)},
				},
			})
			if telemetryBody == nil {
				t.Fatalf("no telemetry POST")
			}
			if got := telemetryBody["model"]; got != tc.want {
				t.Errorf("telemetry model = %#v, want %#v; body=%#v",
					got, tc.want, telemetryBody)
			}
		})
	}
}

func TestRecordCompactionCountsOnlyContextCompactionItems(t *testing.T) {
	session := &codexSession{}
	session.recordCompaction(map[string]any{"item": map[string]any{"type": "agentMessage"}})
	session.recordCompaction(map[string]any{"item": map[string]any{"type": "contextCompaction", "id": "compact-1"}})
	session.recordCompaction(map[string]any{"item": map[string]any{"type": "contextCompaction", "id": "compact-1"}})
	session.recordCompaction(map[string]any{"item": map[string]any{"type": "contextCompaction"}})
	if session.compactions != 1 {
		t.Fatalf("compactions = %d, want 1 unique item", session.compactions)
	}
}

func TestActionableCodexListenerLineFiltersTransportDiagnostics(t *testing.T) {
	for line, want := range map[string]bool{
		"[ocagent] listen: connected — streaming http://127.0.0.1": false,
		"[ocagent] listen: stream ended: EOF":                      false,
		"[ocagent] chat from owner (id, 1s ago): hello":            true,
		"[ocagent] task T-1 updated · by owner":                    true,
	} {
		if got := actionableCodexListenerLine(line); got != want {
			t.Errorf("%q: got %v want %v", line, got, want)
		}
	}
}

type runtimeProbeRunner struct{}

func (runtimeProbeRunner) Run(name string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.HasSuffix(name, "codex") && joined == "--version":
		return "codex-cli 0.145.0", nil
	case strings.HasSuffix(name, "codex") && joined == "login status":
		return "Logged in", nil
	default:
		return "", errors.New("unexpected")
	}
}

func TestRuntimeCapabilitiesShape(t *testing.T) {
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := func(key string) string {
		switch key {
		case "OC_CODEX_BIN":
			return codexPath
		case "HOME":
			return "/tmp"
		default:
			return ""
		}
	}
	got := collectRuntimeCapabilities(env, runtimeProbeRunner{}, map[string]any{})
	codex := got["codex"].(map[string]any)
	if installed, _ := codex["installed"].(bool); !installed {
		t.Fatalf("executable Codex override must report installed: %#v", codex)
	}
	if loggedIn, _ := codex["logged_in"].(bool); !loggedIn {
		t.Fatalf("successful login probe must report logged in: %#v", codex)
	}
	if codex["version"] != "0.145.0" {
		t.Fatalf("unexpected Codex version capability: %#v", codex)
	}
}

// ---------------------------------------------------------------------------
// codexAccountKey: the monitoring key must identify the PERSON, not the
// ChatGPT workspace. Every fixture below is obviously synthetic; these tests
// must never touch the real ~/.codex/auth.json, which holds live credentials.
// ---------------------------------------------------------------------------

// fakeIDToken builds an unsigned, obviously-fake JWT. The signature segment is
// a literal placeholder: nothing in the production path verifies it.
func fakeIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(header) + "." + enc.EncodeToString(payload) + ".not-a-real-signature"
}

// codexClaims is the shape of a real Codex id_token payload, with fake values.
func codexClaims(chatgptUserID, workspaceID, email string) map[string]any {
	return map[string]any{
		"sub":   "google-oauth2|000000000000000000000",
		"email": email,
		"name":  "Fake Person",
		"sid":   "session-fake",
		"jti":   "jti-fake",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_user_id":    chatgptUserID,
			"user_id":            chatgptUserID,
			"chatgpt_account_id": workspaceID,
			"chatgpt_plan_type":  "pro",
			"organizations":      []any{map[string]any{"id": workspaceID, "is_default": true}},
			"groups":             []any{},
		},
	}
}

// writeCodexHome lays out a fake machine's ~/.codex/auth.json and returns the
// home directory to feed codexAccountKeyForHome.
func writeCodexHome(t *testing.T, auth map[string]any) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	raw, err := json.Marshal(auth)
	if err != nil {
		t.Fatalf("marshal auth.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), raw, 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	return home
}

func codexAuthFile(idToken, workspaceID string) map[string]any {
	return map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{
			"id_token":      idToken,
			"access_token":  "fake-access-token",
			"refresh_token": "fake-refresh-token",
			"account_id":    workspaceID,
		},
		"last_refresh": "2026-07-27T00:00:00Z",
	}
}

// SENTINEL 1. This is the defect: two people sharing one ChatGPT workspace used
// to hash to the same key, so their spend was summed into one row and their
// separate 5h/7d quotas overwrote each other. Reverting codexAccountKeyForHome
// to hash tokens.account_id turns this test red.
func TestCodexAccountKeyDistinguishesTwoPeopleInOneWorkspace(t *testing.T) {
	const workspace = "11111111-2222-3333-4444-555555555555"
	sethHome := writeCodexHome(t, codexAuthFile(
		fakeIDToken(t, codexClaims("user_FAKE00000000000000000001", workspace, "fake.seth@example.invalid")),
		workspace,
	))
	evaHome := writeCodexHome(t, codexAuthFile(
		fakeIDToken(t, codexClaims("user_FAKE00000000000000000002", workspace, "fake.eva@example.invalid")),
		workspace,
	))
	seth := codexAccountKeyForHome(sethHome)
	eva := codexAccountKeyForHome(evaHome)
	if seth == "" || eva == "" {
		t.Fatalf("both machines must produce a key: seth=%q eva=%q", seth, eva)
	}
	if seth == eva {
		t.Fatalf("two ChatGPT users in one workspace must not share a monitoring key: %q", seth)
	}
}

// SENTINEL 2. The property v1 did get right must survive: one person, two
// machines, one key. The two fixtures differ in everything that is per-machine
// or per-refresh (access/refresh tokens, session and token ids, issue times,
// last_refresh) so the assertion is not merely comparing identical inputs.
//
// Read this before deleting it: this test has ZERO discriminating power against
// the workspace-vs-person defect — it stays green under the v1 mutant, because
// v1 got cross-machine convergence right. Keep it anyway. What it guards is the
// opposite regression class: someone later re-pointing the hash at a field that
// churns (sid, jti, iat, at_hash, access_token, last_refresh — all of which this
// fixture varies), which would silently fork one human into a new monitoring
// account on every token refresh. SENTINEL 1 is the bug sentinel; this is the
// don't-break-it-back-the-other-way sentinel.
func TestCodexAccountKeyIsStableForOnePersonAcrossMachines(t *testing.T) {
	const workspace = "11111111-2222-3333-4444-555555555555"
	const person = "user_FAKE00000000000000000001"

	machineA := codexClaims(person, workspace, "fake.seth@example.invalid")
	machineA["sid"] = "session-machine-a"
	machineA["jti"] = "jti-machine-a"
	machineA["iat"] = 1000

	machineB := codexClaims(person, workspace, "fake.seth@example.invalid")
	machineB["sid"] = "session-machine-b"
	machineB["jti"] = "jti-machine-b"
	machineB["iat"] = 2000

	authA := codexAuthFile(fakeIDToken(t, machineA), workspace)
	authB := codexAuthFile(fakeIDToken(t, machineB), workspace)
	authB["tokens"].(map[string]any)["access_token"] = "another-fake-access-token"
	authB["tokens"].(map[string]any)["refresh_token"] = "another-fake-refresh-token"
	authB["last_refresh"] = "2026-07-28T09:30:00Z"

	keyA := codexAccountKeyForHome(writeCodexHome(t, authA))
	keyB := codexAccountKeyForHome(writeCodexHome(t, authB))
	if keyA == "" {
		t.Fatalf("machine A produced no key")
	}
	if keyA != keyB {
		t.Fatalf("one person on two machines must share one key: %q vs %q", keyA, keyB)
	}
}

// The person's key must not move when workspace-scoped attributes change,
// otherwise a workspace switch silently forks one human's usage history.
func TestCodexAccountKeyIgnoresWorkspaceScopedFields(t *testing.T) {
	const person = "user_FAKE00000000000000000001"
	before := codexAccountKeyForHome(writeCodexHome(t, codexAuthFile(
		fakeIDToken(t, codexClaims(person, "11111111-2222-3333-4444-555555555555", "fake.seth@example.invalid")),
		"11111111-2222-3333-4444-555555555555",
	)))
	moved := codexClaims(person, "99999999-8888-7777-6666-555555555555", "fake.seth@example.invalid")
	moved["https://api.openai.com/auth"].(map[string]any)["chatgpt_plan_type"] = "business"
	after := codexAccountKeyForHome(writeCodexHome(t, codexAuthFile(
		fakeIDToken(t, moved), "99999999-8888-7777-6666-555555555555",
	)))
	if before == "" {
		t.Fatalf("baseline produced no key")
	}
	if before != after {
		t.Fatalf("workspace change must not fork the person's key: %q vs %q", before, after)
	}
}

// The key is an opaque, versioned sha256 of exactly one claim. The expected
// digest is a literal rather than a recomputation of the production constant,
// so bumping the version prefix or changing which claim is hashed goes red
// here instead of passing by agreeing with itself.
func TestCodexAccountKeyIsAVersionedOpaqueDigest(t *testing.T) {
	const person = "user_FAKE00000000000000000001"
	const email = "fake.seth@example.invalid"
	got := codexAccountKeyForHome(writeCodexHome(t, codexAuthFile(
		fakeIDToken(t, codexClaims(person, "11111111-2222-3333-4444-555555555555", email)),
		"11111111-2222-3333-4444-555555555555",
	)))
	want := "codex:6e0629091bd61838068baf0b6a6790720345902dee7bf2ab19bd062acf099ed0"
	if got != want {
		t.Fatalf("account key digest changed: got %q want %q", got, want)
	}
	for _, leak := range []string{person, email, "11111111", "fake-access-token", "fake-refresh-token"} {
		if strings.Contains(got, leak) {
			t.Fatalf("key must not carry %q in cleartext: %q", leak, got)
		}
	}
}

// Degradation: anything that leaves us without the personal claim yields the
// empty string ("this machine has no identifiable Codex account"), never a
// fallback to the workspace id. Each case is paired with a control that proves
// the assertion is not vacuous.
func TestCodexAccountKeyDegradesToEmptyRatherThanTheWorkspaceID(t *testing.T) {
	const workspace = "11111111-2222-3333-4444-555555555555"
	workspaceKeySum := sha256.Sum256([]byte("officraft-codex-account-v1:" + workspace))
	v1Key := "codex:" + fmt.Sprintf("%x", workspaceKeySum[:])

	noClaim := codexClaims("user_FAKE00000000000000000001", workspace, "fake.seth@example.invalid")
	delete(noClaim["https://api.openai.com/auth"].(map[string]any), "chatgpt_user_id")
	delete(noClaim["https://api.openai.com/auth"].(map[string]any), "user_id")

	blankClaim := codexClaims("   ", workspace, "fake.seth@example.invalid")

	noCustomClaim := map[string]any{"sub": "google-oauth2|0", "email": "fake@example.invalid"}

	cases := []struct {
		name string
		auth map[string]any
	}{
		{"claim absent", codexAuthFile(fakeIDToken(t, noClaim), workspace)},
		{"claim blank", codexAuthFile(fakeIDToken(t, blankClaim), workspace)},
		{"custom claim namespace absent", codexAuthFile(fakeIDToken(t, noCustomClaim), workspace)},
		{"id_token absent", codexAuthFile("", workspace)},
		{"id_token not a JWT", codexAuthFile("this-is-not-a-jwt", workspace)},
		{"id_token wrong segment count", codexAuthFile("aaa.bbb", workspace)},
		{"id_token payload not base64url", codexAuthFile("aaa.!!!not-base64!!!.ccc", workspace)},
		{"id_token payload not JSON", codexAuthFile(
			"aaa."+base64.RawURLEncoding.EncodeToString([]byte("plain text, not json"))+".ccc", workspace)},
		{"id_token payload is a JSON array", codexAuthFile(
			"aaa."+base64.RawURLEncoding.EncodeToString([]byte(`["nope"]`))+".ccc", workspace)},
		// JSON null unmarshals into a struct without error and leaves it zeroed;
		// only the explicit empty-claim check keeps this from producing a key
		// over an empty string.
		{"id_token payload is JSON null", codexAuthFile(
			"aaa."+base64.RawURLEncoding.EncodeToString([]byte(`null`))+".ccc", workspace)},
		// Wrong JSON types for the claim and for its namespace: both must fail
		// closed rather than coerce.
		{"chatgpt_user_id is a number", codexAuthFile(
			"aaa."+base64.RawURLEncoding.EncodeToString(
				[]byte(`{"https://api.openai.com/auth":{"chatgpt_user_id":12345}}`))+".ccc", workspace)},
		{"custom claim namespace is a string", codexAuthFile(
			"aaa."+base64.RawURLEncoding.EncodeToString(
				[]byte(`{"https://api.openai.com/auth":"user_FAKE00000000000000000001"}`))+".ccc", workspace)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := codexAccountKeyForHome(writeCodexHome(t, tc.auth))
			if got == v1Key {
				t.Fatalf("degraded path fell back to the workspace id: %q", got)
			}
			if got != "" {
				t.Fatalf("degraded path must report no account, got %q", got)
			}
		})
	}

	// Control: the same fixture shape WITH the claim present is not empty, so
	// the assertions above are discriminating rather than always-true.
	ok := codexAccountKeyForHome(writeCodexHome(t, codexAuthFile(
		fakeIDToken(t, codexClaims("user_FAKE00000000000000000001", workspace, "fake.seth@example.invalid")),
		workspace,
	)))
	if ok == "" {
		t.Fatalf("control fixture must produce a key, otherwise the empty-string assertions prove nothing")
	}
	if ok == v1Key {
		t.Fatalf("control fixture must not equal the v1 workspace key")
	}
}

// The auth.json container itself can be missing or unusable. Same rule: empty,
// and never a read of some other home directory.
func TestCodexAccountKeyHandlesMissingOrUnreadableAuthFile(t *testing.T) {
	if got := codexAccountKeyForHome(t.TempDir()); got != "" {
		t.Fatalf("absent ~/.codex/auth.json must yield no key, got %q", got)
	}

	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := codexAccountKeyForHome(home); got != "" {
		t.Fatalf("unparsable auth.json must yield no key, got %q", got)
	}

	// A directory where auth.json should be: read fails, not panics.
	home2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home2, ".codex", "auth.json"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := codexAccountKeyForHome(home2); got != "" {
		t.Fatalf("unreadable auth.json must yield no key, got %q", got)
	}
}
