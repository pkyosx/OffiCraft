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
// 🔴 THESE ARE DELIBERATELY NOT IN docCap.ts, AND THE RANGES ARE WHY — the same
// reason chatBudget.ts and stepNoteCap.ts are their own files. It is no longer
// the direction: since owner 2026-09-07 (card rc-5b66ba099e28 option [1]) the
// eight `doc.cap_chars.*` knobs share one floor of DOC_CAP_CHARS_MIN and turn
// both ways, as these four always have. What differs is the numbers — the doc
// caps run DOC_CAP_CHARS_MIN..100000, and the two ENTRY bounds below are
// sentence-sized (10..10000: the owner lowered two of them himself the day they
// shipped, title 140 → 80 and body 1000 → 500, and a floor of 100 would forbid
// the 80 he is sitting on). Filing them as rows of a table that states one
// range would put a bound on this page that is false about these four.
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
