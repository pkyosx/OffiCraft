#!/usr/bin/env python3
"""Positive controls for bin/ci-round-guard.py.

A guard that reports green proves nothing until it has been shown to redden on a
tree that deserves it. Every case below builds a MUTATED copy of the real three
files, runs the guard against that copy, and requires BOTH a non-zero exit AND a
message that names the thing that is actually wrong — because "it went red" and
"it went red for the reason I think" are not the same statement, and a guard that
reddens for the wrong reason will keep passing after the real rule stops working.

Case C0 is the control in the other direction: the UNMUTATED tree must be green,
so a red above cannot be explained by the harness itself being broken.
"""
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
GUARD = ROOT / "bin" / "ci-round-guard.py"
ROUND_REL = Path("bin/lib/ci-round.txt")
MK_REL = Path("Makefile")
YML_REL = Path(".github/workflows/ci.yml")

PASS = FAIL = 0


def report(ok: bool, label: str, detail: str = "") -> None:
    global PASS, FAIL
    if ok:
        PASS += 1
        print(f"  ok   {label}")
    else:
        FAIL += 1
        print(f"  FAIL {label}{(' — ' + detail) if detail else ''}", file=sys.stderr)


def build_tree(dst: Path) -> None:
    for rel in (ROUND_REL, MK_REL, YML_REL):
        (dst / rel).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / rel, dst / rel)


def run_guard(tree: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(GUARD), str(tree)], capture_output=True, text=True
    )


def case(label: str, mutate, expect_red: bool, expect_msg: str | None = None) -> None:
    with tempfile.TemporaryDirectory(prefix="oc-ci-round-selftest.") as tmp:
        tree = Path(tmp)
        build_tree(tree)
        mutate(tree)
        r = run_guard(tree)
        out = r.stdout + r.stderr
        if expect_red:
            if r.returncode == 0:
                report(False, label, "guard stayed GREEN on a tree that should redden")
                return
            if expect_msg and expect_msg not in out:
                report(
                    False,
                    label,
                    f"reddened, but the message never named {expect_msg!r} — it may be red for another reason: {out.strip()[:300]}",
                )
                return
            report(True, label)
        else:
            report(r.returncode == 0, label, f"expected green, got rc={r.returncode}: {out.strip()[:300]}")


# ── C0 control: the real tree, unmutated, must be green ─────────────────────
case("C0 control — the unmutated tree is green (so a red below is the mutant, not the harness)",
     lambda t: None, expect_red=False)


# ── M1 Makefile has a target the round list does not ────────────────────────
# This is the historical miss, in its exact shape: a check exists and nothing
# runs it. It is the direction that stayed possible after T-127.
def m1(tree: Path) -> None:
    mk = tree / MK_REL
    mk.write_text(mk.read_text() + "\nlint-freshly-forgotten:\n\t@echo hi\n")


case("M1 Makefile target with NO round line reddens and names the target",
     m1, expect_red=True, expect_msg="lint-freshly-forgotten")


# ── M2 round list names a target the Makefile does not have ─────────────────
def m2(tree: Path) -> None:
    rf = tree / ROUND_REL
    rf.write_text(rf.read_text() + "\nhygiene lint-never-defined\n")


case("M2 round line with NO Makefile target reddens and names the target",
     m2, expect_red=True, expect_msg="lint-never-defined")


# ── M3 a lane that is not a declared gate job ───────────────────────────────
# Its checks would run locally and in no cloud cell — the same silence as M1,
# reached by a different route.
def m3(tree: Path) -> None:
    rf = tree / ROUND_REL
    rf.write_text(
        re.sub(r"^hygiene(\s+scan-secrets)$", r"not-a-real-lane\1", rf.read_text(), flags=re.M)
    )


case("M3 lane that is not an `oc-job-role: gate` job reddens and names the lane",
     m3, expect_red=True, expect_msg="not-a-real-lane")


# ── M4 the same target assigned to two lanes ────────────────────────────────
def m4(tree: Path) -> None:
    rf = tree / ROUND_REL
    rf.write_text(rf.read_text() + "\nhygiene scan-secrets\n")


case("M4 a target assigned twice reddens", m4, expect_red=True, expect_msg="scan-secrets")


# ── M5 emptied round list must FAIL, not pass over an empty set ─────────────
# The failure this repo keeps meeting: a shrunken denominator and a clean pass
# are the same picture. A one-way containment check would go green here.
def m5(tree: Path) -> None:
    (tree / ROUND_REL).write_text("# everything removed\n")


