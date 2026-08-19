#!/usr/bin/env python3
"""shadow-claim-guard — while ANY warden-command dispatch is left ungated by
`--no-reconcile`, no sentence in the tree may claim the flag covers them all.

THE INCIDENT THIS GUARDS AGAINST (T-941e). `api_machines.go` said, verbatim,
"--no-reconcile is the shadow-deploy kill-switch over EVERY warden-command
dispatch — a shadow server must never command wardens". It was false the day it
was written: the outsource-worker verbs (stop, restart, model change, relocate,
refocus, terminate-dismisses-workers, the worker's own report_stopped) reach
`enqueueToWarden` without ever reading the flag. Three more copies of the same
promise sat in main.go, reconcile.go and api_stub.go, and a fourth in
docs/design/. The failure is SILENT and its direction is the bad one: nothing
goes red, nothing logs, and the sentence only cashes out at the moment somebody
BELIEVES it and presses stop on a shadow cockpit — killing a real session.
The owner ruled (T-941e, 2026-08-18): do NOT add the gates; delete the promise.

WHY THIS IS NOT A BLOCKLIST OF THE FOUR SENTENCES. A list of banned strings goes
green the day someone re-words the promise, and it would ALSO stay red forever
if a later ticket actually closed the holes — freezing today's ruling into a
lint. So the rule is a CONJUNCTION of two things, both measured from the tree on
every run:

    (A) there exists at least one call to a warden-command dispatch helper
        whose enclosing function never reads `noReconcile`, AND
    (B) there exists a line that names the flag (or a shadow deployment) and
        makes an UNQUALIFIED universal claim about what it covers

    ⇒ the answer must be zero rows.

Both halves are discovered by shape, never from a path list. If a future ticket
gates every dispatch, (A) empties and the prose is allowed back automatically —
the guard tracks the code, it does not enshrine the ruling. If someone adds a
new ungated dispatch AND a fresh universal sentence, it reddens on the pair.

WHAT THIS GUARD DELIBERATELY DOES NOT COVER — say it out loud rather than let a
green imply it:

  * PARAPHRASE. (B) is keyword-shaped. "the shadow box can't touch prod" names
    neither the flag nor any universal marker this scan knows, and passes.
    This is the biggest hole and it cannot be closed by a text scan; the honest
    claim is "the four sentences that existed, and their close relatives,
    cannot come back unnoticed" — NOT "no false promise can ever be written".
  * A CLAIM SPLIT ACROSS MORE THAN THREE LINES. (B) reads a +/-2-line window
    around the universal, because a comment block writes one sentence over
    several lines — the four sentences this ticket deleted all did. A claim
    whose subject sits further away than that is still invisible. The window is
    the SAME for the subject and for the qualifier on purpose: widening it for
    the subject alone manufactures reds, widening it for the qualifier alone
    manufactures greens.
  * INDIRECT GATING. (A) asks whether the ENCLOSING function reads the flag. A
    dispatch reached only through a caller that checks the flag is reported as
    ungated (conservative — it over-counts, which keeps (A) true and therefore
    keeps the prose banned; the failure direction is a guard that is too strict,
    never one that is too quiet).
  * NON-GO DISPATCH. Only server/ocserverd/*.go is scanned for (A).
  * FALSE POSITIVES, and this direction is the dangerous one to leave unsaid.
    (B) is three regexes and it CAN redden on a true sentence — a review found
    three. That matters more than it looks: the FAIL message tells the reader
    not to widen SCOPED, so somebody staring at a wrongly-flagged true sentence
    has only two moves left, and one of them is to quietly loosen a pattern
    until the row disappears. So: a false red here is NOT "the safe direction",
    it is the first step of a false green. If a TRUE sentence is flagged, add a
    real qualifier to it (name which dispatches, or point at §4.1) — and if it
    genuinely cannot be qualified because it is about something else entirely,
    that is a bug in COVERAGE, so fix COVERAGE and add the sentence to the
    selftest's TRUE_SENTENCES so the fix stays fixed.
  * NOT-YET-TRACKED FILES. The sweep is over `git ls-files`, so a brand-new
    file is invisible until it is added.
"""
from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path
from typing import List, Tuple

ROOT = Path(__file__).resolve().parent.parent
DISPATCH_DIR = ROOT / "server" / "ocserverd"

# The helpers that put a frame on a warden's FIFO. Discovered names, not a
# manifest: each is defined in this package and every warden command in the
# server funnels through one of them.
DISPATCH_HELPERS = ("enqueueWardenFrame", "enqueueToWarden", "enqueueWorkerStop")

