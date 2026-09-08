// Skeleton generated from server/ocserverd/api_machines.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"testing/fstest"
)

func TestMint(t *testing.T) {
	t.Skip("TODO: mint issues a fresh single-use code bound to machineID (32 random bytes, base64url — the ensureFirstRunClaimToken mint pattern) and sweeps expired entries so abandoned boot commands never accumulate.")
}

func TestTake(t *testing.T) {
	t.Skip("TODO: take redeems a code: on a live match the entry is deleted ATOMICALLY under the same lock (single-use by construction) and the bound machine id is returned.")
}

func TestBuildInstallScript(t *testing.T) {
	t.Skip("TODO: buildInstallScript is the self-contained bash installer served over GET /install.sh (handlers._build_install_script — byte-shape twin).")
}

func TestBuildInstallScriptWithCode(t *testing.T) {
	t.Skip("TODO: buildInstallScriptWithCode is the claim-code variant of the installer: the script FIRST probes that the server can actually serve the warden binary (a HEAD on the public binary route — a 503 there must NOT burn the one-time code), THEN redeems the code for the machine's real exec-token (POST /api/machines/claim) — a dead code fails before any bytes are downloaded — then proceeds exactly like the token variant (which stays byte-identical for legacy ?token= URLs).")
}

func TestMachineBinStatus(t *testing.T) {
	t.Skip("TODO: machineBinStatus compares the content fingerprints machineID's warden heartbeat reported (the telemetry entry's `binaries` — keyed by the warden's own member id, which IS the machine id) against the server's embedded prebuilt hashes (s.binHashes).")
}

func TestValidWardenShape(t *testing.T) {
	t.Skip("TODO: ValidWardenShape gates the ingest handler's closed enum.")
}

func TestMachineWardenShape(t *testing.T) {
	t.Skip("TODO: machineWardenShape reads back the shape machineID's warden REPORTED, keyed the same way as machineBinStatus (the warden's own member id IS the machine id).")
}

func TestValidCutoverEffect(t *testing.T) {
	t.Skip("TODO: ValidCutoverEffect gates the ingest handler's closed enum.")
}

func TestMachineCutoverEffect(t *testing.T) {
	t.Skip("TODO: machineCutoverEffect reads back the verdict machineID's warden REPORTED.")
}

func TestMachineClaudeInfo(t *testing.T) {
	t.Skip("TODO: machineClaudeInfo derives the machine rows' claude CLI columns (T-97ee) from machineID's warden heartbeat (the telemetry entry's `claude` probe — keyed by the warden's own member id, which IS the machine id; the same keying as machineBinStatus above): - version: the probed CLI version string; nil when unreported (claude unresolved, probe failed, or an older warden that never probes); - credSource: synthesized from the cred_file × keychain presence bools — \"both\" | \"file\" | \"keychain\" | \"none\" when both are known; with only one bool reported (e.g.")
}

func TestMachineRuntimeCapabilities(t *testing.T) {
	t.Skip("TODO: machineRuntimeCapabilities projects the provider-neutral readiness probes from a warden heartbeat.")
}

