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


print(f"ci-round guard selftest: {PASS} ok, {FAIL} failed")
sys.exit(1 if FAIL else 0)
