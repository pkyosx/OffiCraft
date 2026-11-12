import { describe, expect, it } from "vitest";
import {
  activeSeasonalWindow,
  nextSeasonalBoundary,
  parseLocalDateTime,
  scheduleFrom,
  seasonalEntryProblem,
  seasonalPickAt,
  seasonalThemeIds,
} from "./seasonalSchedule";

const HARVEST = { id: "harvest", start: new Date(2030, 9, 24, 0, 0), end: new Date(2030, 10, 1, 0, 0) };
const FROST = { id: "frost", start: new Date(2030, 11, 20, 0, 0), end: new Date(2030, 11, 27, 0, 0) };

describe("parseLocalDateTime", () => {
  it("reads a minute-precision date-time as that wall-clock moment in the viewer's zone", () => {
    expect(parseLocalDateTime("2026-10-24T00:00")).toEqual(new Date(2026, 9, 24, 0, 0));
    expect(parseLocalDateTime("2026-11-01T13:45")).toEqual(new Date(2026, 10, 1, 13, 45));
  });

  it("answers an invalid date for anything that is not a real local date-time", () => {
    for (const bad of [
      "2026-10-24",
      "2026-10-24T00:00Z",
      "2026-10-24T00:00:00",
      "2026-02-30T00:00",
      "2026-13-01T00:00",
      "2026-10-24T24:00",
      "2026-10-24T23:60",
      "",
    ]) {
      expect(Number.isNaN(parseLocalDateTime(bad).getTime()), bad).toBe(true);
    }
  });
});

describe("scheduleFrom", () => {
  it("turns each entry's start and end into local dates and keeps its id and theme file", () => {
    expect(
      scheduleFrom([
        { id: "harvest", start: "2030-10-24T00:00", end: "2030-11-01T00:00", theme: "harvest.theme.json" },
      ])
    ).toEqual([{ ...HARVEST, theme: "harvest.theme.json" }]);
  });
});

describe("activeSeasonalWindow", () => {
  const windows = [HARVEST, FROST];

  it("answers no window before the start", () => {
    expect(activeSeasonalWindow(new Date(2030, 9, 23, 23, 59, 59, 999), windows)).toBeNull();
  });

  it("answers the window from the first millisecond of its start (inclusive)", () => {
    expect(activeSeasonalWindow(new Date(2030, 9, 24, 0, 0), windows)).toBe(HARVEST);
  });

  it("answers the window inside it", () => {
    expect(activeSeasonalWindow(new Date(2030, 9, 28, 12, 0), windows)).toBe(HARVEST);
    expect(activeSeasonalWindow(new Date(2030, 11, 24, 18, 0), windows)).toBe(FROST);
  });

  it("answers the window up to the last millisecond before its end", () => {
    expect(activeSeasonalWindow(new Date(2030, 9, 31, 23, 59, 59, 999), windows)).toBe(HARVEST);
  });

  it("answers no window at its end (exclusive) and after it", () => {
    expect(activeSeasonalWindow(new Date(2030, 10, 1, 0, 0), windows)).toBeNull();
    expect(activeSeasonalWindow(new Date(2031, 0, 1, 0, 0), windows)).toBeNull();
  });

  it("lets the entry listed first win while two windows overlap, and hands over to the other where only it is open", () => {
    const a = { id: "a", start: new Date(2030, 9, 24, 0, 0), end: new Date(2030, 10, 1, 0, 0) };
    const b = { id: "b", start: new Date(2030, 9, 30, 0, 0), end: new Date(2030, 10, 28, 0, 0) };

    expect(activeSeasonalWindow(new Date(2030, 9, 30, 12, 0), [a, b])).toBe(a);
    expect(activeSeasonalWindow(new Date(2030, 9, 31, 23, 59, 59, 999), [a, b])).toBe(a);
    expect(activeSeasonalWindow(new Date(2030, 10, 1, 0, 0), [a, b])).toBe(b);

    expect(activeSeasonalWindow(new Date(2030, 9, 29, 12, 0), [b, a])).toBe(a);
    expect(activeSeasonalWindow(new Date(2030, 9, 30, 12, 0), [b, a])).toBe(b);
    expect(activeSeasonalWindow(new Date(2030, 10, 1, 0, 0), [b, a])).toBe(b);
  });

  it("never matches an entry whose dates did not parse", () => {
    const broken = { id: "broken", start: new Date(NaN), end: new Date(NaN) };
    expect(activeSeasonalWindow(new Date(2030, 9, 28), [broken])).toBeNull();
  });
});

