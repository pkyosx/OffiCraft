// The frontend edge of the ONE status→code table (docs/design/api-error-envelope.codes.json).
//
// Two guards, and neither is about a single wrong string:
//
//   1. `codeForStatus` answers what the spec table says, cell by cell, in both
//      fallback buckets — the same rows server/ocserverd's
//      TestErrorCodeForStatusMatchesSpec pins `errorCodeForStatus` against and
//      conformance/test_error_envelope.py pins CODE_BY_STATUS against. Edit the
//      JSON on its own and the Go side reddens; edit the Go map on its own and
//      it reddens too. Nobody can move one side quietly.
//
//   2. No hand-written source file under frontend/ (src/, visual-guards/,
//      paint-guards/, scripts/ — .ts/.tsx/.js/.mjs/.cjs, build output and
//      node_modules excluded) writes an error code the server cannot emit.
//      Structurally the mock adapter can no longer write one at all (it imports
//      `mockApiError`, not `ApiError`); this catches the hand-built fakes and
//      stories, which the structure cannot reach.
//
// 🔴 WHAT GUARD 2 DOES *NOT* PROMISE. It is a literal-scanner over three
// syntactic shapes, not a type system, and the following were measured to stay
// GREEN while carrying a wrong code:
//
//   a. A code that travels through a variable, constant or template literal.
//      All three scanned positions require a `"..."` literal at the exact spot;
//      `code: SOME_CONST` and `` `${x}` `` are invisible.
//   b. A hand-built envelope that orders the keys `message` before `code`, or
//      that asserts the code through anything other than the two matched
//      forms (`error: { code: "…" }` / `.code).toBe("…")`) — e.g. `toEqual`,
//      `toMatchObject`, `toHaveProperty`, a destructured compare.
//   c. A code that IS in the vocabulary but does not belong to the status it is
//      paired with. Only the `new ApiError(msg, STATUS, "code", …)` shape
//      carries a status the scanner can read, so only THAT shape is
//      status-cross-checked. The envelope shape and the `.toBe` shape are
//      checked against the vocabulary ONLY — a 404 answered with `conflict`
//      passes. (The three real envelope call sites put the status in three
//      different syntactic positions — a positional argument before the body,
//      a `status:` key, a trailing argument after it — so there is no
//      extraction rule that would not misjudge one of them.)
//
// So: this guard pins the closed vocabulary broadly, and status↔code pairing
// narrowly. It is a floor, not a proof.

import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { codeForStatus, ERROR_CODE_VOCABULARY } from "./errorCodes";
import spec from "../../../docs/design/api-error-envelope.codes.json";

const SRC = join(__dirname, "..");
const FRONTEND = join(__dirname, "..", "..");

/** Directories that hold no hand-written source: dependencies and build
 * output. Scanning them is both slow and meaningless — a minified bundle
 * re-states every literal src/ already declares. */
const SKIP_DIRS = new Set([
  "node_modules",
  "dist",
  "dist-ssr",
  "dist-paint-guard",
  "test-results",
  "coverage",
  ".vite",
  ".cache",
  ".git",
]);

function sources(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      sources(join(dir, entry.name), out);
    } else if (/\.(tsx?|mjs|cjs|js)$/.test(entry.name)) {
      out.push(join(dir, entry.name));
    }
  }
  return out;
}

function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}

/** Every place a source file TYPES an error code: the `code` key of a
 * hand-built `{error: {…}}` envelope, a `.code` assertion, and the third
 * `new ApiError(...)` argument. The narrow envelope shape matters — a bare
 * `code:` key also names unrelated vocabularies (backup health, doc-cap save
 * reasons) that this table has no say over. */
function codeLiterals(source: string): Array<{ code: string; status: number | null }> {
  const found: Array<{ code: string; status: number | null }> = [];
  for (const m of source.matchAll(/\berror:\s*\{\s*code:\s*"([a-z_]+)"/g)) {
    found.push({ code: m[1], status: null });
  }
  for (const m of source.matchAll(/\.code\)?\s*\)?\.toBe\("([a-z_]+)"\)/g)) {
    found.push({ code: m[1], status: null });
  }
  for (const m of source.matchAll(/\bnew ApiError\(/g)) {
    const tail = source.slice(m.index! + m[0].length, m.index! + m[0].length + 400);
    const args = tail.match(/,\s*(\d{3}),\s*"([a-z_]+)"/);
    if (args) found.push({ code: args[2], status: Number(args[1]) });
  }
  return found;
}

describe("codeForStatus", () => {
  it("answers the spec table for every mapped status", () => {
    const rows = Object.entries(spec.by_status);
    expect(rows.length).toBeGreaterThan(5);
    for (const [status, code] of rows) {
      expect(codeForStatus(Number(status))).toBe(code);
    }
  });

  it("falls into the spec's two honest buckets for unmapped statuses", () => {
    const mapped = new Set(Object.keys(spec.by_status).map(Number));
    let unmapped5xx = 0;
    let unmapped4xx = 0;
    for (let status = 100; status < 600; status++) {
      if (mapped.has(status)) continue;
      if (status >= 500) {
        expect(codeForStatus(status)).toBe(spec.fallback_5xx);
        unmapped5xx++;
      } else {
        expect(codeForStatus(status)).toBe(spec.fallback_other);
        unmapped4xx++;
      }
    }
    // A mis-rooted loop would pass this vacuously.
    expect(unmapped5xx).toBeGreaterThan(50);
    expect(unmapped4xx).toBeGreaterThan(50);
  });
});

describe("error codes typed in the frontend", () => {
  it("only ever names codes the server can actually emit", () => {
    const files = sources(FRONTEND);
    expect(files.length).toBeGreaterThan(50);
    // The walk must reach the guard trees outside src/ …
    expect(files.some((f) => f.includes("visual-guards"))).toBe(true);
    expect(files.some((f) => f.includes("paint-guards"))).toBe(true);
    expect(files.some((f) => f.endsWith(".mjs"))).toBe(true);
    // … and must not descend into dependencies or build output.
    expect(files.filter((f) => [...SKIP_DIRS].some((d) => f.includes(`/${d}/`)))).toEqual([]);
    const offenders: string[] = [];
    let seen = 0;
    for (const file of files) {
      if (file === __filename) continue;
      for (const hit of codeLiterals(stripComments(readFileSync(file, "utf8")))) {
        seen++;
        if (!ERROR_CODE_VOCABULARY.has(hit.code)) {
          offenders.push(`${file}: "${hit.code}" is not a code the server emits`);
        } else if (hit.status !== null && codeForStatus(hit.status) !== hit.code) {
          offenders.push(
            `${file}: http ${hit.status} carries "${codeForStatus(hit.status)}", not "${hit.code}"`
          );
        }
      }
    }
    // A dead regex would pass this vacuously.
    expect(seen).toBeGreaterThan(0);
    expect(offenders).toEqual([]);
  });

  it("leaves the mock adapter unable to type a code at all", () => {
    const mock = readFileSync(join(SRC, "api/mock.ts"), "utf8");
    expect(mock).toContain('import { mockApiError } from "./errorCodes"');
    expect(stripComments(mock)).not.toContain("new ApiError(");
  });
});
