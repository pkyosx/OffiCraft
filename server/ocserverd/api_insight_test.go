package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFoldInsightDTO(t *testing.T) {
	const assistantInsightSeed = `# Intake Point of Contact

When you receive a request, first clarify how it will be handled, how the task will be arranged, and the task type. When an Owner ruling is needed, open a card to confirm the following with the Owner, and bring all of it on the same card at once:

1. **Handling approach and executor**

   **The Assistant stays the single point of contact**
   If the Assistant is chosen to stay the single point of contact, the Assistant acts as the sole contact: it receives and organizes the request, hands it to the executor, tracks progress, and relays the results back. The Assistant tells the executor that the point of contact for this matter is the Assistant, not the Owner (the person in charge); the executor reports progress and problems to the Assistant, and when an Owner ruling is needed the Assistant organizes it and opens a card to confirm — the executor does not communicate with the Owner directly; the requester keeps dealing with the Assistant.

   **Hand off directly to the executor**
   If a direct handoff is chosen, the point of contact clearly explains the scope of the request and the next step to the designated executor, then leaves the handling chain; from then on the executor and the requester deal with each other directly and handle it.

2. **Creating the task and the task type**
   If a task is to be created, the card must provide at least:
   - **Problem and expected outcome:** the problem to solve, its impact and likelihood, and the expected situation once it is solved.
   - **Planned solution and scope:** how it is expected to be solved, the scope of work this time, and any problems that may remain or new risks that may arise.
   - **Task type:** the candidate task types that fit and their scope of application, for the requester to choose from; if no type fits, state the gap and leave it to the Owner to confirm — do not apply a similar type on your own.
   - **Task priority:** a suggested priority based on the problem's impact, likelihood, and other scheduling factors.
   - **Execution arrangement:** expected executor, execution machine, runtime / model, effort.

Mark the items awaiting an Owner ruling explicitly and leave them to the Owner to confirm.

When remote access needs to be opened (the user wants to reach the console from somewhere other than this computer), create a task with the built-in task manual "Open remote access" (` + "`builtin-remote-access`" + `) and execute it following the manual's steps.

# Operations Guide Point of Contact

## Finding and answering usage questions

- First find the source that matches the question: for features, rules, and settings, check the console's user guide; for MCP operations, read the current tool's description and schema; for CLI commands, start with ` + "`ocagent <subcommand> --help`" + `; for task workflows, check the task manual.
- Answer only after reading, and check against the system's actual current data when needed; state clearly the conditions that apply, the path to perform the operation, and whether it is currently in effect.
- When you cannot find documentation, or the documentation does not match reality, say plainly what is uncertain and what differs; do not make up rules, fields, or permissions.

## Performing restricted operations on someone's behalf

- First confirm the operation's goal, target, scope, reason, and completion condition.
- The Assistant's permissions are broader than a regular member's; OffiCraft operations the Owner delegates are also within the scope of what it performs on others' behalf. When the request is clear, the responsibility is clear, and it is within permissions, perform it on their behalf and report the actual result.
- When an Owner decision, approval, or authorization is needed, organize the necessary information and open a card for the Owner to rule on — do not make the decision in the Owner's place; when the content is unclear or beyond your permissions, first fill in the information or confirm.
`

	t.Run("an unedited seeded role returns its factory text as the default DTO", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, err := api.foldInsightDTO("assistant")
		if err != nil {
			t.Fatalf("foldInsightDTO(seed): %v", err)
		}
		if got == nil {
			t.Fatal("foldInsightDTO(seed) = nil")
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, got), map[string]any{
			"size_chars":     3978,
			"cap_chars":      15000,
			"role_key":       "assistant",
			"text":           assistantInsightSeed,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"has_seed":       true,
		})
	})

	t.Run("a live overlay replaces factory text while the factory seed remains available", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutInsight(Insight{RoleKey: "assistant", Text: "mine"}); err != nil {
			t.Fatalf("PutInsight: %v", err)
		}

		got, err := api.foldInsightDTO("assistant")
		if err != nil {
			t.Fatalf("foldInsightDTO(overlay): %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, got), map[string]any{
			"size_chars":     4,
			"cap_chars":      15000,
			"role_key":       "assistant",
			"text":           "mine",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       true,
		})
	})

	t.Run("a tombstoned overlay restores the factory text and default flag", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutInsight(Insight{RoleKey: "assistant", Text: "old", Tombstoned: true}); err != nil {
			t.Fatalf("PutInsight: %v", err)
		}

		got, err := api.foldInsightDTO("assistant")
		if err != nil {
			t.Fatalf("foldInsightDTO(tombstoned): %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, got), map[string]any{
			"size_chars":     3978,
			"cap_chars":      15000,
			"role_key":       "assistant",
			"text":           assistantInsightSeed,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"has_seed":       true,
		})
	})

	t.Run("a role without a factory seed returns an empty default DTO", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, err := api.foldInsightDTO("engineer")
		if err != nil {
			t.Fatalf("foldInsightDTO(no seed): %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, got), map[string]any{
			"size_chars":     0,
			"cap_chars":      15000,
			"role_key":       "engineer",
			"text":           "",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"has_seed":       false,
		})
	})
}

