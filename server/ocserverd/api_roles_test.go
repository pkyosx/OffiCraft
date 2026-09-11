package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const apiTestAssistantSeedDefinitionMD = "# 助理\n\nOwner 的助理，工作室的預設對口。\n\n- **不知道該找誰**：先找我，我會判斷並安排後續。\n- **OffiCraft 怎麼運作**：怎麼使用、規則是什麼、某個操作在哪裡，都可以問我。\n- **你做不到的操作**：我的權限比一般成員大，權限之內的我可以代你執行；只有 Owner 能決定的，我整理好開一張卡送到他面前。\n"

const apiTestCustomRoleTemplateMD = "# 角色定義\n\n## 你是誰\n\n（待填：這個角色的身分與定位——用一兩句話說明「你是誰」、在辦公室裡站什麼位置、面對 owner 與其他成員時以什麼視角說話。）\n\n## 你做什麼\n\n（待填：這個角色的職責與工作方式——負責哪些事、怎麼做事、輸出長什麼樣、與 owner 及其他成員怎麼協作、什麼事不歸你管。）\n"

func TestHistoryJSON(t *testing.T) {
	t.Run("a supported value is encoded as its JSON object", func(t *testing.T) {
		type historyEntry struct {
			Text  string `json:"text"`
			Count int    `json:"count"`
		}

		got, err := historyJSON(historyEntry{Text: "note", Count: 2})
		if err != nil {
			t.Fatalf("historyJSON: %v", err)
		}
		if got != `{"text":"note","count":2}` {
			t.Fatalf("historyJSON = %q, want %q", got, `{"text":"note","count":2}`)
		}
	})

	t.Run("an unsupported value answers an empty string and its marshal error", func(t *testing.T) {
		got, err := historyJSON(func() {})
		if got != "" {
			t.Fatalf("historyJSON unsupported value = %q, want empty string", got)
		}
		if err == nil || err.Error() != "json: unsupported type: func()" {
			t.Fatalf("historyJSON unsupported error = %v, want json: unsupported type: func()", err)
		}
	})
}

func TestHandleGetGlobalContextApiGlobalContextGet(t *testing.T) {
	t.Run("a station where nobody has written the block answers the empty block flagged as default", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/global-context", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"text":           "",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"org_name":       "",
		})
		dashboard.wantFrames()
	})

	t.Run("after a replace the block reads back the stored text and is no longer default", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"house style: ship small"}`)

		status, data := apiJSON(t, h, "GET", "/api/global-context", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"text":           "house style: ship small",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"org_name":       "",
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/global-context", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReplaceGlobalContextApiGlobalContextPost(t *testing.T) {
	t.Run("a replace answers the receipt over the stored block and fans the owner-only delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"hello"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"is_default": false,
			"size_chars": 5,
			"sha256":     "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
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

	t.Run("emptying a block that had content answers 400 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"hello"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":""}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"this would replace the existing global context with an empty one — pass allow_shrink=true if that is intended, or use reset_global_context; nothing was written")
		dashboard.wantFrames()
	})

	t.Run("emptying a block with allow_shrink answers the empty receipt and still fans the delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"hello"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"","allow_shrink":true}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"is_default": false,
			"size_chars": 0,
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

		status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"x","bogus":1}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `invalid request body: json: unknown field "bogus"`)
		dashboard.wantFrames()
	})

	t.Run("a body that is not JSON answers 422 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/global-context", agent, `{"text":"hello"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/global-context", "", `{"text":"hello"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})
}

