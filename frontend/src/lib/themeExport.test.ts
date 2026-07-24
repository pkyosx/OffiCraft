import { afterEach, describe, expect, it } from "vitest";
import {
  bundleFilename,
  exportComputedTheme,
  exportOfficeBaseTheme,
  nextCustomThemeId,
  parseImportedBundle,
  serializeBundle,
} from "./themeExport";

function freshRoot(): HTMLElement {
  const el = document.createElement("div");
  document.body.appendChild(el);
  return el;
}

afterEach(() => {
  document.body.innerHTML = "";
  document.documentElement.removeAttribute("style");
});

describe("exportComputedTheme", () => {
  it("packs the resolved value of each --color-* token set on the element", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#123456");
    el.style.setProperty("--color-bg", "rgb(10, 20, 30)");

    const bundle = exportComputedTheme("mine", "Mine", el);

    expect(bundle.id).toBe("mine");
    expect(bundle.name).toBe("Mine");
    expect(bundle.colors["--color-accent"]).toBe("#123456");
    expect(bundle.colors["--color-bg"]).toBe("rgb(10, 20, 30)");
  });

  it("omits tokens with no value and tokens that resolve to a non-concrete colour", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#abcabc");
    // an unresolved color-mix() must never poison the exported bundle
    el.style.setProperty("--color-bg", "color-mix(in srgb, red, blue)");

    const bundle = exportComputedTheme("mine", "Mine", el);

    expect(bundle.colors["--color-accent"]).toBe("#abcabc");
    expect(bundle.colors).not.toHaveProperty("--color-bg");
    expect(bundle.colors).not.toHaveProperty("--color-text");
  });

  it("produces a bundle that re-imports without loss", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#0af");

    const round = parseImportedBundle(
      serializeBundle(exportComputedTheme("mine", "Mine", el))
    );

    expect("bundle" in round).toBe(true);
  });
});

describe("exportOfficeBaseTheme", () => {
  it("reads through an active theme's inline overrides and restores them", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#111111"); // theme.css :root default stand-in
    // an "active custom theme" override is layered on top
    el.style.setProperty("--color-accent", "#abcdef");

    const bundle = exportOfficeBaseTheme("custom-1", "New theme", el);

    // exportOfficeBaseTheme strips the inline override to read the base; in jsdom
    // there is no stylesheet base, so the stripped token drops out entirely —
    // the point under test is that the override is gone during the read and put
    // BACK afterwards (no permanent mutation of the live element).
    expect(bundle.id).toBe("custom-1");
    expect(bundle.name).toBe("New theme");
    expect(el.style.getPropertyValue("--color-accent")).toBe("#abcdef");
  });

  it("uses a non-reserved id ('office-base') so its download re-imports — unlike 'office'", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#0af");
    // The office 列下載 path exports under id "office-base"; the base read is
    // simulated here (jsdom has no stylesheet base) via exportComputedTheme.
    const round = parseImportedBundle(
      serializeBundle(exportComputedTheme("office-base", "辦公室", el))
    );
    expect("bundle" in round).toBe(true);
    // The reserved built-in id would be rejected on re-import.
    expect("error" in parseImportedBundle(
      serializeBundle(exportComputedTheme("office", "辦公室", el))
    )).toBe(true);
  });
});

describe("nextCustomThemeId", () => {
  it("returns custom-1 when nothing is taken", () => {
    expect(nextCustomThemeId([])).toBe("custom-1");
  });

  it("skips taken ids and the reserved built-in", () => {
    expect(nextCustomThemeId(["custom-1", "custom-2"])).toBe("custom-3");
    expect(nextCustomThemeId(["office"])).toBe("custom-1");
  });
});

describe("parseImportedBundle", () => {
  it("returns the normalized bundle for admissible JSON", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "midnight",
        name: "Midnight",
        colors: { "--color-accent": "#0b1020" },
      })
    );
    expect(res).toEqual({
      bundle: {
        id: "midnight",
        name: "Midnight",
        colors: { "--color-accent": "#0b1020" },
      },
    });
  });

  it("carries a valid wording overlay through (T-16a1 P3)", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "worded",
        name: "Worded",
        colors: { "--color-accent": "#0b1020" },
        wording: { zh: { "nav.tasks": "任務榜" } },
      })
    );
    expect("bundle" in res && res.bundle.wording).toEqual({
      zh: { "nav.tasks": "任務榜" },
    });
  });

  it("carries a valid avatars overlay through (bb2e3b4)", () => {
    const pngAvatar =
      "data:image/png;base64," +
      btoa(
        String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
      );
    const res = parseImportedBundle(
      JSON.stringify({
        id: "faced",
        name: "Faced",
        colors: { "--color-accent": "#0b1020" },
        avatars: { outsource: pngAvatar },
      })
    );
    expect("bundle" in res && res.bundle.avatars).toEqual({
      outsource: pngAvatar,
    });
  });

  it("rejects a wording overlay keyed on a non-whitelisted code", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "worded",
        name: "Worded",
        colors: { "--color-accent": "#0b1020" },
        wording: { zh: { "not.a.real.code": "x" } },
      })
    );
    expect("error" in res).toBe(true);
  });

  it("rejects malformed JSON with a plain-language error", () => {
    const res = parseImportedBundle("{ not json");
    expect("error" in res && res.error).toBe("不是有效的 JSON");
  });

  it("rejects a bundle carrying an injection-shaped colour value", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "evil",
        name: "Evil",
        colors: { "--color-bg": "red; } body { background: url(x)" },
      })
    );
    expect("error" in res).toBe(true);
  });

  it("rejects a bundle whose id is reserved for a built-in", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "office",
        name: "Nope",
        colors: { "--color-accent": "#fff" },
      })
    );
    expect("error" in res).toBe(true);
  });

  it("rejects a token name outside the theme.css whitelist", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "sneaky",
        name: "Sneaky",
        colors: { "--color-not-a-token": "#fff" },
      })
    );
    expect("error" in res).toBe(true);
  });
});

describe("bundleFilename", () => {
  it("derives a filesystem-safe name from the bundle id", () => {
    expect(bundleFilename({ id: "midnight", name: "M", colors: {} })).toBe(
      "officraft-theme-midnight.json"
    );
  });
});
