#!/usr/bin/env python3
"""mcp-write-receipt-guard — an agent-callable WRITE must answer a receipt, never
the thing it just wrote.

THE INCIDENT THIS GUARDS AGAINST. Owner, verbatim: 「我們應該記在哪裡 mcp call 成功
以後不要帶大量 payload 回來，尤其是 create / update 以後回傳完整 payload 的
pattern，會浪費 agent context」. Every one of these answers lands in a model's
context window, so a create that hands back the row it created spends the
caller's budget restating what the caller already knew. T-91, T-a98d, T-bb70,
T-83ef and T-133 each reshaped a batch of these by hand; nothing stopped the next
route from being written the old way, and nothing ever will go red for it — the
route works, the tests pass, the spec is consistent with itself, and the only
symptom is a context window that fills faster.

SCOPE, AND WHY IT IS THIS AND NOT "EVERY WRITE". Owner scoped it a second time,
also verbatim: 「重點看 mcp 不是 UI api」. So the scan covers exactly the surface an
AGENT can reach — a write operation carrying an `x-mcp` name that spec/mcp-catalog.json
actually publishes as a tool. A cockpit-only route answering a fat payload costs
a browser some bytes; the same route answering an agent costs a model its
context, and those are not the same problem. The catalog is consulted rather than
trusted from `x-mcp.include` alone because the catalog is what ocserverd serves
verbatim from tools/list — it is the surface, not a declaration about it.

THE RULE, STATED AS A QUERY THAT MUST COME BACK EMPTY:

    for every agent-callable write operation (POST/PUT/PATCH/DELETE with a
      published MCP tool name), look at its 2xx success body —
    it either REUSES A READ TYPE (the same schema some GET answers)
      or it is MOSTLY THE REQUEST HANDED BACK (at least 60% of the fields the
      RESPONSE declares are field names the caller sent)

    ⇒ the answer must be zero rows, minus a named exemption list.

WHY RULE 2 IS WRITTEN OVER FIELD NAMES, AND WHY IT IS A RATIO RATHER THAN AN
EQUALITY. Both halves are MEASUREMENTS, not preferences. Every number below was
run against this repo; two candidate criteria were rejected on their own scores.

  FIELD NAMES, NOT `$ref`. The obvious implementation compares the request's
  `$ref` with the response's `$ref`, and on this tree it scores ZERO against the
  two ingest routes that were the plainest instances of the pattern:
  `POST /api/monitoring/telemetry` took `AgentTelemetryIngestDTO` in and handed
  `AgentTelemetryDTO` back, `POST /api/agent/context` took `AgentContextIngestDTO`
  in and handed `AgentContextDTO` back. Two names, one payload. A type-name
  comparison cannot see that class at all, and it fails SILENTLY — it prints all
  green over the exact routes the rule exists for.

  A RATIO, NOT SET EQUALITY. The first version of this rule fired only on
  `response_fields == request_fields`. Measured on the pre-T-133 spec
  (`git show 94d7ec15:spec/openapi.json`) that scores ZERO on BOTH ingest routes
  as well: telemetry answered 15 of its 17 request fields plus 2 of its own,
  context answered all 3 of its request fields plus 2 of its own, and neither is
  an EQUAL set. So the field-name rewrite cured the type-name blindness and kept
  the silence — set equality is not a stricter version of this rule, it is a rule
  that never fires on this repo at all. It was also invisible: delete the whole
  branch and the guard stayed byte-identically green on the real tree, which is
  why bin/tests/ now pins the threshold as well as the rule.

  AND NOT SUBSET COVERAGE EITHER, which was the obvious next widening
  (`request_fields <= response_fields`). Measured on both specs: on the pre-T-133
  spec it catches `ingest_agent_context` but NOT `ingest_telemetry` — telemetry's
  request carries `account_label` and `model`, which its response never had, so
  the request is not a subset — i.e. it recovers ONE of the two routes the owner
  named. On the post-T-133 tree it fires on FOUR settled receipts
  (update_account, update_machine, mark_duplicate, set_task_priority), each of
  which would need a permanent exemption written purely to silence a false
  positive. One of two routes, bought with four exemptions.

WHY THE THRESHOLD IS 0.6, WITH ITS DENOMINATOR. The criterion is ECHO MASS: of
the fields the RESPONSE declares, what share are names the caller sent —
`|request ∩ response| / |response|`. Measured across all 83 agent-callable
writes (49 of which declare a JSON body on both ends), on the pre-T-133 spec:

    97%  update_settings           |resp|=32  echoed=31
    88%  ingest_telemetry          |resp|=17  echoed=15   ← owner-named route
    60%  ingest_agent_context      |resp|= 5  echoed= 3   ← owner-named route
    ─────────────────────────  threshold 0.6  ─────────────────────────
    50%  post_chat                 |resp|= 4  echoed= 2
    50%  update_scheduled_message  |resp|=12  echoed= 6
    42%  create_scheduled_message  |resp|=12  echoed= 5
    33%  post_chat_mark-read, create_role, set_task_priority
    25%  update_account, update_machine, create_reply_card, post_task_message
    ..   nothing else scores above 25%

THE GAP IS BETWEEN 50% AND 60%, AND THE THRESHOLD SITS INSIDE THE GAP. 0.6 is
not a number somebody liked; it is the widest separation this distribution
offers, and moving it either way costs something nameable — 0.5 pulls in
`post_chat` and `update_scheduled_message`, 0.9 drops BOTH owner-named routes
(`ingest_agent_context` at 60% and `ingest_telemetry` at 88%) and leaves the
rule firing only on the one route that is already exempt. Above the line: both
owner-named routes plus `update_settings`, which is the one standing exemption.
Below it: every settled receipt on this tree. So the exemption list stays at ONE
entry under this rule, where subset coverage would have made it five.

WHAT IS ALLOWED, said out loud so a green means something:

  1. A success with no JSON body at all (204, or a 2xx that declares no content).
  2. A DEDICATED type — one no GET operation answers. That is the whole of rule
     1: a receipt is allowed to be as detailed as it likes as long as it is not
     the read surface wearing a different hat.
  3. Server-computed values the caller could not predict — a minted id, a token,
     a pending flag, a count, a post-clamp watermark. Rule 2 weighs the response
     against ITSELF, so a receipt whose MAJORITY is server-computed passes on its
     own merits however many of the caller's own fields it also carries back for
     readability.

WHAT THIS GUARD DELIBERATELY DOES NOT COVER. Each bullet is a hole that was
looked at and left, not one that was overlooked; a green here does not deny any
of them:

  * SIZE. Nothing here counts bytes or fields. A dedicated 40-field receipt type
    that nothing else answers is invisible to both rules, and that is the largest
    residue by far. Rule 1 asks "is this the read surface", not "is this small".
  * A RENAMED ECHO. Rule 2 compares field NAMES, so a response that carries the
    request back under different keys (`peer` in, `peer_id` out) is not an echo
    by this definition. Making the comparison fuzzy would put the guard in the
    business of guessing which renames are the same field, and a guard that cries
    wolf is one everybody learns to ignore.
  * 🔴 A CORRECTION, left in place rather than quietly edited away, because the
    thing worth remembering is that a prediction was written down without being
    run. This bullet used to say that a PARTIAL ECHO was deliberately out of
    scope, and that widening rule 2 to "the response covers the request" would
    catch "five routes on this tree that are settled receipts (update_account,
    update_machine, mark_duplicate, set_task_priority, ingest_agent_context as it
    now stands)". Both halves are wrong when measured. (a) The partial echo is
    now IN scope — it is the whole point of the ratio above, and it is what
    recovers `ingest_telemetry`, which no earlier version of this rule could see.
    (b) The prediction said five; the MEASURED cost on this tree is FOUR.
    `ingest_agent_context` as it now stands takes {context_pct,
    compaction_count, rate_limits} and answers {agent_id, ts}, so the response
    is not a superset of the request and it never appears under subset coverage.
    The first four names above are the measured set; the fifth was a guess
    sitting in a list of facts. (It was not even wrong about the OLD tree — on
    the pre-T-133 spec `ingest_agent_context` answered a superset and subset
    coverage did fire on all five. What the prediction got wrong is the tense.)
  * A BIG ANSWER THAT IS HALF ECHO. The ratio is taken over the RESPONSE, so a
    3-field answer of which 2 came from the caller (67%) is a violation while a
    20-field answer of which 10 did (50%) is not. That is on purpose — what this
    rule prices is the SHARE of an answer that tells the caller nothing — but it
    does mean size is still not what is measured, and a fat response can dilute
    its own echo below the line. See the SIZE bullet above; they are the same
    hole seen from two directions.
  * NESTED payloads. Only TOP-LEVEL field names are compared. A receipt whose one
    field is the whole stored object is a dedicated type answering rule 1 and an
    empty intersection answering rule 2.
  * Anything outside the MCP surface, by the owner ruling quoted above.

THE EXEMPTION LIST is bin/mcp-write-receipt-exemptions.json, and it is
STRUCTURALLY not a rubber stamp:
  * every entry must carry a NON-BLANK reason — an empty or whitespace-only one
    fails the guard rather than being tolerated, because a reason nobody has to
    write is a reason nobody writes;
  * an entry for an operation that is no longer in scope fails (a stale
    exemption is a silent narrowing);
  * an entry for an operation that no longer violates anything fails, so the
    list shrinks as routes are fixed instead of accumulating permanent cover.

The scanned / exempt counts are PRINTED on every green run, in the shape
bin/effort-vocab-guard.py established: a scan that has been narrowed shows up as
a smaller number, not as silence.

bin/tests/mcp-write-receipt-guard-selftest.py is what keeps this honest — one
planted mutant per rule, each of which must still be caught AND still be NAMED,
plus one sitting on the 0.6 line itself, because a threshold is the part of a
rule that can be moved without deleting anything.
Both halves are dispatched by ONE Makefile target (lint-mcp-write-receipt), so
the scanner cannot travel without its positive control.
"""

