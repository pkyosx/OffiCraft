// useQuotedMessageOverlay — the ONE way the cockpit opens a message somebody
// pointed at WITHOUT moving them (T-0b78).
//
// 🔴 IT HAS EXACTLY ONE CALLER: the chat bubble's quote row 「看原訊息」, and
// since T-48 (R14-1.6) that is a machine-checked fact rather than a sentence
// here — `lint:async-landing`'s QUOTED_OVERLAY_CALLERS sweeps the whole tree.
// It matters because this hook keeps NO room stamp of its own: `ChatArea` is
// mounted under `key={peerId}`, so a room switch unmounts it and takes the
// overlay with it. A second caller keyed on a card id would not unmount, and
// would paint room A's message full-screen over room B — R8-3's shape.
// T-0b78 briefly routed the 請示 page's 跳到原訊息 and the inline task card's
// 在聊天室回覆 through here too; owner 2026-08-29 sent those two BACK to
// navigating (「1 跟 2 變回去原本那樣」) and knowingly took back the miss
// described below, parking the fix. So the history in this header is history —
// do not read it as "three call sites still share this".
//
// 🔴 WHY THIS IS A HOOK AND NOT A HANDLER. Until T-0b78 the cockpit had
// two answers to one intent, on screens that sit next to each other:
//
//   * the chat bubble's 看原訊息 read THAT message by id and showed it whole,
//     staying exactly where the reader was (owner ruling 2026-08-21,
//     「全部統一就撈那一則顯示出來就好」);
//   * the 請示 card and the inline task card wrote a route — #office/chat/<id>/
//     msg/<msgId> — which walked the reader off the page they were on and left
//     ChatArea to hunt for `[data-msg-id]` in DOM it had ALREADY PAINTED. When
//     the target was not in that DOM the search simply missed and the reader
//     landed on the newest message, with nothing on screen saying so. The
//     variable there is DENSITY, not age: a message sent one minute ago is
//     already outside the window on a busy line.
//
// The read, the one-click latch and the failure sentence therefore live HERE,
// once. A SECOND copy of any of them — including one grown by either card that
// went back to navigating — is what `useQuotedMessageOverlay.test.tsx`'s last
// case is written to redden (its whole-tree sweep still covers them) —
// behaviour tests cannot see a duplicate, because a duplicate draws the same
// pixels right up until it drifts.
//
// 🔴 THERE IS NO LOADING STATE, AND THAT IS A DECISION. One was written first
// and it drove `disabled` on the button while the read was in flight; measured
// in a real Chromium, disabling the focused button BLURS it, so the overlay
// captured <body> as the element to restore focus to and closing it dropped a
// keyboard user at the top of the page. The read is one point query.
//
// 🔴 ONE CLICK, ONE REQUEST, AND NOTHING THAT OUTLIVES IT. `open` is a click
// handler, not an effect: React never re-runs it and no dependency array can
// fire it again on a repaint. On failure it remembers exactly one id and stops
// — no retry, no queue, no repair on the next SSE event. That promise is about
// a machine that USED to exist (useQuotedMessages, deleted 2026-08-21) whose
// states drew identical pixels whether they were right or wrong;
// `ChatArea.quote-no-fetch.test.tsx` holds that line.

import { useRef, useState, type ReactNode } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import { MarkdownPreviewOverlay } from "../components/MarkdownPreviewOverlay";
import "../components/office.css"; // .chat__msg-quote__error

export type QuotedMessageOverlay = {
  /** Read that ONE message by id and show it whole. Does not navigate, does
   * not scroll, does not touch the route. */
  open: (id: string) => Promise<void>;
  /** The full-view overlay, or null. Render it from the caller's tree. */
  overlay: ReactNode;
  /** The id whose LAST open attempt failed, or null. */
  failedId: string | null;
  /** The visible failure sentence for `id`, or null — so a failed read is said
   * out loud on every surface in the same words, from one place. */
  failureNotice: (id: string) => ReactNode;
};

/**
 * ⚠️ THIS HOOK'S STATE MUST DIE WITH THE CONVERSATION (T-48, R8-3). `shown` set
 * by a read that started in A would otherwise land as a full-screen overlay of
 * A's message on top of B's room — during the read the overlay is not open yet,
 * so the roster is fully clickable and the switch is not blocked by anything.
 * It used to be given an explicit VISIT TOKEN for that, because `ChatArea` was
 * reused between rooms; `ChatArea` is mounted under `key={peerId}` now (R13-5),
 * so this hook is unmounted with the room and both `setShown` and `setFailedId`
 * land in a component React has discarded.
 *
 * @param resolveName how to title the overlay for a message's sender. ChatArea
 * passes its roster-aware `nameOf` (the owner's own nickname, outsource
 * codenames, 系統). Surfaces without a roster omit it and get the server's
 * resolved `fromName`, falling back to the raw address — never a fabricated
 * name.
 */
export function useQuotedMessageOverlay(
  resolveName?: (id: string) => string,
): QuotedMessageOverlay {
  const { t } = useI18n();
  const [shown, setShown] = useState<{ title: string; source: string } | null>(
    null,
  );
  const [failedId, setFailedId] = useState<string | null>(null);
  // The in-flight latch. A `useState` flag cannot do this job: two clicks
  // landing in the same tick both read the PRE-UPDATE state and both would
  // fire. A ref is written synchronously inside the handler.
  const busyRef = useRef(false);

  async function open(id: string): Promise<void> {
    if (busyRef.current) return;
    busyRef.current = true;
    setFailedId(null);
    try {
      const original = await api.getChatMessage(id);
      setShown({
        title: resolveName
          ? resolveName(original.from)
          : original.fromName || original.from,
        source: original.body,
      });
    } catch {
      // Deliberately swallowed rather than logged-and-retried: the person who
      // clicked is told on screen, and there is nothing else to do about it.
      setFailedId(id);
    } finally {
      busyRef.current = false;
    }
  }

  // FOCUS IS NOT HANDLED HERE. MarkdownPreviewOverlay focuses itself on mount
  // and hands focus back on unmount — to the button that was clicked. Doing it
  // again from this side would be two owners for one behaviour.
  const overlay = shown ? (
    <MarkdownPreviewOverlay
      title={shown.title}
      source={shown.source}
      onClose={() => setShown(null)}
    />
  ) : null;

  // 🔴 THE FAILURE IS SAID BESIDE THE BUTTON, AND IT IS NOT A CLAIM ABOUT THE
  // ORIGINAL. A chat quote line either carries the server's excerpt or carries
  // 「這則訊息已不存在」, which is a statement about whether the original
  // EXISTS; a read that failed says nothing about that, so this sentence never
  // overwrites it. `role="status"` so a screen reader hears the outcome of the
  // button it just pressed.
  const failureNotice = (id: string): ReactNode =>
    failedId === id ? (
      <span
        className="chat__msg-quote__error"
        data-testid="msg-quote-error"
        role="status"
      >
        {t.chat.replyQuoteOpenFailed}
      </span>
    ) : null;

  return { open, overlay, failedId, failureNotice };
}
