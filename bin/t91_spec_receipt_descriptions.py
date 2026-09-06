#!/usr/bin/env python3
"""T-91 follow-through (owner 2026-09-06) — repair three receipt descriptions
that the previous commit's deletions left pointing at text that no longer
exists.

WHAT AND WHY.

(1) AgentRelocateReceiptDTO.relocation_deferred lost a detail in the move. The
    OutsourceWorkerDTO copy that was just deleted carried BOTH causes of a
    deferred move; the receipt's copy compressed them into one clause and the
    ladder cause — an EXISTING wind-down at a HIGHER rung already owning the
    agent, the pin saved, the lower stage refused, the move landing on the
    deadline that wind-down already carries — went with the deleted text. A
    parity test's comment still tells the reader to go and read it there. This
    restores that cause, in the receipt, which is the only place the signal
    lives now.

(2) OutsourceRestartReceiptDTO and MemberActivateReceiptDTO each say, at schema
    level, that "the openapi text describes" the three flags as absent or null
    on every other read. The properties that text described are gone, so the
    self-reference points at nothing. The fact is STRONGER than it was: the
    flags are not declared on any read structure at all, so they are not fields
    that read back null — the read face simply does not have them.

(3) The same correction, at property level, for the two "absent or null on every
    other read" self-descriptions. "Absent or null" was accurate while the
    property existed and could serialise as null; with the property deleted the
    accurate statement is that no read structure declares it.

Edits spec/openapi.json as TEXT rather than reserialising it: the file is
hand-maintained house style (compact leaf objects, deliberate non-alphabetical
grouping) and a json.dump round-trip would reformat all 21k lines and bury the
real change. Precedent: bin/t91_spec_drop_dead_read_flags.py.

Refuses to run twice, and re-parses + re-checks its own result.
"""

import json
import sys

SPEC = "spec/openapi.json"

# (label, exact old JSON-escaped fragment, new fragment)
EDITS = [
    (
        "AgentRelocateReceiptDTO.relocation_deferred",
        "WHICH of ``relocation_pending``'s two causes this is: true means a deliberately deferred move — a wind-down is open and the agent keeps running on the old machine until its own 收口 — rather than a delivery failure. A caller must hold back the \\\"nothing was dispatched\\\" alert for this case; the cockpit does exactly that.",
        "WHICH of ``relocation_pending``'s two causes this is: true means a deliberately deferred move — the agent is live with uncollected state, so a graceful wind-down owns the move and the agent keeps running on the old machine until its own 收口 — rather than a delivery failure. TWO ways that happens, and the field does not distinguish them because the consumer's question is the same in both: (a) THIS relocate opened the wind-down, and the move lands when the agent answers report_stopped; (b) an EXISTING wind-down at a HIGHER rung of the 停止 → 加速停止 → 強制停止 ladder already owns the agent, so the pin was saved and the ladder refused to re-open a lower stage — the move lands at THAT wind-down's collect, on whatever deadline it already carries (T-170e). A caller must hold back the \\\"nothing was dispatched\\\" alert for either case; the cockpit does exactly that.",
    ),
    (
        "OutsourceRestartReceiptDTO (schema)",
        "``activation_pending`` is additionally one of the three flags the openapi text describes as set ONLY on this kind of response and absent or null on every other read, so no follow-up query can recover it.",
        "``activation_pending`` is additionally one of the three flags that no READ structure declares at all: ``OutsourceWorkerDTO`` does not carry it, so it is not a field that reads back null - the read face does not have the field. No follow-up query can recover it.",
    ),
    (
        "OutsourceRestartReceiptDTO.activation_pending (property)",
        "It is here or nowhere: this flag is set only on responses of this kind and is absent or null on every other read of the worker, so a caller that drops it cannot ask again.",
        "It is here or nowhere: this flag is set only on responses of this kind, and no read structure declares it at all - there is no worker read that carries the field, null or otherwise - so a caller that drops it cannot ask again.",
    ),
    (
        "MemberActivateReceiptDTO (schema)",
        "It is one of the three flags this document describes as set only on this kind of response and absent or null on every other read, so a caller that drops it cannot ask again — there is no row to ask.",
        "It is one of the three flags that no READ structure declares at all: ``MemberDTO`` does not carry it, so it is not a field that reads back null — the read face does not have the field. A caller that drops it cannot ask again; there is no row to ask.",
    ),
    (
        "MemberActivateReceiptDTO.activation_pending (property)",
        "It is here or nowhere: the flag is set only on responses of this kind and is absent or null on every other read of the member.",
        "It is here or nowhere: the flag is set only on responses of this kind, and no read structure declares it at all — there is no member read that carries the field, null or otherwise.",
    ),
]


def main() -> int:
    with open(SPEC, encoding="utf-8") as fh:
        raw = fh.read()

    for label, old, new in EDITS:
        if new in raw:
            print(f"refusing: {label} already carries the new text", file=sys.stderr)
            return 1
        n = raw.count(old)
        if n != 1:
            print(f"refusing: {label} old fragment occurs {n} times, want 1", file=sys.stderr)
            return 1
        raw = raw.replace(old, new)
        print(f"rewrote {label}")

    doc = json.loads(raw)  # re-parse: still legal JSON
    schemas = doc["components"]["schemas"]

    rd = schemas["AgentRelocateReceiptDTO"]["properties"]["relocation_deferred"]["description"]
    for needle in ("(a) THIS relocate opened the wind-down", "HIGHER rung", "T-170e",
                   "deadline it already carries"):
        assert needle in rd, needle
    for name, read_face in (("OutsourceRestartReceiptDTO", "OutsourceWorkerDTO"),
                            ("MemberActivateReceiptDTO", "MemberDTO")):
        sch = schemas[name]
        assert "no READ structure declares" in sch["description"], name
        assert read_face in sch["description"], name
        prop = sch["properties"]["activation_pending"]["description"]
        assert "no read structure declares it at all" in prop, name
        assert "absent or null on every other read" not in prop, name
    # the receipts still carry the fields themselves - descriptions only
    assert set(schemas["AgentRelocateReceiptDTO"]["properties"]) == {
        "id", "relocation_pending", "relocation_deferred"}
    assert set(schemas["OutsourceRestartReceiptDTO"]["properties"]) == {
        "id", "activation_pending", "last_op_reason"}
    assert set(schemas["MemberActivateReceiptDTO"]["properties"]) == {
        "id", "activation_pending", "last_op_reason"}

    with open(SPEC, "w", encoding="utf-8") as fh:
        fh.write(raw)
    print("spec/openapi.json rewritten and re-parsed clean")
    return 0


if __name__ == "__main__":
    sys.exit(main())