func TestGlobalContextReceiptOf(t *testing.T) {
	t.Run("a receipt keeps the default flag and reports rune size and hash of the stored text", func(t *testing.T) {
		got := globalContextReceiptOf(&globalContextDTO{
			Text:          "é😊",
			OwnerID:       "owner",
			SchemaVersion: 3,
			IsDefault:     true,
			OrgName:       "Studio",
		})
		want := globalContextReceiptDTO{
			IsDefault: true,
			SizeChars: 2,
			Sha256:    "a94589e1b89859b57430d91195095e623e7aefe07d4426d4788ae716a24dd36a",
		}
		if got != want {
			t.Fatalf("globalContextReceiptOf = %#v, want %#v", got, want)
		}
	})

	t.Run("a stored empty replacement keeps the non-default state and the empty-text hash", func(t *testing.T) {
		got := globalContextReceiptOf(&globalContextDTO{IsDefault: false})
		want := globalContextReceiptDTO{
			IsDefault: false,
			SizeChars: 0,
			Sha256:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		}
		if got != want {
			t.Fatalf("globalContextReceiptOf empty = %#v, want %#v", got, want)
		}
	})
}

func TestHandleResetGlobalContextApiGlobalContextResetPost(t *testing.T) {
	t.Run("a reset answers the empty receipt flagged default and fans the owner-only delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"hello"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/global-context/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"is_default": true,
			"size_chars": 0,
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
		bystander.wantFrames()
	})

	t.Run("resetting a block that was never written answers the same default receipt", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/global-context/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"is_default": true,
			"size_chars": 0,
			"sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/global-context/reset", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/global-context/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})
}

func TestListRoleKeys(t *testing.T) {
	t.Run("the seed role leads active custom roles while seed overlays and tombstones stay off the roster", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		for _, role := range []RoleDef{
			{RoleKey: "assistant", Name: "Changed", DefinitionMD: "# Changed"},
			{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"},
			{RoleKey: "r-gone", Name: "Gone", Tombstoned: true},
		} {
			if err := d.PutRoleDef(role); err != nil {
				t.Fatalf("PutRoleDef(%q): %v", role.RoleKey, err)
			}
		}

		got, err := api.listRoleKeys()
		if err != nil {
			t.Fatalf("listRoleKeys: %v", err)
		}
		want := []string{"assistant", "r-design"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("listRoleKeys = %#v, want %#v", got, want)
		}
	})
}

