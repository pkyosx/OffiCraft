// GUARD (T-4e95) — 「回覆這則」的版面契約。
//
// jsdom 已經證過行為（ChatArea.reply-to.test.tsx：入口在每一列、橫幅顯示對象、
// x 不清字、送出帶對象、引用列點得回去）。這裡補的是 jsdom **量不到**的三件，
// 每一件都對應一個真的會壞掉的方式：
//
//  ① 回覆入口是 hover 才顯形的，但**永遠占著版面**。用 display:none 做隱藏會讓
//     氣泡在滑過去的當下橫向跳一格 —— 那是使用者感覺得到、jsdom 完全看不到的。
//  ② 引用列與「正在回覆」橫幅的**每一行**都必須裁掉溢出。它們攜帶的是別人訊息的
//     原文，長度不受這一則控制；一旦允許折行，一句長訊息就會把版面撐開，或把輸入
//     框往下推。🔴 2026-08-22 起兩者都是**固定兩行**（第一行「寄件者 → 收件者」，
//     第二行被引的內容 —— owner 裁定），所以這裡量的是「恰好兩個行框、每半各一
//     個」，不是「總高小於一行」。行數固定，所以瞄準不同訊息時 composer 仍然不會
//     跳動 —— 那才是這一條原本要守的事。
//  ③ 引用列必須留在氣泡那一欄裡，不得溢出訊息串的可視寬度（窄視窗尤其）。
//
// 四個寬度都量：手機寬、小平板寬與桌面寬在這個元件上是不同的失敗面。
// 🔴 375 與 720 是 2026-08-22 加的。那一天引用列從「寄件者」變成「寄件者 →
// 收件者」——同一條列多了一個名字加一個箭頭，而它本來就是這個元件最會溢出的
// 那一列。（同一天稍後 owner 把那一列拆成兩行，因為多出來的那半是直接從句子身上
// 扣寬度；拆完之後最先撞牆的不再是句子，而是第一行的控制項。）只量 390／1280 量到的是兩個端點：窄到跳轉標籤整個消失，與寬到什麼
// 都塞得下。375 是實機最窄的那一格；720 落在 520 這個 container 斷點的另一
// 側（pane 672px：標籤畫得出來，卻沒有 1280 的餘裕），正是變長之後最先撞牆
// 的那一段。
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatReplyToStory } from "./stories/ChatReplyToStory";

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

