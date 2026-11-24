import { useEffect, useRef } from "react";
import type { MouseEvent, PointerEvent } from "react";

export const LONG_PRESS_MS = 500;
const MOVE_TOLERANCE_PX = 10;

const byFinger = (e: { nativeEvent: Event }) =>
  (e.nativeEvent as globalThis.PointerEvent).pointerType === "touch";

/** Long-press by a finger — on any device, a touch screen with a mouse
 * included — delegated from one container. `accept` maps the pressed element
 * to the thing the press is about, or null to leave the press to the browser.
 * A finger that travels is a scroll, not a press. The click a completed press
 * would otherwise deliver to what lay under the finger (a link, the quote row,
 * a card's own toggle) is swallowed. While a finger is down the container
 * carries `data-touch-press`, for CSS that turns the browser's own long press
 * off. */
export function useLongPress<T>(
  accept: (target: Element) => T | null,
  onLongPress: (hit: T) => void,
) {
  const timer = useRef<number | null>(null);
  const start = useRef<{ x: number; y: number } | null>(null);
  const fired = useRef(false);
  const latest = useRef({ accept, onLongPress });
  latest.current = { accept, onLongPress };

  const cancel = () => {
    if (timer.current != null) window.clearTimeout(timer.current);
    timer.current = null;
    start.current = null;
  };
  const lift = (e: PointerEvent) => {
    cancel();
    delete (e.currentTarget as HTMLElement).dataset.touchPress;
  };
  useEffect(() => cancel, []);

  return {
    onPointerDown(e: PointerEvent) {
      fired.current = false;
      cancel();
      if (!byFinger(e)) return;
      const hit = latest.current.accept(e.target as Element);
      if (hit == null) return;
      (e.currentTarget as HTMLElement).dataset.touchPress = "";
      start.current = { x: e.clientX, y: e.clientY };
      timer.current = window.setTimeout(() => {
        timer.current = null;
        start.current = null;
        fired.current = true;
        latest.current.onLongPress(hit);
      }, LONG_PRESS_MS);
    },
    onPointerMove(e: PointerEvent) {
      const s = start.current;
      if (!s) return;
      if (Math.hypot(e.clientX - s.x, e.clientY - s.y) > MOVE_TOLERANCE_PX) cancel();
    },
    onPointerUp: lift,
    onPointerCancel: lift,
    // Android raises its own long-press menu through contextmenu.
    onContextMenu(e: MouseEvent) {
      if (fired.current || timer.current != null) e.preventDefault();
    },
    // Chromium follows a long press with the compatibility mousedown and click;
    // the mousedown would take focus off whatever the press opened.
    onMouseDownCapture(e: MouseEvent) {
      if (fired.current) e.preventDefault();
    },
    onClickCapture(e: MouseEvent) {
      if (!fired.current) return;
      fired.current = false;
      e.preventDefault();
      e.stopPropagation();
    },
  };
}
