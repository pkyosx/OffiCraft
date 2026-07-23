import { afterEach, describe, expect, it } from "vitest";
import {
  bundleFilename,
  exportComputedTheme,
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
