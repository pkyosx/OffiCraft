package main

// api_machines.go — machines (warden members), the installer surface, the
// prebuilt binary downloads, and the display-name overlays
// (handlers.handle_list_machines … handle_delete_machine +
// handle_update_account / handle_update_machine + handle_install_script +
// handle_warden_binary / handle_agent_binary + bootstrap/teardown-here).

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"
)

// defaultMachineTTLDays is the exec-token lifetime an onboard defaults to
// (handlers._DEFAULT_MACHINE_TTL_DAYS); still capped at maxAgentTTLSecs.
const defaultMachineTTLDays int64 = 90

// machineClaimTTLSecs is the lifetime of a one-time machine claim code — the
// short-lived credential the boot command carries INSTEAD of the 90-day
// exec-token (a pasted/leaked one-liner stops granting anything after 10
// minutes or one use, whichever comes first).
const machineClaimTTLSecs int64 = 600

// claimCodeDeniedMsg is the single 401 face for every failed redemption —
// unknown, expired, and already-used codes are indistinguishable on the wire
// (no guessing oracle).
const claimCodeDeniedMsg = "claim code is invalid, expired, or already used — " +
	"fetch a fresh boot command from the cockpit"

// machineClaimStore holds the pending one-time claim codes IN MEMORY ONLY:
// a 10-minute credential needs no DB row — a server restart voids the codes,
// which reads exactly like expiry (re-fetch the boot command).
type machineClaimStore struct {
	mu    sync.Mutex
	codes map[string]machineClaim
}

type machineClaim struct {
	machineID string
	expiresAt time.Time
}

func newMachineClaimStore() *machineClaimStore {
	return &machineClaimStore{codes: map[string]machineClaim{}}
}

// mint issues a fresh single-use code bound to machineID (32 random bytes,
// base64url — the ensureFirstRunClaimToken mint pattern) and sweeps expired
// entries so abandoned boot commands never accumulate.
func (st *machineClaimStore) mint(machineID string, now time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	st.mu.Lock()
	defer st.mu.Unlock()
	for k, v := range st.codes {
		if now.After(v.expiresAt) {
			delete(st.codes, k)
		}
	}
	st.codes[code] = machineClaim{
		machineID: machineID,
		expiresAt: now.Add(time.Duration(machineClaimTTLSecs) * time.Second),
	}
	return code, nil
}

// take redeems a code: on a live match the entry is deleted ATOMICALLY under
// the same lock (single-use by construction) and the bound machine id is
// returned. The scan compares constant-time (the first-run claim-token
// posture) — a map lookup on the attacker-supplied string would leak timing.
func (st *machineClaimStore) take(code string, now time.Time) (string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for k, v := range st.codes {
		if subtle.ConstantTimeCompare([]byte(k), []byte(code)) == 1 {
			delete(st.codes, k)
			if now.After(v.expiresAt) {
				return "", false
			}
			return v.machineID, true
		}
	}
	return "", false
}

// machineTTL is the capped default machine exec-token TTL in seconds.
func machineTTL(ttlDays int64) int64 {
	ttl := ttlDays * 86400
	if ttl > maxAgentTTLSecs {
		ttl = maxAgentTTLSecs
	}
	return ttl
}

// requestBaseURL rebuilds the request base ("scheme://host", no trailing
// slash) — the Python str(request.base_url).rstrip("/").
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// buildBootCommand is the copy-paste one-liner an EMPTY machine runs
// (handlers._build_boot_command — the curl|bash wrapper over GET /install.sh).
// It carries the one-time claim code, NEVER the exec-token: the served script
// redeems the code via POST /api/machines/claim for the real token.
func buildBootCommand(baseURL, code string) string {
	return "curl -fsSL '" + baseURL + "/install.sh?code=" + code + "' | bash"
}

