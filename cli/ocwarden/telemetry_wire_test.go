package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// ── the frozen-schema coupling test (T-6b42) ────────────────────────────────
//
// The server declares AgentTelemetryIngestDTO with additionalProperties:false and
// decodes every mutable write with DisallowUnknownFields. One undeclared key does
// not get dropped — it rejects the WHOLE report. For the warden that means the
// entire machine row (hardware, binary fingerprints, claude probe, runtime
// capabilities) goes null at once, and the 30s producer loop used to discard the
// verdict, so a warden whose every heartbeat was refused looked exactly like a
// healthy one.
//
// The warden and the spec are separate Go modules and cannot import each other,
// which is why this drift was invisible to the compiler. This test reads the
// frozen spec off disk and checks the real payloads against it.

// ── depth (T-90be) ──────────────────────────────────────────────────────────
//
// The check above was TOP-LEVEL ONLY, and the nested blocks declared nothing at
// all: `hardware` was literally {"title": "Hardware"}. Renaming a nested key —
// cpu_pct -> cpu_percent — was accepted (HTTP 200), stored verbatim, and then
// read back as null forever, with the whole suite green. This test's own old
// fixture proved it: it passed `hardware: {"cpu": "M5"}`, a key no consumer has
// ever read, and the guard was happy.
//
// So the spec now declares the nested shape and this walker descends into it.
// Two things keep it honest:
//   - the payloads come from the REAL producers (collectHardware, claudeProber,
//     collectRuntimeCapabilities), not from literals typed in this file. A
//     literal fixture only ever proves the fixture matches the spec, which
//     stays true when the producer is renamed.
//   - the nested declaration is asserted to EXIST (a walker with nothing to
//     descend into silently passes everything).

// ── the VALUE layer (T-aad2) ────────────────────────────────────────────────
//
// Everything above is about KEY NAMES, and that is only half of what the
// declaration says. A producer that keeps the name and changes the TYPE —
// cpu_pct sent as the string "47" instead of the number 47 — walked straight
// through: the nested blocks are open by owner ruling, so the server takes the
// body with a 200 and stores it verbatim, and the reader (which needs a
// float64) then serves null forever. Measured on the real ingest and read paths
// before this guard existed: the resulting machine row was byte-for-byte
// identical to one from a host that has never had a CPU probe.
//
// A rejection at ingest is exactly the fail-closed tightening the owner ruled
// out, so nothing here is refused. This guard is aimed somewhere much narrower:
// OUR OWN producers. It says nothing about what the server accepts from a warden
// in the field, so it costs no tolerance; what it buys is that "we broke our own
// reporter" reddens a build instead of quietly emptying a column.
//
// ⚠️ AND THAT IS ALL IT BUYS — say it plainly, because a guard described more
// broadly than it protects is how this repo has been bitten before. Describing
// it more NARROWLY is its own bug though: it sends the next person to build a
// guard that can never fire. So, measured against the live handler rather than
// assumed, the three declared blocks are covered by three different mechanisms:
//
//	runtimes  — FAIL-CLOSED AT INGEST already, and not by this change: the
//	            handler type-checks installed / logged_in / version per key and
//	            answers a flat 400 (`runtimes.codex.installed must be a
//	            boolean`). A wrong-typed value never reaches the store. This
//	            test is a second, earlier net for our own producers; runtimes is
//	            NOT in the hole, and needs no read-side marker.
//	hardware  — nothing is refused at ingest (owner ruling), so the READ side
//	            carries it: the server names the unreadable key on the wire
//	            (hardware_invalid), and a wrong-typed value from ANY warden,
//	            ours or not, shows up in the cockpit.
//	claude    — THIS TEST AND NOTHING ELSE. `claude: {"version": 9.9}` is
//	            accepted with a 200, stored, and read back as null with nothing
//	            anywhere saying a value was lost. This test only sees payloads
//	            THIS module builds, so an older or third-party warden drifting
//	            there stays invisible at runtime.
//
// That last gap is known and deliberately unfixed (owner ruling: separate
// ticket, not folded into this one). Do not read the green tick below as "the
// value layer is covered" — for claude it means only "our producers are not the
// ones breaking it".

// schemaNode is as much of a JSON-Schema node as this guard needs: the declared
// child properties, the declared value type(s), and whether the node is closed.
type schemaNode struct {
	Properties           map[string]*schemaNode `json:"properties"`
	AdditionalProperties json.RawMessage        `json:"additionalProperties"`
	Type                 string                 `json:"type"`
	Required             []string               `json:"required"`
	AnyOf                []*schemaNode          `json:"anyOf"`
}

// declaredTypes is the set of JSON type names this node accepts, flattening the
// `anyOf: [{type: number}, {type: null}]` shape the spec uses for every nullable
// field. Empty = the node declares no type at all (`{"title": "Binaries"}`), and
// an undeclared node is skipped rather than guessed at — the same rule the key
// walker follows for a block with no declared properties.
func (n *schemaNode) declaredTypes() map[string]bool {
	types := map[string]bool{}
	if n.Type != "" {
		types[n.Type] = true
	}
	for _, alt := range n.AnyOf {
		if alt == nil {
			continue
		}
		for name := range alt.declaredTypes() {
			types[name] = true
		}
	}
	return types
}

