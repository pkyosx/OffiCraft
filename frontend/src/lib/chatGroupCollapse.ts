// Which chat groups are folded, kept in this browser per tab and never sent
// to the server (owner: 依瀏覽器記住群組收合狀況，不需要跨設備). Stored as the
// list of folded group ids, with 未分組 as its own marker id.
import type { ChatTab } from "./chatGroups";

const storageKey = (tab: ChatTab) => `oc_chat_group_collapsed_${tab}`;

export function readCollapsed(tab: ChatTab): Record<string, boolean> {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(storageKey(tab)) ?? "[]");
    if (!Array.isArray(parsed)) return {};
    return Object.fromEntries(parsed.filter((id): id is string => typeof id === "string").map((id) => [id, true]));
  } catch {
    return {};
  }
}

/** Saves the folded ids among `known` (the tab's groups and 未分組), so a
 * deleted group's id does not linger, as the ids a later load will show:
 * `stored` gives a group's server id, or null for one not saved yet. */
export function writeCollapsed(
  tab: ChatTab,
  collapsed: Readonly<Record<string, boolean>>,
  known: readonly string[],
  stored: (id: string) => string | null,
): void {
  const ids = known.filter((id) => collapsed[id] === true).map(stored);
  try {
    window.localStorage.setItem(storageKey(tab), JSON.stringify(ids.filter((id) => id !== null)));
  } catch {
    /* storage unavailable — the folding still holds for this mount */
  }
}
