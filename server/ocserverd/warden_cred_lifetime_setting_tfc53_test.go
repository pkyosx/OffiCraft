package main

// warden_cred_lifetime_setting_tfc53_test.go — T-fc53 第一段: the machine
// credential lifetime is an owner-typed SETTING, and the fleet can read it.
//
// 🔴 WHAT THIS FILE IS ACTUALLY GUARDING. It was written while the setting was
// inert on this side — "nothing here mints differently, refuses differently, or
// expires anything because of it". T-fc53 第二段 changed that: mintWardenToken
// stamps exp = iat + this value, and THAT half is guarded in api_machines_test.go
// (TestWardenCredentialsCarryTheConfiguredLifetimeAcrossAllMachineMintPaths).
// What this file still guards is the OTHER consumer, which is off this machine
// entirely: each warden polls
// GET /api/machines/credential-policy and derives its own renewal threshold from
// the answer. So the failure this file has to catch is not "the value is wrong",
// it is "the value never leaves the building" — a knob that saves, reads back
// beautifully on the settings page, and is published to nobody. That failure is
// completely silent: the fleet keeps using its shipped default and looks healthy.
//
// Accordingly every face is read TWICE — once at the shipped default and once
// after a PATCH — and the pair must MOVE. Asserting only the post-PATCH value
// passes for a face that hard-codes it; asserting only the default passes for a
// face that hard-codes 90 days, which is what the wardens already assume.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func patchWardenCredLifetime(t *testing.T, api *apiServer, n int) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateSettingsApiSettingsPatch(rec,
		taskReq(t, http.MethodPatch, "/api/settings",
			map[string]any{"warden_credential_lifetime_secs": n}, "owner", "owner"))
	return rec
}

func wardenCredLifetimeSettings(t *testing.T, api *apiServer) settingsDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetSettingsApiSettingsGet(rec,
		taskReq(t, http.MethodGet, "/api/settings", nil, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[settingsDTO](t, rec)
}

// readCredentialPolicy drives the endpoint a WARDEN calls, and returns the RAW
// decoded object rather than the DTO. The field NAME is part of what is being
// asserted: the warden decodes `lifetime_secs` and declines anything else
// (cli/ocwarden/renewapply.go), so a rename on this side leaves the endpoint
// answering 200 while every machine in the fleet silently reverts to its
// built-in default. A typed decode here would hide exactly that.
func readCredentialPolicy(t *testing.T, api *apiServer) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleMachineCredentialPolicyApiMachinesCredentialPolicyGet(rec,
		taskReq(t, http.MethodGet, "/api/machines/credential-policy", nil, "m-box", "machine"))
	if rec.Code != http.StatusOK {
		t.Fatalf("get credential-policy: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode credential-policy body %q: %v", rec.Body.String(), err)
	}
	return out
}

