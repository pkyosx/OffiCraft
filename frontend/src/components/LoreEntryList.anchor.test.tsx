// 定位 (#lore/entry/<id>) 的三種結局 — T-33。
//
// 負責人 2026-09-06 推翻了「傳承頁不做子路徑」那個決定(卡 rc-94d925ac79dc),
// 而三種結局的話術照搬他 2026-09-05 給任務頁的裁定(卡 rc-428906235337):
//
//   抓到    ⇒ 顯示那一條(而且不管它在不在清單的那 100 筆裡、也不管它退不退役)
//   404     ⇒ 錨點留著,畫面明說「查不到這一條」,出口是「清除定位」
//   其他失敗 ⇒ 顯示載入錯誤,並且**壓住**空狀態 ——「沒問出口的問題不得給答案」
//
// 🔴 這一支存在的理由,是這三種在畫面上很容易長成同一個樣子:三個都是「清單裡
// 沒有那一條」。真正把它們分開的是**為什麼**,而使用者接下來該做的事完全不同。

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import type { LoreEntrySummaryView, LoreSearchView } from "../types";
import { LoreEntryList } from "./LoreEntryList";

function entry(over: Partial<LoreEntrySummaryView> = {}): LoreEntrySummaryView {
  return {
    entryId: "lore-1",
    heading: "整套測試綠燈，而它跑過的分母是零",
    subjects: ["repo:officraft"],
    ...over,
  };
}

function searchView(entries: LoreEntrySummaryView[]): LoreSearchView {
  return {
    entries,
    total: entries.length,
    truncated: false,
    subjectResolved: true,
    unresolvedSubject: "",
    applied: {
      subject: "",
      query: "",
      queryMatch: "literal-substring",
      limit: 100,
    },
  };
}

const searchLore = vi.fn();
const getLoreEntry = vi.fn();

vi.mock("../api", () => ({
  api: {
    searchLore: (...args: unknown[]) => searchLore(...args),
    getLoreEntry: (...args: unknown[]) => getLoreEntry(...args),
    getLoreRevision: vi.fn(),
  },
}));

/** A 傳承 entry detail, as `getLoreEntry` answers it. Only the four cells the
 * list row is built from matter here. */
function detail(over: Record<string, unknown> = {}) {
  return {
    entryId: "lore-far-away",
    heading: "掉在一百筆之外的那一條",
    reviewed: false,
    content: "",
    events: [],
    subjects: ["repo:officraft"],
    status: "active",
    original: "",
    sha256: "",
    revisions: [],
    ...over,
  };
}

function renderAt(hash: string) {
  window.location.hash = hash;
  return render(
    <I18nProvider>
      <LoreEntryList />
    </I18nProvider>,
  );
}

beforeEach(() => {
  searchLore.mockReset();
  getLoreEntry.mockReset();
  vi.spyOn(console, "warn").mockImplementation(() => {});
});
afterEach(() => {
  window.location.hash = "";
  vi.restoreAllMocks();
});

