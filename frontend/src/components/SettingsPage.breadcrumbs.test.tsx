// T-8f6e 設定導航統一 — EVERY settings page heads with the shared breadcrumb
// (「設定 › 子頁 › …」, each parent segment clickable back up the tree) with
// the page title directly below, and the old ‹ 返回 back row is GONE.
//
//   1. Per-page header assertions: landing / 軟體更新 / 角色誌 / 角色詳情 /
//      系統互動·使用者自訂·啟動程序 / 任務手冊 (list + hub) / 參數調整 all
//      render the breadcrumb and NO back button.
//   2. Crumb jumps: a parent segment click lands on that page; jumps that have
//      a hash route (#settings / #settings/roles) also write the hash through
//      lib/hashRoute so the URL follows.
//
// Runs against the REAL mock adapter, like the sibling SettingsPage tests.

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

/** The breadcrumb's segment labels, in order (separators stripped). */
function crumbSegs(utils: Utils): string[] {
  return Array.from(
    utils.container.querySelectorAll("nav.crumbs .crumbs__seg")
  ).map((el) => (el.textContent ?? "").replace(/^›/, ""));
}

/** The unified header contract: breadcrumb segments + NO back button. */
function expectHeader(utils: Utils, segs: string[]) {
  expect(crumbSegs(utils)).toEqual(segs);
  // 返回鍵移除 — neither the old .set-back row nor any 返回-labelled button.
  expect(utils.container.querySelector(".set-back")).toBeNull();
  expect(utils.queryByRole("button", { name: "返回" })).toBeNull();
}

beforeEach(() => {
  __resetMock();
  // Crumb jumps write the hash (lib/hashRoute) — start every test clean.
  history.replaceState(null, "", window.location.pathname);
});

describe("SettingsPage · unified breadcrumb header (T-8f6e)", () => {
  it("landing: single 設定 segment with the title below", async () => {
    const utils = renderSettings();
    expectHeader(utils, [s.title]);
    // Title sits below the crumbs — and the landing's crumb is the current
    // page (plain text), so the only 設定 button would be none.
    expect(utils.getByRole("heading", { name: s.title })).toBeTruthy();
    expect(utils.queryByRole("button", { name: s.title })).toBeNull();
  });

  it("軟體更新: 設定 › 軟體更新 + title, no back button", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.software));
    expectHeader(utils, [s.title, s.software]);
    expect(utils.getByRole("heading", { name: s.software })).toBeTruthy();
  });

  it("角色誌: 設定 › 角色誌 + title, no back button", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);
    expectHeader(utils, [s.title, s.roles]);
    expect(utils.getByRole("heading", { name: s.roles })).toBeTruthy();
  });

  it("角色詳情: 設定 › 角色誌 › <role>, no back button", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);
    fireEvent.click(utils.getByText(zh.office.role.assistant));
    await utils.findAllByText(s.edit);
    expectHeader(utils, [s.title, s.roles, zh.office.role.assistant]);
  });

  it("系統互動 / 使用者自訂 / 啟動程序: 設定 › 角色誌 › <doc>", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);

    for (const name of [s.systemName, s.customName, s.bootName]) {
      fireEvent.click(utils.getByText(name));
      expectHeader(utils, [s.title, s.roles, name]);
      // Back up to the roles list via the 角色誌 crumb for the next doc.
      fireEvent.click(utils.getByRole("button", { name: s.roles }));
      await utils.findByText(s.systemName);
    }
  });

  it("任務手冊列表: 設定 › 任務手冊 + title; hub: 設定 › 任務手冊 › <type>", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByTestId("settings-manuals-entry"));
    await utils.findByTestId("manual-add-entry");
    expectHeader(utils, [s.title, s.manuals]);
    expect(utils.getByRole("heading", { name: s.manuals })).toBeTruthy();

    // Create a type by DISPLAY NAME (T-fa76: the system mints the tm- key),
    // open its hub (the pattern's reference page).
    fireEvent.click(utils.getByTestId("manual-add-entry"));
    fireEvent.change(utils.getByTestId("manual-create-key"), {
      target: { value: "審查 PR" },
    });
    fireEvent.keyDown(utils.getByTestId("manual-create-key"), { key: "Enter" });
    fireEvent.click(await utils.findByText("審查 PR"));
    await utils.findByTestId("manual-assignee-card");
    // The trailing crumb is the DISPLAY name — the tm- key never leaks into
    // the crumb (no text, no tooltip).
    expectHeader(utils, [s.title, s.manuals, "審查 PR"]);
    const lastCrumb = utils.container.querySelector(
      "nav.crumbs .crumbs__seg:last-child .crumbs__here"
    );
    expect(lastCrumb?.textContent).toBe("審查 PR");
    expect(lastCrumb?.getAttribute("title")).toBeNull();

    // The 任務手冊 crumb jumps back to the list.
    fireEvent.click(utils.getByRole("button", { name: s.manuals }));
    await utils.findByTestId("manual-add-entry");
    expectHeader(utils, [s.title, s.manuals]);
  });

  it("參數調整: 設定 › 參數調整 + title, no back button", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByTestId("settings-params-entry"));
    await utils.findByLabelText(s.sessionTtl);
    expectHeader(utils, [s.title, s.params]);
    expect(utils.getByRole("heading", { name: s.params })).toBeTruthy();
  });

  it("使用說明列表: 設定 › 使用說明 + title; doc: 設定 › 使用說明 › <title>", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByTestId("settings-guide-entry"));
    const entry = await utils.findByTestId("guide-doc-entry");
    expectHeader(utils, [s.title, s.guide]);
    expect(utils.getByRole("heading", { name: s.guide })).toBeTruthy();

    // Open the doc — its markdown renders and its title trails the crumb.
    const docTitle = entry.textContent ?? "";
    fireEvent.click(entry);
    await utils.findByText("各項功能的說明", { exact: false });
    expectHeader(utils, [s.title, s.guide, docTitle]);

    // The 使用說明 crumb jumps back to the list.
    fireEvent.click(utils.getByRole("button", { name: s.guide }));
    await utils.findByTestId("guide-doc-entry");
    expectHeader(utils, [s.title, s.guide]);
  });
});

describe("SettingsPage · crumb jumps write the hash (lib/hashRoute)", () => {
  it("設定 crumb: back to the landing, hash → #settings", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByTestId("settings-params-entry"));
    await utils.findByLabelText(s.sessionTtl);

    fireEvent.click(utils.getByRole("button", { name: s.title }));
    // Landing again (the entries render) and the URL followed the jump.
    expect(await utils.findByTestId("settings-manuals-entry")).toBeTruthy();
    expect(window.location.hash).toBe("#settings");
  });

  it("角色誌 crumb: back to the roles list, hash → #settings/roles", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);
    fireEvent.click(utils.getByText(zh.office.role.assistant));
    await utils.findAllByText(s.edit);

    fireEvent.click(utils.getByRole("button", { name: s.roles }));
    await utils.findByText(s.systemName); // the roles list is back
    expect(window.location.hash).toBe("#settings/roles");
  });
});
