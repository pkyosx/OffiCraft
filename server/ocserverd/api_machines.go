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
	"strings"
	"sync"
	"time"
)

// machineClaimTTLSecs is the lifetime of a one-time machine claim code — the
// short-lived credential the boot command carries INSTEAD of the permanent
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

// warden_shape wire vocabulary (AgentTelemetryIngestDTO.warden_shape →
// machineDTO.warden_shape / MonitoringMachineDTO): which launchd SHAPE a
// machine's warden reported it is actually running under. Mirrors the producer's
// own consts (cli/ocwarden/cutover.go) — two Go modules with no import between
// them, so the vocabulary is checked against these literals on the way in.
const (
	wardenShapeAnchor  = "anchor"
	wardenShapeLegacy  = "legacy"
	wardenShapeUnknown = "unknown"
)

// ValidWardenShape gates the ingest handler's closed enum. "unknown" is a
// REPORTED verdict ("the warden ran and could not read its parent"), not a
// fallback the server may invent — an absent field means something else entirely
// ("this warden build predates the anchor-cutover release") and the two are
// never converted into one another.
func ValidWardenShape(shape string) bool {
	return shape == wardenShapeAnchor || shape == wardenShapeLegacy ||
		shape == wardenShapeUnknown
}

// machineWardenShape reads back the shape machineID's warden REPORTED, keyed the
// same way as machineBinStatus (the warden's own member id IS the machine id).
//
// 🔴 The contrast with machineBinStatus one screen up is the whole point of this
// function existing separately: bin_status is COMPUTED here, by comparing what
// the machine reported against what the server embeds. There is no equivalent
// second source for the shape — only the reporting process can see its own
// parent — so this passes the stored value through and derives nothing. An
// absent value stays absent: nil is "this warden has never reported a shape",
// which the wire keeps distinct from the reported "unknown".
func (s *apiServer) machineWardenShape(machineID string) *string {
	if s.telemetry == nil {
		return nil
	}
	entry := s.telemetry.Get(machineID)
	if entry == nil {
		return nil
	}
	shape, isStr := entry["warden_shape"].(string)
	if !isStr || shape == "" {
		return nil
	}
	return &shape
}

// cutover_effect wire vocabulary (AgentTelemetryIngestDTO.cutover_effect →
// machineDTO.cutover_effect / MonitoringMachineDTO): whether the anchor cutover
// is actually IN EFFECT for the processes that carry agents. Mirrors the
// producer's own consts (cli/ocwarden/cutovereffect.go) across the module
// boundary, exactly like the shape vocabulary above.
const (
	cutoverEffectEffective    = "effective"
	cutoverEffectNotEffective = "not_effective"
	cutoverEffectUnproven     = "unproven"
)

// ValidCutoverEffect gates the ingest handler's closed enum.
//
// 🔴 "unproven" is a REPORTED verdict of its own, not a degenerate "effective".
// The whole reason this field exists is that a two-valued light let a machine
// whose cutover had NOT taken effect show green for three hours, so nothing on
// this side may ever collapse the third state into either of the other two.
func ValidCutoverEffect(effect string) bool {
	return effect == cutoverEffectEffective || effect == cutoverEffectNotEffective ||
		effect == cutoverEffectUnproven
}

