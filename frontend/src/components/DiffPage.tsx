// components/DiffPage.tsx — the compare url opened as ITS OWN PAGE (T-59).
//
// This is the half of the promise that has to work for someone who is NOT in
// the studio: the link was pasted into Slack, or typed into a browser, and the
// share link it carries is the only credential involved. So the page draws the
// comparison and NOTHING that would need a session — no nav, no unread badges,
// no polling of authed endpoints, and no auth wall in front of it (see
// main.tsx, which mounts this ahead of AuthGate when the url carries a ?link=).
//
// 🔴 "NOTHING THAT WOULD NEED A SESSION" IS A PROMISE ABOUT THE EXTERNAL FLAVOUR,
// and this page has two. The SAME file is also what an owner sees when he opens
// an UNSIGNED /diff url — that one goes through AuthGate like every other
// address, so the reader is signed in by the time it renders. The one control
// on this page (the share button) is drawn for that reader only, and the
// url's own credential is the gate: see the note beside it below.
//
// The comparison itself is DiffScreen, the same component the in-studio modal
// puts inside the preview overlay. This file is a page shell around it and
// almost nothing more — one compare screen, two hosts.

import { useEffect } from "react";
import { useI18n } from "../i18n";
import { carriesShareCredential, type DiffParams } from "../lib/diffLink";
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
        {/* The share button only for the internal flavour: a url carrying a
          * credential is mounted AHEAD of AuthGate (main.tsx), for a reader
          * who may have no session and must not be offered a re-mint. */}
        {!carriesShareCredential(params) && (
          <div className="diff-page__actions">
            <DiffShareLinkButton params={params} className="diff-page__share" />
          </div>
        )}
        <DiffScreen params={params} />
      </div>
    </main>
  );
}