func TestInsightWriteAuthz(t *testing.T) {
	t.Run("the owner and admin agent may write any role's insight", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		for _, tc := range []struct {
			name  string
			token string
		}{
			{name: "owner", token: owner},
			{name: "admin agent", token: apiTestAgentToken(t, api, "mira", "")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				var allowed bool
				taskTestUnderCaller(t, api, d, tc.token, func(r *http.Request) {
					allowed = api.insightWriteAuthz(rec, r, "engineer")
				})
				if !allowed {
					t.Fatal("insightWriteAuthz refused a privileged caller")
				}
				if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
					t.Fatalf("privileged authorization wrote an unexpected response: %d %q", rec.Code, rec.Body.String())
				}
			})
		}
	})

	t.Run("a plain agent may write only its own role's insight", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()
		var allowed bool
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			allowed = api.insightWriteAuthz(rec, r, "engineer")
		})
		if !allowed {
			t.Fatal("insightWriteAuthz refused the caller's own role")
		}
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Fatalf("allowed authorization wrote an unexpected response: %d %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("a plain agent writing another role's insight answers 403", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()
		var allowed bool
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			allowed = api.insightWriteAuthz(rec, r, "assistant")
		})
		if allowed {
			t.Fatal("insightWriteAuthz allowed a caller to write another role")
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
		}
		apiWantError(t, apiTestDecodeJSONBody(t, rec), "forbidden",
			"an agent may only write its own role's insight")
	})

	t.Run("a roleless outsource caller cannot write any role's insight", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.putMemberWholeRowForTest(Member{
			ID: "ow-roleless", Name: "Roleless worker", Kind: KindOutsource,
			RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		rec := httptest.NewRecorder()
		var allowed bool
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "ow-roleless", ""), func(r *http.Request) {
			allowed = api.insightWriteAuthz(rec, r, "engineer")
		})
		if allowed {
			t.Fatal("insightWriteAuthz allowed a roleless caller")
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
		}
		apiWantError(t, apiTestDecodeJSONBody(t, rec), "forbidden",
			"an agent may only write its own role's insight")
	})
}

func TestInsightHistorySnapshot(t *testing.T) {
	for name, tc := range map[string]struct {
		current *Insight
		want    string
	}{
		"no row":               {current: nil, want: `{}`},
		"a live empty row":     {current: &Insight{RoleKey: "engineer"}, want: `{"text":"","tombstoned":"false"}`},
		"a tombstoned row":     {current: &Insight{RoleKey: "engineer", Text: "保留版本", Tombstoned: true}, want: `{}`},
		"a live row with text": {current: &Insight{RoleKey: "engineer", Text: "如何權衡"}, want: `{"text":"如何權衡","tombstoned":"false"}`},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := insightHistorySnapshot(tc.current)
			if err != nil {
				t.Fatalf("insightHistorySnapshot: %v", err)
			}
			if got != tc.want {
				t.Fatalf("snapshot = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInsightSnapshotIn(t *testing.T) {
	t.Run("the transaction reader returns the addressed insight document", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		if err := d.PutInsight(Insight{
			RoleKey: "engineer", Text: "交易前洞見",
		}); err != nil {
			t.Fatalf("PutInsight: %v", err)
		}

		got, err := insightSnapshotIn("engineer")(d.rdb)
		if err != nil {
			t.Fatalf("insightSnapshotIn: %v", err)
		}
		if got != `{"text":"交易前洞見","tombstoned":"false"}` {
			t.Fatalf("snapshot = %q, want %q", got, `{"text":"交易前洞見","tombstoned":"false"}`)
		}
	})

	t.Run("the transaction reader represents a reset insight document as the empty object", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		if err := d.PutInsight(Insight{
			RoleKey: "engineer", Text: "交易前洞見", Tombstoned: true,
		}); err != nil {
			t.Fatalf("PutInsight: %v", err)
		}
		got, err := insightSnapshotIn("engineer")(d.rdb)
		if err != nil {
			t.Fatalf("insightSnapshotIn: %v", err)
		}
		if got != `{}` {
			t.Fatalf("snapshot = %q, want %q", got, `{}`)
		}
	})

	t.Run("the transaction reader represents an absent insight document as the empty object", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		got, err := insightSnapshotIn("r-missing")(d.rdb)
		if err != nil {
			t.Fatalf("insightSnapshotIn: %v", err)
		}
		if got != `{}` {
			t.Fatalf("snapshot = %q, want %q", got, `{}`)
		}
	})
}

