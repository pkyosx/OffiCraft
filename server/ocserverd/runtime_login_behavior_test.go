package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	loginMachine      = "m-box"
	loginOtherMachine = "m-other"
	loginEpoch        = 1_800_000_000
	loginStartPath    = "/api/machines/m-box/runtime-login"
	loginReportPath   = "/api/monitoring/runtime-login"
	loginTestURL      = "https://claude.ai/oauth/authorize?code=true&state=needle-url-4f1c"
	loginTestCode     = "needle-code-9b7e#state"
)

type loginFixture struct {
	api        *apiServer
	h          http.Handler
	dal        *DAL
	owner      string
	warden     string
	other      string
	clock      time.Time
	dashboard  *apiTestListener
	wardenConn *hubListener
}

func newLoginFixture(t *testing.T) *loginFixture {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	putWarden(t, api, loginMachine)
	putWarden(t, api, loginOtherMachine)
	f := &loginFixture{api: api, h: h, dal: d, owner: owner, clock: time.Unix(loginEpoch, 0)}
	api.runtimeLogins.now = func() time.Time { return f.clock }
	var err error
	if f.warden, err = api.mintWardenToken(Member{ID: loginMachine, Kind: KindWarden}); err != nil {
		t.Fatalf("mint warden credential: %v", err)
	}
	if f.other, err = api.mintWardenToken(Member{ID: loginOtherMachine, Kind: KindWarden}); err != nil {
		t.Fatalf("mint other warden credential: %v", err)
	}
	f.wardenConn = connectOnlineMachine(t, api, loginMachine, "")
	connectOnlineMachine(t, api, loginOtherMachine, "")
	f.dashboard = apiTestListen(t, api, "")
	return f
}

func (f *loginFixture) advance(d time.Duration) { f.clock = f.clock.Add(d) }

func (f *loginFixture) start(t *testing.T) string {
	t.Helper()
	status, data := apiJSON(t, f.h, "POST", loginStartPath, f.owner, `{"runtime":"claude"}`)
	if status != http.StatusOK {
		t.Fatalf("start: %d %v", status, data)
	}
	id, _ := data["login_id"].(string)
	if !strings.HasPrefix(id, "rl-") {
		t.Fatalf("start answered login_id %q", id)
	}
	return id
}

func (f *loginFixture) report(t *testing.T, token, body string) (int, map[string]any) {
	t.Helper()
	return apiJSON(t, f.h, "POST", loginReportPath, token, body)
}

func (f *loginFixture) awaitingCode(t *testing.T) string {
	t.Helper()
	id := f.start(t)
	if status, data := f.report(t, f.warden,
		`{"login_id":"`+id+`","state":"awaiting_code","auth_url":"`+loginTestURL+`"}`); status != http.StatusOK {
		t.Fatalf("awaiting_code report: %d %v", status, data)
	}
	return id
}

func (f *loginFixture) get(t *testing.T, id string) (int, map[string]any) {
	t.Helper()
	return apiJSON(t, f.h, "GET", loginStartPath+"/"+id, f.owner, "")
}

func loginBody(id, state string, authURL, account, reason any, ts float64) map[string]any {
	return map[string]any{
		"login_id": id, "machine_id": loginMachine, "runtime": "claude", "state": state,
		"auth_url": authURL, "account": account, "reason": reason, "updated_ts": ts,
	}
}

func loginSignal(id, trigger string) map[string]any {
	return map[string]any{
		"seq": apiAnyNumber, "topic": "runtime_login", "op": "signal",
		"data": map[string]any{
			"entity": "runtime_login", "key": id, "epoch": apiAnyNumber, "deleted": false, "payload": nil,
		},
		"ts": apiAnyNumber, "trigger": trigger,
	}
}

func wantNoWardenFrames(t *testing.T, f *loginFixture, machine string) {
	t.Helper()
	if frames := drainFrames(t, f.api, machine); len(frames) != 0 {
		t.Fatalf("%s was sent %d frame(s): %+v", machine, len(frames), frames)
	}
}

