package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// apiTestTaskCloseoutSeed is the task close-out document exactly as this build
// ships it: the read-only head the server fills in, the marker line, and the
// editable body under it. The three constants are the three halves the read
// face names.
const (
	apiTestTaskCloseoutSeed = "任務 {task_no} 已結案，結案的人是 {closed_by}。\n\n<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->\n\n停下這個任務，移除你開啟的外部資源，並停止更新 task。\n"

	apiTestTaskCloseoutHead = "任務 {task_no} 已結案，結案的人是 {closed_by}。"

	apiTestTaskCloseoutBody = "停下這個任務，移除你開啟的外部資源，並停止更新 task。\n"
)

const apiTestBootDocMarker = "<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->"

// apiTestTakeoverFreshSeed is the 〈新任務〉 document exactly as this build ships
// it — the smallest split document, so its whole text can be written down beside
// the folds that produce it.
const (
	apiTestTakeoverFreshSeed = "[{task_no}] 你接手了這張任務。\n\n<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->\n\n請先讀任務內容，準備好後由你自己呼叫 claim_task（認領）解除轉派鎖再開始執行；任務狀態一律照步驟推導，不必也不能自己報。\n"
	apiTestTakeoverFreshHead = "[{task_no}] 你接手了這張任務。"
	apiTestTakeoverFreshBody = "請先讀任務內容，準備好後由你自己呼叫 claim_task（認領）解除轉派鎖再開始執行；任務狀態一律照步驟推導，不必也不能自己報。\n"
)

// apiTestUnblockedHead / apiTestUnblockedBody are the two halves of 〈解除阻擋〉,
// the one document of the ten whose body is a bullet list and whose join is
// therefore a blank line.
const (
	apiTestUnblockedHead = "[{blocked_task_no}] 擋著這張任務的前置任務已經結束，它不再擋著你。"
	apiTestUnblockedBody = "- **還沒開始**：請 get_task 讀內容、submit_plan 規劃步驟後開始執行。\n- **已經在進行中**：接著推進，不必重新規劃。\n- **優先權是凍結**：先問清楚為什麼被凍結，等能解凍的人解開再動。\n"
)

// apiTestAcceleratedHeadAt is the 〈加速停止〉 head as it renders for the instant
// 1767225600, the epoch second the wind-down tests hand it.
const apiTestAcceleratedHeadAt = "你的結束時刻是 2026-01-01T00:00:00Z。"

// apiTestCloseoutHeadFilled is 〈任務結案〉's head with both of its names filled.
const apiTestCloseoutHeadFilled = "任務 T-1 已結案，結案的人是 owner。"

// apiTestBootDocRow is one expected row of bootDocRegistry, written out rather
// than read off the registry: the two lists are compared, so a kind added,
// dropped or re-declared shows up here as a failure.
type apiTestBootDocRow struct {
	kind     string
	keys     []string
	seeds    []string
	docNames []string
	capChars int
	vars     []string
	split    bool
	join     string
	readOnly bool
}

var apiTestBootDocRows = []apiTestBootDocRow{
	{
		kind: "system_interaction", keys: []string{"global"},
		seeds: []string{"system_interaction.md"}, docNames: []string{"system interaction block"},
		capChars: 60000, vars: nil, split: false, join: "", readOnly: false,
	}, {
		kind: "boot_sequence", keys: []string{"claude", "codex"},
		seeds:    []string{"boot_sequence.md", "boot_sequence_codex.md"},
		docNames: []string{"boot steps (claude)", "boot steps (codex)"},
		capChars: 15000, vars: nil, split: false, join: "", readOnly: false,
	}, {
		kind: "offboard", keys: []string{"global"},
		seeds: []string{"offboard.md"}, docNames: []string{"Stop document"},
		capChars: 15000, vars: []string{}, split: false, join: "", readOnly: false,
	}, {
		kind: "accelerated_stop", keys: []string{"global"},
		seeds: []string{"accelerated_stop.md"}, docNames: []string{"accelerated stop sequence"},
		capChars: 15000, vars: []string{"deadline"}, split: true, join: "\n", readOnly: false,
	}, {
		kind: "task_closeout", keys: []string{"global"},
		seeds: []string{"task_closeout.md"}, docNames: []string{"task close-out procedure"},
		capChars: 15000, vars: []string{"task_no", "closed_by"}, split: true, join: "\n", readOnly: false,
	}, {
		kind: "task_reassign_predecessor", keys: []string{"global"},
		seeds:    []string{"task_reassign_predecessor.md"},
		docNames: []string{"task reassignment document (to the predecessor)"},
		capChars: 15000, vars: []string{"task_no"}, split: true, join: "", readOnly: false,
	}, {
		kind: "task_takeover_with_predecessor", keys: []string{"global"},
		seeds:    []string{"task_takeover_with_predecessor.md"},
		docNames: []string{"task reassignment document (to the successor)"},
		capChars: 15000, vars: []string{"task_no", "predecessor"}, split: true, join: "", readOnly: false,
	}, {
		kind: "task_takeover_fresh", keys: []string{"global"},
		seeds: []string{"task_takeover_fresh.md"}, docNames: []string{"new task document"},
		capChars: 15000, vars: []string{"task_no"}, split: true, join: "", readOnly: false,
	}, {
		kind: "task_unblocked", keys: []string{"global"},
		seeds: []string{"task_unblocked.md"}, docNames: []string{"dependency-released notice"},
		capChars: 15000, vars: []string{"blocked_task_no"}, split: true, join: "\n\n", readOnly: false,
	}, {
		kind: "task_ready_for_done", keys: []string{"global"},
		seeds: []string{"task_ready_for_done.md"}, docNames: []string{"ready-for-done notice"},
		capChars: 15000, vars: []string{"task_no", "visit_no"}, split: true, join: "\n\n", readOnly: false,
	},
}

func TestBootDocRegFor(t *testing.T) {
	t.Run("every kind this build ships answers its own row, and the registry holds exactly those and nothing else", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		if len(bootDocRegistry) != len(apiTestBootDocRows) {
			t.Fatalf("bootDocRegistry holds %d kinds, want %d", len(bootDocRegistry), len(apiTestBootDocRows))
		}
		for i, want := range apiTestBootDocRows {
			if bootDocRegistry[i].Kind != want.kind {
				t.Fatalf("bootDocRegistry[%d].Kind = %q, want %q", i, bootDocRegistry[i].Kind, want.kind)
			}
			reg, ok := bootDocRegFor(want.kind)
			if !ok {
				t.Fatalf("bootDocRegFor(%q): not found", want.kind)
			}
			if !reflect.DeepEqual(reg.Keys, want.keys) {
				t.Fatalf("%s Keys = %#v, want %#v", want.kind, reg.Keys, want.keys)
			}
			if !reflect.DeepEqual(reg.Vars, want.vars) {
				t.Fatalf("%s Vars = %#v, want %#v", want.kind, reg.Vars, want.vars)
			}
			if reg.Split != want.split || reg.Join != want.join || reg.ReadOnly != want.readOnly {
				t.Fatalf("%s Split/Join/ReadOnly = %v/%q/%v, want %v/%q/%v",
					want.kind, reg.Split, reg.Join, reg.ReadOnly, want.split, want.join, want.readOnly)
			}
			if got := reg.Cap(api); got != want.capChars {
				t.Fatalf("%s Cap = %d, want %d", want.kind, got, want.capChars)
			}
			for j, key := range want.keys {
				if got := reg.SeedFor(key); got != want.seeds[j] {
					t.Fatalf("%s SeedFor(%q) = %q, want %q", want.kind, key, got, want.seeds[j])
				}
				if got := reg.DocName(key); got != want.docNames[j] {
					t.Fatalf("%s DocName(%q) = %q, want %q", want.kind, key, got, want.docNames[j])
				}
			}
		}
	})

	t.Run("a kind nobody registered answers the zero row and not-found, so no seed or cap can be read off it", func(t *testing.T) {
		for _, kind := range []string{"bogus", "", "System_Interaction", "task_description"} {
			reg, ok := bootDocRegFor(kind)
			if ok {
				t.Fatalf("bootDocRegFor(%q) reported found", kind)
			}
			if !reflect.DeepEqual(reg, bootDocReg{}) {
				t.Fatalf("bootDocRegFor(%q) = %#v, want the zero row", kind, reg)
			}
		}
	})
}

