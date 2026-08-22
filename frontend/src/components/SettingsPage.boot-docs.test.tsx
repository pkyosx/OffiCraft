// 設定 › 角色誌 lists all ten boot/lifecycle documents (T-3201), and the two the
// server refuses every write to are SHOWN without an editor.
//
// The claim underneath every assertion here: a document that ships must be
// reachable, and whether it may be edited is the SERVER's answer, read off the
// document itself. The cockpit keeps no list of which ones are read-only — the
// registry-parity gate (api/mock.boot-doc-registry.test.ts) covers the other
// half, that no document is missing from the list at all.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

async function openRolesLog() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  await utils.findByText(s.systemName);
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · boot / lifecycle documents", () => {
  it("lists every document under its own group heading", async () => {
    const { getByText, getByTestId } = await openRolesLog();

    for (const label of [
      s.globalSection,
      s.stopSection,
      s.taskEventSection,
      s.readOnlySection,
    ]) {
      expect(getByText(label)).toBeTruthy();
    }

    for (const kind of [
      "system_interaction",
      "boot_sequence",
      "offboard",
      "accelerated_stop",
      "task_closeout",
      "task_reassign_predecessor",
      "task_takeover_with_predecessor",
      "task_takeover_fresh",
      "task_unblocked",
    ]) {
      expect(getByTestId(`boot-doc-entry-${kind}`)).toBeTruthy();
    }
    // 使用者自訂 is not a boot document — different route, no cap, its own
    // allow_shrink — but it still sits in the 上線 group where the boot context
    // assembles it.
    expect(getByTestId("boot-doc-entry-custom")).toBeTruthy();
  });

  it("opens an editable event procedure and saves through it", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByTestId("boot-doc-entry-task_closeout"));

    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "# 結案時要做的事" },
    });
    fireEvent.click(utils.getByTestId("doc-card-save"));
    fireEvent.click(await utils.findByTestId("doc-card-save-confirm-btn"));
    await utils.findByText("結案時要做的事");
  });

  it("opens 加速停止 through its own row, not 下線程序's", async () => {
    // The two share a cap and sit in the same group; opening one must not land
    // on the other. The page TITLE is what says which one is on screen — the
    // 加速停止 body legitimately contains 「下線程序」 (it is the same procedure
    // under a shorter clock), so a body-text assertion would be measuring the
    // seed rather than the routing.
    const utils = await openRolesLog();
    fireEvent.click(utils.getByTestId("boot-doc-entry-accelerated_stop"));
    await utils.findByTestId("doc-card-edit");
    expect(
      utils.container.querySelector(".settings__title--doc")?.textContent
    ).toBe(s.acceleratedStopName);
  });

  it("renders a read-only document and offers no way to change it", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByTestId("boot-doc-entry-task_unblocked"));

    // The BODY is there — the whole reason it is a document rather than a
    // string literal is that the owner can read what an agent is told.
    await utils.findByTestId("doc-card-note");
    expect(utils.container.querySelectorAll(".doc-md").length).toBe(1);
    expect(utils.getByText(s.bootDocReadOnlyNote)).toBeTruthy();

    // …and nothing that could change it: no editor entry, and therefore no
    // version list and no 還原出廠版 (both live inside edit mode).
    expect(utils.queryByTestId("doc-card-edit")).toBeNull();
    expect(utils.queryByTestId("doc-card-editor")).toBeNull();
    expect(utils.queryByText(s.historyTaskUnblockedTitle)).toBeNull();
    // 「儲存＝整份取代」 is a fact about saving, so a document nobody may save
    // does not carry it.
    expect(utils.queryByTestId("doc-card-replace-note")).toBeNull();
  });

  it("keeps the editor for the documents the server does allow", async () => {
    // The paired control: without it, a page that hid the editor from EVERY
    // document would pass the assertion above.
    const utils = await openRolesLog();
    fireEvent.click(utils.getByTestId("boot-doc-entry-task_takeover_with_predecessor"));
    await utils.findByTestId("doc-card-edit");
    expect(utils.queryByTestId("doc-card-note")).toBeNull();
    expect(utils.getByTestId("doc-card-replace-note")).toBeTruthy();
  });
});
