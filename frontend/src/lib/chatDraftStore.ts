// lib/chatDraftStore.ts — the composer DRAFT survival layer for the office chat.
//
// The bug (T-8aaa): the座艙 chat composer's draft (typed text AND staged image
// attachments) lived ONLY in ChatArea's component state. Navigating to another
// page unmounts OfficePage (and with it the single ChatArea instance), so the
// draft was dropped; coming back showed an empty composer.
//
// The layer: a per-peer, module-level in-memory store. Module state outlives a
// component unmount/remount, so a 跳頁-then-return restores the draft. It is
// deliberately NOT persisted to localStorage:
//   • image attachments are `data:…;base64,…` URIs — potentially MBs each;
//     serializing every keystroke into the ~5 MB localStorage quota is a real
//     cost and a real eviction risk, for a payload that is already sitting in JS
//     memory anyway.
//   • the owner's reported scenario is "跳到別頁再回來" (an in-app SPA
//     navigation), which never tears down the module — only a full page reload
//     does, and losing an in-progress draft across a hard reload is acceptable
//     (documented tradeoff).
//
// Keyed by CHAT PEER id (member id / outsource worker id), matching how chat
// history and the compose seed are keyed — so each conversation carries its own
// independent draft.

import type { PendingAttachment } from "../hooks/useAttachmentStaging";

/** A saved composer draft for one peer: the text plus the staged attachments
 * (held as the same fully-serializable `PendingAttachment` the composer uses). */
export interface ChatDraft {
  text: string;
  attachments: PendingAttachment[];
}

const drafts = new Map<string, ChatDraft>();

/** The saved draft for a peer, or undefined when none (never typed, or cleared
 * by a send / manual empty). */
export function getChatDraft(peerId: string): ChatDraft | undefined {
  return drafts.get(peerId);
}

/** Persist a peer's draft. An EMPTY draft (no text and no attachments) deletes
 * the entry instead of storing a blank — this is the "送出 / 手動清空後歸零"
 * path, so a later return finds nothing to restore (and the compose seed is free
 * to inject into the genuinely-empty composer). */
export function saveChatDraft(peerId: string, draft: ChatDraft): void {
  if (draft.text.length === 0 && draft.attachments.length === 0) {
    drafts.delete(peerId);
    return;
  }
  drafts.set(peerId, draft);
}

/** Test-only reset so a module-level store never leaks state across tests. */
export function resetChatDrafts(): void {
  drafts.clear();
}
