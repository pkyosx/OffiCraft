// The office chat list's 篩選: which rows of a tab to show. The checks are a
// per-viewer convenience, kept in this browser per tab and never sent to the
// server; the name search is not kept (free text left over from last time
// would quietly hide rows).
import type { ChatTab } from "./chatGroups";

export const FILTER_CHECKS = ["unread", "online", "busy"] as const;
export type FilterCheck = (typeof FILTER_CHECKS)[number];

/** What the filter looks at for one row, from the state the row itself shows. */
export interface RowFacts {
  unread: boolean;
  /** The row's presence dot shows online. */
  online: boolean;
  /** The row's activity icon shows 工作中. */
  busy: boolean;
  /** Matched by the search: display name and role. */
  names: string[];
}

/** Every check holds and, with a query, a name contains it (any case). */
export function matchesFilter(facts: RowFacts, checks: readonly FilterCheck[], query: string): boolean {
  const q = query.trim().toLowerCase();
  return (
    checks.every((c) => facts[c]) &&
    (q === "" || facts.names.some((n) => n.toLowerCase().includes(q)))
  );
}

const storageKey = (tab: ChatTab) => `oc_chat_filter_${tab}`;

export function readFilterChecks(tab: ChatTab): FilterCheck[] {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(storageKey(tab)) ?? "[]");
    return Array.isArray(parsed) ? FILTER_CHECKS.filter((c) => parsed.includes(c)) : [];
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
