// Skeleton generated from server/ocserverd/api_stub.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestHealth(t *testing.T) {
	t.Run("the shared body writer answers 200, one JSON object saying ok, and a JSON content type", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()

		api.health(rec)

		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantValue(t, "body", any(rec.Body.String()), any(`{"status":"ok"}`))
		apiWantValue(t, "content type", any(rec.Header().Get("Content-Type")), any("application/json"))
	})
}

func TestHandleHealthHealthGet(t *testing.T) {
	t.Run("the deploy liveness probe answers ok to a caller carrying no credentials at all", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/health", "", "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		dashboard.wantFrames()
	})

	t.Run("an owner credential is served the identical answer, because the row is public rather than merely open", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/health", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
	})

	t.Run("another method on /health is refused 405 instead of falling through to some other row", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/health", "", "")
		if status != 405 {
			t.Fatalf("want 405, got %d (%v)", status, data)
		}
		apiWantError(t, data, "method_not_allowed", "method not allowed")
	})

	t.Run("an ignored request body does not change the public liveness response", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/health", "", "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
	})
}

func TestHandleHealthApiHealthGet(t *testing.T) {
	t.Run("the api liveness probe answers ok to a caller carrying no credentials at all", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/health", "", "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		dashboard.wantFrames()
	})

	t.Run("the api probe and the deploy probe answer the same body, so an ops check may use either", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		_, viaAPI := apiJSON(t, h, "GET", "/api/health", "", "")
		_, viaDeploy := apiJSON(t, h, "GET", "/health", "", "")
		apiWantBody(t, viaAPI, map[string]any{"status": "ok"})
		apiWantValue(t, "deploy probe", any(viaDeploy), any(viaAPI))
	})

	t.Run("another method on /api/health is refused 405 instead of falling through to some other row", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/health", "", "")
		if status != 405 {
			t.Fatalf("want 405, got %d (%v)", status, data)
		}
		apiWantError(t, data, "method_not_allowed", "method not allowed")
	})

	t.Run("an ignored request body does not change the API liveness response", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/health", "", "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
	})
}

func TestHandleVersionApiVersionGet(t *testing.T) {
	t.Run("a station that has never reached GitHub reports its build identity with no newer version known and no successful-check stamp", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/version", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"version":          "0.0.0",
			"git_sha":          apiAnyString,
			"git_time":         apiAnyString,
			"catalog_hash":     apiAnyString,
			"update_available": false,
			"latest_version":   nil,
		})
		dashboard.wantFrames()
	})

	t.Run("the row is public: a caller with no credentials is served the same build identity as the owner", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		_, anonymous := apiJSON(t, h, "GET", "/api/version", "", "")
		_, asOwner := apiJSON(t, h, "GET", "/api/version", owner, "")
		apiWantValue(t, "anonymous", any(anonymous), any(asOwner))
	})

	t.Run("another method on /api/version is refused 405 instead of falling through to some other row", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/version", "", "")
		if status != 405 {
			t.Fatalf("want 405, got %d (%v)", status, data)
		}
		apiWantError(t, data, "method_not_allowed", "method not allowed")
	})

	t.Run("an ignored request body does not change the public build identity", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/version", "", "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"version":          "0.0.0",
			"git_sha":          apiAnyString,
			"git_time":         apiAnyString,
			"catalog_hash":     apiAnyString,
			"update_available": false,
			"latest_version":   nil,
		})
	})
}

func TestHandleProbeVersionVersionGet(t *testing.T) {
	t.Run("the deploy probe carries only the three fields an autodeploy compares, and nothing about updates", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/version", "", "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"version":      "0.0.0",
			"sha":          apiAnyString,
			"catalog_hash": apiAnyString,
		})
		dashboard.wantFrames()
	})

	t.Run("the deploy probe and /api/version describe the SAME build, so a sha compare against either settles the same question", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		_, probe := apiJSON(t, h, "GET", "/version", "", "")
		_, full := apiJSON(t, h, "GET", "/api/version", owner, "")
		apiWantBody(t, probe, map[string]any{
			"version":      full["version"],
			"sha":          full["git_sha"],
			"catalog_hash": full["catalog_hash"],
		})
	})

	t.Run("another method on /version is refused 405 instead of falling through to some other row", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/version", "", "")
		if status != 405 {
			t.Fatalf("want 405, got %d (%v)", status, data)
		}
		apiWantError(t, data, "method_not_allowed", "method not allowed")
	})

	t.Run("an ignored request body does not change the public deploy identity", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/version", "", "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"version":      "0.0.0",
			"sha":          apiAnyString,
			"catalog_hash": apiAnyString,
		})
	})
}

