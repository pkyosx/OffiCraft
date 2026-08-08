#!/usr/bin/env python3
"""Positive control for bin/effort-vocab-guard.py — one planted mutant per copy
shape the guard claims to cover, each of which must still be caught AND still be
NAMED.

Why this file exists at all. A guard that scans by shape can have its scan
narrowed by one line — a tightened regex, an extra SKIP_FILES entry, a `continue`
— and the only visible effect is that it keeps printing all green over a smaller
set. The scanner cannot detect that about itself. So every shape gets a mutant
here, and the guard must go red on it and must name the file and line, because
"went red" alone is satisfied by a guard that reddens for an unrelated reason.

Two properties make this a control rather than decoration, and a future edit
must not lose either:

  1. The unmodified staged copy is checked GREEN FIRST. If the clean copy were
     already red every case below would "pass" for the wrong reason, and the
     whole file would be a rubber stamp.
  2. Each case commits WHAT IT EXPECTS TO SEE, and EXPECTED_CASES commits the
     set of (name, expectation) pairs. A count floor is too weak (delete one
     with teeth, add a harmless one); a set of names is too weak (flip an
     expectation to None and you retire the rule and its control together).

Most fixtures are synthetic: the guard is a scanner over text shapes, so a
purpose-written file exercises a shape more precisely than a copy of production
code that happens to contain it, and it does not go stale when that code moves.
The exceptions are the two `ssot-grows-*` cases, which stage PRODUCTION bytes
only (REAL_ONLY below) and grow the vocabulary without touching anything else —
that is the T-dbd4 scenario in the flesh, and they are what prove the guard bites
on production code rather than only on fixtures. They get their own green-first
check for the reason spelled out at REAL_ONLY: a case that starts red passes
whether or not its mutation does anything.

Run: python3 bin/tests/effort-vocab-guard-selftest.py
"""

import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Callable, List, Optional, Tuple

ROOT = Path(__file__).resolve().parents[2]
GUARD = ROOT / "bin/effort-vocab-guard.py"

SSOT_REL = "server/ocserverd/api_helpers.go"
CODEX_REL = "cli/ocwarden/codex_session.go"

# ── the synthetic clean tree ──────────────────────────────────────────────────
# One file per shape the guard covers, each listing exactly {low, medium, high}.
CLEAN_TREE = {
    SSOT_REL: (
        "package ocserverd\n\n"
        "func validEffort(effort string) bool {\n"
        '\treturn effort == "low" || effort == "medium" || effort == "high"\n'
        "}\n"
    ),
    "server/ocserverd/api_members.go": (
        "package ocserverd\n\n"
        "func patchMember() string {\n"
        '\treturn "effort must be one of [high low medium]; got \'x\'"\n'
        "}\n"
    ),
    CODEX_REL: (
        "package ocwarden\n\n"
        "func normalizeCodexEffort(effort string) string {\n"
        "\tswitch effort {\n"
        '\tcase "low", "high":\n'
        "\t\treturn effort\n"
        "\tdefault:\n"
        '\t\treturn "medium"\n'
        "\t}\n"
        "}\n"
    ),
    "frontend/src/types.ts": 'export type Effort = "low" | "medium" | "high";\n',
    "frontend/src/components/ModelEffortEditor.tsx": (
        'export const EFFORTS: readonly Effort[] = ["low", "medium", "high"] as const;\n'
    ),
    "frontend/src/components/AgentDetailPanel.tsx": (
        "const known =\n"
        '  shownEffort === "low" || shownEffort === "medium" || shownEffort === "high";\n'
    ),
    "frontend/src/i18n/locales/en.ts": (
        "export const en = {\n"
        '  effortOf: { low: "Low", medium: "Medium", high: "High" },\n'
        "};\n"
    ),
    "docs/guide/members.md": (
        "- **EFFORT · Thinking（思考強度）** — 這個成員的推理強度（低／中／高）。\n"
    ),
    # ── negative-control fixtures: vocabulary-shaped text that is NOT this
    # vocabulary. These must never make the clean tree red.
    "cli/ocagent/contextreport.go": (
        "package ocagent\n\n"
        "// effortLabel renders the REPORTED effort verbatim — any string the\n"
        "// harness sends, validated nowhere.\n"
        "func effortLabel(level string) string {\n"
        '\tif level == "medium" {\n'
        '\t\treturn "med"\n'
        "\t}\n"
        "\treturn level\n"
        "}\n"
    ),
    "frontend/src/components/TaskCard.tsx": (
        '// priority dropdown (高/中/低/凍結 incl. freeze)\n'
        'const PRIORITIES = ["high", "mid", "low", "frozen"] as const;\n'
    ),
}

CASES: List[Tuple[str, Callable[[Path], None], Optional[str]]] = []


def case(name: str, expect: Optional[str]):
    def register(fn):
        CASES.append((name, fn, expect))
        return fn

    return register


