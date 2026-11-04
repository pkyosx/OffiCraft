import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  LS_SCHEDULE_PAINT,
  LS_SEASONAL_PAINT,
  LS_SEASONAL_THEME,
  LS_THEME,
  LS_THEME_PAINT,
  LS_THEME_SCHEDULE,
  paintRecordFor,
} from "../lib/themePaint";
import {
  MALICIOUS_PAINT_CASES,
  PAINT_THEME_ID,
  VALID_RICH_BUNDLE,
  paintRecordJSON,
} from "../lib/paintFixtures";
import type { SeasonalScheduleEntry } from "../lib/seasonalSchedule";

const schedule = vi.hoisted(() => [] as SeasonalScheduleEntry[]);
vi.mock("../lib/seasonalSchedule", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../lib/seasonalSchedule")>()),
  SEASONAL_SCHEDULE: schedule,
}));

const HARVEST_WINDOW: SeasonalScheduleEntry = {
  id: "harvest",
  start: new Date(2030, 9, 24, 0, 0),
  end: new Date(2030, 10, 1, 0, 0),
  theme: "harvest.theme.json",
};
const HARVEST_BUNDLE = {
  id: "harvest",
  name: "Harvest",
  colors: { "--color-bg": "#15101f", "--color-accent": "#ff8c2b" },
};

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
    schedule.length = 0;
    vi.useRealTimers();
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

  describe("with a seasonal window in the schedule", () => {
    function seed(seasonalPref: string | null) {
      schedule.push(HARVEST_WINDOW);
      localStorage.setItem(LS_THEME, PAINT_THEME_ID);
      localStorage.setItem(LS_THEME_PAINT, JSON.stringify(paintRecordFor(VALID_RICH_BUNDLE)));
      localStorage.setItem(LS_SEASONAL_PAINT, JSON.stringify(paintRecordFor(HARVEST_BUNDLE)));
      if (seasonalPref !== null) localStorage.setItem(LS_SEASONAL_THEME, seasonalPref);
    }
    function at(date: Date) {
      vi.useFakeTimers({ toFake: ["Date"] });
      vi.setSystemTime(date);
    }

    it("before the window opens, paints the display theme and not the cached seasonal record", async () => {
      seed(null);
      at(new Date(2030, 9, 23, 23, 59, 59, 999));

      await runPrePaint();

      expect(root.dataset.theme).toBe("office");
      expect(root.style.getPropertyValue("--color-bg")).toBe(VALID_RICH_BUNDLE.colors["--color-bg"]);
    });

    it("from the window's start, paints the cached seasonal record instead of the display theme", async () => {
      seed(null);
      at(new Date(2030, 9, 24, 0, 0));

      await runPrePaint();

      expect(root.dataset.theme).toBe("office");
      expect(root.style.getPropertyValue("--color-bg")).toBe("#15101f");
      expect(root.style.getPropertyValue("--color-accent")).toBe("#ff8c2b");
      expect(window.__ocPaintTokens).toEqual(["--color-bg", "--color-accent", "color-scheme"]);
      expect(root.style.getPropertyValue("color-scheme")).toBe("dark");
    });

    it("at the window's end, paints the display theme again", async () => {
      seed("true");
      at(new Date(2030, 10, 1, 0, 0));

      await runPrePaint();

      expect(root.style.getPropertyValue("--color-bg")).toBe(VALID_RICH_BUNDLE.colors["--color-bg"]);
    });

    it("inside the window with the seasonal theme turned off, paints the display theme", async () => {
      seed("false");
      at(new Date(2030, 9, 28, 12, 0));

      await runPrePaint();

      expect(root.style.getPropertyValue("--color-bg")).toBe(VALID_RICH_BUNDLE.colors["--color-bg"]);
      expect(window.__ocPaintTokens).toContain("--color-bg");
    });

    it("inside the window with no seasonal record cached, paints nothing rather than the display theme", async () => {
      seed(null);
      localStorage.removeItem(LS_SEASONAL_PAINT);
      at(new Date(2030, 9, 28, 12, 0));

      await runPrePaint();

      expect(root.hasAttribute("data-theme")).toBe(false);
      expect(root.getAttribute("style")).toBeNull();
      expect(window.__ocPaintTokens).toBeUndefined();
    });

    it("inside the window, ignores a seasonal record cached for a different window", async () => {
      seed(null);
      localStorage.setItem(
        LS_SEASONAL_PAINT,
        JSON.stringify(paintRecordFor({ ...HARVEST_BUNDLE, id: "frost" }))
      );
      at(new Date(2030, 9, 28, 12, 0));

      await runPrePaint();

      expect(root.getAttribute("style")).toBeNull();
    });
  });

  describe("with a theme schedule cached", () => {
    const DUSK = { id: "dusk", name: "Dusk", colors: { "--color-bg": "#101418" } };
    function seed(schedulePaint: string | null) {
      localStorage.setItem(LS_THEME, "dusk");
      localStorage.setItem(LS_THEME_PAINT, JSON.stringify(paintRecordFor(DUSK)));
      localStorage.setItem(
        LS_THEME_SCHEDULE,
        JSON.stringify([
          { start: "07:00", theme: PAINT_THEME_ID },
          { start: "19:00", theme: "office-light" },
        ])
      );
      if (schedulePaint !== null) localStorage.setItem(LS_SCHEDULE_PAINT, schedulePaint);
    }
    function at(date: Date) {
      vi.useFakeTimers({ toFake: ["Date"] });
      vi.setSystemTime(date);
    }

    it("paints the current entry's cached record instead of the display theme", async () => {
      seed(JSON.stringify([paintRecordFor(VALID_RICH_BUNDLE)]));
      at(new Date(2030, 0, 15, 7, 0));

      await runPrePaint();

      expect(root.dataset.theme).toBe("office");
      expect(root.style.getPropertyValue("--color-bg")).toBe(VALID_RICH_BUNDLE.colors["--color-bg"]);
      expect(window.__ocPaintTokens).toContain("--color-bg");
    });

    it("before the day's first start, paints the previous day's last entry, a built-in", async () => {
      seed(JSON.stringify([paintRecordFor(VALID_RICH_BUNDLE)]));
      at(new Date(2030, 0, 15, 6, 59, 59, 999));

      await runPrePaint();

      expect(root.dataset.theme).toBe("office-light");
      expect(root.getAttribute("style")).toBeNull();
      expect(window.__ocPaintTokens).toBeUndefined();
    });

    it("with no record cached for the current entry, paints nothing rather than the display theme", async () => {
      seed(JSON.stringify([paintRecordFor(DUSK)]));
      at(new Date(2030, 0, 15, 12, 0));

      await runPrePaint();

      expect(root.hasAttribute("data-theme")).toBe(false);
      expect(root.getAttribute("style")).toBeNull();
    });

    for (const c of MALICIOUS_PAINT_CASES.filter((m) => !m.notGuardedByValidator)) {
      it(`a ${c.name} record for the current entry paints nothing`, async () => {
        seed(`[${paintRecordJSON(c.bundle)}]`);
        at(new Date(2030, 0, 15, 12, 0));

        await runPrePaint();

        expect(root.getAttribute("style")).toBeNull();
        expect(window.__ocPaintTokens).toBeUndefined();
      });
    }

    it("a tampered schedule is no schedule: the display theme is painted", async () => {
      seed(JSON.stringify([paintRecordFor(VALID_RICH_BUNDLE)]));
      localStorage.setItem(
        LS_THEME_SCHEDULE,
        JSON.stringify([{ start: "07:00", theme: PAINT_THEME_ID }])
      );
      at(new Date(2030, 0, 15, 12, 0));

      await runPrePaint();

      expect(root.style.getPropertyValue("--color-bg")).toBe("#101418");
    });

    it("inside a seasonal window with the seasonal theme on, paints the seasonal record over the schedule", async () => {
      seed(JSON.stringify([paintRecordFor(VALID_RICH_BUNDLE)]));
      schedule.push(HARVEST_WINDOW);
      localStorage.setItem(LS_SEASONAL_PAINT, JSON.stringify(paintRecordFor(HARVEST_BUNDLE)));
      at(new Date(2030, 9, 28, 12, 0));

      await runPrePaint();

      expect(root.style.getPropertyValue("--color-bg")).toBe("#15101f");
    });

    it("inside a seasonal window with the seasonal theme off, paints the schedule's entry", async () => {
      seed(JSON.stringify([paintRecordFor(VALID_RICH_BUNDLE)]));
      schedule.push(HARVEST_WINDOW);
      localStorage.setItem(LS_SEASONAL_PAINT, JSON.stringify(paintRecordFor(HARVEST_BUNDLE)));
      localStorage.setItem(LS_SEASONAL_THEME, "false");
      at(new Date(2030, 9, 28, 12, 0));

      await runPrePaint();

      expect(root.style.getPropertyValue("--color-bg")).toBe(VALID_RICH_BUNDLE.colors["--color-bg"]);
    });
  });
});
