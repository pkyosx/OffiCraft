package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFoldInsightDTO(t *testing.T) {
	const assistantInsightSeed = `# 接案窗口

接到請求時，先釐清處理方式、任務安排與任務類型。需要 Owner 裁定時，開卡向 Owner 確認以下事項，並在同一張卡一次帶齊：

1. **處理方式與執行者**

   **由特助維持統一窗口**
   若選擇由特助維持統一窗口，特助作為單一對口，負責接收與整理需求、將需求交給執行者、追蹤處理進度並回傳結果。特助向執行者說明，本案的對接窗口是特助，不是 Owner（負責人）；執行者將進度與問題回報特助，需要 Owner 裁定時由特助整理後開卡確認，執行者不直接向 Owner 溝通；請求方持續向特助對接。

   **直接轉交給執行者**
   若選擇直接轉交，窗口向指定的執行者明確交代需求範圍與下一步後退出處理鏈；後續由執行者與請求方直接對接並處理。

2. **建立任務與任務類型**
   若要建立任務，卡上至少提供：
   - **問題與預期結果：** 要解決的問題、影響程度與發生可能性，以及問題解決後預期的情境。
   - **預計解法與範圍：** 預計如何解決、這次要處理的工作範圍，以及可能遺留的問題或新產生的風險。
   - **任務類型：** 符合的候選任務類型及其適用範圍，供請求方選擇；若沒有合適類型，標明差距並交由 Owner 確認，不自行套用相近類型。
   - **任務優先權：** 根據問題的影響程度、發生可能性與其他時程因素，提出建議的處理優先權。
   - **執行安排：** 預期執行者、執行機器、runtime／model、effort。

待 Owner 裁定的項目直接標明，交由 Owner 確認。

# 操作導覽窗口

## 取得並回答使用說明

- 先按問題找來源：功能、規則與設定查控制台說明文件；MCP 操作看當前工具的說明與 schema；CLI 指令先看 ` + "`ocagent <子命令> --help`" + `；任務流程查任務手冊。
- 讀完後再回答，必要時對照目前系統的實際資料；說清楚適用條件、操作路徑與目前有效性。
- 找不到說明，或說明與實況不一致時，明確說出不確定與差異，不自行補出規則、欄位或權限。

## 代為執行受限操作

- 先確認操作目標、對象、範圍、理由與完成條件。
- 特助的權限比一般成員大；Owner 交辦的 OffiCraft 操作也包含在代為執行範圍內。請求明確、責任清楚且在權限內時，代為執行並回報實際結果。
- 需要 Owner 決定、核可或授權時，整理必要資訊後開一張卡交 Owner 裁定，不代替 Owner 做決定；內容不清楚或超出權限範圍時，先補齊資訊或確認。
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
			"size_chars":     1089,
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
			"size_chars":     1089,
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
		if err := d.PutMember(Member{
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
		"a tombstoned row":     {current: &Insight{RoleKey: "engineer", Text: "保留版本", Tombstoned: true}, want: `{"text":"保留版本","tombstoned":"true"}`},
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
			RoleKey: "engineer", Text: "交易前洞見", Tombstoned: true,
		}); err != nil {
			t.Fatalf("PutInsight: %v", err)
		}

		got, err := insightSnapshotIn("engineer")(d.rdb)
		if err != nil {
			t.Fatalf("insightSnapshotIn: %v", err)
		}
		if got != `{"text":"交易前洞見","tombstoned":"true"}` {
			t.Fatalf("snapshot = %q, want %q", got, `{"text":"交易前洞見","tombstoned":"true"}`)
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
			"size_chars": 1089,
			"cap_chars":  15000,
			"sha256":     "bfce6af1fc381233ae8755a4b8c9a7a58aacb417705b0e70b03736e955c7063f",
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
