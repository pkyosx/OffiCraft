// hooks/useSuggestedReplies.ts — the 建議回覆 the two reply boxes offer (T-122).
//
// TWO LISTS, TWO HOOKS: `useSuggestedRepliesReplyCard` for the 請示卡 composer,
// `useSuggestedRepliesTaskMessage` for the 任務 message box. The owner ruled the
// two boxes get separate lists — answering a 請示卡 and writing to a task in
// progress are different conversations — so a box reads ITS list and only its
// list. Whichever one is empty, the other still renders.
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

import { useEffect, useState } from "react";
import { loadServerSettings } from "./sharedServerSettings";

type SuggestedRepliesBox = "replyCard" | "taskMessage";

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
          if (alive) {
            setReplies(
              box === "replyCard"
                ? s.suggestedRepliesReplyCard
                : s.suggestedRepliesTaskMessage
            );
          }
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
