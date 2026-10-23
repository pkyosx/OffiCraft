// "Open the person this @mention names", supplied by the chat room around its
// message bodies. Kept apart from ChatArea for the same reason useDiffOpener
// is: Markdown consumes it, and Markdown must not import a component that
// renders Markdown.
//
// Null outside a provider: every other Markdown surface keeps rendering a
// mention link as the ordinary same-origin anchor it is.

import { createContext, useContext } from "react";
import type { MentionTarget } from "../lib/mentionLinks";

/** Returns the navigation for a target the room knows, or null when it does
 * not (a released outsource worker, a removed member). */
export type MentionOpener = (target: MentionTarget) => (() => void) | null;

export const MentionOpenerContext = createContext<MentionOpener | null>(null);

export function useMentionOpener(): MentionOpener | null {
  return useContext(MentionOpenerContext);
}
