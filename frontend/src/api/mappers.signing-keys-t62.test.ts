// toSigningKeys — the narrowing the settings card CANNOT do for itself (T-62).
//
// 🔴 THIS FILE EXISTS BECAUSE A MUTANT SURVIVED. The card's own tests mock
// useSigningKeys, so they hand the component a `createdTs: null` and never
// touch the mapper. Deleting the `created_ts === 0 -> null` narrowing left every
// one of them green while the real cockpit would have rendered a key's creation
// date as January 1970 — a WRONG fact, not a missing one, which is the whole
// distinction the wire's 0 exists to carry.
//
// So the narrowing is pinned here, at the layer that performs it.

import { describe, it, expect } from "vitest";
import { toSigningKeys } from "./mappers";

describe("toSigningKeys", () => {
  it("turns a created_ts of 0 into null — 'never recorded', not 1970", () => {
    const [key] = toSigningKeys({
      keys: [{ key_id: "k-legacy", created_ts: 0, is_signing: true }],
    });
    expect(key.createdTs).toBeNull();
  });

  it("passes a real timestamp through untouched", () => {
    // The positive control for the test above: if the mapper nulled everything
    // the first assertion would still pass and mean nothing.
    const [key] = toSigningKeys({
      keys: [{ key_id: "k-new", created_ts: 1788400000, is_signing: false }],
    });
    expect(key.createdTs).toBe(1788400000);
  });

  it("keeps the server's order and its is_signing marks", () => {
    const keys = toSigningKeys({
      keys: [
        { key_id: "k-old", created_ts: 0, is_signing: false },
        { key_id: "k-new", created_ts: 1788400000, is_signing: true },
      ],
    });
    expect(keys.map((k) => k.keyId)).toEqual(["k-old", "k-new"]);
    expect(keys.map((k) => k.isSigning)).toEqual([false, true]);
  });

  it("carries no field beyond the three the wire declares", () => {
    // Structural, not a list of forbidden names: if a future field arrives
    // carrying a fingerprint or key material, this reddens without anyone
    // having predicted what it would be called.
    const [key] = toSigningKeys({
      keys: [{ key_id: "k-a", created_ts: 1, is_signing: true }],
    });
    expect(Object.keys(key).sort()).toEqual(["createdTs", "isSigning", "keyId"]);
  });
});
