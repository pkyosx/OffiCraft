import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { seasonalWindowsFrom } from "./seasonalTheme";
import {
  activeSeasonalWindow,
  nextSeasonalBoundary,
  parseLocalDateTime,
  scheduleFrom,
  seasonalEntryProblem,
  seasonalPickAt,
  themeIdOfFile,
} from "./seasonalSchedule";
import { validateThemeBundleWith, type ThemeBundle } from "./themeBundleCore";
import { validateWording } from "./themeWording";
import { readDictMessage } from "../i18n/wording";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { paintRecordFor } from "./themePaint";

const THEMES_DIR = resolve(__dirname, "../../../themes");

function readThemesFile(name: string): unknown {
  return JSON.parse(readFileSync(resolve(THEMES_DIR, name), "utf8"));
}

describe("seasonalWindowsFrom", () => {
  const harvest: ThemeBundle = { id: "harvest", name: "Harvest", colors: { "--color-bg": "#15101f" } };
  const schedule = [
    { id: "harvest", start: new Date(2030, 9, 24), end: new Date(2030, 10, 1), theme: "harvest.theme.json" },
  ];

  it("fetches no theme file until a window's load() is called, then fetches only that one", async () => {
    const harvestFile = vi.fn(() => Promise.resolve(harvest));
    const otherFile = vi.fn(() => Promise.resolve(harvest));
    const windows = seasonalWindowsFrom(schedule, {
      "../../../themes/harvest.theme.json": harvestFile,
      "../../../themes/other.theme.json": otherFile,
    });

    expect(windows.map(({ id, start, end }) => ({ id, start, end }))).toEqual([
      { id: "harvest", start: new Date(2030, 9, 24), end: new Date(2030, 10, 1) },
    ]);
    expect(harvestFile).not.toHaveBeenCalled();

    await expect(windows[0].load()).resolves.toBe(harvest);
    expect(harvestFile).toHaveBeenCalledTimes(1);
    expect(otherFile).not.toHaveBeenCalled();
  });

  it("a window with day parts loads the day part's file it is asked for, and only that one", async () => {
    const dayFile = vi.fn(() => Promise.resolve(harvest));
    const nightFile = vi.fn(() => Promise.resolve(harvest));
    const [w] = seasonalWindowsFrom(
      [
        {
          id: "harvest",
          start: new Date(2030, 9, 24),
          end: new Date(2030, 10, 1),
          schedule: [
            { start: "06:00", theme: "harvest-day.theme.json" },
            { start: "18:00", theme: "harvest-night.theme.json" },
          ],
        },
      ],
      {
        "../../../themes/harvest-day.theme.json": dayFile,
        "../../../themes/harvest-night.theme.json": nightFile,
      }
    );

    expect(w.schedule).toHaveLength(2);
    await w.load("harvest-night.theme.json");
    expect(nightFile).toHaveBeenCalledTimes(1);
    expect(dayFile).not.toHaveBeenCalled();
  });

  it("rejects load() naming the file when the schedule points at a theme file that does not exist", async () => {
    const [w] = seasonalWindowsFrom(schedule, {});
    await expect(w.load()).rejects.toThrow("seasonal theme file not found: themes/harvest.theme.json");
  });
});

