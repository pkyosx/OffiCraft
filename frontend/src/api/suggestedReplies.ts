// api/suggestedReplies.ts — the ONE place the 建議回覆 settings fields are
// spelled (T-122; the 傳承 list T-33).
//
// ONE LIST PER BOX, NOT ONE SHARED LIST. The owner ruled that the 請示卡 reply
// box, the 任務 message box and the 傳承 entry's message box get SEPARATE lists:
// answering a 請示卡, steering a task in progress and asking the writer of a
// 傳承 entry whether it still holds are three different conversations, so a
// sentence written for one is wrong in another's box. They are flat top-level
// wire fields and separate DB rows, never one nested object — patching one can
// then never read-modify-write another.
//
// 🔴 NOTHING OUTSIDE THIS MODULE MAY SPELL EITHER WIRE FIELD. It is what keeps
// a rename one edit instead of a sweep across the api layer, the mock, the
// settings page and two composers. The check, which must print THIS FILE AND
// NOTHING ELSE:
//
//     grep -rln 'suggested_replies_\(reply_card\|task_message\|lore_message\)' \
//       frontend/src --exclude-dir=generated
//
// The two exclusions are load-bearing and were both measured. `generated/` is
// openapi-typescript's output — it spells every wire name in the spec by
// construction, so it is not a violation, and dropping `--exclude-dir` is the
// negative control that proves the grep reaches it. And the pattern matches the
// WIRE names specifically, not the prefix: prose elsewhere legitimately names
// the DOTTED DB keys (`suggested_replies.reply_card`, SettingsPage.tsx) and the
// key family (mock.suggested-replies.test.ts), which are different strings for
// different things. An earlier version of this comment named the loose
// `grep -rn suggested_replies frontend/src`, which has four hits — a check that
// cannot be passed is a check nobody runs.
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

/** The wire field carrying the 傳承 message-box suggestions (T-33) — the box
 * that writes to the person who WROTE that entry. */
export const SUGGESTED_REPLIES_LORE_MESSAGE_FIELD =
  "suggested_replies_lore_message";

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

/** Read the 傳承 message-box suggestions off a settings payload (T-33). Same
 * rules again, and again a SEPARATE read: the three lists say nothing about
 * each other, and a server predating T-33 simply omits this one. */
export function readSuggestedRepliesLoreMessage(settings: unknown): string[] {
  return readList(settings, SUGGESTED_REPLIES_LORE_MESSAGE_FIELD);
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
  suggestedRepliesLoreMessage?: string[];
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
  if (patch.suggestedRepliesLoreMessage !== undefined) {
    out[SUGGESTED_REPLIES_LORE_MESSAGE_FIELD] = [
      ...patch.suggestedRepliesLoreMessage,
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

/** Put a 傳承 message-box list ONTO a settings payload. Mock-only, as above. */
export function withSuggestedRepliesLoreMessage<T extends object>(
  settings: T,
  replies: readonly string[]
): T {
  return {
    ...settings,
    [SUGGESTED_REPLIES_LORE_MESSAGE_FIELD]: [...replies],
  };
}
