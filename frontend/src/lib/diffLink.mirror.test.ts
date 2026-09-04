// The cockpit's half of the comparison-address mirror confrontation (T-59).
//
// `server/ocserverd/diffaddr.go` is THE AUTHORITY on how one side of a
// comparison is spelled. `cli/ocagent/diff.go` carries a pre-flight copy, and
// `lib/diffLink.ts` carries a reader's copy — three transcriptions of one rule,
// in three languages, with no import path between any two of them.
//
// The two Go copies are already driven against
// bin/tests/fixtures/diff-side-addresses.tsv rather than against each other, so
// a drift reddens the copy that drifted BY NAME. This file puts the cockpit's
// copy on the same table, which is why that fixture's header names all THREE
// copies as driven from it.
//
// THE SECOND CONFRONTATION below is the URL GRAMMAR — the page path and the
// five parameter names. Those are not in the fixture and cannot be: they are
// the server's own constants, and the cockpit both builds a url from them
// (lib/diffLink.ts) and asks the data route with them (api/diff.ts). So they
// are checked against `server/ocserverd/api_diff.go`'s SOURCE, exactly as
// cli/ocagent/diff_mirror_test.go checks its own copy — a language boundary is
// precisely what these tests exist to reach across.
//
// 🔴 A MISSING OR UNREADABLE FIXTURE OR SOURCE IS A FAILURE, NEVER A SKIP. A
// mirror guard that quietly passes when it cannot find its own table means
// nothing was checked, which is worse than no guard at all.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  DIFF_PATH,
  DIFF_PARAM_AFTER,
  DIFF_PARAM_BEFORE,
  DIFF_PARAM_LABEL_AFTER,
  DIFF_PARAM_LABEL_BEFORE,
  DIFF_PARAM_SIG,
  parseDiffSideAddress,
} from "./diffLink";
import { diffQuery } from "../api/diff";

// Resolved from the vitest ROOT (frontend/), not from import.meta.url: the
// suite runs the transformed module, whose URL is not a file: one.
const FIXTURE = resolve(process.cwd(), "../bin/tests/fixtures/diff-side-addresses.tsv");

interface Row {
  line: number;
  address: string;
  ok: boolean;
  about: string;
}

function rows(): Row[] {
  const text = readFileSync(FIXTURE, "utf8");
  const out: Row[] = [];
  text.split("\n").forEach((raw, i) => {
    const line = i + 1;
    if (raw.trim() === "" || raw.startsWith("#")) return;
    const cells = raw.split("\t");
    if (cells.length !== 3) {
      throw new Error(`${FIXTURE}:${line}: expected 3 tab-separated cells, got ${cells.length}`);
    }
    const [address, verdict, about] = cells;
    if (verdict !== "ok" && verdict !== "bad") {
      throw new Error(`${FIXTURE}:${line}: verdict must be ok|bad, got ${verdict}`);
    }
    // The table spells a literal space as <space>, because a bare one between
    // two tabs is too easy for an editor to eat — and the padded rows are the
    // ones whose exactness matters most.
    out.push({ line, address: address.split("<space>").join(" "), ok: verdict === "ok", about });
  });
  return out;
}

describe("parseDiffSideAddress", () => {
  const table = rows();

  it("reads a table with rows in it", () => {
    // A fixture that parsed to nothing would make every assertion below vacuous.
    expect(table.length).toBeGreaterThan(20);
  });

  it.each(table.map((r) => [r.line, r.address, r.ok, r.about] as const))(
    "line %i: %s → %s (%s)",
    (line, address, ok) => {
      expect(parseDiffSideAddress(address) !== null, `${FIXTURE}:${line}`).toBe(ok);
    },
  );
});

// The page path and the five parameter names, confronted against the server's
// own declarations. `cli/ocagent` runs the identical check from Go; without
// this one, renaming `label_before` in the spec and the server would redden the
// CLI's mirror while the cockpit went on asking for the old name — and the test
// that pins the query would stay green, because it asserted the same
// hand-written string.
describe("the compare url's spelling", () => {
  const SERVER_SOURCE = resolve(process.cwd(), "../server/ocserverd/api_diff.go");

  const source = (): string => {
    const text = readFileSync(SERVER_SOURCE, "utf8");
    // gofmt pads a const block into columns, so the comparison is made on a
    // whitespace-normalised copy rather than on the bytes.
    return text.split(/\s+/).join(" ");
  };

  it.each([
    ["diffPagePath", DIFF_PATH],
    ["diffParamBefore", DIFF_PARAM_BEFORE],
    ["diffParamAfter", DIFF_PARAM_AFTER],
    ["diffParamLabelBefor", DIFF_PARAM_LABEL_BEFORE],
    ["diffParamLabelAfter", DIFF_PARAM_LABEL_AFTER],
    ["diffParamSig", DIFF_PARAM_SIG],
  ])("is the server's own %s", (name, value) => {
    expect(source(), `${SERVER_SOURCE} must declare ${name} = "${value}"`).toContain(
      `${name} = "${value}"`,
    );
  });

  it("is what the compare READ asks for, not a second hand-written copy", () => {
    // api/diff.ts used to spell the five words again. It now builds its query
    // from the constants above, so this asserts the two are one grammar.
    const query = new URLSearchParams(
      diffQuery({
        before: "att-0123456789ab",
        after: "att-ba9876543210",
        labelBefore: "a",
        labelAfter: "b",
        sig: "s",
      }),
    );
    expect([...query.keys()].sort()).toEqual(
      [
        DIFF_PARAM_BEFORE,
        DIFF_PARAM_AFTER,
        DIFF_PARAM_LABEL_BEFORE,
        DIFF_PARAM_LABEL_AFTER,
        DIFF_PARAM_SIG,
      ].sort(),
    );
  });
});
