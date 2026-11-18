import { describe, it, expect } from "vitest";
import { toTask, toTaskCount, toTaskPageCount, toTaskListItem } from "./mappers";
import type { WireTask, WireTaskCount, WireTaskListItem } from "./wire";

function wireTask(over: Partial<WireTask>): WireTask {
  return {
    id: "t-1",
    task_no: "T-1",
    status: "in_progress",
    priority: "mid",
    executor_kind: "staff",
    closed_ts: null,
    deps: [],
    steps: [],
    progress_done: 0,
    progress_total: 0,
    ...over,
  } as WireTask;
}

function wireListItem(over: Partial<WireTaskListItem>): WireTaskListItem {
  return {
    id: "t-1",
    task_no: "T-1",
    status: "in_progress",
    priority: "mid",
    executor_kind: "staff",
    closed_ts: null,
    deps: [],
    progress_done: 0,
    progress_total: 0,
    ...over,
  } as WireTaskListItem;
}

describe("toTask", () => {
  it("carries due_ts as dueTs and due_soon_lead_minutes as dueSoonLeadMinutes", () => {
    const task = toTask(wireTask({ due_ts: 1793375999, due_soon_lead_minutes: 10080 }));
    expect(task.dueTs).toBe(1793375999);
    expect(task.dueSoonLeadMinutes).toBe(10080);
  });

  it("reads an absent or null due_ts as no due date and an absent lead as the workspace setting", () => {
    expect(toTask(wireTask({})).dueTs).toBeNull();
    expect(toTask(wireTask({ due_ts: null })).dueTs).toBeNull();
    expect(toTask(wireTask({})).dueSoonLeadMinutes).toBeNull();
  });
});

describe("toTaskListItem", () => {
  it("carries due_ts as dueTs and due_soon_lead_minutes as dueSoonLeadMinutes", () => {
    const item = toTaskListItem(
      wireListItem({ due_ts: 1793375999, due_soon_lead_minutes: 90 })
    );
    expect(item.dueTs).toBe(1793375999);
    expect(item.dueSoonLeadMinutes).toBe(90);
  });

  it("reads an absent or null due_ts as no due date and a null lead as the workspace setting", () => {
    expect(toTaskListItem(wireListItem({})).dueTs).toBeNull();
    expect(toTaskListItem(wireListItem({ due_ts: null })).dueTs).toBeNull();
    expect(
      toTaskListItem(wireListItem({ due_soon_lead_minutes: null })).dueSoonLeadMinutes
    ).toBeNull();
  });
});

describe("toTaskCount", () => {
  it("maps the due counts and the next change moment", () => {
    const wire = {
      open: 4,
      total: 9,
      overdue: 2,
      due_soon: 1,
      due_next_change_ts: 1792857599,
    } as WireTaskCount;
    expect(toTaskCount(wire)).toEqual({
      open: 4,
      total: 9,
      overdue: 2,
      dueSoon: 1,
      dueNextChangeTs: 1792857599,
    });
  });

  it("reads an older server's missing due fields as nothing due", () => {
    expect(toTaskCount({ open: 4 } as WireTaskCount)).toEqual({
      open: 4,
      total: 0,
      overdue: 0,
      dueSoon: 0,
      dueNextChangeTs: null,
    });
  });
});

describe("toTaskPageCount", () => {
  it("maps the page's list fields beside the badge fields", () => {
    const wire: WireTaskCount = {
      open: 4,
      total: 9,
      overdue: 2,
      due_soon: 1,
      due_next_change_ts: 1792857599,
      executor_counts: { kip: 3, unassigned: 1 },
      lanes: [
        { key: "overdue", count: 2 },
        { key: "nodue", count: 5 },
      ],
      list_total: 2,
      now: 1792850000,
      list_due_next_change_ts: 1792851000,
    };
    expect(toTaskPageCount(wire)).toEqual({
      open: 4,
      total: 9,
      overdue: 2,
      dueSoon: 1,
      dueNextChangeTs: 1792857599,
      executorCounts: { kip: 3, unassigned: 1 },
      lanes: [
        { key: "overdue", count: 2 },
        { key: "nodue", count: 5 },
      ],
      listTotal: 2,
      now: 1792850000,
      listDueNextChangeTs: 1792851000,
    });
  });

  it("reads a badge-only answer as an empty page count", () => {
    expect(toTaskPageCount({ open: 4 } as WireTaskCount)).toEqual({
      open: 4,
      total: 0,
      overdue: 0,
      dueSoon: 0,
      dueNextChangeTs: null,
      executorCounts: {},
      lanes: [],
      listTotal: 0,
      now: 0,
      listDueNextChangeTs: null,
    });
  });
});
