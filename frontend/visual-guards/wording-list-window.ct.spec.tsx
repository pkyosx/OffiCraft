// T-8115 guard: the 用詞 list mounts only the rows its scroll window shows, and
// that windowing must not cost the owner a single code.
//
// Why this needs a REAL browser on top of the vitest pins: the whole mechanism
// is geometry. The component measures the row pitch and the viewport height off
// the live layout and reserves the rest with two spacers; jsdom reports 0 for
// both, so over there the component runs on its fallback constants and every
// arithmetic mistake — wrong pitch, spacers that do not add up, a window that
// drifts away from the scroll offset — is structurally invisible. Here the
// numbers come from theme-settings.css itself, so a drift of even one row shows
// up as the wrong code sitting at the top of the viewport.
//
// The premise checks come first on purpose: if the list were not actually a
// scroll box, or if it mounted every row anyway, every assertion below would be
// trivially true and this file would be theatre.
//
// MUTANTS (each re-run against THIS file, not inherited from an earlier one):
//   * render `wordingRows.slice(0)` (drop the windowing) → all FOUR tests go
//     red, but read them carefully, because only the first is on the nose:
//     "mounts far fewer rows than it offers" is the honest one. The Tab walk
//     is a FALSE red — it computes `steps = mounted + 12`, which with every
//     row mounted is 878 > 866, so its tail reads MESSAGE_KEYS[867…] =
//     undefined; Tab itself works fine there. The other two go red on premise
//     checks that say "the window really did move on" (`toHaveCount(0)`),
//     i.e. they assert windowing happens at all — deliberately the opposite
//     of the jsdom layer, which is written so mounting MORE rows still passes.
//   * render `wordingRows.slice(0, 30)` (v1's browse cap) → the reachability
//     and scroll-range assertions go red.
//   * drop the two spacers → the scroll range collapses to the mounted rows,
//     and the Tab walk goes red at #26 with a null code.
//   * make `wordingPinned` always null (drop the focus pin) → only the last
//     test goes red, with `document.activeElement` measured as BODY.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator } from "@playwright/test";
import { ThemeSettingsAddStory } from "./stories/ThemeSettingsAddStory";
import { MESSAGE_KEYS } from "../src/i18n/messageKeys.generated";

const LIST = ".ts-wording-list";
const ROW = ".ts-wording-row";

/** Open 設定 › 主題管理, create a theme (that lands in the edit view), and hand
 * back the 用詞 list — the same three clicks an owner makes. */
async function openWordingList(cmp: Locator) {
  await cmp.getByRole("button", { name: "新增" }).click();
  const list = cmp.locator(LIST);
  await expect(list).toBeVisible();
  return list;
}

