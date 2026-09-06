#!/usr/bin/env bash
# bin/tests/gen-migration-lock-signal-guard.sh — bin/gen-migration-lock must
# never claim the lock is current without having checked (T-75).
#
# WHAT WENT WRONG. The script's last lines were an UNCONDITIONAL
# "server/ocserverd/migration.lock is up to date.", printed AFTER the generator
# had already rewritten the entire file. It compared nothing; it could not have
# been false. On 2026-09-04 a reader took `tail -5` of a run, saw that sentence,
# and concluded the file had not moved. It had.
#
# WHY THAT CANNOT BE CAUGHT ANYWHERE ELSE. Every existing check around this
# script is about whether the GENERATOR ran: the wrapper greps for the test's
# end marker, and migration_lock_t75_test.go checks the lock's CONTENT. Nothing
# looks at the sentence the human reads. The lock can be correct, the generator
# green, the whole suite green, and the closing line still be a lie about
# whether the reader has work to do — which is precisely the state that shipped.
#
# WHAT IS ASSERTED. The reporting stage is driven through the script's hidden
# `--report-state` seam against four purpose-built git fixtures, so this runs
# without a Go toolchain, a build, or a `go test`:
#
#   F1 committed and clean   -> must say UNCHANGED
#   F2 committed and dirty   -> must say CHANGED, and must NOT claim otherwise
#   F3 untracked lock        -> must say CANNOT DETERMINE
#   F4 no git on PATH        -> must say CANNOT DETERMINE
#
# F2 is the regression itself. F3 and F4 are the honesty half: when git cannot
# answer, the script must say it did not check rather than fall back to the
# reassuring sentence — an unverified all-clear is what made the original bug
# expensive, because it stopped the reader from looking.
#
# WHAT IS NOT ASSERTED. This guard never runs the generator, so it says nothing
# about whether migration.lock's CONTENT is right; that is
# migration_lock_t75_test.go's job. It also does not prove the real run wires
# ENTRIES_LINE correctly beyond the pass-through checked here.
set -euo pipefail
# 🔴 WITHOUT THIS, THE FIXTURES CAN FAIL SILENTLY AND THE GUARD STILL SAYS ok.
# Command substitution runs in a subshell that does NOT inherit errexit unless this
# is set (bash 4.4+), and every fixture below is built inside one — `F1="$(make_fixture
# ...)"`. An independent reviewer sabotaged `git commit` in make_fixture and measured
# it: five failed commits, and F2/F3/F4/F5 all still reported ok, because the only
# thing the substitution returns is the status of its LAST command, a printf that
# always succeeds. Four of five assertions were being made against repos that were
# never built. That is the same false-green this whole guard exists to prevent, one
# level up — so the fix is belt AND braces: this shopt where it exists, and an
# explicit post-condition inside make_fixture for the bash 3.2 that ships on macOS,
# where this option does not exist at all.
shopt -s inherit_errexit 2>/dev/null || true

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SELF_REL="bin/tests/gen-migration-lock-signal-guard.sh"
SCRIPT="$ROOT/bin/gen-migration-lock"
LOCK_REL="server/ocserverd/migration.lock"

# The exact sentences the script may print. Kept whole rather than as loose
# keywords: "CHANGED" is a substring of "UNCHANGED", so a keyword grep here
# would pass on a script that says the opposite of what it means.
UNCHANGED_CLAIM='is UNCHANGED — what was just written is'
CHANGED_CLAIM='CHANGED — this run rewrote it'
UNKNOWN_CLAIM='RESULT: CANNOT DETERMINE'
OLD_BUG_CLAIM='is up to date'

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

[[ -f "$SCRIPT" ]] || {
  bad "bin/gen-migration-lock is missing"
  exit 1
}

TMP="$(mktemp -d -t oc-genlock-signal.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT

# make_fixture NAME COMMIT DIRTY — a throwaway repo holding only a copy of the
# script and a lock file. The real repo is never touched.
make_fixture() {
  local name="$1" commit="$2" dirty="$3"
  local d="$TMP/$name"
  mkdir -p "$d/bin" "$d/$(dirname "$LOCK_REL")"
  cp "$SCRIPT" "$d/bin/gen-migration-lock"
  printf 'seed line\n' >"$d/$LOCK_REL"
  git -C "$d" init -q
  git -C "$d" config user.email guard@example.invalid
  git -C "$d" config user.name guard
  git -C "$d" add bin/gen-migration-lock >/dev/null
  if [[ "$commit" == "commit" ]]; then
    git -C "$d" add "$LOCK_REL" >/dev/null
  fi
  git -C "$d" commit -qm fixture >/dev/null
  if [[ "$dirty" == "dirty" ]]; then
    printf 'a migration this run appended\n' >>"$d/$LOCK_REL"
  fi
  # POST-CONDITION, load-bearing on bash 3.2 where inherit_errexit does not exist:
  # prove the repo we are about to assert against actually got built. A fixture whose
  # `git init/add/commit` failed looks exactly like a good one from the caller's side,
  # and every assertion made against it then passes vacuously.
  git -C "$d" rev-parse --verify HEAD >/dev/null 2>&1 || {
    printf 'make_fixture %s: the fixture repo has no commit — refusing to assert against it\n' "$name" >&2
    return 1
  }
  printf '%s' "$d"
}

# report FIXTURE-DIR [PATH-OVERRIDE] — run the reporting stage only.
report() {
  local d="$1" pathenv="${2:-}"
  if [[ -n "$pathenv" ]]; then
    PATH="$pathenv" bash "$d/bin/gen-migration-lock" --report-state 'wrote migration.lock: 42 entries' 2>&1
  else
    bash "$d/bin/gen-migration-lock" --report-state 'wrote migration.lock: 42 entries' 2>&1
  fi
}

says() { grep -qF -- "$2" <<<"$1"; }

# ── F1: committed, clean ─────────────────────────────────────────────────────
F1="$(make_fixture clean commit clean)" || exit 1
OUT1="$(report "$F1")"
if says "$OUT1" "$UNCHANGED_CLAIM"; then
  ok "F1 clean tree — reports the lock as UNCHANGED"
else
  bad "F1 clean tree — did not report the lock as UNCHANGED. Got:"
  printf '%s\n' "$OUT1" >&2
fi
if says "$OUT1" "$CHANGED_CLAIM"; then
  bad "F1 clean tree — also claimed the lock CHANGED; the two states are mutually exclusive"
else
  ok "F1 clean tree — does not also claim the lock changed"
fi

# ── F2: committed, dirty — THE REGRESSION ────────────────────────────────────
F2="$(make_fixture dirty commit dirty)" || exit 1
OUT2="$(report "$F2")"
if says "$OUT2" "$CHANGED_CLAIM"; then
  ok "F2 lock actually changed — reports it CHANGED and points at the diff"
else
  bad "F2 lock actually changed — did not report it as CHANGED. Got:"
  printf '%s\n' "$OUT2" >&2
fi
if says "$OUT2" "$UNCHANGED_CLAIM" || says "$OUT2" "$OLD_BUG_CLAIM"; then
  bad "F2 lock actually changed — the script STILL claims the lock is current. This is the"
  bad "     2026-09-04 regression: an unconditional all-clear printed after the file was"
  bad "     rewritten. Make the closing message depend on the git answer in $SELF_REL's subject."
  printf '%s\n' "$OUT2" >&2
else
  ok "F2 lock actually changed — makes no up-to-date claim"
fi

# ── F3: lock untracked — git has no baseline ─────────────────────────────────
F3="$(make_fixture untracked nocommit clean)" || exit 1
OUT3="$(report "$F3")"
if says "$OUT3" "$UNKNOWN_CLAIM" && ! says "$OUT3" "$UNCHANGED_CLAIM" && ! says "$OUT3" "$OLD_BUG_CLAIM"; then
  ok "F3 untracked lock — says it CANNOT DETERMINE instead of guessing"