// jsonTypeOf names a decoded JSON value's type in JSON-Schema vocabulary. The
// input must have been through encoding/json — the producers hand back Go-native
// values (parseBattery returns an int, binaries is a map[string]string) and it is
// the WIRE type, after marshalling, that the server and the spec are talking
// about.
func jsonTypeOf(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// mistypedPayloadValues walks payload against the schema and returns dotted
// paths whose value type is not one the spec declares for that path, formatted
// as `path: got <type>, want <types>`. Undeclared keys are the key walker's
// business, not this one's, so they are skipped here; a declared node with no
// declared type is skipped too.
func mistypedPayloadValues(payload map[string]any, node *schemaNode) []string {
	var bad []string
	var walk func(map[string]any, *schemaNode, string)
	walk = func(obj map[string]any, at *schemaNode, prefix string) {
		for key, value := range obj {
			child, declared := at.Properties[key]
			if !declared || child == nil {
				continue // undeclared — reported by undeclaredPayloadKeys instead
			}
			if want := child.declaredTypes(); len(want) > 0 && !want[jsonTypeOf(value)] {
				names := make([]string, 0, len(want))
				for name := range want {
					names = append(names, name)
				}
				sort.Strings(names)
				bad = append(bad, fmt.Sprintf("%s%s: got %s, want %s",
					prefix, key, jsonTypeOf(value), strings.Join(names, "|")))
				continue
			}
			if nested, isObj := value.(map[string]any); isObj && len(child.Properties) > 0 {
				walk(nested, child, prefix+key+".")
			}
		}
	}
	walk(payload, node, "")
	sort.Strings(bad)
	return bad
}

// onTheWire round-trips a payload through encoding/json, so the walkers see the
// values the SERVER sees rather than the Go types the producers happened to
// build them from. Without this an int battery_pct would be judged against the
// wrong vocabulary — and, worse, would look like a defect while being perfectly
// fine on the wire.
func onTheWire(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("the payload does not even marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("re-decode the marshalled payload: %v", err)
	}
	return wire
}

// closed reports additionalProperties:false — the setting that makes the SERVER
// refuse an undeclared key instead of storing it.
func (n *schemaNode) closed() bool {
	return strings.TrimSpace(string(n.AdditionalProperties)) == "false"
}

// frozenRequestSchema resolves the request schema for one ROUTE, following the
// spec's own requestBody $ref to get there.
//
// Going via the route rather than naming the schema is the point. A test that
// spells out "AgentTelemetryIngestDTO" keeps comparing against that DTO after the
// operation has been repointed at a different one — the client would then be
// checked against a schema the server no longer uses for this route, and every
// assertion would stay green. The route is what the producer actually sends to, so
// the route is what the schema has to be looked up by.
func frozenRequestSchema(t *testing.T, method, route string) *schemaNode {
	t.Helper()
	specPath := filepath.Join("..", "..", "spec", "openapi.json")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read frozen spec %s: %v", specPath, err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]*schemaNode `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse frozen spec: %v", err)
	}
	operation, ok := spec.Paths[route][method]
	if !ok {
		t.Fatalf("the frozen spec has no %s %s — this guard has no schema to compare against",
			strings.ToUpper(method), route)
	}
	ref := operation.RequestBody.Content["application/json"].Schema.Ref
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		t.Fatalf("%s %s declares no application/json requestBody $ref (got %q) — there is "+
			"nothing for a body to be compared against", strings.ToUpper(method), route, ref)
	}
	name := strings.TrimPrefix(ref, prefix)
	schema, ok := spec.Components.Schemas[name]
	if !ok || schema == nil {
		t.Fatalf("%s is referenced by %s %s but not defined in the frozen spec",
			name, strings.ToUpper(method), route)
	}
	if !schema.closed() {
		t.Fatalf("%s is not a closed schema — this guard would be vacuous", name)
	}
	return schema
}

func frozenTelemetrySchema(t *testing.T) *schemaNode {
	t.Helper()
	return frozenRequestSchema(t, "post", "/api/monitoring/telemetry")
}

// undeclaredPayloadKeys walks payload against the schema and returns the
// dotted paths of keys the frozen spec does not declare. It descends wherever
// the schema declares properties for that path; a block that declares none
// (binaries, tokens, command_result, …) is checked at its own level only, so
// this reports drift, not "the spec is less detailed here".
func undeclaredPayloadKeys(payload map[string]any, node *schemaNode) []string {
	var extra []string
	var walk func(map[string]any, *schemaNode, string)
	walk = func(obj map[string]any, at *schemaNode, prefix string) {
		for key, value := range obj {
			child, declared := at.Properties[key]
			if !declared {
				extra = append(extra, prefix+key)
				continue
			}
			if child == nil || len(child.Properties) == 0 {
				continue // nothing declared below → nothing to check below
			}
			if nested, isObj := value.(map[string]any); isObj {
				walk(nested, child, prefix+key+".")
			}
		}
	}
	walk(payload, node, "")
	sort.Strings(extra)
	return extra
}

// declaredNestedBlockPaths returns every dotted path whose schema node declares
// child properties — exactly the paths the two walkers above are able to descend
// into. It is DERIVED from the frozen spec on every run, and that is the point:
// the depth grading (which keys are checked below their own level and which are
// structurally out of reach) is something the spec says, not something this file
// remembers. A hand-written grading is a snapshot, and this one went stale inside
// a day — `model` and `warden_shape` were declared after the list was written, so
// a listed grading silently stopped describing the DTO while every test stayed
// green.
func declaredNestedBlockPaths(root *schemaNode) []string {
	var paths []string
	var walk func(*schemaNode, string)
	walk = func(at *schemaNode, prefix string) {
		for key, child := range at.Properties {
			if child == nil || len(child.Properties) == 0 {
				continue
			}
			paths = append(paths, prefix+key)
			walk(child, prefix+key+".")
		}
	}
	walk(root, "")
	sort.Strings(paths)
	return paths
}

