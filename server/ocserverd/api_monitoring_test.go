package main

// api_monitoring_test.go — foldCommandResult unit coverage: the durable
// last_op* fold of one warden command_result receipt, focused on the
// last_op_reason field (成員啟動失敗原因全鏈可見: the warden's structured
// "<code>: <detail>" refusal cause must survive the fold verbatim, clamp at
// the reason cap, and stay honest-empty for an old-warden receipt that
// carries no reason).

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// foldTestServer is the minimal apiServer a foldCommandResult call needs:
// a real (temp-SQLite) DAL plus a live hub (putMember publishes a member
// delta on every fold).
func foldTestServer(t *testing.T) *apiServer {
	t.Helper()
	return &apiServer{dal: newTestDAL(t), hub: NewHub()}
}

// doIngestTelemetry drives POST /api/monitoring/telemetry with agent-scope
// claims for sub (machine_id claim included when machineClaim != "").
func doIngestTelemetry(api *apiServer, sub, machineClaim, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/monitoring/telemetry", strings.NewReader(body))
	claims := map[string]any{"sub": sub, "scope": "agent"}
	if machineClaim != "" {
		claims["machine_id"] = machineClaim
	}
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleIngestTelemetryApiMonitoringTelemetryPost(rec, req)
	return rec
}