import json
import os
import re
import sys
from pathlib import Path
from typing import Any, Dict, List, NoReturn, Set, Tuple

ROOT = Path(os.environ.get("OC_MCP_RECEIPT_GUARD_ROOT", Path(__file__).resolve().parents[1]))

SPEC_REL = "spec/openapi.json"
CATALOG_REL = "spec/mcp-catalog.json"
EXEMPTIONS_REL = "bin/mcp-write-receipt-exemptions.json"

WRITE_METHODS = ("post", "put", "patch", "delete")

# Rule 2's threshold. See "WHY THE THRESHOLD IS 0.6, WITH ITS DENOMINATOR" above:
# the measured distribution has a gap between 50% and 60% and this sits in it.
# bin/tests/mcp-write-receipt-guard-selftest.py plants a mutant at EXACTLY this
# ratio, so raising the number is a red test rather than a quiet narrowing.
ECHO_MASS_THRESHOLD = 0.6

# The two rules, named. An exemption must say which of them it excuses, so an
# exemption cannot outlive the violation it was granted for — see load_exemptions.
RULE_READ_SURFACE = "read-surface-reuse"
RULE_REQUEST_ECHO = "request-echo"
RULES = (RULE_READ_SURFACE, RULE_REQUEST_ECHO)


def fail(message: str) -> NoReturn:
    print("[mcp-write-receipt-guard] FAIL — " + message, file=sys.stderr)
    raise SystemExit(1)