// missingRequiredKeys walks payload against the schema and returns the dotted
// paths the spec REQUIRES but the producer did not send.
//
// The other two walkers both ask "is what we send acceptable?". This one asks the
// opposite question, and it is the half that was missing: tightening a schema has
// two shapes on the wire, and they are symmetric. "Stop accepting a key the client
// sends" is caught by the key walker. "Start requiring a key the client does not
// send" was caught by nothing — the body simply starts coming back 422 in
// production while every test here stays green, which is precisely the incident
// this file exists to prevent, in its other direction.
func missingRequiredKeys(payload map[string]any, node *schemaNode) []string {
	var missing []string
	var walk func(map[string]any, *schemaNode, string)
	walk = func(obj map[string]any, at *schemaNode, prefix string) {
		for _, key := range at.Required {
			if _, present := obj[key]; !present {
				missing = append(missing, prefix+key)
			}
		}
		for key, value := range obj {
			child, declared := at.Properties[key]
			if !declared || child == nil || len(child.Properties) == 0 {
				continue
			}
			if nested, isObj := value.(map[string]any); isObj {
				walk(nested, child, prefix+key+".")
			}
		}
	}
	walk(payload, node, "")
	sort.Strings(missing)
	return missing
}

// nodeAt resolves a dotted path of declared properties, failing the test when
// the path is not declared. Used to state, as an assertion rather than as a
// comment, that the guard above actually has somewhere to descend.
func nodeAt(t *testing.T, root *schemaNode, path string) *schemaNode {
	t.Helper()
	at := root
	for _, step := range strings.Split(path, ".") {
		child, ok := at.Properties[step]
		if !ok || child == nil {
			t.Fatalf("the frozen spec no longer declares %q — the nested guard has "+
				"nothing to descend into there and would pass any rename", path)
		}
		at = child
	}
	return at
}

// wireBodies runs `drive` against a test server and returns every JSON body that
// really went out, in order. The evidence for an uplink is what a producer PUT ON
// THE WIRE — not a literal a test author typed next to it. Two of the three payloads
// below used to be literals, and independent review measured exactly what that is
// worth: adding an undeclared top-level key to the real command_result producer left
// this file green, because nothing here had ever run that producer.
func wireBodies(t *testing.T, drive func(base string)) []map[string]any {
	t.Helper()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("producer sent a body that is not JSON: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	drive(server.URL)
	return bodies
}

// realCommandReceipt drives the command_result reporter ITSELF — newCommandReporter's
// closure, the one production wires onto CommandDeps.Report — and returns the body it
// posted. This is the send `warden-command-result` claims in cli/uplinks.json.
func realCommandReceipt(t *testing.T) map[string]any {
	t.Helper()
	var bodies []map[string]any
	bodies = wireBodies(t, func(base string) {
		report := newCommandReporter(Config{Base: base, Token: "tok", ID: "m-1"})
		if err := report(CommandResult{MemberID: "m-1", RPC: rpcStop, OK: true,
			Reason: "stopped", Log: "session=member-m-1: stopped"}); err != nil {
			t.Fatalf("the real command_result reporter did not deliver: %v", err)
		}
	})
	if len(bodies) != 1 {
		t.Fatalf("the reporter put %d bodies on the wire, want exactly 1", len(bodies))
	}
	return bodies[0]
}

// realCommandReceipts drives the FOUR command_result producers in command.go — the
// start / worker_stop / stop / uninstall receipts — through the real reporter, and
// returns the bodies keyed by rpc verb.
//
// These four are why the enumeration in bin/uplink-guard.py is closed to a fixpoint.
// They were invisible to it for as long as it stopped one hop from net/http: the path
// is deps.report → CommandDeps.Report → newCommandReporter's closure → post, and the
// guard's own report for cli/ocwarden/command.go was EMPTY while all four were live.
// Each verb builds its own CommandResult, so each gets its own run here: a shared one
// would let three of them be paid for by driving the fourth.
func realCommandReceipts(t *testing.T) map[string]map[string]any {
	t.Helper()
	receipts := map[string]map[string]any{}
	for _, one := range []struct {
		rpc  string
		args map[string]any
	}{
		{rpcStart, fullStartArgs()},
		{rpcWorkerStop, map[string]any{"worker_id": "ow-9"}},
		{rpcStop, map[string]any{"member_id": "m-5", "session_name": "member-m-5"}},
		{rpcUninstall, uninstallArgs()},
	} {
		bodies := wireBodies(t, func(base string) {
			deps := CommandDeps{
				Spawn:    func(StartParams) SpawnOutcome { return SpawnOutcome{OK: true} },
				Stop:     func(string) (bool, bool) { return true, false },
				Teardown: func() (bool, string) { return true, "teardown complete\n" },
				// The uninstall case self-exits after its receipt lands; a fake keeps
				// the test binary alive without changing the producer's path.
				Exit:   func(int) {},
				Report: newCommandReporter(Config{Base: base, Token: "tok", ID: "m-1"}),
			}
			if err := dispatchCommand(&Command{RPC: one.rpc, Args: one.args}, deps); err != nil {
				t.Fatalf("dispatch %s: %v", one.rpc, err)
			}
		})
		if len(bodies) != 1 {
			t.Fatalf("%s put %d bodies on the wire, want exactly 1", one.rpc, len(bodies))
		}
		receipts[one.rpc] = bodies[0]
	}
	return receipts
}

// realSelfUpdateAnnounce drives the real self-update announcement (updater.
// announceSelfUpdate over the real httpPoster) and returns the body it posted.
func realSelfUpdateAnnounce(t *testing.T) map[string]any {
	t.Helper()
	bodies := wireBodies(t, func(base string) {
		up := &updater{
			post:     httpPoster(&http.Client{Timeout: 5 * time.Second}, base, "tok"),
			agentID:  "m-1",
			logf:     func(string, ...any) {},
			lastSwap: &selfUpdateEvent{Binary: "ocwarden", OldHash: "aaa", NewHash: "bbb", At: "2026-01-01T00:00:00Z"},
		}
		up.announceSelfUpdate()
	})
	if len(bodies) != 1 {
		t.Fatalf("the self-update announce put %d bodies on the wire, want exactly 1", len(bodies))
	}
	return bodies[0]
}

// realHeartbeat builds the heartbeat payload from the REAL collectors, driven by
// faked shell/fs seams — the same producers a live 30s cycle calls. Renaming a
// key in any of them changes what this returns, which is the whole point: a
// hand-written fixture would keep matching the spec after the producer drifted.
func realHeartbeat(t *testing.T) map[string]any {
	t.Helper()
	runner := fakeRunner{out: fakeProbes}
	hardware := collectHardware(runner, "darwin")
	if len(hardware) == 0 {
		t.Fatal("precondition: the hardware collector produced nothing to check")
	}

	prober, _, _, _ := newTestProber(newProbeRunner())
	claude := prober.collect()
	if len(claude) == 0 {
		t.Fatal("precondition: the claude prober produced nothing to check")
	}

	// Both runtimes must RESOLVE, or collectRuntimeCapabilities reports
	// installed:false and skips logged_in/version entirely — the guard would
	// then never see the keys it exists to protect.
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("stage a resolvable codex: %v", err)
	}
	probes := map[string]string{}
	for key, value := range fakeProbes {
		probes[key] = value
	}
	probes[bin+" --version"] = "codex-cli 0.52.0"
	probes[bin+" login status"] = "Logged in"
	env := func(key string) string {
		switch key {
		case "OC_CLAUDE_BIN", "OC_CODEX_BIN":
			return bin
		}
		return ""
	}
	runtimes := collectRuntimeCapabilities(env, fakeRunner{out: probes}, claude)

	// The shape verdict comes from the REAL collector too. The package-default
	// cutover seam is blocked inside the test binary, so its `ps` fails and the
	// honest answer is "unknown" — which is still the producer's own word for it,
	// not a literal typed here.
	shape := newShapeReporter("/home/u/.officraft/warden/officraft", 4242)()
	if shape == "" {
		t.Fatal("precondition: the shape collector produced nothing to check")
	}

	// Same reasoning for the cutover-effect verdict: the real collector, whose
	// probes are all blocked inside the test binary.
	//
	// Asserted as "unproven" specifically, NOT as "non-empty". The collector
	// stringifies a closed three-value type, so it can never return "" and a
	// non-empty check is true no matter what the code does — a dead assertion
	// wearing the shape of a precondition.
	//
	// Be precise about what this DOES pin, because the obvious reading is wrong:
	// with every probe refused by the blocked seam, sampleCutoverEffect returns
	// at its own fail-closed guards and judgeCutoverEffect is never reached. So
	// this covers the SAMPLER's refusal to answer without operands — not the
	// judge's truth table, which is pinned in cutovereffect_test.go. Claiming it
	// covered the judge would be a second dead assertion, just a better dressed
	// one.
	effect := newCutoverEffectReporter("/home/u/.officraft/warden/officraft", "officraft", 4242)()
	if effect != string(effectUnproven) {
		t.Fatalf("a collector whose every probe is blocked reported %q, want %q — "+
			"an unmeasurable machine must never claim a verdict", effect, effectUnproven)
	}

	heartbeat, err := buildTelemetryPayload("m-1", "lab-1", hardware,
		map[string]string{"ocwarden": "abc123abc123"}, claude, shape, effect, runtimes)
	if err != nil {
		t.Fatalf("buildTelemetryPayload: %v", err)
	}
	return heartbeat
}

