#!/usr/bin/env bash
# bin/tests/station-sha-header-guard.sh — the SSE station-sha header name is one
# contract written down TWICE, and nothing else makes a mismatch fail (T-5b83).
#
# WHY THIS EXISTS. ocagent's connection line names the build it just attached
# to; the sha rides the SSE response headers. Server and client live in
# DIFFERENT Go modules that cannot import each other, so the header name is
# necessarily spelled in two places:
#
#   server/ocserverd/api_infra.go   const sseStationSHAHeader = "..."
#   cli/ocagent/listen.go           const stationSHAHeader    = "..."
#
# 🔴 A MISMATCH IS SILENT. Header.Get on a name nobody sent returns "" and the
# client's suffix is simply omitted — which is BYTE-IDENTICAL to the honest
# "this station sent no sha" (an older station, a proxy that stripped it). So
# the failure mode and the designed-for degradation look exactly the same, and
# no test, compiler or type can tell them apart. That is what this guard buys.
#
# It also checks each half is actually WIRED, not merely declared: a constant
# nobody passes to Set/Get is the same silence by another route.
#
# 🔴 EMPTY-EXTRACTION IS THE TRAP THIS GUARD MUST NOT FALL INTO. If either
# pattern stops matching (someone reformats the const block), both sides
# extract "" — and "" == "" would PASS. So emptiness is checked BEFORE
# equality, and case 4 plants a real mutant to prove the comparison can still
# go red. A guard nobody verified prints all green either way.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

SERVER_FILE="$ROOT/server/ocserverd/api_infra.go"
CLIENT_FILE="$ROOT/cli/ocagent/listen.go"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

# Anchored at the ASSIGNMENT, so the prose in either file's comments — which
# names the other module's constant on purpose — cannot satisfy it. Comment
# lines start with // and can never match a leading-whitespace-then-identifier
# assignment.
extract() { # <file> <const-name>
  sed -nE "s/^[[:space:]]*$2[[:space:]]*=[[:space:]]*\"([^\"]*)\".*/\1/p" "$1" | head -1
}

# ── 1. both halves are declared and NON-EMPTY ────────────────────────────────
SRV="$(extract "$SERVER_FILE" sseStationSHAHeader)"
CLI="$(extract "$CLIENT_FILE" stationSHAHeader)"

if [[ -n "$SRV" ]]; then
  ok "server declares sseStationSHAHeader ('$SRV')"
else
  bad "server/ocserverd/api_infra.go: could not extract sseStationSHAHeader (pattern stopped matching?)"
fi
if [[ -n "$CLI" ]]; then
  ok "client declares stationSHAHeader ('$CLI')"
else
  bad "cli/ocagent/listen.go: could not extract stationSHAHeader (pattern stopped matching?)"
fi

# ── 2. they agree ────────────────────────────────────────────────────────────
if [[ -n "$SRV" && -n "$CLI" ]]; then
  if [[ "$SRV" == "$CLI" ]]; then
    ok "both modules spell the header identically"
  else
    bad "header name MISMATCH — server sends '$SRV', client reads '$CLI'. The sha would silently vanish from every connection line, indistinguishable from a station that sent none."
  fi
fi

# ── 3. each half is WIRED, not just declared ─────────────────────────────────
# Code lines only: the comments deliberately discuss the other side.
#
# ⚠️ COUNT, never `grep -q`, on the downstream end of a pipe. `-q` exits the
# moment it matches, the upstream grep takes SIGPIPE, and under `pipefail` the
# whole pipeline reports non-zero — so a SUCCESSFUL match reads as a failure.
# This guard was written with `-q` first and the client check went red while
# the code was perfectly correct; the server check (one small file, written in
# a single flush) stayed green. A bug whose appearance depends on file size is
# exactly the kind that gets "fixed" by editing the code under test.
code_hits() { # <needle> <file...>
  local needle="$1"; shift
  grep -vE '^[[:space:]]*//' "$@" | grep -cF "$needle"
}

if [[ "$(code_hits 'Header().Set(sseStationSHAHeader' "$SERVER_FILE")" -ge 1 ]]; then
  ok "server actually stamps the header onto the SSE response"
else
  bad "server declares the constant but never passes it to Header().Set — the header is never sent"
fi
if [[ "$(code_hits 'Header.Get(stationSHAHeader' "$CLIENT_FILE" "$ROOT/cli/ocagent/listen_run.go")" -ge 1 ]]; then
  ok "client actually reads the header off its own response"
else
  bad "client declares the constant but never passes it to Header.Get — the sha is never read"
fi

# ── 4. POSITIVE CONTROL: a planted mismatch MUST be caught ───────────────────
# Without this, "all green" is equally consistent with a comparison that can no
# longer fail. Runs the same extract+compare over a throwaway copy.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp "$CLIENT_FILE" "$WORK/listen.go"
# Flip one character of the client's value only — the realistic typo.
sed -iE "s/^([[:space:]]*stationSHAHeader[[:space:]]*=[[:space:]]*\")([^\"]*)(\".*)/\1\2X\3/" "$WORK/listen.go" 2>/dev/null \
  || sed -i '' -E "s/^([[:space:]]*stationSHAHeader[[:space:]]*=[[:space:]]*\")([^\"]*)(\".*)/\1\2X\3/" "$WORK/listen.go"
MUT="$(extract "$WORK/listen.go" stationSHAHeader)"
if [[ -z "$MUT" ]]; then
  bad "positive control could not plant a mutant (the extractor or the sed no longer matches) — a green run above proves nothing"
elif [[ "$MUT" == "$SRV" ]]; then
  bad "positive control: planted mutant '$MUT' still equals the server's '$SRV' — the comparison cannot go red"
else
  ok "positive control: a one-character typo IS detected ('$MUT' != '$SRV')"
fi

echo "station-sha-header tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
