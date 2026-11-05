import { describe, it, expect } from "vitest";
import { toReplyCard, toReplyCardRow } from "./mappers";
import type { WireReplyCard, WireReplyCardRow } from "./wire";

const row = (over: Partial<WireReplyCardRow>): WireReplyCardRow =>
  ({ id: "rc-1", from: "mira", kind: "decision", status: "waiting", answer: null,
     answered_ts: null, expired_ts: null, ...over }) as WireReplyCardRow;
const card = (over: Partial<WireReplyCard>): WireReplyCard =>
  ({ id: "rc-1", from: "mira", kind: "decision", status: "waiting", select_mode: "single",
     answer: null, answered_ts: null, expired_ts: null, chat_message_id: "c-1", ...over }) as WireReplyCard;

describe("reply card task priority and due mapping", () => {
  it("passes the task's served priority, due date and lead through", () => {
    expect(
      toReplyCardRow(row({ priority: "high", due_ts: 1900000000, due_soon_lead_minutes: 90 }))
    ).toMatchObject({ priority: "high", dueTs: 1900000000, dueSoonLeadMinutes: 90 });
    expect(toReplyCard(card({ priority: "low", due_ts: 1800000000 }))).toMatchObject({
      priority: "low", dueTs: 1800000000, dueSoonLeadMinutes: null,
    });
  });

  it("keeps a card naming no task without a priority or due date, never inventing mid", () => {
    const none = { priority: null, dueTs: null, dueSoonLeadMinutes: null };
    expect(toReplyCardRow(row({}))).toMatchObject(none);
    expect(toReplyCard(card({ priority: null, due_ts: null }))).toMatchObject(none);
  });
});