// asPatchSettings drives one settings PATCH as the owner and fails loudly, so a
// live-read scenario cannot silently assert against a refused write.
func asPatchSettings(t *testing.T, h http.Handler, owner, body string) {
	t.Helper()
	status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, body)
	if status != 200 {
		t.Fatalf("PATCH /api/settings %s: %d (%v)", body, status, data)
	}
}

func TestAuthPasswordHash(t *testing.T) {
	t.Run("a first-run server that nobody has claimed holds no password hash at all", func(t *testing.T) {
		api, _, _, _ := newAPITestStack(t)

		apiWantValue(t, "password hash", any(api.authPasswordHash()), any(""))
	})

	t.Run("a claimed server serves the hash the claim stored, and a password change replaces it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)

		stored, err := d.GetSetting(settingPasswordHash)
		if err != nil {
			t.Fatalf("GetSetting: %v", err)
		}
		if stored == nil || *stored == "" {
			t.Fatalf("the claim must have stored a hash, got %v", stored)
		}
		apiWantValue(t, "password hash", any(api.authPasswordHash()), any(*stored))

		status, data := apiJSON(t, h, "POST", "/api/auth/change-password", owner,
			`{"current_password":"`+apiTestOwnerPassword+`","new_password":"another-good-passphrase"}`)
		if status != 200 {
			t.Fatalf("change-password: %d (%v)", status, data)
		}
		changed, err := d.GetSetting(settingPasswordHash)
		if err != nil {
			t.Fatalf("GetSetting: %v", err)
		}
		apiWantValue(t, "password hash after the change", any(api.authPasswordHash()), any(*changed))
		if *changed == *stored {
			t.Fatalf("a password change must replace the hash, both are %q", *stored)
		}
	})
}

func TestAuthMFAOffered(t *testing.T) {
	t.Run("the second factor ships dark: a freshly claimed server does not offer it", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "mfa offered", any(api.authMFAOffered()), any(false))
	})

	t.Run("the owner's rollout switch flips it on and back off, and the accessor reads the live value each time", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		if status != 200 {
			t.Fatalf("offer: %d (%v)", status, data)
		}
		apiWantValue(t, "mfa offered", any(api.authMFAOffered()), any(true))

		status, data = apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":false}`)
		if status != 200 {
			t.Fatalf("withdraw: %d (%v)", status, data)
		}
		apiWantValue(t, "mfa offered", any(api.authMFAOffered()), any(false))
	})

	t.Run("arming a factor does NOT turn the set-up flag on, because the flag is about set-up and not about verification", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		apiTestArmMFA(t, api, d)

		apiWantValue(t, "mfa offered", any(api.authMFAOffered()), any(false))
		apiWantValue(t, "mfa enrolled", any(api.authMFAEnrolled()), any(true))
	})
}

