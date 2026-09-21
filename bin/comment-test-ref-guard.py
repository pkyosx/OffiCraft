#!/usr/bin/env python3
r"""comment-test-ref-guard — a comment in non-test Go may not name a test that
does not exist.

WHAT THIS IS FOR (T-265). A comment that says "pinned by TestFoo" answers the
next reader's question for them. If TestFoo is gone, the answer is wrong in the
direction that stops the enquiry: the reader concludes the path is watched and
does not go looking. When this check was written the tree carried 97 such names
across 45 files, and 94 of them had no test with even a similar name — they were
not renames, they were tests deleted by T-125's rewrite of the Go test surface
with their mentions left behind.

WHAT A GREEN HERE MEANS, AND IT IS NARROW. It means no comment names a test that
does not exist. It does NOT mean the behaviour beside the comment is tested, and
it never will: this check reads names, not coverage. It makes the comments
honest; it does not make the suite bigger.

THE MATCH IS BY PREFIX, AND THAT IS NOT LENIENCY, IT IS THE WRAP RULE. Comment
text wraps, and these test names are long, so a name is regularly split across
two comment lines. The fragment left on the first line is a PREFIX of the real
name, so a reference counts as satisfied when some defined test name STARTS with
it. Requiring equality would have this guard manufacture a red for every wrapped
name in the tree — a check whose reds are mostly its own fault is one people
learn to bypass.

  The cost of the prefix rule, stated rather than left implied: a name that is
  truncated or mistyped in a way that still leaves a real test's prefix passes.
  `TestGetMonitoring` passes while thirty different tests begin with it. That is
  the deliberate trade — the wrap problem is common and the truncation problem
  is not.

WHY IT IS A LANE CHECK AND NOT A GO TEST. The comparison spans every Go module
in the tree at once (a comment in one module may name a test in another), and it
has to keep working when a module's test surface is rewritten — which is exactly
the event that created the mess it cleans up.

WHAT IT DOES NOT COVER — DERIVED, NOT REMEMBERED.

An earlier version of this section was a list of gaps the author could think of.
One review round put three holes in it (a constant outside the list, a
commented-out definition, a cited FILENAME), and all three were absent from the
list — because a list of remembered gaps is a fourth act of imagination, not a
derivation. So the rule here is mechanical instead:

    Walk the transformations the input goes through. EVERY transformation
    discards a class of thing, and the class it discards is exactly what this
    check cannot see. Read them off in order.

  1. SELECT FILES: `git ls-files '*.go'` ⇒ discards untracked files (a brand-new
     file is invisible until it is added), and every non-Go file — TypeScript,
     Markdown, shell. Those carried roughly +47 references when this was
     written, and `bin/tests/*.sh` headers cite tests too.
  2. SPLIT test / non-test by the `_test.go` suffix ⇒ discards comments inside
     test files (roughly +23 references), which are never scanned for claims.
  3. EXTRACT COMMENTS from the non-test side ⇒ discards everything in code and
     in string literals, which is correct here, and it means a claim written as
     a string constant is not seen.
  4. MATCH `\bTest[A-Z]\w*` or `\bTest_\w*` inside those comments ⇒ discards every
     other way to name a guard: a `*_test.go` FILENAME, a `t.Run` label, a shell
     guard's path, a suite. A citation of any of those is unchecked.
     🔴 THE FILENAME HALF IS THE ONE THAT HAS ALREADY BITTEN. 28 distinct
     nonexistent filenames were cited across 32 places in 25 files when this was
     written; the owner ruled them out and they were cleared BY HAND. Nothing
     here held that, and nothing here holds it now: the very next one written is
     invisible, and the tree reads as clean because the function-name half is.
     Widening this transformation to filenames would change the acceptance
     criteria the owner approved, so it is a question for him, not a gap to
     close quietly — it is on T-265's follow-up list.
  5. BUILD THE DEFINED SET from `^func Test…` over `code_only()` of each test
     file, minus the files behind a build constraint ⇒ discards a test whose
     name exists but whose BODY cannot run. Two shapes are no longer discarded
     SILENTLY — a commented-out definition, and a definition in a
     `//go:build`-excluded file — because both were measured passing green and
     both now report. One shape is still kept and still useless: a `t.Skip`ped
     test counts as defined, and there are two in this tree. Saying this
     transformation discards nothing would be the kind of unchecked sentence
     this whole guard exists to delete.
  6. COMPARE BY PREFIX ⇒ discards the distinction between a real name and any
     TRUNCATION of one. `TestGetMonitoring` passes while thirty tests begin with
     it. Deliberate — see the wrap rule above — and the cost is stated there.
  7. RESOLVE AGAINST THE WHOLE TREE ⇒ discards module boundaries: a citation in
     module A is satisfied by a same-named test in module B. Four `TestRealMain`
     exist here.

If you add a transformation, add its discard. That is the whole method, and it
is the reason this section can be checked rather than believed.

🔴 ONE LIMIT IS NOT A DISCARD AND SO WILL NEVER FALL OUT OF THAT LIST: THIS
CHECKS THAT A NAME EXISTS, NOT THAT THE TEST COVERS THE SENTENCE. It is the
biggest gap and the one a green most invites you to forget. T-265's cleanup
matched every citation it wrote to its sentence BY HAND, and one candidate was
rejected because the surviving test with the obvious name never calls the
function the sentence was about. Nothing here would have caught that, and
nothing re-checks the survivors from now on.

THIS CHECK CANNOT TELL YOU WHICH FIX IS RIGHT, and its worst failure mode is a
person clearing a row by deleting a warning that was worth keeping. Usually only
half of such a sentence is false: "change this and X breaks silently" is still
true, and only "TestFoo is watching it" is not. So the failure message names
KEEPING THE WARNING AND DROPPING THE NAME first, ahead of repointing or removing
the claim. Writing a test purely to make a comment true is the one answer that
is always wrong.
"""
from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path
from typing import Dict, List, Set, Tuple

