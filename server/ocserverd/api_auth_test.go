package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMintWardenToken(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	token, err := api.mintWardenToken(Member{ID: "m-box", Kind: machineKind})
	if err != nil {
		t.Fatalf("mintWardenToken: %v", err)
	}
	claims, err := verifyJWT(token, api.keys.signingSecret(), time.Now().Unix())
	if err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	if claims["sub"] != "m-box" || claims["scope"] != "agent" {
		t.Fatalf("claims = %v, want sub m-box and agent scope", claims)
	}
	iat, iatOK := claims["iat"].(float64)
	exp, expOK := claims["exp"].(float64)
	if !iatOK || !expOK || exp-iat != float64(api.wardenCredLifetimeValue()) {
		t.Fatalf("warden claims = %v, want exp exactly warden credential lifetime after iat", claims)
	}
	if _, ok := claims["machine_id"]; ok {
		t.Fatalf("warden claims unexpectedly carry machine_id: %v", claims)
	}
	if _, err := api.mintWardenToken(Member{ID: "agent-1", Kind: KindStaff}); err == nil {
		t.Fatal("a non-warden member must not receive a permanent token")
	}
}

func TestMintMemberToken(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	token, err := api.mintMemberToken(Member{ID: "mira", DesiredMachineID: "m-lab"}, 3600)
	if err != nil {
		t.Fatalf("mintMemberToken: %v", err)
	}

	claims, err := verifyJWT(token, api.keys.signingSecret(), time.Now().Unix())
	if err != nil {
		t.Fatalf("verify minted member token: %v", err)
	}
	if claims["sub"] != "mira" || claims["scope"] != "agent" || claims["machine_id"] != "m-lab" {
		t.Fatalf("claims = %v, want mira/agent/m-lab", claims)
	}
	iat, iatOK := claims["iat"].(float64)
	exp, expOK := claims["exp"].(float64)
	if !iatOK || !expOK || exp-iat != 3600 {
		t.Fatalf("claims = %v, want an expiry exactly 3600 seconds after iat", claims)
	}

	unplaced, err := api.mintMemberToken(Member{ID: "mira"}, 3600)
	if err != nil {
		t.Fatalf("mintMemberToken without placement: %v", err)
	}
	unplacedClaims, err := verifyJWT(unplaced, api.keys.signingSecret(), time.Now().Unix())
	if err != nil {
		t.Fatalf("verify unplaced member token: %v", err)
	}
	if _, ok := unplacedClaims["machine_id"]; ok {
		t.Fatalf("unplaced member claims unexpectedly carry machine_id: %v", unplacedClaims)
	}
}

