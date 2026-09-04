// mappers — `reply_to` survives the wire→view crossing (T-4e95 r15).
//
// `toChatMessage` is the ONLY place the reply link crosses from the real
// server's JSON into the view model, and every real read goes through it:
// listChat, postChat and the wake snapshot alike. Turn
// that one line into `replyTo: null` and the whole feature is gone in the real
// app — every quote row, every banner, every jump.
//
// It had 2003 tests around it and NOT ONE of them went red on that change. The
// mock adapter builds its own view objects, so it never crosses this line; the
// one parity test that does feed it a wire message chose `reply_to: ""` — the
// single value for which the correct mapping and the broken one agree. That is
// the ninth time in this package that a new guard could not see the failure it
// was added for, which is why this file states the value explicitly.

import { describe, it, expect } from "vitest";
import { toChatMessage } from "./mappers";

const BASE = {
  id: "c-2",
  from: "owner",
  to: "m1",
  body: "回你這句",
  ts: 2,
  meta: {},
  reply_card_id: "",
  reply_card_status: "",
  body_omitted_chars: 0,
} as unknown as Parameters<typeof toChatMessage>[0];

describe("toChatMessage · reply_to", () => {
  it("carries a real id through to the view model", () => {
    expect(toChatMessage({ ...BASE, reply_to: "c-1" }).replyTo).toBe("c-1");
  });

  it("reads the server's empty string as 'replies to nothing'", () => {
    // The wire has no null: a message that answers nothing sends "". Both
    // directions matter — this one is what keeps every ordinary message from
    // rendering an empty quote row.
    expect(toChatMessage({ ...BASE, reply_to: "" }).replyTo).toBeNull();
  });
});

// ── reply_to_chat (T-4e95, owner ruling 2026-08-21) ──────────────────────────
//
// The SECOND thing that crosses here, and the one the whole redesign turns on:
// the quoted message travels WITH the reply instead of being fetched. Same
// argument as above — this is the only crossing, every real read uses it, and
// nothing else in the package would notice it turning into `null`.
describe("toChatMessage · reply_to_chat", () => {
  it("carries the server's quote through to the view model, field for field", () => {
    expect(
      toChatMessage({
        ...BASE,
        reply_to: "c-1",
        reply_to_chat: {
          id: "c-1",
          from: "m1",
          from_name: "小佩",
          // 🔴 A RECIPIENT THAT IS NOT THIS MESSAGE'S PEER. BASE is owner→m1;
          // the quoted line is m1→m2, i.e. out of ANOTHER conversation, which
          // is the shape the wire exists to carry (owner ruling 2026-08-21).
          // Using "owner" here would make a mapper that read the WRONG field —
          // the enclosing message's `to` — pass.
          to: "m2",
          to_name: "阿凱",
          content: "被引用的那一行",
        },
      } as unknown as Parameters<typeof toChatMessage>[0]).replyToChat,
    ).toEqual({
      id: "c-1",
      from: "m1",
      fromName: "小佩",
      to: "m2",
      toName: "阿凱",
      content: "被引用的那一行",
    });
  });

  it("reads an ABSENT quote as null while reply_to survives", () => {
    // The state the server documents as "the original is gone": the key is
    // omitted, `reply_to` is not. They must arrive as two separate facts —
    // collapsing them (clearing `replyTo` too) is what would turn a reply whose
    // original is gone into an ordinary message, silently.
    const m = toChatMessage({ ...BASE, reply_to: "c-longgone" });
    expect(m.replyToChat).toBeNull();
    expect(m.replyTo).toBe("c-longgone");
  });

  it("reads an EMPTY content as an empty string, never as a missing quote", () => {
    // An attachment-only original. "" and absent are different answers and the
    // UI renders them differently — a named empty line vs one fixed sentence.
    const m = toChatMessage({
      ...BASE,
      reply_to: "c-photo",
      reply_to_chat: { id: "c-photo", from: "m1", to: "owner" },
    } as unknown as Parameters<typeof toChatMessage>[0]);
    expect(m.replyToChat).not.toBeNull();
    expect(m.replyToChat!.content).toBe("");
    expect(m.replyToChat!.fromName).toBe("");
    expect(m.replyToChat!.toName).toBe("");
  });
});
