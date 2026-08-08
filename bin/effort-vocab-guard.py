#!/usr/bin/env python3
"""effort-vocab-guard — every hand-written copy of the CONFIGURED effort
vocabulary must list exactly the set the server actually enforces.

THE INCIDENT THIS GUARDS AGAINST. `effort` is a closed vocabulary (an unknown
value is a 422, never silently coerced) but it is written down by hand in a
dozen places: the server gate, seven server error strings, the codex launcher's
own re-enumeration, the frontend union type, the dropdown array, the mock's
runtime checks, two locales' label maps, and prose in the spec and the docs.
Adding a level means editing all of them. Miss one and the failure is SILENT in
the worst possible way — T-dbd4 found `normalizeCodexEffort` in the codex
launcher coercing every value it does not recognise down to "medium", so a new
level would have been selectable in the cockpit, storable, readable back, and
WRONG only in the one place nobody can see: the effort the session actually
launched at. Nothing goes red for that today.

THE DESIGN THAT WOULD HAVE FAILED SILENTLY. The obvious guard is a list of the
places to check. That list is a forward-only allowlist: the day someone adds a
thirteenth copy the guard keeps printing all green over twelve, and a list that
is missing an entry does not go red, it just covers less. So the copies are
DISCOVERED from the tree on every run, by shape, and the enumeration count is
printed so a narrowed scan shows up as a smaller number rather than as silence.

THE RULE, STATED AS A QUERY THAT MUST COME BACK EMPTY:

    for every place in the tree that WRITES DOWN the effort vocabulary
      — a `must be one of …` message, a union type, an array literal,
        an `=== "x" || … === "y"` chain, an `effortOf` label map,
        or prose listing the levels on a line that says "effort" —
    the set it lists ≠ the set `validEffort` enforces

    ⇒ the answer must be zero rows.

TWO PROPERTIES ARE LOAD-BEARING and a future edit must not lose them:

  1. The source of truth is the RUNNING GATE (`validEffort` in
     server/ocserverd/api_helpers.go), not a manifest invented for this guard.
     A manifest nothing executes can agree with itself while the gate disagrees
     with the world; `validEffort` is the function that actually 422s, so
     "copy disagrees with SSOT" always means "the copy is wrong".
  2. The scan is SHAPE-DRIVEN, never a path list. The only hardcoded paths are
     in SKIP_FILES below, and every one of them carries a committed reason —
     that list is the sanctioned exception precisely because the only way to
     bypass it is to edit it, and that edit is the review this guard exists to
     force.

WHAT THIS GUARD DELIBERATELY DOES NOT COVER — say it out loud rather than let a
green imply it. Each bullet below was constructed and its behaviour OBSERVED
during T-dbd4's review rounds — they are measurements, not predictions:

  * A listing SPLIT ACROSS TWO LINES. Every sweep here is per-line, so a marker
    on one line and its list on the next is invisible. (Found live during T-dbd4:
    frontend/CLAUDE.md said "投入程度 =" then "低/中/高" on the following line. It
    was reflowed onto one line when it was fixed, so it is inside the sweep now —
    the shape stays a hole even though that instance is gone.)
  * A copy on a line that never says "effort" (or 思考強度 / 投入). This gates
    the prose sweep AND the array-literal shape — an earlier version of this
    paragraph claimed the structural checks were marker-free, and that was
    simply false. The gate is load-bearing rather than lazy: task PRIORITY is
    high/mid/low/frozen, which overlaps this vocabulary by two words and would
    redden on every run, and an alarm that cries wolf is one everybody learns to
    ignore. (Found live during T-dbd4: a test whose NAME still described the old
    vocabulary. Renaming it to say "effort" pulled it INTO the sweep, which is the
    cheapest way to opt a line in.)
  * A COUNT instead of a list — "the 3-item picker", "three levels". There is no
    listing to compare, so nothing here can see it.
  * A switch whose function is not named normalize*Effort*, a TS union under a
    type name other than Effort, or a file whose suffix is not in TEXT_SUFFIXES.

NOT a bypass, and listed here because an earlier version of this paragraph said it
was: a list ASSEMBLED AT RUNTIME (string concatenation, strings.Join) reddens
rather than escaping — the scan can only read literals, so it sees an empty list
and says so. That errs in the safe direction but it cannot be SATISFIED by writing
the message correctly, so the failure message for that case says what to do
instead of repeating "your copy is wrong".

The residue is real and it is mostly documentation phrasing; the job of catching
it belongs to the doc-truth step of a change, not to a green here.

REPORTED effort is a different thing and is NOT in scope. `actual_effort` /
`effortLabel` render whatever the harness reports, verbatim and unvalidated, by
design (cli/CLAUDE.md is explicit about the passthrough). A value like "xhigh"
appearing there says only that the channel accepts any string; it is not a
configurable level and must not be dragged into this set.

bin/tests/effort-vocab-guard-selftest.py is what keeps this honest: one planted
mutant per copy shape, each of which must still be caught AND still be named.
Both halves are dispatched by bin/ci.sh and bin/ci-cloud.sh — a scanner nobody
verified is a green with a hole in it.
"""

