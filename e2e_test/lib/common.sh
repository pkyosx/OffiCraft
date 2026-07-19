#!/usr/bin/env bash
# e2e_test/lib/common.sh — shared config + guards for the isolated e2e harness.
# Sourced by setup.sh / teardown.sh. This harness runs an ISOLATED officraft
# service on a NON-PROD port with an isolated SQLite DB. It must NEVER touch the
# production ports :8770 / :8766, and must never authenticate against or emit to
# the fleet/prod server.
set -euo pipefail

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
PROD_PORTS=(8770 8766)
for _p in "${PROD_PORTS[@]}"; do
  if [ "$OC_E2E_PORT" = "$_p" ]; then
    echo "FATAL: OC_E2E_PORT=$OC_E2E_PORT is a PROD port — refuse." >&2
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