func TestRuntimeLoginStart(t *testing.T) {
	t.Run("under an online warden, the owner's start answers a starting login, sends login_start to that warden only, and signals the owner", func(t *testing.T) {
		f := newLoginFixture(t)
		status, data := apiJSON(t, f.h, "POST", loginStartPath, f.owner, `{"runtime":"claude"}`)
		if status != http.StatusOK {
			t.Fatalf("start: %d %v", status, data)
		}
		apiWantBody(t, data, loginBody(apiAnyString, "starting", nil, nil, nil, loginEpoch))
		id := data["login_id"].(string)
		apiWantValue(t, "frames", any(drainFrames(t, f.api, loginMachine)), any([]drainedFrame{{
			Topic: "warden-command", RPC: "login_start",
			Args: map[string]any{"member_id": loginMachine, "login_id": id, "runtime": "claude"},
		}}))
		wantNoWardenFrames(t, f, loginOtherMachine)
		f.dashboard.wantFrames(loginSignal(id, "owner"))
		assertNoFrame(t, f.wardenConn, "the owner-only runtime_login signal")
	})

	t.Run("under a login already in flight for the machine, a second start answers that login and sends nothing", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		drainFrames(t, f.api, loginMachine)
		f.dashboard.wantFrames(loginSignal(id, "owner"))
		f.advance(5 * time.Second)

		status, data := apiJSON(t, f.h, "POST", loginStartPath, f.owner, `{"runtime":"claude"}`)
		if status != http.StatusOK {
			t.Fatalf("repeat start: %d %v", status, data)
		}
		apiWantBody(t, data, loginBody(id, "starting", nil, nil, nil, loginEpoch))
		wantNoWardenFrames(t, f, loginMachine)
		f.dashboard.wantFrames()
	})

	t.Run("under a terminal login for the machine, a start begins a new login", func(t *testing.T) {
		f := newLoginFixture(t)
		first := f.start(t)
		if status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+first+"/cancel", f.owner, ""); status != http.StatusOK {
			t.Fatalf("cancel: %d %v", status, data)
		}
		drainFrames(t, f.api, loginMachine)
		second := f.start(t)
		if second == first {
			t.Fatalf("a start after a cancelled login answered the cancelled one (%s)", first)
		}
		apiWantValue(t, "frames", any(drainFrames(t, f.api, loginMachine)), any([]drainedFrame{{
			Topic: "warden-command", RPC: "login_start",
			Args: map[string]any{"member_id": loginMachine, "login_id": second, "runtime": "claude"},
		}}))
	})

	t.Run("under an admin agent caller, the start is allowed", func(t *testing.T) {
		f := newLoginFixture(t)
		admin := apiTestPrincipalToken(t, f.api, f.dal, principalAdminAgent, "adm-1")
		status, data := apiJSON(t, f.h, "POST", loginStartPath, admin, `{"runtime":"claude"}`)
		if status != http.StatusOK {
			t.Fatalf("admin start: %d %v", status, data)
		}
		apiWantBody(t, data, loginBody(apiAnyString, "starting", nil, nil, nil, loginEpoch))
	})

	t.Run("under a plain agent or the warden itself, the start is 403 and nothing is sent", func(t *testing.T) {
		f := newLoginFixture(t)
		plain := apiTestPrincipalToken(t, f.api, f.dal, principalAgent, "plain-1")
		for _, token := range []string{plain, f.warden} {
			status, data := apiJSON(t, f.h, "POST", loginStartPath, token, `{"runtime":"claude"}`)
			if status != http.StatusForbidden {
				t.Fatalf("start: want 403, got %d %v", status, data)
			}
		}
		wantNoWardenFrames(t, f, loginMachine)
		f.dashboard.wantFrames()
	})

	t.Run("under an offline warden, the start is 409 and no login exists afterwards", func(t *testing.T) {
		f := newLoginFixture(t)
		putWarden(t, f.api, "m-dark")
		status, data := apiJSON(t, f.h, "POST", "/api/machines/m-dark/runtime-login", f.owner, `{"runtime":"claude"}`)
		if status != http.StatusConflict {
			t.Fatalf("offline start: want 409, got %d %v", status, data)
		}
		apiWantError(t, data, "conflict", runtimeLoginOfflineMsg)
		wantNoWardenFrames(t, f, "m-dark")
		f.dashboard.wantFrames()
		f.api.runtimeLogins.mu.Lock()
		held := len(f.api.runtimeLogins.logins)
		f.api.runtimeLogins.mu.Unlock()
		if held != 0 {
			t.Fatalf("the refused start left %d login(s) held", held)
		}
	})

	t.Run("under an unknown id or a non-machine member, the start is 404", func(t *testing.T) {
		f := newLoginFixture(t)
		for _, id := range []string{"m-nope", apiTestPlainAgentID} {
			status, data := apiJSON(t, f.h, "POST", "/api/machines/"+id+"/runtime-login", f.owner, `{"runtime":"claude"}`)
			if status != http.StatusNotFound {
				t.Fatalf("start on %s: want 404, got %d %v", id, status, data)
			}
		}
	})

	t.Run("under a runtime other than claude, the start is 422", func(t *testing.T) {
		f := newLoginFixture(t)
		status, data := apiJSON(t, f.h, "POST", loginStartPath, f.owner, `{"runtime":"codex"}`)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("codex start: want 422, got %d %v", status, data)
		}
		wantNoWardenFrames(t, f, loginMachine)
	})
}

