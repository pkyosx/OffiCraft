// components/DiffModalHost.tsx — the studio's answer to a compare LINK.
//
// T-59, and it is the acceptance surface: clicking a compare url inside the
// studio must open the comparison IN PLACE. The reader stays on the message
// they were reading, and closing the comparison puts them back on it — no
// navigation, no scroll position thrown away, no tab.
//
// WHY A CONTEXT AND NOT A PROP: the links live in markdown, and markdown is
// rendered by one leaf component (Markdown.tsx) that eighteen surfaces mount —
// chat bubbles, reply cards, task cards, manuals, the guide. Threading an
// "open the compare screen" callback through all of them would be eighteen
// edits to say one thing, and the next surface to render markdown would
// silently not get it. A provider at the app root says it once.
//
// 🔴 NO PROVIDER ⇒ NO INTERCEPTION, and that is the correct default, not a
// degradation: the standalone compare page renders markdown too, and a compare
// link there should navigate like the ordinary link it is. `useDiffOpener`
// answers null outside a provider and the renderer keeps the plain anchor.
//
// WHY NO HISTORY ENTRY: the modal deliberately does NOT push the /diff url.
// The studio's own navigational state lives in the URL HASH (lib/hashRoute.ts),
// and pushing a path-carrying url would drop that hash — so the office behind
// the backdrop would silently reset to home while the reader was reading, and
// the Back button would restore a position they never left. Back therefore does
// what it did before the click, and a refresh reloads the page the reader was
// on with no modal. The way out of the modal is Esc, the ×, or the backdrop —
// the same three ways out every other overlay in the cockpit offers.

import { useCallback, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import type { DiffParams } from "../lib/diffLink";
import { DiffOpenerContext } from "../hooks/useDiffOpener";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";

export function DiffModalHost({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const [open, setOpen] = useState<DiffParams | null>(null);
  const openDiff = useCallback((params: DiffParams) => setOpen(params), []);
  const value = useMemo(() => openDiff, [openDiff]);

  return (
    <DiffOpenerContext.Provider value={value}>
      {children}
      {open !== null && (
        <MarkdownPreviewOverlay
          // The overlay portals to document.body, so it is not nested inside
          // whatever was on screen — the reader's page is untouched underneath.
          title={t.diff.ariaLabel}
          diffParams={open}
          onClose={() => setOpen(null)}
        />
      )}
    </DiffOpenerContext.Provider>
  );
}
