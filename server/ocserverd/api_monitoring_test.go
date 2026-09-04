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
	"fmt"
	"net/http/httptest"
	"reflect"
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

func TestHandleIngestTelemetry_RuntimeCapabilities(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	if !api.machineSupportsRuntime("legacy-warden", RuntimeClaude) ||
		api.machineSupportsRuntime("legacy-warden", RuntimeCodex) {
		t.Fatal("an absent capability map must preserve only legacy Claude placement")
	}
	rec := doIngestTelemetry(api, "m-box", "m-box",
		`{"runtimes":{"claude":{"installed":true,"logged_in":true,"version":"2.1.211"},"codex":{"installed":true,"logged_in":false,"version":"0.145.0"}}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	if !api.machineSupportsRuntime("m-box", RuntimeClaude) {
		t.Fatal("logged-in Claude capability must be eligible")
	}
	if api.machineSupportsRuntime("m-box", RuntimeCodex) {
		t.Fatal("logged-out Codex capability must be ineligible")
	}
	caps := api.machineRuntimeCapabilities("m-box")
	if got := caps[RuntimeCodex].Version; got == nil || *got != "0.145.0" {
		t.Fatalf("Codex version did not round-trip: %#v", caps)
	}
	codexOnly := doIngestTelemetry(api, "m-box", "m-box",
		`{"runtimes":{"codex":{"installed":true,"logged_in":true}}}`)
	if codexOnly.Code != 200 || api.machineSupportsRuntime("m-box", RuntimeClaude) {
		t.Fatal("once a map is reported, a missing Claude entry must fail closed")
	}

	bad := doIngestTelemetry(api, "m-box", "m-box",
		`{"runtimes":{"codex":{"installed":"yes"}}}`)
	if bad.Code != 400 {
		t.Fatalf("wrong-typed capability must be 400: %d %s", bad.Code, bad.Body.String())
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
	// A non-object claude is refused. It is a 422 rather than the flat 400 the
	// UNDECLARED blocks (binaries above) still answer, because declaring the
	// nested shape (T-90be) makes codegen type this field as an object, so the
	// refusal now happens in the decoder instead of in the handler's own
	// asObject check. Both are refusals and both are logged by the warden; the
	// asymmetry is documented on AgentTelemetryIngestDTO in the frozen spec.
	// Only the TYPE of the block moved — its CONTENTS stay permissive, which is
	// pinned by TestHandleIngestTelemetry_UndeclaredNestedKeyStillLands below.
	if rec := doIngestTelemetry(api, "m-2", "m-2", `{"claude": "not-an-object"}`); rec.Code != 422 {
		t.Fatalf("non-object claude: %d, want 422", rec.Code)
	}
}

// TestHandleIngestTelemetry_WardenShapeFoldsEchoesAndValidates covers the ingest
// half of the anchor-cutover signal (T-ff5d): the warden's launchd SHAPE verdict
// is a closed three-state enum, and it is the only way the fleet can tell a
// converted machine from an unconverted one.
func TestHandleIngestTelemetry_WardenShapeFoldsEchoesAndValidates(t *testing.T) {
	// A SHAPE-ONLY heartbeat must be a valid report. The "at least one field"
	// check is hand-enumerated, so a new field that is not listed there turns the
	// very heartbeat this ticket adds into a 400.
	for _, want := range []string{"anchor", "legacy", "unknown"} {
		t.Run("a "+want+" heartbeat lands and echoes", func(t *testing.T) {
			api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
			rec := doIngestTelemetry(api, "m-1", "m-1", `{"warden_shape": "`+want+`"}`)
			if rec.Code != 200 {
				t.Fatalf("shape-only ingest: %d %s", rec.Code, rec.Body.String())
			}
			if got := api.telemetry.Get("m-1")["warden_shape"]; got != want {
				t.Fatalf("stored warden_shape = %v, want %q", got, want)
			}
			if !strings.Contains(rec.Body.String(), `"warden_shape":"`+want+`"`) {
				t.Fatalf("echo must round-trip the shape: %s", rec.Body.String())
			}
		})
	}

	// Outside the vocabulary is a FLAT 400 — the handler's own refusal, never the
	// decoder's 422. The distinction is the one spec/lifecycle.md §3 pins for
	// every other permissive scalar here, and a conformance suite asserting the
	// wrong one either goes red on correct behaviour or green on a regression.
	for _, value := range []string{`"modern"`, `"ANCHOR"`, `""`, `5`, `true`, `["anchor"]`} {
		api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
		body := `{"warden_shape": ` + value + `}`
		rec := doIngestTelemetry(api, "m-1", "m-1", body)
		if rec.Code != 400 {
			t.Errorf("%s = %d, want a flat 400", body, rec.Code)
		}
		if got := api.telemetry.Get("m-1"); got != nil {
			t.Errorf("%s stored %v; a refused value must not land", body, got)
		}
	}

	// null is not a state. It decodes to an absent field, so a body whose only
	// key is null is the all-absent 400 — NOT a stored "unknown". The server
	// never manufactures a verdict out of silence.
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "m-1", `{"warden_shape": null}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "is required") {
		t.Errorf(`{"warden_shape": null} = %d %s, want the all-absent 400`,
			rec.Code, rec.Body.String())
	}

	// PARTIAL MERGE. Most heartbeats on this endpoint carry no shape at all (a
	// command_result receipt, an agent's context report), and none of them may
	// erase the machine's verdict — an erased one reads as "this build does not
	// report a shape", which is a different and false claim.
	merge := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	if rec := doIngestTelemetry(merge, "m-2", "m-2", `{"warden_shape": "anchor"}`); rec.Code != 200 {
		t.Fatalf("seed ingest: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doIngestTelemetry(merge, "m-2", "m-2", `{"hardware": {"cpu_pct": 1}}`); rec.Code != 200 {
		t.Fatalf("second ingest: %d %s", rec.Code, rec.Body.String())
	}
	if got := merge.telemetry.Get("m-2")["warden_shape"]; got != "anchor" {
		t.Fatalf("warden_shape = %v after a shape-less heartbeat, want it to survive", got)
	}
}

// TestGetMonitoring_WardenShapeIsReportedNeverInvented is the monitoring-fold
// half of the read-back. The machines table renders this column beside
// bin_status, and the two are derived in opposite ways: bin_status is the
// server's own comparison, warden_shape is whatever the machine said. A machine
// that has said nothing must therefore leave the cell null rather than pick up
// the "unknown" state, which means something the server cannot know.
func TestGetMonitoring_WardenShapeIsReportedNeverInvented(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedRegisteredMachine(t, s, "m-converted")
	seedRegisteredMachine(t, s, "m-silent")
	if rec := doIngestTelemetry(s, "m-converted", "m-converted",
		`{"warden_shape": "anchor"}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}

	if got := machineRow(t, s, "m-converted")["warden_shape"]; got != "anchor" {
		t.Fatalf("m-converted warden_shape = %v, want anchor", got)
	}
	row := machineRow(t, s, "m-silent")
	if got, present := row["warden_shape"]; !present || got != nil {
		t.Fatalf("m-silent warden_shape = %v (present=%v), want an explicit null — "+
			"a machine that never reported has not reported \"unknown\"", got, present)
	}
}

// TestHandleIngestTelemetry_CutoverEffectFoldsEchoesAndValidates covers the
// ingest half of the cutover-EFFECT signal (T-17b4) — the verdict that says
// whether the anchor cutover actually reached the processes carrying agents,
// which warden_shape above cannot answer.
//
// Same closed-vocabulary contract as the shape, with one difference worth its
// own case: the two enums are NOT the same three words. "unknown" is a legal
// shape and an illegal effect, so a handler that validated the effect against
// the shape's vocabulary would store a verdict the wire does not define and the
// UI cannot narrow.
func TestHandleIngestTelemetry_CutoverEffectFoldsEchoesAndValidates(t *testing.T) {
	// An EFFECT-ONLY heartbeat must be a valid report. The "at least one field"
	// check is hand-enumerated, so a new field that is not listed there turns the
	// very heartbeat this ticket adds into a 400.
	for _, want := range []string{"effective", "not_effective", "unproven"} {
		t.Run("a "+want+" heartbeat lands and echoes", func(t *testing.T) {
			api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
			rec := doIngestTelemetry(api, "m-1", "m-1", `{"cutover_effect": "`+want+`"}`)
			if rec.Code != 200 {
				t.Fatalf("effect-only ingest: %d %s", rec.Code, rec.Body.String())
			}
			if got := api.telemetry.Get("m-1")["cutover_effect"]; got != want {
				t.Fatalf("stored cutover_effect = %v, want %q", got, want)
			}
			if !strings.Contains(rec.Body.String(), `"cutover_effect":"`+want+`"`) {
				t.Fatalf("echo must round-trip the verdict: %s", rec.Body.String())
			}
		})
	}

	// Outside the vocabulary is a FLAT 400 — the handler's own refusal, never the
	// decoder's 422 (spec/lifecycle.md §3). "unknown" and "anchor" are in the
	// list on purpose: they are the SHAPE's words, and borrowing a neighbouring
	// enum is the most likely way this validation goes wrong.
	for _, value := range []string{
		`"unknown"`, `"anchor"`, `"legacy"`, `"EFFECTIVE"`, `"not-effective"`,
		`"proven"`, `""`, `5`, `true`, `["effective"]`,
	} {
		api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
		body := `{"cutover_effect": ` + value + `}`
		rec := doIngestTelemetry(api, "m-1", "m-1", body)
		if rec.Code != 400 {
			t.Errorf("%s = %d, want a flat 400", body, rec.Code)
		}
		if got := api.telemetry.Get("m-1"); got != nil {
			t.Errorf("%s stored %v; a refused value must not land", body, got)
		}
	}

	// null is not a state. It decodes to an absent field, so a body whose only
	// key is null is the all-absent 400 — NOT a stored "unproven". The server
	// never manufactures a verdict out of silence, and "unproven" is the one
	// word most likely to be reached for as a default.
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "m-1", `{"cutover_effect": null}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "is required") {
		t.Errorf(`{"cutover_effect": null} = %d %s, want the all-absent 400`,
			rec.Code, rec.Body.String())
	}

	// PARTIAL MERGE. Most heartbeats carry no verdict at all, and none of them
	// may erase the machine's: an erased one reads as "this build does not report
	// the verdict", which is a different and false claim.
	merge := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	if rec := doIngestTelemetry(merge, "m-2", "m-2", `{"cutover_effect": "not_effective"}`); rec.Code != 200 {
		t.Fatalf("seed ingest: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doIngestTelemetry(merge, "m-2", "m-2", `{"hardware": {"cpu_pct": 1}}`); rec.Code != 200 {
		t.Fatalf("second ingest: %d %s", rec.Code, rec.Body.String())
	}
	if got := merge.telemetry.Get("m-2")["cutover_effect"]; got != "not_effective" {
		t.Fatalf("cutover_effect = %v after an effect-less heartbeat, want it to survive", got)
	}

	// The shape and the effect are STORED INDEPENDENTLY. "anchor" + "not_effective"
	// is not a contradiction to be reconciled — it is the state the incident had,
	// and it is the reason the second field exists at all.
	pair := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	if rec := doIngestTelemetry(pair, "m-3", "m-3",
		`{"warden_shape": "anchor", "cutover_effect": "not_effective"}`); rec.Code != 200 {
		t.Fatalf("paired ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := pair.telemetry.Get("m-3")
	if entry["warden_shape"] != "anchor" || entry["cutover_effect"] != "not_effective" {
		t.Fatalf("stored pair = %v/%v, want anchor/not_effective — neither may be "+
			"derived from or overwritten by the other",
			entry["warden_shape"], entry["cutover_effect"])
	}
}

// TestGetMonitoring_CutoverEffectIsReportedNeverInvented is the read-back half.
// Only the reporting machine can see its own carrier processes, so the server
// has no second source to compute the verdict from: a machine that has said
// nothing must leave the cell null rather than pick up "unproven", which would
// claim a check ran.
func TestGetMonitoring_CutoverEffectIsReportedNeverInvented(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedRegisteredMachine(t, s, "m-measured")
	seedRegisteredMachine(t, s, "m-silent")
	if rec := doIngestTelemetry(s, "m-measured", "m-measured",
		`{"cutover_effect": "unproven"}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}

	if got := machineRow(t, s, "m-measured")["cutover_effect"]; got != "unproven" {
		t.Fatalf("m-measured cutover_effect = %v, want unproven", got)
	}
	row := machineRow(t, s, "m-silent")
	if got, present := row["cutover_effect"]; !present || got != nil {
		t.Fatalf("m-silent cutover_effect = %v (present=%v), want an explicit null — "+
			"a machine that never reported has not reported \"unproven\"", got, present)
	}
}

// TestHandleIngestTelemetry_WrongTypedBlockStatusTable is the executable copy of
// the refusal-code table in spec/lifecycle.md §3 (and its restatement in
// conformance/CLAUDE.md). That doc line used to say a wrong-typed telemetry
// field is a flat 400, full stop — and it went on saying it after T-90be moved
// three of the blocks into the decoder, i.e. the normative source of truth for
// conformance was describing a wire that no longer existed.
//
// The split is not cosmetic: 400 is the handler REFUSING a value it understood,
// 422 is the decoder refusing to build the request at all, and a black-box suite
// asserting the wrong one either goes red on correct behaviour or stays green on
// a regression. Pinning it here means the doc can only drift from the code
// across a red test.
func TestHandleIngestTelemetry_WrongTypedBlockStatusTable(t *testing.T) {
	// DECLARED shape (T-90be) => codegen types the field as an object => a
	// non-object never reaches the handler, so the refusal is the decoder's 422.
	for _, field := range []string{"hardware", "claude", "runtimes"} {
		for _, value := range []string{`"not-an-object"`, `5`, `[]`} {
			api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
			body := `{"` + field + `": ` + value + `}`
			if rec := doIngestTelemetry(api, "m-1", "m-1", body); rec.Code != 422 {
				t.Errorf("%s = %d, want 422 — this block declares its shape, so the "+
					"decoder refuses it before the handler runs", body, rec.Code)
			}
		}
	}
	// UNDECLARED blocks are still permissive `any` on the generated type, so the
	// handler's own asObject check answers, and it answers with the flat 400 the
	// spec line describes. Both halves have to be pinned: a test that only knew
	// about the 422 would happily accept someone declaring these too, which is
	// the change that turns an unknown nested key into a rejected report.
	for _, field := range []string{
		"binaries", "rate_limits", "tokens", "command_result", "self_update",
	} {
		api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
		body := `{"` + field + `": "not-an-object"}`
		if rec := doIngestTelemetry(api, "m-1", "m-1", body); rec.Code != 400 {
			t.Errorf("%s = %d, want 400 — an undeclared block is refused by the "+
				"handler, not by the decoder", body, rec.Code)
		}
	}
	// A JSON null is not a wrong TYPE — it decodes to an absent field, so a body
	// whose only key is null lands in the all-absent 400 with `{}`. Declaring the
	// shape did NOT move this case, and the doc says so; if it ever became a 422
	// a warden clearing a block would start getting a different error class than
	// a warden sending nothing.
	for _, body := range []string{
		`{"hardware": null}`, `{"claude": null}`, `{"runtimes": null}`, `{}`,
	} {
		api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
		rec := doIngestTelemetry(api, "m-1", "m-1", body)
		if rec.Code != 400 {
			t.Errorf("%s = %d, want 400 (all-absent)", body, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "is required") {
			t.Errorf("%s: %s, want the all-absent refusal, not a type refusal",
				body, rec.Body.String())
		}
	}
	// And the empty OBJECT is not a refusal at all: a warden whose every probe
	// failed reports `hardware: {}`, which is a real (if contentless) sample.
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	if rec := doIngestTelemetry(api, "m-1", "m-1", `{"hardware": {}}`); rec.Code != 200 {
		t.Errorf(`{"hardware": {}} = %d, want 200 — an empty sample is a sample`,
			rec.Code)
	}
}

// TestHandleIngestTelemetry_UndeclaredNestedKeyStillLands is the compatibility
// SENTINEL for the nested declaration (T-90be, owner ruling rc-55861dd893c6).
//
// Declaring hardware/claude/runtimes buys CI a rename check; it must NOT buy
// runtime a rejection. A warden that grows a probe (or an older one that never
// had a key) sends nested keys this spec version has never heard of, and its
// WHOLE report — hardware, binaries, claude and runtimes together — must still
// land. Setting additionalProperties:false on any of these blocks turns exactly
// this request into `422 unknown field`, which is the a7fa594 outage verbatim
// (every machine's telemetry silently null at once). If someone "tightens" the
// spec, this goes red before the fleet does.
func TestHandleIngestTelemetry_UndeclaredNestedKeyStillLands(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	body := `{"hardware": {"cpu_pct": 12, "disk_pct": 41},
		"claude": {"version": "9.9.9", "cred_mtime": 1720000000},
		"runtimes": {"claude": {"installed": true, "sandboxed": true}},
		"binaries": {"ocwarden": "abc123abc123"}}`
	rec := doIngestTelemetry(api, "m-1", "m-1", body)
	if rec.Code != 200 {
		t.Fatalf("a heartbeat carrying undeclared NESTED keys must still land: %d %s",
			rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	hw, _ := entry["hardware"].(map[string]any)
	if hw["cpu_pct"] != 12.0 {
		t.Errorf("the declared sibling must survive: cpu_pct = %v, want 12", hw["cpu_pct"])
	}
	if hw["disk_pct"] != 41.0 {
		t.Errorf("an undeclared nested key must be stored, not dropped: hardware = %v", hw)
	}
	probe, _ := entry["claude"].(map[string]any)
	if probe["version"] != "9.9.9" || probe["cred_mtime"] != 1720000000.0 {
		t.Errorf("claude = %v, want the whole probe kept", probe)
	}
	rts, _ := entry["runtimes"].(map[string]any)
	claudeCap, _ := rts["claude"].(map[string]any)
	if claudeCap["installed"] != true || claudeCap["sandboxed"] != true {
		t.Errorf("runtimes.claude = %v, want the whole capability kept", claudeCap)
	}
	if bins, _ := entry["binaries"].(map[string]any); bins["ocwarden"] != "abc123abc123" {
		t.Errorf("the rest of the report must land too: binaries = %v", bins)
	}
}

// ── account_label (T-260e): human-readable account default display ──────────

const teleWithLabel = `{"runtime":"claude","hardware": {"cpu_pct": 1},
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
	// Carries a runtime, so the ingest reaches stampReportedLaunchFacts and
	// needs a real DAL (a runtime report is durable now — T-7f28).
	api := &apiServer{dal: newTestDAL(t), telemetry: newMemStore(), hub: NewHub()}
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
		`{"runtime":"claude","hardware": {"cpu_pct": 2}, "cost": 1.5, "account": "acct-123/team"}`)
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
		`{"runtime":"claude","hardware": {"cpu_pct": 1}, "account": "acct-123/team"}`)
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

// TestGetMonitoring_SessionEffortRoundTrips pins the reported effort all the way
// from ingest to the session row. The whole path (parse, store, serve) was
// already built and had NO test at all, so when the Claude reporter turned out
// never to have sent the key, nothing anywhere went red — the monitoring page
// simply showed a blank effort for every session, which is exactly what an
// honestly-not-yet-reported session looks like. The second case pins that blank:
// an unreported effort must stay empty, never fall back to the roster's
// owner-intent value.
func TestGetMonitoring_SessionEffortRoundTrips(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	reported := fullMember("kyle")
	reported.Effort = "medium"
	silent := fullMember("mira")
	silent.Effort = "high"
	for _, m := range []Member{reported, silent} {
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed member: %v", err)
		}
	}
	rec := doIngestTelemetry(s, "kyle", "m-abc123", `{"runtime":"claude","effort":"medium"}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	// mira reports too, but without effort: a live session that simply never
	// carried the field must NOT borrow its configured "high".
	if rec := doIngestTelemetry(s, "mira", "m-abc123", `{"runtime":"claude"}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	got := map[string]any{}
	for _, raw := range d["sessions"].([]any) {
		row := raw.(map[string]any)
		got[row["id"].(string)] = row["effort"]
	}
	if got["kyle"] != "medium" {
		t.Errorf("reported effort = %v, want medium", got["kyle"])
	}
	if got["mira"] != "" {
		t.Errorf("unreported effort = %v, want \"\" (never the roster's high)", got["mira"])
	}
}

// TestGetMonitoring_SessionModelRoundTrips is the model twin of the effort test
// above, and it exists because the same shape broke twice on the same wire.
//
// The OUTSOURCE case is the one the owner reported. A worker's model column was
// served from ActualModel, whose only two writers both sit on report_waking —
// and the outsource overlay seed deliberately removed report_waking from a
// worker's boot sequence at the time (it claimed a worker's online signal was
// get_my_task, which never touches the field). So every outsource worker's
// model was STRUCTURALLY the empty string, forever, and empty is
// indistinguishable from "has not reported yet". Nothing was red because
// nothing tested the path end to end.
//
// T-4595 closed that hole from BOTH ends, and the two halves shipped as two
// packages: the overlay seed was deleted outright (report_waking always worked
// for an outsource caller — the seed that said otherwise was simply wrong), and
// get_my_task was retired, which moved the assigned→active flip onto
// report_waking so every worker now calls it on boot. That particular structural
// emptiness is therefore gone; the telemetry writer added here remains the
// load-bearing fix, and this test still guards the wire it left behind.
//
// The STAFF case is the other half of the same ruling (owner, 2026-07-31: both
// kinds read the reported value, with no fall-back-to-configured branch left
// anywhere). A staff row that has never reported now blanks, which is what the
// row's every other cell already does.
//
// 🔴 Each assertion below names a CONCRETE string. Asserting non-nil, or key
// presence, or "not the configured value" would all have passed BEFORE this
// change — the field was "" on every row, so those shapes are tautologies here.
func TestGetMonitoring_SessionModelRoundTrips(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	staff := fullMember("kyle")
	staff.Model = "opus"
	silent := fullMember("mira")
	silent.Model = "opus"
	for _, m := range []Member{staff, silent} {
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed member: %v", err)
		}
	}
	// The worker is configured "sonnet" and reports something else, so a row
	// serving the configured value is distinguishable from one serving the
	// reported value — the assertion cannot be satisfied by either fallback.
	seedWorker(t, s, "ow-eva", "E1", 0, WorkerStatusActive)
	wk, err := s.dal.GetOutsourceWorker("ow-eva")
	if err != nil || wk == nil {
		t.Fatalf("seed worker: %v", err)
	}
	wk.Model = "sonnet"
	// Through model's sole writer (T-55): seedWorker already INSERTed this row
	// with "opus", and a whole-row write no longer carries the column — leaving
	// the configured model at "opus" would quietly undo the setup this test's
	// comment above depends on (configured must differ from reported).
	if err := s.dal.SetMemberModel(wk.ID, "sonnet"); err != nil {
		t.Fatalf("seed worker model: %v", err)
	}

	for _, tc := range []struct{ actor, body string }{
		{"kyle", `{"runtime":"claude","model":"claude-opus-4-5-20251101[1m]"}`},
		{"ow-eva", `{"runtime":"claude","model":"claude-sonnet-4-5-20250929"}`},
		// mira reports, but carries no model: a live session that never sent the
		// field must not borrow its configured "opus".
		{"mira", `{"runtime":"claude"}`},
	} {
		if rec := doIngestTelemetry(s, tc.actor, "m-abc123", tc.body); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", tc.actor, rec.Code, rec.Body.String())
		}
	}

	rows := sessionRows(t, monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})))
	for _, tc := range []struct{ id, want, why string }{
		{"kyle", "claude-opus-4-5-20251101[1m]", "the staff session's reported model"},
		{"ow-eva", "claude-sonnet-4-5-20250929", "the worker's reported model, not its configured sonnet"},
		{"mira", "", "never reported ⇒ blank, never the configured opus"},
	} {
		row, ok := rows[tc.id]
		if !ok {
			t.Fatalf("no session row for %s; got %v", tc.id, sessionIDs(monitoringOf(t,
				doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))))
		}
		if row["model"] != tc.want {
			t.Errorf("%s model = %v, want %q (%s)", tc.id, row["model"], tc.want, tc.why)
		}
	}
}

