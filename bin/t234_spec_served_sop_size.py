#!/usr/bin/env python3
"""T-234 follow-up — the task manual listing's SOP size is measured on the
document the station serves, which for an unedited built-in manual is its
shipped SOP rather than a stored row.

Edits spec/openapi.json as TEXT, like bin/t234_spec_builtin_manuals.py, with
the expected number of hits asserted. Refuses to run twice.

🔴 THE WORDING BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH. Once this
script has run, spec/openapi.json is authoritative.
"""

import json
import sys

SPEC = "spec/openapi.json"

EDITS = [
    (
        "Its size is measured on the STORED\\nrow, so the row still answers",
        "Its size is measured on the SERVED\\ndocument (an unedited built-in "
        "manual's is its shipped SOP), so the row still answers",
        1,
    ),
    (
        "Size of the type's sop_md in CHARACTERS, measured on the STORED document.",
        "Size of the type's sop_md in CHARACTERS, measured on the document "
        "get_task_manual serves (for an unedited built-in manual, its shipped SOP).",
        1,
    ),
]


def fail(msg):
    print("[t234-size] FAIL — " + msg, file=sys.stderr)
    raise SystemExit(1)


def main():
    text = open(SPEC, encoding="utf-8").read()
    if "measured on the SERVED" in text:
        fail("spec already carries the served-size wording — this script runs once")
    for old, new, expected in EDITS:
        if text.count(old) != expected:
            fail("expected %d hits for %r, got %d" % (expected, old[:60], text.count(old)))
        text = text.replace(old, new)
    json.loads(text)
    open(SPEC, "w", encoding="utf-8").write(text)
    print("[t234-size] ok")


if __name__ == "__main__":
    main()