// machineCutoverEffect reads back the verdict machineID's warden REPORTED. Like
// machineWardenShape it derives nothing: only the reporting machine can see its
// own carrier processes, so the server has no second source to compute from. An
// absent value stays absent — nil is "this warden build does not report the
// verdict", which the wire keeps distinct from the reported "unproven".
func (s *apiServer) machineCutoverEffect(machineID string) *string {
	if s.telemetry == nil {
		return nil
	}
	entry := s.telemetry.Get(machineID)
	if entry == nil {
		return nil
	}
	effect, isStr := entry["cutover_effect"].(string)
	if !isStr || effect == "" {
		return nil
	}
	return &effect
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

// machineRuntimeCapabilities projects the provider-neutral readiness probes
// from a warden heartbeat. Older wardens return an empty map: placement treats
// unknown as unavailable instead of dispatching a runtime that cannot launch.
func (s *apiServer) machineRuntimeCapabilities(machineID string) map[string]RuntimeCapabilityDTO {
	out := map[string]RuntimeCapabilityDTO{}
	entry := s.telemetry.Get(machineID)
	if entry == nil {
		return out
	}
	raw, _ := entry["runtimes"].(map[string]any)
	for name, value := range raw {
		if !ValidRuntime(name) {
			continue
		}
		obj, ok := value.(map[string]any)
		if !ok {
			continue
		}
		capability := RuntimeCapabilityDTO{}
		if v, ok := obj["installed"].(bool); ok {
			capability.Installed = &v
		}
		if v, ok := obj["logged_in"].(bool); ok {
			capability.LoggedIn = &v
		}
		if v, ok := obj["version"].(string); ok {
			capability.Version = &v
		}
		out[name] = capability
	}
	return out
}

func (s *apiServer) machineSupportsRuntime(machineID, runtime string) bool {
	normalized := NormalizeRuntime(runtime)
	capabilities := s.machineRuntimeCapabilities(machineID)
	// Rolling-upgrade compatibility: every pre-capability warden is a Claude
	// warden by construction. Codex never gets this inference and therefore
	// remains fail-closed until explicitly probed.
	if len(capabilities) == 0 {
		return normalized == RuntimeClaude
	}
	capability, ok := capabilities[normalized]
	if !ok {
		// A reported map is the warden's full answer: a runtime it did not
		// mention is absent, not unknown. Only the no-map case above may infer
		// Claude.
		return false
	}
	// Claude's historic placement contract is intentionally permissive: the
	// spawn-time operator escape hatch OC_CLAUDE_CRED_CHECK=0 exists for hosts
	// whose credential heuristic false-negatives.  Codex introduced this
	// placement gate; do not retroactively tighten Claude with it.
	if normalized == RuntimeClaude {
		return true
	}
	if capability.Installed == nil || !*capability.Installed {
		return false
	}
	return capability.LoggedIn == nil || *capability.LoggedIn
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
			MachineID:           m.ID,
			DisplayName:         display,
			Online:              s.hub.IsOnline(m.ID),
			IsSelf:              m.ID == ServerSelfHost,
			BinStatus:           s.machineBinStatus(m.ID),
			ClaudeVersion:       claudeVersion,
			ClaudeCredSource:    claudeCredSource,
			ClaudeSubReadable:   claudeSubReadable,
			RuntimeCapabilities: s.machineRuntimeCapabilities(m.ID),
			WardenShape:         s.machineWardenShape(m.ID),
			CutoverEffect:       s.machineCutoverEffect(m.ID),
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
	// ttl_days remains accepted for wire compatibility, but cannot turn a
	// warden credential back into an expiring one.
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
	token, err := s.mintWardenToken(member)
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
		ExpiresIn:      0, // 0 is the wire sentinel for a credential with no exp.
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
	token, err := s.mintWardenToken(*machine)
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
		ExpiresIn:      0,
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
	token, err := s.mintWardenToken(*machine)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, machineClaimResultDTO{
		Token:     token,
		ExpiresIn: 0,
		MachineID: machine.ID,
	})
}

