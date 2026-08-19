#!/usr/bin/env bash
# bin/tests/run.sh — the DISPATCHER for the bin/ hermetic guard suites.
#
# It owns no test cases of its own any more. Each guard below is a self-contained
# suite with its OWN PATH shim, its OWN tempdir/HOME and its OWN fixtures (the
# same bats-free pattern as e2e_test/tests_guard/run.sh), and this file's job is
# to run every one of them under a wall-clock ceiling, account the results into
# one PASS/FAIL tally, and exit non-zero if anything failed. bin/ci.sh step 0b
# dispatches exactly this file, so a guard that stops being listed here stops
# being gated — adding a guard means adding a block below, not just a file.
#
# HISTORY, so nobody goes looking for what is gone: until T-0398 this file was
# ALSO the hermetic unit-test suite for bin/codesign-artifact and
# bin/setup-codesign-cert (a uname/security/codesign PATH shim, a tripwire per
# forbidden invocation, and static drift-guards on which scripts set
# OC_CODESIGN_*). All of it was deleted with the signing machinery itself
# (owner ruling 2026-07-31: remove code signing entirely, manual escape hatch
# included). The dispatch half of the file — including bin/tests/release-guard.sh,
# which guards the pre-build CI gate and the post-upload READ-BACK of a release,
# and has nothing to do with signing — was deliberately kept.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

# ── process hygiene (T-1a54) ─────────────────────────────────────────────────
# Every guard is dispatched through run_bounded.py, which runs it in its own
# session/process group under a wall-clock ceiling and group-kills the whole
# subtree on timeout. A mutant a guard runs that busy-loops therefore dies at
# GUARD_TIMEOUT instead of burning a core forever, and the framework reaps its
# own children when it exits or is interrupted (EXIT/INT/TERM) — no orphans.
LIB_RUN_BOUNDED="$HERE/lib/run_bounded.py"
GUARD_TIMEOUT="${OC_GUARD_TIMEOUT:-300}"   # per-guard ceiling; normal runs finish in seconds
RB_PID=""
_framework_cleanup() {
  # Forward to the in-flight bounded guard, if any. run_bounded detached it into
  # its own session and group-kills the whole subtree on TERM, so this collects
  # everything the guard spawned rather than only the direct child.
  if [[ -n "$RB_PID" ]] && kill -0 "$RB_PID" 2>/dev/null; then
    kill -TERM "$RB_PID" 2>/dev/null
    for _ in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$RB_PID" 2>/dev/null || break; sleep 0.2; done
    kill -KILL "$RB_PID" 2>/dev/null || true
  fi
}
trap '_framework_cleanup' EXIT
trap '_framework_cleanup; exit 130' INT
trap '_framework_cleanup; exit 143' TERM

run_guard() { # run_guard PATH-to-guard — returns the guard's rc (124 if it timed out)
  python3 "$LIB_RUN_BOUNDED" "$GUARD_TIMEOUT" bash "$1" &
  RB_PID=$!
  wait "$RB_PID"; local rc=$?
  RB_PID=""
  return "$rc"
}

# ── bin/install.sh live-service gate (T-eefc) ────────────────────────────────
# Own file, own PATH shim (launchctl/lsof/uname) and own temp HOME, so it cannot
# share or disturb any other suite's fixtures. Its exit code is folded in here rather
# than left to a human to run — the thing it guards is an OUTAGE of the live
# station, which is precisely the class of regression nobody re-tests by hand.
GUARD="$HERE/install-guard.sh"
echo
if [[ -f "$GUARD" ]]; then
  if run_guard "$GUARD"; then
    ok "install.sh live-service gate suite passed"
  else
    bad "install.sh live-service gate suite FAILED (see output above)"
  fi
else
  bad "bin/tests/install-guard.sh is missing"
fi

# ── default-port contract (oc.toml.example ↔ bin/ocserver render guard) ─────
# Own file, own tempdir. The template and render_oc_toml's literal guard are a
# contract with no compiler behind it: drift in either direction is invisible
# until an install detonates at render time. Folded in here so it reddens CI.
PORTS="$HERE/port-default.sh"
echo
if [[ -f "$PORTS" ]]; then
  if run_guard "$PORTS"; then
    ok "default-port contract suite passed"
  else
    bad "default-port contract suite FAILED (see output above)"
  fi
else
  bad "bin/tests/port-default.sh is missing"
fi

