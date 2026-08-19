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

// maybeRenewCredential runs ONE renewal attempt and reports whether the process
// should now re-exec to pick the new credential up. It returns true ONLY after a
// fresh non-empty token has been written to disk successfully; every other path
// returns false having changed nothing at all.
//
// It is called once per poll turn, BEFORE the self-update's sha gate. That
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
	return true
}
