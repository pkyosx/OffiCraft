// 設定 › 實驗功能 (owner 2026-09-06:「在參數調整下面可以多一個實驗功能」).
//
// The new landing row sits between 參數調整 and 主題, and its page lists the
// station-wide switches for features still being tried out. Today that list is
// ONE row — 傳承 (lore) — because `lore.enabled` is the only boolean in the
// closed /api/settings key set that is an experiment: receive_beta and
// auto_update are update behaviour and display.wide is a layout preference.
//
// No new API: the page loads, saves and fails through the SAME /api/settings
// seam 參數調整 already uses, so the honesty contract is inherited rather than
// re-invented — a rejected LOAD shows the error line instead of a switch whose
// position would be a guess, and a rejected WRITE leaves the switch on the last
// value the server confirmed.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";

const s = zh.settings;

function renderSettings() {
  return render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
}

/** Render Settings and navigate landing → 實驗功能 (async settings load). */
async function openExperimental() {
  const utils = renderSettings();
  fireEvent.click(utils.getByTestId("settings-experimental-entry"));
  await utils.findByTestId("settings-lore-enabled"); // settings loaded
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · 實驗功能", () => {
  it("puts the landing row between 參數調整 and 主題", () => {
    const utils = renderSettings();
    const rows = Array.from(
      utils.container.querySelectorAll<HTMLElement>(".set-entries > .set-entry")
    ).map((el) => el.textContent ?? "");
    const at = (label: string) => rows.findIndex((r) => r.includes(label));
    const params = at(s.params);
    const experimental = at(s.experimental);
    const theme = at(s.themeManage);
    // Position IS the request —「在參數調整下面」 — so this pins the two
    // neighbours rather than merely asserting the row exists somewhere.
    expect(params).toBeGreaterThanOrEqual(0);
    expect(experimental).toBe(params + 1);
    expect(theme).toBe(experimental + 1);
  });

  it("lists 傳承 with what turning it on does, and the server's own value", async () => {
    const utils = await openExperimental();
    expect(utils.getByText(s.experimentalLore)).toBeTruthy();
    // The one-line description is the point of the row: a bare switch labelled
    // 「傳承」 tells the owner nothing about what flipping it costs or gives.
    expect(utils.getByText(s.experimentalLoreSub)).toBeTruthy();
    const sw = utils.getByTestId("settings-lore-enabled");
    // The shipped default is OFF, and the switch reads the server, not a guess.
    expect(sw.getAttribute("aria-checked")).toBe("false");
    expect((await api.getServerSettings()).loreEnabled).toBe(false);
  });

  it("toggling PATCHes lore_enabled and reads it back durably", async () => {
    const patch = vi.spyOn(api, "patchServerSettings");
    const utils = await openExperimental();

    fireEvent.click(utils.getByTestId("settings-lore-enabled"));
    await waitFor(() =>
      expect(
        utils.getByTestId("settings-lore-enabled").getAttribute("aria-checked")
      ).toBe("true")
    );
    expect(patch).toHaveBeenCalledWith({ loreEnabled: true });
    expect((await api.getServerSettings()).loreEnabled).toBe(true);

    // And back off — the switch writes the negation of the server's value, so a
    // second press must not send `true` again.
    fireEvent.click(utils.getByTestId("settings-lore-enabled"));
    await waitFor(() =>
      expect(
        utils.getByTestId("settings-lore-enabled").getAttribute("aria-checked")
      ).toBe("false")
    );
    expect(patch).toHaveBeenLastCalledWith({ loreEnabled: false });
    expect((await api.getServerSettings()).loreEnabled).toBe(false);
    patch.mockRestore();
  });

  it("a rejected load shows the error instead of a fabricated switch", async () => {
    const get = vi
      .spyOn(api, "getServerSettings")
      .mockRejectedValue(new Error("boom"));
    const utils = renderSettings();
    fireEvent.click(utils.getByTestId("settings-experimental-entry"));

    await utils.findByText(s.experimentalLoadError);
    // 🔴 The switch must be ABSENT, not merely defaulted to off: an off switch
    // over a dead fetch is a claim that the station has lore disabled, which
    // this page did not learn and cannot know.
    expect(utils.queryByTestId("settings-lore-enabled")).toBeNull();
    get.mockRestore();
  });

  it("a rejected write leaves the switch on the server-confirmed value", async () => {
    const utils = await openExperimental();
    const patch = vi
      .spyOn(api, "patchServerSettings")
      .mockRejectedValue(new Error("nope"));

    fireEvent.click(utils.getByTestId("settings-lore-enabled"));
    await utils.findByText(s.experimentalSaveError);
    expect(
      utils.getByTestId("settings-lore-enabled").getAttribute("aria-checked")
    ).toBe("false");
    expect((await api.getServerSettings()).loreEnabled).toBe(false);
    patch.mockRestore();
  });
});
