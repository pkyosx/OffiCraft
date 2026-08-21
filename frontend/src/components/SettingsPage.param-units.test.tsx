// T-d05a · 參數列單位的「性質」護欄（不是文案測試）。
//
// owner 的裁定：不要斷言「這一格的字等於『次』」——那種測試只會把今天的字面
// 抄一份到測試裡，改字就紅、漏字卻不一定紅。這裡釘的是三條性質：
//
//   1) 每一個帶數字輸入的參數列（.param-pct）都必須渲染得出一個單位標記
//      （.param-pct__sign），而且它不是空字串。漏字 ⇒ 紅。
//   2) 那個單位是「從 i18n 解出來的」——用 zh 與 en 兩本**真的字典**各渲染
//      一次，同一格的文字必須不同；相同就只有一種情況可以放行：它整串
//      **不含任何文字字元**（`%` 這種跨語言符號）。繞過字典的英文字面
//      （`rounds`、`BANANAS`）在兩種語言下會一模一樣 ⇒ 紅。
//   3) 英文介面下任何單位格都不含 CJK 字元。硬編中文 ⇒ 紅。
//
// 為什麼要有 (2)：獨立審查造了一顆 mutant，把兩格都改成**繞過字典的英文
// 字面**，只有 (1)+(3) 的版本全綠放行——它們合起來只證明「非空 ＋ 非
// CJK」，連 `BANANAS` 都過。(2) 才是真正把「單位來自 i18n key」釘住的那條。

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { render, fireEvent, type RenderResult } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

/** 任何漢字/假名區段——單位格裡出現這些就代表沒走翻譯。 */
const CJK = /[　-〿぀-ヿ㐀-䶿一-鿿豈-﫿]/;

/** 整串都不是「文字字元」⇒ 跨語言通用符號（`%`），兩種語言相同是合法的。
 *  只要出現任何一個字母/漢字/假名，它就是「詞」，就必須隨語言改變。 */
const SYMBOL_ONLY = /^\P{L}+$/u;

/** 目前設定頁帶單位的參數列至少有 11 列
 *  （Claude 通知/換手 2 + Codex 通知/回合 2 + 監控刷新 1 + 文件上限 5 + 聊天預算 1）。
 *  刻意用「至少」而不是「等於」：合法新增一列不該讓這裡誤紅——漏單位由下面
 *  的 `missing` 那條指名抓，它比一個 `12 to be 11` 的數字有用得多。 */
const MIN_UNIT_ROWS = 11;

/** 一格單位：它所屬參數列的標題（給失敗訊息用）＋ 單位標記的文字。 */
interface UnitCell {
  label: string;
  /** null = 這一列根本沒渲染出 .param-pct__sign。 */
  sign: string | null;
}

function readUnitCells(utils: RenderResult): UnitCell[] {
  return Array.from(
    utils.container.querySelectorAll<HTMLElement>(".param-pct")
  ).map((cell, i) => {
    const sign = cell.querySelector<HTMLElement>(".param-pct__sign");
    return {
      label:
        cell.closest(".param-row")?.querySelector(".param-row__name")
          ?.textContent?.trim() ?? `#${i}`,
      sign: sign ? (sign.textContent ?? "").trim() : null,
    };
  });
}

async function openParams(dict: typeof zh) {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(dict.settings.sessionTtl); // settings loaded
  return utils;
}

/** 語言偏好存在 localStorage，provider 掛載時讀它，所以這裡渲染的是**真的
 *  英文字典**，而不是拿 key 對 key 自我證明。 */
async function openParamsIn(locale: "zh" | "en"): Promise<UnitCell[]> {
  window.localStorage.setItem("oc.language", locale);
  const utils = await openParams(locale === "zh" ? zh : en);
  const cells = readUnitCells(utils);
  utils.unmount();
  return cells;
}

beforeEach(() => {
  __resetMock();
});
afterEach(() => {
  window.localStorage.removeItem("oc.language");
});

describe("SettingsPage · 參數列單位（性質）", () => {
  it("每一個帶輸入框的參數列都渲染得出一個非空的單位標記", async () => {
    const cells = await openParamsIn("zh");
    expect(cells.length).toBeGreaterThanOrEqual(MIN_UNIT_ROWS);

    const missing = cells
      .filter((c) => c.sign === null || c.sign === "")
      .map((c) => c.label);

    expect(missing).toEqual([]);
  });

  it("單位字是從 i18n 解出來的——兩種語言下同一格的字必須不同（純符號除外）", async () => {
    const zhCells = await openParamsIn("zh");
    const enCells = await openParamsIn("en");
    expect(enCells.length).toBe(zhCells.length);

    // 兩種語言渲染出一模一樣的「詞」⇒ 那個詞沒有經過字典，是寫死的字面。
    // `%` 這種不含文字字元的符號是唯一放行的相同值。
    const hardcoded = zhCells
      .map((c, i) => ({ label: c.label, zh: c.sign ?? "", en: enCells[i].sign ?? "" }))
      .filter((r) => r.zh === r.en && !SYMBOL_ONLY.test(r.zh))
      .map((r) => `${r.label}: ${r.zh}`);

    expect(hardcoded).toEqual([]);
  });

  it("英文介面下沒有任何單位格是中文", async () => {
    const cells = await openParamsIn("en");
    const cjk = cells
      .filter((c) => CJK.test(c.sign ?? ""))
      .map((c) => `${c.label}: ${c.sign}`);

    expect(cjk).toEqual([]);
  });
});
