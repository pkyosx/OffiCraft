// The 用詞 list renders EVERY overridable code, and these are the capabilities
// that ride on that. It was briefly virtualised (T-8115, only the visible rows
// mounted); the owner reverted that on 2026-08-02 — the theme editor is opened
// rarely and themes usually arrive by import, so the measured open cost
// (~7.6ms → ~64ms, ~34ms → ~165ms on a 4x-throttled CPU) was judged not worth
// what windowing took away.
//
// Why these need a REAL browser rather than the jsdom pins in
// src/components/ThemeSettings.test.tsx: two of the three things at stake are
// browser features we do not implement. Pressing Tab and asking where focus
// went, and asking the browser's own find whether it can see a row, have no
// jsdom equivalent — over there focus never moves on its own and there is no
// find at all. jsdom covers the halves it genuinely can see (what is in the
// document, and DOM order); this file covers the rest.
//
// MUTANT for this file: put the virtualisation back (render
// `wordingRows.slice(window.first, window.last)` plus the focused-row pin).
// Measured by restoring the pre-revert implementation from a backup — and read
// the third line carefully, because one of these four does NOT reliably catch it:
//   * "every code is in the document"  → 🔴 20 rows of 866.
//   * "Tab off a scrolled-away row"    → 🔴 focus lands on the 取消 BUTTON, twice,
//                                        and the posinset series ends …865, 866, 1
//                                        with only 21 entries.
//   * "Tab walks from row to row"      → 🔶 AN UNRELIABLE, LOAD-DEPENDENT
//                                        DETECTOR. Run on its own it usually goes
//                                        green; under parallel load it fails for
//                                        real ("Tab #37 left the 用詞 list…",
//                                        Received "BUTTON"). Independent review
//                                        measured 1 red in 5 solo runs — treat
//                                        that as "intermittent", NOT as a 20%
//                                        rate; n=5 cannot support a number.
//                                        Mechanism: the windowed implementation
//                                        carried an overscan margin so a
//                                        sequential walk kept working (each focus
//                                        scrolled the next row into view, which
//                                        advanced the window) — but on a busy
//                                        machine the window does not keep up with
//                                        the focus-driven scroll.
//                                        ⚠️ On HEAD it is deterministically green
//                                        (5 solo runs + 3 whole-spec runs), so a
//                                        red here is the mutant, not a flake this
//                                        branch introduced.
//                                        Still do NOT count it towards
//                                        "windowing cannot come back" — the other
//                                        three are the reliable ones. It is kept
//                                        because it DOES deterministically catch
//                                        a hard cap: independent review built the
//                                        v1 `slice(0, 30)` mutant and it failed at
//                                        Tab #30, 3 runs of 3.
//   * "find / select-all / print"      → 🔴 the browser's find returns false for
//                                        a deep row's English original.
// These guard against one specific regression returning, so they do hold for any
// implementation that keeps every row mounted. That is the point — the invariant
// IS "every row stays in the document".
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { ThemeSettingsAddStory } from "./stories/ThemeSettingsAddStory";
import { MESSAGE_KEYS } from "../src/i18n/messageKeys.generated";
import { en } from "../src/i18n/locales/en";
import { readDictMessage } from "../src/i18n/wording";

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

/** The code of the row focus is in, or the tag name when focus left the list. */
const focusedCode = (page: Page) =>
  page.evaluate(
    () =>
      document.activeElement
        ?.closest("[data-wording-code]")
        ?.getAttribute("data-wording-code") ??
      document.activeElement?.tagName ??
      null
  );

