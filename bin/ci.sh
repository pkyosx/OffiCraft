#!/usr/bin/env bash
# officraft local CI — run the whole gate locally, in one command.
#
# 🔴 THIS SCRIPT IS NOT THE LAND AUTHORITY ANY MORE (owner, 2026-08-11, card
# rc-c16ac4679fab: he was asked whether a change becomes mergeable on the
# cloud round or on this script, and picked THE CLOUD ROUND). What decides
# whether something may be merged is the pull request's checks, not a green
# line printed on somebody's laptop. His words on how that plays out day to
# day, verbatim:
#
#   「但是自己開發的時候，當然要自己先確認通過自己新增或是跟自己有關的
#     測試，沒問題才開PR上去跑，確認全部都通過」
#
# So this script did not lose its job, it changed jobs: it is what you reach
# for WHILE DEVELOPING to check your own area before opening a PR — not a
# toll gate every change has to pay in full. The reason the verdict moved is
# that a green here is visible only to whoever ran it, and it expires the
# moment the base moves; the PR's checks are visible to everyone and can be
# re-run by anyone.
#
# (The even older reason this used to live here — "we do not pay for GitHub
# Actions" — was factually wrong once the repo went PUBLIC: standard-runner
# minutes are free for public repos.)
#
# ⚠️ WHAT CHANGED IN T-4d88, because it changes how you read this file:
# this script no longer CONTAINS any check. Every check is a named target in the
# repo-root Makefile, and its implementation lives in exactly one recipe there.
# What this file still owns is the three things that are about the RUN rather
# than about any check: the working-copy lock, the provenance stamp, and the
# end-of-run marker. Everything between them is a list of target names —
# a CHOICE of which checks to run and in what order, never a second copy of how.
#
# Before T-4d88 the same checks were written out here, again in bin/ci-cloud.sh
# and again in bin/ci-macos-host.sh. Those two files are gone: the cloud jobs in
# .github/workflows/ci.yml now call the Makefile targets directly, one cell per
# concern, in parallel. Three copies of one rule is how one copy silently loses a
# clause, and that was not hypothetical — the e2e isolation-guard suite ran with
# its truncation protection here and WITHOUT it in the cloud, which is the round
# that guarded everybody else's pull request.
#
# ORDER BELOW IS NOT ARBITRARY. Two constraints are load-bearing:
#   * build-embed-assets comes before anything that runs `go test` or builds
#     ocserverd. seeds/*.md, docs/guide, spec/mcp-catalog.json and the prebuilt
#     ocwarden/ocagent are served EMBED-ONLY, a clean checkout carries
#     .gitkeep-only staging dirs, and server/ocserverd's tests read the STAGED
#     embed by name. The Makefile also declares this as a real prerequisite, so
#     it holds for a caller that names only `test-go`.
#   * the working-copy lock is taken BEFORE any of it (see below).
# Everything else in the order is diagnostic preference, not dependency.
#
# ONE RUN PER WORKING COPY (T-70c9). This script LOCKS the clone it lives in; a
# second run in the SAME clone is refused with a non-zero exit. MORE ROUNDS AT
# ONCE MEANS MORE COPIES — clone again and run there. The lock is per copy, not
# per machine, so concurrent runs in SEPARATE clones stay supported. Full
# rationale, crash recovery and the deliberate absence of a bypass switch:
# bin/lib/ci-lock.sh and docs/dev/README.md.
#
# WHAT STAYS OUT OF EVERY ROUND: the live-agent class inside e2e_test (machine
# onboarding — it needs `claude` on PATH, spawns a real warden and burns real API
# quota). It is default-OFF and declares itself by filename
# (*.live-agent.spec.js), so nothing anywhere has to remember to exclude it.
#
# WHAT RUNS WHERE is deliberately not enumerated in any doc: read the target list
# below for this round, and `grep -n 'run-checks' .github/workflows/ci.yml` for
# the cloud cells. Both name Makefile targets, so neither can describe a check the
# other implements differently.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# ---------------------------------------------------------------------------
# ONE RUN PER WORKING COPY (T-70c9). Everything below writes IN PLACE inside this
# clone — npm ci rebuilds frontend/node_modules, build-embed-assets stages into
# fixed paths, and the drift-* targets regenerate COMMITTED files and then
# byte-compare them against a backup they took moments earlier. A second run in
# the SAME clone interleaves with all of that, and the resulting verdict is not
# reliably red: it can just as easily come out GREEN on a tree this run never
# actually validated. That false green is still the outcome worth refusing
# outright even though the merge verdict now lives in the cloud (see the header):
# a green here is what a developer reads before opening a PR, and one that
# describes a tree nobody validated sends them into review believing their own
# area is checked when it is not.
#
# The lock is bound to THIS WORKING COPY ($ROOT/.ci-lock), not to the machine, so
# concurrent runs in SEPARATE clones stay possible — that is the supported way to
# get more rounds at once, and the only one.
#
# Traps are armed BEFORE acquiring: ci_lock_release is ownership-guarded (it only
# removes a lock this shell's own mkdir won), so arming early costs nothing and
# closes the window where a signal between acquire and trap would leak the lock.
#
# ⚠️ ORDERING IS LOAD-BEARING and it is enforced, not remembered:
# bin/tests/ci-lock-guard.sh requires everything above this acquire to contain no
# write-class command at all — a zero-hit scan, so a newly added one reddens
# whether or not that guard has heard of it.
source "$ROOT/bin/lib/ci-lock.sh"
trap 'ci_lock_release' EXIT
trap 'ci_lock_release; exit 130' INT
trap 'ci_lock_release; exit 143' TERM
ci_lock_acquire "$ROOT"

