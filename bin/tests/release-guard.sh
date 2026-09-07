#!/usr/bin/env bash
# bin/tests/release-guard.sh — HERMETIC unit tests for bin/release (T-588c;
# the pre-build CI gate, section G, added by T-b65e).
#
# WHAT IS ACTUALLY UNDER TEST
# ---------------------------
# `bin/release publish` exists because the old form built an artifact and then
# PRINTED a `gh release create` line for a human to paste, so every interesting
# failure lived downstream of anything that could observe it: a half-uploaded
# asset set, a release pinned to the wrong commit, a draft nobody can consume, a
# station that never moved onto the new build. The fix is that publish READS BACK
# what GitHub stored and what the station reports, and FAILS NAMING THE ITEM.
#
# That promise is only worth what its failure paths are worth, and a failure path
# nobody exercises is indistinguishable from a missing one. Every assertion below
# is therefore aimed at a NEGATIVE: given a stored release that violates exactly
# one requirement, does the command exit non-zero AND name that requirement? If
# any single check in verify_stored_release / verify_artifacts / settle_station is
# deleted, a case here goes red — that is the whole design.
#
# WHY IT BINDS TO THE REAL FUNCTIONS
# ----------------------------------
# The obvious way to test read-back rules is to re-state them in the test. Then
# the test passes forever while the real rules rot, which is the exact class of
# bug this ticket is about. So this file `source`s bin/release (its dispatch is
# guarded on "am I being executed") and calls verify_stored_release,
# verify_artifacts and settle_station DIRECTLY. There is no second copy of the
# rules anywhere in here.
#
# HERMETICITY — nothing real is touched
# -------------------------------------
#   * `gh` is a PATH shim. It NEVER reaches the network. `gh release create` is
#     the irreversible point of the whole system, so the shim records the call to
#     a tripwire and creates nothing; the dry-run cases assert that tripwire is
#     EMPTY, which is the only way to prove --dry-run cannot publish.
#   * `curl` is a PATH shim serving a canned /api/version + /api/health AND the
#     canned GitHub Actions run + jobs payloads the CI gate reads, so neither a
#     station — least of all a live one — nor api.github.com is contacted. The
#     gate needs no knob in bin/release to be driven: it is pure HTTP, and the
#     shim is the whole seam.
#   * The end-to-end cases run against a THROWAWAY git repo in mktemp that
#     carries its own bin/build (and a bin/ci.sh that exists only as a tripwire —
#     publish must never start it). bin/release cuts its staging worktree from
#     OC_RELEASE_SRC and runs the build from INSIDE it, so this exercises the real
#     CI gate + staging + packaging + verify + upload + read-back + settle arc
#     without this repo, this worktree, or any npm/go build of the actual
#     product being involved.
#   * Artifacts land in a mktemp OC_RELEASE_OUT, never dist/release/.
#
# `go` IS required (a real Mach-O arm64 binary with real linker flags is the only
# thing that can exercise the version-stamp check, which reads `go version -m`).
# It is resolved by absolute path the same way bin/ci.sh does it, because the
# launchd autodeploy job runs with a minimal PATH.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELEASE="$HERE/../release"
[[ -f "$RELEASE" ]] || { echo "FATAL: script not found at $RELEASE" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ # check DESC EXPECTED ACTUAL
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi
}
# named_failure DESC WANT_ITEM RC OUT — the shape EVERY verification failure must
# have. Two halves, deliberately inseparable: a non-zero exit (6, the verify
# code) AND a line naming the item. A non-zero exit with no item name is the old
# world, where the operator knew something broke and not what.
named_failure() {
  local desc="$1" item="$2" rc="$3" out="$4"
  if [[ "$rc" == "0" ]]; then
    bad "$desc — exited 0; the violation was ACCEPTED (out: $(printf '%s' "$out" | tr '\n' '|'))"
    return
  fi
  if [[ "$rc" != "6" ]]; then
    bad "$desc — exited $rc, expected the verify code 6 (out: $(printf '%s' "$out" | tr '\n' '|'))"
    return
  fi
  case "$out" in
    *"VERIFY-FAILED [$item]"*) ok "$desc — exit 6, named [$item]" ;;
    *) bad "$desc — exit 6 but did NOT name [$item] (out: $(printf '%s' "$out" | tr '\n' '|'))" ;;
  esac
}

GO="$(command -v go 2>/dev/null || true)"
if [[ -z "$GO" ]]; then
  for cand in /opt/homebrew/bin/go /usr/local/go/bin/go /usr/local/bin/go; do
    [[ -x "$cand" ]] && { GO="$cand"; break; }
  done
fi
[[ -n "$GO" && -x "$GO" ]] || { echo "FATAL: go not found — release-guard needs it to build a real Mach-O fixture" >&2; exit 2; }

WORK="$(mktemp -d -t oc-release-guard.XXXXXX)"
SHIMDIR="$WORK/shim"; mkdir -p "$SHIMDIR"
GHWIRE="$WORK/.gh-wire"        # every gh invocation, one per line
: > "$GHWIRE"

cleanup() { rm -rf "$WORK" 2>/dev/null || true; }
trap cleanup EXIT

# ── PATH shims ───────────────────────────────────────────────────────────────
# gh: `release view` prints $GH_VIEW_JSON (or fails when GH_VIEW_RC is set);
# `api .../releases/latest` prints $GH_LATEST_JSON (default empty == "no
# release is latest", the same shape a real 404 from that endpoint leaves
# after bin/release's own `|| true`); `release create` / `release edit` only
# ever record themselves.
cat > "$SHIMDIR/gh" <<'SH'
#!/usr/bin/env bash
echo "gh $*" >> "$GH_WIRE"
if [[ "${1:-}" == "release" && "${2:-}" == "view" ]]; then
  # stdout is emitted FIRST and the exit code applied AFTER, so a case can set
  # GH_VIEW_RC!=0 together with a non-empty GH_VIEW_JSON. That combination is not
  # a claim about how real gh behaves — it is the only way to reach the rc!=0 rule
  # on its own, because with an empty payload the very next rule ("returned
  # nothing") would have caught it anyway. See S1b.
  printf '%s' "${GH_VIEW_JSON:-}"
  [[ "${GH_VIEW_RC:-0}" == "0" ]] || { echo "release not found" >&2; exit "${GH_VIEW_RC}"; }
  exit 0
fi
if [[ "${1:-}" == "api" ]]; then
  case "${2:-}" in
    */releases/latest)
      # GH_LATEST_FLIP_AFTER lets a retry case answer WRONG for its first N
      # calls then flip to GH_LATEST_JSON_AFTER — the shape of GitHub's own
      # eventual-consistency window on /releases/latest right after a
      # `gh release edit --latest=true`.
      n="$(grep -c 'api repos/.*/releases/latest' "$GH_WIRE" || true)"
      if [[ -n "${GH_LATEST_FLIP_AFTER:-}" && "$n" -gt "${GH_LATEST_FLIP_AFTER}" ]]; then
        printf '%s' "${GH_LATEST_JSON_AFTER:-}"
      else
        printf '%s' "${GH_LATEST_JSON:-}"
      fi
      exit "${GH_LATEST_RC:-0}" ;;
  esac
fi
if [[ "${1:-}" == "release" && ( "${2:-}" == "create" || "${2:-}" == "edit" ) ]]; then
  exit "${GH_WRITE_RC:-0}"
fi
echo "unexpected gh invocation: $*" >&2
exit 99
SH
# curl: serves the canned station AND the canned GitHub Actions API. Only the
# URLs bin/release actually fetches exist; anything else 404s, so a new
# unreviewed network call cannot silently pass.
#
# The two Actions routes are what the CI gate reads since it stopped re-running
# CI itself (T-7e6c): the RUN gives head_sha (the binding to --target) and the
# JOBS list gives each required job's conclusion. Both record themselves on
# ORDER_WIRE, which is what pins that the verdict is read BEFORE the build —
# presence alone would be satisfied by a gate that reads it afterwards and
# ignores the answer.
#
# ⚠️ The jobs route is matched FIRST: `/actions/runs/<id>/jobs` also matches the
# run pattern, and a case falls to the first match.
cat > "$SHIMDIR/curl" <<'SH'
#!/usr/bin/env bash
url="${!#}"
# Did this request PRESENT an Authorization header? Both shapes are recognised —
# a plain -H argument and a `--config` file carrying `header = "Authorization: …"`
# — so the assertion is about the REQUEST, not about which plumbing bin/release
# happens to use to build it. The token VALUE is never recorded; only yes/no.
auth=no; prev=""
for a in "$@"; do
  case "$a" in Authorization:*|*" Authorization:"*) auth=yes ;; esac
  if [[ "$prev" == "--config" || "$prev" == "-K" ]]; then
    if [[ "$a" == "-" ]]; then
      # curl reads the config from STDIN — the shape that keeps a credential out
      # of both argv and the filesystem. Consume it here so the shim sees what
      # the real curl would have seen.
      grep -qiE '^[[:space:]]*header[[:space:]]*=.*Authorization:' && auth=yes
    elif [[ -f "$a" ]]; then
      grep -qiE '^[[:space:]]*header[[:space:]]*=.*Authorization:' "$a" && auth=yes
    fi
  fi
  prev="$a"