def load(rel: str) -> Any:
    path = ROOT / rel
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except OSError as exc:
        fail(f"cannot read {rel}: {exc}")
    except json.JSONDecodeError as exc:
        fail(f"{rel} is not valid JSON: {exc}")


def json_content(holder: Any) -> Any:
    """The application/json schema node of a requestBody / response, or None."""
    if not isinstance(holder, dict):
        return None
    content = holder.get("content")
    if not isinstance(content, dict):
        return None
    for media, body in content.items():
        if "json" in media and isinstance(body, dict) and isinstance(body.get("schema"), dict):
            return body["schema"]
    return None


def success_bodies(op: Dict[str, Any]) -> List[Tuple[str, Any]]:
    """Every 2xx response of one operation that declares a JSON body."""
    out = []
    for code, response in (op.get("responses") or {}).items():
        if not str(code).startswith("2"):
            continue
        schema = json_content(response)
        if schema is not None:
            out.append((str(code), schema))
    return out


def schema_names(node: Any, schemas: Dict[str, Any], seen: Set[str] = None) -> Set[str]:
    """Every component schema NAME a node answers with, unwrapping arrays.

    An array of MemberDTO and a bare MemberDTO are the same read surface, so the
    element type counts. Composition keywords are followed for the same reason: a
    200 declared as `anyOf: [MemberDTO, null]` is still MemberDTO.
    """
    seen = seen if seen is not None else set()
    if not isinstance(node, dict):
        return set()
    ref = node.get("$ref")
    if isinstance(ref, str) and ref.startswith("#/components/schemas/"):
        name = ref.rsplit("/", 1)[-1]
        if name in seen:
            return set()
        seen.add(name)
        return {name} | schema_names(schemas.get(name, {}), schemas, seen)
    out: Set[str] = set()
    if isinstance(node.get("items"), dict):
        out |= schema_names(node["items"], schemas, seen)
    for key in ("allOf", "anyOf", "oneOf"):
        for sub in node.get(key) or []:
            out |= schema_names(sub, schemas, seen)
    return out


