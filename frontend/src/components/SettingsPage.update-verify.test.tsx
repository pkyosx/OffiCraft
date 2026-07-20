// 設定 › 軟體更新 — the T-1c2e honesty behaviors, post updater-server teardown
// (t-dc68: updates come from GitHub Releases; the URL/invite-code form is
// gone). Pinned here:
//   - the 自動更新 switch verifies a save by reading the settings BACK and
//     comparing (write → fresh GET → compare): success feedback only after
//     the read-back matches, and both failure shapes (rejected write /
//     unverifiable read-back) report failure, never a fabricated success;
//   - the explicit 檢查更新 button surfaces the mock server's honest fresh
//     verdict (up to date — never a phantom newer release).

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";

const s = zh.settings;

/** Render Settings and open 軟體更新 (async settings load). */
async function openSoftware() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.software));
  await utils.findByTestId("settings-auto-update"); // settings loaded
  return utils;
}

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SettingsPage · 軟體更新 · 檢查更新 (explicit fresh verdict)", () => {
  it("clicking 檢查更新 shows the honest up-to-date verdict", async () => {
    const utils = await openSoftware();
    fireEvent.click(utils.getByTestId("settings-check-release"));
    const status = await utils.findByTestId("settings-update-status");
    await vi.waitFor(() => expect(status.textContent).toBe(s.upToDate));
  });

  it("a failed check reports the failure line, never a fabricated verdict", async () => {
    const utils = await openSoftware();
    vi.spyOn(api, "checkRelease").mockRejectedValue(
      new Error("boom: transport down")
    );
    fireEvent.click(utils.getByTestId("settings-check-release"));
    await utils.findByText(s.checkFailed);
    expect(utils.queryByText(s.upToDate)).toBeNull();
  });

  // ── owner 2026-07-20, the whole point of this fixup ──
  // "資訊應該直接 refresh 已是最新版那邊,而不是再產生新的 message":
  // the card already carries a 已是最新版 chip, and the old build appended a
  // SECOND one below it after a check. Pin that the check REPLACES the one
  // status line and creates no additional result element.
  it("checking does not add a second result element — 已是最新版 stays a single node", async () => {
    const utils = await openSoftware();
    // Before: exactly one status line, carrying the cached verdict.
    expect(utils.getAllByText(s.upToDate)).toHaveLength(1);
    const status = utils.getByTestId("settings-update-status");

    fireEvent.click(utils.getByTestId("settings-check-release"));
    // The SAME node goes through the in-flight state — not a new one.
    await vi.waitFor(() => expect(status.textContent).toBe(s.checkingUpdate));
    await vi.waitFor(() => expect(status.textContent).toBe(s.upToDate));

    // After: still exactly one. A regression that re-adds the separate
    // verdict row makes this 2.
    expect(utils.getAllByText(s.upToDate)).toHaveLength(1);
    expect(utils.getAllByTestId("settings-update-status")).toHaveLength(1);
  });

  // The refresh control is icon-only: its accessible name must survive.
  it("the refresh control is reachable by its accessible name", async () => {
    const utils = await openSoftware();
    const btn = utils.getByRole("button", { name: s.checkUpdate });
    expect(btn).toBe(utils.getByTestId("settings-check-release"));
  });
});

describe("SettingsPage · 軟體更新 · 自動更新存檔回讀 (存檔測連通)", () => {
  it("toggling 自動更新 writes, reads back, and reports the reconciled success", async () => {
    const utils = await openSoftware();
    const toggle = utils.getByTestId("settings-auto-update");
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(toggle);
    // Success line appears only after the read-back matched.
    await utils.findByText(s.configSaved);
    expect((await api.getServerSettings()).updaterAutoUpdate).toBe(true);
    expect(toggle.getAttribute("aria-checked")).toBe("true");
  });

  // ── the read-back verification's FAILURE paths ──
  // These two pin that success is decided by the RE-GET + compare, not by the
  // PATCH resolving: strip commitVerified's read-back (report success
  // unconditionally) and both go red.

  it("a rejected write reads back as fail: 失敗 line + the switch stays put, never the success line", async () => {
    const utils = await openSoftware();
    // Server refuses the write (e.g. down mid-request). The hook folds the
    // rejection away, so ONLY the read-back can tell the truth here.
    vi.spyOn(api, "patchServerSettings").mockRejectedValue(
      new Error("boom: server unreachable")
    );
    const toggle = utils.getByTestId("settings-auto-update");
    fireEvent.click(toggle);
    await utils.findByText(s.configSaveFailed);
    expect(utils.queryByText(s.configSaved)).toBeNull();
    // The switch still shows the server's last confirmed value.
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect((await api.getServerSettings()).updaterAutoUpdate).toBe(false);
  });

  it("a write whose read-back cannot be verified reports fail, not a fabricated success", async () => {
    const utils = await openSoftware();
    // The PATCH lands but the verifying GET rejects — the view cannot know
    // what the server stored, so it must NOT claim the reconciled success.
    vi.spyOn(api, "getServerSettings").mockRejectedValue(
      new Error("boom: verify fetch failed")
    );
    const toggle = utils.getByTestId("settings-auto-update");
    fireEvent.click(toggle);
    await utils.findByText(s.configSaveFailed);
    expect(utils.queryByText(s.configSaved)).toBeNull();
  });
});
