/** The chips under a composer: the most specific list first — the task type's,
 * then the member's own — then the box's shared list, with identical text shown
 * once at its first position. */
export function mergeSuggestedReplies(...lists: readonly (readonly string[])[]): string[] {
  return [...new Set(lists.flat())];
}
