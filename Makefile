# officraft — ONE NAMED TARGET PER CHECK, and each check's HOW exists exactly once.
#
# WHY THIS FILE EXISTS (T-4d88)
# Every check in this repo used to be written down twice or three times: once in
# bin/ci.sh (which was the land gate until 2026-08-11, when the owner moved the
# merge verdict to the pull request's cloud round — card rc-c16ac4679fab), once
# in bin/ci-cloud.sh (the Linux subset, and Linux itself is gone too) and
# once in bin/ci-macos-host.sh (the macOS-shaped subset). Three copies of one
# rule is how one copy silently loses a clause — measured, not feared: the
# e2e isolation-guard suite ran with its truncation protection in ci.sh and
# WITHOUT it in the cloud, so the one round that guarded everybody else's pull
# request was the weaker one. The same shape produced a gen-ocapi call with a
# PATH prefix on one side and not the other, and a staging step that lived in
# .github/workflows/ci.yml rather than in the subset it was staging for.
#
# So: each check is a NAMED TARGET here, its implementation lives in exactly one
# recipe, and every caller — the local run and every cloud cell — calls the same
# target. A caller can choose WHICH checks to run; it cannot restate HOW.
#
# NAMING IS BY WHAT THE CHECK IS, NEVER BY WHO CALLS IT OR WHERE IT RUNS.
# There is deliberately no `ci-local-*` / `ci-cloud-*` / `macos-*` prefix: that
# is what produced the three drifting copies in the first place. The prefixes
# are the ordinary ones:
#   lint-*   static analysis over the tree as committed (nothing is generated)
#   build-*  produce an artifact something else needs
#   test-*   execute a suite and let its own exit code decide
#   scan-*   hygiene / secret / integrity scans over files
#   drift-*  regenerate a COMMITTED generated artifact and require it to come
#            back byte-identical (the M1 wire freeze and its relatives)
#
# THERE IS DELIBERATELY NO AGGREGATE TARGET (no `make ci`, no `make all`).
# Owner ruling: an aggregate is a second list of what the checks are, and the
# list nobody watches is the one that drifts. Callers name the targets they want.
#
# ORDER IS NOT FREE, and two constraints are load-bearing:
#   * build-embed-assets MUST precede any `go test` — server/ocserverd's tests
#     read the STAGED embed (server/ocserverd/assets_test.go asserts on it by
#     name), and a clean checkout carries .gitkeep-only staging dirs. Declared
#     as a real prerequisite below rather than left to the caller's memory.
#   * bin/ci.sh takes its working-copy lock BEFORE calling anything here.
#     Everything in this file writes in place. bin/tests/ci-lock-guard.sh pins
#     that ordering with a zero-hit scan of ci.sh's prologue.
#
# GNU make 3.81 (what macOS ships, and what the runners have) has no .ONESHELL
# and no .SHELLFLAGS, so every recipe is ONE shell command built with backslash
# continuations and opened with `set -euo pipefail`. Do not "tidy" a recipe into
# separate lines: each line would become its own shell and a failing line in the
# middle would stop failing the target.

SHELL := /bin/bash
ROOT := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

# Every recipe opens with this: fail fast, run from the repo root regardless of
# where make was invoked from, and pull in the single definition of toolchain
# resolution (bin/lib/toolchain.sh — a missing tool is a FAILURE, never a skip).
P = set -euo pipefail; cd "$(ROOT)"; source bin/lib/toolchain.sh;

# EVERY check target ends with this, as the LAST CLAUSE OF ITS RECIPE'S SINGLE
# SHELL COMMAND — never as a separate recipe line.
#
# A zero exit says "nothing failed", not "something ran": a target whose recipe
# is emptied, commented out, or cut short by an early `exit 0` succeeds
# instantly and in complete silence, and no caller can tell that apart from a
# check that ran and passed. Before T-4d88 the cloud macOS cell bought this
# protection by grepping the subset script's final marker out of a tee'd log;
# that script is gone and the marker went with it. This is where it lives now.
#
# Positioning is the whole mechanism. As the tail of the same `;`-joined command
# under `set -e`, anything that stops the recipe early takes this with it. Put it
# on its own recipe line and make would run it in a FRESH shell after an early
# `exit 0` — the marker would print for a check that returned without doing
# anything, which is precisely the failure it exists to catch.
#
# bin/run-checks.sh runs a set of targets and then requires the marker of each
# one it was asked for; .github/workflows/ci.yml calls the checks through it, so
# each cloud cell is held to its OWN targets and no list of "all the checks"
# exists anywhere to drift.
DONE = echo "[oc-check-done] $@"