# ── SSE station-sha header: one contract spelled in two modules (T-5b83) ─────
# server/ocserverd and cli/ocagent cannot import each other, so the header name
# exists twice. A mismatch is SILENT — Header.Get returns "" and the connection
# line just omits the sha, which is byte-identical to the honest "this station
# sent none". Nothing else in the tree can tell those two apart.
STATIONSHA="$HERE/station-sha-header-guard.sh"
echo
if [[ -f "$STATIONSHA" ]]; then
  if run_guard "$STATIONSHA"; then
    ok "station-sha header contract suite passed"
  else
    bad "station-sha header contract suite FAILED (see output above)"
  fi
else
  bad "bin/tests/station-sha-header-guard.sh is missing"
fi

# ── serve-plist runtime stamps: claude + codex, both installers (T-ba62/T-ff48) ─
# Own file, own tempdir, same PATH-shim discipline. The stamp is what carries
# PATH/OC_CLAUDE_BIN/OC_CODEX_BIN from the operator's interactive shell into the
# serve plist, and from there (via bootstrap-here's env passthrough) into
# `ocwarden install`. Losing it means the host installs a warden that cannot
# resolve that runtime — the silent failure T-ba62 closed for claude on the
# one-click path, and T-ff48 closed for codex on both paths. The suite covers
# bin/install.sh hermetically and bin/ocserver through its render-serve-plist
# seam; its central case asserts BOTH runtimes are rescued in the same host
# situation, because a codex-only assertion goes green on a change that fixes
# codex by breaking claude.
CLAUDESTAMP="$HERE/install-claude-stamp.sh"
echo
if [[ -f "$CLAUDESTAMP" ]]; then
  if run_guard "$CLAUDESTAMP"; then
    ok "serve-plist runtime stamp suite passed"
  else
    bad "serve-plist runtime stamp suite FAILED (see output above)"
  fi
else
  bad "bin/tests/install-claude-stamp.sh is missing"
fi

# ── install.sh tool preflight (T-7f38) ──────────────────────────────────────
# Own file, own PATH shim, own temp HOME. It guards the fail-closed gate that
# stops an install on a machine missing tmux or claude — the state where the
# install goes green and every member then sits at 「waking」 with nothing naming
# the cause. Its own counterfactual is inside the suite: the same run against a
# mutant install.sh with the preflight call deleted must SUCCEED, so a gate that
# stops firing reddens here instead of quietly passing.
PREFLIGHT="$HERE/install-preflight-guard.sh"
echo
if [[ -f "$PREFLIGHT" ]]; then
  if run_guard "$PREFLIGHT"; then
    ok "install.sh tool preflight suite passed"
  else
    bad "install.sh tool preflight suite FAILED (see output above)"
  fi
else
  bad "bin/tests/install-preflight-guard.sh is missing"
fi

# ── install.sh --uninstall/--purge/--dry-run ownership + safety (T-3ef9) ────
# Own file, own PATH shim (launchctl only), own temp HOME. This is a NEW
# destructive capability (stop a launchd job, move-or-delete files) — folded
# in here so a regression in its ownership check reddens CI instead of
# waiting for someone to hit it on a real machine.
UNINSTALLGUARD="$HERE/uninstall-guard.sh"
echo
if [[ -f "$UNINSTALLGUARD" ]]; then
  if run_guard "$UNINSTALLGUARD"; then
    ok "install.sh --uninstall ownership/safety suite passed"
  else
    bad "install.sh --uninstall ownership/safety suite FAILED (see output above)"
  fi
else
  bad "bin/tests/uninstall-guard.sh is missing"
fi

# ── namespace mirror across the hand-transcribed copies (T-5047) ───────────
# The namespace→(root, launchd label) derivation exists at ELEVEN SITES in SIX
# FILES, in three languages, across three Go modules that cannot import each
# other. Do NOT restate a smaller number here: this comment said FOUR, and an
# out-of-date count in a dispatcher comment is exactly how the missing sites went
# unnoticed three times. The FILE count has itself been wrong four times (FOUR,
# FIVE, SIX, SEVEN) while the SITE count was right — which is why the shared
# table's header says to count sites. The authoritative, maintained list is the header of
# namespace-mirror-guard.sh. The Go copies are guarded by their own module tests
# (cli/ocwarden/namespace_mirror_test.go, cli/ocagent/namespace_mirror_test.go,
# server/ocserverd/onboarding_mirror_test.go) against the same shared table; this
# guard covers the two shell copies and the charset regex. The
# consequence of a one-character drift is not a wrong string — the server asks
# launchd about a label the warden never registered, concludes "no warden here",
# and installs a second one over the live job.
NSMIRROR="$HERE/namespace-mirror-guard.sh"
echo
if [[ -f "$NSMIRROR" ]]; then
  if run_guard "$NSMIRROR"; then
    ok "namespace mirror suite passed"
  else
    bad "namespace mirror suite FAILED (see output above)"
  fi
