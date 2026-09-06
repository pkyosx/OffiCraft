// Mock adapter parity for 「回覆這則」 (T-4e95). Offline preview exists so the
// cockpit can be driven without a server; that is only worth anything if the
// mock REFUSES what the server refuses. A mock that accepted any reply_to would
// let someone build and demo a reply the real server rejects — the failure would
// surface later, on a real backend, to somebody else.
//
// Pinned here, against what server/ocserverd/api_chat.go actually does:
//   • the quoted message must EXIST — the ONE refusal;
//   • it does NOT have to be in the same conversation (owner ruling,
//     2026-08-21). This was a refusal on both sides until that date, and a mock
//     that still refused it would preview a product the server no longer is;
//   • the accepted case really stores the link and serves it back;
//   • EVERY read attaches `replyToChat`, built from the log at read time,
//     unconditionally — which is the whole of the current design.

import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { mockApi, __resetMock } from "./mock";
import type { ChatMessage } from "./adapter";

/** Post a message and hand back the row the mock STORED for it.
 *
 * 🔴 T-91: `postChat` answers a RECEIPT, and this seam returns nothing at all —
 * so there is no echo left to assert against. Every claim in this file is about
 * what the mock stored and what a READ serves back, which is what the tests
 * below always meant (the file's own header says the point is that reads carry
 * the quote). The read door used is `listChat`, the same one the cockpit uses;
 * the post lands last in its own thread. */
async function post(msg: {
  to: string;
  body: string;
  replyTo?: string;
}): Promise<ChatMessage> {
  await mockApi.postChat(msg);
  const thread = await mockApi.listChat(msg.to);
  return thread[thread.length - 1];
}

// The Go constant this mock mirrors, read out of the source file rather than
// copied. See the cross-language guard at the bottom of this file.
const WIRE_GO = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "server",
  "ocserverd",
  "wire.go",
);