func TestServes(t *testing.T) {
	t.Run("a kind serves exactly the keys it declares, and nothing that merely resembles one", func(t *testing.T) {
		bootSequence, _ := bootDocRegFor("boot_sequence")
		closeout, _ := bootDocRegFor("task_closeout")
		for name, c := range map[string]struct {
			reg  bootDocReg
			key  string
			want bool
		}{
			"the claude boot sequence":                 {bootSequence, "claude", true},
			"the codex boot sequence":                  {bootSequence, "codex", true},
			"a boot sequence addressed as a singleton": {bootSequence, "global", false},
			"a boot sequence with a capitalised key":   {bootSequence, "Codex", false},
			"a boot sequence with no key at all":       {bootSequence, "", false},
			"the task close-out singleton":             {closeout, "global", true},
			"the task close-out under a runtime key":   {closeout, "claude", false},
			"the zero row, which serves nothing":       {bootDocReg{}, "global", false},
		} {
			if got := c.reg.serves(c.key); got != c.want {
				t.Fatalf("%s: serves(%q) = %v, want %v", name, c.key, got, c.want)
			}
		}
	})
}

func TestMustBootDocSpec(t *testing.T) {
	t.Run("a pair this binary is built with resolves to the whole spec that pair declares", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got := api.mustBootDocSpec("task_closeout", "global")

		want := bootDocSpec{
			Kind: "task_closeout", Key: "global", SeedFile: "task_closeout.md", Cap: 15000,
			DocName: "task close-out procedure", Vars: []string{"task_no", "closed_by"},
			Split: true, Join: "\n", ReadOnly: false,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("spec = %#v, want %#v", got, want)
		}
	})

	t.Run("a pair no row addresses panics naming the pair rather than degrading to an empty spec", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for name, c := range map[string]struct{ kind, key, want string }{
			"an unregistered kind":      {"bogus", "global", "boot document bogus/global is not in bootDocRegistry"},
			"a key the kind refuses":    {"task_closeout", "claude", "boot document task_closeout/claude is not in bootDocRegistry"},
			"a boot sequence with none": {"boot_sequence", "", "boot document boot_sequence/ is not in bootDocRegistry"},
		} {
			func() {
				defer func() {
					got, _ := recover().(string)
					if got != c.want {
						t.Fatalf("%s: recovered %#v, want %q", name, got, c.want)
					}
				}()
				api.mustBootDocSpec(c.kind, c.key)
				t.Fatalf("%s: mustBootDocSpec returned instead of panicking", name)
			}()
		}
	})
}

func TestBootDocSpecFor(t *testing.T) {
	t.Run("every (kind, key) pair the registry serves resolves to the seed, cap, name and split rules that pair declares", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for _, row := range apiTestBootDocRows {
			for i, key := range row.keys {
				got, ok := api.bootDocSpecFor(row.kind, key)
				if !ok {
					t.Fatalf("bootDocSpecFor(%q, %q): not found", row.kind, key)
				}
				want := bootDocSpec{
					Kind: row.kind, Key: key, SeedFile: row.seeds[i], Cap: row.capChars,
					DocName: row.docNames[i], Vars: row.vars,
					Split: row.split, Join: row.join, ReadOnly: row.readOnly,
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("bootDocSpecFor(%q, %q) = %#v, want %#v", row.kind, key, got, want)
				}
			}
		}
	})

	t.Run("a kind that declares no variables at all is told apart from one that declares an empty set", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		unvalidated, _ := api.bootDocSpecFor("system_interaction", "global")
		validated, _ := api.bootDocSpecFor("offboard", "global")

		if unvalidated.Vars != nil {
			t.Fatalf("system_interaction Vars = %#v, want nil", unvalidated.Vars)
		}
		if validated.Vars == nil || len(validated.Vars) != 0 {
			t.Fatalf("offboard Vars = %#v, want an empty non-nil slice", validated.Vars)
		}
	})

	t.Run("a pair naming no document answers the zero spec and not-found", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for name, c := range map[string]struct{ kind, key string }{
			"an unregistered kind":                               {"bogus", "global"},
			"a registered kind under a wrong key":                {"task_closeout", "bogus"},
			"a boot sequence under a wrong case":                 {"boot_sequence", "Codex"},
			"a document-history kind from elsewhere in the tree": {"task_manual", "global"},
			"no kind and no key at all":                          {"", ""},
		} {
			got, ok := api.bootDocSpecFor(c.kind, c.key)
			if ok {
				t.Fatalf("%s: bootDocSpecFor(%q, %q) reported found", name, c.kind, c.key)
			}
			if !reflect.DeepEqual(got, bootDocSpec{}) {
				t.Fatalf("%s: spec = %#v, want the zero spec", name, got)
			}
		}
	})
}

func TestBootDocHistoryKeyKnown(t *testing.T) {
	t.Run("it answers true for exactly the pairs bootDocSpecFor resolves, without a server to ask", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for _, row := range apiTestBootDocRows {
			for _, key := range row.keys {
				if !bootDocHistoryKeyKnown(row.kind, key) {
					t.Fatalf("bootDocHistoryKeyKnown(%q, %q) = false", row.kind, key)
				}
			}
		}
		for name, c := range map[string]struct{ kind, key string }{
			"an unregistered kind":                {"bogus", "global"},
			"a registered kind under a wrong key": {"task_closeout", "bogus"},
			"a boot sequence under a wrong case":  {"boot_sequence", "Codex"},
			"no kind and no key at all":           {"", ""},
		} {
			if bootDocHistoryKeyKnown(c.kind, c.key) {
				t.Fatalf("%s: bootDocHistoryKeyKnown(%q, %q) = true", name, c.kind, c.key)
			}
			if _, ok := api.bootDocSpecFor(c.kind, c.key); ok {
				t.Fatalf("%s: bootDocSpecFor disagrees and resolves the pair", name)
			}
		}
	})
}

func TestUnknownBootDocKeyMsg(t *testing.T) {
	t.Run("a kind nobody registered is told so, and a bad key is told the keys that kind does serve", func(t *testing.T) {
		for name, c := range map[string]struct{ kind, key, want string }{
			"an unregistered kind": {"bogus", "global",
				"document history kind 'bogus' names no editable document on this server"},
			"an empty kind": {"", "global",
				"document history kind '' names no editable document on this server"},
			"a typo against the two-key kind": {"boot_sequence", "opus",
				"document history key 'opus' does not name a boot_sequence document — the key is 'claude' or 'codex'"},
			"a typo against a singleton kind": {"task_closeout", "zz",
				"document history key 'zz' does not name a task_closeout document — the key is 'global'"},
			"an empty key against a singleton kind": {"accelerated_stop", "",
				"document history key '' does not name a accelerated_stop document — the key is 'global'"},
		} {
			if got := unknownBootDocKeyMsg(c.kind, c.key); got != c.want {
				t.Fatalf("%s: got %q, want %q", name, got, c.want)
			}
		}
	})
}