else
  bad "bin/tests/namespace-mirror-guard.sh is missing"
fi

# ── install.sh EXIT-time stdin drain (T-fa39) ──────────────────────────────
# Own file, own temp HOME + fake label + launchctl shim. Guards the cosmetic
# half of the same defect the --uninstall rewrite fixed: `curl … | bash` exits
# early, the pipe's reading end closes, and curl signs off with
# "curl: (23|56) Failure writing output to destination" — a red line at the end
# of a SUCCESSFUL run. Kept separate from uninstall-guard.sh because it tests a
# property of how the script is FED, not of what --uninstall decides.
DRAINGUARD="$HERE/stdin-drain-guard.sh"
echo
if [[ -f "$DRAINGUARD" ]]; then
  if run_guard "$DRAINGUARD"; then
    ok "install.sh stdin-drain suite passed"
  else
    bad "install.sh stdin-drain suite FAILED (see output above)"
  fi
else
  bad "bin/tests/stdin-drain-guard.sh is missing"
fi

# ── install.sh prints and exits nothing until fully read (T-4358) ──────────
# The other half of the same defect. The drain above shortens the window in
# which curl gets EPIPE; it cannot close it, because a piped bash acts on the
# first chunk it parses while the rest of the file is still on the wire. The fix
# is structural — one oc_main() the last line calls — so it needs its own real
# HTTP probe rather than another assertion inside the drain suite.
# NOT "fully read before it executes": install.sh's top-level prologue really
# does run as it is parsed. What holds is that none of it prints or exits, so
# nothing is observable until delivery completes. The guard asserts both halves.
READBEFOREEXEC="$HERE/curl-bash-read-before-execute-guard.sh"
echo
if [[ -f "$READBEFOREEXEC" ]]; then
  if run_guard "$READBEFOREEXEC"; then
    ok "install.sh read-before-execute suite passed"
  else
    bad "install.sh read-before-execute suite FAILED (see output above)"
  fi
else
  bad "bin/tests/curl-bash-read-before-execute-guard.sh is missing"
fi

# ── harness process hygiene (T-1a54) ────────────────────────────────────────
# Pins run_bounded.py: the ceiling that stops a busy-loop mutant from running
# forever, and the group-reap that stops anything a guard spawned from leaking
# as an orphan (the seth-m5 46h core-burn).
PROCHYGIENE="$HERE/proc-hygiene-guard.sh"
echo
if [[ -f "$PROCHYGIENE" ]]; then
  if run_guard "$PROCHYGIENE"; then
    ok "harness process-hygiene suite passed"
  else
    bad "harness process-hygiene suite FAILED (see output above)"
  fi
else
  bad "bin/tests/proc-hygiene-guard.sh is missing"
fi

# ── per-working-copy CI mutex (T-70c9) ──────────────────────────────────────
# Owner ruling (card rc-bbf6a418fc23): one CI run per working copy; a second run
# in the SAME copy is refused loudly with a non-zero exit; more rounds means more
# copies. Two runs in one clone interleave over node_modules, the staged dist
# assets and the five regenerate-and-byte-compare gates, and the verdict is not
# reliably red — it can come out GREEN on a tree that was never validated, which
# is a forged `[ci] all green`. The guard drives bin/lib/ci-lock.sh directly
# against throwaway directories: it never races two real CI runs, because
# verifying a mutant means disabling the lock and a test that then corrupts the
# developer's tree is a bomb, not a test.
echo
CILOCK="$HERE/ci-lock-guard.sh"
if [[ -f "$CILOCK" ]]; then
  if run_guard "$CILOCK"; then
    ok "per-working-copy CI lock suite passed"
  else
    bad "per-working-copy CI lock suite FAILED (see output above)"
  fi
else
  bad "bin/tests/ci-lock-guard.sh is missing"
fi

