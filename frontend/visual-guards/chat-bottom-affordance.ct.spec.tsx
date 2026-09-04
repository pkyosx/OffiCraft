// GUARD (T-48) — 底部兩個元件的版面契約：回到最新箭頭 ① 與新訊息預覽列 ②。
//
// jsdom 已經釘完「什麼時候出現、誰讓位給誰、點了跳到哪一則」
// （ChatArea.unread-jump.test.tsx + lib/chatBottomAffordance.test.ts）。這裡補的
// 是 jsdom **量不到**的四件，每一件都對應一個真的會壞掉的方式：
//
//  ① 箭頭是絕對定位的圓，必須在訊息面板的**右下角、輸入框正上方**。定位寫錯
//     （left 而不是 right、忘了 position:absolute、bottom 給太大）在 jsdom 完全
//     看不見 —— offsetHeight 永遠 0，絕對定位不解算。
//  ② 預覽列**兩行、各自裁掉、高度固定**。高度固定是這條列最重要的性質，因為它
//     「更新內容不堆疊」：同一條列會被下一則訊息換掉內容，一旦會折行，每來一則
//     訊息輸入框就在打字的人手底下跳一次。空 body（純附件訊息）也算一種內容。
//  ③ 預覽列排在回覆橫幅**上面**（owner 指定）。DOM 順序由 ChatArea 決定並由
//     jsdom 釘住；這裡量的是「即使 DOM 順序對了，CSS 也沒有把它翻回去」。
//  ④ 顏色**全部走 token**。寫死一個 #fff 在內建（深色）theme 下看起來完全正常，
//     只有換上淺色 theme 才會現形 —— 所以兩個 theme 都量。
//
// 窄寬兩寬都量：390 是實機最窄的一格，1280 是桌面。兩者對這兩個元件是不同的
// 失敗面（窄寬時預覽列的兩行才真的會被裁，箭頭才真的貼到面板邊緣）。
import { test, expect } from "@playwright/experimental-ct-react";
import {
  ChatBottomAffordanceStory,
  LatestRowInViewStory,
  NewMsgPreviewHeightStory,
} from "./stories/ChatBottomAffordanceStory";
import { LIGHT_PACK } from "./stories/chatBottomAffordanceFixtures";

type Rgba = { r: number; g: number; b: number; a: number };

function parseColour(s: string): Rgba {
  const rgb = s.match(/rgba?\(([^)]+)\)/i);
  if (rgb) {
    const p = rgb[1].split(/[,/]/).map((x) => parseFloat(x.trim()));
    return { r: p[0], g: p[1], b: p[2], a: p[3] === undefined ? 1 : p[3] };
  }
  const srgb = s.match(/color\(\s*srgb\s+([^)]+)\)/i);
  if (srgb) {
    const [chans, alpha] = srgb[1].split("/").map((x) => x.trim());
    const c = chans.split(/\s+/).map((x) => parseFloat(x));
    return {
      r: c[0] * 255,
      g: c[1] * 255,
      b: c[2] * 255,
      a: alpha === undefined ? 1 : parseFloat(alpha),
    };
  }
  throw new Error(`unparseable colour: ${s}`);
}

function over(fg: Rgba, bg: Rgba): Rgba {
  return {
    r: fg.r * fg.a + bg.r * (1 - fg.a),
    g: fg.g * fg.a + bg.g * (1 - fg.a),
    b: fg.b * fg.a + bg.b * (1 - fg.a),
    a: 1,
  };
}

