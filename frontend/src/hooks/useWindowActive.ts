// hooks/useWindowActive.ts — is the owner ACTUALLY LOOKING at the cockpit?
//
// "Active" = the tab is visible AND the window has OS focus. This is the gate
// for read side effects (the chat "list 即讀" auto-mark + explicit mark-read):
// only a user who can actually see the thread may consume its unread state. A
// backgrounded window (another app in front → hasFocus()=false) or a hidden tab
// (visibilityState="hidden") must NOT silently wipe a read watermark — the
// roster badge has to survive until the owner really comes back and looks
// (Bug: the unread badge flashed and died because the SSE-triggered refetch of
// the open thread fired GET /api/chat?with= — a side-effectful list — while the
// window sat in the background).

import { useEffect, useState } from "react";

/** Synchronous truth — usable outside React (e.g. inside an SSE callback). */
export function isWindowActive(): boolean {
  return document.visibilityState === "visible" && document.hasFocus();
}

/** Reactive version: re-renders on focus/blur/visibilitychange. */
export function useWindowActive(): boolean {
  const [active, setActive] = useState(isWindowActive);
  useEffect(() => {
    const update = () => setActive(isWindowActive());
    window.addEventListener("focus", update);
    window.addEventListener("blur", update);
    document.addEventListener("visibilitychange", update);
    return () => {
      window.removeEventListener("focus", update);
      window.removeEventListener("blur", update);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);
  return active;
}
