#!/usr/bin/env python3
"""Positive control for bin/mcp-write-receipt-guard.py — one planted mutant per
rule the guard claims to enforce, each of which must still be caught AND still be
NAMED.

Why this file exists at all. The guard's whole output on a healthy tree is one
green line with a count on it. A narrowed selector — a tightened scope test, a
`continue`, an exemption that quietly grew — keeps printing that line over a
smaller set, and nothing about the line says which set. The scanner cannot detect
that about itself.

Three properties make this a control rather than decoration, and a future edit
must not lose any of them:

  1. The unmutated tree is checked GREEN FIRST. If it were already red, every
     case below would "pass" for the wrong reason and this file would be a rubber
     stamp.
  2. Every case commits WHAT IT EXPECTS TO SEE IN THE OUTPUT, and EXPECTED_CASES
     commits the set of (name, expectation) pairs. A count floor is too weak
     (delete one with teeth, add a harmless one); a bare set of names is too weak
     (flip an expectation to None and you retire a rule and its control in the
     same edit).
  3. Cases 1 and 2 mutate the REAL production spec rather than a fixture — they
     take a route that is a settled receipt today and turn it back into the shape
     it had before T-133. A guard that only bites on synthetic input is a guard
     nobody has shown bites on this repo.

⚠️ CASES 2 AND 3 ARE A PAIR, and neither replaces the other.

  Case 2 (`request-echo`) pins the RULE. It plants a 100% echo under a DIFFERENT
  schema name on each end, because a $ref comparison would score zero on it and
  that mistake is precisely what the rule was written to avoid.

  Case 3 (`request-echo-at-the-0.6-line`) pins the NUMBER. Rule 2 fires at
  `|request ∩ response| / |response| >= 0.6`, and case 2 sits at 100% — it
  survives every threshold up to 1.0, so on its own it lets somebody raise 0.6 to
  0.9 with the whole file still green. Case 3 restores POST /api/agent/context to
  its pre-T-133 shape, which lands on EXACTLY 60% (3 echoed of 5 response
  fields), so it is the case that dies the moment the line moves up. It is also
  one of the two routes the owner named, so what it protects is not an abstract
  constant: at 0.9 the guard stops seeing a route it exists for.

The threshold is the part of this rule with no other witness. Rule 2 does fire
once on the real tree (update_settings, silenced by its exemption), so the rule
itself is no longer invisible — but nothing outside this file would go red for a
threshold quietly moved to 0.9.

⚠️ The `exemption-overclaims-a-rule` case below is the same lesson learned the
expensive way. Rule 2's only real violation is on an EXEMPT route that also
breaks rule 1, so deleting rule 2 outright left the guard byte-identically green:
the exemption simply went on standing, and the count on the green line was the
only thing that moved. Exemptions now name the rules they excuse and the guard
holds them to exactly that set, which is what makes a deleted rule a red line
instead of a smaller number nobody reads.

Run: python3 bin/tests/mcp-write-receipt-guard-selftest.py
"""

import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Callable, List, Optional, Tuple

ROOT = Path(__file__).resolve().parents[2]
GUARD = ROOT / "bin/mcp-write-receipt-guard.py"

SPEC_REL = "spec/openapi.json"
CATALOG_REL = "spec/mcp-catalog.json"
EXEMPTIONS_REL = "bin/mcp-write-receipt-exemptions.json"
STAGED = (SPEC_REL, CATALOG_REL, EXEMPTIONS_REL)


def stage(root: Path) -> None:
    for rel in STAGED:
        target = root / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / rel, target)


def run_guard(root: Path) -> Tuple[int, str]:
    proc = subprocess.run(
        [sys.executable, str(GUARD)],
        capture_output=True,
        text=True,
        env={"PATH": "/usr/bin:/bin", "OC_MCP_RECEIPT_GUARD_ROOT": str(root)},
    )
    return proc.returncode, proc.stdout + proc.stderr


def read_json(root: Path, rel: str):
    return json.loads((root / rel).read_text(encoding="utf-8"))


def write_json(root: Path, rel: str, value) -> None:
    (root / rel).write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding="utf-8")


# ── the mutants ───────────────────────────────────────────────────────────────

