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
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
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
const mergeLoreEntity = vi.fn();
const approveLoreEntity = vi.fn();

vi.mock("../api", () => ({
  api: {
    listPendingLoreEntities: () => listPendingLoreEntities(),
    approveLoreEntity: (...a: unknown[]) => approveLoreEntity(...a),
    mergeLoreEntity: (...a: unknown[]) => mergeLoreEntity(...a),
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
  mergeLoreEntity.mockReset();
  mergeLoreEntity.mockResolvedValue({});
  approveLoreEntity.mockReset();
  approveLoreEntity.mockResolvedValue({});
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

// ── round 4:合併的單一入口 ────────────────────────────────────────────────
//
// owner 2026-09-05 逐字:「改成單一入口:只留一顆合併鈕,按了列出候選讓你挑,再
// 確認」。上一輪每一個 `similar` 候選旁邊各一顆合併鈕,一按就送出 —— 而 `similar`
// 裡有 `prefix` / `substring` 這種弱匹配,所以那是一排長得一樣、其中幾顆按下去
// 就錯的不可逆按鈕。
//
// 🔴 這一組裡最重的是最後兩個 it:後端的合併是**單向**的(沒有 unmerge 路由),
// 所以「確認畫面明寫無法還原」跟「確認之前一次 API 都不打」不是體驗問題,是這一
// 輪存在的理由。
describe("LorePendingSection — 合併走單一入口", () => {
  // 🔴 五種 reason 全部種進來,不是三種。`reasonText` 有五個 case,fixture 只
  // 蓋三種的話,「每個候選都帶著理由」這句話的射程比測試寬 —— 少掉的那兩種
  // 印成空白也不會有人知道。
  const CANDIDATES = [
    { entityId: "en-a", canonical: "repo:offcraft", reason: "same_normalized" },
    { entityId: "en-b", canonical: "repo:offcraft-cli", reason: "prefix" },
    { entityId: "en-c", canonical: "repo:craft", reason: "substring" },
    { entityId: "en-d", canonical: "repo:offcraf", reason: "edit_distance_1" },
    { entityId: "en-e", canonical: "repo:offcra", reason: "edit_distance_2" },
  ];

  function rowWithCandidates() {
    return row({ entityId: "en-1", canonical: "repo:Offcraft", similar: CANDIDATES });
  }

  async function openPicker() {
    listPendingLoreEntities.mockResolvedValue([rowWithCandidates()]);
    renderSection();
    fireEvent.click(await screen.findByTestId("lore-pending-merge-start"));
  }

  it("一列上只有一顆合併鈕,不是每個候選各一顆", async () => {
    listPendingLoreEntities.mockResolvedValue([rowWithCandidates()]);
    renderSection();

    await waitFor(() => screen.getByTestId("lore-pending-row"));
    // 🔴 三個候選,但合併鈕只有一顆。
    expect(screen.getAllByTestId("lore-pending-merge-start")).toHaveLength(1);
    // 整列的出口還是兩個:核可 + 合併。三個候選不可以變成三個出口。
    expect(screen.getAllByRole("button")).toHaveLength(2);
    // 「像誰」那一排還在 —— 它是證據,只是不再兼任按鈕。
    expect(
      screen.queryByText(zh.lore.pendingMerge("repo:offcraft")),
    ).toBeNull();
  });

  it("按下去才列出候選,而且每個候選都帶著它被判為相似的理由", async () => {
    listPendingLoreEntities.mockResolvedValue([rowWithCandidates()]);
    renderSection();

    await waitFor(() => screen.getByTestId("lore-pending-row"));
    // 按之前沒有清單:單一入口的意思是候選藏在鈕後面。
    expect(screen.queryByTestId("lore-pending-merge-picker")).toBeNull();

    fireEvent.click(screen.getByTestId("lore-pending-merge-start"));

    const picked = screen.getAllByTestId("lore-pending-merge-candidate");
    expect(picked).toHaveLength(5);
    expect(picked[0].textContent).toContain("repo:offcraft");
    // 🔴 理由必須跟候選一起出現。強匹配跟弱匹配長得一樣的話,這一步等於在猜。
    // 五種 reason 一種都不能漏 —— 漏掉的那一種會印成空白,而空白讀起來像
    // 「這個候選沒有理由」,不像「這個畫面不認得這種理由」。
    expect(picked[0].textContent).toContain(zh.lore.reasonSameNormalized);
    expect(picked[1].textContent).toContain(zh.lore.reasonPrefix);
    expect(picked[2].textContent).toContain(zh.lore.reasonSubstring);
    expect(picked[3].textContent).toContain(zh.lore.reasonEditDistance1);
    expect(picked[4].textContent).toContain(zh.lore.reasonEditDistance2);
    // 五個理由不可以印成同一句話,否則「看得出弱匹配」就是假的。
    // 🔴 比對之前要先把候選的名字從文字裡拿掉。`textContent` 是「名字＋理由」
    // 的串接,而五個 canonical 本來就兩兩相異 —— 直接對整段做集合大小,五種
    // 理由全部塌成同一句它照樣通過。這一行原本就是那樣寫的,由 2026-09-05 的
    // 增量審查實測抓到:一條看起來在守、實際上永遠不會紅的斷言。
    const shownReasons = picked.map((p, i) =>
      (p.textContent ?? "").replace(CANDIDATES[i].canonical, ""),
    );
    expect(new Set(shownReasons).size).toBe(5);
  });

  // ⚠️ 這支的名字刻意寫窄。它守住的是**送出鈕停用**,不是「送不出去」——
  // jsdom 對 disabled 的 button 本來就不派發 click,所以「點下去沒有打 API」
  // 在這裡是恆真的,寫了也擋不住任何東西。恆真的斷言比沒有斷言更貴:它會讓
  // 下一個人以為那一格有人守。真正擋住「不挑也能送」的是 `disabled` 那一格,
  // 以及下一支測試裡「挑完之後 disabled 才變 false」的對照。
  it("沒有挑候選的時候,送出鈕是停用的,而且鈕上說得出為什麼停用", async () => {
    await openPicker();

    const next = screen.getByTestId("lore-pending-merge-next");
    expect((next as HTMLButtonElement).disabled).toBe(true);
    // 字面斷言,不是字典比字典:字典比字典只證明這句話沒被改掉,不證明它說了
    // 什麼。這裡要的是鈕上真的叫人去挑一個。
    expect(next.textContent).toContain("挑一個候選");
    // 還沒到確認那一步。
    expect(screen.queryByTestId("lore-pending-merge-confirm")).toBeNull();
  });

  it("挑完之後還有一個確認步驟,而且明寫這個動作無法還原", async () => {
    await openPicker();

    fireEvent.click(screen.getAllByRole("radio")[1]);
    const next = screen.getByTestId("lore-pending-merge-next");
    expect((next as HTMLButtonElement).disabled).toBe(false);
    // 挑完了但還沒確認 ⇒ 一次 API 都還沒打。
    expect(mergeLoreEntity).not.toHaveBeenCalled();

    fireEvent.click(next);

    const body = screen.getByTestId("lore-pending-merge-confirm-body");
    expect(body.textContent).toBe(
      zh.lore.pendingMergeConfirmBody("repo:Offcraft", "repo:offcraft-cli"),
    );
    // 🔴 這一句是這整輪存在的理由:後端沒有 unmerge,按錯救不回來,所以畫面上
    // 必須明寫。鎖字典的字串是不夠的 —— 這裡鎖的是「畫面上真的讀得到」。
    expect(body.textContent).toContain("無法還原");
    // 🔴 這一句在 2026-09-05 的第三輪審查裡被抓到是**假的**:它原本寫「這個名
    // 字會就此消失」,而 `MergeLoreEntity` 的交易做的是把來源的 canonical 寫成
    // 存活者的 alias、並把 lore_subject 也掛一份給存活者 —— 名字沒有消失,是
    // 降級成別名。假話的方向很具體:相信名字會消失的人不敢按合併,會改去按核
    // 可,而核可才是把重複名字送進開機目錄、且之後再也 merge 不動的那個動作。
    expect(body.textContent).toContain("別名");
    // 釘整句,不釘「消失」兩個字:那是常用詞,正文哪天正當地用到它就會誤傷。
    expect(body.textContent).not.toContain("名字會就此消失");
    // 🔴 英文那句也要有字面斷言。只鎖中文的話,刪掉 `en.ts` 裡的
    // "This cannot be undone" 是一個 token 的編輯,而 vitest / tsc / drift gate
    // 全綠、零訊號 —— 全樹沒有第二個東西碰這個畫面。
    expect(en.lore.pendingMergeConfirmBody("a", "b")).toContain(
      "This cannot be undone",
    );
    expect(en.lore.pendingMergeConfirmBody("a", "b")).toContain("alias");
    expect(en.lore.pendingMergeConfirmBody("a", "b")).not.toContain(
      "disappears",
    );
    expect(mergeLoreEntity).not.toHaveBeenCalled();
  });

  it("確認之後才真的送出;取消一次 API 都不打", async () => {
    await openPicker();
    fireEvent.click(screen.getAllByRole("radio")[0]);
    fireEvent.click(screen.getByTestId("lore-pending-merge-next"));

    // 先取消:確認框關掉,而且沒有送出。
    fireEvent.click(screen.getByText(zh.common.cancel));
    expect(screen.queryByTestId("lore-pending-merge-confirm")).toBeNull();
    expect(mergeLoreEntity).not.toHaveBeenCalled();

    // 再走一次,這次按確認。
    fireEvent.click(screen.getByTestId("lore-pending-merge-next"));
    fireEvent.click(screen.getByTestId("lore-pending-merge-confirm-ok"));

    await waitFor(() => expect(mergeLoreEntity).toHaveBeenCalledTimes(1));
    // 併的是**他挑的那一個**,不是清單上的第一個或伺服器指定的那一個。
    expect(mergeLoreEntity).toHaveBeenCalledWith("en-1", "en-a");
  });
});
