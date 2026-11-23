/** The chips under a box that talks to one member (their chat room, or a reply
 * card they opened): the most specific list first — for a reply card, the
 * card's task type, then the member's own — then the box's shared list, with
 * identical text shown once at its first position. */
export function mergeSuggestedReplies(...lists: readonly (readonly string[])[]): string[] {
  return [...new Set(lists.flat())];
}