# The macOS-shaped checks refuse to pretend on another platform rather than
# reporting a green that means nothing. This is a helper, not a check category.
REQUIRE_DARWIN = [[ "$$(uname -s)" == "Darwin" ]] || { echo "FAIL — this check is macOS-shaped; refusing to pretend on $$(uname -s)" >&2; exit 1; };

# One regenerate-and-byte-compare gate, parameterised: $(1) label, $(2) npm
# script, $(3) and $(4) the two COMMITTED generated files. The generator writes
# IN PLACE, so the committed bytes are snapshotted first and RESTORED before
# failing — a red must not leave a mutated worktree behind. One definition
# instead of three near-identical blocks, which is exactly how one of them
# silently loses its restore during a later edit.
REGEN_PAIR_GATE = $(P) \
	NPM="$$(oc_npm)"; \
	a="$(3)"; b="$(4)"; \
	bak_a="$$(mktemp -t oc-regen-a.XXXXXX)"; bak_b="$$(mktemp -t oc-regen-b.XXXXXX)"; \
	cp "$$a" "$$bak_a"; cp "$$b" "$$bak_b"; \
	(cd frontend && "$$NPM" run --silent $(2) >/dev/null); \
	if ! diff -u "$$bak_a" "$$a" || ! diff -u "$$bak_b" "$$b"; then \
	  echo "FAIL — $(1) drift: the generated files are STALE vs their source."; \
	  echo "regenerate + commit: (cd frontend && npm run $(2)) then git add both generated files"; \
	  cp "$$bak_a" "$$a"; cp "$$bak_b" "$$b"; \
	  rm -f "$$bak_a" "$$bak_b"; \
	  exit 1; \
	fi; \
	rm -f "$$bak_a" "$$bak_b"

.PHONY: \
  lint-go-naming lint-go-fmt lint-go-vet lint-uplink-contract lint-effort-vocab \
  lint-shadow-claim \
  lint-conformance-blackbox lint-ts lint-css-tokens lint-css-token-roles \
  build-embed-assets build-go build-frontend-deps \
  test-e2e-isolation-guard test-bin-guards test-go test-system-interaction-examples \
  test-frontend-unit \
  test-frontend-ct test-conformance \
  scan-tracked-paths scan-secrets scan-tcc-anchor \
  drift-ocapi drift-schema-ts drift-theme-tokens drift-message-keys drift-fonts \
  drift-mcp-catalog

# ===========================================================================
# build
# ===========================================================================

# Stage the embed assets (T-e731). seeds/*.md, docs/guide, spec/mcp-catalog.json
# and the prebuilt ocwarden/ocagent are served EMBED-ONLY (no disk fallback), and
# a clean checkout carries .gitkeep-only seedsdist/docsdist/bindist — so anything
# that boots or builds ocserverd reads an EMPTY embed unless this ran first.
# build-bindist compiles ocwarden/ocagent with a bare `go`, hence the PATH prefix.
build-embed-assets:
	@$(P) \
	GO="$$(oc_go)"; \
	echo "[build-embed-assets] staging seedsdist + docsdist + bindist"; \
	PATH="$$(dirname "$$GO"):$$PATH" bash bin/build-seedsdist; \
	PATH="$$(dirname "$$GO"):$$PATH" bash bin/build-docsdist; \
	PATH="$$(dirname "$$GO"):$$PATH" bash bin/build-bindist; \
	$(DONE)

# Compile every module and DROP the fresh binary (gitignored). Nothing else in
# the deploy pipeline compiles the Go modules on its own, so without this a
# change could land — and autodeploy — while failing to compile.
build-go:
	@$(P) \
	GO="$$(oc_go)"; \
	for gomod in cli/*/go.mod server/*/go.mod; do \
	  [[ -f "$$gomod" ]] || continue; \
	  dir="$$(dirname "$$gomod")"; binary="$$(basename "$$dir")"; \
	  echo "[build-go] go build $$dir"; \
	  (cd "$$dir" && "$$GO" build -o "$$binary" ./...); \
	done; \
	$(DONE)

# npm ci is its own target because five other targets need node_modules and none
# of them should be the one that happens to install it.
build-frontend-deps:
	@$(P) \
	NPM="$$(oc_npm)"; \
	[[ -f frontend/package.json ]] || { echo "FAIL — frontend/package.json missing" >&2; exit 1; }; \
	echo "[build-frontend-deps] npm ci (frontend)"; \
	(cd frontend && "$$NPM" ci --silent); \
	$(DONE)

# ===========================================================================
# lint
# ===========================================================================

