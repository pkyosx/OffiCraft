package main

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
	"time"

	"ocserverd/txguard"
)

const machineClaimTTLSecs int64 = 600

// One message for unknown, expired and used codes on purpose: telling them
// apart would be a guessing oracle.
const claimCodeDeniedMsg = "claim code is invalid, expired, or already used — " +
	"fetch a fresh boot command from the cockpit"

type machineClaimStore struct {
	mu    txguard.Mutex
	codes map[string]machineClaim
}

type machineClaim struct {
	machineID string
	expiresAt time.Time
}

func newMachineClaimStore() *machineClaimStore {
	return &machineClaimStore{codes: map[string]machineClaim{}}
}

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

// Constant-time scan on purpose: a map lookup on the attacker-supplied code
// would leak timing.
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

// The scheme comes from the Host (the T-78 rule, base_scheme_t78.go), NOT from
// r.TLS: TLS terminates at Cloudflare, so r.TLS is nil for 100% of production
// traffic. X-Forwarded-Proto is deliberately not used either (attacker-
// suppliable). The result is baked into the installed machine for good.
func requestBaseURL(r *http.Request) string {
	return baseURLForHost(r.Host)
}

func baseURLForHost(host string) string {
	return schemeForHost(host) + "://" + host
}

func buildBootCommand(baseURL, code string) string {
	return "curl -fsSL '" + baseURL + "/install.sh?code=" + code + "' | bash"
}

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

const (
	binStatusCurrent = "current"
	binStatusStale   = "stale"
)

// A comparison verdict only — no per-machine version numbers (owner-approved).
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
			continue
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
	return nil
}

// Mirrors the producer's consts in cli/ocwarden/cutover.go — a separate Go
// module with no import between them.
const (
	wardenShapeAnchor  = "anchor"
	wardenShapeLegacy  = "legacy"
	wardenShapeUnknown = "unknown"
)

// "unknown" is a verdict the warden REPORTED; an absent field means a warden
// build that predates the anchor cutover. Never convert one into the other.
func ValidWardenShape(shape string) bool {
	return shape == wardenShapeAnchor || shape == wardenShapeLegacy ||
		shape == wardenShapeUnknown
}

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

// Mirrors the producer's consts in cli/ocwarden/cutovereffect.go (same
// cross-module arrangement as the shape vocabulary above).
const (
	cutoverEffectEffective    = "effective"
	cutoverEffectNotEffective = "not_effective"
	cutoverEffectUnproven     = "unproven"
)

// "unproven" is a reported verdict of its own, and absent (nil) is yet another
// state; never collapse any of them into another — a two-valued light once
// showed a machine whose cutover had NOT taken effect green for three hours.
func ValidCutoverEffect(effect string) bool {
	return effect == cutoverEffectEffective || effect == cutoverEffectNotEffective ||
		effect == cutoverEffectUnproven
}

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

const (
	claudeCredSourceFile     = "file"
	claudeCredSourceKeychain = "keychain"
	claudeCredSourceBoth     = "both"
	claudeCredSourceNone     = "none"
)

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
	// Pre-capability wardens are Claude wardens by construction; Codex never gets
	// this inference and stays fail-closed until probed.
	if len(capabilities) == 0 {
		return normalized == RuntimeClaude
	}
	capability, ok := capabilities[normalized]
	if !ok {
		return false
	}
	// Claude is intentionally permissive: hosts whose credential heuristic
	// false-negatives rely on the spawn-time escape hatch OC_CLAUDE_CRED_CHECK=0.
	// Do not tighten Claude with this Codex-era gate.
	if normalized == RuntimeClaude {
		return true
	}
	if capability.Installed == nil || !*capability.Installed {
		return false
	}
	return capability.LoggedIn == nil || *capability.LoggedIn
}

