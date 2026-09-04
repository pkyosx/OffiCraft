// GUARD (T-48) — 跳轉提示列 `.chat__jump-miss` 的版面與可讀性。
//
// 這條列是這張票唯一新增的 DOM/CSS，而它到目前為止**沒有任何視覺護欄**：
// jsdom 釘住了「什麼時候出現、寫哪一句、關得掉」，但版面一格都量不到（沒有版面
// 引擎，offsetHeight 永遠 0，@media 不對視窗求值）。這裡補的是三件真的會壞的事：
//
//  ① 它坐在 composer 上面，而且**不得把輸入框擠出視窗**。出現時機是「跳轉剛落
//     空」——正是使用者準備打字的那一刻，輸入框被推走是這條列最貴的失敗方式。
//  ② 文字**不得溢出**自己的框。兩句話長度差很多（英文那句最長），390 的窄版是
//     真正會現形的那一格。
//  ③ 顏色全部走 token：換 theme 要跟著換，而且換完之後**兩個 theme 都讀得到**。
//     寫死一個顏色在內建（深色）theme 下完全正常，只有淺色 theme 才會現形。
//
// 兩句話都畫、窄寬兩寬都量、兩個 theme 都看 —— 因為這三個維度各自都藏得住一種
// 只在其中一格出現的壞法。
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatJumpNoticeStory } from "./stories/ChatBottomAffordanceStory";
import { LIGHT_PACK } from "./stories/chatBottomAffordanceFixtures";
import { en } from "../src/i18n/locales/en";
import { zh } from "../src/i18n/locales/zh";

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

function flatten(layers: string[]): string {
  let acc: Rgba = { r: 255, g: 255, b: 255, a: 1 };
  for (const l of [...layers].reverse()) acc = over(parseColour(l), acc);
  return `rgb(${acc.r}, ${acc.g}, ${acc.b})`;
}

const WIDTHS = [390, 1280];
// 四種真的會出現在螢幕上的字：兩句話 × 兩個語系。英文那兩句明顯更長，而窄版
// (390) 是它們唯一會現形的地方 —— 只量預設語系等於挑了一句一定塞得下的。
// ⚠️ 第三欄 = 那句話旁邊有沒有重試鈕，而它必須跟 `ChatArea` 的算式對齊：
// 「找不到」沒有(server 已經答了,再問一次不會不一樣)，另外兩句都有(T-48 R3-5：
// interrupted 原本叫使用者「再點一次連結」,而同一條連結再點不會觸發任何事)。
const COPY = [
  ["zh · missing", zh.chat.jumpTargetMissing, false],
  ["zh · interrupted", zh.chat.jumpTargetInterrupted, true],
  ["zh · unreachable", zh.chat.jumpTargetUnreachable, true],
  ["en · missing", en.chat.jumpTargetMissing, false],
  ["en · interrupted", en.chat.jumpTargetInterrupted, true],
  ["en · unreachable", en.chat.jumpTargetUnreachable, true],
] as const;

for (const width of WIDTHS) {
  for (const [label, copy, retry] of COPY) {
    test(`width ${width} · ${label}: 提示列在輸入框上面，而且沒有把輸入框擠走`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 600 });
      const cmp = await mount(
        <ChatJumpNoticeStory text={copy} retry={retry} />,
      );

      const notice = cmp.getByTestId("jump-miss");
      await expect(notice).toBeVisible();
      const box = (await notice.boundingBox())!;
      const row = (await cmp.getByTestId("composer-row").boundingBox())!;

      // ① 在輸入框「上面」，不是蓋住它、也不是排到下面去。
      expect(
        box.y + box.height,
        "提示列必須整條在輸入框上方",
      ).toBeLessThanOrEqual(row.y + 1);
      // …而且它**不准把對話吃掉**。這是這條列真正的成本：它出現的那一刻正是使用
      // 者要打字的那一刻，而 `.chat` 是一個固定高度的 flex 欄 —— 提示列長高不會
      // 把輸入框頂到畫面外（那條寫法量不到任何東西，實測 `height: 420px` 的
      // mutant 在它底下 9 支全綠），會做的是把訊息面板壓扁。所以量的是**比例**。
      const chat = (await cmp.locator(".chat").boundingBox())!;
      const msgs = (await cmp.locator(".chat__messages").boundingBox())!;
      expect(
        box.height / chat.height,
        "提示列不得佔掉聊天面板四分之一以上的高度",
      ).toBeLessThanOrEqual(0.25);
      expect(
        msgs.height / chat.height,
        "訊息面板必須還是這個畫面的主體",
      ).toBeGreaterThanOrEqual(0.5);
      expect(row.y, "輸入框不得被頂到視窗上緣之外").toBeGreaterThanOrEqual(0);

      // ② 文字不得溢出自己的框 —— 兩句話長度差很多，390 才是真正會現形的那格。
      // ⚠️ 用 Range 量**真的畫出來的字**的邊界，不用 scrollWidth：
      // `overflow: visible` 的元素，scrollWidth 會等於 clientWidth，於是
      // `white-space: nowrap` 的 mutant（長句子整條噴出框外）量起來是零溢出 ——
      // 實測那個 mutant 在 scrollWidth 的寫法下 5 支全綠。
      const spill = await notice.evaluate((el) => {
        const t = el.querySelector('[data-testid="jump-miss-text"]') as HTMLElement;
        const range = document.createRange();
        range.selectNodeContents(t);
        const text = range.getBoundingClientRect();
        const strip = el.getBoundingClientRect();
        const cs = getComputedStyle(el);
        const padR = parseFloat(cs.paddingRight);
        const padL = parseFloat(cs.paddingLeft);
        return {
          right: text.right - (strip.right - padR),
          left: strip.left + padL - text.left,
          below: text.bottom - strip.bottom,
        };
      });
      expect(spill.right, "提示文字不得從右邊噴出提示列").toBeLessThanOrEqual(1);
      expect(spill.left, "提示文字不得從左邊噴出提示列").toBeLessThanOrEqual(1);
      expect(spill.below, "提示文字不得從下緣噴出提示列").toBeLessThanOrEqual(1);
      // 整條列也不得比視窗寬。
      expect(box.width).toBeLessThanOrEqual(width);

      // × 一直在，而且是可以按到的大小（不是一個 2px 的點）。
      const x = (await cmp.getByTestId("jump-miss-x").boundingBox())!;
      expect(x.width).toBeGreaterThanOrEqual(8);
      expect(x.height).toBeGreaterThanOrEqual(8);

      // 讀取失敗那一種還多一顆「再試一次」：它是這條提示唯一的出路。
      //
      // ⚠️ 這裡量的是**它跟文字不重疊**，不是「它有沒有被擠扁」。擠扁那條我試不出
      // 一個會紅的 mutant：文字那格是 `flex: 1; min-width: 0`，收縮永遠先發生在
      // 文字上，鈕不會變窄（實測拿掉 `flex: none`、把 padding 歸零都殺不掉那條斷言，
      // 所以那是一條沒有牙的斷言，刪掉而不是留著充數）。會真的發生的是另一種：
      // 有人把它改成絕對定位或負 margin 去「靠右一點」，於是它壓在文字上。
      if (retry) {
        const btn = (await cmp.getByTestId("jump-miss-retry").boundingBox())!;
        const txt = (await cmp.getByTestId("jump-miss-text").boundingBox())!;
        expect(
          btn.x,
          "重試鈕不得壓在提示文字上 —— 兩個都在同一列，重疊就是兩個都讀不清",
        ).toBeGreaterThanOrEqual(txt.x + txt.width - 1);
        expect(
          btn.x + btn.width,
          "重試鈕不得被推出提示列",
        ).toBeLessThanOrEqual(box.x + box.width + 1);
      }
    });
  }
}