test("keeps every one of the 866 codes in the document, and the last one editable", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<ThemeSettingsAddStory />);
  const list = await openWordingList(cmp);

  const total = MESSAGE_KEYS.length;
  expect(total, "the panel is only interesting because the set is big").toBeGreaterThan(500);

  // Premise: it is still a scroll box. All 866 rows are in the DOCUMENT, not on
  // screen at once — losing this would mean the panel grew to the full height of
  // the code set and pushed 取消 off the page.
  const box = await list.evaluate((el) => ({
    clientH: el.clientHeight,
    scrollH: el.scrollHeight,
    overflowY: getComputedStyle(el).overflowY,
  }));
  expect(box.overflowY, ".ts-wording-list must own the overflow").toBe("auto");
  expect(box.clientH).toBeGreaterThan(0);
  expect(box.scrollH, "and the content must be taller than the box").toBeGreaterThan(
    box.clientH
  );

  await expect(list.locator(ROW)).toHaveCount(total);

  // Rows have a real, uniform pitch and the scroll range spans the whole set —
  // measured, because this is the arithmetic the spacers used to fake.
  const geom = await list.evaluate((el) => {
    const rows = el.querySelectorAll<HTMLElement>(".ts-wording-row");
    return {
      pitch: rows[1].offsetTop - rows[0].offsetTop,
      firstTop: rows[0].offsetTop,
      lastTop: rows[rows.length - 1].offsetTop,
    };
  });
  expect(geom.pitch).toBeGreaterThan(0);
  expect(geom.lastTop - geom.firstTop).toBeCloseTo(geom.pitch * (total - 1), -1);

  // The last code is a real editable input, not a truncated tail.
  await list.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  const lastRow = list.locator(`[data-wording-code="${MESSAGE_KEYS[total - 1]}"]`);
  await expect(lastRow).toBeVisible();
  await lastRow.locator("input").fill("末列可編輯");
  await expect(lastRow.locator("input")).toHaveValue("末列可編輯");
});

test("Tab walks from row to row, well past the first screenful", async ({
  mount,
  page,
}) => {
  // Walked one Tab at a time from the first row, the way a keyboard user does,
  // rather than teleporting focus — teleporting proves nothing about traversal.
  // The walk has to outrun one screenful, because that is where a windowed
  // implementation runs out of mounted rows and drops the user out of the list.
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<ThemeSettingsAddStory />);
  const list = await openWordingList(cmp);

  const onScreen = await list.evaluate((el) => {
    const rows = el.querySelectorAll<HTMLElement>(".ts-wording-row");
    return Math.ceil(el.clientHeight / (rows[1].offsetTop - rows[0].offsetTop));
  });
  const steps = 40;
  expect(steps, "the walk must outrun one screenful").toBeGreaterThan(onScreen);

  await list.locator(ROW).first().locator("input").focus();
  for (let i = 1; i <= steps; i++) {
    await page.keyboard.press("Tab");
    expect(
      await focusedCode(page),
      `Tab #${i} left the 用詞 list instead of moving to the next code`
    ).toBe(MESSAGE_KEYS[i]);
  }
});

test("Tab off a row the list has scrolled away from goes to the NEXT code, and the reading order never runs backwards", async ({
  mount,
  page,
}) => {
  // The gesture windowing broke, and the reason removing it was worth doing. The
  // caret is in a row, the list has scrolled that row out of sight, and the owner
  // presses Tab. Under windowing that row was a pinned copy rendered AFTER the
  // window, so Tab left the list and landed on 取消 — and the same ordering made
  // a screen reader's virtual cursor jump from item 866 back to item 1.
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<ThemeSettingsAddStory />);
  const list = await openWordingList(cmp);

  await list.locator(ROW).first().locator("input").focus();
  await list.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  // Premise: the list really did scroll away from the focused row — otherwise
  // this is an ordinary in-view Tab and proves nothing.
  const away = await list.evaluate((el) => {
    const row = el.querySelector<HTMLElement>(".ts-wording-row")!;
    return {
      rowBottom: row.getBoundingClientRect().bottom,
      boxTop: el.getBoundingClientRect().top,
      scrollTop: el.scrollTop,
    };
  });
  expect(away.scrollTop, "the list must actually be scrolled").toBeGreaterThan(0);
  expect(
    away.rowBottom,
    "the focused row must be off the top of the visible box"
  ).toBeLessThan(away.boxTop);
  // …and the caret is still in it, because nothing was unmounted to take it.
  expect(await focusedCode(page)).toBe(MESSAGE_KEYS[0]);

  // Sequential DOM order — what a screen reader's virtual cursor walks.
  const positions = await list.evaluate((el) =>
    Array.from(el.querySelectorAll<HTMLElement>("[data-wording-code]")).map((r) =>
      Number(r.getAttribute("aria-posinset"))
    )
  );
  const backwards = positions.findIndex((p, i) => i > 0 && p < positions[i - 1]);

  await page.keyboard.press("Tab");
  const first = await focusedCode(page);
  await page.keyboard.press("Tab");
  const second = await focusedCode(page);

  // Soft: these are independent losses from one root cause, and a hard first
  // assertion would stop the others from ever being measured.
  expect
    .soft(first, "Tab from a scrolled-away row must move to the next code")
    .toBe(MESSAGE_KEYS[1]);
  expect.soft(second, "…and the walk must carry on from there").toBe(MESSAGE_KEYS[2]);
  expect
    .soft(
      backwards === -1
        ? null
        : positions.slice(Math.max(0, backwards - 2), backwards + 1),
      "reading order must not jump back to the top of the list"
    )
    .toBeNull();
  expect
    .soft(positions.length, "and it must cover the whole set")
    .toBe(MESSAGE_KEYS.length);
});

