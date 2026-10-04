const UNITS = ["KB", "MB", "GB", "TB"] as const;

/** Bytes as "41.7 GB": 1024-based, one decimal, never below KB. A value that
 * would round to 1024 of one unit is shown as 1.0 of the next. */
export function formatBytes(bytes: number): string {
  let value = Math.max(0, bytes) / 1024;
  let unit = 0;
  while (unit < UNITS.length - 1 && Math.round(value * 10) / 10 >= 1024) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(1)} ${UNITS[unit]}`;
}
