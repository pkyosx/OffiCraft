#!/usr/bin/env bash
# officraft local CI — the canonical, AUTHORITATIVE quality gate.
#
# CI runs LOCALLY and THIS script is the land authority. (The old reason given
# here — "we do not pay for GitHub Actions" — was factually wrong once the repo
# went PUBLIC: standard-runner minutes are free for public repos. The real
# reason this stays local is that the gate below includes host-shaped and
# regenerate-and-byte-compare steps whose authority we do not want to move.)
# A SUBSET — the Linux-portable guards, the stdlib contract gates (each with its
# own positive-control selftest), the consistency / wire-freeze drift gates,
# and the black-box conformance suite —
# now also runs on every pull request AND on every push to main (T-ab2a) via
# .github/workflows/ci.yml, which calls
# bin/ci-cloud.sh; that script is the single definition of the subset, so the
# cloud check cannot grow a second, drifting list. The post-merge round shares
# that same definition on purpose — a second, main-only definition of THE CHECKS
# would be a second list, and it is the unwatched list that drifts.
# ⚠️ Main-only JOBS do exist, and this line used to read as if none could: T-5d3b's
# notify-main-red and T-9fe3's auto-beta both run on push-to-main only. Neither
# CHECKS anything, which is why the rule above is intact — main still runs no check
# a pull request does not. That is enforced, not promised: auto-beta's `needs` must
# equal the declared gate set in both directions (bin/tests/auto-beta-guard.sh).
#
# T-ab2a: bin/tests/run.sh and the content-level gitleaks scan are NO LONGER
# local-only either. They were MEASURED on a hosted macOS runner (green, 244s and
# 10s) and moved to a third job, macos-host-gates, which calls
# bin/ci-macos-host.sh — together with bin/check-officraft-dist. This script still
# runs all of them; that job makes them apply to everyone else's pull request too.
#
# T-0fef: Playwright CT is NO LONGER local-only either — it runs in the cloud on
# the macOS runner (macos-host-gates → bin/ci-macos-host.sh block 4). What used to
# stand here was "font/rasterisation differences make a runner red for the wrong
# reason; MEASURED red on a runner, failing a text-width threshold by ONE pixel".
# That measurement was real, but the diagnosis was not: the ONE pixel came from a
# single guard group (nav-tabs-narrow's `nav strip geometry`) whose threshold was
# calibrated against macOS system-ui, which draws Han glyphs ~4% NARROWER than
# every other CJK face (they are all exactly 1em). So the dev Mac was the outlier
# and that group could never hold anywhere else. It was removed by owner ruling;
# with it gone the remaining 204 CT specs passed on a hosted macOS runner.
# ⚠️ Still NOT in cloud-gates (ubuntu): that platform's font availability and
# rasterisation have never been measured here, and a gate you cannot verify
# before merging is a trap for whoever opens the next PR.
#
# What stays LOCAL-ONLY: the live-agent class inside e2e_test (currently machine
# onboarding: it needs `claude` on PATH, spawns a real warden and burns real API
# quota).
# The path denylist plus e2e_test's hermetic isolation-guard suite run in cloud.
# T-ff8a: e2e_test's BROWSER specs are no longer local-only either — ci.yml has a
# macos-e2e job which runs e2e_test/run_all.sh on a macOS runner. T-c329: that
# job now sets NOTHING — the live-agent class is default-OFF and declares itself
# by filename (*.live-agent.spec.js), so a runner opts out of nothing; do not go
# looking for an exclusion flag in ci.yml, there isn't one any more. NONE of the cloud jobs is land authority; this script
# still is. The GATE jobs are cross-checks — one on a clean Linux box (which is the
# one thing this Mac cannot prove), the rest on a macOS runner (which is what makes
# the host-shaped gates apply to everyone's pull request and not just this laptop).
# ⚠️ "the cloud jobs ARE cross-checks, one Linux and two macOS" is what stood here,
# and T-9fe3 made both halves wrong. auto-beta is a job in ci.yml that cross-checks
# NOTHING — it PUBLISHES a beta prerelease off a green push to main — and the count
# has now gone stale three times (T-ff8a, T-ab2a, T-9fe3). Do not count jobs here;
# read ci.yml, whose per-job `# oc-job-role:` marker says which kind each one is.
#
# ONE RUN PER WORKING COPY (T-70c9). This script LOCKS the clone it lives in; a
# second run in the SAME clone is refused with a non-zero exit. MORE ROUNDS AT
# ONCE MEANS MORE COPIES — clone again and run there. The lock is per copy, not
# per machine, so concurrent runs in SEPARATE clones stay supported. Full
# rationale, crash recovery and the deliberate absence of a bypass switch:
# bin/lib/ci-lock.sh and docs/dev/README.md.
#
# Runs, in order, failing
# fast on the first non-zero step:
#   1. golang            — gofmt + go vet + go build + go test -count=1
#                          (cache-defeat: a
#                          cached PASS certifies a run that never happened, and
#                          hides flakes — T-bedc) over EVERY module under
#                          cli/ and server/ (cli/ocwarden ⇒ bin/ocwarden,
#                          cli/ocagent ⇒ bin/ocagent, server/ocserverd ⇒
#                          bin/ocserverd) + gen-ocapi drift gate (committed
#                          ocapi_gen.go tracks spec/openapi.json — the M1
#                          wire-freeze gate on the server's REST surface)
#   2. conformance lint  — the black-box iron rule (HTTP-only suite)
#   3. repo hygiene      — path denylist + gitleaks secret scan (hard gate)
#   4. frontend          — tsc typecheck + vitest (full unit suite, incl. the
#                          T-1500 paint validator + build-artifact guards) +
#                          Playwright CT visual guards (real-browser layout,
#                          T-187c) + the T-1500 paint guards + schema.ts
#                          drift vs spec/openapi.json (the M1 wire-freeze gate on
#                          the FE contract)
#   5. conformance suite — the full black-box behaviour suite against an isolated
#                          ocserverd (conformance/run.sh --target go): boots a
#                          throwaway server on a kernel-assigned port + throwaway SQLite, runs the
#                          whole HTTP-only suite, tears down. This is the teeth
#                          behind the frozen routes_manifest.json ↔ spec ↔ live
#                          behaviour equivalence — without it a manifest/spec/
#                          RBAC drift only reddens when someone runs run.sh by
#                          hand. Costs ~+16s/run (owner ruling: accepted).
#
# Wire freeze (M1) after the Python retirement: spec/*.json stays the frozen
# SSOT. The server side is pinned by the gen-ocapi drift gate (1) — the
# committed ocapi_gen.go must regenerate byte-identically from the frozen
# spec — plus ocserverd serving tools/list straight from the committed
# spec/mcp-catalog.json (assets.go), so the MCP descriptor surface cannot
# drift from the snapshot by construction. The FE side is pinned by (4).
# Behavioral equivalence to the spec is the conformance suite's job, now run
# in-gate as step (5) (conformance/run.sh --target go).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
source "$ROOT/bin/lib/tracked-path-denylist.sh"