// TestMissingRequiredKeysActuallyNamesAMissingKey is the negative control for the
// required-key half of the walker above.
//
// It needs one because that half is VACUOUS where it is used: AgentTelemetryIngestDTO
// declares no `required` at any depth, so `missingRequiredKeys` returns nil for every
// payload it is handed there — it is a forward-looking net for the day the spec grows
// one, and until then a check that cannot fail. A check that cannot fail is worth
// exactly nothing as evidence, so the walker is exercised HERE against the one frozen
// request schema that does declare required keys (ReplyCardCreateDTO: kind, summary,
// options). If the walker is ever broken, this is what goes red.
func TestMissingRequiredKeysActuallyNamesAMissingKey(t *testing.T) {
	declared := frozenRequestSchema(t, "post", "/api/reply-cards")
	if len(declared.Required) == 0 {
		t.Fatalf("precondition: the frozen spec no longer declares required keys for "+
			"POST /api/reply-cards, so this control has nothing to prove; point it at a "+
			"schema that does. required = %v", declared.Required)
	}
	complete := map[string]any{}
	for _, key := range declared.Required {
		complete[key] = "x"
	}
	if missing := missingRequiredKeys(complete, declared); len(missing) > 0 {
		t.Errorf("a payload carrying every required key was reported as missing %v", missing)
	}
	for _, key := range declared.Required {
		partial := map[string]any{}
		for other := range complete {
			if other != key {
				partial[other] = "x"
			}
		}
		missing := missingRequiredKeys(partial, declared)
		if len(missing) != 1 || missing[0] != key {
			t.Errorf("dropping %q was reported as missing %v, want exactly [%s] — a walker "+
				"that cannot name an omitted required key is not evidence that our payloads "+
				"carry theirs", key, missing, key)
		}
	}
}

