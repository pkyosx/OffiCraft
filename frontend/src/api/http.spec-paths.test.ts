// Every `/api/...` path this adapter spells must exist in the frozen spec
// (T-791e).
//
// 🔴 WHY THIS EXISTS. The boot-context routes shipped with three hand-typed
// path strings on a bare fetch — `/api/system-interaction/${key}` — against a
// backend that serves `/api/system-interaction` with no key segment. Nothing
// went red: tsc cannot see inside a template string, and every test in the
// suite runs on a mock adapter that answers whatever path it is handed. The
// failure only exists against the real server, as a runtime 404, on a screen
// whose whole job is editing the documents that decide whether agents boot.
//
// The typed client removes that class for the calls that ride it, but it
// removes it only for as long as they ride it: the next route that lands
// before the spec catches up gets the same bare-fetch escape hatch and the
// same silence. This assertion is what stays red in that window — it reads the
// path strings out of http.ts itself and requires each one to be a path the
// spec actually declares.
//
// It is deliberately a source scan, not a call-site scan: a path that is
// written down but not yet called is exactly the shape that ships broken.
//
// ── WHAT THE SCAN COVERS, AND WHAT IT STILL DOES NOT ────────────────────────
// The first version of this file read ONLY double-quoted literals
// (`/"\/api\/[^"]*"/g`), which is to say it could not see the exact shape of
// the accident it was written for. A template string sailed through it. So the
// scan now has two halves, and each half is anchored by an assertion below:
//
//   COVERED (a) double-quoted literals — `"/api/system-interaction"` — matched
//     verbatim against the declared paths. Unchanged from the first version.
//   COVERED (b) backtick template literals whose static text is path-shaped —
//     `` `/api/boot-sequence/${runtimeKey}` `` . Each `${…}` interpolation
//     normalises to the spec's own parameter notation and the whole string is
//     matched against the declared paths under that same normalisation, so
//     `/api/boot-sequence/{runtime_key}` is a hit and the original accident,
//     `/api/system-interaction/${key}`, is not. A `?query` or `#hash` tail is
//     trimmed before matching (the SSE downlink carries `?token=`).
//
// ── WHY THE FIXED-SAMPLE ANCHOR BELOW EXISTS ────────────────────────────────
// The interpolation half of this scan (COVERED (b) above) has exactly one
// positive sample in http.ts today — the SSE downlink, `/api/events` — and
// that sample carries no parameterised path segment — its only `${…}` sits
// in the `?token=` query tail, which is trimmed before matching (and is,
// today, the sole real sample exercising that trim). Nothing here proves PATH_SHAPE
// still recognises a *parameterised* template as path-shaped once it is
// normalised. If PATH_SHAPE is ever tightened enough to filter out a real
// parameterised route (e.g. by requiring the whole string to be non-empty
// after stripping PARAM, or by rejecting PARAM adjacent to another PARAM,
// or any other narrowing that looks like a reasonable shape check), every
// template in http.ts could quietly stop being scanned and the assertion in
// "names only paths the frozen spec declares, interpolations included"
// would still pass — vacuously, since `used` would just be smaller. The
// one-off "six real routes rewritten as templates must not go red" check
// that surfaced this gap during review is not a fixture that stays on the
// tree, so it buys no protection tomorrow.
//
// The block below is that guard, made permanent: it feeds FIXED, hand-typed
// sample strings (not read out of http.ts) — one parameterised
// (`` `/api/boot-sequence/${runtimeKey}` ``) and one bare
// (`/api/system-interaction`) — through the exact same
// extract → normalise → PATH_SHAPE-filter pipeline the real scan uses, and
// requires both to come out recognised as spec-declared. It is falsifiable
// in one specific way: if PATH_SHAPE (or the extraction/normalisation
// around it) is ever written strict enough to drop a parameterised real
// path, THIS test goes red on its own line, independent of whatever
// http.ts happens to spell that day.
//
// NOT COVERED, stated so the next reader can falsify it rather than trust it.
// Each of these can be pasted into http.ts and this file stays GREEN — all
// three were measured, not reasoned about:
//   • a path whose `/api/` prefix is never spelled adjacent to the route, so
//     no `/api/<something>` string exists to read at all:
//         const base = "/api";
//         const p = `${base}/${kind}/reset`;
//     This is the residual, and it is the shape both permanently hand-written
//     seams already have: `authedAttachmentUrl` appends a token to a URL it is
//     handed and never spells a route.
//     ⚠️ The neighbouring shape `"/api/" + kind` is NOT in this hole — it goes
//     red, because the fragment `"/api/"` is itself a quoted `/api/…` literal
//     and the spec declares no such path. Do not generalise "concatenation
//     escapes the scan"; only losing the prefix does.
//   • a template whose static text is not path-shaped (spaces, `<id>`, an
//     interpolation containing a `}`) — it is skipped rather than guessed at,
//     which is also why prose in a comment does not produce a false red on
//     this half; a double-quoted `/api/…` in a comment is read like any
//     other literal and will go red. That misfire is scoped to this one
//     file — the scan only reads http.ts, so the same fake path planted in
//     a comment in another file (e.g. mock.ts) leaves this test green.
//   • a path spelled in any file other than http.ts. This scan reads one file.
// Closing any of those means widening the scan, not widening this paragraph.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const FE_ROOT = resolve(__dirname, "../..");

