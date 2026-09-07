#!/usr/bin/env python3
"""lint-ci-round — the round list and the Makefile must name the same checks.

WHY THIS EXISTS (T-127)
-----------------------
Before T-127 the set of checks was written down twice — bin/ci.sh's OC_ROUND and
ten hand-typed argument lists in .github/workflows/ci.yml — and nothing compared
them. Three checks had already been missed that way. Not one of the three was
caught by a mechanism: each was noticed by a person, afterwards, by chance.
`lint-chat-pushdown` was still missing from every cloud cell on 0f84a859, so the
round that decides whether a change may land never ran it.

T-127 deleted the workflow's copy. The cells now ask for their own lane and
bin/lib/ci-round.txt is the only place a target is written down, so "the cloud
forgot to add it" is no longer a thing anyone can do.

WHAT IS LEFT, AND WHY IT NEEDS THIS FILE
----------------------------------------
One half of the mistake did NOT become impossible: a Makefile target can still be
added without a line in the round list. That is a different and much narrower
slip than the one T-127 removed, but it is the same shape — a check that exists
and runs nowhere — and it would be just as silent. This guard is the mechanism
that was missing for all three of the historical misses.

It takes the set difference BOTH WAYS, deliberately:
  * Makefile target with no round line  ⇒ a check that runs nowhere.
  * round line with no Makefile target  ⇒ a lane that will fail at `make` time,
    or worse, a name that quietly matches nothing.

WHY BOTH DIRECTIONS RATHER THAN "IS EVERYTHING COVERED"
-------------------------------------------------------
A one-way containment check passes over an empty set. If the round list were
emptied — or if this file's parser stopped finding targets for any reason — a
one-way check would report a clean green, because nothing uncovered was found.
Every count below is therefore asserted to be non-zero before any difference is
reported, and the denominators are printed on success.
"""
import re
import sys
from pathlib import Path

# argv[1] overrides the root ONLY so bin/tests/ci-round-guard-selftest.py can
# point this guard at a mutated copy of the tree. Nothing in CI passes it, so the
# real round is always checked against the real Makefile.
ROOT = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
ROUND = ROOT / "bin" / "lib" / "ci-round.txt"
MAKEFILE = ROOT / "Makefile"
CI_YML = ROOT / ".github" / "workflows" / "ci.yml"

FAILURES: list[str] = []


def fail(msg: str) -> None:
    FAILURES.append(msg)


def read_round() -> list[tuple[int, str, str]]:
    rows = []
    for n, raw in enumerate(ROUND.read_text().splitlines(), 1):
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        parts = line.split()
        if len(parts) != 2:
            fail(f"{ROUND.name}:{n} is not `<lane> <target>`: {raw!r}")
            continue
        rows.append((n, parts[0], parts[1]))
    return rows


def read_makefile_targets() -> list[str]:
    # The same shape CONTRIBUTING.md tells a reader to use:
    #   grep -nE '^[a-z][a-z0-9-]*:' Makefile
    return [
        m.group(1)
        for m in (
            re.match(r"^([a-z][a-z0-9-]*):", line) for line in MAKEFILE.read_text().splitlines()
        )
        if m
    ]


def read_gate_jobs() -> list[str]:
    """Job ids in ci.yml that declare `# oc-job-role: gate`.

    Line-based on purpose: the hosted macOS runner has no PyYAML, and this guard
    must not be the reason a lane cannot be checked. The marker sits inside the
    job's own block, so the job id is the last 2-space key seen above it.
    """
    gates, current = [], None
    for line in CI_YML.read_text().splitlines():
        m = re.match(r"^  ([A-Za-z0-9_-]+):\s*$", line)
        if m:
            current = m.group(1)
            continue
        if re.match(r"^\s*#\s*oc-job-role:\s*gate\s*$", line) and current:
            gates.append(current)
    return gates


def main() -> int:
    for path in (ROUND, MAKEFILE, CI_YML):
        if not path.is_file():
            print(f"[lint-ci-round] FAIL — missing {path}", file=sys.stderr)
            return 2

    rows = read_round()
    round_targets = [t for _, _, t in rows]
    round_lanes = [l for _, l, _ in rows]
    mk_targets = read_makefile_targets()
    gates = read_gate_jobs()

    # ── denominators first: a zero here makes every difference below vacuous ──
    if not round_targets:
        fail("the round list names no checks at all — every check below would pass over an empty set")
    if not mk_targets:
        fail("no named targets were found in the Makefile — the target scan is broken, not the Makefile")
    if not gates:
        fail("no job in ci.yml declares `# oc-job-role: gate` — the lane check would pass over an empty set")
    if FAILURES:
        for f in FAILURES:
            print(f"[lint-ci-round] FAIL — {f}", file=sys.stderr)
        return 1

    # ── (iii) a target may be assigned exactly once ──────────────────────────
    seen: dict[str, int] = {}
    for n, _, t in rows:
        if t in seen:
            fail(f"{t} is assigned twice ({ROUND.name}:{seen[t]} and :{n}); it would run twice and mean one lane owns it and another does too")
        seen[t] = n

    # ── (i) set difference BOTH ways ─────────────────────────────────────────
    missing_from_round = sorted(set(mk_targets) - set(round_targets))
    missing_from_makefile = sorted(set(round_targets) - set(mk_targets))
    for t in missing_from_round:
        fail(
            f"Makefile target `{t}` has no line in {ROUND.name}, so NOTHING runs it — "
            f"not the local round, not any cloud lane. Add a line `<lane> {t}`."
        )
    for t in missing_from_makefile:
        fail(
            f"{ROUND.name} lists `{t}` but the Makefile has no such target — "
            f"the lane that names it would fail at make time."
        )

    # ── (ii) every lane is a declared gate job ───────────────────────────────
    for lane in sorted(set(round_lanes)):
        if lane not in gates:
            fail(
                f"lane `{lane}` is not a job declaring `# oc-job-role: gate` in ci.yml — "
                f"its checks would run locally and in no cloud cell. Gate jobs today: {', '.join(sorted(gates))}."
            )

    if FAILURES:
        for f in FAILURES:
            print(f"[lint-ci-round] FAIL — {f}", file=sys.stderr)
        return 1

    print(
        f"[lint-ci-round] ok — {len(round_targets)} checks, "
        f"{len(set(round_lanes))} lanes, all present in the Makefile ({len(mk_targets)} targets) "
        f"and every lane is one of {len(gates)} declared gate jobs"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