func TestAuthMFAEnrolled(t *testing.T) {
	t.Run("nothing is armed on a first-run server or on a freshly claimed one", func(t *testing.T) {
		firstRun, _, _, _ := newAPITestStack(t)
		apiWantValue(t, "mfa enrolled on a first-run server", any(firstRun.authMFAEnrolled()), any(false))

		claimed, _, _, _ := newAPITestServer(t)
		apiWantValue(t, "mfa enrolled on a claimed server", any(claimed.authMFAEnrolled()), any(false))
	})

	t.Run("activating a factor arms it and disabling it disarms it again, both read live", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`); status != 200 {
			t.Fatalf("offer: %d (%v)", status, data)
		}
		status, enrolled := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		if status != 200 {
			t.Fatalf("enroll: %d (%v)", status, enrolled)
		}
		secret, _ := enrolled["secret"].(string)
		apiWantValue(t, "mfa enrolled while only a pending secret exists", any(api.authMFAEnrolled()), any(false))

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)
		if status != 200 {
			t.Fatalf("activate: %d (%v)", status, data)
		}
		apiWantValue(t, "mfa enrolled", any(api.authMFAEnrolled()), any(true))

	})

	t.Run("disabling an armed factor disarms it again, read live", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		secret := apiTestArmMFA(t, api, d)
		apiWantValue(t, "mfa enrolled", any(api.authMFAEnrolled()), any(true))

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)
		if status != 200 {
			t.Fatalf("disable: %d (%v)", status, data)
		}
		apiWantValue(t, "mfa enrolled", any(api.authMFAEnrolled()), any(false))
	})
}

func TestAuthPasswordChangedAt(t *testing.T) {
	t.Run("no cut has ever been made on a first-run server, nor by the claim that sets the first password", func(t *testing.T) {
		firstRun, _, _, _ := newAPITestStack(t)
		apiWantValue(t, "token floor on a first-run server", any(float64(firstRun.authPasswordChangedAt())), any(0))

		claimed, _, _, _ := newAPITestServer(t)
		apiWantValue(t, "token floor after the claim", any(float64(claimed.authPasswordChangedAt())), any(0))
	})

	t.Run("changing the password cuts every older owner token, and the floor is the moment of the change", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/change-password", owner,
			`{"current_password":"`+apiTestOwnerPassword+`","new_password":"another-good-passphrase"}`)
		if status != 200 {
			t.Fatalf("change-password: %d (%v)", status, data)
		}

		stored, err := d.GetSetting(settingPasswordChangedAt)
		if err != nil {
			t.Fatalf("GetSetting: %v", err)
		}
		if stored == nil {
			t.Fatalf("the change must have stored a floor")
		}
		apiWantValue(t, "token floor", any(strconv.FormatInt(api.authPasswordChangedAt(), 10)), any(*stored))
		if api.authPasswordChangedAt() <= 0 {
			t.Fatalf("want a floor in the past, got %d", api.authPasswordChangedAt())
		}
	})
}

func TestOwnerTokenTTLValue(t *testing.T) {
	t.Run("the shipped owner-session lifetime is one day, and a patch takes effect on the next read with no restart", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "owner token ttl", any(float64(api.ownerTokenTTLValue())), any(86400))

		asPatchSettings(t, h, owner, `{"owner_token_ttl":43200}`)
		apiWantValue(t, "owner token ttl", any(float64(api.ownerTokenTTLValue())), any(43200))
	})
}

func TestAgentTokenTTLValue(t *testing.T) {
	t.Run("the shipped agent-session lifetime is one week, and a patch takes effect on the next read with no restart", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "agent token ttl", any(float64(api.agentTokenTTLValue())), any(604800))

		asPatchSettings(t, h, owner, `{"agent_token_ttl":2592000}`)
		apiWantValue(t, "agent token ttl", any(float64(api.agentTokenTTLValue())), any(2592000))
	})
}

func TestWardenCredLifetimeValue(t *testing.T) {
	t.Run("the shipped machine-credential lifetime is thirty days, and a patch takes effect on the next read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "machine credential lifetime", any(float64(api.wardenCredLifetimeValue())), any(2592000))

		asPatchSettings(t, h, owner, `{"warden_credential_lifetime_secs":864000}`)
		apiWantValue(t, "machine credential lifetime", any(float64(api.wardenCredLifetimeValue())), any(864000))
	})

	t.Run("a server assembled without the boot defaults is served the shipped default rather than a zero every warden would have to sanitise", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		api.wardenCredLifetimeSecs = 0
		apiWantValue(t, "machine credential lifetime", any(float64(api.wardenCredLifetimeValue())), any(2592000))

		api.wardenCredLifetimeSecs = -5
		apiWantValue(t, "machine credential lifetime", any(float64(api.wardenCredLifetimeValue())), any(2592000))
	})
}

func TestReconcileConfigLive(t *testing.T) {
	// asReconcileConfig is the whole config as a comparable value, so a field
	// the struct grows cannot slip past unnoticed.
	asReconcileConfig := func(cfg reconcileConfig) map[string]any {
		return map[string]any{
			"start_timeout": cfg.StartTimeout, "stop_grace": cfg.StopGrace,
			"stop_retry": cfg.StopRetry, "recycle_grace": cfg.RecycleGrace,
			"soft_offboard_grace": cfg.SoftOffboardGrace,
			"backoff_base":        cfg.BackoffBase, "backoff_cap": cfg.BackoffCap,
			"circuit_threshold": float64(cfg.CircuitThreshold),
			"circuit_cooldown":  cfg.CircuitCooldown,
			"zombie_confirm":    cfg.ZombieConfirmGrace,
		}
	}
	shipped := map[string]any{
		"start_timeout": 120.0, "stop_grace": 120.0, "stop_retry": 90.0,
		"recycle_grace": 120.0, "soft_offboard_grace": 600.0,
		"backoff_base": 5.0, "backoff_cap": 300.0,
		"circuit_threshold": 5.0, "circuit_cooldown": 120.0, "zombie_confirm": 240.0,
	}

	t.Run("a freshly booted server answers the shipped config, every number of it", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "reconcile config", any(asReconcileConfig(api.reconcileConfigLive())), any(shipped))
	})

	t.Run("the one owner-adjustable number is folded in fresh, and it moves ONLY the recycle grace", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"accelerated_grace_secs":90}`)

		want := map[string]any{}
		for key, value := range shipped {
			want[key] = value
		}
		want["recycle_grace"] = 90.0
		apiWantValue(t, "reconcile config", any(asReconcileConfig(api.reconcileConfigLive())), any(want))
	})

	t.Run("a zero adjustable number leaves the boot-time grace standing rather than publishing a zero deadline", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.acceleratedGraceSecs = 0

		apiWantValue(t, "reconcile config", any(asReconcileConfig(api.reconcileConfigLive())), any(shipped))
	})

	t.Run("the answer is a COPY, so writing to one call site's config cannot reach the next read", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		mine := api.reconcileConfigLive()
		mine.RecycleGrace = 9999
		mine.StartTimeout = 1

		apiWantValue(t, "reconcile config", any(asReconcileConfig(api.reconcileConfigLive())), any(shipped))
	})
}