func TestMachineSupportsRuntime(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestMachineTokenKey(t *testing.T) {
	t.Skip("TODO: machineTokenKey projects the T-80 observation onto the wire: WHICH signing key this station last verified that machine's credential with, and whether that is the key signing right now.")
}

func TestHandleListMachinesApiMachinesGet(t *testing.T) {
	t.Run("the out-of-box roster answers the server-local machine alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{apiTestServerSelfMachineRow(false)})
		dashboard.wantFrames()
	})

	t.Run("an onboarded machine is listed after the server-local row under its overlay name", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiJSON(t, h, "PATCH", "/api/machines/"+machineID, owner, `{"display_name":"Loft"}`)

		rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{
			apiTestServerSelfMachineRow(false),
			map[string]any{
				"machine_id":           machineID,
				"display_name":         "Loft",
				"online":               false,
				"is_self":              false,
				"bin_status":           nil,
				"claude_version":       nil,
				"claude_cred_source":   nil,
				"claude_sub_readable":  nil,
				"runtime_capabilities": map[string]any{},
				"warden_shape":         nil,
				"cutover_effect":       nil,
				"token_key_id":         nil,
				"token_key_current":    nil,
			},
		})
	})

	t.Run("a machine holding a live connection is listed online", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestListen(t, api, ServerSelfHost)

		rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{apiTestServerSelfMachineRow(true)})
	})

	t.Run("an ordinary agent identity is served the same roster because this row's floor is machine", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		rec := apiRequest(t, h, "GET", "/api/machines", agent, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{apiTestServerSelfMachineRow(false)})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/machines", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

// apiTestServerSelfMachineRow is the out-of-box seed's own machine row as
// GET /api/machines renders it — every column a warden that has never
// heartbeated leaves unknown.
func apiTestServerSelfMachineRow(online bool) map[string]any {
	return map[string]any{
		"machine_id":           "m-server-self",
		"display_name":         "伺服器這一台",
		"online":               online,
		"is_self":              true,
		"bin_status":           nil,
		"claude_version":       nil,
		"claude_cred_source":   nil,
		"claude_sub_readable":  nil,
		"runtime_capabilities": map[string]any{},
		"warden_shape":         nil,
		"cutover_effect":       nil,
		"token_key_id":         nil,
		"token_key_current":    nil,
	}
}

func TestHandleOnboardMachineApiMachinesPost(t *testing.T) {
	t.Run("onboarding a machine answers the minted credential and installer one-liner and fans the machine's member delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"Studio Mac"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":        apiAnyString,
			"machine_id":       apiAnyString,
			"token":            apiAnyString,
			"expires_in":       0,
			"boot_command":     apiAnyString,
			"claim_code":       apiAnyString,
			"claim_expires_in": 600,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     apiAnyString,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            apiAnyString,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a display name that is only whitespace answers 422 and onboards nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"  "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name is required")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines", agent, `{"display_name":"Studio Mac"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines", "", `{"display_name":"Studio Mac"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestClearResidualUninstall(t *testing.T) {
	t.Skip("TODO: clearResidualUninstall consumes a leftover one-shot uninstall intent on an install path: every re-install entry point MUST zero a residual desired_state=\"uninstall\" BEFORE installing, or the fresh warden would reconnect straight into a standing kill order (uninstall→re-install loop — real incident, 2026-07).")
}

func TestHandleMachineBootCommandApiMachinesMachineIdBootCommandGet(t *testing.T) {
	t.Run("a machine re-fetching its one-liner is handed a claim code and a freshly minted credential", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"machine_id":       machineID,
			"boot_command":     apiAnyString,
			"token":            apiAnyString,
			"expires_in":       0,
			"claim_code":       apiAnyString,
			"claim_expires_in": 600,
		})
		claimCode, _ := data["claim_code"].(string)
		wantCommand := "curl -fsSL 'https://example.com/install.sh?code=" + claimCode + "' | bash"
		if got, _ := data["boot_command"].(string); got != wantCommand {
			t.Fatalf("boot_command: want %q, got %q", wantCommand, got)
		}
		dashboard.wantFrames()
	})

	t.Run("a residual uninstall intent is zeroed before the fresh one-liner is handed out", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		dashboard.wantFrames(map[string]any{
			"seq":   3,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{
					"id":            machineID,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		apiTestWantDesiredState(t, d, machineID, "offline")
	})

	t.Run("a machine id nothing carries answers 404 naming it and mints nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/nope/boot-command", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/machines/m-server-self/boot-command", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

// apiTestWantDesiredState asserts the durable machine intent the roster now
// carries — the half of a machine-lifecycle write that never reaches the wire.
func apiTestWantDesiredState(t *testing.T, d *DAL, machineID, want string) {
	t.Helper()
	m, err := d.GetMember(machineID)
	if err != nil {
		t.Fatalf("GetMember(%q): %v", machineID, err)
	}
	if m == nil {
		t.Fatalf("GetMember(%q): no such row", machineID)
	}
	if m.DesiredState != want {
		t.Fatalf("desired_state: want %q, got %q", want, m.DesiredState)
	}
}

// apiTestWantRosterStatus is apiTestWantDesiredState for the other half a
// teardown would move: whether the row is still on the roster at all.
func apiTestWantRosterStatus(t *testing.T, d *DAL, machineID, want string) {
	t.Helper()
	m, err := d.GetMember(machineID)
	if err != nil {
		t.Fatalf("GetMember(%q): %v", machineID, err)
	}
	if m == nil {
		t.Fatalf("GetMember(%q): no such row", machineID)
	}
	if m.RosterStatus != want {
		t.Fatalf("roster_status: want %q, got %q", want, m.RosterStatus)
	}
}

func TestHandleClaimMachineTokenApiMachinesClaimPost(t *testing.T) {
	t.Run("a fresh claim code is exchanged for the machine's credential by a caller carrying no identity", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		_, onboarded := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", owner, "")
		code, _ := onboarded["claim_code"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{"code":"`+code+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"expires_in": 0,
			"machine_id": machineID,
		})
		dashboard.wantFrames()
	})

	t.Run("the same code a second time answers the flat refusal", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		_, onboarded := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", owner, "")
		code, _ := onboarded["claim_code"].(string)
		apiJSON(t, h, "POST", "/api/machines/claim", "", `{"code":"`+code+`"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{"code":"`+code+`"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", apiTestClaimDeniedMsg)
		dashboard.wantFrames()
	})

	t.Run("a code nothing ever minted is refused in the same words as a used one", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{"code":"nothing-was-ever-minted-here"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", apiTestClaimDeniedMsg)
	})

	t.Run("a body naming no code answers 422", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: code")
	})

	t.Run("a body that is not JSON answers 422 without reaching the redemption", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
	})
}

// apiTestClaimDeniedMsg is the single sentence every failed redemption gets —
// unknown, expired and already-used codes are indistinguishable on the wire.
const apiTestClaimDeniedMsg = "claim code is invalid, expired, or already used — " +
	"fetch a fresh boot command from the cockpit"

func TestHandleRenewMachineCredentialApiMachinesRenewCredentialPost(t *testing.T) {
	t.Run("a warden trades its own credential for a fresh one bound to the same machine", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		status, onboarded := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"Studio Mac"}`)
		if status != 200 {
			t.Fatalf("onboard: %d %v", status, onboarded)
		}
		machineID, _ := onboarded["machine_id"].(string)
		held, _ := onboarded["token"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/renew-credential", held, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"expires_in": 0,
			"machine_id": machineID,
		})
		renewed, _ := data["token"].(string)
		if again, body := apiJSON(t, h, "GET", "/api/machines", renewed, ""); again != 200 {
			t.Fatalf("the renewed credential must open the machine roster, got %d (%v)", again, body)
		}
		dashboard.wantFrames()
	})

	t.Run("an ordinary agent identity answers 403 because it is not a machine", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/renew-credential", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", apiTestRenewNotAWardenMsg)
	})

	t.Run("the owner answers the same 403 because this row acts on the caller alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/renew-credential", owner, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", apiTestRenewNotAWardenMsg)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/renew-credential", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

const apiTestRenewNotAWardenMsg = "only a machine credential can be renewed here; " +
	"this endpoint acts on the caller and takes no target"

func TestHandleMachineCredentialPolicyApiMachinesCredentialPolicyGet(t *testing.T) {
	t.Run("the station answers its shipped machine-credential lifetime", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/credential-policy", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"lifetime_secs": 2592000})
		dashboard.wantFrames()
	})

	t.Run("a lifetime the owner lowered is what the next poll reads", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"warden_credential_lifetime_secs":86400}`); status != 200 {
			t.Fatalf("settings patch: %d %v", status, data)
		}

		status, data := apiJSON(t, h, "GET", "/api/machines/credential-policy", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"lifetime_secs": 86400})
	})

	t.Run("an ordinary agent identity reads the same number", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/credential-policy", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"lifetime_secs": 2592000})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/machines/credential-policy", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestOcwardenChildEnv(t *testing.T) {
	t.Skip("TODO: ocwardenChildEnv projects `environ` down to ocwardenChildEnvAllowlist.")
}

func TestExecOcwarden(t *testing.T) {
	t.Skip("TODO: execOcwarden runs `<ocwarden> <verb>` bounded by 60s (the injectable-runner twins of handlers._default_bootstrap_runner / _default_teardown_runner).")
}

func TestResolveOcwardenBinary(t *testing.T) {
	t.Run("an embedded warden is materialized into the binary cache", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cache := t.TempDir()
		api.binCacheDir = cache
		api.ocwardenFS = fstest.MapFS{
			"ocwarden":  &fstest.MapFile{Data: []byte("warden bytes")},
			"officraft": &fstest.MapFile{Data: []byte("anchor bytes")},
		}
		rec := httptest.NewRecorder()

		path, ok := api.resolveOcwardenBinary(rec)

		if !ok {
			t.Fatalf("want ok, got %d (%s)", rec.Code, rec.Body.String())
		}
		if path != cache+"/ocwarden" {
			t.Fatalf("path: %q", path)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("nothing may be written on success, got %s", rec.Body.String())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(data) != "warden bytes" {
			t.Fatalf("materialized: %q", data)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("mode: %v", info.Mode().Perm())
		}
	})

	t.Run("a build carrying no embedded warden answers 503", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.binCacheDir = t.TempDir()
		api.ocwardenFS = fstest.MapFS{}
		rec := httptest.NewRecorder()

		path, ok := api.resolveOcwardenBinary(rec)

		if ok || path != "" {
			t.Fatalf("want a refusal, got %q", path)
		}
		if rec.Code != 503 {
			t.Fatalf("want 503, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, map[string]any{
			"error": map[string]any{
				"code":    "service_unavailable",
				"message": "ocwarden binary is not available (no embedded copy in this server build): open ocwarden: file does not exist",
			},
		})
	})
}

func TestResolveOcwardenBinaryFrom(t *testing.T) {
	t.Skip("TODO: resolveOcwardenBinaryFrom is resolveOcwardenBinary over an injectable embedded FS (tests pass fstest.MapFS; production passes bindistFS()).")
}

func TestBootstrapHereForeignTargetMsg(t *testing.T) {
	t.Skip("TODO: bootstrapHereForeignTargetMsg is the refusal bootstrap-here owes a caller who named a machine other than this server's own.")
}

func TestBootstrapHereRefusal(t *testing.T) {
	t.Skip("TODO: bootstrapHereRefusal answers \"what does bootstrap-here owe a caller who named this machine?\" and returns \"\" when the target may proceed.")
}

func TestHandleBootstrapHereApiMachinesMachineIdBootstrapHerePost(t *testing.T) {
	t.Run("the server-local machine clears a residual uninstall intent and then reports the missing embedded binary", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		apiTestListen(t, api, ServerSelfHost)
		apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/uninstall", owner, `{}`)
		apiTestWantDesiredState(t, d, ServerSelfHost, "uninstall")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/bootstrap-here", owner, `{}`)
		if status != 503 {
			t.Fatalf("want 503, got %d (%v)", status, data)
		}
		apiWantError(t, data, "service_unavailable",
			"ocwarden binary is not available (no embedded copy in this server build): open ocwarden: file does not exist")
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + ServerSelfHost,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            ServerSelfHost,
					"name":          "伺服器這一台",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		apiTestWantDesiredState(t, d, ServerSelfHost, "offline")
	})

	t.Run("a machine other than the server's own is refused and its uninstall intent survives untouched", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/bootstrap-here", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"bootstrap-here only ever installs the warden running on THIS server host — "+
				"it carries no machine selector, so it cannot reach "+machineID+
				"; refusing rather than overwriting this host's warden with another "+
				"machine's identity. To install a different machine, fetch its own "+
				"one-liner with GET /api/machines/{member_id}/boot-command and run it "+
				"on that host.")
		dashboard.wantFrames()
		apiTestWantDesiredState(t, d, machineID, "uninstall")
	})

	t.Run("a machine id nothing carries answers 404 naming it and installs nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/nope/bootstrap-here", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/bootstrap-here", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/bootstrap-here", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestRunWardenInstallHere(t *testing.T) {
	t.Skip("TODO: runWardenInstallHere is the bootstrap-here CORE, split out (T-ba62) so the automatic first-run onboarding can install this host's warden through the EXACT same path the cockpit button uses — one implementation, one set of semantics, no second copy to drift.")
}

func TestRunWardenTeardownHere(t *testing.T) {
	t.Skip("TODO: runWardenTeardownHere is the teardown-here CORE — the exact twin of runWardenInstallHere, split out for the SAME reason: the env this builds is the whole safety story of the verb, and it has to be reachable by a test without an HTTP recorder, an embedded bindist, or a real launchd domain.")
}

func TestTeardownHereForeignTargetMsg(t *testing.T) {
	t.Skip("TODO: teardownHereForeignTargetMsg is the refusal for the defect T-42a0 exists to close: `teardown-here` NEVER consumed the {machine_id} it was handed.")
}

func TestTeardownHereRefusal(t *testing.T) {
	t.Skip("TODO: teardownHereRefusal answers ONE question — \"what does teardown-here owe a caller who named this machine?\" — and returns \"\" when the target may proceed.")
}

func TestHandleTeardownHereApiMachinesMachineIdTeardownHerePost(t *testing.T) {
	t.Run("the server-local machine is refused as undeletable and stays on the roster", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/teardown-here", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "the server-local machine cannot be deleted")
		dashboard.wantFrames()
		apiTestWantRosterStatus(t, d, ServerSelfHost, "active")
		apiTestWantDesiredState(t, d, ServerSelfHost, "offline")
	})

	t.Run("a machine other than the server's own is refused and sent to the retire verbs instead", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/teardown-here", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"teardown-here only ever tears down the warden running on THIS server host — "+
				"it carries no machine selector, so it cannot reach "+machineID+
				"; refusing rather than destroying this host's daemon under another "+
				"machine's name. To retire a different machine, use POST "+
				"/api/machines/{member_id}/uninstall (the remote uninstall the target's "+
				"own warden executes) and then DELETE /api/machines/{member_id}. To "+
				"repair this host's own warden, use install_warden_on_server_host — it "+
				"runs `ocwarden install --force`, which overwrites an existing install, "+
				"so nothing has to be torn down first.")
		dashboard.wantFrames()
		apiTestWantRosterStatus(t, d, machineID, "active")
	})

	t.Run("a machine id nothing carries answers 404 naming it and tears nothing down", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/nope/teardown-here", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/teardown-here", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiTestWantRosterStatus(t, d, ServerSelfHost, "active")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/teardown-here", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUninstallMachineApiMachinesMemberIdUninstallPost(t *testing.T) {
	t.Run("uninstalling a machine that is connected arms the uninstall intent and fans it to the dashboard and to that machine", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		self := apiTestListen(t, api, machineID)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"dispatched": true,
		})
		frame := map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            machineID,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "uninstall",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("uninstalling a machine nothing is connected from dispatches nothing and leaves the intent offline", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"dispatched": false,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            machineID,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a machine id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/nope/uninstall", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/m-server-self/uninstall", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func apiTestOnboardMachine(t *testing.T, h http.Handler, owner, displayName string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"`+displayName+`"}`)
	if status != 200 {
		t.Fatalf("onboard machine: %d %v", status, data)
	}
	id, _ := data["machine_id"].(string)
	if id == "" {
		t.Fatalf("onboard machine must mint a machine id: %v", data)
	}
	return id
}