// buildInstallScript is the self-contained bash installer served over
// GET /install.sh (handlers._build_install_script — byte-shape twin). A
// non-empty namespace ([server].namespace) prefixes the install line with
// OC_NAMESPACE so the remote warden installs under the namespaced root/label;
// the empty namespace keeps the script byte-identical to the historical output.
func buildInstallScript(baseURL, token, namespace string) string {
	nsPrefix := ""
	if namespace != "" {
		nsPrefix = `OC_NAMESPACE="` + namespace + `" `
	}
	return `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL '` + baseURL + `/install.sh?token=<jwt>' | bash
set -euo pipefail

# Precheck: only the KEY tools the install truly needs (not an exhaustive audit).
#   tmux — the warden spawns each member's session through it (auto-installed
#          via Homebrew when available).
#   curl — used just below to pull the ocwarden binary.
for tool in tmux curl; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  # tmux is the one tool worth auto-installing: Homebrew boxes get it hands-free.
  if [ "$tool" = tmux ] && command -v brew >/dev/null 2>&1; then
    echo "tmux not found — installing via Homebrew..."
    brew install tmux || true
    if command -v tmux >/dev/null 2>&1; then
      continue
    fi
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Pull the prebuilt ocwarden binary from the PUBLIC binary endpoint (no auth
# header needed — the boot token authorizes the install, not this fetch).
curl -fsSL "` + baseURL + `/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
` + nsPrefix + `OC_BASE="` + baseURL + `" OC_TOKEN="` + token + `" ./ocwarden install --force
`
}

// buildInstallScriptWithCode is the claim-code variant of the installer: the
// script FIRST probes that the server can actually serve the warden binary (a
// HEAD on the public binary route — a 503 there must NOT burn the one-time
// code), THEN redeems the code for the machine's real exec-token
// (POST /api/machines/claim) — a dead code fails before any bytes are
// downloaded — then proceeds exactly like the token variant (which stays
// byte-identical for legacy ?token= URLs).
func buildInstallScriptWithCode(baseURL, code, namespace string) string {
	nsPrefix := ""
	if namespace != "" {
		nsPrefix = `OC_NAMESPACE="` + namespace + `" `
	}
	return `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL '` + baseURL + `/install.sh?code=<one-time code>' | bash
set -euo pipefail

# Precheck: only the KEY tools the install truly needs (not an exhaustive audit).
#   tmux — the warden spawns each member's session through it (auto-installed
#          via Homebrew when available).
#   curl — claims the machine token just below, then pulls the ocwarden binary.
#   sed  — extracts the token from the claim response JSON.
for tool in tmux curl sed; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  # tmux is the one tool worth auto-installing: Homebrew boxes get it hands-free.
  if [ "$tool" = tmux ] && command -v brew >/dev/null 2>&1; then
    echo "tmux not found — installing via Homebrew..."
    brew install tmux || true
    if command -v tmux >/dev/null 2>&1; then
      continue
    fi
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Probe the warden binary availability BEFORE redeeming the one-time claim
# code — a server that cannot serve the binary (503) must not burn the code.
if ! curl -fsI "` + baseURL + `/api/warden/binary" >/dev/null 2>&1; then
  echo "Error: the server cannot serve the warden binary (` + baseURL + `/api/warden/binary is unavailable)." >&2
  echo "Fix: redeploy the server with the prebuilt binaries (bin/ocwarden) or an embed-carrying build, then re-run this one-liner — the install code was NOT consumed." >&2
  exit 1
fi

# Exchange the ONE-TIME claim code for this machine's real exec-token FIRST —
# before any download — so an expired/used install link fails at the earliest
# possible point. The code is single-use: a replayed one-liner lands here.
if ! CLAIM_RESPONSE="$(curl -fsS -X POST "` + baseURL + `/api/machines/claim" \
  -H 'Content-Type: application/json' --data '{"code":"` + code + `"}')"; then
  echo "Error: this install link has expired or was already used." >&2
  echo "Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner." >&2
  exit 1
fi
# The token is a base64url JWT — no quote/backslash can appear inside it.
OC_TOKEN="$(printf '%s' "$CLAIM_RESPONSE" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
if [ -z "$OC_TOKEN" ]; then
  echo "Error: this install link has expired or was already used." >&2
  echo "Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner." >&2
  exit 1
fi

# Pull the prebuilt ocwarden binary from the PUBLIC binary endpoint (no auth
# header needed — the claimed token authorizes the install, not this fetch).
curl -fsSL "` + baseURL + `/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
` + nsPrefix + `OC_BASE="` + baseURL + `" OC_TOKEN="$OC_TOKEN" ./ocwarden install --force
`
}

