// 傳承活動卡 (T-33) — 四種「什麼都沒有」必須是四句話。
//
// 🔴 這一支存在的唯一理由,是那四種在畫面上天生長得一樣:載入中、載入失敗、
// 沒有在跑、跑著但沒讀。其中只有最後一種是在講這個 agent 做了什麼;把任何兩種
// 併成同一句,就會在畫面上生出一句關於他的假話 —— 最糟的是對一個已下線的 agent
// 說「這一任沒有取用紀錄」,那是在怪他睡著的時候沒讀書。

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import type { LoreActivityView, LoreActivityRowView } from "../types";
import { LoreActivityCard } from "./LoreActivityCard";

const getMemberLoreActivity = vi.fn();

vi.mock("../api", () => ({
  api: {
    getMemberLoreActivity: (...a: unknown[]) => getMemberLoreActivity(...a),
  },
}));

function row(over: Partial<LoreActivityRowView> = {}): LoreActivityRowView {
  return {
    createdTs: 1_000_600,
    sinceBootSecs: 600,
    door: "entry-read",
    entryId: "lore-1",
    heading: "整套測試綠燈，而它跑過的分母是零",
    headingFound: true,
    status: "active",
    ...over,
  };
}

function view(over: Partial<LoreActivityView> = {}): LoreActivityView {
  return {
    memberId: "m-1",
    sessionActive: true,
    sessionBootTs: 1_000_000,
    rows: [row()],
    ...over,
  };
}

function renderCard(loreEnabled = true) {
  return render(
    <I18nProvider>
      <LoreActivityCard agentId="m-1" loreEnabled={loreEnabled} />
    </I18nProvider>,
  );
}

/** Open the card — nothing is fetched until the first expand, by contract. */
function expand() {
  fireEvent.click(screen.getByTestId("mp-lore-toggle"));
}

beforeEach(() => {
  getMemberLoreActivity.mockReset();
  window.location.hash = "";
});
afterEach(() => {
  window.location.hash = "";
});

