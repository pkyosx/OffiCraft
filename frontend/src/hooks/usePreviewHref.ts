// The attachment's one-hour preview URL, minted when `enabled` — the address
// an HTML or PDF page's frame loads and a 「在新頁面顯示」 anchor opens. Minted
// ahead of any click: a tab opened after an await is eaten by popup blockers.
// `href` is undefined when no link will be minted (no attachment id, or not
// enabled), null until the mint lands and when it fails, so no caller can
// render a link that would 404 or wait for one that is never coming.

import { useEffect, useState } from "react";
import { attachmentPreviewUrl } from "../lib/shareLink";

export function usePreviewHref(
  attachmentId: string | undefined,
  enabled: boolean,
  warnAs: string,
): { href: string | null | undefined; failed: boolean } {
  const [href, setHref] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    setHref(null);
    setFailed(false);
    if (attachmentId === undefined || attachmentId === "" || !enabled) return;
    let alive = true;
    attachmentPreviewUrl(attachmentId)
      .then((next) => {
        if (alive) setHref(next);
      })
      .catch((e) => {
        console.warn(`${warnAs}: preview link failed`, e);
        if (alive) setFailed(true);
      });
    return () => {
      alive = false;
    };
  }, [attachmentId, enabled, warnAs]);
  const willMint = attachmentId !== undefined && attachmentId !== "" && enabled;
  return { href: willMint ? href : undefined, failed };
}
