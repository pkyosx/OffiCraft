import { type DueState, dueState, nextDueWake } from "../lib/dueDate";
import { useNowSeconds } from "./useNowSeconds";

/** A due date's state, re-rendered at the exact moment it next changes colour
 * or remaining time (and at local midnight) rather than on a poll. `live`
 * false (a closed task) stops the wake-ups; the state is still computed. */
export function useDueState(
  dueTs: number | null,
  leadMinutes: number,
  live = true
): { nowSec: number; due: DueState | null } {
  const nowSec = useNowSeconds(
    dueTs === null || !live ? null : nextDueWake(dueTs, Date.now() / 1000, leadMinutes)
  );
  return { nowSec, due: dueTs === null ? null : dueState(dueTs, nowSec, leadMinutes) };
}
