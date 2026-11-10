// ProfileDropdown change-password (B3): the main menu owns the account
// sub-views while preferences keeps only appearance controls.
//
// T-83ef: themes are their own resource, so the seeding below writes them ONE
// AT A TIME through api.putTheme; the status line reads `themeList` (id + name).
//
// The /api/settings parameter knobs (登入有效期 / 自動換手門檻) MOVED to the
// 設定 page's 參數調整 entry (owner 2026-07-12), and theme MANAGEMENT (import /
// export / edit / delete) MOVED to 設定/主題 (T-16a1 P3b →
// ThemeSettings.test.tsx), and so did choosing one (owner 2026-11-05): the
// dropdown only names the theme on screen.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { useState } from "react";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import type { LoadableSeasonalWindow } from "../lib/seasonalTheme";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { makeMessages } from "../i18n/compose";
import { ProfileDropdown } from "./ProfileDropdown";
import { __resetMock, __setMockNoAppearance } from "../api/mock";
import { api } from "../api";

const seasonalWindows = vi.hoisted(() => [] as LoadableSeasonalWindow[]);
vi.mock("../lib/seasonalTheme", () => ({ SEASONAL_WINDOWS: seasonalWindows }));
import { setToken, clearToken } from "../api/auth";

const p = zh.profile;

/** The menu with its trigger, the way App mounts it: onClose really closes it. */
function MenuWithTrigger() {
  const [open, setOpen] = useState(true);
  return (
    <div>
      <button type="button" aria-haspopup="menu" onClick={() => setOpen((o) => !o)}>
        pill
      </button>
      <ProfileDropdown
        open={open}
        onClose={() => setOpen(false)}
        userName="使用者"
        setOwnerName={vi.fn()}
      />
    </div>
  );
}

async function openPreferences() {
  const utils = render(
    <I18nProvider>
      <MenuWithTrigger />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(p.preferences));
  await utils.findByText(p.theme);
  return utils;
}

beforeEach(() => {
  __resetMock();
  __setMockNoAppearance();
  clearToken();
  // Some cases seed oc.theme in localStorage; clear it so a
  // later test's first paint is not tinted (and stays on the zh default dict).
  localStorage.clear();
  delete document.documentElement.dataset.theme;
  delete document.documentElement.dataset.layout;
});

describe("ProfileDropdown · preferences scope", () => {
  const themeStatus = (utils: ReturnType<typeof render>) => {
    const entry = utils.getByTestId("theme-entry");
    return [
      within(entry).getByTestId("theme-entry-name").textContent,
      within(entry).queryByTestId("theme-entry-tag")?.textContent,
    ]
      .filter(Boolean)
      .join(" · ");
  };

  it("renders the theme status and language", async () => {
    const utils = await openPreferences();
    expect(utils.getByText(p.theme)).toBeTruthy();
    expect(utils.getByText(p.language)).toBeTruthy();
  });

  it("names the selected theme in one row; the row closes the menu and opens the dialog that sets it, and closing returns focus to the menu's trigger", async () => {
    setToken("owner-token");
    await api.putTheme({
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-bg": "#101018" },
    });
    await api.patchServerSettings({ displayTheme: "midnight" });
    const utils = await openPreferences();
    await waitFor(() => expect(themeStatus(utils)).toBe("午夜藍"));

    fireEvent.click(
      utils.getByRole("button", { name: makeMessages(zh, "zh").themeEntryLabel("午夜藍") })
    );
    expect(utils.queryByRole("menu")).toBeNull();
    const dialog = utils.getByRole("dialog", { name: zh.settings.themeDialogTitle });
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    expect(document.activeElement).toBe(dialog);
    expect(within(dialog).getByRole("switch", { name: p.seasonalTheme })).toBeTruthy();
    expect(within(dialog).getByRole("combobox", { name: zh.settings.themeBgMotion })).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: zh.settings.themeRestoreDefault })).toBeTruthy();
    const select = within(dialog).getByRole("combobox", { name: zh.settings.themePick }) as HTMLSelectElement;
    expect(select.value).toBe("midnight");

    fireEvent.change(select, { target: { value: "office-light" } });
    await waitFor(async () =>
      expect((await api.getServerSettings()).displayTheme).toBe("office-light")
    );
    fireEvent.click(within(dialog).getByRole("switch", { name: zh.settings.themeSchedule }));
    fireEvent.click(within(dialog).getByRole("button", { name: zh.settings.themeScheduleSave }));
    await waitFor(async () =>
      expect((await api.getServerSettings()).displayThemeSchedule).toHaveLength(3)
    );

    fireEvent.keyDown(window, { key: "Escape" });
    expect(utils.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(utils.getByRole("button", { name: "pill" }));

    fireEvent.click(utils.getByRole("button", { name: "pill" }));
    fireEvent.click(utils.getByText(p.preferences));
    await waitFor(() => expect(themeStatus(utils)).toContain(p.themeEntryScheduled));
    fireEvent.click(utils.getByTestId("theme-entry"));
    const firstStart = async () =>
      (await within(utils.getByRole("dialog")).findByLabelText(
        `${zh.settings.themeScheduleStart} 1`
      )) as HTMLInputElement;
    fireEvent.change(await firstStart(), { target: { value: "07:15" } });
    fireEvent.click(
      within(utils.getByRole("dialog")).getByRole("button", { name: zh.settings.themeDialogClose })
    );
    expect(utils.queryByRole("dialog")).toBeNull();

    fireEvent.click(utils.getByRole("button", { name: "pill" }));
    fireEvent.click(utils.getByText(p.preferences));
    fireEvent.click(utils.getByTestId("theme-entry"));
    expect((await firstStart()).value).toBe("06:00");
  });

  it("names a built-in theme by its own identity name", async () => {
    localStorage.setItem("oc.theme", "office-light");
    const utils = await openPreferences();
    expect(themeStatus(utils)).toBe(zh.themeIdentity.officeLight);
  });

  it("while a theme schedule is on, says so and names the theme it shows now", async () => {
    await api.patchServerSettings({
      displayThemeSchedule: [
        { start: "00:00", theme: "office-light" },
        { start: "12:00", theme: "office-light" },
      ],
    });
    setToken("owner-token");
    const utils = await openPreferences();
    await waitFor(() =>
      expect(themeStatus(utils)).toBe(`${zh.themeIdentity.officeLight} · ${p.themeEntryScheduled}`)
    );
    expect(
      utils.getByRole("button", {
        name: makeMessages(zh, "zh").themeEntryLabel(zh.themeIdentity.officeLight, p.themeEntryScheduled),
      })
    ).toBeTruthy();
  });

  describe("inside a seasonal window", () => {
    afterEach(() => {
      seasonalWindows.length = 0;
      vi.useRealTimers();
    });

    it("names the seasonal theme on screen even while a theme schedule is on", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["Date", "setTimeout", "clearTimeout"] });
      vi.setSystemTime(new Date(2030, 9, 28, 12, 0));
      seasonalWindows.push({
        id: "harvest",
        start: new Date(2030, 9, 24, 0, 0),
        end: new Date(2030, 10, 1, 0, 0),
        load: () => Promise.resolve({ id: "harvest", name: "豐收", colors: { "--color-bg": "#15101f" } }),
      });
      await api.patchServerSettings({
        displayThemeSchedule: [
          { start: "00:00", theme: "office-light" },
          { start: "12:00", theme: "office" },
        ],
      });
      setToken("owner-token");
      const utils = await openPreferences();
      await waitFor(() => expect(themeStatus(utils)).toBe(`豐收 · ${p.themeEntrySeasonal}`));
    });
  });

  it("offers the 版面 segmented control and flips the layout both ways (T-756f)", async () => {
    const utils = await openPreferences();
    expect(utils.getByText(p.layout)).toBeTruthy();
    // Narrow is the default, so nothing is applied to <html> yet.
    expect(document.documentElement.hasAttribute("data-layout")).toBe(false);

    fireEvent.click(utils.getByText(p.layoutWide));
    expect(document.documentElement.dataset.layout).toBe("wide");

    fireEvent.click(utils.getByText(p.layoutNarrow));
    expect(document.documentElement.hasAttribute("data-layout")).toBe(false);
  });
});

