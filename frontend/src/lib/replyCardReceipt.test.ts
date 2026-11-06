import { describe, it, expect } from "vitest";
import { mergeReplyCardRowWrite, mergeReplyCardWrite, secretCardMayStillChange } from "./replyCardReceipt";
import type { ReplyCard, ReplyCardRow, ReplyCardSecretView, ReplyCardWriteReceipt } from "../api/adapter";

const card = (secret: ReplyCardSecretView | null): ReplyCard => ({
  id: "rc-1",
  from: "mira",
  kind: secret ? "secret" : "decision",
  summary: "s",
  body: "b",
  options: [],
  selectMode: "single",
  status: "waiting",
  attachments: [],
  createdTs: 1,
  answeredTs: null,
  chatMessageId: "m",
  answer: null,
  secret,
});

const receipt = (over: Partial<ReplyCardWriteReceipt>): ReplyCardWriteReceipt => ({
  id: "rc-1",
  status: "answered",
  answer: { optionIdxs: null, text: "", attachments: [] },
  answeredTs: 50,
  expiredTs: null,
  ...over,
});

const waiting: ReplyCardSecretView = { state: "waiting", providedTs: null, retrievedTs: null, expiresTs: null };
const provided: ReplyCardSecretView = { state: "provided", providedTs: 50, retrievedTs: null, expiresTs: 150 };
const retrieved: ReplyCardSecretView = { state: "retrieved", providedTs: 50, retrievedTs: 60, expiresTs: 70 };

describe("mergeReplyCardWrite on a secret card", () => {
  it("an empty answer is a provide", () => {
    expect(mergeReplyCardWrite(card(waiting), receipt({})).secret).toEqual({
      state: "provided", providedTs: 50, retrievedTs: null, expiresTs: null,
    });
  });

  it("a worded answer is a reply", () => {
    const merged = mergeReplyCardWrite(
      card(waiting),
      receipt({ answer: { optionIdxs: null, text: "no", attachments: [] } }),
    );
    expect(merged.secret).toEqual({ ...waiting, state: "replied" });
  });

  it("an expiry marks it expired and forgets the deadline", () => {
    const merged = mergeReplyCardWrite(card(provided), receipt({ status: "expired", answer: null, answeredTs: null, expiredTs: 60 }));
    expect(merged.secret).toEqual({ ...provided, state: "expired", expiresTs: null });
  });

  it("ending the retention keeps it retrieved and clears the deadline", () => {
    expect(mergeReplyCardWrite(card(retrieved), receipt({})).secret).toEqual({ ...retrieved, expiresTs: null });
  });

  it("other kinds grow no secret field", () => {
    expect("secret" in mergeReplyCardWrite({ ...card(null), secret: undefined }, receipt({}))).toBe(true);
    expect(mergeReplyCardWrite({ ...card(null), secret: undefined }, receipt({})).secret).toBeUndefined();
  });
});

describe("mergeReplyCardRowWrite on a secret card", () => {
  const row = (secret: ReplyCardSecretView): ReplyCardRow => ({
    id: "rc-1", from: "mira", kind: "secret", summary: "s", status: "answered", createdTs: 1, answeredTs: 50, secret,
  });

  it("moves the row's state with the write, which is what re-reads an open card", () => {
    expect(mergeReplyCardRowWrite(row(retrieved), receipt({})).secret).toEqual({ ...retrieved, expiresTs: null });
  });
});

describe("secretCardMayStillChange", () => {
  it.each([
    [waiting, false],
    [provided, true],
    [retrieved, true],
    [{ ...retrieved, expiresTs: null }, false],
    [{ ...waiting, state: "replied" as const }, false],
    [{ ...waiting, state: "expired" as const }, false],
  ])("%j → %s", (secret, want) => {
    expect(secretCardMayStillChange(card(secret))).toBe(want);
  });

  it("a card of another kind never does", () => {
    expect(secretCardMayStillChange(card(null))).toBe(false);
    expect(secretCardMayStillChange(null)).toBe(false);
  });
});
