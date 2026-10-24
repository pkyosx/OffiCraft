import { describe, it, expect } from "vitest";
import { toTask, toTaskCount, toTaskListItem } from "./mappers";
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
  it("carries due_ts as dueTs and due_soon_lead_days as dueSoonLeadDays", () => {
    const task = toTask(wireTask({ due_ts: 1793375999, due_soon_lead_days: 7 }));
    expect(task.dueTs).toBe(1793375999);
    expect(task.dueSoonLeadDays).toBe(7);
  });

  it("reads an absent or null due_ts as no due date and an absent lead as the workspace setting", () => {
    expect(toTask(wireTask({})).dueTs).toBeNull();
    expect(toTask(wireTask({ due_ts: null })).dueTs).toBeNull();
    expect(toTask(wireTask({})).dueSoonLeadDays).toBeNull();
  });
});

describe("toTaskListItem", () => {
  it("carries due_ts as dueTs and due_soon_lead_days as dueSoonLeadDays", () => {
    const item = toTaskListItem(
      wireListItem({ due_ts: 1793375999, due_soon_lead_days: 3 })
    );
    expect(item.dueTs).toBe(1793375999);
    expect(item.dueSoonLeadDays).toBe(3);
  });

  it("reads an absent or null due_ts as no due date and a null lead as the workspace setting", () => {
    expect(toTaskListItem(wireListItem({})).dueTs).toBeNull();
    expect(toTaskListItem(wireListItem({ due_ts: null })).dueTs).toBeNull();
    expect(
      toTaskListItem(wireListItem({ due_soon_lead_days: null })).dueSoonLeadDays
    ).toBeNull();
  });
});

describe("toTaskCount", () => {
  it("maps the due counts and the next change moment", () => {
    const wire: WireTaskCount = {
      open: 4,
      total: 9,
      overdue: 2,
      due_soon: 1,
      due_next_change_ts: 1792857599,
    };
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
