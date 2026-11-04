import { describe, expect, it } from "vitest";
import { contrastRatio, parseColor } from "./themeColor";
import type { ThemeBundle } from "./themeBundleCore";
import { daypartFrameAt, daypartPositionAt } from "./themeDayparts";

const DAY_BG = "data:image/webp;base64,REFZ";
const DUSK_BG = "data:image/webp;base64,RFVTSw==";

// Day is light, dusk and night are dark: day→dusk flips the scheme, dusk→night does not.
const bundle: ThemeBundle = {
  id: "desk",
  name: "Desk",
  colors: { "--color-bg": "#f4efe6", "--color-text": "#1f1b16", "--color-accent": "#c06a2b" },
  backgrounds: { canvas: DAY_BG },
  backgroundModes: { canvas: "cover" },
  dayparts: {
    transitionMinutes: 40,
    parts: [
      { start: "06:30" },
      {
        start: "17:00",
        colors: { "--color-bg": "#2a1d1a", "--color-text": "#f3e6d8" },
        backgrounds: { canvas: DUSK_BG },
      },
      { start: "19:00", colors: { "--color-bg": "#12141c", "--color-text": "#e6e8f0" } },
    ],
  },
};

const at = (h: number, m: number, s = 0, day = 5) => new Date(new Date(2026, 10, day, h, m).getTime() + s * 1000);

describe("daypartPositionAt", () => {
  it("returns null for a bundle without parts", () => {
    const { dayparts: _d, ...plain } = bundle;
    void _d;
    expect(daypartPositionAt(plain, at(12, 0))).toBeNull();
  });

  it("names the part shown, the one before it and the next start", () => {
    expect(daypartPositionAt(bundle, at(12, 0))).toEqual({
      from: 2,
      to: 0,
      startedAt: at(6, 30),
      elapsedMs: 5.5 * 3_600_000,
      transitionMs: 40 * 60_000,
      nextStart: at(17, 0),
    });
  });

  it("wraps past midnight to the previous day's last part", () => {
    expect(daypartPositionAt(bundle, at(2, 0))).toMatchObject({
      from: 1,
      to: 2,
      startedAt: at(19, 0, 0, 4),
      nextStart: at(6, 30),
    });
  });

  it("treats the start minute itself as the beginning of the move", () => {
    expect(daypartPositionAt(bundle, at(17, 0))).toMatchObject({ from: 0, to: 1, elapsedMs: 0 });
  });
});

describe("daypartFrameAt", () => {
  it("shows the settled part with no overlay and wakes at the next start", () => {
    expect(daypartFrameAt(bundle, at(18, 0))).toEqual({
      base: {
        id: "desk",
        name: "Desk",
        colors: { "--color-bg": "#2a1d1a", "--color-text": "#f3e6d8", "--color-accent": "#c06a2b" },
        backgrounds: { canvas: DUSK_BG },
        backgroundModes: { canvas: "cover" },
      },
      overlay: null,
      nextUpdate: at(19, 0),
    });
  });

  it("keeps the old colours before the middle of a dark↔light move and fades the new image in", () => {
    expect(daypartFrameAt(bundle, at(17, 10))).toEqual({
      base: {
        id: "desk",
        name: "Desk",
        colors: { "--color-bg": "#f4efe6", "--color-text": "#1f1b16", "--color-accent": "#c06a2b" },
        backgrounds: { canvas: DAY_BG },
        backgroundModes: { canvas: "cover" },
      },
      overlay: { canvas: DUSK_BG, mode: "cover", opacity: 0.25 },
      nextUpdate: at(17, 19, 58.5),
    });
  });

  it("switches a dark↔light move's colours within 3 seconds at the middle", () => {
    expect(daypartFrameAt(bundle, at(17, 19, 58.5))!.base.colors["--color-bg"]).toBe("#f4efe6");
    expect(daypartFrameAt(bundle, at(17, 20, 0))!.base.colors["--color-bg"]).toBe("rgba(143, 134, 128, 1)");
    expect(daypartFrameAt(bundle, at(17, 20, 1.5))!.base.colors["--color-bg"]).toBe("#2a1d1a");
    expect(daypartFrameAt(bundle, at(17, 20, 0))!.nextUpdate).toEqual(new Date(at(17, 20).getTime() + 100));
  });

  it("moves same-scheme colours gradually and cross-fades to the image the new part inherits", () => {
    expect(daypartFrameAt(bundle, at(19, 10))).toEqual({
      base: {
        id: "desk",
        name: "Desk",
        colors: { "--color-bg": "rgba(36, 27, 27, 1)", "--color-text": "rgba(240, 231, 222, 1)", "--color-accent": "#c06a2b" },
        backgrounds: { canvas: DUSK_BG },
        backgroundModes: { canvas: "cover" },
      },
      overlay: { canvas: DAY_BG, mode: "cover", opacity: 0.25 },
      nextUpdate: at(19, 10, 15),
    });
  });

  it("fades the outgoing image out over a plain background when the new part has no image", () => {
    const { backgrounds: _b, backgroundModes: _m, ...noTopImage } = bundle;
    void _b;
    void _m;
    expect(daypartFrameAt(noTopImage, at(19, 30))).toMatchObject({
      base: { colors: { "--color-bg": "rgba(24, 22, 28, 1)" } },
      overlay: { canvas: DUSK_BG, mode: undefined, opacity: 0.25 },
    });
    expect(daypartFrameAt(noTopImage, at(19, 30))!.base.backgrounds).toBeUndefined();
  });

  it("keeps main text readable against the page background outside the 3-second swap", () => {
    const start = at(17, 0).getTime();
    const swapFrom = start + 20 * 60_000 - 1_500;
    const swapTo = start + 20 * 60_000 + 1_500;
    for (let t = start; t <= start + 40 * 60_000; t += 1_000) {
      if (t > swapFrom && t < swapTo) continue;
      const c = daypartFrameAt(bundle, new Date(t))!.base.colors;
      expect(contrastRatio(parseColor(c["--color-text"])!, parseColor(c["--color-bg"])!)).toBeGreaterThanOrEqual(4.5);
    }
  });
});