// binStatusCurrent / binStatusStale are the machine rows' binary-freshness
// verdicts (wire vocabulary of machineDTO.bin_status / MonitoringMachineDTO).
const (
	binStatusCurrent = "current"
	binStatusStale   = "stale"
)

// machineBinStatus compares the content fingerprints machineID's warden
// heartbeat reported (the telemetry entry's `binaries` — keyed by the
// warden's own member id, which IS the machine id) against the server's
// embedded prebuilt hashes (s.binHashes). Verdict:
//   - any reported fingerprint differs from its embed twin → "stale";
//   - every embedded binary matched a reported fingerprint → "current";
//   - anything less (no heartbeat yet, an older warden build that reports no
//     fingerprints, a partial report, or no embedded bindist to compare
//     against) → nil, the honest unknown — never a guessed verdict.
//
// Comparison result only, by design (owner-approved): no per-machine version
// number, no embedded version stamps — the same raw-content oracle the warden
// self-update swaps on (fingerprint equality IS "already the served build").
func (s *apiServer) machineBinStatus(machineID string) *string {
	if len(s.binHashes) == 0 || s.telemetry == nil {
		return nil
	}
	entry := s.telemetry.Get(machineID)
	if entry == nil {
		return nil
	}
	reported, _ := entry["binaries"].(map[string]any)
	if len(reported) == 0 {
		return nil
	}
	matched := 0
	for name, want := range s.binHashes {
		got, isStr := reported[name].(string)
		if !isStr || got == "" {
			continue // this binary not reported → the pair proves nothing
		}
		if got != want {
			verdict := binStatusStale
			return &verdict
		}
		matched++
	}
	if matched == len(s.binHashes) {
		verdict := binStatusCurrent
		return &verdict
	}
	return nil // no mismatch but partial coverage → unknown, not "current"
}

// claude_cred_source wire vocabulary (machineDTO.claude_cred_source /
// monitoringMachineDTO): where the machine's claude CLI credentials live,
// synthesized from the warden probe's presence bools.
const (
	claudeCredSourceFile     = "file"
	claudeCredSourceKeychain = "keychain"
	claudeCredSourceBoth     = "both"
	claudeCredSourceNone     = "none"
)

// machineClaudeInfo derives the machine rows' claude CLI columns (T-97ee)
// from machineID's warden heartbeat (the telemetry entry's `claude` probe —
// keyed by the warden's own member id, which IS the machine id; the same
// keying as machineBinStatus above):
//   - version: the probed CLI version string; nil when unreported (claude
//     unresolved, probe failed, or an older warden that never probes);
//   - credSource: synthesized from the cred_file × keychain presence bools —
//     "both" | "file" | "keychain" | "none" when both are known; with only one
//     bool reported (e.g. non-darwin skips keychain) a true still identifies
//     its source, but a lone false proves nothing → nil;
//   - subReadable: the probe's subscriptionType readability verdict.
//
// Anything unreported is nil, the honest unknown — an older warden that sends
// no `claude` field reads as all-nil, never a guessed verdict (the same
// backward-compat semantics as bin_status).
func (s *apiServer) machineClaudeInfo(machineID string) (version, credSource *string, subReadable *bool) {
	if s.telemetry == nil {
		return nil, nil, nil
	}
	entry := s.telemetry.Get(machineID)
	if entry == nil {
		return nil, nil, nil
	}
	probe, _ := entry["claude"].(map[string]any)
	if len(probe) == 0 {
		return nil, nil, nil
	}
	if v, isStr := probe["version"].(string); isStr && v != "" {
		version = &v
	}
	credFile, hasFile := probe["cred_file"].(bool)
	keychain, hasKeychain := probe["keychain"].(bool)
	switch {
	case hasFile && hasKeychain:
		verdict := claudeCredSourceNone
		switch {
		case credFile && keychain:
			verdict = claudeCredSourceBoth
		case credFile:
			verdict = claudeCredSourceFile
		case keychain:
			verdict = claudeCredSourceKeychain
		}
		credSource = &verdict
	case hasFile && credFile:
		verdict := claudeCredSourceFile
		credSource = &verdict
	case hasKeychain && keychain:
		verdict := claudeCredSourceKeychain
		credSource = &verdict
	}
	if b, isBool := probe["sub_readable"].(bool); isBool {
		subReadable = &b
	}
	return version, credSource, subReadable
}

