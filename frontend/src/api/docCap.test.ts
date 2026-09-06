// The cockpit's half of the document-cap mirror confrontation (T-7d33). The
// twin is server/ocserverd/doc_cap_mirror_test.go; the table both read is
// bin/tests/fixtures/doc-cap-cases.tsv, and the reasoning lives in its header.
//
// The short version: DocCapBlocked (Go) is the authority that refuses a write;
// docCapBlocked (TS) is a temporary copy that lets the cockpit grey out a
// revision BEFORE the owner clicks it. A drift between them raises no error
// anywhere — it just makes the cockpit lie. So neither side is asserted against
// the other (a mock would only prove the mock agrees with itself); both are
// asserted against the committed table.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import {
  DOC_CAP_CHARS_DEFAULT,
  docCapBlocked,
  runeLength,
  shownDocSize,
} from "./docCap";

const CASES_PATH = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "bin",
  "tests",
  "fixtures",
  "doc-cap-cases.tsv"
);

interface CapRow {
  line: number;
  name: string;
  before: number;
  after: number;
  blocked: boolean;
  fill: string;
}

/** Parse the shared table. An unreadable/short fixture THROWS — a guard that
 * goes green when it could not read its fixture is a lie. */
function loadCases(): { rows: CapRow[]; cap: number } {
  const raw = readFileSync(CASES_PATH, "utf8");
  const rows: CapRow[] = [];
  let cap = 0;
  raw.split("\n").forEach((line, i) => {
    const n = i + 1;
    if (line.startsWith("# cap\t")) {
      cap = Number(line.slice("# cap\t".length).trim());
      return;
    }
    const trimmed = line.trim();
    if (trimmed === "" || trimmed.startsWith("#")) return;
    const cols = line.split("\t");
    if (cols.length !== 5) {
      throw new Error(
        `${CASES_PATH}:${n}: want 5 tab-separated columns, got ${cols.length}: ${line}`
      );
    }
    if (cols[0] === "case") return; // the header row
    // Counted HERE with a literal spread, never through runeLength: the parser
    // must not lean on the function under test, or swapping its unit turns a
    // row failure into a fixture-load crash and the rows never run at all.
    if ([...cols[4]].length !== 1) {
      throw new Error(
        `${CASES_PATH}:${n}: fill must be exactly ONE code point, got ${cols[4]}`
      );
    }
    rows.push({
      line: n,
      name: cols[0],
      before: Number(cols[1]),
      after: Number(cols[2]),
      blocked: cols[3] === "true",
      fill: cols[4],
    });
  });
  if (!Number.isInteger(cap) || cap === 0) {
    throw new Error(`${CASES_PATH} carries no \`# cap<TAB><n>\` line`);
  }
  if (rows.length < 5) {
    throw new Error(`${CASES_PATH} carries ${rows.length} rows — too few`);
  }
  return { rows, cap };
}

const { rows, cap } = loadCases();

describe("docCapBlocked · the shared cap table", () => {
  it("caps at the size the shared table names", () => {
    // The threshold is ON the table, so the number is not a third copy of
    // itself — the table moves when the shipped default moves.
    expect(DOC_CAP_CHARS_DEFAULT).toBe(cap);
  });

  it.each(rows.map((r) => [r.name, r] as const))(
    "%s agrees with the shared table",
    (_name, r) => {
      const before = r.fill.repeat(r.before);
      const after = r.fill.repeat(r.after);
      // A drift here means the cockpit and server/ocserverd's DocCapBlocked now
      // disagree about which revisions are restorable — the cockpit would grey
      // out a good revision, or offer one the server refuses with a 400.
      expect(docCapBlocked(cap, before, after)).toBe(r.blocked);
    }
  );

  it("carries a multi-byte row, or the rune-vs-byte unit is untested", () => {
    // Without one, swapping [...s].length for s.length (UTF-16 units) or a byte
    // count passes silently — the exact defect those rows exist to catch, so
    // their ABSENCE must fail too.
    expect(rows.some((r) => r.fill.length > 1 || r.fill.charCodeAt(0) > 127)).toBe(
      true
    );
  });

  it("counts code points, not UTF-16 units", () => {
    // The property the table's astral rows encode, stated directly: one emoji
    // is ONE character to this rule and two to String.length.
    expect(runeLength("🙂")).toBe(1);
    expect("🙂".length).toBe(2);
  });
});

// ── shownDocSize (T-100) ────────────────────────────────────────────────────
//
// The arithmetic behind every 「已用 / 上限」 readout in the cockpit. It moved
// out of DocCard when the two task-manual editors grew the same readout without
// being DocCards: two copies of this is how one of them ends up counting the
// saved document while the other counts the draft, and nothing would say so.
describe("shownDocSize · what a usage readout must show", () => {
  it("reports the STORED size when there is no draft", () => {
    // `null` is a state, not a missing value: nothing is being edited, so the
    // stored size is the honest answer.
    expect(shownDocSize(42, "whatever the stored text is", null)).toBe(42);
  });

  it("reports the DRAFT while editing, not the stored size", () => {
    // The whole reason the function exists. A readout that answered 9 here
    // would move only AFTER the write its reader was trying to decide about.
    // (8 = the rune count of the stored text, so there is no read-only head
    // here; the head term has its own case below.)
    expect(shownDocSize(8, "九個字元的舊內容", "新的")).toBe(2);
  });

  it("adds back the part of the document the editor does not hold", () => {
    // A document with a read-only head: the editor holds `body` (4 runes) while
    // the server stores 10, so the head is 6 and the cap is on all 10. Dropping
    // that term promises room the server will refuse.
    expect(shownDocSize(10, "abcd", "abcdefgh")).toBe(14);
  });

  it("never subtracts when the stored size is SMALLER than the text held", () => {
    // Can happen transiently — an adopted write echo lands before the stored
    // size does. Flooring at 0 keeps the readout merely stale for one frame
    // instead of showing a number smaller than what is on screen.
    expect(shownDocSize(2, "a much longer stored text", "abc")).toBe(3);
  });

  it("counts CODE POINTS, matching utf8.RuneCountInString on the server", () => {
    // `String.length` would say 4 for two astral characters and would show a
    // budget the server never judges anything by.
    expect(shownDocSize(0, "", "🙂🙂")).toBe(2);
  });
});
