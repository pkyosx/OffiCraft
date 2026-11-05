import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { seasonalWindowsFrom } from "./seasonalTheme";
import { parseLocalDateTime, seasonalEntryProblem, themeIdOfFile } from "./seasonalSchedule";
import { validateThemeBundleWith, type ThemeBundle } from "./themeBundleCore";
import { validateWording } from "./themeWording";
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

    it(`${file} still validates once pruned to the pre-paint record`, () => {
      const bundle = readThemesFile(file) as ThemeBundle;
      expect(validateThemeBundleWith(paintRecordFor(bundle).bundle, file, undefined, null)).toBeNull();
    });
  }
});
