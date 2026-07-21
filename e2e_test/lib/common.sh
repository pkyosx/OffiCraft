#!/usr/bin/env bash
# e2e_test/lib/common.sh — shared config + guards for the isolated e2e harness.
# Sourced by setup.sh / teardown.sh. This harness runs an ISOLATED officraft
# service on a NON-PROD port with an isolated SQLite DB. It must NEVER touch
# the officraft production port (the CURRENT default per
# server/ocserverd/config.go, not a number hand-copied here — see PROD_PORTS
# below) or the "vibe" product's :8766, and must never authenticate against or
# emit to the fleet/prod server.
#
# T-d41a — deliberately `set -uo pipefail`, NOT `-euo`. This file is SOURCED, so
# `set -e` here silently rewrites the ERR-handling policy of whoever sourced it.
# Two callers deliberately run WITHOUT `-e` so they can capture a failure's rc
# and report it: run_all.sh (`RC=$?; echo "[run_all] specs exit=$RC"`) and
# teardown.sh (best-effort, must survive no-op steps). Under an inherited `-e`
# those lines are unreachable — a red run lost its diagnostic line while the
# exit code stayed identical, so the hole looked exactly like a healthy net.
# Every caller that WANTS `-e` (setup.sh, single_machine_e2e.sh, a1_zombie_e2e.sh,
# task_system_e2e.sh) already sets it itself BEFORE sourcing, and `set -uo` does
# not clear it — so errexit stays a per-entrypoint decision. Do not add `-e` here.
# Guarded by e2e_test/tests_guard/run.sh case (11).
set -uo pipefail

# repo root = two levels up from this file (e2e_test/lib/common.sh)
E2E_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$E2E_ROOT/.." && pwd)"
STATE_DIR="$E2E_ROOT/.state"

# Target implementation: go (ocserverd built fresh from server/ocserverd with
# the SPA embedded) is the ONLY target — the py leg retired with the Python
# backend (rollback anchor: git tag py-final). The knob stays so an explicit
# OC_E2E_TARGET=go keeps working and a stale =py invocation fails loud.
OC_E2E_TARGET="${OC_E2E_TARGET:-go}"
if [ "$OC_E2E_TARGET" != "go" ]; then
  echo "FATAL: OC_E2E_TARGET=$OC_E2E_TARGET — go is the only target (py retired; git tag py-final)." >&2
  exit 2
fi

# Isolated, non-prod port (overridable, but a prod port is hard-refused below).
OC_E2E_PORT="${OC_E2E_PORT:-8791}"
OC_E2E_HOST="127.0.0.1"
OC_E2E_BASE="http://${OC_E2E_HOST}:${OC_E2E_PORT}"

# Hard guard: refuse to run against a known prod port.
#
# PROD_OFFICRAFT_PORT — the CURRENT officraft prod default, read from the
# single source of truth (server/ocserverd/config.go's `defaultPort` const)
# instead of a hand-maintained number that silently goes stale (T-a3ba
# follow-up: this list used to say "8770/8766" as if those WERE the current
# prod port; 8770 is actually a RETIRED former officraft default — config.go's
# own migration-history comment: 8770 → 8780 → 7755, the real current one,
# which this list never named — so the guard's NAME promised more than it
# enforced. It went unnoticed only because the separate "leftover guard"
# elsewhere in the harness happens to cover a live prod on 7755 too — that is
# cover from a DIFFERENT guard, not this one actually working; do not read
# "nothing broke" as "the enumeration was fine"). A failed parse here is a
# HARD FAIL, not a silently-skipped guard.
PROD_OFFICRAFT_PORT="$(grep -E '^[[:space:]]*defaultPort[[:space:]]*=[[:space:]]*[0-9]+' \
  "$REPO_ROOT/server/ocserverd/config.go" 2>/dev/null | grep -oE '[0-9]+' | head -1)"
if [ -z "$PROD_OFFICRAFT_PORT" ]; then
  echo "FATAL: could not parse server/ocserverd/config.go's defaultPort — refusing to run without a working prod-port guard (T-a3ba)." >&2
  exit 2
