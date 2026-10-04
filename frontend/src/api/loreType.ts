import type { LoreType } from "./adapter";

/** Every type tag the server accepts, in the order the cockpit lists them. */
export const LORE_TYPES: readonly LoreType[] = [
  "instruction_conflict",
  "instruction_supplement",
  "owner_decision",
  "owner_preference",
  "other",
];

export function isLoreType(v: string | undefined): v is LoreType {
  return (LORE_TYPES as readonly (string | undefined)[]).includes(v);
}