test("windows the 用詞 list without putting a single code out of reach", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<ThemeSettingsAddStory />);
  const list = await openWordingList(cmp);

  const total = MESSAGE_KEYS.length;
  expect(total, "the panel is only interesting because the set is big").toBeGreaterThan(500);

  // ── premise 1: this really is a scroll box, not a list that just runs on ──
  const box = await list.evaluate((el) => ({
    clientH: el.clientHeight,
    scrollH: el.scrollHeight,
    overflowY: getComputedStyle(el).overflowY,
  }));
  expect(box.overflowY, ".ts-wording-list must own the overflow").toBe("auto");
  expect(box.clientH).toBeGreaterThan(0);

  // ── premise 2: it is genuinely windowed — otherwise there is no fix here ──
  const mounted = await list.locator(ROW).count();
  expect(
    mounted,
    "mounts far fewer rows than it offers, or nothing was optimised"
  ).toBeLessThan(total / 4);
  expect(mounted, "but enough to fill the visible window").toBeGreaterThan(4);

  // ── the scroll range still spans every code ──
  // scrollHeight is what the scrollbar is drawn from: it must account for all
  // of them, not just the mounted handful. One row pitch of slack absorbs the
  // list's own padding.
  const pitch = await list.evaluate((el) => {
    const rows = el.querySelectorAll<HTMLElement>(".ts-wording-row");
    return rows[1].offsetTop - rows[0].offsetTop;
  });
  expect(pitch, "rows must have a real, uniform pitch").toBeGreaterThan(0);
  expect(box.scrollH).toBeGreaterThanOrEqual(total * pitch - pitch);

  // ── the window tracks the scroll offset, with no drift ──
  // At an arbitrary deep offset the code sitting at the top of the viewport
  // must be the one the offset points at. This is the assertion jsdom cannot
  // make, and the one a wrong pitch or a mis-sized spacer breaks.
  const probeIndex = Math.floor(total * 0.6);
  await list.evaluate((el, top) => {
    el.scrollTop = top;
  }, probeIndex * pitch);
  // Polled, because the scroll event → React re-render that swaps the window's
  // contents lands a frame later than the scrollTop write.
  await expect
    .poll(
      () =>
        list.evaluate((el) => {
          const listTop = el.getBoundingClientRect().top;
          const rows = Array.from(
            el.querySelectorAll<HTMLElement>("[data-wording-code]")
          );
          // The first row whose bottom edge is below the top of the viewport.
          const hit = rows.find(
            (r) => r.getBoundingClientRect().bottom > listTop + 1
          );
          return hit?.getAttribute("data-wording-code") ?? null;
        }),
      {
        message:
          "the row under the scroll offset must be the row that offset points at",
      }
    )
    .toBe(MESSAGE_KEYS[probeIndex]);

  // ── the very last code is reachable, and it is a real editable input ──
  await list.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  const last = MESSAGE_KEYS[total - 1];
  const lastRow = list.locator(`[data-wording-code="${last}"]`);
  await expect(lastRow).toBeVisible();
  await lastRow.locator("input").fill("末列可編輯");
  await expect(lastRow.locator("input")).toHaveValue("末列可編輯");

  // …and scrolling back to the top brings the first code back, still editable.
  await list.evaluate((el) => {
    el.scrollTop = 0;
  });
  await expect(
    list.locator(`[data-wording-code="${MESSAGE_KEYS[0]}"]`)
  ).toBeVisible();
});

test("Tab still walks from row to row past the edge of the mounted window", async ({
  mount,
  page,
}) => {
  // The claim the overscan exists to support. Tab can only reach a row that is
  // mounted, so a window with no margin below the viewport would drop the
  // keyboard user out of the list and onto 取消 partway down. What makes it work
  // is sequential: focusing a row that has scrolled below the fold makes the
  // browser scroll it into view, which advances the window and mounts more
  // rows underneath. So this walks the list the way a keyboard user does —
  // from the first row, one Tab at a time — rather than teleporting focus to
  // the last mounted row, which is not a gesture and would prove nothing.
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<ThemeSettingsAddStory />);
  const list = await openWordingList(cmp);

  const mounted = await list.locator(ROW).count();
  // Walk well past the first window, so the test spans the boundary where an
  // overscan-less implementation would fall out of the list.
  const steps = mounted + 12;
  await list.locator(ROW).first().locator("input").focus();

  for (let i = 1; i <= steps; i++) {
    await page.keyboard.press("Tab");
    const code = await page.evaluate(
      () =>
        document.activeElement
          ?.closest("[data-wording-code]")
          ?.getAttribute("data-wording-code") ?? null
    );
    expect(
      code,
      `Tab #${i} left the 用詞 list instead of moving to the next code`
    ).toBe(MESSAGE_KEYS[i]);
  }
});

