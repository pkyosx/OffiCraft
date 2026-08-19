// T-789b — the reinstall confirm's new copy must follow the theme token layer.
//
// WHY CT AND NOT jsdom: jsdom resolves no `var()` and loads no stylesheet, so
// `getComputedStyle(el).color` there answers about a browser default that never
// ships. Whether this paragraph is readable under a LIGHT theme pack is a
// question only a real browser can answer.
//
// DISCRIMINATING POWER, per assertion:
//   * "reads the token value in the built-in (dark) palette" → weak on its own:
//     a hardcoded #e7e8ee would also pass it. It is here to pin the SLOT.
//   * "repaints when a light pack re-values the tokens" → THE one with power.
//     A colour literal on `.mon-confirm__title` / `.mon-confirm__body` (the
//     exact mistake presence-and-badges.md forbids) keeps the dark text over the
//     light card and turns this red.
import { test, expect } from "@playwright/experimental-ct-react";
import { MonitorBootstrapConfirmStory } from "./stories/MonitorBootstrapConfirmStory";

/** A LIGHT theme pack, expressed the way themePaint.ts applies one: token
 * values set on :root. Nothing else about the page changes. */
const LIGHT = {
  "--color-card": "#ffffff",
  "--color-text": "#232733",
  "--color-text-strong": "#0d1018",
};

/** Resolve a raw token value to the rgb() string the browser would paint. */
async function asPainted(page: import("@playwright/test").Page, raw: string) {
  return page.evaluate((v) => {
    const probe = document.createElement("span");
    probe.style.color = v;
    document.body.appendChild(probe);
    const out = getComputedStyle(probe).color;
    probe.remove();
    return out;
  }, raw);
}

async function paintedColours(page: import("@playwright/test").Page) {
  return page.evaluate(() => {
    const read = (id: string) => {
      const el = document.querySelector(`[data-testid="${id}"]`);
      if (!el) throw new Error(`missing ${id}`);
      return getComputedStyle(el).color;
    };
    const box = document.querySelector(".mon-confirm__box");
    if (!box) throw new Error("missing .mon-confirm__box");
    return {
      title: read("confirm-title"),
      body: read("confirm-body"),
      boxBg: getComputedStyle(box).backgroundColor,
    };
  });
}

test("the confirm copy takes its colours from the theme tokens (built-in palette)", async ({
  mount,
  page,
}) => {
  await mount(<MonitorBootstrapConfirmStory />);
  const tokens = await page.evaluate(() => {
    const cs = getComputedStyle(document.documentElement);
    return {
      text: cs.getPropertyValue("--color-text").trim(),
      strong: cs.getPropertyValue("--color-text-strong").trim(),
      card: cs.getPropertyValue("--color-card").trim(),
    };
  });
  expect(tokens.text, "theme.css must define --color-text").not.toBe("");

  const got = await paintedColours(page);
  expect(got.title).toBe(await asPainted(page, tokens.strong));
  expect(got.body).toBe(await asPainted(page, tokens.text));
  expect(got.boxBg).toBe(await asPainted(page, tokens.card));
});

test("a light theme pack repaints the same copy — no colour is baked in", async ({
  mount,
  page,
}) => {
  await mount(<MonitorBootstrapConfirmStory />);
  const dark = await paintedColours(page);

  await page.evaluate((pack) => {
    for (const [tok, val] of Object.entries(pack)) {
      document.documentElement.style.setProperty(tok, val);
    }
  }, LIGHT);

  const light = await paintedColours(page);
  expect(light.title).toBe(await asPainted(page, LIGHT["--color-text-strong"]));
  expect(light.body).toBe(await asPainted(page, LIGHT["--color-text"]));
  expect(light.boxBg).toBe(await asPainted(page, LIGHT["--color-card"]));
  // …and it actually moved: the dark values are not still on screen.
  expect(light.title).not.toBe(dark.title);
  expect(light.body).not.toBe(dark.body);
  expect(light.boxBg).not.toBe(dark.boxBg);
});