// GET /api/machines — one row per ACTIVE warden member; display name folds
// the machine-alias overlay over the member name; server-self always FIRST.
func (s *apiServer) HandleListMachinesApiMachinesGet(w http.ResponseWriter, r *http.Request) {
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	machineNames, err := s.dal.MachineDisplayNames()
	if err != nil {
		internalError(w, err)
		return
	}
	rows := []machineDTO{}
	for _, m := range members {
		if m.Kind != machineKind || m.RosterStatus != RosterStatusActive {
			continue
		}
		display := machineNames[m.ID]
		if display == "" {
			display = m.Name
		}
		claudeVersion, claudeCredSource, claudeSubReadable := s.machineClaudeInfo(m.ID)
		rows = append(rows, machineDTO{
			MachineID:         m.ID,
			DisplayName:       display,
			Online:            s.hub.IsOnline(m.ID),
			IsSelf:            m.ID == ServerSelfHost,
			BinStatus:         s.machineBinStatus(m.ID),
			ClaudeVersion:     claudeVersion,
			ClaudeCredSource:  claudeCredSource,
			ClaudeSubReadable: claudeSubReadable,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].IsSelf && !rows[j].IsSelf })
	writeJSON(w, http.StatusOK, rows)
}

// POST /api/machines — onboard: mint a NEW warden member whose own id IS the
// machine id + its exec-token + the copy-paste boot command. Blank
// display_name → 422; no host-dedup (each call is a new physical machine).
func (s *apiServer) HandleOnboardMachineApiMachinesPost(w http.ResponseWriter, r *http.Request) {
	var body MachineOnboardDTO
	if !decodeJSONBodyRequired(w, r, &body, "display_name") {
		return
	}
	displayName := trimString(body.DisplayName)
	if displayName == "" {
		writeError(w, http.StatusUnprocessableEntity, "display_name is required")
		return
	}
	ttlDays := defaultMachineTTLDays
	if body.TtlDays != nil {
		ttlDays = int64(*body.TtlDays)
	}
	ttl := machineTTL(ttlDays)
	member := Member{
		ID:   "m-" + newHexID(12),
		Name: displayName,
		Kind: machineKind,
		// explicit: a warden carries NO self-binding (routing resolves it by
		// get_member of its own id == the machine id).
		DesiredMachineID: "",
		DesiredState:     DesiredStateOffline,
		Effort:           "medium",
		RosterStatus:     RosterStatusActive,
	}
	if err := s.putMember(member, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.PutMachineAlias(MachineAlias{
		MachineID:   member.ID,
		DisplayName: displayName,
	}); err != nil {
		internalError(w, err)
		return
	}
	token, err := s.mintMemberToken(member, ttl)
	if err != nil {
		internalError(w, err)
		return
	}
	code, err := s.machineClaims.mint(member.ID, time.Now())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, machineOnboardResultDTO{
		MemberID:       member.ID,
		MachineID:      member.ID,
		Token:          token,
		ExpiresIn:      ttl,
		BootCommand:    buildBootCommand(requestBaseURL(r), code),
		ClaimCode:      code,
		ClaimExpiresIn: machineClaimTTLSecs,
	})
}

// clearResidualUninstall consumes a leftover one-shot uninstall intent on an
// install path: every re-install entry point MUST zero a residual
// desired_state="uninstall" BEFORE installing, or the fresh warden would
// reconnect straight into a standing kill order (uninstall→re-install loop —
// real incident, 2026-07). No-op when no residue.
func (s *apiServer) clearResidualUninstall(m *Member, trigger string) error {
	if m.DesiredState != DesiredStateUninstall {
		return nil
	}
	m.DesiredState = DesiredStateOffline
	return s.putMember(*m, trigger)
}