func TestOutsourceParallelCap(t *testing.T) {
	t.Run("three workers ship as the concurrency cap, and a patch takes effect on the next read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "outsource parallel cap", any(float64(api.outsourceParallelCap())), any(3))

		asPatchSettings(t, h, owner, `{"outsource_max_parallel":7}`)
		apiWantValue(t, "outsource parallel cap", any(float64(api.outsourceParallelCap())), any(7))
	})

	t.Run("the uncapped setting is served verbatim as minus one, not normalised away", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"outsource_max_parallel":-1}`)
		apiWantValue(t, "outsource parallel cap", any(float64(api.outsourceParallelCap())), any(-1))
	})
}

// asDocCaps is every accumulating-document cap in one value, so a scenario that
// moves one has to say what happened to the other seven.
func asDocCaps(api *apiServer) map[string]any {
	return map[string]any{
		"duty": float64(api.dutyCap()), "insight": float64(api.insightCap()),
		"learning": float64(api.learningCap()), "manual_sop": float64(api.manualSopCap()),
		"manual_learnings":   float64(api.manualLearningsCap()),
		"system_interaction": float64(api.systemInteractionCap()),
		"boot_sequence":      float64(api.bootSequenceCap()),
		"offboard":           float64(api.offboardCap()),
	}
}

// asShippedDocCaps is the out-of-box answer of all eight.
func asShippedDocCaps() map[string]any {
	return map[string]any{
		"duty": 1000.0, "insight": 15000.0, "learning": 15000.0,
		"manual_sop": 15000.0, "manual_learnings": 15000.0,
		"system_interaction": 60000.0, "boot_sequence": 15000.0, "offboard": 15000.0,
	}
}

// asDocCapMoved is the shipped table with one entry replaced.
func asDocCapMoved(field string, value float64) map[string]any {
	want := asShippedDocCaps()
	want[field] = value
	return want
}

func TestDutyCap(t *testing.T) {
	t.Run("the duty document ships with the smallest cap of the eight", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asShippedDocCaps()))
	})

	t.Run("patching the duty cap moves that cap alone and takes effect on the next read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"doc_cap_chars_duty":2000}`)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asDocCapMoved("duty", 2000)))
	})
}

func TestInsightCap(t *testing.T) {
	t.Run("patching the insight cap moves that cap alone and takes effect on the next read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"doc_cap_chars_insight":20000}`)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asDocCapMoved("insight", 20000)))
	})
}

func TestLearningCap(t *testing.T) {
	t.Run("patching the learning cap moves that cap alone and takes effect on the next read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"doc_cap_chars_learning":20001}`)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asDocCapMoved("learning", 20001)))
	})
}

func TestManualSopCap(t *testing.T) {
	t.Run("patching the manual SOP cap moves that cap alone, leaving the manual's other half where it was", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"doc_cap_chars_manual_sop":20002}`)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asDocCapMoved("manual_sop", 20002)))
	})
}

func TestManualLearningsCap(t *testing.T) {
	t.Run("patching the manual learnings cap moves that cap alone, leaving the manual's SOP where it was", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"doc_cap_chars_manual_learnings":20003}`)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asDocCapMoved("manual_learnings", 20003)))
	})
}

