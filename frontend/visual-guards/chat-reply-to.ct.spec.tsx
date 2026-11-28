// GUARD (T-4e95) — 「回覆這則」的版面契約。
//
// jsdom 已經證過行為（ChatArea.reply-to.test.tsx：入口在每一列、橫幅顯示對象、
// x 不清字、送出帶對象、引用列點得回去）。這裡補的是 jsdom **量不到**的三件，
// 每一件都對應一個真的會壞掉的方式：
//
//  ① 每則訊息的工具列（回覆・複製・放大閱讀／刪除）hover 才顯形，浮在氣泡上緣、不占文字寬度，
//     顯形時文字一格都不能動，也不能蓋到第一行字。觸控裝置沒有工具列，改成長按選單（檔尾）。
//  ② 引用列與「正在回覆」橫幅的**每一行**都必須裁掉溢出。它們攜帶的是別人訊息的
//     原文，長度不受這一則控制；一旦允許折行，一句長訊息就會把版面撐開，或把輸入
//     框往下推。🔴 2026-08-22 起兩者都是**固定兩行**（第一行「寄件者 → 收件者」，
//     第二行被引的內容 —— owner 裁定），所以這裡量的是「恰好兩個行框、每半各一
//     個」，不是「總高小於一行」。行數固定，所以瞄準不同訊息時 composer 仍然不會
//     跳動 —— 那才是這一條原本要守的事。
//  ③ 引用列必須留在氣泡那一欄裡，不得溢出訊息串的可視寬度（窄視窗尤其）。
//
// 四個寬度都量：手機寬、小平板寬與桌面寬在這個元件上是不同的失敗面。375 是實機
// 最窄的那一格；720 在 1280 的餘裕之下，第一行的名字最先撐到氣泡的邊。
import { test, expect, type MountResult } from "@playwright/experimental-ct-react";
import { ChatReplyToStory } from "./stories/ChatReplyToStory";
import { ChatSelectionReplyStory } from "./stories/ChatSelectionReplyStory";
import { ChatCardTimestampStory } from "./stories/ChatCardTimestampStory";
import { ChatMessageActionsStory } from "./stories/ChatMessageActionsStory";
import { BUILTIN_THEME_IDS, expectBuiltinApplied, selectBuiltinTheme } from "./builtinThemes";
import { centreOf, preferReduced, touch } from "./touchInput";

/** Parse what getComputedStyle hands back for a colour — `rgb()`, `rgba()` or
 * the `color(srgb r g b / a)` form a `color-mix()` resolves to — into 0..255
 * channels plus alpha. Written here rather than pulled in because the ONLY
 * consumer is the contrast assertion below. */
function parseColour(v: string): [number, number, number, number] {
  const n = v.match(/-?[\d.]+(?=[\s,)/])|-?[\d.]+$/g)?.map(Number) ?? [];
  if (v.startsWith("color(")) {
    // color(srgb 0.43 0.83 0.69 / 0.55) — channels are 0..1
    const [r, g, b, a = 1] = n;
    return [r * 255, g * 255, b * 255, a];
  }
  const [r, g, b, a = 1] = n;
  return [r, g, b, a];
}

/** WCAG 2.x relative-luminance contrast, after compositing `fg` over `bg`. */
function contrast(fg: string, bg: string): number {
  const [fr, fg_, fb, fa] = parseColour(fg);
  const [br, bg_, bb] = parseColour(bg);
  const over = [fr * fa + br * (1 - fa), fg_ * fa + bg_ * (1 - fa), fb * fa + bb * (1 - fa)];
  const lum = (c: number[]) => {
    const [r, g, b] = c.map((v) => {
      const x = v / 255;
      return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
  };
  const l1 = lum(over);
  const l2 = lum([br, bg_, bb]);
  const [hi, lo] = l1 > l2 ? [l1, l2] : [l2, l1];
  return (hi + 0.05) / (lo + 0.05);
}

type Box = { x: number; y: number; width: number; height: number };

function intersects(a: Box, b: Box): boolean {
  return (
    a.x < b.x + b.width &&
    b.x < a.x + a.width &&
    a.y < b.y + b.height &&
    b.y < a.y + a.height
  );
}

/** The whole quote row is the 看原訊息 target (owner rc-4f3663c42554): the
 * overlay button's box is the row's, it never reaches the toolbar, and a click
 * at each toolbar button's centre still lands on that button. */
async function expectWholeRowTarget(
  row: import("@playwright/test").Locator,
  label: string,
): Promise<void> {
  // elementFromPoint only sees the viewport; the toolbar takes clicks only
  // while it is shown.
  await row.scrollIntoViewIfNeeded();
  await row.hover({ position: { x: 1, y: 1 } });
  const quote = (await row.locator(".chat__msg-quote").boundingBox())!;
  const jump = (await row.locator(".chat__msg-quote__jump").boundingBox())!;
  for (const k of ["x", "y", "width", "height"] as const) {
    expect(
      Math.abs(jump[k] - quote[k]),
      `${label}: the jump overlay's ${k} is not the quote row's`,
    ).toBeLessThanOrEqual(0.5);
  }
  const bubble = (await row.locator(".chat__msg-bubble").boundingBox())!;
  expect(jump.x, `${label}: jump escapes the bubble`).toBeGreaterThanOrEqual(bubble.x - 0.5);
  expect(jump.x + jump.width, `${label}: jump escapes the bubble`).toBeLessThanOrEqual(
    bubble.x + bubble.width + 0.5,
  );
  const buttons = row.locator(".chat__msg-actions > button");
  const n = await buttons.count();
  expect(n, `${label}: the row has toolbar buttons to check against`).toBeGreaterThan(0);
  for (let i = 0; i < n; i++) {
    const button = buttons.nth(i);
    const c = (await button.boundingBox())!;
    expect(intersects(jump, c), `${label}: jump overlaps toolbar button ${i}`).toBe(false);
    const hit = await button.evaluate((el, [x, y]) => {
      const at = document.elementFromPoint(x, y);
      return !!at && el.contains(at);
    }, [c.x + c.width / 2, c.y + c.height / 2] as const);
    expect(hit, `${label}: toolbar button ${i} is not what its own centre hits`).toBe(true);
  }
  const head = await row
    .locator(".chat__msg-quote__head")
    .evaluate((e) => ({ scroll: e.scrollWidth, client: e.clientWidth }));
  expect(head.scroll, `${label}: line 1 overflows instead of truncating`).toBeLessThanOrEqual(
    head.client + 1,
  );
}

for (const width of [375, 390, 720, 1280]) {
  test(`width ${width}: the toolbar is hidden at rest, revealed on hover over the bubble's top edge, and moves no text`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    for (const row of ["row-incoming", "row-mine-deletable"]) {
      const r = cmp.getByTestId(row);
      const toolbar = r.locator(".chat__msg-toolbar");
      const text = r.locator(".chat__msg-text");
      await r.scrollIntoViewIfNeeded();
      await page.mouse.move(0, 0);
      // Hidden by OPACITY, not by display/visibility: it keeps its box, so the
      // reveal cannot move anything.
      await expect(toolbar, `${row} at rest`).toHaveCSS("opacity", "0");
      await expect(toolbar, `${row} at rest`).toHaveCSS("pointer-events", "none");
      const before = (await text.boundingBox())!;

      await r.hover();
      await expect(toolbar, `${row} hovered`).toHaveCSS("opacity", "1");
      const after = (await text.boundingBox())!;
      for (const k of ["x", "y", "width", "height"] as const) {
        expect(Math.abs(after[k] - before[k]), `${row}: text ${k} moved`).toBeLessThanOrEqual(0.5);
      }
      const bar = (await toolbar.boundingBox())!;
      const bubble = (await r.locator(".chat__msg-bubble").boundingBox())!;
      expect(bar.y, `${row}: the toolbar starts above the bubble`).toBeLessThan(bubble.y);
      expect(bar.y + bar.height, `${row}: the toolbar reaches into the bubble's edge`).toBeGreaterThan(bubble.y);
      expect(bar.y + bar.height, `${row}: the toolbar covers the first line`).toBeLessThanOrEqual(after.y + 0.5);
    }
  });

  test(`width ${width}: the text runs the bubble's full content width`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    for (const row of ["row-incoming", "row-mine", "row-mine-deletable", "row-incoming-quote"]) {
      const { text, content } = await cmp
        .getByTestId(row)
        .locator(".chat__msg-text")
        .evaluate((e) => ({
          text: e.getBoundingClientRect().width,
          // 13px of bubble padding on each side and nothing else.
          content: e.closest(".chat__msg-bubble")!.clientWidth - 26,
        }));
      expect(Math.abs(text - content), `${row}: text ${text} vs bubble content ${content}`).toBeLessThanOrEqual(0.5);
    }
  });

  test(`width ${width}: the quote row is two lines, each clipped, and stays inside the thread`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const quote = cmp.getByTestId("quote-row");
    const bubble = cmp.getByTestId("row-mine").locator(".chat__msg-bubble");
    const quoteBox = (await quote.boundingBox())!;
    const bubbleBox = (await bubble.boundingBox())!;

    // 🔴 TWO LINES, AND EXACTLY TWO. Owner ruling 2026-08-22 split this row —
    // 「誰跟誰說話」 on one line, the quoted sentence on the next — because the
    // two were competing for one line's width and the sentence was losing (see
    // `.chat__msg-quote` in office.css for the measurement that forced it).
    // This assertion used to read `< 30` for one line; it is not a relaxation to
    // two, because the two LINE BOXES are counted individually below. The
    // quoted text is long enough to wrap to three or four lines if anything let
    // it, so a third line is still the failure.
    //
    // Measured in this harness at all four widths: 38.7px (the text's 36.7 plus
    // the 2px the hover wash borrows from the margin). 46 is a ceiling with
    // room for a font, not a reading of a declaration.
    expect(quoteBox.height).toBeLessThan(46);
    expect(quoteBox.height).toBeGreaterThan(24);
    const boxes = await quote.evaluate((el) => ({
      head: (
        el.querySelector(".chat__msg-quote__head") as HTMLElement
      ).getClientRects().length,
      body: (
        el.querySelector(".chat__msg-quote__body") as HTMLElement
      ).getClientRects().length,
    }));
    expect(boxes.head, "line 1 (who → whom) must be ONE line box").toBe(1);
    expect(boxes.body, "line 2 (the sentence) must be ONE line box").toBe(1);

    // INSIDE the bubble, not floating beside it (owner 2026-08-20). Measured
    // rather than asserted on the DOM: a quote nested in the markup but pulled
    // out visually would still read as a separate strip, which is the actual
    // complaint.
    expect(quoteBox.x).toBeGreaterThanOrEqual(bubbleBox.x - 0.5);
    expect(quoteBox.x + quoteBox.width).toBeLessThanOrEqual(
      bubbleBox.x + bubbleBox.width + 0.5,
    );
    expect(quoteBox.y).toBeGreaterThanOrEqual(bubbleBox.y - 0.5);

    await expectWholeRowTarget(cmp.getByTestId("row-mine"), "row-mine");
    await expectWholeRowTarget(cmp.getByTestId("row-incoming-quote"), "row-incoming-quote");

    // The hover wash is painted by the ROW, under the text, and only on hover.
    for (const id of ["quote-row", "quote-row-incoming"]) {
      const bg = () =>
        cmp.getByTestId(id).evaluate((e) => getComputedStyle(e).backgroundColor);
      await page.mouse.move(0, 0);
      await expect.poll(bg, { message: `${id} is washed at rest` }).toBe("rgba(0, 0, 0, 0)");
      await cmp.getByTestId(id).hover();
      await expect
        .poll(async () => parseColour(await bg())[3], {
          message: `${id} shows no wash on hover`,
        })
        .toBeGreaterThan(0.05);
    }
  });

  test(`width ${width}: a short sender name in the quote row is never ellipsised`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const who = cmp.getByTestId("quote-who-short");
    const body = cmp.getByTestId("quote-body-short");

    // POSITIVE CONTROL first: the row really is tight enough to force a cut.
    // Without this the assertion below would also pass on a row with acres of
    // spare width — i.e. it would guard nothing.
    const bodyCut = await body.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 0.5,
    );
    expect(bodyCut).toBe(true);

    // 🔴 AND THE NAME SURVIVES IT. Two shrinkable items shrink in proportion,
    // so the quoted text taking a cut used to drag the name down with it —
    // 「Mira」 rendered as 「M…」 with room to spare. The name answers WHO;
    // the quoted text is the part that may be trimmed.
    const whoCut = await who.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 0.5,
    );
    expect(whoCut).toBe(false);
    expect((await who.textContent())?.trim()).toBe("Mira → 韓立");
  });

  test(`width ${width}: a name is never cut while the excerpt still fits`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const who = cmp.getByTestId("quote-who-tight");
    const body = cmp.getByTestId("quote-body-tight");

    // POSITIVE CONTROL, the other way round from the test above: this row has
    // room to spare, so NOTHING on it should be cut. If the excerpt were being
    // trimmed the row would be genuinely tight and the name's fate would say
    // nothing about the rule.
    const bodyCut = await body.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 0.5,
    );
    expect(bodyCut).toBe(false);

    // 🔴 SO THE NAME MUST BE WHOLE. A `max-width` in PERCENT does not survive
    // shrink-to-fit: the bubble is sized from intrinsic width, where a
    // percentage cap is ignored, and the cap then clips the name inside a
    // bubble that was measured to hold it. The result was a name losing
    // characters at 1600px next to a one-word excerpt — a shortage the layout
    // invented. Whatever keeps long names inside the bubble, it must not fire
    // when the row is not full.
    const whoCut = await who.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 0.5,
    );
    expect(whoCut).toBe(false);
    expect((await who.textContent())?.trim()).toBe("ow-8808ccf51794 → 韓立");
  });

  for (const theme of BUILTIN_THEME_IDS) {
  test(`width ${width} ${theme}: the toolbar's icons read at 3:1 on the toolbar, which stands off both bubbles`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    await selectBuiltinTheme(page, theme);
    const cmp = await mount(<ChatReplyToStory />);
    await expectBuiltinApplied(page, theme);

    for (const [row, ids] of [
      ["row-incoming", ["reply-entry-incoming", "copy-entry-incoming", "expand-entry-incoming"]],
      ["row-mine-deletable", ["reply-entry-mine-deletable", "copy-entry-mine-deletable", "delete-entry-mine"]],
    ] as const) {
      await cmp.getByTestId(row).hover();
      await page.waitForTimeout(250);
      for (const id of ids) {
        const { ink, ground } = await cmp.getByTestId(id).evaluate((e) => ({
          ink: getComputedStyle(e).color,
          ground: getComputedStyle(e.closest(".chat__msg-toolbar")!).backgroundColor,
        }));
        expect(parseColour(ground)[3], `${id}: the toolbar has its own ground`).toBe(1);
        expect(contrast(ink, ground), `${id}: icon vs toolbar`).toBeGreaterThanOrEqual(3);
      }
      // Its ground is not the bubble's own colour, or the half that overlaps
      // the bubble would melt into it (the incoming bubble is card-coloured).
      const edge = await cmp.getByTestId(row).locator(".chat__msg-toolbar").evaluate((e) => ({
        ground: getComputedStyle(e).backgroundColor,
        bubble: getComputedStyle(e.closest(".chat__msg-bubble")!).backgroundColor,
      }));
      expect(
        contrast(edge.ground, edge.bubble),
        `${row}: the toolbar's ground is its bubble's`,
      ).toBeGreaterThan(1.05);
    }
  });
  }

  test(`width ${width}: the 正在回覆 banner is two clipped lines and its x stays reachable`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const banner = cmp.getByTestId("chat-reply-banner");
    const bannerBox = (await banner.boundingBox())!;
    // 🔴 TWO LINES SINCE 2026-08-22, AND THE RULE IT ENFORCES DID NOT CHANGE.
    // The banner is the composer's own copy of the quote row and it was split
    // the same way (owner ruling; see `.chat__reply-banner__text`). What this
    // test was always defending is that the composer does not GROW OR JUMP as
    // the owner aims at different messages — and a fixed two lines does not
    // jump. Each half is still clipped to exactly one line box, which is
    // asserted per element below rather than inferred from the total height.
    //
    // 🔴 AND THE FIXTURE'S BODY REALLY CONTAINS NEWLINES (see the story). That
    // matters because the browser is the only thing collapsing them now: the
    // `oneLine()` helper that used to pre-collapse this text in <ChatArea> was
    // deleted on 2026-08-21 as unreachable, and this assertion is what makes
    // `white-space: nowrap` on `.chat__reply-banner__text` load-bearing rather
    // than decorative. Deleting that declaration turns this test red; with a
    // single-line fixture it would only have been measuring the clipping.
    const raw = await cmp
      .getByTestId("chat-reply-banner")
      .locator(".chat__reply-banner__body")
      .evaluate((e) => e.textContent ?? "");
    expect(
      raw,
      "the fixture must carry the newlines this test claims to survive",
    ).toContain("\n");
    // ONE line box, not merely a short box: a 34px height could also be a
    // one-line body that happened to fit. Client rects count the line boxes.
    const lineBoxes = await cmp
      .getByTestId("chat-reply-banner")
      .locator(".chat__reply-banner__body")
      .evaluate((e) => (e as HTMLElement).getClientRects().length);
    expect(lineBoxes, "the banner body must lay out as ONE line box").toBe(1);
    const whoBoxes = await cmp
      .getByTestId("chat-reply-banner")
      .locator(".chat__reply-banner__who")
      .evaluate((e) => (e as HTMLElement).getClientRects().length);
    expect(whoBoxes, "the banner who must lay out as ONE line box").toBe(1);
    // TWO of them and no more. Measured here at all four widths: 42.4px (34px
    // when this was one line). A third line means something wrapped.
    expect(bannerBox.height).toBeLessThan(52);
    expect(bannerBox.height).toBeGreaterThan(36);
    expect(bannerBox.x + bannerBox.width).toBeLessThanOrEqual(width + 1);

    // The x must remain a real, hittable control at BOTH widths — it is the
    // only way back to the ordinary send state.
    const x = cmp.getByTestId("reply-banner-x");
    const xBox = (await x.boundingBox())!;
    expect(xBox.width).toBeGreaterThanOrEqual(20);
    expect(xBox.height).toBeGreaterThanOrEqual(20);
    expect(xBox.x + xBox.width).toBeLessThanOrEqual(
      bannerBox.x + bannerBox.width + 0.5,
    );
  });
}

