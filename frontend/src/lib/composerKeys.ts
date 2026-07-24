// composerKeys — the ONE decision source for "should this Enter SEND the draft?"
// shared by every multi-line composer (ChatArea chat / ReplyComposer reply-card
// answer / TaskCard message box). Three surfaces, one rule — so a fix to the IME
// or mobile handling can never land on one and drift on the others.
//
// Desktop: Enter sends, Shift+Enter inserts a newline (the classic chat contract).
// Mobile/touch (viewport ≤ MOBILE_BREAKPOINT — see useIsMobile): a phone has no
// physical keyboard, so Shift+Enter is impossible and a bare Enter as "send" left
// the user with no way to type a second line. There, Enter is NOT a send — the
// caller lets it fall through to the textarea's native newline, and the always-
// visible send button becomes the only way to send (owner report 2026-07-24).
//
// IME guard (all environments): an Enter that CONFIRMS a CJK candidate must never
// send. `composing` folds the caller's own composition ref (covers the browser
// where compositionend precedes this keydown but nativeEvent.isComposing is
// already false); this helper also reads the native isComposing flag and the 229
// keyCode browsers stamp during composition.

import type { KeyboardEvent } from "react";

export interface EnterSendContext {
  /** True when the viewport is a phone (useIsMobile). */
  isMobile: boolean;
  /** The caller's composition ref (isComposingRef.current). */
  composing: boolean;
}

/**
 * Whether this keydown should submit the composer draft. False for anything that
 * is not a bare desktop Enter — including a mobile Enter, which the caller must
 * leave un-prevented so the textarea inserts a newline natively.
 */
export function enterShouldSend(
  e: KeyboardEvent<HTMLTextAreaElement>,
  ctx: EnterSendContext,
): boolean {
  if (e.nativeEvent.isComposing || e.keyCode === 229 || ctx.composing) {
    return false;
  }
  if (ctx.isMobile) return false;
  return e.key === "Enter" && !e.shiftKey;
}
