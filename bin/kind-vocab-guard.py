#!/usr/bin/env python3
"""kind-vocab-guard — every hand-written copy of the TASK EXECUTOR KIND
vocabulary must speak the current set. The old value `member` must be gone
from every one of them, or the rename is not finished.

THE INCIDENT THIS GUARDS AGAINST. `executor_kind` (and its handover twin
`reassigned_from_kind`) is a closed two-value vocabulary that is written down
BY HAND in dozens of places: the Go constant block, the SQL CHECK constraints
in three migrations, Go doc comments on the wire structs, generated ocapi doc
strings, several `spec/openapi.json` descriptions, the frontend adapter type
and its prose, every mapper, and something like a hundred test / story
fixtures. Renaming `member` to `staff` means editing all of them. Miss one and
NOTHING GOES RED: a fixture that still says `"member"` type-checks (the field
is `string`, not a union), an SQL CHECK that still lists `'member'` only
rejects rows nobody writes any more, and a description in openapi.json is read
by agents, not by a compiler. The failure mode is a half-renamed vocabulary
that looks green.

THE DESIGN THAT WOULD HAVE FAILED SILENTLY. The obvious guard is a list of the
places to check. That list is a forward-only allowlist: the day someone adds
another fixture the guard keeps printing all green over the old count, and a
list that is missing an entry does not go red, it just covers less. So the
copies are DISCOVERED from the tree on every run, BY SHAPE, and the
enumeration counts are printed on stdout so a narrowed scan shows up as a
SMALLER NUMBER rather than as silence.

THE SHAPE, STATED PRECISELY. A "copy" is a line that writes an executor-kind
VALUE down in VALUE POSITION, inside the NEIGHBOURHOOD of one of the four
field tokens:

    tokens        executor_kind | executorKind | ExecutorKind
                  reassigned_from_kind | reassignedFromKind | ReassignedFromKind
    neighbourhood the token's own line, plus BEFORE lines above and AFTER
                  lines below it (a Go doc comment sits above its field; a
                  JSON `enum` sits below its property key)
    value position quoted ("member" / 'member' / `member`), or after an
                  = / == / === / : , or used as a map key (member:918)

Prose is deliberately NOT a copy. `spec/openapi.json` says "executed by a
roster member" one clause away from ``executor_kind='outsource'``; the word
there is English, not a value, and it does not change when the vocabulary
does. Requiring VALUE POSITION is what keeps this guard from crying wolf on
every sentence that happens to contain the noun.

THE RULE, STATED AS A QUERY THAT MUST COME BACK EMPTY:

    for every copy discovered as above
    the value it writes down is in RETIRED

    ⇒ the answer must be zero rows.

THE BOUNDARY, WHICH IS AN OWNER RULING AND NOT A DETAIL. This guard covers the
EXECUTOR-KIND AXIS AND NOTHING ELSE — the two fields named above, and no other
field that happens to be called `kind`. The member roster's own `kind` column
(staff / warden / outsource), the SSE delta `kind`, and the avatar `kind` are
each somebody else's vocabulary. T-101 makes the executor axis SPELL THE SAME
WORDS as the roster axis, which is precisely why the boundary has to be
mechanical rather than lexical: after the rename the two axes are
indistinguishable by their VALUES and separable only by their FIELD NAME.

Two consequences are load-bearing:

  * the roster axis is not scanned — none of its field names is in TOKENS, so a
    roster `kind: "warden"` is invisible here and must stay invisible. Widening
    TOKENS to a bare `kind` would swallow every one of those axes at once;
  * the guard only ever reports a RETIRED value. It never reports "unknown
    value", because an unknown-value rule would redden on `warden` the moment
    a roster line drifted into an executor-kind neighbourhood. Silence about
    words this guard does not own is the point, not an omission.

THE SOURCE OF TRUTH is the running constant block — the `TaskExecutor…`
consts in the server's domain file, which is what the handlers actually
compare against. It is located BY SHAPE (`TaskExecutor<Name> = "<value>"`), not
by constant name, so the rename may rename the constant too. The block is
checked against TARGET like every other copy, and it is checked FIRST: an SSOT
that still says "member" is the loudest row in the report, because until it
moves nothing downstream can be right.

WHAT THIS GUARD DELIBERATELY DOES NOT COVER — say it out loud rather than let
a green imply it:

  * A copy MORE than AFTER lines below (or BEFORE lines above) its field
    token. A long JSON schema whose `enum` is eight lines under its property
    key is outside the window and invisible.
  * A copy with NO field token anywhere near it. `server/ocserverd/routes.go`
    is the worked example: its route-table Summaries describe the reassign
    verb as "to a member or a fresh outsource worker" without ever naming
    `executor_kind`, so this guard does not see them. Pulling them in would
    mean matching the bare English noun, which is the cry-wolf design rejected
    above. The cheapest honest fix for such a line is to NAME the field on it.
  * A value assembled at runtime (string concatenation, strings.Join): there
    is no literal to read, so there is nothing to compare.
  * A file whose suffix is not in TEXT_SUFFIXES.
  * The DATABASE. Rows already written say `member`; migrating them is a
    migration's job (00088), and a green here says nothing about stored data.
  * A legacy value BUILT FROM RUNES rather than written as a literal —
    `string([]rune{'m','e','b','e','r'})` in CanonicalTaskExecutorKind is
    deliberately spelled that way so a repo-wide replacement cannot reach it.
    There is no literal to read, so this guard cannot see it either. That is
    the intended trade and it is stated here so a green does not imply
    otherwise.

--selftest is what keeps this honest: a synthetic positive tree (a planted
retired value per copy shape) that MUST redden and be named, and a synthetic
negative tree (fully renamed, plus roster-axis and prose decoys) that MUST go
green. A scanner nobody verified is a green with a hole in it.

🔴 IF YOU ARE PLANTING A MUTANT IN THE REAL TREE TO TEST THIS GUARD, READ THIS
FIRST — both traps below were hit on the first attempt, and each produced output
IDENTICAL to a clean run.

  1. THE FILE MUST BE TRACKED BY GIT. Enumeration is `git ls-files`, so an
     untracked file is not scanned at all: the file count, the copy count and
     the exit code are byte-identical to the run without it. "The guard is green"
     and "the guard never looked" are indistinguishable here, which is the exact
     failure this guard's own design section warns about. `git add -N <file>`
     before measuring, and check that the FILE COUNT moved.
  2. THE VALUE MUST LAND INSIDE THE NEIGHBOURHOOD. A planted map whose keys sit
     more than BEFORE lines above its field token is outside the window, and it
     will be missed for that reason rather than for its shape. Diagnosing a
     window miss as a shape miss sends you to widen a regex that was already
     correct. Put the token adjacent, then vary one thing at a time.

Read the printed enumeration, not just the exit code: a mutant that landed moves
`files scanned` AND `vocabulary copies found`. If neither moved, the guard did
not see your file, and nothing it printed is evidence about your change.
"""

