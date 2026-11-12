import type { ChatMessage } from "../api/adapter";
import { OWNER_ID } from "./ownerUnread";

/** Mirrors the server's `chatDeleteWindowSecs`: a message can be deleted
 * while it is younger than this. */
export const CHAT_DELETE_WINDOW_SECS = 24 * 60 * 60;

/** Whether the owner may still delete `m` — the server's own gate (its sender,
 * no task, no reply card, younger than 24 hours) plus "not already deleted".
 * Evaluated at render time, so an entry can outlive the window on a screen
 * nobody repaints; the server's 409 then says so in the confirm dialog. */
export function chatDeletable(m: ChatMessage, nowSecs: number): boolean {
  return (
    m.from === OWNER_ID &&
    !m.replyCardId &&
    !m.taskId &&
    !((m.deletedTs ?? 0) > 0) &&
    nowSecs - m.ts < CHAT_DELETE_WINDOW_SECS
  );
}
