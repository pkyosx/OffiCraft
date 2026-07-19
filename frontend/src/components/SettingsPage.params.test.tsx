// 設定 › 參數調整 (moved here from the profile dropdown, owner 2026-07-12):
// the /api/settings knobs — 登入有效期 dropdown + 自動換手門檻 number — live in
// their own settings-page view behind a landing entry below 角色誌. Same api
// seam as before (mock adapter here; validation parity with the server); the
// PATCH echo is adopted, an invalid % snaps back to the server-confirmed value.

import { describe, it, expect, beforeEach } from "vitest";
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
  });

  it("changing the login TTL patches the server immediately", async () => {
    const utils = await openParams();
    fireEvent.change(utils.getByLabelText(s.sessionTtl), {
      target: { value: "604800" },
    });
    await waitFor(async () => {
      expect((await api.getServerSettings()).tokenTtl).toBe(604800);
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
});
