package main

// renewapply.go — ACTING on the decision renew.go makes: ask the server for a
// fresh credential, put it on disk, and re-exec so the new process picks it up.
//
// 🔴 THE ORDER IS THE WHOLE DESIGN. This code runs unattended on every machine in
// the fleet at once and ends in a syscall.Exec that replaces the running warden.
// The one outcome that cannot be recovered from here is a machine whose
// credential was thrown away before a working replacement was on disk: nothing
// reaches that host afterwards except a person walking to it. So:
//
//	get a new credential -> write it successfully -> only then exec.
//
// Every failure short-circuits to "return, change nothing": the old credential
// stays on disk, in this process, and in use. A refused renewal is a machine that
// keeps working; a half-applied renewal is a machine that is gone.
//
// NO EXTRA BACKOFF ON FAILURE. The caller is the 15-minute self-update loop, and a
// failed attempt simply waits for the next turn. Backing off would mean recording
// "when did we last try", i.e. new mutable state on the path whose failure mode is
// bricking a host, bought for nothing but a handful of body-less requests.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// renewCredentialPath is the zero-argument renewal endpoint. It takes NO body and
// names NO target: the machine acted on is the caller's own verified `sub`.
const renewCredentialPath = "/api/machines/renew-credential"

// credentialProbePath is the endpoint a REPLACEMENT credential is tried against
// before anything on disk is touched. It is read-only, it is the cheapest thing a
// warden is entitled to call (authGated + principalMachine, the lowest rank), and
// it leaves nothing behind — the telemetry endpoint would have done too, but it
// writes a sample, and a probe should not be able to be mistaken for a heartbeat.
const credentialProbePath = "/api/machines"

// credentialRenewer POSTs the renewal request and returns (status, decoded body,
// transport error) — the same three-way split as selfupdate.go's `getter`, so the
// caller classifies "server said no" and "never reached the server" itself instead
// of receiving one indistinguishable error. Injected into updater so the renewal
// path is testable without a live server.
type credentialRenewer func() (int, map[string]any, error)

// httpCredentialRenewer builds the real POST-{base}{path} closure with the
// warden's Bearer token baked in, mirroring httpGetter/httpPoster.
func httpCredentialRenewer(client *http.Client, base, token string) credentialRenewer {
	return func() (int, map[string]any, error) {
		req, err := http.NewRequest(http.MethodPost, base+renewCredentialPath, nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp.StatusCode, nil, err
		}
		var obj map[string]any
		_ = json.Unmarshal(raw, &obj)
		return resp.StatusCode, obj, nil
	}
}

// renewalWiring is everything the renewal path needs from the outside world,
// built in ONE place so it can be asserted.
//
// WHY IT IS NOT ASSEMBLED INLINE IN newSelfUpdater. That constructor opens with
// refuseInTestBinary, so NOTHING in this package can construct it — which means
// every field it fills is outside the reach of every test. Measured: with the
// wiring inline, `envToken: ""` and `renew: nil` both compiled and the whole
// suite stayed green, i.e. the OC_TOKEN guard and the entire feature could be
// switched off in production without a single red test. maybeRenewCredential was
// well covered; whether production handed it the right values was not covered at
// all.
//
// ⚠️ RESIDUAL GAP, stated rather than papered over: this makes the VALUES
// testable, not the one line in newSelfUpdater that copies them onto the updater.
// Dropping a field there still compiles and still goes green. Closing that needs
// a static check of the shape hostseam_test.go uses, which is more machinery than
// this ticket carries.
type renewalWiring struct {
	renew       credentialRenewer
	verify      credentialVerifier
	writeTok    func(path, token string) error
	tokfilePath string
	token       string
	envToken    string
	// client is carried so a test can assert WHICH budget the renewal runs under.
	// The 10s announce budget exists for the swap→exit critical path; a renewal
	// that gives up early throws away a credential the station already minted.
	client *http.Client
}

// newRenewalWiring resolves the renewal inputs from config and the RAW
// environment. `env` must be the raw environment, never the tokfile-folded view
// loadConfig is given: envToken has to say whether OC_TOKEN was set by whoever
// STARTED this process, and the folded view answers with the token file's
// contents as if someone had exported them — which would silently disable the
// infinite-exec guard on exactly the machines it protects.
func newRenewalWiring(cfg Config, env func(string) string) renewalWiring {
	// The client is built HERE rather than taken from the caller, so that WHICH
	// budget a renewal runs under is a decision inside a function tests can call.
	// Handed in from newSelfUpdater it sat beside a 10s announce client, one
	// character away, with nothing able to tell the two apart.
	client := &http.Client{Timeout: selfUpdateHTTPTimeout}
	return renewalWiring{
		renew:       httpCredentialRenewer(client, cfg.Base, cfg.Token),
		verify:      httpCredentialVerifier(client, cfg.Base),
		writeTok:    osTokfileWriter().write,
		tokfilePath: tokfilePath(env),
		token:       cfg.Token,
		envToken:    env("OC_TOKEN"),
		client:      client,
	}
}

