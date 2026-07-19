// lib/cost.ts — the single money formatter for token-cost surfaces (T-075a).
//
// Cost values arrive from warden telemetry as precise fractional USD, and the
// wire keeps that full precision (every SUM / roster attribution the monitoring
// fold does keys on the exact number). For DISPLAY the owner wants a coarse,
// glanceable figure — "token cost 不用顯示小數點,太細了不好讀懂": no decimals,
// thousands-separated. Every cockpit surface that shows a $ token cost renders
// through here, so the rounding rule lives in exactly one place (was three
// divergent inline renders: two `toFixed(4)` rows + one raw-float account card).

/** Render a USD cost as a whole-dollar, thousands-separated "$N" (T-075a).
 * Rounds half-up to the nearest dollar (cost is non-negative), so a sub-50¢
 * value reads "$0". The "en-US" grouping is pinned so the separator is a comma
 * regardless of the cockpit locale — the figure is USD either way. */
export function formatCost(usd: number): string {
  return `$${Math.round(usd).toLocaleString("en-US")}`;
}