test("顏色是 token 畫的（換得動），而且內建值真的讀得到", async ({
  mount,
  page,
}) => {
  // ⚠️ 這條刻意**不**用 LIGHT_PACK 去斷言「換 theme 顏色要變」，因為那會是假的：
  // 警示色是自己的一組 token（`--color-warn-*`，與 `.chat__gap-notice` 共用），
  // LIGHT_PACK 那份真實出貨過的 pack 並沒有覆寫它們 —— 於是不變才是正確行為，
  // 拿它當斷言只會把一條既有設計判紅。這裡量的是真正該成立的兩件：
  //  ① 這片面**是 token 畫的**：覆寫那組 token，畫面就得跟著變（寫死顏色會紅）。
  //  ② 內建值下**讀得到**（AA 4.5:1）。它是不透明的底，所以外層 pack 換不換都
  //     不影響這個比值 —— 為了不留一條永遠成立的假斷言，這裡只量它自己那一格。
  await page.setViewportSize({ width: 1280, height: 600 });
  const cmp = await mount(
    <ChatJumpNoticeStory text={en.chat.jumpTargetInterrupted} />,
  );
  await expect(cmp.getByTestId("jump-miss")).toBeVisible();

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
      const el = document.querySelector(".chat__jump-miss") as HTMLElement;
      const cs = getComputedStyle(el);
      return {
        background: cs.backgroundColor,
        border: cs.borderTopColor,
        borderWidth: cs.borderTopWidth,
        color: cs.color,
        layers: stack(el),
      };
    });

  const builtIn = await sample();
  expect(
    contrast(builtIn.color, flatten(builtIn.layers)),
    "內建警示色下,提示文字要讀得到",
  ).toBeGreaterThanOrEqual(4.5);
  expect(
    parseFloat(builtIn.borderWidth),
    "提示列要有邊框 —— 它是一片警示色的面,沒有邊界就融進 composer",
  ).toBeGreaterThan(0);

  // ① token 化:換掉那三顆,畫面就得跟著換。
  await page.evaluate(() => {
    const r = document.documentElement.style;
    r.setProperty("--color-warn-bg", "#fff4d6");
    r.setProperty("--color-warn-fg", "#4a3708");
    r.setProperty("--color-warn-border", "#c9a648");
  });
  const swapped = await sample();
  for (const [what, a, b] of [
    ["提示列底色", builtIn.background, swapped.background],
    ["提示列文字", builtIn.color, swapped.color],
    ["提示列邊框", builtIn.border, swapped.border],
  ] as const) {
    expect(a, `${what} 必須由 token 決定 —— 一樣就是寫死了顏色`).not.toBe(b);
  }
  expect(
    contrast(swapped.color, flatten(swapped.layers)),
    "換上淺色警示盤之後也要讀得到",
  ).toBeGreaterThanOrEqual(4.5);

  // …而在那之外,外層 theme pack 換掉整片座艙的底色時,這條列仍然畫得出來。
  await page.evaluate((pack) => {
    for (const [k, v] of Object.entries(pack))
      document.documentElement.style.setProperty(k, v);
  }, LIGHT_PACK);
  await expect(cmp.getByTestId("jump-miss")).toBeVisible();
});
