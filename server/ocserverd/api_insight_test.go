// Skeleton generated from server/ocserverd/api_insight.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestFoldInsightDTO(t *testing.T) {
	t.Skip("TODO: Per-role INSIGHT doc (T-3809) — the third block of the role journal, beside Duty (role_def.definition_md) and Learning (lessons.text).")
}

func TestInsightWriteAuthz(t *testing.T) {
	t.Skip("TODO: insightWriteAuthz enforces the per-role insight WRITE authz shared by EVERY face that writes this document — replace_insight, patch_insight, reset_insight (T-6501), and api_document_history.go's restore of kind \"insight\": a caller at or above principalAdminAgent (owner, and the admin agent) writes ANY role's insight; everyone else writes ONLY its own member's role_key (read from the roster by the verified sub, never a client field).")
}

func TestInsightHistorySnapshot(t *testing.T) {
	t.Skip("TODO: insightHistorySnapshot renders the retained revision of an insight doc.")
}

func TestInsightSnapshotIn(t *testing.T) {
	t.Skip("TODO: insightSnapshotIn is what SaveWithDocumentHistory calls from INSIDE the write transaction.")
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
			"size_chars": 1089,
			"cap_chars":  15000,
			"sha256":     "bfce6af1fc381233ae8755a4b8c9a7a58aacb417705b0e70b03736e955c7063f",
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
			"size_chars": 1089,
			"cap_chars":  15000,
			"sha256":     "bfce6af1fc381233ae8755a4b8c9a7a58aacb417705b0e70b03736e955c7063f",
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

	t.Run("a role that ships no factory insight answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/insight/engineer/reset", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "role 'engineer' has no factory insight to reset to")
		dashboard.wantFrames()
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
	t.Skip("TODO: insightReceiptOf reduces the read face's fold to the write face's receipt (T-91), for the verb that answers FROM A RE-READ (reset).")
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