# ── go test cache-defeat (T-bedc) ───────────────────────────────────────────
# bin/ci.sh's go test step used to run a bare `go test ./...`, so go served green
# from its TEST RESULT CACHE — a real CI log contained `ok  ocwarden  (cached)`, i.e. a
# grid cell that certified a run which never executed (and, worse, structurally
# hid flakes: a suite only runs on the first commit that changes its inputs).
# `-count=1` defeats that cache; this guard pins the flag on every go test call
# site in the repo's shell scripts, by COMMAND-POSITION parsing rather than a
# substring grep (which would match the prose in ci.sh and in the guard itself).
echo
NOCACHE="$HERE/go-test-nocache-guard.sh"
if [[ -f "$NOCACHE" ]]; then
  if run_guard "$NOCACHE"; then
    ok "go test cache-defeat suite passed"
  else
    bad "go test cache-defeat suite FAILED (see output above)"
  fi
else
  bad "bin/tests/go-test-nocache-guard.sh is missing"
fi

# ── bin/release publish/promote: CI gate + read-back (T-588c, T-b65e) ────────
# Own file because it needs its own shim set (`gh`, `curl`) and its own fixture
# git repo, and because what it guards is a different KIND of property: not "does
# this script decide correctly" but "after the irreversible step, does it check
# what actually happened". Every case there is a negative — one violated
# requirement per case — so deleting any single read-back rule in bin/release
# turns exactly one of them red instead of silently widening what ships.
# `gh release create` is NEVER reached: the shim records and creates nothing.
RELEASEGUARD="$HERE/release-guard.sh"
echo
if [[ -f "$RELEASEGUARD" ]]; then
  if run_guard "$RELEASEGUARD"; then
    ok "bin/release publish/promote read-back suite passed"
  else
    bad "bin/release publish/promote read-back suite FAILED (see output above)"
  fi
else
  bad "bin/tests/release-guard.sh is missing"
fi

# ── the automatic beta path: workflow shape + version rule (T-9fe3) ──────────
# Own file because it is the ONLY thing in this repo that parses
# .github/workflows/*.yml, and it needs a YAML parser (ruby+psych — the hosted
# macOS runner has no PyYAML) that nothing else here wants. That parse is the
# point: a workflow change has already gone green through every local gate and
# then produced a GitHub startup failure — zero jobs, no checks, and a commit that
# looks exactly like one nobody pushed. The rest of the suite guards the shape of
# a job that publishes a release without a human present: needs covering every
# gate, an `if` that cannot fire off main, contents:write scoped to that one job,
# and no workflow file being able to automate the beta→final flip.
AUTOBETA="$HERE/auto-beta-guard.sh"
echo
if [[ -f "$AUTOBETA" ]]; then
  if run_guard "$AUTOBETA"; then
    ok "auto-beta workflow + version-rule suite passed"
  else
    bad "auto-beta workflow + version-rule suite FAILED (see output above)"
  fi
else
  bad "bin/tests/auto-beta-guard.sh is missing"
fi

# ── retired image overlay stays retired (T-f014) ────────────────────────────
# The cockpit used to carry two full-size overlays for the same click: the
# shared preview shell (filename, share link, download, close) and a bare
# `Lightbox` backdrop. Which one a user got depended on the call site, and the
# split rotted invisibly — AttachmentStrip stopped reading its `onOpenImage`
# prop, so five call sites passed a handler into a component that ignored it and
# mounted a second overlay that could never open, with nothing red. The
# component and its stylesheet block are gone; this keeps them gone. A green
# does NOT mean there is only one image surface — see the guard's header.
LIGHTBOX="$HERE/lightbox-retired-guard.sh"
echo
if [[ -f "$LIGHTBOX" ]]; then
  if run_guard "$LIGHTBOX"; then
    ok "retired-Lightbox suite passed"
  else
    bad "retired-Lightbox suite FAILED (see output above)"
  fi
else
  bad "bin/tests/lightbox-retired-guard.sh is missing"
fi


# ── single-source rule review digest (T-c19c) ────────────────────────────────
# The "兩份權威打架" rule has one operational paragraph in
# seeds/system_interaction.md §2.2. The guard records its last-reviewed digest
# outside agent-facing documents; changing that paragraph turns CI red and
# forces a human to re-read the owner-facing restatement before updating the
# digest. It is a re-read reminder, not a semantic-fidelity proof.
RULEDEFER="$HERE/rule-defer-guard.sh"
echo
if [[ -f "$RULEDEFER" ]]; then
  if run_guard "$RULEDEFER"; then
    ok "single-source rule review-digest suite passed"
  else
    bad "single-source rule review-digest suite FAILED (see output above)"
  fi