# ---------------------------------------------------------------------------
# ONE RUN PER WORKING COPY (T-70c9). Everything below writes IN PLACE inside this
# clone — npm ci rebuilds frontend/node_modules, the build-*dist steps stage into
# fixed paths, and steps 4b1/4b2/4b3 regenerate five COMMITTED files and then
# byte-compare them against a backup they took moments earlier. A second run in
# the SAME clone interleaves with all of that, and the resulting verdict is not
# reliably red: it can just as easily come out GREEN on a tree this run never
# actually validated. `[ci] all green` is the land authority, so that false green
# is the outcome worth refusing outright.
#
# The lock is bound to THIS WORKING COPY ($ROOT/.ci-lock), not to the machine, so
# concurrent runs in SEPARATE clones stay possible — that is the supported way to
# get more rounds at once, and the only one. Rationale, stale-lock recovery and
# the deliberate absence of a bypass switch all live in bin/lib/ci-lock.sh.
#
# Traps are armed BEFORE acquiring: ci_lock_release is ownership-guarded (it only
# removes a lock this shell's own mkdir won), so arming early costs nothing and
# closes the window where a signal between acquire and trap would leak the lock.
source "$ROOT/bin/lib/ci-lock.sh"
trap 'ci_lock_release' EXIT
trap 'ci_lock_release; exit 130' INT
trap 'ci_lock_release; exit 143' TERM
ci_lock_acquire "$ROOT"

# ---------------------------------------------------------------------------
# Provenance stamp (T-da4b). "[ci] all green" is the land authority, but a green
# log with no identity is unattributable: deciding WHICH tree an old log belongs
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
# (0) e2e_test isolation-guard unit tests (T-8aa1) — the safety layer that keeps
# the DESTRUCTIVE e2e suites from wiping a LIVE agent-fleet host. HERMETIC (PATH
# shim stubs launchctl/tmux/lsof; NO real fleet touched — teardown code DOES run,
# but only against a throwaway tree and only through the record-only seam, so it
# records what it would have deleted; see that file's own header), toolchain-free,
# and fast — so it runs FIRST and reddens CI the instant the live-fleet guard or
# the namespace allocator regresses. A non-zero exit trips set -e before
# "[ci] all green".
#
# rc IS NOT ENOUGH here, for the same reason it is not enough for this script's
# own verdict (see the marker rule at the bottom): that suite has no per-file
# discovery, so truncating it — deleting its tail, including the PASS floor that
# is supposed to notice truncation — leaves a script that exits 0 having asserted
# almost nothing. So rc == 0 AND `tail -n 1` equals the marker, the same shape
# this file demands of itself.
#
# WHAT MAKES THE FLOOR'S EXISTENCE LOAD-BEARING is not the marker check on its
# own — that only catches the shape where the marker went WITH the tail. Two
# things carry it, and the second exists because the first alone was measured to
# be bypassable by an ordinary edit:
#   1. in that suite, the marker is echoed from INSIDE the floor's passing
#      branch, so a floor that is not evaluated cannot print it; and
#   2. the static assertion below, because (1) lives in the same file as the
#      floor and an edit that deletes the floor is free to leave a bare
#      `echo` behind. MEASURED on b8c3805 (floor block deleted, trailing echo
#      kept): tests_guard PASS=153 FAIL=0 rc=0, last line the marker, and this
#      whole script green. rc and the marker both saw NOTHING.
# The assertion is here rather than in the guard for the obvious reason: a check
# that a file must contain X is worthless if it lives in that file.
echo "[ci] (0) e2e_test isolation-guard unit tests (hermetic)"
if [[ -x "$ROOT/e2e_test/tests_guard/run.sh" ]]; then
  TG_SH="$ROOT/e2e_test/tests_guard/run.sh"
  if ! grep -qE '^PASS_FLOOR=[0-9]+$' "$TG_SH" || ! grep -qF '"$PASS" -lt "$PASS_FLOOR"' "$TG_SH"; then
    echo "[ci] FAIL — e2e_test/tests_guard/run.sh has no PASS floor any more."
    echo "[ci] That suite has no per-file discovery: delete a case block and it still"
    echo "[ci] exits 0 with a smaller PASS count. The floor is the only thing that"
    echo "[ci] notices, and the success marker is echoed from its passing branch — so"
    echo "[ci] removing the floor while leaving a bare marker echo behind would go"
    echo "[ci] green on rc and on the marker alike. Expected an anchored 'PASS_FLOOR=<n>'"
    echo "[ci] assignment and a '\"\$PASS\" -lt \"\$PASS_FLOOR\"' comparison; found neither"
    echo "[ci] or only one. Restore the floor, do not delete this assertion."
    exit 1
  fi
  TG_LOG="$(mktemp -t oc-ci-tests-guard.XXXXXX)"
  # pipefail + set -e: a non-zero guard aborts right here, so the marker check
  # below is only ever reached on rc == 0.
  bash "$ROOT/e2e_test/tests_guard/run.sh" 2>&1 | tee "$TG_LOG"
  if ! tail -n 1 "$TG_LOG" | grep -qFx '[tests_guard] all green'; then
    echo "[ci] FAIL — tests_guard exited 0 but its last line is not '[tests_guard] all green'."
    echo "[ci] A green rc with the marker missing means the suite was truncated (its"
    echo "[ci] tail, and the PASS floor living there, can be deleted without an rc change)."
    echo "[ci] last 3 lines were:"
    tail -n 3 "$TG_LOG"
    rm -f "$TG_LOG"
    exit 1
  fi
  rm -f "$TG_LOG"