// TestGetMonitoring_ReportedModelSurvivesATelemetryWipe pins the durable half.
// The telemetry store is in-memory, so serving the model out of it alone would
// blank the whole fleet's 模型 column on every server re-exec — the same
// fleet-wide blank this change exists to remove, just on a timer. Dropping the
// stampReportedModel call makes this red while the round-trip test above stays
// green, so the two failures point at different halves of the fix.
func TestGetMonitoring_ReportedModelSurvivesATelemetryWipe(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedWorker(t, s, "ow-eva", "E1", 0, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-eva", "m-abc123",
		`{"runtime":"claude","model":"claude-opus-5"}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	// Exactly what a re-exec does to the process-local telemetry map.
	s.telemetry = newMemStore()

	rows := sessionRows(t, monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})))
	if got := rows["ow-eva"]["model"]; got != "claude-opus-5" {
		t.Errorf("model after telemetry wipe = %v, want claude-opus-5 — the report "+
			"must be persisted on the roster row, not only in memory", got)
	}
}

// TestStampReportedModel_BlankReportNeverErasesAStoredModel covers the guard
// stampReportedModel's comment devotes a paragraph to, and which had no test at
// all: an explicit blank must be a no-op, not an erasure.
//
// Zero coverage was not academic. BOTH producers omit the key when they cannot
// read a model, so nothing in production reaches this branch today — which means
// the guard was held up entirely by an upstream convention with no downstream
// enforcement. That is the same shape as the bug this whole change repairs (a
// contract everyone believed, that nothing checked), so it gets a test rather
// than a comment.
func TestStampReportedModel_BlankReportNeverErasesAStoredModel(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedWorker(t, s, "ow-eva", "E1", 0, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-eva", "m-abc123",
		`{"runtime":"claude","model":"claude-opus-5"}`); rec.Code != 200 {
		t.Fatalf("seed ingest: %d %s", rec.Code, rec.Body.String())
	}
	for _, blank := range []string{`""`, `"   "`} {
		if rec := doIngestTelemetry(s, "ow-eva", "m-abc123",
			`{"runtime":"claude","model":`+blank+`}`); rec.Code != 200 {
			t.Fatalf("blank ingest %s: %d %s", blank, rec.Code, rec.Body.String())
		}
		rows := sessionRows(t, monitoringOf(t,
			doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})))
		if got := rows["ow-eva"]["model"]; got != "claude-opus-5" {
			t.Errorf("model after an explicit %s report = %v, want claude-opus-5 "+
				"— a blank means \"not measured\", never \"erase what you knew\"",
				blank, got)
		}
	}
}

// TestStampReportedModel_TelemetryNeverResurrectsADismissedMember covers the
// other guard with no test: a telemetry POST must not be able to CREATE or
// RESURRECT a roster row. putMember is an upsert, so without the roster check
// this handler would happily write a member row for any authenticated sub —
// including one the owner has dismissed, which would walk back onto the cockpit
// carrying a model it "reported" after being let go.
func TestStampReportedModel_TelemetryNeverResurrectsADismissedMember(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	dismissed := fullMember("mira")
	dismissed.RosterStatus = RosterStatusRemoved
	if err := s.dal.PutMember(dismissed); err != nil {
		t.Fatalf("seed dismissed member: %v", err)
	}
	if rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","model":"claude-opus-5"}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	got, err := s.dal.GetMember("mira")
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	if got.ActualModel != "" {
		t.Errorf("dismissed member's actual_model = %q, want \"\" — a telemetry "+
			"report must never write onto a dismissed roster row", got.ActualModel)
	}
	if got.RosterStatus != RosterStatusRemoved {
		t.Errorf("roster_status = %q, want it to stay removed — telemetry "+
			"resurrected a dismissed member", got.RosterStatus)
	}
	// And it must not appear on the cockpit's session list either.
	rows := sessionRows(t, monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})))
	if _, present := rows["mira"]; present {
		t.Errorf("dismissed member surfaced on the sessions fold: %v", rows["mira"])
	}
}

// TestGetMonitoring_ReportedLaunchFactsSurviveAReExec records, as an executable
// fact, that all THREE reported launch facts are now durable columns: a server
// re-exec throws away the in-memory telemetry store and the fold still answers
// from actual_model / actual_runtime / actual_effort.
//
// This assertion used to pin the OPPOSITE for effort and runtime — the
// asymmetry where only model survived — and said in so many words that adding
// an actual_effort column had to update the spec text in the same commit
// (T-7f28 did). Kept as a re-exec test rather than deleted: if any of the three
// goes back to being read off s.telemetry, this fails instead of the cockpit
// quietly blanking fleet-wide on the next restart.
func TestGetMonitoring_ReportedLaunchFactsSurviveAReExec(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedWorker(t, s, "ow-eva", "E1", 0, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-eva", "m-abc123",
		`{"runtime":"claude","model":"claude-opus-5","effort":"xhigh"}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	s.telemetry = newMemStore() // what a server re-exec does

	row := sessionRows(t, monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})))["ow-eva"]
	for _, c := range []struct{ field, want string }{
		{"model", "claude-opus-5"},
		{"runtime", RuntimeClaude},
		{"effort", "xhigh"},
	} {
		if row[c.field] != c.want {
			t.Errorf("%s after re-exec = %v, want %q (durable column)",
				c.field, row[c.field], c.want)
		}
	}
}

// TestGetMonitoring_ReportedLaunchFactsNeverFallBackToTheConfiguredValue is the
// half the re-exec test cannot see: a member that has reported NOTHING must
// read blank on all three, not echo what the owner configured. A fallback here
// is what made a launch change that had not taken effect yet
// byte-indistinguishable from one that had (T-7f28 — the reason the ticket
// exists).
func TestGetMonitoring_ReportedLaunchFactsNeverFallBackToTheConfiguredValue(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	configured := fullMember("mira")
	configured.Runtime = RuntimeClaude
	configured.Model = "opus"
	configured.Effort = "xhigh"
	if err := s.dal.PutMember(configured); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	row := sessionRows(t, monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})))["mira"]
	for _, field := range []string{"model", "runtime", "effort"} {
		if row[field] != "" {
			t.Errorf("%s = %v, want \"\" — nothing has reported one, and serving "+
				"the configured value here makes a pending change look applied",
				field, row[field])
		}
	}
}

// sessionRows indexes the sessions fold by row id.
func sessionRows(t *testing.T, d map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, raw := range d["sessions"].([]any) {
		row := raw.(map[string]any)
		out[row["id"].(string)] = row
	}
	return out
}

// sessionIDs returns the sessions fold's ids in wire order.
func sessionIDs(d map[string]any) []string {
	var out []string
	for _, raw := range d["sessions"].([]any) {
		out = append(out, raw.(map[string]any)["id"].(string))
	}
	return out
}

