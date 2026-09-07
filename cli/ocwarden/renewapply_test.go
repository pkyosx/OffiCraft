package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// sentRequest is one request a seam closure actually put on the wire.
type sentRequest struct {
	method string
	url    string
	agent  string
	accept string
	auth   string
	body   string
}

// recordingClient answers every request with status/body and records what went out.
func recordingClient(status int, body string, transportErr error) (*http.Client, *[]sentRequest) {
	var sent []sentRequest
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw := ""
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			raw = string(b)
		}
		sent = append(sent, sentRequest{
			method: r.Method, url: r.URL.String(),
			agent:  r.Header.Get("User-Agent"),
			accept: r.Header.Get("Accept"),
			auth:   r.Header.Get("Authorization"),
			body:   raw,
		})
		if transportErr != nil {
			return nil, transportErr
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{},
		}, nil
	})}
	return client, &sent
}

func TestHttpCredentialRenewer(t *testing.T) {
	t.Run("a 200 with a token is decoded and the request names no target", func(t *testing.T) {
		client, sent := recordingClient(http.StatusOK, `{"token":"`+credIssuedAtMillion+`","machine_id":"warden-1"}`, nil)

		status, body, err := httpCredentialRenewer(client, "https://station.example", jwtWardenOne)()

		if status != http.StatusOK || err != nil {
			t.Fatalf("renew = (%d, %v), want (200, nil)", status, err)
		}
		want := map[string]any{"token": credIssuedAtMillion, "machine_id": "warden-1"}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("body = %#v, want %#v", body, want)
		}
		wantSent := []sentRequest{{
			method: http.MethodPost,
			url:    "https://station.example/api/machines/renew-credential",
			agent:  "ocwarden/0.1",
			accept: "application/json",
			auth:   "Bearer " + jwtWardenOne,
			body:   "",
		}}
		if !reflect.DeepEqual(*sent, wantSent) {
			t.Errorf("wire = %#v, want %#v", *sent, wantSent)
		}
	})

	t.Run("an empty credential sends no Authorization header", func(t *testing.T) {
		client, sent := recordingClient(http.StatusOK, `{}`, nil)

		if _, _, err := httpCredentialRenewer(client, "https://station.example", "")(); err != nil {
			t.Fatalf("renew = %v", err)
		}
		if len(*sent) != 1 || (*sent)[0].auth != "" {
			t.Errorf("wire = %#v, want one request with no Authorization header", *sent)
		}
	})

	t.Run("a refusal is a status, not an error", func(t *testing.T) {
		client, _ := recordingClient(http.StatusForbidden, `{"detail":"nope"}`, nil)

		status, body, err := httpCredentialRenewer(client, "https://station.example", jwtWardenOne)()

		if status != http.StatusForbidden || err != nil {
			t.Errorf("renew = (%d, %v), want (403, nil)", status, err)
		}
		if want := map[string]any{"detail": "nope"}; !reflect.DeepEqual(body, want) {
			t.Errorf("body = %#v, want %#v", body, want)
		}
	})

	t.Run("a body that is not JSON leaves the decode empty without failing", func(t *testing.T) {
		client, _ := recordingClient(http.StatusOK, "<html>gateway</html>", nil)

		status, body, err := httpCredentialRenewer(client, "https://station.example", jwtWardenOne)()

		if status != http.StatusOK || err != nil || body != nil {
			t.Errorf("renew = (%d, %#v, %v), want (200, nil, nil)", status, body, err)
		}
	})

	t.Run("never reaching the station is an error with no status", func(t *testing.T) {
		client, _ := recordingClient(0, "", errors.New("dial tcp: connection refused"))

		status, body, err := httpCredentialRenewer(client, "https://station.example", jwtWardenOne)()

		if status != 0 || body != nil {
			t.Errorf("renew = (%d, %#v), want (0, nil)", status, body)
		}
		if err == nil || !strings.Contains(err.Error(), "connection refused") {
			t.Errorf("err = %v, want the transport failure", err)
		}
	})
}

