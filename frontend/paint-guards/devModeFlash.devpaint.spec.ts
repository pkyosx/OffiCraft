// devModeFlash.devpaint.spec.ts — T-1500, the DEV-MODE half of the paint
// measurement.
//
// WHY IT EXISTS: zeroFlash.paint.spec.ts only ever measures the PRODUCTION
// artifact (dist/, served by settingsStub.mjs). Design v2 additionally claimed
// "dev mode is zero-flash too". Nothing measured that claim. This file measures
// it, on the real `vite` dev server, with the same per-frame sampler.
//
// It differs from the production guard in exactly two ways, both deliberate:
//
//   1. THE SERVER IS `vite` (npm run dev), not settingsStub.mjs. That is the
//      whole point: dev serves index.html through transformIndexHtml and ships
//      unbundled ES modules, so the first-paint waterfall is a different shape
//      from dist/.
//   2. The reconcile's endpoints are answered by page.route() instead of a
//      second process — same JSON shapes as settingsStub.mjs in mode=ok, same
//      400 ms delay on each. The delay is NOT padding: the flash this ticket
//      fixes IS the wait for those responses, and a 0 ms answer deletes the
//      window under test.
//
// [T-83ef] "the reconcile" is THREE requests now, not one: themes left settings
// (`custom_themes` is gone from both faces), so the provider does
// Promise.all([GET /api/settings, GET /api/themes]) and then fetches the ACTIVE
// bundle from GET /api/themes/{id}. All three are routed here and ALL THREE are
// delayed — delaying only settings would leave the other legs answering
// instantly and shrink the very window being measured.
//
// THE PRECONDITION IS ASSERTED, NOT ASSUMED — same discipline as the production
// guard, for the same reason: reconcileFromServer() is gated on hasToken(), so a
// tokenless session never reconciles and reads BAD_FRAMES=0 on a build whose
// reconcile handoff is completely untested. Every run here proves in-band that
// (a) the reconcile's requests really answered 200, (b) the server really
// reported this theme as existing and really handed back its bundle,
// and (c) the app really adopted the SERVER's copy — the seeded record carries a
// different `name`, and the record on disk afterwards must carry the server's.
// (b) is now read off GET /api/themes — the server reported the theme as
// existing — plus GET /api/themes/{id}, which is the only place the picture can
// come from; that is the same claim `custom_themes` used to carry.
//
// THE ASSERTION SHAPE IS "no frame is anything but the cached colour", never
// "no frame is office-coloured". A white/unpainted frame (`NOBODY`,
// `rgb(255, 255, 255)`, `rgba(0, 0, 0, 0)`) is a flash too, and an
// office-only assertion is blind to it.

import { expect, test } from "@playwright/test";
import {
  CACHED_BG_RGB,
  PAINT_THEME_ID,
  VALID_RICH_BUNDLE,
  paintRecordJSON,
} from "../src/lib/paintFixtures";
import {
  MIN_SAMPLES,
  applyNetProfile,
  badFrames,
  collect,
  collectPageErrors,
  installFrameSampler,
  readStoredPaint,
  seedSession,
  summarize,
  type FrameSample,
  type NetProfile,
} from "./frameProbe";

const DEV_URL = process.env.PAINT_DEV_URL ?? "http://localhost:4320";
const TOKEN = "paint-guard-owner-token";
/** Byte-identical to the server's copy EXCEPT the name: adopting the server's
 * copy is visually a no-op but observably distinct. */
const STALE_NAME = "STALE-CACHE-NAME";
const STALE_RECORD = paintRecordJSON({ ...VALID_RICH_BUNDLE, name: STALE_NAME });

/** The office default, #191c24 (src/styles/theme.css) — what the pre-fix build
 * paints while it waits for /api/settings. */
const OFFICE_BG_RGB = "rgb(25, 28, 36)";
/** An unpainted frame: no <body> yet, the UA default white, or a transparent
 * body over a white canvas. All three are "the user sees white". */
const WHITE_BGS = new Set(["NOBODY", "rgb(255, 255, 255)", "rgba(0, 0, 0, 0)"]);

const isWhite = (f: FrameSample) => WHITE_BGS.has(f.bg);
const isOffice = (f: FrameSample) => f.bg === OFFICE_BG_RGB;

/** GET /api/settings in mode=ok — the SAME shape settingsStub.mjs sends, built
 * from the SAME fixture object, so the two cannot drift into disagreeing about
 * what the server said. [T-83ef] `custom_themes` is NOT here: it is not on the
 * real SettingsDTO any more. `display_theme` stays — settings still owns WHICH
 * theme is active. */
