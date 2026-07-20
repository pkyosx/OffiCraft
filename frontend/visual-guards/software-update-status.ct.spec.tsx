// HOTSPOT — 設定 › 軟體更新: the refresh icon's anchor row + the 查看 release
// link's contrast (owner round-2 acceptance, T-dc68).
//
// Bug ① (owner, phone screenshot): the 檢查更新 refresh icon was anchored to the
// STATUS CHIP's row. The chip's text is variable-length — 已是最新版 /
// 有可用的新版本 v0.9.9 · 查看 release / 連不上 GitHub、查不到最新版本——請稍後
// 再試 — so at 375px the `unknown` sentence consumed the whole row and
// flex-wrap pushed the icon down to a second line, orphaned at the bottom-left.
// Owner's verdict: pin the icon to the VERSION NUMBER's row, whose string is
// short and bounded, so its geometry is identical in every state.
//
// Bug ②: the 查看 release <a> inside the indigo pill carried no styling at all,
// so it rendered in the UA default link blue (#0000EE) — jarring on the dark
// card and far under 4.5:1.
//
// jsdom cannot reach either: it has no layout engine (every rect is 0, so
// "which row is the icon on" is unanswerable) and no cascade/compositing (so
// the effective backdrop behind a pill, and therefore any contrast number, is
// unavailable). Hence a CT guard, measured at BOTH 375px and desktop.
//
// MUTANTS (§5) — every assertion below was individually proven red, and where
// an earlier assertion would have short-circuited the test it was temporarily
// relaxed so the intended one failed ALONE. Full log in kyle-dc68-fixup2.md:
//   M1 move the <button> back inside .sw-status (+ restore its wrapping flex
//      row) → "icon must share the version row" red in all 8 layout cases.
//   M4 render .sw-status before .sw-build → "version row sits above the status
//      chip" red.
//   M2 delete `.sw-badge a { color: inherit }` → link falls back to the UA
//      #0000EE; "link colour inherits" red, and with that relaxed the 4.5:1
//      assertion reds on its own at 1.32:1 (office) / 1.28:1 (xian).
//   M5 `text-decoration: none` → "the link must stay underlined" red.
//   Overflow assertions are regression nets rather than fix-carriers (nothing
//   overflows today, long tag included). Their liveness was proven by probe:
//   forcing the chip to min-width 900px reds `.sw-card` at 547px, and with
//   that relaxed reds the chip's own rect-vs-content-box check at 567px.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { SoftwareUpdateStory } from "./stories/SoftwareUpdateStory";
import type { SwVerdict } from "./stories/SoftwareUpdateStory";

const VIEWPORTS = [
  { name: "375px phone", width: 375, height: 812 },
  { name: "desktop", width: 1280, height: 900 },
];
const VERDICTS: SwVerdict[] = [
  "up_to_date",
  "update_available",
  "unknown",
  "update_available_long_tag",
];

/** Navigate into 軟體更新 and run one explicit check, leaving the card in the
 * story's forced verdict. */
async function openAndCheck(page: Page) {
  await page.getByText("軟體更新", { exact: true }).first().click();
  const refresh = page.getByTestId("settings-check-release");
  await expect(refresh).toBeVisible();
  await refresh.click();
  // The badge settles out of 檢查中… into the forced verdict.
  await expect(page.getByTestId("settings-update-status")).not.toContainText(
    "檢查中"
  );
  return refresh;
}

// ── ① the refresh icon is pinned to the version-number row ──
for (const vp of VIEWPORTS) {
  for (const verdict of VERDICTS) {
    test(`${vp.name} · ${verdict}: refresh icon sits on the version-number row`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width: vp.width, height: vp.height });
      await mount(<SoftwareUpdateStory verdict={verdict} />);
      const refresh = await openAndCheck(page);

      const version = page.locator(".sw-build__version");
      const vBox = (await version.boundingBox())!;
      const rBox = (await refresh.boundingBox())!;

      // (1) CORE red→green: same row. Their vertical centres line up (the row
      // is a centred flex line), and the icon is to the RIGHT of the number —
      // not wrapped below it, which is exactly what owner saw on `unknown`.
      const vMid = vBox.y + vBox.height / 2;
      const rMid = rBox.y + rBox.height / 2;
      expect(
        Math.abs(vMid - rMid),
        `icon must share the version row (version mid ${vMid}, icon mid ${rMid})`
      ).toBeLessThanOrEqual(4);
      expect(
        rBox.x,
        "icon must sit to the right of the version number"
      ).toBeGreaterThanOrEqual(vBox.x + vBox.width - 1);

      // (2) it must not overlap the version text.
      expect(rBox.x, "icon must not overlap the version text").toBeGreaterThan(
        vBox.x + vBox.width - 1
      );

      // (3) the icon's row is ABOVE the status chip — the chip can grow to two
      // lines without ever moving the icon.
      const badge = page.locator(".sw-badge").first();
      const bBox = (await badge.boundingBox())!;
      expect(
        rBox.y + rBox.height,
        "the version row (with the icon) sits above the status chip"
      ).toBeLessThanOrEqual(bBox.y + 1);

      // (4) no horizontal overflow. NOTE on method: `scrollWidth-clientWidth`
      // is only meaningful on the CONTAINER that clips. The chip itself is the
      // element that would BURST OUT, and its own scrollWidth just grows with
      // it — so the chip is measured as a RECT against its container's content
      // box instead. (`.sw-status__badge` is an inline <span>: both metrics are
      // 0 on it, so measuring it would be vacuous — verified by probe.)
      const cardOver = await page
        .locator(".sw-card")
        .evaluate((el) => el.scrollWidth - el.clientWidth);
      expect(cardOver, ".sw-card must not overflow horizontally").toBeLessThanOrEqual(1);
      const rowOver = await page
        .locator(".sw-build__headline")
        .evaluate((el) => el.scrollWidth - el.clientWidth);
      expect(rowOver, ".sw-build__headline must not overflow").toBeLessThanOrEqual(1);
      const burst = await page.locator(".sw-badge").first().evaluate((el) => {
        const host = el.closest(".sw-card") as HTMLElement;
        const hr = host.getBoundingClientRect();
        const cs = getComputedStyle(host);
        const contentRight = hr.right - parseFloat(cs.paddingRight) - parseFloat(cs.borderRightWidth);
        return el.getBoundingClientRect().right - contentRight;
      });
      expect(burst, "the status chip must stay inside the card's content box").toBeLessThanOrEqual(1);
      const pageOver = await page.evaluate(
        () =>
          document.scrollingElement!.scrollWidth -
          document.scrollingElement!.clientWidth
      );
      expect(pageOver, "page has no horizontal overflow").toBeLessThanOrEqual(1);
    });
  }
}