func TestRuntimeLoginReport(t *testing.T) {
	t.Run("under the login's own warden, awaiting_code stores the sign-in URL, the owner reads it back, and the owner is signalled", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		f.dashboard.wantFrames(loginSignal(id, "owner"))
		f.advance(3 * time.Second)

		status, data := f.report(t, f.warden, `{"login_id":"`+id+`","state":"awaiting_code","auth_url":"`+loginTestURL+`"}`)
		if status != http.StatusOK {
			t.Fatalf("report: %d %v", status, data)
		}
		want := loginBody(id, "awaiting_code", loginTestURL, nil, nil, loginEpoch+3)
		apiWantBody(t, data, want)
		f.dashboard.wantFrames(loginSignal(id, loginMachine))
		_, got := f.get(t, id)
		apiWantBody(t, got, want)
	})

	t.Run("under a succeeded report, the account is kept and a later report leaves the login unchanged", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		f.advance(time.Second)
		status, data := f.report(t, f.warden, `{"login_id":"`+id+`","state":"succeeded",`+
			`"account":{"email":"owner@example.test","org_name":"Example Org"}}`)
		if status != http.StatusOK {
			t.Fatalf("succeeded report: %d %v", status, data)
		}
		want := loginBody(id, "succeeded", loginTestURL,
			map[string]any{"email": "owner@example.test", "org_name": "Example Org"}, nil, loginEpoch+1)
		apiWantBody(t, data, want)

		f.advance(time.Second)
		status, data = f.report(t, f.warden, `{"login_id":"`+id+`","state":"failed","reason":"late"}`)
		if status != http.StatusOK {
			t.Fatalf("late report: %d %v", status, data)
		}
		apiWantBody(t, data, want)
	})

	t.Run("under a code the login process refused, an awaiting_code report carries its reason and the next relayed code clears it", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		f.advance(time.Second)
		if status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/code", f.owner, `{"code":"abc#def"}`); status != http.StatusOK {
			t.Fatalf("code: %d %v", status, data)
		}
		f.advance(time.Second)
		status, data := f.report(t, f.warden, `{"login_id":"`+id+`","state":"awaiting_code",`+
			`"reason":"Invalid code. Please make sure the full code was copied."}`)
		if status != http.StatusOK {
			t.Fatalf("report: %d %v", status, data)
		}
		apiWantBody(t, data, loginBody(id, "awaiting_code", loginTestURL, nil,
			"Invalid code. Please make sure the full code was copied.", loginEpoch+2))

		f.advance(time.Second)
		status, data = apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/code", f.owner, `{"code":"ghi#jkl"}`)
		if status != http.StatusOK {
			t.Fatalf("second code: %d %v", status, data)
		}
		apiWantBody(t, data, loginBody(id, "verifying", loginTestURL, nil, nil, loginEpoch+3))
	})

	t.Run("under a failed report, the reason is kept", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		status, data := f.report(t, f.warden, `{"login_id":"`+id+`","state":"failed",`+
			`"reason":"Login failed: Request failed with status code 400"}`)
		if status != http.StatusOK {
			t.Fatalf("failed report: %d %v", status, data)
		}
		apiWantBody(t, data, loginBody(id, "failed", loginTestURL, nil,
			"Login failed: Request failed with status code 400", loginEpoch))
	})

	t.Run("under another machine's warden, the report is 404, the login is unchanged and nobody is signalled", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		f.dashboard.wantFrames(loginSignal(id, "owner"))
		status, data := f.report(t, f.other, `{"login_id":"`+id+`","state":"awaiting_code","auth_url":"https://evil.test/"}`)
		if status != http.StatusNotFound {
			t.Fatalf("foreign report: want 404, got %d %v", status, data)
		}
		apiWantError(t, data, "not_found", "runtime login '"+id+"' not found")
		f.dashboard.wantFrames()
		_, got := f.get(t, id)
		apiWantBody(t, got, loginBody(id, "starting", nil, nil, nil, loginEpoch))
	})

	t.Run("under a caller that is not a machine, the report is 404", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		agent := apiTestAgentToken(t, f.api, apiTestPlainAgentID, "")
		status, data := f.report(t, agent, `{"login_id":"`+id+`","state":"awaiting_code","auth_url":"https://evil.test/"}`)
		if status != http.StatusNotFound {
			t.Fatalf("agent report: want 404, got %d %v", status, data)
		}
		_, got := f.get(t, id)
		apiWantBody(t, got, loginBody(id, "starting", nil, nil, nil, loginEpoch))
	})

	t.Run("under a starting report, the report is 422", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		status, data := f.report(t, f.warden, `{"login_id":"`+id+`","state":"starting"}`)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("starting report: want 422, got %d %v", status, data)
		}
	})

	t.Run("under an unknown login id, the report is 404", func(t *testing.T) {
		f := newLoginFixture(t)
		status, data := f.report(t, f.warden, `{"login_id":"rl-nope","state":"verifying"}`)
		if status != http.StatusNotFound {
			t.Fatalf("unknown report: want 404, got %d %v", status, data)
		}
	})
}

