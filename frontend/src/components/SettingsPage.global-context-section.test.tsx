// T-a241 — 全域情境 is its own 〈設定〉 section, not a zone inside 角色誌.
//
// THE CLAIM. The ten boot/lifecycle documents (plus 使用者自訂, which is not one
// of them) were never role definitions; they were printed on the 角色誌 page
// only because that page existed first. This suite pins the split three ways:
//
//   1. POSITION on the landing, not mere presence — 全域情境 sits BETWEEN
//      系統更新與備份 and 角色誌. Asserting existence would pass with the row
//      appended last, which is a different page from the one that was asked for.
//   2. The whole 事件程序文件 zone moved, INCLUDING 使用者自訂 — the one row the
//      list prints that BOOT_DOC_ROWS does not own (its own route, no cap, its
//      own allow_shrink). A move that left it behind would strand it under a
//      heading whose other rows are gone.
//   3. A boot document's breadcrumb middle segment points at 全域情境, and
//      clicking it lands there — a trail that still said 角色誌 would send the
//      reader to a page the document is no longer on.
//
// Runs against the REAL mock adapter, like the sibling SettingsPage suites.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

function renderSettings() {
  return render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
}

type Utils = ReturnType<typeof renderSettings>;

/** The landing rows' visible names, in DOM order. */
function landingRowNames(utils: Utils): string[] {
  return Array.from(
    utils.container.querySelectorAll(".set-entries .set-entry__name")
  ).map((el) => el.textContent ?? "");
}

/** The breadcrumb's segment labels, in order (separators stripped). */
function crumbSegs(utils: Utils): string[] {
  return Array.from(
    utils.container.querySelectorAll("nav.crumbs .crumbs__seg")
  ).map((el) => (el.textContent ?? "").replace(/^›/, ""));
}

async function openGlobalContext() {
  const utils = renderSettings();
  fireEvent.click(utils.getByText(s.globalContext));
  await utils.findByTestId("boot-doc-entry-system_interaction");
  return utils;
}

beforeEach(() => {
  __resetMock();
  history.replaceState(null, "", window.location.pathname);
});

describe("T-a241 · 全域情境 as its own settings section", () => {
  it("landing: 全域情境 sits between 系統更新與備份 and 角色誌", () => {
    const utils = renderSettings();
    const names = landingRowNames(utils);
    // POSITION, not presence: the three are compared as an ordered slice, so a
    // row appended at the end of the landing fails here.
    expect(names.indexOf(s.globalContext)).toBeGreaterThan(-1);
    expect(names.slice(0, 3)).toEqual([s.software, s.globalContext, s.roles]);
  });

  it("全域情境 holds the whole 事件程序文件 zone, 使用者自訂 included", async () => {
    const { getByTestId, getByText } = await openGlobalContext();
    for (const kind of [
      "system_interaction",
      "boot_sequence",
      "offboard",
      "accelerated_stop",
      "task_closeout",
      "task_reassign_predecessor",
      "task_takeover_with_predecessor",
      "task_unblocked",
    ]) {
      expect(getByTestId(`boot-doc-entry-${kind}`)).toBeTruthy();
    }
    // 使用者自訂 rode along — it is NOT an eleventh boot document (own route,
    // own allow_shrink), it is the one extra row this list prints.
    expect(getByTestId("boot-doc-entry-custom")).toBeTruthy();
    // Under the same three group headings it always had.
    for (const label of [s.globalSection, s.stopSection, s.taskEventSection]) {
      expect(getByText(label)).toBeTruthy();
    }
  });

  it("a boot document's breadcrumb middle segment is 全域情境, and jumps there", async () => {
    const utils = await openGlobalContext();
    fireEvent.click(utils.getByTestId("boot-doc-entry-offboard"));
    await utils.findByRole("heading", { name: s.offboardName });
    expect(crumbSegs(utils)).toEqual([
      s.title,
      s.globalContext,
      s.offboardName,
    ]);
    // The middle segment is live navigation, not a label: it must land back on
    // the section that now holds the document.
    fireEvent.click(utils.getByRole("button", { name: s.globalContext }));
    expect(utils.getByTestId("boot-doc-entry-system_interaction")).toBeTruthy();
  });
});
