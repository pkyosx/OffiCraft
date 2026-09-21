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
func Test_ZZZUnderscoreSpellingExists(t *testing.T) {}
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
        # Go accepts `Test_Foo` and the definition scan has always read it, so a
        # citation the reference scan could not even see was a test defined
        # under a name this guard could never check. Measured green before the
        # fix: an underscored dangling citation was skipped in silence.
        "a comment naming an underscored test that does not exist",
        '''package sample

// Pinned by Test_ZZZGoneWithTheRewrite.
func doWork() {}
''',
        True,
    ),
    (
        # The other direction, so the fix cannot be "redden on every underscore".
        "a comment naming an underscored test that does exist",
        '''package sample

// Pinned by Test_ZZZUnderscoreSpellingExists.
func doWork() {}
''',
        False,
    ),
    (
        # `Test_` with nothing after it is prose, not a name.
        "a bare Test_ in prose",
        '''package sample

// The Test_ prefix convention is not used in this package.
func doWork() {}
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

# 🔴 THE PHANTOM CASES — a defect review found, so they are pinned here rather
# than trusted to stay fixed. The definition scan used to run over raw text, so
# a `func Test…` that had been COMMENTED OUT still counted as defined and a
# citation of it passed. That is the worst possible blind spot for this check:
# commenting a test out is a more ordinary act than deleting it, and it was the
# one deletion the guard could not see. A `//` breaks the `^func` anchor by
# itself; a `/* */` block does not, and neither does Go source quoted in a raw
# string (a codegen fixture, a golden file). Both shapes are planted, and both
# must be red — if either goes green the `code_only()` masking has been lost.
PHANTOM_DEFINITIONS = '''package sample

import "testing"

func TestExistingGuardHoldsTheLine(t *testing.T) {}
func TestAnotherOneEntirely(t *testing.T)        {}

/*
func TestZZZPhantomInABlockComment(t *testing.T) {}
*/
'''

PHANTOM_RAW_STRING_TEST_FILE = '''package sample

var goldenSource = `
func TestZZZPhantomInARawString(t *testing.T) {}
`
'''

PHANTOM_CITATIONS = '''package sample

// Pinned by TestZZZPhantomInABlockComment.
func one() {}

// Pinned by TestZZZPhantomInARawString.
func two() {}
'''

# 🔴 THE BUILD-CONSTRAINED CASE — the same disappearance as a commented-out
# definition, spelled in a way Go accepts. `//go:build neverbuilt` leaves
# `func TestFoo` in the source for the definition scan to find while `go test`
# with no tags never compiles the file and `go test -list` cannot name it.
# Measured green before the fix, with the phantom counted among the defined.
CONSTRAINED_TEST_FILE = '''//go:build neverbuilt

package sample

import "testing"

func TestZZZBehindABuildTag(t *testing.T) {}
'''

CONSTRAINED_CITATION = '''package sample

// Pinned by TestZZZBehindABuildTag.
func doWork() {}
'''

# A comment in a *_test.go file is out of scope by ruling, and a green must not
# be read as covering it. Planted separately because it is about WHICH FILES are
# swept, not about what a comment says.
OUT_OF_SCOPE_TEST_FILE = '''package sample

// Pinned by TestThisIsInATestFileSoItIsNotSwept.
func helper() {}
'''


def build(tmp: Path, source: str | None, extra_test: str | None = None,
          definitions: str = DEFINITIONS, raw_string_test: str | None = None,
          constrained_test: str | None = None) -> Path:
    tree = Path(tempfile.mkdtemp(prefix="comment-test-ref-selftest-", dir=tmp))
    pkg = tree / "sample"
    pkg.mkdir()
    (pkg / "definitions_test.go").write_text(definitions, encoding="utf-8")
    if raw_string_test is not None:
        (pkg / "golden_source_test.go").write_text(raw_string_test, encoding="utf-8")
    if constrained_test is not None:
        (pkg / "behind_a_tag_test.go").write_text(constrained_test, encoding="utf-8")
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

        # A test that was commented out, or quoted as data, is NOT defined.
        if verdict(build(tmp, PHANTOM_CITATIONS,
                         definitions=PHANTOM_DEFINITIONS,
                         raw_string_test=PHANTOM_RAW_STRING_TEST_FILE)) == 0:
            failures.append(
                "a citation of a test that exists only inside a block comment or "
                "inside a raw string passed — the definition scan has stopped "
                "masking non-code, so the most ordinary way a test disappears is "
                "invisible again"
            )

        # A test behind a build constraint is named in the source but absent
        # from the suite, so citing it must redden.
        if verdict(build(tmp, CONSTRAINED_CITATION,
                         constrained_test=CONSTRAINED_TEST_FILE)) == 0:
            failures.append(
                "a citation of a test in a //go:build-excluded file passed — a name "
                "that `go test -list` cannot even print is being counted as a test "
                "that exists"
            )

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
        "1 phantom-definition control, 1 build-constraint control, "
        "1 out-of-scope control, 1 empty-sweep control)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
