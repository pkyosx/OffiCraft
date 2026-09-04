package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// T-ce3d — bootstrap-here's OC_BASE comes from the SERVER, not from whoever
// called it, and the verb refuses to act for any machine but the server's own.
//
// 🔴 Why this file exists: the base used to be requestBaseURL(r) = scheme://
// r.Host, and mcp.go's loopbackCall synthesises its request with a literal
// `Host: "loopback"`. So every AI-triggered reinstall handed the installer
// OC_BASE=http://loopback — a warden that can never call home. It was not a
// flaky failure, it was structural: the only path that worked was a browser,
// and the cockpit button for it is disabled while the machine is online.
//
// The first-run onboarding path (onboarding.go) has always passed s.selfBase.
// This pins that the button agrees with it, so the two cannot drift into two
// answers again.
//
// The broken URL was also, by accident, the only thing stopping a caller from
// installing ANOTHER machine's identity over this host's warden. Fixing the
// base removes that accident, so bootstrapHereRefusal now says it out loud —
// the same shape and the same 409 teardown-here has carried since T-42a0.

// installBaseTestSelfBase is deliberately NOT the default 127.0.0.1:7755: a
// fixture on the default port cannot tell s.selfBase apart from a hardcoded
// literal, and a fix that hardcoded the default would pass.
const installBaseTestSelfBase = "http://127.0.0.1:59123"

func ocwardenInstallBaseServer(t *testing.T) *apiServer {
	t.Helper()
	s := newMachinesTestServer(t)
	s.selfBase = installBaseTestSelfBase
	s.binCacheDir = filepath.Join(t.TempDir(), "cache-bin")
	s.ocwardenFS = fstest.MapFS{
		"ocwarden":  {Data: []byte("fake warden — never exec'd")},
		"officraft": {Data: []byte("fake anchor — never exec'd")},
	}
	putTestMember(t, s, Member{
		ID: ServerSelfHost, Name: "this server", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	putTestMember(t, s, Member{
		ID: "m-remote", Name: "remote", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateUninstall, RosterStatus: RosterStatusActive,
	})
	return s
}

func ocBaseOf(t *testing.T, env []string) string {
	t.Helper()
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "OC_BASE="); ok {
			return v
		}
	}
	t.Fatalf("the child env carries no OC_BASE at all: %v", env)
	return ""
}

func postBootstrapHereFor(t *testing.T, s *apiServer, id, host string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/machines/"+id+"/bootstrap-here", nil)
	req.Host = host
	s.HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost(rec, req, id)
	return rec
}

func TestBootstrapHereBaseIgnoresTheCallersHost(t *testing.T) {
	// Every one of these is a Host a real caller produces. "loopback" is the
	// one mcp.go writes, and it is the reason this test exists; the others are
	// the browser paths, present so a fix that merely special-cased the string
	// "loopback" would still have to answer for them.
	for _, host := range []string{"loopback", "officraft.example.com", "127.0.0.1:7755", "attacker.example"} {
		t.Run("Host: "+host, func(t *testing.T) {
			s := ocwardenInstallBaseServer(t)
			runs := withRecordedOcwarden(t, 0)

			rec := postBootstrapHereFor(t, s, ServerSelfHost, host)
			if rec.Code != http.StatusOK {
				t.Fatalf("bootstrap-here: %d %s", rec.Code, rec.Body.String())
			}
			if len(*runs) != 1 {
				t.Fatalf("expected exactly one ocwarden invocation, got %d", len(*runs))
			}
			// The literal, not s.selfBase: `got != s.selfBase` is self-referential
			// and would pass even with selfBase left empty.
			if got := ocBaseOf(t, (*runs)[0].env); got != installBaseTestSelfBase {
				t.Fatalf("OC_BASE = %q, want the server's own base %q — the caller's Host reached the installer",
					got, installBaseTestSelfBase)
			}
		})
	}
}