func TestHandleUpgradeMachineApiMachinesMemberIdUpgradePost(t *testing.T) {
	t.Run("a machine holding a live downstream is handed the update verb and nothing durable is written", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/upgrade", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"dispatched": true,
		})
		apiTestWantWardenCommands(t, api, machineID,
			`data: {"topic":"warden-command","data":{"rpc":"update","args":{"member_id":"`+machineID+`"}}}`+"\n\n")
		dashboard.wantFrames()
		apiTestWantDesiredState(t, d, machineID, "offline")
	})

	t.Run("a machine with no live downstream is dispatched nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/upgrade", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"dispatched": false,
		})
		apiTestWantWardenCommands(t, api, machineID)
		dashboard.wantFrames()
		apiTestWantDesiredState(t, d, machineID, "offline")
	})

	t.Run("a machine id nothing carries answers 404 naming it and enqueues nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/nope/upgrade", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		apiTestWantWardenCommands(t, api, "nope")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/upgrade", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiTestWantWardenCommands(t, api, machineID)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/upgrade", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWardenCommands(t, api, ServerSelfHost)
	})
}

// apiTestWantWardenCommands drains machineID's warden command FIFO — the same
// queue the machine's SSE loop collects from — and asserts it held exactly the
// frames named, in order. Passing none asserts nothing was enqueued at all.
func apiTestWantWardenCommands(t *testing.T, api *apiServer, machineID string, want ...string) {
	t.Helper()
	pending := api.hub.DrainWardenCommands(machineID)
	if len(pending) != len(want) {
		t.Fatalf("warden commands: want %d, got %d (%q)", len(want), len(pending), pending)
	}
	for i, w := range want {
		if got := string(pending[i].Frame); got != w {
			t.Fatalf("warden command %d: want %q, got %q", i, w, got)
		}
		if pending[i].Subject != machineID {
			t.Fatalf("warden command %d subject: want %q, got %q", i, machineID, pending[i].Subject)
		}
	}
}

