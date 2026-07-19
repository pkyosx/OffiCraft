// The 3-block global-context restructure (step2 FE):
//
//   1. 系統互動 (system interaction) — read-only seed card: NO edit affordance.
//   2. 使用者自訂 (user custom)      — the ONLY editable block; save/reset ride
//      the /api/global-context wire (mock adapter here; the POST-verb contract
//      is pinned in http.global-context.test.ts).
//   3. 啟動程序 (boot sequence)      — read-only seed card: NO edit affordance.
//
// Plus the presentation rule: none of the three blocks surfaces a .md filename
// (blocks are content, not files).

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

/** Render Settings and navigate landing → 角色誌 (the roles/blocks list). */
async function openRolesLog() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  // The three block entries render synchronously; the role list is async.
  await utils.findByText(s.systemName);
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · global-context 3 blocks", () => {
  it("lists the three blocks in boot-assembly order, without .md filenames", async () => {
    const { container, getByText } = await openRolesLog();
    const text = container.textContent ?? "";
    const iSystem = text.indexOf(s.systemName);
    const iCustom = text.indexOf(s.customName);
    const iBoot = text.indexOf(s.bootName);
    expect(iSystem).toBeGreaterThanOrEqual(0);
    expect(iCustom).toBeGreaterThan(iSystem);
    expect(iBoot).toBeGreaterThan(iCustom);
    expect(getByText(s.globalSection)).toBeTruthy();
    // Presentation rule: the blocks never expose their backing filenames.
    expect(text).not.toContain("global-context.md");
    expect(text).not.toContain("boot_sequence.md");
    expect(text).not.toContain("system_interaction.md");
  });

  it("系統互動 is read-only: badge, content, no edit entry, no filename", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(s.systemName));
    await utils.findByText(s.readOnlyBadge);
    // The rendered seed content is present (its top heading), read-only.
    expect(
      within(utils.container).getByText(/Global Context（AI 工作室/)
    ).toBeTruthy();
    expect(utils.queryByText(s.edit)).toBeNull();
    expect(utils.container.querySelector("textarea")).toBeNull();
    // No filename chrome on the doc card.
    expect(utils.container.querySelector(".doc-card__file code")).toBeNull();
    expect(utils.container.textContent).not.toMatch(/\.md\b/);
  });

  it("啟動程序 is read-only: badge, no edit entry, no filename", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(s.bootName));
    await utils.findByText(s.bootBadge);
    expect(utils.queryByText(s.edit)).toBeNull();
    expect(utils.container.querySelector("textarea")).toBeNull();
    expect(utils.container.querySelector(".doc-card__file code")).toBeNull();
    expect(utils.container.textContent).not.toContain("boot_sequence.md");
  });

  it("使用者自訂 is editable: starts empty/default, save persists via the api", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(s.customName));
    // Empty seed → the default badge shows and the edit affordance exists.
    await utils.findByText(s.edit);
    expect(utils.getByText(s.defaultBadge)).toBeTruthy();
    expect(utils.container.querySelector(".doc-card__file code")).toBeNull();

    fireEvent.click(utils.getByText(s.edit));
    const editor = utils.container.querySelector("textarea");
    expect(editor).toBeTruthy();
    fireEvent.change(editor!, { target: { value: "多用 emoji 回覆 owner" } });
    fireEvent.click(utils.getByText(s.doneEdit));

    // The save response folds back: owner text rendered, default badge gone.
    await utils.findByText("多用 emoji 回覆 owner");
    await waitFor(() => expect(utils.queryByText(s.defaultBadge)).toBeNull());
  });
});
