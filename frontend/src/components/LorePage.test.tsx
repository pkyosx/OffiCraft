// 傳承 Lore 分頁 (T-33)。
//
// 這裡鎖的是這張票唯一的價值:站上只有六條 lore route,而設計稿上一半的區塊需要
// 不存在的路。那些區塊必須畫成「尚無資料來源」並寫明缺哪一條 —— 而且一個數字
// 都不能有。一個沒有生產者的 0 讀起來就是「我們查過,沒有」。

import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import type { LoreSearchView } from "../types";
import { LorePage } from "./LorePage";

const ENTRY = {
  entryId: "lore-31274bbb892c",
  label: "綠燈的射程等於它看得到的範圍",
  short: "綠燈只證明它看得到的那些東西沒問題。",
  symptoms: "整套測試回 PASS，而我正要拿這個結果去說這一包沒問題。",
  subjects: ["repo:officraft"],
  actions: [],
  origin: "agent:O-197",
  degraded: false,
  tier: "T1",
  tierNote: "",
  trustScope: "method",
  trustFellBack: false,
};

function searchView(over: Partial<LoreSearchView> = {}): LoreSearchView {
  return {
    entries: [ENTRY],
    total: 1,
    truncated: false,
    subjectResolved: true,
    unresolvedSubject: "",
    applied: {
      subject: "",
      actions: [],
      query: "",
      queryMatch: "literal-substring",
      limit: 20,
      tieredBy: [],
    },
    unmappedActions: [],
    ...over,
  };
}

const searchLore = vi.fn();
const getLoreEntry = vi.fn();
const getLoreRevision = vi.fn();

vi.mock("../api", () => ({
  api: {
    searchLore: (...args: unknown[]) => searchLore(...args),
    getLoreEntry: (...args: unknown[]) => getLoreEntry(...args),
    getLoreRevision: (...args: unknown[]) => getLoreRevision(...args),
  },
}));

function renderPage() {
  return render(
    <I18nProvider>
      <LorePage />
    </I18nProvider>,
  );
}

/** 每一個「尚無資料來源」區塊。 */
function noSourceBlocks(): HTMLElement[] {
  return screen.queryAllByTestId("lore-no-source");
}

