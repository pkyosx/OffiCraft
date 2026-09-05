#!/usr/bin/env bash
# e2e_test/run_all.sh — one-shot: setup -> playwright specs -> teardown.
# teardown ALWAYS runs (EXIT trap), even if a spec fails or setup aborts.
#
# WHO EXERCISES A CHANGE TO THIS FILE — and how far that reaches
# `bin/ci.sh` does NOT run the playwright specs. What it DOES run, via step (0)
# `e2e_test/tests_guard/run.sh`, is this script's WIRING SHAPE: tests_guard
# copies this file into a throwaway tree and executes it against stubs (the
# record-only teardown seam), and separately pins the shape statically: the rc
# capture must sit immediately after the playwright invocation and the
# spec-exit report line immediately after that, and the EXIT trap must reach
# teardown through `oc_e2e_teardown_on_exit` rather than calling teardown.sh
# directly. Measured: inserting one line before the rc capture takes tests_guard
# to FAIL=2 rc=1; pointing the trap straight at teardown.sh takes it to
# FAIL=4 rc=1.
#
# Prose in this file is free to quote the statements that case (11) lifts (the
# `set -` line, the source line, the rc capture, the report echo): that case
# anchors each pattern at column 0 and to the statement's shape, so a comment
# cannot win the match. It did not always: with an unanchored `grep -m1 -F`, a
# comment here mentioning the report line was picked up instead of the echo, the
# reconstructed fixture printed nothing, and case (11) failed pointing at
# lib/common.sh's `set -e` — measured PASS=152 FAIL=1 rc=1, for a comment.
#
# So a local `[ci] all green` DOES cover those wiring properties — but it says
# nothing about any spec having run, because none did. Spec-level acceptance for
# a change here is the `macos-e2e` job on the PR and its log.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# T-45/B: member e2e uses the Playwright path, not cmux browser discovery.  The
# browser tool lives outside this repository, so this explicit selector is the
# boundary we can enforce here: a caller that tries to route this harness
# through cmux gets a named refusal instead of a misleading setup failure.
case "${OC_E2E_BROWSER_BACKEND:-playwright}" in
  playwright)
    echo "[run_all] member e2e browser backend=Playwright (cmux browser is unsupported; use this harness)"
    ;;
  cmux)
    echo "[run_all] FATAL: OffiCraft members do not use cmux browser for e2e; run 'bash e2e_test/run_all.sh' or setup.sh -> Playwright -> teardown.sh instead. NOT a server failure." >&2
    exit 2
    ;;
  *)
    echo "[run_all] FATAL: unsupported member e2e browser backend '${OC_E2E_BROWSER_BACKEND}' (supported: Playwright; cmux browser is not a member e2e path)." >&2
    exit 2
    ;;
esac

# `||` does NOT swallow common.sh's own hard guards: an `exit 2` inside a sourced
# file exits this script with 2 regardless (verified) — this only catches the
# file being missing/unreadable, which would otherwise surface as a confusing
# unbound-variable error on $STATE_DIR further down.
source "$HERE/lib/common.sh" || { echo "[run_all] FATAL: cannot source $HERE/lib/common.sh" >&2; exit 1; }

# T-ff8a. A stale arm file from an EARLIER run is not consent from this one —
# disarm before anything else, so what the trap below sees was written by THIS
# run's setup.
oc_e2e_disarm_teardown

# The trap is registered UNCONDITIONALLY and before setup, on purpose: setup can
# fail half-way through creating things, and that is precisely when a teardown is
# needed. What is conditional is what the trap DOES — oc_e2e_teardown_on_exit
# (lib/common.sh) runs teardown.sh only if setup ARMED it, i.e. only if this run
# actually created something. Before T-ff8a this line ran teardown unconditionally,
# so setup.sh's three prod guards — which `exit 2` BEFORE creating anything —
# each ended in `rm -rf "$REPO_ROOT/var/data"` on the very DB they had just
# refused to touch. Guarded by e2e_test/tests_guard/run.sh case (20).
cleanup() { oc_e2e_teardown_on_exit "$HERE"; }
trap cleanup EXIT

echo "[run_all] === SETUP ==="
if ! bash "$HERE/setup.sh"; then
  echo "[run_all] setup failed — aborting (teardown will still run)." >&2
  exit 1
fi

# Export persisted state (OC_E2E_BASE / OC_E2E_TOKEN) + password for the specs
# (setup.sh seeded a per-run random password into the DB and persisted it).
# Each prerequisite below aborts EXPLICITLY. This script runs without `-e` (it
# must survive a failing spec long enough to report the rc), so nothing aborts
# implicitly here — the abort has to be written out, and loudly. Until T-d41a
# these steps died silently on an `-e` leaked in from lib/common.sh.
set -a; source "$STATE_DIR/env" || { echo "[run_all] FATAL: cannot source $STATE_DIR/env" >&2; set +a; exit 1; }; set +a
OC_E2E_PASSWORD=$(cat "$STATE_DIR/owner.password") || { echo "[run_all] FATAL: cannot read $STATE_DIR/owner.password" >&2; exit 1; }
export OC_E2E_PASSWORD

