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

  it("handles a part starting at midnight", () => {
    const midnight: ThemeBundle = {
      ...bundle,
      dayparts: { transitionMinutes: 10, parts: [{ start: "00:00" }, { start: "12:00" }] },
    };
    expect(daypartPositionAt(midnight, at(0, 5))).toMatchObject({
      from: 1,
      to: 0,
      startedAt: at(0, 0),
      elapsedMs: 5 * 60_000,
      nextStart: at(12, 0),
    });
    expect(daypartPositionAt(midnight, at(23, 59))).toMatchObject({ to: 1, nextStart: at(0, 0, 0, 6) });
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

  it("keeps the old colours before the middle of a dark↔light move and fades the new image toward its next-update value", () => {
    expect(daypartFrameAt(bundle, at(17, 10))).toEqual({
      base: {
        id: "desk",
        name: "Desk",
        colors: { "--color-bg": "#f4efe6", "--color-text": "#1f1b16", "--color-accent": "#c06a2b" },
        backgrounds: { canvas: DAY_BG },
        backgroundModes: { canvas: "cover" },
      },
      overlay: { canvas: DUSK_BG, mode: "cover", opacity: 0.25, targetOpacity: 0.25625, fadeMs: 15_000 },
      nextUpdate: at(17, 10, 15),
    });
  });

  it("lands an update exactly on the start of the 1-second swap and on the end of the move", () => {
    expect(daypartFrameAt(bundle, at(17, 19, 50))!.nextUpdate).toEqual(at(17, 19, 59.5));
    expect(daypartFrameAt(bundle, at(17, 39, 50))!.nextUpdate).toEqual(at(17, 40));
    expect(daypartFrameAt(bundle, at(17, 39, 50))!.overlay).toEqual({
      canvas: DUSK_BG,
      mode: "cover",
      opacity: expect.closeTo(2390 / 2400, 10),
      targetOpacity: 1,
      fadeMs: 10_000,
    });
  });

  it("switches a dark↔light move's colours within 1 second at the middle", () => {
    expect(daypartFrameAt(bundle, at(17, 19, 59.5))!.base.colors["--color-bg"]).toBe("#f4efe6");
    expect(daypartFrameAt(bundle, at(17, 20, 0))!.base.colors["--color-bg"]).toBe("rgba(143, 134, 128, 1)");
    expect(daypartFrameAt(bundle, at(17, 20, 0.5))!.base.colors["--color-bg"]).toBe("#2a1d1a");
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
      overlay: { canvas: DAY_BG, mode: "cover", opacity: 0.25, targetOpacity: 0.25625, fadeMs: 15_000 },
      nextUpdate: at(19, 10, 15),
    });
  });

  it("fades the outgoing image out over a plain background when the new part has no image", () => {
    const { backgrounds: _b, backgroundModes: _m, ...noTopImage } = bundle;
    void _b;
    void _m;
    expect(daypartFrameAt(noTopImage, at(19, 30))).toMatchObject({
      base: { colors: { "--color-bg": "rgba(24, 22, 28, 1)" } },
      overlay: { canvas: DUSK_BG, mode: undefined, opacity: 0.25, targetOpacity: expect.closeTo(0.24375, 10), fadeMs: 15_000 },
    });
    expect(daypartFrameAt(noTopImage, at(19, 30))!.base.backgrounds).toBeUndefined();
  });

  it("keeps main text readable against the page background outside the 1-second swap", () => {
    const start = at(17, 0).getTime();
    const swapFrom = start + 20 * 60_000 - 500;
    const swapTo = start + 20 * 60_000 + 500;
    for (let t = start; t <= start + 40 * 60_000; t += 1_000) {
      if (t > swapFrom && t < swapTo) continue;
      const c = daypartFrameAt(bundle, new Date(t))!.base.colors;
      expect(contrastRatio(parseColor(c["--color-text"])!, parseColor(c["--color-bg"])!)).toBeGreaterThanOrEqual(4.5);
    }
  });
});