func TestRuntimeLoginCode(t *testing.T) {
	t.Run("under awaiting_code, the code is relayed to the warden in a login_code frame and the login moves to verifying", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		drainFrames(t, f.api, loginMachine)
		f.dashboard.wantFrames(loginSignal(id, "owner"), loginSignal(id, loginMachine))
		f.advance(2 * time.Second)

		status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/code", f.owner,
			`{"code":"  `+loginTestCode+`\n"}`)
		if status != http.StatusOK {
			t.Fatalf("code: %d %v", status, data)
		}
		apiWantBody(t, data, loginBody(id, "verifying", loginTestURL, nil, nil, loginEpoch+2))
		apiWantValue(t, "frames", any(drainFrames(t, f.api, loginMachine)), any([]drainedFrame{{
			Topic: "warden-command", RPC: "login_code",
			Args: map[string]any{"member_id": loginMachine, "login_id": id, "code": loginTestCode},
		}}))
		wantNoWardenFrames(t, f, loginOtherMachine)
		f.dashboard.wantFrames(loginSignal(id, "owner"))
	})

	t.Run("under a partial code, the code is 422, nothing is sent and the login stays awaiting_code", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		drainFrames(t, f.api, loginMachine)
		f.dashboard.wantFrames(loginSignal(id, "owner"), loginSignal(id, loginMachine))
		for _, code := range []string{"needle-code-9b7e", "needle-code-9b7e#", "#state", "a#b#c"} {
			status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/code", f.owner, `{"code":"`+code+`"}`)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("code %q: want 422, got %d %v", code, status, data)
			}
			apiWantError(t, data, "validation_error",
				"code is incomplete: copy the whole code the sign-in page shows (two parts joined by '#')")
		}
		wantNoWardenFrames(t, f, loginMachine)
		f.dashboard.wantFrames()
		_, got := f.get(t, id)
		apiWantBody(t, got, loginBody(id, "awaiting_code", loginTestURL, nil, nil, loginEpoch))
	})

	t.Run("under a login that is not awaiting_code, the code is 409 and nothing is sent", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		drainFrames(t, f.api, loginMachine)
		status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/code", f.owner, `{"code":"abc#def"}`)
		if status != http.StatusConflict {
			t.Fatalf("code while starting: want 409, got %d %v", status, data)
		}
		apiWantError(t, data, "conflict",
			"runtime login '"+id+"' is starting, not awaiting_code")
		wantNoWardenFrames(t, f, loginMachine)
	})

	t.Run("under a warden that went offline, the code is 409 and the login stays awaiting_code", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		putWarden(t, api, loginMachine)
		warden, err := api.mintWardenToken(Member{ID: loginMachine, Kind: KindWarden})
		if err != nil {
			t.Fatal(err)
		}
		conn, err := api.hub.Connect(loginMachine, "")
		if err != nil {
			t.Fatal(err)
		}
		f := &loginFixture{api: api, h: h, owner: owner, warden: warden, clock: time.Unix(loginEpoch, 0)}
		api.runtimeLogins.now = func() time.Time { return f.clock }
		id := f.awaitingCode(t)
		drainFrames(t, api, loginMachine)
		api.hub.Disconnect(conn)

		status, data := apiJSON(t, h, "POST", loginStartPath+"/"+id+"/code", owner, `{"code":"abc#def"}`)
		if status != http.StatusConflict {
			t.Fatalf("code to an offline warden: want 409, got %d %v", status, data)
		}
		_, got := f.get(t, id)
		apiWantBody(t, got, loginBody(id, "awaiting_code", loginTestURL, nil, nil, loginEpoch))
	})

	t.Run("under a blank code, the code is 422", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/code", f.owner, `{"code":"   "}`)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("blank code: want 422, got %d %v", status, data)
		}
	})

	t.Run("under an unknown login, a malformed code is still 404, not 422", func(t *testing.T) {
		f := newLoginFixture(t)
		f.awaitingCode(t)
		status, data := apiJSON(t, f.h, "POST", loginStartPath+"/rl-nope/code", f.owner, `{"code":"conf-code"}`)
		if status != http.StatusNotFound {
			t.Fatalf("want 404, got %d %v", status, data)
		}
		apiWantError(t, data, "not_found", "runtime login 'rl-nope' not found")
	})

	t.Run("under a login that is not awaiting_code, a malformed code is 409, not 422", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/code", f.owner, `{"code":"conf-code"}`)
		if status != http.StatusConflict {
			t.Fatalf("want 409, got %d %v", status, data)
		}
	})

	t.Run("under an unknown login or a login on another machine's path, the code is 404", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		for _, path := range []string{
			loginStartPath + "/rl-nope/code",
			"/api/machines/" + loginOtherMachine + "/runtime-login/" + id + "/code",
		} {
			status, data := apiJSON(t, f.h, "POST", path, f.owner, `{"code":"abc#def"}`)
			if status != http.StatusNotFound {
				t.Fatalf("%s: want 404, got %d %v", path, status, data)
			}
		}
		wantNoWardenFrames(t, f, loginOtherMachine)
	})
}