echo "[run_all] === E2E (playwright) ==="
# Live-agent prerequisite, and ONLY built when a live agent was actually asked
# for (OC_E2E_LIVE_AGENT=1 — the same predicate playwright.config.js uses, so the
# two can never disagree about whether that class is running).
#
# What it does when it runs: build BOTH cli binaries IN-TREE so the warden's spawn
# shim can resolve ocagent. resolveOcAgentBin walks three parents up from the
# ocwarden executable to find <repoRoot>/cli/ocagent/ocagent — the spec's default
# ocwarden path (REPO_ROOT/../ocwarden) walks to /Users, where nothing exists.
# T-81 CHANGED WHAT HAPPENS NEXT, so do not go looking for the old symptom: the
# spawn used to symlink that broken path into the agent's workdir and carry on,
# producing a DEAF agent that only came online if claude self-rescued in time (the
# presence-timeout flake). start() now REFUSES the spawn outright with
# ocagent_not_found. Same prerequisite, but the failure is immediate and says what
# is wrong instead of surfacing minutes later as a flaky timeout. In-tree builds
# restore the dev layout the resolver is written for. Both artifacts are gitignored.
#
# Why it is conditional (T-c329): it was unconditional, which cost every caller
# two builds for a class that, by default, does not run. It does NOT make the
# suite runnable without a Go toolchain — setup.sh (line 29, well before this)
# unconditionally needs one: build-bindist builds three CLIs and ocserverd is
# built from source. The saving here is the duplicate in-tree pair, nothing more.
# OC_E2E_OCWARDEN is consumed by that class alone (verified: the
# only readers in the whole repo are this line and the live-agent spec itself), so
# skipping it changes nothing for the specs that do run.
if [ "${OC_E2E_LIVE_AGENT:-}" = "1" ]; then
  echo "[run_all] OC_E2E_LIVE_AGENT=1 — building in-tree cli binaries (ocagent + ocwarden) for the live-agent class…"
  (cd "$REPO_ROOT/cli/ocagent" && go build -o ocagent .) || { echo "[run_all] FATAL: go build cli/ocagent failed — the live-agent class would flake on a stale/absent binary." >&2; exit 1; }
  (cd "$REPO_ROOT/cli/ocwarden" && go build -o ocwarden .) || { echo "[run_all] FATAL: go build cli/ocwarden failed." >&2; exit 1; }
  export OC_E2E_OCWARDEN="$REPO_ROOT/cli/ocwarden/ocwarden"
else
  echo "[run_all] live-agent class NOT requested (OC_E2E_LIVE_AGENT unset/≠1) — skipping its cli builds; no agent will be spawned and no API quota spent."
fi
cd "$HERE" || { echo "[run_all] FATAL: cannot cd to $HERE — playwright would run from the wrong dir and miss its config." >&2; exit 1; }
# nvm/volta lazy-load defines npm/npx as shell FUNCTIONS that shadow the real
# binary; oc_resolve_bin (lib/common.sh) drops the shadow, prefers PATH, then
# falls back to the common install locations — no more hardcoded homebrew abspath
# (which broke on Intel-brew / asdf / volta / ~/.local/bin layouts). Callers below
# still `unset -f node npm` in their subshells as belt-and-suspenders.
NPM="$(oc_resolve_bin npm)" || { echo "[run_all] FATAL: npm not found (checked PATH + ~/.local/bin, asdf, homebrew, /usr/local) — cannot install/run playwright. NOT a spec failure." >&2; exit 1; }
NPX="$(oc_resolve_bin npx)" || { echo "[run_all] FATAL: npx not found (checked PATH + common locations) — cannot run playwright. NOT a spec failure." >&2; exit 1; }
if [ ! -d "$HERE/node_modules/@playwright/test" ]; then
  echo "[run_all] installing @playwright/test (first run)…"
  ( unset -f node npm 2>/dev/null; "$NPM" install --no-audit --no-fund ) \
    || { echo "[run_all] FATAL: '$NPM install' failed — @playwright/test not installed. NOT a spec failure." >&2; exit 1; }
fi
# Browser render specs (B1/B6) need a real Chromium; install is idempotent (fast
# no-op once cached). API-only specs don't use it but this keeps run_all complete.
( unset -f node npm npx 2>/dev/null; "$NPX" playwright install chromium ) \
  || { echo "[run_all] FATAL: '$NPX playwright install chromium' failed — no browser for B1/B6. NOT a spec failure." >&2; exit 1; }
# The ONLY unguarded command in this script, on purpose: its rc is the payload.
( unset -f node npm npx 2>/dev/null; "$NPX" playwright test )
RC=$?
echo "[run_all] specs exit=$RC"
exit $RC
