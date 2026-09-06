// api/suggestedReplies.ts — the ONE place the 建議回覆 settings fields are
// spelled (T-122).
//
// TWO LISTS, NOT ONE. The owner ruled that the 請示卡 reply box and the 任務
// message box get SEPARATE lists: answering a 請示卡 and writing to a task in
// progress are different conversations, so a sentence written for one is wrong
// in the other's box. They are two flat top-level wire fields and two DB rows,
// never one nested object — patching one can then never read-modify-write the
// other.
//
// 🔴 NOTHING OUTSIDE THIS MODULE MAY SPELL EITHER WIRE FIELD. The invariant is
// checkable in one command (`grep -rn suggested_replies frontend/src`), and it
// is what keeps a rename one edit instead of a sweep across the api layer, the
// mock, the settings page and two composers.
//
// The readers stay STRUCTURAL even though the fields are in the frozen spec
// now. A server older than T-122 simply omits them, an owner can hand-edit a
// row, and the honest reading of anything unusable is "no suggestions" — which
// renders nothing at all. A reader never manufactures a suggestion and never
// throws: the chips are a convenience laid over a reply box that must keep
// working on its own.

/** The wire field carrying the 請示卡 reply-box suggestions. */
export const SUGGESTED_REPLIES_REPLY_CARD_FIELD =
  "suggested_replies_reply_card";

/** The wire field carrying the 任務 message-box suggestions. */
export const SUGGESTED_REPLIES_TASK_MESSAGE_FIELD =
  "suggested_replies_task_message";

/** How many sentences ONE list may hold, and how long ONE sentence may be
 * (runes). Mirrors maxSuggestedReplies / maxSuggestedReplyLen in
 * server/ocserverd/settings.go.
 *
 * 🔴 THE AUTHORITY IS THE SERVER, NOT THIS FILE. These exist so the settings
 * page can say the limit before a round trip and the mock can refuse what the
 * real server refuses; the server 422 is what actually decides. */
export const SUGGESTED_REPLIES_MAX_ENTRIES = 20;
export const SUGGESTED_REPLY_MAX_LEN = 120;

function readList(settings: unknown, field: string): string[] {
  if (typeof settings !== "object" || settings === null) return [];
  const raw = (settings as Record<string, unknown>)[field];
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((v): v is string => typeof v === "string")
    .map((v) => v.trim())
    .filter((v) => v.length > 0);
}

/** Read the 請示卡 reply-box suggestions off a settings payload.
 *
 * Absent, null, not an array, or an array of things that are not usable text
 * all answer `[]` — the same answer as "the owner configured none". Entries are
 * trimmed and blank ones dropped, so a stray empty string in the setting can
 * never render as a chip with no label. */
export function readSuggestedRepliesReplyCard(settings: unknown): string[] {
  return readList(settings, SUGGESTED_REPLIES_REPLY_CARD_FIELD);
}

/** Read the 任務 message-box suggestions off a settings payload. Same rules as
 * `readSuggestedRepliesReplyCard`, and deliberately a SEPARATE read: one list
 * being empty says nothing about the other. */
export function readSuggestedRepliesTaskMessage(settings: unknown): string[] {
  return readList(settings, SUGGESTED_REPLIES_TASK_MESSAGE_FIELD);
}

/** The PATCH-body fragment for whichever of the two lists a patch names.
 *
 * `http.ts` spreads this into the settings PATCH body rather than spelling the
 * fields itself — that is what keeps the "one module knows the names" rule true
 * on the WRITE side too, not just the read side. An `undefined` list is left
 * out entirely (omitted = unchanged); an EMPTY one is sent, because `[]` is a
 * legal value that clears the list. */
export function suggestedRepliesPatchFields(patch: {
  suggestedRepliesReplyCard?: string[];
  suggestedRepliesTaskMessage?: string[];
}): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  if (patch.suggestedRepliesReplyCard !== undefined) {
    out[SUGGESTED_REPLIES_REPLY_CARD_FIELD] = [
      ...patch.suggestedRepliesReplyCard,
    ];
  }
  if (patch.suggestedRepliesTaskMessage !== undefined) {
    out[SUGGESTED_REPLIES_TASK_MESSAGE_FIELD] = [
      ...patch.suggestedRepliesTaskMessage,
    ];
  }
  return out;
}

/** Put a 請示卡 reply-box list ONTO a settings payload — the mock's only door to
 * the field name. Production never calls this. */
export function withSuggestedRepliesReplyCard<T extends object>(
  settings: T,
  replies: readonly string[]
): T {
  return {
    ...settings,
    [SUGGESTED_REPLIES_REPLY_CARD_FIELD]: [...replies],
  };
}

/** Put a 任務 message-box list ONTO a settings payload. Mock-only, as above. */
export function withSuggestedRepliesTaskMessage<T extends object>(
  settings: T,
  replies: readonly string[]
): T {
  return {
    ...settings,
    [SUGGESTED_REPLIES_TASK_MESSAGE_FIELD]: [...replies],
  };
}