import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Dict, List, NoReturn, Optional, Set, Tuple

# ── the vocabulary ───────────────────────────────────────────────────────────
# The axis this guard owns, and only this axis. TARGET is what the tree must
# say after the rename; RETIRED is what it must no longer say anywhere.
TARGET = {"staff", "outsource"}
RETIRED = {"member"}
VOCAB = TARGET | RETIRED

# The four field tokens. A value literal is only a COPY of this vocabulary if
# it sits in the neighbourhood of one of these — that gate is what keeps the
# roster `kind` axis and every English sentence out.
# Each is listed in the three spellings the tree actually uses: the wire /
# SQL snake_case, the frontend camelCase, and Go's exported PascalCase. The
# Go struct field `ExecutorKind string // "member" | "outsource"` is a real
# copy, and it was invisible until PascalCase was in this list.
TOKENS = (
    "executor_kind", "executorKind", "ExecutorKind",
    "reassigned_from_kind", "reassignedFromKind", "ReassignedFromKind",
)
TOKEN_RE = re.compile("|".join(TOKENS))

# The neighbourhood, in lines. Above, because a Go doc comment sits over its
# struct field; below, because a JSON property key sits over its enum.
BEFORE = 2
AFTER = 3

TEXT_SUFFIXES = {
    ".go", ".ts", ".tsx", ".js", ".mjs", ".py", ".sh",
    ".md", ".json", ".yml", ".yaml", ".sql",
}

