// Fixtures for the T-48 jump-shift guard, in their own module so that BOTH the
// story and the spec can import them.
//
// ⚠️ Not a tidiness choice: Playwright CT rewrites a component import into its
// own registry declaration, so importing a plain value from the SAME module as
// the story fails at collect time with "Identifier … has already been declared"
// and the whole spec file silently reports "No tests found".
export const OWNER = "owner";
export const PEER = "m-aaaaaaaaaaaa";
/** The message the jump lands on. */
export const TARGET_ID = "a40";
/** The waiting card's row, deliberately ABOVE the target. */
export const CARD_ROW_ID = "a35-card";
export const CARD_ID = "rc-shift";
/** How late the card answers. Long enough that a first frame painted without it
 * is unmistakably a first frame, short enough to sit well inside the guard's
 * 500ms window. */
export const CARD_LATENCY_MS = 120;
