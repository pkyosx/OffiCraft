// The 稍後再看 menu's arithmetic. Every choice is a span from now; the server
// takes the absolute time and refuses anything more than 90 days ahead.

export type SnoozeUnit = "hour" | "day";

export const SNOOZE_UNIT_SECONDS: Record<SnoozeUnit, number> = {
  hour: 3600,
  day: 86_400,
};

export const REPLY_SNOOZE_MAX_SECONDS = 90 * 86_400;

export const SNOOZE_PRESETS: readonly { amount: number; unit: SnoozeUnit }[] = [
  { amount: 1, unit: "hour" },
  { amount: 3, unit: "hour" },
  { amount: 1, unit: "day" },
  { amount: 3, unit: "day" },
];

export function snoozeUntil(amount: number, unit: SnoozeUnit, nowTs: number): number {
  return Math.floor(nowTs) + amount * SNOOZE_UNIT_SECONDS[unit];
}

export type SnoozeDraft =
  | { ok: true; untilTs: number }
  | { ok: false; reason: "empty" | "invalid" | "tooFar" };

/** The custom row's draft: a whole number of hours or days, 1 or more, at most
 * 90 days out. */
export function snoozeDraft(amountDraft: string, unit: SnoozeUnit, nowTs: number): SnoozeDraft {
  const text = amountDraft.trim();
  if (text === "") return { ok: false, reason: "empty" };
  if (!/^\d+$/.test(text)) return { ok: false, reason: "invalid" };
  const amount = Number(text);
  if (amount < 1) return { ok: false, reason: "invalid" };
  if (amount * SNOOZE_UNIT_SECONDS[unit] > REPLY_SNOOZE_MAX_SECONDS) {
    return { ok: false, reason: "tooFar" };
  }
  return { ok: true, untilTs: snoozeUntil(amount, unit, nowTs) };
}
