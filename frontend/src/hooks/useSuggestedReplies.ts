// hooks/useSuggestedReplies.ts — the 建議回覆 the composer offers (T-122).
//
// Same seam as useOrgName / useOwnerName: the value lives on `/api/settings`
// and is read through `loadServerSettings`, never through a direct settings
// call of its own (frontend/.claude/rules/data-layer.md: 所有 mount 都經
// loadServerSettings). Several composers can be on screen at once — every card
// on 等我回覆, every expanded task on 任務 — and the shared snapshot merges
// them into the one request the page was already making.
//
// A failed load answers the EMPTY list, which renders no chips. That is the
// same answer as "the owner configured none", and it is the right one: the
// suggestions are a convenience laid over a reply box that must keep working
// on its own. Nothing here can take the box down, and nothing here invents a
// suggestion the owner did not write.

import { useEffect, useState } from "react";
import { loadServerSettings } from "./sharedServerSettings";

/** The owner's suggested replies, or `[]` until (or unless) they arrive. */
export function useSuggestedReplies(): string[] {
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
          if (alive) setReplies(s.suggestedReplies);
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
  }, []);

  return replies;
}
