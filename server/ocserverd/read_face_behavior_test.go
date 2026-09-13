package main

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var memberReadFaceKeys = []string{
	"actual_effort", "actual_machine", "actual_model", "actual_runtime",
	"avatar_url", "desired_machine_id", "desired_state", "effort",
	"forced_stop_at", "id", "kind", "last_op", "last_op_at", "last_op_log",
	"last_op_ok", "last_op_reason", "machine", "model", "name", "owner_id",
	"presence", "refocus_deadline", "refocus_op", "refocus_since", "role_key",
	"role_name", "roster_status", "runtime", "schema_version",
	"terminal_attach_command", "unread_count",
}

var memberDeclaredFaceKeys = append(append([]string{}, memberReadFaceKeys...),
	"account", "banked_cost", "compaction_count", "context_pct", "cost",
	"created_ts", "creator_id", "delegated_by", "status", "task_created_ts",
	"task_id", "task_no", "task_status", "task_title", "task_type_key",
	"task_type_name",
)

func marshalledKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func declaredJSONKeys(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	keys := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag != "" && tag != "-" {
			keys = append(keys, strings.Split(tag, ",")[0])
		}
	}
	sort.Strings(keys)
	return keys
}

func diffKeys(got, want []string) (extra, missing []string) {
	wanted := map[string]bool{}
	for _, key := range want {
		wanted[key] = true
	}
	seen := map[string]bool{}
	for _, key := range got {
		seen[key] = true
		if !wanted[key] {
			extra = append(extra, key)
		}
	}
	for _, key := range want {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	return extra, missing
}

func assertJSONKeys(t *testing.T, got, want []string) {
	t.Helper()
	want = append([]string(nil), want...)
	sort.Strings(want)
	extra, missing := diffKeys(got, want)
	if len(extra) > 0 || len(missing) > 0 {
		t.Fatalf("JSON key set changed: unexpected %v, missing %v", extra, missing)
	}
}

func TestMemberDTOReadFaceKeySet_T91(t *testing.T) {
	assertJSONKeys(t, marshalledKeys(t, memberDTO{}), memberReadFaceKeys)
}

func TestMemberDTODeclaredKeySet_T91(t *testing.T) {
	assertJSONKeys(t, declaredJSONKeys(t, memberDTO{}), memberDeclaredFaceKeys)
	assertJSONKeys(t, declaredJSONKeys(t, MemberDTO{}), memberDeclaredFaceKeys)

	raw, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	schema, ok := doc.Components.Schemas["MemberDTO"]
	if !ok {
		t.Fatal("spec/openapi.json has no MemberDTO schema")
	}
	keys := make([]string, 0, len(schema.Properties))
	for key := range schema.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	assertJSONKeys(t, keys, memberDeclaredFaceKeys)
}