# Files whose vocabulary-shaped text is NOT a copy of the vocabulary. Every
# entry needs a committed reason; adding one is the review this guard forces.
SKIP_FILES = {
    "bin/kind-vocab-guard.py": "the predicate itself — it spells the vocabulary out in prose",
}

# ── the two exemptions, both DISCOVERED rather than listed ───────────────────
# 1. APPLIED MIGRATIONS. `migration.lock` freezes a content hash per shipped
#    migration and migration_lock_t75_test.go fails if one is edited, so an
#    already-locked .sql is APPLIED HISTORY, not a live copy of the vocabulary:
#    it is REQUIRED to keep saying what it said. A red row there is one nobody
#    is allowed to act on, and an alarm nobody may answer is one everybody
#    learns to ignore. The lock is READ FROM THE TREE — a migration that is not
#    yet locked is still scanned, which is exactly the one you can still fix.
MIGRATION_LOCK_NAME = "migration.lock"
MIGRATION_LOCK_ENTRY = re.compile(r"^\d+\s+(\S+\.sql)\s+sha256:", re.M)

# 2. A DELIBERATE MENTION OF THE OLD VALUE. The rename's own error path has to
#    NAME what was renamed ("'member' was renamed to 'staff'"), and so does the
#    comment above it. Such a line carries this marker; it is counted and
#    printed on every run, so waiving one is a visible edit rather than a
#    silent narrowing. It is LINE-level on purpose — SKIP_FILES is file-level
#    and would take the file's real copies out of the sweep with it.
LEGACY_WAIVER = "kind-vocab-guard:legacy"

# The source of truth, located by shape rather than by constant name so the
# rename may rename the constant too.
SSOT_HINT = "server/ocserverd/domain.go"
SSOT_CONST = re.compile(r"TaskExecutor(\w+)\s*=\s*\"([a-z_]+)\"")

# ── value position ───────────────────────────────────────────────────────────
# A vocabulary word only counts as a COPY when it is written as a VALUE:
#   "member"  'member'  `member`  ``member``   (quoted / backticked / rst)
#   = member  == member  === member  : member  kind=member
#   member:918                                  (map key / a count)
# The bare English noun in a sentence is none of these, on purpose.
_V = "(?:" + "|".join(sorted(VOCAB, key=len, reverse=True)) + ")"
VALUE_SHAPES = (
    ("quoted", re.compile(r"[\"'`](" + _V + r")[\"'`]")),
    ("assigned", re.compile(r"(?:=|==|===|:)\s*(" + _V + r")\b(?![\"'`\w-])")),
    # A table KEYED by the kind — a label map, an i18n lookup, a per-kind config
    # block. The value after the colon may be a number, a string or a nested
    # object; what it may NOT be is a bare identifier, because `member: MemberDTO`
    # is a DTO field on the ROSTER axis and naming it here would drag a second
    # vocabulary into this guard's scope.
    #
    # 🔴 THIS USED TO REQUIRE A DIGIT (`\s*\d`), which made the commonest form of
    # the shape invisible: a map from kind to a display string was scanned, its
    # kind field was counted among the token sites, and it contributed ZERO
    # copies — so a stale spelling there passed with exit 0 and a copy count that
    # did not move. Measured on a planted file rather than reasoned about.
    ("mapkey", re.compile(r"\b(" + _V + r")\s*:\s*[\"'`\d{\[]")),
)


def fail(message: str) -> NoReturn:
    print("[kind-vocab-guard] FAIL — " + message, file=sys.stderr)
    raise SystemExit(1)


class Copy:
    """One line that writes an executor-kind value down in value position."""

    def __init__(self, rel: str, line: int, values: Set[str], shapes: Set[str], excerpt: str):
        self.rel, self.line = rel, line
        self.values, self.shapes, self.excerpt = values, shapes, excerpt

    @property
    def bad(self) -> bool:
        return bool(self.values & RETIRED)

    def render(self) -> str:
        listed = ", ".join(sorted(self.values))
        return (
            f"{self.rel}:{self.line} [{'+'.join(sorted(self.shapes))}] writes {{{listed}}}\n"
            f"      {self.excerpt.strip()[:140]}"
        )


