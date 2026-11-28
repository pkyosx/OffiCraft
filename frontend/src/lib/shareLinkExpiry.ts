// The share menu's expiry arithmetic. The server takes an absolute time and
// refuses anything more than 365 days ahead; 永久 is null.

export type ShareExpiryUnit = "hour" | "day" | "forever";

const UNIT_SECONDS = { hour: 3600, day: 86_400 } as const;

export const SHARE_LINK_MAX_SECONDS = 365 * 86_400;

// The server checks the 365-day cap against its own clock; a device running a
// little ahead would otherwise have 「365 天」 refused.
const CLOCK_AHEAD_ALLOWANCE_SECONDS = 60;

export const SHARE_EXPIRY_DEFAULT = { amount: "7", unit: "day" as ShareExpiryUnit };

export type ShareExpiryDraft =
  | { ok: true; expiresTs: number | null }
  | { ok: false; reason: "empty" | "invalid" | "tooFar" };

export function shareExpiryDraft(
  amountDraft: string,
  unit: ShareExpiryUnit,
  nowTs: number,
): ShareExpiryDraft {
  if (unit === "forever") return { ok: true, expiresTs: null };
  const text = amountDraft.trim();
  if (text === "") return { ok: false, reason: "empty" };
  if (!/^\d+$/.test(text)) return { ok: false, reason: "invalid" };
  const amount = Number(text);
  if (amount < 1) return { ok: false, reason: "invalid" };
  const span = amount * UNIT_SECONDS[unit];
  if (span > SHARE_LINK_MAX_SECONDS) return { ok: false, reason: "tooFar" };
  const sent = Math.min(span, SHARE_LINK_MAX_SECONDS - CLOCK_AHEAD_ALLOWANCE_SECONDS);
  return { ok: true, expiresTs: Math.floor(nowTs) + sent };
}