def read_surface_reuse(root: Path) -> None:
    """Point a receipted write back at the READ surface.

    POST /api/chat/mark-read is the route T-133 took OFF ChatReadDTO, and
    GET /api/chat/reads still answers that type — so this is the pre-T-133 shape
    restored, not an invented one.
    """
    spec = read_json(root, SPEC_REL)
    op = spec["paths"]["/api/chat/mark-read"]["post"]
    schema = op["responses"]["200"]["content"]["application/json"]["schema"]
    assert schema["$ref"].endswith("ChatMarkReadReceiptDTO"), schema
    reads = spec["paths"]["/api/chat/reads"]["get"]["responses"]["200"]
    reads_schema = reads["content"]["application/json"]["schema"]
    assert json.dumps(reads_schema).count("ChatReadDTO") == 1, reads_schema
    schema["$ref"] = "#/components/schemas/ChatReadDTO"
    write_json(root, SPEC_REL, spec)


def request_echo(root: Path) -> None:
    """Make a write answer the field set it was handed, under a DIFFERENT type name.

    Two names, one payload — the shape a $ref comparison cannot see. The
    conversion is mechanical: copy the request schema's properties onto a fresh
    response schema and point the 200 at it.
    """
    spec = read_json(root, SPEC_REL)
    op = spec["paths"]["/api/agent/context"]["post"]
    request_ref = op["requestBody"]["content"]["application/json"]["schema"]["$ref"]
    request = spec["components"]["schemas"][request_ref.rsplit("/", 1)[-1]]
    assert request["properties"], request
    spec["components"]["schemas"]["SelftestContextEchoDTO"] = {
        "properties": json.loads(json.dumps(request["properties"])),
        "title": "SelftestContextEchoDTO",
        "type": "object",
    }
    op["responses"]["200"]["content"]["application/json"]["schema"] = {
        "$ref": "#/components/schemas/SelftestContextEchoDTO"
    }
    write_json(root, SPEC_REL, spec)


def request_echo_at_the_threshold(root: Path) -> None:
    """Restore POST /api/agent/context to its pre-T-133 shape, which lands on the
    0.6 line EXACTLY: the old answer carried {agent_id, ts} plus all three fields
    the request sent, i.e. 3 echoed of 5 response fields = 60%.

    The asserts below are the arithmetic, not decoration: if the request or the
    receipt ever changes shape this mutant stops sitting on the line and stops
    testing the threshold, and the assert says so instead of the case quietly
    becoming a second copy of case 2.
    """
    spec = read_json(root, SPEC_REL)
    op = spec["paths"]["/api/agent/context"]["post"]
    request_ref = op["requestBody"]["content"]["application/json"]["schema"]["$ref"]
    request = spec["components"]["schemas"][request_ref.rsplit("/", 1)[-1]]
    sent = json.loads(json.dumps(request["properties"]))
    assert sorted(sent) == ["compaction_count", "context_pct", "rate_limits"], sorted(sent)
    receipt = spec["components"]["schemas"]["AgentContextReceiptDTO"]["properties"]
    assert sorted(receipt) == ["agent_id", "ts"], sorted(receipt)
    merged = dict(sent, **json.loads(json.dumps(receipt)))
    assert len(sent) / len(merged) == 0.6, (len(sent), len(merged))
    spec["components"]["schemas"]["SelftestContextPreT133DTO"] = {
        "properties": merged,
        "title": "SelftestContextPreT133DTO",
        "type": "object",
    }
    op["responses"]["200"]["content"]["application/json"]["schema"] = {
        "$ref": "#/components/schemas/SelftestContextPreT133DTO"
    }
    write_json(root, SPEC_REL, spec)


def blank_exemption_reason(root: Path) -> None:
    """Empty one exemption's reason. Whitespace, not deletion — a missing key is
    the easy case; a reason someone typed a space into is the one that looks filled."""
    doc = read_json(root, EXEMPTIONS_REL)
    assert doc["exemptions"], doc
    doc["exemptions"][0]["reason"] = "   "
    write_json(root, EXEMPTIONS_REL, doc)


def stale_exemption(root: Path) -> None:
    """Name an operation that is not on the agent-callable write surface."""
    doc = read_json(root, EXEMPTIONS_REL)
    doc["exemptions"].append(
        {"operationId": "handle_selftest_no_such_route_get", "reason": "planted"}
    )
    write_json(root, EXEMPTIONS_REL, doc)


