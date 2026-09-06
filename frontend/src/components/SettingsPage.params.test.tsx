// 設定 › 參數調整 (moved here from the profile dropdown, owner 2026-07-12):
// the /api/settings knobs — 登入有效期 dropdown + 自動換手門檻 number — live in
// their own settings-page view behind a landing entry below 角色誌. Same api
// seam as before (mock adapter here; validation parity with the server); the
// PATCH echo is adopted, an invalid % snaps back to the server-confirmed value.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";

const s = zh.settings;

/** Render Settings and navigate landing → 參數調整 (async settings load). */
async function openParams() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(s.sessionTtl); // settings loaded
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · 參數調整", () => {
  it("the landing lists the params entry after 角色誌", () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    const text = utils.container.textContent ?? "";
    expect(text.indexOf(s.params)).toBeGreaterThan(text.indexOf(s.roles));
  });

  it("shows the current settings once the view opens", async () => {
    const utils = await openParams();
    const select = utils.getByLabelText(s.sessionTtl) as HTMLSelectElement;
    expect(select.value).toBe("86400");
    const pct = utils.getByLabelText(s.handover) as HTMLInputElement;
    expect(pct.value).toBe("50");
    expect((utils.getByLabelText(s.monitoringRefresh) as HTMLInputElement).value).toBe("5");
    // 加速停止 的秒數 (T-ed79). The whole premise of that button is that the
    // number is the owner's, and docs/guide/members.md sends him HERE to change
    // it — until this row existed the only way to move it was the API.
    expect((utils.getByLabelText(s.acceleratedGrace) as HTMLInputElement).value).toBe("120");
    // 機器憑證壽命 (T-fc53). The shipped 30 days must be readable HERE: a fleet
    // renews on this number whether or not anyone ever opened the API, so the
    // page has to show what the machines are actually running on.
    expect((utils.getByLabelText(s.wardenCredentialLifetime) as HTMLInputElement).value).toBe("2592000");
  });

  it("changing the login TTL patches the server immediately", async () => {
    const utils = await openParams();
    fireEvent.change(utils.getByLabelText(s.sessionTtl), {
      target: { value: "604800" },
    });
    await waitFor(async () => {
      const settings = await api.getServerSettings();
      expect(settings.ownerTokenTtl).toBe(604800);
      expect(settings.agentTokenTtl).toBe(604800);
    });
  });

  it("changing the agent TTL leaves the login TTL unchanged", async () => {
    const utils = await openParams();
    fireEvent.change(utils.getByLabelText(s.agentTokenTtl), { target: { value: "2592000" } });
    await waitFor(async () => {
      const settings = await api.getServerSettings();
      expect(settings.agentTokenTtl).toBe(2592000);
      expect(settings.ownerTokenTtl).toBe(86400);
    });
  });

  it("commits a valid handover threshold and snaps back an invalid one", async () => {
    const utils = await openParams();
    const pct = utils.getByLabelText(s.handover) as HTMLInputElement;

    fireEvent.change(pct, { target: { value: "70" } });
    fireEvent.blur(pct);
    await waitFor(async () => {
      expect((await api.getServerSettings()).handoverPct).toBe(70);
    });

    // Below the warn band (40) — local guard snaps back, nothing written.
    fireEvent.change(pct, { target: { value: "20" } });
    fireEvent.blur(pct);
    await utils.findByText(s.paramsSaveError);
    expect(pct.value).toBe("70");
    expect((await api.getServerSettings()).handoverPct).toBe(70);
  });

  it("persists the 加速停止 grace and refuses a value the server would 422", async () => {
    const patch = vi.spyOn(api, "patchServerSettings");
    const utils = await openParams();
    const secs = utils.getByLabelText(s.acceleratedGrace) as HTMLInputElement;
    fireEvent.change(secs, { target: { value: "300" } });
    fireEvent.blur(secs);
    await waitFor(async () =>
      expect((await api.getServerSettings()).acceleratedGraceSecs).toBe(300),
    );
    expect(patch).toHaveBeenCalledWith({ acceleratedGraceSecs: 300 });
    // The local guard mirrors settings.go's 10..3600 exactly: a value the server
    // would reject must never leave the page, or the owner reads a 422 for a
    // number the UI offered him.
    fireEvent.change(secs, { target: { value: "5" } });
    fireEvent.blur(secs);
    await utils.findByText(s.paramsSaveError);
    expect((await api.getServerSettings()).acceleratedGraceSecs).toBe(300);
    expect(patch).toHaveBeenCalledTimes(1);
    patch.mockRestore();
  });

  it("persists the 機器憑證壽命 and refuses a value the server would 422", async () => {
    const patch = vi.spyOn(api, "patchServerSettings");
    const utils = await openParams();
    const secs = utils.getByLabelText(s.wardenCredentialLifetime) as HTMLInputElement;
    fireEvent.change(secs, { target: { value: "604800" } });
    fireEvent.blur(secs);
    await waitFor(async () =>
      expect((await api.getServerSettings()).wardenCredentialLifetimeSecs).toBe(604800),
    );
    expect(patch).toHaveBeenCalledWith({ wardenCredentialLifetimeSecs: 604800 });
    // Below the one-day floor: the local guard mirrors the server's 422 range
    // exactly, so a number the server would reject never leaves the page.
    fireEvent.change(secs, { target: { value: "3600" } });
    fireEvent.blur(secs);
    await utils.findByText(s.paramsSaveError);
    expect((await api.getServerSettings()).wardenCredentialLifetimeSecs).toBe(604800);
    expect(patch).toHaveBeenCalledTimes(1);
    patch.mockRestore();
  });

  it("persists the monitoring refresh interval and rejects zero", async () => {
    const patch = vi.spyOn(api, "patchServerSettings");
    const utils = await openParams();
    const seconds = utils.getByLabelText(s.monitoringRefresh) as HTMLInputElement;
    fireEvent.change(seconds, { target: { value: "12" } });
    fireEvent.blur(seconds);
    await waitFor(async () => expect((await api.getServerSettings()).monitoringRefreshSeconds).toBe(12));
    expect(patch).toHaveBeenCalledWith({ monitoringRefreshSeconds: 12 });
    fireEvent.change(seconds, { target: { value: "0" } });
    fireEvent.blur(seconds);
    await utils.findByText(s.paramsSaveError);
    expect((await api.getServerSettings()).monitoringRefreshSeconds).toBe(12);
    expect(patch).toHaveBeenCalledTimes(1);
    patch.mockRestore();
  });
});