describe("ProfileDropdown · change password", () => {
  it("changes the password through the seam and confirms inline", async () => {
    const utils = render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
    fireEvent.click(utils.getByText(p.accountSecurity));
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
    const utils = render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
    fireEvent.click(utils.getByText(p.accountSecurity));
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
    const utils = render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
    fireEvent.click(utils.getByText(p.accountSecurity));
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

describe("ProfileDropdown · notification email", () => {
  it("enables Save only for unsaved edits and disables it again after saving", async () => {
    const utils = render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
    fireEvent.click(utils.getByText(p.pushContactEmail));
    const input = await utils.findByLabelText(p.pushContactEmail);
    const save = utils.getByRole("button", { name: p.save });
    expect((save as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(input, { target: { value: "push@example.com" } });
    expect((save as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(save);
    await vi.waitFor(() => expect((save as HTMLButtonElement).disabled).toBe(true));
  });
});

describe("ProfileDropdown · menu levels", () => {
  function renderMenu() {
    return render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
  }
  const rowTexts = (root: HTMLElement) =>
    Array.from(root.querySelectorAll(".profile-dd__row")).map((r) => r.textContent);
  const backText = (root: HTMLElement) => root.querySelector(".profile-dd__back")?.textContent;

  it("the first level holds preferences, notification email and account security", () => {
    const utils = renderMenu();
    expect(rowTexts(utils.container)).toEqual([
      "偏好設定外觀、語言、版面",
      "通知信箱填寫後才會送出通知。這個信箱會提供給瀏覽器的推播服務，請用可公開的信箱。",
      "帳號安全修改密碼、兩步驟驗證、通行密鑰",
      "登出",
    ]);
  });

  it("account security opens a sub-list of password, two-step verification and passkeys", () => {
    const utils = renderMenu();
    fireEvent.click(utils.getByText("帳號安全"));
    expect(backText(utils.container)).toBe("帳號安全");
    expect(rowTexts(utils.container)).toEqual([
      "修改密碼登入這個主控台用的密碼",
      "兩步驟驗證",
      "通行密鑰用手機或電腦本身的解鎖方式登入，例如 Face ID、Touch ID、指紋或裝置密碼",
    ]);
  });

  it("each security screen's Back returns to the sub-list, and the sub-list's Back to the first level", async () => {
    const utils = renderMenu();
    fireEvent.click(utils.getByText("帳號安全"));
    for (const entry of ["修改密碼", "兩步驟驗證", "通行密鑰"]) {
      fireEvent.click(utils.getByText(entry));
      expect(backText(utils.container)).toBe(entry);
      fireEvent.click(utils.container.querySelector(".profile-dd__back")!);
      expect(backText(utils.container)).toBe("帳號安全");
    }
    fireEvent.click(utils.container.querySelector(".profile-dd__back")!);
    expect(backText(utils.container)).toBeUndefined();
    expect(utils.getByText("偏好設定")).toBeTruthy();
    await utils.findByText("帳號安全");
  });
});
