package main

// api_machines_test.go — machine-lifecycle handler tests: the uninstall
// one-shot-intent hygiene on the install paths (先歸零再裝) and the
// actual-online delete gate. Handlers are invoked directly (auth lives on the
// route table, not in the handler bodies).

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// newMachinesTestServer wires an apiServer whose checkout root is an EMPTY
// temp dir: resolveOcwardenBinary answers 503 and no real `ocwarden install`
// can ever run on the test host.
func newMachinesTestServer(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "machines-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return newAPIServer(dal, NewHub(), []byte("machines-test-secret"), 3600,
		assetRoot(t.TempDir()))
}

func putResidualUninstallWarden(t *testing.T, s *apiServer, id string) {
	t.Helper()
	putTestMember(t, s, Member{
		ID: id, Name: id, Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateUninstall, RosterStatus: RosterStatusActive,
	})
}

func desiredStateOf(t *testing.T, s *apiServer, id string) string {
	t.Helper()
	m, err := s.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("get member %s: %v", id, err)
	}
	return m.DesiredState
}

func TestHandleMachineBootCommand(t *testing.T) {
	t.Run("clears a residual uninstall intent before re-minting the installer", func(t *testing.T) {
		s := newMachinesTestServer(t)
		putResidualUninstallWarden(t, s, "m-box")

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/machines/m-box/boot-command", nil)
		s.HandleMachineBootCommandApiMachinesMachineIdBootCommandGet(rec, req, "m-box")
		if rec.Code != http.StatusOK {
			t.Fatalf("boot-command: %d %s", rec.Code, rec.Body.String())
		}
		if got := desiredStateOf(t, s, "m-box"); got != DesiredStateOffline {
			t.Fatalf("install path must zero the residual uninstall intent, got %q", got)
		}
	})

	t.Run("boot command embeds a fresh claim code, never the exec-token", func(t *testing.T) {
		s := newMachinesTestServer(t)
		putResidualUninstallWarden(t, s, "m-box")

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/machines/m-box/boot-command", nil)
		s.HandleMachineBootCommandApiMachinesMachineIdBootCommandGet(rec, req, "m-box")
		if rec.Code != http.StatusOK {
			t.Fatalf("boot-command: %d %s", rec.Code, rec.Body.String())
		}
		var dto bootCommandResultDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if dto.ClaimCode == "" || dto.ClaimExpiresIn != machineClaimTTLSecs {
			t.Fatalf("claim_code/claim_expires_in missing or wrong: %+v", dto)
		}
		if !strings.Contains(dto.BootCommand, "/install.sh?code="+dto.ClaimCode) {
			t.Fatalf("boot command must carry the claim code: %q", dto.BootCommand)
		}
		if strings.Contains(dto.BootCommand, dto.Token) {
			t.Fatalf("boot command must never embed the exec-token: %q", dto.BootCommand)
		}
	})
}

func TestHandleBootstrapHere(t *testing.T) {
	t.Run("clears a residual uninstall intent before touching the installer", func(t *testing.T) {
		s := newMachinesTestServer(t)
		putResidualUninstallWarden(t, s, "m-box")

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/machines/m-box/bootstrap-here", nil)
		s.HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost(rec, req, "m-box")
		// The empty test root carries no bin/ocwarden → 503; the intent zeroing
		// precedes the binary (先歸零再裝), so the residue is spent regardless.
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 (no ocwarden binary in the test root): %d %s",
				rec.Code, rec.Body.String())
		}
		if got := desiredStateOf(t, s, "m-box"); got != DesiredStateOffline {
			t.Fatalf("install path must zero the residual uninstall intent, got %q", got)
		}
	})
}

