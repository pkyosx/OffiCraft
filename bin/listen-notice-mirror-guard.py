#!/usr/bin/env python3
"""listen-notice-mirror-guard — the listener's wire to the codex sidecar is
spelled twice, in two Go modules that cannot import each other. This is the only
thing that reads both spellings.

WHAT IS ACTUALLY TWO COPIES (T-265). `cli/ocagent` prints the transport notices
and reads the ack switch out of its environment; `cli/ocwarden` matches those
notices with HasPrefix from column 0 and is the process that sets the switch.
Neither module is importable from the other — four separate go.mod files, no
go.work, no replace — so the contract between them is physically two constants
that happen to agree.

WHY A MIRROR CHECK AND NOT ONE SHARED CONSTANT. Sharing would mean a fifth
module plus a workspace file, i.e. changing how all four binaries are built, for
six strings. The tree already answers this question the same way twice — the
namespace derivation and the http/https rule are both hand-copied across modules
and both guarded by a mirror check rather than merged (bin/tests/
namespace-mirror-guard.sh, bin/tests/base-scheme-mirror-guard.sh). This is the
third of that family.

WHAT DRIFT COSTS, WHICH IS WHY THE CHECK IS WORTH ITS OWN LANE ENTRY. Every
failure in this pair is silent and the ack one is not recoverable:

  * a notice head that no longer matches ⇒ the sidecar stops recognising
    transport lines. Independent review once moved one head rightward
    (`listen: disconnected` → `net listen: disconnected`); both suites stayed
    green while every codex member went silent about its transport for the rest
    of its session.
  * the ack switch that no longer matches ⇒ the listener never turns the gate
    on, falls back to "printed means delivered", and marks read every message
    that never reached the model. Nothing errors and nothing can be replayed.
    Until T-265 that name was not spelled out in the sidecar's own tests either,
    so mistyping it left BOTH modules green — measured, by renaming it.

There WAS a check: listen_notice_contract_test.go read the sidecar's file and
required these literals to still be there. T-125 rewrote the Go test surface and
deleted it, and nothing replaced it. This is the replacement, placed outside
both modules so no rewrite of either test surface can take it with it.

HOW IT READS THEM. Each constant is located by NAME in its own file and its Go
string literal is decoded. A name that is missing, declared twice, or not a
plain string literal is a FAILURE, never a skip: the whole point is that a
rename cannot make this check quietly stop comparing anything.

WHAT THIS DOES NOT COVER — DERIVED, NOT REMEMBERED.

The first version of this section was a list of gaps the author could think of,
and review went straight through it: a FIFTH notice pair, misspelled on the
consumer side, passed with everything green, because the pair list was closed
and nothing said so. A remembered list is another act of imagination. So read
the gaps off the pipeline instead:

    Every transformation the input goes through discards a class of thing, and
    the class it discards is what this check cannot see.

  1. READ THREE NAMED FILES ⇒ discards every other file. A third copy of one of
     these strings in server/ocserverd or cli/officraft is invisible, and the
     green says nothing about it.
  2. LOCATE CONSTANTS BY NAME from a fixed pair list ⇒ used to discard any
     constant not on the list. That was F1, and `unpaired_notices` closes it:
     a `notice*` string constant in these files that no pair compares is now a
     failure, and a deliberately one-sided one has to be written into
     `NOT_PAIRED` with a reason. An explicit type between the name and the `=`
     used to slip past this and is now read, because it did not fail the way a
     missed shape usually does — it was a green over a real unpaired constant.
     What is still discarded: a contract constant that is not spelled `notice…`
     and is not in the pair list. That spelling is a CONVENTION, not a rule the
     compiler keeps — `cli/ocagent/listen.go` declares `unreadableAnswerNotice`
     with the word at the END — so a one-sided notice named that way is
     invisible here and only the pair list catches it.
  3. REQUIRE A PLAIN STRING LITERAL ⇒ discards nothing silently; a constant
     built by concatenation, renamed or deleted is REPORTED, not skipped. That
     is the whole reason this check can be trusted to still be reading.
  4. COMPARE THE DECODED VALUES ⇒ discards behaviour. Both sides agreeing on
     "[ocagent] listen: connected" does not prove the listener prints it or the
     sidecar acts on it. Each module's own tests own that half.
  5. COMPARE THE TWO SIDES TO EACH OTHER ⇒ discards whether either side is
     RIGHT. 🔴 This catches the two walking apart, never the two being wrong
     together: a consistent rename is green on purpose, and that green means
     "these agree", not "this spelling is correct". The defect that started
     T-265 was in the family this can never see — the ack switch was pinned to
     a literal on NEITHER side, and the only thing that catches a name nobody
     else expects is a literal in one side's own tests.

If you add a transformation, add its discard.
"""
from __future__ import annotations