// Both nil together for a machine never verified: collapsing that into `false`
// would lump it with a machine genuinely still on an old key, and the owner must
// act oppositely on the two.
func (s *apiServer) machineTokenKey(m Member) (*string, *bool) {
	if m.TokenKeyID == "" {
		return nil, nil
	}
	id := m.TokenKeyID
	current := s.keys != nil && s.keys.activeKeyID() == id
	return &id, &current
}

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
		tokenKeyID, tokenKeyCurrent := s.machineTokenKey(m)
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
			TokenKeyID:          tokenKeyID,
			TokenKeyCurrent:     tokenKeyCurrent,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].IsSelf && !rows[j].IsSelf })
	writeJSON(w, http.StatusOK, rows)
}

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
	// ttl_days remains accepted for wire compatibility; the lifetime does not
	// come from it.
	member := Member{
		ID:   "m-" + newHexID(12),
		Name: displayName,
		Kind: machineKind,
		// A warden carries NO self-binding: routing resolves it by its own id, which
		// IS the machine id.
		DesiredMachineID: "",
		DesiredState:     DesiredStateOffline,
		Effort:           "medium",
		RosterStatus:     RosterStatusActive,
	}
	if err := s.dal.inTx(func(tx *writeTx) error {
		if err := writeMemberOn(tx, member); err != nil {
			return err
		}
		return putMachineAliasOn(tx, MachineAlias{
			MachineID:   member.ID,
			DisplayName: displayName,
		})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.publishMemberPatch(member, requestTrigger(r))
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
		MemberID:  member.ID,
		MachineID: member.ID,
		Token:     token,

		ExpiresIn:      int64(s.wardenCredLifetimeValue()),
		BootCommand:    buildBootCommand(requestBaseURL(r), code),
		ClaimCode:      code,
		ClaimExpiresIn: machineClaimTTLSecs,
	})
}

// Every re-install entry point must call this BEFORE installing, or the fresh
// warden reconnects straight into a standing uninstall order (a real
// uninstall→re-install loop).
//
// The fold lands on the row as it is inside its transaction, and *m becomes that row.
func (s *apiServer) clearResidualUninstall(m *Member, trigger string) error {
	if m.DesiredState != DesiredStateUninstall {
		return nil
	}
	folded, fresh, err := s.foldUninstallIntentOnRow(m.ID)
	if err != nil {
		return err
	}
	if fresh != nil {
		*m = *fresh
	}
	if folded != nil {
		s.publishMemberPatch(*folded, trigger)
	}
	return nil
}

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
		ExpiresIn:      int64(s.wardenCredLifetimeValue()),
		ClaimCode:      code,
		ClaimExpiresIn: machineClaimTTLSecs,
	})
}

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
		ExpiresIn: int64(s.wardenCredLifetimeValue()),
		MachineID: machine.ID,
	})
}

const renewNotAWardenMsg = "only a machine credential can be renewed here; " +
	"this endpoint acts on the caller and takes no target"

// Takes no body on purpose: the machine acted on is the caller's own verified
// `sub`, so renewing someone else's credential is impossible by construction.
// Do not add a target parameter — target + check is strictly weaker.
// The route floor (principalMachine) admits ordinary agents too; resolveMachine
// is what keeps this warden-only.
func (s *apiServer) HandleRenewMachineCredentialApiMachinesRenewCredentialPost(w http.ResponseWriter, r *http.Request) {
	machine, err := s.resolveMachine(currentActor(r))
	if err != nil {
		writeError(w, http.StatusForbidden, renewNotAWardenMsg)
		return
	}
	token, err := s.mintWardenToken(*machine)
	if err != nil {
		writeError(w, http.StatusForbidden, renewNotAWardenMsg)
		return
	}
	writeJSON(w, http.StatusOK, machineClaimResultDTO{
		Token:     token,
		ExpiresIn: int64(s.wardenCredLifetimeValue()),
		MachineID: machine.ID,
	})
}

// Why this endpoint stays although the credential has an exp again: wardens
// installed while credentials were permanent hold ones with no exp, and an exp is
// fixed at mint time — the token records what the lifetime WAS, this says what
// it IS.
// Why the renew decision stays on the warden rather than being pushed down the
// `renew` verb: that would make every renewal depend on the SSE downlink, which
// is fail-closed on reachability, so a broken stream would silently never renew.
func (s *apiServer) HandleMachineCredentialPolicyApiMachinesCredentialPolicyGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, machineCredentialPolicyDTO{
		LifetimeSecs: s.wardenCredLifetimeValue(),
	})
}

// bootstrap-here / teardown-here run ocwarden as a CHILD of this server, and the
// child's env decides WHICH instance gets installed or booted out (an inherited
// OC_NAMESPACE, WARDEN_INSTALL_DRYRUN or OC_AGENT_BIN used to steer it). An
// allowlist, not a denylist: cli/ocwarden is a separate module, so a denylist
// silently rots as keys are added, while a missing allowlisted key fails loudly.
// OC_ID is deliberately absent — identity rides solely in the token's sub.
//   - HOME: the child derives root/tokfile/plist from it.
//   - PATH: the child execs `launchctl` and resolves the claude/codex shim by it.
//   - OC_CLAUDE_BIN / OC_CODEX_BIN: stamped into the serve plist for this relay
//     by bin/install.sh and bin/ocserver install (guard:
//     bin/tests/install-claude-stamp.sh); without them the warden refuses spawns.
//   - OC_CLAUDE_CRED_CHECK: an operator's shell export reaches a launchd warden
//     only through this relay.
//
// OC_BASE / OC_TOKEN / OC_NAMESPACE are appended by the callers afterwards, so
// an inherited value can never shadow them.
var ocwardenChildEnvAllowlist = []string{
	"HOME",
	"PATH",
	"OC_CLAUDE_BIN",
	"OC_CODEX_BIN",
	"OC_CLAUDE_CRED_CHECK",
}

