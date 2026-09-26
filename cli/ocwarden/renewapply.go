package main

// 🔴 THE ORDER IS THE WHOLE DESIGN. This runs unattended on every machine at once
// and ends in a syscall.Exec. A machine whose credential was thrown away before a
// working replacement was on disk is unrecoverable — only a person walking to it
// fixes it. So:
//
//	get a new credential -> write it successfully -> only then exec.
//
// Every failure returns having changed nothing. No extra backoff: a failed
// attempt costs one body-less request and waits for the next poll; backing off
// would add mutable state on the path whose failure mode is bricking a host.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const renewCredentialPath = "/api/machines/renew-credential"

// Asked even though credentials carry an exp again, for the reasons in renew.go.
const credentialPolicyPath = "/api/machines/credential-policy"

// Read-only and the lowest-rank endpoint a warden may call. Not the telemetry
// endpoint: that writes a sample, and a probe must not be mistaken for a heartbeat.
const credentialProbePath = "/api/machines"

type credentialRenewer func() (int, map[string]any, error)

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

// apply stays separate from the constructor so a test can call it
// (TestApply_CarriesEveryFieldOntoTheUpdater).
type renewalWiring struct {
	renew       credentialRenewer
	verify      credentialVerifier
	writeTok    func(path, token string) error
	tokfilePath string
	token       string
	envToken    string
}

// `env` must be the RAW environment, never the tokfile-folded view loadConfig is
// given: envToken must say whether whoever STARTED this process set OC_TOKEN, and
// the folded view would silently disable the infinite-exec guard.
func newRenewalWiring(cfg Config, env func(string) string) renewalWiring {
	// ⚠️ No test watches which budget this is: swapping selfUpdateRequestBudget for
	// selfUpdateReportBudget leaves the package green — measured.
	client := &http.Client{Timeout: selfUpdateRequestBudget}
	return renewalWiring{
		renew:       httpCredentialRenewer(client, cfg.Base, cfg.Token),
		verify:      httpCredentialVerifier(client, cfg.Base),
		writeTok:    osTokfileWriter().write,
		tokfilePath: tokfilePath(env),
		token:       cfg.Token,
		envToken:    env("OC_TOKEN"),
	}
}

func (u *updater) apply(w renewalWiring) {
	u.renew, u.verify, u.writeTok = w.renew, w.verify, w.writeTok
	u.tokfilePath, u.token, u.envToken = w.tokfilePath, w.token, w.envToken
}

type credentialVerifier func(token string) (int, error)

func httpCredentialVerifier(client *http.Client, base string) credentialVerifier {
	return func(candidate string) (int, error) {
		status, _, err := httpGetter(client, base, candidate)(credentialProbePath)
		return status, err
	}
}

