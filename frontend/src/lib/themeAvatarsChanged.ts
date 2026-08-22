// themeAvatarsChanged.ts — the one notice that says "the faces the roster is
// holding may no longer be the right ones".
//
// WHY THIS EXISTS AT ALL. A member's face is stored per (member, theme), but
// the wire carries only the ALREADY-RESOLVED answer for whatever theme is
// active right now: MemberDTO.avatar_icon_id is one id, picked server-side.
// That makes the roster's copy theme-shaped without saying so — switch themes
// and every card is holding an id from the theme you just left. The id will not
// resolve in the new theme's pool, so each card falls back to that pool's FIRST
// image instead of the one this member actually has recorded there. The data is
// correct and the picture is wrong, which is precisely the silent face-swap the
// per-theme model was built to end.
//
// The same hole opens from the other side: editing or deleting a theme prunes
// the associations it can no longer resolve, so faces change without any member
// row being written.
//
// WHY A WINDOW EVENT AND NOT AN SSE FRAME. The honest fix is the server telling
// everyone, but `spec/sse.md` is FROZEN and defines the `member` topic as "any
// roster write" — none of these three are roster writes, and widening a frozen
// contract is not a call to make while fixing a bug. A window event keeps the
// fix inside this client, where all three actions already happen.
//
// WHAT IT THEREFORE DOES NOT FIX, stated rather than papered over: another tab
// or another device still will not learn about it until it re-reads. That is
// the SAME boundary `hooks/sharedServerSettings.ts` already documents for
// settings ("the server pushes no settings delta, so another tab's save is
// invisible here until a reload, a known and accepted boundary"). This does not
// widen that boundary and does not narrow it. Closing it means giving the
// server a way to say "the roster's projection moved", which is an SSE contract
// change and belongs in its own decision.
export const THEME_AVATARS_CHANGED_EVENT = "oc-theme-avatars-changed";

/** Announce that a theme switch, theme write or theme delete may have changed
 * which image each member resolves to. Safe to call outside a browser (SSR,
 * node test runners): it simply does nothing there. */
export function notifyThemeAvatarsChanged(): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new Event(THEME_AVATARS_CHANGED_EVENT));
}