// Absent keys stay absent: "unset" and "set to empty" differ to the child.
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

// A var so tests can rebind it and assert the exact child env — the only thing
// proving both call sites use ocwardenChildEnv (reverting them to os.Environ()
// once left the whole suite green).
var runOcwarden = execOcwarden

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

// Embed-only on purpose: a stale bin/ocwarden under the CWD must never be run
// (bootstrap-here once installed a frozen checkout's stale warden that way).
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

// {machine_id} is not a target selector: ocwarden installs under THIS host's
// HOME / uid / namespace, so naming machine B would overwrite this host's warden
// with B's credentials and roster row.
func bootstrapHereForeignTargetMsg(machineID string) string {
	return "bootstrap-here only ever installs the warden running on THIS server " +
		"host — it carries no machine selector, so it cannot reach " + machineID +
		"; refusing rather than overwriting this host's warden with another " +
		"machine's identity. To install a different machine, fetch its own " +
		"one-liner with GET /api/machines/{member_id}/boot-command and run it " +
		"on that host."
}

func bootstrapHereRefusal(machineID string) string {
	if machineID == ServerSelfHost {
		return ""
	}
	return bootstrapHereForeignTargetMsg(machineID)
}

func (s *apiServer) HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost(w http.ResponseWriter, r *http.Request, machineId string) {
	machine, err := s.resolveMachine(machineId)
	if err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
	// Refuse before touching ANY state, or a foreign call still zeroes the target's
	// uninstall intent.
	if refusal := bootstrapHereRefusal(machine.ID); refusal != "" {
		writeError(w, http.StatusConflict, refusal)
		return
	}
	if err := s.clearResidualUninstall(machine, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	binPath, ok := s.resolveOcwardenBinary(w)
	if !ok {
		return
	}
	// The base comes from the server, not the caller's Host: over MCP loopbackCall
	// synthesises `Host: "loopback"`, which handed the installer OC_BASE=http://loopback.
	res, err := s.runWardenInstallHere(*machine, binPath, s.selfBase)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *apiServer) runWardenInstallHere(machine Member, binPath, baseURL string) (bootstrapResultDTO, error) {
	token, err := s.mintWardenToken(machine)
	if err != nil {
		return bootstrapResultDTO{}, err
	}
	env := ocwardenChildEnv(os.Environ())
	env = append(env, "OC_BASE="+baseURL, "OC_TOKEN="+token)
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

func (s *apiServer) runWardenTeardownHere(binPath string) (int, string, bool) {
	env := ocwardenChildEnv(os.Environ())
	args := []string{"teardown"}
	if s.namespace != "" {
		env = append(env, "OC_NAMESPACE="+s.namespace)
	} else {
		// The CLI refuses an implicit canonical target; a bare `teardown`
		// would exit 1 forever and, since removal keys off exit 0, strand the machine
		// in the roster.
		args = append(args, "--canonical")
	}
	return runOcwarden(binPath, args, env)
}

const serverSelfUndeletableMsg = "the server-local machine cannot be deleted"

// teardown-here never consumed {machine_id}: pointed at machine B it
// tore down THIS host's warden and removed B from the roster. The refusal
// deliberately offers no bypass.
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

// One either/or, not two consecutive guards: as two guards the second condition
// was provably always true (replacing it with `if true` left the suite green).
// It never returns "" today: server-self is unretirable (soft-deleting it
// revokes the token of every member placed on it) and every other machine is
// unreachable, so the subprocess path is dead through HTTP. It is kept
// because retiring a frozen-wire route is an owner decision; this function is
// the one place that changes then. The two messages must not merge: they send
// the caller to different fixes (repair vs retire).
func teardownHereRefusal(machineID string) string {
	if machineID == ServerSelfHost {
		return serverSelfUndeletableMsg
	}
	return teardownHereForeignTargetMsg(machineID)
}

func (s *apiServer) HandleTeardownHereApiMachinesMachineIdTeardownHerePost(w http.ResponseWriter, r *http.Request, machineId string) {
	machine, err := s.resolveMachine(machineId)
	if err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
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

// The 409 gate counts ONLY agents actually online on this machine right now
// (hub.AgentsOnMachine); offline agents merely bound here never block.
func (s *apiServer) HandleUninstallMachineApiMachinesMemberIdUninstallPost(w http.ResponseWriter, r *http.Request, memberId string) {
	if _, err := s.resolveMachine(memberId); err != nil {
		writeResolveError(w, err, "machine", memberId)
		return
	}
	if agents := s.hub.AgentsOnMachine(memberId); len(agents) > 0 {
		writeError(w, http.StatusConflict,
			"machine still has agent(s) running; move or stop them first")
		return
	}
	online := s.hub.IsOnline(memberId)
	var m Member
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := resolveMachineOn(tx, memberId)
		if err != nil {
			return err
		}
		if online {
			cur.DesiredState = DesiredStateUninstall
		} else {
			cur.DesiredState = DesiredStateOffline
		}
		m = *cur
		return writeMemberOn(tx, m)
	})
	if err != nil {
		writeResolveTxError(w, err, "machine", memberId)
		return
	}
	s.publishMemberPatch(m, requestTrigger(r))
	s.reconcileMemberNow(m.ID)
	writeJSON(w, http.StatusOK, machineUninstallResultDTO{
		MemberID:   m.ID,
		MachineID:  m.ID,
		Dispatched: online,
	})
}

// Fire-and-forget by design: the warden's self-update is idempotent
// (content-hash swap), so there is no durable intent; an offline warden
// self-updates on reconnect, and an older warden skips the unknown verb safely.
func (s *apiServer) HandleUpgradeMachineApiMachinesMemberIdUpgradePost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMachine(memberId)
	if err != nil {
		writeResolveError(w, err, "machine", memberId)
		return
	}
	dispatched := false
	// --no-reconcile gates this one dispatch; it is NOT a server-wide gate over
	// warden commands (spec/lifecycle.md §4.1).
	if !s.noReconcile {
		if frame, ok := buildTargetFrame(reconcileCmdUpdate, m.ID); ok {
			dispatched = s.enqueueWardenFrame(m.ID, frame)
		}
	}
	writeJSON(w, http.StatusOK, machineUpgradeResultDTO{
		MemberID:   m.ID,
		MachineID:  m.ID,
		Dispatched: dispatched,
	})
}

