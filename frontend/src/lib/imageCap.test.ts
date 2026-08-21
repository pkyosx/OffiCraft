// The cockpit's half of the theme-bundle image-cap mirror confrontation
// (T-72da). The twin is server/ocserverd/image_cap_mirror_test.go; the table
// both read is bin/tests/fixtures/image-cap-cases.tsv, and the reasoning lives
// in its header.
//
// The short version: validImageValue (Go) is the authority that refuses a theme
// bundle with a 422; isValidImageValue (TS) is the cockpit's copy, so a picked
// file is refused at the file picker instead of after a round trip. A drift
// between them raises no error anywhere — it just makes the cockpit lie, in one
// direction (refusing an image the server would take) or the other (taking one
// the server will refuse). So neither side is asserted against the other (a mock
// would only prove the mock agrees with itself); both are asserted against the
// committed table.
//
// The rows are driven through the real validateAvatars / validateAvatarPools /
// validateLogo /
// validateNavIcons / validateBackgrounds entry points rather than the bare gate,
// so a cap that is raised but wired to the wrong call site still fails here.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import {
  MAX_AVATAR_BYTES,
  MAX_AVATAR_VALUE_LEN,
  MAX_BACKGROUND_BYTES,
  MAX_BACKGROUND_VALUE_LEN,
  validateAvatars,
  validateAvatarPools,
  validateLogo,
  validateNavIcons,
  validateBackgrounds,
} from "./themeBundleCore";

const CASES_PATH = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "bin",
  "tests",
  "fixtures",
  "image-cap-cases.tsv"
);

const DECL_NAMES = [
  "avatar_bytes",
  "avatar_value_len",
  "background_bytes",
  "background_value_len",
] as const;
type DeclName = (typeof DECL_NAMES)[number];

interface CapRow {
  line: number;
  name: string;
  purpose: string;
  decoded: number;
  accepted: boolean;
}

/** Parse the shared table. An unreadable/short fixture THROWS — a guard that
 * goes green when it could not read its fixture is a lie. */
function loadCases(): { rows: CapRow[]; decls: Record<DeclName, number> } {
  const raw = readFileSync(CASES_PATH, "utf8");
  const rows: CapRow[] = [];
  const decls = {} as Record<DeclName, number>;
  raw.split("\n").forEach((line, i) => {
    const n = i + 1;
    const tab = line.indexOf("\t");
    if (line.startsWith("# ") && tab >= 0) {
      const name = line.slice(2, tab).trim() as DeclName;
      if ((DECL_NAMES as readonly string[]).includes(name)) {
        decls[name] = Number(line.slice(tab + 1).trim());
        return;
      }
    }
    const trimmed = line.trim();
    if (trimmed === "" || trimmed.startsWith("#")) return;
    const cols = line.split("\t");
    if (cols.length !== 4) {
      throw new Error(
        `${CASES_PATH}:${n}: want 4 tab-separated columns, got ${cols.length}: ${line}`
      );
    }
    if (cols[0] === "case") return; // the header row
    rows.push({
      line: n,
      name: cols[0],
      purpose: cols[1],
      decoded: Number(cols[2]),
      accepted: cols[3] === "true",
    });
  });
  for (const name of DECL_NAMES) {
    if (!Number.isInteger(decls[name]) || !decls[name]) {
      throw new Error(`${CASES_PATH} carries no \`# ${name}<TAB><n>\` line`);
    }
  }
  if (rows.length < 8) {
    throw new Error(`${CASES_PATH} carries ${rows.length} rows — too few`);
  }
  return { rows, decls };
}

const { rows, decls } = loadCases();

const PNG_SIGNATURE = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];

/** A `data:image/png;base64,...` URI whose DECODED length is exactly n bytes and
 * which begins with the PNG signature, so the magic-byte check (orthogonal to
 * size) keeps passing and a row can only go red for the reason it is testing. */