test("the browser's own find, whole-page select-all and print can see the whole list", async ({
  mount,
  page,
}) => {
  // Three capabilities the panel does not implement and cannot re-expose: the
  // browser's find bar, whole-page text selection, and print. All three read the
  // rendered document, so a windowed list showed them the visible handful only.
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<ThemeSettingsAddStory />);
  await openWordingList(cmp);

  // A row 70% of the way down — far past anything the viewport shows. Keyed on
  // its ENGLISH ORIGINAL, not its message code: the code is never rendered as
  // text, so searching for the code finds nothing either way and would make this
  // assertion meaningless.
  //
  // The needle comes from the DICTIONARY, not from the DOM. Reading it off the
  // row would make the needle disappear together with the row under a windowing
  // mutant, and this test would then red on its own setup instead of on the
  // capability it is about (measured: `needle` came back null).
  // Scan FORWARD from the 70% mark for the first code whose English original is
  // long enough to be a real search, instead of demanding that the code sitting
  // at exactly that index happens to have one. The dictionary legitimately holds
  // one-character leaves (`settings.historyVersionLabelTail` is `")"`), so which
  // code lands on the 70% index — and therefore whether this probe has a usable
  // needle at all — moves every time a key is added anywhere in the dictionary.
  // That is arithmetic luck, not a property of the list: adding 17 unrelated keys
  // slid the index onto that `")"` and red-ed this test on its own setup rather
  // than on the capability it is about. Scanning forward keeps the row deep (it
  // starts at the same 70% mark and only ever moves further down) while making
  // the needle's existence independent of the dictionary's exact length.
  // The forward scan also has to skip COMPOSE FRAGMENTS. `window.find` matches
  // against what the browser laid out, and a fragment that carries its own
  // leading/trailing whitespace (`settings.historyBlockedReasonMid` is
  // `'" is over the '`) never appears in the layout with that whitespace intact
  // — so a needle like that is unfindable no matter how long it is, and the
  // probe reddens on its own setup rather than on the capability it is about.
  // That is the same failure the length filter above was added for, one key
  // later: T-40 added one message key, the 70% index slid onto that fragment,
  // and this test went red on a change that touched nothing it guards.
  // Requiring the needle to equal its own trim keeps the probe on a whole,
  // laid-out label without pinning it to any particular key.
  const startIdx = Math.floor(MESSAGE_KEYS.length * 0.7);
  const usableNeedle = (code: string) => {
    const text = readDictMessage(en, code) ?? "";
    return text.length > 3 && text === text.trim();
  };
  const deep =
    MESSAGE_KEYS.slice(startIdx).find(usableNeedle) ?? MESSAGE_KEYS[startIdx];
  const needle = readDictMessage(en, deep) ?? "";
  expect(needle, "the probe needs a real English original to search for").toBeTruthy();
  expect(needle.length, "…and one long enough to be a real search").toBeGreaterThan(3);

  expect(
    await page.evaluate((n) => {
      window.getSelection()?.removeAllRanges();
      return (window as unknown as { find: (s: string) => boolean }).find(n);
    }, needle),
    "the browser's find must reach a row far down the list"
  ).toBe(true);

  // Select-all and print both serialise the rendered document. Asserted against
  // a floor rather than the exact 32,189 chars, so re-wording one label cannot
  // red this.
  const text = await page.evaluate(() => {
    const r = document.createRange();
    r.selectNodeContents(document.body);
    return { selectable: r.toString().length, inner: document.body.innerText.length };
  });
  expect(
    text.selectable,
    "whole-page selection must carry the list, not one screenful of it"
  ).toBeGreaterThan(20000);
  expect(text.inner).toBeGreaterThan(20000);
  expect(
    await page.evaluate((n) => document.body.innerText.includes(n), needle),
    "…and the deep row's text must be part of it"
  ).toBe(true);
});
