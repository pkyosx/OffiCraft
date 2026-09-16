// HOTSPOT — 任務卡的步驟備註,量在真的瀏覽器裡。
//
// WHY THIS FILE IS A CT GUARD AND NOT A JSDOM TEST: these are claims about what a
// HUMAN can see. jsdom has no layout engine (every box is 0×0 and nothing is
// ever "visible"); here the assertions are boxes and hit tests.
//
// Owner (2026-08-15): 任務備注「預設不顯示」. T-6630 ④ (owner 2026-08-16):「備註
// 不是很常按,可以放在 step 的右下角,點開再跳出另一個 Modal 打開嗎?像是我們開 .md
// 檔那種方式」— the entry is the step's corner (`[data-testid='step-note-open']`)
// and the reader is portalled (`.md-preview`). A note is not on screen until
// someone asks for it.
//
// MUTANT REGISTER — five mutants, planted IN PLACE, run against all three note
// guards, observed. Counts below are this file's own (4 tests) and expire if the
// case list changes.
//   M1 · TaskCard.tsx — render the entry as a <div> instead of a <button>
//        ⇒ HERE 1 failed / 3 passed: "the note entry and its reader fit a 390px
//        phone with no page hscroll", with `.task-step__note-open missing (never
//        rendered)` — pressing a <div> collapses the whole card, so the entry it
//        then looks for is gone. Owned by taskcard-note-entry.ct.spec.tsx
//        (6 red), which names the property instead of tripping over it.
//   M2 · tasks.css `.task-step__note-open` — delete `min-height: 44px`
//        ⇒ HERE 4 passed / 0 failed. The 44px touch floor belongs to the entry
//        guard (2 red, measured 29px). Registered so nobody reads this file's
//        green as touch-target coverage.
//   M3 · TaskCard.tsx, the entry's onClick — also `.tasks` `scrollTop += 120`
//   M4 · TaskCard.tsx, the overlay's onClose — also `.tasks` `scrollTop -= 120`
//        ⇒ both 4 passed / 0 failed HERE: this story mounts bare, with no
//        `.tasks`. They are the anchor guard's mutants (5 red each).
//   M5 · TaskCard.tsx — render the entry on every step, not only on one that has
//        a note (`{step.note && (` → `{true && (`)
//        ⇒ HERE 3 failed / 1 passed, and this file OWNS the property: red are
//        "collapsed, a step WITH a note is visibly taller than one without"
//        (the two rows become the same height), "a step note is not on screen
//        until the corner entry is pressed" and "the note entry and its reader
//        fit a 390px phone" (the strict locator resolves to two entries). This
//        is the owner's reason for reading the timeline at all: while every note
//        is closed, the entry is the ONLY thing separating a step someone wrote
//        a note on from one nobody did.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardNoteDisclosureStory } from "./stories/TaskCardNoteDisclosureStory";

async function mountExpanded(mount: any, page: any, width = 1280) {
  await page.setViewportSize({ width, height: 1200 });
  const cmp = await mount(<TaskCardNoteDisclosureStory />);
  await cmp.locator(".task-card__head").first().click();
  // Non-vacuity for everything below: the card really is expanded, so the
  // surfaces the assertions look for are the ones a reader would be seeing.
  await expect(cmp.locator(".task-card__workflow")).toBeVisible();
  return cmp;
}

// The visible, non-degenerate box of a selector, or null when it has none.
async function boxOf(page: any, selector: string) {
  return page.evaluate((s: string) => {
    const el = document.querySelector(s) as HTMLElement | null;
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { w: r.width, h: r.height, x: r.x, y: r.y };
  }, selector);
}

test("the expanded card shows its title and description", async ({
  mount,
  page,
}) => {
  const cmp = await mountExpanded(mount, page);
  await expect(cmp.locator(".task-card__title")).toContainText(
    "任務卡標題不可就地編輯"
  );
  await expect(cmp.locator(".task-card__desc")).toBeVisible();
});

