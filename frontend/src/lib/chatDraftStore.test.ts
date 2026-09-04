// chatDraftStore.test.ts — the store that OUTLIVES a room (T-48, R13-4).
//
// 🔴 WHY THIS FILE EXISTS. `ChatArea` is mounted under `key={peerId}`, so every
// piece of per-conversation state dies with the room. This module is the ONE
// deliberate exception: a draft and its staged files have to survive leaving the
// room, because that is what a draft IS.
//
// So the rule this file keeps is narrow and mechanical: NOTHING IN THIS MODULE
// MAY BE MUTABLE STATE THAT IS NOT KEYED BY PEER. A `let lastError` here is one
// room's sentence shown in another room, and no component test would go red.
//
// 🔴 WHAT THIS FILE CANNOT KEEP, AND WHO KEEPS IT (T-48, R14-3.1). The store's
// header claims everything surviving a room switch now lives here. This test
// cannot check that claim: it reads THIS file, the one file already obeying it.
// The instance that actually happened was `liveComposers` — a second
// module-level, peer-keyed table grown in `ChatArea.tsx` — and a one-file
// census is blind to it by construction. `lint:async-landing`'s MODULE_STATE
// register is the census over the whole chat import graph; this one is the
// narrow rule for the file that is the exception.

import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  getChatDraft,
  getChatDraftAttachments,
  getChatAttachError,
  resetChatDrafts,
  saveChatDraftText,
  setChatAttachError,
  openChatAttachErrorScope,
  subscribeChatDraft,
  updateChatDraftAttachments,
} from "./chatDraftStore";

const SOURCE = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), "chatDraftStore.ts"),
  "utf8",
);

const A = "m-aaa";
const B = "m-bbb";

beforeEach(() => resetChatDrafts());

describe("chatDraftStore", () => {
  it("keeps every room's draft, attachments and error to itself", () => {
    saveChatDraftText(A, "for A", "c-1");
    saveChatDraftText(B, "for B");
    updateChatDraftAttachments(A, () => [
      { id: "att-a", name: "a.png" } as never,
    ]);
    setChatAttachError(B, "太大了");

    expect(getChatDraft(A)).toMatchObject({ text: "for A", replyTo: "c-1" });
    expect(getChatDraft(B)).toMatchObject({ text: "for B" });
    expect(getChatDraft(B)?.replyTo).toBeUndefined();
    expect(getChatDraftAttachments(A)).toHaveLength(1);
    expect(getChatDraftAttachments(B)).toHaveLength(0);
    expect(getChatAttachError(A)).toBeNull();
    expect(getChatAttachError(B)).toBe("太大了");
  });

  it("notifies only the room that changed", () => {
    let seenA = 0;
    let seenB = 0;
    subscribeChatDraft(A, () => seenA++);
    subscribeChatDraft(B, () => seenB++);

    saveChatDraftText(A, "typing");
    setChatAttachError(A, "太大了");

    expect(seenA).toBe(2);
    expect(seenB).toBe(0);
  });

  it("hands back one frozen empty list, so a subscriber does not re-render forever", () => {
    // useSyncExternalStore re-renders whenever the snapshot's identity changes;
    // a fresh [] per read is an infinite loop rather than a wrong pixel.
    expect(getChatDraftAttachments(A)).toBe(getChatDraftAttachments(B));
  });

  it("keeps a refusal alive while any chat surface still holds the scope open", () => {
    // The scope is a COUNT, not a flag: closing one surface while another is
    // still up must not drop the notices the second one is showing. Nothing
    // mounts two office pages today, so no component test reaches this — it is
    // the half of `openChatAttachErrorScope` React cannot exercise.
    setChatAttachError(A, "圖片太大");
    const closeFirst = openChatAttachErrorScope();
    const closeSecond = openChatAttachErrorScope();

    closeFirst();
    return Promise.resolve().then(() => {
      expect(getChatAttachError(A)).toBe("圖片太大");
      closeSecond();
      return Promise.resolve().then(() => {
        expect(getChatAttachError(A)).toBeNull();
      });
    });
  });

  it("does not drop a refusal raised after the last surface closed", () => {
    // The close is deferred, so a `FileReader` landing in that gap would be
    // swept up by a decision taken before it existed. The doomed list is
    // captured at close time for exactly this.
    const close = openChatAttachErrorScope();
    close();
    setChatAttachError(A, "圖片太大");
    return Promise.resolve().then(() => {
      expect(getChatAttachError(A)).toBe("圖片太大");
    });
  });

  it("declares no module-level mutable state that is not keyed by peer", () => {
    // The census, reduced to the one rule that matters here. A top-level `let`
    // or `var` in this module is state shared by every room — the exact shape
    // `key={peerId}` was introduced to delete, reappearing where no component
    // test can see it. Keyed containers (Map/Set) and frozen constants are the
    // permitted forms.
    //
    // 🔴 THE ONE EXCEPTION, NAMED RATHER THAN INFERRED (R16 D-2). The chat
    // page's notice SCOPE needs two plain counters (how many surfaces are open,
    // and which close a deferred sweep belongs to). A number cannot hold a
    // peer's value, so it is not the shape this rule bans — but "it's only a
    // number" is a judgement, so each one is listed here by name and pinned to
    // a numeric initializer. A `let` holding anything else, or a name not on
    // this list, is still the banned shape.
    const COUNTERS = ["attachErrorScopes", "scopeEpoch"];
    const topLevelMutable = [
      ...SOURCE.matchAll(/^(?:let|var)\s+(\w+)\s*(?::[^=]+)?=\s*(.*)$/gm),
    ];
    const bare = SOURCE.split("\n").filter(
      (line) => /^(let|var)\s/.test(line) && !/=/.test(line),
    );
    expect(bare, "a top-level `let` with no initializer holds anything at all").toEqual([]);
    expect(topLevelMutable.map(([, name]) => name).sort()).toEqual([...COUNTERS].sort());
    for (const [, name, init] of topLevelMutable) {
      expect(
        /^-?\d+;?$/.test(init.trim()),
        `${name} is a top-level \`let\` that is not a plain counter: ${init}`,
      ).toBe(true);
    }

    const topLevelConsts = [...SOURCE.matchAll(/^const\s+(\w+)\s*(?::[^=]+)?=\s*(.*)$/gm)];
    expect(topLevelConsts.length).toBeGreaterThan(0);
    for (const [, name, init] of topLevelConsts) {
      expect(
        /^new (Map|Set)[(<]/.test(init) || /^Object\.freeze\(/.test(init),
        `${name} is module-level state that is neither keyed by peer (Map/Set) nor frozen: ${init}`,
      ).toBe(true);
    }
  });
});