done
case "$url" in
  */actions/runs/*/jobs*)
    [[ -z "${API_WIRE:-}" ]]   || echo "auth=$auth $url" >> "$API_WIRE"
    [[ -z "${ORDER_WIRE:-}" ]] || echo "ci-verdict-jobs" >> "$ORDER_WIRE"
    printf '%s' "${GH_JOBS_JSON:-}"; exit "${GH_JOBS_RC:-0}" ;;
  */actions/runs/*)
    [[ -z "${API_WIRE:-}" ]]   || echo "auth=$auth $url" >> "$API_WIRE"
    [[ -z "${ORDER_WIRE:-}" ]] || echo "ci-verdict-run" >> "$ORDER_WIRE"
    printf '%s' "${GH_RUN_JSON:-}"; exit "${GH_RUN_RC:-0}" ;;
  */api/version) printf '%s' "${STATION_VERSION_JSON:-}"; exit "${STATION_VERSION_RC:-0}" ;;
  */api/health)  printf 'ok';                             exit "${STATION_HEALTH_RC:-0}" ;;
esac
exit 22
SH
chmod +x "$SHIMDIR/gh" "$SHIMDIR/curl"

export GH_WIRE="$GHWIRE"

# release_fn — run ONE function out of the real bin/release, in a subshell so the
# script's `set -e` and its fail_item `exit 6` land on the case and not on this
# harness. OUT captures stdout+stderr together on purpose: the read-back prints
# its machine-readable answer on stdout and its complaints on stderr, and a case
# that only looked at one of them could not tell "named the item" from "said
# nothing".
release_fn() { # release_fn <fn> [args...]
  OUT="$(PATH="$SHIMDIR:$PATH" bash -c '
    set -uo pipefail
    source "$1" || exit $?
    fn="$2"; shift 2
    "$fn" "$@"
  ' _ "$RELEASE" "$@" 2>&1)"
  RC=$?
}
# release_cli — run bin/release as a real command, shims on PATH.
release_cli() {
  OUT="$(PATH="$SHIMDIR:$PATH" bash "$RELEASE" "$@" 2>&1)"
  RC=$?
}

# stored_json — the payload the gh shim will hand back. Defaults describe a
# CORRECT prerelease; each case overrides exactly ONE field, so a red case points
# at one rule rather than at "something in here is wrong".
#
# THE FIXTURE IS MEASURED AGAINST REAL GITHUB, NOT INVENTED (2026-07-26). A shim
# that returns a made-up shape tests the code against the test author's guess, and
# stays green forever while the real payload differs — which is exactly how the
# `file -b` arch check shipped broken (it matched an ordering `file` never emits).
# So this fixture mirrors an actual `gh release view --json
# assets,isDraft,isPrerelease,targetCommitish` run on pkyosx/OffiCraft v0.5.38:
#
#   isDraft False | isPrerelease True | targetCommitish fb89a69aad8c
#   {'name': 'checksums.txt',                          'state': 'uploaded', 'size': 181}
#   {'name': 'install.sh',                             'state': 'uploaded', 'size': 70730}
#   {'name': 'officraft-v0.5.38-darwin-arm64.tar.gz',  'state': 'uploaded', 'size': 16842394}
#
# Confirmed by that run: the asset sub-fields name/state/size exist, `state` is the
# literal string "uploaded", and `size` is a non-zero integer — i.e. the three
# things verify_stored_release actually keys off. Re-measure before changing the
# shape here; do not "tidy" it to match what the code expects.
export TAG="v9.9.9-guard"
SHA="0123456789abcdef0123456789abcdef01234567"
TARBALL="officraft-$TAG-darwin-arm64.tar.gz"
stored_json() { # stored_json [key=value ...]  (values are raw JSON)
  python3 - "$@" <<PY
import json, sys
rel = {
  "tagName": "$TAG", "isDraft": False, "isPrerelease": True,
  "targetCommitish": "$SHA",
  "assets": [
    {"name": "$TARBALL", "state": "uploaded", "size": 4096},
    {"name": "install.sh",   "state": "uploaded", "size": 2048},
    {"name": "checksums.txt","state": "uploaded", "size": 128},
  ],
}
for arg in sys.argv[1:]:
    k, _, v = arg.partition("=")
    rel[k] = json.loads(v)
print(json.dumps(rel))
PY
}
# asset_list — same helper for the asset array alone.
assets_json() { python3 -c 'import json,sys; print(json.dumps(json.loads(sys.argv[1])))' "$1"; }

echo "bin/release hermetic tests (no network, no gh, no station, no product build)"

# ═══════════════════════════════════════════════════════════════════════════
# S — READ-BACK: verify_stored_release. One case per DoD item.
# ═══════════════════════════════════════════════════════════════════════════
echo "── S: read-back of what GitHub stored"

export GH_VIEW_RC=0
export GH_VIEW_JSON="$(stored_json)"
release_fn verify_stored_release "$TAG" "$SHA" prerelease
check "S0 a correct prerelease reads back clean" "0" "$RC"
# The function's CONTRACT is its stdout: line 1 = targetCommitish, line 2 =
# isLatest, lines 3+ = the asset fingerprint. promote compares two of these
# (target, fingerprint) to detect a re-upload, so a silent change to this
# shape would break drift detection while every other assertion stayed green.
check "S0 line 1 of the contract is the targetCommitish" "$SHA" "$(printf '%s\n' "$OUT" | grep -v '^\[release\]' | sed -n '1p')"
check "S0 line 2 of the contract is isLatest (no GH_LATEST_JSON stubbed -> False)" "False" \
  "$(printf '%s\n' "$OUT" | grep -v '^\[release\]' | sed -n '2p')"
check "S0 the fingerprint is name<TAB>size for all 3 assets, sorted" \
  "checksums.txt	128
install.sh	2048
$TARBALL	4096" \
  "$(printf '%s\n' "$OUT" | grep -v '^\[release\]' | sed -n '3,$p')"

# S0-latest: --latest read-back is an ENFORCED, not merely informational, check
# when a caller asks for one (this is the ticket: promote must not just flip
# --latest and trust it landed). WANT_LATEST="" (S0 above) does not enforce;
# these do.
GH_LATEST_JSON="" release_fn verify_stored_release "$TAG" "$SHA" prerelease false
check "S0a WANT_LATEST=false matches an unstubbed (not-latest) release" "0" "$RC"

GH_LATEST_JSON="" release_fn verify_stored_release "$TAG" "$SHA" prerelease true
named_failure "S0b WANT_LATEST=true but GitHub's /releases/latest is NOT this tag" release-latest "$RC" "$OUT"
case "$OUT" in
  *"gh release edit $TAG --repo"*"--latest=true"*) ok "S0b …names an executable next step (gh release edit ... --latest=true)" ;;
  *) bad "S0b …names an executable next step (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
esac

GH_LATEST_JSON="{\"tag_name\":\"$TAG\"}" release_fn verify_stored_release "$TAG" "$SHA" prerelease true
check "S0c WANT_LATEST=true matches when /releases/latest IS this tag" "0" "$RC"

GH_LATEST_JSON="{\"tag_name\":\"$TAG\"}" release_fn verify_stored_release "$TAG" "$SHA" prerelease false
named_failure "S0d WANT_LATEST=false but GitHub's /releases/latest IS this tag" release-latest "$RC" "$OUT"

# S0e/S0f — THE RETRY ITSELF (owner-requested 2026-08-28): the isLatest
# read-back reuses settle_station's poll-retry shape rather than a single
# call, because GitHub does not promise /releases/latest reflects a
# `--latest=true` flip the instant `gh release edit` returns. GH_WIRE is
# cleared first so the `gh api .../releases/latest` count in it is exactly
# this case's own attempts, not a running total from earlier S-cases.
: > "$GHWIRE"
GH_LATEST_JSON="{\"tag_name\":\"v0.0.1-not-us\"}" \
  GH_LATEST_JSON_AFTER="{\"tag_name\":\"$TAG\"}" GH_LATEST_FLIP_AFTER=1 \
  OC_RELEASE_LATEST_TRIES=3 OC_RELEASE_LATEST_SLEEP=0 \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease true
check "S0e first read WRONG, second CORRECT -> retry makes it succeed" "0" "$RC"
check "S0e …and it actually retried (2 gh api .../releases/latest calls, not 1)" "2"   "$(grep -c 'api repos/.*/releases/latest' "$GHWIRE" || true)"

: > "$GHWIRE"
GH_LATEST_JSON="{\"tag_name\":\"v0.0.1-not-us\"}" \
  OC_RELEASE_LATEST_TRIES=3 OC_RELEASE_LATEST_SLEEP=0 \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease true
named_failure "S0f NEVER matches -> exhausts the retry budget and still fails closed" release-latest "$RC" "$OUT"
check "S0f …and it spent the WHOLE retry budget (3 gh api calls), not fewer" "3"   "$(grep -c 'api repos/.*/releases/latest' "$GHWIRE" || true)"

GH_VIEW_RC=1 GH_VIEW_JSON="" release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S1 gh release view FAILS (no such release)" release-exists "$RC" "$OUT"

