// 🔴 THE ASYMMETRY BETWEEN `is_seed` AND `has_seed` IS DELIBERATE.
// READ THIS BEFORE "MAKING THE TWO MAPPERS CONSISTENT" — the inconsistency is
// the correct behaviour, and unifying it breaks the cockpit in one direction.
//
// Two neighbouring mappers default a missing seed-ish boolean the OPPOSITE way:
//
//   toRoleDef:  is_seed  missing → TRUE
//   toInsight:  has_seed missing → FALSE
//
// They look like the same kind of field and they are not. What decides the safe
// default is WHAT THE UI DOES WITH IT — and the two fields drive OPPOSITE
// affordances, so "safe" points opposite ways:
//
//   * `is_seed` gates DELETE. A seed role cannot be deleted (the server 403s).
//     Guessing TRUE means "assume this is a built-in, do NOT offer delete" —
//     the cautious answer. Guessing FALSE would offer a destructive button on a
//     role the server refuses to delete.
//   * `has_seed` gates the 預設內容 RESET row (T-6501). Guessing FALSE means
//     "cannot prove a factory version exists, so offer nothing" — the cautious
//     answer. Guessing TRUE would draw a row that 404s on every role with no
//     seeds/insight_<role_key>.md, which is every role but `assistant`.
//
// So both defaults are fail-safe; they simply differ because one field enables
// a DESTRUCTIVE action and the other enables a RECOVERY action. Anyone who
// "unifies" them to true resurrects a dead affordance in the cockpit, and the
// symptom is a confirm dialog that ends in a 404 the user cannot act on.
//
// These defaults only fire against a server too old to send the field, which is
// why nothing else in the suite can see them: every fixture and the mock send
// both fields. That invisibility is exactly why the rule needs a test rather
// than the comment it had before — a comment does not go red.

import { describe, it, expect } from "vitest";
import { toInsight, toRoleDef } from "./mappers";
import type { WireInsight, WireRoleDef } from "./wire";

// An OLD server's payload: every field this cockpit's generated types demand,
// MINUS the one under test. The cast is the point of the fixture — the wire
// types describe today's server, and the whole question here is what happens
// when the response predates the field.
function insightWithoutHasSeed(): WireInsight {
  return {
    size_chars: 3,
    cap_chars: 15000,
    role_key: "r-legacy",
    text: "old",
    owner_id: "owner",
    schema_version: 3,
    is_default: false,
  } as unknown as WireInsight;
}

function roleDefWithoutIsSeed(): WireRoleDef {
  return {
    size_chars: 3,
    cap_chars: 1000,
    key: "r-legacy",
    name: "Legacy",
    definition_md: "old",
    owner_id: "owner",
    schema_version: 3,
    is_default: false,
  } as unknown as WireRoleDef;
}

describe("mappers · the seed-flag defaults are deliberately OPPOSITE", () => {
  it("toInsight defaults a MISSING has_seed to FALSE (offer no reset we cannot prove)", () => {
    expect(toInsight(insightWithoutHasSeed()).hasSeed).toBe(false);
  });

  it("toRoleDef defaults a MISSING is_seed to TRUE (offer no delete we cannot prove is safe)", () => {
    expect(toRoleDef(roleDefWithoutIsSeed()).isSeed).toBe(true);
  });

  // The pair, stated as one fact so that "unifying" the two defaults cannot
  // pass by flipping both. A single test per field would still be green for
  // someone who changed BOTH to true — which is the exact edit this file exists
  // to stop.
  it("the two defaults must stay OPPOSITE — see this file's header before changing either", () => {
    const insightDefault = toInsight(insightWithoutHasSeed()).hasSeed;
    const roleDefault = toRoleDef(roleDefWithoutIsSeed()).isSeed;
    expect(insightDefault).not.toBe(roleDefault);
    // Named, not merely different: "they differ" would also hold if the two
    // were swapped, and swapped is the harmful arrangement (a reset row that
    // 404s, plus a delete button on a built-in role).
    expect({ insightDefault, roleDefault }).toEqual({
      insightDefault: false,
      roleDefault: true,
    });
  });

  // POSITIVE CONTROL: the defaults must not be swallowing a value the server
  // DID send. Without this, a mapper that ignored the wire entirely and always
  // returned the fail-safe constant would pass everything above.
  it("a field the server DOES send wins over the default, both ways", () => {
    expect(
      toInsight({ ...insightWithoutHasSeed(), has_seed: true }).hasSeed
    ).toBe(true);
    expect(
      toRoleDef({ ...roleDefWithoutIsSeed(), is_seed: false }).isSeed
    ).toBe(false);
  });
});