ROOT = Path(__file__).resolve().parent.parent

# A Go test function as `go test` recognises one. Benchmarks, examples and fuzz
# targets are out: a comment naming one of those names it by its own prefix.
DEFINITION = re.compile(r"^func\s+(Test[A-Za-z0-9_]*)\s*\(", re.M)

# A build constraint in a test file's header. `go test` with no tags does not
# compile such a file, so the tests in it are not in the suite a green here
# invites the reader to trust — `go test -list` cannot even name them. Counting
# them as defined let a citation of an unbuildable test pass, which is the
# deletion this guard is for wearing a different hat.
BUILD_CONSTRAINT = re.compile(r"^//go:build\s+\S", re.M)

# A reference inside a comment. The capital after `Test` is what keeps ordinary
# English out — "Testing", "Tested" and "Tests" are words, `TestFoo` is a name.
# `Test_Foo` is the other spelling Go accepts and DEFINITION has always read it,
# so leaving it out here made the two ends asymmetric: a test could be DEFINED
# under a name this could never CITE, and every underscored citation in the tree
# was skipped without a word. An underscore only counts with something after it,
# so a bare `Test_` in prose is still not a name.
REFERENCE = re.compile(r"\bTest(?:[A-Z]|_[A-Za-z0-9])[A-Za-z0-9_]*")

# This file states the shapes in order to find them, and its selftest plants
# deliberately-broken ones.
SKIP = {
    "bin/comment-test-ref-guard.py",
    "bin/tests/comment-test-ref-guard-selftest.py",
}


def tracked_go(root: Path) -> List[str]:
    out = subprocess.run(
        ["git", "-C", str(root), "ls-files", "*.go"],
        capture_output=True, text=True, check=True,
    )
    return [p for p in out.stdout.split("\n") if p and p not in SKIP]


def comment_spans(src: str) -> List[Tuple[int, str]]:
    """Every `//` and `/* */` comment, with the offset it starts at.

    Written as a scanner rather than a regex because Go string and rune literals
    routinely contain `//` — a URL in a constant would otherwise be read as a
    comment, and a `Test…` inside a real string (a table-driven case name, say)
    would be reported as a reference nobody wrote in prose.
    """
    spans: List[Tuple[int, str]] = []
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c == "`":  # raw string literal: no escapes, ends at the next backtick
            j = src.find("`", i + 1)
            i = n if j < 0 else j + 1
            continue
        if c in '"\'':  # interpreted string or rune literal
            quote, i = c, i + 1
            while i < n and src[i] != quote:
                i += 2 if src[i] == "\\" else 1
            i += 1
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            j = src.find("\n", i)
            j = n if j < 0 else j
            spans.append((i, src[i:j]))
            i = j
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            j = n if j < 0 else j
            spans.append((i, src[i:j]))
            i = j + 2
            continue
        i += 1
    return spans


def code_only(src: str) -> str:
    """`src` with every comment and every string literal blanked to spaces.

    Offsets and line breaks are preserved, so a `^func` anchor still means what
    it meant. Used to decide what is really declared, as opposed to what merely
    LOOKS declared inside a comment or inside Go source quoted as data.
    """
    out = list(src)
    i, n = 0, len(src)

    def blank(a: int, b: int) -> None:
        for k in range(a, min(b, n)):
            if out[k] != "\n":
                out[k] = " "

    while i < n:
        c = src[i]
        if c == "`":
            j = src.find("`", i + 1)
            j = n if j < 0 else j + 1
            blank(i, j)
            i = j
            continue
        if c in '"\'':
            quote, j = c, i + 1
            while j < n and src[j] != quote:
                j += 2 if src[j] == "\\" else 1
            blank(i, min(j + 1, n))
            i = j + 1
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            j = src.find("\n", i)
            j = n if j < 0 else j
            blank(i, j)
            i = j
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            j = n if j < 0 else j + 2
            blank(i, j)
            i = j
            continue
        i += 1
    return "".join(out)


