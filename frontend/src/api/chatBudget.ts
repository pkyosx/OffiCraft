// api/chatBudget.ts — the frontend's ONE copy of the wake snapshot's chat
// budget range (T-c9b4).
//
// ⚠️ THE AUTHORITY IS THE SERVER, NOT THIS FILE. `chatBudgetCharsDefault` /
// `minChatBudgetChars` / `maxChatBudgetChars` in server/ocserverd/domain.go
// decide what a PATCH is allowed to write; these constants exist only so the
// settings field can refuse an out-of-range value before the owner clicks save,
// instead of letting them collect an HTTP 422 that reads like a broken system.
//
// 🔴 THIS IS DELIBERATELY NOT IN docCap.ts, and the RANGE is why. It is no
// longer the direction: since owner 2026-09-07 (card rc-5b66ba099e28 option
// [1]) the eight `doc.cap_chars.*` knobs share one floor of DOC_CAP_CHARS_MIN
// and turn both ways, exactly as this one always has. What has not converged is
// the numbers — the doc caps run DOC_CAP_CHARS_MIN..100000, and this budget is
// a per-read packing allowance with its own 1000..13000 (the ceiling below is
// tied to a server window, not to a document size). Folding it into a table
// whose rows all state one range would either silently widen this knob or
// narrow those.
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
