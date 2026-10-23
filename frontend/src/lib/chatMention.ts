// The composer half of chat @mentions: who can be picked, which `@query` the
// caret is in, how that query narrows the list, and what picking writes back.

import type { Member } from "../types";
import type { OutsourceWorkerView } from "../api/adapter";
import type { NamedMentionTarget } from "./mentionLinks";

export interface MentionCandidate extends NamedMentionTarget {
  roleName?: string;
  taskNo?: string;
  taskTitle?: string;
}

export interface MentionQuery {
  /** Index of the `@` in the text. */
  start: number;
  /** What sits between the `@` and the caret. */
  query: string;
}

const MAX_QUERY_CHARS = 64;

export function mentionCandidates(
  members: Member[],
  workers: OutsourceWorkerView[],
  outsourceLabel: (codename: string) => string,
): MentionCandidate[] {
  const staff: MentionCandidate[] = members
    .filter((m) => m.kind === "staff")
    .map((m) => ({
      kind: "member",
      id: m.id,
      name: m.name,
      ...(m.roleName ? { roleName: m.roleName } : {}),
    }));
  const outsource: MentionCandidate[] = workers.map((w) => {
    // Same allowlist as the 請示卡 header: a released worker keeps its last
    // task_no, which is a record of finished work, not what it is doing now.
    const onTask =
      (w.status === "assigned" || w.status === "active") && Boolean(w.taskNo);
    return {
      kind: "worker",
      id: w.id,
      name: outsourceLabel(w.codename),
      ...(onTask ? { taskNo: w.taskNo, taskTitle: w.taskTitle ?? "" } : {}),
    };
  });
  return [...staff, ...outsource];
}

export function activeMentionQuery(text: string, caret: number): MentionQuery | null {
  const before = text.slice(0, caret);
  const at = before.lastIndexOf("@");
  if (at < 0) return null;
  if (at > 0 && !/\s/.test(before[at - 1])) return null;
  const query = before.slice(at + 1);
  if (query.includes("\n") || /^\s/.test(query) || query.length > MAX_QUERY_CHARS) {
    return null;
  }
  return { start: at, query };
}

function taskNoMatches(taskNo: string, q: string): boolean {
  const no = taskNo.toLowerCase();
  if (no.startsWith(q)) return true;
  if (!/^\d+$/.test(q)) return false;
  const digits = no.slice(no.lastIndexOf("-") + 1);
  return digits.startsWith(q);
}

function matches(c: MentionCandidate, q: string): boolean {
  if (c.name.toLowerCase().includes(q)) return true;
  if (c.roleName && c.roleName.toLowerCase().includes(q)) return true;
  if (c.taskNo && taskNoMatches(c.taskNo, q)) return true;
  if (c.taskTitle && c.taskTitle.toLowerCase().includes(q)) return true;
  return false;
}

/** Narrow `pool` by `query` (case-insensitive). Names that START with the
 * query come first; everything else keeps the pool's order. */
export function filterMentionCandidates(
  pool: MentionCandidate[],
  query: string,
): MentionCandidate[] {
  const q = query.toLowerCase();
  if (q === "") return pool;
  const hits = pool.filter((c) => matches(c, q));
  const prefix = hits.filter((c) => c.name.toLowerCase().startsWith(q));
  const rest = hits.filter((c) => !c.name.toLowerCase().startsWith(q));
  return [...prefix, ...rest];
}

/** Replace `@query` (from its `@` up to the caret) with `@<name> `, reusing a
 * space already after the query instead of adding a second one. The caret
 * lands after that space. A line break is not reused: the caret would jump to
 * the next line. */
export function insertMention(
  text: string,
  at: MentionQuery,
  name: string,
): { text: string; caret: number } {
  const tail = text.slice(at.start + 1 + at.query.length);
  const inserted = /^[^\S\r\n]/.test(tail) ? `@${name}` : `@${name} `;
  return {
    text: text.slice(0, at.start) + inserted + tail,
    caret: at.start + name.length + 2,
  };
}