func TestHandleLoginApiLoginPost(t *testing.T) {
	t.Run("the owner password answers 200 and a bearer owner token", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": 86400,
			"owner_id":   "owner",
		})
	})

	t.Run("the minted token opens an owner-gated row", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		_, minted := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`"}`)
		token, _ := minted["token"].(string)

		status, data := apiJSON(t, h, "GET", "/api/auth/mfa", token, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"offered":     false,
			"enrolled":    false,
			"secret":      nil,
			"otpauth_uri": nil,
		})
	})

	t.Run("the owner password plus a live code answers 200 while a factor is armed", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		secret := apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": 86400,
			"owner_id":   "owner",
		})
	})

	t.Run("a request missing `password` answers 422", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "", `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: password")
	})

	t.Run("a request on a server with no signing secret answers 401 naming the configuration", func(t *testing.T) {
		_, h, _, _ := newAPITestStackWithoutSigningSecret(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "auth not configured")
	})

	t.Run("a wrong password answers 401 naming neither factor", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "", `{"password":"not-the-password"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("a password with no password ever set answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestStack(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "", `{"password":"anything-at-all"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("the right password with a wrong code answers 401 with the same refusal", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`","code":"000000"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("the right password with a wrong code hands the assistant alert one refusal to report", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		apiTestArmMFA(t, api, d)
		reported := make(chan int, 4)
		api.authAlertDeliver = func(count int) { reported <- count }

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`","code":"000000"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")

		select {
		case count := <-reported:
			if count != 1 {
				t.Fatalf("want 1 refusal folded into the alert, got %d", count)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the refusal reported nothing to the assistant alert")
		}
	})

	// The refusal floor is a wait, and a wait must not hold the one write
	// connection: while a wrong-code login sits in its floor, another request's
	// write still answers at once. The alert hand-off marks the refusal as
	// decided; the floor starts right after it.
	for _, shape := range windowDALShapes {
		t.Run(shape+": a write answers at once while a wrong-code login waits out its floor", func(t *testing.T) {
			const floor, answerWithin = 2 * time.Second, 500 * time.Millisecond
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			apiTestArmMFA(t, api, d)
			api.credentialFailureFloor = floor
			refused := make(chan struct{}, 1)
			api.authAlertDeliver = func(int) { refused <- struct{}{} }
			dalPutTask(t, d, windowOpenTask("T-1"))

			login := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				login <- apiRequest(t, h, "POST", "/api/login", "",
					`{"password":"`+apiTestOwnerPassword+`","code":"000000"}`)
			}()
			select {
			case <-refused:
			case <-time.After(floor):
				t.Fatal("the wrong-code login never reached its refusal")
			}
			time.Sleep(300 * time.Millisecond)

			start := time.Now()
			status, data := windowJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)
			took := time.Since(start)

			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			if took > answerWithin {
				t.Fatalf("the write answered after %v, want within %v: it waited behind the login's floor", took, answerWithin)
			}
			rec := <-login
			if rec.Code != http.StatusUnauthorized ||
				rec.Body.String() != `{"error":{"code":"unauthorized","message":"invalid password or code"}}` {
				t.Fatalf("login: want 401 invalid password or code, got %d %s", rec.Code, rec.Body.String())
			}
		})
	}

	t.Run("attempts beyond the in-flight cap answer 429 with a one-second Retry-After", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		api.credentialFailureFloor = 2 * time.Second

		type answer struct {
			code       int
			retryAfter string
			body       string
		}
		answers := make(chan answer, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"password":"wrong"}`))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				answers <- answer{rec.Code, rec.Header().Get("Retry-After"), rec.Body.String()}
			}()
		}
		wg.Wait()
		close(answers)
		throttled := 0
		for got := range answers {
			switch got.code {
			case 429:
				throttled++
				if got.retryAfter != "1" {
					t.Fatalf("Retry-After: want \"1\", got %q", got.retryAfter)
				}
				if got.body != `{"error":{"code":"client_error","message":"too many failed credential attempts; retry in 1s"}}` {
					t.Fatalf("throttled body: %q", got.body)
				}
			case 401:
				if got.body != `{"error":{"code":"unauthorized","message":"invalid password or code"}}` {
					t.Fatalf("refused body: %q", got.body)
				}
			default:
				t.Fatalf("want 401 or 429, got %d (%s)", got.code, got.body)
			}
		}
		if throttled != 4 {
			t.Fatalf("want 4 of 8 concurrent attempts refused for concurrency, got %d", throttled)
		}
	})
}