case("M5 emptied round list FAILS instead of passing over an empty set",
     m5, expect_red=True, expect_msg="no checks at all")


# ── M6 every gate marker removed must FAIL for the same reason ──────────────
def m6(tree: Path) -> None:
    y = tree / YML_REL
    y.write_text(re.sub(r"^(\s*)#\s*oc-job-role:\s*gate\s*$", r"\1# oc-job-role: retired", y.read_text(), flags=re.M))


case("M6 no declared gate job at all FAILS instead of passing over an empty set",
     m6, expect_red=True, expect_msg="pass over an empty set")


# ═════════════════════════════════════════════════════════════════════════════
# THE WORKFLOW-SIDE MUTANTS
# ═════════════════════════════════════════════════════════════════════════════
# Every case below was built by an INDEPENDENT REVIEW against a candidate this
# guard had already declared green. All three passed every check that existed at
# the time, and in all three the cell went green while running nothing. They are
# fixtures now so the hole cannot come back quietly.


def _first_lane_run_span(yml: str, job: str) -> tuple[int, int]:
    i = yml.index(f"\n  {job}:")
    j = yml.index("bash bin/run-checks.sh --lane", i)
    return j, yml.index("\n", j)


def w1(tree: Path) -> None:
    """The `run:` line deleted outright — the cell keeps only its checkout."""
    y = tree / YML_REL
    t = y.read_text()
    j, k = _first_lane_run_span(t, "contract-guards")
    y.write_text(t[: t.rindex("\n", 0, j) + 1] + t[k + 1 :])


case("W1 a gate whose lane invocation was DELETED reddens and names the job",
     w1, expect_red=True, expect_msg="contract-guards")


def w2(tree: Path) -> None:
    """The line points at somebody else's lane."""
    y = tree / YML_REL
    t = y.read_text()
    j, _ = _first_lane_run_span(t, "contract-guards")
    old = '--lane "${{ github.job }}"'
    at = t.index(old, j)
    y.write_text(t[:at] + "--lane hygiene" + t[at + len(old) :])


case("W2 a gate invoking the WRONG lane reddens and names the job",
     w2, expect_red=True, expect_msg="contract-guards")


def w3(tree: Path) -> None:
    """The invocation wrapped in `echo` — the tokens are all still there."""
    y = tree / YML_REL
    t = y.read_text()
    j, _ = _first_lane_run_span(t, "go-checks")
    y.write_text(t[:j] + "echo " + t[j:])


case("W3 a gate whose wrapper call is inside `echo` reddens — a name is not an invocation",
     w3, expect_red=True, expect_msg="go-checks")


def w4(tree: Path) -> None:
    """A brand-new gate job that runs no checks at all."""
    y = tree / YML_REL
    t = y.read_text()
    i = t.index("\n  tcc-anchor:") + 1
    block = (
        "  brand-new-gate:\n"
        "    # oc-job-role: gate\n"
        "    runs-on: macos-15\n"
        "    steps:\n"
        "      - uses: actions/checkout@v5\n\n"
    )
    y.write_text(t[:i] + block + t[i:])


case("W4 a NEW gate job with no lane reddens rather than becoming a required check that runs nothing",
     w4, expect_red=True, expect_msg="brand-new-gate")


# ── CRLF: the guard and the runtime must not disagree about the round ────────
# Found the same way. The runtime's awk carried the \r into the target name and
# handed it to make; this guard read the same file through Python's universal
# newlines and saw a clean name. Green here, dead round there.
def w5(tree: Path) -> None:
    f = tree / ROUND_REL
    f.write_bytes(f.read_bytes().replace(b"\n", b"\r\n"))


case("W5 a CRLF round list is REFUSED, so the guard and the runtime cannot disagree about it",
     w5, expect_red=True, expect_msg="carriage return")


# ---------------------------------------------------------------------------
# W6-W10 — THE CONTROL-FLOW FAMILY.
#
# Two independent reviewers (Lumi and Joey), working separately on candidate
# cc5f543c, produced the SAME mutant within minutes of each other: wrap the
# route in `if false; then … fi`. The wrapper is still in command position on
# its own line, so the guard's line-by-line tokeniser counted it as routed,
# printed "each really invoking its own lane in command position", and the cell
# ran nothing.
#
# The lesson is not "block `if false`". A line-by-line tokeniser cannot see
# control flow AT ALL, so every one of W6-W9 is the same hole wearing different
# clothes and a blacklist would have to grow forever. The fix requires the route
# step to BE the canonical string; these fixtures exist to keep anyone from
# relaxing that back into a shape test. W10 pins the COST of that strictness
# (even a comment is refused) so it is a decision on the record, not a surprise.
def _replace_route(tree: Path, job: str, repl: str) -> None:
    y = tree / YML_REL
    t = y.read_text()
    j, k = _first_lane_run_span(t, job)
    start = t.rindex("\n", 0, j) + 1
    y.write_text(t[:start] + repl + t[k:])