describe("mock 回覆這則 — server parity", () => {
  beforeEach(() => __resetMock());

  it("stores the link on the accepted case and serves it back", async () => {
    const quoted = await post({ to: "mira", body: "問題" });
    expect(quoted.replyTo ?? null).toBeNull();

    const reply = await post({
      to: "mira",
      body: "答案",
      replyTo: quoted.id,
    });
    expect(reply.replyTo).toBe(quoted.id);

    const thread = await mockApi.listChat("mira");
    expect(thread.find((m) => m.id === reply.id)?.replyTo).toBe(quoted.id);
  });

  // Found while writing the test above, and it is this feature's problem rather
  // than a tidy-up: ids used to be `mock-${Date.now()}`, so two posts inside one
  // millisecond shared an id. Nothing pointed AT a message before, so it never
  // surfaced; a reply carries the quoted id and NOTHING else, so an ambiguous id
  // resolves the quote to whichever row sorts first.
  it("mints a distinct id per message even within one millisecond", async () => {
    await Promise.all(
      Array.from({ length: 5 }, (_, i) =>
        mockApi.postChat({ to: "mira", body: `第 ${i} 則` }),
      ),
    );
    // Read back rather than echoed (T-91): the ids under test are the STORED
    // ones, which is where an ambiguous id would actually do its damage.
    const posted = await mockApi.listChat("mira");
    expect(posted).toHaveLength(5);
    expect(new Set(posted.map((m) => m.id)).size).toBe(posted.length);
  });

  it("refuses a reply_to that names no message", async () => {
    await expect(
      mockApi.postChat({ to: "mira", body: "孤兒", replyTo: "mock-nosuch" }),
    ).rejects.toThrow(/mock-nosuch/);

    // …and the refused message is not in the thread afterwards.
    const thread = await mockApi.listChat("mira");
    expect(thread.some((m) => m.body === "孤兒")).toBe(false);
  });

  it("ACCEPTS a reply_to that points at another conversation", async () => {
    // The reversal, and the reason the whole redesign exists: the owner quotes a
    // line out of another conversation to step into it. This test read
    // `.rejects.toThrow(/another conversation/)` until 2026-08-21.
    const elsewhere = await post({ to: "kye", body: "別條線" });

    const reply = await post({
      to: "mira",
      body: "側向引用",
      replyTo: elsewhere.id,
    });
    expect(reply.replyTo).toBe(elsewhere.id);
    // …and the quote crossed with it, or the reply is a pointer to nothing the
    // reader can see.
    expect(reply.replyToChat?.content).toBe("別條線");
  });

  it("attaches replyToChat on EVERY read, with no condition", async () => {
    // 🔴 SERVER PARITY IS THE POINT OF THIS TEST. The server builds the quote in
    // servedChatMessageDTO, which every read door goes through; the mock builds
    // it in mockServedChatMessage for the same reason. A mock that only attached
    // it sometimes would make offline preview show a bug — a quote flickering
    // between present and absent — that the real product does not have.
    const quoted = await post({ to: "mira", body: "被引用的那句" });
    const reply = await post({
      to: "mira",
      body: "答案",
      replyTo: quoted.id,
    });

    // ① the row the post stored, read back. Whole-object equality, so a field
    // the mock forgets to join (the recipient half arrived after the sender
    // half did) is a failure here rather than a silently thinner quote in
    // offline preview. (This used to read the POST echo; T-91 removed it, and
    // the read door is what offline preview actually renders from anyway.)
    expect(reply.replyToChat).toEqual({
      id: quoted.id,
      from: "owner",
      fromName: "",
      to: "mira",
      toName: "",
      content: "被引用的那句",
    });
    // ② the listing — unconditional, and note the quoted message is IN the
    // window too: "it is already here" is not a reason to skip it.
    const rows = await mockApi.listChat("mira");
    const row = rows.find((m) => m.id === reply.id)!;
    expect(row, "listChat must carry the reply").toBeTruthy();
    expect(row.replyToChat?.content, "listChat must carry the quote").toBe(
      "被引用的那句",
    );
    // …and a message that replies to nothing claims no quote, or a mock that
    // stamped every row would pass the line above.
    expect(rows.find((m) => m.id === quoted.id)?.replyToChat ?? null).toBeNull();
  });

  it("carries the quote on the WAKE SNAPSHOT too, and it is the read that fills its names", async () => {
    // 🔴 THE SNAPSHOT IS A THIRD READ DOOR, and it is not the same as the two
    // above. `getMemberResumeSummary` mirrors the server's own snapshot
    // assembly, which joins the quote through the same helper but hands it the
    // ROSTER — so `from_name`/`to_name` are filled there and empty on every
    // browser read (api_chat.go resumeChatMessageDTO vs servedChatMessageDTO).
    // The snapshot is also the read whose chat budget BILLS those characters,
    // so a mock that dropped the quote here would preview a card cheaper and
    // emptier than the live one — which is the exact defect T-9871 fixed on the
    // rendering side.
    const quoted = await post({ to: "mira", body: "被引用的那句" });
    const reply = await post({
      to: "mira",
      body: "答案",
      replyTo: quoted.id,
    });

    const snap = await mockApi.getMemberResumeSummary("mira");
    const row = snap.chat.find((m) => m.id === reply.id)!;
    expect(row, "the snapshot must carry the reply at all").toBeTruthy();
    // Whole-object equality: the name halves are the difference between this
    // door and the other two, so asserting only `content` would pass on the
    // shape this test exists to distinguish. "" for the owner is honest — the
    // mock roster resolves member ids, and `owner` is not a roster row.
    expect(row.replyToChat).toEqual({
      id: quoted.id,
      from: "owner",
      fromName: "",
      to: "mira",
      toName: "Mira",
      content: "被引用的那句",
    });
    // …and the browser read of the SAME message leaves those name halves empty,
    // which is what makes the line above a statement about this door.
    const listed = (await mockApi.listChat("mira")).find(
      (m) => m.id === reply.id,
    )!;
    expect(listed.replyToChat?.toName).toBe("");
    // A message that replies to nothing claims no quote here either.
    expect(
      snap.chat.find((m) => m.id === quoted.id)?.replyToChat ?? null,
    ).toBeNull();
  });

  it("shortens and flattens the quote content the way the server does", async () => {
    // The length is the SERVER's (chatReplyQuoteMaxChars = 60) and the mock
    // holds the only other copy of it. Asserted by rune count, not by a literal,
    // so it stays true for the CJK this studio is mostly written in.
    //
    // 🔴 THE BLANK LINE MUST LAND INSIDE THE 60 RUNES THAT ARE KEPT. This
    // fixture used to put it at rune 90 — past the cut — so `not.toMatch(/[\n\r]/)`
    // below was satisfied by the TRUNCATION, not by the whitespace collapse, and
    // a mock that dropped the collapse entirely would still have passed it. The
    // Go test of the same name learned this and wrote it down; this one had not
    // applied the lesson.
    const quoted = await post({
      to: "mira",
      body: "長".repeat(10) + "\n\n" + "話".repeat(30) + "   " + "短".repeat(50),
    });
    const reply = await post({
      to: "mira",
      body: "tl;dr",
      replyTo: quoted.id,
    });
    const content = reply.replyToChat!.content;
    expect(content).not.toMatch(/[\n\r]/);
    expect([...content]).toHaveLength(61); // 60 + the ellipsis
    expect(content.endsWith("\u2026")).toBe(true);
  });

  // ── the number itself, confronted with the server's ───────────────────────
  //
  // 🔴 THE DRIFT THIS CLOSES WAS SILENT AND THE COMMENTS SAID IT COULD NOT
  // HAPPEN. wire.go called `chatReplyQuoteMaxChars` "THE ONLY DEFINITION OF THAT
  // LENGTH ANYWHERE" and mock.ts called its own copy "the one place it is
  // written on this side" — both written on the same day, about the same number,
  // in two files. Neither was checked: the Go test asserts against its own
  // literal 60 and the truncation test above asserts against its own 61, so
  // changing the server to 40 and updating the Go test (which tells you to)
  // leaves this whole suite green while offline preview keeps cutting at 60.
  // A mock that previews a different product from the one that ships is exactly
  // what the mock exists to prevent.
  //
  // So the number is READ from the server source, the way errorCodes.test.ts
  // reads the shared status→code table. This does not make the mock derive its
  // constant at runtime (it cannot — there is no server in offline preview); it
  // makes the two copies unable to disagree quietly.
  //
  // ⚠️ WHAT IT DOES NOT PROMISE: it matches the literal spelling
  // `chatReplyQuoteMaxChars = <digits>` on one line. Move that constant behind an
  // expression, a build flag or another file and this guard goes quiet rather
  // than red — so it also asserts the line was FOUND, which is the half that
  // catches a rename.
  it("cuts at the same length the server does — read out of wire.go, not copied", () => {
    const src = readFileSync(WIRE_GO, "utf8");
    const m = src.match(/^const chatReplyQuoteMaxChars = (\d+)$/m);
    expect(
      m,
      "chatReplyQuoteMaxChars is no longer a plain one-line const in " +
        "server/ocserverd/wire.go — this guard reads that line, so it has just " +
        "stopped guarding. Point it at the new home before changing anything else.",
    ).toBeTruthy();
    const serverLen = Number(m![1]);
    // Re-derived through the mock's own public behaviour rather than by
    // importing the constant: what has to agree with the server is what the
    // mock SERVES, not a number sitting beside it.
    return (async () => {
      __resetMock();
      const quoted = await post({
        to: "mira",
        body: "長".repeat(serverLen + 40),
      });
      const reply = await post({
        to: "mira",
        body: "tl;dr",
        replyTo: quoted.id,
      });
      expect(
        [...reply.replyToChat!.content],
        `the mock cuts the quote at a different length from the server ` +
          `(server/ocserverd/wire.go says ${serverLen}). Offline preview would ` +
          `render a quote line the live product never produces — change ` +
          `MOCK_REPLY_QUOTE_MAX_CHARS in mock.ts to match.`,
      ).toHaveLength(serverLen + 1); // + the ellipsis
    })();
  });
});
