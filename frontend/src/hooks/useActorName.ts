import { useCallback } from "react";
import { useI18n } from "../i18n";
import { useMembers } from "./useMembers";
import { useOwnerDisplayName } from "./useOwnerName";

export function useActorName(): (id: string) => string {
  const { t } = useI18n();
  const { members } = useMembers();
  const ownerName = useOwnerDisplayName(t.user);
  return useCallback(
    (id: string) => {
      if (id === "owner") return ownerName;
      return members.find((member) => member.id === id)?.name || id;
    },
    [members, ownerName],
  );
}
