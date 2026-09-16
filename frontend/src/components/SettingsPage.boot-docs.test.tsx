// 設定 › 全域情境 lists all ten boot/lifecycle documents (T-3201) under THREE
// group headings (定稿 2026-08-24), and the ones the server refuses every write
// to are SHOWN without an editor.
//
// The claim underneath every assertion here: a document that ships must be
// reachable, and whether it may be edited is the SERVER's answer, read off the
// document itself. The cockpit keeps no list of which ones are read-only — that
// is exactly why the fourth group (「只顯示、不給改」) is gone and its two
// documents sit with the other task events, where their SUBJECT puts them. The
// registry-parity gate (api/mock.boot-doc-registry.test.ts) covers the other
// half, that no document is missing from the list at all.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, __setBootDocReadOnly } from "../api/mock";

const s = zh.settings;

async function openRolesLog() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.globalContext));
  await utils.findByText(s.systemName);
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · boot / lifecycle documents", () => {
  it("lists every document under its own group heading", async () => {
    const { getByText, getByTestId } = await openRolesLog();

    for (const label of [s.globalSection, s.stopSection, s.taskEventSection]) {
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

  it("prints the two ex-唯讀 documents inside the task-event group", async () => {
    // The MOVE itself, not just the vanished heading: 新任務 and 擋著你手上任務的
    // 票解開了 are task events by subject, and the group they sit in says nothing
    // about whether the server lets them be written — that answer is read off
    // each document (the read-only case is asserted further down, unchanged).
    const { container } = await openRolesLog();
    const heading = [...container.querySelectorAll(".set-group-label")].find(
      (el) => el.textContent === s.taskEventSection
    );
    expect(heading).toBeTruthy();
    const group = heading!.parentElement!;
    for (const kind of [
      "task_closeout",
      "task_reassign_predecessor",
      "task_takeover_with_predecessor",
      "task_takeover_fresh",
      "task_unblocked",
    ]) {
      expect(
        group.querySelector(`[data-testid="boot-doc-entry-${kind}"]`)
      ).toBeTruthy();
    }
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

  it("opens 加速停止 through its own row, not 停止's", async () => {
    // The two share a cap and sit in the same group; opening one must not land
    // on the other, and only an EXACT title match can tell them apart:
    // 「加速停止」contains 「停止」 as a substring, and the accelerated seed's own
    // H1 is literally 「# 加速停止」 — so a substring check on the title AND a
    // body-text search both pass on either page. Hence the strict equality
    // against s.acceleratedStopName below.
    // ⚠️ The previous version of this comment justified the same assertion by
    // saying the 加速停止 body "legitimately contains 「下線程序」". That is FALSE
    // today: seeds/accelerated_stop.md contains 「下線程序」 zero times (T-6f44
    // renamed that document). The conclusion survived; its reason did not.
    const utils = await openRolesLog();
    fireEvent.click(utils.getByTestId("boot-doc-entry-accelerated_stop"));
    await utils.findByTestId("doc-card-edit");
    expect(
      utils.container.querySelector(".settings__title--doc")?.textContent
    ).toBe(s.acceleratedStopName);
  });

  // 🔴 NO SHIPPED DOCUMENT IS READ-ONLY ANY MORE (定稿 決定 2, 2026-08-24), so
  // this drives the refusal path with a SYNTHETIC one. The alternative was to
  // delete these assertions, which would have retired a live path into code
  // nothing exercises — reachable again the day a document ships read-only,
  // which is exactly the day nobody would notice it had rotted.
  it("renders a read-only document and offers no way to change it", async () => {
    __setBootDocReadOnly(["task_unblocked/global"]);
    const utils = await openRolesLog();
    fireEvent.click(utils.getByTestId("boot-doc-entry-task_unblocked"));

    // The BODY is there — the whole reason it is a document rather than a
    // string literal is that the owner can read what an agent is told.
    await utils.findByTestId("doc-card-note");
    // ⚠️ ONE BODY, and the selector says so: a boot-context document renders a
    // SECOND `.doc-md` for its read-only head (T-3201), so counting every one
    // on the page would count two and the assertion would have to be relaxed
    // to a number that no longer means "one document".
    expect(
      utils.container.querySelectorAll(".doc-card__body > .doc-md").length
    ).toBe(1);
    expect(
      utils.container.querySelectorAll(".doc-card__readonly-head .doc-md").length
    ).toBe(1);
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

  // 決定 2 itself, on the UI side: the two documents that used to be refused
  // are editable now. Without this, the mock could quietly go back to calling
  // them read-only and only the synthetic test above would still pass — it
  // supplies its own read-only list, so it cannot notice.
  it("offers the editor on the two documents that used to be read-only", async () => {
    for (const kind of ["task_takeover_fresh", "task_unblocked"]) {
      const utils = await openRolesLog();
      fireEvent.click(utils.getByTestId(`boot-doc-entry-${kind}`));
      expect(await utils.findByTestId("doc-card-edit")).toBeTruthy();
      utils.unmount();
      __resetMock();
    }
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