// ── ② the 查看 release link's contrast, computed from real painted colours ──
//
// The measurement composites every ancestor background (alpha included) down
// onto the page, so a translucent "wash" container CANNOT be mistaken for the
// pill's own backdrop. The CONTROL is asserted alongside: the composited
// backdrop must equal the pill's own declared background-color. If they ever
// diverge, the number being reported is a wash artefact, not the pill.
const CONTRAST_PROBE = `(() => {
  const parse = (c) => {
    const m = c.match(/rgba?\\(([^)]+)\\)/);
    if (!m) return null;
    const p = m[1].split(',').map((x) => parseFloat(x));
    return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 };
  };
  const over = (fg, bg) => ({
    r: fg.r * fg.a + bg.r * (1 - fg.a),
    g: fg.g * fg.a + bg.g * (1 - fg.a),
    b: fg.b * fg.a + bg.b * (1 - fg.a),
    a: 1,
  });
  const lum = (c) => {
    const f = (v) => {
      const s = v / 255;
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
  };
  const link = document.querySelector('.sw-badge a');
  if (!link) return null;
  const fg = parse(getComputedStyle(link).color);

  // Composite the FULL ancestor chain (any translucent wash included).
  const layers = [];
  for (let el = link; el; el = el.parentElement) {
    const bg = parse(getComputedStyle(el).backgroundColor);
    if (bg && bg.a > 0) layers.push({ el, bg });
  }
  let composited = { r: 255, g: 255, b: 255, a: 1 };
  for (let i = layers.length - 1; i >= 0; i--) composited = over(layers[i].bg, composited);

  // CONTROL: the pill's OWN declared background, ignoring everything behind it.
  const pill = link.closest('.sw-badge');
  const pillBg = parse(getComputedStyle(pill).backgroundColor);

  const ratio = (a, b) => {
    const l1 = lum(a), l2 = lum(b);
    return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
  };
  return {
    color: getComputedStyle(link).color,
    decoration: getComputedStyle(link).textDecorationLine,
    composited,
    pillBg,
    // pillOpaque: the pill declares its own opaque fill, so nothing behind it
    // can lift the measured backdrop.
    pillOpaque: pillBg.a === 1,
    ratioComposited: ratio(fg, composited),
    ratioPillOnly: ratio(fg, pillBg),
  };
})()`;

for (const theme of ["office", "xian"] as const) {
  test(`${theme} theme: 查看 release link is card-styled and clears 4.5:1`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await mount(
      <SoftwareUpdateStory verdict="update_available" theme={theme} />
    );
    await openAndCheck(page);
    await expect(page.getByText("查看 release")).toBeVisible();

    const m = await page.evaluate(CONTRAST_PROBE);
    expect(m, "the link must exist inside the pill").not.toBeNull();
    const r = m as NonNullable<typeof m>;
    // eslint-disable-next-line no-console
    console.log(`[contrast/${theme}]`, JSON.stringify(r));

    // (1) CORE red→green: the link is NOT the UA default blue any more — it
    // inherits the pill's foreground.
    const badgeColor = await page
      .locator(".sw-badge--new")
      .evaluate((el) => getComputedStyle(el).color);
    expect(r.color, "link colour inherits the pill's foreground").toBe(
      badgeColor
    );

    // (2) the link affordance survives the recolour — underline carries it.
    expect(r.decoration, "the link must stay underlined").toContain("underline");

    // (3) CONTROL GROUP: the pill declares its own opaque background, so the
    // composited backdrop and the pill's own colour are the SAME. A divergence
    // here would mean the number below came from an ancestor wash.
    expect(r.pillOpaque, "the pill must declare its own opaque background").toBe(
      true
    );
    expect(
      Math.abs(r.ratioComposited - r.ratioPillOnly),
      "composited backdrop must equal the pill's own background (no wash contribution)"
    ).toBeLessThan(0.01);

    // (4) WCAG AA normal text.
    expect(
      r.ratioComposited,
      `${theme}: link contrast ${r.ratioComposited.toFixed(2)}:1 must clear 4.5:1`
    ).toBeGreaterThanOrEqual(4.5);
  });
}