// 🔴 SILENT BY DESIGN, unlike every other failure in this file: an unknown
// threshold is not an incident (the last answer or the shipped default stands), and
// this endpoint fails on ordinary occasions — a station not upgraded yet (404), a
// network blink. Logging would add a line per machine per poll and train readers
// to ignore this path. 404/500/timeout are not distinguished: the action is the
// same.
func (u *updater) refreshCredentialPolicy() {
	if u.get == nil {
		return
	}
	status, raw, err := u.get(credentialPolicyPath)
	if err != nil || status != http.StatusOK {
		return
	}
	var doc struct {
		LifetimeSecs *int64 `json:"lifetime_secs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return
	}
	// 🔴 A MISSING FIELD MUST NOT LAND AS ZERO, hence the pointer: credentialRenewAfter
	// reads 0 as "never answered" and substitutes the default, so a RENAMED field would
	// silently revert the fleet to the default while the endpoint kept answering 200.
	if doc.LifetimeSecs == nil || *doc.LifetimeSecs <= 0 {
		return
	}
	u.credLifetimeSecs = *doc.LifetimeSecs
}

// Called once per WAKE of the poll loop, and the SSE transport kicks it on every
// reconnect — so on a flapping network attempts follow reconnects. Keep this
// cheap. Each turn costs one GET for the station's lifetime setting, which buys what
// the owner asked for: lowering the setting reaches every machine within one poll.
func (u *updater) maybeRenewCredential() bool {
	if u.renew == nil || u.writeTok == nil {
		return false
	}
	// 🔴 The token is read once at startup and lives in memory, so after a write
	// credentialDueForRenewal would keep judging the retired credential "due" and
	// renew every poll, fleet-wide.
	if u.renewedAwaitingRestart {
		u.logf("[ocwarden] renew: a fresh credential is ALREADY on disk at %s and this "+
			"process is still running on the previous one (the in-place exec did not "+
			"take) — not renewing again; the new credential takes effect on the next "+
			"restart of this warden.", u.tokfilePath)
		return false
	}
	// 🔴 THE DEMAND EXISTS BECAUSE EXPIRY CANNOT ANSWER IT: a credential signed by a
	// key the station retired is not expiring (it may carry no exp at all) and becomes
	// worthless the instant that key is removed. The token cannot say which key signed
	// it (constant JWT header, no kid), so only the station can raise this.
	u.refreshCredentialPolicy()

	demanded := u.renewDemanded.Load()
	if !demanded && !credentialDueForRenewal(u.token, u.clock(), u.renewAfter()) {
		return false
	}
	if demanded {
		u.logf("[ocwarden] renew: the station has asked this machine to replace its " +
			"credential (it is signed by a key that is no longer the signing key) — " +
			"renewing now.")
	}

	// 🔴 THE INFINITE-EXEC TRAP. execSelf passes os.Environ() through, so an explicit
	// OC_TOKEN survives the exec and keeps winning over the token file (tokfileEnv):
	// renewing would exec once per poll, forever. The launchd job never sets OC_TOKEN
	// (only OC_WARDEN_TOKFILE), so this only hits hand-started / tmux / test wardens.
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

	// 🔴 WHAT ARRIVED MUST BE A CREDENTIAL FOR THIS MACHINE. A non-JWT replacement makes
	// credentialDueForRenewal permanently false, nothing in the warden acts on a 401,
	// and the old credential is gone — the box is bricked. Realistic: the renewal DTO
	// carries `token` beside two other strings, so `Token: machine.ID` compiles and
	// answers 200, fleet-wide within one poll.
	// Check `sub` — what binds the credential to THIS machine — not jwtLifetime
	// (genuine credentials minted before T-fc53 第二段 carried no exp) and not
	// jwtIssuedAt (`iat` is what the AGE arm reads; requiring it would make this
	// guard and that arm fail together).
	freshSub := jwtSub(fresh)
	if freshSub == "" || freshSub != jwtSub(u.token) {
		u.logf("[ocwarden] renew: POST %s answered 200, but what came back is not a "+
			"credential for this machine (parsed subject %q, expected %q) — keeping the "+
			"current credential and NOT exec'ing; retrying on the next poll",
			renewCredentialPath, freshSub, jwtSub(u.token))
		return false
	}

	// 🔴 PRESENT IT BEFORE WRITING. A token minted with the wrong secret, scope or
	// machine_id parses and carries the right subject, then gets 401 — bricking the
	// host like a non-JWT. Verifying after the write would be too late. A transport
	// failure is NOT a refusal: it says nothing about the credential.
	if u.verify != nil {
		status, err := u.verify(fresh)
		switch {
		case err != nil:
			u.logf("[ocwarden] renew: could not present the new credential to %s (%v) — "+
				"nothing has been written; retrying on the next poll", credentialProbePath, err)
			return false
		case status >= 500:
			// The auth path this probe hits is fail-closed on a roster read (authz.go), so
			// one database blink looks like a refusal; only the log differs, and "refused"
			// would send readers hunting a minting bug that does not exist.
			u.logf("[ocwarden] renew: could not get an answer about the new credential "+
				"(%s answered %d) — keeping the current credential; retrying on the next "+
				"poll. This says nothing about the credential itself.",
				credentialProbePath, status)
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
		u.logf("[ocwarden] renew: writing the new credential to %s failed (%v) — the "+
			"previous credential is untouched and still in use; retrying on the next poll",
			u.tokfilePath, err)
		return false
	}
	u.renewedAwaitingRestart = true
	// Cleared only after a successful write, even when the exec below is skipped: the
	// station stops asking once it observes the new key id, which the next start makes
	// it do.
	u.renewDemanded.Store(false)
	u.logf("[ocwarden] renew: wrote a fresh credential to %s", u.tokfilePath)

	if credentialDueForRenewal(fresh, u.clock(), u.renewAfter()) {
		u.logf("[ocwarden] renew: the credential just issued is ALREADY due for renewal "+
			"— not exec'ing, because doing so would replace this process once per poll. "+
			"The new credential is on disk at %s and takes effect on the next restart; "+
			"the lifetime the server mints, or this machine's clock, needs looking at.",
			u.tokfilePath)
		return false
	}
	return true
}

// 🔴 "Renewed, waiting for a restart" is not announced to the server on purpose.
// u.post makes it look like four lines, but the ingest endpoint takes a closed set
// of top-level fields (a new one is a spec + client + handler change), and reusing
// self_update would OVERWRITE that machine's real last-swap record.
//
// 🔴 Deliberately NOT execInPlace, whose exec-failed fallback is exit(0): the
// running credential is still valid, and launchd does NOT relaunch an exited
// warden (observed twice on real hosts — see selfupdate.go's header).
func (u *updater) execAfterRenewal() {
	u.execAttempts++
	if u.execSelf == nil {
		if u.execAttempts == 1 {
			u.logf("[ocwarden] renew: no exec seam is wired, so the new credential at %s "+
				"takes effect on the next restart", u.tokfilePath)
		}
		return
	}
	err := u.execSelf()
	if u.execAttempts > 1 {
		u.logf("[ocwarden] renew: in-place exec still failing (attempt %d: %v) — still "+
			"running, still holding the previous credential", u.execAttempts, err)
		return
	}
	u.logf("[ocwarden] renew: the in-place exec did NOT happen (%v) — staying alive on "+
		"the credential this process already holds, which still works for days. The new "+
		"credential is on disk at %s and takes effect on the next restart, and the exec "+
		"is retried on every poll. NOT exiting: launchd does not relaunch an exited "+
		"warden, and losing the machine to save one restart's delay is the wrong trade.",
		err, u.tokfilePath)
}