def field_names(node: Any, schemas: Dict[str, Any], seen: Set[str] = None) -> Set[str]:
    """The TOP-LEVEL field names a schema node declares.

    Resolved through $ref and composition on purpose: this is what makes rule 2
    see an echo whose two ends wear different type names, which is the whole
    reason the rule is not a $ref comparison.
    """
    seen = seen if seen is not None else set()
    if not isinstance(node, dict):
        return set()
    ref = node.get("$ref")
    if isinstance(ref, str) and ref.startswith("#/components/schemas/"):
        name = ref.rsplit("/", 1)[-1]
        if name in seen:
            return set()
        seen.add(name)
        return field_names(schemas.get(name, {}), schemas, seen)
    out: Set[str] = set()
    props = node.get("properties")
    if isinstance(props, dict):
        out |= set(props.keys())
    for key in ("allOf", "anyOf", "oneOf"):
        for sub in node.get(key) or []:
            if isinstance(sub, dict) and sub.get("type") == "null":
                continue
            out |= field_names(sub, schemas, seen)
    if isinstance(node.get("items"), dict):
        out |= field_names(node["items"], schemas, seen)
    return out


class Operation:
    def __init__(self, path: str, method: str, tool: str, op: Dict[str, Any]):
        self.path, self.method, self.tool, self.op = path, method, tool, op
        self.operation_id = op.get("operationId") or ""

    @property
    def where(self) -> str:
        return f"{self.tool} ({self.method.upper()} {self.path})"


def in_scope(spec: Dict[str, Any], catalog: Dict[str, Any]) -> List[Operation]:
    published = set()
    for tool in catalog.get("tools") or []:
        if isinstance(tool, dict) and isinstance(tool.get("name"), str):
            published.add(tool["name"])
    if not published:
        fail(
            f"{CATALOG_REL} publishes no tools. An empty MCP surface would make every "
            f"check below vacuously pass — that is a broken read, not a clean tree."
        )
    found: List[Operation] = []
    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        for method in WRITE_METHODS:
            op = item.get(method)
            if not isinstance(op, dict):
                continue
            xmcp = op.get("x-mcp")
            name = xmcp.get("name") if isinstance(xmcp, dict) else None
            if isinstance(name, str) and name in published:
                found.append(Operation(path, method, name, op))
    if not found:
        fail(
            "no agent-callable write operations were found at all. This repo has dozens; "
            "an empty scan means the selector broke, not that the tree got clean."
        )
    return found