// MUTANT: make bootstrapHereRefusal return "" for a non-self target — this test
// goes red on all three of its claims (the 409, "nothing was executed", and the
// roster row untouched), because the handler falls through to the install and to
// the residual-uninstall write.
func TestBootstrapHere_NamingAnotherMachineIsRefused(t *testing.T) {
	s := ocwardenInstallBaseServer(t)
	runs := withRecordedOcwarden(t, 0)

	rec := postBootstrapHereFor(t, s, "m-remote", "officraft.example.com")

	if rec.Code != http.StatusConflict {
		t.Fatalf("bootstrap-here aimed at another machine: want 409, got %d %s "+
			"— the verb installs on THIS host, so a 200 means it overwrote this "+
			"host's warden with m-remote's identity", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"conflict"`) {
		t.Fatalf("refusal must ride the conflict envelope, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "m-remote") {
		t.Fatalf("the refusal must name the machine it was asked about, got %s",
			rec.Body.String())
	}
	// The guard has to precede the subprocess AND the roster write: a 409
	// returned after `ocwarden install` ran is the whole incident.
	if len(*runs) != 0 {
		t.Fatalf("a refused bootstrap must never exec ocwarden, got %d run(s): %+v",
			len(*runs), *runs)
	}
	if got := desiredStateOf(t, s, "m-remote"); got != DesiredStateUninstall {
		t.Fatalf("a refused bootstrap must not touch the named machine's row, got desired_state=%q", got)
	}
}

// The paths that legitimately need an EXTERNALLY reachable address still take it
// from the request Host, and this asserts it THROUGH THE HANDLERS — not by
// calling requestBaseURL directly, which controls nothing about whether the
// handlers still call it. A "fix" that routed these through selfBase too would
// hand a remote machine a curl of 127.0.0.1.
func TestRemoteInstallSurfacesStillUseTheRequestHost(t *testing.T) {
	const host = "officraft.example.com"

	// ⚠️ THE SCHEME HERE MOVED http→https (T-78, owner 2026-09-04). What this
	// test exists to pin did NOT move: these surfaces must still take the HOST
	// from the request, not from s.selfBase — a "fix" that routed them through
	// selfBase would hand a remote machine a curl of 127.0.0.1, which is the
	// bug T-ce3d was written for. Only the scheme half is different now, and it
	// is no longer read off r.TLS at all (requestBaseURL → baseURLForHost):
	// behind a TLS-terminating proxy r.TLS is nil for every caller, so the old
	// expectation encoded a value this server should never hand out again.
	const wantBase = "https://" + host

	t.Run("GET boot-command", func(t *testing.T) {
		s := ocwardenInstallBaseServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/machines/m-remote/boot-command", nil)
		req.Host = host
		s.HandleMachineBootCommandApiMachinesMachineIdBootCommandGet(rec, req, "m-remote")
		if rec.Code != http.StatusOK {
			t.Fatalf("boot-command: %d %s", rec.Code, rec.Body.String())
		}
		var dto bootCommandResultDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !strings.Contains(dto.BootCommand, wantBase+"/install.sh?code="+dto.ClaimCode) {
			t.Fatalf("boot_command = %q, want the caller-reachable base %q — a machine "+
				"that is not this host cannot curl the server's own selfBase",
				dto.BootCommand, wantBase)
		}
	})

	t.Run("POST onboard", func(t *testing.T) {
		s := ocwardenInstallBaseServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/machines",
			strings.NewReader(`{"display_name":"a new box"}`))
		req.Host = host
		s.HandleOnboardMachineApiMachinesPost(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("onboard: %d %s", rec.Code, rec.Body.String())
		}
		var dto machineOnboardResultDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !strings.Contains(dto.BootCommand, wantBase+"/install.sh?code="+dto.ClaimCode) {
			t.Fatalf("boot_command = %q, want the caller-reachable base %q",
				dto.BootCommand, wantBase)
		}
	})

	t.Run("GET install.sh", func(t *testing.T) {
		s := ocwardenInstallBaseServer(t)
		code := "CODE1"
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/install.sh?code="+code, nil)
		req.Host = host
		s.HandleInstallScriptInstallShGet(rec, req, HandleInstallScriptInstallShGetParams{Code: &code})
		if rec.Code != http.StatusOK {
			t.Fatalf("install.sh: %d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), wantBase) {
			t.Fatalf("the served installer must point back at the caller-reachable base %q, got:\n%s",
				wantBase, rec.Body.String())
		}
	})
}
