// Day-divider grouping + labels (dateFormat.ts) — the pure calendar logic
// under the chat stream's LINE-style date dividers and the reply cards'
// absolute stamps. Every timestamp is FIXED and built via the local-time Date
// constructor (never Date.now), so the pins hold in any timezone.

import { describe, it, expect } from "vitest";
import {
  dayStartOf,
  splitByDay,
  formatDayLabel,
  formatAbsolute,
  type DayLabelDict,
} from "./dateFormat";

/** Local-time epoch seconds for y/m/d h:mm (month 1-based). */
function ts(
  y: number,
  mo: number,
  d: number,
  h = 12,
  mi = 0,
): number {
  return new Date(y, mo - 1, d, h, mi, 0, 0).getTime() / 1000;
}

// A locale-neutral label dict: the pins assert WHICH branch fired, not any
// specific language's wording (that belongs to the i18n dicts).
const dict: DayLabelDict = {
  dateToday: "TODAY",
  dateYesterday: "YESTERDAY",
  dateOn: (month, day, weekday) => `D:${month}/${day}/w${weekday}`,
  dateOnYear: (year, month, day, weekday) =>
    `Y:${year}/${month}/${day}/w${weekday}`,
};

describe("dayStartOf", () => {
  it("floors any time of day to the same local midnight", () => {
    const midnight = ts(2026, 7, 11, 0, 0);
    expect(dayStartOf(ts(2026, 7, 11, 0, 0))).toBe(midnight);
    expect(dayStartOf(ts(2026, 7, 11, 9, 30))).toBe(midnight);
    expect(dayStartOf(ts(2026, 7, 11, 23, 59))).toBe(midnight);
  });

  it("23:59 and 00:00 one minute later are DIFFERENT days", () => {
    expect(dayStartOf(ts(2026, 7, 11, 23, 59))).not.toBe(
      dayStartOf(ts(2026, 7, 12, 0, 0)),
    );
  });
});

describe("splitByDay", () => {
  const m = (id: string, t: number) => ({ id, ts: t });

  it("same-day messages stay in ONE group", () => {
    const groups = splitByDay([
      m("a", ts(2026, 7, 13, 9, 0)),
      m("b", ts(2026, 7, 13, 12, 0)),
      m("c", ts(2026, 7, 13, 23, 59)),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].dayTs).toBe(ts(2026, 7, 13, 0, 0));
    expect(groups[0].items.map((x) => x.id)).toEqual(["a", "b", "c"]);
  });

  it("splits exactly at the local-midnight crossing (23:59 → 00:00)", () => {
    const groups = splitByDay([
      m("a", ts(2026, 7, 11, 23, 59)),
      m("b", ts(2026, 7, 12, 0, 0)),
      m("c", ts(2026, 7, 12, 0, 1)),
    ]);
    expect(groups.map((g) => g.dayTs)).toEqual([
      ts(2026, 7, 11, 0, 0),
      ts(2026, 7, 12, 0, 0),
    ]);
    expect(groups[0].items.map((x) => x.id)).toEqual(["a"]);
    expect(groups[1].items.map((x) => x.id)).toEqual(["b", "c"]);
  });

  it("a three-day stream yields three groups, order preserved", () => {
    const groups = splitByDay([
      m("d1", ts(2026, 7, 11)),
      m("d2a", ts(2026, 7, 12, 8)),
      m("d2b", ts(2026, 7, 12, 20)),
      m("d3", ts(2026, 7, 13)),
    ]);
    expect(groups.map((g) => g.items.length)).toEqual([1, 2, 1]);
    expect(groups.map((g) => g.dayTs)).toEqual([
      ts(2026, 7, 11, 0, 0),
      ts(2026, 7, 12, 0, 0),
      ts(2026, 7, 13, 0, 0),
    ]);
  });

  it("empty stream → no groups", () => {
    expect(splitByDay([])).toEqual([]);
  });
});

describe("formatDayLabel", () => {
  // Fixed clock: 2026-07-13 (a Monday) at 10:00 local.
  const now = ts(2026, 7, 13, 10, 0);

  it("today → the today label (any time of the current day)", () => {
    expect(formatDayLabel(ts(2026, 7, 13, 0, 0), now, dict)).toBe("TODAY");
    expect(formatDayLabel(ts(2026, 7, 13, 23, 59), now, dict)).toBe("TODAY");
  });

  it("yesterday → the yesterday label, including its 23:59 edge", () => {
    expect(formatDayLabel(ts(2026, 7, 12, 0, 0), now, dict)).toBe("YESTERDAY");
    expect(formatDayLabel(ts(2026, 7, 12, 23, 59), now, dict)).toBe(
      "YESTERDAY",
    );
  });

  it("two days ago → the dated label with the correct weekday (Sat=6)", () => {
    // 2026-07-11 is a Saturday.
    expect(formatDayLabel(ts(2026, 7, 11), now, dict)).toBe("D:7/11/w6");
  });

  it("a different year → the year-carrying label", () => {
    // 2025-12-31 is a Wednesday (w3).
    expect(formatDayLabel(ts(2025, 12, 31), now, dict)).toBe(
      "Y:2025/12/31/w3",
    );
  });

  it("today/yesterday judgement moves with the caller's clock", () => {
    const target = ts(2026, 7, 12, 15, 0);
    expect(formatDayLabel(target, ts(2026, 7, 12, 23, 0), dict)).toBe("TODAY");
    expect(formatDayLabel(target, ts(2026, 7, 13, 1, 0), dict)).toBe(
      "YESTERDAY",
    );
    expect(formatDayLabel(target, ts(2026, 7, 14, 1, 0), dict)).toBe(
      "D:7/12/w0",
    ); // 2026-07-12 is a Sunday
  });
});

describe("formatAbsolute", () => {
  const now = ts(2026, 7, 13, 10, 0);

  it("same year → M/D HH:mm with zero-padded time (today NOT special-cased)", () => {
    expect(formatAbsolute(ts(2026, 7, 13, 9, 5), now)).toBe("7/13 09:05");
    expect(formatAbsolute(ts(2026, 7, 11, 23, 59), now)).toBe("7/11 23:59");
  });

  it("different year → YYYY/M/D HH:mm", () => {
    expect(formatAbsolute(ts(2025, 12, 31, 8, 0), now)).toBe(
      "2025/12/31 08:00",
    );
  });
});