func TestHandleIngestTelemetry_MachineClaimOverridesSelfReport(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "m-claimed",
		`{"machine": "m-self-reported", "hardware": {"cpu_pct": 1}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	if got, _ := entry["machine"].(string); got != "m-claimed" {
		t.Fatalf("machine must come from the token claim, got %q", got)
	}
}

func TestHandleIngestTelemetry_NoClaimFallsBackToSelfReport(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "",
		`{"machine": "m-self-reported", "hardware": {"cpu_pct": 1}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	if got, _ := entry["machine"].(string); got != "m-self-reported" {
		t.Fatalf("without a machine_id claim the self-report must fold, got %q", got)
	}
}

func TestHandleIngestTelemetry_ClaimFoldsWithoutSelfReport(t *testing.T) {
	// A claim-bearing token attributes the entry even when the payload carries
	// no machine at all.
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "m-claimed", `{"hardware": {"cpu_pct": 1}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	if got, _ := entry["machine"].(string); got != "m-claimed" {
		t.Fatalf("machine must fold from the claim alone, got %q", got)
	}
}

func TestHandleIngestTelemetry_BinariesFingerprintsFoldAndEcho(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	// A binaries-only heartbeat is a valid telemetry POST (first-class field),
	// and the fingerprints fold onto the entry + echo back.
	rec := doIngestTelemetry(api, "m-1", "m-1",
		`{"binaries": {"ocwarden": "aaa111", "ocagent": "bbb222"}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	bins, _ := entry["binaries"].(map[string]any)
	if bins["ocwarden"] != "aaa111" || bins["ocagent"] != "bbb222" {
		t.Fatalf("binaries fold = %v, want the reported fingerprints", bins)
	}
	if !strings.Contains(rec.Body.String(), `"ocwarden":"aaa111"`) {
		t.Fatalf("echo must carry binaries: %s", rec.Body.String())
	}
	// A later hardware-only heartbeat must not clobber the stored fingerprints.
	if rec := doIngestTelemetry(api, "m-1", "m-1", `{"hardware": {"cpu_pct": 1}}`); rec.Code != 200 {
		t.Fatalf("second ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry = api.telemetry.Get("m-1")
	if bins, _ := entry["binaries"].(map[string]any); bins["ocwarden"] != "aaa111" {
		t.Fatalf("binaries must survive a binaries-less report, got %v", entry["binaries"])
	}
	// A non-object binaries is the flat 400 every other object field gets.
	if rec := doIngestTelemetry(api, "m-2", "m-2", `{"binaries": "not-an-object"}`); rec.Code != 400 {
		t.Fatalf("non-object binaries: %d, want 400", rec.Code)
	}
}

func TestHandleIngestTelemetry_ClaudeProbeFoldAndEcho(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	// A claude-only heartbeat is a valid telemetry POST (first-class field),
	// and the probe folds onto the entry + echoes back (T-97ee).
	rec := doIngestTelemetry(api, "m-1", "m-1",
		`{"claude": {"version": "2.1.211", "cred_file": true, "sub_readable": false, "keychain": true}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	probe, _ := entry["claude"].(map[string]any)
	if probe["version"] != "2.1.211" || probe["cred_file"] != true ||
		probe["sub_readable"] != false || probe["keychain"] != true {
		t.Fatalf("claude fold = %v, want the reported probe", probe)
	}
	if !strings.Contains(rec.Body.String(), `"version":"2.1.211"`) {
		t.Fatalf("echo must carry claude: %s", rec.Body.String())
	}
	// A later hardware-only heartbeat must not clobber the stored probe.
	if rec := doIngestTelemetry(api, "m-1", "m-1", `{"hardware": {"cpu_pct": 1}}`); rec.Code != 200 {
		t.Fatalf("second ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry = api.telemetry.Get("m-1")
	if probe, _ := entry["claude"].(map[string]any); probe["version"] != "2.1.211" {
		t.Fatalf("claude must survive a claude-less report, got %v", entry["claude"])
	}
	// A non-object claude is the flat 400 every other object field gets.
	if rec := doIngestTelemetry(api, "m-2", "m-2", `{"claude": "not-an-object"}`); rec.Code != 400 {
		t.Fatalf("non-object claude: %d, want 400", rec.Code)
	}
}

// ── account_label (T-260e): human-readable account default display ──────────

const teleWithLabel = `{"hardware": {"cpu_pct": 1},
	"account": "acct-123/team",
	"account_label": "eva.cheng@gofreight.com(GoFreight)"}`

// labelTestServer seeds one active member ("mira", no admin role so an
// agent-scope GET resolves as a plain agent) and ingests one telemetry report
// carrying both the opaque account key and the human-readable account_label.
func labelTestServer(t *testing.T) *apiServer {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rec := doIngestTelemetry(s, "mira", "m-abc123", teleWithLabel)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	return s
}

// doGetMonitoring drives GET /api/monitoring with the given verified claims.
func doGetMonitoring(api *apiServer, claims map[string]any) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/monitoring", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleGetMonitoringApiMonitoringGet(rec, req)
	return rec
}

func monitoringOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("GET /api/monitoring: %d %s", rec.Code, rec.Body.String())
	}
	var d map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("body not JSON: %s", rec.Body.String())
	}
	return d
}

func TestHandleIngestTelemetry_AccountLabelFolds(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "", teleWithLabel)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	if got, _ := entry["account_label"].(string); got != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("account_label must fold into the entry, got %q", got)
	}
	// PRIVACY: the ingest echo (agent-readable) must NOT mint an account_label
	// wire field — the label only ever surfaces on the owner-facing fold.
	if strings.Contains(rec.Body.String(), "account_label") {
		t.Fatalf("ingest echo must not carry account_label: %s", rec.Body.String())
	}
}

func TestGetMonitoring_OwnerSeesLabelAsDefaultDisplayName(t *testing.T) {
	s := labelTestServer(t)
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	accounts := d["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("accounts = %v, want 1 row", accounts)
	}
	row := accounts[0].(map[string]any)
	if row["account"] != "acct-123/team" {
		t.Fatalf("account key must stay the stable tag, got %v", row["account"])
	}
	if row["display_name"] != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("owner default display must be the reported label, got %v", row["display_name"])
	}
	// The session row's account column resolves the same way for the owner.
	sessions := d["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %v, want 1 row", sessions)
	}
	if got := sessions[0].(map[string]any)["account"]; got != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("owner session account = %v, want the label", got)
	}
}

func TestGetMonitoring_SameAccountKeyFoldsIntoOneRow(t *testing.T) {
	// REGRESSION (T-f694): the accounts fold is a pure string aggregation — two
	// members reporting the SAME account key (e.g. the same uid/org account on a
	// file-creds machine and a Keychain-only machine, now that the plan no
	// longer joins the key) must fold into ONE row with the costs summed.
	s := labelTestServer(t)
	m := fullMember("joey")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rec := doIngestTelemetry(s, "joey", "m-other",
		`{"hardware": {"cpu_pct": 2}, "cost": 1.5, "account": "acct-123/team"}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	accounts := d["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("accounts = %v, want the two members' identical keys folded into 1 row", accounts)
	}
	if got := accounts[0].(map[string]any)["account"]; got != "acct-123/team" {
		t.Fatalf("account = %v, want acct-123/team", got)
	}
}

func TestGetMonitoring_OwnerAliasWinsOverLabel(t *testing.T) {
	// 不覆蓋: a display name the owner set by hand ALWAYS beats the reported label.
	s := labelTestServer(t)
	if err := s.dal.PutAccountAlias(AccountAlias{
		Account: "acct-123/team", DisplayName: "Eva 的 Team 帳號"}); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "Eva 的 Team 帳號" {
		t.Fatalf("owner alias must win over the reported label, got %v", row["display_name"])
	}
}

func TestGetMonitoring_AgentNeverSeesLabel(t *testing.T) {
	// PRIVACY: the email-bearing label is owner-facing ONLY. An agent-principal
	// GET /api/monitoring (same route, lower rank) sees the raw stable key and
	// the response body must not contain the label/email anywhere.
	s := labelTestServer(t)
	rec := doGetMonitoring(s, map[string]any{"sub": "mira", "scope": "agent"})
	d := monitoringOf(t, rec)
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "acct-123/team" {
		t.Fatalf("agent-facing display must fall back to the raw key, got %v", row["display_name"])
	}
	if strings.Contains(rec.Body.String(), "eva.cheng@gofreight.com") ||
		strings.Contains(rec.Body.String(), "GoFreight") {
		t.Fatalf("agent-facing monitoring leaked the label: %s", rec.Body.String())
	}
}

func TestGetMonitoring_AgentStillSeesOwnerAlias(t *testing.T) {
	// The owner-set alias is a deliberate, non-PII display overlay — it stays
	// visible at every rank (pre-existing behaviour, unchanged by T-260e).
	s := labelTestServer(t)
	if err := s.dal.PutAccountAlias(AccountAlias{
		Account: "acct-123/team", DisplayName: "Team 帳號"}); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "mira", "scope": "agent"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "Team 帳號" {
		t.Fatalf("agent-facing display must still resolve the owner alias, got %v", row["display_name"])
	}
}

// ── account_label passthrough field (T-a9a7): raw label survives aliasing ───

func TestGetMonitoring_OwnerAccountRowCarriesLabelEvenWithAlias(t *testing.T) {
	// The account row must expose the reporter-supplied label VERBATIM in the
	// dedicated account_label field, and — the whole point of the field — the
	// label must STILL be there after the owner sets an alias (display_name
	// switches to the alias; account_label keeps the real identity).
	s := labelTestServer(t)
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["account_label"] != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("owner account_label = %v, want the raw reported label", row["account_label"])
	}
	if err := s.dal.PutAccountAlias(AccountAlias{
		Account: "acct-123/team", DisplayName: "Eva 的 Team 帳號"}); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	d = monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row = d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "Eva 的 Team 帳號" {
		t.Fatalf("alias must stay the display, got %v", row["display_name"])
	}
	if row["account_label"] != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("account_label must survive the alias, got %v", row["account_label"])
	}
}

func TestGetMonitoring_AgentNeverSeesAccountLabelField(t *testing.T) {
	// PRIVACY GATE: the account_label field is owner-facing ONLY. For an
	// agent-principal GET the KEY ITSELF must be absent from the wire body
	// (omitempty on an empty overlay), not just empty.
	s := labelTestServer(t)
	rec := doGetMonitoring(s, map[string]any{"sub": "mira", "scope": "agent"})
	d := monitoringOf(t, rec)
	row := d["accounts"].([]any)[0].(map[string]any)
	if _, present := row["account_label"]; present {
		t.Fatalf("agent-facing account row must not carry account_label: %v", row)
	}
	if strings.Contains(rec.Body.String(), "account_label") {
		t.Fatalf("agent-facing monitoring body must not mention account_label: %s", rec.Body.String())
	}
}

func TestGetMonitoring_SessionAccountNeverServesRawKey(t *testing.T) {
	// T-ba6b: the session row's account column feeds the member detail panel's
	// Claude Account cell — with NO readable name (no alias, no label) it must
	// serve "" (the panel's dash), NEVER the raw credential key. The accounts
	// row keeps its raw-key display_name fallback (it is the aliasing surface).
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"hardware": {"cpu_pct": 1}, "account": "acct-123/team"}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "" {
		t.Fatalf("unresolvable session account = %v, want \"\"", got)
	}
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "acct-123/team" {
		t.Fatalf("accounts-row display_name keeps the raw-key fallback, got %v", row["display_name"])
	}
}

func TestGetMonitoring_WorkerReportedLabelResolvesSessionAccount(t *testing.T) {
	// T-ba6b (recon §6-4/§6-6): the label overlay scans the WHOLE telemetry
	// snapshot, so an account_label reported by an OUTSOURCE-WORKER session
	// resolves a member session on the same account (the old fold scanned only
	// roster members and left the raw key).
	s := labelTestServer(t)
	// Strip the member's own label; keep only the account key.
	s.telemetry.Set("mira", map[string]any{"account": "acct-123/team"})
	// A worker entry (NOT a roster member) reports the label for the same key.
	s.telemetry.Set("ow-1", map[string]any{
		"account": "acct-123/team", "account_label": "eva@corp(Corp)", "ts": 99.0})
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "eva@corp(Corp)" {
		t.Fatalf("worker-reported label must resolve the session account, got %v", got)
	}
}

func TestGetMonitoring_NoLabelReportedOmitsAccountLabel(t *testing.T) {
	// Honest-absent: telemetry that carries only the opaque account key (no
	// account_label) yields an owner-facing row WITHOUT the key — never "".
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"hardware": {"cpu_pct": 1}, "account": "acct-123/team"}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if _, present := row["account_label"]; present {
		t.Fatalf("label-less report must omit account_label, got %v", row)
	}
}

// TestFoldCommandResult_WorkerReceiptFoldsOntoWorkerRow (T-9ccf): a receipt
// keyed on worker_id (a worker has NO roster member) must fold the last-op
// fields onto the durable outsource_worker row — the worker twin of the member
// fold, and the server half of the O-19 visibility fix.
func TestFoldCommandResult_WorkerReceiptFoldsOntoWorkerRow(t *testing.T) {
	s := foldTestServer(t)
	w := OutsourceWorker{ID: "ow-1", Codename: "O-7", Model: "opus", Effort: "high",
		TaskID: "t-1", Status: WorkerStatusAssigned, CreatedTS: 100}
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	reason := `session_already_exists: tmux session "worker-ow-1" is already live (clobber-guard refused to stomp it)`
	s.foldCommandResult(map[string]any{
		"worker_id": "ow-1",
		"rpc":       "worker_start",
		"ok":        false,
		"reason":    reason,
		"log":       reason,
		"at":        "2026-07-13T08:00:00Z",
	}, "w-test")

	got, err := s.dal.GetOutsourceWorker("ow-1")
	if err != nil || got == nil {
		t.Fatalf("get worker: %v %v", got, err)
	}
	if got.LastOp != "worker_start" || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("fold must record a failed worker_start, got %+v", got)
	}
	if got.LastOpReason != reason {
		t.Fatalf("worker last_op_reason must persist verbatim:\n got %q\nwant %q", got.LastOpReason, reason)
	}
	if got.LastOpAt == 0 {
		t.Fatalf("worker last_op_at must be stamped, got 0")
	}
	// The fold must NOT disturb lifecycle columns.
	if got.Status != WorkerStatusAssigned || got.Codename != "O-7" {
		t.Fatalf("fold must leave lifecycle untouched, got %+v", got)
	}
}