def stage(tmp: Path, files: dict) -> Path:
    root = tmp / "tree"
    for rel, body in files.items():
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(body)
    return root


def stage_clean(tmp: Path) -> Path:
    return stage(tmp, CLEAN_TREE)


# Production files staged verbatim for the real-tree cases. Nothing synthetic is
# mixed in: an earlier version copied two real files ON TOP of the fixture tree,
# and once the real vocabulary grew past the fixtures' hardcoded three values that
# hybrid was ALREADY red before any mutation — which makes a case pass whether or
# not its mutation does anything. A case that starts red proves nothing, so this
# tree is checked green first, exactly like the fixture tree.
REAL_ONLY = (SSOT_REL, CODEX_REL, "frontend/src/types.ts")


def stage_real(tmp: Path) -> Path:
    root = tmp / "tree"
    for rel in REAL_ONLY:
        (root / rel).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy(ROOT / rel, root / rel)
    return root


def run_guard(root: Path) -> Tuple[int, str]:
    out = subprocess.run(
        [sys.executable, str(GUARD)],
        capture_output=True,
        text=True,
        env={**os.environ, "OC_EFFORT_GUARD_ROOT": str(root)},
    )
    return out.returncode, out.stdout + out.stderr


def edit(root: Path, rel: str, old: str, new: str) -> None:
    """Replace and PROVE the replacement landed — a mutant that silently fails
    to apply is indistinguishable from a guard that failed to catch it."""
    path = root / rel
    text = path.read_text()
    if old not in text:
        raise AssertionError(f"mutant anchor not present in {rel}: {old!r}")
    path.write_text(text.replace(old, new, 1))
    if path.read_text() == text:
        raise AssertionError(f"mutant did not change {rel}")


# ── one mutant per shape ──────────────────────────────────────────────────────


@case("error-string-drops-a-level", "server/ocserverd/api_members.go:4")
def _(root: Path) -> None:
    edit(root, "server/ocserverd/api_members.go", "[high low medium]", "[high low]")


@case("codex-normaliser-drops-a-level", "cli/ocwarden/codex_session.go:3")
def _(root: Path) -> None:
    edit(root, CODEX_REL, '\tcase "low", "high":', '\tcase "low":')


@case("union-type-drops-a-level", "frontend/src/types.ts:1")
def _(root: Path) -> None:
    edit(root, "frontend/src/types.ts", ' | "high"', "")


@case("dropdown-array-drops-a-level", "frontend/src/components/ModelEffortEditor.tsx:1")
def _(root: Path) -> None:
    edit(root, "frontend/src/components/ModelEffortEditor.tsx", ', "high"', "")


@case("equality-chain-drops-a-level", "frontend/src/components/AgentDetailPanel.tsx:2")
def _(root: Path) -> None:
    edit(
        root,
        "frontend/src/components/AgentDetailPanel.tsx",
        ' || shownEffort === "high"',
        "",
    )


@case("label-map-drops-a-level", "frontend/src/i18n/locales/en.ts:2")
def _(root: Path) -> None:
    edit(root, "frontend/src/i18n/locales/en.ts", ', high: "High"', "")


@case("chinese-doc-prose-drops-a-level", "docs/guide/members.md:1")
def _(root: Path) -> None:
    edit(root, "docs/guide/members.md", "低／中／高", "低／中")


@case("message-named-but-list-not-readable", "server/ocserverd/api_members.go:6 names 'effort must be one of' with no list")
def _(root: Path) -> None:
    """A validation message whose list this scan cannot read must not pass as
    "nothing to compare". The expectation pins the RENDERED SENTENCE, not the
    file:line — with only the line pinned this case passed even with the rule
    deleted, because the generic mismatch finding names the same line (verified)."""
    path = root / "server/ocserverd/api_members.go"
    path.write_text(
        path.read_text().replace(
            "}\n",
            'func assertMessage(err error) bool {\n'
            '\treturn strings.Contains(err.Error(), "effort must be one of")\n}\n',
            1,
        )
    )


# ── the scenario this guard was built for ─────────────────────────────────────


@case("ssot-grows-real-tree", CODEX_REL)
def _(root: Path) -> None:
    """Grow the vocabulary in the REAL gate and change nothing else. The copy
    that must be named is the REAL codex launcher, because that is the one whose
    staleness is invisible from the cockpit: it swallows the unknown level into
    its catch-all and the session launches at the wrong effort with nothing going
    red. The added level is FICTIONAL on purpose — using a real one would make
    this case pass or fail depending on what the vocabulary happens to be today,
    and it would stop biting the moment that level got added for real."""
    edit(
        root,
        SSOT_REL,
        "func validEffort(effort string) bool {",
        'func validEffort(effort string) bool {\n\tif effort == "selftestonly" {\n\t\treturn true\n\t}',
    )