def tracked_files(root: Path) -> List[str]:
    out = subprocess.run(["git", "ls-files"], cwd=root, capture_output=True, text=True)
    if out.returncode == 0 and out.stdout.strip():
        return sorted(p for p in out.stdout.split("\n") if p)
    # A staged copy (the selftest's synthetic tree, a mutant sandbox) is not a
    # git repo. Walk it instead — same file set, same sorted order so failure
    # output is stable.
    found = []
    for path in sorted(root.rglob("*")):
        if path.is_file() and ".git" not in path.parts:
            found.append(str(path.relative_to(root)))
    return found


def normalise(rel: str, text: str) -> str:
    if rel.endswith(".json"):
        # JSON keeps its newlines as the two characters \ and n, and a nested
        # JSON blob keeps its quotes as \". Without this the value `\"member\"`
        # inside a descriptor string is not in quoted position and escapes the
        # scan — a confirmed bypass in spec/openapi.json's `legacy.descriptor`.
        text = re.sub(r"\\[nrt]", " ", text)
        text = text.replace('\\"', '"')
    return text


def ssot(root: Path, files: List[str]) -> Tuple[str, Set[str], int]:
    """The executor-kind constant block, found by shape.

    Returns (relative path, the set of values it declares, its line number).
    """
    hits: List[Tuple[str, Set[str], int]] = []
    ordered = sorted(files, key=lambda p: (p != SSOT_HINT, p))
    for rel in ordered:
        if not rel.endswith(".go") or rel.endswith("_test.go"):
            continue
        try:
            text = (root / rel).read_text(errors="replace")
        except OSError:
            continue
        found = list(SSOT_CONST.finditer(text))
        if found:
            line = text.count("\n", 0, found[0].start()) + 1
            hits.append((rel, {m.group(2) for m in found}, line))
    if not hits:
        fail(
            "no executor-kind constant block found anywhere in the tree "
            f"(shape: TaskExecutor<Name> = \"<value>\", expected in {SSOT_HINT}). "
            "Without the running gate there is nothing to anchor this vocabulary "
            "to and every comparison below would be vacuous. If the constants "
            "moved, move this guard's shape with them; do NOT delete the anchor."
        )
    return hits[0]


def frozen_migrations(root: Path, files: List[str]) -> Set[str]:
    """The migrations already locked, read out of migration.lock itself."""
    frozen: Set[str] = set()
    for rel in files:
        if Path(rel).name != MIGRATION_LOCK_NAME:
            continue
        try:
            text = (root / rel).read_text(errors="replace")
        except OSError:
            continue
        base = Path(rel).parent
        for m in MIGRATION_LOCK_ENTRY.finditer(text):
            frozen.add(str(base / m.group(1)))
    return frozen


def scan(root: Path) -> Tuple[List[Copy], Dict[str, int]]:
    counts = {
        "files_scanned": 0, "files_with_token": 0, "token_sites": 0,
        "frozen_migrations": 0, "legacy_waivers": 0,
    }
    copies: List[Copy] = []
    files = tracked_files(root)
    frozen = frozen_migrations(root, files)

    for rel in files:
        if Path(rel).suffix not in TEXT_SUFFIXES:
            continue
        counts["files_scanned"] += 1
        if rel in SKIP_FILES:
            continue
        if rel in frozen:
            counts["frozen_migrations"] += 1
            continue
        try:
            text = (root / rel).read_text(errors="replace")
        except OSError:
            continue
        if not TOKEN_RE.search(text):
            continue
        counts["files_with_token"] += 1
        text = normalise(rel, text)
        lines = text.split("\n")

        # The neighbourhood: every line within [BEFORE, AFTER] of a token line.
        near: Set[int] = set()
        for idx, line in enumerate(lines):
            if TOKEN_RE.search(line):
                counts["token_sites"] += 1
                near.update(range(max(0, idx - BEFORE), min(len(lines), idx + AFTER + 1)))

        for idx in sorted(near):
            line = lines[idx]
            if LEGACY_WAIVER in line:
                counts["legacy_waivers"] += 1
                continue
            values: Set[str] = set()
            shapes: Set[str] = set()
            for shape, pattern in VALUE_SHAPES:
                for m in pattern.finditer(line):
                    values.add(m.group(1))
                    shapes.add(shape)
            if values:
                copies.append(Copy(rel, idx + 1, values, shapes, line))

    return copies, counts