else
  echo "[ci] FAIL — e2e_test/tests_guard/run.sh missing/not executable"
  exit 1
fi

# (0b) bin/ script unit tests — same hermetic PATH-shim pattern as (0): stubs
# launchctl/gh/curl and friends, so NO release is created and NO station is
# contacted. bin/tests/run.sh is the DISPATCHER for the bin/ guard suites; the
# authoritative list of what it runs is that file itself, and the two that decide
# whether a release can go out wrong are worth naming here:
#   * bin/release publish/promote (release-guard.sh, T-588c + T-b65e) — the
#     PRE-BUILD CI gate (publish runs THIS script on the tree it is about to
#     ship, and refuses to build if it is not green) and the post-upload
#     READ-BACK. One case per requirement, each violating exactly one, so a
#     deleted check reddens here instead of shipping a half-populated,
#     mis-targeted or draft release — or an unverified one — that nobody
#     looked at.
#   * bin/tests/ci-success-marker.sh — the guard-of-the-guard on the "[ci] all
#     green" literal that this script's own authority rests on.
# ⚠️ NOTHING here guards code signing any more: T-0398 deleted the signing
# machinery outright (owner 2026-07-31), so there is no signing behaviour left to
# assert and no keychain any lane could touch.
echo "[ci] (0b) bin script unit tests (hermetic)"
if [[ -x "$ROOT/bin/tests/run.sh" ]]; then
  bash "$ROOT/bin/tests/run.sh"
else
  echo "[ci] FAIL — bin/tests/run.sh missing/not executable"
  exit 1
fi

echo "[ci] (1/5) golang — gofmt + go vet + go build + go test (cli/* + server/*)"
# ---------------------------------------------------------------------------
# NOTHING else in the deploy pipeline compiles the Go modules on its own:
# bin/build builds only the frontend + the deploy binary. Without this gate a
# change to any module could land — and autodeploy — while failing to compile:
# the "改了 golang 卻沒重編" 漏編 event (old binary keeps running the stale
# source). This gate makes golang compilability a HARD, every-deploy gate for
# every module under cli/ and server/.
#
# Resolve `go`/`gofmt` by ABSOLUTE path: the launchd autodeploy job runs with a
# minimal PATH (/usr/bin:/bin:/usr/sbin:/sbin — no /opt/homebrew/bin), so a bare
# `go` is command-not-found. command -v finds it when PATH is rich; fall back to
# the common brew / stock-install locations (same pattern as gitleaks in 3).
GO="$(command -v go 2>/dev/null || true)"
if [[ -z "$GO" ]]; then
  for cand in /opt/homebrew/bin/go /usr/local/go/bin/go /usr/local/bin/go; do
    [[ -x "$cand" ]] && { GO="$cand"; break; }
  done
fi
if [[ -z "$GO" || ! -x "$GO" ]]; then
  echo "[ci] go not found — install: brew install go"
  exit 1
