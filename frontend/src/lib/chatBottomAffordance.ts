// lib/chatBottomAffordance.ts — WHICH of the chat's two bottom affordances is
// on screen (T-48).
//
// There are exactly two, they answer the same question ("the newest message is
// not where you are"), and the owner's ruling is that they are MUTUALLY
// EXCLUSIVE: a new-message preview strip takes the place of the round
// jump-to-latest arrow rather than stacking beside it.
//
// 🔴 IT LIVES IN ITS OWN MODULE BECAUSE THE EXCLUSION IS THE BUG-PRONE PART.
// Written inline, the two conditions are two independent booleans and the
// natural mistake is to give the arrow the condition it obviously wants —
// `!latestInView` — which is true in every state the preview is up in. That
// mutant paints BOTH, and nothing about either element's own markup is wrong,
// so only a test that looks for the other one can see it. One function, one
// answer, one thing to mutate.
export type ChatBottomAffordance = "preview" | "arrow" | "none";

export function chatBottomAffordance(state: {
  /** Is the LAST LOADED message row inside the scroll viewport right now? This
   * is a pure geometric fact about the DOM — it does NOT know whether that row
   * is the newest message in the conversation. */
  latestInView: boolean;
  /** Is there an unseen inbound message waiting in the preview strip? */
  hasNewMsgPreview: boolean;
  /** 🔴 T-48 ③: is the loaded thread an ANCHOR WINDOW with more stream below
   * it (useChat's `hasNewer`)? A jump to an old message loads a window from the
   * middle of the history, and scrolling to the bottom of THAT lands on a row
   * that is not remotely the newest one. Without this input `latestInView`
   * alone answers "the last row I can see is the last row I loaded", which was
   * the same question only while the thread was always the live tail — and the
   * arrow would vanish exactly where the owner most needs it. */
  windowHasNewer: boolean;
}): ChatBottomAffordance {
  // The preview wins whenever it exists: it says everything the arrow says AND
  // who wrote what, and clicking it goes to the same place.
  if (state.hasNewMsgPreview) return "preview";
  return state.latestInView && !state.windowHasNewer ? "none" : "arrow";
}
