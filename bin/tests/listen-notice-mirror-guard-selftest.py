#!/usr/bin/env python3
"""Positive control for bin/listen-notice-mirror-guard.py.

The guard's whole value is that it goes red when one side of the listener↔codex
contract moves. A scanner nobody mutates prints all green whether or not it can
still see anything, and this pair is exactly where that happened before: there
WAS a contract test, T-125 deleted it, and every suite stayed green.

So this plants, one at a time, the drifts that actually occurred or are one
keystroke away, and requires a red for each — plus an unmutated control that
must stay green, because a scanner that reddens on everything is no better.

The tree is copied to a temp directory per case and the guard is pointed at the
copy: nothing here writes in the working tree, so a failing case cannot leave a
mutated file behind.
"""
from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import List, Tuple

ROOT = Path(__file__).resolve().parent.parent.parent
GUARD = ROOT / "bin" / "listen-notice-mirror-guard.py"

RUN = "cli/ocagent/listen_run.go"
ACK = "cli/ocagent/listen.go"
SIDECAR = "cli/ocwarden/codex_session.go"

# (name, file, the exact text to replace, what to replace it with).
# Each is a ONE-SIDED change: the point is that one side moving must redden, and
# only a matching change on BOTH sides is allowed through.
MUTANTS: Tuple[Tuple[str, str, str, str], ...] = (
    (
        # The drift that really happened: independent review moved this head
        # rightward and both module suites stayed green.
        "the producer's disconnect head moves rightward",
        RUN, '\tnoticeDisconnected = "listen: disconnected"',
        '\tnoticeDisconnected = "net listen: disconnected"',
    ),
    (
        "the producer's connect head moves",
        RUN, '\tnoticeConnected    = "listen: connected"',
        '\tnoticeConnected    = "listen: online"',
    ),
    (
        "the producer's give-up head moves",
        RUN, '\tnoticeGivingUp     = "listen: giving up"',
        '\tnoticeGivingUp     = "listen: gave up"',
    ),
    (
        "the line prefix every notice is built from moves",
        RUN, 'const agentLinePrefix = "[ocagent] "',
        'const agentLinePrefix = "[oc] "',
    ),
    (
        "the end-of-batch marker loses the space the sidecar splits on",
        SIDECAR, '\tnoticeBatchPrefix = "[ocagent] listen: batch "',
        '\tnoticeBatchPrefix = "[ocagent] listen: batch"',
    ),
    (
        "the blanket filter's head stops covering the notices it must swallow",
        SIDECAR, '\tnoticeTransportHead = "[ocagent] listen:"',
        '\tnoticeTransportHead = "[ocagent] transport:"',
    ),
    (
        # The one that used to leave BOTH modules green, and the only failure
        # here that loses messages for good.
        "the sidecar renames the ack switch",
        SIDECAR, '\tlistenAckEnv = "OC_LISTEN_ACK"',
        '\tlistenAckEnv = "OC_LISTEN_ACKX"',
    ),
    (
        "the listener renames the ack switch",
        ACK, 'const listenAckEnv = "OC_LISTEN_ACK"',
        'const listenAckEnv = "OC_LISTEN_ACK2"',
    ),
    (
        # 🔴 The unpaired-constant fix used to have a shape-shaped hole in it:
        # `unpaired_notices` only recognised `name = "…"`, so an unpaired
        # constant that carried an explicit type walked straight through the
        # very check that was added to catch unpaired constants. Measured green
        # before the fix, on a tree with real one-sided drift in it.
        "an unpaired notice constant that carries an explicit type",
        RUN, '\tnoticeBatch = "listen: batch"',
        '\tnoticeBatch = "listen: batch"\n\tnoticeResuming string = "listen: resuming"',
    ),
    (
        # The same hole from the other side: with the type unread, this read as
        # "the declaration is gone" rather than as the value it plainly is. The
        # case earns its place by drifting the VALUE too, so a guard that passed
        # it merely by ignoring typed declarations would not survive here.
        "a registered constant gains a type and its value moves",
        RUN, '\tnoticeConnected    = "listen: connected"',
        '\tnoticeConnected    string = "listen: online"',
    ),
    (
        # 🔴 SAME FAMILY, THIRD ROUND. A one-sided constant written with a RAW
        # (backtick) string used to pass with everything green — legal,
        # gofmt-clean, compiling Go, and invisible to the only check that looks
        # for one-sided notices.
        "an unpaired notice constant written as a raw string",
        RUN, '\tnoticeBatch = "listen: batch"',
        '\tnoticeBatch = "listen: batch"\n\tnoticeResumed = `listen: resumed`',
    ),
    (
        # The next spelling in the same family, pinned before it is found the
        # hard way: a one-sided notice is drift however its value is built.
        "an unpaired notice constant built by concatenation",
        RUN, '\tnoticeBatch = "listen: batch"',
        '\tnoticeBatch = "listen: batch"\n\tnoticeResumed = "listen: " + "resumed"',
    ),
    (
        # The trailing-comment shape from the other direction. It drifts the
        # VALUE too, so a guard that passed it merely by ignoring commented
        # lines would not survive here.
        "a registered constant gains a trailing comment and its value moves",
        RUN, '\tnoticeConnected    = "listen: connected"',
        '\tnoticeConnected    = "listen: online" // keep in step with the sidecar',
    ),
    (
        "a registered constant becomes a raw string and its value moves",
        RUN, '\tnoticeConnected    = "listen: connected"',
        '\tnoticeConnected    = `listen: online`',
    ),
    (
        # A rename is not a drift the guard may shrug at: it means the guard has
        # stopped comparing that pair, which is indistinguishable from agreement
        # unless it is reported.
        "a constant is renamed away, so there is nothing left to compare",
        SIDECAR, '\tnoticeGivingUpPrefix     = "[ocagent] listen: giving up"',
        '\tnoticeGivingUpRoot       = "[ocagent] listen: giving up"',
    ),
)

