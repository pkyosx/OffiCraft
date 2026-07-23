// ProfileDropdown change-password (B3): the preferences sub-view keeps
// appearance (theme/language) + a 修改密碼 sub-view — through the api seam
// (mock adapter here; validation parity with the server).
//
// The /api/settings parameter knobs (登入有效期 / 自動換手門檻) MOVED to the
// 設定 page's 參數調整 entry (owner 2026-07-12) — their coverage lives in
// SettingsPage.params.test.tsx; here we pin that the dropdown stayed clean.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ProfileDropdown } from "./ProfileDropdown";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { setToken, clearToken } from "../api/auth";

const p = zh.profile;

async function openPreferences() {
  const utils = render(
    <I18nProvider>
      <ProfileDropdown
        open
        onClose={vi.fn()}
        userName="使用者"
        setOwnerName={vi.fn()}
      />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(p.preferences));
  await utils.findByText(p.changePassword);
  return utils;
}

beforeEach(() => {
  __resetMock();
  clearToken();
});

describe("ProfileDropdown · preferences scope", () => {
  it("no longer renders the server parameter knobs (they live in 設定/參數調整)", async () => {
    const utils = await openPreferences();
    const text = utils.container.textContent ?? "";
    expect(text).not.toContain(zh.settings.sessionTtl);
    expect(text).not.toContain(zh.settings.handover);
    // Appearance + password remain.
    expect(utils.getByText(p.theme)).toBeTruthy();
    expect(utils.getByText(p.language)).toBeTruthy();
  });
});

describe("ProfileDropdown · theme picker", () => {
  it("imports a pasted bundle and lists it as a selectable custom theme", async () => {
    setToken("owner-token"); // enable the server-sync path in commitCustomThemes
    const utils = await openPreferences();
    fireEvent.click(utils.getByText(p.themeImport));
    fireEvent.change(utils.getByLabelText(p.themeImportTitle), {
      target: {
        value: JSON.stringify({
          id: "midnight",
          name: "午夜藍",
          colors: { "--color-accent": "#0b1020" },
        }),
      },
    });
    fireEvent.click(utils.getByText(p.themeConfirmImport));
    // Back on the list, the imported theme is now a row.
    const row = await utils.findByText("午夜藍");
    expect(row).toBeTruthy();
    // It reached the server through the seam.
    const s = await api.getServerSettings();
    expect(s.customThemes.map((b) => b.id)).toContain("midnight");
  });

  it("blocks an injection-shaped bundle inline and never reaches the server", async () => {
    const utils = await openPreferences();
    fireEvent.click(utils.getByText(p.themeImport));
    fireEvent.change(utils.getByLabelText(p.themeImportTitle), {
      target: {
        value: JSON.stringify({
          id: "evil",
          name: "Evil",
          colors: { "--color-bg": "red; } body { background: url(x)" },
        }),
      },
    });
    fireEvent.click(utils.getByText(p.themeConfirmImport));
    // Stays on the import view with an error; no custom theme persisted.
    expect(utils.getByLabelText(p.themeImportTitle)).toBeTruthy();
    const s = await api.getServerSettings();
    expect(s.customThemes).toHaveLength(0);
  });
});

describe("ProfileDropdown · change password", () => {
  it("changes the password through the seam and confirms inline", async () => {
    const utils = await openPreferences();
    fireEvent.click(utils.getByText(p.changePassword));
    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: "mock-password" },
    });
    fireEvent.change(utils.getByLabelText(p.newPasswordPlaceholder), {
      target: { value: "next-password" },
    });
    fireEvent.change(utils.getByLabelText(p.confirmPasswordPlaceholder), {
      target: { value: "next-password" },
    });
    fireEvent.click(utils.getByText(p.save));
    await utils.findByText(p.pwdChanged);
    // The mock credential really rotated: the old current password now fails.
    await expect(api.changePassword("mock-password", "another-pass-1")).rejects.toThrow();
    await expect(api.changePassword("next-password", "another-pass-1")).resolves.toBeUndefined();
  });

  it("keeps a wrong current password an inline error (no logout bounce)", async () => {
    const utils = await openPreferences();
    fireEvent.click(utils.getByText(p.changePassword));
    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: "wrong-password" },
    });
    fireEvent.change(utils.getByLabelText(p.newPasswordPlaceholder), {
      target: { value: "next-password" },
    });
    fireEvent.change(utils.getByLabelText(p.confirmPasswordPlaceholder), {
      target: { value: "next-password" },
    });
    fireEvent.click(utils.getByText(p.save));
    await utils.findByText(p.pwdErrorCurrent);
  });

  it("rejects a short or mismatched new password locally", async () => {
    const utils = await openPreferences();
    fireEvent.click(utils.getByText(p.changePassword));
    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: "mock-password" },
    });
    fireEvent.change(utils.getByLabelText(p.newPasswordPlaceholder), {
      target: { value: "short" },
    });
    fireEvent.change(utils.getByLabelText(p.confirmPasswordPlaceholder), {
      target: { value: "short" },
    });
    fireEvent.click(utils.getByText(p.save));
    await utils.findByText(p.pwdErrorTooShort);

    fireEvent.change(utils.getByLabelText(p.newPasswordPlaceholder), {
      target: { value: "long-enough-pass" },
    });
    fireEvent.change(utils.getByLabelText(p.confirmPasswordPlaceholder), {
      target: { value: "different-pass" },
    });
    fireEvent.click(utils.getByText(p.save));
    await utils.findByText(p.pwdErrorMismatch);
  });
});