// ---------------------------------------------------------------------------
// child env — ALLOWLIST (fail-closed), not a denylist
// ---------------------------------------------------------------------------
//
// bootstrap-here / teardown-here run `ocwarden` AS A CHILD OF THIS SERVER. The
// child's env is what decides WHICH instance gets installed or torn down: root
// (~/.officraft[-ns]), launchd label, tokfile, identity, even whether it does
// anything at all. Until T-5047 the child inherited os.Environ() WHOLESALE with
// a single key scrubbed (OC_ID), i.e. a denylist of length one. Everything else
// the serve process happened to have in its environment steered the install:
//
//	OC_NAMESPACE          — inherited on a MAIN-instance server (s.namespace ""),
//	                        so a stray value installs — or, on teardown-here,
//	                        BOOTS OUT — a completely different instance's warden.
//	WARDEN_INSTALL_DRYRUN — inherited "1" makes `ocwarden install` mutate nothing
//	                        and exit 0: the server reports a successful install
//	                        of a warden that does not exist.
//	OC_AGENT_BIN          — redirects the installed ocagent to an arbitrary local
//	                        file instead of the server's own verified download.
//	OC_WARDEN_TOKFILE, OC_BASE, OC_TOKEN, OC_AGENT_HOME, …
//
// WHY AN ALLOWLIST. A denylist is only correct for the variables someone thought
// of, and it silently stops being correct every time either side grows a new
// key — the child (cli/ocwarden) and the parent (this file) are separate modules
// with no compiler between them. That failure mode is not hypothetical here: the
// one-key denylist below was written when OC_NAMESPACE did not exist yet, and it
// was still "correct" the day namespacing shipped. An allowlist fails the other
// way: a variable the child newly depends on is simply ABSENT, which surfaces as
// a loud install failure on the very first run, not as an install that silently
// went somewhere else. Fail-closed beats fail-open for anything that can bootout
// a live warden.
//
// EVERY entry below is a DELIBERATE relay with a reason:
//   - HOME  — the child derives root/tokfile/plist from it; it is the install.
//   - PATH  — the child execs `launchctl` by name, and resolves the claude/codex
//     shim under this PATH (the serve plist's enriched PATH is exactly what
//     `bin/ocserver install` / bin/install.sh stamped there for this hop).
//   - OC_CLAUDE_BIN / OC_CODEX_BIN — the runtime-binary stamps that must survive
//     the launchd-minimal env; without them the installed warden refuses every
//     spawn of that runtime (claude_bin_unresolved / runtime_bin_unresolved).
//     BOTH are stamped into the serve plist for this relay, by bin/install.sh and
//     by bin/ocserver install alike — the codex half of that was missing until
//     T-ff48, which is why a codex member could not spawn on a version-manager
//     host whose claude worked. The guard is bin/tests/install-claude-stamp.sh.
//   - OC_CLAUDE_CRED_CHECK — the advertised escape hatch for the spawn-time
//     credential gate. A warden is a launchd job, so an operator's shell export
//     reaches it ONLY through this relay.
//
// Everything the server MEANS to set (OC_BASE / OC_TOKEN / OC_NAMESPACE) is
// appended by the callers AFTER this, from server-computed values — so it can
// never be shadowed by whatever the serve process inherited.
var ocwardenChildEnvAllowlist = []string{
	"HOME",
	"PATH",
	"OC_CLAUDE_BIN",
	"OC_CODEX_BIN",
	"OC_CLAUDE_CRED_CHECK",
}

// ocwardenChildEnv projects `environ` down to ocwardenChildEnvAllowlist. Keys
// absent from the parent env are simply not emitted (an empty value is NOT
// synthesised — "unset" and "set to empty" mean different things to the child).
func ocwardenChildEnv(environ []string) []string {
	allowed := make(map[string]bool, len(ocwardenChildEnvAllowlist))
	for _, k := range ocwardenChildEnvAllowlist {
		allowed[k] = true
	}
	out := make([]string, 0, len(ocwardenChildEnvAllowlist))
	for _, kv := range environ {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		if allowed[kv[:eq]] {
			out = append(out, kv)
		}
	}
	return out
}

