// The browser half of `ChatPostDTO.reply_excerpt` and
// `ReplyCardAnswerPostDTO.reply_excerpt`: the same trim and the same rune cap
// the server applies to both (server/ocserverd/api_chat.go
// chatReplyExcerptMaxChars). The server refuses an over-long excerpt rather
// than cutting it, so no composer may offer to send one.

export const REPLY_EXCERPT_MAX_CHARS = 1000;

export function normalizeReplyExcerpt(raw: string): string {
  return raw.trim();
}

/** A selection read out of the page as an excerpt. The browser puts blank
 * lines between the blocks it crosses (title, question, each option), and the
 * quote box shows only three lines, so every line break run becomes one. */
export function excerptFromSelection(selected: string): string {
  return normalizeReplyExcerpt(selected.replace(/\s*\n\s*/g, "\n"));
}

/** Counted in code points, as the server counts runes. */
export function replyExcerptTooLong(excerpt: string): boolean {
  return [...excerpt].length > REPLY_EXCERPT_MAX_CHARS;
}