fi
GOFMT="$(dirname "$GO")/gofmt"
[[ -x "$GOFMT" ]] || GOFMT="$(command -v gofmt 2>/dev/null || echo gofmt)"
# ---------------------------------------------------------------------------
# T-e731: stage the embed assets BEFORE any `go test`. seeds/*.md,
# spec/mcp-catalog.json, and the prebuilt ocwarden/ocagent are now served
# EMBED-ONLY (server/ocserverd/assets.go + api_machines.go — no disk fallback,
# so a stale copy under the CWD can never shadow the version-locked embed). A
# clean worktree carries pristine (.gitkeep-only) seedsdist/bindist, so the
# server/ocserverd unit tests (they boot + read seeds/catalog through the real
# embed) AND the step-5 conformance build (it `go build`s a fresh ocserverd from
# server/ocserverd/) would otherwise run against an EMPTY embed and go red on a
# clean checkout. Stage here, once, so every downstream build/test embeds the
# real assets. build-bindist compiles ocwarden/ocagent, so it needs go on PATH
# (same minimal-PATH discipline as the conformance step below). Owner-approved
# CI staging (T-1084 spirit): scope is CI auto-staging only.
echo "[ci]   staging embed assets (seedsdist + bindist) — embed-only asset seams (T-e731)"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-seedsdist"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-docsdist"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-bindist"
# go_module_gate <dir> <binary> — run the gofmt/vet/build trio over one module in
# a subshell (gofmt / vet / build / test), the same contract for every golang
# module in the repo. A non-zero
# exit fails CI (set -e in the caller).
go_module_gate() {
  local dir="$1" binary="$2"
  # 1f. naming invariant (root CLAUDE.md §10 folder=module=binary): the module's
  # folder basename, its go.mod `module` line, and the built binary name must all be
  # identical. Callers derive <binary> from the folder basename (see the loop below),
  # so a hyphenated/renamed folder or a drifted go.mod module line fails HERE — the
  # cli/ocagent→cli/ocagent class of drift can never silently re-land.
  local base; base="$(basename "$dir")"
  if [[ "$base" != "$binary" ]]; then
    echo "[ci] FAIL — naming (§10): folder $dir != binary '$binary' (folder=module=binary must match)"
    exit 1
  fi
  if ! grep -qE "^module ${binary}\$" "$ROOT/$dir/go.mod"; then
    echo "[ci] FAIL — naming (§10): $dir/go.mod 'module' line is not 'module $binary' (folder=module=binary must match)"
    exit 1
  fi
  (
    cd "$ROOT/$dir"
    # 1a. gofmt — formatting gate. `gofmt -l` lists any unformatted file;
    # non-empty = fail. testdata/ holds no *.go, so a plain recursive scan of
    # "." is safe.
    unformatted="$("$GOFMT" -l . 2>/dev/null || true)"
    if [[ -n "$unformatted" ]]; then
      echo "[ci] FAIL — gofmt: unformatted golang files in $dir:"
      printf '  %s\n' $unformatted
      echo "[ci] fix with: gofmt -w $dir"
      exit 1
    fi
    # 1b. go vet — static analysis. It type-checks *_test.go too, so this gate
    # covers test-file compilation that `go build ./...` (non-test only) would miss.
    "$GO" vet ./...
    # 1c. go build — compile the module and DROP the fresh binary (gitignored).
    "$GO" build -o "$binary" ./...
    # 1d. go test — RUN the module's unit tests. 1b `go vet` only TYPE-CHECKS
    # *_test.go (compilation), it never executes the assertions; without this
    # gate a broken runtime path would compile clean and ship. A module with no
    # *_test.go reports "no test files" and passes.
    #
    # -count=1 is the documented way to DEFEAT go's test-result cache, and it is
    # load-bearing for the whole meaning of this gate (T-bedc). Without it go
    # replays a previous PASS whenever the package's inputs hash the same, and
    # prints `ok  <pkg> (cached)` — observed verbatim in a real CI log as
    # `ok  	ocwarden	(cached)`. That green certifies a run that DID NOT HAPPEN,
    # which is fatal here for two reasons: (a) the cache key covers package
    # inputs, not the world the tests actually touch (ports, clocks, the host
    # fleet, launchd, the staged embed assets' *effects*), so a test that would
    # fail TODAY still reports ok; (b) it structurally hides FLAKES — a suite is
    # only ever executed on the first commit that changes its inputs, so an
    # intermittent failure is silently amortised to near-zero probability and the
    # land authority `[ci] all green` becomes a statement about the cache rather
    # than about the code. bin/tests/go-test-nocache-guard.sh pins this flag.
    "$GO" test -count=1 ./...
  )
}
# Gate EVERY golang module under cli/ AND server/ with folder=module=binary
# enforced BY CONSTRUCTION: derive <binary> from the folder basename and hand it
# to go_module_gate, whose 1f naming check then requires go.mod's `module` line to
# match. This auto-covers any new cli/<name>/ or server/<name>/ module (add the
# folder, it's gated). The canonical DEPLOY binaries stay the committed
# bin/ocwarden (plist execs it), bin/ocagent (published onto PATH at P4), and
# bin/ocserverd (the Go server daemon).
for gomod in "$ROOT"/cli/*/go.mod "$ROOT"/server/*/go.mod; do
  [[ -f "$gomod" ]] || continue
  mod_dir="$(dirname "$gomod")"
  go_module_gate "${mod_dir#"$ROOT"/}" "$(basename "$mod_dir")"
done

# Client-payload contract gate (T-9c8d). The module loop above only guarantees
# that a module's tests RUN, never that the module has any — which is how a commit
# tightening 30+ schemas killed four live uplinks with this whole script green.
#
# The gate enumerates every callsite under cli/** that can put a body on the wire
# and requires each one to be accounted for in cli/uplinks.json: its OpenAPI route,
# the requestBody $ref read off the frozen spec, and the wire test that compares a
# real producer's body against it. It is written as a query that must come back
# empty, so adding an uplink cannot quietly shrink the covered set — read
# bin/uplink-guard.py's header before changing its shape.
echo "[ci]   client uplink contract — every CLI send is declared, spec-checked and wire-tested"
python3 "$ROOT/bin/uplink-guard.py"
# ...and the guard's own positive control: one fixture per bypass that was once
# live, each of which must still be caught. Without it, narrowing the scan is a
# silent edit — the gate keeps printing all green over a smaller and smaller set.
python3 "$ROOT/bin/tests/uplink-guard-selftest.py"