import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Dict, List, NoReturn, Set, Tuple

ROOT = Path(os.environ.get("OC_EFFORT_GUARD_ROOT", Path(__file__).resolve().parents[1]))

# The gate itself. Overridable only so the selftest can point the guard at a
# staged copy of the tree; production runs never set it.
SSOT_FILE = "server/ocserverd/api_helpers.go"
SSOT_FUNC = "validEffort"

# Files whose vocabulary-shaped text is NOT a copy of the vocabulary. Every
# entry needs a committed reason; adding one is the review this guard forces.
SKIP_FILES = {
    # This guard and its selftest spell the vocabulary out in prose and in
    # fixtures. A predicate that counts itself is the classic self-matching
    # false positive.
    "bin/effort-vocab-guard.py": "the predicate itself",
    "bin/tests/effort-vocab-guard-selftest.py": "the predicate's own fixtures",
}

TEXT_SUFFIXES = {".go", ".ts", ".tsx", ".js", ".mjs", ".py", ".sh", ".md", ".json", ".yml", ".yaml"}

# A line is talking about CONFIGURED effort if it names it. Chinese docs use
# 思考強度 / 投入; task PRIORITY uses the same 高/中/低 glyphs and must not be
# dragged in, which is exactly what this marker keeps out.
EFFORT_MARKER = re.compile(r"effort|思考強度|投入", re.I)

# Chinese renderings of the levels. Committed here on purpose: the zh labels have
# no mechanical link to the English keys, so without this table a new level would
# silently keep the old Chinese labels in the docs. Adding a level means adding
# its rendering here too — that edit is the point, not an obstacle.
# Matched longest-first so a multi-character label wins over a substring of it
# (最高 must not be read as 高).
ZH_LABEL = {"低": "low", "中": "medium", "高": "high", "最高": "max"}
ZH_TOKEN = "(?:" + "|".join(sorted(ZH_LABEL, key=len, reverse=True)) + ")"


def fail(message: str) -> NoReturn:
    print("[effort-vocab-guard] FAIL — " + message, file=sys.stderr)
    raise SystemExit(1)


def tracked_files() -> List[str]:
    out = subprocess.run(
        ["git", "ls-files"], cwd=ROOT, capture_output=True, text=True
    )
    if out.returncode != 0:
        # A staged copy is not a git repo; walk it instead. Same file set, same
        # order discipline — sorted, so failure output is stable.
        found = []
        for path in sorted(ROOT.rglob("*")):
            if path.is_file() and path.suffix in TEXT_SUFFIXES:
                found.append(str(path.relative_to(ROOT)))
        return found
    return sorted(p for p in out.stdout.split("\n") if p)


def read(rel: str) -> str:
    return (ROOT / rel).read_text(errors="replace")


def ssot() -> Set[str]:
    """The set validEffort enforces, read out of the function body."""
    try:
        text = read(SSOT_FILE)
    except OSError as exc:
        fail(f"cannot read the source of truth {SSOT_FILE}: {exc}")
    anchors = [m for m in re.finditer(rf"func {SSOT_FUNC}\(", text)]
    if len(anchors) != 1:
        fail(
            f"{SSOT_FILE}: expected exactly 1 definition of {SSOT_FUNC}, found "
            f"{len(anchors)}. The source of truth must be unambiguous — if the "
            f"gate moved, move this guard's anchor with it; do NOT add a second gate."
        )
    body = text[anchors[0].end():]
    end = body.find("\n}")
    if end == -1:
        fail(f"{SSOT_FILE}: cannot find the end of {SSOT_FUNC}'s body")
    values = set(re.findall(r'"([a-z]+)"', body[:end]))
    if not values:
        fail(
            f"{SSOT_FILE}: {SSOT_FUNC} lists no effort values. An empty source of "
            f"truth would make every comparison below vacuously pass."
        )
    return values


def words(region: str) -> Set[str]:
    return set(re.findall(r"[a-z]{2,12}", region))