// runOcwarden is the SEAM through which bootstrap-here / teardown-here reach the
// ocwarden child. Production leaves it at execOcwarden; the test binary rebinds it
// to a recorder so a test can assert on the EXACT env the child would have been
// handed — which is the only way to pin the WIRING rather than the projection.
//
// WHY A VAR. ocwardenChildEnv (the allowlist) was fully unit-tested as a pure
// function while NOTHING pinned that these two call sites actually call it: with
// `env := ocwardenChildEnv(os.Environ())` reverted to `env := os.Environ()` at both
// sites — the pre-T-5047 defect restored byte for byte — the whole server suite
// stayed green. A projection nobody is proven to call is not a defence.
var runOcwarden = execOcwarden

// execOcwarden runs `<ocwarden> <verb>` bounded by 60s (the injectable-runner
// twins of handlers._default_bootstrap_runner / _default_teardown_runner).
// argv list only — zero command-injection surface; the wiring rides in env.
// Returns (exitCode, mergedLog, timedOut).
func execOcwarden(binPath string, args []string, env []string) (int, string, bool) {
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
	embedded := s.ocwardenFS
	if embedded == nil {
		embedded = bindistFS()
	}
	path, err := s.resolveOcwardenBinaryFrom(embedded)
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
	anchor, err := fs.ReadFile(embedded, "officraft")
	if err != nil {
		return "", err
	}
	if s.binCacheDir == "" {
		return "", errors.New("no binary cache directory configured")
	}
	if _, err := materializeBinary(s.binCacheDir, "officraft", anchor); err != nil {
		return "", err
	}
	return materializeBinary(s.binCacheDir, "ocwarden", data)
}

// bootstrapHereForeignTargetMsg is the refusal bootstrap-here owes a caller who
// named a machine other than this server's own. Like teardown-here, this verb
// carries NO machine selector on the path it actually walks: ocwarden installs
// under THIS host's HOME / uid / namespace. The {machine_id} only picks whose
// IDENTITY the local install claims — so naming machine B overwrites this
// host's warden with B's credentials and B's roster row.
//
// Until T-ce3d that was accidentally impossible over MCP: loopbackCall
// synthesises `Host: "loopback"`, and the base derived from it made the
// installer's download fail. Taking the base from the server (the fix this
// ticket ships) removes that accident, so the refusal has to be explicit.
func bootstrapHereForeignTargetMsg(machineID string) string {
	return "bootstrap-here only ever installs the warden running on THIS server " +
		"host — it carries no machine selector, so it cannot reach " + machineID +
		"; refusing rather than overwriting this host's warden with another " +
		"machine's identity. To install a different machine, fetch its own " +
		"one-liner with GET /api/machines/{member_id}/boot-command and run it " +
		"on that host."
}

// bootstrapHereRefusal answers "what does bootstrap-here owe a caller who named
// this machine?" and returns "" when the target may proceed. Shaped after
// teardownHereRefusal so the two host-mutating verbs cannot drift into
// disagreeing about who they are allowed to act on.
func bootstrapHereRefusal(machineID string) string {
	if machineID == ServerSelfHost {
		return ""
	}
	return bootstrapHereForeignTargetMsg(machineID)
}