// GET /api/machines/{machine_id}/boot-command — re-fetch the installer
// one-liner for an EXISTING machine (re-mints a fresh exec-token). A
// re-install path: any residual uninstall intent is zeroed first.
func (s *apiServer) HandleMachineBootCommandApiMachinesMachineIdBootCommandGet(w http.ResponseWriter, r *http.Request, machineId string) {
	machine, err := s.resolveMachine(machineId)
	if err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
	if err := s.clearResidualUninstall(machine, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	ttl := machineTTL(defaultMachineTTLDays)
	token, err := s.mintMemberToken(*machine, ttl)
	if err != nil {
		internalError(w, err)
		return
	}
	code, err := s.machineClaims.mint(machine.ID, time.Now())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootCommandResultDTO{
		MachineID:      machine.ID,
		BootCommand:    buildBootCommand(requestBaseURL(r), code),
		Token:          token,
		ExpiresIn:      ttl,
		ClaimCode:      code,
		ClaimExpiresIn: machineClaimTTLSecs,
	})
}

// POST /api/machines/claim — PUBLIC: redeem a one-time claim code (minted by
// onboard / boot-command, carried by the install.sh?code= one-liner) for the
// machine's real exec-token. The code is consumed atomically on redemption;
// every failure face is the same flat 401 (no unknown/expired/used oracle).
func (s *apiServer) HandleClaimMachineTokenApiMachinesClaimPost(w http.ResponseWriter, r *http.Request) {
	var body MachineClaimDTO
	if !decodeJSONBodyRequired(w, r, &body, "code") {
		return
	}
	machineID, ok := s.machineClaims.take(body.Code, time.Now())
	if !ok {
		writeError(w, http.StatusUnauthorized, claimCodeDeniedMsg)
		return
	}
	// A machine deleted in the 10-minute window folds into the same 401: the
	// code no longer grants anything, and existence stays undisclosed.
	machine, err := s.resolveMachine(machineID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, claimCodeDeniedMsg)
		return
	}
	ttl := machineTTL(defaultMachineTTLDays)
	token, err := s.mintMemberToken(*machine, ttl)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, machineClaimResultDTO{
		Token:     token,
		ExpiresIn: ttl,
		MachineID: machine.ID,
	})
}

// runOcwarden runs `<ocwarden> <verb>` bounded by 60s (the injectable-runner
// twins of handlers._default_bootstrap_runner / _default_teardown_runner).
// argv list only — zero command-injection surface; the wiring rides in env.
// Returns (exitCode, mergedLog, timedOut).
func runOcwarden(binPath string, args []string, env []string) (int, string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return -1, string(out), true
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return -1, string(out) + err.Error(), false
		}
	}
	return exitCode, string(out), false
}

// resolveOcwardenBinary returns an EXECUTABLE ocwarden binary path (503 when
// absent). Embed-only: the embedded bindist copy is materialized into the
// per-instance binary cache (binCacheDir, beside the SQLite data file) and
// run — a stale bin/ocwarden under the CWD must never be exec'd in its place
// (bootstrap-here once installed a frozen checkout's stale warden this way).
func (s *apiServer) resolveOcwardenBinary(w http.ResponseWriter) (string, bool) {
	path, err := s.resolveOcwardenBinaryFrom(bindistFS())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable,
			"ocwarden binary is not available (no embedded copy in this server build): "+err.Error())
		return "", false
	}
	return path, true
}

// resolveOcwardenBinaryFrom is resolveOcwardenBinary over an injectable
// embedded FS (tests pass fstest.MapFS; production passes bindistFS()).
// Embed-only, mirroring serveBinary's download path: always the embed,
// materialized to an executable — never an on-disk bin/ocwarden.
func (s *apiServer) resolveOcwardenBinaryFrom(embedded fs.FS) (string, error) {
	data, err := fs.ReadFile(embedded, "ocwarden")
	if err != nil {
		return "", err
	}
	if s.binCacheDir == "" {
		return "", errors.New("no binary cache directory configured")
	}
	return materializeBinary(s.binCacheDir, "ocwarden", data)
}

