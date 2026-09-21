#!/usr/bin/env python3
"""Positive control for bin/comment-test-ref-guard.py.

Two things have to be true at once and neither is obvious from a green run:
the scanner must redden on a comment that names a test which is not there, and
it must NOT redden on the things that look like that but are not — a name split
across two comment lines, a `Test…` inside a string literal, a comment in a test
file, a plain English word beginning with "Test".

A guard that only ever says green is indistinguishable from one that has stopped
reading, and a guard that reddens on wrapped names is one people route around.
So every case below is built into a synthetic tree and run for a specific
verdict.
"""
from __future__ import annotations

import subprocess
import sys
import tempfile
from pathlib import Path
from typing import List, Tuple

ROOT = Path(__file__).resolve().parent.parent.parent
GUARD = ROOT / "bin" / "comment-test-ref-guard.py"

# Every synthetic tree carries this, so there is always something to match
# against and a green can never come from an empty definition set.
#
# 🔴 THE NAMES BELOW, AND THE DANGLING ONES IN THE CASES, ARE DELIBERATELY
# ABSURD — DO NOT TIDY THEM INTO PLAUSIBLE ONES. Matching is by PREFIX, so a
# control name that somebody might one day really define stops controlling
# anything the moment they do: the reference starts passing and the case goes
# green, which is byte-identical to the case still working. An earlier draft of
# this ticket used `TestMain` as a control in its measurement — a name Go itself
# gives meaning to, and one that any future `TestMainLoop` would silently
# satisfy. The defence is a name nobody would write and nothing can prefix.
# (The trees here are hermetic, so this is belt and braces; the habit is the
# point, because the measurement that is NOT hermetic needs the same rule.)
DEFINITIONS = """package sample

import "testing"

func TestExistingGuardHoldsTheLine(t *testing.T) {}
func TestAnotherOneEntirely(t *testing.T)        {}
"""

# (name, the non-test source to plant, must the guard go red?)
CASES: Tuple[Tuple[str, str, bool], ...] = (
    (
        "a comment naming a test that does not exist",
        '''package sample

// Pinned by TestThisWasDeletedLongAgo.
func doWork() {}
''',
        True,
    ),
    (
        "a comment naming a test that does exist",
        '''package sample

// Pinned by TestExistingGuardHoldsTheLine.
func doWork() {}
''',
        False,
    ),
    (
        # The reason matching is by prefix at all. Comment text wraps and these
        # names are long, so the fragment on the first line is a prefix.
        "a real name split across two comment lines",
        '''package sample

// Pinned by TestExistingGuardHoldsThe
// LineTheWayThisSentenceWraps.
func doWork() {}
''',
        False,
    ),
    (
        "a dangling name inside a block comment",
        '''package sample

/*
Pinned by TestGoneWithTheRewrite, which nobody replaced.
*/
func doWork() {}
''',
        True,
    ),
    (
        # A table-driven case name lives in a string, not in prose. Reading it
        # as a reference would make this guard redden on ordinary data.
        "a Test-shaped name inside a string literal",
        '''package sample

var label = "TestNotARealReferenceItIsData"

func doWork() { _ = label }
''',
        False,
    ),
    (
        "a Test-shaped name inside a raw string literal",
        '''package sample

var doc = `TestNotARealReferenceEither`

func doWork() { _ = doc }
''',
        False,
    ),
    (
        # A URL in a constant contains `//`. Treating it as a comment would drag
        # whatever follows into the scan.
        "a // inside a string does not open a comment",
        '''package sample

var link = "https://example.invalid/TestThisIsPartOfAUrl"

func doWork() { _ = link }
''',
        False,
    ),
    (
        "ordinary English beginning with Test",
        '''package sample

// Testing this by hand is fine; the Tested path is the one below.
func doWork() {}
''',
        False,
    ),
)

# A comment in a *_test.go file is out of scope by ruling, and a green must not
# be read as covering it. Planted separately because it is about WHICH FILES are
# swept, not about what a comment says.
OUT_OF_SCOPE_TEST_FILE = '''package sample

// Pinned by TestThisIsInATestFileSoItIsNotSwept.
func helper() {}
'''


def build(tmp: Path, source: str | None, extra_test: str | None = None) -> Path:
    tree = Path(tempfile.mkdtemp(prefix="comment-test-ref-selftest-", dir=tmp))
    pkg = tree / "sample"
    pkg.mkdir()
    (pkg / "definitions_test.go").write_text(DEFINITIONS, encoding="utf-8")
    if source is not None:
        (pkg / "sample.go").write_text(source, encoding="utf-8")
    if extra_test is not None:
        (pkg / "extra_helper_test.go").write_text(extra_test, encoding="utf-8")
    subprocess.run(["git", "-C", str(tree), "init", "-q"], check=True)
    subprocess.run(["git", "-C", str(tree), "add", "-A"], check=True)
    return tree


def verdict(tree: Path) -> int:
    return subprocess.run(
        [sys.executable, str(GUARD), str(tree)], capture_output=True, text=True
    ).returncode


def main() -> int:
    failures: List[str] = []
    with tempfile.TemporaryDirectory(prefix="comment-test-ref-selftest-root-") as tmpdir:
        tmp = Path(tmpdir)

        for name, source, want_red in CASES:
            got_red = verdict(build(tmp, source)) != 0
            if got_red != want_red:
                wanted = "red" if want_red else "green"
                failures.append(f"{name}: wanted {wanted}, got {'red' if got_red else 'green'}")

        if verdict(build(tmp, None, extra_test=OUT_OF_SCOPE_TEST_FILE)) != 0:
            failures.append(
                "a comment in a *_test.go file was reported — the sweep has grown "
                "beyond the scope the guard's own docstring claims"
            )

        # A tree with no test definitions at all must NOT read as clean: every
        # reference would be dangling and an empty sweep would look identical to
        # a good one.
        bare = Path(tempfile.mkdtemp(prefix="comment-test-ref-selftest-bare-", dir=tmp))
        (bare / "sample.go").write_text("package sample\n", encoding="utf-8")
        subprocess.run(["git", "-C", str(bare), "init", "-q"], check=True)
        subprocess.run(["git", "-C", str(bare), "add", "-A"], check=True)
        if verdict(bare) == 0:
            failures.append(
                "a tree with zero defined tests passed — an empty sweep is being "
                "reported as a clean one"
            )

    if failures:
        rows = "\n  ".join(failures)
        print(
            f"FAIL — comment-test-ref-guard's positive control found {len(failures)} "
            f"problem(s):\n  {rows}\n\n"
            "  A missed red means a false 'this is watched' can be written again with\n"
            "  every check green. A false red is worse than it looks: it teaches people\n"
            "  to delete the sentence instead of fixing it. Fix the guard, not this\n"
            "  control.",
            file=sys.stderr,
        )
        return 1
    print(
        f"[comment-test-ref-guard-selftest] all green ({len(CASES)} shape cases, "
        "1 out-of-scope control, 1 empty-sweep control)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
