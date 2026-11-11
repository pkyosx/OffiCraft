import { useCallback, useSyncExternalStore } from "react";
import type { SetStateAction } from "react";
import {
  getChatDraft,
  saveChatDraftText,
  subscribeChatDraft,
} from "../lib/chatDraftStore";

/** A composer's typed text, read from and written to its draft in
 * `chatDraftStore` rather than held in component state — so a box mounted
 * while another instance's send is in flight sees that send clear the draft,
 * and two boxes on the same key never disagree. */
export function useDraftText(
  draftKey: string,
): [string, (next: SetStateAction<string>) => void] {
  const subscribe = useCallback(
    (cb: () => void) => subscribeChatDraft(draftKey, cb),
    [draftKey],
  );
  const text = useSyncExternalStore(
    subscribe,
    useCallback(() => getChatDraft(draftKey)?.text ?? "", [draftKey]),
  );
  const setText = useCallback(
    (next: SetStateAction<string>) => {
      const cur = getChatDraft(draftKey)?.text ?? "";
      saveChatDraftText(draftKey, typeof next === "function" ? next(cur) : next);
    },
    [draftKey],
  );
  return [text, setText];
}