func TestHttpCredentialVerifier(t *testing.T) {
	t.Run("the CANDIDATE credential is what is presented, at a read-only endpoint", func(t *testing.T) {
		client, sent := recordingClient(http.StatusOK, `[]`, nil)

		status, err := httpCredentialVerifier(client, "https://station.example")(credIssuedAtMillion)

		if status != http.StatusOK || err != nil {
			t.Fatalf("verify = (%d, %v), want (200, nil)", status, err)
		}
		wantSent := []sentRequest{{
			method: http.MethodGet,
			url:    "https://station.example/api/machines",
			agent:  "ocwarden/0.1",
			auth:   "Bearer " + credIssuedAtMillion,
		}}
		if !reflect.DeepEqual(*sent, wantSent) {
			t.Errorf("wire = %#v, want %#v", *sent, wantSent)
		}
	})

	t.Run("a refusal comes back as its status", func(t *testing.T) {
		client, _ := recordingClient(http.StatusUnauthorized, `{"detail":"bad credential"}`, nil)

		status, err := httpCredentialVerifier(client, "https://station.example")(credIssuedAtMillion)

		if status != http.StatusUnauthorized || err != nil {
			t.Errorf("verify = (%d, %v), want (401, nil)", status, err)
		}
	})

	t.Run("a transport failure is an error, not a refusal", func(t *testing.T) {
		client, _ := recordingClient(0, "", errors.New("i/o timeout"))

		status, err := httpCredentialVerifier(client, "https://station.example")(credIssuedAtMillion)

		if status != 0 {
			t.Errorf("status = %d, want 0", status)
		}
		if err == nil || !strings.Contains(err.Error(), "i/o timeout") {
			t.Errorf("err = %v, want the transport failure", err)
		}
	})
}

func TestNewRenewalWiring(t *testing.T) {
	cases := []struct {
		name        string
		cfg         Config
		env         map[string]string
		wantTokfile string
		wantToken   string
		wantEnvTok  string
	}{
		{"the default token file follows HOME",
			Config{Base: "https://station.example", Token: credIssuedAtMillion},
			map[string]string{"HOME": "/Users/eva"},
			"/Users/eva/.officraft/warden/exec-warden.tok", credIssuedAtMillion, ""},
		{"an explicit OC_WARDEN_TOKFILE wins",
			Config{Base: "https://station.example", Token: credIssuedAtMillion},
			map[string]string{"HOME": "/Users/eva", "OC_WARDEN_TOKFILE": "/custom/warden.tok"},
			"/custom/warden.tok", credIssuedAtMillion, ""},
		{"a namespace moves the token file with it",
			Config{Base: "https://station.example", Token: credIssuedAtMillion},
			map[string]string{"HOME": "/Users/eva", "OC_NAMESPACE": "beta"},
			"/Users/eva/.officraft-beta/warden/exec-warden.tok", credIssuedAtMillion, ""},
		{"an invalid namespace resolves to no path at all, rather than a guessed one",
			Config{Base: "https://station.example", Token: credIssuedAtMillion},
			map[string]string{"HOME": "/Users/eva", "OC_NAMESPACE": "NOT VALID"},
			"", credIssuedAtMillion, ""},
		{"no HOME resolves to no path",
			Config{Base: "https://station.example", Token: credIssuedAtMillion},
			map[string]string{},
			"", credIssuedAtMillion, ""},
		{"an OC_TOKEN exported by whoever started this process is carried separately",
			Config{Base: "https://station.example", Token: credIssuedAtMillion},
			map[string]string{"HOME": "/Users/eva", "OC_TOKEN": credIssuedAtMillion},
			"/Users/eva/.officraft/warden/exec-warden.tok", credIssuedAtMillion, credIssuedAtMillion},
	}
	for _, c := range cases {
		w := newRenewalWiring(c.cfg, envMap(c.env))
		if w.tokfilePath != c.wantTokfile {
			t.Errorf("%s: tokfilePath = %q, want %q", c.name, w.tokfilePath, c.wantTokfile)
		}
		if w.token != c.wantToken {
			t.Errorf("%s: token = %q, want %q", c.name, w.token, c.wantToken)
		}
		if w.envToken != c.wantEnvTok {
			t.Errorf("%s: envToken = %q, want %q", c.name, w.envToken, c.wantEnvTok)
		}
		if w.renew == nil || w.verify == nil || w.writeTok == nil {
			t.Errorf("%s: wiring left renew/verify/writeTok at %v/%v/%v, want all three built",
				c.name, w.renew == nil, w.verify == nil, w.writeTok == nil)
		}
	}

	t.Run("the token file it names is the one the write actually lands in", func(t *testing.T) {
		home := t.TempDir()
		w := newRenewalWiring(Config{Base: "https://station.example", Token: credIssuedAtMillion},
			envMap(map[string]string{"HOME": home}))

		if err := w.writeTok(w.tokfilePath, credIssuedAtMillion); err != nil {
			t.Fatalf("writeTok = %v", err)
		}
		want := filepath.Join(home, ".officraft", "warden", "exec-warden.tok")
		if w.tokfilePath != want {
			t.Fatalf("tokfilePath = %q, want %q", w.tokfilePath, want)
		}
		if got := readTokfile(envMap(map[string]string{"HOME": home}), os.ReadFile); got != credIssuedAtMillion {
			t.Errorf("the credential read back = %q, want %q", got, credIssuedAtMillion)
		}
	})
}