// POST /api/machines/{machine_id}/bootstrap-here — install this machine's
// warden ON THE SERVER HOST (owner-only on the route table). A non-zero exit
// is NOT an HTTP error: ok=false with the log surfaced.
func (s *apiServer) HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost(w http.ResponseWriter, r *http.Request, machineId string) {
	machine, err := s.resolveMachine(machineId)
	if err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
	// An install path: zero any residual uninstall intent BEFORE installing
	// (先歸零再裝) so the fresh warden never boots into a standing kill order.
	if err := s.clearResidualUninstall(machine, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	binPath, ok := s.resolveOcwardenBinary(w)
	if !ok {
		return
	}
	ttl := machineTTL(defaultMachineTTLDays)
	token, err := s.mintMemberToken(*machine, ttl)
	if err != nil {
		internalError(w, err)
		return
	}
	// env = the server process env (PATH/HOME inherited) + the OC_* wiring;
	// OC_ID scrubbed — identity rides SOLELY in the token's sub.
	// NOTE (claude spawn chain): the passthrough deliberately carries
	// OC_CLAUDE_BIN (and a possibly-enriched PATH) from the serve plist —
	// stamped there by `bin/ocserver install`, which ran in the operator's
	// interactive shell and could actually FIND claude. The `ocwarden install`
	// spawned below runs under this launchd-minimal env, so without that relay
	// it could never resolve a version-manager claude, and the warden it
	// installs would refuse every spawn (claude_bin_unresolved).
	env := []string{}
	for _, kv := range os.Environ() {
		if len(kv) >= 6 && kv[:6] == "OC_ID=" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "OC_BASE="+requestBaseURL(r), "OC_TOKEN="+token)
	// Namespaced instance: the warden it installs on this host must key its
	// root/label/socket off the same namespace (single propagation env).
	if s.namespace != "" {
		env = append(env, "OC_NAMESPACE="+s.namespace)
	}
	exitCode, log, timedOut := runOcwarden(binPath, []string{"install", "--force"}, env)
	if timedOut {
		writeJSON(w, http.StatusOK, bootstrapResultDTO{
			MachineID: machine.ID,
			OK:        false,
			ExitCode:  -1,
			Log:       "ocwarden install timed out (exceeded 60s) — no changes confirmed",
		})
		return
	}
	writeJSON(w, http.StatusOK, bootstrapResultDTO{
		MachineID: machine.ID,
		OK:        exitCode == 0,
		ExitCode:  exitCode,
		Log:       log,
	})
}

// POST /api/machines/{machine_id}/teardown-here — tear the warden down ON THE
// SERVER HOST (owner-only). CONFIRM-THEN-REMOVE: the member is soft-deleted
// ONLY on a confirmed teardown (exit 0).
func (s *apiServer) HandleTeardownHereApiMachinesMachineIdTeardownHerePost(w http.ResponseWriter, r *http.Request, machineId string) {
	machine, err := s.resolveMachine(machineId)
	if err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
	binPath, ok := s.resolveOcwardenBinary(w)
	if !ok {
		return
	}
	// teardown is identity-agnostic (HOME/uid only) — no OC_* wiring needed,
	// EXCEPT the instance namespace: a namespaced server must tear down its OWN
	// warden (label/tokfile under its namespace), never the main instance's.
	env := os.Environ()
	if s.namespace != "" {
		env = append(env, "OC_NAMESPACE="+s.namespace)
	}
	exitCode, log, timedOut := runOcwarden(binPath, []string{"teardown"}, env)
	if timedOut {
		writeJSON(w, http.StatusOK, machineTeardownHereResultDTO{
			MachineID: machine.ID,
			OK:        false,
			ExitCode:  -1,
			Log: "ocwarden teardown timed out (exceeded 60s) — daemon not " +
				"confirmed torn down, member kept",
			Removed: false,
		})
		return
	}
	removed := exitCode == 0
	if removed {
		machine.RosterStatus = RosterStatusRemoved
		machine.DesiredState = DesiredStateOffline
		if err := s.putMember(*machine, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, machineTeardownHereResultDTO{
		MachineID: machine.ID,
		OK:        removed,
		ExitCode:  exitCode,
		Log:       log,
		Removed:   removed,
	})
}

// POST /api/machines/{member_id}/uninstall — the REMOTE machine-lifecycle
// verb: arm desired_state="uninstall" for an ONLINE warden and fire the
// event-driven reconcile (the uninstall RPC dispatches NOW, the cadence stays
// the idempotent backstop); an offline warden is treated as already
// uninstalled (intent left offline, nothing dispatched). The 409 gate counts
// ONLY agents ACTUALLY online on this machine right now (live SSE machine
// claim — hub.AgentsOnMachine); offline agents merely *bound* here
// (desired_machine_id) never block.
func (s *apiServer) HandleUninstallMachineApiMachinesMemberIdUninstallPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMachine(memberId)
	if err != nil {
		writeResolveError(w, err, "machine", memberId)
		return
	}
	if agents := s.hub.AgentsOnMachine(m.ID); len(agents) > 0 {
		writeError(w, http.StatusConflict,
			"machine still has agent(s) running; move or stop them first")
		return
	}
	online := s.hub.IsOnline(m.ID)
	if online {
		m.DesiredState = DesiredStateUninstall
	} else {
		m.DesiredState = DesiredStateOffline
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// EVENT-DRIVEN: dispatch the uninstall now (the click), not on the next
	// ~30s tick. When offline this is an inert no-op — nothing was armed.
	s.reconcileMemberNow(m.ID)
	writeJSON(w, http.StatusOK, machineUninstallResultDTO{
		MemberID:   m.ID,
		MachineID:  m.ID, // the warden's machine id IS its own id
		Dispatched: online,
	})
}

// POST /api/machines/{member_id}/upgrade — the owner's one-click "pull this
// machine current NOW" (T-5f01): enqueue the `update` warden-command verb
// onto the machine's live SSE downstream; the warden's dispatch kicks its
// EXISTING self-update reconcile immediately (the T-c93d kick seam — download
// + verify-before-swap + atomic swap + exec-in-place) instead of waiting out
// the poll backstop. Fire-and-forget BY DESIGN: no desired_state intent, no
// durable write — the self-update loop is idempotent (content-hash swap
// oracle), so a repeated/spurious upgrade is a cheap no-op, and convergence
// shows up as the next heartbeat flipping bin_status to "current". An offline
// warden gets nothing (dispatched=false — its own reconnect-kick already
// self-updates it the moment it comes back). An older warden build refuses
// the unknown verb safely (logged + skipped, reader loop unharmed).
func (s *apiServer) HandleUpgradeMachineApiMachinesMemberIdUpgradePost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMachine(memberId)
	if err != nil {
		writeResolveError(w, err, "machine", memberId)
		return
	}
	dispatched := false
	// --no-reconcile is the shadow-deploy kill-switch over EVERY warden-command
	// dispatch (reconcileMemberNow / dispatchRobustStopNow posture) — a shadow
	// server must never command wardens, including this one-shot kick.
	if !s.noReconcile {
		if frame, ok := buildTargetFrame(reconcileCmdUpdate, m.ID); ok {
			// enqueueWardenFrame carries the same reachability gate as every
			// warden command: offline → nothing enqueued, dispatched stays false.
			dispatched = s.enqueueWardenFrame(m.ID, frame)
		}
	}
	writeJSON(w, http.StatusOK, machineUpgradeResultDTO{
		MemberID:   m.ID,
		MachineID:  m.ID, // the warden's machine id IS its own id
		Dispatched: dispatched,
	})
}

// DELETE /api/machines/{member_id} — a PURE soft-delete of the machine
// record; dispatches nothing. Non-warden → 409; server-self → 409. Same
// actual-online gate as uninstall: agents ACTUALLY online on this machine
// right now → 409; a machine whose agents are all offline deletes directly
// (a desired_machine_id binding alone never blocks).
func (s *apiServer) HandleDeleteMachineApiMachinesMemberIdDelete(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if m.Kind != machineKind {
		writeError(w, http.StatusConflict,
			"member '"+memberId+"' is not a warden machine (kind='"+m.Kind+"')")
		return
	}
	if m.ID == ServerSelfHost {
		writeError(w, http.StatusConflict, "the server-local machine cannot be deleted")
		return
	}
	if agents := s.hub.AgentsOnMachine(m.ID); len(agents) > 0 {
		writeError(w, http.StatusConflict,
			"machine still has agent(s) running; move or stop them first")
		return
	}
	m.RosterStatus = RosterStatusRemoved
	m.DesiredState = DesiredStateOffline
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, machineDeleteResultDTO{
		MemberID:  m.ID,
		MachineID: m.ID,
		Removed:   true,
	})
}