function pngURIOfSize(n: number): string {
  const bytes = new Uint8Array(n);
  bytes.set(PNG_SIGNATURE);
  // Chunked: spreading half a megabyte into String.fromCharCode blows the
  // argument limit.
  let s = "";
  for (let i = 0; i < bytes.length; i += 8192) {
    s += String.fromCharCode(...bytes.subarray(i, i + 8192));
  }
  return `data:image/png;base64,${btoa(s)}`;
}

/** Run one value through the REAL bundle-field validator for that purpose.
 * Returns null when the field admits the value. */
function feedPurpose(purpose: string, value: string): string | null {
  switch (purpose) {
    case "avatar":
      // A SINGLETON kind — see the Go twin: T-cd6f moved member / outsource
      // images into avatarPools, so "member" would fail on the kind and never
      // reach the cap.
      return validateAvatars({ assistant: value });
    case "avatarpool":
      return validateAvatarPools({ member: [value] });
    case "logo":
      return validateLogo(value);
    case "navicon":
      return validateNavIcons({ office: value });
    case "background":
      return validateBackgrounds({ canvas: value });
    default:
      throw new Error(
        `unknown purpose "${purpose}" — the table and this switch have drifted`
      );
  }
}

describe("theme-bundle image caps · the shared cap table", () => {
  // The four thresholds are ON the table, so these constants are not a third
  // copy of themselves. Each names ITSELF, so a drift says which of the four
  // moved and on which side.
  it.each([
    ["MAX_AVATAR_BYTES", MAX_AVATAR_BYTES, "avatar_bytes"],
    ["MAX_AVATAR_VALUE_LEN", MAX_AVATAR_VALUE_LEN, "avatar_value_len"],
    ["MAX_BACKGROUND_BYTES", MAX_BACKGROUND_BYTES, "background_bytes"],
    [
      "MAX_BACKGROUND_VALUE_LEN",
      MAX_BACKGROUND_VALUE_LEN,
      "background_value_len",
    ],
  ] as const)("%s is the size the shared table names", (name, got, key) => {
    // A failure here means: COCKPIT SIDE DRIFTED —
    // frontend/src/lib/themeBundleCore.ts's ${name} no longer matches the shared
    // table, while server/ocserverd/avatar_bundle.go still follows it. The two
    // now disagree about which images are accepted.
    expect(got, `COCKPIT SIDE DRIFTED: themeBundleCore.ts ${name}`).toBe(
      decls[key]
    );
  });

  it("keeps the two purposes apart, or these rows prove nothing", () => {
    // The whole point of T-72da is that the two purposes DIFFER. If a later
    // tidy-up collapses them back into one number, every row below still passes
    // for one of the two purposes — so the table's own premise must fail loudly.
    expect(decls.avatar_bytes).not.toBe(decls.background_bytes);
  });

  it.each(rows.map((r) => [r.name, r] as const))(
    "%s agrees with the shared table",
    (_name, r) => {
      const err = feedPurpose(r.purpose, pngURIOfSize(r.decoded));
      // A drift here means the cockpit and the server's image gate now disagree
      // about which images are accepted — the cockpit would refuse a background
      // the server takes, or offer one the server refuses with a 422.
      expect(
        err === null,
        `${CASES_PATH}:${r.line} ${r.name}: ${r.purpose} field ${
          err === null ? "ACCEPTED" : "REFUSED"
        } a ${r.decoded}-byte image (err=${err})`
      ).toBe(r.accepted);
    }
  );

  it("carries rows proving the split in BOTH directions", () => {
    // Without both, the table could not tell a split gate from a single one:
    // only-relaxed rows pass with every cap raised to 512 KiB (avatars relaxed
    // too — the exact thing the split exists to prevent), and only-refused rows
    // pass with nothing relaxed at all.
    const pastAvatarCap = rows.filter((r) => r.decoded > decls.avatar_bytes);
    expect(
      pastAvatarCap.some((r) => r.accepted),
      "no row proves the background cap was actually RAISED"
    ).toBe(true);
    expect(
      pastAvatarCap.some((r) => !r.accepted),
      "no row proves a GLYPH field still refuses a background-sized image"
    ).toBe(true);
  });
});