class Finding:
    def __init__(self, rel: str, line: int, shape: str, listed: Set[str], excerpt: str):
        self.rel, self.line, self.shape, self.listed, self.excerpt = rel, line, shape, listed, excerpt

    def render(self, truth: Set[str]) -> str:
        missing = sorted(truth - self.listed)
        extra = sorted(self.listed - truth)
        detail = []
        if missing:
            detail.append("missing " + ", ".join(missing))
        if extra:
            detail.append("has " + ", ".join(extra) + " which the gate does not accept")
        return (
            f"{self.rel}:{self.line} [{self.shape}] lists "
            f"{{{', '.join(sorted(self.listed))}}} — {'; '.join(detail)}\n"
            f"      {self.excerpt.strip()[:120]}"
        )


def line_of(text: str, index: int) -> int:
    return text.count("\n", 0, index) + 1


def excerpt_at(text: str, index: int) -> str:
    start = text.rfind("\n", 0, index) + 1
    end = text.find("\n", index)
    return text[start: end if end != -1 else len(text)]


def scan(truth: Set[str]) -> Tuple[List[Finding], List[str], Dict[str, int]]:
    findings: List[Finding] = []
    unreadable: List[str] = []
    counts = {
        "must-be-one-of": 0,
        "codex-normalize": 0,
        "union-type": 0,
        "array-literal": 0,
        "equality-chain": 0,
        "label-map": 0,
        "prose": 0,
        "unreadable-message": 0,
    }

    for rel in tracked_files():
        if rel in SKIP_FILES:
            continue
        if Path(rel).suffix not in TEXT_SUFFIXES:
            continue
        try:
            text = read(rel)
        except OSError:
            continue
        if rel.endswith(".json"):
            # JSON keeps its newlines as the two characters \ and n, so a
            # description reading "…outside\nlow/medium/high…" would otherwise be
            # scanned as the word "nlow" and reported as an unknown level.
            text = re.sub(r"\\[nrt]", " ", text)
        if "effort" not in text.lower() and not EFFORT_MARKER.search(text):
            continue

        def record(shape: str, index: int, listed: Set[str]) -> None:
            counts[shape] += 1
            if listed != truth:
                findings.append(
                    Finding(rel, line_of(text, index), shape, listed, excerpt_at(text, index))
                )

        # ── shape 1: a "must be one of …" validation message ─────────────────
        # The region ends at the first ';' so the "; got '<value>'" tail does
        # not leak the word "got" into the listed set.
        for m in re.finditer(r"effort must be one of([^\";]*)", text, re.I):
            listed = words(m.group(1))
            if not listed:
                # OBSERVED: the phrase is here and no list follows it on this line.
                # WHY is not observable from here — it could be a message assembled
                # at runtime, a test asserting only the prefix, or prose naming the
                # message — so this says what it saw and lets the reader pick the
                # remedy. An earlier version diagnosed "built at runtime", which the
                # scan never established and which was wrong for two of the three.
                counts["unreadable-message"] += 1
                unreadable.append(
                    f"{rel}:{line_of(text, m.start())} names 'effort must be one "
                    f"of' with no list after it on this line\n"
                    f"      {excerpt_at(text, m.start()).strip()[:120]}"
                )
                continue
            record("must-be-one-of", m.start(), listed)

        # ── shape 2: the codex launcher's own re-enumeration ─────────────────
        # `case "low", "high": … default: return "medium"` — the ACCEPTED set is
        # the cases plus whatever default returns, because the default silently
        # coerces. That coercion is the silent failure this guard exists for, so
        # the default's value counts as a listed level, not as an escape.
        for m in re.finditer(r"func\s+normalize\w*Effort\w*\(", text, re.I):
            body = text[m.end():]
            end = body.find("\n}")
            if end == -1:
                fail(f"{rel}:{line_of(text, m.start())} cannot find the end of the effort normaliser")
            record("codex-normalize", m.start(), set(re.findall(r'"([a-z]+)"', body[:end])))

        # ── shape 3: a union type ────────────────────────────────────────────
        for m in re.finditer(r"type\s+Effort\s*=\s*([^;\n]+)", text):
            record("union-type", m.start(), words(m.group(1)))

        # ── shape 4: an array literal of level strings ───────────────────────
        # Discovered by shape (a bracketed run of >=2 short lowercase strings
        # that overlaps the vocabulary), not by variable name — renaming the
        # constant must not retire the check. The effort marker on the same line
        # is what keeps task PRIORITY out: ["high","mid","low","frozen"] overlaps
        # this vocabulary by two words and is a different closed set entirely.
        # Both bracket styles: a TS/Python/JSON array and a Go composite literal
        # (`[]string{"low", …}`), which is the same copy wearing different
        # punctuation and was a confirmed bypass until this line covered it.
        for m in re.finditer(
            r"[\[{]\s*((?:['\"][a-z]{2,12}['\"]\s*,?\s*){2,})[\]}]", text
        ):
            listed = words(m.group(1))
            if listed & truth and EFFORT_MARKER.search(excerpt_at(text, m.start())):
                record("array-literal", m.start(), listed)

        # ── shape 5: an `=== "x" || … === "y"` chain on an effort variable ────
        for m in re.finditer(
            r"(\w*[eE]ffort\w*)\s*===\s*[\"'][a-z]+[\"'](?:\s*\|\|\s*\w*[eE]ffort\w*\s*===\s*[\"'][a-z]+[\"'])+",
            text,
        ):
            record("equality-chain", m.start(), words(m.group(0).replace(m.group(1), "")))

        # ── shape 6: an effortOf label map ───────────────────────────────────
        for m in re.finditer(r"effortOf\s*:\s*\{", text):
            depth, i = 0, m.end() - 1
            while i < len(text):
                if text[i] == "{":
                    depth += 1
                elif text[i] == "}":
                    depth -= 1
                    if depth == 0:
                        break
                i += 1
            else:
                fail(f"{rel}:{line_of(text, m.start())} unbalanced effortOf map")
            record("label-map", m.start(), set(re.findall(r"(\w+)\s*:", text[m.end(): i])))

        # ── shape 7: prose listing the levels ────────────────────────────────
        for idx, line in enumerate(text.split("\n"), 1):
            if not EFFORT_MARKER.search(line):
                continue
            start = sum(len(part) + 1 for part in text.split("\n")[: idx - 1])
            for m in re.finditer(
                r"[a-z]{2,12}(?:\s*(?:[/,|]|\bor\b)\s*[a-z]{2,12}){1,}", line
            ):
                # Trim the run to the span between the FIRST and LAST token that
                # the gate accepts. Prose runs on past the list ("low/medium/high,
                # from member.effort") and a raw span would report the trailing
                # word as an unknown level — a false red, and a guard that cries
                # wolf is one everybody learns to ignore. A stale copy still goes
                # red: it lists the old set, and the old set is not the new one.
                tokens = re.findall(r"[a-z]{2,12}", m.group(0))
                known = [i for i, tok in enumerate(tokens) if tok in truth]
                if len(known) < 2:
                    continue
                record("prose", start + m.start(), set(tokens[known[0]: known[-1] + 1]))
            for m in re.finditer(rf"{ZH_TOKEN}(?:\s*[/／、]\s*{ZH_TOKEN})+", line):
                listed = {ZH_LABEL[tok] for tok in re.findall(ZH_TOKEN, m.group(0))}
                if listed:
                    record("prose", start + m.start(), listed)

    return findings, unreadable, counts


