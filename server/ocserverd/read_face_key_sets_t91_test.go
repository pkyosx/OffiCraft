package main

// read_face_key_sets_t91_test.go — the two READ faces carry no dead pending
// flags (T-91 follow-through, owner 2026-09-06).
//
// WHAT WENT WRONG THAT THIS PINS. `activation_pending`, `relocation_pending`
// and `relocation_deferred` were response-only decorations: a handler computed
// them at dispatch time and wrote them onto the row it was about to answer with.
// When the fifteen lifecycle writes moved to bounded receipts, the three flags
// moved WITH them — onto MemberActivateReceiptDTO, AgentRelocateReceiptDTO and
// OutsourceRestartReceiptDTO — and nothing was left that could ever set them on
// memberDTO / outsourceWorkerDTO. They sat there for one commit as six fields
// that were structurally always absent, still carrying a description that said
// "set ONLY on the activate/relocate response" about a response that no longer
// existed. This change deletes them; this test is what keeps them deleted.
//
// 🔴 KEY-SET EQUALITY, NOT ABSENCE CHECKS, and the difference is the whole point:
// an assertion that only says `activation_pending` is missing goes green again
// the moment someone re-adds a DIFFERENT dead flag, and an assertion that only
// names the keys it wants goes green when the struct grows. Equality against a
// written-out set is the only shape where BOTH a re-added field and a quietly
// dropped one redden — and a reader who reddens it is told which of the two
// happened by the diff of the two sets.
//
// The read faces are pinned by marshalling the DTOs rather than by driving a
// route, because `omitempty` is the only thing that could make a served body
// narrower than the struct and it is visible right here: memberDTO has none, so
// its body is these 30 keys on every read there is; outsourceWorkerDTO has
// exactly one (`compaction_count`), so its body is these 39 minus at most that.
// Its conformance twin (conformance/test_rest_happy.py) makes the same claim
// against a live server, but only for the member arm — the black-box harness
// cannot mint a worker row.

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The member roster row as every read face serves it. No omitempty on this
// struct, so this is an exact body key set, not a ceiling.
var memberReadFaceKeys = []string{
	"actual_effort", "actual_machine", "actual_model", "actual_runtime",
	"avatar_url", "desired_machine_id", "desired_state", "effort",
	"forced_stop_at", "id", "kind", "last_op", "last_op_at", "last_op_log",
	"last_op_ok", "last_op_reason", "machine", "model", "name", "owner_id",
	"presence", "refocus_deadline", "refocus_op", "refocus_since", "role_key",
	"role_name", "roster_status", "runtime", "schema_version",
	"terminal_attach_command", "unread_count",
}

// The worker row as list_outsource_workers / get_outsource_worker serve it.
// `compaction_count` is the ONE omitempty on the struct, so a served body is
// this set or this set minus that key — never anything else.
var workerReadFaceKeys = []string{
	"account", "actual_effort", "actual_machine", "actual_model",
	"actual_runtime", "avatar_url", "banked_cost", "codename",
	"compaction_count", "context_pct", "cost", "created_ts", "creator_id",
	"delegated_by", "desired_machine_id", "desired_state", "effort", "id",
	"last_op", "last_op_at", "last_op_log", "last_op_ok", "last_op_reason",
	"machine", "model", "presence", "refocus_deadline", "refocus_op",
	"refocus_since", "runtime", "status", "task_created_ts", "task_id", "task_no",
	"task_status", "task_title", "task_type_key", "task_type_name",
	"terminal_attach_command", "unread_count",
}

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
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	return got
}

func diffKeys(got, want []string) (extra, missing []string) {
	w := map[string]bool{}
	for _, k := range want {
		w[k] = true
	}
	g := map[string]bool{}
	for _, k := range got {
		g[k] = true
		if !w[k] {
			extra = append(extra, k)
		}
	}
	for _, k := range want {
		if !g[k] {
			missing = append(missing, k)
		}
	}
	return extra, missing
}

