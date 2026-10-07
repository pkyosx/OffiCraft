import { describe, expect, it } from "vitest";
import {
  activeSeasonalWindow,
  nextSeasonalBoundary,
  parseLocalDateTime,
  scheduleFrom,
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