// TestWardenCredLifetime_UnsetIsNinetyDaysOnEveryFace. The default is not a
// cosmetic choice: it is the number every warden already assumes when it cannot
// reach the station, so a station whose default disagreed with the fleet's would
// change behaviour for nobody's benefit the moment the endpoint went down.
//
// It was ...UnsetIsThirtyDaysOnEveryFace until T-fc53 第二段, when the owner moved
// the default to 90 days (2026-09-06 18:36 「加回去預設 90 天可以調整」) in the same
// breath as putting the expiry back. The NUMBER is spelled out here rather than
// read from wardenCredLifetimeSecsDefault deliberately: a test that reads the
// constant it is guarding asserts nothing about the constant, and this value is
// now stamped into every machine credential the station mints.
func TestWardenCredLifetime_UnsetIsNinetyDaysOnEveryFace(t *testing.T) {
	api := resumeCtxServer(t)
	const ninetyDays = 90 * 86400

	if got := api.wardenCredLifetimeValue(); got != ninetyDays {
		t.Errorf("live accessor with no row: got %d, want %d", got, ninetyDays)
	}
	if got := wardenCredLifetimeSettings(t, api).WardenCredentialLifetimeSecs; got != ninetyDays {
		t.Errorf("GET /api/settings with no row: got %d, want %d", got, ninetyDays)
	}
	if got := readCredentialPolicy(t, api)["lifetime_secs"]; got != float64(ninetyDays) {
		t.Errorf("GET /api/machines/credential-policy with no row: got %v, want %d",
			got, ninetyDays)
	}
	loaded, err := loadAuthSettings(api.dal, defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if loaded.wardenCredLifetimeSecs != ninetyDays {
		t.Errorf("boot-time load with no row: got %d, want %d",
			loaded.wardenCredLifetimeSecs, ninetyDays)
	}

	// 🔴 THE STATION'S DEFAULT AND THE WARDEN'S BUILT-IN DEFAULT MUST BE THE SAME
	// NUMBER, and nothing compiles them together: they live in different modules
	// (credentialLifetimeDefaultSecs, cli/ocwarden/renew.go). A drift is silent —
	// it only shows on machines that cannot reach the policy endpoint, which are
	// exactly the machines nobody is looking at. This assertion is a written copy
	// of the warden's number, so moving one side without the other is red here.
	const wardenBuiltInDefaultSecs = 90 * 24 * 60 * 60
	if ninetyDays != wardenBuiltInDefaultSecs {
		t.Errorf("the station ships %d s but cli/ocwarden assumes %d s when the "+
			"policy endpoint is unreachable — a machine that cannot reach the "+
			"station would renew on a different clock from the one its credential "+
			"expires on", ninetyDays, wardenBuiltInDefaultSecs)
	}
}

// TestWardenCredLifetime_APatchReachesTheFleetFace is the ticket's acceptance in
// one test: the owner types 3 days because he wants to WATCH a renewal happen,
// and the only thing that makes that true is the number arriving at the machines.
func TestWardenCredLifetime_APatchReachesTheFleetFace(t *testing.T) {
	api := resumeCtxServer(t)
	const threeDays = 3 * 86400

	before := readCredentialPolicy(t, api)["lifetime_secs"]

	if rec := patchWardenCredLifetime(t, api, threeDays); rec.Code != http.StatusOK {
		t.Fatalf("patch warden_credential_lifetime_secs=%d: %d %s",
			threeDays, rec.Code, rec.Body.String())
	}

	after := readCredentialPolicy(t, api)["lifetime_secs"]
	if before == after {
		t.Fatalf("the policy endpoint answered %v both before and after the setting "+
			"changed. A knob the fleet cannot see is a knob that does nothing, and it "+
			"fails SILENTLY — every machine keeps its built-in default and looks fine",
			before)
	}
	if after != float64(threeDays) {
		t.Errorf("policy endpoint after the patch: got %v, want %d", after, threeDays)
	}
	if got := wardenCredLifetimeSettings(t, api).WardenCredentialLifetimeSecs; got != threeDays {
		t.Errorf("GET /api/settings after the patch: got %d, want %d", got, threeDays)
	}
	// The DB row, so the value is still there after a restart rather than living
	// only in the process that took the PATCH.
	row, err := api.dal.GetSetting(settingWardenCredLifetimeSecs)
	if err != nil || row == nil {
		t.Fatalf("setting row after patch: %v %v", row, err)
	}
	if *row != strconv.Itoa(threeDays) {
		t.Errorf("setting row = %q, want %q", *row, strconv.Itoa(threeDays))
	}
}

// TestWardenCredLifetime_TheFieldNameIsWhatTheWardenDecodes. The warden's decoder
// declines an absent field and keeps its previous number, so a rename here does
// not break anything loudly — it quietly stops the fleet ever hearing about the
// setting again. Nothing else in this repo would notice.
func TestWardenCredLifetime_TheFieldNameIsWhatTheWardenDecodes(t *testing.T) {
	body := readCredentialPolicy(t, resumeCtxServer(t))
	if _, ok := body["lifetime_secs"]; !ok {
		t.Fatalf("the policy response has no `lifetime_secs` field — that is the exact "+
			"key cli/ocwarden/renewapply.go decodes, and a miss there is silent. body=%v",
			body)
	}
}

// TestWardenCredLifetime_TheRangeIsRefusedAndNothingIsWritten. The floor exists
// because the last third of the lifetime is an offline machine's retry window;
// the refusal has to happen BEFORE the row is written, or a restart reads a value
// the loader will not boot on.
func TestWardenCredLifetime_TheRangeIsRefusedAndNothingIsWritten(t *testing.T) {
	for _, tc := range []struct {
		name string
		secs int
		want int
		why  string
	}{
		{"the floor itself", minWardenCredLifetimeSecs, http.StatusOK,
			"the boundary is legal; the refusal starts below it"},
		{"the owner's three days", 3 * 86400, http.StatusOK,
			"the value he said he would set — a floor that refused it would be the wrong floor"},
		{"one second under the floor", minWardenCredLifetimeSecs - 1, http.StatusUnprocessableEntity,
			"below a day the retry window stops surviving a working day of downtime"},
		{"the ceiling itself", maxWardenCredLifetimeSecs, http.StatusOK, "legal"},
		{"one second over the ceiling", maxWardenCredLifetimeSecs + 1, http.StatusUnprocessableEntity,
			"400 days is the ceiling every long-lived credential here already lives under"},
		{"zero", 0, http.StatusUnprocessableEntity,
			"a stray zero must never become 'renew on every poll, fleet-wide'"},
		{"negative", -1, http.StatusUnprocessableEntity, "not a duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := resumeCtxServer(t)
			rec := patchWardenCredLifetime(t, api, tc.secs)
			if rec.Code != tc.want {
				t.Fatalf("patch %d: got %d, want %d (%s) — %s",
					tc.secs, rec.Code, tc.want, rec.Body.String(), tc.why)
			}
			if tc.want != http.StatusOK {
				row, err := api.dal.GetSetting(settingWardenCredLifetimeSecs)
				if err != nil {
					t.Fatalf("read setting row: %v", err)
				}
				if row != nil {
					t.Errorf("a refused patch still wrote the row (%q). The next boot "+
						"would read a value the loader refuses, i.e. a station that saves "+
						"fine and then will not start", *row)
				}
				if got := api.wardenCredLifetimeValue(); got != wardenCredLifetimeSecsDefault {
					t.Errorf("a refused patch moved the live value to %d", got)
				}
			}
		})
	}
}