func TestRuntimeLoginCancel(t *testing.T) {
	t.Run("under a login in flight, cancel ends it at once, sends login_cancel to the warden and signals the owner", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		drainFrames(t, f.api, loginMachine)
		f.dashboard.wantFrames(loginSignal(id, "owner"), loginSignal(id, loginMachine))
		f.advance(4 * time.Second)

		status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/cancel", f.owner, "")
		if status != http.StatusOK {
			t.Fatalf("cancel: %d %v", status, data)
		}
		want := loginBody(id, "cancelled", loginTestURL, nil, "cancelled by the owner", loginEpoch+4)
		apiWantBody(t, data, want)
		apiWantValue(t, "frames", any(drainFrames(t, f.api, loginMachine)), any([]drainedFrame{{
			Topic: "warden-command", RPC: "login_cancel",
			Args: map[string]any{"member_id": loginMachine, "login_id": id},
		}}))
		f.dashboard.wantFrames(loginSignal(id, "owner"))

		status, data = f.report(t, f.warden, `{"login_id":"`+id+`","state":"verifying"}`)
		if status != http.StatusOK {
			t.Fatalf("report after cancel: %d %v", status, data)
		}
		apiWantBody(t, data, want)
	})

	t.Run("under a terminal login, cancel answers it unchanged and sends nothing", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		f.report(t, f.warden, `{"login_id":"`+id+`","state":"failed","reason":"boom"}`)
		drainFrames(t, f.api, loginMachine)
		f.advance(time.Second)
		status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/cancel", f.owner, "")
		if status != http.StatusOK {
			t.Fatalf("cancel: %d %v", status, data)
		}
		apiWantBody(t, data, loginBody(id, "failed", loginTestURL, nil, "boom", loginEpoch))
		wantNoWardenFrames(t, f, loginMachine)
	})

	t.Run("under an unknown login, cancel is 404", func(t *testing.T) {
		f := newLoginFixture(t)
		status, data := apiJSON(t, f.h, "POST", loginStartPath+"/rl-nope/cancel", f.owner, "")
		if status != http.StatusNotFound {
			t.Fatalf("cancel unknown: want 404, got %d %v", status, data)
		}
	})
}

