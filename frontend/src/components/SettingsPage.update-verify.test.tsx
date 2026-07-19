// 設定 › 軟體更新 — the T-1c2e honesty behaviors, at their post-rework home
// (owner: the standalone 伺服器設定 view duplicated existing edit surfaces and
// was retired; its one non-duplicated line + the 存檔測連通 read-back rule
// moved into this card). Pinned here:
//   - the SECRET status row (updater.invite_code) shows only 已設定/未設定 and
//     the stored plaintext NEVER appears anywhere in the rendered view (repo
//     red line — the wire itself only carries the set/unset bit);
//   - the 自動更新 switch verifies a save by reading the settings BACK and
//     comparing (write → fresh GET → compare): success feedback only after
//     the read-back matches, and both failure shapes (rejected write /
//     unverifiable read-back) report failure, never a fabricated success.

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
  await utils.findByLabelText(s.updaterUrl); // settings loaded
  return utils;
}

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SettingsPage · 軟體更新 · 邀請碼狀態 (secret masking)", () => {
  it("shows 未設定/已設定 and NEVER the stored plaintext", async () => {
    // Unset first — the badge reads 未設定.
    let utils = await openSoftware();
    expect(utils.getByTestId("settings-invite-status").textContent).toBe(
      s.configValueUnset
    );
    utils.unmount();

    // Store a secret, reopen: the badge flips to 已設定 and the plaintext is
    // nowhere in the rendered document (not as value, not as attribute).
    await api.patchServerSettings({ updaterInviteCode: "hunter2-secret" });
    utils = await openSoftware();
    expect(utils.getByTestId("settings-invite-status").textContent).toBe(
      s.configSecretSet
    );
    expect(utils.container.innerHTML).not.toContain("hunter2-secret");
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