func TestHandleMintApiMintPost(t *testing.T) {
	t.Run("an owner minting for a staff member answers 200 and a bearer token for that member", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", owner,
			`{"member_id":"mira","ttl_days":7}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": 604800,
			"owner_id":   "mira",
		})
	})

	t.Run("a ttl over the ceiling answers 200 capped at 400 days", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", owner,
			`{"member_id":"mira","ttl_days":9999}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": 34560000,
			"owner_id":   "mira",
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", "", `{"member_id":"mira","ttl_days":7}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated admin_agent identity answers 403 because this row requires owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/mint", admin, `{"member_id":"mira","ttl_days":7}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request missing `ttl_days` answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", owner, `{"member_id":"mira"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: ttl_days")
	})

	t.Run("a member that is not on the roster answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", owner,
			`{"member_id":"ghost","ttl_days":7}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})
}

const (
	previewRoleKey      = "preview-writer"
	previewCodexBoot    = "# CODEX BOOT PREVIEW MARKER"
	previewClaudeBoot   = "# CLAUDE BOOT PREVIEW MARKER"
	previewEveryoneLore = "EVERYONE LORE FOR EVERY MEMBER"
)

func seedRolePreview(t *testing.T, d *DAL, members []Member, ownLore map[string]string) {
	t.Helper()
	if err := d.PutRoleDef(RoleDef{
		RoleKey: previewRoleKey, Name: "Preview Writer", DefinitionMD: "# Preview role instructions",
	}); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	for _, doc := range []BootDocument{
		{Kind: docKindBootSequence, Key: bootSequenceKeyCodex, Text: previewCodexBoot},
		{Kind: docKindBootSequence, Key: bootSequenceKeyClaude, Text: previewClaudeBoot},
	} {
		if err := d.PutBootDocument(doc); err != nil {
			t.Fatalf("PutBootDocument(%s): %v", doc.Key, err)
		}
	}
	for _, member := range members {
		if err := d.PutMember(member); err != nil {
			t.Fatalf("PutMember(%s): %v", member.ID, err)
		}
	}
	entries := []LoreEntry{{ScopeKind: LoreScopeEveryone, Body: previewEveryoneLore}}
	for memberID, body := range ownLore {
		entries = append(entries, LoreEntry{ScopeKind: LoreScopeAgent, ScopeKey: memberID, Body: body})
	}
	for _, e := range entries {
		e.Title, e.AuthorID, e.State = "preview lore", "owner", LoreStateActive
		e.EffectiveTS, e.CreatedTS, e.UpdatedTS = 1, 1, 1
		if _, err := d.CreateLoreEntryMintingID(e); err != nil {
			t.Fatalf("CreateLoreEntryMintingID(%s): %v", e.Body, err)
		}
	}
}

func TestHandleBootstrapApiBootstrapPost(t *testing.T) {
	t.Run("a spawn naming a member answers 200 with that member's boot package and a token", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"member_id":"mira"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":    "assistant",
			"name":    "Mira",
			"context": apiAnyString,
			"token":   apiAnyString,
		})
		token, ok := data["token"].(string)
		if !ok || token == "" {
			t.Fatalf("bootstrap token: want a non-empty string, got %#v", data["token"])
		}
		claims, err := verifyJWT(token, api.keys.signingSecret(), time.Now().Unix())
		if err != nil {
			t.Fatalf("verify member token: %v", err)
		}
		if claims["sub"] != "mira" || claims["scope"] != "agent" || claims["machine_id"] != ServerSelfHost {
			t.Fatalf("member token claims = %v, want mira/agent/%s", claims, ServerSelfHost)
		}
	})

	t.Run("a role-only preview uses its one active staff but answers a null token", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"role":"assistant"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":    "assistant",
			"name":    "Mira",
			"context": apiAnyString,
			"token":   nil,
		})
	})

	t.Run("a role-only preview of a role with one active member answers that member's boot context and a null token", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		seedRolePreview(t, d, []Member{
			{ID: "m-preview-codex", Name: "Codex Previewer", Kind: KindStaff, RoleKey: previewRoleKey,
				Runtime: RuntimeCodex, RosterStatus: RosterStatusActive},
			{ID: "m-preview-removed", Name: "Former Previewer", Kind: KindStaff, RoleKey: previewRoleKey,
				Runtime: RuntimeClaude, RosterStatus: RosterStatusRemoved},
		}, map[string]string{
			"m-preview-codex":   "ONLY ACTIVE MEMBER LORE",
			"m-preview-removed": "REMOVED MEMBER LORE",
		})

		status, preview := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"role":"`+previewRoleKey+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, preview)
		}
		apiWantBody(t, preview, map[string]any{
			"role":    previewRoleKey,
			"name":    "Codex Previewer",
			"context": apiAnyString,
			"token":   nil,
		})
		context := preview["context"].(string)
		everyoneAt := strings.Index(context, previewEveryoneLore)
		ownAt := strings.Index(context, "ONLY ACTIVE MEMBER LORE")
		if !strings.Contains(context, "\n# 傳承\n") || everyoneAt < 0 || ownAt < everyoneAt {
			t.Fatalf("want a 傳承 block with everyone entries before the member's own: %s", context)
		}
		if strings.Contains(context, "REMOVED MEMBER LORE") {
			t.Fatalf("preview carries a removed member's lore: %s", context)
		}
		if !strings.HasSuffix(context, previewCodexBoot+"\n") {
			t.Fatalf("want the Codex boot sequence last: %s", context)
		}

		status, boot := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"member_id":"m-preview-codex"}`)
		if status != 200 {
			t.Fatalf("member boot: want 200, got %d (%v)", status, boot)
		}
		if boot["context"] != context {
			t.Fatalf("preview differs from the member's boot context\npreview: %s\nboot: %s", context, boot["context"])
		}
	})

	for _, tc := range []struct {
		name    string
		members []Member
	}{
		{
			name: "a role-only preview of a role with no active member omits 傳承 and uses the Claude boot sequence",
			members: []Member{
				{ID: "m-preview-former", Name: "Former Previewer", Kind: KindStaff, RoleKey: previewRoleKey,
					Runtime: RuntimeCodex, RosterStatus: RosterStatusRemoved},
			},
		},
		{
			name: "a role-only preview of a role with two active members omits 傳承 and uses the Claude boot sequence",
			members: []Member{
				{ID: "m-preview-one", Name: "First Previewer", Kind: KindStaff, RoleKey: previewRoleKey,
					Runtime: RuntimeCodex, RosterStatus: RosterStatusActive},
				{ID: "m-preview-two", Name: "Second Previewer", Kind: KindStaff, RoleKey: previewRoleKey,
					Runtime: RuntimeCodex, RosterStatus: RosterStatusActive},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h, d, owner := newAPITestServer(t)
			ownLore := map[string]string{}
			for _, m := range tc.members {
				ownLore[m.ID] = "LORE OF " + m.ID
			}
			seedRolePreview(t, d, tc.members, ownLore)

			status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"role":"`+previewRoleKey+`"}`)
			if status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			apiWantBody(t, data, map[string]any{
				"role":    previewRoleKey,
				"name":    "Preview Writer",
				"context": apiAnyString,
				"token":   nil,
			})
			context := data["context"].(string)
			if strings.Contains(context, "# 傳承") || strings.Contains(context, previewEveryoneLore) ||
				strings.Contains(context, "LORE OF ") {
				t.Fatalf("want no 傳承 block: %s", context)
			}
			if !strings.HasSuffix(context, previewClaudeBoot+"\n") {
				t.Fatalf("want the Claude boot sequence last: %s", context)
			}
		})
	}

	t.Run("an empty body answers 200 on the default role", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":    "assistant",
			"name":    "Mira",
			"context": apiAnyString,
			"token":   nil,
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", "", `{"member_id":"mira"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", agent, `{"member_id":"mira"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a body that is not JSON answers 422 without reaching the roster", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `not json`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character 'o' in literal null (expecting 'u')")
	})

	t.Run("a member that is not on the roster answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"member_id":"ghost"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})

	t.Run("a role nobody defines answers 404 naming the role", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"role":"nosuchrole"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "role 'nosuchrole' not found")
	})
}

func TestNoteTokenKeyObservation(t *testing.T) {
	api, h, d, owner := newAPITestServer(t)
	machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")

	stream := apiEventsStream(t, h, credential, "")
	m, err := d.GetMember(machineID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if m == nil || m.TokenKeyID != "k-legacy" {
		t.Fatalf("machine token key = %+v, want k-legacy", m)
	}
	stream.stop()
	api.tokenKeyObsMu.Lock()
	_, machineMemoized := api.tokenKeyObs[machineID]
	_, agentMemoized := api.tokenKeyObs[apiTestPlainAgentID]
	api.tokenKeyObsMu.Unlock()
	if !machineMemoized || agentMemoized {
		t.Fatalf("token-key memo = %v, want only the machine identity", api.tokenKeyObs)
	}

	agentStream := apiEventsStream(t, h, apiTestAgentToken(t, api, apiTestPlainAgentID, ""), "")
	agentStream.stop()
	api.tokenKeyObsMu.Lock()
	_, agentMemoized = api.tokenKeyObs[apiTestPlainAgentID]
	api.tokenKeyObsMu.Unlock()
	if agentMemoized {
		t.Fatal("a non-warden observation must not be memoized")
	}

	t.Run("a direct machine observation records the signing key and memo", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID, _ := apiTestMachineCredential(t, h, owner, "Direct observation")

		api.noteTokenKeyObservation(map[string]any{"sub": machineID}, "k-legacy")

		m, err := d.GetMember(machineID)
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m == nil || m.TokenKeyID != "k-legacy" {
			t.Fatalf("machine token key = %+v, want k-legacy", m)
		}
		api.tokenKeyObsMu.Lock()
		obs, memoized := api.tokenKeyObs[machineID]
		api.tokenKeyObsMu.Unlock()
		if !memoized || obs.keyID != "k-legacy" {
			t.Fatalf("direct observation memo = %+v, %v; want k-legacy", obs, memoized)
		}
	})
}

func TestAskMachineToRenewIfStale(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	api.keys = newKeyring([]signingKey{
		{ID: "k-old", Key: []byte("old-secret")},
		{ID: "k-new", Key: []byte("new-secret")},
	}, "k-new")
	api.keyRenewClock = func() time.Time { return time.Unix(1700000000, 0) }
	api.rememberTokenKey("m-box", tokenKeyObservation{keyID: "k-old"})
	if _, err := api.hub.Connect("m-box", "m-box"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	api.askMachineToRenewIfStale("m-box", "k-old")
	if got := api.hub.PendingWardenCommands("m-box"); got != 1 {
		t.Fatalf("pending renew commands = %d, want 1", got)
	}
	queued := api.hub.DrainWardenCommands("m-box")
	if len(queued) != 1 || queued[0].Subject != "m-box" {
		t.Fatalf("queued renew command = %+v, want one command for m-box", queued)
	}
	digest, ok := decodeWardenCommandFrame(queued[0].Frame)
	if !ok || digest != (wardenCommandDigest{Verb: reconcileCmdRenew, MemberID: "m-box"}) {
		t.Fatalf("renew frame digest = %+v, %v", digest, ok)
	}

	api.rememberTokenKey("m-box", tokenKeyObservation{keyID: "k-old"})
	api.askMachineToRenewIfStale("m-box", "k-new")
	if got := api.hub.PendingWardenCommands("m-box"); got != 0 {
		t.Fatalf("current-key renew commands = %d, want 0", got)
	}
}

func TestClaimRenewAsk(t *testing.T) {
	now := time.Unix(1700000000, 0)
	api := &apiServer{keyRenewClock: func() time.Time { return now }}
	api.rememberTokenKey("m-box", tokenKeyObservation{keyID: "k-old"})
	if !api.claimRenewAsk("m-box", "k-old") {
		t.Fatal("the first ask for a key must win")
	}
	if api.claimRenewAsk("m-box", "k-old") {
		t.Fatal("a second ask inside the interval must be refused")
	}
	if api.claimRenewAsk("m-box", "k-new") {
		t.Fatal("an ask for a different key must not use the old memo")
	}
	now = now.Add(renewAskInterval)
	if !api.claimRenewAsk("m-box", "k-old") {
		t.Fatal("an ask at the interval boundary must be allowed")
	}
}

func TestKeyRenewNow(t *testing.T) {
	want := time.Unix(1700000000, 123)
	api := &apiServer{keyRenewClock: func() time.Time { return want }}
	if got := api.keyRenewNow(); !got.Equal(want) {
		t.Fatalf("keyRenewNow() = %v, want %v", got, want)
	}
}

func TestRememberTokenKey(t *testing.T) {
	api := &apiServer{}
	api.rememberTokenKey("m-box", tokenKeyObservation{keyID: "k-old"})
	api.tokenKeyObsMu.Lock()
	got := api.tokenKeyObs["m-box"]
	api.tokenKeyObsMu.Unlock()
	if got.keyID != "k-old" || !got.renewAskedAt.IsZero() {
		t.Fatalf("first memo = %+v, want k-old with no ask stamp", got)
	}

	askedAt := time.Unix(1700000000, 0)
	api.rememberTokenKey("m-box", tokenKeyObservation{keyID: "k-new", renewAskedAt: askedAt})
	api.tokenKeyObsMu.Lock()
	got = api.tokenKeyObs["m-box"]
	api.tokenKeyObsMu.Unlock()
	if got.keyID != "k-new" || !got.renewAskedAt.Equal(askedAt) {
		t.Fatalf("replacement memo = %+v, want k-new at %v", got, askedAt)
	}
}
