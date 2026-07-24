// T-ea81 — NavIcon renders the active theme's per-tab icon image when present,
// and falls back to the built-in icon otherwise (a tab the theme omits keeps
// its built-in icon).

import { describe, it, expect, beforeEach } from "vitest";
import { render, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "../i18n";
import { NavIcon } from "./NavIcon";
import { OfficeIcon, TasksIcon } from "./icons";

function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}
const OFFICE_IMG =
  "data:image/png;base64," + b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0]);

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

describe("NavIcon", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("renders the built-in fallback icon (an svg, no img) under the office theme", () => {
    const { container } = render(
      <I18nProvider>
        <Capture />
        <NavIcon tabKey="office" fallback={<OfficeIcon size={15} />} />
      </I18nProvider>
    );
    expect(container.querySelector("img.nav-tab__icon-img")).toBeNull();
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("renders the theme icon image only for a tab the theme provides", () => {
    const { getByTestId } = render(
      <I18nProvider>
        <Capture />
        <div data-testid="office">
          <NavIcon tabKey="office" fallback={<OfficeIcon size={15} />} />
        </div>
        <div data-testid="tasks">
          <NavIcon tabKey="tasks" fallback={<TasksIcon size={15} />} />
        </div>
      </I18nProvider>
    );
    act(() => {
      ctx.commitCustomThemes([
        {
          id: "icons",
          name: "Icons",
          colors: { "--color-bg": "#101018" },
          navIcons: { office: OFFICE_IMG },
        },
      ]);
      ctx.setTheme("icons");
    });
    expect(
      getByTestId("office").querySelector("img.nav-tab__icon-img")?.getAttribute("src")
    ).toBe(OFFICE_IMG);
    // tasks has no themed icon → built-in fallback svg, no img
    expect(getByTestId("tasks").querySelector("img.nav-tab__icon-img")).toBeNull();
    expect(getByTestId("tasks").querySelector("svg")).not.toBeNull();
  });
});