describe("LoreActivityCard", () => {
  it("面板載入時不打 API —— 只有第一次展開才讀", async () => {
    getMemberLoreActivity.mockResolvedValue(view());
    renderCard();
    expect(getMemberLoreActivity).not.toHaveBeenCalled();
    expand();
    await waitFor(() => expect(getMemberLoreActivity).toHaveBeenCalledTimes(1));
  });

  // ── 四種空,四句話 ────────────────────────────────────────────────────────

  it("① 載入失敗 ⇒ 錯誤訊息 + 重試，絕不是空清單", async () => {
    getMemberLoreActivity.mockRejectedValue(new Error("http 500"));
    renderCard();
    expand();
    await waitFor(() => expect(screen.getByTestId("mp-lore-error")).toBeTruthy());
    // 🔴 「載入失敗」跟「沒有取用紀錄」是相反的斷言:讀失敗時我們對這個成員
    // 讀了什麼一無所知,而空清單是一句關於他的話。
    expect(screen.queryByTestId("mp-lore-empty")).toBeNull();
    expect(screen.queryByTestId("mp-lore-nosession")).toBeNull();
    expect(screen.getByTestId("mp-lore-retry")).toBeTruthy();
  });

  it("② 沒有在跑 ⇒ 「目前沒有在跑」，不是「沒有取用紀錄」", async () => {
    getMemberLoreActivity.mockResolvedValue(
      view({ sessionActive: false, sessionBootTs: 0, rows: [] }),
    );
    renderCard();
    expand();
    await waitFor(() =>
      expect(screen.getByTestId("mp-lore-nosession")).toBeTruthy(),
    );
    expect(screen.getByTestId("mp-lore-nosession").textContent).toBe(
      zh.mp.loreActivity.noSession,
    );
    // 🔴 這一格就是本卡最容易犯的錯:兩個都是 rows:[]。
    expect(screen.queryByTestId("mp-lore-empty")).toBeNull();
  });

  it("③ 跑著但沒讀 ⇒ 「這一任還沒有取用任何條目」，跟 ② 不同句", async () => {
    getMemberLoreActivity.mockResolvedValue(
      view({ sessionActive: true, rows: [] }),
    );
    renderCard();
    expand();
    await waitFor(() => expect(screen.getByTestId("mp-lore-empty")).toBeTruthy());
    expect(screen.getByTestId("mp-lore-empty").textContent).toBe(
      zh.mp.loreActivity.empty,
    );
    expect(screen.queryByTestId("mp-lore-nosession")).toBeNull();
    // 🔴 兩句話真的不一樣 —— 不是兩個 key 指向同一串字。
    expect(zh.mp.loreActivity.empty).not.toBe(zh.mp.loreActivity.noSession);
  });

  // ── 行的內容 ──────────────────────────────────────────────────────────────

  it("④ 有紀錄 ⇒ 「上線後 N 分鐘 - Load Lore: <heading>」，標題可點連到該條目", async () => {
    getMemberLoreActivity.mockResolvedValue(view());
    renderCard();
    expand();
    await waitFor(() => expect(screen.getByTestId("mp-lore-row")).toBeTruthy());

    const line = screen.getByTestId("mp-lore-row").textContent ?? "";
    // 負責人指定的格式骨架,逐字。
    expect(line).toContain("上線後 10 分鐘");
    expect(line).toContain("Load Lore:");
    expect(line).toContain("整套測試綠燈，而它跑過的分母是零");

    fireEvent.click(screen.getByTestId("mp-lore-heading-link"));
    expect(window.location.hash).toBe("#lore/entry/lore-1");
  });

  it("不到一分鐘用秒 —— 45 秒不可以被寫成「0 分鐘」", async () => {
    getMemberLoreActivity.mockResolvedValue(
      view({ rows: [row({ sinceBootSecs: 45 })] }),
    );
    renderCard();
    expand();
    await waitFor(() => expect(screen.getByTestId("mp-lore-when")).toBeTruthy());
    expect(screen.getByTestId("mp-lore-when").textContent).toBe("上線後 45 秒");
  });

  it("同一條在同一任被讀兩次 ⇒ 兩行，不會被去重", async () => {
    getMemberLoreActivity.mockResolvedValue(
      view({
        rows: [
          row({ sinceBootSecs: 60, door: "search" }),
          row({ sinceBootSecs: 600, door: "entry-read" }),
        ],
      }),
    );
    renderCard();
    expand();
    // 🔴 重複讀是這本日誌被建出來要量的訊號(migrations/00082),不是雜訊。
    await waitFor(() =>
      expect(screen.getAllByTestId("mp-lore-row").length).toBe(2),
    );
  });

  it("退役的條目 ⇒ 標上「已退役」，但照樣可點（退役不是查不到）", async () => {
    getMemberLoreActivity.mockResolvedValue(
      view({ rows: [row({ status: "retired" })] }),
    );
    renderCard();
    expand();
    await waitFor(() => expect(screen.getByTestId("mp-lore-retired")).toBeTruthy());
    // 退役是顯示,不是錯誤:連結照給,因為單條讀取讀得到它。
    expect(screen.getByTestId("mp-lore-heading-link")).toBeTruthy();
  });

  it("查不到的條目 ⇒ 保留該行、說「查不到這個 id」、不可點，而且不寫「被刪掉」", async () => {
    getMemberLoreActivity.mockResolvedValue(
      view({
        rows: [
          row({ entryId: "lore-gone", heading: "", headingFound: false, status: "" }),
        ],
      }),
    );
    renderCard();
    expand();
    await waitFor(() =>
      expect(screen.getByTestId("mp-lore-heading-plain")).toBeTruthy(),
    );
    const text = screen.getByTestId("mp-lore-heading-plain").textContent ?? "";
    expect(text).toContain("lore-gone");
    // 🔴 2026-09-06 實測:全樹沒有 DELETE FROM lore_entry,退役也不是刪除。
    // 寫「被刪掉」會讓讀的人去找一個不存在的機制。
    expect(text).not.toContain("刪");
    // 🔴 而且那一行不可以被丟掉 —— 丟掉等於把「讀過但現在查不到」偽裝成「沒讀過」。
    expect(screen.getByTestId("mp-lore-row")).toBeTruthy();
    expect(screen.queryByTestId("mp-lore-heading-link")).toBeNull();
  });

  it("傳承開關關著 ⇒ 明說功能關閉，標題不做成連結（歷史照樣看得到）", async () => {
    getMemberLoreActivity.mockResolvedValue(view());
    renderCard(false);
    expand();
    await waitFor(() =>
      expect(screen.getByTestId("mp-lore-switchoff")).toBeTruthy(),
    );
    // 歷史還在:日誌記的是功能開著時發生的事,關掉開關不會讓它沒發生過。
    expect(screen.getByTestId("mp-lore-row")).toBeTruthy();
    // 🔴 但連結不給 —— 開關關著時 App 會把 #lore 導回辦公室頁,一個到站什麼都
    // 不會說的連結,最好的處理是不要交出去。
    expect(screen.queryByTestId("mp-lore-heading-link")).toBeNull();
    expect(screen.getByTestId("mp-lore-heading-plain")).toBeTruthy();
  });
});