@case("ssot-grows-names-every-stale-copy", "frontend/src/types.ts")
def _(root: Path) -> None:
    """Same growth, seen from the other end: EVERY stale copy must be listed,
    not just the first one the scanner happens to reach. Fictional level for the
    same reason as above."""
    edit(
        root,
        SSOT_REL,
        "func validEffort(effort string) bool {",
        'func validEffort(effort string) bool {\n\tif effort == "selftestonly" {\n\t\treturn true\n\t}',
    )


# ── negative controls: these must stay GREEN ──────────────────────────────────


@case("reported-effort-passthrough-is-not-a-copy", None)
def _(root: Path) -> None:
    """The telemetry channel renders whatever the harness reports. Adding a
    value there says the channel accepts any string; it is not a configurable
    level and must not redden this guard."""
    path = root / "cli/ocagent/contextreport.go"
    path.write_text(
        path.read_text().replace(
            '\treturn level\n',
            '\t// e.g. "xhigh" arrives here and is rendered verbatim\n\treturn level\n',
        )
    )


@case("task-priority-vocabulary-is-not-this-vocabulary", None)
def _(root: Path) -> None:
    """Task priority is high/mid/low/frozen and shares two words plus the same
    Chinese glyphs. A guard that reddens on it would be teaching everyone to
    ignore it."""
    path = root / "frontend/src/components/TaskCard.tsx"
    path.write_text(path.read_text() + 'const AGAIN = ["high", "mid", "low"] as const;\n')


EXPECTED_CASES = frozenset(
    {
        ("error-string-drops-a-level", "server/ocserverd/api_members.go:4"),
        ("codex-normaliser-drops-a-level", "cli/ocwarden/codex_session.go:3"),
        ("union-type-drops-a-level", "frontend/src/types.ts:1"),
        ("dropdown-array-drops-a-level", "frontend/src/components/ModelEffortEditor.tsx:1"),
        ("equality-chain-drops-a-level", "frontend/src/components/AgentDetailPanel.tsx:2"),
        ("label-map-drops-a-level", "frontend/src/i18n/locales/en.ts:2"),
        ("chinese-doc-prose-drops-a-level", "docs/guide/members.md:1"),
        ("message-named-but-list-not-readable", "server/ocserverd/api_members.go:6 names 'effort must be one of' with no list"),
        ("ssot-grows-real-tree", CODEX_REL),
        ("ssot-grows-names-every-stale-copy", "frontend/src/types.ts"),
        ("reported-effort-passthrough-is-not-a-copy", None),
        ("task-priority-vocabulary-is-not-this-vocabulary", None),
    }
)


def main() -> None:
    registered = frozenset((name, expect) for name, _fn, expect in CASES)
    if registered != EXPECTED_CASES:
        added = sorted(str(one) for one in registered - EXPECTED_CASES)
        gone = sorted(str(one) for one in EXPECTED_CASES - registered)
        print(
            "[effort-vocab-selftest] FAIL — the registered cases no longer match the "
            "committed set. Retiring a control is a decision, so it has to be an edit "
            f"here too.\n  added: {added}\n  gone:  {gone}",
            file=sys.stderr,
        )
        raise SystemExit(1)

    failures: List[str] = []

    with tempfile.TemporaryDirectory() as tmp:
        clean = stage_clean(Path(tmp))
        rc, output = run_guard(clean)
        if rc != 0:
            print(
                "[effort-vocab-selftest] FAIL — the unmodified copy is already red, so "
                f"every case below would 'pass' for the wrong reason:\n{output}",
                file=sys.stderr,
            )
            raise SystemExit(1)

    for name, plant, expect in CASES:
        with tempfile.TemporaryDirectory() as tmp:
            real_tree = name.startswith("ssot-grows")
            root = stage_real(Path(tmp)) if real_tree else stage_clean(Path(tmp))
            if real_tree:
                rc, output = run_guard(root)
                if rc != 0:
                    failures.append(
                        f"{name}: the unmutated PRODUCTION copies are already red, so "
                        f"this case would pass for the wrong reason:\n{output}"
                    )
                    continue
            try:
                plant(root)
            except AssertionError as exc:
                failures.append(f"{name}: the mutant did not apply — {exc}")
                continue
            rc, output = run_guard(root)

            if expect is None:
                if rc != 0:
                    failures.append(
                        f"{name}: this must stay GREEN but the guard went red:\n{output}"
                    )
                continue
            if rc == 0:
                failures.append(f"{name}: the guard did NOT notice it")
            elif expect not in output:
                failures.append(
                    f"{name}: went red but never named {expect}, so it caught something "
                    f"else:\n{output}"
                )

    if failures:
        print(
            "[effort-vocab-selftest] FAIL —\n  " + "\n  ".join(failures), file=sys.stderr
        )
        raise SystemExit(1)
    print(
        f"[effort-vocab-selftest] all green ({len(CASES)} known bypasses, each still caught)"
    )


if __name__ == "__main__":
    main()