GH_VIEW_RC=0 GH_VIEW_JSON="" release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S2 gh release view returns NOTHING" release-exists "$RC" "$OUT"

# S1 above cannot tell the two release-exists rules apart: its payload is empty,
# so deleting the rc!=0 rule still leaves the "returned nothing" rule to catch it
# — S1 passes on S2's rule. S1b is the only case that reaches the rc!=0 rule
# alone: a non-zero exit carrying a perfectly well-formed payload, which must
# still lose. Output from a command that failed is not an answer.
GH_VIEW_RC=1 GH_VIEW_JSON="$(stored_json)" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S1b gh release view FAILS but still printed a valid payload" release-exists "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'tagName="v0.0.0-other"')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S3 GitHub answered about a DIFFERENT tag" release-tag "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'isDraft=true')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S4 the release is a DRAFT (invisible to every consumer)" release-draft "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'isPrerelease=false')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S5 publish stored a FINAL release, not a prerelease" release-channel "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'isPrerelease=true')" \
  release_fn verify_stored_release "$TAG" "$SHA" final
named_failure "S6 promote left it a PRERELEASE" release-channel "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'targetCommitish=""')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S7 EMPTY targetCommitish (cannot say which commit shipped)" release-target "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'targetCommitish="deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S8 the release is bound to the WRONG commit" release-target "$RC" "$OUT"

# The defect that motivated the ticket: the tarball uploaded, install.sh did not,
# and the release existed anyway with a documented install one-liner that 404s.
GH_VIEW_JSON="$(stored_json "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"uploaded","size":4096},
  {"name":"checksums.txt","state":"uploaded","size":128}]')")" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S9 an asset is MISSING (half-populated release)" asset-present "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"starter","size":4096},
  {"name":"install.sh","state":"uploaded","size":2048},
  {"name":"checksums.txt","state":"uploaded","size":128}]')")" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S10 an asset is present but state!=uploaded (not downloadable)" asset-uploaded "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"uploaded","size":0},
  {"name":"install.sh","state":"uploaded","size":2048},
  {"name":"checksums.txt","state":"uploaded","size":128}]')")" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S11 an asset is ZERO BYTES" asset-uploaded "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"uploaded","size":4096},
  {"name":"install.sh","state":"uploaded","size":2048},
  {"name":"checksums.txt","state":"uploaded","size":128},
  {"name":"officraft-v9.9.9-guard-darwin-arm64.tar.gz.OLD","state":"uploaded","size":9}]')")" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S12 an UNEXPECTED extra asset (somebody uploaded behind us)" asset-extra "$RC" "$OUT"

# An empty expected-target means "whatever GitHub has, as long as it has one" —
# the shape promote uses before it knows the target. It must still reject blanks
# (S7) but accept any concrete value.
GH_VIEW_JSON="$(stored_json 'targetCommitish="somebranchsha"')" \
  release_fn verify_stored_release "$TAG" "" prerelease
check "S13 an empty EXPECTED target accepts any concrete stored target" "0" "$RC"

# S14 is the ONLY case that reaches the "stored target is blank" rule. S7 looks
# like it does, but it passes a concrete expected target ($SHA), so the blank is
# actually caught one line later by the MISMATCH rule — which fails under the same
# item name, release-target, so the two are indistinguishable from the outside.
# S13 passes the empty expected target but a concrete stored one. Only
# expected="" AND stored="" lands on `if not target`. That combination is exactly
# promote's pre-flip call (it verifies with WANT_TARGET=""), so without this case
# promote could flip a release that cannot say which commit it was cut from —
# the one thing the read-back exists to prevent. Deleting the rule from
# bin/release left the whole suite green before this case existed.
GH_VIEW_JSON="$(stored_json 'targetCommitish=""')" \
  release_fn verify_stored_release "$TAG" "" prerelease
named_failure "S14 EMPTY stored target with an empty EXPECTED target (promote's pre-flip shape)" \
  release-target "$RC" "$OUT"

# ═══════════════════════════════════════════════════════════════════════════
# T — SETTLE: the station is actually running the release.
# ═══════════════════════════════════════════════════════════════════════════
echo "── T: settle (the live station moved onto this commit)"
SHORT="${SHA:0:7}"
export OC_RELEASE_SITE="http://127.0.0.1:1"   # unreachable on purpose; curl is shimmed
export OC_RELEASE_SETTLE_TRIES=2 OC_RELEASE_SETTLE_SLEEP=0

STATION_VERSION_JSON="{\"git_sha\":\"$SHORT\"}" STATION_HEALTH_RC=0 \
  release_fn settle_station "$SHA" "$SHORT"
check "T0 station reports the target git_sha and healthy → settles" "0" "$RC"

STATION_VERSION_JSON='{"git_sha":"aaaaaaa"}' \
  release_fn settle_station "$SHA" "$SHORT"
named_failure "T1 station is on a DIFFERENT commit" station-settle "$RC" "$OUT"

STATION_VERSION_JSON="{\"git_sha\":\"$SHORT\"}" STATION_HEALTH_RC=7 \
  release_fn settle_station "$SHA" "$SHORT"
named_failure "T2 station is on the right commit but /api/health is down" station-health "$RC" "$OUT"

# The prefix comparison is `$SHA == $seen*`, so a SHORT-ENOUGH answer would match
# by accident — "0" prefixes this sha. The >=7 length floor is the only thing
# stopping a truncated or placeholder field from reading as a successful deploy.
STATION_VERSION_JSON='{"git_sha":"0"}' \
  release_fn settle_station "$SHA" "$SHORT"
named_failure "T3 a 1-char git_sha that PREFIX-MATCHES is still rejected" station-settle "$RC" "$OUT"

STATION_VERSION_JSON='{}' release_fn settle_station "$SHA" "$SHORT"
named_failure "T4 /api/version has no git_sha field at all" station-settle "$RC" "$OUT"

STATION_VERSION_JSON='not json at all' release_fn settle_station "$SHA" "$SHORT"
named_failure "T5 /api/version is not JSON" station-settle "$RC" "$OUT"

STATION_VERSION_RC=7 STATION_VERSION_JSON='' release_fn settle_station "$SHA" "$SHORT"
named_failure "T6 /api/version cannot be reached at all" station-settle "$RC" "$OUT"

unset OC_RELEASE_SITE OC_RELEASE_SETTLE_TRIES OC_RELEASE_SETTLE_SLEEP

# ═══════════════════════════════════════════════════════════════════════════
# A — PRE-UPLOAD artifact verification.
# ═══════════════════════════════════════════════════════════════════════════
# These run against a hand-built OUT dir, so each case can violate exactly one
# property. The Go fixture is real: `go version -m` reads the LINKER FLAGS, which
# is the only evidence that the tag/commit a release CLAIMS is the tag/commit
# compiled into it — a grep over the tarball would "pass" on an unstamped binary
# because README.md inside the tarball also contains the tag.
echo "── A: pre-upload artifact verification"
GOSRC="$WORK/gosrc"; mkdir -p "$GOSRC"
cat > "$GOSRC/main.go" <<'GO'
package main

var appVersion = "0.0.0"
var buildSHA = "unknown"

func main() { println(appVersion, buildSHA) }
GO
( cd "$GOSRC" && "$GO" mod init ocfixture >/dev/null 2>&1 )
build_fixture() { # build_fixture <out> [ldflags]
  ( cd "$GOSRC" && "$GO" build -ldflags "${2:-}" -o "$1" . ) \
    || { echo "FATAL: go build of the fixture failed" >&2; exit 2; }
}
STAMPED="$WORK/ocserverd.stamped"
build_fixture "$STAMPED" "-X main.appVersion=$TAG -X main.buildSHA=$SHORT"
PLAIN="$WORK/ocserverd.plain"
build_fixture "$PLAIN" ""

# make_out — assemble a well-formed OUT dir, then let the caller break one thing.
AOUT="$WORK/out"
make_out() { # make_out <serverd-binary> [member-to-omit] [ocagent-binary]
  rm -rf "$AOUT" "$WORK/pkg"; mkdir -p "$AOUT" "$WORK/pkg/officraft-$TAG-darwin-arm64"
  local pkg="$WORK/pkg/officraft-$TAG-darwin-arm64" skip="${2:-}" agent="${3:-$STAMPED}"
  cp "$1" "$pkg/ocserverd"
  cp "$STAMPED" "$pkg/ocwarden"; cp "$agent" "$pkg/ocagent"; cp "$STAMPED" "$pkg/officraft"
  printf '#!/bin/sh\n' > "$pkg/install.sh"; printf 'MIT\n' > "$pkg/LICENSE"
  printf '# OffiCraft %s\n' "$TAG" > "$pkg/README.md"
  [[ -n "$skip" ]] && rm -f "$pkg/$skip"
  tar -C "$WORK/pkg" -czf "$AOUT/$TARBALL" "officraft-$TAG-darwin-arm64"
  cp "$pkg/install.sh" "$AOUT/install.sh" 2>/dev/null || printf '#!/bin/sh\n' > "$AOUT/install.sh"
  ( cd "$AOUT" && shasum -a 256 "$TARBALL" install.sh > checksums.txt )
}
verify_out() { # verify_out — call the real verify_artifacts against $AOUT
  OUT="$(PATH="$SHIMDIR:$PATH" OC_RELEASE_OUT="$AOUT" bash -c '
    set -uo pipefail
    source "$1" || exit $?
    verify_artifacts "$2" "$3" "$4" "$5"
  ' _ "$RELEASE" "$TAG" "$SHA" "$SHORT" "officraft-$TAG-darwin-arm64" 2>&1)"
  RC=$?
}