// TestGetMonitoring_SessionsListStaffAndOutsourceAlike pins owner ruling
// rc-1f8156f25b7a ①: ONE list for every AI session. The cockpit used to JOIN
// this list with GET /api/outsource-workers, where `effort`/`model` mean the
// owner's configured LAUNCH INTENT (the detail panel's editor round-trips
// them) — so one table rendered two columns whose meaning changed per row.
func TestGetMonitoring_SessionsListStaffAndOutsourceAlike(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	if err := s.dal.PutMember(fullMember("mira")); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	seedWorker(t, s, "ow-eva", "E1", 2.5, WorkerStatusActive)

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got, want := sessionIDs(d), []string{"mira", "ow-eva"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("session ids = %v, want %v (staff first, then workers by created_ts)", got, want)
	}
	rows := sessionRows(t, d)
	worker := rows["ow-eva"]
	if worker["name"] != "E1" {
		t.Errorf("worker name = %v, want its codename E1", worker["name"])
	}
	// A worker's member row carries role_key "" by construction
	// (memberFromWorker), so the honest answer is the empty role a role-less
	// member already serves — never a fabricated 「外包」 that no role registry
	// would resolve. The cockpit tells the kinds apart by the `ow-` id prefix.
	if worker["role"] != "" {
		t.Errorf("worker role = %v, want \"\" — a worker has no role_key", worker["role"])
	}
	// Reported runtime, honest-empty: seedWorker configures claude but nothing
	// has reported one. Serving the configured value here is what made a
	// runtime change look applied the instant it was saved (T-7f28).
	if worker["runtime"] != "" {
		t.Errorf("worker runtime = %v, want \"\" — nothing has reported one",
			worker["runtime"])
	}
	if worker["banked_cost"] != 2.5 {
		t.Errorf("worker banked_cost = %v, want 2.5", worker["banked_cost"])
	}
	member := rows["mira"]
	if member["name"] != "Mira" || member["role"] != "Assistant" {
		t.Errorf("staff row changed: %v", member)
	}
	// fullMember("mira") is configured model "opus" and has reported nothing, so
	// the honest answer is the SAME blank the worker beside it shows. This
	// assertion used to read `== "opus"` — it pinned the configured launch value
	// onto a column the owner has since ruled is reported state for both kinds
	// (2026-07-31), which is the half of rc-1f8156f25b7a this fold had not
	// finished: one list, but still two meanings under one header.
	if member["model"] != "" {
		t.Errorf("staff model = %v, want \"\" — unreported means blank, never the "+
			"configured opus", member["model"])
	}
}

