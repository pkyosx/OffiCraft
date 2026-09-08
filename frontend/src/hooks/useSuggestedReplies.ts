// hooks/useSuggestedReplies.ts — the 建議回覆 the two reply boxes offer (T-122).
//
// ONE LIST PER BOX, ONE HOOK PER LIST: `useSuggestedRepliesReplyCard` for the
// 請示卡 composer, `useSuggestedRepliesTaskMessage` for the 任務 message box, and
// `useSuggestedRepliesLoreMessage` for the 傳承 entry's message box (T-33). The
// owner ruled the boxes get separate lists — answering a 請示卡, writing to a
// task in progress and asking a 傳承 entry's writer about it are different
// conversations — so a box reads ITS list and only its list. Whichever ones are
// empty, the others still render.
//
// Same seam as useOrgName / useOwnerName: the values live on `/api/settings` and
// are read through `loadServerSettings`, never through a direct settings call of
// their own (frontend/.claude/rules/data-layer.md: 所有 mount 都經
// loadServerSettings). Several composers can be on screen at once — every card
// on 等我回覆, every expanded task on 任務 — and the shared snapshot merges them
// into the one request the page was already making.
//
// 🔴 ONE LOADER BEHIND BOTH HOOKS, deliberately. Two copies of the effect would
// be two async landing points where the census (scripts/check-async-landing
// -points.mjs) records one, and two places to get the failure path wrong.
//
// A failed load answers the EMPTY list, which renders no chips. That is the same
// answer as "the owner configured none", and it is the right one: the
// suggestions are a convenience laid over a reply box that must keep working on
// its own. Nothing here can take the box down, and nothing here invents a
// suggestion the owner did not write.
//
// 🔴 "NOTHING HERE CAN TAKE THE BOX DOWN" COVERS THE RETURN VALUE, NOT JUST THE
// CALL. It did not always: the try/catch below guards a read that THROWS or
// REJECTS, and for a while that was the whole promise — a read that RESOLVED an
// object whose lists were absent, null, or a string sailed straight through and
// killed the box during render instead (`replies.length` of undefined). Same
// damage as commit 7d4e5874, one door along. `listFrom` closes that door, and
// the sentence above is only true because it is there.

import { useEffect, useState } from "react";
import { loadServerSettings } from "./sharedServerSettings";

type SuggestedRepliesBox = "replyCard" | "taskMessage" | "loreMessage";

/** box → the `ServerSettingsView` property carrying that box's list. Spelled as
 * literals for the reason the block below explains; the map is here so adding a
 * box is one row rather than another arm of a nested ternary. */
const SETTINGS_FIELD: Record<SuggestedRepliesBox, string> = {
  replyCard: "suggestedRepliesReplyCard",
  taskMessage: "suggestedRepliesTaskMessage",
  loreMessage: "suggestedRepliesLoreMessage",
};

/** ⚠️ THE FIELD NAMES BELOW ARE STRING LITERALS, DELIBERATELY, AND THAT COSTS
 * SOMETHING. Reading a value typed `unknown` cannot also be bound to
 * `ServerSettingsView`'s property names, so renaming either field will NOT turn
 * this file red — `useSuggestedReplies.test.tsx` is what goes red instead. The
 * guard moved from the source to the test; it did not disappear. Reviewed and
 * accepted rather than overlooked: binding the names back would mean tangling a
 * lenient parse of `unknown` with a type-checked read, for less than it costs.
 *
 * The named list off a settings view, or `[]` for ANY shape that is not a list
 * of sentences — an absent field, null, a string, an array of non-strings. The
 * structural read is deliberate: this value has travelled through the wire and
 * a mapper, and "I cannot read it" and "there are none" are the same answer to
 * the only question the caller asks. */
function listFrom(s: unknown, box: SuggestedRepliesBox): string[] {
  if (typeof s !== "object" || s === null) return [];
  const raw = (s as Record<string, unknown>)[SETTINGS_FIELD[box]];
  if (!Array.isArray(raw)) return [];
  // Blanks go too, not just non-strings: a whitespace-only entry renders as an
  // unlabelled but CLICKABLE chip that pastes whitespace into the box. Same rule
  // as the wire reader (api/suggestedReplies.ts) and as the component's own
  // guard — both layers, for the reason F-1 established.
  return raw
    .filter((v): v is string => typeof v === "string")
    .map((v) => v.trim())
    .filter((v) => v.length > 0);
}

function useSuggestedReplies(box: SuggestedRepliesBox): string[] {
  const [replies, setReplies] = useState<string[]>([]);

  useEffect(() => {
    let alive = true;
    // Both failure modes, deliberately: the read can REJECT (the server said
    // no) and it can THROW SYNCHRONOUSLY (measured — a caller that swapped the
    // api client for a partial one has no getServerSettings at all, and an
    // effect that throws takes the whole card down with it, message box
    // included). The paragraph above promises this hook cannot do that, and
    // one `.catch` alone does not keep the promise.
    try {
      loadServerSettings()
        .then((s) => {
          if (alive) setReplies(listFrom(s, box));
        })
        .catch((e) => {
          console.warn("useSuggestedReplies: load failed", e);
        });
    } catch (e) {
      console.warn("useSuggestedReplies: load threw", e);
    }
    return () => {
      alive = false;
    };
  }, [box]);

  return replies;
}

/** The owner's 建議回覆 for a 請示卡 reply box, or `[]` until (or unless) they
 * arrive. */
export function useSuggestedRepliesReplyCard(): string[] {
  return useSuggestedReplies("replyCard");
}

/** The owner's 建議回覆 for a 任務 message box, or `[]` until (or unless) they
 * arrive. Independent of the reply-card list. */
export function useSuggestedRepliesTaskMessage(): string[] {
  return useSuggestedReplies("taskMessage");
}

/** The owner's 建議回覆 for a 傳承 entry's message box (T-33), or `[]` until (or
 * unless) they arrive. Independent of the other two lists. */
export function useSuggestedRepliesLoreMessage(): string[] {
  return useSuggestedReplies("loreMessage");
}
