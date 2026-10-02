package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	upgradeStartPath  = "/api/machines/m-box/runtime-upgrade"
	upgradeReportPath = "/api/monitoring/runtime-upgrade"
)

func newUpgradeFixture(t *testing.T) *loginFixture {
	t.Helper()
	f := newLoginFixture(t)
	f.api.runtimeUpgrades.now = func() time.Time { return f.clock }
	return f
}

func (f *loginFixture) startUpgrade(t *testing.T) string {
	t.Helper()
	status, data := apiJSON(t, f.h, "POST", upgradeStartPath, f.owner, `{"runtime":"claude"}`)
	if status != http.StatusOK {
		t.Fatalf("upgrade start: %d %v", status, data)
	}
	id, _ := data["upgrade_id"].(string)
	if !strings.HasPrefix(id, "ru-") {
		t.Fatalf("upgrade start answered upgrade_id %q", id)
	}
	return id
}

func (f *loginFixture) reportUpgrade(t *testing.T, token, body string) (int, map[string]any) {
	t.Helper()
	return apiJSON(t, f.h, "POST", upgradeReportPath, token, body)
}

func (f *loginFixture) getUpgrade(t *testing.T, id string) (int, map[string]any) {
	t.Helper()
	return apiJSON(t, f.h, "GET", upgradeStartPath+"/"+id, f.owner, "")
}

func upgradeBody(id, state string, from, to, reason any, started, updated float64) map[string]any {
	return map[string]any{
		"upgrade_id": id, "machine_id": loginMachine, "runtime": "claude", "state": state,
		"from_version": from, "to_version": to, "reason": reason,
		"started_ts": started, "updated_ts": updated,
	}
}

func upgradeSignal(id, trigger string) map[string]any {
	return map[string]any{
		"seq": apiAnyNumber, "topic": "runtime_upgrade", "op": "signal",
		"data": map[string]any{
			"entity": "runtime_upgrade", "key": id, "epoch": apiAnyNumber, "deleted": false, "payload": nil,
		},
		"ts": apiAnyNumber, "trigger": trigger,
	}
}