function contrast(fg: string, bg: string): number {
  const lum = ({ r, g, b }: Rgba) => {
    const lin = (v: number) => {
      const c = v / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
  };
  const back = parseColour(bg);
  const l1 = lum(over(parseColour(fg), back));
  const l2 = lum(back);
  const [hi, lo] = l1 >= l2 ? [l1, l2] : [l2, l1];
  return (hi + 0.05) / (lo + 0.05);
}

/** 把一條祖先鏈的底色疊成一個不透明的顏色。最底下沒有東西時是白畫布。 */
function flatten(layers: string[]): string {
  let acc: Rgba = { r: 255, g: 255, b: 255, a: 1 };
  for (const l of [...layers].reverse()) acc = over(parseColour(l), acc);
  return `rgb(${acc.r}, ${acc.g}, ${acc.b})`;
}

const WIDTHS = [390, 1280];

for (const width of WIDTHS) {
  test(`width ${width}: ⑤ 「最新那一則在不在視野內」量的是那一列，不是盒子的底部`, async ({
    mount,
    page,
  }) => {
    // 🔴 出貨時壞掉的就是這一格(T-48)。`.chat__messages` 是 gap 的 flex 欄，
    // 最後一列下面還有一個零高哨兵，所以**盒子的可捲底部不是最新那一列的底部**。
    // 舊的判準問的是盒子(`scrollHeight - scrollTop - clientHeight <= 4px`)，於是
    // 在一個最新訊息完整可見的畫面上答「還沒到最新」，回到最新的箭頭按了不會走。
    // 真瀏覽器 12/12 次量到 distance=12/11 而 rowBottomGap=+0.13/-0.50。
    //
    // 這一條把那個差別本身釘住:落在 `scrollToLatest` 的落點上，
    //   · `distance` 必須 > 0    ← 盒子底下真的還有版面(這是版面事實，不是常數)
    //   · `rowBottomGap` ≈ 0     ← 而最新那一列已經完整在視野裡
    //   · `inView` 必須 true     ← 產品的判準要跟得上眼睛
    // 把 `isLatestRowInView` 換回任何一種「盒子捲到底了沒」的寫法，第三行就紅。
    // 沒有任何一個數字被寫死成 gap 的大小:gap 改成 40px，這條照樣成立。
    await page.setViewportSize({ width, height: 600 });
    const bottom = await mount(<LatestRowInViewStory at="bottom" />);
    const landed = JSON.parse(
      await bottom.getByTestId("latest-probe").innerText(),
    );
    expect(
      landed.distance,
      "前提:盒子的底部在最新那一列底下 —— 沒有這段版面就沒有這個 bug，這條也就沒有在量東西",
    ).toBeGreaterThan(0);
    expect(
      Math.abs(landed.rowBottomGap),
      "落點:最新那一列的底邊貼齊視窗底邊",
    ).toBeLessThanOrEqual(1);
    expect(
      landed.inView,
      "最新那一列完整在視野裡 ⇒ 判準必須說「在」，否則箭頭永遠不會走",
    ).toBe(true);

    await bottom.unmount();
    // 🔴 前兩格加起來夾不住這個判準的「寬度」:一個貼齊、一個差好幾千 px，把容忍
    // 值灌大到能吞掉 gap 仍然兩格全綠(實測:1 改成 40，37 支 jsdom ＋ 這支全綠 —— 獨立
    // 審查 #17 F-2)。這一格把最新那一列壓到視窗底邊下面**半個 flex gap**(從 CSS 讀，
    // 不是打字打進去的 —— 獨立審查 #18 A-6:寫死的數字會在別人改版面時靜默失去意義，
    // 那正是它要防的那個病)。還是被切掉、人還是會想要那顆箭頭 ⇒ 判準必須說「不在」，
    // 容忍值一旦大到吞得下半個 gap，這行就紅。
    const clipped = await mount(<LatestRowInViewStory at="just-below" />);
    const under = JSON.parse(
      await clipped.getByTestId("latest-probe").innerText(),
    );
    expect(
      under.rowBottomGap,
      "前提:最新那一列的底邊真的落在視窗底邊下面(不然這一格沒有在量東西)",
    ).toBeGreaterThan(1);
    expect(
      under.inView,
      "最新那一列被切掉半個 gap ⇒ 判準必須說「不在」，容忍值不准長到蓋過版面距離",
    ).toBe(false);

    await clipped.unmount();
    const top = await mount(<LatestRowInViewStory at="top" />);
    const away = JSON.parse(await top.getByTestId("latest-probe").innerText());
    expect(
      away.inView,
      "捲到最上面 ⇒ 最新那一列在視野外，判準必須說「不在」(否則它只是永遠回 true)",
    ).toBe(false);
  });


  test(`width ${width}: ① 箭頭是圓的，貼在訊息面板右下角、輸入框正上方`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatBottomAffordanceStory />);

    const arrow = cmp.getByTestId("chat-jump-latest");
    await expect(arrow).toBeVisible();
    const box = (await arrow.boundingBox())!;
    const pane = (await cmp.locator(".chat__body").boundingBox())!;
    const composer = (await cmp.locator(".chat__composer").boundingBox())!;

    // 圓：正方形 + 半徑至少到半寬。`border-radius: 999px` 會被算成
    // `999px`（不解算成實際半徑），所以比的是宣告值有沒有到一半以上；把它改成
    // 6px 就紅在這一行。
    expect(Math.abs(box.width - box.height)).toBeLessThanOrEqual(0.5);
    const radius = await arrow.evaluate(
      (el) => parseFloat(getComputedStyle(el).borderTopLeftRadius),
    );
    expect(
      radius,
      "圓形箭頭：半徑至少要到一半寬，否則畫出來是圓角方塊",
    ).toBeGreaterThanOrEqual(box.width / 2);

    // 右下角：貼右邊、貼底部，而且整顆在面板裡。用「離右邊比離左邊近很多」表達
    // 「靠右」—— 把 `right: 12px` 改成 `left: 12px` 就紅在這裡。
    const fromRight = pane.x + pane.width - (box.x + box.width);
    const fromLeft = box.x - pane.x;
    expect(fromRight, "箭頭必須貼在面板右緣").toBeLessThanOrEqual(24);
    expect(fromRight, "箭頭不得溢出面板右緣").toBeGreaterThanOrEqual(0);
    expect(
      fromLeft,
      "箭頭必須在右半邊 —— 置中或靠左都是舊 chip 的位置",
    ).toBeGreaterThan(pane.width / 2);

    // 輸入框正上方：整顆在 composer 之上，且貼著面板底部（不是浮在中間）。
    // 拿掉 `position: absolute` 會讓它跟著訊息流走，兩條都紅。
    expect(
      box.y + box.height,
      "箭頭必須完全在輸入框上方",
    ).toBeLessThanOrEqual(composer.y + 0.5);
    const fromBottom = pane.y + pane.height - (box.y + box.height);
    expect(fromBottom, "箭頭必須貼在面板底緣").toBeLessThanOrEqual(24);
    expect(fromBottom).toBeGreaterThanOrEqual(0);
  });

  test(`width ${width}: ② 預覽列兩行、各自裁掉，高度不隨內容改變`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<NewMsgPreviewHeightStory />);

    const strip = (id: string) =>
      cmp.getByTestId(`preview-case-${id}`).getByTestId("chat-new-msg-preview");

    const measure = async (id: string) =>
      strip(id).evaluate((el) => {
        const who = el.querySelector(".chat__new-msg__who") as HTMLElement;
        const body = el.querySelector(".chat__new-msg__body") as HTMLElement;
        const x = el.querySelector(".chat__new-msg__x") as HTMLElement;
        // 🔴 數的是有幾條**基線**，不是幾個 rect。Range.getClientRects() 會把
        // 一行裡的每一個文字 run 各給一個 rect，而預覽列的內容是中英混排 ——
        // 實測 390 下寄件者那一行是同一個 y 上的兩個 rect，直接數 rect 會誤判成
        // 折行，然後這條 guard 會對著一個沒有壞掉的版面喊紅。
        const lines = (n: HTMLElement) => {
          const r = document.createRange();
          r.selectNodeContents(n);
          return new Set(
            [...r.getClientRects()].map((x) => Math.round(x.top)),
          ).size;
        };
        return {
          h: el.getBoundingClientRect().height,
          right: el.getBoundingClientRect().right,
          whoLines: lines(who),
          bodyLines: lines(body),
          whoClipped: who.scrollWidth > who.clientWidth,
          bodyClipped: body.scrollWidth > body.clientWidth,
          xRight: x.getBoundingClientRect().right,
          xW: x.getBoundingClientRect().width,
          xH: x.getBoundingClientRect().height,
        };
      });

    const short = await measure("short");
    const long = await measure("long");
    const empty = await measure("empty");

    // 🔴 高度固定，這是這條測試的主張。把 `.chat__new-msg__text` 的
    // `white-space: nowrap` 拿掉，long 那條就長成好幾行，這一行立刻紅。
    expect(
      long.h,
      "長訊息不得把預覽列撐高 —— 內容會被下一則換掉，撐高就是輸入框在手底下跳",
    ).toBeCloseTo(short.h, 0);
    expect(
      empty.h,
      "空 body（純附件訊息）也不得讓預覽列縮矮",
    ).toBeCloseTo(short.h, 0);

    // 兩行，各自一個行框 —— 不是「總高小於一行」，行數本身就是契約。
    for (const [id, m] of [
      ["short", short],
      ["long", long],
    ] as const) {
      expect(m.whoLines, `${id}: 寄件者恰好一行`).toBe(1);
      expect(m.bodyLines, `${id}: 內容摘錄恰好一行`).toBe(1);
    }

    // 長的那條真的被裁了 —— 否則上面的等高只是因為 fixture 本來就塞得下，這條
    // 測試會在一個什麼都沒發生的版面上綠。
    //
    // ⚠️ 寄件者只在窄寬下斷言。display name 再長也是有限的（這裡是 80 字，實測
    // 648px），1280 的預覽列容得下它 —— 那不是缺陷，那是寬螢幕。要在 1280 也逼出
    // 裁切就得寫一個沒有人取得出來的名字，那會讓 fixture 變成假的。內容摘錄則是
    // 兩寬都斷言：訊息長度沒有上限，寬螢幕塞不下才是常態。
    if (width <= 390) {
      expect(long.whoClipped, "窄寬下長寄件者必須被裁").toBe(true);
    }
    expect(long.bodyClipped, "長內容必須被裁").toBe(true);

    // × 是 24px 觸控框，貼最右，而且位置不隨文字長度移動。
    expect(short.xW).toBeCloseTo(24, 0);
    expect(short.xH).toBeCloseTo(24, 0);
    expect(
      Math.abs(long.xRight - short.xRight),
      "× 的位置不得隨被預覽的訊息長短移動",
    ).toBeLessThanOrEqual(0.5);
    expect(short.right - short.xRight, "× 貼在預覽列最右").toBeLessThanOrEqual(12);
  });

  test(`width ${width}: ② 預覽列排在回覆橫幅上面`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(
      <ChatBottomAffordanceStory arrow={false} preview banner />,
    );
    const preview = (await cmp
      .getByTestId("chat-new-msg-preview")
      .boundingBox())!;
    const banner = (await cmp.getByTestId("chat-reply-banner").boundingBox())!;
    const composerRow = (await cmp.getByTestId("composer-row").boundingBox())!;
    expect(
      preview.y + preview.height,
      "預覽列必須完全在回覆橫幅上方（owner 指定的順序）",
    ).toBeLessThanOrEqual(banner.y + 0.5);
    expect(
      banner.y + banner.height,
      "回覆橫幅仍然緊貼輸入框 —— 預覽列不得插到它們中間",
    ).toBeLessThanOrEqual(composerRow.y + 0.5);
  });
}