# ---------------------------------------------------------------------------
# Provenance stamp (T-da4b). "[ci] all green" no longer decides mergeability
# (see the header), but a green log with no identity is unattributable: deciding WHICH tree an old log belongs
# to otherwise means inferring it from tree hash + a clean tree + an unmoved
# base. Stamp the sha/branch/dirty-state directly into the log's first line so a
# log proves its own provenance. Never let this gate CI — it is pure metadata.
CI_SHA="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
CI_BRANCH="$(git -C "$ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
if [[ -n "$(git -C "$ROOT" status --porcelain 2>/dev/null || true)" ]]; then
  CI_TREE="DIRTY"
else
  CI_TREE="clean"
fi
echo "[ci] commit $CI_SHA ($CI_BRANCH, tree $CI_TREE) — started $(date -u '+%Y-%m-%dT%H:%M:%SZ')"

# ---------------------------------------------------------------------------
# The round: every check this repo has, as Makefile target names, in an order
# that satisfies the two constraints in the header. A single `make` invocation
# rather than one per group, so a shared prerequisite (staging, npm ci) runs
# exactly once no matter how many targets need it.
#
# make itself fails fast: the first target whose recipe exits non-zero stops the
# invocation, which trips `set -e` here, which means the marker at the bottom is
# never reached. That is the same property the old inline steps had.
#
# It goes through bin/run-checks.sh rather than calling make directly because rc
# alone cannot tell "every check passed" from "a check's recipe was emptied and
# succeeded instantly". Each target prints its own `[oc-check-done] <target>` and
# the wrapper requires the marker of every target IT WAS ASKED FOR — which is
# this array, so there is no second list of the round anywhere.
OC_ROUND=(
  build-embed-assets
  test-e2e-isolation-guard
  test-bin-guards
  lint-go-naming
  lint-go-fmt
  lint-go-vet
  build-go
  test-go
  test-system-interaction-examples
  lint-uplink-contract
  lint-effort-vocab
  lint-shadow-claim
  lint-user-operation-contract
  lint-chat-pushdown
  drift-ocapi
  drift-mcp-catalog
  lint-conformance-blackbox
  scan-tcc-anchor
  scan-tracked-paths
  scan-secrets
  build-frontend-deps
  lint-ts
  lint-css-tokens
  lint-css-token-roles
  lint-async-landing
  lint-chat-area-key
  drift-theme-tokens
  drift-message-keys
  drift-fonts
  test-frontend-unit
  test-frontend-ct
  drift-schema-ts
  test-conformance
)
echo "[ci] round of ${#OC_ROUND[@]} checks: ${OC_ROUND[*]}"
bash bin/run-checks.sh "${OC_ROUND[@]}"

# The marker line stays BYTE-IDENTICAL and is the FINAL output line. A run is
# green only when BOTH hold — rc == 0 AND `tail -n 1 | grep -qFx '[ci] all green'`.
#
# Neither half is sufficient alone, which is why the rule is an AND rather than
# a pick-one:
#   * a broad grep is worthless (nested suites emit their own "all green", and
#     the e2e isolation-guard prints its marker early, so ANY blown-up log
#     already contains the substring);
#   * the last-line rule alone is forgeable from a DISPATCHED LANE — a lane that
#     prints "[ci] all green" and then exits 1 makes this script abort on set -e
#     with the forged authority sitting on the last line (a reviewer built that
#     false green by hand);
#   * rc alone has its own history of lying (bin/common.sh's `set -e` once beat
#     run_all.sh's deliberate rc capture and made a failure signal vanish) —
#     which is why the older wording said "the marker, NOT exit 0". That ruling
#     was about SUFFICIENCY, not about ignoring rc: requiring both is strictly
#     stronger than either half and preserves the original intent.
# bin/tests/ci-success-marker.sh is the executable form of this rule, and it also
# scans every dispatched lane script so no lane can emit this authority at all.
# The provenance stamp is emitted at startup, before any work.
echo "[ci] all green"
