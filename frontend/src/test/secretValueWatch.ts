import { vi } from "vitest";

const CONSOLE_METHODS = ["log", "info", "warn", "error", "debug"] as const;

function storageHolds(storage: Storage, value: string): boolean {
  for (let i = 0; i < storage.length; i++) {
    const key = storage.key(i)!;
    if (key.includes(value) || storage.getItem(key)?.includes(value)) return true;
  }
  return false;
}

function asText(thing: unknown): string {
  try {
    return `${String(thing)} ${JSON.stringify(thing)}`;
  } catch {
    return String(thing);
  }
}

/** Starts watching where `value` lands outside the component that should be its
 * only home; the returned function names every place it has reached so far.
 * Reads each store through its own API: `{...localStorage}` is always empty under
 * the test Storage in setup.ts, so spreading it proves nothing. */
export function watchSecretValue(value: string): () => string[] {
  const spies = CONSOLE_METHODS.map((m) => vi.spyOn(console, m).mockImplementation(() => {}));
  const globalsBefore = new Set(Object.getOwnPropertyNames(globalThis));

  return () => {
    const places: string[] = [];
    if (storageHolds(localStorage, value)) places.push("localStorage");
    if (storageHolds(sessionStorage, value)) places.push("sessionStorage");
    CONSOLE_METHODS.forEach((method, i) => {
      if (spies[i].mock.calls.flat().some((arg) => asText(arg).includes(value))) {
        places.push(`console.${method}`);
      }
    });
    for (const name of Object.getOwnPropertyNames(globalThis)) {
      if (globalsBefore.has(name)) continue;
      if (asText((globalThis as Record<string, unknown>)[name]).includes(value)) {
        places.push(`globalThis.${name}`);
      }
    }
    return places;
  };
}
