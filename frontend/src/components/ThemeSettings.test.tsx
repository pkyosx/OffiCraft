// ThemeSettings (T-16a1 P3b): the 設定/主題 management surface that theme
// management MOVED to from the profile dropdown. Covers import (moved verbatim
// + injection block), friendly grouped colour editing, and the 用詞 (wording)
// overlay editor round-trip.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, within, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ThemeSettings } from "./ThemeSettings";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { setToken, clearToken } from "../api/auth";

const p = zh.profile;
const s = zh.settings;

// Let the provider's mount reconcile (getServerSettings) settle BEFORE we touch
// the custom-theme set — otherwise its late-resolving GET overwrites an import
// with the (still-empty) server value.
async function renderManage() {
  let utils!: ReturnType<typeof render>;
  await act(async () => {
    utils = render(
      <I18nProvider>
        <ThemeSettings crumbs={[{ label: zh.settings.title }]} />
      </I18nProvider>
    );
    await new Promise((r) => setTimeout(r, 0));
  });
  return utils;
}

beforeEach(() => {
  __resetMock();
  clearToken();
  document.documentElement.removeAttribute("style");
  delete document.documentElement.dataset.theme;
});

async function importBundle(
  utils: Awaited<ReturnType<typeof renderManage>>,
  bundle: unknown
) {
  fireEvent.click(utils.getByText(p.themeImport));
  fireEvent.change(utils.getByLabelText(p.themeImportTitle), {
    target: { value: JSON.stringify(bundle) },
  });
  fireEvent.click(utils.getByText(p.themeConfirmImport));
}

describe("ThemeSettings · import", () => {
  it("imports a pasted bundle, lists it, and lands it on the server", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    expect(await utils.findByText("午夜藍")).toBeTruthy();
    const srv = await api.getServerSettings();
    expect(srv.customThemes.map((b) => b.id)).toContain("midnight");
  });

  it("blocks an injection-shaped bundle inline and never reaches the server", async () => {
    const utils = await renderManage();
    await importBundle(utils, {
      id: "evil",
      name: "Evil",
      colors: { "--color-bg": "red; } body { background: url(x)" },
    });
    expect(utils.getByLabelText(p.themeImportTitle)).toBeTruthy();
    const srv = await api.getServerSettings();
    expect(srv.customThemes).toHaveLength(0);
  });
});

describe("ThemeSettings · colour editing", () => {
  it("shows friendly names grouped by purpose — never the raw --color-* token", async () => {
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020", "--color-bg": "#040506" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    // The friendly group + label are shown; the raw token is not visible text.
    const colorSection = utils.container.querySelector(".ts-color-group__label");
    expect(colorSection?.textContent).toBe("主色"); // brand group heading
    expect(utils.getAllByText("主色").length).toBeGreaterThan(0); // group + accent label
    expect(utils.queryByText("--color-accent")).toBeNull();
  });

  it("round-trips an edited colour value through save", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));
    // The value text field carries the friendly label as its accessible name.
    fireEvent.change(utils.getByLabelText("主色"), {
      target: { value: "#ffffff" },
    });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
    expect(b?.colors["--color-accent"]).toBe("#ffffff");
  });
});

describe("ThemeSettings · wording overlay", () => {
  it("stores a wording override and lands it on the server bundle", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    // Narrow the (large) wording list to exactly one code by searching the code.
    fireEvent.change(utils.getByLabelText(s.themeWordingSearch), {
      target: { value: "common.apply" },
    });
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    const input = within(list).getByRole("textbox");
    fireEvent.change(input, { target: { value: "套用替代" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
    expect(b?.wording?.zh?.["common.apply"]).toBe("套用替代");
  });
});

describe("ThemeSettings · delete", () => {
  it("deletes a custom theme via the confirm modal", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeDelete} 午夜藍`));
    fireEvent.click(utils.getByTestId("theme-delete-confirm-btn"));

    expect(utils.queryByText("午夜藍")).toBeNull();
    const srv = await api.getServerSettings();
    expect(srv.customThemes).toHaveLength(0);
  });
});