// ── display-name overlays ────────────────────────────────────────────────────

// PATCH /api/accounts/{account_id} — upsert an account display-name overlay
// keyed by the STABLE tag. Blank display_name → 422.
func (s *apiServer) HandleUpdateAccountApiAccountsAccountIdPatch(w http.ResponseWriter, r *http.Request, accountId string) {
	var body AliasUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.DisplayName == nil {
		writeError(w, http.StatusUnprocessableEntity, "display_name is required")
		return
	}
	name := trimString(*body.DisplayName)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "display_name cannot be blank")
		return
	}
	alias := AccountAlias{Account: accountId, DisplayName: name}
	if err := ValidateAccountAlias(alias); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := s.dal.PutAccountAlias(alias); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, aliasDTO{
		ID:            accountId,
		DisplayName:   name,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
	})
}

// PATCH /api/machines/{machine_id} — upsert a machine display-name overlay
// keyed by the STABLE machine id. Blank display_name → 422.
func (s *apiServer) HandleUpdateMachineApiMachinesMachineIdPatch(w http.ResponseWriter, r *http.Request, machineId string) {
	var body AliasUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.DisplayName == nil {
		writeError(w, http.StatusUnprocessableEntity, "display_name is required")
		return
	}
	name := trimString(*body.DisplayName)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "display_name cannot be blank")
		return
	}
	alias := MachineAlias{MachineID: machineId, DisplayName: name}
	if err := ValidateMachineAlias(alias); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := s.dal.PutMachineAlias(alias); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, aliasDTO{
		ID:            machineId,
		DisplayName:   name,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
	})
}

