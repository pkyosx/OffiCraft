// The global-context block list and the 使用者自訂 block.
//
// 使用者語言 and 使用者自訂 head the list, open whatever developer mode says;
// the boot documents follow in boot-assembly order and are locked while
// developer mode is off. The editable
// behaviour of the document pages lives in BootDocPage.test.tsx; what is
// pinned here is the list itself, the routing into each page, and the
// 使用者自訂 block.
//

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { api } from "../api";
import { __resetMock } from "../api/mock";
import { infoHintText } from "../test/infoHint";

/** A row's explanation (behind its ⓘ) and its state tag (null when none). */
function entryCopy(row: HTMLElement) {
  const hint = row.querySelector('[data-testid="info-hint"]') ? infoHintText(row) : null;
  return [hint, row.querySelector('[data-testid="set-entry-tag"]')?.textContent ?? null];
}

const s = zh.settings;

/** Render Settings and navigate landing → 全域情境 (the block list; T-a241
 * moved it out of 角色誌 into its own section). */
async function openRolesLog() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.globalContext));
  // The three block entries render synchronously; the role list is async.
  await utils.findByText(s.systemName);
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · global-context 4 blocks", () => {
  it("lists 使用者語言 then 使用者自訂 on top, then the boot documents in boot-assembly order", async () => {
    // 啟動步驟 is ONE row, not one per runtime (owner 2026-08-14, card
    // rc-e1abbc506b70 option 1: "我沒有想到被切割成這麼多份"). The runtime is
    // chosen inside the page. The two DOCUMENTS stay separate — that is
    // asserted on the page itself, in BootDocPage.test.tsx.
    const { container, getByText } = await openRolesLog();
    const text = container.textContent ?? "";
    const iUserLanguage = text.indexOf(s.userLanguageName);
    const iCustom = text.indexOf(s.customName);
    const iBootGroup = text.indexOf(s.globalSection);
    const iSystem = text.indexOf(s.systemName);
    const iGlossary = text.indexOf(s.termGlossaryName);
    const iBoot = text.indexOf(s.bootName);
    // 〈停止〉 follows 啟動步驟: the list runs an agent's life end to end,
    // 開機 → 下線.
    const iOffboard = text.indexOf(s.offboardName);
    expect(iUserLanguage).toBeGreaterThanOrEqual(0);
    expect(iCustom).toBeGreaterThan(iUserLanguage);
    expect(iBootGroup).toBeGreaterThan(iCustom);
    expect(iSystem).toBeGreaterThan(iBootGroup);
    expect(iGlossary).toBeGreaterThan(iSystem);
    expect(iBoot).toBeGreaterThan(iGlossary);
    expect(iOffboard).toBeGreaterThan(iBoot);
    // The per-runtime names belong to the PAGE's two cards, never to this list.
    expect(text).not.toContain(s.bootClaudeName);
    expect(text).not.toContain(s.bootCodexName);
    expect(getByText(s.globalSection)).toBeTruthy();
  });

  it("while developer mode is off, only 使用者語言 and 使用者自訂 say they are editable", async () => {
    const utils = await openRolesLog();
    const copy = (testId: string) => entryCopy(utils.getByTestId(testId));
    expect(copy("boot-doc-entry-custom")).toEqual(["每位成員開機時都會讀到的自訂內容", "可編輯"]);
    expect(copy("boot-doc-entry-system_interaction")).toEqual(["每位成員開機時都會讀到的系統運作說明", null]);
    expect(copy("boot-doc-entry-task_closeout")).toEqual([null, null]);
    expect(copy("boot-doc-entry-user_language")).toEqual(["成員跟你說話用的語言 · 沒改過時跟著顯示語言", "可編輯"]);
    expect(copy("boot-doc-entry-term_glossary")).toEqual([null, "不能編輯"]);
    expect(copy("boot-doc-entry-accelerated_stop")).toEqual([null, null]);
    expect(copy("boot-doc-entry-task_takeover_with_predecessor")).toEqual([s.taskTakeoverWithPredecessorSub, null]);
    expect(utils.getByTestId("boot-doc-entry-task_closeout").hasAttribute("aria-describedby")).toBe(false);
    expect(
      utils.getByRole("button", { name: `${s.userLanguageName} 可編輯`, description: "成員跟你說話用的語言 · 沒改過時跟著顯示語言" }),
    ).toBe(
      utils.getByTestId("boot-doc-entry-user_language")
    );
  });

  it("with developer mode on, every boot document but the glossary says it is editable", async () => {
    await api.patchServerSettings({ developerMode: true });
    const utils = await openRolesLog();
    const copy = (testId: string) => entryCopy(utils.getByTestId(testId));
    await waitFor(() =>
      expect(copy("boot-doc-entry-system_interaction")).toEqual([
        "每位成員開機時都會讀到的系統運作說明",
        "可編輯",
      ])
    );
    expect(copy("boot-doc-entry-task_closeout")).toEqual([null, "可編輯"]);
    expect(copy("boot-doc-entry-term_glossary")).toEqual([null, "不能編輯"]);
    expect(copy("boot-doc-entry-boot_sequence")).toEqual([null, s.bootDocEditableTag]);
  });

  it("使用者語言 opens its document directly and saves it while developer mode is off", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByTestId("boot-doc-entry-user_language"));
    await utils.findByRole("heading", { name: s.userLanguageName });
    expect(utils.getByTestId("doc-card-status-badge").textContent).toBe("與預設內容同步");
    fireEvent.click(utils.getByTestId("doc-card-edit"));
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "請用克林貢語回報。" },
    });
    fireEvent.click(utils.getByTestId("doc-card-save"));
    fireEvent.click(await utils.findByTestId("doc-card-save-confirm-btn"));
    await utils.findByText("請用克林貢語回報。");
    expect((await api.getBootDoc("user_language", "global")).body).toBe("請用克林貢語回報。");
  });

  it("系統互動 opens its own editable page instead of a read-only seed card", async () => {
    await api.patchServerSettings({ developerMode: true });
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(s.systemName));
    // The live document arrives from the api seam, and the page offers the
    // affordances the read-only card had none of. Since T-c33e it draws them
    // with the SHARED <DocCard> — the same testids as 角色定義, and nothing of
    // its own: the top-level 還原出廠版 that used to be its one difference went
    // into edit mode with everything else (owner 2026-08-14, rc-f1950f4d286e).
    await utils.findByTestId("doc-card-usage");
    expect(utils.getByTestId("doc-card-edit")).toBeTruthy();
    expect(utils.container.querySelector(".doc-card__file code")).toBeNull();
  });

  it("使用者自訂 starts in sync with its empty default, and a save marks it modified", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(s.customName));
    await utils.findByText(s.edit);
    expect(utils.getByTestId("doc-card-status-badge").textContent).toBe(
      "與預設內容同步"
    );
    expect(utils.container.querySelector(".doc-card__file code")).toBeNull();

    fireEvent.click(utils.getByText(s.edit));
    const editor = utils.container.querySelector("textarea");
    expect(editor).toBeTruthy();
    fireEvent.change(editor!, { target: { value: "多用 emoji 回覆 owner" } });
    fireEvent.click(utils.getByText(s.doneEdit));

    await utils.findByText("多用 emoji 回覆 owner");
    await waitFor(() =>
      expect(utils.getByTestId("doc-card-status-badge").textContent).toBe(
        "已修改"
      )
    );
  });
});
