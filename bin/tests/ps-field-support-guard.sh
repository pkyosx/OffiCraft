#!/usr/bin/env bash
# Proves the `ps` output fields the warden's cutover-effect probe asks for
# ACTUALLY EXIST on this host, and that their output parses.
#
# ── the bug this guard exists to stop from coming back (T-1ac8) ───────────────
# cli/ocwarden/cutovereffect.go read a process's age with `ps -p <pid> -o
# etimes=`. `etimes` (seconds) is a GNU/procps extension. BSD ps — macOS, the
# ONLY platform this warden runs on, because the anchor identity question is a
# TCC question — does not have it:
#
#     $ ps -p 1 -o etimes=
#     ps: etimes: keyword not found        # rc=1
#
# The probe therefore could not read ANY age on ANY machine in the fleet. Every
# verdict folded to `unproven` forever: the three-valued light the whole feature
# exists to provide had exactly one reachable value, and the red state — a
# machine whose cutover has NOT taken effect, the incident this all answers to —
# could never appear.
#
# 🔴 WHY A UNIT TEST CANNOT REPLACE THIS. The Go suite reaches `ps` through a
# seam, and the fake was keyed on the same wrong argv the production code used.
# So the fake answered the broken flag politely, the suite went green, and the
# green ACTIVELY ENDORSED the defect. Three separate readings of that code —
# author, independent reviewer, ticket owner — all missed it, because all three
# were reading rather than running. The only evidence that discriminates here is
# executing the real binary on a real host.
#
# This guard is HOST-SHAPED on purpose: it asserts a property of the machine CI
# runs on, not of the source. That is the point, and it survived the verdict
# moving: the bin-guards cell that runs it is on macOS, like the machines it
# describes, so a host-shaped assertion is as authoritative as the rest of the
# gate wherever the gate runs.
# (This paragraph used to justify itself with "the repo's land authority is the
# local bin/ci.sh run on a fleet machine". That premise is gone — owner ruled on
# 2026-08-11, card rc-c16ac4679fab, that the merge verdict is the cloud round.
# The conclusion did not depend on it, but the sentence did, so it is rewritten
# rather than left standing on a premise nobody holds any more.)
#
# ── WHAT IT DOES *NOT* COVER ─────────────────────────────────────────────────
#  1. It proves the fields exist HERE. A fleet machine on a different OS
#     revision is only covered when CI runs there.
#  2. The flags are extracted from the Go source, so an argv assembled
#     dynamically would be invisible to it (same static-matching limit as
#     bin/tests/go-test-nocache-guard.sh). The companion Go test pins the argv
#     string itself, which is the half this cannot see.
#  3. It says nothing about whether the VERDICT built from these readings is
#     right — that is the judge's truth table.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SRC="$ROOT/cli/ocwarden/cutovereffect.go"
SHAPE_SRC="$ROOT/cli/ocwarden/cutover.go"
PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

[[ -f "$SRC" ]] || { echo "FATAL: $SRC not found" >&2; exit 2; }

# ── which fields does the source actually ask for? ───────────────────────────
# Read them OUT OF THE CODE rather than hand-copying them here: a guard holding
# its own copy of the argv is one more place to drift, and drift is the whole
# defect.
#
# ⚠️ PER FILE, not pooled. Pooling was this guard's own version of the bug it
# exists to catch: with the two files' fields merged into one list, a call site
# that stopped matching the pattern left the list non-empty (the OTHER file's
# field was still in it) and the guard went green having checked nothing about
# the probe that vanished. Mutation testing caught exactly that. Every scanned
# file must contribute at least one field, or this guard says so.
#
# ⚠️ NO `mapfile` HERE. It is a bash 4+ builtin and macOS ships bash 3.2 as
# /bin/bash — a fleet machine without a newer bash on PATH aborts the guard with
# "mapfile: command not found" instead of checking anything. Found by running
# this guard on a SECOND machine, which is the same lesson the guard itself
# exists to teach: it had been verified on exactly one host.
FIELDS=""
for src in "$SRC" "$SHAPE_SRC"; do
  found=()
  while IFS= read -r field; do
    [[ -n "$field" ]] && found+=("$field")
  done < <(
    grep -hoE '"-o", "[a-z_]+="' "$src" \
      | sed -E 's/.*"-o", "([a-z_]+)=".*/\1/' | sort -u
  )
  if [[ ${#found[@]} -eq 0 ]]; then
    bad "no 'ps -o <field>=' call site found in ${src#"$ROOT"/} — either the probe moved or it is now assembled in a way this scan cannot see, and in both cases this guard has stopped checking it"
    continue
  fi
  printf '  ..   — %s asks for: %s\n' "${src#"$ROOT"/}" "${found[*]}"
  FIELDS="$FIELDS ${found[*]}"
done
FIELDS="$(printf '%s\n' $FIELDS | sort -u)"
if [[ -z "$FIELDS" ]]; then
  printf 'ps-field-support: %d ok, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

for field in $FIELDS; do
  if ! out="$(ps -p $$ -o "${field}=" 2>&1)"; then
    bad "ps -o ${field}= is NOT supported by this host's ps (rc!=0): ${out%%$'\n'*}"
    continue
  fi
  out="$(printf '%s' "$out" | tr -d ' ')"
  if [[ -z "$out" ]]; then
    bad "ps -o ${field}= exited 0 but printed nothing — an unusable reading is not support"
    continue
  fi
  ok "ps -o ${field}= is supported and answered (${out})"
done

# ── the elapsed field additionally has to PARSE ──────────────────────────────
# Supported-and-non-empty is not enough for the one field a number is derived
# from: `etime` prints [[dd-]hh:]mm:ss, and a reader expecting bare seconds
# would silently take "01:48:50" for 1 second.
if elapsed="$(ps -p $$ -o etime= 2>/dev/null)"; then
  elapsed="$(printf '%s' "$elapsed" | tr -d ' ')"
  if [[ "$elapsed" =~ ^(([0-9]+)-)?(([0-9]{1,2}):)?([0-9]{1,2}):([0-9]{2})$ ]]; then
    ok "ps -o etime= output '${elapsed}' matches the [[dd-]hh:]mm:ss shape the parser accepts"
  else
    bad "ps -o etime= printed '${elapsed}', which is NOT the [[dd-]hh:]mm:ss shape cutovereffect.go parses — the age would be read as unavailable on every machine"
  fi
  # And it must not be bare seconds: a reader that treats this as an integer
  # would take "01:48:50" for 1. Assert the separator is really there, because
  # THAT is the difference between the two fields.
  if [[ "$elapsed" == *:* ]]; then
    ok "ps -o etime= is the colon-separated form (not the seconds-valued 'etimes' this host does not have)"
  else
    bad "ps -o etime= printed '${elapsed}' with no ':' — the parser's assumption about this host is wrong"
  fi
else
  bad "ps -o etime= failed on this host — the cutover-effect probe cannot read a process age here"
fi

printf 'ps-field-support: %d ok, %d failed\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
