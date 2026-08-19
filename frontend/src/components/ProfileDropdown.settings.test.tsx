// ProfileDropdown change-password (B3): the main menu owns the account
// sub-views while preferences keeps only appearance controls.
//
// T-83ef: the themes the picker lists no longer ride on /api/settings — they
// are their own resource, so the seeding below writes them ONE AT A TIME
// through api.putTheme. The picker itself reads `themeList` (id + name), which
// is all it ever rendered.
//
// The /api/settings parameter knobs (登入有效期 / 自動換手門檻) MOVED to the
// 設定 page's 參數調整 entry (owner 2026-07-12), and theme MANAGEMENT (import /
// export / edit / delete) MOVED to 設定/主題 (T-16a1 P3b →
// ThemeSettings.test.tsx). Here we pin that the dropdown kept only selection.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
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
  await utils.findByText(p.theme);
  return utils;
}

beforeEach(() => {
  __resetMock();
  clearToken();
  // The theme-selector test writes oc.theme to localStorage; clear it so a
  // later test's first paint is not tinted (and stays on the zh default dict).
  localStorage.clear();
  delete document.documentElement.dataset.theme;
  delete document.documentElement.dataset.layout;
});

describe("ProfileDropdown · preferences scope", () => {
  it("no longer renders the server parameter knobs (they live in 設定/參數調整)", async () => {
    const utils = await openPreferences();
    const text = utils.container.textContent ?? "";
    expect(text).not.toContain(zh.settings.sessionTtl);
    expect(text).not.toContain(zh.settings.handover);
    // Theme selector + language remain.
    expect(utils.getByText(p.theme)).toBeTruthy();
    expect(utils.getByText(p.language)).toBeTruthy();
  });

  it("keeps only the theme SELECTOR — no management affordances (moved to 設定/主題)", async () => {
    setToken("owner-token");
    await api.putTheme({
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-bg": "#101018" },
    });
    const utils = await openPreferences();
    const select = utils.getByLabelText(p.theme);
    const custom = await waitFor(() => {
      const o = Array.from(select.querySelectorAll("option")).find(
        (x) => x.value === "midnight"
      );
      expect(o).toBeTruthy();
      return o!;
    });
    // A flat list (owner 2026-07-27: no 分區 in the quick picker) — every
    // option's text is the theme's own name and nothing else, and the 內建 /
    // 自訂 marking lives in 設定 › 主題 (ThemeSettings.test.tsx).
    expect(select.querySelectorAll("optgroup").length).toBe(0);
    const builtin = Array.from(select.querySelectorAll("option")).find(
      (o) => o.value === "office"
    )!;
    expect(builtin.textContent).toBe(zh.themeIdentity.office);
    expect(custom.textContent).toBe("午夜藍");
    // Management chips no longer live in the quick menu.
    expect(utils.queryByText(p.themeConfirmImport)).toBeNull();
    // A hint points the owner to the settings page instead.
    expect(utils.getByText(p.themeManageHint)).toBeTruthy();
  });

  it("cannot be made to show two identical built-in rows by a theme's NAME", async () => {
    // The owner's original symptom, re-entered through the other door (T-081b
    // review round 3, BLOCKER-2): while the marker was TEXT — 「辦公室」 + 「(內建)」 —
    // a pack simply naming itself 「辦公室(內建)」 produced two byte-identical rows
    // and the shipped theme became unfindable again. Both names are legal (neither
    // equals the reserved 「辦公室」), so the fix cannot be a name rule: the marker
    // has to be structure the name cannot reach.
    setToken("owner-token");
    const spoof = `${zh.themeIdentity.office}(${zh.themeMarkers.builtinGroup})`;
    await api.putTheme({
      id: "spoofpack",
      name: spoof,
      colors: { "--color-bg": "#101018" },
    });
    const utils = await openPreferences();
    const select = utils.getByLabelText(p.theme);
    await waitFor(() => {
      expect(
        Array.from(select.querySelectorAll("option")).some((o) => o.value === "spoofpack")
      ).toBe(true);
    });
    const options = Array.from(select.querySelectorAll("option"));
    const builtin = options.find((o) => o.value === "office")!;
    const forged = options.find((o) => o.value === "spoofpack")!;
    // The spoof keeps its own name (it is legal), but it is NOT the built-in row:
    // the built-in's text is the bare identity name, so no pack name can be
    // byte-identical to it while also carrying a 內建 marker — the picker prints
    // no marker at all.
    expect(forged.textContent).toBe(spoof);
    expect(builtin.textContent).toBe(zh.themeIdentity.office);
    expect(builtin.textContent).not.toBe(forged.textContent);
    expect(select.textContent).not.toContain(
      `${zh.themeIdentity.office}${zh.themeMarkers.builtinGroup}`
    );
    // And the pack cannot buy the built-in's place in the list by its name.
    expect(options.indexOf(builtin)).toBe(0);
  });

  it("keeps the built-in first and the packs after, whatever they are named", async () => {
    // The one thing the flat picker still asserts is ORDER (owner 2026-07-27).
    // It has to come from the rendering, not from the data: neither a name that
    // sorts first nor the order the packs were imported in may push a pack
    // ahead of the built-in.
    setToken("owner-token");
    // Saved one at a time — that is the only door there is now. The ORDER the
    // picker must respect is the order the server keeps them in, which is the
    // order they were created in, so seeding them in sequence is also what
    // makes the assertion below about the render rather than about the data.
    await api.putTheme({
      id: "aaa",
      name: "AAA 最前面",
      colors: { "--color-bg": "#101018" },
    });
    await api.putTheme({
      id: "zzz",
      name: "000 更前面",
      colors: { "--color-bg": "#181018" },
    });
    const utils = await openPreferences();
    const select = utils.getByLabelText(p.theme);
    await waitFor(() => {
      expect(select.querySelectorAll("option").length).toBe(3);
    });
    expect(
      Array.from(select.querySelectorAll("option")).map((o) => o.value)
    ).toEqual(["office", "aaa", "zzz"]);
  });

  it("selects the built-in office theme from the quick picker", async () => {
    const utils = await openPreferences();
    fireEvent.change(utils.getByLabelText(p.theme), { target: { value: "office" } });
    expect(document.documentElement.dataset.theme).toBe("office");
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