// TestWardenTelemetryPayloadsMatchFrozenSchema covers all three payloads the
// warden POSTs to the telemetry endpoint — the heartbeat, the command receipt and
// the self-update announcement — because a single undeclared key kills whichever
// one carries it. Since T-90be it checks NESTED keys too: a rename inside
// hardware/claude/runtimes lands with a 200 and is then unreadable forever, so
// CI is the only place that can notice.
func TestWardenTelemetryPayloadsMatchFrozenSchema(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	heartbeat := realHeartbeat(t)
	receipts := realCommandReceipts(t)
	// Each receipt has to be ITS OWN receipt. Without this the four rows buy only
	// "a command_result key was present": command_result is free-form in the frozen
	// spec, so the walkers below never look inside it, and review measured that
	// changing a verb to a value the server has never heard of left this green.
	for _, want := range []struct{ wireCase, rpc, idKey, id string }{
		{"start", rpcStart, "member_id", "m-1"},
		{"worker_stop", rpcWorkerStop, "worker_id", "ow-9"},
		{"stop", rpcStop, "member_id", "m-5"},
		{"uninstall", rpcUninstall, "member_id", "m-5"},
	} {
		receipt, _ := receipts[want.rpc]["command_result"].(map[string]any)
		if receipt == nil {
			t.Errorf("the %s receipt carries no command_result object", want.wireCase)
			continue
		}
		if receipt["rpc"] != want.rpc {
			t.Errorf("the %s receipt says rpc=%v, so this row's evidence is another verb's "+
				"run — the server folds last_op onto the member by this field", want.wireCase, receipt["rpc"])
		}
		if receipt[want.idKey] != want.id {
			t.Errorf("the %s receipt addresses %s=%v, want %s — a receipt that names the wrong "+
				"target is folded onto the wrong row", want.wireCase, want.idKey, receipt[want.idKey], want.id)
		}
	}
	cases := map[string]map[string]any{
		"heartbeat":                  heartbeat,
		"command_result":             realCommandReceipt(t),
		"command_result-start":       receipts[rpcStart],
		"command_result-worker_stop": receipts[rpcWorkerStop],
		"command_result-stop":        receipts[rpcStop],
		"command_result-uninstall":   receipts[rpcUninstall],
		"self_update":                realSelfUpdateAnnounce(t),
	}
	walked := map[string]int{}
	for name, payload := range cases {
		walked[name+" \u2192 "+telemetryPath]++
		// An EMPTY payload satisfies both walkers below — they only look at keys that
		// are present — so without this, the evidence for a fourth uplink could be the
		// literal `{}`, and the join above would count it. Review demonstrated exactly
		// that: a new warden uplink carrying a key that 422s the whole heartbeat, with
		// a one-line `"extra": {}` as its committed proof. The marginal cost of a new
		// uplink has to be a real payload, not a pair of braces.
		// Measured, because "this branch never runs" was reported about it and the two
		// reasons that could be true of it are not the same finding: it is REACHABLE
		// (add an empty case and this is the error you get), and it does not change the
		// VERDICT — delete it and an empty payload still fails, one check down, as
		// "carries no key the frozen schema declares". Both halves were run. It stays
		// for the message: the next check names the symptom, this one names the cause.
		if len(payload) == 0 {
			t.Errorf("%s has an empty payload, so every comparison below it is vacuous — "+
				"the walkers only inspect keys that are present. Give this uplink the body "+
				"its producer actually builds.", name)
			continue
		}
		if declaredHere := undeclaredPayloadKeys(payload, declared); len(declaredHere) == len(payload) {
			t.Errorf("%s carries no key the frozen schema declares (%v), so it cannot be "+
				"evidence that this uplink matches the contract.", name, declaredHere)
		}
		if missing := missingRequiredKeys(payload, declared); len(missing) > 0 {
			t.Errorf("%s omits key(s) the frozen spec REQUIRES: %v.\n"+
				"Tightening a schema has two shapes and they are symmetric: refusing a key "+
				"we send (caught below) and requiring one we do not (this). Both 422 the "+
				"whole report in production; only the first used to redden a build.\n"+
				"payload = %#v", name, missing, payload)
		}
		if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
			t.Errorf("%s payload carries keys the frozen spec does not declare %v.\n"+
				"A TOP-LEVEL undeclared key 422s the whole report. A NESTED one is worse: "+
				"the server answers 200, stores it, and no consumer ever reads it again — "+
				"nothing outside this test can see that. Declare the key in "+
				"spec/openapi.json (and teach the server to read it) or fix the producer.\n"+
				"payload = %#v", name, extra, payload)
		}
	}
	// The join, per (producer run → route) against the committed manifest — the same
	// key the codex wire test uses. A per-route total alone is interchangeable between
	// rows on one route, and every case here posts to that same one.
	//
	// Be precise about what it does and does not prove now that every payload above
	// comes from a real producer driven against a test server: a producer that stops
	// sending, or sends twice, reddens here, and so does a top-level key the frozen
	// spec does not declare. What it still cannot see is drift INSIDE command_result /
	// tokens / rate_limits: those blocks are free-form by owner ruling, the spec
	// declares no properties under them, so the walkers have nothing to descend into.
	// Measured: adding an undeclared key inside command_result leaves this green;
	// adding one at the top level reddens it.
	if want := manifestUplinkPaths(t, "cli/ocwarden/telemetry_wire_test.go"); !maps.Equal(walked, want) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but %v was walked against "+
			"the frozen schema. A row nobody compared is coverage on paper only — that is "+
			"how the shipped clients broke under a green build.", want, walked)
	}

	// COVERAGE. A payload that passed the schema by being empty would be
	// worthless, and so would one whose nested blocks are empty: the walker only
	// checks keys that are actually present, so an absent key is invisible to it.
	// These are the keys the server reads by name, listed here so that "the
	// producer stopped emitting it" is as red as "the producer renamed it".
	// (Checked AFTER the drift assertion above, so a rename is reported as drift
	// rather than as a missing key.)
	for _, key := range []string{"machine", "hardware", "binaries", "claude", "runtimes",
		"warden_shape"} {
		if _, present := heartbeat[key]; !present {
			t.Errorf("heartbeat dropped %s; payload = %#v", key, heartbeat)
		}
	}
	blockKeys := map[string][]string{
		"hardware": {"cpu_pct", "ram_pct", "battery_pct", "ac_power"},
		"claude":   {"version", "cred_file", "sub_readable", "keychain"},
	}
	for name, keys := range blockKeys {
		block, _ := heartbeat[name].(map[string]any)
		for _, key := range keys {
			if _, present := block[key]; !present {
				t.Errorf("heartbeat %s carries no %s — the server reads that key by "+
					"name, and a key the fixture never emits is a key this guard "+
					"cannot protect; block = %v", name, key, block)
			}
		}
	}
	runtimes, _ := heartbeat["runtimes"].(map[string]any)
	for _, name := range []string{"claude", "codex"} {
		capability, _ := runtimes[name].(map[string]any)
		// version is omitted when the runtime is not installed, and for claude it
		// is transcribed from the claude probe (covered above); installed and
		// logged_in are what machineSupportsRuntime gates placement on.
		for _, key := range []string{"installed", "logged_in"} {
			if _, present := capability[key]; !present {
				t.Errorf("heartbeat runtimes.%s carries no %s — placement fail-closes "+
					"without it and the machine silently stops accepting that "+
					"runtime; capability = %v", name, key, capability)
			}
		}
	}
	if capability, _ := runtimes["codex"].(map[string]any); capability["version"] == nil {
		t.Errorf("heartbeat runtimes.codex carries no version; capability = %v", capability)
	}
}

