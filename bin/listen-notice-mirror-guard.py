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

WHAT THIS GUARD DELIBERATELY DOES NOT COVER — say it out loud rather than let a
green imply it:

  * IT COMPARES SPELLINGS, NOT BEHAVIOUR. Both sides agreeing on
    "[ocagent] listen: connected" does not prove the listener prints it or the
    sidecar acts on it. Each module's own tests own that half.
  * ONLY THESE TWO MODULES. If server/ocserverd or cli/officraft ever grows a
    third copy of one of these strings, this check will not see it, and its
    green says nothing about it.
  * A CONSISTENT RENAME IS ALLOWED, ON PURPOSE. Change both sides to the same
    new value and this passes — that is the contract holding, not drifting.
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

# A Go interpreted string literal, with the escapes these six values can
# actually contain. A value using anything else is reported rather than guessed
# at — a guard that silently mis-decodes one side compares the wrong bytes.
ESCAPES = {"n": "\n", "t": "\t", "r": "\r", '"': '"', "\\": "\\"}


def unquote(raw: str) -> str:
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


def read_consts(root: Path) -> Tuple[Dict[Tuple[str, str], str], List[str]]:
    """Locate every WANTED constant by name in its own file."""
    values: Dict[Tuple[str, str], str] = {}
    problems: List[str] = []
    cache: Dict[str, List[str]] = {}
    for rel, name in WANTED:
        if rel not in cache:
            try:
                cache[rel] = (root / rel).read_text(encoding="utf-8").split("\n")
            except OSError as exc:
                cache[rel] = []
                problems.append(f"{rel}: cannot be read ({exc.strerror})")
        # Both spellings Go allows: a line inside a `const (…)` block, and a
        # standalone `const name = "…"`. Two of these six are declared each way.
        pattern = re.compile(
            r'^\s*(?:const\s+)?' + re.escape(name) + r'\s*=\s*"((?:[^"\\]|\\.)*)"\s*$'
        )
        hits = [(n, m.group(1)) for n, m in
                ((n, pattern.match(line)) for n, line in enumerate(cache[rel], 1)) if m]
        if not hits:
            problems.append(
                f"{rel}: no declaration `{name} = \"…\"`. It was renamed, deleted or "
                "rewritten as an expression — either way this check has stopped "
                "comparing that pair, which is the state it exists to prevent."
            )
            continue
        if len(hits) > 1:
            lines = ", ".join(str(n) for n, _ in hits)
            problems.append(f"{rel}: `{name}` is declared more than once (lines {lines})")
            continue
        try:
            values[(rel, name)] = unquote(hits[0][1])
        except ValueError as exc:
            problems.append(f"{rel}:{hits[0][0]} `{name}`: {exc}")
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
    rows = problems + compare(values)
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
        "3 files in 2 modules, 6 pairs compared)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(run(Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else ROOT))