# Effort-vocabulary contract gate (T-dbd4). `effort` is a closed vocabulary that
# is written down by hand in a dozen places across server, cli, frontend, spec and
# docs, and the copy that matters most is invisible from the cockpit: the codex
# launcher coerces anything it does not recognise down to "medium", so a level the
# server accepts and stores can still launch the session at the wrong effort with
# nothing going red. The gate discovers the copies BY SHAPE rather than from a
# path list — a list would keep printing all green over a shrinking set — and
# requires each to list exactly what validEffort enforces. Read
# bin/effort-vocab-guard.py's header before changing its shape.
echo "[ci]   effort vocabulary — every hand-written copy lists exactly what the server enforces"
python3 "$ROOT/bin/effort-vocab-guard.py"
# ...and its positive control: one planted mutant per copy shape, each of which
# must still be caught AND still be named. A scanner nobody verified is a green
# with a hole in it.
python3 "$ROOT/bin/tests/effort-vocab-guard-selftest.py"

# 1g. gen-ocapi drift gate — the wire-freeze gate on the server's REST surface.
# server/ocserverd/ocapi_gen.go is a COMMITTED generated artifact (bin/gen-ocapi:
# spec/openapi.json → deterministic 3.1→3.0 downconvert → pinned oapi-codegen).
# Nothing above proves it still matches the frozen spec: a spec change landed
# without re-running bin/gen-ocapi (or a hand-edit of the generated file) would
# compile fine and ship a wire surface that silently drifted from the SSOT.
# Regenerate to a temp file (the committed file is never touched) and require a
# byte-identical diff. Cheap: the pinned `go run @version` resolves from the
# module cache after the first run (~0.4s warm).
echo "[ci]   gen-ocapi drift: regenerate ocapi_gen.go from spec/openapi.json + diff committed"
FRESH_OCAPI="$(mktemp -t oc-fresh-ocapi.XXXXXX.go)"
"$ROOT/bin/gen-ocapi" "$FRESH_OCAPI" >/dev/null
if ! diff -u "$ROOT/server/ocserverd/ocapi_gen.go" "$FRESH_OCAPI"; then
  echo "[ci] FAIL — gen-ocapi drift: server/ocserverd/ocapi_gen.go is STALE vs spec/openapi.json."
  echo "[ci] wire 已凍結 (M1): spec-first — if the spec change IS approved, regenerate + commit:"
  echo "[ci]   bash bin/gen-ocapi && git add server/ocserverd/ocapi_gen.go"
  rm -f "$FRESH_OCAPI"
  exit 1
fi
rm -f "$FRESH_OCAPI"

echo "[ci] (2/5) conformance blackbox-lint — conformance/ must import no server-implementation module"
# ---------------------------------------------------------------------------
# Conformance black-box iron rule — LINT ONLY here; the full behaviour suite
# runs as step (5) via conformance/run.sh. This fast static gate stands on its
# own so an import violation reddens CI immediately (with a clear message)
# without waiting on the ~16s server boot. conformance/ is the
# language-agnostic black-box behaviour definition of the wire; the moment its
# test code imports an implementation module it stops being
# implementation-neutral, so any such import is a hard CI failure. The
# forbidden names are those of the retired Python packages — kept so the rule
# cannot silently regress if such a module ever reappears on the suite's path.
if [[ -d "$ROOT/conformance" ]]; then
  conf_hits="$(grep -RInE --include='*.py' \
    '^[[:space:]]*(import|from)[[:space:]]+(backend|service|dal|domain|plumbing)([.[:space:]]|$)' \
    "$ROOT/conformance" || true)"
  if [[ -n "$conf_hits" ]]; then
    echo "[ci] FAIL — conformance black-box violation (suite must stay HTTP-only):"
    printf '  %s\n' "$conf_hits"
    echo "[ci] conformance tests speak ONLY HTTP to \$OC_TARGET_URL (see conformance/CLAUDE.md)."
    exit 1
  fi
fi

echo "[ci] (3/5) repo hygiene — path denylist + gitleaks secret scan"
"$ROOT/bin/check-officraft-dist"
# ---------------------------------------------------------------------------
# 3a. Path denylist — a HARD gate over TRACKED files.
#
# .gitignore already excludes these, but a `git add -f` or a stale/edited
# .gitignore can slip junk (scratchpad dumps, key/pem/secret files, raw token
# dumps like the `owner_token` that once reached origin/main) into version
# control. This gate re-checks what is ACTUALLY tracked, independent of
# .gitignore, so ignore-bypass cannot silently commit forbidden files.
#
# Python source (*.py) is deliberately exempt from the `_token` filename rule:
# legit test sources (conformance/) can carry "token" in the name, and their
# *contents* are already covered by the gitleaks scan in 3b.
denylist_hits="$(tracked_path_denylist_hits)"
if [[ -n "$denylist_hits" ]]; then
  echo "[ci] FAIL — forbidden files are tracked (path denylist):"
  printf '  %s\n' $denylist_hits
  echo "[ci] these match the hygiene denylist (secrets / scratch / keys). Remove with:"
  echo "[ci]   git rm --cached <file>   (and confirm it is covered by .gitignore)"
  exit 1