func TestRuntimeUpgradeStart(t *testing.T) {
	t.Run("under an online warden, the owner's start answers a starting upgrade, sends runtime_upgrade to that warden only, and signals the owner", func(t *testing.T) {
		f := newUpgradeFixture(t)
		status, data := apiJSON(t, f.h, "POST", upgradeStartPath, f.owner, `{"runtime":"claude"}`)
		if status != http.StatusOK {
			t.Fatalf("start: %d %v", status, data)
		}
		apiWantBody(t, data, upgradeBody(apiAnyString, "starting", nil, nil, nil, loginEpoch, loginEpoch))
		id := data["upgrade_id"].(string)
		apiWantValue(t, "frames", any(drainFrames(t, f.api, loginMachine)), any([]drainedFrame{{
			Topic: "warden-command", RPC: "runtime_upgrade",
			Args: map[string]any{"member_id": loginMachine, "upgrade_id": id, "runtime": "claude"},
		}}))
		wantNoWardenFrames(t, f, loginOtherMachine)
		f.dashboard.wantFrames(upgradeSignal(id, "owner"))
		assertNoFrame(t, f.wardenConn, "the owner-only runtime_upgrade signal")
	})

	t.Run("under an upgrade already in flight for the machine, a second start answers that upgrade and sends nothing", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		drainFrames(t, f.api, loginMachine)
		f.dashboard.wantFrames(upgradeSignal(id, "owner"))
		f.advance(5 * time.Second)

		status, data := apiJSON(t, f.h, "POST", upgradeStartPath, f.owner, `{"runtime":"claude"}`)
		if status != http.StatusOK {
			t.Fatalf("repeat start: %d %v", status, data)
		}
		apiWantBody(t, data, upgradeBody(id, "starting", nil, nil, nil, loginEpoch, loginEpoch))
		wantNoWardenFrames(t, f, loginMachine)
		f.dashboard.wantFrames()
	})

	t.Run("under a terminal upgrade for the machine, a start begins a new upgrade", func(t *testing.T) {
		f := newUpgradeFixture(t)
		first := f.startUpgrade(t)
		if status, data := f.reportUpgrade(t, f.warden, `{"upgrade_id":"`+first+`","state":"failed","reason":"x"}`); status != http.StatusOK {
			t.Fatalf("failed report: %d %v", status, data)
		}
		drainFrames(t, f.api, loginMachine)
		second := f.startUpgrade(t)
		if second == first {
			t.Fatalf("a start after a failed upgrade answered the failed one (%s)", first)
		}
		apiWantValue(t, "frames", any(drainFrames(t, f.api, loginMachine)), any([]drainedFrame{{
			Topic: "warden-command", RPC: "runtime_upgrade",
			Args: map[string]any{"member_id": loginMachine, "upgrade_id": second, "runtime": "claude"},
		}}))
	})

	t.Run("under an admin agent caller, the start is allowed", func(t *testing.T) {
		f := newUpgradeFixture(t)
		admin := apiTestPrincipalToken(t, f.api, f.dal, principalAdminAgent, "adm-1")
		status, data := apiJSON(t, f.h, "POST", upgradeStartPath, admin, `{"runtime":"claude"}`)
		if status != http.StatusOK {
			t.Fatalf("admin start: %d %v", status, data)
		}
		apiWantBody(t, data, upgradeBody(apiAnyString, "starting", nil, nil, nil, loginEpoch, loginEpoch))
	})

	t.Run("under a plain agent or the warden itself, the start is 403 and nothing is sent", func(t *testing.T) {
		f := newUpgradeFixture(t)
		plain := apiTestPrincipalToken(t, f.api, f.dal, principalAgent, "plain-1")
		for _, token := range []string{plain, f.warden} {
			status, data := apiJSON(t, f.h, "POST", upgradeStartPath, token, `{"runtime":"claude"}`)
			if status != http.StatusForbidden {
				t.Fatalf("start: want 403, got %d %v", status, data)
			}
		}
		wantNoWardenFrames(t, f, loginMachine)
		f.dashboard.wantFrames()
	})

	t.Run("under an offline warden, the start is 409 and no upgrade exists afterwards", func(t *testing.T) {
		f := newUpgradeFixture(t)
		putWarden(t, f.api, "m-dark")
		status, data := apiJSON(t, f.h, "POST", "/api/machines/m-dark/runtime-upgrade", f.owner, `{"runtime":"claude"}`)
		if status != http.StatusConflict {
			t.Fatalf("offline start: want 409, got %d %v", status, data)
		}
		apiWantError(t, data, "conflict", "machine is offline; its warden cannot run an upgrade")
		wantNoWardenFrames(t, f, "m-dark")
		f.dashboard.wantFrames()
		f.api.runtimeUpgrades.mu.Lock()
		held := len(f.api.runtimeUpgrades.upgrades)
		f.api.runtimeUpgrades.mu.Unlock()
		if held != 0 {
			t.Fatalf("the refused start left %d upgrade(s) held", held)
		}
	})

	t.Run("under an unknown id or a non-machine member, the start is 404", func(t *testing.T) {
		f := newUpgradeFixture(t)
		for _, id := range []string{"m-nope", apiTestPlainAgentID} {
			status, data := apiJSON(t, f.h, "POST", "/api/machines/"+id+"/runtime-upgrade", f.owner, `{"runtime":"claude"}`)
			if status != http.StatusNotFound {
				t.Fatalf("start on %s: want 404, got %d %v", id, status, data)
			}
		}
	})

	t.Run("under runtime codex or one the server does not know, the start is 422 and nothing is sent", func(t *testing.T) {
		f := newUpgradeFixture(t)
		for _, runtime := range []string{"codex", "gemini"} {
			status, data := apiJSON(t, f.h, "POST", upgradeStartPath, f.owner, `{"runtime":"`+runtime+`"}`)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("%s start: want 422, got %d %v", runtime, status, data)
			}
			apiWantError(t, data, "validation_error", "runtime must be 'claude'")
		}
		wantNoWardenFrames(t, f, loginMachine)
		f.dashboard.wantFrames()
	})
}