func TestSystemInteractionCap(t *testing.T) {
	t.Run("patching the system-interaction cap moves that cap alone and takes effect on the next read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"doc_cap_chars_system_interaction":70000}`)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asDocCapMoved("system_interaction", 70000)))
	})
}

func TestBootSequenceCap(t *testing.T) {
	t.Run("ONE boot-sequence cap serves both runtimes, and patching it moves that cap alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"doc_cap_chars_boot_sequence":20004}`)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asDocCapMoved("boot_sequence", 20004)))
	})
}

func TestOffboardCap(t *testing.T) {
	t.Run("patching the offboard cap moves that cap alone and takes effect on the next read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"doc_cap_chars_offboard":20005}`)

		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asDocCapMoved("offboard", 20005)))
	})
}

func TestChatBudget(t *testing.T) {
	t.Run("the wake-snapshot chat budget ships at six thousand characters and a patch takes effect on the next snapshot", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "chat budget", any(float64(api.chatBudget())), any(6000))

		asPatchSettings(t, h, owner, `{"chat_budget_chars":9000}`)
		apiWantValue(t, "chat budget", any(float64(api.chatBudget())), any(9000))
		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asShippedDocCaps()))
	})
}

func TestStepNoteCap(t *testing.T) {
	t.Run("one task step's working note ships capped at ten thousand characters and a patch takes effect on the next write", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "step note cap", any(float64(api.stepNoteCap())), any(10000))

		asPatchSettings(t, h, owner, `{"step_note_cap_chars":20006}`)
		apiWantValue(t, "step note cap", any(float64(api.stepNoteCap())), any(20006))
		apiWantValue(t, "document caps", any(asDocCaps(api)), any(asShippedDocCaps()))
	})
}

func TestBackupRetainSetting(t *testing.T) {
	t.Run("five backups ship as the retention, and a patch shows up on the next read with no restart", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "backup retention", any(float64(api.backupRetainSetting())), any(5))

		asPatchSettings(t, h, owner, `{"backup_retain":9}`)
		apiWantValue(t, "backup retention", any(float64(api.backupRetainSetting())), any(9))
	})
}

// asDisplayPrefs is the four owner-facing display strings in one value.
func asDisplayPrefs(api *apiServer) map[string]any {
	return map[string]any{
		"org": api.orgNameSnapshot(), "owner": api.ownerNameSnapshot(),
		"theme": api.displayThemeSnapshot(), "language": api.displayLanguageSnapshot(),
		"wide": api.displayWideSnapshot(),
	}
}

func TestOrgNameSnapshot(t *testing.T) {
	t.Run("an unnamed studio reads empty, a patch names it live, and clearing it empties it again", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "display prefs", any(asDisplayPrefs(api)), any(map[string]any{
			"org": "", "owner": "", "theme": "", "language": "", "wide": false,
		}))

		asPatchSettings(t, h, owner, `{"org_name":"  Studio Nine  "}`)
		apiWantValue(t, "display prefs", any(asDisplayPrefs(api)), any(map[string]any{
			"org": "Studio Nine", "owner": "", "theme": "", "language": "", "wide": false,
		}))

		asPatchSettings(t, h, owner, `{"org_name":""}`)
		apiWantValue(t, "studio name", any(api.orgNameSnapshot()), any(""))
	})
}

func TestOwnerNameSnapshot(t *testing.T) {
	t.Run("an unnamed owner reads empty, a patch names them live, and clearing it empties it again", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "owner nickname", any(api.ownerNameSnapshot()), any(""))

		asPatchSettings(t, h, owner, `{"owner_name":"  Eva  "}`)
		apiWantValue(t, "display prefs", any(asDisplayPrefs(api)), any(map[string]any{
			"org": "", "owner": "Eva", "theme": "", "language": "", "wide": false,
		}))

		asPatchSettings(t, h, owner, `{"owner_name":""}`)
		apiWantValue(t, "owner nickname", any(api.ownerNameSnapshot()), any(""))
	})
}

func TestDisplayThemeSnapshot(t *testing.T) {
	t.Run("an unset theme reads empty so the frontend keeps its own default, and a patch takes effect live", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "cockpit theme", any(api.displayThemeSnapshot()), any(""))

		asPatchSettings(t, h, owner, `{"display_theme":"office"}`)
		apiWantValue(t, "display prefs", any(asDisplayPrefs(api)), any(map[string]any{
			"org": "", "owner": "", "theme": "office", "language": "", "wide": false,
		}))

		asPatchSettings(t, h, owner, `{"display_theme":""}`)
		apiWantValue(t, "cockpit theme", any(api.displayThemeSnapshot()), any(""))
	})
}