def read_surface(spec: Dict[str, Any], schemas: Dict[str, Any]) -> Dict[str, str]:
    """Schema name -> the GET that answers it. The READ surface, discovered."""
    surface: Dict[str, str] = {}
    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        op = item.get("get")
        if not isinstance(op, dict):
            continue
        for _code, schema in success_bodies(op):
            for name in schema_names(schema, schemas):
                surface.setdefault(name, f"GET {path}")
    return surface


def violations_of(entry: Operation, spec: Dict[str, Any], schemas: Dict[str, Any],
                  surface: Dict[str, str]) -> List[Tuple[str, str]]:
    """Every rule this operation breaks, as (rule name, its own sentence)."""
    rows: List[Tuple[str, str]] = []
    bodies = success_bodies(entry.op)
    if not bodies:
        return rows  # allowed: a success with no JSON body says only that it happened

    request = json_content(entry.op.get("requestBody"))
    request_fields = field_names(request, schemas) if request is not None else set()

    for code, schema in bodies:
        # ── rule 1: the read surface, answered by a write ────────────────────
        for name in sorted(schema_names(schema, schemas)):
            if name in surface:
                rows.append((RULE_READ_SURFACE,
                    f"its {code} answers {name}, which is the READ surface — "
                    f"{surface[name]} answers the same type. Give this write a receipt "
                    f"type of its own; a caller that wants the row can ask for it."
                ))
        # ── rule 2: the request handed back ──────────────────────────────────
        response_fields = field_names(schema, schemas)
        echoed = request_fields & response_fields
        if response_fields and len(echoed) / len(response_fields) >= ECHO_MASS_THRESHOLD:
            listed = ", ".join(sorted(echoed))
            share = len(echoed) / len(response_fields)
            rows.append((RULE_REQUEST_ECHO,
                f"its {code} is mostly the request handed back: {len(echoed)} of its "
                f"{len(response_fields)} response fields ({{{listed}}}) are names the "
                f"caller sent — {share:.0%} echo mass, at or over the "
                f"{ECHO_MASS_THRESHOLD:.0%} line. Most of that answer restates the "
                f"request. Note this is measured on FIELD NAMES, not on schema names "
                f"— the two ends may well be different types."
            ))
    return rows


def load_exemptions(scope_by_id: Dict[str, Operation]) -> Dict[str, Tuple[str, Set[str]]]:
    raw = load(EXEMPTIONS_REL)
    entries = raw.get("exemptions") if isinstance(raw, dict) else None
    if not isinstance(entries, list):
        fail(f"{EXEMPTIONS_REL} must be an object with an \"exemptions\" list")
    out: Dict[str, Tuple[str, Set[str]]] = {}
    problems: List[str] = []
    for index, entry in enumerate(entries):
        if not isinstance(entry, dict):
            problems.append(f"entry #{index} is not an object")
            continue
        operation_id = entry.get("operationId")
        reason = entry.get("reason")
        if not isinstance(operation_id, str) or not operation_id.strip():
            problems.append(f"entry #{index} has no operationId")
            continue
        # 🔴 The reason is STRUCTURAL, not documentation. An exemption whose
        # justification can be left blank is an exemption nobody has to justify,
        # and this list is the only thing standing between the rule and "just
        # add it to the file".
        if not isinstance(reason, str) or not reason.strip():
            problems.append(
                f"{operation_id}: reason is empty or blank. Every exemption must say "
                f"WHY, in words a reviewer can disagree with — 'reason' is the review."
            )
            continue
        if operation_id in out:
            problems.append(f"{operation_id}: listed twice")
            continue
        if operation_id not in scope_by_id:
            problems.append(
                f"{operation_id}: not an agent-callable write operation any more. A "
                f"stale exemption silently narrows the scan — delete the entry."
            )
            continue
        # 🔴 An exemption excuses NAMED RULES, not the route. Without this an
        # entry granted for one rule silently covers every rule added later, and
        # — the case that actually bit — a rule DELETED from this file leaves its
        # exemption standing over a violation that no longer exists, so the whole
        # guard stays green through the deletion. main() holds the claim to the
        # rules the route really breaks, in both directions.
        rules = entry.get("rules")
        if not isinstance(rules, list) or not rules or not all(r in RULES for r in rules):
            problems.append(
                f"{operation_id}: \"rules\" must be a non-empty list naming which rules "
                f"this exemption excuses, drawn from {list(RULES)} — got {rules!r}."
            )
            continue
        out[operation_id] = (reason, set(rules))
    if problems:
        fail(f"{EXEMPTIONS_REL} is not usable:\n  " + "\n  ".join(problems))
    return out