// 🔴 A LONG DISPLAY NAME MUST NOT EAT THE BANNER. The name half was `flex: none`
// until r15 — the same assumption that failed on the quote row and took three
// tries to get right there. Display names are free text the owner types, so an
// unshrinkable name is hard-cut by the parent's `overflow: hidden`, with no
// ellipsis to admit it.
//
// 🔴 AND THE PREMISE OF THE SECOND HALF OF THIS TEST CHANGED ON 2026-08-22.
// Until then the name and the excerpt shared ONE line, and what this loop
// asserted was the arbitration between them: an asymmetric shrink factor
// (excerpt 10000, name 1) meant the excerpt absorbed the whole deficit and the
// name only gave once the excerpt had nothing left — stated here as the
// implication "if the name was truncated, the excerpt must already be at zero".
// The banner is two lines now (owner ruling), so there is no deficit to share:
// each half owns the full width of the text column. The implication is not
// weakened, it is VACUOUS — the excerpt is never at zero, so the old assertion
// could only ever pass by the name never being cut, which is not the rule. What
// replaces it is the STRONGER statement the split actually buys: however long
// the name gets, the excerpt still has the whole line.
//
// The story's default name is four characters, which is why this needs its own
// mount: the existing banner test cannot see this failure at all. 1200 and 1250
// are where the earlier version of this assertion was measured red on untouched
// production CSS; they are kept because they are the middle of the band.
for (const width of [390, 1200, 1250, 1280]) {
  test(`width ${width}: a long display name is ellipsised inside its own line and takes nothing from the excerpt`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    // ⚠️ THE FIXTURE NAME IS REPEATED AND THAT IS NOT DECORATION. Measured here:
    // one copy comes to exactly the text column's width at viewport 1280 (856px
    // = 856px) and two copies to exactly it again (970 = 970), so the positive
    // control below could not fire at the three wide widths and the test would
    // have been asserting the rule on a name under no pressure at all. Four
    // copies put the name past the column at every width in this list, with room
    // that is not a rounding coincidence.
    const longName =
      "一個非常非常長的顯示名稱這是負責人自己在設定裡打進去的沒有任何上限也沒有人會攔他";
    const cmp = await mount(
      <ChatReplyToStory bannerWho={`正在回覆 ${longName.repeat(4)}`} />,
    );

    const banner = cmp.getByTestId("chat-reply-banner");
    // 🔴 THE ASSERTION THAT ACTUALLY SEES IT, and it took two tries to find.
    // The first version of this test measured bounding boxes and passed under
    // the very mutant it was written for — `overflow: hidden` on the parent
    // clips the PAINT, so a name that has run past its container still reports
    // a box inside it. What does move is the parent's own scrollWidth: an
    // unshrinkable name makes the text column wider than the space it has.
    const overflow = await banner.evaluate((el) => {
      const t = el.querySelector(".chat__reply-banner__text") as HTMLElement;
      return { client: t.clientWidth, scroll: t.scrollWidth };
    });
    expect(
      overflow.scroll,
      "the text column must not be wider than the space it has",
    ).toBeLessThanOrEqual(overflow.client + 1);

    const share = await banner.evaluate((el) => {
      const w = el.querySelector(".chat__reply-banner__who") as HTMLElement;
      const b = el.querySelector(".chat__reply-banner__body") as HTMLElement;
      const t = el.querySelector(".chat__reply-banner__text") as HTMLElement;
      return {
        whoClient: w.clientWidth,
        whoScroll: w.scrollWidth,
        body: b.clientWidth,
        column: t.clientWidth,
      };
    });
    // POSITIVE CONTROL: the name really is under pressure at this width — this
    // fixture is long enough that it cannot fit anywhere in the list, so a
    // version of the banner where it DID fit would be measuring nothing.
    expect(
      share.whoScroll,
      "the fixture name must be longer than the line it is given",
    ).toBeGreaterThan(share.whoClient);

    // 🔴 AND THE EXCERPT IS UNTOUCHED BY IT. This is the whole point of the
    // split: the name's overflow is now the name's problem. Before it, at 390
    // this same fixture drove the excerpt to 0px — the banner named someone and
    // then said nothing about what they had said.
    expect(
      share.body,
      "the excerpt keeps the whole line however long the name is",
    ).toBe(share.column);
    expect(share.body).toBeGreaterThan(0);
  });
}

// The whole-row target against the toolbar, on every quote row including the
// incoming one with a body (three buttons).
for (const width of [300, 320, 336, 360, 390, 600, 640, 760]) {
  test(`width ${width}: the whole quote row is the jump target and never reaches the toolbar`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    for (const row of [
      "row-mine",
      "row-mine-short",
      "row-mine-tight",
      "row-incoming-quote",
      "row-mine-pane347",
    ]) {
      await expectWholeRowTarget(cmp.getByTestId(row), row);
    }
  });
}