func TestMemberDTOReadFaceKeySet_T91(t *testing.T) {
	// A zero value is deliberate: with no omitempty on this struct the zero
	// value marshals every key, which is exactly the body shape a read serves.
	got := marshalledKeys(t, memberDTO{})
	want := append([]string(nil), memberReadFaceKeys...)
	sort.Strings(want)
	extra, missing := diffKeys(got, want)
	if len(extra) > 0 || len(missing) > 0 {
		t.Fatalf("memberDTO wire key set changed: unexpected keys %v, missing keys %v.\n"+
			"If %v includes activation_pending / relocation_pending / relocation_deferred, "+
			"read this first: those three are RESPONSE-ONLY signals and live on the "+
			"receipts (MemberActivateReceiptDTO, AgentRelocateReceiptDTO), not on the "+
			"roster row — nothing on any read path can set them here, so a field here "+
			"is a permanently-absent field with a description that lies (T-91). "+
			"If the new key is a genuine roster field, add it to memberReadFaceKeys "+
			"in this file and to _MEMBER_READ_KEYS in conformance/test_rest_happy.py.",
			extra, missing, extra)
	}
}

func TestOutsourceWorkerDTOReadFaceKeySet_T91(t *testing.T) {
	// compaction_count is omitempty; a non-zero value is set below so the
	// marshalled body carries the full declared set and the equality is exact.
	got := marshalledKeys(t, func() outsourceWorkerDTO {
		n := 1
		return outsourceWorkerDTO{CompactionCount: &n}
	}())
	want := append([]string(nil), workerReadFaceKeys...)
	sort.Strings(want)
	extra, missing := diffKeys(got, want)
	if len(extra) > 0 || len(missing) > 0 {
		t.Fatalf("outsourceWorkerDTO wire key set changed: unexpected keys %v, missing keys %v.\n"+
			"If %v includes activation_pending / relocation_pending / relocation_deferred, "+
			"read this first: those three moved to AgentRelocateReceiptDTO / "+
			"OutsourceRestartReceiptDTO with the write reshape, and no handler can set "+
			"them on the worker row any more (T-91). If the new key is a genuine worker "+
			"field, add it to workerReadFaceKeys in this file.",
			extra, missing, extra)
	}
}

// ── the same claim one level up: the CONTRACT, not just the served body ─────
//
// 🔴 WHY THE TWO TESTS ABOVE ARE NOT ENOUGH ON THEIR OWN, stated because it is
// exactly the hole a re-added field would slip through: all six deleted fields
// were `*bool` with `omitempty`, so a nil one marshals to NOTHING. Re-add any
// of them in that shape and the served body is byte-identical — the key-set
// tests above stay green, and so does their conformance twin, because there is
// no key to see. What DOES change is the declared contract: spec/openapi.json
// grows the property, and `bin/gen-ocapi` grows the field on the generated
// struct. So this test reads the generated struct's json tags (declaration, not
// marshalling) and holds it to the same two sets. A dead flag cannot grow back
// in its own shape without reddening here.

func declaredJSONKeys(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	keys := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		keys = append(keys, strings.Split(tag, ",")[0])
	}
	sort.Strings(keys)
	return keys
}

