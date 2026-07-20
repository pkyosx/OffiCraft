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

  // ── owner round-2 item ④: the two verdicts the mock server never returns ──
  // The mock's checkRelease() only ever answers up_to_date, so update_available
  // and unknown had ZERO behavioural coverage — their render paths were carried
  // by tsc alone. Both are forced here through the api seam (spyOn), which is
  // how every other honesty test in this file forces a server answer.

  it("update_available renders the tag + release link in the ONE status line", async () => {
    const utils = await openSoftware();
    vi.spyOn(api, "checkRelease").mockResolvedValue({
      status: "update_available",
      currentVersion: "0.4.7",
      latestTag: "v0.9.9",
      releaseUrl: "https://github.com/pkyosx/OffiCraft/releases/tag/v0.9.9",
    });
    fireEvent.click(utils.getByTestId("settings-check-release"));

    const status = await utils.findByTestId("settings-update-status");
    await vi.waitFor(() =>
      expect(status.textContent).toContain(s.updateAvailable)
    );
    // The server's tag is shown verbatim — never a fabricated or omitted one.
    expect(status.textContent).toContain("v0.9.9");
    // The link points at the server's release_url and opens out-of-app.
    const link = utils.getByText(s.viewRelease) as HTMLAnchorElement;
    expect(link.tagName).toBe("A");
    expect(link.getAttribute("href")).toBe(
      "https://github.com/pkyosx/OffiCraft/releases/tag/v0.9.9"
    );
    expect(link.getAttribute("target")).toBe("_blank");
    // Still ONE status line, and the stale 已是最新版 is gone.
    expect(utils.getAllByTestId("settings-update-status")).toHaveLength(1);
    expect(utils.queryByText(s.upToDate)).toBeNull();
  });

  // ── owner round-2 item ③ ──
  // The 升級 button used to be gated on the CACHED /api/version flag while the
  // badge showed the FRESH verdict, so a fresh update_available produced a card
  // that announced a new version with no way to take it. Same source now.
  it("update_available also offers the 升級 button — badge and button share one source", async () => {
    const utils = await openSoftware();
    // The cache says up to date (the mock's /api/version), so ONLY the fresh
    // verdict can put this button on screen.
    expect(utils.queryByText(s.upgrade)).toBeNull();
    vi.spyOn(api, "checkRelease").mockResolvedValue({
      status: "update_available",
      currentVersion: "0.4.7",
      latestTag: "v0.9.9",
      releaseUrl: null,
    });
    fireEvent.click(utils.getByTestId("settings-check-release"));
    await utils.findByText(s.upgrade);
  });

  it("unknown reports GitHub-unreachable, never a fabricated up-to-date or upgrade offer", async () => {
    const utils = await openSoftware();
    vi.spyOn(api, "checkRelease").mockResolvedValue({
      status: "unknown",
      currentVersion: "0.4.7",
      latestTag: null,
      releaseUrl: null,
    });
    fireEvent.click(utils.getByTestId("settings-check-release"));

    const status = await utils.findByTestId("settings-update-status");
    await vi.waitFor(() => expect(status.textContent).toBe(s.checkUnknown));
    // The honest degraded verdict must not read as either good news…
    expect(utils.queryByText(s.upToDate)).toBeNull();
    expect(utils.queryByText(s.updateAvailable)).toBeNull();
    // …nor as a transport failure (a different, distinct sentence).
    expect(utils.queryByText(s.checkFailed)).toBeNull();
    // …and it must never offer an upgrade it cannot substantiate.
    expect(utils.queryByText(s.upgrade)).toBeNull();
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