function settingsDTO() {
  return {
    owner_token_ttl: 86400,
    agent_token_ttl: 604800,
    handover_pct: 50,
    codex_compaction_threshold: 3,
    monitoring_refresh_seconds: 5,
    outsource_max_parallel: 3,
    doc_cap_chars_duty: 1000,
    doc_cap_chars_insight: 15000,
    doc_cap_chars_learning: 15000,
    doc_cap_chars_manual_sop: 15000,
    doc_cap_chars_manual_learnings: 15000,
    updater_receive_beta: false,
    updater_auto_update: false,
    org_name: "",
    owner_name: "",
    push_contact_email: "",
    display_theme: PAINT_THEME_ID,
    display_language: "zh",
    display_wide: false,
    onboarding: null,
  };
}

/** GET /api/themes in mode=ok — ThemeListItemDTO rows: id + name ONLY, never the
 * bundle. Handing back whole bundles here would let a regression that reads
 * `colors` off a list row stay green in this guard and paint nothing in
 * production. */
function themeListDTO() {
  return [{ id: VALID_RICH_BUNDLE.id, name: VALID_RICH_BUNDLE.name }];
}

interface Wiring {
  /** How many times GET /api/settings was answered 200. */
  settingsHits: () => number;
  /** How many times GET /api/themes was answered 200. */
  themeListHits: () => number;
  /** How many times GET /api/themes/{id} was answered 200 — i.e. how many times
   * the reconcile got as far as fetching the ACTIVE theme's picture. */
  themeBundleHits: () => number;
}

/** Stand in for settingsStub.mjs with page.route(): a delayed 200 on each of the
 * reconcile's three endpoints (/api/settings, /api/themes, /api/themes/{id}),
 * and the stub's 404 error envelope on every other /api/ path.
 * NEVER 401 — a 401 clears the token and bounces to the login wall, unmounting
 * the very page being sampled. */
async function wireApi(page: import("@playwright/test").Page): Promise<Wiring> {
  let settingsHits = 0;
  let themeListHits = 0;
  let themeBundleHits = 0;
  // The flash under measurement IS the wait for the reconcile, so every leg of it
  // pays the same 400 ms. NOT padding on any of them.
  const RECONCILE_DELAY_MS = 400;
  const delay = () => new Promise((r) => setTimeout(r, RECONCILE_DELAY_MS));
  // MATCH ON THE PATHNAME, NOT ON A GLOB. Measured trap: the glob `**/api/**`
  // ALSO matches the dev server's own module URLs `/src/api/index.ts`,
  // `/src/api/http.ts`, … — in dev those are real HTTP requests, so the catch-all
  // 404'd the app's own source, React never mounted, /api/settings was never
  // requested, and every frame was blank. That reads as "dev mode is one long
  // white flash" when the truth is "the probe broke the app". Same reason the
  // settings matcher is a pathname equality: `**/api/settings` would happily
  // match `/src/api/settings.ts` if such a module were ever added.
  //
  // Playwright matches routes LAST-registered-first, so the catch-all goes on
  // first and the specific handlers registered after it win.
  await page.route(
    (url) => url.pathname.startsWith("/api/"),
    async (route) => {
      await route.fulfill({
        status: 404,
        contentType: "application/json; charset=utf-8",
        body: JSON.stringify({
          error: { code: "not_found", message: "dev paint probe: not stubbed" },
        }),
      });
    }
  );
  await page.route(
    (url) => url.pathname === "/api/settings",
    async (route) => {
      if (route.request().method() !== "GET") return route.fallback();
      await delay();
      settingsHits += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json; charset=utf-8",
        headers: { "cache-control": "no-store" },
        body: JSON.stringify(settingsDTO()),
      });
    }
  );
  // Pathname EQUALITY again, for the same measured reason as above: a glob would
  // also match the dev server's own module URLs, and `/api/themes` must not
  // swallow `/api/themes/{id}` — they carry different shapes and the guard reads
  // both.
  await page.route(
    (url) => url.pathname === "/api/themes",
    async (route) => {
      if (route.request().method() !== "GET") return route.fallback();
      await delay();
      themeListHits += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json; charset=utf-8",
        headers: { "cache-control": "no-store" },
        body: JSON.stringify(themeListDTO()),
      });
    }
  );
  await page.route(
    (url) => url.pathname === `/api/themes/${VALID_RICH_BUNDLE.id}`,
    async (route) => {
      if (route.request().method() !== "GET") return route.fallback();
      await delay();
      themeBundleHits += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json; charset=utf-8",
        headers: { "cache-control": "no-store" },
        // The FULL bundle — the only place the server's picture can come from.
        body: JSON.stringify(VALID_RICH_BUNDLE),
      });
    }
  );
  return {
    settingsHits: () => settingsHits,
    themeListHits: () => themeListHits,
    themeBundleHits: () => themeBundleHits,
  };
}

