// T-ea81 — BrandLogo renders the active theme's studio logo image when present,
// and falls back to the built-in LogoMark (an <svg>) otherwise.

import { describe, it, expect, beforeEach } from "vitest";
import { render, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "../i18n";
import { BrandLogo } from "./BrandLogo";

function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}
const LOGO_IMG =
  "data:image/png;base64," + b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0]);

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

describe("BrandLogo", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("renders the built-in LogoMark (an svg, no img) under the office theme", () => {
    const { container } = render(
      <I18nProvider>
        <Capture />
        <BrandLogo size={20} />
      </I18nProvider>
    );
    expect(container.querySelector("img.topbar__logo-img")).toBeNull();
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("renders the theme logo image when the active theme carries one", () => {
    const { container } = render(
      <I18nProvider>
        <Capture />
        <BrandLogo size={20} />
      </I18nProvider>
    );
    act(() => {
      ctx.commitCustomThemes([
        {
          id: "branded",
          name: "Branded",
          colors: { "--color-bg": "#101018" },
          logo: LOGO_IMG,
        },
      ]);
      ctx.setTheme("branded");
    });
    expect(
      container.querySelector("img.topbar__logo-img")?.getAttribute("src")
    ).toBe(LOGO_IMG);
  });
});
