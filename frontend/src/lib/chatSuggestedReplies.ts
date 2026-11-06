/** The chips in one chat room: the member's own sentences first, then the
 * all-member ones, with identical text shown once. */
export function mergeChatSuggestedReplies(
  own: readonly string[],
  allMembers: readonly string[],
): string[] {
  return [...new Set([...own, ...allMembers])];
}