interface Numbers {
  profile: string;
  samples: number;
  bad: number;
  white: number;
  office: number;
  other: number;
  firstFrameMs: number | null;
  firstBadMs: number | null;
  lastBadMs: number | null;
  /** When the cached colour FIRST reached the screen. `null` = never. The gap
   * between firstFrameMs and this is the flash the user sees. */
  firstGoodMs: number | null;
  lastFrameMs: number | null;
  /** frameProbe's rAF loop stops at 400 frames. When this is true the sampling
   * window ENDED before the page settled, so `bad` is a floor, not a total. */
  capped: boolean;
  /** How long the poll waited for mount + reconcile, ms. */
  settleMs: number;
  settingsHits: number;
  themeListHits: number;
  themeBundleHits: number;
  mounted: boolean;
  /** The `name` on the paint record AFTER the load: the seeded STALE name means
   * reconcile never ran, null means the record was dropped. */
  storedName: string | null;
  pageErrors: number;
}

/** The scenario is fully set up once React has mounted AND the whole reconcile
 * has answered — i.e. once reconcileFromServer() has had ALL of its input.
 *
 * [T-83ef] Waiting on /api/settings alone is no longer enough: settings answering
 * only means the FIRST leg landed, and the paint record is not rewritten until
 * the ACTIVE bundle arrives from /api/themes/{id}. Polling on the old condition
 * would let the run reach its assertions while the record still carried the
 * seeded STALE name, failing as a setup error on a perfectly healthy build. */
async function isSettled(
  page: import("@playwright/test").Page,
  wiring: Wiring
): Promise<boolean> {
  const mounted = await page.evaluate(() => !!document.getElementById("root")?.firstChild);
  return (
    mounted &&
    wiring.settingsHits() > 0 &&
    wiring.themeListHits() > 0 &&
    wiring.themeBundleHits() > 0
  );
}

/** Read the record's name without throwing when there is no record. */
function storedNameOf(raw: string | null): string | null {
  if (raw === null) return null;
  try {
    return (JSON.parse(raw) as { bundle?: { name?: string } })?.bundle?.name ?? null;
  } catch {
    return null;
  }
}

/** Emit the numbers on stdout BEFORE any assertion runs, so a red run still
 * yields the measurement the A/B comparison needs. */
function report(n: Numbers): void {
  // eslint-disable-next-line no-console
  console.log(`OC_DEVPAINT_RESULT ${JSON.stringify(n)}`);
}

test("dev-server index.html carries the inlined pre-paint script, first to EXECUTE", async ({
  request,
}) => {
  const html = await (await request.get(`${DEV_URL}/`)).text();
  // Every <script> tag, in document order, with the two facts that decide
  // execution order: is it a module (deferred to after parsing) and does it have
  // a src (network-bound)?
  const tags = [...html.matchAll(/<script\b[^>]*>/g)].map((m) => ({
    idx: m.index as number,
    module: /type\s*=\s*"module"/.test(m[0]),
    src: /\bsrc\s*=/.test(m[0]),
    tag: m[0],
  }));
  // The pre-paint is the first CLASSIC inline script — no type=module, no src.
  const prePaint = tags.find((t) => !t.module && !t.src);
  const mainScript = html.indexOf("/src/main.tsx");
  // eslint-disable-next-line no-console
  console.log(
    `OC_DEVPAINT_HTML ${JSON.stringify({
      hasPlaceholder: html.includes("<!--oc-prepaint-->"),
      scriptTags: tags.map((t) => ({ idx: t.idx, module: t.module, src: t.src })),
      prePaintIdx: prePaint?.idx ?? null,
      mainScriptIdx: mainScript,
      prePaint200: prePaint ? html.slice(prePaint.idx, prePaint.idx + 200) : null,
    })}`
  );

  expect(
    html.includes("<!--oc-prepaint-->"),
    "the placeholder is still in the dev HTML — transformIndexHtml did not run in dev"
  ).toBe(false);
  expect(prePaint, "no classic inline <script> in the dev HTML — nothing pre-paints").toBeTruthy();
  // NOTE (measured): in dev @vitejs/plugin-react injects TWO type="module" tags
  // (the react-refresh preamble and /@vite/client) ahead of it. Both are
  // deferred to after HTML parsing, so the classic inline script still runs
  // FIRST — being textually second is not being second to execute.
  expect(
    (prePaint as { idx: number }).idx < mainScript,
    "the pre-paint is textually after main.tsx"
  ).toBe(true);
  expect(
    tags.filter((t) => !t.module && !t.src).length,
    "more than one classic inline script — which one pre-paints is now ambiguous"
  ).toBe(1);
});

