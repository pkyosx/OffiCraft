package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// ── spec/openapi.json ↔ spec/mcp-catalog.json:結構性對質 ──────────────────────
//
// T-74f8 round-2 §1-#6 / §7-3. The MCP tool surface is written down TWICE — once
// as an OpenAPI operation (spec/openapi.json, which CI regenerates schema.ts
// from and diffs, bin/ci.sh) and once as a tools/list descriptor
// (spec/mcp-catalog.json, which ocserverd serves verbatim). Until this test
// existed, NOTHING compared the two: mcp_test.go and conformance/test_rest_happy.py
// both key on the tool NAME set only, so a parameter could exist on one side and
// not the other indefinitely.
//
// That is not hypothetical. It is exactly the half-finished state this very
// ticket shipped in: openapi.json carried handoff / handoff_note /
// handoff_task_id, the catalog did not, and every check stayed green — which
// means the gate's 422 told agents to send parameters their only view of the
// tool said did not exist. A gate that names an unreachable way out is an
// outage, so the two files agreeing is load-bearing, not tidiness.
//
// The confrontation is THREE-way, because neither spec file knows how to find
// the other: the routes table (defaultRouteSpecs) supplies tool name → method +
// path, openapi.json supplies that operation's parameter set, and the catalog
// supplies the tool's inputSchema. Drift in any one of the three reddens here.
//
// Deliberately a set comparison on property NAMES, not on types or prose: the
// two files describe the same fact in genuinely different vocabularies (JSON
// Schema vs OpenAPI 3.1 with $ref indirection), and pinning more than the
// parameter set would make this test fail on formatting rather than on drift.

// knownCatalogDrift is the drift that ALREADY existed when this confrontation
// was first written (T-74f8 round-2 rework): a parameter spec/openapi.json
// accepts, the handler genuinely READS, and the frozen catalog does not
// advertise — so an agent, whose only view of a tool is tools/list, cannot
// discover a feature that works.
//
// VERDICT ON EACH ENTRY: every one below was traced to the line that reads it,
// so all six are MISSING, not deliberately withheld. Recorded because "we could
// not tell whether this was intentional" is exactly the kind of shrug that lets
// a real gap live forever:
//
//	list_tasks.open          → api_tasks.go:534        trimmedOrEmpty(params.Open) == "true"
//	get_chat.peek            → api_chat.go:399         trimmedOrEmpty(params.Peek) == "true"
//	get_members.fields       → api_members.go:90       trimmedOrEmpty(params.Fields) == "light"
//	list_task_manuals.view   → api_taskmanuals.go:123  trimmedOrEmpty(params.View) == "list"
//	update_task_manual.display_name → api_taskmanuals.go:279-280
//	ingest_telemetry.binaries/claude → api_monitoring.go:339,343,388,391
//
// The last one deserves a note because it was initially GUESSED to be
// CLI-only: binaries and claude are read in the same handler, by the same
// asObject calls, as hardware and self_update — and those two ARE already in
// the catalog. There is no line anywhere that treats them differently. A guess
// from the parameter's NAME was simply wrong; only following the read proved it.
//
// There is no parameter-level "deliberately not on MCP" marker in this codebase
// (RouteSpec.MCPExclude is whole-route only), which is why intent has to be
// established by tracing reads. That absence is itself worth a ticket: today
// "forgotten" and "withheld on purpose" are indistinguishable by inspection.
//
// Baselined rather than fixed HERE: repairing the catalog changes the tools/list
// wire surface and therefore catalog_hash for six tools unrelated to the handoff
// gate, which needs its own ticket and its own conformance run. Baselining keeps
// this test fail-closed for anything NEW while staying loud about the debt.
//
// Checked in BOTH directions (see the rot assertion below): if one is fixed,
// this test fails until the entry is deleted. A stale allowlist that silently
// permits drift is the same disease as a stale comment, and this file exists
// because of a stale comment.
var knownCatalogDrift = map[string][]string{
	"ingest_telemetry":   {"binaries", "claude"},
	"list_task_manuals":  {"view"},
	"update_task_manual": {"display_name"},
	"list_tasks":         {"open"},
	"get_members":        {"fields"},
	"get_chat":           {"peek"},
}

// openapiOverweight is the OTHER direction, and it is NOT debt: openapi lists a
// field for this operation that the operation's handler never reads, so the
// catalog omitting it is CORRECT and openapi is the one describing a parameter
// that does nothing.
//
// open_gate.bind: ReplyCardCreateDTO is SHARED by two operations. Bind is read
// in exactly one place in the whole server — api_replycards.go:401, inside
// HandleCreateReplyCard — and HandleOpenTaskGate (api_tasks.go) decodes the same
// DTO and never touches it. It arrived with T-4166 on the shared DTO.
//
// Kept separate from knownCatalogDrift deliberately: filing this as "debt" would
// record a bug that does not exist, and would invite someone to "fix" it by
// advertising a lever open_gate ignores — which is the exact failure this ticket
// is about, just pointed the other way.
var openapiOverweight = map[string][]string{
	"open_gate": {"bind"},
}