// TestRunLogsRefusedTelemetry: a server REFUSAL must reach the log. The producer
// loop has always computed the verdict and thrown it away, so a warden reporting
// into a 422 every 30 seconds — leaving every machine row in the cockpit null —
// was indistinguishable from a healthy one for as long as it ran. A transport
// fault (status 0, i.e. the server is simply down) stays quiet by design.
func TestRunLogsRefusedTelemetry(t *testing.T) {
	cfg := Config{Base: "http://x", Token: "t", ID: "m-1"}
	collect := func() map[string]any { return map[string]any{"cpu": "M5"} }
	machine := func() string { return "lab-1" }
	noSleep := func(context.Context, time.Duration) bool { return true }

	refuse := func(string, map[string]any) (int, map[string]any) {
		return 422, map[string]any{"error": map[string]any{
			"code":    "validation_error",
			"message": `invalid request body: json: unknown field "agent_id"`,
		}}
	}
	var out bytes.Buffer
	run(context.Background(), cfg, collect, machine, refuse, nil, nil, nil, nil, noSleep, 1, &out)
	log := out.String()
	if !strings.Contains(log, "422") || !strings.Contains(log, "unknown field") {
		t.Errorf("a refused heartbeat must log the status AND the server's reason; got %q", log)
	}
	if !strings.Contains(log, "NOT stored") {
		t.Errorf("log must say the report did not land; got %q", log)
	}

	// A server that is merely unreachable must NOT spam the log.
	down := func(string, map[string]any) (int, map[string]any) { return 0, nil }
	var quiet bytes.Buffer
	run(context.Background(), cfg, collect, machine, down, nil, nil, nil, nil, noSleep, 1, &quiet)
	if quiet.Len() != 0 {
		t.Errorf("an unreachable server is expected, not a refusal; log = %q", quiet.String())
	}

	// And a stored report says nothing either.
	okPost := func(string, map[string]any) (int, map[string]any) { return 200, map[string]any{} }
	var silent bytes.Buffer
	run(context.Background(), cfg, collect, machine, okPost, nil, nil, nil, nil, noSleep, 1, &silent)
	if silent.Len() != 0 {
		t.Errorf("a stored report must stay quiet; log = %q", silent.String())
	}
}

// TestCodexTelemetryPayloadsMatchFrozenSchema is the sentinel for the OTHER
// runtime: the Codex sidecar reports through the same endpoint and must stay
// unaffected by the Claude-side fix. Its keys are asserted against the same
// frozen schema, including the runtime-specific ones (its own camelCase token
// names; `effort` is no longer runtime-specific — T-e12c gave the Claude reporter
// the same key, which it had been omitting since the field was declared).
func TestCodexTelemetryPayloadsMatchFrozenSchema(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	cases := map[string]map[string]any{
		"identity": {"runtime": "codex", "account": "codex:abc", "account_label": "ChatGPT"},
		"token usage": {"runtime": "codex", "effort": "medium",
			"account": "codex:abc", "account_label": "ChatGPT",
			"tokens": map[string]any{"inputTokens": 1, "cachedInputTokens": 2}},
		"rate limits": {"runtime": "codex", "account": "codex:abc", "account_label": "ChatGPT",
			"rate_limits": map[string]any{"five_hour": map[string]any{
				"used_percentage": 10.0, "resets_at": 1720000000.0}}},
	}
	for name, payload := range cases {
		if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
			t.Errorf("codex %s payload has keys the frozen schema refuses %v", name, extra)
		}
	}
}

