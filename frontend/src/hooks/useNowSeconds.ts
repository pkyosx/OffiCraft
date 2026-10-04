import { useEffect, useState } from "react";

function msUntilNextLocalMidnight(nowMs: number): number {
  const next = new Date(nowMs);
  // Local calendar math, not +24h: a DST day is 23 or 25 hours long.
  next.setHours(24, 0, 0, 0);
  return next.getTime() - nowMs;
}

/** The current time in epoch seconds, read at each render. The caller also
 * re-renders at every local midnight, so a 今天/明天 label built from it rolls
 * over even when nothing else on screen changes. */
export function useNowSeconds(): number {
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
  return Date.now() / 1000;
}