func TestResolveOcwardenBinaryFrom(t *testing.T) {
	embedded := fstest.MapFS{"ocwarden": {Data: []byte("embedded warden bytes")}}

	// EMBED-ONLY (T-e731): a stale bin/ocwarden under the CWD must never shadow
	// the embed — disk-first once had bootstrap-here exec a frozen checkout's
	// stale warden (the third crash of the trilogy). The embed is always
	// materialized and run, disk copy or not. Mirrors serveBinary's download
	// path, already embed-only.
	t.Run("ignores a stale on-disk bin/ocwarden, materializes the embed", func(t *testing.T) {
		s := newMachinesTestServer(t)
		s.binCacheDir = filepath.Join(t.TempDir(), "cache-bin")
		binDir := filepath.Join(string(s.root), "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(binDir, "ocwarden"), []byte("STALE disk warden"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := s.resolveOcwardenBinaryFrom(embedded)
		if err != nil {
			t.Fatalf("resolveOcwardenBinaryFrom: %v", err)
		}
		if got != filepath.Join(s.binCacheDir, "ocwarden") {
			t.Fatalf("want the materialized cache path (never the stale disk path), got %q", got)
		}
		raw, _ := os.ReadFile(got)
		if string(raw) != "embedded warden bytes" {
			t.Fatalf("want the embed bytes to win over the stale disk copy, got %q", raw)
		}
	})

	t.Run("materializes the embed as an executable", func(t *testing.T) {
		s := newMachinesTestServer(t)
		s.binCacheDir = filepath.Join(t.TempDir(), "cache-bin")

		got, err := s.resolveOcwardenBinaryFrom(embedded)
		if err != nil {
			t.Fatalf("resolveOcwardenBinaryFrom: %v", err)
		}
		if got != filepath.Join(s.binCacheDir, "ocwarden") {
			t.Fatalf("want the cache path, got %q", got)
		}
		info, err := os.Stat(got)
		if err != nil {
			t.Fatalf("materialized file missing: %v", err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("materialized binary must be executable, mode %v", info.Mode())
		}
		raw, _ := os.ReadFile(got)
		if string(raw) != "embedded warden bytes" {
			t.Fatalf("materialized bytes drifted: %q", raw)
		}

		// Idempotent: a second resolve reuses the same path without error.
		again, err := s.resolveOcwardenBinaryFrom(embedded)
		if err != nil || again != got {
			t.Fatalf("second resolve must reuse the cache (%q, %v)", again, err)
		}
	})

	t.Run("errs without a cache dir or with an empty embed", func(t *testing.T) {
		s := newMachinesTestServer(t)
		if _, err := s.resolveOcwardenBinaryFrom(embedded); err == nil {
			t.Fatal("no binCacheDir configured must refuse the embed fallback")
		}
		s.binCacheDir = t.TempDir()
		if _, err := s.resolveOcwardenBinaryFrom(fstest.MapFS{}); err == nil {
			t.Fatal("disk miss + embed miss must err")
		}
	})
}

func TestServeBinary(t *testing.T) {
	get := func(t *testing.T, embedded fs.FS) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/warden/binary", nil)
		serveBinary(rec, req, "ocwarden", embedded)
		return rec
	}

	t.Run("serves the embed bytes", func(t *testing.T) {
		rec := get(t, fstest.MapFS{"ocwarden": {Data: []byte("embed bytes")}})
		if rec.Code != http.StatusOK || rec.Body.String() != "embed bytes" {
			t.Fatalf("want the embed bytes: %d %q", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "ocwarden") {
			t.Fatalf("attachment filename missing: %q", got)
		}
	})

	// embed-only, no disk override: even with a (stale) bin/ocwarden sitting in
	// the process CWD, the handler must deliver the embedded bytes — the disk
	// copy must never shadow the version-locked embed. This is the regression
	// guard for T-4c12 (prod was serving a stale on-disk ocagent).
	t.Run("disk copy in CWD never shadows the embed", func(t *testing.T) {
		dir := t.TempDir()
		// Lay down a fake stale bin/ocwarden under a temp CWD.
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(binDir, "ocwarden"), []byte("STALE disk bytes"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		rec := get(t, fstest.MapFS{"ocwarden": {Data: []byte("fresh embed bytes")}})
		if rec.Code != http.StatusOK || rec.Body.String() != "fresh embed bytes" {
			t.Fatalf("embed must win over the stale disk copy: %d %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("503s when the embed is missing", func(t *testing.T) {
		rec := get(t, fstest.MapFS{})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("want 503: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// TestBuildInstallScript pins the install.sh bytes: the EMPTY namespace must
// reproduce the canonical script byte-for-byte (the zero-diff proof for the
// server's only namespace-bearing output), and a namespaced server prefixes
// ONLY the install line with OC_NAMESPACE.
func TestBuildInstallScript(t *testing.T) {
	const base, token = "http://127.0.0.1:8770", "tok-abc.def.ghi"

	t.Run("empty namespace is byte-identical to the canonical script", func(t *testing.T) {
		want := `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL '` + base + `/install.sh?token=<jwt>' | bash
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
curl -fsSL "` + base + `/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
OC_BASE="` + base + `" OC_TOKEN="` + token + `" ./ocwarden install --force
`
		if got := buildInstallScript(base, token, ""); got != want {
			t.Fatalf("empty-namespace install.sh diverged from the historical bytes:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("namespace prefixes only the install line", func(t *testing.T) {
		plain := buildInstallScript(base, token, "")
		got := buildInstallScript(base, token, "seth")
		wantLine := `OC_NAMESPACE="seth" OC_BASE="` + base + `" OC_TOKEN="` + token + `" ./ocwarden install --force`
		if !strings.Contains(got, wantLine) {
			t.Fatalf("namespaced install line missing %q:\n%s", wantLine, got)
		}
		// The prefix is the ONLY difference.
		if strings.Replace(got, `OC_NAMESPACE="seth" `, "", 1) != plain {
			t.Fatalf("namespace must change nothing but the install-line prefix:\n%s", got)
		}
	})
}

// TestBuildInstallScriptWithCode pins the claim-code installer variant: the
// claim exchange runs BEFORE any download, sed joins the precheck, and the
// namespace prefixes only the install line (the token variant's contract).
func TestBuildInstallScriptWithCode(t *testing.T) {
	const base, code = "http://127.0.0.1:8770", "one-time-code-abc"

	t.Run("probes the binary, then claims the token, then downloads", func(t *testing.T) {
		got := buildInstallScriptWithCode(base, code, "")
		if !strings.Contains(got, "for tool in tmux curl sed; do") {
			t.Fatalf("sed missing from the tool precheck:\n%s", got)
		}
		// A 503-ing binary route must fail BEFORE the one-time code is burnt:
		// HEAD probe → claim → download, strictly in that order.
		probe := strings.Index(got, `curl -fsI "`+base+`/api/warden/binary"`)
		claim := strings.Index(got, base+"/api/machines/claim")
		download := strings.Index(got, `curl -fsSL "`+base+`/api/warden/binary"`)
		if probe == -1 || claim == -1 || download == -1 || probe > claim || claim > download {
			t.Fatalf("want probe < claim < download (probe=%d claim=%d download=%d):\n%s",
				probe, claim, download, got)
		}
		if !strings.Contains(got, "the install code was NOT consumed") {
			t.Fatalf("probe failure message must say the code survives:\n%s", got)
		}
		if !strings.Contains(got, `'{"code":"`+code+`"}'`) {
			t.Fatalf("claim code not templated into the claim body:\n%s", got)
		}
		for _, line := range []string{
			"Error: this install link has expired or was already used.",
			"Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner.",
			`echo "tmux not found — installing via Homebrew..."`,
			"brew install tmux || true",
			`echo "  macOS:  brew install $tool" >&2`,
			`echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2`,
		} {
			if !strings.Contains(got, line) {
				t.Fatalf("plain-language failure line missing %q:\n%s", line, got)
			}
		}
		if !strings.Contains(got, `OC_BASE="`+base+`" OC_TOKEN="$OC_TOKEN" ./ocwarden install --force`) {
			t.Fatalf("install line must ride the claimed $OC_TOKEN:\n%s", got)
		}
	})

	t.Run("namespace prefixes only the install line", func(t *testing.T) {
		plain := buildInstallScriptWithCode(base, code, "")
		got := buildInstallScriptWithCode(base, code, "seth")
		wantLine := `OC_NAMESPACE="seth" OC_BASE="` + base + `" OC_TOKEN="$OC_TOKEN" ./ocwarden install --force`
		if !strings.Contains(got, wantLine) {
			t.Fatalf("namespaced install line missing %q:\n%s", wantLine, got)
		}
		if strings.Replace(got, `OC_NAMESPACE="seth" `, "", 1) != plain {
			t.Fatalf("namespace must change nothing but the install-line prefix:\n%s", got)
		}
	})
}

func TestHandleClaimMachineTokenApiMachinesClaimPost(t *testing.T) {
	onboard := func(t *testing.T, s *apiServer) machineOnboardResultDTO {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/machines",
			strings.NewReader(`{"display_name":"claim-test-box"}`))
		s.HandleOnboardMachineApiMachinesPost(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("onboard: %d %s", rec.Code, rec.Body.String())
		}
		var dto machineOnboardResultDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return dto
	}
	claim := func(t *testing.T, s *apiServer, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/machines/claim", strings.NewReader(body))
		s.HandleClaimMachineTokenApiMachinesClaimPost(rec, req)
		return rec
	}

	t.Run("redeems a live code once: 200 with a working token, then 401", func(t *testing.T) {
		s := newMachinesTestServer(t)
		ob := onboard(t, s)
		if ob.ClaimCode == "" || ob.ClaimExpiresIn != machineClaimTTLSecs {
			t.Fatalf("onboard must mint a claim code: %+v", ob)
		}
		if !strings.Contains(ob.BootCommand, "/install.sh?code="+ob.ClaimCode) {
			t.Fatalf("onboard boot command must carry the claim code: %q", ob.BootCommand)
		}
		if strings.Contains(ob.BootCommand, ob.Token) {
			t.Fatalf("onboard boot command must never embed the exec-token: %q", ob.BootCommand)
		}

		rec := claim(t, s, `{"code":"`+ob.ClaimCode+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
		}
		var dto machineClaimResultDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if dto.MachineID != ob.MachineID || dto.ExpiresIn != machineTTL(defaultMachineTTLDays) {
			t.Fatalf("claim result drifted from the onboard mint: %+v", dto)
		}
		claims, err := verifyJWT(dto.Token, s.secret, time.Now().Unix())
		if err != nil {
			t.Fatalf("claimed token must verify: %v", err)
		}
		if claims["sub"] != ob.MachineID || claims["scope"] != "agent" {
			t.Fatalf("claimed token claims drifted: %v", claims)
		}

		// Single-use: the same code is spent.
		if rec := claim(t, s, `{"code":"`+ob.ClaimCode+`"}`); rec.Code != http.StatusUnauthorized {
			t.Fatalf("second redemption must 401: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown, expired, and missing codes are flat denials", func(t *testing.T) {
		s := newMachinesTestServer(t)
		if rec := claim(t, s, `{"code":"never-minted"}`); rec.Code != http.StatusUnauthorized {
			t.Fatalf("unknown code must 401: %d %s", rec.Code, rec.Body.String())
		}
		ob := onboard(t, s)
		s.machineClaims.mu.Lock()
		entry := s.machineClaims.codes[ob.ClaimCode]
		entry.expiresAt = time.Now().Add(-time.Second)
		s.machineClaims.codes[ob.ClaimCode] = entry
		s.machineClaims.mu.Unlock()
		if rec := claim(t, s, `{"code":"`+ob.ClaimCode+`"}`); rec.Code != http.StatusUnauthorized {
			t.Fatalf("expired code must 401: %d %s", rec.Code, rec.Body.String())
		}
		if rec := claim(t, s, `{}`); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("missing code field must 422: %d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMachineBinStatus(t *testing.T) {
	newServerWithHashes := func(t *testing.T) *apiServer {
		s := newMachinesTestServer(t)
		// Pin the embed hashes: the go:embed bindist content depends on what a
		// CI stage left behind, so the verdict logic is tested against fixed
		// fixtures, never the checkout state.
		s.binHashes = map[string]string{"ocwarden": "aaa111", "ocagent": "bbb222"}
		return s
	}
	report := func(s *apiServer, id string, bins map[string]any) {
		s.telemetry.Set(id, map[string]any{"binaries": bins})
	}
	verdict := func(v *string) string {
		if v == nil {
			return "<nil>"
		}
		return *v
	}

	t.Run("no heartbeat fingerprints yet reads unknown", func(t *testing.T) {
		s := newServerWithHashes(t)
		if got := s.machineBinStatus("m-box"); got != nil {
			t.Fatalf("verdict = %s, want nil (no telemetry entry)", verdict(got))
		}
		report(s, "m-box", nil) // entry exists, binaries absent (old warden build)
		if got := s.machineBinStatus("m-box"); got != nil {
			t.Fatalf("verdict = %s, want nil (no fingerprints reported)", verdict(got))
		}
	})

	t.Run("all fingerprints matching reads current", func(t *testing.T) {
		s := newServerWithHashes(t)
		report(s, "m-box", map[string]any{"ocwarden": "aaa111", "ocagent": "bbb222"})
		if got := s.machineBinStatus("m-box"); got == nil || *got != binStatusCurrent {
			t.Fatalf("verdict = %s, want current", verdict(got))
		}
	})

	t.Run("any mismatching fingerprint reads stale", func(t *testing.T) {
		s := newServerWithHashes(t)
		report(s, "m-box", map[string]any{"ocwarden": "aaa111", "ocagent": "OLD999"})
		if got := s.machineBinStatus("m-box"); got == nil || *got != binStatusStale {
			t.Fatalf("verdict = %s, want stale", verdict(got))
		}
	})

	t.Run("a partial match with no mismatch reads unknown, never current", func(t *testing.T) {
		s := newServerWithHashes(t)
		report(s, "m-box", map[string]any{"ocwarden": "aaa111"})
		if got := s.machineBinStatus("m-box"); got != nil {
			t.Fatalf("verdict = %s, want nil (ocagent unproven)", verdict(got))
		}
	})

	t.Run("no embedded bindist to compare against reads unknown", func(t *testing.T) {
		s := newMachinesTestServer(t)
		s.binHashes = map[string]string{}
		report(s, "m-box", map[string]any{"ocwarden": "aaa111", "ocagent": "bbb222"})
		if got := s.machineBinStatus("m-box"); got != nil {
			t.Fatalf("verdict = %s, want nil (nothing embedded)", verdict(got))
		}
	})

	t.Run("the machine list row carries the verdict", func(t *testing.T) {
		s := newServerWithHashes(t)
		putTestMember(t, s, Member{
			ID: "m-box", Name: "box", Kind: KindWarden, Effort: "medium",
			DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
		})
		report(s, "m-box", map[string]any{"ocwarden": "aaa111", "ocagent": "OLD999"})
		rec := httptest.NewRecorder()
		s.HandleListMachinesApiMachinesGet(rec, httptest.NewRequest("GET", "/api/machines", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("list machines: %d %s", rec.Code, rec.Body.String())
		}
		var rows []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		byID := map[string]map[string]any{}
		for _, row := range rows {
			byID[row["machine_id"].(string)] = row
		}
		if got := byID["m-box"]["bin_status"]; got != binStatusStale {
			t.Fatalf("m-box bin_status = %v, want stale", got)
		}
		// The seeded server-self warden has no telemetry → honest null.
		if got := byID[ServerSelfHost]["bin_status"]; got != nil {
			t.Fatalf("server-self bin_status = %v, want null", got)
		}
	})
}

func TestMachineClaudeInfo(t *testing.T) {
	report := func(s *apiServer, id string, probe map[string]any) {
		s.telemetry.Set(id, map[string]any{"claude": probe})
	}
	str := func(v *string) string {
		if v == nil {
			return "<nil>"
		}
		return *v
	}

	t.Run("no telemetry entry reads all-nil", func(t *testing.T) {
		s := newMachinesTestServer(t)
		version, credSource, subReadable := s.machineClaudeInfo("m-box")
		if version != nil || credSource != nil || subReadable != nil {
			t.Fatalf("got %s/%s/%v, want all nil (no entry)", str(version), str(credSource), subReadable)
		}
	})

	t.Run("entry without a claude probe reads all-nil (old warden)", func(t *testing.T) {
		s := newMachinesTestServer(t)
		s.telemetry.Set("m-box", map[string]any{"hardware": map[string]any{"cpu_pct": 1.0}})
		version, credSource, subReadable := s.machineClaudeInfo("m-box")
		if version != nil || credSource != nil || subReadable != nil {
			t.Fatalf("got %s/%s/%v, want all nil (no probe)", str(version), str(credSource), subReadable)
		}
	})

	t.Run("cred_source synthesizes the four-value matrix", func(t *testing.T) {
		s := newMachinesTestServer(t)
		for _, tc := range []struct {
			credFile, keychain bool
			want               string
		}{
			{true, true, claudeCredSourceBoth},
			{true, false, claudeCredSourceFile},
			{false, true, claudeCredSourceKeychain},
			{false, false, claudeCredSourceNone},
		} {
			report(s, "m-box", map[string]any{"cred_file": tc.credFile, "keychain": tc.keychain})
			_, credSource, _ := s.machineClaudeInfo("m-box")
			if credSource == nil || *credSource != tc.want {
				t.Errorf("cred_file=%v keychain=%v: cred_source = %s, want %s",
					tc.credFile, tc.keychain, str(credSource), tc.want)
			}
		}
	})

	t.Run("partial probe synthesizes what it can", func(t *testing.T) {
		s := newMachinesTestServer(t)
		// keychain unreported (non-darwin) + cred_file true → a true still
		// identifies its source.
		report(s, "m-box", map[string]any{"version": "2.1.211", "cred_file": true})
		version, credSource, subReadable := s.machineClaudeInfo("m-box")
		if version == nil || *version != "2.1.211" {
			t.Fatalf("version = %s, want 2.1.211", str(version))
		}
		if credSource == nil || *credSource != claudeCredSourceFile {
			t.Fatalf("cred_source = %s, want file", str(credSource))
		}
		if subReadable != nil {
			t.Fatalf("sub_readable = %v, want nil (unreported)", *subReadable)
		}
		// A lone false proves nothing (the other source is unknown) → nil.
		report(s, "m-box", map[string]any{"cred_file": false})
		if _, credSource, _ := s.machineClaudeInfo("m-box"); credSource != nil {
			t.Fatalf("lone-false cred_source = %s, want nil", str(credSource))
		}
		// Both bools missing → nil; empty version string → nil.
		report(s, "m-box", map[string]any{"version": "", "sub_readable": false})
		version, credSource, subReadable = s.machineClaudeInfo("m-box")
		if version != nil || credSource != nil {
			t.Fatalf("got version=%s cred_source=%s, want nil/nil", str(version), str(credSource))
		}
		if subReadable == nil || *subReadable != false {
			t.Fatalf("sub_readable = %v, want false", subReadable)
		}
	})

	t.Run("the machine list row carries the claude columns", func(t *testing.T) {
		s := newMachinesTestServer(t)
		putTestMember(t, s, Member{
			ID: "m-box", Name: "box", Kind: KindWarden, Effort: "medium",
			DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
		})
		report(s, "m-box", map[string]any{
			"version": "2.1.211", "cred_file": false, "sub_readable": false, "keychain": true})
		rec := httptest.NewRecorder()
		s.HandleListMachinesApiMachinesGet(rec, httptest.NewRequest("GET", "/api/machines", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("list machines: %d %s", rec.Code, rec.Body.String())
		}
		var rows []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		byID := map[string]map[string]any{}
		for _, row := range rows {
			byID[row["machine_id"].(string)] = row
		}
		box := byID["m-box"]
		if box["claude_version"] != "2.1.211" || box["claude_cred_source"] != claudeCredSourceKeychain ||
			box["claude_sub_readable"] != false {
			t.Fatalf("m-box claude columns = %v/%v/%v, want 2.1.211/keychain/false",
				box["claude_version"], box["claude_cred_source"], box["claude_sub_readable"])
		}
		// The seeded server-self warden has no telemetry → honest nulls.
		self := byID[ServerSelfHost]
		if self["claude_version"] != nil || self["claude_cred_source"] != nil ||
			self["claude_sub_readable"] != nil {
			t.Fatalf("server-self claude columns = %v/%v/%v, want nulls",
				self["claude_version"], self["claude_cred_source"], self["claude_sub_readable"])
		}
	})
}

func TestHandleUpgradeMachine(t *testing.T) {
	putBox := func(t *testing.T, s *apiServer) {
		t.Helper()
		putTestMember(t, s, Member{
			ID: "m-box", Name: "box", Kind: KindWarden, Effort: "medium",
			DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
		})
	}
	upgrade := func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/machines/"+id+"/upgrade", nil)
		s.HandleUpgradeMachineApiMachinesMemberIdUpgradePost(rec, req, id)
		return rec
	}
	decode := func(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v (%s)", err, rec.Body.String())
		}
		return out
	}

	t.Run("online warden gets exactly one update frame", func(t *testing.T) {
		s := newMachinesTestServer(t)
		putBox(t, s)
		l, err := s.hub.Connect("m-box", "m-box") // the warden's own live SSE
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer s.hub.Disconnect(l)

		rec := upgrade(t, s, "m-box")
		if rec.Code != http.StatusOK {
			t.Fatalf("upgrade: %d %s", rec.Code, rec.Body.String())
		}
		out := decode(t, rec)
		if out["dispatched"] != true || out["member_id"] != "m-box" || out["machine_id"] != "m-box" {
			t.Fatalf("result = %v, want dispatched m-box", out)
		}
		frames := s.hub.DrainWardenCommands("m-box")
		if len(frames) != 1 {
			t.Fatalf("warden FIFO = %d frames, want 1", len(frames))
		}
		text := string(frames[0])
		if !strings.HasPrefix(text, "data: ") {
			t.Fatalf("frame is not a bare data: event: %q", text)
		}
		var envelope struct {
			Topic string `json:"topic"`
			Data  struct {
				RPC  string         `json:"rpc"`
				Args map[string]any `json:"args"`
			} `json:"data"`
		}
		payload := strings.TrimSuffix(strings.TrimPrefix(text, "data: "), "\n\n")
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			t.Fatalf("decode frame: %v (%q)", err, payload)
		}
		if envelope.Topic != wardenCommandTopic || envelope.Data.RPC != reconcileCmdUpdate ||
			envelope.Data.Args["member_id"] != "m-box" {
			t.Fatalf("frame = %+v, want warden-command update for m-box", envelope)
		}
		// Fire-and-forget: no desired-state intent was written.
		if got := desiredStateOf(t, s, "m-box"); got != DesiredStateOffline {
			t.Fatalf("desired_state = %q, want untouched offline", got)
		}
	})

	t.Run("offline warden dispatches nothing", func(t *testing.T) {
		s := newMachinesTestServer(t)
		putBox(t, s)
		rec := upgrade(t, s, "m-box")
		if rec.Code != http.StatusOK {
			t.Fatalf("upgrade: %d %s", rec.Code, rec.Body.String())
		}
		if out := decode(t, rec); out["dispatched"] != false {
			t.Fatalf("result = %v, want dispatched=false", out)
		}
		if frames := s.hub.DrainWardenCommands("m-box"); len(frames) != 0 {
			t.Fatalf("offline warden FIFO = %d frames, want 0", len(frames))
		}
	})

	t.Run("unknown or non-warden member is 404", func(t *testing.T) {
		s := newMachinesTestServer(t)
		if rec := upgrade(t, s, "m-ghost"); rec.Code != http.StatusNotFound {
			t.Fatalf("unknown machine: %d, want 404", rec.Code)
		}
		putTestMember(t, s, testAgent("m-a"))
		if rec := upgrade(t, s, "m-a"); rec.Code != http.StatusNotFound {
			t.Fatalf("non-warden member: %d, want 404", rec.Code)
		}
	})
}

func TestHandleDeleteMachine(t *testing.T) {
	putBox := func(t *testing.T, s *apiServer) {
		t.Helper()
		putTestMember(t, s, Member{
			ID: "m-box", Name: "box", Kind: KindWarden, Effort: "medium",
			DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
		})
	}
	deleteBox := func(t *testing.T, s *apiServer) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/api/machines/m-box", nil)
		s.HandleDeleteMachineApiMachinesMemberIdDelete(rec, req, "m-box")
		return rec
	}

	t.Run("blocks while an agent is ACTUALLY online on the machine", func(t *testing.T) {
		s := newMachinesTestServer(t)
		putBox(t, s)
		putTestMember(t, s, testAgent("m-a"))
		l, err := s.hub.Connect("m-a", "m-box") // live SSE machine claim
		if err != nil {
			t.Fatalf("connect: %v", err)
		}

		if rec := deleteBox(t, s); rec.Code != http.StatusConflict {
			t.Fatalf("online agent on the machine must 409: %d %s", rec.Code, rec.Body.String())
		}
		s.hub.Disconnect(l)
		if rec := deleteBox(t, s); rec.Code != http.StatusOK {
			t.Fatalf("all agents offline must delete directly: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("an offline agent merely BOUND to the machine never blocks", func(t *testing.T) {
		s := newMachinesTestServer(t)
		putBox(t, s)
		bound := testAgent("m-a")
		bound.DesiredMachineID = "m-box" // desired binding, no live session
		putTestMember(t, s, bound)

		if rec := deleteBox(t, s); rec.Code != http.StatusOK {
			t.Fatalf("desired-bound offline agent must not block: %d %s", rec.Code, rec.Body.String())
		}
		m, _ := s.dal.GetMember("m-box")
		if m.RosterStatus != RosterStatusRemoved {
			t.Fatalf("delete must soft-remove the record: %+v", m)
		}
	})
}
