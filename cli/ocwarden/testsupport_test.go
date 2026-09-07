package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fakeRunner is the shell seam: an argv key → canned stdout, and os.ErrNotExist
// for anything the fixture does not stage.
type fakeRunner struct{ out map[string]string }

func (f fakeRunner) Run(name string, args ...string) (string, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	if canned, ok := f.out[key]; ok {
		return canned, nil
	}
	return "", os.ErrNotExist
}

// realVMStat is verbatim `vm_stat` from a 64 GiB Apple-silicon box (page size
// 16384); realMemTotal is `sysctl -n hw.memsize` from the same box.
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

const realMemTotal = "68719476736\n"

var fakeProbes = map[string]string{
	"pmset -g batt":             "Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t87%; charged; 0:00 remaining present: true",
	"top -l1 -n0":               "CPU usage: 12.50% user, 7.50% sys, 80.00% idle\nPhysMem: 12G used (2G wired), 4G unused.",
	"scutil --get ComputerName": "Seth's MacBook Pro\n",
	"vm_stat":                   realVMStat,
	"sysctl -n hw.memsize":      realMemTotal,
}

// schemaNode is as much of a JSON-Schema node as the walkers below need.
type schemaNode struct {
	Properties           map[string]*schemaNode `json:"properties"`
	AdditionalProperties json.RawMessage        `json:"additionalProperties"`
	Type                 string                 `json:"type"`
	Required             []string               `json:"required"`
	AnyOf                []*schemaNode          `json:"anyOf"`
}

// declaredTypes flattens the `anyOf: [{type: x}, {type: null}]` shape the spec
// uses for every nullable field. Empty = the node declares no type at all.
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

// closed reports additionalProperties:false — the setting that makes the server
// refuse an undeclared key instead of storing it.
func (n *schemaNode) closed() bool {
	return strings.TrimSpace(string(n.AdditionalProperties)) == "false"
}

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

// frozenRequestSchema resolves the request schema for one ROUTE, following the
// spec's own requestBody $ref. Looked up by route rather than by DTO name: a test
// that spells the DTO keeps comparing against it after the operation has been
// repointed somewhere else.
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
		t.Fatalf("the frozen spec has no %s %s — there is no schema to compare against",
			strings.ToUpper(method), route)
	}
	const prefix = "#/components/schemas/"
	ref := operation.RequestBody.Content["application/json"].Schema.Ref
	if !strings.HasPrefix(ref, prefix) {
		t.Fatalf("%s %s declares no application/json requestBody $ref (got %q)",
			strings.ToUpper(method), route, ref)
	}
	name := strings.TrimPrefix(ref, prefix)
	schema, ok := spec.Components.Schemas[name]
	if !ok || schema == nil {
		t.Fatalf("%s is referenced by %s %s but not defined in the frozen spec",
			name, strings.ToUpper(method), route)
	}
	if !schema.closed() {
		t.Fatalf("%s is not a closed schema — comparing against it proves nothing", name)
	}
	return schema
}

// undeclaredPayloadKeys returns the dotted paths of keys the frozen spec does not
// declare, descending wherever the schema declares properties for that path.
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
				continue
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

// missingRequiredKeys returns the dotted paths the spec REQUIRES but the producer
// did not send.
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

// mistypedPayloadValues returns dotted paths whose value type is not one the spec
// declares, as `path: got <type>, want <types>`. Undeclared keys belong to
// undeclaredPayloadKeys; a declared node with no declared type is skipped.
func mistypedPayloadValues(payload map[string]any, node *schemaNode) []string {
	var bad []string
	var walk func(map[string]any, *schemaNode, string)
	walk = func(obj map[string]any, at *schemaNode, prefix string) {
		for key, value := range obj {
			child, declared := at.Properties[key]
			if !declared || child == nil {
				continue
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

// wireBodies runs `drive` against a test server and returns every JSON body that
// really went out, in order.
func wireBodies(t *testing.T, drive func(base string)) []map[string]any {
	t.Helper()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("producer sent a body that is not a JSON object: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	drive(server.URL)
	return bodies
}
