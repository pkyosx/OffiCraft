// Skeleton generated from server/ocserverd/api_machines.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	t.Run("a well-formed GET /api/machines answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/machines request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/machines reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/machines request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed GET /api/machines/{machine_id}/boot-command answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/machines/{machine_id}/boot-command request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/machines/{machine_id}/boot-command reaches this handler with machine_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/machines/{machine_id}/boot-command request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleClaimMachineTokenApiMachinesClaimPost(t *testing.T) {
	t.Run("a well-formed POST /api/machines/claim answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/machines/claim reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/claim request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleRenewMachineCredentialApiMachinesRenewCredentialPost(t *testing.T) {
	t.Run("a well-formed POST /api/machines/renew-credential answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/renew-credential request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/machines/renew-credential reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/renew-credential request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleMachineCredentialPolicyApiMachinesCredentialPolicyGet(t *testing.T) {
	t.Run("a well-formed GET /api/machines/credential-policy answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/machines/credential-policy request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/machines/credential-policy reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/machines/credential-policy request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed POST /api/machines/{machine_id}/bootstrap-here answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/{machine_id}/bootstrap-here request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/machines/{machine_id}/bootstrap-here reaches this handler with machine_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/{machine_id}/bootstrap-here request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed POST /api/machines/{machine_id}/teardown-here answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/{machine_id}/teardown-here request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/machines/{machine_id}/teardown-here reaches this handler with machine_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/{machine_id}/teardown-here request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed POST /api/machines/{member_id}/upgrade answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/{member_id}/upgrade request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/machines/{member_id}/upgrade reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/machines/{member_id}/upgrade request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed PATCH /api/accounts/{account_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PATCH /api/accounts/{account_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to PATCH /api/accounts/{account_id} reaches this handler with account_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PATCH /api/accounts/{account_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleUpdateMachineApiMachinesMachineIdPatch(t *testing.T) {
	t.Run("a well-formed PATCH /api/machines/{machine_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PATCH /api/machines/{machine_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to PATCH /api/machines/{machine_id} reaches this handler with machine_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PATCH /api/machines/{machine_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleInstallScriptInstallShGet(t *testing.T) {
	t.Run("a well-formed GET /install.sh answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /install.sh reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /install.sh request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

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
	t.Run("a well-formed GET /api/warden/binary answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/warden/binary reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/warden/binary request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleAgentBinaryApiAgentBinaryGet(t *testing.T) {
	t.Run("a well-formed GET /api/agent/binary answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/agent/binary reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/agent/binary request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
