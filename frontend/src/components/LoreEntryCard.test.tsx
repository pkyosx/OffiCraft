// 條目詳情卡的五格 (T-33)。
//
// 這一支存在的理由是**第 5 格**:事件是這一輪才接到線上的一格,前端本來完全沒有
// 它。而它最容易出錯的方式不是「畫不出來」,是「畫得太滿」——人／地／物空著是
// 合法的,補一個「未知」上去,畫面就再也分不出「查不出是誰」跟「還沒有人去查」,
// 而兩者長得一樣的那一刻,這一格就沒有在記錄任何東西了。
//
// 同一輪也鎖住 degraded 沒有回來(owner 裁定 rc-1e32c690018d 整個移除)。

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import type {
  LoreEntryDetailView,
  LoreEntrySummaryView,
} from "../types";
import { LoreEntryCard } from "./LoreEntryCard";

const getLoreEntry = vi.fn();

vi.mock("../api", () => ({
  api: {
    searchLore: vi.fn(),
    getLoreEntry: (...args: unknown[]) => getLoreEntry(...args),
    getLoreRevision: vi.fn(),
  },
}));

function summary(
  over: Partial<LoreEntrySummaryView> = {},
): LoreEntrySummaryView {
  return {
    entryId: "lore-1",
    // 標題與第 1 格刻意不是同一句話：一個把兩格接反的元件，兩邊值相同時會看起來
    // 完全正確。
    heading: "整套測試綠燈，而它跑過的分母是零",
    impactStars: 2,
    trigger: "整套測試回 PASS，而我正要拿這個結果去說這一包沒問題。",
    subjects: ["repo:officraft"],
    origin: "agent:Kyle",
    ...over,
  };
}

function detail(over: Partial<LoreEntryDetailView> = {}): LoreEntryDetailView {
  return {
    entryId: "lore-1",
    heading: "整套測試綠燈，而它跑過的分母是零",
    impactStars: 2,
    reviewed: false,
    trigger: "整套測試回 PASS，而我正要拿這個結果去說這一包沒問題。",
    content: "綠燈只證明它看得到的那些東西沒問題。",
    retireWhen: "",
    impact: "",
    events: [],
    subjects: ["repo:officraft"],
    origin: "agent:Kyle",
    status: "active",
    original: "trigger:\n…\n\ncontent:\n…\n\nretire_when:\n\n\nimpact:\n\n\nevents:\n\n",
    sha256: "a".repeat(64),
    supersedes: "",
    writtenBy: "agent:Kyle",
    revisions: [],
    ...over,
  };
}

function renderCard(over: Partial<LoreEntrySummaryView> = {}) {
  return render(
    <I18nProvider>
      <LoreEntryCard entry={summary(over)} />
    </I18nProvider>,
  );
}

async function open() {
  fireEvent.click(screen.getByRole("button", { name: zh.lore.entryOpen }));
  await screen.findByText(zh.lore.fieldTrigger);
}

beforeEach(() => {
  getLoreEntry.mockReset();
});

describe("LoreEntryCard 五格", () => {
  it("展開後五格都印出欄位名，選填的空格印成空白而不是消失", async () => {
    getLoreEntry.mockResolvedValue(detail());
    renderCard();
    await open();

    for (const name of [
      zh.lore.fieldHeading,
      zh.lore.fieldTrigger,
      zh.lore.fieldContent,
      zh.lore.fieldRetireWhen,
      zh.lore.fieldImpact,
      zh.lore.fieldImpactStars,
      zh.lore.fieldEvents,
    ]) {
      expect(screen.getByText(name)).toBeTruthy();
    }
    // 第 3、4 格空著 —— 兩格都要看得出來是「空白」,不是「沒有這一節」。
    expect(screen.getAllByText(zh.lore.fieldEmpty).length).toBe(2);
    // 六格時代的欄位名一個都不准回來。
    expect(screen.queryByText(/證偽條件/)).toBeNull();
    expect(screen.queryByText(/殘餘風險/)).toBeNull();
  });

  it("一筆事件都沒有的時候，事件那一節照樣在並且說出來", async () => {
    getLoreEntry.mockResolvedValue(detail({ events: [] }));
    renderCard();
    await open();

    expect(screen.getByText(zh.lore.eventsEmpty)).toBeTruthy();
    expect(screen.queryByTestId("lore-events")).toBeNull();
  });

  it("🔴 事件的人／地／物空著時標成「沒有記下」，不是被填成「未知」", async () => {
    getLoreEntry.mockResolvedValue(
      detail({
        events: [
          {
            happenedTs: 1788330000,
            what: "Kyle 把 trial 站的 DB 複製到分站",
            actor: "agent:Kyle",
            place: "",
            object: "",
          },
        ],
      }),
    );
    renderCard();
    await open();

    const rows = screen.getAllByTestId("lore-event");
    expect(rows.length).toBe(1);
    const row = rows[0];
    expect(row.textContent).toContain("Kyle 把 trial 站的 DB 複製到分站");
    // 人有,原樣印出來。
    expect(row.textContent).toContain("agent:Kyle");
    // 地／物沒有 ⇒ 兩格都在,而且都明說沒有記下。
    expect(row.textContent).toContain(zh.lore.eventPlace);
    expect(row.textContent).toContain(zh.lore.eventObject);
    const blanks = row.textContent?.split(zh.lore.eventBlank).length ?? 0;
    expect(blanks - 1).toBe(2);
    // 🔴 這一行是這支測試的重點:一個字都不准是「未知」。
    expect(row.textContent).not.toContain("未知");
  });

  it("事件照伺服器給的順序印 —— 那是事情發生的順序，不是寫下的順序", async () => {
    getLoreEntry.mockResolvedValue(
      detail({
        events: [
          { happenedTs: 100, what: "先發生的", actor: "", place: "", object: "" },
          { happenedTs: 200, what: "後發生的", actor: "", place: "", object: "" },
        ],
      }),
    );
    renderCard();
    await open();

    const rows = screen.getAllByTestId("lore-event");
    expect(rows.map((r) => r.querySelector(".lore-event__what")?.textContent)).toEqual([
      "先發生的",
      "後發生的",
    ]);
  });

  // 🔴 這支以前叫「摺起來那一列的標題是第 1 格，摘要是第 2 格」，而那是 v7 的
  // 行為。v8 把標題拉出來成獨立的一格，owner 2026-09-05 逐字定了它的職責：
  // 「title 應該就是 agent 透過 target 會看到的列表 因為這會決定他們要不要看內容」。
  // ⇒ 摺起來那一列印的是**標題**，內容要點開才讀得到。
  it("摺起來那一列印的是標題，不是內容 —— 內容要點開才拿得到", async () => {
    renderCard();
    expect(screen.getByText("整套測試綠燈，而它跑過的分母是零")).toBeTruthy();
    // 🔴 陰性對照，而它是這支測試的重點：內容**不可以**出現在摺起來的那一列。
    // 少了這一句，一個把標題與內容都印出來的元件也會通過 —— 而那正是這一層要
    // 省掉的東西（清單那一層倒出整段內容，就是這張票在治的病）。
    expect(
      screen.queryByText("綠燈只證明它看得到的那些東西沒問題。"),
    ).toBeNull();
    // 第 1 格在副標的位置：它說的是這條**為什麼在這份清單裡**。
    expect(
      screen.getByText("整套測試回 PASS，而我正要拿這個結果去說這一包沒問題。"),
    ).toBeTruthy();
    expect(screen.queryByText("證偽條件與實例都空")).toBeNull();
  });
});
