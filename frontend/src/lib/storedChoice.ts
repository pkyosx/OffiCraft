// A per-browser remembered choice out of a fixed set (the 顯示方式 selects).
// Storage that is unavailable or holds an unknown value reads as the fallback.

export function readStoredChoice<T extends string>(
  key: string,
  options: readonly T[],
  fallback: T
): T {
  try {
    const stored = window.localStorage.getItem(key);
    return (options as readonly string[]).includes(stored ?? "") ? (stored as T) : fallback;
  } catch {
    return fallback;
  }
}

export function writeStoredChoice(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    /* storage unavailable — the choice still holds for this mount */
  }
}
