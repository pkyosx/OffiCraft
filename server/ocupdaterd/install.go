package main

// install.go — GET /install.sh?code=<invite>: the invited user's one-liner
// (style twin of ocserverd api_machines.go buildInstallScriptWithCode —
// precheck → download → verify → install). The invite code gates the script
// itself AND rides inside it as the download credential.
//
// After download + verify, the script PRE-FILLS the installed server's DB
// settings with this updater's base URL + the same invite code (`ocserverd
// set-updater`, the local shell-access seam — an installer has no owner token
// yet), so the very first `serve` already checks for updates and the settings
// page's software-update card lights up with zero manual setup. The invite
// code is per-user, long-lived, revocable, and doubles as the update-check
// credential (owner ruling) — the code that fetched this script is the code
// the server keeps using. The base URL is the face the CLIENT downloaded
// through (baseURL below: Host + X-Forwarded-Proto), which is exactly the
// face reachable from the machine the server is being installed on.

import (
	"net/http"
	"net/url"
	"strings"
)

// handleInstallScript serves the dynamic installer. The invite code arrives as
// ?code= (a curl|bash one-liner cannot set headers); invalid/revoked codes get
// the same 401 as the API routes. The script pins the LATEST version of the
// requested channel (?channel=ga|beta, default ga — invited users install the
// stable channel unless the one-liner says otherwise) at generation time —
// version + digest are baked in, so the script verifies exactly what the
// server promised.
func (s *updaterServer) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if s.authenticate(w, r, kindInvite, code) == nil {
		return
	}
	channel, ok := channelParam(w, r)
	if !ok {
		return
	}
	rel, err := s.store.LatestRelease(channel)
	if err != nil {
		internalError(w, err)
		return
	}
	if rel == nil {
		writeError(w, http.StatusNotFound, noLatestMessage(channel)+" — there is nothing to install yet")
		return
	}
	w.Header().Set("Content-Type", "text/x-sh; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(buildInstallScript(baseURL(r), code, *rel)))
}

// baseURL reconstructs the URL the CLIENT used, so the script it receives
// points back at the same face (loopback directly, or the tunnel hostname
// when proxied — the tunnel sets X-Forwarded-Proto).
func baseURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
	}
	return proto + "://" + r.Host
}

// buildInstallScript renders the self-contained installer for one pinned
// release. Single-quoted heredoc-free string assembly, same as the ocserverd
// installer templates; the invite code and digest are base64url/hex-shaped so
// no quoting can break.
func buildInstallScript(base, code string, rel Release) string {
	var b strings.Builder
	b.WriteString(`#!/usr/bin/env bash
# officraft — invited-user server installer (served by ocupdaterd GET /install.sh).
# Usage: curl -fsSL '` + base + `/install.sh?code=<invite>' | bash
# Pinned release: version ` + rel.Version + ` (sha256 ` + rel.SHA256 + `)
set -euo pipefail

# Precheck: only the KEY tools this install truly needs.
#   curl   — pulls the server binary just below.
#   shasum — verifies the download against the server-promised digest.
for tool in curl shasum; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Download the pinned release with the invite code as the bearer credential.
curl -fsSL "` + base + `/api/binary?version=` + url.QueryEscape(rel.Version) + `" \
  -H 'Authorization: Bearer ` + code + `' -o ocserverd

# Verify the bytes against the digest the server promised at script-generation
# time — a truncated or tampered download stops HERE, before anything runs.
echo "` + rel.SHA256 + `  ocserverd" | shasum -a 256 -c - >/dev/null || {
  echo "Error: the downloaded binary failed sha256 verification — download corrupt, nothing was installed. Re-run the one-liner." >&2
  rm -f ocserverd
  exit 1
}
chmod +x ocserverd

# Pre-fill this server's settings with the updater it was installed from
# (URL + your invite code, written straight into the server's DB settings) —
# update checks work from the very first start, and the same env/config the
# later 'serve' uses resolves the same database here. The invite code never
# rides argv (ps-visible); it rides the environment, same as set-password.
OC_UPDATER_URL='` + base + `' OC_UPDATER_INVITE_CODE='` + code + `' ./ocserverd set-updater

echo "ocserverd ` + rel.Version + ` downloaded, verified, and pre-configured into $(pwd)/ocserverd."
echo "Update checks are pre-filled against ` + base + ` — start it with: ./ocserverd serve"
`)
	return b.String()
}
