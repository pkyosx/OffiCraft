// Shared calendar-date formatting for the chat stream's LINE/Slack-style day
// dividers (ChatArea) and the 等我回覆 cards' absolute timestamps
// (RepliesPage). Pure functions over epoch-second timestamps — every "now" is
// an explicit parameter so tests pin behavior with fixed clocks, never
// Date.now() flakiness. All day math is LOCAL time (the owner's wall
// calendar): a "day" boundary is local midnight, exactly like LINE.

/** Local midnight (epoch seconds) of the day containing `tsSeconds`. */
export function dayStartOf(tsSeconds: number): number {
  const d = new Date(tsSeconds * 1000);
  d.setHours(0, 0, 0, 0);
  return Math.floor(d.getTime() / 1000);
}

/** One calendar day's slice of a chronological stream. */
export interface DayGroup<T> {
  /** Local midnight (epoch seconds) of this group's day — the divider key. */
  dayTs: number;
  items: T[];
}

/**
 * Partition a chronological (oldest→newest) stream into contiguous local
 * calendar-day groups. Order and membership are preserved exactly — this only
 * splits at local-midnight crossings, never reorders. (Out-of-order input is
 * still honestly split on every day CHANGE, matching the stream as rendered.)
 */
export function splitByDay<T extends { ts: number }>(items: T[]): DayGroup<T>[] {
  const groups: DayGroup<T>[] = [];
  for (const item of items) {
    const dayTs = dayStartOf(item.ts);
    const last = groups[groups.length - 1];
    if (last && last.dayTs === dayTs) last.items.push(item);
    else groups.push({ dayTs, items: [item] });
  }
  return groups;
}

/** The locale-owned pieces of a day-divider label (lives in the i18n dicts —
 * each language renders its own calendar convention; this module only does
 * the calendar MATH). `weekday` is 0=Sunday … 6=Saturday. */
export interface DayLabelDict {
  dateToday: string;
  dateYesterday: string;
  dateOn: (month: number, day: number, weekday: number) => string;
  dateOnYear: (
    year: number,
    month: number,
    day: number,
    weekday: number,
  ) => string;
}

/**
 * Divider label for the day containing `tsSeconds`, judged against the
 * caller's `nowSeconds` clock: 今天 / 昨天 / a locale-formatted date (the year
 * appears only when it differs from the current year — LINE convention).
 */
export function formatDayLabel(
  tsSeconds: number,
  nowSeconds: number,
  d: DayLabelDict,
): string {
  const dayTs = dayStartOf(tsSeconds);
  const todayTs = dayStartOf(nowSeconds);
  if (dayTs === todayTs) return d.dateToday;
  // "Yesterday" = the day whose local midnight precedes today's. Computed via
  // Date arithmetic (not todayTs - 86400) so a DST-shifted 23h/25h day still
  // resolves correctly.
  const y = new Date(todayTs * 1000);
  y.setDate(y.getDate() - 1);
  if (dayTs === Math.floor(y.getTime() / 1000)) return d.dateYesterday;
  const date = new Date(dayTs * 1000);
  const now = new Date(nowSeconds * 1000);
  return date.getFullYear() === now.getFullYear()
    ? d.dateOn(date.getMonth() + 1, date.getDate(), date.getDay())
    : d.dateOnYear(
        date.getFullYear(),
        date.getMonth() + 1,
        date.getDate(),
        date.getDay(),
      );
}

/**
 * Absolute date+time stamp for the 等我回覆 cards: always numeric, always
 * carries the date — "7/13 09:05", with the year prefixed only when it
 * differs from the current year ("2025/12/31 09:05"). Numeric M/D + 24h HH:mm
 * is deliberately locale-neutral (no i18n indirection): unambiguous in
 * zh/en/xian alike, and "today" is NOT special-cased — one consistent shape,
 * no 今天-vs-date drift between cards on the same page.
 */
export function formatAbsolute(tsSeconds: number, nowSeconds: number): string {
  const d = new Date(tsSeconds * 1000);
  const now = new Date(nowSeconds * 1000);
  const hm = `${String(d.getHours()).padStart(2, "0")}:${String(
    d.getMinutes(),
  ).padStart(2, "0")}`;
  const md = `${d.getMonth() + 1}/${d.getDate()}`;
  return d.getFullYear() === now.getFullYear()
    ? `${md} ${hm}`
    : `${d.getFullYear()}/${md} ${hm}`;
}
