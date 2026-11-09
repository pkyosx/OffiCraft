import { describe, it, expect } from "vitest";
import { toChatMessage } from "./mappers";

type Wire = Parameters<typeof toChatMessage>[0];

const BASE = {
  id: "c-2",
  from: "owner",
  to: "m1",
  body: "回你這句",
  ts: 2,
  meta: {},
  reply_card_status: "",
  body_omitted_chars: 0,
  recalled_ts: 0,
  reply_to: "",
} as unknown as Wire;

describe("toChatMessage · recall", () => {
  it("carries recalled_ts across, 0 meaning not recalled", () => {
    expect(toChatMessage({ ...BASE, recalled_ts: 1786000123.5 }).recalledTs).toBe(1786000123.5);
    expect(toChatMessage(BASE).recalledTs).toBe(0);
  });

  it("reads meta.task_id as the message's task, null when absent or empty", () => {
    expect(toChatMessage({ ...BASE, meta: { task_id: "T-12" } }).taskId).toBe("T-12");
    expect(toChatMessage({ ...BASE, meta: { task_id: "" } }).taskId).toBeNull();
    expect(toChatMessage(BASE).taskId).toBeNull();
  });

  it("carries a quote's recalled flag, with the empty content the server sends", () => {
    const quote = (recalled: boolean, content: string) =>
      toChatMessage({
        ...BASE,
        reply_to: "c-1",
        reply_to_chat: { id: "c-1", from: "owner", from_name: "", to: "m1", to_name: "", content, recalled },
      } as Wire).replyToChat;
    expect(quote(true, "")).toEqual({
      id: "c-1",
      from: "owner",
      fromName: "",
      to: "m1",
      toName: "",
      content: "",
      recalled: true,
    });
    expect(quote(false, "原句")).toEqual({
      id: "c-1",
      from: "owner",
      fromName: "",
      to: "m1",
      toName: "",
      content: "原句",
      recalled: false,
    });
  });
});