def dead_exemption(root: Path) -> None:
    """Exempt a route that breaks no rule. Cover for a regression that has not
    happened yet reads exactly like cover for one that has."""
    doc = read_json(root, EXEMPTIONS_REL)
    doc["exemptions"].append(
        {
            "operationId": "handle_mark_chat_read_api_chat_mark_read_post",
            "rules": ["read-surface-reuse"],
            "reason": "planted — this route is clean since T-133",
        }
    )
    write_json(root, EXEMPTIONS_REL, doc)


def drop_the_exemption(root: Path) -> None:
    """Remove the ONE real exemption. update_settings answers SettingsDTO, which
    GET /api/settings also answers — so this proves rule 1 fires on production
    bytes and that the exemption (not a hole in the scan) is what silences it."""
    doc = read_json(root, EXEMPTIONS_REL)
    doc["exemptions"] = []
    write_json(root, EXEMPTIONS_REL, doc)


def exemption_overclaims_a_rule(root: Path) -> None:
    """Leave the exemption alone and take rule 2's ONLY real violation away, by
    shrinking update_settings' request body so its echo mass drops to zero. The
    200 still answers SettingsDTO, so rule 1 still fires and the entry is still
    live — only its `request-echo` claim goes stale.

    🔴 This is the case that exists because the guard once survived
    `if rule_2: -> if False:` byte-identically green. update_settings is the only
    route rule 2 fires on, it is EXEMPT, and it breaks rule 1 as well — so
    deleting rule 2 removed a violation nobody was counting and the exemption
    went on standing over it. Holding an exemption to the rules it NAMES is what
    turns that into a red line; this case is what keeps that property.
    """
    spec = read_json(root, SPEC_REL)
    op = spec["paths"]["/api/settings"]["patch"]
    schema = op["responses"]["200"]["content"]["application/json"]["schema"]
    assert schema["$ref"].endswith("SettingsDTO"), schema
    spec["components"]["schemas"]["SelftestNarrowSettingsPatchDTO"] = {
        "properties": {"selftest_nothing_the_response_carries": {"type": "string"}},
        "title": "SelftestNarrowSettingsPatchDTO",
        "type": "object",
    }
    op["requestBody"]["content"]["application/json"]["schema"] = {
        "$ref": "#/components/schemas/SelftestNarrowSettingsPatchDTO"
    }
    write_json(root, SPEC_REL, spec)


def exemption_omits_a_rule(root: Path) -> None:
    """The other direction: the route breaks a rule its entry does not name. An
    exemption that grows to cover rules added after it was granted is review
    nobody did."""
    doc = read_json(root, EXEMPTIONS_REL)
    rules = doc["exemptions"][0]["rules"]
    assert "request-echo" in rules, rules
    doc["exemptions"][0]["rules"] = [r for r in rules if r != "request-echo"]
    write_json(root, EXEMPTIONS_REL, doc)


def empty_catalog(root: Path) -> None:
    """A catalog that publishes nothing would make every check vacuous."""
    catalog = read_json(root, CATALOG_REL)
    catalog["tools"] = []
    write_json(root, CATALOG_REL, catalog)


def receipt_stays_green(root: Path) -> None:
    """A NEW dedicated receipt type carrying only server-computed values must NOT
    redden. Without this the whole file is satisfied by a guard that fails on
    everything."""
    spec = read_json(root, SPEC_REL)
    spec["components"]["schemas"]["SelftestMintReceiptDTO"] = {
        "properties": {
            "minted_id": {"title": "Minted Id", "type": "string"},
            "pending": {"title": "Pending", "type": "boolean"},
            "ts": {"title": "Ts", "type": "number"},
        },
        "required": ["minted_id"],
        "title": "SelftestMintReceiptDTO",
        "additionalProperties": False,
        "type": "object",
    }
    op = spec["paths"]["/api/agent/context"]["post"]
    op["responses"]["200"]["content"]["application/json"]["schema"] = {
        "$ref": "#/components/schemas/SelftestMintReceiptDTO"
    }
    write_json(root, SPEC_REL, spec)