type openapiSpec struct {
	Paths map[string]map[string]struct {
		Parameters []struct {
			Name string `json:"name"`
		} `json:"parameters"`
		RequestBody struct {
			Content map[string]struct {
				Schema map[string]any `json:"schema"`
			} `json:"content"`
		} `json:"requestBody"`
	} `json:"paths"`
	Components struct {
		Schemas map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

type catalogSpec struct {
	Tools []struct {
		Name        string `json:"name"`
		InputSchema struct {
			Properties map[string]any `json:"properties"`
		} `json:"inputSchema"`
	} `json:"tools"`
}

// openapiParamsFor returns the parameter-name set an operation accepts: its
// path/query parameters plus the property names of its JSON request body
// ($ref resolved through components/schemas).
//
// The second result distinguishes the two reasons there may be nothing to
// compare, because collapsing them is how this leg rots silently: "the route is
// not in openapi at all" is a REAL failure (a live MCP tool with no spec entry),
// while "the body is multipart" is a legitimate skip. An earlier version of this
// helper returned the same bare false for both, which meant a route quietly
// disappearing from openapi looked exactly like an upload endpoint and reddened
// nothing — the same enumerate-and-hope shape this whole ticket is about.
type openapiLookup int

const (
	openapiFound       openapiLookup = iota // compare it
	openapiMissing                          // route exists, spec does not — FAIL
	openapiNotJSONBody                      // multipart upload — genuinely skip
)

func openapiParamsFor(spec openapiSpec, method, path string) (map[string]bool, openapiLookup) {
	ops, hit := spec.Paths[path]
	if !hit {
		return nil, openapiMissing
	}
	op, hit := ops[strings.ToLower(method)]
	if !hit {
		return nil, openapiMissing
	}
	out := map[string]bool{}
	for _, p := range op.Parameters {
		out[p.Name] = true
	}
	if len(op.RequestBody.Content) > 0 {
		body, isJSON := op.RequestBody.Content["application/json"]
		if !isJSON {
			return nil, openapiNotJSONBody // multipart upload — nothing to compare
		}
		if ref, isRef := body.Schema["$ref"].(string); isRef {
			named := ref[strings.LastIndex(ref, "/")+1:]
			for name := range spec.Components.Schemas[named].Properties {
				out[name] = true
			}
		} else if inline, isInline := body.Schema["properties"].(map[string]any); isInline {
			for name := range inline {
				out[name] = true
			}
		}
	}
	return out, openapiFound
}

func TestFrozenCatalogAgreesWithOpenapiOnEveryToolsParameters(t *testing.T) {
	rawAPI, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var api openapiSpec
	if err := json.Unmarshal(rawAPI, &api); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	rawCat, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var cat catalogSpec
	if err := json.Unmarshal(rawCat, &cat); err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	catalogProps := map[string]map[string]bool{}
	for _, tool := range cat.Tools {
		props := map[string]bool{}
		for name := range tool.InputSchema.Properties {
			props[name] = true
		}
		catalogProps[tool.Name] = props
	}

	index := mcpToolIndex(defaultRouteSpecs())
	if len(index) == 0 {
		t.Fatalf("no MCP tools in the routes table — this test would pass vacuously")
	}
	compared := 0
	for name, spec := range index {
		want, lookup := openapiParamsFor(api, spec.Method, spec.Path)
		switch lookup {
		case openapiNotJSONBody:
			continue // multipart upload — genuinely nothing to compare
		case openapiMissing:
			t.Errorf("tool %q is a live MCP tool on the routes table but %s %s is "+
				"NOT in spec/openapi.json — the routes≡openapi leg of this "+
				"confrontation has broken for it, and every parameter check "+
				"below would have skipped it in silence",
				name, spec.Method, spec.Path)
			continue
		}
		got, listed := catalogProps[name]
		if !listed {
			t.Errorf("tool %q is on the routes table (and in openapi as %s %s) but "+
				"MISSING from spec/mcp-catalog.json — agents reach tools only "+
				"through tools/list, so this tool does not exist for them",
				name, spec.Method, spec.Path)
			continue
		}
		compared++
		baseline := map[string]bool{}
		for _, p := range knownCatalogDrift[name] {
			baseline[p] = true
		}
		overweight := map[string]bool{}
		for _, p := range openapiOverweight[name] {
			overweight[p] = true
		}
		var missing, extra []string
		for p := range want {
			if got[p] {
				// Recorded but actually present now: the debt was paid, or the
				// "openapi is overweight" call was wrong. Either way the map has
				// to shrink, or it starts hiding real drift.
				if baseline[p] {
					t.Errorf("stale baseline: tool %q advertises %q again — delete "+
						"it from knownCatalogDrift. An allowlist nobody prunes "+
						"stops being a record of debt and becomes a hole.", name, p)
				}
				if overweight[p] {
					t.Errorf("stale entry: tool %q now advertises %q, so it was NOT "+
						"an unread field — delete it from openapiOverweight and "+
						"work out which side changed.", name, p)
				}
				continue
			}
			if baseline[p] || overweight[p] {
				continue
			}
			missing = append(missing, p)
		}
		for p := range got {
			if !want[p] {
				extra = append(extra, p)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("DRIFT on tool %q (%s %s): spec/openapi.json and "+
				"spec/mcp-catalog.json disagree about its parameters.\n"+
				"  in openapi but not in the catalog: %v (agents cannot send these)\n"+
				"  in the catalog but not in openapi: %v (agents are told to send "+
				"these and the server does not read them)\n"+
				"  Fix BOTH files; they are the same fact written twice.",
				name, spec.Method, spec.Path, missing, extra)
		}
	}
	if compared < 20 {
		t.Fatalf("only %d tools were actually compared — the routes/openapi join "+
			"has broken and this test has stopped discriminating (dead assertion, "+
			"the failure mode this file exists to prevent)", compared)
	}
	t.Logf("confronted %s tool(s) across routes table ≡ openapi ≡ frozen catalog",
		fmt.Sprint(compared))
}