def report(root: Path, quiet: bool = False) -> int:
    files = tracked_files(root)
    ssot_rel, ssot_values, ssot_line = ssot(root, files)
    copies, counts = scan(root)

    ssot_bad = bool(ssot_values & RETIRED)
    bad = [c for c in copies if c.bad]
    ok = len(copies) - len(bad)

    if not quiet:
        print(
            f"[kind-vocab-guard] enumerated: {counts['files_scanned']} files scanned, "
            f"{counts['files_with_token']} name a kind field ({counts['token_sites']} token sites), "
            f"{len(copies)} vocabulary copies found — {ok} speak {{{', '.join(sorted(TARGET))}}}, "
            f"{len(bad)} still write a retired value {{{', '.join(sorted(RETIRED))}}}"
        )
        print(
            f"[kind-vocab-guard] exempted: {counts['frozen_migrations']} migrations "
            f"frozen by {MIGRATION_LOCK_NAME} (applied history — editing one is a "
            f"lock failure), {counts['legacy_waivers']} lines marked "
            f"{LEGACY_WAIVER} (a deliberate mention of the renamed-away value)"
        )
        print(
            f"[kind-vocab-guard] source of truth: {ssot_rel}:{ssot_line} declares "
            f"{{{', '.join(sorted(ssot_values))}}}"
            + ("  ← RETIRED" if ssot_bad else "")
        )

    if not bad and not ssot_bad:
        if not quiet:
            print(
                f"[kind-vocab-guard] all green ({len(copies)} copies of the executor-kind "
                f"vocabulary agree on {{{', '.join(sorted(TARGET))}}}; the member-roster "
                f"`kind` axis is deliberately out of scope)"
            )
        return 0

    blocks: List[str] = []
    if ssot_bad:
        blocks.append(
            f"the SOURCE OF TRUTH still speaks the retired vocabulary: {ssot_rel}:{ssot_line} "
            f"declares {{{', '.join(sorted(ssot_values))}}}, expected {{{', '.join(sorted(TARGET))}}}. "
            "Nothing downstream can be right until this block moves."
        )
    if bad:
        listing = "\n  ".join(c.render() for c in sorted(bad, key=lambda c: (c.rel, c.line)))
        blocks.append(
            f"{len(bad)} hand-written copies of the executor-kind vocabulary still write a "
            f"retired value {{{', '.join(sorted(RETIRED))}}}:\n  {listing}\n\n"
            "  Fix the copies, not this guard. Do NOT narrow the scan (BEFORE/AFTER, "
            "TEXT_SUFFIXES, VALUE_SHAPES) to make a row go away: over-matching costs one "
            "committed line in SKIP_FILES with a reason, while a narrowed scan silently "
            "covers less and nothing here goes red for it. If a line is about the MEMBER "
            "ROSTER `kind` axis (staff/warden/outsource) rather than this one, it should "
            "not be naming an executor-kind field at all — fix the wording, do not widen "
            "this guard's boundary to cover a second axis."
        )
    if not quiet:
        print("[kind-vocab-guard] FAIL — " + "\n\n  ".join(blocks), file=sys.stderr)
    return 1


# ── selftest ─────────────────────────────────────────────────────────────────
# One planted retired value per copy shape, and a negative tree carrying the
# decoys that a lazier scan would redden on.

_SSOT_STAFF = '''package ocserverd

// The executor-track closed set.
const (
\tTaskExecutorStaff     = "staff"
\tTaskExecutorOutsource = "outsource"
)
'''

_SSOT_MEMBER = _SSOT_STAFF.replace('TaskExecutorStaff     = "staff"',
                                   'TaskExecutorMember    = "member"')

