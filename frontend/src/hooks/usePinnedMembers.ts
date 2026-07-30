// hooks/usePinnedMembers.ts — the owner's manually pinned roster members
// (T-ed38, display.pinned_member_ids behind /api/settings).
//
// SERVER-ONLY, deliberately: no localStorage cache. The same reasoning
// useOrgName wrote down — the server is the single source of truth, so a stale
// per-browser copy could only mislead. Here it would be worse than misleading:
// a cached pin for a member the owner unpinned (or dismissed) on another device
// would briefly resurrect a row at the top of the roster.
//
// Owner-only surface: the whole cockpit is owner-authed, and /api/settings is
// governance-gated (owner / admin agent, T-6020), so this write path is
// reachable here and nowhere an ordinary agent runs.
//
// The cross-device promise is deliberately modest: pins agree after a reload /
// login. There is no settings SSE topic, so a pin made on another device does
// NOT appear live here — that would be a separate ticket.

import { useCallback, useEffect, useState } from "react";
import { api } from "../api";

interface UsePinnedMembers {
  /** The pinned member ids in DISPLAY order (newest pin first). `[]` is both
   * "nothing pinned" and the honest degradation when the settings read failed —
   * the roster then renders exactly as it did before this feature. */
  pinnedMemberIds: string[];
  /** Pin a member: it goes to the FRONT (newest pin on top). A no-op if it is
   * already pinned, so a double click cannot duplicate an id (the server 422s
   * duplicates, which would surface as a spurious failed write). */
  pin: (memberId: string) => void;
  /** Unpin a member. A no-op if it was not pinned. */
  unpin: (memberId: string) => void;
}

export function usePinnedMembers(): UsePinnedMembers {
  const [pinnedMemberIds, setPinned] = useState<string[]>([]);

  useEffect(() => {
    let alive = true;
    api
      .getServerSettings()
      .then((s) => {
        if (alive) setPinned(s.pinnedMemberIds);
      })
      .catch((e) => {
        // Degrade honestly to "nothing pinned" — that is the state the roster
        // already renders correctly. Never blank the list, never retry forever.
        console.warn("usePinnedMembers: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, []);

  // One writer for both verbs: optimistic update, then adopt the server's echo,
  // and snap back to the last confirmed value on rejection (the useOrgName
  // pattern). The whole set is replaced — the server never merges, because two
  // devices merging would leave the ORDER undefined.
  const save = useCallback((prev: string[], next: string[]) => {
    setPinned(next);
    api
      .patchServerSettings({ pinnedMemberIds: next })
      .then((s) => setPinned(s.pinnedMemberIds))
      .catch((e) => {
        console.warn("usePinnedMembers: save failed", e);
        setPinned(prev);
      });
  }, []);

  // Both verbs read the current value from state rather than from a setPinned
  // updater: the write is a SIDE EFFECT, and React may invoke an updater twice
  // (StrictMode), which would fire two PATCHes for one click.
  const pin = useCallback(
    (memberId: string) => {
      if (pinnedMemberIds.includes(memberId)) return;
      save(pinnedMemberIds, [memberId, ...pinnedMemberIds]);
    },
    [pinnedMemberIds, save],
  );

  const unpin = useCallback(
    (memberId: string) => {
      if (!pinnedMemberIds.includes(memberId)) return;
      save(
        pinnedMemberIds,
        pinnedMemberIds.filter((id) => id !== memberId),
      );
    },
    [pinnedMemberIds, save],
  );

  return { pinnedMemberIds, pin, unpin };
}
