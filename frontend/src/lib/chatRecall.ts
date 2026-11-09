import type { ChatMessage } from "../api/adapter";
import { OWNER_ID } from "./ownerUnread";

/** Mirrors the server's `chatRecallWindowSecs`: a message can be recalled
 * while it is younger than this. */
export const CHAT_RECALL_WINDOW_SECS = 24 * 60 * 60;

/** Whether the owner may still recall `m` — the server's own gate (its sender,
 * no task, no reply card, younger than 24 hours) plus "not already recalled".
 * Evaluated at render time, so an entry can outlive the window on a screen
 * nobody repaints; the server's 409 then says so in the confirm dialog. */
export function chatRecallable(m: ChatMessage, nowSecs: number): boolean {
  return (
    m.from === OWNER_ID &&
    !m.replyCardId &&
    !m.taskId &&
    !((m.recalledTs ?? 0) > 0) &&
    nowSecs - m.ts < CHAT_RECALL_WINDOW_SECS
  );
}
