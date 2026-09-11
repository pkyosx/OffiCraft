// The size/cap fields on a task manual, on the HTTP seam (T-100).
//
// 🔴 WHY THIS FILE EXISTS AS A SEPARATE THING FROM THE PAGE TEST. The page test
// (`TaskManualsPage.usage.test.tsx`) reaches the readout through `SettingsPage`
// and the MOCK adapter — and the mock builds its views itself, so it never
// calls these mappers at all. A mutant that blanked `sopMdChars` here left that
// file completely green. `toTaskManualSummary` is the ONLY thing standing
// between the wire and the readout on the real seam (http.ts calls it for the
// list and `toTaskManual` for every single-manual read and write echo), and it
// spent its whole life until this ticket deliberately DROPPING these fields —
// so "nothing tests it" is not a hypothetical here, it is the state the code
// was found in.

import { describe, it, expect } from "vitest";
import { toTaskManual, toTaskManualSummary } from "./mappers";
import type { WireTaskManual, WireTaskManualListItem } from "./wire";

/** A wire manual carrying DISTINCT numbers. Distinct on purpose: with equal
 * sizes or equal caps, a mapper that read one pair into the other's fields
 * would be indistinguishable from a correct one. */
const WIRE: WireTaskManual = {
  type_key: "tm-usage",
  display_name: "審查 PR",
  purpose: "",
  fields: [],
  sop_md: "SOP",
  assignee: {},
  updated_ts: 0,
  sop_md_chars: 15796,
  sop_md_cap_chars: 18000,
  // The lore pair. Distinct numbers again for the same reason: lore rides its
  // own field (owner ruling 2026-09-07), and a mapper crossing it with the SOP
  // pair would be indistinguishable from a correct one if the numbers matched.
  lore: "# 傳承",
  lore_chars: 5,
};

describe("toTaskManual / toTaskManualSummary · the size + cap pair", () => {
  it("carries both numbers through, each into its own field", () => {
    const v = toTaskManual(WIRE);
    expect(v.sopMdChars).toBe(15796);
    expect(v.sopMdCapChars).toBe(18000);
  });

  it("carries them on the DIRECTORY row too, which omits the document", () => {
    // The list answer drops sop_md but keeps its measurements — that asymmetry
    // is the whole reason the wire has the size fields, and a mapper that only
    // handled the full read would leave the list rows blank.
    const { sop_md: _s, ...row } = WIRE;
    const v = toTaskManualSummary(row as WireTaskManualListItem);
    expect(v.sopMdChars).toBe(15796);
    expect(v.sopMdCapChars).toBe(18000);
  });

  it("floors a field an older server omits at 0, rather than inventing a cap", () => {
    // The generated schema marks both required and today's server always emits
    // them, so this case is reachable only from a server that predates them —
    // which is exactly why the mapper keeps `?? 0` and why the cast below is
    // honest rather than a way around the type. 0 is the "not known" value the
    // readout is gated on; substituting a default would draw a real budget that
    // nothing enforces.
    const { sop_md_cap_chars: _c, ...thin } = WIRE;
    const v = toTaskManual(thin as WireTaskManual);
    expect(v.sopMdCapChars).toBe(0);
    // The one that WAS sent still arrives — a blanket 0 would pass the
    // assertion above.
    expect(v.sopMdChars).toBe(15796);
  });
});