func TestHandleListRolesApiRolesGet(t *testing.T) {
	t.Run("the listing carries the seed role first, then the custom role, each without its persona body", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty\nDesign things."}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/roles", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{
			map[string]any{
				"size_chars":     169,
				"cap_chars":      1000,
				"key":            "assistant",
				"name":           "Assistant",
				"owner_id":       "owner",
				"schema_version": 3,
				"is_default":     true,
				"is_seed":        true,
			},
			map[string]any{
				"size_chars":     21,
				"cap_chars":      1000,
				"key":            "r-design",
				"name":           "Design",
				"owner_id":       "owner",
				"schema_version": 3,
				"is_default":     false,
				"is_seed":        false,
			},
		})
		dashboard.wantFrames()
	})

	t.Run("an edited seed role reports its own size while a tombstoned custom role is off the roster entirely", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/roles/assistant", owner, `{"definition_md":"# mine"}`)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-gone", Name: "Gone", Tombstoned: true}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty\nDesign things."}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}

		rec := apiRequest(t, h, "GET", "/api/roles", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{
			map[string]any{
				"size_chars":     6,
				"cap_chars":      1000,
				"key":            "assistant",
				"name":           "Assistant",
				"owner_id":       "owner",
				"schema_version": 3,
				"is_default":     false,
				"is_seed":        true,
			},
			map[string]any{
				"size_chars":     21,
				"cap_chars":      1000,
				"key":            "r-design",
				"name":           "Design",
				"owner_id":       "owner",
				"schema_version": 3,
				"is_default":     false,
				"is_seed":        false,
			},
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/roles", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetRoleApiRolesRoleGet(t *testing.T) {
	t.Run("a custom role answers its stored definition with the overlay flags cleared", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty\nDesign things."}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/roles/r-design", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     21,
			"cap_chars":      1000,
			"key":            "r-design",
			"name":           "Design",
			"definition_md":  "# Duty\nDesign things.",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"is_seed":        false,
		})
		dashboard.wantFrames()
	})

	t.Run("an unedited seed role answers the shipped definition flagged default and seed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/roles/assistant", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     169,
			"cap_chars":      1000,
			"key":            "assistant",
			"name":           "Assistant",
			"definition_md":  apiTestAssistantSeedDefinitionMD,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     true,
			"is_seed":        true,
		})
	})

	t.Run("a role key nothing carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/roles/nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "role 'nope' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/roles/assistant", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleCreateRoleApiRolesPost(t *testing.T) {
	t.Run("a named role with a named founding member answers the two minted ids and fans the role and member deltas", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/roles", owner, `{"name":"Design","member_name":"Zed"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":    apiAnyString,
			"member_id":   apiAnyString,
			"member_name": "Zed",
		})
		dashboard.wantFrames(
			map[string]any{
				"seq":   1,
				"topic": "role_def",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "role_def",
					"key":     apiAnyString,
					"epoch":   1,
					"deleted": false,
					"payload": nil,
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			map[string]any{
				"seq":   2,
				"topic": "member",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "member",
					"key":     apiAnyString,
					"epoch":   2,
					"deleted": false,
					"payload": map[string]any{
						"id":            apiAnyString,
						"name":          "Zed",
						"owner_id":      "owner",
						"status":        "active",
						"desired_state": "offline",
					},
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
		)
		bystander.wantFrames()
	})

	t.Run("a request that names no member gets one picked for it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/roles", owner, `{"name":"Design"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role_key":    apiAnyString,
			"member_id":   apiAnyString,
			"member_name": apiAnyString,
		})
	})

	t.Run("the new role is readable at the roster and at its own row", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, created := apiJSON(t, h, "POST", "/api/roles", owner, `{"name":"Design","member_name":"Zed"}`)
		roleKey, _ := created["role_key"].(string)

		status, data := apiJSON(t, h, "GET", "/api/roles/"+roleKey, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"size_chars":     155,
			"cap_chars":      1000,
			"key":            roleKey,
			"name":           "Design",
			"definition_md":  apiTestCustomRoleTemplateMD,
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"is_seed":        false,
		})
	})

	t.Run("a body with no name at all answers 422 and creates nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: name")
		dashboard.wantFrames()
	})

	t.Run("a name that is only whitespace answers 422 and creates nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles", owner, `{"name":"  "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "role requires a name")
		dashboard.wantFrames()
	})

	t.Run("an effort outside the vocabulary answers 422 naming what was sent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles", owner, `{"name":"Design","effort":"bogus"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "effort must be one of [high low max medium xhigh]; got 'bogus'")
		dashboard.wantFrames()
	})

	t.Run("a runtime outside the vocabulary answers 422 naming what was sent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles", owner, `{"name":"Design","runtime":"bogus"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "runtime must be one of [claude codex]; got 'bogus'")
		dashboard.wantFrames()
	})

	t.Run("a body that is not JSON answers 422 and creates nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles", agent, `{"name":"Design"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles", "", `{"name":"Design"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})
}