import re
import sys
from pathlib import Path
from typing import Dict, List, Tuple

ROOT = Path(__file__).resolve().parent.parent

PRODUCER_RUN = "cli/ocagent/listen_run.go"
PRODUCER_ACK = "cli/ocagent/listen.go"
CONSUMER = "cli/ocwarden/codex_session.go"

# Every constant this check reads, as (file, name). Nothing is optional: a name
# that has gone missing is reported, because "I could not find it" and "it still
# agrees" must never look the same from the outside.
WANTED: Tuple[Tuple[str, str], ...] = (
    (PRODUCER_RUN, "agentLinePrefix"),
    (PRODUCER_RUN, "noticeDisconnected"),
    (PRODUCER_RUN, "noticeConnected"),
    (PRODUCER_RUN, "noticeGivingUp"),
    (PRODUCER_RUN, "noticeBatch"),
    (PRODUCER_ACK, "listenAckEnv"),
    (CONSUMER, "noticeDisconnectedPrefix"),
    (CONSUMER, "noticeConnectedPrefix"),
    (CONSUMER, "noticeGivingUpPrefix"),
    (CONSUMER, "noticeBatchPrefix"),
    (CONSUMER, "noticeTransportHead"),
    (CONSUMER, "listenAckEnv"),
)

# The three notices the disconnect-notice policy says must reach the agent. Each
# consumer constant is the producer's line prefix followed by the producer's own
# head, with nothing in between — that "nothing" is the property the review
# incident broke.
NOTICE_PAIRS = (
    ("noticeDisconnected", "noticeDisconnectedPrefix"),
    ("noticeConnected", "noticeConnectedPrefix"),
    ("noticeGivingUp", "noticeGivingUpPrefix"),
)

# The end-of-batch marker is the one pair that is NOT a plain concatenation: the
# consumer carries a trailing space because it splits the token off the head, and
# the producer does not because it prints the token itself. Encoding the space
# here rather than trimming it keeps the check able to see the space disappear.
BATCH_TAIL = " "

# Names this check would otherwise flag as one-sided, exempted deliberately. It
# must stay a decision rather than an oversight: a one-sided notice constant
# belongs here WITH a reason, not left to fall through as if nobody noticed it.
NOT_PAIRED: Dict[str, str] = {}

ESCAPES = {"n": "\n", "t": "\t", "r": "\r", '"': '"', "\\": "\\"}


def unquote(raw: str) -> str:
    """Decode a Go INTERPRETED string literal's body (raw strings never come here)."""
    out: List[str] = []
    i = 0
    while i < len(raw):
        c = raw[i]
        if c != "\\":
            out.append(c)
            i += 1
            continue
        if i + 1 >= len(raw) or raw[i + 1] not in ESCAPES:
            raise ValueError(f"escape this guard does not decode: {raw[i:i + 2]!r}")
        out.append(ESCAPES[raw[i + 1]])
        i += 2
    return "".join(out)


# 🔴 WHY THIS IS A SCANNER AND NOT A PATTERN, AND WHY THAT MATTERS MORE THAN THE
# TWO SHAPES IT FIXES. Three rounds of review each found one more Go spelling the
# line pattern did not know about — an explicit type between the name and the
# `=`, a raw (backtick) string literal, a comment after the value. Every one was
# legal, gofmt-clean, compiling Go, and every one produced a WRONG ANSWER IN BOTH
# DIRECTIONS at once: a registered constant read as "the declaration is gone"
# (a red nobody caused, whose message named a rename that never happened), and an
# unregistered one slipped past the unpaired check entirely (a green over real
# one-sided drift). The third round is where the pattern stops being the fix: the
# defect is not the missing shapes, it is matching Go syntax with a regex over
# raw lines. So the source is TOKENISED first — comments removed, string literals
# of both kinds lifted out and decoded — and the small pattern then runs over
# what is left, where a declaration has exactly one shape.
#
# The trailing-comment case deserves naming on its own: this file's header tells
# the reader these two copies must move together, and the most natural response
# to reading that is to leave a reminder beside the constant. That very act used
# to redden the tree and send the reader looking for a rename nobody did.
#
# ⚠️ THIS IS THE SECOND GO SOURCE SCANNER IN bin/ — comment-test-ref-guard.py
# carries an equivalent one. They are NOT shared, and that is the same shape this
# whole check exists to complain about, so it is written down rather than left to
# be discovered: `bin/` has no Python module convention at all (`bin/lib/` is
# shell), so sharing them means introducing one, which is a structural change
# that belongs in its own round with its own review. It is on T-265's follow-up
# list. Until then: a fix to one scanner belongs in both.

