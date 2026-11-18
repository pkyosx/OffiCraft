/** The chips under a box that talks to one member (their chat room, or a reply
 * card they opened): that member's own sentences first, then the box's shared
 * list, with identical text shown once. */
export function mergeChatSuggestedReplies(
  own: readonly string[],
  allMembers: readonly string[],
): string[] {
  return [...new Set([...own, ...allMembers])];
}
