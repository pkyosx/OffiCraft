// The two `secret` reply-card durations. The server decides what a PATCH may
// write (settings.go); these copies only let the settings field refuse an
// out-of-range value before saving, and show it in a readable unit.

export const SECRET_CARD_UNRETRIEVED_TTL_SECS_DEFAULT = 86400;
export const SECRET_CARD_UNRETRIEVED_TTL_SECS_MIN = 60;
export const SECRET_CARD_UNRETRIEVED_TTL_SECS_MAX = 2592000;
export const SECRET_CARD_RETRIEVED_RETENTION_SECS_DEFAULT = 600;
export const SECRET_CARD_RETRIEVED_RETENTION_SECS_MIN = 0;
export const SECRET_CARD_RETRIEVED_RETENTION_SECS_MAX = 86400;

export type DurationUnit = "day" | "hour" | "minute" | "second";

export const DURATION_UNITS: readonly DurationUnit[] = ["day", "hour", "minute", "second"];

export const DURATION_UNIT_SECS: Record<DurationUnit, number> = {
  day: 86400,
  hour: 3600,
  minute: 60,
  second: 1,
};

/** Seconds in the largest unit that divides them exactly; zero reads as minutes. */
export function splitDurationSecs(secs: number): { amount: number; unit: DurationUnit } {
  if (secs === 0) return { amount: 0, unit: "minute" };
  const unit = DURATION_UNITS.find((u) => secs % DURATION_UNIT_SECS[u] === 0) ?? "second";
  return { amount: secs / DURATION_UNIT_SECS[unit], unit };
}

/** A whole-number draft in `unit` as seconds, or null when it cannot be saved. */
export function durationSecsFromDraft(
  amountDraft: string,
  unit: DurationUnit,
  min: number,
  max: number,
): number | null {
  const trimmed = amountDraft.trim();
  if (trimmed === "") return null;
  const amount = Number(trimmed);
  if (!Number.isInteger(amount)) return null;
  const secs = amount * DURATION_UNIT_SECS[unit];
  return secs >= min && secs <= max ? secs : null;
}