func TestDisplayLanguageSnapshot(t *testing.T) {
	t.Run("an unset language reads empty so the frontend keeps its own default, and a patch takes effect live", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "cockpit language", any(api.displayLanguageSnapshot()), any(""))

		asPatchSettings(t, h, owner, `{"display_language":"en"}`)
		apiWantValue(t, "display prefs", any(asDisplayPrefs(api)), any(map[string]any{
			"org": "", "owner": "", "theme": "", "language": "en", "wide": false,
		}))

		asPatchSettings(t, h, owner, `{"display_language":""}`)
		apiWantValue(t, "cockpit language", any(api.displayLanguageSnapshot()), any(""))
	})
}

func TestDisplayWideSnapshot(t *testing.T) {
	t.Run("the narrow centred column is the shipped layout, and a patch widens it live", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "cockpit layout width", any(api.displayWideSnapshot()), any(false))

		asPatchSettings(t, h, owner, `{"display_wide":true}`)
		apiWantValue(t, "display prefs", any(asDisplayPrefs(api)), any(map[string]any{
			"org": "", "owner": "", "theme": "", "language": "", "wide": true,
		}))

		asPatchSettings(t, h, owner, `{"display_wide":false}`)
		apiWantValue(t, "cockpit layout width", any(api.displayWideSnapshot()), any(false))
	})
}

// asCtxHigh is the whole context-high band config as a comparable value,
// alongside the two codex rounds read under the same lock.
func asCtxHigh(api *apiServer) map[string]any {
	cfg := api.ctxHighConfig()
	return map[string]any{
		"notice_pct": float64(cfg.NoticePct), "handover_pct": float64(cfg.HandoverPct),
		"min_boot_secs": cfg.MinBootSecs, "stale_guard": cfg.StaleGuard,
		"codex_notice_round":    float64(api.codexNoticeRoundSetting()),
		"codex_final_threshold": float64(api.codexCompactionThresholdSetting()),
	}
}

func TestCtxHighConfig(t *testing.T) {
	t.Run("the shipped band notices at 40 and hands over at 50, guarded against a boot storm and stale gauges", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "context band", any(asCtxHigh(api)), any(map[string]any{
			"notice_pct": 40.0, "handover_pct": 50.0, "min_boot_secs": 120.0,
			"stale_guard": true, "codex_notice_round": 2.0, "codex_final_threshold": 3.0,
		}))
	})

	t.Run("a patch of both percentages is read live as ONE coherent pair", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		asPatchSettings(t, h, owner, `{"notice_pct":45,"handover_pct":80}`)

		apiWantValue(t, "context band", any(asCtxHigh(api)), any(map[string]any{
			"notice_pct": 45.0, "handover_pct": 80.0, "min_boot_secs": 120.0,
			"stale_guard": true, "codex_notice_round": 2.0, "codex_final_threshold": 3.0,
		}))
	})

	t.Run("the answer is a COPY, so a call site editing its snapshot cannot reach the next read", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		mine := api.ctxHighConfig()
		mine.NoticePct = 1
		mine.HandoverPct = 99
		mine.StaleGuard = false

		apiWantValue(t, "context band", any(asCtxHigh(api)), any(map[string]any{
			"notice_pct": 40.0, "handover_pct": 50.0, "min_boot_secs": 120.0,
			"stale_guard": true, "codex_notice_round": 2.0, "codex_final_threshold": 3.0,
		}))
	})
}

func TestCodexNoticeRoundSetting(t *testing.T) {
	t.Run("the codex first notice ships at round two, and a patch is read live beside the band it is paired with", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "codex first-notice round", any(float64(api.codexNoticeRoundSetting())), any(2))

		asPatchSettings(t, h, owner, `{"codex_notice_round":4,"codex_compaction_threshold":6}`)
		apiWantValue(t, "context band", any(asCtxHigh(api)), any(map[string]any{
			"notice_pct": 40.0, "handover_pct": 50.0, "min_boot_secs": 120.0,
			"stale_guard": true, "codex_notice_round": 4.0, "codex_final_threshold": 6.0,
		}))
	})
}