// TestWardenTelemetryGuardSeesNestedRenames is the guard's own POSITIVE
// CONTROL. TestWardenTelemetryPayloadsMatchFrozenSchema passing means "today's
// payload matches today's spec" — which is also what a walker that never
// descends would report, and what the old top-level-only walker DID report
// while cpu_pct -> cpu_percent went silently unread in production.
//
// So: take the real heartbeat, rename ONE nested key at each of the three
// depths, and require the guard to name it. It fails if the walker stops at the
// top level, if it stops one level short of runtimes.<rt>.*, or if the spec
// stops declaring the nested shape.
func TestWardenTelemetryGuardSeesNestedRenames(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	// The nested shape must be DECLARED — a walker with nothing to descend into
	// passes every payload, which is the failure mode this whole test exists to
	// rule out.
	for _, path := range []string{"hardware", "claude", "runtimes",
		"runtimes.claude", "runtimes.codex"} {
		if node := nodeAt(t, declared, path); len(node.Properties) == 0 {
			t.Fatalf("%s declares no properties — nothing below it can be checked", path)
		}
	}

	rename := func(payload map[string]any, path []string, from, to string) {
		at := payload
		for _, step := range path {
			next, ok := at[step].(map[string]any)
			if !ok {
				t.Fatalf("precondition: %v is not an object in the real payload", path)
			}
			at = next
		}
		value, present := at[from]
		if !present {
			t.Fatalf("precondition: the real producer no longer emits %v.%s", path, from)
		}
		delete(at, from)
		at[to] = value
	}

	cases := []struct {
		name       string
		path       []string
		from, to   string
		wantReport string
	}{
		{"hardware", nil, "cpu_pct", "cpu_percent", "hardware.cpu_percent"},
		{"claude", nil, "version", "cli_version", "claude.cli_version"},
		{"runtimes", []string{"codex"}, "logged_in", "loggedIn", "runtimes.codex.loggedIn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := realHeartbeat(t)
			block, ok := payload[tc.name].(map[string]any)
			if !ok {
				t.Fatalf("precondition: heartbeat has no %s block", tc.name)
			}
			if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
				t.Fatalf("precondition: the untouched payload is already drifting %v", extra)
			}
			rename(block, tc.path, tc.from, tc.to)
			extra := undeclaredPayloadKeys(payload, declared)
			found := false
			for _, key := range extra {
				if key == tc.wantReport {
					found = true
				}
			}
			if !found {
				t.Errorf("renaming %s.%v.%s -> %s went UNREPORTED (guard said %v). A "+
					"nested rename is accepted by the server with a 200, stored, and "+
					"then read as null forever — CI is the only place it can be seen.",
					tc.name, tc.path, tc.from, tc.to, extra)
			}
		})
	}
}

// TestWardenTelemetryValueTypesMatchFrozenSchema is the VALUE-layer twin of
// TestWardenTelemetryPayloadsMatchFrozenSchema: same real producers, same frozen
// spec, but it asks what TYPE each declared value arrives as instead of whether
// the key is known.
//
// The failure it exists to catch is a producer-side type regression — a probe
// parser that starts returning its number as a string, an `installed` flag that
// becomes "true". None of that is refused anywhere: the block is open, the
// server stores it, and the column it feeds goes blank. Nothing else in this
// repo can see it.
//
// It is deliberately a check on OUR OWN payloads and nothing else. The server
// stays as permissive as the owner ruling requires (rc-55861dd893c6); this is a
// regression test for the warden, not a tightening of the wire.
func TestWardenTelemetryValueTypesMatchFrozenSchema(t *testing.T) {
	declared := frozenTelemetrySchema(t)

	// COVERAGE FIRST. A walker whose leaves declare no type passes everything,
	// which is indistinguishable from "the payload is fine". These are the exact
	// leaves the server reads by name, asserted to carry a declared type before
	// anything is judged against them.
	for path, want := range map[string]string{
		"hardware.cpu_pct":          "number",
		"hardware.ram_pct":          "number",
		"hardware.battery_pct":      "number",
		"hardware.ac_power":         "boolean",
		"claude.version":            "string",
		"claude.cred_file":          "boolean",
		"claude.sub_readable":       "boolean",
		"claude.keychain":           "boolean",
		"runtimes.claude.installed": "boolean",
		"runtimes.codex.installed":  "boolean",
		"runtimes.codex.logged_in":  "boolean",
		"runtimes.codex.version":    "string",
	} {
		types := nodeAt(t, declared, path).declaredTypes()
		if !types[want] {
			t.Errorf("the frozen spec no longer declares %s as %s (got %v) — this "+
				"guard would pass any value there", path, want, types)
		}
	}

	heartbeat := onTheWire(t, realHeartbeat(t))
	if bad := mistypedPayloadValues(heartbeat, declared); len(bad) > 0 {
		t.Errorf("the heartbeat sends values the frozen spec does not declare %v.\n"+
			"Nothing refuses this: the nested blocks are open by owner ruling, so "+
			"the server answers 200 and stores it, and the reader — which needs the "+
			"declared type — serves null forever. Fix the producer, or change the "+
			"spec and teach the server to read the new type.\npayload = %#v",
			bad, heartbeat)
	}
}

// TestWardenTelemetryValueGuardSeesRetypedValues is that guard's own POSITIVE
// CONTROL, and it is the reason the guard is worth having: a green
// TestWardenTelemetryValueTypesMatchFrozenSchema is also exactly what a walker
// that never compares anything reports.
//
// So: take the real heartbeat, change ONE nested value's type at each depth, and
// require the guard to name it. It fails if the walker never descends, if the
// spec stops declaring types, or if the comparison is vacuous.
func TestWardenTelemetryValueGuardSeesRetypedValues(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	cases := []struct {
		name       string
		path       []string
		key        string
		bad        any
		wantReport string
	}{
		{"hardware number as string", []string{"hardware"}, "cpu_pct", "47",
			"hardware.cpu_pct: got string, want null|number"},
		{"hardware bool as string", []string{"hardware"}, "ac_power", "yes",
			"hardware.ac_power: got string, want boolean|null"},
		{"claude string as number", []string{"claude"}, "version", 9.9,
			"claude.version: got number, want null|string"},
		{"runtime bool as string", []string{"runtimes", "codex"}, "installed", "true",
			"runtimes.codex.installed: got string, want boolean|null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := onTheWire(t, realHeartbeat(t))
			if bad := mistypedPayloadValues(payload, declared); len(bad) > 0 {
				t.Fatalf("precondition: the untouched payload already mistypes %v", bad)
			}
			at := payload
			for _, step := range tc.path {
				next, isObj := at[step].(map[string]any)
				if !isObj {
					t.Fatalf("precondition: %v is not an object in the real payload", tc.path)
				}
				at = next
			}
			if _, present := at[tc.key]; !present {
				t.Fatalf("precondition: the real producer no longer emits %v.%s",
					tc.path, tc.key)
			}
			at[tc.key] = tc.bad
			bad := mistypedPayloadValues(payload, declared)
			found := false
			for _, report := range bad {
				if report == tc.wantReport {
					found = true
				}
			}
			if !found {
				t.Errorf("retyping %v.%s went UNREPORTED (guard said %v). A wrongly-"+
					"typed value is accepted with a 200, stored, and then read as "+
					"null forever — CI is the only place it can be seen.",
					tc.path, tc.key, bad)
			}
		})
	}
}