# 🔴 THE FIFTH-PAIR MUTANT — a defect review found, so it is pinned here rather
# than trusted to stay fixed. The pair list used to be CLOSED: it compared the
# twelve constants it knew about and never asked whether the files had grown a
# thirteenth. Adding a notice is an ordinary change with nothing to suggest the
# guard must be edited too, so a fifth pair misspelled on the consumer side
# passed with the guard green, the selftest green and both modules compiling.
# That is a WORSE failure than the incident this check was built for, because
# it needs no mistake beyond forgetting a file you had no reason to open.
# `unpaired_notices` is what closes it; this case is what keeps it closed.
FIFTH_PAIR = (
    (RUN, '\tnoticeBatch = "listen: batch"',
     '\tnoticeBatch = "listen: batch"\n\tnoticeResuming = "listen: resuming"'),
    (SIDECAR, '\tnoticeTransportHead = "[ocagent] listen:"',
     '\tnoticeResumingPrefix = "[ocagent] listen: resumeing"\n'
     '\tnoticeTransportHead = "[ocagent] listen:"'),
)

# The control that must stay green: both sides changed to the same new value is
# the contract HOLDING, not drifting, and a guard that reddens on it would push
# people to stop touching these names at all.
CONSISTENT = (
    (RUN, '\tnoticeGivingUp     = "listen: giving up"',
     '\tnoticeGivingUp     = "listen: stopped retrying"'),
    (SIDECAR, '\tnoticeGivingUpPrefix     = "[ocagent] listen: giving up"',
     '\tnoticeGivingUpPrefix     = "[ocagent] listen: stopped retrying"'),
)