func TestHandleDeleteMachineApiMachinesMemberIdDelete(t *testing.T) {
	t.Run("deleting a machine answers the removal and fans a member removal carrying no payload", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, machineID)
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "DELETE", "/api/machines/"+machineID, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"removed":    true,
		})
		frame := map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "remove",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   2,
				"deleted": true,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a member that is not a warden answers 409 naming the kind it is and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/machines/kip", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "member 'kip' is not a warden machine (kind='staff')")
		dashboard.wantFrames()
	})

	t.Run("the server-local machine answers 409 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/machines/m-server-self", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "the server-local machine cannot be deleted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "DELETE", "/api/machines/m-server-self", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateAccountApiAccountsAccountIdPatch(t *testing.T) {
	t.Run("an overlay over a compound account tag answers the alias and is what the station stores", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/claude%2F9f2c", owner, `{"display_name":"  Studio plan  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":             "claude/9f2c",
			"display_name":   "Studio plan",
			"owner_id":       "owner",
			"schema_version": 3,
		})
		apiTestWantAccountAliases(t, d, map[string]string{"claude/9f2c": "Studio plan"})
		dashboard.wantFrames()
	})

	t.Run("an ordinary agent identity may set one because this row's floor is machine", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", agent, `{"display_name":"Bench"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":             "codex",
			"display_name":   "Bench",
			"owner_id":       "owner",
			"schema_version": 3,
		})
		apiTestWantAccountAliases(t, d, map[string]string{"codex": "Bench"})
	})

	t.Run("a body naming no display name answers 422 and stores nothing", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name is required")
		apiTestWantAccountAliases(t, d, map[string]string{})
	})

	t.Run("a display name that is only whitespace answers 422 and stores nothing", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", owner, `{"display_name":"   "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name cannot be blank")
		apiTestWantAccountAliases(t, d, map[string]string{})
	})

	t.Run("a body carrying a field the shape does not declare answers 422 and stores nothing", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", owner, `{"display_name":"Bench","note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `invalid request body: json: unknown field "note"`)
		apiTestWantAccountAliases(t, d, map[string]string{})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", "", `{"display_name":"Bench"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantAccountAliases(t, d, map[string]string{})
	})
}

