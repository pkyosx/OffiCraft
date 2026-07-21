// hooks/useOwnerName.ts — the owner's display nickname in the topbar (T-0b41).
//
// The nickname lives SERVER-SIDE now (DB owner.name, behind /api/settings) so
// every device shows the same name. This hook is the profile pill's seam:
// mount-fetch the stored value, PATCH on commit, and resolve to the localized
// default when unset. Mirrors useOrgName (T-d693); unlike org.name the nickname
// is NOT an agent read path (it never enters get_global_context).
//
// Owner-only surface: the whole cockpit is owner-authed, and /api/settings is
// owner-gated — a member/agent never reaches this write path. It replaces the
// old localStorage-only override (client cache dropped: the server is now the
// single source of truth, so a stale per-browser copy could only mislead).

import { useCallback, useEffect, useState } from "react";
import { api } from "../api";

interface UseOwnerName {
  /** The name to render in the profile pill: the stored nickname if the owner
   * has set one, else the caller-supplied localized default (`t.user`). */
  ownerName: string;
  /** Commit an edited nickname: PATCH /api/settings {owner_name}, adopting the
   * server's echoed value. Optimistic — reverts to the last confirmed value if
   * the write rejects. */
  setOwnerName: (next: string) => void;
}

export function useOwnerName(fallback: string): UseOwnerName {
  // null = not yet loaded / never set; "" is never stored (a set name is
  // non-empty). Either way an empty resolved value falls back to the default.
  const [stored, setStored] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    api
      .getServerSettings()
      .then((s) => {
        if (alive) setStored(s.ownerName);
      })
      .catch((e) => {
        // A failed load must never masquerade as "no name" — keep the localized
        // default showing rather than fabricating a value.
        console.warn("useOwnerName: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, []);

  const setOwnerName = useCallback(
    (next: string) => {
      const trimmed = next.trim();
      const prev = stored;
      setStored(trimmed); // optimistic
      api
        .patchServerSettings({ ownerName: trimmed })
        .then((s) => setStored(s.ownerName))
        .catch((e) => {
          console.warn("useOwnerName: save failed", e);
          setStored(prev); // snap back to the last server-confirmed value
        });
    },
    [stored]
  );

  const ownerName = stored && stored.length > 0 ? stored : fallback;
  return { ownerName, setOwnerName };
}