# Naming invariant (root CLAUDE.md §10 folder = module = binary). THREE names,
# so this needs THREE INDEPENDENT SOURCES or it proves nothing. Folder basename
# and go.mod's `module` line are two of them. The third — the name the shipped
# executable actually gets — lives ONLY in the build scripts' `-o` flags
# (bin/build-bindist for the three cli/ modules, bin/build for ocserverd). That
# is what deploys, nothing ties it to the folder name, and it drifts freely.
# Until T-ac67 the first clause read `binary="$$base"` and then compared $$base
# against $$binary: a variable against itself, structurally incapable of being
# false. Renaming a `-o` target passed silently.
#
# DELIBERATELY NOT A TABLE HERE. Hardcoding a module→binary map in this Makefile
# would be a FOURTH copy of the name, i.e. one more thing that can drift; the
# build scripts are read because they are what actually decides the name.
#
# A module with NO matching `go build … -o` line is a FAIL, never a skip, so
# that DELETING a build line is caught rather than silently skipped. Deletion is
# the whole of what that clause guarantees — see the range limits below, and do
# not restate it more broadly than this.
#
# MATCH SHAPE. Backslash continuations are folded first (bin/build's ocserverd
# build spans three physical lines), whole-line comments are dropped and
# trailing `#` comments are stripped; a line then counts only if it carries ALL
# THREE of the module's own `cd "$$ROOT/<dir>"`, `go build`, and a
# double-quoted `-o "…"`. The trailing-comment strip happens BEFORE the `-o` is
# read because the extractor deliberately takes the LAST `-o` on the line (which
# is the one `go build` itself honours). Without the strip, a correct line
# followed by `# historic: -o "xOLD"` reported 'xOLD' and went red on a good
# script, and the mirror image (`-o "xOLD" … # -o "x"`) would have gone false
# GREEN on a bad one.
#
# 🔴 RANGE LIMITS. Keep this list honest: a comment here claiming protection
# that does not exist is worse than no comment, because the next reader skips
# the check on the strength of it.
#   * This is a TEXTUAL scan, NOT an execution trace. A line merely CONTAINING
#     the three markers satisfies the match even if it never runs — swapping the
#     real build for `echo 'cd "$$ROOT/cli/ocwarden" && go build -o "…"'
#     >/dev/null` passes while that module is never built at all. So: deleting
#     the build line is caught; RESHAPING it into inert text is NOT.
#   * The mirror failure: a CORRECT build line written differently is not
#     recognised and goes red. Unquoted or single-quoted `-o`, `go build -C
#     <dir>`, or the `cd` on its own line all read as "missing". That is a false
#     red from this matcher — widen the matcher here, never reshape a working
#     build script to satisfy it.
#   * Variables are NOT expanded, and that case lands in the OTHER branch:
#     `OUT="…"; … -o "$$OUT"` matches, so it is reported as a name MISMATCH
#     (`builds it as '$$OUT'`), never as "missing" — so the "widen the matcher"
#     advice on the missing branch is not shown to whoever hits it.
#   * The trailing-comment strip costs both directions. It can cut a line short
#     at the first whitespace-then-`#` even when that `#` is inside a quoted
#     value, and what follows the cut is discarded: if the discarded tail held a
#     LATER `-o`, the check reads the earlier one. So the strip turns
#     good-`-o` … quoted-`#` … bad-`-o` on one folded line from RED into GREEN,
#     as well as truncating some lines into a (red) "missing".
#   * Only bin/build-bindist and bin/build are read (hardcoded below), and only
#     the BASENAME of `-o` is compared, so the destination directory is not
#     checked at all.
lint-go-naming:
	@$(P) \
	scripts="bin/build-bindist bin/build"; \
	for gomod in cli/*/go.mod server/*/go.mod; do \
	  [[ -f "$$gomod" ]] || continue; \
	  dir="$$(dirname "$$gomod")"; base="$$(basename "$$dir")"; \
	  echo "[lint-go-naming] $$dir"; \
	  needle="cd \"\$$ROOT/$$dir\""; \
	  found=0; \
	  for s in $$scripts; do \
	    if [[ ! -f "$$s" ]]; then \
	      echo "FAIL — naming (CLAUDE.md 10): build script '$$s' is missing, so the produced-executable name for $$dir cannot be read"; exit 1; \
	    fi; \
	    outs="$$(awk '/\\$$/ { sub(/\\$$/,""); buf = buf $$0; next } { print buf $$0; buf = "" }' "$$s" \
	      | grep -v '^[[:space:]]*#' \
	      | sed -E 's/[[:space:]]#.*$$//' \
	      | grep -F "$$needle" \
	      | grep -F 'go build' \
	      | sed -nE 's/.*[[:space:]]-o[[:space:]]+"([^"]*)".*/\1/p' || true)"; \
	    for out in $$outs; do \
	      found=1; obin="$$(basename "$$out")"; \
	      if [[ "$$base" != "$$obin" ]]; then \
	        echo "FAIL — naming (CLAUDE.md 10): module $$dir has folder name '$$base' but $$s builds it as '$$obin' (-o \"$$out\") — folder, go.mod module and produced executable must all be the same name"; exit 1; \
	      fi; \
	    done; \
	  done; \
	  if [[ "$$found" != 1 ]]; then \
	    echo "FAIL — naming (CLAUDE.md 10): module $$dir (folder '$$base') — no build line found, so the produced executable name is unknowable and folder=module=binary cannot be checked."; \
	    echo "  Searched: $$scripts"; \
	    echo "  Wanted: ONE line carrying ALL THREE of  cd \"\$$ROOT/$$dir\"  +  go build  +  a DOUBLE-QUOTED  -o \"…\"  (after backslash-continuations are folded, whole-line comments dropped and trailing '#' comments stripped)."; \
	    echo "  If the build line was DELETED: restore it. Deletion is what this clause catches."; \
	    echo "  If your build line is CORRECT but written differently — unquoted or single-quoted -o, 'go build -C $$dir', or the cd on its own line — then this is a RANGE LIMIT of this matcher, not a fault in your script: widen the matcher in this Makefile target, do NOT reshape the build script to satisfy it."; \
	    echo "  NOT caught by this clause: text that merely LOOKS like the build line. A line containing those three markers satisfies the match even if it never executes, so replacing the real build with inert text (echo/quoted/dead code) passes silently. This is a textual scan, not an execution trace."; \
	    exit 1; \
	  fi; \
	  if ! grep -qE "^module $${base}\$$" "$$dir/go.mod"; then \
	    echo "FAIL — naming (CLAUDE.md 10): $$dir/go.mod 'module' line is not 'module $$base'"; exit 1; \
	  fi; \
	done; \
	$(DONE)

# gofmt -l lists any unformatted file; non-empty = fail. testdata/ holds no *.go,
# so a plain recursive scan of "." is safe.
lint-go-fmt:
	@$(P) \
	GOFMT="$$(oc_gofmt)"; \
	for gomod in cli/*/go.mod server/*/go.mod; do \
	  [[ -f "$$gomod" ]] || continue; \
	  dir="$$(dirname "$$gomod")"; \
	  echo "[lint-go-fmt] $$dir"; \
	  unformatted="$$(cd "$$dir" && "$$GOFMT" -l . 2>/dev/null || true)"; \
	  if [[ -n "$$unformatted" ]]; then \
	    echo "FAIL — gofmt: unformatted golang files in $$dir:"; \
	    printf '  %s\n' $$unformatted; \
	    echo "fix with: gofmt -w $$dir"; \
	    exit 1; \
	  fi; \
	done; \
	$(DONE)

# go vet type-checks *_test.go too, so this covers test-file compilation that
# `go build ./...` (non-test only) would miss.
lint-go-vet:
	@$(P) \
	GO="$$(oc_go)"; \
	for gomod in cli/*/go.mod server/*/go.mod; do \
	  [[ -f "$$gomod" ]] || continue; \
	  dir="$$(dirname "$$gomod")"; \
	  echo "[lint-go-vet] $$dir"; \
	  (cd "$$dir" && "$$GO" vet ./...); \
	done; \
	$(DONE)

# Client-payload contract gate (T-9c8d) plus its own positive control. Both
# halves move together, always: the selftest is what proves the scanner still
# bites, and a scanner nobody verified is a green with a hole in it.
lint-uplink-contract:
	@$(P) \
	echo "[lint-uplink-contract] every CLI send is declared, spec-checked and wire-tested"; \
	python3 bin/uplink-guard.py; \
	python3 bin/tests/uplink-guard-selftest.py; \
	$(DONE)

# Effort-vocabulary contract gate (T-dbd4) plus its positive control, same shape
# and same reason as the pair above.
lint-effort-vocab:
	@$(P) \
	echo "[lint-effort-vocab] every hand-written copy lists exactly what the server enforces"; \
	python3 bin/effort-vocab-guard.py; \
	python3 bin/tests/effort-vocab-guard-selftest.py; \
	$(DONE)

# Shadow-claim contract gate (T-941e) plus its control, same shape and same
# reason as the pair above: the promise "--no-reconcile covers every warden
# command" was false for as long as it was written down, and nothing went red.
lint-shadow-claim:
	@$(P) \
	echo "[lint-shadow-claim] no sentence may promise a coverage the flag does not have"; \
	python3 bin/shadow-claim-guard.py; \
	python3 bin/tests/shadow-claim-guard-selftest.py; \
	$(DONE)

# The conformance suite is the language-agnostic black-box definition of the
# wire; the moment its test code imports an implementation module it stops being
# implementation-neutral. Static and fast, so it reddens without waiting on the
# ~16s server boot that test-conformance pays.
lint-conformance-blackbox:
	@$(P) \
	echo "[lint-conformance-blackbox] conformance/ must import no server-implementation module"; \
	if [[ -d conformance ]]; then \
	  hits="$$(grep -RInE --include='*.py' '^[[:space:]]*(import|from)[[:space:]]+(backend|service|dal|domain|plumbing)([.[:space:]]|$$)' conformance || true)"; \
	  if [[ -n "$$hits" ]]; then \
	    echo "FAIL — conformance black-box violation (suite must stay HTTP-only):"; \
	    printf '  %s\n' "$$hits"; \
	    echo "conformance tests speak ONLY HTTP to \$$OC_TARGET_URL (see conformance/CLAUDE.md)."; \
	    exit 1; \
	  fi; \
	fi; \
	$(DONE)

# The SECOND line of contract-drift defence: Wire* re-exports the generated
# OpenAPI schema, so a DTO change surfaces as a tsc error even if drift-schema-ts
# somehow missed it.
lint-ts: build-frontend-deps
	@$(P) \
	NPM="$$(oc_npm)"; \
	echo "[lint-ts] tsc --noEmit (frontend typecheck)"; \
	(cd frontend && "$$NPM" run --silent typecheck); \
	$(DONE)

# A raw colour literal outside theme.css is invisible to the theme switch and to
# user-defined themes — exactly how a new theme sprouts an un-restyled patch.
lint-css-tokens: build-frontend-deps
	@$(P) \
	NPM="$$(oc_npm)"; \
	echo "[lint-css-tokens] no raw colour literals outside theme.css (T-16a1)"; \
	(cd frontend && "$$NPM" run --silent lint:tokens); \
	$(DONE)

# Three tokens each used to carry two semantically opposite jobs; T-081b split
# them. A re-merge is INVISIBLE in the dark theme and breaks only light-theme
# users, so it has to fail here instead.
lint-css-token-roles: build-frontend-deps
	@$(P) \
	NPM="$$(oc_npm)"; \
	echo "[lint-css-token-roles] the T-081b splits stay split"; \
	(cd frontend && "$$NPM" run --silent lint:token-roles); \
	$(DONE)

# ===========================================================================
# test
# ===========================================================================

# The hermetic safety layer that keeps the DESTRUCTIVE e2e suites from wiping a
# live agent-fleet host.
#
# rc IS NOT ENOUGH here. That suite has no per-file discovery, so truncating it —
# deleting its tail, including the PASS floor that is supposed to notice
# truncation — leaves a script that exits 0 having asserted almost nothing.
# MEASURED on b8c3805 (floor block deleted, trailing echo kept): PASS=153 FAIL=0
# rc=0, last line still the marker, whole gate green. rc and the marker both saw
# NOTHING. So: rc == 0, AND the last line equals the marker, AND the floor is
# still statically present. The static assertion lives HERE rather than in the
# guard for the obvious reason: a check that a file must contain X is worthless
# if it lives in that file.
#
# ⚠️ This is the check that used to exist in two strengths — ci.sh had all three
# clauses, the cloud round had only rc. The cloud round is the one that guarded
# everybody else's pull request. One definition, the strong one.
test-e2e-isolation-guard:
	@$(P) \
	echo "[test-e2e-isolation-guard] e2e_test isolation-guard unit tests (hermetic)"; \
	tg=e2e_test/tests_guard/run.sh; \
	[[ -x "$$tg" ]] || { echo "FAIL — $$tg missing or not executable (renamed? then this check stopped running)"; exit 1; }; \
	if ! grep -qE '^PASS_FLOOR=[0-9]+$$' "$$tg" || ! grep -qF '"$$PASS" -lt "$$PASS_FLOOR"' "$$tg"; then \
	  echo "FAIL — $$tg has no PASS floor any more."; \
	  echo "That suite has no per-file discovery: delete a case block and it still exits 0"; \
	  echo "with a smaller PASS count. The floor is the only thing that notices, and the"; \
	  echo "success marker is echoed from its passing branch — so removing the floor while"; \
	  echo "leaving a bare marker echo behind would go green on rc and on the marker alike."; \
	  echo "Restore the floor, do not delete this assertion."; \
	  exit 1; \
	fi; \
	log="$$(mktemp -t oc-tests-guard.XXXXXX)"; \
	bash "$$tg" 2>&1 | tee "$$log"; \
	if ! tail -n 1 "$$log" | grep -qFx '[tests_guard] all green'; then \
	  echo "FAIL — tests_guard exited 0 but its last line is not '[tests_guard] all green'."; \
	  echo "A green rc with the marker missing means the suite was truncated."; \
	  tail -n 3 "$$log"; \
	  rm -f "$$log"; \
	  exit 1; \
	fi; \
	rm -f "$$log"; \
	$(DONE)

# The dispatcher for the bin/ guard suites (hermetic PATH shims: no release is
# created and no station is contacted). Its own cell in the cloud on owner's
# ruling — it is not part of the ordinary CI classes and its reds should be
# readable as themselves. macOS-shaped: its Linux reds were BSD/GNU `mktemp -t`
# semantics and macOS-shaped install.sh fixtures.
test-bin-guards:
	@$(P) $(REQUIRE_DARWIN) \
	echo "[test-bin-guards] bin/tests/run.sh"; \
	[[ -x bin/tests/run.sh ]] || { echo "FAIL — bin/tests/run.sh missing or not executable (renamed? then this check stopped running)"; exit 1; }; \
	bash bin/tests/run.sh; \
	$(DONE)

# -count=1 is the documented way to DEFEAT go's test-result cache and it is
# load-bearing for the whole meaning of this check (T-bedc): without it go
# replays a previous PASS whenever the package's inputs hash the same, which
# certifies a run that DID NOT HAPPEN and structurally hides flakes.
# bin/tests/go-test-nocache-guard.sh pins this flag.
test-go: build-embed-assets
	@$(P) \
	GO="$$(oc_go)"; \
	for gomod in cli/*/go.mod server/*/go.mod; do \
	  [[ -f "$$gomod" ]] || continue; \
	  dir="$$(dirname "$$gomod")"; \
	  echo "[test-go] go test -count=1 $$dir"; \
	  (cd "$$dir" && "$$GO" test -count=1 ./...); \
	done; \
	$(DONE)

test-system-interaction-examples:
	@$(P) \
	GO="$$(oc_go)"; \
	echo "[test-system-interaction-examples] system_interaction.md MCP/CLI examples"; \
	OC_GO="$$GO" bash bin/tests/system-interaction-examples.sh; \
	$(DONE)

# vitest runs in jsdom, which applies no layout engine — see test-frontend-ct for
# the half this one is structurally blind to.
test-frontend-unit: build-frontend-deps
	@$(P) \
	NPM="$$(oc_npm)"; \
	echo "[test-frontend-unit] vitest run (frontend unit suite)"; \
	(cd frontend && "$$NPM" run --silent test); \
	$(DONE)

# `test:ct` is TWO Playwright configs: the CT visual guards against a dev server,
# then the paint guards against a REAL `vite build` output served over HTTP. The
# build is part of the check, not setup.
#
# Browser resolution: point Playwright at the machine's shared cache explicitly
# rather than relying on default discovery (minimal-PATH callers). The install
# probe's `|| true` keeps an offline run from failing on the probe; a genuinely
# absent browser then fails the test run itself — never a silent skip.
#
# If a CT guard reddens on a runner and is green on a dev Mac, the fix is the
# font environment, NEVER a looser threshold.
test-frontend-ct: build-frontend-deps
	@$(P) $(REQUIRE_DARWIN) \
	NPM="$$(oc_npm)"; \
	echo "[test-frontend-ct] real-browser CT layout guards + paint guards"; \
	export PLAYWRIGHT_BROWSERS_PATH="$${PLAYWRIGHT_BROWSERS_PATH:-$$HOME/Library/Caches/ms-playwright}"; \
	(cd frontend && npx --no-install playwright install chromium >/dev/null 2>&1 || true); \
	(cd frontend && "$$NPM" run --silent test:ct); \
	$(DONE)

# The full HTTP-only behaviour suite: boots a throwaway ocserverd on a
# kernel-assigned port against a throwaway SQLite, runs the suite, tears down.
# It builds ocserverd from source, so it needs the staged embed too.
# run.sh shells out to a BARE `go`, hence the PATH prefix and GOTOOLCHAIN.
test-conformance: build-embed-assets
	@$(P) \
	GO="$$(oc_go)"; \
	echo "[test-conformance] full black-box behaviour suite (isolated ocserverd)"; \
	if ! GOTOOLCHAIN="$${GOTOOLCHAIN:-auto}" PATH="$$(dirname "$$GO"):$$PATH" conformance/run.sh --target go; then \
	  echo "FAIL — conformance suite went red: the frozen wire drifted from live behaviour"; \
	  echo "(manifest/spec/RBAC) or a behaviour pin broke."; \
	  echo "Reproduce: bash conformance/run.sh --target go"; \
	  exit 1; \
	fi; \
	$(DONE)

# ===========================================================================
# scan
# ===========================================================================

# A HARD gate over TRACKED files. .gitignore already excludes these, but a
# `git add -f` or an edited .gitignore can slip junk in; this re-checks what is
# ACTUALLY tracked, independent of .gitignore.
scan-tracked-paths:
	@$(P) \
	source bin/lib/tracked-path-denylist.sh; \
	echo "[scan-tracked-paths] tracked-file path denylist"; \
	hits="$$(tracked_path_denylist_hits)"; \
	if [[ -n "$$hits" ]]; then \
	  echo "FAIL — forbidden files are tracked (path denylist):"; \
	  printf '  %s\n' $$hits; \
	  echo "remove with: git rm --cached <file>   (and confirm .gitignore covers it)"; \
	  exit 1; \
	fi; \
	$(DONE)

# The other half of hygiene: file CONTENTS. `dir` scans the tree, --config pins
# the allowlist policy. A missing .gitleaks.toml is a refusal, not a default-rule
# scan reported as a pass.
scan-secrets:
	@$(P) $(REQUIRE_DARWIN) \
	GITLEAKS="$$(oc_gitleaks)"; \
	[[ -f .gitleaks.toml ]] || { echo "FAIL — .gitleaks.toml missing — refusing to scan with default rules and call it a pass" >&2; exit 1; }; \
	echo "[scan-secrets] gitleaks content scan"; \
	"$$GITLEAKS" dir . --no-banner --config .gitleaks.toml; \
	$(DONE)

# The one owner-approved committed binary (the TCC identity anchor). Its manifest
# binds a reviewable source snapshot to the checked-in executable, so this fails
# closed whenever the source moves without an explicit binary refresh. Its own
# cell in the cloud on owner's ruling.
scan-tcc-anchor:
	@$(P) $(REQUIRE_DARWIN) \
	echo "[scan-tcc-anchor] bin/check-officraft-dist"; \
	[[ -x bin/check-officraft-dist ]] || { echo "FAIL — bin/check-officraft-dist missing or not executable (renamed? then this check stopped running)"; exit 1; }; \
	bin/check-officraft-dist; \
	$(DONE)

# ===========================================================================
# drift — regenerate a committed artifact, require it back byte-identical
# ===========================================================================
#
# ⚠️ THIS CLASS IS THE TOOLCHAIN-SENSITIVE ONE. Every gate here asserts
# "regenerating produces byte-identical output", so a generator that behaves
# differently on a different Node/Go build reddens while the CODE IS FINE. That
# is why the workflow pins go + node to the dev machine's exact versions; loosen
# those pins and this class is where it breaks first.

# The wire-freeze gate on the SERVER's REST surface. ocapi_gen.go is a COMMITTED
# generated artifact; a spec change landed without re-running bin/gen-ocapi (or a
# hand-edit of the generated file) compiles fine and ships a drifted wire.
# Regenerate to a temp file — the committed file is never touched.
# The .go suffix on the temp file and the PATH prefix were one-per-copy before
# T-4d88; this keeps both.
drift-ocapi:
	@$(P) \
	GO="$$(oc_go)"; \
	echo "[drift-ocapi] regenerate ocapi_gen.go from spec/openapi.json + diff committed"; \
	fresh="$$(mktemp -t oc-fresh-ocapi.XXXXXX.go)"; \
	PATH="$$(dirname "$$GO"):$$PATH" bin/gen-ocapi "$$fresh" >/dev/null; \
	if ! diff -u server/ocserverd/ocapi_gen.go "$$fresh"; then \
	  echo "FAIL — gen-ocapi drift: server/ocserverd/ocapi_gen.go is STALE vs spec/openapi.json."; \
	  echo "wire 已凍結 (M1): spec-first — if the spec change IS approved, regenerate + commit:"; \
	  echo "  bash bin/gen-ocapi && git add server/ocserverd/ocapi_gen.go"; \
	  rm -f "$$fresh"; \
	  exit 1; \
	fi; \
	rm -f "$$fresh"; \
	$(DONE)

# The wire-freeze gate on the MCP tool surface. spec/mcp-catalog.json is what
# ocserverd serves verbatim from tools/list, and it was HAND-maintained until
# T-2590 — spec/mcp.md §5 said so in one paragraph while §4 required the catalog
# to be derived, which is two rules for one file and therefore no rule at all.
# It is now a COMMITTED GENERATED artifact: bin/gen-mcp-catalog renders it from
# the x-mcp metadata on each spec/openapi.json operation, and this gate requires
# the render to come back BYTE-IDENTICAL to the committed bytes.
#
# 🔴 THIS IS NOT THE SAME CHECK AS bin/tests/mcp-catalog-generator.sh, and
# neither replaces the other. That guard drives the GENERATOR against mutated
# inputs (does it still refuse a lie?); this gate asserts the COMMITTED FILE has
# not drifted from its source. A generator that is provably correct still leaves
# a stale catalog on disk if nobody re-runs it, and that stale file is what the
# wire serves.
# Regenerate to a temp file — the committed file is never touched.
drift-mcp-catalog:
	@$(P) \
	echo "[drift-mcp-catalog] regenerate spec/mcp-catalog.json from spec/openapi.json x-mcp + diff committed"; \
	fresh="$$(mktemp -t oc-fresh-mcp-catalog.XXXXXX.json)"; \
	bin/gen-mcp-catalog "$$fresh" >/dev/null; \
	if ! diff -u spec/mcp-catalog.json "$$fresh"; then \
	  echo "FAIL — gen-mcp-catalog drift: spec/mcp-catalog.json is STALE vs spec/openapi.json."; \
	  echo "wire 已凍結 (M1): the MCP tool surface is spec-first — if the spec change IS approved, regenerate + commit:"; \
	  echo "  bin/gen-mcp-catalog && git add spec/mcp-catalog.json"; \
	  rm -f "$$fresh"; \
	  exit 1; \
	fi; \
	rm -f "$$fresh"; \
	$(DONE)

# The wire-freeze gate on the CLIENT surface: feed the FROZEN spec through the
# SAME generator the committed schema was made with.
drift-schema-ts: build-frontend-deps
	@$(P) \
	echo "[drift-schema-ts] regenerate schema.ts from spec/openapi.json + diff committed"; \
	fresh="$$(mktemp -t oc-fresh-schema.XXXXXX)"; \
	(cd frontend && npx --no-install openapi-typescript "$(ROOT)/spec/openapi.json" -o "$$fresh"); \
	if ! diff -u frontend/src/api/generated/schema.ts "$$fresh"; then \
	  echo "FAIL — contract drift: frontend/src/api/generated/schema.ts is STALE vs spec/openapi.json."; \
	  echo "regenerate + commit: (cd frontend && npm run gen:api)"; \
	  rm -f "$$fresh"; \
	  exit 1; \
	fi; \
	rm -f "$$fresh"; \
	$(DONE)

# styles/theme.css is the single token contract; the user-theme validators read a
# GENERATED whitelist of its --color-* names (T-16a1 P2).
drift-theme-tokens: build-frontend-deps
	@echo "[drift-theme-tokens] regenerate from theme.css + diff committed (T-16a1 P2)"
	@$(call REGEN_PAIR_GATE,theme-token whitelist,gen:tokens,frontend/src/styles/themeTokens.generated.ts,server/ocserverd/theme_colornames_gen.go); \
	$(DONE)

# locales/en.ts is the single message-code contract; the wording-overlay
# validators read a GENERATED whitelist of its leaf-string key paths (T-16a1 P3).
drift-message-keys: build-frontend-deps
	@echo "[drift-message-keys] regenerate from locales/en.ts + diff committed (T-16a1 P3)"
	@$(call REGEN_PAIR_GATE,message-key whitelist,gen:msgkeys,frontend/src/i18n/messageKeys.generated.ts,server/ocserverd/message_keys_gen.go); \
	$(DONE)

# themeFonts.source.json is the single font contract; the theme-bundle `fonts`
# validators read a GENERATED whitelist of its --font-* names and its closed
# safe-family stack set (T-16a1 P4).
drift-fonts: build-frontend-deps
	@echo "[drift-fonts] regenerate from themeFonts.source.json + diff committed (T-16a1 P4)"
	@$(call REGEN_PAIR_GATE,font whitelist,gen:fonts,frontend/src/styles/themeFonts.generated.ts,server/ocserverd/theme_fonts_gen.go); \
	$(DONE)