test("keeps the typed row exactly where it is, and keeps the override on scroll-away and back", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<ThemeSettingsAddStory />);
  const list = await openWordingList(cmp);

  // Type into a row partway down the visible window and watch its box, not its
  // index: an earlier attempt at this panel promoted overridden codes to the
  // top, which yanked the row out from under the caret mid-keystroke.
  const target = list.locator(ROW).nth(4);
  const code = await target.getAttribute("data-wording-code");
  // Measured against the list box, not the viewport: focusing an input makes
  // the browser scroll the PAGE, which moves every viewport coordinate without
  // the row having moved within its list at all.
  const offsetInList = () =>
    target.evaluate(
      (row) =>
        row.getBoundingClientRect().top -
        row.closest(".ts-wording-list")!.getBoundingClientRect().top
    );
  const before = await offsetInList();
  await target.locator("input").fill("甲");
  expect(
    await offsetInList(),
    "the row must not move while it is being typed into"
  ).toBeCloseTo(before, 0);
  await expect(list.locator(ROW).nth(4)).toHaveAttribute(
    "data-wording-code",
    code!
  );

  // The override survives the row being unmounted by a scroll and remounted —
  // the edit state lives above the window, not in the DOM node.
  // The blur is load-bearing: `fill` leaves the caret in that input, and a
  // FOCUSED row is deliberately kept mounted (see the pinned-row test below),
  // so without it there would be nothing to unmount and this would prove
  // nothing about where the edit state lives.
  await target.locator("input").blur();
  await list.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await expect(list.locator(`[data-wording-code="${code}"]`)).toHaveCount(0);
  await list.evaluate((el) => {
    el.scrollTop = 0;
  });
  await expect(
    list.locator(`[data-wording-code="${code}"] input`)
  ).toHaveValue("甲");
});

test("keeps the caret in the row being edited when the list scrolls past it", async ({
  mount,
  page,
}) => {
  // Windowing's one genuine regression, and the reason for the pinned row:
  // unmounting the element that holds focus makes the browser move focus to
  // <body>, so an owner who scrolls down to check another code and scrolls
  // back finds the caret gone. Measured in a real browser because that
  // hand-off is the browser's behaviour, not React's.
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<ThemeSettingsAddStory />);
  const list = await openWordingList(cmp);

  const target = list.locator(ROW).first();
  const code = (await target.getAttribute("data-wording-code"))!;
  await target.locator("input").fill("我的未存編輯");

  const focusedCode = () =>
    page.evaluate(
      () =>
        document.activeElement
          ?.closest("[data-wording-code]")
          ?.getAttribute("data-wording-code") ?? document.activeElement?.tagName ?? null
    );
  expect(await focusedCode()).toBe(code);

  // Scroll well past it — far enough that the window cannot still cover row 0.
  await list.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  // The window really did move on (a neighbour that is NOT pinned is gone).
  await expect(list.locator(`[data-wording-code="${MESSAGE_KEYS[1]}"]`)).toHaveCount(0);

  expect(
    await focusedCode(),
    "scrolling the list away must not take the caret with it"
  ).toBe(code);
  // …and it is still the live input: typing keeps landing on the same code.
  await page.keyboard.type("A");
  await expect(list.locator(`[data-wording-code="${code}"] input`)).toHaveValue(
    "我的未存編輯A"
  );

  // Scrolling back shows exactly one copy of it, sitting where it belongs.
  await list.evaluate((el) => {
    el.scrollTop = 0;
  });
  await expect(list.locator(`[data-wording-code="${code}"]`)).toHaveCount(1);
  // …and the caret survived the trip BACK too. This is a separate moment from
  // the one asserted above: coming back is when React moves the node out of
  // the pinned tail and into its in-flow position, which is exactly the kind
  // of move that silently rebuilds an element and drops focus.
  expect(
    await focusedCode(),
    "scrolling back must not take the caret either"
  ).toBe(code);
  const geom = await list.evaluate((el) => {
    const rows = Array.from(el.querySelectorAll<HTMLElement>("[data-wording-code]"));
    const first = rows[0].getBoundingClientRect();
    const second = rows[1].getBoundingClientRect();
    return { firstTop: first.top, secondTop: second.top, listTop: el.getBoundingClientRect().top, firstW: first.width, secondW: second.width };
  });
  expect(geom.firstTop - geom.listTop, "the pinned row is back in flow position").toBeCloseTo(0, 0);
  expect(geom.secondTop - geom.firstTop, "and the row after it is one pitch below").toBeGreaterThan(0);
  expect(geom.firstW, "and it is the same width as every other row").toBeCloseTo(geom.secondW, 0);
});