def main() -> None:
    truth = ssot()
    findings, unreadable, counts = scan(truth)
    if findings:
        listing = "\n  ".join(f.render(truth) for f in sorted(findings, key=lambda f: (f.rel, f.line)))
        fail(
            f"the effort vocabulary enforced by {SSOT_FILE}:{SSOT_FUNC} is "
            f"{{{', '.join(sorted(truth))}}}, but these copies disagree:\n  {listing}\n\n"
            "  Fix the copies, not this guard. Do NOT narrow the scan to make a row go "
            "away: over-matching costs one committed line in SKIP_FILES with a reason, "
            "while a narrowed scan silently covers less and nothing here goes red for it. "
            "If a level really is not meant to reach one of these places, that is a "
            "coercion the cockpit cannot see — say so in code, do not hide it here."
        )
    if unreadable:
        rows = "\n  ".join(unreadable)
        fail(
            "these lines name the validation message but carry no list this scan "
            f"can read, so nothing here compares them to {SSOT_FILE}:{SSOT_FUNC}:\n  "
            f"{rows}\n\n"
            "  Pick the one that is true and act on it — this guard cannot tell them "
            "apart:\n"
            "    * the message is assembled at runtime ⇒ write the levels as a "
            "literal, or the printed list can drift forever with nothing to catch it;\n"
            "    * this only NAMES the message (a test matching the prefix, prose "
            "about it) and the list lives elsewhere ⇒ that elsewhere is what this "
            "guard should be reading, so add this file to SKIP_FILES with a committed "
            "reason;\n"
            "    * the list was meant to be here and got lost ⇒ put it back."
        )
    total = sum(counts.values())
    breakdown = ", ".join(f"{n} {shape}" for shape, n in sorted(counts.items()) if n)
    print(
        f"[effort-vocab-guard] all green ({total} vocabulary copies compared against "
        f"{{{', '.join(sorted(truth))}}} — {breakdown})"
    )


if __name__ == "__main__":
    main()