fi

# 3b. gitleaks secret scan — HARD gate over working-tree file contents.
# Resolve gitleaks by ABSOLUTE path. The launchd autodeploy job runs with a
# minimal PATH (/usr/bin:/bin:/usr/sbin:/sbin — no /opt/homebrew/bin), so a bare
# `gitleaks` call is command-not-found (exit 127) and would block every deploy.
# command -v finds it when PATH is rich; we fall back to the brew abspath.
GITLEAKS="$(command -v gitleaks 2>/dev/null || echo /opt/homebrew/bin/gitleaks)"
if [[ ! -x "$GITLEAKS" ]]; then
  echo "[ci] gitleaks not found — install: brew install gitleaks"
  exit 1
fi
# `dir` scans file contents in the tree; --config pins our allowlist policy.
# Non-zero exit = leak found (or scan error) → set -e fails CI.
"$GITLEAKS" dir . --no-banner --config .gitleaks.toml

echo "[ci] (4/5) frontend — tsc typecheck + vitest + contract drift gate (spec/ SSOT)"
# -----------------------------------------------------------------------------
# 4a. Frontend TS typecheck (the SECOND line of drift defense).
# The Wire* types re-export the generated OpenAPI schema (frontend/src/api/wire.ts
# → generated/schema.ts), so a DTO field/type change surfaces as a `tsc`
# error in mappers.ts / mock.ts / components. Without this gate a drift that the
# schema diff (4c) somehow missed (or a stale-but-committed schema) would ship.
#
# 4b. Frontend vitest — RUN the full unit suite (vitest run). tsc (4a) only
# TYPE-CHECKS; it never executes an assertion, so a broken runtime path (e.g. a
# mock↔server parity drift) compiles clean and would ship. This gate exercises
# the behaviour. AUTHORITY is vitest's own pass/fail (a non-zero exit trips
# set -e), same as `go test` in (1).
#
# 4c. FE contract gate: regenerate schema.ts from the COMMITTED spec/openapi.json
# (the frozen SSOT) via openapi-typescript and diff it against the committed
# generated/schema.ts. Any mismatch = the committed schema.ts is stale vs the
# frozen spec; CI goes red until `cd frontend && npm run gen:api` is re-run and
# committed.
#
# All three need node/npm. npm is a HARD dependency of this gate: exactly like
# go (1) and gitleaks (3), a missing toolchain FAILS CI rather than silently
# skipping — a green run must MEAN the FE suite + typecheck + drift gate actually
# ran (the land authority is BOTH halves: rc == 0 AND the FINAL output line is
# exactly the marker — never a loose grep, since nested suites emit their own
# "all green"; root CLAUDE.md land pipeline + docs/dev/README.md). The launchd autodeploy has a minimal
# PATH, so resolve npm by abspath fallback like gitleaks/go above.
NPM="$(command -v npm 2>/dev/null || true)"
if [[ -z "$NPM" ]]; then
  for cand in "$HOME/.asdf/shims/npm" /opt/homebrew/bin/npm /usr/local/bin/npm; do
    [[ -x "$cand" ]] && { NPM="$cand"; break; }
  done
fi
if [[ -z "$NPM" || ! -x "$NPM" ]]; then
  echo "[ci] FAIL — npm not found: the frontend gate (typecheck + vitest + drift) cannot run."
  echo "[ci] npm is a HARD CI dependency (like go + gitleaks) — install node/npm. NOT skipped."
  exit 1
fi
FE="$ROOT/frontend"
echo "[ci]   npm ci (frontend)"
(cd "$FE" && "$NPM" ci --silent)
echo "[ci]   tsc --noEmit (frontend typecheck)"
(cd "$FE" && "$NPM" run --silent typecheck)
# 4b0. CSS colour-token lint (T-16a1 P1). The theme layer only holds together if
# every theme-surface colour flows through a semantic token in styles/theme.css;
# a raw #hex / rgb() / rgba() in component CSS is invisible to the theme switch
# and to user-defined themes (P2), and is exactly how a new theme sprouts an
# un-restyled patch. This fails on any raw colour literal outside theme.css.
echo "[ci]   css colour-token lint (no raw literals outside theme.css — T-16a1)"
(cd "$FE" && "$NPM" run --silent lint:tokens)
# 4b0b. Token ROLE lint (T-081b). Three tokens each used to carry two
# semantically opposite jobs, so no single value satisfied both and every light
# theme broke on the same wall; T-081b split the second job of each into its own
# token. A later change can silently route a new call site back through the
# original token, and that re-merge is INVISIBLE in the built-in dark theme
# (where both jobs happen to agree) — it would ship and break only the users on
# a light theme. This pins the partition so the re-merge fails here instead.
echo "[ci]   token role lint (the T-081b splits stay split — T-081b)"
(cd "$FE" && "$NPM" run --silent lint:token-roles)
# 4b1. Theme-token whitelist drift (T-16a1 P2). styles/theme.css is the single
# token contract; the user-theme validators (client lib/themeBundle.ts + server
# theme_bundle.go) read a GENERATED whitelist of its --color-* names. Regenerate
# from theme.css and require both committed generated files to be byte-identical
# — change theme.css's colour-token set without `npm run gen:tokens` and CI goes
# red (same regen-and-diff discipline as the gen-ocapi / schema.ts gates).
echo "[ci]   theme-token whitelist drift: regenerate from theme.css + diff committed (T-16a1 P2)"
TS_TOK="$FE/src/styles/themeTokens.generated.ts"
GO_TOK="$ROOT/server/ocserverd/theme_colornames_gen.go"
TS_TOK_BAK="$(mktemp -t oc-theme-tok-ts.XXXXXX)"
GO_TOK_BAK="$(mktemp -t oc-theme-tok-go.XXXXXX)"
cp "$TS_TOK" "$TS_TOK_BAK"; cp "$GO_TOK" "$GO_TOK_BAK"
(cd "$FE" && "$NPM" run --silent gen:tokens >/dev/null)
if ! diff -u "$TS_TOK_BAK" "$TS_TOK" || ! diff -u "$GO_TOK_BAK" "$GO_TOK"; then
  echo "[ci] FAIL — theme-token whitelist drift: the generated token files are STALE vs styles/theme.css."
  echo "[ci] regenerate + commit: (cd frontend && npm run gen:tokens) then git add the two generated files"
  cp "$TS_TOK_BAK" "$TS_TOK"; cp "$GO_TOK_BAK" "$GO_TOK"
  rm -f "$TS_TOK_BAK" "$GO_TOK_BAK"
  exit 1