make_out "$STAMPED"
verify_out
check "A0 a correct artifact set verifies clean" "0" "$RC"

make_out "$STAMPED"; rm -f "$AOUT/install.sh"
verify_out
named_failure "A1 an expected artifact is missing from the output dir" artifact-present "$RC" "$OUT"

make_out "$STAMPED"; : > "$AOUT/checksums.txt"
verify_out
named_failure "A2 an artifact is empty" artifact-nonempty "$RC" "$OUT"

# A tarball missing ocwarden is a broken install, not a cosmetic defect: the
# installer lays down a warden that does not exist.
make_out "$STAMPED" ocwarden
verify_out
named_failure "A3 the tarball is MISSING a member (ocwarden)" tarball-members "$RC" "$OUT"

# …and the other direction: a stray file that sneaked into the package is equally
# a wrong tarball. The member list is asserted EXACTLY, not as a subset.
make_out "$STAMPED"
printf 'oops\n' > "$WORK/pkg/officraft-$TAG-darwin-arm64/scratch.log"
tar -C "$WORK/pkg" -czf "$AOUT/$TARBALL" "officraft-$TAG-darwin-arm64"
( cd "$AOUT" && shasum -a 256 "$TARBALL" install.sh > checksums.txt )
verify_out
named_failure "A3b the tarball grew a STRAY member" tarball-members "$RC" "$OUT"

make_out "$STAMPED"
printf 'not a binary' > "$WORK/pkg/officraft-$TAG-darwin-arm64/ocagent"
tar -C "$WORK/pkg" -czf "$AOUT/$TARBALL" "officraft-$TAG-darwin-arm64"
( cd "$AOUT" && shasum -a 256 "$TARBALL" install.sh > checksums.txt )
verify_out
named_failure "A4 a shipped binary is not a darwin/arm64 Mach-O" artifact-arch "$RC" "$OUT"

make_out "$PLAIN"
verify_out
named_failure "A5 ocserverd carries NO version stamp" version-stamp "$RC" "$OUT"

build_fixture "$WORK/wrongver" "-X main.appVersion=v0.0.0-wrong -X main.buildSHA=$SHORT"
make_out "$WORK/wrongver"
verify_out
named_failure "A6 ocserverd is stamped with the WRONG appVersion" version-stamp "$RC" "$OUT"

build_fixture "$WORK/wrongsha" "-X main.appVersion=$TAG -X main.buildSHA=beefbee"
make_out "$WORK/wrongsha"
verify_out
named_failure "A7 ocserverd is stamped with the WRONG buildSHA" version-stamp "$RC" "$OUT"

# ── the PACKAGED ocagent's own stamp (T-8f7d) ──────────────────────────────
# Not a duplicate of A5–A7. ocserverd can be asked what it is at any time —
# /api/version, the running process, the artifact on disk. A listener cannot: it
# holds the image it started with, and an ocagent with no stamp prints connection
# lines with no [agent …] segment, which is BYTE-IDENTICAL to a dev build doing
# exactly the right thing. There is no downstream moment where the difference is
# visible, so if this is not caught on the artifact it is not caught at all.
# ⚠️ And the tree is otherwise silent about it: measured, an unstamped ocagent
# staged into bindist gets embedded into ocserverd verbatim while
# bin/tests/agent-build-sha-guard.sh still prints 13 ok (it rebuilds the very
# file the mutant replaced) and check-officraft-dist returns 0.
make_out "$STAMPED" "" "$PLAIN"
verify_out
named_failure "A7b the packaged ocagent carries NO build stamp" agent-stamp "$RC" "$OUT"

make_out "$STAMPED" "" "$WORK/wrongsha"
verify_out
named_failure "A7c the packaged ocagent is stamped with the WRONG buildSHA" agent-stamp "$RC" "$OUT"

# A checksums file covering ONE file is how install.sh's sha256 step starts
# silently verifying nothing.
make_out "$STAMPED"
( cd "$AOUT" && shasum -a 256 "$TARBALL" > checksums.txt )
verify_out
named_failure "A8 checksums.txt covers only 1 of the 2 downloads" checksums-scope "$RC" "$OUT"

make_out "$STAMPED"
python3 - "$AOUT/checksums.txt" <<'PY'
import sys
p = sys.argv[1]
lines = open(p).read().splitlines()
lines[0] = "0" * 64 + lines[0][64:]
open(p, "w").write("\n".join(lines) + "\n")
PY
verify_out
named_failure "A9 checksums.txt digests do NOT match the files" checksums-match "$RC" "$OUT"

# ═══════════════════════════════════════════════════════════════════════════
# C — CLI gates. These must all fail BEFORE any build or upload.
# ═══════════════════════════════════════════════════════════════════════════
echo "── C: argument gates"
: > "$GHWIRE"
release_cli; check "C0 no subcommand → usage, exit 2" "2" "$RC"
release_cli publish --beta v1.0.0; check "C1 publish without --target → exit 2" "2" "$RC"
release_cli publish --target HEAD; check "C2 publish without --beta → exit 2" "2" "$RC"
release_cli publish --beta 1.0.0 --target HEAD
check "C3 a tag without the v prefix → exit 2" "2" "$RC"
release_cli promote nope; check "C4 promote with a non-tag → exit 2" "2" "$RC"
release_cli promote v1.0.0 v2.0.0; check "C5 promote with two tags → exit 2" "2" "$RC"
release_cli publish --beta v1.0.0 --target HEAD --bogus
check "C6 an unknown publish flag → exit 2" "2" "$RC"

# The legacy form must be REFUSED, never reinterpreted. Silently treating
# `bin/release v0.1.0` as `publish --beta v0.1.0 --target HEAD` would keep the
# words and change the deed: it would really upload, and really pick a commit the
# operator never named.
release_cli v0.1.0
check "C7 the removed 'bin/release <tag>' form exits 2" "2" "$RC"
case "$OUT" in
  *REMOVED*"publish --beta v0.1.0 --target"*) ok "C7 …and names the replacement command" ;;
  *) bad "C7 …and names the replacement command (out: $OUT)" ;;
esac
check "C8 NOTHING in the argument gates ever invoked gh" "" "$(cat "$GHWIRE")"

# ═══════════════════════════════════════════════════════════════════════════
# E — END TO END. Real staging worktree, real packaging, real read-back.
# ═══════════════════════════════════════════════════════════════════════════
# The fixture repo carries its own bin/build, and bin/release runs
# `$STAGE/bin/build` — a path inside the staging worktree it cut from
# OC_RELEASE_SRC. So this drives the genuine control flow with a build that
# finishes in a second.
echo "── E: end-to-end publish against a fixture repo"
SRC="$WORK/src"; mkdir -p "$SRC/bin" "$SRC/gosrc"
cp "$GOSRC/main.go" "$SRC/gosrc/main.go"
cp "$GOSRC/go.mod"  "$SRC/gosrc/go.mod"
printf 'MIT\n' > "$SRC/LICENSE"
printf '#!/bin/sh\necho fixture installer\n' > "$SRC/bin/install.sh"
# The fixture build records WHICH builder publish invoked. Since T-0398 there is
# exactly one (bin/build) — the signed variant and every OC_CODESIGN_* knob are
# deleted — so this wire's remaining job is to pin that publish still routes
# through the staging worktree's own bin/build and hands it the tag.
cat > "$SRC/bin/build" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
R="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo "builder=bin/build" > "$BUILD_WIRE"
echo "OC_APP_VERSION=${OC_APP_VERSION:-unset}" >> "$BUILD_WIRE"
echo "build" >> "$ORDER_WIRE"
SHORT="$(git -C "$R" rev-parse --short HEAD)"
mkdir -p "$R/.deploy" "$R/server/ocserverd/bindist"
( cd "$R/gosrc" && "$GO_BIN" build \
    -ldflags "-X main.appVersion=${OC_APP_VERSION} -X main.buildSHA=${SHORT}" \
    -o "$R/.deploy/ocserverd" . )
( cd "$R/gosrc" && "$GO_BIN" build -o "$R/server/ocserverd/bindist/ocwarden" . )
# ⚠️ ocagent is BUILT, not copied from ocwarden: the real bin/build-bindist
# stamps it with -X main.buildSHA and verify_artifacts now reads that stamp off
# the packaged copy (T-8f7d). A fixture that shipped an unstamped ocagent would
# make every E case here fail for a reason none of them are about.
( cd "$R/gosrc" && "$GO_BIN" build -ldflags "-X main.buildSHA=${SHORT}" \
    -o "$R/server/ocserverd/bindist/ocagent" . )
