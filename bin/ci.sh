#!/usr/bin/env bash
# officraft local CI — the canonical, AUTHORITATIVE quality gate.
#
# CI runs LOCALLY (we do not pay for GitHub Actions). Runs, in order, failing
# fast on the first non-zero step:
#   1. golang            — gofmt + go vet + go build + committed-prebuilt
#                          parity dryrun + go test over EVERY module under
#                          cli/ and server/ (cli/ocwarden ⇒ bin/ocwarden,
#                          cli/ocagent ⇒ bin/ocagent, server/ocserverd ⇒
#                          bin/ocserverd) + gen-ocapi drift gate (committed
#                          ocapi_gen.go tracks spec/openapi.json — the M1
#                          wire-freeze gate on the server's REST surface)
#   2. conformance lint  — the black-box iron rule (HTTP-only suite)
#   3. repo hygiene      — path denylist + gitleaks secret scan (hard gate)
#   4. frontend          — tsc typecheck + vitest (full unit suite) + Playwright
#                          CT visual guards (real-browser layout, T-187c) + schema.ts
#                          drift vs spec/openapi.json (the M1 wire-freeze gate on
#                          the FE contract)
#   5. conformance suite — the full black-box behaviour suite against an isolated
#                          ocserverd (conformance/run.sh --target go): boots a
#                          throwaway server on :8795 + throwaway SQLite, runs the
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
# shim stubs launchctl/tmux/lsof; NO real fleet touched, NO teardown exercised),
# toolchain-free, and fast — so it runs FIRST and reddens CI the instant the
# live-fleet guard or the namespace allocator regresses. A non-zero exit trips
# set -e before "[ci] all green".
echo "[ci] (0) e2e_test isolation-guard unit tests (hermetic)"
if [[ -x "$ROOT/e2e_test/tests_guard/run.sh" ]]; then
  bash "$ROOT/e2e_test/tests_guard/run.sh"
else
  echo "[ci] FAIL — e2e_test/tests_guard/run.sh missing/not executable"
  exit 1
fi

# (0b) bin/ script unit tests (T-33d5) — same hermetic PATH-shim pattern as (0):
# stubs uname/security/codesign, so NO keychain is touched and NOTHING is signed;
# guards the codesign-artifact seam (release-artifact signing must warn-and-pass
# on machines without the identity, never block a build) — plus, since T-da4b,
# the SIGPIPE red/green (a present identity must NEVER read as absent) and the
# sentinel: a BROKEN check hard-fails (exit 3) and OC_CODESIGN_REQUIRE=1 turns a
# missing identity into a hard error (exit 4) instead of a silent adhoc ship.
echo "[ci] (0b) bin script unit tests (hermetic)"
if [[ -x "$ROOT/bin/tests/run.sh" ]]; then
  bash "$ROOT/bin/tests/run.sh"
else
  echo "[ci] FAIL — bin/tests/run.sh missing/not executable"
  exit 1
fi

