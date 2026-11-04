import { describe, expect, it } from "vitest";
import {
  activeScheduleEntry,
  nextScheduleBoundary,
  parseThemeSchedule,
  themeScheduleProblem,
} from "./themeSchedule";

const DAY = [
  { start: "07:00", theme: "office-light" },
  { start: "12:30", theme: "dusk" },
  { start: "19:00", theme: "office" },
];

describe("activeScheduleEntry", () => {
  it("answers the entry whose start is the latest at or before now", () => {
    expect(activeScheduleEntry(new Date(2030, 0, 15, 12, 0), DAY)).toEqual({
      start: "07:00",
      theme: "office-light",
    });
    expect(activeScheduleEntry(new Date(2030, 0, 15, 20, 15), DAY)).toEqual({
      start: "19:00",
      theme: "office",
    });
  });

  it("answers an entry from the first millisecond of its start up to the last one before the next", () => {
    expect(activeScheduleEntry(new Date(2030, 0, 15, 12, 30, 0, 0), DAY)?.start).toBe("12:30");
    expect(activeScheduleEntry(new Date(2030, 0, 15, 12, 29, 59, 999), DAY)?.start).toBe("07:00");
    expect(activeScheduleEntry(new Date(2030, 0, 15, 18, 59, 59, 999), DAY)?.start).toBe("12:30");
  });

  it("before the day's first start, answers the previous day's last entry", () => {
    expect(activeScheduleEntry(new Date(2030, 0, 15, 0, 0), DAY)).toEqual({
      start: "19:00",
      theme: "office",
    });
    expect(activeScheduleEntry(new Date(2030, 0, 15, 6, 59, 59, 999), DAY)?.start).toBe("19:00");
  });

  it("answers the same entries whatever order the schedule is listed in", () => {
    const shuffled = [DAY[2], DAY[0], DAY[1]];
    expect(activeScheduleEntry(new Date(2030, 0, 15, 3, 0), shuffled)?.start).toBe("19:00");
    expect(activeScheduleEntry(new Date(2030, 0, 15, 13, 0), shuffled)?.start).toBe("12:30");
  });

  it("answers null without a schedule", () => {
    expect(activeScheduleEntry(new Date(2030, 0, 15, 12, 0), [])).toBeNull();
  });
});

describe("nextScheduleBoundary", () => {
  it("answers the next start later today", () => {
    expect(nextScheduleBoundary(new Date(2030, 0, 15, 8, 0), DAY)).toEqual(
      new Date(2030, 0, 15, 12, 30)
    );
  });

  it("at a start itself, answers the following start, not the same one", () => {
    expect(nextScheduleBoundary(new Date(2030, 0, 15, 12, 30), DAY)).toEqual(
      new Date(2030, 0, 15, 19, 0)
    );
  });

  it("after the day's last start, answers tomorrow's first start, across a month end", () => {
    expect(nextScheduleBoundary(new Date(2030, 0, 31, 22, 0), DAY)).toEqual(
      new Date(2030, 1, 1, 7, 0)
    );
  });

  it("answers null without a schedule", () => {
    expect(nextScheduleBoundary(new Date(2030, 0, 15, 8, 0), [])).toBeNull();
  });
});

describe("themeScheduleProblem", () => {
  it("accepts no entries, or 2 to 6 with unique HH:MM starts", () => {
    expect(themeScheduleProblem([])).toBeNull();
    expect(themeScheduleProblem(DAY)).toBeNull();
  });

  it("names the first broken rule", () => {
    expect(themeScheduleProblem([DAY[0]])).toBe("count");
    expect(
      themeScheduleProblem(
        ["01", "02", "03", "04", "05", "06", "07"].map((h) => ({ start: `${h}:00`, theme: "office" }))
      )
    ).toBe("count");
    expect(themeScheduleProblem([DAY[0], { start: "", theme: "office" }])).toBe("start");
    expect(themeScheduleProblem([DAY[0], { start: "24:00", theme: "office" }])).toBe("start");
    expect(themeScheduleProblem([DAY[0], { start: "7:00", theme: "office" }])).toBe("start");
    expect(themeScheduleProblem([DAY[0], { start: "07:00", theme: "office" }])).toBe("duplicate");
    expect(themeScheduleProblem([DAY[0], { start: "09:00", theme: "" }])).toBe("theme");
  });
});

describe("parseThemeSchedule", () => {
  it("reads a valid list back sorted by start", () => {
    expect(parseThemeSchedule([DAY[2], DAY[0], DAY[1]])).toEqual(DAY);
  });

  it("reads anything malformed as no schedule at all", () => {
    for (const raw of [
      null,
      "07:00",
      { start: "07:00", theme: "office" },
      [DAY[0]],
      [DAY[0], null],
      [DAY[0], { start: "08:00" }],
      [DAY[0], { start: 800, theme: "office" }],
      [DAY[0], { start: "25:00", theme: "office" }],
      [DAY[0], { start: "07:00", theme: "office" }],
      [DAY[0], { start: "08:00", theme: "Bad Id" }],
      [DAY[0], { start: "08:00", theme: "x;color:red" }],
      [DAY[0], { start: "08:00", theme: "__proto__" }],
    ]) {
      expect(parseThemeSchedule(raw), JSON.stringify(raw)).toEqual([]);
    }
  });
});
