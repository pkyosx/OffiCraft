// Office sidebar chat-list groups. The state is that of
// GET/PUT /api/chat/groups: both tabs' groups in one list, staff first, each
// tab's groups in display order. A member in no group is ungrouped; ungrouped
// members are not stored and show after the groups in the tab's default order.

import type { ChatGroupView } from "../api/adapter";

export type ChatTab = ChatGroupView["tab"];
export type ChatGroup = ChatGroupView;

export const GROUP_NAME_MAX = 40;

export type GroupNameProblem = "blank" | "long" | "taken";

export function tabGroups(groups: readonly ChatGroup[], tab: ChatTab): ChatGroup[] {
  return groups.filter((g) => g.tab === tab);
}

/** Replace one tab's groups, keeping staff before outsource. */
function withTab(groups: readonly ChatGroup[], tab: ChatTab, next: ChatGroup[]): ChatGroup[] {
  const staff = tab === "staff" ? next : tabGroups(groups, "staff");
  const outsource = tab === "outsource" ? next : tabGroups(groups, "outsource");
  return [...staff, ...outsource];
}

/** One tab's rows laid out by group. Ids not among `items` are skipped (a
 * member who left), and a member listed twice shows only in its first group. */
export function layoutTab<T>(
  groups: readonly ChatGroup[],
  tab: ChatTab,
  items: readonly T[],
  idOf: (item: T) => string,
): { sections: { group: ChatGroup; items: T[] }[]; ungrouped: T[] } {
  const byId = new Map(items.map((it) => [idOf(it), it]));
  const placed = new Set<string>();
  const sections = tabGroups(groups, tab).map((group) => ({
    group,
    items: group.memberIds.flatMap((id) => {
      const it = byId.get(id);
      if (!it || placed.has(id)) return [];
      placed.add(id);
      return [it];
    }),
  }));
  return { sections, ungrouped: items.filter((it) => !placed.has(idOf(it))) };
}

export function groupNameProblem(
  groups: readonly ChatGroup[],
  tab: ChatTab,
  name: string,
  exceptId?: string,
): GroupNameProblem | null {
  const trimmed = name.trim();
  if (trimmed === "") return "blank";
  if ([...trimmed].length > GROUP_NAME_MAX) return "long";
  if (tabGroups(groups, tab).some((g) => g.id !== exceptId && g.name === trimmed)) return "taken";
  return null;
}

/** Add a group at the top of its tab; the members leave any group they were in. */
export function createGroup(
  groups: readonly ChatGroup[],
  group: { id: string; tab: ChatTab; name: string; memberIds?: string[] },
): ChatGroup[] {
  const memberIds = group.memberIds ?? [];
  const rest = groups.map((g) => ({
    ...g,
    memberIds: g.memberIds.filter((m) => !memberIds.includes(m)),
  }));
  const created: ChatGroup = {
    id: group.id,
    tab: group.tab,
    name: group.name.trim(),
    memberIds: [...memberIds],
    taskTypeKeys: [],
  };
  return withTab(rest, group.tab, [created, ...tabGroups(rest, group.tab)]);
}

export function renameGroup(groups: readonly ChatGroup[], id: string, name: string): ChatGroup[] {
  return groups.map((g) => (g.id === id ? { ...g, name: name.trim() } : g));
}

/** The group's members become ungrouped and its auto-join types are dropped. */
export function deleteGroup(groups: readonly ChatGroup[], id: string): ChatGroup[] {
  return groups.filter((g) => g.id !== id);
}

/** Move a group to `index` among its tab's groups. */
export function reorderGroup(groups: readonly ChatGroup[], id: string, index: number): ChatGroup[] {
  const group = groups.find((g) => g.id === id);
  if (!group) return [...groups];
  const others = tabGroups(groups, group.tab).filter((g) => g.id !== id);
  const at = Math.max(0, Math.min(index, others.length));
  return withTab(groups, group.tab, [...others.slice(0, at), group, ...others.slice(at)]);
}

/** Move a member into `groupId` at `index` (end when omitted), or out of every
 * group when `groupId` is null. */
export function moveMember(
  groups: readonly ChatGroup[],
  memberId: string,
  groupId: string | null,
  index?: number,
): ChatGroup[] {
  return groups.map((g) => {
    const without = g.memberIds.filter((m) => m !== memberId);
    if (g.id !== groupId) return without.length === g.memberIds.length ? g : { ...g, memberIds: without };
    const at = index === undefined ? without.length : Math.max(0, Math.min(index, without.length));
    return { ...g, memberIds: [...without.slice(0, at), memberId, ...without.slice(at)] };
  });
}

/** Set an outsource group's auto-join task types. A type belongs to one
 * group, so it leaves any other group. Nobody moves: a rule applies to workers
 * assigned from now on. */
export function setGroupTaskTypes(
  groups: readonly ChatGroup[],
  id: string,
  typeKeys: readonly string[],
): ChatGroup[] {
  if (!groups.some((g) => g.id === id)) return [...groups];
  return groups.map((g) => {
    if (g.id === id) return { ...g, taskTypeKeys: [...typeKeys] };
    if (g.tab !== "outsource") return g;
    const kept = g.taskTypeKeys.filter((k) => !typeKeys.includes(k));
    return kept.length === g.taskTypeKeys.length ? g : { ...g, taskTypeKeys: kept };
  });
}

/** Outsource workers with this task type that are not in `groupId` — in
 * 未分組 or in another group (`from`) — in `workers` order (the tab's default
 * order): the ones a newly ticked rule offers to move in. */
export function outsideOfType(
  groups: readonly ChatGroup[],
  workers: readonly { id: string; taskTypeKey?: string }[],
  typeKey: string,
  groupId: string,
): { id: string; from: string | null }[] {
  const groupOf = new Map(tabGroups(groups, "outsource").flatMap((g) => g.memberIds.map((m) => [m, g.id] as const)));
  return workers
    .filter((w) => w.taskTypeKey === typeKey && groupOf.get(w.id) !== groupId)
    .map((w) => ({ id: w.id, from: groupOf.get(w.id) ?? null }));
}

/** Append members to the end of a group (they leave any other group). */
export function appendMembers(groups: readonly ChatGroup[], id: string, memberIds: readonly string[]): ChatGroup[] {
  return memberIds.reduce<ChatGroup[]>((acc, m) => moveMember(acc, m, id), [...groups]);
}