// ── installer + prebuilt binaries (public, secret-free) ──────────────────────

// GET /install.sh — the one-line remote warden installer. PUBLIC; exactly ONE
// of ?code= (one-time claim code — the current boot-command surface) or
// ?token= (legacy: the exec-token itself, byte-identical script kept
// indefinitely) is required — neither, or both, is a 422. The credential only
// authorizes the eventual install, not this fetch.
func (s *apiServer) HandleInstallScriptInstallShGet(w http.ResponseWriter, r *http.Request, params HandleInstallScriptInstallShGetParams) {
	if (params.Token == nil) == (params.Code == nil) {
		writeError(w, http.StatusUnprocessableEntity,
			"exactly one of ?code= or ?token= is required")
		return
	}
	var script string
	if params.Code != nil {
		script = buildInstallScriptWithCode(requestBaseURL(r), *params.Code, s.namespace)
	} else {
		script = buildInstallScript(requestBaseURL(r), *params.Token, s.namespace)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

// serveBinary streams a prebuilt binary as a download: ALWAYS the embedded
// bindist copy (served straight from memory — the download path never needs a
// materialized file), version-locked to this exact ocserverd build. There is
// deliberately no disk override: a stale bin/<filename> in the server's CWD
// must never shadow the copy this server was built with. 503 when the embed
// itself is missing (an honest, actionable failure — never a 404 that reads
// like a bad route).
func serveBinary(w http.ResponseWriter, r *http.Request, filename string, embedded fs.FS) {
	data, err := fs.ReadFile(embedded, filename)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable,
			filename+" binary is not available (no embedded copy in this server build)")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
}

// GET /api/warden/binary — the prebuilt ocwarden download (public: the
// artifact carries no secret; the boot token rides in the install env).
func (s *apiServer) HandleWardenBinaryApiWardenBinaryGet(w http.ResponseWriter, r *http.Request) {
	serveBinary(w, r, "ocwarden", bindistFS())
}

// GET /api/agent/binary — the prebuilt ocagent download (public; a warden
// pulls it to self-update without a Go toolchain).
func (s *apiServer) HandleAgentBinaryApiAgentBinaryGet(w http.ResponseWriter, r *http.Request) {
	serveBinary(w, r, "ocagent", bindistFS())
}