for (const width of [375, 390, 720, 1280]) {
  test(`width ${width}: the reply entry is hover-revealed but never re-flows the row`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const entry = cmp.getByTestId("reply-entry-incoming");
    // The bubble's TEXT is what a reflow would move now that the controls live
    // inside the bubble: measuring the bubble itself would miss a slot that
    // grew and pushed the words sideways.
    const bubble = cmp.getByTestId("row-incoming").locator(".chat__msg-text");

    // Hidden by OPACITY, not by display/visibility: it must still occupy space.
    // Both bubble kinds, because the contrast test downstream measures the ink
    // on both and describes it as "the colour they are revealed in" — a claim
    // that needs the resting state pinned on both, not just this one.
    await expect(entry).toHaveCSS("opacity", "0");
    await expect(cmp.getByTestId("reply-entry-mine")).toHaveCSS("opacity", "0");
    const box = await entry.boundingBox();
    expect(box, "an opacity-hidden control still has a box").not.toBeNull();
    expect(box!.width).toBeGreaterThan(0);
    expect(box!.height).toBeGreaterThan(0);

    // The decisive measurement: hovering reveals it WITHOUT moving the bubble.
    const before = await bubble.boundingBox();
    await cmp.getByTestId("row-incoming").hover();
    await expect(entry).toHaveCSS("opacity", "1");
    const after = await bubble.boundingBox();
    expect(Math.abs(after!.x - before!.x)).toBeLessThanOrEqual(0.5);
    expect(Math.abs(after!.width - before!.width)).toBeLessThanOrEqual(0.5);
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
    // Measured in this harness at all four widths: 36.7px. 46 is a ceiling with
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

    // The jump stays inside the bubble. Its LABEL is not necessarily HERE at all,
    // and when it is absent it is GONE, not shortened: the label's only rule in
    // office.css is `display: none` inside `@container chat-pane
    // (max-width: 520px)`, and this harness hands the pane 342px at viewport 390
    // and 1232px at 1280 — so this loop sees the arrow alone at 375/390 and the
    // whole label at 720/1280. Nothing in the stylesheet can TRIM it: the button is
    // `flex: none` with `white-space: nowrap` and the label carries no
    // `text-overflow` of its own. What may never go is the arrow: what survives
    // must still read as "go somewhere".
    //
    // This replaces an earlier `jump.width > 40`, which encoded "the jump keeps
    // its whole width, a cut 跳到原訊息 helps nobody". That rule was measured
    // against the 69px Chinese string and could not survive the 154px English
    // one: a control that never gives way does not stay inside the bubble, it
    // runs under the corner buttons. Dropping the whole label is the lesser
    // loss, and which side of that the row is on is pinned separately below, on
    // the pane's width ("the jump label collapses on the PANE's width, at 520").
    const jump = (await cmp.getByTestId("quote-jump").boundingBox())!;
    expect(jump.x + jump.width).toBeLessThanOrEqual(
      bubbleBox.x + bubbleBox.width + 0.5,
    );
    const chevron = (await cmp
      .getByTestId("row-mine")
      .locator(".chat__msg-quote__jump-chevron")
      .boundingBox())!;
    expect(chevron.width).toBeGreaterThanOrEqual(11.5);
    expect(chevron.x + chevron.width).toBeLessThanOrEqual(
      jump.x + jump.width + 0.5,
    );

    // 🔴 AND IT MUST NOT REACH THE CORNER CONTROLS. Measured, not inferred: a
    // row whose min-content width exceeded the bubble pushed the jump ON TOP of
    // the reply button — the affordance covering the affordance. Nothing about
    // the DOM says that; only geometry does.
    const acts = (await cmp
      .getByTestId("row-mine")
      .locator(".chat__msg-actions")
      .boundingBox())!;
    expect(jump.x + jump.width).toBeLessThanOrEqual(acts.x + 0.5);
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

    // …and the jump's label is whole here. Nothing in the stylesheet can trim
    // it — the button is `flex: none` with `white-space: nowrap` and the label
    // carries no `text-overflow` — so the label is present whole or absent
    // outright, never cut. This is a floor, not a discriminator.
    const labelCut = await cmp
      .getByTestId("row-mine-short")
      .locator(".chat__msg-quote__jump-label")
      .evaluate((e) => e.scrollWidth > e.clientWidth + 0.5);
    expect(labelCut).toBe(false);
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

  test(`width ${width}: the corner buttons take their ink from the bubble they sit on`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    // owner 2026-08-20 (rc-8056a06aa2b8): the entry used to be a chip in
    // --color-card, which is the same surface as an incoming bubble and a
    // foreign grey box on your own — that one is painted accent green. He asked
    // why the two buttons looked like different colours; they were not
    // different from each other, they were different from what they sat on.
    const incoming = cmp.getByTestId("reply-entry-incoming");
    const mine = cmp.getByTestId("reply-entry-mine");

    // 🔴 THE RULE, stated as something that can fail: the same button on two
    // differently-coloured bubbles must not come out the same colour. A
    // hardcoded muted ink passes any "is it readable" check and fails this one.
    const inkIncoming = await incoming.evaluate((e) => getComputedStyle(e).color);
    const inkMine = await mine.evaluate((e) => getComputedStyle(e).color);
    expect(inkMine).not.toBe(inkIncoming);

    // And no box of its own: the chip is what made it read as a foreign object.
    for (const btn of [incoming, mine]) {
      const box = await btn.evaluate((e) => {
        const c = getComputedStyle(e);
        return { bg: c.backgroundColor, bw: c.borderTopWidth };
      });
      expect(box.bg).toBe("rgba(0, 0, 0, 0)");
      expect(box.bw).toBe("0px");
    }

    // 🔴 AND THE INK MUST BE READABLE. "Follows the bubble" is satisfied just as
    // well by ink at 5% — the two would still differ from each other, and this
    // test would still pass, while the control had effectively vanished. So the
    // rule is stated as contrast: WCAG's 3:1 floor for a non-text control,
    // measured against the bubble each button actually sits on.
    //
    // Precisely: this measures the INK, not the resting appearance. On a fine
    // pointer these sit at `opacity: 0` until hover, so what is pinned here is
    // the colour they are revealed IN; the reveal itself is pinned by the
    // opacity assertion at the top of this file, and the state a touch user
    // actually gets — where there is no reveal — is pinned by its own test
    // under `hasTouch`, which does fold opacity into the contrast.
    for (const [btn, row] of [
      [incoming, "row-incoming"],
      [mine, "row-mine"],
    ] as const) {
      const { ink, ground } = await btn.evaluate((e) => ({
        ink: getComputedStyle(e).color,
        ground: getComputedStyle(e.closest(".chat__msg-bubble")!).backgroundColor,
      }));
      expect(contrast(ink, ground), `${row}: resting ink vs its bubble`).toBeGreaterThanOrEqual(3);
    }

    // Hover: back to the bubble's full-strength colour, plus a wash of the same
    // colour so the target still reads as a target. owner asked for the box to
    // go; he did not ask for the hover state to stop existing.
    await mine.hover();
    // ⚠️ WAIT OUT THE TRANSITION. These buttons animate `color` and
    // `background-color` over 0.15s, and a computed value read mid-flight comes
    // back as an interpolated `oklab(...)` that matches nothing — it reads as
    // "the rule did not apply" when in fact it had not finished applying.
    await page.waitForTimeout(400);
    const hovered = await mine.evaluate((e) => {
      const c = getComputedStyle(e);
      return {
        ink: c.color,
        wash: c.backgroundColor,
        bubbleInk: getComputedStyle(e.closest(".chat__msg-bubble")!).color,
      };
    });
    const ink = parseColour(hovered.ink);
    const bubbleInk = parseColour(hovered.bubbleInk);
    for (let i = 0; i < 3; i++) {
      expect(Math.abs(ink[i] - bubbleInk[i])).toBeLessThanOrEqual(1.5);
    }
    expect(ink[3]).toBeGreaterThan(0.95);
    expect(parseColour(hovered.wash)[3]).toBeGreaterThan(0);
  });

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

// The longer English label is the limiting case for the jump control's width.
// Pane width can drop when shell chrome appears at a larger viewport.
for (const width of [300, 320, 336, 360, 390, 600, 640, 760]) {
  test(`width ${width}: the English jump label never reaches the corner controls`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(
      <ChatReplyToStory jumpLabel="Go to the original message" />,
    );

    // ⚠️ WHICH DECLARATION HAS A WITNESS, AND WHICH HAS NONE.
    //
    // 🔴 RE-MEASURED 2026-08-22 AGAINST THIS FILE. The table that sat here on
    // 2026-08-21 named three mutants that CANNOT BE PERFORMED — it asked for
    // `flex: 0 10 auto`, the label's `min-width: 0` and the button's
    // `min-width: 14px` to be mutated, and all three had been deleted from
    // office.css by the very commit that wrote the table. It also pointed at a
    // "run log in the task report" that is not in this repository. Do not copy a
    // table forward, and do not cite evidence that does not live beside the code.
    //
    // Run: one declaration at a time in `src/components/office.css`, restored
    // from a `cp` backup between runs, `npx playwright test -c
    // playwright-ct.config.ts chat-reply-to`. RE-RUN 2026-08-22 after the row was
    // split into two lines; baseline 45 passed. (The table this replaces claimed
    // a baseline of 32, which was already wrong when it was written — the file
    // had 44 tests before this change added one. Do not copy a count forward.)
    //
    //   MUTANT                              RESULT
    //   @container … max-width: 520px→400   1 failed / 44 · width 560
    //                                       "pane 512px: the label must be
    //                                       collapsed to its arrow"
    //   @container … max-width: 520px→900   1 failed / 44 · width 620
    //                                       "pane 572px: the label must be
    //                                       rendered whole"
    //   the whole @container block deleted  3 failed / 42 · widths 300/320 of
    //                                       "row-mine: the arrow reaches the
    //                                       corner controls", and width 560
    //   the two-line split reverted         5 failed / 40 · all four widths of
    //   (`.chat__msg-quote` back to a row,  "the quote row is two lines…", and
    //    `__head` display: contents,        "pane 347px: the quoted sentence
    //    `__body` flex: 1 10000 auto)       still has characters"
    //   .chat__msg-quote__jump              45 passed — NO WITNESS
    //     flex: none → flex: 0 10 auto
    //   .chat__msg-quote__jump-chevron      45 passed — NO WITNESS
    //     flex: none → flex: 1 1 auto
    //
    // ⚠️ THE @container BLOCK'S WITNESS NARROWED FROM 7 ROWS TO 3, AND THAT IS A
    // CONSEQUENCE OF THE SPLIT, NOT A REGRESSION IN THE GUARD. Four of the seven
    // were "the quoted sentence outranks the jump's boilerplate" and width 336 —
    // both of which measured the label stealing width from the EXCERPT. It
    // cannot: they are not on the same line any more. What is left is the label
    // colliding with the corner controls, which is a real failure and is still
    // caught.
    //
    // 🔴 READ THE LAST TWO ROWS AS THEY ARE WRITTEN. Those two declarations are
    // not guarded by anything in this suite, and that is a consequence of the
    // redesign rather than a hole to plug here: a button that is never asked to
    // shrink has no observable flex behaviour. What the widths 600/640/760 buy
    // is not a witness for them — it is that the corner-collision geometry below
    // is measured at all with the label present, which before 2026-08-21 it
    // never was.
    //
    // The loop stops at the first failing row, so a report naming a later row
    // means the earlier ones passed. Do not delete widths from this list on the
    // belief that one of them does the work.
    // ⚠️ THE CSS-PROPERTY TRIPWIRE THAT USED TO SIT HERE IS GONE, AND SO ARE
    // THE DECLARATIONS IT WATCHED. It pinned `overflow: hidden` and
    // `text-overflow: ellipsis` on `.chat__msg-quote__jump-label`, which existed
    // because the label had to be TRIMMABLE: the button was `flex: 0 10 auto`
    // with a `min-width: 14px` floor, so under pressure it shrank and the label
    // ellipsised inside it. That whole mechanism is retired — the label is now
    // either rendered whole or `display: none`, decided by the pane's width, and
    // the button is `flex: none` again.
    //
    // They were removed on the strength of a measurement taken while they still
    // existed — each of the three mutated with the container rule in place, each
    // leaving the suite green. ⚠️ THAT RUN IS NOT REPRODUCIBLE FROM THIS TREE:
    // the declarations are gone, so the mutants cannot be re-performed, and the
    // count it reported ("all 30 tests") is not this file's count (32 today).
    // What IS reproducible, and what the table above records, is the modern
    // equivalent of the same question — `flex: none` → `flex: 0 10 auto` on the
    // button leaves all 32 green — which is the same conclusion reached against
    // the file as it stands. The geometry below is what remains, and it is real.

    for (const row of [
      "row-mine",
      "row-mine-short",
      "row-mine-tight",
      "row-incoming-quote",
    ]) {
      const jump = (await cmp
        .getByTestId(row)
        .locator(".chat__msg-quote__jump")
        .boundingBox())!;
      const acts = (await cmp
        .getByTestId(row)
        .locator(".chat__msg-actions")
        .boundingBox())!;
      const bubble = (await cmp
        .getByTestId(row)
        .locator(".chat__msg-bubble")
        .boundingBox())!;

      // 🔴 MEASURE THE ARROW, NOT THE BOX. The first version of this loop
      // asserted only on the button's own rectangle, and a later review showed
      // why that guards nothing: with `min-width: 0` the button collapsed to
      // ZERO width — satisfying every "does not reach the corner controls"
      // assertion vacuously — while the arrow inside it, being `flex: none`,
      // kept its 12px and painted outside the box and under the corner buttons.
      // The box was innocent; the pixels were not.
      const chevron = (await cmp
        .getByTestId(row)
        .locator(".chat__msg-quote__jump-chevron")
        .boundingBox())!;

      // This one catches a collapse to zero and nothing finer. ⚠️ IT PINS NO
      // NUMBER. There is no `min-width` floor on this button any more — the
      // width it reports is whatever its content comes to (collapsed: the
      // chevron; whole: chevron + gap + label), so 11.5 is a floor below which
      // the control has visibly vanished, not a declaration read back out of the
      // stylesheet. An earlier version of this note claimed the next assertion
      // pinned 14 "measured by setting the floor to 12"; that floor no longer
      // exists and the mutant cannot be performed. What the next assertion
      // actually catches is the arrow painting outside its own button box.
      expect(
        jump.width,
        `${row}: the jump collapsed past its arrow`,
      ).toBeGreaterThanOrEqual(11.5);
      expect(
        chevron.x + chevron.width,
        `${row}: the arrow paints outside its own button`,
      ).toBeLessThanOrEqual(jump.x + jump.width + 0.5);
      expect(
        chevron.x + chevron.width,
        `${row}: the arrow reaches the corner controls`,
      ).toBeLessThanOrEqual(acts.x + 0.5);

      // …and the box itself, which is still worth stating now that it cannot be
      // satisfied by vanishing.
      expect(
        jump.x + jump.width,
        `${row}: jump overlaps the corner controls`,
      ).toBeLessThanOrEqual(acts.x + 0.5);
      expect(
        jump.x + jump.width,
        `${row}: jump escapes the bubble`,
      ).toBeLessThanOrEqual(bubble.x + bubble.width + 0.5);
    }
  });
}