describe("LorePage", () => {
  it("對象頁把搜尋結果一條一條畫出來，並印出伺服器實際套用的條件", async () => {
    searchLore.mockResolvedValue(searchView());
    renderPage();
    fireEvent.click(screen.getByText(zh.lore.tabSubjects));

    expect(await screen.findByText(ENTRY.label)).toBeTruthy();
    expect(screen.getAllByTestId("lore-entry")).toHaveLength(1);
    // 分層跟它的軸必須同框:分層沒有它的軸會被讀成另一個意思。
    expect(screen.getByText(zh.lore.appliedTitle)).toBeTruthy();
    expect(screen.getByText(zh.lore.appliedTieredByEmpty)).toBeTruthy();
  });

  it("尚無資料來源的區塊會出現、寫明缺哪一條路，而且一個數字都不印", async () => {
    searchLore.mockResolvedValue(searchView());
    renderPage();

    // 概覽:對象總數、待審新對象、需要你看一眼、合併圖表都沒有來源。
    await waitFor(() => expect(noSourceBlocks().length).toBeGreaterThan(0));
    for (const tab of [
      zh.lore.tabOverview,
      zh.lore.tabPending,
      zh.lore.tabHealth,
    ]) {
      fireEvent.click(screen.getByText(tab));
      expect(noSourceBlocks().length).toBeGreaterThan(0);
      for (const block of noSourceBlocks()) {
        expect(block.textContent).toContain(zh.lore.noSource);
        // 這是整份測試的重點:這些格子裡不准有數字。
        expect(block.textContent ?? "").not.toMatch(/\d/);
      }
    }
    // 待審頁列得出缺的那幾條路,而且沒有核可/合併/駁回的出口。
    fireEvent.click(screen.getByText(zh.lore.tabPending));
    const pending = noSourceBlocks()
      .map((b) => b.textContent ?? "")
      .join(" ");
    expect(pending).toContain("POST /api/lore/subjects/{id}/approve");
    expect(pending).toContain("POST /api/lore/subjects/{id}/merge");
    expect(screen.getByText(zh.lore.pendingNoRejectNote)).toBeTruthy();
  });

  it("對象鍵指不到任何東西的時候顯示回音，而不是一份空結果", async () => {
    searchLore.mockResolvedValue(
      searchView({
        entries: [],
        total: 0,
        subjectResolved: false,
        unresolvedSubject: "repo:offi-craft",
        applied: {
          subject: "repo:offi-craft",
          actions: [],
          query: "",
          queryMatch: "literal-substring",
          limit: 20,
          tieredBy: [],
        },
      }),
    );
    renderPage();
    fireEvent.click(screen.getByText(zh.lore.tabSubjects));

    const unresolved = await screen.findByTestId("lore-unresolved");
    expect(unresolved.textContent).toContain(zh.lore.unresolvedTitle);
    expect(unresolved.textContent).toContain("repo:offi-craft");
    // 「站上沒有這個對象」不可以長得像「這個對象底下沒有條目」。
    expect(screen.queryByText(zh.lore.resultsEmpty)).toBeNull();
  });

  it("展開一條會讀出原文、六格（空的也印欄位名）與版本時間軸", async () => {
    searchLore.mockResolvedValue(searchView());
    getLoreEntry.mockResolvedValue({
      entryId: ENTRY.entryId,
      label: ENTRY.label,
      symptoms: ENTRY.symptoms,
      short: ENTRY.short,
      falsify: "找到一次量法涵蓋範圍為零、而輸出跟正確答案不一樣的例子。",
      instance: "2026-09-01：-run 的正則打錯字，一顆都沒跑。",
      residualRisk: "",
      subjects: ENTRY.subjects,
      actions: [],
      origin: ENTRY.origin,
      status: "active",
      degraded: false,
      original: "symptoms: 整套測試回 PASS",
      sha256: "",
      supersedes: "",
      writtenBy: "agent:O-197",
      revisions: [
        {
          revisionId: 2,
          createdTs: 0,
          actorId: "agent:O-197",
          sha256: "",
          shrinkChars: 412,
        },
        {
          revisionId: 1,
          createdTs: 0,
          actorId: "agent:O-197",
          sha256: "",
          shrinkChars: 0,
        },
      ],
    });
    getLoreRevision.mockResolvedValue({
      revisionId: 1,
      entryId: ENTRY.entryId,
      body: "第一版的原文",
      sha256: "",
      createdTs: 0,
      actorId: "agent:O-197",
      shrinkChars: 0,
    });

    renderPage();
    fireEvent.click(screen.getByText(zh.lore.tabSubjects));
    fireEvent.click(await screen.findByLabelText(zh.lore.entryOpen));

    expect(await screen.findByText("symptoms: 整套測試回 PASS")).toBeTruthy();
    expect(getLoreEntry).toHaveBeenCalledWith(ENTRY.entryId);
    // 殘餘風險是空的 —— 欄位名照印,值印「空白」,不是整格消失。
    expect(screen.getByText(zh.lore.fieldResidual)).toBeTruthy();
    expect(screen.getByText(zh.lore.fieldEmpty)).toBeTruthy();
    // 被磨短的那一版:條目被磨空的時候條數一條都不會少,這是唯一看得見的地方。
    expect(
      screen.getByText(
        `${zh.lore.revisionShrinkLead}412${zh.lore.revisionShrinkTail}`,
      ),
    ).toBeTruthy();
    expect(screen.getByText(zh.lore.revisionNoShrink)).toBeTruthy();

    fireEvent.click(screen.getAllByText(zh.lore.revisionView)[0]);
    expect(await screen.findByText("第一版的原文")).toBeTruthy();
  });
});
