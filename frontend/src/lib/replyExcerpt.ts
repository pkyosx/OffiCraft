// The browser half of `ChatPostDTO.reply_excerpt` and
// `ReplyCardAnswerPostDTO.reply_excerpt`: the same trim and the same rune cap
// the server applies to both (server/ocserverd/api_chat.go
// chatReplyExcerptMaxChars). The server refuses an over-long excerpt rather
// than cutting it, so no composer may offer to send one.

export const REPLY_EXCERPT_MAX_CHARS = 1000;

export function normalizeReplyExcerpt(raw: string): string {
  return raw.trim();
}

/** Counted in code points, as the server counts runes. */
export function replyExcerptTooLong(excerpt: string): boolean {
  return [...excerpt].length > REPLY_EXCERPT_MAX_CHARS;
}
