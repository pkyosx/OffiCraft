// T-16a1 P5 — the Avatar picks a per-member-type image off the ACTIVE theme,
// and falls back to the built-in glyph when the theme carries none (office
// never degrades). jsdom is enough: this is DOM-shape logic (which node renders
// for a given kind), not geometry.

import { describe, it, expect, beforeEach } from "vitest";
import { render, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "../i18n";
import { Avatar } from "./Avatar";

// Two tiny-but-valid base64 rasters (magic bytes only — enough to pass the
// shared validator, which the ThemeSettings upload path enforces before these
// ever reach a bundle).
function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}
const MEMBER_IMG =
  "data:image/png;base64," + b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0]);
const OUTSOURCE_IMG =
  "data:image/webp;base64," +
  b64([0x52, 0x49, 0x46, 0x46, 0x10, 0, 0, 0, 0x57, 0x45, 0x42, 0x50, 0]);

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

describe("Avatar avatars-by-kind (T-16a1 P5)", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("renders the built-in glyph (no <img>) under the office theme", () => {
    const { container } = render(
      <I18nProvider>
        <Capture />
        <Avatar size={40} kind="member" />
      </I18nProvider>
    );
    expect(container.querySelector("img.avatar__img")).toBeNull();
    // the fallback UserIcon is an <svg>
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("selects the member image for kind=member and the outsource image for kind=outsource", () => {
    const { getByTestId } = render(
      <I18nProvider>
        <Capture />
        <div data-testid="member">
          <Avatar size={40} kind="member" />
        </div>
        <div data-testid="outsource">
          <Avatar size={40} kind="outsource" />
        </div>
      </I18nProvider>
    );
    act(() => {
      ctx.commitCustomThemes([
        {
          id: "portraits",
          name: "Portraits",
          colors: { "--color-bg": "#101018" },
          avatars: { member: MEMBER_IMG, outsource: OUTSOURCE_IMG },
        },
      ]);
      ctx.setTheme("portraits");
    });
    expect(
      getByTestId("member").querySelector("img.avatar__img")?.getAttribute("src")
    ).toBe(MEMBER_IMG);
    expect(
      getByTestId("outsource")
        .querySelector("img.avatar__img")
        ?.getAttribute("src")
    ).toBe(OUTSOURCE_IMG);
  });

  it("falls back per-kind: a theme with only a member image keeps the glyph for outsource", () => {
    const { getByTestId } = render(
      <I18nProvider>
        <Capture />
        <div data-testid="member">
          <Avatar size={40} kind="member" />
        </div>
        <div data-testid="outsource">
          <Avatar size={40} kind="outsource" />
        </div>
      </I18nProvider>
    );
    act(() => {
      ctx.commitCustomThemes([
        {
          id: "half",
          name: "Half",
          colors: { "--color-bg": "#101018" },
          avatars: { member: MEMBER_IMG },
        },
      ]);
      ctx.setTheme("half");
    });
    expect(
      getByTestId("member").querySelector("img.avatar__img")?.getAttribute("src")
    ).toBe(MEMBER_IMG);
    // outsource kind has no image on this theme → built-in glyph, no <img>
    expect(getByTestId("outsource").querySelector("img.avatar__img")).toBeNull();
    expect(getByTestId("outsource").querySelector("svg")).not.toBeNull();
  });
});