// POST /api/machines/{machine_id}/bootstrap-here — install this machine's
// warden ON THE SERVER HOST (requires=admin_agent on the route table since
// T-6020; a plain agent is a flat 403). A non-zero exit
// is NOT an HTTP error: ok=false with the log surfaced.
//
// ⚠️ {machine_id} IS NOT A TARGET SELECTOR, exactly as on teardown-here
// (T-42a0): naming anything but the server-local machine is a 409.
func (s *apiServer) HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost(w http.ResponseWriter, r *http.Request, machineId string) {
	machine, err := s.resolveMachine(machineId)
	if err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
	// Before ANY state is touched: a refused target must not even have its
	// uninstall intent zeroed, or a foreign call still mutates the roster.
	if refusal := bootstrapHereRefusal(machine.ID); refusal != "" {
		writeError(w, http.StatusConflict, refusal)
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
	// T-ce3d: the base comes from the SERVER, not from the caller's Host
	// header. bootstrap-here installs the warden ON THIS HOST, so the address
	// it must call home on is one the server already knows — and asking the
	// request instead made this path structurally impossible over MCP, where
	// loopbackCall synthesises `Host: "loopback"` and the installer was handed
	// OC_BASE=http://loopback. The first-run onboarding path (onboarding.go)
	// has always passed s.selfBase; this makes the cockpit button agree with
	// it rather than each deriving its own answer.
	res, err := s.runWardenInstallHere(*machine, binPath, s.selfBase)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// runWardenInstallHere is the bootstrap-here CORE, split out (T-ba62) so the
// automatic first-run onboarding can install this host's warden through the
// EXACT same path the cockpit button uses — one implementation, one set of
// semantics, no second copy to drift. HTTP concerns stay in the handler; this
// returns the result DTO (a non-zero exit is a RESULT, not an error) and an
// error only for a genuine server fault (token mint).
func (s *apiServer) runWardenInstallHere(machine Member, binPath, baseURL string) (bootstrapResultDTO, error) {
	token, err := s.mintWardenToken(machine)
	if err != nil {
		return bootstrapResultDTO{}, err
	}
	// env = the ALLOWLISTED projection of the server process env (see
	// ocwardenChildEnvAllowlist: HOME/PATH + the deliberate claude/codex/cred
	// relays) plus the OC_* wiring THIS server computes. OC_ID is not on the
	// allowlist — identity rides SOLELY in the token's sub — and neither is
	// anything else that could steer the install (OC_NAMESPACE, OC_AGENT_BIN,
	// WARDEN_INSTALL_DRYRUN, a stray OC_BASE/OC_TOKEN, …).
	env := ocwardenChildEnv(os.Environ())
	env = append(env, "OC_BASE="+baseURL, "OC_TOKEN="+token)
	// Namespaced instance: the warden it installs on this host must key its
	// root/label/socket off the same namespace (single propagation env).
	if s.namespace != "" {
		env = append(env, "OC_NAMESPACE="+s.namespace)
	}
	exitCode, log, timedOut := runOcwarden(binPath, []string{"install", "--force"}, env)
	if timedOut {
		return bootstrapResultDTO{
			MachineID: machine.ID,
			OK:        false,
			ExitCode:  -1,
			Log:       "ocwarden install timed out (exceeded 60s) — no changes confirmed",
		}, nil
	}
	return bootstrapResultDTO{
		MachineID: machine.ID,
		OK:        exitCode == 0,
		ExitCode:  exitCode,
		Log:       log,
	}, nil
}

// runWardenTeardownHere is the teardown-here CORE — the exact twin of
// runWardenInstallHere, split out for the SAME reason: the env this builds is the
// whole safety story of the verb, and it has to be reachable by a test without an
// HTTP recorder, an embedded bindist, or a real launchd domain.
//
// teardown is identity-agnostic (HOME/uid only) — no OC_* wiring needed, EXCEPT
// the instance namespace: a namespaced server must tear down its OWN warden
// (label/tokfile under its namespace), never the main instance's. SAME ALLOWLIST
// as bootstrap-here, and it matters MORE here: this verb BOOTS OUT a launchd job.
// A main-instance server that merely inherited a stray OC_NAMESPACE used to tear
// down a DIFFERENT instance's live warden.
func (s *apiServer) runWardenTeardownHere(binPath string) (int, string, bool) {
	env := ocwardenChildEnv(os.Environ())
	args := []string{"teardown"}
	if s.namespace != "" {
		env = append(env, "OC_NAMESPACE="+s.namespace)
	} else {
		// T-2257: canonical teardown is destructive and the CLI now REFUSES an
		// implicit canonical target. The server has already resolved which
		// instance it is, so it must spell the authorization — a bare
		// `teardown` here would exit 1 forever and, because CONFIRM-THEN-REMOVE
		// keys off exit 0, strand the machine in the roster.
		args = append(args, "--canonical")
	}
	return runOcwarden(binPath, args, env)
}

// serverSelfUndeletableMsg is the ONE refusal both verbs that would soft-delete
// the server-local machine speak. It is a shared constant rather than two
// literals because the two refusals must never drift into disagreeing about
// whether server-self is retirable — the drift is the defect, not the wording.
const serverSelfUndeletableMsg = "the server-local machine cannot be deleted"

// teardownHereForeignTargetMsg is the refusal for the defect T-42a0 exists to
// close: `teardown-here` NEVER consumed the {machine_id} it was handed.
// runWardenTeardownHere builds an argv+env addressed by HOME / uid / namespace
// ONLY — there is no machine selector anywhere on that path — so pointing this
// verb at machine B ran `ocwarden teardown` against THIS server host's own
// warden and then wrote RosterStatusRemoved onto B. Two live daemons in one
// click: the local one destroyed, the named one stranded off the roster (and,
// since T-9cf8, with its credentials revoked). The name promised a target the
// implementation could not reach.
//
// The refusal deliberately names the two verbs that DO what the caller meant,
// and deliberately does NOT offer a way around itself: there is no flag, no
// query parameter and no alternate route that makes this endpoint reach
// another host. Teaching a bypass here would just re-open the hole under a
// longer URL.
func teardownHereForeignTargetMsg(machineID string) string {
	return "teardown-here only ever tears down the warden running on THIS server " +
		"host — it carries no machine selector, so it cannot reach " + machineID +
		"; refusing rather than destroying this host's daemon under another " +
		"machine's name. To retire a different machine, use POST " +
		"/api/machines/{member_id}/uninstall (the remote uninstall the target's " +
		"own warden executes) and then DELETE /api/machines/{member_id}. To " +
		"repair this host's own warden, use install_warden_on_server_host — it " +
		"runs `ocwarden install --force`, which overwrites an existing install, " +
		"so nothing has to be torn down first."
}

// teardownHereRefusal answers ONE question — "what does teardown-here owe a
// caller who named this machine?" — and returns "" when the target may proceed.
//
// WHY THE TWO REFUSALS SHARE A FUNCTION. Written as two consecutive guards in
// the handler, the second condition (`machine.ID != ServerSelfHost`, reached
// only after the first one returned) was PROVABLY ALWAYS TRUE: an independent
// review replaced it with `if true` and the entire suite stayed green. A
// condition with no discriminating power is worse than no condition, because it
// reads like a second layer of protection that is not there. Here the branch is
// a genuine either/or — WHICH sentence the caller gets — and both directions
// are pinned (TestTeardownHere_ServerLocalRefusalIsUnchanged and
// TestTeardownHereRefusesAnOrdinaryMachineToo fail if it is forced either way).
//
// WHY IT NEVER RETURNS "" TODAY, and why that is not hidden behind a bare
// `return`: the server-local machine is unretirable (T-9cf8 — soft-deleting it
// revokes its credentials and the token of every member placed on it), and
// every other machine is unreachable (T-42a0 — this verb carries no machine
// selector). So the handler's subprocess and its CONFIRM-THEN-REMOVE fold are
// currently DEAD THROUGH HTTP. They are kept because retiring a route on the
// frozen wire is an owner decision, not a side effect of closing a defect, and
// the "" return is kept because the day that decision lands, this function is
// the single place that changes. runWardenTeardownHere keeps its own direct
// tests either way.
//
// THE TWO SENTENCES MUST NOT MERGE. They send the caller to different places —
// server-self to `install --force` (repair), anything else to uninstall +
// delete (retire) — so a caller given the wrong one goes looking for the wrong
// fix. That is asserted, not just asked for.
func teardownHereRefusal(machineID string) string {
	if machineID == ServerSelfHost {
		return serverSelfUndeletableMsg
	}
	return teardownHereForeignTargetMsg(machineID)
}

// POST /api/machines/{machine_id}/teardown-here — tear the warden down ON THE
// SERVER HOST (requires=admin_agent since T-6020). CONFIRM-THEN-REMOVE: the member is soft-deleted
// ONLY on a confirmed teardown (exit 0).
//
// ⚠️ {machine_id} IS NOT A TARGET SELECTOR — it never was (T-42a0). The child
// process is addressed by HOME / uid / OC_NAMESPACE, i.e. always THIS host.
// The path parameter therefore only identifies which roster row the caller
// claims to be talking about, and both possible answers are refused by the
// single guard below.
func (s *apiServer) HandleTeardownHereApiMachinesMachineIdTeardownHerePost(w http.ResponseWriter, r *http.Request, machineId string) {
	machine, err := s.resolveMachine(machineId)
	if err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
	// The refusals themselves live in teardownHereRefusal (below the DTO
	// helpers); what follows is the RECORD OF WHY each one was written, kept
	// next to the call site because that is where someone tempted to loosen
	// them will be standing.
	//
	// server-self is NOT retirable — the SAME 409 DELETE /api/machines already
	// speaks, mirrored rather than reinvented (T-9cf8 follow-up).
	//
	// WHY THIS GUARD IS PART OF *THIS* TICKET AND NOT A DRIVE-BY: T-9cf8 did
	// not create this hole, it RAISED THE PRICE OF FALLING INTO IT. Before,
	// tearing down the server's own warden left the row soft-deleted and the
	// host merely offline. Now the roster is the authority over machine
	// credentials, so the same soft-delete ALSO revokes the token of every
	// agent whose desired_machine_id is ServerSelfHost — which, per dbseed.go,
	// is the default placement for the seeded roster. Whoever raises the cost
	// of an action owns closing the guard; otherwise the bill goes to the next
	// person who presses it.
	//
	// AND IT IS NOT A LEGITIMATE FLOW BEING BLOCKED — checked before writing it:
	//   - NO UI path calls it. `teardownOnServer` exists in the frontend api
	//     layer but is referenced by zero components; the only live driver is
	//     the MCP tool uninstall_warden_on_server_host.
	//   - it is ALREADY a one-way trip for every machine, self included:
	//     teardown-here sets RosterStatusRemoved, and resolveMachine demands
	//     RosterStatusActive, so bootstrap-here and boot-command both 404
	//     afterwards and GET /api/machines filters the row out of the cockpit.
	//     "Tear the server host down and rebuild it" therefore cannot be an
	//     existing migration flow — there is no path back.
	//   - a restart does not heal it either: seedOutOfBox only seeds
	//     ServerSelfHost when GetMember returns nil, and a soft-deleted row is
	//     not nil.
	//   - instance decommission / e2e rebuild go through the SHELL
	//     `ocwarden teardown` (e2e_test oc_teardown_bounded), not this endpoint,
	//     so namespaced-instance cleanup is untouched by this refusal.
	//   - conformance only ever aims this route at an unknown machine id (404).
	// ONE guard, not two (T-42a0). Both refusals live in teardownHereRefusal so
	// that the branch here is a real question with a real answer, and so that
	// nobody reads this spot as two independent layers of protection. See that
	// function for why it currently never lets anything through, and for the
	// discipline that keeps the two sentences from merging.
	if refusal := teardownHereRefusal(machine.ID); refusal != "" {
		writeError(w, http.StatusConflict, refusal)
		return
	}
	binPath, ok := s.resolveOcwardenBinary(w)
	if !ok {
		return
	}
	exitCode, log, timedOut := s.runWardenTeardownHere(binPath)
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
	// This one-shot kick borrows the reconcile producer's --no-reconcile
	// posture (reconcileMemberNow / dispatchRobustStopNow): the flag gates THIS
	// dispatch. It is NOT a server-wide gate over every warden command — what
	// it does not cover is enumerated in spec/lifecycle.md §4.1.
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
		// Shared with teardown-here (the other verb that would soft-delete this
		// row) so the two refusals cannot drift apart.
		writeError(w, http.StatusConflict, serverSelfUndeletableMsg)
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
