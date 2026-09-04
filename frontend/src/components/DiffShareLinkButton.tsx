// components/DiffShareLinkButton.tsx — put the EXTERNAL link to THIS comparison
// on the clipboard (T-59, owner 2026-09-03: 「1. 用圖示」).
//
// WHY IT EXISTS: minting the signed link was a CLI action only, so the one
// person who most often has to paste a comparison to someone outside the studio
// was the only one who could not get it. This is that action, on the screen he
// is already looking at.
//
// 🔴 IT IS THE SAME CONTROL AS THE FILE ONE, deliberately. The attachment
// preview's copy-share-link button (MarkdownPreviewOverlay) is the precedent
// for all of it — icon-only with the accessible name carrying the words, three
// states drawn as three different icons, and feedback ONLY on a real success.
// A second idiom for "copy a signed link" would be the thing to avoid, not the
// duplication.
//
// ⚠️ THREE STATES, THREE ICONS. A failed copy drawn as the idle icon is
// indistinguishable from "nothing happened yet", and a copy that silently did
// not happen is the one outcome the reader must not have to guess at. The
// accessible name says the same thing in words.
//
// 🔴 WHO MAY SEE IT: only a reader whose session is CERTAIN. `GET
// /api/diff/share-link` is gated like every other route, so offering this to
// the unauthenticated viewer of a ?sig= link would be a control that fails on
// click — and it is also the one reader who must not be handed a re-mint. This
// component does not test for that itself (a component cannot know whether the
// token it can see is still alive); its two hosts place it only where the
// answer is structural — see the note at each call site.

import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { DiffParams } from "../lib/diffLink";
import { copyDiffShareLink } from "../lib/shareLink";
import { AlertTriangleIcon, CheckIcon, CopyIcon } from "./icons";

/** How long a copy result stays on the icon. Same 2s the file-level control
 * uses — long enough to be read, short enough that the button is idle again
 * before the reader wonders whether it is stuck. */
const FEEDBACK_MS = 2000;

export function DiffShareLinkButton({
  params,
  className,
}: {
  params: DiffParams;
  /** The host's own skin. The look belongs to the surface the button sits on
   * (the overlay header, the standalone page) and is shared through the
   * stylesheet, never by this component naming one host's class. */
  className: string;
}) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const [copyFailed, setCopyFailed] = useState(false);
  // Cleared on unmount: the comparison can be closed inside the feedback
  // window, and a timer that then calls setState is a warning in the console
  // and a leak in a test that mounts this a hundred times.
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  async function onCopy() {
    window.clearTimeout(timer.current);
    setCopied(false);
    setCopyFailed(false);
    try {
      // 🔴 THE AWAIT IS THE POINT. The link does not exist until the server
      // mints it, so nothing may be claimed before this resolves — the failure
      // path below is the whole reason this is not a fire-and-forget.
      await copyDiffShareLink(params);
      setCopied(true);
    } catch (e) {
      console.warn("DiffShareLinkButton: copy compare link failed", e);
      setCopyFailed(true);
    }
    timer.current = window.setTimeout(() => {
      setCopied(false);
      setCopyFailed(false);
    }, FEEDBACK_MS);
  }

  const label = copyFailed
    ? t.diff.shareLinkCopyFailed
    : copied
      ? t.diff.shareLinkCopied
      : t.diff.copyShareLink;

  return (
    <button
      type="button"
      className={className}
      data-testid="diff-share-link"
      // Both, and they are not the same thing: `title` is the sighted reader's
      // tooltip, `aria-label` is the control's name. Icon-only means there is
      // no visible text for either to fall back on.
      aria-label={label}
      title={label}
      onClick={() => void onCopy()}
    >
      {copied ? (
        <CheckIcon size={14} />
      ) : copyFailed ? (
        <AlertTriangleIcon size={14} />
      ) : (
        <CopyIcon size={14} />
      )}
    </button>
  );
}
