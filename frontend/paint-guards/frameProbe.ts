// frameProbe.ts — T-1500 gate 4c. The sampling machinery shared by the two paint
// guards.
//
// It samples EVERY animation frame from document_start. Sampling late is the
// mistake that made the first version of this guard useless: React's apply effect
// removes the properties the pre-paint script wrote and re-writes its own, so a
// read 300 ms after `load` shows a clean DOM even when a rejected payload WAS on
// screen. Measured on one build with the validator stripped out: per-frame
// sampling fails 4 of 6 injection cases; the same build read once after load
// passes all 6.
//
// Every helper here refuses to let an absent measurement read as a pass —
// `SAMPLES` has a floor, not just `> 0`. A probe that regressed to one late read
// still reports SAMPLES=1, which is `> 0`.

import type { Page, Response } from "@playwright/test";
// The storage keys come FROM the modules that own them, never from a literal
// spelled here. A probe with its own copy of "oc.theme" keeps passing after a
// key rename — it would just be seeding a key nothing reads, and asserting that
// the theme it never seeded did not appear.
import { TOKEN_KEY } from "../src/api/auth";
import { LS_THEME, LS_THEME_PAINT } from "../src/lib/themePaint";

/** Any frame count below this means the sampler did not actually run per frame
 * (measured: a healthy 3 s window yields 200-260 samples; a single late read
 * yields 1). */
export const MIN_SAMPLES = 80;

export const NET_PROFILES = {
  loopback: null,
  /** ~4 Mbit/s down, 150 ms RTT. Slow enough that a build without the inline
   * script shows office-coloured frames for ~450-630 ms. */
  fourg: {
    offline: false,
    downloadThroughput: (4 * 1024 * 1024) / 8,
    uploadThroughput: (3 * 1024 * 1024) / 8,
    latency: 150,
  },
} as const;

export type NetProfile = keyof typeof NET_PROFILES;

export interface FrameSample {
  /** ms since navigation start. */
  t: number;
  /** computed background-color of <body>, or "NOBODY" before body parses. */
  bg: string;
  /** the serialized `style` attribute of <html> — what the appliers wrote. */
  htmlStyle: string;
  mounted: boolean;
}

export interface ProbeResult {
  samples: FrameSample[];
  pageErrors: string[];
  prototypePolluted: boolean;
}

declare global {
  interface Window {
    __ocFrames?: FrameSample[];
  }
}

/** Install the per-frame sampler. MUST be called before the navigation whose
 * frames you want (i.e. before `page.reload()`), because it runs at
 * document_start. */