describe("the shipped themes/ seasonal data", () => {
  const raw = readThemesFile("seasonal-schedule.json") as {
    id: string;
    start: string;
    end: string;
    theme?: string;
    schedule?: { start: string; theme: string }[];
  }[];

  it("is a list of entries, each with one theme file or 2 to 6 day parts", () => {
    expect(Array.isArray(raw)).toBe(true);
    expect(raw.length).toBeGreaterThan(0);
    for (const e of raw) expect(seasonalEntryProblem(e), JSON.stringify(e)).toBeNull();
  });

  it("gives every entry a parseable local start strictly before its end, and a unique id", () => {
    for (const e of raw) {
      const start = parseLocalDateTime(e.start);
      const end = parseLocalDateTime(e.end);
      expect(Number.isNaN(start.getTime()), `${e.id} start ${e.start}`).toBe(false);
      expect(Number.isNaN(end.getTime()), `${e.id} end ${e.end}`).toBe(false);
      expect(start.getTime(), `${e.id} start must be before end`).toBeLessThan(end.getTime());
    }
    expect(new Set(raw.map((e) => e.id)).size).toBe(raw.length);
  });

  it("shows New Year's Eve from Dec 31 06:00 and New Year on Jan 1, switching day parts by time and flipping from Eve night to New Year night at midnight", () => {
    const windows = scheduleFrom(raw);
    const shown = (at: Date) => {
      const w = activeSeasonalWindow(at, windows);
      return w ? seasonalPickAt(at, w).id : null;
    };
    const boundaries: Date[] = [];
    for (let b = nextSeasonalBoundary(new Date(2026, 11, 30, 12, 0), windows); b; b = nextSeasonalBoundary(b, windows)) {
      if (b.getTime() > new Date(2027, 0, 2, 0, 0).getTime()) break;
      boundaries.push(b);
    }
    expect(boundaries.map((b) => [b, shown(b)])).toEqual([
      [new Date(2026, 11, 31, 6, 0), "new-years-eve-2026-day"],
      [new Date(2026, 11, 31, 16, 0), "new-years-eve-2026-dusk"],
      [new Date(2026, 11, 31, 18, 30), "new-years-eve-2026-night"],
      [new Date(2027, 0, 1, 0, 0), "new-year-2027-night"],
      [new Date(2027, 0, 1, 6, 0), "new-year-2027-day"],
      [new Date(2027, 0, 1, 16, 0), "new-year-2027-dusk"],
      [new Date(2027, 0, 1, 18, 30), "new-year-2027-night"],
      [new Date(2027, 0, 2, 0, 0), null],
    ]);
    expect(shown(new Date(2026, 11, 31, 5, 59))).toBeNull();
    expect(shown(new Date(2026, 11, 31, 23, 59))).toBe("new-years-eve-2026-night");
    expect(shown(new Date(2027, 0, 1, 5, 59))).toBe("new-year-2027-night");
    expect(shown(new Date(2027, 0, 1, 23, 59))).toBe("new-year-2027-night");
  });

  // A day part's bundle carries its file's stem; a one-file window's, the entry's id.
  const files = raw.flatMap((e) =>
    e.schedule
      ? e.schedule.map((r) => ({ file: r.theme, id: themeIdOfFile(r.theme) }))
      : [{ file: e.theme!, id: e.id }]
  );
  for (const { file, id } of files) {
    it(`${file} exists, passes the bundle grammar whole, wording included, and carries the id ${id}`, () => {
      const bundle = readThemesFile(file);
      const skipped: string[] = [];
      expect(validateThemeBundleWith(bundle, file, skipped, validateWording)).toBeNull();
      expect(skipped, "wording codes the app does not know").toEqual([]);
      expect((bundle as ThemeBundle).id).toBe(id);
    });

    it(`${file} keeps the plain wording for background commands, monitors and the waits count`, () => {
      const { wording } = readThemesFile(file) as ThemeBundle;
      for (const locale of ["zh", "en"] as const) {
        const words = wording?.[locale] ?? {};
        for (const code of [
          "activity.waitKind.background_command",
          "activity.waitKind.monitor",
          "activity.waitsTailOne",
          "activity.waitsTailMany",
        ]) {
          expect(words[code], `${locale} ${code}`).toBeUndefined();
        }
      }
    });

    it(`${file} keeps every plain sentence whole, adding wording only before or after it`, () => {
      const { wording } = readThemesFile(file) as ThemeBundle;
      const plain = { zh, en };
      const rewritten: string[] = [];
      for (const locale of ["zh", "en"] as const) {
        for (const [code, text] of Object.entries(wording?.[locale] ?? {})) {
          const original = readDictMessage(plain[locale], code);
          if (original === null || !text.includes(original)) {
            rewritten.push(`${locale} ${code}: "${original}" → "${text}"`);
          }
        }
      }
      expect(rewritten).toEqual([]);
    });

    it(`${file} still validates once pruned to the pre-paint record`, () => {
      const bundle = readThemesFile(file) as ThemeBundle;
      expect(validateThemeBundleWith(paintRecordFor(bundle).bundle, file, undefined, null)).toBeNull();
    });
  }
});