echo "[ci] (1/5) golang — gofmt + go vet + go build + committed-prebuilt parity + go test (cli/* + server/*)"
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
# a subshell (1a gofmt / 1b vet / 1c build / 1d committed-prebuilt parity /
# 1e go test), the same contract for every golang module in the repo. A non-zero
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
    # 1d. committed-prebuilt parity dryrun — verify the COMMITTED bin/<binary> is
    # functionally in lock-step with the source just compiled in 1c. Every module
    # ships a committed, stripped prebuilt (bin/ocwarden, bin/ocagent,
    # bin/ocserverd) as the go-forward deploy artifact, so CI must PROVE that
    # artifact still tracks the landed source — otherwise a "改了 golang 卻沒重編
    # committed binary" event lands a stale blob that autodeploy would ship. A
    # byte compare is the WRONG test: `go build` is byte-nondeterministic
    # (build-id / path stamping) and 1c omits the committed binary's
    # `-ldflags "-s -w"`, so the bytes legitimately differ. Instead run BOTH the
    # committed prebuilt and the fresh 1c byproduct through a side-effect-free
    # smoke invocation (`--help` → usage text + exit 0; no network, no files,
    # no launchctl) and require identical stdout+stderr+exit. Drift in the CLI
    # surface ⇒ the committed prebuilt is stale ⇒ fail CI.
    # OffiCraft policy: ZERO committed binaries — the repo ships no prebuilt
    # bin/<binary> blobs, so the parity dryrun only applies when a local
    # (gitignored) prebuilt happens to exist; absence is the normal state.
    committed="$ROOT/bin/$binary"
    if [[ ! -x "$committed" ]]; then
      echo "[ci]   (1d) parity dryrun skipped — no prebuilt bin/$binary (zero-committed-binary policy)"
    else
      set +e
      fresh_help="$("$ROOT/$dir/$binary" --help 2>&1)"; fresh_rc=$?
      committed_help="$("$committed" --help 2>&1)"; committed_rc=$?
      set -e
      if [[ "$fresh_help" != "$committed_help" || "$fresh_rc" != "$committed_rc" ]]; then
        echo "[ci] FAIL — local prebuilt bin/$binary is STALE vs source (functional parity dryrun)"
        echo "[ci] rebuild it: (cd $dir && $GO build -ldflags=\"-s -w\" -o \"$committed\" ./...)"
        exit 1
      fi
    fi
    # 1e. go test — RUN the module's unit tests. 1b `go vet` only TYPE-CHECKS
    # *_test.go (compilation), it never executes the assertions; without this
    # gate a broken runtime path would compile clean and ship. A module with no
    # *_test.go reports "no test files" and passes.
    "$GO" test ./...
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
denylist_hits="$(
  git ls-files -z | tr '\0' '\n' | grep -iE \
    -e '(^|/)scratchpad/' \
    -e '\.bak$' \
    -e '\.pem$' \
    -e '\.key$' \
    -e '\.secret$' \
    -e '(^|/)oc\.toml$' \
    -e '(^|/)oc\.lock$' \
    | { grep -vE '\.py$' || true; }
  # `_token` filename rule, source-exempt so it never fires on *.py test files.
  git ls-files -z | tr '\0' '\n' | grep -iE '_token' | grep -vE '\.py$' || true
)"
denylist_hits="$(printf '%s\n' "$denylist_hits" | grep -vE '^$' | sort -u || true)"
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
# ran (the land authority is the "[ci] all green" marker, not exit 0; root
# CLAUDE.md land pipeline + docs/dev/README.md). The launchd autodeploy has a minimal
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
echo "[ci]   playwright CT visual guards (real-browser layout — T-187c)"
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

echo "[ci] (5/5) conformance suite — full black-box behaviour run (isolated ocserverd :8795)"
# ---------------------------------------------------------------------------
# The full HTTP-only conformance suite is the behavioural authority for the
# frozen wire: routes_manifest.json ≡ spec operations, the auth matrix's per
# (route × identity) status derivation, and every REST/MCP/SSE/lifecycle
# semantic pin. Without running it in-gate, a manifest/spec/RBAC drift only
# reddens when someone runs run.sh by hand (the exact hole this step closes).
#
# conformance/run.sh is self-contained and idempotent: it builds a fresh
# ocserverd, migrates + serves on an ISOLATED port (:8795 — never the prod
# :8770/:8766 or the e2e :8791; it refuses a prod port and refuses to stomp a
# busy port), runs pytest against a throwaway SQLite, and tears everything down
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

# The marker line itself stays BYTE-IDENTICAL ("[ci] all green" is the literal
# land authority per CLAUDE.md) — the provenance goes on its own line beside it,
# so even a tailed log pairs the verdict with the tree it was reached on.
echo "[ci] all green"
echo "[ci] green for commit $CI_SHA ($CI_BRANCH, tree $CI_TREE)"