// ── the collapse threshold, in both directions ───────────────────────────────
// The label hides at a pane width of 520px. Pane width matters because shell
// chrome can narrow the pane even when viewport width grows.
for (const width of [560, 620]) {
  test(`width ${width}: the jump label collapses on the PANE's width, at 520`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(
      <ChatReplyToStory jumpLabel="View the original message" />,
    );
    const seen = await cmp.getByTestId("row-mine").evaluate((row) => {
      const pane = document.querySelector(".chat__messages") as HTMLElement;
      const label = row.querySelector(
        ".chat__msg-quote__jump-label",
      ) as HTMLElement;
      const body = row.querySelector(".chat__msg-quote__body") as HTMLElement;
      return {
        paneW: pane.clientWidth,
        display: getComputedStyle(label).display,
        bodyW: body.clientWidth,
      };
    });
    // The mapping the stylesheet promises, read off the box that actually
    // decides it. A viewport-based rule cannot satisfy this in production (the
    // shell breaks the two apart at 721) and a moved threshold cannot satisfy it
    // anywhere.
    expect(
      seen.display,
      `pane ${seen.paneW}px: the label must be ${
        seen.paneW <= 520 ? "collapsed to its arrow" : "rendered whole"
      }`,
    ).toBe(seen.paneW <= 520 ? "none" : "block");
    // POSITIVE CONTROL, and the reason the threshold is where it is: above it
    // the excerpt must still have room to say something WITH the label present.
    // A threshold pushed too high satisfies the line above by collapsing the
    // label everywhere; this one goes red if the label is ever kept at a width
    // where it leaves the quote nothing.
    expect(
      seen.bodyW,
      `pane ${seen.paneW}px: the quoted sentence must still be readable`,
    ).toBeGreaterThan(0);
  });
}

