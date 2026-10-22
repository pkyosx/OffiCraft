import type { ActivitySubagent } from "../types";

export interface ActivityTreeNode {
  subagent: ActivitySubagent;
  children: ActivityTreeNode[];
}

/** Nests the running sub-agents by `parentId`, earliest started first at every
 * level. A sub-agent whose parent is null, itself, not in the list, or part of
 * a parent cycle hangs directly under the main agent, so none is ever dropped. */
export function buildActivityTree(subagents: ActivitySubagent[]): ActivityTreeNode[] {
  const sorted = [...subagents].sort((a, b) => a.startedTs - b.startedTs);
  const nodes = new Map(sorted.map((s) => [s.id, { subagent: s, children: [] as ActivityTreeNode[] }]));
  const roots: ActivityTreeNode[] = [];
  const reachesRoot = (s: ActivitySubagent): boolean => {
    const seen = new Set<string>([s.id]);
    let parent = s.parentId;
    while (parent !== null && nodes.has(parent)) {
      if (seen.has(parent)) return false;
      seen.add(parent);
      parent = nodes.get(parent)!.subagent.parentId;
    }
    return true;
  };
  for (const s of sorted) {
    const node = nodes.get(s.id)!;
    const parent = s.parentId !== null && s.parentId !== s.id ? nodes.get(s.parentId) : undefined;
    if (parent && reachesRoot(s)) parent.children.push(node);
    else roots.push(node);
  }
  return roots;
}