for (const profile of ["fourg", "loopback"] as NetProfile[]) {
  test(`DEV MODE — no frame is anything but the cached colour (${profile})`, async ({ page }) => {
    const pageErrors = collectPageErrors(page);
    const wiring = await wireApi(page);

    await page.goto(DEV_URL);
    await seedSession(page, {
      token: TOKEN,
      themeId: PAINT_THEME_ID,
      paintRecord: STALE_RECORD,
    });

    await installFrameSampler(page);
    await applyNetProfile(page, profile);
    await page.reload({ waitUntil: "load" });

    // A FIXED 3 s wait is what the production guard uses, and it is wrong here:
    // measured on this dev server under `fourg`, the first sampled frame does
    // not arrive until ~2.5 s (the unbundled module waterfall), so a 3 s window
    // ends before React mounts and before /api/settings is even requested —
    // every precondition would fail as a setup error and the run would prove
    // nothing. So: sleep past the flash window untouched, then POLL.
    const settleStart = Date.now();
    await page.waitForTimeout(2000);
    for (let i = 0; i < 60 && !(await isSettled(page, wiring)); i++) {
      await page.waitForTimeout(500);
    }
    // Give the reconcile write itself time to land after mount.
    await page.waitForTimeout(1500);
    const settleMs = Date.now() - settleStart;

    const { samples, prototypePolluted } = await collect(page);
    const storedPaint = await readStoredPaint(page);
    const bad = badFrames(samples, CACHED_BG_RGB);

    report({
      profile,
      samples: samples.length,
      bad: bad.length,
      white: bad.filter(isWhite).length,
      office: bad.filter(isOffice).length,
      other: bad.filter((f) => !isWhite(f) && !isOffice(f)).length,
      firstFrameMs: samples[0]?.t ?? null,
      firstBadMs: bad[0]?.t ?? null,
      lastBadMs: bad.length ? bad[bad.length - 1].t : null,
      firstGoodMs: samples.find((f) => f.bg === CACHED_BG_RGB)?.t ?? null,
      lastFrameMs: samples.length ? samples[samples.length - 1].t : null,
      capped: samples.length >= 400,
      settleMs,
      settingsHits: wiring.settingsHits(),
      themeListHits: wiring.themeListHits(),
      themeBundleHits: wiring.themeBundleHits(),
      mounted: samples.some((f) => f.mounted),
      storedName: storedNameOf(storedPaint),
      pageErrors: pageErrors.length,
    });

    // ---- preconditions: this really is the authenticated, server-confirmed
    // scenario. A failure here is a SETUP error, not a paint verdict. ----
    expect(wiring.settingsHits(), "GET /api/settings never answered 200").toBeGreaterThan(0);
    // The server reported the theme as EXISTING — the claim `custom_themes` used
    // to carry, read off the resource that carries it now…
    expect(wiring.themeListHits(), "GET /api/themes never answered 200").toBeGreaterThan(0);
    // …and it really handed back the picture. Without this the run cannot tell a
    // confirmed reconcile from an app that kept its own cache and never asked.
    expect(
      wiring.themeBundleHits(),
      `GET /api/themes/${VALID_RICH_BUNDLE.id} never answered 200 — the reconcile ` +
        "never fetched the active bundle, so no server copy was ever adopted"
    ).toBeGreaterThan(0);
    expect(storedPaint, "the paint record was removed — reconcile did not confirm it").not.toBeNull();
    const stored = JSON.parse(storedPaint as string) as { bundle: { name: string } };
    expect(
      stored.bundle.name,
      "the record still carries the SEEDED name, so reconcile never adopted the server copy — " +
        "this run proves nothing about the authenticated path"
    ).not.toBe(STALE_NAME);
    expect(stored.bundle.name).toBe(VALID_RICH_BUNDLE.name);

    // ---- the probe itself must have run ----
    expect(
      samples.length,
      `only ${samples.length} frames sampled — the per-frame sampler did not run`
    ).toBeGreaterThanOrEqual(MIN_SAMPLES);
    expect(
      samples.some((f) => f.mounted),
      "React never mounted — the page did not actually render"
    ).toBe(true);
    expect(prototypePolluted).toBe(false);
    expect(pageErrors, "uncaught page errors").toEqual([]);

    // ---- the contract ----
    expect(
      bad.length,
      `${bad.length}/${samples.length} frames were not the cached colour ` +
        `(white=${bad.filter(isWhite).length} office=${bad.filter(isOffice).length}); ` +
        `first at ${bad[0]?.t}ms bg=${bad[0]?.bg}\n${summarize(samples)}`
    ).toBe(0);
  });
}
