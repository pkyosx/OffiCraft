// components/DiffPage.tsx — the compare url opened as ITS OWN PAGE (T-59).
//
// This is the half of the promise that has to work for someone who is NOT in
// the studio: the link was pasted into Slack, or typed into a browser, and the
// signature it carries is the only credential involved. So the page draws the
// comparison and NOTHING that would need a session — no nav, no unread badges,
// no polling of authed endpoints, and no auth wall in front of it (see
// main.tsx, which mounts this ahead of AuthGate when the url carries a ?sig=).
//
// 🔴 "NOTHING THAT WOULD NEED A SESSION" IS A PROMISE ABOUT THE SIGNED FLAVOUR,
// and this page has two. The SAME file is also what an owner sees when he opens
// an UNSIGNED /diff url — that one goes through AuthGate like every other
// address, so the reader is signed in by the time it renders. The one control
// on this page (the external link, T-59) is drawn for that reader only, and
// `sig` is the gate: see the note beside it below.
//
// The comparison itself is DiffScreen, the same component the in-studio modal
// puts inside the preview overlay. This file is a page shell around it and
// almost nothing more — one compare screen, two hosts.

import { useEffect } from "react";
import { useI18n } from "../i18n";
import type { DiffParams } from "../lib/diffLink";
import { DiffScreen } from "./DiffScreen";
import { DiffShareLinkButton } from "./DiffShareLinkButton";
import "./diff-screen.css";

export function DiffPage({ params }: { params: DiffParams }) {
  const { t } = useI18n();
  // The browser tab has to say what it is holding. The studio's own title is
  // the org name (App.tsx), which this page has no business reading: it is
  // server-backed and owner-gated, and this reader may have no session at all.
  useEffect(() => {
    document.title = t.diff.ariaLabel;
  }, [t]);

  return (
    <main className="diff-page">
      <div className="diff-page__frame">
        {/* T-59 — the EXTERNAL link to this comparison, and the ONE control
          * this otherwise chrome-free page carries.
          *
          * 🔴 THE GATE IS `sig`, AND IT IS STRUCTURAL, not a guess about who
          * is looking. main.tsx mounts this page two different ways, and the
          * signature is what tells them apart: a url CARRYING one is mounted
          * AHEAD of AuthGate, for a reader who may have no session at all —
          * exactly the reader who must not be offered a mint they cannot make
          * and must not be handed a way to re-mint the link they were sent.
          * A url with NO signature goes through AuthGate like every other
          * address, so by the time this renders the reader is signed in and
          * the mint will answer. Same rule as the studio's: draw it only where
          * a session is certain.
          *
          * Above the comparison and to the right: the reader's eye starts at
          * the top of the page, and it is the same corner the in-studio host
          * keeps its actions in. */}
        {params.sig === undefined && (
          <div className="diff-page__actions">
            <DiffShareLinkButton params={params} className="diff-page__share" />
          </div>
        )}
        <DiffScreen params={params} />
      </div>
    </main>
  );
}
