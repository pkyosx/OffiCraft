// The 稍後再看 menu's four choices, in the owner's own timezone (the browser's).

export const SNOOZE_CHOICES = ["1h", "3h", "tomorrow", "nextMonday"] as const;
export type SnoozeChoice = (typeof SNOOZE_CHOICES)[number];

/** Epoch seconds the choice ends at, counted from `now`. 下週一 is the first
 * Monday after today, so on a Monday it is a week away and on a Sunday it is
 * tomorrow. */
export function snoozeUntil(choice: SnoozeChoice, now: Date): number {
  const at9 = (days: number) =>
    new Date(now.getFullYear(), now.getMonth(), now.getDate() + days, 9, 0, 0, 0);
  switch (choice) {
    case "1h":
      return Math.floor(now.getTime() / 1000) + 3600;
    case "3h":
      return Math.floor(now.getTime() / 1000) + 3 * 3600;
    case "tomorrow":
      return at9(1).getTime() / 1000;
    case "nextMonday":
      return at9((8 - now.getDay()) % 7 || 7).getTime() / 1000;
  }
}