func TestFoldBootDocDTO(t *testing.T) {
	t.Run("a split document nobody has edited folds the shipped seed into the whole text, its read-only head and the half a write takes", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		dto, err := api.foldBootDocDTO(api.mustBootDocSpec("task_takeover_fresh", "global"))
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, dto), map[string]any{
			"size_chars":     127,
			"cap_chars":      15000,
			"kind":           "task_takeover_fresh",
			"key":            "global",
			"text":           apiTestTakeoverFreshSeed,
			"read_only_head": apiTestTakeoverFreshHead,
			"body":           apiTestTakeoverFreshBody,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("an overlay replaces the body under the shipped head and stops the document reading as the default", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_takeover_fresh/global", owner, `{"body":"F1"}`)

		dto, err := api.foldBootDocDTO(api.mustBootDocSpec("task_takeover_fresh", "global"))
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, dto), map[string]any{
			"size_chars":     63,
			"cap_chars":      15000,
			"kind":           "task_takeover_fresh",
			"key":            "global",
			"text":           apiTestTakeoverFreshHead + "\n\n" + apiTestBootDocMarker + "\n\nF1",
			"read_only_head": apiTestTakeoverFreshHead,
			"body":           "F1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("a kind that declares no read-only half names none, and its whole text is the half a write takes", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)

		dto, err := api.foldBootDocDTO(api.mustBootDocSpec("offboard", "global"))
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, dto), map[string]any{
			"size_chars":     2,
			"cap_chars":      15000,
			"kind":           "offboard",
			"key":            "global",
			"text":           "O1",
			"read_only_head": "",
			"body":           "O1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("a stored row from before the marker existed folds head-less, and its whole text reads as the body", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutBootDocument(BootDocument{
			Kind: "task_takeover_fresh", Key: "global", Text: "written before the marker existed",
		}); err != nil {
			t.Fatalf("PutBootDocument: %v", err)
		}

		dto, err := api.foldBootDocDTO(api.mustBootDocSpec("task_takeover_fresh", "global"))
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, dto), map[string]any{
			"size_chars":     33,
			"cap_chars":      15000,
			"kind":           "task_takeover_fresh",
			"key":            "global",
			"text":           "written before the marker existed",
			"read_only_head": "",
			"body":           "written before the marker existed",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
	})
}

func TestSystemInteractionText(t *testing.T) {
	t.Run("the boot fold reads the shipped block byte for byte, the same bytes the cockpit is served", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, data := apiJSON(t, h, "GET", "/api/system-interaction", owner, "")

		got, err := api.systemInteractionText()
		if err != nil {
			t.Fatalf("systemInteractionText: %v", err)
		}
		if got != data["text"] {
			t.Fatalf("the boot fold and the read face disagree (%d vs %d runes)",
				utf8.RuneCountInString(got), utf8.RuneCountInString(data["text"].(string)))
		}
		if n := utf8.RuneCountInString(got); n != 16906 {
			t.Fatalf("the shipped block is %d runes, want 16906", n)
		}
	})

	t.Run("an owner edit reaches the boot fold in place of the shipped block", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)

		got, err := api.systemInteractionText()
		if err != nil {
			t.Fatalf("systemInteractionText: %v", err)
		}
		if got != "S1" {
			t.Fatalf("systemInteractionText = %q, want %q", got, "S1")
		}
	})
}

func TestWinddownNoticeText(t *testing.T) {
	t.Run("a soft wind-down is sent the whole Stop document and no instant at all", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		dashboard := apiTestListen(t, api, "")

		if got := api.winddownNoticeText(offboardKindSoft, 0); got != "O1" {
			t.Fatalf("soft notice = %q, want %q", got, "O1")
		}
		if got := api.winddownNoticeText(offboardKindSoft, 1767225600); got != "O1" {
			t.Fatalf("a clock on the soft arm changed the notice: %q", got)
		}
		dashboard.wantFrames()
	})

	t.Run("the shipped Stop document is what a soft wind-down reads when nobody has edited it", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		if got := api.winddownNoticeText(offboardKindSoft, 0); got != apiTestOffboardNotice {
			t.Fatalf("soft notice is not the shipped 〈停止〉 document (%d runes)", utf8.RuneCountInString(got))
		}
	})

	t.Run("the final call reads the accelerated-stop document with the deadline rendered UTC above its body", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/accelerated_stop/global", owner, `{"body":"A1"}`)

		got := api.winddownNoticeText(offboardKindFinal, 1767225600)

		if want := apiTestAcceleratedHeadAt + "\nA1"; got != want {
			t.Fatalf("final notice = %q, want %q", got, want)
		}
	})

	t.Run("a final call with no clock sends nothing rather than a 1970 deadline", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/accelerated_stop/global", owner, `{"body":"A1"}`)

		for name, deadline := range map[string]float64{"no clock": 0, "a clock before the epoch": -1} {
			if got := api.winddownNoticeText(offboardKindFinal, deadline); got != "" {
				t.Fatalf("%s: final notice = %q, want the empty string", name, got)
			}
		}
	})

	t.Run("a stored accelerated-stop row from before the marker existed sends nothing rather than the instructions with the deadline sliced off", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutBootDocument(BootDocument{
			Kind: "accelerated_stop", Key: "global", Text: "written before the marker existed",
		}); err != nil {
			t.Fatalf("PutBootDocument: %v", err)
		}

		if got := api.winddownNoticeText(offboardKindFinal, 1767225600); got != "" {
			t.Fatalf("final notice = %q, want the empty string", got)
		}
	})
}

func TestTaskEventBodyText(t *testing.T) {
	t.Run("it answers the instructions alone, with neither the head's claim nor the document's trailing newline", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got := api.taskEventBodyText("task_takeover_fresh")

		if want := strings.TrimSuffix(apiTestTakeoverFreshBody, "\n"); got != want {
			t.Fatalf("body = %q, want %q", got, want)
		}
	})

	t.Run("an owner edit is the half this caller reads, and the shipped head is still not in it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)

		if got := api.taskEventBodyText("task_closeout"); got != "C1" {
			t.Fatalf("body = %q, want %q", got, "C1")
		}
	})

	t.Run("a stored row from before the marker existed answers nothing rather than a document whose head cannot be told from its body", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutBootDocument(BootDocument{
			Kind: "task_closeout", Key: "global", Text: "written before the marker existed",
		}); err != nil {
			t.Fatalf("PutBootDocument: %v", err)
		}

		if got := api.taskEventBodyText("task_closeout"); got != "" {
			t.Fatalf("body = %q, want the empty string", got)
		}
	})
}

func TestTaskNoticeText(t *testing.T) {
	t.Run("the shipped 〈新任務〉 notice runs its filled head into its body inside one paragraph, trimmed to a single chat row", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		got := api.taskNoticeText("task_takeover_fresh", map[string]string{"task_no": "T-9"})

		want := "[T-9] " + strings.TrimPrefix(apiTestTakeoverFreshHead, "[{task_no}] ") +
			strings.TrimSuffix(apiTestTakeoverFreshBody, "\n")
		if got != want {
			t.Fatalf("notice = %q, want %q", got, want)
		}
		dashboard.wantFrames()
	})

	t.Run("both of the close-out document's names are filled and its own join is used", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)

		got := api.taskNoticeText("task_closeout",
			map[string]string{"task_no": "T-1", "closed_by": "owner"})

		if want := apiTestCloseoutHeadFilled + "\nC1"; got != want {
			t.Fatalf("notice = %q, want %q", got, want)
		}
	})

	t.Run("a name the caller left out sends nothing rather than a sentence with the braces still in it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)

		for name, values := range map[string]map[string]string{
			"only one of the two names": {"task_no": "T-1"},
			"no names at all":           {},
			"nothing supplied at all":   nil,
		} {
			if got := api.taskNoticeText("task_closeout", values); got != "" {
				t.Fatalf("%s: notice = %q, want the empty string", name, got)
			}
		}
	})

	t.Run("a value the document never names is ignored rather than appended", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		with := api.taskNoticeText("task_takeover_fresh",
			map[string]string{"task_no": "T-9", "predecessor": "mira"})
		without := api.taskNoticeText("task_takeover_fresh", map[string]string{"task_no": "T-9"})

		if with != without {
			t.Fatalf("an unnamed value changed the notice: %q vs %q", with, without)
		}
	})
}