test("a step note is not on screen until the corner entry is pressed, and goes away again", async ({
  mount,
  page,
}) => {
  const cmp = await mountExpanded(mount, page);

  // default: neither the note text nor the reader is anywhere on the page.
  expect(await boxOf(page, ".md-preview")).toBeNull();
  expect(
    await page.evaluate(
      () => !!document.body.textContent?.includes("handler 已完成")
    ),
    "the note text must not be on screen while nobody has asked for it"
  ).toBe(false);

  const entry = cmp.locator("[data-testid='step-note-open']");
  await expect(entry).toHaveCount(1);
  await expect(entry).toBeVisible();

  await entry.click();
  const open = await boxOf(page, ".md-preview__panel");
  expect(open, "the reader must render after pressing 備註").not.toBeNull();
  expect(
    open!.h,
    "the reader must occupy real height, not a 0px shell"
  ).toBeGreaterThan(10);
  await expect(page.locator(".md-preview")).toContainText("handler 已完成");

  await page.locator(".md-preview__close").click();
  expect(await boxOf(page, ".md-preview")).toBeNull();
});

test("collapsed, a step WITH a note is visibly taller than one without", async ({
  mount,
  page,
}) => {
  // 🔴 DoD ④ — the owner reads this timeline to find out where a step got to.
  // With notes collapsed, "nobody wrote anything" and "someone wrote something
  // you cannot see" must not look identical. The story's two step names are
  // the same character length, so a height difference can only come from the
  // corner entry. Moving the note into an overlay makes this the ONLY on-card
  // trace a note leaves, which is why it survives the redesign.
  await mountExpanded(mount, page);

  const sizes = await page.evaluate(() => {
    const pick = (id: string) => {
      const el = document.querySelector(
        `[data-step-id='${id}']`
      ) as HTMLElement | null;
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { h: r.height, w: r.width };
    };
    return { withNote: pick("s-note"), noNote: pick("s-nonote") };
  });
  expect(sizes.noNote, "fixture step without a note must render").not.toBeNull();
  expect(sizes.withNote, "fixture step with a note must render").not.toBeNull();
  expect(
    sizes.withNote!.h - sizes.noNote!.h,
    "a step carrying a note must be taller than one that carries none, even collapsed"
  ).toBeGreaterThan(8);

  // …and the difference is the CONTROL, not some incidental padding: the
  // entry exists on exactly the step that has a note, with a real box.
  const which = await page.evaluate(() => {
    const has = (id: string) =>
      !!document
        .querySelector(`[data-step-id='${id}']`)!
        .querySelector("[data-testid='step-note-open']");
    const t = document.querySelector(
      "[data-testid='step-note-open']"
    ) as HTMLElement;
    const r = t.getBoundingClientRect();
    return { onNote: has("s-note"), onNoNote: has("s-nonote"), w: r.width, h: r.height };
  });
  expect(which.onNote).toBe(true);
  expect(which.onNoNote).toBe(false);
  expect(which.w).toBeGreaterThan(20);
  expect(which.h).toBeGreaterThan(8);
});

test("the note entry and its reader fit a 390px phone with no page hscroll", async ({
  mount,
  page,
}) => {
  // The note surface must not burst a phone viewport.
  // Both states are measured — a rule that holds only while the reader is shut
  // is not the rule anyone needs, and the reader is a full-viewport overlay,
  // which is exactly the shape that bursts a narrow page when it is wrong.
  const cmp = await mountExpanded(mount, page, 390);
  const hscroll = () =>
    page.evaluate(
      () =>
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth
    );
  expect(await hscroll(), "reader closed").toBeLessThanOrEqual(1);
  await cmp.locator("[data-testid='step-note-open']").click();
  await expect(page.locator(".md-preview")).toBeVisible();
  expect(await hscroll(), "reader open").toBeLessThanOrEqual(1);

  // …and neither the entry nor the reader's own body scrolls sideways inside
  // itself. `-2` scores a MISSING element as a failure, so a renamed surface
  // cannot retire the assertion quietly.
  for (const sel of [".task-step__note-open", ".md-preview__md"]) {
    const over = await page.evaluate((s: string) => {
      const el = document.querySelector(s) as HTMLElement | null;
      return el ? el.scrollWidth - el.clientWidth : -2;
    }, sel);
    expect(over, `[390px] ${sel} missing (never rendered)`).not.toBe(-2);
    expect(over, `[390px] ${sel} content overflow`).toBeLessThanOrEqual(1);
  }
});
