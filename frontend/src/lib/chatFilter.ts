// The office chat list's 篩選: which rows of a tab to show. The ticks are a
// per-viewer convenience, kept in this browser per tab and never sent to the
// server; the name search is not kept (free text left over from last time
// would quietly hide rows).
import type { ChatTab } from "./chatGroups";
import type { LifecycleVisualStatus } from "../components/LifecycleDot";
import type { MemberActivity } from "../types";

/** The options, by section. Ticks in one section widen it (OR); sections, and
 * the search, narrow each other (AND). The presence options are the presence
 * dot's own states, the activity options the activity icon's. */
export const FILTER_SECTIONS = {
  unread: ["unread"],
  presence: ["online-awake", "waking", "stopping", "stopped", "offline"],
  activity: ["busy", "waiting", "idle"],
} as const;
export type FilterSection = keyof typeof FILTER_SECTIONS;
export type FilterCheck = (typeof FILTER_SECTIONS)[FilterSection][number];

const SECTION_OF = new Map<string, FilterSection>(
  (Object.keys(FILTER_SECTIONS) as FilterSection[]).flatMap((s) => FILTER_SECTIONS[s].map((c) => [c, s] as const)),
);

/** What the filter looks at for one row, from what the row itself shows. */
export interface RowFacts {
  unread: boolean;
  /** The presence dot's state. */
  presence: LifecycleVisualStatus;
  /** The activity icon's state; null when the row shows none (not reporting,
   * or nothing reported yet). */
  activity: MemberActivity["state"] | null;
  /** Matched by the search: display name and role. */
  names: string[];
}

export function matchesCheck(facts: RowFacts, check: FilterCheck): boolean {
  if (check === "unread") return facts.unread;
  return facts.presence === check || facts.activity === check;
}

export function matchesFilter(facts: RowFacts, checks: readonly FilterCheck[], query: string): boolean {
  const q = query.trim().toLowerCase();
  const sections = (Object.keys(FILTER_SECTIONS) as FilterSection[]).map((s) =>
    checks.filter((c) => SECTION_OF.get(c) === s),
  );
  return (
    sections.every((ticked) => ticked.length === 0 || ticked.some((c) => matchesCheck(facts, c))) &&
    (q === "" || facts.names.some((n) => n.toLowerCase().includes(q)))
  );
}

const storageKey = (tab: ChatTab) => `oc_chat_filter_${tab}`;
// The first version's ticks; anything else not known is dropped.
const LEGACY: Record<string, FilterCheck> = { online: "online-awake" };

export function readFilterChecks(tab: ChatTab): FilterCheck[] {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(storageKey(tab)) ?? "[]");
    if (!Array.isArray(parsed)) return [];
    const known: string[] = parsed.map((c) => (typeof c === "string" ? (LEGACY[c] ?? c) : ""));
    return [...SECTION_OF.keys()].filter((c): c is FilterCheck => known.includes(c));
  } catch {
    return [];
  }
}

export function writeFilterChecks(tab: ChatTab, checks: readonly FilterCheck[]): void {
  try {
    window.localStorage.setItem(storageKey(tab), JSON.stringify(checks));
  } catch {
    /* storage unavailable — the choice still holds for this mount */
  }
}
