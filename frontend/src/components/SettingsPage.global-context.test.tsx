// The global-context block list and the 使用者自訂 block.
//
// 使用者自訂 heads the list on its own; the boot documents follow in
// boot-assembly order and are locked while developer mode is off. The editable
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
  it("lists 使用者自訂 alone on top, then the lock line, then the boot documents in boot-assembly order", async () => {
    // 啟動步驟 is ONE row, not one per runtime (owner 2026-08-14, card
    // rc-e1abbc506b70 option 1: "我沒有想到被切割成這麼多份"). The runtime is
    // chosen inside the page. The two DOCUMENTS stay separate — that is
    // asserted on the page itself, in BootDocPage.test.tsx.
    const { container, getByText, findByTestId } = await openRolesLog();
    await findByTestId("boot-docs-locked-note");
    const text = container.textContent ?? "";
    const iCustom = text.indexOf(s.customName);
    const iLock = text.indexOf("以下文件要修改，請先在系統設定打開開發者模式。");
    const iBootGroup = text.indexOf(s.globalSection);
    const iSystem = text.indexOf(s.systemName);
    const iBoot = text.indexOf(s.bootName);
    // 〈停止〉 follows 啟動步驟: the list runs an agent's life end to end,
    // 開機 → 下線.
    const iOffboard = text.indexOf(s.offboardName);
    expect(iCustom).toBeGreaterThanOrEqual(0);
    expect(iLock).toBeGreaterThan(iCustom);
    expect(iBootGroup).toBeGreaterThan(iLock);
    expect(iSystem).toBeGreaterThan(iBootGroup);
    expect(iBoot).toBeGreaterThan(iSystem);
    expect(iOffboard).toBeGreaterThan(iBoot);
    // The per-runtime names belong to the PAGE's two cards, never to this list.
    expect(text).not.toContain(s.bootClaudeName);
    expect(text).not.toContain(s.bootCodexName);
    expect(getByText(s.globalSection)).toBeTruthy();
  });

  it("while developer mode is off, only 使用者自訂 says it is editable, and the lock line's link opens 系統更新與備份", async () => {
    const utils = await openRolesLog();
    await utils.findByTestId("boot-docs-locked-note");
    const sub = (testId: string) =>
      utils.getByTestId(testId).querySelector(".set-entry__sub")?.textContent;
    expect(sub("boot-doc-entry-custom")).toBe("每位成員開機時都會讀到的自訂內容 · 可編輯");
    expect(sub("boot-doc-entry-system_interaction")).toBe("每位成員開機時都會讀到的系統運作說明");
    expect(sub("boot-doc-entry-task_closeout")).toBe("任務被判定結束時給 agent 的收尾指示");

    fireEvent.click(utils.getByRole("button", { name: "系統設定" }));
    expect(
      (await utils.findByTestId("settings-developer-mode")).getAttribute("aria-checked")
    ).toBe("false");
  });

  it("with developer mode on, there is no lock line and every row says it is editable", async () => {
    await api.patchServerSettings({ developerMode: true });
    const utils = await openRolesLog();
    const sub = (testId: string) =>
      utils.getByTestId(testId).querySelector(".set-entry__sub")?.textContent;
    await waitFor(() =>
      expect(sub("boot-doc-entry-system_interaction")).toBe(
        "每位成員開機時都會讀到的系統運作說明 · 可編輯"
      )
    );
    expect(sub("boot-doc-entry-task_closeout")).toBe("任務被判定結束時給 agent 的收尾指示 · 可編輯");
    expect(utils.queryByTestId("boot-docs-locked-note")).toBeNull();
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