STRING_SENTINEL = "\x00"


def _scan(src: str) -> Tuple[List[str], List[List[str]]]:
    """Tokenise Go source into per-line CODE (comments gone, literals lifted out).

    Returns (lines, values): `lines[n]` is line n+1 with every comment removed and
    every string literal replaced by a sentinel, and `values[n]` holds that line's
    decoded literals in order. A literal spanning lines (a raw string) leaves its
    sentinel on the line it STARTED on, which is the line the declaration is on.
    """
    lines: List[str] = [""]
    values: List[List[str]] = [[]]
    i, n = 0, len(src)

    def newline() -> None:
        lines.append("")
        values.append([])

    def emit(text: str) -> None:
        lines[-1] += text

    while i < n:
        c = src[i]
        if c == "\n":
            newline()
            i += 1
            continue
        if c == "`":  # raw string: no escapes, may span lines, \r is dropped
            j = src.find("`", i + 1)
            j = n if j < 0 else j
            values[-1].append(src[i + 1:j].replace("\r", ""))
            emit(STRING_SENTINEL)
            # consume the literal, keeping the line counter honest
            for ch in src[i:j + 1]:
                if ch == "\n":
                    newline()
            i = j + 1
            continue
        if c == '"':  # interpreted string: never spans a line
            j = i + 1
            while j < n and src[j] != '"':
                j += 2 if src[j] == "\\" else 1
            body = src[i + 1:j]
            try:
                values[-1].append(unquote(body))
            except ValueError:
                values[-1].append(None)  # reported by the caller, never guessed at
            emit(STRING_SENTINEL)
            i = j + 1
            continue
        if c == "'":  # rune literal: not a string, but must not open one
            j = i + 1
            while j < n and src[j] != "'":
                j += 2 if src[j] == "\\" else 1
            i = j + 1
            emit(" ")
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            j = src.find("\n", i)
            i = n if j < 0 else j
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            j = n if j < 0 else j + 2
            for ch in src[i:j]:
                if ch == "\n":
                    newline()
            i = j
            continue
        emit(c)
        i += 1
    return lines, values


# A declaration, matched against a TOKENISED line: an optional `const`, the name,
# an optional type, `=`, and one lifted-out literal. Anything else — a
# concatenation, a function call, a second value — does not match, and not
# matching is REPORTED rather than skipped.
DECL = re.compile(
    r"^\s*(?:const\s+)?([A-Za-z_]\w*)(?:\s+[A-Za-z_][\w.*\[\]]*)?\s*=\s*"
    + STRING_SENTINEL + r"\s*$"
)

# A `notice…` name given a value that STARTS with a string literal, whatever the
# rest of it looks like. Deliberately wider than DECL: a one-sided notice is
# drift however its value is spelled, and a concatenated one would otherwise be
# the next shape to walk through.
NOTICE_ASSIGN = re.compile(
    r"^\s*(?:const\s+)?(notice[A-Za-z0-9_]*)(?:\s+[A-Za-z_][\w.*\[\]]*)?\s*=\s*"
    + STRING_SENTINEL
)


def declarations(src: str):
    """Yield (line_no, name, value) for each `name [T] = <one string literal>`."""
    lines, values = _scan(src)
    for idx, line in enumerate(lines):
        m = DECL.match(line)
        if m and values[idx]:
            yield idx + 1, m.group(1), values[idx][0]


def notice_assignments(src: str):
    """Yield (line_no, name) for each `notice… = <string literal>…`, DECL or not."""
    lines, _ = _scan(src)
    for idx, line in enumerate(lines):
        m = NOTICE_ASSIGN.match(line)
        if m:
            yield idx + 1, m.group(1)


def unpaired_notices(root: Path) -> List[str]:
    """Notice constants that exist in the source but no pair here compares."""
    known = {name for _, name in WANTED}
    rows: List[str] = []
    for rel in (PRODUCER_RUN, PRODUCER_ACK, CONSUMER):
        try:
            text = (root / rel).read_text(encoding="utf-8")
        except OSError:
            continue  # already reported by read_consts
        for line_no, name in notice_assignments(text):
            if name in known or name in NOT_PAIRED:
                continue
            rows.append(
                f"{rel}:{line_no} declares {name}, which no pair in this check "
                "compares — a notice added on one side only is exactly the drift "
                "this exists to catch, and it would pass"
            )
    return rows