// 「拿不到這則訊息」 lands on a row the overlay covers; it must be painted
// above the overlay, not under it.
for (const width of [320, 1280]) {
  test(`width ${width}: the failure notice on line 1 sits above the overlay`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory quoteError />);

    const error = cmp.getByTestId("quote-error-incoming");
    await expect(error).toBeVisible();
    const e = (await error.boundingBox())!;
    expect(e.width, "the failure notice was squeezed away").toBeGreaterThan(20);
    const hit = await error.evaluate((el, [x, y]) => {
      const at = document.elementFromPoint(x, y);
      return !!at && el.contains(at);
    }, [e.x + e.width / 2, e.y + e.height / 2] as const);
    expect(hit, "the overlay paints over the failure notice").toBe(true);
    await expectWholeRowTarget(cmp.getByTestId("row-incoming-quote"), "row-incoming-quote");
  });
}

// A one-character quote must survive whole at the narrow end — there is no
// version of "not enough room" that justifies eating it — and the jump must
// still be there: deleting the control would satisfy the first half too.
for (const width of [320, 360, 390]) {
  test(`width ${width}: a one-character quote is never trimmed and the jump is still there`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const cut = await cmp
      .getByTestId("quote-body-incoming")
      .evaluate((e) => e.scrollWidth > e.clientWidth + 0.5);
    expect(cut, "a one-character quote was trimmed away").toBe(false);

    const jump = (await cmp.getByTestId("quote-jump-short").boundingBox())!;
    const row = (await cmp.getByTestId("quote-row-short").boundingBox())!;
    expect(jump.width).toBeGreaterThanOrEqual(row.width - 0.5);
    expect(jump.height).toBeGreaterThanOrEqual(row.height - 0.5);
    await expect(cmp.getByTestId("quote-jump-short")).toHaveJSProperty(
      "tagName",
      "BUTTON",
    );
  });
}

// ── PANE 347px — NARROW SHELL GEOMETRY ──────────────────────────────────────
// At vw 721, the shell's 264px roster reduces the pane from 628px to 347px.
// overflow:hidden can clip every character while leaving the element box intact.

test("pane 347px: the quoted sentence still has characters (the width the shell hands the thread at vw 721)", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 395, height: 900 });
  const cmp = await mount(<ChatReplyToStory />);

  const seen = await cmp
    .getByTestId("quote-body-pane347")
    .evaluate((el) => {
      const pane = document.querySelector(".chat__messages") as HTMLElement;
      const node = el.firstChild;
      const clip = el.getBoundingClientRect();
      let chars = 0;
      if (node && node.nodeType === 3) {
        const range = document.createRange();
        const text = node.textContent ?? "";
        for (let i = 0; i < text.length; i++) {
          range.setStart(node, i);
          range.setEnd(node, i + 1);
          const r = range.getBoundingClientRect();
          if (r.width > 0 && r.right <= clip.right + 0.5) chars++;
        }
      }
      return { chars, total: (el.textContent ?? "").length, paneW: pane.clientWidth };
    });

  expect(seen.paneW, "the viewport must put the pane at 347px").toBe(347);

  expect(
    seen.chars,
    `pane ${seen.paneW}px: the quoted sentence must not be squeezed to nothing`,
  ).toBeGreaterThan(0);

  const geom = await cmp.getByTestId("quote-row-pane347").evaluate((row) => {
    const b = row.querySelector(".chat__msg-quote__body") as HTMLElement;
    const h = row.querySelector(".chat__msg-quote__head") as HTMLElement;
    const cs = getComputedStyle(row);
    return {
      bodyW: b.clientWidth,
      rowW:
        row.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight),
      headBoxes: h.getClientRects().length,
      bodyBoxes: b.getClientRects().length,
    };
  });
  expect(geom.bodyW, "the sentence gets the full row width").toBe(geom.rowW);
  expect(geom.headBoxes).toBe(1);
  expect(geom.bodyBoxes).toBe(1);
});

test("narrow 390: the reply entry and the banner x are focusable native buttons", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 800 });
  const cmp = await mount(<ChatReplyToStory />);

  for (const id of ["reply-entry-incoming", "copy-entry-incoming", "reply-banner-x", "quote-jump"]) {
    const control = cmp.getByTestId(id);
    await expect(control).toHaveJSProperty("tagName", "BUTTON");
    await control.focus();
    await expect(control).toBeFocused();
  }
  // Keyboard focus outlines the whole quote: the overlay is the row's box.
  await page.keyboard.press("Shift+Tab");
  await page.keyboard.press("Tab");
  const jump = cmp.getByTestId("quote-jump");
  await expect(jump).toBeFocused();
  await expect(jump).not.toHaveCSS("outline-style", "none");
});

// A failed copy shows its words in the toolbar over the bubble's top edge.
// `.chat__messages` clips sideways (overflow-x: hidden), so a label that grew
// past the thread's edge would not scroll the page — it would just be cut off
// mid-word. (It can still be scrolled programmatically, which is why nothing
// here calls scrollIntoView: that would slide the thread sideways and hide the
// cut.) Seasonal labels sit within a few px of the 96px cap, so whether they
// wrap is up to the runner's fonts; wrapping is the intended fallback, so only
// the plain wordings are held to one line. A copy that worked shows nothing
// there: its check is the copy button's own icon.
const COPY_STATUS_ROWS = [
  ["copy-status-incoming", "row-incoming-copy-result"],
  ["copy-status-incoming-short", "row-incoming-short-copy-result"],
  ["copy-status-mine", "row-mine-copy-result"],
] as const;
for (const width of [320, 390, 1440]) {
  for (const [wording, label, oneLine] of [
    ["zh", "複製失敗", true],
    ["en", "Copy failed", true],
    ["thanksgiving zh", "複製失敗 🍂", false],
    ["thanksgiving en", "Copy failed 🍂", false],
    ["christmas zh", "複製失敗 🎄", false],
    ["christmas en", "Copy failed 🎄", false],
    ["long theme wording", "Couldn't copy — it slipped out of the basket", false],
  ] as const) {
    test(`width ${width} ${wording} failed: the copy result stays whole inside the thread and moves no text`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 800 });
      const textInBubble = (row: import("@playwright/test").Locator) =>
        row.locator(".chat__msg-text").evaluate((e) => {
          const t = e.getBoundingClientRect();
          const b = e.closest(".chat__msg-bubble")!.getBoundingClientRect();
          return { dx: t.x - b.x, dy: t.y - b.y, right: b.right - t.right };
        });
      const plain = await mount(<ChatReplyToStory copyResult="failed" copyLabel="x" />);
      const before = {
        incoming: await textInBubble(plain.getByTestId("row-incoming-copy-result")),
        mine: await textInBubble(plain.getByTestId("row-mine-copy-result")),
      };
      await plain.unmount();

      const cmp = await mount(<ChatReplyToStory copyResult="failed" copyLabel={label} />);
      const geom = await cmp.getByTestId("chat-messages").evaluate((t) => {
        const r = t.getBoundingClientRect();
        const left = r.x + t.clientLeft;
        const labels = ["copy-status-incoming", "copy-status-incoming-short", "copy-status-mine"].map((id) => {
          const e = t.querySelector(`[data-testid="${id}"]`)!;
          const b = e.getBoundingClientRect();
          return { id, x: b.x, right: b.right, lines: Math.round(b.height / parseFloat(getComputedStyle(e).lineHeight)) };
        });
        return {
          scrollLeft: t.scrollLeft,
          left,
          right: left + t.clientWidth,
          overflow: t.scrollWidth - t.clientWidth,
          labels,
        };
      });
      expect(geom.scrollLeft).toBe(0);
      for (const l of geom.labels) {
        expect(l.x, `${l.id} cut at the thread's left edge`).toBeGreaterThanOrEqual(geom.left - 0.5);
        expect(l.right, `${l.id} cut at the thread's right edge`).toBeLessThanOrEqual(geom.right + 0.5);
        if (oneLine) expect(l.lines, `${l.id} wraps`).toBe(1);
      }
      expect(geom.overflow, "the thread grew sideways").toBeLessThanOrEqual(0);
      for (const [status, row] of COPY_STATUS_ROWS) {
        const l = (await cmp.getByTestId(status).boundingBox())!;
        const text = (await cmp.getByTestId(row).locator(".chat__msg-text").boundingBox())!;
        expect(l.y + l.height, `${status} covers the message`).toBeLessThanOrEqual(text.y + 0.5);
        await expect(cmp.getByTestId(status)).toHaveText(label);
        // Shown while the toolbar's buttons are not: nothing between it and
        // the bubble fades.
        const shown = await cmp.getByTestId(status).evaluate((e) => {
          let alpha = 1;
          for (let n: Element | null = e; n && !n.matches(".chat__msg"); n = n.parentElement)
            alpha *= Number(getComputedStyle(n).opacity);
          return alpha;
        });
        expect(shown, status).toBe(1);
      }
      expect(await textInBubble(cmp.getByTestId("row-incoming-copy-result"))).toEqual(before.incoming);
      expect(await textInBubble(cmp.getByTestId("row-mine-copy-result"))).toEqual(before.mine);
    });
  }

  test(`width ${width} copied: nothing is shown beside the toolbar, and it stays hidden at rest`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory copyResult="copied" copyLabel="已複製" />);
    for (const [status, row] of COPY_STATUS_ROWS) {
      await expectScreenReaderOnly(cmp.getByTestId(status), "已複製");
      await expect(cmp.getByTestId(row).locator(".chat__msg-toolbar"), row).toHaveCSS("opacity", "0");
    }
  });
}

// The live <ChatArea>: the toolbar on real rows, the reply-card row, and the
// long-press menu.
const SHOTS = process.env.CHAT_ACTIONS_SHOTS;

async function mountThread(
  mount: (component: React.JSX.Element) => Promise<MountResult>,
  page: import("@playwright/test").Page,
  width: number,
  height: number,
  props: { cardReadFails?: boolean; cardsWithoutTask?: boolean; tallMessage?: boolean } = {},
) {
  await page.setViewportSize({ width, height });
  const cmp = await mount(<ChatMessageActionsStory withRoster={width > 720} {...props} />);
  await expect(page.locator('[data-msg-id="m-card"]')).toBeVisible();
  return cmp;
}

const rowAt = (page: import("@playwright/test").Page, id: string) =>
  page.locator(`[data-msg-id="${id}"]`);

/** A copy that worked shows only a check; its words are there for a screen
 * reader, in a 1px box. */
async function expectCheckOnly(result: import("@playwright/test").Locator, words: string) {
  await expect(result.locator("svg")).toBeVisible();
  await expect(result.locator(".chat__sr-text")).toHaveText(words);
  expect((await result.locator(".chat__sr-text").boundingBox())!.width).toBeLessThanOrEqual(1);
  expect(
    await result.evaluate((e) => {
      const shown = e.cloneNode(true) as Element;
      shown.querySelectorAll(".chat__sr-text").forEach((n) => n.remove());
      return shown.textContent!.trim();
    }),
    "words shown beside the check",
  ).toBe("");
}

/** A copy result only a screen reader gets: its words in a 1px box, nothing
 * else drawn. */