# Decoys on ANOTHER AXIS. These must yield ZERO copies: nothing on them names
# an executor-kind field, so the guard must not see them at all — including the
# deliberately stale roster value in frontend/roster.ts, which belongs to the
# member-roster `kind` vocabulary and is somebody else's problem.
_DECOYS_OTHER_AXIS = {
    "server/roster.go": '''package ocserverd

// The member roster's own kind column — a DIFFERENT axis (T-0076).
const (
\tMemberKindStaff     = "staff"
\tMemberKindWarden    = "warden"
\tMemberKindOutsource = "outsource"
)

type MemberDTO struct {
\tKind string `json:"kind"` // "staff" | "warden" | "outsource"
}
''',
    "frontend/roster.ts": (
        'export type MemberKind = "staff" | "warden" | "outsource";\n'
        'export const roster = [{ id: "mira", kind: "member" }];\n'
        '// no executor-kind field on this line, so the stale roster value above\n'
        '// is NOT this guard\'s business.\n'
    ),
}

# Decoys made of PROSE. The English noun "member" sits one clause away from the
# field name; it is not a value and must not redden. (The `'outsource'` written
# as a value on the same line IS a copy, and counting it is correct.)
_DECOYS_PROSE = {
    "spec/prose.json": (
        '{\n  "TaskDTO": {\n'
        '    "description": "A task executed by a roster member or an anonymous '
        'outsource worker. ``executor_kind=\'outsource\'`` with an empty '
        '``executor_id`` is the transient unassigned state.",\n'
        '    "executor_kind": { "type": "string" }\n  }\n}\n'
    ),
}

_DECOYS = {**_DECOYS_OTHER_AXIS, **_DECOYS_PROSE}

_CLEAN = {
    "server/ocserverd/domain.go": _SSOT_STAFF,
    "server/ocserverd/dal_tasks.go":
        'type taskRow struct {\n'
        '\tExecutorKind string // "staff" | "outsource"\n'
        '}\n',
    "server/ocserverd/migrations/00004_tasks.sql":
        "    executor_kind  TEXT NOT NULL CHECK (executor_kind IN ('staff', 'outsource'))\n",
    "server/ocserverd/ocapi_gen.go":
        '// ReassignedFromKind The kind of ``reassigned_from`` (``staff`` | ``outsource``).\n'
        '\tReassignedFromKind *string `json:"reassigned_from_kind,omitempty"`\n',
    "spec/openapi.json":
        '{\n  "executor_kind": {\n    "enum": ["staff", "outsource"],\n'
        '    "type": "string"\n  }\n}\n',
    "frontend/src/api/wire.ts":
        'export interface TaskWire {\n  executor_kind: string;\n}\n'
        'export const KINDS = ["staff", "outsource"];\n',
    "frontend/src/components/TaskCard.tsx":
        'const isStaff = task.executorKind === "staff";\n'
        'const wasOutsourced = task.reassignedFromKind === "outsource";\n',
    "frontend/fixtures.ts":
        'export const t = { id: "T-1", executorKind: "staff", executorId: "mira" };\n',
    "docs/notes.md":
        "The task's `executor_kind` is `staff` or `outsource`.\n",
}