fi
# Additional refusals, NOT derived from config.go (nothing in this repo can
# derive them, so they stay a hand-maintained list and CAN drift again —
# named honestly as such, unlike the guard's old self-description):
#   - 8770, 8780: officraft's own RETIRED former defaults (config.go history)
#     — kept for any install that still has one explicitly pinned in oc.toml.
#   - 8766: a DIFFERENT product's live port ("vibe-clicking", see
#     conformance/CLAUDE.md) — not derivable from this repo at all.
PROD_PORTS=("$PROD_OFFICRAFT_PORT" 8770 8780 8766)
for _p in "${PROD_PORTS[@]}"; do
  if [ "$OC_E2E_PORT" = "$_p" ]; then
    echo "FATAL: OC_E2E_PORT=$OC_E2E_PORT is a PROD port (current officraft default=$PROD_OFFICRAFT_PORT per server/ocserverd/config.go, or a retired officraft default / a different live product's port) — refuse." >&2
    exit 2
  fi
done

# Strip ambient fleet env (OC_ID / OC_TOKEN / OC_BASE) so the isolated serve and
# any tool we spawn never talk to the fleet/prod server. Critical: without this,
# ambient OC_* silently redirects auth/telemetry at the real server.
# OC_RELEASE_API_BASE (t-dc68): pin the GitHub Releases update check at an
# unroutable loopback — the harness must never reach the real api.github.com
# (hermeticity + the anonymous rate limit); checks fail fast and honestly.
oc_env() { env -u OC_ID -u OC_TOKEN -u OC_BASE OC_RELEASE_API_BASE="http://127.0.0.1:1" "$@"; }

# python3 as a text tool only (tomllib/json parsing) — not a server dependency.
py() { python3 "$@"; }

# Resolve a dev tool (npm/npx/…) to a real executable path, portably. nvm/volta
# install these as lazy-load SHELL FUNCTIONS that shadow the real binary, which
# is why callers historically bypassed PATH with a hardcoded /opt/homebrew/bin
# abspath — but that hardcode breaks on Intel brew (/usr/local), asdf/volta, or a
# ~/.local/bin layout (the eva-m5 case: `which npm` = ~/.local/bin/npm, not in any
# fixed list). Drop the shadowing function in THIS command-substitution subshell,
# prefer PATH resolution, then fall back to the common install locations. Mirrors
# bin/ci.sh's npm probe (command -v first, abspath fallback). Echoes the abspath;
# returns 1 (no output) if the tool cannot be found anywhere.
oc_resolve_bin() {
  local name="$1" p cand
  unset -f "$name" 2>/dev/null || true
  p="$(command -v "$name" 2>/dev/null || true)"
  if [ -n "$p" ] && [ -x "$p" ]; then printf '%s\n' "$p"; return 0; fi
  for cand in "$HOME/.local/bin/$name" "$HOME/.asdf/shims/$name" \
              "/opt/homebrew/bin/$name" "/usr/local/bin/$name"; do
    [ -x "$cand" ] && { printf '%s\n' "$cand"; return 0; }
  done
  return 1
}

# Restore server/ocserverd/webdist to pristine (only .gitkeep survives). The go
# leg stages the built SPA here for go:embed; a stray file that survives cleanup
# gets baked into the COMMITTED bin/ocserverd by a later `go build` (server/
# CLAUDE.md) — so a SILENT delete failure is a real hazard, not cosmetic. This
# historically ran `find … -delete 2>/dev/null` with no rc check, so a half-failed
# delete retired silently (T-c5d4 weakness-2). Now: let find's stderr through,
# check its rc, AND independently re-assert nothing but .gitkeep remains (rc alone
# can miss a partial delete — a fail-closed existence assertion, not a sentinel).
# Best-effort — never aborts the caller; prints a loud WARN to stderr and returns
# 1 on trouble, else the normal status line + 0.
oc_restore_webdist_pristine() {
  local webdist="$1" find_rc leftover
  [ -d "$webdist" ] || { echo "[teardown] webdist absent ($webdist) — nothing to restore"; return 0; }
  find "$webdist" -mindepth 1 -not -name '.gitkeep' -delete
  find_rc=$?
  leftover=$(find "$webdist" -mindepth 1 -not -name '.gitkeep' | wc -l | tr -d ' ')
  if [ "$find_rc" -ne 0 ] || [ "$leftover" -ne 0 ]; then
    echo "[teardown] WARN: webdist NOT fully restored to pristine — find rc=$find_rc, $leftover stray entries left under $webdist. A later 'go build' could embed this stray SPA into the committed bin/ocserverd; inspect + clean manually." >&2
    return 1
  fi
  echo "[teardown] restored server/ocserverd/webdist to pristine (.gitkeep only)"
  return 0
}
