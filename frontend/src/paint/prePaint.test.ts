import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LS_THEME, LS_THEME_PAINT, paintRecordFor } from "../lib/themePaint";
import { PAINT_THEME_ID, VALID_RICH_BUNDLE } from "../lib/paintFixtures";

async function runPrePaint() {
  vi.resetModules();
  await import("./prePaint");
}

describe("prePaint", () => {
  const root = document.documentElement;

  beforeEach(() => {
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    delete window.__ocPaintTokens;
  });
  afterEach(() => {
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    delete window.__ocPaintTokens;
  });

  for (const builtin of ["office", "office-light"]) {
    it(`a cached built-in ${builtin} sets data-theme and paints no inline tokens`, async () => {
      localStorage.setItem(LS_THEME, builtin);
      localStorage.setItem(LS_THEME_PAINT, JSON.stringify(paintRecordFor(VALID_RICH_BUNDLE)));

      await runPrePaint();

      expect(root.dataset.theme).toBe(builtin);
      expect(root.getAttribute("style")).toBeNull();
      expect(window.__ocPaintTokens).toBeUndefined();
    });
  }

  it("a cached custom theme paints its record over the office base", async () => {
    localStorage.setItem(LS_THEME, PAINT_THEME_ID);
    localStorage.setItem(LS_THEME_PAINT, JSON.stringify(paintRecordFor(VALID_RICH_BUNDLE)));

    await runPrePaint();

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe(VALID_RICH_BUNDLE.colors["--color-bg"]);
    expect(window.__ocPaintTokens).toContain("--color-bg");
  });

  it("an unknown stored id leaves the document untouched", async () => {
    localStorage.setItem(LS_THEME, "ghost");

    await runPrePaint();

    expect(root.hasAttribute("data-theme")).toBe(false);
    expect(root.getAttribute("style")).toBeNull();
  });
});
