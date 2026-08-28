// payloadInjection.paint.spec.ts — T-1500 gate 4c.
//
// The source is an attacker-writable localStorage record; the sink is
// element.style.setProperty on <html>. src/lib/themePaint.test.ts already proves
// the VALIDATOR rejects every payload in the shared fixture list. This file
// proves the thing jsdom structurally cannot: that the inline script in the REAL
// built artifact actually CALLS that validator on a real page load, before any
// React code exists.
//
// Replacing readValidatedPaint() with a bare JSON.parse keeps tsc green, the
// build green, the artifact assertions green and the decision tests green. This
// is the file that goes red for it — measured: 4 of the 6 payloads land on <html>
// within 8-9 ms of navigation start.
//
// Sampling is per-frame for a reason: React's apply effect wipes the injected
// properties, so a single read after `load` sees a clean DOM and passes all six.

import { expect, test } from "@playwright/test";
import {
  EXPECT_APPLIED,
  MALICIOUS_PAINT_CASES,
  PAINT_THEME_ID,
  VALID_RICH_BUNDLE,
  paintRecordJSON,
} from "../src/lib/paintFixtures";
import {
  MIN_SAMPLES,
  applyNetProfile,
  collect,
  collectPageErrors,
  frameCarrying,
  frameCarryingBeforeMount,
  installFrameSampler,
  seedSession,
  stubURL,
  summarize,
} from "./frameProbe";

const TOKEN = "paint-guard-owner-token";

// Deliberately the server that knows NO themes.
//
// Against the happy-path server this file produced three false failures, and the
// reason matters: that server hands back the real theme, so React legitimately
// applies `--canvas-bg-image` and `--color-bg: #010203` once reconcile lands
// (measured at ~2038 ms). Those are the very substrings the svg-canvas-bg,
// illegal-canvasMode and wording cases forbid — the assertion could not tell
// "the pre-paint script leaked a rejected value" from "the server sent a real
// theme and React applied it".
//
// [T-83ef] That server now says so on the theme resource: GET /api/themes is
// `[]` and display_theme is "", so the reconcile finds the active id in no set,
// never issues GET /api/themes/{id}, and no bundle ever reaches the page. With
// no server bundle in play the ONLY code that can put a theme custom property on
// <html> is the pre-paint script reading the record under test, so every
// occurrence is attributable. The injection window (8-9 ms) is far earlier than
// the reconcile that then clears the record, so nothing is lost.
const INJECTION_SERVER = stubURL("PAINT_GUARD_UNKNOWN_URL");

async function loadWith(page: import("@playwright/test").Page, record: string) {
  await page.goto(INJECTION_SERVER);
  await seedSession(page, { token: TOKEN, themeId: PAINT_THEME_ID, paintRecord: record });
  await installFrameSampler(page);
  // Throttled deliberately, and not just for realism: unthrottled, React mounts
  // before the sampler's first animation frame even runs (measured: first sample
  // at 24.4 ms, already mounted), so there are ZERO pre-mount frames and the
  // attribution assertion below has nothing to look at. 4G gives a ~450 ms window
  // in which the inline script is the only code that exists.
  await applyNetProfile(page, "fourg");
  await page.reload({ waitUntil: "load" });
  await page.waitForTimeout(2500);
}

for (const c of MALICIOUS_PAINT_CASES) {
  test(`no frame ever carries a rejected value — ${c.name}`, async ({ page }) => {
    const pageErrors = collectPageErrors(page);
    await loadWith(page, paintRecordJSON(c.bundle));
    const { samples, prototypePolluted } = await collect(page);

    // The probe must have run — SAMPLES has a floor, because a probe that
    // regressed to a single post-load read still satisfies `> 0` and would pass
    // every case below.
    expect(
      samples.length,
      `only ${samples.length} frames sampled — the per-frame sampler did not run`
    ).toBeGreaterThanOrEqual(MIN_SAMPLES);
    expect(samples.some((f) => f.mounted), "React never mounted").toBe(true);
    expect(pageErrors, "uncaught page errors").toEqual([]);
    // Recorded, not asserted as coverage: no code path in the applier writes to a
    // payload-controlled key, so this can never go red. It is here to be noticed
    // if that ever stops being true.
    expect(prototypePolluted).toBe(false);

    for (const needle of c.forbidden) {
      const hit = frameCarrying(samples, needle);
      expect(
        hit,
        `"${needle}" reached <html> at ${hit?.t}ms :: ${hit?.htmlStyle.slice(0, 160)}` +
          (c.notGuardedByValidator
            ? " (NOTE: this case is normally dropped by CSSOM, not by the validator)"
            : "")
      ).toBeUndefined();
    }
  });
}

test("baseline — the VALID rich record is accepted and every branch of it paints", async ({
  page,
}) => {
  // The positive control. Without it the whole file is satisfiable by a pre-paint
  // script that does nothing at all, and by an applier that has quietly dropped
  // its fonts loop and canvas branch (measured: that mutant passes an
  // absence-only suite 6/6).
  const pageErrors = collectPageErrors(page);
  await loadWith(page, paintRecordJSON(VALID_RICH_BUNDLE));
  const { samples } = await collect(page);

  expect(samples.length).toBeGreaterThanOrEqual(MIN_SAMPLES);
  expect(pageErrors).toEqual([]);
  for (const needle of EXPECT_APPLIED) {
    // BEFORE React mounted — otherwise React's own !themesLoaded fallback
    // satisfies this and the control says nothing about the inline script.
    expect(
      frameCarryingBeforeMount(samples, needle),
      `no PRE-REACT frame carried ${needle}\n${summarize(samples)}`
    ).toBeTruthy();
  }
});

test("a record the validator rejects leaves the page on the default theme, not blank", async ({
  page,
}) => {
  // Rejecting must degrade to today's behaviour (the office default), never to an
  // unpainted page — "no injection" is not enough on its own.
  const pageErrors = collectPageErrors(page);
  await loadWith(page, paintRecordJSON(MALICIOUS_PAINT_CASES[0].bundle));
  const { samples } = await collect(page);

  expect(samples.length).toBeGreaterThanOrEqual(MIN_SAMPLES);
  expect(pageErrors).toEqual([]);
  const last = samples[samples.length - 1];
  expect(last.bg, `page ended on ${last.bg}`).not.toBe("NOBODY");
  expect(last.bg).toMatch(/^rgb/);
  expect(last.mounted).toBe(true);
});
