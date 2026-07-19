// Vitest setup: patch the browser globals jsdom/Node leave broken or missing
// (Web Storage, ResizeObserver) so tests run against real browser semantics.
//
// --- Web Storage (`localStorage`/`sessionStorage`) ---
//
// Why this exists: Node 22+ (we run on 25) ships its own global `localStorage`,
// but with no `--localstorage-file` configured it is a non-functional stub whose
// `getItem`/`setItem`/`clear` are `undefined`. Vitest's jsdom environment does
// NOT replace it: its populateGlobal only overrides a pre-existing global when
// the key is in its own window-key list, and `localStorage`/`sessionStorage`
// are not in that list (only `Storage` is). So jsdom's real Storage never wins,
// and every test that touches storage throws `localStorage.clear is not a
// function`. Product code (api/auth.ts) and the tests both assume a real browser
// Storage, so we provide one here rather than weaken any test.

import { afterEach } from "vitest";
import { resetChatDrafts } from "../lib/chatDraftStore";

// T-8aaa: the chat composer draft store is module-level in-memory state (it must
// outlive a component unmount/remount within the app). That same persistence
// would leak a typed draft from one test into the next when they share a peer
// id, so reset it between tests — mirrors the localStorage.clear() that
// per-suite beforeEach hooks already do.
afterEach(() => resetChatDrafts());

class MemoryStorage implements Storage {
  #data = new Map<string, string>();

  get length(): number {
    return this.#data.size;
  }

  key(index: number): string | null {
    return Array.from(this.#data.keys())[index] ?? null;
  }

  getItem(key: string): string | null {
    return this.#data.has(key) ? this.#data.get(key)! : null;
  }

  setItem(key: string, value: string): void {
    this.#data.set(String(key), String(value));
  }

  removeItem(key: string): void {
    this.#data.delete(String(key));
  }

  clear(): void {
    this.#data.clear();
  }
}

for (const name of ["localStorage", "sessionStorage"] as const) {
  Object.defineProperty(globalThis, name, {
    value: new MemoryStorage(),
    configurable: true,
    writable: true,
  });
}

// jsdom implements neither ResizeObserver nor the layout that would make it
// fire, so ChatArea's jump-to-origin re-center observer (ChatArea.tsx) throws
// `ResizeObserver is not defined`. A no-op stub is the faithful test double:
// the tests pin the synchronous center-scroll + highlight and the one-shot
// "never re-scrolls" contract, none of which depend on the observer callback
// (a firing callback would inject scroll calls the one-shot test forbids).
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  };
}