// TestGetMonitoring_WorkerSessionReadsItsOwnTelemetry: every telemetry-sourced
// column on a worker row comes from THAT worker's entry (keyed by its `ow-` id,
// which is its token sub — its ocagent already posts under that key), never
// from a member's and never from the roster's launch intent.
func TestGetMonitoring_WorkerSessionReadsItsOwnTelemetry(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	if err := s.dal.PutMember(fullMember("mira")); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	seedRegisteredMachine(t, s, "m-eva-m5")
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-eva", Codename: "E1", Runtime: RuntimeClaude,
		// Configured launch intent, deliberately DIFFERENT from what the
		// session reports below.
		Model: "opus", Effort: "medium",
		ActualModel: "sonnet-4.6",
		TaskID:      "t-1", Status: WorkerStatusActive,
		CreatedTS: 1.0, DesiredState: "online",
	}); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	if rec := doIngestTelemetry(s, "mira", "m-seth-m5",
		`{"runtime":"claude","effort":"high","cost":9.5}`); rec.Code != 200 {
		t.Fatalf("member ingest: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doIngestTelemetry(s, "ow-eva", "m-eva-m5",
		`{"runtime":"claude","effort":"low","cost":1.25}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}
	s.gauge.Set("ow-eva", map[string]any{"context_pct": 37.0})
	s.gauge.Set("mira", map[string]any{"context_pct": 88.0})

	rows := sessionRows(t, monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})))
	worker := rows["ow-eva"]
	if worker["effort"] != "low" {
		t.Errorf("worker effort = %v, want the reported low (never the configured medium)",
			worker["effort"])
	}
	if worker["model"] != "sonnet-4.6" {
		t.Errorf("worker model = %v, want the self-reported sonnet-4.6 (never the configured opus)",
			worker["model"])
	}
	if worker["cost"] != 1.25 {
		t.Errorf("worker cost = %v, want 1.25", worker["cost"])
	}
	if worker["context_pct"] != 37.0 {
		t.Errorf("worker context_pct = %v, want 37", worker["context_pct"])
	}
	if worker["machine"] != "m-eva-m5" {
		t.Errorf("worker machine = %v, want m-eva-m5", worker["machine"])
	}
	member := rows["mira"]
	if member["effort"] != "high" || member["cost"] != 9.5 || member["context_pct"] != 88.0 {
		t.Errorf("staff row must keep its OWN telemetry, got %v", member)
	}
}

// TestGetMonitoring_SilentWorkerSessionShowsHonestBlanks is the counterpart:
// a worker that has reported NOTHING must render blank, exactly like a silent
// staff session. Falling back to its configured model/effort would republish
// the owner's launch intent as an observation — the blank IS the finding.
func TestGetMonitoring_SilentWorkerSessionShowsHonestBlanks(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedWorker(t, s, "ow-quiet", "Q1", 0, WorkerStatusAssigned) // Model opus, Effort medium

	worker := sessionRows(t, monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})))["ow-quiet"]
	if worker == nil {
		t.Fatalf("a silent worker must still list as a session")
	}
	if worker["effort"] != "" {
		t.Errorf("effort = %v, want \"\" (never the configured medium)", worker["effort"])
	}
	if worker["model"] != "" {
		t.Errorf("model = %v, want \"\" (never the configured opus)", worker["model"])
	}
	for _, k := range []string{"cost", "context_pct"} {
		if worker[k] != nil {
			t.Errorf("%s = %v, want null", k, worker[k])
		}
	}
	if worker["account"] != "" || worker["machine"] != "" {
		t.Errorf("unobserved account/machine must stay empty, got %v", worker)
	}
}

// TestGetMonitoring_ReleasedWorkerIsNotASession: released is the STEADY state
// for a worker (every task close releases one) and worker rows are retained
// forever as the audit trail, so listing them would grow this table with every
// task the station has ever run. Released maps onto RosterStatusRemoved, the
// same predicate the staff side is filtered by — one rule, not two. The
// accounts fold keeps counting their spend (ReleasedWorkerSpendStaysInTheAccount).
func TestGetMonitoring_ReleasedWorkerIsNotASession(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedWorker(t, s, "ow-live", "L1", 0, WorkerStatusActive)
	seedWorker(t, s, "ow-gone", "G1", 3.0, WorkerStatusReleased)
	if rec := doIngestTelemetry(s, "ow-gone", "m-eva-m5",
		`{"runtime":"claude","account":"eva-m5-claude","cost":1.0}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got, want := sessionIDs(d), []string{"ow-live"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("session ids = %v, want %v", got, want)
	}
	// The 3.0 seeded as banked was never reported, so it is not in the account's
	// accumulator (rc-5c5d7c7c6dcd); the 1.0 that WAS reported must survive.
	if row := accountRow(t, d, "eva-m5-claude"); row["cost"] != 1.0 {
		t.Errorf("released worker's spend = %v, want 1 (the figure it reported) — "+
			"dropping its SESSION must not drop its money", row["cost"])
	}
}

func TestGetMonitoring_WorkerReportedLabelResolvesSessionAccount(t *testing.T) {
	// T-ba6b (recon §6-4/§6-6): the label overlay scans the WHOLE telemetry
	// snapshot, so an account_label reported by an OUTSOURCE-WORKER session
	// resolves a member session on the same account (the old fold scanned only
	// roster members and left the raw key).
	s := labelTestServer(t)
	// Strip the member's own label; keep only the account key.
	s.telemetry.Set("mira", map[string]any{"account": "acct-123/team", accountRuntimeKey: RuntimeClaude})
	// A worker entry (NOT a roster member) reports the label for the same key.
	s.telemetry.Set("ow-1", map[string]any{
		"account": "acct-123/team", accountRuntimeKey: RuntimeClaude,
		"account_label": "eva@corp(Corp)", "ts": 99.0})
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
		`{"runtime":"claude","hardware": {"cpu_pct": 1}, "account": "acct-123/team"}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if _, present := row["account_label"]; present {
		t.Fatalf("label-less report must omit account_label, got %v", row)
	}
}

// runtimeAccountServer reproduces the owner-reported shape: a member's current
// runtime is Claude, but its durable telemetry entry carries an older Codex
// account. The owner alias makes an accidental attribution immediately visible.
func runtimeAccountServer(t *testing.T, memberRuntime, report string) *apiServer {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("kyle")
	m.Runtime = memberRuntime
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	// BOTH keys are aliased, so every assertion below can tell "withheld" apart
	// from "merely unresolvable" — an empty account cell means the server
	// refused to attribute the key, never that it had no readable name for it.
	for _, alias := range []AccountAlias{
		{Account: "codex:8906abc", DisplayName: "EvaChatGPT"},
		{Account: "claude:uid/org", DisplayName: "EvaClaude"},
	} {
		if err := s.dal.PutAccountAlias(alias); err != nil {
			t.Fatalf("seed alias: %v", err)
		}
	}
	seedRegisteredMachine(t, s, "m-eva-m5")
	if rec := doIngestTelemetry(s, "kyle", "m-eva-m5", report); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	return s
}

const codexReportedAccount = `{"runtime":"codex","account":"codex:8906abc","account_label":"ReporterOnly"}`
const claudeReportedAccount = `{"runtime":"claude","account":"claude:uid/org","account_label":"ReporterOnly"}`

// sessionAccount reads the one member row's account cell from the owner-facing
// monitoring fold (the surface the member panel renders).
func sessionAccount(t *testing.T, s *apiServer) any {
	t.Helper()
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	return d["sessions"].([]any)[0].(map[string]any)["account"]
}

func TestGetMonitoring_RuntimeAccountNeverBorrowsAnotherRuntime(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeClaude, codexReportedAccount)
	ownerRec := doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})
	d := monitoringOf(t, ownerRec)
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "" {
		t.Fatalf("claude session account = %v, want honest empty", got)
	}
	// Addressed by host rather than by index: the machines list no longer
	// contains every host that ever reported (T-b89d), so "row 0" is not a
	// stable way to name the box under test.
	if mr := machineRowIn(d, "m-eva-m5"); mr == nil {
		t.Fatalf("registered machine m-eva-m5 lost its row: %v", d["machines"])
	} else if got := mr["accounts"].([]any); len(got) != 0 {
		t.Fatalf("foreign-runtime account leaked into machine fold: %v", got)
	}
	if got := d["accounts"].([]any); len(got) != 1 || got[0].(map[string]any)["display_name"] != "EvaChatGPT" {
		t.Fatalf("global account overview lost owner alias visibility: %v", got)
	}
	if got := d["accounts"].([]any)[0].(map[string]any)["machine"]; got != "" {
		t.Fatalf("global foreign account must not inherit Claude machine: %v", got)
	}
}

func TestGetMonitoring_RuntimeAccountKeepsCodexAndOwnerGate(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeCodex, codexReportedAccount)
	owner := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got := owner["sessions"].([]any)[0].(map[string]any)["account"]; got != "EvaChatGPT" {
		t.Fatalf("codex session account = %v, want EvaChatGPT", got)
	}
	if got := owner["accounts"].([]any); len(got) != 1 {
		t.Fatalf("codex account must remain observable, got %v", got)
	}
	// Existing privacy gate: an agent never receives reporter-supplied labels.
	agentRec := doGetMonitoring(s, map[string]any{"sub": "kyle", "scope": "agent"})
	if strings.Contains(agentRec.Body.String(), `"account_label":"ReporterOnly"`) {
		t.Fatalf("agent response leaked reporter-only account label: %s", agentRec.Body.String())
	}
}

// TestHandleIngestTelemetry_AccountPairingIsAtomic pins the ingest half of the
// guarantee: `account`, its provenance stamp and the reporter label are ONE
// unit, and any report that cannot prove the pairing retires it instead of
// leaving a stale one standing for a later report to inherit.
func TestHandleIngestTelemetry_AccountPairingIsAtomic(t *testing.T) {
	// Its reports carry a runtime, so the ingest reaches
	// stampReportedLaunchFacts and needs a real DAL (T-7f28).
	api := &apiServer{dal: newTestDAL(t), telemetry: newMemStore(), hub: NewHub()}
	entry := func() map[string]any { return api.telemetry.Get("kyle") }
	ingest := func(body string) {
		t.Helper()
		if rec := doIngestTelemetry(api, "kyle", "", body); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", body, rec.Code, rec.Body.String())
		}
	}

	// ① a proven report stamps key + provenance + label together.
	ingest(codexReportedAccount)
	if got, _ := entry()[accountRuntimeKey].(string); got != RuntimeCodex {
		t.Fatalf("account runtime = %q, want codex", got)
	}
	// ② a same-runtime report with no account leaves the pairing alone.
	ingest(`{"runtime":"codex","cost":1}`)
	if got, _ := entry()["account"].(string); got != "codex:8906abc" {
		t.Fatalf("same-runtime heartbeat lost the paired account: %q", got)
	}
	// ③ an account WITHOUT a runtime is unprovable: it is not stored, and it
	// must not leave the previous pairing behind either — that leftover is what
	// a later runtime-only heartbeat used to inherit.
	ingest(`{"cost":1,"account":"unprovable"}`)
	for _, key := range []string{"account", accountRuntimeKey, "account_label"} {
		if v, present := entry()[key]; present {
			t.Fatalf("runtime-less account report left %s = %v standing", key, v)
		}
	}
	// ④ a runtime-only heartbeat on a DIFFERENT runtime retires the pairing:
	// the key belonged to the runtime the actor just left.
	ingest(codexReportedAccount)
	ingest(`{"runtime":"claude"}`)
	if v, present := entry()["account"]; present {
		t.Fatalf("runtime switch kept the prior runtime's account %v", v)
	}
}

// TestGetMonitoring_RuntimelessAccountCannotDegradeIntoOlderRuntime is the
// end-to-end sequence blocker 2 named: a proven pairing, then a report whose
// runtime went missing. "Missing runtime" must never degrade into "some older
// runtime" — the panel shows nothing rather than the key the actor used before.
func TestGetMonitoring_RuntimelessAccountCannotDegradeIntoOlderRuntime(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeClaude, claudeReportedAccount)
	if got := sessionAccount(t, s); got != "EvaClaude" {
		t.Fatalf("proven Claude pairing must be served, got %v", got)
	}
	// The actor has moved to Codex; this report carries the new key but lost
	// its runtime field (older / partial reporter). Unprovable in, nothing out.
	if rec := doIngestTelemetry(s, "kyle", "m-eva-m5",
		`{"cost":1,"account":"codex:8906abc"}`); rec.Code != 200 {
		t.Fatalf("runtime-less account report: %d %s", rec.Code, rec.Body.String())
	}
	if got := sessionAccount(t, s); got != "" {
		t.Fatalf("runtime-less report degraded into the older runtime's account: %v", got)
	}
	if v, present := s.telemetry.Get("kyle")["account"]; present {
		t.Fatalf("unprovable report left an inheritable account %v behind", v)
	}
}

// TestGetMonitoring_RuntimeSwitchHeartbeatRetiresPriorAccount covers the other
// half of the same sequence: the actor announces its new runtime before the
// member row catches up. The old key must not keep being served under the
// lagging row.
func TestGetMonitoring_RuntimeSwitchHeartbeatRetiresPriorAccount(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeClaude, claudeReportedAccount)
	if rec := doIngestTelemetry(s, "kyle", "m-eva-m5", `{"runtime":"codex"}`); rec.Code != 200 {
		t.Fatalf("codex heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	if got := sessionAccount(t, s); got != "" {
		t.Fatalf("account survived the runtime the actor left: %v", got)
	}
}

// TestGetMonitoring_OwnerAliasVisibleToEveryCallerRank pins the contract
// blocker 1 questioned (resolveAccountDisplay ①, account_display.go): the
// owner's hand-set alias is readable by EVERY caller rank; only the
// reporter-supplied account_label is owner-gated PII (T-260e). Runtime-aware
// attribution decides WHICH key an actor owns — it must never narrow WHO may
// read the alias of a key that is displayed.
func TestGetMonitoring_OwnerAliasVisibleToEveryCallerRank(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeCodex, codexReportedAccount)
	agentRec := doGetMonitoring(s, map[string]any{"sub": "kyle", "scope": "agent"})
	d := monitoringOf(t, agentRec)
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "EvaChatGPT" {
		t.Fatalf("agent-facing session account = %v, want owner alias EvaChatGPT", got)
	}
	if got := d["accounts"].([]any)[0].(map[string]any)["display_name"]; got != "EvaChatGPT" {
		t.Fatalf("agent-facing accounts display_name = %v, want owner alias EvaChatGPT", got)
	}
	// The owner-only half of the same contract holds in the very same body.
	if strings.Contains(agentRec.Body.String(), "ReporterOnly") {
		t.Fatalf("agent response leaked the reporter-only label: %s", agentRec.Body.String())
	}
}

func TestGetMonitoring_RuntimeHeartbeatCannotReattributePairedAccount(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeClaude, codexReportedAccount)
	// Counterfactual: the same actor later sends only a Claude runtime heartbeat.
	// Its account remains stamped Codex, so the heartbeat must not borrow it.
	if rec := doIngestTelemetry(s, "kyle", "m-eva-m5", `{"runtime":"claude"}`); rec.Code != 200 {
		t.Fatalf("claude heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "" {
		t.Fatalf("runtime-only Claude heartbeat reattributed Codex account: %v", got)
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
	}, "w-test", "")

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
	}, "w-test", "")
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
	}, "w-test", "")

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
	}, "w-test", "")
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
	}, "w-test", "")
	got, _ := s.dal.GetMember("mira")
	if got.LastOpReason != "" {
		t.Fatalf("a reason-less receipt must fold an empty reason, got %q", got.LastOpReason)
	}
	if got.LastOp != "stop" || got.LastOpLog == "" {
		t.Fatalf("the rest of the fold must be untouched, got %+v", got)
	}
}

// ── T-9adc: no-op stop receipts must not pollute last_op ─────────────────────

// TestFoldCommandResult_NoopStopDoesNotPolluteLastOp: an idempotent no-op stop
// receipt (ok=true, reason no_such_session — the warden had no session and no
// member process; identity sweeps and mis-routed stops produce exactly these)
// must NOT overwrite the member's last_op* — get_member keeps showing what
// actually happened, not a forged "successfully stopped" (the 2026-07-20
// incident's misleading observation surface).
func TestFoldCommandResult_NoopStopDoesNotPolluteLastOp(t *testing.T) {
	s := foldTestServer(t)
	m := fullMember("mira")
	ok := true
	m.LastOp, m.LastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt =
		"start", &ok, "spawned", "", 1_000.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "mira",
		"rpc":       "stop",
		"ok":        true,
		"reason":    "no_such_session: stop was a no-op (no session, no member process on this warden)",
		"log":       "session=member-mira: no_such_session",
		"at":        "2026-07-20T04:30:00Z",
	}, "w-test", "")
	got, err := s.dal.GetMember("mira")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.LastOp != "start" {
		t.Fatalf("no-op stop must NOT overwrite last_op, got %q", got.LastOp)
	}
	if got.LastOpOK == nil || !*got.LastOpOK || got.LastOpLog != "spawned" ||
		got.LastOpAt != 1_000.0 {
		t.Fatalf("no-op stop must leave the whole last_op* block untouched, got %+v", got)
	}
}

// TestFoldCommandResult_FailedStopAlwaysFolds (guard): only the ok=true no-op
// is skipped — a FAILED stop folds even if its reason ever carried the no-op
// code (failure must stay visible; the honest-partial contract is untouched).
func TestFoldCommandResult_FailedStopAlwaysFolds(t *testing.T) {
	s := foldTestServer(t)
	m := fullMember("mira")
	m.LastOp = "start"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "mira", "rpc": "stop", "ok": false,
		"reason": "no_such_session: contradictory failed no-op (defensive)",
	}, "w-test", "")
	got, _ := s.dal.GetMember("mira")
	if got.LastOp != "stop" || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("a failed stop must fold regardless of reason, got %+v", got)
	}
}

// TestFoldCommandResult_RealStopStillFolds (guard): a genuine kill's receipt
// (ok=true, reason "stopped") keeps folding exactly as before.
func TestFoldCommandResult_RealStopStillFolds(t *testing.T) {
	s := foldTestServer(t)
	m := fullMember("mira")
	m.LastOp = "start"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "mira", "rpc": "stop", "ok": true, "reason": "stopped",
	}, "w-test", "")
	got, _ := s.dal.GetMember("mira")
	if got.LastOp != "stop" || got.LastOpOK == nil || !*got.LastOpOK {
		t.Fatalf("a real stop receipt must keep folding, got %+v", got)
	}
}

// TestFoldWorkerCommandResult_NoopStopSkipped: the worker-row twin — an
// identity-sweep no-op stop for an outsource worker must not overwrite the
// worker's last_op* either.
func TestFoldWorkerCommandResult_NoopStopSkipped(t *testing.T) {
	s := foldTestServer(t)
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-7", Codename: "O-7", Model: "opus", Effort: "high",
		TaskID: "t-1", Status: WorkerStatusAssigned, CreatedTS: 1.0,
		LastOp: "start", LastOpAt: 500.0,
	}); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"worker_id": "ow-7", "rpc": "stop", "ok": true,
		"reason": "no_such_session: stop was a no-op (no session, no member process on this warden)",
	}, "w-test", "")
	got, err := s.dal.GetOutsourceWorker("ow-7")
	if err != nil || got == nil {
		t.Fatalf("get worker: %v %v", got, err)
	}
	if got.LastOp != "start" || got.LastOpAt != 500.0 {
		t.Fatalf("no-op stop must not pollute the worker last_op, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Hardware freshness (T-b36a)
//
// Telemetry is only cleared when a member is DISMISSED, never when it goes
// away, and nothing on the wire has ever said how old a hardware sample is. So
// a machine that reported once and then went dark kept serving that sample
// forever — a confident "47%" sitting next to an offline badge. These pin that
// a sample past telemetryFreshSecs reads as NO DATA (the same honest nulls a
// machine that never reported hardware serves), and that a fresh one is
// untouched.
// ---------------------------------------------------------------------------

// freshnessServer seeds one member reporting from host m-abc123, then returns
// the server plus a hook that rewrites how long ago its hardware was sampled.
func freshnessServer(t *testing.T, hw string) (*apiServer, func(ageSecs float64)) {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	seedRegisteredMachine(t, s, "m-abc123")
	if rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","hardware":`+hw+`}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	return s, func(ageSecs float64) {
		entry := s.telemetry.Get("mira")
		entry["hardware_ts"] = nowSecs() - ageSecs
		s.telemetry.Set("mira", entry)
	}
}

// seedRegisteredMachine registers the warden member that makes `id` a REAL
// machine — the roster fact GET /api/machines lists and (since T-b89d) the
// monitoring machines fold requires before it will emit a row for a host.
//
// ⚠️ Not decoration, and not "making the test pass": a host id can only ever
// reach this handler by being a machine the server itself placed something on
// (a member's desired_machine_id, an SSE machine claim, a worker's spawn
// target — all of them warden member ids), so a fixture that reports telemetry
// from an unregistered host was describing a world production cannot produce.
// Registering it is what makes the fixture faithful; the T-b89d filter is what
// made the difference visible.
//
// Two deliberate deviations from fullMember: a name sorting after "Mira" (the
// warden is also a member, so it joins the `sessions` list — several tests
// index sessions[0]) and a zeroed banked balance (so it cannot perturb a cost
// total under any future fold).
func seedRegisteredMachine(t *testing.T, s *apiServer, id string) {
	t.Helper()
	m := fullMember(id)
	m.Kind = machineKind
	m.Name = "zz-warden-" + id
	m.BankedCost = 0
	m.DesiredMachineID = ""
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed machine %s: %v", id, err)
	}
}

func machineRow(t *testing.T, s *apiServer, machine string) map[string]any {
	t.Helper()
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	for _, raw := range d["machines"].([]any) {
		row := raw.(map[string]any)
		if row["machine"] == machine {
			return row
		}
	}
	t.Fatalf("no machine row for %q in %v", machine, d["machines"])
	return nil
}

// TestGetMonitoring_StaleHardwareReadsAsNoData: the counterfactual. A machine
// that reported 47% CPU and then went dark must stop presenting that number.
func TestGetMonitoring_StaleHardwareReadsAsNoData(t *testing.T) {
	s, age := freshnessServer(t,
		`{"cpu_pct": 47, "ram_pct": 61, "battery_pct": 88, "ac_power": true}`)
	age(telemetryFreshSecs + 1) // reported, then went away

	row := machineRow(t, s, "m-abc123")
	for _, key := range []string{"cpu_pct", "ram_pct", "battery_pct", "ac_power"} {
		if got, present := row[key]; !present || got != nil {
			t.Errorf("%s = %v, want null — a sample older than %vs is not a live "+
				"measurement and must read as no data, not as the last value",
				key, got, telemetryFreshSecs)
		}
	}
	// The machine itself must NOT vanish: "this host exists but nobody has
	// measured it lately" is exactly the honest state we want on screen.
	if row["agents"] == nil {
		t.Errorf("the machine row itself must survive a stale sample; got %v", row)
	}
}

// TestGetMonitoring_FreshHardwareStillServed is the SENTINEL: the TTL must not
// be so eager that it kills healthy data. One heartbeat cadence of age (30s) is
// the NORMAL steady state — every machine on screen is usually this old.
func TestGetMonitoring_FreshHardwareStillServed(t *testing.T) {
	s, age := freshnessServer(t, `{"cpu_pct": 47, "ram_pct": 61, "ac_power": false}`)
	// Straight off the real ingest path, with NOTHING rewritten: a sample that
	// just arrived must be served. (This is what goes red if the ingest handler
	// stops stamping hardware_ts — an unstamped sample is fail-closed stale, so
	// dropping the stamp would black out every healthy machine.)
	if got := machineRow(t, s, "m-abc123")["cpu_pct"]; got != 47.0 {
		t.Fatalf("freshly ingested cpu_pct = %v, want 47", got)
	}
	for _, seconds := range []float64{0, 30, telemetryFreshSecs - 1} {
		age(seconds)
		row := machineRow(t, s, "m-abc123")
		if row["cpu_pct"] != 47.0 {
			t.Errorf("age %vs: cpu_pct = %v, want 47 — a healthy machine that has "+
				"missed at most two heartbeats must never flicker to no-data",
				seconds, row["cpu_pct"])
		}
		if row["ac_power"] != false {
			t.Errorf("age %vs: ac_power = %v, want false (a real false, not dropped)",
				seconds, row["ac_power"])
		}
	}
}

// TestGetMonitoring_UnstampedHardwareIsNotFresh: fail-closed. An entry carrying
// hardware with no hardware_ts has an UNKNOWN sample age, and unknown age must
// never be presented as a live reading.
func TestGetMonitoring_UnstampedHardwareIsNotFresh(t *testing.T) {
	s, _ := freshnessServer(t, `{"cpu_pct": 47}`)
	entry := s.telemetry.Get("mira")
	delete(entry, "hardware_ts")
	s.telemetry.Set("mira", entry)

	row := machineRow(t, s, "m-abc123")
	if got := row["cpu_pct"]; got != nil {
		t.Errorf("cpu_pct = %v, want null — hardware of unknown age is not fresh", got)
	}
	// And the stamp fields go with it. This half was untested and it is the half
	// that decides what the cockpit SAYS: withholding the number while emitting a
	// stamp (or a stale verdict) would claim "measured at <some time>, too old",
	// which is a fact the server does not have. An undateable sample is reported
	// as no sample — the same honest blank a machine that never measured gets.
	if got, present := row["hardware_ts"]; !present || got != nil {
		t.Errorf("hardware_ts = %v, want null — a sample the server cannot date "+
			"must not be given a time it never had", got)
	}
	if got, present := row["hardware_stale"]; !present || got != nil {
		t.Errorf("hardware_stale = %v, want null — 'stale' is a verdict about a "+
			"known age; unknown age is neither fresh nor stale", got)
	}
}

// TestGetMonitoring_LaterReportWithoutHardwareCannotRefreshIt: the freshness
// verdict is about the HARDWARE SAMPLE, not about the entry. A command_result
// receipt or an identity-only heartbeat advances entry["ts"] while carrying no
// hardware at all — reading the entry ts would let it resurrect an arbitrarily
// old CPU number, which is the same lie in a new costume.
func TestGetMonitoring_LaterReportWithoutHardwareCannotRefreshIt(t *testing.T) {
	s, age := freshnessServer(t, `{"cpu_pct": 47}`)
	age(telemetryFreshSecs + 1)
	// A hardware-less report lands NOW: entry["ts"] jumps to the present.
	if rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","cost": 1.5}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := s.telemetry.Get("mira")
	if ts, _ := entry["ts"].(float64); nowSecs()-ts > 5 {
		t.Fatalf("precondition: the entry ts must have been refreshed, got %v", ts)
	}
	if got := machineRow(t, s, "m-abc123")["cpu_pct"]; got != nil {
		t.Errorf("cpu_pct = %v, want null — a report carrying no hardware must not "+
			"make old hardware look freshly measured", got)
	}
}

// TestHandleIngestTelemetry_StampsHardwareSampleTime: the ingest half of the
// freshness contract. hardware and hardware_ts move together, and a report that
// carries no hardware must leave the previous sample's stamp ALONE (it did not
// measure anything, so it has nothing to vouch for).
func TestHandleIngestTelemetry_StampsHardwareSampleTime(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	if rec := doIngestTelemetry(api, "m-1", "m-1",
		`{"hardware": {"cpu_pct": 1}}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	stamp, ok := api.telemetry.Get("m-1")["hardware_ts"].(float64)
	if !ok || nowSecs()-stamp > 5 {
		t.Fatalf("hardware_ts = %v (ok=%v), want the sample time", stamp, ok)
	}
	// Rewind, then send a report with NO hardware block.
	entry := api.telemetry.Get("m-1")
	entry["hardware_ts"] = 1000.0
	api.telemetry.Set("m-1", entry)
	if rec := doIngestTelemetry(api, "m-1", "m-1", `{"cost": 2.5}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := api.telemetry.Get("m-1")["hardware_ts"].(float64); got != 1000.0 {
		t.Errorf("hardware_ts = %v, want 1000 untouched — a hardware-less report "+
			"must not vouch for a sample it did not take", got)
	}
}

// ---------------------------------------------------------------------------
// Per-machine sample stamps on the wire (T-b36a step 2b)
//
// Nulling an expired sample fixed the confident-wrong number, but it left two
// different worlds looking identical on screen: "this box has never reported
// hardware" and "this box reported, then went away an hour ago". The second is
// the one an operator has to act on. The stamp is what tells them apart, and it
// is the reason the fold keeps the timestamp of a sample whose VALUES it refuses
// to serve.
// ---------------------------------------------------------------------------

// TestGetMonitoring_StaleHardwareKeepsItsStamp: expired values, surviving stamp.
func TestGetMonitoring_StaleHardwareKeepsItsStamp(t *testing.T) {
	s, age := freshnessServer(t, `{"cpu_pct": 47, "ram_pct": 61}`)
	age(telemetryFreshSecs + 600)

	row := machineRow(t, s, "m-abc123")
	if got := row["cpu_pct"]; got != nil {
		t.Errorf("cpu_pct = %v, want null (the sample expired)", got)
	}
	ts, ok := row["hardware_ts"].(float64)
	if !ok {
		t.Fatalf("hardware_ts = %v, want the sample time — without it an expired "+
			"machine is indistinguishable from one that never reported hardware, "+
			"which is the whole reason the numbers could be trusted too long",
			row["hardware_ts"])
	}
	if age := nowSecs() - ts; age < telemetryFreshSecs {
		t.Errorf("hardware_ts is %.0fs old, want the ORIGINAL sample time (~%.0fs) — "+
			"a stamp that advances on read would say the expired numbers are fresh",
			age, telemetryFreshSecs+600)
	}
	// The stamp alone does not tell a client WHY the numbers are missing; it
	// would have to re-derive the window against its own clock to find out. The
	// verdict rides along so the threshold keeps exactly one home.
	if got := row["hardware_stale"]; got != true {
		t.Errorf("hardware_stale = %v, want true — this is the field that says the "+
			"blanks mean 'nobody has measured this box lately' rather than "+
			"'this box has never reported hardware'", got)
	}
}

// TestGetMonitoring_NeverReportedHardwareHasNoStamp is the other half: a machine
// with no sample must not get a fabricated one.
func TestGetMonitoring_NeverReportedHardwareHasNoStamp(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	seedRegisteredMachine(t, s, "m-abc123")
	if rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","cost":1.5}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	row := machineRow(t, s, "m-abc123")
	if got, present := row["hardware_ts"]; !present || got != nil {
		t.Errorf("hardware_ts = %v, want null — no sample means no sample time", got)
	}
	if got, present := row["hardware_stale"]; !present || got != nil {
		t.Errorf("hardware_stale = %v, want null — a machine that never measured is "+
			"not 'stale'; branding it so would send an operator after a box that "+
			"has nothing to report in the first place", got)
	}
	if got, present := row["runtime_capabilities_stale"]; !present || got != nil {
		t.Errorf("runtime_capabilities_stale = %v, want null — a machine that never "+
			"probed is not 'fresh' and not 'stale', it is unknown", got)
	}
}

// TestGetMonitoring_FreshHardwareCarriesAStampAndItsZeroes is the SENTINEL for
// the honest-zero problem: 0 and false are real measurements, and the easiest
// way to break this fold is to treat them as "no data".
func TestGetMonitoring_FreshHardwareCarriesAStampAndItsZeroes(t *testing.T) {
	s, _ := freshnessServer(t,
		`{"cpu_pct": 0, "ram_pct": 0, "battery_pct": 0, "ac_power": false}`)
	row := machineRow(t, s, "m-abc123")
	for _, key := range []string{"cpu_pct", "ram_pct", "battery_pct"} {
		if row[key] != 0.0 {
			t.Errorf("%s = %v, want 0 — a measured zero is data, not absence", key, row[key])
		}
	}
	if row["ac_power"] != false {
		t.Errorf("ac_power = %v, want false (a real false, not dropped)", row["ac_power"])
	}
	ts, ok := row["hardware_ts"].(float64)
	if !ok || nowSecs()-ts > 5 {
		t.Errorf("hardware_ts = %v, want the just-taken sample time", row["hardware_ts"])
	}
	// Straight off the real ingest path, nothing rewritten: a sample that just
	// arrived must be declared FRESH. This is what goes red if the verdict is
	// ever wired backwards — and a wrong `true` here would brand every healthy
	// machine on screen as out of date.
	if got := row["hardware_stale"]; got != false {
		t.Errorf("hardware_stale = %v, want false — a sample taken seconds ago is "+
			"current, and its measured zeroes are real readings", got)
	}
}

// runtimeCapabilityServer seeds one member that reported a capability probe from
// host m-abc123, plus a hook that rewrites how long ago it was probed.
func runtimeCapabilityServer(t *testing.T) (*apiServer, func(ageSecs float64)) {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	// The capability probe rides the WARDEN's own heartbeat, and a warden member
	// IS the machine (same id) — that keying is what machineRuntimeCapabilities
	// reads, so the fixture has to be a warden, not an agent sitting on the host.
	warden := fullMember("m-abc123")
	warden.Kind = "warden"
	warden.RoleKey = ""
	warden.DesiredMachineID = "m-abc123"
	if err := s.dal.PutMember(warden); err != nil {
		t.Fatalf("seed warden: %v", err)
	}
	if rec := doIngestTelemetry(s, "m-abc123", "m-abc123",
		`{"runtimes":{"claude":{"installed":true,"logged_in":true,"version":"2.1.211"},
			"codex":{"installed":true,"logged_in":false,"version":"0.52.0"}}}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	if len(s.machineRuntimeCapabilities("m-abc123")) != 2 {
		t.Fatalf("precondition: the probe did not land on the machine (%v)",
			s.machineRuntimeCapabilities("m-abc123"))
	}
	return s, func(ageSecs float64) {
		entry := s.telemetry.Get("m-abc123")
		entry["runtimes_ts"] = nowSecs() - ageSecs
		s.telemetry.Set("m-abc123", entry)
	}
}

func capabilityOf(t *testing.T, row map[string]any, runtime string) map[string]any {
	t.Helper()
	caps, _ := row["runtime_capabilities"].(map[string]any)
	capability, _ := caps[runtime].(map[string]any)
	if capability == nil {
		t.Fatalf("no runtime_capabilities.%s in %v", runtime, row)
	}
	return capability
}

// TestGetMonitoring_StaleRuntimeCapabilitiesAreMarkedNotBlanked: a machine that
// probed and then went dark must not present its old readiness as current — but
// the values stay, because they are the only explanation an operator has for a
// worker stuck on machine_unavailable. Marked, not deleted.
func TestGetMonitoring_StaleRuntimeCapabilitiesAreMarkedNotBlanked(t *testing.T) {
	s, age := runtimeCapabilityServer(t)
	age(telemetryFreshSecs + 1)

	row := machineRow(t, s, "m-abc123")
	if row["runtime_capabilities_stale"] != true {
		t.Errorf("runtime_capabilities_stale = %v, want true — past the window this "+
			"map is a memory, and rendering it plain is a second field that lies "+
			"the way the hardware numbers used to", row["runtime_capabilities_stale"])
	}
	if _, ok := row["runtime_capabilities_ts"].(float64); !ok {
		t.Errorf("runtime_capabilities_ts = %v, want the probe time",
			row["runtime_capabilities_ts"])
	}
	if got := capabilityOf(t, row, "codex")["logged_in"]; got != false {
		t.Errorf("codex logged_in = %v, want the reported false to SURVIVE — it is "+
			"the only surface that explains why codex work will not place here", got)
	}
}

// TestGetMonitoring_FreshRuntimeCapabilitiesAreNotStale is the sentinel: a
// machine heartbeating normally must never be marked stale, and its honest
// false must arrive as false.
func TestGetMonitoring_FreshRuntimeCapabilitiesAreNotStale(t *testing.T) {
	s, age := runtimeCapabilityServer(t)
	// Straight off the real ingest path, with NOTHING rewritten: a probe that
	// just arrived must read as current. This is what goes red if the ingest
	// handler stops stamping runtimes_ts — an unstamped map is fail-closed
	// stale, so dropping the stamp would mark every healthy machine out of date.
	if got := machineRow(t, s, "m-abc123")["runtime_capabilities_stale"]; got != false {
		t.Fatalf("freshly ingested runtime_capabilities_stale = %v, want false", got)
	}
	for _, seconds := range []float64{0, 30, telemetryFreshSecs - 1} {
		age(seconds)
		row := machineRow(t, s, "m-abc123")
		if row["runtime_capabilities_stale"] != false {
			t.Errorf("age %vs: runtime_capabilities_stale = %v, want false — a machine "+
				"that has missed at most two heartbeats is not out of date",
				seconds, row["runtime_capabilities_stale"])
		}
		if got := capabilityOf(t, row, "codex")["logged_in"]; got != false {
			t.Errorf("age %vs: codex logged_in = %v, want false", seconds, got)
		}
		if got := capabilityOf(t, row, "claude")["installed"]; got != true {
			t.Errorf("age %vs: claude installed = %v, want true", seconds, got)
		}
	}
}

// TestGetMonitoring_UnstampedRuntimeCapabilitiesAreNotFresh: fail-closed, the
// same reading hardware gets. A map whose age is unknown is not current.
func TestGetMonitoring_UnstampedRuntimeCapabilitiesAreNotFresh(t *testing.T) {
	s, _ := runtimeCapabilityServer(t)
	entry := s.telemetry.Get("m-abc123")
	delete(entry, "runtimes_ts")
	s.telemetry.Set("m-abc123", entry)

	row := machineRow(t, s, "m-abc123")
	if row["runtime_capabilities_stale"] != true {
		t.Errorf("runtime_capabilities_stale = %v, want true — unknown age is not freshness",
			row["runtime_capabilities_stale"])
	}
}

// TestGetMonitoring_ReportWithoutRuntimesCannotRefreshThem: the verdict is about
// the PROBE, not about the entry. A hardware-only heartbeat advances the entry
// ts while carrying no capability probe at all.
func TestGetMonitoring_ReportWithoutRuntimesCannotRefreshThem(t *testing.T) {
	s, age := runtimeCapabilityServer(t)
	age(telemetryFreshSecs + 1)
	if rec := doIngestTelemetry(s, "m-abc123", "m-abc123",
		`{"hardware":{"cpu_pct":47}}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	row := machineRow(t, s, "m-abc123")
	if row["cpu_pct"] != 47.0 {
		t.Fatalf("precondition: the new hardware sample must be served, got %v", row["cpu_pct"])
	}
	if row["runtime_capabilities_stale"] != true {
		t.Errorf("runtime_capabilities_stale = %v, want true — a report carrying no "+
			"capability probe must not make an old one look freshly measured",
			row["runtime_capabilities_stale"])
	}
}

// ---------------------------------------------------------------------------
// T-fc2f — the accounts overview must include OUTSOURCE usage.
//
// Root cause the sentinels below exist to keep dead: the three VALUE folds
// (acctByHost / freshRL → five_hour+seven_day / acctCost) iterated `members`,
// and at that time `dal.ListMembers()` was `WHERE kind != 'outsource'` — so SQL
// removed every worker before the fold ever ran. (The clause is gone since T-14
// 項目 6; `members` is now narrowed by the handler's own driver guard instead,
// which is what TestGetMonitoring_LiveContractorCountsAsOneAgentNotTwo pins. The
// root cause below is unchanged — only the mechanism that produced it is.) The accounts row itself was still MINTED,
// because the raw-key loop near the end of the handler scans the WHOLE
// telemetry snapshot. Net effect: a green card with three dashes.
//
// ⚠️ TestGetMonitoring_WorkerReportedLabelResolvesSessionAccount (above) does
// NOT cover this. It seeds a member holding the very same key, so its
// assertions pass with the member-only fold — the fixture supplies the value
// the behaviour under test is supposed to supply. Do not treat it as coverage.
//
// ⚠️ THE FIVE TESTS BELOW ARE NOT ALL THE SAME KIND OF TEST. Know which is
// which before you trust one of them to protect you.
//
// KEEP THIS LIST COMPLETE. Every test in this block must appear in exactly one
// category below. An earlier revision said "four tests" while five existed and
// silently omitted one — which is precisely the failure mode this block exists
// to prevent, committed by the block itself. If you add a sentinel here, run it
// against e4a8872 and file it before you commit.
//
//	GAP-GUARDS — VERIFIED to FAIL against the base implementation (e4a8872) by
//	checking that impl out and running them, not by reasoning. These are the
//	reason this ticket is fixed; break the fix and they go red:
//	  - WorkerOnlyAccountFillsMachineCostAndValidWindows
//	  - SharedAccountSumsMemberAndWorkerExactlyOnce (dual-purpose: it fails on
//	    base because the worker's share is missing, AND it pins the exact total
//	    so widening the fold cannot double-count either side)
//	  - ReleasedWorkerSpendStaysInTheAccount
//	  - ReleasedOnlyHostKeepsRowAndAccountButCountsZeroAgents (fails on base
//	    because the row and its account attribution do not exist there at all)
//
//	REGRESSION-GUARD — VERIFIED to already PASS on e4a8872. It discriminates
//	nothing about the T-fc2f gap; it exists so that widening the fold to
//	workers does not quietly cost us a property we already had:
//	  - WorkerAccountStillNeedsProvenance (guards the T-69bc runtime gate)
//
// Do not cite the regression-guard as evidence that the outsource gap is
// closed.
// ---------------------------------------------------------------------------

// seedWorker persists one outsource worker (a kind='outsource' member row, the
// only thing ListOutsourceWorkers can see) with the given banked balance.
func seedWorker(t *testing.T, s *apiServer, id, codename string, banked float64, status string) {
	t.Helper()
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: id, Codename: codename, Runtime: RuntimeClaude, Model: "opus",
		Effort: "medium", TaskID: "t-1", Status: status,
		CreatedTS: 1.0, DesiredState: "online", BankedCost: banked,
	}); err != nil {
		t.Fatalf("seed worker %s: %v", id, err)
	}
}

// accountRow returns the accounts row for key, or fails.
func accountRow(t *testing.T, d map[string]any, key string) map[string]any {
	t.Helper()
	for _, raw := range d["accounts"].([]any) {
		row := raw.(map[string]any)
		if row["account"] == key {
			return row
		}
	}
	t.Fatalf("no accounts row for %q in %v", key, d["accounts"])
	return nil
}

// machineRow returns the machines row for host, or nil.
func machineRowIn(d map[string]any, host string) map[string]any {
	for _, raw := range d["machines"].([]any) {
		row := raw.(map[string]any)
		if row["machine"] == host {
			return row
		}
	}
	return nil
}

// Worker fold fixtures use valid windows.  T-6166 deliberately changed the
// old partial-window contract: owner decision rc-2fa255439eac requires an
// unusable reset time to render that whole window as null, which is covered by
// TestGetMonitoring_UnusableRateLimitWindowStaysEmpty instead.
func workerRateLimits(now float64) string {
	return fmt.Sprintf(`"rate_limits":{"five_hour":{"used_percentage":42.0,"resets_at":%g},`+
		`"seven_day":{"used_percentage":7.0,"resets_at":%g}}`,
		now+WindowSeconds["five_hour"]-60, now+WindowSeconds["seven_day"]-60)
}

func TestGetMonitoring_SameWindowUsesNewestRateLimitSnapshot(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	for _, id := range []string{"new-session", "old-session"} {
		m := fullMember(id)
		m.RoleKey = "builder"
		m.Runtime = RuntimeCodex
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	resetAt := nowSecs() + 3600
	if rec := doIngestTelemetry(s, "new-session", "m-seth-m5", `{"runtime":"codex","account":"codex:seth",`+
		fmt.Sprintf(`"rate_limits":{"seven_day":{"used_percentage":40,"resets_at":%g}}}`, resetAt)); rec.Code != 200 {
		t.Fatalf("new snapshot ingest: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doIngestTelemetry(s, "old-session", "m-seth-m5", `{"runtime":"codex","account":"codex:seth",`+
		fmt.Sprintf(`"rate_limits":{"seven_day":{"used_percentage":66,"resets_at":%g}}}`, resetAt)); rec.Code != 200 {
		t.Fatalf("old snapshot ingest: %d %s", rec.Code, rec.Body.String())
	}
	old := s.telemetry.Get("old-session")
	oldRateLimitsTS, ok := old["rate_limits_ts"].(float64)
	if !ok {
		t.Fatal("precondition: rate-limit report must receive its own timestamp")
	}
	if rec := doIngestTelemetry(s, "old-session", "m-seth-m5",
		`{"runtime":"codex","account":"codex:seth"}`); rec.Code != 200 {
		t.Fatalf("identity heartbeat ingest: %d %s", rec.Code, rec.Body.String())
	}
	old = s.telemetry.Get("old-session")
	if got, _ := old["rate_limits_ts"].(float64); got != oldRateLimitsTS {
		t.Fatalf("heartbeat changed rate_limits_ts from %v to %v", oldRateLimitsTS, got)
	}
	newer := s.telemetry.Get("new-session")
	newer["rate_limits_ts"] = 200.0
	newer["ts"] = 100.0
	s.telemetry.Set("new-session", newer)
	old["rate_limits_ts"] = 100.0
	old["ts"] = 300.0
	s.telemetry.Set("old-session", old)

	row := accountRow(t, monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})), "codex:seth")
	sevenDay, ok := row["seven_day"].(map[string]any)
	if !ok {
		t.Fatalf("seven_day = %v, want the newer snapshot's shaped window", row["seven_day"])
	}
	if got := sevenDay["used_pct"]; got != 40.0 {
		t.Errorf("seven_day.used_pct = %v, want 40 from the newer rate-limit snapshot", got)
	}
	if got := sevenDay["resets_at"]; got != resetAt {
		t.Errorf("seven_day.resets_at = %v, want the newer snapshot's reset time", got)
	}
}

func TestGetMonitoring_NewerWindowBeatsNewerRateLimitSnapshot(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	for _, id := range []string{"current-window", "expired-window"} {
		m := fullMember(id)
		m.RoleKey = "builder"
		m.Runtime = RuntimeCodex
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	now := nowSecs()
	currentResetAt := now + 3600
	olderResetAt := now + 1800
	if rec := doIngestTelemetry(s, "current-window", "m-seth-m5", `{"runtime":"codex","account":"codex:seth",`+
		fmt.Sprintf(`"rate_limits":{"seven_day":{"used_percentage":40,"resets_at":%g}}}`, currentResetAt)); rec.Code != 200 {
		t.Fatalf("current window ingest: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doIngestTelemetry(s, "expired-window", "m-seth-m5", `{"runtime":"codex","account":"codex:seth",`+
		fmt.Sprintf(`"rate_limits":{"seven_day":{"used_percentage":66,"resets_at":%g}}}`, olderResetAt)); rec.Code != 200 {
		t.Fatalf("expired window ingest: %d %s", rec.Code, rec.Body.String())
	}
	current := s.telemetry.Get("current-window")
	current["rate_limits_ts"] = 100.0
	s.telemetry.Set("current-window", current)
	expired := s.telemetry.Get("expired-window")
	expired["rate_limits_ts"] = 300.0
	s.telemetry.Set("expired-window", expired)

	row := accountRow(t, monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})), "codex:seth")
	sevenDay, ok := row["seven_day"].(map[string]any)
	if !ok {
		t.Fatalf("seven_day = %v, want the current window", row["seven_day"])
	}
	if got := sevenDay["used_pct"]; got != 40.0 {
		t.Errorf("seven_day.used_pct = %v, want 40 from the current window", got)
	}
	if got := sevenDay["resets_at"]; got != currentResetAt {
		t.Errorf("seven_day.resets_at = %v, want %v from the current window", got, currentResetAt)
	}
}

func TestGetMonitoring_PartialRateLimitSnapshotsChooseEachWindowIndependently(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	for _, id := range []string{"full-snapshot", "five-hour-snapshot"} {
		m := fullMember(id)
		m.RoleKey = "builder"
		m.Runtime = RuntimeCodex
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	now := nowSecs()
	if rec := doIngestTelemetry(s, "full-snapshot", "m-seth-m5", fmt.Sprintf(`{"runtime":"codex","account":"codex:seth",`+
		`"rate_limits":{"five_hour":{"used_percentage":66,"resets_at":%g},`+
		`"seven_day":{"used_percentage":66,"resets_at":%g}}}`,
		now+1800, now+WindowSeconds["seven_day"]-1800)); rec.Code != 200 {
		t.Fatalf("full snapshot ingest: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doIngestTelemetry(s, "five-hour-snapshot", "m-seth-m5", fmt.Sprintf(`{"runtime":"codex","account":"codex:seth",`+
		`"rate_limits":{"five_hour":{"used_percentage":40,"resets_at":%g}}}`,
		now+3600)); rec.Code != 200 {
		t.Fatalf("five-hour snapshot ingest: %d %s", rec.Code, rec.Body.String())
	}

	row := accountRow(t, monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})), "codex:seth")
	fiveHour, ok := row["five_hour"].(map[string]any)
	if !ok || fiveHour["used_pct"] != 40.0 {
		t.Errorf("five_hour = %v, want the newer five-hour window", row["five_hour"])
	}
	sevenDay, ok := row["seven_day"].(map[string]any)
	if !ok || sevenDay["used_pct"] != 66.0 {
		t.Errorf("seven_day = %v, want the only reported seven-day window", row["seven_day"])
	}
}

func TestGetMonitoring_UnusableRateLimitWindowStaysEmpty(t *testing.T) {
	now := nowSecs()
	cases := map[string]string{
		"missing":    `{}`,
		"nonnumeric": `{"five_hour":{"used_percentage":40,"resets_at":"bad"}}`,
		"zero":       `{"five_hour":{"used_percentage":40,"resets_at":0}}`,
		"negative":   `{"five_hour":{"used_percentage":40,"resets_at":-1}}`,
		"expired":    fmt.Sprintf(`{"five_hour":{"used_percentage":40,"resets_at":%g}}`, now-1),
		"too future": fmt.Sprintf(`{"five_hour":{"used_percentage":40,"resets_at":%g}}`, now+2*WindowSeconds["five_hour"]),
	}
	for name, rateLimits := range cases {
		t.Run(name, func(t *testing.T) {
			s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
				telemetry: newMemStore(), gauge: newMemStore()}
			m := fullMember("session")
			m.RoleKey = "builder"
			m.Runtime = RuntimeCodex
			if err := s.dal.PutMember(m); err != nil {
				t.Fatalf("seed member: %v", err)
			}
			if rec := doIngestTelemetry(s, "session", "m-seth-m5", `{"runtime":"codex","account":"codex:seth","rate_limits":`+rateLimits+`}`); rec.Code != 200 {
				t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
			}
			row := accountRow(t, monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})), "codex:seth")
			if got := row["five_hour"]; got != nil {
				t.Errorf("five_hour = %v, want nil for unusable window", got)
			}
			if got := row["seven_day"]; got != nil {
				t.Errorf("seven_day = %v, want nil when every window is unusable", got)
			}
		})
	}
}

func TestGetMonitoring_EqualRateLimitCandidatesKeepExistingWindow(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), telemetry: newMemStore(), gauge: newMemStore()}
	for _, id := range []string{"a-session", "b-session"} {
		m := fullMember(id)
		m.RoleKey = "builder"
		m.Runtime = RuntimeCodex
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	resetAt := nowSecs() + 3600
	for id, used := range map[string]int{"a-session": 40, "b-session": 66} {
		if rec := doIngestTelemetry(s, id, "m-seth-m5", fmt.Sprintf(`{"runtime":"codex","account":"codex:seth","rate_limits":{"five_hour":{"used_percentage":%d,"resets_at":%g}}}`, used, resetAt)); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", id, rec.Code, rec.Body.String())
		}
		entry := s.telemetry.Get(id)
		entry["rate_limits_ts"] = 100.0
		s.telemetry.Set(id, entry)
	}
	row := accountRow(t, monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})), "codex:seth")
	fiveHour, ok := row["five_hour"].(map[string]any)
	if !ok || fiveHour["used_pct"] != 40.0 {
		t.Errorf("five_hour = %v, want the existing candidate", row["five_hour"])
	}
}

func TestUsableRateLimitWindow_EndedWindowIsRejected(t *testing.T) {
	window, _, usable := usableRateLimitWindow(map[string]any{"resets_at": 100.0}, WindowSeconds["five_hour"], 100.0)
	if usable || window != nil {
		t.Errorf("ended window = %v, usable = %v; want rejected", window, usable)
	}
}

// TestGetMonitoring_WorkerOnlyAccountFillsMachineCostAndValidWindows is the primary
// T-fc2f sentinel. The world is deliberately WORKER-ONLY for the key under
// test: a staff member exists and reports telemetry, but holds NO account at
// all, so every value asserted here can only have come from the outsource
// worker. Under the member-only fold all four cells are dash/absent and the
// machines section is empty — that is the owner-reported eva-m5-claude shape.
func TestGetMonitoring_WorkerOnlyAccountFillsMachineCostAndValidWindows(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	// A staff member that holds no account key — proves "a member exists" is not
	// what makes the assertions below pass.
	m := fullMember("mira")
	m.RoleKey = "builder"
	m.BankedCost = 0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if rec := doIngestTelemetry(s, "mira", "m-seth-m5", `{"runtime":"claude"}`); rec.Code != 200 {
		t.Fatalf("member ingest: %d %s", rec.Code, rec.Body.String())
	}
	// m-eva-m5 is a REGISTERED machine (T-b89d): a worker can only ever be
	// spawned onto a warden member id, so this is the world production makes.
	seedRegisteredMachine(t, s, "m-eva-m5")
	seedWorker(t, s, "ow-eva", "E1", 2.5, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-eva", "m-eva-m5",
		`{"runtime":"claude","account":"eva-m5-claude","cost":1.25,`+
			workerRateLimits(nowSecs())+`}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := accountRow(t, d, "eva-m5-claude")

	// (1) machine — the box the worker is burning on.
	if row["machine"] != "m-eva-m5" {
		t.Errorf("machine = %v, want m-eva-m5 — a worker-only account must still "+
			"attribute to the host it runs on", row["machine"])
	}
	// (2) cost — what the worker REPORTED (1.25). Its 2.5 banked balance was
	// seeded straight into the database and never reported, so it is not in the
	// account's own accumulator (rc-5c5d7c7c6dcd).
	if row["cost"] != 1.25 {
		t.Errorf("cost = %v, want 1.25 (the figure the worker reported)", row["cost"])
	}
	// (3)+(4) both rate-limit windows.
	for _, win := range []string{"five_hour", "seven_day"} {
		shaped, ok := row[win].(map[string]any)
		if !ok {
			t.Fatalf("%s = %v, want a shaped window — an outsource session burns "+
				"the same quota as a member one", win, row[win])
		}
		if shaped["used_pct"] == nil {
			t.Errorf("%s.used_pct is nil, want the worker-reported figure", win)
		}
	}
	// (5) the machines section must carry a row for the worker-only host, and
	// that row must name the account. A host carrying nothing but workers did
	// not exist at all before this fix.
	mr := machineRowIn(d, "m-eva-m5")
	if mr == nil {
		t.Fatalf("no machines row for m-eva-m5 in %v", d["machines"])
	}
	found := false
	for _, a := range mr["accounts"].([]any) {
		if a == "eva-m5-claude" {
			found = true
		}
	}
	if !found {
		t.Errorf("machines[m-eva-m5].accounts = %v, want it to contain eva-m5-claude",
			mr["accounts"])
	}
}

// TestGetMonitoring_SharedAccountSumsMemberAndWorkerExactlyOnce is the reverse
// sentinel: the seth-m5-claude shape, a key held by a staff member AND an
// outsource worker at once. It pins the exact total, so widening the fold to
// `members ∪ workers` may not double-count either side's live cost or banked
// balance. If members and workers ever stop being disjoint, this goes red. ⚠️
// They are NOT disjoint by SQL any more (T-14 項目 6 deleted ListMembers'
// `WHERE kind != 'outsource'`): the handler's own driver guard is what separates
// them now, so this test's premise is a guard someone can delete, not a query.
func TestGetMonitoring_SharedAccountSumsMemberAndWorkerExactlyOnce(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("seth")
	m.RoleKey = "builder"
	m.BankedCost = 4.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if rec := doIngestTelemetry(s, "seth", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":1.0}`); rec.Code != 200 {
		t.Fatalf("member ingest: %d %s", rec.Code, rec.Body.String())
	}
	seedWorker(t, s, "ow-7", "S7", 0.25, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":0.5}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := accountRow(t, d, "seth-m5-claude")
	// 1.0 (member) + 0.5 (worker) — each REPORT contributes exactly once, which
	// is what this test has always been about; the seeded banked balances never
	// went through a report, so they are not in the accumulator
	// (rc-5c5d7c7c6dcd). A missing arm reads 0.5 or 1.0, a double-counted one 2.0.
	if row["cost"] != 1.5 {
		t.Errorf("cost = %v, want 1.5 (member 1.0, worker 0.5) — each actor's "+
			"report must contribute exactly once", row["cost"])
	}
	// Both sit on the same box, so the host set must stay a SET.
	if row["machine"] != "m-seth-m5" {
		t.Errorf("machine = %v, want the single host m-seth-m5", row["machine"])
	}
}

// TestGetMonitoring_ReleasedWorkerSpendStaysInTheAccount is the second
// gap-guard, and it is the INVERSE of what this test asserted in its first
// version. That first version filtered released workers out of the fold and
// asserted the values were absent — i.e. it pinned the T-fc2f BUG SYMPTOM as
// expected behaviour, which would have shipped the ticket unfixed.
//
// Why released must be INCLUDED (criterion: follow the telemetry lifecycle,
// not the roster status):
//   - released is the STEADY STATE for outsource workers, not an edge case.
//     ReleaseWorkersForTask fires on every task close (api_tasks.go closeTask)
//     and on every close-out report (dismissOutsourceWorkersForTask).
//   - a released worker's telemetry entry is NEVER deleted. The repo's only
//     s.telemetry.Delete is api_roles.go, on staff hard-delete. So the raw-key
//     loop still mints the account row while a filtered fold starves it — the
//     green-card-with-dashes shape, verbatim.
//   - SPEC §6.3 keeps the released worker's SESSION alive on purpose to run
//     close-out duties, so it is still live and still spending.
//   - money already spent is a historical fact; a cumulative total that jumps
//     backwards when a task closes reads as broken data more readily than a
//     dash does.
//
// MUTANT: re-add `if wk.Status == WorkerStatusReleased { continue }` to the
// worker branch of `actors` -> RED here.
func TestGetMonitoring_ReleasedWorkerSpendStaysInTheAccount(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	// Released AND worker-only: no member holds this key, so every value below
	// can only have come from a released outsource worker. The box it ran on is
	// still REGISTERED — this test is about a released worker, not a removed
	// machine (that is TestGetMonitoring_RemovedMachineLeavesNoOrphanRow).
	seedRegisteredMachine(t, s, "m-eva-m5")
	seedWorker(t, s, "ow-gone", "G1", 9.0, WorkerStatusReleased)
	if rec := doIngestTelemetry(s, "ow-gone", "m-eva-m5",
		`{"runtime":"claude","account":"eva-m5-claude","cost":3.0,`+
			workerRateLimits(nowSecs())+`}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := accountRow(t, d, "eva-m5-claude")
	// The 3.0 it reported stays after the release. (The 9.0 banked balance was
	// seeded, never reported, so it is not in the accumulator — rc-5c5d7c7c6dcd.)
	if row["cost"] != 3.0 {
		t.Errorf("cost = %v, want 3 (the figure it reported) — a task close must "+
			"not make an account's cumulative spend jump backwards", row["cost"])
	}
	if row["machine"] != "m-eva-m5" {
		t.Errorf("machine = %v, want m-eva-m5 — a released worker still ran on a box",
			row["machine"])
	}
	for _, win := range []string{"five_hour", "seven_day"} {
		if _, ok := row[win].(map[string]any); !ok {
			t.Errorf("%s = %v, want a shaped window", win, row[win])
		}
	}
	mr := machineRowIn(d, "m-eva-m5")
	if mr == nil {
		t.Fatalf("no machines row for m-eva-m5 in %v", d["machines"])
	}
	if len(mr["accounts"].([]any)) == 0 {
		t.Errorf("machines[m-eva-m5].accounts is empty, want eva-m5-claude")
	}
}

// TestGetMonitoring_WorkerAccountStillNeedsProvenance pins that widening the
// fold to workers did NOT widen attribution. telemetryAccount's provenance gate
// (T-69bc / 2eb6590) is the other half of the read: an account with no runtime
// stamp, or one stamped for a runtime the actor has left, is unproven and must
// stay unreadable — for a worker exactly as for a member.
func TestGetMonitoring_WorkerAccountStillNeedsProvenance(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedWorker(t, s, "ow-eva", "E1", 0, WorkerStatusActive)
	// A CODEX-stamped key under a CLAUDE worker: proven, but not for this
	// runtime. The values must NOT be folded in under the worker's account.
	s.telemetry.Set("ow-eva", map[string]any{
		"machine": "m-eva-m5", "account": "codex:stale",
		accountRuntimeKey: RuntimeCodex, "cost": 2.0, "ts": 5.0,
	})
	// A second worker whose account carries NO provenance stamp at all, next to
	// an ordinary `runtime` field. That field is rewritten by every later
	// heartbeat, so falling back to it is exactly the "account borrowed from an
	// older runtime" regression 2eb6590 removed. Unstamped ⇒ unproven ⇒ empty.
	//
	// ⚠️ HONEST LIMITATION — do not read this arm as proof that the provenance
	// gate is guarded in production. This state is UNREACHABLE through the real
	// ingest path: applyAccountReport (account_display.go) is the sole writer of
	// entry["account"] in the repo, and its rule 2 (`account != "" && runtime ==
	// nil`) CLEARS the pairing rather than storing a stampless key — so
	// "account present, stamp absent" cannot be produced by any reporter. This
	// test reaches it only by writing the telemetry entry directly.
	//
	// Consequence, measured not assumed: a mutant that adds a
	// `reported == "" -> fall back to entry["runtime"]` branch to
	// telemetryAccount is killed ONLY by this arm. In reachable states that
	// mutant SURVIVES the whole suite. So what is guarded here is the pure
	// function's defense-in-depth, not an end-to-end property. Kept deliberately:
	// the invariant lives in a different file from the gate that relies on it,
	// and nothing forces them to stay in agreement.
	seedWorker(t, s, "ow-kyle", "K1", 0, WorkerStatusActive)
	s.telemetry.Set("ow-kyle", map[string]any{
		"machine": "m-kyle-m5", "account": "claude:unstamped",
		"runtime": RuntimeClaude, "cost": 8.0, "ts": 6.0,
	})

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := accountRow(t, d, "codex:stale")
	if row["cost"] != nil {
		t.Errorf("cost = %v, want null — a key whose provenance does not match the "+
			"worker's runtime must not be read under it", row["cost"])
	}
	if mr := machineRowIn(d, "m-eva-m5"); mr != nil && len(mr["accounts"].([]any)) != 0 {
		t.Errorf("unproven key must not attribute to a host: %v", mr["accounts"])
	}
	unstamped := accountRow(t, d, "claude:unstamped")
	if unstamped["cost"] != nil {
		t.Errorf("cost = %v, want null — an UNSTAMPED account must never inherit "+
			"provenance from the entry's mutable runtime field (2eb6590)",
			unstamped["cost"])
	}
	if mr := machineRowIn(d, "m-kyle-m5"); mr != nil && len(mr["accounts"].([]any)) != 0 {
		t.Errorf("unstamped key must not attribute to a host: %v", mr["accounts"])
	}
}

// TestGetMonitoring_ReleasedOnlyHostKeepsRowAndAccountButCountsZeroAgents is
// the third gap-guard, and it pins the ONE place where account attribution and
// agent counting must part ways.
//
//	acctByHost  — follows the TELEMETRY lifecycle: released included. The
//	              account was burned on that box; that is a historical fact.
//	hostCounts  — follows the LIVE actor set: released excluded. "How many
//	              agents are on this box" is a question about right now, and
//	              the member side has always answered it that way (the handler
//	              filters RosterStatusRemoved before it ever builds `actors`).
//
// ⚠️ The earlier revision of this file argued the opposite in a comment: that
// "a row claiming 0 agents while naming an account observed there would
// contradict itself", and used that to justify counting released workers.
// That argument is REJECTED and must not be reinstated. There is no
// contradiction: the account is money already spent on that box (history), the
// agent count is who is alive on it (present tense). One may be non-empty while
// the other is 0. A machine that has run forty closed-out tasks must not report
// forty agents when two are running — that number misleads the owner, and nobody
// asked for it. 0 is the honest answer.
//
// MUTANT: count released workers in hostCounts (e.g. drop the `a.live` guard)
// -> RED on the agents assertion here.
func TestGetMonitoring_ReleasedOnlyHostKeepsRowAndAccountButCountsZeroAgents(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	// m-eva-m5 is a registered machine that carries NOTHING but a released
	// worker — no live member, no live worker.
	seedRegisteredMachine(t, s, "m-eva-m5")
	seedWorker(t, s, "ow-gone", "G1", 9.0, WorkerStatusReleased)
	if rec := doIngestTelemetry(s, "ow-gone", "m-eva-m5",
		`{"runtime":"claude","account":"eva-m5-claude","cost":3.0,`+
			workerRateLimits(nowSecs())+`}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))

	// (1) the machines row must SURVIVE — `hosts` is derived from the host-key
	// set, so excluding released workers from the COUNT must not also stop the
	// key from being minted. Losing this row would drop the account's machine
	// attribution with it.
	mr := machineRowIn(d, "m-eva-m5")
	if mr == nil {
		t.Fatalf("machines row for m-eva-m5 vanished — host-key minting must be "+
			"separate from agent counting; got %v", d["machines"])
	}
	// (2) the account attribution must survive on that row.
	found := false
	for _, a := range mr["accounts"].([]any) {
		if a == "eva-m5-claude" {
			found = true
		}
	}
	if !found {
		t.Errorf("machines[m-eva-m5].accounts = %v, want it to contain eva-m5-claude "+
			"— the spend happened on this box", mr["accounts"])
	}
	// (3) ...and the released worker must contribute NOTHING to the agent count.
	//
	// ⚠️ The expected number is 1, not 0, and the 1 is not the worker. Every
	// registered machine's own warden IS a member (observedHost of a warden is
	// its own id), so it is an actor on its own row and has always been counted
	// live — that is pre-existing behaviour of this handler on 2e74953, not
	// something T-b89d introduced, and this fixture only started seeing it when
	// it started registering the machine (T-b89d: an unregistered host gets no
	// row at all, so the old expectation of 0 was only reachable in a world
	// where m-eva-m5 did not exist).
	//
	// The DISCRIMINATION is unchanged and is the whole point of the assertion:
	// the mutant this pins — counting released workers in hostCounts, e.g. by
	// dropping the countsAsPresentAgent guard — makes this 2. Verified, not
	// reasoned: 1 with the guard, 2 without it.
	if mr["agents"] != 1.0 {
		t.Errorf("agents = %v, want 1 (the box's own warden, and ONLY it) — a "+
			"released worker is not a live agent; counting it inflates the "+
			"number the owner reads", mr["agents"])
	}
	// (4) the account row keeps its machine attribution end-to-end.
	if row := accountRow(t, d, "eva-m5-claude"); row["machine"] != "m-eva-m5" {
		t.Errorf("accounts[eva-m5-claude].machine = %v, want m-eva-m5", row["machine"])
	}
}

// ---------------------------------------------------------------------------
// T-b89d — the machines list has an UPPER bound, not just a lower one.
//
// Every sentinel above guards "this row MUST exist". None of them guarded
// "this row must NOT exist", and the handler had no such bound at all: the row
// set was minted from telemetry `machine` strings, telemetry is append-only
// (the repo's only s.telemetry.Delete is the staff hard-delete in
// api_roles.go), and deleting a machine flips its warden member's roster
// status without touching a single telemetry entry. Net effect: a box the
// owner removed came back on EVERY request, forever, resurrected by the
// released workers that once ran on it.
//
// The fix is a membership rule, not a filter on values: WHICH BOXES EXIST is
// the machine roster's answer (kind=warden ∧ roster=active — the same
// predicate GET /api/machines lists), and telemetry only supplies the numbers
// for a box that exists. KEEP THIS LIST COMPLETE — one test, one category.
//
//	GAP-GUARDS — VERIFIED to FAIL on 2e74953 by running them against the
//	pre-filter handler, not by reasoning:
//	  - RemovedMachineLeavesNoOrphanRow (the orphan row; also the REVERSE
//	    sentinel — it asserts in the same body that the owner-reported
//	    eva-m5-claude attribution and spend are untouched)
//	  - UnplacedActorMintsNoBlankMachineRow (the machine:"" row)
//
//	REGRESSION-GUARDS — VERIFIED to already PASS on 2e74953. They discriminate
//	nothing about the orphan bug; they are here because the tempting cheap
//	fixes for it all go red on one of them — "key on presence" / "key on who
//	reported recently" on the first, "removed and uninstalled are the same
//	thing" on the second:
//	  - RegisteredButSilentMachineStillListed
//	  - UninstalledButUndeletedMachineStillListed
// ---------------------------------------------------------------------------

// TestGetMonitoring_RemovedMachineLeavesNoOrphanRow is the upper-bound
// sentinel this ticket exists for, and it is deliberately built on the
// HARDEST world for the fix to survive: the eva-m5-claude shape (a key held
// ONLY by an outsource worker, and a RELEASED one) sitting on the machine
// being removed. So the two halves pull against each other in one body:
//
//	the machines row must GO      — the owner removed that box
//	the account must NOT be hurt  — its cost and its historical machine
//	                                attribution are the owner-reported T-fc2f
//	                                bug, and must not regress
//
// Removal goes through the REAL route (DELETE /api/machines/{id}), not a
// hand-written roster flip, so the test also pins the premise the fix rests
// on: deleting a machine is a pure roster soft-delete that leaves the worker's
// telemetry `machine` string in place. If someone ever makes deletion clear
// telemetry too, this still passes — and that is fine, it is the same
// invariant reached another way.
//
// MUTANT: derive `hosts` from the observed host set (`for host := range
// hostCounts`) instead of from the roster -> RED on (1) here (the orphan row
// comes back). It is also RED under the T-fc2f mutant that filters released
// workers out of `actors` — on (2), which is the reverse half doing its job.
func TestGetMonitoring_RemovedMachineLeavesNoOrphanRow(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedRegisteredMachine(t, s, "m-eva-m5")
	seedWorker(t, s, "ow-gone", "G1", 9.0, WorkerStatusReleased)
	if rec := doIngestTelemetry(s, "ow-gone", "m-eva-m5",
		`{"runtime":"claude","account":"eva-m5-claude","cost":3.0,`+
			workerRateLimits(nowSecs())+`}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}
	// A SECOND worker on the same box that is NOT released — assigned, holding
	// no account, never online (so the delete route's 409 gate stays open).
	//
	// ⚠️ Load-bearing for the MUTANT, not for the story. Without it the only
	// actor naming this host is a released one, which contributes to no counter
	// at all, so reverting the row set to "hosts we observed" would leave this
	// test green and the guard would only bite in combination with something
	// else. With it, the host is observed by a live actor and the orphan row
	// comes back the instant the roster stops deciding membership. Measured:
	// green here without this worker under that mutant, red with it.
	seedWorker(t, s, "ow-assigned", "A1", 0, WorkerStatusAssigned)
	if rec := doIngestTelemetry(s, "ow-assigned", "m-eva-m5",
		`{"runtime":"claude"}`); rec.Code != 200 {
		t.Fatalf("second worker ingest: %d %s", rec.Code, rec.Body.String())
	}
	// Pre-condition: while the machine is registered, it HAS a row. Without
	// this the test could pass by asserting the absence of something that was
	// never there.
	if mr := machineRowIn(monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})),
		"m-eva-m5"); mr == nil {
		t.Fatalf("pre-condition: a registered machine must have a machines row")
	}

	// The owner removes the machine, through the route they would actually use.
	del := httptest.NewRecorder()
	s.HandleDeleteMachineApiMachinesMemberIdDelete(del,
		httptest.NewRequest("DELETE", "/api/machines/m-eva-m5", nil), "m-eva-m5")
	if del.Code != 200 {
		t.Fatalf("delete machine: %d %s", del.Code, del.Body.String())
	}
	// The premise, verified rather than assumed: removal did NOT touch the
	// worker's telemetry, so the orphan-minting input is still present.
	if got, _ := s.telemetry.Get("ow-gone")["machine"].(string); got != "m-eva-m5" {
		t.Fatalf("premise broken: worker telemetry machine = %q, want it to still "+
			"name the removed box (that is what used to mint the orphan)", got)
	}

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	// (1) THE TICKET: a machine the owner removed must not be in the list.
	if mr := machineRowIn(d, "m-eva-m5"); mr != nil {
		t.Errorf("removed machine still listed: %v — telemetry is append-only, so "+
			"a row set minted from it can only grow; membership must come from "+
			"the machine roster", mr)
	}
	// (2) REVERSE SENTINEL — the owner-reported T-fc2f bug must not regress.
	// A worker-only, released account still reports its full spend...
	row := accountRow(t, d, "eva-m5-claude")
	if row["cost"] != 3.0 {
		t.Errorf("cost = %v, want 3 (the figure it reported) — removing a machine "+
			"must not claw back money that was spent on it", row["cost"])
	}
	// ...its rate-limit windows...
	for _, win := range []string{"five_hour", "seven_day"} {
		if _, ok := row[win].(map[string]any); !ok {
			t.Errorf("%s = %v, want a shaped window", win, row[win])
		}
	}
	// ...and its machine attribution, which is HISTORY and stays truthful.
	// Deliberate: acctByHost is untouched by this fix. "That spend happened on
	// m-eva-m5" does not stop being true when the box is decommissioned, and
	// blanking it would delete the only record of where the money went.
	if row["machine"] != "m-eva-m5" {
		t.Errorf("accounts[eva-m5-claude].machine = %v, want m-eva-m5 — where the "+
			"money was burned is a historical fact, not a live inventory lookup",
			row["machine"])
	}
}

// TestGetMonitoring_RegisteredButSilentMachineStillListed pins the boundary
// that rules out every cheaper version of the fix. A machine that is
// registered but has NOTHING on it — no member, no worker, no telemetry of its
// own, no SSE connection — must still be listed. Existence is a roster fact,
// not a liveness fact: a laptop that is closed has not stopped being one of
// your machines, and a cockpit that hides it makes the owner think it was
// deleted.
//
// This is a REGRESSION-GUARD, not a gap-guard: it passes on 2e74953 too
// (the box's own warden member is an actor there as well, so the key was
// minted). It is here because "key on presence" / "key on recent telemetry"
// are the obvious ways to bound the machines list, and both go red here.
func TestGetMonitoring_RegisteredButSilentMachineStillListed(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	seedRegisteredMachine(t, s, "m-quiet")

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	mr := machineRowIn(d, "m-quiet")
	if mr == nil {
		t.Fatalf("a registered machine with nothing running on it vanished from "+
			"the list: %v — offline is not removed", d["machines"])
	}
	// It is honest about having nothing measured, rather than absent.
	if got, present := mr["hardware_ts"]; !present || got != nil {
		t.Errorf("hardware_ts = %v, want null", got)
	}
	if got := mr["accounts"].([]any); len(got) != 0 {
		t.Errorf("accounts = %v, want empty", got)
	}
}

// TestGetMonitoring_UninstalledButUndeletedMachineStillListed pins boundary 3:
// the machine roster predicate is roster_status, and DELIBERATELY not the
// lifecycle intent. `POST /api/machines/{id}/uninstall` is a ONE-SHOT intent
// (desired_state=uninstall, consumed back to offline when the warden really
// disconnects — see spec/lifecycle.md §4.3) that keeps the member record on
// purpose so the box can be re-installed. It never writes roster_status.
//
// So an uninstalled-but-undeleted box is still one of the owner's machines,
// GET /api/machines still lists it, and monitoring must agree. The two
// surfaces sharing one predicate is the reason this fix chose that predicate;
// a monitoring list that quietly hides a box the machines page still shows
// would be the same class of lie as the orphan row, pointing the other way.
//
// REGRESSION-GUARD, not a gap-guard: it passes on 2e74953 too. It is here
// because "uninstalled" is the most tempting thing to fold into "removed"
// while reading this ticket, and nothing else in the suite says no.
//
// MUTANT: add `&& m.DesiredState != DesiredStateUninstall` to the `hosts`
// predicate -> RED here, and GREEN everywhere else in the file.
func TestGetMonitoring_UninstalledButUndeletedMachineStillListed(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	// ARM (a) — INTENT IN FLIGHT. A box parked in desired_state=uninstall: what
	// the route writes when the warden is still connected, i.e. the window
	// between the owner's click and the warden actually disconnecting. This is
	// the arm that discriminates: it is the only state in the suite where the
	// lifecycle intent and the roster status disagree.
	seedRegisteredMachine(t, s, "m-retiring")
	armed, err := s.dal.GetMember("m-retiring")
	if err != nil || armed == nil {
		t.Fatalf("get machine member: %v %v", armed, err)
	}
	armed.DesiredState = DesiredStateUninstall
	if err := s.dal.PutMember(*armed); err != nil {
		t.Fatalf("arm uninstall intent: %v", err)
	}

	// ARM (b) — REAL ROUTE. Drives POST /api/machines/{id}/uninstall on an
	// offline box (the arm that converges the intent straight back to offline),
	// so the test also pins the PREMISE the boundary rests on: uninstall never
	// writes roster_status, by either arm.
	seedRegisteredMachine(t, s, "m-retired")
	rec := httptest.NewRecorder()
	s.HandleUninstallMachineApiMachinesMemberIdUninstallPost(rec,
		httptest.NewRequest("POST", "/api/machines/m-retired/uninstall", nil), "m-retired")
	if rec.Code != 200 {
		t.Fatalf("uninstall machine: %d %s", rec.Code, rec.Body.String())
	}
	m, err := s.dal.GetMember("m-retired")
	if err != nil || m == nil {
		t.Fatalf("get machine member: %v %v", m, err)
	}
	if m.RosterStatus != RosterStatusActive {
		t.Fatalf("premise broken: uninstall set roster_status=%q — if uninstall "+
			"ever starts removing the record, this test is asserting the wrong "+
			"thing and boundary 3 needs re-deciding, not re-greening",
			m.RosterStatus)
	}

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	for _, host := range []string{"m-retiring", "m-retired"} {
		if mr := machineRowIn(d, host); mr == nil {
			t.Errorf("uninstalled-but-undeleted machine %s vanished from monitoring: "+
				"%v — the record is kept on purpose (re-installable) and "+
				"GET /api/machines still lists it; the two surfaces must not "+
				"disagree about what exists", host, d["machines"])
		}
	}
}

// TestGetMonitoring_UnplacedActorMintsNoBlankMachineRow covers the second
// symptom on the ticket. An actor whose host cannot be resolved
// (observedWorkerHost's honest "" — the common case for a worker refused
// placement, see the no_machine_selected refusal in worker_spawn.go) used to
// mint a machines row with machine:"" / display_name:"", and since T-fc2f that
// blank row also carried the actor's ACCOUNT. So the cockpit rendered a
// nameless box, and hung real money off it.
//
// "" is not a machine id, it is the absence of one, so it can never be in the
// registry and the row is now simply gone. The account is NOT lost with it:
// the accounts section still carries the key, with an honest-empty machine
// cell. "I don't know where this ran" is a true statement; "it ran on «blank»"
// is not — and only one of the two can be acted on.
//
// MUTANT: drop the registry guard -> RED on (1) (the blank row returns,
// carrying the account).
func TestGetMonitoring_UnplacedActorMintsNoBlankMachineRow(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	// No machine claim and no self-reported machine: observedWorkerHost has
	// nothing to resolve, exactly like a worker that never got placed.
	seedWorker(t, s, "ow-nowhere", "N1", 1.0, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-nowhere", "",
		`{"runtime":"claude","account":"eva-m5-claude","cost":2.0}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	// (1) no nameless row, with or without an account hanging off it.
	if mr := machineRowIn(d, ""); mr != nil {
		t.Errorf("blank machines row present: %v — \"\" is the absence of a "+
			"machine id, so it must not render as a box", mr)
	}
	// (2) the account is still observable, and honest about not knowing where.
	row := accountRow(t, d, "eva-m5-claude")
	if row["cost"] != 2.0 {
		t.Errorf("cost = %v, want 2 (the figure it reported) — suppressing the blank "+
			"ROW must not suppress the SPEND", row["cost"])
	}
	if row["machine"] != "" {
		t.Errorf("accounts[eva-m5-claude].machine = %v, want honest empty", row["machine"])
	}
}

// Wrongly-typed hardware values (T-aad2)
//
// The KEY layer has had a guard since T-90be: a nested rename reddens CI. The
// VALUE layer had none. `cpu_pct: "47"` was accepted (200), stored verbatim,
// and read back as null — and that null was byte-for-byte the row a machine
// with no CPU probe at all serves. Measured before the fix, on the real ingest
// and read paths: the two rows were identical, hardware_ts and hardware_stale
// included.
//
// The fix is deliberately NOT a refusal. Refusing the body is the fail-closed
// move the owner already ruled against for these blocks (rc-55861dd893c6) and
// its blast radius is the whole heartbeat. The report still lands exactly as
// before; what changes is that the server now SAYS which declared key it could
// not read.
// ---------------------------------------------------------------------------

// invalidOf reads the hardware_invalid list off a monitoring machine row.
func invalidOf(t *testing.T, row map[string]any) []string {
	t.Helper()
	raw, present := row["hardware_invalid"]
	if !present {
		t.Fatalf("the machine row carries no hardware_invalid at all: %v", row)
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("hardware_invalid = %v, want an array (never null — 'nothing is "+
			"broken' is an answer every row can give)", raw)
	}
	keys := []string{}
	for _, v := range list {
		s, isStr := v.(string)
		if !isStr {
			t.Fatalf("hardware_invalid must carry key NAMES only, got %v", v)
		}
		keys = append(keys, s)
	}
	return keys
}

// TestGetMonitoring_WrongTypedHardwareIsNamedNotSilent is the SENTINEL. A
// declared key that arrived with the wrong type must be named on the wire, and
// its healthy siblings must be unaffected.
func TestGetMonitoring_WrongTypedHardwareIsNamedNotSilent(t *testing.T) {
	s, _ := freshnessServer(t,
		`{"cpu_pct": "47", "ram_pct": 61, "ac_power": "yes"}`)

	// (1) COMPATIBILITY, checked at the source: the report still LANDED, whole
	// and verbatim. This is the half the owner ruling protects — the fix must
	// remove the silence, not the tolerance.
	entry := s.telemetry.Get("mira")
	hw, _ := entry["hardware"].(map[string]any)
	if hw["cpu_pct"] != "47" || hw["ac_power"] != "yes" || hw["ram_pct"] != 61.0 {
		t.Fatalf("the stored sample must be untouched, got %v — this fix does not "+
			"reject, drop or coerce anything at ingest", hw)
	}

	// (2) the wire names exactly the broken keys, sorted.
	row := machineRow(t, s, "m-abc123")
	got := invalidOf(t, row)
	want := []string{"ac_power", "cpu_pct"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("hardware_invalid = %v, want %v — the cockpit's only way to say "+
			"'this WAS measured and IS unreadable' instead of showing the same "+
			"blank a never-probed machine gets", got, want)
	}

	// (3) the values themselves stay null: naming the fault is not licence to
	// serve a number the server does not have.
	if row["cpu_pct"] != nil || row["ac_power"] != nil {
		t.Errorf("cpu_pct = %v / ac_power = %v, want null — an unreadable value "+
			"must not be rendered", row["cpu_pct"], row["ac_power"])
	}

	// (4) PER KEY, not per row: the healthy sibling is still served. One broken
	// probe must not cost the operator the readings that did work.
	if row["ram_pct"] != 61.0 {
		t.Errorf("ram_pct = %v, want 61 — a wrongly-typed cpu_pct says nothing "+
			"about ram_pct", row["ram_pct"])
	}
}

// TestGetMonitoring_BrokenAndUnmeasuredHardwareAreDistinguishable is the
// owner's actual acceptance criterion, stated as a comparison rather than as
// two separate readings: the row of a machine whose cpu_pct arrived broken must
// not be the same row as one whose cpu_pct never arrived. Before the fix these
// two JSON objects were equal field for field.
func TestGetMonitoring_BrokenAndUnmeasuredHardwareAreDistinguishable(t *testing.T) {
	broken, _ := freshnessServer(t, `{"cpu_pct": "47", "ram_pct": 61}`)
	never, _ := freshnessServer(t, `{"ram_pct": 61}`)

	brokenRow := machineRow(t, broken, "m-abc123")
	neverRow := machineRow(t, never, "m-abc123")

	// Both really are blank in the value itself — that is the premise, and if it
	// ever stops holding this test is asking the wrong question.
	if brokenRow["cpu_pct"] != nil || neverRow["cpu_pct"] != nil {
		t.Fatalf("precondition: both rows must blank cpu_pct; got %v / %v",
			brokenRow["cpu_pct"], neverRow["cpu_pct"])
	}
	if len(invalidOf(t, brokenRow)) == 0 {
		t.Errorf("the BROKEN row says nothing about why cpu_pct is blank: %v", brokenRow)
	}
	if got := invalidOf(t, neverRow); len(got) != 0 {
		t.Errorf("the NEVER-MEASURED row must stay silent, got %v — an absent probe "+
			"is not a defect, and calling it one would make every battery-less "+
			"machine look broken", got)
	}
}

// TestGetMonitoring_HealthyHardwareNamesNothing is the false-positive sentinel:
// the legitimate shapes the real warden produces must go through untouched and
// unaccused. collectHardware emits float64 percents and a bool ac_power, omits
// every probe that failed, and the frozen spec additionally declares null as a
// legal value for each — so none of those may be reported as invalid. If this
// goes red, the guard has started blaming healthy reporters.
//
// ⚠️ NOT ALL EIGHT CASES CARRY THEIR WEIGHT, and it is worth writing down which,
// so that trimming this table later is a decision rather than a coin flip.
// LOAD-BEARING — each of these fails against a plausible SIMPLER classifier, so
// deleting it removes real protection:
//   - "explicit declared nulls": a naive `_, ok := v.(float64)` sees a present,
//     non-numeric value here and would accuse every failed probe on the fleet.
//   - "the -1 未量到 sentinel": teleNum WITHHOLDS this value, so a classifier
//     written as "the reader returned nil ⇒ blame the key" brands a perfectly
//     healthy reporter. This is the case that separates "unreadable" from
//     "deliberately withheld", and nothing else in the file covers it.
//   - "an undeclared new probe": the owner-ruling boundary (rc-55861dd893c6). A
//     classifier that walked the SAMPLE instead of the declared key set passes
//     everything else here and fails only this.
//
// REDUNDANT-BUT-DOCUMENTARY — "omitted failed probes" and "every probe failed"
// are both the absent-key path the first bullet already forces a classifier to
// get right. They are kept because they name the two shapes a reader actually
// wonders about, not because anything else would catch them; if this table ever
// needs to shrink, those two are the safe ones to drop.
func TestGetMonitoring_HealthyHardwareNamesNothing(t *testing.T) {
	cases := map[string]string{
		"a full healthy sample":    `{"cpu_pct": 47, "ram_pct": 61, "battery_pct": 88, "ac_power": true}`,
		"a false is data":          `{"cpu_pct": 47, "ac_power": false}`,
		"omitted failed probes":    `{"cpu_pct": 47}`,
		"explicit declared nulls":  `{"cpu_pct": null, "ram_pct": null, "battery_pct": null, "ac_power": null}`,
		"every probe failed":       `{}`,
		"the -1 未量到 sentinel":      `{"cpu_pct": -1, "ram_pct": 61}`,
		"an undeclared new probe":  `{"cpu_pct": 47, "disk_pct": "n/a"}`,
		"a zero is a real reading": `{"cpu_pct": 0, "ram_pct": 0, "ac_power": false}`,
	}
	for name, sample := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := freshnessServer(t, sample)
			if got := invalidOf(t, machineRow(t, s, "m-abc123")); len(got) != 0 {
				t.Errorf("%s was reported as invalid %v — this is a shape the real "+
					"producers emit (or the frozen spec declares legal), and "+
					"accusing it turns a healthy fleet into a red screen", sample, got)
			}
		})
	}
}

// TestGetMonitoring_UndeclaredHardwareKeyIsNeverAccused is the compatibility
// sentinel with teeth of its own. `additionalProperties` on the hardware block
// stays TRUE by owner ruling, so a warden that grows a probe this spec version
// has never heard of still lands its whole report. An undeclared key has no
// declared type to violate, so judging one would move the very intolerance the
// ruling rejected from the ingest path to the read path.
func TestGetMonitoring_UndeclaredHardwareKeyIsNeverAccused(t *testing.T) {
	s, _ := freshnessServer(t, `{"cpu_pct": 47, "disk_pct": {"nested": "junk"}}`)
	if got := invalidOf(t, machineRow(t, s, "m-abc123")); len(got) != 0 {
		t.Errorf("hardware_invalid = %v, want empty — disk_pct is not declared, so "+
			"the server has no expectation for it to break", got)
	}
	// And the report is still stored whole, undeclared key and all.
	hw, _ := s.telemetry.Get("mira")["hardware"].(map[string]any)
	if _, present := hw["disk_pct"]; !present {
		t.Errorf("the undeclared key must still be stored: %v", hw)
	}
}

// TestGetMonitoring_StaleSampleAccusesNobody: a stale row's blanks already have
// a published reason (hardware_stale), and the fold is not reading its values
// at all — so it must not also pass judgement on them. Two competing
// explanations for one blank cell is a worse screen than one.
func TestGetMonitoring_StaleSampleAccusesNobody(t *testing.T) {
	s, age := freshnessServer(t, `{"cpu_pct": "47"}`)
	age(telemetryFreshSecs + 1)
	row := machineRow(t, s, "m-abc123")
	if row["hardware_stale"] != true {
		t.Fatalf("precondition: the sample must be stale, got %v", row["hardware_stale"])
	}
	if got := invalidOf(t, row); len(got) != 0 {
		t.Errorf("hardware_invalid = %v, want empty — hardware_stale already "+
			"explains this row's blanks", got)
	}
}

// TestHardwareInvalidKeys covers the classifier directly, including the mixed
// case the fold-level tests cannot easily stage.
func TestHardwareInvalidKeys(t *testing.T) {
	got := hardwareInvalidKeys(map[string]any{
		"cpu_pct":     "47",  // wrong type
		"ram_pct":     true,  // wrong type
		"battery_pct": 88.0,  // fine
		"ac_power":    1.0,   // wrong type (a number is not a boolean)
		"disk_pct":    "n/a", // undeclared — not ours to judge
	})
	want := "ac_power,cpu_pct,ram_pct"
	if strings.Join(got, ",") != want {
		t.Errorf("hardwareInvalidKeys = %v, want [%s] (sorted, declared keys only)",
			got, want)
	}
	if got := hardwareInvalidKeys(map[string]any{}); len(got) != 0 {
		t.Errorf("an empty sample accuses nobody, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// T-14 項目 6 — the merged roster read must not make ONE contractor look like TWO.
//
// The bug this pins is SILENT and cannot go red on its own: after
// ListMembers' `WHERE kind != 'outsource'` was deleted (dal.go), this handler's
// roster read returns the contractor's MEMBER row, and its worker loop returns
// the SAME contractor off ListOutsourceWorkers. Both branches resolve the host
// through the same expression (observedHost / observedWorkerHost both fall back
// to telemetry `machine`), so the one contractor lands in `actors` twice on the
// same host key and `hostCounts[host]++` runs twice for it.
//
// What the owner sees: a machine card reading one more agent than the box is
// running. No error, no log line, no other assertion in this file moves.
//
// The discrimination is VERIFIED, not reasoned: with the driver guard at the
// top of HandleGetMonitoringApiMonitoringGet this reads 2; with the guard's
// `continue` deleted it reads 3.
func TestGetMonitoring_LiveContractorCountsAsOneAgentNotTwo(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	// One registered machine. Its own warden member is an actor on its own row
	// and has always counted as one live agent (see
	// TestGetMonitoring_ReleasedWorkerSpendStaysInTheAccount for why the
	// baseline is 1 and not 0), so the expected total below is warden + worker.
	seedRegisteredMachine(t, s, "m-eva-m5")
	// Exactly ONE live contractor, on that box.
	seedWorker(t, s, "ow-eva", "E1", 0, WorkerStatusActive)
	if rec := doIngestTelemetry(s, "ow-eva", "m-eva-m5",
		`{"runtime":"claude","account":"eva-m5-claude","cost":1.25}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}

	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	mr := machineRowIn(d, "m-eva-m5")
	if mr == nil {
		t.Fatalf("no machines row for m-eva-m5 in %v", d["machines"])
	}
	if mr["agents"] != 2.0 {
		t.Errorf("machines[m-eva-m5].agents = %v, want 2 (the box's own warden + the "+
			"ONE live contractor). 3 means the merged roster read let the same "+
			"contractor into `actors` twice — once off its member row, once off its "+
			"worker row — and the owner is reading an agent count for a box that "+
			"gained no agent", mr["agents"])
	}

	// The same double-entry also duplicates the SESSIONS row. It is invisible in
	// the cockpit today (MonitorPage filters every `ow-` id out of this list),
	// but findSessionFor / joinSessionRuntime are `sessions.find(...)` — first
	// match wins — so a duplicate silently decides WHICH of the two rows any
	// telemetry join lands on. Assert the id appears once.
	seen := 0
	for _, raw := range d["sessions"].([]any) {
		if raw.(map[string]any)["id"] == "ow-eva" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("sessions rows for ow-eva = %d, want exactly 1 — two rows share an "+
			"id and a name but are built from different presence expressions, so "+
			"which one a first-match join picks is decided by loop order", seen)
	}
}