func TestRuntimeUpgradeReport(t *testing.T) {
	t.Run("under the upgrade's own warden, running stores from_version, the owner reads it back, and the owner is signalled", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		f.dashboard.wantFrames(upgradeSignal(id, "owner"))
		f.advance(3 * time.Second)

		status, data := f.reportUpgrade(t, f.warden, `{"upgrade_id":"`+id+`","state":"running","from_version":"2.1.200"}`)
		if status != http.StatusOK {
			t.Fatalf("report: %d %v", status, data)
		}
		want := upgradeBody(id, "running", "2.1.200", nil, nil, loginEpoch, loginEpoch+3)
		apiWantBody(t, data, want)
		f.dashboard.wantFrames(upgradeSignal(id, loginMachine))
		_, got := f.getUpgrade(t, id)
		apiWantBody(t, got, want)
	})

	t.Run("under a succeeded report, both versions are kept and a later report leaves the upgrade unchanged", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		f.advance(time.Second)
		f.reportUpgrade(t, f.warden, `{"upgrade_id":"`+id+`","state":"running","from_version":"2.1.200"}`)
		f.advance(time.Second)
		status, data := f.reportUpgrade(t, f.warden,
			`{"upgrade_id":"`+id+`","state":"succeeded","from_version":"2.1.999","to_version":"2.1.290"}`)
		if status != http.StatusOK {
			t.Fatalf("succeeded report: %d %v", status, data)
		}
		want := upgradeBody(id, "succeeded", "2.1.200", "2.1.290", nil, loginEpoch, loginEpoch+2)
		apiWantBody(t, data, want)

		f.advance(time.Second)
		status, data = f.reportUpgrade(t, f.warden, `{"upgrade_id":"`+id+`","state":"failed","reason":"late"}`)
		if status != http.StatusOK {
			t.Fatalf("late report: %d %v", status, data)
		}
		apiWantBody(t, data, want)
	})

	t.Run("under a failed report, the reason and the version read back are kept", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		status, data := f.reportUpgrade(t, f.warden, `{"upgrade_id":"`+id+`","state":"failed",`+
			`"from_version":"2.1.200","to_version":"2.1.200","reason":"版本沒有變"}`)
		if status != http.StatusOK {
			t.Fatalf("failed report: %d %v", status, data)
		}
		apiWantBody(t, data, upgradeBody(id, "failed", "2.1.200", "2.1.200", "版本沒有變", loginEpoch, loginEpoch))
	})

	t.Run("under a running report carrying to_version or reason, neither is kept", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		status, data := f.reportUpgrade(t, f.warden,
			`{"upgrade_id":"`+id+`","state":"running","from_version":"2.1.200","to_version":"9.9.9","reason":"r"}`)
		if status != http.StatusOK {
			t.Fatalf("report: %d %v", status, data)
		}
		apiWantBody(t, data, upgradeBody(id, "running", "2.1.200", nil, nil, loginEpoch, loginEpoch))
	})

	t.Run("under another machine's warden, the report is 404, the upgrade is unchanged and nobody is signalled", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		f.dashboard.wantFrames(upgradeSignal(id, "owner"))
		status, data := f.reportUpgrade(t, f.other, `{"upgrade_id":"`+id+`","state":"succeeded","to_version":"9.9.9"}`)
		if status != http.StatusNotFound {
			t.Fatalf("foreign report: want 404, got %d %v", status, data)
		}
		apiWantError(t, data, "not_found", "runtime upgrade '"+id+"' not found")
		f.dashboard.wantFrames()
		_, got := f.getUpgrade(t, id)
		apiWantBody(t, got, upgradeBody(id, "starting", nil, nil, nil, loginEpoch, loginEpoch))
	})

	t.Run("under a caller that is not a machine, the report is 404", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		agent := apiTestAgentToken(t, f.api, apiTestPlainAgentID, "")
		status, data := f.reportUpgrade(t, agent, `{"upgrade_id":"`+id+`","state":"succeeded"}`)
		if status != http.StatusNotFound {
			t.Fatalf("agent report: want 404, got %d %v", status, data)
		}
		_, got := f.getUpgrade(t, id)
		apiWantBody(t, got, upgradeBody(id, "starting", nil, nil, nil, loginEpoch, loginEpoch))
	})

	t.Run("under a starting or expired report, the report is 422", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		for _, state := range []string{"starting", "expired"} {
			status, data := f.reportUpgrade(t, f.warden, `{"upgrade_id":"`+id+`","state":"`+state+`"}`)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("%s report: want 422, got %d %v", state, status, data)
			}
			apiWantError(t, data, "validation_error", "state must be one of running, succeeded, failed")
		}
	})

	t.Run("under an unknown upgrade id, the report is 404", func(t *testing.T) {
		f := newUpgradeFixture(t)
		status, data := f.reportUpgrade(t, f.warden, `{"upgrade_id":"ru-nope","state":"running"}`)
		if status != http.StatusNotFound {
			t.Fatalf("unknown report: want 404, got %d %v", status, data)
		}
		apiWantError(t, data, "not_found", "runtime upgrade 'ru-nope' not found")
	})
}

