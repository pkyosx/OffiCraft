#!/usr/bin/env bash
# Read THE round — bin/lib/ci-round.txt — for both callers that need it.
#
# There is exactly one list of this repo's checks (T-127). bin/ci.sh asks for all
# of it in order; each cloud gate cell asks for its own lane. Neither writes a
# target name down, so neither can fall behind the other.
#
# Format: `<lane> <target>`, blank lines and `#` comments ignored. The file's own
# header explains why the lane is the gate job's id and why order matters.

ci_round_file() {
  printf '%s\n' "${1:-$ROOT}/bin/lib/ci-round.txt"
}

# ci_round_rows <root> — every non-comment row, in file order, as "<lane> <target>".
ci_round_rows() {
  local file; file="$(ci_round_file "$1")"
  if [[ ! -f "$file" ]]; then
    echo "FAIL — the round list is missing: $file" >&2
    return 2
  fi
  awk 'BEGIN{n=0}
       # A CRLF file used to survive to here with the carriage return still
       # GLUED TO THE TARGET NAME, while the Python guard read the same file
       # through .strip()/.split() and saw a clean name. The two sides then
       # disagreed about what the round contains: the guard went green and make
       # died later on a target nobody could find. Strip it on this side so a
       # stray \r degrades instead of exploding; bin/ci-round-guard.py REFUSES
       # the file outright and names it, which is what gets it fixed.
       {gsub(/\r/,"")}
       {sub(/#.*/,"")}
       {gsub(/^[ \t]+|[ \t]+$/,"")}
       $0=="" {next}
       NF!=2 {printf("FAIL — bin/lib/ci-round.txt line %d is not `<lane> <target>`: %s\n", NR, $0) > "/dev/stderr"; bad=1; next}
       {print $1, $2; n++}
       END{
         if (bad) exit 3
         # A round that came out empty must not read as "nothing to run": an
         # emptied list and a passing round would otherwise be the same picture.
         if (n==0) {print "FAIL — bin/lib/ci-round.txt lists no checks at all." > "/dev/stderr"; exit 4}
       }' "$file"
}

# ci_round_targets <root> — every target, in file order (the local round).
#
# ⚠️ NOT `ci_round_rows "$1" | awk ...`. A pipeline's status is its LAST stage,
# so a pipe here reports awk's success and DISCARDS a refusal from ci_round_rows
# — the same fail-open shape an independent review found in bin/ci.sh, one layer
# down. Measured: with a malformed row in the file this used to answer rc 0 and
# a short list. Capture first, check, then filter.
ci_round_targets() {
  local rows
  rows="$(ci_round_rows "$1")" || return $?
  printf '%s\n' "$rows" | awk 'NF{print $2}'
}

# ci_round_lane <root> <lane> — that lane's targets, in file order.
#
# An unknown lane is a FAILURE, never an empty list. A cloud cell that asked for
# a lane nobody assigned would otherwise run zero checks and report success —
# which is the exact shape ("nothing ran" wearing the face of "nothing failed")
# that bin/run-checks.sh exists to refuse.
ci_round_lane() {
  local root="$1" lane="$2" out
  local rows
  rows="$(ci_round_rows "$root")" || return $?
  out="$(printf '%s\n' "$rows" | awk -v lane="$lane" 'NF && $1==lane {print $2}')"
  if [[ -z "$out" ]]; then
    echo "FAIL — no check is assigned to lane '$lane' in bin/lib/ci-round.txt." >&2
    echo "A lane with no checks would run nothing and exit 0, which is indistinguishable from a green round." >&2
    return 5
  fi
  printf '%s\n' "$out"
}

# ci_round_lanes <root> — every lane named in the file, once each, in file order.
# Same capture-then-filter rule as ci_round_targets: a pipe would hide a refusal.
ci_round_lanes() {
  local rows
  rows="$(ci_round_rows "$1")" || return $?
  printf '%s\n' "$rows" | awk 'NF && !seen[$1]++ {print $1}'
}