// ④ 顏色全部走 token：兩個 theme 都量，而且量的是「換 theme 之後顏色真的變了」。
// 寫死一個 #fff 在內建 theme 下看起來完全正常 —— 只有這一條會紅。
test("④ 箭頭與預覽列的顏色全部跟著 theme 走，兩個 theme 下字都看得見", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatBottomAffordanceStory preview />);

  // 🔴 量的是「被畫出來的那個顏色」，不是宣告值。預覽列的底是半透明的，箭頭浮在
  // 面板上，所以每個元素的有效底色要把祖先鏈上的每一層疊回去 —— 只讀
  // `backgroundColor` 會拿到 `rgba(0,0,0,0)`，然後算出一個沒有意義的對比度。
  const sample = () =>
    page.evaluate(() => {
      const stack = (el: HTMLElement): string[] => {
        const out: string[] = [];
        let node: HTMLElement | null = el;
        while (node) {
          out.push(getComputedStyle(node).backgroundColor);
          node = node.parentElement;
        }
        return out;
      };
      const read = (sel: string) => {
        const el = document.querySelector(sel) as HTMLElement;
        const cs = getComputedStyle(el);
        return {
          background: cs.backgroundColor,
          border: cs.borderTopColor,
          borderWidth: cs.borderTopWidth,
          color: cs.color,
          layers: stack(el),
        };
      };
      return {
        arrow: read(".chat__jump-latest"),
        strip: read(".chat__new-msg"),
        who: read(".chat__new-msg__who"),
        body: read(".chat__new-msg__body"),
      };
    });

  const dark = await sample();
  await page.evaluate((pack) => {
    for (const [k, v] of Object.entries(pack))
      document.documentElement.style.setProperty(k, v);
  }, LIGHT_PACK);
  const light = await sample();

  for (const [what, a, b] of [
    ["箭頭底色", dark.arrow.background, light.arrow.background],
    ["箭頭邊框", dark.arrow.border, light.arrow.border],
    ["箭頭圖示", dark.arrow.color, light.arrow.color],
    ["預覽列邊框", dark.strip.border, light.strip.border],
    ["預覽列文字", dark.strip.color, light.strip.color],
    ["預覽列寄件者", dark.who.color, light.who.color],
  ] as const) {
    expect(a, `${what} 必須跟著 theme 換 —— 一樣就是寫死了顏色`).not.toBe(b);
  }

  // …而且兩個 theme 下都真的看得見。這是 token 化真正要保證的事 —— 換了 theme
  // 顏色會變，但變完之後還是讀得到。
  await expect(cmp.getByTestId("chat-jump-latest")).toBeVisible();
  for (const [label, s] of [
    ["built-in", dark],
    ["light pack", light],
  ] as const) {
    expect(
      contrast(s.arrow.color, flatten(s.arrow.layers)),
      `${label}: 箭頭的圖示`,
    ).toBeGreaterThanOrEqual(4.5);
    // ⚠️ 邊框只斷言「存在」，不斷言對比度。內建 theme 的 `--color-border` 對
    // `--color-card` 實測只有 1.16:1 —— 那是這個座艙一貫的 hairline 語言（退場的
    // 「有新訊息」藥丸用的是同一組 token），不是這張票引進的問題。在這裡放一個
    // 對比度門檻等於用一條 T-48 的 guard 去否決既有 theme，紅的會是無辜的人。
    expect(
      parseFloat(s.arrow.borderWidth),
      `${label}: 箭頭必須有邊框 —— 它浮在訊息上，沒有邊界就融進去了`,
    ).toBeGreaterThan(0);
    expect(
      contrast(s.who.color, flatten(s.who.layers)),
      `${label}: 預覽列的寄件者`,
    ).toBeGreaterThanOrEqual(4.5);
    expect(
      contrast(s.body.color, flatten(s.body.layers)),
      `${label}: 預覽列的內容摘錄`,
    ).toBeGreaterThanOrEqual(4.5);
  }
});
