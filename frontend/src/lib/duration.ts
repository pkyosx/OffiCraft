/**
 * Compact elapsed-time string from seconds: "3m" / "2h 15m" / "1d 4h".
 * Floors to a minimum of 1m — nothing is ever "waiting 0". Unit letters are
 * locale-neutral; the locale prefix (已等你 / 已歷時 / Waiting / …) is the
 * caller's i18n string. Shared by the 等我回覆 waited counter (RepliesPage)
 * and the task card's 已歷時 / per-step 耗時 counters (M3) so the two
 * surfaces can never drift on formatting.
 */
/** The same floor in ONE unit only — "59m", then "23h", then "3d" — for a
 * stamp drawn at one fixed width (the 請示 page's desktop pill). */
export function formatDurationShort(seconds: number): string {
  const mins = Math.max(1, Math.floor(seconds / 60));
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

/** Days, hours and minutes of the same floor, each 0 left out: the full
 * precision the short forms above drop. */
export function durationParts(seconds: number): { days: number; hours: number; minutes: number } {
  const mins = Math.max(1, Math.floor(seconds / 60));
  return { days: Math.floor(mins / 1440), hours: Math.floor(mins / 60) % 24, minutes: mins % 60 };
}

export function formatDuration(seconds: number): string {
  const mins = Math.max(1, Math.floor(seconds / 60));
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) {
    const m = mins % 60;
    return m > 0 ? `${hours}h ${m}m` : `${hours}h`;
  }
  const days = Math.floor(hours / 24);
  const h = hours % 24;
  return h > 0 ? `${days}d ${h}h` : `${days}d`;
}