else
  bad "bin/tests/rule-defer-guard.sh is missing"
fi

# ── ps field support (T-1ac8) ────────────────────────────────────────────────
# HOST-SHAPED on purpose, and the only guard here that is. The warden's
# cutover-effect probe asks `ps` for named output fields; the Go suite reaches
# ps through a seam, and the fake was keyed on the same argv production used —
# so when production asked for `etimes` (a GNU/procps field BSD ps does not
# have, on macOS, the only platform this warden runs on) the fake answered it
# politely and the entire suite went green while the probe was dead on every
# real machine. Three separate readings of that code missed it because all three
# were reading rather than running. This guard runs the real ps.
#
# It CANNOT be a Go test: cli/ocwarden's TestMain refuses real exec inside the
# test binary (refuseInTestBinary), deliberately, so that a test can never act
# on this machine's live launchd domain. That refusal is what makes the Go side
# structurally blind here, and it is why "tidy this into the Go suite" is not
# available to a future reader.
PSFIELDS="$HERE/ps-field-support-guard.sh"
echo
if [[ -f "$PSFIELDS" ]]; then
  if run_guard "$PSFIELDS"; then
    ok "ps output fields the warden probes are supported on this host"
  else
    bad "ps output-field support suite FAILED (see output above)"
  fi
else
  bad "bin/tests/ps-field-support-guard.sh is missing"
fi

# ── main-red notify job in .github/workflows/ci.yml (T-5d3b) ────────────────
# Static, hermetic: it reads the workflow file and sends nothing. Folded in here
# because the thing it guards is an ENUMERATION — the notify job's `needs:` list
# — and a job added without touching that line does not fail anything, it just
# stops being reported. That is the exact failure shape the notify job exists to
# remove, so it cannot be left to whoever remembers.
NOTIFY_GUARD="$HERE/main-red-notify-guard.sh"
echo
if [[ -f "$NOTIFY_GUARD" ]]; then
  if run_guard "$NOTIFY_GUARD"; then
    ok "main-red notify guard passed"
  else
    bad "main-red notify guard FAILED (see output above)"
  fi
else
  bad "bin/tests/main-red-notify-guard.sh is missing"
fi

# ── the MCP catalog generator: is it honest? (T-2590) ───────────────────────
# spec/mcp-catalog.json stopped being hand-maintained: bin/gen-mcp-catalog
# renders it from the x-mcp metadata on spec/openapi.json's operations. Static
# and hermetic — it regenerates into its own tempdir and never writes the tree.
# Positive controls, because a generator that silently accepts a mutated input
# turns `make drift-mcp-catalog` into a check that can never fail: it drives
# mutants at the spec (a lying x-mcp.name must be REFUSED, an edited descriptor
# must REACH the output) and at the committed catalog (a drifted byte must be
# NAMED in the diff). The byte-diff over the committed file is the OTHER check
# — `make drift-mcp-catalog`, run in the cloud's drift cell — and this suite
# also asserts that gate still exists and is still called.
MCPCATALOG="$HERE/mcp-catalog-generator.sh"
echo
if [[ -f "$MCPCATALOG" ]]; then
  if run_guard "$MCPCATALOG"; then
    ok "MCP catalog generator suite passed"
  else
    bad "MCP catalog generator suite FAILED (see output above)"
  fi
else
  bad "bin/tests/mcp-catalog-generator.sh is missing"
fi

# ── the wrapper that proves a check RAN: its own contract (T-4d88) ───────────
# bin/run-checks.sh runs `make <targets>` and then requires each target's own
# `[oc-check-done] <target>` line, because a zero exit says "nothing failed",
# not "something ran". Its behaviour was verified once, by hand, with two
# mutants driven at a real target — evidence about that afternoon and nothing
# after it. Neuter the marker loop, or soften the missing-marker exit into a
# printed warning, and every cloud cell keeps reporting green while asserting
# nothing. Those mutants live in the guard now and run every round, against a
# throwaway root with a fixture Makefile — no real check is executed.
RUNCHECKS="$HERE/run-checks-guard.sh"
echo
if [[ -f "$RUNCHECKS" ]]; then
  if run_guard "$RUNCHECKS"; then
    ok "run-checks.sh wrapper contract suite passed"
  else
    bad "run-checks.sh wrapper contract suite FAILED (see output above)"
  fi