// TestWardenTelemetryValueGuardIgnoresUndeclaredKeys keeps the value guard
// inside the owner ruling. `additionalProperties` stays true so a warden that
// grows a probe still lands its whole report; an undeclared key has no declared
// type, so this guard must have no opinion about its value either. Judging one
// would re-import the intolerance the ruling rejected through the back door.
func TestWardenTelemetryValueGuardIgnoresUndeclaredKeys(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	payload := onTheWire(t, realHeartbeat(t))
	hardware, _ := payload["hardware"].(map[string]any)
	hardware["disk_pct"] = "n/a"
	hardware["thermal"] = map[string]any{"nominal": true}
	if bad := mistypedPayloadValues(payload, declared); len(bad) > 0 {
		t.Errorf("an undeclared nested key was judged on its value %v — the spec "+
			"declares nothing about it, so there is nothing for it to violate", bad)
	}
}

// TestFrozenTelemetryNestedBlocksStayOpen is the COMPATIBILITY sentinel for the
// owner ruling (rc-55861dd893c6): declare the shape, but keep accepting keys
// the spec has not heard of.
//
// The tempting "improvement" is additionalProperties:false, because then the
// server rejects a rename at runtime instead of only in CI. That refusal is not
// per-key: DisallowUnknownFields fails the WHOLE body, so one undeclared nested
// key nulls hardware, binaries, claude and runtimes together on every machine at
// once — measured, and identical to the a7fa594 outage. The two failure modes
// are not symmetric: a rename caught only by CI costs one red build, while a
// closed nested schema costs the fleet's telemetry the moment any warden version
// differs from the spec in either direction.
// The paths come from the spec, not from a list here: the sentinel has to cover
// whatever nested shape the DTO declares TODAY, including the next block someone
// declares. A listed set would keep passing while a newly declared block was
// closed — and closing one is exactly the fleet-wide outage described above.
func TestFrozenTelemetryNestedBlocksStayOpen(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	nested := declaredNestedBlockPaths(declared)
	if len(nested) == 0 {
		t.Fatal("the frozen DTO declares no nested shape at all — this sentinel would " +
			"be vacuous, and the key walker would have nothing to descend into")
	}
	for _, path := range nested {
		if nodeAt(t, declared, path).closed() {
			t.Errorf("%s has additionalProperties:false. Declaring the shape is for CI; "+
				"closing it makes the SERVER 422 the entire heartbeat over one nested "+
				"key it has not heard of — every machine's telemetry going null at "+
				"once, which is the outage this declaration was written to avoid.", path)
		}
	}
	// The top level is a different question and stays closed: those keys are the
	// DTO's own fields, the server has always refused unknown ones there, and
	// every producer in this module is checked against that list above.
	if !declared.closed() {
		t.Error("the top-level DTO must stay closed")
	}
}

// TestEveryDeclaredNestedBlockIsActuallyWalked is the meta-assertion that keeps the
// depth grading from becoming decoration.
//
// The grading itself is honest and load-bearing: a key the spec declares with no
// shape below it (`tokens`, `command_result`, `rate_limits`, …) carries free-form
// content by DESIGN — the server holds it in an untyped field — so nothing here can
// judge what is inside, and claiming otherwise would be a lie. The danger is the
// other half: once "this key is unchecked below its own level" is an accepted answer,
// it becomes the quietest possible place for a real gap to sit. If a block that DOES
// declare a shape is never actually descended into, its payload reads exactly like a
// free-form one — green, with nothing inside ever compared.
//
// So this asks the question as a query that must come back empty: for every path the
// spec declares a shape at, plant an undeclared key there and require the walker to
// name it. Nothing is listed, so a block declared tomorrow is covered tomorrow. It
// reddens if a walker stops short of a declared depth, and it reddens if someone
// grades a shaped block as free-form.
func TestEveryDeclaredNestedBlockIsActuallyWalked(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	nested := declaredNestedBlockPaths(declared)
	if len(nested) == 0 {
		t.Fatal("the frozen DTO declares no nested shape at all — there is no depth " +
			"for this assertion to be about")
	}

	const probe = "oc_undeclared_probe_key"
	var unwalked []string
	for _, path := range nested {
		payload := map[string]any{}
		at := payload
		for _, step := range strings.Split(path, ".") {
			next := map[string]any{}
			at[step] = next
			at = next
		}
		at[probe] = true

		want := path + "." + probe
		found := false
		for _, reported := range undeclaredPayloadKeys(payload, declared) {
			if reported == want {
				found = true
				break
			}
		}
		if !found {
			unwalked = append(unwalked, path)
		}
	}
	if len(unwalked) > 0 {
		t.Errorf("the frozen spec declares a nested shape at %v, but the key walker does "+
			"not descend there — a rename inside those blocks would go unread, which is "+
			"the a7fa594 shape. Whether a key is checked below its own level has to be "+
			"read off the spec, not remembered here.", unwalked)
	}
}