cp "$R/server/ocserverd/bindist/ocwarden" "$R/server/ocserverd/bindist/officraft"
SH
# The fixture CI — now a TRIPWIRE, not a driven lane (T-7e6c).
#
# publish used to run `$STAGE/bin/ci.sh` and judge its log. It no longer runs any
# CI: the gate reads the verdict of the GitHub Actions run that this commit
# already went through (see the G section). The fixture repo still carries a
# bin/ci.sh precisely so that change is ASSERTABLE rather than assumed — the file
# is present, executable, and would record itself on ORDER_WIRE if anything
# invoked it. G0 asserts the wire holds no "ci-local" entry, which is what pins
# "publish does not re-run CI on this machine". Delete this file and that
# assertion degrades into a tautology.
cat > "$SRC/bin/ci.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "ci-local" >> "$ORDER_WIRE"
echo "[ci] (fixture) a local CI round nobody should have started"
exit 0
SH
chmod +x "$SRC/bin/build" "$SRC/bin/install.sh" "$SRC/bin/ci.sh"
(
  cd "$SRC"
  git init -q .
  git config user.email guard@example.invalid
  git config user.name  guard
  git add -A
  git -c commit.gpgsign=false commit -qm "fixture"
) || { echo "FATAL: could not build the fixture repo" >&2; exit 2; }
E_SHA="$(git -C "$SRC" rev-parse HEAD)"
E_SHORT="$(git -C "$SRC" rev-parse --short HEAD)"
E_TAG="v9.9.9-e2e"
E_TARBALL="officraft-$E_TAG-darwin-arm64.tar.gz"
EOUT="$WORK/e2e-out"
BUILD_WIRE="$WORK/.build-wire"
# ORDER_WIRE records the SEQUENCE of the things publish does before it builds:
# the curl shim appends "ci-verdict-run" then "ci-verdict-jobs" as the gate reads
# GitHub's verdict, the fixture bin/ci.sh would append "ci-local" if publish ever
# started a local round, and the build appends "build". The gate is only worth
# anything if it is consulted BEFORE the build, so the ORDER is asserted, not
# just the presence of both.
ORDER_WIRE="$WORK/.order-wire"
# API_WIRE records every Actions request the shimmed curl was asked for, as
# `auth=yes|no <url>` (the station poll is deliberately NOT recorded — it is a
# different subsystem). So a case can assert WHICH run was interrogated, not
# merely that "some HTTP happened"; whether the request was AUTHENTICATED; and,
# for the manual case, that GitHub was not asked about a round at all. The token
# VALUE never reaches this wire — only the yes/no.
API_WIRE="$WORK/.api-wire"

# The GitHub Actions run this fixture publish pretends to be running inside.
# bin/release reads GITHUB_RUN_ID; empty means "not inside a run", which is the
# manual path.
E_RUN_ID="8675309"
OC_GUARD_RUN_ID="$E_RUN_ID"

# ── the two canned Actions payloads ─────────────────────────────────────────
# MEASURED SHAPE, NOT INVENTED. Both mirror a real anonymous fetch of
# api.github.com/repos/pkyosx/OffiCraft/actions/runs/<id> and .../jobs (the repo
# is public; the gate uses no token). The fields the gate keys off are `id` and
# `head_sha` on the run, and `name` + `conclusion` (plus `total_count`) on the
# jobs list. Re-measure before reshaping this; do not tidy it to match the code.
#
# ⚠️ `?per_page=100` and NO `filter` parameter: the API defaults to
# filter=latest, which is what excludes a re-run's superseded earlier attempt.
GATE_JOBS=(go-checks frontend-checks frontend-ct drift-checks contract-guards
           conformance hygiene bin-guards tcc-anchor e2e-isolation-guard macos-e2e)
gh_run_json() { # gh_run_json [head_sha] [id]
  GH_RUN_JSON="$(python3 -c '
import json, sys
print(json.dumps({"id": int(sys.argv[2]), "head_sha": sys.argv[1],
                  "status": "in_progress", "conclusion": None,
                  "run_attempt": 1, "name": "ci"}))' "${1:-$E_SHA}" "${2:-$E_RUN_ID}")"
  export GH_RUN_JSON
}
# gh_jobs_json [name=conclusion ...] — starts from all-green over the required
# set; each argument overrides ONE job's conclusion, and the pseudo-value
# `absent` DROPS the job from the list entirely (a job that never ran produces no
# entry at all — the case a "conclusions are all fine" check cannot see).
gh_jobs_json() {
  GH_JOBS_JSON="$(python3 - "${GATE_JOBS[@]}" -- "$@" <<'PY'
import json, sys
argv = sys.argv[1:]
sep = argv.index("--")
names, overrides = argv[:sep], argv[sep+1:]
ov = dict(a.split("=", 1) for a in overrides)
jobs = []
for i, n in enumerate(names):
    c = ov.pop(n, "success")
    if c == "absent":
        continue
    jobs.append({"id": 1000 + i, "name": n, "status": "completed", "conclusion": c})
# the two declared not-a-gate jobs really are in this payload and really are
# skipped on most rounds — the gate must ignore them, not trip over them.
jobs.append({"id": 9001, "name": "auto-beta", "status": "in_progress", "conclusion": None})
jobs.append({"id": 9002, "name": "notify-main-red", "status": "completed", "conclusion": "skipped"})
for n, c in ov.items():
    jobs.append({"id": 9100, "name": n, "status": "completed", "conclusion": c})
print(json.dumps({"total_count": len(jobs), "jobs": jobs}))
PY
)"
  export GH_JOBS_JSON
}

e2e() { # e2e [extra publish args...] — env overrides come from the caller
  : > "$GHWIRE"; : > "$BUILD_WIRE"; : > "$ORDER_WIRE"; : > "$API_WIRE"
  # E2E_KEEP_OUT keeps the previous run's output dir, which is how the CI-evidence
  # case can observe TWO runs accumulating rather than one run overwriting.
  [[ -n "${E2E_KEEP_OUT:-}" ]] || rm -rf "$EOUT"
  OUT="$(PATH="$SHIMDIR:$PATH" \
    OC_RELEASE_SRC="$SRC" OC_RELEASE_OUT="$EOUT" \
    OC_RELEASE_GH_REPO="guard/fixture" \
    OC_RELEASE_SITE="http://127.0.0.1:1" \
    OC_RELEASE_SETTLE_TRIES=2 OC_RELEASE_SETTLE_SLEEP=0 \
    BUILD_WIRE="$BUILD_WIRE" ORDER_WIRE="$ORDER_WIRE" API_WIRE="$API_WIRE" GO_BIN="$GO" \
    GITHUB_RUN_ID="${OC_GUARD_RUN_ID:-}" \
    GITHUB_TOKEN="${OC_GUARD_GH_TOKEN:-}" GH_TOKEN="${OC_GUARD_GH_TOKEN_ALT:-}" \
    bash "$RELEASE" publish --beta "$E_TAG" --target "$E_SHA" "$@" 2>&1)"
  RC=$?
}
e2e_stored() { # e2e_stored [stored_json overrides...] — what the shim will report
  GH_VIEW_JSON="$(python3 - "$E_TAG" "$E_SHA" "$E_TARBALL" "$@" <<'PY'
import json, sys
tag, sha, tarball = sys.argv[1:4]
rel = {"tagName": tag, "isDraft": False, "isPrerelease": True, "targetCommitish": sha,
       "assets": [{"name": n, "state": "uploaded", "size": 4096}
                  for n in (tarball, "install.sh", "checksums.txt")]}
for arg in sys.argv[4:]:
    k, _, v = arg.partition("=")
    rel[k] = json.loads(v)
print(json.dumps(rel))
PY
)"
  export GH_VIEW_JSON
}

# E0 — --dry-run. The single most important negative in this file: it builds and
# verifies for real and MUST NOT be able to publish. `gh release create` is the
# irreversible point of the whole system, so this is asserted on the gh tripwire.
export GH_VIEW_RC=0
# The default world for every E case: this run really is the run that built this
# commit, and every required gate job went green. Each G case below overrides
# exactly ONE thing about that, so a red case points at one rule.
gh_run_json
gh_jobs_json
e2e_stored
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e --dry-run
check "E0 --dry-run exits 0" "0" "$RC"
check "E0 --dry-run invoked gh EXACTLY ZERO times" "" "$(cat "$GHWIRE")"
check "E0 --dry-run still produced the real artifacts" "yes" \
  "$([[ -s "$EOUT/$E_TARBALL" && -s "$EOUT/install.sh" && -s "$EOUT/checksums.txt" ]] && echo yes || echo no)"
case "$OUT" in *"artifacts verified"*) ok "E0 --dry-run ran the real pre-upload verification" ;;
  *) bad "E0 --dry-run ran the real pre-upload verification (out: $(printf '%s' "$OUT" | tail -c 400))" ;; esac
check "E0 publish built through the staging worktree's own bin/build" \
  "builder=bin/build" "$(sed -n '1p' "$BUILD_WIRE")"
check "E0 the tag reached the build as OC_APP_VERSION" \
  "OC_APP_VERSION=$E_TAG" "$(sed -n '2p' "$BUILD_WIRE")"

