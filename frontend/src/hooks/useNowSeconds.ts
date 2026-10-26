import { useEffect, useState } from "react";

// setTimeout fires immediately for delays above 2^31-1 ms (~24.8 days).
export const MAX_TIMEOUT_MS = 2_147_483_647;

function msUntilNextLocalMidnight(nowMs: number): number {
  const next = new Date(nowMs);
  // Local calendar math, not +24h: a DST day is 23 or 25 hours long.
  next.setHours(24, 0, 0, 0);
  return next.getTime() - nowMs;
}

/** The current time in epoch seconds, read at each render. The caller also
 * re-renders at every local midnight, so a 今天/明天 label built from it rolls
 * over even when nothing else on screen changes — and at `wakeAt` (epoch
 * seconds) when one is given, for a label that flips at a moment of its own. */
export function useNowSeconds(wakeAt: number | null = null): number {
  const [, setDay] = useState(0);
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>;
    const arm = () => {
      timer = setTimeout(() => {
        setDay((d) => d + 1);
        arm();
      }, msUntilNextLocalMidnight(Date.now()));
    };
    arm();
    return () => clearTimeout(timer);
  }, []);

  const [woke, setWoke] = useState(0);
  useEffect(() => {
    if (wakeAt === null) return;
    // wakeAt was worked out at render; a render just before the moment can
    // reach this effect after it, and that render's label is already stale.
    const remaining = Math.max(0, wakeAt * 1000 - Date.now());
    const timer = setTimeout(
      () => setWoke((n) => n + 1),
      Math.min(remaining, MAX_TIMEOUT_MS)
    );
    return () => clearTimeout(timer);
  }, [wakeAt, woke]);

  return Date.now() / 1000;
}