else
  bad "F3 untracked lock — must say it cannot determine the state, never fall back to an"
  bad "     all-clear. Got:"
  printf '%s\n' "$OUT3" >&2
fi

# ── F4: git not available at all ─────────────────────────────────────────────
NOGIT="$TMP/nogit-bin"
mkdir -p "$NOGIT"
# bash and env are in here because the stripped PATH is also what `bash` itself
# is looked up on; without them the fixture fails with 127 and proves nothing.
for tool in bash env dirname basename sed grep cat mktemp rm tail; do
  if p="$(command -v "$tool" 2>/dev/null)"; then ln -sf "$p" "$NOGIT/$tool"; fi
done
if PATH="$NOGIT" command -v git >/dev/null 2>&1; then
  bad "F4 setup — git is still reachable on the stripped PATH; this fixture proves nothing"
else
  F4="$(make_fixture nogit commit clean)" || exit 1
  OUT4="$(report "$F4" "$NOGIT")"
  if says "$OUT4" "$UNKNOWN_CLAIM" && ! says "$OUT4" "$UNCHANGED_CLAIM" && ! says "$OUT4" "$OLD_BUG_CLAIM"; then
    ok "F4 no git on PATH — says it CANNOT DETERMINE instead of guessing"
  else
    bad "F4 no git on PATH — must say it cannot determine the state. Got:"
    printf '%s\n' "$OUT4" >&2
  fi
fi

# ── F5: git holds NO version of the lock path at all ─────────────────────────
# `git status --porcelain -- <path>` is empty for TWO different reasons: the
# tracked file matches its committed version, and git has never heard of the
# path (missing / ignored / the LOCK_REL constant drifted away from what the
# generator writes). Reading the second as "unchanged" reprints the very
# sentence this script was written to delete, so the empty case must first
# prove git can see the path. Found by the independent reviewer of PR #417,
# who reproduced it against the pre-fix script.
F5="$(make_fixture nolockfile nocommit clean)" || exit 1
rm -f "$F5/$LOCK_REL"
if [[ -n "$(git -C "$F5" status --porcelain -- "$LOCK_REL")" ]]; then
  bad "F5 setup — porcelain is not empty for a path git never heard of; this fixture proves nothing"
else
  OUT5="$(report "$F5")"
  if says "$OUT5" "$UNKNOWN_CLAIM" && ! says "$OUT5" "$UNCHANGED_CLAIM" && ! says "$OUT5" "$OLD_BUG_CLAIM"; then
    ok "F5 git holds no version of the lock path — an empty diff is reported as CANNOT DETERMINE, not as UNCHANGED"
  else
    bad "F5 git holds no version of the lock path — an empty diff was read as proof of sameness. Got:"
    printf '%s\n' "$OUT5" >&2
  fi
fi

# ── the generator's own count is carried through, not recomputed ─────────────
if says "$OUT2" 'wrote migration.lock: 42 entries'; then
  ok "the generator's entry count is passed through verbatim"
else
  bad "the generator's entry count was dropped; 'how many were written' is half the signal"
  printf '%s\n' "$OUT2" >&2
fi

# ── dispatch ─────────────────────────────────────────────────────────────────
RUNNER="$ROOT/bin/tests/run.sh"
if grep -qE '^[[:space:]]*GENLOCKSIGNAL="\$HERE/gen-migration-lock-signal-guard\.sh"[[:space:]]*$' "$RUNNER" &&
  grep -qE '^[[:space:]]*if run_guard "\$GENLOCKSIGNAL"; then[[:space:]]*$' "$RUNNER"; then
  ok "bin/tests/run.sh still dispatches this guard through run_guard"
else
  bad "bin/tests/run.sh no longer dispatches this guard"
fi

echo "gen-migration-lock signal tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