func TestRuntimeLoginLifetime(t *testing.T) {
	t.Run("under 15 minutes without a warden report, the login becomes expired and the sweep signals the owner", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		f.dashboard.wantFrames(loginSignal(id, "owner"))

		f.advance(15*time.Minute - time.Second)
		f.api.sweepRuntimeLogins()
		f.dashboard.wantFrames()
		_, got := f.get(t, id)
		apiWantBody(t, got, loginBody(id, "starting", nil, nil, nil, loginEpoch))

		f.advance(time.Second)
		f.api.sweepRuntimeLogins()
		f.dashboard.wantFrames(loginSignal(id, "server"))
		_, got = f.get(t, id)
		apiWantBody(t, got, loginBody(id, "expired", nil, nil,
			"no report from the machine for 15 minutes", loginEpoch+15*60))
	})

	t.Run("under a warden report, the 15 minutes count from that report", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.start(t)
		f.advance(10 * time.Minute)
		f.report(t, f.warden, `{"login_id":"`+id+`","state":"awaiting_code","auth_url":"`+loginTestURL+`"}`)
		f.advance(10 * time.Minute)
		_, got := f.get(t, id)
		apiWantBody(t, got, loginBody(id, "awaiting_code", loginTestURL, nil, nil, loginEpoch+10*60))
	})

	t.Run("under 10 minutes after a terminal state, the login is dropped and reads 404", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		f.report(t, f.warden, `{"login_id":"`+id+`","state":"failed","reason":"boom"}`)
		f.dashboard.wantFrames(loginSignal(id, "owner"), loginSignal(id, loginMachine), loginSignal(id, loginMachine))

		f.advance(10*time.Minute - time.Second)
		if status, data := f.get(t, id); status != http.StatusOK {
			t.Fatalf("just before the drop: want 200, got %d %v", status, data)
		}
		f.advance(time.Second)
		f.api.sweepRuntimeLogins()
		f.dashboard.wantFrames(loginSignal(id, "server"))
		if status, data := f.get(t, id); status != http.StatusNotFound {
			t.Fatalf("after the drop: want 404, got %d %v", status, data)
		}
	})
}