# E1 — the happy full arc.
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" STATION_HEALTH_RC=0 e2e
check "E1 a fully correct publish exits 0" "0" "$RC"
check "E1 gh release create was called exactly once" "1" \
  "$(grep -c 'release create' "$GHWIRE" || true)"
case "$(grep 'release create' "$GHWIRE")" in
  *"--prerelease"*"--target $E_SHA"*) ok "E1 upload was --prerelease and --target the named sha" ;;
  *) bad "E1 upload was --prerelease and --target the named sha ($(grep 'release create' "$GHWIRE"))" ;;
esac

# ── G: THE CI GATE (T-b65e; rebuilt on GitHub's verdict by T-7e6c) ──────────
# Merging was loosened on purpose, so this gate is the ONLY behavioural check a
# beta gets before the station picks it up by itself. What changed in T-7e6c is
# the EVIDENCE, not the existence of the gate: publish no longer starts its own
# CI round on the runner, it reads the verdict of the GitHub Actions run this
# very commit already went through. Everything below is aimed at the three ways
# that could be worthless: reading SOMEBODY ELSE'S round, believing a round that
# did not actually cover every gate job, and believing a non-green conclusion.
#
# NOTE these cases deliberately do NOT assert that any particular string appears
# in bin/release. Such an assertion is true even when the call is commented out,
# and it is true of any implementation that reads a verdict and then ignores the
# answer. Every case below drives the real command and judges what it DID.
echo "── G: the pre-build CI gate (reads this commit's GitHub Actions round)"

# G0 — the gate is consulted, it is consulted about THIS run, it is consulted
# BEFORE the build, and publish does NOT start a CI round of its own.
gh_run_json; gh_jobs_json
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" STATION_HEALTH_RC=0 e2e
check "G0 a green GitHub round lets publish through" "0" "$RC"
check "G0 the verdict was read BEFORE the build, and no local CI round was started" \
  "ci-verdict-run
ci-verdict-jobs
build" "$(cat "$ORDER_WIRE")"
check "G0 it asked about THIS run's id, on this repo, and never paginated blind" "2" \
  "$(grep -c "/repos/guard/fixture/actions/runs/$E_RUN_ID" "$API_WIRE" || true)"
# The jobs listing must NOT carry a `filter` parameter: the API default
# (filter=latest) is what excludes a re-run's superseded earlier attempt, and
# `filter=all` would let a stale green attempt certify a red re-run.
check "G0 the jobs listing pages to 100 and passes NO filter parameter" "yes" \
  "$(grep -q "/jobs?per_page=100$" "$API_WIRE" && echo yes || echo no)"

# G1 — THE BINDING. A perfectly green round that belongs to a DIFFERENT commit
# must not certify this one. This is the rule that makes "read a verdict"
# defensible at all: without it, publish would ship $E_SHA on the strength of a
# green round for some other tree. Everything else here is green.
gh_run_json "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G1 the round is green but its head_sha is ANOTHER commit → publish aborts" \
  ci-run-binding "$RC" "$OUT"
check "G1 …and NOTHING was built" "" "$(cat "$BUILD_WIRE")"
check "G1 …and gh was never invoked (no tag, no release, no upload)" "" "$(cat "$GHWIRE")"
check "G1 …and no artifacts were produced" "no" \
  "$([[ -e "$EOUT/$E_TARBALL" ]] && echo yes || echo no)"

# G1b — the same binding from the other end: the payload answers about a
# different run id than the one we asked about. A green answer to a question we
# did not ask is not an answer.
gh_run_json "$E_SHA" 111222
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G1b the payload describes a DIFFERENT run id than the one asked about" \
  ci-run-binding "$RC" "$OUT"

# G2 — THE ROLL-CALL. A job that never ran produces NO ENTRY at all, so "every
# conclusion I can see is success" is satisfied by a round that skipped it (a
# `paths-ignore`, a dropped `needs` edge, a renamed job). The gate must require
# each required job to be PRESENT.
gh_run_json
gh_jobs_json 'conformance=absent'
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G2 a required job is ABSENT from the round → publish aborts" \
  release-ci "$RC" "$OUT"
check "G2 …and NOTHING was built" "" "$(cat "$BUILD_WIRE")"
check "G2 …and gh was never invoked" "" "$(cat "$GHWIRE")"

# G2b — macos-e2e SPECIFICALLY, and it is not a duplicate of G2. It is the one
# required job with no counterpart anywhere in bin/ci.sh's round (that round is
# bin/lib/ci-round.txt; its size is not restated here, because a number in prose
# goes stale silently): the real-browser e2e round. Reading GitHub's verdict is what brought it inside the gate at all,
# and the next person to drop it from auto-beta's `needs` — or from the gate's
# required list — must be told by a test rather than by a customer.
gh_jobs_json 'macos-e2e=absent'
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G2b macos-e2e (the only REAL-BROWSER check, absent from bin/ci.sh) must be in the required set" \
  release-ci "$RC" "$OUT"
case "$OUT" in
  *macos-e2e*) ok "G2b …and the failure names macos-e2e" ;;
  *) bad "G2b …and the failure names macos-e2e (out: $(printf '%s' "$OUT" | tail -c 400))" ;;
esac

# G3 — CONCLUSION IS COMPARED TO "success", NOT TO "not failure". Measured over
# the last 200 runs of this workflow, conclusions are success 168, cancelled 23,
# failure 7, action_required 2 — so a `!= "failure"` test would wave through the
# three non-green outcomes that actually occur, plus `skipped`, which is what
# every gate job reports when an earlier one went red. One case per value: they
# are separate strings and a mutant can get any one of them wrong on its own.
for concl in skipped cancelled action_required neutral timed_out null; do
  if [[ "$concl" == "null" ]]; then
    gh_jobs_json 'hygiene=absent'
    # a job still RUNNING has an entry with conclusion null — present, not green
    GH_JOBS_JSON="$(python3 -c '
import json,sys
d=json.loads(sys.argv[1])
d["jobs"].append({"id":1234,"name":"hygiene","status":"in_progress","conclusion":None})
print(json.dumps(d))' "$GH_JOBS_JSON")"
    export GH_JOBS_JSON
  else
    gh_jobs_json "hygiene=$concl"
  fi
  STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
  named_failure "G3 a required job with conclusion=$concl is NOT green → publish aborts" \
    release-ci "$RC" "$OUT"
  check "G3 ($concl) …and NOTHING was built" "" "$(cat "$BUILD_WIRE")"
done
gh_jobs_json 'go-checks=failure'
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G3 …and an outright failure still aborts (the obvious half)" \
  release-ci "$RC" "$OUT"

# G3b — the two declared not-a-gate jobs are skipped on every round that reaches
# this code (auto-beta is the job DOING the publishing, so it is in_progress;
# notify-main-red only runs when something went red). A gate that judged every
# job in the payload would therefore refuse every legitimate release. The
# required set is a roll-call, not a sweep.
gh_jobs_json
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" STATION_HEALTH_RC=0 e2e
check "G3b a round whose NOT-a-gate jobs are skipped/running still publishes" "0" "$RC"

# G4 — the gate is not a dry-run-only nicety, and it is not skippable by the
# rehearsal flag either: a rehearsal must rehearse the step most likely to stop
# the release, and must still refuse when the round is not green.
gh_jobs_json 'drift-checks=cancelled'
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e --dry-run
named_failure "G4 --dry-run also applies the gate and also refuses a non-green round" \
  release-ci "$RC" "$OUT"

# G5 — FAIL CLOSED. Inside a run, an unreadable verdict is not permission to
# ship: it is the one shape that could quietly turn the gate off for everybody
# (a 403, a rename, an outage). Both halves are separately reachable.
gh_run_json; gh_jobs_json
GH_RUN_RC=22 STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G5 the run lookup FAILS while inside a run → publish aborts (fail closed)" \
  release-ci "$RC" "$OUT"
check "G5 …and NOTHING was built" "" "$(cat "$BUILD_WIRE")"
GH_JOBS_RC=22 STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G5b the JOBS lookup fails → publish aborts" release-ci "$RC" "$OUT"
GH_RUN_JSON='not json at all' STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G5c an unparseable payload is not a green verdict" release-ci "$RC" "$OUT"

# G5d — pagination honesty. If GitHub says the round has more jobs than this one
# page returned, the roll-call cannot be trusted to be complete, and a missing
# page is indistinguishable from a missing job.
gh_run_json
gh_jobs_json
GH_JOBS_JSON="$(python3 -c '
import json,sys
d=json.loads(sys.argv[1]); d["total_count"]=d["total_count"]+5; print(json.dumps(d))' "$GH_JOBS_JSON")" \
  STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G5d total_count exceeds the page returned (truncated roll-call) → publish aborts" \
  release-ci "$RC" "$OUT"