else
  bad "bin/tests/run-checks-guard.sh is missing"
fi

# ── and that the cloud cells actually come through that door (T-4d88) ────────
# The wrapper only protects the cells that USE it, and nothing forced them to:
# changing one cell's step back to `run: make …` removes the marker check for
# that cell with no red, no shape change in the log, and a green tick on the PR.
# This guard reads .github/workflows/ci.yml and refuses a bare `make` in any job
# that declares itself `# oc-job-role: gate`. It deliberately does NOT enumerate
# which checks exist or who owns which — that consistency assertion is absent by
# owner ruling, and the enumeration it needs is the duplication T-4d88 deleted.
ENTRYPOINT="$HERE/ci-run-checks-entrypoint-guard.sh"
echo
if [[ -f "$ENTRYPOINT" ]]; then
  if run_guard "$ENTRYPOINT"; then
    ok "every gate cell reaches its checks through bin/run-checks.sh"
  else
    bad "gate-cell run-checks entrypoint suite FAILED (see output above)"
  fi
else
  bad "bin/tests/ci-run-checks-entrypoint-guard.sh is missing"
fi

# ── T-1170 cost-comparison refusal ──────────────────────────────────────────
# Own file, own tempdir, no server and no network: the comparator is a pure
# function over two JSON files. It is folded in here because the thing it
# guards is a CLAIM — the before/after cost figures this ticket reported — and
# the first version of that claim was a comment rather than an assertion, which
# an independent reviewer falsified by raising one cap on one arm and watching
# the run stay green. A number nobody can falsify is what this guard exists to
# stop being quoted again.
GUARD="$HERE/t1170-cost-compare-guard.sh"
echo
if [[ -f "$GUARD" ]]; then
  if run_guard "$GUARD"; then
    ok "T-1170 cost-comparison refusal suite passed"
  else
    bad "T-1170 cost-comparison refusal suite FAILED (see output above)"
  fi
else
  bad "bin/tests/t1170-cost-compare-guard.sh is missing"
fi

# ── guard-of-the-guard (T-d3e3 rework) ──────────────────────────────────────
# The ci success-marker guard is dispatched at the very BOTTOM of this file,
# AFTER the `[[ "$FAIL" == "0" ]] || exit 1` enforcement below, so its exit code
# is carried by nothing but its own `|| exit 1`. That `|| exit 1` is exactly the
# regression that caused this ticket's rework: without it the guard prints
# "8 ok, 3 failed" and run.sh still exits 0, i.e. the guard is decorative and CI
# step 0b stays green on a forged marker. Nothing reddened when it was removed.
# It does now: this assertion is accounted through bad(), so it is enforced by
# the FAIL count below — and the marker guard, symmetrically, asserts that THIS
# enforcement line still exists. Neither can be deleted alone without a red.

echo
SELF="$HERE/run.sh"
# Anchored, because this file greps ITSELF: an unanchored -F pattern matches the
# very line that carries it, which is a check that can never fail.
if grep -qE '^[[:space:]]*run_guard "\$MARKER_GUARD" \|\| exit 1[[:space:]]*$' "$SELF"; then
  ok "ci success-marker guard dispatch is exit-code-enforced (|| exit 1)"
else
  bad "ci success-marker guard dispatch is exit-code-enforced (|| exit 1) — without it the guard prints FAIL and run.sh still exits 0"
fi

echo "bin tests (incl. install guard): $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1

# T-d3e3: the top-level marker is a final exact log authority, not a broad grep.
# This file runs under `set -uo pipefail` with NO -e, so the guard's exit code
# must be enforced EXPLICITLY (same convention as the `[[ "$FAIL" == "0" ]] ||
# exit 1` above). Without the `|| exit 1` the guard is decorative: it prints
# FAIL and run.sh still exits 0, so CI step 0b stays green on a forged marker.
# Dispatched through run_guard (T-1a54) like every other guard in this file, so
# it inherits the wall-clock ceiling and the process-group reap rather than
# being the one guard that can hang the harness forever.
MARKER_GUARD="$HERE/ci-success-marker.sh"
if [[ -x "$MARKER_GUARD" ]]; then
  run_guard "$MARKER_GUARD" || exit 1
else
  echo "FATAL: ci success-marker guard missing/not executable: $MARKER_GUARD" >&2
  exit 1
fi
exit 0
