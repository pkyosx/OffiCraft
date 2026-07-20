#!/usr/bin/env bash
# bin/tests/port-default.sh — HERMETIC unit tests for the DEFAULT-PORT contract
# that spans oc.toml.example and bin/ocserver's render_oc_toml.
#
# THE COUPLING UNDER TEST
# -----------------------
# render_oc_toml rewrites the example's default-port line by LITERAL string
# match. It carries its own guard: if the literal is absent it raises rather
# than writing a config with the wrong port. That guard is a hardcoded copy of
# the same number the template ships, so the two are a contract with no
# compiler behind it: change the template alone and every ocserver-managed
# install dies at render time; change the guard alone and the guard silently
# stops guarding.
#
# WHY ASSERT THE MESSAGE, NOT JUST THE EXIT CODE
# ----------------------------------------------
# "correctly refused" and "fell over for an unrelated reason" share exit 1.
# A suite that only checks rc is structurally blind to the difference — it
# would go green on a render that aborted because python3 was missing, or
# because the tempdir was unwritable. So every failure case below asserts on
# the REASON in the output; the exit code is the weaker, secondary witness.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
OCSERVER="$ROOT/bin/ocserver"
EXAMPLE="$ROOT/oc.toml.example"
[[ -f "$OCSERVER" ]] || { echo "FATAL: bin/ocserver not found at $OCSERVER" >&2; exit 2; }
[[ -f "$EXAMPLE" ]]  || { echo "FATAL: oc.toml.example not found at $EXAMPLE" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-port-default.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

DSN="sqlite:///$WORK/officraft.db"

echo "default-port contract — oc.toml.example ↔ bin/ocserver render guard"

# ── 1. the template ships the standard default ──────────────────────────────
# Line-anchored: a `port = 7755` buried in a comment must not satisfy this.
if grep -qE '^port = 7755$' "$EXAMPLE"; then
  ok "oc.toml.example ships the standard default port line (port = 7755)"
else
  bad "oc.toml.example does not ship 'port = 7755' as an active line"
fi

# ── 2. the pin-a-port render works against the shipped template ─────────────
# This is the path every ocserver-managed install takes. If the guard's literal
# and the template have drifted, this is where it detonates.
OUT="$(bash "$OCSERVER" render-config "$EXAMPLE" "$WORK/pinned.toml" "$DSN" "" 9312 2>&1)"; RC=$?
check "render-config with an explicit port: succeeds" "0" "$RC"
if [[ "$RC" == 0 ]] && grep -qE '^port = 9312$' "$WORK/pinned.toml"; then
  ok "render-config with an explicit port: the pinned port lands in the output"
else
  bad "render-config with an explicit port: 9312 is not in the rendered file (output: '$OUT')"
fi

# ── 3. the render leaves NO trace of the default once a port is pinned ──────
if [[ "$RC" == 0 ]] && ! grep -qE '^port = 7755$' "$WORK/pinned.toml"; then
  ok "render-config with an explicit port: the template default is replaced, not duplicated"
else
  bad "render-config with an explicit port: an active 'port = 7755' line survived the pin"
fi

# ── 4. THE GUARD ITSELF — assert the REASON, not just the failure ───────────
# Feed a template whose default-port line has drifted away from what the guard
# expects (exactly the "template changed, guard didn't" state). The render must
# refuse AND say which line it could not find. Asserting the message is the
# whole point: without it this case cannot tell a working guard apart from an
# unrelated crash.
sed 's/^port = 7755$/port = 8780/' "$EXAMPLE" > "$WORK/drifted.example"
OUT="$(bash "$OCSERVER" render-config "$WORK/drifted.example" "$WORK/drifted.toml" "$DSN" "" 9312 2>&1)"; RC=$?
if [[ "$RC" != 0 ]]; then
  ok "drifted template: render-config refuses (non-zero)"
else
  bad "drifted template: render-config SUCCEEDED — the guard is not guarding"
fi
case "$OUT" in
  *"missing the \`port = 7755\` line"*)
    ok "drifted template: the refusal NAMES the missing line (not a generic crash)" ;;
  *)
    bad "drifted template: refusal did not name the missing 'port = 7755' line (output: '$OUT')" ;;
esac
if [[ -f "$WORK/drifted.toml" ]]; then
  bad "drifted template: a config was written despite the refusal"
else
  ok "drifted template: nothing was written (fail-closed)"
fi

# ── 5. the 3-arg test seam keeps the template's own default ─────────────────
# No port argument = no rewrite, so the rendered file must carry the shipped
# default verbatim. This is what pins the template's number as the number a
# config-less consumer inherits.
OUT="$(bash "$OCSERVER" render-config "$EXAMPLE" "$WORK/plain.toml" "$DSN" 2>&1)"; RC=$?
check "render-config without a port: succeeds" "0" "$RC"
if [[ "$RC" == 0 ]] && grep -qE '^port = 7755$' "$WORK/plain.toml"; then
  ok "render-config without a port: the shipped default (7755) is what comes out"
else
  bad "render-config without a port: 'port = 7755' is not in the rendered file (output: '$OUT')"
fi

echo "port-default tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