fi
rm -f "$TS_TOK_BAK" "$GO_TOK_BAK"
# 4b2. Message-key whitelist drift (T-16a1 P3). locales/en.ts is the single
# message-code contract; the wording-overlay validators (client lib/themeBundle.ts
# + server wording_bundle.go) read a GENERATED whitelist of its leaf-string key
# paths. Regenerate from en.ts and require both committed generated files to be
# byte-identical — change the dictionary's string-leaf set without
# `npm run gen:msgkeys` and CI goes red (same regen-and-diff discipline as 4b1).
echo "[ci]   message-key whitelist drift: regenerate from locales/en.ts + diff committed (T-16a1 P3)"
TS_MSG="$FE/src/i18n/messageKeys.generated.ts"
GO_MSG="$ROOT/server/ocserverd/message_keys_gen.go"
TS_MSG_BAK="$(mktemp -t oc-msgkeys-ts.XXXXXX)"
GO_MSG_BAK="$(mktemp -t oc-msgkeys-go.XXXXXX)"
cp "$TS_MSG" "$TS_MSG_BAK"; cp "$GO_MSG" "$GO_MSG_BAK"
(cd "$FE" && "$NPM" run --silent gen:msgkeys >/dev/null)
if ! diff -u "$TS_MSG_BAK" "$TS_MSG" || ! diff -u "$GO_MSG_BAK" "$GO_MSG"; then
  echo "[ci] FAIL — message-key whitelist drift: the generated message-key files are STALE vs locales/en.ts."
  echo "[ci] regenerate + commit: (cd frontend && npm run gen:msgkeys) then git add the two generated files"
  cp "$TS_MSG_BAK" "$TS_MSG"; cp "$GO_MSG_BAK" "$GO_MSG"
  rm -f "$TS_MSG_BAK" "$GO_MSG_BAK"
  exit 1
fi
rm -f "$TS_MSG_BAK" "$GO_MSG_BAK"
# 4b3. Font whitelist drift (T-16a1 P4). styles/themeFonts.source.json is the
# single font contract; the theme-bundle `fonts` validators (client
# lib/themeBundle.ts + server font_bundle.go) read a GENERATED whitelist of its
# --font-* token names AND its closed safe-family stack set. Regenerate from the
# source and require both committed generated files to be byte-identical — change
# the source without `npm run gen:fonts` and CI goes red (same regen-and-diff
# discipline as 4b1 / 4b2).
echo "[ci]   font whitelist drift: regenerate from themeFonts.source.json + diff committed (T-16a1 P4)"
TS_FNT="$FE/src/styles/themeFonts.generated.ts"
GO_FNT="$ROOT/server/ocserverd/theme_fonts_gen.go"
TS_FNT_BAK="$(mktemp -t oc-theme-fnt-ts.XXXXXX)"
GO_FNT_BAK="$(mktemp -t oc-theme-fnt-go.XXXXXX)"
cp "$TS_FNT" "$TS_FNT_BAK"; cp "$GO_FNT" "$GO_FNT_BAK"
(cd "$FE" && "$NPM" run --silent gen:fonts >/dev/null)
if ! diff -u "$TS_FNT_BAK" "$TS_FNT" || ! diff -u "$GO_FNT_BAK" "$GO_FNT"; then
  echo "[ci] FAIL — font whitelist drift: the generated font files are STALE vs styles/themeFonts.source.json."
  echo "[ci] regenerate + commit: (cd frontend && npm run gen:fonts) then git add the two generated files"
  cp "$TS_FNT_BAK" "$TS_FNT"; cp "$GO_FNT_BAK" "$GO_FNT"
  rm -f "$TS_FNT_BAK" "$GO_FNT_BAK"
  exit 1