func TestCodexCompactionThresholdSetting(t *testing.T) {
	t.Run("the codex final round ships at three, and a patch is read live beside the notice round it is paired with", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		apiWantValue(t, "codex final round", any(float64(api.codexCompactionThresholdSetting())), any(3))

		asPatchSettings(t, h, owner, `{"codex_notice_round":4,"codex_compaction_threshold":6}`)
		apiWantValue(t, "codex final round", any(float64(api.codexCompactionThresholdSetting())), any(6))
		apiWantValue(t, "codex first-notice round", any(float64(api.codexNoticeRoundSetting())), any(4))
	})
}

// asGauge is one context gauge record carrying a session anchor.
func asGauge(bootTS float64) map[string]any {
	return map[string]any{"boot_ts": bootTS}
}

func TestClaimHandoverNotice(t *testing.T) {
	t.Run("the notice is granted exactly once per session anchor, and the claim is written durably onto the member row", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		record := asGauge(1800000000)

		apiWantValue(t, "the first claim", any(api.claimHandoverNotice("mira", record)), any(true))

		m, err := d.GetMember("mira")
		if err != nil || m == nil {
			t.Fatalf("GetMember: %v (%v)", m, err)
		}
		apiWantValue(t, "the durable claim", any(m.HandoverNoticedTS), any(1800000000.0))
		apiWantValue(t, "the process-local cache", any(api.cachedHandoverClaim("mira")), any(1800000000.0))
		apiWantValue(t, "the second claim on the same anchor", any(api.claimHandoverNotice("mira", record)), any(false))
	})

	t.Run("a genuinely new session brings a new anchor and is entitled to its own notice", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.claimHandoverNotice("mira", asGauge(1800000000))

		apiWantValue(t, "the claim on a fresh anchor",
			any(api.claimHandoverNotice("mira", asGauge(1800000099))), any(true))

		m, err := d.GetMember("mira")
		if err != nil || m == nil {
			t.Fatalf("GetMember: %v (%v)", m, err)
		}
		apiWantValue(t, "the durable claim", any(m.HandoverNoticedTS), any(1800000099.0))
	})

	t.Run("a record with no usable anchor is refused, so no notice fires off something we cannot recognise again", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiWantValue(t, "no record at all", any(api.claimHandoverNotice("mira", nil)), any(false))
		apiWantValue(t, "no boot anchor", any(api.claimHandoverNotice("mira", map[string]any{})), any(false))
		apiWantValue(t, "a zero anchor", any(api.claimHandoverNotice("mira", asGauge(0))), any(false))
		apiWantValue(t, "a negative anchor", any(api.claimHandoverNotice("mira", asGauge(-1))), any(false))
		apiWantValue(t, "an anchor that is not a number",
			any(api.claimHandoverNotice("mira", map[string]any{"boot_ts": "yesterday"})), any(false))

		m, err := d.GetMember("mira")
		if err != nil || m == nil {
			t.Fatalf("GetMember: %v (%v)", m, err)
		}
		apiWantValue(t, "the durable claim", any(m.HandoverNoticedTS), any(0))
		apiWantValue(t, "the process-local cache", any(api.cachedHandoverClaim("mira")), any(0))
	})

	t.Run("a second process over the same station finds the column already claimed, stays quiet, and warms its own cache", func(t *testing.T) {
		first, _, d, _ := newAPITestServer(t)
		record := asGauge(1800000000)
		if !first.claimHandoverNotice("mira", record) {
			t.Fatalf("the first process must take the claim")
		}
		second := newAPIServer(d, NewHub(), first.keys, 86400, "../..")

		apiWantValue(t, "the fresh process's cache before it asks",
			any(second.cachedHandoverClaim("mira")), any(0))
		apiWantValue(t, "the claim after a re-exec",
			any(second.claimHandoverNotice("mira", record)), any(false))
		apiWantValue(t, "the warmed cache", any(second.cachedHandoverClaim("mira")), any(1800000000.0))
	})

	t.Run("a member the roster does not carry still gets its claim, because silence is the expensive direction here", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "the claim", any(api.claimHandoverNotice("ghost", asGauge(1800000000))), any(true))
		apiWantValue(t, "the process-local cache", any(api.cachedHandoverClaim("ghost")), any(1800000000.0))
		apiWantValue(t, "the second claim", any(api.claimHandoverNotice("ghost", asGauge(1800000000))), any(false))
	})
}

