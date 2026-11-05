import { describe, it, expect } from "vitest";
import {
  toReplyCard,
  toReplyCardCounts,
  toReplyCardRow,
  toReplyCardSnoozeReceipt,
} from "./mappers";
import type { WireReplyCard, WireReplyCardCount, WireReplyCardRow } from "./wire";

const WIRE_TASK = {
  id: "T-584",
  type_key: "review-pr",
  title: "請示頁改版",
  priority: "high",
  due_ts: 1793375999,
  due_soon_lead_minutes: 120,
};

const wireRow = (over: Partial<WireReplyCardRow>): WireReplyCardRow =>
  ({
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要寄嗎？",
    status: "waiting",
    created_ts: 100,
    answered_ts: null,
    expired_ts: null,
    task: null,
    ...over,
  }) as WireReplyCardRow;

describe("toReplyCardRow", () => {
  it("a snoozed row naming a task → snoozedUntilTs and the task's priority, due date and own lead", () => {
    expect(toReplyCardRow(wireRow({ snoozed_until_ts: 1793000000, task: WIRE_TASK }))).toEqual({
      id: "rc-1",
      from: "mira",
      kind: "decision",
      summary: "要寄嗎？",
      status: "waiting",
      createdTs: 100,
      answeredTs: null,
      expiredTs: null,
      snoozedUntilTs: 1793000000,
      task: {
        id: "T-584",
        typeKey: "review-pr",
        title: "請示頁改版",
        priority: "high",
        dueTs: 1793375999,
        dueSoonLeadMinutes: 120,
      },
    });
  });

  it("a row without a snooze and a task without a due date → null, not undefined", () => {
    const row = toReplyCardRow(
      wireRow({ task: { id: "T-1", priority: "low" } as WireReplyCardRow["task"] })
    );
    expect(row.snoozedUntilTs).toBeNull();
    expect(row.task).toEqual({
      id: "T-1",
      typeKey: "",
      title: "",
      priority: "low",
      dueTs: null,
      dueSoonLeadMinutes: null,
    });
  });
});

describe("toReplyCard", () => {
  it("carries snoozed_until_ts and the task ref's priority and due date", () => {
    const card = toReplyCard({
      ...wireRow({}),
      body: "",
      options: [],
      select_mode: "single",
      attachments: [],
      chat_message_id: "msg-1",
      answer: null,
      snoozed_until_ts: 1793000000,
      task: WIRE_TASK,
    } as unknown as WireReplyCard);
    expect(card.snoozedUntilTs).toBe(1793000000);
    expect(card.task).toEqual({
      id: "T-584",
      typeKey: "review-pr",
      title: "請示頁改版",
      priority: "high",
      dueTs: 1793375999,
      dueSoonLeadMinutes: 120,
    });
  });
});

describe("toReplyCardCounts", () => {
  it("carries waiting_next_change_ts, null when nothing is snoozed", () => {
    expect(
      toReplyCardCounts({ waiting: 2, answered: 1, expired: 0, waiting_next_change_ts: 1793000000 })
    ).toEqual({ waiting: 2, answered: 1, expired: 0, waitingNextChangeTs: 1793000000 });
    // An older server leaves the field out.
    expect(toReplyCardCounts({ waiting: 2, answered: 1, expired: 0 } as WireReplyCardCount)).toEqual({
      waiting: 2,
      answered: 1,
      expired: 0,
      waitingNextChangeTs: null,
    });
  });
});

describe("toReplyCardSnoozeReceipt", () => {
  it("maps the stored snooze, null after an unsnooze", () => {
    expect(toReplyCardSnoozeReceipt({ id: "rc-1", snoozed_until_ts: 1793000000 })).toEqual({
      id: "rc-1",
      snoozedUntilTs: 1793000000,
    });
    expect(toReplyCardSnoozeReceipt({ id: "rc-1", snoozed_until_ts: null })).toEqual({
      id: "rc-1",
      snoozedUntilTs: null,
    });
  });
});
