#!/usr/bin/env python3
"""comment-test-ref-guard — a comment in non-test Go may not name a test that
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

WHAT IT DELIBERATELY DOES NOT COVER — say it out loud rather than let a green
imply it:

  * ONLY GO, and only non-test Go. A comment in a `_test.go` file naming a
    missing test is invisible here, and so is every TypeScript file and every
    Markdown page. Those were measured at roughly +23 and +47 references when
    this was written and are out of this check's scope by ruling, not by
    oversight.
  * ONLY NAMES BEGINNING `Test`, AND ONLY THE TOP-LEVEL ONE. A citation written
    `TestFoo/"the case it actually guards"` is checked as far as `TestFoo` and
    no further — the part after the slash is a `t.Run` label and nothing here
    looks for it. That form is worth writing anyway (it tells a reader where to
    go), but its second half carries no mechanical promise, and this shape is
    common here because T-125 folded whole families of `TestFoo_Behaviour`
    functions into one `TestFoo` with subtests. Checking subtest labels would
    mean reading `t.Run` arguments, which are often built from table variables
    rather than written as literals.
  * NOT-YET-TRACKED FILES. The sweep is over `git ls-files`, so a brand-new file
    is invisible until it is added.
  * IT CANNOT TELL YOU WHICH FIX IS RIGHT, and its worst failure mode is a
    person clearing a row by deleting a warning that was worth keeping. Usually
    only half of such a sentence is false: "change this and X breaks silently"
    is still true, and only "TestFoo is watching it" is not. So the failure
    message names KEEPING THE WARNING AND DROPPING THE NAME first, ahead of
    repointing or removing the claim. Writing a test purely to make a comment
    true is the one answer that is always wrong.
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

# A reference inside a comment. The capital after `Test` is what keeps ordinary
# English out — "Testing", "Tested" and "Tests" are words, `TestFoo` is a name.
REFERENCE = re.compile(r"\bTest[A-Z][A-Za-z0-9_]*")

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


def defined_tests(root: Path, paths: List[str]) -> Set[str]:
    names: Set[str] = set()
    for rel in paths:
        if not rel.endswith("_test.go"):
            continue
        names.update(DEFINITION.findall((root / rel).read_text(encoding="utf-8")))
    return names


def dangling(root: Path, paths: List[str], defined: Set[str]) -> Tuple[List[str], int]:
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
            rows.append(f"{rel}:{line} names {name}, which no test defines")
    return rows, checked


def run(root: Path) -> int:
    paths = tracked_go(root)
    defined = defined_tests(root, paths)
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
    rows, checked = dangling(root, paths, defined)
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