// apiTestWantAccountAliases asserts the whole stored account-overlay table —
// the durable half of this row's answer, which reaches no other endpoint.
func apiTestWantAccountAliases(t *testing.T, d *DAL, want map[string]string) {
	t.Helper()
	got, err := d.AccountDisplayNames()
	if err != nil {
		t.Fatalf("AccountDisplayNames: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("account aliases: want %v, got %v", want, got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("account alias %q: want %q, got %q", k, v, got[k])
		}
	}
}

func TestHandleUpdateMachineApiMachinesMachineIdPatch(t *testing.T) {
	t.Run("an overlay answers the alias and is the name the machine roster then shows", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, owner, `{"display_name":"  Loft mini  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":             ServerSelfHost,
			"display_name":   "Loft mini",
			"owner_id":       "owner",
			"schema_version": 3,
		})
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "Loft mini")
		dashboard.wantFrames()
	})

	t.Run("an overlay may name a machine the roster does not carry", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/m-never-onboarded", owner, `{"display_name":"Spare"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":             "m-never-onboarded",
			"display_name":   "Spare",
			"owner_id":       "owner",
			"schema_version": 3,
		})
		names, err := d.MachineDisplayNames()
		if err != nil {
			t.Fatalf("MachineDisplayNames: %v", err)
		}
		if names["m-never-onboarded"] != "Spare" {
			t.Fatalf("stored overlays: %v", names)
		}
	})

	t.Run("a body naming no display name answers 422 and leaves the roster name alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name is required")
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "伺服器這一台")
	})

	t.Run("a display name that is only whitespace answers 422 and leaves the roster name alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, owner, `{"display_name":"   "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name cannot be blank")
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "伺服器這一台")
	})

	t.Run("a body carrying a field the shape does not declare answers 422 and leaves the roster name alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, owner, `{"display_name":"Loft","note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `invalid request body: json: unknown field "note"`)
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "伺服器這一台")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, "", `{"display_name":"Loft"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "伺服器這一台")
	})
}

// apiTestWantMachineDisplayName reads the overlay back the way the cockpit
// does — through the machine roster, where the alias folds over the member
// name — so a stored overlay is observed on the surface that consumes it.
func apiTestWantMachineDisplayName(t *testing.T, h http.Handler, owner, machineID, want string) {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
	if rec.Code != 200 {
		t.Fatalf("list machines: %d (%s)", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	for _, row := range rows {
		if row["machine_id"] == machineID {
			if row["display_name"] != want {
				t.Fatalf("display_name: want %q, got %q", want, row["display_name"])
			}
			return
		}
	}
	t.Fatalf("machine %q is not in the roster: %v", machineID, rows)
}

func TestHandleInstallScriptInstallShGet(t *testing.T) {
	t.Run("the claim-code installer is served verbatim as plain text to a caller carrying no identity", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		rec := apiRequest(t, h, "GET", "/install.sh?code=one-time-claim-code-placeholder", "", "")

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Fatalf("content-type: %q", got)
		}
		if got := rec.Body.String(); got != apiTestClaimCodeInstaller {
			t.Fatalf("script:\n%s", got)
		}
	})

	t.Run("the legacy query-parameter installer is served verbatim", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		rec := apiRequest(t, h, "GET", "/install.sh?token=legacy-credential-placeholder", "", "")

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Fatalf("content-type: %q", got)
		}
		if got := rec.Body.String(); got != apiTestLegacyInstaller {
			t.Fatalf("script:\n%s", got)
		}
	})

	t.Run("naming neither parameter answers 422", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/install.sh", "", "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "exactly one of ?code= or ?token= is required")
	})

	t.Run("naming both parameters answers the same 422", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h,
			"GET", "/install.sh?code=one-time-claim-code-placeholder&token=legacy-credential-placeholder", "", "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "exactly one of ?code= or ?token= is required")
	})
}

const apiTestClaimCodeInstaller = `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL 'https://example.com/install.sh?code=<one-time code>' | bash
set -euo pipefail