func TestApply(t *testing.T) {
	w := renewalWiring{
		renew:       func() (int, map[string]any, error) { return 201, map[string]any{"from": "renew"}, nil },
		verify:      func(string) (int, error) { return 202, nil },
		writeTok:    func(string, string) error { return errors.New("from writeTok") },
		tokfilePath: "/Users/eva/.officraft/warden/exec-warden.tok",
		token:       credIssuedAtMillion,
		envToken:    credNoIatNoExp,
	}
	u := &updater{}

	u.apply(w)

	if status, body, _ := u.renew(); status != 201 || !reflect.DeepEqual(body, map[string]any{"from": "renew"}) {
		t.Errorf("renew = (%d, %#v), want the wiring's renewer", status, body)
	}
	if status, _ := u.verify("x"); status != 202 {
		t.Errorf("verify = %d, want the wiring's verifier", status)
	}
	if err := u.writeTok("", ""); err == nil || err.Error() != "from writeTok" {
		t.Errorf("writeTok = %v, want the wiring's writer", err)
	}
	if u.tokfilePath != w.tokfilePath || u.token != w.token || u.envToken != w.envToken {
		t.Errorf("carried (%q, %q, %q), want (%q, %q, %q)",
			u.tokfilePath, u.token, u.envToken, w.tokfilePath, w.token, w.envToken)
	}
}