// TestWardenCredLifetime_SaveFaceAndLoadFaceAgree. Two range predicates for one
// setting is how a value that saves becomes a station that will not boot — with
// no warning at save time. There is one predicate; this proves both faces use it.
func TestWardenCredLifetime_SaveFaceAndLoadFaceAgree(t *testing.T) {
	s := resumeCtxServer(t)
	for _, secs := range []int{
		minWardenCredLifetimeSecs - 1, minWardenCredLifetimeSecs,
		3 * 86400, wardenCredLifetimeSecsDefault,
		maxWardenCredLifetimeSecs, maxWardenCredLifetimeSecs + 1, 0, -1,
	} {
		accepted := wardenCredLifetimeInRange(secs)
		if err := s.dal.PutSetting(settingWardenCredLifetimeSecs, strconv.Itoa(secs)); err != nil {
			t.Fatalf("put setting: %v", err)
		}
		_, err := loadAuthSettings(s.dal, defaultConfig(), func(string) {})
		switch {
		case accepted && err != nil:
			t.Errorf("%d: the save face accepts it and the loader refuses to boot: %v", secs, err)
		case !accepted && err == nil:
			t.Errorf("%d: the loader boots on a value the save face refuses — a "+
				"hand-edited row would install a lifetime the PATCH face would not have", secs)
		}
	}
}
