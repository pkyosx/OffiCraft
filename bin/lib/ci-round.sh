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
ci_round_targets() {
  ci_round_rows "$1" | awk '{print $2}'
}

# ci_round_lane <root> <lane> — that lane's targets, in file order.
#
# An unknown lane is a FAILURE, never an empty list. A cloud cell that asked for
# a lane nobody assigned would otherwise run zero checks and report success —
# which is the exact shape ("nothing ran" wearing the face of "nothing failed")
# that bin/run-checks.sh exists to refuse.
ci_round_lane() {
  local root="$1" lane="$2" out
  out="$(ci_round_rows "$root" | awk -v lane="$lane" '$1==lane {print $2}')" || return $?
  if [[ -z "$out" ]]; then
    echo "FAIL — no check is assigned to lane '$lane' in bin/lib/ci-round.txt." >&2
    echo "A lane with no checks would run nothing and exit 0, which is indistinguishable from a green round." >&2
    return 5
  fi
  printf '%s\n' "$out"
}

# ci_round_lanes <root> — every lane named in the file, once each, in file order.
ci_round_lanes() {
  ci_round_rows "$1" | awk '!seen[$1]++ {print $1}'
}