describe("#lore/entry/<id> 定位", () => {
  // 🔴 這一支就是「錨點自己補抓 + merge」買到的東西。清單那 100 筆裡沒有它,
  // 沒有補抓的話畫面會說「查不到」—— 而那是一句假話:它好端端在資料庫裡。
  it("清單裡沒有的條目，補抓回來之後照樣顯示（100 筆上限之外）", async () => {
    searchLore.mockResolvedValue(searchView([entry({ entryId: "in-list" })]));
    getLoreEntry.mockResolvedValue(detail());

    renderAt("#lore/entry/lore-far-away");

    await waitFor(() => {
      expect(screen.getByTestId("lore-anchored-entry")).toBeTruthy();
    });
    expect(screen.getByText("掉在一百筆之外的那一條")).toBeTruthy();
    expect(getLoreEntry).toHaveBeenCalledWith("lore-far-away");
    // 錨點窄化成那一條 ⇒ 清單裡原本那一筆不該還在畫面上。
    expect(screen.queryByText(entry().heading)).toBeNull();
  });

  // 退役的條目,搜尋 DAL 用 `status <> 'retired'` 濾掉了,所以清單永遠沒有它。
  // 但 GetLoreEntry 沒有 status 濾器 ——「退役」的意思是不再被檢索,不是讀不到。
  it("已退役的條目一樣定位得到（清單濾掉它，補抓拿得到）", async () => {
    searchLore.mockResolvedValue(searchView([entry({ entryId: "in-list" })]));
    getLoreEntry.mockResolvedValue(
      detail({
        entryId: "lore-retired",
        heading: "退役了但當時真的被讀過",
        status: "retired",
      }),
    );

    renderAt("#lore/entry/lore-retired");

    await waitFor(() => {
      expect(screen.getByText("退役了但當時真的被讀過")).toBeTruthy();
    });
    expect(screen.getByTestId("lore-anchored-entry")).toBeTruthy();
  });

  // 結局 ②:404。錨點留著、明說查不到、給出口。
  it("404 ⇒ 明說查不到那一條，並且給「清除定位」出口", async () => {
    searchLore.mockResolvedValue(searchView([entry({ entryId: "in-list" })]));
    getLoreEntry.mockRejectedValue(new Error("http 404 lore entry not found"));

    renderAt("#lore/entry/lore-nope");

    await waitFor(() => {
      expect(screen.getByTestId("lore-anchor-missing")).toBeTruthy();
    });
    expect(screen.getByTestId("lore-anchor-missing").textContent).toContain(
      "lore-nope",
    );
    expect(screen.getByTestId("lore-clear-anchor")).toBeTruthy();
    // 🔴 錨點不可以自己把 hash 拿掉 —— 靜默 self-heal 會讓 404 一閃而過,
    // 只留下一份正常清單,而讀的人會以為自己看到了全部。
    expect(window.location.hash).toBe("#lore/entry/lore-nope");
    // 而且不可以顯示載入錯誤:404 是一個答案,不是「載不動」。
    expect(screen.queryByTestId("lore-list-error")).toBeNull();
  });

  // 結局 ③:其他失敗。顯示載入錯誤,並且**壓住**出口按鈕。
  it("500 ⇒ 顯示載入錯誤，而且不給「清除定位」（沒問出口的問題不得給答案）", async () => {
    searchLore.mockResolvedValue(searchView([entry({ entryId: "in-list" })]));
    getLoreEntry.mockRejectedValue(new Error("http 500 boom"));

    renderAt("#lore/entry/lore-maybe");

    await waitFor(() => {
      expect(screen.getByTestId("lore-list-error")).toBeTruthy();
    });
    // 🔴 THE WHOLE POINT: 500 跟 404 在畫面上必須是兩句話。
    expect(screen.queryByTestId("lore-anchor-missing")).toBeNull();
    expect(screen.queryByTestId("lore-clear-anchor")).toBeNull();
    expect(screen.getByTestId("lore-list-error").textContent).toContain(
      zh.lore.anchorLoadFailed,
    );
  });

  // 一條掛在 N 個對象底下就會在 N 個群裡各出現一次(那是刻意的)。只展開第一群
  // 等於替使用者挑了一個他沒挑的對象 —— 負責人指定「每一群都展開」。
  it("含這一條的每一群都展開，不是只有第一群", async () => {
    searchLore.mockResolvedValue(searchView([]));
    getLoreEntry.mockResolvedValue(
      detail({
        entryId: "lore-multi",
        heading: "掛在兩個對象底下的那一條",
        subjects: ["repo:officraft", "tool:sqlite"],
      }),
    );

    renderAt("#lore/entry/lore-multi");

    await waitFor(() => {
      expect(screen.getAllByTestId("lore-anchored-entry").length).toBe(2);
    });
    // 兩個群的標題都在(getAllByText:對象鍵在卡片內也各印一次,所以不只一個
    // 節點命中——這裡要的是「群存在」,不是「只出現一次」)。
    expect(screen.getAllByText("repo:officraft").length).toBeGreaterThan(0);
    expect(screen.getAllByText("tool:sqlite").length).toBeGreaterThan(0);
    // 兩個群的標題按鈕都是展開態。收合的群標題會是 aria-expanded="false",
    // 所以這裡數的是「展開的群有幾個」。
    const groupHeads = screen
      .getAllByRole("button")
      .filter((b) => b.className.includes("lore-list__group-title"));
    expect(groupHeads.length).toBe(2);
    for (const g of groupHeads) {
      expect(g.getAttribute("aria-expanded")).toBe("true");
    }
  });

  // 沒有錨點時,一切照舊 —— 這一支是上面四支的陰性對照:如果錨點那段程式在
  // 沒有 hash 的情況下也在跑,補抓會被呼叫,而這裡會抓到。
  it("沒有錨點時完全不補抓", async () => {
    searchLore.mockResolvedValue(searchView([entry()]));
    renderAt("#lore");
    // 沒有錨點時群組維持預設收合(那是這一頁本來的行為),所以看得到的是群標題,
    // 不是條目標題 —— 這正好也是「錨點沒有偷偷把所有群攤開」的反面證據。
    await waitFor(() => {
      expect(screen.getByText("repo:officraft")).toBeTruthy();
    });
    expect(getLoreEntry).not.toHaveBeenCalled();
    expect(screen.queryByTestId("lore-anchor-missing")).toBeNull();
    expect(screen.queryByTestId("lore-anchored-entry")).toBeNull();
  });
});
