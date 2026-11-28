import { describe, it, expect } from "vitest";
import {
  dueBucketOf,
  dueState,
  dueTsFromInputs,
  effectiveLeadMinutes,
  formatDue,
  isDueSoon,
  leadMinutesFromDraft,
  nextDueChangeAmong,
  nextDueChangeTs,
  splitLeadMinutes,
  toTimeInputValue,
} from "./dueDate";

// 2026-10-24 10:00:00 Asia/Taipei (the suite's TZ).
const NOW = 1792807200;
const OCT_30_END = 1793375999;
const OCT_30_1430 = 1793341800;
const OCT_22_1430 = 1792650600;

describe("effectiveLeadMinutes", () => {
  it("answers the task's own lead when it has one", () => {
    expect(effectiveLeadMinutes(10080, 2880)).toBe(10080);
  });

  it("answers the workspace lead for a task without one", () => {
    expect(effectiveLeadMinutes(null, 2880)).toBe(2880);
    expect(effectiveLeadMinutes(undefined, 2880)).toBe(2880);
  });
});

describe("splitLeadMinutes", () => {
  it("answers the largest unit that divides the lead exactly", () => {
    expect(splitLeadMinutes(1440)).toEqual({ amount: 1, unit: "day" });
    expect(splitLeadMinutes(43200)).toEqual({ amount: 30, unit: "day" });
    expect(splitLeadMinutes(120)).toEqual({ amount: 2, unit: "hour" });
    expect(splitLeadMinutes(1500)).toEqual({ amount: 25, unit: "hour" });
    expect(splitLeadMinutes(90)).toEqual({ amount: 90, unit: "minute" });
  });
});

describe("leadMinutesFromDraft", () => {
  it("converts a whole amount in its unit to minutes and an empty draft to null", () => {
    expect(leadMinutesFromDraft("2", "hour")).toBe(120);
    expect(leadMinutesFromDraft("1", "day")).toBe(1440);
    expect(leadMinutesFromDraft("30", "day")).toBe(43200);
    expect(leadMinutesFromDraft("1", "minute")).toBe(1);
    expect(leadMinutesFromDraft("", "day")).toBeNull();
  });

  it("refuses 0, a fraction, anything past 30 days and unparseable text", () => {
    expect(leadMinutesFromDraft("0", "minute")).toBeUndefined();
    expect(leadMinutesFromDraft("1.5", "hour")).toBeUndefined();
    expect(leadMinutesFromDraft("43201", "minute")).toBeUndefined();
    expect(leadMinutesFromDraft("31", "day")).toBeUndefined();
    expect(leadMinutesFromDraft("721", "hour")).toBeUndefined();
    expect(leadMinutesFromDraft("e", "day")).toBeUndefined();
  });
});

describe("isDueSoon", () => {
  it("is true from exactly the lead's minutes before the due date, and false a second earlier", () => {
    expect(isDueSoon(NOW + 90 * 60, NOW, 90)).toBe(true);
    expect(isDueSoon(NOW + 90 * 60 + 1, NOW, 90)).toBe(false);
  });
});

describe("dueState", () => {
  it("is soon inside the lead, later outside it, and counts calendar days once overdue", () => {
    expect(dueState(OCT_30_END, NOW, 10080)).toEqual({ kind: "soon" });
    expect(dueState(OCT_30_END, NOW, 4320)).toEqual({ kind: "later" });
    expect(dueState(OCT_30_END, OCT_30_END + 2 * 86_400, 4320)).toEqual({ kind: "overdue", days: 2 });
    expect(dueState(OCT_22_1430, NOW, 4320)).toEqual({ kind: "overdue", days: 2 });
  });
});

describe("dueBucketOf", () => {
  it("is nodue without a date, later before the lead, soon from the lead's first second, overdue from the due second", () => {
    const due = NOW + 2 * 3600;
    expect([
      dueBucketOf(null, NOW, 60),
      dueBucketOf(due, due - 3601, 60),
      dueBucketOf(due, due - 3600, 60),
      dueBucketOf(due, due - 1, 60),
      dueBucketOf(due, due, 60),
    ]).toEqual(["nodue", "later", "soon", "soon", "overdue"]);
  });
});

describe("nextDueChangeAmong", () => {
  it("is the earliest band change across the items, each judged by its own lead else the workspace's, skipping undated ones", () => {
    const items = [
      { dueTs: null },
      { dueTs: NOW + 10 * 3600, dueSoonLeadMinutes: null },
      { dueTs: NOW + 10 * 3600, dueSoonLeadMinutes: 300 },
    ];
    expect(nextDueChangeAmong(items, NOW, 60)).toBe(NOW + 5 * 3600);
    expect(nextDueChangeAmong(items.slice(0, 2), NOW, 60)).toBe(NOW + 9 * 3600);
    expect(nextDueChangeAmong([{ dueTs: null }], NOW, 60)).toBeNull();
  });
});

describe("nextDueChangeTs", () => {
  it("is the start of the lead window, then the due moment", () => {
    expect(nextDueChangeTs(OCT_30_END, NOW, 4320)).toBe(OCT_30_END - 3 * 86_400);
    expect(nextDueChangeTs(OCT_30_END, OCT_30_END - 60, 4320)).toBe(OCT_30_END);
  });
});

describe("formatDue", () => {
  it("writes a due moment with its time, a date-only due without one, the year only in another year", () => {
    expect(formatDue(OCT_30_1430, NOW)).toBe("10/30 14:30");
    expect(formatDue(OCT_30_END, NOW)).toBe("10/30");
    expect(formatDue(OCT_22_1430 - 365 * 86400, NOW)).toBe("2025/10/22 14:30");
  });
});

describe("dueTsFromInputs", () => {
  it("is 23:59:59 local of the date without a time, and that local minute with one", () => {
    expect(dueTsFromInputs("2026-10-30", "")).toBe(OCT_30_END);
    expect(dueTsFromInputs("2026-10-30", "14:30")).toBe(OCT_30_1430);
    expect(dueTsFromInputs("", "14:30")).toBeNull();
  });
});

describe("toTimeInputValue", () => {
  it("is empty for 23:59:59 local and HH:MM otherwise", () => {
    expect(toTimeInputValue(OCT_30_END)).toBe("");
    expect(toTimeInputValue(OCT_30_1430)).toBe("14:30");
    expect(toTimeInputValue(OCT_30_END - 59)).toBe("23:59");
  });
});