describe("nextSeasonalBoundary", () => {
  const windows = [FROST, HARVEST];

  it("answers the nearest start when no window is open", () => {
    expect(nextSeasonalBoundary(new Date(2030, 0, 1), windows)).toEqual(new Date(2030, 9, 24, 0, 0));
  });

  it("answers the open window's end, not its start, at the start itself", () => {
    expect(nextSeasonalBoundary(new Date(2030, 9, 24, 0, 0), windows)).toEqual(new Date(2030, 10, 1, 0, 0));
  });

  it("answers the next window's start at a window's end", () => {
    expect(nextSeasonalBoundary(new Date(2030, 10, 1, 0, 0), windows)).toEqual(new Date(2030, 11, 20, 0, 0));
  });

  it("inside two overlapping windows, answers the earlier of their ends", () => {
    const a = { id: "a", start: new Date(2030, 9, 24, 0, 0), end: new Date(2030, 10, 1, 0, 0) };
    const b = { id: "b", start: new Date(2030, 9, 30, 0, 0), end: new Date(2030, 10, 28, 0, 0) };
    expect(nextSeasonalBoundary(new Date(2030, 9, 30, 12, 0), [a, b])).toEqual(new Date(2030, 10, 1, 0, 0));
    expect(nextSeasonalBoundary(new Date(2030, 10, 1, 0, 0), [a, b])).toEqual(new Date(2030, 10, 28, 0, 0));
  });

  it("answers null once every boundary has passed", () => {
    expect(nextSeasonalBoundary(new Date(2030, 11, 27, 0, 0), windows)).toBeNull();
  });
});

describe("seasonalEntryProblem", () => {
  const base = { id: "harvest", start: "2030-10-24T00:00", end: "2030-11-01T00:00" };
  const rows = [
    { start: "06:00", theme: "harvest-day.theme.json" },
    { start: "18:00", theme: "harvest-night.theme.json" },
  ];

  it("accepts a window with one theme file, or with 2 to 6 day parts", () => {
    expect(seasonalEntryProblem({ ...base, theme: "harvest.theme.json" })).toBeNull();
    expect(seasonalEntryProblem({ ...base, schedule: rows })).toBeNull();
    expect(
      seasonalEntryProblem({
        ...base,
        schedule: ["00:00", "04:00", "08:00", "12:00", "16:00", "20:00"].map((start) => ({
          start,
          theme: "harvest-day.theme.json",
        })),
      })
    ).toBeNull();
  });

  it("refuses a window carrying both a theme and day parts, or neither", () => {
    expect(seasonalEntryProblem({ ...base, theme: "harvest.theme.json", schedule: rows })).not.toBeNull();
    expect(seasonalEntryProblem(base)).not.toBeNull();
  });

  it("refuses day parts that break the user schedule's rules", () => {
    for (const schedule of [
      [],
      [rows[0]],
      Array.from({ length: 7 }, (_, i) => ({ start: `0${i}:00`, theme: "harvest-day.theme.json" })),
      [rows[0], { start: "06:00", theme: "harvest-night.theme.json" }],
      [rows[0], { start: "24:00", theme: "harvest-night.theme.json" }],
      [rows[0], { start: "6:00", theme: "harvest-night.theme.json" }],
      [rows[0], { start: "18:00", theme: "harvest-night.json" }],
      [rows[0], { start: "18:00", theme: "harvest-night.theme.json", end: "20:00" }],
    ]) {
      expect(seasonalEntryProblem({ ...base, schedule }), JSON.stringify(schedule)).not.toBeNull();
    }
  });
});

describe("seasonal day parts", () => {
  const WINDOW = {
    ...HARVEST,
    schedule: [
      { start: "06:00", theme: "harvest-day.theme.json" },
      { start: "18:00", theme: "harvest-night.theme.json" },
    ],
  };

  it("scheduleFrom keeps the day parts sorted by start instead of a theme file", () => {
    expect(
      scheduleFrom([
        {
          id: "harvest",
          start: "2030-10-24T00:00",
          end: "2030-11-01T00:00",
          schedule: [WINDOW.schedule[1], WINDOW.schedule[0]],
        },
      ])
    ).toEqual([{ ...HARVEST, schedule: WINDOW.schedule }]);
  });

  it("picks the day part by local time, its bundle id being the file's stem, and the previous evening's before the first start", () => {
    expect(seasonalPickAt(new Date(2030, 9, 25, 12, 0), WINDOW)).toEqual({
      id: "harvest-day",
      file: "harvest-day.theme.json",
    });
    expect(seasonalPickAt(new Date(2030, 9, 25, 5, 59), WINDOW)).toEqual({
      id: "harvest-night",
      file: "harvest-night.theme.json",
    });
    expect(seasonalPickAt(new Date(2030, 9, 25, 12, 0), { ...HARVEST, theme: "harvest.theme.json" })).toEqual({
      id: "harvest",
      file: "harvest.theme.json",
    });
  });

  it("names every bundle id a window can show", () => {
    expect(seasonalThemeIds(WINDOW)).toEqual(["harvest-day", "harvest-night"]);
    expect(seasonalThemeIds(HARVEST)).toEqual(["harvest"]);
  });

  it("nextSeasonalBoundary answers the next day part inside an open window, and ignores day parts of a closed one", () => {
    expect(nextSeasonalBoundary(new Date(2030, 9, 25, 12, 0), [WINDOW])).toEqual(new Date(2030, 9, 25, 18, 0));
    expect(nextSeasonalBoundary(new Date(2030, 9, 31, 20, 0), [WINDOW])).toEqual(HARVEST.end);
    expect(nextSeasonalBoundary(new Date(2030, 9, 1, 12, 0), [WINDOW])).toEqual(HARVEST.start);
  });
});