# Precheck: only the KEY tools the install truly needs (not an exhaustive audit).
#   tmux — the warden spawns each member's session through it (auto-installed
#          via Homebrew when available).
#   curl — claims the machine token just below, then pulls the ocwarden binary.
#   sed  — extracts the token from the claim response JSON.
for tool in tmux curl sed; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  # tmux is the one tool worth auto-installing: Homebrew boxes get it hands-free.
  if [ "$tool" = tmux ] && command -v brew >/dev/null 2>&1; then
    echo "tmux not found — installing via Homebrew..."
    brew install tmux || true
    if command -v tmux >/dev/null 2>&1; then
      continue
    fi
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Probe the warden binary availability BEFORE redeeming the one-time claim
# code — a server that cannot serve the binary (503) must not burn the code.
if ! curl -fsI "https://example.com/api/warden/binary" >/dev/null 2>&1; then
  echo "Error: the server cannot serve the warden binary (https://example.com/api/warden/binary is unavailable)." >&2
  echo "Fix: redeploy the server with the prebuilt binaries (bin/ocwarden) or an embed-carrying build, then re-run this one-liner — the install code was NOT consumed." >&2
  exit 1
fi

# Exchange the ONE-TIME claim code for this machine's real exec-token FIRST —
# before any download — so an expired/used install link fails at the earliest
# possible point. The code is single-use: a replayed one-liner lands here.
if ! CLAIM_RESPONSE="$(curl -fsS -X POST "https://example.com/api/machines/claim" \
  -H 'Content-Type: application/json' --data '{"code":"one-time-claim-code-placeholder"}')"; then
  echo "Error: this install link has expired or was already used." >&2
  echo "Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner." >&2
  exit 1