ROUTE_MUTANTS = [
    ("W6 a route inside `if false` reddens — an unreachable call is not a call",
     '      - run: |\n          if false; then\n            bash bin/run-checks.sh --lane "${{ github.job }}"\n          fi'),
    ("W7 a route ending in `|| true` reddens — a swallowed failure is a green cell that failed",
     '      - run: bash bin/run-checks.sh --lane "${{ github.job }}" || true'),
    ("W8 a BACKGROUNDED route reddens — the cell would not wait for it or read its status",
     '      - run: bash bin/run-checks.sh --lane "${{ github.job }}" &'),
    ("W9 a route behind a false short-circuit reddens — `[ 1 = 2 ] &&` runs nothing either",
     '      - run: \'[ 1 = 2 ] && bash bin/run-checks.sh --lane "${{ github.job }}"\''),
    ("W10 even a COMMENT above the route reddens — the strictness is exact, and that is the cost",
     '      - run: |\n          # route\n          bash bin/run-checks.sh --lane "${{ github.job }}"'),
]

for _label, _repl in ROUTE_MUTANTS:
    case(_label,
         (lambda r: (lambda tree: _replace_route(tree, "go-checks", r)))(_repl),
         expect_red=True, expect_msg="go-checks")


# W11 — a correct route with a DECOY beside it. Nothing ran twice and nothing
# was skipped, but a second mention is either dead text or a round nobody
# counted; either way the next reader cannot tell which line is load-bearing.
def w11(tree: Path) -> None:
    _replace_route(
        tree, "go-checks",
        '      - run: bash bin/run-checks.sh --lane "${{ github.job }}"\n'
        '      - run: echo bash bin/run-checks.sh --lane "${{ github.job }}"',
    )


case("W11 a second `run:` step mentioning the wrapper reddens, even beside a correct route",
     w11, expect_red=True, expect_msg="go-checks")


# W12 — the exemption list is the one door left open on purpose, so it must not
# be openable QUIETLY. Joey's mutant: add a gate that runs nothing, then add its
# name to EXEMPT_GATES. That edit cannot be forbidden, but a stale entry can be,
# and so can an unjustified one.
# NOTE this one mutates the GUARD, not the tree, so it cannot go through case():
# case() always runs the repo's own guard against a temp tree, and EXEMPT_GATES
# lives in the guard. It runs a mutated COPY of the guard against the clean tree.
def _run_mutated_guard(edit) -> subprocess.CompletedProcess:
    with tempfile.TemporaryDirectory(prefix="oc-ci-round-selftest.guard.") as tmp:
        tree = Path(tmp) / "tree"
        tree.mkdir()
        build_tree(tree)
        gcopy = Path(tmp) / "guard.py"
        gcopy.write_text(edit(GUARD.read_text()))
        return subprocess.run(
            [sys.executable, str(gcopy), str(tree)], capture_output=True, text=True
        )


_r = _run_mutated_guard(
    lambda t: t.replace("EXEMPT_GATES = {\n", 'EXEMPT_GATES = {\n    "ghost-gate": "left behind",\n', 1)
)
report(_r.returncode != 0 and "ghost-gate" in (_r.stdout + _r.stderr),
       "W12 an EXEMPT_GATES entry naming no gate job reddens — a stale exemption is dead cover",
       f"rc={_r.returncode} out={(_r.stdout + _r.stderr).strip()[:200]}")

_r = _run_mutated_guard(
    lambda t: t.replace(
        '"macos-e2e": (', '"macos-e2e": "", "unused-key-macos-e2e": (', 1)
)
report(_r.returncode != 0 and "macos-e2e" in (_r.stdout + _r.stderr),
       "W13 an EXEMPT_GATES entry with an EMPTY reason reddens — an unjustified exemption is a silent channel",
       f"rc={_r.returncode} out={(_r.stdout + _r.stderr).strip()[:200]}")


print(f"ci-round guard selftest: {PASS} ok, {FAIL} failed")
sys.exit(1 if FAIL else 0)