# Each mutant is a DIFFERENT copy shape, so a scan that loses one shape loses
# exactly one row here rather than going quietly green.
_MUTANTS = {
    "server/ocserverd/domain.go": ("ssot-const", _SSOT_MEMBER),
    "server/ocserverd/dal_tasks.go": ("go-doc-comment",
        'type taskRow struct {\n'
        '\tExecutorKind string // "member" | "outsource"\n'
        '}\n'),
    "server/ocserverd/migrations/00004_tasks.sql": ("sql-check",
        "    executor_kind  TEXT NOT NULL CHECK (executor_kind IN ('member', 'outsource'))\n"),
    "server/ocserverd/ocapi_gen.go": ("rst-backtick-above-field",
        '// ReassignedFromKind The kind of ``reassigned_from`` (``member`` | ``outsource``).\n'
        '\tReassignedFromKind *string `json:"reassigned_from_kind,omitempty"`\n'),
    "spec/openapi.json": ("json-enum-below-key",
        '{\n  "executor_kind": {\n    "enum": ["member", "outsource"],\n'
        '    "type": "string"\n  }\n}\n'),
    "frontend/src/api/wire.ts": ("ts-array-literal",
        'export interface TaskWire {\n  executor_kind: string;\n}\n'
        'export const KINDS = ["member", "outsource"];\n'),
    "frontend/src/components/TaskCard.tsx": ("ts-equality",
        'const isStaff = task.executorKind === "member";\n'
        'const wasOutsourced = task.reassignedFromKind === "outsource";\n'),
    "frontend/fixtures.ts": ("fixture",
        'export const t = { id: "T-1", executorKind: "member", executorId: "mira" };\n'),
    # A table KEYED by the kind whose values are STRINGS — a label map, an i18n
    # lookup. The mapkey shape used to require a DIGIT after the colon, so this
    # whole family was invisible; the file was scanned and its kind field
    # counted, and it contributed zero copies.
    # 🔴 The token is placed ON THE LINE ABOVE the keys ON PURPOSE. The keys must
    # fall inside the BEFORE/AFTER neighbourhood or this control tests the window
    # rather than the shape — which is how the hole was mis-diagnosed the first
    # time it was measured.
    "frontend/src/i18n/kindLabels.ts": ("mapkey-string-value",
        'export function labelOfExecutorKind(executorKind: string): string {\n'
        '  const LABELS: Record<string, string> = {\n'
        '    member: "\u6b63\u8077",\n    outsource: "\u5916\u5305",\n  };\n'
        '  return LABELS[executorKind] ?? "";\n}\n'),
    "docs/notes.md": ("markdown-backtick",
        "The task's `executor_kind` is `member` or `outsource`.\n"),
}


def _materialise(base: Path, files: Dict[str, str]) -> Path:
    for rel, body in files.items():
        path = base / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(body)
    return base