FUNC_RE = re.compile(r"^func\s+(?:\([^)]*\)\s*)?(\w+)", re.M)
CALL_RE = re.compile(r"\bs\.(" + "|".join(DISPATCH_HELPERS) + r")\s*\(")

# (B) is a THREE-part shape, and the third part was added after a review found
# the second one alone reddening on true sentences about other subjects (see the
# FALSE POSITIVES bullet in the docstring). A line must name the subject …
SUBJECT = re.compile(
    r"no-reconcile|noReconcile|shadow-deploy|shadow deployment|shadow server|"
    r"shadow 部署|shadow 站",
    re.I,
)
# … it must be talking about WHAT REACHES A WARDEN (a sentence about, say, which
# database a shadow may be pointed at is a different and often true claim) …
COVERAGE = re.compile(
    r"warden|dispatch|command|kill|wake|agent|殺|命令|出口",
    re.I,
)
# … and it must quantify over all of them.
UNIVERSAL = re.compile(
    # Deliberately NOT a bare `every` / `never` / `nothing`. A first attempt at
    # widening this to catch two paraphrases reddened four TRUE lines in the
    # same run — including "the same reachability gate as every warden command",
    # which is a true statement about a different subject. So each alternative
    # here binds the quantifier TO the thing being quantified.
    r"\bEVERY\b|"
    r"every (?:other )?(?:event-driven )?(?:warden-)?command|"
    r"every (?:other )?(?:event-driven )?warden-command dispatch|"
    r"all warden|"
    # "no warden ANYWHERE RECEIVES a command" is a universal over wardens;
    # "no warden-command dispatch" is a noun phrase describing what a producer
    # does not do, and it is how the two TRUE lines in main.go / server.go are
    # worded. The (?!-) is what tells them apart, and it is load-bearing:
    # without it this guard reddens on the CLI help text and the serve banner.
    r"\b(?:no|zero) wardens?\b(?!-)[^.]{0,60}(?:receive|get\b|gets|see|is sent)|"
    r"(?:must|can|will|would) never|never (?:wake|kill|command|dispatch)|"
    r"沒有人殺人|沒有人重生|不會(?:對真|殺)|永不|全部封死|沒有例外",
)
# The qualifiers that make such a sentence TRUE. Every entry must be a phrase
# that ACTUALLY NARROWS the claim: an earlier draft listed 決策迴圈, which the
# false sentence in offboard-flow.md used too, so the qualifier let the very
# line it was meant to catch walk straight through.
SCOPED = re.compile(
    # NOT a bare `producer`: the false sentence in main.go says "the reconcile
    # producer" too, so that word alone lets the very line this guard exists to
    # catch walk through — the same shape as the 決策迴圈 mistake below, found
    # the same way (the selftest went green when it must not have).
    r"this producer|THIS producer|it owns|IT OWNS|producer's|producer owns|"
    # The window is joined with newlines and comment markers, so a qualifier
    # split across two comment lines ("… and nothing" / "// outside it") needs
    # slack between the words or it reads as unqualified.
    r"§ ?4\.1|射程|不受.{0,6}旗標|nothing[\s\S]{0,8}outside|only the|"
    r"not a server-wide",
)

TEXT_SUFFIXES = (".go", ".md", ".py", ".ts", ".tsx", ".sh", ".json")
# This file states the banned shapes in order to detect them; scanning itself
# would report every pattern above as a violation.
WINDOW = 2  # lines of context searched for the subject AND the qualifier
SKIP_FILES = {"bin/shadow-claim-guard.py", "bin/tests/shadow-claim-guard-selftest.py"}


def tracked(root: Path) -> List[str]:
    out = subprocess.run(
        ["git", "-C", str(root), "ls-files"], capture_output=True, text=True, check=True
    )
    return [p for p in out.stdout.split("\n") if p]