# G6 — THE MANUAL PATH (owner ruling, card rc-538d9ca1ad1d). A publish run by
# hand is not inside a GitHub Actions run, so there is no round of its own to
# read. The owner ruled that this WARNS AND PROCEEDS rather than refusing; the
# cost — a hand-run publish carries no CI verdict at all — is explicitly accepted.
# This case exists to keep that a DECISION rather than a drift: it pins that the
# publish really happens, that no CI round is started to paper over it, and that
# the operator is told in terms that cannot be mistaken for a green.
gh_run_json; gh_jobs_json
OC_GUARD_RUN_ID="" STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" STATION_HEALTH_RC=0 e2e
check "G6 a manual publish with no GitHub round PUBLISHES anyway (owner ruling)" "0" "$RC"
check "G6 …and really uploaded (this is a warning, not a refusal)" "1" \
  "$(grep -c 'release create' "$GHWIRE" || true)"
check "G6 …and started NO local CI round to compensate" "build" "$(cat "$ORDER_WIRE")"
check "G6 …and asked GitHub nothing at all" "" "$(cat "$API_WIRE")"
case "$OUT" in
  *"NO CI VERDICT"*) ok "G6 …and says in terms of the VERDICT that none was checked" ;;
  *) bad "G6 …and says in terms of the VERDICT that none was checked (out: $(printf '%s' "$OUT" | tail -c 600))" ;;
esac
# The warning must not be readable as a green. A log that says "CI green" when
# nothing was consulted is worse than one that says nothing.
case "$OUT" in
  *"[release]   CI green"*) bad "G6 …and never claims CI was green" ;;
  *) ok "G6 …and never claims CI was green" ;;
esac

# G7 — the verdict is recorded. Both payloads land in a PER-RUN directory under
# the output dir, so the next person can see WHAT was believed and WHY, and two
# publishes cannot share (or overwrite) each other's evidence.
gh_run_json; gh_jobs_json
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e --dry-run
check "G7 the round's own payloads land under a per-run directory in the output dir" "1" \
  "$(find "$EOUT/ci" -name jobs.json 2>/dev/null | wc -l | tr -d '[:space:]')"
check "G7 …together with the run payload that carries the binding" "1" \
  "$(find "$EOUT/ci" -name run.json 2>/dev/null | wc -l | tr -d '[:space:]')"
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" E2E_KEEP_OUT=1 e2e --dry-run
check "G7 a second publish gets its OWN directory (it cannot reuse or overwrite the first)" "2" \
  "$(find "$EOUT/ci" -name jobs.json 2>/dev/null | wc -l | tr -d '[:space:]')"
check "G7 …and every evidence file holds the run that was actually believed" "$E_RUN_ID" \
  "$(find "$EOUT/ci" -name run.json | while IFS= read -r f; do python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$f"; done | sort -u)"

# ── G8: CREDENTIALS. Token when there is one, anonymous when there is not ───
# WHY THIS EXISTS AT ALL. The gate started out unconditionally anonymous, on the
# measurement that both endpoints answer 200 without a token to a public repo.
# That measurement was taken on a developer machine, whose egress IP is ours
# alone. GitHub's ANONYMOUS quota is 60 requests/hour PER IP, and a hosted runner
# does not have an IP of its own in that sense — so "anonymous works" was never
# actually established for the one environment that ships releases. A 403 there
# fails closed (good: no silent false green) but it fails closed for EVERY
# release, which is an outage of the shipping path.
#
# So: authenticate when a token is reachable, stay anonymous when it is not. The
# VERDICT RULES ARE IDENTICAL on both paths — binding, roll-call, literal
# "success" — and the cases below assert exactly that, so the credential can
# never become a second, weaker gate.
E_TOKEN="ghs_guardfixturetoken0000000000000000"

# G8 — with a token reachable, BOTH requests carry an Authorization header, and
# the publish still goes through on the same rules.
gh_run_json; gh_jobs_json
OC_GUARD_GH_TOKEN="$E_TOKEN" STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" STATION_HEALTH_RC=0 e2e
check "G8 a green round still publishes when a token is present" "0" "$RC"
check "G8 …and EVERY Actions request was authenticated (both of them)" "2" \
  "$(grep -c '^auth=yes ' "$API_WIRE" || true)"
check "G8 …and none of them went out anonymously" "0" \
  "$(grep -c '^auth=no ' "$API_WIRE" || true)"
# GH_TOKEN is the spelling the auto-beta workflow already sets on the publish
# step; GITHUB_TOKEN is the one the runner exports. Both must work, or the wiring
# silently depends on which of the two a caller happened to set.
OC_GUARD_GH_TOKEN_ALT="$E_TOKEN" STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" STATION_HEALTH_RC=0 e2e
check "G8 GH_TOKEN works as well as GITHUB_TOKEN (the workflow sets GH_TOKEN)" "2" \
  "$(grep -c '^auth=yes ' "$API_WIRE" || true)"
# The token must not leak into the log or into the evidence a publish leaves on
# disk. A credential in dist/release/ci/ outlives the run that used it.
case "$OUT" in
  *"$E_TOKEN"*) bad "G8 …and the token VALUE never appears in the publish output" ;;
  *) ok "G8 …and the token VALUE never appears in the publish output" ;;
esac
check "G8 …nor in any evidence file the run wrote" "0" \
  "$(grep -rl "$E_TOKEN" "$EOUT" 2>/dev/null | wc -l | tr -d '"'"'[:space:]'"'"')"

# G8b — NO token: the anonymous path must still work, unauthenticated, on the
# same rules. This is the negative control for G8: without it, "we authenticate"
# could be satisfied by an implementation that simply refuses without a token.
gh_run_json; gh_jobs_json
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" STATION_HEALTH_RC=0 e2e
check "G8b with NO token the gate still works (anonymous path intact)" "0" "$RC"
check "G8b …and both requests really went out UNauthenticated" "2" \
  "$(grep -c '^auth=no ' "$API_WIRE" || true)"
# …and the anonymous path is held to the SAME rules, not a softer set. If the
# credential ever became the thing that decides how strict the gate is, this is
# the case that catches it.
gh_jobs_json 'macos-e2e=skipped'
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G8b2 the ANONYMOUS path applies the identical verdict rules (a skipped gate job still refuses)" \
  release-ci "$RC" "$OUT"
gh_jobs_json
gh_run_json "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G8b3 …including the binding (a round for another commit still refuses)" \
  ci-run-binding "$RC" "$OUT"

# G8c — TOKEN PRESENT BUT REJECTED (403/401): refuse, and do NOT quietly retry
# anonymously.
#
# THE RULING, AND WHY. A token that cannot read the run is the gate's own
# plumbing being broken — the workflow grants `actions: read` precisely so this
# works — and the honest answer to "I thought I could verify this and I could
# not" is to stop. An anonymous retry would make the verdict's PROVENANCE
# non-deterministic (the same publish verified two different ways on two
# different days, distinguishable only by reading the log closely), which is the
# one property this whole gate is built to have. It would also be useless in the
# case it looks like it protects: anonymous is the path we authenticated to get
# AWAY from, so falling back to it is most likely to hit the very 403 that
# started this. Failing closed costs a red auto-beta, fixable in minutes by
# fixing `permissions:`; a silent downgrade costs a verdict nobody can name.
gh_run_json; gh_jobs_json
OC_GUARD_GH_TOKEN="$E_TOKEN" GH_RUN_RC=22 STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "G8c a REJECTED token fails closed (it is not permission to ship)" \
  release-ci "$RC" "$OUT"
check "G8c …and it did NOT silently retry anonymously" "0" \
  "$(grep -c '^auth=no ' "$API_WIRE" || true)"
check "G8c …and NOTHING was built" "" "$(cat "$BUILD_WIRE")"

# restore the green world for the cases that follow
gh_run_json; gh_jobs_json

# E2/E3 — THE POINT OF THE TICKET. The upload succeeded; the world is still
# wrong; the command must fail anyway and say which item. Before T-588c both of
# these were a green release nobody checked.
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e_stored 'isDraft=true'
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "E2 upload OK but GitHub stored a DRAFT" release-draft "$RC" "$OUT"
check "E2 …and it did upload first (this is a post-upload failure)" "1" \
  "$(grep -c 'release create' "$GHWIRE" || true)"

e2e_stored
STATION_VERSION_JSON='{"git_sha":"aaaaaaa"}' e2e
named_failure "E3 upload+read-back OK but the STATION never moved" station-settle "$RC" "$OUT"

# ── E3b/E3c/E3d: --no-settle (T-9fe3) ───────────────────────────────────────
# The owner ruled that a self-hosted station's upgrade is not a success condition
# for a release, and an unattended runner cannot reach one at all. E3 immediately
# above is this block's NEGATIVE CONTROL and it is the same fake station: on
# ANOTHER commit ('aaaaaaa'), no flag, fails station-settle. The cases below use
# that identical station state, so what changes the verdict is provably the flag
# and not the fixture.
e2e_stored
STATION_VERSION_JSON='{"git_sha":"aaaaaaa"}' e2e --no-settle
check "E3b the SAME station state that fails E3 exits 0 with --no-settle" "0" "$RC"
case "$OUT" in
  *"DELIBERATELY NOT CHECKED"*"self-hosted"*)
    ok "E3b …and says WHY the station was not verified" ;;
  *) bad "E3b …and says WHY the station was not verified (out: $(printf '%s' "$OUT" | tail -c 400))" ;;