func TestCachedHandoverClaim(t *testing.T) {
	t.Run("an agent this process holds no claim for reads zero", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "cached claim", any(api.cachedHandoverClaim("mira")), any(0))
	})

	t.Run("the anchor this process claimed is read back, and each agent's claim is its own", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.rememberHandoverClaim("mira", 1800000000)
		api.rememberHandoverClaim(apiTestPlainAgentID, 1800000099)

		apiWantValue(t, "cached claims", any(map[string]any{
			"mira":   api.cachedHandoverClaim("mira"),
			"kip":    api.cachedHandoverClaim(apiTestPlainAgentID),
			"nobody": api.cachedHandoverClaim("nobody"),
			"empty":  api.cachedHandoverClaim(""),
		}), any(map[string]any{
			"mira": 1800000000.0, "kip": 1800000099.0, "nobody": 0.0, "empty": 0.0,
		}))
	})
}

func TestRememberHandoverClaim(t *testing.T) {
	t.Run("the first caller on an anchor takes it and a second caller on the SAME anchor is told it did not", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "the first caller", any(api.rememberHandoverClaim("mira", 1800000000)), any(true))
		apiWantValue(t, "the racing caller", any(api.rememberHandoverClaim("mira", 1800000000)), any(false))
		apiWantValue(t, "cached claim", any(api.cachedHandoverClaim("mira")), any(1800000000.0))
	})

	t.Run("a different anchor on the same agent, and the same anchor on a different agent, are both taken", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.rememberHandoverClaim("mira", 1800000000)

		apiWantValue(t, "a fresh anchor on the same agent",
			any(api.rememberHandoverClaim("mira", 1800000099)), any(true))
		apiWantValue(t, "the same anchor on another agent",
			any(api.rememberHandoverClaim(apiTestPlainAgentID, 1800000099)), any(true))
		apiWantValue(t, "cached claims", any(map[string]any{
			"mira": api.cachedHandoverClaim("mira"),
			"kip":  api.cachedHandoverClaim(apiTestPlainAgentID),
		}), any(map[string]any{"mira": 1800000099.0, "kip": 1800000099.0}))
	})

	t.Run("a server that has never held a claim opens its book on the first one rather than faulting", func(t *testing.T) {
		bare := &apiServer{}

		apiWantValue(t, "the first claim", any(bare.rememberHandoverClaim("mira", 1800000000)), any(true))
		apiWantValue(t, "cached claim", any(bare.cachedHandoverClaim("mira")), any(1800000000.0))
	})
}

func TestHandoverNoticeSettled(t *testing.T) {
	t.Run("a fresh anchor this process has not claimed is NOT settled, so the tick goes on to compose the notice", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "settled", any(api.handoverNoticeSettled("mira", asGauge(1800000000))), any(false))
	})

	t.Run("once the claim is in the cache the tick is settled, and it goes back to unsettled on a new session anchor", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		record := asGauge(1800000000)
		api.claimHandoverNotice("mira", record)

		apiWantValue(t, "settled on the claimed anchor",
			any(api.handoverNoticeSettled("mira", record)), any(true))
		apiWantValue(t, "settled on the next session's anchor",
			any(api.handoverNoticeSettled("mira", asGauge(1800000099))), any(false))
		apiWantValue(t, "settled for another agent on the same anchor",
			any(api.handoverNoticeSettled(apiTestPlainAgentID, record)), any(false))
	})

	t.Run("a record with no usable anchor is settled, because a claim could never have been granted for one", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiWantValue(t, "no record at all", any(api.handoverNoticeSettled("mira", nil)), any(true))
		apiWantValue(t, "no boot anchor", any(api.handoverNoticeSettled("mira", map[string]any{})), any(true))
		apiWantValue(t, "a zero anchor", any(api.handoverNoticeSettled("mira", asGauge(0))), any(true))
		apiWantValue(t, "a negative anchor", any(api.handoverNoticeSettled("mira", asGauge(-1))), any(true))
		apiWantValue(t, "an anchor that is not a number",
			any(api.handoverNoticeSettled("mira", map[string]any{"boot_ts": "yesterday"})), any(true))
	})

	t.Run("a cache MISS is never an answer: a claim only the durable column holds still leaves the tick unsettled", func(t *testing.T) {
		first, _, d, _ := newAPITestServer(t)
		record := asGauge(1800000000)
		first.claimHandoverNotice("mira", record)
		second := newAPIServer(d, NewHub(), first.keys, 86400, "../..")

		apiWantValue(t, "settled", any(second.handoverNoticeSettled("mira", record)), any(false))
	})
}