const httpSource = readFileSync(resolve(FE_ROOT, "src/api/http.ts"), "utf8");

const spec = JSON.parse(
  readFileSync(resolve(FE_ROOT, "../spec/openapi.json"), "utf8")
) as { paths: Record<string, unknown> };

/** Paths this adapter spells that the OpenAPI contract does NOT declare, each
 * with the reason it is outside the contract. A WRITTEN-DOWN ROLL CALL, not a
 * pattern: the only way past this assertion is to edit this list, and that
 * edit is the review it is asking for. Adding a route here means claiming it
 * can never be typed — say why. */
const OUTSIDE_THE_CONTRACT = new Map<string, string>([
  // Empty, and that is the current fact rather than an oversight: every
  // `/api/...` string this file spells today — quoted or interpolated — is a
  // declared path. The SSE downlink is hand-written and will stay that way
  // (EventSource cannot carry a header), but `/api/events` IS declared, so it
  // needs no exemption; it only needed the scan to stop being blind to the
  // template string it is written as.
]);

/** The spec writes a path parameter as `{runtime_key}`; a template string
 * writes the same segment as `${runtimeKey}`. Both mean "one segment supplied
 * at call time", so both collapse to one token and are compared under it.
 * Note what this deliberately does NOT do: it collapses the parameter, not the
 * path. `/api/boot-sequence/${x}/nope` normalises to
 * `/api/boot-sequence/{param}/nope`, which the spec does not declare — a
 * legal-looking parameter position does not buy a route. */
const PARAM = "{param}";

function normalizeDeclared(path: string): string {
  return path.replace(/\{[^{}]*\}/g, PARAM);
}

/** A normalised template must still look like a path. Anything else (prose,
 * spaces, an interpolation we could not parse) is not something this scan can
 * judge, so it is skipped rather than reported as unknown. */
const PATH_SHAPE = new RegExp(
  `^/api/[A-Za-z0-9._~/-]*(?:${PARAM.replace(/[{}]/g, "\\$&")}[A-Za-z0-9._~/-]*)*$`
);

