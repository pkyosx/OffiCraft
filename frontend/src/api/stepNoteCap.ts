// api/stepNoteCap.ts — the frontend's ONE copy of the step-note cap range
// (T-119).
//
// ⚠️ THE AUTHORITY IS THE SERVER, NOT THIS FILE. `stepNoteCapCharsDefault` /
// `minStepNoteCapChars` / `maxStepNoteCapChars` in server/ocserverd/domain.go
// decide what a PATCH is allowed to write; these constants exist only so the
// settings field can refuse an out-of-range value before the owner clicks save,
// instead of letting them collect an HTTP 422 that reads like a broken system.
//
// 🔴 THIS IS DELIBERATELY NOT IN docCap.ts, and the RANGE is why — the same
// reason chatBudget.ts is its own file. It is no longer the direction: since
// owner 2026-09-07 (card rc-5b66ba099e28 option [1]) the eight
// `doc.cap_chars.*` knobs share one floor of DOC_CAP_CHARS_MIN and turn both
// ways, as this one always has. What differs is the numbers — the doc caps run
// DOC_CAP_CHARS_MIN..100000, and this one's floor is 1000, because a step note
// is one agent's handover line rather than a document, and a note capped at a
// couple of hundred characters cannot carry a handover at all.
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