func TestHandleUpdateRoleApiRolesRolePost(t *testing.T) {
	t.Run("editing a custom role's name and definition answers the receipt over what was judged and fans the role delta", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty\nDesign things."}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/roles/r-design", owner,
			`{"name":"Design Lead","definition_md":"# Duty\nDesign things."}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"key":        "r-design",
			"name":       "Design Lead",
			"is_default": false,
			"is_seed":    false,
			"size_chars": 21,
			"cap_chars":  1000,
			"sha256":     "724db319a7b3d260d62d6fbe709d8c12cbbf7ff023b99d9efb162cec188b5ff0",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "role_def",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "role_def",
				"key":     "owner::r-design",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a seed role keeps its shipped name and reports it back, while the definition edit lands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/assistant", owner, `{"name":"Renamed"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"key":        "assistant",
			"name":       "Assistant",
			"is_default": false,
			"is_seed":    true,
			"size_chars": 169,
			"cap_chars":  1000,
			"sha256":     "8e4957c5da2787a69e5d3470fafcc3badbf5b6e2f863d3eb0cfb29a8988c2425",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "role_def",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "role_def",
				"key":     "owner::assistant",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a definition over the cap that is not shorter than what is stored answers 400 and writes nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty\nDesign things."}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/r-design", owner,
			`{"definition_md":"`+strings.Repeat("x", 1001)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"the role definition doc you are writing is 1001 chars, over the 1000-char cap, "+
				"and is not shorter than the 21 chars already stored — nothing was written. "+
				"What is already stored is never truncated, but every update must land at or "+
				"under the cap, or at least come out SHORTER than what is there now. Drop stale "+
				"or superseded material as part of this write (or in a shrinking write first), "+
				"then write again.")
		dashboard.wantFrames()
	})

	t.Run("a role key nothing carries answers 404 naming it and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/nope", owner, `{"name":"Design"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "role 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a body that is not JSON answers 422 and writes nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/r-design", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/assistant", agent, `{"name":"Design"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/assistant", "", `{"name":"Design"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})
}

func TestRoleDefReceiptOf(t *testing.T) {
	t.Run("a receipt keeps role identity flags and read-face size while hashing the definition", func(t *testing.T) {
		got := roleDefReceiptOf(&roleDefDTO{
			SizeChars:     2,
			CapChars:      1000,
			Key:           "assistant",
			Name:          "Assistant",
			DefinitionMD:  "é😊",
			OwnerID:       "owner",
			SchemaVersion: 3,
			IsDefault:     false,
			IsSeed:        true,
		})
		want := roleDefReceiptDTO{
			Key:       "assistant",
			Name:      "Assistant",
			IsDefault: false,
			IsSeed:    true,
			SizeChars: 2,
			CapChars:  1000,
			Sha256:    "a94589e1b89859b57430d91195095e623e7aefe07d4426d4788ae716a24dd36a",
		}
		if got != want {
			t.Fatalf("roleDefReceiptOf = %#v, want %#v", got, want)
		}
	})
}

func TestHandleResetRoleApiRolesRoleResetPost(t *testing.T) {
	t.Run("resetting an edited seed role answers the seed receipt flagged default and fans the role delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/roles/assistant", owner, `{"definition_md":"# mine"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/roles/assistant/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"key":        "assistant",
			"name":       "Assistant",
			"is_default": true,
			"is_seed":    true,
			"size_chars": 169,
			"cap_chars":  1000,
			"sha256":     "8e4957c5da2787a69e5d3470fafcc3badbf5b6e2f863d3eb0cfb29a8988c2425",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "role_def",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "role_def",
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

	t.Run("resetting a seed role nobody has edited answers the same seed receipt", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/roles/assistant/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"key":        "assistant",
			"name":       "Assistant",
			"is_default": true,
			"is_seed":    true,
			"size_chars": 169,
			"cap_chars":  1000,
			"sha256":     "8e4957c5da2787a69e5d3470fafcc3badbf5b6e2f863d3eb0cfb29a8988c2425",
		})
	})

	t.Run("a custom role has no seed to go back to and answers 404 naming it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/r-design/reset", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "role 'r-design' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/assistant/reset", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/roles/assistant/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})
}

func TestHandleDeleteRoleApiRolesRoleDelete(t *testing.T) {
	t.Run("deleting a custom role answers the cascade counts and fans a delta for every table it emptied", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		if err := d.PutMember(Member{
			ID: "m-zed", Name: "Zed", Kind: KindStaff,
			RoleKey: "r-design", RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"m-zed","body":"hi"}`)
		apiJSON(t, h, "POST", "/api/chat/mark-read", owner, `{"peer":"m-zed"}`)
		apiJSON(t, h, "POST", "/api/insight/r-design", owner, `{"text":"I"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/r-design", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":                     "r-design",
			"removed_member_ids":       []any{"m-zed"},
			"deleted_chat_messages":    1,
			"deleted_chat_attachments": 0,
			"deleted_chat_reads":       1,
		})
		memberFrame := map[string]any{
			"seq":   6,
			"topic": "member",
			"op":    "remove",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::m-zed",
				"epoch":   6,
				"deleted": true,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(
			map[string]any{
				"seq":   4,
				"topic": "chat",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "chat",
					"key":     "owner::m-zed",
					"epoch":   4,
					"deleted": false,
					"payload": nil,
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			map[string]any{
				"seq":   5,
				"topic": "chat_read",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "chat_read",
					"key":     "owner::m-zed",
					"epoch":   5,
					"deleted": false,
					"payload": nil,
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			memberFrame,
			map[string]any{
				"seq":   7,
				"topic": "insight",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "insight",
					"key":     "owner::r-design",
					"epoch":   7,
					"deleted": false,
					"payload": nil,
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			map[string]any{
				"seq":   8,
				"topic": "role_def",
				"op":    "remove",
				"data": map[string]any{
					"entity":  "role_def",
					"key":     "owner::r-design",
					"epoch":   8,
					"deleted": true,
					"payload": nil,
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
		)
		bystander.wantFrames()
	})

	t.Run("the deleted member's own connection receives the member removal and nobody else's does", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		if err := d.PutMember(Member{
			ID: "m-zed", Name: "Zed", Kind: KindStaff,
			RoleKey: "r-design", RosterStatus: RosterStatusRemoved,
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		deleted := apiTestListen(t, api, "m-zed")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/r-design", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":                     "r-design",
			"removed_member_ids":       []any{"m-zed"},
			"deleted_chat_messages":    0,
			"deleted_chat_attachments": 0,
			"deleted_chat_reads":       0,
		})
		memberFrame := map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "remove",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::m-zed",
				"epoch":   1,
				"deleted": true,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(memberFrame, map[string]any{
			"seq":   2,
			"topic": "role_def",
			"op":    "remove",
			"data": map[string]any{
				"entity":  "role_def",
				"key":     "owner::r-design",
				"epoch":   2,
				"deleted": true,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		deleted.wantFrames(memberFrame)
		bystander.wantFrames()
	})

	t.Run("deleting a custom role nothing hangs off answers zero cascade counts and fans only the role removal", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/r-design", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":                     "r-design",
			"removed_member_ids":       []any{},
			"deleted_chat_messages":    0,
			"deleted_chat_attachments": 0,
			"deleted_chat_reads":       0,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "role_def",
			"op":    "remove",
			"data": map[string]any{
				"entity":  "role_def",
				"key":     "owner::r-design",
				"epoch":   1,
				"deleted": true,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a role with an online member answers 409 naming the member and deletes nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		if err := d.PutMember(Member{
			ID: "m-zed", Name: "Zed", Kind: KindStaff,
			RoleKey: "r-design", RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		apiTestListen(t, api, "m-zed")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/r-design", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "role 'r-design' has online member(s): m-zed — stop them before deleting")
		dashboard.wantFrames()
	})

	t.Run("a role with two online members answers 409 listing both in id order", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		for _, m := range []Member{
			{ID: "m-b", Name: "Bo", Kind: KindStaff, RoleKey: "r-design", RosterStatus: RosterStatusActive},
			{ID: "m-a", Name: "Ana", Kind: KindStaff, RoleKey: "r-design", RosterStatus: RosterStatusActive},
		} {
			if err := d.PutMember(m); err != nil {
				t.Fatalf("PutMember: %v", err)
			}
		}
		apiTestListen(t, api, "m-b")
		apiTestListen(t, api, "m-a")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/r-design", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "role 'r-design' has online member(s): m-a, m-b — stop them before deleting")
		dashboard.wantFrames()
	})

	t.Run("a seed role answers 403 saying it is built in", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/assistant", owner, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "role 'assistant' is a built-in seed role and cannot be deleted")
		dashboard.wantFrames()
	})

	t.Run("a role key nothing carries answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/r-nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "role 'r-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/r-design", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/roles/r-design", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})
}