// TestFoldCommandResult_WorkerReceiptUnknownWorkerIgnored: a worker receipt for
// an unknown worker id is a safe no-op (never a panic / 500), mirroring the
// unknown-member branch.
func TestFoldCommandResult_WorkerReceiptUnknownWorkerIgnored(t *testing.T) {
	s := foldTestServer(t)
	s.foldCommandResult(map[string]any{
		"worker_id": "ow-nope", "rpc": "worker_start", "ok": true,
	}, "w-test")
}

func TestFoldCommandResult_ReasonPersistedVerbatim(t *testing.T) {
	s := foldTestServer(t)
	m := fullMember("mira")
	m.LastOp, m.LastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt = "", nil, "", "", 0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	reason := `session_already_exists: tmux session "member-mira" is already live (clobber-guard refused to stomp it)`
	s.foldCommandResult(map[string]any{
		"member_id": "mira",
		"rpc":       "start",
		"ok":        false,
		"reason":    reason,
		"log":       reason,
		"at":        "2026-07-13T08:00:00Z",
	}, "w-test")

	got, err := s.dal.GetMember("mira")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.LastOp != "start" || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("fold must record a failed start, got %+v", got)
	}
	if got.LastOpReason != reason {
		t.Fatalf("last_op_reason must persist verbatim:\n got %q\nwant %q", got.LastOpReason, reason)
	}
}