// 🔴 THE STRUCT THAT ACTUALLY SUPPLIES THE BODY IS THE HAND-WRITTEN ONE, and
// until this test existed nothing held IT to a declaration. The four other
// assertions in this file leave one shape uncovered, and it is the shape that
// ships:
//
//   - `marshalledKeys` (the two tests above) cannot see it. All six deleted
//     fields were `*bool` with `omitempty`; re-add one to memberDTO in that
//     shape and a nil pointer marshals to nothing, so the served JSON is
//     byte-for-byte what it was. Nothing to compare, nothing to redden.
//   - `TestGeneratedReadDTOsDeclareNoDeadPendingFlags_T91` does read
//     declarations, but it reads MemberDTO / OutsourceWorkerDTO — the structs
//     `bin/gen-ocapi` writes out of the spec. Those are not what any handler
//     answers with (api_helpers.go and api_outsource.go build memberDTO and
//     outsourceWorkerDTO), so a field added only to the hand-written struct
//     never reaches them.
//   - `TestSpecReadDTOsDeclareNoDeadPendingFlags_T91` reads the spec, and the
//     hand-written struct can grow a field without the spec being touched at
//     all — that is precisely the drift the spec-first rule exists to prevent
//     and precisely the drift a test has to catch when someone skips it.
//
// So: same `declaredJSONKeys`, same two written-out sets, aimed at the two
// structs on the wire. A dead flag re-added straight to wire.go reddens HERE
// and nowhere else.
func TestHandWrittenReadDTOsDeclareNoDeadPendingFlags_T91(t *testing.T) {
	for _, tc := range []struct {
		name    string
		zero    any
		want    []string
		setName string
	}{
		{"memberDTO", memberDTO{}, memberReadFaceKeys, "memberReadFaceKeys"},
		{"outsourceWorkerDTO", outsourceWorkerDTO{}, workerReadFaceKeys, "workerReadFaceKeys"},
	} {
		want := append([]string(nil), tc.want...)
		sort.Strings(want)
		extra, missing := diffKeys(declaredJSONKeys(t, tc.zero), want)
		if len(extra) > 0 || len(missing) > 0 {
			t.Errorf("hand-written %s (server/ocserverd/wire.go — the struct the "+
				"handlers actually answer with) declares unexpected %v, missing %v. "+
				"activation_pending / relocation_pending / relocation_deferred belong "+
				"on the receipts the writes answer, never on a read face (T-91). Note "+
				"that a re-added *bool with omitempty is INVISIBLE to the marshalling "+
				"tests in this file and to the generated-struct and spec tests below, "+
				"which is why this assertion exists: if one of them is back here, it is "+
				"back on the served body. If the new key is a genuine field, add it to "+
				"%s in this file — and to the spec, or the generated struct and this "+
				"struct will disagree.", tc.name, extra, missing, tc.setName)
		}
	}
}

func TestGeneratedReadDTOsDeclareNoDeadPendingFlags_T91(t *testing.T) {
	for _, tc := range []struct {
		name string
		zero any
		want []string
	}{
		{"MemberDTO", MemberDTO{}, memberReadFaceKeys},
		{"OutsourceWorkerDTO", OutsourceWorkerDTO{}, workerReadFaceKeys},
	} {
		want := append([]string(nil), tc.want...)
		sort.Strings(want)
		extra, missing := diffKeys(declaredJSONKeys(t, tc.zero), want)
		if len(extra) > 0 || len(missing) > 0 {
			t.Errorf("generated %s (from spec/openapi.json via bin/gen-ocapi) declares "+
				"unexpected %v, missing %v. activation_pending / relocation_pending / "+
				"relocation_deferred belong on the receipts the writes answer, never on "+
				"a read face (T-91) — if one of them is back, the spec grew it back and "+
				"the fix is in spec/openapi.json, not here.", tc.name, extra, missing)
		}
	}
}

// The spec itself, read as text, so the claim does not depend on the generator
// having been re-run. `bin/gen-ocapi` is what keeps ocapi_gen.go and the spec in
// step, but a spec edit without a regen would leave the test above green while
// the published contract already carried the dead field.
func TestSpecReadDTOsDeclareNoDeadPendingFlags_T91(t *testing.T) {
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
	for _, tc := range []struct {
		name string
		want []string
	}{
		{"MemberDTO", memberReadFaceKeys},
		{"OutsourceWorkerDTO", workerReadFaceKeys},
	} {
		sch, ok := doc.Components.Schemas[tc.name]
		if !ok {
			t.Fatalf("spec/openapi.json has no %s schema", tc.name)
		}
		got := make([]string, 0, len(sch.Properties))
		for k := range sch.Properties {
			got = append(got, k)
		}
		sort.Strings(got)
		want := append([]string(nil), tc.want...)
		sort.Strings(want)
		extra, missing := diffKeys(got, want)
		if len(extra) > 0 || len(missing) > 0 {
			t.Errorf("spec/openapi.json %s declares unexpected %v, missing %v. "+
				"The three pending flags are RESPONSE-ONLY and live on "+
				"MemberActivateReceiptDTO / AgentRelocateReceiptDTO / "+
				"OutsourceRestartReceiptDTO; nothing can set them on a read face, so a "+
				"property here is a permanently-absent field carrying a description "+
				"that lies (T-91).", tc.name, extra, missing)
		}
	}
}