export async function installFrameSampler(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const frames: FrameSample[] = [];
    window.__ocFrames = frames;
    const tick = () => {
      // A throw here (getComputedStyle(null) before <body> parses) would kill the
      // rAF loop, leaving zero bad frames = a green run. So it is recorded, not
      // thrown, and "NOBODY" is itself a bad frame for the zero-flash assertion.
      let bg = "NOBODY";
      let htmlStyle = "";
      let mounted = false;
      try {
        if (document.body) bg = getComputedStyle(document.body).backgroundColor;
        htmlStyle = document.documentElement.getAttribute("style") ?? "";
        mounted = !!document.getElementById("root")?.firstChild;
      } catch (e) {
        bg = `THREW:${(e as Error).message}`;
      }
      frames.push({
        t: Math.round(performance.now() * 100) / 100,
        bg,
        htmlStyle,
        mounted,
      });
      if (frames.length < 400) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
}

/** Seed the state a returning owner would have: an owner token — so the app takes
 * the AUTHENTICATED path and reconcileFromServer() actually runs, which is the
 * whole point — plus the selected theme id and the cached paint record.
 *
 * Pass `token: null` only to build a deliberately-broken scenario; every real
 * measurement needs one. */
export async function seedSession(
  page: Page,
  opts: { token: string | null; themeId: string; paintRecord: string }
): Promise<void> {
  await page.evaluate(
    ([tokenKey, themeKey, paintKey, token, themeId, record]) => {
      localStorage.clear();
      if (token !== null) localStorage.setItem(tokenKey as string, token as string);
      localStorage.setItem(themeKey as string, themeId as string);
      localStorage.setItem(paintKey as string, record as string);
    },
    [TOKEN_KEY, LS_THEME, LS_THEME_PAINT, opts.token, opts.themeId, opts.paintRecord] as const
  );
}

/** Read the stored paint record via the module's own key. */
export async function readStoredPaint(page: Page): Promise<string | null> {
  return page.evaluate((k) => localStorage.getItem(k), LS_THEME_PAINT);
}

export async function applyNetProfile(page: Page, profile: NetProfile): Promise<void> {
  const conditions = NET_PROFILES[profile];
  if (!conditions) return;
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Network.enable");
  await cdp.send("Network.emulateNetworkConditions", conditions);
}

export async function collect(page: Page): Promise<Omit<ProbeResult, "pageErrors">> {
  return page.evaluate(() => ({
    samples: window.__ocFrames ?? [],
    prototypePolluted: ({} as Record<string, unknown>).polluted !== undefined,
  }));
}

/** Attach a page-error collector. Returns the (growing) array. */
export function collectPageErrors(page: Page): string[] {
  const errs: string[] = [];
  page.on("pageerror", (e) => errs.push(String(e)));
  return errs;
}

/** Capture the 200 bodies of every response whose PATHNAME matches, so a guard
 * can PROVE what the server actually said rather than assuming the scenario it
 * meant to set up.
 *
 * Matched on the pathname, never on a substring of the URL: `/api/themes` must
 * not also collect `/assets/api-themes-abc123.js`, and the single-bundle read
 * must not collect the list. */
function captureJSONResponses(
  page: Page,
  matches: (pathname: string) => boolean
): Promise<unknown>[] {
  const bodies: Promise<unknown>[] = [];
  page.on("response", (res: Response) => {
    if (matches(new URL(res.url()).pathname) && res.status() === 200) {
      bodies.push(res.json().catch(() => null));
    }
  });
  return bodies;
}

/** GET /api/settings — still the face that says WHICH theme is active
 * (`display_theme`). It no longer carries the themes themselves. */
export function captureSettingsResponses(page: Page): Promise<unknown>[] {
  return captureJSONResponses(page, (p) => p === "/api/settings");
}

/** GET /api/themes — the face that says WHICH THEMES EXIST (id + name rows).
 * [T-83ef] This is where `custom_themes` went; a guard proving "the server knows
 * this theme" has to read it here now. */
export function captureThemeListResponses(page: Page): Promise<unknown>[] {
  return captureJSONResponses(page, (p) => p === "/api/themes");
}

/** GET /api/themes/{id} — the face that carries the actual PICTURE (the full
 * bundle). Only the ACTIVE theme is ever fetched, and only when the list says it
 * exists, so a hit here is proof the reconcile got as far as adopting a bundle. */
export function captureThemeBundleResponses(page: Page): Promise<unknown>[] {
  return captureJSONResponses(page, (p) => p.startsWith("/api/themes/"));
}

/** The frames whose background is not the expected cached colour. "NOBODY" and
 * "THREW:…" count as bad — an unpainted first frame is a flash too. */
export function badFrames(samples: FrameSample[], expectedBg: string): FrameSample[] {
  return samples.filter((f) => f.bg !== expectedBg);
}

/** The first frame carrying `needle` in <html>'s style attribute, if any. */
export function frameCarrying(samples: FrameSample[], needle: string): FrameSample | undefined {
  return samples.find((f) => f.htmlStyle.includes(needle));
}

/** The first frame carrying `needle` while React had NOT yet mounted.
 *
 * This is the attribution assertion. `frameCarrying` alone cannot tell the inline
 * pre-paint script from React's own `!themesLoaded` fallback branch, which also
 * calls readValidatedPaint() and applies the cache on mount — measured: with the
 * inline plugin removed entirely, a plain `frameCarrying` positive control still
 * passes, because React put the values there a few hundred ms later. Only a frame
 * with no mounted React tree can have been painted by the inline script. */
export function frameCarryingBeforeMount(
  samples: FrameSample[],
  needle: string
): FrameSample | undefined {
  return samples.find((f) => !f.mounted && f.htmlStyle.includes(needle));
}

/** A compact, greppable dump for a failure message. */
export function summarize(samples: FrameSample[]): string {
  const seen: string[] = [];
  for (const f of samples) {
    const line = `${f.t}\tbg=${f.bg}\tmounted=${f.mounted}\tstyle=${f.htmlStyle.slice(0, 90)}`;
    if (seen.length === 0 || !seen[seen.length - 1].endsWith(line.split("\t").slice(1).join("\t"))) {
      seen.push(line);
    }
  }
  return seen.slice(0, 25).join("\n");
}
