import { useCallback } from "react";
import type { Member } from "../types";
import { useI18n } from "../i18n";
import { useMembers } from "./useMembers";
import { useOwnerDisplayName } from "./useOwnerName";

/** Who an actor id is, from one roster read: the name to print, and the
 * member behind it (undefined for the owner and for an id off the roster). */
export function useActors(): { name: (id: string) => string; member: (id: string) => Member | undefined } {
  const { t } = useI18n();
  const { members } = useMembers();
  const ownerName = useOwnerDisplayName(t.user);
  const member = useCallback((id: string) => members.find((m) => m.id === id), [members]);
  const name = useCallback(
    (id: string) => {
      if (id === "owner") return ownerName;
      return member(id)?.name || id;
    },
    [member, ownerName],
  );
  return { name, member };
}

export function useActorName(): (id: string) => string {
  return useActors().name;
}