fi
# The token is a base64url JWT — no quote/backslash can appear inside it.
OC_TOKEN="$(printf '%s' "$CLAIM_RESPONSE" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
if [ -z "$OC_TOKEN" ]; then
  echo "Error: this install link has expired or was already used." >&2
  echo "Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner." >&2
  exit 1
fi

# Pull the prebuilt ocwarden binary from the PUBLIC binary endpoint (no auth
# header needed — the claimed token authorizes the install, not this fetch).
curl -fsSL "https://example.com/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
OC_BASE="https://example.com" OC_TOKEN="$OC_TOKEN" ./ocwarden install --force
`

const apiTestLegacyInstaller = `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL 'https://example.com/install.sh?token=<jwt>' | bash
set -euo pipefail

# Precheck: only the KEY tools the install truly needs (not an exhaustive audit).
#   tmux — the warden spawns each member's session through it (auto-installed
#          via Homebrew when available).
#   curl — used just below to pull the ocwarden binary.
for tool in tmux curl; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  # tmux is the one tool worth auto-installing: Homebrew boxes get it hands-free.
  if [ "$tool" = tmux ] && command -v brew >/dev/null 2>&1; then
    echo "tmux not found — installing via Homebrew..."
    brew install tmux || true
    if command -v tmux >/dev/null 2>&1; then
      continue
    fi
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Pull the prebuilt ocwarden binary from the PUBLIC binary endpoint (no auth
# header needed — the boot token authorizes the install, not this fetch).
curl -fsSL "https://example.com/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
OC_BASE="https://example.com" OC_TOKEN="legacy-credential-placeholder" ./ocwarden install --force
`

func TestServeBinary(t *testing.T) {
	t.Run("the embedded copy is streamed as an attachment", func(t *testing.T) {
		embedded := fstest.MapFS{"ocwarden": &fstest.MapFile{Data: []byte("warden bytes")}}
		rec := httptest.NewRecorder()

		serveBinary(rec, httptest.NewRequest("GET", "/api/warden/binary", nil), "ocwarden", embedded)

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Body.String(); got != "warden bytes" {
			t.Fatalf("body: %q", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
			t.Fatalf("content-type: %q", got)
		}
		if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="ocwarden"` {
			t.Fatalf("content-disposition: %q", got)
		}
	})

	t.Run("a build carrying no embedded copy answers 503", func(t *testing.T) {
		rec := httptest.NewRecorder()

		serveBinary(rec, httptest.NewRequest("GET", "/api/warden/binary", nil), "ocwarden", fstest.MapFS{})

		if rec.Code != 503 {
			t.Fatalf("want 503, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, map[string]any{
			"error": map[string]any{
				"code":    "service_unavailable",
				"message": "ocwarden binary is not available (no embedded copy in this server build)",
			},
		})
		if got := rec.Header().Get("Content-Disposition"); got != "" {
			t.Fatalf("content-disposition: %q", got)
		}
	})
}