func TestHandleGetInsightApiInsightRoleKeyGet(t *testing.T) {
	t.Run("a role with no seed that nobody has written for answers the empty doc flagged default", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/insight/engineer", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":       "engineer",
			"text":           "",
			"size_chars":     0,
			"cap_chars":      15000,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"has_seed":       false,
		})
		dashboard.wantFrames()
	})

	t.Run("after a write the doc reads back the stored text and is no longer default", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"weigh latency over purity"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/insight/engineer", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":       "engineer",
			"text":           "weigh latency over purity",
			"size_chars":     25,
			"cap_chars":      15000,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"has_seed":       false,
		})
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/insight/engineer", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetInsightApiInsightRoleKeyResetPost(t *testing.T) {
	t.Run("resetting an edited seeded role answers the factory receipt flagged default and fans the owner-only delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/assistant", owner, `{"text":"mine"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/insight/assistant/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":   "assistant",
			"is_default": true,
			"has_seed":   true,
			"size_chars": 3978,
			"cap_chars":  15000,
			"sha256":     "cebe7c08fbba259bce207ecb3dc96c909426722ed77237947d7f3039c19ebbaf",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "insight",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "insight",
				"key":     "owner::assistant",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("resetting a seeded role nobody has edited answers the same factory receipt", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/assistant/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":   "assistant",
			"is_default": true,
			"has_seed":   true,
			"size_chars": 3978,
			"cap_chars":  15000,
			"sha256":     "cebe7c08fbba259bce207ecb3dc96c909426722ed77237947d7f3039c19ebbaf",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "insight",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "insight",
				"key":     "owner::assistant",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a role that does not exist answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/reset", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "role 'engineer' not found")
		dashboard.wantFrames()
	})

	t.Run("a role that ships no factory insight answers 409 not applicable and keeps its insight", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		if status, data := apiJSON(t, h, "POST", "/api/insight/r-design", owner, `{"text":"weigh latency"}`); status != 200 {
			t.Fatalf("write insight: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/r-design/reset", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "reset is not applicable to the insight of role 'r-design': "+
			"it has no shipped version — only roles that ship an insight can be reset")
		dashboard.wantFrames()
		overlay, err := d.GetInsight("r-design")
		if err != nil {
			t.Fatalf("GetInsight: %v", err)
		}
		if overlay == nil || *overlay != (Insight{RoleKey: "r-design", Text: "weigh latency"}) {
			t.Fatalf("insight after refused reset = %#v", overlay)
		}
	})

	t.Run("an agent resetting another role's insight answers 403 and fans nothing", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/assistant/reset", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "an agent may only write its own role's insight")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/insight/assistant/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestInsightReceiptOf(t *testing.T) {
	t.Run("the reset receipt keeps the folded role, flags, size, cap and hash", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dto, err := api.foldInsightDTO("assistant")
		if err != nil {
			t.Fatalf("foldInsightDTO: %v", err)
		}

		got := insightReceiptOf(dto)
		apiWantValue(t, "receipt", apiTestJSONOf(t, got), map[string]any{
			"role_key":   "assistant",
			"is_default": true,
			"has_seed":   true,
			"size_chars": 3978,
			"cap_chars":  15000,
			"sha256":     "cebe7c08fbba259bce207ecb3dc96c909426722ed77237947d7f3039c19ebbaf",
		})
	})

	t.Run("the receipt omits the folded text and other DTO metadata", func(t *testing.T) {
		got := insightReceiptOf(&insightDTO{
			RoleKey: "engineer", Text: "I1", SizeChars: 2, CapChars: 15000,
			OwnerID: "owner", SchemaVersion: 3, IsDefault: false, HasSeed: true,
		})
		apiWantValue(t, "receipt", apiTestJSONOf(t, got), map[string]any{
			"role_key":   "engineer",
			"is_default": false,
			"has_seed":   true,
			"size_chars": 2,
			"cap_chars":  15000,
			"sha256":     "c2818bc4e5ec4ae4a357a0df6fed73652e169ec676f7d4718cfb6807c5f7d1b0",
		})
	})
}

func TestHandleReplaceInsightApiInsightRoleKeyPost(t *testing.T) {
	t.Run("a whole-doc replace answers the receipt over what was judged and fans the owner-only delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"I1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":   "engineer",
			"is_default": false,
			"has_seed":   false,
			"size_chars": 2,
			"cap_chars":  15000,
			"sha256":     "c2818bc4e5ec4ae4a357a0df6fed73652e169ec676f7d4718cfb6807c5f7d1b0",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "insight",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "insight",
				"key":     "owner::engineer",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("an agent writing its own role's insight is allowed and fans the same delta under its own name", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer", agent, `{"text":"I1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":   "engineer",
			"is_default": false,
			"has_seed":   false,
			"size_chars": 2,
			"cap_chars":  15000,
			"sha256":     "c2818bc4e5ec4ae4a357a0df6fed73652e169ec676f7d4718cfb6807c5f7d1b0",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "insight",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "insight",
				"key":     "owner::engineer",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		})
	})

	t.Run("an agent writing another role's insight answers 403 and writes nothing", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/assistant", agent, `{"text":"I1"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "an agent may only write its own role's insight")
		dashboard.wantFrames()
	})

	t.Run("emptying a doc that had content answers 400 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"I1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":""}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "this would replace the existing insight doc with an empty one — pass allow_shrink=true if that is intended; nothing was written")
		dashboard.wantFrames()
	})

	t.Run("emptying a doc with allow_shrink answers the empty receipt and still fans the delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"I1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"","allow_shrink":true}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":   "engineer",
			"is_default": false,
			"has_seed":   false,
			"size_chars": 0,
			"cap_chars":  15000,
			"sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "insight",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "insight",
				"key":     "owner::engineer",
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

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"I1","note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"note\"")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer", "", `{"text":"I1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandlePatchInsightApiInsightRoleKeyPatchPost(t *testing.T) {
	t.Run("an anchored edit answers the receipt over the resulting doc and fans the owner-only delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"alpha beta"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/patch", owner,
			`{"edits":[{"old":"alpha","new":"gamma"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":       "engineer",
			"applied_edits":  1,
			"size_chars":     10,
			"cap_chars":      15000,
			"sha256":         "1ab01121cca28ffac7e9b0bf95890779867a992a0a9620be18b13cffd14186d2",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "insight",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "insight",
				"key":     "owner::engineer",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("an empty old appends the new text and fans the delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"alpha beta"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/patch", owner,
			`{"edits":[{"old":"","new":" delta"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":       "engineer",
			"applied_edits":  1,
			"size_chars":     17,
			"cap_chars":      15000,
			"sha256":         "6d691094f4bd670b68408444286bbfcf3c14fbf003a4d2df0f0c3f7b093af1f9",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "insight",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "insight",
				"key":     "owner::engineer",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("two edits that undo one another answer applied_edits 2 and fan nothing at all", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"alpha beta"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/patch", owner,
			`{"edits":[{"old":"alpha","new":"gamma"},{"old":"gamma","new":"alpha"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":       "engineer",
			"applied_edits":  2,
			"size_chars":     10,
			"cap_chars":      15000,
			"sha256":         "1a989ea86150171c687b0727f218eedbb94c4665a7da9b0add1bf5de607f2bf1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
		})
		dashboard.wantFrames()
	})

	t.Run("an anchor that matches nothing answers 400 naming the insight read and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/engineer", owner, `{"text":"alpha beta"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/patch", owner,
			`{"edits":[{"old":"omega","new":"gamma"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "edits[0]: old not found in the current doc — re-read (get_insight) and re-anchor; nothing was written")
		dashboard.wantFrames()
	})

	t.Run("an edits list with no entry answers 422 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/patch", owner, `{"edits":[]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "edits requires at least one {old, new} entry")
		dashboard.wantFrames()
	})

	t.Run("an edit carrying neither old nor new answers 422 naming its index and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/patch", owner, `{"edits":[{}]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "edits[0]: neither old nor new was given — an edit needs at least one of them (empty old appends new); nothing was written")
		dashboard.wantFrames()
	})

	t.Run("an agent patching another role's insight answers 403 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/assistant", owner, `{"text":"alpha beta"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/assistant/patch", agent,
			`{"edits":[{"old":"alpha","new":"gamma"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "an agent may only write its own role's insight")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/patch", "",
			`{"edits":[{"old":"alpha","new":"gamma"}]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}