func TestFoldCommandResult_ReasonClampedAtCap(t *testing.T) {
	s := foldTestServer(t)
	if err := s.dal.PutMember(fullMember("mira")); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	long := "mkdir_failed: " + strings.Repeat("x", 2*commandResultReasonMax)
	s.foldCommandResult(map[string]any{
		"member_id": "mira", "rpc": "start", "ok": false, "reason": long,
	}, "w-test")
	got, _ := s.dal.GetMember("mira")
	if len(got.LastOpReason) != commandResultReasonMax {
		t.Fatalf("reason must clamp to %d bytes, got %d", commandResultReasonMax, len(got.LastOpReason))
	}
	if !strings.HasPrefix(got.LastOpReason, "mkdir_failed: ") {
		t.Fatalf("clamp must keep the head (the structured code), got %q", got.LastOpReason[:32])
	}
}

func TestFoldCommandResult_NoReasonFoldsEmpty(t *testing.T) {
	// Old-warden compat: a receipt WITHOUT a reason key must fold "" — and
	// OVERWRITE any stale prior reason (the reason always describes THIS op).
	s := foldTestServer(t)
	m := fullMember("mira")
	m.LastOpReason = "spawn_exec_failed: stale prior cause"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "mira", "rpc": "stop", "ok": true, "log": "session=member-mira: stopped",
	}, "w-test")
	got, _ := s.dal.GetMember("mira")
	if got.LastOpReason != "" {
		t.Fatalf("a reason-less receipt must fold an empty reason, got %q", got.LastOpReason)
	}
	if got.LastOp != "stop" || got.LastOpLog == "" {
		t.Fatalf("the rest of the fold must be untouched, got %+v", got)
	}
}