esac
# The final line must not be readable as "the station is on this commit". A run
# that skipped the check has to be distinguishable from one that made it, or the
# log tells the next reader something nobody verified.
E3B_LAST="$(printf '%s\n' "$OUT" | grep -v '^$' | tail -n 1)"
case "$E3B_LAST" in
  *"NO station was checked"*) ok "E3b …and the final line does not claim the station moved" ;;
  *) bad "E3b …and the final line does not claim the station moved (last line: $E3B_LAST)" ;;
esac
check "E3b …and it still uploaded exactly once (the flag skips step 8, not the release)" "1" \
  "$(grep -c 'release create' "$GHWIRE" || true)"

# --no-settle must not soften step 7. A read-back violation is still fatal, and
# the item named is still the read-back's — otherwise the flag would be a way to
# publish something GitHub never actually stored correctly.
e2e_stored 'isDraft=true'
STATION_VERSION_JSON='{"git_sha":"aaaaaaa"}' e2e --no-settle
named_failure "E3c --no-settle does NOT skip the read-back (a stored DRAFT still fails)" \
  release-draft "$RC" "$OUT"

# …nor the CI gate. The flag drops step 8 only; a round that is not green still
# stops the release before anything is built.
e2e_stored
gh_jobs_json 'bin-guards=failure'
STATION_VERSION_JSON='{"git_sha":"aaaaaaa"}' e2e --no-settle
named_failure "E3d --no-settle does NOT skip the pre-build CI gate" release-ci "$RC" "$OUT"
check "E3d …and NOTHING was built" "" "$(cat "$BUILD_WIRE")"
# …and not the binding half of it either.
gh_jobs_json
gh_run_json "ffffffffffffffffffffffffffffffffffffffff"
STATION_VERSION_JSON='{"git_sha":"aaaaaaa"}' e2e --no-settle
named_failure "E3d2 --no-settle does NOT skip the round-to-target binding" ci-run-binding "$RC" "$OUT"
gh_run_json

e2e_stored "targetCommitish=\"$(printf '%040d' 0)\""
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "E4 upload OK but the release is bound to another commit" release-target "$RC" "$OUT"

# E5 — REMOVED with the signing machinery (T-0398). It used to assert that
# `--sign` was the ONLY way to sign and that it routed through bin/build-release;
# there is no --sign flag and no bin/build-release any more, so there is nothing
# left for it to pin. The publish arc's own read-back cases (E0-E4, E6, E7) are
# untouched.

# E6 — a failed upload must NOT be followed by a read-back that "passes" against
# a stale/foreign release. It has to stop at the upload.
e2e_stored
GH_WRITE_RC=1 STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
check "E6 a failed gh release create fails the publish" "1" "$RC"
check "E6 …and never proceeds to the read-back" "0" \
  "$(grep -c 'release view' "$GHWIRE" || true)"
unset GH_WRITE_RC

# E7 — the staging worktree is a THROWAWAY and must be cleaned up, or the next
# publish of any tag dies on `git worktree add` refusing an existing path.
check "E7 no staging worktree is left registered in the fixture repo" "1" \
  "$(git -C "$SRC" worktree list | wc -l | tr -d '[:space:]')"

# ═══════════════════════════════════════════════════════════════════════════
# P — PROMOTE. Rebuilds nothing; must detect a re-upload under the flip.
# ═══════════════════════════════════════════════════════════════════════════
echo "── P: promote"
promote() {
  : > "$GHWIRE"
  OUT="$(PATH="$SHIMDIR:$PATH" OC_RELEASE_GH_REPO="guard/fixture" \
    bash "$RELEASE" promote "$@" 2>&1)"
  RC=$?
}

# The gh shim answers every `release view` identically, so a promote whose
# before/after agree is the happy path.
export GH_VIEW_JSON="$(stored_json 'isPrerelease=false')"
promote "$TAG"
named_failure "P0 promote refuses a tag that is ALREADY final" release-channel "$RC" "$OUT"

# Happy promote needs the pre-flip view to be a prerelease and the post-flip view
# to be final — two different answers from one shim, so it switches on a counter.
# /releases/latest switches the SAME way, on whether `release edit` (the flip)
# has already happened: before it, this tag is not GitHub's latest; after it,
# it is — which is what a promote that actually applied --latest=true would
# cause GitHub to report. This is what lets P1's post-flip isLatest=true
# enforcement pass without a per-case GH_LATEST_JSON.
cat > "$SHIMDIR/gh" <<'SH'
#!/usr/bin/env bash
echo "gh $*" >> "$GH_WIRE"
if [[ "${1:-}" == "release" && "${2:-}" == "view" ]]; then
  n="$(grep -c 'release view' "$GH_WIRE")"
  if [[ "$n" == "1" ]]; then printf '%s' "${GH_VIEW_BEFORE:-}"; else printf '%s' "${GH_VIEW_AFTER:-}"; fi
  exit 0
fi
if [[ "${1:-}" == "api" ]]; then
  case "${2:-}" in
    */releases/latest)
      if [[ -n "${GH_LATEST_JSON+set}" ]]; then
        printf '%s' "${GH_LATEST_JSON}"
      elif grep -q 'release edit' "$GH_WIRE"; then
        printf '{"tag_name":"%s"}' "$TAG"
      fi
      exit 0 ;;
  esac
fi
if [[ "${1:-}" == "release" && ( "${2:-}" == "create" || "${2:-}" == "edit" ) ]]; then
  exit "${GH_WRITE_RC:-0}"
fi
echo "unexpected gh invocation: $*" >&2
exit 99
SH
chmod +x "$SHIMDIR/gh"
unset GH_VIEW_JSON

export GH_VIEW_BEFORE="$(stored_json)"
export GH_VIEW_AFTER="$(stored_json 'isPrerelease=false')"
promote "$TAG"
check "P1 a clean promote exits 0" "0" "$RC"
check "P1 …and flipped it with exactly one gh release edit" "1" \
  "$(grep -c 'release edit' "$GHWIRE" || true)"

promote --dry-run "$TAG"
check "P2 --dry-run promote exits 0" "0" "$RC"
check "P2 --dry-run promote NEVER flips anything" "0" \
  "$(grep -c 'release edit' "$GHWIRE" || true)"

# THE promote-specific invariant: promote is a metadata-only flip, so if the
# asset set moved under it, somebody re-uploaded and the promoted bytes are not
# the bytes anyone tested. That is a failure, not a warning.
GH_VIEW_AFTER="$(stored_json 'isPrerelease=false' "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"uploaded","size":999999},
  {"name":"install.sh","state":"uploaded","size":2048},
  {"name":"checksums.txt","state":"uploaded","size":128}]')")" \
  promote "$TAG"
named_failure "P3 an asset was RE-UPLOADED under the flip (size changed)" promote-assets "$RC" "$OUT"

# The target moving under a metadata-only flip is caught INSIDE the post-flip
# read-back, because promote passes the pre-flip target as the expected one — so
# the item named is release-target, with both values in the message. Asserting the
# real name here (rather than a promote-specific one) is deliberate: the moment a
# case is written against a nicer-sounding item that nothing emits, it stops being
# a test of the code and becomes a test of the wish.
GH_VIEW_AFTER="$(stored_json 'isPrerelease=false' 'targetCommitish="ffffffffffffffffffffffffffffffffffffffff"')" \
  promote "$TAG"
named_failure "P4 the target commit MOVED under the flip" release-target "$RC" "$OUT"
case "$OUT" in
  *"ffffffffffffffffffffffffffffffffffffffff"*"$SHA"*) ok "P4 …naming both the stored and the expected commit" ;;
  *) bad "P4 …naming both the stored and the expected commit (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
esac

GH_VIEW_AFTER="$(stored_json 'isPrerelease=true')" promote "$TAG"
named_failure "P5 the flip did not take (still a prerelease afterwards)" release-channel "$RC" "$OUT"

GH_WRITE_RC=1 promote "$TAG"
check "P6 a failed gh release edit fails the promote" "1" "$RC"
unset GH_WRITE_RC

# P7 — THE TICKET ITSELF: `gh release edit --latest=true` returned success, but
# GitHub's own /releases/latest still does NOT say this tag — the exact shape a
# silently-ignored flag, a propagation lag, or a promote that forgot to read
# --latest back would produce. GH_LATEST_JSON is set to a DIFFERENT tag
# explicitly (overriding the shim's default "release edit happened -> now
# latest" simulation), so this is reachable ONLY if cmd_promote itself demands
# isLatest=true on the post-flip read-back — not just verify_stored_release
# having the capability. Deleting that demand from cmd_promote (passing "" or
# omitting the 4th argument to the AFTER verify_stored_release call) makes this
# case pass silently at exit 0 instead of failing — that is the mutant this
# case exists to catch.
GH_LATEST_JSON='{"tag_name":"v0.0.1-someone-elses-release"}' promote "$TAG"
named_failure "P7 gh release edit succeeded but GitHub never marked it Latest" release-latest "$RC" "$OUT"
case "$OUT" in
  *"gh release edit $TAG --repo"*"--latest=true"*) ok "P7 …names an executable next step (gh release edit ... --latest=true)" ;;
  *) bad "P7 …names an executable next step (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
esac
unset GH_LATEST_JSON

echo "release guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