func (s *apiServer) HandleDeleteMachineApiMachinesMemberIdDelete(w http.ResponseWriter, r *http.Request, memberId string) {
	// The open resolver on purpose: the kind check below is the real guard, and it
	// answers the honest 409 "not a warden" rather than a 404.
	m, err := s.resolveMember(memberId, anyMember)
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
		writeError(w, http.StatusConflict, serverSelfUndeletableMsg)
		return
	}
	if agents := s.hub.AgentsOnMachine(m.ID); len(agents) > 0 {
		writeError(w, http.StatusConflict,
			"machine still has agent(s) running; move or stop them first")
		return
	}
	// Removed on the row as it is inside the transaction: a column another writer
	// landed since the read above (a connect edge's session anchor) stands.
	var removed Member
	err = s.dal.inTx(func(tx *writeTx) error {
		cur, err := getMemberOn(tx, m.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			return errNotFound
		}
		cur.RosterStatus = RosterStatusRemoved
		cur.DesiredState = DesiredStateOffline
		removed = *cur
		return writeMemberOn(tx, *cur)
	})
	if err != nil {
		writeResolveTxError(w, err, "member", memberId)
		return
	}
	s.publishMemberPatch(removed, requestTrigger(r))
	writeJSON(w, http.StatusOK, machineDeleteResultDTO{
		MemberID:  m.ID,
		MachineID: m.ID,
		Removed:   true,
	})
}

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
	if err := s.dal.inTx(func(*writeTx) error { return s.dal.PutAccountAlias(alias) }); err != nil {
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
	if err := s.dal.inTx(func(*writeTx) error { return s.dal.PutMachineAlias(alias) }); err != nil {
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

// ?token= is the legacy form; its script is kept byte-identical indefinitely.
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

func (s *apiServer) HandleWardenBinaryApiWardenBinaryGet(w http.ResponseWriter, r *http.Request) {
	serveBinary(w, r, "ocwarden", bindistFS())
}

func (s *apiServer) HandleAgentBinaryApiAgentBinaryGet(w http.ResponseWriter, r *http.Request) {
	serveBinary(w, r, "ocagent", bindistFS())
}