func TestRefreshCredentialPolicy(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		getErr   error
		start    int64
		want     int64
		wantPath []string
	}{
		{"a positive lifetime is remembered", http.StatusOK, `{"lifetime_secs":604800}`, nil,
			0, 604800, []string{credentialPolicyPath}},
		{"a later answer replaces the earlier one", http.StatusOK, `{"lifetime_secs":86400}`, nil,
			604800, 86400, []string{credentialPolicyPath}},
		{"a station that has not been upgraded leaves the number alone", http.StatusNotFound, `{"detail":"no"}`, nil,
			604800, 604800, []string{credentialPolicyPath}},
		{"a server error leaves the number alone", http.StatusInternalServerError, ``, nil,
			604800, 604800, []string{credentialPolicyPath}},
		{"a network blink leaves the number alone", 0, ``, errors.New("i/o timeout"),
			604800, 604800, []string{credentialPolicyPath}},
		{"a body that is not JSON leaves the number alone", http.StatusOK, `<html>`, nil,
			604800, 604800, []string{credentialPolicyPath}},
		{"a renamed or absent field leaves the number alone rather than landing as zero", http.StatusOK, `{"lifetime":604800}`, nil,
			604800, 604800, []string{credentialPolicyPath}},
		{"an explicit null leaves the number alone", http.StatusOK, `{"lifetime_secs":null}`, nil,
			604800, 604800, []string{credentialPolicyPath}},
		{"a zero lifetime is declined", http.StatusOK, `{"lifetime_secs":0}`, nil,
			604800, 604800, []string{credentialPolicyPath}},
		{"a negative lifetime is declined", http.StatusOK, `{"lifetime_secs":-1}`, nil,
			604800, 604800, []string{credentialPolicyPath}},
	}
	for _, c := range cases {
		var asked []string
		var log []string
		u := &updater{
			credLifetimeSecs: c.start,
			logf:             func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) },
			get: func(path string) (int, []byte, error) {
				asked = append(asked, path)
				return c.status, []byte(c.body), c.getErr
			},
		}

		u.refreshCredentialPolicy()

		if u.credLifetimeSecs != c.want {
			t.Errorf("%s: credLifetimeSecs = %d, want %d", c.name, u.credLifetimeSecs, c.want)
		}
		if !reflect.DeepEqual(asked, c.wantPath) {
			t.Errorf("%s: asked %v, want %v", c.name, asked, c.wantPath)
		}
		if len(log) != 0 {
			t.Errorf("%s: log = %#v, want silence — an unknown threshold is not an incident", c.name, log)
		}
	}

	t.Run("an unwired getter asks nothing and keeps the number", func(t *testing.T) {
		u := &updater{credLifetimeSecs: 604800}
		u.refreshCredentialPolicy()
		if u.credLifetimeSecs != 604800 {
			t.Errorf("credLifetimeSecs = %d, want 604800", u.credLifetimeSecs)
		}
	})
}

// renewalFixture is an updater wired for one renewal attempt, plus the record of
// everything the attempt did to the outside world.
type renewalFixture struct {
	u        *updater
	log      []string
	writes   [][2]string
	verified []string
	renews   int
	policies int
}

func newRenewalFixture(t *testing.T) *renewalFixture {
	t.Helper()
	f := &renewalFixture{}
	f.u = &updater{
		agentID:     "warden-1",
		token:       credIssuedAtMillion,
		tokfilePath: "/Users/eva/.officraft/warden/exec-warden.tok",
		now:         func() time.Time { return time.Unix(1000000, 0).Add(25 * 24 * time.Hour) },
		logf:        func(format string, a ...any) { f.log = append(f.log, fmt.Sprintf(format, a...)) },
		get: func(path string) (int, []byte, error) {
			f.policies++
			return http.StatusOK, []byte(`{"lifetime_secs":2592000}`), nil
		},
		renew: func() (int, map[string]any, error) {
			f.renews++
			return http.StatusOK, map[string]any{"token": credFreshForWardenOne}, nil
		},
		verify: func(candidate string) (int, error) {
			f.verified = append(f.verified, candidate)
			return http.StatusOK, nil
		},
		writeTok: func(path, token string) error {
			f.writes = append(f.writes, [2]string{path, token})
			return nil
		},
	}
	return f
}

