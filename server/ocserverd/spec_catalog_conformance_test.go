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
// was first written (T-74f8 round-2 rework). Every entry is a query parameter
// that spec/openapi.json accepts and the frozen catalog does not advertise, so
// an agent — whose only view of a tool is tools/list — cannot discover it.
//
// They are baselined rather than fixed HERE on purpose: repairing the catalog
// changes the tools/list wire surface and therefore catalog_hash for seven
// tools that have nothing to do with the handoff gate, which is a wire change
// that belongs in its own ticket with its own conformance run. Baselining keeps
// this test fail-closed for anything NEW while being loud about the debt.
//
// The map is checked in BOTH directions (see the rot assertion below): if one
// of these is fixed, this test fails until the entry is deleted. A stale
// allowlist that silently permits drift is the same disease as a stale comment,
// and this file exists because of a stale comment.
var knownCatalogDrift = map[string][]string{
	"ingest_telemetry":   {"binaries", "claude"},
	"list_task_manuals":  {"view"},
	"update_task_manual": {"display_name"},
	"list_tasks":         {"open"},
	"get_members":        {"fields"},
	"open_gate":          {"bind"},
	"get_chat":           {"peek"},
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
// ($ref resolved through components/schemas). ok is false when the operation is
// absent, or when its body is not JSON (upload routes — nothing to compare).
func openapiParamsFor(spec openapiSpec, method, path string) (map[string]bool, bool) {
	ops, hit := spec.Paths[path]
	if !hit {
		return nil, false
	}
	op, hit := ops[strings.ToLower(method)]
	if !hit {
		return nil, false
	}
	out := map[string]bool{}
	for _, p := range op.Parameters {
		out[p.Name] = true
	}
	if len(op.RequestBody.Content) > 0 {
		body, isJSON := op.RequestBody.Content["application/json"]
		if !isJSON {
			return nil, false // multipart upload — out of scope
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
	return out, true
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
		want, comparable := openapiParamsFor(api, spec.Method, spec.Path)
		if !comparable {
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
		var missing, extra []string
		for p := range want {
			if got[p] {
				// Baselined but actually present now: the debt was paid and the
				// allowlist has to shrink, or it starts hiding real drift.
				if baseline[p] {
					t.Errorf("stale baseline: tool %q advertises %q again — delete "+
						"it from knownCatalogDrift. An allowlist nobody prunes "+
						"stops being a record of debt and becomes a hole.", name, p)
				}
				continue
			}
			if baseline[p] {
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
