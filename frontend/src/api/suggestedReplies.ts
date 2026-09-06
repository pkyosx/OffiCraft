// api/suggestedReplies.ts — the ONE place the 建議回覆 settings field is spelled
// (T-122).
//
// 🔴 THE FIELD IS NOT IN THE FROZEN WIRE YET. T-121 owns the settings field,
// `spec/openapi.json` and the server side; at the time this was written
// (origin/main 59816a53) `SettingsDTO` declared 28 properties with
// `additionalProperties: false` and none of them was this one. So
// `generated/schema.ts` cannot see the field and `WireServerSettings` cannot
// type it — the read below is deliberately structural, and it is deliberately
// the ONLY read in the tree.
//
// 🔴 THE NAME IS PROVISIONAL. It changes the moment the owner reviews T-121's
// spec diff. That is the whole reason this file exists: a rename is one edit
// here, not a grep across two pages. Nothing outside this module may spell
// `suggested_replies`.
//
// WHY A STRUCTURAL READ IS NOT A LICENCE TO GUESS: an older server (every
// server today) simply omits the field, and the honest reading of that is "no
// suggestions", which renders nothing at all. The reader never manufactures a
// suggestion and never throws — a settings payload the frontend cannot parse
// must not take the reply box down with it.

/** The wire field on `GET /api/settings` carrying the owner's suggested
 * replies. PROVISIONAL — see the file header. */
export const SUGGESTED_REPLIES_WIRE_FIELD = "suggested_replies";

/** Read the suggested replies off a settings payload.
 *
 * Absent, null, not an array, or an array of things that are not usable text
 * all answer `[]` — the same answer as "the owner configured none", which is
 * the only honest reading of a field this frontend cannot verify. Entries are
 * trimmed and blank ones dropped, so a stray empty string in the setting can
 * never render as a chip with no label. */
export function readSuggestedReplies(settings: unknown): string[] {
  if (typeof settings !== "object" || settings === null) return [];
  const raw = (settings as Record<string, unknown>)[
    SUGGESTED_REPLIES_WIRE_FIELD
  ];
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((v): v is string => typeof v === "string")
    .map((v) => v.trim())
    .filter((v) => v.length > 0);
}

/** Put a suggested-replies list ONTO a settings payload — the mock's only door
 * to a field the frozen wire type does not declare. Production never calls
 * this; it exists so `api/mock.ts` can serve the setting without spelling the
 * field name a second time. */
export function withSuggestedReplies<T extends object>(
  settings: T,
  replies: readonly string[]
): T {
  return { ...settings, [SUGGESTED_REPLIES_WIRE_FIELD]: [...replies] };
}