def main() -> None:
    spec = load(SPEC_REL)
    catalog = load(CATALOG_REL)
    schemas = (spec.get("components") or {}).get("schemas") or {}
    if not schemas:
        fail(f"{SPEC_REL} declares no component schemas; nothing below could be true")

    scope = in_scope(spec, catalog)
    scope_by_id = {}
    for entry in scope:
        if not entry.operation_id:
            fail(f"{entry.where} has no operationId — an exemption could never name it")
        scope_by_id[entry.operation_id] = entry
    exemptions = load_exemptions(scope_by_id)
    surface = read_surface(spec, schemas)

    findings: List[str] = []
    excused: List[str] = []
    kinds = {"no-json-body": 0, "dedicated-receipt": 0, RULE_READ_SURFACE: 0, RULE_REQUEST_ECHO: 0}

    for entry in sorted(scope, key=lambda e: (e.path, e.method)):
        rows = violations_of(entry, spec, schemas, surface)
        if not success_bodies(entry.op):
            kinds["no-json-body"] += 1
        elif not rows:
            kinds["dedicated-receipt"] += 1
        for rule, _row in rows:
            kinds[rule] += 1
        if entry.operation_id in exemptions:
            reason, claimed = exemptions[entry.operation_id]
            broken = {rule for rule, _row in rows}
            if not broken:
                findings.append(
                    f"{entry.where} is EXEMPT but no longer breaks any rule. A permanent "
                    f"exemption over a clean route is cover for the next regression — "
                    f"delete its entry from {EXEMPTIONS_REL}."
                )
                continue
            if claimed - broken:
                findings.append(
                    f"{entry.where} is EXEMPT for {sorted(claimed - broken)}, which it does "
                    f"NOT break. Either the route was fixed and the entry should shrink, or "
                    f"the RULE was weakened or deleted and this exemption is what is keeping "
                    f"the guard green over its own missing check — look at the rule first."
                )
                continue
            if broken - claimed:
                findings.append(
                    f"{entry.where} breaks {sorted(broken - claimed)}, which its exemption "
                    f"does not list. An exemption excuses exactly the rules it names; add "
                    f"the rule with a reason somebody can argue with, or fix the route."
                )
                continue
            excused.append(f"{entry.where} [{', '.join(sorted(claimed))}] — {reason}")
            continue
        for _rule, row in rows:
            findings.append(f"{entry.where} {row}")

    if findings:
        listing = "\n  ".join(findings)
        fail(
            f"an agent-callable write must answer a RECEIPT, not the thing it just "
            f"wrote:\n  {listing}\n\n"
            f"  Fix the route, not this guard. The shape to copy is any of the "
            f"*ReceiptDTO types already in {SPEC_REL}: name the write's own identity, "
            f"carry the values the SERVER computed (a minted id, a count, a stamp, a "
            f"pending flag) and nothing the caller sent. If a route genuinely must "
            f"answer the full payload, that is an entry in {EXEMPTIONS_REL} with a "
            f"reason — and the reason is the review, so write one somebody can argue "
            f"with."
        )

    breakdown = ", ".join(f"{n} {kind}" for kind, n in sorted(kinds.items()) if n)
    print(
        f"[mcp-write-receipt-guard] all green ({len(scope)} agent-callable write "
        f"operations scanned, {len(excused)} exempt — {breakdown})"
    )
    for row in excused:
        print(f"  exempt: {row}")


if __name__ == "__main__":
    main()