# (name, mutate, the substring the guard's output must carry — None = stay green)
CASES: List[Tuple[str, Callable[[Path], None], Optional[str]]] = [
    ("read-surface-reuse", read_surface_reuse, "post_chat_mark-read"),
    ("request-echo", request_echo, "is mostly the request handed back"),
    ("request-echo-at-the-0.6-line (ingest_agent_context, 3 of 5 = 60%)", request_echo_at_the_threshold, "ingest_agent_context (POST /api/agent/context) its 200 is mostly the request handed back: 3 of its 5 response fields"),
    ("blank-exemption-reason", blank_exemption_reason, "reason is empty or blank"),
    ("stale-exemption", stale_exemption, "handle_selftest_no_such_route_get"),
    ("dead-exemption", dead_exemption, "no longer breaks any rule"),
    ("exemption-is-what-silences-update-settings", drop_the_exemption, "update_settings"),
    ("exemption-overclaims-a-rule", exemption_overclaims_a_rule, "is EXEMPT for ['request-echo'], which it does NOT break"),
    ("exemption-omits-a-rule", exemption_omits_a_rule, "breaks ['request-echo'], which its exemption does not list"),
    ("empty-catalog", empty_catalog, "publishes no tools"),
    ("dedicated-receipt-stays-green", receipt_stays_green, None),
]

# Committed so a case cannot be retired by deleting it OR by flattening its
# expectation to None.
EXPECTED_CASES = {
    ("read-surface-reuse", "post_chat_mark-read"),
    ("request-echo", "is mostly the request handed back"),
    ("request-echo-at-the-0.6-line (ingest_agent_context, 3 of 5 = 60%)", "ingest_agent_context (POST /api/agent/context) its 200 is mostly the request handed back: 3 of its 5 response fields"),
    ("blank-exemption-reason", "reason is empty or blank"),
    ("stale-exemption", "handle_selftest_no_such_route_get"),
    ("dead-exemption", "no longer breaks any rule"),
    ("exemption-is-what-silences-update-settings", "update_settings"),
    ("exemption-overclaims-a-rule", "is EXEMPT for ['request-echo'], which it does NOT break"),
    ("exemption-omits-a-rule", "breaks ['request-echo'], which its exemption does not list"),
    ("empty-catalog", "publishes no tools"),
    ("dedicated-receipt-stays-green", None),
}


def main() -> None:
    declared = {(name, expect) for name, _plant, expect in CASES}
    if declared != EXPECTED_CASES:
        print(
            "[mcp-write-receipt-selftest] FAIL — the case set drifted from what this "
            f"file commits.\n  missing: {sorted(map(str, EXPECTED_CASES - declared))}"
            f"\n  unexpected: {sorted(map(str, declared - EXPECTED_CASES))}",
            file=sys.stderr,
        )
        raise SystemExit(1)

    failures: List[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        clean = Path(tmp) / "clean"
        clean.mkdir()
        stage(clean)
        rc, output = run_guard(clean)
        if rc != 0:
            print(
                "[mcp-write-receipt-selftest] FAIL — the UNMUTATED tree is already red, "
                f"so every case below would pass for the wrong reason:\n{output}",
                file=sys.stderr,
            )
            raise SystemExit(1)

        for name, plant, expect in CASES:
            root = Path(tmp) / name
            root.mkdir()
            stage(root)
            try:
                plant(root)
            except AssertionError as exc:
                failures.append(f"{name}: the mutant did not apply — {exc}")
                continue
            rc, output = run_guard(root)
            if expect is None:
                if rc != 0:
                    failures.append(f"{name}: this must stay GREEN but the guard went red:\n{output}")
                continue
            if rc == 0:
                failures.append(f"{name}: the guard did NOT notice it:\n{output}")
            elif expect not in output:
                failures.append(
                    f"{name}: went red but never named {expect!r}, so it caught something "
                    f"else:\n{output}"
                )

    if failures:
        print("[mcp-write-receipt-selftest] FAIL —\n  " + "\n  ".join(failures), file=sys.stderr)
        raise SystemExit(1)
    print(
        f"[mcp-write-receipt-selftest] all green ({len(CASES)} known bypasses, each still caught)"
    )


if __name__ == "__main__":
    main()
