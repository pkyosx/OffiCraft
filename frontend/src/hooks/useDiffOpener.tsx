// hooks/useDiffOpener.tsx — "open this comparison in place", published to the
// whole studio tree (T-59).
//
// The context lives HERE, apart from the provider that fills it
// (components/DiffModalHost.tsx), for one concrete reason: the consumer is
// components/Markdown.tsx, and the provider renders the preview overlay, which
// renders Markdown. Importing the provider's module from the renderer would
// close that loop into an import cycle. A context in a module of its own has no
// component in it and therefore no cycle to close.
//
// 🔴 NULL OUTSIDE A PROVIDER, AND THAT IS A REAL ANSWER. The standalone compare
// page and every test that mounts a markdown surface on its own have no studio
// around them, and a compare link there must behave like the ordinary link it
// is. A caller that reads null must NOT swallow the click.

import { createContext, useContext } from "react";
import type { DiffParams } from "../lib/diffLink";

export type DiffOpener = (params: DiffParams) => void;

export const DiffOpenerContext = createContext<DiffOpener | null>(null);

/** Open the compare screen in place, or null where nothing can. */
export function useDiffOpener(): DiffOpener | null {
  return useContext(DiffOpenerContext);
}