// 🔴 WHAT THE ROW IS FOR MUST GET THE WIDTH. Since 2026-08-21 the quoted
// SENTENCE is shipped with the reply and is the entire value of this row; the
// jump label is boilerplate the arrow already implies (and `aria-label` still
// carries in full). The shrink order in office.css was tuned before that — it
// let the jump keep its intrinsic width and the excerpt absorb everything — and
// measured in a real browser it produced 「Mira 他… Go to the original messa… ›」:
// at 390px/en the excerpt held 26px and the label 154px; at 320px the excerpt
// was 0.
//
// ⚠️ THE EXISTING GUARDS HAVE ZERO DISCRIMINATION HERE, which is why this one
// exists as its own loop. Everything above measures where the jump ENDS (does it
// reach the corner buttons, does the arrow paint outside its box); a 0px excerpt
// satisfies every one of them perfectly. Nothing in this file asked how much of
// the quote survived.
//
// STATED WITHOUT A NUMBER FROM THIS MACHINE, on purpose — this file has been
// burned three times by width-shaped constants. The claim is an ORDERING
// between two things on the same row, and it holds at any width where both are
// present. English, because it reaches the failure first (154px vs 69px), and
// the narrow end, because that is where the row runs out of width at all.
for (const width of [320, 360, 390]) {
  test(`width ${width}: the quoted sentence outranks the jump's boilerplate`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(
      <ChatReplyToStory jumpLabel="Go to the original message" />,
    );

    // ① THE LONG-EXCERPT ROW. Its quoted text cannot fit at these widths, so
    // something must be trimmed — and what is left of the quote must still be
    // worth more room than the control that only says how to navigate to it.
    //
    // ⚠️ AND SINCE 2026-08-22 THIS PARTICULAR COMPARISON HAS LITTLE LEFT TO
    // DISCRIMINATE — say so rather than let it read as a live guard. The two are
    // on different lines now, so the excerpt gets the whole row by construction
    // and this can only fail if the split itself is undone (in which case the
    // two tests named in the mutant table go red first and more loudly). It is
    // kept because the RULE it states is still the rule, and because ② and ③
    // below are not covered anywhere else.
    const share = await cmp.getByTestId("row-mine-short").evaluate((row) => {
      const b = row.querySelector(".chat__msg-quote__body") as HTMLElement;
      const j = row.querySelector(".chat__msg-quote__jump") as HTMLElement;
      return { body: b.clientWidth, jump: j.clientWidth };
    });
    expect(
      share.body,
      "the quoted sentence is the row; the jump label is not",
    ).toBeGreaterThan(share.jump);

    // ② AND A SHORT QUOTE MUST SURVIVE WHOLE. This row quotes one character —
    // there is no version of "not enough room" that justifies eating it. A
    // cut here is the layout spending the row's width on boilerplate.
    const cut = await cmp
      .getByTestId("quote-body-incoming")
      .evaluate((e) => e.scrollWidth > e.clientWidth + 0.5);
    expect(cut, "a one-character quote was trimmed away").toBe(false);

    // ③ POSITIVE CONTROL: the jump is still THERE and still usable. The two
    // assertions above are also satisfied by deleting the control outright,
    // which would take the only way back to the original with it.
    const chevron = (await cmp
      .getByTestId("row-mine-short")
      .locator(".chat__msg-quote__jump-chevron")
      .boundingBox())!;
    expect(chevron.width).toBeGreaterThanOrEqual(11.5);
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
  const cmp = await mount(
    <ChatReplyToStory jumpLabel="View the original message" />,
  );

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
    return {
      bodyW: b.clientWidth,
      rowW: row.clientWidth,
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

  for (const id of ["reply-entry-incoming", "reply-banner-x", "quote-jump"]) {
    const control = cmp.getByTestId(id);
    await expect(control).toHaveJSProperty("tagName", "BUTTON");
    await control.focus();
    await expect(control).toBeFocused();
  }
});

// A coarse pointer has no hover to reveal these buttons with, so the
// `@media (hover: none)` branch IS the state they live in — and on a phone that
// entry is the only way to reply at all. `hasTouch` is what puts the browser in
// that branch; it is a context option, so this half of the file runs under its
// own describe.
test.describe("coarse pointer", () => {
  // `hasTouch` alone is what flips the media branch — verified by inverting each:
  // hasTouch on / isMobile off keeps this green, hasTouch off / isMobile on turns
  // it red. isMobile additionally rewrites the viewport meta, so it is left out.
  test.use({ hasTouch: true });

  for (const width of [390, 1280]) {
  test(`width ${width}: on a touch device the entry is readable without hover`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    // Sanity FIRST: if the coarse branch is not actually active, the buttons sit
    // at the fine-pointer resting opacity of 0 and every contrast below comes
    // out 1:1 — which would read as "the colour rule is broken" rather than
    // "this test never entered the branch it is named after".
    const coarseApplied = await page.evaluate(
      () => window.matchMedia("(hover: none)").matches,
    );
    expect(coarseApplied, "the coarse-pointer branch is not active").toBe(true);

    // 🔴 OPACITY MULTIPLIES THE INK'S OWN ALPHA. That is the whole reason this
    // test exists: the branch said `opacity: 0.55`, calibrated when the ink was
    // opaque, and the ink later became 55% of the bubble's colour — 0.55 × 0.55
    // is 30%, measured at 2.02:1 on your own bubble. Nothing was red; the two
    // rules are forty lines apart and each is defensible alone.
    for (const row of ["row-incoming", "row-mine"]) {
      const { ink, alpha, ground } = await cmp
        .getByTestId(row)
        .locator(".chat__msg-reply")
        .evaluate((e) => {
          const c = getComputedStyle(e);
          return {
            ink: c.color,
            alpha: Number(c.opacity),
            ground: getComputedStyle(e.closest(".chat__msg-bubble")!)
              .backgroundColor,
          };
        });
      const parsed = parseColour(ink);
      const effective = `color(srgb ${parsed[0] / 255} ${parsed[1] / 255} ${
        parsed[2] / 255
      } / ${parsed[3] * alpha})`;
      expect(
        contrast(effective, ground),
        `${row}: coarse-pointer resting ink vs its bubble`,
      ).toBeGreaterThanOrEqual(3);
    }
  });

  }
});
