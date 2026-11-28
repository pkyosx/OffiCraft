// T-8f6e 設定導航統一 — every settings sub-page heads with the shared
// breadcrumb (「設定 › 子頁 › …」, each parent segment clickable back up the
// tree) with the page title directly below, and no 返回 back button. The
// landing itself has no breadcrumb.
//
//   1. Per-page header assertions: landing / 系統更新與備份 / 角色誌 / 角色詳情 /
//      系統互動·使用者自訂·啟動步驟 / 任務手冊 (list + hub) / 參數調整 /
//      使用說明 (list + doc).
//   2. Crumb jumps: a parent segment click lands on that page; jumps that have
//      a hash route (#settings / #settings/roles) also write the hash through
//      lib/hashRoute so the URL follows.
//
// Runs against the REAL mock adapter, like the sibling SettingsPage tests.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
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
  expect(utils.queryByRole("button", { name: "返回" })).toBeNull();
}

beforeEach(() => {
  __resetMock();
  // Crumb jumps write the hash (lib/hashRoute) — start every test clean.
  history.replaceState(null, "", window.location.pathname);
});

describe("SettingsPage · unified breadcrumb header (T-8f6e)", () => {
  it("landing: no breadcrumb nav, only the 設定 title", async () => {
    const utils = renderSettings();
    expect(utils.container.querySelector("nav.crumbs")).toBeNull();
    expect(utils.getByRole("heading", { name: "設定" })).toBeTruthy();
    expect(utils.queryByRole("button", { name: "設定" })).toBeNull();
  });

  it("#settings/guide/<slug>: that doc under 設定 › 使用說明 › <doc>", async () => {
    history.replaceState(null, "", window.location.pathname + "#settings/guide/why");
    const utils = renderSettings();
    await waitFor(() => {
      expect(utils.container.querySelector(".doc-md h1")?.textContent).toBe(
        "為什麼是 OffiCraft",
      );
    });
    expectHeader(utils, ["設定", "使用說明", "為什麼是 OffiCraft"]);
    expect(utils.getByRole("button", { name: "設定" })).toBeTruthy();
    expect(utils.getByRole("button", { name: "使用說明" })).toBeTruthy();
  });

  it("系統更新與備份: 設定 › 系統更新與備份 + title, no back button", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.software));
    expectHeader(utils, [s.title, s.software]);
    expect(utils.getByRole("heading", { name: s.software })).toBeTruthy();
  });

  it("角色誌: 設定 › 角色誌 + title, no back button", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(`+ ${s.addRole}`);
    expectHeader(utils, [s.title, s.roles]);
    expect(utils.getByRole("heading", { name: s.roles })).toBeTruthy();
  });

  it("角色詳情: 設定 › 角色誌 › <role>, no back button", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(`+ ${s.addRole}`);
    fireEvent.click(utils.getByText(zh.office.role.assistant));
    await utils.findAllByText(s.edit);
    expectHeader(utils, [s.title, s.roles, zh.office.role.assistant]);
  });

  it("系統互動 / 使用者自訂 / 啟動步驟: 設定 › 全域情境 › <doc>", async () => {
    // T-a241 moved these documents out of 角色誌 into their own section, so the
    // MIDDLE segment of every one of these trails is 全域情境 — and it has to
    // be, because that is the page the document is listed on now.
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.globalContext));
    await utils.findByText(s.systemName);

    for (const name of [s.systemName, s.customName, s.bootName]) {
      fireEvent.click(utils.getByText(name));
      expectHeader(utils, [s.title, s.globalContext, name]);
      // Back up to the 全域情境 list via its crumb for the next doc.
      fireEvent.click(utils.getByRole("button", { name: s.globalContext }));
      await utils.findByText(s.systemName);
    }

    // 啟動步驟 is ONE entry listing TWO documents (T-bac4: an index of two nav
    // rows, replacing the stacked pair). It gets ONE trail — the second row
    // must not add a second one. Without this half the loop above is satisfied
    // by a page that dropped the codex document altogether.
    //
    // ⚠️ Count the TRAIL, not the STRING 啟動步驟: the index has its own <h1>
    // (like every other settings page), so the string appears twice on a
    // correct page.
    fireEvent.click(utils.getByText(s.bootName));
    expect(await utils.findByTestId("boot-entry-claude")).toBeTruthy();
    expect(utils.getByTestId("boot-entry-codex")).toBeTruthy();
    expect(utils.container.querySelectorAll("nav.crumbs").length).toBe(1);

    // …and one level deeper. The runtime's own name is the TERMINAL segment on
    // purpose: Breadcrumbs renders the last one as plain text, so a trail
    // ending at 啟動步驟 would leave a reader who opened one runtime unable to
    // reach the other without going out to 全域情境 and back in.
    fireEvent.click(utils.getByTestId("boot-entry-claude"));
    await utils.findByTestId("doc-card-edit");
    expectHeader(utils, [
      s.title,
      s.globalContext,
      s.bootName,
      s.bootClaudeName,
    ]);
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

  it("一般設定 and 進階設定: 設定 › <page> + title, no back button", async () => {
    const general = renderSettings();
    fireEvent.click(general.getByTestId("settings-params-entry"));
    await general.findByLabelText(s.dueSoonLead);
    expectHeader(general, ["設定", "一般設定"]);
    expect(general.getByRole("heading", { name: "一般設定" })).toBeTruthy();
    general.unmount();

    const advanced = renderSettings();
    fireEvent.click(advanced.getByTestId("settings-advanced-entry"));
    await advanced.findByLabelText(s.sessionTtl);
    expectHeader(advanced, ["設定", "進階設定"]);
    expect(advanced.getByRole("heading", { name: "進階設定" })).toBeTruthy();
  });
});

describe("SettingsPage · crumb jumps write the hash (lib/hashRoute)", () => {
  it("使用說明 row: opens the list under 設定 › 使用說明; its 設定 crumb returns to the landing", async () => {
    const utils = renderSettings();
    expect(utils.getByTestId("settings-guide-entry").textContent).toBe("使用說明");
    fireEvent.click(utils.getByTestId("settings-guide-entry"));
    expect(window.location.hash).toBe("#settings/guide");
    await utils.findAllByTestId("guide-doc-entry");
    expectHeader(utils, ["設定", "使用說明"]);

    fireEvent.click(utils.getByRole("button", { name: "設定" }));
    expect(await utils.findByTestId("settings-guide-entry")).toBeTruthy();
    expect(window.location.hash).toBe("#settings");
    expect(utils.container.querySelector("nav.crumbs")).toBeNull();
  });

  it("設定 crumb: back to the landing, hash → #settings", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByTestId("settings-advanced-entry"));
    await utils.findByLabelText(s.sessionTtl);

    fireEvent.click(utils.getByRole("button", { name: s.title }));
    // Landing again (the entries render) and the URL followed the jump.
    expect(await utils.findByTestId("settings-manuals-entry")).toBeTruthy();
    expect(window.location.hash).toBe("#settings");
  });

  it("角色誌 crumb: back to the roles list, hash → #settings/roles", async () => {
    const utils = renderSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(`+ ${s.addRole}`);
    fireEvent.click(utils.getByText(zh.office.role.assistant));
    await utils.findAllByText(s.edit);

    fireEvent.click(utils.getByRole("button", { name: s.roles }));
    await utils.findByText(`+ ${s.addRole}`); // the roles list is back
    expect(window.location.hash).toBe("#settings/roles");
  });
});