function pathLiterals(source: string): string[] {
  return [...new Set(source.match(/"\/api\/[^"]*"/g) ?? [])].map((s) =>
    s.slice(1, -1)
  );
}

function templatePaths(source: string): string[] {
  const raw = source.match(/`\/api\/[^`]*`/g) ?? [];
  const out = raw
    .map((s) => s.slice(1, -1))
    .map((s) => s.split(/[?#]/, 1)[0])
    .map((s) => s.replace(/\$\{[^{}]*\}/g, PARAM))
    .filter((s) => PATH_SHAPE.test(s));
  return [...new Set(out)];
}

describe("httpApi path strings", () => {
  it("names only paths the frozen spec declares", () => {
    const used = pathLiterals(httpSource);
    // Anti-vacuity: a regex that stopped matching would leave an empty set and
    // an empty set satisfies every assertion below. The adapter covers most of
    // the surface, so the real number is far above this floor.
    expect(used.length).toBeGreaterThan(50);
    expect(Object.keys(spec.paths).length).toBeGreaterThan(50);

    const declared = new Set(Object.keys(spec.paths));
    const unknown = used.filter(
      (p) => !declared.has(p) && !OUTSIDE_THE_CONTRACT.has(p)
    );
    expect(unknown).toEqual([]);

    // An exemption that outlived its call site is how a roll call rots into a
    // list nobody reads: it keeps granting permission for a path that is gone.
    const stale = [...OUTSIDE_THE_CONTRACT.keys()].filter(
      (p) => !used.includes(p) && !templatePaths(httpSource).includes(p)
    );
    expect(stale).toEqual([]);
  });

  it("names only paths the frozen spec declares, interpolations included", () => {
    const used = templatePaths(httpSource);
    // Anti-vacuity, and it names a route rather than counting: templates are
    // rare here by design (the typed client spells its paths as quoted
    // literals), so a floor of "more than N" would be a floor of zero or one
    // either way. The SSE downlink is the one path that MUST stay hand-written
    // — EventSource cannot carry an Authorization header — so it is the anchor
    // that goes red if this extractor ever stops matching.
    expect(used).toContain("/api/events");

    const declared = new Set(Object.keys(spec.paths).map(normalizeDeclared));
    const unknown = used.filter(
      (p) => !declared.has(p) && !OUTSIDE_THE_CONTRACT.has(p)
    );
    expect(unknown).toEqual([]);
  });

  it("spells each boot-context route the way the backend serves it", () => {
    // The six routes this ticket added, asserted one by one rather than folded
    // into the sweep above: the sweep proves each string is SOME declared
    // path, and `/api/system-interaction/global` failing that is the whole
    // point — but only naming them individually says which six must be there,
    // so deleting a call site cannot quietly shrink the coverage.
    const used = new Set(pathLiterals(httpSource));
    for (const route of [
      "/api/system-interaction",
      "/api/system-interaction/reset",
      "/api/boot-sequence/{runtime_key}",
      "/api/boot-sequence/{runtime_key}/reset",
      "/api/document-history/{kind}/{key}",
      "/api/document-history/{kind}/{key}/{id}/restore",
    ]) {
      expect(used, `http.ts no longer spells ${route}`).toContain(route);
      expect(spec.paths, `spec does not declare ${route}`).toHaveProperty([
        route,
      ]);
    }
  });

  it("recognises fixed parameterised and bare route samples as spec-declared, independent of http.ts", () => {
    // These strings are hand-typed here, not read out of http.ts. If PATH_SHAPE
    // (or the extraction/normalisation feeding it) is ever tightened enough to
    // filter out a real parameterised route, this assertion goes red on its
    // own line — see the header comment for why the SSE-only anchor above
    // cannot catch that.
    const FIXTURE_SOURCE = [
      "const boot = `/api/boot-sequence/${runtimeKey}`;",
      'const interaction = "/api/system-interaction";',
    ].join("\n");

    const templated = templatePaths(FIXTURE_SOURCE);
    expect(templated).toContain(`/api/boot-sequence/${PARAM}`);

    const declaredNormalized = new Set(
      Object.keys(spec.paths).map(normalizeDeclared)
    );
    for (const p of templated) {
      expect(declaredNormalized.has(p), `${p} is not declared`).toBe(true);
    }

    const literal = pathLiterals(FIXTURE_SOURCE);
    expect(literal).toContain("/api/system-interaction");
    const declared = new Set(Object.keys(spec.paths));
    for (const p of literal) {
      expect(declared.has(p), `${p} is not declared`).toBe(true);
    }
  });
});