func TestRuntimeUpgradeRead(t *testing.T) {
	t.Run("under an upgrade on another machine's path or an unknown id, the read is 404", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		if status, data := f.getUpgrade(t, id); status != http.StatusOK {
			t.Fatalf("control: own path read: %d %v", status, data)
		}
		for _, path := range []string{"/api/machines/" + loginOtherMachine + "/runtime-upgrade/" + id, upgradeStartPath + "/ru-nope"} {
			status, data := apiJSON(t, f.h, "GET", path, f.owner, "")
			if status != http.StatusNotFound {
				t.Fatalf("%s: want 404, got %d %v", path, status, data)
			}
		}
	})

	t.Run("under a plain agent, the read is 403", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		plain := apiTestPrincipalToken(t, f.api, f.dal, principalAgent, "plain-1")
		if status, data := apiJSON(t, f.h, "GET", upgradeStartPath+"/"+id, plain, ""); status != http.StatusForbidden {
			t.Fatalf("plain read: want 403, got %d %v", status, data)
		}
	})
}

func TestRuntimeUpgradeLifetime(t *testing.T) {
	t.Run("under 20 minutes without a warden report, the upgrade becomes expired and the sweep signals the owner", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		f.dashboard.wantFrames(upgradeSignal(id, "owner"))

		f.advance(20*time.Minute - time.Second)
		f.api.sweepRuntimeUpgrades()
		f.dashboard.wantFrames()
		_, got := f.getUpgrade(t, id)
		apiWantBody(t, got, upgradeBody(id, "starting", nil, nil, nil, loginEpoch, loginEpoch))

		f.advance(time.Second)
		f.api.sweepRuntimeUpgrades()
		f.dashboard.wantFrames(upgradeSignal(id, "server"))
		_, got = f.getUpgrade(t, id)
		apiWantBody(t, got, upgradeBody(id, "expired", nil, nil,
			"no report from the machine for 20 minutes", loginEpoch, loginEpoch+20*60))
	})

	t.Run("under a warden report, the 20 minutes count from that report", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		f.advance(15 * time.Minute)
		f.reportUpgrade(t, f.warden, `{"upgrade_id":"`+id+`","state":"running","from_version":"2.1.200"}`)
		f.advance(15 * time.Minute)
		_, got := f.getUpgrade(t, id)
		apiWantBody(t, got, upgradeBody(id, "running", "2.1.200", nil, nil, loginEpoch, loginEpoch+15*60))
	})

	t.Run("under 10 minutes after a terminal state, the upgrade is dropped and reads 404", func(t *testing.T) {
		f := newUpgradeFixture(t)
		id := f.startUpgrade(t)
		f.reportUpgrade(t, f.warden, `{"upgrade_id":"`+id+`","state":"failed","reason":"boom"}`)
		f.dashboard.wantFrames(upgradeSignal(id, "owner"), upgradeSignal(id, loginMachine))

		f.advance(10*time.Minute - time.Second)
		if status, data := f.getUpgrade(t, id); status != http.StatusOK {
			t.Fatalf("just before the drop: want 200, got %d %v", status, data)
		}
		f.advance(time.Second)
		f.api.sweepRuntimeUpgrades()
		f.dashboard.wantFrames(upgradeSignal(id, "server"))
		if status, data := f.getUpgrade(t, id); status != http.StatusNotFound {
			t.Fatalf("after the drop: want 404, got %d %v", status, data)
		}
	})
}

func TestRuntimeUpgradeFrameIsNotPersisted(t *testing.T) {
	t.Run("under a started upgrade, its frame is in no table while a warden update frame is", func(t *testing.T) {
		f := newUpgradeFixture(t)
		f.startUpgrade(t)
		if status, data := apiJSON(t, f.h, "POST", "/api/machines/"+loginOtherMachine+"/upgrade", f.owner, ""); status != http.StatusOK {
			t.Fatalf("update control: %d %v", status, data)
		}
		if hits := scanDatabaseFor(t, f.dal, `"rpc":"update"`); len(hits) == 0 {
			t.Fatal("control: the persisted update frame was not found — the scan does not reach the command store")
		}
		if hits := scanDatabaseFor(t, f.dal, `"rpc":"runtime_upgrade"`); len(hits) != 0 {
			t.Errorf("the runtime_upgrade frame is at rest in %v", hits)
		}
	})
}