func TestMaybeRenewCredential(t *testing.T) {
	t.Run("a due credential is replaced and the caller is told to re-exec", func(t *testing.T) {
		f := newRenewalFixture(t)

		if !f.u.maybeRenewCredential() {
			t.Fatal("maybeRenewCredential = false, want true after a successful replacement")
		}
		wantWrites := [][2]string{{"/Users/eva/.officraft/warden/exec-warden.tok", credFreshForWardenOne}}
		if !reflect.DeepEqual(f.writes, wantWrites) {
			t.Errorf("writes = %#v, want %#v", f.writes, wantWrites)
		}
		if !reflect.DeepEqual(f.verified, []string{credFreshForWardenOne}) {
			t.Errorf("verified = %#v, want the CANDIDATE presented once", f.verified)
		}
		if f.renews != 1 || f.policies != 1 {
			t.Errorf("renews=%d policy reads=%d, want 1/1", f.renews, f.policies)
		}
		wantLog := []string{"[ocwarden] renew: wrote a fresh credential to /Users/eva/.officraft/warden/exec-warden.tok"}
		if !reflect.DeepEqual(f.log, wantLog) {
			t.Errorf("log = %#v, want %#v", f.log, wantLog)
		}
		if !f.u.renewedAwaitingRestart {
			t.Error("the already-renewed latch was not set")
		}
		if f.u.renewDemanded.Load() {
			t.Error("the station's demand should be cleared once a replacement is on disk")
		}
	})

	t.Run("a credential that is not yet due costs one policy read and nothing else", func(t *testing.T) {
		f := newRenewalFixture(t)
		f.u.now = func() time.Time { return time.Unix(1000000, 0).Add(10 * 24 * time.Hour) }

		if f.u.maybeRenewCredential() {
			t.Error("maybeRenewCredential = true, want false for a young credential")
		}
		if f.renews != 0 || len(f.writes) != 0 || len(f.verified) != 0 {
			t.Errorf("renews=%d writes=%v verified=%v, want nothing sent", f.renews, f.writes, f.verified)
		}
		if f.policies != 1 {
			t.Errorf("policy reads = %d, want 1 — the threshold is re-read every turn", f.policies)
		}
		if len(f.log) != 0 {
			t.Errorf("log = %#v, want silence", f.log)
		}
	})

	t.Run("the station's demand renews a credential that is not due", func(t *testing.T) {
		f := newRenewalFixture(t)
		f.u.now = func() time.Time { return time.Unix(1000000, 0).Add(10 * 24 * time.Hour) }
		f.u.renewDemanded.Store(true)

		if !f.u.maybeRenewCredential() {
			t.Fatal("maybeRenewCredential = false, want true on the station's demand")
		}
		wantLog := []string{
			"[ocwarden] renew: the station has asked this machine to replace its credential " +
				"(it is signed by a key that is no longer the signing key) — renewing now.",
			"[ocwarden] renew: wrote a fresh credential to /Users/eva/.officraft/warden/exec-warden.tok",
		}
		if !reflect.DeepEqual(f.log, wantLog) {
			t.Errorf("log = %#v, want %#v", f.log, wantLog)
		}
		if f.u.renewDemanded.Load() {
			t.Error("the demand must be cleared once a replacement has been written")
		}
	})

	t.Run("an unwired renewal path does nothing at all", func(t *testing.T) {
		for _, c := range []struct {
			name string
			mut  func(*updater)
		}{
			{"no renewer", func(u *updater) { u.renew = nil }},
			{"no token writer", func(u *updater) { u.writeTok = nil }},
		} {
			f := newRenewalFixture(t)
			c.mut(f.u)
			if f.u.maybeRenewCredential() {
				t.Errorf("%s: maybeRenewCredential = true, want false", c.name)
			}
			if f.policies != 0 || f.renews != 0 || len(f.writes) != 0 || len(f.log) != 0 {
				t.Errorf("%s: an unwired path still did something (policies=%d renews=%d writes=%v log=%#v)",
					c.name, f.policies, f.renews, f.writes, f.log)
			}
		}
	})

	t.Run("a machine already holding a replacement says so and asks the station nothing", func(t *testing.T) {
		f := newRenewalFixture(t)
		f.u.renewedAwaitingRestart = true

		if f.u.maybeRenewCredential() {
			t.Error("maybeRenewCredential = true, want false while a replacement is already on disk")
		}
		if f.policies != 0 || f.renews != 0 || len(f.writes) != 0 {
			t.Errorf("policies=%d renews=%d writes=%v, want nothing", f.policies, f.renews, f.writes)
		}
		wantLog := []string{"[ocwarden] renew: a fresh credential is ALREADY on disk at " +
			"/Users/eva/.officraft/warden/exec-warden.tok and this process is still running on the " +
			"previous one (the in-place exec did not take) — not renewing again; the new credential " +
			"takes effect on the next restart of this warden."}
		if !reflect.DeepEqual(f.log, wantLog) {
			t.Errorf("log = %#v, want %#v", f.log, wantLog)
		}
	})

	t.Run("every refusal keeps the old credential and writes nothing", func(t *testing.T) {
		cases := []struct {
			name    string
			mut     func(*renewalFixture)
			wantLog string
			renews  int
		}{
			{"an exported OC_TOKEN would survive the exec",
				func(f *renewalFixture) { f.u.envToken = "  " + credIssuedAtMillion + "  " },
				"[ocwarden] renew: credential is due, but OC_TOKEN is set explicitly in the environment — " +
					"it would survive the exec and override the token file, so renewing would change nothing. " +
					"Skipping (restart without OC_TOKEN to renew).", 0},
			{"the token file path does not resolve",
				func(f *renewalFixture) { f.u.tokfilePath = "" },
				"[ocwarden] renew: credential is due, but the token file path does not resolve " +
					"(no HOME / malformed OC_NAMESPACE) — not renewing rather than writing a credential to a guessed path", 0},
			{"the station was never reached",
				func(f *renewalFixture) {
					f.u.renew = func() (int, map[string]any, error) {
						f.renews++
						return 0, nil, errors.New("dial tcp: connection refused")
					}
				},
				"[ocwarden] renew: POST /api/machines/renew-credential failed (dial tcp: connection refused) — " +
					"keeping the current credential; retrying on the next poll", 1},
			{"the station refused the request",
				func(f *renewalFixture) {
					f.u.renew = func() (int, map[string]any, error) {
						f.renews++
						return http.StatusForbidden, map[string]any{"detail": "no"}, nil
					}
				},
				"[ocwarden] renew: POST /api/machines/renew-credential returned status 403 — " +
					"keeping the current credential; retrying on the next poll", 1},
			{"a 200 carrying no token",
				func(f *renewalFixture) {
					f.u.renew = func() (int, map[string]any, error) {
						f.renews++
						return http.StatusOK, map[string]any{"machine_id": "warden-1"}, nil
					}
				},
				"[ocwarden] renew: POST /api/machines/renew-credential answered 200 with no token — " +
					"keeping the current credential; retrying on the next poll", 1},
			{"a 200 carrying whitespace",
				func(f *renewalFixture) {
					f.u.renew = func() (int, map[string]any, error) {
						f.renews++
						return http.StatusOK, map[string]any{"token": "   "}, nil
					}
				},
				"[ocwarden] renew: POST /api/machines/renew-credential answered 200 with no token — " +
					"keeping the current credential; retrying on the next poll", 1},
			{"a 200 carrying something that is not a JWT",
				func(f *renewalFixture) {
					f.u.renew = func() (int, map[string]any, error) {
						f.renews++
						return http.StatusOK, map[string]any{"token": "warden-1"}, nil
					}
				},
				"[ocwarden] renew: POST /api/machines/renew-credential answered 200, but what came back is " +
					"not a credential for this machine (parsed subject \"\", expected \"warden-1\") — keeping the " +
					"current credential and NOT exec'ing; retrying on the next poll", 1},
			{"a 200 carrying another machine's credential",
				func(f *renewalFixture) {
					f.u.renew = func() (int, map[string]any, error) {
						f.renews++
						return http.StatusOK, map[string]any{"token": credFreshForWardenTwo}, nil
					}
				},
				"[ocwarden] renew: POST /api/machines/renew-credential answered 200, but what came back is " +
					"not a credential for this machine (parsed subject \"warden-2\", expected \"warden-1\") — keeping the " +
					"current credential and NOT exec'ing; retrying on the next poll", 1},
			{"the probe could not be reached",
				func(f *renewalFixture) {
					f.u.verify = func(candidate string) (int, error) {
						f.verified = append(f.verified, candidate)
						return 0, errors.New("i/o timeout")
					}
				},
				"[ocwarden] renew: could not present the new credential to /api/machines (i/o timeout) — " +
					"nothing has been written; retrying on the next poll", 1},
			{"the station could not answer about the new credential",
				func(f *renewalFixture) {
					f.u.verify = func(candidate string) (int, error) {
						f.verified = append(f.verified, candidate)
						return http.StatusBadGateway, nil
					}
				},
				"[ocwarden] renew: could not get an answer about the new credential (/api/machines answered 502) — " +
					"keeping the current credential; retrying on the next poll. This says nothing about the credential itself.", 1},
			{"the station refused the credential it just issued",
				func(f *renewalFixture) {
					f.u.verify = func(candidate string) (int, error) {
						f.verified = append(f.verified, candidate)
						return http.StatusUnauthorized, nil
					}
				},
				"[ocwarden] renew: the station REFUSED the credential it just issued (/api/machines answered 401) — " +
					"keeping the current credential, which still works. Writing it would have replaced a working " +
					"credential with one this machine cannot authenticate with, and there is no way back from that.", 1},
		}
		for _, c := range cases {
			f := newRenewalFixture(t)
			f.u.renewDemanded.Store(true)
			c.mut(f)

			if f.u.maybeRenewCredential() {
				t.Errorf("%s: maybeRenewCredential = true, want false", c.name)
			}
			if len(f.writes) != 0 {
				t.Errorf("%s: a refused renewal still wrote %v", c.name, f.writes)
			}
			if f.renews != c.renews {
				t.Errorf("%s: renew requests = %d, want %d", c.name, f.renews, c.renews)
			}
			if f.u.renewedAwaitingRestart {
				t.Errorf("%s: a refused renewal set the already-renewed latch", c.name)
			}
			if !f.u.renewDemanded.Load() {
				t.Errorf("%s: the station's demand was consumed by a failed attempt", c.name)
			}
			if len(f.log) == 0 || f.log[len(f.log)-1] != c.wantLog {
				t.Errorf("%s: last log line =\n%q\nwant\n%q", c.name, strings.Join(f.log, "\n"), c.wantLog)
			}
		}
	})

	t.Run("a write failure leaves the previous credential in force", func(t *testing.T) {
		f := newRenewalFixture(t)
		f.u.writeTok = func(path, token string) error { return errors.New("read-only file system") }

		if f.u.maybeRenewCredential() {
			t.Error("maybeRenewCredential = true, want false when the write failed")
		}
		if f.u.renewedAwaitingRestart {
			t.Error("a failed write must not set the already-renewed latch")
		}
		wantLog := []string{"[ocwarden] renew: writing the new credential to " +
			"/Users/eva/.officraft/warden/exec-warden.tok failed (read-only file system) — the previous " +
			"credential is untouched and still in use; retrying on the next poll"}
		if !reflect.DeepEqual(f.log, wantLog) {
			t.Errorf("log = %#v, want %#v", f.log, wantLog)
		}
	})

	t.Run("a replacement that is already due lands on disk without an exec", func(t *testing.T) {
		f := newRenewalFixture(t)
		f.u.renew = func() (int, map[string]any, error) {
			f.renews++
			return http.StatusOK, map[string]any{"token": credIssuedAtMillion}, nil
		}

		if f.u.maybeRenewCredential() {
			t.Error("maybeRenewCredential = true, want false — exec'ing on it is the once-per-poll runaway")
		}
		wantWrites := [][2]string{{"/Users/eva/.officraft/warden/exec-warden.tok", credIssuedAtMillion}}
		if !reflect.DeepEqual(f.writes, wantWrites) {
			t.Errorf("writes = %#v, want %#v — the fresh credential still lands", f.writes, wantWrites)
		}
		if !f.u.renewedAwaitingRestart {
			t.Error("the already-renewed latch must be set so the next poll skips the whole attempt")
		}
		wantLog := []string{
			"[ocwarden] renew: wrote a fresh credential to /Users/eva/.officraft/warden/exec-warden.tok",
			"[ocwarden] renew: the credential just issued is ALREADY due for renewal — not exec'ing, because " +
				"doing so would replace this process once per poll. The new credential is on disk at " +
				"/Users/eva/.officraft/warden/exec-warden.tok and takes effect on the next restart; the lifetime " +
				"the server mints, or this machine's clock, needs looking at.",
		}
		if !reflect.DeepEqual(f.log, wantLog) {
			t.Errorf("log = %#v, want %#v", f.log, wantLog)
		}
	})

	t.Run("an unwired verifier skips the probe rather than blocking the renewal", func(t *testing.T) {
		f := newRenewalFixture(t)
		f.u.verify = nil

		if !f.u.maybeRenewCredential() {
			t.Fatal("maybeRenewCredential = false, want true")
		}
		if len(f.writes) != 1 {
			t.Errorf("writes = %#v, want the replacement written", f.writes)
		}
	})

	t.Run("a lowered lifetime makes a credential due that the default would not have", func(t *testing.T) {
		f := newRenewalFixture(t)
		f.u.now = func() time.Time { return time.Unix(1000000, 0).Add(10 * 24 * time.Hour) }
		f.u.get = func(path string) (int, []byte, error) {
			f.policies++
			return http.StatusOK, []byte(`{"lifetime_secs":259200}`), nil
		}

		if !f.u.maybeRenewCredential() {
			t.Fatal("maybeRenewCredential = false, want true — a lowered lifetime reaches this machine on the next poll")
		}
		if f.u.credLifetimeSecs != 259200 {
			t.Errorf("credLifetimeSecs = %d, want 259200", f.u.credLifetimeSecs)
		}
	})
}

