import { describe, it, expect } from "vitest";
import { toReplyCard, toReplyCardRow } from "./mappers";
import type { WireReplyCard, WireReplyCardRow } from "./wire";

const row = (over: Partial<WireReplyCardRow>): WireReplyCardRow =>
  ({ id: "rc-1", from: "mira", kind: "decision", status: "waiting", answer: null,
     answered_ts: null, expired_ts: null, ...over }) as WireReplyCardRow;
const card = (over: Partial<WireReplyCard>): WireReplyCard =>
  ({ id: "rc-1", from: "mira", kind: "decision", status: "waiting", select_mode: "single",
     answer: null, answered_ts: null, expired_ts: null, chat_message_id: "c-1", ...over }) as WireReplyCard;

describe("reply card priority and due mapping", () => {
  it("passes the served priority and due date through", () => {
    expect(toReplyCardRow(row({ priority: "high", due_ts: 1900000000 }))).toMatchObject({
      priority: "high", dueTs: 1900000000,
    });
    expect(toReplyCard(card({ priority: "low", due_ts: 1800000000 }))).toMatchObject({
      priority: "low", dueTs: 1800000000,
    });
  });

  it("reads a server that does not serve them yet as mid with no due date", () => {
    expect(toReplyCardRow(row({}))).toMatchObject({ priority: "mid", dueTs: null });
    expect(toReplyCard(card({ due_ts: null }))).toMatchObject({ priority: "mid", dueTs: null });
  });
});
