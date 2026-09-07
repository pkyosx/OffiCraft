// api/loreCap.ts — the frontend's ONE copy of the four 傳承 cap ranges (T-33).
//
// ⚠️ THE AUTHORITY IS THE SERVER, NOT THIS FILE. `loreRoleCapCharsDefault` /
// `loreManualCapCharsDefault` / `loreTitleCapCharsDefault` /
// `loreBodyCapCharsDefault` and the two min/max pairs in
// server/ocserverd/domain.go decide what a PATCH is allowed to write; these
// constants exist only so a settings field can refuse an out-of-range value
// before the owner clicks save, instead of letting them collect an HTTP 422
// that reads like a broken system.
//
// 🔴 THESE ARE DELIBERATELY NOT IN docCap.ts, AND THE FLOOR IS WHY — the same
// reason chatBudget.ts and stepNoteCap.ts are their own files. Every
// `doc.cap_chars.*` knob has floor == its own shipped default, because lowering
// a document cap puts existing legal documents into shrink-only mode. A 傳承
// entry has NO EDIT PATH AT ALL, so a lowered cap cannot strand one that is
// already stored — it binds the next write and nothing else. The owner lowered
// two of them himself the day they shipped (title 140 → 80, body 1000 → 500),
// so filing them under the table whose whole meaning is "up only" would put a
// claim into the settings page that is false about these four rows.
//
// 🔴 TWO RANGES, NOT ONE. The two FOLD budgets are document-sized (how much
// 傳承 a boot document or a manual read carries); the two ENTRY bounds are
// sentence-sized (the longest title and body one write may store). A shared
// range would either let a title grow into a document or stop a fold from
// holding more than a paragraph.

/** The shipped defaults, in CHARACTERS (Unicode code points — the unit the
 * server measures in). Used only as the fallback for a caller with no server
 * value yet; the numbers in force always arrive on GET /api/settings. */
export const LORE_CAP_CHARS_DEFAULTS: Record<
  "role" | "manual" | "title" | "body",
  number
> = {
  role: 10000,
  manual: 10000,
  title: 80,
  body: 500,
};

/** The adjustable range for the two FOLD budgets, mirroring the server's 422. */
export const LORE_FOLD_CAP_CHARS_MIN = 100;
export const LORE_FOLD_CAP_CHARS_MAX = 100000;

/** The adjustable range for the two ENTRY bounds, mirroring the server's 422. */
export const LORE_ENTRY_CAP_CHARS_MIN = 10;
export const LORE_ENTRY_CAP_CHARS_MAX = 10000;
