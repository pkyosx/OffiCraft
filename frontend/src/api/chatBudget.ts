// api/chatBudget.ts — the frontend's ONE copy of the wake snapshot's chat
// budget range (T-c9b4).
//
// ⚠️ THE AUTHORITY IS THE SERVER, NOT THIS FILE. `chatBudgetCharsDefault` /
// `minChatBudgetChars` / `maxChatBudgetChars` in server/ocserverd/domain.go
// decide what a PATCH is allowed to write; these constants exist only so the
// settings field can refuse an out-of-range value before the owner clicks save,
// instead of letting them collect an HTTP 422 that reads like a broken system.
//
// 🔴 THIS IS DELIBERATELY NOT IN docCap.ts, and the floor is why. Every
// `doc.cap_chars.*` knob has floor == its own shipped default, because lowering
// a document cap puts existing legal documents into shrink-only mode. The chat
// block has no such state — it is repacked from scratch on every read — so this
// budget is adjustable in BOTH directions, and folding it in beside the doc caps
// would put a knob with the opposite rule under a heading that states theirs.
//
// 🔴 The ceiling is not a round number either: the server reads a bounded window
// of newest messages before packing, and that window has to be able to overrun
// any budget the owner can dial in, or the block would silently under-fill.
// Raising MAX here without raising that window first is a change to server
// behaviour, not a widening of a form field.

/** The shipped default, in CHARACTERS (Unicode code points — the unit the
 * server measures in). Used only as the fallback for a caller with no server
 * value yet; the number in force always arrives on GET /api/settings. */
export const CHAT_BUDGET_CHARS_DEFAULT = 6000;

/** The adjustable range, mirroring the server's 422. */
export const CHAT_BUDGET_CHARS_MIN = 1000;
export const CHAT_BUDGET_CHARS_MAX = 13000;