# 🔴 THE RESPELLING CONTROLS — every one of these changes HOW the value is
# written and not WHAT it is, so every one must stay GREEN. Each was RED before
# the scanner replaced the line pattern: a red nobody caused, on a tree where
# nothing had moved, carrying a message that named a rename that never happened.
# The trailing-comment one is the worst of the three, because this guard's own
# header tells the reader these two copies must move together and the natural
# response is to leave exactly that reminder beside the constant.
#
# They are paired with the MUTANTS above on purpose: those drift the value under
# the same spelling. Neither set can be satisfied by ignoring a spelling — one
# demands the value still be read, the other demands it not be misread.
RESPELLINGS = (
    ("an explicit type",
     RUN, '\tnoticeConnected    = "listen: connected"',
     '\tnoticeConnected    string = "listen: connected"'),
    ("a trailing comment",
     RUN, '\tnoticeConnected    = "listen: connected"',
     '\tnoticeConnected    = "listen: connected" // keep in step with the sidecar'),
    ("a raw string literal",
     RUN, '\tnoticeConnected    = "listen: connected"',
     '\tnoticeConnected    = `listen: connected`'),
)


def stage() -> Path:
    tmp = Path(tempfile.mkdtemp(prefix="listen-notice-mirror-selftest-"))
    for rel in (RUN, ACK, SIDECAR):
        dst = tmp / rel
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / rel, dst)
    return tmp


def patch(tree: Path, rel: str, old: str, new: str) -> None:
    path = tree / rel
    text = path.read_text(encoding="utf-8")
    if text.count(old) != 1:
        raise SystemExit(
            f"FAIL — selftest is stale: {rel} contains {text.count(old)} copies of\n"
            f"  {old!r}\n"
            "  The source moved under this control. Update the case rather than "
            "dropping it: an unplanted mutant is a control that silently stopped "
            "controlling anything."
        )
    path.write_text(text.replace(old, new), encoding="utf-8")


def verdict(tree: Path) -> int:
    return subprocess.run(
        [sys.executable, str(GUARD), str(tree)],
        capture_output=True, text=True,
    ).returncode


def main() -> int:
    failures: List[str] = []

    clean = stage()
    if verdict(clean) != 0:
        failures.append("the unmutated tree is RED — the guard reddens on a good tree")
    shutil.rmtree(clean)

    for name, rel, old, new in MUTANTS:
        tree = stage()
        patch(tree, rel, old, new)
        if verdict(tree) == 0:
            failures.append(f"survived: {name} ({rel})")
        shutil.rmtree(tree)

    tree = stage()
    for rel, old, new in FIFTH_PAIR:
        patch(tree, rel, old, new)
    if verdict(tree) == 0:
        failures.append(
            "a FIFTH notice pair, misspelled on the consumer side, passed — the "
            "pair list has gone closed again and a newly added notice is invisible"
        )
    shutil.rmtree(tree)

    for label, rel, old, new in RESPELLINGS:
        tree = stage()
        patch(tree, rel, old, new)
        if verdict(tree) != 0:
            failures.append(
                f"giving a constant {label}, value unchanged, was reported as drift "
                "— the guard reddens on a respelling that moved nothing"
            )
        shutil.rmtree(tree)

    tree = stage()
    for rel, old, new in CONSISTENT:
        patch(tree, rel, old, new)
    if verdict(tree) != 0:
        failures.append(
            "a matching rename on BOTH sides was reported as drift — the guard "
            "forbids renaming instead of forbidding disagreement"
        )
    shutil.rmtree(tree)

    if failures:
        rows = "\n  ".join(failures)
        print(
            f"FAIL — listen-notice-mirror-guard's positive control found "
            f"{len(failures)} problem(s):\n  {rows}\n\n"
            "  A surviving mutant means that pair can drift with every check green,\n"
            "  which is the state this guard exists to end. Fix the guard, not this\n"
            "  control.",
            file=sys.stderr,
        )
        return 1
    print(
        f"[listen-notice-mirror-guard-selftest] all green ({len(MUTANTS)} mutants "
        "killed, 1 fifth-pair mutant killed, 1 clean control, "
        f"{len(RESPELLINGS)} respelling controls, 1 consistent-rename control)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