async function expectScreenReaderOnly(status: import("@playwright/test").Locator, words: string) {
  await expect(status).toHaveText(words);
  await expect(status.locator(".chat__sr-text")).toHaveText(words);
  expect(await status.locator("svg").count(), "an icon beside the toolbar").toBe(0);
  expect((await status.boundingBox())!.width, "a box beside the toolbar").toBeLessThanOrEqual(1);
  expect((await status.locator(".chat__sr-text").boundingBox())!.width).toBeLessThanOrEqual(1);
}

/** A toolbar copy that worked: the copy button itself is the check, and the
 * result beside it is for a screen reader only. */
async function expectCopiedOnTheButtonOnly(row: import("@playwright/test").Locator) {
  await expect(row.locator(".chat__msg-copy")).toHaveAttribute("aria-label", "已複製");
  await expect(row.locator('.chat__msg-copy svg path[d="M20 6 9 17l-5-5"]')).toHaveCount(1);
  await expectScreenReaderOnly(row.getByRole("status"), "已複製");
}

/** The focused box would make iOS zoom the page in, after which it pans
 * sideways. */
async function expectNoZoomOnFocus(page: import("@playwright/test").Page) {
  const size = await page.evaluate(() => parseFloat(getComputedStyle(document.activeElement!).fontSize));
  expect(size, "the focused box's font size").toBeGreaterThanOrEqual(16);
  expect(
    await page.evaluate(() => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth),
    "the page is wider than the screen",
  ).toBeLessThanOrEqual(0);
}

/** The messages whose toolbar shows its buttons right now. */
function toolbarsShown(page: import("@playwright/test").Page) {
  return page.locator(".chat__msg").evaluateAll((rows) =>
    rows
      .filter((r) => {
        const bar = r.querySelector(".chat__msg-toolbar");
        const actions = r.querySelector(".chat__msg-actions");
        return (
          !!bar &&
          !!actions &&
          getComputedStyle(bar).opacity === "1" &&
          getComputedStyle(actions).display !== "none"
        );
      })
      .map((r) => r.getAttribute("data-msg-id")),
  );
}

for (const [width, height] of [
  [320, 700],
  [390, 844],
  [1440, 790],
] as const) {
  test(`live thread ${width}: hovering a message shows its toolbar on the bubble's edge, incoming and own alike, and nothing moves`, async ({
    mount,
    page,
  }) => {
    await mountThread(mount, page, width, height);
    const textBox = async (id: string) =>
      (await rowAt(page, id).locator(".chat__msg-text").boundingBox())!;
    const scroller = (await page.locator(".chat__messages").boundingBox())!;

    for (const id of ["m-long", "m-short", "m-mine", "m-mine-short", "m-mention", "m-card"]) {
      const row = rowAt(page, id);
      const toolbar = row.locator(".chat__msg-toolbar");
      await row.scrollIntoViewIfNeeded();
      await page.mouse.move(0, 0);
      await expect(toolbar, `${id} at rest`).toHaveCSS("opacity", "0");
      const before = id === "m-card" ? null : await textBox(id);

      await row.hover({ position: { x: 2, y: 2 } });
      await expect(toolbar, `${id} hovered`).toHaveCSS("opacity", "1");
      expect(await toolbarsShown(page), "one toolbar at a time").toEqual([id]);
      const bar = (await toolbar.boundingBox())!;
      expect(bar.x, `${id}: toolbar cut at the thread's left`).toBeGreaterThanOrEqual(scroller.x - 0.5);
      expect(bar.x + bar.width, `${id}: toolbar cut at the thread's right`).toBeLessThanOrEqual(
        scroller.x + scroller.width + 0.5,
      );
      const name = row.locator(".chat__msg-name");
      if (await name.count()) {
        const n = (await name.boundingBox())!;
        expect(intersects(bar, n), `${id}: the toolbar covers the sender's name`).toBe(false);
      }
      if (before) {
        const after = await textBox(id);
        for (const k of ["x", "y", "width", "height"] as const) {
          expect(Math.abs(after[k] - before[k]), `${id}: text ${k} moved`).toBeLessThanOrEqual(0.5);
        }
        const first = (await row
          .locator(".chat__msg-quote, .chat__msg-text")
          .first()
          .boundingBox())!;
        expect(bar.y + bar.height, `${id}: the toolbar covers the first line`).toBeLessThanOrEqual(
          first.y + 0.5,
        );
      } else {
        const stub = (await row.locator(".reply-card__collapsed-row").boundingBox())!;
        expect(bar.y + bar.height, "m-card: the toolbar covers the card's row").toBeLessThanOrEqual(
          stub.y + 0.5,
        );
      }
    }
    expect(
      await rowAt(page, "m-mine").locator(".chat__msg-actions button").evaluateAll((b) =>
        b.map((e) => e.getAttribute("aria-label")),
      ),
    ).toEqual(["回覆這則", "複製訊息", "刪除"]);
    expect(
      await rowAt(page, "m-card").locator(".chat__msg-actions button").evaluateAll((b) =>
        b.map((e) => e.getAttribute("aria-label")),
      ),
    ).toEqual(["回覆這則", "複製訊息"]);

    // The incoming multi-line message uses the bubble's whole content width.
    const text = await rowAt(page, "m-long").locator(".chat__msg-text").evaluate((e) => ({
      text: e.getBoundingClientRect().width,
      content: e.closest(".chat__msg-bubble")!.clientWidth - 26,
    }));
    expect(Math.abs(text.text - text.content)).toBeLessThanOrEqual(0.5);
    test.info().annotations.push({ type: "m-long text width", description: String(text.text) });

    if (SHOTS) {
      await rowAt(page, "m-long").scrollIntoViewIfNeeded();
      await rowAt(page, "m-long").hover({ position: { x: 2, y: 2 } });
      await page.waitForTimeout(300);
      await page.screenshot({ path: `${SHOTS}/${width}x${height}-hover-incoming.png` });
      await rowAt(page, "m-short").locator(".chat__msg-bubble").hover();
      await page.waitForTimeout(300);
      await page.screenshot({ path: `${SHOTS}/${width}x${height}-hover-incoming-short.png` });
      await rowAt(page, "m-mine").locator(".chat__msg-bubble").hover();
      await page.waitForTimeout(300);
      await page.screenshot({ path: `${SHOTS}/${width}x${height}-hover-own.png` });
    }
  });

  test(`live thread ${width}: a click leaves no toolbar behind; keyboard focus shows one`, async ({
    mount,
    page,
  }) => {
    await mountThread(mount, page, width, height);
    const short = rowAt(page, "m-short");
    await short.scrollIntoViewIfNeeded();
    await short.locator(".chat__msg-bubble").hover();
    await short.getByRole("button", { name: "複製訊息" }).click();
    await expectCopiedOnTheButtonOnly(short);
    if (SHOTS && width !== 390)
      await page.screenshot({ path: `${SHOTS}/${width}x${height}-copied-short.png` });

    await rowAt(page, "m-mine").locator(".chat__msg-bubble").hover();
    await page.waitForTimeout(200);
    expect(await toolbarsShown(page)).toEqual(["m-mine"]);
    await page.mouse.move(0, 0);
    // Within the result's 1.6s: a success holds nothing up after the pointer leaves.
    await expect(short.locator(".chat__msg-toolbar")).toHaveCSS("opacity", "0", { timeout: 500 });
    await expect(short.getByRole("status")).toHaveText("已複製", { timeout: 0 });

    // A dialog handing focus back to the button that opened it is no reveal.
    const long = rowAt(page, "m-long");
    await long.scrollIntoViewIfNeeded();
    await long.locator(".chat__msg-bubble").hover();
    await long.getByRole("button", { name: "放大閱讀" }).click();
    await page.getByRole("button", { name: "關閉預覽" }).click();
    await page.mouse.move(0, 0);
    await page.waitForTimeout(200);
    await expect(long.locator(".chat__msg-toolbar")).toHaveCSS("opacity", "0");

    await page.keyboard.press("Tab");
    await long.getByRole("button", { name: "回覆這則" }).focus();
    await expect(long.locator(".chat__msg-toolbar")).toHaveCSS("opacity", "1");
  });
}

test("live thread 1440: a selection running through a mention and a link offers 回覆這段", async ({
  mount,
  page,
}) => {
  await mountThread(mount, page, 1440, 790);
  const text = rowAt(page, "m-mention").locator(".chat__msg-text");
  await expect(text.locator(".md-mention")).toHaveText("@Mira");
  const t = (await text.boundingBox())!;
  await page.mouse.move(t.x + 1, t.y + t.height / 2);
  await page.mouse.down();
  await page.mouse.move(t.x + t.width - 1, t.y + t.height / 2, { steps: 8 });
  await page.mouse.up();
  const reply = page.getByTestId("chat-selection-reply");
  await expect(reply).toBeVisible();
  await reply.click();
  await expect(page.locator(".chat__reply-banner__body")).toHaveText("請 @Mira 先看 這份計畫 再回我");
});

test("live thread 1440: 複製 on a reply card writes its question, and a failed read writes nothing", async ({
  mount,
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const clipboard = () => page.evaluate(() => navigator.clipboard.readText());
  const copyCard = async () => {
    const card = rowAt(page, "m-card");
    await card.scrollIntoViewIfNeeded();
    await card.locator(".reply-card").hover();
    await card.getByRole("button", { name: "複製訊息" }).click();
    return card.getByRole("status");
  };
  expect(await page.evaluate(() => typeof ClipboardItem)).toBe("function");

  const first = await mountThread(mount, page, 1440, 790);
  await page.evaluate(() => navigator.clipboard.writeText("PRE"));
  await copyCard();
  await expectCopiedOnTheButtonOnly(rowAt(page, "m-card"));
  expect(await clipboard()).toBe("要不要先在試用站備份？\n\nmigration 0142 會改索引。");
  await first.unmount();

  await page.evaluate(() => navigator.clipboard.writeText("PRE"));
  await mountThread(mount, page, 1440, 790, { cardReadFails: true });
  const failed = await copyCard();
  await expect(failed).toHaveText("複製失敗");
  expect(await clipboard()).toBe("PRE");
  await page.mouse.move(0, 0);
  await expect(failed, "the failure left with the pointer").toBeVisible();
  await expect(rowAt(page, "m-card").locator(".chat__msg-toolbar")).toHaveCSS("opacity", "1");
});

test("live thread 1440, mouse and touch screen: a finger's long press opens the menu, the mouse keeps the toolbar", async ({
  mount,
  page,
}) => {
  await mountThread(mount, page, 1440, 790);
  expect(await page.evaluate(() => matchMedia("(hover: hover)").matches)).toBe(true);
  const menu = page.getByTestId("chat-msg-menu");
  const bubble = rowAt(page, "m-long").locator(".chat__msg-bubble");

  await bubble.hover();
  await expect(rowAt(page, "m-long").locator(".chat__msg-toolbar")).toHaveCSS("opacity", "1");
  await page.mouse.move(0, 0);

  await touch(page, await centreOf(bubble), { holdMs: 80 });
  await page.waitForTimeout(600);
  await expect(menu, "a tap opened the menu").toHaveCount(0);

  // The mouse may select text in a bubble; a finger held on it may not.
  await expect(bubble).toHaveCSS("user-select", "auto");
  const at = await centreOf(bubble);
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Input.dispatchTouchEvent", {
    type: "touchStart",
    touchPoints: [{ x: at.x, y: at.y, id: 1 }],
  });
  await expect(bubble).toHaveCSS("user-select", "none");
  await page.waitForTimeout(700);
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  await cdp.detach();
  await expect(menu.getByRole("menuitem")).toHaveText(["回覆這則", "複製訊息", "放大閱讀", "選取文字"]);
  expect(
    await page.evaluate(() => window.getSelection()!.toString()),
    "the press also selected text",
  ).toBe("");
  await expect(bubble).toHaveCSS("user-select", "auto");
});

