#!/usr/bin/env bash
# bin/tests/namespace-mirror-guard.sh — the BASH half of the cross-module
# namespace mirror confrontation.
#
# WHAT IS BEING GUARDED — AND WHAT IS NOT
# ---------------------------------------
# The namespace→(root, launchd label) derivation is hand-transcribed at ELEVEN
# CODE SITES in SIX FILES. Count sites, not files: bin/install.sh alone carries
# five, and a site hides most easily inside a file that is already listed.
#
#   cli/ocagent/config.go           ← 1 site (agents-home fallback root, as a
#                                     function: fallbackAgentsHome). By literal
#                                     value in cli/ocagent/config_test.go
#                                     (TestFallbackAgentsHome).
#   cli/ocwarden/namespace.go       ← 1 site (root + label + tmux socket +
#                                     tokfile, as functions). By literal value in
#                                     cli/ocwarden/namespace_test.go
#                                     (TestOfficraftRootFor, TestWardenLabelFor,
#                                     TestTmuxSocketFor, TestTokfileFor).
#   server/ocserverd/onboarding.go  ← 1 site (label + root + tokfile, as
#                                     functions). By literal value in
#                                     server/ocserverd/onboarding_test.go
#                                     (TestOfficraftRootPath, TestWardenLaunchdLabel,
#                                     and the tokfile paths in
#                                     TestWardenAlreadyInstalledHere).
#   bin/install.sh                  ← 5 sites: install root ($NS_DASH), install
#                                     serve label ($NS_DOT), uninstall root
#                                     ($ns_dash), uninstall serve label
#                                     ($ns_dot), and uninstall OCWARDEN label
#                                     ($ns_dot). All five checked HERE
#                                     (structure) + install-guard.sh §10 and
#                                     uninstall-guard.sh (behaviour).
#   bin/ocserver                    ← 2 sites (root, and three labels each
#                                     appearing on install+uninstall). HERE,
#                                     structure only.
#   e2e_test/lib/oc_lifecycle.sh    ← 1 site. NOT GUARDED, deliberately: harness
#                                     code that derives the root to know where to
#                                     LOOK, so a drift makes the e2e run fail to
#                                     find anything and say so loudly. Listed
#                                     because an unlisted copy is an unknown one.
#
# That is 10 of the 11 sites checked: 7 by this file, 3 by the Go module tests.
#
# STILL NOT GUARDED, NAMED RATHER THAN IMPLIED (do not read the green as more):
#   - e2e_test/lib/oc_lifecycle.sh, per the reasoning above.
#   - bin/ocserver's namespacing END TO END: it has no hermetic suite, so only
#     the text of its derivation is pinned, never its behaviour.
#   - Any site added after this comment. This list is maintained by hand;
#     `grep -rn 'officraft[-.]\$' bin/` is the cheap way to re-derive it before
#     trusting it.
#
# ⚠️ The derivation checks HERE are hard-coded regexes, one per site; they do
# NOT compare anything against the values in fixtures/namespace-axes.tsv. From
# that table this file reads only the `# charset` line, and checks that the
# empty-namespace row is present verbatim and at least one other row exists. The
# namespaced rows' values are read by nothing, here or in Go: editing them
# changes no check's outcome.
#
# WHAT THIS GUARD CANNOT SEE (stated so nobody reads its green as more than it is)
#   - The Go derivations' VALUES. Those are checked by the three module tests
#     above, which call the functions; this file only greps text.
#   - Whether bin/ocserver's namespacing actually WORKS end to end. install.sh
#     has install-guard.sh §10 / uninstall-guard.sh for that; bin/ocserver has
#     no equivalent hermetic suite, so its coverage here is structure only.
#   - The tmux-socket axis and the agents-home fallback, which exist only in the
#     Go copies — checked by value in cli/ocwarden/namespace_test.go and
#     cli/ocagent/config_test.go respectively, just not by this file.
#
# WHY THE SHELL COPIES GET A DIFFERENT TREATMENT
# ----------------------------------------------
# The Go copies are FUNCTIONS, so their tests call them and compare results. The
# shell copies are variable assignments in the middle of a long installer.
# What no behavioural suite can see is the CHARSET: it is a regex literal, not a
# derivation, and a copy looser than the others admits a namespace the rest will
# refuse — one component then builds a path or label the others do not
# recognise: a split-brain install.
#
# EVERY MATCH BELOW IS TAKEN FROM CODE LINES ONLY (see code_only). These files
# discuss the derivation and the charset at length in prose; a guard that greps
# raw would be satisfiable by its own documentation — green because someone wrote
# the right thing in a COMMENT while the code said something else.
#
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
TABLE="$HERE/fixtures/namespace-axes.tsv"
[[ -f "$TABLE" ]] || { echo "FATAL: shared namespace table not found at $TABLE" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

echo "namespace mirror — 11 hand-transcribed derivation sites in 6 files; 10 checked (7 here, 3 by Go module tests; e2e harness copy deliberately unguarded), + the charset in its 5 copies (4 grepped here; cli/ocagent's only exercised by accept/reject cases in its module test)"

# ── the charset, in the FOUR copies this file can grep ──────────────────────
# The charset literal lives in FIVE product files: cli/ocwarden/namespace.go,
# cli/ocagent/config.go, server/ocserverd/config.go, bin/install.sh and
# bin/ocserver. FIVE is the charset's own count and is not the derivation-site
# count above (server's charset is in config.go while its label/root derivation is
# in onboarding.go — which is why the two lists differ). Only four are grepped
# here. cli/ocagent's literal is compared against nothing: its only cover is
# TestFallbackAgentsHome in cli/ocagent/config_test.go, whose literal
# accept/reject cases would catch a loosened or tightened charset only where a
# case happens to sit on the changed boundary.
CHARSET="$(sed -n 's/^# charset	//p' "$TABLE" | head -1)"
if [[ -z "$CHARSET" ]]; then
  echo "FATAL: $TABLE carries no '# charset<TAB><regex>' line — the charset is unpinned" >&2
  exit 2
fi
ok "shared table declares the charset: $CHARSET"

# code_only <file> — the file with comments removed, so nothing below can be
# satisfied by prose. Drops whole-line # and // comments, then trailing ones.
# LIMITATION, stated rather than hidden: a '#' or '//' inside a quoted string
# truncates that line early. That direction is safe — it can only LOSE a real
# match and turn the guard RED (a loud false alarm someone will investigate),
# never invent one and turn it green.
code_only() {
  sed -e 's://.*::' -e 's:[[:space:]]#.*::' -e 's:^[[:space:]]*#.*::' "$1"
}

# The Go copies' module tests only feed literal accept/reject cases through the
# compiled regex (TestNamespaceFromEnv in cli/ocwarden/namespace_test.go, the
# "invalid namespace" case in server/ocserverd/config_test.go); none compares the
# regex source. The text match below is the only check that the source is
# byte-identical to the table's charset line.
for f in cli/ocwarden/namespace.go server/ocserverd/config.go bin/install.sh bin/ocserver; do
  n="$(code_only "$ROOT/$f" | grep -cF -- "$CHARSET")"
  if [[ "$n" -ge 1 ]]; then
    ok "$f carries the shared charset in CODE ($n site(s), comments excluded)"
  else
    raw="$(grep -cF -- "$CHARSET" "$ROOT/$f")"
    if [[ "$raw" -ge 1 ]]; then
      bad "$f mentions '$CHARSET' ONLY IN A COMMENT — the code enforces something else. A namespace one component accepts and another rejects is a split-brain install"
    else
      bad "$f does not contain '$CHARSET' at all — a namespace one component accepts and another rejects is a split-brain install"
    fi
  fi
done

# ── the two shell derivations ───────────────────────────────────────────────
# Structural, not behavioural: both scripts must derive root and label from the
# SAME namespace variable rather than from two independently-set knobs. The end-
# to-end behaviour for a concrete namespace is covered by install-guard.sh §10
# and uninstall-guard.sh; what a grep can add is that the derivation exists at
# all and is keyed off one variable — the property that makes the axes unable to
# disagree.
# check_derivation <label-for-humans> <file> <pattern> <count> <consequence>
# COUNT is asserted, not just presence: install and uninstall each need their own
# derivation, and "one of the two is left" is the one-way-door failure.
check_derivation() {
  local what="$1" f="$2" pat="$3" want="$4" why="$5"
  local n
  n="$(code_only "$ROOT/$f" | grep -cE -- "$pat")"
  if [[ "$n" == "$want" ]]; then
    ok "$f: $what derived from the namespace key ($n code site(s))"
  else
    bad "$f: $what — found $n CODE site(s) matching /$pat/, want $want. $why"
  fi
}

# install.sh: the install path uses NS_DASH/NS_DOT, the uninstall path
# ns_dash/ns_dot. Removal must be the exact inverse of install or --namespace is
# a one-way door, so BOTH halves are required.
check_derivation "install-path root"  bin/install.sh '\.officraft\$NS_DASH' 1 \
  "A namespaced install would write into the MAIN instance's ~/.officraft."
check_derivation "install-path label" bin/install.sh 'com\.officraft\.serve\$NS_DOT' 1 \
  "A namespaced install would boot out the MAIN instance's launchd job."
check_derivation "uninstall-path root"  bin/install.sh '\.officraft\$ns_dash' 1 \
  "--uninstall --namespace would remove the MAIN instance instead."
check_derivation "uninstall-path label" bin/install.sh 'com\.officraft\.serve\$ns_dot' 1 \
  "--uninstall --namespace would boot out the MAIN instance's job."
# The OCWARDEN label is READ-ONLY (the uninstall path never boots the
# warden out; it only reports whether a job is registered and how to remove it),
# which is exactly why it is easy to overlook and why losing it is still bad: the
# script would answer "no warden job is registered" for a machine that has one,
# and the operator's next move is a reinstall on top of a live launchd job.
# Behavioural cover: uninstall-guard.sh's namespaced section, whose fixture must
# build warden/ for that branch to be reachable.
check_derivation "uninstall-path WARDEN label" bin/install.sh 'com\.officraft\.ocwarden\$ns_dot' 1 \
  "--uninstall --namespace would report the MAIN instance's warden job (or none) for the namespaced machine."

# bin/ocserver derives labels from *_LABEL_BASE and carries each pair TWICE
# (install + uninstall) — the same inverse requirement as install.sh, which is
# why the counts are 2 and not "at least one". It namespaces THREE labels, not
# one: serve, autodeploy and tunnel are all per-instance launchd jobs, and an
# un-suffixed autodeploy or tunnel job collides with the main instance's just as
# surely as the serve one does.
check_derivation "root (install+uninstall)" bin/ocserver '\.officraft\$NS_DASH/server' 2 \
  "From-source installs would collide in the MAIN instance's root, or become unremovable."
for lbl in SERVE AUTODEPLOY TUNNEL; do
  check_derivation "${lbl} label (install+uninstall)" bin/ocserver "${lbl}_LABEL_BASE\\\$NS_DOT" 2 \
    "The namespaced instance's ${lbl} job would fight the MAIN instance for its launchd label."
done
# …and each base must still be the shared literal, or the checks above would pass
# while pointing at some other job entirely.
# NOTE `grep -c`, never `grep -q`, on the right-hand side of these pipes. This
# file runs under `set -uo pipefail`, and `grep -q` exits the moment it matches —
# which closes the pipe, SIGPIPEs the still-writing `sed`, and makes pipefail
# report the whole pipeline as failed. The guard then goes RED on a match, i.e.
# exactly backwards.
for pair in "SERVE:com.officraft.serve" "AUTODEPLOY:com.officraft.autodeploy" "TUNNEL:com.officraft.tunnel"; do
  var="${pair%%:*}"; lit="${pair#*:}"
  if [[ "$(code_only "$ROOT/bin/ocserver" | grep -cE "^readonly ${var}_LABEL_BASE=\"${lit//./\\.}\"$")" -ge 1 ]]; then
    ok "bin/ocserver: ${var}_LABEL_BASE is still $lit"
  else
    bad "bin/ocserver: ${var}_LABEL_BASE is no longer $lit — the namespaced label now suffixes something else"
  fi
done

# ── the table itself must still contain the two rows that matter ────────────
# The <empty> row is matched verbatim; for the rest only a row count is taken.
# No check above or in Go derives anything from these rows.
if grep -qE '^<empty>	<empty>	com\.officraft\.ocwarden	officraft$' "$TABLE"; then
  ok "the table still pins the EMPTY namespace to the historical literals"
else
  bad "the table lost its empty-namespace row — the main-instance claim is unverified everywhere"
fi
if [[ "$(grep -cvE '^[[:space:]]*(#.*)?$' "$TABLE")" -ge 2 ]]; then
  ok "the table carries at least one namespaced row as well"
else
  bad "the table has no namespaced row — nothing proves the suffixing works"
fi

echo "namespace-mirror tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