// apply copies the wiring onto the updater. One line, so the copy itself is easy
// to read against the struct above.
func (u *updater) apply(w renewalWiring) {
	u.renew, u.verify, u.writeTok = w.renew, w.verify, w.writeTok
	u.tokfilePath, u.token, u.envToken = w.tokfilePath, w.token, w.envToken
}

// credentialVerifier answers whether a credential is ACCEPTED by the station —
// the difference between "the server sent us something" and "the server will take
// it back". Injected so the renewal path is testable without a live station.
type credentialVerifier func(token string) (int, error)

// httpCredentialVerifier presents the candidate credential — not the one this
// process is running on — at a read-only endpoint and reports the status.
func httpCredentialVerifier(client *http.Client, base string) credentialVerifier {
	return func(candidate string) (int, error) {
		status, _, err := httpGetter(client, base, candidate)(credentialProbePath)
		return status, err
	}
}

// maybeRenewCredential runs ONE renewal attempt and reports whether the process
// should now re-exec to pick the new credential up. It returns true ONLY after a
// fresh non-empty token has been written to disk successfully; every other path
// returns false having changed nothing at all.
//
// It is called once per WAKE of the poll loop, BEFORE the self-update's sha gate.
// A wake is the 15-minute timer OR a kick, and the SSE transport kicks on every
// reconnect — so on a flapping network the attempts follow the reconnects, not the
// timer. That is affordable only because a failed attempt costs one body-less
// request and changes nothing; it is the reason there is no backoff, and also the
// reason not to put anything expensive here. That
// placement is deliberate and load-bearing: the sha gate returns early whenever
// the server has not shipped a new commit, so a credential check living behind it
// would only ever run on release days — and a machine's credential expires on its
// own schedule, not on ours. The check itself is local arithmetic over a token
// this process already holds; only an actually-due credential costs a request.
func (u *updater) maybeRenewCredential() bool {
	if u.renew == nil || u.writeTok == nil {
		return false // unwired (tests, --once) — renewal is simply not in play
	}
	if !credentialDueForRenewal(u.token, u.clock()) {
		return false
	}

	// 🔴 THE INFINITE-EXEC TRAP. execSelf passes os.Environ() through, so an
	// OC_TOKEN that was set explicitly in this process's environment SURVIVES the
	// exec and keeps winning over the token file (see tokfileEnv). Renewing under
	// that env would write a fresh credential nothing reads, find the same stale
	// token still due on the next turn, and exec again — once per poll, forever.
	// The launchd job never sets OC_TOKEN (only OC_WARDEN_TOKFILE), so this only
	// ever describes a hand-started / tmux / test warden; for those, doing nothing
	// is exactly right, because there is nothing a renewal could accomplish.
	if strings.TrimSpace(u.envToken) != "" {
		u.logf("[ocwarden] renew: credential is due, but OC_TOKEN is set explicitly in " +
			"the environment — it would survive the exec and override the token file, so " +
			"renewing would change nothing. Skipping (restart without OC_TOKEN to renew).")
		return false
	}
	if u.tokfilePath == "" {
		u.logf("[ocwarden] renew: credential is due, but the token file path does not " +
			"resolve (no HOME / malformed OC_NAMESPACE) — not renewing rather than writing " +
			"a credential to a guessed path")
		return false
	}

	status, body, err := u.renew()
	if err != nil {
		u.logf("[ocwarden] renew: POST %s failed (%v) — keeping the current credential; "+
			"retrying on the next poll", renewCredentialPath, err)
		return false
	}
	if status != http.StatusOK {
		u.logf("[ocwarden] renew: POST %s returned status %d — keeping the current "+
			"credential; retrying on the next poll", renewCredentialPath, status)
		return false
	}
	fresh, _ := body["token"].(string)
	fresh = strings.TrimSpace(fresh)
	if fresh == "" {
		u.logf("[ocwarden] renew: POST %s answered 200 with no token — keeping the "+
			"current credential; retrying on the next poll", renewCredentialPath)
		return false
	}

	// 🔴 WHAT ARRIVED MUST BE A CREDENTIAL FOR THIS MACHINE, and non-empty is not
	// that. Every byte that gets past here is renamed over the only copy of the
	// credential this host has, and the process then execs into it. The failure is
	// UNRECOVERABLE in a way the other failures are not: a replacement that is not
	// a JWT makes credentialDueForRenewal permanently false (renew.go — no exp, not
	// due), so this machine never attempts a renewal again; nothing anywhere in the
	// warden acts on a 401; and the old credential is gone. Somebody walks to the
	// box.
	//
	// This is not hypothetical. The renewal response DTO carries `token` beside two
	// other strings, so a server-side transposition — `Token: machine.ID` — compiles,
	// answers 200, and is non-empty. Every machine in the fleet would receive the
	// same wrong answer within one poll of each other and apply it simultaneously.
	// Four lines here turn "the whole fleet is bricked" into "renewal refused, the
	// machines keep running".
	//
	// The test is the strongest one that every REAL credential passes: it parses as
	// a JWT carrying a `sub`, and that `sub` is the identity this process is already
	// running as. Deliberately NOT jwtLifetime: warden credentials carry no exp
	// today, so demanding one would reject every genuine renewal — a check that a
	// lie and the truth both fail is not a check.
	freshSub := jwtSub(fresh)
	if freshSub == "" || freshSub != jwtSub(u.token) {
		u.logf("[ocwarden] renew: POST %s answered 200, but what came back is not a "+
			"credential for this machine (parsed subject %q, expected %q) — keeping the "+
			"current credential and NOT exec'ing; retrying on the next poll",
			renewCredentialPath, freshSub, jwtSub(u.token))
		return false
	}

	// 🔴 PRESENT IT BEFORE REPLACING ANYTHING. The subject check above reads the
	// credential; this asks the only party that can settle it whether the credential
	// WORKS. They catch different things: a token minted with the wrong secret, or
	// with a scope or machine_id the station refuses, parses fine and carries the
	// right subject — and every one of those bricks the host exactly like a
	// non-JWT would, because the station answers 401 to a warden that has no code
	// path for 401 and no way back to a credential it has already overwritten.
	//
	// The order is the point: this runs BEFORE the write, so a replacement that is
	// not accepted costs one read-only GET and nothing else. The ticket's wording is
	// "the old credential must not be retired before the new one is written AND
	// CONFIRMED USABLE"; confirming it after the write would satisfy the sentence
	// and not the intent.
	//
	// A TRANSPORT failure is deliberately NOT treated as a refusal — it says nothing
	// about the credential, and treating "the network blinked" as "this credential is
	// bad" would strand a machine that is holding a perfectly good replacement.
	// Either way nothing has been written yet, so both paths simply wait for the
	// next poll.
	if u.verify != nil {
		status, err := u.verify(fresh)
		switch {
		case err != nil:
			u.logf("[ocwarden] renew: could not present the new credential to %s (%v) — "+
				"nothing has been written; retrying on the next poll", credentialProbePath, err)
			return false
		case status != http.StatusOK:
			u.logf("[ocwarden] renew: the station REFUSED the credential it just issued "+
				"(%s answered %d) — keeping the current credential, which still works. "+
				"Writing it would have replaced a working credential with one this "+
				"machine cannot authenticate with, and there is no way back from that.",
				credentialProbePath, status)
			return false
		}
	}

	if err := u.writeTok(u.tokfilePath, fresh); err != nil {
		// The write is atomic (temp -> rename), so a failure here means the OLD
		// credential is still the one on disk. That is the reason this returns
		// instead of exec'ing: the running process still holds a working token.
		u.logf("[ocwarden] renew: writing the new credential to %s failed (%v) — the "+
			"previous credential is untouched and still in use; retrying on the next poll",
			u.tokfilePath, err)
		return false
	}
	u.logf("[ocwarden] renew: wrote a fresh credential to %s", u.tokfilePath)

	// A replacement that is ALREADY due is not worth an exec, and exec'ing on it is
	// how this loop would run away: the new process reads a credential that is due
	// the moment it starts, renews, execs, forever — once per poll. That can only
	// come from the server minting a lifetime shorter than the renewal threshold or
	// from a clock far enough out to look like it; both are somebody else's bug, and
	// the harmless response to both is to leave the fresh credential on disk for the
	// next real restart and say so. The renewal itself still repeats each poll, but
	// a repeated mint is wasted work, not a machine that keeps replacing itself.
	if credentialDueForRenewal(fresh, u.clock()) {
		u.logf("[ocwarden] renew: the credential just issued is ALREADY due for renewal "+
			"— not exec'ing, because doing so would replace this process once per poll. "+
			"The new credential is on disk at %s and takes effect on the next restart; "+
			"the lifetime the server mints, or this machine's clock, needs looking at.",
			u.tokfilePath)
		return false
	}
	return true
}