def ungated_dispatches(root: Path) -> List[str]:
    """(A): dispatch call sites whose enclosing func never reads noReconcile."""
    rows: List[str] = []
    src = root / "server" / "ocserverd"
    for path in sorted(src.glob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        text = path.read_text(encoding="utf-8")
        starts = [(m.start(), m.group(1)) for m in FUNC_RE.finditer(text)]
        for call in CALL_RE.finditer(text):
            enclosing = None
            for i, (pos, name) in enumerate(starts):
                if pos <= call.start() and (i + 1 == len(starts) or starts[i + 1][0] > call.start()):
                    enclosing = (pos, name, starts[i + 1][0] if i + 1 < len(starts) else len(text))
                    break
            if enclosing is None:
                continue
            pos, name, end = enclosing
            body = text[pos:end]
            # A helper calling its own sibling is the plumbing, not a dispatch site.
            if name in DISPATCH_HELPERS:
                continue
            if "noReconcile" in body:
                continue
            line = text.count("\n", 0, call.start()) + 1
            rows.append(f"server/ocserverd/{path.name}:{line} {name}() → {call.group(1)}()")
    return rows


def universal_claims(root: Path) -> List[str]:
    """(B): lines naming the flag that make an unqualified universal claim."""
    rows: List[str] = []
    for rel in tracked(root):
        if rel in SKIP_FILES or not rel.endswith(TEXT_SUFFIXES):
            continue
        path = root / rel
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        lines = text.split("\n")
        for n, line in enumerate(lines, 1):
            if not UNIVERSAL.search(line):
                continue
            if not COVERAGE.search(line):
                continue
            # A prose claim is written across a comment block, so the subject and
            # the qualifier that scopes it may sit on neighbouring lines. The
            # window is deliberately the SAME for both: widening it for the
            # subject alone would manufacture reds, widening it for the qualifier
            # alone would manufacture greens.
            window = "\n".join(lines[max(0, n - 1 - WINDOW): n + WINDOW])
            if not SUBJECT.search(window):
                continue
            if SCOPED.search(window):
                continue
            rows.append(f"{rel}:{n} {line.strip()[:140]}")
    return rows


# The owner's second ruling (T-941e, 2026-08-18): once the false promise is
# gone, SAY THE TRUE THING IN ITS PLACE. That sentence needs the same protection
# as the ban — deleting it is silent, and its absence is exactly the state that
# let the false one live unexamined. Same conjunction as everything else here:
# it is required only while (A) holds, so closing the holes in code retires this
# obligation automatically instead of freezing it.
REQUIRED_WARNINGS = (
    ("server/ocserverd/api_stub.go", "STILL COMMANDS REAL"),
    ("docs/design/offboard-flow.md", "演練站不是安全的沙盒"),
)


def missing_warnings(root: Path) -> List[str]:
    rows: List[str] = []
    for rel, marker in REQUIRED_WARNINGS:
        try:
            text = (root / rel).read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            rows.append(f"{rel} (unreadable — the warning cannot be there)")
            continue
        if marker not in text:
            rows.append(f"{rel} no longer says {marker!r}")
    return rows


def main() -> None:
    ungated = ungated_dispatches(ROOT)
    claims = universal_claims(ROOT)
    absent = missing_warnings(ROOT) if ungated else []
    if ungated and claims:
        listing = "\n  ".join(claims)
        sites = "\n  ".join(ungated)
        print(
            "FAIL — a sentence promises --no-reconcile covers every warden command, "
            f"but {len(ungated)} dispatch site(s) never read the flag.\n\n"
            f"  the sentences:\n  {listing}\n\n"
            f"  the ungated dispatch sites:\n  {sites}\n\n"
            "  Two honest fixes, and the choice is a RULING, not a lint decision:\n"
            "    * scope the sentence (say WHICH dispatches — 'the ones this producer\n"
            "      owns' — or point at spec/lifecycle.md §4.1, which enumerates the\n"
            "      holes and is the one place in this tree that states this correctly);\n"
            "    * or close the holes in code, at which point this guard goes quiet by\n"
            "      itself and the universal sentence becomes true and allowed.\n"
            "  Do NOT widen SCOPED to make a row disappear: a qualifier that does not\n"
            "  actually narrow the claim turns this guard into decoration.",
            file=sys.stderr,
        )
        sys.exit(1)
    if absent:
        rows = "\n  ".join(absent)
        print(
            f"FAIL — {len(ungated)} warden-command dispatch site(s) still ignore "
            "--no-reconcile, so a shadow server still kills real sessions, and the "
            f"warning that says so is gone:\n  {rows}\n\n"
            "  The owner ruled (T-941e) that the true sentence takes the place of the\n"
            "  false one. If you closed the holes in code, this check retires itself —\n"
            "  the list above is only consulted while an ungated dispatch exists. If you\n"
            "  moved the warning, move the marker here with it. Do NOT delete the marker\n"
            "  to make this pass: an operator rehearsing on a shadow station has nothing\n"
            "  else telling them which buttons are live.",
            file=sys.stderr,
        )
        sys.exit(1)
    print(
        f"[shadow-claim-guard] all green ({len(ungated)} ungated warden-command "
        f"dispatch site(s), {len(claims)} unqualified universal claim(s), "
        f"{len(REQUIRED_WARNINGS)} required warning(s) present — "
        "the pair is what reddens, so a zero on EITHER side is a pass)"
    )


if __name__ == "__main__":
    main()