def defined_tests(root: Path, paths: List[str]) -> Tuple[Set[str], Dict[str, str]]:
    """Test functions that actually compile into the suite, and the ones that do not.

    🔴 A DECLARATION INSIDE A COMMENT IS NOT A DEFINITION, and skipping that
    check is how this guard would bless the most common way a test disappears.
    `//` breaks the `^func` anchor by itself, but a `/* … */` block does not,
    and neither does a raw string holding Go source (a codegen fixture, a
    golden file). Commenting a test out is more ordinary than deleting it, so
    without this filter the one deletion the guard cannot see is the likely
    one — measured by independent review, which parked a commented-out
    `func TestZZZPhantomNeverRuns` in a test file, cited it from production
    code, and got all green with the phantom counted among the defined.

    🔴 A BUILD-CONSTRAINED TEST FILE IS THE SAME DISAPPEARANCE, SPELLED LEGALLY.
    `//go:build neverbuilt` on a test file leaves `func TestFoo` right there in
    the source for this to find while `go test -list` cannot name it — measured
    the same way, and green. Those names are returned SEPARATELY so a citation
    of one reddens with a message that says why, instead of being reported as a
    test that was never written. The tree carries no build-constrained test file
    today, so this costs nothing here and exists for the day one appears.
    """
    names: Set[str] = set()
    constrained: Dict[str, str] = {}
    for rel in paths:
        if not rel.endswith("_test.go"):
            continue
        src = (root / rel).read_text(encoding="utf-8")
        found = DEFINITION.findall(code_only(src))
        # Read the constraint off the RAW source: code_only() blanks comments,
        # and a build constraint is a comment.
        if BUILD_CONSTRAINT.search(src):
            for name in found:
                constrained.setdefault(name, rel)
            continue
        names.update(found)
    return names, constrained


def dangling(root: Path, paths: List[str], defined: Set[str],
             constrained: Dict[str, str]) -> Tuple[List[str], int]:
    """Comment references with no test whose name starts with them."""
    rows: List[str] = []
    checked = 0
    for rel in paths:
        if rel.endswith("_test.go"):
            continue
        src = (root / rel).read_text(encoding="utf-8")
        seen: Dict[str, int] = {}
        for start, text in comment_spans(src):
            for m in REFERENCE.finditer(text):
                name = m.group(0)
                checked += 1
                if any(d.startswith(name) for d in defined):
                    continue
                line = src.count("\n", 0, start + m.start()) + 1
                seen.setdefault(name, line)
        for name, line in sorted(seen.items(), key=lambda kv: kv[1]):
            hit = next((n for n in constrained if n.startswith(name)), None)
            if hit is not None:
                rows.append(
                    f"{rel}:{line} names {name}, defined in {constrained[hit]} — a file "
                    "behind a build constraint, so `go test` with no tags never "
                    "compiles it and the sentence promises a test that cannot run"
                )
                continue
            rows.append(f"{rel}:{line} names {name}, which no test defines")
    return rows, checked


def run(root: Path) -> int:
    paths = tracked_go(root)
    defined, constrained = defined_tests(root, paths)
    if not defined:
        # Zero defined tests would make every reference dangling AND would make
        # an empty tree look like a clean one. Neither reading is safe, so this
        # is a failure rather than a green.
        print(
            "FAIL — no `func Test…` was found in any tracked *_test.go. Either the "
            "sweep is looking at the wrong tree or the test surface is gone; both "
            "make every answer this check could give meaningless.",
            file=sys.stderr,
        )
        return 1
    rows, checked = dangling(root, paths, defined, constrained)
    if rows:
        listing = "\n  ".join(rows)
        print(
            f"FAIL — {len(rows)} comment(s) in non-test Go name a test that does not "
            f"exist:\n  {listing}\n\n"
            "  A named test is an answer to 'is this watched?', so a name that is not\n"
            "  there answers it wrongly and stops the next reader looking.\n"
            "  Three honest fixes, in the order that loses the least:\n"
            "    * KEEP THE WARNING, DROP THE NAME. Usually only the 'someone is\n"
            "      watching this' half is false; the 'change this and X breaks' half is\n"
            "      still worth reading. Delete the citation, keep the sentence.\n"
            "    * point the sentence at the test that does exist (it may have been\n"
            "      renamed rather than deleted);\n"
            "    * or drop the claim and say plainly what is and is not covered.\n"
            "  Do NOT write a test whose only purpose is to make a comment true, and do\n"
            "  NOT delete a whole warning to clear a row — the first fix above exists\n"
            "  precisely so that is never the cheapest way out.\n"
            "  Matching is by prefix, so a name split across two comment lines is fine;\n"
            "  if a row above looks like a wrap, the second half is missing too.",
            file=sys.stderr,
        )
        return 1
    print(
        f"[comment-test-ref-guard] all green ({checked} test name(s) referenced in "
        f"non-test Go comments, all matching one of {len(defined)} defined tests)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(run(Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else ROOT))