func TestEventNoticeText(t *testing.T) {
	t.Run("each kind joins its two halves its own way, so the same fold renders three shapes", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_takeover_fresh/global", owner, `{"body":"F1"}`)
		apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)
		apiJSON(t, h, "POST", "/api/boot-docs/task_unblocked/global", owner, `{"body":"U1"}`)

		for name, c := range map[string]struct {
			kind   string
			values map[string]string
			want   string
		}{
			"the paragraph join runs the head into the body": {
				"task_takeover_fresh", map[string]string{"task_no": "T-9"}, "[T-9] 你接手了這張任務。F1"},
			"the single-newline join stacks the body under the sentence": {
				"task_closeout", map[string]string{"task_no": "T-1", "closed_by": "owner"},
				apiTestCloseoutHeadFilled + "\nC1"},
			"the blank-line join separates the sentence from a bullet list": {
				"task_unblocked", map[string]string{"blocked_task_no": "T-3"},
				"[T-3] 擋著這張任務的前置任務已經結束，它不再擋著你。\n\nU1"},
		} {
			got := api.eventNoticeText(api.mustBootDocSpec(c.kind, "global"), c.values)
			if got != c.want {
				t.Fatalf("%s: got %q, want %q", name, got, c.want)
			}
		}
	})

	t.Run("a kind with no read-only half is rendered whole, marker line and all being absent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1\n\nO2"}`)

		got := api.eventNoticeText(api.mustBootDocSpec("offboard", "global"), map[string]string{})

		if got != "O1\n\nO2" {
			t.Fatalf("notice = %q, want %q", got, "O1\n\nO2")
		}
	})

	t.Run("a split kind whose stored text carries no marker is refused rather than sent head-less", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutBootDocument(BootDocument{
			Kind: "task_unblocked", Key: "global", Text: "written before the marker existed",
		}); err != nil {
			t.Fatalf("PutBootDocument: %v", err)
		}

		got := api.eventNoticeText(api.mustBootDocSpec("task_unblocked", "global"),
			map[string]string{"blocked_task_no": "T-3"})

		if got != "" {
			t.Fatalf("notice = %q, want the empty string", got)
		}
	})

	t.Run("the shipped 〈解除阻擋〉 notice is the whole document with the marker gone and the number filled", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got := api.eventNoticeText(api.mustBootDocSpec("task_unblocked", "global"),
			map[string]string{"blocked_task_no": "T-3"})

		want := "[T-3] " + strings.TrimPrefix(apiTestUnblockedHead, "[{blocked_task_no}] ") +
			"\n\n" + apiTestUnblockedBody
		if got != want {
			t.Fatalf("notice = %q, want %q", got, want)
		}
	})
}

func TestBootSequenceText(t *testing.T) {
	t.Run("each runtime reads its own document, and editing one leaves the other's alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"B1"}`)

		claude, err := api.bootSequenceText("claude")
		if err != nil {
			t.Fatalf("bootSequenceText(claude): %v", err)
		}
		if claude != "B1" {
			t.Fatalf("claude sequence = %q, want %q", claude, "B1")
		}
		codex, err := api.bootSequenceText("codex")
		if err != nil {
			t.Fatalf("bootSequenceText(codex): %v", err)
		}
		if n := utf8.RuneCountInString(codex); n != 2786 {
			t.Fatalf("the codex sequence is %d runes, want the shipped 2786", n)
		}
	})

	t.Run("a runtime with no sequence of its own reads the claude document rather than an empty one", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"B1"}`)
		apiJSON(t, h, "POST", "/api/boot-sequence/codex", owner, `{"body":"B2"}`)

		for _, runtime := range []string{"opus", "Codex", ""} {
			got, err := api.bootSequenceText(runtime)
			if err != nil {
				t.Fatalf("bootSequenceText(%q): %v", runtime, err)
			}
			if got != "B1" {
				t.Fatalf("bootSequenceText(%q) = %q, want the claude document %q", runtime, got, "B1")
			}
		}
	})

	t.Run("the shipped claude sequence is what an unedited server boots on", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, data := apiJSON(t, h, "GET", "/api/boot-sequence/claude", owner, "")

		got, err := api.bootSequenceText("claude")
		if err != nil {
			t.Fatalf("bootSequenceText: %v", err)
		}
		if got != data["text"] {
			t.Fatalf("the boot fold and the read face disagree (%d runes)", utf8.RuneCountInString(got))
		}
		if n := utf8.RuneCountInString(got); n != 2965 {
			t.Fatalf("the shipped claude sequence is %d runes, want 2965", n)
		}
	})
}

func TestBootDocSnapshotIn(t *testing.T) {
	t.Run("it serialises the row as it stands inside the transaction, text and tombstone together", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)

		got, err := bootDocSnapshotIn("offboard", "global")(d.rdb)

		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if want := `{"text":"O1","tombstoned":"false"}`; got != want {
			t.Fatalf("snapshot = %q, want %q", got, want)
		}
	})

	t.Run("a reset row is retained as the tombstone it is, not as the seed it now reads back as", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		apiJSON(t, h, "POST", "/api/offboard/reset", owner, "")

		got, err := bootDocSnapshotIn("offboard", "global")(d.rdb)

		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if want := `{"text":"","tombstoned":"true"}`; got != want {
			t.Fatalf("snapshot = %q, want %q", got, want)
		}
	})

	t.Run("a document nobody has ever written snapshots to the empty object, which retains no revision", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)

		got, err := bootDocSnapshotIn("offboard", "global")(d.rdb)

		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if got != "{}" {
			t.Fatalf("snapshot = %q, want %q", got, "{}")
		}
	})
}

func TestBootDocHistorySnapshot(t *testing.T) {
	t.Run("a row is retained as its text and its tombstone flag, and no row at all as the empty object", func(t *testing.T) {
		for name, c := range map[string]struct {
			row  *BootDocument
			want string
		}{
			"an edited row": {
				&BootDocument{Kind: "offboard", Key: "global", Text: "O1"},
				`{"text":"O1","tombstoned":"false"}`},
			"a tombstoned row": {
				&BootDocument{Kind: "offboard", Key: "global", Text: "O1", Tombstoned: true},
				`{"text":"O1","tombstoned":"true"}`},
			"a row holding an empty document": {
				&BootDocument{Kind: "offboard", Key: "global"},
				`{"text":"","tombstoned":"false"}`},
			"no row at all": {nil, "{}"},
		} {
			got, err := bootDocHistorySnapshot(c.row)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got != c.want {
				t.Fatalf("%s: got %q, want %q", name, got, c.want)
			}
		}
	})
}

func TestPublishBootDoc(t *testing.T) {
	t.Run("the change is fanned to the owner cockpit alone on the global_context topic, carrying no payload", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		api.publishBootDoc(req)

		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("the frame is attributed to whoever made the request", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		var req *http.Request
		taskTestUnderCaller(t, api, d, agent, func(r *http.Request) { req = r })
		dashboard := apiTestListen(t, api, "")

		api.publishBootDoc(req)

		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		})
	})
}

func TestWriteBootDoc(t *testing.T) {
	t.Run("a write that changes the text stores it, retains the revision it replaced and fans the delta", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		spec := api.mustBootDocSpec("offboard", "global")
		current, err := api.foldBootDocDTO(spec)
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
		dashboard := apiTestListen(t, api, "")

		wrote, err := api.writeBootDoc(req, spec, current,
			BootDocument{Kind: "offboard", Key: "global", Text: "O2"}, "O2")

		if err != nil || !wrote {
			t.Fatalf("writeBootDoc = %v, %v", wrote, err)
		}
		after, err := api.foldBootDocDTO(spec)
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		if after.Text != "O2" || after.IsDefault {
			t.Fatalf("stored document = %q (is_default %v)", after.Text, after.IsDefault)
		}
		apiWantValue(t, "history", apiTestBootDocHistory(t, d, "offboard", "global"), []any{
			map[string]any{"actor": "owner", "content": `{"text":"O1","tombstoned":"false"}`},
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a write that changes nothing stores nothing, retains nothing and fans nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		spec := api.mustBootDocSpec("offboard", "global")
		current, err := api.foldBootDocDTO(spec)
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
		dashboard := apiTestListen(t, api, "")

		wrote, err := api.writeBootDoc(req, spec, current,
			BootDocument{Kind: "offboard", Key: "global", Text: "O1"}, "O1")

		if err != nil || wrote {
			t.Fatalf("writeBootDoc = %v, %v", wrote, err)
		}
		after, err := api.foldBootDocDTO(spec)
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		if after.Text != "O1" || after.IsDefault {
			t.Fatalf("stored document = %q (is_default %v)", after.Text, after.IsDefault)
		}
		apiWantValue(t, "history", apiTestBootDocHistory(t, d, "offboard", "global"), []any{})
		dashboard.wantFrames()
	})

	t.Run("adopting the shipped bytes as an edit changes no text and is still a write, because the next reset turns on it", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		spec := api.mustBootDocSpec("offboard", "global")
		current, err := api.foldBootDocDTO(spec)
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		if !current.IsDefault {
			t.Fatalf("the fixture document is already edited: %#v", current)
		}
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
		dashboard := apiTestListen(t, api, "")

		wrote, err := api.writeBootDoc(req, spec, current,
			BootDocument{Kind: "offboard", Key: "global", Text: current.Text}, current.Text)

		if err != nil || !wrote {
			t.Fatalf("writeBootDoc = %v, %v", wrote, err)
		}
		after, err := api.foldBootDocDTO(spec)
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}
		if after.Text != current.Text || after.IsDefault {
			t.Fatalf("stored document reads as default %v after adopting the seed", after.IsDefault)
		}
		apiWantValue(t, "history", apiTestBootDocHistory(t, d, "offboard", "global"), []any{})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})
}

// apiTestBootDocHistory is the retained revisions of one document, newest
// first, reduced to the two facts a caller of writeBootDoc can observe.
func apiTestBootDocHistory(t *testing.T, d *DAL, kind, key string) any {
	t.Helper()
	rows, err := d.ListDocumentHistory(kind, key)
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	out := []any{}
	for _, row := range rows {
		out = append(out, map[string]any{"actor": row.ActorID, "content": row.ContentJSON})
	}
	return out
}

func TestReplaceBootDoc(t *testing.T) {
	t.Run("the body a caller reads back is the body it may write, and writing it changes only the default flag", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, read := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner,
			`{"body":`+apiTestJSONString(t, read["body"].(string))+`}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "task_closeout",
			"key":        "global",
			"is_default": false,
			"size_chars": 105,
			"cap_chars":  15000,
			"sha256":     "1a7b91966130612348b1b472b4956266ffc156f3c0cb04744c4b2086edb392f8",
		})
		_, after := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", owner, "")
		if after["text"] != read["text"] || after["body"] != read["body"] {
			t.Fatalf("the round trip changed the document: %q", after["text"])
		}
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a body over the cap is refused with the three numbers and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner,
			`{"body":"`+strings.Repeat("x", 15001)+`"}`)

		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"the task close-out procedure you are writing is 15076 chars, over the 15000-char cap, "+
				"and is not shorter than the 105 chars already stored — nothing was written. What is "+
				"already stored is never truncated, but every update must land at or under the cap, or "+
				"at least come out SHORTER than what is there now. Drop stale or superseded material as "+
				"part of this write (or in a shrinking write first), then write again.")
		_, after := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", owner, "")
		apiWantValue(t, "text", after["text"], apiTestTaskCloseoutSeed)
		dashboard.wantFrames()
	})

	t.Run("a body naming a declared variable is accepted and stored under the shipped head", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, read := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", owner, "")
		head, ok := read["read_only_head"].(string)
		if !ok || head == "" {
			t.Fatalf("the split document must serve its read-only head: %v", read)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner,
			`{"body":"closing {task_no}"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "kind", data["kind"], "task_closeout")
		apiWantValue(t, "key", data["key"], "global")
		apiWantValue(t, "is_default", data["is_default"], false)
		apiWantValue(t, "size_chars", data["size_chars"], apiAnyNumber)
		apiWantValue(t, "cap_chars", data["cap_chars"], 15000)
		apiWantValue(t, "sha256", data["sha256"], apiAnyString)
		_, after := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", owner, "")
		apiWantValue(t, "body", after["body"], "closing {task_no}")
		apiWantValue(t, "text", after["text"], DocJoinHeadBody(head, "closing {task_no}"))
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a read-only document is refused before anything is read, written or fanned", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
		spec := api.mustBootDocSpec("task_takeover_fresh", "global")
		spec.ReadOnly = true
		dashboard := apiTestListen(t, api, "")
		rec := httptest.NewRecorder()

		api.replaceBootDoc(rec, req, spec, "F1", false)

		if rec.Code != 405 {
			t.Fatalf("want 405, got %d (%s)", rec.Code, rec.Body.String())
		}
		apiWantError(t, apiTestDecodeJSONBody(t, rec), "method_not_allowed",
			"the new task document is a read-only document — it is shown so you can see what "+
				"agents are told, but no caller may edit it and there is no version of it other "+
				"than the shipped one; nothing was written")
		stored, err := d.GetBootDocument("task_takeover_fresh", "global")
		if err != nil || stored != nil {
			t.Fatalf("an overlay row was written: %#v (%v)", stored, err)
		}
		dashboard.wantFrames()
	})
}

// apiTestJSONString quotes a string as a JSON literal, for a request body built
// around a document the server itself just answered with.
func apiTestJSONString(t *testing.T, s string) string {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// apiTestDecodeJSONBody decodes the answer of a handler called directly, the
// way apiJSON decodes one driven through the whole stack.
func apiTestDecodeJSONBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("non-JSON body (%d): %s", rec.Code, rec.Body.String())
	}
	return data
}

func TestBootDocReceiptOf(t *testing.T) {
	t.Run("the receipt keeps the address, the default flag and the size and hash of the whole stored document", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dto, err := api.foldBootDocDTO(api.mustBootDocSpec("task_takeover_fresh", "global"))
		if err != nil {
			t.Fatalf("foldBootDocDTO: %v", err)
		}

		got := bootDocReceiptOf(dto)

		apiWantValue(t, "receipt", apiTestJSONOf(t, got), map[string]any{
			"kind":       "task_takeover_fresh",
			"key":        "global",
			"is_default": true,
			"size_chars": 127,
			"cap_chars":  15000,
			"sha256":     "caeacca0168ce3f556c6770133957bf9c380696913f18b707c34ce18fc73772f",
		})
	})

	t.Run("it says nothing about the text, the head, the body or the owner the fold carried", func(t *testing.T) {
		got := bootDocReceiptOf(&bootDocDTO{
			SizeChars: 2, CapChars: 15000, Kind: "offboard", Key: "global",
			Text: "O1", ReadOnlyHead: "a head", Body: "O1", OwnerID: "owner",
			SchemaVersion: 3, IsDefault: false, HasSeed: true, ReadOnly: true,
		})

		apiWantValue(t, "receipt", apiTestJSONOf(t, got), map[string]any{
			"kind":       "offboard",
			"key":        "global",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  15000,
			"sha256":     "5b1f94750d53ab84b5f3fb4bf25b8bf15b162f800f004ae2709b56fdc53a5fb7",
		})
	})
}

func TestBootDocStoredText(t *testing.T) {
	t.Run("a split kind gets the SHIPPED head joined back on, whatever is stored now", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)

		got, err := api.bootDocStoredText(api.mustBootDocSpec("task_closeout", "global"), "C2")

		if err != nil {
			t.Fatalf("bootDocStoredText: %v", err)
		}
		if want := apiTestTaskCloseoutHead + "\n\n" + apiTestBootDocMarker + "\n\nC2"; got != want {
			t.Fatalf("stored text = %q, want %q", got, want)
		}
	})

	t.Run("a kind with no read-only half stores the body byte for byte", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got, err := api.bootDocStoredText(api.mustBootDocSpec("offboard", "global"), "O1\n\nO2")

		if err != nil {
			t.Fatalf("bootDocStoredText: %v", err)
		}
		if got != "O1\n\nO2" {
			t.Fatalf("stored text = %q, want %q", got, "O1\n\nO2")
		}
	})

	t.Run("a split kind whose seed carries no marker errors rather than storing the body alone", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for name, spec := range map[string]bootDocSpec{
			"a seed that is not shipped at all": {Kind: "k", Key: "g", Split: true, SeedFile: "no_such_seed.md"},
			"a shipped seed with no marker":     {Kind: "k", Key: "g", Split: true, SeedFile: "offboard.md"},
		} {
			got, err := api.bootDocStoredText(spec, "X")
			if got != "" {
				t.Fatalf("%s: stored text = %q, want the empty string", name, got)
			}
			want := "boot document k/g is declared split but its seed carries no " +
				apiTestBootDocMarker + " line"
			if err == nil || err.Error() != want {
				t.Fatalf("%s: err = %v, want %q", name, err, want)
			}
		}
	})
}

func TestBootDocBodyOf(t *testing.T) {
	t.Run("a split kind answers the half under the marker, and everything else answers the whole text", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		split := api.mustBootDocSpec("task_takeover_fresh", "global")
		unsplit := api.mustBootDocSpec("offboard", "global")

		for name, c := range map[string]struct {
			spec bootDocSpec
			text string
			want string
		}{
			"a split kind over a document carrying the marker": {
				split, apiTestTakeoverFreshSeed, apiTestTakeoverFreshBody},
			"a split kind over a row stored before the marker existed": {
				split, "written before the marker existed", "written before the marker existed"},
			"a split kind over an empty document": {split, "", ""},
			"a kind with no read-only half, over a document that carries the marker anyway": {
				unsplit, apiTestTakeoverFreshSeed, apiTestTakeoverFreshSeed},
			"a kind with no read-only half, over its own document": {unsplit, "O1", "O1"},
		} {
			if got := bootDocBodyOf(c.spec, c.text); got != c.want {
				t.Fatalf("%s: got %q, want %q", name, got, c.want)
			}
		}
	})
}

func TestBootDocStoredTextPreservesCallerBraces(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	for name, c := range map[string]struct {
		spec bootDocSpec
		body string
		want string
	}{
		"unsplit body is stored literally": {
			spec: api.mustBootDocSpec("offboard", "global"),
			body: "you are at {where}",
			want: "you are at {where}",
		},
		"split body is stored literally under the shipped head": {
			spec: api.mustBootDocSpec("task_closeout", "global"),
			body: "closing {task_no} for {closed_by}",
		},
	} {
		t.Run(name, func(t *testing.T) {
			want := c.want
			if want == "" {
				seed, hasSeed, err := api.root.seedBlockMD(c.spec.SeedFile)
				if err != nil || !hasSeed {
					t.Fatalf("seed %q: hasSeed=%v err=%v", c.spec.SeedFile, hasSeed, err)
				}
				head, _, split := DocSplitHeadBody(seed)
				if !split {
					t.Fatal("task close-out seed lost its marker")
				}
				want = DocJoinHeadBody(head, c.body)
			}
			got, err := api.bootDocStoredText(c.spec, c.body)
			if err != nil {
				t.Fatalf("bootDocStoredText: %v", err)
			}
			if got != want {
				t.Fatalf("stored text = %q, want %q", got, want)
			}
		})
	}
}

func TestResetBootDoc(t *testing.T) {
	t.Run("a document with no shipped default answers 404 and writes nothing", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
		dashboard := apiTestListen(t, api, "")
		rec := httptest.NewRecorder()

		api.resetBootDoc(rec, req, bootDocSpec{Kind: "offboard", Key: "global", SeedFile: "no_such_seed.md"})

		if rec.Code != 404 {
			t.Fatalf("want 404, got %d (%s)", rec.Code, rec.Body.String())
		}
		apiWantError(t, apiTestDecodeJSONBody(t, rec), "not_found",
			"document 'offboard/global' has no shipped default to reset to")
		stored, err := d.GetBootDocument("offboard", "global")
		if err != nil || stored != nil {
			t.Fatalf("an overlay row was written: %#v (%v)", stored, err)
		}
		dashboard.wantFrames()
	})

	t.Run("a read-only document is refused rather than answered with a no-op success", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_takeover_fresh/global", owner, `{"body":"F1"}`)
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
		spec := api.mustBootDocSpec("task_takeover_fresh", "global")
		spec.ReadOnly = true
		dashboard := apiTestListen(t, api, "")
		rec := httptest.NewRecorder()

		api.resetBootDoc(rec, req, spec)

		if rec.Code != 405 {
			t.Fatalf("want 405, got %d (%s)", rec.Code, rec.Body.String())
		}
		apiWantError(t, apiTestDecodeJSONBody(t, rec), "method_not_allowed",
			"the new task document is a read-only document — it is shown so you can see what "+
				"agents are told, but no caller may edit it and there is no version of it other "+
				"than the shipped one; nothing was written")
		_, after := apiJSON(t, h, "GET", "/api/boot-docs/task_takeover_fresh/global", owner, "")
		apiWantValue(t, "body", after["body"], "F1")
		dashboard.wantFrames()
	})

	t.Run("the reset falls the folded read back to the shipped seed and retains the edit it replaced", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_takeover_fresh/global", owner, `{"body":"F1"}`)
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
		dashboard := apiTestListen(t, api, "")
		rec := httptest.NewRecorder()

		api.resetBootDoc(rec, req, api.mustBootDocSpec("task_takeover_fresh", "global"))

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		apiWantBody(t, apiTestDecodeJSONBody(t, rec), map[string]any{
			"kind":       "task_takeover_fresh",
			"key":        "global",
			"is_default": true,
			"size_chars": 127,
			"cap_chars":  15000,
			"sha256":     "caeacca0168ce3f556c6770133957bf9c380696913f18b707c34ce18fc73772f",
		})
		_, after := apiJSON(t, h, "GET", "/api/boot-docs/task_takeover_fresh/global", owner, "")
		apiWantValue(t, "text", after["text"], apiTestTakeoverFreshSeed)
		apiWantValue(t, "history", apiTestBootDocHistory(t, d, "task_takeover_fresh", "global"), []any{
			map[string]any{
				"actor":   "owner",
				"content": `{"text":"` + apiTestTakeoverFreshHead + `\n\n\u003c!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） --\u003e\n\nF1","tombstoned":"false"}`,
			},
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})
}

func TestHandleGetSystemInteractionApiSystemInteractionGet(t *testing.T) {
	t.Run("an edited document answers the overlay, its size and the flags that say it is an edit", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/system-interaction", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     2,
			"cap_chars":      60000,
			"kind":           "system_interaction",
			"key":            "global",
			"text":           "S1",
			"read_only_head": "",
			"body":           "S1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
		dashboard.wantFrames()
	})

	t.Run("an ordinary agent identity may read it, because this row sits at the machine floor", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "GET", "/api/system-interaction", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     2,
			"cap_chars":      60000,
			"kind":           "system_interaction",
			"key":            "global",
			"text":           "S1",
			"read_only_head": "",
			"body":           "S1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("the unedited read serves the shipped system-interaction document and its metadata", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/system-interaction", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "size_chars", data["size_chars"], 16906)
		apiWantValue(t, "cap_chars", data["cap_chars"], 60000)
		apiWantValue(t, "kind", data["kind"], "system_interaction")
		apiWantValue(t, "key", data["key"], "global")
		apiWantValue(t, "text", data["text"], apiAnyString)
		apiWantValue(t, "body", data["body"], apiAnyString)
		apiWantValue(t, "is_default", data["is_default"], true)
		apiWantValue(t, "has_seed", data["has_seed"], true)
		apiWantValue(t, "schema_version", data["schema_version"], 3)
		text, ok := data["text"].(string)
		if !ok || utf8.RuneCountInString(text) != 16906 {
			t.Fatalf("the shipped system-interaction text has %d runes, want 16906", utf8.RuneCountInString(text))
		}
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/system-interaction", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReplaceSystemInteractionApiSystemInteractionPost(t *testing.T) {
	t.Run("a replace answers the receipt over the stored document and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  60000,
			"sha256":     "3696ad59777e09d5f2daacb544022d435e7cd053d87c5781241fabd8fdf2a90d",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("writing the same body twice changes nothing the second time and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  60000,
			"sha256":     "3696ad59777e09d5f2daacb544022d435e7cd053d87c5781241fabd8fdf2a90d",
		})
		dashboard.wantFrames()
	})

	t.Run("emptying a document that had content answers 400 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":""}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "this would replace the existing system interaction block with an empty one — pass allow_shrink=true if that is intended, or reset it to the shipped default; nothing was written")
		dashboard.wantFrames()
	})

	t.Run("emptying a document with allow_shrink answers the empty receipt and still fans the delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"","allow_shrink":true}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": false,
			"size_chars": 0,
			"cap_chars":  60000,
			"sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a body carrying a key this route does not declare answers 422 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1","note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"note\"")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", "", `{"body":"S1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetSystemInteractionApiSystemInteractionResetPost(t *testing.T) {
	t.Run("resetting an edited document answers the shipped receipt flagged default and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": true,
			"size_chars": 16906,
			"cap_chars":  60000,
			"sha256":     "6a8b2480d6317377c0a5bc61fac248803b1afe12e9ee1a1ccb0235941635bb10",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("resetting a document nobody has edited changes nothing and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": true,
			"size_chars": 16906,
			"cap_chars":  60000,
			"sha256":     "6a8b2480d6317377c0a5bc61fac248803b1afe12e9ee1a1ccb0235941635bb10",
		})
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/system-interaction/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetOffboardApiOffboardGet(t *testing.T) {
	t.Run("a document nobody has edited answers the shipped 〈停止〉 text flagged default", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/offboard", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     1805,
			"cap_chars":      15000,
			"kind":           "offboard",
			"key":            "global",
			"text":           apiTestOffboardNotice,
			"read_only_head": "",
			"body":           apiTestOffboardNotice,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"has_seed":       true,
			"read_only":      false,
		})
		dashboard.wantFrames()
	})

	t.Run("an edited document answers the overlay in place of the seed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)

		status, data := apiJSON(t, h, "GET", "/api/offboard", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     2,
			"cap_chars":      15000,
			"kind":           "offboard",
			"key":            "global",
			"text":           "O1",
			"read_only_head": "",
			"body":           "O1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("an ordinary agent identity may read it, because this row sits at the machine floor", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "GET", "/api/offboard", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     2,
			"cap_chars":      15000,
			"kind":           "offboard",
			"key":            "global",
			"text":           "O1",
			"read_only_head": "",
			"body":           "O1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/offboard", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReplaceOffboardApiOffboardPost(t *testing.T) {
	t.Run("a replace answers the receipt over the stored document and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "offboard",
			"key":        "global",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  15000,
			"sha256":     "5b1f94750d53ab84b5f3fb4bf25b8bf15b162f800f004ae2709b56fdc53a5fb7",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/offboard", "", `{"body":"O1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetOffboardApiOffboardResetPost(t *testing.T) {
	t.Run("resetting an edited document answers the shipped receipt flagged default and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/offboard/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "offboard",
			"key":        "global",
			"is_default": true,
			"size_chars": 1805,
			"cap_chars":  15000,
			"sha256":     "49f67a2aa2e8755f4652fc648ae333a0d31024d0cba232ae553a220387ddb3ae",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/offboard/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetBootSequenceApiBootSequenceRuntimeKeyGet(t *testing.T) {
	t.Run("an edited runtime answers its own overlay and the other runtime keeps its own", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"B1"}`)
		apiJSON(t, h, "POST", "/api/boot-sequence/codex", owner, `{"body":"B2"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/boot-sequence/claude", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     2,
			"cap_chars":      15000,
			"kind":           "boot_sequence",
			"key":            "claude",
			"text":           "B1",
			"read_only_head": "",
			"body":           "B1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})

		status, data = apiJSON(t, h, "GET", "/api/boot-sequence/codex", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     2,
			"cap_chars":      15000,
			"kind":           "boot_sequence",
			"key":            "codex",
			"text":           "B2",
			"read_only_head": "",
			"body":           "B2",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
		dashboard.wantFrames()
	})

	t.Run("an ordinary agent identity may read it, because this row sits at the machine floor", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"B1"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "GET", "/api/boot-sequence/claude", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     2,
			"cap_chars":      15000,
			"kind":           "boot_sequence",
			"key":            "claude",
			"text":           "B1",
			"read_only_head": "",
			"body":           "B1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("a runtime with no boot sequence answers 404 naming the runtimes that have one", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/boot-sequence/Codex", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found",
			"no boot sequence for runtime 'Codex' — the runtimes with their own boot sequence are 'claude' and 'codex'")
	})

	t.Run("the unedited read serves the shipped claude sequence and its metadata", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/boot-sequence/claude", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "size_chars", data["size_chars"], 2965)
		apiWantValue(t, "cap_chars", data["cap_chars"], 15000)
		apiWantValue(t, "kind", data["kind"], "boot_sequence")
		apiWantValue(t, "key", data["key"], "claude")
		apiWantValue(t, "text", data["text"], apiAnyString)
		apiWantValue(t, "body", data["body"], apiAnyString)
		apiWantValue(t, "is_default", data["is_default"], true)
		apiWantValue(t, "has_seed", data["has_seed"], true)
		apiWantValue(t, "schema_version", data["schema_version"], 3)
		text, ok := data["text"].(string)
		if !ok || utf8.RuneCountInString(text) != 2965 {
			t.Fatalf("the shipped claude sequence has %d runes, want 2965", utf8.RuneCountInString(text))
		}
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/boot-sequence/claude", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost(t *testing.T) {
	t.Run("a replace of one runtime's steps answers the receipt and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"B1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "boot_sequence",
			"key":        "claude",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  15000,
			"sha256":     "5b950e77941d01cdf246d00b1ece546bc95234b77d98b44c9187e2733afa696a",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a runtime with no boot sequence answers 404 naming the runtimes that have one and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/Codex", owner, `{"body":"B1"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "no boot sequence for runtime 'Codex' — the runtimes with their own boot sequence are 'claude' and 'codex'")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude", "", `{"body":"B1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost(t *testing.T) {
	t.Run("resetting one runtime's edited steps answers the shipped receipt flagged default and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"B1"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "boot_sequence",
			"key":        "claude",
			"is_default": true,
			"size_chars": 2965,
			"cap_chars":  15000,
			"sha256":     "a07cefcbbf849bed70edc1cfa8c407e77eadc7f75227a2b936f1061483db3d36",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a runtime with no boot sequence answers 404 naming the runtimes that have one and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/Codex/reset", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "no boot sequence for runtime 'Codex' — the runtimes with their own boot sequence are 'claude' and 'codex'")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestWriteUnknownBootSequence(t *testing.T) {
	t.Run("the refusal names both runtimes that have a boot sequence, so a typo is not read as a server with none", func(t *testing.T) {
		for _, runtimeKey := range []string{"opus", "Codex", ""} {
			rec := httptest.NewRecorder()

			writeUnknownBootSequence(rec, runtimeKey)

			if rec.Code != 404 {
				t.Fatalf("%q: want 404, got %d (%s)", runtimeKey, rec.Code, rec.Body.String())
			}
			apiWantError(t, apiTestDecodeJSONBody(t, rec), "not_found",
				"no boot sequence for runtime '"+runtimeKey+"' — the runtimes with their own "+
					"boot sequence are 'claude' and 'codex'")
		}
	})
}

func TestBootDocReadOnlyRefusal(t *testing.T) {
	t.Run("the refusal says what the document is rather than which permission the caller lacks", func(t *testing.T) {
		for _, docName := range []string{"new task document", "dependency-released notice"} {
			got := bootDocReadOnlyRefusal(bootDocSpec{Kind: "k", Key: "g", DocName: docName})

			want := "the " + docName + " is a read-only document — it is shown so you can see " +
				"what agents are told, but no caller may edit it and there is no version of it " +
				"other than the shipped one; nothing was written"
			if got != want {
				t.Fatalf("refusal = %q, want %q", got, want)
			}
		}
	})
}

func TestGenericBootDocSpec(t *testing.T) {
	t.Run("a pair this server serves resolves to its whole spec and writes nothing to the response", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()

		got, ok := api.genericBootDocSpec(rec, "offboard", "global")

		if !ok {
			t.Fatalf("genericBootDocSpec refused a served pair: %s", rec.Body.String())
		}
		want := bootDocSpec{
			Kind: "offboard", Key: "global", SeedFile: "offboard.md", Cap: 15000,
			DocName: "Stop document", Vars: []string{}, Split: false, Join: "", ReadOnly: false,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("spec = %#v, want %#v", got, want)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("the resolver wrote to the response: %s", rec.Body.String())
		}
	})

	t.Run("a kind nobody registered answers 404 saying so, and a bad key answers 404 naming the keys that kind serves", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for name, c := range map[string]struct{ kind, key, want string }{
			"an unregistered kind": {"bogus", "global",
				"document history kind 'bogus' names no editable document on this server"},
			"a key the kind refuses": {"task_closeout", "claude",
				"document history key 'claude' does not name a task_closeout document — the key is 'global'"},
			"a boot sequence under a wrong case": {"boot_sequence", "Codex",
				"document history key 'Codex' does not name a boot_sequence document — the key is 'claude' or 'codex'"},
		} {
			rec := httptest.NewRecorder()

			got, ok := api.genericBootDocSpec(rec, c.kind, c.key)

			if ok {
				t.Fatalf("%s: genericBootDocSpec resolved %#v", name, got)
			}
			if !reflect.DeepEqual(got, bootDocSpec{}) {
				t.Fatalf("%s: spec = %#v, want the zero spec", name, got)
			}
			if rec.Code != 404 {
				t.Fatalf("%s: want 404, got %d (%s)", name, rec.Code, rec.Body.String())
			}
			apiWantError(t, apiTestDecodeJSONBody(t, rec), "not_found", c.want)
		}
	})
}

func TestHandleGetBootDocApiBootDocsKindKeyGet(t *testing.T) {
	t.Run("a document nobody has edited answers the shipped text split into the half a write takes and the half it cannot", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     105,
			"cap_chars":      15000,
			"kind":           "task_closeout",
			"key":            "global",
			"text":           apiTestTaskCloseoutSeed,
			"read_only_head": apiTestTaskCloseoutHead,
			"body":           apiTestTaskCloseoutBody,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"has_seed":       true,
			"read_only":      false,
		})
		dashboard.wantFrames()
	})

	t.Run("an edited document answers the overlay still joined under the shipped head", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)

		status, data := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     77,
			"cap_chars":      15000,
			"kind":           "task_closeout",
			"key":            "global",
			"text":           apiTestTaskCloseoutHead + "\n\n<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->\n\nC1",
			"read_only_head": apiTestTaskCloseoutHead,
			"body":           "C1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("an ordinary agent identity may read it, because this row sits at the machine floor", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     105,
			"cap_chars":      15000,
			"kind":           "task_closeout",
			"key":            "global",
			"text":           apiTestTaskCloseoutSeed,
			"read_only_head": apiTestTaskCloseoutHead,
			"body":           apiTestTaskCloseoutBody,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"has_seed":       true,
			"read_only":      false,
		})
	})

	t.Run("a kind this server does not serve answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/boot-docs/bogus/global", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "document history kind 'bogus' names no editable document on this server")
	})

	t.Run("a key this kind does not serve answers 404 naming the keys it does", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/bogus", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found",
			"document history key 'bogus' does not name a task_closeout document — the key is 'global'")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/boot-docs/task_closeout/global", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReplaceBootDocApiBootDocsKindKeyPost(t *testing.T) {
	t.Run("a replace addressed by kind and key answers the receipt and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "task_closeout",
			"key":        "global",
			"is_default": false,
			"size_chars": 77,
			"cap_chars":  15000,
			"sha256":     "700418ca3be147deaee00b83e27c7c66b9821283dba4b4d21b2f108129f514dd",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a kind this server does not serve answers 404 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/bogus/global", owner, `{"body":"C1"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "document history kind 'bogus' names no editable document on this server")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", "", `{"body":"C1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetBootDocApiBootDocsKindKeyResetPost(t *testing.T) {
	t.Run("resetting an edited document addressed by kind and key answers the shipped receipt and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "task_closeout",
			"key":        "global",
			"is_default": true,
			"size_chars": 105,
			"cap_chars":  15000,
			"sha256":     "1a7b91966130612348b1b472b4956266ffc156f3c0cb04744c4b2086edb392f8",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}
