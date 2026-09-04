#!/usr/bin/env bash
# Reminds maintainers to re-read owner-facing restatements when the canonical
# authority-conflict rule changes (T-c19c).
#
# The operational rule lives in the "來源衝突時" paragraph of
# seeds/system_interaction.md §2.2. CI metadata used to be embedded in that
# seed and in the restatements as HTML comments. Those comments were visible in
# the cockpit and in agent boot context, so the reminder now lives here, in the
# test plane, instead.
#
# This guard compares the current canonical paragraph with the last-reviewed
# digest recorded below. A changed paragraph fails and requires a human to
# re-read the listed restatement files before updating the digest. It does not
# inspect prose for semantic fidelity and a green result is not proof that no
# unregistered copy exists.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SELF_REL="bin/tests/rule-defer-guard.sh"
SEED="$ROOT/seeds/system_interaction.md"
RULE_START="**來源衝突時**"
RULE_END="使用請示卡時，先依你需要負責人做的事選擇種類："

# Update this only after a human has re-read every listed restatement against
# the changed canonical paragraph.
REVIEWED_RULE_HASH="9f11cb39088a535a90ecf83cdfa7e9988a782d94eb203390d6bed7ff77a0104d"

required_sites=(
  "CLAUDE.md"
  "docs/guide/best-practices.md"
)

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

sha256_of_stdin() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 | awk '{print $1}'
  else
    sha256sum | awk '{print $1}'
  fi
}

extract_rule() {
  awk -v start="$RULE_START" -v end="$RULE_END" '
    index($0, start) == 1 { in_rule = 1 }
    in_rule {
      if (index($0, end) == 1) exit
      print
    }
  ' "$1"
}

[[ -f "$SEED" ]] || {
  bad "seed file missing: $SEED"
  exit 1
}

RULE="$(extract_rule "$SEED")"
[[ -n "${RULE//[[:space:]]/}" ]] || {
  bad "canonical rule paragraph not found between $RULE_START and $RULE_END"
  exit 1
}

CURRENT_RULE_HASH="$(printf '%s\n' "$RULE" | sha256_of_stdin)"
if [[ "$CURRENT_RULE_HASH" == "$REVIEWED_RULE_HASH" ]]; then
  ok "canonical rule digest is reviewed ($CURRENT_RULE_HASH)"
else
  bad "canonical rule digest changed (current $CURRENT_RULE_HASH, reviewed $REVIEWED_RULE_HASH)"
  bad "re-read every listed restatement against seeds/system_interaction.md §2.2, then update REVIEWED_RULE_HASH in $SELF_REL"
fi

for site in "${required_sites[@]}"; do
  if [[ -f "$ROOT/$site" ]]; then
    ok "restatement site exists: $site"
  else
    bad "restatement site is missing: $site — update required_sites in $SELF_REL if it was intentionally moved"
  fi
done

# Prove that a one-character edit inside the canonical paragraph changes the
# digest. The real seed is never modified.
MUT_DIR="$(mktemp -d -t oc-rule-defer.XXXXXX)"
trap 'rm -rf "$MUT_DIR"' EXIT
MUT_SEED="$MUT_DIR/seed.md"
awk -v start="$RULE_START" '
  index($0, start) == 1 && !done {
    print $0 "X"
    done = 1
    next
  }
  { print }
' "$SEED" >"$MUT_SEED"

if cmp -s "$SEED" "$MUT_SEED"; then
  bad "mutant — could not perturb the canonical rule paragraph"
else
  MUT_RULE="$(extract_rule "$MUT_SEED")"
  MUT_RULE_HASH="$(printf '%s\n' "$MUT_RULE" | sha256_of_stdin)"
  if [[ "$MUT_RULE_HASH" != "$CURRENT_RULE_HASH" ]]; then
    ok "mutant — a one-character rule edit changes the digest ($CURRENT_RULE_HASH -> $MUT_RULE_HASH)"
  else
    bad "mutant — a one-character rule edit did not change the digest"
  fi
fi

RUNNER="$ROOT/bin/tests/run.sh"
if grep -qE '^[[:space:]]*RULEDEFER="\$HERE/rule-defer-guard\.sh"[[:space:]]*$' "$RUNNER" &&
  grep -qE '^[[:space:]]*if run_guard "\$RULEDEFER"; then[[:space:]]*$' "$RUNNER"; then
  ok "bin/tests/run.sh still dispatches this guard through run_guard"
else
  bad "bin/tests/run.sh no longer dispatches this guard"
fi

echo "rule-defer review-digest tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