def read_consts(root: Path) -> Tuple[Dict[Tuple[str, str], str], List[str]]:
    """Locate every WANTED constant by name in its own file."""
    values: Dict[Tuple[str, str], str] = {}
    problems: List[str] = []
    cache: Dict[str, List[Tuple[int, str, str]]] = {}
    for rel, name in WANTED:
        if rel not in cache:
            try:
                cache[rel] = list(declarations((root / rel).read_text(encoding="utf-8")))
            except OSError as exc:
                cache[rel] = []
                problems.append(f"{rel}: cannot be read ({exc.strerror})")
        hits = [(n, v) for n, dname, v in cache[rel] if dname == name]
        if not hits:
            problems.append(
                f"{rel}: no declaration `{name} = \"…\"`. It was renamed, deleted or "
                "rewritten as something other than one string literal — either way "
                "this check has stopped comparing that pair, which is the state it "
                "exists to prevent."
            )
            continue
        if len(hits) > 1:
            lines = ", ".join(str(n) for n, _ in hits)
            problems.append(f"{rel}: `{name}` is declared more than once (lines {lines})")
            continue
        line_no, value = hits[0]
        if value is None:
            problems.append(
                f"{rel}:{line_no} `{name}`: the literal uses an escape this guard "
                "does not decode, so its value is not being compared"
            )
            continue
        values[(rel, name)] = value
    return values, problems


def compare(values: Dict[Tuple[str, str], str]) -> List[str]:
    rows: List[str] = []
    head = values.get((PRODUCER_RUN, "agentLinePrefix"))
    if head is None:
        return rows  # already reported as a missing declaration

    for produced, consumed in NOTICE_PAIRS:
        a, b = values.get((PRODUCER_RUN, produced)), values.get((CONSUMER, consumed))
        if a is None or b is None:
            continue
        if head + a != b:
            rows.append(
                f"{produced} + the line prefix is {head + a!r}, but {consumed} is {b!r}"
            )

    batch_a, batch_b = values.get((PRODUCER_RUN, "noticeBatch")), values.get((CONSUMER, "noticeBatchPrefix"))
    if batch_a is not None and batch_b is not None and head + batch_a + BATCH_TAIL != batch_b:
        rows.append(
            f"noticeBatch + the line prefix + a trailing space is "
            f"{head + batch_a + BATCH_TAIL!r}, but noticeBatchPrefix is {batch_b!r}"
        )

    # The blanket filter's head has no constant of its own on the producing side:
    # it is whatever all four notices begin with. So the property to hold is
    # containment, not equality — the filter must recognise every line the
    # producer can print, or a notice stops being transport chatter and becomes a
    # turn on the model.
    transport = values.get((CONSUMER, "noticeTransportHead"))
    if transport is not None:
        for produced, consumed in NOTICE_PAIRS + (("noticeBatch", "noticeBatchPrefix"),):
            full = values.get((CONSUMER, consumed))
            if full is not None and not full.startswith(transport):
                rows.append(
                    f"noticeTransportHead is {transport!r}, which {consumed} "
                    f"({full!r}) does not start with — the blanket filter would stop "
                    "recognising that line as transport"
                )

    ack_a, ack_b = values.get((PRODUCER_ACK, "listenAckEnv")), values.get((CONSUMER, "listenAckEnv"))
    if ack_a is not None and ack_b is not None and ack_a != ack_b:
        rows.append(
            f"the listener reads {ack_a!r} from its environment but the sidecar sets "
            f"{ack_b!r} — the ack gate never turns on and every undelivered message "
            "is marked read"
        )
    return rows


def run(root: Path) -> int:
    values, problems = read_consts(root)
    rows = problems + unpaired_notices(root) + compare(values)
    if rows:
        listing = "\n  ".join(rows)
        print(
            "FAIL — the listener and the codex sidecar no longer spell the same "
            f"contract:\n  {listing}\n\n"
            "  These are two copies in two Go modules that cannot import each other, so\n"
            "  nothing but this check compares them and each side's own tests stay green\n"
            "  while they drift. Fix the pair, do not delete the constant: a declaration\n"
            "  that disappears is reported here for the same reason.",
            file=sys.stderr,
        )
        return 1
    print(
        f"[listen-notice-mirror-guard] all green ({len(WANTED)} constants read across "
        "3 files in 2 modules, 6 pairs compared, no unpaired notice constant)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(run(Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else ROOT))