func TestExecAfterRenewal(t *testing.T) {
	t.Run("an unwired exec seam explains itself once and then stays quiet", func(t *testing.T) {
		var log []string
		u := &updater{
			tokfilePath: "/Users/eva/.officraft/warden/exec-warden.tok",
			logf:        func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) },
		}

		u.execAfterRenewal()
		u.execAfterRenewal()
		u.execAfterRenewal()

		wantLog := []string{"[ocwarden] renew: no exec seam is wired, so the new credential at " +
			"/Users/eva/.officraft/warden/exec-warden.tok takes effect on the next restart"}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
		if u.execAttempts != 3 {
			t.Errorf("execAttempts = %d, want 3", u.execAttempts)
		}
	})

	t.Run("a failed exec explains itself once, then narrates the retries in one line each", func(t *testing.T) {
		var log []string
		attempts := 0
		exits := 0
		u := &updater{
			tokfilePath: "/Users/eva/.officraft/warden/exec-warden.tok",
			logf:        func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) },
			exit:        func(int) { exits++ },
			execSelf: func() error {
				attempts++
				return fmt.Errorf("exec attempt %d: permission denied", attempts)
			},
		}

		u.execAfterRenewal()
		u.execAfterRenewal()
		u.execAfterRenewal()

		wantLog := []string{
			"[ocwarden] renew: the in-place exec did NOT happen (exec attempt 1: permission denied) — staying " +
				"alive on the credential this process already holds, which still works for days. The new credential " +
				"is on disk at /Users/eva/.officraft/warden/exec-warden.tok and takes effect on the next restart, " +
				"and the exec is retried on every poll. NOT exiting: launchd does not relaunch an exited warden, " +
				"and losing the machine to save one restart's delay is the wrong trade.",
			"[ocwarden] renew: in-place exec still failing (attempt 2: exec attempt 2: permission denied) — " +
				"still running, still holding the previous credential",
			"[ocwarden] renew: in-place exec still failing (attempt 3: exec attempt 3: permission denied) — " +
				"still running, still holding the previous credential",
		}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log =\n%s\nwant\n%s", strings.Join(log, "\n"), strings.Join(wantLog, "\n"))
		}
		if attempts != 3 || u.execAttempts != 3 {
			t.Errorf("exec calls = %d, execAttempts = %d, want 3/3", attempts, u.execAttempts)
		}
		if exits != 0 {
			t.Errorf("the process was exited %d times, want 0 — launchd does not relaunch an exited warden", exits)
		}
	})
}
