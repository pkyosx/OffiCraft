// 待審對象一列上「看得到什麼」(T-33 round 3)。
//
// owner 2026-09-04 逐字:「為什麼核可的可見內容這麼少 我根本無從審核起」。他要
// 做的判斷是**「這個被自動鑄出來的名字,是真的新對象,還是既有對象的錯字」**,
// 而舊的那一列答不了。這裡鎖的就是新增的那三件,每一件都用**兩列的對照**鎖:
//
//   ① 「底下 0 條」的兩種成因必須說不同的話。一列從來沒被用過(打錯字的形狀),
//      一列曾經有兩條、都退役了(跟名字對不對無關)—— 舊畫面兩列都印「底下還沒
//      有記憶」。
//   ② 誰鑄出這個名字要印在列上;沒有記錄要**照實說沒有記錄**,不是留一片空白
//      讓人以為系統沒查。
//   ③ 底下不只一條的時候,每一條都要看得到,不是只有第一條的前 120 字。
//
// 🔴 這三件都是**多給資訊**,不是多給出口。最後一個 it 就是在鎖這個。
// ⚠️ 它原本還鎖「建議還是伺服器算的」;owner 2026-09-05 把整組 `suggestion` /
// `mergeTarget` 裁掉了(改成 AI 判一輪、人可回 comment 重判,另一張票),所以那
// 半句連同斷言一起拿掉。剩下的那半句沒有變弱:一列**沒有相似候選**的時候仍然
// 只有「核可」一顆鈕,「駁回」還是不存在。

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import type { LorePendingEntityView } from "../types";
import { LorePendingSection } from "./LorePendingSection";

function row(over: Partial<LorePendingEntityView> = {}): LorePendingEntityView {
  return {
    entityId: "en-1",
    canonical: "repo:offcraft",
    type: "repo",
    name: "offcraft",
    createdTs: 1788330000,
    createdBy: "m-o197",
    entries: 0,
    entriesEver: 0,
    entryRefs: [],
    similar: [],
    sampleShort: "",
    ...over,
  };
}

const listPendingLoreEntities = vi.fn();

vi.mock("../api", () => ({
  api: {
    listPendingLoreEntities: () => listPendingLoreEntities(),
    approveLoreEntity: vi.fn(),
    mergeLoreEntity: vi.fn(),
  },
}));

function renderSection() {
  return render(
    <I18nProvider>
      <LorePendingSection />
    </I18nProvider>,
  );
}

beforeEach(() => {
  listPendingLoreEntities.mockReset();
});

describe("LorePendingSection — 一列上看得到的東西", () => {
  it("「底下 0 條」的兩種成因說的是兩句不同的話", async () => {
    listPendingLoreEntities.mockResolvedValue([
      row({ entityId: "en-never", canonical: "repo:offcraft" }),
      row({
        entityId: "en-emptied",
        canonical: "human:Mira",
        entries: 0,
        entriesEver: 2,
      }),
    ]);
    renderSection();

    const notes = await screen.findAllByTestId("lore-pending-entries");
    expect(notes).toHaveLength(2);
    // 從來沒被用過 ⇒ 這是打錯字的形狀,而且畫面要說得出來。
    expect(notes[0].textContent).toBe(zh.lore.pendingNeverUsed);
    // 曾經有、都退役了 ⇒ 完全不同的處置,所以完全不同的一句話。
    expect(notes[1].textContent).toBe(zh.lore.pendingAllRetired(2));
    // 🔴 這一條才是真正的守衛:兩列**不可以**長得一樣。
    expect(notes[0].textContent).not.toBe(notes[1].textContent);
  });

  it("底下還有退役的條目時,主數字不動而退役的另外說", async () => {
    listPendingLoreEntities.mockResolvedValue([
      row({ entries: 3, entriesEver: 5, entryRefs: [] }),
    ]);
    renderSection();

    const note = await screen.findByTestId("lore-pending-entries");
    // 主數字要跟核可後真的服務得到的量對得起來 ⇒ 3,不是 5。
    expect(note.textContent).toContain(zh.lore.pendingEntries(3));
    // 但躺在底下的 2 條退役條目也不能藏起來。
    expect(note.textContent).toContain(zh.lore.pendingAlsoRetired(2));
  });

  it("印出是誰鑄出這個名字;沒有記錄就照實說沒有記錄", async () => {
    listPendingLoreEntities.mockResolvedValue([
      row({ entityId: "en-known", createdBy: "m-o197" }),
      row({ entityId: "en-unknown", canonical: "repo:old", createdBy: "" }),
    ]);
    renderSection();

    const minters = await screen.findAllByTestId("lore-pending-minter");
    expect(minters[0].textContent).toBe(zh.lore.pendingMintedBy("m-o197"));
    // 空白會讓人以為系統沒查 —— 它查了,答案是沒有記錄。
    expect(minters[1].textContent).toBe(zh.lore.pendingMintedByUnknown);
  });

  it("底下每一條都列得出來,不是只有第一條的前 120 字", async () => {
    listPendingLoreEntities.mockResolvedValue([
      row({
        entries: 3,
        entriesEver: 3,
        sampleShort: "只有第一條的前 120 字",
        entryRefs: [
          { entryId: "le-1", trigger: "我要跑整套測試", status: "active" },
          { entryId: "le-2", trigger: "我要相信一個綠燈", status: "superseded" },
          {
            entryId: "le-3",
            trigger: "我要加一個 migration",
            status: "underspecified",
          },
        ],
      }),
    ]);
    renderSection();

    const entries = await screen.findAllByTestId("lore-pending-entry");
    expect(entries).toHaveLength(3);
    // 每一條的第 1 格(兼標題)跟它的 id 都要在,不然「看得到」等於看不到。
    expect(entries[0].textContent).toContain("我要跑整套測試");
    expect(entries[0].textContent).toContain("le-1");
    expect(entries[2].textContent).toContain("我要加一個 migration");
    // 狀態不是 active 的要看得出來 —— 三條全 superseded 跟三條全 active 是兩
    // 個完全不同的名字。
    expect(entries[1].textContent).toContain(
      zh.lore.pendingEntryStatusSuperseded,
    );
    expect(entries[2].textContent).toContain(
      zh.lore.pendingEntryStatusUnderspecified,
    );
    // 🔴 active 不掛標 —— 每一條都掛「正常」等於沒有訊號。
    expect(entries[0].textContent).not.toContain(
      zh.lore.pendingEntryStatusSuperseded,
    );
  });

  it("多看到的資訊沒有變成多出來的出口", async () => {
    listPendingLoreEntities.mockResolvedValue([
      row({
        entries: 2,
        entriesEver: 2,
        entryRefs: [
          { entryId: "le-1", trigger: "我要跑整套測試", status: "active" },
          { entryId: "le-2", trigger: "我要相信一個綠燈", status: "active" },
        ],
      }),
    ]);
    renderSection();

    await waitFor(() => screen.getByTestId("lore-pending-row"));
    // 只有「核可」一顆:這一列 `similar` 是空的 ⇒ 沒有可以併進去的候選 ⇒ 沒有
    // 合併鈕,而「駁回」從來沒有存在過,列出底下有幾條也不會讓它長出來。
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByText(zh.lore.pendingApprove)).toBeTruthy();
  });
});
