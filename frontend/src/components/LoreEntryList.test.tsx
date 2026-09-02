// 記憶清單 (T-33)。
//
// 這裡鎖的是 owner 2026-09-02 那幾句話變成的規則:清單一進來就看得到、對象是
// 導覽(預設收合)、品質訊號比總條數醒目、篩選框只在清單長的時候才出現。
// 「先想關鍵字再按搜尋」那一版被他否掉了(「殺雞用牛刀」「我無法一眼看出有哪些
// 對象」),所以這裡也鎖住:畫面上不會再有送出型的搜尋。

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import type { LoreEntrySummaryView, LoreSearchView } from "../types";
import { LoreEntryList } from "./LoreEntryList";

function entry(over: Partial<LoreEntrySummaryView> = {}): LoreEntrySummaryView {
  return {
    entryId: "lore-1",
    label: "綠燈的射程等於它看得到的範圍",
    short: "綠燈只證明它看得到的那些東西沒問題。",
    symptoms: "整套測試回 PASS，而我正要拿這個結果去說這一包沒問題。",
    subjects: ["repo:officraft"],
    actions: [],
    origin: "agent:Kyle",
    degraded: false,
    tier: "T1",
    tierNote: "",
    trustScope: "method",
    trustFellBack: false,
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
      actions: [],
      query: "",
      queryMatch: "literal-substring",
      limit: 100,
      tieredBy: [],
    },
    unmappedActions: [],
  };
}

const searchLore = vi.fn();

vi.mock("../api", () => ({
  api: {
    searchLore: (...args: unknown[]) => searchLore(...args),
    getLoreEntry: vi.fn(),
    getLoreRevision: vi.fn(),
  },
}));

function renderList() {
  return render(
    <I18nProvider>
      <LoreEntryList />
    </I18nProvider>,
  );
}

beforeEach(() => {
  searchLore.mockReset();
});

describe("LoreEntryList", () => {
  it("一進來就撈全部並照對象分群，群預設收合", async () => {
    searchLore.mockResolvedValue(
      searchView([
        entry({ entryId: "a", subjects: ["repo:officraft"] }),
        entry({
          entryId: "b",
          label: "複製資料庫等於複製設定",
          subjects: ["tool:sqlite"],
        }),
      ]),
    );
    renderList();

    // 沒有人按任何東西 —— 條件是空的,拿的就是全部。
    await waitFor(() => expect(searchLore).toHaveBeenCalledTimes(1));
    expect(searchLore.mock.calls[0][0]).toEqual({ limit: 100 });

    // 對象自己就是導覽:兩個群名都在,而條目還沒展開。
    expect(await screen.findByText("repo:officraft")).toBeTruthy();
    expect(screen.getByText("tool:sqlite")).toBeTruthy();
    expect(screen.queryByText("複製資料庫等於複製設定")).toBeNull();

    fireEvent.click(screen.getByText("tool:sqlite"));
    expect(screen.getByText("複製資料庫等於複製設定")).toBeTruthy();
  });

  it("沒有送出型的搜尋 —— 條數少的時候連篩選框都不長出來", async () => {
    searchLore.mockResolvedValue(searchView([entry()]));
    renderList();

    await screen.findByText("repo:officraft");
    expect(screen.queryByRole("button", { name: "搜尋" })).toBeNull();
    expect(
      screen.queryByPlaceholderText(zh.lore.listFilterPlaceholder),
    ).toBeNull();
  });

  it("條數多的時候長出即時篩選框，打字就篩、不用送出", async () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      entry({
        entryId: `e${i}`,
        label: i === 3 ? "複製資料庫等於複製設定" : `條目 ${i}`,
        subjects: [i === 3 ? "tool:sqlite" : "repo:officraft"],
      }),
    );
    searchLore.mockResolvedValue(searchView(many));
    renderList();

    const box = await screen.findByPlaceholderText(
      zh.lore.listFilterPlaceholder,
    );
    fireEvent.change(box, { target: { value: "複製資料庫" } });

    // 篩過之後只剩那一群、而且直接攤開 —— 篩完還要自己點開等於沒篩。
    expect(screen.getByText("複製資料庫等於複製設定")).toBeTruthy();
    expect(screen.queryByText("條目 0")).toBeNull();
    // 篩選是前端做的:沒有為了篩而再打一次伺服器。
    expect(searchLore).toHaveBeenCalledTimes(1);
  });

  it("品質訊號比總條數醒目，而且可以只看那些條目", async () => {
    searchLore.mockResolvedValue(
      searchView([
        entry({ entryId: "ok" }),
        entry({ entryId: "bad", label: "沒有實例的口號", degraded: true }),
      ]),
    );
    renderList();

    const quality = await screen.findByTestId("lore-quality");
    expect(quality.textContent).toContain(zh.lore.qualityFlagged(1));

    fireEvent.click(screen.getByText(zh.lore.qualityShowOnly));
    expect(screen.getByText("沒有實例的口號")).toBeTruthy();
    expect(screen.queryByText("綠燈的射程等於它看得到的範圍")).toBeNull();
  });

  it("讀不到清單時說讀不到，不畫成一份空清單", async () => {
    searchLore.mockRejectedValue(new Error("boom"));
    renderList();

    // 「站上沒有記憶」跟「我沒讀到」不能長得一樣。
    expect(await screen.findByText(/讀不到記憶清單/)).toBeTruthy();
    expect(screen.queryByText(zh.lore.listEmpty)).toBeNull();
  });
});
