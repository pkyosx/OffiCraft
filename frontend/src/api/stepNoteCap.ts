// api/stepNoteCap.ts — the frontend's ONE copy of the step-note cap range
// (T-119).
//
// ⚠️ THE AUTHORITY IS THE SERVER, NOT THIS FILE. `stepNoteCapCharsDefault` /
// `minStepNoteCapChars` / `maxStepNoteCapChars` in server/ocserverd/domain.go
// decide what a PATCH is allowed to write; these constants exist only so the
// settings field can refuse an out-of-range value before the owner clicks save,
// instead of letting them collect an HTTP 422 that reads like a broken system.
//
// 🔴 THIS IS DELIBERATELY NOT IN docCap.ts, and the floor is why — the same
// reason chatBudget.ts is its own file. Every `doc.cap_chars.*` knob has
// floor == its own shipped default, because lowering a document cap puts
// existing legal documents into shrink-only mode. A step note is measured only
// when it is WRITTEN, so a note already over a lowered cap still reads back in
// full and merely becomes uneditable; the owner asked for a knob that turns
// both ways, so the floor here is a floor.
//
// 🔴 IT GOVERNS THE STEP NOTE ALONE. The task-level handover note and a chat
// message body keep their own 4,000-character server constant (owner ruling
// 2026-09-06) — raising this number widens neither of them.

/** The shipped default, in CHARACTERS (Unicode code points — the unit the
 * server measures in). Used only as the fallback for a caller with no server
 * value yet; the number in force always arrives on GET /api/settings. */
export const STEP_NOTE_CAP_CHARS_DEFAULT = 10000;

/** The adjustable range, mirroring the server's 422. */
export const STEP_NOTE_CAP_CHARS_MIN = 1000;
export const STEP_NOTE_CAP_CHARS_MAX = 100000;