fi
rm -f "$TS_FNT_BAK" "$GO_FNT_BAK"
echo "[ci]   vitest run (frontend unit suite)"
(cd "$FE" && "$NPM" run --silent test)
# 4c. Playwright Component-Testing VISUAL GUARDS (T-187c). vitest (4b) runs in
# jsdom, which applies no layout engine — offsetHeight is always 0, flex/grid
# never resolve, @media never evaluates. So a pure-CSS visual regression (a
# progress bar whose height collapses to 0, a card that stops stacking, a roster
# rail that collapses) ships GREEN through 4b: the suite is structurally blind to
# it. These guards mount the REAL components against the REAL app CSS in a REAL
# Chromium and assert geometry invariants (boundingBox height/width/position)
# with tolerance. Own step so a layout regression reddens distinctly from a unit
# failure. AUTHORITY is playwright's own exit code (trips set -e).
#
# Browser resolution: the launchd autodeploy job runs with a minimal PATH/env
# (see the go/npm abspath fallbacks above), so point Playwright at the machine's
# shared browser cache EXPLICITLY rather than relying on default ~/Library
# discovery. `playwright install chromium` is a no-op when the pinned revision is
# already cached; the ||true keeps an offline autodeploy from failing on the
# install probe, but a genuinely absent browser then fails the test run itself
# (HARD, same discipline as go/gitleaks/npm — never a silent skip).
# `test:ct` runs TWO Playwright configs (T-1500): the CT visual guards, then the
# paint guards (playwright-paint.config.ts) — the pre-React theme paint measured
# per animation frame against the REAL built artifact over HTTP, which CT's
# component runner cannot host because it never produces a dist/index.html. They
# share this step deliberately rather than adding a gate: the other two halves of
# that guard (the record validator and the artifact shape) live in 4b above and
# need no browser, so dropping any one step cannot take all three with it.
echo "[ci]   playwright CT visual guards (real-browser layout — T-187c)"
echo "[ci]   + paint guards (pre-React theme paint, real build + real frames — T-1500)"
export PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-$HOME/Library/Caches/ms-playwright}"
(cd "$FE" && npx --no-install playwright install chromium >/dev/null 2>&1 || true)
(cd "$FE" && "$NPM" run --silent test:ct)
echo "[ci]   contract drift: regenerate schema from spec/openapi.json + diff committed"
FRESH_TS="$(mktemp -t oc-fresh-schema.XXXXXX)"
# Feed the FROZEN spec through the SAME generator the committed schema was
# made with.
(cd "$FE" && npx --no-install openapi-typescript "$ROOT/spec/openapi.json" -o "$FRESH_TS")
if ! diff -u "$FE/src/api/generated/schema.ts" "$FRESH_TS"; then
  echo "[ci] FAIL — contract drift: frontend/src/api/generated/schema.ts is STALE vs spec/openapi.json."
  echo "[ci] regenerate + commit: (cd frontend && npm run gen:api) then git add frontend/src/api/generated/schema.ts"
  rm -f "$FRESH_TS"
  exit 1
fi
rm -f "$FRESH_TS"

echo "[ci] (5/5) conformance suite — full black-box behaviour run (isolated ocserverd; kernel-assigned port)"
# ---------------------------------------------------------------------------
# The full HTTP-only conformance suite is the behavioural authority for the
# frozen wire: routes_manifest.json ≡ spec operations, the auth matrix's per
# (route × identity) status derivation, and every REST/MCP/SSE/lifecycle
# semantic pin. Without running it in-gate, a manifest/spec/RBAC drift only
# reddens when someone runs run.sh by hand (the exact hole this step closes).
#
# conformance/run.sh is self-contained and idempotent: it builds a fresh
# ocserverd, migrates + serves on an ISOLATED kernel-assigned port (never the officraft
# prod port [the CURRENT default per server/ocserverd/config.go, 7755 as of
# T-a3ba — run.sh derives it from that source instead of hand-listing a number
# here that would go stale the next time the default moves], nor the "vibe"
# product's :8766, nor the e2e :8791; it refuses a prod port and refuses to
# stomp a busy port), runs pytest against a throwaway SQLite, and tears everything down
# on EXIT (kills only captured pids, removes the workdir). The pytest venv is
# created once and reused. A non-zero exit trips set -e and fails CI before the
# "[ci] all green" marker.
#
# run.sh shells out to a BARE `go` (and python3) for the build/serve; under the
# launchd autodeploy's minimal PATH that would be command-not-found, so hand it
# the same resolved go directory this script already found (1), and default
# GOTOOLCHAIN=auto (the pinned toolchain the go modules ask for). Same abspath
# discipline as go/gitleaks/npm above — a missing toolchain FAILS, never skips.
if ! GOTOOLCHAIN="${GOTOOLCHAIN:-auto}" PATH="$(dirname "$GO"):$PATH" \
    "$ROOT/conformance/run.sh" --target go; then
  echo "[ci] FAIL — conformance suite (conformance/run.sh --target go) went red."
  echo "[ci] the frozen wire drifted from live behaviour (manifest/spec/RBAC) or a"
  echo "[ci] behaviour pin broke. Reproduce locally: bash conformance/run.sh --target go"
  exit 1
fi

# The marker line stays BYTE-IDENTICAL and is the FINAL output line. A run is
# green only when BOTH hold — rc == 0 AND `tail -n 1 | grep -qFx '[ci] all green'`.
#
# Neither half is sufficient alone, which is why the rule is an AND rather than
# a pick-one:
#   * a broad grep is worthless (nested suites emit their own "all green", and
#     e2e_test/tests_guard prints its marker in step 0, so ANY blown-up log
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