func TestHandleWardenBinaryApiWardenBinaryGet(t *testing.T) {
	t.Run("the prebuilt warden is streamed as an attachment to a caller carrying no identity", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		apiTestWantBinaryDownload(t, apiRequest(t, h, "GET", "/api/warden/binary", "", ""), "ocwarden")
	})
}

// apiTestWantBinaryDownload asserts the whole download envelope for a prebuilt
// artifact. The bytes themselves are whatever this build staged, so what is
// pinned is that they arrived intact and are declared as a download of exactly
// this file.
func apiTestWantBinaryDownload(t *testing.T, rec *httptest.ResponseRecorder, filename string) {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("content-type: %q", got)
	}
	if got, want := rec.Header().Get("Content-Disposition"), `attachment; filename="`+filename+`"`; got != want {
		t.Fatalf("content-disposition: want %q, got %q", want, got)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("accept-ranges: %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Fatalf("%s was served empty", filename)
	}
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(rec.Body.Len()); got != want {
		t.Fatalf("content-length: want %q, got %q", want, got)
	}
}

func TestHandleAgentBinaryApiAgentBinaryGet(t *testing.T) {
	t.Run("the prebuilt agent is streamed as an attachment to a caller carrying no identity", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		apiTestWantBinaryDownload(t, apiRequest(t, h, "GET", "/api/agent/binary", "", ""), "ocagent")
	})
}
