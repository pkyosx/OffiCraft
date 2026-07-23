// ProfileDropdown change-password (B3): the preferences sub-view keeps the
// theme SELECTOR + language + a 修改密碼 sub-view — through the api seam (mock
// adapter here; validation parity with the server).
//
// The /api/settings parameter knobs (登入有效期 / 自動換手門檻) MOVED to the
// 設定 page's 參數調整 entry (owner 2026-07-12), and theme MANAGEMENT (import /
// export / edit / delete) MOVED to 設定/主題 (T-16a1 P3b →
// ThemeSettings.test.tsx). Here we pin that the dropdown kept only selection.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ProfileDropdown } from "./ProfileDropdown";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { clearToken } from "../api/auth";

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
  // The theme-selector test writes oc.theme to localStorage; clear it so a
  // later test's first paint is not tinted (and stays on the zh default dict).
  localStorage.clear();
  delete document.documentElement.dataset.theme;
});

describe("ProfileDropdown · preferences scope", () => {
  it("no longer renders the server parameter knobs (they live in 設定/參數調整)", async () => {
    const utils = await openPreferences();
    const text = utils.container.textContent ?? "";
    expect(text).not.toContain(zh.settings.sessionTtl);
    expect(text).not.toContain(zh.settings.handover);
    // Theme selector + language + password remain.
    expect(utils.getByText(p.theme)).toBeTruthy();
    expect(utils.getByText(p.language)).toBeTruthy();
  });

  it("keeps only the theme SELECTOR — no management affordances (moved to 設定/主題)", async () => {
    const utils = await openPreferences();
    // The built-in selection row is present (office is the only built-in).
    expect(utils.getByText(p.themeOffice)).toBeTruthy();
    // Management chips no longer live in the quick menu.
    expect(utils.queryByText(p.themeConfirmImport)).toBeNull();
    // A hint points the owner to the settings page instead.
    expect(utils.getByText(p.themeManageHint)).toBeTruthy();
  });

  it("selects the built-in office theme from the quick picker", async () => {
    const utils = await openPreferences();
    fireEvent.click(utils.getByText(p.themeOffice));
    expect(document.documentElement.dataset.theme).toBe("office");
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
