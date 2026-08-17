#!/usr/bin/env python3
"""Compare two measurement arms — and REFUSE when they are not comparable.

    compare.py BEFORE.json AFTER.json

🔴 THIS FILE IS THE ASSERTION. The first version of this harness printed the
caps and the corpus sizes into each arm's JSON and called that "the two arms
are the same" — but nothing ever READ the two files together, so nothing could
ever disagree. An independent reviewer built the counterexample: raise
``doc.cap_chars.duty`` to 2000 on the AFTER arm only (which doubles the corpus
that is sized off it), and the run still exited 0, still printed no warning,
and still produced a flattering before/after story out of two arms that were
measuring different worlds. A claim nothing can falsify is not evidence, and
the harness said in a COMMENT what it did not do in CODE.

So: every field that decides how big a response can be is compared here, and a
mismatch is a non-zero exit, never a warning. The comparison is deliberately
whole-dict equality rather than a hand-listed set of keys — a NEW cap setting
must break this loudly and be dealt with, instead of being silently outside a
list somebody forgot to extend.
"""
import json
import sys

ENDPOINTS = ["list_document_history", "list_task_manuals", "list_roles"]


def load(path):
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


def main(argv):
    if len(argv) != 3:
        print("usage: compare.py BEFORE.json AFTER.json", file=sys.stderr)
        return 64
    before, after = load(argv[1]), load(argv[2])

    problems = []

    # The two things that decide how big a response CAN be. If either differs,
    # the two numbers are measuring different worlds and no delta between them
    # means anything — which is why this is a refusal and not a footnote.
    for field in ("_caps", "_corpus_chars"):
        b, a = before.get(field), after.get(field)
        if b is None or a is None:
            problems.append("%s: missing from %s arm"
                            % (field, "before" if b is None else "after"))
            continue
        for key in sorted(set(b) | set(a)):
            if b.get(key) != a.get(key):
                problems.append("%s.%s differs: before=%r after=%r"
                                % (field, key, b.get(key), a.get(key)))

    for name in ENDPOINTS:
        for label, arm in (("before", before), ("after", after)):
            if name not in arm:
                problems.append("%s: not measured on the %s arm" % (name, label))

    if problems:
        print("REFUSING to compare — the two arms are not the same experiment:",
              file=sys.stderr)
        for p in problems:
            print("  " + p, file=sys.stderr)
        print("\nA delta between arms that differ in caps or corpus is not a\n"
              "measurement of this change; it is a measurement of the difference\n"
              "between the two setups. Re-run both arms from the same harness.",
              file=sys.stderr)
        return 1

    print("arms are comparable — caps and corpus identical on both")
    print("  caps  : %s" % json.dumps(before["_caps"], sort_keys=True))
    print("  corpus: %s" % json.dumps(before["_corpus_chars"], sort_keys=True))
    print()
    print("%-24s %10s %10s %10s %9s" % ("endpoint", "before", "after", "delta", "of before"))
    for name in ENDPOINTS:
        b, a = before[name], after[name]
        bc, ac = b["chars"], a["chars"]
        # Spread across the REPEATED READS of one arm, reported rather than
        # smoothed away. ⚠️ It is normally 0, and that does NOT mean the figure
        # is exact to the character: re-reading an unchanged corpus is stable,
        # but a FRESH RUN seeds fresh `updated_ts` floats whose shortest
        # round-trip repr can be a digit longer, so the same measurement taken
        # twice has been seen to differ by ~1 (719 vs 720 for
        # list_task_manuals). This instrument does not see that — it measures
        # read-to-read, not run-to-run — so treat the last digit as noise.
        jit = ""
        for arm in (b, a):
            if arm.get("chars_max", arm["chars"]) != arm.get("chars_min", arm["chars"]):
                jit = "  (±%d over %d reads)" % (
                    max(b.get("chars_max", bc) - b.get("chars_min", bc),
                        a.get("chars_max", ac) - a.get("chars_min", ac)),
                    b.get("reads", 1))
        print("%-24s %10d %10d %10d %8.1f%%%s"
              % (name, bc, ac, ac - bc, 100.0 * ac / bc, jit))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