test.describe("touch screen", () => {
  // `hasTouch` alone is what flips `(hover: none)` in this Chromium.
  test.use({ hasTouch: true });

  for (const [width, height] of [
    [320, 700],
    [390, 844],
  ] as const) {
    test(`live thread ${width}: no toolbar; a long press opens the message's sheet at the bottom of the screen, a tap or a scroll does not`, async ({
      mount,
      page,
    }) => {
      await mountThread(mount, page, width, height);
      expect(await page.evaluate(() => matchMedia("(hover: none)").matches)).toBe(true);
      const menu = page.getByTestId("chat-msg-menu");
      const row = rowAt(page, "m-long");
      const bubble = row.locator(".chat__msg-bubble");

      // The browser's own long press is off on the bubble. Nothing shows, but
      // a screen reader still finds every action.
      await expect(bubble).toHaveCSS("user-select", "none");
      await expect(row.locator(".chat__msg-toolbar")).toHaveCSS("opacity", "0");
      await expect(row.getByRole("button")).toHaveText(["", "", ""]);
      expect(
        await row.locator(".chat__msg-actions button").evaluateAll((b) =>
          b.map((e) => e.getAttribute("aria-label")),
        ),
      ).toEqual(["回覆這則", "複製訊息", "放大閱讀"]);

      if (SHOTS && width === 390)
        await page.screenshot({ path: `${SHOTS}/${width}x${height}-rest.png` });

      await touch(page, await centreOf(bubble), { holdMs: 80 });
      await page.waitForTimeout(600);
      await expect(menu, "a tap opened the sheet").toHaveCount(0);

      await touch(page, await centreOf(bubble), { holdMs: 700, moveY: -60 });
      await expect(menu, "a scroll opened the sheet").toHaveCount(0);

      // Pressed near the top of the thread…
      await touch(page, await centreOf(bubble), { holdMs: 700 });
      await expect(menu).toBeVisible();
      await expect(menu.getByRole("menuitem")).toHaveText(["回覆這則", "複製訊息", "放大閱讀", "選取文字"]);
      const items = menu.getByRole("menuitem");
      await expect(items.first()).toBeFocused();
      await expect(items.first(), "a finger's press drew a focus ring").toHaveCSS("outline-style", "none");
      const tint = await items.nth(1).evaluate((e) => getComputedStyle(e).backgroundColor);
      await expect(items.first(), "a finger's press tinted the focused item").toHaveCSS("background-color", tint);
      await page.keyboard.press("ArrowDown");
      await expect(items.nth(1)).toBeFocused();
      const accent = await page.evaluate(() => {
        const probe = document.createElement("i");
        probe.style.color = "var(--color-accent)";
        document.body.append(probe);
        const c = getComputedStyle(probe).color;
        probe.remove();
        return c;
      });
      await expect(items.nth(1)).toHaveCSS("outline-style", "solid");
      await expect(items.nth(1)).toHaveCSS("outline-color", accent);
      await page.keyboard.press("ArrowUp");
      await page.keyboard.press("ArrowUp");
      await expect(items.last()).toBeFocused();
      await expect(row).toHaveClass(/chat__msg--pressed/);
      await expect(bubble).toHaveCSS("outline-style", "solid");
      await page.waitForTimeout(300);
      const top = (await menu.boundingBox())!;
      expect(Math.abs(top.x)).toBeLessThanOrEqual(0.5);
      expect(Math.abs(top.width - width)).toBeLessThanOrEqual(0.5);
      expect(Math.abs(top.y + top.height - height), "the sheet sits on the screen's bottom edge").toBeLessThanOrEqual(0.5);
      expect(
        await page.evaluate(() => window.getSelection()!.toString()),
        "the press also selected text",
      ).toBe("");
      if (SHOTS && width === 390)
        await page.screenshot({ path: `${SHOTS}/${width}x${height}-long-press-top.png` });

      // A tap on the dimmed backdrop closes it.
      await touch(page, { x: 4, y: 4 }, { holdMs: 50 });
      await expect(menu).toHaveCount(0);
      await expect(row).not.toHaveClass(/chat__msg--pressed/);

      const own = rowAt(page, "m-mine").locator(".chat__msg-text");
      await touch(page, await centreOf(own), { holdMs: 700 });
      await expect(menu).toHaveCount(1);
      await expect(menu.getByRole("menuitem")).toHaveText(["回覆這則", "複製訊息", "刪除", "選取文字"]);

      // …and near the bottom: the same sheet, in the same place.
      await touch(page, { x: 4, y: 4 }, { holdMs: 50 });
      const card = rowAt(page, "m-card").locator(".reply-card");
      await touch(page, await centreOf(card), { holdMs: 700 });
      await expect(menu).toHaveCount(1);
      await expect(menu.getByRole("menuitem")).toHaveText(["回覆這則", "複製訊息", "選取文字"]);
      await expect(card, "the press also opened the card").toHaveClass(/reply-card--collapsed/);
      await page.waitForTimeout(300);
      const bottom = (await menu.boundingBox())!;
      expect(bottom.y).toBeCloseTo(top.y + (top.height - bottom.height), 0);
      expect(Math.abs(bottom.y + bottom.height - height)).toBeLessThanOrEqual(0.5);
      if (SHOTS && width === 390)
        await page.screenshot({ path: `${SHOTS}/${width}x${height}-long-press-bottom.png` });

      await touch(page, { x: 4, y: 4 }, { holdMs: 50 });
      await expect(menu).toHaveCount(0);
    });

    test(`live thread ${width}: 複製 from the menu shows nothing once the menu closes when it worked, and 複製失敗 on the message when it did not`, async ({
      mount,
      page,
    }) => {
      const first = await mountThread(mount, page, width, height);
      const row = rowAt(page, "m-short");
      await touch(page, await centreOf(row.locator(".chat__msg-bubble")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-copy").tap();
      await expect(page.getByTestId("chat-msg-menu")).toHaveCount(0);
      await expectScreenReaderOnly(row.getByRole("status"), "已複製");
      await expect(row.locator(".chat__msg-toolbar")).toHaveCSS("opacity", "0");
      if (SHOTS && width === 390)
        await page.screenshot({ path: `${SHOTS}/${width}x${height}-after-copy.png` });
      await first.unmount();

      await mountThread(mount, page, width, height, { cardReadFails: true });
      const card = rowAt(page, "m-card");
      await touch(page, await centreOf(card.locator(".reply-card")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-copy").tap();
      await expect(page.getByTestId("chat-msg-menu")).toHaveCount(0);
      const status = card.getByRole("status");
      await expect(status).toHaveText("複製失敗");
      await expect(status).toBeVisible();
      await expect(card.locator(".chat__msg-toolbar")).toHaveCSS("opacity", "1");
      const s = (await status.boundingBox())!;
      const scroller = (await page.locator(".chat__messages").boundingBox())!;
      expect(s.x).toBeGreaterThanOrEqual(scroller.x - 0.5);
      expect(s.x + s.width).toBeLessThanOrEqual(scroller.x + scroller.width + 0.5);
      const stub = (await card.locator(".reply-card__collapsed-row").boundingBox())!;
      expect(s.y + s.height, "the result covers the card's row").toBeLessThanOrEqual(stub.y + 0.5);
      if (SHOTS && width === 390)
        await page.screenshot({ path: `${SHOTS}/${width}x${height}-copy-failed.png` });
    });

    for (const [id, selected, part] of [
      ["m-mine", "第二點的 migration 先等我確認備份再跑。", [0, 3, "第二點"]],
      ["m-mention", "請 @Mira 先看 這份計畫 再回我", null],
    ] as const) {
      test(`live thread ${width}: 選取文字 on ${id} enters its selection mode — the rest blurred and inert, a bar of 取消・複製・回覆 in the composer's place, and its links and quote do nothing there`, async ({
        mount,
        page,
        context,
      }) => {
        await context.grantPermissions(["clipboard-read", "clipboard-write"]);
        await mountThread(mount, page, width, height);
        await preferReduced(page, false);
        const row = rowAt(page, id);
        const bubble = row.locator(".chat__msg-bubble");
        const bar = page.getByTestId("chat-select-bar");
        await page.locator(".chat__input").fill("還沒送出的草稿");
        await touch(page, await centreOf(row.locator(".chat__msg-text")), { holdMs: 700 });
        const thread = page.locator(".chat__messages");
        const scrolledFrom = await thread.evaluate((e) => e.scrollTop);
        await page.getByTestId("chat-msg-menu-select").tap();

        await expect(bar).toBeVisible();
        await expect(bar).toBeFocused();
        const [scrollTop, scrollMax] = await thread.evaluate((e) => [e.scrollTop, e.scrollHeight - e.clientHeight]);
        expect(scrollTop, "the thread moved under a message in view").toBe(Math.min(scrolledFrom, scrollMax));
        await expect(bubble).toHaveCSS("user-select", "auto");
        await expect(bubble).toHaveCSS("filter", "none");
        await expect(row).toHaveJSProperty("inert", false);
        for (const other of ["m-long", "m-card"]) {
          await expect(rowAt(page, other)).toHaveCSS("filter", /blur/);
          await expect(rowAt(page, other)).toHaveCSS("pointer-events", "none");
          await expect(rowAt(page, other)).toHaveJSProperty("inert", true);
        }
        await expect(page.locator(".chat__header")).toHaveCSS("filter", /blur/);
        await expect(page.locator(".chat__header")).toHaveJSProperty("inert", true);
        await rowAt(page, "m-long").locator(".chat__msg-actions button").first().focus();
        await expect(bar).toBeFocused();
        await expect(page.locator(".chat__composer")).toBeHidden();
        const b = (await bar.boundingBox())!;
        const chat = (await page.locator(".chat").boundingBox())!;
        expect(Math.abs(b.y + b.height - (chat.y + chat.height)), "the bar is the bottom of the chat").toBeLessThanOrEqual(0.5);
        expect(await page.evaluate(() => window.getSelection()!.toString())).toBe(selected);
        await expect(page.getByTestId("chat-select-copy")).toBeEnabled();
        await expect(page.getByTestId("chat-select-reply")).toBeEnabled();
        const looks = await bar.locator("button").evaluateAll((bs) =>
          bs.map((b) => {
            const c = getComputedStyle(b);
            return [c.color, c.backgroundColor, c.borderColor, c.fontWeight].join(" ");
          }),
        );
        expect(new Set(looks).size, "取消、複製、回覆 look the same").toBe(1);
        if (SHOTS && width === 390 && id === "m-mine")
          await page.screenshot({ path: `${SHOTS}/${width}x${height}-select-mode-selected.png` });

        let popups = 0;
        page.context().on("page", () => popups++);
        const urlBefore = page.url();
        for (const action of await row.locator("a, button:not(.chat__msg-actions button)").all())
          await action.tap();
        await page.waitForTimeout(300);
        expect(popups, "a link in the message opened").toBe(0);
        expect(page.url(), "a link in the message navigated").toBe(urlBefore);
        await expect(page.locator("[role='dialog']")).toHaveCount(0);
        await expect(row.getByTestId("msg-quote-error")).toHaveCount(0);
        await expect(bar).toBeVisible();
        await expect(row).toHaveClass(/chat__msg--selecting/);
        await row.evaluate((r) => {
          const range = document.createRange();
          range.selectNodeContents(r.querySelector(".chat__msg-text")!);
          window.getSelection()!.removeAllRanges();
          window.getSelection()!.addRange(range);
        });
        await expect(page.getByTestId("chat-select-reply")).toBeEnabled();

        if (part) {
          // 複製 takes exactly what is selected, and the mode stays.
          await page.evaluate(([msg, from, to]) => {
            const t = document.querySelector(`[data-msg-id="${msg}"] .chat__msg-text p`)!.firstChild!;
            const r = document.createRange();
            r.setStart(t, from as number);
            r.setEnd(t, to as number);
            window.getSelection()!.removeAllRanges();
            window.getSelection()!.addRange(r);
          }, [id, part[0], part[1]] as const);
          await page.getByTestId("chat-select-copy").tap();
          await expectCheckOnly(page.getByTestId("chat-select-copy"), "已複製");
          await expect(bar.getByRole("status")).toHaveText("已複製");
          expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(part[2]);
          await expect(bar).toBeVisible();
          if (SHOTS && width === 390)
            await page.screenshot({ path: `${SHOTS}/${width}x${height}-select-mode-copied.png` });
          await page.getByTestId("chat-select-reply").tap();
          await expect(page.locator(".chat__reply-banner__body")).toHaveText(part[2]);
          // The 已複製 of the mode is gone with it, not left to run out its
          // 1.6s over the message (read once: a retrying check would wait it out).
          expect(await row.locator(".chat__msg-copy-status").textContent()).toBe("");
        } else {
          await page.getByTestId("chat-select-reply").tap();
          await expect(page.locator(".chat__reply-banner__body")).toHaveText(selected);
        }
        await expect(bar).toHaveCount(0);
        await expect(page.locator(".chat__input")).toBeVisible();
        await expect(page.locator(".chat__input")).toHaveValue("還沒送出的草稿");
        await expect(page.locator(".chat__input")).toBeFocused();
        await expectNoZoomOnFocus(page);
        await expect(bubble).toHaveCSS("user-select", "none");
        await expect(rowAt(page, "m-long")).toHaveCSS("filter", "none");
        await expect(rowAt(page, "m-long")).toHaveJSProperty("inert", false);
      });
    }

    test(`live thread ${width}: with reduced motion or transparency the selection mode fades the rest instead of blurring it, and it is just as out of reach`, async ({
      mount,
      page,
    }) => {
      await mountThread(mount, page, width, height);
      await preferReduced(page, true);
      await touch(page, await centreOf(rowAt(page, "m-mine").locator(".chat__msg-text")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      await expect(page.getByTestId("chat-select-bar")).toBeVisible();
      for (const other of ["m-long", "m-card"]) {
        await expect(rowAt(page, other)).toHaveCSS("filter", "none");
        await expect(rowAt(page, other)).toHaveCSS("opacity", "0.2");
        await expect(rowAt(page, other)).toHaveCSS("pointer-events", "none");
        await expect(rowAt(page, other)).toHaveJSProperty("inert", true);
      }
      await expect(rowAt(page, "m-mine")).toHaveCSS("opacity", "1");
      await page.locator(".chat__messages").evaluate((e) => e.scrollTo({ top: 0 }));
      const jump = page.getByTestId("chat-jump-latest");
      await expect(jump).toBeVisible();
      await expect(jump).toHaveCSS("opacity", "0.2");
      await expect(jump).toHaveJSProperty("inert", true);
    });

    test(`live thread ${width}: in selection mode an empty selection, or one running into the quoted message, turns 複製 and 回覆 off, a tap on the blurred thread only ends the mode, and 取消 leaves the message in view`, async ({
      mount,
      page,
    }) => {
      await mountThread(mount, page, width, height);
      const bar = page.getByTestId("chat-select-bar");
      await touch(page, await centreOf(rowAt(page, "m-mine").locator(".chat__msg-text")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      await page.evaluate(() => window.getSelection()!.removeAllRanges());
      await expect(page.getByTestId("chat-select-copy")).toBeDisabled();
      await expect(page.getByTestId("chat-select-reply")).toBeDisabled();
      await expect(page.getByTestId("chat-select-copy")).toHaveText("複製");
      if (SHOTS && width === 390)
        await page.screenshot({ path: `${SHOTS}/${width}x${height}-select-mode-rest.png` });
      await rowAt(page, "m-mine").evaluate((row) => {
        const r = document.createRange();
        r.selectNodeContents(row.querySelector(".chat__msg-text")!);
        window.getSelection()!.addRange(r);
      });
      await expect(page.getByTestId("chat-select-copy")).toBeEnabled();
      await rowAt(page, "m-mine").evaluate((row) => {
        const r = document.createRange();
        r.setStartBefore(row.querySelector(".chat__msg-quote")!);
        r.setEndAfter(row.querySelector(".chat__msg-text")!);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(r);
      });
      expect(await page.evaluate(() => window.getSelection()!.toString())).toContain("第二點的 migration");
      await expect(page.getByTestId("chat-select-copy")).toBeDisabled();
      await expect(page.getByTestId("chat-select-reply")).toBeDisabled();

      // The link in the blurred message under the finger is not followed and
      // its own sheet does not open.
      let popups = 0;
      page.context().on("page", () => popups++);
      const link = rowAt(page, "m-long").locator("a").first();
      const l = await centreOf(link);
      await touch(page, l, { holdMs: 60 });
      await expect(bar).toHaveCount(0);
      await expect(page.getByTestId("chat-msg-menu")).toHaveCount(0);
      await page.waitForTimeout(300);
      expect(popups, "the blurred link opened").toBe(0);
      expect(page.url()).not.toContain("example.com");

      const thread = page.locator(".chat__messages");
      await thread.evaluate((e) => {
        const row = e.querySelector('[data-msg-id="m-mine"]')!.getBoundingClientRect();
        e.scrollTop += row.bottom - e.getBoundingClientRect().bottom;
      });
      await touch(page, await centreOf(rowAt(page, "m-mine").locator(".chat__msg-text")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      await page.getByTestId("chat-select-cancel").tap();
      await expect(bar).toHaveCount(0);
      const shown = (await rowAt(page, "m-mine").boundingBox())!;
      const threadBox = (await thread.boundingBox())!;
      expect(shown.y + shown.height, "the message went under the composer").toBeLessThanOrEqual(threadBox.y + threadBox.height);
      await expect(rowAt(page, "m-mine").getByRole("button", { name: "回覆這則" })).toBeFocused();
    });

    test(`live thread ${width}: on an answered card the selection mode drops its first line, task box and 已回覆 row, still copies, and 回覆 is off; after 取消 the card is folded or open as it was before`, async ({
      mount,
      page,
      context,
    }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      const first = await mountThread(mount, page, width, height);
      const card = rowAt(page, "m-card-answered");
      const bar = page.getByTestId("chat-select-bar");
      const head = card.locator(".reply-card__head");
      await touch(page, await centreOf(card.locator(".reply-card")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      const body = card.locator(".reply-card__body");
      await expect(body).toBeVisible();
      await expect(card.locator(".reply-card__summary")).toBeVisible();
      await expect(head).toHaveCSS("display", "none");
      await expect(card.getByTestId("reply-task-ref")).toBeHidden();
      await expect(card.locator(".reply-card__meta")).toBeHidden();
      await expect(card.locator(".reply-card__divider")).toHaveCount(1);
      await expect(card.locator(".reply-card__divider")).toBeHidden();
      const thread = page.locator(".chat__messages");
      const clearOfBar = async (state: string) => {
        const rowBottom = (await card.boundingBox())!;
        const barTop = (await bar.boundingBox())!.y;
        expect(rowBottom.y + rowBottom.height, `${state}: the bar covers the newest message`).toBeLessThanOrEqual(barTop);
      };
      await expect(async () => clearOfBar("entered")).toPass();
      await thread.evaluate((e) => e.scrollTo({ top: e.scrollHeight }));
      await clearOfBar("scrolled to the end");
      await body.evaluate((e) => {
        const r = document.createRange();
        r.selectNodeContents(e);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(r);
      });
      await expect(page.getByTestId("chat-select-copy")).toBeEnabled();
      await expect(page.getByTestId("chat-select-reply")).toBeDisabled();
      await expect(bar.getByRole("status")).toHaveText("");
      await page.getByTestId("chat-select-copy").tap();
      await expect(bar.getByRole("status")).toHaveText("已複製");
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("上線前要先跑完回歸測試。");
      await page.evaluate(() => window.getSelection()!.removeAllRanges());
      await expect(page.getByTestId("chat-select-copy")).toBeDisabled();
      await page.getByTestId("chat-select-cancel").tap();
      await expect(bar).toHaveCount(0);
      await expect(card.locator(".reply-card")).toHaveClass(/reply-card--collapsed/);
      await card.getByTestId("chat-reply-card-expand").tap();
      await expect(head).toBeVisible();
      await expect(card.getByTestId("reply-task-ref")).toBeVisible();
      await first.unmount();

      await mountThread(mount, page, width, height, { cardReadFails: true });
      await card.getByTestId("chat-reply-card-expand").tap();
      await touch(page, await centreOf(card.locator(".reply-card")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      const error = card.locator(".reply-card__error");
      await expect(error).toHaveCount(1);
      await expect(error).toBeHidden();
      await expect(card.locator(".reply-card__summary")).toBeVisible();
      await page.getByTestId("chat-select-cancel").tap();
      await expect(bar).toHaveCount(0);
      await expect(error).toBeVisible();
    });

    for (const season of ["thanksgiving", "christmas"] as const) {
      for (const language of ["zh", "en"] as const) {
        test(`live thread ${width} ${season} ${language}: the selection bar holds 取消・複製・回覆 whole and unthemed, and 複製 shows a check in place`, async ({
          mount,
          page,
          context,
        }) => {
          await context.grantPermissions(["clipboard-read", "clipboard-write"]);
          await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
          await page.setViewportSize({ width, height });
          await mount(<ChatMessageActionsStory withRoster={false} season={season} />);
          await expect(page.locator('[data-msg-id="m-card"]')).toBeVisible();
          await touch(page, await centreOf(rowAt(page, "m-mine").locator(".chat__msg-text")), { holdMs: 700 });
          await page.getByTestId("chat-msg-menu-select").tap();
          const bar = page.getByTestId("chat-select-bar");
          let barHeight: number | undefined;
          const fits = async (state: string) => {
            const geom = await bar.evaluate((b) => {
              const box = b.getBoundingClientRect();
              return {
                bar: { x: box.x, right: box.right, top: box.top, height: box.height },
                widths: Array.from(b.querySelectorAll("button")).map((e) => e.getBoundingClientRect().width),
                buttonsTop: Math.min(...Array.from(b.querySelectorAll("button")).map((e) => e.getBoundingClientRect().top)),
                parts: Array.from(b.querySelectorAll<HTMLElement>("button")).map((e) => {
                  const r = e.getBoundingClientRect();
                  return {
                    text: e.textContent ?? "",
                    x: r.x,
                    right: r.right,
                    cut: e.scrollWidth > e.clientWidth + 0.5,
                  };
                }),
              };
            });
            expect(geom.bar.x, `${state}: bar cut on the left`).toBeGreaterThanOrEqual(-0.5);
            expect(geom.bar.right, `${state}: bar cut on the right`).toBeLessThanOrEqual(width + 0.5);
            for (const p of geom.parts) {
              expect(p.x, `${state} 「${p.text}」 leaves the bar`).toBeGreaterThanOrEqual(geom.bar.x - 0.5);
              expect(p.right, `${state} 「${p.text}」 leaves the bar`).toBeLessThanOrEqual(geom.bar.right + 0.5);
              expect(p.cut, `${state} 「${p.text}」 is cut inside itself`).toBe(false);
            }
            expect(geom.widths, `${state}: the buttons share the width`).toHaveLength(3);
            for (const w of geom.widths)
              expect(Math.abs(w - geom.widths[0]), `${state}: the buttons share the width ${geom.widths}`).toBeLessThanOrEqual(1);
            expect(geom.buttonsTop - geom.bar.top, `${state}: a row above the buttons`).toBeLessThanOrEqual(8.5);
            barHeight ??= geom.bar.height;
            expect(geom.bar.height, `${state}: the bar changed height`).toBeCloseTo(barHeight, 1);
            return geom.parts.map((p) => p.text);
          };
          await expect(page.getByTestId("chat-select-copy")).toBeEnabled();
          await expect(bar.getByRole("status")).toHaveText("");
          const labels = await fits("selected");
          if (SHOTS && width === 390 && language === "zh")
            await page.screenshot({ path: `${SHOTS}/${width}x${height}-select-bar-${season}.png` });
          expect(labels).toEqual(language === "zh" ? ["取消", "複製", "回覆"] : ["Cancel", "Copy", "Reply"]);
          await page.evaluate(() => window.getSelection()!.removeAllRanges());
          await expect(page.getByTestId("chat-select-copy")).toBeDisabled();
          await fits("nothing selected");
          await page.evaluate(() => {
            const t = document.querySelector('[data-msg-id="m-mine"] .chat__msg-text p')!.firstChild!;
            const r = document.createRange();
            r.setStart(t, 0);
            r.setEnd(t, 3);
            window.getSelection()!.addRange(r);
          });
          await page.getByTestId("chat-select-copy").tap();
          await expectCheckOnly(page.getByTestId("chat-select-copy"), language === "zh" ? "已複製" : "Copied");
          await fits("copied");
        });
      }
    }

    test(`live thread ${width}: 選取文字 on a folded waiting card, then 回覆, shows the card's title and question only, a tap on it folds nothing, copies and quotes what is selected a line each — also when dragged across where its task row and attachments were — into THAT card's answer box, the card left open with the rest back`, async ({
      mount,
      page,
      context,
    }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await mountThread(mount, page, width, height);
      const card = rowAt(page, "m-card");
      await expect(card.locator(".reply-card")).toHaveClass(/reply-card--collapsed/);
      await touch(page, await centreOf(card.locator(".reply-card")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      const body = card.locator(".reply-card__body");
      await expect(body).toBeVisible();
      await expect(card.locator(".reply-card__summary")).toBeVisible();
      await expect(card.locator(".reply-card__options")).toBeHidden();
      await expect(card.locator(".reply-composer")).toBeHidden();
      await expect(card.getByTestId("reply-task-ref")).toBeHidden();
      await expect(card.locator(".reply-card__question-atts")).toBeHidden();
      const expire = card.getByTestId("chat-reply-card-expire");
      await expect(expire).toHaveCount(1);
      await expect(expire).toBeHidden();
      await expect(card.locator(".reply-card__head")).toHaveCSS("display", "none");
      const divider = card.locator(".reply-card__divider");
      await expect(divider).toHaveCount(1);
      await expect(divider).toBeHidden();
      for (const spot of [body, card.locator(".reply-card__summary")]) await spot.tap();
      const box = (await card.locator(".reply-card").boundingBox())!;
      await page.touchscreen.tap(box.x + box.width - 4, box.y + box.height - 4);
      await expect(card.locator(".reply-card")).not.toHaveClass(/reply-card--collapsed/);
      await expect(page.getByTestId("chat-select-bar")).toBeVisible();
      const selected = "要不要先在試用站備份？\nmigration 0142 會改索引。";
      await card.evaluate((row) => {
        const text = row.querySelector(".reply-card__body p")!.firstChild!;
        window.getSelection()!.setBaseAndExtent(text, 4, row.querySelector(".reply-card")!, 0);
      });
      await expect(page.getByTestId("chat-select-copy")).toBeEnabled();
      await expect(page.getByTestId("chat-select-reply")).toBeEnabled();
      await page.getByTestId("chat-select-copy").tap();
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
        "要不要先在試用站備份？\nmigr",
      );
      await card.evaluate((row) => {
        const r = document.createRange();
        r.selectNodeContents(row.querySelector(".reply-card")!);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(r);
      });
      await expect(page.getByTestId("chat-select-copy")).toBeEnabled();
      await expect(page.getByTestId("chat-select-reply")).toBeEnabled();
      await page.getByTestId("chat-select-copy").tap();
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(selected);
      await card.evaluate((row) => {
        const r = document.createRange();
        r.setStartBefore(row.querySelector(".reply-card__summary")!);
        r.setEndAfter(row.querySelector(".reply-card__body")!);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(r);
      });
      await page.getByTestId("chat-select-copy").tap();
      await expectCheckOnly(page.getByTestId("chat-select-copy"), "已複製");
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(selected);
      await expect(page.getByTestId("chat-select-reply")).toBeEnabled();
      await page.getByTestId("chat-select-reply").tap();
      const quote = card.getByTestId("reply-composer-quote").locator(".reply-card__quote-text");
      await expect(quote).toBeVisible();
      expect(await quote.evaluate((e) => e.textContent)).toBe(selected);
      await expect(page.locator(".chat__reply-banner")).toHaveCount(0);
      await expect(page.getByTestId("chat-select-bar")).toHaveCount(0);
      await expect(card.locator(".reply-card__options")).toBeVisible();
      await expect(card.getByTestId("reply-task-ref")).toBeVisible();
      await expect(card.locator(".reply-card__question-atts")).toBeVisible();
      await expect(expire).toBeVisible();
      await expect(divider).toBeVisible();
      await expect(card.locator(".reply-card")).not.toHaveClass(/reply-card--collapsed/);
      await expect(card.locator(".chat__input")).toBeFocused();
      await expectNoZoomOnFocus(page);
    });

    test(`live thread ${width}: a waiting card on no task keeps its title in the selection mode, its first line and 標為過期 not displayed, and they are back after`, async ({
      mount,
      page,
    }) => {
      await mountThread(mount, page, width, height, { cardsWithoutTask: true });
      const card = rowAt(page, "m-card");
      await touch(page, await centreOf(card.locator(".reply-card")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      const expire = card.getByTestId("chat-reply-card-expire");
      await expect(expire).toHaveCount(1);
      await expect(expire).toBeHidden();
      await expect(card.locator(".reply-card__summary")).toBeVisible();
      await expect(card.locator(".reply-card__head")).toBeHidden();
      await page.getByTestId("chat-select-cancel").tap();
      await expect(card.locator(".reply-card")).toHaveClass(/reply-card--collapsed/);
      await card.getByTestId("chat-reply-card-expand").tap();
      await expect(expire).toBeVisible();
    });
  }

  for (const [width, height] of [
    [320, 700],
    [390, 844],
    [390, 640],
  ] as const) {
    test(`live thread ${width}x${height}: in selection mode on a message taller than the room above the bar, the thread ends at the bar's top, the message's last line scrolls fully above it, a selection ending near the bar is scrolled clear of it, and 複製 takes that selection`, async ({
      mount,
      page,
      context,
    }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await mountThread(mount, page, width, height, { tallMessage: true });
      const row = rowAt(page, "m-long");
      const opening = row.locator(".chat__msg-text p").first();
      await opening.scrollIntoViewIfNeeded();
      await touch(page, await centreOf(opening), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      const bar = page.getByTestId("chat-select-bar");
      await expect(bar).toBeFocused();
      const barTop = (await bar.boundingBox())!.y;
      const thread = page.locator(".chat__messages");
      const threadBox = (await thread.boundingBox())!;
      expect(threadBox.y + threadBox.height, "the thread runs on under the bar").toBeLessThanOrEqual(barTop + 0.5);
      await page.waitForTimeout(300);
      const pressed = (await opening.boundingBox())!;
      expect(pressed.y, "the pressed paragraph went above the thread").toBeGreaterThanOrEqual(threadBox.y - 0.5);
      expect(pressed.y + pressed.height, "the pressed paragraph went under the bar").toBeLessThanOrEqual(barTop + 0.5);
      expect((await row.boundingBox())!.height, "the message fits above the bar").toBeGreaterThan(barTop - threadBox.y);

      const closing = row.locator(".chat__msg-text p").last();
      await thread.evaluate((t, p) => {
        t.scrollTop += p!.getBoundingClientRect().bottom - t.getBoundingClientRect().bottom;
      }, await closing.elementHandle());
      const line = (await closing.boundingBox())!;
      expect(line.y + line.height, "the last line, scrolled to the thread's end, is under the bar").toBeLessThanOrEqual(barTop + 0.5);
      expect(line.y, "the last line is above the thread").toBeGreaterThanOrEqual(threadBox.y - 0.5);
      expect(
        await page.evaluate(
          ([x, y]) => !!document.elementFromPoint(x, y)?.closest('[data-msg-id="m-long"]'),
          [line.x + line.width / 2, line.y + line.height / 2],
        ),
        "a tap on the last line lands on the message",
      ).toBe(true);

      const endBottom = () =>
        page.evaluate(() => {
          const rects = window.getSelection()!.getRangeAt(0).getClientRects();
          return rects[rects.length - 1].bottom;
        });
      await thread.evaluate((t, p) => {
        t.scrollTop += p!.getBoundingClientRect().bottom - (t.getBoundingClientRect().bottom - 10);
      }, await closing.elementHandle());
      await closing.evaluate((p) => {
        const text = p.firstChild!;
        const r = document.createRange();
        r.setStart(text, 0);
        r.setEnd(text, text.textContent!.length);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(r);
      });
      expect(await endBottom(), "the selection does not start near the bar").toBeGreaterThan(barTop - 40);
      await expect.poll(endBottom, { message: "the selection's end stays by the bar" }).toBeLessThanOrEqual(barTop - 40);
      await page.waitForTimeout(300);
      expect(await endBottom(), "the thread moved again").toBeLessThanOrEqual(barTop - 40);

      await page.getByTestId("chat-select-copy").tap();
      await expectCheckOnly(page.getByTestId("chat-select-copy"), "已複製");
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("有問題再跟我說。");
      await expect(bar).toBeVisible();
    });

    test(`live thread ${width}x${height}: in selection mode a tall message pressed partway down stays where it was, and once the reader is inside it a resize of the screen or the message does not scroll back to its top`, async ({
      mount,
      page,
    }) => {
      await mountThread(mount, page, width, height, { tallMessage: true });
      const thread = page.locator(".chat__messages");
      const row = rowAt(page, "m-long");
      const item = row.locator(".chat__msg-text li").nth(10);
      await item.scrollIntoViewIfNeeded();
      await thread.evaluate((t, li) => {
        const box = t.getBoundingClientRect();
        t.scrollTop += li!.getBoundingClientRect().top - (box.top + box.height / 2);
      }, await item.elementHandle());
      const threadTop = (await thread.boundingBox())!.y;
      expect((await row.boundingBox())!.y, "the message's top is still on screen").toBeLessThan(threadTop);
      const before = (await item.boundingBox())!.y;
      await touch(page, await centreOf(item), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      const bar = page.getByTestId("chat-select-bar");
      await expect(bar).toBeFocused();
      await page.waitForTimeout(300);
      const after = (await item.boundingBox())!;
      expect(Math.abs(after.y - before), "the pressed line moved").toBeLessThanOrEqual(1);
      expect(after.y + after.height, "the pressed line went under the bar").toBeLessThanOrEqual((await bar.boundingBox())!.y);

      const frames = () =>
        page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      const scrolled = await thread.evaluate((t) => {
        t.scrollTop += 150;
        return t.scrollTop;
      });
      expect((await row.boundingBox())!.y, "the message's top is still on screen").toBeLessThan(threadTop);
      await page.setViewportSize({ width, height: height - 40 });
      await frames();
      expect(Math.abs((await thread.evaluate((t) => t.scrollTop)) - scrolled), "a shorter screen scrolled the thread").toBeLessThanOrEqual(1);
      await row.locator(".chat__msg-text").evaluate((e) => {
        (e as HTMLElement).style.paddingBottom = "30px";
      });
      await frames();
      expect(Math.abs((await thread.evaluate((t) => t.scrollTop)) - scrolled), "a taller message scrolled the thread").toBeLessThanOrEqual(1);
    });
  }

  test("390: a finger's selection outside the thread's selection mode gets 回覆這段 docked at the bottom, clear of the system's selection bar", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await mount(<ChatSelectionReplyStory selecting />);
    const text = page.locator('[data-msg-id="c-2"] .chat__msg-text');
    await page.touchscreen.tap(...(Object.values(await centreOf(text)) as [number, number]));
    await selectInBodies(page, { row: "c-2", start: 0 }, { row: "c-2", end: 2 });
    const reply = page.getByTestId("chat-selection-reply");
    await expect(reply).toBeVisible();
    const r = (await reply.boundingBox())!;
    expect(Math.abs(r.y + r.height - (800 - 12)), "the button is not docked").toBeLessThanOrEqual(0.5);
    expect(r.y - (await selectionBottom(page)), "the button sits by the selection").toBeGreaterThanOrEqual(60);
  });
});

// 「回覆這段」: the floating button is portaled to <body> with fixed
// coordinates, so the guards are that it lands under the selection (above it
// would cover the line before and iOS's own Copy menu), inside the viewport
// (selection at the right end of an own bubble, where it would otherwise poke
// out), never widens the page, and fills the banner with exactly the selected
// text on one clipped line.
function selectionBottom(page: import("@playwright/test").Page) {
  return page.evaluate(
    () => window.getSelection()!.getRangeAt(0).getBoundingClientRect().bottom,
  );
}

function selectInBodies(
  page: import("@playwright/test").Page,
  from: { row: string; start: number },
  to: { row: string; end: number | "end" },
) {
  return page.evaluate(
    ([f, t]) => {
      const textOf = (row: string) =>
        document.querySelector(`[data-msg-id="${row}"] .chat__msg-text p`)!
          .firstChild as Text;
      const a = textOf(f.row);
      const b = textOf(t.row);
      const range = document.createRange();
      range.setStart(a, f.start);
      range.setEnd(b, t.end === "end" ? b.length : t.end);
      const sel = window.getSelection()!;
      sel.removeAllRanges();
      sel.addRange(range);
      return sel.toString();
    },
    [from, to] as const,
  );
}

for (const [language, width] of [
  ["zh", 390],
  ["en", 390],
  ["zh", 1280],
] as const) {
  test(`${language} width ${width}: 回覆這段 over a selection at the bubble's far edge stays inside the viewport and fills a one-line banner`, async ({
    mount,
    page,
  }) => {
    await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
    await page.setViewportSize({ width, height: 800 });
    await mount(<ChatSelectionReplyStory />);
    const length = await page.evaluate(() => {
      const p = document.querySelector('[data-msg-id="c-3"] .chat__msg-text p')!;
      return (p.firstChild as Text).length;
    });
    const picked = "個字";

    const button = page.getByTestId("chat-selection-reply");
    await selectInBodies(page, { row: "c-2", start: 0 }, { row: "c-2", end: 1 });
    await expect(button).toBeVisible();
    const leftBox = (await button.boundingBox())!;
    expect(leftBox.x, "a selection at the far left keeps the button on screen").toBeGreaterThanOrEqual(0);
    expect(leftBox.y, "the button sits under the selection").toBeGreaterThanOrEqual(
      await selectionBottom(page),
    );

    await selectInBodies(page, { row: "c-3", start: length - 2 }, { row: "c-3", end: "end" });
    await expect(button).toBeVisible();
    const box = (await button.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(width);
    expect(box.y, "the button sits under the selection").toBeGreaterThanOrEqual(
      await selectionBottom(page),
    );
    const lineBoxes = await button.evaluate((e) => e.getClientRects().length);
    expect(lineBoxes, "the label stays on one line").toBe(1);
    const pageScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(pageScroll, "no horizontal page scroll").toBeLessThanOrEqual(0);

    await button.click();
    await expect(button).toHaveCount(0);
    const banner = page.getByTestId("chat-reply-banner");
    await expect(banner).toHaveAttribute("data-reply-to", "c-3");
    const body = banner.locator(".chat__reply-banner__body");
    await expect(body).toHaveText(picked);
    expect(await body.evaluate((e) => e.getClientRects().length)).toBe(1);
    const bannerBox = (await banner.boundingBox())!;
    expect(bannerBox.x + bannerBox.width).toBeLessThanOrEqual(width + 0.5);
  });
}

for (const width of [390, 1280]) {
  test(`width ${width}: a selection dragged across two messages offers no 回覆這段, one dragged inside one does`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    await mount(<ChatSelectionReplyStory />);
    await selectInBodies(page, { row: "c-1", start: 2 }, { row: "c-2", end: 2 });
    await page.waitForTimeout(50);
    await expect(page.getByTestId("chat-selection-reply")).toHaveCount(0);

    await page.evaluate(() => window.getSelection()!.removeAllRanges());
    const text = page.locator('[data-msg-id="c-2"] .chat__msg-text p');
    const t = (await text.boundingBox())!;
    await page.mouse.move(t.x + 1, t.y + t.height / 2);
    await page.mouse.down();
    await page.mouse.move(t.x + t.width - 1, t.y + t.height / 2, { steps: 5 });
    await page.mouse.up();
    await expect(page.getByTestId("chat-selection-reply")).toBeVisible();
  });
}

// A triple-click ends the range at offset 0 of whatever follows the block —
// for a message's last block that is the next row's time or name, outside the
// body.
for (const width of [390, 1280]) {
  test.describe(`width ${width}`, () => {
    test.use({ hasTouch: width === 390, isMobile: width === 390 });

    test("a triple-clicked paragraph or list item offers 回覆這段 and quotes exactly that block", async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await mount(<ChatSelectionReplyStory selecting={width === 390} />);
      const button = page.getByTestId("chat-selection-reply");
      const banner = page.getByTestId("chat-reply-banner");
      for (const [row, selector, quoted] of [
        ["c-4", "p:nth-child(2)", "中間這一段是重點"],
        ["c-4", "p:nth-child(3)", "最後一段收尾"],
        ["c-2", "p", "第二則訊息"],
        ["c-3", "p", "好的我確認這兩個字"],
        ["c-5", "li:nth-child(1)", "第一項待辦"],
        ["c-5", "li:nth-child(2)", "第二項待辦"],
      ] as const) {
        const block = page.locator(`[data-msg-id="${row}"] .chat__msg-text ${selector}`);
        await block.scrollIntoViewIfNeeded();
        const box = (await block.boundingBox())!;
        await page.mouse.click(box.x + 4, box.y + box.height / 2, { clickCount: 3 });
        await expect(button, `${row} ${selector}`).toBeVisible();
        await button.click();
        await expect(banner).toHaveAttribute("data-reply-to", row);
        expect(await banner.locator(".chat__reply-banner__body").textContent()).toBe(quoted);
      }

      await page.evaluate(() => {
        const meta = document.querySelector('[data-msg-id="c-5"] .chat__msg-meta')!;
        const item = document.querySelector('[data-msg-id="c-5"] li')!;
        const range = document.createRange();
        range.setStart(meta, meta.childNodes.length);
        range.setEnd(item.firstChild!, 3);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(range);
      });
      await expect(button, "a range starting just above the body").toBeVisible();
      await button.click();
      await expect(banner).toHaveAttribute("data-reply-to", "c-5");
      expect(await banner.locator(".chat__reply-banner__body").textContent()).toBe("第一項");

      await page.evaluate(() => {
        const last = document.querySelector('[data-msg-id="c-4"] .chat__msg-text p:nth-child(3)')!;
        const name = document.querySelector('[data-msg-id="c-5"] .chat__msg-name')!;
        const range = document.createRange();
        range.setStart(last.firstChild!, 0);
        range.setEnd(name.firstChild!, 2);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(range);
      });
      await page.waitForTimeout(50);
      await expect(button, "a range reaching into the next sender's name").toHaveCount(0);
    });
  });
}

// 回覆這段 on a waiting chat card quotes into the card's own answer box (owner
// rc-cf02eac19977). The quote is a piece of agent-written markdown and can be
// one unbroken token, so it must wrap inside the card above the box and under
// the sent answer, never push either wider.
for (const width of [390, 1280]) {
  test(`width ${width}: an unbroken quote above a chat card's answer box, and under its sent answer, wraps inside the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<ChatCardTimestampStory />);
    if (width === 1280) {
      await page.evaluate(() => {
        document.documentElement.dataset.layout = "wide";
      });
    }
    await cmp.getByTestId("chat-reply-card-expand").click();
    const card = cmp.getByTestId("chat-reply-card");
    await expect(card.getByTestId("reply-option").first()).toBeVisible();

    await page.evaluate(() => {
      const code = document.querySelector(".reply-card__summary pre code")!;
      const text = code.firstChild as Text;
      const range = document.createRange();
      range.setStart(text, 0);
      range.setEnd(text, text.length);
      window.getSelection()!.removeAllRanges();
      window.getSelection()!.addRange(range);
    });
    const button = page.getByTestId("chat-selection-reply");
    await expect(button).toBeVisible();
    await button.click();

    const inThread = async (label: string, quote: import("@playwright/test").Locator) => {
      await expect(quote).toBeVisible();
      const cardBox = (await card.boundingBox())!;
      const quoteBox = (await quote.boundingBox())!;
      expect(quoteBox.x, `${label}: starts inside the card`).toBeGreaterThanOrEqual(cardBox.x - 1);
      expect(quoteBox.x + quoteBox.width, `${label}: ends inside the card`).toBeLessThanOrEqual(
        cardBox.x + cardBox.width + 1,
      );
      const textOver = await quote
        .locator(".reply-card__quote-text")
        .evaluate((el) => el.scrollWidth - el.clientWidth);
      expect(textOver, `${label}: the passage wraps instead of running sideways`).toBeLessThanOrEqual(1);
      const messagesOver = await cmp
        .getByTestId("chat-messages")
        .evaluate((el) => el.scrollWidth - el.clientWidth);
      expect(messagesOver, `${label}: the thread does not scroll sideways`).toBeLessThanOrEqual(1);
      const pageOver = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(pageOver, `${label}: no horizontal page scroll`).toBeLessThanOrEqual(0);
    };

    const armed = card.getByTestId("reply-composer-quote");
    await inThread("above the answer box", armed);
    const composerBox = (await card.locator(".reply-composer").boundingBox())!;
    const armedBox = (await armed.boundingBox())!;
    expect(
      armedBox.x + armedBox.width,
      "the quote is no wider than the answer box it sits on",
    ).toBeLessThanOrEqual(composerBox.x + composerBox.width + 1);
    const input = card.locator(".reply-composer .chat__input");
    await expect(input).toBeFocused();

    await input.fill("先確認欄位");
    await card.locator(".reply-composer .chat__send").click();
    await inThread("under the sent answer", card.getByTestId("final-answer-quote"));
  });
}