// scanDatabaseFor reads every text-bearing column of every table.
func scanDatabaseFor(t *testing.T, d *DAL, needle string) []string {
	t.Helper()
	db := d.rdb.raw
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	var hits []string
	for _, table := range tables {
		hits = append(hits, scanTableFor(t, db, table, needle)...)
	}
	return hits
}

func scanTableFor(t *testing.T, db *sql.DB, table, needle string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM "` + table + `"`)
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range values {
			var text string
			switch x := v.(type) {
			case string:
				text = x
			case []byte:
				text = string(x)
			default:
				continue
			}
			if strings.Contains(text, needle) {
				hits = append(hits, fmt.Sprintf("%s.%s", table, cols[i]))
			}
		}
	}
	return hits
}

func TestRuntimeLoginLeavesNoCopyAtRest(t *testing.T) {
	t.Run("under a whole login, neither the code nor the sign-in URL is in any table, while values written through ordinary paths are", func(t *testing.T) {
		f := newLoginFixture(t)
		id := f.awaitingCode(t)
		if status, data := apiJSON(t, f.h, "POST", loginStartPath+"/"+id+"/code", f.owner,
			`{"code":"`+loginTestCode+`"}`); status != http.StatusOK {
			t.Fatalf("code: %d %v", status, data)
		}
		if status, data := f.report(t, f.warden, `{"login_id":"`+id+`","state":"succeeded",`+
			`"account":{"email":"owner@example.test","org_name":"Example Org"}}`); status != http.StatusOK {
			t.Fatalf("succeeded: %d %v", status, data)
		}

		const renameNeedle = "needle-rename-31d0"
		if status, data := apiJSON(t, f.h, "PATCH", "/api/machines/"+loginMachine, f.owner,
			`{"display_name":"`+renameNeedle+`"}`); status != http.StatusOK {
			t.Fatalf("rename control: %d %v", status, data)
		}
		if status, data := apiJSON(t, f.h, "POST", "/api/machines/"+loginOtherMachine+"/upgrade", f.owner, ""); status != http.StatusOK {
			t.Fatalf("upgrade control: %d %v", status, data)
		}

		if hits := scanDatabaseFor(t, f.dal, renameNeedle); len(hits) == 0 {
			t.Fatal("control: a display name written through PATCH was not found — the scan reads nothing")
		}
		if hits := scanDatabaseFor(t, f.dal, `"rpc":"update"`); len(hits) == 0 {
			t.Fatal("control: the persisted upgrade frame was not found — the scan does not reach the command store")
		}
		for _, secret := range []string{loginTestCode, loginTestURL, "needle-url-4f1c", "needle-code-9b7e"} {
			if hits := scanDatabaseFor(t, f.dal, secret); len(hits) != 0 {
				t.Errorf("%q is at rest in %v", secret, hits)
			}
		}
		m, err := f.dal.GetMember(loginMachine)
		if err != nil || m == nil {
			t.Fatalf("read machine row: %v", err)
		}
		if m.LastOp != "" || m.LastOpLog != "" || m.LastOpReason != "" {
			t.Errorf("the login wrote last_op on the machine: op=%q log=%q reason=%q", m.LastOp, m.LastOpLog, m.LastOpReason)
		}
	})
}