def selftest() -> int:
    failures: List[str] = []
    tmp = Path(tempfile.mkdtemp(prefix="kind-vocab-guard-selftest-"))
    try:
        # ── negative: a fully renamed tree plus every decoy ───────────────────
        clean = _materialise(tmp / "clean", {**_CLEAN, **_DECOYS})
        rc = report(clean, quiet=True)
        copies, _ = scan(clean)
        if rc != 0:
            failures.append(
                "NEGATIVE control reddened: a fully renamed tree (with the member-roster "
                "axis and English prose as decoys) must exit 0, got " + str(rc)
            )
        print(f"  [negative] renamed tree + decoys → exit {rc}, {len(copies)} copies, all clean")
        # The decoys must not merely pass — they must not be COUNTED as copies
        # of this vocabulary at all, or the boundary has moved.
        strays = sorted({c.rel for c in copies} & set(_DECOYS_OTHER_AXIS))
        if strays:
            failures.append(
                "NEGATIVE control counted the member-roster axis as copies of this "
                "vocabulary: " + ", ".join(strays)
            )
        prose_values = {v for c in copies if c.rel in _DECOYS_PROSE for v in c.values}
        if prose_values - TARGET:
            failures.append(
                "NEGATIVE control read the English noun as a value in prose: "
                + ", ".join(sorted(prose_values - TARGET))
            )
        print(f"  [negative] roster axis contributed {len(strays)} copies (must be 0); "
              f"prose contributed only {{{', '.join(sorted(prose_values)) or '-'}}}")

        # ── positive: one planted retired value per copy shape ────────────────
        for rel, (shape, body) in sorted(_MUTANTS.items()):
            case = _materialise(
                tmp / ("mutant-" + shape),
                {**_CLEAN, **_DECOYS, rel: body},
            )
            rc = report(case, quiet=True)
            copies, _ = scan(case)
            named = [c for c in copies if c.rel == rel and c.bad]
            ssot_hit = rel == "server/ocserverd/domain.go"
            caught = rc != 0
            reported = bool(named) or ssot_hit
            print(
                f"  [positive] {shape:<26} {rel:<48} → exit {rc}, "
                f"{'named' if reported else 'NOT NAMED'}"
            )
            if not caught:
                failures.append(f"mutant '{shape}' in {rel} was NOT caught (exit 0)")
            elif not reported:
                failures.append(
                    f"mutant '{shape}' in {rel} reddened the run but the row was not "
                    "attributed to that file — a red nobody can act on"
                )
        # ── the two exemptions, in BOTH directions ───────────────────────────
        # An exemption that is never exercised is an untested branch, and one
        # that over-fires is a hole. Each is checked to fire where it should
        # and NOT to fire one step outside.
        exempt_cases = [
            (
                "frozen-migration-exempt",
                {
                    "server/ocserverd/migration.lock":
                        "roll sha256:deadbeef\n"
                        "00004 migrations/00004_tasks.sql sha256:abc123\n",
                    "server/ocserverd/migrations/00004_tasks.sql":
                        "executor_kind TEXT CHECK (executor_kind IN ('member','outsource'))\n",
                },
                0,
            ),
            (
                "unlocked-migration-still-scanned",
                {
                    "server/ocserverd/migration.lock": "roll sha256:deadbeef\n",
                    "server/ocserverd/migrations/00099_new.sql":
                        "executor_kind TEXT CHECK (executor_kind IN ('member','outsource'))\n",
                },
                1,
            ),
            (
                "waived-line-skipped-neighbour-still-scanned",
                {
                    "server/ocserverd/api_tasks.go":
                        '// executor_kind: the caller sending the pre-rename \'member\' is\n'
                        '// told it was RENAMED, not merely rejected.\n',
                    "server/ocserverd/api_manuals.go":
                        '// executor_kind: pre-rename \'member\' is REJECTED with a message\n'
                        '// naming the rename.  kind-vocab-guard:legacy\n',
                },
                1,
            ),
            (
                "legacy-waiver-exempt",
                {
                    "server/ocserverd/api_tasks.go":
                        '// executor_kind: pre-rename \'member\' is REJECTED. kind-vocab-guard:legacy\n',
                },
                0,
            ),
        ]
        for name, extra, want in exempt_cases:
            case = _materialise(tmp / ("exempt-" + name), {**_CLEAN, **_DECOYS, **extra})
            rc = report(case, quiet=True)
            print(f"  [exemption] {name:<34} → exit {rc} (want {want})")
            if rc != want:
                failures.append(
                    f"exemption control '{name}' exited {rc}, expected {want}"
                )
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

    total = 1 + len(_MUTANTS) + 4
    if failures:
        print(
            "[kind-vocab-guard] SELFTEST FAIL — "
            + "\n  ".join([f"{len(failures)}/{total} controls failed:"] + failures),
            file=sys.stderr,
        )
        return 1
    print(
        f"[kind-vocab-guard] selftest all green ({total} controls: 1 negative "
        f"(clean tree + {len(_DECOYS)} out-of-scope decoys must stay silent), "
        f"{len(_MUTANTS)} positive (one planted retired value per copy shape, "
        f"each must redden AND be named), 4 exemption (each of the two "
        f"exemptions must fire where it should and NOT one step outside))"
    )
    return 0


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Guard the task executor-kind vocabulary (executor_kind / "
                    "reassigned_from_kind) against half-finished renames.",
    )
    parser.add_argument(
        "--root",
        default=os.environ.get("OC_KIND_GUARD_ROOT"),
        help="tree to scan (default: the repo this script lives in, or $OC_KIND_GUARD_ROOT)",
    )
    parser.add_argument(
        "--selftest", action="store_true",
        help="run the planted-mutant controls against synthetic trees and exit",
    )
    parser.add_argument(
        "--list", action="store_true",
        help="print every discovered copy, compliant ones included, then the verdict",
    )
    args = parser.parse_args()

    if args.selftest:
        raise SystemExit(selftest())

    root = Path(args.root).resolve() if args.root else Path(__file__).resolve().parents[1]
    if not root.is_dir():
        fail(f"root {root} is not a directory")

    if args.list:
        copies, _ = scan(root)
        for c in sorted(copies, key=lambda c: (c.rel, c.line)):
            print(("RETIRED " if c.bad else "ok      ") + c.render().split("\n")[0])

    raise SystemExit(report(root))


if __name__ == "__main__":
    main()
